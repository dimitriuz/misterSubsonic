package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOutputMode(t *testing.T) {
	for _, c := range []struct {
		name, ini string
		want      Size
		ok        bool
	}{
		{"custom mode", "[MiSTer]\nvideo_mode=1920,1200,60\n", Size{1920, 1200}, true},
		{"spaces and a comment", "[MiSTer]\n video_mode = 1920, 1200, 60   ; my TV\n", Size{1920, 1200}, true},
		{"modeline", "[MiSTer]\nvideo_mode=1920,48,32,80,1200,3,6,26,154000\n", Size{1920, 1200}, true},
		{"menu section wins", "[MiSTer]\nvideo_mode=8\n[Menu]\nvideo_mode=640,16,64,80,240,1,3,14,12380\n", Size{640, 240}, true},
		{"sections any case", "[mister]\nvideo_mode=2560,1440,60\n", Size{2560, 1440}, true},
		{"another core's section is ignored", "[MiSTer]\nvideo_mode=1920,1200,60\n[SNES]\nvideo_mode=1280,720,60\n", Size{1920, 1200}, true},
		{"a preset number", "[MiSTer]\nvideo_mode=8\n", Size{}, false},
		{"no video_mode", "[MiSTer]\nvga_scaler=1\n", Size{}, false},
		{"garbage", "[MiSTer]\nvideo_mode=1920,x,60\n", Size{}, false},
		{"commented out", "[MiSTer]\n;video_mode=1920,1200,60\n", Size{}, false},
	} {
		p := filepath.Join(t.TempDir(), "MiSTer.ini")
		os.WriteFile(p, []byte(c.ini), 0o644)
		got, ok := OutputMode(p)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
	if _, ok := OutputMode(filepath.Join(t.TempDir(), "missing.ini")); ok {
		t.Error("a missing MiSTer.ini gave a mode")
	}
}

func TestShouldSwitch(t *testing.T) {
	for _, c := range []struct {
		fb, out Size
		want    bool
	}{
		{Size{960, 600}, Size{1920, 1200}, true},   // the user's TV
		{Size{960, 720}, Size{1920, 1440}, false},  // too big to draw
		{Size{1280, 720}, Size{2560, 1440}, false}, // too big
		{Size{1280, 720}, Size{1280, 720}, false},  // not halved
		{Size{960, 600}, Size{1920, 1080}, false},  // a mismatch (another ini?)
		{Size{0, 0}, Size{1920, 1200}, false},
	} {
		if got := ShouldSwitch(c.fb, c.out); got != c.want {
			t.Errorf("ShouldSwitch(%v, %v) = %v", c.fb, c.out, got)
		}
	}
}

// fakeMenu stands in for Main_MiSTer: a command file, and a res_count that
// goes up when a command arrives (unless it is deaf).
func fakeMenu(t *testing.T, deaf bool) FBControl {
	t.Helper()
	dir := t.TempDir()
	c := FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 300 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "res_count"), []byte("8\n"), 0o644)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		seen := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
			}
			b, _ := os.ReadFile(c.Cmd)
			if n := strings.Count(string(b), "\n"); n > seen && !deaf {
				seen = n
				os.WriteFile(filepath.Join(dir, "res_count"), []byte(strconv.Itoa(8+n)+"\n"), 0o644)
			}
		}
	}()
	return c
}

func commands(t *testing.T, c FBControl) []string {
	b, _ := os.ReadFile(c.Cmd)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Switching saves the old size, asks for the new one and waits for the
// menu; restoring asks for the old size again and forgets it.
func TestSwitchAndRestore(t *testing.T) {
	c := fakeMenu(t, false)
	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.State); string(b) != "960 600\n" {
		t.Fatalf("state %q", b)
	}
	if err := c.Restore(); err != nil {
		t.Fatal(err)
	}
	if got := commands(t, c); len(got) != 2 || got[0] != "fb_cmd1 8888 1 1920 1200" || got[1] != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", got)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("the state file is still there")
	}
	if err := c.Restore(); err != nil { // nothing saved: nothing to do
		t.Fatal(err)
	}
	if got := commands(t, c); len(got) != 2 {
		t.Fatalf("a restore without state sent %q", got)
	}
}

// A menu that never confirms: Switch gives up, asks for the old size again
// and leaves nothing to restore.
func TestSwitchThatIsNeverConfirmed(t *testing.T) {
	c := fakeMenu(t, true)
	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err == nil {
		t.Fatal("no error")
	}
	if got := commands(t, c); len(got) != 2 || got[1] != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", got)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("a failed switch left a state file")
	}
}

// When the old size can't be had back either, the error says both.
func TestSwitchReportsAFailedWayBack(t *testing.T) {
	c := fakeMenu(t, true)
	err := c.Switch(Size{960, 600}, Size{1920, 1200})
	if err == nil || !strings.Contains(err.Error(), "old size back") {
		t.Fatalf("error %v doesn't mention the failed way back", err)
	}
}

func TestRestoreWithBadState(t *testing.T) {
	c := fakeMenu(t, false)
	os.WriteFile(c.State, []byte("junk"), 0o644)
	if err := c.Restore(); err == nil {
		t.Fatal("bad state accepted")
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("bad state kept")
	}
}

func TestSaved(t *testing.T) {
	c := fakeMenu(t, false)
	if c.Saved() {
		t.Fatal("saved before any switch")
	}
	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err != nil {
		t.Fatal(err)
	}
	if !c.Saved() {
		t.Fatal("not saved after a switch")
	}
	if err := c.Restore(); err != nil {
		t.Fatal(err)
	}
	if c.Saved() {
		t.Fatal("still saved after a restore")
	}
}
