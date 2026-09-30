// Package cache is a small on-disk LRU for cover art bytes. Entries are
// files named by a hash of the key; recency is the file's mtime, which is
// refreshed on every hit (the SD card's exFAT has no reliable atime).
package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// writeFile is replaceable in tests (a full SD card fails mid-write).
var writeFile = os.WriteFile

// walkDir is replaceable in tests (a walk that fails).
var walkDir = filepath.WalkDir

type Disk struct {
	dir string
	max int64
	now func() time.Time

	mu   sync.Mutex
	size int64 // bytes on disk, maintained incrementally
}

// Open uses dir (created if needed) with a byte budget. maxBytes <= 0 disables caching.
// Half-written entries left by a crash (*.tmp) are removed.
func Open(dir string, maxBytes int64) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	d := &Disk{dir: dir, max: maxBytes, now: time.Now}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".tmp") {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				log.Printf("art cache: removing %s: %v", e.Name(), err)
			}
			continue
		}
		d.size += info.Size()
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
	if err := writeFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp) // don't leave a partial entry behind (a full card)
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

// Delete removes key's entry, if any.
func (d *Disk) Delete(key string) {
	if d.max <= 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.path(key)
	if info, err := os.Stat(p); err == nil {
		if os.Remove(p) == nil {
			d.size -= info.Size()
		}
	}
}

// evictions counts directory walks (test hook).
var evictions int

// evictLocked removes the oldest entries down to a low-water mark of 90% of
// the budget, so the directory walk happens once per ~10% of new data. The
// walk also recounts the size, which drifts if files are deleted behind the
// cache's back. A walk that fails leaves the size and the files alone: a
// partial count would let the cache outgrow its budget.
func (d *Disk) evictLocked(keep string) {
	evictions++
	target := d.max * 9 / 10
	type ent struct {
		path string
		size int64
		mod  time.Time
	}
	var all []ent
	var total int64
	walkErr := walkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil // removed since the listing
		}
		if err != nil {
			return err
		}
		total += info.Size()
		if p != keep {
			all = append(all, ent{p, info.Size(), info.ModTime()})
		}
		return nil
	})
	if walkErr != nil {
		log.Printf("art cache: eviction walk: %v", walkErr)
		return
	}
	d.size = total
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, e := range all {
		if d.size <= target {
			return
		}
		if err := os.Remove(e.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			d.size -= e.size
		}
	}
}
