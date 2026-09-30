package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Grid is a focusable grid of equal cells that scrolls by rows (HDMI cover
// grids). Like List, it returns false for moves it can't make, so a
// neighbour can take the focus: Left in the first column (the sidebar), Up
// in the first row (tabs).
type Grid struct {
	Focus int
	top   int // first visible row
	cols  int // at the last Draw
	rows  int

	// Where the last Draw put the cells, and the focus before the last
	// Handle (for Moved).
	area         gfx.Rect
	x0           int
	cellW, cellH int
	prev         int
}

func (g *Grid) columns() int { return max(g.cols, 1) }

// Handle moves the focus: arrows by one cell, L/R by a page.
func (g *Grid) Handle(e input.Event, n int) bool {
	if n == 0 {
		return false
	}
	cols := g.columns()
	f := g.Focus
	g.prev = f
	switch e.Button {
	case input.BtnUp:
		if f < cols {
			return false
		}
		f -= cols
	case input.BtnDown:
		if f/cols == (n-1)/cols {
			return false
		}
		f = min(f+cols, n-1) // into a shorter last row: its last cell
	case input.BtnLeft:
		if f%cols == 0 {
			return false
		}
		f--
	case input.BtnRight:
		if f%cols == cols-1 || f == n-1 {
			return false
		}
		f++
	case input.BtnL:
		f = max(f-cols*max(g.rows, 1), f%cols)
	case input.BtnR:
		f = min(f+cols*max(g.rows, 1), n-1)
	default:
		return false
	}
	g.Focus = f
	return true
}

// Draw lays out n cells of cellW×cellH in area (centred horizontally),
// keeping the focused row visible, and calls cell for each visible one.
func (g *Grid) Draw(c *gfx.Canvas, area gfx.Rect, n, cellW, cellH int, cell func(i int, r gfx.Rect, focused bool)) {
	g.cols = max(area.W/cellW, 1)
	g.rows = max(area.H/cellH, 1)
	g.Focus = min(max(g.Focus, 0), max(n-1, 0))
	row := g.Focus / g.cols
	if row < g.top {
		g.top = row
	}
	if row >= g.top+g.rows {
		g.top = row - g.rows + 1
	}
	x0 := area.X + (area.W-g.cols*cellW)/2
	g.area, g.x0, g.cellW, g.cellH = area, x0, cellW, cellH
	for i := g.top * g.cols; i < n && i < (g.top+g.rows)*g.cols; i++ {
		r := gfx.R(x0+i%g.cols*cellW, area.Y+(i/g.cols-g.top)*cellH, cellW, cellH)
		cell(i, r, i == g.Focus)
	}
}

// Moved is what the last Handle changed on screen: the old and the new
// focused cell, or the whole grid when the move scrolls it. It is nil
// before the grid was drawn.
func (g *Grid) Moved() []gfx.Rect {
	if g.cellH == 0 || g.area.Empty() {
		return nil
	}
	cols := g.columns()
	if row := g.Focus / cols; row < g.top || row >= g.top+g.rows {
		return []gfx.Rect{g.area}
	}
	cell := func(i int) gfx.Rect {
		return gfx.R(g.x0+i%cols*g.cellW, g.area.Y+(i/cols-g.top)*g.cellH, g.cellW, g.cellH)
	}
	return []gfx.Rect{cell(g.prev), cell(g.Focus)}
}

// NearEnd reports whether the focus is within a page of the end (to load more).
func (g *Grid) NearEnd(n int) bool {
	return n > 0 && g.Focus >= n-g.columns()*max(g.rows, 1)
}
