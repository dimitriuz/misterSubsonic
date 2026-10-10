package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

const testDAC = "hw:CARD=DAC,DEV=0"

func newRateEngine(out *fakeOutput) *Engine {
	return NewEngine(EngineOptions{Output: out, OutputName: testDAC, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
}

func TestEngineRateChangeWaitsForTheOldSongToPlayOut(t *testing.T) {
	out := newNativeOutput(100000, 44100, 48000, 96000)
	e := newRateEngine(out)
	defer e.Close()

	a, b := ramp(700, 0), ramp(900, 700)
	e.Play(Track{ID: 1, Source: newFakeSource(a, 44100)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, 96000)})
	waitFor(t, "the first song written", func() bool { return len(out.written())/2 == 700 })
	if r := out.reopenCalls(); len(r) != 1 || r[0].rate != 44100 || r[0].name != testDAC {
		t.Fatalf("reopens %+v, want one at 44100 before the first song", r)
	}
	expectEvent(t, e, EventStarted, 1)

	out.consume(350)
	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 350*time.Second/44100 {
		t.Fatalf("position %v %v %v, want song 1 at 350 frames of 44.1 kHz", id, pos, ok)
	}
	out.consume(349)
	time.Sleep(20 * time.Millisecond)
	if r := out.reopenCalls(); len(r) != 1 {
		t.Fatalf("reopened with a frame of the old song still to play: %+v", r)
	}
	out.consume(1)
	waitFor(t, "the reopen at 96 kHz", func() bool { return len(out.reopenCalls()) == 2 })
	r := out.reopenCalls()[1]
	if r.rate != 96000 || r.written != 700 || r.consumed != 700 {
		t.Fatalf("reopen %+v, want 96000 once all 700 frames played", r)
	}
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	waitFor(t, "the second song written", func() bool { return len(out.written())/2 == 1600 })
	out.consume(450)
	if id, pos, ok := e.Position(); !ok || id != 2 || pos != 450*time.Second/96000 {
		t.Fatalf("position %v %v %v, want song 2 at 450 frames of 96 kHz", id, pos, ok)
	}
	playOut(t, out, 1600)
	got := out.written()
	want := append(append([]float32(nil), a...), b...)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v: native rates must not be resampled", i, got[i], want[i])
		}
	}
}

func TestEngineSameNativeRateStaysGapless(t *testing.T) {
	out := newNativeOutput(100000, 48000, 96000)
	e := newRateEngine(out)
	defer e.Close()

	a, b := ramp(700, 0), ramp(900, 700)
	e.Play(Track{ID: 1, Source: newFakeSource(a, 96000)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, 96000)})
	waitFor(t, "both songs written", func() bool { return len(out.written())/2 == 1600 })
	if r := out.reopenCalls(); len(r) != 1 || r[0].rate != 96000 {
		t.Fatalf("reopens %+v, want just the one at 96000", r)
	}
	expectEvent(t, e, EventStarted, 1)
	out.consume(700)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)
}

func TestEngineStopDuringARateChangeDrainCancelsIt(t *testing.T) {
	out := newNativeOutput(100000, 44100, 96000)
	e := newRateEngine(out)
	defer e.Close()

	src2 := newFakeSource(ramp(900, 0), 96000)
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(700, 0), 44100)})
	e.QueueNext(Track{ID: 2, Source: src2})
	waitFor(t, "the first song written", func() bool { return len(out.written())/2 == 700 })
	time.Sleep(10 * time.Millisecond) // the engine now waits for the old song to play out
	e.Stop()
	waitFor(t, "the queued song closed", src2.isClosed)
	time.Sleep(10 * time.Millisecond)
	if r := out.reopenCalls(); len(r) != 1 {
		t.Fatalf("reopens %+v: a stopped drain must not reopen", r)
	}
}

func TestEngineRateTheDeviceLacksIsResampledTo48k(t *testing.T) {
	out := newNativeOutput(100000, 44100, 48000)
	e := newRateEngine(out)
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(2205, 0), 22050)})
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 4700)
	if r := out.reopenCalls(); len(r) != 0 {
		t.Fatalf("reopens %+v, want none: the device already runs at 48 kHz", r)
	}
	if n := len(out.written()) / 2; n < 4700 || n > 4900 {
		t.Fatalf("%d frames for 0.1 s, want about 4800 (resampled to 48 kHz)", n)
	}
}

func TestEngineRateRefusedAtReopenFallsBackTo48k(t *testing.T) {
	out := newNativeOutput(100000, 44100, 48000, 88200)
	out.rate = 44100
	out.refuse = []int{88200}
	e := newRateEngine(out)
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(882, 0), 88200)})
	expectEvent(t, e, EventStarted, 1)
	r := out.reopenCalls()
	if len(r) != 2 || r[0].rate != 88200 || r[1].rate != 48000 {
		t.Fatalf("reopens %+v, want 88200 refused then 48000", r)
	}
	playOut(t, out, 470)
	if out.Supports(88200) {
		t.Error("a refused rate is still reported as supported")
	}
}

func TestEngineReopenFailureHoldsTheSongForSwitchOutput(t *testing.T) {
	out := newNativeOutput(100000, 44100, 48000)
	out.reopenErr = errors.New("unplugged")
	e := newRateEngine(out)
	defer e.Close()

	src := newFakeSource(ramp(441, 0), 44100)
	e.Play(Track{ID: 1, Source: src})
	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost {
		t.Fatalf("event %+v, want EventDeviceLost", ev)
	}
	if src.isClosed() {
		t.Fatal("the song was dropped; it must wait for a device")
	}
	if err := e.SwitchOutput("default"); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 470) // 441 frames at 44.1 kHz, resampled to 48 kHz on the default device
	expectEvent(t, e, EventEnded, 1)
}

func TestEngineReportsALostDeviceOncePerLoss(t *testing.T) {
	out := newFakeOutput(1000)
	e := newTestEngine(out)
	defer e.Close()

	out.setLost()
	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost || ev.TrackID != 0 {
		t.Fatalf("event %+v, want EventDeviceLost", ev)
	}
	noEvent(t, e, 20*time.Millisecond)
	if err := e.SwitchOutput("default"); err != nil {
		t.Fatal(err)
	}
	noEvent(t, e, 20*time.Millisecond)
	out.setLost()
	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost {
		t.Fatalf("event %+v, want a second EventDeviceLost", ev)
	}
}

func TestEngineSwitchOutputCarriesOnFromWhatWasHeard(t *testing.T) {
	out := newNativeOutput(2000, 48000)
	e := newRateEngine(out)
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(10000, 0), 48000)})
	expectEvent(t, e, EventStarted, 1)
	waitFor(t, "the ring full", func() bool { return len(out.written())/2 == 2000 })
	out.consume(500)
	if err := e.SwitchOutput("default"); err != nil {
		t.Fatal(err)
	}
	r := out.reopenCalls()
	if len(r) != 1 || r[0].name != "default" || r[0].rate != 48000 {
		t.Fatalf("reopens %+v, want default at 48000", r)
	}
	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 500*time.Second/48000 {
		t.Fatalf("position %v %v %v, want song 1 at frame 500", id, pos, ok)
	}
	cut := int(r[0].written)
	waitFor(t, "more written after the switch", func() bool { return len(out.written())/2 > cut+10 })
	if got, want := out.written()[2*cut], ramp(1, 500)[0]; got != want {
		t.Fatalf("after the switch the song resumed at %v, want frame 500 (%v)", got, want)
	}
}

func TestEngineSwitchOutputFailureKeepsThePreviousDevice(t *testing.T) {
	out := newNativeOutput(2000, 48000)
	e := newRateEngine(out)
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(10000, 0), 48000)})
	expectEvent(t, e, EventStarted, 1)
	out.consume(100)
	out.reopenErr = errors.New("busy")
	if err := e.SwitchOutput("hw:CARD=Other,DEV=0"); err == nil {
		t.Fatal("no error from a device that won't open")
	}
	r := out.reopenCalls()
	if len(r) != 2 || r[1].name != testDAC {
		t.Fatalf("reopens %+v, want the failed one then %s again", r, testDAC)
	}
	playOut(t, out, 3000) // still playing
}

func TestEngineSwitchOutputAnnouncesTheTrackChangeOfAGaplessSuccessor(t *testing.T) {
	out := newNativeOutput(2000, 48000)
	e := newRateEngine(out)
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(700, 0), 48000)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(ramp(10000, 700), 48000)})
	waitFor(t, "the ring full", func() bool { return len(out.written())/2 == 2000 })
	expectEvent(t, e, EventStarted, 1)
	out.consume(100) // song 1 is still audible, song 2 is already in the ring
	if err := e.SwitchOutput("default"); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)
	if id, _, ok := e.Position(); !ok || id != 2 {
		t.Fatalf("position of song %d (%v), want song 2", id, ok)
	}
	noEvent(t, e, 20*time.Millisecond)
}

// downOutput is a fakeOutput whose next fails Reopens all fail.
type downOutput struct {
	*fakeOutput
	mu    sync.Mutex
	fails int
}

func (d *downOutput) Reopen(name string, rate int) error {
	d.mu.Lock()
	failing := d.fails > 0
	if failing {
		d.fails--
	}
	d.mu.Unlock()
	if failing {
		d.fakeOutput.reopenErr = errors.New("gone")
	}
	return d.fakeOutput.Reopen(name, rate)
}

func TestEngineSwitchOutputThatFailsEverywhereResumesWhereItWasHeard(t *testing.T) {
	out := &downOutput{fakeOutput: newNativeOutput(2000, 48000)}
	e := NewEngine(EngineOptions{Output: out, OutputName: testDAC, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
	defer e.Close()

	e.Play(Track{ID: 1, Source: newFakeSource(ramp(10000, 0), 48000)})
	expectEvent(t, e, EventStarted, 1)
	waitFor(t, "the ring full", func() bool { return len(out.written())/2 == 2000 })
	out.consume(500)
	out.mu.Lock()
	out.fails = 2 // the new device and the previous one
	out.mu.Unlock()
	if err := e.SwitchOutput("default"); err == nil {
		t.Fatal("no error though no device opened")
	}
	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 500*time.Second/48000 {
		t.Fatalf("held position %v %v %v, want song 1 at frame 500", id, pos, ok)
	}
	cut := len(out.written()) / 2
	if err := e.SwitchOutput(testDAC); err != nil {
		t.Fatal(err)
	}
	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 500*time.Second/48000 {
		t.Fatalf("position %v %v %v, want song 1 at frame 500", id, pos, ok)
	}
	waitFor(t, "more written after the switch", func() bool { return len(out.written())/2 > cut+10 })
	if got, want := out.written()[2*cut], ramp(1, 500)[0]; got != want {
		t.Fatalf("after the switch the song resumed at %v, want frame 500 (%v)", got, want)
	}
	noEvent(t, e, 20*time.Millisecond) // the song never ended or started again
}
