package ui

import (
	"mistersubsonic/internal/player"
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func TestMuteKeyTogglesAndTheVolumeUnmutes(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.pl.st.VolumeDB = -10
	ta.press(input.BtnMute)
	if !ta.muted || !ta.pl.st.Muted || ta.pl.st.VolumeDB != -10 {
		t.Fatalf("muted %v, player muted %v at %v dB", ta.muted, ta.pl.st.Muted, ta.pl.st.VolumeDB)
	}
	ta.press(input.BtnMute)
	if ta.muted || ta.pl.st.Muted {
		t.Fatalf("the second press didn't unmute: %v %v", ta.muted, ta.pl.st.Muted)
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

// Holding Select on Now Playing mutes; a short press cycles the play mode.
func TestSelectHoldMutesAndShortPressCyclesMode(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	sel := func(k input.Kind) { ta.onInput(input.Event{Button: input.BtnSelect, Kind: k}) }
	sel(input.Press)
	ta.now = ta.now.Add(muteHold - time.Millisecond)
	ta.onWake()
	sel(input.Release)
	if ta.muted || !ta.pl.st.Shuffle {
		t.Fatalf("short press: muted %v, shuffle %v", ta.muted, ta.pl.st.Shuffle)
	}
	ta.now = ta.now.Add(2 * muteHold)
	ta.onWake() // the short press's timer must not fire later
	if ta.muted {
		t.Fatal("a released press muted later")
	}
	hold := func() {
		sel(input.Press)
		ta.now = ta.now.Add(muteHold)
		ta.onWake()
		sel(input.Repeat)
		ta.now = ta.now.Add(muteHold)
		ta.onWake() // one hold mutes once
	}
	hold()
	if !ta.muted || !ta.pl.st.Muted || ta.volumeUntil.IsZero() {
		t.Fatalf("hold: muted %v/%v, panel %v", ta.muted, ta.pl.st.Muted, !ta.volumeUntil.IsZero())
	}
	sel(input.Release)
	if !ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
		t.Fatalf("the release after a hold cycled the mode: shuffle %v repeat %v", ta.pl.st.Shuffle, ta.pl.st.Repeat)
	}
	hold()
	sel(input.Release)
	if ta.muted || ta.pl.st.Muted {
		t.Fatal("a second hold didn't unmute")
	}
}

func TestSelectHoldIsCancelledByLeavingTheScreen(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press})
	ta.Push(NewQueueScreen())
	ta.now = ta.now.Add(2 * muteHold)
	ta.onWake()
	if ta.muted {
		t.Fatal("muted from another screen")
	}
}

// A short Select tap changes the mode label, which the player announces
// with no event: the release must redraw the frame.
func TestSelectTapRedrawsTheModeLabel(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.pl.st.Repeat = player.RepeatAll
	ta.Push(NewNowPlayingScreen())
	ta.settle(t)
	ta.clean()
	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press})
	ta.settle(t)
	ta.clean() // only the release can mark the frame dirty now
	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Release})
	if ta.pl.st.Repeat != player.RepeatOne || !ta.dirty {
		t.Fatalf("repeat %v, dirty %v", ta.pl.st.Repeat, ta.dirty)
	}
	ta.settle(t) // the verify mode compares with a full frame
}
