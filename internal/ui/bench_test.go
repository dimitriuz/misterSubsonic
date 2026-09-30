package ui

import (
	"fmt"
	"testing"

	"mistersubsonic/internal/gfx"
)

// BenchmarkRepaint measures a full frame: draw the screen and scale it to the
// physical framebuffer. Presenting (packing into the framebuffer) is
// BenchmarkPack in gfx; covers come from memory, as in the app. Spec §11
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
		after  func(ta *testApp) // runs once the screen has loaded
	}{
		{"albums-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
		{"nowplaying-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
		{"feed-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
		{"search-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewSearchScreen() }, nil},
		// A focused title that overflows, past its pre-scroll delay: every frame scrolls.
		{"marquee-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen {
			ta.lib.albums[0].Name = "A Rather Long Album Title That Will Need Truncating Somewhere"
			return NewAlbumListScreen("Recently added", "newest")
		}, func(ta *testApp) {
			ta.now = ta.now.Add(2 * marqueeDelay)
		}},
		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
		// The layout scaled to the framebuffer and drawn at its size (no
		// scaling pass): 960x600 is what a 1920x1200 display gets (the
		// MiSTer halves framebuffers above 1920x1080).
		{"albums-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
		{"feed-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
		{"nowplaying-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
		{"albums-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
		{"feed-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			t := &testing.T{}
			ta := newTestApp(t, c.prof)
			ta.o.Display = nullDisplay{c.fbW, c.fbH} // the framebuffer's pack is measured in gfx
			ta.scaler = nil
			if c.prof.W != c.fbW || c.prof.H != c.fbH {
				ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
			}
			for i := 0; i < 40; i++ { // a long list, so every row is drawn
				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
			}
			ta.Push(NewHomeScreen())
			ta.Push(c.screen(ta))
			ta.settle(t)
			if c.after != nil {
				c.after(ta)
				if ta.settle(t); !ta.animate {
					b.Fatal("the marquee case does not scroll")
				}
			}
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

// nullDisplay takes frames without doing anything with them.
type nullDisplay struct{ w, h int }

func (d nullDisplay) Size() (int, int)          { return d.w, d.h }
func (d nullDisplay) Present(*gfx.Canvas) error { return nil }
func (d nullDisplay) Close() error              { return nil }

// BenchmarkScreenshot measures saving a frame as a PNG (off the UI
// goroutine in the app), at the MiSTer's usual framebuffer sizes:
//
//	./ui.test -test.run '^$' -test.bench Screenshot -test.benchtime 5x
func BenchmarkScreenshot(b *testing.B) {
	for _, s := range []struct{ w, h int }{{960, 600}, {1920, 1080}} {
		b.Run(fmt.Sprintf("%dx%d", s.w, s.h), func(b *testing.B) {
			ta := newTestApp(&testing.T{}, scaleProfile(ProfileHDMI, s.w, s.h))
			ta.Push(newSidebarRoot())
			ta.settle(&testing.T{})
			dir := b.TempDir()
			b.ResetTimer()
			for i := range b.N {
				if _, err := saveScreenshot(dir, fmt.Sprint(i), ta.canvas); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
