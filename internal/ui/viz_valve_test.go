package ui

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func runFor(t testing.TB, ta *testApp, d time.Duration) {
	t.Helper()
	for end := ta.now.Add(d); ta.now.Before(end); {
		runFrames(t, ta, 1)
	}
}

// The panel at 1920x1200 costs about 16 ms on the MiSTer: well inside the
// budget of 30 fps, though over the old 30% step-up rule's 10 ms.
func TestVizValveStepsUpWhenTheCostFitsTheFasterBudget(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	ta.verify = false // the valve is the subject; many frames
	cost := 100 * time.Millisecond
	ta.o.Display = slowDisplay{ta.disp, ta, &cost}
	runFor(t, ta, 8*time.Second)
	if ta.vizFPS() != 10 {
		t.Fatalf("did not reach the floor: %d fps", ta.vizFPS())
	}
	cost = 16 * time.Millisecond
	runFor(t, ta, 26*time.Second) // two steps up, ten seconds of calm each
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
	runFor(t, ta, 15*time.Second) // longer than the calm that steps up
	if ta.vizFPS() != 15 {
		t.Errorf("a 25 ms frame went to %d fps, want 15", ta.vizFPS())
	}
}

// What the valve learnt in one size is not true in the other: back to the top
// rate when the picture changes size, measurements cleared.
func TestVizValveStartsAgainWhenThePanelChangesSize(t *testing.T) {
	for _, toFull := range []bool{false, true} {
		ta := vizApp(t, ProfileHDMI, VizBars)
		ta.verify = false
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

// A valve that cycles (slow, calm, slow, ...) must not turn into a log line
// every few seconds: the log is on the SD card.
func TestVizValveLogsSlowingOncePerMinute(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ta := vizApp(t, ProfileCRT240, VizBars)
	runFrames(t, ta, 2)                   // the valve's rates are set by the first frames
	step := func(cost, d time.Duration) { // frames of this cost for d
		for end := ta.now.Add(d); ta.now.Before(end); {
			ta.now = ta.now.Add(50 * time.Millisecond)
			ta.vizCost(cost)
		}
	}
	downs := 0
	for cycle := 0; cycle < 12; cycle++ { // 12 x 13 s: 156 s
		before := ta.vizFPS()
		step(100*time.Millisecond, 2*time.Second) // far over budget
		if ta.vizFPS() < before {
			downs++
		}
		step(time.Millisecond, 11*time.Second) // calm: steps up again
	}
	if downs < 10 {
		t.Fatalf("the valve stepped down in only %d of 12 cycles: the test is not cycling it", downs)
	}
	got := strings.Count(logs.String(), "visualizer: slowing to")
	if got < 2 || got > 3 {
		t.Fatalf("%d slowing lines in 156 s, want 2 or 3 (one a minute): %q", got, logs.String())
	}
}
