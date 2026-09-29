package player

import (
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

func TestSetReplayGainAppliesToTheNextTrack(t *testing.T) {
	h := newHarness(t, nil) // ReplayGain off
	q := songs(2, 100)
	g := -6.0
	for i := range q {
		q[i].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
	}
	h.p.PlayNow(q, 0)
	if tr := h.playAndStart(1); tr.Gain != 1 {
		t.Fatalf("gain %v with replaygain off", tr.Gain)
	}
	h.p.SetReplayGain("track")
	h.p.Next()
	h.waitFor("second song", func() bool { return h.eng.playCount() == 2 })
	if tr := h.eng.lastPlayed(); tr.Gain < 0.50 || tr.Gain > 0.51 {
		t.Fatalf("gain %v after SetReplayGain(track), want -6 dB", tr.Gain)
	}
}

func TestSetScrobbleOff(t *testing.T) {
	h := newHarness(t, nil)
	h.p.SetScrobble(false)
	h.p.PlayNow(songs(1, 100), 0)
	a := h.playAndStart(1)
	pos := time.Duration(0)
	for range 4 * 60 {
		pos += 250 * time.Millisecond
		h.tickAt(a.ID, pos)
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(h.api.nowPlayings()) + len(h.api.submissions()); n != 0 {
		t.Fatalf("%d scrobble calls with scrobbling off", n)
	}
}
