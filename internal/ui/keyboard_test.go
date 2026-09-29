package ui

import (
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func TestTextKeyboardLayoutsAndPresets(t *testing.T) {
	k := NewTextKeyboard(urlPresets...)
	if got := k.Focused(); got.action != keyInsert || got.text != "http://" {
		t.Fatalf("top-left key %+v, want the http:// preset", got)
	}
	k.Move(press(input.BtnDown))
	k.Move(press(input.BtnDown)) // presets -> digits -> letters, under the middle of "http://"
	if got := k.Focused(); got.r != 'b' {
		t.Fatalf("focused %q, want b", got.label)
	}
	k.Move(press(input.BtnLeft))
	for range 4 {
		k.Move(press(input.BtnDown))
	}
	if got := k.Focused(); got.action != keySpace {
		t.Fatalf("bottom-left %q, want Space", got.label)
	}
	for range 2 {
		k.Move(press(input.BtnRight))
	}
	up := k.Focused()
	if up.action != keyLayout || up.label != "ABC" {
		t.Fatalf("third bottom key %q", up.label)
	}
	k.Switch(up.to)
	if got := k.Focused(); got.label != "abc" {
		t.Fatalf("after switching to upper case the key reads %q", got.label)
	}
	k.Move(press(input.BtnUp))
	k.Move(press(input.BtnUp))
	if got := k.Focused(); got.r < 'A' || got.r > 'Z' {
		t.Fatalf("upper-case layout has %q", got.label)
	}
	rows := k.rows()
	sym := rows[len(rows)-1][3] // bottom row, fourth key
	if sym.label != "#+=" {
		t.Fatalf("fourth bottom key %q", sym.label)
	}
	k.Switch(sym.to)
	found := map[rune]bool{}
	for _, row := range k.rows() {
		for _, key := range row {
			found[key.r] = true
		}
	}
	for _, r := range `!@#$%^&*()-_=+[]{}\|;:'",.<>/?` + "`~" {
		if !found[r] {
			t.Errorf("symbols layout lacks %q", r)
		}
	}
	for _, row := range k.rows() {
		units := 0
		for _, key := range row {
			units += key.units
		}
		if units > kbUnits {
			t.Errorf("row %v is %d units wide", row, units)
		}
	}
}

func TestMaskedTextFieldHidesTheValue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	draw := func(v string, masked bool) *gfx.Canvas {
		c := gfx.NewCanvas(400, 60)
		ta.drawTextField(c, c.Bounds(), []rune(v), "hint", masked)
		return c
	}
	if !samePixels(draw("secret", true).ToRGBA(), draw("abcdef", true).ToRGBA()) {
		t.Fatal("a masked field shows something of its value")
	}
	if samePixels(draw("secret", false).ToRGBA(), draw("abcdef", false).ToRGBA()) {
		t.Fatal("an unmasked field doesn't show its value")
	}
	if samePixels(draw("", false).ToRGBA(), draw("x", false).ToRGBA()) {
		t.Fatal("an empty field looks like a filled one")
	}
}
