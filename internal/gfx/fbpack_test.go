package gfx

import (
	"testing"
	"unsafe"
)

func TestPackXRGB8888WithStride(t *testing.T) {
	f := fbFormat{width: 2, height: 2, stride: 12, bpp: 32} // 4 bytes padding per line
	c := NewCanvas(2, 2)
	c.Pix = []uint32{0x112233, 0x445566, 0x778899, 0xAABBCC}
	mem := make([]byte, 24)
	for i := range mem {
		mem[i] = 0xEE
	}
	f.pack(mem, c)
	want := []byte{0x33, 0x22, 0x11, 0, 0x66, 0x55, 0x44, 0, 0xEE, 0xEE, 0xEE, 0xEE,
		0x99, 0x88, 0x77, 0, 0xCC, 0xBB, 0xAA, 0, 0xEE, 0xEE, 0xEE, 0xEE}
	for i := range want {
		if mem[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x (padding must be untouched)", i, mem[i], want[i])
		}
	}
}

func TestPackRGB565(t *testing.T) {
	f := fbFormat{width: 3, height: 1, stride: 6, bpp: 16}
	c := NewCanvas(3, 1)
	c.Pix = []uint32{0xFF0000, 0x00FF00, 0x0000FF}
	mem := make([]byte, 6)
	f.pack(mem, c)
	got := []uint16{uint16(mem[0]) | uint16(mem[1])<<8, uint16(mem[2]) | uint16(mem[3])<<8, uint16(mem[4]) | uint16(mem[5])<<8}
	if got[0] != 0xF800 || got[1] != 0x07E0 || got[2] != 0x001F {
		t.Fatalf("RGB565 = %04x %04x %04x", got[0], got[1], got[2])
	}
}

func TestFBFormatValidate(t *testing.T) {
	if (fbFormat{640, 480, 2560, 32}).validate() != nil {
		t.Fatal("valid 32bpp rejected")
	}
	if (fbFormat{640, 480, 1280, 24}).validate() == nil {
		t.Fatal("24bpp accepted")
	}
	if (fbFormat{640, 480, 100, 32}).validate() == nil {
		t.Fatal("stride smaller than a line accepted")
	}
}

// The ioctl structs must match the kernel's sizes on this platform.
func TestFBStructSizes(t *testing.T) {
	if s := unsafe.Sizeof(fbVarScreenInfo{}); s != 160 {
		t.Fatalf("fb_var_screeninfo = %d bytes, want 160", s)
	}
	want := uintptr(68) // 32-bit ARM
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 80
	}
	if s := unsafe.Sizeof(fbFixScreenInfo{}); s != want {
		t.Fatalf("fb_fix_screeninfo = %d bytes, want %d", s, want)
	}
}
