package logfile

import (
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
