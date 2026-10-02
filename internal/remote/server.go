// Package remote is the web remote: a small HTTP server that shows what is
// playing, takes playback and queue commands, and streams live updates to
// every open page. It reaches the app only through Controller.
package remote

import (
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"mistersubsonic/internal/webguard"
)

// Options configure a Server; zero values take the defaults.
type Options struct {
	MaxClients int           // open event streams allowed; default 8
	Tick       time.Duration // position event period while playing; default 1 s
	Heartbeat  time.Duration // comment line period on idle streams; default 15 s
	Hostnames  []string      // extra Host names accepted besides IPs and localhost
	Log        func(format string, args ...any)
}

// Change says what Notify is telling the streams about.
type Change int

const (
	StateChanged Change = iota
	QueueChanged
)

const (
	maxBody      = 64 << 10
	writeTimeout = 10 * time.Second // a stalled stream is dropped after this
	logEvery     = time.Minute
)

// Server is the web remote.
type Server struct {
	ctl  Controller
	opts Options
	mux  *http.ServeMux

	mu      sync.Mutex
	srv     *http.Server
	ln      net.Listener
	clients map[*client]struct{}
	nclient atomic.Int32 // len(clients), for Notify's cheap path
	done    chan struct{}
	closed  bool

	logMu   sync.Mutex
	lastLog map[string]time.Time

	now    func() time.Time
	ifaces func() ([]net.Addr, error)
}

// client is one event stream. Notify only sets flags and wakes it; the
// stream's own goroutine reads the latest state when it writes, so a slow
// client skips intermediate events and never blocks anyone else.
type client struct {
	wake  chan struct{}
	state atomic.Bool
	queue atomic.Bool
}

// New returns a Server for ctl. Nothing listens until Listen.
func New(ctl Controller, opts Options) *Server {
	if opts.MaxClients <= 0 {
		opts.MaxClients = 8
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}
	s := &Server{
		ctl:     ctl,
		opts:    opts,
		mux:     http.NewServeMux(),
		clients: map[*client]struct{}{},
		done:    make(chan struct{}),
		lastLog: map[string]time.Time{},
		now:     time.Now,
		ifaces:  net.InterfaceAddrs,
	}
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /api/queue", s.handleQueue)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/cmd", s.handleCmd)
	s.mux.HandleFunc("POST /api/play", s.handlePlay)
	return s
}

// Handler is the whole server, guards included.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.guarded) }

// guarded applies the checks every route needs: the Host header against DNS
// rebinding, and for POSTs a JSON content type and a same-origin Origin
// against cross-site requests, then caps the body.
func (s *Server) guarded(w http.ResponseWriter, r *http.Request) {
	if !webguard.HostAllowed(r.Host, s.opts.Hostnames...) {
		s.reject(w, "unexpected Host header")
		return
	}
	if r.Method == http.MethodPost {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			s.reject(w, "POST without a JSON content type")
			return
		}
		if !webguard.SameOrigin(r) {
			s.reject(w, "POST from another origin")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	s.mux.ServeHTTP(w, r)
}

// reject answers 403 and logs the reason, at most once a minute per reason.
// It never logs anything the request carried.
func (s *Server) reject(w http.ResponseWriter, reason string) {
	writeError(w, http.StatusForbidden, reason)
	if s.opts.Log == nil {
		return
	}
	s.logMu.Lock()
	now := s.now()
	quiet := now.Sub(s.lastLog[reason]) < logEvery
	if !quiet {
		s.lastLog[reason] = now
	}
	s.logMu.Unlock()
	if !quiet {
		s.opts.Log("remote: refused a request: %s", reason)
	}
}

// Listen starts serving on addr in a goroutine.
func (s *Server) Listen(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return http.ErrServerClosed
	}
	if s.ln != nil {
		return fmt.Errorf("remote: already listening on %s", s.ln.Addr())
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	return nil
}

// Addr is the listening address, or nil before Listen.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// URLs lists http://<ip>:<port>/ for each non-loopback IPv4 address of the
// machine; it is empty before Listen.
func (s *Server) URLs() []string {
	a, ok := s.Addr().(*net.TCPAddr)
	if !ok {
		return nil
	}
	addrs, err := s.ifaces()
	if err != nil {
		return nil
	}
	var urls []string
	for _, ad := range addrs {
		ipn, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		urls = append(urls, "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(a.Port))+"/")
	}
	return urls
}

// Close stops listening and ends every event stream. It is safe to call twice.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// Notify tells every open stream that the state or queue changed. It never
// blocks, and costs one atomic load when nobody is connected.
func (s *Server) Notify(c Change) {
	if s.nclient.Load() == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for cl := range s.clients {
		if c == QueueChanged {
			cl.queue.Store(true)
		} else {
			cl.state.Store(true)
		}
		select {
		case cl.wake <- struct{}{}:
		default: // already woken; it will see the flag
		}
	}
}

// join registers a stream, or reports false when the server is full or closed.
func (s *Server) join() (*client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.clients) >= s.opts.MaxClients {
		return nil, false
	}
	c := &client{wake: make(chan struct{}, 1)}
	s.clients[c] = struct{}{}
	s.nclient.Store(int32(len(s.clients)))
	return c, true
}

func (s *Server) leave(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.nclient.Store(int32(len(s.clients)))
	s.mu.Unlock()
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	c, ok := s.join()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "too many open remotes")
		return
	}
	defer s.leave(c)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	sendState := func() bool { return send("state", s.ctl.State()) }
	sendQueue := func() bool { return send("queue", queueOrEmpty(s.ctl.Queue())) }

	if !sendState() || !sendQueue() {
		return
	}
	tick := time.NewTicker(s.opts.Tick)
	defer tick.Stop()
	beat := time.NewTicker(s.opts.Heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-c.wake:
			if c.queue.Swap(false) && !sendQueue() {
				return
			}
			if c.state.Swap(false) && !sendState() {
				return
			}
		case <-tick.C:
			if st := s.ctl.State(); st.Status == "playing" && !send("state", st) {
				return
			}
		case <-beat.C:
			rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
