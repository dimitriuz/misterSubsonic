package ui

import (
	"context"

	"mistersubsonic/internal/subsonic"
)

type starKind int

const (
	starSong starKind = iota
	starAlbum
	starArtist
)

// starItem is something the user can star: a song, an album or an artist.
type starItem struct {
	kind   starKind
	id     subsonic.ID
	name   string
	server bool // starred according to the server's data
}

// starKey identifies an item: servers number songs, albums and artists
// separately, so an id alone isn't unique.
type starKey struct {
	kind starKind
	id   subsonic.ID
}

func (it starItem) key() starKey { return starKey{it.kind, it.id} }

func songStar(s subsonic.Song) starItem {
	return starItem{starSong, s.ID, s.Title, s.IsStarred()}
}
func albumStar(al subsonic.Album) starItem {
	return starItem{starAlbum, al.ID, al.Name, al.IsStarred()}
}
func artistStar(ar subsonic.Artist) starItem {
	return starItem{starArtist, ar.ID, ar.Name, ar.IsStarred()}
}

func (it starItem) target() subsonic.StarTarget {
	switch it.kind {
	case starAlbum:
		return subsonic.StarTarget{AlbumIDs: []subsonic.ID{it.id}}
	case starArtist:
		return subsonic.StarTarget{ArtistIDs: []subsonic.ID{it.id}}
	}
	return subsonic.StarTarget{SongIDs: []subsonic.ID{it.id}}
}

// isStarred is the star state of it: changes made in this session win over
// the (possibly older) server data a screen loaded.
func (a *App) isStarred(it starItem) bool {
	if on, ok := a.stars[it.key()]; ok {
		return on
	}
	return it.server
}

// toggleStar stars or unstars it on the server (off the UI goroutine,
// under owner) and remembers the new state once the server agrees.
func (a *App) toggleStar(owner Screen, it starItem) {
	lib := a.Library()
	if lib == nil {
		return
	}
	on := !a.isStarred(it)
	a.Load(owner, func(ctx context.Context) (any, error) {
		if on {
			return nil, lib.Star(ctx, it.target())
		}
		return nil, lib.Unstar(ctx, it.target())
	}, func(_ any, err error) {
		verb := "unstar"
		if on {
			verb = "star"
		}
		if err != nil {
			a.Toast("Couldn't %s %s: %s", verb, it.name, subsonic.Classify(err))
			return
		}
		a.stars[it.key()] = on
		a.starGen++
		if on {
			a.Toast("Starred %s", it.name)
		} else {
			a.Toast("Unstarred %s", it.name)
		}
	})
}
