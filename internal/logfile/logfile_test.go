package logfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRotatesAtTheCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, err := Open(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, s := range []string{"aaaa\n", "bbbb\n", "cccc\n", "dddd\n", "eeee\n"} {
		if _, err := l.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	// 10 bytes each: a+b fill log.txt, c+d the next one, e starts a third;
	// only the newest two files are kept.
	if got := read(t, p); got != "eeee\n" {
		t.Errorf("log.txt = %q", got)
	}
	if got := read(t, p+".1"); got != "cccc\ndddd\n" {
		t.Errorf("log.txt.1 = %q", got)
	}
}

func TestAppendsToAnExistingLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	os.WriteFile(p, []byte("12345678\n"), 0o644)
	l, err := Open(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("x\n")) // 9 + 2 > 10: the old content moves to .1
	l.Close()
	if read(t, p) != "x\n" || read(t, p+".1") != "12345678\n" {
		t.Fatalf("log %q, .1 %q", read(t, p), read(t, p+".1"))
	}
}

func TestALongLineStillGoesIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 4)
	long := strings.Repeat("z", 20) + "\n"
	l.Write([]byte(long))
	l.Write([]byte(long))
	l.Close()
	if read(t, p) != long || read(t, p+".1") != long {
		t.Fatal("a line longer than the cap was split or dropped")
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "log.txt"), 100)
	l.Close()
	if _, err := l.Write([]byte("x")); err != os.ErrClosed {
		t.Fatalf("err %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
}

func TestConcurrentWritesKeepWholeLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.Write([]byte("0123456789\n"))
			}
		}()
	}
	wg.Wait()
	l.Close()
	for _, line := range strings.Split(strings.TrimSuffix(read(t, p), "\n"), "\n") {
		if line != "0123456789" {
			t.Fatalf("mixed line %q", line)
		}
	}
}

func TestOpenFailsInAMissingDir(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "no", "log.txt"), 10); err == nil {
		t.Fatal("opened a log in a missing directory")
	}
}

// blockRotation makes log.txt.1 a non-empty directory, so renaming onto it fails.
func blockRotation(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(p+".1", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestAFailedRotationBacksOff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 10)
	defer l.Close()
	blockRotation(t, p)
	renames := 0
	rename = func(a, b string) error { renames++; return os.Rename(a, b) }
	defer func() { rename = os.Rename }()
	line := []byte("aaaa\n")
	for i := 0; i < 8; i++ { // 40 bytes: well past the cap
		if _, err := l.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, p); got != strings.Repeat("aaaa\n", 8) {
		t.Errorf("lines were lost: %q", got)
	}
	// A try at 10 bytes, the next only after the file has grown by the cap again.
	if renames > 3 {
		t.Errorf("%d rename attempts for 40 bytes; want a back-off", renames)
	}
}

// A rotation that keeps failing must not let the log grow for ever: after
// maxFailedRotations tries the file is truncated in place and the newest
// lines stay.
func TestAPermanentlyFailingRotationIsCapped(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 10)
	defer l.Close()
	blockRotation(t, p)
	for i := 0; i < 200; i++ {
		if _, err := l.Write([]byte(fmt.Sprintf("%03d\n", i))); err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(p); st.Size() > int64(10*(maxFailedRotations+1)+4) {
			t.Fatalf("log.txt grew to %d bytes after %d lines", st.Size(), i+1)
		}
	}
	if got := read(t, p); !strings.HasSuffix(got, "199\n") {
		t.Errorf("the newest line is missing: %q", got)
	}
}

func TestRotationResumesAfterAFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 10)
	defer l.Close()
	blockRotation(t, p)
	l.Write([]byte("aaaa\n"))
	l.Write([]byte("bbbb\n"))
	l.Write([]byte("cccc\n")) // fails to rotate
	os.RemoveAll(p + ".1")
	for _, s := range []string{"dddd\n", "eeee\n", "ffff\n"} {
		l.Write([]byte(s))
	}
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatalf("never rotated after the block went away: %v", err)
	}
	if got := read(t, p); len(got) > 10 {
		t.Errorf("log.txt still %d bytes", len(got))
	}
}

func TestReopenFailureFallsBackToStderr(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 10)
	defer l.Close()
	l.Write([]byte("aaaa\n"))
	l.Write([]byte("bbbb\n"))
	errFile, w := mustPipe(t)
	oldStderr := os.Stderr
	os.Stderr = w
	fail := true
	openFile = func(n string, f int, m os.FileMode) (*os.File, error) {
		if fail {
			return nil, os.ErrPermission
		}
		return os.OpenFile(n, f, m)
	}
	defer func() { os.Stderr = oldStderr; openFile = os.OpenFile }()
	n, err := l.Write([]byte("cccc\n")) // rotates, can't reopen
	if err != nil || n != 5 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	fail = false
	l.Write([]byte("dddd\n")) // the file comes back
	w.Close()
	b, _ := io.ReadAll(errFile)
	if string(b) != "cccc\n" {
		t.Errorf("stderr got %q", b)
	}
	if got := read(t, p); got != "dddd\n" {
		t.Errorf("log.txt = %q", got)
	}
}

func mustPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return r, w
}
