package audio

/*
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// DefaultResampleQuality is the speexdsp quality level (0-10).
const DefaultResampleQuality = 5

// Resampler converts interleaved stereo float32 between sample rates.
// Feeding consecutive tracks of the same rate through one Resampler is
// seamless, which is what keeps gapless playback gapless.
type Resampler struct {
	r       *C.mss_resampler
	inRate  int
	outRate int
}

func NewResampler(inRate, outRate, quality int) (*Resampler, error) {
	var rc C.int
	r := C.mss_resampler_new(C.uint32_t(inRate), C.uint32_t(outRate), C.int(quality), &rc)
	if r == nil {
		return nil, fmt.Errorf("audio: resampler %d->%d: speex error %d", inRate, outRate, int(rc))
	}
	return &Resampler{r: r, inRate: inRate, outRate: outRate}, nil
}

func (r *Resampler) InRate() int { return r.inRate }

// Process resamples all of in and appends the output to out.
func (r *Resampler) Process(in []float32, out []float32) []float32 {
	for len(in) >= 2 {
		inFrames := len(in) / 2
		want := inFrames*r.outRate/r.inRate + 64
		if cap(out)-len(out) < want*2 {
			grown := make([]float32, len(out), len(out)+want*2)
			copy(grown, out)
			out = grown
		}
		dst := out[len(out):cap(out)]
		cin := C.uint32_t(inFrames)
		cout := C.uint32_t(len(dst) / 2)
		C.mss_resampler_process(r.r, (*C.float)(unsafe.Pointer(&in[0])), &cin, (*C.float)(unsafe.Pointer(&dst[0])), &cout)
		out = out[:len(out)+int(cout)*2]
		in = in[int(cin)*2:]
		if cin == 0 && cout == 0 {
			break
		}
	}
	return out
}

// Flush pushes the resampler's buffered tail out by feeding silence
// equal to its input latency, and appends the output to out.
func (r *Resampler) Flush(out []float32) []float32 {
	lat := int(C.mss_resampler_input_latency(r.r))
	return r.Process(make([]float32, lat*2), out)
}

func (r *Resampler) Close() {
	if r.r != nil {
		C.mss_resampler_free(r.r)
		r.r = nil
	}
}
