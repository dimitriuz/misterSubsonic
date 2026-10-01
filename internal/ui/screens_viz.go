package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// The full-screen visualizer (spec §2.2): a screen pushed on Now Playing by
// Start. The picture fills the body; one small line in a corner names the
// song and moves to the next corner once a minute (burn-in); the hint bar
// shows only for a few seconds after a press.

const (
	vizHintShow    = 3 * time.Second // the hint bar stays this long after a press
	vizCornerEvery = time.Minute     // the info line moves to the next corner this often
)

type VizScreen struct {
	np        *NowPlayingScreen // the screen below: it handles every key but the ones here
	since     time.Time         // when the screen opened: the corner follows from it
	hintUntil time.Time
	armed     bool // a timer is waiting to hide the hint bar

	// The info line as last laid out, and what it was laid out from. Draw
	// only paints text and rect, so a partial frame equals a full one;
	// refresh changes them and damages both places.
	song   subsonic.ID
	sec    int
	corner int
	laid   bool
	text   string
	rect   gfx.Rect
}

func NewVizScreen(np *NowPlayingScreen) *VizScreen { return &VizScreen{np: np} }

func (s *VizScreen) Title() string { return "Now Playing" }

func (s *VizScreen) Enter(a *App) {
	s.since = a.o.Now()
	if a.VizStyle() == VizOff { // the point of the screen is the picture
		a.SetVizStyle(VizBars)
	}
	s.hintUntil = s.since.Add(vizHintShow)
	s.arm(a)
	s.refresh(a)
}

// canFullScreen reports whether Start on Now Playing opens the full screen:
// there is a sound source to draw and something playing.
func (a *App) canFullScreen() bool { return a.o.Visual != nil && a.playingSomething() }

// playingSomething reports whether a song is selected and the player has not
// stopped (the queue ended, or was cleared).
func (a *App) playingSomething() bool {
	return a.hasCurrent() && a.state().Status != player.Stopped
}

// vizHost is the full screen when it is on top, or under a menu opened from it.
func (a *App) vizHost() *VizScreen {
	switch t := a.Top().(type) {
	case *VizScreen:
		return t
	case *MenuScreen:
		v, _ := t.parent.(*VizScreen)
		return v
	}
	return nil
}

// vizBody is the full screen's area: the whole screen inside the title-safe
// lines. The hint bar is drawn over its bottom edge when it is up, so the
// picture doesn't change size with it.
func (a *App) vizBody() gfx.Rect { return gfx.R(0, a.P.SafeY, a.P.W, a.P.H-2*a.P.SafeY) }

// hintsUp reports whether the hint bar is drawn: always, except on the full
// screen, where it shows for vizHintShow after a press.
func (a *App) hintsUp() bool {
	if v, ok := a.Top().(*VizScreen); ok {
		return a.clock().Before(v.hintUntil)
	}
	return true
}

// vizCornerDue is when the info line moves next (zero: no full screen).
func (a *App) vizCornerDue() time.Time {
	v := a.vizHost()
	if v == nil {
		return time.Time{}
	}
	n := a.clock().Sub(v.since)/vizCornerEvery + 1
	return v.since.Add(n * vizCornerEvery)
}

// reveal brings the hint bar up for vizHintShow from now.
func (s *VizScreen) reveal(a *App) {
	if !a.hintsUp() {
		r := a.hintRect()
		a.Damage(gfx.R(r.X, r.Y, r.W, a.P.H-r.Y)) // drawHints paints down to the bottom edge
	}
	s.hintUntil = a.o.Now().Add(vizHintShow)
	s.arm(a)
}

// arm makes sure a timer fires when the bar is due to go (a press after
// it was set only moves hintUntil; the timer then waits again).
func (s *VizScreen) arm(a *App) {
	if s.armed {
		return
	}
	s.armed = true
	a.After(s, s.hintUntil.Sub(a.o.Now()), func() {
		s.armed = false
		if a.o.Now().Before(s.hintUntil) {
			s.arm(a)
		}
	})
}

func (s *VizScreen) Handle(a *App, e input.Event) bool {
	if e.Kind == input.Press {
		switch e.Button {
		case input.BtnB, input.BtnStart:
			a.Pop()
			return true
		case input.BtnY: // the queue opens over Now Playing, so Back returns there
			a.Pop()
			return s.np.Handle(a, e)
		case input.BtnQueue:
			a.Pop()
			return false // the app opens the queue
		}
	}
	s.reveal(a)
	return s.np.Handle(a, e)
}

// Release goes to Now Playing too (a short Select or X ends there); its menu
// opens over this screen.
func (s *VizScreen) Release(a *App, b input.Button) {
	s.np.host = s
	s.np.Release(a, b)
	s.np.host = nil
}

// Hints are Now Playing's, less Start, which leaves (Back says so).
func (s *VizScreen) Hints(a *App) []Hint {
	var hs []Hint
	for _, h := range s.np.Hints(a) {
		if h.Button != input.BtnStart {
			hs = append(hs, h)
		}
	}
	return hs
}

// Sync leaves when nothing plays any more, and keeps the info line current
// before each frame.
func (s *VizScreen) Sync(a *App) {
	if !a.playingSomething() {
		a.Pop()
		return
	}
	s.refresh(a)
}

// refresh lays out the info line (title, artist and elapsed time, in the
// corner for this minute) and damages the old and the new place when it
// changed: each second for the time, or when it moves. It builds nothing
// while the song, the second and the corner are the same.
func (s *VizScreen) refresh(a *App) {
	st := a.state()
	song, ok := st.Current()
	if !ok {
		return
	}
	now := a.clock()
	sec := int(s.np.position(a, st, now) / time.Second)
	corner := int(now.Sub(s.since)/vizCornerEvery) % 4
	if s.laid && song.ID == s.song && sec == s.sec && corner == s.corner {
		return
	}
	p, fs, body := a.P, a.F.Small, a.vizBody()
	pad := p.Margin / 2
	tail := "  " + clock(time.Duration(sec)*time.Second)
	head := song.Title
	if song.Artist != "" {
		head += " — " + song.Artist
	}
	head = fs.Truncate(head, body.W-2*p.Margin-2*pad-fs.Measure(tail))
	text := head + tail
	w, h := fs.Measure(text)+2*pad, fs.Height()+pad
	r := gfx.R(body.X+p.Margin, body.Y+p.Margin, w, h) // clockwise: top left, top right, bottom right, bottom left
	if corner == 1 || corner == 2 {
		r.X = body.Right() - p.Margin - w
	}
	if corner >= 2 {
		r.Y = body.Bottom() - p.Margin - a.hintH() - h
	}
	if s.laid {
		a.Damage(s.rect)
	}
	a.Damage(r)
	s.song, s.sec, s.corner, s.laid, s.text, s.rect = song.ID, sec, corner, true, text, r
}

func (s *VizScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	a.drawVizPanel(c, area)
	if s.text == "" {
		return
	}
	fs := a.F.Small
	pad := a.P.Margin / 2
	c.Fill(s.rect, colOverlay)
	fs.Draw(c, s.rect.X+pad, s.rect.Y+pad/2+fs.Ascent(), s.text, colText, s.rect)
}
