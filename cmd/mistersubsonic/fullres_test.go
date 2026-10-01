package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
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
	exitRestore.graphics = true // the run entered graphics mode, as the size change needs
	exitRestore.mu.Unlock()
	t.Cleanup(func() {
		exitRestore.mu.Lock()
		exitRestore.done = false
		exitRestore.graphics = false
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

// failingConsole makes graphicsMode fail, as when the console can't be opened.
func failingConsole(t *testing.T, texts *int) {
	t.Helper()
	og, or := graphicsMode, restoreText
	t.Cleanup(func() { graphicsMode, restoreText = og, or })
	graphicsMode = func() (*platform.Console, error) { return nil, errors.New("no console") }
	restoreText = func() error { *texts++; return nil }
}

// A leftover state whose "to" is the current size would resize in text mode
// if the run never entered graphics mode: no size command then.
func noSizeCommandFixture(t *testing.T) platform.FBControl {
	t.Helper()
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 200 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
	answeringMenu(t, c)
	os.WriteFile(filepath.Join(dir, "width"), []byte("1920\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "height"), []byte("1200\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	t.Cleanup(func() { fbControl = old })
	return c
}

func noCommand(t *testing.T, c platform.FBControl, what string) {
	t.Helper()
	if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
		t.Fatalf("%s sent a size command without graphics mode: %q", what, b)
	}
}

func TestExitRestoreWithoutGraphicsModeSendsNoSizeCommand(t *testing.T) {
	resetExitRestore(t)
	exitRestore.mu.Lock()
	exitRestore.graphics = false
	exitRestore.mu.Unlock()
	c := noSizeCommandFixture(t)
	texts := 0
	failingConsole(t, &texts)
	restoreDisplay(nil)
	noCommand(t, c, "restoreDisplay")
}

func TestForcedExitWithoutGraphicsModeSendsNoSizeCommand(t *testing.T) {
	resetExitRestore(t)
	exitRestore.mu.Lock()
	exitRestore.graphics = false
	exitRestore.mu.Unlock()
	c := noSizeCommandFixture(t)
	texts := 0
	failingConsole(t, &texts)
	restoreOnForcedExit()
	noCommand(t, c, "restoreOnForcedExit")
	if texts != 1 {
		t.Fatalf("restoreText ran %d times, want 1", texts)
	}
}

func TestRestoreConsoleWithoutGraphicsModeSendsNoSizeCommand(t *testing.T) {
	c := noSizeCommandFixture(t)
	texts := 0
	failingConsole(t, &texts)
	if err := restoreConsole(); err != nil {
		t.Fatal(err)
	}
	noCommand(t, c, "restoreConsole")
	if texts != 1 {
		t.Fatalf("restoreText ran %d times, want 1", texts)
	}
}

// A restore that hangs while holding the guard must not hold up the forced
// exit: past the wait it logs and skips the display and console restore (the
// launcher's -restore-console does it, in graphics mode).
func TestForceExitGivesUpOnAHungRestore(t *testing.T) {
	resetExitRestore(t)
	old := forcedExitWait
	forcedExitWait = 100 * time.Millisecond
	defer func() { forcedExitWait = old }()
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 200 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
	oldC := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = oldC }()
	texts := 0
	fakeConsole(t, nil, func() { texts++ })
	exitRestore.mu.Lock() // the hung restore
	done := make(chan struct{})
	go func() { restoreOnForcedExit(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		exitRestore.mu.Unlock()
		t.Fatal("the forced exit is stuck behind the guard")
	}
	exitRestore.mu.Unlock()
	if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
		t.Fatalf("size command sent without the guard: %q", b)
	}
	if texts != 0 {
		t.Fatalf("restoreText ran %d times", texts)
	}
}

// fakeScreen is a framebuffer of the given size.
type fakeScreen struct {
	gfx.Display
	w, h   int
	closed bool
}

func (f *fakeScreen) Size() (int, int) { return f.w, f.h }
func (f *fakeScreen) Close() error     { f.closed = true; return nil }

// scriptedFB makes openFBDev hand out the steps in order: a size, or an
// error (w == 0).
type fbStep struct {
	w, h int
	err  error
}

func scriptedFB(t *testing.T, steps ...fbStep) (opened *[]*fakeScreen) {
	t.Helper()
	old := openFBDev
	t.Cleanup(func() { openFBDev = old })
	opened = new([]*fakeScreen)
	openFBDev = func(string) (gfx.Display, error) {
		if len(*opened) >= len(steps) {
			t.Fatal("the framebuffer was opened more often than scripted")
		}
		st := steps[len(*opened)]
		if st.err != nil {
			*opened = append(*opened, nil)
			return nil, st.err
		}
		fs := &fakeScreen{w: st.w, h: st.h}
		*opened = append(*opened, fs)
		return fs, nil
	}
	return opened
}

// openFBFixture: a halved 960x600 HDMI framebuffer, MiSTer.ini asking for
// 1920x1200, and a menu that answers.
func openFBFixture(t *testing.T) (dataDir string, c platform.FBControl) {
	t.Helper()
	root := t.TempDir()
	dataDir = filepath.Join(root, "mistersubsonic")
	os.WriteFile(filepath.Join(root, "MiSTer.ini"), []byte("[MiSTer]\nvideo_mode=1920,1200,60\n"), 0o644)
	c = platform.FBControl{Cmd: filepath.Join(root, "cmd"), Sys: root, State: filepath.Join(root, "state"), Wait: 500 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(filepath.Join(root, "width"), []byte("960\n"), 0o644)
	os.WriteFile(filepath.Join(root, "height"), []byte("600\n"), 0o644)
	answeringMenu(t, c)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	t.Cleanup(func() { fbControl = old })
	return dataDir, c
}

func TestOpenFBSwitchesToFullResolution(t *testing.T) {
	dir, c := openFBFixture(t)
	opened := scriptedFB(t, fbStep{w: 960, h: 600}, fbStep{w: 1920, h: 1200})
	fb, keep, err := openFB("/dev/fb0", "auto", dir, true)
	if err != nil || keep == nil {
		t.Fatalf("%v %v", keep, err)
	}
	if w, h := fb.Size(); w != 1920 || h != 1200 {
		t.Fatalf("size %dx%d", w, h)
	}
	if !(*opened)[0].closed || (*opened)[1].closed {
		t.Fatal("the halved framebuffer must be closed, the new one kept")
	}
	if got := strings.TrimSpace(readFile(c.Cmd)); got != "fb_cmd1 8888 1 1920 1200" {
		t.Fatalf("commands %q", got)
	}
}

func TestOpenFBWithoutFullResolutionKeepsTheFramebuffer(t *testing.T) {
	dir, c := openFBFixture(t)
	scriptedFB(t, fbStep{w: 960, h: 600})
	fb, keep, err := openFB("/dev/fb0", "auto", dir, false)
	if err != nil || keep != nil || fb == nil {
		t.Fatalf("%v %v %v", fb, keep, err)
	}
	if readFile(c.Cmd) != "" {
		t.Fatal("sent a command")
	}
}

// Fallback 1: the size request fails; the old framebuffer is opened again.
func TestOpenFBFallsBackWhenTheSwitchFails(t *testing.T) {
	dir, c := openFBFixture(t)
	c.Cmd = filepath.Join(c.Sys, "missing", "cmd") // the request can't be sent
	fbControl = func() platform.FBControl { return c }
	opened := scriptedFB(t, fbStep{w: 960, h: 600}, fbStep{w: 960, h: 600})
	fb, keep, err := openFB("/dev/fb0", "auto", dir, true)
	if err != nil || keep != nil {
		t.Fatalf("%v %v", keep, err)
	}
	if w, h := fb.Size(); w != 960 || h != 600 || len(*opened) != 2 {
		t.Fatalf("size %dx%d after %d opens", w, h, len(*opened))
	}
}

// Fallback 2: the new framebuffer has another size than asked for; the old
// size is requested again and the framebuffer reopened.
func TestOpenFBFallsBackWhenTheSizeIsNotTaken(t *testing.T) {
	dir, _ := openFBFixture(t)
	opened := scriptedFB(t, fbStep{w: 960, h: 600}, fbStep{w: 1280, h: 720}, fbStep{w: 960, h: 600})
	fb, keep, err := openFB("/dev/fb0", "auto", dir, true)
	if err != nil || keep != nil {
		t.Fatalf("%v %v", keep, err)
	}
	if w, h := fb.Size(); w != 960 || h != 600 || len(*opened) != 3 {
		t.Fatalf("size %dx%d after %d opens", w, h, len(*opened))
	}
	if !(*opened)[1].closed {
		t.Fatal("the wrong-sized framebuffer was left open")
	}
}

// Fallback 3: reopening after the switch fails; the old size is requested
// again and the framebuffer reopened (its error is the result's).
func TestOpenFBFallsBackWhenTheReopenFails(t *testing.T) {
	dir, _ := openFBFixture(t)
	boom := errors.New("boom")
	opened := scriptedFB(t, fbStep{w: 960, h: 600}, fbStep{err: boom}, fbStep{err: boom})
	fb, keep, err := openFB("/dev/fb0", "auto", dir, true)
	if !errors.Is(err, boom) || keep != nil || fb != nil {
		t.Fatalf("%v %v %v", fb, keep, err)
	}
	if len(*opened) != 3 {
		t.Fatalf("%d opens", len(*opened))
	}
}

func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }
