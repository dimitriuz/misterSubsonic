//go:build linux

package input

import (
	"os"
	"path/filepath"
	"testing"
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
