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
	QueueNext(t audio.Track)
	ClearNext()
	Stop()
	Seek(id uint64, pos time.Duration) error
	SetPaused(bool)
	SetVolume(float32)
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
	Transcoded bool
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
	reopening  bool // true while reopening after a transcoded seek (I2)
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
		volumeDB:  o.VolumeDB,
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
	p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
	go p.scrobbles.Flush(ctx, p.o.API)
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
		case ev := <-p.o.Engine.Events():
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

// Remove deletes queue[i]. Removing the current song plays the next one.
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
		p.emit(Event{Kind: QueueChanged})
		if next := p.cursor + 1; next < len(p.order) {
			p.startAt(next, 0)
		} else {
			p.stop()
		}
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
			p.setStatus(Playing)
		case Stopped:
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
		if p.curSrc.Transcoded {
			p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
			return
		}
		if err := p.o.Engine.Seek(p.curID, pos); err != nil {
			p.reopenAt(pos) // I2: use reopenAt as fallback for raw seek
		}
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
		p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
	})
}

// Resumable finds a saved queue: the server's first, then the local file.
func (p *Player) Resumable(ctx context.Context) (*Resume, error) {
	if pq, err := p.o.API.GetPlayQueue(ctx); err == nil && pq != nil {
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
	select {
	case p.events <- ev:
	default:
	}
}

func (p *Player) publish() {
	s := State{Queue: p.queue, Index: p.currentIndex(), Status: p.status, Position: p.position,
		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Transcoded: p.curSrc.Transcoded}
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
	// Don't reset listen state or pause state; just reopening at a new offset
	if p.status != Paused {
		p.setStatus(Loading)
	}
	p.reopening = true
	p.openAsync(id, song, offset, false)
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
		if err == nil && !op.Transcoded {
			r.seek = offset
		}
		select {
		case p.opens <- r:
		case <-ctx.Done():
			closeOpened(op)
		}
	}()
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
			p.nextID = 0
			p.emit(Event{Kind: Error, Song: r.song, Err: r.err})
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
	p.curSrc = r.opened
	p.o.Engine.Play(p.track(r.id, r.song, r.opened))
	if r.seek > 0 {
		if err := p.o.Engine.Seek(r.id, r.seek); err != nil {
			log.Printf("player: resume seek: %v", err)
		}
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
		if p.reopening { // I2: skip normal started handling for reopening seek
			p.reopening = false
			if pr, ok := p.curSrc.Source.(Promoter); ok {
				pr.Promote()
			}
			p.failures = 0
			p.lastMove = p.o.Now()
			if p.status != Paused {
				p.setStatus(Playing)
			}
			return // skip nowPlaying for reopening
		}
		if pr, ok := p.curSrc.Source.(Promoter); ok {
			pr.Promote()
		}
		p.failures = 0
		p.lastMove = p.o.Now()
		if p.status != Paused {
			p.setStatus(Playing)
		}
		p.nowPlaying()
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
	case audio.EventError:
		switch ev.TrackID {
		case p.nextID:
			song := p.queue[p.order[p.nextCursor]]
			p.nextID = 0
			p.emit(Event{Kind: Error, Song: song, Err: ev.Err})
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
}

func (p *Player) nowPlaying() {
	if !p.o.Scrobble {
		return
	}
	song, _ := p.currentSong()
	p.startedAt = p.o.Now()
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
