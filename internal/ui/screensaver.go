package ui

import (
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
)

// The screensaver (spec §8.2): after display.screensaver_minutes without
// input on Now Playing, the screen goes dark and the cover drifts slowly
// (burn-in on plasma and OLED TVs, and CRTs). Any input wakes it, and that
// press does nothing else. It redraws only every saverStep.

const (
	saverStep  = 2 * time.Second
	saverDrift = 6 // px per step (logical)
)

func (a *App) saverMinutes() int {
	if a.cfg == nil {
		return config.Default().Display.ScreensaverMinutes
	}
	return a.cfg.Display.ScreensaverMinutes
}

// saverDue is when the screensaver starts, or zero if it can't now.
func (a *App) saverDue() time.Time {
	m := a.saverMinutes()
	if _, np := a.Top().(*NowPlayingScreen); !np || m <= 0 || a.saver {
		return time.Time{}
	}
	return a.lastInput.Add(time.Duration(m) * time.Minute)
}

// wake ends the screensaver; it reports whether it was on.
func (a *App) wake() bool {
	on := a.saver
	a.saver = false
	if on {
		a.dirty = true
	}
	return on
}

// bounce folds x into 0..span, going back and forth.
func bounce(x, span int) int {
	if span <= 0 {
		return 0
	}
	x %= 2 * span
	if x > span {
		x = 2*span - x
	}
	return x
}

func (a *App) drawSaver(c *gfx.Canvas) {
	c.Clear(gfx.RGB(0, 0, 0))
	pl := a.Player()
	if pl == nil {
		return
	}
	song, ok := a.state().Current()
	if !ok {
		return
	}
	p := a.P
	art := p.ArtNow / 2
	steps := int(a.clock().Sub(a.saverSince) / saverStep)
	x := bounce(steps*saverDrift, p.W-art)
	y := bounce(steps*saverDrift*2/3, p.H-art-a.F.Small.Height())
	r := gfx.R(x, y, art, art)
	a.drawArt(c, song.CoverArt, r)
	c.Fill(r, gfx.RGBA(0, 0, 0, 0x90)) // dimmed
	fs := a.F.Small
	fs.Draw(c, x, r.Bottom()+fs.Ascent(), fs.Truncate(song.Title, art), colDim.WithAlpha(0x80), c.Bounds())
}
