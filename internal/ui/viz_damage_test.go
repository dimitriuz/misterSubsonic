package ui

import (
	"bytes"
	"testing"
	"time"
	"unsafe"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// ownArea is the pixels the wake damaged in the visualizer's panel, its own
// areas and any other damage over it (overlaps counted twice: an upper bound).
func ownArea(ta *testApp) int {
	n := 0
	for _, r := range ta.viz.dmg {
		n += r.W * r.H
	}
	for _, r := range ta.damage {
		r = r.Intersect(ta.viz.rect)
		n += r.W * r.H
	}
	return n
}

// steadyApp is the visualizer in style at prof, over a signal that doesn't
// change, run until the levels and peaks have settled.
func steadyApp(t *testing.T, prof Profile, style VizStyle, full bool) *testApp {
	t.Helper()
	ta := vizApp(t, prof, style)
	if full {
		ta.press(input.BtnStart)
		ta.settle(t)
	}
	sig := make([]float32, 4096)
	(&fakeVisual{}).Window(sig)
	ta.o.Visual = &cachedVisual{sig}
	ta.verify = false
	runFrames(t, ta, 150) // the peak caps hold (0.4 s), then fall to the bars (0.5 s), and the VU peaks fall (15 dB/s)
	ta.verify = true
	return ta
}

func TestVizDamagesOnlyWhatChangedForSteadyMusic(t *testing.T) {
	for _, full := range []bool{false, true} {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := steadyApp(t, ProfileHDMI, style, full)
			screen := ta.P.W * ta.P.H
			worst := 0
			for i := 0; i < 30; i++ {
				ta.now = ta.now.Add(ta.untilWake())
				ta.onWake()
				worst = max(worst, ownArea(ta))
				if err := ta.render(); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("full=%v %v: worst frame damages %d px = %.2f%% of the screen, %.1f%% of the panel",
				full, style, worst, 100*float64(worst)/float64(screen), 100*float64(worst)/float64(ta.viz.rect.W*ta.viz.rect.H))
			if limit := screen * 15 / 100; full && worst > limit {
				t.Errorf("%v: %d px damaged in a frame, want under %d (15%% of the screen)", style, worst, limit)
			}
			if worst*5 > ta.viz.rect.W*ta.viz.rect.H {
				t.Errorf("%v: %d px damaged, over a fifth of the panel %v", style, worst, ta.viz.rect)
			}
		}
	}
}

// A frame that moves the bars presents their small areas, not one box over
// the whole picture.
func TestVizPresentsItsAreasWithoutOneBoundingBox(t *testing.T) {
	ta := vizApp(t, PickProfile(1920, 1200, "auto"), VizBars)
	ta.press(input.BtnStart)
	ta.settle(t)
	fb := newFBSim(ta.P.W, ta.P.H)
	ta.o.Display = fb
	runFrames(t, ta, 20)
	px, rects := 0, 0
	for i := 0; i < 20; i++ {
		fb.rects, fb.px = 0, 0
		runFrames(t, ta, 1)
		px, rects = max(px, fb.px), max(rects, fb.rects)
	}
	if rects <= maxDamageRects {
		t.Errorf("at most %d rectangles presented: they were merged", rects)
	}
	if limit := ta.P.W * ta.P.H * 60 / 100; px > limit {
		t.Errorf("presented %d px in a frame of moving bars, want under %d", px, limit)
	}
	t.Logf("worst frame: %d rects, %d px of %d", rects, px, ta.P.W*ta.P.H)
}

// vizScenario runs one style through what can happen to the picture, with
// the app's verify mode on: the partial frame must equal the full one.
// n is how many frames each step runs.
func vizScenario(t *testing.T, prof Profile, style VizStyle, full bool, n int) {
	t.Helper()
	ta := vizApp(t, prof, style)
	fb := newFBSim(ta.P.W, ta.P.H)
	ta.o.Display = fb
	ta.dirty = true // the first frame is the whole screen
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	if full {
		ta.press(input.BtnStart)
		ta.settle(t)
	}
	runFrames(t, ta, 2*n)
	// a toast over the picture, and its going
	ta.Toast("Added to the queue")
	ta.dirty = true
	runFrames(t, ta, n)
	ta.now = ta.now.Add(10 * time.Second)
	runFrames(t, ta, n)
	// the hint bar comes up with a press (full screen)
	ta.press(input.BtnRight)
	runFrames(t, ta, n)
	// the sound pauses, the levels fall, it plays again
	ta.pl.st.Status = player.Paused
	ta.onPlayer(player.Event{})
	runFrames(t, ta, 2*n)
	ta.pl.st.Status = player.Playing
	ta.onPlayer(player.Event{})
	runFrames(t, ta, n)
	// the style changes under it
	ta.SetVizStyle(style%VizWaterfall + 1)
	runFrames(t, ta, n)
	ta.SetVizStyle(style)
	runFrames(t, ta, n)
	// a menu over it
	ta.press(input.BtnX)
	runFrames(t, ta, n)
	ta.press(input.BtnB)
	runFrames(t, ta, n)
	checkPresented(t, ta, fb)
}

// checkPresented renders what is pending and fails if the framebuffer
// differs from the canvas: a pixel that changed was not presented.
func checkPresented(t *testing.T, ta *testApp, fb *fbSim) {
	t.Helper()
	prof, style := ta.P, ta.VizStyle()
	if ta.redrawDue() {
		if err := ta.render(); err != nil {
			t.Fatal(err)
		}
	}
	if ta.scaler == nil && fb.w == ta.canvas.W && fb.h == ta.canvas.H {
		want := unsafe.Slice((*byte)(unsafe.Pointer(&ta.canvas.Pix[0])), 4*len(ta.canvas.Pix))
		if !bytes.Equal(fb.mem, want) {
			t.Errorf("%s %v: the framebuffer differs from the canvas (%d px): a change was not presented", prof.Name, style, fbDiff(fb.mem, want))
		}
	} else {
		t.Logf("%s: scaled (%dx%d canvas on %dx%d): framebuffer not compared", prof.Name, ta.canvas.W, ta.canvas.H, fb.w, fb.h)
	}
}

// fbDiff counts the 4-byte pixels in which a and b differ.
func fbDiff(a, b []byte) int {
	n := 0
	for i := 0; i+4 <= len(a) && i+4 <= len(b); i += 4 {
		if !bytes.Equal(a[i:i+4], b[i:i+4]) {
			n++
		}
	}
	return n
}

// Every style, in the panel and full screen, on the layouts; the biggest
// screen gets fewer frames (every one is also drawn in full on the side), and
// so does every layout under -race, which makes a frame about five times
// slower: a step still lasts several frames.
func TestVizVerifyModeHoldsInPanelAndFullScreen(t *testing.T) {
	sizes := [3]int{15, 15, 4}
	if underRace {
		sizes = [3]int{6, 6, 2}
	}
	for _, c := range []struct {
		prof Profile
		n    int
	}{{ProfileHDMI, sizes[0]}, {ProfileCRT240, sizes[1]}, {PickProfile(1920, 1200, "auto"), sizes[2]}} {
		for style := VizBars; style <= VizWaterfall; style++ {
			for _, full := range []bool{false, true} {
				vizScenario(t, c.prof, style, full, c.n)
			}
		}
	}
}

func TestVizDamageOverlappedByTheHintBarGoesThroughDrawFrame(t *testing.T) {
	ta := vizApp(t, PickProfile(1920, 1200, "auto"), VizBars)
	ta.press(input.BtnStart)
	ta.settle(t)
	v := ta.viz.rect
	hint := ta.hintRect()
	if !ta.hintsUp() {
		t.Fatal("the hint bar is not up after Start")
	}
	// A bar's own area on the hint bar's rows: it must not leave the bar
	// half drawn.
	ta.viz.dmg = append(ta.viz.dmg, gfx.R(v.X+100, hint.Y-10, 40, 30))
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
}
