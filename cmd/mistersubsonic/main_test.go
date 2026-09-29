package main

import (
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
)

func TestDisplayURLHidesCredentials(t *testing.T) {
	got := displayURL("http://alice:s3cret@host:4533/nav")
	if strings.Contains(got, "s3cret") || strings.Contains(got, "alice") || !strings.Contains(got, "host:4533") {
		t.Fatalf("displayURL leaked or lost host: %q", got)
	}
	if got := displayURL("http://host:4533/nav"); got != "http://host:4533/nav" {
		t.Fatalf("plain URL changed: %q", got)
	}
	if got := displayURL("http://[bad"); got != "<server>" {
		t.Fatalf("garbage: %q", got)
	}
}

func TestNoAudioDeviceShowsMessage(t *testing.T) {
	old := openDevice
	openDevice = func(audio.DeviceOptions) (audio.Output, error) { return nil, errors.New("boom") }
	defer func() { openDevice = old }()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	os.WriteFile(cfg, []byte("[[server]]\nname = \"x\"\nurl = \"http://127.0.0.1:1\"\nusername = \"u\"\npassword = \"p\"\n"), 0o600)
	frames := filepath.Join(dir, "frames", "nested")
	err := run(flags{config: cfg, display: "headless", frames: frames, volume: math.NaN(), exitAfter: 1e9})
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	pngs, _ := filepath.Glob(filepath.Join(frames, "*.png"))
	if len(pngs) == 0 {
		t.Fatal("no frame presented")
	}
}

func TestStartVolume(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name         string
		cfg, flag    float64
		null         bool
		goos, goarch string
		want         float64
		quiet        bool
	}{
		{"desktop default", -6, nan, false, "linux", "amd64", -30, true},
		{"desktop, any display", 0, nan, false, "darwin", "arm64", -30, true},
		{"desktop already quieter", -40, nan, false, "linux", "amd64", -40, false},
		{"desktop explicit flag wins", -6, -10, false, "linux", "amd64", -10, false},
		{"desktop null device", -6, nan, true, "linux", "amd64", -6, false},
		{"mister keeps config", -6, nan, false, "linux", "arm", -6, false},
		{"mister flag wins", -6, -12, false, "linux", "arm", -12, false},
	}
	for _, c := range cases {
		got, quiet := startVolume(c.cfg, c.flag, c.null, c.goos, c.goarch)
		if got != c.want || quiet != c.quiet {
			t.Errorf("%s: got %v,%v want %v,%v", c.name, got, quiet, c.want, c.quiet)
		}
	}
}

func TestConfigMessageNamesNoMissingFile(t *testing.T) {
	msg := configMessage("/x/config.toml", config.ErrNotFound)
	if strings.Contains(msg, "config.example.toml") || !strings.Contains(msg, "/x/config.toml") {
		t.Fatalf("message %q", msg)
	}
}

func nullDevice(t *testing.T) {
	t.Helper()
	old := openDevice
	openDevice = func(audio.DeviceOptions) (audio.Output, error) {
		return audio.OpenDevice(audio.DeviceOptions{Null: true})
	}
	t.Cleanup(func() { openDevice = old })
}

func writeConfig(t *testing.T, url string) (dir, cfg string) {
	t.Helper()
	dir = t.TempDir()
	cfg = filepath.Join(dir, "config.toml")
	body := "[[server]]\nname = \"x\"\nurl = \"" + url + "\"\nusername = \"u\"\npassword = \"p\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

func pingServer(gate <-chan struct{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`)
	}))
}

// The player and art cache are built after a successful connect and torn
// down on exit (run under -race: construction happens off the UI goroutine).
func TestConnectBuildsSessionAndExitsCleanly(t *testing.T) {
	nullDevice(t)
	srv := pingServer(nil)
	defer srv.Close()
	dir, cfg := writeConfig(t, srv.URL)
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "art")); err != nil {
		t.Fatalf("art cache not created after connect: %v", err)
	}
}

// Exiting while the connect is still in flight must not build (or leak) a
// player afterwards.
func TestExitDuringConnectBuildsNothing(t *testing.T) {
	nullDevice(t)
	gate := make(chan struct{})
	srv := pingServer(gate)
	defer srv.Close()
	dir, cfg := writeConfig(t, srv.URL)
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	close(gate)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "cache", "art")); err == nil {
		t.Fatal("art cache built after the app exited")
	}
}
