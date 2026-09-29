//go:build linux

// Package platform holds the MiSTer specifics the app handles itself: while
// it draws on the framebuffer, the Linux console is put in graphics mode so
// its text and cursor stay off the screen, and put back on exit (spec §9).
// Pausing BGM and SAM and the single-instance lock are the launcher's.
package platform

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// linux/kd.h
const (
	kdSetMode  = 0x4B3A
	kdGetMode  = 0x4B3B
	kdText     = 0
	kdGraphics = 1
)

// Consoles are tried in order: the active virtual terminal (the Scripts
// menu runs scripts on tty2 and switches to it), then the app's own
// terminal.
var consoles = []string{"/dev/tty0", "/dev/tty"}

// getMode and setMode are the console ioctls; tests replace them.
var (
	getMode = func(fd uintptr) (uint32, error) {
		var mode uint32
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, kdGetMode, uintptr(unsafe.Pointer(&mode))); e != 0 {
			return 0, e
		}
		return mode, nil
	}
	setMode = func(fd uintptr, mode uint32) error {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, kdSetMode, uintptr(mode)); e != 0 {
			return e
		}
		return nil
	}
)

// Console is a console switched to graphics mode.
type Console struct {
	mu   sync.Mutex
	f    *os.File
	prev uint32 // the mode it was in
}

// GraphicsMode switches the console to KD_GRAPHICS. It fails where there
// is no virtual terminal (over ssh, in a desktop terminal).
func GraphicsMode() (*Console, error) {
	var errs []error
	for _, path := range consoles {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		mode, err := getMode(f.Fd())
		if err != nil {
			f.Close()
			errs = append(errs, fmt.Errorf("%s: KDGETMODE: %w", path, err))
			continue
		}
		if err := setMode(f.Fd(), kdGraphics); err != nil {
			f.Close()
			errs = append(errs, fmt.Errorf("%s: KDSETMODE: %w", path, err))
			continue
		}
		return &Console{f: f, prev: mode}, nil
	}
	return nil, fmt.Errorf("platform: no console for graphics mode: %w", errors.Join(errs...))
}

// Restore puts the console back in the mode it had. It is safe to call
// more than once and on a nil Console.
func (c *Console) Restore() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return nil
	}
	err := setMode(c.f.Fd(), c.prev)
	c.f.Close()
	c.f = nil
	return err
}

// RestoreText puts the console in text mode whatever a crashed run left
// behind. The launcher runs it (mistersubsonic -restore-console) after
// every exit.
func RestoreText() error {
	var errs []error
	for _, path := range consoles {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = setMode(f.Fd(), kdText)
		f.Close()
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: KDSETMODE: %w", path, err))
	}
	return fmt.Errorf("platform: console not restored: %w", errors.Join(errs...))
}
