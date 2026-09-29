package audio

/*
#include "shim.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"runtime/cgo"
	"unsafe"
)

// Decoder produces interleaved stereo float32 frames at the source's native sample rate.
type Decoder interface {
	SampleRate() int
	// LengthFrames is the total length in frames, or 0 when unknown.
	LengthFrames() uint64
	// Read fills dst (len must be even) and returns the number of frames
	// written. It returns io.EOF (with 0 frames) at the end of the stream.
	Read(dst []float32) (int, error)
	SeekFrame(frame uint64) error
	Close() error
}

// OpenDecoderFunc is how the engine opens decoders; tests substitute fakes.
type OpenDecoderFunc func(src io.ReadSeeker, f Format) (Decoder, error)

const maAtEnd = -17 // MA_AT_END

type source struct {
	r   io.ReadSeeker
	err error // last read error; sticky, returned by Read
	// seekErr is the last seek error. It is kept apart from err because
	// miniaudio probes seeks it can live without (dr_mp3 asks for SeekEnd at
	// open, which a chunked transcode with no Content-Length can't answer).
	seekErr error
}

type cDecoder struct {
	d         *C.mss_decoder
	h         cgo.Handle
	src       *source
	rate      int
	length    uint64
	lengthSet bool
	got       C.uint64_t // frames of the last Read; a field so passing &got to C doesn't heap-allocate per call
}

// OpenDecoder opens a miniaudio decoder that pulls bytes from src.
func OpenDecoder(src io.ReadSeeker, f Format) (Decoder, error) {
	s := &source{r: src}
	h := cgo.NewHandle(s)
	var rc C.int
	d := C.mss_decoder_open(C.uintptr_t(h), C.int(f), &rc)
	if d == nil {
		h.Delete()
		if s.err != nil {
			return nil, fmt.Errorf("audio: open %s decoder: %w", f, s.err)
		}
		if s.seekErr != nil {
			return nil, fmt.Errorf("audio: open %s decoder: %w", f, s.seekErr)
		}
		return nil, fmt.Errorf("audio: open %s decoder: miniaudio result %d", f, int(rc))
	}
	var info C.mss_decoder_info
	C.mss_decoder_get_info(d, &info)
	return &cDecoder{d: d, h: h, src: s, rate: int(info.sample_rate)}, nil
}

func (c *cDecoder) SampleRate() int { return c.rate }

// LengthFrames is computed on first use and cached. For an MP3 without a
// Xing header miniaudio reads the whole stream to answer (then seeks back),
// so the engine never calls it; tools working on local files may.
func (c *cDecoder) LengthFrames() uint64 {
	if !c.lengthSet && c.d != nil {
		c.length = uint64(C.mss_decoder_length(c.d))
		c.lengthSet = true
	}
	return c.length
}

func (c *cDecoder) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	c.got = 0
	rc := C.mss_decoder_read(c.d, (*C.float)(unsafe.Pointer(&dst[0])), C.uint64_t(frames), &c.got)
	got := c.got
	if c.src.err != nil {
		return int(got), c.src.err
	}
	if got == 0 {
		if rc == maAtEnd || rc == 0 {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("audio: decode: miniaudio result %d", int(rc))
	}
	return int(got), nil
}

func (c *cDecoder) SeekFrame(frame uint64) error {
	c.src.seekErr = nil
	if rc := C.mss_decoder_seek(c.d, C.uint64_t(frame)); rc != 0 {
		if c.src.seekErr != nil {
			return c.src.seekErr
		}
		if c.src.err != nil {
			return c.src.err
		}
		return fmt.Errorf("audio: seek: miniaudio result %d", int(rc))
	}
	return nil
}

func (c *cDecoder) Close() error {
	if c.d == nil {
		return errors.New("audio: decoder already closed")
	}
	C.mss_decoder_close(c.d)
	c.d = nil
	c.h.Delete()
	return nil
}

//export mssGoRead
func mssGoRead(h C.uintptr_t, buf unsafe.Pointer, n C.size_t, nread *C.size_t) C.int {
	s := cgo.Handle(h).Value().(*source)
	dst := unsafe.Slice((*byte)(buf), int(n))
	total := 0
	for total < len(dst) {
		k, err := s.r.Read(dst[total:])
		total += k
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.err = err
			*nread = C.size_t(total)
			return 1
		}
	}
	*nread = C.size_t(total)
	return 0
}

//export mssGoSeek
func mssGoSeek(h C.uintptr_t, offset C.int64_t, whence C.int) C.int {
	s := cgo.Handle(h).Value().(*source)
	if _, err := s.r.Seek(int64(offset), int(whence)); err != nil {
		s.seekErr = err
		return 1
	}
	return 0
}
