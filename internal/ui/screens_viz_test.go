package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// fullApp is Now Playing with the visualizer in style, then Start: the full
// screen, with the hint bar already gone (it shows for 3 s after a press).
func fullApp(t *testing.T, prof Profile, style VizStyle) *testApp {
	t.Helper()
	ta := vizApp(t, prof, style)
	ta.press(input.BtnStart)
	ta.settle(t)
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Fatalf("Start left %T on top, want the full screen", ta.Top())
	}
	return ta
}

func lastCall(ta *testApp) string {
	if len(ta.pl.calls) == 0 {
		return ""
	}
	return ta.pl.calls[len(ta.pl.calls)-1]
}

func TestStartOnNowPlayingOpensFullScreenElsewherePauses(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnStart)
	if _, ok := ta.Top().(*VizScreen); !ok || len(ta.pl.calls) != 0 {
		t.Fatalf("on Now Playing: top %T, calls %q", ta.Top(), ta.pl.calls)
	}
	ta = vizApp(t, ProfileHDMI, VizBars)
	ta.Pop() // the Home screen
	ta.press(input.BtnStart)
	if _, ok := ta.Top().(*VizScreen); ok || lastCall(ta) != "toggle" {
		t.Fatalf("on Home: top %T, calls %q", ta.Top(), ta.pl.calls)
	}
	// With no sound source there is nothing to fill the screen with, and
	// with nothing playing nothing to show: Start pauses/plays as before.
	for name, setup := range map[string]func(ta *testApp){
		"no visual":       func(ta *testApp) { ta.o.Visual = nil },
		"nothing playing": func(ta *testApp) { ta.pl.st.Status = player.Stopped },
	} {
		ta = vizApp(t, ProfileHDMI, VizBars)
		setup(ta)
		ta.press(input.BtnStart)
		if _, ok := ta.Top().(*VizScreen); ok || lastCall(ta) != "toggle" {
			t.Errorf("%s: top %T, calls %q", name, ta.Top(), ta.pl.calls)
		}
	}
}

func TestFullScreenLeavesWithBAndStart(t *testing.T) {
	for _, b := range []input.Button{input.BtnB, input.BtnStart} {
		ta := fullApp(t, ProfileHDMI, VizBars)
		ta.press(b)
		if _, ok := ta.Top().(*NowPlayingScreen); !ok {
			t.Errorf("%v left %T on top, want Now Playing", b, ta.Top())
		}
		if len(ta.pl.calls) != 0 {
			t.Errorf("%v called the player: %q", b, ta.pl.calls)
		}
		ta.settle(t)
		if ta.viz.rect.Empty() || ta.viz.rect.H > ta.P.H/2 {
			t.Errorf("%v: the panel is not back: %v", b, ta.viz.rect)
		}
	}
}

func TestFullScreenPassesOtherKeysToNowPlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	for _, c := range []struct {
		b    input.Button
		call string
	}{{input.BtnA, "toggle"}, {input.BtnL, "prev"}, {input.BtnR, "next"}, {input.BtnLeft, "seek"}} {
		ta.press(c.b)
		if got := lastCall(ta); got != c.call {
			t.Errorf("%v called %q, want %q", c.b, got, c.call)
		}
	}
	ta.press(input.BtnDown)
	if ta.pl.st.VolumeDB >= 0 {
		t.Errorf("Down left the volume at %v dB", ta.pl.st.VolumeDB)
	}
	ta.press(input.BtnSelect) // a short Select: the next style
	if ta.VizStyle() != VizScope {
		t.Errorf("Select gave %v, want scope", ta.VizStyle())
	}
	hold(ta, input.BtnSelect, 2*time.Second) // held: mute (its timer belongs to Now Playing)
	if !ta.pl.st.Muted {
		t.Error("holding Select did not mute")
	}
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Fatalf("those keys left %T on top", ta.Top())
	}
	// X opens Now Playing's menu over the full screen; closing it returns.
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok || m.parent != ta.stack[len(ta.stack)-2].s {
		t.Fatalf("X: top %T", ta.Top())
	}
	ta.settle(t)
	if r := ta.viz.rect; r.Empty() || r.W != ta.P.W {
		t.Errorf("the full screen is not drawn behind the menu: %v", r)
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Errorf("closing the menu left %T", ta.Top())
	}
}

func TestYFromFullScreenOpensTheQueueOverNowPlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Fatalf("Y: top %T", ta.Top())
	}
	if _, ok := ta.stack[len(ta.stack)-2].s.(*NowPlayingScreen); !ok {
		t.Fatalf("under the queue: %T, want Now Playing", ta.stack[len(ta.stack)-2].s)
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Errorf("Back from the queue: %T, want Now Playing", ta.Top())
	}
	ta = fullApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Errorf("Q: top %T", ta.Top())
	}
}

func TestEnteringWithTheStyleOffSetsBarsAndSaves(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizOff)
	ta.press(input.BtnStart)
	if ta.VizStyle() != VizBars || ta.cfg.Display.Visualizer != "bars" {
		t.Fatalf("style %v, config %q", ta.VizStyle(), ta.cfg.Display.Visualizer)
	}
	ta.settle(t)
	if ta.viz.rect.Empty() {
		t.Error("nothing is drawn")
	}
	ta = fullApp(t, ProfileHDMI, VizWaterfall) // a chosen style stays
	if ta.VizStyle() != VizWaterfall {
		t.Errorf("style %v after entering with waterfall", ta.VizStyle())
	}
}

func TestFullScreenWithTheStyleOffDrawsNoVisualizer(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.SetVizStyle(VizOff)
	ta.dirty = true
	ta.settle(t)
	if ta.vizActive() || !ta.vizDue().IsZero() || !ta.viz.rect.Empty() {
		t.Errorf("active=%v due=%v rect=%v", ta.vizActive(), ta.vizDue(), ta.viz.rect)
	}
}

func TestFullScreenFillsTheBodyInsideTheSafeArea(t *testing.T) {
	for _, l := range vizLayouts {
		ta := fullApp(t, l.prof, VizBars)
		r := ta.viz.rect
		if r.X != 0 || r.W != l.prof.W || r.Y != l.prof.SafeY || r.Bottom() != l.prof.H-l.prof.SafeY {
			t.Errorf("%s: visualizer %v, want the screen inside SafeY=%d", l.name, r, l.prof.SafeY)
		}
		ta.press(input.BtnUp) // the hint bar shows: the body doesn't change
		ta.settle(t)
		if ta.viz.rect != r {
			t.Errorf("%s: the hint bar changed the visualizer from %v to %v", l.name, r, ta.viz.rect)
		}
	}
}

// cornerOf names where r sits in the body: 0 top left, 1 top right, 2
// bottom right, 3 bottom left (clockwise).
func cornerOf(ta *testApp, vs *VizScreen) int {
	body := ta.vizBody()
	left, top := vs.rect.X < body.X+body.W/2, vs.rect.Y < body.Y+body.H/2
	switch {
	case top && left:
		return 0
	case top:
		return 1
	case !left:
		return 2
	}
	return 3
}

func TestInfoLineMovesClockwiseOncePerMinute(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	vs := ta.Top().(*VizScreen)
	if vs.text != "Капитан Африка — Аквариум  1:15" {
		t.Errorf("info line %q", vs.text)
	}
	if ta.vizBody().Intersect(vs.rect) != vs.rect {
		t.Fatalf("the line %v is outside the body %v", vs.rect, ta.vizBody())
	}
	var seen []int
	for i := 0; i < 5; i++ {
		seen = append(seen, cornerOf(ta, vs))
		before := vs.rect
		ta.now = ta.now.Add(time.Minute)
		ta.pl.st.Position += time.Minute
		ta.clean()
		ta.onWake()
		if vs.rect == before {
			t.Fatalf("minute %d: the line did not move", i+1)
		}
		if !containsRect(ta.damage, before) || !containsRect(ta.damage, vs.rect) {
			t.Errorf("minute %d: moving from %v to %v damaged %v", i+1, before, vs.rect, ta.damage)
		}
		if err := ta.render(); err != nil {
			t.Fatal(err)
		}
	}
	if want := []int{0, 1, 2, 3, 0}; !slices.Equal(seen, want) {
		t.Errorf("corners %v, want %v", seen, want)
	}
}

func TestInfoLineIsDamagedEachSecondNotEachFrame(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	vs := ta.Top().(*VizScreen)
	runFrames(t, ta, 4)
	wakes, hits := 0, 0
	end := ta.now.Add(3 * time.Second)
	for ta.now.Before(end) {
		d := ta.untilWake()
		ta.now = ta.now.Add(d)
		ta.pl.st.Position += d
		ta.clean()
		ta.onWake()
		wakes++
		if containsRect(ta.damage, vs.rect) {
			hits++
		}
		if ta.redrawDue() {
			if err := ta.render(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if wakes < 60 || hits < 2 || hits > 4 {
		t.Errorf("%d wakes in 3 s damaged the info line %d times, want about 3", wakes, hits)
	}
	if vs.text != "Капитан Африка — Аквариум  1:18" && vs.text != "Капитан Африка — Аквариум  1:19" {
		t.Errorf("the line says %q after 3 s", vs.text)
	}
}

func TestHintBarShowsForThreeSecondsAfterAPress(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	// The press that opened the full screen counts: the bar is up.
	if !ta.hintsUp() {
		t.Fatal("no bar when the full screen opens")
	}
	step := func(d time.Duration) {
		ta.now = ta.now.Add(d)
		ta.onWake()
		if err := ta.render(); err != nil {
			t.Fatal(err)
		}
	}
	step(3*time.Second + 10*time.Millisecond)
	if ta.hintsUp() {
		t.Fatal("the bar is still up after 3 s")
	}
	hidden := cropRect(ta.disp.Last(), ta.hintRect())
	ta.press(input.BtnUp)
	ta.settle(t)
	if !ta.hintsUp() {
		t.Fatal("no bar after a press")
	}
	if got := cropRect(ta.disp.Last(), ta.hintRect()); samePixels(hidden.ToRGBA(), got.ToRGBA()) {
		t.Error("the bar is not drawn")
	}
	step(2 * time.Second)
	if !ta.hintsUp() {
		t.Error("the bar went before 3 s")
	}
	ta.press(input.BtnDown) // another press: 3 s from this one
	step(2 * time.Second)
	if !ta.hintsUp() {
		t.Error("a second press did not extend the bar")
	}
	step(1100 * time.Millisecond)
	if ta.hintsUp() {
		t.Error("the bar stayed after the last press + 3 s")
	}
	// Hints turned off in Settings: never a bar.
	ta.cfg.Display.Hints = false
	ta.press(input.BtnUp)
	if ta.hintH() != 0 || ta.hintRect().H != 0 {
		t.Errorf("a bar with hints off: %v", ta.hintRect())
	}
}

func TestScreensaverDoesNotStartInFullScreenWhilePlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.cfg.Display.ScreensaverMinutes = 1
	if !ta.saverDue().IsZero() {
		t.Fatalf("a screensaver due at %v while playing in full screen", ta.saverDue())
	}
	ta.now = ta.now.Add(10 * time.Minute)
	ta.onWake()
	if ta.saver {
		t.Fatal("the screensaver started while playing in full screen")
	}
	ta.pl.st.Status = player.Paused
	ta.onPlayer(player.Event{})
	if ta.saverDue().IsZero() {
		t.Fatal("paused in full screen: no screensaver due")
	}
	ta.onWake()
	ta.settle(t)
	if !ta.saver {
		t.Fatal("the screensaver did not start while paused")
	}
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Errorf("the screensaver replaced the full screen with %T", ta.Top())
	}
	// ...and on Now Playing with the panel it works as before, playing too.
	np := vizApp(t, ProfileHDMI, VizBars)
	np.cfg.Display.ScreensaverMinutes = 1
	np.now = np.now.Add(2 * time.Minute)
	np.onWake()
	if !np.saver {
		t.Error("no screensaver on Now Playing while playing")
	}
}

func TestFullScreenPopsWhenNothingIsPlaying(t *testing.T) {
	for name, end := range map[string]func(ta *testApp){
		"queue cleared": func(ta *testApp) { ta.pl.st.Queue, ta.pl.st.Index, ta.pl.st.Status = nil, -1, player.Stopped },
		"queue ended":   func(ta *testApp) { ta.pl.st.Status = player.Stopped },
	} {
		ta := fullApp(t, ProfileHDMI, VizBars)
		end(ta)
		ta.onPlayer(player.Event{})
		ta.settle(t)
		if _, ok := ta.Top().(*NowPlayingScreen); !ok {
			t.Errorf("%s: top %T, want Now Playing", name, ta.Top())
		}
	}
}

func TestFullScreenHints(t *testing.T) {
	hints := func(ta *testApp) []string {
		var got []string
		for _, h := range ta.screenHints() {
			got = append(got, h.Label)
		}
		return got
	}
	ta := vizApp(t, ProfileHDMI, VizBars)
	if !slices.Contains(hints(ta), "Full screen") {
		t.Errorf("Now Playing hints %q lack Full screen", hints(ta))
	}
	for _, h := range ta.screenHints() {
		if h.Label == "Full screen" && h.Button != input.BtnStart {
			t.Errorf("Full screen is on %v", h.Button)
		}
	}
	ta.pad = false
	if cp, ok := ta.capFor(input.BtnStart); !ok || cp != "Space" {
		t.Errorf("Start's key is %q", cp)
	}
	none := vizApp(t, ProfileHDMI, VizBars)
	none.o.Visual = nil
	if slices.Contains(hints(none), "Full screen") {
		t.Error("Full screen is hinted with no sound source")
	}
	full := fullApp(t, ProfileHDMI, VizBars)
	if got := hints(full); slices.Contains(got, "Full screen") || !slices.Contains(got, "Back") || !slices.Contains(got, "Visualizer") {
		t.Errorf("full screen hints %q", got)
	}
}

func TestFullScreenVerifyModeHolds(t *testing.T) {
	for _, l := range []struct {
		name string
		prof Profile
	}{{"hdmi", ProfileHDMI}, {"crt", ProfileCRT240}} {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := fullApp(t, l.prof, style) // newTestApp turns verify on and fails the test on a mismatch
			runFrames(t, ta, 20)
			ta.press(input.BtnUp) // the bar comes up over the picture
			runFrames(t, ta, 20)
			ta.now = ta.now.Add(time.Minute) // the line moves
			ta.pl.st.Position += time.Minute
			ta.onWake()
			runFrames(t, ta, 10)
			ta.press(input.BtnX) // a menu over it
			runFrames(t, ta, 10)
			ta.press(input.BtnB)
			ta.pl.st.Status = player.Paused // the levels fall, the clock stops
			ta.onPlayer(player.Event{})
			runFrames(t, ta, 30)
		}
	}
}

func TestGoldenVizFull(t *testing.T) {
	for _, l := range vizLayouts {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := fullApp(t, l.prof, style)
			ta.verify = false // checked in TestFullScreenVerifyModeHolds; this keeps the big layouts quick
			runFrames(t, ta, 100)
			ta.verify = true
			runFrames(t, ta, 3)
			if ta.hintsUp() {
				t.Fatalf("%s %v: the hint bar is still up", l.name, style)
			}
			golden(t, "viz-full-"+style.String()+"-"+l.name, ta.disp.Last())
		}
	}
}

func TestSelectInFullScreenSkipsOff(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizWaterfall)
	for _, want := range []VizStyle{VizBars, VizScope, VizVU, VizWaterfall, VizBars} {
		ta.press(input.BtnSelect)
		if got := ta.VizStyle(); got != want {
			t.Fatalf("Select gave %v, want %v", got, want)
		}
	}
	// On Now Playing itself the cycle still has Off.
	np := vizApp(t, ProfileHDMI, VizWaterfall)
	np.press(input.BtnSelect)
	if np.VizStyle() != VizOff {
		t.Errorf("Now Playing: Select from waterfall gave %v, want off", np.VizStyle())
	}
}

func TestScreensaverStaysAwayInFullScreenWhileLoadingOrBuffering(t *testing.T) {
	for _, mid := range []player.Status{player.Loading, player.Buffering} {
		ta := fullApp(t, ProfileHDMI, VizBars)
		ta.cfg.Display.ScreensaverMinutes = 1
		ta.now = ta.now.Add(10 * time.Minute) // long unattended
		for _, st := range []player.Status{mid, player.Playing, mid} {
			ta.pl.st.Status = st
			ta.onPlayer(player.Event{})
			if !ta.saverDue().IsZero() {
				t.Fatalf("%v: a screensaver due at %v", st, ta.saverDue())
			}
			ta.onWake()
			if ta.saver {
				t.Fatalf("the screensaver started while %v in full screen", st)
			}
		}
	}
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.cfg.Display.ScreensaverMinutes = 1
	ta.pl.st.Status = player.Paused
	ta.onPlayer(player.Event{})
	if ta.saverDue().IsZero() {
		t.Error("paused in full screen: no screensaver due")
	}
}

func TestRevealRepaintsTheWholeBarInVerifyMode(t *testing.T) {
	for _, prof := range []Profile{ProfileCRT240, ProfileHDMI} {
		ta := fullApp(t, prof, VizBars)
		ta.now = ta.now.Add(5 * time.Second)
		ta.onWake()
		ta.settle(t)
		if ta.hintsUp() {
			t.Fatal("the bar is still up")
		}
		ta.press(input.BtnUp) // verify mode fails the test if the partial frame differs
		ta.settle(t)
		if !ta.hintsUp() {
			t.Fatal("no bar after a press")
		}
	}
}
