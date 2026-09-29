//go:build linux

package gfx

import (
	"errors"
	"fmt"
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
	f   *os.File
	mem []byte
	fmt fbFormat
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
	ff := fbFormat{width: int(v.Xres), height: int(v.Yres), stride: int(fx.LineLength), bpp: int(v.BitsPerPixel)}
	if err := ff.validate(); err != nil {
		f.Close()
		return nil, err
	}
	if err := v.checkLayout(); err != nil {
		f.Close()
		return nil, err
	}
	size := ff.stride * ff.height
	if uint64(size) > uint64(fx.SmemLen) {
		f.Close()
		return nil, fmt.Errorf("gfx: framebuffer memory %d < %d needed", fx.SmemLen, size)
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		mem, err = mmapDevMem(fx.SmemStart, size)
		if err != nil {
			f.Close()
			return nil, err
		}
	}
	return &FB{f: f, mem: mem, fmt: ff}, nil
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

func mmapDevMem(phys uintptr, size int) ([]byte, error) {
	m, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("gfx: fb mmap failed and /dev/mem unavailable: %w", err)
	}
	defer m.Close()
	mem, err := syscall.Mmap(int(m.Fd()), int64(phys), size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("gfx: mmap /dev/mem at %#x: %w", phys, err)
	}
	return mem, nil
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
	return nil
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
	err := syscall.Munmap(b.mem)
	b.mem = nil
	if b.f != nil {
		if e := b.f.Close(); err == nil {
			err = e
		}
		b.f = nil
	}
	return err
}
