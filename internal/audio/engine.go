package audio

import (
	"errors"
	"io"
	"sync"
	"time"
)

// Track is one thing for the engine to play.
type Track struct {
	// ID is the caller's token; it comes back in Events and Position. Must be non-zero.
	ID uint64
	// Source is closed by the engine when it is done with it (if it is an io.Closer).
	Source io.ReadSeeker
	Format Format
	// Gain is a linear multiplier (ReplayGain). 0 means unity.
	Gain float32
	// Offset is the song position of Source's first frame (non-zero for
	// transcoded streams reopened with timeOffset).
	Offset time.Duration
}

type EventKind int

const (
	// EventStarted: the track's first frame became audible.
	EventStarted EventKind = iota + 1
	// EventEnded: the track's last frame was played out.
	EventEnded
	// EventError: the track failed to open or decode. For the current track
	// the engine then moves on as if it had ended.
	EventError
	// EventSeekFailed: a Seek could not be done (Err says why, ErrNotCurrent
	// if the track wasn't the one being decoded). Playback carries on from
	// wherever the decoder is; the caller decides whether to reopen.
	EventSeekFailed
)

type Event struct {
	Kind    EventKind
	TrackID uint64
	Err     error
}

// ErrNotCurrent is the EventSeekFailed error when the track is not the one being decoded.
var ErrNotCurrent = errors.New("audio: track is not the one being decoded")

// ErrOpenTimeout is the EventError for a queued track that was still opening
// when it was needed, openWait after the previous track finished decoding.
var ErrOpenTimeout = errors.New("audio: next track took too long to open")

type EngineOptions struct {
	Output          Output
	OpenDecoder     OpenDecoderFunc // default OpenDecoder
	ResampleQuality int             // default DefaultResampleQuality
	ChunkFrames     int             // default 2048
	Poll            time.Duration   // default 5 ms

	// openWait bounds how long the end of a track waits for its queued
	// successor to finish opening; default 10 s. Unexported: tests only.
	openWait time.Duration
}

type voice struct {
	t    Track
	dec  Decoder
	gain float32
}

// segment marks where, in the device's frame count, a stretch of one track
// begins. id 0 means silence.
type segment struct {
	start uint64
	id    uint64
	base  time.Duration
}

type openResult struct {
	gen int
	v   *voice
	err error
	t   Track
}

// command is one call for the run goroutine. src is the track source it
// hands over: if the engine closes before running it, Close closes src.
type command struct {
	run func()
	src io.ReadSeeker
}

// Engine decodes tracks into an Output. Decoder state is owned by the run
// goroutine and public methods post commands to it. A separate monitor
// goroutine turns device progress into Started/Ended events, so events keep
// flowing even while a decode is blocked on a slow network read.
type Engine struct {
	o       EngineOptions
	cmds    chan command
	events  chan Event // closed once the engine has closed
	openCh  chan openResult
	quit    chan struct{}
	closing chan struct{} // closed first thing in Close: frees senders blocked on cmds
	done    chan struct{}
	openers sync.WaitGroup // queued tracks still opening

	closeOnce sync.Once
	sendMu    sync.Mutex // orders commands against Close
	closed    bool       // guarded by sendMu

	mu      sync.Mutex
	segs    []segment // guarded by mu
	evq     []Event   // guarded by mu
	busy    io.Closer // guarded by mu: source of the voice being decoded
	killed  io.Closer // guarded by mu: source closed by interrupt, reason for failed open
	seekReq *seekReq  // guarded by mu: latest requested seek, not yet run

	// Owned by the run goroutine.
	cur      *voice
	ended    bool // decoding reached the end of the queue naturally (not Stop)
	next     *voice
	opening  *Track
	openedAt time.Time // when opening was queued
	gen      int
	stops    int // incremented at top of doStop to detect Stop during finishCur's wait
	rs       *Resampler
	chainOut uint64
	chainIn  uint64
	written  uint64
	pending  []float32 // resampled output not yet taken by the device
	pendBuf  []float32 // pending's backing array, reused so decoding doesn't allocate
	scratch  []float32
}

func NewEngine(o EngineOptions) *Engine {
	if o.OpenDecoder == nil {
		o.OpenDecoder = OpenDecoder
	}
	if o.ResampleQuality == 0 {
		o.ResampleQuality = DefaultResampleQuality
	}
	if o.ChunkFrames == 0 {
		o.ChunkFrames = 2048
	}
	if o.Poll == 0 {
		o.Poll = 5 * time.Millisecond
	}
	if o.openWait == 0 {
		o.openWait = 10 * time.Second
	}
	e := &Engine{
		o:       o,
		cmds:    make(chan command, 64),
		events:  make(chan Event, 256),
		openCh:  make(chan openResult, 4),
		quit:    make(chan struct{}),
		closing: make(chan struct{}),
		done:    make(chan struct{}),
		segs:    []segment{{start: 0, id: 0}},
		scratch: make([]float32, o.ChunkFrames*2),
	}
	go e.run()
	go e.monitor()
	return e
}

func (e *Engine) Events() <-chan Event { return e.events }

// Play stops whatever is playing and starts t immediately.
func (e *Engine) Play(t Track) {
	e.dropSeekReq()
	e.send(func() { e.doPlay(t) }, t.Source)
	e.interrupt(t.Source)
}

// QueueNext sets the track to continue with, gaplessly, after the current
// one. It replaces any previously queued track. If the current track has
// already finished decoding (or finished playing), the queued track starts
// as soon as it is open. A track still opening 10 s after it is needed is
// dropped with EventError (ErrOpenTimeout) and the queue ends.
func (e *Engine) QueueNext(t Track) { e.send(func() { e.doQueueNext(t) }, t.Source) }

// ClearNext drops the queued track.
func (e *Engine) ClearNext() { e.send(e.cancelNext, nil) }

// Stop halts playback and discards everything buffered.
func (e *Engine) Stop() {
	e.dropSeekReq()
	e.send(e.doStop, nil)
	e.interrupt(nil)
}

// dropSeekReq ends seek coalescing: a seek issued after Play or Stop must
// queue its own command behind it, not overwrite one queued before it.
func (e *Engine) dropSeekReq() {
	e.mu.Lock()
	e.seekReq = nil
	e.mu.Unlock()
}

// seekReq is a requested seek that has not started yet.
type seekReq struct {
	id  uint64
	pos time.Duration
}

// Seek moves the track being decoded to pos (song time) and returns at once:
// the decoder seek may restart an HTTP request, and the caller must not wait
// for that. A failure arrives as EventSeekFailed; success shows in Position.
// Seeks coalesce per track: if a seek for the same track is still waiting to
// run, this one replaces its position (latest wins), so a burst of seeks
// behind a slow HTTP restart never fills the command queue. A seek for a
// different track, or after a Play or Stop, always queues its own command,
// behind that Play or Stop.
func (e *Engine) Seek(id uint64, pos time.Duration) {
	e.mu.Lock()
	if r := e.seekReq; r != nil && r.id == id {
		r.pos = pos // latest wins while this track's seek is still queued
		e.mu.Unlock()
		return
	}
	r := &seekReq{id: id, pos: pos}
	e.seekReq = r
	e.mu.Unlock()
	e.send(func() {
		e.mu.Lock()
		if e.seekReq == r {
			e.seekReq = nil
		}
		target := *r // read pos under the lock
		e.mu.Unlock()
		if err := e.doSeek(target.id, target.pos); err != nil {
			e.queueEvent(Event{Kind: EventSeekFailed, TrackID: target.id, Err: err})
		}
	}, nil)
}

// SetGain changes a track's gain (ReplayGain) at once: the track being
// decoded, the queued successor, or one still opening. What is already
// buffered (about half a second) plays at the old gain. 0 means unity.
func (e *Engine) SetGain(id uint64, gain float32) {
	if gain == 0 {
		gain = 1
	}
	e.send(func() {
		for _, v := range []*voice{e.cur, e.next} {
			if v != nil && v.t.ID == id {
				v.gain = gain
			}
		}
		if e.opening != nil && e.opening.ID == id {
			e.opening.Gain = gain // applied when it opens
		}
	}, nil)
}

func (e *Engine) SetPaused(p bool)    { e.o.Output.SetPaused(p) }
func (e *Engine) SetVolume(v float32) { e.o.Output.SetVolume(v) }

// Position reports which track is audible and where in it.
func (e *Engine) Position() (id uint64, pos time.Duration, ok bool) {
	c := e.o.Output.Consumed()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.segs) - 1; i >= 0; i-- {
		s := e.segs[i]
		if c >= s.start {
			if s.id == 0 {
				return 0, 0, false
			}
			return s.id, s.base + time.Duration(c-s.start)*time.Second/OutputRate, true
		}
	}
	return 0, 0, false
}

// Close stops the engine and closes every track source it still holds,
// including those of commands it never got to and of successors that
// finished opening too late. Events() is closed afterwards. The Output is
// not closed.
func (e *Engine) Close() {
	e.closeOnce.Do(func() { close(e.closing) }) // before sendMu: a blocked send holds it
	e.sendMu.Lock()
	if e.closed {
		e.sendMu.Unlock()
		return
	}
	e.closed = true
	e.sendMu.Unlock()
	close(e.quit) // before interrupt: the run loop must see quit when the read fails
	e.interrupt(nil)
	<-e.done
	go func() { e.openers.Wait(); close(e.openCh) }()
	for r := range e.openCh {
		closeVoice(r.v)
	}
	for {
		select {
		case c := <-e.cmds:
			closeSource(c.src)
		default:
			return
		}
	}
}

// send queues f for the run goroutine. src is the track source f takes over;
// if the engine is closed, src is closed instead.
func (e *Engine) send(f func(), src io.ReadSeeker) bool {
	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	if e.closed {
		closeSource(src)
		return false
	}
	select {
	case e.cmds <- command{run: f, src: src}:
		return true
	case <-e.closing:
		closeSource(src)
		return false
	case <-e.done:
		closeSource(src)
		return false
	}
}

// interrupt closes the source currently being read so a stalled network
// read can't hold up Play/Stop. The command is always queued first, so the
// run loop sees it before it reacts to the read error. keep is the new
// track's source: if the run loop already picked the command up, busy is
// that source and must be left alone.
func (e *Engine) interrupt(keep io.ReadSeeker) {
	kc, _ := keep.(io.Closer)
	e.mu.Lock()
	c := e.busy
	if c == nil || (kc != nil && c == kc) {
		e.mu.Unlock()
		return
	}
	e.busy = nil
	e.killed = c
	e.mu.Unlock()
	c.Close()
}

func (e *Engine) setBusy(src io.ReadSeeker) {
	c, _ := src.(io.Closer)
	e.mu.Lock()
	e.busy = c
	e.mu.Unlock()
}

func (e *Engine) run() {
	defer close(e.done)
	defer e.doStop()
	for {
		if !e.poll() {
			return
		}
		if e.ended && e.opening != nil && time.Since(e.openedAt) >= e.o.openWait {
			e.abandonOpening() // queued after the end; nothing else would give up on it
		}
		switch {
		case len(e.pending) > 0:
			e.writePending()
			if len(e.pending) > 0 && !e.wait(e.o.Poll) {
				return
			}
		case e.cur != nil:
			e.decodeChunk()
		default:
			if !e.wait(4 * e.o.Poll) {
				return
			}
		}
	}
}

// poll runs queued commands and open results without blocking.
func (e *Engine) poll() bool {
	for {
		if e.quitting() {
			return false // checked first: with quit closed, no queued command runs
		}
		select {
		case c := <-e.cmds:
			if !e.runCommand(c) {
				return false
			}
		case r := <-e.openCh:
			if !e.handleOpened(r) {
				return false
			}
		case <-e.quit:
			return false
		default:
			return true
		}
	}
}

func (e *Engine) quitting() bool {
	select {
	case <-e.quit:
		return true
	default:
		return false
	}
}

// runCommand runs c unless the engine is closing; then it only closes the
// source c took over (a Play would open a decoder, which may block on the
// network).
func (e *Engine) runCommand(c command) bool {
	if e.quitting() {
		closeSource(c.src)
		return false
	}
	c.run()
	return true
}

// handleOpened is runCommand for an open result.
func (e *Engine) handleOpened(r openResult) bool {
	if e.quitting() {
		closeVoice(r.v)
		return false
	}
	e.onOpened(r)
	return true
}

func (e *Engine) wait(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case c := <-e.cmds:
		return e.runCommand(c)
	case r := <-e.openCh:
		return e.handleOpened(r)
	case <-e.quit:
		return false
	case <-t.C:
	}
	return !e.quitting()
}

func (e *Engine) queueEvent(ev Event) {
	e.mu.Lock()
	e.evq = append(e.evq, ev)
	e.mu.Unlock()
}

func (e *Engine) monitor() {
	defer close(e.events)
	t := time.NewTicker(e.o.Poll)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-e.done:
			return
		}
		e.checkBoundaries()
		e.mu.Lock()
		evs := e.evq
		e.evq = nil
		e.mu.Unlock()
		for _, ev := range evs {
			select {
			case e.events <- ev:
			case <-e.done:
				return
			}
		}
	}
}

func (e *Engine) checkBoundaries() {
	c := e.o.Output.Consumed()
	e.mu.Lock()
	i := 0
	for i+1 < len(e.segs) && c >= e.segs[i+1].start {
		prev, nxt := e.segs[i], e.segs[i+1]
		if nxt.id != prev.id {
			if prev.id != 0 {
				e.evq = append(e.evq, Event{Kind: EventEnded, TrackID: prev.id})
			}
			if nxt.id != 0 {
				e.evq = append(e.evq, Event{Kind: EventStarted, TrackID: nxt.id})
			}
		}
		i++
	}
	e.segs = e.segs[i:]
	e.mu.Unlock()
}

func (e *Engine) addSegment(s segment) {
	e.mu.Lock()
	e.segs = append(e.segs, s)
	e.mu.Unlock()
}

func (e *Engine) resetSegments(s segment) {
	e.mu.Lock()
	e.segs = []segment{s}
	e.mu.Unlock()
}

func (e *Engine) decodeChunk() {
	v := e.cur
	n, err := v.dec.Read(e.scratch)
	if n > 0 {
		e.emit(e.scratch[:n*2], v)
	}
	if err == nil {
		return
	}
	if !e.poll() {
		return
	}
	if e.cur != v {
		return // a Play/Stop took over; the error came from the interrupted source
	}
	if !errors.Is(err, io.EOF) {
		e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
	}
	e.finishCur()
}

// writePending hands the device as much of pending as it takes.
func (e *Engine) writePending() {
	n := e.o.Output.Write(e.pending)
	e.written += uint64(n)
	e.pending = e.pending[n*2:]
}

func (e *Engine) emit(samples []float32, v *voice) {
	switch {
	case v.gain > 1:
		for i, s := range samples {
			samples[i] = softClip(s * v.gain)
		}
	case v.gain != 1:
		for i := range samples {
			samples[i] *= v.gain
		}
	}
	if len(e.pending) == 0 {
		e.pending = e.pendBuf[:0] // start over at the front of the buffer
	}
	if e.rs != nil {
		e.pending = e.rs.Process(samples, e.pending)
		e.chainIn += uint64(len(samples) / 2)
	} else {
		e.pending = append(e.pending, samples...)
	}
	if cap(e.pending) > cap(e.pendBuf) {
		e.pendBuf = e.pending[:0] // it grew: keep the bigger buffer
	}
}

// clipKnee is where softClip starts bending: below it samples pass exactly.
const clipKnee = 0.9

// softClip (spec §6) keeps a boosted sample inside (-1, 1) without the harsh
// edge of a hard clip. Above the knee it follows knee + (1-knee)·u/(1+u),
// u = (|s|-knee)/(1-knee): continuous with slope 1 at the knee, strictly
// increasing, and approaching but never reaching full scale. A rational
// curve instead of tanh, as it is cheap on the A9. Used only for gain > 1,
// so unity and attenuating gains stay bit-exact.
func softClip(s float32) float32 {
	switch {
	case s > clipKnee:
		u := (s - clipKnee) / (1 - clipKnee)
		return clipKnee + (1-clipKnee)*u/(1+u)
	case s < -clipKnee:
		u := (-s - clipKnee) / (1 - clipKnee)
		return -(clipKnee + (1-clipKnee)*u/(1+u))
	}
	return s
}

func (e *Engine) finishCur() {
	closeVoice(e.cur)
	e.cur = nil
	e.setBusy(nil)
	stops := e.stops
	deadline := time.Now().Add(e.o.openWait)
	for e.next == nil && e.opening != nil && e.cur == nil {
		if !time.Now().Before(deadline) {
			// Don't sit at the end of this track forever: drop the successor
			// and end the queue, so the player sees Ended and opens it anew.
			e.abandonOpening()
			break
		}
		e.writePending() // the end of this track still plays while the next one opens
		if !e.wait(e.o.Poll) {
			return
		}
	}
	if e.stops != stops {
		return // Stop occurred while we waited
	}
	if e.cur != nil {
		return // a Play arrived while we waited
	}
	if e.next != nil {
		v := e.next
		e.next = nil
		e.startVoice(v, true)
		return
	}
	e.breakChain()
	e.addSegment(segment{start: e.written + uint64(len(e.pending)/2), id: 0})
	e.ended = true
}

// breakChain flushes the resampler tail into pending and drops the resampler.
func (e *Engine) breakChain() {
	if e.rs != nil {
		e.pending = e.rs.Flush(e.pending)
		e.rs.Close()
		e.rs = nil
	}
}

func (e *Engine) startVoice(v *voice, gapless bool) {
	rate := v.dec.SampleRate()
	var start uint64
	if gapless && e.rs != nil && e.rs.InRate() == rate {
		start = e.chainOut + e.chainIn*OutputRate/uint64(rate)
	} else {
		e.breakChain()
		start = e.written + uint64(len(e.pending)/2)
		if rate != OutputRate {
			rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
			if err != nil {
				e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
				closeVoice(v)
				e.addSegment(segment{start: start, id: 0})
				return
			}
			e.rs, e.chainOut, e.chainIn = rs, start, 0
		}
	}
	e.cur = v
	e.ended = false
	e.setBusy(v.t.Source)
	e.addSegment(segment{start: start, id: v.t.ID, base: v.t.Offset})
}

func (e *Engine) openVoice(t Track) (*voice, error) {
	dec, err := e.o.OpenDecoder(t.Source, t.Format)
	if err != nil {
		closeSource(t.Source)
		return nil, err
	}
	g := t.Gain
	if g == 0 {
		g = 1
	}
	return &voice{t: t, dec: dec, gain: g}, nil
}

func (e *Engine) doPlay(t Track) {
	e.doStop()
	e.setBusy(t.Source)
	if e.quitting() { // Close closes quit before it reads busy: if this misses quit, interrupt sees this source
		e.setBusy(nil)
		closeSource(t.Source)
		return
	}
	v, err := e.openVoice(t)
	e.setBusy(nil)
	if err != nil {
		// Check if this error is because the source was interrupted.
		kc, _ := t.Source.(io.Closer)
		e.mu.Lock()
		interrupted := kc != nil && e.killed == kc
		e.mu.Unlock()
		if !interrupted {
			e.queueEvent(Event{Kind: EventError, TrackID: t.ID, Err: err})
		}
		return
	}
	e.startVoice(v, false)
}

func (e *Engine) doStop() {
	e.stops++
	closeVoice(e.cur)
	e.cur = nil
	e.ended = false
	e.setBusy(nil)
	e.cancelNext()
	if e.rs != nil {
		e.rs.Close()
		e.rs = nil
	}
	e.resetSegments(segment{start: e.written, id: 0})
	e.o.Output.Flush()
	e.pending = e.pending[:0]
}

func (e *Engine) doQueueNext(t Track) {
	e.cancelNext()
	e.gen++
	op := t // SetGain may change op; the opener goroutine reads t
	e.opening = &op
	e.openedAt = time.Now()
	gen := e.gen
	e.openers.Add(1)
	go func() {
		defer e.openers.Done()
		v, err := e.openVoice(t)
		select {
		case e.openCh <- openResult{gen: gen, v: v, err: err, t: t}:
		case <-e.done:
			closeVoice(v) // the engine stopped: nobody else will
		}
	}()
}

func (e *Engine) cancelNext() {
	if e.next != nil {
		closeVoice(e.next)
		e.next = nil
	}
	if e.opening != nil {
		closeSource(e.opening.Source) // unblocks the opener; its result is dropped as stale
		e.opening = nil
		e.gen++
	}
}

// abandonOpening cancels the successor that is still opening and reports it.
func (e *Engine) abandonOpening() {
	id := e.opening.ID
	e.cancelNext()
	e.queueEvent(Event{Kind: EventError, TrackID: id, Err: ErrOpenTimeout})
}

func (e *Engine) onOpened(r openResult) {
	if r.gen != e.gen {
		closeVoice(r.v)
		return
	}
	gain := e.opening.Gain
	e.opening = nil
	if r.err != nil {
		e.queueEvent(Event{Kind: EventError, TrackID: r.t.ID, Err: r.err})
		return
	}
	if gain != 0 {
		r.v.gain = gain // a SetGain that came while it opened
	}
	e.next = r.v
	if e.cur == nil && e.ended {
		// The current track finished decoding before its successor was
		// queued: start the successor right after whatever is still buffered.
		v := e.next
		e.next = nil
		e.startVoice(v, true)
	}
}

func (e *Engine) doSeek(id uint64, pos time.Duration) error {
	v := e.cur
	if v == nil || v.t.ID != id {
		return ErrNotCurrent
	}
	rel := pos - v.t.Offset
	if rel < 0 {
		rel = 0
	}
	rate := v.dec.SampleRate()
	if err := v.dec.SeekFrame(uint64(rel.Seconds() * float64(rate))); err != nil {
		return err
	}
	e.mu.Lock()
	// The seek makes track id audible now. If the monitor hasn't announced
	// it yet (its boundary is still pending), announce it here, because the
	// reset below discards that boundary.
	if cur := e.segs[0].id; cur != id {
		if cur != 0 {
			e.evq = append(e.evq, Event{Kind: EventEnded, TrackID: cur})
		}
		e.evq = append(e.evq, Event{Kind: EventStarted, TrackID: id})
	}
	e.segs = []segment{{start: e.written, id: id, base: pos}}
	e.mu.Unlock()
	e.o.Output.Flush()
	e.pending = e.pending[:0]
	if e.rs != nil {
		e.rs.Close()
		e.rs = nil
		rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
		if err != nil {
			return err
		}
		e.rs, e.chainOut, e.chainIn = rs, e.written, 0
	}
	return nil
}

func closeVoice(v *voice) {
	if v == nil {
		return
	}
	v.dec.Close()
	closeSource(v.t.Source)
}

func closeSource(s io.ReadSeeker) {
	if c, ok := s.(io.Closer); ok {
		c.Close()
	}
}
