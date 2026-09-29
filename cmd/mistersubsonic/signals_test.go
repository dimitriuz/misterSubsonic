package main

import (
	"os"
	"slices"
	"syscall"
	"testing"
)

func TestShutdownSignalsIncludeHangup(t *testing.T) {
	for _, s := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !slices.Contains(shutdownSignals, os.Signal(s)) {
			t.Errorf("shutdownSignals lacks %v", s)
		}
	}
}
