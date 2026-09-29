package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// drawTextRow draws a thumbnail (if id != "" and the profile has thumbs),
// a main line and an optional dim second line into r.
func (a *App) drawTextRow(c *gfx.Canvas, r gfx.Rect, coverID subsonic.ID, withThumb bool, main, sub string, mainCol gfx.Color) {
	a.drawRow(c, r, row{cover: coverID, thumb: withThumb, main: main, sub: sub, col: mainCol})
}

// row is one list row: an optional thumbnail, a main line (scrolling when
// focused and too long), an optional dim second line, and on the right an
// optional star and dim text (a duration).
type row struct {
	cover     subsonic.ID
	thumb     bool
	main, sub string
	col       gfx.Color // main line; 0 = colText
	focused   bool
	starred   bool
	right     string
}

func (a *App) drawRow(c *gfx.Canvas, r gfx.Rect, w row) {
	p := a.P
	if w.col == 0 {
		w.col = colText
	}
	x := r.X + p.Margin
	if w.thumb && p.Thumb > 0 {
		t := min(p.Thumb, r.H-2)
		a.drawArt(c, w.cover, gfx.R(x, r.Y+(r.H-t)/2, t, t))
		x += t + p.Margin/2
	}
	right := r.Right() - p.Margin
	fb, fs := a.F.Body, a.F.Small
	mid := r.Y + (r.H+fb.Ascent()-fb.Descent())/2
	if w.right != "" {
		rw := fb.Measure(w.right)
		fb.Draw(c, right-rw, mid, w.right, colDim, r)
		right -= rw + p.Margin/2
	}
	if w.starred {
		s := fb.Ascent() * 3 / 4
		drawIcon(c, iconStar, gfx.R(right-s, mid-fb.Ascent()/2-s/2, s, s), colAccent)
		right -= s + p.Margin/2
	}
	width := right - x
	if w.sub == "" {
		a.drawFit(c, fb, x, mid, width, w.main, w.col, r, w.focused)
		return
	}
	total := fb.Height() + fs.Height()
	y := r.Y + (r.H-total)/2
	a.drawFit(c, fb, x, y+fb.Ascent(), width, w.main, w.col, r, w.focused)
	fs.Draw(c, x, y+fb.Height()+fs.Ascent(), fs.Truncate(w.sub, width), colDim, r)
}

// coverCell is the size of one cell in a cover grid.
func (a *App) coverCell() (w, h int) {
	p := a.P
	pad := p.Margin / 3
	return p.Cover + 2*pad, pad + p.Cover + pad/2 + 2*a.F.Small.Height() + pad
}

// drawCoverCell draws a grid cell: the cover, a title (scrolling when
// focused) and a dim second line.
func (a *App) drawCoverCell(c *gfx.Canvas, r gfx.Rect, cover subsonic.ID, title, sub string, focused, starred bool) {
	p := a.P
	pad := p.Margin / 3
	if focused {
		c.Fill(r, colFocus)
	}
	art := p.Cover
	x := r.X + (r.W-art)/2
	a.drawArt(c, cover, gfx.R(x, r.Y+pad, art, art))
	if starred {
		s := art / 7
		badge := gfx.R(x+art-s-pad, r.Y+2*pad, s+pad/2, s+pad/2)
		c.Fill(badge, colOverlay)
		drawIcon(c, iconStar, badge.Inset(pad/4), colAccent)
	}
	fs := a.F.Small
	y := r.Y + pad + art + pad/2 + fs.Ascent()
	a.drawFit(c, fs, x, y, art, title, colText, r, focused)
	fs.Draw(c, x, y+fs.Height(), fs.Truncate(sub, art), colDim, r)
}

// drawCentered draws a dim message in the middle of area (loading, empty, error).
func (a *App) drawCentered(c *gfx.Canvas, area gfx.Rect, text string, col gfx.Color) {
	f := a.F.Body
	t := f.Truncate(text, area.W-2*a.P.Margin)
	f.Draw(c, area.X+(area.W-f.Measure(t))/2, area.Y+area.H/2+f.Ascent()/2, t, col, area)
}
