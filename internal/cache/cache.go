// Package cache is a small on-disk LRU for cover art bytes. Entries are
// files named by a hash of the key; recency is the file's mtime, which is
// refreshed on every hit (the SD card's exFAT has no reliable atime).
package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Disk struct {
	dir string
	max int64
	now func() time.Time

	mu   sync.Mutex
	size int64 // bytes on disk, maintained incrementally
}

// Open uses dir (created if needed) with a byte budget. maxBytes <= 0 disables caching.
func Open(dir string, maxBytes int64) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	d := &Disk{dir: dir, max: maxBytes, now: time.Now}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			d.size += info.Size()
		}
	}
	return d, nil
}

func (d *Disk) path(key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(d.dir, hex.EncodeToString(h[:]))
}

// Get returns the cached bytes for key.
func (d *Disk) Get(key string) ([]byte, bool) {
	if d.max <= 0 {
		return nil, false
	}
	p := d.path(key)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	t := d.now()
	os.Chtimes(p, t, t)
	return b, true
}

// Put stores data under key, evicting least-recently-used entries to stay
// within the budget. Items larger than the whole budget are not stored.
func (d *Disk) Put(key string, data []byte) error {
	if d.max <= 0 || int64(len(data)) > d.max {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.path(key)
	var old int64
	if info, err := os.Stat(p); err == nil {
		old = info.Size()
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	t := d.now()
	os.Chtimes(p, t, t)
	d.size += int64(len(data)) - old
	if d.size > d.max {
		d.evictLocked(p)
	}
	return nil
}

// Size is the bytes currently cached.
func (d *Disk) Size() int64 { d.mu.Lock(); defer d.mu.Unlock(); return d.size }

func (d *Disk) evictLocked(keep string) {
	type ent struct {
		path string
		size int64
		mod  time.Time
	}
	var all []ent
	filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || p == keep {
			return nil
		}
		if info, err := e.Info(); err == nil {
			all = append(all, ent{p, info.Size(), info.ModTime()})
		}
		return nil
	})
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, e := range all {
		if d.size <= d.max {
			return
		}
		if err := os.Remove(e.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			d.size -= e.size
		}
	}
}
