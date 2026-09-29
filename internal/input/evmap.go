package input

import (
	"encoding/binary"
	"fmt"
)

// Linux input event types and codes used here.
const (
	evKey = 0x01
	evAbs = 0x03

	absX     = 0x00
	absY     = 0x01
	absHat0X = 0x10
	absHat0Y = 0x11
)

// Keyboard and gamepad key codes (linux/input-event-codes.h).
const (
	keyEsc       = 1
	keyBackspace = 14
	keyTab       = 15
	keyQ         = 16
	keyEnter     = 28
	keyN         = 49
	keySpace     = 57
	keyPageUp    = 104
	keyUp        = 103
	keyLeft      = 105
	keyRight     = 106
	keyPageDown  = 109
	keyDown      = 108
	keyKPEnter   = 96

	btnSouth  = 0x130
	btnEast   = 0x131
	btnNorth  = 0x133
	btnWest   = 0x134
	btnTL     = 0x136
	btnTR     = 0x137
	btnSelect = 0x13a
	btnStart  = 0x13b
	btnDpadUp = 0x220
	btnDpadDn = 0x221
	btnDpadL  = 0x222
	btnDpadR  = 0x223
)

// DefaultKeys is the built-in key-code map: keyboard (spec §8.3) plus Linux
// gamepad defaults. SNES layout: A is the east face button, B the south one.
var DefaultKeys = map[uint16]Button{
	keyUp: BtnUp, keyDown: BtnDown, keyLeft: BtnLeft, keyRight: BtnRight,
	keyEnter: BtnA, keyKPEnter: BtnA, keyEsc: BtnB, keyBackspace: BtnB,
	keyTab: BtnX, keySpace: BtnStart, keyPageUp: BtnL, keyPageDown: BtnR,
	keyN: BtnY, keyQ: BtnY,

	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
	btnDpadUp: BtnUp, btnDpadDn: BtnDown, btnDpadL: BtnLeft, btnDpadR: BtnRight,
}

// MiSTer .map slots (Main_MiSTer): 32 little-endian uint32, low 16 bits = key code.
var misterSlots = [...]Button{BtnRight, BtnLeft, BtnDown, BtnUp, BtnA, BtnB, BtnX, BtnY, BtnL, BtnR, BtnSelect, BtnStart}

// ParseMisterMap reads an input_VID_PID_v3.map file into a key-code map.
// Slots without a key code (0, or axis entries above 0x2ff) are skipped;
// directions on axes/hats are handled generically.
func ParseMisterMap(b []byte) (map[uint16]Button, error) {
	if len(b) < 4*len(misterSlots) {
		return nil, fmt.Errorf("input: map file too short (%d bytes)", len(b))
	}
	m := map[uint16]Button{}
	for i, btn := range misterSlots {
		code := uint16(binary.LittleEndian.Uint32(b[4*i:]) & 0xFFFF)
		if code != 0 && code <= 0x2ff {
			m[code] = btn
		}
	}
	return m, nil
}

// AbsRange is an absolute axis's calibration (from EVIOCGABS).
type AbsRange struct{ Min, Max int32 }

// translator converts one device's raw events into button events.
type translator struct {
	keys  map[uint16]Button
	abs   map[uint16]AbsRange
	state map[uint16]int // axis -> -1, 0, +1
}

func newTranslator(keys map[uint16]Button, abs map[uint16]AbsRange) *translator {
	return &translator{keys: keys, abs: abs, state: map[uint16]int{}}
}

// handle converts a raw event. value: 1 press, 0 release, 2 kernel autorepeat (ignored).
func (t *translator) handle(typ, code uint16, value int32) []Event {
	switch typ {
	case evKey:
		b, ok := t.keys[code]
		if !ok {
			b, ok = DefaultKeys[code]
		}
		if !ok || value == 2 {
			return nil
		}
		if value == 1 {
			return []Event{{b, Press}}
		}
		return []Event{{b, Release}}
	case evAbs:
		var neg, pos Button
		switch code {
		case absX, absHat0X:
			neg, pos = BtnLeft, BtnRight
		case absY, absHat0Y:
			neg, pos = BtnUp, BtnDown
		default:
			return nil
		}
		dir := 0
		if code == absHat0X || code == absHat0Y {
			if value < 0 {
				dir = -1
			} else if value > 0 {
				dir = 1
			}
		} else {
			r, ok := t.abs[code]
			if !ok || r.Max <= r.Min {
				return nil
			}
			center, half := (r.Min+r.Max)/2, (r.Max-r.Min)/2
			if value <= center-half/2 {
				dir = -1
			} else if value >= center+half/2 {
				dir = 1
			}
		}
		prev := t.state[code]
		if dir == prev {
			return nil
		}
		t.state[code] = dir
		var out []Event
		switch prev {
		case -1:
			out = append(out, Event{neg, Release})
		case 1:
			out = append(out, Event{pos, Release})
		}
		switch dir {
		case -1:
			out = append(out, Event{neg, Press})
		case 1:
			out = append(out, Event{pos, Press})
		}
		return out
	}
	return nil
}
