package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// Library is the server API the screens use (subset of *subsonic.Client).
type Library interface {
	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
}

// Player is the playback API the screens use (subset of *player.Player).
type Player interface {
	State() player.State
	Events() <-chan player.Event
	PlayNow(songs []subsonic.Song, start int)
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

type Options struct {
	Display gfx.Display
	Profile Profile
	Library Library
	Player  Player
	Art     ArtSource
	Inputs  []<-chan input.Event
	// Start runs on the UI goroutine before the first frame; it pushes the
	// first screen (main uses it for the connect-or-error flow).
	Start         func(a *App)
	FallbackFonts string
	Now           func() time.Time
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

const (
	toastTime    = 3 * time.Second
	exitHold     = 2 * time.Second
	progressTick = 500 * time.Millisecond
)

// App owns the screen stack and the event loop. All methods except Post
// must be called on the UI goroutine.
type App struct {
	o       Options
	P       Profile
	F       Fonts
	canvas  *gfx.Canvas
	scaler  *gfx.Scaler
	stack   []screenEntry
	toasts  []toast
	rep     input.Repeater
	in      chan input.Event
	post    chan func()
	loads   int
	dirty   bool
	quit    bool
	bDown   time.Time // when B went down on the root screen (zero if not held)
	confirm bool      // exit confirmation shown
}

func New(o Options) (*App, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true}
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
	a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
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
	a.dirty = true
	s.Enter(a)
}

// Pop closes the top screen (never the last one) and cancels its loads.
func (a *App) Pop() {
	if len(a.stack) <= 1 {
		return
	}
	top := a.stack[len(a.stack)-1]
	top.cancel()
	a.stack = a.stack[:len(a.stack)-1]
	a.dirty = true
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

func (a *App) entry(s Screen) *screenEntry {
	for i := range a.stack {
		if a.stack[i].s == s {
			return &a.stack[i]
		}
	}
	return nil
}

// Load runs fn off the UI goroutine and delivers its result to done on the
// UI goroutine, unless screen s has been popped by then.
func (a *App) Load(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) {
	e := a.entry(s)
	if e == nil {
		return
	}
	ctx := e.ctx
	a.loads++
	go func() {
		v, err := fn(ctx)
		a.Post(func() {
			a.loads--
			if ctx.Err() == nil {
				done(v, err)
				a.dirty = true
			}
		})
	}()
}

// Toast shows a short message over the current screen.
func (a *App) Toast(format string, args ...any) {
	a.toasts = append(a.toasts, toast{fmt.Sprintf(format, args...), a.o.Now().Add(toastTime)})
	a.dirty = true
}

// Run drives the UI until ctx ends or the user exits.
func (a *App) Run(ctx context.Context) error {
	if a.o.Start != nil {
		a.o.Start(a)
	}
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
	if !a.bDown.IsZero() {
		consider(a.bDown.Add(exitHold))
	}
	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
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
	if !a.bDown.IsZero() && !now.Before(a.bDown.Add(exitHold)) {
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
		if _, ok := a.Top().(*NowPlayingScreen); !ok && a.hasQueue() {
			a.Push(NewNowPlayingScreen())
		}
	}
}

func (a *App) hasQueue() bool {
	return a.o.Player != nil && len(a.o.Player.State().Queue) > 0
}

func (a *App) render() error {
	a.dirty = false
	c := a.canvas
	c.Clear(colBg)
	top := a.Top()
	p := a.P
	body := gfx.R(0, 0, p.W, p.H)
	if top != nil {
		_, fullscreen := top.(*NowPlayingScreen)
		if !fullscreen {
			a.drawHeader(c, top.Title())
			body = gfx.R(0, p.HeaderH, p.W, p.H-p.HeaderH)
			if a.hasQueue() {
				body.H -= p.MiniBarH
				a.drawMiniBar(c, gfx.R(0, p.H-p.MiniBarH, p.W, p.MiniBarH))
			}
		}
		top.Draw(a, c, body)
	}
	a.drawToasts(c)
	if a.confirm {
		a.drawConfirm(c)
	}
	return a.o.Display.Present(a.scaler.Scale(c))
}

func (a *App) drawHeader(c *gfx.Canvas, title string) {
	p := a.P
	c.Fill(gfx.R(0, 0, p.W, p.HeaderH), colPanel)
	f := a.F.Title
	y := (p.HeaderH + f.Ascent() - f.Descent()) / 2
	f.Draw(c, p.Margin, y, f.Truncate(title, p.W-2*p.Margin), colText, c.Bounds())
}

func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
	st := a.o.Player.State()
	song, _ := st.Current()
	c.Fill(r, colPanel)
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
		bw := w * int(min(st.Position, d)) / int(d)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, w, 2), colArtBg)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, bw, 2), colAccent)
	}
}

func (a *App) drawToasts(c *gfx.Canvas) {
	f := a.F.Body
	p := a.P
	y := p.H - p.MiniBarH - p.Margin
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
