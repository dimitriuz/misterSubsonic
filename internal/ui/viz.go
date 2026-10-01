package ui

import (
	"log"
	"math"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/viz"
)

// VizStyle is the visualizer's look (spec §3.2); VizOff draws nothing.
type VizStyle int

const (
	VizOff VizStyle = iota
	VizBars
	VizScope
	VizVU
	VizWaterfall
)

var vizNames = [...]string{"off", "bars", "scope", "vu", "waterfall"}
var vizLabels = [...]string{"Off", "Bars", "Scope", "VU meters", "Waterfall"}

// ParseVizStyle reads a config name; anything unknown is Off.
func ParseVizStyle(s string) VizStyle {
	for i, n := range vizNames {
		if n == s {
			return VizStyle(i)
		}
	}
	return VizOff
}

// String is the name in the config file.
func (v VizStyle) String() string { return vizNames[v] }

// Label is the name shown to the user.
func (v VizStyle) Label() string { return vizLabels[v] }

// VizStyle is the saved style (Off with no config).
func (a *App) VizStyle() VizStyle {
	if a.cfg == nil {
		return VizOff
	}
	return ParseVizStyle(a.cfg.Display.Visualizer)
}

// SetVizStyle changes the style and saves it, like the other settings.
func (a *App) SetVizStyle(v VizStyle) {
	a.UpdateConfig(func(c *config.Config) { c.Display.Visualizer = v.String() }, false)
	a.dirty = true
}

// The frame clock and the safety valve (spec §5). While a visualizer is
// visible and the sound plays, the app wakes at the target rate; each wake
// analyses the window and damages the panel. The valve steps the rate down
// when frames cost too much (the MiSTer's CPU is not measured yet), and up
// again after a calm spell.
const (
	vizFPSHDMI = 30
	vizFPSCRT  = 20

	vizMeasure = time.Second      // the span a frame's average cost is taken over
	vizSlow    = 0.6              // step down above this share of the frame budget
	vizCalm    = 0.3              // ...step up when under this share of the faster budget
	vizCalmFor = 10 * time.Second // ...for this long
	vizGap     = 500 * time.Millisecond
	vizSweep   = 6 // seconds the waterfall's cursor takes across the panel
)

var (
	vizRatesHDMI = []int{vizFPSHDMI, 15, 10}
	vizRatesCRT  = []int{vizFPSCRT, 10}
)

// vizState is what the visualizer keeps between frames.
type vizState struct {
	an     *viz.Analyzer
	rates  []int // the frame rates, fastest first
	level  int   // index into rates
	win    []float32
	scope  []float32 // the scope's points, one per panel column
	img    *gfx.Image
	cursor int      // the waterfall's next column
	style  VizStyle // what the picture in img is of
	rect   gfx.Rect // where the panel was drawn (empty: not on screen)

	last, next time.Time // the last analysis, the next due (zero: now)

	work    time.Duration // the analysis of the frame awaiting its draw
	pending bool
	sum     time.Duration // frame costs since since
	n       int
	since   time.Time
	calm    time.Time // when the cheap spell began (zero: not in one)
}

func (a *App) isCRT() bool { return a.P.Name == "crt" }

// vizFPS is the frame rate now.
func (a *App) vizFPS() int {
	if a.viz.rates == nil {
		return vizFPSHDMI
	}
	return a.viz.rates[a.viz.level]
}

func (a *App) vizInterval() time.Duration { return time.Second / time.Duration(a.vizFPS()) }

// markViz records where the visualizer is drawn, like markTick: the frame
// clock damages it.
func (a *App) markViz(r gfx.Rect) { a.viz.rect = r }

// vizActive reports whether the frame clock runs: a visualizer is on screen
// and the sound plays, or the levels are still falling.
func (a *App) vizActive() bool {
	v := &a.viz
	if !a.vizShown() || v.rect.Empty() || a.saver {
		return false
	}
	return a.state().Status == player.Playing || v.an != nil && !v.an.Idle()
}

// vizDue is when the next frame is due (zero: no frame clock).
func (a *App) vizDue() time.Time {
	if !a.vizActive() {
		return time.Time{}
	}
	if a.viz.next.IsZero() {
		return a.o.Now()
	}
	return a.viz.next
}

func (a *App) vizInit() {
	cfg, rates := viz.HDMI(), vizRatesHDMI
	if a.isCRT() {
		cfg, rates = viz.CRT(), vizRatesCRT
	}
	a.viz.an, a.viz.rates = viz.New(cfg), rates
	a.viz.win = make([]float32, 2*cfg.FFTSize)
}

// vizTick is one frame's analysis, run from the wake: the window into the
// analyzer, the waterfall's new column, and the panel damaged.
func (a *App) vizTick(now time.Time) {
	v := &a.viz
	start := a.o.Now()
	if v.an == nil {
		a.vizInit()
	}
	a.vizStyleSync()
	dt := a.vizInterval()
	if d := now.Sub(v.last); !v.last.IsZero() && d < vizGap {
		dt = d
	} else { // the clock was stopped: the cost average starts again
		v.sum, v.n, v.since, v.calm = 0, 0, time.Time{}, time.Time{}
	}
	n := 0
	if a.state().Status == player.Playing { // a paused device stops consuming: let the levels fall
		n = a.o.Visual.Window(v.win)
	}
	v.an.Update(v.win, n, dt)
	switch style := a.VizStyle(); style {
	case VizScope:
		if len(v.scope) != v.rect.W {
			v.scope = make([]float32, v.rect.W)
		}
		v.an.Scope(v.scope)
	case VizWaterfall:
		a.sweepWaterfall()
	}
	v.last, v.next = now, now.Add(a.vizInterval())
	a.Damage(v.rect)
	v.work, v.pending = a.o.Now().Sub(start), true
}

// vizStyleSync notes the style in use; choosing the waterfall again starts
// its picture clean, with the cursor at the left.
func (a *App) vizStyleSync() {
	v := &a.viz
	if style := a.VizStyle(); style != v.style {
		v.style = style
		if style == VizWaterfall && v.img != nil {
			clear(v.img.Pix)
		}
		v.cursor = 0
	}
}

// sweepWaterfall paints this frame's spectrum column at the cursor and moves
// the cursor on; the picture is never scrolled.
func (a *App) sweepWaterfall() {
	v := &a.viz
	r := v.rect
	if v.img == nil || v.img.W != r.W || v.img.H != r.H {
		v.img, v.cursor = gfx.NewImage(r.W, r.H), 0
	}
	col := v.an.Column()
	cw := max(int(math.Round(float64(r.W)/float64(vizSweep*a.vizFPS()))), 1)
	for dx := 0; dx < cw; dx++ {
		x := (v.cursor + dx) % r.W
		for y := 0; y < r.H; y++ {
			b := min((r.H-1-y)*len(col)/r.H, len(col)-1) // low frequencies at the bottom
			i := int(min(max(col[b], 0), 1)*255 + 0.5)
			v.img.Pix[y*r.W+x] = 0xFF000000 | viz.Palette[i]
		}
	}
	v.cursor = (v.cursor + cw) % r.W
}

// vizCost takes in what a visualizer frame cost (analysis, draw, present)
// and moves the rate: down when the last second averaged over vizSlow of the
// frame budget, up after vizCalmFor under vizCalm of the faster budget's.
func (a *App) vizCost(cost time.Duration) {
	v := &a.viz
	now := a.o.Now()
	v.pending = false
	if v.since.IsZero() {
		v.since = now
	}
	v.sum += cost
	v.n++
	if now.Sub(v.since) < vizMeasure {
		return
	}
	avg := v.sum / time.Duration(v.n)
	v.sum, v.n, v.since = 0, 0, now
	budget := func(level int) time.Duration { return time.Second / time.Duration(v.rates[level]) }
	switch {
	case float64(avg) > vizSlow*float64(budget(v.level)) && v.level < len(v.rates)-1:
		v.level++
		v.calm = time.Time{}
		log.Printf("visualizer: slowing to %d fps", v.rates[v.level])
	case v.level > 0 && float64(avg) < vizCalm*float64(budget(v.level-1)):
		if v.calm.IsZero() {
			v.calm = now
		} else if now.Sub(v.calm) >= vizCalmFor {
			v.level--
			v.calm = time.Time{}
		}
	default:
		v.calm = time.Time{}
	}
}

// drawVizPanel draws the visualizer in r (Now Playing's panel) and records
// it for the frame clock. Nothing is drawn, or woken, with the style Off or
// no sound source.
func (a *App) drawVizPanel(c *gfx.Canvas, r gfx.Rect) {
	if !a.vizShown() || r.W < 8 || r.H < 8 {
		return
	}
	a.markViz(r)
	a.vizStyleSync()
	a.drawViz(c, r, a.VizStyle())
}

// vizShown reports whether a visualizer is chosen and has a sound source.
func (a *App) vizShown() bool { return a.VizStyle() != VizOff && a.o.Visual != nil }

// drawViz draws one style into r from the analyzer's current state; it
// changes no state, so a partial frame equals a full one.
func (a *App) drawViz(c *gfx.Canvas, r gfx.Rect, style VizStyle) {
	c.Fill(r, colPanel)
	if a.viz.an == nil {
		return // no frame yet
	}
	switch style {
	case VizBars:
		a.drawBars(c, r)
	case VizScope:
		a.drawScope(c, r)
	case VizVU:
		a.drawVU(c, r)
	case VizWaterfall:
		a.drawWaterfall(c, r)
	}
}

var colWhite = gfx.RGB(0xff, 0xff, 0xff)

// mixColor is a toward b by t/256.
func mixColor(a, b gfx.Color, t int) gfx.Color {
	ch := func(sh uint) uint8 {
		x, y := int(a>>sh)&0xff, int(b>>sh)&0xff
		return uint8((x*(256-t) + y*t) >> 8)
	}
	return gfx.RGB(ch(16), ch(8), ch(0))
}

// drawBars: log-spaced bars, accent at the bottom to white at the top, with
// a brighter cap on each peak.
func (a *App) drawBars(c *gfx.Canvas, r gfx.Rect) {
	level, peak := a.viz.an.Bars()
	n := len(level)
	gap := 1
	if !a.isCRT() {
		gap = max(r.W/256, 2)
	}
	bw := max((r.W-gap*(n-1))/n, 1)
	x := r.X + (r.W-(bw*n+gap*(n-1)))/2
	seg := max(r.H/32, 1) // the gradient is drawn in steps this high
	capH := max(r.H/48, 1)
	shade := func(y int) gfx.Color { return mixColor(colAccent, colWhite, y*256/r.H) }
	for i := range level {
		h := int(min(max(level[i], 0), 1)*float32(r.H) + 0.5)
		for y := 0; y < h; y += seg {
			sh := min(seg, h-y)
			c.Fill(gfx.R(x, r.Bottom()-y-sh, bw, sh), shade(y+sh/2))
		}
		if ph := int(min(max(peak[i], 0), 1)*float32(r.H) + 0.5); ph > 0 {
			ph = max(ph, capH)
			c.Fill(gfx.R(x, r.Bottom()-ph, bw, capH), mixColor(shade(ph), colWhite, 160))
		}
		x += bw + gap
	}
}

// drawScope: the wave as one accent line, a point per column, with vertical
// runs joining neighbours.
func (a *App) drawScope(c *gfx.Canvas, r gfx.Rect) {
	pts := a.viz.scope
	if len(pts) != r.W {
		return
	}
	th := 1
	if !a.isCRT() {
		th = 2
	}
	mid := r.Y + r.H/2
	c.Fill(gfx.R(r.X, mid, r.W, 1), colArtBg)
	amp := float32(r.H/2 - th)
	prev := 0
	for i, v := range pts {
		y := mid - int(min(max(v, -1), 1)*amp)
		top, h := y, th
		if i > 0 {
			top, h = min(y, prev), abs(y-prev)+th
		}
		c.Fill(gfx.R(r.X+i, top, 1, h), colAccent)
		prev = y
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

var (
	colLEDGreen  = gfx.RGB(0x4c, 0xaf, 0x50)
	colLEDYellow = gfx.RGB(0xff, 0xc1, 0x07)
)

// glyphs are the meters' labels, 3×5 dots each (too small a strip for a font).
var glyphs = map[byte][5]string{
	'L': {"#..", "#..", "#..", "#..", "###"},
	'R': {"##.", "#.#", "##.", "#.#", "#.#"},
}

// drawVU: two horizontal LED meters, one segment per dB from -40 to +3,
// green, then yellow, then red above -3 dB, with a peak marker.
func (a *App) drawVU(c *gfx.Canvas, r gfx.Rect) {
	level, peak := a.viz.an.VU()
	gap := max(r.H/10, 1)
	rowH := (r.H - gap) / 2
	segGap := 1
	if !a.isCRT() {
		segGap = 2
	}
	const nseg = 43
	s := min(max(rowH/6, 1), 6) // glyph dot size
	mx := r.X + 5*s
	segW := max((r.Right()-mx-segGap*(nseg-1))/nseg, 1)
	for ch, name := range "LR" {
		y := r.Y + ch*(rowH+gap)
		for gy, line := range glyphs[byte(name)] {
			for gx, dot := range line {
				if dot == '#' {
					c.Fill(gfx.R(r.X+s+gx*s, y+(rowH-5*s)/2+gy*s, s, s), colDim)
				}
			}
		}
		pk := -1
		if peak[ch] > 0 {
			pk = min(int(peak[ch]*nseg), nseg-1)
		}
		for i := 0; i < nseg; i++ {
			col := colLEDGreen
			switch db := a.viz.an.DB(float32(i) / nseg); {
			case db >= -3:
				col = colError
			case db >= -12:
				col = colLEDYellow
			}
			switch {
			case i == pk:
				col = colText
			case level[ch] < float32(i+1)/nseg:
				col = mixColor(col, colBg, 210) // unlit
			}
			c.Fill(gfx.R(mx+i*(segW+segGap), y, segW, rowH), col)
		}
	}
}

// drawWaterfall: the sweep's picture, with the cursor line at the next column.
func (a *App) drawWaterfall(c *gfx.Canvas, r gfx.Rect) {
	v := &a.viz
	if v.img == nil || v.img.W != r.W || v.img.H != r.H {
		return
	}
	c.Blit(v.img, r)
	c.Fill(gfx.R(r.X+v.cursor, r.Y, 1, r.H), colText)
}
