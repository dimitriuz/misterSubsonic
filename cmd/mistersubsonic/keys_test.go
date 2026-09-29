package main

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("a:2s, down ,'abba:1s,queue")
	if err != nil {
		t.Fatal(err)
	}
	want := []scripted{
		{b: input.BtnA, wait: 2 * time.Second},
		{b: input.BtnDown, wait: 400 * time.Millisecond},
		{text: "abba", wait: time.Second},
		{b: input.BtnQueue, wait: 400 * time.Millisecond},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	for _, bad := range []string{"jump", "a:soon", "'"} {
		if _, err := parseKeys(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}
