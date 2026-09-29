package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// List is a vertical, focusable, scrolling list. Rows are drawn by the owner.
type List struct {
	Focus int
	top   int
	rows  int // visible rows at the last Draw
}

// Handle moves the focus: Up/Down by one, L/R by a page. It reports whether
// the event was used.
func (l *List) Handle(e input.Event, n int) bool {
	if n == 0 {
		return false
	}
	page := max(l.rows-1, 1)
	switch e.Button {
	case input.BtnUp:
		l.Focus--
	case input.BtnDown:
		l.Focus++
	case input.BtnL:
		l.Focus -= page
	case input.BtnR:
		l.Focus += page
	default:
		return false
	}
	l.Focus = min(max(l.Focus, 0), n-1)
	return true
}

// Draw lays out n rows of height rowH in area, keeping the focus visible,
// and calls row for each visible index.
func (l *List) Draw(c *gfx.Canvas, area gfx.Rect, n, rowH int, row func(i int, r gfx.Rect, focused bool)) {
	l.rows = max(area.H/rowH, 1)
	l.Focus = min(max(l.Focus, 0), max(n-1, 0))
	if l.Focus < l.top {
		l.top = l.Focus
	}
	if l.Focus >= l.top+l.rows {
		l.top = l.Focus - l.rows + 1
	}
	l.top = max(min(l.top, n-l.rows), 0)
	for i := l.top; i < n && i < l.top+l.rows; i++ {
		r := gfx.R(area.X, area.Y+(i-l.top)*rowH, area.W, rowH)
		if i == l.Focus {
			c.Fill(r, colFocus)
		}
		row(i, r, i == l.Focus)
	}
}

// NearEnd reports whether the focus is within a page of the end (to load more).
func (l *List) NearEnd(n int) bool { return n > 0 && l.Focus >= n-max(l.rows, 1) }
