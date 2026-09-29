package main

import (
	"math"
	"testing"
)

// R14 sound safety: without -volume, a run on a real device starts quiet.
func TestStartVolume(t *testing.T) {
	cases := []struct {
		name     string
		flag     float64
		null     bool
		cfg      float64
		want     float64
		announce bool
	}{
		{"real device, no flag: -30 dB", math.NaN(), false, 0, -30, true},
		{"real device, quieter config kept", math.NaN(), false, -45, -45, true},
		{"explicit -volume wins", -6, false, 0, -6, false},
		{"explicit -volume wins on null", -12, true, 0, -12, false},
		{"null device keeps config", math.NaN(), true, -3, -3, false},
	}
	for _, c := range cases {
		got, msg := startVolume(c.flag, c.null, c.cfg)
		if got != c.want || (msg != "") != c.announce {
			t.Errorf("%s: startVolume = %v, %q; want %v, announce=%v", c.name, got, msg, c.want, c.announce)
		}
	}
	if _, msg := startVolume(math.NaN(), false, 0); msg != "starting at -30 dB (use -volume to change)" {
		t.Errorf("message = %q", msg)
	}
}
