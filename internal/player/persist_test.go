package player

import (
	"context"
	"os"
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
	releaseCh   chan struct{}
	callArrived chan subsonic.ID // signals when a Scrobble call arrives
	scrobbled   []subsonic.ID
	failOn      subsonic.ID // if set, Scrobble returns error for this ID
	mu          sync.Mutex
}

func (a *testBlockingAPI) Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error {
	// Signal arrival before blocking
	if a.callArrived != nil {
		select {
		case a.callArrived <- id:
		default:
		}
	}
	<-a.releaseCh
	a.mu.Lock()
	a.scrobbled = append(a.scrobbled, id)
	a.mu.Unlock()
	if id == a.failOn {
		return context.DeadlineExceeded
	}
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
	api := &testBlockingAPI{
		releaseCh:   make(chan struct{}),
		callArrived: make(chan subsonic.ID, 10),
	}
	q := LoadScrobbleQueue("")
	q.Add(Scrobble{ID: "x"})
	q.Add(Scrobble{ID: "y"})
	q.Add(Scrobble{ID: "z"})

	// Start Flush #1 in a goroutine
	done1 := make(chan error)
	go func() {
		done1 <- q.Flush(context.Background(), api)
	}()

	// Wait until Flush #1's first Scrobble call arrives
	select {
	case <-api.callArrived:
	case <-time.After(1 * time.Second):
		t.Fatal("Flush #1 never called Scrobble")
	}

	// Now call Flush #2 synchronously and require it returns quickly (short-circuit)
	done2 := make(chan error)
	go func() {
		done2 <- q.Flush(context.Background(), api)
	}()

	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("Flush #2 returned error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Flush #2 did not short-circuit (still waiting after 200ms)")
	}

	// Release the blocking API to let Flush #1 complete
	close(api.releaseCh)
	if err := <-done1; err != nil {
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

func TestLegacyQueueFileWithoutSeqKeepsUnsent(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/legacy.json"

	// Write a legacy file without seq field (all items will have Seq==0 after JSON unmarshal)
	legacyJSON := `[{"id":"a","at":"2026-01-01T00:00:00Z"},{"id":"b","at":"2026-01-01T00:00:00Z"},{"id":"c","at":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filePath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// Load the legacy file (LoadScrobbleQueue should renumber items with fresh Seq values)
	q := LoadScrobbleQueue(filePath)
	if q.Len() != 3 {
		t.Fatalf("loaded queue len = %d, want 3", q.Len())
	}

	// Create API that fails on "b"
	api := &testBlockingAPI{
		releaseCh: make(chan struct{}),
		failOn:    "b",
	}
	close(api.releaseCh) // no blocking for this test

	// Flush should attempt a, b (fail), then stop
	err := q.Flush(context.Background(), api)
	if err == nil {
		t.Fatal("expected Flush to return error from failOn b")
	}

	// Verify API received exactly [a, b-attempt]
	if len(api.scrobbled) != 2 {
		t.Fatalf("API got %d calls, want 2", len(api.scrobbled))
	}
	if api.scrobbled[0] != "a" || api.scrobbled[1] != "b" {
		t.Fatalf("API calls = %v, want [a b]", api.scrobbled)
	}

	// Queue should now have 2 items (b and c, with fresh Seq > lastSentSeq)
	if q.Len() != 2 {
		t.Fatalf("queue len after failed flush = %d, want 2", q.Len())
	}

	// Reload from disk and verify the unsent items persisted
	q2 := LoadScrobbleQueue(filePath)
	if q2.Len() != 2 {
		t.Fatalf("reloaded queue len = %d, want 2", q2.Len())
	}
}
