package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func margins(l, r, y int) config.Display {
	d := config.Default().Display
	d.OverscanLeft, d.OverscanRight, d.OverscanY = l, r, y
	return d
}

func TestOverscanRect(t *testing.T) {
	for _, c := range []struct {
		pw, ph int
		d      config.Display
		want   gfx.Rect
	}{
		{1280, 720, margins(0, 0, 0), gfx.R(0, 0, 1280, 720)},
		{1280, 720, margins(3, 0, 2), gfx.R(38, 14, 1242, 692)}, // 38.4 and 14.4 px
		{1280, 720, margins(1, 0, 0), gfx.R(13, 0, 1266, 720)},  // 1267 would be odd: the right margin takes the pixel
		{1280, 720, margins(10, 10, 10), gfx.R(128, 72, 1024, 576)},
		{1920, 1200, margins(5, 5, 5), gfx.R(96, 60, 1728, 1080)},
		{1920, 1200, margins(2, 3, 1), gfx.R(38, 12, 1824, 1176)}, // 38.4, 57.6 -> 58: 1824
		{320, 240, margins(0, 0, 5), gfx.R(0, 12, 320, 216)},
		{320, 240, margins(4, 4, 0), gfx.R(13, 0, 294, 240)}, // 12.8 -> 13 each side
	} {
		if got := overscanRect(c.pw, c.ph, c.d); got != c.want {
			t.Errorf("%dx%d %v: %v, want %v", c.pw, c.ph, c.d, got, c.want)
		}
	}
}

func TestProfileForTheInnerArea(t *testing.T) {
	// HDMI: laid out for the inner size, scaled as for a framebuffer of that size.
	in := overscanRect(1280, 720, margins(3, 0, 2))
	p := PickProfileIn(1280, 720, in.W, in.H, "auto")
	if want := scaleProfile(ProfileHDMI, in.W, in.H); p != want || p.W != 1242 || p.H != 692 {
		t.Fatalf("1280x720 inner: %dx%d, %+v", p.W, p.H, p)
	}
	if p.RowH >= ProfileHDMI.RowH || p.Title > ProfileHDMI.Title {
		t.Fatalf("the layout did not get smaller: %+v", p)
	}
	in = overscanRect(1920, 1200, margins(5, 5, 5))
	if p := PickProfileIn(1920, 1200, in.W, in.H, "auto"); p.W != 1728 || p.H != 1080 || p.Name != "hdmi" {
		t.Fatalf("1920x1200 inner: %+v", p)
	}
	// No margins: the profile of the framebuffer, as before.
	if p := PickProfileIn(1280, 720, 1280, 720, "auto"); p != PickProfile(1280, 720, "auto") {
		t.Fatalf("no margins: %+v", p)
	}
	// A CRT keeps its logical size; the scaler fits it into the inner area.
	in = overscanRect(320, 240, margins(4, 4, 5))
	if p := PickProfileIn(320, 240, in.W, in.H, "auto"); p.Name != "crt" || p.W != 320 || p.H != 240 {
		t.Fatalf("crt inner: %+v", p)
	}
}

// overscanApp is an app on a w×h display with margins, over a framebuffer
// simulation that starts full of white, so a border never drawn shows.
func overscanApp(t *testing.T, w, h int, d config.Display) (*testApp, *fbSim) {
	t.Helper()
	fb := newFBSim(w, h)
	for i := range fb.mem {
		fb.mem[i] = 0xFF
	}
	cfg := config.Default()
	cfg.Display = d
	cfg.Display.Visualizer = "bars"
	ta := &testApp{disp: gfx.NewHeadless(w, h, ""), lib: sampleLibrary(), pl: newFakePlayer(), now: time.Unix(1_800_000_000, 0)}
	a, err := New(Options{Display: fb, Profile: PickProfile(w, h, "auto"), Library: ta.lib, Player: ta.pl, Art: newFakeArt(),
		Config: cfg, ConfigPath: filepath.Join(t.TempDir(), "config.toml"), Visual: &fakeVisual{},
		Now: func() time.Time { return ta.now }})
	if err != nil {
		t.Fatal(err)
	}
	ta.App = a
	a.verify = true
	a.verifyFail = func(m string) { t.Errorf("%s", m) }
	t.Cleanup(func() { waitSaves(ta) })
	return ta, fb
}

// borderBlack fails if a pixel outside the inner area of the framebuffer
// simulation is not black, and if the inside differs from the canvas.
func borderBlack(t *testing.T, ta *testApp, fb *fbSim, when string) {
	t.Helper()
	in := ta.inset
	bad := 0
	for y := 0; y < fb.h; y++ {
		for x := 0; x < fb.w; x++ {
			px := fb.mem[y*fb.stride+x*4 : y*fb.stride+x*4+4]
			inside := in.Contains(x, y)
			if !inside && (px[0]|px[1]|px[2]) != 0 {
				bad++
			}
			if inside {
				if v := ta.canvas.Pix[(y-in.Y)*ta.canvas.W+(x-in.X)]; uint32(px[0])|uint32(px[1])<<8|uint32(px[2])<<16 != v&0xFFFFFF {
					bad++
				}
			}
		}
	}
	if bad > 0 {
		t.Errorf("%s: %d pixels of the framebuffer are not as expected (border black, inside the canvas)", when, bad)
	}
}

func TestOverscanBorderStaysBlackAndPartialsStayPartial(t *testing.T) {
	ta, fb := overscanApp(t, 1280, 720, margins(3, 2, 2))
	if ta.canvas.W != 1280-38-26 || ta.canvas.H != 720-2*14 || ta.inset != gfx.R(38, 14, 1216, 692) {
		t.Fatalf("canvas %dx%d inset %v", ta.canvas.W, ta.canvas.H, ta.inset)
	}
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	borderBlack(t, ta, fb, "first frame")
	full := fb.fullPresents
	runFrames(t, ta, 60)
	borderBlack(t, ta, fb, "60 frames of the visualizer")
	ta.Toast("Added to the queue")
	runFrames(t, ta, 20)
	borderBlack(t, ta, fb, "a toast")
	ta.press(input.BtnStart) // the full-screen visualizer
	runFrames(t, ta, 60)
	borderBlack(t, ta, fb, "full-screen visualizer")
	ta.Toast("Again")
	ta.press(input.BtnRight)
	runFrames(t, ta, 20)
	borderBlack(t, ta, fb, "full-screen visualizer with hints and a toast")
	if fb.partials == 0 {
		t.Fatal("no partial present: the frames were all presented whole")
	}
	t.Logf("%d full presents of %d, %d partial", fb.fullPresents, fb.fullPresents+fb.partials, fb.partials)
	if fb.fullPresents-full > 12 {
		t.Errorf("%d whole frames while the visualizer ran: partial presents are lost", fb.fullPresents-full)
	}
	// Presented rectangles lie within the inner area, offset by its origin.
	for _, r := range fb.last {
		if r.Intersect(ta.inset) != r {
			t.Errorf("presented %v lies outside the inner area %v", r, ta.inset)
		}
	}
}

func TestOverscanWithTheScalerKeepsTheBorderBlack(t *testing.T) {
	ta, fb := overscanApp(t, 640, 240, margins(4, 4, 5)) // a CRT mode
	ta.Push(NewHomeScreen())
	ta.settle(t)
	if ta.scaler == nil || ta.canvas.W != 320 || ta.canvas.H != 240 {
		t.Fatalf("crt canvas %dx%d, scaler %v", ta.canvas.W, ta.canvas.H, ta.scaler != nil)
	}
	in := ta.inset
	if in.X != 26 || in.Y != 12 || in.W != 588 || in.H != 216 {
		t.Fatalf("inset %v", in)
	}
	bad, lit := 0, 0
	for y := 0; y < fb.h; y++ {
		for x := 0; x < fb.w; x++ {
			px := fb.mem[y*fb.stride+x*4 : y*fb.stride+x*4+4]
			if in.Contains(x, y) {
				if px[0]|px[1]|px[2] != 0 {
					lit++
				}
			} else if px[0]|px[1]|px[2] != 0 {
				bad++
			}
		}
	}
	if bad > 0 || lit == 0 {
		t.Fatalf("%d border pixels lit, %d inside", bad, lit)
	}
	ta.press(input.BtnDown)
	ta.settle(t)
	if a := ta.scaler.Area(); a != in {
		t.Fatalf("the scaler's area %v, want the inset %v", a, in)
	}
}

func TestOverscanVerifyModePassesAcrossScreens(t *testing.T) {
	ta, fb := overscanApp(t, 1280, 720, margins(3, 3, 2)) // verify is on: a partial frame that differs fails the test
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	for _, b := range []input.Button{input.BtnDown, input.BtnRight, input.BtnDown, input.BtnLeft, input.BtnUp} {
		ta.press(b)
		ta.settle(t)
	}
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	ta.pl.st.Status = player.Playing
	runFrames(t, ta, 40)
	ta.press(input.BtnX) // a menu
	runFrames(t, ta, 5)
	ta.press(input.BtnB)
	borderBlack(t, ta, fb, "after the screens")
}

func TestOverscanScreenshotIsTheInnerPicture(t *testing.T) {
	ta, _ := overscanApp(t, 1280, 720, margins(3, 2, 2))
	dir := t.TempDir()
	ta.o.ScreenshotDir = dir
	ta.Push(NewHomeScreen())
	ta.settle(t)
	if err := ta.screenshot(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for ta.shooting {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("the screenshot did not finish")
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	if len(files) != 1 {
		t.Fatalf("screenshots %v", files)
	}
	f, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1216 || cfg.Height != 692 {
		t.Fatalf("screenshot %dx%d, want the inner 1216x692", cfg.Width, cfg.Height)
	}
}

func rowIndex(t *testing.T, ta *testApp, label string) int {
	t.Helper()
	for i, r := range displaySettings(ta.App) {
		if r.label == label {
			return i
		}
	}
	t.Fatalf("no %q row in %v", label, labels(displaySettings(ta.App)))
	return -1
}

func focusRow(t *testing.T, ta *testApp, label string) {
	t.Helper()
	for range rowIndex(t, ta, label) {
		ta.press(input.BtnDown)
	}
}

func TestOverscanSettingsRowsCycleSaveAndRelayoutLive(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Display", displaySettings))
	ta.settle(t)
	if ta.canvas.W != 1280 {
		t.Fatalf("canvas %d", ta.canvas.W)
	}
	rowH := ta.P.RowH
	focusRow(t, ta, "Overscan: left")
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	if ta.cfg.Display.OverscanLeft != 2 {
		t.Fatalf("left = %d, want 2", ta.cfg.Display.OverscanLeft)
	}
	if ta.canvas.W != 1280-26 || ta.canvas.H != 720 || ta.inset != gfx.R(26, 0, 1254, 720) {
		t.Fatalf("canvas %dx%d, inset %v after left 2%%", ta.canvas.W, ta.canvas.H, ta.inset)
	}
	if ta.P.W != ta.canvas.W || ta.P.RowH >= rowH {
		t.Fatalf("the layout did not follow: P %dx%d row %d (was %d)", ta.P.W, ta.P.H, ta.P.RowH, rowH)
	}
	ta.settle(t)
	// right, then top & bottom
	ta.press(input.BtnDown)
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	if d := ta.cfg.Display; d.OverscanLeft != 2 || d.OverscanRight != 1 || d.OverscanY != 3 {
		t.Fatalf("display %+v", d)
	}
	if ta.canvas.H != 720-2*22 { // 3% of 720 = 21.6 -> 22
		t.Fatalf("canvas height %d", ta.canvas.H)
	}
	ta.settle(t) // verify mode passes on the new layout
	// Left at 0 steps back to 10 (it wraps, like the other number rows) and Right from 10 to 0.
	ta.press(input.BtnUp)
	ta.press(input.BtnUp)
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft)
	if ta.cfg.Display.OverscanLeft != 10 {
		t.Fatalf("left after 0 and one more Left = %d, want 10", ta.cfg.Display.OverscanLeft)
	}
	ta.press(input.BtnRight)
	if ta.cfg.Display.OverscanLeft != 0 {
		t.Fatalf("left after 10 and Right = %d, want 0", ta.cfg.Display.OverscanLeft)
	}
	// Saved: the file holds the values after the writes finish.
	waitSaves(ta)
	b, err := os.ReadFile(ta.o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"overscan_right = 1", "overscan_y = 3", "overscan_left = 0"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("saved config lacks %q:\n%s", want, b)
		}
	}
	// Back to no margins: the canvas is the display's again, without a copy.
	ta.cfg.Display.OverscanLeft, ta.cfg.Display.OverscanRight, ta.cfg.Display.OverscanY = 0, 0, 0
	ta.relayout()
	if ta.canvas.W != 1280 || ta.canvas.H != 720 || ta.frame != nil {
		t.Fatalf("no margins: canvas %dx%d, frame %v", ta.canvas.W, ta.canvas.H, ta.frame != nil)
	}
}

func TestOverscanRowsHaveHelp(t *testing.T) {
	ta, _ := connectedApp(t)
	for _, label := range []string{"Overscan: left", "Overscan: right", "Overscan: top & bottom"} {
		r := displaySettings(ta.App)[rowIndex(t, ta, label)]
		if !strings.Contains(r.help, "TVs that cut") || r.toggle {
			t.Errorf("%s: help %q, toggle %v", label, r.help, r.toggle)
		}
	}
}

func TestGoldenOverscan(t *testing.T) {
	ta, fb := overscanApp(t, 1280, 720, margins(3, 0, 2))
	ta.Push(NewRootScreen(ta.P))
	ta.settle(t)
	golden(t, "home-hdmi-overscan", fbCanvas(fb))
	ta, _ = connectedApp(t)
	ta.cfg.Display = margins(3, 0, 2)
	ta.relayout()
	ta.Push(newSettingsList("Display", displaySettings))
	focusRow(t, ta, "Overscan: left")
	golden(t, "settings-display-overscan", ta.settle(t))
}

// fbCanvas is the framebuffer simulation's memory as a canvas.
func fbCanvas(fb *fbSim) *gfx.Canvas {
	c := gfx.NewCanvas(fb.w, fb.h)
	for i := range c.Pix {
		c.Pix[i] = uint32(fb.mem[4*i]) | uint32(fb.mem[4*i+1])<<8 | uint32(fb.mem[4*i+2])<<16 | uint32(fb.mem[4*i+3])<<24
	}
	return c
}
