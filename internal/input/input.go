// Package input turns controllers and keyboards into UI buttons: it reads
// evdev devices (grabbing them so MiSTer Main doesn't react), applies the
// user's MiSTer controller maps, and generates key repeat for navigation.
package input

import "time"

// Button is a logical controller button.
type Button int

const (
	BtnNone Button = iota
	BtnUp
	BtnDown
	BtnLeft
	BtnRight
	BtnA
	BtnB
	BtnX
	BtnY
	BtnL
	BtnR
	BtnSelect
	BtnStart
)

var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start"}

func (b Button) String() string {
	if int(b) < len(buttonNames) {
		return buttonNames[b]
	}
	return "?"
}

// Kind is what happened to a button.
type Kind int

const (
	Press Kind = iota + 1
	Release
	Repeat // generated while a navigation button is held
)

// Event is one button transition.
type Event struct {
	Button Button
	Kind   Kind
}

// Repeat timing (spec §8.4).
const (
	RepeatDelay = 350 * time.Millisecond
	RepeatRate  = 110 * time.Millisecond
	RepeatFast  = 45 * time.Millisecond
	FastAfter   = 6 // repeats before switching to RepeatFast
)

// repeats reports whether b auto-repeats when held.
func repeats(b Button) bool {
	switch b {
	case BtnUp, BtnDown, BtnLeft, BtnRight, BtnL, BtnR:
		return true
	}
	return false
}

// Repeater generates Repeat events for held navigation buttons. It is a pure
// state machine driven by the caller's clock: Feed each Press/Release, call
// Due whenever NextDeadline passes.
type Repeater struct {
	held  Button
	next  time.Time
	count int
}

// Feed records a press or release. Pressing another button replaces the held one.
func (r *Repeater) Feed(e Event, now time.Time) {
	switch e.Kind {
	case Press:
		if repeats(e.Button) {
			r.held, r.next, r.count = e.Button, now.Add(RepeatDelay), 0
		} else {
			r.held = BtnNone
		}
	case Release:
		if e.Button == r.held {
			r.held = BtnNone
		}
	}
}

// NextDeadline is when Due should be called next (zero if nothing is held).
func (r *Repeater) NextDeadline() time.Time {
	if r.held == BtnNone {
		return time.Time{}
	}
	return r.next
}

// Due returns the repeat that is due at now, if any. It emits at most one
// repeat per call: after a stall or clock jump the schedule restarts from now
// instead of bursting the missed repeats.
func (r *Repeater) Due(now time.Time) []Event {
	if r.held == BtnNone || now.Before(r.next) {
		return nil
	}
	r.count++
	step := RepeatRate
	if r.count >= FastAfter {
		step = RepeatFast
	}
	r.next = r.next.Add(step)
	if !r.next.After(now) {
		r.next = now.Add(step)
	}
	return []Event{{Button: r.held, Kind: Repeat}}
}
