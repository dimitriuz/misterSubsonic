package main

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("a:2s, down ,'127.0.0.1:4533,pause:1s,enter,queue")
	if err != nil {
		t.Fatal(err)
	}
	want := []scripted{
		{b: input.BtnA, wait: 2 * time.Second},
		{b: input.BtnDown, wait: 400 * time.Millisecond},
		{text: "127.0.0.1:4533", wait: 400 * time.Millisecond},
		{pause: true, wait: time.Second},
		{b: input.BtnA, r: '\n', wait: 400 * time.Millisecond},
		{b: input.BtnQueue, wait: 400 * time.Millisecond},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, bad := range []string{"jump", "a:soon", "'", "pause:x"} {
		if _, err := parseKeys(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestPlayKeys(t *testing.T) {
	ch := playKeys([]scripted{
		{b: input.BtnDown},
		{pause: true, wait: time.Millisecond},
		{text: "ab"},
		{b: input.BtnA, r: '\n'},
	})
	want := []input.Event{
		{Button: input.BtnDown, Kind: input.Press}, {Button: input.BtnDown, Kind: input.Release},
		{Kind: input.Press, Rune: 'a'}, {Kind: input.Release, Rune: 'a'},
		{Kind: input.Press, Rune: 'b'}, {Kind: input.Release, Rune: 'b'},
		{Button: input.BtnA, Kind: input.Press, Rune: '\n'}, {Button: input.BtnA, Kind: input.Release, Rune: '\n'},
	}
	for i, w := range want {
		select {
		case got := <-ch:
			if got != w {
				t.Fatalf("event %d: got %+v, want %+v", i, got, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d missing", i)
		}
	}
	select {
	case e := <-ch:
		t.Fatalf("extra event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}
