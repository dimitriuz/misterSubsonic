// Package stream provides a seekable, memory-bounded reader over an HTTP
// resource. It keeps a sliding window of the file in RAM, fetches ahead with
// Range requests, reconnects on stalls and drops, and re-requests at the
// target offset when a seek leaves the window.
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Client *http.Client // default http.DefaultClient
	Header http.Header  // extra request headers
	// CheckResponse can reject a response (e.g. an error document served
	// with status 200). It must not read the body unless it returns an error.
	CheckResponse func(*http.Response) error

	WindowBytes   int64           // RAM per reader; default 32 MiB
	BehindBytes   int64           // kept behind the read position; default WindowBytes/4
	NearBytes     int64           // forward seeks within this distance of the window just wait; default 256 KiB
	PrefetchBytes int64           // if > 0, fetch at most this far ahead of the read position until Promote (ring 1.25×, grown to WindowBytes then)
	StallTimeout  time.Duration   // no bytes (or no response headers) for this long -> reconnect; Open fails after it; default 10 s
	Backoff       []time.Duration // retry delays; default 0.5, 1, 2, 4, 8 s (last repeats)
	RetryBudget   time.Duration   // give up after this long without progress; default 30 s
}

var (
	ErrClosed  = errors.New("stream: reader closed")
	errStale   = errors.New("stream: stale fetch")
	errStalled = errors.New("stream: stalled")
	// errBudgetSpent is Open's retry budget running out on a request.
	errBudgetSpent = fmt.Errorf("stream: retry budget spent: %w", context.DeadlineExceeded)
)

// HTTPError is a non-success HTTP status.
type HTTPError struct {
	StatusCode int
	Status     string
	RetryAfter time.Duration // the server's Retry-After, if it sent one
}

func (e *HTTPError) Error() string { return "stream: HTTP " + e.Status }

// retryLater: statuses where the server asks to come back (busy, rate
// limited, a gateway in the way), even on the first request.
func retryLater(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// maxRetryAfter caps a Retry-After: far beyond any retry budget, and far
// from overflowing a Duration.
const maxRetryAfter = 24 * time.Hour

// parseRetryAfter reads a Retry-After header: seconds or an HTTP date.
func parseRetryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if s, err := strconv.ParseInt(h, 10, 64); err == nil && s >= 0 {
		return time.Duration(min(s, int64(maxRetryAfter/time.Second))) * time.Second
	} else if errors.Is(err, strconv.ErrRange) && h[0] != '-' {
		return maxRetryAfter
	}
	if t, err := http.ParseTime(h); err == nil && t.After(now) {
		return min(t.Sub(now), maxRetryAfter)
	}
	return 0
}

type Reader struct {
	url string
	o   Options

	mu        sync.Mutex
	cond      *sync.Cond
	ring      []byte
	lo, hi    int64 // file offsets held in ring: [lo, hi)
	pos       int64
	size      int64 // -1 when unknown
	seekable  bool
	eof       bool
	err       error
	closed    bool
	prefetch  int64
	promoting bool // a Promote is allocating the window; another needn't
	gen       int
	cancel    context.CancelFunc
	alloc     func(n int64) []byte // makes the ring Promote grows to; a test hook
}

// Open issues the first request and returns once response headers arrive.
// ctx bounds only that first request; the reader lives until Close.
func Open(ctx context.Context, url string, o Options) (*Reader, error) {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.WindowBytes <= 0 {
		o.WindowBytes = 32 << 20
	}
	if o.BehindBytes <= 0 || o.BehindBytes >= o.WindowBytes {
		o.BehindBytes = o.WindowBytes / 4
	}
	if o.NearBytes <= 0 {
		o.NearBytes = 256 << 10
	}
	if o.StallTimeout <= 0 {
		o.StallTimeout = 10 * time.Second
	}
	if len(o.Backoff) == 0 {
		o.Backoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	}
	if o.RetryBudget <= 0 {
		o.RetryBudget = 30 * time.Second
	}
	ringBytes := o.WindowBytes
	if o.PrefetchBytes > 0 {
		// A queued successor needs only its prefetch until it plays; the
		// full window comes with Promote.
		ringBytes = min(o.WindowBytes, o.PrefetchBytes+o.PrefetchBytes/4)
	}
	r := &Reader{url: url, o: o, ring: make([]byte, ringBytes), size: -1, prefetch: o.PrefetchBytes,
		alloc: func(n int64) []byte { return make([]byte, n) }}
	r.cond = sync.NewCond(&r.mu)

	fctx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	deadline, _ := ctx.Deadline() // fctx is detached from ctx, so openRequest is told separately
	resp, reqCancel, err := r.openRequest(fctx, deadline)
	stop()
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	r.seekable = resp.StatusCode == http.StatusPartialContent || strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
	r.size = responseSize(resp)
	r.cancel = cancel
	go r.fetch(fctx, r.gen, 0, resp, reqCancel)
	return r, nil
}

// Size is the total length in bytes, or -1 if the server didn't say.
func (r *Reader) Size() int64 { r.mu.Lock(); defer r.mu.Unlock(); return r.size }

// Seekable reports whether the server honours Range requests.
func (r *Reader) Seekable() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.seekable }

// Buffered is how many bytes ahead of the read position are in memory.
func (r *Reader) Buffered() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pos < r.lo || r.pos > r.hi {
		return 0
	}
	return r.hi - r.pos
}

// Capacity is the bytes the ring holds now: the prefetch ring until Promote.
func (r *Reader) Capacity() int64 { r.mu.Lock(); defer r.mu.Unlock(); return int64(len(r.ring)) }

// Holds reports whether byte off is in memory, so that seeking there starts
// no request (unless the window moves on first).
func (r *Reader) Holds(off int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && off >= r.lo && off < r.hi
}

// SeekIfBuffered moves the read position to off if that byte is in memory,
// so it never starts a request, and reports whether it did. A Read waiting
// for data wakes and continues from there.
func (r *Reader) SeekIfBuffered(off int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || off < r.lo || off >= r.hi {
		return false
	}
	r.pos = off
	r.cond.Broadcast()
	return true
}

// Promote lifts the prefetch limit and grows the ring to the full window.
// The new ring is allocated without the lock (a Read meanwhile isn't held up
// by up to 32 MiB of zeroing); what the fetcher added during that is copied
// over under it. A second Promote meanwhile doesn't allocate again.
func (r *Reader) Promote() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.prefetch = 0
	r.cond.Broadcast()
	grow := int64(len(r.ring)) < r.o.WindowBytes && !r.promoting
	r.promoting = grow
	r.mu.Unlock()
	if !grow {
		return
	}
	ring := r.alloc(r.o.WindowBytes)
	r.mu.Lock()
	r.promoting = false
	if !r.closed && int64(len(r.ring)) < int64(len(ring)) {
		r.growLocked(ring)
		r.cond.Broadcast()
	}
	r.mu.Unlock()
}

// growLocked moves the buffered bytes [lo, hi) into ring, which is larger.
func (r *Reader) growLocked(ring []byte) {
	n := int64(len(ring))
	old := int64(len(r.ring))
	for off := r.lo; off < r.hi; {
		s := off % old
		k := min(old-s, r.hi-off)
		d := off % n
		c := int64(copy(ring[d:], r.ring[s:s+k]))
		copy(ring, r.ring[s+c:s+k]) // the part that wraps in the new ring, if any
		off += k
	}
	r.ring = ring
}

func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		switch {
		case r.closed:
			return 0, ErrClosed
		case r.size >= 0 && r.pos >= r.size:
			return 0, io.EOF
		case r.pos >= r.lo && r.pos < r.hi:
			n := r.copyOut(p)
			r.pos += int64(n)
			r.cond.Broadcast()
			return n, nil
		case r.eof && r.pos >= r.hi:
			return 0, io.EOF
		case r.err != nil:
			return 0, r.err
		case r.pos < r.lo:
			// Back-seek during in-flight fetch moved pos below lo; restart.
			if r.seekable {
				r.restartLocked(r.pos)
			} else {
				r.restartLocked(0)
			}
			continue
		}
		r.cond.Wait()
	}
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, ErrClosed
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		if r.size < 0 {
			return 0, errors.New("stream: SeekEnd with unknown size")
		}
		abs = r.size + offset
	default:
		return 0, errors.New("stream: bad whence")
	}
	if abs < 0 {
		return 0, errors.New("stream: negative position")
	}
	r.pos = abs
	inWindow := abs >= r.lo && abs <= r.hi+r.o.NearBytes
	if !inWindow && (abs < r.lo || r.seekable) {
		from := abs
		if !r.seekable {
			from = 0 // replay from the start and let the window skip forward
		}
		r.restartLocked(from)
	}
	r.cond.Broadcast()
	return abs, nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.cancel()
		r.cond.Broadcast()
	}
	return nil
}

func (r *Reader) restartLocked(from int64) {
	r.cancel()
	r.gen++
	r.lo, r.hi = from, from
	r.eof, r.err = false, nil
	if r.size >= 0 && from >= r.size {
		r.eof = true
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.fetch(ctx, r.gen, from, nil, nil)
}

// copyOut copies from the ring at pos; caller holds mu and ensured lo <= pos < hi.
func (r *Reader) copyOut(p []byte) int {
	n := int(min(int64(len(p)), r.hi-r.pos))
	c := int64(len(r.ring))
	start := int(r.pos % c)
	k := copy(p[:n], r.ring[start:])
	if k < n {
		copy(p[k:n], r.ring)
	}
	return n
}

// effectiveLo is the lowest offset we must keep: BehindBytes behind pos, or
// a quarter of the ring while it is still the small prefetch ring (keeping
// more than the ring holds would leave the fetcher no room at all).
func (r *Reader) effectiveLo() int64 {
	behind := min(r.o.BehindBytes, int64(len(r.ring))/4)
	lo := max(r.lo, r.pos-behind)
	return min(lo, r.hi)
}

// room is how many bytes the fetcher may append now.
func (r *Reader) room() int64 {
	room := int64(len(r.ring)) - (r.hi - r.effectiveLo())
	if r.prefetch > 0 {
		// The cap is relative to the reader: a decoder that must read past
		// PrefetchBytes to open (big embedded art, an MP3 length scan) keeps
		// going instead of deadlocking at an absolute offset.
		room = min(room, r.pos+r.prefetch-r.hi)
	}
	return room
}

func (r *Reader) appendLocked(data []byte) {
	r.lo = r.effectiveLo()
	c := int64(len(r.ring))
	for len(data) > 0 {
		start := int(r.hi % c)
		k := copy(r.ring[start:], data)
		data = data[k:]
		r.hi += int64(k)
	}
	// Clamp lo to ensure the invariant hi - lo <= len(ring).
	r.lo = max(r.lo, r.hi-c)
}

// stripURL removes the URL from *url.Error to avoid leaking credentials.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = fmt.Errorf("stream: %s: %w", ue.Op, ue.Err)
	}
	return err
}

// openRequest is the first request. A server that asks to come back later
// (429 and the like) is retried within RetryBudget and ctx; anything else
// fails Open at once. A wait that would end after deadline (zero: none) is
// not taken: the server's answer is returned instead.
func (r *Reader) openRequest(ctx context.Context, deadline time.Time) (*http.Response, context.CancelFunc, error) {
	start := time.Now()
	var last error // the latest "come back later"
	for attempt := 0; ; attempt++ {
		resp, reqCancel, err := r.budgetedRequest(ctx, start.Add(r.o.RetryBudget))
		var he *HTTPError
		if err == nil || !errors.As(err, &he) || !retryLater(he.StatusCode) {
			if err != nil && last != nil && time.Since(start) >= r.o.RetryBudget {
				err = last // the budget cut this request short: the server's answer says more
			}
			return resp, reqCancel, err
		}
		last = err
		d := max(r.o.Backoff[min(attempt, len(r.o.Backoff)-1)], he.RetryAfter)
		if time.Since(start)+d > r.o.RetryBudget {
			return nil, nil, err
		}
		if !deadline.IsZero() && time.Now().Add(d).After(deadline) {
			return nil, nil, err // the wait would outlast the caller's ctx
		}
		t := time.NewTimer(d)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, nil, err
		}
	}
}

// budgetedRequest is request, cut off at limit: Open's retries end with the
// budget even when a request hangs.
func (r *Reader) budgetedRequest(ctx context.Context, limit time.Time) (*http.Response, context.CancelFunc, error) {
	lctx, lcancel := context.WithCancel(ctx)
	t := time.AfterFunc(time.Until(limit), lcancel)
	resp, reqCancel, err := r.request(lctx, 0)
	if !t.Stop() { // the limit passed: whatever came back is too late
		if err == nil {
			resp.Body.Close()
			reqCancel()
		}
		if err == nil || (ctx.Err() == nil && errors.Is(err, context.Canceled)) {
			err = errBudgetSpent // the limit cut it, not the caller
		}
		lcancel()
		return nil, nil, err
	}
	if err != nil {
		lcancel()
		return nil, nil, err
	}
	// The body outlives this call, so the limit's timer is already stopped;
	// the caller's cancel ends the request.
	return resp, func() { reqCancel(); lcancel() }, nil
}

func (r *Reader) request(ctx context.Context, off int64) (*http.Response, context.CancelFunc, error) {
	rctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, r.url, nil)
	if err != nil {
		cancel()
		return nil, nil, stripURL(err)
	}
	for k, v := range r.o.Header {
		req.Header[k] = v
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(off, 10)+"-")
	t := time.AfterFunc(r.o.StallTimeout, cancel)
	resp, err := r.o.Client.Do(req)
	if !t.Stop() {
		// Timer fired; either the response is partial/missing or we hit stall timeout.
		if err == nil {
			resp.Body.Close()
		}
		return nil, nil, errStalled
	}
	if err != nil {
		cancel()
		return nil, nil, stripURL(err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
	default:
		resp.Body.Close()
		cancel()
		return nil, nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	if r.o.CheckResponse != nil {
		if err := r.o.CheckResponse(resp); err != nil {
			resp.Body.Close()
			cancel()
			return nil, nil, err
		}
	}
	return resp, cancel, nil
}

func responseSize(resp *http.Response) int64 {
	if resp.StatusCode == http.StatusPartialContent {
		// Content-Range: bytes 0-99/1234
		cr := resp.Header.Get("Content-Range")
		if i := strings.LastIndexByte(cr, '/'); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				return n
			}
		}
		return -1
	}
	return resp.ContentLength
}

// fetch fills the ring from off onwards, reconnecting as needed, until EOF,
// a terminal error, or the generation changes.
func (r *Reader) fetch(ctx context.Context, gen int, off int64, resp *http.Response, reqCancel context.CancelFunc) {
	buf := make([]byte, 64<<10)
	attempt := 0
	lastProgress := time.Now()
	for {
		if resp == nil {
			var err error
			resp, reqCancel, err = r.request(ctx, off)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				var he *HTTPError
				if errors.As(err, &he) && he.StatusCode == http.StatusRequestedRangeNotSatisfiable {
					r.mu.Lock()
					size := r.size
					r.mu.Unlock()
					if size >= 0 && off < size {
						// Not the end: the file is shorter than it was (changed on
						// the server). Say so rather than end the track early.
						r.finish(gen, fmt.Errorf("stream: the server refused byte %d of %d (did the file change?): %w", off, size, err))
					} else {
						r.finish(gen, nil)
					}
					return
				}
				if errors.As(err, &he) && he.StatusCode < 500 && !retryLater(he.StatusCode) {
					r.finish(gen, err)
					return
				}
				if !r.sleepBackoff(ctx, gen, &attempt, lastProgress, err) {
					return
				}
				continue
			}
			// The file may have changed since the last connection: the
			// latest size is what a later 416 and the end are judged by.
			if n := responseSize(resp); n >= 0 {
				r.mu.Lock()
				if r.gen == gen {
					r.size = n
				}
				r.mu.Unlock()
			}
		}
		skip := int64(0)
		if resp.StatusCode == http.StatusOK {
			skip = off // server ignored Range and started at 0
		}
		progressed, err := r.copyBody(gen, resp.Body, reqCancel, &off, skip, buf)
		resp.Body.Close()
		reqCancel()
		resp = nil
		if err == nil || errors.Is(err, errStale) || ctx.Err() != nil {
			return
		}
		if progressed {
			attempt = 0
			lastProgress = time.Now()
		}
		if !r.sleepBackoff(ctx, gen, &attempt, lastProgress, err) {
			return
		}
	}
}

func (r *Reader) copyBody(gen int, body io.Reader, reqCancel context.CancelFunc, off *int64, skip int64, buf []byte) (progressed bool, err error) {
	for {
		r.mu.Lock()
		for r.gen == gen && !r.closed && r.room() <= 0 {
			r.cond.Wait()
		}
		if r.gen != gen || r.closed {
			r.mu.Unlock()
			return progressed, errStale
		}
		// The read below may overwrite everything under effectiveLo, so
		// those bytes are gone from now on: Holds and a seek back there must
		// not count on them while the lock is released.
		r.lo = r.effectiveLo()
		room := r.room()
		r.mu.Unlock()

		n := int(min(int64(len(buf)), room))
		if skip > 0 {
			n = int(min(int64(len(buf)), skip))
		}
		var stalled atomic.Bool
		t := time.AfterFunc(r.o.StallTimeout, func() { stalled.Store(true); reqCancel() })
		k, rerr := body.Read(buf[:n])
		if !t.Stop() && stalled.Load() {
			rerr = errStalled
		}
		data := buf[:k]
		if skip > 0 {
			d := min(skip, int64(len(data)))
			data = data[d:]
			skip -= d
		}
		if len(data) > 0 {
			r.mu.Lock()
			if r.gen != gen || r.closed {
				r.mu.Unlock()
				return progressed, errStale
			}
			r.appendLocked(data)
			*off = r.hi
			r.cond.Broadcast()
			r.mu.Unlock()
			progressed = true
		}
		if errors.Is(rerr, io.EOF) {
			r.mu.Lock()
			short := r.size >= 0 && r.hi < r.size
			r.mu.Unlock()
			if short {
				return progressed, io.ErrUnexpectedEOF
			}
			r.finish(gen, nil)
			return progressed, nil
		}
		if rerr != nil {
			return progressed, rerr
		}
	}
}

// finish marks the end of the stream (err == nil) or a terminal error.
func (r *Reader) finish(gen int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gen != gen {
		return
	}
	if err != nil {
		r.err = err
	} else {
		r.eof = true
		if r.size < 0 {
			r.size = r.hi
		}
	}
	r.cond.Broadcast()
}

func (r *Reader) sleepBackoff(ctx context.Context, gen int, attempt *int, lastProgress time.Time, cause error) bool {
	if time.Since(lastProgress) > r.o.RetryBudget {
		r.finish(gen, fmt.Errorf("stream: giving up: %w", cause))
		return false
	}
	d := r.o.Backoff[min(*attempt, len(r.o.Backoff)-1)]
	*attempt++
	var he *HTTPError
	if errors.As(cause, &he) && he.RetryAfter > d {
		if time.Since(lastProgress)+he.RetryAfter > r.o.RetryBudget {
			r.finish(gen, fmt.Errorf("stream: giving up: %w", cause))
			return false
		}
		d = he.RetryAfter
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
