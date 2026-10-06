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
	vizCalm    = vizSlow          // ...step up when under this share of the faster budget (what would step it down again)
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
	prev   []float32 // the scope's points of the frame before
	img    *gfx.Image
	cursor int      // the waterfall's next column
	wf     wfall    // its tables and the last spectrum
	style  VizStyle // what the picture in img is of
	rect   gfx.Rect // where the panel was drawn (empty: not on screen)
	size   [2]int   // the size it was last drawn at: a new size starts the valve again

	seen vizSeen    // what the picture on screen is made of (viz_damage.go)
	dmg  []gfx.Rect // the visualizer's own changed areas, awaiting their draw

	last, next time.Time // the last analysis, the next due (zero: now)

	work    time.Duration // the analysis of the frame awaiting its draw
	pending bool
	sum     time.Duration // frame costs since since
	n       int
	since   time.Time
	calm    time.Time // when the cheap spell began (zero: not in one)
	warned  bool      // the over-budget line was logged in this stay at the floor
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
func (a *App) markViz(r gfx.Rect) {
	v := &a.viz
	v.rect = r
	if size := [2]int{r.W, r.H}; size != v.size {
		// The cost of a frame depends on the picture's size, so what the
		// valve learnt in the panel says nothing about the full screen:
		// start at the top rate again, with the measurements cleared.
		v.size = size
		v.level, v.warned = 0, false
		v.sum, v.n, v.since, v.calm = 0, 0, time.Time{}, time.Time{}
	}
}

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
	strip := stripOf{}
	switch style := a.VizStyle(); style {
	case VizScope:
		if len(v.scope) != v.rect.W {
			v.scope, v.prev = make([]float32, v.rect.W), make([]float32, v.rect.W)
			v.seen.valid = false
		}
		copy(v.prev, v.scope)
		v.an.Scope(v.scope)
	case VizWaterfall:
		strip = a.sweepWaterfall()
	}
	v.last, v.next = now, now.Add(a.vizInterval())
	a.vizDamage(strip)
	v.work, v.pending = a.o.Now().Sub(start), true
}

// vizStyleSync notes the style in use; choosing the waterfall again starts
// its picture clean, with the cursor at the left.
func (a *App) vizStyleSync() {
	v := &a.viz
	if style := a.VizStyle(); style != v.style {
		v.style = style
		v.seen.valid = false
		if style == VizWaterfall && v.img != nil {
			clear(v.img.Pix)
		}
		v.cursor = 0
	}
}

// stripOf is the columns a waterfall frame painted: n from x (mod the
// width), then the cursor. ok false means the picture began anew.
type stripOf struct {
	x, n int
	ok   bool
}

// wfall is the waterfall's working storage, made once for a picture size.
type wfall struct {
	prev []float32 // the spectrum of the frame before
	val  []int32   // one column's bands in 1/256 palette steps, plus a copy of the last
	pos  []int32   // per row: the band below, and how far toward the next (1/256), packed band<<8|frac
}

// setup sizes the tables for a picture h high and nb bands. A row's position
// between the bands is worked out once here: the low bands are at the bottom,
// the first band's centre on the bottom row and the last band's on the top.
func (w *wfall) setup(h, nb int) {
	w.prev = make([]float32, nb)
	w.val = make([]int32, nb+1)
	w.pos = make([]int32, h)
	for y := range w.pos {
		p := (h - 1 - y) * (nb - 1) * 256 / max(h-1, 1)
		w.pos[y] = int32(p)
	}
}

// sweepWaterfall paints this frame's spectrum column at the cursor and moves
// the cursor on; the picture is never scrolled. A column blends the two
// nearest bands in the palette index. A strip wider than a column blends
// from the previous frame's spectrum to this one's across its width.
func (a *App) sweepWaterfall() stripOf {
	v := &a.viz
	r := v.rect
	col := v.an.Column()
	fresh := false
	if v.img == nil || v.img.W != r.W || v.img.H != r.H || len(v.wf.prev) != len(col) || len(v.wf.pos) != r.H {
		v.img, v.cursor = gfx.NewImage(r.W, r.H), 0
		v.wf.setup(r.H, len(col))
		copy(v.wf.prev, col)
		fresh = true
	}
	w := &v.wf
	cw := max(int(math.Round(float64(r.W)/float64(vizSweep*a.vizFPS()))), 1)
	from := v.cursor
	nb := len(col)
	for dx := 0; dx < cw; dx++ {
		x := (v.cursor + dx) % r.W
		t := float32(dx+1) / float32(cw)
		for k, c := range col {
			p := w.prev[k]
			l := min(max(p+(c-p)*t, 0), 1)
			w.val[k] = int32(l*(255*256) + 0.5)
		}
		w.val[nb] = w.val[nb-1] // the top row stands on the last band exactly
		pix := v.img.Pix[x:]
		for y, p := range w.pos {
			k, f := p>>8, p&255
			i := (w.val[k]*(256-f) + w.val[k+1]*f + 128<<8) >> 16 // palette index: 1/256 steps twice over
			pix[y*r.W] = 0xFF000000 | viz.Palette[i]
		}
	}
	copy(w.prev, col)
	v.cursor = (v.cursor + cw) % r.W
	return stripOf{from, cw, !fresh}
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
	case float64(avg) > vizSlow*float64(budget(v.level)): // already at the lowest rate
		v.calm = time.Time{}
		if !v.warned {
			v.warned = true
			log.Printf("visualizer: over budget at %d fps", v.rates[v.level])
		}
	case v.level > 0 && float64(avg) < vizCalm*float64(budget(v.level-1)):
		if v.calm.IsZero() {
			v.calm = now
		} else if now.Sub(v.calm) >= vizCalmFor {
			v.level--
			v.calm, v.warned = time.Time{}, false
		}
	default:
		v.calm = time.Time{}
	}
}

// drawVizPanel draws the visualizer in r (Now Playing's panel) and records
// it for the frame clock. Nothing is drawn, or woken, with the style Off or
// no sound source.
func (a *App) drawVizPanel(c *gfx.Canvas, r gfx.Rect) {
	a.vizStyleSync() // also while Off: the next waterfall starts clean
	if !a.vizShown() || r.W < 8 || r.H < 8 {
		return
	}
	a.markViz(r)
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

// barGeom is where the bars stand in r.
type barGeom struct{ x0, bw, step, seg, capH int }

func (a *App) barGeometry(r gfx.Rect, n int) barGeom {
	gap := 1
	if !a.isCRT() {
		gap = max(r.W/256, 2)
	}
	bw := max((r.W-gap*(n-1))/n, 1)
	return barGeom{
		x0:   r.X + (r.W-(bw*n+gap*(n-1)))/2,
		bw:   bw,
		step: bw + gap,
		seg:  max(r.H/32, 1), // the gradient is drawn in steps this high
		capH: max(r.H/48, 1),
	}
}

// barHeight is a level's bar in pixels of a panel h high.
func barHeight(level float32, h int) int { return int(min(max(level, 0), 1)*float32(h) + 0.5) }

// capHeight is where a bar's peak cap sits (its bottom edge above the panel's
// bottom), or 0 for none.
func (g barGeom) capHeight(peak float32, h int) int {
	if ph := barHeight(peak, h); ph > 0 {
		return max(ph, g.capH)
	}
	return 0
}

// drawBars: log-spaced bars, accent at the bottom to white at the top, with
// a brighter cap on each peak. Only the bars and steps inside the clip are
// visited (a partial frame draws a few columns of them).
func (a *App) drawBars(c *gfx.Canvas, r gfx.Rect) {
	level, peak := a.viz.an.Bars()
	g := a.barGeometry(r, len(level))
	clip := c.Clip().Intersect(r)
	if clip.Empty() {
		return
	}
	shade := func(y int) gfx.Color { return mixColor(colAccent, colWhite, y*256/r.H) }
	lo, hi := r.Bottom()-clip.Bottom(), r.Bottom()-clip.Y // the clip's rows, up from the panel's bottom
	for i := range level {
		x := g.x0 + i*g.step
		if x >= clip.Right() || x+g.bw <= clip.X {
			continue
		}
		h := barHeight(level[i], r.H)
		for y := lo / g.seg * g.seg; y < min(h, hi); y += g.seg {
			sh := min(g.seg, h-y)
			c.Fill(gfx.R(x, r.Bottom()-y-sh, g.bw, sh), shade(y+sh/2))
		}
		if ph := g.capHeight(peak[i], r.H); ph > 0 {
			c.Fill(gfx.R(x, r.Bottom()-ph, g.bw, g.capH), mixColor(shade(ph), colWhite, 160))
		}
	}
}

// scopeGeom is the scope's line: its thickness, middle row and amplitude.
type scopeGeom struct {
	th, mid int
	amp     float32
}

func (a *App) scopeGeometry(r gfx.Rect) scopeGeom {
	th := 1
	if !a.isCRT() {
		th = 2
	}
	return scopeGeom{th, r.Y + r.H/2, float32(r.H/2 - th)}
}

// y is the row of the line at sample v.
func (g scopeGeom) y(v float32) int { return g.mid - int(min(max(v, -1), 1)*g.amp) }

// span is the rows column i fills: the line from the column before to this one.
func (g scopeGeom) span(pts []float32, i int) (top, h int) {
	y := g.y(pts[i])
	if i == 0 {
		return y, g.th
	}
	prev := g.y(pts[i-1])
	return min(y, prev), abs(y-prev) + g.th
}

// drawScope: the wave as one accent line, a point per column, with vertical
// runs joining neighbours. Only the columns inside the clip are visited.
func (a *App) drawScope(c *gfx.Canvas, r gfx.Rect) {
	pts := a.viz.scope
	if len(pts) != r.W {
		return
	}
	g := a.scopeGeometry(r)
	c.Fill(gfx.R(r.X, g.mid, r.W, 1), colArtBg)
	clip := c.Clip().Intersect(r)
	for i := max(clip.X-r.X, 0); i < min(clip.Right()-r.X, r.W); i++ {
		top, h := g.span(pts, i)
		c.Fill(gfx.R(r.X+i, top, 1, h), colAccent)
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

const vuSegs = 43 // one per dB from -40 to +3

// vuGeom is where the meters stand in r.
type vuGeom struct{ gap, rowH, segGap, s, mx, segW int }

func (a *App) vuGeometry(r gfx.Rect) vuGeom {
	g := vuGeom{gap: max(r.H/10, 1), segGap: 1}
	g.rowH = (r.H - g.gap) / 2
	if !a.isCRT() {
		g.segGap = 2
	}
	g.s = min(max(g.rowH/6, 1), 6) // glyph dot size
	g.mx = r.X + 5*g.s
	g.segW = max((r.Right()-g.mx-g.segGap*(vuSegs-1))/vuSegs, 1)
	return g
}

// row is meter ch's top edge.
func (g vuGeom) row(r gfx.Rect, ch int) int { return r.Y + ch*(g.rowH+g.gap) }

// segs is the rectangle of segments i0 up to i1 of meter ch.
func (g vuGeom) segs(r gfx.Rect, ch, i0, i1 int) gfx.Rect {
	return gfx.R(g.mx+i0*(g.segW+g.segGap), g.row(r, ch), (i1-i0-1)*(g.segW+g.segGap)+g.segW, g.rowH)
}

// vuLit is how many segments of a meter at level are lit (they are a prefix).
func vuLit(level float32) int {
	n := 0
	for n < vuSegs && !(level < float32(n+1)/vuSegs) {
		n++
	}
	return n
}

// vuPeak is the segment of the peak marker, or -1.
func vuPeak(peak float32) int {
	if peak > 0 {
		return min(int(peak*vuSegs), vuSegs-1)
	}
	return -1
}

// drawVU: two horizontal LED meters, one segment per dB from -40 to +3,
// green, then yellow, then red above -3 dB, with a peak marker. Only what is
// inside the clip is visited.
func (a *App) drawVU(c *gfx.Canvas, r gfx.Rect) {
	level, peak := a.viz.an.VU()
	g := a.vuGeometry(r)
	clip := c.Clip().Intersect(r)
	if clip.Empty() {
		return
	}
	for ch, name := range "LR" {
		y := g.row(r, ch)
		if y >= clip.Bottom() || y+g.rowH <= clip.Y {
			continue
		}
		for gy, line := range glyphs[byte(name)] {
			for gx, dot := range line {
				if dot == '#' {
					c.Fill(gfx.R(r.X+g.s+gx*g.s, y+(g.rowH-5*g.s)/2+gy*g.s, g.s, g.s), colDim)
				}
			}
		}
		pk := vuPeak(peak[ch])
		step := g.segW + g.segGap
		i0 := max((clip.X-g.mx)/step, 0)
		for i := i0; i < vuSegs && g.mx+i*step < clip.Right(); i++ {
			col := colLEDGreen
			switch db := a.viz.an.DB(float32(i) / vuSegs); {
			case db >= -3:
				col = colError
			case db >= -12:
				col = colLEDYellow
			}
			switch {
			case i == pk:
				col = colText
			case level[ch] < float32(i+1)/vuSegs:
				col = mixColor(col, colBg, 210) // unlit
			}
			c.Fill(gfx.R(g.mx+i*step, y, g.segW, g.rowH), col)
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
