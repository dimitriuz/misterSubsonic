package devview

import (
	"bytes"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func TestFrameLongPollAndKeys(t *testing.T) {
	v := New(8, 4)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	defer v.Close()

	got := make(chan *http.Response, 1)
	go func() {
		r, err := http.Get(srv.URL + "/frame?after=0")
		if err == nil {
			got <- r
		}
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("frame returned before anything was presented")
	default:
	}
	c := gfx.NewCanvas(8, 4)
	c.Clear(gfx.RGB(10, 20, 30))
	v.Present(c)
	var r *http.Response
	select {
	case r = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("long poll not woken by Present")
	}
	defer r.Body.Close()
	if r.Header.Get("X-Seq") != "1" || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("headers %v", r.Header)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	img, err := png.Decode(&buf)
	if err != nil || img.Bounds().Dx() != 8 {
		t.Fatalf("frame png: %v", err)
	}

	resp, _ := http.Post(srv.URL+"/key?b=a&down=1", "", nil)
	resp.Body.Close()
	resp, _ = http.Post(srv.URL+"/key?b=a&down=0", "", nil)
	resp.Body.Close()
	if e := <-v.Events(); e != (input.Event{Button: input.BtnA, Kind: input.Press}) {
		t.Fatalf("event %+v", e)
	}
	if e := <-v.Events(); e.Kind != input.Release {
		t.Fatalf("event %+v", e)
	}
	resp, _ = http.Post(srv.URL+"/key?b=bogus&down=1", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown button status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPageServed(t *testing.T) {
	v := New(1, 1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1:8090"
	v.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("/frame?after=")) {
		t.Fatalf("page: %d", rec.Code)
	}
}

func TestCloseReleasesWaiters(t *testing.T) {
	v := New(1, 1)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	done := make(chan int, 1)
	go func() {
		r, err := http.Get(srv.URL + "/frame?after=0")
		if err == nil {
			r.Body.Close()
			done <- r.StatusCode
		}
	}()
	time.Sleep(30 * time.Millisecond)
	v.Close()
	select {
	case code := <-done:
		if code != http.StatusNoContent {
			t.Fatalf("status %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter not released on Close")
	}
}

func TestReleaseIsNotDroppedWhenFull(t *testing.T) {
	v := New(1, 1)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	for i := 0; i < 64; i++ {
		resp, err := http.Post(srv.URL+"/key?b=a&down=1", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	done := make(chan struct{})
	go func() {
		resp, err := http.Post(srv.URL+"/key?b=a&down=0", "", nil)
		if err == nil {
			resp.Body.Close()
		}
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	<-v.Events() // make room
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("release POST did not complete")
	}
	var last input.Event
	for i := 0; i < 64; i++ {
		select {
		case last = <-v.Events():
		case <-time.After(time.Second):
			t.Fatalf("only %d events queued", i)
		}
	}
	if last.Kind != input.Release {
		t.Fatalf("last event %+v, want Release", last)
	}
}

func TestKeyRejectsCrossOrigin(t *testing.T) {
	v := New(1, 1)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	post := func(hdr map[string]string) int {
		req, _ := http.NewRequest("POST", srv.URL+"/key?b=start&down=1", nil)
		for k, val := range hdr {
			req.Header.Set(k, val)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post(map[string]string{"Origin": "http://evil.example"}); c != http.StatusForbidden {
		t.Fatalf("cross-origin status %d", c)
	}
	if c := post(map[string]string{"Sec-Fetch-Site": "cross-site"}); c != http.StatusForbidden {
		t.Fatalf("cross-site status %d", c)
	}
	select {
	case e := <-v.Events():
		t.Fatalf("unexpected event %+v", e)
	default:
	}
	if c := post(map[string]string{"Origin": srv.URL, "Sec-Fetch-Site": "same-origin"}); c != http.StatusNoContent {
		t.Fatalf("same-origin status %d", c)
	}
}

func TestRejectsForeignHost(t *testing.T) {
	v := New(2, 2)
	v.Present(gfx.NewCanvas(2, 2))
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	do := func(method, path, host string) int {
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, ep := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/frame?after=0"}, {"POST", "/key?b=start&down=1"}} {
		if c := do(ep.method, ep.path, "evil.example:"+port); c != http.StatusForbidden {
			t.Errorf("%s %s with foreign Host: status %d, want 403", ep.method, ep.path, c)
		}
	}
	select {
	case e := <-v.Events():
		t.Fatalf("event from a refused request: %+v", e)
	default:
	}
	for _, ep := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/frame?after=0"}, {"POST", "/key?b=start&down=1"}} {
		for _, h := range []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port, "127.0.0.2"} {
			if c := do(ep.method, ep.path, h); c == http.StatusForbidden {
				t.Errorf("%s %s with Host %s: refused", ep.method, ep.path, h)
			}
		}
	}
}

func TestURLForWildcardBindIsLoopback(t *testing.T) {
	if got := pageURL(&net.TCPAddr{IP: net.IPv6unspecified, Port: 8090}); got != "http://127.0.0.1:8090/" {
		t.Fatalf("wildcard URL = %q", got)
	}
	if got := pageURL(&net.TCPAddr{IP: net.IPv4(192, 168, 1, 5), Port: 8090}); got != "http://192.168.1.5:8090/" {
		t.Fatalf("LAN URL = %q", got)
	}
}

func TestTextKeys(t *testing.T) {
	v := New(1, 1)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	post := func(q string) int {
		resp, err := http.Post(srv.URL+"/key?"+q, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for q, want := range map[string]input.Event{
		"t=a&down=1":       {Kind: input.Press, Rune: 'a'},
		"b=y&t=n&down=1":   {Button: input.BtnY, Kind: input.Press, Rune: 'n'},
		"t=%D0%B6&down=1":  {Kind: input.Press, Rune: 'ж'},
		"b=b&t=%08&down=1": {Button: input.BtnB, Kind: input.Press, Rune: '\b'},
		"b=queue&down=1":   {Button: input.BtnQueue, Kind: input.Press},
		"b=mute&down=1":    {Button: input.BtnMute, Kind: input.Press},
	} {
		if c := post(q); c != http.StatusNoContent {
			t.Fatalf("%s: status %d", q, c)
		}
		if e := <-v.Events(); e != want {
			t.Fatalf("%s: event %+v, want %+v", q, e, want)
		}
	}
	if c := post("down=1"); c != http.StatusBadRequest {
		t.Fatalf("no button or text: status %d", c)
	}
}

// After an app restart the frame counter starts over; a tab still polling
// with the old, higher sequence number must get the current frame at once.
func TestFramePollWithStaleSeqReturnsCurrent(t *testing.T) {
	v := New(2, 2)
	v.Present(gfx.NewCanvas(2, 2))
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	defer v.Close()
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(srv.URL + "/frame?after=500")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Seq") != "1" {
		t.Fatalf("status %d seq %q, want 200 seq 1", resp.StatusCode, resp.Header.Get("X-Seq"))
	}
}
