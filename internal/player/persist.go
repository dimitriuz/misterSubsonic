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

	// Synced is set by LoadResume from the position file: the server's saved
	// queue was last brought up to this very state by a save that succeeded.
	// local marks a Resume read from the local files by Player.Resumable;
	// loads is the player's count of queue-file writes at that time.
	Synced bool `json:"-"`
	local  bool
	loads  uint64
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, b)
}

// writeBytesAtomic writes b to path through a temporary file and a rename.
func writeBytesAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SavedPosition is the small file written every few seconds, next to the
// (large) queue file: where in the queue the listener is.
type SavedPosition struct {
	ID       subsonic.ID   `json:"id"`
	Index    int           `json:"index"`
	Position time.Duration `json:"position"`
	// Synced: the last save of this queue to the server succeeded, so the
	// server's copy is this device's own, not another device's.
	Synced bool `json:"synced"`
}

// positionPath is the position file that goes with the queue file at path.
func positionPath(path string) string { return filepath.Join(filepath.Dir(path), "position.json") }

// SaveResume writes the local queue file (the songs, and the index and
// position at the time of writing).
func SaveResume(path string, r Resume) error { return writeJSONAtomic(path, r) }

// SavePosition writes the small position file that goes with the queue file
// at path.
func SavePosition(path string, p SavedPosition) error { return writeJSONAtomic(positionPath(path), p) }

// LoadResume reads the local resume files; (nil, nil) if absent. The queue
// file gives the songs; the position file's index and position replace the
// queue file's when its song is the one at that index. A queue file written
// by an older version holds the position itself and loads as it is.
func LoadResume(path string) (*Resume, error) {
	r, err := loadQueueFile(path)
	if r == nil || err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(positionPath(path)); err == nil {
		var sp SavedPosition
		if json.Unmarshal(b, &sp) == nil && sp.Position >= 0 {
			if sp.Index >= 0 && sp.Index < len(r.Songs) && r.Songs[sp.Index].ID == sp.ID {
				r.Index, r.Position, r.Synced = sp.Index, sp.Position, sp.Synced
			} else if i := indexOfSong(r.Songs, sp.ID); i >= 0 {
				// The queue file is older than the position file (a crash
				// between the two writes): the song moved. Synced stays
				// false, as the queue it describes is not this one.
				r.Index, r.Position = i, sp.Position
			}
		}
	}
	return r, nil
}

func indexOfSong(songs []subsonic.Song, id subsonic.ID) int {
	for i := range songs {
		if songs[i].ID == id {
			return i
		}
	}
	return -1
}

// nearestSong is the index of the song with this id that is closest to near
// (the earlier one on a tie), or -1.
func nearestSong(songs []subsonic.Song, id subsonic.ID, near int) int {
	best := -1
	for i := range songs {
		if songs[i].ID == id && (best < 0 || abs(i-near) < abs(best-near)) {
			best = i
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func loadQueueFile(path string) (*Resume, error) {
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
	ID  subsonic.ID `json:"id"`
	At  time.Time   `json:"at"`
	Seq uint64      `json:"seq"`
}

// MaxQueuedScrobbles caps the retry queue; the oldest entries are dropped.
const MaxQueuedScrobbles = 500

// ScrobbleQueue persists failed scrobble submissions.
type ScrobbleQueue struct {
	path     string
	mu       sync.Mutex
	items    []Scrobble
	flushing bool
	nextSeq  uint64
}

// LoadScrobbleQueue reads path; a missing or corrupt file gives an empty queue.
func LoadScrobbleQueue(path string) *ScrobbleQueue {
	q := &ScrobbleQueue{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &q.items)
	}
	// Renumber loaded items with fresh unique sequential values to prevent data loss
	// from seq collisions (legacy files without seq, hand-edits, duplicates all have Seq==0)
	for i := range q.items {
		q.items[i].Seq = uint64(i + 1)
	}
	q.nextSeq = uint64(len(q.items) + 1)
	return q
}

func (q *ScrobbleQueue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }

func (q *ScrobbleQueue) Add(s Scrobble) {
	q.mu.Lock()
	defer q.mu.Unlock()
	s.Seq = q.nextSeq
	q.nextSeq++
	q.items = append(q.items, s)
	if len(q.items) > MaxQueuedScrobbles {
		q.items = q.items[len(q.items)-MaxQueuedScrobbles:]
	}
	q.saveLocked()
}

// Flush submits queued scrobbles in order, stopping at the first failure.
func (q *ScrobbleQueue) Flush(ctx context.Context, api API) error {
	q.mu.Lock()
	if q.flushing {
		q.mu.Unlock()
		return nil
	}
	q.flushing = true
	snapshot := append([]Scrobble(nil), q.items...)
	q.mu.Unlock()

	// Send the snapshot without holding the lock
	sent := 0
	var err error
	var lastSentSeq uint64
	for _, s := range snapshot {
		if err = api.Scrobble(ctx, s.ID, s.At, true); err != nil {
			break
		}
		sent++
		lastSentSeq = s.Seq
	}

	// Re-lock to update state
	q.mu.Lock()
	defer func() {
		q.flushing = false
		q.mu.Unlock()
	}()

	if sent > 0 {
		// Remove items whose Seq <= lastSentSeq
		newItems := []Scrobble{}
		for _, s := range q.items {
			if s.Seq > lastSentSeq {
				newItems = append(newItems, s)
			}
		}
		q.items = newItems
		q.saveLocked()
	}
	return err
}

func (q *ScrobbleQueue) saveLocked() {
	if q.path != "" {
		writeJSONAtomic(q.path, q.items)
	}
}
