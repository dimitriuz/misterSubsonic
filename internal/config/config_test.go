package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
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

// R14: the TOML parser quotes the offending text in its message ("found
// \"hunter\" instead"), which for an unquoted secret is the secret itself.
// The error shown on screen and in log.txt must say where, not what.
func TestParseErrorDoesNotEchoSecrets(t *testing.T) {
	_, _, err := Load(write(t, "[[server]]\nname=\"h\"\nurl=\"http://x:1\"\nusername=\"u\"\npassword = hunter2secret\n"))
	if err == nil {
		t.Fatal("want a parse error")
	}
	if strings.Contains(err.Error(), "hunter") {
		t.Fatalf("error echoes the secret: %v", err)
	}
	if !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("error does not say where: %v", err)
	}
}

// encodeForCompare renders c through the TOML encoder: equal text means equal values.
func encodeForCompare(c *Config) string {
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err.Error()
	}
	return b.String()
}

func TestVisualizerDefaultsToOffAndLoads(t *testing.T) {
	cfg, warns, err := Load(write(t, minimal))
	if err != nil || len(warns) != 0 || cfg.Display.Visualizer != "off" {
		t.Fatalf("default: %q, %v, %v", cfg.Display.Visualizer, warns, err)
	}
	for _, v := range []string{"off", "bars", "scope", "vu", "waterfall"} {
		cfg, warns, err := Load(write(t, minimal+"\n[display]\nvisualizer = \""+v+"\"\n"))
		if err != nil || len(warns) != 0 || cfg.Display.Visualizer != v {
			t.Errorf("%s: loaded %q, %v, %v", v, cfg.Display.Visualizer, warns, err)
		}
	}
}

func TestUnknownVisualizerLoadsAsOffWithAWarning(t *testing.T) {
	cfg, warns, err := Load(write(t, minimal+"\n[display]\nvisualizer = \"disco\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Display.Visualizer != "off" {
		t.Fatalf("visualizer = %q, want off", cfg.Display.Visualizer)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "display.visualizer") || !strings.Contains(warns[0], "disco") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestRemoteDefaultsToOffOnPort8080(t *testing.T) {
	if d := Default().Remote; d.Enabled || d.Port != 8080 {
		t.Fatalf("default remote = %+v", d)
	}
	cfg, warns, err := Load(write(t, minimal))
	if err != nil || len(warns) != 0 || cfg.Remote.Enabled || cfg.Remote.Port != 8080 {
		t.Fatalf("minimal: %+v, %v, %v", cfg.Remote, warns, err)
	}
	cfg, warns, err = Load(write(t, minimal+"\n[remote]\nenabled = true\nport = 9000\n"))
	if err != nil || len(warns) != 0 || !cfg.Remote.Enabled || cfg.Remote.Port != 9000 {
		t.Fatalf("set: %+v, %v, %v", cfg.Remote, warns, err)
	}
}

func TestBadRemotePortLoadsAs8080WithAWarning(t *testing.T) {
	for _, port := range []string{"0", "-1", "65536", "99999"} {
		cfg, warns, err := Load(write(t, minimal+"\n[remote]\nenabled = true\nport = "+port+"\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Remote.Port != 8080 || !cfg.Remote.Enabled {
			t.Errorf("port %s loaded as %+v, want 8080 and still enabled", port, cfg.Remote)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "remote.port") || !strings.Contains(warns[0], port) {
			t.Errorf("port %s: warnings = %v", port, warns)
		}
	}
	for _, port := range []string{"1", "65535"} {
		cfg, warns, _ := Load(write(t, minimal+"\n[remote]\nport = "+port+"\n"))
		if n, _ := strconv.Atoi(port); len(warns) != 0 || cfg.Remote.Port != n {
			t.Errorf("port %s: %+v %v", port, cfg.Remote, warns)
		}
	}
}

func TestOverscanDefaultsToZeroAndLoads(t *testing.T) {
	d := Default().Display
	if d.OverscanLeft != 0 || d.OverscanRight != 0 || d.OverscanY != 0 {
		t.Fatalf("default overscan = %+v", d)
	}
	cfg, warns, err := Load(write(t, minimal+"\n[display]\noverscan_left = 3\noverscan_right = 10\noverscan_y = 2\n"))
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v %v", warns, err)
	}
	if d := cfg.Display; d.OverscanLeft != 3 || d.OverscanRight != 10 || d.OverscanY != 2 {
		t.Fatalf("loaded %+v", d)
	}
}

func TestOverscanOutOfRangeClampsWithAWarning(t *testing.T) {
	cfg, warns, err := Load(write(t, minimal+"\n[display]\noverscan_left = -4\noverscan_right = 11\noverscan_y = 99\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d := cfg.Display; d.OverscanLeft != 0 || d.OverscanRight != 10 || d.OverscanY != 10 {
		t.Fatalf("clamped to %+v", d)
	}
	if len(warns) != 3 {
		t.Fatalf("warnings = %v", warns)
	}
	for i, key := range []string{"display.overscan_left", "display.overscan_right", "display.overscan_y"} {
		if !strings.Contains(warns[i], key) {
			t.Errorf("warning %d = %q, want it to name %s", i, warns[i], key)
		}
	}
}

func TestValidateReportsOverscanInOrder(t *testing.T) {
	c := Default()
	c.Display.OverscanLeft, c.Display.OverscanRight, c.Display.OverscanY = 11, -1, 99
	for range 20 {
		err := c.Validate()
		if err == nil {
			t.Fatal("out-of-range margins accepted")
		}
		msg := err.Error()
		l, r, y := strings.Index(msg, "overscan_left"), strings.Index(msg, "overscan_right"), strings.Index(msg, "overscan_y")
		if l < 0 || !(l < r && r < y) {
			t.Fatalf("errors out of order:\n%s", msg)
		}
	}
}
