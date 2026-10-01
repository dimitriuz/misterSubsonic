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
