package player

import (
	"context"
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

// Queue edits the UI's Queue screen can make (plan-1 follow-ups).

func TestRemoveCurrentWhilePausedStaysPaused(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 0)
	h.playAndStart(1)
	h.p.TogglePause()
	h.p.Remove(0)
	h.waitFor("next song opened", func() bool { return h.eng.playCount() == 2 })
	if st := h.p.State(); st.Status != Paused || !h.eng.paused {
		t.Fatalf("status %v, engine paused %v; want paused", st.Status, h.eng.paused)
	}
	tr := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
	h.p.do(func() {})
	if st := h.p.State(); st.Status != Paused {
		t.Fatalf("Started turned a paused player into %v", st.Status)
	}
}

func TestRemoveLastCurrentStopsWithNothingSelected(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 2)
	h.playAndStart(1)
	h.p.Remove(2)
	st := h.p.State()
	if st.Status != Stopped || st.Index != -1 || len(st.Queue) != 2 {
		t.Fatalf("after removing the last, current song: %+v", st)
	}
	h.p.TogglePause() // Start on a stopped queue plays it from the top
	h.waitFor("first song opened", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "sa" {
		t.Fatalf("playing %v, want sa", cur.ID)
	}
}

func TestRemoveLastCurrentWrapsUnderRepeatAll(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 2)
	h.playAndStart(1)
	h.p.SetRepeat(RepeatAll)
	h.p.Remove(2)
	h.waitFor("wrapped to the first song", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "sa" {
		t.Fatalf("playing %v, want sa", cur.ID)
	}
}

func TestEmptyPlayNextAndEnqueueAreIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNext(nil)
	h.p.Enqueue([]subsonic.Song{})
	if st := h.p.State(); len(st.Queue) != 0 || st.Status != Stopped || st.Index != -1 {
		t.Fatalf("state after empty adds: %+v", st)
	}
	if len(h.opener.callList()) != 0 {
		t.Fatal("an empty add opened a stream")
	}
}

func TestUnpauseDoesNotFlickerToBuffering(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.p.TogglePause()
	for i := 0; i < 8; i++ { // 2 s paused: the position doesn't move
		h.tickAt(a.ID, time.Second)
	}
	h.p.TogglePause()
	h.tickAt(a.ID, time.Second) // first tick after unpause: audio not moved yet
	if st := h.p.State(); st.Status != Playing {
		t.Fatalf("status right after unpause = %v, want playing", st.Status)
	}
}

// A seek while the track is still opening (resume, or a seek right after
// Play) must land where the user asked, not where the open started.
func TestSeekWhileOpeningAimsTheOpen(t *testing.T) {
	h := newHarness(t, nil)
	block := make(chan struct{})
	open := h.opener.open
	h.p.o.Open = func(ctx context.Context, s subsonic.Song, off time.Duration, pre bool) (Opened, error) {
		<-block
		return open(ctx, s, off, pre)
	}
	h.p.ResumeFrom(&Resume{Songs: songs(1, 300), Index: 0, Position: 60 * time.Second})
	h.p.Seek(120 * time.Second)
	close(block)
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() == 1 })
	h.waitFor("seek sent", func() bool { return len(h.eng.seekList()) == 1 })
	if s := h.eng.seekList()[0]; s.pos != 120*time.Second {
		t.Fatalf("seeked to %v, want 120s (the user's seek, not the resume offset)", s.pos)
	}
}

func TestSeekWhileOpeningTranscodedReopensAtTarget(t *testing.T) {
	h := newHarness(t, nil)
	block := make(chan struct{})
	open := h.opener.open
	h.p.o.Open = func(ctx context.Context, s subsonic.Song, off time.Duration, pre bool) (Opened, error) {
		<-block
		return open(ctx, s, off, pre)
	}
	q := songs(1, 300)
	q[0].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	h.p.Seek(90 * time.Second)
	close(block)
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() == 1 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.offset != 90*time.Second {
		t.Fatalf("played an open at %v, want 90s; calls %+v", last.offset, calls)
	}
	if tr := h.eng.lastPlayed(); tr.Offset != 90*time.Second {
		t.Fatalf("engine got offset %v, want 90s", tr.Offset)
	}
}

// A successor that fails to prefetch is retried when its turn comes; only
// that attempt reports, so the user sees one toast, not two.
func TestFailedPrefetchReportsOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.opener.fail["sb"] = true
	h.p.PlayNow(songs(3, 100), 0)
	a := h.playAndStart(1)
	drainEvents(h)
	h.tickAt(a.ID, 85*time.Second) // prefetch of sb fails
	h.waitFor("prefetch attempted", func() bool { return len(h.opener.callList()) == 2 })
	h.p.do(func() {})
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("sc played after sb failed again", func() bool { return h.eng.playCount() == 2 })
	errs := 0
	for len(h.p.Events()) > 0 {
		if ev := <-h.p.Events(); ev.Kind == Error {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("error events = %d, want 1", errs)
	}
}

// Move (the web remote's reorder): the queue order changes, the current song
// and its playback do not.

func queueIDs(p *Player) []subsonic.ID {
	var out []subsonic.ID
	for _, s := range p.State().Queue {
		out = append(out, s.ID)
	}
	return out
}

func TestMoveReordersAndCurrentIndexFollows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cur       int
		from, to  int
		wantQueue []subsonic.ID
		wantIndex int
	}{
		{"entry before current crosses it", 2, 0, 3, []subsonic.ID{"sb", "sc", "sd", "sa", "se"}, 1},
		{"entry after current crosses it", 1, 3, 0, []subsonic.ID{"sd", "sa", "sb", "sc", "se"}, 2},
		{"current itself moves down", 1, 1, 3, []subsonic.ID{"sa", "sc", "sd", "sb", "se"}, 3},
		{"current itself moves up", 3, 3, 0, []subsonic.ID{"sd", "sa", "sb", "sc", "se"}, 0},
		{"entries on one side only", 0, 3, 4, []subsonic.ID{"sa", "sb", "sc", "se", "sd"}, 0},
		{"first to last", 2, 0, 4, []subsonic.ID{"sb", "sc", "sd", "se", "sa"}, 1},
		{"last to first", 2, 4, 0, []subsonic.ID{"se", "sa", "sb", "sc", "sd"}, 3},
		{"same place", 2, 2, 2, []subsonic.ID{"sa", "sb", "sc", "sd", "se"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.p.PlayNow(songs(5, 100), tc.cur)
			h.playAndStart(1)
			h.p.Move(tc.from, tc.to)
			if got := queueIDs(h.p); !slices.Equal(got, tc.wantQueue) {
				t.Fatalf("queue = %v, want %v", got, tc.wantQueue)
			}
			st := h.p.State()
			if st.Index != tc.wantIndex {
				t.Fatalf("index = %d, want %d", st.Index, tc.wantIndex)
			}
			if cur, _ := st.Current(); cur.ID != subsonic.ID("s"+string(rune('a'+tc.cur))) {
				t.Fatalf("current song = %v after the move", cur.ID)
			}
			if h.eng.playCount() != 1 || h.eng.stops != 0 {
				t.Fatalf("playback was interrupted: %d plays, %d stops", h.eng.playCount(), h.eng.stops)
			}
		})
	}
}

func TestMoveOutOfRangeDoesNothing(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 1)
	h.playAndStart(1)
	for _, m := range [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {9, 9}} {
		h.p.Move(m[0], m[1])
	}
	if got := queueIDs(h.p); !slices.Equal(got, []subsonic.ID{"sa", "sb", "sc"}) {
		t.Fatalf("queue = %v", got)
	}
	if st := h.p.State(); st.Index != 1 {
		t.Fatalf("index = %d", st.Index)
	}
	newHarness(t, nil).p.Move(0, 1) // an empty queue
}

// During shuffle the visible queue moves like any other; the play order
// (which song comes next) is kept, only relabelled.
func TestMoveDuringShuffleKeepsPlayOrder(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(6, 100), 2)
	h.playAndStart(1)
	h.p.SetShuffle(true)
	seq := func() []subsonic.ID { // ids in play order from the cursor on
		var out []subsonic.ID
		h.p.do(func() {
			for _, qi := range h.p.order[h.p.cursor:] {
				out = append(out, h.p.queue[qi].ID)
			}
		})
		return out
	}
	before := seq()
	h.p.Move(5, 0)
	h.p.Move(1, 4)
	if got := seq(); !slices.Equal(got, before) {
		t.Fatalf("play order %v, was %v", got, before)
	}
	st := h.p.State()
	if cur, _ := st.Current(); cur.ID != "sc" {
		t.Fatalf("current = %v", cur.ID)
	}
	if next := st.Queue[st.NextIndex].ID; next != before[1] {
		t.Fatalf("next = %v, want %v", next, before[1])
	}
	if h.eng.playCount() != 1 {
		t.Fatal("shuffled Move restarted playback")
	}
}

func TestMoveRePrefetchesOnlyWhenNextChanged(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(5, 100), 0) // sa sb sc sd se
	a := h.playAndStart(1)
	h.tickAt(a.ID, 85*time.Second) // prefetches sb
	h.waitFor("queued", func() bool { return h.eng.queueCount() == 1 })
	clears := h.eng.clearCount()

	h.p.Move(3, 4) // behind the next song: sb stays next
	if h.eng.clearCount() != clears {
		t.Fatal("a move away from the next song dropped the prefetch")
	}
	if st := h.p.State(); st.Queue[st.NextIndex].ID != "sb" {
		t.Fatalf("next = %v", st.Queue[st.NextIndex].ID)
	}

	h.p.Move(1, 3) // the prefetched song leaves: sc is next now
	if h.eng.clearCount() == clears {
		t.Fatal("the prefetched song moved but the prefetch was kept")
	}
	h.tickAt(a.ID, 86*time.Second)
	h.waitFor("re-prefetched", func() bool { return h.eng.queueCount() == 2 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.id != "sc" || !last.prefetch {
		t.Fatalf("re-prefetch call = %+v, want sc", last)
	}
}

// Without shuffle Next follows the moved queue, not the old order.
func TestMoveThenNextPlaysTheNewNeighbour(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(4, 100), 0) // sa sb sc sd
	h.playAndStart(1)
	h.p.Move(3, 1) // sa sd sb sc
	h.p.Next()
	h.waitFor("next opened", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "sd" {
		t.Fatalf("Next played %v, want sd", cur.ID)
	}
}

// A Move that leaves the next song the same must still point the gapless
// handover at that song's new place in the order, whether the successor is
// ready (queued in the engine) or still opening.
func TestMoveKeepsHandoverOnTheSameNextSong(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to int
		wantQ    []subsonic.ID
		wantIdx  int
	}{
		{"entry before current moves behind next", 0, 4, []subsonic.ID{"sb", "sc", "sd", "se", "sa"}, 1},
		{"entry behind next moves before current", 4, 0, []subsonic.ID{"se", "sa", "sb", "sc", "sd"}, 3},
	} {
		for _, opening := range []bool{false, true} {
			name := tc.name + "/prefetched"
			if opening {
				name = tc.name + "/still opening"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t, nil)
				h.p.PlayNow(songs(5, 100), 1) // sa [sb] sc sd se, sc is next
				a := h.playAndStart(1)
				h.tickAt(a.ID, 85*time.Second)
				h.waitFor("queued", func() bool { return h.eng.queueCount() == 1 })
				if opening {
					// the engine has the successor, the player has not seen it ready yet
					h.p.do(func() { h.p.nextSrc = Opened{} })
				}
				clears := h.eng.clearCount()
				h.p.Move(tc.from, tc.to)
				if got := queueIDs(h.p); !slices.Equal(got, tc.wantQ) {
					t.Fatalf("queue = %v, want %v", got, tc.wantQ)
				}
				if h.eng.clearCount() != clears {
					t.Fatal("the next song did not change but the prefetch was dropped")
				}
				if !opening {
					b := h.eng.queued[0]
					h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
					h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
				} else {
					h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: h.eng.queued[0].ID}
				}
				h.waitFor("handover", func() bool { return h.p.State().Index == tc.wantIdx })
				if cur, _ := h.p.State().Current(); cur.ID != "sc" {
					t.Fatalf("current after handover = %v, want sc", cur.ID)
				}
			})
		}
	}
}
