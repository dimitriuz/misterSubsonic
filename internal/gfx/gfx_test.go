package gfx

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFillOpaqueAndBlend(t *testing.T) {
	c := NewCanvas(4, 4)
	c.Clear(RGB(0, 0, 0))
	c.Fill(R(1, 1, 2, 2), RGB(255, 0, 0))
	if c.At(1, 1) != RGB(255, 0, 0) || c.At(0, 0) != RGB(0, 0, 0) || c.At(3, 3) != RGB(0, 0, 0) {
		t.Fatalf("opaque fill wrong: %08x %08x %08x", c.At(1, 1), c.At(0, 0), c.At(3, 3))
	}
	c.Fill(R(0, 0, 4, 4), RGBA(255, 255, 255, 128))
	if got := c.At(0, 0); got != RGB(128, 128, 128) {
		t.Fatalf("50%% white over black = %08x, want ff808080", got)
	}
	c.Fill(R(-10, -10, 100, 100), RGB(1, 2, 3)) // clipped, must not panic
	if c.At(3, 3) != RGB(1, 2, 3) {
		t.Fatal("clipped fill did not paint")
	}
}

func TestRectIntersect(t *testing.T) {
	if got := R(0, 0, 10, 10).Intersect(R(5, 5, 10, 10)); got != R(5, 5, 5, 5) {
		t.Fatalf("intersect = %+v", got)
	}
	if !R(0, 0, 2, 2).Intersect(R(5, 5, 1, 1)).Empty() {
		t.Fatal("disjoint rects should not intersect")
	}
}

func TestBlitScalesAndRespectsAlpha(t *testing.T) {
	src := NewImage(2, 1)
	src.Pix[0] = 0xFFFF0000 // opaque red
	src.Pix[1] = 0x00000000 // transparent
	c := NewCanvas(4, 2)
	c.Clear(RGB(0, 0, 255))
	c.Blit(src, R(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		if c.At(0, y) != RGB(255, 0, 0) || c.At(1, y) != RGB(255, 0, 0) {
			t.Fatalf("left half not red at row %d", y)
		}
		if c.At(2, y) != RGB(0, 0, 255) || c.At(3, y) != RGB(0, 0, 255) {
			t.Fatalf("transparent half painted at row %d", y)
		}
	}
	c.Blit(src, R(-3, 0, 4, 2)) // partially off-canvas: must not panic
}

func TestResizeAveragesAndFitKeepsAspect(t *testing.T) {
	src := NewImage(2, 2)
	src.Pix = []uint32{0xFF000000, 0xFFFFFFFF, 0xFFFFFFFF, 0xFF000000}
	out := Resize(src, 1, 1)
	if out.Pix[0] != 0xFF7F7F7F {
		t.Fatalf("average = %08x, want ff7f7f7f", out.Pix[0])
	}
	wide := NewImage(400, 200)
	if f := Fit(wide, 100, 100); f.W != 100 || f.H != 50 {
		t.Fatalf("Fit 400x200 into 100x100 = %dx%d", f.W, f.H)
	}
	small := NewImage(10, 10)
	if Fit(small, 100, 100) != small {
		t.Fatal("Fit must not upscale")
	}
}

func TestDecodeImagePNG(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for i := range m.Pix {
		m.Pix[i] = 0xFF
	}
	m.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, m)
	img, err := DecodeImage(buf.Bytes(), 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if img.W != 32 || img.H != 16 {
		t.Fatalf("decoded to %dx%d, want 32x16", img.W, img.H)
	}
	if _, err := DecodeImage([]byte("not an image"), 10, 10); err == nil {
		t.Fatal("garbage decoded")
	}
}

func testFont(t *testing.T, px int) *Font {
	t.Helper()
	tf, err := LoadTypeface(false, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := tf.Face(px)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func inkIn(c *Canvas, r Rect) int {
	n := 0
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X; x < r.Right(); x++ {
			if c.At(x, y) != RGB(0, 0, 0) {
				n++
			}
		}
	}
	return n
}

func TestTextDrawsLatinCyrillicGreek(t *testing.T) {
	f := testFont(t, 24)
	for _, s := range []string{"Björk", "Аквариум", "Ελλάδα"} {
		c := NewCanvas(300, 40)
		w := f.Measure(s)
		end := f.Draw(c, 10, 30, s, RGB(255, 255, 255), c.Bounds())
		if end != 10+w {
			t.Fatalf("%q: Draw returned %d, want %d", s, end, 10+w)
		}
		if inkIn(c, R(10, 0, w, 40)) < 50 {
			t.Fatalf("%q rendered almost nothing", s)
		}
		if inkIn(c, R(10+w+2, 0, 300-(10+w+2), 40)) != 0 {
			t.Fatalf("%q drew past its measured width", s)
		}
	}
	if f.Height() < 24 || f.Ascent() <= 0 {
		t.Fatalf("metrics: ascent %d height %d", f.Ascent(), f.Height())
	}
}

func TestTextClip(t *testing.T) {
	f := testFont(t, 24)
	c := NewCanvas(200, 40)
	f.Draw(c, 0, 30, "Hello world", RGB(255, 255, 255), R(0, 0, 30, 40))
	if inkIn(c, R(30, 0, 170, 40)) != 0 {
		t.Fatal("text escaped its clip rect")
	}
}

func TestTruncate(t *testing.T) {
	f := testFont(t, 20)
	s := "A very long album title that will not fit"
	got := f.Truncate(s, 100)
	if f.Measure(got) > 100 || got[len(got)-len("…"):] != "…" {
		t.Fatalf("Truncate = %q (%d px)", got, f.Measure(got))
	}
	if f.Truncate("Short", 1000) != "Short" {
		t.Fatal("short text changed")
	}
}

func TestScalerHDMIAndCRT(t *testing.T) {
	// 1280x720 UI on a 1920x1080 framebuffer: 1.5x, full screen.
	s := NewScaler(1280, 720, 1920, 1080)
	if s.Area() != R(0, 0, 1920, 1080) {
		t.Fatalf("1080p area = %+v", s.Area())
	}
	// On 1280x1024 it letterboxes vertically.
	s = NewScaler(1280, 720, 1280, 1024)
	if a := s.Area(); a.W != 1280 || a.H != 720 || a.Y != 152 {
		t.Fatalf("1280x1024 area = %+v", a)
	}
	// 320x240 UI on a 640x240 CRT framebuffer: fills, 2x horizontally.
	s = NewScaler(320, 240, 640, 240)
	src := NewCanvas(320, 240)
	src.Pix[0] = 0xFF0000
	out := s.Scale(src)
	if out.At(0, 0) != RGB(255, 0, 0) || out.At(1, 0) != RGB(255, 0, 0) || out.At(2, 0) == RGB(255, 0, 0) {
		t.Fatal("CRT horizontal doubling wrong")
	}
	// 320x240 UI on 640x480: uniform 2x (line doubled).
	s = NewScaler(320, 240, 640, 480)
	out = s.Scale(src)
	if s.Area() != R(0, 0, 640, 480) || out.At(1, 1) != RGB(255, 0, 0) || out.At(0, 2) == RGB(255, 0, 0) {
		t.Fatalf("480-line doubling wrong (area %+v)", s.Area())
	}
}

func TestHeadlessKeepsLastFrame(t *testing.T) {
	d := NewHeadless(4, 4, t.TempDir())
	c := NewCanvas(4, 4)
	c.Clear(RGB(9, 9, 9))
	if err := d.Present(c); err != nil {
		t.Fatal(err)
	}
	c.Clear(RGB(1, 1, 1)) // later changes must not affect the stored frame
	if d.Last().At(0, 0) != RGB(9, 9, 9) || d.Frames() != 1 {
		t.Fatal("headless did not keep a copy of the frame")
	}
}

func TestMarqueeScrollsAndLoops(t *testing.T) {
	f := testFont(t, 20)
	s, dx := f.Marquee("Hello", 0)
	if dx != 0 || s[:5] != "Hello" {
		t.Fatalf("offset 0 = %q, %d", s, dx)
	}
	w := f.Measure("H")
	s, dx = f.Marquee("Hello", w+1)
	if s[0] != 'e' || dx != -1 {
		t.Fatalf("offset past first glyph = %q, %d", s[:3], dx)
	}
}

// Titles in scripts the bundled font lacks (CJK, emoji) draw .notdef boxes
// instead of crashing or vanishing.
func TestMissingGlyphsDrawBoxes(t *testing.T) {
	f := testFont(t, 24)
	c := NewCanvas(200, 40)
	w := f.Measure("日本語")
	f.Draw(c, 0, 30, "日本語", RGB(255, 255, 255), c.Bounds())
	if w <= 0 || inkIn(c, R(0, 0, w, 40)) == 0 {
		t.Fatalf("missing glyphs rendered nothing (width %d)", w)
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	f := testFont(t, 20)
	for _, s := range []string{"Рок-н-ролл мёртв и снова жив", "Ελληνική μουσική παράδοση"} {
		for w := 10; w < f.Measure(s); w += 7 {
			if got := f.Truncate(s, w); !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", s, w, got)
			}
		}
	}
}

// A PNG header claiming a gigantic image is rejected before decoding.
func TestDecodeRejectsDecompressionBomb(t *testing.T) {
	for _, dim := range [][2]uint32{{20000, 20000}, {65536, 65536}} {
		m := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		var buf bytes.Buffer
		png.Encode(&buf, m)
		b := buf.Bytes()
		// IHDR width/height live at bytes 16..23; fix the chunk CRC so the
		// header parses and only the size guard can reject it.
		binary.BigEndian.PutUint32(b[16:], dim[0])
		binary.BigEndian.PutUint32(b[20:], dim[1])
		binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
		_, err := DecodeImage(b, 100, 100)
		// On 32-bit (the MiSTer) image/png refuses these sizes itself.
		if err == nil || !strings.Contains(err.Error(), "too large") && !strings.Contains(err.Error(), "dimension overflow") {
			t.Fatalf("%dx%d: err = %v, want \"too large\"", dim[0], dim[1], err)
		}
	}
}

func TestOpaqueTextIsExactColour(t *testing.T) {
	f := testFont(t, 40)
	c := NewCanvas(60, 60)
	f.Draw(c, 5, 50, "H", RGB(255, 255, 255), c.Bounds())
	for _, p := range c.Pix {
		if Color(p)|0xFF000000 == RGB(255, 255, 255) {
			return
		}
	}
	t.Fatal("no pixel reached exact white")
}

func TestScalerInStaysInsideItsArea(t *testing.T) {
	src := NewCanvas(4, 2)
	for i := range src.Pix {
		src.Pix[i] = 0x00FFFFFF
	}
	for _, c := range []struct {
		pw, ph int
		in     Rect
		want   Rect
	}{
		{640, 240, Rect{26, 12, 588, 216}, Rect{26, 12, 588, 216}},    // a CRT mode fills its area
		{1280, 720, Rect{40, 10, 1200, 700}, Rect{40, 60, 1200, 600}}, // uniform scale, centred in the area
	} {
		s := NewScalerIn(4, 2, c.pw, c.ph, c.in)
		if s.Area() != c.want {
			t.Errorf("%dx%d in %v: area %v, want %v", c.pw, c.ph, c.in, s.Area(), c.want)
		}
		d := s.Scale(src)
		for y := 0; y < c.ph; y++ {
			for x := 0; x < c.pw; x++ {
				if lit := d.Pix[y*c.pw+x] != 0; lit != c.want.Contains(x, y) {
					t.Fatalf("%dx%d: pixel (%d,%d) lit=%v, area %v", c.pw, c.ph, x, y, lit, c.want)
				}
			}
		}
	}
	a, b := NewScaler(4, 2, 640, 240), NewScalerIn(4, 2, 640, 240, Rect{0, 0, 640, 240})
	if a.Area() != b.Area() {
		t.Fatal("NewScaler is not NewScalerIn of the whole framebuffer")
	}
}

// recorder is a Display that keeps the frames and rectangles it is given.
type recorder struct {
	w, h  int
	fulls []*Canvas
	rects [][]Rect
	last  *Canvas
}

func (r *recorder) Size() (int, int) { return r.w, r.h }
func (r *recorder) Close() error     { return nil }
func (r *recorder) Present(c *Canvas) error {
	r.fulls, r.last = append(r.fulls, c), c
	return nil
}
func (r *recorder) PresentRects(c *Canvas, rs []Rect) error {
	r.rects, r.last = append(r.rects, append([]Rect(nil), rs...)), c
	return nil
}

func TestInsetterCentresACanvasOnABlackFrame(t *testing.T) {
	rec := &recorder{w: 12, h: 8}
	d := NewInsetter(rec)
	in := Rect{2, 1, 8, 6}
	d.SetInset(in)
	c := NewCanvas(8, 6)
	for i := range c.Pix {
		c.Pix[i] = 0xFFFFFF
	}
	if err := d.Present(c); err != nil {
		t.Fatal(err)
	}
	f := rec.last
	if f.W != 12 || f.H != 8 {
		t.Fatalf("frame %dx%d", f.W, f.H)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			if lit := f.Pix[y*12+x] != 0; lit != in.Contains(x, y) {
				t.Fatalf("(%d,%d) lit=%v", x, y, lit)
			}
		}
	}
	c.Pix[0] = 0x00FF00
	if err := d.PresentRects(c, []Rect{{0, 0, 1, 1}}); err != nil {
		t.Fatal(err)
	}
	if len(rec.rects) != 1 || rec.rects[0][0] != (Rect{2, 1, 1, 1}) || rec.last.Pix[1*12+2] != 0x00FF00 {
		t.Fatalf("partial present %v", rec.rects)
	}
	d.SetInset(Rect{})
	full := NewCanvas(12, 8)
	if d.Present(full); rec.last != full {
		t.Fatal("with no inset a canvas goes through as it is")
	}
}
