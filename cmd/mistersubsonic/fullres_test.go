package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

type fakeFB struct {
	events  *[]string
	size    func() platform.Size // what the driver reports at Reopen time
	want    platform.Size
	blankAt time.Time
}

func (f *fakeFB) Blank()         { *f.events = append(*f.events, "blank"); f.blankAt = time.Now() }
func (f *fakeFB) Release() error { *f.events = append(*f.events, "release"); return nil }
func (f *fakeFB) Reopen() error {
	*f.events = append(*f.events, "reopen")
	if s := f.size(); s != f.want {
		return fmt.Errorf("gfx: framebuffer is %v, not %v", s, f.want)
	}
	return nil
}

// keeperMenu is a menu that starts at the full size (res_count 8); reset()
// simulates the hotplug reset, and a command sets the size asked for.
type keeperMenu struct {
	ctl    platform.FBControl
	events []string
	mu     sync.Mutex
	size   platform.Size
	fb     *fakeFB
}

var fullSz = platform.Size{W: 1920, H: 1200}

func newKeeperMenu(t *testing.T, deaf bool) (*keeperMenu, *keeper) {
	return newKeeperMenuIgnoring(t, deaf, 0)
}

// ignore: commands arriving within that time of the start are dropped, like
// the menu's while it re-initialises video.
func newKeeperMenuIgnoring(t *testing.T, deaf bool, ignore time.Duration) (*keeperMenu, *keeper) {
	t.Helper()
	dir := t.TempDir()
	m := &keeperMenu{size: fullSz}
	m.ctl = platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 300 * time.Millisecond}
	os.WriteFile(m.ctl.Cmd, nil, 0o644)
	m.publish(8)
	m.fb = &fakeFB{events: &m.events, size: m.get, want: fullSz}
	os.WriteFile(m.ctl.State, []byte("960 600\n"), 0o644) // as openFB's Switch left it
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	start := time.Now()
	go func() {
		seen := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
			}
			b, _ := os.ReadFile(m.ctl.Cmd)
			if n := strings.Count(string(b), "\n"); n > seen && !deaf {
				seen = n
				if time.Since(start) < ignore {
					continue
				}
				var w, h int
				txt := strings.TrimSpace(string(b))
				fmt.Sscanf(txt[strings.LastIndex(txt, "fb_cmd1"):], "fb_cmd1 8888 1 %d %d", &w, &h)
				m.set(platform.Size{W: w, H: h})
				m.publish(100 + n)
			}
		}
	}()
	k := newKeeper(m.ctl, m.fb, fullSz)
	k.settle = 100 * time.Millisecond
	k.quiet, k.maxQuiet = 60*time.Millisecond, 2*time.Second
	k.attempts, k.attemptWait, k.gap = 3, 150*time.Millisecond, 20*time.Millisecond
	k.poll = 5 * time.Millisecond
	return m, k
}

func (m *keeperMenu) get() platform.Size  { m.mu.Lock(); defer m.mu.Unlock(); return m.size }
func (m *keeperMenu) set(s platform.Size) { m.mu.Lock(); m.size = s; m.mu.Unlock() }

func (m *keeperMenu) publish(count int) {
	d, sz := m.ctl.Sys, m.get()
	os.WriteFile(filepath.Join(d, "width"), []byte(strconv.Itoa(sz.W)+"\n"), 0o644)
	os.WriteFile(filepath.Join(d, "height"), []byte(strconv.Itoa(sz.H)+"\n"), 0o644)
	os.WriteFile(filepath.Join(d, "res_count"), []byte(strconv.Itoa(count)+"\n"), 0o644)
}

func (m *keeperMenu) commands() string {
	b, _ := os.ReadFile(m.ctl.Cmd)
	return strings.TrimSpace(string(b))
}

func TestKeeperIgnoresAnUnchangedCount(t *testing.T) {
	m, k := newKeeperMenu(t, false)
	if rep, err := k.check(); rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if m.commands() != "" || len(m.events) != 0 {
		t.Fatal("did something")
	}
}

func TestKeeperRepaintsWhenTheSizeIsStillWanted(t *testing.T) {
	m, k := newKeeperMenu(t, true)
	m.publish(9) // reconfigured, same size
	if rep, err := k.check(); !rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if m.commands() != "" || len(m.events) != 0 {
		t.Fatalf("commands %q events %v", m.commands(), m.events)
	}
	if rep, _ := k.check(); rep {
		t.Fatal("repainted twice for one change")
	}
}

func TestKeeperRestoresFullResolutionAfterAReset(t *testing.T) {
	m, k := newKeeperMenu(t, false)
	m.set(platform.Size{W: 960, H: 600})
	m.publish(9)
	rep, err := k.check()
	if !rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if got := m.commands(); got != "fb_cmd1 8888 1 1920 1200" {
		t.Fatalf("commands %q", got)
	}
	if strings.Join(m.events, ",") != "blank,release,reopen" {
		t.Fatalf("events %v", m.events)
	}
	if b, _ := os.ReadFile(m.ctl.State); string(b) != "960 600\n" {
		t.Fatalf("the saved size is %q", b)
	}
	if rep, err := k.check(); rep || err != nil {
		t.Fatalf("our own change was taken for a new one: %v %v", rep, err)
	}
}

func TestKeeperReportsAMenuThatNeverConfirms(t *testing.T) {
	m, k := newKeeperMenu(t, true)
	m.set(platform.Size{W: 960, H: 600})
	m.publish(9)
	rep, err := k.check()
	if rep || err == nil {
		t.Fatalf("%v %v", rep, err)
	}
	if n := len(strings.Split(m.commands(), "\n")); n != 3 {
		t.Errorf("%d attempts, want 3", n)
	}
	for _, want := range []string{"reset to 960x600", "full resolution couldn't be restored", "not usable"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Join(m.events, ",") != "blank,release,reopen" {
		t.Fatalf("events %v", m.events)
	}
}

// The menu resets twice in a row and ignores requests while it is busy: the
// keeper blanks at once, waits for the counter to stay still, and retries.
func TestKeeperWaitsForTheMenuAndRetries(t *testing.T) {
	m, k := newKeeperMenuIgnoring(t, false, 250*time.Millisecond)
	m.set(platform.Size{W: 960, H: 600})
	m.publish(9)
	go func() { time.Sleep(100 * time.Millisecond); m.publish(10) }()
	start := time.Now()
	rep, err := k.check()
	if !rep || err != nil {
		t.Fatalf("%v %v", rep, err)
	}
	if m.fb.blankAt.Sub(start) > 50*time.Millisecond {
		t.Errorf("blanked after %v, not first", m.fb.blankAt.Sub(start))
	}
	if strings.Join(m.events, ",") != "blank,release,reopen" {
		t.Fatalf("events %v", m.events)
	}
	if n := len(strings.Split(m.commands(), "\n")); n < 2 {
		t.Errorf("%d command(s): no retry was needed?", n)
	}
	if time.Since(start) < 160*time.Millisecond {
		t.Errorf("recovered after %v, before the second reset had settled", time.Since(start))
	}
}
