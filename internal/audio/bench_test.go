package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkDecodeResample measures FLAC decode + resample to 48 kHz, the
// per-track CPU cost on the device. Run on the MiSTer via a cross-compiled
// test binary (see docs/spikes.md). It reports the fraction of one core
// needed for real-time playback as "core%".
func BenchmarkDecodeResample(b *testing.B) {
	for _, c := range []struct {
		name    string
		file    string
		quality int
	}{
		{"44k16-q3", "tone-44k16.flac", 3},
		{"44k16-q5", "tone-44k16.flac", 5},
		{"44k16-q7", "tone-44k16.flac", 7},
		{"96k24-q5", "tone-96k24.flac", 5},
	} {
		b.Run(c.name, func(b *testing.B) {
			data, err := os.ReadFile("testdata/" + c.file)
			if err != nil {
				b.Fatal(err)
			}
			buf := make([]float32, 4096*2)
			var out []float32
			var audioSeconds float64
			start := time.Now()
			for i := 0; i < b.N; i++ {
				d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
				if err != nil {
					b.Fatal(err)
				}
				rs, err := NewResampler(d.SampleRate(), OutputRate, c.quality)
				if err != nil {
					b.Fatal(err)
				}
				frames := 0
				for {
					n, err := d.Read(buf)
					out = rs.Process(buf[:n*2], out[:0])
					frames += n
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				audioSeconds += float64(frames) / float64(d.SampleRate())
				rs.Close()
				d.Close()
			}
			b.ReportMetric(100*time.Since(start).Seconds()/audioSeconds, "core%")
		})
	}
}

// BenchmarkDecodeNative compares a native device's path (decode only, at
// the file's rate) with a fixed device's (decode + resample to 48 kHz),
// as the fraction of one core real-time playback needs ("core%"). Files
// come from MSS_BENCH_FLAC (comma-separated paths), else the 96k/24 tone.
func BenchmarkDecodeNative(b *testing.B) {
	files := []string{"testdata/tone-96k24.flac"}
	if env := os.Getenv("MSS_BENCH_FLAC"); env != "" {
		files = strings.Split(env, ",")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			b.Fatal(err)
		}
		for _, resample := range []bool{false, true} {
			name := filepath.Base(file) + "/native"
			if resample {
				name = filepath.Base(file) + "/resample48k"
			}
			b.Run(name, func(b *testing.B) {
				buf := make([]float32, 4096*2)
				var out []float32
				var audioSeconds float64
				start := time.Now()
				for i := 0; i < b.N; i++ {
					d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
					if err != nil {
						b.Fatal(err)
					}
					var rs *Resampler
					if resample {
						if rs, err = NewResampler(d.SampleRate(), OutputRate, DefaultResampleQuality); err != nil {
							b.Fatal(err)
						}
					}
					frames := 0
					for {
						n, err := d.Read(buf)
						if rs != nil {
							out = rs.Process(buf[:n*2], out[:0])
						}
						frames += n
						if errors.Is(err, io.EOF) {
							break
						}
						if err != nil {
							b.Fatal(err)
						}
					}
					audioSeconds += float64(frames) / float64(d.SampleRate())
					if rs != nil {
						rs.Close()
					}
					d.Close()
				}
				b.ReportMetric(100*time.Since(start).Seconds()/audioSeconds, "core%")
			})
		}
	}
}
