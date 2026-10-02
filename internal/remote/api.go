package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"mistersubsonic/internal/subsonic"
)

// ErrStale is what Controller.Do returns when a queue command names an index
// that is out of range or no longer holds the song the page saw (409).
var ErrStale = errors.New("remote: the queue changed")

// ErrBusy is what the Controller returns when the UI did not answer in time (503).
var ErrBusy = errors.New("remote: the MiSTer is busy")

// Controller is everything the server needs from the app. Tests use fakes.
type Controller interface {
	State() State
	Queue() QueueView
	// Do runs a playback or queue command and returns when it has run or been refused.
	Do(c Command) error
	// Play resolves the request to songs, starts or queues them, and returns how many.
	Play(p PlayRequest) (int, error)
	Library() Library
	// Cover returns the image bytes and content type; the bytes pass through undecoded.
	Cover(ctx context.Context, id string, size int) ([]byte, string, error)
}

// Library is the read-only part of the app's Subsonic client the browse and
// search routes use.
type Library interface {
	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
	GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error)
	GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error)
	GetGenres(ctx context.Context) ([]subsonic.Genre, error)
	GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error)
	GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error)
	GetStarred2(ctx context.Context) (*subsonic.Starred, error)
	Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error)
}

// Song is what the page knows about a song, and nothing else.
type Song struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	CoverID    string `json:"cover_id"`
	DurationMS int64  `json:"duration_ms"`
	Starred    bool   `json:"starred"`
}

// State is a snapshot of the player. Song is nil when nothing is loaded;
// Status is playing, paused, loading, buffering or stopped; Repeat is off,
// all or one.
type State struct {
	Song       *Song   `json:"song"`
	Status     string  `json:"status"`
	PositionMS int64   `json:"position_ms"`
	DurationMS int64   `json:"duration_ms"`
	VolumeDB   float64 `json:"volume_db"`
	Muted      bool    `json:"muted"`
	Shuffle    bool    `json:"shuffle"`
	Repeat     string  `json:"repeat"`
	Index      int     `json:"index"`
}

// QueueView is the queue and the index of the current song in it.
type QueueView struct {
	Index int    `json:"index"`
	Songs []Song `json:"songs"`
}

// Command is one POST /api/cmd body. Which fields count depends on Do.
// SongID is the id of the song the page saw at Index (jump, remove) or at
// From (move); the Controller answers ErrStale when it no longer matches.
type Command struct {
	Do         string  `json:"do"`
	PositionMS int64   `json:"position_ms"`
	DB         float64 `json:"db"`
	On         bool    `json:"on"`
	Mode       string  `json:"mode"`
	Index      int     `json:"index"`
	From       int     `json:"from"`
	To         int     `json:"to"`
	SongID     string  `json:"song_id"`
}

// PlayRequest is one POST /api/play body. What is album, playlist, artist
// (all its albums in order) or songs; How is now, next or end; Start is the
// song to begin at, for now.
type PlayRequest struct {
	What  string   `json:"what"`
	ID    string   `json:"id"`
	IDs   []string `json:"ids"`
	Start int      `json:"start"`
	How   string   `json:"how"`
}

// Volume range of a command, in dB.
const (
	minVolumeDB = -60
	maxPlayIDs  = 1000
)

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ctl.State())
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, queueOrEmpty(s.ctl.Queue()))
}

// queueOrEmpty makes the songs an empty list, not null, for the page.
func queueOrEmpty(q QueueView) QueueView {
	if q.Songs == nil {
		q.Songs = []Song{}
	}
	return q
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	var c Command
	if !readBody(w, r, &c) {
		return
	}
	if err := validate(&c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.ctl.Do(c); err != nil {
		writeCtlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// validate checks the fields the command uses and clamps the volume.
func validate(c *Command) error {
	switch c.Do {
	case "toggle", "next", "prev", "mute", "shuffle", "star", "clear":
	case "seek":
		if c.PositionMS < 0 {
			return errors.New("position_ms must not be negative")
		}
	case "volume":
		if math.IsNaN(c.DB) {
			return errors.New("db is not a number")
		}
		c.DB = math.Max(minVolumeDB, math.Min(0, c.DB))
	case "repeat":
		if c.Mode != "off" && c.Mode != "all" && c.Mode != "one" {
			return errors.New("mode must be off, all or one")
		}
	case "jump", "remove":
		if c.Index < 0 {
			return errors.New("index must not be negative")
		}
	case "move":
		if c.From < 0 || c.To < 0 {
			return errors.New("from and to must not be negative")
		}
	default:
		return fmt.Errorf("unknown command %q", c.Do)
	}
	return nil
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	var p PlayRequest
	if !readBody(w, r, &p) {
		return
	}
	if p.How == "" {
		p.How = "now"
	}
	if err := validatePlay(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.ctl.Play(p)
	if err != nil {
		writeCtlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": n})
}

func validatePlay(p *PlayRequest) error {
	switch p.What {
	case "album", "playlist", "artist":
		if p.ID == "" {
			return errors.New("id is required")
		}
	case "songs":
		if len(p.IDs) == 0 || len(p.IDs) > maxPlayIDs {
			return fmt.Errorf("ids must hold 1 to %d song ids", maxPlayIDs)
		}
	default:
		return fmt.Errorf("unknown what %q", p.What)
	}
	switch p.How {
	case "now", "next", "end":
	default:
		return fmt.Errorf("unknown how %q", p.How)
	}
	if p.Start < 0 {
		return errors.New("start must not be negative")
	}
	return nil
}

// readBody decodes the JSON body into v, or answers 400 (413 when the body is
// over the cap) and reports false.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

func writeCtlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrBusy):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":"encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(append(b, '\n'))
}
