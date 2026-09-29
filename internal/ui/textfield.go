package ui

import (
	"strings"

	"mistersubsonic/internal/gfx"
)

// drawTextField draws a one-line text field in r: the value with a caret
// (dots instead of characters when masked), or a dim hint when it is
// empty. A long value shows its end, where the typing is.
func (a *App) drawTextField(c *gfx.Canvas, r gfx.Rect, value []rune, hint string, masked bool) {
	p := a.P
	f := a.F.Body
	c.Fill(r, colPanel)
	x := r.X + p.Margin/2
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	inner := r.Inset(p.Margin / 4)
	if len(value) == 0 {
		f.Draw(c, x, y, f.Truncate(hint, r.W-p.Margin), colDim, inner)
		return
	}
	text := string(value)
	if masked {
		text = strings.Repeat("•", len(value))
	}
	for f.Measure(text) > r.W-p.Margin-f.Ascent()/2 && len(text) > 0 {
		_, size := firstRune(text)
		text = text[size:]
	}
	end := f.Draw(c, x, y, text, colText, inner)
	c.Fill(gfx.R(end+1, y-f.Ascent(), max(f.Ascent()/8, 2), f.Ascent()+f.Descent()), colAccent)
}

// firstRune returns the second rune's offset (the first rune's size).
func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}
