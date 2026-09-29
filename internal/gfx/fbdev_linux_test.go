//go:build linux

package gfx

import "testing"

func TestFBClosedIsSafe(t *testing.T) {
	b := &FB{mem: make([]byte, 16), fmt: fbFormat{2, 2, 8, 16}}
	b.Close() // Munmap of a heap slice errors; the fields must still be cleared
	b.Close()
	if err := b.Present(NewCanvas(2, 2)); err == nil {
		t.Fatal("Present after Close returned nil")
	}
	b.Blank()
}

func TestCheckLayout(t *testing.T) {
	v := fbVarScreenInfo{BitsPerPixel: 32, Red: fbBitfield{16, 8, 0}, Green: fbBitfield{8, 8, 0}, Blue: fbBitfield{0, 8, 0}}
	if err := v.checkLayout(); err != nil {
		t.Fatalf("XRGB8888 rejected: %v", err)
	}
	v = fbVarScreenInfo{BitsPerPixel: 16, Red: fbBitfield{11, 5, 0}, Green: fbBitfield{5, 6, 0}, Blue: fbBitfield{0, 5, 0}}
	if err := v.checkLayout(); err != nil {
		t.Fatalf("RGB565 rejected: %v", err)
	}
	v = fbVarScreenInfo{BitsPerPixel: 32, Red: fbBitfield{0, 8, 0}, Green: fbBitfield{8, 8, 0}, Blue: fbBitfield{16, 8, 0}}
	if v.checkLayout() == nil {
		t.Fatal("32bpp BGR accepted")
	}
}
