package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// TapFrames is the app's tap size: about 0.68 s, which holds the device ring
// (up to 0.5 s not yet played) plus a window.
const TapFrames = 32768

// tapIdle is how long without a Window call before Write stops copying.
const tapIdle = time.Second

// Tap wraps an Output and keeps the last frames the device took, so the
// visualizer can see what is playing now. Everything passes through.
type Tap struct {
	out Output
	now func() time.Time // the clock (tests set it)

	lastRead atomic.Int64 // when Window last ran, UnixNano

	mu      sync.Mutex
	ring    []float32 // interleaved stereo; frame i is at ring[2*(i%size)]
	size    int       // frames
	written uint64    // frames the device has taken, ever
	floor   uint64    // frames before this are not in the ring (Flush, idle)
}

// NewTap wraps out; frames is the ring size in stereo frames.
func NewTap(out Output, frames int) *Tap {
	return &Tap{out: out, now: time.Now, ring: make([]float32, 2*frames), size: frames}
}

// Write passes frames to the device, then keeps the ones it took, unless
// nobody has looked for a second.
func (t *Tap) Write(frames []float32) int {
	n := t.out.Write(frames)
	idle := t.now().UnixNano()-t.lastRead.Load() > int64(tapIdle)
	t.mu.Lock()
	if idle {
		t.written += uint64(n)
		t.floor = t.written
	} else {
		if n > t.size { // only the last size frames can matter
			frames = frames[2*(n-t.size):]
			t.written += uint64(n - t.size)
			n = t.size
		}
		pos := int(t.written % uint64(t.size))
		first := min(n, t.size-pos)
		copy(t.ring[2*pos:], frames[:2*first])
		copy(t.ring, frames[2*first:2*n])
		t.written += uint64(n)
	}
	t.mu.Unlock()
	return n
}

// Window fills dst (interleaved stereo, len(dst)/2 frames wanted) with the
// latest frames that end at the device's Consumed position, the ones playing
// now, and returns how many it filled. That is fewer when the tap doesn't
// hold that many, for example right after Flush.
func (t *Tap) Window(dst []float32) int {
	t.lastRead.Store(t.now().UnixNano())
	end := t.out.Consumed()
	want := min(len(dst)/2, t.size)
	t.mu.Lock()
	defer t.mu.Unlock()
	end = min(end, t.written)
	start := max(t.floor, t.written-min(t.written, uint64(t.size)))
	if end < uint64(want) || end-uint64(want) < start {
		if end <= start {
			return 0
		}
		want = int(end - start)
	}
	from := end - uint64(want)
	pos := int(from % uint64(t.size))
	first := min(want, t.size-pos)
	copy(dst, t.ring[2*pos:2*(pos+first)])
	copy(dst[2*first:], t.ring[:2*(want-first)])
	return want
}

func (t *Tap) Consumed() uint64    { return t.out.Consumed() }
func (t *Tap) SetPaused(p bool)    { t.out.SetPaused(p) }
func (t *Tap) SetVolume(v float32) { t.out.SetVolume(v) }
func (t *Tap) Close() error        { return t.out.Close() }

// Flush discards what the device buffered and what the tap kept.
func (t *Tap) Flush() {
	t.out.Flush()
	t.mu.Lock()
	t.floor = t.written
	t.mu.Unlock()
}
