//go:build linux

package gfx

import (
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"unsafe"
)

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
)

type fbBitfield struct{ Offset, Length, MsbRight uint32 }

// fbVarScreenInfo mirrors struct fb_var_screeninfo.
type fbVarScreenInfo struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset uint32
	BitsPerPixel, Grayscale                                uint32
	Red, Green, Blue, Transp                               fbBitfield
	Nonstd, Activate, Height, Width, AccelFlags            uint32
	Pixclock, LeftMargin, RightMargin, UpperMargin         uint32
	LowerMargin, HsyncLen, VsyncLen, Sync, Vmode, Rotate   uint32
	Colorspace                                             uint32
	Reserved                                               [4]uint32
}

// fbFixScreenInfo mirrors struct fb_fix_screeninfo (unsigned long = uintptr).
type fbFixScreenInfo struct {
	ID                             [16]byte
	SmemStart                      uintptr
	SmemLen, Type, TypeAux, Visual uint32
	XPanStep, YPanStep, YWrapStep  uint16
	LineLength                     uint32
	MmioStart                      uintptr
	MmioLen, Accel                 uint32
	Capabilities                   uint16
	Reserved                       [2]uint16
}

// FB is the Linux framebuffer display.
type FB struct {
	f       *os.File
	mapping []byte // the whole mmap, for Munmap
	mem     []byte // the visible screen inside it
	fmt     fbFormat
	last    *Canvas // the frame last presented, for Intact
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// OpenFB maps the framebuffer device (normally /dev/fb0). If mmap on the
// device fails, it maps the same memory through /dev/mem.
func OpenFB(path string) (*FB, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("gfx: open %s: %w", path, err)
	}
	var v fbVarScreenInfo
	var fx fbFixScreenInfo
	if err := ioctl(f.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&v)); err != nil {
		f.Close()
		return nil, fmt.Errorf("gfx: FBIOGET_VSCREENINFO: %w", err)
	}
	if err := ioctl(f.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&fx)); err != nil {
		f.Close()
		return nil, fmt.Errorf("gfx: FBIOGET_FSCREENINFO: %w", err)
	}
	ff, off, end, err := fbLayout(&v, &fx)
	if err != nil {
		f.Close()
		return nil, err
	}
	mapping, err := syscall.Mmap(int(f.Fd()), 0, end, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	delta := 0
	if err != nil {
		mapping, delta, err = mmapDevMem(fx.SmemStart, end)
		if err != nil {
			f.Close()
			return nil, err
		}
	}
	mem, err := screenSlice(mapping, off, end, delta)
	if err != nil {
		syscall.Munmap(mapping)
		f.Close()
		return nil, err
	}
	return &FB{f: f, mapping: mapping, mem: mem, fmt: ff}, nil
}

// fbLayout checks the screen format and finds the visible screen in
// framebuffer memory: it starts at the pan offset (the console may have
// panned to another page) and must end inside smem_len. It returns the
// screen's byte offset and where it ends.
func fbLayout(v *fbVarScreenInfo, fx *fbFixScreenInfo) (ff fbFormat, off, end int, err error) {
	ff = fbFormat{width: int(v.Xres), height: int(v.Yres), stride: int(fx.LineLength), bpp: int(v.BitsPerPixel)}
	if err := ff.validate(); err != nil {
		return ff, 0, 0, err
	}
	if err := v.checkLayout(); err != nil {
		return ff, 0, 0, err
	}
	// The last line needs only its pixels, not the stride's padding. The sums
	// are 64-bit: int is 32 bits on the MiSTer's ARM.
	off64 := int64(v.Yoffset)*int64(ff.stride) + int64(v.Xoffset)*int64(ff.bpp)/8
	end64 := off64 + int64(ff.height-1)*int64(ff.stride) + int64(ff.width)*int64(ff.bpp)/8
	if end64 > int64(fx.SmemLen) || end64 > math.MaxInt {
		return ff, 0, 0, fmt.Errorf("gfx: framebuffer memory %d < %d needed", fx.SmemLen, end64)
	}
	return ff, int(off64), int(end64), nil
}

// screenSlice is the visible screen inside the mapping: the mapping covers
// [0, end) of the framebuffer memory (plus delta bytes in front when it came
// through /dev/mem), the screen is [off, end).
func screenSlice(mapping []byte, off, end, delta int) ([]byte, error) {
	if off < 0 || off > end || delta < 0 || end+delta > len(mapping) {
		return nil, fmt.Errorf("gfx: screen %d..%d (+%d) outside the %d-byte mapping", off, end, delta, len(mapping))
	}
	return mapping[off+delta : end+delta], nil
}

// checkLayout accepts only XRGB8888 (32 bpp) and RGB565 (16 bpp) channel layouts.
func (v *fbVarScreenInfo) checkLayout() error {
	r, g, b := v.Red, v.Green, v.Blue
	ok := false
	switch v.BitsPerPixel {
	case 32:
		ok = r.Offset == 16 && r.Length == 8 && g.Offset == 8 && g.Length == 8 && b.Offset == 0 && b.Length == 8
	case 16:
		ok = r.Offset == 11 && r.Length == 5 && g.Offset == 5 && g.Length == 6 && b.Offset == 0 && b.Length == 5
	}
	if !ok {
		return fmt.Errorf("gfx: unsupported %d bpp pixel layout r%d/%d g%d/%d b%d/%d",
			v.BitsPerPixel, r.Offset, r.Length, g.Offset, g.Length, b.Offset, b.Length)
	}
	return nil
}

// mmapDevMem maps size bytes of physical memory at phys through /dev/mem.
// mmap needs a page-aligned offset, so the mapping starts at phys's page;
// delta is where phys lies inside it.
func mmapDevMem(phys uintptr, size int) (mapping []byte, delta int, err error) {
	m, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("gfx: fb mmap failed and /dev/mem unavailable: %w", err)
	}
	defer m.Close()
	base, delta := pageAlign(phys, os.Getpagesize())
	mapping, err = syscall.Mmap(int(m.Fd()), int64(base), size+delta, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, 0, fmt.Errorf("gfx: mmap /dev/mem at %#x: %w", phys, err)
	}
	return mapping, delta, nil
}

// pageAlign splits a physical address into the start of its page and the
// offset inside that page.
func pageAlign(phys uintptr, page int) (base uintptr, delta int) {
	delta = int(phys % uintptr(page))
	return phys - uintptr(delta), delta
}

func (b *FB) Size() (int, int) { return b.fmt.width, b.fmt.height }

func (b *FB) Present(c *Canvas) error {
	if b.mem == nil {
		return errors.New("gfx: framebuffer closed")
	}
	if c.W != b.fmt.width || c.H != b.fmt.height {
		return fmt.Errorf("gfx: frame %dx%d != framebuffer %dx%d", c.W, c.H, b.fmt.width, b.fmt.height)
	}
	b.fmt.pack(b.mem, c)
	b.last = c
	return nil
}

// PresentRects shows the rectangles rs of c, a full-size frame whose other
// pixels are already on screen.
func (b *FB) PresentRects(c *Canvas, rs []Rect) error {
	if b.mem == nil {
		return errors.New("gfx: framebuffer closed")
	}
	if c.W != b.fmt.width || c.H != b.fmt.height {
		return fmt.Errorf("gfx: frame %dx%d != framebuffer %dx%d", c.W, c.H, b.fmt.width, b.fmt.height)
	}
	for _, r := range rs {
		b.fmt.packRect(b.mem, c, r)
	}
	b.last = c
	return nil
}

// Intact reports whether the screen still shows the frame last presented,
// from a grid of sampled pixels: Main_MiSTer or the console may have drawn
// over it (spec §8.1). It is true before the first Present and after Close.
func (b *FB) Intact() bool {
	if b.mem == nil || b.last == nil {
		return true
	}
	return b.fmt.matches(b.mem, b.last)
}

// Blank clears the framebuffer to black (used on exit).
func (b *FB) Blank() {
	for i := range b.mem {
		b.mem[i] = 0
	}
}

// Close blanks and unmaps the framebuffer. It is idempotent.
func (b *FB) Close() error {
	if b.mem == nil {
		return nil
	}
	b.Blank()
	err := syscall.Munmap(b.mapping)
	b.mem, b.mapping, b.last = nil, nil, nil
	if b.f != nil {
		if e := b.f.Close(); err == nil {
			err = e
		}
		b.f = nil
	}
	return err
}
