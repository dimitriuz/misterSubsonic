package main

import (
	"fmt"
	"strings"
	"time"

	"mistersubsonic/internal/input"
)

var keyNames = map[string]input.Button{
	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
}

type scripted struct {
	b    input.Button
	wait time.Duration // pause before this press
}

// parseKeys reads a -keys script: comma-separated button names, each
// optionally followed by ":<duration>" to wait before pressing it, e.g.
// "a:2s,a:1s,a,a:1s". The default wait is 400 ms.
func parseKeys(s string) ([]scripted, error) {
	var out []scripted
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, wait, found := strings.Cut(part, ":")
		b, ok := keyNames[name]
		if !ok {
			return nil, fmt.Errorf("-keys: unknown button %q", name)
		}
		d := 400 * time.Millisecond
		if found {
			var err error
			if d, err = time.ParseDuration(wait); err != nil {
				return nil, fmt.Errorf("-keys: %q: %v", part, err)
			}
		}
		out = append(out, scripted{b, d})
	}
	return out, nil
}

// playKeys sends the script as press/release pairs.
func playKeys(script []scripted) <-chan input.Event {
	ch := make(chan input.Event, 4)
	go func() {
		for _, k := range script {
			time.Sleep(k.wait)
			ch <- input.Event{Button: k.b, Kind: input.Press}
			ch <- input.Event{Button: k.b, Kind: input.Release}
		}
	}()
	return ch
}
