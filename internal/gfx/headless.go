package gfx

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
)

// Headless is an in-memory Display for tests and screenshots. If Dir is set,
// every presented frame is also written there as frame-NNNNN.png.
type Headless struct {
	W, H int
	Dir  string

	mu     sync.Mutex
	last   *Canvas
	frames int
}

func NewHeadless(w, h int, dir string) *Headless { return &Headless{W: w, H: h, Dir: dir} }

func (d *Headless) Size() (int, int) { return d.W, d.H }
func (d *Headless) Close() error     { return nil }

func (d *Headless) Present(c *Canvas) error {
	cp := &Canvas{W: c.W, H: c.H, Pix: append([]uint32(nil), c.Pix...)}
	d.mu.Lock()
	d.last = cp
	d.frames++
	n := d.frames
	d.mu.Unlock()
	if d.Dir == "" {
		return nil
	}
	return SavePNG(filepath.Join(d.Dir, fmt.Sprintf("frame-%05d.png", n)), cp)
}

// Last returns the most recent frame (nil before the first Present).
func (d *Headless) Last() *Canvas { d.mu.Lock(); defer d.mu.Unlock(); return d.last }

// Frames is how many frames were presented.
func (d *Headless) Frames() int { d.mu.Lock(); defer d.mu.Unlock(); return d.frames }

// SavePNG writes c to path.
func SavePNG(path string, c *Canvas) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, c.ToRGBA()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
