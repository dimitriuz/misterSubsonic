// Command mocksubsonic serves a folder of audio files as a one-album
// Subsonic server, for exercising the client without a real Navidrome.
// It accepts any credentials and supports HTTP Range on stream.
//
// A file whose name contains "-transcoded" is listed as an m4a (which the
// client can't decode, so it asks for a transcode). A stream request with
// a format other than "raw" is answered like a Navidrome transcode: HTTP
// 200, chunked, no Content-Length, no Accept-Ranges, Range ignored. The
// file's bytes are sent as they are, so a transcoded file must already be
// in the requested format. timeOffset is honoured for MP3 (the stream
// starts that share of the file in; frames resync) and ignored otherwise.
//
//	go run ./tools/mocksubsonic -dir internal/audio/testdata -addr 127.0.0.1:4533
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mistersubsonic/internal/audio"
)

type song struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Album       string `json:"album"`
	Artist      string `json:"artist"`
	AlbumID     string `json:"albumId"`
	Track       int    `json:"track"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	Duration    int    `json:"duration"`
	Size        int64  `json:"size"`
	path        string
}

func main() {
	dir := flag.String("dir", ".", "folder of .flac/.mp3/.wav files")
	addr := flag.String("addr", "127.0.0.1:4533", "listen address")
	flag.Parse()

	songs, err := scan(*dir)
	if err != nil || len(songs) == 0 {
		log.Fatalf("no audio files in %s (%v)", *dir, err)
	}
	album := map[string]any{"id": "al-mock", "name": "Mock Album", "artist": "Mock Artist", "artistId": "ar-mock", "songCount": len(songs)}
	artist := map[string]any{"id": "ar-mock", "name": "Mock Artist", "albumCount": 1}
	playlist := map[string]any{"id": "pl-mock", "name": "Mock Playlist", "owner": "test", "songCount": len(songs)}

	http.HandleFunc("/rest/", func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
		q := r.URL.Query()
		log.Printf("%s %s format=%q range=%q query=%q", endpoint, q.Get("id"), q.Get("format"), r.Header.Get("Range"), q.Get("query"))
		switch endpoint {
		case "stream":
			i, err := strconv.Atoi(strings.TrimPrefix(q.Get("id"), "so-"))
			if err != nil || i < 0 || i >= len(songs) {
				reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 70, "message": "not found"}})
				return
			}
			f, err := os.Open(songs[i].path)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer f.Close()
			if format := q.Get("format"); format != "raw" {
				if t, _ := strconv.Atoi(q.Get("timeOffset")); t > 0 && format == "mp3" && songs[i].Duration > 0 {
					f.Seek(songs[i].Size*int64(min(t, songs[i].Duration))/int64(songs[i].Duration), io.SeekStart)
				}
				serveTranscode(w, f, format)
				return
			}
			w.Header().Set("Content-Type", songs[i].ContentType)
			http.ServeContent(w, r, "", time.Time{}, f)
		case "ping", "scrobble", "savePlayQueue", "star", "unstar":
			reply(w, nil)
		case "getOpenSubsonicExtensions":
			reply(w, map[string]any{"openSubsonicExtensions": []any{}})
		case "getAlbum":
			a := map[string]any{"song": songs}
			for k, v := range album {
				a[k] = v
			}
			reply(w, map[string]any{"album": a})
		case "getAlbumList2":
			reply(w, map[string]any{"albumList2": map[string]any{"album": []any{album}}})
		case "getArtists":
			reply(w, map[string]any{"artists": map[string]any{"index": []any{map[string]any{"name": "M", "artist": []any{artist}}}}})
		case "getArtist":
			a := map[string]any{"album": []any{album}}
			for k, v := range artist {
				a[k] = v
			}
			reply(w, map[string]any{"artist": a})
		case "getGenres":
			reply(w, map[string]any{"genres": map[string]any{"genre": []any{map[string]any{"value": "Test", "albumCount": 1, "songCount": len(songs)}}}})
		case "getPlaylists":
			reply(w, map[string]any{"playlists": map[string]any{"playlist": []any{playlist}}})
		case "getPlaylist":
			p := map[string]any{"entry": songs}
			for k, v := range playlist {
				p[k] = v
			}
			reply(w, map[string]any{"playlist": p})
		case "getStarred2":
			reply(w, map[string]any{"starred2": map[string]any{}})
		case "search3":
			// Everything matches; only the first page has results.
			res := map[string]any{}
			if q.Get("artistOffset") == "0" && q.Get("artistCount") != "0" {
				res["artist"] = []any{artist}
			}
			if q.Get("albumOffset") == "0" && q.Get("albumCount") != "0" {
				res["album"] = []any{album}
			}
			if q.Get("songOffset") == "0" && q.Get("songCount") != "0" {
				res["song"] = songs
			}
			reply(w, map[string]any{"searchResult3": res})
		case "getPlayQueue":
			reply(w, nil)
		default:
			reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 0, "message": "mock does not implement " + endpoint}})
		}
	})
	log.Printf("serving %d songs from %s on http://%s", len(songs), *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// serveTranscode streams f the way Navidrome sends a live transcode: status
// 200 with chunked encoding (Flush before the handler returns, and never set
// Content-Length), no Accept-Ranges, and any Range header ignored.
func serveTranscode(w http.ResponseWriter, f *os.File, format string) {
	ct := map[string]string{"mp3": "audio/mpeg", "flac": "audio/flac", "wav": "audio/wav"}[format]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 16<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
		if err != nil {
			return
		}
	}
}

func reply(w http.ResponseWriter, payload map[string]any) {
	body := map[string]any{"status": "ok", "version": "1.16.1", "type": "mock", "openSubsonic": true}
	for k, v := range payload {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
}

func scan(dir string) ([]song, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if audio.FormatFromSuffix(filepath.Ext(e.Name())) != audio.FormatUnknown {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	types := map[audio.Format]string{audio.FormatFLAC: "audio/flac", audio.FormatMP3: "audio/mpeg", audio.FormatWAV: "audio/wav"}
	var out []song
	for i, n := range names {
		p := filepath.Join(dir, n)
		f := audio.FormatFromSuffix(filepath.Ext(n))
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		suffix, ct := strings.TrimPrefix(filepath.Ext(n), "."), types[f]
		if strings.Contains(n, "-transcoded") {
			suffix, ct = "m4a", "audio/mp4" // not decodable on the device: the client asks for a transcode
		}
		out = append(out, song{
			ID: fmt.Sprintf("so-%d", i), Title: strings.TrimSuffix(n, filepath.Ext(n)), Album: "Mock Album", Artist: "Mock Artist",
			AlbumID: "al-mock", Track: i + 1, Suffix: suffix, ContentType: ct,
			Duration: durationOf(p, f), Size: fi.Size(), path: p,
		})
	}
	return out, nil
}

func durationOf(p string, f audio.Format) int {
	file, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer file.Close()
	d, err := audio.OpenDecoder(file, f)
	if err != nil {
		return 0
	}
	defer d.Close()
	if d.SampleRate() == 0 {
		return 0
	}
	return int((d.LengthFrames() + uint64(d.SampleRate()) - 1) / uint64(d.SampleRate()))
}
