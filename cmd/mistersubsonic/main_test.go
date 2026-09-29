package main

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistersubsonic/internal/audio"
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
