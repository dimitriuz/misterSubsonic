package remote

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

type fakeCtl struct {
	mu     sync.Mutex
	state  State
	queue  QueueView
	cmds   []Command
	plays  []PlayRequest
	doErr  error
	played int
}

func (f *fakeCtl) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
func (f *fakeCtl) setState(st State) { f.mu.Lock(); f.state = st; f.mu.Unlock() }
func (f *fakeCtl) Queue() QueueView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queue
}
func (f *fakeCtl) Do(c Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, c)
	return f.doErr
}
func (f *fakeCtl) Play(p PlayRequest) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plays = append(f.plays, p)
	return f.played, f.doErr
}
func (f *fakeCtl) Library() Library { return nil }
func (f *fakeCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	return nil, "", errors.New("none")
}
func (f *fakeCtl) lastCmd(t *testing.T) Command {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cmds) == 0 {
		t.Fatal("no command reached the controller")
	}
	return f.cmds[len(f.cmds)-1]
}
func (f *fakeCtl) ncmds() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.cmds) }

var song = Song{ID: "s1", Title: "Title", Artist: "Artist", Album: "Album", CoverID: "c1", DurationMS: 200000, Starred: true}

func newFake() *fakeCtl {
	return &fakeCtl{
		state: State{Song: &song, Status: "paused", PositionMS: 1500, DurationMS: 200000, VolumeDB: -12.5, Repeat: "off", Index: 1},
		queue: QueueView{Index: 1, Songs: []Song{song, song}},
	}
}

func do(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "192.168.1.50:8080"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestStateJSON(t *testing.T) {
	s := New(newFake(), Options{})
	w := do(s.Handler(), "GET", "/api/state", "", nil)
	want := `{"song":{"id":"s1","title":"Title","artist":"Artist","album":"Album","cover_id":"c1","duration_ms":200000,"starred":true},"status":"paused","position_ms":1500,"duration_ms":200000,"volume_db":-12.5,"muted":false,"shuffle":false,"repeat":"off","index":1}`
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
}

func TestStateNoSong(t *testing.T) {
	f := newFake()
	f.state = State{Status: "stopped", Repeat: "off"}
	w := do(New(f, Options{}).Handler(), "GET", "/api/state", "", nil)
	if !strings.HasPrefix(w.Body.String(), `{"song":null,"status":"stopped"`) {
		t.Fatal(w.Body)
	}
}

func TestQueueJSON(t *testing.T) {
	f := newFake()
	w := do(New(f, Options{}).Handler(), "GET", "/api/queue", "", nil)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), `{"index":1,"songs":[{"id":"s1"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	f.queue = QueueView{}
	w = do(New(f, Options{}).Handler(), "GET", "/api/queue", "", nil)
	if strings.TrimSpace(w.Body.String()) != `{"index":0,"songs":[]}` {
		t.Fatalf("empty queue: %s", w.Body)
	}
}

func TestCmdValidation(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		want       *Command // what the controller must receive, nil if it must not be called
	}{
		{"toggle", `{"do":"toggle"}`, 200, &Command{Do: "toggle"}},
		{"next", `{"do":"next"}`, 200, &Command{Do: "next"}},
		{"prev", `{"do":"prev"}`, 200, &Command{Do: "prev"}},
		{"clear", `{"do":"clear"}`, 200, &Command{Do: "clear"}},
		{"screenshot", `{"do":"screenshot"}`, 200, &Command{Do: "screenshot"}},
		{"seek", `{"do":"seek","position_ms":4000}`, 200, &Command{Do: "seek", PositionMS: 4000}},
		{"seek negative", `{"do":"seek","position_ms":-1}`, 400, nil},
		{"volume", `{"do":"volume","db":-20}`, 200, &Command{Do: "volume", DB: -20}},
		{"volume clamped low", `{"do":"volume","db":-90}`, 200, &Command{Do: "volume", DB: -60}},
		{"volume clamped high", `{"do":"volume","db":6}`, 200, &Command{Do: "volume", DB: 0}},
		{"mute", `{"do":"mute","on":true}`, 200, &Command{Do: "mute", On: true}},
		{"shuffle", `{"do":"shuffle","on":true}`, 200, &Command{Do: "shuffle", On: true}},
		{"star", `{"do":"star","on":true}`, 200, &Command{Do: "star", On: true}},
		{"repeat all", `{"do":"repeat","mode":"all"}`, 200, &Command{Do: "repeat", Mode: "all"}},
		{"repeat bad", `{"do":"repeat","mode":"twice"}`, 400, nil},
		{"repeat missing", `{"do":"repeat"}`, 400, nil},
		{"jump", `{"do":"jump","index":3,"song_id":"x"}`, 200, &Command{Do: "jump", Index: 3, SongID: "x"}},
		{"jump negative", `{"do":"jump","index":-1}`, 400, nil},
		{"remove", `{"do":"remove","index":0,"song_id":"x"}`, 200, &Command{Do: "remove", SongID: "x"}},
		{"remove negative", `{"do":"remove","index":-2}`, 400, nil},
		{"move", `{"do":"move","from":2,"to":0,"song_id":"x"}`, 200, &Command{Do: "move", From: 2, SongID: "x"}},
		{"move negative", `{"do":"move","from":1,"to":-1}`, 400, nil},
		{"unknown", `{"do":"reboot"}`, 400, nil},
		{"empty do", `{}`, 400, nil},
		{"bad json", `{"do":`, 400, nil},
		{"not an object", `[1]`, 400, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			w := do(New(f, Options{}).Handler(), "POST", "/api/cmd", tc.body, nil)
			if w.Code != tc.code {
				t.Fatalf("code %d, want %d: %s", w.Code, tc.code, w.Body)
			}
			if tc.want == nil {
				if f.ncmds() != 0 {
					t.Fatalf("controller was called: %+v", f.cmds)
				}
				if !strings.HasPrefix(w.Body.String(), `{"error":"`) {
					t.Fatalf("error body %s", w.Body)
				}
				return
			}
			if got := f.lastCmd(t); got != *tc.want {
				t.Fatalf("got %+v, want %+v", got, *tc.want)
			}
		})
	}
}

func TestCmdErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		body string
	}{
		{ErrStale, 409, ErrStale.Error()},
		{ErrBusy, 503, ErrBusy.Error()},
		{ErrNoConnection, 503, ErrNoConnection.Error()},
		{ErrShooting, 409, "remote: screenshot in progress"},
		{ErrNoScreenshots, 503, "remote: screenshots are off"},
		{errors.New("boom http://user:pw@server/rest?t=secret"), 500, "internal error"},
	} {
		f := newFake()
		f.doErr = tc.err
		var logs []string
		s := New(f, Options{Log: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }})
		for _, path := range []string{"/api/cmd", "/api/play"} {
			body := `{"do":"toggle"}`
			if path == "/api/play" {
				body = `{"what":"album","id":"a"}`
			}
			w := do(s.Handler(), "POST", path, body, nil)
			if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != `{"error":"`+tc.body+`"}` {
				t.Errorf("%v %s: %d %s", tc.err, path, w.Code, w.Body)
			}
		}
		if tc.code == 500 {
			if len(logs) != 2 || !strings.Contains(logs[0], "boom") {
				t.Errorf("the real error should reach the log: %q", logs)
			}
		}
	}
}

func TestCmdMissingFields(t *testing.T) {
	for _, body := range []string{
		`{"do":"volume"}`, `{"do":"volume","db":null}`, `{"do":"seek"}`,
		`{"do":"mute"}`, `{"do":"shuffle"}`, `{"do":"star"}`,
	} {
		f := newFake()
		w := do(New(f, Options{}).Handler(), "POST", "/api/cmd", body, nil)
		if w.Code != 400 || f.ncmds() != 0 {
			t.Errorf("%s: %d, calls %d", body, w.Code, f.ncmds())
		}
	}
	f := newFake()
	if w := do(New(f, Options{}).Handler(), "POST", "/api/cmd", `{"do":"mute","on":false}`, nil); w.Code != 200 {
		t.Errorf("explicit false: %d", w.Code)
	}
}

// A library error out of Play or Do is told by its kind, as in the browse
// routes: not found is 404, an unreachable server 503, and the error text
// (it can hold the server's URL) never reaches the page.
func TestPlayLibraryErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		body string
	}{
		{&subsonic.APIError{Code: subsonic.CodeNotFound, Message: "song not found"}, 404, `{"error":"not found"}`},
		{fmt.Errorf("get http://u:secret@host/rest: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}), 503, `{"error":"server unreachable"}`},
		{context.DeadlineExceeded, 503, `{"error":"timed out"}`},
	} {
		f := newFake()
		f.doErr = tc.err
		w := do(New(f, Options{}).Handler(), "POST", "/api/play", `{"what":"songs","ids":["x"]}`, nil)
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
}

func TestPlay(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		want       PlayRequest
	}{
		{"album", `{"what":"album","id":"a1","start":2,"how":"now"}`, 200, PlayRequest{What: "album", ID: "a1", Start: 2, How: "now"}},
		{"default how", `{"what":"playlist","id":"p1"}`, 200, PlayRequest{What: "playlist", ID: "p1", How: "now"}},
		{"artist end", `{"what":"artist","id":"r1","how":"end"}`, 200, PlayRequest{What: "artist", ID: "r1", How: "end"}},
		{"songs next", `{"what":"songs","ids":["a","b"],"how":"next"}`, 200, PlayRequest{What: "songs", IDs: []string{"a", "b"}, How: "next"}},
		{"album no id", `{"what":"album"}`, 400, PlayRequest{}},
		{"songs no ids", `{"what":"songs","how":"now"}`, 400, PlayRequest{}},
		{"unknown what", `{"what":"genre","id":"x"}`, 400, PlayRequest{}},
		{"unknown how", `{"what":"album","id":"x","how":"later"}`, 400, PlayRequest{}},
		{"negative start", `{"what":"album","id":"x","start":-1}`, 400, PlayRequest{}},
		{"bad json", `nope`, 400, PlayRequest{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.played = 12
			w := do(New(f, Options{}).Handler(), "POST", "/api/play", tc.body, nil)
			if w.Code != tc.code {
				t.Fatalf("code %d: %s", w.Code, w.Body)
			}
			if tc.code != 200 {
				if len(f.plays) != 0 {
					t.Fatal("controller was called")
				}
				return
			}
			if strings.TrimSpace(w.Body.String()) != `{"added":12}` {
				t.Fatal(w.Body)
			}
			if got := f.plays[0]; got.What != tc.want.What || got.ID != tc.want.ID || got.Start != tc.want.Start || got.How != tc.want.How || strings.Join(got.IDs, ",") != strings.Join(tc.want.IDs, ",") {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	f := newFake()
	f.doErr = ErrBusy
	if w := do(New(f, Options{}).Handler(), "POST", "/api/play", `{"what":"album","id":"a"}`, nil); w.Code != 503 {
		t.Fatalf("busy play: %d", w.Code)
	}
}

func TestGuards(t *testing.T) {
	f := newFake()
	var logs []string
	s := New(f, Options{Hostnames: []string{"mister"}, Log: func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }})
	h := s.Handler()
	req := func(method, path, host, ctype, origin string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"do":"toggle"}`))
		r.Host = host
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	tests := []struct {
		name                              string
		method, path, host, ctype, origin string
		code                              int
	}{
		{"ip host", "GET", "/api/state", "192.168.1.50:8080", "", "", 200},
		{"localhost", "GET", "/api/state", "localhost:8080", "", "", 200},
		{"own name", "GET", "/api/state", "mister:8080", "", "", 200},
		{"dns name", "GET", "/api/state", "evil.example:8080", "", "", 403},
		{"dns name on events", "GET", "/api/events", "evil.example", "", "", 403},
		{"post ok", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "", 200},
		{"post json with charset", "POST", "/api/cmd", "192.168.1.50:8080", "application/json; charset=utf-8", "", 200},
		{"post same origin", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "http://192.168.1.50:8080", 200},
		{"post foreign origin", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "http://evil.example", 403},
		{"post form", "POST", "/api/cmd", "192.168.1.50:8080", "application/x-www-form-urlencoded", "", 403},
		{"post text/plain", "POST", "/api/cmd", "192.168.1.50:8080", "text/plain", "", 403},
		{"post no type", "POST", "/api/cmd", "192.168.1.50:8080", "", "", 403},
		{"post origin null", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "null", 403},
		{"post mixed-case type", "POST", "/api/cmd", "192.168.1.50:8080", "Application/JSON; charset=utf-8", "", 200},
		{"post dns host", "POST", "/api/play", "evil.example", "application/json", "", 403},
	}
	for _, tc := range tests {
		before := f.ncmds()
		if got := req(tc.method, tc.path, tc.host, tc.ctype, tc.origin); got != tc.code {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.code)
		}
		if tc.code == 403 && f.ncmds() != before {
			t.Errorf("%s: refused request reached the controller", tc.name)
		}
	}
	for _, l := range logs {
		if strings.Contains(l, "evil") {
			t.Errorf("log carries request data: %q", l)
		}
	}
}

func TestBodyCap(t *testing.T) {
	f := newFake()
	h := New(f, Options{}).Handler()
	big := `{"do":"toggle","mode":"` + strings.Repeat("x", 70<<10) + `"}`
	if w := do(h, "POST", "/api/cmd", big, nil); w.Code != 413 {
		t.Fatalf("oversized body: %d", w.Code)
	}
	if f.ncmds() != 0 {
		t.Fatal("oversized body reached the controller")
	}
	ok := `{"do":"toggle","mode":"` + strings.Repeat("x", 60<<10) + `"}`
	if w := do(h, "POST", "/api/cmd", ok, nil); w.Code != 200 {
		t.Fatalf("body under the cap: %d", w.Code)
	}
}

func TestRejectLogRateLimit(t *testing.T) {
	var logs []string
	s := New(newFake(), Options{Log: func(format string, args ...any) { logs = append(logs, format) }})
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	bad := func() { do(s.Handler(), "POST", "/api/cmd", "{}", map[string]string{"Content-Type": "text/plain"}) }
	bad()
	bad()
	if len(logs) != 1 {
		t.Fatalf("%d log lines for one reason within a minute", len(logs))
	}
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "evil.example"
	s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	if len(logs) != 2 {
		t.Fatalf("a second reason should log: %d", len(logs))
	}
	now = now.Add(61 * time.Second)
	bad()
	if len(logs) != 3 {
		t.Fatalf("after a minute it should log again: %d", len(logs))
	}
}

func TestUnknownRoutes(t *testing.T) {
	h := New(newFake(), Options{}).Handler()
	if w := do(h, "GET", "/api/nothing", "", nil); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if w := do(h, "GET", "/api/cmd", "", nil); w.Code != 405 {
		t.Fatalf("GET on cmd: %d", w.Code)
	}
}

func TestURLs(t *testing.T) {
	s := New(newFake(), Options{})
	if len(s.URLs()) != 0 {
		t.Fatal("URLs before Listen")
	}
	s.ifaces = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("10.0.0.7"), Mask: net.CIDRMask(8, 32)},
			&net.UnixAddr{Name: "x", Net: "unix"},
		}, nil
	}
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	port := s.Addr().(*net.TCPAddr).Port
	got := strings.Join(s.URLs(), " ")
	want := "http://192.168.1.50:" + strconv.Itoa(port) + "/ http://10.0.0.7:" + strconv.Itoa(port) + "/"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	s.ifaces = func() ([]net.Addr, error) { return nil, errors.New("no interfaces") }
	if len(s.URLs()) != 0 {
		t.Fatal("URLs with a failing lister")
	}
}

func TestListenServes(t *testing.T) {
	s := New(newFake(), Options{})
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + s.Addr().String() + "/api/state")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp)
	}
	resp.Body.Close()
	if err := s.Listen("127.0.0.1:0"); err == nil {
		t.Fatal("second Listen should fail")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + s.Addr().String() + "/api/state"); err == nil {
		t.Fatal("still serving after Close")
	}
	if err := s.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
}

// ---- SSE ----

type event struct{ name, data string }

type stream struct {
	resp *http.Response
	ev   chan event
	all  chan string // every raw line
}

func open(t *testing.T, url string) *stream {
	t.Helper()
	resp, err := http.Get(url + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	st := &stream{resp: resp, ev: make(chan event, 100), all: make(chan string, 1000)}
	go func() {
		defer close(st.ev)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 4<<20)
		var name, data string
		for sc.Scan() {
			l := sc.Text()
			select {
			case st.all <- l:
			default:
			}
			switch {
			case strings.HasPrefix(l, "event: "):
				name = l[7:]
			case strings.HasPrefix(l, "data: "):
				data = l[6:]
			case l == "":
				if name != "" {
					st.ev <- event{name, data}
				}
				name, data = "", ""
			}
		}
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return st
}

func (s *stream) next(t *testing.T) event {
	t.Helper()
	select {
	case e, ok := <-s.ev:
		if !ok {
			t.Fatal("stream ended")
		}
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	return event{}
}

func (s *stream) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case e := <-s.ev:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(d):
	}
}

func startServer(t *testing.T, f *fakeCtl, o Options) (*Server, string) {
	t.Helper()
	s := New(f, o)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { s.Close() })
	return s, ts.URL
}

func waitClients(t *testing.T, s *Server, n int) {
	t.Helper()
	for i := 0; i < 300; i++ {
		s.mu.Lock()
		c := len(s.clients)
		s.mu.Unlock()
		if c == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("clients never reached %d", n)
}

func TestSSEInitialAndNotify(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})
	st := open(t, url)
	if ct := st.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"status":"paused"`) {
		t.Fatalf("first: %+v", e)
	}
	if e := st.next(t); e.name != "queue" || !strings.Contains(e.data, `"songs":[{"id":"s1"`) {
		t.Fatalf("second: %+v", e)
	}
	waitClients(t, s, 1)

	f.setState(State{Status: "playing", Repeat: "off"})
	s.Notify(StateChanged)
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"status":"playing"`) {
		t.Fatalf("after notify: %+v", e)
	}
	f.mu.Lock()
	f.queue = QueueView{Index: 0, Songs: []Song{}}
	f.mu.Unlock()
	s.Notify(QueueChanged)
	if e := st.next(t); e.name != "queue" || e.data != `{"index":0,"songs":[]}` {
		t.Fatalf("queue notify: %+v", e)
	}
	st.none(t, 50*time.Millisecond) // a queue change sends no state event
}

func TestSSEBroadcastToAll(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})
	a, b := open(t, url), open(t, url)
	for _, st := range []*stream{a, b} {
		st.next(t)
		st.next(t)
	}
	waitClients(t, s, 2)
	s.Notify(StateChanged)
	if a.next(t).name != "state" || b.next(t).name != "state" {
		t.Fatal("not every client got the event")
	}
}

func TestSSETickOnlyWhilePlaying(t *testing.T) {
	f := newFake() // paused
	s, url := startServer(t, f, Options{Tick: 20 * time.Millisecond, Heartbeat: time.Hour})
	st := open(t, url)
	st.next(t)
	st.next(t)
	waitClients(t, s, 1)
	st.none(t, 150*time.Millisecond)

	f.setState(State{Status: "playing", PositionMS: 42, Repeat: "off"})
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"position_ms":42`) {
		t.Fatalf("tick: %+v", e)
	}
	st.next(t)
	st.next(t)

	f.setState(State{Status: "paused", Repeat: "off"})
	for len(st.ev) > 0 { // drop ticks already in flight
		<-st.ev
	}
	time.Sleep(50 * time.Millisecond)
	for len(st.ev) > 0 {
		<-st.ev
	}
	st.none(t, 150*time.Millisecond)
}

func TestSSEHeartbeat(t *testing.T) {
	_, url := startServer(t, newFake(), Options{Tick: time.Hour, Heartbeat: 20 * time.Millisecond})
	st := open(t, url)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l := <-st.all:
			if l == "event: ping" {
				return
			}
		case <-deadline:
			t.Fatal("no ping event")
		}
	}
}

func TestSSECapAndDisconnect(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{MaxClients: 2, Tick: time.Hour, Heartbeat: time.Hour})
	a, b := open(t, url), open(t, url)
	a.next(t)
	b.next(t)
	waitClients(t, s, 2)

	// Over the cap the answer is a short stream with one "full" event, so the
	// page (which can't read an EventSource status) can tell the user why.
	f9 := open(t, url)
	if e := f9.next(t); e.name != "full" {
		t.Fatalf("over the cap: %+v", e)
	}
	if _, ok := <-f9.ev; ok {
		t.Fatal("the full stream did not end")
	}

	a.resp.Body.Close() // a disconnects
	waitClients(t, s, 1)
	c := open(t, url)
	if e := c.next(t); e.name != "state" {
		t.Fatalf("the freed slot: %+v", e)
	}
}

func TestSSECloseEndsStreams(t *testing.T) {
	s, url := startServer(t, newFake(), Options{Tick: time.Hour, Heartbeat: time.Hour})
	st := open(t, url)
	st.next(t)
	st.next(t)
	waitClients(t, s, 1)
	s.Close()
	select {
	case _, ok := <-st.ev:
		if ok {
			t.Fatal("event after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open after Close")
	}
	waitClients(t, s, 0)
	if resp, err := http.Get(url + "/api/events"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Fatalf("stream opened after Close: %d", resp.StatusCode)
		}
	}
}

func TestNotifyNeverBlocksOnASlowClient(t *testing.T) {
	f := newFake()
	f.queue = QueueView{Songs: make([]Song, 2000)} // big events fill the socket buffers quickly
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})

	conn, err := net.Dial("tcp", strings.TrimPrefix(url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(*net.TCPConn).SetReadBuffer(1024)
	io.WriteString(conn, "GET /api/events HTTP/1.1\r\nHost: localhost\r\n\r\n") // and never read
	waitClients(t, s, 1)

	fast := open(t, url)
	fast.next(t)
	fast.next(t)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			s.Notify(QueueChanged)
			s.Notify(StateChanged)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Notify blocked on a client that is not reading")
	}
	s.Notify(StateChanged)
	for { // the reading client still gets the latest state
		if e := fast.next(t); e.name == "state" {
			break
		}
	}
}

func TestNotifyWithoutClientsIsCheap(t *testing.T) {
	s := New(newFake(), Options{})
	for i := 0; i < 100000; i++ {
		s.Notify(StateChanged)
	}
}

func TestCrossSiteGetsRefused(t *testing.T) {
	srv, f := browse(&fakeLib{})
	h := srv.Handler()
	f.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return []byte("\xff\xd8jpeg"), "image/jpeg", nil
	}
	for _, path := range []string{"/api/state", "/api/cover/x", "/api/search?q=ab", "/api/albums?list=recent"} {
		for _, tc := range []struct {
			name string
			hdr  map[string]string
			code int
		}{
			{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
			{"same-site", map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
			{"foreign origin", map[string]string{"Origin": "http://evil.example"}, 403},
			{"origin null", map[string]string{"Origin": "null"}, 403},
			{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
			{"typed address", map[string]string{"Sec-Fetch-Site": "none"}, 200},
			{"same origin header", map[string]string{"Origin": "http://192.168.1.50:8080"}, 200},
			{"no headers", nil, 200},
		} {
			if w := do(h, "GET", path, "", tc.hdr); w.Code != tc.code {
				t.Errorf("%s %s: %d, want %d", path, tc.name, w.Code, tc.code)
			}
		}
	}
	// The events stream is guarded the same way (a refused one never joins).
	if w := do(h, "GET", "/api/events", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != 403 {
		t.Errorf("events cross-site: %d", w.Code)
	}
	// The page itself stays open: a link from another site may open it.
	for _, p := range []string{"/", "/app.js", "/app.css"} {
		if w := do(h, "GET", p, "", map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != 200 {
			t.Errorf("%s cross-site: %d", p, w.Code)
		}
	}
}

func TestJSONNosniff(t *testing.T) {
	h := New(newFake(), Options{}).Handler()
	for _, w := range []*httptest.ResponseRecorder{
		do(h, "GET", "/api/state", "", nil),
		do(h, "GET", "/api/queue", "", nil),
		do(h, "GET", "/api/nope", "", nil),
		do(h, "POST", "/api/cmd", `{"do":"bogus"}`, nil),
	} {
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%d: nosniff %q", w.Code, got)
		}
	}
}

// A body that never finishes is cut off by the read deadline.
func TestPostBodyReadDeadline(t *testing.T) {
	f := newFake()
	s := New(f, Options{})
	s.bodyTimeout = 200 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /api/cmd HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"do\":")
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	b, _ := io.ReadAll(conn) // ends when the server answers and closes
	if time.Since(start) > 2*time.Second || f.ncmds() != 0 {
		t.Fatalf("the trickled body held the connection %v (%d commands, %q)", time.Since(start), f.ncmds(), b)
	}
}
