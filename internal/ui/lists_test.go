package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/input"
)

func TestPlaylistsOpenAndPlay(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewPlaylistsScreen())
	ta.settle(t)
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if m.title != "Дорога домой" || len(m.entries) != 4 {
		t.Fatalf("playlist menu %q %v", m.title, m.entries)
	}
	ta.press(input.BtnB)
	ta.press(input.BtnA)
	pl, ok := ta.Top().(*PlaylistScreen)
	if !ok {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Shuffle
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 2 {
		t.Fatalf("shuffle %v, %d songs", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	ta.Pop()
	if ta.Top() != pl {
		t.Fatal("B from Now Playing did not return to the playlist")
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // second track
	ta.press(input.BtnA)
	if ta.pl.start != 1 || ta.pl.played[1].ID != "s2" {
		t.Fatalf("start %d, played %v", ta.pl.start, ta.pl.played)
	}
}

func TestStarredTabsShareOneCall(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewStarredScreen()
	ta.Push(s)
	ta.settle(t)
	ta.press(input.BtnUp)
	ta.press(input.BtnRight)
	ta.settle(t)
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.settle(t)
	if ta.lib.starCalls != 1 {
		t.Fatalf("getStarred2 called %d times, want 1", ta.lib.starCalls)
	}
	ta.press(input.BtnA) // play the starred tracks
	if !slices.Equal(ta.pl.calls, []string{"playnow"}) || ta.pl.played[0].ID != "s1" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestStarredErrorRetries(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.err = errOffline
	ta.Push(NewHomeScreen())
	ta.Push(NewStarredScreen())
	ta.settle(t)
	ta.lib.err = nil
	ta.press(input.BtnA)
	ta.settle(t)
	if ta.lib.starCalls != 2 {
		t.Fatalf("calls %d, want a retry", ta.lib.starCalls)
	}
}

func TestGoldenPlaylistsAndStarred(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewPlaylistsScreen())
		ta.settle(t)
		golden(t, "playlists-"+p.Name, ta.settle(t))
		ta.press(input.BtnA)
		ta.settle(t)
		golden(t, "playlist-"+p.Name, ta.settle(t))
		ta.Replace(NewHomeScreen())
		ta.Push(NewStarredScreen())
		ta.settle(t)
		ta.press(input.BtnUp)
		ta.press(input.BtnRight)
		ta.press(input.BtnRight)
		ta.press(input.BtnDown)
		golden(t, "starred-tracks-"+p.Name, ta.settle(t))
	}
}
