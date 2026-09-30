package ui

import (
	"fmt"
	"log"

	"mistersubsonic/internal/gfx"
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
	out := append([]gfx.Rect(nil), rs...)
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
		out = []gfx.Rect{box}
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

func area(rs []gfx.Rect) int {
	n := 0
	for _, r := range rs {
		n += r.W * r.H
	}
	return n
}

// renderDamage draws and presents the damaged areas only. It reports false
// when a full frame is better (the areas cover more than half the canvas).
func (a *App) renderDamage() (bool, error) {
	rs := mergeRects(a.damage, maxDamageRects)
	a.damage = a.damage[:0]
	c := a.canvas
	if 2*area(rs) > c.W*c.H {
		return false, nil
	}
	for _, r := range rs {
		c.SetClip(r)
		a.drawFrame(c)
	}
	c.ClearClip()
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
		return pp.PresentRects(c, rs)
	}
	return a.present(c)
}
