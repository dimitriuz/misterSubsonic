package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// Library is the server API the screens use (subset of *subsonic.Client).
type Library interface {
	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
	GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error)
	GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error)
	GetGenres(ctx context.Context) ([]subsonic.Genre, error)
	GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error)
	GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error)
	GetStarred2(ctx context.Context) (*subsonic.Starred, error)
	Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error)
	Star(ctx context.Context, t subsonic.StarTarget) error
	Unstar(ctx context.Context, t subsonic.StarTarget) error
}

// Player is the playback API the screens use (subset of *player.Player).
type Player interface {
	State() player.State
	Events() <-chan player.Event
	PlayNow(songs []subsonic.Song, start int)
	PlayNext(songs []subsonic.Song)
	Enqueue(songs []subsonic.Song)
	Clear()
	SetVolumeDB(db float64)
	SetMuted(on bool)
	SetReplayGain(mode string)
	SetScrobble(on bool)
	TogglePause()
	Next()
	Prev()
	Seek(pos time.Duration)
	Jump(i int)
	Remove(i int)
	SetShuffle(on bool)
	SetRepeat(r player.Repeat)
	Resumable(ctx context.Context) (*player.Resume, error)
	ResumeFrom(r *player.Resume)
}

// ArtSource returns cover images already in memory, queueing a load otherwise.
type ArtSource interface {
	Get(k art.Key) (*gfx.Image, bool)
}

// Screen is one page of the UI.
type Screen interface {
	Title() string
	// Enter runs when the screen is pushed; start loads here.
	Enter(a *App)
	// Handle gets Press and Repeat events; return false to let the app
	// apply global keys (B = back, Y = Now Playing, Start = play/pause).
	Handle(a *App, e input.Event) bool
	Draw(a *App, c *gfx.Canvas, area gfx.Rect)
}

// TextInput is a screen that takes typed characters (physical keyboards).
// Text returns false to let the key act as its button instead (Backspace
// in an empty field goes back).
type TextInput interface {
	Text(a *App, r rune) bool
}

// owner is a screen that shows other screens inside itself (the HDMI root
// shows the selected section); their loads run under the owner's entry.
type owner interface {
	Owns(s Screen) bool
}

type Options struct {
	Display       gfx.Display
	Profile       Profile
	Library       Library
	Player        Player
	Art           ArtSource
	Inputs        []<-chan input.Event
	FallbackFonts string
	Now           func() time.Time

	// ConfigPath is where the configuration is saved. Config is the loaded
	// one (nil if missing or invalid: ConfigErr says why); the UI owns it.
	ConfigPath string
	Config     *config.Config
	ConfigErr  error
	AudioErr   error  // the sound device couldn't be opened
	Version    string // shown in Settings → About
	// Connect starts a connection to cfg's active server, off the UI
	// goroutine, replacing any previous one; it answers with
	// a.Connected or a.ConnectFailed (through a.Post).
	Connect func(a *App, cfg *config.Config)
}

// Fonts used by the screens.
type Fonts struct {
	Title, Body, Small *gfx.Font
}

type screenEntry struct {
	s      Screen
	ctx    context.Context
	cancel context.CancelFunc
}

type toast struct {
	text  string
	until time.Time
}

type timer struct {
	at    time.Time
	owner Screen
	f     func()
}

// marquee tracks the one focused text that scrolls because it doesn't fit.
type marquee struct {
	text  string
	since time.Time
	seen  bool // drawn in the current frame
}

const (
	toastTime    = 3 * time.Second
	maxToasts    = 3
	exitHold     = 2 * time.Second
	progressTick = 500 * time.Millisecond
	marqueeDelay = 1200 * time.Millisecond // before a focused long title starts to scroll
	marqueeFrame = 100 * time.Millisecond
	marqueeGap   = "     "
)

// App owns the screen stack and the event loop. All methods except Post
// must be called on the UI goroutine.
type App struct {
	volumePending bool // the volume changed with no player: apply it on the next Connected
	o             Options
	P             Profile
	F             Fonts
	canvas        *gfx.Canvas
	scaler        *gfx.Scaler
	stack         []screenEntry
	toasts        []toast
	timers        []timer
	mq            marquee
	animate       bool // something on screen moves (marquee): redraw soon
	// mqWake is when a focused long title starts to scroll (zero if none
	// waits): the one wake needed during the pre-scroll delay.
	mqWake time.Time
	// dim is set while drawing what doesn't have the focus (behind a menu,
	// the sidebar's neighbour, dimmed search results): nothing scrolls there.
	dim bool
	// swallowed holds buttons whose press was typed into a text field, so
	// their releases are dropped too.
	swallowed  map[input.Button]bool
	insecure   bool
	stars      map[starKey]bool       // star changes made in this session
	starBusy   map[starKey]bool       // star requests in flight
	starGen    int                    // bumped by every successful star change
	artists    []subsonic.ArtistIndex // getArtists, fetched once per connection
	cfg        *config.Config
	conn       ConnInfo
	saveAt     time.Time  // a debounced config save is due (zero: none)
	saving     bool       // a save is being written
	saveAgain  bool       // the config changed during that write
	saveMu     sync.Mutex // one writer of the config file at a time
	lastInput  time.Time  // for the screensaver
	saver      bool       // the screensaver is on
	saverSince time.Time
	rep        input.Repeater
	in         chan input.Event
	post       chan func()
	loads      int
	dirty      bool
	quit       bool
	bDown      time.Time // when B went down on the root screen (zero if not held)
	confirm    bool      // exit confirmation shown

	muted       bool      // the sound is off (not saved: the app starts with sound)
	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
	overwritten bool      // the last check found the screen drawn over
}

func New(o Options) (*App, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
	if err != nil {
		return nil, err
	}
	bold, err := gfx.LoadTypeface(true, o.FallbackFonts)
	if err != nil {
		return nil, err
	}
	if a.F.Title, err = bold.Face(a.P.Title); err != nil {
		return nil, err
	}
	if a.F.Body, err = regular.Face(a.P.Body); err != nil {
		return nil, err
	}
	if a.F.Small, err = regular.Face(a.P.Small); err != nil {
		return nil, err
	}
	a.canvas = gfx.NewCanvas(a.P.W, a.P.H)
	pw, ph := o.Display.Size()
	if pw != a.P.W || ph != a.P.H { // drawn at the display's size: nothing to scale
		a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
	}
	if _, ok := o.Display.(gfx.Checker); ok {
		a.checkAt = o.Now().Add(watchdogEvery)
	}
	for _, ch := range o.Inputs {
		go func(ch <-chan input.Event) {
			for e := range ch {
				a.in <- e
			}
		}(ch)
	}
	return a, nil
}

// Attach sets the library, player and art source once the server connection
// exists (the UI starts before it, to show "Connecting…" and errors).
// Call on the UI goroutine.
func (a *App) Attach(lib Library, pl Player, art ArtSource) {
	a.o.Library, a.o.Player, a.o.Art = lib, pl, art
	a.dirty = true
}

// SetInsecure shows the "insecure" badge (the server's certificate isn't
// checked: insecure_skip_verify). Call on the UI goroutine.
func (a *App) SetInsecure(on bool) { a.insecure, a.dirty = on, true }

// Post runs f on the UI goroutine. Safe from any goroutine.
func (a *App) Post(f func()) {
	select {
	case a.post <- f:
	default:
		go func() { a.post <- f }() // never block a worker on a busy UI
	}
}

// Redraw marks the frame dirty.
func (a *App) Redraw() { a.dirty = true }

// ArtReady is the art loader's Ready callback: redraw when a cover arrives.
func (a *App) ArtReady(art.Key) { a.Post(a.Redraw) }

func (a *App) Library() Library { return a.o.Library }
func (a *App) Player() Player   { return a.o.Player }

// Art returns a cover at size px if loaded.
func (a *App) Art(id subsonic.ID, px int) (*gfx.Image, bool) {
	if a.o.Art == nil || id == "" {
		return nil, false
	}
	return a.o.Art.Get(art.Key{ID: id, Size: px})
}

// Push opens a screen on top.
func (a *App) Push(s Screen) {
	ctx, cancel := context.WithCancel(context.Background())
	a.stack = append(a.stack, screenEntry{s, ctx, cancel})
	a.bDown = time.Time{}
	a.dirty = true
	s.Enter(a)
}

// Pop closes the top screen (never the last one) and cancels its loads.
// The screen shown again is told (Shown), so it can refresh.
func (a *App) Pop() {
	if len(a.stack) <= 1 {
		return
	}
	top := a.stack[len(a.stack)-1]
	top.cancel()
	a.stack = a.stack[:len(a.stack)-1]
	a.dirty = true
	if sh, ok := a.Top().(shower); ok {
		sh.Shown(a)
	}
}

// popTo pops screens until pred matches the top; if no screen in the stack
// matches, the stack is left unchanged and false is returned.
func (a *App) popTo(pred func(Screen) bool) bool {
	for i := len(a.stack) - 1; i >= 0; i-- {
		if pred(a.stack[i].s) {
			for len(a.stack) > i+1 {
				a.Pop()
			}
			return true
		}
	}
	return false
}

// Replace swaps the whole stack for one root screen.
func (a *App) Replace(s Screen) {
	for _, e := range a.stack {
		e.cancel()
	}
	a.stack = nil
	a.Push(s)
}

// Top is the visible screen.
func (a *App) Top() Screen {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1].s
}

// entry finds the stack entry of s, or of the screen that shows s inside itself.
func (a *App) entry(s Screen) *screenEntry {
	for i := range a.stack {
		if a.stack[i].s == s {
			return &a.stack[i]
		}
		if o, ok := a.stack[i].s.(owner); ok && o.Owns(s) {
			return &a.stack[i]
		}
	}
	return nil
}

// Load runs fn off the UI goroutine and delivers its result to done on the
// UI goroutine, unless screen s has been popped by then.
func (a *App) Load(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) {
	a.LoadCancel(s, fn, done)
}

// LoadCancel is Load that can be called off: after cancel, fn's context is
// done and done is never called (search cancels the previous query).
func (a *App) LoadCancel(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) (cancel func()) {
	e := a.entry(s)
	if e == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(e.ctx)
	a.loads++
	go func() {
		v, err := fn(ctx)
		a.Post(func() {
			a.loads--
			if ctx.Err() == nil {
				done(v, err)
				a.dirty = true
			}
			cancel()
		})
	}()
	return cancel
}

// After runs f on the UI goroutine after d, unless owner has been popped.
func (a *App) After(owner Screen, d time.Duration, f func()) {
	a.timers = append(a.timers, timer{a.o.Now().Add(d), owner, f})
}

// Toast shows a short message over the current screen.
func (a *App) Toast(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	until := a.o.Now().Add(toastTime)
	if n := len(a.toasts); n > 0 && a.toasts[n-1].text == text {
		a.toasts[n-1].until = until
	} else {
		a.toasts = append(a.toasts, toast{text, until})
		if len(a.toasts) > maxToasts {
			a.toasts = a.toasts[len(a.toasts)-maxToasts:]
		}
	}
	a.dirty = true
}

// Run drives the UI until ctx ends or the user exits. A pending
// configuration change is saved before it returns.
func (a *App) Run(ctx context.Context) error {
	defer a.flushConfig()
	a.start()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for !a.quit {
		if a.dirty {
			if err := a.render(); err != nil {
				return err
			}
		}
		timer.Reset(a.untilWake())
		var pevents <-chan player.Event // nil until a player is attached
		if a.o.Player != nil {
			pevents = a.o.Player.Events()
		}
		select {
		case <-ctx.Done():
			return nil
		case e := <-a.in:
			a.onInput(e)
		case ev := <-pevents:
			a.onPlayer(ev)
		case f := <-a.post:
			f()
		case <-timer.C:
			a.onWake()
		}
	}
	return nil
}

func (a *App) untilWake() time.Duration {
	now := a.o.Now()
	next := now.Add(time.Hour)
	consider := func(t time.Time) {
		if !t.IsZero() && t.Before(next) {
			next = t
		}
	}
	consider(a.rep.NextDeadline())
	for _, t := range a.toasts {
		consider(t.until)
	}
	for _, t := range a.timers {
		consider(t.at)
	}
	if a.animate {
		consider(now.Add(marqueeFrame))
	}
	consider(a.mqWake)
	consider(a.saveAt)
	consider(a.checkAt)
	if !a.volumeUntil.IsZero() {
		consider(a.volumeUntil)
	}
	consider(a.saverDue())
	if a.saver {
		consider(now.Add(saverStep)) // the drift; nothing else moves
	}
	if !a.bDown.IsZero() {
		consider(a.bDown.Add(exitHold))
	}
	if a.o.Player != nil && a.o.Player.State().Status == player.Playing && !a.saver {
		consider(now.Add(progressTick))
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return time.Millisecond
}

func (a *App) onWake() {
	now := a.o.Now()
	for _, e := range a.rep.Due(now) {
		a.dispatch(e)
	}
	kept := a.toasts[:0]
	for _, t := range a.toasts {
		if now.Before(t.until) {
			kept = append(kept, t)
		} else {
			a.dirty = true
		}
	}
	a.toasts = kept
	var due []timer
	pending := a.timers[:0]
	for _, t := range a.timers {
		if now.Before(t.at) {
			pending = append(pending, t)
		} else {
			due = append(due, t)
		}
	}
	a.timers = pending
	for _, t := range due {
		if a.entry(t.owner) != nil {
			t.f()
			a.dirty = true
		}
	}
	if due := a.saverDue(); !due.IsZero() && !now.Before(due) {
		a.saver, a.saverSince, a.dirty = true, now, true
	}
	if a.saver {
		a.dirty = true // the drift
	}
	if !a.saveAt.IsZero() && !now.Before(a.saveAt) {
		a.saveAt = time.Time{}
		a.saveConfig()
	}
	a.checkScreen(now)
	if !a.volumeUntil.IsZero() && !now.Before(a.volumeUntil) {
		a.volumeUntil, a.dirty = time.Time{}, true // the panel goes
	}
	if a.animate || (!a.mqWake.IsZero() && !now.Before(a.mqWake)) {
		a.dirty = true
	}
	if !a.bDown.IsZero() && len(a.stack) == 1 && !now.Before(a.bDown.Add(exitHold)) {
		a.bDown = time.Time{}
		a.confirm = true
		a.dirty = true
	}
	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
		a.dirty = true // progress
	}
}

func (a *App) onPlayer(ev player.Event) {
	a.dirty = true
	if ev.Kind == player.Error {
		title := ev.Song.Title
		if title == "" {
			title = "track"
		}
		a.Toast("Can't play %s: %s", title, subsonic.Classify(ev.Err))
	}
}

func (a *App) onInput(e input.Event) {
	now := a.o.Now()
	a.lastInput = now
	if a.wake() && e.Kind == input.Press {
		return // the press that wakes the screensaver does nothing else
	}
	if e.Rune != 0 && !a.confirm {
		if e.Kind == input.Press {
			if t, ok := a.Top().(TextInput); ok && t.Text(a, e.Rune) {
				a.dirty = true
				if e.Button != input.BtnNone {
					a.swallowed[e.Button] = true
				}
				return
			}
		} else if a.swallowed[e.Button] {
			delete(a.swallowed, e.Button)
			return
		}
	}
	if e.Button == input.BtnNone {
		return
	}
	a.rep.Feed(e, now)
	if e.Button == input.BtnB {
		if e.Kind == input.Press && len(a.stack) == 1 && !a.confirm {
			a.bDown = now
		} else if e.Kind == input.Release {
			a.bDown = time.Time{}
		}
	}
	if e.Kind != input.Release {
		a.dispatch(e)
	} else if r, ok := a.Top().(interface{ Release(*App, input.Button) }); ok && !a.confirm {
		r.Release(a, e.Button)
	}
}

func (a *App) dispatch(e input.Event) {
	a.dirty = true
	if a.confirm {
		switch e.Button {
		case input.BtnA:
			a.quit = true
		case input.BtnB:
			a.confirm = false
		}
		return
	}
	if top := a.Top(); top != nil && top.Handle(a, e) {
		return
	}
	if a.mediaKey(e) {
		return
	}
	if e.Kind != input.Press {
		return
	}
	switch e.Button {
	case input.BtnB:
		a.Pop()
	case input.BtnStart:
		if a.o.Player != nil {
			a.o.Player.TogglePause()
		}
	case input.BtnY:
		if !a.hasQueue() {
			break
		}
		if _, ok := a.Top().(*NowPlayingScreen); ok {
			break
		}
		if !a.popTo(func(s Screen) bool { _, ok := s.(*NowPlayingScreen); return ok }) {
			a.Push(NewNowPlayingScreen())
		}
	case input.BtnMute:
		a.toggleMute()
	case input.BtnQueue:
		if !a.hasQueue() {
			break
		}
		if _, ok := a.Top().(*QueueScreen); ok {
			break
		}
		if !a.popTo(func(s Screen) bool { _, ok := s.(*QueueScreen); return ok }) {
			a.Push(NewQueueScreen())
		}
	}
}

func (a *App) hasQueue() bool {
	return a.o.Player != nil && len(a.o.Player.State().Queue) > 0
}

// hasCurrent reports whether a song is selected (the mini bar shows it).
func (a *App) hasCurrent() bool {
	if a.o.Player == nil {
		return false
	}
	_, ok := a.o.Player.State().Current()
	return ok
}

// present shows the logical canvas c on the display, scaled when their
// sizes differ.
func (a *App) present(c *gfx.Canvas) error {
	if a.scaler == nil {
		return a.o.Display.Present(c)
	}
	return a.o.Display.Present(a.scaler.Scale(c))
}

func (a *App) render() error {
	a.dirty = false
	if _, np := a.Top().(*NowPlayingScreen); !np {
		a.saver = false
	}
	if a.saver {
		a.drawSaver(a.canvas)
		return a.present(a.canvas)
	}
	a.animate, a.mq.seen, a.mqWake, a.dim = false, false, time.Time{}, false
	c := a.canvas
	c.Clear(colBg)
	top := a.Top()
	p := a.P
	// Content stays inside the title-safe area (SafeY lines top and bottom;
	// panels still run to the edges).
	body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY)
	if top != nil {
		_, fullscreen := top.(*NowPlayingScreen)
		if !fullscreen {
			a.drawHeader(c, top.Title())
			body = gfx.R(0, p.SafeY+p.HeaderH, p.W, p.H-2*p.SafeY-p.HeaderH)
			if a.hasCurrent() {
				body.H -= p.MiniBarH
				a.drawMiniBar(c, gfx.R(0, body.Bottom(), p.W, p.MiniBarH))
			}
		}
		top.Draw(a, c, body)
	}
	if !a.mq.seen {
		a.mq = marquee{}
	}
	a.drawToasts(c)
	a.drawVolumePanel(c)
	if a.confirm {
		a.drawConfirm(c)
	}
	return a.present(c)
}

func (a *App) drawHeader(c *gfx.Canvas, title string) {
	p := a.P
	c.Fill(gfx.R(0, 0, p.W, p.SafeY+p.HeaderH), colPanel)
	f := a.F.Title
	y := p.SafeY + (p.HeaderH+f.Ascent()-f.Descent())/2
	w := p.W - 2*p.Margin
	if a.insecure {
		fs := a.F.Small
		badge := "insecure"
		bw := fs.Measure(badge)
		fs.Draw(c, p.W-p.Margin-bw, y, badge, colError, c.Bounds())
		w -= bw + p.Margin/2
	}
	f.Draw(c, p.Margin, y, f.Truncate(title, w), colText, c.Bounds())
}

// drawDimmed runs draw for content that doesn't have the focus.
func (a *App) drawDimmed(dim bool, draw func()) {
	prev := a.dim
	a.dim = prev || dim
	draw()
	a.dim = prev
}

// drawFit draws s at (x, baseline y) within w pixels: cut with "…" when it
// doesn't fit, or, when focused, scrolling after a short pause (marquee).
func (a *App) drawFit(c *gfx.Canvas, f *gfx.Font, x, y, w int, s string, col gfx.Color, clip gfx.Rect, focused bool) {
	if !focused || a.dim || f.Measure(s) <= w {
		f.Draw(c, x, y, f.Truncate(s, w), col, clip)
		return
	}
	now := a.o.Now()
	if a.mq.text != s {
		a.mq = marquee{text: s, since: now}
	}
	a.mq.seen = true
	off := 0
	if run := now.Sub(a.mq.since) - marqueeDelay; run >= 0 {
		a.animate = true
		off = int(int64(run) * int64(a.P.MarqueeSpeed) / int64(time.Second))
		off %= f.Measure(s + marqueeGap)
	} else if wake := a.mq.since.Add(marqueeDelay); a.mqWake.IsZero() || wake.Before(a.mqWake) {
		a.mqWake = wake // identical frames until then: sleep to the end of the delay
	}
	vis, dx := f.Marquee(s, off)
	area := gfx.R(x, y-f.Ascent(), w, f.Height()).Intersect(clip)
	f.Draw(c, x+dx, y, vis, col, area)
}

func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
	st := a.o.Player.State()
	song, _ := st.Current()
	c.Fill(gfx.R(r.X, r.Y, r.W, a.P.H-r.Y), colPanel) // to the bottom edge, past the safe area
	p := a.P
	x := p.Margin
	art := r.H - 2*max(p.Margin/3, 2)
	ay := r.Y + (r.H-art)/2
	a.drawArt(c, song.CoverArt, gfx.R(x, ay, art, art))
	x += art + p.Margin/2
	w := r.Right() - p.Margin - x
	line := song.Title
	if song.Artist != "" {
		line += " — " + song.Artist
	}
	f := a.F.Body
	base := r.Y + r.H/2 + f.Ascent()/3
	iconText(c, f, statusIcon(st.Status), x, base, f.Truncate(line, w-f.Ascent()), colText, c.Bounds())
	if d := time.Duration(song.Duration) * time.Second; d > 0 {
		bw := progressW(w, st.Position, d)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, w, 2), colArtBg)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, bw, 2), colAccent)
	}
}

func (a *App) drawToasts(c *gfx.Canvas) {
	f := a.F.Body
	p := a.P
	y := p.H - p.SafeY - p.MiniBarH - p.Margin
	for i := len(a.toasts) - 1; i >= 0; i-- {
		text := f.Truncate(a.toasts[i].text, p.W-4*p.Margin)
		w := f.Measure(text) + p.Margin
		h := f.Height() + p.Margin/2
		r := gfx.R((p.W-w)/2, y-h, w, h)
		c.Fill(r, colOverlay)
		f.Draw(c, r.X+p.Margin/2, r.Y+p.Margin/4+f.Ascent(), text, colText, c.Bounds())
		y -= h + p.Margin/4
	}
}

func (a *App) drawConfirm(c *gfx.Canvas) {
	p := a.P
	c.Fill(c.Bounds(), colOverlay)
	lines := []string{"Exit MiSTer Subsonic?", "A = exit    B = stay"}
	f := a.F.Title
	y := p.H/2 - f.Height()
	for i, l := range lines {
		if i == 1 {
			f = a.F.Body
		}
		f.Draw(c, (p.W-f.Measure(l))/2, y+f.Ascent(), l, colText, c.Bounds())
		y += f.Height() + p.Margin/2
	}
}

// drawArt draws a cover (or a placeholder while it loads) into r.
func (a *App) drawArt(c *gfx.Canvas, id subsonic.ID, r gfx.Rect) {
	c.Fill(r, colArtBg)
	if img, ok := a.Art(id, r.W); ok {
		// Centre non-square art inside the square.
		w, h := r.W, r.H
		if img.W > img.H {
			h = r.H * img.H / img.W
		} else if img.H > img.W {
			w = r.W * img.W / img.H
		}
		c.Blit(img, gfx.R(r.X+(r.W-w)/2, r.Y+(r.H-h)/2, w, h))
	}
}

// clock formats d as m:ss or h:mm:ss.
func clock(d time.Duration) string {
	s := int(d / time.Second)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// quality is the badge for a song, e.g. "FLAC 24/96" or "MP3 320 · transcoded".
func quality(s subsonic.Song, transcoded bool) string {
	q := strings.ToUpper(s.Suffix)
	switch {
	case s.BitDepth > 0 && s.SamplingRate > 0:
		q += fmt.Sprintf(" %d/%s", s.BitDepth, khz(s.SamplingRate))
	case s.BitRate > 0:
		q += fmt.Sprintf(" %d", s.BitRate)
	}
	if transcoded {
		q += " · transcoded"
	}
	return strings.TrimSpace(q)
}

func khz(hz int) string {
	if hz%1000 == 0 {
		return fmt.Sprint(hz / 1000)
	}
	return fmt.Sprintf("%.1f", float64(hz)/1000)
}
