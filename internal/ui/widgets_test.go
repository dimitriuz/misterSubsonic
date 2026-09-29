package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func press(b input.Button) input.Event { return input.Event{Button: b, Kind: input.Press} }

func TestGridNavigation(t *testing.T) {
	var g Grid
	c := gfx.NewCanvas(300, 200)
	var drawn []int
	draw := func() {
		drawn = nil
		g.Draw(c, c.Bounds(), 7, 100, 100, func(i int, r gfx.Rect, focused bool) { drawn = append(drawn, i) })
	}
	draw() // 3 columns, 2 rows visible
	steps := []struct {
		b    input.Button
		ok   bool
		want int
	}{
		{input.BtnRight, true, 1}, {input.BtnRight, true, 2}, {input.BtnRight, false, 2}, // right edge
		{input.BtnUp, false, 2},                            // first row: tabs can take it
		{input.BtnDown, true, 5}, {input.BtnDown, true, 6}, // into the short last row
		{input.BtnDown, false, 6}, {input.BtnLeft, false, 6}, // first column: the sidebar can take it
		{input.BtnUp, true, 3}, {input.BtnL, true, 0}, {input.BtnR, true, 6},
	}
	for i, s := range steps {
		if ok := g.Handle(press(s.b), 7); ok != s.ok || g.Focus != s.want {
			t.Fatalf("step %d (%v): ok %v focus %d, want %v %d", i, s.b, ok, g.Focus, s.ok, s.want)
		}
	}
	draw()
	if !slices.Equal(drawn, []int{3, 4, 5, 6}) {
		t.Fatalf("drawn %v, want rows 1-2 (the focused row is visible)", drawn)
	}
	if !g.NearEnd(7) {
		t.Fatal("NearEnd false on the last row")
	}
	if (&Grid{}).Handle(press(input.BtnDown), 0) {
		t.Fatal("empty grid used a key")
	}
}

func TestListAndTabsLetNeighboursTakeFocus(t *testing.T) {
	var l List
	if l.Handle(press(input.BtnUp), 3) {
		t.Fatal("Up on the first row was used")
	}
	l.Focus = 2
	if l.Handle(press(input.BtnDown), 3) {
		t.Fatal("Down on the last row was used")
	}
	tabs := Tabs{Labels: []string{"A", "B"}}
	if tabs.Handle(press(input.BtnLeft)) {
		t.Fatal("Left on the first tab was used")
	}
	if !tabs.Handle(press(input.BtnRight)) || !tabs.Handle(press(input.BtnRight)) || tabs.Sel != 1 {
		t.Fatalf("tabs: sel %d", tabs.Sel)
	}
}

func TestStarIcon(t *testing.T) {
	c := gfx.NewCanvas(21, 21)
	drawIcon(c, iconStar, c.Bounds(), colAccent)
	if c.At(10, 10) != colAccent || c.At(10, 1) != colAccent {
		t.Fatal("star centre or top point not filled")
	}
	if c.At(0, 0) != gfx.RGB(0, 0, 0) || c.At(20, 20) != gfx.RGB(0, 0, 0) || c.At(10, 20) != gfx.RGB(0, 0, 0) {
		t.Fatal("star filled a corner or between the lower points")
	}
}

func TestToggleStarRemembersAndToasts(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	it := albumStar(ta.lib.albums[1])
	ta.toggleStar(s, it)
	ta.settle(t)
	if !ta.isStarred(it) || ta.toasts[0].text != "Starred Homogenic" {
		t.Fatalf("starred %v, toasts %v", ta.isStarred(it), ta.toasts)
	}
	ta.toggleStar(s, it)
	ta.settle(t)
	if ta.isStarred(it) || !slices.Equal(ta.lib.stars, []string{"star al-2", "unstar al-2"}) {
		t.Fatalf("starred %v, calls %v", ta.isStarred(it), ta.lib.stars)
	}
	ta.lib.err = errOffline
	ta.toggleStar(s, it)
	ta.settle(t)
	if ta.isStarred(it) {
		t.Fatal("a failed star changed the state")
	}
	if last := ta.toasts[len(ta.toasts)-1].text; last != "Couldn't star Homogenic: error" {
		t.Fatalf("toast %q", last)
	}
}

func TestMenuRunsEntryAndCloses(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	parent := &probe{}
	ta.Push(parent)
	var ran []string
	entries := []menuEntry{
		{"One", func(a *App) { ran = append(ran, "one") }},
		{"Two", func(a *App) { ran = append(ran, "two:"+a.Top().Title()) }},
	}
	ta.Push(NewMenuScreen(parent, "Item", entries))
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // stays on the last entry
	ta.press(input.BtnY)    // swallowed: the menu is modal
	ta.press(input.BtnA)
	if !slices.Equal(ran, []string{"two:Probe"}) || ta.Top() != parent {
		t.Fatalf("ran %v, top %T", ran, ta.Top())
	}
	ta.Push(NewMenuScreen(parent, "Item", entries))
	ta.press(input.BtnStart) // global play/pause still works
	ta.press(input.BtnX)
	if ta.Top() != parent || len(ran) != 1 || !slices.Equal(ta.pl.calls, []string{"toggle"}) {
		t.Fatalf("top %T, ran %v, player %v", ta.Top(), ran, ta.pl.calls)
	}
}

func TestGoldenMenu(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		album := NewAlbumScreen(ta.lib.albums[0])
		ta.Push(album)
		ta.settle(t)
		noop := func(*App) {}
		ta.Push(NewMenuScreen(album, "Радио Африка", []menuEntry{
			{"Play now", noop}, {"Play next", noop}, {"Add to queue", noop}, {"Star", noop}, {"Go to artist", noop},
		}))
		ta.press(input.BtnDown)
		golden(t, "menu-"+p.Name, ta.settle(t))
	}
}
