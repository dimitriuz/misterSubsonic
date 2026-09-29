package ui

import (
	"testing"

	"mistersubsonic/internal/input"
)

func TestMuteKeyTogglesAndTheVolumeUnmutes(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.pl.st.VolumeDB = -10
	ta.press(input.BtnMute)
	if !ta.muted || !ta.pl.st.Muted || ta.pl.st.VolumeDB != -10 {
		t.Fatalf("muted %v, player muted %v at %v dB", ta.muted, ta.pl.st.Muted, ta.pl.st.VolumeDB)
	}
	if got := ta.volumeText(-10); got != "Muted" {
		t.Fatalf("volume shown as %q while muted", got)
	}
	ta.press(input.BtnMute)
	if ta.muted || ta.pl.st.Muted || ta.volumeText(-10) != "Vol −10 dB" {
		t.Fatalf("the second press didn't unmute: %v %v %q", ta.muted, ta.pl.st.Muted, ta.volumeText(-10))
	}
	ta.press(input.BtnMute)
	ta.setVolume(-9) // Up or Down on Now Playing
	if ta.muted || ta.pl.st.Muted || ta.pl.st.VolumeDB != -9 {
		t.Fatalf("changing the volume kept the sound off: %v %v %v", ta.muted, ta.pl.st.Muted, ta.pl.st.VolumeDB)
	}
}

func TestMuteCarriesToTheNextServersPlayer(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.press(input.BtnMute)
	pl := newFakePlayer()
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[1]}, ta.lib, pl, fakeArt{})
	if !pl.st.Muted {
		t.Fatal("the new server's player plays while the app shows Muted")
	}
}

func TestMuteIsNotSaved(t *testing.T) {
	ta, _ := connectedApp(t)
	before := ta.saveAt
	ta.press(input.BtnMute)
	if ta.saveAt != before || ta.saving {
		t.Fatal("muting saved the config")
	}
}

func TestTypingMDoesNotMute(t *testing.T) {
	for name, s := range map[string]Screen{"Search": NewSearchScreen(), "the wizard": NewWizardScreen(false, false)} {
		ta, _ := connectedApp(t)
		ta.Push(s)
		ta.onInput(input.Event{Button: input.BtnMute, Kind: input.Press, Rune: 'm'})
		ta.onInput(input.Event{Button: input.BtnMute, Kind: input.Release, Rune: 'm'})
		if ta.muted {
			t.Fatalf("typing m in %s muted the sound", name)
		}
	}
}

func TestMuteSettingsRow(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	rows := playbackSettings(ta.App)
	if !ta.muted || rows[0].value(ta.App) != "Muted" || rows[1].value(ta.App) != "On" {
		t.Fatalf("muted %v: volume row %q, mute row %q", ta.muted, rows[0].value(ta.App), rows[1].value(ta.App))
	}
	ta.press(input.BtnLeft)
	if ta.muted {
		t.Fatal("Left on the Mute row didn't turn the sound back on")
	}
}
