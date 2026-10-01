package ui

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type fakeLibrary struct {
	mu        sync.Mutex
	albums    []subsonic.Album
	tracks    map[subsonic.ID][]subsonic.Song
	artists   []subsonic.ArtistIndex
	genres    []subsonic.Genre
	playlists []subsonic.Playlist
	plSongs   map[subsonic.ID][]subsonic.Song
	starred   subsonic.Starred
	err       error
	failAlbum map[subsonic.ID]bool // GetAlbum fails for these
	calls     []subsonic.AlbumListQuery
	searches  []string
	stars     []string      // "star al-1", "unstar s2"
	block     chan struct{} // if set, Search3 waits for it (or ctx)
	starCalls int           // GetStarred2 calls
}

func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, q)
	if l.err != nil {
		return nil, l.err
	}
	end := min(q.Offset+q.Size, len(l.albums))
	if q.Offset >= end {
		return nil, nil
	}
	return append([]subsonic.Album(nil), l.albums[q.Offset:end]...), nil
}

func (l *fakeLibrary) GetAlbum(_ context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	if l.failAlbum[id] {
		return nil, errOffline
	}
	for _, al := range l.albums {
		if al.ID == id {
			return &subsonic.AlbumWithSongs{Album: al, Songs: l.tracks[id]}, nil
		}
	}
	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no album"}
}

func (l *fakeLibrary) GetArtists(context.Context) ([]subsonic.ArtistIndex, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.artists, l.err
}

func (l *fakeLibrary) GetArtist(_ context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	for _, ix := range l.artists {
		for _, ar := range ix.Artists {
			if ar.ID == id {
				out := &subsonic.ArtistWithAlbums{Artist: ar}
				for _, al := range l.albums {
					if al.ArtistID == id {
						out.Albums = append(out.Albums, al)
					}
				}
				return out, nil
			}
		}
	}
	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no artist"}
}

func (l *fakeLibrary) GetGenres(context.Context) ([]subsonic.Genre, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.genres, l.err
}

func (l *fakeLibrary) GetPlaylists(context.Context) ([]subsonic.Playlist, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.playlists, l.err
}

func (l *fakeLibrary) GetPlaylist(_ context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	for _, pl := range l.playlists {
		if pl.ID == id {
			return &subsonic.PlaylistWithSongs{Playlist: pl, Songs: l.plSongs[id]}, nil
		}
	}
	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no playlist"}
}

func (l *fakeLibrary) GetStarred2(context.Context) (*subsonic.Starred, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.starCalls++
	if l.err != nil {
		return nil, l.err
	}
	s := l.starred
	return &s, nil
}

// Search3 matches names containing the query (case-insensitive).
func (l *fakeLibrary) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
	l.mu.Lock()
	l.searches = append(l.searches, query)
	block := l.block
	l.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(query)) }
	r := &subsonic.SearchResult{}
	for _, ix := range l.artists {
		for _, ar := range ix.Artists {
			if has(ar.Name) {
				r.Artists = append(r.Artists, ar)
			}
		}
	}
	for _, al := range l.albums {
		if has(al.Name) || has(al.Artist) {
			r.Albums = append(r.Albums, al)
		}
	}
	for _, al := range l.albums {
		for _, so := range l.tracks[al.ID] {
			if has(so.Title) {
				r.Songs = append(r.Songs, so)
			}
		}
	}
	page := func(n, off, count int) (int, int) { return min(off, n), min(off+count, n) }
	a0, a1 := page(len(r.Artists), q.ArtistOffset, q.ArtistCount)
	b0, b1 := page(len(r.Albums), q.AlbumOffset, q.AlbumCount)
	c0, c1 := page(len(r.Songs), q.SongOffset, q.SongCount)
	r.Artists, r.Albums, r.Songs = r.Artists[a0:a1], r.Albums[b0:b1], r.Songs[c0:c1]
	return r, nil
}

func (l *fakeLibrary) starCall(verb string, t subsonic.StarTarget) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	for _, ids := range [][]subsonic.ID{t.SongIDs, t.AlbumIDs, t.ArtistIDs} {
		for _, id := range ids {
			l.stars = append(l.stars, verb+" "+string(id))
		}
	}
	return nil
}

func (l *fakeLibrary) Star(_ context.Context, t subsonic.StarTarget) error {
	return l.starCall("star", t)
}
func (l *fakeLibrary) Unstar(_ context.Context, t subsonic.StarTarget) error {
	return l.starCall("unstar", t)
}

type fakePlayer struct {
	st      player.State
	events  chan player.Event
	resume  *player.Resume
	calls   []string
	played  []subsonic.Song
	start   int
	seekPos time.Duration
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{st: player.State{Index: -1, NextIndex: -1}, events: make(chan player.Event, 8)}
}

func (p *fakePlayer) State() player.State         { return p.st }
func (p *fakePlayer) Events() <-chan player.Event { return p.events }
func (p *fakePlayer) call(s string)               { p.calls = append(p.calls, s) }
func (p *fakePlayer) TogglePause()                { p.call("toggle") }
func (p *fakePlayer) Next()                       { p.call("next") }
func (p *fakePlayer) Prev()                       { p.call("prev") }
func (p *fakePlayer) Seek(d time.Duration)        { p.call("seek"); p.seekPos, p.st.Position = d, d }
func (p *fakePlayer) Jump(i int)                  { p.call("jump"); p.st.Index = i }
func (p *fakePlayer) Remove(i int)                { p.call("remove") }
func (p *fakePlayer) SetShuffle(on bool)          { p.st.Shuffle = on }
func (p *fakePlayer) SetRepeat(r player.Repeat)   { p.st.Repeat = r }
func (p *fakePlayer) ResumeFrom(r *player.Resume) {
	p.call("resume")
	p.st.Queue, p.st.Index = r.Songs, r.Index
}
func (p *fakePlayer) Resumable(context.Context) (*player.Resume, error) {
	return p.resume, nil
}
func (p *fakePlayer) PlayNext(songs []subsonic.Song) {
	p.call("playnext")
	p.played = songs
}
func (p *fakePlayer) Enqueue(songs []subsonic.Song) {
	p.call("enqueue")
	p.played = songs
}
func (p *fakePlayer) Clear() {
	p.call("clear")
	p.st.Queue, p.st.Index, p.st.Status = nil, -1, player.Stopped
}
func (p *fakePlayer) SetVolumeDB(db float64)    { p.st.VolumeDB = max(-60, min(0, db)) }
func (p *fakePlayer) SetMuted(on bool)          { p.st.Muted = on }
func (p *fakePlayer) SetReplayGain(mode string) { p.call("replaygain " + mode) }
func (p *fakePlayer) SetScrobble(on bool) {
	if on {
		p.call("scrobble on")
	} else {
		p.call("scrobble off")
	}
}
func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
	p.call("playnow")
	p.played, p.start = songs, start
	p.st.Queue, p.st.Index, p.st.Status = songs, start, player.Playing
}

// fakeArt makes a deterministic two-tone square per cover id.
type fakeArt struct {
	// cache keeps each generated cover, as the real art source keeps
	// decoded covers in memory: a repaint must not pay for making them.
	// It belongs to one test (see newFakeArt).
	cache *sync.Map // art.Key -> *gfx.Image
}

func newFakeArt() fakeArt { return fakeArt{cache: new(sync.Map)} }

func (f fakeArt) Get(k art.Key) (*gfx.Image, bool) {
	if k.ID == "" {
		return nil, false
	}
	if img, ok := f.cache.Load(k); ok {
		return img.(*gfx.Image), true
	}
	img := makeFakeArt(k)
	f.cache.Store(k, img)
	return img, true
}

func makeFakeArt(k art.Key) *gfx.Image {
	h := fnv.New32a()
	h.Write([]byte(k.ID))
	v := h.Sum32()
	img := gfx.NewImage(k.Size, k.Size)
	for y := 0; y < k.Size; y++ {
		for x := 0; x < k.Size; x++ {
			c := 0xFF000000 | v&0xFFFFFF
			if (x/(k.Size/4+1)+y/(k.Size/4+1))%2 == 0 {
				c = 0xFF000000 | ^v&0xFFFFFF
			}
			img.Pix[y*k.Size+x] = c
		}
	}
	return img
}

func sampleLibrary() *fakeLibrary {
	l := &fakeLibrary{tracks: map[subsonic.ID][]subsonic.Song{}}
	l.albums = []subsonic.Album{
		{ID: "al-1", Name: "Радио Африка", Artist: "Аквариум", ArtistID: "ar-1", CoverArt: "al-1", Year: 1983},
		{ID: "al-2", Name: "Homogenic", Artist: "Björk", ArtistID: "ar-2", CoverArt: "al-2", Year: 1997},
		{ID: "al-3", Name: "A Rather Long Album Title That Will Need Truncating Somewhere", Artist: "Some Artist", ArtistID: "ar-3", CoverArt: "al-3", Year: 2001},
	}
	l.artists = []subsonic.ArtistIndex{
		{Name: "B", Artists: []subsonic.Artist{{ID: "ar-2", Name: "Björk", CoverArt: "ar-2", AlbumCount: 1}}},
		{Name: "S", Artists: []subsonic.Artist{{ID: "ar-3", Name: "Some Artist", CoverArt: "ar-3", AlbumCount: 1}}},
		{Name: "А", Artists: []subsonic.Artist{{ID: "ar-1", Name: "Аквариум", CoverArt: "ar-1", AlbumCount: 1, Starred: "2026-01-01T00:00:00Z"}}},
	}
	l.genres = []subsonic.Genre{{Name: "rock", AlbumCount: 2}, {Name: "Electronic", AlbumCount: 1}}
	l.playlists = []subsonic.Playlist{
		{ID: "pl-1", Name: "Дорога домой", Owner: "alice", SongCount: 2, Duration: 375, CoverArt: "pl-1"},
		{ID: "pl-2", Name: "Empty", Owner: "alice", CoverArt: "pl-2"},
	}
	l.tracks["al-1"] = []subsonic.Song{
		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", ArtistID: "ar-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
		{ID: "s2", Title: "Время Луны", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 2, Duration: 160, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
		{ID: "s3", Title: "Рок-н-ролл мёртв", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 3, Duration: 215, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
	}
	l.plSongs = map[subsonic.ID][]subsonic.Song{"pl-1": {l.tracks["al-1"][2], l.tracks["al-1"][1]}}
	l.starred = subsonic.Starred{Albums: l.albums[1:2], Artists: l.artists[2].Artists, Songs: l.tracks["al-1"][:1]}
	return l
}

type testApp struct {
	*App
	disp *gfx.Headless
	lib  *fakeLibrary
	pl   *fakePlayer
	now  time.Time
}

func newTestApp(t *testing.T, prof Profile) *testApp {
	t.Helper()
	ta := &testApp{disp: gfx.NewHeadless(prof.W, prof.H, ""), lib: sampleLibrary(), pl: newFakePlayer(), now: time.Unix(1_800_000_000, 0)}
	a, err := New(Options{Display: ta.disp, Profile: prof, Library: ta.lib, Player: ta.pl, Art: newFakeArt(),
		Now: func() time.Time { return ta.now }})
	if err != nil {
		t.Fatal(err)
	}
	ta.App = a
	a.verify = true // every partial frame must equal a full one
	a.verifyFail = func(m string) { t.Errorf("%s", m) }
	return ta
}

// settle runs posted work until no loads are pending, then renders.
func (ta *testApp) settle(t *testing.T) *gfx.Canvas {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for ta.loads > 0 || len(ta.post) > 0 {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("loads did not finish")
		}
	}
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	return ta.disp.Last()
}

// redrawDue reports whether the next frame redraws anything (all of it, or
// damaged parts); clean forgets both.
func (ta *testApp) redrawDue() bool { return ta.dirty || len(ta.damage) > 0 }
func (ta *testApp) clean()          { ta.dirty, ta.damage = false, nil }

func (ta *testApp) press(b input.Button) {
	ta.onInput(input.Event{Button: b, Kind: input.Press})
	ta.onInput(input.Event{Button: b, Kind: input.Release})
}

var errOffline = errors.New("offline")
