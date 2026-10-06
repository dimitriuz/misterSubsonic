package ui

import (
	"testing"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/viz"
)

// waterfallApp is a waterfall that has run a frame, so the analyzer and the
// picture exist.
func waterfallApp(t *testing.T, prof Profile) *testApp {
	ta := vizApp(t, prof, VizWaterfall)
	ta.press(input.BtnStart) // the full screen: tall enough for a band to be many rows
	ta.settle(t)
	runFrames(t, ta, 3)
	return ta
}

func columnPixels(ta *testApp, x int) []uint32 {
	img := ta.viz.img
	out := make([]uint32, img.H)
	for y := range out {
		out[y] = img.Pix[y*img.W+x]
	}
	return out
}

// A column blends the two nearest bands in the palette index: a hot band
// between cold ones fades off above and below it, not a block per band.
func TestWaterfallColumnIsInterpolatedBetweenBands(t *testing.T) {
	ta := waterfallApp(t, PickProfile(1920, 1200, "auto"))
	v := &ta.viz
	col := v.an.Column()
	for i := range col {
		col[i] = 0
	}
	ta.sweepWaterfall() // the frame before: silence
	col[len(col)/2] = 1
	x := v.cursor
	a := ta.sweepWaterfall()
	if a.n < 1 {
		t.Fatalf("painted nothing: %+v", a)
	}
	px := columnPixels(ta, (x+a.n-1)%v.img.W) // the strip's last column is all this frame's
	distinct := map[uint32]bool{}
	for _, p := range px {
		distinct[p] = true
	}
	if len(distinct) < 30 {
		t.Errorf("%d distinct colours in a column with one hot band: blocks, not a blend", len(distinct))
	}
	// The hot band's centre is the palette's top; its neighbours are lower.
	hot := map[uint32]bool{} // the palette's top steps
	for _, c := range viz.Palette[250:] {
		hot[0xFF000000|c] = true
	}
	peak := 0
	for y, p := range px {
		if hot[p] {
			peak = y
			break
		}
	}
	if peak == 0 {
		t.Fatal("the hot band's centre is not in the picture")
	}
	if px[peak-v.img.H/len(col)/2] == px[peak] || px[peak+v.img.H/len(col)/2] == px[peak] {
		t.Error("the colour does not fall off beside the hot band's centre")
	}
}

// A strip wider than a pixel blends from the previous frame's spectrum to
// this one's, by each column's place in the strip.
func TestWaterfallStripBlendsFromThePreviousFrame(t *testing.T) {
	ta := waterfallApp(t, PickProfile(1920, 1200, "auto"))
	v := &ta.viz
	col := v.an.Column()
	for i := range col {
		col[i] = 0
	}
	ta.sweepWaterfall() // the previous frame: silence
	for i := range col {
		col[i] = 1
	}
	x := v.cursor
	st := ta.sweepWaterfall()
	if st.n < 3 {
		t.Fatalf("strip of %d columns: too narrow to test the blend", st.n)
	}
	row := v.img.H / 2
	for dx := 0; dx < st.n; dx++ {
		i := int(float64(255)*float64(dx+1)/float64(st.n) + 0.5)
		got := v.img.Pix[row*v.img.W+(x+dx)%v.img.W]
		want := 0xFF000000 | viz.Palette[i]
		if got != want {
			t.Errorf("column %d of %d: %08x, want palette[%d] = %08x", dx, st.n, got, i, want)
		}
	}
}

func TestWaterfallSweepAllocatesNothing(t *testing.T) {
	ta := waterfallApp(t, PickProfile(1920, 1200, "auto"))
	if n := testing.AllocsPerRun(20, func() { ta.sweepWaterfall() }); n != 0 {
		t.Errorf("%v allocations per sweep", n)
	}
}
