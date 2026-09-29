package ui

import (
	"log"
	"time"

	"mistersubsonic/internal/gfx"
)

// The overwrite watchdog (spec §8.1): every watchdogEvery, a display that
// can check itself (the MiSTer framebuffer) is asked whether its last frame
// is still on screen. If Main_MiSTer or the console drew over it, the whole
// screen is repainted.
const watchdogEvery = 2 * time.Second

// checkScreen runs the watchdog when it is due.
func (a *App) checkScreen(now time.Time) {
	chk, ok := a.o.Display.(gfx.Checker)
	if !ok || a.checkAt.IsZero() || now.Before(a.checkAt) {
		return
	}
	a.checkAt = now.Add(watchdogEvery)
	if chk.Intact() {
		a.overwritten = false
		return
	}
	if !a.overwritten { // once per overwrite, not every 2 s while it lasts
		log.Print("ui: something drew over the screen; repainting")
	}
	a.overwritten, a.dirty = true, true
}
