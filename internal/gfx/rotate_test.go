package gfx

import "testing"

// A round, one-colour-per-half disc.
func testDisc(n int) *Image {
	img := NewImage(n, n)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := 2*x+1-n, 2*y+1-n
			if dx*dx+dy*dy > n*n {
				continue // transparent corner
			}
			col := uint32(0xFFFF0000) // red left half
			if x >= n/2 {
				col = 0xFF0000FF // blue right half
			}
			img.Pix[y*n+x] = col
		}
	}
	return img
}

func TestBlitRotatedQuarterTurnSwapsHalves(t *testing.T) {
	d := testDisc(64)
	c := NewCanvas(64, 64)
	c.Clear(0x000000)
	c.BlitRotated(d, R(0, 0, 64, 64), 3.14159265/2) // clockwise: left goes to the top
	if got := c.At(32, 8) & 0xFFFFFF; got != 0xFF0000 {
		t.Fatalf("top = %06x, want red", got)
	}
	if got := c.At(32, 56) & 0xFFFFFF; got != 0x0000FF {
		t.Fatalf("bottom = %06x, want blue", got)
	}
	if got := c.At(1, 1) & 0xFFFFFF; got != 0 {
		t.Fatalf("corner = %06x, want it left alone", got)
	}
}

func TestBlitRotatedZeroIsAPlainBlit(t *testing.T) {
	d := testDisc(48)
	a, b := NewCanvas(48, 48), NewCanvas(48, 48)
	a.Clear(0x102030)
	b.Clear(0x102030)
	a.Blit(d, R(0, 0, 48, 48))
	b.BlitRotated(d, R(0, 0, 48, 48), 0)
	diff := 0
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			diff++
		}
	}
	if diff > 48*4 { // only the smoothed edge may differ
		t.Fatalf("%d pixels differ", diff)
	}
}

func BenchmarkBlitRotated400(b *testing.B) {
	d := testDisc(400)
	c := NewCanvas(400, 400)
	for i := 0; i < b.N; i++ {
		c.BlitRotated(d, R(0, 0, 400, 400), float64(i)*0.1)
	}
}
