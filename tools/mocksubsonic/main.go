// Command mocksubsonic serves a folder of audio files as a one-album
// Subsonic server, for exercising the client without a real Navidrome.
// It accepts any credentials and supports HTTP Range on stream.
//
//	go run ./tools/mocksubsonic -dir internal/audio/testdata -addr 127.0.0.1:4533
package main

import (
	"encoding/json"
	"flag"
	"fmt"
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

	http.HandleFunc("/rest/", func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
		q := r.URL.Query()
		log.Printf("%s %s range=%q", endpoint, q.Get("id"), r.Header.Get("Range"))
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
			reply(w, map[string]any{"artists": map[string]any{"index": []any{map[string]any{"name": "M", "artist": []any{map[string]any{"id": "ar-mock", "name": "Mock Artist", "albumCount": 1}}}}}})
		case "search3":
			reply(w, map[string]any{"searchResult3": map[string]any{"song": songs}})
		case "getPlayQueue":
			reply(w, nil)
		default:
			reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 0, "message": "mock does not implement " + endpoint}})
		}
	})
	log.Printf("serving %d songs from %s on http://%s", len(songs), *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
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
		out = append(out, song{
			ID: fmt.Sprintf("so-%d", i), Title: strings.TrimSuffix(n, filepath.Ext(n)), Album: "Mock Album", Artist: "Mock Artist",
			AlbumID: "al-mock", Track: i + 1, Suffix: strings.TrimPrefix(filepath.Ext(n), "."), ContentType: types[f],
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
