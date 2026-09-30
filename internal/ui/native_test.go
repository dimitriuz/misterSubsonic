package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/player"
)

// Screens at 960x600, the framebuffer a 1920x1200 display gets: the HDMI
// layout scaled by 0.75 and drawn at that size.
func TestGoldenNative960x600(t *testing.T) {
	prof := PickProfile(960, 600, "auto")
	ta := newTestApp(t, prof)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	ta.Push(newSidebarRoot())
	ta.settle(t)
	golden(t, "root-feed-hdmi-960x600", ta.settle(t))

	ta = newTestApp(t, prof)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", "newest"))
	golden(t, "albums-hdmi-960x600", ta.settle(t))
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	golden(t, "album-hdmi-960x600", ta.settle(t))

	ta = newTestApp(t, prof)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	golden(t, "nowplaying-hdmi-960x600", ta.settle(t))
}
