package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func decodeAll(t *testing.T, name string, f Format) ([]float32, Decoder) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	d, err := OpenDecoder(bytes.NewReader(data), f)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	var all []float32
	buf := make([]float32, 4096*2)
	for {
		n, err := d.Read(buf)
		all = append(all, buf[:n*2]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
	return all, d
}

func TestDecodeFLACMatchesWAV(t *testing.T) {
	for _, base := range []string{"tone-44k16", "tone-96k24", "mono-44k16"} {
		t.Run(base, func(t *testing.T) {
			flac, fd := decodeAll(t, base+".flac", FormatFLAC)
			defer fd.Close()
			wav, wd := decodeAll(t, base+".wav", FormatWAV)
			defer wd.Close()
			if len(flac) == 0 || len(flac) != len(wav) {
				t.Fatalf("sample count flac=%d wav=%d", len(flac), len(wav))
			}
			for i := range flac {
				if flac[i] != wav[i] {
					t.Fatalf("sample %d differs: flac=%v wav=%v", i, flac[i], wav[i])
				}
			}
			if fd.SampleRate() != wd.SampleRate() {
				t.Fatalf("rate flac=%d wav=%d", fd.SampleRate(), wd.SampleRate())
			}
			if got := fd.LengthFrames(); got != uint64(len(flac)/2) {
				t.Fatalf("LengthFrames=%d, decoded %d", got, len(flac)/2)
			}
		})
	}
}

func TestDecodeRatesAndMonoUpmix(t *testing.T) {
	_, d := decodeAll(t, "tone-96k24.flac", FormatFLAC)
	defer d.Close()
	if d.SampleRate() != 96000 {
		t.Fatalf("rate = %d, want 96000", d.SampleRate())
	}
	mono, md := decodeAll(t, "mono-44k16.flac", FormatFLAC)
	defer md.Close()
	for i := 0; i < len(mono); i += 2 {
		if mono[i] != mono[i+1] {
			t.Fatalf("mono not duplicated to both channels at frame %d", i/2)
		}
	}
}

func TestDecodeMP3(t *testing.T) {
	pcm, d := decodeAll(t, "tone-44k16.mp3", FormatMP3)
	defer d.Close()
	if d.SampleRate() != 44100 {
		t.Fatalf("rate = %d", d.SampleRate())
	}
	frames := len(pcm) / 2
	if frames < 44100*4/10 || frames > 44100*6/10 {
		t.Fatalf("mp3 decoded %d frames, want about 22050", frames)
	}
	var peak float64
	for _, s := range pcm {
		peak = math.Max(peak, math.Abs(float64(s)))
	}
	if peak < 0.1 {
		t.Fatalf("mp3 decoded to near-silence (peak %v)", peak)
	}
}

func TestDecoderSeek(t *testing.T) {
	all, d0 := decodeAll(t, "tone-44k16.flac", FormatFLAC)
	d0.Close()
	data, _ := os.ReadFile("testdata/tone-44k16.flac")
	d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SeekFrame(12345); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 64)
	n, err := d.Read(buf)
	if err != nil || n != 32 {
		t.Fatalf("read after seek: n=%d err=%v", n, err)
	}
	for i := 0; i < 64; i++ {
		if buf[i] != all[12345*2+i] {
			t.Fatalf("after seek sample %d = %v, want %v", i, buf[i], all[12345*2+i])
		}
	}
}

type failingReader struct{ io.ReadSeeker }

var errBoom = errors.New("boom")

func (failingReader) Read([]byte) (int, error) { return 0, errBoom }

func TestDecoderPropagatesSourceError(t *testing.T) {
	data, _ := os.ReadFile("testdata/tone-44k16.flac")
	_, err := OpenDecoder(failingReader{bytes.NewReader(data)}, FormatFLAC)
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want wrapping errBoom", err)
	}
}

func TestFormatFromSuffix(t *testing.T) {
	cases := map[string]Format{"flac": FormatFLAC, ".FLAC": FormatFLAC, "mp3": FormatMP3, "wav": FormatWAV, "m4a": FormatUnknown, "opus": FormatUnknown, "": FormatUnknown}
	for in, want := range cases {
		if got := FormatFromSuffix(in); got != want {
			t.Errorf("FormatFromSuffix(%q) = %v, want %v", in, got, want)
		}
	}
}
