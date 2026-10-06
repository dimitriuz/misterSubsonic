package ui

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/subsonic"
)

// The web remote's side of the app: Controller is the adapter the remote
// server drives. Commands are posted to the UI goroutine and run the same
// code as the buttons; reads (state, library, covers) never touch UI state.

// RemoteNotifier is told when the player state or the queue changed
// (*remote.Server through cmd's host; nil: no remote).
type RemoteNotifier interface {
	Notify(remote.Change)
}

// RemoteSwitch starts and stops the remote server (Settings → Remote).
// cmd implements it, so the UI doesn't need the server.
type RemoteSwitch interface {
	SetEnabled(on bool) error
	URLs() []string
	// Running is whether the server is listening now (a failed start at
	// launch leaves the config on and this false).
	Running() bool
}

// remoteTimeout is how long a command waits for the UI goroutine.
var remoteTimeout = 5 * time.Second

// remoteLibTimeout bounds the library calls that resolve a play request.
const remoteLibTimeout = 10 * time.Second

// maxSeenSongs bounds the songs remembered for play requests by id.
const maxSeenSongs = 4096

var (
	errNoConnection = remote.ErrNoConnection
	errNoCover      = errors.New("remote: no cover")
)

// liveRefs are the connection's parts, published for goroutines other than
// the UI's (the UI changes them in Attach and Detach).
type liveRefs struct {
	lib Library
	pl  Player
	art ArtSource
}

// publishLive makes the current library, player and art visible to the
// remote. Call on the UI goroutine after changing them.
func (a *App) publishLive() {
	a.live.Store(&liveRefs{lib: a.o.Library, pl: a.o.Player, art: a.o.Art})
}

// publishStars copies this session's star changes on songs for the remote.
// Call on the UI goroutine after a.stars changed.
func (a *App) publishStars() {
	m := map[subsonic.ID]bool{}
	for k, on := range a.stars {
		if k.kind == starSong {
			m[k.id] = on
		}
	}
	a.songStars.Store(&m)
}

// notifyRemote tells the remote server, if there is one.
func (a *App) notifyRemote(c remote.Change) {
	if a.o.Remote != nil {
		a.o.Remote.Notify(c)
	}
}

// RemoteController is the adapter the remote server uses to read and drive
// the app. Its methods are safe from any goroutine.
func (a *App) RemoteController() remote.Controller { return remoteCtl{a} }

type remoteCtl struct{ a *App }

func (c remoteCtl) State() remote.State {
	st := remote.State{Status: "stopped", Repeat: "off", Index: -1}
	r := c.a.live.Load()
	if r == nil || r.pl == nil {
		return st
	}
	ps := r.pl.State()
	st.Status = ps.Status.String()
	st.PositionMS = ps.Position.Milliseconds()
	st.VolumeDB, st.Muted, st.Shuffle, st.Index = ps.VolumeDB, ps.Muted, ps.Shuffle, ps.Index
	st.Repeat = [...]string{"off", "all", "one"}[ps.Repeat]
	if song, ok := ps.Current(); ok {
		rs := c.a.remoteSong(song)
		st.Song, st.DurationMS = &rs, rs.DurationMS
	}
	return st
}

func (c remoteCtl) Queue() remote.QueueView {
	q := remote.QueueView{Index: -1, Songs: []remote.Song{}}
	r := c.a.live.Load()
	if r == nil || r.pl == nil {
		return q
	}
	ps := r.pl.State()
	q.Index = ps.Index
	for _, s := range ps.Queue {
		q.Songs = append(q.Songs, c.a.remoteSong(s))
	}
	return q
}

// remoteSong is s as the page sees it; the star is this session's change if
// there is one, else the server's.
func (a *App) remoteSong(s subsonic.Song) remote.Song {
	starred := s.IsStarred()
	if m := a.songStars.Load(); m != nil {
		if on, ok := (*m)[s.ID]; ok {
			starred = on
		}
	}
	return remote.Song{ID: string(s.ID), Title: s.Title, Artist: s.Artist, Album: s.Album,
		CoverID: string(s.CoverArt), DurationMS: int64(s.Duration) * 1000, Starred: starred}
}

// Library is the connection's library; every song its albums, playlists,
// starred and searches return is remembered, so a later play request can name
// songs by id. nil without a connection.
func (c remoteCtl) Library() remote.Library {
	r := c.a.live.Load()
	if r == nil || r.lib == nil {
		return nil
	}
	return seenLibrary{r.lib, c.a.seen}
}

// Cover returns the encoded cover as the art loader cached or fetched it.
func (c remoteCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	r := c.a.live.Load()
	if r == nil {
		return nil, "", errNoCover
	}
	src, ok := r.art.(interface {
		Bytes(context.Context, art.Key) ([]byte, error)
	})
	if !ok {
		return nil, "", errNoCover
	}
	data, err := src.Bytes(ctx, art.Key{ID: subsonic.ID(id), Size: size})
	if err != nil {
		return nil, "", err
	}
	return data, sniffImage(data), nil
}

// sniffImage names the type of encoded image data.
func sniffImage(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF8")):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return "application/octet-stream"
}

// onUI runs f on the UI goroutine and waits for it. If the UI doesn't get to
// it within remoteTimeout the caller gets ErrBusy and f never runs.
func (a *App) onUI(f func() error) error {
	const (
		waiting = iota
		running
		abandoned
	)
	var state atomic.Int32
	res := make(chan error, 1)
	a.Post(func() {
		if state.CompareAndSwap(waiting, running) {
			res <- f()
		}
	})
	t := time.NewTimer(remoteTimeout)
	defer t.Stop()
	select {
	case err := <-res:
		return err
	case <-t.C:
		if state.CompareAndSwap(waiting, abandoned) {
			return remote.ErrBusy
		}
		return <-res // it started just now: it is quick
	}
}

func (c remoteCtl) Do(cmd remote.Command) error {
	return c.a.onUI(func() error { return c.a.remoteDo(cmd) })
}

// remoteDo runs a command on the UI goroutine, as the matching button does.
func (a *App) remoteDo(c remote.Command) error {
	pl := a.Player()
	queue := func(i int, id string) error { // the entry the page saw is still at i
		if pl == nil {
			return remote.ErrStale
		}
		q := pl.State().Queue
		if i < 0 || i >= len(q) || (id != "" && string(q[i].ID) != id) {
			return remote.ErrStale
		}
		return nil
	}
	panel := false // only the volume panel changes: it damages its own area
	switch c.Do {
	case "volume":
		a.setVolume(c.DB)
		panel = true
	case "mute":
		a.setMuted(c.On)
		panel = true
	case "toggle":
		if pl != nil {
			pl.TogglePause()
		}
	case "next", "prev":
		if pl != nil && a.hasQueue() {
			if c.Do == "next" {
				pl.Next()
			} else {
				pl.Prev()
			}
		}
	case "seek":
		if pl != nil && a.hasCurrent() {
			pos := time.Duration(c.PositionMS) * time.Millisecond
			if song, ok := pl.State().Current(); ok && song.Duration > 0 {
				pos = min(pos, time.Duration(song.Duration)*time.Second-time.Second) // as the media keys
			}
			pl.Seek(max(pos, 0))
		}
	case "shuffle":
		if pl != nil {
			pl.SetShuffle(c.On)
		}
	case "repeat":
		if pl != nil {
			pl.SetRepeat(map[string]player.Repeat{"off": player.RepeatOff, "all": player.RepeatAll, "one": player.RepeatOne}[c.Mode])
		}
	case "star":
		if pl != nil {
			if song, ok := pl.State().Current(); ok {
				if it := songStar(song); a.isStarred(it) != c.On {
					a.toggleStar(it)
				}
			}
		}
	case "jump":
		if err := queue(c.Index, c.SongID); err != nil {
			return err
		}
		pl.Jump(c.Index)
	case "remove":
		if err := queue(c.Index, c.SongID); err != nil {
			return err
		}
		pl.Remove(c.Index)
	case "move":
		if err := queue(c.From, c.SongID); err != nil {
			return err
		}
		if err := queue(c.To, ""); err != nil {
			return err
		}
		pl.Move(c.From, c.To)
	case "clear":
		if pl != nil {
			pl.Clear()
		}
	case "screenshot": // as the button; nothing on screen or in the state changes
		return a.screenshot()
	default:
		return errors.New("remote: unknown command " + c.Do)
	}
	if !panel {
		a.dirty = true
	}
	a.notifyRemote(remote.StateChanged) // volume, repeat and the like send no player event
	return nil
}

func (c remoteCtl) Play(p remote.PlayRequest) (int, error) {
	r := c.a.live.Load()
	if r == nil || r.lib == nil || r.pl == nil {
		return 0, errNoConnection
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteLibTimeout)
	defer cancel()
	songs, err := c.a.resolve(ctx, r, p)
	if err != nil || len(songs) == 0 {
		return 0, err
	}
	err = c.a.onUI(func() error {
		pl := c.a.Player() // the connection may have changed meanwhile
		if pl == nil || c.a.live.Load() != r || pl != r.pl {
			return errNoConnection
		}
		switch p.How {
		case "next":
			pl.PlayNext(songs)
		case "end":
			pl.Enqueue(songs)
		default:
			start := p.Start
			if start < 0 || start >= len(songs) {
				start = 0
			}
			pl.PlayNow(songs, start)
		}
		c.a.dirty = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(songs), nil
}

// resolve turns a play request into songs, off the UI goroutine.
func (a *App) resolve(ctx context.Context, r *liveRefs, p remote.PlayRequest) ([]subsonic.Song, error) {
	switch p.What {
	case "album":
		al, err := r.lib.GetAlbum(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		a.seen.add(al.Songs)
		return al.Songs, nil
	case "playlist":
		pl, err := r.lib.GetPlaylist(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		a.seen.add(pl.Songs)
		return pl.Songs, nil
	case "artist":
		ar, err := r.lib.GetArtist(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		songs, err := artistSongsOf(r.lib, ar.Albums)
		if err != nil {
			return nil, err
		}
		a.seen.add(songs)
		return songs, nil
	case "songs":
		var queued []subsonic.Song
		if r.pl != nil {
			queued = r.pl.State().Queue
		}
		out := make([]subsonic.Song, 0, len(p.IDs))
		for _, id := range p.IDs {
			s, ok := a.seen.get(subsonic.ID(id))
			if !ok {
				for _, q := range queued {
					if string(q.ID) == id {
						s, ok = q, true
						break
					}
				}
			}
			if !ok { // not browsed since the app started (the page may be older): ask the server
				so, err := r.lib.GetSong(ctx, subsonic.ID(id))
				if err != nil {
					return nil, err
				}
				a.seen.add([]subsonic.Song{*so})
				s = *so
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("remote: unknown play request " + p.What)
}

// artistFetchers is how many of an artist's albums are fetched at once.
const artistFetchers = 4

// artistSongsOf is the songs of albums in order, at most remote.MaxPlay (the
// cap the server puts on a play request). The albums are fetched a few at a
// time, each under its own remoteLibTimeout, so a big artist on a slow server
// is not cut off by one budget for all of them. Albums past the cap are not
// fetched.
func artistSongsOf(lib Library, albums []subsonic.Album) ([]subsonic.Song, error) {
	type result struct {
		songs []subsonic.Song
		err   error
		done  bool
	}
	res := make([]result, len(albums))
	var (
		mu   sync.Mutex
		next int
		have int // songs in the leading run of finished albums
		stop bool
		wg   sync.WaitGroup
	)
	for range min(artistFetchers, len(albums)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if stop || next >= len(albums) {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), remoteLibTimeout)
				full, err := lib.GetAlbum(ctx, albums[i].ID)
				cancel()
				mu.Lock()
				res[i].done, res[i].err = true, err
				if err == nil {
					res[i].songs = full.Songs
				}
				// Once the albums from the start already hold enough, or one failed, stop.
				have = 0
				for j := range res {
					if !res[j].done {
						break
					}
					if res[j].err != nil {
						stop = true
						break
					}
					if have += len(res[j].songs); have >= remote.MaxPlay {
						stop = true
						break
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var songs []subsonic.Song
	for _, r := range res {
		if !r.done {
			break
		}
		if r.err != nil {
			return nil, r.err
		}
		songs = append(songs, r.songs...)
		if len(songs) >= remote.MaxPlay {
			return songs[:remote.MaxPlay], nil
		}
	}
	return songs, nil
}

// songMemory keeps the songs the remote's library calls returned, so a play
// request can name songs by id (the page only has what it browsed). Bounded:
// the oldest go first.
type songMemory struct {
	mu    sync.Mutex
	songs map[subsonic.ID]subsonic.Song
	order []subsonic.ID
}

func (m *songMemory) add(songs []subsonic.Song) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.songs == nil {
		m.songs = map[subsonic.ID]subsonic.Song{}
	}
	for _, s := range songs {
		if _, ok := m.songs[s.ID]; !ok {
			m.order = append(m.order, s.ID)
		}
		m.songs[s.ID] = s
	}
	if over := len(m.order) - maxSeenSongs; over > 0 {
		for _, id := range m.order[:over] {
			delete(m.songs, id)
		}
		m.order = append(m.order[:0], m.order[over:]...)
	}
}

func (m *songMemory) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.songs, m.order = nil, nil
}

func (m *songMemory) get(id subsonic.ID) (subsonic.Song, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.songs[id]
	return s, ok
}

// seenLibrary is the library the remote browses; it remembers the songs.
type seenLibrary struct {
	Library
	seen *songMemory
}

func (l seenLibrary) GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	al, err := l.Library.GetAlbum(ctx, id)
	if err == nil {
		l.seen.add(al.Songs)
	}
	return al, err
}

func (l seenLibrary) GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
	pl, err := l.Library.GetPlaylist(ctx, id)
	if err == nil {
		l.seen.add(pl.Songs)
	}
	return pl, err
}

func (l seenLibrary) GetStarred2(ctx context.Context) (*subsonic.Starred, error) {
	st, err := l.Library.GetStarred2(ctx)
	if err == nil {
		l.seen.add(st.Songs)
	}
	return st, err
}

func (l seenLibrary) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
	res, err := l.Library.Search3(ctx, query, q)
	if err == nil {
		l.seen.add(res.Songs)
	}
	return res, err
}
