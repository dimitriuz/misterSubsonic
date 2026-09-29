package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
)

type icon int

const (
	iconPlay icon = iota
	iconPause
	iconStop
	iconBusy
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
