package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

// Media keys act on every screen: volume (saved, held to repeat, unmuting),
// play/pause, next, previous and seeking, even while Search takes letters.
func TestMediaKeysActEverywhere(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.pl.st.VolumeDB = -20
	ta.Push(NewSearchScreen()) // a text screen: the media keys type nothing
	ta.press(input.BtnVolUp)
	ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Repeat})
	if ta.pl.st.VolumeDB != -18 || ta.cfg.Playback.VolumeDB != -18 {
		t.Fatalf("volume %v (config %v), want -18", ta.pl.st.VolumeDB, ta.cfg.Playback.VolumeDB)
	}
	ta.press(input.BtnMute)
	ta.press(input.BtnVolDown)
	if ta.muted || ta.pl.st.VolumeDB != -19 {
		t.Fatalf("volume down while muted: muted %v at %v", ta.muted, ta.pl.st.VolumeDB)
	}
	ta.pl.calls = nil
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack} {
		ta.press(b)
	}
	if !slices.Equal(ta.pl.calls, []string{"toggle", "next", "prev"}) {
		t.Fatalf("player calls %v", ta.pl.calls)
	}
	ta.pl.st.Position = time.Minute
	ta.press(input.BtnSeekFwd)
	if ta.pl.seekPos != time.Minute+seekStep {
		t.Fatalf("seek to %v", ta.pl.seekPos)
	}
	ta.press(input.BtnSeekBack)
	if ta.pl.seekPos != time.Minute-seekStep {
		t.Fatalf("seek back to %v", ta.pl.seekPos)
	}
	if s, ok := ta.Top().(*SearchScreen); !ok || len(s.query) != 0 {
		t.Fatal("a media key typed into Search or left it")
	}
}

// With nothing playing, the transport keys do nothing (and don't panic).
func TestMediaKeysWithoutAQueue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack, input.BtnSeekFwd} {
		ta.press(b)
	}
	if slices.Contains(ta.pl.calls, "next") || slices.Contains(ta.pl.calls, "seek") {
		t.Fatalf("player calls %v with no queue", ta.pl.calls)
	}
}
