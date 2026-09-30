//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The real command pipe is a FIFO: with no reader, opening it for writing
// must fail at once, not block the app (or the launcher's restore) forever.
func TestNoReaderOnCommandPipe(t *testing.T) {
	dir := t.TempDir()
	c := FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 300 * time.Millisecond}
	if err := syscall.Mkfifo(c.Cmd, 0o600); err != nil {
		t.Skip(err)
	}
	within := func(name string, f func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: no error", name)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s blocked on a pipe nobody reads", name)
		}
	}
	within("Switch", func() error { return c.Switch(Size{960, 600}, Size{1920, 1200}) })
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Error("a failed switch left a state file")
	}
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	within("Restore", c.Restore)
}
