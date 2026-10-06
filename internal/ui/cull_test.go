package ui

import (
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
)

// Shapes wholly outside the canvas clip do no work at all: a partial frame
// runs the whole drawFrame per damaged area, and the hint bar's chips would
// otherwise cost their scanline maths on every frame of the visualizer.

// countWork counts the scanlines and the outlines drawn from now to the end
// of the test (the hooks are nil in production).
func countWork(t *testing.T) (rows, builds *int) {
	rows, builds = new(int), new(int)
	polyRowHook = func() { *rows++ }
	shapeBuildHook = func() { *builds++ }
	t.Cleanup(func() { polyRowHook, shapeBuildHook = nil, nil })
	return rows, builds
}

func TestFillPolygonOutsideTheClipDoesNoWork(t *testing.T) {
	c := gfx.NewCanvas(200, 100)
	polyRows, _ := countWork(t)
	tri := [][2]float64{{20, 20}, {80, 30}, {40, 90}}
	for name, clip := range map[string]gfx.Rect{
		"right": gfx.R(100, 0, 100, 100), "below": gfx.R(0, 95, 200, 5),
		"above": gfx.R(0, 0, 200, 10), "left": gfx.R(0, 0, 10, 100),
	} {
		c.SetClip(clip)
		*polyRows = 0
		fillPolygon(c, tri, colText)
		if *polyRows != 0 {
			t.Errorf("clip %s: %d scanlines worked", name, *polyRows)
		}
	}
	c.SetClip(gfx.R(0, 40, 200, 10)) // overlapping: only its rows
	*polyRows = 0
	fillPolygon(c, tri, colText)
	if *polyRows == 0 || *polyRows > 11 {
		t.Errorf("overlapping clip worked %d scanlines, want 1..11", *polyRows)
	}
}

func TestFillRoundRectOutsideTheClipBuildsNothing(t *testing.T) {
	c := gfx.NewCanvas(200, 100)
	polyRows, shapeBuilds := countWork(t)
	c.SetClip(gfx.R(0, 0, 200, 10))
	fillRoundRect(c, gfx.R(20, 50, 100, 30), 8, colText)
	if *shapeBuilds != 0 || *polyRows != 0 {
		t.Errorf("built %d shapes, %d scanlines outside the clip", *shapeBuilds, *polyRows)
	}
	c.SetClip(gfx.R(0, 40, 200, 20))
	fillRoundRect(c, gfx.R(20, 50, 100, 30), 8, colText)
	if *shapeBuilds != 1 {
		t.Errorf("a shape in the clip: %d builds, want 1", *shapeBuilds)
	}
}

// The hint bar's chips lie outside a partial frame's damage in the panel.
func TestPartialFrameOfTheVisualizerSkipsTheHintBar(t *testing.T) {
	ta := vizApp(t, PickProfile(1920, 1200, "auto"), VizBars)
	runFrames(t, ta, 3)
	ta.pl.st.Status = player.Paused // no progress regions join the damage
	ta.dirty = true
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	ta.verify = false // its full frame on the side would count too
	polyRows, shapeBuilds := countWork(t)
	ta.Damage(ta.viz.rect)
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	if *shapeBuilds != 0 || *polyRows != 0 {
		t.Errorf("a panel-only frame built %d shapes, %d scanlines", *shapeBuilds, *polyRows)
	}
}
