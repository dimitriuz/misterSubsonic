package audio

import (
	"bytes"
	"os"
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

	if err := e.Seek(7, 2*time.Second+500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if out.flushes < 2 { // one from Play, one from Seek
		t.Fatalf("Seek did not flush the output (flushes=%d)", out.flushes)
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
	if err := e.Seek(99, time.Second); err != ErrNotCurrent {
		t.Fatalf("err = %v, want ErrNotCurrent", err)
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

	a := ramp(200, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	// Queue B with openBlock set (never released) so finishCur waits
	b := newFakeSource(ramp(300, 200), OutputRate)
	b.openBlock = make(chan struct{})
	e.QueueNext(Track{ID: 2, Source: b})

	// Wait until A is fully written (finishCur is now waiting for B to open)
	waitFor(t, "a fully written", func() bool { return len(out.written())/2 == 200 })

	// Stop while finishCur is waiting
	e.Stop()

	// Queue C (normal, no block)
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
	e.Play(Track{ID: 1, Source: a})

	// Play B immediately (interrupts A's open)
	time.Sleep(10 * time.Millisecond) // Give A's open a chance to start blocking
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

