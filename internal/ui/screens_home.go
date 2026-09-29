package ui

import (
	"context"
	"fmt"
	"math/rand/v2"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type homeItem struct {
	label    string
	listType string // album list type; "" for Resume
}

var homeLists = []homeItem{
	{"Recently added", subsonic.ListNewest},
	{"Recently played", subsonic.ListRecent},
	{"Most played", subsonic.ListFrequent},
	{"Random albums", subsonic.ListRandom},
}

// HomeScreen is the root: Resume (if a saved queue exists) and album lists.
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
	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
			s.resume = r
		}
	})
}

func (s *HomeScreen) items() []homeItem {
	var out []homeItem
	if s.resume != nil {
		song := s.resume.Songs[s.resume.Index]
		out = append(out, homeItem{label: fmt.Sprintf("Resume: %s — %s", song.Title, song.Artist)})
	}
	return append(out, homeLists...)
}

func (s *HomeScreen) Handle(a *App, e input.Event) bool {
	items := s.items()
	if s.list.Handle(e, len(items)) {
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA || len(items) == 0 {
		return false
	}
	it := items[s.list.Focus]
	if it.listType == "" {
		a.Player().ResumeFrom(s.resume)
		s.resume = nil
		a.Push(NewNowPlayingScreen())
		return true
	}
	a.Push(NewAlbumListScreen(it.label, it.listType))
	return true
}

func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	items := s.items()
	s.list.Draw(c, area, len(items), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		col := colText
		if items[i].listType == "" {
			col = colAccent
		}
		a.drawTextRow(c, r, "", false, items[i].label, "", col)
	})
}

// AlbumListScreen pages through getAlbumList2 results.
type AlbumListScreen struct {
	title, listType string
	list            List
	albums          []subsonic.Album
	loading, more   bool
	err             error
}

const albumPage = 100

func NewAlbumListScreen(title, listType string) *AlbumListScreen {
	return &AlbumListScreen{title: title, listType: listType, more: true}
}

func (s *AlbumListScreen) Title() string { return s.title }

func (s *AlbumListScreen) Enter(a *App) { s.loadMore(a) }

func (s *AlbumListScreen) loadMore(a *App) {
	if s.loading || !s.more {
		return
	}
	s.loading, s.err = true, nil
	offset := len(s.albums)
	a.Load(s, func(ctx context.Context) (any, error) {
		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: s.listType, Size: albumPage, Offset: offset})
	}, func(v any, err error) {
		s.loading = false
		if err != nil {
			s.err = err
			return
		}
		page, _ := v.([]subsonic.Album)
		s.albums = append(s.albums, page...)
		// The random list has no end; one page is plenty.
		s.more = len(page) == albumPage && s.listType != subsonic.ListRandom
	})
}

func (s *AlbumListScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.albums)) {
		if s.list.NearEnd(len(s.albums)) {
			s.loadMore(a)
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		if s.err != nil && len(s.albums) == 0 {
			s.more = true
			s.loadMore(a)
			return true
		}
		if len(s.albums) > 0 {
			a.Push(NewAlbumScreen(s.albums[s.list.Focus]))
		}
		return true
	}
	return false
}

func (s *AlbumListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case len(s.albums) == 0 && s.loading:
		a.drawCentered(c, area, "Loading…", colDim)
		return
	case len(s.albums) == 0 && s.err != nil:
		a.drawCentered(c, area, "Couldn't load: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case len(s.albums) == 0:
		a.drawCentered(c, area, "No albums", colDim)
		return
	}
	s.list.Draw(c, area, len(s.albums), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		al := s.albums[i]
		sub := al.Artist
		if al.Year > 0 {
			sub += fmt.Sprintf(" · %d", al.Year)
		}
		a.drawTextRow(c, r, al.CoverArt, true, al.Name, sub, colText)
	})
}

// shuffleStart picks a random first track for shuffle play.
func shuffleStart(n int) int {
	if n <= 1 {
		return 0
	}
	return rand.IntN(n)
}
