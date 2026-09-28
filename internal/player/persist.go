package player

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mistersubsonic/internal/subsonic"
)

// Resume is a saved queue position.
type Resume struct {
	Songs    []subsonic.Song `json:"songs"`
	Index    int             `json:"index"`
	Position time.Duration   `json:"position"`
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveResume writes the local resume file.
func SaveResume(path string, r Resume) error { return writeJSONAtomic(path, r) }

// LoadResume reads the local resume file; (nil, nil) if absent.
func LoadResume(path string) (*Resume, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Resume
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if len(r.Songs) == 0 || r.Index < 0 || r.Index >= len(r.Songs) {
		return nil, nil
	}
	return &r, nil
}

// Scrobble is a submission waiting to be retried.
type Scrobble struct {
	ID subsonic.ID `json:"id"`
	At time.Time   `json:"at"`
}

// MaxQueuedScrobbles caps the retry queue; the oldest entries are dropped.
const MaxQueuedScrobbles = 500

// ScrobbleQueue persists failed scrobble submissions.
type ScrobbleQueue struct {
	path  string
	mu    sync.Mutex
	items []Scrobble
}

// LoadScrobbleQueue reads path; a missing or corrupt file gives an empty queue.
func LoadScrobbleQueue(path string) *ScrobbleQueue {
	q := &ScrobbleQueue{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &q.items)
	}
	return q
}

func (q *ScrobbleQueue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }

func (q *ScrobbleQueue) Add(s Scrobble) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, s)
	if len(q.items) > MaxQueuedScrobbles {
		q.items = q.items[len(q.items)-MaxQueuedScrobbles:]
	}
	q.saveLocked()
}

// Flush submits queued scrobbles in order, stopping at the first failure.
func (q *ScrobbleQueue) Flush(ctx context.Context, api API) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	sent := 0
	var err error
	for _, s := range q.items {
		if err = api.Scrobble(ctx, s.ID, s.At, true); err != nil {
			break
		}
		sent++
	}
	if sent > 0 {
		q.items = q.items[sent:]
		q.saveLocked()
	}
	return err
}

func (q *ScrobbleQueue) saveLocked() {
	if q.path != "" {
		writeJSONAtomic(q.path, q.items)
	}
}
