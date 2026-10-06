package ui

import (
	"testing"
	"unsafe"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// fbSim is a 32 bpp framebuffer: packs like fbFormat.packRect (copied from
// gfx/fbpack.go, little-endian XRGB8888 path).
type fbSim struct {
	w, h, stride int
	mem          []byte
	rects, px    int // since reset
	fullPresents int
	partials     int
	maxRect      gfx.Rect
	last         []gfx.Rect
}

func newFBSim(w, h int) *fbSim { return &fbSim{w: w, h: h, stride: w * 4, mem: make([]byte, w*h*4)} }

func (f *fbSim) Size() (int, int) { return f.w, f.h }
func (f *fbSim) Close() error     { return nil }
func (f *fbSim) packRect(c *gfx.Canvas, r gfx.Rect) {
	r = r.Intersect(gfx.Rect{X: 0, Y: 0, W: f.w, H: f.h})
	if r.Empty() {
		return
	}
	for y := r.Y; y < r.Bottom(); y++ {
		src := c.Pix[y*c.W+r.X : y*c.W+r.Right()]
		line := f.mem[y*f.stride+r.X*4:]
		copy(line[:4*r.W], unsafe.Slice((*byte)(unsafe.Pointer(&src[0])), 4*r.W))
	}
}
func (f *fbSim) Present(c *gfx.Canvas) error {
	f.fullPresents++
	f.rects++
	f.px += f.w * f.h
	f.last = append(f.last[:0], gfx.Rect{X: 0, Y: 0, W: f.w, H: f.h})
	f.packRect(c, gfx.Rect{X: 0, Y: 0, W: f.w, H: f.h})
	return nil
}
func (f *fbSim) PresentRects(c *gfx.Canvas, rs []gfx.Rect) error {
	f.partials++
	f.last = append(f.last[:0], rs...)
	for _, r := range rs {
		f.rects++
		f.px += r.W * r.H
		f.packRect(c, r)
	}
	return nil
}

var _ gfx.PartialPresenter = (*fbSim)(nil)

// cycleVisual hands out a few different windows in turn, so the levels move
// from frame to frame like music (made once: making sines is not what the
// benchmark measures).
type cycleVisual struct {
	wins [][]float32
	i    int
}

func newCycleVisual(n int) *cycleVisual {
	c := &cycleVisual{}
	f := &fakeVisual{}
	for k := 0; k < 12; k++ {
		w := make([]float32, n)
		f.Window(w)
		c.wins = append(c.wins, w)
	}
	return c
}

func (c *cycleVisual) Window(dst []float32) int {
	w := c.wins[c.i%len(c.wins)]
	c.i++
	copy(dst, w)
	return min(len(dst), len(w)) / 2
}

// BenchmarkVizRealFrame is one visualizer frame as the app runs it: advance
// the clock, onWake (vizTick, progress damage), render (renderDamage,
// drawFrame per rect, presentRects), into a simulated framebuffer.
func BenchmarkVizRealFrame(b *testing.B) {
	type c struct {
		name  string
		w, h  int
		crt   bool
		full  bool
		style VizStyle
	}
	var cases []c
	for _, full := range []bool{false, true} {
		n := "panel"
		if full {
			n = "full"
		}
		for style := VizBars; style <= VizWaterfall; style++ {
			cases = append(cases, c{n + "-1920x1200/" + style.String(), 1920, 1200, false, full, style})
		}
	}
	cases = append(cases, c{"panel-1280x720/bars", 1280, 720, false, false, VizBars},
		c{"crt-320x240/bars", 320, 240, true, false, VizBars})
	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			t := &testing.T{}
			prof := PickProfile(cs.w, cs.h, "auto")
			if cs.crt {
				prof = ProfileCRT240
			}
			ta := vizApp(t, prof, cs.style)
			if cs.full {
				ta.press(input.BtnStart)
			}
			ta.verify = false
			ta.o.Visual = newCycleVisual(4096)
			fb := newFBSim(prof.W, prof.H)
			ta.o.Display = fb
			runFrames(t, ta, 3)
			if ta.scaler != nil {
				b.Logf("scaler in use (profile %dx%d)", prof.W, prof.H)
			}
			step := func() (full bool) {
				d := ta.untilWake()
				ta.now = ta.now.Add(d)
				ta.pl.st.Position += d
				ta.onWake()
				full = ta.dirty
				if ta.redrawDue() { // the app's loop renders only then
					if err := ta.render(); err != nil {
						b.Fatal(err)
					}
				}
				return
			}
			for i := 0; i < 5; i++ {
				step()
			}
			*fb = fbSim{w: fb.w, h: fb.h, stride: fb.stride, mem: fb.mem}
			fulls := 0
			var first []gfx.Rect
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if step() {
					fulls++
				}
				if i == 0 {
					first = append(first, fb.last...)
				}
			}
			b.StopTimer()
			n := float64(b.N)
			b.ReportMetric(float64(fb.rects)/n, "rects/op")
			b.ReportMetric(float64(fb.px)/n, "px/op")
			b.ReportMetric(float64(fulls), "fullframes")
			b.Logf("viz rect %v, ticks %v, first frame rects %v, fullPresents %d partials %d, fps %d", ta.viz.rect, ta.ticks, first, fb.fullPresents, fb.partials, ta.vizFPS())
		})
	}
}
