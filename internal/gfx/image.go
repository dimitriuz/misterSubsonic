package gfx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // cover art formats
	_ "image/png"
)

// MaxDecodeBytes bounds the memory a cover may take while it is decoded,
// before it is scaled down: a 4096×4096 JPEG fits, a 4096×4096 RGBA PNG
// doesn't. It guards against decompression bombs and keeps two art workers
// within about 100 MB on the MiSTer.
const MaxDecodeBytes = 48 << 20

// decodedBytes estimates what the decoder allocates for an image of cfg.
// YCbCr counts as 4:4:4 (the worst case); 4:2:0 needs half of that.
func decodedBytes(cfg image.Config) int64 {
	px := int64(cfg.Width) * int64(cfg.Height)
	if _, ok := cfg.ColorModel.(color.Palette); ok {
		return px
	}
	switch cfg.ColorModel {
	case color.GrayModel, color.AlphaModel:
		return px
	case color.Gray16Model, color.Alpha16Model:
		return 2 * px
	case color.YCbCrModel:
		return 3 * px
	case color.RGBA64Model, color.NRGBA64Model:
		return 8 * px
	}
	return 4 * px // RGBA, NRGBA, CMYK
}

// DecodeImage decodes JPEG or PNG bytes and scales the result to fit within
// maxW×maxH (aspect preserved, never upscaled) with a box filter. Only the
// scaled result is converted: the full-size picture exists once, in the
// decoder's own format.
func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || decodedBytes(cfg) > MaxDecodeBytes {
		return nil, fmt.Errorf("gfx: image %dx%d too large", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	b := m.Bounds()
	w, h := fitSize(b.Dx(), b.Dy(), maxW, maxH)
	return boxFilter(b.Dx(), b.Dy(), pixels(m), w, h), nil
}

// FromImage converts any image.Image to an Image.
func FromImage(m image.Image) *Image {
	b := m.Bounds()
	return boxFilter(b.Dx(), b.Dy(), pixels(m), b.Dx(), b.Dy())
}

// pixels reads m's pixel (x, y), counted from its top-left corner, as
// non-premultiplied ARGB. The common decoder outputs are read directly; other
// types go through At.
func pixels(m image.Image) func(x, y int) uint32 {
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

// fitSize is sw×sh scaled down to fit maxW×maxH, preserving aspect ratio.
func fitSize(sw, sh, maxW, maxH int) (int, int) {
	if sw <= maxW && sh <= maxH {
		return sw, sh
	}
	w, h := maxW, sh*maxW/sw
	if h > maxH {
		h, w = maxH, sw*maxH/sh
	}
	return max(w, 1), max(h, 1)
}

// Fit returns src scaled down to fit maxW×maxH, preserving aspect ratio.
func Fit(src *Image, maxW, maxH int) *Image {
	w, h := fitSize(src.W, src.H, maxW, maxH)
	if w == src.W && h == src.H {
		return src
	}
	return Resize(src, w, h)
}

// Resize box-filters src down to w×h (use Canvas.Blit for upscaling).
func Resize(src *Image, w, h int) *Image {
	return boxFilter(src.W, src.H, func(x, y int) uint32 { return src.Pix[y*src.W+x] }, w, h)
}

// boxFilter averages the sw×sh source read by px down to w×h, weighting
// colour by alpha. Sums are 64-bit: a box can hold any number of pixels.
func boxFilter(sw, sh int, px func(x, y int) uint32, w, h int) *Image {
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
