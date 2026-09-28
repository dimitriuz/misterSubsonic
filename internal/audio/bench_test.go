package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
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
