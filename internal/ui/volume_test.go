package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

// A volume change shows the panel for a moment; muting shows it muted.
func TestGoldenVolumePanel(t *testing.T) {
	for _, p := range append(profiles, PickProfile(960, 600, "auto")) {
		name := p.Name
		if p.W == 960 {
			name += "-960x600"
		}
		ta := newTestApp(t, p)
		playingState(ta)
		ta.pl.st.VolumeDB = -19
		ta.Push(NewHomeScreen())
		ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Press}) // → -18 dB
		golden(t, "volume-panel-"+name, ta.settle(t))
		ta.press(input.BtnMute)
		golden(t, "volume-panel-muted-"+name, ta.settle(t))
	}
}

// The panel goes after volumeShowTime without another change.
func TestVolumePanelHides(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.setVolume(-10)
	if ta.volumeUntil.IsZero() {
		t.Fatal("no panel after a volume change")
	}
	if w := ta.untilWake(); w > volumeShowTime {
		t.Fatalf("next wake %v, want the panel's end within %v", w, volumeShowTime)
	}
	// Holding volume up keeps it up: every repeat restarts the time.
	ta.now = ta.now.Add(volumeShowTime - 100*time.Millisecond)
	ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Repeat})
	ta.now = ta.now.Add(volumeShowTime - 100*time.Millisecond)
	ta.onWake()
	if ta.volumeUntil.IsZero() {
		t.Fatal("the panel went while the key repeated")
	}
	ta.now = ta.now.Add(100 * time.Millisecond)
	ta.clean()
	ta.onWake()
	if !ta.volumeUntil.IsZero() || !ta.redrawDue() {
		t.Fatal("the panel didn't go, or the screen wasn't redrawn without it")
	}
}

func TestVolumeLevel(t *testing.T) {
	for db, want := range map[float64]float64{0: 1, -60: 0, -30: 0.5, -70: 0, 5: 1} {
		if got := volumeLevel(db); got != want {
			t.Errorf("volumeLevel(%v) = %v, want %v", db, got, want)
		}
	}
}

// Now Playing shows no volume of its own: the frame is the same at any
// level (the panel shows changes; Settings shows the dB).
func TestNowPlayingDoesNotShowTheVolume(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.pl.st.VolumeDB = -10
	loud := ta.settle(t).ToRGBA()
	ta.pl.st.VolumeDB = -45
	ta.dirty = true
	if !samePixels(loud, ta.settle(t).ToRGBA()) {
		t.Fatal("Now Playing changed with the volume")
	}
}
