package subsonic

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func connected(t *testing.T) (*fakeServer, *Client) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestGetArtistsHandlesSingleObjectList(t *testing.T) {
	_, c := connected(t)
	idx, err := c.GetArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 2 || len(idx[0].Artists) != 2 || len(idx[1].Artists) != 1 {
		t.Fatalf("index = %+v", idx)
	}
	if idx[0].Artists[1].Name != "Аквариум" || !idx[0].Artists[1].IsStarred() {
		t.Fatalf("artist = %+v", idx[0].Artists[1])
	}
	if idx[1].Artists[0].Name != "Björk" {
		t.Fatalf("single-object index decoded as %+v", idx[1].Artists)
	}
}

func TestGetAlbumDecodesSongsAndNumericIDs(t *testing.T) {
	s, c := connected(t)
	al, err := c.GetAlbum(ctx, "al-10")
	if err != nil {
		t.Fatal(err)
	}
	if s.query("getAlbum").Get("id") != "al-10" {
		t.Fatal("album id not sent")
	}
	if len(al.Songs) != 2 {
		t.Fatalf("songs = %d", len(al.Songs))
	}
	a, b := al.Songs[0], al.Songs[1]
	if a.Suffix != "flac" || a.BitDepth != 24 || a.SamplingRate != 96000 || a.Duration != 240 || a.Size != 41943040 {
		t.Fatalf("song 1 = %+v", a)
	}
	if a.ReplayGain == nil || a.ReplayGain.TrackGain == nil || *a.ReplayGain.TrackGain != -6.5 || *a.ReplayGain.AlbumGain != -7.1 {
		t.Fatalf("replayGain = %+v", a.ReplayGain)
	}
	if b.ID != "1002" || b.ReplayGain != nil || !b.IsStarred() {
		t.Fatalf("song 2 = %+v", b)
	}
}

func TestGetArtist(t *testing.T) {
	_, c := connected(t)
	ar, err := c.GetArtist(ctx, "ar-2")
	if err != nil || len(ar.Albums) != 2 || ar.Albums[0].Year != 1983 {
		t.Fatalf("artist = %+v, %v", ar, err)
	}
}

func TestGetAlbumList2Params(t *testing.T) {
	s, c := connected(t)
	list, err := c.GetAlbumList2(ctx, AlbumListQuery{Type: ListByYear, FromYear: 1980, ToYear: 1999, Offset: 100})
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	q := s.query("getAlbumList2")
	if q.Get("type") != "byYear" || q.Get("fromYear") != "1980" || q.Get("toYear") != "1999" || q.Get("size") != "100" || q.Get("offset") != "100" {
		t.Fatalf("query = %v", q)
	}
	c.GetAlbumList2(ctx, AlbumListQuery{Type: ListByGenre, Genre: "Rock"})
	if q := s.query("getAlbumList2"); q.Get("genre") != "Rock" || q.Has("fromYear") {
		t.Fatalf("byGenre query = %v", q)
	}
}

func TestGenresPlaylistsStarredSearch(t *testing.T) {
	s, c := connected(t)
	if g, err := c.GetGenres(ctx); err != nil || len(g) != 2 || g[0].Name != "Rock" {
		t.Fatalf("genres = %v, %v", g, err)
	}
	if p, err := c.GetPlaylists(ctx); err != nil || len(p) != 1 || p[0].Name != "Evening" {
		t.Fatalf("playlists = %v, %v", p, err)
	}
	if p, err := c.GetPlaylist(ctx, "pl-1"); err != nil || len(p.Songs) != 2 {
		t.Fatalf("playlist = %v, %v", p, err)
	}
	if st, err := c.GetStarred2(ctx); err != nil || st == nil || len(st.Songs) != 0 {
		t.Fatalf("starred = %v, %v", st, err)
	}
	res, err := c.Search3(ctx, "björk", SearchQuery{ArtistCount: 5, AlbumCount: 10, SongCount: 20, SongOffset: 20})
	if err != nil || len(res.Artists) != 1 || len(res.Songs) != 1 {
		t.Fatalf("search = %+v, %v", res, err)
	}
	q := s.query("search3")
	if q.Get("query") != "björk" || q.Get("artistCount") != "5" || q.Get("songOffset") != "20" {
		t.Fatalf("search query = %v", q)
	}
}

func TestStarAndScrobbleParams(t *testing.T) {
	s, c := connected(t)
	if err := c.Star(ctx, StarTarget{SongIDs: []ID{"so-1", "so-2"}, AlbumIDs: []ID{"al-10"}}); err != nil {
		t.Fatal(err)
	}
	q := s.query("star")
	if strings.Join(q["id"], ",") != "so-1,so-2" || q.Get("albumId") != "al-10" {
		t.Fatalf("star query = %v", q)
	}
	at := time.UnixMilli(1790000000123)
	if err := c.Scrobble(ctx, "so-1", at, true); err != nil {
		t.Fatal(err)
	}
	q = s.query("scrobble")
	if q.Get("id") != "so-1" || q.Get("time") != "1790000000123" || q.Get("submission") != "true" {
		t.Fatalf("scrobble query = %v", q)
	}
}

func TestPlayQueueRoundTrip(t *testing.T) {
	s, c := connected(t)
	pq, err := c.GetPlayQueue(ctx)
	if err != nil || pq == nil || pq.Current != "so-9" || pq.Position != 61500 || len(pq.Songs) != 2 {
		t.Fatalf("play queue = %+v, %v", pq, err)
	}
	s.override["getPlayQueue"] = `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
	if pq, err := c.GetPlayQueue(ctx); err != nil || pq != nil {
		t.Fatalf("empty play queue = %+v, %v; want nil, nil", pq, err)
	}
	s.override["getPlayQueue"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"no queue"}}}`
	if pq, err := c.GetPlayQueue(ctx); err != nil || pq != nil {
		t.Fatalf("not-found play queue = %+v, %v; want nil, nil", pq, err)
	}
	if err := c.SavePlayQueue(ctx, []ID{"so-1", "so-9"}, "so-9", 61500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	q := s.query("savePlayQueue")
	if strings.Join(q["id"], ",") != "so-1,so-9" || q.Get("current") != "so-9" || q.Get("position") != "61500" {
		t.Fatalf("savePlayQueue query = %v", q)
	}
}

func TestStreamAndCoverURLs(t *testing.T) {
	_, c := connected(t)
	u := c.StreamURL("so-1", StreamOptions{Format: "mp3", MaxBitRate: 320, TimeOffset: 42})
	for _, want := range []string{"/rest/stream.view?", "id=so-1", "format=mp3", "maxBitRate=320", "timeOffset=42", "t=", "s=", "u=alice"} {
		if !strings.Contains(u, want) {
			t.Errorf("stream URL %s lacks %q", u, want)
		}
	}
	if u := c.CoverArtURL("al-10", 300); !strings.Contains(u, "/rest/getCoverArt.view?") || !strings.Contains(u, "size=300") {
		t.Errorf("cover URL = %s", u)
	}
}

func TestMissingEntityIsNotFound(t *testing.T) {
	s, c := connected(t)
	s.override["getAlbum"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"Album not found"}}}`
	_, err := c.GetAlbum(ctx, "nope")
	if Classify(err) != KindNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestGetSong(t *testing.T) {
	s, c := connected(t)
	so, err := c.GetSong(ctx, "so-1")
	if err != nil {
		t.Fatal(err)
	}
	if s.query("getSong").Get("id") != "so-1" {
		t.Fatal("song id not sent")
	}
	if so.ID != "so-1" || so.Title != "Капитан Африка" || so.AlbumID != "al-10" || so.Suffix != "flac" || so.Duration != 240 {
		t.Fatalf("song = %+v", so)
	}
}

func TestGetSongMissingIsNotFound(t *testing.T) {
	s, c := connected(t)
	s.override["getSong"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"Song not found"}}}`
	if _, err := c.GetSong(ctx, "nope"); Classify(err) != KindNotFound {
		t.Fatalf("err = %v", err)
	}
	// An ok reply without a song is not found too.
	s.override["getSong"] = `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
	if _, err := c.GetSong(ctx, "nope"); Classify(err) != KindNotFound {
		t.Fatalf("empty reply: err = %v", err)
	}
}

func TestSavePlayQueuePostsAForm(t *testing.T) {
	s, c := connected(t)
	ids := make([]ID, 700)
	for i := range ids {
		ids[i] = ID(fmt.Sprintf("so-%d", i))
	}
	if err := c.SavePlayQueue(ctx, ids, "so-9", 61500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	method, ct, urlq := s.request("savePlayQueue")
	if method != "POST" || ct != "application/x-www-form-urlencoded" {
		t.Fatalf("savePlayQueue was %s %q", method, ct)
	}
	if urlq != "" {
		t.Fatalf("the URL carries a query (%d bytes); credentials and ids belong in the body", len(urlq))
	}
	q := s.query("savePlayQueue")
	if len(q["id"]) != 700 || q.Get("current") != "so-9" || q.Get("position") != "61500" || q.Get("t") == "" || q.Get("u") != "alice" {
		t.Fatalf("form = %d ids, current %q, position %q, t %q", len(q["id"]), q.Get("current"), q.Get("position"), q.Get("t"))
	}
}

func TestSavePlayQueueFallsBackToGet(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		s, c := connected(t)
		s.postStatus = status
		if err := c.SavePlayQueue(ctx, []ID{"so-1", "so-2"}, "so-2", time.Second); err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		method, _, _ := s.request("savePlayQueue")
		q := s.query("savePlayQueue")
		if method != "GET" || strings.Join(q["id"], ",") != "so-1,so-2" || q.Get("current") != "so-2" {
			t.Fatalf("status %d: fallback was %s %v", status, method, q)
		}
	}
}

// A server that refuses POSTs gets one request per save, not two, once the
// client has learned that.
func TestSavePlayQueueRemembersThatThePostFellBack(t *testing.T) {
	s, c := connected(t)
	s.postStatus = http.StatusMethodNotAllowed
	for i := 0; i < 3; i++ {
		if err := c.SavePlayQueue(ctx, []ID{"so-1", "so-2"}, "so-2", time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(s.methodList("savePlayQueue"), " "); got != "POST GET GET GET" {
		t.Fatalf("requests = %q, want one POST and then GETs only", got)
	}
}

// The memory is only for a GET that worked: a server that is down does not
// make the client give up on POST.
func TestSavePlayQueueDoesNotRememberAFailedFallback(t *testing.T) {
	s, c := connected(t)
	s.postStatus = http.StatusMethodNotAllowed
	s.override["savePlayQueue"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":0,"message":"busy"}}}`
	_ = c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0)
	s.postStatus = 0
	delete(s.override, "savePlayQueue")
	if err := c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.methodList("savePlayQueue"), " "); got != "POST GET POST" {
		t.Fatalf("requests = %q", got)
	}
}

func TestSavePlayQueueDoesNotRetryAuthErrors(t *testing.T) {
	s, c := connected(t)
	s.override["savePlayQueue"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":50,"message":"not allowed"}}}`
	if err := c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0); err == nil {
		t.Fatal("want the error")
	}
	if m, _, _ := s.request("savePlayQueue"); m != "POST" {
		t.Fatalf("an authorization error fell back to %s", m)
	}
}

func TestSavePlayQueueFallsBackOnASubsonicError(t *testing.T) {
	s, c := connected(t)
	s.override["savePlayQueue"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":0,"message":"use GET"}}}`
	_ = c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0)
	if m, _, _ := s.request("savePlayQueue"); m != "GET" {
		t.Fatalf("a generic error did not fall back; last method %s", m)
	}
}

const subsonicGenericError = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":0,"message":"use GET"}}}`

// A Subsonic error on the POST is a one-off: the next save tries POST again.
func TestSavePlayQueueDoesNotStickToGetAfterASubsonicError(t *testing.T) {
	s, c := connected(t)
	s.postOverride = map[string]string{"savePlayQueue": subsonicGenericError}
	_ = c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0)
	_ = c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0)
	if got := strings.Join(s.methodList("savePlayQueue"), " "); got != "POST GET POST GET" {
		t.Fatalf("requests = %q, want POST tried each time", got)
	}
}

// When a GET fails after the client stuck to GET, it goes back to POST.
func TestSavePlayQueueGoesBackToPostWhenTheStickyGetFails(t *testing.T) {
	s, c := connected(t)
	s.postStatus = http.StatusMethodNotAllowed
	if err := c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0); err != nil {
		t.Fatal(err)
	}
	s.override["savePlayQueue"] = subsonicGenericError
	if err := c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0); err == nil {
		t.Fatal("want the error")
	}
	s.postStatus = 0
	delete(s.override, "savePlayQueue")
	if err := c.SavePlayQueue(ctx, []ID{"so-1"}, "so-1", 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.methodList("savePlayQueue"), " "); got != "POST GET GET POST" {
		t.Fatalf("requests = %q", got)
	}
}
