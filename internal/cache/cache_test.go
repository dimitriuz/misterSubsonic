package cache

import (
	"bytes"
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
