package gfx

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"testing"
)

// coverJPEG is a 2000×2000 photo-like JPEG (a gradient with noise).
func coverJPEG(tb testing.TB) []byte {
	tb.Helper()
	r := rand.New(rand.NewSource(1))
	m := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 2000; x++ {
			i := m.PixOffset(x, y)
			m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = uint8(x/8+r.Intn(16)), uint8(y/8+r.Intn(16)), uint8((x+y)/16+r.Intn(16)), 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: 85}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkDecodeCover measures what the art workers do with a big cover:
// decode it and scale it to fit the screen. "scale" is the scaling alone,
// from the decoder's own image (4:2:0 YCbCr, as JPEGs come).
func BenchmarkDecodeCover(b *testing.B) {
	data := coverJPEG(b)
	b.Run("decode+scale", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := DecodeImage(data, 600, 600); err != nil {
				b.Fatal(err)
			}
		}
	})
	m, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	bnd := m.Bounds()
	b.Run("scale", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			boxFilter(bnd.Dx(), bnd.Dy(), rowsOf(m), make([]uint32, bnd.Dx()), isOpaque(m), 600, 600)
		}
	})
}
