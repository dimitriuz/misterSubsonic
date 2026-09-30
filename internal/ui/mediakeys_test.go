package ui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// Media keys act on every screen: volume (saved, held to repeat, unmuting),
// play/pause, next, previous and seeking, even while Search takes letters.
func TestMediaKeysActEverywhere(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.pl.st.VolumeDB = -20
	ta.Push(NewSearchScreen()) // a text screen: the media keys type nothing
	ta.press(input.BtnVolUp)
	ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Repeat})
	if ta.pl.st.VolumeDB != -18 || ta.cfg.Playback.VolumeDB != -18 {
		t.Fatalf("volume %v (config %v), want -18", ta.pl.st.VolumeDB, ta.cfg.Playback.VolumeDB)
	}
	ta.press(input.BtnMute)
	ta.press(input.BtnVolDown)
	if ta.muted || ta.pl.st.VolumeDB != -19 {
		t.Fatalf("volume down while muted: muted %v at %v", ta.muted, ta.pl.st.VolumeDB)
	}
	ta.pl.calls = nil
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack} {
		ta.press(b)
	}
	if !slices.Equal(ta.pl.calls, []string{"toggle", "next", "prev"}) {
		t.Fatalf("player calls %v", ta.pl.calls)
	}
	ta.pl.st.Position = time.Minute
	ta.press(input.BtnSeekFwd)
	if ta.pl.seekPos != time.Minute+seekStep {
		t.Fatalf("seek to %v", ta.pl.seekPos)
	}
	ta.press(input.BtnSeekBack)
	if ta.pl.seekPos != time.Minute-seekStep {
		t.Fatalf("seek back to %v", ta.pl.seekPos)
	}
	if s, ok := ta.Top().(*SearchScreen); !ok || len(s.query) != 0 {
		t.Fatal("a media key typed into Search or left it")
	}
}

// With nothing playing, the transport keys do nothing (and don't panic).
func TestMediaKeysWithoutAQueue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack, input.BtnSeekFwd} {
		ta.press(b)
	}
	if slices.Contains(ta.pl.calls, "next") || slices.Contains(ta.pl.calls, "seek") {
		t.Fatalf("player calls %v with no queue", ta.pl.calls)
	}
}

// A volume or mute key never writes a config the user didn't set up: not over
// a config file with a problem, and not when there is none yet.
func TestVolumeKeysNeverWriteAConfigNobodySetUp(t *testing.T) {
	broken := []byte("this is [not toml\n")
	for _, tc := range []struct {
		name string
		err  error
		file []byte
	}{
		{"invalid", errors.New("config: invalid TOML"), broken},
		{"missing", config.ErrNotFound, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta, _ := sessionApp(t, nil)
			ta.o.ConfigErr = tc.err
			if tc.file != nil {
				if err := os.WriteFile(ta.o.ConfigPath, tc.file, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ta.start()
			ta.press(input.BtnVolUp)
			ta.press(input.BtnVolDown)
			ta.press(input.BtnMute)
			ta.press(input.BtnMute)
			ta.now = ta.now.Add(saveDelay + time.Second)
			ta.onWake()
			waitSaves(ta)
			ta.flushConfig()
			got, err := os.ReadFile(ta.o.ConfigPath)
			if tc.file == nil {
				if err == nil {
					t.Fatalf("a config was created: %q", got)
				}
			} else if err != nil || !bytes.Equal(got, tc.file) {
				t.Fatalf("config file changed: %q, %v", got, err)
			}
			if ta.cfg != nil {
				t.Fatal("a default config was created in memory")
			}
			if ents, _ := os.ReadDir(filepath.Dir(ta.o.ConfigPath)); len(ents) > 1 || (tc.file == nil && len(ents) > 0) {
				t.Fatalf("stray files: %v", ents)
			}
		})
	}
}

// Without a config the level is kept for the next player.
func TestVolumeWithoutAConfigIsKeptForThePlayer(t *testing.T) {
	ta, _ := sessionApp(t, nil)
	ta.o.ConfigErr = config.ErrNotFound
	ta.press(input.BtnVolDown)
	ta.press(input.BtnVolDown)
	if ta.volumeDB() != -2 || ta.cfg != nil {
		t.Fatalf("volume %v, cfg %v", ta.volumeDB(), ta.cfg)
	}
	ta.cfg = twoServers()
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[0], Info: &subsonic.ServerInfo{}}, ta.lib, ta.pl, fakeArt{})
	if ta.pl.st.VolumeDB != -2 {
		t.Fatalf("player volume %v, want -2", ta.pl.st.VolumeDB)
	}
}
