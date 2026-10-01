package ui

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type homeItem struct {
	label    string
	listType string        // album list type
	open     func() Screen // a section; nil for album lists and Resume
	exit     bool          // the Exit entry: A asks to quit
}

// exitItem is the last entry of both roots (the HDMI sidebar and the CRT
// list); A on it opens the exit prompt.
var exitItem = homeItem{label: "Exit", exit: true}

var homeLists = []homeItem{
	{label: "Recently added", listType: subsonic.ListNewest},
	{label: "Recently played", listType: subsonic.ListRecent},
	{label: "Most played", listType: subsonic.ListFrequent},
	{label: "Random albums", listType: subsonic.ListRandom},
}

// sections are the library's parts: the HDMI sidebar after Home, and the
// CRT root list after the album lists.
var sections = []homeItem{
	{label: "Artists", open: func() Screen { return NewArtistsScreen() }},
	{label: "Albums", open: func() Screen { return NewAlbumsScreen() }},
	{label: "Playlists", open: func() Screen { return NewPlaylistsScreen() }},
	{label: "Starred", open: func() Screen { return NewStarredScreen() }},
	{label: "Search", open: func() Screen { return NewSearchScreen() }},
	{label: "Settings", open: func() Screen { return NewSettingsScreen() }},
}

// NewRootScreen is the first screen after connecting: the sidebar and home
// feed on HDMI, the sections list on a CRT.
func NewRootScreen(p Profile) Screen {
	if p.SideW > 0 {
		return newSidebarRoot()
	}
	return NewHomeScreen()
}

// HomeScreen is the CRT root (and the fallback): Resume (if a saved queue
// exists), the album lists and the sections.
type HomeScreen struct {
	list   List
	resume *player.Resume
}

func NewHomeScreen() *HomeScreen { return &HomeScreen{} }

func (s *HomeScreen) Title() string { return "MiSTer Subsonic" }

func (s *HomeScreen) Enter(a *App) {
	if a.Player() == nil {
		return
	}
	pl := a.Player() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) { return pl.Resumable(ctx) }, func(v any, err error) {
		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
			s.resume = r
			if s.list.Focus > 0 {
				s.list.Focus++ // keep the focus on the same item
			}
		}
	})
}

// Sync runs before each frame (Draw leaves the screen as it is).
func (s *HomeScreen) Sync(a *App) { s.sync(a) }

// sync drops the Resume row once something is playing: resuming then
// would replace the live queue.
func (s *HomeScreen) sync(a *App) {
	if s.resume != nil && a.hasQueue() {
		s.resume = nil
		if s.list.Focus > 0 {
			s.list.Focus-- // keep the focus on the same item
		}
	}
}

func (s *HomeScreen) items() []homeItem {
	var out []homeItem
	if s.resume != nil {
		song := s.resume.Songs[s.resume.Index]
		out = append(out, homeItem{label: fmt.Sprintf("Resume: %s — %s", song.Title, song.Artist)})
	}
	return append(append(append(out, homeLists...), sections...), exitItem)
}

func (s *HomeScreen) Handle(a *App, e input.Event) bool {
	s.sync(a)
	items := s.items()
	if s.list.Handle(e, len(items)) {
		a.moved(s.list.Moved())
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA || len(items) == 0 {
		return false
	}
	it := items[s.list.Focus]
	switch {
	case it.exit:
		a.confirm = true // the same prompt as holding B
	case it.open != nil:
		a.Push(it.open())
	case it.listType == "":
		a.Player().ResumeFrom(s.resume)
		s.resume = nil
		a.Push(NewNowPlayingScreen())
	default:
		a.Push(NewAlbumListScreen(it.label, it.listType))
	}
	return true
}

func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	items := s.items()
	s.list.Draw(c, area, len(items), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		col := colText
		if items[i].listType == "" && items[i].open == nil && !items[i].exit {
			col = colAccent
		}
		a.drawRow(c, r, row{main: items[i].label, col: col, focused: focused})
	})
}

// AlbumListScreen pages through getAlbumList2 results.
type AlbumListScreen struct {
	title         string
	q             subsonic.AlbumListQuery
	view          albumsView
	loading, more bool
	err           error
}

const albumPage = 100

func NewAlbumListScreen(title, listType string) *AlbumListScreen {
	return NewAlbumQueryScreen(title, subsonic.AlbumListQuery{Type: listType})
}

// NewAlbumQueryScreen lists albums for any getAlbumList2 query (by year, by genre).
func NewAlbumQueryScreen(title string, q subsonic.AlbumListQuery) *AlbumListScreen {
	return &AlbumListScreen{title: title, q: q, more: true}
}

func (s *AlbumListScreen) Title() string { return s.title }

func (s *AlbumListScreen) Enter(a *App) { s.loadMore(a) }

func (s *AlbumListScreen) loadMore(a *App) {
	if s.loading || !s.more {
		return
	}
	s.loading, s.err = true, nil
	q := s.q
	q.Size, q.Offset = albumPage, len(s.view.albums)
	lib := a.Library() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) {
		return lib.GetAlbumList2(ctx, q)
	}, func(v any, err error) {
		s.loading = false
		if err != nil {
			s.err = err
			if len(s.view.albums) > 0 {
				a.Toast("Couldn't load more: %s", subsonic.Classify(err))
			}
			return
		}
		page, _ := v.([]subsonic.Album)
		s.view.albums = append(s.view.albums, page...)
		// The random list has no end; one page is plenty.
		s.more = len(page) == albumPage && s.q.Type != subsonic.ListRandom
	})
}

func (s *AlbumListScreen) Handle(a *App, e input.Event) bool {
	if s.view.Handle(a, e) {
		// After a failure, only a Press retries, not the repeats of a held key.
		if s.view.cur.nearEnd(a, len(s.view.albums)) && (s.err == nil || e.Kind == input.Press) {
			s.loadMore(a)
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	// Down at the very end does not move, but retries a page that failed.
	switch e.Button {
	case input.BtnDown, input.BtnRight, input.BtnR:
		if len(s.view.albums) > 0 && s.view.cur.nearEnd(a, len(s.view.albums)) {
			s.loadMore(a)
		}
	}
	switch e.Button {
	case input.BtnA:
		if s.err != nil && len(s.view.albums) == 0 {
			s.more = true
			s.loadMore(a)
			return true
		}
	case input.BtnSelect:
		if len(s.view.albums) > 0 {
			a.withSongs(albumsSongs(sampleAlbums(s.view.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
			return true
		}
	}
	return false
}

func (s *AlbumListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case len(s.view.albums) == 0 && s.loading:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.view.albums) == 0 && s.err != nil:
		a.drawCentered(c, area, "Couldn't load: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case len(s.view.albums) == 0:
		a.drawCentered(c, area, "No albums", colDim)
	default:
		s.view.Draw(a, c, area)
	}
}

// GenresScreen lists genres by name; A opens a genre's albums.
type GenresScreen struct {
	genres []subsonic.Genre
	list   List
	loaded bool
	err    error
}

func NewGenresScreen() *GenresScreen { return &GenresScreen{} }

func (s *GenresScreen) Title() string { return "Genres" }

func (s *GenresScreen) Enter(a *App) {
	s.err = nil
	lib := a.Library() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetGenres(ctx) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		g, _ := v.([]subsonic.Genre)
		slices.SortFunc(g, func(x, y subsonic.Genre) int {
			return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		})
		s.genres, s.loaded = g, true
	})
}

func (s *GenresScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.genres)) {
		a.moved(s.list.Moved())
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA {
		return false
	}
	switch {
	case s.err != nil:
		s.Enter(a)
	case len(s.genres) > 0:
		g := s.genres[s.list.Focus]
		a.Push(NewAlbumQueryScreen(g.Name, subsonic.AlbumListQuery{Type: subsonic.ListByGenre, Genre: g.Name}))
	}
	return true
}

func (s *GenresScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load genres: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.genres) == 0:
		a.drawCentered(c, area, "No genres", colDim)
	default:
		s.list.Draw(c, area, len(s.genres), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
			g := s.genres[i]
			a.drawRow(c, r, row{main: g.Name, focused: focused, right: albumCount(g.AlbumCount)})
		})
	}
}

// shuffleStart picks a random first track for shuffle play.
func shuffleStart(n int) int {
	if n <= 1 {
		return 0
	}
	return rand.IntN(n)
}
