package main

import (
	"os"
	"path/filepath"
	"strconv"
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
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	answeringMenu(t, c)
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

// answeringMenu stands in for Main_MiSTer: res_count goes up when a command
// arrives in the command file.
func answeringMenu(t *testing.T, c platform.FBControl) {
	t.Helper()
	os.WriteFile(filepath.Join(c.Sys, "res_count"), []byte("1\n"), 0o644)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
			if b, _ := os.ReadFile(c.Cmd); len(b) > 0 {
				os.WriteFile(filepath.Join(c.Sys, "res_count"), []byte("2\n"), 0o644)
				return
			}
		}
	}()
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
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	answeringMenu(t, c)
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
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
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

// The shutdown deadline puts the size back too, then the text mode. The
// console is still in graphics mode then (the app entered it at start and
// only the deferred exit path leaves it), which the size change needs.
func TestForceExitRestoresTheSizeThenText(t *testing.T) {
	resetExitRestore(t)
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
	answeringMenu(t, c)
	os.WriteFile(filepath.Join(dir, "width"), []byte("1920\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "height"), []byte("1200\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	text := false
	fakeConsole(t, nil, func() {
		if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
			t.Errorf("text mode before the size was restored: %q", b)
		}
		text = true
	})
	restoreOnForcedExit()
	if !text {
		t.Fatal("text mode not restored")
	}
	if c.Saved() {
		t.Fatal("the saved size is still there")
	}
}

func resetExitRestore(t *testing.T) {
	t.Helper()
	exitRestore.mu.Lock()
	exitRestore.done = false
	exitRestore.mu.Unlock()
	t.Cleanup(func() {
		exitRestore.mu.Lock()
		exitRestore.done = false
		exitRestore.mu.Unlock()
	})
}

// After the exit path restored (even when the size request failed and the
// state stayed) the console is in text mode: a late forced exit must send no
// size command and not touch the console again.
func TestForceExitAfterTheExitRestoreDoesNothing(t *testing.T) {
	resetExitRestore(t)
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 200 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644) // no res_count: the request fails
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	texts := 0
	fakeConsole(t, nil, func() { texts++ })
	restoreDisplay(nil)
	if !c.Saved() {
		t.Fatal("the failed restore dropped the state")
	}
	restoreOnForcedExit()
	if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
		t.Fatalf("size command after the console left graphics mode: %q", b)
	}
	if texts != 0 {
		t.Fatalf("restoreText ran %d times", texts)
	}
}

// A forced exit that starts while the exit restore waits for the menu waits
// for it: one size command, and no console change of its own.
func TestForceExitWaitsForTheExitRestore(t *testing.T) {
	resetExitRestore(t)
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 2 * time.Second}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "width"), []byte("1920\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "height"), []byte("1200\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "res_count"), []byte("1\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	texts := 0
	fakeConsole(t, nil, func() { texts++ })
	go func() { // a slow menu: it answers 300 ms after the command
		for {
			if b, _ := os.ReadFile(c.Cmd); len(b) > 0 {
				time.Sleep(300 * time.Millisecond)
				os.WriteFile(filepath.Join(dir, "res_count"), []byte("2\n"), 0o644)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	deferred := make(chan struct{})
	go func() { restoreDisplay(nil); close(deferred) }()
	time.Sleep(100 * time.Millisecond) // the deferred restore has sent its command
	restoreOnForcedExit()
	select {
	case <-deferred:
	default:
		t.Fatal("the forced exit did not wait for the exit restore")
	}
	if b, _ := os.ReadFile(c.Cmd); strings.Count(string(b), "fb_cmd1") != 1 {
		t.Fatalf("commands: %q", b)
	}
	if texts != 0 {
		t.Fatalf("restoreText ran %d times", texts)
	}
}

func TestAllowFullRes(t *testing.T) {
	con := &platform.Console{}
	if !allowFullRes(true, con) {
		t.Error("full resolution refused with the console in graphics mode")
	}
	if allowFullRes(true, nil) {
		t.Error("full resolution allowed with the console still in text mode")
	}
	if allowFullRes(false, con) {
		t.Error("full resolution allowed although disabled")
	}
}

// keeperMenu stands in for the menu: res_count and the driver's size files.
type keeperMenu struct {
	ctl platform.FBControl
}

var fullSz = platform.Size{W: 1920, H: 1200}

func newKeeperMenu(t *testing.T) (*keeperMenu, *keeper) {
	t.Helper()
	dir := t.TempDir()
	m := &keeperMenu{ctl: platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 100 * time.Millisecond}}
	os.WriteFile(m.ctl.Cmd, nil, 0o644)
	os.WriteFile(m.ctl.State, []byte("960 600\n"), 0o644) // as openFB's Switch left it
	m.publish(8, fullSz)
	return m, newKeeper(m.ctl, fullSz)
}

func (m *keeperMenu) publish(count int, sz platform.Size) {
	d := m.ctl.Sys
	os.WriteFile(filepath.Join(d, "width"), []byte(strconv.Itoa(sz.W)+"\n"), 0o644)
	os.WriteFile(filepath.Join(d, "height"), []byte(strconv.Itoa(sz.H)+"\n"), 0o644)
	os.WriteFile(filepath.Join(d, "res_count"), []byte(strconv.Itoa(count)+"\n"), 0o644)
}

func (m *keeperMenu) commands() string {
	b, _ := os.ReadFile(m.ctl.Cmd)
	return strings.TrimSpace(string(b))
}

func TestKeeperIgnoresAnUnchangedCount(t *testing.T) {
	m, k := newKeeperMenu(t)
	if rep, err := k.check(); rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if m.commands() != "" {
		t.Fatal("sent a command")
	}
}

func TestKeeperRepaintsWhenTheSizeIsStillWanted(t *testing.T) {
	m, k := newKeeperMenu(t)
	m.publish(9, fullSz) // reconfigured, same size
	if rep, err := k.check(); !rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if m.commands() != "" {
		t.Fatalf("commands %q", m.commands())
	}
	if rep, _ := k.check(); rep {
		t.Fatal("repainted twice for one change")
	}
}

// After a display reconnect the menu shows its own background and ignores
// requests: the keeper reports it at once, without asking.
func TestKeeperFailsAtOnceAfterAReset(t *testing.T) {
	m, k := newKeeperMenu(t)
	m.publish(9, platform.Size{W: 960, H: 600})
	start := time.Now()
	rep, err := k.check()
	if rep || err == nil {
		t.Fatalf("%v %v", rep, err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Errorf("took %v", time.Since(start))
	}
	for _, want := range []string{"reconnected", "960x600", "start MiSTer Subsonic again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if m.commands() != "" {
		t.Fatalf("commands %q", m.commands())
	}
	if b, _ := os.ReadFile(m.ctl.State); string(b) != "960 600\n" {
		t.Fatalf("the saved size is %q", b)
	}
}
