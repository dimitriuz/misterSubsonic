package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// TapFrames is the app's tap size at 48 kHz: about 0.68 s, which holds the
// device ring (up to 0.5 s not yet played) plus a window. At other rates the
// tap holds the same time.
const TapFrames = 32768

// tapIdle is how long without a Window call before Write stops copying.
const tapIdle = time.Second

// Tap wraps an Output and keeps the last frames the device took, so the
// visualizer can see what is playing now. Everything passes through.
type Tap struct {
	out Output
	now func() time.Time // the clock (tests set it)

	lastRead atomic.Int64 // when Window last ran, UnixNano
	pausedAt atomic.Int64 // when the pause began, UnixNano (0: not paused)

	mu      sync.Mutex
	base    int       // the size asked for, in frames at OutputRate
	ring    []float32 // interleaved stereo; frame i is at ring[2*(i%size)]
	size    int       // frames
	rate    int       // the device rate the ring is sized for
	written uint64    // frames the device has taken, ever
	floor   uint64    // frames before this are not in the ring (Flush, idle)
}

// NewTap wraps out; frames is the ring size in stereo frames at OutputRate.
func NewTap(out Output, frames int) *Tap {
	t := &Tap{out: out, now: time.Now, base: frames}
	t.resize(out.Rate())
	return t
}

// resize sizes the ring for rate, the same time as base frames at
// OutputRate, and drops what it held. Callers hold mu (or own t).
func (t *Tap) resize(rate int) {
	size := int((int64(t.base)*int64(rate) + OutputRate - 1) / OutputRate)
	if size != t.size {
		t.ring, t.size = make([]float32, 2*size), size
	}
	t.rate = rate
	t.floor = t.written
}

// Decimation is the factor Window averages frames by at rate: 1 up to
// 48 kHz, 2 at 88.2 and 96, 4 at 176.4 and 192, and so on, so the
// visualizer always sees about 44.1 to 48 kHz.
func Decimation(rate int) int {
	d := 1
	for rate/(2*d) >= 44100 {
		d *= 2
	}
	return d
}

// Write passes frames to the device, then keeps the ones it took, unless
// nobody has looked for a second.
func (t *Tap) Write(frames []float32) int {
	took := t.out.Write(frames)
	n := took
	idle := t.now().UnixNano()-t.lastRead.Load() > int64(tapIdle)
	t.mu.Lock()
	if idle {
		if n > 0 { // a paused device takes nothing: what it holds is still to be heard
			t.written += uint64(n)
			t.floor = t.written
		}
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
	return took
}

// Window fills dst (interleaved stereo, len(dst)/2 frames wanted) with the
// latest frames that end at the device's Consumed position, the ones playing
// now, and returns how many it filled and their rate. That is fewer frames
// when the tap doesn't hold that many, for example right after Flush. Above
// 48 kHz each frame is the mean of Decimation(rate) device frames, and rate
// is the device rate divided by that.
func (t *Tap) Window(dst []float32) (n, rate int) {
	t.lastRead.Store(t.now().UnixNano())
	end := t.out.Consumed()
	t.mu.Lock()
	defer t.mu.Unlock()
	d := Decimation(t.rate)
	want := min(len(dst)/2, t.size/d) * d // device frames
	end = min(end, t.written)
	start := max(t.floor, t.written-min(t.written, uint64(t.size)))
	if end < uint64(want) || end-uint64(want) < start {
		if end <= start {
			return 0, t.rate / d
		}
		want = int(end-start) / d * d
	}
	from := end - uint64(want)
	if d == 1 {
		pos := int(from % uint64(t.size))
		first := min(want, t.size-pos)
		copy(dst, t.ring[2*pos:2*(pos+first)])
		copy(dst[2*first:], t.ring[:2*(want-first)])
		return want, t.rate
	}
	inv := 1 / float32(d)
	for i := 0; i < want/d; i++ {
		var l, r float32
		for j := 0; j < d; j++ {
			k := 2 * int((from+uint64(i*d+j))%uint64(t.size))
			l += t.ring[k]
			r += t.ring[k+1]
		}
		dst[2*i], dst[2*i+1] = l*inv, r*inv
	}
	return want / d, t.rate / d
}

func (t *Tap) Consumed() uint64    { return t.out.Consumed() }
func (t *Tap) Rate() int           { return t.out.Rate() }
func (t *Tap) Supports(r int) bool { return t.out.Supports(r) }
func (t *Tap) Lost() bool          { return t.out.Lost() }
func (t *Tap) SetVolume(v float32) { t.out.SetVolume(v) }
func (t *Tap) Close() error        { return t.out.Close() }

// SetPaused passes the pause on. A pause doesn't count as idle time for a tap
// that was being read when it began, so the picture is there on resume.
func (t *Tap) SetPaused(p bool) {
	t.out.SetPaused(p)
	now := t.now().UnixNano()
	if p {
		t.pausedAt.CompareAndSwap(0, now)
		return
	}
	if at := t.pausedAt.Swap(0); at != 0 && at-t.lastRead.Load() <= int64(tapIdle) {
		t.lastRead.Store(now)
	}
}

// Reopen passes the reopen on; the ring is sized for the new rate, and what
// it kept is dropped.
func (t *Tap) Reopen(name string, rate int) error {
	err := t.out.Reopen(name, rate)
	t.mu.Lock()
	t.resize(t.out.Rate())
	t.mu.Unlock()
	return err
}

// Flush discards what the device buffered and what the tap kept.
func (t *Tap) Flush() {
	t.out.Flush()
	t.mu.Lock()
	t.floor = t.written
	t.mu.Unlock()
}
