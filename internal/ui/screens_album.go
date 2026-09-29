package ui

import (
	"context"
	"fmt"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// AlbumScreen shows an album header and its tracks, with Play, Shuffle and
// Star rows. X opens the menu of the focused track, or of the album.
type AlbumScreen struct {
	stub  subsonic.Album // from the list, shown while loading
	album *subsonic.AlbumWithSongs
	list  List
	err   error
}

const albumActionRows = 3 // Play, Shuffle, Star

func NewAlbumScreen(a subsonic.Album) *AlbumScreen { return &AlbumScreen{stub: a} }

// Title is the artist: the album name is already the header block's headline.
func (s *AlbumScreen) Title() string { return s.stub.Artist }

func (s *AlbumScreen) Enter(a *App) { s.load(a) }

func (s *AlbumScreen) load(a *App) {
	s.err = nil
	lib := a.Library() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetAlbum(ctx, s.stub.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.album = v.(*subsonic.AlbumWithSongs)
	})
}

// info is the loaded album, or the stub from the list until then.
func (s *AlbumScreen) info() subsonic.Album {
	if s.album != nil {
		return s.album.Album
	}
	return s.stub
}

func (s *AlbumScreen) songs() []subsonic.Song {
	if s.album == nil {
		return nil
	}
	return s.album.Songs
}

func (s *AlbumScreen) play(a *App, start int, shuffle bool) {
	a.playSongs(s.songs(), start, shuffle)
}

func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
	n := albumActionRows + len(s.songs())
	if s.album == nil {
		n = 0
	}
	if s.list.Handle(e, n) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		switch {
		case s.album == nil && s.err != nil:
			s.load(a)
		case s.album == nil:
		case s.list.Focus == 0:
			s.play(a, 0, false)
		case s.list.Focus == 1:
			s.play(a, 0, true)
		case s.list.Focus == 2:
			a.toggleStar(albumStar(s.info()))
		default:
			s.play(a, s.list.Focus-albumActionRows, false)
		}
		return true
	case input.BtnX:
		if i := s.list.Focus - albumActionRows; s.album != nil && i >= 0 && i < len(s.songs()) {
			so := s.songs()[i]
			a.openMenu(so.Title, songMenu(a, so))
		} else {
			a.openMenu(s.info().Name, albumMenu(a, s.info()))
		}
		return true
	case input.BtnSelect:
		s.play(a, 0, true)
		return true
	}
	return false
}

// summary is e.g. "1983 · 11 tracks · 42:00 · FLAC 24/96".
func (s *AlbumScreen) summary() string {
	var parts []string
	if s.stub.Year > 0 {
		parts = append(parts, fmt.Sprint(s.stub.Year))
	}
	songs := s.songs()
	if len(songs) > 0 {
		total := 0
		for _, so := range songs {
			total += so.Duration
		}
		parts = append(parts, fmt.Sprintf("%d tracks", len(songs)), clock(secs(total)))
		parts = append(parts, quality(songs[0], false))
	}
	return strings.Join(parts, " · ")
}

func (s *AlbumScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	art := p.ArtAlbum
	header := gfx.R(area.X, area.Y, area.W, art+p.Margin)
	a.drawArt(c, s.stub.CoverArt, gfx.R(area.X+p.Margin, area.Y+p.Margin/2, art, art))
	x := area.X + p.Margin + art + p.Margin
	w := area.Right() - p.Margin - x
	y := area.Y + p.Margin/2
	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
	ft.Draw(c, x, y+ft.Ascent(), ft.Truncate(s.stub.Name, w), colText, header)
	y += ft.Height()
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(s.stub.Artist, w), colDim, header)
	y += fb.Height()
	fs.Draw(c, x, y+fs.Ascent(), fs.Truncate(s.summary(), w), colDim, header)

	body := gfx.R(area.X, header.Bottom(), area.W, area.H-header.H)
	switch {
	case s.album == nil && s.err != nil:
		a.drawCentered(c, body, "Couldn't load album: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case s.album == nil:
		a.drawCentered(c, body, "Loading…", colDim)
		return
	}
	songs := s.songs()
	s.list.Draw(c, body, albumActionRows+len(songs), p.RowH, func(i int, r gfx.Rect, focused bool) {
		switch i {
		case 0:
			f := a.F.Body
			iconText(c, f, iconPlay, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, "Play", colAccent, r)
		case 1:
			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
		case 2:
			label := "Star"
			if a.isStarred(albumStar(s.info())) {
				label = "Unstar"
			}
			a.drawTextRow(c, r, "", false, label, "", colAccent)
		default:
			so := songs[i-albumActionRows]
			a.drawTrackRow(c, r, so, false, focused)
		}
	})
}

// drawTrackRow: "03  Title ...  ★ 3:45".
func (a *App) drawTrackRow(c *gfx.Canvas, r gfx.Rect, so subsonic.Song, current, focused bool) {
	p := a.P
	f := a.F.Body
	col := colText
	if current {
		col = colAccent
	}
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	num := fmt.Sprintf("%02d", so.Track)
	if so.Track == 0 {
		num = "  "
	}
	x := r.X + p.Margin
	f.Draw(c, x, y, num, colDim, r)
	x += f.Measure("00") + p.Margin/2
	dur := clock(secs(so.Duration))
	dw := f.Measure(dur)
	f.Draw(c, r.Right()-p.Margin-dw, y, dur, colDim, r)
	right := r.Right() - p.Margin - dw - p.Margin/2
	if a.isStarred(songStar(so)) {
		s := f.Ascent() * 3 / 4
		drawIcon(c, iconStar, gfx.R(right-s, y-f.Ascent()/2-s/2, s, s), colAccent)
		right -= s + p.Margin/2
	}
	a.drawFit(c, f, x, y, right-x, so.Title, col, r, focused)
}
