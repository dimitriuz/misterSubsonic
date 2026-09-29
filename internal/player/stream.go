package player

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/stream"
	"mistersubsonic/internal/subsonic"
)

// Opened is a song's byte stream, ready for the engine.
type Opened struct {
	Source     io.ReadSeeker // also an io.Closer
	Format     audio.Format
	Transcoded bool
	Offset     time.Duration // song position of the first byte (transcoded seeks)
}

// Opener opens a song's stream. prefetch=true asks for a small read-ahead
// until the source is promoted (see Promoter).
type Opener func(ctx context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error)

// Promoter is implemented by sources opened in prefetch mode.
type Promoter interface{ Promote() }

type StreamSettings struct {
	TranscodeFormat  string // mp3 | flac | wav
	TranscodeBitrate int    // kbps, for lossy targets
	WindowBytes      int64
	PrefetchBytes    int64 // default 4 MiB
}

var contentTypes = map[string]audio.Format{
	"audio/flac": audio.FormatFLAC, "audio/x-flac": audio.FormatFLAC,
	"audio/mpeg": audio.FormatMP3, "audio/mp3": audio.FormatMP3,
	"audio/wav": audio.FormatWAV, "audio/x-wav": audio.FormatWAV, "audio/wave": audio.FormatWAV,
}

// PlanStream decides between the original file and a server transcode.
func PlanStream(s subsonic.Song, st StreamSettings) (subsonic.StreamOptions, audio.Format, bool) {
	f := audio.FormatFromSuffix(s.Suffix)
	if f == audio.FormatUnknown {
		f = contentTypes[strings.ToLower(s.ContentType)]
	}
	if f != audio.FormatUnknown {
		return subsonic.StreamOptions{Format: "raw"}, f, false
	}
	o := subsonic.StreamOptions{Format: st.TranscodeFormat}
	if st.TranscodeFormat == "mp3" {
		o.MaxBitRate = st.TranscodeBitrate
	}
	return o, audio.FormatFromSuffix(st.TranscodeFormat), true
}

// NewOpener opens songs through the Subsonic stream endpoint.
func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
	if st.PrefetchBytes <= 0 {
		st.PrefetchBytes = 4 << 20
	}
	return func(ctx context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error) {
		so, f, transcoded := PlanStream(s, st)
		if f == audio.FormatUnknown {
			return Opened{}, fmt.Errorf("player: transcode format %q can't be decoded", st.TranscodeFormat)
		}
		var start time.Duration
		if transcoded && offset > 0 {
			so.TimeOffset = int(offset / time.Second)
			start = time.Duration(so.TimeOffset) * time.Second
		}
		o := stream.Options{Client: c.HTTPClient(), CheckResponse: subsonic.CheckStreamResponse, WindowBytes: st.WindowBytes}
		if prefetch {
			o.PrefetchBytes = st.PrefetchBytes
		}
		r, err := stream.Open(ctx, c.StreamURL(s.ID, so), o)
		if err != nil {
			return Opened{}, err
		}
		var src io.ReadSeeker = r
		if !transcoded && offset > 0 && f == audio.FormatMP3 {
			// An MP3 without a seek table seeks by decoding from the start,
			// which takes seconds on the MiSTer: start near the target instead.
			if base, ok := mp3Offset(r, s, offset); ok {
				if _, err := r.Seek(base, io.SeekStart); err == nil {
					src, start = &fromOffset{r: r, base: base}, offset
				}
			} else {
				r.Seek(0, io.SeekStart)
			}
		}
		return Opened{Source: src, Format: f, Transcoded: transcoded, Offset: start}, nil
	}
}

// mp3Offset estimates where offset falls in the MP3 r of s.Size bytes lasting
// s.Duration: after the ID3v2 tag, in proportion to time. That is exact for
// a constant bitrate and close for a variable one. It reads the first bytes
// of r to find the tag.
func mp3Offset(r io.Reader, s subsonic.Song, offset time.Duration) (int64, bool) {
	d := time.Duration(s.Duration) * time.Second
	if s.Size <= 0 || d <= 0 {
		return 0, false
	}
	var h [10]byte
	tag := int64(0)
	if _, err := io.ReadFull(r, h[:]); err == nil && string(h[:3]) == "ID3" {
		size := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
		tag = 10 + size
		if h[5]&0x10 != 0 {
			tag += 10 // a footer
		}
	}
	if tag >= s.Size {
		return 0, false
	}
	off := tag + int64(float64(s.Size-tag)*float64(offset)/float64(d))
	return min(off, s.Size-1), true
}

// fromOffset shows a stream from byte base on as if it began there, so the
// decoder starts at the estimated seek point (MP3 frames resync on their own).
type fromOffset struct {
	r    *stream.Reader
	base int64
}

func (f *fromOffset) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *fromOffset) Close() error               { return f.r.Close() }
func (f *fromOffset) Promote()                   { f.r.Promote() }

func (f *fromOffset) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		off += f.base
	}
	abs, err := f.r.Seek(off, whence)
	return abs - f.base, err
}

// ReplayGainFactor converts the song's ReplayGain to a linear factor for
// mode "track" or "album", falling back to the other value, and limits it
// so the known peak doesn't clip. No data or mode "off" gives 1.
func ReplayGainFactor(s subsonic.Song, mode string) float32 {
	rg := s.ReplayGain
	if rg == nil || mode == "off" || mode == "" {
		return 1
	}
	gain, peak := rg.TrackGain, rg.TrackPeak
	if mode == "album" {
		gain, peak = rg.AlbumGain, rg.AlbumPeak
	}
	if gain == nil {
		gain, peak = rg.AlbumGain, rg.AlbumPeak
		if mode == "album" {
			gain, peak = rg.TrackGain, rg.TrackPeak
		}
	}
	if gain == nil {
		return 1
	}
	f := math.Pow(10, *gain/20)
	if peak != nil && *peak > 0 && f**peak > 1 {
		f = 1 / *peak
	}
	return float32(f)
}
