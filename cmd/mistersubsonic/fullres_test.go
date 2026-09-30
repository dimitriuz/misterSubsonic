package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/platform"
)

func TestFullSize(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "mistersubsonic")
	os.WriteFile(filepath.Join(root, "MiSTer.ini"), []byte("[MiSTer]\nvideo_mode=1920,1200,60\n"), 0o644)
	half := platform.Size{W: 960, H: 600}
	if got, ok := fullSize(half, "auto", data, true); !ok || got != (platform.Size{W: 1920, H: 1200}) {
		t.Fatalf("halved HDMI: %v %v", got, ok)
	}
	if _, ok := fullSize(half, "auto", data, false); ok {
		t.Error("switched with full_resolution = false")
	}
	if _, ok := fullSize(half, "crt", data, true); ok {
		t.Error("switched for a CRT layout")
	}
	if _, ok := fullSize(platform.Size{W: 640, H: 240}, "auto", data, true); ok {
		t.Error("switched a 240-line framebuffer")
	}
	if _, ok := fullSize(half, "auto", filepath.Join(t.TempDir(), "x"), true); ok {
		t.Error("switched without a MiSTer.ini")
	}
}

// -restore-console (run by the launcher after every exit, crashes too)
// puts back the framebuffer size the app saved.
func TestRestoreConsoleRestoresTheFramebuffer(t *testing.T) {
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	restoreConsole() // the console part fails off the MiSTer; the framebuffer part must still run
	if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", b)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("the saved size is still there")
	}
}
