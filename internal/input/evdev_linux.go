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

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// ioctlVal passes an integer argument (EVIOCGRAB takes the grab flag by value).
func ioctlVal(fd, req, val uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, val); e != 0 {
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
	wg     sync.WaitGroup

	mu   sync.Mutex
	devs map[string]*os.File
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
	m := &Manager{o: o, events: make(chan Event, 64), quit: make(chan struct{}), devs: map[string]*os.File{}}
	m.scan()
	m.wg.Add(1)
	go m.rescanLoop()
	return m
}

func (m *Manager) Events() <-chan Event { return m.events }

// Close releases every grab and stops all readers.
func (m *Manager) Close() {
	close(m.quit)
	m.mu.Lock()
	for path, f := range m.devs {
		if f == nil {
			delete(m.devs, path)
			continue
		}
		if m.o.Grab {
			ioctlVal(f.Fd(), eviocgrab, 0)
		}
		f.Close()
		delete(m.devs, path)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) rescanLoop() {
	defer m.wg.Done()
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
	if err := ioctl(f.Fd(), eviocgname(uintptr(len(buf))), unsafe.Pointer(&buf[0])); err != nil {
		return ""
	}
	return string(bytes.TrimRight(buf, "\x00"))
}

func hasEventType(f *os.File, typ uint) bool {
	var bits [4]byte
	if err := ioctl(f.Fd(), eviocgbit(0, uintptr(len(bits))), unsafe.Pointer(&bits[0])); err != nil {
		return false
	}
	return bits[typ/8]&(1<<(typ%8)) != 0
}

func absRange(f *os.File, code uint16) (AbsRange, bool) {
	var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
	if err := ioctl(f.Fd(), eviocgabs(uintptr(code)), unsafe.Pointer(&info[0])); err != nil {
		return AbsRange{}, false
	}
	return AbsRange{Min: info[1], Max: info[2]}, true
}

func (m *Manager) open(path string) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
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
	if vid, pid, ok := sysfsIDs(path); ok {
		if mp := findMapFile(m.o.MapDir, vid, pid); mp != "" {
			if b, err := os.ReadFile(mp); err == nil {
				if km, err := ParseMisterMap(b); err == nil {
					keys = km
				}
			}
		}
	}
	abs := map[uint16]AbsRange{}
	for _, c := range []uint16{absX, absY} {
		if r, ok := absRange(f, c); ok {
			abs[c] = r
		}
	}
	if m.o.Grab {
		if err := ioctlVal(f.Fd(), eviocgrab, 1); err != nil {
			log.Printf("input: grab %s (%s): %v", path, name, err)
		}
	}
	m.mu.Lock()
	m.devs[path] = f
	m.mu.Unlock()
	m.wg.Add(1)
	go m.read(path, f, newTranslator(keys, abs))
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
