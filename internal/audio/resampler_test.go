package audio

import (
	"math"
	"testing"
)

func sine(frames, rate int, hz float64) []float32 {
	out := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		v := float32(0.5 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
		out[2*i], out[2*i+1] = v, v
	}
	return out
}

func TestResamplerOutputLength(t *testing.T) {
	r, err := NewResampler(44100, 48000, DefaultResampleQuality)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	in := sine(44100, 44100, 1000)
	var out []float32
	for i := 0; i < len(in); i += 4096 * 2 {
		end := min(i+4096*2, len(in))
		out = r.Process(in[i:end], out)
	}
	out = r.Flush(out)
	frames := len(out) / 2
	if frames < 47990 || frames > 48010 {
		t.Fatalf("1 s at 44.1k resampled to %d frames, want ~48000", frames)
	}
}

// A 1 kHz sine resampled 44.1k -> 48k must stay a clean 1 kHz sine.
func TestResamplerLowDistortion(t *testing.T) {
	r, err := NewResampler(44100, 48000, DefaultResampleQuality)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := r.Flush(r.Process(sine(44100, 44100, 1000), nil))
	// Compare the middle of the output against an ideal 48k sine with the best-fit phase.
	var num, den float64
	start, n := 4800, 38400
	for i := start; i < start+n; i++ {
		s := float64(out[2*i])
		num += s * math.Sin(2*math.Pi*1000*float64(i)/48000)
		den += s * math.Cos(2*math.Pi*1000*float64(i)/48000)
	}
	phase := math.Atan2(den, num)
	var errPow, sigPow float64
	for i := start; i < start+n; i++ {
		ideal := 0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000+phase)
		d := float64(out[2*i]) - ideal
		errPow += d * d
		sigPow += ideal * ideal
	}
	snr := 10 * math.Log10(sigPow/errPow)
	if snr < 60 {
		t.Fatalf("resampled sine SNR = %.1f dB, want >= 60 dB", snr)
	}
}

func TestResamplerContinuityAcrossChunks(t *testing.T) {
	in := sine(20000, 44100, 440)
	whole, _ := NewResampler(44100, 48000, DefaultResampleQuality)
	defer whole.Close()
	a := whole.Process(in, nil)

	split, _ := NewResampler(44100, 48000, DefaultResampleQuality)
	defer split.Close()
	b := split.Process(in[:7777*2], nil)
	b = split.Process(in[7777*2:], b)

	if len(a) != len(b) {
		t.Fatalf("len whole=%d split=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs whole=%v split=%v", i, a[i], b[i])
		}
	}
}
