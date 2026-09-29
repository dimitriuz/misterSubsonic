package ui

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
)

// checkDisplay is a display that reports whether its frame is intact.
type checkDisplay struct {
	*gfx.Headless
	intact bool
	checks int
}

func (d *checkDisplay) Intact() bool { d.checks++; return d.intact }

func TestWatchdogRepaintsAnOverwrittenScreen(t *testing.T) {
	log.SetOutput(io.Discard) // the repaint is logged
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	d := &checkDisplay{Headless: gfx.NewHeadless(ProfileCRT240.W, ProfileCRT240.H, ""), intact: true}
	now := time.Unix(1_800_000_000, 0)
	a, err := New(Options{Display: d, Profile: ProfileCRT240, Library: sampleLibrary(), Player: newFakePlayer(), Art: fakeArt{},
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	a.render()
	if w := a.untilWake(); w > watchdogEvery {
		t.Fatalf("next wake in %v, want the watchdog within %v", w, watchdogEvery)
	}
	now = now.Add(time.Second)
	a.onWake()
	if d.checks != 0 {
		t.Fatal("checked before it was due")
	}
	now = now.Add(time.Second)
	a.onWake()
	if d.checks != 1 || a.dirty {
		t.Fatalf("checks %d dirty %v: an intact screen must not be repainted", d.checks, a.dirty)
	}
	d.intact = false
	now = now.Add(watchdogEvery)
	a.onWake()
	if d.checks != 2 || !a.dirty {
		t.Fatalf("checks %d dirty %v: an overwritten screen must be repainted", d.checks, a.dirty)
	}
	frames := d.Frames()
	a.render()
	if d.Frames() != frames+1 {
		t.Fatal("no frame presented after the overwrite")
	}
}

func TestNoWatchdogWithoutAChecker(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.render()
	ta.now = ta.now.Add(time.Minute)
	ta.onWake() // must not panic on a display that can't check itself
	if !ta.checkAt.IsZero() {
		t.Fatal("a watchdog was scheduled for the headless display")
	}
}
