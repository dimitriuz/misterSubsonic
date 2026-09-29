package player

import (
	"context"
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
