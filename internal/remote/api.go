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

// ErrNoConnection is what the Controller returns when there is no server
// connection to act on (503).
var ErrNoConnection = errors.New("remote: no connection")

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
	MaxPlay     = maxPlayIDs // most songs one play request may start with (the UI caps an artist at it)
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

// cmdBody is what /api/cmd decodes: the Command the Controller sees, with the
// fields whose zero value is a real setting (loudest volume, seek to the
// start, off) shadowed by pointers, so an absent field can be told from zero.
type cmdBody struct {
	Command
	PositionMS *int64   `json:"position_ms"`
	DB         *float64 `json:"db"`
	On         *bool    `json:"on"`
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	var b cmdBody
	if !readBody(w, r, &b) {
		return
	}
	c, err := validate(&b)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.ctl.Do(c); err != nil {
		s.writeCtlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// validate checks the fields the command uses, requires the ones whose
// absence would silently mean a zero value, and clamps the volume.
func validate(b *cmdBody) (Command, error) {
	c := b.Command
	if b.PositionMS != nil {
		c.PositionMS = *b.PositionMS
	}
	if b.DB != nil {
		c.DB = *b.DB
	}
	if b.On != nil {
		c.On = *b.On
	}
	switch c.Do {
	case "toggle", "next", "prev", "clear":
	case "mute", "shuffle", "star":
		if b.On == nil {
			return c, errors.New("on is required")
		}
	case "seek":
		if b.PositionMS == nil {
			return c, errors.New("position_ms is required")
		}
		if c.PositionMS < 0 {
			return c, errors.New("position_ms must not be negative")
		}
	case "volume":
		if b.DB == nil {
			return c, errors.New("db is required")
		}
		if math.IsNaN(c.DB) {
			return c, errors.New("db is not a number")
		}
		c.DB = math.Max(minVolumeDB, math.Min(0, c.DB))
	case "repeat":
		if c.Mode != "off" && c.Mode != "all" && c.Mode != "one" {
			return c, errors.New("mode must be off, all or one")
		}
	case "jump", "remove":
		if c.Index < 0 {
			return c, errors.New("index must not be negative")
		}
	case "move":
		if c.From < 0 || c.To < 0 {
			return c, errors.New("from and to must not be negative")
		}
	default:
		return c, fmt.Errorf("unknown command %q", c.Do)
	}
	return c, nil
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
		s.writeCtlError(w, err)
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

// writeCtlError maps a Controller error to a status. Only the known kinds put
// their text in the body; anything else may carry a server URL, a path or
// credentials, so it goes to the log and the page gets a fixed text.
func (s *Server) writeCtlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrStale):
		writeError(w, http.StatusConflict, ErrStale.Error())
	case errors.Is(err, ErrBusy):
		writeError(w, http.StatusServiceUnavailable, ErrBusy.Error())
	case errors.Is(err, ErrNoConnection):
		writeError(w, http.StatusServiceUnavailable, ErrNoConnection.Error())
	default:
		// A library error out of Play: told by its kind, never by its text,
		// which can hold the server URL and credentials (as in browse.go).
		if k := subsonic.Classify(err); k != subsonic.KindOther {
			writeLibError(w, err)
			return
		}
		if s.opts.Log != nil {
			s.opts.Log("remote: internal error: %v", err)
		}
		writeError(w, http.StatusInternalServerError, "internal error")
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	w.Write(append(b, '\n'))
}
