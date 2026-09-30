package ui

import (
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// Each screen's hints, taken from what its Handle does (hints_test.go
// presses every one and checks that something happens). The app adds Back
// and Now Playing (hints.go).

var (
	openHint    = hk(input.BtnA, "Open")
	playHint    = hk(input.BtnA, "Play")
	menuHint    = hk(input.BtnX, "Menu")
	shuffleHint = hk(input.BtnSelect, "Shuffle")
	pageHint    = hkPair(input.BtnL, input.BtnR, "Page")
)

func (s *AlbumScreen) Hints(*App) []Hint     { return []Hint{playHint, menuHint, shuffleHint} }
func (s *ArtistScreen) Hints(*App) []Hint    { return []Hint{openHint, menuHint, shuffleHint} }
func (s *PlaylistScreen) Hints(*App) []Hint  { return []Hint{playHint, menuHint, shuffleHint} }
func (s *PlaylistsScreen) Hints(*App) []Hint { return []Hint{openHint, menuHint} }
func (s *GenresScreen) Hints(*App) []Hint    { return []Hint{openHint} }
func (s *HomeScreen) Hints(*App) []Hint      { return []Hint{openHint} }
func (s *SettingsScreen) Hints(*App) []Hint  { return []Hint{openHint} }
func (s *AboutScreen) Hints(*App) []Hint     { return nil }

func (s *AlbumListScreen) Hints(*App) []Hint {
	return []Hint{openHint, menuHint, shuffleHint, pageHint}
}

func (s *ArtistsScreen) Hints(*App) []Hint {
	return []Hint{openHint, menuHint, hkPair(input.BtnL, input.BtnR, "Letter")}
}

func (s *NowPlayingScreen) Hints(a *App) []Hint {
	play := "Play"
	if pl := a.Player(); pl != nil && pl.State().Status == player.Playing {
		play = "Pause"
	}
	star := "Star"
	if song, ok := a.Player().State().Current(); ok && a.isStarred(songStar(song)) {
		star = "Unstar"
	}
	return []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
		hkPair(input.BtnUp, input.BtnDown, "Volume"), hkPair(input.BtnL, input.BtnR, "Prev/Next"),
		hk(input.BtnX, star), hk(input.BtnY, "Queue"), hk(input.BtnSelect, "Mode")}
}

func (s *QueueScreen) Hints(*App) []Hint {
	return []Hint{playHint, menuHint, hk(input.BtnY, "Now Playing")}
}

func (s *MenuScreen) Hints(*App) []Hint {
	return []Hint{hk(input.BtnA, "Choose"), hk(input.BtnB, "Close")}
}

func (s *MessageScreen) Hints(*App) []Hint {
	if s.retry == nil {
		return nil
	}
	return []Hint{hk(input.BtnA, s.label)}
}

func (s *SettingsListScreen) Hints(*App) []Hint {
	return []Hint{hkPair(input.BtnLeft, input.BtnRight, "Change")}
}

func (s *ServersScreen) Hints(a *App) []Hint {
	if s.list.Focus >= len(s.servers(a)) {
		return []Hint{hk(input.BtnA, "Add")}
	}
	return []Hint{hk(input.BtnA, "Switch"), menuHint}
}

func (s *UnreachableScreen) Hints(*App) []Hint { return []Hint{hk(input.BtnA, "Choose")} }

func (s *SearchScreen) Hints(*App) []Hint {
	if s.inResults {
		return []Hint{openHint, menuHint, hk(input.BtnB, "Keyboard")}
	}
	return typing(len(s.query) > 0)
}

// typing is an on-screen keyboard's hints: Delete only when there is text.
func typing(text bool) []Hint {
	if !text {
		return []Hint{hk(input.BtnA, "Type")}
	}
	return []Hint{hk(input.BtnA, "Type"), hk(input.BtnX, "Delete")}
}

func (s *WizardScreen) Hints(*App) []Hint {
	if s.step == stepTest {
		return []Hint{hk(input.BtnA, "Choose")}
	}
	return typing(len(s.fields[s.step]) > 0)
}

func (s *FeedScreen) Hints(*App) []Hint {
	if s.resume != nil && s.row == 0 { // the Resume card
		return []Hint{hk(input.BtnA, "Resume")}
	}
	return []Hint{openHint, menuHint, shuffleHint}
}

func (s *starredTab) Hints(a *App) []Hint {
	if s.kind == starSong {
		return []Hint{playHint, menuHint, shuffleHint}
	}
	return []Hint{openHint, menuHint}
}

// Containers show their focused part's hints.

func (s *SidebarRoot) Hints(a *App) []Hint {
	if s.inSidebar {
		return []Hint{openHint}
	}
	return hintsOf(a, s.current())
}

func (s *TabbedScreen) Hints(a *App) []Hint {
	if s.onTabs {
		return []Hint{hkPair(input.BtnLeft, input.BtnRight, "Tab"), openHint}
	}
	return hintsOf(a, s.children[s.tabs.Sel])
}

func hintsOf(a *App, s Screen) []Hint {
	if h, ok := s.(Hinter); ok {
		return h.Hints(a)
	}
	return commonHints
}

// ownsAllButtons: a menu handles every press (see modalHints).
func (s *MenuScreen) ownsAllButtons() {}

// Typing: the search field takes typed keys until the results have focus.
func (s *SearchScreen) Typing() bool { return !s.inResults }

// Typing: every wizard step but the test takes typed keys.
func (s *WizardScreen) Typing() bool { return s.step != stepTest }

// Typing: the sidebar root passes keys to a section that types.
func (s *SidebarRoot) Typing() bool {
	t, ok := s.current().(textTaker)
	return ok && !s.inSidebar && t.Typing()
}
