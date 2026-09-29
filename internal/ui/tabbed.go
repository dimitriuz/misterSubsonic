package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// TabbedScreen shows one of several child screens under a row of tabs
// (Albums: A–Z / by year / by genre; Starred). Up from the top of a child
// moves the focus to the tabs, Down or A goes back. A child is entered
// (starts loading) the first time its tab is shown.
type TabbedScreen struct {
	title    string
	tabs     Tabs
	children []Screen
	entered  []bool
	onTabs   bool
}

func newTabbed(title string, labels []string, children ...Screen) *TabbedScreen {
	return &TabbedScreen{title: title, tabs: Tabs{Labels: labels}, children: children, entered: make([]bool, len(children))}
}

// NewAlbumsScreen is the Albums section: A–Z, newest year first, by genre.
func NewAlbumsScreen() *TabbedScreen {
	return newTabbed("Albums", []string{"A–Z", "By year", "By genre"},
		NewAlbumListScreen("Albums A–Z", subsonic.ListAlphabetical),
		NewAlbumQueryScreen("Albums by year", subsonic.AlbumListQuery{Type: subsonic.ListByYear, FromYear: 9999, ToYear: 0}),
		NewGenresScreen())
}

func (s *TabbedScreen) Title() string { return s.title }

func (s *TabbedScreen) Owns(x Screen) bool {
	for _, c := range s.children {
		if c == x {
			return true
		}
		if o, ok := c.(owner); ok && o.Owns(x) {
			return true
		}
	}
	return false
}

func (s *TabbedScreen) Enter(a *App) { s.enter(a) }

func (s *TabbedScreen) enter(a *App) {
	if !s.entered[s.tabs.Sel] {
		s.entered[s.tabs.Sel] = true
		s.children[s.tabs.Sel].Enter(a)
	} else if sh, ok := s.children[s.tabs.Sel].(shower); ok {
		sh.Shown(a)
	}
}

// Shown runs when the screen is visible again (a sidebar section reopened).
func (s *TabbedScreen) Shown(a *App) { s.enter(a) }

func (s *TabbedScreen) Handle(a *App, e input.Event) bool {
	if s.onTabs {
		if s.tabs.Handle(e) {
			s.enter(a)
			return true
		}
		if (e.Button == input.BtnDown && e.Kind != input.Release) || (e.Button == input.BtnA && e.Kind == input.Press) {
			s.onTabs = false
			return true
		}
		return false
	}
	if s.children[s.tabs.Sel].Handle(a, e) {
		return true
	}
	if e.Button == input.BtnUp && e.Kind != input.Release {
		s.onTabs = true
		return true
	}
	return false
}

func (s *TabbedScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	h := a.P.RowH
	s.tabs.Draw(a, c, gfx.R(area.X, area.Y, area.W, h), s.onTabs)
	s.children[s.tabs.Sel].Draw(a, c, gfx.R(area.X, area.Y+h, area.W, area.H-h))
}
