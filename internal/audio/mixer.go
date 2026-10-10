package audio

/*
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

// hwControl is a card's playback volume in dB.
type hwControl interface {
	// Range is the control's lowest and highest level, in dB.
	Range() (min, max float64)
	// Set moves the control to the nearest step at or above db and returns
	// the level it got.
	Set(db float64) (got float64, err error)
	HasSwitch() bool
	SetSwitch(on bool) error
}

// hwVolume drives a hardware control from the app's linear volume.
type hwVolume struct {
	ctl   hwControl
	muted bool // the switch is off
}

// apply sets the control for linear and returns the software gain still
// to apply: 1 when the hardware reaches the level exactly, below 1 to make
// up a step the hardware can't hit, 0 when muted.
func (h *hwVolume) apply(linear float32) (float32, error) {
	lo, hi := h.ctl.Range()
	if linear <= 0 {
		if h.ctl.HasSwitch() {
			if err := h.ctl.SetSwitch(false); err != nil {
				return 0, err
			}
			h.muted = true
		} else if _, err := h.ctl.Set(lo); err != nil {
			return 0, err
		}
		return 0, nil
	}
	// To the hundredth of a dB, ALSA's unit: -60 dB must not arrive as
	// -59.99999996 and round up a step.
	want := math.Round(2000*math.Log10(float64(linear))) / 100
	got, err := h.ctl.Set(math.Max(lo, math.Min(hi, want)))
	if err != nil {
		return 0, err
	}
	if h.muted { // the level first, then sound: no blip at the old level
		if err := h.ctl.SetSwitch(true); err != nil {
			return 0, err
		}
		h.muted = false
	}
	soft := math.Pow(10, (want-got)/20)
	if soft > 1 {
		soft = 1 // the control can't go lower than got; never amplify
	}
	return float32(soft), nil
}

// mixerCard is the ALSA mixer name for a native device's card:
// "hw:CARD=E30,DEV=0" and "hw:E30,0" give "hw:E30".
func mixerCard(device string) string { return "hw:" + CardID(device) }

// alsaMixer is the C mixer: one card at a time.
type alsaMixer struct{}

func openALSAMixer(card string) (hwControl, error) {
	c := C.CString(card)
	defer C.free(unsafe.Pointer(c))
	if rc := C.mss_mixer_open(c); rc != 0 {
		return nil, fmt.Errorf("audio: no hardware volume on %s (%d)", card, int(rc))
	}
	return alsaMixer{}, nil
}

func (alsaMixer) Range() (float64, float64) {
	var lo, hi C.long
	C.mss_mixer_range(&lo, &hi)
	return float64(lo) / 100, float64(hi) / 100
}

func (alsaMixer) Set(db float64) (float64, error) {
	var got C.long
	if rc := C.mss_mixer_set(C.long(math.Round(db*100)), &got); rc < 0 {
		return 0, fmt.Errorf("audio: set hardware volume: %d", int(rc))
	}
	return float64(got) / 100, nil
}

// get reads the level now (tests and probes).
func (alsaMixer) get() (float64, error) {
	var v C.long
	if rc := C.mss_mixer_get(&v); rc < 0 {
		return 0, fmt.Errorf("audio: read hardware volume: %d", int(rc))
	}
	return float64(v) / 100, nil
}

func (alsaMixer) HasSwitch() bool { return C.mss_mixer_has_switch() != 0 }

func (alsaMixer) SetSwitch(on bool) error {
	v := 0
	if on {
		v = 1
	}
	if rc := C.mss_mixer_set_switch(C.int(v)); rc < 0 {
		return fmt.Errorf("audio: set hardware mute: %d", int(rc))
	}
	return nil
}

func closeALSAMixer() { C.mss_mixer_close() }
