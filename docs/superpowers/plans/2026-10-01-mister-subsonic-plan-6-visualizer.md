# MiSTer Subsonic — Plan 6: the visualizer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a visualizer that follows the music as it is heard. It has two forms:
- **A panel on Now Playing.** Select cycles Off, Bars, Scope, VU meters and Waterfall.
- **A full-screen ambient mode,** opened with Start.

Shuffle and repeat move to Now Playing's X menu. A short X opens the menu, and holding X stars.

**Architecture:**
- **`internal/viz`:** the analysis. FFT, bands, VU, scope and the waterfall column, in pure Go.
- **`internal/audio/tap.go`:** wraps the output device and keeps the frames the device took. `Window` returns the ones that end at `Consumed()`, which is what the speakers play.
- **`internal/ui`:**
  - draws the four styles into a rectangle;
  - runs a frame clock in the existing wake loop, with an automatic safety valve;
  - places the panel on Now Playing;
  - adds the full-screen `VizScreen`.
- **`cmd/mistersubsonic`:** puts the tap between the engine and the device.

**Tech Stack:** Go 1.26+, cgo (miniaudio, speexdsp) in `internal/audio` only, no new modules.

**Spec:** `docs/superpowers/specs/2026-10-01-mister-subsonic-visualizer-design.md`, approved by the user. It extends the main spec, `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`: §2, §8 and §13.

**Scope:**

| | |
|---|---|
| **Plans 1–5b (done)** | Playback, the TV interface, setup, MiSTer integration, releases, full resolution, partial redraws, hints, the follow-ups |
| **This plan (6)** | Everything in the spec, the PC benchmarks, and the TV checks written down |
| **Later** | The device numbers and the TV check (the MiSTer is off while this plan is written), the web remote (C), faster list scrolling |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:** `go test -race ./...`, the launcher tests, `make e2e` and the ARM builds pass. The ARM builds are `make mister mister-test`, with glibc 2.29 ≤ 2.31.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-6`. At every task the new tests failed before the code and passed after, and the tree ended identical to the prototype's, goldens included.
- **The MiSTer** was off, so nothing ran on it. The PC benchmark numbers are in Task 6's `docs/spikes.md` patch, next to "pending" for the device.
- **Goldens:** the prototype looked at every new golden screenshot.
  - The panel crops: four styles × HDMI 1280×720, 1920×1200 and CRT 320×240.
  - The full-screen screens for each style on the same three sizes.
  - Now Playing with the panel on HDMI and CRT.
  - The changed hint bars.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:**
  - Go 1.26+ (`go.mod` says `go 1.26.0`).
  - No new modules.
  - cgo only in `internal/audio`.
  - ARM build: `scripts/check-glibc.sh` must report ≤ 2.31, or no glibc for static binaries.
- **The visualizer must never cause an audio dropout** (spec §1).
  - The tap's `Write` holds its mutex only while copying, and it skips the copy after 1 s without a reader.
  - The UI's frame clock steps down 30 → 15 → 10 fps (CRT: 20 → 10) when a frame takes more than 60% of its budget.
- **Off by default:** `[display] visualizer = "off"`. With it off and no full screen, there are no wakes and no analysis.
- **No allocations per frame** in the visualizer's path after setup. `testing.AllocsPerRun` checks this.
- **Partial redraws:** the visualizer damages only its rectangle. The verify mode is on in every UI test and must pass with the visualizer running.
- **Controls (spec §2):**
  - Select: a short press cycles the style; a hold mutes.
  - Start: full screen on Now Playing, and still pause everywhere else.
  - X: a short press opens the menu; holding it for 1 s stars.
  - Every hinted button does something.
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device, never a real sound card.
  - Ask the user's go-ahead each time before a listening check, or before anything is shown on the TV.
- **Tests never touch real devices,** such as `/dev/tty*`, `/dev/fb0`, `/dev/input`, `/dev/MiSTer_cmd` or the MiSTer.
- **Secrets:** nothing from `.env` is printed, committed or written into docs.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These are the five situations most likely to bite the user. Each gets its test in the task that owns the code:

1. **The picture out of time with the sound.** The window must end at what the device has played, not at what was decoded.
   - Tests (Task 2): `TestTapWindowEndsAtConsumed`, `TestTapUnplayedFramesAreSkipped`, and `TestEngineThroughTapFillsWindow`.
2. **A dropout caused by the visualizer.** The tap's copy must never block playback on a reader, and a slow frame must lower the rate.
   - Tests: `TestTapConcurrentWriteAndWindow` under `-race` (Task 2), and `TestVizSafetyValveStepsDownAndUp`, which uses a fake clock and frame cost (Task 4).
3. **A stale or wrong pixel during the visualizer's partial frames.** The frame clock damages only its rectangle while everything else, the progress bar included, stays correct.
   - Tests (Task 4): `TestVizVerifyModeHolds`, and `TestVizFrameClockWakesAtTheFrameRate`, which also checks the damage.
4. **A lost control.** Select, X and Start each change meaning (style, menu and star, full screen), so the old actions must still be reachable and every hint must be true.
   - Shuffle and repeat are in both X menus.
   - Hold Select still mutes.
   - Start still pauses off Now Playing.
   - Tests: `TestEveryHintedButtonDoesSomething` with hold hints, `TestSelectHoldMutesAndLeavesTheStyle`, `TestQueueMenuHasShuffleAndRepeat` (Task 3), and `TestStartOnNowPlayingOpensFullScreenElsewherePauses` (Task 5).
5. **Burn-in and the screensaver in full screen.** Nothing static stays put, and the screensaver comes back when the music stops.
   - Tests (Task 5): `TestInfoLineMovesClockwiseOncePerMinute`, which uses a fake clock, and `TestScreensaverDoesNotStartInFullScreenWhilePlaying`.

## Decisions this plan makes (from the prototype; the spec left room)

- **Analysis:**
  - Bands take the maximum power over their FFT bins. The lowest bands, narrower than a bin, use the bin nearest their centre.
  - Attack is a 20 ms one-pole. Release and the peak fall are linear, 0.5 s from full to zero, and peaks hold 0.4 s.
  - The VU is a 300 ms one-pole on linear RMS, then dB. The peak marker follows the instantaneous RMS in dB and falls 15 dB/s.
  - The scope shows half a window, so its scale is constant, starting at the first rising zero crossing.
- **The menus:**
  - Short X opens a menu that closes after a choice, because `MenuScreen` pops before it runs an entry. Its labels show the state at the moment it opens.
  - Shuffle and repeat are now independent, so the mode line shows both ("Playing · Shuffle · Repeat all").
- **Hold hints:** `Hint.Hold` is drawn as a dim "hold" before the button. `TestEveryHintedButtonDoesSomething` holds the button for hold hints.
- **The panel:**
  - 1050×153 at 1920×1200.
  - 34 px high on CRT. It also takes the unused slack, so on CRT the text block moves up about 10 px when the visualizer is on.
- **The frame clock:**
  - Every wake while playing already damages the progress bar and time, so a visualizer frame draws up to three rectangles. Damaging the progress only when its pixels change is left for after the device measurement.
- **Full screen:**
  - `VizScreen` holds Now Playing below it and passes its keys on. The hold timers test `App.onNowPlaying`, which is true for Now Playing or the full screen above it.
  - Y pops the full screen first, so Back from the queue returns to Now Playing.
  - Start opens full screen only when there is a sound source and something is playing or paused. Otherwise Start still pauses, and the "Full screen" hint follows the same rule.
  - The hint bar is drawn over the bottom of the picture, so the picture never resizes. It shows on entry and for 3 s after any press.
  - The info line moves clockwise between the corners once a minute. It is damaged on its own only when its text changes or it moves.
  - Inside full screen, Select skips Off: Bars → Scope → VU → Waterfall → Bars.
- **Benchmarks:**
  - `BenchmarkVizFrame` (ui) has sub-benchmarks `hdmi-1920x1200/<style>` and `crt-320x240/<style>`, and full-screen cases. `BenchmarkAnalyzerUpdate` is in viz.
  - `viz.test` joins the ARM test binaries.
  - PC numbers: analysis 49 µs (HDMI) and 18 µs (CRT); a panel frame 88–157 µs at 1920×1200; full screen 0.7–2.0 ms at 1920×1200. These leave out the present.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/viz/*.go` | FFT, bands, VU, scope, palette, `Analyzer` | 1 |
| `internal/audio/tap.go`, `internal/ui/app.go`, `cmd/mistersubsonic/main.go` | the tap, `ui.Visual`, the wiring | 2 |
| `internal/config`, `internal/ui/viz.go`, `screens_play.go`, `hints.go`, `hints_screens.go`, `screens_settings.go`, `config.example.toml` | the setting, Select, the X menu and hold, the queue menu, hold hints | 3 |
| `internal/ui/viz.go`, `screens_play.go`, `app.go` | drawing, the panel, the frame clock, the safety valve, benchmarks | 4 |
| `internal/ui/screens_viz.go`, `screens_play.go`, `screensaver.go`, `app.go` | the full screen | 5 |
| README, `docs/spikes.md`, `docs/testing-on-mister.md`, main spec, backlog, Makefile | docs, the TV checks, `viz.test` | 6 |

---

### Task 1: internal/viz: the analysis

**Files:**
- Create: `internal/viz/fft.go`, `internal/viz/viz.go`
- Test: `internal/viz/viz_test.go` (new)

**Interfaces:**
- **Produces** (pure Go, standard library only, no allocations in `Update` after `New`):
  - `viz.Config{FFTSize, Bars, WaterfallBands, SampleRate int}`. `viz.HDMI()` is 2048/32/64/48000, and `viz.CRT()` is 1024/16/32/48000.
  - `viz.New(cfg) *Analyzer`.
    - `Update(stereo []float32, n int, dt time.Duration)` takes interleaved L,R. A short n zero-pads, and n = 0 decays.
    - Readers: `Bars() (level, peak []float32)`, `Column() []float32`, `VU() (level, peak [2]float32)`, `DB(level) float32`, `Scope(out []float32)` and `Idle() bool`.
  - `viz.Palette [256]uint32`: the waterfall colours, `0x00RRGGBB`, from dark blue through cyan and yellow to white.
  - Inside: a radix-2 float32 FFT (`newFFT`, `transform`), a Hann window, and log-spaced bands from 40 Hz to 16 kHz (`bandSet`) over −70..0 dB.
  - Ballistics: a 20 ms attack; a linear 0.5 s release; peaks hold 0.4 s. The VU is a 300 ms one-pole whose peak falls 15 dB/s. The scope shows half a window from a rising zero crossing.

- [ ] **Step 1: Write the failing tests**

`internal/viz/viz_test.go` (new file):

```go
package viz

import (
	"math"
	"math/cmplx"
	"testing"
	"time"
)

// sine fills n stereo frames of a sine at freq Hz with amplitude amp on the
// left and ampR on the right, starting at phase ph (radians).
func sine(n int, freq, amp, ampR, ph float64) []float32 {
	s := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		v := math.Sin(2*math.Pi*freq*float64(i)/48000 + ph)
		s[2*i] = float32(amp * v)
		s[2*i+1] = float32(ampR * v)
	}
	return s
}

// settle runs Update k times so attacks and smoothing have converged.
func settle(a *Analyzer, s []float32, n int, dt time.Duration, k int) {
	for i := 0; i < k; i++ {
		a.Update(s, n, dt)
	}
}

func argmax(x []float32) int {
	best := 0
	for i, v := range x {
		if v > x[best] {
			best = i
		}
	}
	return best
}

// bandOf is the index of the log band (40..16000 Hz, nb bands) holding f.
func bandOf(f float64, nb int) int {
	return int(float64(nb) * math.Log(f/40) / math.Log(16000.0/40))
}

func TestFFTMatchesDFT(t *testing.T) {
	const n = 64
	re := make([]float32, n)
	im := make([]float32, n)
	x := make([]complex128, n)
	for i := range re {
		re[i] = float32(math.Sin(float64(i)*0.7) + 0.3*math.Cos(float64(i)*2.9))
		im[i] = float32(0.2 * math.Sin(float64(i)*1.3))
		x[i] = complex(float64(re[i]), float64(im[i]))
	}
	f := newFFT(n)
	f.transform(re, im)
	for k := 0; k < n; k++ {
		var sum complex128
		for j := 0; j < n; j++ {
			sum += x[j] * cmplx.Exp(complex(0, -2*math.Pi*float64(j*k)/n))
		}
		got := complex(float64(re[k]), float64(im[k]))
		if cmplx.Abs(got-sum) > 1e-3 {
			t.Fatalf("bin %d: fft %v, dft %v", k, got, sum)
		}
	}
}

func TestSinePeaksInItsBand(t *testing.T) {
	for _, cfg := range []Config{HDMI(), CRT()} {
		a := New(cfg)
		s := sine(cfg.FFTSize, 1000, 1, 1, 0)
		settle(a, s, cfg.FFTSize, 33*time.Millisecond, 10)
		lvl, _ := a.Bars()
		want := bandOf(1000, cfg.Bars)
		if got := argmax(lvl); got != want {
			t.Fatalf("fft %d: peak band %d, want %d (%v)", cfg.FFTSize, got, want, lvl)
		}
		if lvl[want] < 0.9 {
			t.Errorf("fft %d: full-scale sine reads %.3f, want >= 0.9", cfg.FFTSize, lvl[want])
		}
		if lvl[want-1] >= lvl[want] || lvl[want+1] >= lvl[want] {
			t.Errorf("fft %d: neighbours %.3f %.3f not below %.3f", cfg.FFTSize, lvl[want-1], lvl[want+1], lvl[want])
		}
		col := a.Column()
		if len(col) != cfg.WaterfallBands {
			t.Fatalf("column len %d", len(col))
		}
		if got, w := argmax(col), bandOf(1000, cfg.WaterfallBands); got != w {
			t.Errorf("fft %d: waterfall peak %d, want %d", cfg.FFTSize, got, w)
		}
	}
}

func TestSweepMovesUp(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	prev := -1
	for b := 4; b < cfg.Bars-1; b += 3 { // the lowest bands are narrower than a bin
		// the geometric centre of band b
		f := 40 * math.Pow(400, (float64(b)+0.5)/float64(cfg.Bars))
		settle(a, sine(cfg.FFTSize, f, 0.8, 0.8, 0), cfg.FFTSize, time.Second, 3)
		lvl, _ := a.Bars()
		got := argmax(lvl)
		if got != b {
			t.Errorf("%.0f Hz: peak band %d, want %d", f, got, b)
		}
		if got <= prev {
			t.Errorf("%.0f Hz: band %d did not move up from %d", f, got, prev)
		}
		prev = got
	}
}

func TestSilence(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	settle(a, sine(cfg.FFTSize, 440, 1, 1, 0), cfg.FFTSize, 33*time.Millisecond, 10)
	if a.Idle() {
		t.Fatal("Idle while playing")
	}
	zero := make([]float32, 2*cfg.FFTSize)
	settle(a, zero, cfg.FFTSize, 33*time.Millisecond, 200)
	lvl, peak := a.Bars()
	for i := range lvl {
		if lvl[i] != 0 || peak[i] != 0 {
			t.Fatalf("band %d: %v %v after silence", i, lvl[i], peak[i])
		}
	}
	for i, v := range a.Column() {
		if v != 0 {
			t.Fatalf("column %d: %v", i, v)
		}
	}
	vl, vp := a.VU()
	if vl != [2]float32{} || vp != [2]float32{} {
		t.Fatalf("VU %v %v after silence", vl, vp)
	}
	if !a.Idle() {
		t.Fatal("not Idle after silence")
	}
}

func TestZeroFramesDecays(t *testing.T) {
	cfg := CRT()
	a := New(cfg)
	settle(a, sine(cfg.FFTSize, 440, 1, 1, 0), cfg.FFTSize, 33*time.Millisecond, 10)
	before, _ := a.Bars()
	top := before[argmax(before)]
	a.Update(nil, 0, 100*time.Millisecond)
	after, _ := a.Bars()
	if got := after[argmax(before)]; got >= top {
		t.Errorf("n=0 did not decay: %v -> %v", top, got)
	}
	for i := 0; i < 100; i++ {
		a.Update(nil, 0, 100*time.Millisecond)
	}
	if !a.Idle() {
		t.Error("not Idle after n=0 updates")
	}
}

func TestShortWindowZeroPads(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	s := sine(100, 1000, 1, 1, 0)
	a.Update(s, 100, 33*time.Millisecond)
	a.Update(s, 1, 33*time.Millisecond)
	a.Update(s[:4], 50, 33*time.Millisecond) // n larger than the slice
	big := sine(cfg.FFTSize*2, 1000, 1, 1, 0)
	a.Update(big, cfg.FFTSize*2, 33*time.Millisecond) // n larger than the FFT
	out := make([]float32, 50)
	a.Scope(out)
}

func TestBallisticsFollowDT(t *testing.T) {
	cfg := HDMI()
	loud := sine(cfg.FFTSize, 1000, 1, 0.5, 0)
	quiet := make([]float32, 2*cfg.FFTSize)
	b := bandOf(1000, cfg.Bars)
	// 0.66 s loud, then 0.33 s and 0.66 s of quiet, sampled mid-fall.
	run := func(dt time.Duration) (v []float32) {
		a := New(cfg)
		step := func(s []float32, d time.Duration) {
			for t := time.Duration(0); t < d; t += dt {
				a.Update(s, cfg.FFTSize, dt)
			}
		}
		sample := func() {
			lvl, peak := a.Bars()
			vl, vp := a.VU()
			v = append(v, lvl[b], peak[b], vl[0], vp[0])
		}
		step(loud, 660*time.Millisecond)
		sample()
		step(quiet, 330*time.Millisecond)
		sample()
		step(quiet, 330*time.Millisecond)
		sample()
		return v
	}
	x, y := run(33*time.Millisecond), run(66*time.Millisecond)
	for i := range x {
		if d := math.Abs(float64(x[i] - y[i])); d > 0.05 {
			t.Errorf("value %d differs between 30 and 15 fps:\n%v\n%v", i, x, y)
		}
	}
	// the test must see the motion: level 0.34 after 0.33 s of release, 0 after 0.66 s
	if x[4] > 0.5 || x[4] < 0.2 || x[8] != 0 || x[11] >= x[3] {
		t.Errorf("unexpected motion: %v", x)
	}
}

func TestVUChannels(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	settle(a, sine(cfg.FFTSize, 440, 0.5, 0, 0), cfg.FFTSize, 33*time.Millisecond, 30)
	l, p := a.VU()
	if l[0] <= 0.5 || p[0] < l[0] {
		t.Errorf("left VU %v peak %v", l[0], p[0])
	}
	if l[1] != 0 || p[1] != 0 {
		t.Errorf("right VU moved: %v %v", l[1], p[1])
	}
	// amplitude 0.5 has RMS 0.354 = -9 dB
	if db := a.DB(l[0]); math.Abs(float64(db)+9) > 1 {
		t.Errorf("DB(%v) = %v, want about -9", l[0], db)
	}
	if db := a.DB(1); math.Abs(float64(db)-3) > 1e-4 {
		t.Errorf("DB(1) = %v, want 3", db)
	}
	if db := a.DB(0); math.Abs(float64(db)+40) > 1e-4 {
		t.Errorf("DB(0) = %v, want -40", db)
	}
}

func TestVUPeakFalls(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	settle(a, sine(cfg.FFTSize, 440, 1, 1, 0), cfg.FFTSize, 33*time.Millisecond, 30)
	_, p0 := a.VU()
	zero := make([]float32, 2*cfg.FFTSize)
	settle(a, zero, cfg.FFTSize, 100*time.Millisecond, 1)
	_, p1 := a.VU()
	fall := a.DB(p0[0]) - a.DB(p1[0])
	if math.Abs(float64(fall)-1.5) > 0.2 {
		t.Errorf("peak fell %.2f dB in 100 ms, want about 1.5", fall)
	}
}

func TestScopeRisingZeroCrossing(t *testing.T) {
	cfg := HDMI()
	a := New(cfg)
	// 300 Hz: the window starts in the falling half of the wave.
	a.Update(sine(cfg.FFTSize, 300, 0.9, 0.9, 2.0), cfg.FFTSize, 33*time.Millisecond)
	out := make([]float32, 200)
	a.Scope(out)
	if math.Abs(float64(out[0])) > 0.1 || out[5] <= out[0] {
		t.Errorf("scope starts at %v, then %v: not a rising crossing", out[0], out[5])
	}
	// every point stays in range, and the wave is visible
	var hi float32
	for _, v := range out {
		if v < -1 || v > 1 {
			t.Fatalf("point %v out of range", v)
		}
		if v > hi {
			hi = v
		}
	}
	if hi < 0.5 {
		t.Errorf("scope peak %v: wave not drawn", hi)
	}
	// silence and no crossing: zeros, from the start
	a.Update(nil, 0, 33*time.Millisecond)
	a.Scope(out)
	for _, v := range out {
		if v != 0 {
			t.Fatal("silent scope not zero")
		}
	}
	a.Scope(nil)
}

func TestPalette(t *testing.T) {
	if Palette[0] == Palette[255] || Palette[255] != 0xFFFFFF {
		t.Errorf("palette ends %06x %06x", Palette[0], Palette[255])
	}
	if b := Palette[0] & 0xFF; b == 0 || Palette[0]>>16 > 0x30 {
		t.Errorf("palette starts %06x, want dark blue", Palette[0])
	}
	for i, c := range Palette {
		if c>>24 != 0 {
			t.Fatalf("entry %d = %08x has alpha bits", i, c)
		}
	}
}

func TestUpdateAllocs(t *testing.T) {
	for _, cfg := range []Config{HDMI(), CRT()} {
		a := New(cfg)
		s := sine(cfg.FFTSize, 1000, 0.7, 0.6, 0)
		out := make([]float32, 300)
		if n := testing.AllocsPerRun(50, func() {
			a.Update(s, cfg.FFTSize, 33*time.Millisecond)
			a.Bars()
			a.Column()
			a.VU()
			a.Scope(out)
			a.Idle()
		}); n != 0 {
			t.Errorf("fft %d: %v allocs per Update, want 0", cfg.FFTSize, n)
		}
	}
}

func BenchmarkAnalyzerUpdate(b *testing.B) {
	for _, bc := range []struct {
		name string
		cfg  Config
	}{{"HDMI", HDMI()}, {"CRT", CRT()}} {
		b.Run(bc.name, func(b *testing.B) {
			a := New(bc.cfg)
			s := sine(bc.cfg.FFTSize, 1000, 0.7, 0.6, 0)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				a.Update(s, bc.cfg.FFTSize, 33*time.Millisecond)
			}
		})
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/viz`

Expected: FAIL, e.g.:

```
undefined: Analyzer
undefined: newFFT
undefined: Config
undefined: HDMI
undefined: CRT
undefined: New
```

- [ ] **Step 3: Implement**

`internal/viz/fft.go` (new file):

```go
package viz

import "math"

// fft is a radix-2, in-place, float32 FFT of a fixed power-of-two size.
type fft struct {
	n        int
	rev      []int     // bit-reversal permutation
	cos, sin []float32 // twiddles e^(-2πik/n), k < n/2
}

func newFFT(n int) *fft {
	if n < 2 || n&(n-1) != 0 {
		panic("viz: FFT size must be a power of two")
	}
	f := &fft{n: n, rev: make([]int, n), cos: make([]float32, n/2), sin: make([]float32, n/2)}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range f.rev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		f.rev[i] = r
	}
	for k := range f.cos {
		a := -2 * math.Pi * float64(k) / float64(n)
		f.cos[k] = float32(math.Cos(a))
		f.sin[k] = float32(math.Sin(a))
	}
	return f
}

// transform replaces (re, im) with its discrete Fourier transform.
func (f *fft) transform(re, im []float32) {
	for i, j := range f.rev {
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= f.n; size <<= 1 {
		half, step := size/2, f.n/size
		for start := 0; start < f.n; start += size {
			for k := 0; k < half; k++ {
				wr, wi := f.cos[k*step], f.sin[k*step]
				a, b := start+k, start+k+half
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a] += tr
				im[a] += ti
			}
		}
	}
}
```

`internal/viz/viz.go` (new file):

```go
// Package viz turns the latest window of audio into what the visualizer
// draws: spectrum bars with peak caps, a waterfall column, VU meters and
// an oscilloscope trace. It is pure Go and allocates nothing in Update.
package viz

import (
	"math"
	"time"
)

// Config sizes an Analyzer.
type Config struct {
	FFTSize        int // power of two: 2048 on HDMI, 1024 on CRT
	Bars           int // spectrum bars: 32 on HDMI, 16 on CRT
	WaterfallBands int // waterfall rows: 64 on HDMI, 32 on CRT
	SampleRate     int // 48000
}

func HDMI() Config { return Config{FFTSize: 2048, Bars: 32, WaterfallBands: 64, SampleRate: 48000} }
func CRT() Config  { return Config{FFTSize: 1024, Bars: 16, WaterfallBands: 32, SampleRate: 48000} }

const (
	loHz, hiHz = 40.0, 16000.0 // the band range
	floorDB    = -70.0         // silence: the bottom of the 0..1 level range

	attackTau  = 0.020 // seconds: a rising band follows in about this time
	releaseSec = 0.5   // seconds for a band to fall from full to zero
	peakHold   = 0.4   // seconds a peak cap stays before it falls

	vuMinDB, vuMaxDB = -40.0, 3.0
	vuTau            = 0.300 // seconds of integration
	vuPeakFallDB     = 15.0  // dB per second: 1.5 dB per 100 ms
)

// bandSet is a row of log-spaced bands with smoothed levels.
type bandSet struct {
	lo, hi []int // FFT bins [lo, hi) of each band
	level  []float32
	peak   []float32 // nil for the waterfall
	hold   []float32 // seconds left before each peak cap falls
}

func newBandSet(nb, fftSize, rate int, peaks bool) bandSet {
	b := bandSet{lo: make([]int, nb), hi: make([]int, nb), level: make([]float32, nb)}
	if peaks {
		b.peak = make([]float32, nb)
		b.hold = make([]float32, nb)
	}
	df := float64(rate) / float64(fftSize)
	edge := func(i int) float64 { return loHz * math.Pow(hiHz/loHz, float64(i)/float64(nb)) }
	maxBin := fftSize / 2
	for i := 0; i < nb; i++ {
		f0, f1 := edge(i), edge(i+1)
		lo, hi := int(math.Ceil(f0/df)), int(math.Ceil(f1/df))
		if hi <= lo { // narrower than a bin: the bin nearest the centre
			lo = int(math.Round(math.Sqrt(f0*f1) / df))
			hi = lo + 1
		}
		lo = max(lo, 1)
		hi = min(max(hi, lo+1), maxBin)
		b.lo[i], b.hi[i] = lo, hi
	}
	return b
}

// update smooths in the new levels: a fast attack, a linear release.
func (b *bandSet) update(power []float32, win2 float32, dt float32) {
	atk := 1 - float32(math.Exp(-float64(dt)/attackTau))
	for i := range b.level {
		var p float32
		for _, v := range power[b.lo[i]:b.hi[i]] {
			p = max(p, v)
		}
		db := 10 * math.Log10(float64(p*win2)+1e-20)
		target := float32(min(max((db-floorDB)/-floorDB, 0), 1))
		if l := b.level[i]; target > l {
			b.level[i] = l + (target-l)*atk
		} else {
			b.level[i] = max(l-dt/releaseSec, target)
		}
		if b.peak == nil {
			continue
		}
		switch {
		case b.level[i] >= b.peak[i]:
			b.peak[i], b.hold[i] = b.level[i], peakHold
		case b.hold[i] > dt:
			b.hold[i] -= dt
		default: // the hold ran out during this step
			fall := dt - b.hold[i]
			b.hold[i] = 0
			b.peak[i] = max(b.peak[i]-fall/releaseSec, b.level[i])
		}
	}
}

func (b *bandSet) zero() bool {
	for i, v := range b.level {
		if v != 0 || (b.peak != nil && b.peak[i] != 0) {
			return false
		}
	}
	return true
}

// Analyzer holds the analysis state. It is not safe for concurrent use.
type Analyzer struct {
	cfg         Config
	fft         *fft
	hann        []float32
	re, im      []float32
	power       []float32 // |X[k]|², k < FFTSize/2
	win2        float32   // scales power to the squared amplitude of a sine
	bars, wfall bandSet

	mono  []float32 // the latest window mixed to mono, zero-padded
	valid int       // frames of mono that are real

	vuRMS     [2]float32 // integrated RMS
	vuPeakDB  [2]float32
	vuLevel   [2]float32
	vuPeakLvl [2]float32
}

func New(cfg Config) *Analyzer {
	n := cfg.FFTSize
	a := &Analyzer{
		cfg: cfg, fft: newFFT(n),
		hann: make([]float32, n), re: make([]float32, n), im: make([]float32, n),
		power: make([]float32, n/2), mono: make([]float32, n),
		bars:  newBandSet(cfg.Bars, n, cfg.SampleRate, true),
		wfall: newBandSet(cfg.WaterfallBands, n, cfg.SampleRate, false),
	}
	for i := range a.hann {
		a.hann[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n)))
	}
	// A sine of amplitude A gives |X| = A·Σw/2 = A·N/4 at its bin.
	a.win2 = float32(16 / (float64(n) * float64(n)))
	a.vuPeakDB = [2]float32{vuMinDB, vuMinDB}
	return a
}

// Update analyses the latest window: stereo is interleaved L,R float32, n
// frames (n may be less than FFTSize: zero-pad; 0 = silence), dt the real
// time since the last Update (ballistics use it).
func (a *Analyzer) Update(stereo []float32, n int, dt time.Duration) {
	size := a.cfg.FFTSize
	n = min(max(n, 0), len(stereo)/2)
	if n > size { // keep the newest frames
		stereo = stereo[2*(n-size):]
		n = size
	}
	sec := float32(max(dt, 0).Seconds())

	var sq [2]float64
	for i := 0; i < n; i++ {
		l, r := stereo[2*i], stereo[2*i+1]
		a.mono[i] = (l + r) / 2
		sq[0] += float64(l) * float64(l)
		sq[1] += float64(r) * float64(r)
	}
	clear(a.mono[n:])
	a.valid = n

	a.updateVU(sq, n, sec)

	if n == 0 { // nothing to transform: the bands just fall
		clear(a.power)
	} else {
		for i, v := range a.mono {
			a.re[i] = v * a.hann[i]
		}
		clear(a.im)
		a.fft.transform(a.re, a.im)
		for k := range a.power {
			a.power[k] = a.re[k]*a.re[k] + a.im[k]*a.im[k]
		}
	}
	a.bars.update(a.power, a.win2, sec)
	a.wfall.update(a.power, a.win2, sec)
}

func (a *Analyzer) updateVU(sq [2]float64, n int, dt float32) {
	k := float32(1 - math.Exp(-float64(dt)/vuTau))
	for c := range sq {
		var rms float64
		if n > 0 {
			rms = math.Sqrt(sq[c] / float64(n))
		}
		a.vuRMS[c] += (float32(rms) - a.vuRMS[c]) * k
		a.vuLevel[c] = vuScale(a.vuRMS[c])
		// the peak marker follows the instantaneous level, then falls
		db := float32(vuMinDB)
		if rms > 1e-9 {
			db = float32(min(max(20*math.Log10(rms), vuMinDB), vuMaxDB))
		}
		a.vuPeakDB[c] = max(db, a.vuPeakDB[c]-vuPeakFallDB*dt, vuMinDB)
		a.vuPeakLvl[c] = (a.vuPeakDB[c] - vuMinDB) / (vuMaxDB - vuMinDB)
	}
}

// vuScale maps a linear RMS onto 0..1 over −40..+3 dB.
func vuScale(rms float32) float32 {
	if rms < 1e-9 {
		return 0
	}
	db := 20 * math.Log10(float64(rms))
	return float32(min(max((db-vuMinDB)/(vuMaxDB-vuMinDB), 0), 1))
}

// Bars returns the spectrum levels and peak caps, 0..1 each. Do not modify.
func (a *Analyzer) Bars() (level, peak []float32) { return a.bars.level, a.bars.peak }

// Column returns the waterfall bands 0..1, low to high. Do not modify.
func (a *Analyzer) Column() []float32 { return a.wfall.level }

// VU returns the meter levels and peak markers, 0..1 on the −40..+3 dB
// scale, left and right.
func (a *Analyzer) VU() (level, peak [2]float32) { return a.vuLevel, a.vuPeakLvl }

// DB maps a VU level back to dB.
func (a *Analyzer) DB(level float32) float32 { return vuMinDB + level*(vuMaxDB-vuMinDB) }

// Scope fills out with the latest window decimated to len(out) points in
// −1..1, starting at a rising zero crossing (if none, from the start). It
// shows half a window, so the picture's scale does not change with where
// the crossing falls.
func (a *Analyzer) Scope(out []float32) {
	if len(out) == 0 {
		return
	}
	span := a.valid / 2
	if span < 1 {
		clear(out)
		return
	}
	start := 0
	for i := 1; i <= span; i++ {
		if a.mono[i-1] < 0 && a.mono[i] >= 0 {
			start = i
			break
		}
	}
	for j := range out {
		out[j] = a.mono[start+j*span/len(out)]
	}
}

// Idle reports that every level and peak is at zero, so the frame clock
// may stop.
func (a *Analyzer) Idle() bool {
	return a.bars.zero() && a.wfall.zero() &&
		a.vuLevel == [2]float32{} && a.vuPeakLvl == [2]float32{}
}

// Palette holds the waterfall colours as 0x00RRGGBB: dark blue, cyan,
// yellow, white.
var Palette = buildPalette()

func buildPalette() (p [256]uint32) {
	stops := [...]struct {
		at      float64
		r, g, b float64
	}{{0, 0, 0, 40}, {0.35, 0, 220, 255}, {0.7, 255, 230, 0}, {1, 255, 255, 255}}
	for i := range p {
		t := float64(i) / 255
		s := 0
		for s < len(stops)-2 && t > stops[s+1].at {
			s++
		}
		a, b := stops[s], stops[s+1]
		f := (t - a.at) / (b.at - a.at)
		c := func(x, y float64) uint32 { return uint32(math.Round(x + (y-x)*f)) }
		p[i] = c(a.r, b.r)<<16 | c(a.g, b.g)<<8 | c(a.b, b.b)
	}
	return p
}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/viz`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `go test -count=1 -run '^$' -bench AnalyzerUpdate ./internal/viz`. Expected: two lines, `hdmi` (about 50 µs on a desktop) and `crt`, both with `0 allocs/op`.

- [ ] **Step 5: Commit**

```bash
git add internal/viz/fft.go internal/viz/viz.go internal/viz/viz_test.go
git commit -m "viz: spectrum bars, VU, scope and waterfall from the audio window, in pure Go" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: The audio tap, wired to the UI

**Files:**
- Create: `internal/audio/tap.go`
- Modify: `internal/audio/engine_test.go`, `internal/ui/app.go`, `cmd/mistersubsonic/main.go`, `cmd/mistersubsonic/main_test.go`
- Test: `internal/audio/tap_test.go` (new)

**Interfaces:**
- **Produces:**
  - **The tap.** `audio.NewTap(out Output, frames int) *Tap` and `audio.TapFrames = 32768`. `*Tap` implements `Output`:
    - **`Write`** passes the frames to the device, then copies the ones the device took into its ring.
    - **`Flush`** clears the ring.
    - **The rest** pass through.
    - **`Window(dst []float32) int`** fills dst with the latest frames that end at `Consumed()`, and returns how many it filled.
    - **The idle skip:** after 1 s with no `Window` call, `Write` skips the copy. Its clock is the injectable `now` field.
  - **`ui.Visual`** is `interface{ Window(dst []float32) int }`. It comes with `ui.Options.Visual` and `(*App).Visual() Visual`.
  - **`cmd/mistersubsonic`** wraps the opened device in `NewTap` before `audio.NewEngine`, and passes the tap as `Options.Visual`.

- [ ] **Step 1: Write the failing tests**

`internal/audio/tap_test.go` (new file):

```go
package audio

import (
	"sync"
	"testing"
	"time"
)

// stereo makes n frames whose left sample is first, first+1, ... and whose
// right sample is the negative of it.
func stereo(first, n int) []float32 {
	s := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		s[2*i], s[2*i+1] = float32(first+i), -float32(first+i)
	}
	return s
}

// lefts returns the left samples of interleaved stereo.
func lefts(s []float32) []int {
	var l []int
	for i := 0; i < len(s); i += 2 {
		l = append(l, int(s[i]))
	}
	return l
}

type tapClock struct{ t time.Time }

func (c *tapClock) now() time.Time { return c.t }

// newTestTap returns a tap on a fake output (ring ringFrames) with a clock
// the test moves; the tap starts out being read.
func newTestTap(ringFrames, tapFrames int) (*Tap, *fakeOutput, *tapClock) {
	out := newFakeOutput(ringFrames)
	clk := &tapClock{t: time.Unix(1000, 0)}
	tp := NewTap(out, tapFrames)
	tp.now = clk.now
	tp.Window(make([]float32, 2)) // mark it read
	return tp, out, clk
}

func TestTapKeepsOnlyTheFramesTheDeviceTook(t *testing.T) {
	tp, out, _ := newTestTap(10, 64)
	if n := tp.Write(stereo(1, 25)); n != 10 {
		t.Fatalf("Write took %d, want 10", n)
	}
	out.consume(10)
	dst := make([]float32, 2*64)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if len(got) != 10 || got[0] != 1 || got[9] != 10 {
		t.Fatalf("window = %v, want 1..10", got)
	}
}

func TestTapWindowEndsAtConsumed(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 50))
	out.consume(30)
	dst := make([]float32, 2*8)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if n != 8 || got[0] != 23 || got[7] != 30 {
		t.Fatalf("window = %v, want 23..30", got)
	}
	if dst[1] != -23 {
		t.Fatalf("right channel = %v, want -23", dst[1])
	}
}

func TestTapRingWraps(t *testing.T) {
	tp, out, _ := newTestTap(100, 16)
	for i := 0; i < 5; i++ { // 5*7 = 35 frames through a 16-frame ring
		tp.Write(stereo(1+i*7, 7))
	}
	out.consume(35)
	dst := make([]float32, 2*10)
	n := tp.Window(dst)
	got := lefts(dst[:2*n])
	if n != 10 || got[0] != 26 || got[9] != 35 {
		t.Fatalf("window = %v, want 26..35", got)
	}
	// Asking for more than the ring holds gives the ring.
	big := make([]float32, 2*40)
	if n = tp.Window(big); n != 16 || lefts(big[:2*n])[0] != 20 {
		t.Fatalf("big window n=%d first=%d, want 16 from 20", n, lefts(big[:2*n])[0])
	}
}

func TestTapUnplayedFramesAreSkipped(t *testing.T) {
	tp, out, _ := newTestTap(100, 16)
	tp.Write(stereo(1, 40))
	out.consume(40)
	tp.Write(stereo(41, 10)) // sit in the device ring, not heard yet
	dst := make([]float32, 2*4)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 4 || got[3] != 40 {
		t.Fatalf("window = %v, want 37..40", got)
	}
}

func TestTapShortRingReturnsFewer(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 5))
	out.consume(5)
	dst := make([]float32, 2*32)
	if n := tp.Window(dst); n != 5 {
		t.Fatalf("n = %d, want 5", n)
	}
}

func TestTapFlushClears(t *testing.T) {
	tp, out, _ := newTestTap(100, 64)
	tp.Write(stereo(1, 20))
	out.consume(20)
	tp.Flush()
	if out.flushes != 1 {
		t.Fatal("Flush not passed through")
	}
	dst := make([]float32, 2*8)
	if n := tp.Window(dst); n != 0 {
		t.Fatalf("after Flush n = %d, want 0", n)
	}
	tp.Write(stereo(100, 3))
	out.consume(3)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 3 || got[0] != 100 {
		t.Fatalf("after new writes window = %v, want 100..102", got)
	}
}

func TestTapIdleSkipsTheCopyAndResumes(t *testing.T) {
	tp, out, clk := newTestTap(1000, 64)
	dst := make([]float32, 2*8)
	tp.Write(stereo(1, 10))
	out.consume(10)
	if n := tp.Window(dst); n != 8 { // reads at t0
		t.Fatalf("n = %d, want 8", n)
	}
	clk.t = clk.t.Add(1500 * time.Millisecond) // nobody reads for over 1 s
	tp.Write(stereo(11, 10))
	out.consume(10)
	if n := tp.Window(dst); n != 0 { // this read wakes it; nothing was copied
		t.Fatalf("idle window n = %d, want 0", n)
	}
	tp.Write(stereo(21, 10))
	out.consume(10)
	n := tp.Window(dst)
	if got := lefts(dst[:2*n]); n != 8 || got[0] != 23 || got[7] != 30 {
		t.Fatalf("resumed window = %v, want 23..30", got)
	}
	// Reading keeps it awake: 0.9 s later is still not idle.
	clk.t = clk.t.Add(900 * time.Millisecond)
	tp.Write(stereo(31, 4))
	out.consume(4)
	if n = tp.Window(dst); n != 8 || lefts(dst[:2*n])[7] != 34 {
		t.Fatalf("window after a short gap n = %d", n)
	}
}

func TestTapPassesThrough(t *testing.T) {
	tp, out, _ := newTestTap(10, 16)
	tp.SetPaused(true)
	tp.SetVolume(0.5)
	tp.Write(stereo(1, 4))
	out.consume(3)
	if tp.Consumed() != 3 {
		t.Fatalf("Consumed = %d, want 3", tp.Consumed())
	}
	if err := tp.Close(); err != nil {
		t.Fatal(err)
	}
}

// A writer and a reader at once; run under -race.
func TestTapConcurrentWriteAndWindow(t *testing.T) {
	out := newFakeOutput(1 << 30)
	tp := NewTap(out, 256)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 2000; i++ {
			tp.Write(stereo(i*16, 16))
			out.consume(16)
		}
	}()
	go func() {
		defer wg.Done()
		dst := make([]float32, 2*64)
		for {
			select {
			case <-stop:
				return
			default:
				n := tp.Window(dst)
				for i := 1; i < n; i++ { // frames are consecutive
					if dst[2*i] != dst[2*i-2]+1 {
						t.Errorf("window not consecutive at %d", i)
						return
					}
				}
			}
		}
	}()
	wg.Wait()
}
```

Then update the existing tests. Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index 6ac040f..217e227 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -285,3 +285,20 @@ func TestAnUnopenableCrashFileIsLogged(t *testing.T) {
 		t.Fatalf("log:\n%s", b)
 	}
 }
+
+// The UI gets the audio tap, so the visualizer can see what is playing.
+func TestUIOptionsGetTheAudioTap(t *testing.T) {
+	nullDevice(t)
+	_, cfg := writeConfig(t, "http://127.0.0.1:1")
+	var got ui.Visual
+	old := beforeRun
+	beforeRun = func(a *ui.App) { got = a.Visual() }
+	defer func() { beforeRun = old }()
+	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 200 * time.Millisecond})
+	if err != nil {
+		t.Fatalf("run: %v", err)
+	}
+	if _, ok := got.(*audio.Tap); !ok {
+		t.Fatalf("Options.Visual = %T, want *audio.Tap", got)
+	}
+}
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index 769bef0..5dbf5bb 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -1424,3 +1424,30 @@ func TestEngineReplaceOpenFailureThenQueueNextStarts(t *testing.T) {
 	}()
 	expectEvent(t, e, EventStarted, 2)
 }
+
+// A track played through a Tap over the null device leaves a window of what
+// was heard.
+func TestEngineThroughTapFillsWindow(t *testing.T) {
+	dev, err := OpenDevice(DeviceOptions{Null: true})
+	if err != nil {
+		t.Fatal(err)
+	}
+	tp := NewTap(dev, TapFrames)
+	tp.Window(make([]float32, 2)) // a reader is present
+	e := NewEngine(EngineOptions{Output: tp, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
+	defer e.Close()
+	defer tp.Close()
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(OutputRate, 1), OutputRate)})
+	dst := make([]float32, 2*512)
+	deadline := time.After(3 * time.Second)
+	for {
+		if n := tp.Window(dst); n == 512 {
+			return
+		}
+		select {
+		case <-deadline:
+			t.Fatal("the tap's window never filled")
+		case <-time.After(5 * time.Millisecond):
+		}
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/audio ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: ui.Visual
a.Visual undefined (type *ui.App has no field or method Visual)
undefined: Tap
undefined: NewTap
undefined: TapFrames
```

- [ ] **Step 3: Implement**

`internal/audio/tap.go` (new file):

```go
package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// TapFrames is the app's tap size: about 0.68 s, which holds the device ring
// (up to 0.5 s not yet played) plus a window.
const TapFrames = 32768

// tapIdle is how long without a Window call before Write stops copying.
const tapIdle = time.Second

// Tap wraps an Output and keeps the last frames the device took, so the
// visualizer can see what is playing now. Everything passes through.
type Tap struct {
	out Output
	now func() time.Time // the clock (tests set it)

	lastRead atomic.Int64 // when Window last ran, UnixNano

	mu      sync.Mutex
	ring    []float32 // interleaved stereo; frame i is at ring[2*(i%size)]
	size    int       // frames
	written uint64    // frames the device has taken, ever
	floor   uint64    // frames before this are not in the ring (Flush, idle)
}

// NewTap wraps out; frames is the ring size in stereo frames.
func NewTap(out Output, frames int) *Tap {
	return &Tap{out: out, now: time.Now, ring: make([]float32, 2*frames), size: frames}
}

// Write passes frames to the device, then keeps the ones it took, unless
// nobody has looked for a second.
func (t *Tap) Write(frames []float32) int {
	n := t.out.Write(frames)
	idle := t.now().UnixNano()-t.lastRead.Load() > int64(tapIdle)
	t.mu.Lock()
	if idle {
		t.written += uint64(n)
		t.floor = t.written
	} else {
		if n > t.size { // only the last size frames can matter
			frames = frames[2*(n-t.size):]
			t.written += uint64(n - t.size)
			n = t.size
		}
		pos := int(t.written % uint64(t.size))
		first := min(n, t.size-pos)
		copy(t.ring[2*pos:], frames[:2*first])
		copy(t.ring, frames[2*first:2*n])
		t.written += uint64(n)
	}
	t.mu.Unlock()
	return n
}

// Window fills dst (interleaved stereo, len(dst)/2 frames wanted) with the
// latest frames that end at the device's Consumed position, the ones playing
// now, and returns how many it filled. That is fewer when the tap doesn't
// hold that many, for example right after Flush.
func (t *Tap) Window(dst []float32) int {
	t.lastRead.Store(t.now().UnixNano())
	end := t.out.Consumed()
	want := min(len(dst)/2, t.size)
	t.mu.Lock()
	defer t.mu.Unlock()
	end = min(end, t.written)
	start := max(t.floor, t.written-min(t.written, uint64(t.size)))
	if end < uint64(want) || end-uint64(want) < start {
		if end <= start {
			return 0
		}
		want = int(end - start)
	}
	from := end - uint64(want)
	pos := int(from % uint64(t.size))
	first := min(want, t.size-pos)
	copy(dst, t.ring[2*pos:2*(pos+first)])
	copy(dst[2*first:], t.ring[:2*(want-first)])
	return want
}

func (t *Tap) Consumed() uint64    { return t.out.Consumed() }
func (t *Tap) SetPaused(p bool)    { t.out.SetPaused(p) }
func (t *Tap) SetVolume(v float32) { t.out.SetVolume(v) }
func (t *Tap) Close() error        { return t.out.Close() }

// Flush discards what the device buffered and what the tap kept.
func (t *Tap) Flush() {
	t.out.Flush()
	t.mu.Lock()
	t.floor = t.written
	t.mu.Unlock()
}
```

Then Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 89f1c55..8f95a17 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -395,10 +395,13 @@ func run(f flags) (err error) {
 		dev = ""
 	}
 	var eng *audio.Engine
+	var visual ui.Visual // the tap on what is playing (nil without a device)
 	out, audioErr := openDevice(audio.DeviceOptions{Name: dev, Null: f.null})
 	if audioErr == nil {
 		defer out.Close()
-		eng = audio.NewEngine(audio.EngineOptions{Output: out})
+		tap := audio.NewTap(out, audio.TapFrames)
+		visual = tap
+		eng = audio.NewEngine(audio.EngineOptions{Output: tap})
 		defer eng.Close()
 	}
 
@@ -421,6 +424,7 @@ func run(f flags) (err error) {
 		VerifyRedraw:  f.verifyRedraw,
 		WatchDisplay:  watchDisplay,
 		PadAtStart:    padAtStart,
+		Visual:        visual,
 		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
 	})
 	if err != nil {
diff --git a/internal/ui/app.go b/internal/ui/app.go
index a56c682..15c77ef 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -123,8 +123,21 @@ type Options struct {
 	// goroutine, replacing any previous one; it answers with
 	// a.Connected or a.ConnectFailed (through a.Post).
 	Connect func(a *App, cfg *config.Config)
+	// Visual is what the visualizer reads: the frames being heard right now
+	// (nil: no sound device, so nothing to show).
+	Visual Visual
 }
 
+// Visual gives the visualizer the audio that is playing: Window fills dst
+// (interleaved stereo float32) with the latest frames ending at the playback
+// position and returns how many frames it filled. *audio.Tap implements it.
+type Visual interface {
+	Window(dst []float32) int
+}
+
+// Visual returns Options.Visual.
+func (a *App) Visual() Visual { return a.o.Visual }
+
 // Fonts used by the screens.
 type Fonts struct {
 	Title, Body, Small *gfx.Font
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/audio ./internal/ui ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9" go vet ./...` with zig on `PATH`. Expected: no output, which means the ARM build of the new code vets clean. `internal/audio` changed, so also run `make mister mister-test`. Expected: every glibc line ends in `ok`.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go cmd/mistersubsonic/main_test.go internal/audio/engine_test.go internal/audio/tap.go internal/audio/tap_test.go internal/ui/app.go
git commit -m "audio, ui, cmd: a tap on the output keeps the frames being heard, for the visualizer" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Now Playing controls, the menus, the setting

**Files:**
- Create: `internal/ui/viz.go`
- Modify: `internal/config/config.go`, `internal/config/edit.go`, `internal/ui/hints.go`, `internal/ui/hints_screens.go`, `internal/ui/screens_play.go`, `internal/ui/screens_settings.go`, `sdcard/mistersubsonic/config.example.toml`
- Test: `internal/ui/vizcontrols_test.go` (new); `internal/config/config_test.go`, `internal/config/edit_test.go`, `internal/ui/hints_test.go`, `internal/ui/mute_test.go`, `internal/ui/play_test.go`, `internal/ui/screensaver_test.go`, `internal/ui/ui_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **The config key.** `config.Display.Visualizer` (`visualizer`): "off", "bars", "scope", "vu" or "waterfall". The default is "off". An unknown value loads as "off" with a warning. The config editor knows the key.
  - **The style type.**
    - `ui.VizStyle`, with the values `VizOff`, `VizBars`, `VizScope`, `VizVU` and `VizWaterfall`.
    - `ParseVizStyle`, `String` (the config name) and `Label` ("Off", "Bars", "Scope", "VU meters", "Waterfall").
    - `(*App).VizStyle()` and `(*App).SetVizStyle(v)`, which saves at once.
  - **Now Playing:**
    - **Short Select** (on Release) cycles the style, toasts "Visualizer: <Label>" and saves. `cycleMode` and `playModes` are removed.
    - **Hold Select** still mutes.
    - **Short X** (on Release) opens the "Now Playing" menu: Star/Unstar, "Shuffle: On/Off", "Repeat: Off/All/One". It closes after a choice.
    - **Hold X** (`starHold`, 1 s) stars or unstars at once, with a fresh hold per press.
    - **The mode line** shows shuffle and repeat independently.
  - **The queue's X menu** gains the same Shuffle and Repeat entries (`modeEntries`).
  - **Hints.** `Hint.Hold` and `hkHold(b, label)`, drawn as a dim "hold" before the button. Now Playing shows Menu, hold Star, Visualizer and hold Mute. `TestEveryHintedButtonDoesSomething` holds the button for hold hints.
  - **Settings.** Settings → Display gets a "Visualizer" row with a help line.

- [ ] **Step 1: Write the failing tests**

`internal/ui/vizcontrols_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

func npApp(t *testing.T) *testApp {
	t.Helper()
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	return ta
}

func hold(ta *testApp, b input.Button, d time.Duration) {
	ta.onInput(input.Event{Button: b, Kind: input.Press})
	ta.now = ta.now.Add(d)
	ta.onWake()
	ta.onInput(input.Event{Button: b, Kind: input.Repeat})
	ta.onInput(input.Event{Button: b, Kind: input.Release})
}

func TestVizStyleNames(t *testing.T) {
	want := []struct {
		s           VizStyle
		name, label string
	}{{VizOff, "off", "Off"}, {VizBars, "bars", "Bars"}, {VizScope, "scope", "Scope"}, {VizVU, "vu", "VU meters"}, {VizWaterfall, "waterfall", "Waterfall"}}
	for _, w := range want {
		if w.s.String() != w.name || w.s.Label() != w.label || ParseVizStyle(w.name) != w.s {
			t.Errorf("%d: %q %q parse %v", w.s, w.s.String(), w.s.Label(), ParseVizStyle(w.name))
		}
	}
	if ParseVizStyle("disco") != VizOff || ParseVizStyle("") != VizOff {
		t.Error("an unknown name is not Off")
	}
}

func TestSelectCyclesTheVisualizerAndSaves(t *testing.T) {
	ta := npApp(t)
	if ta.VizStyle() != VizOff {
		t.Fatalf("starts at %v", ta.VizStyle())
	}
	for _, want := range []VizStyle{VizBars, VizScope, VizVU, VizWaterfall, VizOff} {
		ta.press(input.BtnSelect)
		if ta.VizStyle() != want || ta.cfg.Display.Visualizer != want.String() {
			t.Fatalf("after Select: %v / %q, want %v", ta.VizStyle(), ta.cfg.Display.Visualizer, want)
		}
		if got := ta.toasts[len(ta.toasts)-1].text; got != "Visualizer: "+want.Label() {
			t.Fatalf("toast %q", got)
		}
		ta.settle(t)
		ta.flushConfig()
		waitFile(t, ta.o.ConfigPath, `visualizer = "`+want.String()+`"`)
	}
	if ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
		t.Fatal("Select still changes the play mode")
	}
}

func TestSelectHoldMutesAndLeavesTheStyle(t *testing.T) {
	ta := npApp(t)
	ta.SetVizStyle(VizScope)
	hold(ta, input.BtnSelect, muteHold)
	if !ta.muted || ta.VizStyle() != VizScope {
		t.Fatalf("muted %v, style %v", ta.muted, ta.VizStyle())
	}
}

func menuLabels(m *MenuScreen) []string {
	var out []string
	for _, e := range m.entries {
		out = append(out, e.label)
	}
	return out
}

func TestShortXOpensTheNowPlayingMenu(t *testing.T) {
	ta := npApp(t)
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok || m.title != "Now Playing" {
		t.Fatalf("X opened %T", ta.Top())
	}
	if got := menuLabels(m); !slices.Equal(got, []string{"Star", "Shuffle: Off", "Repeat: Off"}) {
		t.Fatalf("entries %q", got)
	}
	if len(ta.lib.stars) != 0 {
		t.Fatal("a short X starred")
	}
}

func TestNowPlayingMenuEntriesAct(t *testing.T) {
	ta := npApp(t)
	choose := func(i int) {
		t.Helper()
		ta.press(input.BtnX)
		for range i {
			ta.press(input.BtnDown)
		}
		ta.press(input.BtnA)
		ta.settle(t)
	}
	choose(0)
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.press(input.BtnX)
	if got := menuLabels(ta.Top().(*MenuScreen))[0]; got != "Unstar" {
		t.Fatalf("first entry %q, want Unstar", got)
	}
	ta.press(input.BtnB)
	choose(1)
	if !ta.pl.st.Shuffle {
		t.Fatal("Shuffle entry did not turn shuffle on")
	}
	ta.press(input.BtnX)
	if got := menuLabels(ta.Top().(*MenuScreen))[1]; got != "Shuffle: On" {
		t.Fatalf("second entry %q", got)
	}
	ta.press(input.BtnB)
	for _, want := range []player.Repeat{player.RepeatAll, player.RepeatOne, player.RepeatOff} {
		choose(2)
		if ta.pl.st.Repeat != want {
			t.Fatalf("repeat %v, want %v", ta.pl.st.Repeat, want)
		}
	}
	choose(1)
	if ta.pl.st.Shuffle {
		t.Fatal("Shuffle entry did not turn shuffle off")
	}
}

func TestHoldXStarsAtOnceWithoutTheMenu(t *testing.T) {
	ta := npApp(t)
	np := ta.Top()
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if ta.Top() != np {
		t.Fatalf("hold X left %T on top", ta.Top())
	}
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.now = ta.now.Add(3 * starHold)
	ta.onWake() // one hold stars once
	ta.settle(t)
	if len(ta.lib.stars) != 1 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	hold(ta, input.BtnX, starHold) // a second hold unstars
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestShortXTimerDoesNotStarLater(t *testing.T) {
	ta := npApp(t)
	ta.press(input.BtnX)
	ta.press(input.BtnB) // close the menu
	ta.now = ta.now.Add(2 * starHold)
	ta.onWake()
	ta.settle(t)
	if len(ta.lib.stars) != 0 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestXLostReleaseRecovers(t *testing.T) {
	ta := npApp(t)
	np := NewNowPlayingScreen()
	ta.Push(np)
	ta.onInput(input.Event{Button: input.BtnX, Kind: input.Press})
	ta.Push(NewQueueScreen())
	ta.now = ta.now.Add(2 * starHold)
	ta.onWake() // fires under the Queue: no star there
	ta.onInput(input.Event{Button: input.BtnX, Kind: input.Release})
	ta.Pop()
	ta.settle(t)
	if len(ta.lib.stars) != 0 {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if len(ta.lib.stars) != 1 {
		t.Fatalf("hold after a lost release: stars %v", ta.lib.stars)
	}
}

func TestQueueMenuHasShuffleAndRepeat(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewQueueScreen())
	ta.settle(t)
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if got := menuLabels(m); len(got) != 4 || got[2] != "Shuffle: Off" || got[3] != "Repeat: Off" {
		t.Fatalf("entries %q", got)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if !ta.pl.st.Shuffle {
		t.Fatal("the queue menu's Shuffle did nothing")
	}
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if ta.pl.st.Repeat != player.RepeatAll {
		t.Fatalf("repeat %v", ta.pl.st.Repeat)
	}
}

func TestSettingsVisualizerRowCyclesAndSaves(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Display", displaySettings))
	s := ta.Top().(*SettingsListScreen)
	for i, r := range s.rows(ta.App) {
		if r.label == "Visualizer" {
			s.list.Focus = i
		}
	}
	rows := s.rows(ta.App)
	if r := rows[s.list.Focus]; r.label != "Visualizer" || r.value(ta.App) != "Off" || r.help == "" {
		t.Fatalf("row %q = %q, help %q", r.label, r.value(ta.App), r.help)
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	if ta.VizStyle() != VizScope || ta.cfg.Display.Visualizer != "scope" {
		t.Fatalf("style %v, config %q", ta.VizStyle(), ta.cfg.Display.Visualizer)
	}
	if v := rows[s.list.Focus].value(ta.App); v != "Scope" {
		t.Fatalf("value %q", v)
	}
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft)
	ta.press(input.BtnLeft) // wraps from Off to Waterfall
	if ta.VizStyle() != VizWaterfall {
		t.Fatalf("style %v", ta.VizStyle())
	}
	ta.flushConfig()
	waitFile(t, ta.o.ConfigPath, `visualizer = "waterfall"`)
}

func TestNowPlayingHints(t *testing.T) {
	ta := npApp(t)
	ta.pad = true
	var got []string
	for _, h := range ta.screenHints() {
		l := h.Label
		if h.Hold {
			l = "hold " + l
		}
		got = append(got, padCap(h.Button)+" "+l)
	}
	want := []string{"A Pause", " Seek", " Volume", "L Prev/Next", "X Menu", "X hold Star", "Y Queue", "Select Visualizer", "Select hold Mute", "B Back"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("hints %q, want %q", got, want)
	}
	ta.pad = false // Select has no key, so the keyboard bar leaves its hints out
	if _, ok := ta.capFor(input.BtnSelect); ok {
		t.Fatal("Select has a key cap")
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
index 6bce238..79c6a22 100644
--- a/internal/config/config_test.go
+++ b/internal/config/config_test.go
@@ -207,3 +207,29 @@ func encodeForCompare(c *Config) string {
 	}
 	return b.String()
 }
+
+func TestVisualizerDefaultsToOffAndLoads(t *testing.T) {
+	cfg, warns, err := Load(write(t, minimal))
+	if err != nil || len(warns) != 0 || cfg.Display.Visualizer != "off" {
+		t.Fatalf("default: %q, %v, %v", cfg.Display.Visualizer, warns, err)
+	}
+	for _, v := range []string{"off", "bars", "scope", "vu", "waterfall"} {
+		cfg, warns, err := Load(write(t, minimal+"\n[display]\nvisualizer = \""+v+"\"\n"))
+		if err != nil || len(warns) != 0 || cfg.Display.Visualizer != v {
+			t.Errorf("%s: loaded %q, %v, %v", v, cfg.Display.Visualizer, warns, err)
+		}
+	}
+}
+
+func TestUnknownVisualizerLoadsAsOffWithAWarning(t *testing.T) {
+	cfg, warns, err := Load(write(t, minimal+"\n[display]\nvisualizer = \"disco\"\n"))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if cfg.Display.Visualizer != "off" {
+		t.Fatalf("visualizer = %q, want off", cfg.Display.Visualizer)
+	}
+	if len(warns) != 1 || !strings.Contains(warns[0], "display.visualizer") || !strings.Contains(warns[0], "disco") {
+		t.Fatalf("warnings = %v", warns)
+	}
+}
diff --git a/internal/config/edit_test.go b/internal/config/edit_test.go
index 45ed24a..4122134 100644
--- a/internal/config/edit_test.go
+++ b/internal/config/edit_test.go
@@ -261,3 +261,16 @@ func TestSaveChangedServerKeepsTheCommentAboveItsSubtable(t *testing.T) {
 	got, _ := saveEdited(t, src, func(c *Config) { c.Servers[0].Password = "other" })
 	wantAll(t, got, "\n\n# about the extras\n[server.extra]", `note = "mine"`, `password = "other"`)
 }
+
+func TestEditSetsTheVisualizerInPlace(t *testing.T) {
+	got, cfg := saveEdited(t, commented, func(c *Config) { c.Display.Visualizer = "scope" })
+	wantAll(t, got, "[display]\nhints = true\nvisualizer = \"scope\"", "# My own notes on this file.", "keep it quiet at night")
+	if cfg.Display.Visualizer != "scope" {
+		t.Fatal(cfg.Display.Visualizer)
+	}
+	again, _ := saveEdited(t, got, func(c *Config) { c.Display.Visualizer = "vu" })
+	wantAll(t, again, "visualizer = \"vu\"")
+	if strings.Count(again, "visualizer") != 1 {
+		t.Fatalf("visualizer written twice:\n%s", again)
+	}
+}
diff --git a/internal/ui/hints_test.go b/internal/ui/hints_test.go
index 4612ae6..a07e4f3 100644
--- a/internal/ui/hints_test.go
+++ b/internal/ui/hints_test.go
@@ -6,6 +6,7 @@ import (
 	"slices"
 	"strings"
 	"testing"
+	"time"
 
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
@@ -135,6 +136,8 @@ func TestEveryHintedButtonDoesSomething(t *testing.T) {
 					depth, calls, top := len(ta.stack), len(ta.pl.calls), ta.Top()
 					if h.Rune != 0 { // a typing key: Backspace, Enter
 						ta.pressKey(h.Rune)
+					} else if h.Hold {
+						hold(ta, b, 2*time.Second)
 					} else {
 						ta.press(b)
 					}
diff --git a/internal/ui/mute_test.go b/internal/ui/mute_test.go
index dc0bb7a..713c408 100644
--- a/internal/ui/mute_test.go
+++ b/internal/ui/mute_test.go
@@ -1,7 +1,6 @@
 package ui
 
 import (
-	"mistersubsonic/internal/player"
 	"testing"
 	"time"
 
@@ -57,8 +56,8 @@ func TestTypingMDoesNotMute(t *testing.T) {
 	}
 }
 
-// Holding Select on Now Playing mutes; a short press cycles the play mode.
-func TestSelectHoldMutesAndShortPressCyclesMode(t *testing.T) {
+// Holding Select on Now Playing mutes; a short press changes the visualizer.
+func TestSelectHoldMutesAndShortPressChangesTheVisualizer(t *testing.T) {
 	ta, _ := connectedApp(t)
 	playingState(ta)
 	ta.Push(NewNowPlayingScreen())
@@ -67,8 +66,8 @@ func TestSelectHoldMutesAndShortPressCyclesMode(t *testing.T) {
 	ta.now = ta.now.Add(muteHold - time.Millisecond)
 	ta.onWake()
 	sel(input.Release)
-	if ta.muted || !ta.pl.st.Shuffle {
-		t.Fatalf("short press: muted %v, shuffle %v", ta.muted, ta.pl.st.Shuffle)
+	if ta.muted || ta.VizStyle() != VizBars {
+		t.Fatalf("short press: muted %v, style %v", ta.muted, ta.VizStyle())
 	}
 	ta.now = ta.now.Add(2 * muteHold)
 	ta.onWake() // the short press's timer must not fire later
@@ -88,8 +87,8 @@ func TestSelectHoldMutesAndShortPressCyclesMode(t *testing.T) {
 		t.Fatalf("hold: muted %v/%v, panel %v", ta.muted, ta.pl.st.Muted, !ta.volumeUntil.IsZero())
 	}
 	sel(input.Release)
-	if !ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
-		t.Fatalf("the release after a hold cycled the mode: shuffle %v repeat %v", ta.pl.st.Shuffle, ta.pl.st.Repeat)
+	if ta.VizStyle() != VizBars {
+		t.Fatalf("the release after a hold changed the style to %v", ta.VizStyle())
 	}
 	hold()
 	sel(input.Release)
@@ -111,25 +110,6 @@ func TestSelectHoldIsCancelledByLeavingTheScreen(t *testing.T) {
 	}
 }
 
-// A short Select tap changes the mode label, which the player announces
-// with no event: the release must redraw the frame.
-func TestSelectTapRedrawsTheModeLabel(t *testing.T) {
-	ta, _ := connectedApp(t)
-	playingState(ta)
-	ta.pl.st.Repeat = player.RepeatAll
-	ta.Push(NewNowPlayingScreen())
-	ta.settle(t)
-	ta.clean()
-	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press})
-	ta.settle(t)
-	ta.clean() // only the release can mark the frame dirty now
-	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Release})
-	if ta.pl.st.Repeat != player.RepeatOne || !ta.dirty {
-		t.Fatalf("repeat %v, dirty %v", ta.pl.st.Repeat, ta.dirty)
-	}
-	ta.settle(t) // the verify mode compares with a full frame
-}
-
 // A Select release that went to another screen must not leave Now Playing
 // stuck: the next press starts a fresh hold.
 func TestSelectLostReleaseRecovers(t *testing.T) {
@@ -153,7 +133,7 @@ func TestSelectLostReleaseRecovers(t *testing.T) {
 	}
 	sel(input.Press)
 	sel(input.Release)
-	if !ta.pl.st.Shuffle {
-		t.Fatal("tap after that didn't cycle the mode")
+	if ta.VizStyle() != VizBars {
+		t.Fatal("tap after that didn't change the visualizer")
 	}
 }
diff --git a/internal/ui/play_test.go b/internal/ui/play_test.go
index e62368a..cffc877 100644
--- a/internal/ui/play_test.go
+++ b/internal/ui/play_test.go
@@ -26,16 +26,16 @@ func TestNowPlayingVolumeKeys(t *testing.T) {
 	}
 }
 
-func TestNowPlayingXStarsTheSong(t *testing.T) {
+func TestNowPlayingHoldXStarsTheSong(t *testing.T) {
 	ta := newTestApp(t, ProfileHDMI)
 	playingState(ta)
 	ta.Push(NewNowPlayingScreen())
-	ta.press(input.BtnX)
+	hold(ta, input.BtnX, starHold)
 	ta.settle(t)
 	if !slices.Equal(ta.lib.stars, []string{"star s1"}) || !ta.isStarred(songStar(ta.pl.st.Queue[0])) {
 		t.Fatalf("stars %v", ta.lib.stars)
 	}
-	ta.press(input.BtnX)
+	hold(ta, input.BtnX, starHold)
 	ta.settle(t)
 	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) {
 		t.Fatalf("stars %v", ta.lib.stars)
@@ -56,7 +56,7 @@ func TestQueueMenuRemoveAndClear(t *testing.T) {
 		t.Fatalf("X opened %T, want the menu", ta.Top())
 	}
 	menu := ta.Top().(*MenuScreen)
-	if menu.title != "Queue" || menu.entries[0].label != "Remove Время Луны" || menu.entries[1].label != "Clear queue" {
+	if menu.title != "Queue" || menu.entries[0].label != "Remove Время Луны" || menu.entries[1].label != "Clear queue" || len(menu.entries) != 4 {
 		t.Fatalf("menu %q with entries %q, %q", menu.title, menu.entries[0].label, menu.entries[1].label)
 	}
 	ta.press(input.BtnA) // Remove
diff --git a/internal/ui/screensaver_test.go b/internal/ui/screensaver_test.go
index d9b665d..d637cca 100644
--- a/internal/ui/screensaver_test.go
+++ b/internal/ui/screensaver_test.go
@@ -7,7 +7,6 @@ import (
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
-	"mistersubsonic/internal/player"
 )
 
 func saverApp(t *testing.T, p Profile, minutes int) *testApp {
@@ -129,12 +128,12 @@ func TestScreensaverWakeSwallowsTheReleaseToo(t *testing.T) {
 	ta.onWake()
 	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press}) // wakes
 	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Release})
-	if len(ta.toasts) != 0 || ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
-		t.Fatalf("the wake press's release cycled the mode: toasts %v, %+v", ta.toasts, ta.pl.st)
+	if len(ta.toasts) != 0 || ta.VizStyle() != VizOff {
+		t.Fatalf("the wake press's release changed the visualizer: toasts %v, %v", ta.toasts, ta.VizStyle())
 	}
-	ta.press(input.BtnSelect) // a real tap still cycles
-	if !ta.pl.st.Shuffle {
-		t.Fatal("the next tap didn't cycle the mode")
+	ta.press(input.BtnSelect) // a real tap still changes it
+	if ta.VizStyle() != VizBars {
+		t.Fatal("the next tap didn't change the visualizer")
 	}
 }
 
diff --git a/internal/ui/ui_test.go b/internal/ui/ui_test.go
index 0aed2dc..d494872 100644
--- a/internal/ui/ui_test.go
+++ b/internal/ui/ui_test.go
@@ -174,12 +174,8 @@ func TestNowPlayingControls(t *testing.T) {
 		}
 	}
 	ta.press(input.BtnSelect)
-	if !ta.pl.st.Shuffle {
-		t.Fatal("Select should switch to shuffle")
-	}
-	ta.press(input.BtnSelect)
-	if ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatAll {
-		t.Fatalf("second Select: shuffle %v repeat %v", ta.pl.st.Shuffle, ta.pl.st.Repeat)
+	if ta.VizStyle() != VizBars {
+		t.Fatalf("Select should switch to Bars, got %v", ta.VizStyle())
 	}
 	ta.press(input.BtnY)
 	if _, ok := ta.Top().(*QueueScreen); !ok {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/config`

Expected: FAIL, e.g.:

```
cfg.Display.Visualizer undefined (type Display has no field or method Visualizer)
c.Display.Visualizer undefined (type Display has no field or method Visualizer)
h.Hold undefined (type Hint has no field or method Hold)
ta.VizStyle undefined (type *testApp has no field or method VizStyle)
undefined: VizBars
undefined: starHold
```

- [ ] **Step 3: Implement**

`internal/ui/viz.go` (new file):

```go
package ui

import "mistersubsonic/internal/config"

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
```

Then Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index 0c0dda1..028f61e 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -54,6 +54,7 @@ type Display struct {
 	ScreensaverMinutes int    `toml:"screensaver_minutes"`
 	FullResolution     bool   `toml:"full_resolution"`
 	Hints              bool   `toml:"hints"`
+	Visualizer         string `toml:"visualizer"` // off, bars, scope, vu or waterfall
 }
 
 type Cache struct {
@@ -64,7 +65,7 @@ type Cache struct {
 func Default() *Config {
 	return &Config{
 		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
-		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true, Hints: true},
+		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true, Hints: true, Visualizer: "off"},
 		Cache:    Cache{CoverArtMB: 200},
 	}
 }
@@ -90,6 +91,12 @@ func Load(path string) (cfg *Config, warnings []string, err error) {
 	for _, k := range md.Undecoded() {
 		warnings = append(warnings, "unknown key "+k.String())
 	}
+	switch cfg.Display.Visualizer {
+	case "off", "bars", "scope", "vu", "waterfall":
+	default: // a typo must not stop the app: show nothing and say so
+		warnings = append(warnings, fmt.Sprintf("display.visualizer: unknown value %q, using off", cfg.Display.Visualizer))
+		cfg.Display.Visualizer = "off"
+	}
 	if err := cfg.Validate(); err != nil {
 		return nil, warnings, fmt.Errorf("config: %s: %w", path, err)
 	}
diff --git a/internal/config/edit.go b/internal/config/edit.go
index 5693392..7604449 100644
--- a/internal/config/edit.go
+++ b/internal/config/edit.go
@@ -81,6 +81,7 @@ func scalars(c *Config) []scalar {
 		{"display", "screensaver_minutes", c.Display.ScreensaverMinutes},
 		{"display", "full_resolution", c.Display.FullResolution},
 		{"display", "hints", c.Display.Hints},
+		{"display", "visualizer", c.Display.Visualizer},
 		{"cache", "cover_art_mb", c.Cache.CoverArtMB},
 	}
 }
diff --git a/internal/ui/hints.go b/internal/ui/hints.go
index 035845c..27f253b 100644
--- a/internal/ui/hints.go
+++ b/internal/ui/hints.go
@@ -15,12 +15,14 @@ import (
 // Hint is one entry: a button (and a second one for pairs such as L/R or
 // Left/Right) and what it does on this screen. A hint for a typing key
 // (Backspace, Enter) has Key, the cap drawn on a keyboard, and Rune, what
-// that key types; only a keyboard shows it.
+// that key types; only a keyboard shows it. A Hold hint is for holding the
+// button, drawn "hold X Star".
 type Hint struct {
 	Button, Pair input.Button
 	Label        string
 	Key          string
 	Rune         rune
+	Hold         bool
 }
 
 // Hinter is a screen with its own hints, most important first. The app adds
@@ -42,6 +44,7 @@ type textTaker interface {
 }
 
 func hk(b input.Button, label string) Hint        { return Hint{Button: b, Label: label} }
+func hkHold(b input.Button, label string) Hint    { return Hint{Button: b, Label: label, Hold: true} }
 func hkPair(b, p input.Button, label string) Hint { return Hint{Button: b, Pair: p, Label: label} }
 
 // commonHints are for screens without their own list.
@@ -207,10 +210,18 @@ func (a *App) drawHints(c *gfx.Canvas) {
 			}
 			cw += (len(caps) - 1) * pad
 		}
-		need := cw + pad + f.Measure(h.Label)
+		holdW := 0
+		if h.Hold {
+			holdW = f.Measure("hold") + pad
+		}
+		need := holdW + cw + pad + f.Measure(h.Label)
 		if x+need > end {
 			break
 		}
+		if h.Hold {
+			f.Draw(c, x, base, "hold", colDim, c.Bounds())
+			x += holdW
+		}
 		if arrows {
 			chip := gfx.R(x, chipY, cw, chipH)
 			fillRoundRect(c, chip, pad, colArtBg)
@@ -279,7 +290,7 @@ func hintLabels(hs []Hint) string {
 func hintKey(hs []Hint) string {
 	var b strings.Builder
 	for _, h := range hs {
-		fmt.Fprintf(&b, "%d/%d/%s/%s;", h.Button, h.Pair, h.Label, h.Key)
+		fmt.Fprintf(&b, "%d/%d/%s/%s/%v;", h.Button, h.Pair, h.Label, h.Key, h.Hold)
 	}
 	return b.String()
 }
diff --git a/internal/ui/hints_screens.go b/internal/ui/hints_screens.go
index c5ed7c0..ff739b7 100644
--- a/internal/ui/hints_screens.go
+++ b/internal/ui/hints_screens.go
@@ -82,7 +82,8 @@ func (s *NowPlayingScreen) Hints(a *App) []Hint {
 	}
 	return []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
 		hkPair(input.BtnUp, input.BtnDown, "Volume"), hkPair(input.BtnL, input.BtnR, "Prev/Next"),
-		hk(input.BtnX, star), hk(input.BtnY, "Queue"), hk(input.BtnSelect, "Mode · hold: Mute")}
+		menuHint, hkHold(input.BtnX, star), hk(input.BtnY, "Queue"),
+		hk(input.BtnSelect, "Visualizer"), hkHold(input.BtnSelect, "Mute")}
 }
 
 func (s *QueueScreen) Hints(a *App) []Hint {
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 4660f7d..f66368a 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -21,8 +21,10 @@ const (
 	// the player's position catches up.
 	seekShow = time.Second
 	volStep  = 1.0 // dB per Up/Down
-	// Select held this long mutes (a shorter press cycles the play mode).
+	// Select held this long mutes (a shorter press changes the visualizer).
 	muteHold = time.Second
+	// X held this long stars or unstars the song (a shorter press opens the menu).
+	starHold = time.Second
 )
 
 // NowPlayingScreen is the full-screen player (spec §8.2).
@@ -35,6 +37,10 @@ type NowPlayingScreen struct {
 	selDown  bool // Select is held
 	selMuted bool // ...and has already muted: its release does nothing
 	selHold  int  // counts holds, so an old timer can't fire in a newer one
+
+	xDown    bool // X is held
+	xStarred bool // ...and has already starred: its release does nothing
+	xHold    int  // like selHold
 }
 
 func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }
@@ -42,27 +48,24 @@ func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }
 func (s *NowPlayingScreen) Title() string { return "Now Playing" }
 func (s *NowPlayingScreen) Enter(a *App)  {}
 
-// playModes is the Select cycle: (shuffle, repeat).
-var playModes = []struct {
-	shuffle bool
-	repeat  player.Repeat
-	label   string
-}{
-	{false, player.RepeatOff, "In order"},
-	{true, player.RepeatOff, "Shuffle"},
-	{false, player.RepeatAll, "Repeat all"},
-	{false, player.RepeatOne, "Repeat one"},
-}
-
 // Release flushes a seek target the throttle held back, and ends a Select
-// press: a short one cycles the play mode.
+// or X press: a short Select changes the visualizer, a short X opens the menu.
 func (s *NowPlayingScreen) Release(a *App, b input.Button) {
 	if b == input.BtnSelect && s.selDown {
 		muted := s.selMuted
 		s.selDown, s.selMuted = false, false
 		if !muted {
-			s.cycleMode(a)
-			a.dirty = true // the mode label may change without a player event
+			next := (a.VizStyle() + 1) % VizStyle(len(vizNames))
+			a.SetVizStyle(next)
+			a.Toast("Visualizer: %s", next.Label())
+		}
+		return
+	}
+	if b == input.BtnX && s.xDown {
+		starred := s.xStarred
+		s.xDown, s.xStarred = false, false
+		if !starred {
+			a.Push(NewMenuScreen(s, "Now Playing", append([]menuEntry{s.starEntry(a)}, modeEntries(a)...)))
 		}
 		return
 	}
@@ -82,20 +85,44 @@ func (s *NowPlayingScreen) settle(a *App, st player.State) {
 	s.unsent, s.song = false, ""
 }
 
-// cycleMode steps through the shuffle/repeat modes.
-func (s *NowPlayingScreen) cycleMode(a *App) {
-	pl := a.Player()
-	st := pl.State()
-	cur := 0
-	for i, m := range playModes {
-		if m.shuffle == st.Shuffle && m.repeat == st.Repeat {
-			cur = i
-		}
+// starEntry is the menu's Star or Unstar for the current song.
+func (s *NowPlayingScreen) starEntry(a *App) menuEntry {
+	song, ok := a.state().Current()
+	if !ok {
+		return menuEntry{"Star", func(a *App) {}}
+	}
+	label := "Star"
+	if a.isStarred(songStar(song)) {
+		label = "Unstar"
+	}
+	return menuEntry{label, func(a *App) { a.toggleStar(songStar(song)) }}
+}
+
+// modeEntries are the Shuffle and Repeat menu entries (Now Playing's and the
+// queue's), labelled with the current state.
+func modeEntries(a *App) []menuEntry {
+	st := a.state()
+	shuffle, repeat := "Shuffle: Off", "Repeat: Off"
+	next := player.RepeatAll
+	if st.Shuffle {
+		shuffle = "Shuffle: On"
+	}
+	switch st.Repeat {
+	case player.RepeatAll:
+		repeat, next = "Repeat: All", player.RepeatOne
+	case player.RepeatOne:
+		repeat, next = "Repeat: One", player.RepeatOff
+	}
+	return []menuEntry{
+		{shuffle, func(a *App) {
+			a.Player().SetShuffle(!st.Shuffle)
+			a.dirty = true // the mode label changes without a player event
+		}},
+		{repeat, func(a *App) {
+			a.Player().SetRepeat(next)
+			a.dirty = true
+		}},
 	}
-	m := playModes[(cur+1)%len(playModes)]
-	pl.SetShuffle(m.shuffle)
-	pl.SetRepeat(m.repeat)
-	a.Toast("%s", m.label)
 }
 
 func (s *NowPlayingScreen) curID(st player.State) subsonic.ID {
@@ -167,17 +194,25 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 	switch e.Button {
 	case input.BtnA:
 		pl.TogglePause()
-	case input.BtnX:
-		if song, ok := st.Current(); ok {
-			a.toggleStar(songStar(song))
-		}
+	case input.BtnX: // every press starts a fresh hold (a lost release is recovered): short opens the menu, long stars
+		s.xDown, s.xStarred = true, false
+		s.xHold++
+		hold := s.xHold
+		a.After(s, starHold, func() {
+			if s.xDown && s.xHold == hold && a.Top() == Screen(s) {
+				s.xStarred = true
+				if song, ok := a.state().Current(); ok {
+					a.toggleStar(songStar(song))
+				}
+			}
+		})
 	case input.BtnL:
 		pl.Prev()
 	case input.BtnR:
 		pl.Next()
 	case input.BtnY:
 		a.Push(NewQueueScreen())
-	case input.BtnSelect: // every press starts a fresh hold (a lost release is recovered): short cycles the mode, long mutes
+	case input.BtnSelect: // every press starts a fresh hold (a lost release is recovered): short changes the visualizer, long mutes
 		s.selDown, s.selMuted = true, false
 		s.selHold++
 		hold := s.selHold
@@ -254,10 +289,14 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	y += fs.Height() + p.Margin/2
 
 	mode := statusLabel(st.Status)
-	for _, m := range playModes {
-		if m.shuffle == st.Shuffle && m.repeat == st.Repeat && m.label != "In order" {
-			mode += "  ·  " + m.label
-		}
+	if st.Shuffle {
+		mode += "  ·  Shuffle"
+	}
+	switch st.Repeat {
+	case player.RepeatAll:
+		mode += "  ·  Repeat all"
+	case player.RepeatOne:
+		mode += "  ·  Repeat one"
 	}
 	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
 	y += fb.Height()
@@ -312,7 +351,7 @@ func (s *QueueScreen) Handle(a *App, e input.Event) bool {
 	case input.BtnX:
 		i := s.list.Focus
 		title := st.Queue[i].Title
-		a.Push(NewMenuScreen(s, "Queue", []menuEntry{
+		a.Push(NewMenuScreen(s, "Queue", append([]menuEntry{
 			{"Remove " + title, func(a *App) {
 				a.Player().Remove(i)
 				a.Toast("Removed %s", title)
@@ -324,7 +363,7 @@ func (s *QueueScreen) Handle(a *App, e input.Event) bool {
 				}
 				a.Toast("Queue cleared")
 			}},
-		}))
+		}, modeEntries(a)...)))
 		return true
 	}
 	return false
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index ce56476..fa1f031 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -315,6 +315,11 @@ func displaySettings(a *App) []setting {
 				a.UpdateConfig(func(c *config.Config) { c.Display.Hints = on }, false)
 			},
 			help: "The bar of buttons along the bottom of every screen.", toggle: true},
+		{label: "Visualizer", value: func(a *App) string { return a.VizStyle().Label() },
+			change: func(a *App, dir int) {
+				a.SetVizStyle(VizStyle((int(a.VizStyle()) + dir + len(vizNames)) % len(vizNames)))
+			},
+			help: "A moving picture of the music on Now Playing. Select there changes it too.", toggle: false},
 	}
 }
 
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index 3e70d2a..9c1138d 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -36,6 +36,7 @@ profile = "auto"           # auto, hdmi or crt (set crt for a 480i/576i CRT)
 screensaver_minutes = 5    # minutes idle on Now Playing; 0 turns it off
 full_resolution = true     # HDMI above 1080p (e.g. 1920x1200): draw at the output's size, not half
 hints = true               # the bar of buttons (or keys) along the bottom of every screen
+visualizer = "off"         # moving picture on Now Playing: off, bars, scope, vu or waterfall (Select there changes it)
 
 [cache]
 cover_art_mb = 200         # cover art kept on the SD card, per server
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./internal/config`

Expected: `ok`. Golden screenshots written or changed: `nowplaying-hdmi-960x600`, `nowplaying-hdmi`, `nowplaying-starred-hdmi`. Open each one and check it: only the hint bar changes. On HDMI it reads Pause, Back, Seek, Volume, Prev/Next, `Tab` Menu, "hold `Tab` Star", and Queue; the keyboard set has no Select hints.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/edit.go internal/config/edit_test.go internal/ui/hints.go internal/ui/hints_screens.go internal/ui/hints_test.go internal/ui/mute_test.go internal/ui/play_test.go internal/ui/screens_play.go internal/ui/screens_settings.go internal/ui/screensaver_test.go internal/ui/ui_test.go internal/ui/viz.go internal/ui/vizcontrols_test.go sdcard/mistersubsonic/config.example.toml internal/ui/testdata/golden
git commit -m "ui, config: Select picks the visualizer style, X opens a menu and hold-X stars, shuffle and repeat in both menus" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Drawing, the Now Playing panel, the frame clock, the safety valve

**Files:**
- Modify: `internal/ui/viz.go`, `internal/ui/app.go`, `internal/ui/damage.go`, `internal/ui/screens_play.go`
- Test: `internal/ui/vizdraw_test.go` (new)

**Interfaces:**
- **Consumes:**
  - `viz.New`, `viz.HDMI()`, `viz.CRT()`, the `Analyzer` readers and `viz.Palette` (Task 1);
  - `ui.Visual` (Task 2);
  - `VizStyle` and `(*App).VizStyle()` (Task 3).
- **Produces:**
  - **State.** `vizState` on App holds:
    - the analyzer, per layout (`isCRT`);
    - the window buffer;
    - the scope buffer;
    - the waterfall image and its cursor;
    - the fps level;
    - the valve's timing.
  - **Drawing.** `(*App).drawViz(c, r, style)`, plus `drawBars`, `drawScope`, `drawVU` and `drawWaterfall`. Each one draws into r and changes no state.
  - **The frame clock.**
    - `vizFPS()`, `vizInterval()`, `vizDue()` and `vizTick(now)` are joined to `untilWake` and `onWake`. `markViz(r)` records the visualizer's rect each frame.
    - The rate is 30 fps on HDMI (`vizFPSHDMI`) and 20 on CRT (`vizFPSCRT`).
    - It runs while a visualizer is shown and the player is Playing, or until the analyzer is Idle.
  - **The safety valve.** `vizCost(d)`:
    - over 60% of the budget for 1 s, it steps down a level and logs "visualizer: slowing to N fps";
    - under 30% of the faster level's budget for 10 s, it steps back up.
  - **The panel.** `drawVizPanel` places it in Now Playing's right column: below the last text line, down to the cover's bottom edge, as wide as the progress bar. That is 1050×153 at 1920×1200. On CRT it is 34 px high, and the text block re-centres.
  - **The benchmark.** `BenchmarkVizFrame`, with `hdmi-1920x1200/<style>` and `crt-320x240/<style>`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/vizdraw_test.go` (new file):

```go
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
		if ta.dirty || len(ta.damage) == 0 {
			t.Fatalf("%s: wake left dirty=%v damage=%v", c.prof.Name, ta.dirty, ta.damage)
		}
		for _, r := range ta.damage { // the panel, and the progress regions every wake already brings
			if r != ta.viz.rect && !containsRect(ta.ticks, r) {
				t.Errorf("%s: wake damaged %v, which is neither the panel %v nor progress %v", c.prof.Name, r, ta.viz.rect, ta.ticks)
			}
		}
		if !containsRect(ta.damage, ta.viz.rect) {
			t.Errorf("%s: panel not damaged: %v", c.prof.Name, ta.damage)
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

func TestVizFrameAllocatesNothing(t *testing.T) {
	for _, l := range vizLayouts {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := vizApp(t, l.prof, style)
			runFrames(t, ta, 3) // setup: the analyzer, the buffers, the image
			c, r := ta.canvas, ta.viz.rect
			if n := testing.AllocsPerRun(20, func() {
				ta.now = ta.now.Add(40 * time.Millisecond)
				ta.damage = ta.damage[:0] // the app clears it with every frame
				ta.vizTick(ta.now)
				ta.drawViz(c, r, style)
			}); n != 0 {
				t.Errorf("%s %v: %v allocations per frame", l.name, style, n)
			}
		}
	}
}

// BenchmarkVizFrame is one visualizer frame: the analysis and the drawing
// into the panel, for each style on a big HDMI panel and a CRT strip.
// On the device: ./ui.test -test.run '^$' -test.bench VizFrame
func BenchmarkVizFrame(b *testing.B) {
	for _, l := range []struct {
		name string
		prof Profile
	}{{"hdmi-1920x1200", PickProfile(1920, 1200, "auto")}, {"crt-320x240", ProfileCRT240}} {
		for style := VizBars; style <= VizWaterfall; style++ {
			b.Run(l.name+"/"+style.String(), func(b *testing.B) {
				t := &testing.T{}
				ta := vizApp(t, l.prof, style)
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/viz`

Expected: FAIL, e.g.:

```
ta.viz undefined (type *testApp has no field or method viz)
vizApp(t, ProfileCRT240, VizBars).viz undefined (type *testApp has no field or method viz)
undefined: vizFPSHDMI
undefined: vizFPSCRT
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 15c77ef..736f122 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -244,6 +244,7 @@ type App struct {
 	verifyCanvas *gfx.Canvas
 	checkAt      time.Time // the next watchdog check (zero: the display can't check itself)
 	overwritten  bool      // the last check found the screen drawn over
+	viz          vizState
 }
 
 func New(o Options) (*App, error) {
@@ -519,6 +520,7 @@ func (a *App) untilWake() time.Duration {
 	if !a.volumeUntil.IsZero() {
 		consider(a.volumeUntil)
 	}
+	consider(a.vizDue())
 	consider(a.saverDue())
 	if a.saver {
 		consider(now.Add(saverStep)) // the drift; nothing else moves
@@ -578,6 +580,9 @@ func (a *App) onWake() {
 		a.saveConfig()
 	}
 	a.checkScreen(now)
+	if a.vizActive() && !now.Before(a.viz.next) {
+		a.vizTick(now)
+	}
 	if !a.volumeUntil.IsZero() && !now.Before(a.volumeUntil) {
 		a.Damage(a.volumePanelRect()) // the panel goes
 		a.volumeUntil = time.Time{}
@@ -762,6 +767,10 @@ func (a *App) present(c *gfx.Canvas) error {
 // changed, otherwise the whole frame.
 func (a *App) render() error {
 	a.frameNow = a.o.Now()
+	if a.viz.pending { // the frame's cost: its analysis, and this draw and present
+		start := a.frameNow
+		defer func() { a.vizCost(a.viz.work + a.o.Now().Sub(start)) }()
+	}
 	if a.o.Player != nil {
 		st := a.o.Player.State()
 		a.frameState = &st
diff --git a/internal/ui/damage.go b/internal/ui/damage.go
index 172aac6..76394c7 100644
--- a/internal/ui/damage.go
+++ b/internal/ui/damage.go
@@ -177,7 +177,7 @@ func (a *App) markArt(id subsonic.ID, r gfx.Rect) {
 }
 
 func (a *App) resetMarks() {
-	a.ticks, a.mqRect = a.ticks[:0], gfx.Rect{}
+	a.ticks, a.mqRect, a.viz.rect = a.ticks[:0], gfx.Rect{}, gfx.Rect{}
 	for id := range a.arts {
 		delete(a.arts, id)
 	}
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index f66368a..a7a0a92 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -240,6 +240,12 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	barH := max(p.Margin/6, 3)
 	// Height of the text block, used to centre everything vertically.
 	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + 2*fs.Height()
+	// With the visualizer on, the panel is part of the block when the cover
+	// is too short to hold it beside the text (a CRT).
+	vizGapY := p.Margin / 4
+	if a.vizShown() {
+		textH += vizGapY + max(p.Margin*5/4, 20)
+	}
 	// Art left, text right, centred as a block. The gap is narrower on a
 	// CRT, where the text needs the width.
 	gap := 2 * p.Margin
@@ -307,6 +313,9 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	if a.insecure {
 		line(fs, "insecure: certificate not checked", colError)
 	}
+	// The visualizer takes the rest of the column, down to the cover's bottom
+	// edge (or the text block's, when that is lower).
+	a.drawVizPanel(c, gfx.R(text.X, y+vizGapY, text.W, text.Bottom()-y-vizGapY))
 }
 
 // QueueScreen lists the play queue.
diff --git a/internal/ui/viz.go b/internal/ui/viz.go
index b53e988..bfe96cb 100644
--- a/internal/ui/viz.go
+++ b/internal/ui/viz.go
@@ -1,6 +1,15 @@
 package ui
 
-import "mistersubsonic/internal/config"
+import (
+	"log"
+	"math"
+	"time"
+
+	"mistersubsonic/internal/config"
+	"mistersubsonic/internal/gfx"
+	"mistersubsonic/internal/player"
+	"mistersubsonic/internal/viz"
+)
 
 // VizStyle is the visualizer's look (spec §3.2); VizOff draws nothing.
 type VizStyle int
@@ -45,3 +54,358 @@ func (a *App) SetVizStyle(v VizStyle) {
 	a.UpdateConfig(func(c *config.Config) { c.Display.Visualizer = v.String() }, false)
 	a.dirty = true
 }
+
+// The frame clock and the safety valve (spec §5). While a visualizer is
+// visible and the sound plays, the app wakes at the target rate; each wake
+// analyses the window and damages the panel. The valve steps the rate down
+// when frames cost too much (the MiSTer's CPU is not measured yet), and up
+// again after a calm spell.
+const (
+	vizFPSHDMI = 30
+	vizFPSCRT  = 20
+
+	vizMeasure = time.Second      // the span a frame's average cost is taken over
+	vizSlow    = 0.6              // step down above this share of the frame budget
+	vizCalm    = 0.3              // ...step up when under this share of the faster budget
+	vizCalmFor = 10 * time.Second // ...for this long
+	vizGap     = 500 * time.Millisecond
+	vizSweep   = 6 // seconds the waterfall's cursor takes across the panel
+)
+
+var (
+	vizRatesHDMI = []int{vizFPSHDMI, 15, 10}
+	vizRatesCRT  = []int{vizFPSCRT, 10}
+)
+
+// vizState is what the visualizer keeps between frames.
+type vizState struct {
+	an     *viz.Analyzer
+	rates  []int // the frame rates, fastest first
+	level  int   // index into rates
+	win    []float32
+	scope  []float32 // the scope's points, one per panel column
+	img    *gfx.Image
+	cursor int      // the waterfall's next column
+	style  VizStyle // what the picture in img is of
+	rect   gfx.Rect // where the panel was drawn (empty: not on screen)
+
+	last, next time.Time // the last analysis, the next due (zero: now)
+
+	work    time.Duration // the analysis of the frame awaiting its draw
+	pending bool
+	sum     time.Duration // frame costs since since
+	n       int
+	since   time.Time
+	calm    time.Time // when the cheap spell began (zero: not in one)
+}
+
+func (a *App) isCRT() bool { return a.P.Name == "crt" }
+
+// vizFPS is the frame rate now.
+func (a *App) vizFPS() int {
+	if a.viz.rates == nil {
+		return vizFPSHDMI
+	}
+	return a.viz.rates[a.viz.level]
+}
+
+func (a *App) vizInterval() time.Duration { return time.Second / time.Duration(a.vizFPS()) }
+
+// markViz records where the visualizer is drawn, like markTick: the frame
+// clock damages it.
+func (a *App) markViz(r gfx.Rect) { a.viz.rect = r }
+
+// vizActive reports whether the frame clock runs: a visualizer is on screen
+// and the sound plays, or the levels are still falling.
+func (a *App) vizActive() bool {
+	v := &a.viz
+	if !a.vizShown() || v.rect.Empty() || a.saver {
+		return false
+	}
+	return a.state().Status == player.Playing || v.an != nil && !v.an.Idle()
+}
+
+// vizDue is when the next frame is due (zero: no frame clock).
+func (a *App) vizDue() time.Time {
+	if !a.vizActive() {
+		return time.Time{}
+	}
+	if a.viz.next.IsZero() {
+		return a.o.Now()
+	}
+	return a.viz.next
+}
+
+func (a *App) vizInit() {
+	cfg, rates := viz.HDMI(), vizRatesHDMI
+	if a.isCRT() {
+		cfg, rates = viz.CRT(), vizRatesCRT
+	}
+	a.viz.an, a.viz.rates = viz.New(cfg), rates
+	a.viz.win = make([]float32, 2*cfg.FFTSize)
+}
+
+// vizTick is one frame's analysis, run from the wake: the window into the
+// analyzer, the waterfall's new column, and the panel damaged.
+func (a *App) vizTick(now time.Time) {
+	v := &a.viz
+	start := a.o.Now()
+	if v.an == nil {
+		a.vizInit()
+	}
+	dt := a.vizInterval()
+	if d := now.Sub(v.last); !v.last.IsZero() && d < vizGap {
+		dt = d
+	} else { // the clock was stopped: the cost average starts again
+		v.sum, v.n, v.since, v.calm = 0, 0, time.Time{}, time.Time{}
+	}
+	n := 0
+	if a.state().Status == player.Playing { // a paused device stops consuming: let the levels fall
+		n = a.o.Visual.Window(v.win)
+	}
+	v.an.Update(v.win, n, dt)
+	switch style := a.VizStyle(); style {
+	case VizScope:
+		if len(v.scope) != v.rect.W {
+			v.scope = make([]float32, v.rect.W)
+		}
+		v.an.Scope(v.scope)
+	case VizWaterfall:
+		a.sweepWaterfall()
+	}
+	v.last, v.next = now, now.Add(a.vizInterval())
+	a.Damage(v.rect)
+	v.work, v.pending = a.o.Now().Sub(start), true
+}
+
+// sweepWaterfall paints this frame's spectrum column at the cursor and moves
+// the cursor on; the picture is never scrolled.
+func (a *App) sweepWaterfall() {
+	v := &a.viz
+	r := v.rect
+	if v.img == nil || v.img.W != r.W || v.img.H != r.H {
+		v.img, v.cursor = gfx.NewImage(r.W, r.H), 0
+	} else if v.style != VizWaterfall {
+		clear(v.img.Pix)
+		v.cursor = 0
+	}
+	v.style = VizWaterfall
+	col := v.an.Column()
+	cw := max(int(math.Round(float64(r.W)/float64(vizSweep*a.vizFPS()))), 1)
+	for dx := 0; dx < cw; dx++ {
+		x := (v.cursor + dx) % r.W
+		for y := 0; y < r.H; y++ {
+			b := min((r.H-1-y)*len(col)/r.H, len(col)-1) // low frequencies at the bottom
+			i := int(min(max(col[b], 0), 1)*255 + 0.5)
+			v.img.Pix[y*r.W+x] = 0xFF000000 | viz.Palette[i]
+		}
+	}
+	v.cursor = (v.cursor + cw) % r.W
+}
+
+// vizCost takes in what a visualizer frame cost (analysis, draw, present)
+// and moves the rate: down when the last second averaged over vizSlow of the
+// frame budget, up after vizCalmFor under vizCalm of the faster budget's.
+func (a *App) vizCost(cost time.Duration) {
+	v := &a.viz
+	now := a.o.Now()
+	v.pending = false
+	if v.since.IsZero() {
+		v.since = now
+	}
+	v.sum += cost
+	v.n++
+	if now.Sub(v.since) < vizMeasure {
+		return
+	}
+	avg := v.sum / time.Duration(v.n)
+	v.sum, v.n, v.since = 0, 0, now
+	budget := func(level int) time.Duration { return time.Second / time.Duration(v.rates[level]) }
+	switch {
+	case float64(avg) > vizSlow*float64(budget(v.level)) && v.level < len(v.rates)-1:
+		v.level++
+		v.calm = time.Time{}
+		log.Printf("visualizer: slowing to %d fps", v.rates[v.level])
+	case v.level > 0 && float64(avg) < vizCalm*float64(budget(v.level-1)):
+		if v.calm.IsZero() {
+			v.calm = now
+		} else if now.Sub(v.calm) >= vizCalmFor {
+			v.level--
+			v.calm = time.Time{}
+		}
+	default:
+		v.calm = time.Time{}
+	}
+}
+
+// drawVizPanel draws the visualizer in r (Now Playing's panel) and records
+// it for the frame clock. Nothing is drawn, or woken, with the style Off or
+// no sound source.
+func (a *App) drawVizPanel(c *gfx.Canvas, r gfx.Rect) {
+	if !a.vizShown() || r.W < 8 || r.H < 8 {
+		return
+	}
+	a.markViz(r)
+	a.drawViz(c, r, a.VizStyle())
+}
+
+// vizShown reports whether a visualizer is chosen and has a sound source.
+func (a *App) vizShown() bool { return a.VizStyle() != VizOff && a.o.Visual != nil }
+
+// drawViz draws one style into r from the analyzer's current state; it
+// changes no state, so a partial frame equals a full one.
+func (a *App) drawViz(c *gfx.Canvas, r gfx.Rect, style VizStyle) {
+	c.Fill(r, colPanel)
+	if a.viz.an == nil {
+		return // no frame yet
+	}
+	switch style {
+	case VizBars:
+		a.drawBars(c, r)
+	case VizScope:
+		a.drawScope(c, r)
+	case VizVU:
+		a.drawVU(c, r)
+	case VizWaterfall:
+		a.drawWaterfall(c, r)
+	}
+}
+
+var colWhite = gfx.RGB(0xff, 0xff, 0xff)
+
+// mixColor is a toward b by t/256.
+func mixColor(a, b gfx.Color, t int) gfx.Color {
+	ch := func(sh uint) uint8 {
+		x, y := int(a>>sh)&0xff, int(b>>sh)&0xff
+		return uint8((x*(256-t) + y*t) >> 8)
+	}
+	return gfx.RGB(ch(16), ch(8), ch(0))
+}
+
+// drawBars: log-spaced bars, accent at the bottom to white at the top, with
+// a brighter cap on each peak.
+func (a *App) drawBars(c *gfx.Canvas, r gfx.Rect) {
+	level, peak := a.viz.an.Bars()
+	n := len(level)
+	gap := 1
+	if !a.isCRT() {
+		gap = max(r.W/256, 2)
+	}
+	bw := max((r.W-gap*(n-1))/n, 1)
+	x := r.X + (r.W-(bw*n+gap*(n-1)))/2
+	seg := max(r.H/32, 1) // the gradient is drawn in steps this high
+	capH := max(r.H/48, 1)
+	shade := func(y int) gfx.Color { return mixColor(colAccent, colWhite, y*256/r.H) }
+	for i := range level {
+		h := int(min(max(level[i], 0), 1)*float32(r.H) + 0.5)
+		for y := 0; y < h; y += seg {
+			sh := min(seg, h-y)
+			c.Fill(gfx.R(x, r.Bottom()-y-sh, bw, sh), shade(y+sh/2))
+		}
+		if ph := int(min(max(peak[i], 0), 1)*float32(r.H) + 0.5); ph > 0 {
+			ph = max(ph, capH)
+			c.Fill(gfx.R(x, r.Bottom()-ph, bw, capH), mixColor(shade(ph), colWhite, 160))
+		}
+		x += bw + gap
+	}
+}
+
+// drawScope: the wave as one accent line, a point per column, with vertical
+// runs joining neighbours.
+func (a *App) drawScope(c *gfx.Canvas, r gfx.Rect) {
+	pts := a.viz.scope
+	if len(pts) != r.W {
+		return
+	}
+	th := 1
+	if !a.isCRT() {
+		th = 2
+	}
+	mid := r.Y + r.H/2
+	c.Fill(gfx.R(r.X, mid, r.W, 1), colArtBg)
+	amp := float32(r.H/2 - th)
+	prev := 0
+	for i, v := range pts {
+		y := mid - int(min(max(v, -1), 1)*amp)
+		top, h := y, th
+		if i > 0 {
+			top, h = min(y, prev), abs(y-prev)+th
+		}
+		c.Fill(gfx.R(r.X+i, top, 1, h), colAccent)
+		prev = y
+	}
+}
+
+func abs(x int) int {
+	if x < 0 {
+		return -x
+	}
+	return x
+}
+
+var (
+	colLEDGreen  = gfx.RGB(0x4c, 0xaf, 0x50)
+	colLEDYellow = gfx.RGB(0xff, 0xc1, 0x07)
+)
+
+// glyphs are the meters' labels, 3×5 dots each (too small a strip for a font).
+var glyphs = map[byte][5]string{
+	'L': {"#..", "#..", "#..", "#..", "###"},
+	'R': {"##.", "#.#", "##.", "#.#", "#.#"},
+}
+
+// drawVU: two horizontal LED meters, one segment per dB from -40 to +3,
+// green, then yellow, then red above -3 dB, with a peak marker.
+func (a *App) drawVU(c *gfx.Canvas, r gfx.Rect) {
+	level, peak := a.viz.an.VU()
+	gap := max(r.H/10, 1)
+	rowH := (r.H - gap) / 2
+	segGap := 1
+	if !a.isCRT() {
+		segGap = 2
+	}
+	const nseg = 43
+	s := min(max(rowH/6, 1), 6) // glyph dot size
+	mx := r.X + 5*s
+	segW := max((r.Right()-mx-segGap*(nseg-1))/nseg, 1)
+	for ch, name := range "LR" {
+		y := r.Y + ch*(rowH+gap)
+		for gy, line := range glyphs[byte(name)] {
+			for gx, dot := range line {
+				if dot == '#' {
+					c.Fill(gfx.R(r.X+s+gx*s, y+(rowH-5*s)/2+gy*s, s, s), colDim)
+				}
+			}
+		}
+		pk := -1
+		if peak[ch] > 0 {
+			pk = min(int(peak[ch]*nseg), nseg-1)
+		}
+		for i := 0; i < nseg; i++ {
+			col := colLEDGreen
+			switch db := a.viz.an.DB(float32(i) / nseg); {
+			case db >= -3:
+				col = colError
+			case db >= -12:
+				col = colLEDYellow
+			}
+			switch {
+			case i == pk:
+				col = colText
+			case level[ch] < float32(i+1)/nseg:
+				col = mixColor(col, colBg, 210) // unlit
+			}
+			c.Fill(gfx.R(mx+i*(segW+segGap), y, segW, rowH), col)
+		}
+	}
+}
+
+// drawWaterfall: the sweep's picture, with the cursor line at the next column.
+func (a *App) drawWaterfall(c *gfx.Canvas, r gfx.Rect) {
+	v := &a.viz
+	if v.img == nil || v.img.W != r.W || v.img.H != r.H {
+		return
+	}
+	c.Blit(v.img, r)
+	c.Fill(gfx.R(r.X+v.cursor, r.Y, 1, r.H), colText)
+}
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./internal/viz`

Expected: `ok`. Golden screenshots written or changed: `nowplaying-viz-crt`, `nowplaying-viz-hdmi`, `viz-bars-crt`, `viz-bars-hdmi-1920x1200`, `viz-bars-hdmi`, `viz-scope-crt`, `viz-scope-hdmi-1920x1200`, `viz-scope-hdmi`, `viz-vu-crt`, `viz-vu-hdmi-1920x1200`, `viz-vu-hdmi`, `viz-waterfall-crt`, `viz-waterfall-hdmi-1920x1200`, `viz-waterfall-hdmi`. Open each one and check it: the `viz-<style>-*` crops show: Bars as light-blue gradient bars with white peak caps; Scope as a single accent line; VU as two LED meters L and R running green into a dim yellow and red zone; Waterfall as a sweep, painted up to its cursor line in the dark-blue-to-white palette. `nowplaying-viz-hdmi` and `nowplaying-viz-crt` show the bars panel under the "Next:" line, on CRT with the text moved up a little.

Then run `go test -count=1 -run '^$' -bench VizFrame ./internal/ui`. Expected: eight lines, all with `0 allocs/op`. On a desktop a 1920×1200 panel frame is about 0.1–0.2 ms.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/damage.go internal/ui/screens_play.go internal/ui/viz.go internal/ui/vizdraw_test.go internal/ui/testdata/golden
git commit -m "ui: the visualizer panel on Now Playing, drawn by a frame clock that damages only its rect, with a safety valve" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: The full-screen mode

**Files:**
- Create: `internal/ui/screens_viz.go`
- Modify: `internal/ui/app.go`, `internal/ui/hints_screens.go`, `internal/ui/screens_play.go`, `internal/ui/screensaver.go`
- Test: `internal/ui/screens_viz_test.go` (new); `internal/ui/hints_test.go`, `internal/ui/vizdraw_test.go` (modified)

**Interfaces:**
- **Consumes:**
  - `drawViz`, `markViz`, `vizTick` and `vizState` (Task 4);
  - `VizStyle` and `SetVizStyle` (Task 3).
- **Produces:**
  - **The screen.** `NewVizScreen(np *NowPlayingScreen) *VizScreen`.
    - It fills the body inside the title-safe area (`vizBody`).
    - It forwards `Handle` and `Release` to Now Playing.
    - B and Start pop it. Y pops it first, then opens the queue.
    - Entering with Off sets Bars.
    - Inside it, Select skips Off.
    - It pops (`Sync`) when nothing is playing.
  - **Start on Now Playing.** It pushes `VizScreen` when `canFullScreen()`: there is a Visual, a song is selected, and the player is not Stopped. Otherwise Start pauses as before.
  - **Hold timers.** They test `App.onNowPlaying(s)`, which is true for Now Playing on top or Now Playing under its `VizScreen`.
  - **The info line.** "Title — Artist  m:ss" in a dark box, laid out by `refresh` only when it changes. It moves clockwise once a minute (`vizCornerDue`).
  - **The hint bar.** It is drawn over the picture's bottom edge (`hintsUp`), on entry and for 3 s after any press.
  - **Screensaver.** It doesn't start on `VizScreen` while Playing (`saverScreen`).
  - **Hints.** Now Playing hints "Full screen" for Start, but only when `canFullScreen()`. `VizScreen`'s hints are Now Playing's without Start.
  - **Benchmark.** Full-screen cases are added to `BenchmarkVizFrame`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/screens_viz_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// fullApp is Now Playing with the visualizer in style, then Start: the full
// screen, with the hint bar already gone (it shows for 3 s after a press).
func fullApp(t *testing.T, prof Profile, style VizStyle) *testApp {
	t.Helper()
	ta := vizApp(t, prof, style)
	ta.press(input.BtnStart)
	ta.settle(t)
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Fatalf("Start left %T on top, want the full screen", ta.Top())
	}
	return ta
}

func lastCall(ta *testApp) string {
	if len(ta.pl.calls) == 0 {
		return ""
	}
	return ta.pl.calls[len(ta.pl.calls)-1]
}

func TestStartOnNowPlayingOpensFullScreenElsewherePauses(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnStart)
	if _, ok := ta.Top().(*VizScreen); !ok || len(ta.pl.calls) != 0 {
		t.Fatalf("on Now Playing: top %T, calls %q", ta.Top(), ta.pl.calls)
	}
	ta = vizApp(t, ProfileHDMI, VizBars)
	ta.Pop() // the Home screen
	ta.press(input.BtnStart)
	if _, ok := ta.Top().(*VizScreen); ok || lastCall(ta) != "toggle" {
		t.Fatalf("on Home: top %T, calls %q", ta.Top(), ta.pl.calls)
	}
	// With no sound source there is nothing to fill the screen with, and
	// with nothing playing nothing to show: Start pauses/plays as before.
	for name, setup := range map[string]func(ta *testApp){
		"no visual":       func(ta *testApp) { ta.o.Visual = nil },
		"nothing playing": func(ta *testApp) { ta.pl.st.Status = player.Stopped },
	} {
		ta = vizApp(t, ProfileHDMI, VizBars)
		setup(ta)
		ta.press(input.BtnStart)
		if _, ok := ta.Top().(*VizScreen); ok || lastCall(ta) != "toggle" {
			t.Errorf("%s: top %T, calls %q", name, ta.Top(), ta.pl.calls)
		}
	}
}

func TestFullScreenLeavesWithBAndStart(t *testing.T) {
	for _, b := range []input.Button{input.BtnB, input.BtnStart} {
		ta := fullApp(t, ProfileHDMI, VizBars)
		ta.press(b)
		if _, ok := ta.Top().(*NowPlayingScreen); !ok {
			t.Errorf("%v left %T on top, want Now Playing", b, ta.Top())
		}
		if len(ta.pl.calls) != 0 {
			t.Errorf("%v called the player: %q", b, ta.pl.calls)
		}
		ta.settle(t)
		if ta.viz.rect.Empty() || ta.viz.rect.H > ta.P.H/2 {
			t.Errorf("%v: the panel is not back: %v", b, ta.viz.rect)
		}
	}
}

func TestFullScreenPassesOtherKeysToNowPlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	for _, c := range []struct {
		b    input.Button
		call string
	}{{input.BtnA, "toggle"}, {input.BtnL, "prev"}, {input.BtnR, "next"}, {input.BtnLeft, "seek"}} {
		ta.press(c.b)
		if got := lastCall(ta); got != c.call {
			t.Errorf("%v called %q, want %q", c.b, got, c.call)
		}
	}
	ta.press(input.BtnDown)
	if ta.pl.st.VolumeDB >= 0 {
		t.Errorf("Down left the volume at %v dB", ta.pl.st.VolumeDB)
	}
	ta.press(input.BtnSelect) // a short Select: the next style
	if ta.VizStyle() != VizScope {
		t.Errorf("Select gave %v, want scope", ta.VizStyle())
	}
	hold(ta, input.BtnSelect, 2*time.Second) // held: mute (its timer belongs to Now Playing)
	if !ta.pl.st.Muted {
		t.Error("holding Select did not mute")
	}
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Fatalf("those keys left %T on top", ta.Top())
	}
	// X opens Now Playing's menu over the full screen; closing it returns.
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok || m.parent != ta.stack[len(ta.stack)-2].s {
		t.Fatalf("X: top %T", ta.Top())
	}
	ta.settle(t)
	if r := ta.viz.rect; r.Empty() || r.W != ta.P.W {
		t.Errorf("the full screen is not drawn behind the menu: %v", r)
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Errorf("closing the menu left %T", ta.Top())
	}
}

func TestYFromFullScreenOpensTheQueueOverNowPlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Fatalf("Y: top %T", ta.Top())
	}
	if _, ok := ta.stack[len(ta.stack)-2].s.(*NowPlayingScreen); !ok {
		t.Fatalf("under the queue: %T, want Now Playing", ta.stack[len(ta.stack)-2].s)
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Errorf("Back from the queue: %T, want Now Playing", ta.Top())
	}
	ta = fullApp(t, ProfileHDMI, VizBars)
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Errorf("Q: top %T", ta.Top())
	}
}

func TestEnteringWithTheStyleOffSetsBarsAndSaves(t *testing.T) {
	ta := vizApp(t, ProfileHDMI, VizOff)
	ta.press(input.BtnStart)
	if ta.VizStyle() != VizBars || ta.cfg.Display.Visualizer != "bars" {
		t.Fatalf("style %v, config %q", ta.VizStyle(), ta.cfg.Display.Visualizer)
	}
	ta.settle(t)
	if ta.viz.rect.Empty() {
		t.Error("nothing is drawn")
	}
	ta = fullApp(t, ProfileHDMI, VizWaterfall) // a chosen style stays
	if ta.VizStyle() != VizWaterfall {
		t.Errorf("style %v after entering with waterfall", ta.VizStyle())
	}
}

func TestFullScreenWithTheStyleOffDrawsNoVisualizer(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.SetVizStyle(VizOff)
	ta.dirty = true
	ta.settle(t)
	if ta.vizActive() || !ta.vizDue().IsZero() || !ta.viz.rect.Empty() {
		t.Errorf("active=%v due=%v rect=%v", ta.vizActive(), ta.vizDue(), ta.viz.rect)
	}
}

func TestFullScreenFillsTheBodyInsideTheSafeArea(t *testing.T) {
	for _, l := range vizLayouts {
		ta := fullApp(t, l.prof, VizBars)
		r := ta.viz.rect
		if r.X != 0 || r.W != l.prof.W || r.Y != l.prof.SafeY || r.Bottom() != l.prof.H-l.prof.SafeY {
			t.Errorf("%s: visualizer %v, want the screen inside SafeY=%d", l.name, r, l.prof.SafeY)
		}
		ta.press(input.BtnUp) // the hint bar shows: the body doesn't change
		ta.settle(t)
		if ta.viz.rect != r {
			t.Errorf("%s: the hint bar changed the visualizer from %v to %v", l.name, r, ta.viz.rect)
		}
	}
}

// cornerOf names where r sits in the body: 0 top left, 1 top right, 2
// bottom right, 3 bottom left (clockwise).
func cornerOf(ta *testApp, vs *VizScreen) int {
	body := ta.vizBody()
	left, top := vs.rect.X < body.X+body.W/2, vs.rect.Y < body.Y+body.H/2
	switch {
	case top && left:
		return 0
	case top:
		return 1
	case !left:
		return 2
	}
	return 3
}

func TestInfoLineMovesClockwiseOncePerMinute(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	vs := ta.Top().(*VizScreen)
	if vs.text != "Капитан Африка — Аквариум  1:15" {
		t.Errorf("info line %q", vs.text)
	}
	if ta.vizBody().Intersect(vs.rect) != vs.rect {
		t.Fatalf("the line %v is outside the body %v", vs.rect, ta.vizBody())
	}
	var seen []int
	for i := 0; i < 5; i++ {
		seen = append(seen, cornerOf(ta, vs))
		before := vs.rect
		ta.now = ta.now.Add(time.Minute)
		ta.pl.st.Position += time.Minute
		ta.clean()
		ta.onWake()
		if vs.rect == before {
			t.Fatalf("minute %d: the line did not move", i+1)
		}
		if !containsRect(ta.damage, before) || !containsRect(ta.damage, vs.rect) {
			t.Errorf("minute %d: moving from %v to %v damaged %v", i+1, before, vs.rect, ta.damage)
		}
		if err := ta.render(); err != nil {
			t.Fatal(err)
		}
	}
	if want := []int{0, 1, 2, 3, 0}; !slices.Equal(seen, want) {
		t.Errorf("corners %v, want %v", seen, want)
	}
}

func TestInfoLineIsDamagedEachSecondNotEachFrame(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	vs := ta.Top().(*VizScreen)
	runFrames(t, ta, 4)
	wakes, hits := 0, 0
	end := ta.now.Add(3 * time.Second)
	for ta.now.Before(end) {
		d := ta.untilWake()
		ta.now = ta.now.Add(d)
		ta.pl.st.Position += d
		ta.clean()
		ta.onWake()
		wakes++
		if containsRect(ta.damage, vs.rect) {
			hits++
		}
		if ta.redrawDue() {
			if err := ta.render(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if wakes < 60 || hits < 2 || hits > 4 {
		t.Errorf("%d wakes in 3 s damaged the info line %d times, want about 3", wakes, hits)
	}
	if vs.text != "Капитан Африка — Аквариум  1:18" && vs.text != "Капитан Африка — Аквариум  1:19" {
		t.Errorf("the line says %q after 3 s", vs.text)
	}
}

func TestHintBarShowsForThreeSecondsAfterAPress(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	// The press that opened the full screen counts: the bar is up.
	if !ta.hintsUp() {
		t.Fatal("no bar when the full screen opens")
	}
	step := func(d time.Duration) {
		ta.now = ta.now.Add(d)
		ta.onWake()
		if err := ta.render(); err != nil {
			t.Fatal(err)
		}
	}
	step(3*time.Second + 10*time.Millisecond)
	if ta.hintsUp() {
		t.Fatal("the bar is still up after 3 s")
	}
	hidden := cropRect(ta.disp.Last(), ta.hintRect())
	ta.press(input.BtnUp)
	ta.settle(t)
	if !ta.hintsUp() {
		t.Fatal("no bar after a press")
	}
	if got := cropRect(ta.disp.Last(), ta.hintRect()); samePixels(hidden.ToRGBA(), got.ToRGBA()) {
		t.Error("the bar is not drawn")
	}
	step(2 * time.Second)
	if !ta.hintsUp() {
		t.Error("the bar went before 3 s")
	}
	ta.press(input.BtnDown) // another press: 3 s from this one
	step(2 * time.Second)
	if !ta.hintsUp() {
		t.Error("a second press did not extend the bar")
	}
	step(1100 * time.Millisecond)
	if ta.hintsUp() {
		t.Error("the bar stayed after the last press + 3 s")
	}
	// Hints turned off in Settings: never a bar.
	ta.cfg.Display.Hints = false
	ta.press(input.BtnUp)
	if ta.hintH() != 0 || ta.hintRect().H != 0 {
		t.Errorf("a bar with hints off: %v", ta.hintRect())
	}
}

func TestScreensaverDoesNotStartInFullScreenWhilePlaying(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizBars)
	ta.cfg.Display.ScreensaverMinutes = 1
	if !ta.saverDue().IsZero() {
		t.Fatalf("a screensaver due at %v while playing in full screen", ta.saverDue())
	}
	ta.now = ta.now.Add(10 * time.Minute)
	ta.onWake()
	if ta.saver {
		t.Fatal("the screensaver started while playing in full screen")
	}
	ta.pl.st.Status = player.Paused
	ta.onPlayer(player.Event{})
	if ta.saverDue().IsZero() {
		t.Fatal("paused in full screen: no screensaver due")
	}
	ta.onWake()
	ta.settle(t)
	if !ta.saver {
		t.Fatal("the screensaver did not start while paused")
	}
	if _, ok := ta.Top().(*VizScreen); !ok {
		t.Errorf("the screensaver replaced the full screen with %T", ta.Top())
	}
	// ...and on Now Playing with the panel it works as before, playing too.
	np := vizApp(t, ProfileHDMI, VizBars)
	np.cfg.Display.ScreensaverMinutes = 1
	np.now = np.now.Add(2 * time.Minute)
	np.onWake()
	if !np.saver {
		t.Error("no screensaver on Now Playing while playing")
	}
}

func TestFullScreenPopsWhenNothingIsPlaying(t *testing.T) {
	for name, end := range map[string]func(ta *testApp){
		"queue cleared": func(ta *testApp) { ta.pl.st.Queue, ta.pl.st.Index, ta.pl.st.Status = nil, -1, player.Stopped },
		"queue ended":   func(ta *testApp) { ta.pl.st.Status = player.Stopped },
	} {
		ta := fullApp(t, ProfileHDMI, VizBars)
		end(ta)
		ta.onPlayer(player.Event{})
		ta.settle(t)
		if _, ok := ta.Top().(*NowPlayingScreen); !ok {
			t.Errorf("%s: top %T, want Now Playing", name, ta.Top())
		}
	}
}

func TestFullScreenHints(t *testing.T) {
	hints := func(ta *testApp) []string {
		var got []string
		for _, h := range ta.screenHints() {
			got = append(got, h.Label)
		}
		return got
	}
	ta := vizApp(t, ProfileHDMI, VizBars)
	if !slices.Contains(hints(ta), "Full screen") {
		t.Errorf("Now Playing hints %q lack Full screen", hints(ta))
	}
	for _, h := range ta.screenHints() {
		if h.Label == "Full screen" && h.Button != input.BtnStart {
			t.Errorf("Full screen is on %v", h.Button)
		}
	}
	ta.pad = false
	if cp, ok := ta.capFor(input.BtnStart); !ok || cp != "Space" {
		t.Errorf("Start's key is %q", cp)
	}
	none := vizApp(t, ProfileHDMI, VizBars)
	none.o.Visual = nil
	if slices.Contains(hints(none), "Full screen") {
		t.Error("Full screen is hinted with no sound source")
	}
	full := fullApp(t, ProfileHDMI, VizBars)
	if got := hints(full); slices.Contains(got, "Full screen") || !slices.Contains(got, "Back") || !slices.Contains(got, "Visualizer") {
		t.Errorf("full screen hints %q", got)
	}
}

func TestFullScreenVerifyModeHolds(t *testing.T) {
	for _, l := range []struct {
		name string
		prof Profile
	}{{"hdmi", ProfileHDMI}, {"crt", ProfileCRT240}} {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := fullApp(t, l.prof, style) // newTestApp turns verify on and fails the test on a mismatch
			runFrames(t, ta, 20)
			ta.press(input.BtnUp) // the bar comes up over the picture
			runFrames(t, ta, 20)
			ta.now = ta.now.Add(time.Minute) // the line moves
			ta.pl.st.Position += time.Minute
			ta.onWake()
			runFrames(t, ta, 10)
			ta.press(input.BtnX) // a menu over it
			runFrames(t, ta, 10)
			ta.press(input.BtnB)
			ta.pl.st.Status = player.Paused // the levels fall, the clock stops
			ta.onPlayer(player.Event{})
			runFrames(t, ta, 30)
		}
	}
}

func TestGoldenVizFull(t *testing.T) {
	for _, l := range vizLayouts {
		for style := VizBars; style <= VizWaterfall; style++ {
			ta := fullApp(t, l.prof, style)
			ta.verify = false // checked in TestFullScreenVerifyModeHolds; this keeps the big layouts quick
			runFrames(t, ta, 100)
			ta.verify = true
			runFrames(t, ta, 3)
			if ta.hintsUp() {
				t.Fatalf("%s %v: the hint bar is still up", l.name, style)
			}
			golden(t, "viz-full-"+style.String()+"-"+l.name, ta.disp.Last())
		}
	}
}

func TestSelectInFullScreenSkipsOff(t *testing.T) {
	ta := fullApp(t, ProfileHDMI, VizWaterfall)
	for _, want := range []VizStyle{VizBars, VizScope, VizVU, VizWaterfall, VizBars} {
		ta.press(input.BtnSelect)
		if got := ta.VizStyle(); got != want {
			t.Fatalf("Select gave %v, want %v", got, want)
		}
	}
	// On Now Playing itself the cycle still has Off.
	np := vizApp(t, ProfileHDMI, VizWaterfall)
	np.press(input.BtnSelect)
	if np.VizStyle() != VizOff {
		t.Errorf("Now Playing: Select from waterfall gave %v, want off", np.VizStyle())
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/hints_test.go b/internal/ui/hints_test.go
index a07e4f3..42f031a 100644
--- a/internal/ui/hints_test.go
+++ b/internal/ui/hints_test.go
@@ -31,11 +31,23 @@ var hintFixtures = map[string]func(ta *testApp) Screen{
 	"search":      func(ta *testApp) Screen { return NewSearchScreen() },
 	"queue":       func(ta *testApp) Screen { playingState(ta); return NewQueueScreen() },
 	"now playing": func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() },
-	"settings":    func(ta *testApp) Screen { return NewSettingsScreen() },
-	"playback":    func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
-	"display":     func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
-	"servers":     func(ta *testApp) Screen { return NewServersScreen() },
-	"wizard":      func(ta *testApp) Screen { return NewWizardScreen(false, false) },
+	"now playing with a visualizer": func(ta *testApp) Screen {
+		ta.o.Visual = &fakeVisual{}
+		playingState(ta)
+		return NewNowPlayingScreen()
+	},
+	"full screen": func(ta *testApp) Screen {
+		ta.o.Visual = &fakeVisual{}
+		playingState(ta)
+		np := NewNowPlayingScreen()
+		ta.Push(np)
+		return NewVizScreen(np)
+	},
+	"settings": func(ta *testApp) Screen { return NewSettingsScreen() },
+	"playback": func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
+	"display":  func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
+	"servers":  func(ta *testApp) Screen { return NewServersScreen() },
+	"wizard":   func(ta *testApp) Screen { return NewWizardScreen(false, false) },
 	"wizard with text": func(ta *testApp) Screen {
 		w := NewWizardScreen(false, false)
 		w.fields[stepURL] = []rune("http://h:4533")
diff --git a/internal/ui/vizdraw_test.go b/internal/ui/vizdraw_test.go
index c37129e..0d20d87 100644
--- a/internal/ui/vizdraw_test.go
+++ b/internal/ui/vizdraw_test.go
@@ -11,6 +11,7 @@ import (
 
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
+	"mistersubsonic/internal/input"
 	"mistersubsonic/internal/player"
 )
 
@@ -299,23 +300,32 @@ func TestVizSafetyValveStepsDownAndUp(t *testing.T) {
 func TestVizFrameAllocatesNothing(t *testing.T) {
 	for _, l := range vizLayouts {
 		for style := VizBars; style <= VizWaterfall; style++ {
-			ta := vizApp(t, l.prof, style)
-			runFrames(t, ta, 3) // setup: the analyzer, the buffers, the image
-			c, r := ta.canvas, ta.viz.rect
-			if n := testing.AllocsPerRun(20, func() {
-				ta.now = ta.now.Add(40 * time.Millisecond)
-				ta.damage = ta.damage[:0] // the app clears it with every frame
-				ta.vizTick(ta.now)
-				ta.drawViz(c, r, style)
-			}); n != 0 {
-				t.Errorf("%s %v: %v allocations per frame", l.name, style, n)
+			for _, full := range []bool{false, true} {
+				ta := vizApp(t, l.prof, style)
+				if full {
+					ta.press(input.BtnStart)
+				}
+				runFrames(t, ta, 3) // setup: the analyzer, the buffers, the image
+				c, r := ta.canvas, ta.viz.rect
+				if n := testing.AllocsPerRun(20, func() {
+					ta.now = ta.now.Add(40 * time.Millisecond)
+					ta.damage = ta.damage[:0] // the app clears it with every frame
+					ta.vizTick(ta.now)
+					ta.drawViz(c, r, style)
+					if vs, ok := ta.Top().(*VizScreen); ok {
+						vs.refresh(ta.App) // the wake checks the corner line every frame
+					}
+				}); n != 0 {
+					t.Errorf("%s %v full=%v: %v allocations per frame", l.name, style, full, n)
+				}
 			}
 		}
 	}
 }
 
 // BenchmarkVizFrame is one visualizer frame: the analysis and the drawing
-// into the panel, for each style on a big HDMI panel and a CRT strip.
+// into the panel (or the full screen), for each style on a big HDMI panel and
+// a CRT strip.
 // On the device: ./ui.test -test.run '^$' -test.bench VizFrame
 func BenchmarkVizFrame(b *testing.B) {
 	for _, l := range []struct {
@@ -323,24 +333,33 @@ func BenchmarkVizFrame(b *testing.B) {
 		prof Profile
 	}{{"hdmi-1920x1200", PickProfile(1920, 1200, "auto")}, {"crt-320x240", ProfileCRT240}} {
 		for style := VizBars; style <= VizWaterfall; style++ {
-			b.Run(l.name+"/"+style.String(), func(b *testing.B) {
-				t := &testing.T{}
-				ta := vizApp(t, l.prof, style)
-				ta.verify = false
-				sig := make([]float32, 4096)
-				(&fakeVisual{}).Window(sig)
-				ta.o.Visual = &cachedVisual{sig}
-				runFrames(t, ta, 3)
-				c, r := ta.canvas, ta.viz.rect
-				b.ReportAllocs()
-				b.ResetTimer()
-				for i := 0; i < b.N; i++ {
-					ta.now = ta.now.Add(40 * time.Millisecond)
-					ta.damage = ta.damage[:0] // the app clears it with every frame
-					ta.vizTick(ta.now)
-					ta.drawViz(c, r, style)
+			for _, full := range []bool{false, true} {
+				name := l.name
+				if full {
+					name += "-full"
 				}
-			})
+				b.Run(name+"/"+style.String(), func(b *testing.B) {
+					t := &testing.T{}
+					ta := vizApp(t, l.prof, style)
+					if full {
+						ta.press(input.BtnStart)
+					}
+					ta.verify = false
+					sig := make([]float32, 4096)
+					(&fakeVisual{}).Window(sig)
+					ta.o.Visual = &cachedVisual{sig}
+					runFrames(t, ta, 3)
+					c, r := ta.canvas, ta.viz.rect
+					b.ReportAllocs()
+					b.ResetTimer()
+					for i := 0; i < b.N; i++ {
+						ta.now = ta.now.Add(40 * time.Millisecond)
+						ta.damage = ta.damage[:0] // the app clears it with every frame
+						ta.vizTick(ta.now)
+						ta.drawViz(c, r, style)
+					}
+				})
+			}
 		}
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
undefined: VizScreen
ta.vizBody undefined (type *testApp has no field or method vizBody)
undefined: NewVizScreen
```

- [ ] **Step 3: Implement**

`internal/ui/screens_viz.go` (new file):

```go
package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// The full-screen visualizer (spec §2.2): a screen pushed on Now Playing by
// Start. The picture fills the body; one small line in a corner names the
// song and moves to the next corner once a minute (burn-in); the hint bar
// shows only for a few seconds after a press.

const (
	vizHintShow    = 3 * time.Second // the hint bar stays this long after a press
	vizCornerEvery = time.Minute     // the info line moves to the next corner this often
)

type VizScreen struct {
	np        *NowPlayingScreen // the screen below: it handles every key but the ones here
	since     time.Time         // when the screen opened: the corner follows from it
	hintUntil time.Time
	armed     bool // a timer is waiting to hide the hint bar

	// The info line as last laid out, and what it was laid out from. Draw
	// only paints text and rect, so a partial frame equals a full one;
	// refresh changes them and damages both places.
	song   subsonic.ID
	sec    int
	corner int
	laid   bool
	text   string
	rect   gfx.Rect
}

func NewVizScreen(np *NowPlayingScreen) *VizScreen { return &VizScreen{np: np} }

func (s *VizScreen) Title() string { return "Now Playing" }

func (s *VizScreen) Enter(a *App) {
	s.since = a.o.Now()
	if a.VizStyle() == VizOff { // the point of the screen is the picture
		a.SetVizStyle(VizBars)
	}
	s.hintUntil = s.since.Add(vizHintShow)
	s.arm(a)
	s.refresh(a)
}

// canFullScreen reports whether Start on Now Playing opens the full screen:
// there is a sound source to draw and something playing.
func (a *App) canFullScreen() bool { return a.o.Visual != nil && a.playingSomething() }

// playingSomething reports whether a song is selected and the player has not
// stopped (the queue ended, or was cleared).
func (a *App) playingSomething() bool {
	return a.hasCurrent() && a.state().Status != player.Stopped
}

// vizHost is the full screen when it is on top, or under a menu opened from it.
func (a *App) vizHost() *VizScreen {
	switch t := a.Top().(type) {
	case *VizScreen:
		return t
	case *MenuScreen:
		v, _ := t.parent.(*VizScreen)
		return v
	}
	return nil
}

// vizBody is the full screen's area: the whole screen inside the title-safe
// lines. The hint bar is drawn over its bottom edge when it is up, so the
// picture doesn't change size with it.
func (a *App) vizBody() gfx.Rect { return gfx.R(0, a.P.SafeY, a.P.W, a.P.H-2*a.P.SafeY) }

// hintsUp reports whether the hint bar is drawn: always, except on the full
// screen, where it shows for vizHintShow after a press.
func (a *App) hintsUp() bool {
	if v, ok := a.Top().(*VizScreen); ok {
		return a.clock().Before(v.hintUntil)
	}
	return true
}

// vizCornerDue is when the info line moves next (zero: no full screen).
func (a *App) vizCornerDue() time.Time {
	v := a.vizHost()
	if v == nil {
		return time.Time{}
	}
	n := a.o.Now().Sub(v.since)/vizCornerEvery + 1
	return v.since.Add(n * vizCornerEvery)
}

// reveal brings the hint bar up for vizHintShow from now.
func (s *VizScreen) reveal(a *App) {
	if !a.hintsUp() {
		a.Damage(a.hintRect())
	}
	s.hintUntil = a.o.Now().Add(vizHintShow)
	s.arm(a)
}

// arm makes sure a timer fires when the bar is due to go (a press after
// it was set only moves hintUntil; the timer then waits again).
func (s *VizScreen) arm(a *App) {
	if s.armed {
		return
	}
	s.armed = true
	a.After(s, s.hintUntil.Sub(a.o.Now()), func() {
		s.armed = false
		if a.o.Now().Before(s.hintUntil) {
			s.arm(a)
		}
	})
}

func (s *VizScreen) Handle(a *App, e input.Event) bool {
	if e.Kind == input.Press {
		switch e.Button {
		case input.BtnB, input.BtnStart:
			a.Pop()
			return true
		case input.BtnY: // the queue opens over Now Playing, so Back returns there
			a.Pop()
			return s.np.Handle(a, e)
		case input.BtnQueue:
			a.Pop()
			return false // the app opens the queue
		}
	}
	s.reveal(a)
	return s.np.Handle(a, e)
}

// Release goes to Now Playing too (a short Select or X ends there); its menu
// opens over this screen.
func (s *VizScreen) Release(a *App, b input.Button) {
	s.np.host = s
	s.np.Release(a, b)
	s.np.host = nil
}

// Hints are Now Playing's, less Start, which leaves (Back says so).
func (s *VizScreen) Hints(a *App) []Hint {
	var hs []Hint
	for _, h := range s.np.Hints(a) {
		if h.Button != input.BtnStart {
			hs = append(hs, h)
		}
	}
	return hs
}

// Sync leaves when nothing plays any more, and keeps the info line current
// before each frame.
func (s *VizScreen) Sync(a *App) {
	if !a.playingSomething() {
		a.Pop()
		return
	}
	s.refresh(a)
}

// refresh lays out the info line (title, artist and elapsed time, in the
// corner for this minute) and damages the old and the new place when it
// changed: each second for the time, or when it moves. It builds nothing
// while the song, the second and the corner are the same.
func (s *VizScreen) refresh(a *App) {
	st := a.state()
	song, ok := st.Current()
	if !ok {
		return
	}
	now := a.clock()
	sec := int(s.np.position(a, st, now) / time.Second)
	corner := int(now.Sub(s.since)/vizCornerEvery) % 4
	if s.laid && song.ID == s.song && sec == s.sec && corner == s.corner {
		return
	}
	p, fs, body := a.P, a.F.Small, a.vizBody()
	pad := p.Margin / 2
	tail := "  " + clock(time.Duration(sec)*time.Second)
	head := song.Title
	if song.Artist != "" {
		head += " — " + song.Artist
	}
	head = fs.Truncate(head, body.W-2*p.Margin-2*pad-fs.Measure(tail))
	text := head + tail
	w, h := fs.Measure(text)+2*pad, fs.Height()+pad
	r := gfx.R(body.X+p.Margin, body.Y+p.Margin, w, h) // clockwise: top left, top right, bottom right, bottom left
	if corner == 1 || corner == 2 {
		r.X = body.Right() - p.Margin - w
	}
	if corner >= 2 {
		r.Y = body.Bottom() - p.Margin - a.hintH() - h
	}
	if s.laid {
		a.Damage(s.rect)
	}
	a.Damage(r)
	s.song, s.sec, s.corner, s.laid, s.text, s.rect = song.ID, sec, corner, true, text, r
}

func (s *VizScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	a.drawVizPanel(c, area)
	if s.text == "" {
		return
	}
	fs := a.F.Small
	pad := a.P.Margin / 2
	c.Fill(s.rect, colOverlay)
	fs.Draw(c, s.rect.X+pad, s.rect.Y+pad/2+fs.Ascent(), s.text, colText, s.rect)
}
```

Then Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 736f122..809f556 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -521,6 +521,7 @@ func (a *App) untilWake() time.Duration {
 		consider(a.volumeUntil)
 	}
 	consider(a.vizDue())
+	consider(a.vizCornerDue())
 	consider(a.saverDue())
 	if a.saver {
 		consider(now.Add(saverStep)) // the drift; nothing else moves
@@ -583,6 +584,9 @@ func (a *App) onWake() {
 	if a.vizActive() && !now.Before(a.viz.next) {
 		a.vizTick(now)
 	}
+	if v := a.vizHost(); v != nil {
+		v.refresh(a)
+	}
 	if !a.volumeUntil.IsZero() && !now.Before(a.volumeUntil) {
 		a.Damage(a.volumePanelRect()) // the panel goes
 		a.volumeUntil = time.Time{}
@@ -781,7 +785,7 @@ func (a *App) render() error {
 	}
 	full := a.dirty
 	a.dirty = false
-	if _, np := a.Top().(*NowPlayingScreen); !np {
+	if !saverScreen(a.Top()) {
 		a.saver = false
 	}
 	if a.saver {
@@ -809,9 +813,13 @@ func (a *App) drawFrame(c *gfx.Canvas) {
 	// Content stays inside the title-safe area (SafeY lines top and bottom;
 	// panels still run to the edges).
 	body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY-a.hintH())
+	vs := a.vizHost()
+	if vs != nil { // the hint bar is drawn over the picture's bottom edge
+		body = a.vizBody()
+	}
 	if top != nil {
 		_, fullscreen := top.(*NowPlayingScreen)
-		if !fullscreen {
+		if !fullscreen && vs == nil {
 			a.drawHeader(c, top.Title())
 			body = gfx.R(0, p.SafeY+p.HeaderH, p.W, body.Bottom()-p.SafeY-p.HeaderH)
 			if a.hasCurrent() {
@@ -825,7 +833,9 @@ func (a *App) drawFrame(c *gfx.Canvas) {
 		c.SetClip(body.Intersect(outer))
 		top.Draw(a, c, body)
 		c.SetClip(outer)
-		a.drawHints(c)
+		if a.hintsUp() {
+			a.drawHints(c)
+		}
 	}
 	if !a.mq.seen {
 		a.mq = marquee{}
diff --git a/internal/ui/hints_screens.go b/internal/ui/hints_screens.go
index ff739b7..cef89fc 100644
--- a/internal/ui/hints_screens.go
+++ b/internal/ui/hints_screens.go
@@ -80,10 +80,14 @@ func (s *NowPlayingScreen) Hints(a *App) []Hint {
 	if song, ok := st.Current(); ok && a.isStarred(songStar(song)) {
 		star = "Unstar"
 	}
-	return []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
+	hs := []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
 		hkPair(input.BtnUp, input.BtnDown, "Volume"), hkPair(input.BtnL, input.BtnR, "Prev/Next"),
 		menuHint, hkHold(input.BtnX, star), hk(input.BtnY, "Queue"),
 		hk(input.BtnSelect, "Visualizer"), hkHold(input.BtnSelect, "Mute")}
+	if a.canFullScreen() {
+		hs = append(hs, hk(input.BtnStart, "Full screen"))
+	}
+	return hs
 }
 
 func (s *QueueScreen) Hints(a *App) []Hint {
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index a7a0a92..2d646ec 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -41,6 +41,8 @@ type NowPlayingScreen struct {
 	xDown    bool // X is held
 	xStarred bool // ...and has already starred: its release does nothing
 	xHold    int  // like selHold
+
+	host Screen // the full screen while it forwards a release here: menus open over it (nil: this screen)
 }
 
 func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }
@@ -56,6 +58,9 @@ func (s *NowPlayingScreen) Release(a *App, b input.Button) {
 		s.selDown, s.selMuted = false, false
 		if !muted {
 			next := (a.VizStyle() + 1) % VizStyle(len(vizNames))
+			if next == VizOff && s.host != nil {
+				next = VizBars // an empty full screen is no use: it cycles through the four pictures
+			}
 			a.SetVizStyle(next)
 			a.Toast("Visualizer: %s", next.Label())
 		}
@@ -65,7 +70,11 @@ func (s *NowPlayingScreen) Release(a *App, b input.Button) {
 		starred := s.xStarred
 		s.xDown, s.xStarred = false, false
 		if !starred {
-			a.Push(NewMenuScreen(s, "Now Playing", append([]menuEntry{s.starEntry(a)}, modeEntries(a)...)))
+			var parent Screen = s
+			if s.host != nil {
+				parent = s.host
+			}
+			a.Push(NewMenuScreen(parent, "Now Playing", append([]menuEntry{s.starEntry(a)}, modeEntries(a)...)))
 		}
 		return
 	}
@@ -199,7 +208,7 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 		s.xHold++
 		hold := s.xHold
 		a.After(s, starHold, func() {
-			if s.xDown && s.xHold == hold && a.Top() == Screen(s) {
+			if s.xDown && s.xHold == hold && a.onNowPlaying(s) {
 				s.xStarred = true
 				if song, ok := a.state().Current(); ok {
 					a.toggleStar(songStar(song))
@@ -212,12 +221,17 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 		pl.Next()
 	case input.BtnY:
 		a.Push(NewQueueScreen())
+	case input.BtnStart: // the full screen (elsewhere Start pauses)
+		if !a.canFullScreen() {
+			return false
+		}
+		a.Push(NewVizScreen(s))
 	case input.BtnSelect: // every press starts a fresh hold (a lost release is recovered): short changes the visualizer, long mutes
 		s.selDown, s.selMuted = true, false
 		s.selHold++
 		hold := s.selHold
 		a.After(s, muteHold, func() {
-			if s.selDown && s.selHold == hold && a.Top() == Screen(s) {
+			if s.selDown && s.selHold == hold && a.onNowPlaying(s) {
 				s.selMuted = true
 				a.toggleMute()
 			}
@@ -228,6 +242,15 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 	return true
 }
 
+// onNowPlaying reports whether s is what the user is looking at: on top, or
+// under its full screen.
+func (a *App) onNowPlaying(s *NowPlayingScreen) bool {
+	if v, ok := a.Top().(*VizScreen); ok {
+		return v.np == s
+	}
+	return a.Top() == Screen(s)
+}
+
 func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	p := a.P
 	st := a.state()
diff --git a/internal/ui/screensaver.go b/internal/ui/screensaver.go
index aeed7ad..cafaeb6 100644
--- a/internal/ui/screensaver.go
+++ b/internal/ui/screensaver.go
@@ -5,6 +5,7 @@ import (
 
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
+	"mistersubsonic/internal/player"
 )
 
 // The screensaver (spec §8.2): after display.screensaver_minutes without
@@ -25,15 +26,28 @@ func (a *App) saverMinutes() int {
 }
 
 // saverDue is when the screensaver starts, or zero if it can't now (not on
-// Now Playing, off, on, or the exit prompt is up).
+// Now Playing or its full screen, off, on, or the exit prompt is up).
 func (a *App) saverDue() time.Time {
 	m := a.saverMinutes()
-	if _, np := a.Top().(*NowPlayingScreen); !np || m <= 0 || a.saver || a.confirm {
+	if !saverScreen(a.Top()) || m <= 0 || a.saver || a.confirm {
 		return time.Time{}
 	}
+	if _, full := a.Top().(*VizScreen); full && a.state().Status == player.Playing {
+		return time.Time{} // the full screen is for watching the music
+	}
 	return a.lastInput.Add(time.Duration(m) * time.Minute)
 }
 
+// saverScreen is a screen the screensaver can cover: Now Playing and its
+// full screen.
+func saverScreen(s Screen) bool {
+	switch s.(type) {
+	case *NowPlayingScreen, *VizScreen:
+		return true
+	}
+	return false
+}
+
 // wake ends the screensaver; it reports whether it was on.
 func (a *App) wake() bool {
 	on := a.saver
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `viz-full-bars-crt`, `viz-full-bars-hdmi-1920x1200`, `viz-full-bars-hdmi`, `viz-full-scope-crt`, `viz-full-scope-hdmi-1920x1200`, `viz-full-scope-hdmi`, `viz-full-vu-crt`, `viz-full-vu-hdmi-1920x1200`, `viz-full-vu-hdmi`, `viz-full-waterfall-crt`, `viz-full-waterfall-hdmi-1920x1200`, `viz-full-waterfall-hdmi`. Open each one and check it: each `viz-full-<style>-*` shows the style filling the screen, with the info box "Капитан Африка — Аквариум  1:15" top left. The bars' left third is empty because the test signal has no bass. On CRT everything stays inside the safe area.

Then run `go test -count=1 -run '^$' -bench 'VizFrame/.*full' ./internal/ui`. Expected: the full-screen cases, all with `0 allocs/op`. On a desktop 1920×1200 is about 0.7–2 ms, without the present.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/hints_screens.go internal/ui/hints_test.go internal/ui/screens_play.go internal/ui/screens_viz.go internal/ui/screens_viz_test.go internal/ui/screensaver.go internal/ui/vizdraw_test.go internal/ui/testdata/golden
git commit -m "ui: a full-screen visualizer on Start, with a drifting info line and hints that hide" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Docs, the TV checks and the device benchmarks

**Files:**
- Modify:
  - `README.md`
  - `docs/spikes.md`
  - `docs/testing-on-mister.md`
  - `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`
  - `docs/superpowers/plans/backlog.md`
  - `Makefile`

**Interfaces:**
- **Consumes:** everything above.
- **Produces:**
  - **The README:**
    - the visualizer: Select cycles it, Start opens full screen, and the corner line;
    - X short opens the menu, and holding X stars;
    - shuffle and repeat are independent, and are in both X menus;
    - Settings → Display → Visualizer, and `[display] visualizer`.
  - **`docs/spikes.md`:** "Plan 6 on the MiSTer", with the PC numbers and "pending" tables for the device, plus the exact commands.
  - **The checklist:** new items 30–35. They cover the benchmarks, a listening check per style, the panel on HDMI and CRT, the motion in time with the sound, the corner line, and the screensaver. Log becomes 35.
  - **The main spec:** §2 no longer lists the visualizer as a non-goal. §8 gets the new controls. §13 marks the visualizer as done.
  - **The backlog:** B is done, except for the device numbers.
  - **The Makefile:** `viz` joins `TESTS`, so `viz.test` is built for ARM.

- [ ] **Step 1: Record the results and the new checks**

Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

````diff
diff --git a/Makefile b/Makefile
index a9032b0..c726c34 100644
--- a/Makefile
+++ b/Makefile
@@ -80,7 +80,7 @@ deploy: release
 # Every package's tests are built for ARM too, so 32-bit-only breakage (an
 # int overflow, say) fails here and in CI; the ones run on the device are
 # copied by deploy-dev. Old test binaries (of removed packages) are cleared first.
-TESTS := audio ui gfx
+TESTS := audio ui gfx viz
 mister-test:
 	rm -rf $(BIN)/arm/tests
 	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/tests/ ./...
diff --git a/README.md b/README.md
index 3d5e715..7ee7eb0 100644
--- a/README.md
+++ b/README.md
@@ -53,7 +53,7 @@ safer way is `ca_file`, below). A config file the app can't read is kept as
 `config.toml.invalid-<date>` when the wizard replaces it.
 
 Settings (the last section) switches between servers, adds and removes them, and changes the
-volume, ReplayGain, scrobbling, transcoding, the layout and the screensaver (dark screen with a
+volume, ReplayGain, scrobbling, transcoding, the layout, the visualizer and the screensaver (dark screen with a
 drifting cover after some idle minutes on Now Playing; off with 0). Each server keeps its resume
 state, scrobble queue and cover cache in `servers/<name>-<id>/` next to the config, so `cover_art_mb`
 applies per server (each server's cache gets that much).
@@ -101,19 +101,34 @@ app never changes the video mode or `MiSTer.ini` itself. Keep `fb_size=0` (autom
 | D-pad | move (Left into the HDMI sidebar) | ←/→ seek ±10 s (held: ±30 s), ↑/↓ volume |
 | A | open / play | play/pause |
 | B | back (hold 2 s on the root to exit) | back |
-| X | menu: play now/next, add to queue, star, go to artist/album | star/unstar |
+| X | menu: play now/next, add to queue, star, go to artist/album | press: menu (star, shuffle, repeat); hold 1 s: star/unstar |
 | Y | Now Playing | queue |
 | L / R | page (letter jump on Artists) | previous / next track |
-| Start | play/pause | play/pause |
-| Select | shuffle-play the list | press: shuffle / repeat modes; hold 1 s: mute |
+| Start | play/pause | full screen visualizer (play/pause when nothing is playing) |
+| Select | shuffle-play the list | press: next visualizer style; hold 1 s: mute |
 
 Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = mute, PgUp/PgDn =
 L/R, Space = Start. In Search and the setup wizard, letters type and Backspace deletes. With a
-controller, hold Select on Now Playing to mute; changing the volume turns the sound back on. A
+controller, hold Select on Now Playing to mute (and hold X to star); changing the volume turns the sound back on. A
 multimedia keyboard's media keys work on every screen: volume up/down (held to repeat), mute,
 play/pause, next, previous, and fast-forward/rewind to seek. A volume panel shows the level for a
 moment whenever it changes.
 
+**Visualizer:** Now Playing can draw the music as it is heard, in a panel under the track
+info: Bars (spectrum), Scope (waveform), VU (two level meters) or Waterfall (a scrolling
+spectrogram). Select cycles Off, Bars, Scope, VU and Waterfall, and the choice is saved
+(Settings → Display → Visualizer, or `visualizer = "off"|"bars"|"scope"|"vu"|"waterfall"` under
+`[display]`; off by default). Start on Now Playing, while something is playing, opens it full
+screen inside the title-safe area, with a small title and time line that moves to another corner
+every minute; the hint bar hides after 3 s. In full screen Select cycles the styles without Off,
+and B or Start leaves. If drawing is too slow for the device, it lowers its own frame rate and
+logs "visualizer: slowing to N fps". The screensaver does not start while the visualizer is
+playing music.
+
+**Shuffle and repeat** are in the menu: X on Now Playing opens star/unstar, Shuffle and Repeat
+(off, all, one), and the menu closes after a choice. The queue's X menu has them too. Shuffle and
+repeat can be on together.
+
 **Hints:** the bar along the bottom of every screen shows what the buttons do there: as gamepad
 buttons after a gamepad press, as keys after a key press. Settings → Display → Hints turns it off.
 
diff --git a/docs/spikes.md b/docs/spikes.md
index 941680c..5966328 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -194,3 +194,32 @@ Measured with the test binaries only (`ui.test -test.bench 'Partial|Repaint'`, `
 - What remains is Go's standard JPEG decoder, about 0.77 s for this size.
 
 **On the TV:** pending. Run `docs/testing-on-mister.md` items 26–29 and record them here.
+
+## Plan 6 on the MiSTer
+
+**Visualizer cost.** Desktop reference (Ryzen 9 6900HS, 0 allocs/op), measured as analysis plus drawing into the panel or the full body, without the present:
+
+| Case | Bars | Scope | VU | Waterfall |
+|---|---|---|---|---|
+| Panel 1920×1200 (1050×153) | 88 µs | 99 µs | 111 µs | 157 µs |
+| Panel 320×240 (162×34) | 23 µs | 22 µs | 24 µs | 23 µs |
+| Full screen 1920×1200 | 670 µs | 683 µs | 1.13 ms | 1.97 ms |
+| Full screen 320×240 | 48 µs | 52 µs | 61 µs | 73 µs |
+
+The analysis alone (`internal/viz`, `BenchmarkAnalyzerUpdate`): 49 µs on HDMI settings, 18 µs on CRT settings. The cost on the A9 will be mostly the present of up to 1920×1200 per frame, which these numbers leave out.
+
+**On the MiSTer:** pending. After `make deploy-dev MISTER=<ip>`, on the device:
+
+```
+./ui.test -test.run '^$' -test.bench VizFrame -test.benchtime 30x
+./viz.test -test.run '^$' -test.bench AnalyzerUpdate
+```
+
+| Case | Bars | Scope | VU | Waterfall |
+|---|---|---|---|---|
+| Panel 1920×1200 | pending | pending | pending | pending |
+| Panel 320×240 | pending | pending | pending | pending |
+| Full screen 1920×1200 | pending | pending | pending | pending |
+| Full screen 320×240 | pending | pending | pending | pending |
+
+Then run `docs/testing-on-mister.md` items 31–35 and record them here, with the frame-rate defaults that fit 60% of each budget.
diff --git a/docs/superpowers/plans/backlog.md b/docs/superpowers/plans/backlog.md
index d548ded..f2b1436 100644
--- a/docs/superpowers/plans/backlog.md
+++ b/docs/superpowers/plans/backlog.md
@@ -19,7 +19,9 @@ Plans 5 and 5b did most of this list; see "Resolved by Plan 5" and "Resolved by
 
 ---
 
-## B. Visualizer
+## B. Visualizer (done in Plan 6, except the device numbers)
+
+**Status:** built in Plan 6 (spec `docs/superpowers/specs/2026-10-01-mister-subsonic-visualizer-design.md`): four styles, a panel and a full-screen mode. The frame rates, the listening check and the TV check wait for the MiSTer; see "Plan 6 on the MiSTer" in `docs/spikes.md`. The text below is the original sketch.
 
 **What:** an optional visualizer on Now Playing: spectrum bars and/or an oscilloscope drawn from the music as it is heard. Spec §2 left it out of v1, and §13 lists it as a candidate.
 
diff --git a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
index e1f4265..2b4cfff 100644
--- a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
+++ b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
@@ -34,7 +34,7 @@
 
 ## 2. Non-goals for v1
 
-- Visualizer, EQ, lyrics, internet radio, local or SMB files, CD playback.
+- EQ, lyrics, internet radio, local or SMB files, CD playback. (The visualizer was a non-goal for v1; Plan 6 added it, see `2026-10-01-mister-subsonic-visualizer-design.md`.)
 - Chapter navigation inside single-file album FLACs using the embedded CUESHEET. The file plays as one track with full seek; chapters are the first item for v2.
 - Playing music in the background while another MiSTer core runs. Exiting the app stops playback.
 - A web remote or phone control.
@@ -298,14 +298,16 @@ Wizard: Server URL → Username → Password → (API key, optional) → Test 
 | D-pad | move focus | ←/→ seek ±10 s (hold: ±30 s) |
 | A | open / play | play/pause |
 | B | back | back to previous screen |
-| X | context menu: Play now · Play next · Add to queue · Star/Unstar · Go to artist · Go to album | star/unstar |
+| X | context menu: Play now · Play next · Add to queue · Star/Unstar · Go to artist · Go to album | press: menu (Star/Unstar · Shuffle · Repeat), closed after a choice; hold 1 s: star/unstar |
 | Y | open Now Playing | open Queue |
 | L / R | page up/down (letter jump on Artists) | previous / next track |
-| Start | play/pause (global) | play/pause |
-| Select | shuffle-play current list | press: cycle shuffle → repeat modes; hold 1 s: mute |
+| Start | play/pause | full-screen visualizer when something is playing, else play/pause |
+| Select | shuffle-play current list | press: next visualizer style (Off → Bars → Scope → VU → Waterfall; in full screen without Off); hold 1 s: mute |
 
 Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, Space = play/pause, PgUp/PgDn = L/R, `n` = Now Playing, `q` = Queue.
 
+**Shuffle and repeat** are independent and can be on together; they live in the X menu on Now Playing and in the queue's X menu. **Visualizer:** see the Plan 6 spec (`2026-10-01-mister-subsonic-visualizer-design.md`).
+
 **Mute** is M on a keyboard, the media Mute key, or holding Select on Now Playing for one second (the release after that hold does nothing). Settings → Playback has no Volume or Mute rows; volume is Up/Down on Now Playing and the media keys. **Settings help:** the focused setting's help text (one or two dim lines) shows under a settings list, and Transcode bitrate is listed only while Transcode to is mp3.
 
 **Media keys** (volume up and down, mute, play/pause, next, previous, fast-forward and rewind) work on every screen. The top screen gets each key first, so a text field or a screen with its own meaning for it keeps it; the X menu, the exit prompt and the screensaver let them through (a media key wakes the screensaver and acts on the first press). Next and previous need a queue. Seeking needs a current track, moves ±10 s (held: ±30 s, at most four seeks a second) and stops a second before the end.
@@ -444,7 +446,7 @@ If a spike fails, the design section it tests gets revised before implementation
 
 - CUESHEET chapters in single-file album FLACs.
 - Internet radio stations from Navidrome's `getInternetRadioStations`.
-- Visualizer and EQ.
+- EQ. (The visualizer is done, Plan 6.)
 - Lyrics (`getLyricsBySongId`).
 - A phone web remote.
 - An in-app updater.
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index 069f8ee..6f43d80 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -110,7 +110,12 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
 27. **MP3 seeking.** In a long MP3 album, seek a little forward and back with Left and Right on Now Playing. In the last 20 s of a track (when the next one is queued), tap Left once: it plays on from there and the next track starts without a gap. A VBR MP3 lands near the time shown.
 28. **Config comments.** Add a comment line to `config.toml`, change a setting in the app, and exit: the comment is still there.
 29. **Launcher signals.** Start the launcher in one ssh session with `/media/fat/Scripts/MiSTer_Subsonic.sh; echo $?`. From a second ssh session, run `kill -TERM $(ps | grep '[S]cripts/MiSTer_Subsonic.sh' | awk '{print $1}')`. Expected: the app exits, the screen and menu come back, and the first session prints "MiSTer Subsonic closed." and `0`.
-30. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+30. **Visualizer benchmarks.** On the MiSTer, in the folder `deploy-dev` copied to, run `./ui.test -test.run '^$' -test.bench VizFrame -test.benchtime 30x` and `./viz.test -test.run '^$' -test.bench AnalyzerUpdate`. Record the numbers in `docs/spikes.md` ("Plan 6 on the MiSTer"). No sound or picture is involved.
+31. **Visualizer listening check.** Ask the user before playing anything, and keep the volume low. Play a FLAC album for a few minutes with each style (Bars, Scope, VU, Waterfall) in full screen (Start on Now Playing). There are no dropouts, and `log.txt` has no "visualizer: slowing to" line. If it does, set the frame-rate defaults so that each frame fits in 60% of its budget.
+32. **Visualizer on the TV.** Ask the user before showing anything. The panel under the track info is in a sensible place on HDMI and on a CRT (inside the title-safe area, and the text block does not jump). The motion is in time with the sound: bass hits land with the beat, and after a seek or a skip the picture follows within a moment.
+33. **Full screen and the corner line.** After a minute in full screen the title and time line moves to the next corner (top left, top right, bottom right, bottom left), and no static pixels stay. The hint bar shows after a press and hides after 3 s. Select in full screen cycles the styles without Off, and Start or B leaves.
+34. **Visualizer and the screensaver.** While music plays the screensaver does not start, in the panel or in full screen. Pause and wait: it starts after its idle time, also over the full screen, and a key wakes it without doing anything else.
+35. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
````

The numbers in `docs/spikes.md` are from a desktop. The device tables say "pending", because the MiSTer was off while this plan was written.

- [ ] **Step 2: Check the tree**

Run: `go vet ./... && make test && make e2e && make mister mister-test` (zig on `PATH`)

Expected:
- `test-launcher ok`, and `ok` for every Go package.
- `e2e ok: …` and `e2e-ui ok: …`.
- The glibc lines:
  - `mss-cli`, `mistersubsonic`, `audio.test` and `ui.test` need 2.29;
  - `gfx.test` and `viz.test` need no glibc.

- [ ] **Step 3: Commit**

```bash
git add Makefile README.md docs/spikes.md docs/superpowers/plans/backlog.md docs/superpowers/specs/2026-09-28-mister-subsonic-design.md docs/testing-on-mister.md
git commit -m "docs: the visualizer; TV checks and the device benchmarks to run" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 4: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" tables and stop here: the device steps wait for the next session.

- [ ] **Step 5: If it answers, deploy and hand over to the user (ask first)**

Ask the user before doing anything on the device.
- `make deploy MISTER=<ip>` replaces the installed app. Their `config.toml` is kept.
- The checks play music and show the visualizer on their TV. Ask them to turn the volume down first.
- Never read `/dev/fb0` on the device. For a picture of the screen, ask the user to press Print Screen and fetch the PNG.

With their go-ahead:
1. Run the benchmarks from `docs/spikes.md` with the test binaries. They make no sound.
2. Fill in the device tables.
3. If a style's frame takes more than 60% of its budget, lower `vizFPSHDMI` or `vizFPSCRT` in `internal/ui/viz.go`, and say so.
4. Ask the user to run checklist items 30–35.

Afterwards:
- Record what they report under "Plan 6 on the MiSTer", replacing "pending".
- Commit with `git commit -am "docs: Plan 6 on the TV" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"`.
- Anything that isn't a small fix goes to `docs/superpowers/plans/backlog.md`.

---

## After this plan

- The device numbers and the TV check, for Plans 5, 5b and 6, when the MiSTer is back.
- The web and mobile remote (backlog C): the user asked for it next.
- Faster list scrolling at full resolution, which needs measuring on the device.
- The first release, only when the user asks.
