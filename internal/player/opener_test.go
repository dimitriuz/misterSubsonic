package player

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

// When transcoding isn't configured on the server, stream answers with an
// error document instead of audio; that must fail the track, not play noise.
func TestOpenerRejectsErrorDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stream.view") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":0,"message":"transcoding not configured"}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c, err := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	open := NewOpener(c, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
	_, err = open(context.Background(), subsonic.Song{ID: "x", Suffix: "m4a"}, 0, false)
	var ae *subsonic.APIError
	if !errors.As(err, &ae) || !strings.Contains(ae.Message, "transcoding") {
		t.Fatalf("err = %v, want the server's APIError", err)
	}
}

// An MP3 opened at an offset starts near it (after the ID3v2 tag, where the
// Xing table puts it: toc[50] is 139/256 of the audio), and the decoder
// plays from there.
func TestOpenerStartsAnMP3NearTheOffset(t *testing.T) {
	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3") // 44-byte ID3v2 tag
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	open := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})
	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
	op, err := open(context.Background(), song, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOpened(op)
	if op.Offset != time.Second {
		t.Fatalf("Offset %v, want 1s: the stream starts there", op.Offset)
	}
	base := 44 + int64(float64(len(mp3)-44)*139/256)
	head := make([]byte, 16)
	if _, err := io.ReadFull(op.Source, head); err != nil || !bytes.Equal(head, mp3[base:base+16]) {
		t.Fatalf("stream starts with %x, want the bytes at %d (%v)", head, base, err)
	}
	op.Source.Seek(0, io.SeekStart)
	dec, err := audio.OpenDecoder(op.Source, audio.FormatMP3)
	if err != nil {
		t.Fatal("the decoder can't start mid-file:", err)
	}
	defer dec.Close()
	if n, err := dec.Read(make([]float32, 2048)); n == 0 {
		t.Fatalf("decoded nothing from the middle: %v", err)
	}
}

// Without the size or length there's nothing to estimate from: the MP3
// opens at the start and the engine seeks.
func TestOpenerOpensAnMP3OfUnknownSizeAtTheStart(t *testing.T) {
	mp3, _ := os.ReadFile("../audio/testdata/tone-44k16.mp3")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), subsonic.Song{ID: "m", Suffix: "mp3"}, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOpened(op)
	head := make([]byte, 3)
	io.ReadFull(op.Source, head)
	if op.Offset != 0 || string(head) != "ID3" {
		t.Fatalf("Offset %v, starts with %q", op.Offset, head)
	}
}

func TestMP3Offset(t *testing.T) {
	tagged := append([]byte("ID3\x04\x00\x00\x00\x00\x01\x00"), make([]byte, 128)...) // a 128-byte tag
	for name, c := range map[string]struct {
		data []byte
		song subsonic.Song
		at   time.Duration
		want int64
		ok   bool
	}{
		"no tag":        {make([]byte, 16), subsonic.Song{Size: 1000, Duration: 100}, 50 * time.Second, 500, true},
		"tag":           {tagged, subsonic.Song{Size: 1138, Duration: 100}, 50 * time.Second, 138 + 500, true},
		"at the end":    {make([]byte, 16), subsonic.Song{Size: 1000, Duration: 100}, 200 * time.Second, 999, true},
		"no size":       {make([]byte, 16), subsonic.Song{Duration: 100}, time.Second, 0, false},
		"no duration":   {make([]byte, 16), subsonic.Song{Size: 1000}, time.Second, 0, false},
		"tag past size": {tagged, subsonic.Song{Size: 100, Duration: 100}, time.Second, 0, false},
	} {
		got, ok := probeMP3(bytes.NewReader(c.data)).offset(c.song, c.at)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %d,%v; want %d,%v", name, got, ok, c.want, c.ok)
		}
	}
}

// A VBR MP3 opened at an offset starts where its Xing table says, not where
// the proportion says.
func TestOpenerUsesTheXingTable(t *testing.T) {
	audioBytes := 25600
	mp3 := append(xingFrame(false, false, false, 6, 0, uint32(audioBytes), quadToc()), make([]byte, audioBytes)...)
	for i := range mp3 {
		if i >= 200 {
			mp3[i] = byte(i) // so the first bytes read say where the stream began
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	song := subsonic.Song{ID: "v", Suffix: "mp3", Size: int64(len(mp3)), Duration: 100}
	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), song, 50*time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOpened(op)
	want := int64(audioBytes) / 4 // halfway in time is a quarter of the bytes (toc[50] = 64)
	head := make([]byte, 8)
	io.ReadFull(op.Source, head)
	if !bytes.Equal(head, mp3[want:want+8]) {
		t.Fatalf("stream starts with %x, want the bytes at %d (%x)", head, want, mp3[want:want+8])
	}
}

// A seek inside the bytes the stream holds moves the same stream: the new
// source shares it, so no new request goes out, and closing one of the two
// sources doesn't end the other.
func TestMP3StreamRetargetsWithinItsWindow(t *testing.T) {
	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3", WindowBytes: 8192})(context.Background(), song, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	rt, ok := op.Source.(retargeter)
	if !ok {
		t.Fatalf("a sized MP3 opens as %T, which can't be retargeted", op.Source)
	}
	old := op.Source.(*fromOffset)
	waitBuffered := func(n int64) {
		for i := 0; old.s.r.Buffered() < n; i++ {
			if i > 2000 {
				t.Fatalf("stream never buffered %d bytes", n)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitBuffered(4096)
	// Probing the first frame reads past the 2 KiB this small window keeps
	// behind the reader, so going back to the start may have taken a second
	// request. Only what the retarget costs counts.
	opened := requests.Load()
	if _, _, ok := rt.retarget(song, time.Second); ok {
		t.Fatal("1s is ~11 KB in, beyond the 8 KiB window: it must not retarget")
	}
	next, prep, ok := rt.retarget(song, 100*time.Millisecond)
	if !ok || next.Offset != 100*time.Millisecond || next.Format != audio.FormatMP3 {
		t.Fatalf("retarget = %+v, %v", next, ok)
	}
	prep()
	base := 44 + int64(float64(len(mp3)-44)*34/256) // toc[5]
	head := make([]byte, 16)
	if _, err := io.ReadFull(next.Source, head); err != nil || !bytes.Equal(head, mp3[base:base+16]) {
		t.Fatalf("new source starts with %x, want the bytes at %d (%v)", head, base, err)
	}
	closeOpened(op) // the engine drops the old track
	if _, err := old.Read(head); err == nil {
		t.Fatal("a closed source still reads")
	}
	if _, err := next.Source.Read(head); err != nil {
		t.Fatalf("closing the old source ended the new one: %v", err)
	}
	if n := requests.Load(); n != opened {
		t.Fatalf("the retarget made %d requests, want none", n-opened)
	}
	closeOpened(next)
	if _, err := old.s.r.Read(head); err == nil {
		t.Fatal("the stream stayed open after both sources closed")
	}
}

// flakySeeker is a ReadSeeker whose seeks can be made to fail.
type flakySeeker struct {
	*bytes.Reader
	fail func(off int64) bool
}

func (f *flakySeeker) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekStart && f.fail(off) {
		return 0, errors.New("seek refused")
	}
	return f.Reader.Seek(off, whence)
}

func tonemp3(t *testing.T) []byte {
	t.Helper()
	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3")
	if err != nil {
		t.Fatal(err)
	}
	return mp3
}

// When the estimated start can't be sought to, the stream is back at its
// first byte, not left wherever the header probe stopped.
func TestPositionMP3FallsBackToTheStart(t *testing.T) {
	mp3 := tonemp3(t)
	song := subsonic.Song{Size: int64(len(mp3)), Duration: 2}
	for name, c := range map[string]struct {
		song   subsonic.Song
		offset time.Duration
		fail   func(int64) bool
	}{
		"estimate seek fails": {song, time.Second, func(off int64) bool { return off > 100 }},
		"no size to estimate": {subsonic.Song{Duration: 2}, time.Second, func(int64) bool { return false }},
	} {
		r := &flakySeeker{Reader: bytes.NewReader(mp3), fail: c.fail}
		_, _, at, placed, err := positionMP3(r, c.song, c.offset)
		if err != nil || placed || at != 0 {
			t.Fatalf("%s: placed %v at %v err %v", name, placed, at, err)
		}
		if pos, _ := r.Reader.Seek(0, io.SeekCurrent); pos != 0 {
			t.Errorf("%s: stream left at byte %d, want 0", name, pos)
		}
	}
}

// If even the rewind fails the stream can't be used: an error, not a source
// that reads from the middle of the header.
func TestPositionMP3ReportsAFailedRewind(t *testing.T) {
	mp3 := tonemp3(t)
	r := &flakySeeker{Reader: bytes.NewReader(mp3), fail: func(off int64) bool { return off > 100 || off == 0 }}
	song := subsonic.Song{Size: int64(len(mp3)), Duration: 2}
	if _, _, _, placed, err := positionMP3(r, song, time.Second); err == nil || placed {
		t.Fatalf("placed %v, err %v; want an error", placed, err)
	}
}

// A source that starts at byte base can't be seeked before it.
func TestFromOffsetRejectsSeeksBeforeItsBase(t *testing.T) {
	mp3 := tonemp3(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), song, time.Second, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeOpened(op)
	src := op.Source
	base := op.Source.(*fromOffset).base
	head := make([]byte, 8)
	io.ReadFull(src, head) // now 8 bytes in
	for name, seek := range map[string]func() (int64, error){
		"start":   func() (int64, error) { return src.Seek(-1, io.SeekStart) },
		"current": func() (int64, error) { return src.Seek(-9, io.SeekCurrent) },
		"end":     func() (int64, error) { return src.Seek(-int64(len(mp3))+base-1, io.SeekEnd) },
	} {
		if _, err := seek(); err == nil {
			t.Errorf("seek before the base from %s succeeded", name)
		}
		if pos, _ := src.Seek(0, io.SeekCurrent); pos != 8 {
			t.Fatalf("after the refused seek from %s the position is %d, want 8", name, pos)
		}
	}
	if pos, err := src.Seek(-8, io.SeekCurrent); err != nil || pos != 0 {
		t.Fatalf("seek to the base = %d, %v", pos, err)
	}
	if pos, err := src.Seek(-int64(len(mp3))+base, io.SeekEnd); err != nil || pos != 0 {
		t.Fatalf("seek to the base from the end = %d, %v", pos, err)
	}
}
