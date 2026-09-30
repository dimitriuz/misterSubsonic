package ui

import (
	"strings"
	"testing"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// rectDisplay is a headless display that takes partial frames, and records
// what it was given.
type rectDisplay struct {
	*gfx.Headless
	rects [][]gfx.Rect // one entry per PresentRects
	fulls int
}

func (d *rectDisplay) Present(c *gfx.Canvas) error { d.fulls++; return d.Headless.Present(c) }

func (d *rectDisplay) PresentRects(c *gfx.Canvas, rs []gfx.Rect) error {
	d.rects = append(d.rects, append([]gfx.Rect(nil), rs...))
	return d.Headless.Present(c)
}

// boxScreen paints one coloured box on its background.
type boxScreen struct {
	box gfx.Rect
	col gfx.Color
}

func (s *boxScreen) Title() string                          { return "Box" }
func (s *boxScreen) Enter(*App)                             {}
func (s *boxScreen) Handle(*App, input.Event) bool          { return false }
func (s *boxScreen) Draw(a *App, c *gfx.Canvas, _ gfx.Rect) { c.Fill(s.box, s.col) }

func newRectApp(t *testing.T) (*testApp, *rectDisplay, *boxScreen) {
	t.Helper()
	ta := newTestApp(t, ProfileHDMI)
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	s := &boxScreen{box: gfx.R(100, 200, 40, 30), col: gfx.RGB(200, 0, 0)}
	ta.Push(s)
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	return ta, d, s
}

// A change that damages its area is redrawn and presented there only; the
// frame then equals a full redraw (the tests' verify mode checks that).
func TestDamageRedrawsAndPresentsOnlyTheChangedArea(t *testing.T) {
	ta, d, s := newRectApp(t)
	s.col = gfx.RGB(0, 200, 0)
	ta.Damage(s.box)
	c := ta.settle(t)
	if d.fulls != 0 || len(d.rects) != 1 || len(d.rects[0]) != 1 || d.rects[0][0] != s.box {
		t.Fatalf("presented %v rects and %d full frames, want just %v", d.rects, d.fulls, s.box)
	}
	if got := c.At(110, 210); got != gfx.RGB(0, 200, 0) {
		t.Fatalf("box = %08x", got)
	}
}

// The verify mode catches a change whose damage misses it: the frame is
// then drawn in full, so the screen never keeps stale pixels.
func TestVerifyCatchesAMissedDamage(t *testing.T) {
	ta, d, s := newRectApp(t)
	var failed string
	ta.verifyFail = func(m string) { failed = m }
	s.col = gfx.RGB(0, 0, 200)
	ta.Damage(gfx.R(0, 0, 10, 10)) // not the box
	c := ta.settle(t)
	if !strings.Contains(failed, "partial redraw differs") {
		t.Fatalf("verify said %q", failed)
	}
	if d.fulls != 1 || c.At(110, 210) != gfx.RGB(0, 0, 200) {
		t.Fatalf("after a failed check: %d full frames, box %08x", d.fulls, c.At(110, 210))
	}
}

func TestMergeRects(t *testing.T) {
	got := mergeRects([]gfx.Rect{gfx.R(0, 0, 10, 10), gfx.R(10, 0, 10, 10), gfx.R(50, 50, 5, 5)}, 4)
	if len(got) != 2 || got[0] != gfx.R(0, 0, 20, 10) || got[1] != gfx.R(50, 50, 5, 5) {
		t.Fatalf("merged %v", got)
	}
	got = mergeRects([]gfx.Rect{gfx.R(0, 0, 1, 1), gfx.R(10, 0, 1, 1), gfx.R(20, 0, 1, 1)}, 2)
	if len(got) != 1 || got[0] != gfx.R(0, 0, 21, 1) {
		t.Fatalf("over the limit: %v", got)
	}
}

// Every screen, on every layout, stays exactly as a full redraw would draw
// it while the focus moves around (the verify mode compares each partial
// frame with a full one and fails the test on any difference).
func TestPartialRedrawsMatchFullFramesEverywhere(t *testing.T) {
	screens := map[string]func(ta *testApp) Screen{
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
	}
	keys := []input.Button{input.BtnDown, input.BtnDown, input.BtnRight, input.BtnRight, input.BtnDown,
		input.BtnR, input.BtnUp, input.BtnLeft, input.BtnL, input.BtnUp, input.BtnDown}
	partial := map[string]int{}
	for _, p := range append(profiles, PickProfile(960, 600, "auto")) {
		for name, mk := range screens {
			ta := newTestApp(t, p)
			d := &rectDisplay{Headless: ta.disp}
			ta.o.Display = d
			ta.cfg = config.Default()
			ta.Push(NewHomeScreen())
			ta.Push(mk(ta))
			ta.settle(t)
			for _, b := range keys {
				ta.onInput(input.Event{Button: b, Kind: input.Press})
				ta.settle(t)
				ta.onInput(input.Event{Button: b, Kind: input.Repeat})
				ta.settle(t)
				ta.onInput(input.Event{Button: b, Kind: input.Release})
			}
			if t.Failed() {
				t.Fatalf("%s on %s", name, p.Name)
			}
			partial[name] += len(d.rects)
		}
	}
	// The walk must exercise partial frames where screens opted in.
	for _, name := range []string{"albums", "artists", "album", "genres", "playlists", "queue", "settings", "playback"} {
		if partial[name] == 0 {
			t.Errorf("%s: no partial frame was drawn", name)
		}
	}
	t.Logf("partial frames: %v", partial)
}
