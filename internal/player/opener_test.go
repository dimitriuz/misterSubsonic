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

// An MP3 opened at an offset starts near it (after the ID3v2 tag, in
// proportion to time), and the decoder plays from there.
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
	base := 44 + (len(mp3)-44)/2
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
		got, ok := mp3Offset(bytes.NewReader(c.data), c.song, c.at)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %d,%v; want %d,%v", name, got, ok, c.want, c.ok)
		}
	}
}
