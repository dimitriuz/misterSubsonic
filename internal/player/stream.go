package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync/atomic"
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
		if !transcoded && f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0 {
			// An MP3 without a seek table seeks by decoding from the start,
			// which takes seconds on the MiSTer: start near the target instead.
			layout, base, at, placed, err := positionMP3(r, s, offset)
			if err != nil {
				r.Close()
				return Opened{}, err
			}
			if placed {
				m := &mp3Stream{r: r, layout: layout}
				m.refs.Store(1)
				src = &fromOffset{s: m, base: base, placed: true}
				start = at
			}
		}
		return Opened{Source: src, Format: f, Transcoded: transcoded, Offset: start}, nil
	}
}

// positionMP3 probes r, an MP3 of song s, and moves it to the byte estimated
// for offset (the start for offset 0). placed says the estimate is in use: r
// is at base and the song's position there is at. Otherwise r is back at its
// start, for the engine to seek; err is set only when r can't be put there.
func positionMP3(r io.ReadSeeker, s subsonic.Song, offset time.Duration) (layout mp3Layout, base int64, at time.Duration, placed bool, err error) {
	layout = probeMP3(r)
	ok := false
	if offset > 0 {
		base, ok = layout.offset(s, offset)
	}
	if _, err := r.Seek(base, io.SeekStart); err == nil && (ok || offset == 0) {
		if ok {
			at = offset
		}
		return layout, base, at, true, nil
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return layout, 0, 0, false, err
	}
	return layout, 0, 0, false, nil
}

// mp3Stream is an MP3's stream shared by the sources of successive seeks:
// it closes with the last of them.
type mp3Stream struct {
	r      *stream.Reader
	layout mp3Layout
	refs   atomic.Int32
}

// fromOffset shows an mp3Stream from byte base on as if it began there, so
// the decoder starts at the estimated seek point (MP3 frames resync on their
// own).
type fromOffset struct {
	s      *mp3Stream
	base   int64
	placed bool // the stream is at base or beyond; only the engine's goroutine touches it
	closed atomic.Bool
}

// place moves a source made by retarget to its base on first use, when the
// source before it has been dropped and no longer reads.
func (f *fromOffset) place() error {
	if f.closed.Load() {
		return stream.ErrClosed
	}
	if !f.placed {
		f.placed = true
		if !f.s.r.SeekIfBuffered(f.base) {
			_, err := f.s.r.Seek(f.base, io.SeekStart)
			return err
		}
	}
	return nil
}

func (f *fromOffset) Read(p []byte) (int, error) {
	if err := f.place(); err != nil {
		return 0, err
	}
	return f.s.r.Read(p)
}

func (f *fromOffset) Close() error {
	if f.closed.CompareAndSwap(false, true) && f.s.refs.Add(-1) == 0 {
		return f.s.r.Close()
	}
	return nil
}

func (f *fromOffset) Promote() { f.s.r.Promote() }

func (f *fromOffset) Seek(off int64, whence int) (int64, error) {
	if err := f.place(); err != nil {
		return 0, err
	}
	// The source begins at base: a seek before it is refused, and refused
	// before anything moves.
	var target int64
	switch whence {
	case io.SeekStart:
		target = off
		off += f.base
	case io.SeekCurrent:
		cur, err := f.s.r.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		target = cur - f.base + off
	case io.SeekEnd:
		target = f.s.r.Size() + off - f.base // an unknown size, -1, is refused here too
	}
	if target < 0 {
		return 0, errors.New("player: seek before the start of the stream")
	}
	abs, err := f.s.r.Seek(off, whence)
	return abs - f.base, err
}

// retargeter is a source that can move to another position of its stream
// without opening it again.
type retargeter interface {
	// retarget returns the song at position at as a new Opened over the same
	// stream, if the stream holds that byte. The old source stays valid until
	// closed. prep, run right before the engine swaps the two, moves the
	// stream so a read of the old source that is waiting for data wakes.
	retarget(s subsonic.Song, at time.Duration) (o Opened, prep func(), ok bool)
}

func (f *fromOffset) retarget(s subsonic.Song, at time.Duration) (Opened, func(), bool) {
	base, ok := f.s.layout.offset(s, at)
	if !ok || !f.s.r.Holds(base) {
		return Opened{}, nil, false
	}
	f.s.refs.Add(1)
	return Opened{Source: &fromOffset{s: f.s, base: base}, Format: audio.FormatMP3, Offset: at},
		// If the window moved on since, SeekIfBuffered does nothing: harmless,
		// place() falls back to a plain Seek.
		func() { f.s.r.SeekIfBuffered(base) }, true
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
