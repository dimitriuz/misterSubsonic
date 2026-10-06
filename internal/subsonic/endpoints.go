package subsonic

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (c *Client) GetArtists(ctx context.Context) ([]ArtistIndex, error) {
	r, err := c.call(ctx, "getArtists", nil)
	if err != nil || r.Artists == nil {
		return nil, err
	}
	return r.Artists.Index, nil
}

func (c *Client) GetArtist(ctx context.Context, id ID) (*ArtistWithAlbums, error) {
	r, err := c.call(ctx, "getArtist", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Artist == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "artist not found"}
	}
	return r.Artist, nil
}

func (c *Client) GetAlbum(ctx context.Context, id ID) (*AlbumWithSongs, error) {
	r, err := c.call(ctx, "getAlbum", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Album == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "album not found"}
	}
	return r.Album, nil
}

func (c *Client) GetSong(ctx context.Context, id ID) (*Song, error) {
	r, err := c.call(ctx, "getSong", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Song == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "song not found"}
	}
	return r.Song, nil
}

// Album list types for GetAlbumList2.
const (
	ListNewest       = "newest"
	ListRecent       = "recent"
	ListFrequent     = "frequent"
	ListRandom       = "random"
	ListAlphabetical = "alphabeticalByName"
	ListByYear       = "byYear"
	ListByGenre      = "byGenre"
)

type AlbumListQuery struct {
	Type     string
	Size     int // default 100, max 500
	Offset   int
	FromYear int // ListByYear
	ToYear   int // ListByYear
	Genre    string
}

func (c *Client) GetAlbumList2(ctx context.Context, q AlbumListQuery) ([]Album, error) {
	if q.Size <= 0 {
		q.Size = 100
	}
	v := url.Values{"type": {q.Type}, "size": {strconv.Itoa(q.Size)}, "offset": {strconv.Itoa(q.Offset)}}
	switch q.Type {
	case ListByYear:
		v.Set("fromYear", strconv.Itoa(q.FromYear))
		v.Set("toYear", strconv.Itoa(q.ToYear))
	case ListByGenre:
		v.Set("genre", q.Genre)
	}
	r, err := c.call(ctx, "getAlbumList2", v)
	if err != nil || r.AlbumList2 == nil {
		return nil, err
	}
	return r.AlbumList2.Albums, nil
}

func (c *Client) GetGenres(ctx context.Context) ([]Genre, error) {
	r, err := c.call(ctx, "getGenres", nil)
	if err != nil || r.Genres == nil {
		return nil, err
	}
	return r.Genres.Genres, nil
}

func (c *Client) GetPlaylists(ctx context.Context) ([]Playlist, error) {
	r, err := c.call(ctx, "getPlaylists", nil)
	if err != nil || r.Playlists == nil {
		return nil, err
	}
	return r.Playlists.Playlists, nil
}

func (c *Client) GetPlaylist(ctx context.Context, id ID) (*PlaylistWithSongs, error) {
	r, err := c.call(ctx, "getPlaylist", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Playlist == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "playlist not found"}
	}
	return r.Playlist, nil
}

func (c *Client) GetStarred2(ctx context.Context) (*Starred, error) {
	r, err := c.call(ctx, "getStarred2", nil)
	if err != nil {
		return nil, err
	}
	if r.Starred2 == nil {
		return &Starred{}, nil
	}
	return r.Starred2, nil
}

type SearchQuery struct {
	ArtistCount, ArtistOffset int
	AlbumCount, AlbumOffset   int
	SongCount, SongOffset     int
}

func (c *Client) Search3(ctx context.Context, query string, q SearchQuery) (*SearchResult, error) {
	v := url.Values{
		"query":        {query},
		"artistCount":  {strconv.Itoa(q.ArtistCount)},
		"artistOffset": {strconv.Itoa(q.ArtistOffset)},
		"albumCount":   {strconv.Itoa(q.AlbumCount)},
		"albumOffset":  {strconv.Itoa(q.AlbumOffset)},
		"songCount":    {strconv.Itoa(q.SongCount)},
		"songOffset":   {strconv.Itoa(q.SongOffset)},
	}
	r, err := c.call(ctx, "search3", v)
	if err != nil {
		return nil, err
	}
	if r.SearchResult3 == nil {
		return &SearchResult{}, nil
	}
	return r.SearchResult3, nil
}

type StarTarget struct {
	SongIDs, AlbumIDs, ArtistIDs []ID
}

func (t StarTarget) values() url.Values {
	v := url.Values{}
	for _, id := range t.SongIDs {
		v.Add("id", string(id))
	}
	for _, id := range t.AlbumIDs {
		v.Add("albumId", string(id))
	}
	for _, id := range t.ArtistIDs {
		v.Add("artistId", string(id))
	}
	return v
}

func (c *Client) Star(ctx context.Context, t StarTarget) error {
	_, err := c.call(ctx, "star", t.values())
	return err
}

func (c *Client) Unstar(ctx context.Context, t StarTarget) error {
	_, err := c.call(ctx, "unstar", t.values())
	return err
}

// Scrobble reports a play. submission=false only updates "now playing".
func (c *Client) Scrobble(ctx context.Context, id ID, at time.Time, submission bool) error {
	v := url.Values{
		"id":         {string(id)},
		"time":       {strconv.FormatInt(at.UnixMilli(), 10)},
		"submission": {strconv.FormatBool(submission)},
	}
	_, err := c.call(ctx, "scrobble", v)
	return err
}

// GetPlayQueue returns the saved queue, or nil if there is none.
func (c *Client) GetPlayQueue(ctx context.Context) (*PlayQueue, error) {
	r, err := c.call(ctx, "getPlayQueue", nil)
	var ae *APIError
	if errors.As(err, &ae) && ae.Code == CodeNotFound {
		return nil, nil
	}
	if err != nil || r.PlayQueue == nil || len(r.PlayQueue.Songs) == 0 {
		return nil, err
	}
	return r.PlayQueue, nil
}

func (c *Client) SavePlayQueue(ctx context.Context, ids []ID, current ID, pos time.Duration) error {
	v := url.Values{}
	for _, id := range ids {
		v.Add("id", string(id))
	}
	if current != "" {
		v.Set("current", string(current))
		v.Set("position", strconv.FormatInt(pos.Milliseconds(), 10))
	}
	// A long queue does not fit a URL (servers cut such requests off), so it
	// goes in a form body. A server that does not take that gets the same
	// request as a GET.
	_, err := c.callPost(ctx, "savePlayQueue", v)
	if postUnsupported(err) {
		_, err = c.call(ctx, "savePlayQueue", v)
	}
	return err
}

// postUnsupported reports whether err means the server refused the POST
// itself, as opposed to the request: HTTP 404, 405, 415 or 501, or a Subsonic
// error that is not about credentials or permission.
func postUnsupported(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		switch he.Status {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnsupportedMediaType, http.StatusNotImplemented:
			return true
		}
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return Classify(err) == KindOther
	}
	return false
}
