package remote

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mistersubsonic/internal/subsonic"
)

// The browse, search and cover routes. They are read-only and call the
// Library from the request goroutine, each call under libTimeout.
const (
	defaultLibTimeout = 10 * time.Second
	albumPage         = 50
	minSearch         = 2
	searchArtists     = 20
	searchAlbums      = 20
	searchSongs       = 50
	defaultCover      = 300
	maxCoverBytes     = 8 << 20
	maxQueryRunes     = 200
	maxIDBytes        = 256
	maxOffset         = 100000
)

// coverSizes are the only sizes asked of the server (the nearest at or above
// the request; the last is the largest), so its cache holds few variants.
var coverSizes = []int{96, 200, 300, 500, 600}

// coverTypes are the only types a cover may be served as. Anything else, such
// as HTML or SVG, would run as part of the page's own origin.
var coverTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

// tooLong answers 400 and reports true when a path value is over maxIDBytes.
func tooLong(w http.ResponseWriter, v string) bool {
	if len(v) > maxIDBytes {
		writeError(w, http.StatusBadRequest, "the name or id is too long")
		return true
	}
	return false
}

type artistJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
	CoverID    string `json:"cover_id"`
}

type albumJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artist_id"`
	Year      int    `json:"year"`
	CoverID   string `json:"cover_id"`
	SongCount int    `json:"song_count"`
}

type playlistJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"song_count"`
	CoverID   string `json:"cover_id"`
}

type letterJSON struct {
	Letter  string       `json:"letter"`
	Artists []artistJSON `json:"artists"`
}

type genreJSON struct {
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
	SongCount  int    `json:"song_count"`
}

func artistOf(a subsonic.Artist) artistJSON {
	return artistJSON{string(a.ID), a.Name, a.AlbumCount, string(a.CoverArt)}
}

func albumOf(a subsonic.Album) albumJSON {
	return albumJSON{string(a.ID), a.Name, a.Artist, string(a.ArtistID), a.Year, string(a.CoverArt), a.SongCount}
}

func songOf(s subsonic.Song) Song {
	return Song{
		ID: string(s.ID), Title: s.Title, Artist: s.Artist, Album: s.Album,
		CoverID: string(s.CoverArt), DurationMS: int64(s.Duration) * 1000, Starred: s.Starred != "",
	}
}

// Each of these returns a non-nil slice, so the page gets [] and never null.
func artistsOf(l []subsonic.Artist) []artistJSON {
	out := make([]artistJSON, len(l))
	for i, a := range l {
		out[i] = artistOf(a)
	}
	return out
}

func albumsOf(l []subsonic.Album) []albumJSON {
	out := make([]albumJSON, len(l))
	for i, a := range l {
		out[i] = albumOf(a)
	}
	return out
}

func songsOf(l []subsonic.Song) []Song {
	out := make([]Song, len(l))
	for i, s := range l {
		out[i] = songOf(s)
	}
	return out
}

// lib returns the Library and a context with the library timeout, or answers
// 503 and reports false when the app has no Subsonic client.
func (s *Server) lib(w http.ResponseWriter, r *http.Request) (Library, context.Context, context.CancelFunc, bool) {
	l := s.ctl.Library()
	if l == nil {
		writeError(w, http.StatusServiceUnavailable, "no library")
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.libTimeout)
	return l, ctx, cancel, true
}

// writeLibError answers a failed library call. The message is only ever the
// error's kind, never the error text, which can hold the server URL and the
// credentials the client put in it.
func writeLibError(w http.ResponseWriter, err error) {
	switch k := subsonic.Classify(err); k {
	case subsonic.KindNotFound:
		writeError(w, http.StatusNotFound, k.String())
	case subsonic.KindOther:
		writeError(w, http.StatusInternalServerError, "the library failed")
	default:
		writeError(w, http.StatusServiceUnavailable, k.String())
	}
}

// libJSON runs one library call and writes its result, or the error.
func libJSON[T any](s *Server, w http.ResponseWriter, r *http.Request, call func(context.Context, Library) (T, error)) {
	l, ctx, cancel, ok := s.lib(w, r)
	if !ok {
		return
	}
	defer cancel()
	v, err := call(ctx, l)
	if err != nil {
		writeLibError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]letterJSON, error) {
		idx, err := l.GetArtists(ctx)
		out := make([]letterJSON, len(idx))
		for i, x := range idx {
			out[i] = letterJSON{x.Name, artistsOf(x.Artists)}
		}
		return out, err
	})
}

func (s *Server) handleArtist(w http.ResponseWriter, r *http.Request) {
	if tooLong(w, r.PathValue("id")) {
		return
	}
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		a, err := l.GetArtist(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			artistJSON
			Albums []albumJSON `json:"albums"`
		}{artistOf(a.Artist), albumsOf(a.Albums)}, nil
	})
}

// albumPageJSON is one page of albums and whether another follows.
type albumPageJSON struct {
	Albums []albumJSON `json:"albums"`
	More   bool        `json:"more"`
}

// listAlbums writes one page of the query: it asks for one album more than a
// page, which is how it knows there is a next page, and does not send it.
func (s *Server) listAlbums(w http.ResponseWriter, r *http.Request, q subsonic.AlbumListQuery) {
	off := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxOffset {
			writeError(w, http.StatusBadRequest, "offset must be a number, 0 to 100000")
			return
		}
		off = n
	}
	q.Size, q.Offset = albumPage+1, off
	libJSON(s, w, r, func(ctx context.Context, l Library) (albumPageJSON, error) {
		al, err := l.GetAlbumList2(ctx, q)
		more := len(al) > albumPage
		if more {
			al = al[:albumPage]
		}
		return albumPageJSON{albumsOf(al), more}, err
	})
}

func (s *Server) handleAlbums(w http.ResponseWriter, r *http.Request) {
	typ := map[string]string{"recent": subsonic.ListRecent, "random": subsonic.ListRandom, "newest": subsonic.ListNewest}[r.URL.Query().Get("list")]
	if typ == "" {
		writeError(w, http.StatusBadRequest, "list must be recent, random or newest")
		return
	}
	s.listAlbums(w, r, subsonic.AlbumListQuery{Type: typ})
}

func (s *Server) handleGenre(w http.ResponseWriter, r *http.Request) {
	if tooLong(w, r.PathValue("name")) {
		return
	}
	s.listAlbums(w, r, subsonic.AlbumListQuery{Type: subsonic.ListByGenre, Genre: r.PathValue("name")})
}

func (s *Server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	if tooLong(w, r.PathValue("id")) {
		return
	}
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		a, err := l.GetAlbum(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			albumJSON
			Songs []Song `json:"songs"`
		}{albumOf(a.Album), songsOf(a.Songs)}, nil
	})
}

func playlistOf(p subsonic.Playlist) playlistJSON {
	return playlistJSON{string(p.ID), p.Name, p.SongCount, string(p.CoverArt)}
}

func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]playlistJSON, error) {
		pl, err := l.GetPlaylists(ctx)
		out := make([]playlistJSON, len(pl))
		for i, p := range pl {
			out[i] = playlistOf(p)
		}
		return out, err
	})
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	if tooLong(w, r.PathValue("id")) {
		return
	}
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		p, err := l.GetPlaylist(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			playlistJSON
			Songs []Song `json:"songs"`
		}{playlistOf(p.Playlist), songsOf(p.Songs)}, nil
	})
}

// found is the artists, albums and songs of a starred or search answer.
type found struct {
	Artists []artistJSON `json:"artists"`
	Albums  []albumJSON  `json:"albums"`
	Songs   []Song       `json:"songs"`
}

func (s *Server) handleStarred(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		st, err := l.GetStarred2(ctx)
		if err != nil {
			return nil, err
		}
		return found{artistsOf(st.Artists), albumsOf(st.Albums), songsOf(st.Songs)}, nil
	})
}

func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]genreJSON, error) {
		gl, err := l.GetGenres(ctx)
		out := make([]genreJSON, len(gl))
		for i, g := range gl {
			out[i] = genreJSON{g.Name, g.AlbumCount, g.SongCount}
		}
		return out, err
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < minSearch || n > maxQueryRunes {
		writeError(w, http.StatusBadRequest, "type 2 to 200 characters")
		return
	}
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		res, err := l.Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchArtists, AlbumCount: searchAlbums, SongCount: searchSongs})
		if err != nil {
			return nil, err
		}
		return found{artistsOf(res.Artists), albumsOf(res.Albums), songsOf(res.Songs)}, nil
	})
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	if tooLong(w, r.PathValue("id")) {
		return
	}
	size := defaultCover
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = coverSizes[len(coverSizes)-1]
			for _, c := range coverSizes {
				if n <= c {
					size = c
					break
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.libTimeout)
	defer cancel()
	b, ct, err := s.ctl.Cover(ctx, r.PathValue("id"), size)
	if err != nil {
		// A server that is down is 503; anything else means it has no such cover.
		if k := subsonic.Classify(err); k != subsonic.KindOther && k != subsonic.KindNotFound && !errors.Is(err, context.Canceled) {
			writeError(w, http.StatusServiceUnavailable, k.String())
		} else {
			writeError(w, http.StatusNotFound, "no cover")
		}
		return
	}
	mt, _, perr := mime.ParseMediaType(ct)
	if perr != nil || !coverTypes[mt] || len(b) > maxCoverBytes {
		writeError(w, http.StatusNotFound, "no cover")
		return
	}
	h := w.Header()
	h.Set("Content-Type", mt)
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", "max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	w.Write(b)
}
