package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
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
	"now playing with a visualizer": func(ta *testApp) Screen {
		ta.o.Visual = &fakeVisual{}
		playingState(ta)
		return NewNowPlayingScreen()
	},
	"full screen": func(ta *testApp) Screen {
		ta.o.Visual = &fakeVisual{}
		playingState(ta)
		np := NewNowPlayingScreen()
		ta.Push(np)
		return NewVizScreen(np)
	},
	"settings": func(ta *testApp) Screen { return NewSettingsScreen() },
	"playback": func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
	"display":  func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
	"servers":  func(ta *testApp) Screen { return NewServersScreen() },
	"wizard":   func(ta *testApp) Screen { return NewWizardScreen(false, false) },
	"wizard with text": func(ta *testApp) Screen {
		w := NewWizardScreen(false, false)
		w.fields[stepURL] = []rune("http://h:4533")
		return w
	},
	"wizard second step": func(ta *testApp) Screen {
		w := NewWizardScreen(true, false)
		w.setStep(stepUser)
		return w
	},
	"search with text": func(ta *testApp) Screen {
		s := NewSearchScreen()
		s.query = []rune("ab")
		return s
	},
	"root content": func(ta *testApp) Screen {
		r := NewRootScreen(ta.P)
		if sr, ok := r.(*SidebarRoot); ok {
			sr.open(ta.App)
			sr.inSidebar = false
		}
		return r
	},
	"message": func(ta *testApp) Screen {
		return NewMessageScreen("Oops", "Something broke", func() { ta.Toast("retrying") })
	},
	"message without retry": func(ta *testApp) Screen { return NewMessageScreen("Oops", "Nothing to do", nil) },
	"unreachable": func(ta *testApp) Screen {
		srv := config.Server{Name: "home", URL: "http://h:4533", Username: "u", Password: "p"}
		ta.cfg.AddServer(srv)
		ta.o.Connect = func(*App, *config.Config) {} // "Try again" connects
		return NewUnreachableScreen(srv, errors.New("refused"))
	},
	"feed with resume": func(ta *testApp) Screen {
		s := NewFeedScreen() // set here, not on the player: a load of the Home screen reads that
		s.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1}
		return s
	},
	"search results": func(ta *testApp) Screen {
		s := NewSearchScreen()
		s.query, s.searched, s.inResults = []rune("bj"), "bj", true
		s.artists.artists = ta.lib.artists[2].Artists
		s.albums.albums = ta.lib.albums[:3]
		s.songs.songs = ta.lib.tracks["al-1"]
		s.relabel()
		return s
	},
	"menu with a queue": func(ta *testApp) Screen {
		playingState(ta)
		return NewMenuScreen(ta.Top(), "Menu", []menuEntry{{"One", func(a *App) {}}, {"Two", func(a *App) {}}})
	},
}

func hintApp(t *testing.T, p Profile, mk func(ta *testApp) Screen) *testApp {
	t.Helper()
	return hintAppWith(t, p, nil, mk)
}

// hintAppWith is hintApp with setup run on the app before the screen loads
// (a failing library, an empty one).
func hintAppWith(t *testing.T, p Profile, setup func(ta *testApp), mk func(ta *testApp) Screen) *testApp {
	t.Helper()
	ta := newTestApp(t, p)
	ta.cfg = config.Default()
	for i := range 40 { // grids with several pages
		ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
	}
	if setup != nil {
		setup(ta)
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
					if h.Rune != 0 { // a typing key: Backspace, Enter
						ta.pressKey(h.Rune)
					} else if h.Hold {
						hold(ta, b, 2*time.Second)
					} else {
						ta.press(b)
					}
					after := ta.settle(t).ToRGBA()
					if samePixels(before, after) && len(ta.stack) == depth && len(ta.pl.calls) == calls && ta.Top() == top {
						t.Errorf("%s on %s: %v (%q) does nothing", name, p.Name, b, h.Label)
					}
				}
			}
		}
	}
}

// The hinted button on a screen that failed to load is A, which retries; a
// screen with nothing listed hints nothing of its own (spec §4: every hinted
// button does something).
func TestHintsOnErrorAndEmptyStates(t *testing.T) {
	boom := func(ta *testApp) { ta.lib.err = errors.New("boom") }
	empty := func(ta *testApp) {
		ta.lib.albums, ta.lib.artists, ta.lib.genres, ta.lib.playlists = nil, nil, nil, nil
		ta.lib.starred = subsonic.Starred{}
	}
	failing := []string{"albums", "album list", "artists", "artist", "album", "genres", "playlists", "playlist", "starred", "feed"}
	listed := []string{"album list", "artists", "genres", "playlists", "starred", "feed"}
	for _, p := range profiles {
		for _, name := range failing {
			mk := hintFixtures[name]
			if name == "feed" {
				mk = func(ta *testApp) Screen { return NewFeedScreen() }
			}
			ta := hintAppWith(t, p, boom, mk)
			hs := ta.screenHints()
			if len(hs) == 0 || hs[0].Label != "Retry" || hs[0].Button != input.BtnA {
				t.Errorf("%s failed on %s: hints %q, want Retry first", name, p.Name, hintLabels(hs))
			}
			for _, h := range hs[1:] {
				if h.Label != "Back" {
					t.Errorf("%s failed on %s: hint %q besides Retry and Back", name, p.Name, h.Label)
				}
			}
			ta.lib.err = nil
			ta.press(input.BtnA)
			ta.settle(t)
			if got := ta.screenHints(); len(got) > 0 && got[0].Label == "Retry" {
				t.Errorf("%s on %s: A (Retry) did not load the screen", name, p.Name)
			}
		}
		for _, name := range listed {
			mk := hintFixtures[name]
			if name == "feed" {
				mk = func(ta *testApp) Screen { return NewFeedScreen() }
			}
			ta := hintAppWith(t, p, empty, mk)
			if got := hintLabels(ta.screenHints()); got != "Back" {
				t.Errorf("empty %s on %s: hints %q, want just Back", name, p.Name, got)
			}
		}
	}
	// A loading screen hints nothing of its own either.
	ta := newTestApp(t, ProfileHDMI)
	ta.cfg = config.Default()
	ta.lib.block = make(chan struct{})
	ta.Push(NewHomeScreen())
	ta.Push(NewGenresScreen())
	ta.Top().(*GenresScreen).loaded, ta.Top().(*GenresScreen).genres = false, nil
	if got := hintLabels(ta.screenHints()); got != "Back" {
		t.Errorf("loading genres: hints %q, want just Back", got)
	}
	// The empty queue can only go to Now Playing.
	ta = hintApp(t, ProfileHDMI, func(ta *testApp) Screen { return NewQueueScreen() })
	if got := hintLabels(ta.screenHints()); got != "Now Playing, Back" {
		t.Errorf("empty queue: hints %q", got)
	}
}

// Back and Now Playing are added where the screen leaves B and Y to the app.
func TestCommonHintsAreAddedWhereTheyApply(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["album"])
	if got := hintLabels(ta.screenHints()); got != "Play, Back, Menu, Shuffle" {
		t.Fatalf("album without a queue: %s", got)
	}
	playingState(ta)
	if got := hintLabels(ta.screenHints()); got != "Play, Back, Menu, Shuffle, Now Playing" {
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

// A menu takes every button, so it offers neither Back nor Now Playing.
func TestMenuHintsAreItsOwn(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["menu with a queue"])
	if got := hintLabels(ta.screenHints()); got != "Choose, Close" {
		t.Fatalf("menu with a queue: %s", got)
	}
}

// A text field takes N as a letter on a keyboard, so Now Playing is hinted
// there only for a gamepad, whose Y does open it.
func TestTypingScreensHintNowPlayingOnlyForThePad(t *testing.T) {
	for _, name := range []string{"search", "wizard"} {
		mk := hintFixtures[name]
		for _, pad := range []bool{false, true} {
			ta := hintApp(t, ProfileHDMI, func(ta *testApp) Screen { playingState(ta); return mk(ta) })
			ta.pad = pad
			has := strings.Contains(hintLabels(ta.screenHints()), "Now Playing")
			if has != pad {
				t.Errorf("%s, pad=%v: Now Playing hinted %v (%s)", name, pad, has, hintLabels(ta.screenHints()))
			}
			if pad && has {
				ta.press(input.BtnY)
				ta.settle(t)
				if _, ok := ta.Top().(*NowPlayingScreen); !ok {
					t.Errorf("%s: Y with a gamepad leaves %T on top", name, ta.Top())
				}
			}
		}
	}
	// Back is still hinted: Esc is not a letter.
	ta := hintApp(t, ProfileHDMI, func(ta *testApp) Screen { playingState(ta); return NewSearchScreen() })
	ta.pad = false
	if !strings.Contains(hintLabels(ta.screenHints()), "Back") {
		t.Errorf("Search on a keyboard lost Back: %s", hintLabels(ta.screenHints()))
	}
}

// pressKey is a keyboard key that types r: Enter and Backspace carry their
// button too.
func (ta *testApp) pressKey(r rune) {
	b := input.BtnNone
	switch r {
	case '\n':
		b = input.BtnA
	case '\b':
		b = input.BtnB
	}
	ta.onInput(input.Event{Button: b, Kind: input.Press, Rune: r})
	ta.onInput(input.Event{Button: b, Kind: input.Release, Rune: r})
}

// On a keyboard letters type and Backspace deletes, so that is what a
// typing screen hints (Enter only where it submits); a gamepad still types
// with A and deletes with X.
func TestTypingHintsFollowTheInput(t *testing.T) {
	type key struct{ cap, label string }
	caps := func(ta *testApp) []key {
		var out []key
		for _, h := range ta.screenHints() {
			c, _ := ta.capFor(h.Button)
			if h.Key != "" {
				c = h.Key
			}
			out = append(out, key{c, h.Label})
		}
		return out
	}
	for _, c := range []struct {
		screen string
		pad    bool
		want   []key
	}{
		{"wizard", false, []key{{"Enter", "Next"}, {"Esc", "Back"}}},
		{"wizard with text", false, []key{{"Enter", "Next"}, {"Esc", "Back"}, {"Backspace", "Delete"}}},
		{"wizard with text", true, []key{{"A", "Type"}, {"B", "Back"}, {"X", "Delete"}}},
		{"search", false, []key{{"Esc", "Back"}}},
		{"search with text", false, []key{{"Backspace", "Delete"}, {"Esc", "Back"}}},
		{"search with text", true, []key{{"A", "Type"}, {"B", "Back"}, {"X", "Delete"}}},
	} {
		ta := hintApp(t, ProfileHDMI, hintFixtures[c.screen])
		ta.pad = c.pad
		if got := caps(ta); !slices.Equal(got, c.want) {
			t.Errorf("%s, pad=%v: hints %v, want %v", c.screen, c.pad, got, c.want)
		}
	}
}

// B is hinted where it does something of its own: back a step in the
// wizard, out of a section to the sidebar (both are the app's first screen,
// where the app adds no Back).
func TestBackIsHintedWhereBReturns(t *testing.T) {
	first := func(s Screen) *testApp {
		ta := newTestApp(t, ProfileHDMI)
		ta.cfg = config.Default()
		ta.pad = true
		ta.Push(s)
		ta.settle(t)
		return ta
	}
	w := NewWizardScreen(true, false)
	if got := hintLabels(first(w).screenHints()); strings.Contains(got, "Back") {
		t.Errorf("first-run wizard, first step: %s", got) // B has nowhere to go
	}
	w = NewWizardScreen(true, false)
	w.setStep(stepUser)
	if got := hintLabels(first(w).screenHints()); !strings.Contains(got, "Back") {
		t.Errorf("first-run wizard, second step: %s", got)
	}
	w.setStep(stepTest)
	if got := hintLabels(first(w).screenHints()); !strings.Contains(got, "Back") {
		t.Errorf("first-run wizard, test step: %s", got)
	}
	root := newSidebarRoot() // starts with the content focused
	ta := first(root)
	if got := hintLabels(ta.screenHints()); !strings.Contains(got, "Back") {
		t.Errorf("root, content focused: %s", got)
	}
	ta.press(input.BtnB)
	if !root.inSidebar {
		t.Error("B in the content didn't go to the sidebar")
	}
	if got := hintLabels(ta.screenHints()); strings.Contains(got, "Back") {
		t.Errorf("root, sidebar focused: %s", got)
	}
}

// The sidebar root types only when a section that takes typed keys has the
// focus (a letter on the sidebar or on a list is not text).
func TestSidebarRootTyping(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.cfg = config.Default()
	root := newSidebarRoot()
	ta.Push(root)
	section := func(label string) {
		t.Helper()
		for i, l := range root.labels {
			if l == label {
				root.sel = i
				root.open(ta.App)
				return
			}
		}
		t.Fatalf("no section %q", label)
	}
	section("Search")
	root.inSidebar = true
	if root.Typing() {
		t.Error("typing with the sidebar focused")
	}
	root.inSidebar = false
	if !root.Typing() {
		t.Error("not typing in the Search field")
	}
	root.current().(*SearchScreen).inResults = true
	if root.Typing() {
		t.Error("typing with the search results focused")
	}
	section("Albums")
	if root.Typing() {
		t.Error("typing in a section that takes no text")
	}
	root.sel = 0 // Home: the feed is not loaded as a child yet
	root.children[0] = nil
	if root.Typing() {
		t.Error("typing with no section open")
	}
}
