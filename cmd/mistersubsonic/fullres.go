package main

import (
	"fmt"
	"log"
	"path/filepath"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/platform"
	"mistersubsonic/internal/ui"
)

// openFBDev opens the framebuffer device; tests replace it.
var openFBDev = func(path string) (gfx.Display, error) {
	fb, err := gfx.OpenFB(path)
	if err != nil {
		return nil, err // not a typed nil in the interface
	}
	return fb, nil
}

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

// keeper notices that the menu reset the framebuffer. On a display
// reconnect Main_MiSTer re-initialises video, resets the framebuffer to the
// half size and shows its own background instead of the Linux framebuffer;
// from then on it ignores framebuffer requests, so nothing the app draws is
// visible and the full size can't be asked for again. The app quits then, and
// the user starts it again.
type keeper struct {
	ctl   platform.FBControl
	want  platform.Size
	count string
}

func newKeeper(ctl platform.FBControl, want platform.Size) *keeper {
	return &keeper{ctl: ctl, want: want, count: ctl.ResCount()}
}

// check is the UI's WatchDisplay hook. repaint: the framebuffer was
// reconfigured but is still the wanted size, so the whole frame must be
// drawn. An error means the menu took the screen back.
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
	return false, fmt.Errorf("display: the display was reconnected and the MiSTer menu took the screen back (framebuffer reset to %v); quitting, start MiSTer Subsonic again", cur)
}

// openFB opens the framebuffer, switched to the full output resolution when
// fullSize says so (then the keeper watches it; nil otherwise). Any failure
// falls back to the framebuffer as it was.
func openFB(path, profileName, dataDir string, enabled bool) (gfx.Display, *keeper, error) {
	fb, err := openFBDev(path)
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
		fb, err := openFBDev(path)
		return fb, nil, err
	}
	if fb, err = openFBDev(path); err == nil {
		if w, h := fb.Size(); w == to.W && h == to.H {
			log.Printf("display: framebuffer %v, full resolution (was %v)", to, from)
			return fb, newKeeper(ctl, to), nil
		}
		log.Printf("display: asked for %v, got %dx%d; keeping %v", to, w, h, from)
		fb.Close()
	}
	// The framebuffer is at neither from nor to: ask for from, whatever it has.
	if err := ctl.RestoreAlways(); err != nil {
		log.Printf("display: %v", err)
	}
	fb, err = openFBDev(path)
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
			// A size change in text mode crashes the kernel: leave it.
			log.Printf("console: %v; the saved size is not restored", err)
			return restoreText()
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
