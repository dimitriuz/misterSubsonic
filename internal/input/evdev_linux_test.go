//go:build linux

package input

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func TestIoctlNumbers(t *testing.T) {
	// Values from <linux/input.h> as compiled by gcc.
	if eviocgrab != 0x40044590 {
		t.Fatalf("EVIOCGRAB = %#x", eviocgrab)
	}
	if got := eviocgname(256); got != 0x81004506 {
		t.Fatalf("EVIOCGNAME(256) = %#x", got)
	}
	if got := eviocgabs(absX); got != 0x80184540 {
		t.Fatalf("EVIOCGABS(ABS_X) = %#x", got)
	}
	if got := eviocgbit(0, 4); got != 0x80044520 {
		t.Fatalf("EVIOCGBIT(0,4) = %#x", got)
	}
}

func TestDecodeEvents(t *testing.T) {
	evs := []rawEvent{{Type: evKey, Code: keyEnter, Value: 1}, {Type: evAbs, Code: absHat0X, Value: -1}}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&evs[0])), 2*rawEventSize)
	got := decodeEvents(append(b, 1, 2, 3)) // a trailing partial event is ignored
	if len(got) != 2 || got[0].Code != keyEnter || got[1].Value != -1 {
		t.Fatalf("decoded %+v", got)
	}
}

func TestFindMapFileAndIDs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input_045e_028e_v3.map"), make([]byte, 128), 0o644)
	os.WriteFile(filepath.Join(dir, "input_2dc8_6001_btaddr_v3.map"), make([]byte, 128), 0o644)
	if got := findMapFile(dir, 0x045e, 0x028e); filepath.Base(got) != "input_045e_028e_v3.map" {
		t.Fatalf("exact map = %q", got)
	}
	if got := findMapFile(dir, 0x2dc8, 0x6001); filepath.Base(got) != "input_2dc8_6001_btaddr_v3.map" {
		t.Fatalf("suffixed map = %q", got)
	}
	if findMapFile(dir, 1, 2) != "" {
		t.Fatal("found a map for an unknown device")
	}
	ids := t.TempDir()
	os.WriteFile(filepath.Join(ids, "vendor"), []byte("045e\n"), 0o644)
	os.WriteFile(filepath.Join(ids, "product"), []byte("028e\n"), 0o644)
	if v, p, ok := readIDs(ids); !ok || v != 0x045e || p != 0x028e {
		t.Fatalf("ids = %x %x %v", v, p, ok)
	}
}

func newIdleManager(rescan time.Duration) *Manager {
	return NewManager(ManagerOptions{Glob: filepath.Join(os.TempDir(), "no-such-dir-xyz", "event*"), Rescan: rescan})
}

func TestCloseReturnsWithIdleDevice(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	deviceName(r) // ENOTTY on a pipe; must not make the fd blocking
	hasEventType(r, evKey)
	m := newIdleManager(20 * time.Millisecond)
	m.attach("pipe", r, newTranslator(nil, nil))
	time.Sleep(100 * time.Millisecond) // let the reader block in read
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close hung with an idle device")
	}
}

func TestCloseTwiceAndAttachAfterClose(t *testing.T) {
	m := newIdleManager(20 * time.Millisecond)
	m.Close()
	m.Close() // must not panic
	r, w, _ := os.Pipe()
	defer w.Close()
	m.attach("pipe", r, newTranslator(nil, nil))
	if len(m.devs) != 0 {
		t.Fatalf("device registered after Close: %v", m.devs)
	}
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Fatal("file still open after attach on a closed manager")
	}
}

func TestScanPrunesStalePlaceholders(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "event0")
	os.WriteFile(p, nil, 0o644)
	m := NewManager(ManagerOptions{Glob: filepath.Join(dir, "event*"), Rescan: time.Hour})
	defer m.Close()
	m.mu.Lock()
	v, ok := m.devs[p]
	m.mu.Unlock()
	if !ok || v != nil {
		t.Fatalf("expected nil placeholder, got %v %v", v, ok)
	}
	os.Remove(p)
	m.scan()
	m.mu.Lock()
	_, ok = m.devs[p]
	m.mu.Unlock()
	if ok {
		t.Fatal("stale placeholder not pruned")
	}
}

// A device is a gamepad when its key bits include the joystick or gamepad
// buttons (BTN_JOYSTICK..BTN_THUMBR, or the D-pad buttons); a keyboard
// with mouse buttons or media keys is not.
func TestGamepadKeyBits(t *testing.T) {
	bits := func(codes ...uint16) []byte {
		b := make([]byte, keyBitsLen)
		for _, c := range codes {
			b[c/8] |= 1 << (c % 8)
		}
		return b
	}
	for _, c := range []struct {
		name  string
		codes []uint16
		want  bool
	}{
		{"keyboard", []uint16{keyEnter, keyEsc, keyVolumeUp, keyPlayPause}, false},
		{"keyboard with a touchpad's mouse buttons", []uint16{keyEnter, 0x110, 0x111}, false},
		{"gamepad", []uint16{btnSouth, btnEast, btnStart}, true},
		{"joystick", []uint16{0x120}, true},
		{"d-pad buttons only", []uint16{btnDpadUp}, true},
	} {
		if got := padKeys(bits(c.codes...)); got != c.want {
			t.Errorf("%s: padKeys = %v, want %v", c.name, got, c.want)
		}
	}
}
