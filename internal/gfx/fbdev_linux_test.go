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

// PresentRects updates the framebuffer in the rectangles only, and the
// watchdog then compares against the new frame.
func TestPresentRectsUpdatesOnlyTheRectangles(t *testing.T) {
	f := fbFormat{width: 64, height: 48, stride: 64 * 4, bpp: 32}
	b := &FB{mem: make([]byte, f.stride*f.height), fmt: f}
	old, next := NewCanvas(64, 48), NewCanvas(64, 48)
	old.Clear(RGB(1, 1, 1))
	next.Clear(RGB(1, 1, 1))
	next.Fill(R(10, 10, 5, 5), RGB(200, 0, 0))
	b.Present(old)
	if err := b.PresentRects(next, []Rect{R(10, 10, 5, 5)}); err != nil {
		t.Fatal(err)
	}
	if got := f.read(b.mem, 12, 12); got != 0xC80000 {
		t.Fatalf("inside = %06x", got)
	}
	if !b.Intact() {
		t.Fatal("the watchdog doesn't see the updated frame")
	}
	if err := b.PresentRects(NewCanvas(10, 10), nil); err == nil {
		t.Fatal("a frame of the wrong size was accepted")
	}
}

// The last line ends at its pixels: the stride's padding after it is not
// needed, so a framebuffer exactly that long is accepted.
func TestFBLayoutEndIgnoresTheLastLinesPadding(t *testing.T) {
	v, fx := xrgb(640, 480, 0, 0, 2816, 2816*479+2560)
	_, off, end, err := fbLayout(&v, &fx)
	if err != nil || off != 0 || end != 2816*479+2560 {
		t.Fatalf("off %d end %d err %v", off, end, err)
	}
	v, fx = xrgb(640, 480, 0, 0, 2816, 2816*479+2559)
	if _, _, _, err := fbLayout(&v, &fx); err == nil {
		t.Fatal("a framebuffer one byte short was accepted")
	}
}

// Offsets near 2^32 must not wrap (int is 32 bits on the MiSTer).
func TestFBLayoutRefusesHugeOffsets(t *testing.T) {
	v, fx := xrgb(640, 480, 0, 0xFFFFFFFF, 2560, 0xFFFFFFFF)
	if _, _, _, err := fbLayout(&v, &fx); err == nil {
		t.Fatal("a huge yoffset was accepted")
	}
	v, fx = xrgb(640, 480, 0xFFFFFFFF, 0, 4096, 0xFFFFFFFF)
	if _, _, _, err := fbLayout(&v, &fx); err == nil {
		t.Fatal("a huge xoffset was accepted")
	}
}

func TestScreenSliceBounds(t *testing.T) {
	m := make([]byte, 100)
	for i := range m {
		m[i] = byte(i)
	}
	// Through the device: the mapping is [0, end).
	s, err := screenSlice(m[:80], 20, 80, 0)
	if err != nil || len(s) != 60 || s[0] != 20 {
		t.Fatalf("device: %d %v %v", len(s), s[:1], err)
	}
	// Through /dev/mem: delta bytes in front.
	s, err = screenSlice(m, 20, 80, 15)
	if err != nil || len(s) != 60 || s[0] != 35 {
		t.Fatalf("devmem: %d %v", len(s), err)
	}
	for _, c := range [][3]int{{20, 80, 21}, {-1, 80, 0}, {81, 80, 0}, {0, 101, 0}, {0, 80, -1}} {
		if _, err := screenSlice(m, c[0], c[1], c[2]); err == nil {
			t.Errorf("off %d end %d delta %d accepted", c[0], c[1], c[2])
		}
	}
}

func newTestFB(w, h int) *FB {
	mem := make([]byte, w*h*4)
	for i := range mem {
		mem[i] = 0xEE
	}
	return &FB{mapping: mem, mem: mem, fmt: fbFormat{w, h, w * 4, 32}}
}

func solid(w, h int, col uint32) *Canvas {
	c := NewCanvas(w, h)
	for i := range c.Pix {
		c.Pix[i] = col
	}
	return c
}

func TestFBPresentsAnInsetCanvasAtItsOffsetWithABlackBorder(t *testing.T) {
	b := newTestFB(16, 10)
	in := Rect{3, 2, 10, 6}
	b.SetInset(in)
	if err := b.Present(solid(10, 6, 0xFF0000)); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			want := uint32(0)
			if in.Contains(x, y) {
				want = 0xFF0000
			}
			if got := b.fmt.read(b.mem, x, y); got != want {
				t.Fatalf("(%d,%d) = %06x, want %06x", x, y, got, want)
			}
		}
	}
	if err := b.Present(solid(16, 10, 0)); err == nil {
		t.Fatal("a canvas of the screen's size was accepted with an inset")
	}
}

func TestFBPartialPresentWritesOnlyTheOffsetRects(t *testing.T) {
	b := newTestFB(16, 10)
	in := Rect{3, 2, 10, 6}
	b.SetInset(in)
	c := solid(10, 6, 0xFF0000)
	b.Present(c)
	before := append([]byte(nil), b.mem...)
	c.Pix[1*10+2] = 0x00FF00 // (2,1) of the canvas
	c.Pix[5*10+9] = 0x0000FF // (9,5): not in the rect below
	if err := b.PresentRects(c, []Rect{{2, 1, 1, 1}}); err != nil {
		t.Fatal(err)
	}
	changed := 0
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			o := y*b.fmt.stride + x*4
			if string(before[o:o+4]) != string(b.mem[o:o+4]) {
				changed++
				if x != 5 || y != 3 {
					t.Fatalf("pixel (%d,%d) changed", x, y)
				}
			}
		}
	}
	if changed != 1 || b.fmt.read(b.mem, 5, 3) != 0x00FF00 {
		t.Fatalf("%d pixels changed, (5,3) = %06x", changed, b.fmt.read(b.mem, 5, 3))
	}
}

func TestFBFullPresentRestoresTheBorderAndIntactWatchesIt(t *testing.T) {
	b := newTestFB(16, 10)
	b.SetInset(Rect{3, 2, 10, 6})
	c := solid(10, 6, 0x336699)
	b.Present(c)
	if !b.Intact() {
		t.Fatal("a fresh frame is not intact")
	}
	for i := 0; i < 4*16; i++ { // something drew over the top line
		b.mem[i] = 0xFF
	}
	if b.Intact() {
		t.Fatal("the border drawn over still counts as intact")
	}
	b.Present(c) // the full repaint the watchdog asks for
	if !b.Intact() || b.fmt.read(b.mem, 0, 0) != 0 {
		t.Fatal("a full present did not black the border again")
	}
	// Back to no inset: the whole screen is the canvas.
	b.SetInset(Rect{})
	if err := b.Present(solid(16, 10, 0x112233)); err != nil || b.fmt.read(b.mem, 0, 0) != 0x112233 {
		t.Fatalf("no inset: %v", err)
	}
}
