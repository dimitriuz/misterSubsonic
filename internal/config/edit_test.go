package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commented = `# My own notes on this file.
default_server = "home"   # the one at home

# --- playback ---
[playback]
volume_db = -12.0   # keep it quiet at night
replaygain = "off"
# scrobble stays on

[display]
hints = true

# my servers
[[server]]
# the NAS
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "sesame"   # TODO: use a token

[extra]
custom = 1 # not ours
`

func saveEdited(t *testing.T, body string, change func(*Config)) (string, *Config) {
	t.Helper()
	p := write(t, body)
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	change(cfg)
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got, _, err := Load(p)
	if err != nil {
		t.Fatalf("saved file does not load: %v", err)
	}
	if enc1, enc2 := encodeForCompare(got), encodeForCompare(cfg); enc1 != enc2 {
		t.Fatalf("reload differs from what was saved:\n%s\nvs\n%s", enc1, enc2)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	return string(b), cfg
}

func wantAll(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got, p) {
			t.Errorf("saved file lost %q:\n%s", p, got)
		}
	}
}

func TestSaveKeepsCommentsOnVolumeChange(t *testing.T) {
	got, _ := saveEdited(t, commented, func(c *Config) { c.Playback.VolumeDB = -6 })
	wantAll(t, got, "# My own notes on this file.", "default_server = \"home\"   # the one at home", "# --- playback ---",
		"volume_db = -6.0   # keep it quiet at night", "# scrobble stays on", "# my servers", "# the NAS",
		"password = \"sesame\"   # TODO: use a token", "[extra]", "custom = 1 # not ours")
	if strings.Contains(got, "-12") {
		t.Errorf("old volume left in file:\n%s", got)
	}
}

func TestSaveKeepsCommentsOnServerAdd(t *testing.T) {
	got, cfg := saveEdited(t, commented, func(c *Config) {
		c.AddServer(Server{Name: "work", URL: "https://music.example.com", Username: "bob", Token: "t0k", Salt: "s4lt"})
	})
	wantAll(t, got, "# My own notes on this file.", "# --- playback ---", "volume_db = -12.0   # keep it quiet at night",
		"# the NAS", "password = \"sesame\"   # TODO: use a token", "custom = 1 # not ours", `name = "work"`)
	if !strings.Contains(got, `default_server = "work"   # the one at home`) || cfg.DefaultServer != "work" {
		t.Errorf("default_server not updated in place:\n%s", got)
	}
	if strings.Index(got, `name = "work"`) > strings.Index(got, "[extra]") {
		t.Errorf("new server landed after [extra]:\n%s", got)
	}
}

func TestSaveKeepsCommentsOnServerRemove(t *testing.T) {
	body := commented + "\n[[server]]\nname = \"work\"\nurl = \"http://w:4533\"\nusername = \"bob\"\npassword = \"pw\"\n"
	got, _ := saveEdited(t, body, func(c *Config) { c.RemoveServer("home") })
	wantAll(t, got, "# My own notes on this file.", "# --- playback ---", "[extra]", "custom = 1 # not ours", `name = "work"`, `default_server = "work"`)
	if strings.Contains(got, `"home"`) || strings.Contains(got, "sesame") {
		t.Errorf("removed server still in file:\n%s", got)
	}
}

func TestSaveEditsAChangedServerInPlace(t *testing.T) {
	got, _ := saveEdited(t, commented, func(c *Config) { c.Servers[0].Password = ""; c.Servers[0].Token, c.Servers[0].Salt = "tok", "salt" })
	wantAll(t, got, "# my servers", "token = \"tok\"", "[extra]", "custom = 1 # not ours")
	if strings.Contains(got, "sesame") {
		t.Errorf("old password left:\n%s", got)
	}
}

func TestSaveAddsMissingKeysAndSections(t *testing.T) {
	got, _ := saveEdited(t, minimal, func(c *Config) {
		c.Playback.ReplayGain = "album"
		c.Display.Hints = false
		c.Cache.CoverArtMB = 50
	})
	wantAll(t, got, `replaygain = "album"`, "hints = false", "cover_art_mb = 50", `password = "sesame"`)
}

func TestSaveWithoutChangesLeavesTheFileAlone(t *testing.T) {
	p := write(t, commented)
	cfg, _, _ := Load(p)
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Fatalf("file changed:\n%s", b)
	}
}

func TestSaveKeepsCRLF(t *testing.T) {
	body := strings.ReplaceAll(commented, "\n", "\r\n")
	got, _ := saveEdited(t, body, func(c *Config) {
		c.Playback.VolumeDB = -3
		c.Display.FullResolution = false
	})
	wantAll(t, got, "# --- playback ---", "volume_db = -3.0   # keep it quiet at night")
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("bare LF in a CRLF file:\n%q", got)
	}
}

func TestSaveFallsBackOnLayoutItCannotEdit(t *testing.T) {
	// an inline table: the line editor cannot change one key in it
	got, _ := saveEdited(t, "playback = { volume_db = -5.0 }\n"+minimal, func(c *Config) { c.Playback.VolumeDB = -9 })
	if !strings.HasPrefix(got, "# MiSTer Subsonic configuration") {
		t.Fatalf("expected a fresh file:\n%s", got)
	}
}

func TestSaveOverGarbageWritesAFreshFile(t *testing.T) {
	p := write(t, "not toml [[[")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") {
		t.Fatalf("no header:\n%s", b)
	}
}

func TestSaveCreatesAFreshFileWhenThereIsNone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err != nil {
		t.Fatal(err)
	}
}
