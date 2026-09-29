package ui

import (
	"testing"

	"mistersubsonic/internal/gfx"
)

// BenchmarkRepaint measures a full frame: draw the screen, scale it to the
// physical framebuffer and present it (headless = one frame copy). Spec §11
// spike 3 targets < 30 ms per repaint on the MiSTer's Cortex-A9; run it on the
// device with the cross-compiled test binary:
//
//	./ui.test -test.run '^$' -test.bench Repaint -test.benchtime 50x
func BenchmarkRepaint(b *testing.B) {
	cases := []struct {
		name   string
		prof   Profile
		fbW    int
		fbH    int
		screen func(ta *testApp) Screen
	}{
		{"albums-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
		{"nowplaying-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }},
		{"feed-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }},
		{"search-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewSearchScreen() }},
		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			t := &testing.T{}
			ta := newTestApp(t, c.prof)
			ta.disp = gfx.NewHeadless(c.fbW, c.fbH, "")
			ta.o.Display = ta.disp
			ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
			for i := 0; i < 40; i++ { // a long list, so every row is drawn
				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
			}
			ta.Push(NewHomeScreen())
			ta.Push(c.screen(ta))
			ta.settle(t)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ta.dirty = true
				if err := ta.render(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
