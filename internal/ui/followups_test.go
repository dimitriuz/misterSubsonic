package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// CRT Home: once something plays, Resume would replace the live queue.
func TestCRTHomeDropsResumeOncePlaying(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	home := NewHomeScreen()
	ta.Push(home)
	ta.settle(t)
	if home.resume == nil {
		t.Fatal("no Resume row")
	}
	playingState(ta) // e.g. an album was played and B brought us back
	ta.settle(t)
	ta.press(input.BtnA)
	if slices.Contains(ta.pl.calls, "resume") {
		t.Fatal("A resumed the saved queue over the live one")
	}
	if l, ok := ta.Top().(*AlbumListScreen); !ok || l.title != "Recently added" {
		t.Fatalf("A opened %T", ta.Top())
	}
}

// Starred refreshes when a screen above it closes (Go to album, star, B).
func TestStarredRefreshesWhenShownAgainAfterPop(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewStarredScreen())
	ta.settle(t)
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	ta.settle(t)
	ta.lib.starred.Albums = append(ta.lib.starred.Albums, ta.lib.albums[0])
	ta.toggleStar(albumStar(ta.lib.albums[0]))
	ta.settle(t)
	ta.press(input.BtnB)
	ta.settle(t)
	if ta.lib.starCalls != 2 {
		t.Fatalf("getStarred2 called %d times, want a reload", ta.lib.starCalls)
	}
}

// Leaving the screen before the server answers doesn't lose the star.
func TestStarSurvivesLeavingTheScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	it := albumStar(ta.lib.albums[0])
	ta.toggleStar(it)
	ta.toggleStar(it) // a second press while the first is in flight
	ta.Pop()
	ta.settle(t)
	if !ta.isStarred(it) || !slices.Equal(ta.lib.stars, []string{"star al-1"}) {
		t.Fatalf("starred %v, server calls %v", ta.isStarred(it), ta.lib.stars)
	}
}
