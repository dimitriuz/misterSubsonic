package gfx

import (
	"fmt"
	"unsafe"
)

// littleEndian: a canvas pixel 0x00RRGGBB then lies in memory exactly as
// XRGB8888 stores it (B, G, R, X), so a 32 bpp frame is a plain copy.
var littleEndian = func() bool { v := uint16(1); return *(*byte)(unsafe.Pointer(&v)) == 1 }()

// fbFormat describes a linear framebuffer's memory layout.
type fbFormat struct {
	width, height int
	stride        int // bytes per line
	bpp           int // 16 (RGB565) or 32 (XRGB8888, little-endian)
}

func (f fbFormat) validate() error {
	if f.bpp != 16 && f.bpp != 32 {
		return fmt.Errorf("gfx: unsupported framebuffer depth %d bpp (need 16 or 32)", f.bpp)
	}
	if f.width <= 0 || f.height <= 0 || f.stride < f.width*f.bpp/8 {
		return fmt.Errorf("gfx: bad framebuffer geometry %dx%d stride %d", f.width, f.height, f.stride)
	}
	return nil
}

// pack writes c (same size as the framebuffer) into mem.
func (f fbFormat) pack(mem []byte, c *Canvas) { f.packRect(mem, c, Rect{0, 0, f.width, f.height}) }

// packRect writes the part of c inside r into mem, leaving the rest as it is.
func (f fbFormat) packRect(mem []byte, c *Canvas, r Rect) {
	r = r.Intersect(Rect{0, 0, f.width, f.height})
	if r.Empty() {
		return
	}
	for y := r.Y; y < r.Bottom(); y++ {
		src := c.Pix[y*c.W+r.X : y*c.W+r.Right()]
		line := mem[y*f.stride+r.X*f.bpp/8:]
		if f.bpp == 32 && littleEndian {
			copy(line[:4*r.W], unsafe.Slice((*byte)(unsafe.Pointer(&src[0])), 4*r.W))
			continue
		}
		if f.bpp == 32 {
			for x, p := range src {
				o := 4 * x
				line[o] = byte(p)
				line[o+1] = byte(p >> 8)
				line[o+2] = byte(p >> 16)
				line[o+3] = 0
			}
			continue
		}
		for x, p := range src {
			v := uint16(p>>8&0xF800) | uint16(p>>5&0x07E0) | uint16(p>>3&0x001F)
			line[2*x] = byte(v)
			line[2*x+1] = byte(v >> 8)
		}
	}
}

// Sampling grid for matches: 17×17 points, corners and edges included.
const matchGrid = 16

// matches reports whether mem still holds c at a grid of sample points.
func (f fbFormat) matches(mem []byte, c *Canvas) bool {
	for gy := 0; gy <= matchGrid; gy++ {
		y := min(gy*f.height/matchGrid, f.height-1)
		for gx := 0; gx <= matchGrid; gx++ {
			x := min(gx*f.width/matchGrid, f.width-1)
			if f.read(mem, x, y) != f.encode(c.Pix[y*c.W+x]) {
				return false
			}
		}
	}
	return true
}

// encode is a canvas pixel as the framebuffer stores it (XRGB8888 with X
// zero, or RGB565).
func (f fbFormat) encode(p uint32) uint32 {
	if f.bpp == 32 {
		return p & 0xFFFFFF
	}
	return p>>8&0xF800 | p>>5&0x07E0 | p>>3&0x001F
}

// read is the stored pixel at (x, y), in encode's form.
func (f fbFormat) read(mem []byte, x, y int) uint32 {
	o := y*f.stride + x*f.bpp/8
	if f.bpp == 32 {
		return uint32(mem[o]) | uint32(mem[o+1])<<8 | uint32(mem[o+2])<<16
	}
	return uint32(mem[o]) | uint32(mem[o+1])<<8
}
