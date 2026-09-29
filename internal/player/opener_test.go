package player

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
