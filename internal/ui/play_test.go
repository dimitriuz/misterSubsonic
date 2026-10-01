package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/input"
)

func TestNowPlayingVolumeKeys(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnDown)
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Press})
	ta.dispatch(input.Event{Button: input.BtnDown, Kind: input.Repeat})
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Release})
	if v := ta.pl.st.VolumeDB; v != -3 {
		t.Fatalf("volume %v after three steps down, want -3", v)
	}
	for range 5 {
		ta.press(input.BtnUp)
	}
	if v := ta.pl.st.VolumeDB; v != 0 {
		t.Fatalf("volume %v, want clamped to 0", v)
	}
}

func TestNowPlayingHoldXStarsTheSong(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) || !ta.isStarred(songStar(ta.pl.st.Queue[0])) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	hold(ta, input.BtnX, starHold)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestQueueMenuRemoveAndClear(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	home := NewHomeScreen()
	ta.Push(home)
	ta.Push(NewNowPlayingScreen())
	ta.Push(NewQueueScreen())
	ta.settle(t)
	ta.press(input.BtnDown) // "Время Луны"
	ta.press(input.BtnX)
	if _, ok := ta.Top().(*MenuScreen); !ok {
		t.Fatalf("X opened %T, want the menu", ta.Top())
	}
	menu := ta.Top().(*MenuScreen)
	if menu.title != "Queue" || menu.entries[0].label != "Remove Время Луны" || menu.entries[1].label != "Clear queue" || len(menu.entries) != 4 {
		t.Fatalf("menu %q with entries %q, %q", menu.title, menu.entries[0].label, menu.entries[1].label)
	}
	ta.press(input.BtnA) // Remove
	if !slices.Equal(ta.pl.calls, []string{"remove"}) || ta.toasts[0].text != "Removed Время Луны" {
		t.Fatalf("calls %v toasts %v", ta.pl.calls, ta.toasts)
	}
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Clear queue
	if !slices.Equal(ta.pl.calls, []string{"remove", "clear"}) || ta.Top() != home {
		t.Fatalf("calls %v, top %T", ta.pl.calls, ta.Top())
	}
}

func TestGoldenNowPlayingStarredInsecure(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.pl.st.VolumeDB = -12
		ta.pl.st.Queue[0].Starred = "2026-09-29T10:00:00Z"
		ta.SetInsecure(true)
		ta.Push(NewNowPlayingScreen())
		golden(t, "nowplaying-starred-"+p.Name, ta.settle(t))
	}
}
