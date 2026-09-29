//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// call is one console ioctl: KDGETMODE (mode unused) or KDSETMODE.
type call struct {
	req  uintptr
	mode uint32
}

// fakeConsoles points the console list at plain files and records the
// ioctls; found is what KDGETMODE reports, fail makes every ioctl fail.
func fakeConsoles(t *testing.T, n int, found uint32, fail bool) (paths []string, calls *[]call) {
	t.Helper()
	dir := t.TempDir()
	for i := range n {
		p := filepath.Join(dir, "tty"+string(rune('0'+i)))
		os.WriteFile(p, nil, 0o600)
		paths = append(paths, p)
	}
	calls = &[]call{}
	oldC, oldG, oldS := consoles, getMode, setMode
	consoles = paths
	getMode = func(uintptr) (uint32, error) {
		if fail {
			return 0, syscall.ENOTTY
		}
		*calls = append(*calls, call{kdGetMode, 0})
		return found, nil
	}
	setMode = func(_ uintptr, mode uint32) error {
		if fail {
			return syscall.ENOTTY
		}
		*calls = append(*calls, call{kdSetMode, mode})
		return nil
	}
	t.Cleanup(func() { consoles, getMode, setMode = oldC, oldG, oldS })
	return paths, calls
}

func TestGraphicsModeAndRestore(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdText, false)
	c, err := GraphicsMode()
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1] != (call{kdSetMode, kdGraphics}) {
		t.Fatalf("calls %v", *calls)
	}
	if err := c.Restore(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 || (*calls)[2] != (call{kdSetMode, kdText}) {
		t.Fatalf("restore calls %v", *calls)
	}
	c.Restore() // again: nothing more
	var nilc *Console
	nilc.Restore()
	if len(*calls) != 3 {
		t.Fatalf("a second Restore issued %v", (*calls)[3:])
	}
}

func TestRestoreGoesBackToTheModeItFound(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdGraphics, false) // someone else's graphics mode
	c, _ := GraphicsMode()
	c.Restore()
	if last := (*calls)[len(*calls)-1]; last != (call{kdSetMode, kdGraphics}) {
		t.Fatalf("restored to %v, want the mode found (graphics)", last)
	}
}

func TestGraphicsModeTriesTheNextConsole(t *testing.T) {
	paths, calls := fakeConsoles(t, 2, kdText, false)
	os.Remove(paths[0]) // no tty0 here
	c, err := GraphicsMode()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Restore()
	if c.f.Name() != paths[1] || len(*calls) != 2 {
		t.Fatalf("opened %s, calls %v", c.f.Name(), *calls)
	}
}

func TestGraphicsModeFailsWithoutAVirtualTerminal(t *testing.T) {
	fakeConsoles(t, 2, kdText, true)
	if c, err := GraphicsMode(); err == nil || c != nil {
		t.Fatalf("got %v, %v; want an error", c, err)
	}
	if err := RestoreText(); err == nil {
		t.Fatal("RestoreText succeeded with no virtual terminal")
	}
}

func TestRestoreTextSetsTextMode(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdGraphics, false)
	if err := RestoreText(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != (call{kdSetMode, kdText}) {
		t.Fatalf("calls %v", *calls)
	}
}
