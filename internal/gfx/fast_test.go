package gfx

import (
	"math/rand/v2"
	"testing"
)

// The fast drawing paths give exactly what a plain per-pixel loop gives.

func randomCanvas(r *rand.Rand, w, h int) *Canvas {
	c := NewCanvas(w, h)
	for i := range c.Pix {
		c.Pix[i] = r.Uint32() & 0xFFFFFF
	}
	return c
}

func TestClearAndFillMatchAPixelLoop(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for n := range 200 {
		c := randomCanvas(r, 37, 23)
		want := &Canvas{W: c.W, H: c.H, Pix: append([]uint32(nil), c.Pix...)}
		rect := R(r.IntN(50)-10, r.IntN(40)-10, r.IntN(50), r.IntN(40))
		col := Color(r.Uint32())
		if n%2 == 0 {
			col |= 0xFF000000 // opaque: the copied-rows path
		}
		c.Fill(rect, col)
		cl := rect.Intersect(want.Bounds())
		for y := cl.Y; y < cl.Bottom(); y++ {
			for x := cl.X; x < cl.Right(); x++ {
				i := y*want.W + x
				if a := col.A(); a == 255 {
					want.Pix[i] = uint32(col) & 0xFFFFFF
				} else if a > 0 {
					want.Pix[i] = blend(want.Pix[i], uint32(col)&0xFFFFFF, a)
				}
			}
		}
		for i := range want.Pix {
			if c.Pix[i] != want.Pix[i] {
				t.Fatalf("fill %v %08x: pixel %d = %06x, want %06x", rect, uint32(col), i, c.Pix[i], want.Pix[i])
			}
		}
	}
	c := randomCanvas(r, 1000, 3)
	c.Clear(0xFF123456)
	for i, p := range c.Pix {
		if p != 0x123456 {
			t.Fatalf("clear: pixel %d = %06x", i, p)
		}
	}
}

// A one-to-one blit, clipped or not, equals the general scaled path.
func TestOneToOneBlitMatchesTheScaledPath(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	img := NewImage(19, 13)
	for i := range img.Pix {
		img.Pix[i] = r.Uint32()
		if i%3 == 0 {
			img.Pix[i] |= 0xFF000000 // opaque
		}
		if i%7 == 0 {
			img.Pix[i] &= 0x00FFFFFF // transparent
		}
	}
	for _, at := range []Rect{R(5, 4, 19, 13), R(-6, -3, 19, 13), R(30, 15, 19, 13), R(0, 0, 19, 13)} {
		base := randomCanvas(r, 40, 25)
		fast := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
		fast.Blit(img, at)
		ref := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
		cl := at.Intersect(ref.Bounds())
		for y := cl.Y; y < cl.Bottom(); y++ {
			for x := cl.X; x < cl.Right(); x++ {
				i := y*ref.W + x
				ref.Pix[i] = refOver(ref.Pix[i], img.Pix[(y-at.Y)*img.W+x-at.X])
			}
		}
		for i := range ref.Pix {
			if fast.Pix[i] != ref.Pix[i] {
				t.Fatalf("blit at %v: pixel %d = %06x, want %06x", at, i, fast.Pix[i], ref.Pix[i])
			}
		}
	}
}

// The 32 bpp copy writes what the byte-by-byte packing wrote, padding untouched.
func TestPack32CopyMatchesBytePacking(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	c := randomCanvas(r, 13, 7)
	f := fbFormat{width: 13, height: 7, stride: 13*4 + 12, bpp: 32}
	got := make([]byte, f.stride*f.height)
	for i := range got {
		got[i] = 0xEE
	}
	want := append([]byte(nil), got...)
	f.pack(got, c)
	for y := 0; y < f.height; y++ {
		for x := 0; x < f.width; x++ {
			p, o := c.Pix[y*c.W+x], y*f.stride+4*x
			want[o], want[o+1], want[o+2], want[o+3] = byte(p), byte(p>>8), byte(p>>16), 0
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

// refOver is the straight-alpha pixel p drawn over the opaque d.
func refOver(d, p uint32) uint32 {
	switch a := p >> 24; a {
	case 0:
		return d
	case 255:
		return p & 0xFFFFFF
	default:
		return blend(d, p&0xFFFFFF, a)
	}
}
