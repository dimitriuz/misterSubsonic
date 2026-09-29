package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// drawTextRow draws a thumbnail (if id != "" and the profile has thumbs),
// a main line and an optional dim second line into r.
func (a *App) drawTextRow(c *gfx.Canvas, r gfx.Rect, coverID subsonic.ID, withThumb bool, main, sub string, mainCol gfx.Color) {
	p := a.P
	x := r.X + p.Margin
	if withThumb && p.Thumb > 0 {
		t := min(p.Thumb, r.H-2)
		a.drawArt(c, coverID, gfx.R(x, r.Y+(r.H-t)/2, t, t))
		x += t + p.Margin/2
	}
	w := r.Right() - p.Margin - x
	fb, fs := a.F.Body, a.F.Small
	if sub == "" {
		fb.Draw(c, x, r.Y+(r.H+fb.Ascent()-fb.Descent())/2, fb.Truncate(main, w), mainCol, r)
		return
	}
	total := fb.Height() + fs.Height()
	y := r.Y + (r.H-total)/2
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(main, w), mainCol, r)
	fs.Draw(c, x, y+fb.Height()+fs.Ascent(), fs.Truncate(sub, w), colDim, r)
}

// drawCentered draws a dim message in the middle of area (loading, empty, error).
func (a *App) drawCentered(c *gfx.Canvas, area gfx.Rect, text string, col gfx.Color) {
	f := a.F.Body
	t := f.Truncate(text, area.W-2*a.P.Margin)
	f.Draw(c, area.X+(area.W-f.Measure(t))/2, area.Y+area.H/2+f.Ascent()/2, t, col, area)
}
