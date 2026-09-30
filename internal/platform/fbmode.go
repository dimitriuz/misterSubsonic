package platform

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Full-resolution framebuffer (Plan 4b). Above 1920x1080 the MiSTer menu
// gives Linux a framebuffer of half the output size (1920x1200 → 960x600),
// which the scaler then doubles: text goes soft. The menu accepts a request
// for another size on its command pipe (what `vmode -r` does), so the app
// asks for the output size and puts the old size back when it exits.
// Nothing here writes to the SD card: MiSTer.ini is only read, the command
// pipe is a FIFO and the state file lives in /tmp (RAM).

// Size is a framebuffer or video mode size in pixels.
type Size struct{ W, H int }

func (s Size) String() string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// MaxFullFB is the largest framebuffer the app asks for (proven on the
// device; larger ones cost too much to draw).
var MaxFullFB = Size{1920, 1200}

// OutputMode reads the menu's video mode from MiSTer.ini: video_mode in
// [Menu] if set, otherwise in [MiSTer]. It knows custom modes (W,H,refresh)
// and full modelines (hact,hfp,hs,hbp,vact,…). A single preset number is
// not decoded: presets are 1080p or smaller (never halved) or larger than
// MaxFullFB, so none can lead to a switch.
func OutputMode(iniPath string) (Size, bool) {
	f, err := os.Open(iniPath)
	if err != nil {
		return Size{}, false
	}
	defer f.Close()
	var section, global, menu string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexAny(line, ";#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.ToLower(strings.TrimSpace(k)) != "video_mode" {
			continue
		}
		switch section {
		case "mister":
			global = strings.TrimSpace(v)
		case "menu":
			menu = strings.TrimSpace(v)
		}
	}
	if menu != "" {
		return parseVideoMode(menu)
	}
	return parseVideoMode(global)
}

func parseVideoMode(v string) (Size, bool) {
	parts := strings.Split(v, ",")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return Size{}, false
		}
		nums[i] = n
	}
	switch {
	case len(nums) == 3: // W,H,refresh
		return Size{nums[0], nums[1]}, true
	case len(nums) >= 9: // hact,hfp,hs,hbp,vact,vfp,vs,vbp,pclk[,…]
		return Size{nums[0], nums[4]}, true
	}
	return Size{}, false
}

// ShouldSwitch reports whether the framebuffer fb is the halved output out
// and out is small enough to draw at: then the app asks for out.
func ShouldSwitch(fb, out Size) bool {
	return fb.W > 0 && fb.H > 0 && out.W == 2*fb.W && out.H == 2*fb.H &&
		out.W*out.H <= MaxFullFB.W*MaxFullFB.H
}

// FBControl changes the framebuffer size through the menu.
type FBControl struct {
	Cmd   string // the menu's command pipe
	Sys   string // the MiSTer_fb module's parameters (res_count counts mode changes)
	State string // where the size to restore is kept
	Wait  time.Duration
}

// DefaultFBControl is the MiSTer's.
func DefaultFBControl() FBControl {
	return FBControl{Cmd: "/dev/MiSTer_cmd", Sys: "/sys/module/MiSTer_fb/parameters",
		State: "/tmp/mistersubsonic.fb", Wait: time.Second}
}

// Switch saves from as the size to restore, then asks for to and waits for
// the menu to apply it. On failure it asks for from again.
func (c FBControl) Switch(from, to Size) error {
	if err := os.WriteFile(c.State, []byte(fmt.Sprintf("%d %d\n", from.W, from.H)), 0o644); err != nil {
		return fmt.Errorf("platform: save the framebuffer size: %w", err)
	}
	if err := c.request(to); err != nil {
		if back := c.request(from); back != nil {
			err = errors.Join(err, fmt.Errorf("platform: asking for the old size back: %w", back))
		}
		os.Remove(c.State)
		return err
	}
	return nil
}

// Saved reports whether Switch left a size to restore.
func (c FBControl) Saved() bool {
	_, err := os.Stat(c.State)
	return err == nil
}

// Restore puts back the size Switch saved, if any, and forgets it. Without
// a saved size it does nothing.
func (c FBControl) Restore() error {
	b, err := os.ReadFile(c.State)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("platform: %w", err)
	}
	var s Size
	if _, err := fmt.Sscanf(string(b), "%d %d", &s.W, &s.H); err != nil || s.W <= 0 || s.H <= 0 {
		os.Remove(c.State)
		return fmt.Errorf("platform: bad framebuffer state %q", strings.TrimSpace(string(b)))
	}
	err = c.request(s)
	os.Remove(c.State)
	return err
}

// request sends fb_cmd1 for s (32 bpp) and waits until res_count changes.
// If the driver already has size s, it is done.
func (c FBControl) request(s Size) error {
	if cur, ok := c.Current(); ok && cur == s {
		return nil // the menu doesn't count a change to the size it already has
	}
	count := c.ResCount()
	f, err := os.OpenFile(c.Cmd, os.O_WRONLY|os.O_APPEND|syscall.O_NONBLOCK, 0) // never block: with no reader a FIFO open fails with ENXIO
	if err != nil {
		return fmt.Errorf("platform: %w", err)
	}
	_, err = fmt.Fprintf(f, "fb_cmd1 8888 1 %d %d\n", s.W, s.H)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("platform: %s: %w", c.Cmd, err)
	}
	wait := c.Wait
	if wait <= 0 {
		wait = time.Second
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if c.ResCount() != count {
			return nil
		}
	}
	return fmt.Errorf("platform: the menu didn't switch the framebuffer to %v", s)
}

// ResCount is the driver's mode-change counter ("" if unreadable). It goes
// up whenever the menu reconfigures the framebuffer, e.g. after a display
// hotplug.
func (c FBControl) ResCount() string {
	b, _ := os.ReadFile(filepath.Join(c.Sys, "res_count"))
	return strings.TrimSpace(string(b))
}

// Current is the framebuffer size the driver has now, from its width and
// height parameters.
func (c FBControl) Current() (Size, bool) {
	rd := func(name string) int {
		b, _ := os.ReadFile(filepath.Join(c.Sys, name))
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || n <= 0 {
			return 0
		}
		return n
	}
	s := Size{rd("width"), rd("height")}
	return s, s.W > 0 && s.H > 0
}
