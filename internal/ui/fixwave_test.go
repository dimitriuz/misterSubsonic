package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"slices"
	"testing"

	"mistersubsonic/internal/subsonic"
)

// Subsonic IDs are per table: artist "7", album "7" and song "7" coexist.
func TestStarStateIsPerKind(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	artist := starItem{kind: starArtist, id: "7", name: "Artist seven"}
	album := starItem{kind: starAlbum, id: "7", name: "Album seven"}
	ta.toggleStar(artist)
	ta.settle(t)
	if !ta.isStarred(artist) {
		t.Fatal("artist 7 not starred")
	}
	if ta.isStarred(album) || ta.isStarred(starItem{kind: starSong, id: "7"}) {
		t.Fatal("starring artist 7 starred album/song 7")
	}
	if m := albumMenu(ta.App, subsonic.Album{ID: "7", Name: "Album seven"}); m[starMenuIndex(m)].label != "Star" {
		t.Fatalf("album menu offers %q", m[starMenuIndex(m)].label)
	}
}

func starMenuIndex(m []menuEntry) int {
	for i, e := range m {
		if e.label == "Star" || e.label == "Unstar" {
			return i
		}
	}
	return 0
}

const longTitle = "A Rather Long Album Title That Will Need Truncating Somewhere"

// During the pre-scroll delay the frames are identical: no per-frame redraws,
// one wake at the end of the delay.
func TestMarqueeWaitsQuietlyForItsDelay(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.Push(&probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, longTitle, colText, area, true)
	}})
	ta.settle(t)
	if ta.animate {
		t.Fatal("animating during the delay")
	}
	if d := ta.untilWake(); d < marqueeDelay-time.Millisecond || d > marqueeDelay {
		t.Fatalf("next wake in %v, want the remaining delay %v", d, marqueeDelay)
	}
	ta.now = ta.now.Add(marqueeDelay / 2)
	ta.settle(t)
	if d := ta.untilWake(); d < marqueeDelay/2-time.Millisecond || d > marqueeDelay/2 {
		t.Fatalf("half way: next wake in %v, want %v", d, marqueeDelay/2)
	}
	ta.now = ta.now.Add(marqueeDelay / 2)
	ta.dirty = false
	ta.onWake()
	if !ta.dirty {
		t.Fatal("the wake at the end of the delay did not redraw")
	}
	ta.settle(t)
	if !ta.animate || ta.untilWake() > marqueeFrame {
		t.Fatalf("not animating after the delay (wake %v)", ta.untilWake())
	}
}

// A long title only scrolls where the focus really is: not behind the
// sidebar focus, a menu, or Search's dimmed results.
func TestMarqueeOnlyWhereFocused(t *testing.T) {
	// scrolls draws a frame, waits out the delay, draws again and reports
	// whether something still moves.
	scrolls := func(ta *testApp) bool {
		ta.settle(t)
		ta.now = ta.now.Add(marqueeDelay + time.Second)
		ta.settle(t)
		return ta.animate
	}
	t.Run("sidebar", func(t *testing.T) {
		ta := newTestApp(t, ProfileHDMI)
		ta.Push(newSidebarRoot())
		ta.settle(t)
		ta.press(input.BtnRight)
		ta.press(input.BtnRight) // al-3, the long title
		if !scrolls(ta) {
			t.Fatal("focused long cover title does not scroll")
		}
		ta.press(input.BtnB) // the sidebar, with the long title still selected
		if !ta.Top().(*SidebarRoot).inSidebar {
			t.Fatal("B did not reach the sidebar")
		}
		if scrolls(ta) {
			t.Fatal("marquee runs while the sidebar has the focus")
		}
	})
	t.Run("menu", func(t *testing.T) {
		ta := newTestApp(t, ProfileHDMI)
		ta.Push(newSidebarRoot())
		ta.settle(t)
		ta.press(input.BtnRight)
		ta.press(input.BtnRight)
		ta.press(input.BtnX)
		if _, ok := ta.Top().(*MenuScreen); !ok {
			t.Fatalf("X opened %T", ta.Top())
		}
		if scrolls(ta) {
			t.Fatal("marquee runs under the menu")
		}
		ta.press(input.BtnB)
		if !scrolls(ta) {
			t.Fatal("marquee did not come back after the menu closed")
		}
	})
	t.Run("search", func(t *testing.T) {
		ta := newTestApp(t, ProfileHDMI)
		ta.Push(NewHomeScreen())
		s := NewSearchScreen()
		ta.Push(s)
		typeKeys(ta, "rather")
		ta.now = ta.now.Add(searchDelay)
		ta.onWake()
		ta.settle(t)
		if s.tabs.Sel != resAlbums || len(s.albums.albums) != 1 {
			t.Fatalf("tab %d, albums %d", s.tabs.Sel, len(s.albums.albums))
		}
		if scrolls(ta) {
			t.Fatal("marquee runs in the dimmed results")
		}
		for range 10 { // to the keyboard's right edge, then into the results
			ta.press(input.BtnRight)
		}
		if !scrolls(ta) || !s.inResults {
			t.Fatalf("inResults %v, animate %v", s.inResults, ta.animate)
		}
	})
}

// dwell lets the sidebar selection rest long enough to open its section.
func dwell(ta *testApp) {
	ta.now = ta.now.Add(sidebarDwell)
	ta.onWake()
}

func TestSidebarOnlyOpensWhereItRests(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	root := newSidebarRoot()
	ta.Push(root)
	ta.settle(t)
	ta.lib.calls = nil
	ta.press(input.BtnLeft) // the sidebar
	for range 5 {           // Artists, Albums, Playlists, Starred, Search
		ta.press(input.BtnDown)
	}
	ta.settle(t)
	for i := 1; i <= 4; i++ {
		if root.children[i] != nil {
			t.Fatalf("section %d was opened on the way past", i)
		}
	}
	if ta.lib.starCalls != 0 || len(ta.lib.calls) != 0 {
		t.Fatalf("loads on the way past: starred %d, albums %v", ta.lib.starCalls, ta.lib.calls)
	}
	ta.settle(t) // the placeholder frame
	dwell(ta)
	ta.settle(t)
	if _, ok := root.children[5].(*SearchScreen); !ok {
		t.Fatalf("Search not opened after the dwell: %T", root.children[5])
	}
	ta.press(input.BtnUp) // Starred, entered at once by Right
	ta.press(input.BtnRight)
	if root.children[4] == nil || root.inSidebar {
		t.Fatalf("Right did not open the section: %v", root.children[4])
	}
	ta.settle(t)
	if ta.lib.starCalls != 1 {
		t.Fatalf("getStarred2 called %d times", ta.lib.starCalls)
	}
}

func TestStarredRefreshesAfterAStarChange(t *testing.T) {
	newRoot := func(t *testing.T) (*testApp, *SidebarRoot) {
		ta := newTestApp(t, ProfileHDMI)
		root := newSidebarRoot()
		ta.Push(root)
		ta.settle(t)
		ta.press(input.BtnLeft)
		for range 4 {
			ta.press(input.BtnDown)
		}
		ta.press(input.BtnRight) // Starred
		ta.settle(t)
		if ta.lib.starCalls != 1 {
			t.Fatalf("getStarred2 called %d times", ta.lib.starCalls)
		}
		return ta, root
	}
	star := func(t *testing.T, ta *testApp, owner Screen) {
		ta.lib.starred.Albums = append(ta.lib.starred.Albums, ta.lib.albums[0]) // the server's view
		ta.toggleStar(albumStar(ta.lib.albums[0]))
		ta.settle(t)
	}
	t.Run("sidebar", func(t *testing.T) {
		ta, root := newRoot(t)
		star(t, ta, root)
		ta.press(input.BtnB)
		ta.press(input.BtnDown) // Search
		ta.press(input.BtnUp)   // and back
		dwell(ta)
		ta.settle(t)
		tab := root.children[4].(*TabbedScreen).children[0].(*starredTab)
		if ta.lib.starCalls != 2 || tab.sync() != 2 {
			t.Fatalf("getStarred2 called %d times, %d albums listed", ta.lib.starCalls, tab.sync())
		}
	})
	t.Run("nothing changed", func(t *testing.T) {
		ta, _ := newRoot(t)
		ta.press(input.BtnB)
		ta.press(input.BtnDown)
		ta.press(input.BtnUp)
		dwell(ta)
		ta.settle(t)
		if ta.lib.starCalls != 1 {
			t.Fatalf("reloaded without a star change: %d calls", ta.lib.starCalls)
		}
	})
	t.Run("tabs", func(t *testing.T) {
		ta := newTestApp(t, ProfileHDMI)
		ta.Push(NewHomeScreen())
		s := NewStarredScreen()
		ta.Push(s)
		ta.settle(t)
		star(t, ta, s)
		ta.press(input.BtnUp)
		ta.press(input.BtnRight) // the Artists tab
		ta.settle(t)
		if ta.lib.starCalls != 2 || s.children[0].(*starredTab).sync() != 2 {
			t.Fatalf("getStarred2 called %d times", ta.lib.starCalls)
		}
	})
}

// Navidrome answers nothing to a one-letter query: that's not "nothing found".
func TestSearchOneLetterSaysKeepTyping(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	typeKeys(ta, "q")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if len(ta.lib.searches) != 1 || ta.lib.searches[0] != "q" {
		t.Fatalf("searches %v: one-letter queries are still sent", ta.lib.searches)
	}
	if text, col := s.statusText(); text != "Keep typing…" || col != colDim {
		t.Fatalf("status %q", text)
	}
	typeKeys(ta, "z")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if text, _ := s.statusText(); text != "Nothing found for “qz”" {
		t.Fatalf("status %q", text)
	}
}

func TestSearchSelectRetriesAFailedSearch(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.err = errOffline
	typeKeys(ta, "bj")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	want := "Search failed: " + subsonic.Classify(errOffline).String() + " — Select to retry"
	if text, col := s.statusText(); s.err == nil || text != want || col != colError {
		t.Fatalf("err %v, status %q; want %q", s.err, text, want)
	}
	ta.lib.err = nil
	ta.press(input.BtnSelect) // no debounce
	ta.settle(t)
	if s.err != nil || len(s.artists.artists) != 1 || len(ta.lib.searches) != 2 {
		t.Fatalf("after Select: err %v, artists %v, searches %v", s.err, s.artists.artists, ta.lib.searches)
	}
}

func TestKeyboardLayoutsAreBuiltOnce(t *testing.T) {
	var k Keyboard
	a, b := k.rows(), k.rows()
	if &a[0][0] != &b[0][0] {
		t.Fatal("rows() rebuilt the layout")
	}
	k.Switch(1)
	c, d := k.rows(), k.rows()
	if &c[0][0] != &d[0][0] || &a[0][0] == &c[0][0] {
		t.Fatal("the Cyrillic layout is not cached separately")
	}
}

// The labels are rebuilt when the results change, not on every frame.
func TestSearchTabLabelsFollowTheResults(t *testing.T) {
	s := NewSearchScreen()
	var songs []subsonic.Song
	for range searchPage {
		songs = append(songs, subsonic.Song{})
	}
	s.setResults(&subsonic.SearchResult{Songs: songs, Albums: []subsonic.Album{{}}}, "x")
	if got := s.tabs.Labels; got[resArtists] != "Artists 0" || got[resAlbums] != "Albums 1" || got[resTracks] != "Tracks 50+" {
		t.Fatalf("labels %q", got)
	}
	ta, s := tuneSearch(t, ProfileHDMI)
	for i := 0; s.more[resTracks] && i < 100; i++ {
		ta.press(input.BtnDown)
		ta.settle(t)
	}
	s.tabs.Labels[resTracks] = "stale" // Draw must leave them alone
	ta.settle(t)
	if s.tabs.Labels[resTracks] != "stale" {
		t.Fatal("Draw rewrote the labels")
	}
}

// The Resume card arrives after the screen is up: what the user has focused
// stays focused.
func TestCRTHomeKeepsFocusWhenResumeArrives(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1}
	s := NewHomeScreen()
	ta.Push(s)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // Most played, before Resume is known
	ta.settle(t)
	if s.resume == nil {
		t.Fatal("no Resume row")
	}
	if got := s.items()[s.list.Focus].label; got != "Most played" {
		t.Fatalf("focus moved to %q", got)
	}
}

// Once something else is playing, the saved queue must not be offered: A on
// it would replace the live queue.
func TestFeedDropsResumeOnceSomethingPlays(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1}
	s := NewFeedScreen()
	ta.Push(s)
	ta.settle(t)
	ta.press(input.BtnUp) // the Resume card
	if s.current() != nil {
		t.Fatal("not on the Resume card")
	}
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: -1}
	ta.settle(t)
	if s.resume != nil || s.count() != len(s.rows) || s.current() != s.rows[0] {
		t.Fatalf("Resume still offered: resume %v, count %d, current %v", s.resume, s.count(), s.current())
	}
	ta.press(input.BtnA) // opens the focused album instead
	if slices.Contains(ta.pl.calls, "resume") {
		t.Fatal("A resumed the old queue over the live one")
	}
	if _, ok := ta.Top().(*AlbumScreen); !ok {
		t.Fatalf("A opened %T", ta.Top())
	}
}
