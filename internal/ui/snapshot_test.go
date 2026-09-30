package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// drifting is a player whose position moves on every read once armed, like
// the real one moving on its own goroutine between two reads of a frame.
type drifting struct {
	*fakePlayer
	armed bool
}

func (d *drifting) State() player.State {
	st := d.fakePlayer.State()
	if d.armed {
		d.fakePlayer.st.Position += time.Second
	}
	return st
}

// One frame reads the player once: the partial passes and the verify pass
// draw the same position even while the player keeps moving.
func TestFrameReadsThePlayerOnce(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	d := &drifting{fakePlayer: ta.pl}
	ta.o.Player = d
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumScreen(sampleLibrary().albums[0]))
	ta.settle(t)
	d.armed = true
	ta.press(input.BtnDown)
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.settle(t)
}
