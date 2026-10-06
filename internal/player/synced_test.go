package player

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

func readPosition(t *testing.T, h *harness) SavedPosition {
	t.Helper()
	b, err := os.ReadFile(positionPath(h.p.o.ResumePath))
	if err != nil {
		t.Fatal(err)
	}
	var sp SavedPosition
	if err := json.Unmarshal(b, &sp); err != nil {
		t.Fatal(err)
	}
	return sp
}

// The user's server saves failed for weeks: the server holds an old queue.
// A local queue that was not brought up to the server must win.
func TestAnUnsyncedLocalQueueBeatsADifferingServerQueue(t *testing.T) {
	q := songs(40, 100)
	old := &subsonic.PlayQueue{Songs: q[:5], Current: q[4].ID, Position: 3000}
	t.Run("an old install: a state file alone", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		h.api.queue = old
		r, err := h.p.Resumable(t.Context())
		if err != nil || len(r.Songs) != 40 || r.Index != 7 || r.Position != 20*time.Second {
			t.Fatalf("resumable = %+v, %v", r, err)
		}
	})
	t.Run("a position file without the field", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
		os.WriteFile(positionPath(h.p.o.ResumePath), []byte(`{"id":"`+string(q[8].ID)+`","index":8,"position":9000000000}`), 0o600)
		h.api.queue = old
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 40 || r.Index != 8 || r.Synced {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("a position file that says not synced", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7})
		SavePosition(h.p.o.ResumePath, SavedPosition{ID: q[7].ID, Index: 7, Synced: false})
		h.api.queue = old
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 40 || r.Index != 7 {
			t.Fatalf("resumable = %+v", r)
		}
	})
	t.Run("a synced position file for another song is not synced", func(t *testing.T) {
		h := newHarness(t, nil)
		SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7})
		SavePosition(h.p.o.ResumePath, SavedPosition{ID: "zz", Index: 7, Synced: true})
		h.api.queue = old
		r, _ := h.p.Resumable(t.Context())
		if len(r.Songs) != 40 {
			t.Fatalf("resumable = %+v", r)
		}
	})
}

func TestASyncedLocalQueueYieldsToADifferingServerQueue(t *testing.T) {
	q := songs(40, 100)
	h := newHarness(t, nil)
	SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
	SavePosition(h.p.o.ResumePath, SavedPosition{ID: q[7].ID, Index: 7, Position: 20 * time.Second, Synced: true})
	other := []subsonic.Song{{ID: "x1"}, {ID: "x2"}, {ID: "x3"}}
	h.api.queue = &subsonic.PlayQueue{Songs: other, Current: "x2", Position: 3000}
	r, _ := h.p.Resumable(t.Context())
	if len(r.Songs) != 3 || r.Index != 1 || r.Position != 3*time.Second {
		t.Fatalf("resumable = %+v", r)
	}
}

// The server is on another song of the same queue (the user skipped on
// another device): the whole local queue resumes there, not the 500-song
// window.
func TestASyncedLocalQueueResumesAtTheServersSongWhenItIsInTheQueue(t *testing.T) {
	q := songs(40, 100)
	h := newHarness(t, nil)
	SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 7, Position: 20 * time.Second})
	SavePosition(h.p.o.ResumePath, SavedPosition{ID: q[7].ID, Index: 7, Position: 20 * time.Second, Synced: true})
	h.api.queue = &subsonic.PlayQueue{Songs: q[:5], Current: q[4].ID, Position: 3000}
	r, _ := h.p.Resumable(t.Context())
	if len(r.Songs) != 40 || r.Index != 4 || r.Position != 3*time.Second {
		t.Fatalf("resumable = %+v", r)
	}
}

func TestTheSyncedFlagFollowsTheRealSaveResults(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()
	if !readPosition(t, h).Synced {
		t.Fatal("a save that succeeded left the flag false")
	}
	if r, _ := LoadResume(h.p.o.ResumePath); !r.Synced {
		t.Fatal("LoadResume lost the flag")
	}

	h.api.mu.Lock()
	h.api.failSaves = true
	h.api.mu.Unlock()
	savedAgain(h, a.ID)
	h.settle()
	if readPosition(t, h).Synced {
		t.Fatal("a save that failed left the flag true")
	}

	h.api.mu.Lock()
	h.api.failSaves = false
	h.api.mu.Unlock()
	savedAgain(h, a.ID)
	h.settle()
	if !readPosition(t, h).Synced {
		t.Fatal("a save that succeeded again left the flag false")
	}
}

// Until the server has the new queue (or the new song), the record on disk
// must not claim that it does.
func TestTheFlagIsFalseWhileTheServerIsBehind(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()

	gate := make(chan struct{})
	h.api.mu.Lock()
	h.api.blockSave = gate
	h.api.mu.Unlock()
	h.p.Enqueue(songs(2, 100))
	savedAgain(h, a.ID) // the queue file changes; the server has not heard yet
	if readPosition(t, h).Synced {
		t.Fatal("the flag claims sync with a queue the server has not got")
	}
	close(gate)
	h.settle()
	if !readPosition(t, h).Synced {
		t.Fatal("the flag stayed false after the server took the queue")
	}

	// A new song: the same.
	gate = make(chan struct{})
	h.api.mu.Lock()
	h.api.blockSave = gate
	h.api.mu.Unlock()
	h.p.Next()
	b := h.playAndStart(2)
	savedAgain(h, b.ID)
	if sp := readPosition(t, h); sp.Synced || sp.ID != "sb" {
		t.Fatalf("a new song with the server behind: %+v", sp)
	}
	close(gate)
	h.settle()
	if !readPosition(t, h).Synced {
		t.Fatal("the flag stayed false after the server took the song")
	}
}

// Natural auto-advance across a song boundary changes no queue: the big
// file is not rewritten.
func TestNaturalAdvanceNeverWritesTheQueueFile(t *testing.T) {
	h, w := counted(t)
	h.p.PlayNow(songs(4, 20), 0) // short songs
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()
	if w.get("state.json") != 1 {
		t.Fatalf("first save wrote the queue %d times", w.get("state.json"))
	}
	cur := a
	for i := 1; i < 4; i++ {
		h.tickAt(cur.ID, 5*time.Second) // inside the prefetch window of a 20 s song
		h.waitFor("QueueNext", func() bool { return h.eng.queueCount() == i })
		next := h.eng.queued[i-1]
		h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: cur.ID}
		h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: next.ID}
		h.waitFor("handover", func() bool { return h.p.State().Index == i })
		cur = next
		h.waitFor("playing", func() bool { return h.p.State().Status == Playing })
		savedAgain(h, cur.ID)
		h.settle()
	}
	if got := w.get("state.json"); got != 1 {
		t.Fatalf("the queue file was written %d times across three song changes, want 1", got)
	}
	if sp := readPosition(t, h); sp.Index != 3 {
		t.Fatalf("the position file is at index %d, want 3", sp.Index)
	}
}

func TestACorruptLocalStateIsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	h := newHarness(t, nil)
	os.WriteFile(h.p.o.ResumePath, []byte("{not json"), 0o600)
	q := songs(5, 100)
	h.api.queue = &subsonic.PlayQueue{Songs: q, Current: q[2].ID}
	for i := 0; i < 3; i++ {
		r, err := h.p.Resumable(t.Context())
		if err != nil || r == nil || r.Index != 2 {
			t.Fatalf("resumable = %+v, %v; want the server's", r, err)
		}
	}
	// And without a server queue the error comes back too, still logged once.
	h.api.queue = nil
	if r, err := h.p.Resumable(t.Context()); err == nil || r != nil {
		t.Fatalf("resumable = %+v, %v", r, err)
	}
	if n := strings.Count(buf.String(), "local queue file"); n != 1 {
		t.Fatalf("the corrupt file was logged %d times: %q", n, buf.String())
	}
}

// A Clear or an edit after the queue ended (Stopped) is saved on the next
// interval, not only on exit.
func TestChangesWhileStoppedAreSaved(t *testing.T) {
	end := func(h *harness) {
		h.p.PlayNow(songs(2, 20), 1)
		a := h.playAndStart(1)
		h.tickAt(a.ID, time.Second)
		h.settle()
		h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
		h.waitFor("stopped", func() bool { return h.p.State().Status == Stopped })
	}
	t.Run("clear", func(t *testing.T) {
		h := newHarness(t, nil)
		end(h)
		h.p.Clear()
		h.idleTick()
		h.settle()
		if r, err := LoadResume(h.p.o.ResumePath); r != nil || err != nil {
			t.Fatalf("a cleared queue resumes as %+v, %v", r, err)
		}
	})
	t.Run("enqueue", func(t *testing.T) {
		h := newHarness(t, nil)
		end(h)
		h.p.Enqueue(songs(3, 100))
		h.idleTick()
		h.settle()
		if r, _ := LoadResume(h.p.o.ResumePath); r == nil || len(r.Songs) != 5 {
			t.Fatalf("resume = %+v", r)
		}
	})
	t.Run("a stopped player does not save to the server", func(t *testing.T) {
		h := newHarness(t, nil)
		end(h)
		h.api.mu.Lock()
		n := len(h.api.saves)
		h.api.mu.Unlock()
		h.p.Enqueue(songs(3, 100))
		h.idleTick()
		h.settle()
		h.api.mu.Lock()
		defer h.api.mu.Unlock()
		if len(h.api.saves) != n {
			t.Fatal("a stopped player saved a position to the server")
		}
	})
}

func TestResumingFromTheUnchangedLocalFileDoesNotRewriteIt(t *testing.T) {
	h, w := counted(t)
	q := songs(30, 3000)
	SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 4, Position: 9 * time.Second})
	r, err := h.p.Resumable(t.Context())
	if err != nil || r == nil {
		t.Fatal(err)
	}
	h.p.ResumeFrom(r)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 10*time.Second)
	savedAgain(h, a.ID)
	h.settle()
	if got := w.get("state.json"); got != 0 {
		t.Fatalf("the queue file was rewritten %d times after an unchanged resume", got)
	}
	if got := w.get("position.json"); got < 1 {
		t.Fatal("no position file written")
	}
}

// If the file changed between Resumable and ResumeFrom (the card was left
// up while the user played something else), the resumed queue is written.
func TestResumingAStaleCardRewritesTheQueueFile(t *testing.T) {
	h, w := counted(t)
	SaveResume(h.p.o.ResumePath, Resume{Songs: songs(30, 3000), Index: 4})
	r, _ := h.p.Resumable(t.Context())
	h.p.PlayNow(songs(3, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()
	if w.get("state.json") != 1 {
		t.Fatalf("state writes = %d", w.get("state.json"))
	}
	h.p.ResumeFrom(r)
	b := h.playAndStart(2)
	savedAgain(h, b.ID)
	h.settle()
	if got := w.get("state.json"); got != 2 {
		t.Fatalf("state writes = %d, want 2", got)
	}
	if got, _ := LoadResume(h.p.o.ResumePath); len(got.Songs) != 30 {
		t.Fatalf("file holds %d songs", len(got.Songs))
	}
}

// A jump is a position, not a queue change: the position file holds the
// index, and the load applies it by id and index.
func TestAJumpDoesNotRewriteTheQueueFile(t *testing.T) {
	h, w := counted(t)
	q := songs(8, 3000)
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()
	h.p.Jump(5)
	b := h.playAndStart(2)
	h.tickAt(b.ID, 12*time.Second)
	savedAgain(h, b.ID)
	h.settle()
	if got := w.get("state.json"); got != 1 {
		t.Fatalf("the queue file was written %d times, want 1", got)
	}
	r, err := LoadResume(h.p.o.ResumePath)
	if err != nil || r.Index != 5 || r.Position < 12*time.Second || len(r.Songs) != 8 {
		t.Fatalf("after a jump the resume is %+v, %v", r, err)
	}
}

func TestAPausedPlayerWritesNothingIdentical(t *testing.T) {
	h, w := counted(t)
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 5*time.Second)
	h.settle()
	h.p.TogglePause()
	h.waitFor("paused", func() bool { return h.p.State().Status == Paused })
	h.idleTick() // paused: the position is what it was
	h.settle()
	posWrites := w.get("position.json")
	h.api.mu.Lock()
	srvSaves := len(h.api.saves)
	h.api.mu.Unlock()
	for i := 0; i < 6; i++ {
		h.idleTick()
		h.settle()
	}
	h.api.mu.Lock()
	n := len(h.api.saves)
	h.api.mu.Unlock()
	if got := w.get("position.json"); got != posWrites {
		t.Fatalf("position file rewritten %d times while paused and unchanged", got-posWrites)
	}
	if n != srvSaves {
		t.Fatalf("server saved %d times while paused and unchanged", n-srvSaves)
	}
	// Moving on writes again.
	h.p.TogglePause()
	h.waitFor("playing", func() bool { return h.p.State().Status == Playing })
	h.tickAt(a.ID, 20*time.Second)
	savedAgain(h, a.ID)
	h.settle()
	if w.get("position.json") == posWrites {
		t.Fatal("a moved position was not written")
	}
}

// A failed server save is retried even when nothing changed.
func TestAFailedServerSaveIsRetriedWhilePaused(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 5*time.Second)
	h.settle()
	h.p.TogglePause()
	h.waitFor("paused", func() bool { return h.p.State().Status == Paused })
	h.api.mu.Lock()
	h.api.failSaves = true
	h.api.mu.Unlock()
	savedAgain(h, a.ID)
	h.settle()
	h.api.mu.Lock()
	h.api.failSaves = false
	h.api.mu.Unlock()
	h.idleTick()
	h.settle()
	if !readPosition(t, h).Synced {
		t.Fatal("the failed save was not retried")
	}
}

// The big queue file is marshalled on the player goroutine but written on
// another: a slow disk does not block commands.
func TestASlowQueueWriteDoesNotBlockThePlayer(t *testing.T) {
	h := newHarness(t, nil)
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	h.p.do(func() {
		h.p.writeFile = func(path string, b []byte) error {
			started <- struct{}{}
			<-gate
			return writeBytesAtomic(path, b)
		}
	})
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second) // starts the write, which blocks
	<-started
	finished := make(chan struct{})
	go func() {
		h.p.Enqueue(songs(2, 100))
		h.p.SetVolumeDB(-10)
		savedAgain(h, a.ID) // a second save while the first write is stuck
		h.p.Enqueue(songs(1, 100))
		savedAgain(h, a.ID)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("player commands blocked behind a slow queue write")
	}
	close(gate)
	h.settle()
	// The last write wins: the file holds the newest queue.
	h.waitFor("last queue on disk", func() bool {
		r, _ := LoadResume(h.p.o.ResumePath)
		return r != nil && len(r.Songs) == 9
	})
}

// On the way out the write is synchronous and is the newest.
func TestExitWritesTheNewestQueueEvenIfAWriteIsPending(t *testing.T) {
	h := newHarness(t, nil)
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	h.p.do(func() {
		h.p.writeFile = func(path string, b []byte) error {
			started <- struct{}{}
			<-gate
			return writeBytesAtomic(path, b)
		}
	})
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	<-started
	h.p.Enqueue(songs(4, 100))
	go func() { time.Sleep(50 * time.Millisecond); close(gate) }()
	h.cancel()
	<-h.done
	h.waitFor("writer idle", func() bool { return h.p.busy.Load() == 0 })
	r, _ := LoadResume(h.p.o.ResumePath)
	if r == nil || len(r.Songs) != 10 {
		t.Fatalf("resume = %+v", r)
	}
}

// A server save for song A is still in flight when the user moves on to B and
// quits. Run's context is cancelled first, so the save is cut off and the
// server may still commit A after the exit save for B. Then the local file
// must not claim to be synced, and the local queue wins on the next start.
func TestTheExitSaveWaitsForASaveInFlight(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(6, 3000), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.settle()

	gate := make(chan struct{})
	h.api.mu.Lock()
	h.api.blockOnce = gate
	h.api.lateCommit = true
	h.api.mu.Unlock()
	savedAgain(h, a.ID) // the save for A is now stuck in the server
	h.p.Next()
	b := h.playAndStart(2)
	h.tickAt(b.ID, time.Second)
	h.cancel()
	<-h.done
	close(gate) // the server takes A after B
	h.waitFor("late commit", func() bool {
		h.api.mu.Lock()
		defer h.api.mu.Unlock()
		return h.api.saveCur == "sa"
	})
	if sp := readPosition(t, h); sp.ID != "sb" || sp.Synced {
		t.Fatalf("position file = %+v, want sb not synced", sp)
	}
	h.api.queue = &subsonic.PlayQueue{Songs: songs(1, 100), Current: "sa"}
	r, _ := h.p.Resumable(t.Context())
	if len(r.Songs) != 6 || r.Songs[r.Index].ID != "sb" {
		t.Fatalf("resumable = %d songs at %v, want the local queue at sb", len(r.Songs), r.Index)
	}
}

// A song that is in the queue twice: the copy nearest the local position.
func TestAServersRepeatedSongResumesAtTheNearestCopy(t *testing.T) {
	q := songs(40, 100)
	q[30] = q[4] // the same song twice, at 4 and 30
	h := newHarness(t, nil)
	SaveResume(h.p.o.ResumePath, Resume{Songs: q, Index: 28, Position: 20 * time.Second})
	SavePosition(h.p.o.ResumePath, SavedPosition{ID: q[28].ID, Index: 28, Position: 20 * time.Second, Synced: true})
	h.api.queue = &subsonic.PlayQueue{Songs: q[3:6], Current: q[4].ID, Position: 3000}
	r, _ := h.p.Resumable(t.Context())
	if len(r.Songs) != 40 || r.Index != 30 {
		t.Fatalf("resumable = %d songs at %d, want the copy at 30", len(r.Songs), r.Index)
	}
}
