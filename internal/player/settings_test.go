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

// A ReplayGain change reaches the playing track and the queued successor
// at once, not only the tracks opened after it.
func TestSetReplayGainAppliesNow(t *testing.T) {
	h := newHarness(t, nil) // ReplayGain off
	q := songs(2, 100)
	g, g2 := -6.0, -12.0
	q[0].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
	q[1].ReplayGain = &subsonic.ReplayGain{TrackGain: &g2}
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 90*time.Second) // near the end: the next song is prefetched
	h.waitFor("the successor queued", func() bool { return h.eng.queueCount() == 1 })
	h.p.SetReplayGain("track")
	if got, ok := h.eng.gainOf(a.ID); !ok || got < 0.50 || got > 0.51 {
		t.Fatalf("playing track's gain %v (set %v), want -6 dB", got, ok)
	}
	h.eng.mu.Lock()
	next := h.eng.queued[0].ID
	h.eng.mu.Unlock()
	if got, ok := h.eng.gainOf(next); !ok || got < 0.25 || got > 0.26 {
		t.Fatalf("queued track's gain %v (set %v), want -12 dB", got, ok)
	}
}
