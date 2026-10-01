// Package logfile is the app's log on the SD card: log.txt, capped in size.
// When a write would take log.txt past the cap, log.txt becomes log.txt.1
// (replacing the older one) and a new log.txt starts, so the log never takes
// more than twice the cap (spec §9: 1 MB × 2).
package logfile

import (
	"fmt"
	"os"
	"sync"
)

// Stand-ins for the tests.
var (
	rename   = os.Rename
	openFile = os.OpenFile
)

// File is a size-capped log file. It is safe for concurrent use.
type File struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64

	retryAt int64 // after a failed rotation: the size to try again at
	failed  int   // failed rotations in a row
	closed  bool
}

// Open appends to the log at path, rotating it when it passes max bytes.
func Open(path string, max int64) (*File, error) {
	l := &File{path: path, max: max}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := openFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("logfile: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logfile: %w", err)
	}
	l.f, l.size = f, st.Size()
	return nil
}

// Write appends p, rotating first if p would take the file past the cap.
// A line longer than the cap still goes in whole, into a fresh file. While
// the file can't be reopened, lines go to stderr instead and each write
// tries the file again.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.f == nil && l.open() != nil {
		return os.Stderr.Write(p)
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max && l.size >= l.retryAt {
		l.rotate()
		if l.f == nil {
			return os.Stderr.Write(p)
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// maxFailedRotations bounds the growth when rotating keeps failing: the log
// takes at most maxFailedRotations+1 caps before it is truncated.
const maxFailedRotations = 3

const truncatedMarker = "log truncated after failed rotations\n"

// rotate moves log.txt to log.txt.1 and starts a new log.txt. If the rename
// fails the log goes on growing rather than losing lines, and the next try
// comes when it has grown by the cap again; after maxFailedRotations failures
// in a row the file is truncated in place instead (the SD card must not fill
// up). If the new file can't be opened l.f stays nil.
func (l *File) rotate() {
	if err := rename(l.path, l.path+".1"); err != nil {
		l.failed++
		if l.failed > maxFailedRotations && l.f.Truncate(0) == nil { // O_APPEND: the next write lands at 0
			l.size, l.retryAt, l.failed = 0, 0, 0
			n, _ := l.f.WriteString(truncatedMarker)
			l.size = int64(n)
			return
		}
		l.retryAt = l.size + l.max
		return
	}
	l.retryAt, l.failed = 0, 0
	l.f.Close()
	l.f = nil
	l.open()
}

// Close closes the file. Later writes fail with os.ErrClosed.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
