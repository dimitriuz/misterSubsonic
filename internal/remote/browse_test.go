package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

// fakeLib answers with fixed data, records the queries it got, and returns err
// (when set) from every call. block makes a call wait for its context.
type fakeLib struct {
	mu     sync.Mutex
	err    error
	block  bool
	albums []subsonic.Album
	lastAL subsonic.AlbumListQuery
	lastQ  string
	lastSQ subsonic.SearchQuery
}

func (f *fakeLib) fail(ctx context.Context) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

var (
	sSong  = subsonic.Song{ID: "s1", Title: "T", Artist: "A", Album: "B", CoverArt: "c9", Duration: 200, Starred: "2024-01-01"}
	sAlbum = subsonic.Album{ID: "al1", Name: "Alb", Artist: "Art", ArtistID: "ar1", CoverArt: "c1", Year: 1999, SongCount: 2}
	sArt   = subsonic.Artist{ID: "ar1", Name: "Art", AlbumCount: 3, CoverArt: "c2"}
)

func (f *fakeLib) GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
	f.mu.Lock()
	f.lastAL = q
	f.mu.Unlock()
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	if f.albums != nil {
		return f.albums, nil
	}
	return []subsonic.Album{sAlbum}, nil
}
func (f *fakeLib) GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.AlbumWithSongs{Album: sAlbum, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.ArtistIndex{{Name: "A", Artists: subsonic.List[subsonic.Artist]{sArt}}, {Name: "B"}}, nil
}
func (f *fakeLib) GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.ArtistWithAlbums{Artist: sArt, Albums: subsonic.List[subsonic.Album]{sAlbum}}, nil
}
func (f *fakeLib) GetGenres(ctx context.Context) ([]subsonic.Genre, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.Genre{{Name: "Rock", SongCount: 10, AlbumCount: 2}}, nil
}
func (f *fakeLib) GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.Playlist{{ID: "p1", Name: "Mix", SongCount: 4, CoverArt: "c3"}}, nil
}
func (f *fakeLib) GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.PlaylistWithSongs{Playlist: subsonic.Playlist{ID: "p1", Name: "Mix", SongCount: 1}, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) GetStarred2(ctx context.Context) (*subsonic.Starred, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.Starred{Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
	f.mu.Lock()
	f.lastQ, f.lastSQ = query, q
	f.mu.Unlock()
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.SearchResult{Artists: subsonic.List[subsonic.Artist]{sArt}, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}

// libCtl is a fakeCtl with a Library and a Cover.
type libCtl struct {
	*fakeCtl
	lib   Library
	cover func(ctx context.Context, id string, size int) ([]byte, string, error)
	size  int
}

func (c *libCtl) Library() Library { return c.lib }
func (c *libCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	c.size = size
	return c.cover(ctx, id, size)
}

func browse(lib Library) (*Server, *libCtl) {
	c := &libCtl{fakeCtl: newFake(), lib: lib}
	s := New(c, Options{})
	s.libTimeout = 200 * time.Millisecond
	return s, c
}

func get(s *Server, path string) (int, string) {
	w := do(s.Handler(), "GET", path, "", nil)
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestBrowseJSON(t *testing.T) {
	s, _ := browse(&fakeLib{})
	songJSON := `{"id":"s1","title":"T","artist":"A","album":"B","cover_id":"c9","duration_ms":200000,"starred":true}`
	albJSON := `{"id":"al1","name":"Alb","artist":"Art","artist_id":"ar1","year":1999,"cover_id":"c1","song_count":2}`
	cases := []struct{ path, want string }{
		{"/api/artists", `[{"letter":"A","artists":[{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2"}]},{"letter":"B","artists":[]}]`},
		{"/api/artist/ar1", `{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2","albums":[` + albJSON + `]}`},
		{"/api/albums?list=recent", `{"albums":[` + albJSON + `],"more":false}`},
		{"/api/album/al1", `{"id":"al1","name":"Alb","artist":"Art","artist_id":"ar1","year":1999,"cover_id":"c1","song_count":2,"songs":[` + songJSON + `]}`},
		{"/api/playlists", `[{"id":"p1","name":"Mix","song_count":4,"cover_id":"c3"}]`},
		{"/api/playlist/p1", `{"id":"p1","name":"Mix","song_count":1,"cover_id":"","songs":[` + songJSON + `]}`},
		{"/api/starred", `{"artists":[],"albums":[],"songs":[` + songJSON + `]}`},
		{"/api/genres", `[{"name":"Rock","album_count":2,"song_count":10}]`},
		{"/api/genre/Rock", `{"albums":[` + albJSON + `],"more":false}`},
		{"/api/search?q=ab", `{"artists":[{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2"}],"albums":[],"songs":[` + songJSON + `]}`},
	}
	for _, c := range cases {
		if code, body := get(s, c.path); code != 200 || body != c.want {
			t.Errorf("%s: %d\n got  %s\n want %s", c.path, code, body, c.want)
		}
	}
}

func TestAlbumListTypesAndPaging(t *testing.T) {
	lib := &fakeLib{}
	s, _ := browse(lib)
	for list, typ := range map[string]string{"recent": subsonic.ListRecent, "random": subsonic.ListRandom, "newest": subsonic.ListNewest} {
		if code, _ := get(s, "/api/albums?list="+list+"&offset=100"); code != 200 {
			t.Fatalf("%s: %d", list, code)
		}
		if q := lib.lastAL; q.Type != typ || q.Offset != 100 || q.Size != 51 {
			t.Errorf("%s: query %+v", list, q)
		}
	}
	if code, _ := get(s, "/api/genre/Hip%20Hop?offset=50"); code != 200 {
		t.Fatal(code)
	}
	if q := lib.lastAL; q.Type != subsonic.ListByGenre || q.Genre != "Hip Hop" || q.Offset != 50 {
		t.Errorf("genre query %+v", q)
	}
	// 51 back means a next page: the 51st is not sent.
	for i := 0; i < 51; i++ {
		lib.albums = append(lib.albums, subsonic.Album{ID: subsonic.ID(fmt.Sprint(i))})
	}
	_, body := get(s, "/api/albums?list=newest")
	if !strings.HasSuffix(body, `"more":true}`) || strings.Count(body, `"id"`) != 50 {
		t.Fatalf("full page: %d ids, %s", strings.Count(body, `"id"`), body[len(body)-20:])
	}
	lib.albums = lib.albums[:50]
	if _, body := get(s, "/api/albums?list=newest"); !strings.HasSuffix(body, `"more":false}`) {
		t.Fatal("exactly 50 must not say more")
	}
	lib.albums = []subsonic.Album{}
	if _, body := get(s, "/api/albums?list=newest"); body != `{"albums":[],"more":false}` {
		t.Fatal(body)
	}
}

func TestBrowseBadParams(t *testing.T) {
	s, _ := browse(&fakeLib{})
	for _, p := range []string{
		"/api/albums", "/api/albums?list=frequent", "/api/albums?list=recent&offset=-1",
		"/api/albums?list=recent&offset=x", "/api/genre/Rock?offset=-5",
		"/api/search", "/api/search?q=a", "/api/search?q=%20a%20", "/api/search?q=%C3%A9",
	} {
		if code, body := get(s, p); code != 400 || !strings.HasPrefix(body, `{"error":`) {
			t.Errorf("%s: %d %s", p, code, body)
		}
	}
}

func TestSearchCounts(t *testing.T) {
	lib := &fakeLib{}
	s, _ := browse(lib)
	if code, _ := get(s, "/api/search?q=%20%20Bj%C3%B6rk%20"); code != 200 {
		t.Fatal(code)
	}
	if lib.lastQ != "Björk" || lib.lastSQ != (subsonic.SearchQuery{ArtistCount: 20, AlbumCount: 20, SongCount: 50}) {
		t.Fatalf("%q %+v", lib.lastQ, lib.lastSQ)
	}
	// two characters, counted as characters, are enough
	if code, _ := get(s, "/api/search?q=%C3%A9%C3%A9"); code != 200 {
		t.Fatal(code)
	}
}

func TestBrowseErrors(t *testing.T) {
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused to https://music.example/rest/ping?u=bob&t=abc&s=xyz")}
	cases := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{"unreachable", netErr, 503, "server unreachable"},
		{"not found", &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no such thing at ?u=bob&t=abc"}, 404, "not found"},
		{"auth", &subsonic.APIError{Code: subsonic.CodeWrongCredentials, Message: "bad u=bob t=abc"}, 503, "wrong credentials"},
		{"other", errors.New("Get \"https://h/rest/getAlbum?u=bob&t=abc&s=xyz&id=1\": weird"), 500, "the library failed"},
	}
	paths := []string{"/api/artists", "/api/artist/x", "/api/albums?list=recent", "/api/album/x", "/api/playlists",
		"/api/playlist/x", "/api/starred", "/api/genres", "/api/genre/x", "/api/search?q=abc"}
	for _, c := range cases {
		s, _ := browse(&fakeLib{err: c.err})
		for _, p := range paths {
			code, body := get(s, p)
			if code != c.code || body != `{"error":"`+c.msg+`"}` {
				t.Errorf("%s %s: %d %s", c.name, p, code, body)
			}
			for _, leak := range []string{"u=", "t=", "s=", "http", "bob", "abc"} {
				if strings.Contains(body, leak) {
					t.Errorf("%s %s leaks %q: %s", c.name, p, leak, body)
				}
			}
		}
	}
}

func TestBrowseTimeoutAndNoLibrary(t *testing.T) {
	s, _ := browse(&fakeLib{block: true})
	start := time.Now()
	code, body := get(s, "/api/artists")
	if code != 503 || body != `{"error":"timed out"}` {
		t.Fatalf("%d %s", code, body)
	}
	if d := time.Since(start); d < 150*time.Millisecond || d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	s, _ = browse(nil)
	if code, body := get(s, "/api/artists"); code != 503 || !strings.Contains(body, "no library") {
		t.Fatalf("%d %s", code, body)
	}
	if New(newFake(), Options{}).libTimeout != 10*time.Second {
		t.Fatal("default timeout is not 10 s")
	}
}

func TestCover(t *testing.T) {
	s, c := browse(&fakeLib{})
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		if id == "none" {
			return nil, "", &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "u=bob"}
		}
		return []byte("\xff\xd8jpeg"), "image/jpeg", nil
	}
	w := do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 200 || w.Body.String() != "\xff\xd8jpeg" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %q %v", w.Code, w.Body, w.Header())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "max-age=86400" {
		t.Fatalf("Cache-Control %q", cc)
	}
	for q, want := range map[string]int{"": 300, "?size=10": 64, "?size=64": 64, "?size=200": 200, "?size=600": 600, "?size=5000": 600, "?size=-3": 64, "?size=abc": 300} {
		do(s.Handler(), "GET", "/api/cover/c1"+q, "", nil)
		if c.size != want {
			t.Errorf("size%s: got %d want %d", q, c.size, want)
		}
	}
	w = do(s.Handler(), "GET", "/api/cover/none", "", nil)
	if w.Code != 404 || strings.TrimSpace(w.Body.String()) != `{"error":"no cover"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") == "max-age=86400" {
		t.Fatal("an error must not be cached")
	}
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return nil, "", errors.New("Get https://h/rest/getCoverArt?u=bob&t=abc")
	}
	w = do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 404 || strings.Contains(w.Body.String(), "u=") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return nil, "", &net.OpError{Op: "dial", Err: errors.New("u=bob")}
	}
	w = do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "u=") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestBrowseGuarded(t *testing.T) {
	s, _ := browse(&fakeLib{})
	r := do(s.Handler(), "GET", "/api/artists", "", nil)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	fr := httptest.NewRequest("GET", "/api/artists", nil)
	fr.Host = "evil.example"
	fw := httptest.NewRecorder()
	s.Handler().ServeHTTP(fw, fr)
	if fw.Code != 403 {
		t.Fatalf("foreign host on a browse route: %d", fw.Code)
	}
	r = do(s.Handler(), "GET", "/", "", nil)
	if r.Code != 404 {
		t.Fatalf("page route: %d", r.Code)
	}
}

func TestCoverTypesAndSize(t *testing.T) {
	s, c := browse(&fakeLib{})
	serve := func(ct string, body []byte) *httptest.ResponseRecorder {
		c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) { return body, ct, nil }
		return do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	}
	for _, ct := range []string{"text/html", "image/svg+xml", "", "application/octet-stream", "garbage;;"} {
		w := serve(ct, []byte("<script>x</script>"))
		if w.Code != 404 || strings.TrimSpace(w.Body.String()) != `{"error":"no cover"}` {
			t.Errorf("%q: %d %s", ct, w.Code, w.Body)
		}
	}
	for _, ct := range []string{"image/jpeg", "image/png", "image/gif", "image/webp"} {
		if w := serve(ct, []byte("x")); w.Code != 200 || w.Header().Get("Content-Type") != ct {
			t.Errorf("%q: %d %q", ct, w.Code, w.Header().Get("Content-Type"))
		}
	}
	w := serve("image/jpeg; charset=x", []byte("x"))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("parameters: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "sandbox; default-src 'none'" {
		t.Errorf("CSP %q", csp)
	}
	if w := serve("image/jpeg", make([]byte, 8<<20)); w.Code != 200 {
		t.Errorf("8 MiB: %d", w.Code)
	}
	if w := serve("image/jpeg", make([]byte, 8<<20+1)); w.Code != 404 {
		t.Errorf("over 8 MiB: %d", w.Code)
	}
}

func TestBrowseLengthCaps(t *testing.T) {
	s, c := browse(&fakeLib{})
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return []byte("x"), "image/png", nil
	}
	long := strings.Repeat("a", 257)
	for _, p := range []string{
		"/api/artist/" + long, "/api/album/" + long, "/api/playlist/" + long, "/api/genre/" + long,
		"/api/cover/" + long,
		"/api/search?q=" + strings.Repeat("a", 201),
		"/api/albums?list=recent&offset=100001", "/api/genre/Rock?offset=100001",
	} {
		if code, body := get(s, p); code != 400 || !strings.HasPrefix(body, `{"error":`) {
			t.Errorf("%.40s: %d %.60s", p, code, body)
		}
	}
	for _, p := range []string{
		"/api/album/" + strings.Repeat("a", 256), "/api/search?q=" + strings.Repeat("a", 200),
		"/api/albums?list=recent&offset=100000",
	} {
		if code, _ := get(s, p); code != 200 {
			t.Errorf("%.40s: %d", p, code)
		}
	}
}
