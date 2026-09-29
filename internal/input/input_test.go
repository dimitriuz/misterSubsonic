package input

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestRepeaterTiming(t *testing.T) {
	var r Repeater
	t0 := time.Unix(1000, 0)
	r.Feed(Event{BtnDown, Press}, t0)
	if got := r.Due(t0.Add(349 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("repeat before the 350 ms delay: %v", got)
	}
	if got := r.Due(t0.Add(350 * time.Millisecond)); len(got) != 1 || got[0] != (Event{BtnDown, Repeat}) {
		t.Fatalf("first repeat = %v", got)
	}
	// 5 more at 110 ms, then 45 ms.
	at := t0.Add(350 * time.Millisecond)
	for i := 0; i < 5; i++ {
		at = at.Add(RepeatRate)
		if n := len(r.Due(at)); n != 1 {
			t.Fatalf("repeat %d: %d events", i+2, n)
		}
	}
	at = at.Add(RepeatFast)
	if n := len(r.Due(at)); n != 1 {
		t.Fatalf("fast repeat: %d events", n)
	}
	r.Feed(Event{BtnDown, Release}, at)
	if !r.NextDeadline().IsZero() || len(r.Due(at.Add(time.Second))) != 0 {
		t.Fatal("repeats continued after release")
	}
}

func TestRepeaterOnlyNavigationButtons(t *testing.T) {
	var r Repeater
	t0 := time.Unix(0, 0)
	r.Feed(Event{BtnA, Press}, t0)
	if len(r.Due(t0.Add(time.Second))) != 0 {
		t.Fatal("A must not auto-repeat")
	}
	r.Feed(Event{BtnUp, Press}, t0)
	r.Feed(Event{BtnDown, Press}, t0) // the newer button takes over
	r.Feed(Event{BtnUp, Release}, t0) // releasing the old one changes nothing
	if got := r.Due(t0.Add(RepeatDelay)); len(got) != 1 || got[0].Button != BtnDown {
		t.Fatalf("got %v, want a Down repeat", got)
	}
}

func TestParseMisterMap(t *testing.T) {
	b := make([]byte, 128)
	put := func(slot int, v uint32) { binary.LittleEndian.PutUint32(b[4*slot:], v) }
	put(4, btnSouth) // A on the south button (user swapped)
	put(5, btnEast)  // B on east
	put(11, 0x13b)   // start
	put(0, 0x10000)  // an axis entry (no key code in the low bits)
	put(7, 0x305)    // above KEY_MAX-ish range: skipped
	m, err := ParseMisterMap(b)
	if err != nil {
		t.Fatal(err)
	}
	if m[btnSouth] != BtnA || m[btnEast] != BtnB || m[0x13b] != BtnStart {
		t.Fatalf("map = %v", m)
	}
	if len(m) != 3 {
		t.Fatalf("map has %d entries, want 3: %v", len(m), m)
	}
	if _, err := ParseMisterMap(b[:10]); err == nil {
		t.Fatal("short map accepted")
	}
}

func TestTranslatorKeysAndMapOverride(t *testing.T) {
	tr := newTranslator(map[uint16]Button{btnSouth: BtnA}, nil)
	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{BtnA, Press}) {
		t.Fatalf("mapped south = %v", got)
	}
	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{BtnA, Press}) {
		t.Fatalf("default east = %v", got)
	}
	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{BtnA, Release}) {
		t.Fatalf("enter release = %v", got)
	}
	if got := tr.handle(evKey, keyDown, 2); got != nil {
		t.Fatalf("kernel autorepeat must be ignored, got %v", got)
	}
	if got := tr.handle(evKey, 0x2fe, 1); got != nil {
		t.Fatalf("unknown key produced %v", got)
	}
}

func TestTranslatorHatAndStick(t *testing.T) {
	tr := newTranslator(nil, map[uint16]AbsRange{absX: {0, 255}})
	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{BtnUp, Press}) {
		t.Fatalf("hat up = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{BtnUp, Release}) || got[1] != (Event{BtnDown, Press}) {
		t.Fatalf("hat up→down = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{BtnDown, Release}) {
		t.Fatalf("hat centre = %v", got)
	}
	if got := tr.handle(evAbs, absX, 150); got != nil {
		t.Fatalf("small stick movement = %v", got)
	}
	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{BtnRight, Press}) {
		t.Fatalf("stick right = %v", got)
	}
	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{BtnRight, Release}) {
		t.Fatalf("stick centre = %v", got)
	}
	if got := tr.handle(evAbs, absY, 0); got != nil {
		t.Fatalf("uncalibrated axis = %v", got)
	}
}

func TestRepeaterNoBurstAfterStall(t *testing.T) {
	var r Repeater
	t0 := time.Unix(1000, 0)
	r.Feed(Event{BtnDown, Press}, t0)
	now := t0.Add(5 * time.Second)
	if got := r.Due(now); len(got) != 1 {
		t.Fatalf("Due after a stall = %d events, want 1", len(got))
	}
	if got := r.Due(now); len(got) != 0 {
		t.Fatalf("second Due at the same time = %d events, want 0", len(got))
	}
	if got := r.Due(now.Add(RepeatRate)); len(got) != 1 {
		t.Fatalf("Due one step later = %d events, want 1", len(got))
	}
}

func TestParseMisterMapMasksHighBits(t *testing.T) {
	b := make([]byte, 128)
	binary.LittleEndian.PutUint32(b[4*4:], 0x10001) // slot A: high bits set, key code 1
	m, err := ParseMisterMap(b)
	if err != nil {
		t.Fatal(err)
	}
	if m[1] != BtnA || len(m) != 1 {
		t.Fatalf("map = %v, want only code 1 -> BtnA", m)
	}
}
