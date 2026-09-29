package ui

import (
	"strings"
	"time"
)

func splitLines(s string) []string { return strings.Split(s, "\n") }
func splitWords(s string) []string { return strings.Fields(s) }

// progressW is w scaled by pos/d, clamped to [0,w]. The product is done in
// int64: pos and d are nanoseconds, and w*ns overflows a 32-bit int (arm).
func progressW(w int, pos, d time.Duration) int {
	if d <= 0 || pos <= 0 {
		return 0
	}
	if pos > d {
		pos = d
	}
	return int(int64(w) * int64(pos) / int64(d))
}
