package gfx

import "testing"

// BenchmarkPack measures presenting one frame into framebuffer memory.
func BenchmarkPack(b *testing.B) {
	for _, c := range []struct {
		name      string
		w, h, bpp int
	}{{"960x600x32", 960, 600, 32}, {"1280x720x32", 1280, 720, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1080x16", 1920, 1080, 16}} {
		b.Run(c.name, func(b *testing.B) {
			f := fbFormat{width: c.w, height: c.h, stride: c.w * c.bpp / 8, bpp: c.bpp}
			mem := make([]byte, f.stride*f.height)
			cv := NewCanvas(c.w, c.h)
			for i := range cv.Pix {
				cv.Pix[i] = uint32(i) * 2654435761
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f.pack(mem, cv)
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
