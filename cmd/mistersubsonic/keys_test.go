package main

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("a:2s, down ,a")
	if err != nil {
		t.Fatal(err)
	}
	want := []scripted{{input.BtnA, 2 * time.Second}, {input.BtnDown, 400 * time.Millisecond}, {input.BtnA, 400 * time.Millisecond}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if _, err := parseKeys("jump"); err == nil {
		t.Fatal("unknown button accepted")
	}
	if _, err := parseKeys("a:soon"); err == nil {
		t.Fatal("bad duration accepted")
	}
}
