package input

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestRepeaterTiming(t *testing.T) {
	var r Repeater
	t0 := time.Unix(1000, 0)
	r.Feed(Event{Button: BtnDown, Kind: Press}, t0)
	if got := r.Due(t0.Add(349 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("repeat before the 350 ms delay: %v", got)
	}
	if got := r.Due(t0.Add(350 * time.Millisecond)); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Repeat}) {
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
	r.Feed(Event{Button: BtnDown, Kind: Release}, at)
	if !r.NextDeadline().IsZero() || len(r.Due(at.Add(time.Second))) != 0 {
		t.Fatal("repeats continued after release")
	}
}

func TestRepeaterOnlyNavigationButtons(t *testing.T) {
	var r Repeater
	t0 := time.Unix(0, 0)
	r.Feed(Event{Button: BtnA, Kind: Press}, t0)
	if len(r.Due(t0.Add(time.Second))) != 0 {
		t.Fatal("A must not auto-repeat")
	}
	r.Feed(Event{Button: BtnUp, Kind: Press}, t0)
	r.Feed(Event{Button: BtnDown, Kind: Press}, t0) // the newer button takes over
	r.Feed(Event{Button: BtnUp, Kind: Release}, t0) // releasing the old one changes nothing
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
	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press}) {
		t.Fatalf("mapped south = %v", got)
	}
	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{Button: BtnA, Kind: Press}) {
		t.Fatalf("default east = %v", got)
	}
	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{Button: BtnA, Kind: Release, Rune: '\n'}) {
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
	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{Button: BtnUp, Kind: Press}) {
		t.Fatalf("hat up = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{Button: BtnUp, Kind: Release}) || got[1] != (Event{Button: BtnDown, Kind: Press}) {
		t.Fatalf("hat up→down = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Release}) {
		t.Fatalf("hat centre = %v", got)
	}
	if got := tr.handle(evAbs, absX, 150); got != nil {
		t.Fatalf("small stick movement = %v", got)
	}
	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Press}) {
		t.Fatalf("stick right = %v", got)
	}
	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Release}) {
		t.Fatalf("stick centre = %v", got)
	}
	if got := tr.handle(evAbs, absY, 0); got != nil {
		t.Fatalf("uncalibrated axis = %v", got)
	}
}

func TestRepeaterNoBurstAfterStall(t *testing.T) {
	var r Repeater
	t0 := time.Unix(1000, 0)
	r.Feed(Event{Button: BtnDown, Kind: Press}, t0)
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

func TestTranslatorTypesText(t *testing.T) {
	tr := newTranslator(nil, nil)
	const keyA, keyZ = 30, 44
	if got := tr.handle(evKey, keyA, 1); len(got) != 1 || got[0] != (Event{Button: BtnNone, Kind: Press, Rune: 'a'}) {
		t.Fatalf("a = %v", got)
	}
	if got := tr.handle(evKey, keyA, 0); len(got) != 1 || got[0] != (Event{Button: BtnNone, Kind: Release, Rune: 'a'}) {
		t.Fatalf("a release = %v", got)
	}
	// Mapped keys keep their button and also type.
	if got := tr.handle(evKey, keyN, 1); got[0] != (Event{Button: BtnY, Kind: Press, Rune: 'n'}) {
		t.Fatalf("n = %v", got)
	}
	if got := tr.handle(evKey, keyQ, 1); got[0] != (Event{Button: BtnQueue, Kind: Press, Rune: 'q'}) {
		t.Fatalf("q = %v", got)
	}
	if got := tr.handle(evKey, keyM, 1); got[0] != (Event{Button: BtnMute, Kind: Press, Rune: 'm'}) {
		t.Fatalf("m = %v", got)
	}
	if got := tr.handle(evKey, keySpace, 1); got[0] != (Event{Button: BtnStart, Kind: Press, Rune: ' '}) {
		t.Fatalf("space = %v", got)
	}
	if got := tr.handle(evKey, keyLShift, 1); got != nil {
		t.Fatalf("shift alone = %v", got)
	}
	if got := tr.handle(evKey, keyZ, 1); got[0].Rune != 'Z' {
		t.Fatalf("shift+z = %v", got)
	}
	if got := tr.handle(evKey, 3, 1); got[0].Rune != '@' { // Shift+2
		t.Fatalf("shift+2 = %v", got)
	}
	tr.handle(evKey, keyLShift, 0)
	if got := tr.handle(evKey, 53, 1); got[0].Rune != '/' {
		t.Fatalf("slash = %v", got)
	}
	// Kernel autorepeat repeats typing (Backspace held), never buttons.
	if got := tr.handle(evKey, keyBackspace, 2); len(got) != 1 || got[0] != (Event{Kind: Press, Rune: '\b'}) {
		t.Fatalf("backspace autorepeat = %v", got)
	}
	if got := tr.handle(evKey, keyEnter, 2); got != nil {
		t.Fatalf("enter autorepeat = %v", got)
	}
}

// Enter is A and also types '\n' (the wizard's "next field"); holding it
// must not confirm again and again.
func TestEnterTypesNewlineOnce(t *testing.T) {
	tr := newTranslator(nil, nil)
	if got := tr.handle(evKey, keyEnter, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press, Rune: '\n'}) {
		t.Fatalf("enter = %v", got)
	}
	if got := tr.handle(evKey, keyKPEnter, 1); got[0].Rune != '\n' {
		t.Fatalf("keypad enter = %v", got)
	}
	if got := tr.handle(evKey, keyEnter, 2); got != nil {
		t.Fatalf("held enter repeats: %v", got)
	}
}

// A multimedia keyboard's media keys (a Logitech K400 sends them on its
// main keyboard device) map to the media buttons; volume and seek repeat.
func TestMediaKeys(t *testing.T) {
	tr := newTranslator(nil, nil)
	for code, want := range map[uint16]Button{
		keyMute: BtnMute, keyVolumeUp: BtnVolUp, keyVolumeDown: BtnVolDown,
		keyPlayPause: BtnPlayPause, keyPlay: BtnPlayPause, keyPlayCD: BtnPlayPause, keyPauseCD: BtnPlayPause,
		keyNextSong: BtnNextTrack, keyPreviousSong: BtnPrevTrack, keyFastForward: BtnSeekFwd, keyRewind: BtnSeekBack,
		keySysRq: BtnScreenshot, keyScrollLock: BtnScreenshot,
	} {
		got := tr.handle(evKey, code, 1)
		if len(got) != 1 || got[0].Button != want || got[0].Kind != Press || got[0].Rune != 0 {
			t.Errorf("key %d = %v, want %v", code, got, want)
		}
	}
	for _, b := range []Button{BtnVolUp, BtnVolDown, BtnSeekFwd, BtnSeekBack} {
		if !repeats(b) {
			t.Errorf("%v doesn't repeat when held", b)
		}
	}
	if repeats(BtnPlayPause) || repeats(BtnNextTrack) {
		t.Error("play/pause or next repeats when held")
	}
}
