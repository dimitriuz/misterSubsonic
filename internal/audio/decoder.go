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
	err error
}

type cDecoder struct {
	d      *C.mss_decoder
	h      cgo.Handle
	src    *source
	rate   int
	length uint64
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
		return nil, fmt.Errorf("audio: open %s decoder: miniaudio result %d", f, int(rc))
	}
	var info C.mss_decoder_info
	C.mss_decoder_get_info(d, &info)
	return &cDecoder{d: d, h: h, src: s, rate: int(info.sample_rate), length: uint64(info.length_frames)}, nil
}

func (c *cDecoder) SampleRate() int      { return c.rate }
func (c *cDecoder) LengthFrames() uint64 { return c.length }

func (c *cDecoder) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	var got C.uint64_t
	rc := C.mss_decoder_read(c.d, (*C.float)(unsafe.Pointer(&dst[0])), C.uint64_t(frames), &got)
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
	if rc := C.mss_decoder_seek(c.d, C.uint64_t(frame)); rc != 0 {
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
		s.err = err
		return 1
	}
	return 0
}
