package cache

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPutGetAndLRUEviction(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir, 25)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	d.Put("a", bytes.Repeat([]byte{1}, 10))
	d.Put("b", bytes.Repeat([]byte{2}, 10))
	if _, ok := d.Get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	d.Put("c", bytes.Repeat([]byte{3}, 10)) // over budget: evict b (oldest)
	if _, ok := d.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := d.Get(k); !ok {
			t.Fatalf("%s evicted wrongly", k)
		}
	}
	if d.Size() != 20 {
		t.Fatalf("size = %d, want 20", d.Size())
	}
	// A reopened cache sees the same size.
	d2, _ := Open(dir, 25)
	if d2.Size() != 20 {
		t.Fatalf("reopened size = %d", d2.Size())
	}
	if got, ok := d2.Get("c"); !ok || got[0] != 3 {
		t.Fatal("c not readable after reopen")
	}
}

func TestOverwriteAndOversizeAndDisabled(t *testing.T) {
	d, _ := Open(t.TempDir(), 100)
	d.Put("k", make([]byte, 40))
	d.Put("k", make([]byte, 10))
	if d.Size() != 10 {
		t.Fatalf("size after overwrite = %d", d.Size())
	}
	d.Put("huge", make([]byte, 1000))
	if _, ok := d.Get("huge"); ok {
		t.Fatal("item larger than the budget was stored")
	}
	off, _ := Open(t.TempDir(), 0)
	off.Put("x", []byte("y"))
	if _, ok := off.Get("x"); ok {
		t.Fatal("disabled cache stored data")
	}
}

func TestEvictionUsesLowWaterMark(t *testing.T) {
	d, err := Open(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	for i := 0; i < 10; i++ {
		d.Put(string(rune('a'+i)), bytes.Repeat([]byte{1}, 10))
	}
	before := evictions
	d.Put("k", bytes.Repeat([]byte{1}, 10))
	if d.Size() > 90 {
		t.Fatalf("size after eviction = %d, want <= 90", d.Size())
	}
	if evictions != before+1 {
		t.Fatalf("evictions = %d, want %d", evictions, before+1)
	}
	d.Put("l", bytes.Repeat([]byte{1}, 10))
	if evictions != before+1 {
		t.Fatal("second Put walked the directory again")
	}
}

func TestDeleteAdjustsSize(t *testing.T) {
	d, _ := Open(t.TempDir(), 100)
	d.Put("a", bytes.Repeat([]byte{1}, 10))
	d.Delete("a")
	d.Delete("missing")
	if d.Size() != 0 {
		t.Fatalf("size = %d", d.Size())
	}
	if _, ok := d.Get("a"); ok {
		t.Fatal("a still present")
	}
}

func TestOpenRemovesHalfWrittenEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "0123.tmp"), bytes.Repeat([]byte{9}, 100), 0o644)
	os.WriteFile(filepath.Join(dir, "4567"), bytes.Repeat([]byte{1}, 50), 0o644)
	d, err := Open(dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if d.Size() != 50 {
		t.Fatalf("size %d, want 50 (the .tmp isn't an entry)", d.Size())
	}
	if _, err := os.Stat(filepath.Join(dir, "0123.tmp")); !os.IsNotExist(err) {
		t.Fatal("the half-written entry is still there")
	}
}

func TestAFailedWriteLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	d, _ := Open(dir, 1000)
	old := writeFile
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		os.WriteFile(name, data[:len(data)/2], perm) // the card filled up halfway
		return errors.New("no space left on device")
	}
	defer func() { writeFile = old }()
	if err := d.Put("a", bytes.Repeat([]byte{1}, 100)); err == nil {
		t.Fatal("Put hid the write error")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("left behind %v", ents)
	}
	if d.Size() != 0 {
		t.Fatalf("size %d after a failed write", d.Size())
	}
}

// Files deleted behind the cache's back are noticed at the next eviction,
// instead of evicting live entries to make room that is already free.
func TestEvictionRecountsAfterOutsideDeletes(t *testing.T) {
	dir := t.TempDir()
	d, _ := Open(dir, 1000)
	clock := time.Unix(1000, 0)
	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		d.Put(k, bytes.Repeat([]byte{1}, 150)) // 750 bytes
	}
	for _, k := range []string{"c", "d", "e"} {
		os.Remove(d.path(k)) // cleaned up by hand; the cache still counts 750
	}
	d.Put("f", bytes.Repeat([]byte{2}, 300)) // counted 1050 > 1000: an eviction walk
	for _, k := range []string{"a", "b", "f"} {
		if _, ok := d.Get(k); !ok {
			t.Errorf("%s was evicted, though only 600 bytes are on disk", k)
		}
	}
	if d.Size() != 600 {
		t.Fatalf("size %d, want 600", d.Size())
	}
}
