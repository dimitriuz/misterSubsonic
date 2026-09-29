package ui

import (
	"context"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// NewStarredScreen is the Starred section: albums, artists and tracks tabs
// over one getStarred2 call.
func NewStarredScreen() *TabbedScreen {
	d := &starredData{}
	return newTabbed("Starred", []string{"Albums", "Artists", "Tracks"},
		&starredTab{d: d, kind: starAlbum}, &starredTab{d: d, kind: starArtist}, &starredTab{d: d, kind: starSong})
}

// starredData is the getStarred2 result the three tabs share.
type starredData struct {
	res     *subsonic.Starred
	gen     int // App.starGen when res was requested
	loading bool
	err     error
}

// load fetches the list unless it is loading or current: a star change since
// the last request makes it stale. The old list stays up until the new one
// arrives.
func (d *starredData) load(a *App, s Screen) {
	if d.loading || (d.res != nil && d.err == nil && d.gen == a.starGen) {
		return
	}
	d.loading, d.err, d.gen = true, nil, a.starGen
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetStarred2(ctx) }, func(v any, err error) {
		d.loading = false
		if err != nil {
			d.err = err
			return
		}
		d.res = v.(*subsonic.Starred)
	})
}

// starredTab is one tab of Starred; kind picks albums, artists or tracks.
type starredTab struct {
	d       *starredData
	kind    starKind
	albums  albumsView
	artists artistsView
	songs   songsView
}

func (s *starredTab) Title() string { return "Starred" }
func (s *starredTab) Enter(a *App)  { s.d.load(a, s) }

// Shown reloads the list if a star changed while another screen was up.
func (s *starredTab) Shown(a *App) { s.d.load(a, s) }

// sync copies the shared result into this tab's view.
func (s *starredTab) sync() int {
	if s.d.res == nil {
		return 0
	}
	switch s.kind {
	case starAlbum:
		s.albums.albums = s.d.res.Albums
		return len(s.albums.albums)
	case starArtist:
		s.artists.artists = s.d.res.Artists
		return len(s.artists.artists)
	}
	s.songs.songs = s.d.res.Songs
	return len(s.songs.songs)
}

func (s *starredTab) Handle(a *App, e input.Event) bool {
	s.sync()
	if e.Kind == input.Press && e.Button == input.BtnA && s.d.err != nil {
		s.d.load(a, s)
		return true
	}
	switch s.kind {
	case starAlbum:
		return s.albums.Handle(a, e)
	case starArtist:
		return s.artists.Handle(a, e)
	}
	return s.songs.Handle(a, e)
}

func (s *starredTab) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	n := s.sync()
	switch {
	case s.d.err != nil:
		a.drawCentered(c, area, "Couldn't load starred items: "+subsonic.Classify(s.d.err).String()+" — A to retry", colError)
	case s.d.res == nil:
		a.drawCentered(c, area, "Loading…", colDim)
	case n == 0:
		a.drawCentered(c, area, "Nothing starred here yet — X on an item stars it", colDim)
	case s.kind == starAlbum:
		s.albums.Draw(a, c, area)
	case s.kind == starArtist:
		s.artists.Draw(a, c, area)
	default:
		s.songs.Draw(a, c, area)
	}
}
