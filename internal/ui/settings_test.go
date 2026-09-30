package ui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func connectedApp(t *testing.T) (*testApp, *connectRecorder) {
	t.Helper()
	ta, rec := sessionApp(t, twoServers())
	srv := ta.cfg.Servers[0]
	ta.Connected(ConnInfo{Server: srv, Info: &subsonic.ServerInfo{Type: "navidrome", ServerVersion: "0.59.0"}, Auth: subsonic.AuthToken}, ta.lib, ta.pl, fakeArt{})
	return ta, rec
}

func TestSettingsNoLongerHasExit(t *testing.T) {
	if slices.Contains(settingsItems, "Exit") {
		t.Fatalf("Settings lists Exit: %v", settingsItems)
	}
}

func TestPlaybackSettingsApplyAndSave(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnRight) // ReplayGain off -> track
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // scrobbling off
	ta.press(input.BtnDown)
	ta.press(input.BtnLeft) // transcode mp3 -> wav
	if !slices.Equal(ta.pl.calls, []string{"replaygain track", "scrobble off"}) {
		t.Fatalf("player calls %v", ta.pl.calls)
	}
	pb := ta.cfg.Playback
	if pb.ReplayGain != "track" || pb.Scrobble || pb.TranscodeFormat != "wav" {
		t.Fatalf("config %+v", pb)
	}
	ta.flushConfig()
	waitFile(t, ta.o.ConfigPath, `transcode_format = "wav"`)
}

func TestDisplaySettings(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Display", displaySettings))
	ta.press(input.BtnRight) // auto -> hdmi
	ta.press(input.BtnDown)
	ta.press(input.BtnLeft) // 5 min -> 2 min
	if d := ta.cfg.Display; d.Profile != "hdmi" || d.ScreensaverMinutes != 2 {
		t.Fatalf("display %+v", d)
	}
	if !strings.Contains(ta.toasts[0].text, "next time") {
		t.Fatalf("toast %q", ta.toasts[0].text)
	}
}

func TestNowPlayingVolumeIsSaved(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	if ta.cfg.Playback.VolumeDB != -2 || ta.saveAt.IsZero() {
		t.Fatalf("config volume %v, save pending %v", ta.cfg.Playback.VolumeDB, !ta.saveAt.IsZero())
	}
}

func TestServersSwitchAndRemove(t *testing.T) {
	ta, rec := connectedApp(t) // on "home"
	ta.Push(NewServersScreen())
	ta.press(input.BtnA) // home: already connected
	if len(rec.got) != 0 || !strings.HasPrefix(ta.toasts[0].text, "Already connected") {
		t.Fatalf("connects %v toasts %v", rec.got, ta.toasts)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // cloud
	if len(rec.got) != 1 || rec.got[0].DefaultServer != "cloud" {
		t.Fatalf("switch connects %v", rec.got)
	}
	// Remove the connected server: the next default connects.
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[1]}, ta.lib, ta.pl, fakeArt{})
	ta.Push(NewServersScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Remove
	ta.press(input.BtnA) // confirm
	if names := serverNames(ta.cfg); !slices.Equal(names, []string{"home"}) || len(rec.got) != 2 || rec.got[1].DefaultServer != "home" {
		t.Fatalf("servers %v, connects %d", names, len(rec.got))
	}
	// Remove the last: disconnect and set up again.
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[0]}, ta.lib, ta.pl, fakeArt{})
	ta.Push(NewServersScreen())
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	ta.press(input.BtnA)
	if _, ok := ta.Top().(*WizardScreen); !ok || ta.Player() != nil || len(rec.got[2].Servers) != 0 {
		t.Fatalf("after removing the last: top %T, player %v", ta.Top(), ta.Player())
	}
}

func serverNames(c *config.Config) []string {
	var out []string
	for _, s := range c.Servers {
		out = append(out, s.Name)
	}
	return out
}

func TestServersAddOpensTheWizard(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(NewServersScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // Add a server
	ta.press(input.BtnA)
	w, ok := ta.Top().(*WizardScreen)
	if !ok || w.firstRun || w.Title() != "Add a server" {
		t.Fatalf("top %T", ta.Top())
	}
	ta.press(input.BtnB) // the first step: back to the list
	if _, ok := ta.Top().(*ServersScreen); !ok {
		t.Fatalf("B from the wizard: %T", ta.Top())
	}
}

func TestAboutDescribesTheConnection(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.o.Version = "v0.2.0"
	lines := strings.Join((&AboutScreen{}).lines(ta.App), "\n")
	for _, want := range []string{"MiSTer Subsonic v0.2.0", "home — http://192.168.1.10:4533", "Navidrome 0.59.0, login by token"} {
		if !strings.Contains(lines, want) {
			t.Errorf("About lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "pw") {
		t.Fatal("About shows the password")
	}
	ta.Detach()
	if l := (&AboutScreen{}).lines(ta.App); l[1] != "Not connected" {
		t.Fatalf("disconnected About %v", l)
	}
}

func TestSettingsWorkWithoutAConnection(t *testing.T) {
	ta, _ := sessionApp(t, twoServers()) // detached: no player
	ta.ConnectFailed(ta.cfg.Servers[0], errUnreachable)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Settings
	ta.press(input.BtnDown)
	ta.press(input.BtnA)     // Playback
	ta.press(input.BtnRight) // ReplayGain off -> track
	if ta.cfg.Playback.ReplayGain != "track" {
		t.Fatalf("replaygain %q", ta.cfg.Playback.ReplayGain)
	}
}

func TestGoldenSettings(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.cfg, ta.o.ConfigPath = twoServers(), filepath.Join(t.TempDir(), "config.toml")
		ta.Connected(ConnInfo{Server: ta.cfg.Servers[0], Info: &subsonic.ServerInfo{Type: "navidrome", ServerVersion: "0.59.0"}, Auth: subsonic.AuthToken}, ta.lib, ta.pl, fakeArt{})
		ta.Push(newSettingsList("Playback", playbackSettings))
		golden(t, "settings-playback-"+p.Name, ta.settle(t))
		ta.Replace(NewServersScreen())
		golden(t, "settings-servers-"+p.Name, ta.settle(t))
	}
}

var errUnreachable = errors.New("dial tcp 192.168.1.10:4533: connection refused")

func TestRemovingTheServerBeingConnectedReconnects(t *testing.T) {
	ta, rec := connectedApp(t) // on "home"
	ta.Push(NewServersScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // switch to cloud: the connect is in flight, Conn() is empty
	if len(rec.got) != 1 {
		t.Fatalf("connects %d", len(rec.got))
	}
	ta.Push(NewServersScreen()) // Settings stays reachable while connecting
	ta.press(input.BtnDown)
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Remove
	ta.press(input.BtnA) // confirm
	if names := serverNames(ta.cfg); !slices.Equal(names, []string{"home"}) || len(rec.got) != 2 || rec.got[1].DefaultServer != "home" {
		t.Fatalf("servers %v, connects %d", names, len(rec.got))
	}
}

func TestRemovingTheUnreachableServerReconnects(t *testing.T) {
	ta, rec := sessionApp(t, twoServers())
	ta.ConnectFailed(ta.cfg.Servers[0], errUnreachable)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Settings
	ta.press(input.BtnA) // Servers
	ta.press(input.BtnX) // home, the failing default
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Remove
	ta.press(input.BtnA) // confirm
	if names := serverNames(ta.cfg); !slices.Equal(names, []string{"cloud"}) || len(rec.got) != 1 || rec.got[0].DefaultServer != "cloud" {
		t.Fatalf("servers %v, connects %v", names, rec.got)
	}
}

// A volume changed while disconnected reaches the next player; a connect
// without such a change leaves the player's volume alone.
func TestVolumeChangedWhileDisconnectedReachesTheNextPlayer(t *testing.T) {
	ta, _ := sessionApp(t, twoServers())
	ta.Detach()
	ta.setVolume(-20)
	pl := newFakePlayer()
	pl.st.VolumeDB = -5
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[0]}, ta.lib, pl, fakeArt{})
	if pl.st.VolumeDB != -20 {
		t.Fatalf("new player at %v dB, want -20", pl.st.VolumeDB)
	}
	// The flag is spent: a later volume path that skips the config is kept.
	pl2 := newFakePlayer()
	pl2.st.VolumeDB = -7
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[0]}, ta.lib, pl2, fakeArt{})
	if pl2.st.VolumeDB != -7 {
		t.Fatalf("player without a detached change moved to %v dB", pl2.st.VolumeDB)
	}
}

func labels(rows []setting) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.label)
	}
	return out
}

func TestPlaybackSettingsHaveNoVolumeOrMute(t *testing.T) {
	ta, _ := connectedApp(t)
	got := labels(playbackSettings(ta.App))
	for _, l := range got {
		if l == "Volume" || l == "Mute" {
			t.Fatalf("rows %v", got)
		}
	}
}

func TestTranscodeBitrateRowOnlyForMP3(t *testing.T) {
	ta, _ := connectedApp(t)
	for f, want := range map[string]bool{"mp3": true, "flac": false, "wav": false} {
		ta.cfg.Playback.TranscodeFormat = f
		if has := slices.Contains(labels(playbackSettings(ta.App)), "Transcode bitrate"); has != want {
			t.Errorf("%s: bitrate row shown %v, want %v", f, has, want)
		}
	}
	// The focus stays on a real row when the bitrate row goes away.
	ta.cfg.Playback.TranscodeFormat = "mp3"
	s := newSettingsList("Playback", playbackSettings)
	ta.Push(s)
	ta.settle(t)
	s.list.Focus = len(playbackSettings(ta.App)) - 1 // bitrate
	ta.cfg.Playback.TranscodeFormat = "flac"
	ta.press(input.BtnRight) // must not panic
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	if n := len(playbackSettings(ta.App)); s.list.Focus >= n || s.helpText(ta.App) == "" {
		t.Fatalf("focus %d of %d rows", s.list.Focus, n)
	}
}

func TestSettingsShowTheFocusedRowsHelp(t *testing.T) {
	for _, mk := range []func(*App) []setting{playbackSettings, displaySettings} {
		ta, _ := connectedApp(t)
		s := newSettingsList("x", mk)
		ta.Push(s)
		ta.settle(t)
		seen := map[string]bool{}
		for i := range mk(ta.App) {
			h := s.helpText(ta.App)
			if h == "" || seen[h] {
				t.Fatalf("row %d: help %q", i, h)
			}
			seen[h] = true
			ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Press})
			ta.settle(t) // the verify mode checks the partial frame
		}
	}
	ta, _ := connectedApp(t)
	s := newSettingsList("Playback", playbackSettings)
	ta.Push(s)
	if !strings.HasPrefix(s.helpText(ta.App), "Evens out loudness") {
		t.Fatalf("help %q", s.helpText(ta.App))
	}
}

// Every help text fits its two lines on every layout, untruncated.
func TestSettingsHelpFitsTwoLines(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.cfg = config.Default()
		for _, mk := range []func(*App) []setting{playbackSettings, displaySettings} {
			for _, r := range mk(ta.App) {
				ls := helpLines(ta.F.Small, r.help, p.W-2*p.Margin)
				if len(ls) > 2 || strings.HasSuffix(ls[len(ls)-1], "…") || len(wrap(ta.F.Small, r.help, p.W-2*p.Margin)) > 2 {
					t.Errorf("%s: %q needs more than two lines: %v", p.Name, r.label, ls)
				}
			}
		}
	}
}

func TestHeldKeyFlipsAnOnOffRowOnce(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnDown) // Scrobbling (on)
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
	for range 5 {
		ta.dispatch(input.Event{Button: input.BtnRight, Kind: input.Repeat})
	}
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Release})
	if ta.cfg.Playback.Scrobble {
		t.Fatal("holding Right flipped Scrobbling more than once (it is still on)")
	}
	if n := len(ta.pl.calls); n != 1 {
		t.Fatalf("player calls %v, want one scrobble change", ta.pl.calls)
	}
}

func TestHeldKeyKeepsCyclingAChoiceRow(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
	ta.dispatch(input.Event{Button: input.BtnRight, Kind: input.Repeat})
	if got := ta.cfg.Playback.ReplayGain; got != "album" {
		t.Fatalf("ReplayGain %q after Press+Repeat, want album", got)
	}
}

func TestCycleFromAHandEditedValue(t *testing.T) {
	bitrates := []int{128, 192, 256, 320}
	for _, c := range []struct{ cur, dir, want int }{
		{160, 1, 192}, {160, -1, 128},
		{500, 1, 128}, {500, -1, 320}, // beyond the ends: wraps like the ends do
		{50, 1, 128}, {50, -1, 320},
		{192, 1, 256}, {128, -1, 320}, // listed values are unchanged
	} {
		if got := cycleNear(bitrates, c.cur, c.dir); got != c.want {
			t.Errorf("cycleNear(%d, %+d) = %d, want %d", c.cur, c.dir, got, c.want)
		}
	}
}

func TestSettingsRowsStartFromTheNearestChoice(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Playback.TranscodeFormat = "mp3"
	ta.cfg.Playback.TranscodeBitrate = 160
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // bitrate
	ta.press(input.BtnRight)
	if got := ta.cfg.Playback.TranscodeBitrate; got != 192 {
		t.Fatalf("160 -> Right = %d, want 192", got)
	}
	ta.cfg.Playback.TranscodeBitrate = 160
	ta.press(input.BtnLeft)
	if got := ta.cfg.Playback.TranscodeBitrate; got != 128 {
		t.Fatalf("160 -> Left = %d, want 128", got)
	}
}

func TestScreensaverRowStartsFromTheNearestChoice(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.cfg.Display.ScreensaverMinutes = 7
	ta.Push(newSettingsList("Display", displaySettings))
	ta.press(input.BtnDown)
	ta.press(input.BtnRight)
	if got := ta.cfg.Display.ScreensaverMinutes; got != 10 {
		t.Fatalf("7 -> Right = %d, want 10", got)
	}
}
