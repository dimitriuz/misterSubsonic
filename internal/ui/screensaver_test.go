package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

func saverApp(t *testing.T, p Profile, minutes int) *testApp {
	t.Helper()
	ta := newTestApp(t, p)
	ta.cfg = config.Default()
	ta.cfg.Display.ScreensaverMinutes = minutes
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.lastInput = ta.now
	return ta
}

func TestScreensaverStartsAfterIdleOnNowPlaying(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 5)
	ta.now = ta.now.Add(5*time.Minute - time.Second)
	ta.onWake()
	if ta.saver {
		t.Fatal("started early")
	}
	if d := ta.untilWake(); d > progressTick {
		t.Fatalf("next wake %v while playing", d)
	}
	ta.now = ta.now.Add(time.Second)
	ta.onWake()
	if !ta.saver {
		t.Fatal("not started after 5 idle minutes")
	}
	if d := ta.untilWake(); d != saverStep {
		t.Fatalf("next wake %v with the screensaver on, want only the drift (%v)", d, saverStep)
	}
	first := ta.settle(t).ToRGBA()
	ta.now = ta.now.Add(10 * saverStep)
	ta.onWake()
	if samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("the cover doesn't drift")
	}
}

func TestScreensaverWakePressDoesNothingElse(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	ta.press(input.BtnA) // would toggle pause
	if ta.saver || len(ta.pl.calls) != 0 {
		t.Fatalf("saver %v, player calls %v", ta.saver, ta.pl.calls)
	}
	ta.press(input.BtnA)
	if len(ta.pl.calls) != 1 {
		t.Fatalf("the next press did nothing: %v", ta.pl.calls)
	}
}

func TestScreensaverOnlyOnNowPlayingAndNotWhenOff(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.Pop() // Home
	ta.now = ta.now.Add(time.Hour)
	ta.onWake()
	if ta.saver {
		t.Fatal("screensaver on a browse screen")
	}
	off := saverApp(t, ProfileHDMI, 0)
	off.now = off.now.Add(time.Hour)
	off.onWake()
	if off.saver {
		t.Fatal("screensaver while it is off")
	}
}

func TestBounce(t *testing.T) {
	for _, c := range [][3]int{{0, 10, 0}, {7, 10, 7}, {10, 10, 10}, {13, 10, 7}, {20, 10, 0}, {25, 10, 5}, {5, 0, 0}} {
		if got := bounce(c[0], c[1]); got != c[2] {
			t.Errorf("bounce(%d,%d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestGoldenScreensaver(t *testing.T) {
	for _, p := range profiles {
		ta := saverApp(t, p, 1)
		ta.now = ta.now.Add(time.Minute)
		ta.onWake()
		ta.now = ta.now.Add(7 * saverStep)
		golden(t, "screensaver-"+p.Name, ta.settle(t))
	}
}

func TestToastWakesTheScreensaver(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	if !ta.saver {
		t.Fatal("screensaver not on")
	}
	ta.Toast("Can't play x: offline")
	if ta.saver {
		t.Fatal("a toast left the screensaver on: it can't be seen")
	}
	ta.onWake()
	if ta.saver {
		t.Fatal("the screensaver came straight back over the toast")
	}
}

func TestScreensaverNeverStartsOverTheExitPrompt(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.confirm = true
	ta.now = ta.now.Add(2 * time.Minute)
	ta.onWake()
	if ta.saver || !ta.saverDue().IsZero() {
		t.Fatalf("screensaver over the exit prompt: saver %v, due %v", ta.saver, ta.saverDue())
	}
}

func TestScreensaverWakeSwallowsTheReleaseToo(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press}) // wakes
	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Release})
	if len(ta.toasts) != 0 || ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
		t.Fatalf("the wake press's release cycled the mode: toasts %v, %+v", ta.toasts, ta.pl.st)
	}
	ta.press(input.BtnSelect) // a real tap still cycles
	if !ta.pl.st.Shuffle {
		t.Fatal("the next tap didn't cycle the mode")
	}
}

func TestScreensaverWakeReleaseReachesNoScreenHook(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	rel := &releaseSpy{}
	ta.Push(rel)
	ta.lastInput = ta.now
	ta.saver = true
	ta.onInput(input.Event{Button: input.BtnA, Kind: input.Press})
	ta.onInput(input.Event{Button: input.BtnA, Kind: input.Release})
	if rel.n != 0 {
		t.Fatalf("Release hook called %d times for the wake press", rel.n)
	}
	ta.press(input.BtnA)
	if rel.n != 1 {
		t.Fatalf("Release hook called %d times for a normal press, want 1", rel.n)
	}
}

type releaseSpy struct{ n int }

func (s *releaseSpy) Title() string                    { return "spy" }
func (s *releaseSpy) Enter(*App)                       {}
func (s *releaseSpy) Handle(*App, input.Event) bool    { return true }
func (s *releaseSpy) Draw(*App, *gfx.Canvas, gfx.Rect) {}
func (s *releaseSpy) Release(*App, input.Button)       { s.n++ }
