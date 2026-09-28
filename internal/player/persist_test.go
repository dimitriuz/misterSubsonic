package player

import (
	"strings"
	"testing"

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
