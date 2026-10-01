package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
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

// When Sync drops the Resume row and moves the focus, the whole frame is
// redrawn: a partial frame with some other damage must not leave the old
// list on the screen (the verify mode compares it with a full one).
func TestHomeSyncDroppingResumeRedrawsTheFrame(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1}
	s := NewHomeScreen()
	ta.Push(s)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.settle(t)
	if s.resume == nil || s.list.Focus == 0 {
		t.Fatalf("setup: resume %v, focus %d", s.resume, s.list.Focus)
	}
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: -1}
	ta.clean()
	ta.Damage(gfx.R(0, 0, 4, 4)) // a partial frame, nothing else dirty
	ta.settle(t)                 // verify mode: partial must equal full
	if s.resume != nil {
		t.Fatal("the frame kept the Resume row")
	}
	ta.pl.st = player.State{}
	s.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1}
	s.list.Focus = 2
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: -1}
	ta.clean()
	s.Sync(ta.App)
	if !ta.dirty {
		t.Fatal("Sync moved the focus without marking the frame dirty")
	}
}
