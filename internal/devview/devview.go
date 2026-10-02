// Package devview is the development display: it serves the UI's frames to
// a browser page and turns the browser's key presses into input events, so
// the whole app runs on a PC without a MiSTer.
package devview

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"image/png"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/webguard"
)

//go:embed page.html
var page []byte

var buttons = map[string]input.Button{
	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
	"queue": input.BtnQueue, "mute": input.BtnMute,
	"volup": input.BtnVolUp, "voldown": input.BtnVolDown, "playpause": input.BtnPlayPause,
	"next": input.BtnNextTrack, "prev": input.BtnPrevTrack, "ffwd": input.BtnSeekFwd, "rewind": input.BtnSeekBack,
	"screenshot": input.BtnScreenshot,
}

// Viewer is a gfx.Display that browsers watch.
type Viewer struct {
	w, h   int
	events chan input.Event
	srv    *http.Server
	ln     net.Listener

	mu    sync.Mutex
	cond  *sync.Cond
	frame []byte
	seq   int
	done  bool
}

// New creates a viewer for w×h frames without starting a server (tests use Handler).
func New(w, h int) *Viewer {
	v := &Viewer{w: w, h: h, events: make(chan input.Event, 64)}
	v.cond = sync.NewCond(&v.mu)
	return v
}

// Listen starts serving on addr (e.g. "127.0.0.1:8090").
func (v *Viewer) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	v.ln = ln
	v.srv = &http.Server{Handler: v.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go v.srv.Serve(ln)
	return nil
}

// URL is the page address once Listen succeeded. For a wildcard bind
// (":8090", "0.0.0.0:8090") it names this machine's loopback address.
func (v *Viewer) URL() string {
	if v.ln == nil {
		return ""
	}
	return pageURL(v.ln.Addr().(*net.TCPAddr))
}

func pageURL(addr *net.TCPAddr) string {
	host := addr.IP.String()
	if addr.IP.IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(addr.Port)) + "/"
}

// Exposed reports whether the viewer listens beyond loopback: anyone on
// the network can then watch the screen and press keys.
func (v *Viewer) Exposed() bool {
	return v.ln != nil && !v.ln.Addr().(*net.TCPAddr).IP.IsLoopback()
}

func (v *Viewer) Events() <-chan input.Event { return v.events }
func (v *Viewer) Size() (int, int)           { return v.w, v.h }

func (v *Viewer) Present(c *gfx.Canvas) error {
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, c.ToRGBA()); err != nil {
		return err
	}
	v.mu.Lock()
	v.frame = b.Bytes()
	v.seq++
	v.cond.Broadcast()
	v.mu.Unlock()
	return nil
}

func (v *Viewer) Close() error {
	v.mu.Lock()
	v.done = true
	v.cond.Broadcast()
	v.mu.Unlock()
	if v.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return v.srv.Shutdown(ctx)
	}
	return nil
}

// Handler serves the page (/), frames (/frame?after=N, long-poll) and keys
// (POST /key?b=a&down=1, with t=<character> for keys that type).
func (v *Viewer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /frame", v.serveFrame)
	mux.HandleFunc("POST /key", func(w http.ResponseWriter, r *http.Request) {
		if !webguard.SameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		var e input.Event
		if name := q.Get("b"); name != "" {
			b, ok := buttons[name]
			if !ok {
				http.Error(w, "unknown button", http.StatusBadRequest)
				return
			}
			e.Button = b
		}
		if t := []rune(q.Get("t")); len(t) == 1 {
			e.Rune = t[0]
		}
		if e.Button == input.BtnNone && e.Rune == 0 {
			http.Error(w, "no button or text", http.StatusBadRequest)
			return
		}
		e.Kind = input.Release
		if q.Get("down") == "1" {
			e.Kind = input.Press
		}
		if e.Kind == input.Release {
			// A lost Release would leave the button stuck (auto-repeating).
			select {
			case v.events <- e:
			case <-time.After(time.Second):
			}
		} else {
			select {
			case v.events <- e:
			default: // UI not keeping up; drop rather than block the browser
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return webguard.CheckHost(mux)
}

var errGone = errors.New("viewer closed")

func (v *Viewer) waitFrame(ctx context.Context, after int) ([]byte, int, error) {
	stop := context.AfterFunc(ctx, func() { v.mu.Lock(); v.cond.Broadcast(); v.mu.Unlock() })
	defer stop()
	v.mu.Lock()
	defer v.mu.Unlock()
	// after > seq means the app restarted and the counter started over: hand
	// over the current frame instead of waiting for a number never reached.
	for v.seq <= after && !(after > v.seq && v.frame != nil) && !v.done && ctx.Err() == nil {
		v.cond.Wait()
	}
	if v.done {
		return nil, 0, errGone
	}
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	return v.frame, v.seq, nil
}

func (v *Viewer) serveFrame(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	frame, seq, err := v.waitFrame(ctx, after)
	if err != nil {
		w.WriteHeader(http.StatusNoContent) // client just polls again
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Seq", strconv.Itoa(seq))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(frame)
}
