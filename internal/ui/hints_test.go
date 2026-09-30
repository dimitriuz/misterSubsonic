package ui

import (
	"fmt"
	"testing"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// hintFixtures are the screens whose hints are checked, as the user meets
// them.
var hintFixtures = map[string]func(ta *testApp) Screen{
	"root":        func(ta *testApp) Screen { return NewRootScreen(ta.P) },
	"albums":      func(ta *testApp) Screen { return NewAlbumsScreen() },
	"album list":  func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") },
	"artists":     func(ta *testApp) Screen { return NewArtistsScreen() },
	"artist":      func(ta *testApp) Screen { return NewArtistScreen(ta.lib.artists[2].Artists[0]) },
	"album":       func(ta *testApp) Screen { return NewAlbumScreen(ta.lib.albums[0]) },
	"genres":      func(ta *testApp) Screen { return NewGenresScreen() },
	"playlists":   func(ta *testApp) Screen { return NewPlaylistsScreen() },
	"playlist":    func(ta *testApp) Screen { return NewPlaylistScreen(ta.lib.playlists[0]) },
	"starred":     func(ta *testApp) Screen { return NewStarredScreen() },
	"search":      func(ta *testApp) Screen { return NewSearchScreen() },
	"queue":       func(ta *testApp) Screen { playingState(ta); return NewQueueScreen() },
	"now playing": func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() },
	"settings":    func(ta *testApp) Screen { return NewSettingsScreen() },
	"playback":    func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
	"display":     func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
	"servers":     func(ta *testApp) Screen { return NewServersScreen() },
	"wizard":      func(ta *testApp) Screen { return NewWizardScreen(false, false) },
}

func hintApp(t *testing.T, p Profile, mk func(ta *testApp) Screen) *testApp {
	t.Helper()
	ta := newTestApp(t, p)
	ta.cfg = config.Default()
	for i := range 40 { // grids with several pages
		ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
	}
	ta.Push(NewHomeScreen())
	ta.Push(mk(ta))
	ta.settle(t)
	return ta
}

// Every hinted button does something on its screen: the frame, the screen
// stack or the player changes when it is pressed.
func TestEveryHintedButtonDoesSomething(t *testing.T) {
	for _, p := range profiles {
		for name, mk := range hintFixtures {
			hs := hintApp(t, p, mk).screenHints()
			if len(hs) == 0 && name != "about" {
				t.Errorf("%s on %s: no hints", name, p.Name)
			}
			for _, h := range hs {
				// A pair is pressed second button first (R, Right, Down), so
				// the first one (L: back a page) then has somewhere to go.
				ta := hintApp(t, p, mk)
				for _, b := range []input.Button{h.Pair, h.Button} {
					if b == input.BtnNone {
						continue
					}
					if h.Pair == input.BtnNone || b == h.Pair {
						ta = hintApp(t, p, mk)
					}
					before := ta.settle(t).ToRGBA()
					depth, calls, top := len(ta.stack), len(ta.pl.calls), ta.Top()
					ta.press(b)
					after := ta.settle(t).ToRGBA()
					if samePixels(before, after) && len(ta.stack) == depth && len(ta.pl.calls) == calls && ta.Top() == top {
						t.Errorf("%s on %s: %v (%q) does nothing", name, p.Name, b, h.Label)
					}
				}
			}
		}
	}
}

// Back and Now Playing are added where the screen leaves B and Y to the app.
func TestCommonHintsAreAddedWhereTheyApply(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["album"])
	if got := hintLabels(ta.screenHints()); got != "Play, Menu, Shuffle, Back" {
		t.Fatalf("album without a queue: %s", got)
	}
	playingState(ta)
	if got := hintLabels(ta.screenHints()); got != "Play, Menu, Shuffle, Back, Now Playing" {
		t.Fatalf("album with a queue: %s", got)
	}
	ta.Push(NewNowPlayingScreen())
	for _, h := range ta.screenHints() {
		if h.Label == "Now Playing" {
			t.Fatal("Now Playing offers Now Playing")
		}
	}
}

// The bar shows gamepad buttons after a gamepad press and keyboard keys
// after a key press, redrawing only itself when it switches.
func TestHintBarFollowsTheInput(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	if cp, _ := ta.capFor(input.BtnA); cp != "Enter" {
		t.Fatalf("with no gamepad at start A shows %q", cp)
	}
	for _, e := range []input.Event{
		{Button: input.BtnRight, Kind: input.Press, Pad: true}, // the grid focus moves, and the bar switches
		{Button: input.BtnLeft, Kind: input.Press},             // back with a key
	} {
		ta.onInput(e)
		ta.onInput(input.Event{Button: e.Button, Kind: input.Release, Pad: e.Pad})
		ta.settle(t)
		if cp, _ := ta.capFor(input.BtnA); (cp == "A") != e.Pad {
			t.Fatalf("after a press with Pad=%v A shows %q", e.Pad, cp)
		}
		found := false
		for _, r := range presented(t, d) {
			found = found || r.Contains(ta.hintRect().X, ta.hintRect().Y)
		}
		if !found {
			t.Fatalf("switching the bar (Pad=%v) didn't redraw it", e.Pad)
		}
	}
	if _, ok := ta.capFor(input.BtnSelect); ok {
		t.Fatal("a keyboard hint for Select, which no key does")
	}
}

// Settings → Display → Hints turns the bar off and gives its line back.
func TestHintsCanBeTurnedOff(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["display"])
	if ta.hintH() == 0 {
		t.Fatal("no bar by default")
	}
	s := ta.Top().(*SettingsListScreen)
	for i, r := range s.rows(ta.App) {
		if r.label == "Hints" {
			s.list.Focus = i
		}
	}
	ta.press(input.BtnA)
	ta.settle(t)
	if ta.hintH() != 0 || ta.cfg.Display.Hints {
		t.Fatalf("after Hints → Off: bar %d px, config %v", ta.hintH(), ta.cfg.Display.Hints)
	}
}

func TestGoldenHintBar(t *testing.T) {
	for _, p := range []Profile{ProfileHDMI, PickProfile(960, 600, "auto"), PickProfile(1920, 1200, "auto"), ProfileCRT240, PickProfile(640, 288, "auto")} {
		for _, pad := range []bool{true, false} {
			ta := newTestApp(t, p)
			ta.pad = pad
			playingState(ta)
			ta.Push(NewHomeScreen())
			ta.Push(NewAlbumListScreen("Recently added", "newest"))
			kind := "keys"
			if pad {
				kind = "pad"
			}
			name := p.Name
			if p.W != 1280 && p.Name == "hdmi" {
				name = fmt.Sprintf("hdmi-%dx%d", p.W, p.H)
			} else if p.H == 288 {
				name = "crt288"
			}
			c := ta.settle(t)
			golden(t, "hints-"+kind+"-"+name, cropBottom(c, ta.hintRect().Y-ta.P.MiniBarH))
		}
	}
}

// cropBottom is the canvas from line y down (the goldens keep only the
// bottom of the screen).
func cropBottom(c *gfx.Canvas, y int) *gfx.Canvas {
	out := gfx.NewCanvas(c.W, c.H-y)
	copy(out.Pix, c.Pix[y*c.W:])
	return out
}
