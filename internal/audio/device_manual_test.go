package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"strings"
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

// TestNativeProbeManual opens a native device and reopens it at several
// rates, writing only zeros (silence), and logs what the device reports and
// how long each reopen takes. Skipped unless MSS_NATIVE_PROBE names the
// device, for example on a MiSTer with a USB DAC:
//
//	MSS_NATIVE_PROBE=hw:CARD=E30,DEV=0 ./audio.test -test.run NativeProbe -test.v
func TestNativeProbeManual(t *testing.T) {
	name := os.Getenv("MSS_NATIVE_PROBE")
	if name == "" {
		t.Skip("set MSS_NATIVE_PROBE=hw:CARD=<id>,DEV=0 to probe a native device (silence only)")
	}
	out, err := OpenDevice(DeviceOptions{Name: name, Rate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	d := out.(*device)
	t.Logf("open: rate=%d bits=%d", d.Rate(), d.FormatBits())
	silence := make([]float32, 2*4800)
	play := func(dur time.Duration) {
		end := time.Now().Add(dur)
		for time.Now().Before(end) {
			if out.Write(silence) == 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	hwParams := func() string {
		b, _ := os.ReadFile(os.Getenv("MSS_PROBE_HWPARAMS"))
		return strings.Join(strings.Fields(string(b)), " ")
	}
	play(300 * time.Millisecond)
	t.Logf("hw_params at 48000: %s", hwParams())
	for _, rate := range []int{44100, 96000, 192000, 88200, 48000, 22050} {
		t0 := time.Now()
		err := out.Reopen(name, rate)
		reopen := time.Since(t0)
		if err != nil {
			t.Logf("reopen %d: %v (lost=%v, supports=%v) after %v", rate, err, out.Lost(), out.Supports(rate), reopen)
			if err := out.Reopen(name, 48000); err != nil {
				t.Fatal(err)
			}
			continue
		}
		c0 := out.Consumed()
		out.Write(silence)
		t1 := time.Now()
		for out.Consumed() == c0 && time.Since(t1) < time.Second {
			time.Sleep(time.Millisecond)
		}
		first := time.Since(t1)
		play(300 * time.Millisecond)
		t.Logf("reopen %d: took %v, first frames consumed after %v; rate=%d bits=%d; hw_params: %s",
			rate, reopen, first, out.Rate(), d.FormatBits(), hwParams())
	}
	t.Logf("lost=%v", out.Lost())
}

// TestMixerProbeManual reads a card's hardware volume, moves it, mutes and
// unmutes it, and puts it back. Nothing plays. Skipped unless
// MSS_MIXER_PROBE names the card, for example: MSS_MIXER_PROBE=hw:E30
func TestMixerProbeManual(t *testing.T) {
	card := os.Getenv("MSS_MIXER_PROBE")
	if card == "" {
		t.Skip("set MSS_MIXER_PROBE=hw:<card> to probe a card's hardware volume (silent)")
	}
	ctl, err := openALSAMixer(card)
	if err != nil {
		t.Fatal(err)
	}
	defer closeALSAMixer()
	m := ctl.(alsaMixer)
	was, err := m.get()
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := ctl.Range()
	t.Logf("range %v..%v dB, switch %v, now %v dB", lo, hi, ctl.HasSwitch(), was)
	defer func() {
		got, err := ctl.Set(was)
		t.Logf("restored to %v dB (%v)", got, err)
	}()
	for _, db := range []float64{-10, -10.4, -20, -59.5} {
		got, err := ctl.Set(db)
		t.Logf("set %v dB: got %v (%v)", db, got, err)
	}
	if ctl.HasSwitch() {
		t.Logf("switch off: %v, on: %v", ctl.SetSwitch(false), ctl.SetSwitch(true))
	}
}
