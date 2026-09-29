package art

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/subsonic"
)

func pngBytes(w, h int) []byte {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = 0xFF
	}
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

type harness struct {
	l     *Loader
	calls atomic.Int32
	ready chan Key
}

func newHarness(t *testing.T, fetch func(Key) ([]byte, error), o Options) *harness {
	h := &harness{ready: make(chan Key, 64)}
	o.Fetch = func(_ context.Context, k Key) ([]byte, error) { h.calls.Add(1); return fetch(k) }
	o.Ready = func(k Key) { h.ready <- k }
	h.l = New(o)
	t.Cleanup(h.l.Close)
	return h
}

func (h *harness) waitReady(t *testing.T) Key {
	t.Helper()
	select {
	case k := <-h.ready:
		return k
	case <-time.After(2 * time.Second):
		t.Fatal("no Ready callback")
	}
	return Key{}
}

func TestLoadDecodesScalesAndCaches(t *testing.T) {
	disk, _ := cache.Open(t.TempDir(), 1<<20)
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(600, 300), nil }, Options{Disk: disk})
	k := Key{"al-1", 100}
	if _, ok := h.l.Get(k); ok {
		t.Fatal("first Get hit")
	}
	h.l.Get(k) // a second request while loading must not fetch twice
	if h.waitReady(t) != k {
		t.Fatal("wrong key ready")
	}
	img, ok := h.l.Get(k)
	if !ok || img.W != 100 || img.H != 50 {
		t.Fatalf("image = %v %v", img, ok)
	}
	if h.calls.Load() != 1 {
		t.Fatalf("fetched %d times", h.calls.Load())
	}
	// A fresh loader over the same disk cache doesn't hit the network.
	h2 := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("offline") }, Options{Disk: disk})
	h2.l.Get(k)
	h2.waitReady(t)
	if _, ok := h2.l.Get(k); !ok || h2.calls.Load() != 0 {
		t.Fatalf("disk cache not used (calls %d)", h2.calls.Load())
	}
}

func TestFailuresAreNotRetriedImmediately(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("404") }, Options{
		Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})
	k := Key{"al-x", 64}
	h.l.Get(k)
	h.waitReady(t)
	h.l.Get(k)
	time.Sleep(20 * time.Millisecond)
	if h.calls.Load() != 1 {
		t.Fatalf("retried immediately (%d calls)", h.calls.Load())
	}
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	h.l.Get(k)
	h.waitReady(t)
	if h.calls.Load() != 2 {
		t.Fatalf("not retried after RetryAfter (%d calls)", h.calls.Load())
	}
}

func TestMemoryBudgetEvictsLRU(t *testing.T) {
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(10, 10), nil }, Options{MemBytes: 900, Workers: 1})
	for _, id := range []subsonic.ID{"a", "b", "c"} { // 400 bytes each decoded
		h.l.Get(Key{id, 10})
		h.waitReady(t)
	}
	if _, ok := h.l.Get(Key{"a", 10}); ok {
		t.Fatal("a should have been evicted")
	}
	if _, ok := h.l.Get(Key{"c", 10}); !ok {
		t.Fatal("c missing")
	}
}

func TestEmptyIDNeverLoads(t *testing.T) {
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(1, 1), nil }, Options{})
	h.l.Get(Key{"", 100})
	time.Sleep(20 * time.Millisecond)
	if h.calls.Load() != 0 {
		t.Fatal("fetched an empty cover id")
	}
}

func TestHTTPFetcherRejectsNonImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "ok" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes(2, 2))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "u", Password: "secret-pw"}})
	f := HTTPFetcher(c)
	if b, err := f(context.Background(), Key{"ok", 2}); err != nil || len(b) == 0 {
		t.Fatalf("image fetch: %v", err)
	}
	_, err := f(context.Background(), Key{"missing", 2})
	if err == nil {
		t.Fatal("JSON error document accepted as an image")
	}
	if bytes.Contains([]byte(err.Error()), []byte("t=")) {
		t.Fatalf("error leaks the URL: %v", err)
	}
}
