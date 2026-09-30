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
	fakeConsole(t, nil, nil)
	restoreConsole()
	if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", b)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("the saved size is still there")
	}
}

// fakeConsole replaces the console calls for the test; g and r run when the
// app enters graphics mode and returns to text mode.
func fakeConsole(t *testing.T, g, r func()) {
	t.Helper()
	og, or := graphicsMode, restoreText
	t.Cleanup(func() { graphicsMode, restoreText = og, or })
	graphicsMode = func() (*platform.Console, error) {
		if g != nil {
			g()
		}
		return nil, nil
	}
	restoreText = func() error {
		if r != nil {
			r()
		}
		return nil
	}
}

// The console must be in graphics mode while the size changes (in text mode
// fbcon redraws its text into the new framebuffer and crashes the kernel).
func TestRestoreConsoleKeepsGraphicsModeAroundTheSizeChange(t *testing.T) {
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	var steps []string
	fakeConsole(t, func() {
		if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
			t.Errorf("graphics mode entered after the size command: %q", b)
		}
		steps = append(steps, "graphics")
	}, func() {
		if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
			t.Errorf("text mode before the size was restored: %q", b)
		}
		steps = append(steps, "text")
	})
	if err := restoreConsole(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "graphics,text" {
		t.Fatalf("steps %v", steps)
	}
}

func TestRestoreConsoleWithoutSavedSizeOnlyRestoresText(t *testing.T) {
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	var steps []string
	fakeConsole(t, func() { steps = append(steps, "graphics") }, func() { steps = append(steps, "text") })
	restoreConsole()
	if strings.Join(steps, ",") != "text" {
		t.Fatalf("steps %v", steps)
	}
}
