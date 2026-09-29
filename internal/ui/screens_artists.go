package ui

import (
	"context"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// ArtistsScreen lists every artist A–Z (getArtists, fetched once per
// connection and kept); L/R jump to the previous or next letter.
type ArtistsScreen struct {
	view    artistsView
	letters []letterStart
	loaded  bool
	err     error
}

// letterStart is where one index letter's artists begin in the flat list.
type letterStart struct {
	name string
	at   int
}

func NewArtistsScreen() *ArtistsScreen { return &ArtistsScreen{} }

func (s *ArtistsScreen) Title() string {
	if l, ok := s.letter(); ok {
		return "Artists · " + l.name
	}
	return "Artists"
}

// letter is the index letter of the focused artist.
func (s *ArtistsScreen) letter() (letterStart, bool) {
	var cur letterStart
	found := false
	for _, l := range s.letters {
		if l.at <= s.view.cur.focus() {
			cur, found = l, true
		}
	}
	return cur, found
}

func (s *ArtistsScreen) Enter(a *App) {
	if a.artists != nil {
		s.set(a.artists)
		return
	}
	s.load(a)
}

func (s *ArtistsScreen) load(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtists(ctx) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		idx, _ := v.([]subsonic.ArtistIndex)
		a.artists = idx
		s.set(idx)
	})
}

func (s *ArtistsScreen) set(idx []subsonic.ArtistIndex) {
	s.loaded = true
	s.view.artists, s.letters = nil, nil
	for _, ix := range idx {
		if len(ix.Artists) == 0 {
			continue
		}
		s.letters = append(s.letters, letterStart{ix.Name, len(s.view.artists)})
		s.view.artists = append(s.view.artists, ix.Artists...)
	}
}

func (s *ArtistsScreen) Handle(a *App, e input.Event) bool {
	if (e.Button == input.BtnL || e.Button == input.BtnR) && e.Kind != input.Release && len(s.letters) > 0 {
		cur, _ := s.letter()
		target := cur.at // L: the start of this letter, or of the previous one
		for i, l := range s.letters {
			if l.name != cur.name {
				continue
			}
			switch {
			case e.Button == input.BtnR && i+1 < len(s.letters):
				target = s.letters[i+1].at
			case e.Button == input.BtnL && s.view.cur.focus() == cur.at && i > 0:
				target = s.letters[i-1].at
			}
		}
		s.view.cur.setFocus(target)
		return true
	}
	if s.view.Handle(a, e) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && s.err != nil {
		s.load(a)
		return true
	}
	return false
}

func (s *ArtistsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load artists: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.view.artists) == 0:
		a.drawCentered(c, area, "No artists", colDim)
	default:
		s.view.Draw(a, c, area)
	}
}

// ArtistScreen shows an artist's albums; Select shuffles them.
type ArtistScreen struct {
	artist subsonic.Artist
	view   albumsView
	loaded bool
	err    error
}

func NewArtistScreen(ar subsonic.Artist) *ArtistScreen { return &ArtistScreen{artist: ar} }

func (s *ArtistScreen) Title() string { return s.artist.Name }

func (s *ArtistScreen) Enter(a *App) { s.load(a) }

func (s *ArtistScreen) load(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtist(ctx, s.artist.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		ar := v.(*subsonic.ArtistWithAlbums)
		s.artist, s.view.albums, s.loaded = ar.Artist, ar.Albums, true
	})
}

func (s *ArtistScreen) Handle(a *App, e input.Event) bool {
	if s.view.Handle(a, e) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch {
	case e.Button == input.BtnA && s.err != nil:
		s.load(a)
		return true
	case e.Button == input.BtnSelect && len(s.view.albums) > 0:
		a.withSongs(albumsSongs(sampleAlbums(s.view.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
		return true
	}
	return false
}

func (s *ArtistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load artist: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.view.albums) == 0:
		a.drawCentered(c, area, "No albums", colDim)
	default:
		s.view.Draw(a, c, area)
	}
}
