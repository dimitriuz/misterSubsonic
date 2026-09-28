package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const minimal = `
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "sesame"
`

func TestLoadMinimalFillsDefaults(t *testing.T) {
	cfg, warns, err := Load(write(t, minimal))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %v", warns)
	}
	want := Default().Playback
	if cfg.Playback != want || cfg.Display.Profile != "auto" || cfg.Cache.CoverArtMB != 200 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	s, ok := cfg.ActiveServer()
	if !ok || s.Name != "home" {
		t.Fatalf("active server = %+v", s)
	}
}

func TestExplicitFalseOverridesDefaultTrue(t *testing.T) {
	cfg, _, err := Load(write(t, minimal+"\n[playback]\nscrobble = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Playback.Scrobble {
		t.Fatal("scrobble = false was ignored")
	}
}

func TestMissingFile(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownKeysAreWarnings(t *testing.T) {
	_, warns, err := Load(write(t, minimal+"\n[playback]\nreplay_gain = \"track\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "replay_gain") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestSyntaxErrorLeavesFileAlone(t *testing.T) {
	p := write(t, "[[server]\nname=")
	before, _ := os.ReadFile(p)
	_, _, err := Load(p)
	if err == nil {
		t.Fatal("malformed file loaded")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("Load modified a malformed file")
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"bad url":          strings.Replace(minimal, "http://192.168.1.10:4533", "192.168.1.10:4533", 1),
		"no credentials":   strings.Replace(minimal, `password = "sesame"`, "", 1),
		"token w/o salt":   strings.Replace(minimal, `password = "sesame"`, `token = "abc"`, 1),
		"no username":      strings.Replace(minimal, `username = "alice"`, "", 1),
		"bad replaygain":   minimal + "\n[playback]\nreplaygain = \"loud\"\n",
		"raw transcode":    minimal + "\n[playback]\ntranscode_format = \"raw\"\n",
		"opus transcode":   minimal + "\n[playback]\ntranscode_format = \"opus\"\n",
		"tiny buffer":      minimal + "\n[playback]\nbuffer_mb = 1\n",
		"bad profile":      minimal + "\n[display]\nprofile = \"vga\"\n",
		"unknown default":  "default_server = \"work\"\n" + minimal,
		"duplicate server": minimal + minimal,
	}
	for name, body := range cases {
		if _, _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: loaded without error", name)
		}
	}
}

func TestAPIKeyOnlyServerIsValid(t *testing.T) {
	body := "[[server]]\nname = \"k\"\nurl = \"https://music.example.com\"\napi_key = \"key\"\n"
	if _, _, err := Load(write(t, body)); err != nil {
		t.Fatal(err)
	}
}

func TestMultipleServersAndDefault(t *testing.T) {
	body := "default_server = \"work\"\n" + minimal + strings.Replace(minimal, `"home"`, `"work"`, 1)
	cfg, _, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := cfg.ActiveServer(); s.Name != "work" {
		t.Fatalf("active = %q", s.Name)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	cfg := Default()
	cfg.Servers = []Server{{Name: "home", URL: "https://music.example.com", Username: "alice", Token: "t0k", Salt: "s4lt", CAFile: "/media/fat/ca.pem"}}
	cfg.Playback.ReplayGain = "album"
	cfg.Playback.Scrobble = false
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(p)
	if err != nil || len(warns) != 0 {
		t.Fatalf("reload: %v %v", err, warns)
	}
	if got.Servers[0] != cfg.Servers[0] || got.Playback != cfg.Playback {
		t.Fatalf("round trip changed config:\n got %+v\nwant %+v", got, cfg)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "password") {
		t.Fatal("empty password written to file")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600 (it holds credentials)", fi.Mode().Perm())
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	cfg := Default()
	cfg.Playback.BufferMB = 0
	if err := Save(filepath.Join(t.TempDir(), "c.toml"), cfg); err == nil {
		t.Fatal("saved an invalid config")
	}
}

func TestBackupInvalid(t *testing.T) {
	p := write(t, "garbage")
	dst, err := BackupInvalid(p, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dst, "config.toml.invalid-20260928T120000Z") {
		t.Fatalf("backup name = %s", dst)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("original still present")
	}
}

// Configs edited in Windows Notepad over SMB arrive with a BOM and CRLFs.
func TestLoadToleratesBOMAndCRLF(t *testing.T) {
	body := "\xef\xbb\xbf" + strings.ReplaceAll(minimal, "\n", "\r\n")
	cfg, _, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := cfg.ActiveServer(); s.Password != "sesame" {
		t.Fatalf("password = %q", s.Password)
	}
}
