package audio

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOutput is a ring whose consumption the test controls.
type fakeOutput struct {
	mu       sync.Mutex
	ring     int
	data     []float32 // everything ever written
	consumed uint64
	flushes  int
	onFlush  func() // optional callback called after Flush completes (outside mu)
}

func newFakeOutput(ringFrames int) *fakeOutput { return &fakeOutput{ring: ringFrames} }

func (f *fakeOutput) Write(frames []float32) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	space := f.ring - int(uint64(len(f.data)/2)-f.consumed)
	n := min(space, len(frames)/2)
	f.data = append(f.data, frames[:n*2]...)
	return n
}

func (f *fakeOutput) Consumed() uint64  { f.mu.Lock(); defer f.mu.Unlock(); return f.consumed }
func (f *fakeOutput) SetPaused(bool)    {}
func (f *fakeOutput) SetVolume(float32) {}
func (f *fakeOutput) Close() error      { return nil }

func (f *fakeOutput) Flush() {
	f.mu.Lock()
	f.consumed = uint64(len(f.data) / 2)
	f.flushes++
	f.mu.Unlock()
	if f.onFlush != nil {
		f.onFlush()
	}
}

// consume plays out up to n frames.
func (f *fakeOutput) consume(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	avail := uint64(len(f.data)/2) - f.consumed
	f.consumed += min(uint64(n), avail)
}

func (f *fakeOutput) written() []float32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]float32(nil), f.data...)
}

// fakeSource carries PCM for fakeDecoder. If block is set, reads wait on it
// until the source is closed. If openBlock is set, fakeOpen waits on it.
type fakeSource struct {
	pcm             []float32
	rate            int
	openErr         error
	block           chan struct{}
	readStarted     chan struct{} // closed once a Read starts waiting on block
	readStartedOnce sync.Once     // protects closing readStarted
	openBlock       chan struct{} // blocks fakeOpen if set
	openStarted     chan struct{} // closed when fakeOpen begins
	openStartedOnce sync.Once     // protects closing openStarted
	seekBlock       chan struct{} // blocks SeekFrame if set, until closed
	eofWithData     bool          // the last Read returns its frames together with io.EOF
	openHold        chan struct{} // fakeOpen waits for it even after the source is closed
	decClosed       atomic.Int32  // decoders of this source closed
	once            sync.Once
	closed          chan struct{}
}

func newFakeSource(pcm []float32, rate int) *fakeSource {
	return &fakeSource{pcm: pcm, rate: rate, closed: make(chan struct{})}
}

func (s *fakeSource) Read([]byte) (int, error)       { return 0, io.EOF }
func (s *fakeSource) Seek(int64, int) (int64, error) { return 0, nil }
func (s *fakeSource) Close() error                   { s.once.Do(func() { close(s.closed) }); return nil }
func (s *fakeSource) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

type fakeDecoder struct {
	src *fakeSource
	pos int
}

func fakeOpen(src io.ReadSeeker, _ Format) (Decoder, error) {
	s := src.(*fakeSource)
	if s.openStarted != nil {
		s.openStartedOnce.Do(func() { close(s.openStarted) })
	}
	if s.openErr != nil {
		return nil, s.openErr
	}
	if s.openBlock != nil {
		select {
		case <-s.openBlock:
		case <-s.closed:
			return nil, errClosed
		}
	}
	if s.openHold != nil {
		<-s.openHold // a slow open that notices nothing until it finishes
	}
	return &fakeDecoder{src: s}, nil
}

var errClosed = errors.New("source closed")

func (d *fakeDecoder) SampleRate() int      { return d.src.rate }
func (d *fakeDecoder) LengthFrames() uint64 { return uint64(len(d.src.pcm) / 2) }
func (d *fakeDecoder) Close() error         { d.src.decClosed.Add(1); return nil }
func (d *fakeDecoder) SeekFrame(f uint64) error {
	if d.src.seekBlock != nil {
		select {
		case <-d.src.seekBlock:
		case <-d.src.closed:
			return errClosed
		}
	}
	d.pos = int(f) * 2
	return nil
}
func (d *fakeDecoder) Read(dst []float32) (int, error) {
	if d.src.block != nil {
		if d.src.readStarted != nil {
			d.src.readStartedOnce.Do(func() { close(d.src.readStarted) })
		}
		select {
		case <-d.src.block:
		case <-d.src.closed:
			return 0, errClosed
		}
	}
	if d.src.isClosed() {
		return 0, errClosed
	}
	if d.pos >= len(d.src.pcm) {
		return 0, io.EOF
	}
	n := copy(dst, d.src.pcm[d.pos:])
	d.pos += n
	if d.src.eofWithData && d.pos >= len(d.src.pcm) {
		return n / 2, io.EOF // as a stream error arrives with the last frames
	}
	return n / 2, nil
}

// ramp returns frames whose left and right samples are start+i and -(start+i), scaled.
func ramp(frames int, start float32) []float32 {
	out := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		v := (start + float32(i)) / 1e6
		out[2*i], out[2*i+1] = v, -v
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// playOut consumes output until the engine has written want frames in total.
func playOut(t *testing.T, out *fakeOutput, want int) {
	t.Helper()
	waitFor(t, "output", func() bool {
		out.consume(1 << 30)
		return len(out.written())/2 >= want
	})
	out.consume(1 << 30)
}

func nextEvent(t *testing.T, e *Engine) Event {
	t.Helper()
	select {
	case ev := <-e.Events():
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for engine event")
	}
	return Event{}
}

func expectEvent(t *testing.T, e *Engine, kind EventKind, id uint64) Event {
	t.Helper()
	ev := nextEvent(t, e)
	if ev.Kind != kind || ev.TrackID != id {
		t.Fatalf("event = %+v, want kind %d track %d", ev, kind, id)
	}
	return ev
}

func noEvent(t *testing.T, e *Engine, d time.Duration) {
	t.Helper()
	select {
	case ev := <-e.Events():
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(d):
	}
}
