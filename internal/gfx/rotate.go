package gfx

import "math"

// BlitRotated draws src scaled to fill dst and turned clockwise by angle
// (radians) about dst's centre, smoothed bilinearly. What lies outside src
// after the turn is left alone, so a round image stays inside dst.
func (c *Canvas) BlitRotated(src *Image, dst Rect, angle float64) {
	clip := dst.Intersect(c.Clip())
	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 || dst.W == 0 || dst.H == 0 {
		return
	}
	sin, cos := math.Sincos(angle)
	const one = 1 << 16
	// Source steps per destination pixel, 16.16 fixed point.
	kx := float64(src.W) / float64(dst.W) * one
	ky := float64(src.H) / float64(dst.H) * one
	dux, dvx := int(cos*kx), int(-sin*ky) // per step right
	duy, dvy := int(sin*kx), int(cos*ky)  // per step down
	cx, cy := float64(dst.W)/2, float64(dst.H)/2
	sw, sh := src.W<<16, src.H<<16
	// The sample point is the pixel's centre, less half a source pixel so
	// the weights below are between pixel centres.
	x0, y0 := float64(clip.X-dst.X)+0.5-cx, float64(clip.Y-dst.Y)+0.5-cy
	u0 := int((x0*cos+y0*sin)*kx) + sw/2 - one/2
	v0 := int((-x0*sin+y0*cos)*ky) + sh/2 - one/2
	for y := clip.Y; y < clip.Bottom(); y++ {
		n := y - clip.Y
		u, v := u0+n*duy, v0+n*dvy
		drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
		for i := range drow {
			// Inside the source, with the sample's four pixels in range.
			if u > -one && v > -one && u < sw && v < sh {
				ix, iy := u>>16, v>>16
				fx, fy := uint32(u>>12)&15, uint32(v>>12)&15 // 4-bit weights
				x1, y1 := min(ix+1, src.W-1), min(iy+1, src.H-1)
				ix, iy = max(ix, 0), max(iy, 0)
				p00, p10 := src.Pix[iy*src.W+ix], src.Pix[iy*src.W+x1]
				p01, p11 := src.Pix[y1*src.W+ix], src.Pix[y1*src.W+x1]
				w00, w10 := (16-fx)*(16-fy), fx*(16-fy)
				w01, w11 := (16-fx)*fy, fx*fy
				if p00&p10&p01&p11>>24 == 255 { // all opaque: the common case
					rb := ((p00&0xFF00FF)*w00 + (p10&0xFF00FF)*w10 + (p01&0xFF00FF)*w01 + (p11&0xFF00FF)*w11) >> 8 & 0xFF00FF
					g := ((p00&0x00FF00)*w00 + (p10&0x00FF00)*w10 + (p01&0x00FF00)*w01 + (p11&0x00FF00)*w11) >> 8 & 0x00FF00
					drow[i] = rb | g
				} else if den := (p00>>24)*w00 + (p10>>24)*w10 + (p01>>24)*w01 + (p11>>24)*w11; den > 0 {
					// Colours weighted by alpha, so a transparent
					// pixel's colour doesn't bleed into the edge.
					a00, a10, a01, a11 := (p00>>24)*w00, (p10>>24)*w10, (p01>>24)*w01, (p11>>24)*w11
					ch := func(sh uint) uint32 {
						return ((p00>>sh&255)*a00 + (p10>>sh&255)*a10 + (p01>>sh&255)*a01 + (p11>>sh&255)*a11) / den
					}
					col := ch(16)<<16 | ch(8)<<8 | ch(0)
					if a := den >> 8; a >= 255 {
						drow[i] = col
					} else {
						drow[i] = blend(drow[i], col, a)
					}
				}
			}
			u += dux
			v += dvx
		}
	}
}

// BlitRotatedFast is BlitRotated with the nearest source pixel instead of a
// bilinear blend: about three times cheaper, for a picture that turns often.
func (c *Canvas) BlitRotatedFast(src *Image, dst Rect, angle float64) {
	clip := dst.Intersect(c.Clip())
	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 || dst.W == 0 || dst.H == 0 {
		return
	}
	sin, cos := math.Sincos(angle)
	const one = 1 << 16
	kx := float64(src.W) / float64(dst.W) * one
	ky := float64(src.H) / float64(dst.H) * one
	dux, dvx := int(cos*kx), int(-sin*ky)
	duy, dvy := int(sin*kx), int(cos*ky)
	cx, cy := float64(dst.W)/2, float64(dst.H)/2
	sw, sh := src.W<<16, src.H<<16
	x0, y0 := float64(clip.X-dst.X)+0.5-cx, float64(clip.Y-dst.Y)+0.5-cy
	u0 := int((x0*cos+y0*sin)*kx) + sw/2
	v0 := int((-x0*sin+y0*cos)*ky) + sh/2
	for y := clip.Y; y < clip.Bottom(); y++ {
		n := y - clip.Y
		u, v := u0+n*duy, v0+n*dvy
		drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
		for i := range drow {
			if u >= 0 && v >= 0 && u < sw && v < sh {
				p := src.Pix[(v>>16)*src.W+u>>16]
				switch a := p >> 24; {
				case a == 255:
					drow[i] = p
				case a > 0:
					drow[i] = blend(drow[i], p&0xFFFFFF, a)
				}
			}
			u += dux
			v += dvx
		}
	}
}
