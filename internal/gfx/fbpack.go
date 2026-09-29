package gfx

import "fmt"

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
func (f fbFormat) pack(mem []byte, c *Canvas) {
	for y := 0; y < f.height; y++ {
		src := c.Pix[y*c.W : y*c.W+f.width]
		line := mem[y*f.stride:]
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
