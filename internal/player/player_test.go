package player

import (
	"errors"
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

// C2: when the engine gives up on a queued successor that never finished
// opening, it reports Error(B) and then Ended(A). The player must not hand
// over to B (which will never start) but open it afresh with Play.
func TestSuccessorOpenTimeoutFallsBackToPlay(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 81*time.Second)
	h.waitFor("QueueNext", func() bool { return h.eng.queueCount() == 1 })
	b := h.eng.queued[0]

	h.eng.events <- audio.Event{Kind: audio.EventError, TrackID: b.ID, Err: audio.ErrOpenTimeout}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("second song played", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Index != 1 {
		t.Fatalf("index = %d, want 1", h.p.State().Index)
	}
	if tr := h.eng.lastPlayed(); tr.ID == b.ID || tr.Source == b.Source {
		t.Fatal("Play reused the abandoned successor instead of reopening it")
	}
	var nextSrc Opened
	h.p.do(func() { nextSrc = h.p.nextSrc })
	if nextSrc.Source != nil {
		t.Fatal("abandoned successor's source is still held as nextSrc")
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
	if seeks := h.eng.seekList(); len(seeks) != 1 || seeks[0] != (seekCall{a.ID, 42 * time.Second}) {
		t.Fatalf("seeks = %v", seeks)
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

// A queue edit between Ended(A) and Started(B) must not leave the player stuck.
// This test would wedge before the fix: Enqueue zeroes nextID, so Started(B) is ignored.
func TestQueueEditBetweenEndedAndStartedDoesNotWedge(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 85*time.Second)
	h.waitFor("QueueNext", func() bool { return h.eng.queueCount() == 1 })
	b := h.eng.queued[0]
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	// Wait for Run to receive the event before enqueuing, to ensure deterministic ordering
	h.waitFor("Ended processed", func() bool { return len(h.eng.events) == 0 })
	h.p.do(func() {})
	h.p.Enqueue([]subsonic.Song{{ID: "y", Suffix: "flac", Duration: 100}})
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
	h.p.do(func() {})
	st := h.p.State()
	if st.Index != 1 || len(st.Queue) != 4 {
		t.Fatalf("after handover: index=%d queue len=%d, want 1 and 4", st.Index, len(st.Queue))
	}
	h.tickAt(b.ID, 5*time.Second)
	if h.p.State().Position != 5*time.Second {
		t.Fatalf("position=%v, want 5s", h.p.State().Position)
	}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: b.ID}
	h.waitFor("third song plays", func() bool { return h.eng.playCount() == 2 && h.p.State().Index == 2 && h.p.State().Status == Loading })
}

// A transcoded seek must not reset listen time, resend now-playing, or unpause.
func TestTranscodedSeekKeepsListenTimeAndPause(t *testing.T) {
	h := newHarness(t, nil)
	q := songs(1, 300)
	q[0].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	pos := time.Duration(0)
	for i := 0; i < 4*140; i++ {
		pos += 250 * time.Millisecond
		h.tickAt(a.ID, pos)
	}
	h.p.TogglePause()
	if h.p.State().Status != Paused {
		t.Fatal("not paused")
	}
	// Drain any events from initial play and pause toggle
	for len(h.p.Events()) > 0 {
		<-h.p.Events()
	}
	nowPlayingsBefore := len(h.api.nowPlayings())
	h.p.Seek(200 * time.Second)
	h.waitFor("reopen", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Status != Paused {
		t.Fatalf("after seek status=%v, want paused", h.p.State().Status)
	}
	if !h.eng.paused {
		t.Fatal("engine not paused after reopen")
	}
	if len(h.api.nowPlayings()) != nowPlayingsBefore {
		t.Fatalf("now-playings=%d, want %d (no resend on seek)", len(h.api.nowPlayings()), nowPlayingsBefore)
	}
	events := 0
	for len(h.p.Events()) > 0 {
		<-h.p.Events()
		events++
	}
	if events > 0 {
		t.Fatalf("seek emitted %d events, want 0 (no TrackChanged)", events)
	}
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.offset != 200*time.Second {
		t.Fatalf("opened at %v, want 200s", last.offset)
	}
	b := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
	h.p.do(func() {})
	time.Sleep(30 * time.Millisecond) // Let async nowPlaying complete
	if len(h.api.nowPlayings()) != nowPlayingsBefore {
		t.Fatalf("after Started, nowPlayings=%d, want %d (no duplicate announce)", len(h.api.nowPlayings()), nowPlayingsBefore)
	}
	h.p.TogglePause()
	pos = 200 * time.Second
	for i := 0; i < 44; i++ { // 44 * 250ms = 11s; total listened = 140 + 11 = 151s > 150s threshold
		pos += 250 * time.Millisecond
		h.tickAt(b.ID, pos)
	}
	h.waitFor("scrobble after 11s more", func() bool { return slices.Equal(h.api.submissions(), []subsonic.ID{"sa"}) })
}

// Commands after Run returns must not block.
func TestCommandsAfterRunExitDoNotBlock(t *testing.T) {
	h := newHarness(t, nil)
	h.cancel()
	<-h.done
	done := make(chan struct{})
	go func() {
		h.p.Next()
		h.p.TogglePause()
		h.p.PlayNow(songs(1, 10), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("commands blocked after Run exit")
	}
}

// Next during a transcoded reopen must still announce the next track.
func TestNextDuringTranscodedReopenStillAnnouncesNext(t *testing.T) {
	h := newHarness(t, nil)
	q := songs(2, 100)
	q[0].Suffix = "m4a"
	q[1].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	_ = h.playAndStart(1)
	h.p.Seek(100 * time.Second)
	h.waitFor("reopen", func() bool { return h.eng.playCount() == 2 })
	h.p.Next()
	h.waitFor("second song plays", func() bool { return h.eng.playCount() == 3 })
	b := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
	h.p.do(func() {})
	h.waitFor("both announced", func() bool { return slices.Equal(h.api.nowPlayings(), []subsonic.ID{"sa", "sb"}) })
}

// I1: a seek that is slow in the engine (an HTTP Range restart) must not
// block the player goroutine, and so neither the caller nor the next command.
func TestSeekDoesNotBlockOnEngine(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	h.eng.mu.Lock()
	h.eng.seekBlock = release
	h.eng.mu.Unlock()
	defer close(release)
	h.p.PlayNow(songs(1, 100), 0)
	h.playAndStart(1)

	returned := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { f(); close(done) }()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("%s blocked while the engine seek is in progress", what)
		}
	}
	returned("Seek", func() { h.p.Seek(30 * time.Second) })
	returned("TogglePause", h.p.TogglePause)
}

// I1/R16: a raw seek that fails in the engine arrives as EventSeekFailed.
// The player reopens at the requested position (not wherever ticks moved
// it meanwhile), and only once, so a seek that always fails can't loop.
func TestSeekFailureReopensAtTargetOnce(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	h.eng.mu.Lock()
	h.eng.seekErr = errors.New("range restart failed")
	h.eng.seekBlock = release
	h.eng.mu.Unlock()
	h.p.PlayNow(songs(1, 100), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 5*time.Second)

	h.p.Seek(30 * time.Second)
	h.tickAt(a.ID, 5*time.Second+250*time.Millisecond) // engine still at the old place
	close(release)
	h.waitFor("fallback reopen", func() bool { return h.eng.playCount() == 2 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.offset != 30*time.Second {
		t.Fatalf("reopened at %v, want 30s", last.offset)
	}
	// The reopened track's resume seek fails too: no second reopen.
	h.waitFor("resume seek", func() bool { return len(h.eng.seekList()) == 2 })
	time.Sleep(30 * time.Millisecond)
	h.p.do(func() {})
	if n := h.eng.playCount(); n != 2 {
		t.Fatalf("playCount = %d after a second seek failure, want 2 (reopen once)", n)
	}
}

// Seek while loading must announce when Started arrives.
func TestSeekWhileLoadingStillAnnounces(t *testing.T) {
	h := newHarness(t, nil)
	h.eng.seekErr = audio.ErrNotCurrent
	h.p.PlayNow(songs(1, 100), 0)
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() >= 1 })
	h.p.Seek(30 * time.Second)
	h.waitFor("fallback reopen", func() bool { return h.eng.playCount() == 2 })
	tr := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
	h.p.do(func() {})
	time.Sleep(30 * time.Millisecond) // Wait for async nowPlaying to complete
	if len(h.api.nowPlayings()) != 1 {
		t.Fatalf("nowPlayings=%d, want 1", len(h.api.nowPlayings()))
	}
	if !slices.Equal(h.api.nowPlayings(), []subsonic.ID{"sa"}) {
		t.Fatalf("nowPlayings=%v, want [sa]", h.api.nowPlayings())
	}
}

// I1: a consumer that receives an event must see a State() consistent with
// it, never a stale snapshot from before the event's cause was applied.
// mss-cli's -exit-at-end relies on this: it reads State() right after a
// StatusChanged event and exits when it says Stopped.
func TestStateIsCurrentWhenEventArrives(t *testing.T) {
	const iterations = 2000
	for i := 0; i < iterations; i++ {
		h := newHarness(t, nil)
		h.p.PlayNow(songs(1, 100), 0)
		a := h.playAndStart(1)
		drainEvents(h) // discard the startup QueueChanged/TrackChanged/StatusChanged(Loading->Playing)
		h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
		for {
			ev := <-h.p.Events()
			if ev.Kind == StatusChanged {
				if st := h.p.State().Status; st != Stopped {
					t.Fatalf("iteration %d: State().Status = %v right after receiving StatusChanged, want Stopped", i, st)
				}
				break
			}
		}
		h.cancel()
		<-h.done
	}
}

func drainEvents(h *harness) {
	for {
		select {
		case <-h.p.Events():
		default:
			return
		}
	}
}

// R12: New must clamp the start volume to -60..0 dB, same as SetVolumeDB,
// so a bad config value can't push the real device's volume out of range.
func TestNewClampsVolume(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.VolumeDB = 12 })
	if got := h.p.State().VolumeDB; got != 0 {
		t.Fatalf("VolumeDB = %v, want 0 (clamped from 12)", got)
	}
	h.waitFor("engine volume set", func() bool { return h.eng.getVolume() != 0 })
	if got := h.eng.getVolume(); got < 0.999 || got > 1.001 {
		t.Fatalf("engine.SetVolume = %v, want 1 (0 dB)", got)
	}

	h2 := newHarness(t, func(o *Options) { o.VolumeDB = -100 })
	if got := h2.p.State().VolumeDB; got != -60 {
		t.Fatalf("VolumeDB = %v, want -60 (clamped from -100)", got)
	}
}
