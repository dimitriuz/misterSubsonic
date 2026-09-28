package player

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

func TestPlayNowOpensAndReportsNowPlaying(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.ReplayGain = "track" })
	q := songs(3, 200)
	g := -6.0
	q[1].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
	h.p.PlayNow(q, 1)
	tr := h.playAndStart(1)
	if tr.Format != audio.FormatFLAC || tr.Gain < 0.50 || tr.Gain > 0.51 {
		t.Fatalf("track = %+v, want FLAC with -6 dB gain", tr)
	}
	st := h.p.State()
	if cur, _ := st.Current(); cur.ID != "sb" || st.Index != 1 {
		t.Fatalf("current = %v index %d", cur.ID, st.Index)
	}
	h.waitFor("now playing", func() bool { return slices.Equal(h.api.nowPlayings(), []subsonic.ID{"sb"}) })
}

func TestGaplessPrefetchAndHandover(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 0)
	a := h.playAndStart(1)

	h.tickAt(a.ID, 50*time.Second)
	if calls := h.opener.callList(); len(calls) != 1 {
		t.Fatalf("prefetched too early: %v", calls)
	}
	h.tickAt(a.ID, 81*time.Second)
	h.waitFor("QueueNext", func() bool { return h.eng.queueCount() == 1 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.id != "sb" || !last.prefetch {
		t.Fatalf("prefetch call = %+v", last)
	}
	b := h.eng.queued[0]

	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
	h.waitFor("handover", func() bool { return h.p.State().Index == 1 })
	if h.eng.playCount() != 1 {
		t.Fatal("gapless handover must not call engine.Play")
	}
	if !b.Source.(*fakeSource).isPromoted() {
		t.Fatal("prefetched source not promoted once it started")
	}
	h.waitFor("now playing for b", func() bool { return len(h.api.nowPlayings()) == 2 })
}

func TestEndOfQueueStops(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 100), 0)
	a := h.playAndStart(1)
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("stopped", func() bool { return h.p.State().Status == Stopped })
}

func TestEndBeforePrefetchOpenedFallsBackToPlay(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 0)
	a := h.playAndStart(1)
	// Pretend a prefetch is in flight but not yet handed to the engine.
	h.p.do(func() { h.p.seq++; h.p.nextID, h.p.nextCursor, h.p.prefetched = h.p.seq, 1, a.ID })
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("second song played", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Index != 1 {
		t.Fatalf("index = %d", h.p.State().Index)
	}
}

func TestRepeatAllWrapsAndRepeatOneReplays(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 1)
	h.p.SetRepeat(RepeatAll)
	b := h.playAndStart(1)
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: b.ID}
	h.waitFor("wrap to first", func() bool { return h.eng.playCount() == 2 && h.p.State().Index == 0 })

	h.p.SetRepeat(RepeatOne)
	a := h.playAndStart(2)
	h.tickAt(a.ID, 90*time.Second)
	if h.eng.queueCount() != 0 {
		t.Fatal("repeat-one must not prefetch")
	}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("replay", func() bool { return h.eng.playCount() == 3 && h.p.State().Index == 0 })
}

func TestShuffleKeepsCurrentFirstAndCoversQueue(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(8, 100), 3)
	h.playAndStart(1)
	h.p.SetShuffle(true)
	var order []int
	var cursor int
	h.p.do(func() { order, cursor = append([]int(nil), h.p.order...), h.p.cursor })
	if order[cursor] != 3 {
		t.Fatalf("current moved: order %v cursor %d", order, cursor)
	}
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []int{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("shuffled order %v is not a permutation", order)
	}
	h.p.SetShuffle(false)
	if st := h.p.State(); st.Index != 3 || st.Shuffle {
		t.Fatalf("after unshuffle index=%d shuffle=%v", st.Index, st.Shuffle)
	}
}

func TestScrobbleAfterHalfAndSeeksDontCount(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0) // threshold min(150s, 240s) = 150s
	a := h.playAndStart(1)
	pos := time.Duration(0)
	step := func(n int) {
		for i := 0; i < n; i++ {
			pos += 250 * time.Millisecond
			h.tickAt(a.ID, pos)
		}
	}
	step(300)               // 75 s listened
	pos += 60 * time.Second // a seek forward
	h.tickAt(a.ID, pos)     // jump is not listening
	step(4 * 74)            // 149 s listened
	time.Sleep(20 * time.Millisecond)
	if len(h.api.submissions()) != 0 {
		t.Fatal("scrobbled before 150 s of listening")
	}
	step(8)
	h.waitFor("scrobble", func() bool { return slices.Equal(h.api.submissions(), []subsonic.ID{"sa"}) })
	step(100)
	time.Sleep(20 * time.Millisecond)
	if len(h.api.submissions()) != 1 {
		t.Fatal("scrobbled twice")
	}
}

func TestFailedScrobbleIsQueuedAndRetried(t *testing.T) {
	h := newHarness(t, nil)
	h.api.failNext = 1
	q := songs(2, 8) // threshold 4 s
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	for i := 1; i <= 20; i++ {
		h.tickAt(a.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("queued", func() bool { return h.p.scrobbles.Len() == 1 })

	h.p.Next()
	b := h.playAndStart(2)
	for i := 1; i <= 20; i++ {
		h.tickAt(b.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("both submitted", func() bool {
		s := h.api.submissions()
		return len(s) == 2 && slices.Contains(s, "sa") && slices.Contains(s, "sb")
	})
	h.waitFor("queue drained", func() bool { return h.p.scrobbles.Len() == 0 })
}

func TestThreeFailuresInARowStop(t *testing.T) {
	h := newHarness(t, nil)
	for _, id := range []subsonic.ID{"sa", "sb", "sc"} {
		h.opener.fail[id] = true
	}
	h.p.PlayNow(songs(5, 100), 0)
	h.waitFor("stopped", func() bool { return h.p.State().Status == Stopped && len(h.opener.callList()) == 3 })
	errs := 0
	for len(h.p.Events()) > 0 {
		if ev := <-h.p.Events(); ev.Kind == Error {
			errs++
		}
	}
	if errs != 3 {
		t.Fatalf("error events = %d, want 3", errs)
	}
}

func TestPrevRestartsOrGoesBack(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 1)
	b := h.playAndStart(1)
	h.tickAt(b.ID, 10*time.Second)
	h.p.Prev()
	h.waitFor("restart", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Index != 1 {
		t.Fatal("Prev after 10 s should restart the same song")
	}
	b2 := h.playAndStart(2)
	h.tickAt(b2.ID, time.Second)
	h.p.Prev()
	h.waitFor("previous", func() bool { return h.eng.playCount() == 3 && h.p.State().Index == 0 })
}

func TestSeekRawUsesEngineAndTranscodedReopens(t *testing.T) {
	h := newHarness(t, nil)
	q := songs(2, 300)
	q[1].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	h.p.Seek(42 * time.Second)
	if len(h.eng.seeks) != 1 || h.eng.seeks[0] != (seekCall{a.ID, 42 * time.Second}) {
		t.Fatalf("seeks = %v", h.eng.seeks)
	}
	h.p.Next()
	h.playAndStart(2)
	if !h.p.State().Transcoded {
		t.Fatal("m4a should be transcoded")
	}
	h.p.Seek(90 * time.Second)
	h.waitFor("reopen", func() bool { return h.eng.playCount() == 3 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.id != "sb" || last.offset != 90*time.Second {
		t.Fatalf("reopen call = %+v", last)
	}
	if tr := h.eng.lastPlayed(); tr.Offset != 90*time.Second {
		t.Fatalf("reopened track offset = %v", tr.Offset)
	}
}

func TestQueueEditing(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 0) // sa sb sc
	a := h.playAndStart(1)
	h.tickAt(a.ID, 85*time.Second) // prefetches sb
	h.waitFor("queued", func() bool { return h.eng.queueCount() == 1 })

	h.p.PlayNext([]subsonic.Song{{ID: "x", Suffix: "flac", Duration: 100}})
	if h.eng.clears == 0 {
		t.Fatal("PlayNext must drop the prepared successor")
	}
	h.p.Enqueue([]subsonic.Song{{ID: "y", Suffix: "flac", Duration: 100}})
	ids := func() []subsonic.ID {
		var out []subsonic.ID
		for _, s := range h.p.State().Queue {
			out = append(out, s.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []subsonic.ID{"sa", "x", "sb", "sc", "y"}) {
		t.Fatalf("queue = %v", got)
	}
	h.p.Remove(0) // current: plays "x" next
	h.waitFor("x plays", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "x" {
		t.Fatalf("current after removing = %v", cur.ID)
	}
	h.p.Remove(3) // "y", not current
	if got := ids(); !slices.Equal(got, []subsonic.ID{"x", "sb", "sc"}) {
		t.Fatalf("queue = %v", got)
	}
	h.p.Clear()
	if st := h.p.State(); len(st.Queue) != 0 || st.Status != Stopped || st.Index != -1 {
		t.Fatalf("after Clear: %+v", st)
	}
}

func TestPauseResumeAndVolume(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 100), 0)
	h.playAndStart(1)
	h.p.TogglePause()
	if st := h.p.State(); st.Status != Paused || !h.eng.paused {
		t.Fatalf("status %v engine paused %v", st.Status, h.eng.paused)
	}
	h.p.TogglePause()
	if h.p.State().Status != Playing || h.eng.paused {
		t.Fatal("did not resume")
	}
	h.p.SetVolumeDB(-6)
	if v := h.eng.volume; v < 0.50 || v > 0.51 {
		t.Fatalf("volume = %v", v)
	}
	h.p.SetVolumeDB(12)
	if h.p.State().VolumeDB != 0 {
		t.Fatal("volume not clamped to 0 dB")
	}
}

func TestBufferingDetection(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	for i := 0; i < 6; i++ { // 1.5 s without movement
		h.tickAt(a.ID, time.Second)
	}
	if h.p.State().Status != Buffering {
		t.Fatalf("status = %v, want buffering", h.p.State().Status)
	}
	h.tickAt(a.ID, 1250*time.Millisecond)
	if h.p.State().Status != Playing {
		t.Fatalf("status = %v, want playing again", h.p.State().Status)
	}
}

func TestSavesQueueAndResumes(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 300), 1)
	b := h.playAndStart(1)
	for i := 1; i <= 121; i++ { // a little over 30 s
		h.tickAt(b.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("server save", func() bool {
		h.api.mu.Lock()
		defer h.api.mu.Unlock()
		return len(h.api.saves) > 0 && h.api.saveCur == "sb"
	})
	local, err := LoadResume(h.p.o.ResumePath)
	if err != nil || local == nil || local.Index != 1 || len(local.Songs) != 3 || local.Position < 30*time.Second {
		t.Fatalf("local resume = %+v, %v", local, err)
	}

	// Server queue wins over the local file.
	h.api.queue = &subsonic.PlayQueue{Songs: songs(3, 300), Current: "sc", Position: 12000}
	r, err := h.p.Resumable(t.Context())
	if err != nil || r.Index != 2 || r.Position != 12*time.Second {
		t.Fatalf("resumable = %+v, %v", r, err)
	}
	h.p.ResumeFrom(r)
	h.waitFor("resume play", func() bool { return h.eng.playCount() == 2 })
	h.waitFor("resume seek", func() bool {
		h.eng.mu.Lock()
		defer h.eng.mu.Unlock()
		return len(h.eng.seeks) == 1 && h.eng.seeks[0].pos == 12*time.Second
	})
}

func TestResumeFallsBackToLocalFile(t *testing.T) {
	h := newHarness(t, nil)
	if err := SaveResume(h.p.o.ResumePath, Resume{Songs: songs(2, 100), Index: 1, Position: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	r, err := h.p.Resumable(t.Context())
	if err != nil || r == nil || r.Index != 1 {
		t.Fatalf("resumable = %+v, %v", r, err)
	}
}

// Some servers report duration 0; there is then no prefetch, but the queue
// must still advance when the engine reports the end.
func TestUnknownDurationStillAdvances(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 0), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 10*time.Minute)
	if h.eng.queueCount() != 0 {
		t.Fatal("prefetched without a known duration")
	}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("second song", func() bool { return h.eng.playCount() == 2 && h.p.State().Index == 1 })
}
