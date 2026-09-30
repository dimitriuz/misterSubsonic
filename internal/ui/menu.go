package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

type menuEntry struct {
	label string
	run   func(a *App)
}

// MenuScreen is a context menu (X) drawn over the screen it was opened
// from. A runs the focused entry, B or X closes it; Start and the media keys stay global.
type MenuScreen struct {
	parent  Screen
	title   string
	entries []menuEntry
	list    List
}

func NewMenuScreen(parent Screen, title string, entries []menuEntry) *MenuScreen {
	return &MenuScreen{parent: parent, title: title, entries: entries}
}

func (s *MenuScreen) Title() string { return s.parent.Title() }
func (s *MenuScreen) Enter(a *App)  {}

func (s *MenuScreen) Handle(a *App, e input.Event) bool {
	if isMediaButton(e.Button) {
		return false // volume, play/pause and the rest work under the menu
	}
	if s.list.Handle(e, len(s.entries)) {
		return true
	}
	if e.Kind != input.Press {
		return true
	}
	switch e.Button {
	case input.BtnA:
		// Close first: the entry may open a screen or load under the parent.
		a.Pop()
		if len(s.entries) > 0 {
			s.entries[s.list.Focus].run(a)
		}
	case input.BtnB, input.BtnX:
		a.Pop()
	case input.BtnStart:
		return false
	}
	return true
}

func (s *MenuScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	a.drawDimmed(true, func() { s.parent.Draw(a, c, area) })
	c.Fill(area, colOverlay)
	p := a.P
	fs := a.F.Small
	w := min(area.W-2*p.Margin, p.W*2/5+2*p.Margin)
	titleH := fs.Height() + p.Margin/2
	h := min(titleH+len(s.entries)*p.RowH+p.Margin/2, area.H-p.Margin)
	panel := gfx.R(area.X+(area.W-w)/2, area.Y+(area.H-h)/2, w, h)
	c.Fill(panel, colPanel)
	fs.Draw(c, panel.X+p.Margin, panel.Y+p.Margin/4+fs.Ascent(), fs.Truncate(s.title, w-2*p.Margin), colDim, panel)
	rows := gfx.R(panel.X, panel.Y+titleH, panel.W, panel.H-titleH-p.Margin/4)
	s.list.Draw(c, rows, len(s.entries), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawTextRow(c, r, "", false, s.entries[i].label, "", colText)
	})
}
