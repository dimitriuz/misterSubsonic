package ui

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/remote"
)

// SettingsScreen is Settings (spec §8.2): Servers · Playback · Display · Remote ·
// About. It works without a connection (from the unreachable
// screen): changes are saved to the config and applied to the player when
// there is one.
type SettingsScreen struct {
	list List
}

var settingsItems = []string{"Servers", "Playback", "Display", "Remote", "About"}

func NewSettingsScreen() *SettingsScreen { return &SettingsScreen{} }

func (s *SettingsScreen) Title() string { return "Settings" }
func (s *SettingsScreen) Enter(a *App)  {}

func (s *SettingsScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(settingsItems)) {
		a.moved(s.list.Moved())
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
	case "Remote":
		a.remoteURLs = nil
		if sw := a.o.RemoteSwitch; sw != nil {
			a.remoteURLs = sw.URLs()
		}
		a.Push(newSettingsList("Remote", remoteSettings))
	case "About":
		a.Push(&AboutScreen{})
	}
	return true
}

func (s *SettingsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.list.Draw(c, area, len(settingsItems), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: settingsItems[i], focused: focused})
	})
}

// setting is one row of a settings list: Left/Right (or A, forwards)
// change it. A held key keeps cycling the choices, but an On/Off row
// (toggle) changes once per press.
type setting struct {
	label  string
	value  func(a *App) string
	change func(a *App, dir int)
	help   string // what the setting does, shown under the list while it is focused
	toggle bool   // On/Off: ignores key repeat
	info   bool   // shows a value only: no arrows, nothing to change
}

// SettingsListScreen is a list of settings with their values.
type SettingsListScreen struct {
	title string
	rows  func(a *App) []setting
	list  List
	help  gfx.Rect // where the help line was drawn last
}

func newSettingsList(title string, rows func(a *App) []setting) *SettingsListScreen {
	return &SettingsListScreen{title: title, rows: rows}
}

func (s *SettingsListScreen) Title() string { return s.title }
func (s *SettingsListScreen) Enter(a *App)  {}

func (s *SettingsListScreen) Handle(a *App, e input.Event) bool {
	rows := s.rows(a)
	if s.list.Handle(e, len(rows)) {
		if rs := s.list.Moved(); rs != nil {
			a.moved(append(rs, s.help)) // the help line follows the focus
		}
		return true
	}
	if e.Kind == input.Release || len(rows) == 0 {
		return false
	}
	r := rows[min(s.list.Focus, len(rows)-1)]
	if r.toggle && e.Kind != input.Press {
		return e.Button == input.BtnLeft || e.Button == input.BtnRight // a held key must not flicker it
	}
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
	f := a.F.Small
	helpH := 2*f.Height() + a.P.Margin/2
	list := gfx.R(area.X, area.Y, area.W, max(area.H-helpH, a.P.RowH))
	s.help = gfx.R(area.X, list.Y+list.H, area.W, area.Y+area.H-list.Y-list.H)
	s.list.Draw(c, list, len(rows), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		v := rows[i].value(a)
		if focused && !rows[i].info {
			v = "‹ " + v + " ›"
		}
		a.drawRow(c, r, row{main: rows[i].label, focused: focused, right: v})
	})
	if len(rows) == 0 {
		return
	}
	y := s.help.Y + a.P.Margin/4
	for _, l := range helpLines(f, rows[s.list.Focus].help, area.W-2*a.P.Margin) {
		f.Draw(c, area.X+a.P.Margin, y+f.Ascent(), l, colDim, s.help)
		y += f.Height()
	}
}

// helpLines wraps text to w pixels in at most two lines, the second cut
// with "…" when the text is longer.
func helpLines(f *gfx.Font, text string, w int) []string {
	lines := wrap(f, text, w)
	if len(lines) > 2 {
		lines = []string{lines[0], f.Truncate(strings.Join(lines[1:], " "), w)}
	}
	return lines
}

// helpText is the help of the focused row (for tests).
func (s *SettingsListScreen) helpText(a *App) string {
	rows := s.rows(a)
	if len(rows) == 0 {
		return ""
	}
	return rows[min(max(s.list.Focus, 0), len(rows)-1)].help
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

// cycleNear is cycle for ascending numbers, where cur may be a hand-edited
// value that isn't a choice: the first step goes to the nearest choice in
// that direction, wrapping when there is none (160 → 192 or 128).
func cycleNear(choices []int, cur, dir int) int {
	if slices.Contains(choices, cur) {
		return cycle(choices, cur, dir)
	}
	if dir > 0 {
		for _, c := range choices {
			if c > cur {
				return c
			}
		}
		return choices[0]
	}
	for i := len(choices) - 1; i >= 0; i-- {
		if choices[i] < cur {
			return choices[i]
		}
	}
	return choices[len(choices)-1]
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
		return a.state().VolumeDB
	}
	if a.volumePending {
		return a.pendingDB // changed while there is no player
	}
	if a.cfg != nil {
		return a.cfg.Playback.VolumeDB
	}
	return 0
}

// setVolume changes the volume now and saves it (after a pause, so a held
// key is one write).
func (a *App) setVolume(db float64) {
	if a.muted {
		a.setMuted(false) // changing the volume brings the sound back (and shows the panel)
	} else {
		a.showVolume()
	}
	db = math.Max(-60, math.Min(0, math.Round(db)))
	defer a.notifyRemote(remote.StateChanged)
	if pl := a.Player(); pl != nil {
		pl.SetVolumeDB(db)
	} else {
		a.volumePending, a.pendingDB = true, db // the next player starts at this level
	}
	if a.cfg == nil {
		return // no config was loaded (invalid, or first run): never write one for a volume key
	}
	a.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = db }, true)
}

// setMuted turns the sound off or back on (spec §6). It isn't saved, so the
// app always starts with the sound on.
func (a *App) setMuted(on bool) {
	a.muted = on
	a.showVolume()
	a.notifyRemote(remote.StateChanged)
	if pl := a.Player(); pl != nil {
		pl.SetMuted(on)
	}
}

func (a *App) toggleMute() { a.setMuted(!a.muted) } // the volume panel shows it

func playbackSettings(a *App) []setting {
	pb := func() config.Playback {
		if a.cfg == nil {
			return config.Default().Playback
		}
		return a.cfg.Playback
	}
	rows := []setting{
		{label: "ReplayGain", value: func(a *App) string { return pb().ReplayGain },
			change: func(a *App, dir int) {
				mode := cycle([]string{"off", "track", "album"}, pb().ReplayGain, dir)
				if pl := a.Player(); pl != nil {
					pl.SetReplayGain(mode)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.ReplayGain = mode }, false)
			},
			help: "Evens out loudness: track levels each song, album keeps an album's own dynamics. Off plays files as they are.", toggle: false},
		{label: "Scrobbling", value: func(a *App) string { return onOff(pb().Scrobble) },
			change: func(a *App, dir int) {
				on := !pb().Scrobble
				if pl := a.Player(); pl != nil {
					pl.SetScrobble(on)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.Scrobble = on }, false)
			},
			help: "Tells the server (and Last.fm, if the server is set up for it) what you listen to.", toggle: true},
		{label: "Transcode to", value: func(a *App) string { return pb().TranscodeFormat },
			change: func(a *App, dir int) {
				f := cycle([]string{"mp3", "flac", "wav"}, pb().TranscodeFormat, dir)
				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeFormat = f }, false)
				a.Toast("Used from the next connection")
			},
			help: "The server converts files the MiSTer can't play (AAC, OGG, Opus…) to this. FLAC, MP3 and WAV play as they are.", toggle: false},
	}
	if pb().TranscodeFormat == "mp3" { // the bitrate only matters for mp3
		rows = append(rows,
			setting{label: "Transcode bitrate", value: func(a *App) string { return fmt.Sprintf("%d kbps", pb().TranscodeBitrate) },
				change: func(a *App, dir int) {
					b := cycleNear([]int{128, 192, 256, 320}, pb().TranscodeBitrate, dir)
					a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeBitrate = b }, false)
					a.Toast("Used from the next connection")
				},
				help: "Quality of those conversions to MP3: higher sounds better and uses more network.", toggle: false})
	}
	return rows
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
		{label: "Layout", value: func(a *App) string { return d().Profile },
			change: func(a *App, dir int) {
				p := cycle([]string{"auto", "hdmi", "crt"}, d().Profile, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.Profile = p }, false)
				a.Toast("The layout changes the next time the app starts")
			},
			help: "HDMI or CRT screen layout; auto picks by the screen's lines. Takes effect the next time the app starts.", toggle: false},
		{label: "Screensaver", value: func(a *App) string {
			if m := d().ScreensaverMinutes; m > 0 {
				return fmt.Sprintf("after %d min", m)
			}
			return "Off"
		},
			change: func(a *App, dir int) {
				m := cycleNear(screensaverChoices, d().ScreensaverMinutes, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.ScreensaverMinutes = m }, false)
			},
			help: "Dims Now Playing and drifts the cover after this long without input.", toggle: false},
		{label: "Hints", value: func(a *App) string { return onOff(d().Hints) },
			change: func(a *App, dir int) {
				on := !d().Hints
				a.UpdateConfig(func(c *config.Config) { c.Display.Hints = on }, false)
			},
			help: "The bar of buttons along the bottom of every screen.", toggle: true},
		{label: "Visualizer", value: func(a *App) string { return a.VizStyle().Label() },
			change: func(a *App, dir int) {
				a.SetVizStyle(VizStyle((int(a.VizStyle()) + dir + len(vizNames)) % len(vizNames)))
			},
			help: "A moving picture of the music on Now Playing. Select there changes it too.", toggle: false},
	}
}

// remoteNote is the safety note shown with the remote's settings (spec §2).
const remoteNote = "Anyone on your network can control playback: there is no password, and it uses plain http."

func remoteSettings(a *App) []setting {
	enabled := func() bool { return a.cfg != nil && a.cfg.Remote.Enabled }
	return []setting{
		{label: "Remote", value: func(a *App) string { return onOff(enabled()) },
			change: func(a *App, dir int) { a.setRemote(!enabled()) },
			help:   "Control playback from a phone or computer's browser. " + remoteNote, toggle: true},
		{label: "Address", value: func(a *App) string {
			switch {
			case !enabled():
				return "Off"
			case len(a.remoteURLs) == 0:
				return "no network"
			}
			return strings.TrimSuffix(a.remoteURLs[0], "/")
		},
			change: func(*App, int) {}, info: true,
			help: "Open this address in a browser on the same network. " + remoteNote},
	}
}

// setRemote starts or stops the remote server and, once that worked, saves
// the choice.
func (a *App) setRemote(on bool) {
	sw := a.o.RemoteSwitch
	if sw == nil {
		a.Toast("Remote: not available")
		return
	}
	if err := sw.SetEnabled(on); err != nil {
		a.Toast("Remote: %v", err)
		return
	}
	a.UpdateConfig(func(c *config.Config) { c.Remote.Enabled = on }, false)
	a.remoteURLs = sw.URLs()
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
		a.moved(s.list.Moved())
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
	if a.cfg != nil && a.connecting {
		if srv, ok := a.cfg.ActiveServer(); ok && srv.Name == name {
			a.Toast("Already connecting to %s", name) // not dialled a second time
			return
		}
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
