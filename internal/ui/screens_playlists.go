package ui

import (
	"context"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// PlaylistsScreen lists the user's playlists (covers on HDMI).
type PlaylistsScreen struct {
	playlists []subsonic.Playlist
	cur       cursor
	loaded    bool
	err       error
}

func NewPlaylistsScreen() *PlaylistsScreen { return &PlaylistsScreen{} }

func (s *PlaylistsScreen) Title() string { return "Playlists" }

func (s *PlaylistsScreen) Enter(a *App) {
	s.err = nil
	lib := a.Library() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetPlaylists(ctx) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.playlists, _ = v.([]subsonic.Playlist)
		s.loaded = true
	})
}

func (s *PlaylistsScreen) Handle(a *App, e input.Event) bool {
	if s.cur.handle(a, e, len(s.playlists)) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	if e.Button == input.BtnA && s.err != nil {
		s.Enter(a)
		return true
	}
	if len(s.playlists) == 0 {
		return false
	}
	pl := s.playlists[min(s.cur.focus(), len(s.playlists)-1)]
	switch e.Button {
	case input.BtnA:
		a.Push(NewPlaylistScreen(pl))
		return true
	case input.BtnX:
		a.openMenu(pl.Name, queueEntries(pl.Name, playlistSongs(pl.ID)))
		return true
	}
	return false
}

func (s *PlaylistsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load playlists: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
		return
	case len(s.playlists) == 0:
		a.drawCentered(c, area, "No playlists", colDim)
		return
	}
	s.cur.draw(a, c, area, len(s.playlists), func(i int, r gfx.Rect, focused bool) {
		pl := s.playlists[i]
		a.drawCoverCell(c, r, pl.CoverArt, pl.Name, trackCount(pl.SongCount), focused, false)
	}, func(i int, r gfx.Rect, focused bool) {
		pl := s.playlists[i]
		a.drawRow(c, r, row{cover: pl.CoverArt, thumb: true, main: pl.Name, sub: playlistSummary(pl), focused: focused})
	})
}

func trackCount(n int) string { return plural(n, "track") }

// playlistSummary is "12 tracks · 48:10 · by alice".
func playlistSummary(pl subsonic.Playlist) string {
	parts := []string{trackCount(pl.SongCount)}
	if pl.Duration > 0 {
		parts = append(parts, clock(secs(pl.Duration)))
	}
	if pl.Owner != "" {
		parts = append(parts, "by "+pl.Owner)
	}
	return strings.Join(parts, " · ")
}

// PlaylistScreen shows a playlist's tracks under Play and Shuffle rows.
type PlaylistScreen struct {
	stub subsonic.Playlist
	pl   *subsonic.PlaylistWithSongs
	list List
	err  error
}

const playlistActionRows = 2 // Play, Shuffle

func NewPlaylistScreen(pl subsonic.Playlist) *PlaylistScreen { return &PlaylistScreen{stub: pl} }

func (s *PlaylistScreen) Title() string { return "Playlist" }

func (s *PlaylistScreen) Enter(a *App) {
	s.err = nil
	lib := a.Library() // read on the UI goroutine; the load runs off it
	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetPlaylist(ctx, s.stub.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.pl = v.(*subsonic.PlaylistWithSongs)
		s.stub = s.pl.Playlist
	})
}

func (s *PlaylistScreen) songs() []subsonic.Song {
	if s.pl == nil {
		return nil
	}
	return s.pl.Songs
}

func (s *PlaylistScreen) Handle(a *App, e input.Event) bool {
	n := 0
	if s.pl != nil {
		n = playlistActionRows + len(s.songs())
	}
	if s.list.Handle(e, n) {
		a.moved(s.list.Moved())
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		switch {
		case s.pl == nil && s.err != nil:
			s.Enter(a)
		case s.pl == nil:
		case s.list.Focus < playlistActionRows:
			a.playSongs(s.songs(), 0, s.list.Focus == 1)
		default:
			a.playSongs(s.songs(), s.list.Focus-playlistActionRows, false)
		}
		return true
	case input.BtnX:
		if i := s.list.Focus - playlistActionRows; i >= 0 && i < len(s.songs()) {
			so := s.songs()[i]
			a.openMenu(so.Title, songMenu(a, so))
		} else {
			a.openMenu(s.stub.Name, queueEntries(s.stub.Name, playlistSongs(s.stub.ID)))
		}
		return true
	case input.BtnSelect:
		a.playSongs(s.songs(), 0, true)
		return true
	}
	return false
}

func (s *PlaylistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	art := p.ArtAlbum
	header := gfx.R(area.X, area.Y, area.W, art+p.Margin)
	a.drawArt(c, s.stub.CoverArt, gfx.R(area.X+p.Margin, area.Y+p.Margin/2, art, art))
	x := area.X + p.Margin + art + p.Margin
	w := area.Right() - p.Margin - x
	y := area.Y + p.Margin/2
	ft, fb := a.F.Title, a.F.Body
	ft.Draw(c, x, y+ft.Ascent(), ft.Truncate(s.stub.Name, w), colText, header)
	y += ft.Height()
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(playlistSummary(s.stub), w), colDim, header)

	body := gfx.R(area.X, header.Bottom(), area.W, area.H-header.H)
	switch {
	case s.pl == nil && s.err != nil:
		a.drawCentered(c, body, "Couldn't load playlist: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case s.pl == nil:
		a.drawCentered(c, body, "Loading…", colDim)
		return
	}
	songs := s.songs()
	// Two-line rows (title over artist) where they fit; a CRT has no room for
	// them under Play and Shuffle, so its tracks are one line, "Title — Artist".
	rowH, oneLine := p.Row2H, p.SideW == 0
	if oneLine {
		rowH = p.RowH
	}
	s.list.Draw(c, body, playlistActionRows+len(songs), rowH, func(i int, r gfx.Rect, focused bool) {
		switch i {
		case 0:
			f := a.F.Body
			iconText(c, f, iconPlay, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, "Play", colAccent, r)
		case 1:
			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
		default:
			so := songs[i-playlistActionRows]
			w := row{cover: so.CoverArt, thumb: true, main: so.Title, sub: so.Artist, focused: focused,
				starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))}
			if oneLine {
				w.thumb, w.sub = false, ""
				if so.Artist != "" {
					w.main += " — " + so.Artist
				}
			}
			a.drawRow(c, r, w)
		}
	})
}
