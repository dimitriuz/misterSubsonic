package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

const (
	seekStep     = 10 * time.Second
	seekHoldStep = 30 * time.Second
	// A held seek key sends at most one Seek per seekEvery (each Seek can
	// open a new transcoded stream); the rest accumulate in the target.
	seekEvery = 250 * time.Millisecond
	// The pending target stays on screen this long after its last Seek, until
	// the player's position catches up.
	seekShow = time.Second
)

// NowPlayingScreen is the full-screen player (spec §8.2).
type NowPlayingScreen struct {
	target   time.Duration // pending seek target
	song     subsonic.ID   // track the target belongs to
	unsent   bool          // target not yet sent to the player
	lastSeek time.Time
}

func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }

func (s *NowPlayingScreen) Title() string { return "Now Playing" }
func (s *NowPlayingScreen) Enter(a *App)  {}

// playModes is the Select cycle: (shuffle, repeat).
var playModes = []struct {
	shuffle bool
	repeat  player.Repeat
	label   string
}{
	{false, player.RepeatOff, "In order"},
	{true, player.RepeatOff, "Shuffle"},
	{false, player.RepeatAll, "Repeat all"},
	{false, player.RepeatOne, "Repeat one"},
}

// Release flushes a seek target the throttle held back.
func (s *NowPlayingScreen) Release(a *App, b input.Button) {
	if (b == input.BtnLeft || b == input.BtnRight) && s.unsent {
		s.send(a.Player(), a.o.Now())
		a.dirty = true
	}
}

func (s *NowPlayingScreen) curID(st player.State) subsonic.ID {
	song, _ := st.Current()
	return song.ID
}

func (s *NowPlayingScreen) send(pl Player, now time.Time) {
	pl.Seek(s.target)
	s.unsent, s.lastSeek = false, now
}

// clamp keeps a target inside the track, a second short of the end.
func (s *NowPlayingScreen) clamp(t time.Duration, st player.State) time.Duration {
	song, _ := st.Current()
	if d := secs(song.Duration); d > 0 {
		t = min(t, d-time.Second)
	}
	return max(t, 0)
}

// position is where playback is, or is about to be after a recent seek.
func (s *NowPlayingScreen) position(a *App, st player.State, now time.Time) time.Duration {
	if s.song != "" && s.song == s.curID(st) && (s.unsent || now.Sub(s.lastSeek) < seekShow) {
		return s.target
	}
	return st.Position
}

func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
	pl := a.Player()
	st := pl.State()
	switch e.Button {
	case input.BtnLeft, input.BtnRight:
		step := seekStep
		if e.Kind == input.Repeat {
			step = seekHoldStep
		}
		if e.Button == input.BtnLeft {
			step = -step
		}
		now := a.o.Now()
		s.target = s.clamp(s.position(a, st, now)+step, st)
		s.song, s.unsent = s.curID(st), true
		if e.Kind == input.Press || now.Sub(s.lastSeek) >= seekEvery {
			s.send(pl, now)
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		pl.TogglePause()
	case input.BtnL:
		pl.Prev()
	case input.BtnR:
		pl.Next()
	case input.BtnY:
		a.Push(NewQueueScreen())
	case input.BtnSelect:
		cur := 0
		for i, m := range playModes {
			if m.shuffle == st.Shuffle && m.repeat == st.Repeat {
				cur = i
			}
		}
		m := playModes[(cur+1)%len(playModes)]
		pl.SetShuffle(m.shuffle)
		pl.SetRepeat(m.repeat)
		a.Toast("%s", m.label)
	default:
		return false
	}
	return true
}

func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	st := a.Player().State()
	song, ok := st.Current()
	if !ok {
		a.drawCentered(c, area, "Nothing playing", colDim)
		return
	}
	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
	barH := max(p.Margin/6, 3)
	// Height of the text block, used to centre everything vertically.
	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + fs.Height()
	art := min(p.ArtNow, area.H-2*p.Margin)
	var text gfx.Rect
	if p.W >= p.H*3/2 { // wide: art left, text right, both centred
		a.drawArt(c, song.CoverArt, gfx.R(area.X+p.Margin*2, area.Y+(area.H-art)/2, art, art))
		x := area.X + p.Margin*3 + art
		text = gfx.R(x, area.Y+(area.H-max(art, textH))/2, area.Right()-p.Margin*2-x, max(art, textH))
	} else { // narrow (CRT): art on top, text below, centred as a block
		art = min(art, area.H-textH-2*p.Margin)
		top := area.Y + (area.H-art-p.Margin/2-textH)/2
		a.drawArt(c, song.CoverArt, gfx.R(area.X+(area.W-art)/2, top, art, art))
		text = gfx.R(area.X+p.Margin, top+art+p.Margin/2, area.W-2*p.Margin, textH)
	}
	y := text.Y
	line := func(f *gfx.Font, s string, col gfx.Color) {
		f.Draw(c, text.X, y+f.Ascent(), f.Truncate(s, text.W), col, c.Bounds())
		y += f.Height()
	}
	line(ft, song.Title, colText)
	line(fb, song.Artist, colDim)
	line(fb, song.Album, colDim)
	y += p.Margin / 2
	line(fs, quality(song, st.Transcoded), colAccent)

	// Progress.
	y += p.Margin / 2
	d := secs(song.Duration)
	pos := s.position(a, st, a.o.Now())
	c.Fill(gfx.R(text.X, y, text.W, barH), colArtBg)
	if d > 0 {
		c.Fill(gfx.R(text.X, y, progressW(text.W, pos, d), barH), colAccent)
	}
	y += barH + p.Margin/4
	times := clock(pos)
	if d > 0 {
		times += " / " + clock(d)
	}
	fs.Draw(c, text.X, y+fs.Ascent(), times, colDim, c.Bounds())
	y += fs.Height() + p.Margin/2

	mode := statusLabel(st.Status)
	for _, m := range playModes {
		if m.shuffle == st.Shuffle && m.repeat == st.Repeat && m.label != "In order" {
			mode += "  ·  " + m.label
		}
	}
	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
	y += fb.Height()
	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
		next := st.Queue[st.NextIndex]
		line(fs, "Next: "+next.Title+" — "+next.Artist, colDim)
	}
}

// QueueScreen lists the play queue.
type QueueScreen struct {
	list    List
	focused bool // focus placed on the current song once
}

func NewQueueScreen() *QueueScreen { return &QueueScreen{} }

func (s *QueueScreen) Title() string { return "Queue" }
func (s *QueueScreen) Enter(a *App)  {}

func (s *QueueScreen) Handle(a *App, e input.Event) bool {
	st := a.Player().State()
	if s.list.Handle(e, len(st.Queue)) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	if e.Button == input.BtnY {
		a.Pop()
		if _, ok := a.Top().(*NowPlayingScreen); !ok {
			a.Push(NewNowPlayingScreen())
		}
		return true
	}
	if len(st.Queue) == 0 {
		return false
	}
	if (e.Button == input.BtnA || e.Button == input.BtnX) && s.list.Focus >= len(st.Queue) {
		s.list.Focus = len(st.Queue) - 1
		return true
	}
	switch e.Button {
	case input.BtnA:
		a.Player().Jump(s.list.Focus)
		a.Pop()
		return true
	case input.BtnX:
		title := st.Queue[s.list.Focus].Title
		a.Player().Remove(s.list.Focus)
		a.Toast("Removed %s", title)
		return true
	}
	return false
}

func (s *QueueScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	st := a.Player().State()
	if len(st.Queue) == 0 {
		a.drawCentered(c, area, "The queue is empty", colDim)
		return
	}
	if !s.focused && st.Index >= 0 {
		s.list.Focus, s.focused = st.Index, true
	}
	s.list.Draw(c, area, len(st.Queue), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		so := st.Queue[i]
		col := colText
		if i == st.Index {
			col = colAccent
		}
		a.drawTextRow(c, r, so.CoverArt, true, so.Title, so.Artist+" · "+clock(secs(so.Duration)), col)
	})
}

// MessageScreen shows a problem (no config, server unreachable) with an
// optional A-to-retry action.
type MessageScreen struct {
	title, message string
	retry          func()
}

func NewMessageScreen(title, message string, retry func()) *MessageScreen {
	return &MessageScreen{title: title, message: message, retry: retry}
}

func (s *MessageScreen) Title() string { return s.title }
func (s *MessageScreen) Enter(a *App)  {}

func (s *MessageScreen) Handle(a *App, e input.Event) bool {
	if e.Kind == input.Press && e.Button == input.BtnA && s.retry != nil {
		s.retry()
		return true
	}
	return false
}

func (s *MessageScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	y := area.Y + p.Margin
	for _, para := range wrap(f, s.message, area.W-2*p.Margin) {
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), para, colText, area)
		y += f.Height()
	}
	if s.retry != nil {
		y += p.Margin
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), "Press A to try again", colAccent, area)
	}
}

// wrap breaks text into lines no wider than w (explicit newlines kept).
func wrap(f *gfx.Font, text string, w int) []string {
	var out []string
	for _, para := range splitLines(text) {
		line := ""
		for _, word := range splitWords(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if f.Measure(try) <= w || line == "" {
				line = try
				continue
			}
			out = append(out, line)
			line = word
		}
		out = append(out, line)
	}
	return out
}
