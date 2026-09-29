package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Tabs is a row of labels above a screen's content (Albums: A–Z / by year /
// by genre; Starred; search results). Left/Right switch while it has focus.
type Tabs struct {
	Labels []string
	Sel    int
}

// Handle switches tabs; false at the left end so the sidebar can take focus.
func (t *Tabs) Handle(e input.Event) bool {
	switch e.Button {
	case input.BtnLeft:
		if t.Sel == 0 {
			return false
		}
		t.Sel--
	case input.BtnRight:
		t.Sel = min(t.Sel+1, len(t.Labels)-1)
	default:
		return false
	}
	return true
}

// Draw draws the labels left to right in r; the selected one is underlined,
// and highlighted when the row has the focus.
func (t *Tabs) Draw(a *App, c *gfx.Canvas, r gfx.Rect, focused bool) {
	p := a.P
	f := a.F.Body
	x := r.X + p.Margin
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	for i, l := range t.Labels {
		w := f.Measure(l)
		col := colDim
		if i == t.Sel {
			col = colText
			if focused {
				c.Fill(gfx.R(x-p.Margin/4, r.Y, w+p.Margin/2, r.H), colFocus)
			}
			c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/8, 2), w, max(p.Margin/8, 2)), colAccent)
		}
		f.Draw(c, x, y, l, col, r)
		x += w + p.Margin
	}
}
