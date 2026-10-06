package ui

import (
	"testing"
	"time"

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
	runFrames(t, ta, 400) // the peak caps hold, then fall to the bars
	ta.verify = true
	return ta
}

func TestVizDamagesOnlyWhatChangedForSteadyMusic(t *testing.T) {
	for _, full := range []bool{false, true} {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := steadyApp(t, PickProfile(1920, 1200, "auto"), style, full)
			screen := ta.P.W * ta.P.H
			worst := 0
			for i := 0; i < 60; i++ {
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

// The partial frame must equal the full one, whatever the style, the layout,
// the signal and whatever else is drawn on or over the picture. A mismatch
// is reported by the app's verify mode (on in every test app).
func TestVizVerifyModeHoldsInPanelAndFullScreen(t *testing.T) {
	layouts := []Profile{ProfileHDMI, PickProfile(1920, 1200, "auto"), ProfileCRT240}
	for _, prof := range layouts {
		if prof.Name == "crt" && testing.Short() {
			continue
		}
		for style := VizBars; style <= VizWaterfall; style++ {
			for _, full := range []bool{false, true} {
				ta := vizApp(t, prof, style)
				if full {
					ta.press(input.BtnStart)
					ta.settle(t)
				}
				runFrames(t, ta, 60)
				// a toast over the picture, and its going
				ta.Toast("Added to the queue")
				ta.dirty = true
				runFrames(t, ta, 20)
				ta.now = ta.now.Add(10 * time.Second)
				runFrames(t, ta, 20)
				// the hint bar comes up with a press (full screen)
				ta.press(input.BtnRight)
				runFrames(t, ta, 30)
				// the signal stops, the sound pauses, the levels fall, it plays again
				ta.pl.st.Status = player.Paused
				ta.onPlayer(player.Event{})
				runFrames(t, ta, 60)
				ta.pl.st.Status = player.Playing
				ta.onPlayer(player.Event{})
				runFrames(t, ta, 40)
				// the style changes under it
				next := style%VizWaterfall + 1
				ta.SetVizStyle(next)
				runFrames(t, ta, 30)
				ta.SetVizStyle(style)
				runFrames(t, ta, 30)
				// a menu over it
				ta.press(input.BtnX)
				runFrames(t, ta, 20)
				ta.press(input.BtnB)
				runFrames(t, ta, 20)
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
