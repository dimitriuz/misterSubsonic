package ui

import (
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"mistersubsonic/internal/gfx"
)

// screenshot saves the frame on screen as a PNG in Options.ScreenshotDir,
// named by the time (20260930_101500.png). The frame is copied here and
// encoded off the UI goroutine; a toast says how it went. One runs at a
// time: a press during a save only toasts.
func (a *App) screenshot() {
	dir := a.o.ScreenshotDir
	if dir == "" {
		return
	}
	if a.shooting {
		a.Toast("Still saving the last screenshot")
		return
	}
	a.shooting = true
	snap := gfx.NewCanvas(a.canvas.W, a.canvas.H)
	copy(snap.Pix, a.canvas.Pix)
	base := a.o.Now().Format("20060102_150405")
	go func() {
		path, err := saveScreenshot(dir, base, snap)
		a.Post(func() {
			a.shooting = false
			if err != nil {
				log.Printf("screenshot: %v", err)
				a.Toast("Screenshot failed")
				return
			}
			log.Printf("screenshot: saved %s", path)
			if !a.saver { // success is silent over the screensaver: a toast would wake it
				a.Toast("Screenshot saved")
			}
		})
	}()
}

// saveScreenshot writes c to dir/base.png (base-2.png, … if taken), through
// a .tmp file renamed into place, so a reader that looks for the newest
// .png never sees a half-written one.
func saveScreenshot(dir, base string, c *gfx.Canvas) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, base+".png")
	for n := 2; ; n++ {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if n > 99 {
			return "", fmt.Errorf("%s: too many screenshots this second", base)
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, n))
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed} // the MiSTer's CPU is slow
	err = enc.Encode(f, c.ToRGBA())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}
