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
