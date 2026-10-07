package player

import (
	"os"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

// writeCounter counts the local file writes by file name.
type writeCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func (w *writeCounter) hook(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.n == nil {
		w.n = map[string]int{}
	}
	w.n[pathBase(path)]++
}
func (w *writeCounter) get(name string) int { w.mu.Lock(); defer w.mu.Unlock(); return w.n[name] }

func pathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func counted(t *testing.T) (*harness, *writeCounter) {
	h := newHarness(t, nil)
	w := &writeCounter{}
	h.p.do(func() { h.p.writeHook = w.hook })
	return h, w
}

// runFor ticks the player along for d of the fake clock, advancing the
// position as a playing song would.
func runFor(h *harness, id uint64, from, d time.Duration) time.Duration {
	pos := from
	for el := time.Duration(0); el < d; el += 250 * time.Millisecond {
		pos += 250 * time.Millisecond
		h.tickAt(id, pos)
	}
	return pos
}

func TestLongQueueIsWrittenOnceWhilePlaying(t *testing.T) {
	h, w := counted(t)
	h.p.PlayNow(songs(600, 3000), 5)
	a := h.playAndStart(1)
	runFor(h, a.ID, 0, 2*time.Minute)
	h.settle()
	if got := w.get("state.json"); got != 1 {
		t.Fatalf("queue file written %d times in 2 minutes, want 1", got)
	}
	// Four or five saves, and one more write when the first server save's
	// result turns the synced flag on.
	if got := w.get("position.json"); got < 5 || got > 6 {
		t.Fatalf("position file written %d times in 2 minutes, want 5 or 6", got)
	}
	r, err := LoadResume(h.p.o.ResumePath)
	if err != nil || r == nil || len(r.Songs) != 600 || r.Index != 5 || r.Position < 85*time.Second {
		t.Fatalf("resume: err %v, ok %v", err, r != nil)
	}
}

// savedAgain advances the clock and ticks until a save has happened.
func savedAgain(h *harness, id uint64) {
	h.clock.advance(31 * time.Second)
	// The song moves on with the clock, so the player is not stalled.
	h.tickAt(id, h.clock.now().Sub(time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)))
}

func TestEveryQueueChangeWritesTheQueueFileOnce(t *testing.T) {
	ops := []struct {
		name   string
		starts bool // the operation starts another song
		do     func(p *Player)
	}{
		{"enqueue", false, func(p *Player) { p.Enqueue(songs(2, 100)) }},
		{"play next", false, func(p *Player) { p.PlayNext(songs(2, 100)) }},
		{"remove", false, func(p *Player) { p.Remove(3) }},
		{"move", false, func(p *Player) { p.Move(4, 1) }},
		{"play now", true, func(p *Player) { p.PlayNow(songs(4, 100), 1) }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			h, w := counted(t)
			h.p.PlayNow(songs(6, 3000), 0)
			a := h.playAndStart(1)
			h.tickAt(a.ID, time.Second)
			h.settle()
			if w.get("state.json") != 1 {
				t.Fatalf("first save wrote the queue %d times", w.get("state.json"))
			}
			savedAgain(h, a.ID) // nothing changed: position only
			h.settle()
			// The position file: the first save, the synced flag, this save.
			if w.get("state.json") != 1 || w.get("position.json") != 3 {
				t.Fatalf("unchanged queue: %d queue writes, %d position writes", w.get("state.json"), w.get("position.json"))
			}
			op.do(h.p)
			if op.starts {
				h.playAndStart(2)
			}
			savedAgain(h, h.eng.lastPlayed().ID)
			h.settle()
			if got := w.get("state.json"); got != 2 {
				t.Fatalf("after %s the queue file was written %d times in all, want 2", op.name, got)
			}
			savedAgain(h, h.eng.lastPlayed().ID)
			h.settle()
			if got := w.get("state.json"); got != 2 {
				t.Fatalf("a second save rewrote the queue (%d)", got)
			}
		})
	}
}

func TestClearWritesAnEmptyQueueOnExit(t *testing.T) {
	h, w := counted(t)
	h.p.PlayNow(songs(3, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle() // else the empty queue's write drops the first one, which is right but not counted
	h.p.Clear()
	h.cancel()
	<-h.done
	if w.get("state.json") != 2 {
		t.Fatalf("queue file written %d times, want 2", w.get("state.json"))
	}
	if r, err := LoadResume(h.p.o.ResumePath); r != nil || err != nil {
		t.Fatalf("a cleared queue resumes as %+v, %v", r, err)
	}
}

func TestExitWritesWhatChanged(t *testing.T) {
	h, w := counted(t)
	h.p.PlayNow(songs(3, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second) // first save: both files
	h.settle()
	h.p.Enqueue(songs(1, 100))
	h.cancel()
	<-h.done
	if w.get("state.json") != 2 {
		t.Fatalf("exit wrote the queue %d times in all, want 2", w.get("state.json"))
	}
	if r, _ := LoadResume(h.p.o.ResumePath); r == nil || len(r.Songs) != 4 {
		t.Fatalf("resume = %+v", r)
	}
}

func TestPositionFileAppliesOnlyToItsSong(t *testing.T) {
	path := t.TempDir() + "/state.json"
	q := songs(5, 100)
	if err := SaveResume(path, Resume{Songs: q, Index: 1, Position: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if r, err := LoadResume(path); err != nil || r.Index != 1 || r.Position != 5*time.Second {
		t.Fatalf("queue file alone: %+v, %v", r, err)
	}
	if err := SavePosition(path, SavedPosition{ID: q[3].ID, Index: 3, Position: 40 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if r, _ := LoadResume(path); r.Index != 3 || r.Position != 40*time.Second || len(r.Songs) != 5 {
		t.Fatalf("matching position file: %+v", r)
	}
	SavePosition(path, SavedPosition{ID: "zz", Index: 3, Position: 40 * time.Second})
	if r, _ := LoadResume(path); r.Index != 1 || r.Position != 5*time.Second {
		t.Fatalf("another song: the position file applied: %+v", r)
	}
	os.WriteFile(positionPath(path), []byte("{garbage"), 0o600)
	if r, err := LoadResume(path); err != nil || r.Index != 1 {
		t.Fatalf("garbage position file: %+v, %v", r, err)
	}
}

func TestAnOldStateFileStillLoadsAndIsUpgraded(t *testing.T) {
	h := newHarness(t, nil)
	old := `{"songs":[{"id":"sa"},{"id":"sb"},{"id":"sc"}],"index":2,"position":7000000000}`
	if err := os.WriteFile(h.p.o.ResumePath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := h.p.Resumable(t.Context())
	if err != nil || r == nil || r.Index != 2 || r.Position != 7*time.Second || len(r.Songs) != 3 {
		t.Fatalf("old file: %+v, %v", r, err)
	}
	h.p.ResumeFrom(r)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 9*time.Second)
	if _, err := os.Stat(positionPath(h.p.o.ResumePath)); err != nil {
		t.Fatalf("the next save did not write the new layout: %v", err)
	}
}

func ids(ss []subsonic.Song, from, to int) []subsonic.ID {
	var out []subsonic.ID
	for _, s := range ss[from:to] {
		out = append(out, s.ID)
	}
	return out
}

func lastSave(h *harness) ([]subsonic.ID, subsonic.ID, time.Duration) {
	h.t.Helper()
	h.waitFor("server save", func() bool { h.api.mu.Lock(); defer h.api.mu.Unlock(); return len(h.api.saves) > 0 })
	h.api.mu.Lock()
	defer h.api.mu.Unlock()
	return h.api.saves[len(h.api.saves)-1], h.api.saveCur, h.api.savePos
}

func TestServerSaveIsAWindowOf500(t *testing.T) {
	q := songs(1200, 3000)
	for _, tc := range []struct {
		name      string
		start     int
		from, to  int // the expected window, in play-order positions
		curInside int
	}{
		{"middle", 600, 550, 1050, 50},
		{"near the start", 10, 0, 500, 10},
		{"near the end", 1190, 1140, 1200, 50},
		{"first", 0, 0, 500, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.p.PlayNow(q, tc.start)
			a := h.playAndStart(1)
			h.tickAt(a.ID, 3*time.Second)
			got, cur, pos := lastSave(h)
			want := ids(q, tc.from, tc.to)
			if len(got) != len(want) || got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
				t.Fatalf("window = %d ids %v..%v, want %d ids %v..%v", len(got), got[0], got[len(got)-1], len(want), want[0], want[len(want)-1])
			}
			if cur != q[tc.start].ID || got[tc.curInside] != cur || pos != 3*time.Second {
				t.Fatalf("current %v at %d (want %d), pos %v", cur, indexOf(got, cur), tc.curInside, pos)
			}
		})
	}
}

func indexOf(ids []subsonic.ID, id subsonic.ID) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return -1
}

func TestServerWindowFollowsTheShuffleOrder(t *testing.T) {
	h := newHarness(t, nil)
	h.p.SetShuffle(true)
	q := songs(900, 3000)
	h.p.PlayNow(q, 100)
	a := h.playAndStart(1)
	// Walk into the shuffled order a little, so there is a "before".
	for i := 0; i < 60; i++ {
		h.p.Next()
		a = h.playAndStart(i + 2)
	}
	var want []subsonic.ID
	var cur subsonic.ID
	h.p.do(func() {
		start := max(0, h.p.cursor-50)
		for _, qi := range h.p.order[start:min(len(h.p.order), start+500)] {
			want = append(want, q[qi].ID)
		}
		cur = q[h.p.currentIndex()].ID
	})
	h.api.mu.Lock()
	h.api.saves = nil
	h.api.mu.Unlock()
	h.clock.advance(31 * time.Second)
	h.tickAt(a.ID, time.Second)
	got, gcur, _ := lastSave(h)
	if len(got) != 500 || gcur != cur || got[50] != cur {
		t.Fatalf("window %d ids, current %v at %d, want %v at 50", len(got), gcur, indexOf(got, gcur), cur)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("window[%d] = %v, want %v (not in play order)", i, got[i], want[i])
		}
	}
	if got[0] == q[0].ID && got[1] == q[1].ID && got[2] == q[2].ID {
		t.Fatal("the window is in queue order")
	}
}

func TestResumePrefersTheMatchingLocalQueue(t *testing.T) {
	q := songs(40, 100)
	t.Run("local and server match: the local full queue", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		h.api.queue = &subsonic.PlayQueue{Songs: q[:3], Current: q[7].ID, Position: 20000}
		r, err := h.p.Resumable(t.Context())
		if err != nil || len(r.Songs) != 40 || r.Index != 7 || r.Position != 20*time.Second {
			t.Fatalf("resumable = %+v, %v", r, err)
		}
	})
	t.Run("they differ and the local one is synced: the server's", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		SavePosition(h.p.o.ResumePath, SavedPosition{ID: q[7].ID, Index: 7, Position: 20 * time.Second, Synced: true})
		other := []subsonic.Song{{ID: "x1"}, {ID: "x2"}, {ID: "x3"}, {ID: "x4"}, {ID: "x5"}}
		h.api.queue = &subsonic.PlayQueue{Songs: other, Current: "x5", Position: 3000}
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 5 || r.Index != 4 || r.Position != 3*time.Second {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("no server queue: the local one", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 40 || r.Index != 7 {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("no local file: the server's", func(t *testing.T) {
		h := newHarness(t, nil)
		h.api.queue = &subsonic.PlayQueue{Songs: q[:5], Current: q[4].ID, Position: 3000}
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 5 || r.Index != 4 {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("same song, the server much later: the server's position", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		h.api.queue = &subsonic.PlayQueue{Songs: q[:3], Current: q[7].ID, Position: 90000}
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 40 || r.Index != 7 || r.Position != 90*time.Second {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("same song, the server a little later: the local position", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		h.api.queue = &subsonic.PlayQueue{Songs: q[:3], Current: q[7].ID, Position: 30000}
		r, _ := h.p.Resumable(t.Context())
		if r.Position != 20*time.Second {
			t.Fatalf("resumable = %+v", r)
		}
	})
}

// A crash between the big write and the small one can leave a queue file
// from an older order: the position file's song is found by its id.
func TestThePositionFileFindsItsSongInAnOlderQueueFile(t *testing.T) {
	path := t.TempDir() + "/state.json"
	q := songs(5, 100)
	if err := SaveResume(path, Resume{Songs: q, Index: 1, Position: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	for name, idx := range map[string]int{"another index": 0, "out of range": 9} {
		SavePosition(path, SavedPosition{ID: q[3].ID, Index: idx, Position: 40 * time.Second, Synced: true})
		r, _ := LoadResume(path)
		if r.Index != 3 || r.Position != 40*time.Second || r.Synced {
			t.Fatalf("%s: %+v, want song 3 at 40 s, not synced", name, r)
		}
	}
}
