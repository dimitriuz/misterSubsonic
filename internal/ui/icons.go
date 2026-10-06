package ui

import (
	"math"
	"sort"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
)

type icon int

const (
	iconPlay icon = iota
	iconPause
	iconStop
	iconBusy
	iconStar
)

// drawIcon draws a simple vector icon filling the square r. Icons are drawn
// rather than taken from the font: Noto Sans has no media symbols.
func drawIcon(c *gfx.Canvas, ic icon, r gfx.Rect, col gfx.Color) {
	switch ic {
	case iconPlay: // right-pointing triangle
		mid := r.H / 2
		for y := 0; y < r.H; y++ {
			d := y - mid
			if d < 0 {
				d = -d
			}
			w := r.W * (mid - d) / max(mid, 1)
			c.Fill(gfx.R(r.X, r.Y+y, w, 1), col)
		}
	case iconPause:
		bw := max(r.W/3, 1)
		c.Fill(gfx.R(r.X, r.Y, bw, r.H), col)
		c.Fill(gfx.R(r.Right()-bw, r.Y, bw, r.H), col)
	case iconStop:
		c.Fill(r, col)
	case iconBusy: // three dots
		d := max(r.W/5, 1)
		for i := 0; i < 3; i++ {
			c.Fill(gfx.R(r.X+i*2*d, r.Y+(r.H-d)/2, d, d), col)
		}
	case iconStar:
		fillPolygon(c, starPoints(r), col)
	}
}

// starPoints is a five-pointed star in r: ten vertices, outer and inner.
func starPoints(r gfx.Rect) [][2]float64 {
	cx, cy := float64(r.X)+float64(r.W)/2, float64(r.Y)+float64(r.H)/2
	outer := float64(min(r.W, r.H)) / 2
	pts := make([][2]float64, 10)
	for i := range pts {
		rad := outer
		if i%2 == 1 {
			rad *= 0.4
		}
		a := -math.Pi/2 + float64(i)*math.Pi/5
		pts[i] = [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)}
	}
	return pts
}

// polyRows counts the scanlines fillPolygon worked on (a test reads it to
// see that shapes outside the clip cost nothing).
var polyRows int

// fillPolygon fills the pixels whose centres are inside pts (even-odd rule).
// Rows and columns outside the canvas clip are skipped before any maths: a
// partial frame draws the whole screen clipped, so most shapes are outside.
func fillPolygon(c *gfx.Canvas, pts [][2]float64, col gfx.Color) {
	minX, maxX, minY, maxY := pts[0][0], pts[0][0], pts[0][1], pts[0][1]
	for _, p := range pts {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	clip := c.Clip()
	if clip.Empty() || maxX+1 < float64(clip.X) || minX-1 > float64(clip.Right()) ||
		maxY < float64(clip.Y) || minY > float64(clip.Bottom()) {
		return
	}
	for y := max(int(minY), clip.Y); y <= min(int(maxY), clip.Bottom()-1); y++ {
		polyRows++
		py := float64(y) + 0.5
		var xbuf [16]float64 // on the stack: no allocation per scanline
		xs := xbuf[:0]
		for i := range pts {
			a, b := pts[i], pts[(i+1)%len(pts)]
			if (a[1] <= py) != (b[1] <= py) {
				xs = append(xs, a[0]+(py-a[1])*(b[0]-a[0])/(b[1]-a[1]))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0, x1 := int(math.Round(xs[i])), int(math.Round(xs[i+1]))
			c.Fill(gfx.R(x0, y, x1-x0, 1), col)
		}
	}
}

func statusIcon(s player.Status) icon {
	switch s {
	case player.Playing:
		return iconPlay
	case player.Paused:
		return iconPause
	case player.Loading, player.Buffering:
		return iconBusy
	}
	return iconStop
}

func statusLabel(s player.Status) string {
	switch s {
	case player.Playing:
		return "Playing"
	case player.Paused:
		return "Paused"
	case player.Loading:
		return "Loading…"
	case player.Buffering:
		return "Buffering…"
	}
	return "Stopped"
}

// iconText draws an icon sized to font f followed by text; returns the end x.
func iconText(c *gfx.Canvas, f *gfx.Font, ic icon, x, baseline int, text string, col gfx.Color, clip gfx.Rect) int {
	s := f.Ascent() * 2 / 3
	drawIcon(c, ic, gfx.R(x, baseline-s, s, s), col)
	return f.Draw(c, x+s+s/2, baseline, text, col, clip)
}
