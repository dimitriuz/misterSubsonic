// Package player owns the play queue and drives the audio engine: it opens
// streams, prefetches the next track for gapless playback, reports plays to
// the server and remembers where you were.
package player

import (
	"context"
	"errors"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

// Engine is the subset of *audio.Engine the player uses.
type Engine interface {
	Play(t audio.Track)
	// Replace swaps track cur for t and keeps the queued next one. prep runs
	// first; a cur that is no longer current fails as EventSeekFailed for
	// t.ID (see audio.Engine.Replace).
	Replace(cur uint64, t audio.Track, prep func())
	QueueNext(t audio.Track)
	ClearNext()
	Stop()
	// Seek must not block: failures come back as audio.EventSeekFailed.
	Seek(id uint64, pos time.Duration)
	SetPaused(bool)
	SetVolume(float32)
	// SetGain changes a playing or queued track's gain at once.
	SetGain(id uint64, gain float32)
	Position() (uint64, time.Duration, bool)
	Events() <-chan audio.Event
}

// API is the subset of *subsonic.Client the player uses.
type API interface {
	Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error
	SavePlayQueue(ctx context.Context, ids []subsonic.ID, current subsonic.ID, pos time.Duration) error
	GetPlayQueue(ctx context.Context) (*subsonic.PlayQueue, error)
}

type Status int

const (
	Stopped Status = iota
	Loading
	Playing
	Paused
	Buffering
)

func (s Status) String() string {
	return [...]string{"stopped", "loading", "playing", "paused", "buffering"}[s]
}

type Repeat int

const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// State is a snapshot for the UI. Queue must be treated as read-only.
type State struct {
	Queue      []subsonic.Song
	Index      int // into Queue; -1 when nothing is selected
	Status     Status
	Position   time.Duration
	Shuffle    bool
	Repeat     Repeat
	VolumeDB   float64
	Muted      bool // the output is silenced; VolumeDB is kept for unmuting
	Transcoded bool
	// NextIndex is the queue index that plays after the current song
	// (honouring shuffle and repeat), or -1.
	NextIndex int
}

func (s State) Current() (subsonic.Song, bool) {
	if s.Index < 0 || s.Index >= len(s.Queue) {
		return subsonic.Song{}, false
	}
	return s.Queue[s.Index], true
}

type EventKind int

const (
	TrackChanged EventKind = iota + 1
	StatusChanged
	QueueChanged
	Error
)

type Event struct {
	Kind EventKind
	Song subsonic.Song // Error: the song that failed
	Err  error
}

type Options struct {
	Engine        Engine
	API           API
	Open          Opener
	ReplayGain    string // off | track | album
	Scrobble      bool
	ResumePath    string           // local resume file; "" disables
	ScrobblePath  string           // retry queue file; "" keeps it in memory
	Tick          <-chan time.Time // default: 250 ms ticker
	Now           func() time.Time // default time.Now
	Rand          *rand.Rand
	OpenTimeout   time.Duration // default 15 s
	VolumeDB      float64
	PrefetchAhead time.Duration // default 20 s
}

const (
	maxConsecutiveFailures = 3
	saveInterval           = 30 * time.Second
	prevRestartThreshold   = 3 * time.Second
	stallThreshold         = time.Second
	maxTickJump            = 2 * time.Second
	scrobbleCap            = 240 * time.Second
)

type openResult struct {
	id     uint64
	song   subsonic.Song
	opened Opened
	err    error
	next   bool
	seek   time.Duration // raw seek to apply after Play
}

type Player struct {
	o         Options
	cmds      chan func()
	opens     chan openResult
	events    chan Event
	scrobbles *ScrobbleQueue
	ctx       context.Context
	runDone   chan struct{} // closed when Run returns (I3)

	mu   sync.Mutex
	snap State

	// Owned by the Run goroutine.
	queue      []subsonic.Song // copy-on-write
	order      []int           // play order: indices into queue
	cursor     int             // index into order; -1 none
	status     Status
	shuffle    bool
	repeat     Repeat
	volumeDB   float64
	muted      bool
	seq        uint64
	curID      uint64
	curSrc     Opened
	nextID     uint64
	nextCursor int
	nextSrc    Opened
	prefetched uint64 // curID for which a prefetch was attempted
	position   time.Duration
	lastPos    time.Duration
	lastMove   time.Time
	listened   time.Duration
	scrobbled  bool
	startedAt  time.Time
	failures   int
	lastSave   time.Time
	announced  bool          // true after now-playing sent for current play
	seekTarget time.Duration // position of the last raw seek sent to the engine
	seekRetry  bool          // a failed seek already caused one reopen
	// A seek that arrived while the current track was still opening; the
	// open applies it when it lands (openSeek is valid when hasOpenSeek).
	openSeek    time.Duration
	hasOpenSeek bool
}

func New(o Options) *Player {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	}
	if o.OpenTimeout <= 0 {
		o.OpenTimeout = 15 * time.Second
	}
	if o.PrefetchAhead <= 0 {
		o.PrefetchAhead = 20 * time.Second
	}
	p := &Player{
		o:         o,
		cmds:      make(chan func(), 64),
		opens:     make(chan openResult, 8),
		events:    make(chan Event, 64),
		scrobbles: LoadScrobbleQueue(o.ScrobblePath),
		runDone:   make(chan struct{}),
		cursor:    -1,
		volumeDB:  math.Max(-60, math.Min(0, o.VolumeDB)), // R12: clamp the start volume
	}
	p.publish()
	return p
}

// Events delivers notifications; if nobody reads, they are dropped.
func (p *Player) Events() <-chan Event { return p.events }

// State returns the latest snapshot.
func (p *Player) State() State { p.mu.Lock(); defer p.mu.Unlock(); return p.snap }

// Run drives the player until ctx is done. It saves the queue on the way out.
func (p *Player) Run(ctx context.Context) {
	defer close(p.runDone) // I3: signal that Run has exited
	p.ctx = ctx
	tick := p.o.Tick
	if tick == nil {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		tick = t.C
	}
	p.applyVolume()
	go p.scrobbles.Flush(ctx, p.o.API)
	events := p.o.Engine.Events()
	for {
		select {
		case <-ctx.Done():
			p.saveNow(context.Background(), true)
			p.o.Engine.Stop()
			return
		case f := <-p.cmds:
			f()
		case r := <-p.opens:
			p.onOpened(r)
		case ev, ok := <-events:
			if !ok {
				events = nil // the engine closed; stop listening instead of spinning
				continue
			}
			p.onEngine(ev)
		case <-tick:
			p.onTick()
		}
		p.publish()
	}
}

// do runs f on the player goroutine and waits for it. The snapshot is
// published before do returns, so State() reflects the command.
func (p *Player) do(f func()) {
	done := make(chan struct{})
	select {
	case p.cmds <- func() { f(); p.publish(); close(done) }:
	case <-p.runDone: // I3: don't block if Run has exited
		return
	}
	select {
	case <-done:
	case <-p.runDone: // I3: don't block if Run has exited
	}
}

// ---- commands ----

// PlayNow replaces the queue and starts songs[start].
func (p *Player) PlayNow(songs []subsonic.Song, start int) {
	p.do(func() {
		p.failures = 0 // a user action starts a fresh failure count
		if start < 0 || start >= len(songs) {
			return
		}
		p.queue = append([]subsonic.Song(nil), songs...)
		p.rebuildOrder(start)
		p.emit(Event{Kind: QueueChanged})
		p.startAt(p.cursor, 0)
	})
}

// PlayNext inserts songs right after the current one.
func (p *Player) PlayNext(songs []subsonic.Song) {
	p.do(func() {
		if len(songs) == 0 {
			return
		}
		if len(p.queue) == 0 {
			p.queue = append([]subsonic.Song(nil), songs...)
			p.rebuildOrder(0)
			p.emit(Event{Kind: QueueChanged})
			p.startAt(0, 0)
			return
		}
		at := p.currentIndex() + 1
		q := make([]subsonic.Song, 0, len(p.queue)+len(songs))
		q = append(append(append(q, p.queue[:at]...), songs...), p.queue[at:]...)
		p.queue = q
		for i, qi := range p.order {
			if qi >= at {
				p.order[i] = qi + len(songs)
			}
		}
		ins := make([]int, len(songs))
		for i := range songs {
			ins[i] = at + i
		}
		p.order = append(p.order[:p.cursor+1], append(ins, p.order[p.cursor+1:]...)...)
		p.queueChanged()
	})
}

// Enqueue appends songs to the end of the queue.
func (p *Player) Enqueue(songs []subsonic.Song) {
	p.do(func() {
		if len(songs) == 0 {
			return
		}
		if len(p.queue) == 0 {
			p.queue = append([]subsonic.Song(nil), songs...)
			p.rebuildOrder(0)
			p.emit(Event{Kind: QueueChanged})
			p.startAt(0, 0)
			return
		}
		base := len(p.queue)
		p.queue = append(append([]subsonic.Song(nil), p.queue...), songs...)
		for i := range songs {
			if p.shuffle {
				pos := p.cursor + 1 + p.o.Rand.IntN(len(p.order)-p.cursor)
				p.order = append(p.order[:pos], append([]int{base + i}, p.order[pos:]...)...)
			} else {
				p.order = append(p.order, base+i)
			}
		}
		p.queueChanged()
	})
}

// Remove deletes queue[i]. Removing the current song plays the next one
// (wrapping under repeat-all) and keeps a paused player paused; with nothing
// left to play it stops with no song selected.
func (p *Player) Remove(i int) {
	p.do(func() {
		if i < 0 || i >= len(p.queue) {
			return
		}
		wasCurrent := i == p.currentIndex()
		p.queue = append(append([]subsonic.Song(nil), p.queue[:i]...), p.queue[i+1:]...)
		var order []int
		newCursor := -1
		for oi, qi := range p.order {
			if qi == i {
				if oi <= p.cursor {
					newCursor = len(order) - 1
				}
				continue
			}
			if qi > i {
				qi--
			}
			if oi == p.cursor {
				newCursor = len(order)
			}
			order = append(order, qi)
		}
		p.order = order
		p.cursor = newCursor
		if !wasCurrent {
			p.queueChanged()
			return
		}
		wasPaused := p.status == Paused
		next := p.cursor + 1
		if next >= len(p.order) && p.repeat == RepeatAll {
			next = 0
		}
		if next >= len(p.order) {
			p.cursor = -1
			p.emit(Event{Kind: QueueChanged})
			p.stop()
			return
		}
		p.emit(Event{Kind: QueueChanged})
		p.startAt(next, 0)
		if wasPaused {
			p.o.Engine.SetPaused(true)
			p.setStatus(Paused)
		}
	})
}

// Move moves queue[from] to position to, shifting the entries between. The
// current song keeps playing and the index follows it. The play order (what
// comes next under shuffle) is kept, only relabelled to the new positions.
// Without shuffle the order is the queue's own. The prefetched successor is
// dropped only when the next song changed.
func (p *Player) Move(from, to int) {
	p.do(func() {
		n := len(p.queue)
		if from < 0 || from >= n || to < 0 || to >= n || from == to {
			return
		}
		moved := func(i int) int { // where old position i ends up
			switch {
			case i == from:
				return to
			case from < to && i > from && i <= to:
				return i - 1
			case to < from && i >= to && i < from:
				return i + 1
			}
			return i
		}
		oldNext := -1
		if c := p.followingCursor(true); c >= 0 {
			oldNext = moved(p.order[c])
		}
		q := make([]subsonic.Song, n)
		for i, s := range p.queue {
			q[moved(i)] = s
		}
		p.queue = q
		if p.shuffle { // the same sequence of songs, under their new positions
			for oi, qi := range p.order {
				p.order[oi] = moved(qi)
			}
		} else { // the order is the queue's: the cursor follows the current song
			if p.cursor >= 0 {
				p.cursor = moved(p.cursor)
			}
			for i := range p.order {
				p.order[i] = i
			}
		}
		newNext := -1
		if c := p.followingCursor(true); c >= 0 {
			newNext = p.order[c]
		}
		if newNext != oldNext {
			p.invalidateNext()
		}
		p.emit(Event{Kind: QueueChanged})
	})
}

// Clear empties the queue and stops.
func (p *Player) Clear() {
	p.do(func() {
		p.stop()
		p.queue, p.order, p.cursor = nil, nil, -1
		p.emit(Event{Kind: QueueChanged})
	})
}

// Jump plays queue[i].
func (p *Player) Jump(i int) {
	p.do(func() {
		p.failures = 0 // a user action starts a fresh failure count
		for oi, qi := range p.order {
			if qi == i {
				p.startAt(oi, 0)
				return
			}
		}
	})
}

func (p *Player) Next() {
	p.do(func() {
		p.failures = 0 // a user action starts a fresh failure count
		if c := p.followingCursor(false); c >= 0 {
			p.startAt(c, 0)
		} else {
			p.stop()
		}
	})
}

// Prev goes to the previous song, or restarts the current one if it has
// played for more than 3 seconds.
func (p *Player) Prev() {
	p.do(func() {
		p.failures = 0 // a user action starts a fresh failure count
		if p.cursor < 0 {
			return
		}
		if p.position < prevRestartThreshold && p.cursor > 0 {
			p.startAt(p.cursor-1, 0)
			return
		}
		p.startAt(p.cursor, 0)
	})
}

func (p *Player) TogglePause() {
	p.do(func() {
		switch p.status {
		case Playing, Buffering, Loading:
			p.o.Engine.SetPaused(true)
			p.setStatus(Paused)
		case Paused:
			p.o.Engine.SetPaused(false)
			p.lastMove = p.o.Now() // the paused time isn't a stall
			p.setStatus(Playing)
		case Stopped:
			if p.cursor < 0 && len(p.order) > 0 {
				p.cursor = 0 // nothing selected: play the queue from the top
			}
			if p.cursor >= 0 {
				p.startAt(p.cursor, 0)
			}
		}
	})
}

// Seek moves within the current song.
func (p *Player) Seek(pos time.Duration) {
	p.do(func() {
		song, ok := p.currentSong()
		if !ok || p.curID == 0 {
			return
		}
		if pos < 0 {
			pos = 0
		}
		if d := time.Duration(song.Duration) * time.Second; d > 0 && pos >= d {
			pos = d - time.Second
		}
		p.lastPos, p.position = pos, pos
		p.seekRetry = false
		if p.curSrc.Source == nil {
			// Still opening: the open applies it when it lands.
			p.openSeek, p.hasOpenSeek = pos, true
			return
		}
		if seeksByReopening(p.curSrc, song) {
			if !p.retargetAt(song, pos) {
				p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
			}
			return
		}
		// R16: fire and forget. A failure (e.g. ErrNotCurrent while the
		// track is still loading) comes back as EventSeekFailed -> reopenAt.
		p.seekTarget = pos
		p.o.Engine.Seek(p.curID, pos)
	})
}

func (p *Player) SetShuffle(on bool) {
	p.do(func() {
		if on == p.shuffle {
			return
		}
		p.shuffle = on
		p.rebuildOrder(p.currentIndex())
		p.queueChanged()
	})
}

func (p *Player) SetRepeat(r Repeat) {
	p.do(func() {
		p.repeat = r
		p.invalidateNext()
	})
}

// SetVolumeDB sets the software volume, clamped to -60..0 dB.
func (p *Player) SetVolumeDB(db float64) {
	p.do(func() {
		p.volumeDB = math.Max(-60, math.Min(0, db))
		p.applyVolume()
	})
}

// SetMuted silences the output or brings it back at the volume set.
func (p *Player) SetMuted(on bool) {
	p.do(func() {
		p.muted = on
		p.applyVolume()
	})
}

// applyVolume hands the engine the gain for the volume and mute state.
func (p *Player) applyVolume() {
	g := dbToLinear(p.volumeDB)
	if p.muted {
		g = 0
	}
	p.o.Engine.SetVolume(g)
}

// SetReplayGain sets the ReplayGain mode (off, track or album). It applies
// at once: to the playing track, the queued successor, and every track
// opened from now on.
func (p *Player) SetReplayGain(mode string) {
	p.do(func() {
		p.o.ReplayGain = mode
		if song, ok := p.currentSong(); ok && p.curID != 0 {
			p.o.Engine.SetGain(p.curID, ReplayGainFactor(song, mode))
		}
		if p.nextID != 0 {
			p.o.Engine.SetGain(p.nextID, ReplayGainFactor(p.queue[p.order[p.nextCursor]], mode))
		}
	})
}

// SetScrobble turns now-playing and scrobble reports on or off.
func (p *Player) SetScrobble(on bool) {
	p.do(func() { p.o.Scrobble = on })
}

// Resumable finds a saved queue: the server's first, then the local file.
func (p *Player) Resumable(ctx context.Context) (*Resume, error) {
	if pq, err := p.o.API.GetPlayQueue(ctx); err == nil && pq != nil && len(pq.Songs) > 0 {
		r := &Resume{Songs: pq.Songs, Position: time.Duration(pq.Position) * time.Millisecond}
		for i, s := range pq.Songs {
			if s.ID == pq.Current {
				r.Index = i
			}
		}
		return r, nil
	}
	if p.o.ResumePath == "" {
		return nil, nil
	}
	return LoadResume(p.o.ResumePath)
}

// ResumeFrom loads r into the queue and starts playing at its position.
func (p *Player) ResumeFrom(r *Resume) {
	p.do(func() {
		p.failures = 0 // a user action starts a fresh failure count
		if r == nil || r.Index < 0 || r.Index >= len(r.Songs) {
			return
		}
		p.queue = append([]subsonic.Song(nil), r.Songs...)
		p.rebuildOrder(r.Index)
		p.emit(Event{Kind: QueueChanged})
		p.startAt(p.cursor, r.Position)
	})
}

// ---- internals ----

func dbToLinear(db float64) float32 { return float32(math.Pow(10, db/20)) }

func (p *Player) emit(ev Event) {
	// I1: publish the snapshot before the event is visible to a consumer,
	// so State() is never stale relative to an event already received.
	p.publish()
	select {
	case p.events <- ev:
	default:
	}
}

func (p *Player) publish() {
	s := State{Queue: p.queue, Index: p.currentIndex(), Status: p.status, Position: p.position,
		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Muted: p.muted, Transcoded: p.curSrc.Transcoded, NextIndex: -1}
	if c := p.followingCursor(true); c >= 0 {
		s.NextIndex = p.order[c]
	}
	p.mu.Lock()
	p.snap = s
	p.mu.Unlock()
}

func (p *Player) setStatus(s Status) {
	if p.status != s {
		p.status = s
		p.emit(Event{Kind: StatusChanged})
	}
}

func (p *Player) currentIndex() int {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return -1
	}
	return p.order[p.cursor]
}

func (p *Player) currentSong() (subsonic.Song, bool) {
	i := p.currentIndex()
	if i < 0 {
		return subsonic.Song{}, false
	}
	return p.queue[i], true
}

// rebuildOrder resets the play order so that queue[current] is at the cursor.
func (p *Player) rebuildOrder(current int) {
	n := len(p.queue)
	p.order = make([]int, n)
	for i := range p.order {
		p.order[i] = i
	}
	if n == 0 {
		p.cursor = -1
		return
	}
	if current < 0 {
		current = 0
	}
	if !p.shuffle {
		p.cursor = current
		return
	}
	rest := make([]int, 0, n-1)
	for i := 0; i < n; i++ {
		if i != current {
			rest = append(rest, i)
		}
	}
	p.o.Rand.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
	p.order = append([]int{current}, rest...)
	p.cursor = 0
}

// followingCursor is the cursor after the current one. auto=true applies
// repeat-one (the natural end of a track); Next() passes false.
func (p *Player) followingCursor(auto bool) int {
	if p.cursor < 0 || len(p.order) == 0 {
		return -1
	}
	if auto && p.repeat == RepeatOne {
		return p.cursor
	}
	if p.cursor+1 < len(p.order) {
		return p.cursor + 1
	}
	if p.repeat == RepeatAll || p.repeat == RepeatOne {
		return 0
	}
	return -1
}

func (p *Player) queueChanged() {
	p.invalidateNext()
	p.emit(Event{Kind: QueueChanged})
}

// invalidateNext drops the prepared gapless successor after queue changes.
func (p *Player) invalidateNext() {
	if p.nextID != 0 {
		p.o.Engine.ClearNext()
		p.nextID = 0
		p.nextSrc = Opened{}
	}
	p.prefetched = 0
}

func (p *Player) stop() {
	p.o.Engine.Stop()
	p.o.Engine.SetPaused(false)
	p.curID, p.nextID = 0, 0
	p.curSrc, p.nextSrc = Opened{}, Opened{}
	p.position = 0
	p.setStatus(Stopped)
}

// startAt opens order[cursor] and plays it from offset.
func (p *Player) startAt(cursor int, offset time.Duration) {
	p.invalidateNext()
	p.cursor = cursor
	song, _ := p.currentSong()
	p.seq++
	id := p.seq
	p.curID = id
	p.curSrc = Opened{}
	p.position, p.lastPos = offset, offset
	p.seekRetry = false
	p.hasOpenSeek = false
	p.resetListen()
	p.o.Engine.SetPaused(false)
	p.setStatus(Loading)
	p.emit(Event{Kind: TrackChanged, Song: song})
	p.openAsync(id, song, offset, false)
}

// reopenAt reopens the current track at offset, preserving listen state and pause (I2).
func (p *Player) reopenAt(offset time.Duration) {
	song, ok := p.currentSong()
	if !ok || p.curID == 0 {
		return
	}
	p.invalidateNext()
	p.seq++
	id := p.seq
	p.curID = id
	p.curSrc = Opened{}
	p.position, p.lastPos = offset, offset
	p.hasOpenSeek = false
	// Don't reset listen state or pause state; just reopening at a new offset
	if p.status != Paused {
		p.setStatus(Loading)
	}
	p.openAsync(id, song, offset, false)
}

// retargetAt seeks the current track within the stream it already has open,
// if that holds the target: the engine swaps in a decoder on the same stream
// and the queued successor stays. It reports false when the stream doesn't
// hold it (or isn't one that can): the caller reopens.
func (p *Player) retargetAt(song subsonic.Song, offset time.Duration) bool {
	rt, ok := p.curSrc.Source.(retargeter)
	if !ok {
		return false
	}
	op, prep, ok := rt.retarget(song, offset)
	if !ok {
		return false
	}
	p.seq++
	id, old := p.seq, p.curID
	p.curID, p.curSrc = id, op
	p.position, p.lastPos = offset, offset
	p.hasOpenSeek = false
	// A Replace the engine refuses (a handover got there first) comes back
	// as EventSeekFailed, which reopens at offset like a failed raw seek.
	p.seekTarget, p.seekRetry = offset, false
	if p.status != Paused {
		p.setStatus(Loading)
	}
	p.o.Engine.Replace(old, p.track(id, song, op), prep)
	return true
}

func (p *Player) openAsync(id uint64, song subsonic.Song, offset time.Duration, next bool) {
	ctx := p.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		octx, cancel := context.WithTimeout(ctx, p.o.OpenTimeout)
		defer cancel()
		op, err := p.o.Open(octx, song, offset, next)
		r := openResult{id: id, song: song, opened: op, err: err, next: next}
		if err == nil && !op.Transcoded && op.Offset == 0 {
			r.seek = offset // the opener didn't start the stream at offset: the engine seeks
		}
		select {
		case p.opens <- r:
		case <-ctx.Done():
			closeOpened(op)
		}
	}()
}

// seeksByReopening: transcoded streams (timeOffset) and MP3s of known size
// and length (a byte estimate, see mp3Offset) are sought by opening the
// stream again at the target; everything else by the engine.
func seeksByReopening(o Opened, s subsonic.Song) bool {
	return o.Transcoded || (o.Format == audio.FormatMP3 && s.Size > 0 && s.Duration > 0)
}

func closeOpened(o Opened) {
	if c, ok := o.Source.(interface{ Close() error }); ok {
		c.Close()
	}
}

func (p *Player) onOpened(r openResult) {
	if r.next {
		if r.id != p.nextID {
			closeOpened(r.opened)
			return
		}
		if r.err != nil {
			// No toast: the song is opened again when its turn comes, and
			// that attempt reports if it fails too.
			p.nextID = 0
			return
		}
		p.nextSrc = r.opened
		p.o.Engine.QueueNext(p.track(r.id, r.song, r.opened))
		return
	}
	if r.id != p.curID {
		closeOpened(r.opened)
		return
	}
	if r.err != nil {
		p.trackFailed(r.song, r.err)
		return
	}
	if p.hasOpenSeek {
		p.hasOpenSeek = false
		if seeksByReopening(r.opened, r.song) {
			closeOpened(r.opened) // opened at the old offset
			p.reopenAt(p.openSeek)
			return
		}
		r.seek = p.openSeek
	}
	p.curSrc = r.opened
	p.o.Engine.Play(p.track(r.id, r.song, r.opened))
	if r.seek > 0 {
		p.seekTarget = r.seek
		p.o.Engine.Seek(r.id, r.seek) // a failure arrives as EventSeekFailed
	}
}

func (p *Player) track(id uint64, s subsonic.Song, o Opened) audio.Track {
	return audio.Track{ID: id, Source: o.Source, Format: o.Format, Gain: ReplayGainFactor(s, p.o.ReplayGain), Offset: o.Offset}
}

// trackFailed reports err and moves on, stopping after too many in a row.
func (p *Player) trackFailed(song subsonic.Song, err error) {
	p.emit(Event{Kind: Error, Song: song, Err: err})
	p.failures++
	if p.failures >= maxConsecutiveFailures {
		p.failures = 0
		p.stop()
		return
	}
	if c := p.followingCursor(false); c >= 0 {
		p.startAt(c, 0)
	} else {
		p.stop()
	}
}

func (p *Player) onEngine(ev audio.Event) {
	switch ev.Kind {
	case audio.EventStarted:
		if ev.TrackID == p.nextID {
			p.handover() // I1: use extracted helper
		}
		if ev.TrackID != p.curID {
			return
		}
		if !p.announced {
			p.nowPlaying() // announce this play unless already announced
		}
		if pr, ok := p.curSrc.Source.(Promoter); ok {
			pr.Promote()
		}
		p.failures = 0
		p.lastMove = p.o.Now()
		if p.status != Paused {
			p.setStatus(Playing)
		}
	case audio.EventEnded:
		if ev.TrackID != p.curID {
			return
		}
		if p.nextID != 0 && p.nextSrc.Source != nil {
			p.handover() // I1: handover now instead of waiting for Started(B)
			return
		}
		if c := p.followingCursor(true); c >= 0 {
			p.startAt(c, 0)
		} else {
			p.curID = 0
			p.stop()
		}
	case audio.EventSeekFailed:
		if ev.TrackID != p.curID {
			return // stale: the track was replaced after the seek was sent
		}
		if p.seekRetry {
			// The seek after a reopen failed too; keep playing from where
			// the engine is instead of reopening forever.
			log.Printf("player: seek: %v", ev.Err)
			return
		}
		p.seekRetry = true
		p.reopenAt(p.seekTarget) // I2: reopen preserves listen state and pause
	case audio.EventError:
		switch ev.TrackID {
		case p.nextID:
			// The engine dropped the successor (failed or timed out opening;
			// it closed the source). Ended(cur) then falls back to startAt,
			// which reports if the song fails again.
			p.nextID, p.nextSrc = 0, Opened{}
		case p.curID:
			song, _ := p.currentSong()
			if p.nextID != 0 {
				p.emit(Event{Kind: Error, Song: song, Err: ev.Err}) // engine continues into the successor
				return
			}
			p.trackFailed(song, ev.Err)
		}
	}
}

// handover transitions from current to the already-queued successor (I1).
// Called when Ended(cur) arrives with successor ready, or when Started(next) arrives after handover.
func (p *Player) handover() {
	p.cursor = p.nextCursor
	p.curID, p.curSrc = p.nextID, p.nextSrc
	p.nextID, p.nextSrc = 0, Opened{}
	p.prefetched = 0
	p.position, p.lastPos = 0, 0
	p.resetListen()
	song, _ := p.currentSong()
	p.emit(Event{Kind: TrackChanged, Song: song})
}

func (p *Player) onTick() {
	now := p.o.Now()
	id, pos, ok := p.o.Engine.Position()
	if ok && id == p.curID && p.curID != 0 {
		delta := pos - p.lastPos
		if delta > 0 {
			if delta < maxTickJump && p.status == Playing {
				p.listened += delta
			}
			p.lastMove = now
			if p.status == Buffering {
				p.setStatus(Playing)
			}
		} else if p.status == Playing && now.Sub(p.lastMove) > stallThreshold {
			p.setStatus(Buffering)
		}
		p.lastPos, p.position = pos, pos
		p.maybeScrobble()
		p.maybePrefetch()
	}
	if (p.status == Playing || p.status == Paused) && now.Sub(p.lastSave) >= saveInterval {
		p.saveNow(p.ctx, false)
	}
}

func (p *Player) maybePrefetch() {
	if p.nextID != 0 || p.prefetched == p.curID || p.repeat == RepeatOne {
		return
	}
	song, _ := p.currentSong()
	d := time.Duration(song.Duration) * time.Second
	if d <= 0 || d-p.position > p.o.PrefetchAhead {
		return
	}
	p.prefetched = p.curID
	c := p.followingCursor(true)
	if c < 0 {
		return
	}
	p.seq++
	p.nextID, p.nextCursor = p.seq, c
	p.openAsync(p.nextID, p.queue[p.order[c]], 0, true)
}

func (p *Player) resetListen() {
	p.listened = 0
	p.scrobbled = false
	p.startedAt = p.o.Now()
	p.announced = false
}

func (p *Player) nowPlaying() {
	if !p.o.Scrobble {
		return
	}
	song, _ := p.currentSong()
	p.startedAt = p.o.Now()
	p.announced = true
	go p.o.API.Scrobble(p.ctx, song.ID, p.startedAt, false)
}

func (p *Player) maybeScrobble() {
	if !p.o.Scrobble || p.scrobbled {
		return
	}
	song, _ := p.currentSong()
	need := scrobbleCap
	if d := time.Duration(song.Duration) * time.Second; d > 0 && d/2 < need {
		need = d / 2
	}
	if p.listened < need {
		return
	}
	p.scrobbled = true
	s := Scrobble{ID: song.ID, At: p.startedAt}
	ctx := p.ctx
	go func() {
		if err := p.o.API.Scrobble(ctx, s.ID, s.At, true); err != nil {
			p.scrobbles.Add(s)
			return
		}
		p.scrobbles.Flush(ctx, p.o.API)
	}()
}

// saveNow stores the queue locally and on the server. sync waits for the
// server call (used on exit).
func (p *Player) saveNow(ctx context.Context, sync bool) {
	p.lastSave = p.o.Now()
	i := p.currentIndex()
	if i < 0 {
		return
	}
	songs := p.queue
	pos := p.position
	if p.o.ResumePath != "" {
		if err := SaveResume(p.o.ResumePath, Resume{Songs: songs, Index: i, Position: pos}); err != nil {
			log.Printf("player: save resume: %v", err)
		}
	}
	ids := make([]subsonic.ID, len(songs))
	for k, s := range songs {
		ids[k] = s.ID
	}
	save := func() {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.o.API.SavePlayQueue(sctx, ids, songs[i].ID, pos); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("player: savePlayQueue: %v", err)
		}
	}
	if sync {
		save()
	} else {
		go save()
	}
}
