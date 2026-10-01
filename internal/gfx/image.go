package gfx

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // cover art formats
	_ "image/png"
	"math"
)

// MaxDecodeBytes bounds the memory a cover may take while it is decoded,
// before it is scaled down: a 4096×4096 baseline JPEG fits, a 4096×4096 RGBA
// PNG doesn't, and progressive or CMYK JPEGs are refused sooner because their
// decode costs more (see jpegDecodeBytes). It guards against decompression
// bombs and keeps two art workers within about 100 MB on the MiSTer.
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

// jpegDecodeBytes estimates Go's JPEG decoder memory from the headers alone:
// the sample planes, four more bytes per sample for a progressive frame's DCT
// coefficients, and an RGBA copy when the decoder converts to one (CMYK, or
// RGB components: labelled R, G, B, or an Adobe APP14 segment with transform
// 0). It reports false if the markers don't lead cleanly from the frame
// header to the scan data.
func jpegDecodeBytes(data []byte) (int64, bool) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, false
	}
	var total, px int64
	var nc int
	var rgbIDs, found, adobeRGB bool
	i := 2
	for i+1 < len(data) {
		if data[i] != 0xFF {
			return 0, false
		}
		m := data[i+1]
		if m == 0xFF { // fill byte
			i++
			continue
		}
		if m == 0xD8 || m == 0x01 || m >= 0xD0 && m <= 0xD7 || m == 0 { // no length
			i += 2
			continue
		}
		if m == 0xD9 || m == 0xDA { // end of the headers
			if !found {
				return 0, false
			}
			if nc == 4 || nc == 3 && (rgbIDs || adobeRGB) {
				total += 4 * px
			}
			return total, true
		}
		if i+4 > len(data) {
			return 0, false
		}
		n := int(data[i+2])<<8 | int(data[i+3])
		seg := data[i+4:]
		if m == 0xEE && n >= 14 && len(seg) >= 12 && string(seg[:5]) == "Adobe" {
			adobeRGB = seg[11] == 0 // transform 0: the components are RGB
		}
		if m < 0xC0 || m > 0xCF || m == 0xC4 || m == 0xC8 || m == 0xCC {
			i += 2 + n
			continue
		}
		if found { // a second frame header: not a JPEG Go decodes
			return 0, false
		}
		if n < 8 || len(seg) < n-2 || len(seg) < 6 {
			return 0, false
		}
		h, w := int64(seg[1])<<8|int64(seg[2]), int64(seg[3])<<8|int64(seg[4])
		nc = int(seg[5])
		if nc == 0 || n < 8+3*nc || len(seg) < 6+3*nc {
			return 0, false
		}
		var sum, hmax, vmax int64
		for c := 0; c < nc; c++ {
			hv := seg[7+3*c]
			ch, cv := int64(hv>>4), int64(hv&15)
			if ch == 0 || cv == 0 {
				return 0, false
			}
			sum += ch * cv
			hmax, vmax = max(hmax, ch), max(vmax, cv)
		}
		px = w * h
		samples := px * sum / (hmax * vmax)
		total = samples
		if m == 0xC2 || m == 0xC6 || m == 0xCA || m == 0xCE {
			total += 4 * samples
		}
		rgbIDs = nc == 3 && seg[6] == 'R' && seg[9] == 'G' && seg[12] == 'B'
		found = true
		i += 2 + n
	}
	return 0, false
}

// DecodeImage decodes JPEG or PNG bytes and scales the result to fit within
// maxW×maxH (aspect preserved, never upscaled) with a box filter. Only the
// scaled result is converted: the full-size picture exists once, in the
// decoder's own format.
func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	need := decodedBytes(cfg)
	if format == "jpeg" {
		if n, ok := jpegDecodeBytes(data); ok {
			need = n
		} else { // unreadable markers: assume the decoder's RGBA copy (4 B/px)
			need = max(need, 4*int64(cfg.Width)*int64(cfg.Height))
		}
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || need > MaxDecodeBytes {
		return nil, fmt.Errorf("gfx: image %dx%d too large", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	b := m.Bounds()
	w, h := fitSize(b.Dx(), b.Dy(), maxW, maxH)
	return boxFilter(b.Dx(), b.Dy(), rowsOf(m), make([]uint32, b.Dx()), isOpaque(m), w, h), nil
}

// FromImage converts any image.Image to an Image.
func FromImage(m image.Image) *Image {
	b := m.Bounds()
	return boxFilter(b.Dx(), b.Dy(), rowsOf(m), make([]uint32, b.Dx()), isOpaque(m), b.Dx(), b.Dy())
}

// rowReader returns source row y as non-premultiplied ARGB. It may fill buf
// (sw long) and return it, or return its own storage, which must not be changed.
type rowReader func(y int, buf []uint32) []uint32

// rowsOf reads m's rows, counted from its top-left corner. The common decoder
// outputs are read directly, a row per call; other types go through At.
func rowsOf(m image.Image) rowReader {
	b := m.Bounds()
	w := b.Dx()
	switch m := m.(type) {
	case *image.YCbCr:
		var sh uint // horizontal chroma subsampling: one chroma sample per 1<<sh pixels
		switch m.SubsampleRatio {
		case image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420:
			sh = 1
		case image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410:
			sh = 2
		}
		cx := make([]int, w) // chroma column of each pixel, relative to the row's first
		for x := range cx {
			cx[x] = (b.Min.X+x)/(1<<sh) - b.Min.X/(1<<sh) // once, not per pixel: the A9 has no divide instruction
		}
		return func(y int, buf []uint32) []uint32 {
			y += b.Min.Y
			yp := m.Y[m.YOffset(b.Min.X, y):]
			c0 := m.COffset(b.Min.X, y)
			for x := range w {
				ci := c0 + cx[x]
				// color.YCbCrToRGB, inlined
				yy1 := int32(yp[x]) * 0x10101
				cb1 := int32(m.Cb[ci]) - 128
				cr1 := int32(m.Cr[ci]) - 128
				r := yy1 + 91881*cr1
				if uint32(r)&0xff000000 == 0 {
					r >>= 16
				} else {
					r = ^(r >> 31)
				}
				g := yy1 - 22554*cb1 - 46802*cr1
				if uint32(g)&0xff000000 == 0 {
					g >>= 16
				} else {
					g = ^(g >> 31)
				}
				bl := yy1 + 116130*cb1
				if uint32(bl)&0xff000000 == 0 {
					bl >>= 16
				} else {
					bl = ^(bl >> 31)
				}
				buf[x] = 0xFF<<24 | uint32(uint8(r))<<16 | uint32(uint8(g))<<8 | uint32(uint8(bl))
			}
			return buf
		}
	case *image.NRGBA:
		return func(y int, buf []uint32) []uint32 {
			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := range w {
				q := p[4*x : 4*x+4 : 4*x+4]
				buf[x] = uint32(q[3])<<24 | uint32(q[0])<<16 | uint32(q[1])<<8 | uint32(q[2])
			}
			return buf
		}
	case *image.RGBA:
		return func(y int, buf []uint32) []uint32 {
			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := range w {
				q := p[4*x : 4*x+4 : 4*x+4]
				a := uint32(q[3])
				switch a {
				case 0:
					buf[x] = 0
				case 0xFF:
					buf[x] = 0xFF<<24 | uint32(q[0])<<16 | uint32(q[1])<<8 | uint32(q[2])
				default:
					un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
					buf[x] = a<<24 | un(q[0])<<16 | un(q[1])<<8 | un(q[2])
				}
			}
			return buf
		}
	case *image.Gray:
		return func(y int, buf []uint32) []uint32 {
			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
			for x := range w {
				g := uint32(p[x])
				buf[x] = 0xFF<<24 | g<<16 | g<<8 | g
			}
			return buf
		}
	}
	return func(y int, buf []uint32) []uint32 {
		for x := range w {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			buf[x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
		return buf
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

// Resize box-filters src down to w×h (use Canvas.Blit for upscaling). Its
// rows are read in place, so no row buffer is needed.
func Resize(src *Image, w, h int) *Image {
	return boxFilter(src.W, src.H, func(y int, _ []uint32) []uint32 { return src.Pix[y*src.W : (y+1)*src.W] }, nil, false, w, h)
}

// isOpaque reports whether every pixel of m has alpha 0xFF (then rowsOf
// yields 0xFF alpha for all of them). JPEG covers are: YCbCr, Gray.
func isOpaque(m image.Image) bool {
	o, ok := m.(interface{ Opaque() bool })
	return ok && o.Opaque()
}

// boxFilter averages the sw×sh source read by rows down to w×h, weighting
// colour by alpha; opaque says every source pixel has alpha 0xFF, which
// allows a cheaper loop. buf (sw long) is the scratch row handed to rows; nil
// when rows needs none. Sums are 64-bit: a box can hold any number of pixels.
func boxFilter(sw, sh int, rows rowReader, buf []uint32, opaque bool, w, h int) *Image {
	out := NewImage(w, h)
	if w == sw && h == sh {
		for y := 0; y < h; y++ {
			copy(out.Pix[y*w:(y+1)*w], rows(y, buf))
		}
		return out
	}
	sx0, sx1 := make([]int, w), make([]int, w)
	for x := range w {
		sx0[x], sx1[x] = x*sw/w, max((x+1)*sw/w, x*sw/w+1)
	}
	if opaque && opaqueSumsFit(sw, sh, w, h, sx0, sx1) {
		boxFilterOpaque(sh, rows, buf, w, h, sx0, sx1, out)
		return out
	}
	// per output column: alpha sum and alpha-weighted colour sums
	acc := make([][4]uint64, w)
	var rc recips
	for y := 0; y < h; y++ {
		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		clear(acc)
		for sy := sy0; sy < sy1; sy++ {
			row := rows(sy, buf)
			for x := range w {
				var a, r, g, b uint64
				for _, p := range row[sx0[x]:sx1[x]] {
					pa := uint64(p >> 24)
					a += pa
					r += uint64(p>>16&0xFF) * pa
					g += uint64(p>>8&0xFF) * pa
					b += uint64(p&0xFF) * pa
				}
				c := &acc[x]
				c[0] += a
				c[1] += r
				c[2] += g
				c[3] += b
			}
		}
		for x := range w {
			a, r, g, b := acc[x][0], acc[x][1], acc[x][2], acc[x][3]
			if a == 0 {
				continue
			}
			n := uint64(sy1-sy0) * uint64(sx1[x]-sx0[x])
			out.Pix[y*w+x] = uint32(rc.div(a, n))<<24 | uint32(rc.div(r, a))<<16 | uint32(rc.div(g, a))<<8 | uint32(rc.div(b, a))
		}
	}
	return out
}

// maxRecip is the largest divisor recips handles: 255*k*k must stay below 1<<32.
const maxRecip = 4104

// recips divides by small numbers with a multiply and a shift, which the
// MiSTer's Cortex-A9 (no divide instruction: every / is a library call, 64-bit
// ones a slow one) does much faster. inv[k] is 1<<32/k+1, filled on first use.
type recips struct{ inv [maxRecip + 1]uint32 }

// div returns s/k for s <= 255*k. With inv = (1<<32+e)/k, 1 <= e <= k, the
// product s*inv>>32 is s/k plus s*e/(k<<32), which is below 1/k when
// s*e < 1<<32, and that holds because s*e <= 255*k*k <= 255*maxRecip² < 1<<32:
// so the floor is exact. Larger divisors are divided the slow way.
func (r *recips) div(s, k uint64) uint64 {
	if k == 1 { // 1<<32+1 does not fit inv
		return s
	}
	if k > maxRecip {
		return s / k
	}
	if r.inv[k] == 0 {
		r.inv[k] = uint32((1<<32)/k + 1)
	}
	return uint64(uint32(s)) * uint64(r.inv[k]) >> 32
}

// opaqueSumsFit reports whether the colour sums of the largest box fit in 32
// bits: 255 per pixel of the box. A 4:2:0 cover of MaxDecodeBytes (48 MB) is
// about 32M pixels, so a Gray one can exceed it when scaled to a few pixels.
func opaqueSumsFit(sw, sh, w, h int, sx0, sx1 []int) bool {
	cols, rows := 0, 0
	for x := range sx0 {
		cols = max(cols, sx1[x]-sx0[x])
	}
	for y := range h {
		rows = max(rows, max((y+1)*sh/h, y*sh/h+1)-y*sh/h)
	}
	return 255*int64(cols)*int64(rows) <= math.MaxUint32
}

// boxFilterOpaque is boxFilter for a source without transparency: with alpha
// 255 everywhere the alpha-weighted average is the plain one (r*255/(n*255) is
// the floor of the plain sum over n) and the result's alpha is 255, so the
// output is the same, with 32-bit sums and no multiply per pixel.
func boxFilterOpaque(sh int, rows rowReader, buf []uint32, w, h int, sx0, sx1 []int, out *Image) {
	acc := make([][3]uint32, w)
	var rc recips
	for y := 0; y < h; y++ {
		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		clear(acc)
		for sy := sy0; sy < sy1; sy++ {
			row := rows(sy, buf)
			for x := range w {
				var r, g, b uint32
				for _, p := range row[sx0[x]:sx1[x]] {
					r += p >> 16 & 0xFF
					g += p >> 8 & 0xFF
					b += p & 0xFF
				}
				c := &acc[x]
				c[0] += r
				c[1] += g
				c[2] += b
			}
		}
		for x := range w {
			n := uint64(sy1-sy0) * uint64(sx1[x]-sx0[x])
			out.Pix[y*w+x] = 0xFF<<24 | uint32(rc.div(uint64(acc[x][0]), n))<<16 | uint32(rc.div(uint64(acc[x][1]), n))<<8 | uint32(rc.div(uint64(acc[x][2]), n))
		}
	}
}
