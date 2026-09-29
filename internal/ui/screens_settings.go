package ui

import (
	"fmt"
	"math"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// SettingsScreen is Settings (spec §8.2): Servers · Playback · Display ·
// About · Exit. It works without a connection (from the unreachable
// screen): changes are saved to the config and applied to the player when
// there is one.
type SettingsScreen struct {
	list List
}

var settingsItems = []string{"Servers", "Playback", "Display", "About", "Exit"}

func NewSettingsScreen() *SettingsScreen { return &SettingsScreen{} }

func (s *SettingsScreen) Title() string { return "Settings" }
func (s *SettingsScreen) Enter(a *App)  {}

func (s *SettingsScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(settingsItems)) {
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA {
		return false
	}
	switch settingsItems[s.list.Focus] {
	case "Servers":
		a.Push(NewServersScreen())
	case "Playback":
		a.Push(newSettingsList("Playback", playbackSettings))
	case "Display":
		a.Push(newSettingsList("Display", displaySettings))
	case "About":
		a.Push(&AboutScreen{})
	case "Exit":
		a.confirm = true // the same prompt as holding B
	}
	return true
}

func (s *SettingsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.list.Draw(c, area, len(settingsItems), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: settingsItems[i], focused: focused})
	})
}

// setting is one row of a settings list: Left/Right (or A, forwards)
// change it.
type setting struct {
	label  string
	value  func(a *App) string
	change func(a *App, dir int)
}

// SettingsListScreen is a list of settings with their values.
type SettingsListScreen struct {
	title string
	rows  func(a *App) []setting
	list  List
}

func newSettingsList(title string, rows func(a *App) []setting) *SettingsListScreen {
	return &SettingsListScreen{title: title, rows: rows}
}

func (s *SettingsListScreen) Title() string { return s.title }
func (s *SettingsListScreen) Enter(a *App)  {}

func (s *SettingsListScreen) Handle(a *App, e input.Event) bool {
	rows := s.rows(a)
	if s.list.Handle(e, len(rows)) {
		return true
	}
	if e.Kind == input.Release || len(rows) == 0 {
		return false
	}
	r := rows[min(s.list.Focus, len(rows)-1)]
	switch {
	case e.Button == input.BtnLeft:
		r.change(a, -1)
	case e.Button == input.BtnRight, e.Button == input.BtnA && e.Kind == input.Press:
		r.change(a, 1)
	default:
		return false
	}
	return true
}

func (s *SettingsListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	rows := s.rows(a)
	s.list.Draw(c, area, len(rows), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		v := rows[i].value(a)
		if focused {
			v = "‹ " + v + " ›"
		}
		a.drawRow(c, r, row{main: rows[i].label, focused: focused, right: v})
	})
}

// cycle moves through choices by dir, wrapping.
func cycle[T comparable](choices []T, cur T, dir int) T {
	i := 0
	for k, c := range choices {
		if c == cur {
			i = k
		}
	}
	return choices[(i+dir+len(choices))%len(choices)]
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// volumeDB is the current volume: the player's, else the config's.
func (a *App) volumeDB() float64 {
	if pl := a.Player(); pl != nil {
		return pl.State().VolumeDB
	}
	if a.cfg != nil {
		return a.cfg.Playback.VolumeDB
	}
	return 0
}

// setVolume changes the volume now and saves it (after a pause, so a held
// key is one write).
func (a *App) setVolume(db float64) {
	db = math.Max(-60, math.Min(0, math.Round(db)))
	if pl := a.Player(); pl != nil {
		pl.SetVolumeDB(db)
	}
	a.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = db }, true)
}

func playbackSettings(a *App) []setting {
	pb := func() config.Playback {
		if a.cfg == nil {
			return config.Default().Playback
		}
		return a.cfg.Playback
	}
	return []setting{
		{"Volume", func(a *App) string { return volumeLabel(a.volumeDB()) },
			func(a *App, dir int) { a.setVolume(a.volumeDB() + float64(dir)) }},
		{"ReplayGain", func(a *App) string { return pb().ReplayGain },
			func(a *App, dir int) {
				mode := cycle([]string{"off", "track", "album"}, pb().ReplayGain, dir)
				if pl := a.Player(); pl != nil {
					pl.SetReplayGain(mode)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.ReplayGain = mode }, false)
			}},
		{"Scrobbling", func(a *App) string { return onOff(pb().Scrobble) },
			func(a *App, dir int) {
				on := !pb().Scrobble
				if pl := a.Player(); pl != nil {
					pl.SetScrobble(on)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.Scrobble = on }, false)
			}},
		{"Transcode to", func(a *App) string { return pb().TranscodeFormat },
			func(a *App, dir int) {
				f := cycle([]string{"mp3", "flac", "wav"}, pb().TranscodeFormat, dir)
				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeFormat = f }, false)
				a.Toast("Used from the next connection")
			}},
		{"Transcode bitrate", func(a *App) string { return fmt.Sprintf("%d kbps", pb().TranscodeBitrate) },
			func(a *App, dir int) {
				b := cycle([]int{128, 192, 256, 320}, pb().TranscodeBitrate, dir)
				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeBitrate = b }, false)
				a.Toast("Used from the next connection")
			}},
	}
}

var screensaverChoices = []int{0, 1, 2, 5, 10, 15, 30}

func displaySettings(a *App) []setting {
	d := func() config.Display {
		if a.cfg == nil {
			return config.Default().Display
		}
		return a.cfg.Display
	}
	return []setting{
		{"Layout", func(a *App) string { return d().Profile },
			func(a *App, dir int) {
				p := cycle([]string{"auto", "hdmi", "crt"}, d().Profile, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.Profile = p }, false)
				a.Toast("The layout changes the next time the app starts")
			}},
		{"Screensaver", func(a *App) string {
			if m := d().ScreensaverMinutes; m > 0 {
				return fmt.Sprintf("after %d min", m)
			}
			return "Off"
		},
			func(a *App, dir int) {
				m := cycle(screensaverChoices, d().ScreensaverMinutes, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.ScreensaverMinutes = m }, false)
			}},
	}
}

// ServersScreen lists the configured servers: A switches to one, X opens
// its menu (switch, remove); the last row adds a server with the wizard.
type ServersScreen struct {
	list List
}

func NewServersScreen() *ServersScreen { return &ServersScreen{} }

func (s *ServersScreen) Title() string { return "Servers" }
func (s *ServersScreen) Enter(a *App)  {}

func (s *ServersScreen) servers(a *App) []config.Server {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Servers
}

func (s *ServersScreen) Handle(a *App, e input.Event) bool {
	srvs := s.servers(a)
	if s.list.Handle(e, len(srvs)+1) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	i := s.list.Focus
	if i >= len(srvs) { // Add a server
		if e.Button == input.BtnA {
			a.Push(NewWizardScreen(false, false))
			return true
		}
		return false
	}
	name := srvs[i].Name
	switch e.Button {
	case input.BtnA:
		a.switchServer(name)
		return true
	case input.BtnX:
		a.openMenu(name, []menuEntry{
			{"Switch to this server", func(a *App) { a.switchServer(name) }},
			{"Remove", func(a *App) {
				a.openMenu("Remove "+name+"?", []menuEntry{
					{"Remove", func(a *App) { a.removeServer(name) }},
					{"Cancel", func(*App) {}},
				})
			}},
		})
		return true
	}
	return false
}

// switchServer makes name the default and connects to it.
func (a *App) switchServer(name string) {
	if info, ok := a.Conn(); ok && info.Server.Name == name {
		a.Toast("Already connected to %s", name)
		return
	}
	a.UpdateConfig(func(c *config.Config) { c.DefaultServer = name }, false)
	a.Connect()
}

// removeServer deletes a server from the config. Removing the connected
// one connects to the next default, or, with none left, disconnects and
// starts the wizard.
func (a *App) removeServer(name string) {
	// Was it the active server? Conn() is empty while a connect is in
	// flight and on the unreachable screen, so ask the config as well.
	active := false
	if srv, ok := a.cfg.ActiveServer(); ok && srv.Name == name {
		active = true
	}
	info, connected := a.Conn()
	active = active || (connected && info.Server.Name == name)
	a.UpdateConfig(func(c *config.Config) { c.RemoveServer(name) }, false)
	a.Toast("Removed %s", name)
	switch {
	case len(a.cfg.Servers) == 0:
		a.Detach()
		if a.o.Connect != nil {
			a.o.Connect(a, a.cfg.Clone()) // no server: the old session stops
		}
		a.Replace(NewWizardScreen(true, false))
	case active: // also supersedes a connect still in flight
		a.Connect()
	}
}

func (s *ServersScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	srvs := s.servers(a)
	info, connected := a.Conn()
	s.list.Draw(c, area, len(srvs)+1, a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		if i == len(srvs) {
			a.drawRow(c, r, row{main: "Add a server", col: colAccent, focused: focused})
			return
		}
		srv := srvs[i]
		right := ""
		switch {
		case connected && info.Server.Name == srv.Name:
			right = "connected"
		case srv.Name == a.cfg.DefaultServer:
			right = "default"
		}
		sub := displayURL(srv.URL)
		if srv.Username != "" {
			sub += " · " + srv.Username
		}
		a.drawRow(c, r, row{main: srv.Name, sub: sub, focused: focused, right: right})
	})
}

// AboutScreen shows the app version and what the connection is.
type AboutScreen struct{}

func (s *AboutScreen) Title() string                 { return "About" }
func (s *AboutScreen) Enter(a *App)                  {}
func (s *AboutScreen) Handle(*App, input.Event) bool { return false }
func (s *AboutScreen) lines(a *App) []string {
	v := a.o.Version
	if v == "" {
		v = "dev"
	}
	out := []string{"MiSTer Subsonic " + v}
	info, ok := a.Conn()
	if !ok {
		return append(out, "Not connected", "Config: "+a.o.ConfigPath)
	}
	out = append(out,
		"Server: "+info.Server.Name+" — "+displayURL(info.Server.URL),
		serverName(info.Info)+", login by "+info.Auth.String())
	if info.Info != nil && info.Info.OpenSubsonic {
		out = append(out, fmt.Sprintf("OpenSubsonic: %d extensions", len(info.Info.Extensions)))
	}
	if info.Server.InsecureSkipVerify {
		out = append(out, "Certificate check: off (insecure)")
	}
	return append(out, "Config: "+a.o.ConfigPath)
}

func (s *AboutScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	y := area.Y + p.Margin
	for _, l := range s.lines(a) {
		for _, w := range wrap(f, l, area.W-2*p.Margin) {
			f.Draw(c, area.X+p.Margin, y+f.Ascent(), w, colText, area)
			y += f.Height()
		}
	}
}
