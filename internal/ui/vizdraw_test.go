package ui

import (
	"bytes"
	"log"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// fakeVisual is a fixed, deterministic signal: a sum of sines, with one tone
// that moves from call to call so the waterfall has something to sweep.
type fakeVisual struct{ pos, calls int }

func (f *fakeVisual) Window(dst []float32) int {
	n := len(dst) / 2
	moving := 300 + 400*float64(f.calls%24)
	for i := 0; i < n; i++ {
		t := float64(f.pos+i) / 48000
		l := 0.4*math.Sin(2*math.Pi*440*t) + 0.2*math.Sin(2*math.Pi*2000*t) + 0.1*math.Sin(2*math.Pi*9000*t)
		m := 0.2 * math.Sin(2*math.Pi*moving*t)
		dst[2*i], dst[2*i+1] = float32(l+m), float32(0.7*l+m)
	}
	f.pos += n
	f.calls++
	return n
}

// cachedVisual hands out the same window every time (the benchmark's signal:
// making sines would be most of what it measures).
type cachedVisual struct{ buf []float32 }

func (c *cachedVisual) Window(dst []float32) int {
	copy(dst, c.buf)
	return min(len(dst), len(c.buf)) / 2
}

// vizApp is Now Playing with the visualizer in style, playing, over a fake signal.
func vizApp(t *testing.T, prof Profile, style VizStyle) *testApp {
	t.Helper()
	ta := newTestApp(t, prof)
	ta.cfg = config.Default()
	ta.cfg.Display.Visualizer = style.String()
	ta.o.Visual = &fakeVisual{}
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	return ta
}

// runFrames lets the app's own wake loop run for n wakes of the fake clock.
func runFrames(t *testing.T, ta *testApp, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		ta.now = ta.now.Add(ta.untilWake())
		ta.onWake()
		if ta.redrawDue() {
			if err := ta.render(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func cropRect(c *gfx.Canvas, r gfx.Rect) *gfx.Canvas {
	out := gfx.NewCanvas(r.W, r.H)
	for y := 0; y < r.H; y++ {
		copy(out.Pix[y*r.W:(y+1)*r.W], c.Pix[(r.Y+y)*c.W+r.X:])
	}
	return out
}

var vizLayouts = []struct {
	name string
	prof Profile
}{
	{"hdmi", ProfileHDMI},
	{"hdmi-1920x1200", PickProfile(1920, 1200, "auto")},
	{"crt", ProfileCRT240},
}

func TestGoldenVizPanel(t *testing.T) {
	for _, l := range vizLayouts {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := vizApp(t, l.prof, style)
			ta.verify = false // checked in TestVizVerifyModeHolds; this keeps the big layouts quick
			runFrames(t, ta, 90)
			ta.verify = true
			runFrames(t, ta, 3)
			if ta.viz.rect.Empty() {
				t.Fatalf("%s %v: no panel", l.name, style)
			}
			golden(t, "viz-"+style.String()+"-"+l.name, cropRect(ta.disp.Last(), ta.viz.rect))
			if style == VizBars && l.name != "hdmi-1920x1200" {
				golden(t, "nowplaying-viz-"+l.name, ta.disp.Last())
			}
		}
	}
}

func TestVizPanelSitsBelowNextAndAsWideAsTheBar(t *testing.T) {
	for _, l := range vizLayouts {
		ta := vizApp(t, l.prof, VizBars)
		r, bar := ta.viz.rect, ta.ticks[0]
		if r.X != bar.X || r.W != bar.W {
			t.Errorf("%s: panel %v, progress bar %v", l.name, r, bar)
		}
		if r.Y < bar.Bottom()+ta.F.Small.Height() || r.H < 16 || r.Bottom() > l.prof.H-l.prof.SafeY {
			t.Errorf("%s: panel %v is not in the free part of the column", l.name, r)
		}
		t.Logf("%s: panel %v (progress bar %v)", l.name, r, bar)
	}
	if r := vizApp(t, ProfileCRT240, VizBars).viz.rect; r.H < 16 || r.H > 40 {
		t.Errorf("CRT strip is %d px high, want about 20", r.H)
	}
}

func TestVizVerifyModeHolds(t *testing.T) {
	for _, l := range vizLayouts[:1] {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := vizApp(t, l.prof, style) // newTestApp turns verify on and fails the test on a mismatch
			runFrames(t, ta, 40)
		}
	}
	ta := vizApp(t, ProfileCRT240, VizWaterfall)
	runFrames(t, ta, 40)
}

func TestVizFrameClockWakesAtTheFrameRate(t *testing.T) {
	for _, c := range []struct {
		prof Profile
		fps  int
	}{{ProfileHDMI, vizFPSHDMI}, {ProfileCRT240, vizFPSCRT}} {
		ta := vizApp(t, c.prof, VizBars)
		runFrames(t, ta, 3) // running
		if got, want := ta.untilWake(), time.Second/time.Duration(c.fps); got > want || got < want-2*time.Millisecond {
			t.Errorf("%s: next wake in %v, want %v", c.prof.Name, got, want)
		}
		ta.now = ta.now.Add(ta.untilWake())
		ta.clean()
		ta.onWake()
		if ta.dirty || len(ta.damage) == 0 || len(ta.viz.dmg) == 0 {
			t.Fatalf("%s: wake left dirty=%v damage=%v own=%v", c.prof.Name, ta.dirty, ta.damage, ta.viz.dmg)
		}
		for _, r := range ta.damage { // the progress regions every wake already brings
			if !containsRect(ta.ticks, r) {
				t.Errorf("%s: wake damaged %v, which is not progress %v", c.prof.Name, r, ta.ticks)
			}
		}
		for _, r := range ta.viz.dmg { // the panel's own changes, inside it
			if r.Intersect(ta.viz.rect) != r {
				t.Errorf("%s: the panel's damage %v leaves the panel %v", c.prof.Name, r, ta.viz.rect)
			}
		}
	}
}

func containsRect(rs []gfx.Rect, r gfx.Rect) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

func TestVizFrameClockStopsAfterPauseOnceIdle(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	runFrames(t, ta, 30)
	if ta.viz.an.Idle() {
		t.Fatal("silent while playing")
	}
	ta.pl.st.Status = player.Paused
	ta.onPlayer(player.Event{})
	runFrames(t, ta, 1)
	if !ta.vizActive() {
		t.Fatal("stopped at once: the levels have to fall first")
	}
	for i := 0; i < 300 && ta.vizActive(); i++ {
		runFrames(t, ta, 1)
	}
	if ta.vizActive() || !ta.viz.an.Idle() {
		t.Fatalf("still running: active=%v idle=%v", ta.vizActive(), ta.viz.an.Idle())
	}
	if !ta.vizDue().IsZero() {
		t.Errorf("a wake is still scheduled: %v", ta.vizDue())
	}
	if d := ta.untilWake(); d < time.Second {
		t.Errorf("woken again in %v while paused", d)
	}
	ta.pl.st.Status = player.Playing
	ta.onPlayer(player.Event{})
	ta.render()
	if !ta.vizActive() || ta.untilWake() > 40*time.Millisecond {
		t.Errorf("not restarted by Playing: active=%v wake in %v", ta.vizActive(), ta.untilWake())
	}
}

func TestVizOffOrNoVisualSchedulesNothing(t *testing.T) {
	off := vizApp(t, ProfileHDMI, VizOff)
	none := vizApp(t, ProfileHDMI, VizBars)
	none.o.Visual = nil
	none.dirty = true
	none.render()
	for name, ta := range map[string]*testApp{"off": off, "no visual": none} {
		if ta.vizActive() || !ta.vizDue().IsZero() || !ta.viz.rect.Empty() && name == "off" {
			t.Errorf("%s: active=%v due=%v rect=%v", name, ta.vizActive(), ta.vizDue(), ta.viz.rect)
		}
		if d := ta.untilWake(); d < progressTick {
			t.Errorf("%s: wakes in %v, before the progress tick", name, d)
		}
		ta.clean()
		ta.now = ta.now.Add(time.Second)
		ta.onWake()
		for _, r := range ta.damage {
			if !containsRect(ta.ticks, r) {
				t.Errorf("%s: wake damaged %v", name, r)
			}
		}
	}
	if none.viz.an != nil {
		t.Error("an analyzer was made with no Visual")
	}
}

// slowDisplay makes every present cost d of the fake clock.
type slowDisplay struct {
	*gfx.Headless
	ta *testApp
	d  *time.Duration
}

func (s slowDisplay) Present(c *gfx.Canvas) error {
	s.ta.now = s.ta.now.Add(*s.d)
	return s.Headless.Present(c)
}

func TestVizSafetyValveStepsDownAndUp(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(log.Writer())
	for _, c := range []struct {
		prof  Profile
		rates []int
	}{{ProfileHDMI, []int{30, 15, 10}}, {ProfileCRT240, []int{20, 10}}} {
		logs.Reset()
		ta := vizApp(t, c.prof, VizBars)
		cost := time.Millisecond
		ta.o.Display = slowDisplay{ta.disp, ta, &cost}
		fps := func() int { return ta.vizFPS() }
		upTo := func(d time.Duration) { // run the clock for d
			for end := ta.now.Add(d); ta.now.Before(end); {
				runFrames(t, ta, 1)
			}
		}
		upTo(5 * time.Second)
		if fps() != c.rates[0] {
			t.Fatalf("%s: a cheap frame slowed to %d fps", c.prof.Name, fps())
		}
		cost = 100 * time.Millisecond // far over any budget
		for i, want := range c.rates[1:] {
			upTo(1500 * time.Millisecond)
			if fps() != want {
				t.Fatalf("%s: step %d: %d fps, want %d (log: %s)", c.prof.Name, i+1, fps(), want, logs.String())
			}
			if !strings.Contains(logs.String(), "visualizer: slowing to "+strconv.Itoa(want)+" fps") {
				t.Errorf("%s: no log line for %d fps: %q", c.prof.Name, want, logs.String())
			}
		}
		upTo(5 * time.Second)
		if last := c.rates[len(c.rates)-1]; fps() != last {
			t.Fatalf("%s: went below the lowest rate: %d", c.prof.Name, fps())
		}
		cost = time.Millisecond // comfortably cheap again
		upTo(8 * time.Second)
		if fps() != c.rates[len(c.rates)-1] {
			t.Fatalf("%s: stepped up after only 8 s calm: %d", c.prof.Name, fps())
		}
		upTo(5 * time.Second)
		if fps() != c.rates[len(c.rates)-2] {
			t.Fatalf("%s: not one step up after 13 s calm: %d fps", c.prof.Name, fps())
		}
		upTo(40 * time.Second)
		if fps() != c.rates[0] {
			t.Fatalf("%s: never got back to %d fps: %d", c.prof.Name, c.rates[0], fps())
		}
		// the wake interval follows the rate
		if got, want := ta.vizDue().Sub(ta.viz.last), time.Second/time.Duration(c.rates[0]); got != want {
			t.Errorf("%s: frame interval %v, want %v", c.prof.Name, got, want)
		}
	}
}

func TestVizSafetyValveLogsOnceAtTheFloor(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(log.Writer())
	ta := vizApp(t, ProfileCRT240, VizBars) // rates 20, 10
	cost := 100 * time.Millisecond
	ta.o.Display = slowDisplay{ta.disp, ta, &cost}
	for end := ta.now.Add(10 * time.Second); ta.now.Before(end); {
		runFrames(t, ta, 1)
	}
	if got := strings.Count(logs.String(), "visualizer: over budget at 10 fps"); got != 1 {
		t.Fatalf("over-budget lines = %d, want 1 (log: %q)", got, logs.String())
	}
	cost = time.Millisecond // calm: steps up, and a new stay at the floor logs again
	for end := ta.now.Add(20 * time.Second); ta.now.Before(end); {
		runFrames(t, ta, 1)
	}
	cost = 100 * time.Millisecond
	for end := ta.now.Add(10 * time.Second); ta.now.Before(end); {
		runFrames(t, ta, 1)
	}
	if got := strings.Count(logs.String(), "visualizer: over budget at 10 fps"); got != 2 {
		t.Fatalf("after a second stay: %d lines, want 2 (log: %q)", got, logs.String())
	}
}

func TestVizFrameAllocatesNothing(t *testing.T) {
	for _, l := range vizLayouts {
		for style := VizBars; style <= VizWaterfall; style++ {
			for _, full := range []bool{false, true} {
				ta := vizApp(t, l.prof, style)
				if full {
					ta.press(input.BtnStart)
				}
				runFrames(t, ta, 3) // setup: the analyzer, the buffers, the image
				c, r := ta.canvas, ta.viz.rect
				if n := testing.AllocsPerRun(20, func() {
					ta.now = ta.now.Add(40 * time.Millisecond)
					ta.damage = ta.damage[:0] // the app clears it with every frame
					ta.vizTick(ta.now)
					ta.drawViz(c, r, style)
					if vs, ok := ta.Top().(*VizScreen); ok {
						vs.refresh(ta.App) // the wake checks the corner line every frame
					}
				}); n != 0 {
					t.Errorf("%s %v full=%v: %v allocations per frame", l.name, style, full, n)
				}
			}
		}
	}
}

// BenchmarkVizFrame is one visualizer frame: the analysis and the drawing
// into the panel (or the full screen), for each style on a big HDMI panel and
// a CRT strip.
// On the device: ./ui.test -test.run '^$' -test.bench VizFrame
func BenchmarkVizFrame(b *testing.B) {
	for _, l := range []struct {
		name string
		prof Profile
	}{{"hdmi-1920x1200", PickProfile(1920, 1200, "auto")}, {"crt-320x240", ProfileCRT240}} {
		for style := VizBars; style <= VizWaterfall; style++ {
			for _, full := range []bool{false, true} {
				name := l.name
				if full {
					name += "-full"
				}
				b.Run(name+"/"+style.String(), func(b *testing.B) {
					t := &testing.T{}
					ta := vizApp(t, l.prof, style)
					if full {
						ta.press(input.BtnStart)
					}
					ta.verify = false
					sig := make([]float32, 4096)
					(&fakeVisual{}).Window(sig)
					ta.o.Visual = &cachedVisual{sig}
					runFrames(t, ta, 3)
					c, r := ta.canvas, ta.viz.rect
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						ta.now = ta.now.Add(40 * time.Millisecond)
						ta.damage = ta.damage[:0] // the app clears it with every frame
						ta.vizTick(ta.now)
						ta.drawViz(c, r, style)
					}
				})
			}
		}
	}
}

// wholeFrameAllocs is the allocations of one wake, partial render and
// present of Now Playing in style, verify off. extra is damaged on every
// frame as well (the baseline gets the panel's rect this way).
func wholeFrameAllocs(t *testing.T, prof Profile, style VizStyle, extra gfx.Rect) float64 {
	ta := vizApp(t, prof, style)
	ta.verify = false
	runFrames(t, ta, 5) // setup
	var err error
	n := testing.AllocsPerRun(20, func() {
		ta.now = ta.now.Add(33 * time.Millisecond)
		ta.onWake()
		ta.Damage(extra)
		err = ta.render()
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestVizWholeFrameAllocatesNothing measures the real path: the wake, the
// merge of the damage, the partial render, the present and the valve's
// books. The Now Playing screen itself still allocates per frame (hint
// labels, the time and "Next:" strings: about 15 per clipped pass of the
// whole screen), whatever the style, so the visualizer is measured as the
// excess over the same screen with it off and the panel's rectangle
// damaged all the same.
func TestVizWholeFrameAllocatesNothing(t *testing.T) {
	for _, l := range vizLayouts {
		panel := vizApp(t, l.prof, VizBars).viz.rect
		base := wholeFrameAllocs(t, l.prof, VizOff, panel)
		for style := VizBars; style <= VizWaterfall; style++ {
			if n := wholeFrameAllocs(t, l.prof, style, gfx.Rect{}); n > base {
				t.Errorf("%s %v: %v allocations per whole frame, %v without the visualizer", l.name, style, n, base)
			}
		}
	}
}

// The baseline above pays for the merge too, so the merge is checked on its
// own: renderDamage must keep merging into the same storage.
func TestRenderDamageReusesTheMergeBuffer(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	runFrames(t, ta, 3)
	if len(ta.mergeBuf) == 0 {
		t.Fatal("no merged rectangles")
	}
	first := &ta.mergeBuf[0]
	runFrames(t, ta, 5)
	if &ta.mergeBuf[0] != first {
		t.Error("renderDamage merged into new storage")
	}
}

func TestMergeIntoReusesItsBuffer(t *testing.T) {
	rs := []gfx.Rect{gfx.R(0, 0, 10, 10), gfx.R(10, 0, 10, 10), gfx.R(50, 50, 5, 5), gfx.R(90, 90, 5, 5), gfx.R(70, 0, 1, 1)}
	buf := mergeInto(nil, rs, 2)
	if n := testing.AllocsPerRun(20, func() { buf = mergeInto(buf, rs, 2) }); n != 0 {
		t.Errorf("%v allocations", n)
	}
}

func TestVizWaterfallStartsCleanWhenChosenAgain(t *testing.T) {
	for _, away := range []VizStyle{VizBars, VizOff} {
		ta := vizApp(t, ProfileHDMI, VizWaterfall)
		runFrames(t, ta, 60)
		if ta.viz.cursor == 0 {
			t.Fatal("the sweep never moved")
		}
		ta.cfg.Display.Visualizer = away.String()
		ta.dirty = true
		ta.render() // a frame in the other style
		runFrames(t, ta, 3)
		ta.cfg.Display.Visualizer = VizWaterfall.String()
		ta.dirty = true
		ta.render()
		ta.vizTick(ta.now)
		cw := max(int(math.Round(float64(ta.viz.rect.W)/float64(vizSweep*ta.vizFPS()))), 1)
		if ta.viz.cursor != cw {
			t.Errorf("via %v: cursor at %d after the first frame, want %d", away, ta.viz.cursor, cw)
		}
		for i, px := range ta.viz.img.Pix {
			if x := i % ta.viz.img.W; x >= cw && px != 0 {
				t.Fatalf("via %v: stale picture at column %d", away, x)
			}
		}
	}
}
