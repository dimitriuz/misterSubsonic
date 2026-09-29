package gfx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // cover art formats
	_ "image/png"
)

// MaxDecodePixels guards against decompression bombs in cover art.
const MaxDecodePixels = 4096 * 4096

// DecodeImage decodes JPEG or PNG bytes and scales the result to fit within
// maxW×maxH (aspect preserved, never upscaled) with a box filter.
func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxDecodePixels {
		return nil, fmt.Errorf("gfx: image %dx%d too large", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	return Fit(FromImage(m), maxW, maxH), nil
}

// Fit returns src scaled down to fit maxW×maxH, preserving aspect ratio.
func Fit(src *Image, maxW, maxH int) *Image {
	if src.W <= maxW && src.H <= maxH {
		return src
	}
	w, h := maxW, src.H*maxW/src.W
	if h > maxH {
		h, w = maxH, src.W*maxH/src.H
	}
	return Resize(src, max(w, 1), max(h, 1))
}

// Resize box-filters src down to w×h (use Canvas.Blit for upscaling).
func Resize(src *Image, w, h int) *Image {
	out := NewImage(w, h)
	for y := 0; y < h; y++ {
		sy0, sy1 := y*src.H/h, max((y+1)*src.H/h, y*src.H/h+1)
		for x := 0; x < w; x++ {
			sx0, sx1 := x*src.W/w, max((x+1)*src.W/w, x*src.W/w+1)
			var a, r, g, b, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					p := src.Pix[sy*src.W+sx]
					pa := p >> 24
					a += pa
					r += (p >> 16 & 0xFF) * pa
					g += (p >> 8 & 0xFF) * pa
					b += (p & 0xFF) * pa
					n++
				}
			}
			if a == 0 {
				continue
			}
			out.Pix[y*w+x] = (a/n)<<24 | (r/a)<<16 | (g/a)<<8 | b/a
		}
	}
	return out
}
