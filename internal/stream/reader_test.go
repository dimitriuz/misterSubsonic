package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pattern is the byte at offset off of every virtual test file.
func pattern(off int64) byte { return byte(off*31 + off>>13) }

// virtualFile is an io.ReadSeeker over a file of any size that is never stored.
type virtualFile struct{ size, pos int64 }

func (v *virtualFile) Read(p []byte) (int, error) {
	if v.pos >= v.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), v.size-v.pos))
	for i := 0; i < n; i++ {
		p[i] = pattern(v.pos + int64(i))
	}
	v.pos += int64(n)
	return n, nil
}

func (v *virtualFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		v.pos = off
	case io.SeekCurrent:
		v.pos += off
	case io.SeekEnd:
		v.pos = v.size + off
	}
	return v.pos, nil
}

type server struct {
	*httptest.Server
	mu       sync.Mutex
	ranges   []string
	requests atomic.Int32
}

func (s *server) rangeHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

// newServer serves a virtual file. handler may override behaviour per request
// (1-based request number); returning false falls through to ServeContent.
func newServer(t *testing.T, size int64, handler func(n int, w http.ResponseWriter, r *http.Request) bool) *server {
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.requests.Add(1))
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		s.mu.Unlock()
		if handler != nil && handler(n, w, r) {
			return
		}
		w.Header().Set("Content-Type", "audio/flac")
		http.ServeContent(w, r, "", time.Time{}, &virtualFile{size: size})
	}))
	t.Cleanup(s.Close)
	return s
}

// serveNoRanges writes the virtual file from offset 0 as a plain 200 with no range support.
func serveNoRanges(size int64) func(int, http.ResponseWriter, *http.Request) bool {
	return func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, &virtualFile{size: size})
		return true
	}
}

func testOptions() Options {
	return Options{
		WindowBytes:  1 << 20,
		StallTimeout: 200 * time.Millisecond,
		Backoff:      []time.Duration{10 * time.Millisecond},
		RetryBudget:  500 * time.Millisecond,
	}
}

func open(t *testing.T, url string, o Options) *Reader {
	t.Helper()
	r, err := Open(context.Background(), url, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func checkBytes(t *testing.T, got []byte, off int64) {
	t.Helper()
	for i, b := range got {
		if b != pattern(off+int64(i)) {
			t.Fatalf("byte at offset %d = %d, want %d", off+int64(i), b, pattern(off+int64(i)))
		}
	}
}

func readAt(t *testing.T, r *Reader, off int64, n int) []byte {
	t.Helper()
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read %d bytes at %d: %v", n, off, err)
	}
	return buf
}

func TestSequentialReadLargerThanWindow(t *testing.T) {
	const size = 10 << 20
	s := newServer(t, size, nil)
	r := open(t, s.URL, testOptions())
	if !r.Seekable() || r.Size() != size {
		t.Fatalf("Seekable=%v Size=%d", r.Seekable(), r.Size())
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("read %d bytes, want %d", len(got), size)
	}
	checkBytes(t, got, 0)
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("sequential read used %d requests, want 1", n)
	}
}

func TestSeekInsideWindowUsesNoNewRequest(t *testing.T) {
	s := newServer(t, 4<<20, nil)
	r := open(t, s.URL, testOptions())
	checkBytes(t, readAt(t, r, 0, 200<<10), 0)
	checkBytes(t, readAt(t, r, 100<<10, 50<<10), 100<<10) // back-seek within BehindBytes
	checkBytes(t, readAt(t, r, 300<<10, 10), 300<<10)     // forward, within Near
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("seeks inside the window made %d requests, want 1", n)
	}
}

func TestSeekFarIntoHugeFileUsesRange(t *testing.T) {
	const size = 3 << 30 // 3 GiB, never materialised
	s := newServer(t, size, nil)
	r := open(t, s.URL, testOptions())
	const target = int64(2)<<30 + 12345
	checkBytes(t, readAt(t, r, target, 4096), target)
	hs := s.rangeHeaders()
	if len(hs) != 2 || hs[1] != "bytes="+strconv.FormatInt(target, 10)+"-" {
		t.Fatalf("Range headers = %q", hs)
	}
	checkBytes(t, readAt(t, r, 1000, 10), 1000) // and back to the start
}

func TestSeekEndAndPastEnd(t *testing.T) {
	s := newServer(t, 1<<20, nil)
	r := open(t, s.URL, testOptions())
	pos, err := r.Seek(-10, io.SeekEnd)
	if err != nil || pos != 1<<20-10 {
		t.Fatalf("SeekEnd = %d, %v", pos, err)
	}
	got, err := io.ReadAll(r)
	if err != nil || len(got) != 10 {
		t.Fatalf("tail read %d bytes, %v", len(got), err)
	}
	checkBytes(t, got, 1<<20-10)
	r.Seek(5<<20, io.SeekStart)
	if n, err := r.Read(make([]byte, 10)); n != 0 || err != io.EOF {
		t.Fatalf("read past end = %d, %v; want 0, EOF", n, err)
	}
}

func TestServerWithoutRanges(t *testing.T) {
	const size = 4 << 20
	s := newServer(t, size, serveNoRanges(size))
	r := open(t, s.URL, testOptions())
	if r.Seekable() {
		t.Fatal("Seekable() = true for a server without range support")
	}
	checkBytes(t, readAt(t, r, 3<<20, 1000), 3<<20) // forward: streams through
	checkBytes(t, readAt(t, r, 10, 1000), 10)       // backward: replays from 0
	if n := s.requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
}

func TestReconnectAfterDroppedConnection(t *testing.T) {
	const size = 2 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyN(w, &virtualFile{size: size}, 300<<10)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // drop the connection mid-body
	})
	r := open(t, s.URL, testOptions())
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("read %d bytes, want %d", len(got), size)
	}
	checkBytes(t, got, 0)
	hs := s.rangeHeaders()
	if len(hs) < 2 || hs[1] == "bytes=0-" {
		t.Fatalf("reconnect did not resume from the last byte: %q", hs)
	}
}

func TestReconnectAfterStall(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyN(w, &virtualFile{size: size}, 100<<10)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // go silent until the client gives up
		return true
	})
	r := open(t, s.URL, testOptions())
	start := time.Now()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	checkBytes(t, got, 0)
	if len(got) != size {
		t.Fatalf("read %d bytes", len(got))
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("stall recovery took %v", time.Since(start))
	}
}

func TestGivesUpAfterRetryBudget(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(size))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			io.CopyN(w, &virtualFile{size: size}, 1000)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	r := open(t, s.URL, testOptions())
	got, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("ReadAll succeeded against a dead server")
	}
	if len(got) != 1000 {
		t.Fatalf("got %d bytes before the error, want the 1000 that arrived", len(got))
	}
	checkBytes(t, got, 0)
}

func TestOpenReportsHTTPError(t *testing.T) {
	s := newServer(t, 10, func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		http.NotFound(w, nil)
		return true
	})
	_, err := Open(context.Background(), s.URL, testOptions())
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 404 {
		t.Fatalf("err = %v, want HTTPError 404", err)
	}
}

func TestCheckResponseRejects(t *testing.T) {
	s := newServer(t, 10, func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
		return true
	})
	o := testOptions()
	bad := errors.New("error document")
	o.CheckResponse = func(resp *http.Response) error {
		if resp.Header.Get("Content-Type") == "application/json" {
			return bad
		}
		return nil
	}
	if _, err := Open(context.Background(), s.URL, o); !errors.Is(err, bad) {
		t.Fatalf("err = %v, want the CheckResponse error", err)
	}
}

func TestPrefetchLimitThenPromote(t *testing.T) {
	s := newServer(t, 8<<20, nil)
	o := testOptions()
	o.PrefetchBytes = 256 << 10
	r := open(t, s.URL, o)
	time.Sleep(100 * time.Millisecond)
	if b := r.Buffered(); b > 256<<10 {
		t.Fatalf("buffered %d bytes in prefetch mode, limit is %d", b, 256<<10)
	}
	r.Promote()
	deadline := time.Now().Add(2 * time.Second)
	for r.Buffered() <= 256<<10 {
		if time.Now().After(deadline) {
			t.Fatalf("buffer did not grow after Promote (buffered %d)", r.Buffered())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// C2: the prefetch cap limits how far the fetcher runs ahead of the reader,
// not an absolute offset. A decoder that must read past PrefetchBytes to
// open (large embedded art, an MP3 length scan) must not block there.
func TestPrefetchCapIsRelativeToReadPosition(t *testing.T) {
	const limit = 256 << 10
	s := newServer(t, 8<<20, nil)
	o := testOptions()
	o.PrefetchBytes = limit
	r := open(t, s.URL, o)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, make([]byte, 512<<10))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		r.Close()
		t.Fatal("reading 512 KiB blocked at the 256 KiB prefetch cap")
	}
	time.Sleep(50 * time.Millisecond)
	if b := r.Buffered(); b > limit {
		t.Fatalf("buffered %d bytes ahead of the reader, limit is %d", b, limit)
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	s := newServer(t, 1<<20, func(_ int, w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return true
	})
	o := testOptions()
	o.StallTimeout = time.Minute
	r := open(t, s.URL, o)
	errc := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 10))
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	r.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Read after Close = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
}

func TestWindowBoundsMemory(t *testing.T) {
	s := newServer(t, 64<<20, nil)
	r := open(t, s.URL, testOptions())
	time.Sleep(100 * time.Millisecond) // let it fill
	if b := r.Buffered(); b > 1<<20 {
		t.Fatalf("buffered %d bytes, window is %d", b, 1<<20)
	}
	// Reading still works across many window turnovers.
	buf := make([]byte, 5<<20)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	checkBytes(t, buf, 0)
}

func TestErrorsDoNotIncludeURL(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	_, err := Open(context.Background(), "http://"+addr+"/rest/stream?t=SECRET&s=SALT", testOptions())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v; must exist and must not contain the URL's credentials", err)
	}
}

// While paused the ring stays full and nothing is read from the body; that
// must not count as a stall or kill the stream.
func TestLongPauseDoesNotBreakStream(t *testing.T) {
	const size = 4 << 20
	s := newServer(t, size, nil)
	o := testOptions() // StallTimeout 200 ms
	r := open(t, s.URL, o)
	checkBytes(t, readAt(t, r, 0, 1000), 0)
	time.Sleep(4 * o.StallTimeout) // "paused"
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size-1000 {
		t.Fatalf("read %d bytes after pause, want %d", len(got), size-1000)
	}
	checkBytes(t, got, 1000)
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("pause caused %d requests, want 1 (pause must not count as a stall)", n)
	}
}

// TestBackSeekDuringFetchKeepsDataCorrect verifies that a back-seek during an
// in-flight fetch doesn't overflow the ring and corrupt data.
// The fetcher is already allowed to write 16 KiB before the seek happens.
// Without the capacity clamp, appendLocked would leave lo at 0 while hi grows
// to 80K, so hi-lo exceeds the 64K ring and the slot for offset 1K gets
// overwritten by offset 65K, corrupting data. With the clamp, lo is forced up
// to 16K, which is above pos=1K, so Read restarts the fetch at the right offset.
func TestBackSeekDuringFetchKeepsDataCorrect(t *testing.T) {
	const size = 2 << 20
	release := make(chan struct{})
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusPartialContent)
		// Write exactly 64 KiB (the window), flush, then block until release.
		io.CopyN(w, &virtualFile{size: size}, 64<<10)
		w.(http.Flusher).Flush()
		<-release
		io.Copy(w, &virtualFile{size: size, pos: 64 << 10})
		return true
	})
	o := Options{
		WindowBytes:  64 << 10,
		BehindBytes:  16 << 10,
		NearBytes:    256 << 10,
		StallTimeout: time.Minute, // Don't interfere with the test
		Backoff:      testOptions().Backoff,
		RetryBudget:  testOptions().RetryBudget,
	}
	r := open(t, s.URL, o)

	// Wait until the ring is full: Buffered() == 64 KiB (lo=0, hi=64K).
	deadline := time.Now().Add(2 * time.Second)
	for r.Buffered() < 64<<10 {
		if time.Now().After(deadline) {
			t.Fatalf("ring did not fill to 64 KiB, buffered=%d", r.Buffered())
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Read 32 KiB (pos becomes 32K). The fetcher computes room = 16K and blocks
	// in body.Read waiting for the server.
	buf := make([]byte, 32<<10)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		t.Fatal(err)
	}
	checkBytes(t, buf, 0)

	// Give the fetcher a moment to compute room and block in body.Read.
	time.Sleep(50 * time.Millisecond)

	// Seek back to 1 KiB (inside the window, so no restart here).
	_, err = r.Seek(1<<10, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}

	// Release the server. The fetcher writes the 16 KiB it was allowed and
	// calls appendLocked, whose capacity clamp raises lo to hi-len(ring)
	// regardless of pos.
	close(release)

	// Wait deterministically for the fetcher's append (or the restart it
	// triggers) to land, so the reads below happen after it instead of
	// racing it. Buffered() starts at hi-pos = 64K-1K = 63K and moves once
	// that happens.
	initialBuffered := int64(63 << 10) // 64K - 1K (after seek to 1K)
	deadline = time.Now().Add(2 * time.Second)
	for r.Buffered() == initialBuffered {
		if time.Now().After(deadline) {
			t.Fatalf("fetcher did not append in time")
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Now try to read. If the clamp is missing, the ring is corrupted and we'll
	// get pattern(65K) instead of pattern(1K) at offset 1K.
	got := make([]byte, 4<<10)
	_, err = io.ReadFull(r, got)
	if err != nil {
		t.Fatal(err)
	}
	checkBytes(t, got, 1<<10)

	// Read more to exercise the complete recovery.
	forward := make([]byte, 64<<10)
	_, err = io.ReadFull(r, forward)
	if err != nil {
		t.Fatal(err)
	}
	checkBytes(t, forward, 1<<10+4<<10)

	// With the clamp, pos < lo triggers a restart, so we see 2 requests.
	// Without the clamp, lo stays too low and we might see 1 request (or read
	// corrupted data instead of restarting).
	if n := s.requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2 (pos<lo must restart the fetch)", n)
	}
}

// TestReconnectWhenHeadersNeverArrive verifies that a server hanging before
// response headers triggers the stall timeout.
func TestReconnectWhenHeadersNeverArrive(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			// First request: serve part of the body then abort.
			w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
			w.Header().Set("Content-Length", strconv.Itoa(size))
			w.WriteHeader(http.StatusPartialContent)
			io.CopyN(w, &virtualFile{size: size}, 300<<10)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		if n == 2 {
			// Second request: hang before headers.
			<-r.Context().Done()
			return true
		}
		// Third+ request: serve normally.
		return false
	})
	r := open(t, s.URL, testOptions())
	start := time.Now()
	got, err := io.ReadAll(r)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("read %d bytes, want %d", len(got), size)
	}
	checkBytes(t, got, 0)
	if elapsed > 5*time.Second {
		t.Fatalf("reconnect took %v, should be ~3s", elapsed)
	}
}

// A prefetching reader (a queued next track) holds a small ring until it is
// promoted, then grows to the full window without losing what it buffered.
func TestPrefetchRingIsSmallUntilPromoted(t *testing.T) {
	s := newServer(t, 8<<20, nil)
	o := testOptions()
	o.PrefetchBytes = 64 << 10
	r := open(t, s.URL, o)
	r.mu.Lock()
	small := len(r.ring)
	r.mu.Unlock()
	if small != 80<<10 {
		t.Fatalf("prefetch ring %d bytes, want %d", small, 80<<10)
	}
	buf := make([]byte, 200<<10) // wraps the small ring more than twice
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	checkBytes(t, buf, 0)
	time.Sleep(50 * time.Millisecond) // let the fetcher fill up to the cap
	r.Promote()
	r.mu.Lock()
	big := len(r.ring)
	r.mu.Unlock()
	if big != 1<<20 {
		t.Fatalf("promoted ring %d bytes, want %d", big, 1<<20)
	}
	buf = make([]byte, 300<<10)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	checkBytes(t, buf, 200<<10) // what was buffered before Promote came across intact
	reqs := s.requests.Load()
	checkBytes(t, readAt(t, r, 400<<10, 64<<10), 400<<10) // behind the reader, inside the grown window
	if s.requests.Load() != reqs {
		t.Fatal("a seek inside the grown window made a new request")
	}
}

// A reader opened without a prefetch limit gets the whole window at once.
func TestPlainReaderHasTheFullWindow(t *testing.T) {
	s := newServer(t, 1<<20, nil)
	r := open(t, s.URL, testOptions())
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ring) != 1<<20 {
		t.Fatalf("ring %d bytes", len(r.ring))
	}
}

// dropAfter serves the first n bytes of the file as a 206, then drops the connection.
func dropAfter(w http.ResponseWriter, size, n int64) {
	w.Header().Set("Content-Range", "bytes 0-"+strconv.FormatInt(size-1, 10)+"/"+strconv.FormatInt(size, 10))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusPartialContent)
	io.CopyN(w, &virtualFile{size: size}, n)
	w.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}

// A 416 on reconnect before the end means the file changed: an error, not
// a quiet early end of the track.
func TestRangeRefusedMidFileIsAnError(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			dropAfter(w, size, 100<<10)
		}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return true
	})
	r := open(t, s.URL, testOptions())
	got, err := io.ReadAll(r)
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("ReadAll = %d bytes, %v; want the 416 as an error", len(got), err)
	}
	checkBytes(t, got, 0)
}

// Mid-stream, a 429 is retried after the server's Retry-After.
func TestTooManyRequestsIsRetriedAfterRetryAfter(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		switch n {
		case 1:
			dropAfter(w, size, 100<<10)
		case 2:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	})
	o := testOptions()
	o.RetryBudget = 3 * time.Second
	r := open(t, s.URL, o)
	start := time.Now()
	got, err := io.ReadAll(r)
	if err != nil || len(got) != size {
		t.Fatalf("ReadAll = %d bytes, %v", len(got), err)
	}
	checkBytes(t, got, 0)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("retried after %v, before the server's Retry-After of 1 s", d)
	}
}

// A Retry-After longer than the retry budget gives up at once instead of
// sleeping through it.
func TestRetryAfterBeyondTheBudgetGivesUp(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			dropAfter(w, size, 100<<10)
		}
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	})
	r := open(t, s.URL, testOptions())
	start := time.Now()
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("ReadAll succeeded")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("gave up after %v", d)
	}
}

// The first request is retried too when the server asks to come back later;
// other errors still fail Open at once.
func TestOpenRetriesWhenTheServerIsBusy(t *testing.T) {
	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	r := open(t, s.URL, testOptions())
	checkBytes(t, readAt(t, r, 0, 1000), 0)
	if n := s.requests.Load(); n != 3 {
		t.Fatalf("%d requests, want 3", n)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for h, want := range map[string]time.Duration{
		"": 0, "5": 5 * time.Second, " 2 ": 2 * time.Second, "-1": 0, "soon": 0,
		"Tue, 29 Sep 2026 12:00:30 GMT": 30 * time.Second, "Tue, 29 Sep 2026 11:00:00 GMT": 0,
	} {
		if got := parseRetryAfter(h, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", h, got, want)
		}
	}
}
