package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"testing"
	"time"
)

// TestRealDeviceListening plays through the real sound device so a person
// can listen for glitches. It makes sound on real speakers, so only a human
// runs it, deliberately, after turning the volume down: it is skipped
// unless MSS_DEVICE_TEST=1 (optionally MSS_ALSA_DEVICE=<name>). On a MiSTer:
//
//	MSS_DEVICE_TEST=1 ./audio.test -test.run RealDevice -test.v
//
// Everything plays at about -30 dBFS (quiet). Expect: 2 s of a clean 440 Hz
// tone, then the 44.1k/16 and 96k/24 test tones (0.5 s each), with no
// clicks, crackle or pitch shift.
func TestRealDeviceListening(t *testing.T) {
	if os.Getenv("MSS_DEVICE_TEST") != "1" {
		t.Skip("set MSS_DEVICE_TEST=1 to play through the real device")
	}
	out, err := OpenDevice(DeviceOptions{Name: os.Getenv("MSS_ALSA_DEVICE")})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	const quiet = 0.03 // about -30 dBFS
	var written uint64
	write := func(pcm []float32) {
		for len(pcm) > 0 {
			n := out.Write(pcm)
			written += uint64(n)
			pcm = pcm[n*2:]
			if len(pcm) > 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	tone := make([]float32, 2*OutputRate*2)
	for i := 0; i < len(tone)/2; i++ {
		v := float32(quiet * math.Sin(2*math.Pi*440*float64(i)/OutputRate))
		tone[2*i], tone[2*i+1] = v, v
	}
	write(tone)

	// The fixtures peak at -6 dBFS; a gain of 0.06 brings them to about -30.
	for _, name := range []string{"tone-44k16.flac", "tone-96k24.flac"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := NewResampler(d.SampleRate(), OutputRate, DefaultResampleQuality)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]float32, 4096*2)
		var pcm []float32
		for {
			n, err := d.Read(buf)
			for i := range buf[:n*2] {
				buf[i] *= 0.06
			}
			pcm = rs.Process(buf[:n*2], pcm[:0])
			write(pcm)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		write(rs.Flush(nil))
		rs.Close()
		d.Close()
		t.Logf("played %s (%d Hz)", name, d.SampleRate())
	}
	deadline := time.Now().Add(5 * time.Second)
	for out.Consumed() < written && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
