package gfx

import (
	"math/rand/v2"
	"testing"
)

// Every drawing call writes only inside the clip: inside it the result is
// what an unclipped call gives, outside it the canvas is untouched. This is
// what lets the UI redraw just the parts of a frame that changed.
func TestClipLimitsEveryPrimitive(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	f := testFont(t, 18)
	img := NewImage(40, 30)
	for i := range img.Pix {
		img.Pix[i] = r.Uint32() // every alpha, opaque and transparent included
	}
	draws := map[string]func(c *Canvas){
		"fill opaque":  func(c *Canvas) { c.Fill(R(5, 5, 90, 50), RGB(10, 200, 30)) },
		"fill blended": func(c *Canvas) { c.Fill(R(-10, 20, 200, 30), RGBA(200, 10, 30, 0x80)) },
		"blit 1:1":     func(c *Canvas) { c.Blit(img, R(20, 10, 40, 30)) },
		"blit scaled":  func(c *Canvas) { c.Blit(img, R(0, 0, 100, 70)) },
		"text":         func(c *Canvas) { f.Draw(c, 3, 40, "Капитан Africa", RGB(250, 250, 250), c.Bounds()) },
		"text blended": func(c *Canvas) { f.Draw(c, 3, 60, "Капитан Africa", RGBA(250, 250, 250, 0x90), c.Bounds()) },
		"clear":        func(c *Canvas) { c.Clear(RGB(1, 2, 3)) },
	}
	clips := []Rect{R(30, 15, 25, 20), R(-5, -5, 20, 20), R(90, 60, 50, 50), R(0, 0, 0, 0)}
	for name, draw := range draws {
		for _, clip := range clips {
			base := randomCanvas(r, 100, 70)
			want := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
			draw(want)
			got := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
			got.SetClip(clip)
			draw(got)
			in := clip.Intersect(base.Bounds())
			for y := range base.H {
				for x := range base.W {
					exp := base.Pix[y*base.W+x]
					if in.Contains(x, y) {
						exp = want.Pix[y*base.W+x]
					}
					if g := got.Pix[y*base.W+x]; g != exp {
						t.Fatalf("%s, clip %v: pixel (%d,%d) = %06x, want %06x", name, clip, x, y, g, exp)
					}
				}
			}
		}
	}
}

func TestClipStaysInsideTheCanvas(t *testing.T) {
	c := NewCanvas(100, 50)
	if c.Clip() != c.Bounds() {
		t.Fatalf("a new canvas clips to %v", c.Clip())
	}
	c.SetClip(R(80, -10, 50, 30))
	if c.Clip() != R(80, 0, 20, 20) {
		t.Fatalf("clip %v, want it cut to the canvas", c.Clip())
	}
	c.ClearClip()
	if c.Clip() != c.Bounds() {
		t.Fatalf("after ClearClip the clip is %v", c.Clip())
	}
}

// packRect copies only its rectangle: inside, the framebuffer holds what a
// full pack gives; outside (and the line padding) it keeps what it had.
func TestPackRectCopiesOnlyItsRectangle(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, bpp := range []int{32, 16} {
		f := fbFormat{width: 50, height: 30, stride: 50*bpp/8 + 12, bpp: bpp}
		old, next := randomCanvas(r, 50, 30), randomCanvas(r, 50, 30)
		for _, rect := range []Rect{R(10, 5, 20, 10), R(-5, 25, 100, 20), R(0, 0, 50, 30), R(49, 0, 1, 1)} {
			mem := make([]byte, f.stride*f.height)
			for i := range mem {
				mem[i] = 0xEE
			}
			f.pack(mem, old)
			f.packRect(mem, next, rect)
			full := make([]byte, len(mem))
			for i := range full {
				full[i] = 0xEE
			}
			f.pack(full, next)
			in := rect.Intersect(R(0, 0, 50, 30))
			for y := range f.height {
				for x := range f.width {
					want := f.encode(old.Pix[y*50+x])
					if in.Contains(x, y) {
						want = f.read(full, x, y)
					}
					if got := f.read(mem, x, y); got != want {
						t.Fatalf("%d bpp, rect %v: pixel (%d,%d) = %x, want %x", bpp, rect, x, y, got, want)
					}
				}
				for i := f.width * bpp / 8; i < f.stride; i++ {
					if mem[y*f.stride+i] != 0xEE {
						t.Fatalf("%d bpp: line %d padding written", bpp, y)
					}
				}
			}
		}
	}
}
