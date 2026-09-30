// Package gfx is the software renderer: a 32-bit canvas with fills, image
// blits and text, plus the displays that show it (fbdev, headless, viewer).
package gfx

import (
	"image"
)

// Color is 0xAARRGGBB. Alpha 0xFF is opaque.
type Color uint32

func RGB(r, g, b uint8) Color           { return 0xFF000000 | Color(r)<<16 | Color(g)<<8 | Color(b) }
func RGBA(r, g, b, a uint8) Color       { return Color(a)<<24 | Color(r)<<16 | Color(g)<<8 | Color(b) }
func (c Color) A() uint32               { return uint32(c) >> 24 }
func (c Color) WithAlpha(a uint8) Color { return Color(a)<<24 | c&0xFFFFFF }

type Rect struct{ X, Y, W, H int }

func R(x, y, w, h int) Rect { return Rect{x, y, w, h} }
func (r Rect) Empty() bool  { return r.W <= 0 || r.H <= 0 }
func (r Rect) Right() int   { return r.X + r.W }
func (r Rect) Bottom() int  { return r.Y + r.H }
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.Right() && y < r.Bottom()
}

// Intersect returns the overlap of r and o (empty if none).
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.Right(), o.Right()), min(r.Bottom(), o.Bottom())
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Inset shrinks r by d on every side.
func (r Rect) Inset(d int) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

// Canvas is an opaque XRGB8888 pixel buffer (0x00RRGGBB, row-major).
type Canvas struct {
	W, H int
	Pix  []uint32
}

func NewCanvas(w, h int) *Canvas { return &Canvas{W: w, H: h, Pix: make([]uint32, w*h)} }

func (c *Canvas) Bounds() Rect { return Rect{0, 0, c.W, c.H} }

func (c *Canvas) At(x, y int) Color { return Color(c.Pix[y*c.W+x]) | 0xFF000000 }

// Clear fills the whole canvas with an opaque color.
func (c *Canvas) Clear(col Color) {
	v := uint32(col) & 0xFFFFFF
	for i := range c.Pix {
		c.Pix[i] = v
	}
}

// Fill paints r, blending when col has alpha < 255.
func (c *Canvas) Fill(r Rect, col Color) {
	r = r.Intersect(c.Bounds())
	a := col.A()
	if r.Empty() || a == 0 {
		return
	}
	v := uint32(col) & 0xFFFFFF
	for y := r.Y; y < r.Bottom(); y++ {
		row := c.Pix[y*c.W+r.X : y*c.W+r.Right()]
		if a == 255 {
			for i := range row {
				row[i] = v
			}
			continue
		}
		for i := range row {
			row[i] = blend(row[i], v, a)
		}
	}
}

// blend mixes src over dst with coverage a (0..255).
func blend(dst, src, a uint32) uint32 {
	inv := 255 - a
	rb := ((src&0xFF00FF)*a + (dst&0xFF00FF)*inv + 0x800080) >> 8 & 0xFF00FF
	g := ((src&0x00FF00)*a + (dst&0x00FF00)*inv + 0x008000) >> 8 & 0x00FF00
	return rb | g
}

// Image is a straight-alpha ARGB image (0xAARRGGBB).
type Image struct {
	W, H int
	Pix  []uint32
}

func NewImage(w, h int) *Image { return &Image{W: w, H: h, Pix: make([]uint32, w*h)} }

// Blit draws src scaled (nearest neighbour) into dst, clipped to the canvas.
func (c *Canvas) Blit(src *Image, dst Rect) {
	clip := dst.Intersect(c.Bounds())
	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 {
		return
	}
	if src.W == dst.W && src.H == dst.H {
		// Drawn at its own size (covers are decoded to fit): no index
		// table, no division. The pixel logic stays inline; a helper
		// call per pixel costs more than it saves on the A9.
		for y := clip.Y; y < clip.Bottom(); y++ {
			sy := y - dst.Y
			srow := src.Pix[sy*src.W+clip.X-dst.X : sy*src.W+clip.Right()-dst.X]
			drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
			for i, p := range srow {
				switch a := p >> 24; a {
				case 0:
				case 255:
					drow[i] = p & 0xFFFFFF
				default:
					drow[i] = blend(drow[i], p&0xFFFFFF, a)
				}
			}
		}
		return
	}
	xmap := make([]int, clip.W)
	for i := range xmap {
		xmap[i] = (clip.X + i - dst.X) * src.W / dst.W
	}
	for y := clip.Y; y < clip.Bottom(); y++ {
		sy := (y - dst.Y) * src.H / dst.H
		srow := src.Pix[sy*src.W : (sy+1)*src.W]
		drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
		for i, sx := range xmap {
			p := srow[sx]
			switch a := p >> 24; a {
			case 0:
			case 255:
				drow[i] = p & 0xFFFFFF
			default:
				drow[i] = blend(drow[i], p&0xFFFFFF, a)
			}
		}
	}
}

// ToRGBA converts the canvas to a standard image (for PNG output).
func (c *Canvas) ToRGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for i, p := range c.Pix {
		img.Pix[4*i] = uint8(p >> 16)
		img.Pix[4*i+1] = uint8(p >> 8)
		img.Pix[4*i+2] = uint8(p)
		img.Pix[4*i+3] = 0xFF
	}
	return img
}
