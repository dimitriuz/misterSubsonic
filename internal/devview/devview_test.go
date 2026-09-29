package devview

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
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
	v.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
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
