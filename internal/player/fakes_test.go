package player

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

type seekCall struct {
	id  uint64
	pos time.Duration
}

type fakeEngine struct {
	mu        sync.Mutex
	played    []audio.Track
	replaced  []audio.Track // Replace calls
	queued    []audio.Track
	clears    int
	stops     int
	seeks     []seekCall
	seekErr   error
	seekBlock chan struct{} // if set, the seek doesn't complete until closed
	paused    bool
	volume    float32
	posID     uint64
	pos       time.Duration
	posOK     bool
	events    chan audio.Event
	eventReqs atomic.Int32 // Events() calls
	gains     map[uint64]float32
}

func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan audio.Event, 64)} }

func (e *fakeEngine) Play(t audio.Track) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.played = append(e.played, t)
	e.posID, e.pos, e.posOK = t.ID, t.Offset, true
}
func (e *fakeEngine) Replace(_ uint64, t audio.Track, prep func()) {
	if prep != nil {
		prep()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.replaced = append(e.replaced, t)
	e.posID, e.pos, e.posOK = t.ID, t.Offset, true
}
func (e *fakeEngine) QueueNext(t audio.Track) {
	e.mu.Lock()
	e.queued = append(e.queued, t)
	e.mu.Unlock()
}
func (e *fakeEngine) ClearNext()          { e.mu.Lock(); e.clears++; e.mu.Unlock() }
func (e *fakeEngine) Stop()               { e.mu.Lock(); e.stops++; e.posOK = false; e.mu.Unlock() }
func (e *fakeEngine) SetPaused(p bool)    { e.mu.Lock(); e.paused = p; e.mu.Unlock() }
func (e *fakeEngine) SetVolume(v float32) { e.mu.Lock(); e.volume = v; e.mu.Unlock() }
func (e *fakeEngine) SetGain(id uint64, g float32) {
	e.mu.Lock()
	if e.gains == nil {
		e.gains = map[uint64]float32{}
	}
	e.gains[id] = g
	e.mu.Unlock()
}
func (e *fakeEngine) gainOf(id uint64) (float32, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	g, ok := e.gains[id]
	return g, ok
}
func (e *fakeEngine) getVolume() float32         { e.mu.Lock(); defer e.mu.Unlock(); return e.volume }
func (e *fakeEngine) Events() <-chan audio.Event { e.eventReqs.Add(1); return e.events }

// Seek records the call and returns at once, like audio.Engine. The seek
// completes (position moves, or EventSeekFailed if seekErr is set) right
// away, or once seekBlock is closed.
func (e *fakeEngine) Seek(id uint64, pos time.Duration) {
	e.mu.Lock()
	e.seeks = append(e.seeks, seekCall{id, pos})
	block := e.seekBlock
	e.mu.Unlock()
	if block == nil {
		e.completeSeek(id, pos)
		return
	}
	go func() { <-block; e.completeSeek(id, pos) }()
}
func (e *fakeEngine) completeSeek(id uint64, pos time.Duration) {
	e.mu.Lock()
	err := e.seekErr
	if err == nil {
		e.pos = pos
	}
	e.mu.Unlock()
	if err != nil {
		e.events <- audio.Event{Kind: audio.EventSeekFailed, TrackID: id, Err: err}
	}
}
func (e *fakeEngine) seekList() []seekCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]seekCall(nil), e.seeks...)
}
func (e *fakeEngine) Position() (uint64, time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.posID, e.pos, e.posOK
}
func (e *fakeEngine) setPos(id uint64, pos time.Duration) {
	e.mu.Lock()
	e.posID, e.pos, e.posOK = id, pos, true
	e.mu.Unlock()
}
func (e *fakeEngine) lastPlayed() audio.Track {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.played[len(e.played)-1]
}
func (e *fakeEngine) playCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.played) }
func (e *fakeEngine) replaceList() []audio.Track {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]audio.Track(nil), e.replaced...)
}
func (e *fakeEngine) clearCount() int { e.mu.Lock(); defer e.mu.Unlock(); return e.clears }
func (e *fakeEngine) queueCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.queued) }

type scrobbleCall struct {
	id         subsonic.ID
	submission bool
}

type fakeAPI struct {
	mu        sync.Mutex
	scrobbles []scrobbleCall
	failNext  int // fail this many submission=true calls
	saves     [][]subsonic.ID
	saveCur   subsonic.ID
	savePos   time.Duration
	queue     *subsonic.PlayQueue
	failSaves bool          // SavePlayQueue fails
	blockSave chan struct{} // SavePlayQueue waits for this to close
	blockOnce chan struct{} // the first SavePlayQueue waits for this to close; then it is nil
}

func (a *fakeAPI) Scrobble(_ context.Context, id subsonic.ID, _ time.Time, sub bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sub && a.failNext > 0 {
		a.failNext--
		return errors.New("offline")
	}
	a.scrobbles = append(a.scrobbles, scrobbleCall{id, sub})
	return nil
}
func (a *fakeAPI) SavePlayQueue(_ context.Context, ids []subsonic.ID, cur subsonic.ID, pos time.Duration) error {
	a.mu.Lock()
	block := a.blockSave
	if a.blockOnce != nil {
		block, a.blockOnce = a.blockOnce, nil
	}
	a.mu.Unlock()
	if block != nil {
		<-block
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failSaves {
		return errors.New("server down")
	}
	a.saves = append(a.saves, ids)
	a.saveCur, a.savePos = cur, pos
	return nil
}
func (a *fakeAPI) GetPlayQueue(context.Context) (*subsonic.PlayQueue, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.queue, nil
}
func (a *fakeAPI) submissions() []subsonic.ID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []subsonic.ID
	for _, s := range a.scrobbles {
		if s.submission {
			ids = append(ids, s.id)
		}
	}
	return ids
}
func (a *fakeAPI) nowPlayings() []subsonic.ID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []subsonic.ID
	for _, s := range a.scrobbles {
		if !s.submission {
			ids = append(ids, s.id)
		}
	}
	return ids
}

type fakeSource struct {
	song     subsonic.ID
	promoted bool
	closed   bool
	mu       sync.Mutex
}

func (s *fakeSource) Read([]byte) (int, error)       { return 0, io.EOF }
func (s *fakeSource) Seek(int64, int) (int64, error) { return 0, nil }
func (s *fakeSource) Close() error                   { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }
func (s *fakeSource) Promote()                       { s.mu.Lock(); s.promoted = true; s.mu.Unlock() }
func (s *fakeSource) isPromoted() bool               { s.mu.Lock(); defer s.mu.Unlock(); return s.promoted }

// fakeStreamSource is a sized MP3's stream that can seek within itself, as
// NewOpener's do: retarget answers for positions inside window.
type fakeStreamSource struct {
	fakeSource
	window func(time.Duration) bool
	preps  atomic.Int32
}

func (s *fakeStreamSource) retarget(_ subsonic.Song, at time.Duration) (Opened, func(), bool) {
	if !s.window(at) {
		return Opened{}, nil, false
	}
	return Opened{Source: &fakeSource{song: s.song}, Format: audio.FormatMP3, Offset: at}, func() { s.preps.Add(1) }, true
}

type openCall struct {
	id       subsonic.ID
	offset   time.Duration
	prefetch bool
}

type fakeOpener struct {
	mu    sync.Mutex
	calls []openCall
	fail  map[subsonic.ID]bool
	gate  chan struct{} // if set, open blocks (after recording the call) until it is closed
	// window, if set, makes sized MP3s open as streams that seek themselves
	// within it (see fakeStreamSource).
	window func(time.Duration) bool
}

func (o *fakeOpener) open(_ context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error) {
	o.mu.Lock()
	gate := o.gate
	o.calls = append(o.calls, openCall{s.ID, offset, prefetch})
	o.mu.Unlock()
	if gate != nil {
		<-gate
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.fail[s.ID] {
		return Opened{}, errors.New("cannot open " + string(s.ID))
	}
	_, f, transcoded := PlanStream(s, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
	op := Opened{Source: &fakeSource{song: s.ID}, Format: f, Transcoded: transcoded}
	if o.window != nil && f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0 && !prefetch {
		op.Source = &fakeStreamSource{fakeSource: fakeSource{song: s.ID}, window: o.window}
	}
	if transcoded || (f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0) {
		op.Offset = offset // as NewOpener starts these at offset
	}
	return op, nil
}
func (o *fakeOpener) callList() []openCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]openCall(nil), o.calls...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type harness struct {
	t      *testing.T
	p      *Player
	eng    *fakeEngine
	api    *fakeAPI
	opener *fakeOpener
	clock  *clock
	tick   chan time.Time
	cancel context.CancelFunc
	done   chan struct{}
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	h := &harness{t: t, eng: newFakeEngine(), api: &fakeAPI{}, opener: &fakeOpener{fail: map[subsonic.ID]bool{}},
		clock: &clock{t: time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)}, tick: make(chan time.Time), done: make(chan struct{})}
	o := Options{Engine: h.eng, API: h.api, Open: h.opener.open, Scrobble: true, Tick: h.tick, Now: h.clock.now,
		Rand: rand.New(rand.NewPCG(1, 2)), ScrobblePath: t.TempDir() + "/scrobbles.json", ResumePath: t.TempDir() + "/state.json"}
	if mutate != nil {
		mutate(&o)
	}
	h.p = New(o)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.p.Run(ctx); close(h.done) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s (state %+v)", what, h.p.State())
		}
		time.Sleep(time.Millisecond)
	}
}

// started delivers the engine's Started event for the last played track.
func (h *harness) playAndStart(wantPlays int) audio.Track {
	h.t.Helper()
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() >= wantPlays })
	tr := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
	h.waitFor("playing", func() bool { return h.p.State().Status == Playing })
	return tr
}

// tickAt sets the engine position and runs one tick.
func (h *harness) tickAt(id uint64, pos time.Duration) {
	h.eng.setPos(id, pos)
	h.clock.advance(250 * time.Millisecond)
	h.tick <- h.clock.now()
	h.p.do(func() {})
}

func songs(n int, durationSec int) []subsonic.Song {
	out := make([]subsonic.Song, n)
	for i := range out {
		out[i] = subsonic.Song{ID: subsonic.ID("s" + string(rune('a'+i))), Title: "Song " + string(rune('A'+i)), Suffix: "flac", Duration: durationSec}
	}
	return out
}

// settle waits until every background save has reported back and the player
// has handled the reports.
func (h *harness) settle() {
	h.t.Helper()
	h.waitFor("background saves", func() bool { return h.p.busy.Load() == 0 })
	h.p.do(func() {})
}

// idleTick runs one tick with no engine movement, 31 s later on the clock.
func (h *harness) idleTick() {
	h.clock.advance(31 * time.Second)
	h.tick <- h.clock.now()
	h.p.do(func() {})
}
