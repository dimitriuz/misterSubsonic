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

// File is a size-capped log file. It is safe for concurrent use.
type File struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
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
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
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
// A line longer than the cap still goes in whole, into a fresh file.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves log.txt to log.txt.1. If the rename fails the log goes on
// growing rather than losing lines.
func (l *File) rotate() error {
	l.f.Close()
	l.f = nil
	os.Rename(l.path, l.path+".1")
	return l.open()
}

// Close closes the file. Later writes fail with os.ErrClosed.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
