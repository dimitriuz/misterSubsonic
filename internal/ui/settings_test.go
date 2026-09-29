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

func TestSettingsExitAsks(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(NewSettingsScreen())
	for range 4 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Exit
	if !ta.confirm {
		t.Fatal("Exit did not ask")
	}
	ta.press(input.BtnB)
	if ta.confirm || ta.quit {
		t.Fatal("B did not cancel the exit prompt")
	}
}

func TestPlaybackSettingsApplyAndSave(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.pl.st.VolumeDB = -10
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnRight) // volume +1
	ta.press(input.BtnDown)
	ta.press(input.BtnRight) // ReplayGain off -> track
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // scrobbling off
	ta.press(input.BtnDown)
	ta.press(input.BtnLeft) // transcode mp3 -> wav
	if ta.pl.st.VolumeDB != -9 || !slices.Equal(ta.pl.calls, []string{"replaygain track", "scrobble off"}) {
		t.Fatalf("volume %v, player calls %v", ta.pl.st.VolumeDB, ta.pl.calls)
	}
	pb := ta.cfg.Playback
	if pb.VolumeDB != -9 || pb.ReplayGain != "track" || pb.Scrobble || pb.TranscodeFormat != "wav" {
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
	ta.press(input.BtnA) // Playback
	ta.press(input.BtnLeft)
	if ta.cfg.Playback.VolumeDB != -1 {
		t.Fatalf("volume %v", ta.cfg.Playback.VolumeDB)
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
