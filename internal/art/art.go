// Package art loads album cover images for the UI: memory LRU of decoded
// images, then the on-disk cache, then the server's getCoverArt. Loads run
// on background workers, newest request first (what's on screen now).
package art

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// Key identifies one cover at one pixel size (the server resizes).
type Key struct {
	ID   subsonic.ID
	Size int
}

func (k Key) String() string { return fmt.Sprintf("%s@%d", k.ID, k.Size) }

// Fetcher downloads encoded image bytes.
type Fetcher func(ctx context.Context, k Key) ([]byte, error)

// HTTPFetcher fetches from the server through c's authenticated URL.
func HTTPFetcher(c *subsonic.Client) Fetcher {
	return func(ctx context.Context, k Key) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.CoverArtURL(k.ID, k.Size), nil)
		if err != nil {
			return nil, fmt.Errorf("art: %s: bad request", k)
		}
		resp, err := c.HTTPClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("art: %s: request failed", k) // never echo the URL (credentials)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			return nil, fmt.Errorf("art: %s: HTTP %d %s", k, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	}
}

type Options struct {
	Fetch    Fetcher
	Disk     *cache.Disk // may be nil
	MemBytes int64       // decoded-image budget; default 48 MiB
	Workers  int         // default 2
	// Ready is called (from a worker goroutine) when a requested image
	// becomes available or fails; the UI uses it to schedule a redraw.
	Ready func(Key)
	// RetryAfter is how long a failed key is not retried; default 1 min.
	RetryAfter time.Duration
	Now        func() time.Time
}

// maxPending bounds the request stack; the oldest requests are dropped.
const maxPending = 64

// maxFailed bounds the failed-key map (a server with thousands of missing
// covers must not grow it forever); the oldest failures are forgotten first.
const maxFailed = 512

type entry struct {
	key   Key
	img   *gfx.Image
	bytes int64
}

type Loader struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	cond     *sync.Cond
	lru      *list.List // front = most recent
	items    map[Key]*list.Element
	memBytes int64
	pending  []Key // stack: newest last, at most maxPending
	queued   map[Key]bool
	inflight map[Key]bool
	failed   map[Key]time.Time
	closed   bool
}

func New(o Options) *Loader {
	if o.MemBytes <= 0 {
		o.MemBytes = 48 << 20
	}
	if o.Workers <= 0 {
		o.Workers = 2
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	l := &Loader{o: o, lru: list.New(), items: map[Key]*list.Element{}, inflight: map[Key]bool{}, queued: map[Key]bool{}, failed: map[Key]time.Time{}}
	l.cond = sync.NewCond(&l.mu)
	l.ctx, l.cancel = context.WithCancel(context.Background())
	for i := 0; i < o.Workers; i++ {
		l.wg.Add(1)
		go l.worker()
	}
	return l
}

// Get returns the image if it is in memory; otherwise it queues a load (once)
// and returns false. An empty ID never loads.
func (l *Loader) Get(k Key) (*gfx.Image, bool) {
	if k.ID == "" || k.Size <= 0 {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[k]; ok {
		l.lru.MoveToFront(el)
		return el.Value.(*entry).img, true
	}
	if l.closed || l.inflight[k] {
		return nil, false
	}
	if t, ok := l.failed[k]; ok && l.o.Now().Sub(t) < l.o.RetryAfter {
		return nil, false
	}
	if l.queued[k] {
		n := len(l.pending)
		if l.pending[n-1] == k {
			return nil, false
		}
		for i := n - 2; i >= 0; i-- { // move to the top in place
			if l.pending[i] == k {
				copy(l.pending[i:], l.pending[i+1:])
				l.pending[n-1] = k
				break
			}
		}
		return nil, false
	}
	if len(l.pending) >= maxPending {
		delete(l.queued, l.pending[0])
		copy(l.pending, l.pending[1:])
		l.pending = l.pending[:len(l.pending)-1]
	}
	l.pending = append(l.pending, k)
	l.queued[k] = true
	l.cond.Signal()
	return nil, false
}

// Bytes returns the encoded cover as the disk cache has it, else as the
// server sends it (and caches that), for callers that pass the image on
// undecoded. It runs on the caller's goroutine and doesn't touch the memory
// LRU.
func (l *Loader) Bytes(ctx context.Context, k Key) ([]byte, error) {
	if k.ID == "" || k.Size <= 0 {
		return nil, fmt.Errorf("art: no cover %s", k)
	}
	if l.o.Disk != nil {
		if data, ok := l.o.Disk.Get(k.String()); ok {
			return data, nil
		}
	}
	data, err := l.o.Fetch(ctx, k)
	if err != nil {
		return nil, err
	}
	if l.o.Disk != nil {
		l.o.Disk.Put(k.String(), data)
	}
	return data, nil
}

// Close stops the workers.
func (l *Loader) Close() {
	l.mu.Lock()
	l.closed = true
	l.cond.Broadcast()
	l.mu.Unlock()
	l.cancel()
	l.wg.Wait()
}

func (l *Loader) worker() {
	defer l.wg.Done()
	for {
		l.mu.Lock()
		for len(l.pending) == 0 && !l.closed {
			l.cond.Wait()
		}
		if l.closed {
			l.mu.Unlock()
			return
		}
		k := l.pending[len(l.pending)-1]
		l.pending = l.pending[:len(l.pending)-1]
		delete(l.queued, k)
		l.inflight[k] = true
		l.mu.Unlock()

		img, err := l.load(k)

		l.mu.Lock()
		delete(l.inflight, k)
		if err != nil {
			l.noteFailedLocked(k)
		} else {
			delete(l.failed, k)
			l.insertLocked(k, img)
		}
		l.mu.Unlock()
		if l.o.Ready != nil && l.ctx.Err() == nil {
			l.o.Ready(k)
		}
	}
}

// noteFailedLocked remembers a failure. Past maxFailed, the ones that may
// be retried anyway go first, then the oldest.
func (l *Loader) noteFailedLocked(k Key) {
	now := l.o.Now()
	l.failed[k] = now
	if len(l.failed) <= maxFailed {
		return
	}
	for fk, t := range l.failed {
		if now.Sub(t) >= l.o.RetryAfter {
			delete(l.failed, fk)
		}
	}
	for len(l.failed) > maxFailed {
		var oldest Key
		var ot time.Time
		first := true
		for fk, t := range l.failed {
			if first || t.Before(ot) {
				oldest, ot, first = fk, t, false
			}
		}
		delete(l.failed, oldest)
	}
}

func (l *Loader) load(k Key) (*gfx.Image, error) {
	if l.o.Disk != nil {
		if data, ok := l.o.Disk.Get(k.String()); ok {
			if img, err := gfx.DecodeImage(data, k.Size, k.Size); err == nil {
				return img, nil
			}
			l.o.Disk.Delete(k.String()) // corrupt entry: drop it and refetch once
		}
	}
	ctx, cancel := context.WithTimeout(l.ctx, 15*time.Second)
	defer cancel()
	data, err := l.o.Fetch(ctx, k)
	if err != nil {
		return nil, err
	}
	img, err := gfx.DecodeImage(data, k.Size, k.Size)
	if err != nil {
		return nil, err
	}
	if l.o.Disk != nil {
		l.o.Disk.Put(k.String(), data)
	}
	return img, nil
}

func (l *Loader) insertLocked(k Key, img *gfx.Image) {
	e := &entry{key: k, img: img, bytes: int64(4 * img.W * img.H)}
	l.items[k] = l.lru.PushFront(e)
	l.memBytes += e.bytes
	for l.memBytes > l.o.MemBytes && l.lru.Len() > 1 {
		old := l.lru.Back()
		oe := old.Value.(*entry)
		l.lru.Remove(old)
		delete(l.items, oe.key)
		l.memBytes -= oe.bytes
	}
}
