package ui

import (
	"fmt"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// Views show a slice of library items and handle focus, A (open or play)
// and X (the item's menu). Screens own the loading. On HDMI albums and
// artists are cover grids; on a CRT (Profile.Cover == 0) they are lists
// with thumbnails.

// cursor is a Grid or a List, whichever the profile uses; both keep the
// same focus index.
type cursor struct {
	grid Grid
	list List
}

func (k *cursor) focus() int { return k.list.Focus }

func (k *cursor) setFocus(i int) { k.grid.Focus, k.list.Focus = i, i }

func (k *cursor) handle(a *App, e input.Event, n int) bool {
	var ok bool
	if a.P.Cover > 0 {
		ok = k.grid.Handle(e, n)
	} else {
		ok = k.list.Handle(e, n)
	}
	k.sync(a)
	return ok
}

func (k *cursor) sync(a *App) {
	if a.P.Cover > 0 {
		k.list.Focus = k.grid.Focus
	} else {
		k.grid.Focus = k.list.Focus
	}
}

func (k *cursor) nearEnd(a *App, n int) bool {
	if a.P.Cover > 0 {
		return k.grid.NearEnd(n)
	}
	return k.list.NearEnd(n)
}

// draw lays out n items as grid cells or list rows.
func (k *cursor) draw(a *App, c *gfx.Canvas, area gfx.Rect, n int, cell, rowFn func(i int, r gfx.Rect, focused bool)) {
	if a.P.Cover > 0 {
		w, h := a.coverCell()
		k.grid.Draw(c, gfx.R(area.X, area.Y+a.P.Margin/3, area.W, area.H-a.P.Margin/3), n, w, h, cell)
	} else {
		k.list.Draw(c, area, n, a.P.Row2H, rowFn)
	}
	k.sync(a) // Draw clamps the focus
}

type albumsView struct {
	albums []subsonic.Album
	cur    cursor
}

func (v *albumsView) focused(a *App) (subsonic.Album, bool) {
	if len(v.albums) == 0 {
		return subsonic.Album{}, false
	}
	return v.albums[min(v.cur.focus(), len(v.albums)-1)], true
}

func (v *albumsView) Handle(a *App, e input.Event) bool {
	if v.cur.handle(a, e, len(v.albums)) {
		return true
	}
	al, ok := v.focused(a)
	if e.Kind != input.Press || !ok {
		return false
	}
	switch e.Button {
	case input.BtnA:
		a.Push(NewAlbumScreen(al))
		return true
	case input.BtnX:
		a.openMenu(al.Name, albumMenu(a, al))
		return true
	}
	return false
}

func (v *albumsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.cur.draw(a, c, area, len(v.albums), func(i int, r gfx.Rect, focused bool) {
		al := v.albums[i]
		a.drawCoverCell(c, r, al.CoverArt, al.Name, al.Artist, focused, a.isStarred(albumStar(al)))
	}, func(i int, r gfx.Rect, focused bool) {
		al := v.albums[i]
		a.drawRow(c, r, row{cover: al.CoverArt, thumb: true, main: al.Name, sub: albumSub(al), focused: focused, starred: a.isStarred(albumStar(al))})
	})
}

// albumSub is "Artist · 1997".
func albumSub(al subsonic.Album) string {
	if al.Year > 0 {
		return fmt.Sprintf("%s · %d", al.Artist, al.Year)
	}
	return al.Artist
}

type artistsView struct {
	artists []subsonic.Artist
	cur     cursor
}

func (v *artistsView) Handle(a *App, e input.Event) bool {
	if v.cur.handle(a, e, len(v.artists)) {
		return true
	}
	if e.Kind != input.Press || len(v.artists) == 0 {
		return false
	}
	ar := v.artists[min(v.cur.focus(), len(v.artists)-1)]
	switch e.Button {
	case input.BtnA:
		a.Push(NewArtistScreen(ar))
		return true
	case input.BtnX:
		a.openMenu(ar.Name, artistMenu(a, ar))
		return true
	}
	return false
}

func (v *artistsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.cur.draw(a, c, area, len(v.artists), func(i int, r gfx.Rect, focused bool) {
		ar := v.artists[i]
		a.drawCoverCell(c, r, ar.CoverArt, ar.Name, albumCount(ar.AlbumCount), focused, a.isStarred(artistStar(ar)))
	}, func(i int, r gfx.Rect, focused bool) {
		ar := v.artists[i]
		a.drawRow(c, r, row{cover: ar.CoverArt, thumb: true, main: ar.Name, sub: albumCount(ar.AlbumCount), focused: focused, starred: a.isStarred(artistStar(ar))})
	})
}

func albumCount(n int) string {
	if n == 1 {
		return "1 album"
	}
	return fmt.Sprintf("%d albums", n)
}

// songsView is a track list: A plays the whole list from the focused song.
type songsView struct {
	songs []subsonic.Song
	list  List
}

func (v *songsView) Handle(a *App, e input.Event) bool {
	if v.list.Handle(e, len(v.songs)) {
		return true
	}
	if e.Kind != input.Press || len(v.songs) == 0 {
		return false
	}
	i := min(v.list.Focus, len(v.songs)-1)
	switch e.Button {
	case input.BtnA:
		a.playSongs(v.songs, i, false)
		return true
	case input.BtnX:
		a.openMenu(v.songs[i].Title, songMenu(a, v.songs[i]))
		return true
	case input.BtnSelect:
		a.playSongs(v.songs, 0, true)
		return true
	}
	return false
}

func (v *songsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.list.Draw(c, area, len(v.songs), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		so := v.songs[i]
		sub := so.Artist
		if so.Album != "" {
			sub += " · " + so.Album
		}
		a.drawRow(c, r, row{cover: so.CoverArt, thumb: true, main: so.Title, sub: sub, focused: focused,
			starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))})
	})
}
