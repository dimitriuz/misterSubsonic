package audio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestEngine(out *fakeOutput) *Engine {
	return NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
}

func TestEnginePlaysTrackWithGainThenEnds(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	pcm := ramp(1000, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(pcm, OutputRate), Gain: 0.5})
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 1000)
	expectEvent(t, e, EventEnded, 1)

	got := out.written()
	if len(got) != len(pcm) {
		t.Fatalf("wrote %d samples, want %d", len(got), len(pcm))
	}
	for i := range pcm {
		if got[i] != pcm[i]*0.5 {
			t.Fatalf("sample %d = %v, want %v", i, got[i], pcm[i]*0.5)
		}
	}
}

func TestEngineGaplessAt48k(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	a, b := ramp(700, 0), ramp(900, 700)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	waitFor(t, "both tracks written", func() bool { return len(out.written())/2 == 1600 })
	out.consume(699)
	noEvent(t, e, 30*time.Millisecond)
	out.consume(1)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	want := append(append([]float32(nil), a...), b...)
	got := out.written()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v (gap or overlap at the boundary)", i, got[i], want[i])
		}
	}
	out.consume(900)
	expectEvent(t, e, EventEnded, 2)
}

// Two halves of a 44.1k file, played gaplessly through the resampler, must
// produce exactly the same 48k output as the whole file.
func TestEngineGaplessResampledMatchesWholeFile(t *testing.T) {
	read := func(name string) *bytes.Reader {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.NewReader(b)
	}
	render := func(tracks ...string) []float32 {
		out := newFakeOutput(1 << 20)
		e := NewEngine(EngineOptions{Output: out, Poll: time.Millisecond})
		defer e.Close()
		e.Play(Track{ID: 1, Source: read(tracks[0]), Format: FormatFLAC})
		for i, name := range tracks[1:] {
			e.QueueNext(Track{ID: uint64(i + 2), Source: read(name), Format: FormatFLAC})
		}
		last := uint64(len(tracks))
		deadline := time.After(3 * time.Second)
		for {
			out.consume(1 << 30)
			select {
			case ev := <-e.Events():
				if ev.Kind == EventError {
					t.Fatalf("engine error: %v", ev.Err)
				}
				if ev.Kind == EventEnded && ev.TrackID == last {
					return out.written()
				}
			case <-time.After(time.Millisecond):
			case <-deadline:
				t.Fatal("timed out rendering")
			}
		}
	}
	whole := render("tone-44k16.flac")
	split := render("half-a.flac", "half-b.flac")
	if len(whole) != len(split) {
		t.Fatalf("whole=%d samples split=%d samples", len(whole), len(split))
	}
	for i := range whole {
		if whole[i] != split[i] {
			t.Fatalf("sample %d differs: whole=%v split=%v", i, whole[i], split[i])
		}
	}
}

func TestEngineSeekFlushesAndContinuesFromTarget(t *testing.T) {
	out := newFakeOutput(1000)
	e := newTestEngine(out)
	defer e.Close()

	pcm := ramp(48000, 0)
	e.Play(Track{ID: 7, Source: newFakeSource(pcm, OutputRate), Offset: 2 * time.Second})
	expectEvent(t, e, EventStarted, 7)
	waitFor(t, "ring full", func() bool { return len(out.written())/2 == 1000 })

	// Seek is asynchronous: wait for Position to show the target.
	e.Seek(7, 2*time.Second+500*time.Millisecond)
	waitFor(t, "seek applied", func() bool {
		_, pos, _ := e.Position()
		return pos == 2*time.Second+500*time.Millisecond
	})
	out.mu.Lock()
	flushes := out.flushes
	out.mu.Unlock()
	if flushes < 2 { // one from Play, one from Seek
		t.Fatalf("Seek did not flush the output (flushes=%d)", flushes)
	}
	id, pos, ok := e.Position()
	if !ok || id != 7 || pos != 2*time.Second+500*time.Millisecond {
		t.Fatalf("Position after seek = %d %v %v", id, pos, ok)
	}
	waitFor(t, "post-seek data", func() bool { return len(out.written())/2 >= 1480 })
	got := out.written()
	if got[1000*2] != pcm[24000*2] {
		t.Fatalf("first frame after seek = %v, want frame 24000 (%v)", got[1000*2], pcm[24000*2])
	}
	out.consume(480)
	if _, pos, _ := e.Position(); pos != 2*time.Second+510*time.Millisecond {
		t.Fatalf("Position after 480 frames = %v, want 2.51s", pos)
	}
	noEvent(t, e, 20*time.Millisecond) // a seek is not a new track
}

// I1: Seek posts the command and returns; a decoder seek that blocks on
// the network must not hold up the caller (the player goroutine).
func TestEngineSeekDoesNotWaitForDecoder(t *testing.T) {
	out := newFakeOutput(1000)
	e := newTestEngine(out)
	defer e.Close()
	src := newFakeSource(ramp(48000, 0), OutputRate)
	src.seekBlock = make(chan struct{})
	defer close(src.seekBlock)
	e.Play(Track{ID: 1, Source: src})
	expectEvent(t, e, EventStarted, 1)

	done := make(chan struct{})
	go func() { e.Seek(1, time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Seek waited for the decoder seek to finish")
	}
}

func TestEnginePlayInterruptsStalledRead(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	stalled := newFakeSource(ramp(1000, 0), OutputRate)
	stalled.block = make(chan struct{}) // never released
	e.Play(Track{ID: 1, Source: stalled})
	expectEvent(t, e, EventStarted, 1)

	e.Play(Track{ID: 2, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 2) // no EventError for track 1
	if !stalled.isClosed() {
		t.Fatal("stalled source was not closed")
	}
}

func TestEngineQueuedOpenFailureReportsError(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	bad := newFakeSource(nil, OutputRate)
	bad.openErr = errBoom
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(500, 0), OutputRate)})
	e.QueueNext(Track{ID: 2, Source: bad})
	// Started(1) and Error(2) are independent; either may come first.
	var sawStart, sawErr bool
	for !sawStart || !sawErr {
		ev := nextEvent(t, e)
		switch {
		case ev.Kind == EventStarted && ev.TrackID == 1:
			sawStart = true
		case ev.Kind == EventError && ev.TrackID == 2 && ev.Err == errBoom:
			sawErr = true
		default:
			t.Fatalf("unexpected event %+v", ev)
		}
	}
	playOut(t, out, 500)
	expectEvent(t, e, EventEnded, 1)
}

func TestEngineSeekWrongTrack(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(100000, 0), OutputRate)})
	e.Seek(99, time.Second)
	for {
		ev := nextEvent(t, e)
		if ev.Kind == EventStarted && ev.TrackID == 1 {
			continue
		}
		if ev.Kind != EventSeekFailed || ev.TrackID != 99 || ev.Err != ErrNotCurrent {
			t.Fatalf("event = %+v, want EventSeekFailed for track 99 with ErrNotCurrent", ev)
		}
		return
	}
}

func TestEngineStopSilences(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	src := newFakeSource(ramp(100000, 0), OutputRate)
	e.Play(Track{ID: 1, Source: src})
	expectEvent(t, e, EventStarted, 1)
	e.Stop()
	waitFor(t, "source closed", src.isClosed)
	waitFor(t, "position cleared", func() bool { _, _, ok := e.Position(); return !ok })
}

func TestEngineQueueNextAfterDecodeFinishedStillPlays(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	a, b := ramp(300, 0), ramp(400, 300)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	waitFor(t, "a decoded", func() bool { return len(out.written())/2 == 300 })

	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, OutputRate)}) // late, but a is still buffered
	waitFor(t, "b appended", func() bool { return len(out.written())/2 == 700 })
	out.consume(300)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)
	got := out.written()
	for i := range b {
		if got[600+i] != b[i] {
			t.Fatalf("b sample %d = %v, want %v", i, got[600+i], b[i])
		}
	}

	// Even after everything has played out, a queued track still starts.
	out.consume(400)
	expectEvent(t, e, EventEnded, 2)
	e.QueueNext(Track{ID: 3, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 3)
}

func TestEngineQueueNextAfterStopDoesNotPlay(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	e.Stop()
	e.QueueNext(Track{ID: 2, Source: newFakeSource(ramp(100, 0), OutputRate)})
	noEvent(t, e, 50*time.Millisecond)
}

func TestEngineStopWhileWaitingForNextOpenDoesNotAutoplayLater(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	// Queue B with openBlock BEFORE waiting for any event so finishCur will wait for it
	b := newFakeSource(ramp(300, 200), OutputRate)
	b.openBlock = make(chan struct{})

	a := ramp(200, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	e.QueueNext(Track{ID: 2, Source: b})

	// First event should be Started(1)
	expectEvent(t, e, EventStarted, 1)

	// Wait until A is fully written (finishCur is now waiting for B to open)
	waitFor(t, "a fully written", func() bool { return len(out.written())/2 == 200 })

	// Stop while finishCur is waiting
	e.Stop()

	// Queue C (normal, no block) after Stop
	e.QueueNext(Track{ID: 3, Source: newFakeSource(ramp(100, 200), OutputRate)})

	// C should NOT start (no autoplay after Stop)
	noEvent(t, e, 50*time.Millisecond)
}

func TestEnginePlayInterruptedDuringOpenReportsNoError(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	// Play A whose open blocks
	a := newFakeSource(ramp(1000, 0), OutputRate)
	a.openBlock = make(chan struct{})
	a.openStarted = make(chan struct{})
	e.Play(Track{ID: 1, Source: a})

	// Wait for A's open to start (doPlay is now inside openVoice with busy == a)
	select {
	case <-a.openStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for A's open to start")
	}

	// Play B immediately (interrupts A's open)
	b := newFakeSource(ramp(100, 0), OutputRate)
	e.Play(Track{ID: 2, Source: b})

	// Should see Started(2), but no EventError for A (even though A's open was interrupted)
	eventsSeen := 0
	for eventsSeen < 1 {
		ev := nextEvent(t, e)
		if ev.Kind == EventError && ev.TrackID == 1 {
			t.Fatalf("unexpected EventError for track 1: %v", ev.Err)
		}
		if ev.Kind == EventStarted && ev.TrackID == 2 {
			eventsSeen++
		}
	}

	noEvent(t, e, 30*time.Millisecond)
}

func TestEngineStopDoesNotReportFlushedBoundaries(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	// Set up onFlush to call checkBoundaries at the worst moment
	out.onFlush = func() { e.checkBoundaries() }

	a := ramp(500, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	b := ramp(500, 500)
	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, OutputRate)})

	// Wait until both written
	waitFor(t, "both tracks written", func() bool { return len(out.written())/2 == 1000 })

	// Consume all of A (500 frames), then expect Ended(1) and Started(2)
	out.consume(500)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	// Consume 100 more frames of B
	out.consume(100)

	// Stop() will flush; before the fix, this would report Ended(2) for audio never heard
	e.Stop()

	// Those events must NOT appear
	noEvent(t, e, 50*time.Millisecond)
}

func TestEngineInterruptKeepsNewTrackSource(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	// Play B whose open blocks and notifies when it starts
	b := newFakeSource(ramp(100, 0), OutputRate)
	b.openBlock = make(chan struct{})
	b.openStarted = make(chan struct{})

	go func() {
		e.Play(Track{ID: 1, Source: b})
	}()

	// Wait for the open to start (doPlay is now busy with B's source)
	select {
	case <-b.openStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for open to start")
	}

	// Call interrupt(b) directly to simulate the race
	e.interrupt(b)

	// b.Source should NOT be closed (Play never closes the NEW track's source)
	if b.isClosed() {
		t.Fatal("new track's source was incorrectly closed by interrupt")
	}

	// Unblock the open
	close(b.openBlock)

	// B should start and play normally
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 100)
	expectEvent(t, e, EventEnded, 1)
}

func TestEngineEventsFlowWhileDecodeBlocked(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	// Play A = ramp(300)
	a := ramp(300, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})

	// QueueNext B = ramp(300) whose reads block
	b := newFakeSource(ramp(300, 300), OutputRate)
	b.block = make(chan struct{})
	e.QueueNext(Track{ID: 2, Source: b})

	// expectEvent(Started,1) - first event
	expectEvent(t, e, EventStarted, 1)

	// wait until 300 frames written
	waitFor(t, "300 frames written", func() bool { return len(out.written())/2 >= 300 })

	// consume 300
	out.consume(300)

	// expectEvent(Ended,1); expectEvent(Started,2) — while B is still blocked
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	// close(B.block)
	close(b.block)

	// playOut to 600; expectEvent(Ended,2)
	playOut(t, out, 600)
	expectEvent(t, e, EventEnded, 2)
}

// C2: if the successor never finishes opening, the engine must not sit at
// the end of the current track forever. After openWait it drops the
// successor (EventError for it) and ends the queue, so the player gets
// Ended(A) and can start B itself.
func TestEngineBoundsWaitForSuccessorOpen(t *testing.T) {
	out := newFakeOutput(100000)
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond, openWait: 100 * time.Millisecond})
	defer e.Close()

	b := newFakeSource(ramp(300, 300), OutputRate)
	b.openBlock = make(chan struct{}) // never released
	a := newFakeSource(ramp(300, 0), OutputRate)
	a.block = make(chan struct{}) // hold A's first read so QueueNext lands before A ends
	e.Play(Track{ID: 1, Source: a})
	e.QueueNext(Track{ID: 2, Source: b})
	expectEvent(t, e, EventStarted, 1)
	close(a.block)

	var sawErr, sawEnded bool
	deadline := time.After(time.Second)
	for !sawErr || !sawEnded {
		out.consume(1 << 30)
		select {
		case ev := <-e.Events():
			switch {
			case ev.Kind == EventError && ev.TrackID == 2:
				sawErr = true
			case ev.Kind == EventEnded && ev.TrackID == 1:
				sawEnded = true
			default:
				t.Fatalf("unexpected event %+v", ev)
			}
		case <-time.After(time.Millisecond):
		case <-deadline:
			t.Fatalf("after 1 s: Error(2)=%v Ended(1)=%v; the engine is stuck waiting for B to open", sawErr, sawEnded)
		}
	}
	if !b.isClosed() {
		t.Fatal("the stuck successor's source was not closed")
	}
	noEvent(t, e, 30*time.Millisecond)
}

// C2: same, when the successor is queued only after the current track has
// finished decoding (the player may already have handed over to it).
func TestEngineBoundsWaitForLateSuccessorOpen(t *testing.T) {
	out := newFakeOutput(100000)
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond, openWait: 100 * time.Millisecond})
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(300, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 300)
	expectEvent(t, e, EventEnded, 1)

	b := newFakeSource(ramp(300, 300), OutputRate)
	b.openBlock = make(chan struct{}) // never released
	e.QueueNext(Track{ID: 2, Source: b})
	select {
	case ev := <-e.Events():
		if ev.Kind != EventError || ev.TrackID != 2 {
			t.Fatalf("event = %+v, want EventError for track 2", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no EventError for a successor stuck opening after the queue ended")
	}
	if !b.isClosed() {
		t.Fatal("the stuck successor's source was not closed")
	}
}

// Spec §6 / R14: a boosting gain (> 1) is soft-clipped, so samples pushed
// past full scale stay within [-1, 1] and keep their order (no fold-back).
func TestEngineSoftClipsBoostedSamples(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	const n = 1000
	pcm := make([]float32, n*2)
	for i := 0; i < n; i++ {
		v := 0.9 * float32(i) / (n - 1)
		pcm[2*i], pcm[2*i+1] = v, -v
	}
	e.Play(Track{ID: 1, Source: newFakeSource(pcm, OutputRate), Gain: 2})
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, n)
	expectEvent(t, e, EventEnded, 1)

	got := out.written()
	if len(got) != len(pcm) {
		t.Fatalf("wrote %d samples, want %d", len(got), len(pcm))
	}
	for i := 0; i < n; i++ {
		l, r := got[2*i], got[2*i+1]
		if l > 1 || l < -1 || r > 1 || r < -1 {
			t.Fatalf("frame %d = (%v, %v), outside [-1, 1]", i, l, r)
		}
		if i > 0 && (l < got[2*i-2] || r > got[2*i-1]) {
			t.Fatalf("frame %d = (%v, %v) after (%v, %v): not monotonic", i, l, r, got[2*i-2], got[2*i-1])
		}
		if want := pcm[2*i] * 2; want <= 0.5 && l != want {
			t.Fatalf("frame %d = %v, want %v: quiet samples must only be scaled", i, l, want)
		}
	}
	if peak := got[2*(n-1)]; peak < 0.95 {
		t.Fatalf("peak %v: the loud end was squashed, not clipped", peak)
	}
}

// blockingSeekDecoder blocks inside SeekFrame until released, like a raw
// FLAC seek waiting on an HTTP Range restart.
type blockingSeekDecoder struct {
	fakeDecoder
	release chan struct{}
	seeks   *atomic.Int32
	last    *atomic.Uint64
}

func (d *blockingSeekDecoder) SeekFrame(f uint64) error {
	d.seeks.Add(1)
	<-d.release
	d.last.Store(f)
	return d.fakeDecoder.SeekFrame(f)
}

func TestEngineSeeksCoalesceLatestWins(t *testing.T) {
	out := newFakeOutput(1000)
	release := make(chan struct{})
	var seeks atomic.Int32
	var last atomic.Uint64
	open := func(src io.ReadSeeker, f Format) (Decoder, error) {
		d, err := fakeOpen(src, f)
		if err != nil {
			return nil, err
		}
		return &blockingSeekDecoder{fakeDecoder: *d.(*fakeDecoder), release: release, seeks: &seeks, last: &last}, nil
	}
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: open, ChunkFrames: 256, Poll: time.Millisecond})
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(480000, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	start := time.Now()
	for i := 1; i <= 200; i++ { // far more than the 64-slot command queue
		e.Seek(1, time.Duration(i)*10*time.Millisecond)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("200 seeks took %v; Seek must not block", d)
	}
	waitFor(t, "first seek running", func() bool { return seeks.Load() >= 1 })
	close(release)
	waitFor(t, "latest seek applied", func() bool { return last.Load() == uint64(2*OutputRate) })
	if n := seeks.Load(); n > 2 {
		t.Fatalf("%d decoder seeks for a burst of 200; want at most 2 (running + latest)", n)
	}
}

func TestEngineSeekForNewTrackIsNotRetargeted(t *testing.T) {
	out := newFakeOutput(1000)
	release := make(chan struct{})
	var seeks atomic.Int32
	var last atomic.Uint64
	open := func(src io.ReadSeeker, f Format) (Decoder, error) {
		d, err := fakeOpen(src, f)
		if err != nil {
			return nil, err
		}
		return &blockingSeekDecoder{fakeDecoder: *d.(*fakeDecoder), release: release, seeks: &seeks, last: &last}, nil
	}
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: open, ChunkFrames: 256, Poll: time.Millisecond})
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(480000, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	e.Seek(1, time.Second) // runs and blocks in SeekFrame
	waitFor(t, "first seek running", func() bool { return seeks.Load() >= 1 })
	e.Seek(1, 2*time.Second) // queues its own closure
	e.Play(Track{ID: 2, Source: newFakeSource(ramp(480000, 0), OutputRate)})
	e.Seek(2, 3*time.Second) // must not coalesce into the queued track-1 request
	close(release)

	var seen []Event
	deadline := time.After(2 * time.Second)
	for started := false; !started; {
		out.consume(1 << 30)
		select {
		case ev := <-e.Events():
			seen = append(seen, ev)
			started = ev.Kind == EventStarted && ev.TrackID == 2
		case <-time.After(time.Millisecond):
		case <-deadline:
			t.Fatalf("Started(2) never arrived; events: %+v", seen)
		}
	}
	waitFor(t, "track 2 sought to 3 s", func() bool { return last.Load() == uint64(3*OutputRate) })
	quiet := time.After(100 * time.Millisecond)
	for {
		select {
		case ev := <-e.Events():
			if ev.Kind == EventSeekFailed && ev.TrackID == 2 {
				t.Fatalf("spurious SeekFailed for track 2: %v", ev.Err)
			}
		case <-quiet:
			return
		}
	}
}

func TestEngineSeekRightAfterPlayStillAnnounces(t *testing.T) {
	out := newFakeOutput(1 << 16)
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: 100 * time.Millisecond})
	defer e.Close()
	e.Play(Track{ID: 5, Source: newFakeSource(ramp(48000, 0), OutputRate)})
	// A slow monitor tick lets the seek run before the boundary is announced.
	e.Seek(5, 500*time.Millisecond) // the player's resume path: no wait in between

	started := 0
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case ev := <-e.Events():
			switch {
			case ev.Kind == EventStarted && ev.TrackID == 5:
				started++
			case ev.Kind == EventSeekFailed:
				t.Fatalf("unexpected SeekFailed: %+v", ev)
			}
		case <-deadline:
			break loop
		}
	}
	if started != 1 {
		t.Fatalf("Started(5) fired %d times, want exactly 1", started)
	}
	id, pos, ok := e.Position()
	if !ok || id != 5 || pos < 490*time.Millisecond || pos > 700*time.Millisecond {
		t.Fatalf("Position = %d %v %v, want track 5 near 500ms", id, pos, ok)
	}
}

func TestEngineSeekOfGaplessSuccessorAnnounces(t *testing.T) {
	out := newFakeOutput(1000)
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(300, 0), OutputRate)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(ramp(48000, 300), OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	waitFor(t, "B being decoded", func() bool { return len(out.written())/2 > 300 })

	e.Seek(2, 100*time.Millisecond)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	deadline := time.After(300 * time.Millisecond)
	for {
		out.consume(1 << 30)
		select {
		case ev := <-e.Events():
			// Ended(2) is fine: track 2 plays out once consumed.
			if ev.Kind == EventStarted || ev.Kind == EventSeekFailed || (ev.Kind == EventEnded && ev.TrackID == 1) {
				t.Fatalf("duplicate or unexpected event %+v", ev)
			}
		case <-time.After(time.Millisecond):
		case <-deadline:
			return
		}
	}
}

// loggedSeekDecoder blocks its first seek (if gate is set) and records which
// decoder instance (gen) was sought to which frame.
type loggedSeekDecoder struct {
	fakeDecoder
	gen  int
	gate chan struct{}
	mu   *sync.Mutex
	log  *[]string
}

func (d *loggedSeekDecoder) SeekFrame(f uint64) error {
	if d.gate != nil {
		<-d.gate
	}
	d.mu.Lock()
	*d.log = append(*d.log, fmt.Sprintf("gen%d@%d", d.gen, f))
	d.mu.Unlock()
	return d.fakeDecoder.SeekFrame(f)
}

// A seek queued before Play(id) must not absorb a seek issued after Play of
// the same id (the player's resume path): that one has to run on the new
// decoder, behind the Play.
func TestEngineSeekAfterPlayOfSameTrackQueuesBehindPlay(t *testing.T) {
	out := newFakeOutput(1000)
	gate := make(chan struct{})
	var mu sync.Mutex
	var log []string
	gen := 0
	open := func(src io.ReadSeeker, f Format) (Decoder, error) {
		d, err := fakeOpen(src, f)
		if err != nil {
			return nil, err
		}
		gen++
		var g chan struct{}
		if gen == 1 {
			g = gate
		}
		return &loggedSeekDecoder{fakeDecoder: *d.(*fakeDecoder), gen: gen, gate: g, mu: &mu, log: &log}, nil
	}
	e := NewEngine(EngineOptions{Output: out, OpenDecoder: open, ChunkFrames: 256, Poll: time.Millisecond})
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(480000, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	e.Seek(1, time.Second) // runs on gen 1 and blocks
	time.Sleep(20 * time.Millisecond)
	e.Seek(1, 2*time.Second) // queued behind it
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(480000, 0), OutputRate)})
	e.Seek(1, 3*time.Second) // must run after the Play, on gen 2
	close(gate)

	want := fmt.Sprintf("gen2@%d", 3*OutputRate)
	waitFor(t, "resume seek on the new decoder", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range log {
			if s == want {
				return true
			}
		}
		return false
	})
}
