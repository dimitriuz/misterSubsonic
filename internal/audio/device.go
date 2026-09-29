package audio

/*
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// OutputRate is the fixed device rate. MiSTer's ALSA default device
// resamples to 48 kHz linearly (and crackles), so we always hand it 48 kHz.
const OutputRate = 48000

// Output is where the engine writes interleaved stereo float32 at OutputRate.
type Output interface {
	// Write copies as many frames as fit and returns how many were taken.
	Write(frames []float32) int
	// Consumed is the running total of frames taken out of the ring by the
	// device, including frames discarded by Flush.
	Consumed() uint64
	SetPaused(paused bool)
	SetVolume(linear float32)
	// Flush discards everything buffered. After it returns, Consumed equals
	// the total number of frames ever written.
	Flush()
	Close() error
}

type DeviceOptions struct {
	// Name selects a playback device by name or ALSA id; "" means default.
	Name string
	// Null uses miniaudio's null backend (for tests and headless runs).
	Null bool
	// RingFrames is the ring size; 0 means 500 ms.
	RingFrames int
}

type device struct{}

// OpenDevice opens the single global playback device.
func OpenDevice(o DeviceOptions) (Output, error) {
	ring := o.RingFrames
	if ring == 0 {
		ring = OutputRate / 2
	}
	var name *C.char
	if o.Name != "" {
		name = C.CString(o.Name)
		defer C.free(unsafe.Pointer(name))
	}
	null := 0
	if o.Null {
		null = 1
	}
	if rc := C.mss_device_open(name, C.int(null), C.uint32_t(ring)); rc != 0 {
		return nil, fmt.Errorf("audio: open device: miniaudio result %d", int(rc))
	}
	return device{}, nil
}

func (device) Write(frames []float32) int {
	if len(frames) < 2 {
		return 0
	}
	return int(C.mss_device_write((*C.float)(unsafe.Pointer(&frames[0])), C.uint32_t(len(frames)/2)))
}

func (device) Consumed() uint64 { return uint64(C.mss_device_consumed()) }

func (device) SetPaused(p bool) {
	v := 0
	if p {
		v = 1
	}
	C.mss_device_set_paused(C.int(v))
}

func (device) SetVolume(v float32) { C.mss_device_set_volume(C.float(v)) }
func (device) Flush()              { C.mss_device_flush() }
func (device) Close() error        { C.mss_device_close(); return nil }

// stopDeviceForTest is a test hook: it stops the device callback, as a
// failed or unplugged device would, without closing the device.
func stopDeviceForTest() { C.mss_device_stop_for_test() }
