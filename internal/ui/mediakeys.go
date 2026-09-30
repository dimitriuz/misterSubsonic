package ui

import (
	"time"

	"mistersubsonic/internal/input"
)

// mediaKey handles a multimedia keyboard's (or remote's) media keys, on
// every screen: volume (held to repeat), play/pause, next and previous
// track, and seeking. It reports whether e was one.
func (a *App) mediaKey(e input.Event) bool {
	if e.Kind == input.Release {
		switch e.Button {
		case input.BtnVolUp, input.BtnVolDown, input.BtnPlayPause, input.BtnNextTrack,
			input.BtnPrevTrack, input.BtnSeekFwd, input.BtnSeekBack:
			return true
		}
		return false
	}
	pl := a.Player()
	switch e.Button {
	case input.BtnVolUp, input.BtnVolDown:
		step := volStep
		if e.Button == input.BtnVolDown {
			step = -step
		}
		a.setVolume(a.volumeDB() + step)
	case input.BtnPlayPause:
		if pl != nil && e.Kind == input.Press {
			pl.TogglePause()
		}
	case input.BtnNextTrack, input.BtnPrevTrack:
		if pl != nil && e.Kind == input.Press && a.hasQueue() {
			if e.Button == input.BtnNextTrack {
				pl.Next()
			} else {
				pl.Prev()
			}
		}
	case input.BtnSeekFwd, input.BtnSeekBack:
		if pl != nil && a.hasCurrent() {
			st := pl.State()
			step := seekStep
			if e.Kind == input.Repeat {
				step = seekHoldStep
			}
			if e.Button == input.BtnSeekBack {
				step = -step
			}
			pos := st.Position + step
			if song, ok := st.Current(); ok && song.Duration > 0 {
				pos = min(pos, time.Duration(song.Duration)*time.Second-time.Second)
			}
			pl.Seek(max(pos, 0))
		}
	default:
		return false
	}
	a.dirty = true
	return true
}
