package player

import (
	"io"
	"time"

	"mistersubsonic/internal/subsonic"
)

// mp3Layout is what the start of an MP3 says about where its audio is.
type mp3Layout struct {
	start int64  // the first frame, after the ID3v2 tag
	toc   []byte // the Xing/Info seek table: 100 entries, or nil
	bytes int64  // audio bytes the table's fractions of 256 refer to; 0 if unknown
}

// maxFrameProbe is how far past the tag to look for the first frame, and
// the largest tag probed for one: reading further would restart the stream
// for a header that isn't worth it.
const maxFrameProbe = 4096
const maxProbedTag = 256 << 10

// holder is a reader that says how many bytes it keeps in memory (a
// stream.Reader's ring).
type holder interface{ Capacity() int64 }

// probeMP3 reads the ID3v2 tag size and, after it, the first MPEG frame's
// Xing/Info header. It moves r; the caller seeks it afterwards. Whatever it
// can't find stays zero: without a tag start is 0, without a table toc is nil.
// A first frame that r couldn't hold from the start is not looked for: that
// is another range request, for the sake of a table.
func probeMP3(r io.ReadSeeker) mp3Layout {
	var l mp3Layout
	var h [10]byte
	if _, err := io.ReadFull(r, h[:]); err == nil && string(h[:3]) == "ID3" {
		size := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
		l.start = 10 + size
		if h[5]&0x10 != 0 {
			l.start += 10 // a footer
		}
	}
	if l.start > maxProbedTag {
		return l
	}
	if h, ok := r.(holder); ok && l.start+maxFrameProbe > h.Capacity() {
		return l
	}
	if _, err := r.Seek(l.start, io.SeekStart); err != nil {
		return l
	}
	buf := make([]byte, maxFrameProbe)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		if toc, size, ok := parseXing(buf[i:]); ok {
			l.start += int64(i)
			l.toc, l.bytes = toc, size
			return l
		}
		if mpegLayer3(buf[i:]) {
			l.start += int64(i) // the first frame, without a Xing header
			return l
		}
	}
	return l
}

// mpegLayer3 reports whether f starts with a valid Layer III frame header.
func mpegLayer3(f []byte) bool {
	version, layer, rate, bitrate := f[1]>>3&3, f[1]>>1&3, f[2]>>2&3, f[2]>>4
	return version != 1 && layer == 1 && rate != 3 && bitrate != 0 && bitrate != 15
}

// parseXing reads the Xing/Info header of the frame f starts with: its seek
// table (nil if the header has none) and the audio bytes it counts (0 if
// not said). ok is false if f isn't a frame with such a header.
func parseXing(f []byte) (toc []byte, size int64, ok bool) {
	if !mpegLayer3(f) {
		return nil, 0, false
	}
	mpeg1, mono := f[1]>>3&3 == 3, f[3]>>6 == 3
	side := 32 // the side info's length, by MPEG version and channel mode
	switch {
	case mpeg1 && mono:
		side = 17
	case !mpeg1 && mono:
		side = 9
	case !mpeg1:
		side = 17
	}
	at := 4 + side
	if f[1]&1 == 0 {
		at += 2 // a CRC after the header
	}
	if len(f) < at+8 || (string(f[at:at+4]) != "Xing" && string(f[at:at+4]) != "Info") {
		return nil, 0, false
	}
	be := func(b []byte) int64 { return int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3]) }
	flags := be(f[at+4:])
	at += 8
	if flags&1 != 0 { // the frame count
		at += 4
	}
	if flags&2 != 0 { // the audio bytes
		if len(f) >= at+4 {
			size = be(f[at:])
		}
		at += 4
	}
	if flags&4 != 0 && len(f) >= at+100 {
		toc = append([]byte(nil), f[at:at+100]...)
	}
	return toc, size, true
}

// offset estimates the byte where at falls in an MP3 of s.Size bytes lasting
// s.Duration. With a Xing table it follows the table, which knows the
// bitrate varies; without one, it is in proportion to time after the tag:
// exact for a constant bitrate.
func (l mp3Layout) offset(s subsonic.Song, at time.Duration) (int64, bool) {
	d := time.Duration(s.Duration) * time.Second
	if s.Size <= 0 || d <= 0 || l.start >= s.Size {
		return 0, false
	}
	audio := s.Size - l.start
	if l.toc != nil && l.bytes > 0 && l.bytes <= audio {
		audio = l.bytes
	}
	frac := min(max(float64(at)/float64(d), 0), 1)
	if l.toc != nil {
		p := frac * 100
		i := min(int(p), 99)
		lo, hi := float64(l.toc[i]), 256.0
		if i < 99 {
			hi = float64(l.toc[i+1])
		}
		frac = (lo + (hi-lo)*(p-float64(i))) / 256
	}
	return min(l.start+int64(float64(audio)*frac), s.Size-1), true
}
