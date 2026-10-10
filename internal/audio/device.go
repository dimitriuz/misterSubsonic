package audio

/*
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// OutputRate is the fixed device rate. MiSTer's ALSA default device
// resamples to 48 kHz linearly (and crackles), so a fixed device always gets
// 48 kHz. A native device runs at the music's rate instead (see Native).
const OutputRate = 48000

// Output is where the engine writes interleaved stereo float32 at Rate().
//
// Threading: Write, Flush and Reopen must be called from one goroutine (the
// engine's run goroutine is the only caller). Consumed, SetPaused, SetVolume,
// Rate, Supports and Lost may be called from any goroutine.
type Output interface {
	// Write copies as many frames as fit and returns how many were taken.
	Write(frames []float32) int
	// Consumed is the running total of frames taken out of the ring by the
	// device, including frames discarded by Flush. It keeps counting across
	// Reopen.
	Consumed() uint64
	SetPaused(paused bool)
	SetVolume(linear float32)
	// Flush discards everything buffered. After it returns, Consumed equals
	// the total number of frames ever written.
	Flush()
	Close() error
	// Rate is the rate the device runs at now.
	Rate() int
	// Supports reports whether the device may run at rate: always for a
	// native device until a Reopen found it doesn't, only OutputRate for
	// the others.
	Supports(rate int) bool
	// Reopen closes the device, once what it holds has played, and opens
	// name at rate. Pause and volume carry over. On error the device is
	// closed and the error is the caller's signal (Lost is not set by it).
	Reopen(name string, rate int) error
	// Lost reports that the device stopped without being asked to (it was
	// unplugged, say) or, for a native device, that its card is gone.
	Lost() bool
}

// ErrRateUnsupported: a native device can't run at the rate asked for.
var ErrRateUnsupported = errors.New("audio: the device does not take this sample rate")

// Native reports whether name is a native device: an ALSA hw: name, opened as
// is, run at the music's own rate.
func Native(name string) bool { return strings.HasPrefix(name, "hw:") }

type DeviceOptions struct {
	// Name selects a playback device by name or ALSA id; "" means default.
	// A hw: name (see Native) is opened as is.
	Name string
	// Null uses miniaudio's null backend (for tests and headless runs).
	Null bool
	// Rate is the starting rate of a native device; 0 means OutputRate.
	// Other devices always run at OutputRate.
	Rate int
	// RingFrames is the ring size; 0 means 500 ms at the device's rate.
	RingFrames int
}

// device is the single global playback device. Its methods may be called
// from several goroutines; mu orders Reopen against the rest.
type device struct {
	mu   sync.Mutex
	null bool
	ring int // RingFrames as asked; 0: half a second at each rate

	name    string
	refused map[string]map[int]bool // per device name, the rates it didn't take
	checked time.Time               // when Lost last looked for the card
	gone    bool                    // the card wasn't there then

	linear float32   // the volume asked for
	hw     *hwVolume // the native device's hardware volume, if it has one
	hwCard string    // the card hw drives
}

// OpenDevice opens the single global playback device.
func OpenDevice(o DeviceOptions) (Output, error) {
	d := &device{null: o.Null, ring: o.RingFrames, refused: map[string]map[int]bool{}, linear: 1}
	rate := deviceRate(o.Name, o.Rate)
	if err := d.open(o.Name, rate, false); err != nil {
		return nil, err
	}
	return d, nil
}

func deviceRate(name string, rate int) int {
	if !Native(name) || rate <= 0 {
		return OutputRate
	}
	return rate
}

func (d *device) ringFrames(rate int) int {
	if d.ring > 0 {
		return d.ring
	}
	return rate / 2
}

// openMixer is the hardware volume for a card (a variable for tests).
var openMixer = openALSAMixer

// useMixer points the volume at name's card: its hardware volume for a
// native device that has one, software otherwise. The level is set before
// the device starts, so a DAC never plays a moment at its old level.
func (d *device) useMixer(name string) {
	card := ""
	if Native(name) && !d.null {
		card = mixerCard(name)
	}
	if card != d.hwCard {
		if d.hw != nil {
			closeALSAMixer()
		}
		d.hw, d.hwCard = nil, card
		switch {
		case card == "":
		case !cardPresent(name):
			d.hwCard = "" // not plugged in: the open fails anyway; try again next time
		default:
			ctl, err := openMixer(card)
			if err != nil {
				log.Printf("%v: the volume stays in software", err)
			} else {
				// muted: the switch may have been left off; the first level
				// set turns it on.
				d.hw = &hwVolume{ctl: ctl, muted: ctl.HasSwitch()}
			}
		}
	}
	d.applyVolume()
}

// applyVolume sets d.linear on the hardware control, if any, and the rest
// in software.
func (d *device) applyVolume() {
	soft := d.linear
	if d.hw != nil {
		s, err := d.hw.apply(d.linear)
		if err != nil {
			log.Printf("%v: the volume goes back to software", err)
			closeALSAMixer()
			d.hw = nil
		} else {
			soft = s
		}
	}
	C.mss_device_set_volume(C.float(soft))
}

// open opens name at rate; reopen: close the open device first.
func (d *device) open(name string, rate int, reopen bool) error {
	d.useMixer(name)
	var cname *C.char
	if name != "" {
		cname = C.CString(name)
		defer C.free(unsafe.Pointer(cname))
	}
	null, native := 0, 0
	if d.null {
		null = 1
	}
	if Native(name) && !d.null {
		native = 1
	}
	var rc C.int
	start := time.Now()
	if reopen {
		rc = C.mss_device_reopen(cname, C.int(null), C.uint32_t(rate), C.uint32_t(d.ringFrames(rate)), C.int(native), 1)
	} else {
		rc = C.mss_device_open(cname, C.int(null), C.uint32_t(rate), C.uint32_t(d.ringFrames(rate)), C.int(native))
	}
	switch {
	case rc == C.MSS_RATE_UNSUPPORTED:
		if d.refused[name] == nil {
			d.refused[name] = map[int]bool{}
		}
		d.refused[name][rate] = true
		return fmt.Errorf("%w: %d Hz", ErrRateUnsupported, rate)
	case rc != 0:
		return fmt.Errorf("audio: open device: miniaudio result %d", int(rc))
	}
	d.name, d.gone, d.checked = name, false, time.Time{}
	if native == 1 {
		log.Printf("audio: %s at %d Hz, %d-bit (opened in %d ms)", name, rate, int(C.mss_device_format_bits()), time.Since(start).Milliseconds())
	}
	return nil
}

func (d *device) Reopen(name string, rate int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.open(name, deviceRate(name, rate), true)
}

func (d *device) Rate() int { return int(C.mss_device_rate()) }

func (d *device) Supports(rate int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !Native(d.name) || d.null {
		return rate == OutputRate
	}
	return !d.refused[d.name][rate]
}

// FormatBits is the sample size the device takes: 32 for float or S32.
func (d *device) FormatBits() int { return int(C.mss_device_format_bits()) }

// cardCheckEvery spaces the checks that a native device's card is still
// there.
const cardCheckEvery = 200 * time.Millisecond

// Lost reports a device that stopped by itself, or a native device whose
// card went away. miniaudio doesn't stop an ALSA device whose card is
// unplugged (its wait loop spins on the error, and no stop notification
// comes), and asking ALSA for the state from here would race with the
// audio thread, so it looks for the card in /proc/asound instead.
func (d *device) Lost() bool {
	if C.mss_device_lost() != 0 {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !Native(d.name) || d.null {
		return false
	}
	if now := time.Now(); now.Sub(d.checked) >= cardCheckEvery {
		d.checked = now
		d.gone = !cardPresent(d.name)
	}
	return d.gone
}

// cardPresent reports whether a native device's card is in /proc/asound:
// "/proc/asound/E30" for hw:CARD=E30, "/proc/asound/card1" for hw:1.
var cardPresent = func(device string) bool {
	id := CardID(device)
	if _, err := strconv.Atoi(id); err == nil {
		id = "card" + id
	}
	_, err := os.Stat("/proc/asound/" + id)
	return err == nil
}

func (d *device) Write(frames []float32) int {
	if len(frames) < 2 {
		return 0
	}
	return int(C.mss_device_write((*C.float)(unsafe.Pointer(&frames[0])), C.uint32_t(len(frames)/2)))
}

func (d *device) Consumed() uint64 { return uint64(C.mss_device_consumed()) }

func (d *device) SetPaused(p bool) {
	v := 0
	if p {
		v = 1
	}
	C.mss_device_set_paused(C.int(v))
}

func (d *device) SetVolume(v float32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.linear = v
	d.applyVolume()
}
func (d *device) Flush() { C.mss_device_flush() }
func (d *device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	C.mss_device_close()
	if d.hw != nil {
		closeALSAMixer()
		d.hw, d.hwCard = nil, ""
	}
	return nil
}

// stopDeviceForTest is a test hook: it stops the device callback, as a
// failed or unplugged device would, without closing the device.
func stopDeviceForTest() { C.mss_device_stop_for_test() }

// convertSamples converts float samples to bits (16, 24 packed or 32) of
// integer, or copies them for bits 0; the device's conversion, for tests.
func convertSamples(src []float32, bits int) []byte {
	per := bits / 8
	if bits == 0 {
		per = 4
	}
	dst := make([]byte, len(src)*per)
	if len(src) > 0 {
		C.mss_convert_samples(unsafe.Pointer(&dst[0]), C.int(bits), (*C.float)(unsafe.Pointer(&src[0])), C.uint32_t(len(src)))
	}
	return dst
}
