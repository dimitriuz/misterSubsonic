package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/platform"
	"mistersubsonic/internal/ui"
)

// fbControl is replaceable so tests never touch the real menu.
var fbControl = platform.DefaultFBControl

// The console calls are replaceable for the same reason.
var (
	graphicsMode = platform.GraphicsMode
	restoreText  = platform.RestoreText
)

// fullSize is the framebuffer size to ask for, when the HDMI framebuffer fb
// is the menu's halved output (spec Plan 4b §2): the output size from
// MiSTer.ini, which sits next to the app's folder (/media/fat).
func fullSize(fb platform.Size, profileName, dataDir string, enabled bool) (platform.Size, bool) {
	if !enabled || ui.PickProfile(fb.W, fb.H, profileName).Name != "hdmi" {
		return platform.Size{}, false
	}
	out, ok := platform.OutputMode(filepath.Join(filepath.Dir(dataDir), "MiSTer.ini"))
	if !ok || !platform.ShouldSwitch(fb, out) {
		return platform.Size{}, false
	}
	return out, true
}

// remappable is the part of the framebuffer the keeper works on.
type remappable interface {
	Release() error
	Reopen() error
}

// keeper notices that the menu reset the framebuffer (a display hotplug
// makes Main_MiSTer re-initialise video and go back to the half size) and
// asks for the full size again, the way openFB did. The console is still in
// graphics mode, so the size change is safe. The size to restore at exit
// stays the one openFB saved.
type keeper struct {
	ctl    platform.FBControl
	fb     remappable
	want   platform.Size
	count  string
	settle time.Duration // how long the driver may take to report the new size
}

func newKeeper(ctl platform.FBControl, fb remappable, want platform.Size) *keeper {
	return &keeper{ctl: ctl, fb: fb, want: want, count: ctl.ResCount(), settle: time.Second}
}

// check is the UI's WatchDisplay hook. repaint: the framebuffer was
// reconfigured (and is at the wanted size again), so the whole frame must
// be drawn. An error means full resolution couldn't be restored.
func (k *keeper) check() (repaint bool, err error) {
	count := k.ctl.ResCount()
	if count == k.count {
		return false, nil
	}
	k.count = count
	cur, ok := k.ctl.Current()
	if ok && cur == k.want {
		return true, nil
	}
	fail := func(err error) (bool, error) {
		k.count = k.ctl.ResCount()
		return false, fmt.Errorf("display: the framebuffer was reset to %v and full resolution couldn't be restored: %w", cur, err)
	}
	if err := k.fb.Release(); err != nil {
		log.Printf("display: releasing the framebuffer: %v", err)
	}
	if err := k.ctl.Resize(k.want); err != nil {
		if rerr := k.fb.Reopen(); rerr != nil {
			err = fmt.Errorf("%w; the framebuffer is not usable: %v", err, rerr)
		}
		return fail(err)
	}
	for deadline := time.Now().Add(k.settle); ; time.Sleep(20 * time.Millisecond) {
		if s, ok := k.ctl.Current(); ok && s == k.want {
			break
		}
		if !time.Now().Before(deadline) {
			err := fmt.Errorf("the driver still reports another size than %v", k.want)
			if rerr := k.fb.Reopen(); rerr != nil {
				err = fmt.Errorf("%w; the framebuffer is not usable: %v", err, rerr)
			}
			return fail(err)
		}
	}
	if err := k.fb.Reopen(); err != nil {
		return fail(err)
	}
	k.count = k.ctl.ResCount()
	log.Printf("display: the framebuffer was reset to %v; full resolution %v restored", cur, k.want)
	return true, nil
}

// openFB opens the framebuffer, switched to the full output resolution when
// fullSize says so (then the keeper watches it; nil otherwise). Any failure
// falls back to the framebuffer as it was.
func openFB(path, profileName, dataDir string, enabled bool) (*gfx.FB, *keeper, error) {
	fb, err := gfx.OpenFB(path)
	if err != nil {
		return nil, nil, err
	}
	w, h := fb.Size()
	from := platform.Size{W: w, H: h}
	to, ok := fullSize(from, profileName, dataDir, enabled)
	if !ok {
		return fb, nil, nil
	}
	fb.Close()
	ctl := fbControl()
	if err := ctl.Switch(from, to); err != nil {
		log.Printf("display: full resolution: %v", err)
		fb, err := gfx.OpenFB(path)
		return fb, nil, err
	}
	if fb, err = gfx.OpenFB(path); err == nil {
		if w, h := fb.Size(); w == to.W && h == to.H {
			log.Printf("display: framebuffer %v, full resolution (was %v)", to, from)
			return fb, newKeeper(ctl, fb, to), nil
		}
		log.Printf("display: asked for %v, got %dx%d; keeping %v", to, w, h, from)
		fb.Close()
	}
	if err := ctl.Restore(); err != nil {
		log.Printf("display: %v", err)
	}
	fb, err = gfx.OpenFB(path)
	return fb, nil, err
}

// restoreConsole is -restore-console, which the launcher runs after every
// exit: the framebuffer size the app switched from (after a crash too),
// then the console's text mode. With a size to restore the console goes to
// graphics mode first and stays there until the size is back: in text mode
// fbcon redraws its text into the new framebuffer and crashes the kernel.
func restoreConsole() error {
	ctl := fbControl()
	if ctl.Saved() {
		// The console is not "restored" to the mode it had: after a crash that
		// is graphics mode. restoreText sets text mode, and the process ends.
		if _, err := graphicsMode(); err != nil {
			log.Printf("console: %v", err)
		}
		if err := ctl.Restore(); err != nil {
			log.Printf("display: %v", err)
		}
	}
	return restoreText()
}

// allowFullRes: changing the framebuffer size while the console is in text
// mode crashes the kernel (fbcon), so the switch needs graphics mode entered
// (con is nil when graphicsMode failed).
func allowFullRes(enabled bool, con *platform.Console) bool {
	if enabled && con == nil {
		log.Printf("display: full resolution skipped, the console could not be put in graphics mode")
		return false
	}
	return enabled
}
