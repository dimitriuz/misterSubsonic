package player

import (
	"bytes"
	"os"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

// xingFrame builds a first MPEG frame carrying a Xing/Info header: MPEG-1
// (or 2) Layer III at 44.1 (22.05) kHz, joint stereo or mono.
func xingFrame(mpeg2, mono, crc bool, flags uint32, frames, size uint32, toc []byte) []byte {
	b1 := byte(0xFB) // MPEG-1, layer III, no CRC
	if mpeg2 {
		b1 = 0xF3
	}
	if crc {
		b1 &^= 1
	}
	mode := byte(1) << 6
	side := 32
	switch {
	case mono && mpeg2:
		mode, side = 3<<6, 9
	case mono:
		mode, side = 3<<6, 17
	case mpeg2:
		side = 17
	}
	f := []byte{0xFF, b1, 0x90, mode}
	if crc {
		f = append(f, 0, 0)
	}
	f = append(f, make([]byte, side)...)
	f = append(f, "Xing"...)
	put := func(v uint32) { f = append(f, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }
	put(flags)
	if flags&1 != 0 {
		put(frames)
	}
	if flags&2 != 0 {
		put(size)
	}
	if flags&4 != 0 {
		f = append(f, toc...)
	}
	return f
}

// quadToc puts i% of the time at (i/100)^2 of the bytes: a bitrate that
// starts low and grows.
func quadToc() []byte {
	toc := make([]byte, 100)
	for i := range toc {
		toc[i] = byte(i * i * 256 / 10000)
	}
	return toc
}

func tagged(n int) []byte { // an ID3v2.4 tag of n bytes after its header
	return append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, byte(n >> 7 & 0x7F), byte(n & 0x7F)}, make([]byte, n)...)
}

func TestProbeMP3ReadsTheXingTable(t *testing.T) {
	toc := quadToc()
	for name, c := range map[string]struct {
		mpeg2, mono, crc bool
	}{"mpeg1 stereo": {}, "mpeg1 mono": {mono: true}, "mpeg2 stereo": {mpeg2: true},
		"mpeg2 mono": {mpeg2: true, mono: true}, "crc": {crc: true}} {
		data := append(tagged(300), xingFrame(c.mpeg2, c.mono, c.crc, 7, 3000, 25600, toc)...)
		data = append(data, make([]byte, 1000)...)
		l := probeMP3(bytes.NewReader(data))
		if l.start != 310 || l.bytes != 25600 || !bytes.Equal(l.toc, toc) {
			t.Errorf("%s: start %d bytes %d toc ok %v", name, l.start, l.bytes, bytes.Equal(l.toc, toc))
		}
	}
}

func TestProbeMP3WithoutATable(t *testing.T) {
	audio := append([]byte{0xFF, 0xFB, 0x90, 0x44}, make([]byte, 400)...) // a frame with no Xing/Info
	cases := map[string][]byte{
		"no header at all":  make([]byte, 64),
		"no table flag":     append(tagged(20), xingFrame(false, false, false, 3, 10, 999, nil)...),
		"plain first frame": append(tagged(20), audio...),
		"truncated table":   append(tagged(20), xingFrame(false, false, false, 7, 10, 999, make([]byte, 40))...),
	}
	for name, data := range cases {
		if l := probeMP3(bytes.NewReader(data)); l.toc != nil {
			t.Errorf("%s: toc %v, want none", name, l.toc)
		}
	}
	if l := probeMP3(bytes.NewReader(cases["plain first frame"])); l.start != 30 {
		t.Errorf("start %d, want the frame after the 30-byte tag", l.start)
	}
	if l := probeMP3(bytes.NewReader(cases["no header at all"])); l.start != 0 {
		t.Errorf("no tag: start %d, want 0", l.start)
	}
}

func TestProbeMP3TestdataXingHeader(t *testing.T) {
	data, err := os.ReadFile("../audio/testdata/tone-44k16.mp3") // ffmpeg's Info header, 44-byte tag
	if err != nil {
		t.Fatal(err)
	}
	l := probeMP3(bytes.NewReader(data))
	if l.start != 44 || l.bytes != int64(len(data))-44 || len(l.toc) != 100 || l.toc[1] != 0x17 || l.toc[99] != 0xff {
		t.Fatalf("layout start %d bytes %d toc %d entries (toc[1]=%#x)", l.start, l.bytes, len(l.toc), l.toc[1])
	}
	noxing, _ := os.ReadFile("../audio/testdata/tone-44k16-noxing.mp3")
	if l := probeMP3(bytes.NewReader(noxing)); l.toc != nil || l.start != 44 {
		t.Fatalf("no-Xing file: start %d, toc %v", l.start, l.toc)
	}
}

func TestMP3LayoutOffsetFollowsTheTable(t *testing.T) {
	l := mp3Layout{start: 100, toc: quadToc(), bytes: 25600}
	song := subsonic.Song{Size: 25700, Duration: 100}
	for name, c := range map[string]struct {
		at   time.Duration
		want int64
	}{
		"start":                             {0, 100},
		"halfway is a quarter of the bytes": {50 * time.Second, 100 + 6400},
		"between entries":                   {50500 * time.Millisecond, 100 + 6500},  // toc[50]=64, toc[51]=66
		"last entry":                        {99500 * time.Millisecond, 100 + 25300}, // toc[99]=250, then 256
		"past the end":                      {500 * time.Second, 25699},
	} {
		if got, ok := l.offset(song, c.at); got != c.want || !ok {
			t.Errorf("%s: %d,%v; want %d", name, got, ok, c.want)
		}
	}
	// Without a table the estimate stays proportional.
	l.toc = nil
	if got, _ := l.offset(song, 50*time.Second); got != 100+12800 {
		t.Errorf("no table: %d, want the proportional %d", got, 100+12800)
	}
}

// capReader is a ReadSeeker that says how much its stream holds, and counts
// the seeks that move it.
type capReader struct {
	*bytes.Reader
	capacity int64
	seeks    int
}

func (c *capReader) Capacity() int64 { return c.capacity }
func (c *capReader) Seek(off int64, whence int) (int64, error) {
	c.seeks++
	return c.Reader.Seek(off, whence)
}

// A tag that puts the first frame beyond what the stream holds would cost a
// new range request to look at it: the probe leaves the table and takes the
// tag's end as the start.
func TestProbeMP3SkipsAFrameBeyondTheRing(t *testing.T) {
	data := append(tagged(12000), xingFrame(false, false, false, 7, 3000, 25600, quadToc())...)
	data = append(data, make([]byte, 1000)...)
	c := &capReader{Reader: bytes.NewReader(data), capacity: 8 << 10}
	l := probeMP3(c)
	if l.start != 12010 || l.toc != nil {
		t.Fatalf("start %d, toc %v: want the tag's end and no table", l.start, l.toc)
	}
	if c.seeks != 0 {
		t.Fatalf("%d seeks, the probe went looking beyond the ring", c.seeks)
	}
	// With room for it, the same file is probed.
	c = &capReader{Reader: bytes.NewReader(data), capacity: 1 << 20}
	if l := probeMP3(c); l.toc == nil {
		t.Fatal("no table found with the first frame inside the ring")
	}
}
