package ui

import (
	"fmt"
	"log"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// Partial redraws (Plan 4b). A change that knows exactly which part of the
// screen it affects calls Damage instead of marking the whole frame dirty.
// render then runs the normal drawing code clipped to the damaged areas and
// copies only those areas to the framebuffer. The drawing logic always runs
// in full, so a partial frame differs from a full one only outside the
// damage.

// maxDamageRects is how many separate areas a partial frame redraws; more
// than that and they are merged into their bounding box.
const maxDamageRects = 4

// clock is the time drawing sees: fixed for the whole frame (every damaged
// area and the verify pass), the real clock outside one.
func (a *App) clock() time.Time {
	if !a.frameNow.IsZero() {
		return a.frameNow
	}
	return a.o.Now()
}

// state is the player's state as drawing sees it: read once at the start of
// a frame (like the clock), so every damaged area and the verify pass show
// the same position; live outside a frame. Zero without a player.
func (a *App) state() player.State {
	if a.frameState != nil {
		return *a.frameState
	}
	if a.o.Player == nil {
		return player.State{}
	}
	return a.o.Player.State()
}

// Damage marks r as changed; the next frame redraws it.
func (a *App) Damage(r gfx.Rect) {
	r = r.Intersect(a.canvas.Bounds())
	if !r.Empty() {
		a.damage = append(a.damage, r)
	}
}

// mergeRects joins overlapping or touching rectangles, and folds the lot
// into its bounding box when more than max are left.
func mergeRects(rs []gfx.Rect, max int) []gfx.Rect {
	return mergeInto(nil, rs, max)
}

// mergeInto is mergeRects working in buf's storage, so a frame that merges
// a few rectangles every time allocates nothing once buf has grown.
func mergeInto(buf, rs []gfx.Rect, max int) []gfx.Rect {
	out := append(buf[:0], rs...)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out) && !merged; i++ {
			for j := i + 1; j < len(out); j++ {
				if touches(out[i], out[j]) {
					out[i] = union(out[i], out[j])
					out = append(out[:j], out[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	if len(out) > max {
		box := out[0]
		for _, r := range out[1:] {
			box = union(box, r)
		}
		out = append(out[:0], box)
	}
	return out
}

func touches(a, b gfx.Rect) bool {
	return a.X <= b.Right() && b.X <= a.Right() && a.Y <= b.Bottom() && b.Y <= a.Bottom()
}

func union(a, b gfx.Rect) gfx.Rect {
	x, y := min(a.X, b.X), min(a.Y, b.Y)
	return gfx.R(x, y, max(a.Right(), b.Right())-x, max(a.Bottom(), b.Bottom())-y)
}

// renderDamage draws and presents the damaged areas only. A partial frame
// costs about its area (measured on the MiSTer: never more than a full one,
// even for a list scroll that covers most of the screen), so there is no
// size above which a full frame is drawn instead. It reports false when the
// verify check failed and the full frame is needed.
func (a *App) renderDamage() (bool, error) {
	// Position, marquee and clock move on their own: every partial frame
	// brings their regions up to date too, so what it leaves on screen is
	// what a full frame would show (the verify pass sees the same clock).
	if a.o.Player != nil && a.state().Status == player.Playing {
		a.damage = append(a.damage, a.ticks...)
	}
	if a.animate && !a.mqRect.Empty() {
		a.damage = append(a.damage, a.mqRect)
	}
	a.mergeBuf = mergeInto(a.mergeBuf, a.damage, maxDamageRects)
	rs := a.mergeBuf
	a.damage = a.damage[:0]
	c := a.canvas
	for _, r := range rs {
		c.SetClip(r)
		a.drawFrame(c)
	}
	own := a.drawVizDamage(c, rs)
	c.ClearClip()
	if len(own) > 0 { // the visualizer's own small areas are presented as they are
		a.presentBuf = append(append(a.presentBuf[:0], rs...), own...)
		rs = a.presentBuf
	}
	if a.verify {
		if err := a.checkPartial(); err != nil {
			if a.verifyFail != nil {
				a.verifyFail(err.Error())
			} else {
				log.Printf("ui: %v; drawing the full frame", err)
			}
			return false, nil
		}
	}
	return true, a.presentRects(c, rs)
}

// vizOwnLimit is how many of the visualizer's small areas a frame may draw
// through the whole drawFrame (the ones overlapped by something else); more
// than that are replaced by their bounding box.
const vizOwnLimit = 4

// drawVizDamage draws the visualizer's own damage (viz_damage.go) and
// returns the areas to present. An area nothing else covers is drawn by
// drawViz alone: that is what a full frame leaves there, and it skips the
// rest of the screen's drawing, which would otherwise run once per area. An
// area under the hint bar, the info line, a toast, the volume panel or a
// damaged area goes through drawFrame, in order, clipped.
func (a *App) drawVizDamage(c *gfx.Canvas, drawn []gfx.Rect) []gfx.Rect {
	v := &a.viz
	if len(v.dmg) == 0 {
		return nil
	}
	own := append(a.ownBuf[:0], v.dmg...)
	v.dmg = v.dmg[:0]
	a.ownBuf = own
	direct := a.vizDirect()
	over := a.vizOverlays(drawn)
	via, n := a.viaBuf[:0], 0
	for _, r := range own {
		if direct && !overlaps(r, over) {
			own[n] = r
			n++
			continue
		}
		via = append(via, r)
	}
	a.viaBuf = via
	if len(via) > vizOwnLimit {
		box := via[0]
		for _, r := range via[1:] {
			box = union(box, r)
		}
		via = append(via[:0], box)
	}
	for _, r := range via {
		c.SetClip(r)
		a.drawFrame(c)
	}
	style := a.VizStyle()
	for _, r := range own[:n] {
		c.SetClip(r)
		a.drawViz(c, v.rect, style)
	}
	return append(own[:n], via...)
}

func overlaps(r gfx.Rect, rs []gfx.Rect) bool {
	for _, o := range rs {
		if !r.Intersect(o).Empty() {
			return true
		}
	}
	return false
}

// vizDirect reports whether the visualizer's picture is the last thing a
// frame draws where nothing overlays it: on Now Playing or its full screen,
// with no confirmation up.
func (a *App) vizDirect() bool {
	if a.confirm {
		return false
	}
	switch a.Top().(type) {
	case *NowPlayingScreen, *VizScreen:
		return true
	}
	return false
}

// vizOverlays are the areas drawn over the visualizer, besides the damage.
func (a *App) vizOverlays(drawn []gfx.Rect) []gfx.Rect {
	over := append(a.overBuf[:0], drawn...)
	if a.hintsUp() {
		r := a.hintRect()
		over = append(over, gfx.R(r.X, r.Y, r.W, a.P.H-r.Y))
	}
	if vs, ok := a.Top().(*VizScreen); ok && vs.text != "" {
		over = append(over, vs.rect)
	}
	if !a.volumeUntil.IsZero() {
		over = append(over, a.volumePanelRect())
	}
	if t := a.toastsArea(); !t.Empty() {
		over = append(over, t)
	}
	a.overBuf = over
	return over
}

// checkPartial draws a full frame on the side and compares it with the
// canvas: a partial frame must match it pixel for pixel.
func (a *App) checkPartial() error {
	c := a.canvas
	if a.verifyCanvas == nil || a.verifyCanvas.W != c.W || a.verifyCanvas.H != c.H {
		a.verifyCanvas = gfx.NewCanvas(c.W, c.H)
	}
	a.drawFrame(a.verifyCanvas)
	for i, p := range c.Pix {
		if p != a.verifyCanvas.Pix[i] {
			return fmt.Errorf("partial redraw differs from a full one at (%d,%d): %06x, want %06x",
				i%c.W, i/c.W, p, a.verifyCanvas.Pix[i])
		}
	}
	return nil
}

// presentRects shows the rectangles rs of c: only those areas when the
// display can take them, otherwise the whole frame (scaled on CRT).
func (a *App) presentRects(c *gfx.Canvas, rs []gfx.Rect) error {
	if pp, ok := a.o.Display.(gfx.PartialPresenter); ok && a.scaler == nil {
		if a.frame == nil {
			return pp.PresentRects(c, rs)
		}
		// With margins: those areas go into the full frame, and are
		// presented where they land in it.
		a.shiftBuf = a.shiftBuf[:0]
		for _, r := range rs {
			if r = r.Intersect(c.Bounds()); !r.Empty() {
				a.copyIn(c, r)
				a.shiftBuf = append(a.shiftBuf, gfx.R(r.X+a.inset.X, r.Y+a.inset.Y, r.W, r.H))
			}
		}
		return pp.PresentRects(a.frame, a.shiftBuf)
	}
	return a.present(c)
}

// moved records a key's whole visual effect as rs (a list or grid focus
// move: Moved's rectangles). The event then redraws only those; nil means
// unknown, and the frame is redrawn in full as for any other key.
func (a *App) moved(rs []gfx.Rect) {
	if rs == nil {
		return
	}
	for _, r := range rs {
		a.Damage(r)
	}
	a.exact = true
}

// Regions that change on their own, recorded while drawing (every frame
// rebuilds them): the playback position, the scrolling title, and where each
// cover was drawn.

func (a *App) markTick(r gfx.Rect)    { a.ticks = append(a.ticks, r) }
func (a *App) markMarquee(r gfx.Rect) { a.mqRect = r }

func (a *App) markArt(id subsonic.ID, r gfx.Rect) {
	if a.arts == nil {
		a.arts = map[subsonic.ID][]gfx.Rect{}
	}
	a.arts[id] = append(a.arts[id], r)
}

func (a *App) resetMarks() {
	a.ticks, a.mqRect, a.viz.rect = a.ticks[:0], gfx.Rect{}, gfx.Rect{}
	for id := range a.arts {
		delete(a.arts, id)
	}
}

// damageAll damages rs, or redraws the whole frame when there are none.
func (a *App) damageAll(rs []gfx.Rect) {
	if len(rs) == 0 {
		a.dirty = true
		return
	}
	for _, r := range rs {
		a.Damage(r)
	}
}

// coverArrived redraws where the cover id was drawn.
func (a *App) coverArrived(id subsonic.ID) { a.damageAll(a.arts[id]) }

// toastsArea is where the toasts are drawn now (empty when there are none).
func (a *App) toastsArea() gfx.Rect {
	var box gfx.Rect
	a.eachToast(func(r gfx.Rect, _ string) {
		if box.Empty() {
			box = r
		} else {
			box = union(box, r)
		}
	})
	return box
}
