package audio

import (
	"sync"
	"testing"
	"time"
)

// stereo makes n frames whose left sample is first, first+1, ... and whose
// right sample is the negative of it.
func stereo(first, n int) []float32 {
	s := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		s[2*i], s[2*i+1] = float32(first+i), -float32(first+i)
	}
	return s
}

// lefts returns the left samples of interleaved stereo.
func lefts(s []float32) []int {
	var l []int
	for i := 0; i < len(s); i += 2 {
		l = append(l, int(s[i]))
	}
	return l
}

type tapClock struct{ t time.Time }

func (c *tapClock) now() time.Time { return c.t }

// newTestTap returns a tap on a fake output (ring ringFrames) with a clock
// the test moves; the tap starts out being read.
func newTestTap(ringFrames, tapFrames int) (*Tap, *fakeOutput, *tapClock) {
	out := newFakeOutput(ringFrames)
	clk := &tapClock{t: time.Unix(1000, 0)}
	tp := NewTap(out, tapFrames)
	tp.now = clk.now
	tp.Window(make([]float32, 2)) // mark it read
	return tp, out, clk
}

func TestTapKeepsOnlyTheFramesTheDeviceTook(t *testing.T) {
	tp, out, _ := newTestTap(10, 64)
	if n := tp.Write(stereo(1, 25)); n != 10 {
		t.Fatalf("Write took %d, want 10", n)
	}
	out.consume(10)
	dst := make([]float32, 2*64)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if len(got) != 10 || got[0] != 1 || got[9] != 10 {
		t.Fatalf("window = %v, want 1..10", got)
	}
}

func TestTapWindowEndsAtConsumed(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 50))
	out.consume(30)
	dst := make([]float32, 2*8)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if n != 8 || got[0] != 23 || got[7] != 30 {
		t.Fatalf("window = %v, want 23..30", got)
	}
	if dst[1] != -23 {
		t.Fatalf("right channel = %v, want -23", dst[1])
	}
}

func TestTapRingWraps(t *testing.T) {
	tp, out, _ := newTestTap(100, 16)
	for i := 0; i < 5; i++ { // 5*7 = 35 frames through a 16-frame ring
		tp.Write(stereo(1+i*7, 7))
	}
	out.consume(35)
	dst := make([]float32, 2*10)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if n != 10 || got[0] != 26 || got[9] != 35 {
		t.Fatalf("window = %v, want 26..35", got)
	}
	// Asking for more than the ring holds gives the ring.
	big := make([]float32, 2*40)
	if n = tp.Window(big); n != 16 || lefts(big[:2*n])[0] != 20 {
		t.Fatalf("big window n=%d first=%d, want 16 from 20", n, lefts(big[:2*n])[0])
	}
}

func TestTapUnplayedFramesAreSkipped(t *testing.T) {
	tp, out, _ := newTestTap(100, 16)
	tp.Write(stereo(1, 40))
	out.consume(40)
	tp.Write(stereo(41, 10)) // sit in the device ring, not heard yet
	dst := make([]float32, 2*4)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 4 || got[3] != 40 {
		t.Fatalf("window = %v, want 37..40", got)
	}
}

func TestTapPauseKeepsTheWindow(t *testing.T) {
	tp, out, clk := newTestTap(100, 128)
	tp.Write(stereo(1, 100))
	out.consume(40)
	tp.Write(stereo(101, 40))       // the device ring is full again
	tp.Window(make([]float32, 2*4)) // read once, then paused
	tp.SetPaused(true)
	for i := 0; i < 8; i++ { // 2 s of polling; the device takes nothing
		clk.t = clk.t.Add(250 * time.Millisecond)
		if n := tp.Write(stereo(101, 10)); n != 0 {
			t.Fatalf("paused Write took %d, want 0", n)
		}
	}
	tp.SetPaused(false)
	out.consume(20)
	dst := make([]float32, 2*16)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 16 || got[0] != 45 || got[15] != 60 {
		t.Fatalf("window = %v, want 45..60", got)
	}
	// An early refill write after the resume is kept too.
	clk.t = clk.t.Add(100 * time.Millisecond)
	tp.Write(stereo(141, 20))
	out.consume(20)
	n = tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 16 || got[0] != 65 || got[15] != 80 {
		t.Fatalf("after refill window = %v, want 65..80", got)
	}
}

func TestTapShortRingReturnsFewer(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 5))
	out.consume(5)
	dst := make([]float32, 2*32)
	if n := tp.Window(dst); n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
}

func TestTapFlushClears(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 20))
	out.consume(20)
	tp.Flush()
	if out.flushes != 1 {
		t.Fatal("Flush not passed through")
	}
	dst := make([]float32, 2*8)
	if n := tp.Window(dst); n != 0 {
		t.Fatalf("after Flush n = %d, want 0", n)
	}
	tp.Write(stereo(100, 3))
	out.consume(3)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 3 || got[0] != 100 {
		t.Fatalf("after new writes window = %v, want 100..102", got)
	}
}

func TestTapIdleSkipsTheCopyAndResumes(t *testing.T) {
	tp, out, clk := newTestTap(1000, 64)
	dst := make([]float32, 2*8)
	tp.Write(stereo(1, 10))
	out.consume(10)
	if n := tp.Window(dst); n != 8 { // reads at t0
		t.Fatalf("n = %d, want 8", n)
	}
	clk.t = clk.t.Add(1500 * time.Millisecond) // nobody reads for over 1 s
	tp.Write(stereo(11, 10))
	out.consume(10)
	if n := tp.Window(dst); n != 0 { // this read wakes it; nothing was copied
		t.Fatalf("idle window n = %d, want 0", n)
	}
	tp.Write(stereo(21, 10))
	out.consume(10)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 8 || got[0] != 23 || got[7] != 30 {
		t.Fatalf("resumed window = %v, want 23..30", got)
	}
	// Reading keeps it awake: 0.9 s later is still not idle.
	clk.t = clk.t.Add(900 * time.Millisecond)
	tp.Write(stereo(31, 4))
	out.consume(4)
	if n = tp.Window(dst); n != 8 || lefts(dst[:2*n])[7] != 34 {
		t.Fatalf("window after a short gap n = %d", n)
	}
}

func TestTapPassesThrough(t *testing.T) {
	tp, out, _ := newTestTap(10, 16)
	tp.SetPaused(true)
	tp.SetVolume(0.5)
	tp.Write(stereo(1, 4))
	out.consume(3)
	if tp.Consumed() != 3 {
		t.Fatalf("Consumed = %d, want 3", tp.Consumed())
	}
	if err := tp.Close(); err != nil {
		t.Fatal(err)
	}
}

// A writer and a reader at once; run under -race.
func TestTapConcurrentWriteAndWindow(t *testing.T) {
	out := newFakeOutput(1 << 30)
	tp := NewTap(out, 256)
	tp.Window(make([]float32, 2)) // a reader is present, so Write copies
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 2000; i++ {
			tp.Write(stereo(i*16, 16))
			out.consume(16)
		}
	}()
	go func() {
		defer wg.Done()
		dst := make([]float32, 2*64)
		for {
			select {
			case <-stop:
				return
			default:
				n := tp.Window(dst)
				for i := 1; i < n; i++ { // frames are consecutive
					if dst[2*i] != dst[2*i-2]+1 {
						t.Errorf("window not consecutive at %d", i)
						return
					}
				}
			}
		}
	}()
	wg.Wait()
}

// A write bigger than the tap returns what the device took, and the tap keeps
// the newest frames.
func TestTapOversizeWriteReturnsDeviceCount(t *testing.T) {
	tp, out, _ := newTestTap(100, 16)
	if n := tp.Write(stereo(1, 40)); n != 40 {
		t.Fatalf("Write returned %d, want the device's 40", n)
	}
	out.consume(40)
	dst := make([]float32, 2*16)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if n != 16 || got[0] != 25 || got[15] != 40 {
		t.Fatalf("window = %v, want 25..40", got)
	}
}
