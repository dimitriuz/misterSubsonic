package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
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
	ta.toggleStar(s, artist)
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
