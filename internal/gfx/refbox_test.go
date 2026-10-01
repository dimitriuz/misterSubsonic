package gfx

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// The reference implementations below are the per-pixel closure versions the
// row loops replaced.

// refPixels reads m's pixel (x, y), counted from its top-left corner, as
// non-premultiplied ARGB. The common decoder outputs are read directly; other
// types go through At.
func refPixels(m image.Image) func(x, y int) uint32 {
	b := m.Bounds()
	switch m := m.(type) {
	case *image.YCbCr:
		return func(x, y int) uint32 {
			yi, ci := m.YOffset(b.Min.X+x, b.Min.Y+y), m.COffset(b.Min.X+x, b.Min.Y+y)
			r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			return 0xFF<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
		}
	case *image.NRGBA:
		return func(x, y int) uint32 {
			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
			return uint32(p[3])<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
		}
	case *image.RGBA:
		return func(x, y int) uint32 {
			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
			a := uint32(p[3])
			switch a {
			case 0:
				return 0
			case 0xFF:
				return 0xFF<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
			}
			un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
			return a<<24 | un(p[0])<<16 | un(p[1])<<8 | un(p[2])
		}
	case *image.Gray:
		return func(x, y int) uint32 {
			g := uint32(m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y)])
			return 0xFF<<24 | g<<16 | g<<8 | g
		}
	}
	return func(x, y int) uint32 {
		c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
		return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	}
}

// refBoxFilter averages the sw×sh source read by px down to w×h, weighting
// colour by alpha. Sums are 64-bit: a box can hold any number of pixels.
func refBoxFilter(sw, sh int, px func(x, y int) uint32, w, h int) *Image {
	out := NewImage(w, h)
	if w == sw && h == sh {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Pix[y*w+x] = px(x, y)
			}
		}
		return out
	}
	for y := 0; y < h; y++ {
		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			sx0, sx1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var a, r, g, b, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					p := px(sx, sy)
					pa := uint64(p >> 24)
					a += pa
					r += uint64(p>>16&0xFF) * pa
					g += uint64(p>>8&0xFF) * pa
					b += uint64(p&0xFF) * pa
					n++
				}
			}
			if a == 0 {
				continue
			}
			out.Pix[y*w+x] = uint32(a/n)<<24 | uint32(r/a)<<16 | uint32(g/a)<<8 | uint32(b/a)
		}
	}
	return out
}

// randomImages has one random image of every source type, with a non-zero
// origin and every chroma subsampling for YCbCr.
func randomImages(r *rand.Rand, w, h int) map[string]image.Image {
	rect := image.Rect(3, 5, 3+w, 5+h)
	fill := func(pix []uint8) {
		for i := range pix {
			pix[i] = uint8(r.Intn(256))
		}
	}
	out := map[string]image.Image{}
	nrgba, rgba, gray := image.NewNRGBA(rect), image.NewRGBA(rect), image.NewGray(rect)
	fill(nrgba.Pix)
	fill(gray.Pix)
	fill(rgba.Pix)
	for i := 0; i < len(rgba.Pix); i += 4 { // premultiplied: colour <= alpha
		for c := 0; c < 3; c++ {
			rgba.Pix[i+c] = uint8(int(rgba.Pix[i+c]) * int(rgba.Pix[i+3]) / 255)
		}
		if r.Intn(4) == 0 {
			rgba.Pix[i+3], nrgba.Pix[i+3] = 255, 255
		}
	}
	out["nrgba"], out["rgba"], out["gray"] = nrgba, rgba, gray
	nrgbaO, rgbaO := image.NewNRGBA(rect), image.NewRGBA(rect)
	fill(nrgbaO.Pix)
	fill(rgbaO.Pix)
	for i := 3; i < len(nrgbaO.Pix); i += 4 {
		nrgbaO.Pix[i], rgbaO.Pix[i] = 255, 255
	}
	out["nrgbaOpaque"], out["rgbaOpaque"] = nrgbaO, rgbaO
	g16, cmyk := image.NewGray16(rect), image.NewCMYK(rect)
	fill(g16.Pix)
	fill(cmyk.Pix)
	out["gray16"], out["cmyk"] = g16, cmyk
	for _, sr := range []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410} {
		y := image.NewYCbCr(rect, sr)
		fill(y.Y)
		fill(y.Cb)
		fill(y.Cr)
		out["ycbcr"+sr.String()] = y
	}
	pal := image.NewPaletted(rect, color.Palette{color.Black, color.White, color.NRGBA{200, 10, 90, 128}, color.Transparent})
	for i := range pal.Pix {
		pal.Pix[i] = uint8(r.Intn(4))
	}
	out["paletted"] = pal
	return out
}

func TestBoxFilterMatchesTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, sz := range [][2]int{{61, 47}, {8, 8}, {1, 30}, {30, 1}} {
		for name, m := range randomImages(r, sz[0], sz[1]) {
			b := m.Bounds()
			sw, sh := b.Dx(), b.Dy()
			for _, to := range [][2]int{{sw, sh}, {1, 1}, {7, 5}, {sw/2 + 1, sh/3 + 1}, {sw, 1}, {1, sh}, {sw + 9, sh + 4}} {
				got := boxFilter(sw, sh, rowsOf(m), make([]uint32, sw), isOpaque(m), to[0], to[1])
				want := refBoxFilter(sw, sh, refPixels(m), to[0], to[1])
				for i := range want.Pix {
					if got.Pix[i] != want.Pix[i] {
						t.Fatalf("%s %dx%d -> %dx%d: pixel %d = %08x, want %08x", name, sw, sh, to[0], to[1], i, got.Pix[i], want.Pix[i])
					}
				}
			}
		}
	}
}

func TestResizeMatchesTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	src := FromImage(randomImages(r, 53, 41)["nrgba"])
	for _, to := range [][2]int{{20, 10}, {53, 41}, {1, 1}, {90, 60}} {
		got := Resize(src, to[0], to[1])
		want := refBoxFilter(src.W, src.H, func(x, y int) uint32 { return src.Pix[y*src.W+x] }, to[0], to[1])
		for i := range want.Pix {
			if got.Pix[i] != want.Pix[i] {
				t.Fatalf("-> %dx%d: pixel %d = %08x, want %08x", to[0], to[1], i, got.Pix[i], want.Pix[i])
			}
		}
	}
}

func TestRecipsDivideExactly(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	var rc recips
	for k := uint64(1); k <= maxRecip+3; k++ {
		check := func(s uint64) {
			if s > 255*k {
				return
			}
			if got, want := rc.div(s, k), s/k; got != want {
				t.Fatalf("%d/%d = %d, want %d", s, k, got, want)
			}
		}
		for q := uint64(0); q <= 255; q++ { // the edges of every quotient
			check(q * k)
			check(q*k + k - 1)
			if q > 0 {
				check(q*k - 1)
			}
		}
		for range 2000 {
			check(uint64(r.Int63n(int64(255*k + 1))))
		}
	}
}

func TestBoxFilterBigBoxesMatchTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for name, m := range randomImages(r, 150, 130) {
		for _, to := range [][2]int{{1, 1}, {2, 3}, {5, 4}} { // boxes of hundreds to thousands of pixels
			got := boxFilter(150, 130, rowsOf(m), make([]uint32, 150), isOpaque(m), to[0], to[1])
			want := refBoxFilter(150, 130, refPixels(m), to[0], to[1])
			for i := range want.Pix {
				if got.Pix[i] != want.Pix[i] {
					t.Fatalf("%s -> %dx%d: pixel %d = %08x, want %08x", name, to[0], to[1], i, got.Pix[i], want.Pix[i])
				}
			}
		}
	}
}

// TestOpaqueFastPathIsUsedAndExact runs each opaque source through the
// opaque loop explicitly (and through the general loop) against the reference.
func TestOpaqueFastPathIsUsedAndExact(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	opaque := 0
	for _, sz := range [][2]int{{61, 47}, {150, 130}, {1, 30}, {30, 1}} {
		for name, m := range randomImages(r, sz[0], sz[1]) {
			b := m.Bounds()
			sw, sh := b.Dx(), b.Dy()
			if isOpaque(m) {
				opaque++
			} else if name != "nrgba" && name != "rgba" && name != "paletted" {
				t.Errorf("%s is not detected as opaque", name)
			}
			for _, to := range [][2]int{{1, 1}, {7, 5}, {sw/2 + 1, sh/3 + 1}, {sw + 5, sh + 3}, {sw, sh}} {
				want := refBoxFilter(sw, sh, refPixels(m), to[0], to[1])
				for _, fast := range []bool{false, isOpaque(m)} {
					got := boxFilter(sw, sh, rowsOf(m), make([]uint32, sw), fast, to[0], to[1])
					for i := range want.Pix {
						if got.Pix[i] != want.Pix[i] {
							t.Fatalf("%s opaque=%v %dx%d -> %dx%d: pixel %d = %08x, want %08x", name, fast, sw, sh, to[0], to[1], i, got.Pix[i], want.Pix[i])
						}
					}
				}
			}
		}
	}
	if opaque == 0 {
		t.Fatal("no opaque image tested")
	}
}

func TestOpaqueSumsFit(t *testing.T) {
	cols := func(sw, w int) (a, b []int) {
		a, b = make([]int, w), make([]int, w)
		for x := range w {
			a[x], b[x] = x*sw/w, max((x+1)*sw/w, x*sw/w+1)
		}
		return
	}
	for _, c := range []struct {
		sw, sh, w, h int
		fit          bool
	}{{2000, 2000, 600, 600, true}, {4096, 4096, 1, 1, true}, {5000, 4000, 1, 1, false}, {8000, 6000, 1, 1, false}, {8000, 6000, 8, 6, true}} {
		a, b := cols(c.sw, c.w)
		if got := opaqueSumsFit(c.sw, c.sh, c.w, c.h, a, b); got != c.fit {
			t.Errorf("%dx%d -> %dx%d: fit = %v, want %v", c.sw, c.sh, c.w, c.h, got, c.fit)
		}
	}
}
