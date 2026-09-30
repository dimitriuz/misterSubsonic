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

	// Where the last Draw put the rows, and the focus before the last
	// Handle: what Moved needs to tell which rows a move changed.
	area gfx.Rect
	rowH int
	prev int
}

// Handle moves the focus: Up/Down by one, L/R by a page. It reports whether
// the event was used; Up on the first row and Down on the last aren't, so a
// neighbour (tabs above, a keyboard) can take the focus.
func (l *List) Handle(e input.Event, n int) bool {
	if n == 0 {
		return false
	}
	page := max(l.rows-1, 1)
	l.prev = l.Focus
	switch e.Button {
	case input.BtnUp:
		if l.Focus <= 0 {
			return false
		}
		l.Focus--
	case input.BtnDown:
		if l.Focus >= n-1 {
			return false
		}
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
	l.area, l.rowH = area, rowH
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

// Moved is what the last Handle changed on screen: the old and the new
// focused row, or the whole list when the move scrolls it. It is nil before
// the list was drawn.
func (l *List) Moved() []gfx.Rect {
	if l.rowH == 0 || l.area.Empty() {
		return nil
	}
	if l.Focus < l.top || l.Focus >= l.top+l.rows {
		return []gfx.Rect{l.area}
	}
	row := func(i int) gfx.Rect { return gfx.R(l.area.X, l.area.Y+(i-l.top)*l.rowH, l.area.W, l.rowH) }
	return []gfx.Rect{row(l.prev), row(l.Focus)}
}

// NearEnd reports whether the focus is within a page of the end (to load more).
func (l *List) NearEnd(n int) bool { return n > 0 && l.Focus >= n-max(l.rows, 1) }
