package main

import (
	"log"
	"path/filepath"

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

// openFB opens the framebuffer, switched to the full output resolution when
// fullSize says so. Any failure falls back to the framebuffer as it was.
func openFB(path, profileName, dataDir string, enabled bool) (*gfx.FB, error) {
	fb, err := gfx.OpenFB(path)
	if err != nil {
		return nil, err
	}
	w, h := fb.Size()
	from := platform.Size{W: w, H: h}
	to, ok := fullSize(from, profileName, dataDir, enabled)
	if !ok {
		return fb, nil
	}
	fb.Close()
	ctl := fbControl()
	if err := ctl.Switch(from, to); err != nil {
		log.Printf("display: full resolution: %v", err)
		return gfx.OpenFB(path)
	}
	if fb, err = gfx.OpenFB(path); err == nil {
		if w, h := fb.Size(); w == to.W && h == to.H {
			log.Printf("display: framebuffer %v, full resolution (was %v)", to, from)
			return fb, nil
		}
		log.Printf("display: asked for %v, got %dx%d; keeping %v", to, w, h, from)
		fb.Close()
	}
	if err := ctl.Restore(); err != nil {
		log.Printf("display: %v", err)
	}
	return gfx.OpenFB(path)
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
