package ui

import (
	"fmt"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// UnreachableScreen explains why the server couldn't be reached and offers
// Try again, and Switch server when the config has another one.
type UnreachableScreen struct {
	srv     config.Server
	err     error
	actions []menuEntry
	list    List
}

func NewUnreachableScreen(srv config.Server, err error) *UnreachableScreen {
	return &UnreachableScreen{srv: srv, err: err}
}

func (s *UnreachableScreen) Title() string { return "Can't reach the server" }

func (s *UnreachableScreen) Enter(a *App) {
	s.actions = []menuEntry{{"Try again", func(a *App) { a.Connect() }}}
	if cfg := a.Config(); cfg != nil && len(cfg.Servers) > 1 {
		s.actions = append(s.actions, menuEntry{"Switch server", func(a *App) { a.openMenu("Switch to", switchEntries(a, s.srv.Name)) }})
	}
}

// switchEntries connects to one of the other servers, making it the default.
func switchEntries(a *App, current string) []menuEntry {
	var out []menuEntry
	for _, srv := range a.Config().Servers {
		if srv.Name == current {
			continue
		}
		name := srv.Name
		out = append(out, menuEntry{name + " — " + displayURL(srv.URL), func(a *App) {
			a.UpdateConfig(func(c *config.Config) { c.DefaultServer = name }, false)
			a.Connect()
		}})
	}
	return out
}

func (s *UnreachableScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.actions)) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && len(s.actions) > 0 {
		s.actions[s.list.Focus].run(a)
		return true
	}
	return false
}

func (s *UnreachableScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	msg := fmt.Sprintf("%s: %s.", displayURL(s.srv.URL), subsonic.Classify(s.err))
	if h := hint(s.err); h != "" {
		msg += "\n" + h
	}
	y := area.Y + p.Margin
	for _, line := range wrap(f, msg, area.W-2*p.Margin) {
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), line, colText, area)
		y += f.Height()
	}
	y += p.Margin / 2
	s.list.Draw(c, gfx.R(area.X, y, area.W, area.Bottom()-y), len(s.actions), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: s.actions[i].label, col: colAccent, focused: focused})
	})
}
