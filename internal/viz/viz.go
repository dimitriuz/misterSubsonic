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
