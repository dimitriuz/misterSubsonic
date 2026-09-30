package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// partialApp is a test app on a display that takes partial frames.
func partialApp(t *testing.T, p Profile, s Screen) (*testApp, *rectDisplay) {
	t.Helper()
	ta := newTestApp(t, p)
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	ta.Push(NewHomeScreen())
	ta.Push(s)
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	return ta, d
}

// presented is the one partial frame d got since the last reset.
func presented(t *testing.T, d *rectDisplay) []gfx.Rect {
	t.Helper()
	if d.fulls != 0 || len(d.rects) != 1 {
		t.Fatalf("got %d full frames and partial frames %v, want one partial frame", d.fulls, d.rects)
	}
	rs := d.rects[0]
	d.rects, d.fulls = nil, 0
	return rs
}

// A focus move that doesn't scroll redraws the two rows it changed; one
// that scrolls redraws the list, not the header or the mini bar.
func TestListMovesRedrawTheirRows(t *testing.T) {
	s := NewAlbumScreen(sampleLibrary().albums[0])
	ta, d := partialApp(t, ProfileCRT240, s)
	ta.press(input.BtnDown)
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].H != 2*s.list.rowH || rs[0].Y != s.list.area.Y {
		t.Fatalf("a move down redrew %v, want rows 0 and 1 (height %d at y %d)", rs, 2*s.list.rowH, s.list.area.Y)
	}
}

// Moved names the old and new row, or the whole list when the move scrolls.
func TestListAndGridMoved(t *testing.T) {
	c := gfx.NewCanvas(400, 300)
	var l List
	if l.Moved() != nil {
		t.Fatal("an undrawn list reported damage")
	}
	area := gfx.R(10, 20, 300, 100) // 5 rows of 20
	l.Draw(c, area, 50, 20, func(int, gfx.Rect, bool) {})
	l.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 50)
	if got := l.Moved(); len(got) != 2 || got[0] != gfx.R(10, 20, 300, 20) || got[1] != gfx.R(10, 40, 300, 20) {
		t.Fatalf("down one = %v", got)
	}
	l.Handle(input.Event{Button: input.BtnR, Kind: input.Press}, 50) // a page: scrolls
	if got := l.Moved(); len(got) != 1 || got[0] != area {
		t.Fatalf("a page down = %v, want the list %v", got, area)
	}

	var g Grid
	garea := gfx.R(0, 0, 300, 200) // 3 columns of 100, 2 rows of 100
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnRight, Kind: input.Press}, 20)
	if got := g.Moved(); len(got) != 2 || got[0] != gfx.R(0, 0, 100, 100) || got[1] != gfx.R(100, 0, 100, 100) {
		t.Fatalf("right one = %v", got)
	}
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 20)
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 20) // into row 3: scrolls
	if got := g.Moved(); len(got) != 1 || got[0] != garea {
		t.Fatalf("down past the last visible row = %v, want the grid", got)
	}
}

// The progress tick while playing redraws the bar and the times only.
func TestProgressTickRedrawsTheBarAndTimes(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewNowPlayingScreen())
	playingState(ta)
	ta.dirty = true
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	ta.pl.st.Position += time.Second
	ta.now = ta.now.Add(progressTick)
	ta.onWake()
	ta.settle(t)
	rs := presented(t, d)
	if area(rs) >= ta.P.W*ta.P.H/10 {
		t.Fatalf("the tick redrew %v (%d px), want just the bar and times", rs, area(rs))
	}
}

// A scrolling title redraws its own line each frame, nothing else.
func TestMarqueeRedrawsItsLine(t *testing.T) {
	p := &probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, longTitle, colText, area, true)
	}}
	ta, d := partialApp(t, ProfileHDMI, p)
	ta.now = ta.now.Add(marqueeDelay + time.Second)
	ta.onWake()
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].W != 100 || rs[0].H != ta.F.Body.Height() {
		t.Fatalf("the marquee redrew %v, want its 100 px line", rs)
	}
}

// slowArt has a cover only once it is marked ready.
type slowArt struct{ ready map[subsonic.ID]bool }

func (s slowArt) Get(k art.Key) (*gfx.Image, bool) {
	if !s.ready[k.ID] {
		return nil, false
	}
	return fakeArt{}.Get(k)
}

// A cover that arrives redraws the cells that show it.
func TestArrivingCoverRedrawsItsCells(t *testing.T) {
	sa := slowArt{ready: map[subsonic.ID]bool{}}
	ta := newTestApp(t, ProfileHDMI)
	ta.o.Art = sa
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", "newest"))
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	id := ta.lib.albums[1].CoverArt
	sa.ready[id] = true
	ta.ArtReady(art.Key{ID: id, Size: ta.P.Cover})
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].W > ta.P.Cover+ta.P.Margin {
		t.Fatalf("the cover redrew %v, want its cell", rs)
	}
}

// A media volume key redraws the volume panel only, on Settings lists too.
func TestVolumeKeyRedrawsThePanel(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	ta.press(input.BtnVolUp)
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != ta.volumePanelRect() {
		t.Fatalf("redrew %v, want the panel %v", rs, ta.volumePanelRect())
	}
	ta.now = ta.now.Add(volumeShowTime)
	ta.onWake()
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != ta.volumePanelRect() {
		t.Fatalf("the panel going redrew %v", rs)
	}
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	ta.press(input.BtnVolDown)
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != ta.volumePanelRect() {
		t.Fatalf("on Settings → Playback redrew %v, want the panel %v", rs, ta.volumePanelRect())
	}
}

// Toasts redraw their own area when they appear and when they go.
func TestToastsRedrawTheirArea(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	ta.Toast("Added to queue: %s", "Радио Африка")
	ta.settle(t)
	shown := ta.toastsArea()
	if rs := presented(t, d); len(rs) != 1 || rs[0] != shown {
		t.Fatalf("a toast redrew %v, want %v", rs, shown)
	}
	ta.now = ta.now.Add(toastTime)
	ta.onWake()
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != shown {
		t.Fatalf("the toast going redrew %v, want %v", rs, shown)
	}
}

func area(rs []gfx.Rect) int {
	n := 0
	for _, r := range rs {
		n += r.W * r.H
	}
	return n
}

// The player moves its position between the 500 ms ticks. A partial frame
// drawn in between (a focus move) still shows the current position, and the
// verify pass agrees with it.
func TestPartialFrameKeepsTheProgressCurrent(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	ta.Push(NewHomeScreen())
	s := NewAlbumScreen(sampleLibrary().albums[0])
	ta.Push(s)
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	if len(ta.ticks) == 0 {
		t.Fatal("the mini bar marked no tick region")
	}
	ta.pl.st.Position += 40 * time.Second // as the player does, on its own
	ta.press(input.BtnDown)
	ta.settle(t)
	rs := presented(t, d)
	for _, tk := range ta.ticks {
		if !covered(rs, tk) {
			t.Fatalf("tick region %v not in the partial frame %v", tk, rs)
		}
	}
}

// A marquee keeps scrolling with the clock between its own frames.
func TestPartialFrameKeepsTheMarqueeCurrent(t *testing.T) {
	p := &probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, longTitle, colText, area, true)
	}}
	ta, d := partialApp(t, ProfileHDMI, p)
	ta.now = ta.now.Add(marqueeDelay + time.Second)
	ta.onWake()
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	ta.now = ta.now.Add(300 * time.Millisecond) // no wake yet
	ta.Damage(gfx.R(0, ta.P.H-10, 10, 10))
	ta.settle(t)
	rs := presented(t, d)
	if !covered(rs, ta.mqRect) {
		t.Fatalf("the marquee line %v not in the partial frame %v", ta.mqRect, rs)
	}
}

// covered reports whether r lies inside one of rs.
func covered(rs []gfx.Rect, r gfx.Rect) bool {
	for _, x := range rs {
		if r.Intersect(x) == r {
			return true
		}
	}
	return false
}
