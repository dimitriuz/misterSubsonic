package gfx

import "testing"

// BenchmarkPack measures presenting one frame into framebuffer memory.
func BenchmarkPack(b *testing.B) {
	for _, c := range []struct {
		name      string
		w, h, bpp int
	}{{"960x600x32", 960, 600, 32}, {"1280x720x32", 1280, 720, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1200x32", 1920, 1200, 32}, {"1920x1080x16", 1920, 1080, 16}} {
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

// BenchmarkPackRects measures presenting parts of a 1920x1200 frame (Plan
// 4b's partial redraws): two list rows, a cover cell, a list area.
func BenchmarkPackRects(b *testing.B) {
	f := fbFormat{width: 1920, height: 1200, stride: 1920 * 4, bpp: 32}
	mem := make([]byte, f.stride*f.height)
	cv := NewCanvas(1920, 1200)
	for _, c := range []struct {
		name string
		rs   []Rect
	}{
		{"rows", []Rect{R(280, 300, 1640, 126)}},
		{"cell", []Rect{R(400, 200, 380, 470)}},
		{"list", []Rect{R(280, 96, 1640, 1000)}},
		{"full", []Rect{R(0, 0, 1920, 1200)}},
	} {
		b.Run(c.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				for _, r := range c.rs {
					f.packRect(mem, cv, r)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
