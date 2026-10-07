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

func addWork(c *Config) {
	c.AddServer(Server{Name: "work", URL: "https://music.example.com", Username: "bob", Token: "t0k", Salt: "s4lt"})
}

func TestSaveCommentOnlyFileWithoutNewline(t *testing.T) {
	got, _ := saveEdited(t, "# only", addWork)
	wantAll(t, got, `name = "work"`)
}

func TestSaveEmptyFileGetsAServer(t *testing.T) {
	got, _ := saveEdited(t, "", addWork)
	wantAll(t, got, `name = "work"`)
}

func TestSaveKeepsABOMBeforeTheFirstKey(t *testing.T) {
	body := "\xEF\xBB\xBFdefault_server = \"home\"   # first\n" + strings.TrimPrefix(commented, "# My own notes on this file.\ndefault_server = \"home\"   # the one at home\n")
	got, _ := saveEdited(t, body, func(c *Config) {
		c.AddServer(Server{Name: "work", URL: "https://music.example.com", Username: "bob", Token: "t0k", Salt: "s4lt"})
		c.DefaultServer = "home"
		c.Playback.VolumeDB = -6
	})
	wantAll(t, got, "\xEF\xBB\xBFdefault_server = \"home\"   # first", "volume_db = -6.0   # keep it quiet at night", "# --- playback ---")
}

func TestSaveBOMKeyChangeEditsInPlace(t *testing.T) {
	body := "\xEF\xBB\xBFdefault_server = \"home\"   # first\n" + strings.TrimPrefix(commented, "# My own notes on this file.\ndefault_server = \"home\"   # the one at home\n") +
		"\n[[server]]\nname = \"work\"\nurl = \"http://w:4533\"\nusername = \"bob\"\npassword = \"pw\"\n"
	got, _ := saveEdited(t, body, func(c *Config) { c.DefaultServer = "work" })
	wantAll(t, got, "\xEF\xBB\xBFdefault_server = \"work\"   # first", "# --- playback ---", "custom = 1 # not ours")
}

func TestSaveQuotedKeyUntouchedIsEditedInPlace(t *testing.T) {
	got, _ := saveEdited(t, "\"default_server\" = \"home\"   # mine\n"+minimal, func(c *Config) { c.Playback.VolumeDB = -9 })
	wantAll(t, got, "\"default_server\" = \"home\"   # mine", "volume_db = -9.0")
}

func TestSaveQuotedKeyThatChangesIsEditedInPlace(t *testing.T) {
	for _, q := range []string{`"default_server"`, `'default_server'`, `"default_server" `} {
		got, _ := saveEdited(t, q+"= \"home\"   # mine\n"+minimal, addWork)
		if !strings.HasPrefix(got, q+"= ") || strings.Count(got, "default_server") != 1 {
			t.Errorf("quoted key %s was not edited in place:\n%s", q, got)
		}
	}
}

const twoServers = commented + "\n[[server]]\nname = \"work\"\nurl = \"http://w:4533\"\nusername = \"bob\"\npassword = \"pw\"\n"

func TestSaveReorderedServersFallBack(t *testing.T) {
	got, _ := saveEdited(t, twoServers, func(c *Config) { c.Servers[0], c.Servers[1] = c.Servers[1], c.Servers[0] })
	if !strings.HasPrefix(got, "# MiSTer Subsonic configuration") {
		t.Fatalf("expected a fresh file:\n%s", got)
	}
}

func TestSaveRenamedServer(t *testing.T) {
	got, _ := saveEdited(t, twoServers, func(c *Config) { c.Servers[1].Name = "office" })
	wantAll(t, got, "# My own notes on this file.", `name = "office"`, "# --- playback ---")
	if strings.Contains(got, `"work"`) {
		t.Errorf("old name left:\n%s", got)
	}
}

// a [server.x] table belongs to the [[server]] above it
const withSubtable = "" +
	"# My own notes on this file.\ndefault_server = \"home\"\n\n[[server]]\nname = \"home\"\nurl = \"http://192.168.1.10:4533\"\nusername = \"alice\"\npassword = \"sesame\"\n\n" +
	"[server.extra]\nnote = \"mine\"\n\n[[server]]\nname = \"work\"\nurl = \"http://w:4533\"\nusername = \"bob\"\npassword = \"pw\"\n\n[extra]\ncustom = 1 # not ours\n"

func TestSaveRemovedServerTakesItsSubtable(t *testing.T) {
	got, _ := saveEdited(t, withSubtable, func(c *Config) { c.RemoveServer("home") })
	if strings.Contains(got, "server.extra") || strings.Contains(got, `note = "mine"`) {
		t.Errorf("the subtable of the removed server was left behind:\n%s", got)
	}
	wantAll(t, got, `name = "work"`, "[extra]")
}

func TestSaveChangedServerKeepsItsSubtable(t *testing.T) {
	got, _ := saveEdited(t, withSubtable, func(c *Config) { c.Servers[0].Password = "other" })
	wantAll(t, got, "[server.extra]", `note = "mine"`, `password = "other"`, `name = "work"`)
	if i, j := strings.Index(got, `password = "other"`), strings.Index(got, "[server.extra]"); i > j {
		t.Errorf("the subtable moved above the server's keys:\n%s", got)
	}
}

func TestSaveChangedServerKeepsTheCommentAboveItsSubtable(t *testing.T) {
	src := strings.Replace(withSubtable, "[server.extra]", "# about the extras\n[server.extra]", 1)
	got, _ := saveEdited(t, src, func(c *Config) { c.Servers[0].Password = "other" })
	wantAll(t, got, "\n\n# about the extras\n[server.extra]", `note = "mine"`, `password = "other"`)
}

func TestEditSetsTheVisualizerInPlace(t *testing.T) {
	got, cfg := saveEdited(t, commented, func(c *Config) { c.Display.Visualizer = "scope" })
	wantAll(t, got, "[display]\nhints = true\nvisualizer = \"scope\"", "# My own notes on this file.", "keep it quiet at night")
	if cfg.Display.Visualizer != "scope" {
		t.Fatal(cfg.Display.Visualizer)
	}
	again, _ := saveEdited(t, got, func(c *Config) { c.Display.Visualizer = "vu" })
	wantAll(t, again, "visualizer = \"vu\"")
	if strings.Count(again, "visualizer") != 1 {
		t.Fatalf("visualizer written twice:\n%s", again)
	}
}

func TestEditSetsTheRemoteInPlace(t *testing.T) {
	got, _ := saveEdited(t, commented, func(c *Config) { c.Remote.Enabled = true })
	wantAll(t, got, "[remote]\nenabled = true", "# My own notes on this file.", "keep it quiet at night")
	again, cfg := saveEdited(t, got, func(c *Config) { c.Remote.Port = 9090; c.Remote.Enabled = false })
	wantAll(t, again, "enabled = false", "port = 9090")
	if strings.Count(again, "enabled") != 1 || strings.Count(again, "[remote]") != 1 {
		t.Fatalf("remote keys written twice:\n%s", again)
	}
	if cfg.Remote.Enabled || cfg.Remote.Port != 9090 {
		t.Fatal(cfg.Remote)
	}
}

func TestEditSetsTheOverscanInPlace(t *testing.T) {
	got, cfg := saveEdited(t, commented, func(c *Config) { c.Display.OverscanLeft = 3; c.Display.OverscanY = 2 })
	wantAll(t, got, "overscan_left = 3", "overscan_y = 2", "# My own notes on this file.")
	again, cfg2 := saveEdited(t, got, func(c *Config) { c.Display.OverscanLeft = 4; c.Display.OverscanRight = 1 })
	wantAll(t, again, "overscan_left = 4", "overscan_right = 1", "overscan_y = 2")
	if strings.Count(again, "overscan_left") != 1 {
		t.Fatalf("overscan_left written twice:\n%s", again)
	}
	if cfg.Display.OverscanLeft != 3 || cfg2.Display.OverscanLeft != 4 || cfg2.Display.OverscanRight != 1 {
		t.Fatal(cfg.Display, cfg2.Display)
	}
}
