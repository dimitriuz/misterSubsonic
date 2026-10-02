package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/remote"
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
	volStep  = 1.0 // dB per Up/Down
	// Select held this long mutes (a shorter press changes the visualizer).
	muteHold = time.Second
	// X held this long stars or unstars the song (a shorter press opens the menu).
	starHold = time.Second
)

// NowPlayingScreen is the full-screen player (spec §8.2).
type NowPlayingScreen struct {
	target   time.Duration // pending seek target
	song     subsonic.ID   // track the target belongs to
	unsent   bool          // target not yet sent to the player
	lastSeek time.Time

	selDown  bool // Select is held
	selMuted bool // ...and has already muted: its release does nothing
	selHold  int  // counts holds, so an old timer can't fire in a newer one

	xDown    bool // X is held
	xStarred bool // ...and has already starred: its release does nothing
	xHold    int  // like selHold

	host Screen // the full screen while it forwards a release here: menus open over it (nil: this screen)
}

func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }

func (s *NowPlayingScreen) Title() string { return "Now Playing" }
func (s *NowPlayingScreen) Enter(a *App)  {}

// Release flushes a seek target the throttle held back, and ends a Select
// or X press: a short Select changes the visualizer, a short X opens the menu.
func (s *NowPlayingScreen) Release(a *App, b input.Button) {
	if b == input.BtnSelect && s.selDown {
		muted := s.selMuted
		s.selDown, s.selMuted = false, false
		if !muted {
			next := (a.VizStyle() + 1) % VizStyle(len(vizNames))
			if next == VizOff && s.host != nil {
				next = VizBars // an empty full screen is no use: it cycles through the four pictures
			}
			a.SetVizStyle(next)
			a.Toast("Visualizer: %s", next.Label())
		}
		return
	}
	if b == input.BtnX && s.xDown {
		starred := s.xStarred
		s.xDown, s.xStarred = false, false
		if !starred {
			var parent Screen = s
			if s.host != nil {
				parent = s.host
			}
			var entries []menuEntry
			if e, ok := s.starEntry(a); ok {
				entries = append(entries, e)
			}
			a.Push(NewMenuScreen(parent, "Now Playing", append(entries, modeEntries(a)...)))
		}
		return
	}
	if (b == input.BtnLeft || b == input.BtnRight || b == input.BtnSeekBack || b == input.BtnSeekFwd) && s.unsent && s.song == s.curID(a.Player().State()) {
		s.send(a, a.Player(), a.o.Now())
		a.dirty = true
	}
}

// settle ends a seek gesture: any unsent target goes to the player (before
// the button's own action, so it applies to the track it was aimed at) and
// the pending target is forgotten, so it can't outlive the hold.
func (s *NowPlayingScreen) settle(a *App, st player.State) {
	if s.unsent && s.song == s.curID(st) {
		s.send(a, a.Player(), a.o.Now())
	}
	s.unsent, s.song = false, ""
}

// starEntry is the menu's Star or Unstar for the current song.
func (s *NowPlayingScreen) starEntry(a *App) (menuEntry, bool) {
	song, ok := a.state().Current()
	if !ok {
		return menuEntry{}, false // nothing to star
	}
	label := "Star"
	if a.isStarred(songStar(song)) {
		label = "Unstar"
	}
	return menuEntry{label, func(a *App) { a.toggleStar(songStar(song)) }}, true
}

// modeEntries are the Shuffle and Repeat menu entries (Now Playing's and the
// queue's), labelled with the current state.
func modeEntries(a *App) []menuEntry {
	st := a.state()
	shuffle, repeat := "Shuffle: Off", "Repeat: Off"
	next := player.RepeatAll
	if st.Shuffle {
		shuffle = "Shuffle: On"
	}
	switch st.Repeat {
	case player.RepeatAll:
		repeat, next = "Repeat: All", player.RepeatOne
	case player.RepeatOne:
		repeat, next = "Repeat: One", player.RepeatOff
	}
	return []menuEntry{
		{shuffle, func(a *App) {
			a.Player().SetShuffle(!st.Shuffle)
			a.notifyRemote(remote.StateChanged)
			a.dirty = true // the mode label changes without a player event
		}},
		{repeat, func(a *App) {
			a.Player().SetRepeat(next)
			a.notifyRemote(remote.StateChanged)
			a.dirty = true
		}},
	}
}

func (s *NowPlayingScreen) curID(st player.State) subsonic.ID {
	song, _ := st.Current()
	return song.ID
}

func (s *NowPlayingScreen) send(a *App, pl Player, now time.Time) {
	pl.Seek(s.target)
	a.notifyRemote(remote.StateChanged) // a seek sends no player event
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
	case input.BtnSeekBack, input.BtnSeekFwd:
		if !a.hasCurrent() {
			return false
		}
		fallthrough
	case input.BtnLeft, input.BtnRight:
		step := seekStep
		if e.Kind == input.Repeat {
			step = seekHoldStep
		}
		if e.Button == input.BtnLeft || e.Button == input.BtnSeekBack {
			step = -step
		}
		now := a.o.Now()
		s.target = s.clamp(s.position(a, st, now)+step, st)
		s.song, s.unsent = s.curID(st), true
		if e.Kind == input.Press || now.Sub(s.lastSeek) >= seekEvery {
			s.send(a, pl, now)
		}
		return true
	}
	s.settle(a, st)
	switch e.Button {
	case input.BtnUp, input.BtnDown: // volume, held to repeat
		step := volStep
		if e.Button == input.BtnDown {
			step = -step
		}
		a.setVolume(st.VolumeDB + step) // and saved to the config
		a.exact = true                  // Now Playing shows no volume: only the panel changes
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		pl.TogglePause()
	case input.BtnX: // every press starts a fresh hold (a lost release is recovered): short opens the menu, long stars
		s.xDown, s.xStarred = true, false
		s.xHold++
		hold := s.xHold
		a.After(s, starHold, func() {
			if s.xDown && s.xHold == hold && a.onNowPlaying(s) {
				if song, ok := a.state().Current(); ok {
					s.xStarred = true
					a.toggleStar(songStar(song))
				}
			}
		})
	case input.BtnL:
		pl.Prev()
	case input.BtnR:
		pl.Next()
	case input.BtnY:
		a.Push(NewQueueScreen())
	case input.BtnStart: // the full screen (elsewhere Start pauses)
		if !a.canFullScreen() {
			return false
		}
		a.Push(NewVizScreen(s))
	case input.BtnSelect: // every press starts a fresh hold (a lost release is recovered): short changes the visualizer, long mutes
		s.selDown, s.selMuted = true, false
		s.selHold++
		hold := s.selHold
		a.After(s, muteHold, func() {
			if s.selDown && s.selHold == hold && a.onNowPlaying(s) {
				s.selMuted = true
				a.toggleMute()
			}
		})
	default:
		return false
	}
	return true
}

// onNowPlaying reports whether s is what the user is looking at: on top, or
// under its full screen.
func (a *App) onNowPlaying(s *NowPlayingScreen) bool {
	if v, ok := a.Top().(*VizScreen); ok {
		return v.np == s
	}
	return a.Top() == Screen(s)
}

func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	st := a.state()
	song, ok := st.Current()
	if !ok {
		a.drawCentered(c, area, "Nothing playing", colDim)
		return
	}
	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
	barH := max(p.Margin/6, 3)
	// Height of the text block, used to centre everything vertically.
	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + 2*fs.Height()
	// With the visualizer on, the panel is part of the block when the cover
	// is too short to hold it beside the text (a CRT).
	vizGapY := p.Margin / 4
	if a.vizShown() {
		textH += vizGapY + max(p.Margin*5/4, 20)
	}
	// Art left, text right, centred as a block. The gap is narrower on a
	// CRT, where the text needs the width.
	gap := 2 * p.Margin
	art := min(p.ArtNow, area.H-2*p.Margin)
	if p.W < p.H*3/2 {
		gap = p.Margin
		art = min(art, area.W*2/5)
	}
	a.drawArt(c, song.CoverArt, gfx.R(area.X+gap, area.Y+(area.H-art)/2, art, art))
	x := area.X + gap + art + p.Margin
	text := gfx.R(x, area.Y+(area.H-max(art, textH))/2, area.Right()-gap-x, max(art, textH))
	y := text.Y
	line := func(f *gfx.Font, s string, col gfx.Color) {
		f.Draw(c, text.X, y+f.Ascent(), f.Truncate(s, text.W), col, c.Bounds())
		y += f.Height()
	}
	title := song.Title
	if a.isStarred(songStar(song)) {
		st := ft.Ascent() * 3 / 4
		drawIcon(c, iconStar, gfx.R(text.Right()-st, y+(ft.Ascent()-st)/2+ft.Descent()/2, st, st), colAccent)
		ft.Draw(c, text.X, y+ft.Ascent(), ft.Truncate(title, text.W-st-p.Margin/2), colText, c.Bounds())
		y += ft.Height()
	} else {
		line(ft, title, colText)
	}
	line(fb, song.Artist, colDim)
	line(fb, song.Album, colDim)
	y += p.Margin / 2
	line(fs, quality(song, st.Transcoded), colAccent)

	// Progress.
	y += p.Margin / 2
	d := secs(song.Duration)
	pos := s.position(a, st, a.clock())
	a.markTick(gfx.R(text.X, y, text.W, barH))
	c.Fill(gfx.R(text.X, y, text.W, barH), colArtBg)
	if d > 0 {
		c.Fill(gfx.R(text.X, y, progressW(text.W, pos, d), barH), colAccent)
	}
	y += barH + p.Margin/4
	times := clock(pos)
	if d > 0 {
		times += " / " + clock(d)
	}
	a.markTick(gfx.R(text.X, y, text.W, fs.Height()))
	fs.Draw(c, text.X, y+fs.Ascent(), times, colDim, c.Bounds())
	y += fs.Height() + p.Margin/2

	mode := statusLabel(st.Status)
	if st.Shuffle {
		mode += "  ·  Shuffle"
	}
	switch st.Repeat {
	case player.RepeatAll:
		mode += "  ·  Repeat all"
	case player.RepeatOne:
		mode += "  ·  Repeat one"
	}
	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
	y += fb.Height()
	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
		next := st.Queue[st.NextIndex]
		line(fs, "Next: "+next.Title+" — "+next.Artist, colDim)
	}
	if a.insecure {
		line(fs, "insecure: certificate not checked", colError)
	}
	// The visualizer takes the rest of the column, down to the cover's bottom
	// edge (or the text block's, when that is lower).
	a.drawVizPanel(c, gfx.R(text.X, y+vizGapY, text.W, text.Bottom()-y-vizGapY))
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
		a.moved(s.list.Moved())
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
		i := s.list.Focus
		title := st.Queue[i].Title
		a.Push(NewMenuScreen(s, "Queue", append([]menuEntry{
			{"Remove " + title, func(a *App) {
				a.Player().Remove(i)
				a.Toast("Removed %s", title)
			}},
			{"Clear queue", func(a *App) {
				a.Player().Clear()
				for len(a.stack) > 1 && isPlayScreen(a.Top()) {
					a.Pop() // nothing left to show here or in Now Playing
				}
				a.Toast("Queue cleared")
			}},
		}, modeEntries(a)...)))
		return true
	}
	return false
}

func (s *QueueScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	st := a.state()
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

func isPlayScreen(s Screen) bool {
	switch s.(type) {
	case *QueueScreen, *NowPlayingScreen:
		return true
	}
	return false
}

// MessageScreen shows a problem (no config, server unreachable) with an
// optional A-to-retry action.
type MessageScreen struct {
	title, message string
	retry          func()
	label          string // what A does
}

func NewMessageScreen(title, message string, retry func()) *MessageScreen {
	return &MessageScreen{title: title, message: message, retry: retry, label: "Press A to try again"}
}

// NewMessageAction is a message whose A action is described by label.
func NewMessageAction(title, message, label string, action func()) *MessageScreen {
	return &MessageScreen{title: title, message: message, retry: action, label: label}
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
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), s.label, colAccent, area)
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
