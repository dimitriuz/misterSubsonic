//go:build linux

package gfx

import "testing"

func TestFBClosedIsSafe(t *testing.T) {
	mem := make([]byte, 16)
	b := &FB{mapping: mem, mem: mem, fmt: fbFormat{2, 2, 8, 16}}
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

func xrgb(xres, yres, xoff, yoff, stride, smem uint32) (fbVarScreenInfo, fbFixScreenInfo) {
	v := fbVarScreenInfo{Xres: xres, Yres: yres, Xoffset: xoff, Yoffset: yoff, BitsPerPixel: 32,
		Red: fbBitfield{16, 8, 0}, Green: fbBitfield{8, 8, 0}, Blue: fbBitfield{0, 8, 0}}
	return v, fbFixScreenInfo{LineLength: stride, SmemLen: smem}
}

func TestFBLayoutFollowsThePanOffset(t *testing.T) {
	// A console panned to its second page: the screen starts yoffset lines in.
	v, fx := xrgb(640, 480, 0, 480, 2560, 2560*960)
	_, off, end, err := fbLayout(&v, &fx)
	if err != nil || off != 2560*480 || end != 2560*960 {
		t.Fatalf("off %d end %d err %v", off, end, err)
	}
	v, fx = xrgb(640, 480, 8, 2, 2560, 2560*960)
	if _, off, _, _ := fbLayout(&v, &fx); off != 2*2560+8*4 {
		t.Fatalf("x/y pan: off %d", off)
	}
}

func TestFBLayoutNeedsTheScreenInsideSmem(t *testing.T) {
	v, fx := xrgb(640, 480, 0, 0, 2560, 2560*480)
	if _, _, _, err := fbLayout(&v, &fx); err != nil {
		t.Fatalf("an exact fit was refused: %v", err)
	}
	v, fx = xrgb(640, 480, 0, 1, 2560, 2560*480) // panned one line past the end
	if _, _, _, err := fbLayout(&v, &fx); err == nil {
		t.Fatal("a screen ending past smem_len was accepted")
	}
	v, fx = xrgb(640, 480, 0, 0, 100, 1<<24) // stride shorter than a line
	if _, _, _, err := fbLayout(&v, &fx); err == nil {
		t.Fatal("a stride shorter than the line was accepted")
	}
}

func TestPageAlign(t *testing.T) {
	for _, c := range []struct {
		phys  uintptr
		base  uintptr
		delta int
	}{{0x1E000000, 0x1E000000, 0}, {0x1E000800, 0x1E000000, 0x800}, {0x1E000FFF, 0x1E000000, 0xFFF}, {0x1E001000, 0x1E001000, 0}} {
		if b, d := pageAlign(c.phys, 4096); b != c.base || d != c.delta {
			t.Errorf("pageAlign(%#x) = %#x,%#x; want %#x,%#x", c.phys, b, d, c.base, c.delta)
		}
	}
}

func TestIntactNoticesAnOverwrite(t *testing.T) {
	for _, bpp := range []int{32, 16} {
		f := fbFormat{width: 64, height: 48, stride: 64 * bpp / 8, bpp: bpp}
		b := &FB{mem: make([]byte, f.stride*f.height), fmt: f}
		if !b.Intact() {
			t.Fatalf("%d bpp: not intact before the first frame", bpp)
		}
		c := NewCanvas(64, 48)
		for i := range c.Pix {
			c.Pix[i] = uint32(i) * 2654435761 & 0xFFFFFF
		}
		b.Present(c)
		if !b.Intact() {
			t.Fatalf("%d bpp: the frame just presented doesn't match", bpp)
		}
		for i := range b.mem[:f.stride*4] { // the console writes over the top lines
			b.mem[i] ^= 0xFF
		}
		if b.Intact() {
			t.Fatalf("%d bpp: an overwritten top missed", bpp)
		}
		b.Present(c)
		copy(b.mem[len(b.mem)-f.stride:], make([]byte, f.stride)) // and the last line
		if b.Intact() {
			t.Fatalf("%d bpp: an overwritten bottom line missed", bpp)
		}
	}
}
