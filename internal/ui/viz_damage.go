package ui

import (
	"mistersubsonic/internal/gfx"
)

// What the visualizer damages (Plan 7b). The picture changes in small places
// each frame, and the Cortex-A9 is bound by the pixels it moves, so each frame
// damages only what differs from the picture on screen: per bar the span
// between the old and new level, the caps, a meter's changed segments, the
// waterfall's new strip, the scope's band per chunk of columns. The frame
// draws these areas without merging them into one box (renderDamage); the
// drawing code is the same, clipped.

// vizSeen is what the picture on screen is made of: the numbers the drawing
// reads, as of the last frame. The damage is the difference to the analyzer.
type vizSeen struct {
	valid bool // false: the whole panel is damaged and this is taken anew
	rect  gfx.Rect
	style VizStyle

	bars, caps []int // per bar: its height, and its cap's (0: none)
	lit, peak  [2]int
}

// vizScopeChunks is how many bands of columns the scope's damage has (the
// measured best: 16 leave tall bands, 192 cost more rectangles than they save).
const vizScopeChunks = 48

// vizDamage damages what the frame just analysed changed (strip: where the
// waterfall painted) and takes it as seen.
func (a *App) vizDamage(strip stripOf) {
	v := &a.viz
	s := &v.seen
	style := a.VizStyle()
	whole := !s.valid || s.rect != v.rect || s.style != style
	s.valid, s.rect, s.style = true, v.rect, style
	if whole {
		a.takeSeen(style)
		a.Damage(v.rect)
		return
	}
	switch style {
	case VizBars:
		a.damageBars()
	case VizScope:
		a.damageScope()
	case VizVU:
		a.damageVU()
	case VizWaterfall:
		if !strip.ok {
			a.Damage(v.rect)
			return
		}
		a.damageStrip(strip)
	}
}

// takeSeen notes the analyzer's state as what the screen will show.
func (a *App) takeSeen(style VizStyle) {
	v := &a.viz
	s := &v.seen
	switch style {
	case VizBars:
		level, peak := v.an.Bars()
		g := a.barGeometry(v.rect, len(level))
		if len(s.bars) != len(level) {
			s.bars, s.caps = make([]int, len(level)), make([]int, len(level))
		}
		for i := range level {
			s.bars[i], s.caps[i] = barHeight(level[i], v.rect.H), g.capHeight(peak[i], v.rect.H)
		}
	case VizVU:
		level, peak := v.an.VU()
		for ch := range level {
			s.lit[ch], s.peak[ch] = vuLit(level[ch]), vuPeak(peak[ch])
		}
	}
}

// own adds a rectangle to the visualizer's own damage.
func (a *App) own(r gfx.Rect) {
	if r = r.Intersect(a.viz.rect); !r.Empty() {
		a.viz.dmg = append(a.viz.dmg, r)
	}
}

// damageBars: per bar, the rows between the old and the new height (from the
// start of the top step, whose colour follows the height), and the caps.
func (a *App) damageBars() {
	v := &a.viz
	s := &v.seen
	r := v.rect
	level, peak := v.an.Bars()
	g := a.barGeometry(r, len(level))
	topStep := func(h int) int { // the start of the step the bar's top is in
		if h <= 0 {
			return 0
		}
		return (h - 1) / g.seg * g.seg
	}
	for i := range level {
		x := g.x0 + i*g.step
		h, ph := barHeight(level[i], r.H), g.capHeight(peak[i], r.H)
		if old := s.bars[i]; h != old {
			lo, hi := min(topStep(h), topStep(old)), max(h, old)
			a.own(gfx.R(x, r.Bottom()-hi, g.bw, hi-lo))
		}
		if old := s.caps[i]; ph != old {
			switch {
			case old == 0 || ph == 0:
				a.own(capRect(r, g, x, old+ph)) // one of them is 0: the other's cap
			case abs(ph-old) <= 4*g.capH: // nearby: one rectangle for both
				lo, hi := min(ph, old), max(ph, old)
				a.own(gfx.R(x, r.Bottom()-hi, g.bw, hi-lo+g.capH))
			default:
				a.own(capRect(r, g, x, old))
				a.own(capRect(r, g, x, ph))
			}
		}
		s.bars[i], s.caps[i] = h, ph
	}
}

func capRect(r gfx.Rect, g barGeom, x, ph int) gfx.Rect {
	return gfx.R(x, r.Bottom()-ph, g.bw, g.capH)
}

// damageVU: per meter, the segments whose lit state changed and the old and
// new peak markers.
func (a *App) damageVU() {
	v := &a.viz
	s := &v.seen
	level, peak := v.an.VU()
	g := a.vuGeometry(v.rect)
	for ch := range level {
		lit, pk := vuLit(level[ch]), vuPeak(peak[ch])
		if old := s.lit[ch]; lit != old {
			a.own(g.segs(v.rect, ch, min(lit, old), max(lit, old)))
		}
		if old := s.peak[ch]; pk != old {
			if old >= 0 {
				a.own(g.segs(v.rect, ch, old, old+1))
			}
			if pk >= 0 {
				a.own(g.segs(v.rect, ch, pk, pk+1))
			}
		}
		s.lit[ch], s.peak[ch] = lit, pk
	}
}

// damageStrip: the columns the waterfall painted, and its cursor, which
// moved from the strip's start to its end (in at most two pieces: the
// picture wraps).
func (a *App) damageStrip(st stripOf) {
	r := a.viz.rect
	n := st.n + 1 // the cursor stands on the column after the strip
	if st.x+n <= r.W {
		a.own(gfx.R(r.X+st.x, r.Y, n, r.H))
		return
	}
	a.own(gfx.R(r.X+st.x, r.Y, r.W-st.x, r.H))
	a.own(gfx.R(r.X, r.Y, st.x+n-r.W, r.H))
}

// damageScope: the columns whose line moved, in vizScopeChunks bands of
// columns: each band's rectangle holds both the old and the new line there.
func (a *App) damageScope() {
	v := &a.viz
	r := v.rect
	if len(v.scope) != r.W || len(v.prev) != r.W {
		a.Damage(r)
		return
	}
	g := a.scopeGeometry(r)
	per := (r.W + vizScopeChunks - 1) / vizScopeChunks
	for x0 := 0; x0 < r.W; x0 += per {
		x1 := min(x0+per, r.W)
		first, last, top, bot := -1, 0, 0, 0
		for i := x0; i < x1; i++ {
			nt, nh := g.span(v.scope, i)
			ot, oh := g.span(v.prev, i)
			if nt == ot && nh == oh {
				continue
			}
			t, b := min(nt, ot), max(nt+nh, ot+oh)
			if first < 0 {
				first, top, bot = i, t, b
			}
			last, top, bot = i, min(top, t), max(bot, b)
		}
		if first >= 0 {
			a.own(gfx.R(r.X+first, top, last-first+1, bot-top))
		}
	}
}
