package ui

import (
	"time"

	"mistersubsonic/internal/input"
)

// isMediaButton reports whether b is one mediaKey handles.
func isMediaButton(b input.Button) bool {
	switch b {
	case input.BtnVolUp, input.BtnVolDown, input.BtnMute, input.BtnPlayPause,
		input.BtnNextTrack, input.BtnPrevTrack, input.BtnSeekFwd, input.BtnSeekBack:
		return true
	}
	return false
}

// mediaKey handles a multimedia keyboard's (or remote's) media keys, on
// every screen: volume (held to repeat), play/pause, next and previous
// track, and seeking. It reports whether e was one. Releases never get here.
func (a *App) mediaKey(e input.Event) bool {
	pl := a.Player()
	switch e.Button {
	case input.BtnVolUp, input.BtnVolDown:
		step := volStep
		if e.Button == input.BtnVolDown {
			step = -step
		}
		a.setVolume(a.volumeDB() + step)
		a.exact = true // only the volume panel changes
	case input.BtnMute:
		if e.Kind == input.Press {
			a.toggleMute()
		}
		a.exact = true // only the volume panel changes
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
		now := a.o.Now()
		if e.Kind == input.Repeat && now.Sub(a.mediaSeekAt) < seekEvery {
			break // a held key: at most one Seek per seekEvery (each can open a stream)
		}
		if pl != nil && a.hasCurrent() {
			a.mediaSeekAt = now
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
	return true // dispatch redraws what changed
}
