//go:build linux

package input

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// rawEvent mirrors struct input_event; the timeval fields are C longs, which
// match Go's int on Linux (4 bytes on ARMv7, 8 on amd64).
type rawEvent struct {
	Sec, Usec int
	Type      uint16
	Code      uint16
	Value     int32
}

const rawEventSize = int(unsafe.Sizeof(rawEvent{}))

// ioctl request numbers (asm-generic encoding: dir<<30 | size<<16 | type<<8 | nr).
const (
	iocRead  = 2
	iocWrite = 1
)

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'E'<<8 | nr }

var (
	eviocgrab = ioc(iocWrite, 0x90, 4)
)

func eviocgname(n uintptr) uintptr    { return ioc(iocRead, 0x06, n) }
func eviocgbit(ev, n uintptr) uintptr { return ioc(iocRead, 0x20+ev, n) }
func eviocgabs(abs uintptr) uintptr   { return ioc(iocRead, 0x40+abs, 24) }

// fileIoctl runs an ioctl on f without f.Fd() (which would make reads
// blocking and stop Close from interrupting them). arg is an integer value.
func fileIoctl(f *os.File, req, arg uintptr) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var e syscall.Errno
	if cerr := rc.Control(func(fd uintptr) {
		_, _, e = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	}); cerr != nil {
		return cerr
	}
	if e != 0 {
		return e
	}
	return nil
}

// fileIoctlPtr is fileIoctl with a pointer argument.
func fileIoctlPtr(f *os.File, req uintptr, p unsafe.Pointer) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var e syscall.Errno
	if cerr := rc.Control(func(fd uintptr) {
		_, _, e = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(p))
	}); cerr != nil {
		return cerr
	}
	if e != 0 {
		return e
	}
	return nil
}

// ManagerOptions configures device handling.
type ManagerOptions struct {
	// Glob of device nodes; default /dev/input/event*.
	Glob string
	// MapDir holds MiSTer controller maps; default /media/fat/config/inputs.
	MapDir string
	// Grab takes devices exclusively so MiSTer Main ignores them.
	Grab bool
	// Rescan interval for hotplugged devices; default 2 s.
	Rescan time.Duration
}

// Manager reads all input devices and delivers button events.
type Manager struct {
	o      ManagerOptions
	events chan Event
	quit   chan struct{}
	wg     sync.WaitGroup // device readers
	loopWg sync.WaitGroup // rescan loop

	mu     sync.Mutex
	closed bool
	devs   map[string]*os.File
	pads   map[string]bool // devices that are gamepads
}

// NewManager starts scanning immediately.
func NewManager(o ManagerOptions) *Manager {
	if o.Glob == "" {
		o.Glob = "/dev/input/event*"
	}
	if o.MapDir == "" {
		o.MapDir = "/media/fat/config/inputs"
	}
	if o.Rescan <= 0 {
		o.Rescan = 2 * time.Second
	}
	m := &Manager{o: o, events: make(chan Event, 64), quit: make(chan struct{}), devs: map[string]*os.File{}, pads: map[string]bool{}}
	m.scan()
	m.loopWg.Add(1)
	go m.rescanLoop()
	return m
}

func (m *Manager) Events() <-chan Event { return m.events }

// Close releases every grab and stops all readers. It is safe to call twice.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.mu.Unlock()
	close(m.quit)
	m.loopWg.Wait() // no scan can register devices after this
	m.mu.Lock()
	for path, f := range m.devs {
		delete(m.devs, path)
		if f == nil {
			continue
		}
		if m.o.Grab {
			fileIoctl(f, eviocgrab, 0)
		}
		f.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) rescanLoop() {
	defer m.loopWg.Done()
	t := time.NewTicker(m.o.Rescan)
	defer t.Stop()
	for {
		select {
		case <-m.quit:
			return
		case <-t.C:
			m.scan()
		}
	}
}

func (m *Manager) scan() {
	paths, _ := filepath.Glob(m.o.Glob)
	present := make(map[string]bool, len(paths))
	for _, p := range paths {
		present[p] = true
	}
	m.mu.Lock()
	for p, f := range m.devs {
		if f == nil && !present[p] {
			delete(m.devs, p) // skipped device is gone; its node may be reused
		}
	}
	m.mu.Unlock()
	for _, p := range paths {
		m.mu.Lock()
		_, known := m.devs[p]
		m.mu.Unlock()
		if !known {
			m.open(p)
		}
	}
}

func deviceName(f *os.File) string {
	buf := make([]byte, 256)
	if err := fileIoctlPtr(f, eviocgname(uintptr(len(buf))), unsafe.Pointer(&buf[0])); err != nil {
		return ""
	}
	return string(bytes.TrimRight(buf, "\x00"))
}

func hasEventType(f *os.File, typ uint) bool {
	var bits [4]byte
	if err := fileIoctlPtr(f, eviocgbit(0, uintptr(len(bits))), unsafe.Pointer(&bits[0])); err != nil {
		return false
	}
	return bits[typ/8]&(1<<(typ%8)) != 0
}

// keyBitsLen covers every key code up to KEY_MAX (0x2ff).
const keyBitsLen = 0x300 / 8

// padKeys reports whether a device's EV_KEY bits include joystick or gamepad
// buttons (BTN_JOYSTICK 0x120 .. BTN_THUMBR 0x13e) or the D-pad buttons.
func padKeys(bits []byte) bool {
	has := func(c uint16) bool { return int(c/8) < len(bits) && bits[c/8]&(1<<(c%8)) != 0 }
	for c := uint16(0x120); c <= 0x13e; c++ {
		if has(c) {
			return true
		}
	}
	return has(btnDpadUp) || has(btnDpadDn) || has(btnDpadL) || has(btnDpadR)
}

func isGamepad(f *os.File) bool {
	bits := make([]byte, keyBitsLen)
	if err := fileIoctlPtr(f, eviocgbit(evKey, keyBitsLen), unsafe.Pointer(&bits[0])); err != nil {
		return false
	}
	return padKeys(bits)
}

// HasPad reports whether a gamepad is connected.
func (m *Manager) HasPad() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pads) > 0
}

func absRange(f *os.File, code uint16) (AbsRange, bool) {
	var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
	if err := fileIoctlPtr(f, eviocgabs(uintptr(code)), unsafe.Pointer(&info[0])); err != nil {
		return AbsRange{}, false
	}
	return AbsRange{Min: info[1], Max: info[2]}, true
}

func (m *Manager) open(path string) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	name := deviceName(f)
	if strings.Contains(name, "MiSTer virtual input") || !(hasEventType(f, evKey) || hasEventType(f, evAbs)) {
		f.Close()
		m.mu.Lock()
		m.devs[path] = nil // remember so we don't reopen it every rescan
		m.mu.Unlock()
		return
	}
	keys := map[uint16]Button{}
	var axes map[AxisKey]Button
	if vid, pid, ok := sysfsIDs(path); ok {
		if mp := findMapFile(m.o.MapDir, vid, pid); mp != "" {
			if b, err := os.ReadFile(mp); err == nil {
				if km, err := ParseMisterMap(b); err == nil {
					keys = km
				}
				if am, err := ParseMisterAxes(b); err == nil {
					axes = am
				}
			}
		}
	}
	abs := map[uint16]AbsRange{}
	calibrate := []uint16{absX, absY}
	for k := range axes {
		calibrate = append(calibrate, k.Axis)
	}
	for _, c := range calibrate {
		if r, ok := absRange(f, c); ok {
			abs[c] = r
		}
	}
	if m.o.Grab {
		if err := fileIoctl(f, eviocgrab, 1); err != nil {
			log.Printf("input: grab %s (%s): %v", path, name, err)
		}
	}
	if isGamepad(f) {
		m.mu.Lock()
		m.pads[path] = true
		m.mu.Unlock()
	}
	m.attach(path, f, newTranslator(keys, abs).withAxes(axes))
}

// attach registers f and starts its reader.
func (m *Manager) attach(path string, f *os.File, tr *translator) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		if m.o.Grab {
			fileIoctl(f, eviocgrab, 0)
		}
		f.Close()
		return
	}
	m.devs[path] = f
	m.mu.Unlock()
	m.wg.Add(1)
	go m.read(path, f, tr)
}

func (m *Manager) read(path string, f *os.File, tr *translator) {
	defer m.wg.Done()
	buf := make([]byte, rawEventSize*32)
	for {
		n, err := f.Read(buf)
		if err != nil {
			m.mu.Lock()
			if m.devs[path] == f {
				delete(m.devs, path) // unplugged: rescan may pick it up again
				delete(m.pads, path)
				f.Close()
			}
			m.mu.Unlock()
			return
		}
		for _, ev := range decodeEvents(buf[:n]) {
			for _, e := range tr.handle(ev.Type, ev.Code, ev.Value) {
				select {
				case m.events <- e:
				case <-m.quit:
					return
				}
			}
		}
	}
}

func decodeEvents(b []byte) []rawEvent {
	var out []rawEvent
	for len(b) >= rawEventSize {
		out = append(out, *(*rawEvent)(unsafe.Pointer(&b[0])))
		b = b[rawEventSize:]
	}
	return out
}

// sysfsIDs reads the vendor/product of /dev/input/eventN.
func sysfsIDs(devPath string) (vid, pid uint16, ok bool) {
	return readIDs(filepath.Join("/sys/class/input", filepath.Base(devPath), "device/id"))
}

func readIDs(dir string) (vid, pid uint16, ok bool) {
	read := func(name string) (uint16, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
		return uint16(v), err == nil
	}
	v, ok1 := read("vendor")
	p, ok2 := read("product")
	return v, p, ok1 && ok2
}

// findMapFile locates MiSTer's map for vid:pid, preferring the exact name.
func findMapFile(dir string, vid, pid uint16) string {
	exact := filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_v3.map", vid, pid))
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	matches, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_*_v3.map", vid, pid)))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}
