package ui

import (
	"fmt"
	"math"
	"time"

	"mistersubsonic/internal/gfx"
)

// The volume indicator: a panel that shows the level for a moment whenever
// the volume or mute changes (from any screen or key).

// volumeShowTime is how long the panel stays after the last change.
const volumeShowTime = 1500 * time.Millisecond

// volumeSegments is how many steps the bar is drawn in.
const volumeSegments = 20

// volumeLevel is the volume as 0..1. The bar is linear in dB (−60 dB is
// empty, 0 dB full), so every 1 dB step moves it by the same amount.
func volumeLevel(db float64) float64 { return max(0, min(1, (db+60)/60)) }

// showVolume brings up the panel (again) for volumeShowTime.
func (a *App) showVolume() {
	a.volumeUntil = a.o.Now().Add(volumeShowTime)
	a.dirty = true
}

// drawSpeaker draws a speaker filling the square r: the box and cone, then
// one to three waves by level, or a cross when muted.
func drawSpeaker(c *gfx.Canvas, r gfx.Rect, level float64, muted bool, col gfx.Color) {
	x, y, s := float64(r.X), float64(r.Y), float64(min(r.W, r.H))
	cy := y + s/2
	fillPolygon(c, [][2]float64{ // box and cone as one outline
		{x, cy - s*0.16}, {x + s*0.2, cy - s*0.16}, {x + s*0.45, cy - s*0.4},
		{x + s*0.45, cy + s*0.4}, {x + s*0.2, cy + s*0.16}, {x, cy + s*0.16},
	}, col)
	if muted {
		t := max(s*0.09, 1.5) // an X to the right of the cone
		fillLine(c, x+s*0.6, cy-s*0.17, x+s*0.94, cy+s*0.17, t, col)
		fillLine(c, x+s*0.6, cy+s*0.17, x+s*0.94, cy-s*0.17, t, col)
		return
	}
	waves := 0
	switch {
	case level > 0.67:
		waves = 3
	case level > 0.34:
		waves = 2
	case level > 0:
		waves = 1
	}
	cx, t := x+s*0.45, max(s*0.07, 1)
	for i := range waves {
		rad := s * (0.2 + 0.16*float64(i))
		var pts [][2]float64
		const n = 8
		for j := 0; j <= n; j++ { // outer edge, top to bottom
			ang := -math.Pi/4 + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + (rad+t)*math.Cos(ang), cy + (rad+t)*math.Sin(ang)})
		}
		for j := n; j >= 0; j-- { // inner edge back up
			ang := -math.Pi/4 + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + rad*math.Cos(ang), cy + rad*math.Sin(ang)})
		}
		fillPolygon(c, pts, col)
	}
}

// drawVolumeBar draws the level as segs segments across r: lit up to the
// level, dark after it, and lit in grey when muted.
func drawVolumeBar(c *gfx.Canvas, r gfx.Rect, level float64, muted bool, segs int) {
	lit := int(math.Round(level * float64(segs)))
	gap := max(r.W/(segs*5), 1)
	for i := range segs {
		x0 := r.X + r.W*i/segs
		x1 := r.X + r.W*(i+1)/segs - gap
		col := colArtBg
		if i < lit {
			col = colAccent
			if muted {
				col = colDim
			}
		}
		c.Fill(gfx.R(x0, r.Y, max(x1-x0, 1), r.H), col)
	}
}

// fillLine draws a straight stroke t wide from (x0, y0) to (x1, y1).
func fillLine(c *gfx.Canvas, x0, y0, x1, y1, t float64, col gfx.Color) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*t/2, dx/l*t/2
	fillPolygon(c, [][2]float64{{x0 + nx, y0 + ny}, {x1 + nx, y1 + ny}, {x1 - nx, y1 - ny}, {x0 - nx, y0 - ny}}, col)
}

// fillRoundRect fills r with corners rounded by rad.
func fillRoundRect(c *gfx.Canvas, r gfx.Rect, rad int, col gfx.Color) {
	rad = min(rad, r.W/2, r.H/2)
	if rad <= 0 {
		c.Fill(r, col)
		return
	}
	var pts [][2]float64
	const n = 6
	corner := func(cx, cy, from float64) {
		for j := 0; j <= n; j++ {
			ang := from + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + float64(rad)*math.Cos(ang), cy + float64(rad)*math.Sin(ang)})
		}
	}
	x0, y0 := float64(r.X+rad), float64(r.Y+rad)
	x1, y1 := float64(r.Right()-rad), float64(r.Bottom()-rad)
	corner(x1, y0, -math.Pi/2)
	corner(x1, y1, 0)
	corner(x0, y1, math.Pi/2)
	corner(x0, y0, math.Pi)
	fillPolygon(c, pts, col)
}

// drawVolumePanel draws the volume panel centred under the header while it
// is due: the speaker, the bar, and the level (0–100) or "Muted".
func (a *App) drawVolumePanel(c *gfx.Canvas) {
	if a.volumeUntil.IsZero() || !a.o.Now().Before(a.volumeUntil) {
		return
	}
	p, f := a.P, a.F.Body
	db := a.volumeDB()
	level := volumeLevel(db)
	label := fmt.Sprintf("%d", int(math.Round(level*100)))
	if a.muted {
		label = "Muted"
	}
	icon := f.Height()
	barW := max(p.W/4, 6*icon)
	labelW := f.Measure("Muted")
	pad := p.Margin / 2
	w := pad + icon + pad + barW + pad + labelW + pad
	h := icon + 2*pad
	panel := gfx.R((p.W-w)/2, p.SafeY+p.HeaderH+p.Margin/2, w, h)
	fillRoundRect(c, panel, pad, colPanel.WithAlpha(0xF0))
	x := panel.X + pad
	drawSpeaker(c, gfx.R(x, panel.Y+pad, icon, icon), level, a.muted, colText)
	x += icon + pad
	bh := max(icon*2/5, 3)
	drawVolumeBar(c, gfx.R(x, panel.Y+pad+(icon-bh)/2, barW, bh), level, a.muted, volumeSegments)
	x += barW + pad
	col := colText
	if a.muted {
		col = colDim
	}
	f.Draw(c, x+(labelW-f.Measure(label))/2, panel.Y+pad+(icon+f.Ascent()-f.Descent())/2, label, col, c.Bounds())
}
