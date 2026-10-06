package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func runFor(t *testing.T, ta *testApp, d time.Duration) {
	t.Helper()
	for end := ta.now.Add(d); ta.now.Before(end); {
		runFrames(t, ta, 1)
	}
}

// The panel at 1920x1200 costs about 16 ms on the MiSTer: well inside the
// budget of 30 fps, though over the old 30% step-up rule's 10 ms.
func TestVizValveStepsUpWhenTheCostFitsTheFasterBudget(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	cost := 100 * time.Millisecond
	ta.o.Display = slowDisplay{ta.disp, ta, &cost}
	runFor(t, ta, 8*time.Second)
	if ta.vizFPS() != 10 {
		t.Fatalf("did not reach the floor: %d fps", ta.vizFPS())
	}
	cost = 16 * time.Millisecond
	runFor(t, ta, 40*time.Second)
	if ta.vizFPS() != 30 {
		t.Errorf("a 16 ms frame stays at %d fps, want 30", ta.vizFPS())
	}
	// A frame over 60% of the 30 fps budget (20 ms) is slowed, and does not
	// bounce back to it.
	cost = 25 * time.Millisecond
	runFor(t, ta, 5*time.Second)
	if ta.vizFPS() != 15 {
		t.Fatalf("a 25 ms frame: %d fps, want 15", ta.vizFPS())
	}
	runFor(t, ta, 60*time.Second)
	if ta.vizFPS() != 15 {
		t.Errorf("a 25 ms frame went to %d fps, want 15", ta.vizFPS())
	}
}

// What the valve learnt in one size is not true in the other: back to the top
// rate when the picture changes size, measurements cleared.
func TestVizValveStartsAgainWhenThePanelChangesSize(t *testing.T) {
	for _, toFull := range []bool{false, true} {
		ta := vizApp(t, PickProfile(1920, 1200, "auto"), VizBars)
		if !toFull {
			ta.press(input.BtnStart)
			ta.settle(t)
		}
		cost := 100 * time.Millisecond
		ta.o.Display = slowDisplay{ta.disp, ta, &cost}
		runFor(t, ta, 8*time.Second)
		if ta.vizFPS() != 10 || !ta.viz.warned {
			t.Fatalf("toFull=%v: not at the floor: %d fps, warned %v", toFull, ta.vizFPS(), ta.viz.warned)
		}
		ta.press(input.BtnStart) // the full screen opens, or closes
		ta.settle(t)
		runFrames(t, ta, 1)
		if ta.vizFPS() != 30 || ta.viz.warned || ta.viz.n != 0 && ta.viz.sum > 150*time.Millisecond {
			t.Errorf("toFull=%v: after the change: %d fps, warned %v, %d costs of %v", toFull, ta.vizFPS(), ta.viz.warned, ta.viz.n, ta.viz.sum)
		}
	}
}
