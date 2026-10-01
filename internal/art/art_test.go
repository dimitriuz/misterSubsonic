package art

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFailedKeysAreBounded(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("404") }, Options{
		Workers: 1,
		Now:     func() time.Time { mu.Lock(); defer mu.Unlock(); n := now; now = now.Add(time.Millisecond); return n },
	})
	for i := 0; i < maxFailed+100; i++ {
		h.l.Get(Key{subsonic.ID(fmt.Sprintf("al-%d", i)), 64})
		h.waitReady(t)
	}
	h.l.mu.Lock()
	n := len(h.l.failed)
	_, newest := h.l.failed[Key{subsonic.ID(fmt.Sprintf("al-%d", maxFailed+99)), 64}]
	_, oldest := h.l.failed[Key{"al-0", 64}]
	h.l.mu.Unlock()
	if n > maxFailed {
		t.Fatalf("%d failed keys kept, want at most %d", n, maxFailed)
	}
	if !newest || oldest {
		t.Fatalf("newest kept %v, oldest kept %v; want the newest kept and the oldest forgotten", newest, oldest)
	}
}

// Past maxFailed, the keys whose RetryAfter has passed go first, all of
// them, before any still-blocked key is dropped for being the oldest.
func TestFailedKeysDropExpiredFirst(t *testing.T) {
	now := time.Unix(100000, 0)
	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("404") }, Options{
		Workers:    1,
		RetryAfter: time.Minute,
		Now:        func() time.Time { return now },
	})
	l := h.l
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := 0; i < maxFailed/2; i++ { // expired, though not the oldest of all below
		l.failed[Key{subsonic.ID(fmt.Sprintf("old-%d", i)), 64}] = now.Add(-time.Hour)
	}
	for i := 0; i < maxFailed/2; i++ { // still blocked
		l.failed[Key{subsonic.ID(fmt.Sprintf("new-%d", i)), 64}] = now.Add(-time.Second)
	}
	l.noteFailedLocked(Key{"fresh", 64})
	if len(l.failed) != maxFailed/2+1 {
		t.Fatalf("%d failed keys kept, want %d (every expired one gone, every blocked one kept)", len(l.failed), maxFailed/2+1)
	}
	if _, ok := l.failed[Key{"new-0", 64}]; !ok {
		t.Fatal("a still-blocked key was dropped")
	}
	if _, ok := l.failed[Key{"old-0", 64}]; ok {
		t.Fatal("an expired key was kept")
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

func TestPendingIsBoundedNewestFirst(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	order := make(chan Key, 300)
	var once sync.Once
	l := New(Options{Workers: 1, Fetch: func(_ context.Context, k Key) ([]byte, error) {
		once.Do(func() { close(started); <-release })
		order <- k
		return nil, errors.New("skip")
	}})
	defer l.Close()
	l.Get(Key{"k0", 10})
	<-started
	for i := 1; i < 200; i++ {
		l.Get(Key{subsonic.ID(fmt.Sprintf("k%d", i)), 10})
	}
	l.mu.Lock()
	n := len(l.pending)
	l.mu.Unlock()
	if n > maxPending {
		t.Fatalf("pending = %d, want <= %d", n, maxPending)
	}
	close(release)
	<-order // k0
	select {
	case k := <-order:
		if k.ID != "k199" {
			t.Fatalf("next fetched %s, want k199", k.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing fetched")
	}
}

func TestGetOfQueuedKeyDoesNotAllocate(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	l := New(Options{Workers: 1, Fetch: func(_ context.Context, k Key) ([]byte, error) {
		once.Do(func() { close(started); <-release })
		return nil, errors.New("skip")
	}})
	defer l.Close()
	defer close(release)
	l.Get(Key{"busy", 10})
	<-started
	a, b := Key{"a", 10}, Key{"b", 10}
	l.Get(a)
	l.Get(b)
	if n := testing.AllocsPerRun(100, func() { l.Get(b) }); n != 0 {
		t.Fatalf("Get of top queued key allocs = %v", n)
	}
	if n := testing.AllocsPerRun(100, func() { l.Get(a); l.Get(b) }); n != 0 {
		t.Fatalf("Get moving a queued key allocs = %v", n)
	}
}

func TestCorruptDiskEntryIsRefetched(t *testing.T) {
	disk, _ := cache.Open(t.TempDir(), 1<<20)
	k := Key{"al-1", 20}
	disk.Put(k.String(), []byte("garbage"))
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(40, 40), nil }, Options{Disk: disk})
	h.l.Get(k)
	h.waitReady(t)
	if _, ok := h.l.Get(k); !ok {
		t.Fatal("image not loaded after refetch")
	}
	if h.calls.Load() != 1 {
		t.Fatalf("fetched %d times, want 1", h.calls.Load())
	}
	b, ok := disk.Get(k.String())
	if !ok || !bytes.Equal(b, pngBytes(40, 40)) {
		t.Fatal("disk entry not repaired")
	}
}

func TestHTTPFetcherErrorsDoNotLeakCredentials(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: "http://" + addr, Credentials: subsonic.Credentials{Username: "u", Password: "secret-pw"}})
	_, ferr := HTTPFetcher(c)(context.Background(), Key{"x", 2})
	if ferr == nil {
		t.Fatal("expected an error")
	}
	for _, bad := range []string{"secret-pw", "t=", "s="} {
		if strings.Contains(ferr.Error(), bad) {
			t.Fatalf("error leaks %q: %v", bad, ferr)
		}
	}
}
