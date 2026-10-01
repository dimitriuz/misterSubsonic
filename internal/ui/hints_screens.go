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
	exitHint    = hk(input.BtnA, "Exit")
	playHint    = hk(input.BtnA, "Play")
	menuHint    = hk(input.BtnX, "Menu")
	shuffleHint = hk(input.BtnSelect, "Shuffle")
	pageHint    = hkPair(input.BtnL, input.BtnR, "Page")
)

// loadHints is a loading screen's list: A retries after a failed load, and
// nothing is hinted while it loads or when it lists nothing (usable false);
// the app still adds Back and Now Playing.
func loadHints(failed, usable bool, normal ...Hint) []Hint {
	switch {
	case failed:
		return []Hint{hk(input.BtnA, "Retry")}
	case !usable:
		return nil
	}
	return normal
}

func (s *AlbumScreen) Hints(*App) []Hint {
	return loadHints(s.album == nil && s.err != nil, s.album != nil, playHint, menuHint, shuffleHint)
}

func (s *ArtistScreen) Hints(*App) []Hint {
	return loadHints(s.err != nil, len(s.view.albums) > 0, openHint, menuHint, shuffleHint)
}

func (s *PlaylistScreen) Hints(*App) []Hint {
	return loadHints(s.pl == nil && s.err != nil, s.pl != nil, playHint, menuHint, shuffleHint)
}

func (s *PlaylistsScreen) Hints(*App) []Hint {
	return loadHints(s.err != nil, len(s.playlists) > 0, openHint, menuHint)
}

func (s *GenresScreen) Hints(*App) []Hint {
	return loadHints(s.err != nil, len(s.genres) > 0, openHint)
}

func (s *HomeScreen) Hints(a *App) []Hint {
	s.sync(a)
	if items := s.items(); s.list.Focus < len(items) && items[s.list.Focus].exit {
		return []Hint{exitHint}
	}
	return []Hint{openHint}
}
func (s *SettingsScreen) Hints(*App) []Hint { return []Hint{openHint} }
func (s *AboutScreen) Hints(*App) []Hint    { return nil }

func (s *AlbumListScreen) Hints(*App) []Hint {
	none := len(s.view.albums) == 0
	return loadHints(none && s.err != nil, !none, openHint, menuHint, shuffleHint, pageHint)
}

func (s *ArtistsScreen) Hints(*App) []Hint {
	return loadHints(s.err != nil, len(s.view.artists) > 0, openHint, menuHint, hkPair(input.BtnL, input.BtnR, "Letter"))
}

func (s *NowPlayingScreen) Hints(a *App) []Hint {
	st := a.state() // empty with no player
	play := "Play"
	if st.Status == player.Playing {
		play = "Pause"
	}
	star := "Star"
	if song, ok := st.Current(); ok && a.isStarred(songStar(song)) {
		star = "Unstar"
	}
	return []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
		hkPair(input.BtnUp, input.BtnDown, "Volume"), hkPair(input.BtnL, input.BtnR, "Prev/Next"),
		hk(input.BtnX, star), hk(input.BtnY, "Queue"), hk(input.BtnSelect, "Mode · hold: Mute")}
}

func (s *QueueScreen) Hints(a *App) []Hint {
	if len(a.state().Queue) == 0 {
		return []Hint{hk(input.BtnY, "Now Playing")} // Play and Menu have nothing to act on
	}
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

func (s *SearchScreen) Hints(a *App) []Hint {
	if s.inResults {
		return []Hint{openHint, menuHint, hk(input.BtnB, "Keyboard")}
	}
	return typing(a, "", len(s.query) > 0)
}

// typing is a text field's hints. A gamepad types with A on the on-screen
// keyboard and deletes with X, only when there is text. On a keyboard the
// letters type directly and Backspace deletes; Enter is hinted (as submit,
// next) only on a screen where it does that.
func typing(a *App, enter string, text bool) []Hint {
	if a.pad {
		if !text {
			return []Hint{hk(input.BtnA, "Type")}
		}
		return []Hint{hk(input.BtnA, "Type"), hk(input.BtnX, "Delete")}
	}
	var hs []Hint
	if enter != "" {
		hs = append(hs, Hint{Button: input.BtnA, Key: "Enter", Rune: '\n', Label: enter})
	}
	if text {
		hs = append(hs, Hint{Button: input.BtnX, Key: "Backspace", Rune: '\b', Label: "Delete"})
	}
	return hs
}

func (s *WizardScreen) Hints(a *App) []Hint {
	var hs []Hint
	if s.step == stepTest {
		hs = []Hint{hk(input.BtnA, "Choose")}
	} else {
		hs = typing(a, "Next", len(s.fields[s.step]) > 0)
	}
	if s.step > stepURL { // B goes back a step (on the first one the app adds Back, if it can)
		hs = withBack(hs)
	}
	return hs
}

func (s *FeedScreen) Hints(*App) []Hint {
	if s.resume != nil && s.row == 0 { // the Resume card
		return []Hint{hk(input.BtnA, "Resume")}
	}
	r := s.current()
	if r == nil {
		return nil
	}
	return loadHints(r.err != nil, len(r.albums) > 0, openHint, menuHint, shuffleHint)
}

func (s *starredTab) Hints(a *App) []Hint {
	failed, usable := s.d.err != nil, s.sync() > 0
	if s.kind == starSong {
		return loadHints(failed, usable, playHint, menuHint, shuffleHint)
	}
	return loadHints(failed, usable, openHint, menuHint)
}

// Containers show their focused part's hints.

func (s *SidebarRoot) Hints(a *App) []Hint {
	if s.inSidebar {
		if s.onExit() {
			return []Hint{exitHint}
		}
		return []Hint{openHint}
	}
	// B in a section goes back to the sidebar, unless the section uses it.
	hs := hintsOf(a, s.current())
	for _, h := range hs {
		if h.Button == input.BtnB || h.Pair == input.BtnB {
			return hs
		}
	}
	return withBack(hs)
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
