package player

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	if r, err := LoadResume(dir + "/missing.json"); r != nil || err != nil {
		t.Fatalf("missing resume = %v, %v", r, err)
	}
	q := LoadScrobbleQueue(dir + "/scrobbles.json")
	for i := 0; i < MaxQueuedScrobbles+10; i++ {
		q.Add(Scrobble{ID: subsonic.ID(strings.Repeat("x", 1+i%3))})
	}
	if q.Len() != MaxQueuedScrobbles {
		t.Fatalf("queue len = %d, want cap %d", q.Len(), MaxQueuedScrobbles)
	}
	if again := LoadScrobbleQueue(dir + "/scrobbles.json"); again.Len() != MaxQueuedScrobbles {
		t.Fatalf("reloaded len = %d", again.Len())
	}
	writeJSONAtomic(dir+"/bad.json", "not a list")
	if bad := LoadScrobbleQueue(dir + "/bad.json"); bad.Len() != 0 {
		t.Fatal("corrupt queue file should load empty")
	}
}

// testBlockingAPI implements API and blocks Scrobble until released.
type testBlockingAPI struct {
	releaseCh chan struct{}
	scrobbled []subsonic.ID
	mu        sync.Mutex
}

func (a *testBlockingAPI) Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error {
	<-a.releaseCh
	a.mu.Lock()
	a.scrobbled = append(a.scrobbled, id)
	a.mu.Unlock()
	return nil
}

func (a *testBlockingAPI) SavePlayQueue(ctx context.Context, ids []subsonic.ID, current subsonic.ID, pos time.Duration) error {
	return nil
}

func (a *testBlockingAPI) GetPlayQueue(ctx context.Context) (*subsonic.PlayQueue, error) {
	return nil, nil
}

func TestFlushDoesNotBlockAdd(t *testing.T) {
	api := &testBlockingAPI{releaseCh: make(chan struct{})}
	q := LoadScrobbleQueue("")
	q.Add(Scrobble{ID: "a"})
	q.Add(Scrobble{ID: "b"})

	flushStarted := make(chan struct{})
	flushDone := make(chan error)
	go func() {
		close(flushStarted)
		flushDone <- q.Flush(context.Background(), api)
	}()

	<-flushStarted
	time.Sleep(10 * time.Millisecond) // let Flush start and block on first Scrobble

	// Add must not block despite Flush holding items
	addDone := make(chan struct{})
	go func() {
		q.Add(Scrobble{ID: "c"})
		close(addDone)
	}()

	select {
	case <-addDone:
		// good, Add returned quickly
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Add blocked while Flush was in progress")
	}

	close(api.releaseCh)
	if err := <-flushDone; err != nil {
		t.Fatal(err)
	}

	if q.Len() != 1 {
		t.Fatalf("queue len = %d, want 1", q.Len())
	}
	if len(api.scrobbled) != 2 {
		t.Fatalf("API got %d calls, want 2", len(api.scrobbled))
	}
	if api.scrobbled[0] != "a" || api.scrobbled[1] != "b" {
		t.Fatalf("API calls in wrong order: %v", api.scrobbled)
	}
}

func TestConcurrentFlushSendsOnce(t *testing.T) {
	api := &testBlockingAPI{releaseCh: make(chan struct{})}
	q := LoadScrobbleQueue("")
	q.Add(Scrobble{ID: "x"})
	q.Add(Scrobble{ID: "y"})
	q.Add(Scrobble{ID: "z"})

	done := make(chan error, 2)
	go func() { done <- q.Flush(context.Background(), api) }()
	go func() { done <- q.Flush(context.Background(), api) }()

	close(api.releaseCh) // release any Scrobble calls
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if q.Len() != 0 {
		t.Fatalf("queue len = %d, want 0", q.Len())
	}
	if len(api.scrobbled) != 3 {
		t.Fatalf("API got %d calls, want 3", len(api.scrobbled))
	}
	// Verify each ID was sent exactly once
	counts := make(map[subsonic.ID]int)
	for _, id := range api.scrobbled {
		counts[id]++
	}
	for id, count := range counts {
		if count != 1 {
			t.Fatalf("ID %q sent %d times, want 1", id, count)
		}
	}
}
