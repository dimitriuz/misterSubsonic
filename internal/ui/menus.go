package ui

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"mistersubsonic/internal/remote"

	"mistersubsonic/internal/subsonic"
)

// Item menus (X) and the play actions behind them. Entries run after the
// menu has closed, so a.Top() is the screen the menu was opened from; loads
// run under it.

// openMenu shows entries over the current screen.
func (a *App) openMenu(title string, entries []menuEntry) {
	a.Push(NewMenuScreen(a.Top(), title, entries))
}

// playSongs replaces the queue with songs, starting at start (or at a
// random song, shuffled), and opens Now Playing.
func (a *App) playSongs(songs []subsonic.Song, start int, shuffle bool) {
	if len(songs) == 0 || a.Player() == nil {
		return
	}
	a.Player().SetShuffle(shuffle)
	a.notifyRemote(remote.StateChanged)
	if shuffle {
		start = shuffleStart(len(songs))
	}
	a.Player().PlayNow(songs, start)
	a.Push(NewNowPlayingScreen())
}

// songLoader fetches the songs behind an item (an album, a playlist...).
type songLoader func(ctx context.Context, lib Library) ([]subsonic.Song, error)

// partialLoad is the error of a load that got some of its songs: how many
// of its albums failed. withSongs plays what loaded and says so.
type partialLoad struct{ failed, total int }

func (e *partialLoad) Error() string {
	return fmt.Sprintf("%d of %d albums failed", e.failed, e.total)
}

// withSongs loads songs off the UI goroutine, then calls f with them; an
// error or an empty result is a toast instead.
func (a *App) withSongs(load songLoader, f func([]subsonic.Song)) {
	lib := a.Library()
	if lib == nil {
		return
	}
	a.Load(a.Top(), func(ctx context.Context) (any, error) { return load(ctx, lib) }, func(v any, err error) {
		songs, _ := v.([]subsonic.Song)
		var part *partialLoad
		if errors.As(err, &part) {
			a.Toast("Couldn't load %d of %d albums", part.failed, part.total)
			err = nil
		}
		switch {
		case err != nil:
			a.Toast("Couldn't load: %s", subsonic.Classify(err))
		case len(songs) == 0:
			a.Toast("Nothing to play")
		default:
			f(songs)
		}
	})
}

func albumSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		al, err := lib.GetAlbum(ctx, id)
		if err != nil {
			return nil, err
		}
		return al.Songs, nil
	}
}

func playlistSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		pl, err := lib.GetPlaylist(ctx, id)
		if err != nil {
			return nil, err
		}
		return pl.Songs, nil
	}
}

// maxShuffleAlbums caps how many albums a shuffle of a list or an artist
// loads (one getAlbum each).
const maxShuffleAlbums = 10

// albumsSongs loads the songs of albums, one request each. Albums that fail
// are skipped unless all do; when some do, the error is a *partialLoad
// beside the songs.
func albumsSongs(albums []subsonic.Album) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		var songs []subsonic.Song
		var firstErr error
		failed := 0
		for _, al := range albums {
			got, err := albumSongs(al.ID)(ctx, lib)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				firstErr = cmpErr(firstErr, err)
				failed++
				continue
			}
			songs = append(songs, got...)
		}
		if len(songs) == 0 {
			return nil, firstErr
		}
		if failed > 0 {
			return songs, &partialLoad{failed, len(albums)}
		}
		return songs, nil
	}
}

func cmpErr(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

// sampleAlbums picks up to maxShuffleAlbums albums at random (Select on a
// long album list shuffles a sample, not the whole library).
func sampleAlbums(albums []subsonic.Album) []subsonic.Album {
	if len(albums) <= maxShuffleAlbums {
		return albums
	}
	out := make([]subsonic.Album, 0, maxShuffleAlbums)
	for _, i := range rand.Perm(len(albums))[:maxShuffleAlbums] {
		out = append(out, albums[i])
	}
	return out
}

func artistSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		ar, err := lib.GetArtist(ctx, id)
		if err != nil {
			return nil, err
		}
		return albumsSongs(sampleAlbums(ar.Albums))(ctx, lib)
	}
}

// queueEntries are the play actions for a group of songs loaded on demand.
func queueEntries(name string, load songLoader) []menuEntry {
	return []menuEntry{
		{"Play now", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.playSongs(s, 0, false) })
		}},
		{"Shuffle", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.playSongs(s, 0, true) })
		}},
		{"Play next", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.Player().PlayNext(s); a.Toast("Playing next: %s", name) })
		}},
		{"Add to queue", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.Player().Enqueue(s); a.Toast("Added to queue: %s", name) })
		}},
	}
}

func starEntry(a *App, it starItem) menuEntry {
	label := "Star"
	if a.isStarred(it) {
		label = "Unstar"
	}
	return menuEntry{label, func(a *App) { a.toggleStar(it) }}
}

// The menu builders take the App to label Star/Unstar.

func songMenu(a *App, so subsonic.Song) []menuEntry {
	one := []subsonic.Song{so}
	out := []menuEntry{
		{"Play now", func(a *App) { a.playSongs(one, 0, false) }},
		{"Play next", func(a *App) { a.Player().PlayNext(one); a.Toast("Playing next: %s", so.Title) }},
		{"Add to queue", func(a *App) { a.Player().Enqueue(one); a.Toast("Added to queue: %s", so.Title) }},
		starEntry(a, songStar(so)),
	}
	if so.AlbumID != "" {
		out = append(out, menuEntry{"Go to album", func(a *App) {
			a.Push(NewAlbumScreen(subsonic.Album{ID: so.AlbumID, Name: so.Album, Artist: so.Artist,
				ArtistID: so.ArtistID, CoverArt: so.CoverArt, Year: so.Year}))
		}})
	}
	if so.ArtistID != "" {
		out = append(out, goToArtist(so.ArtistID, so.Artist))
	}
	return out
}

func albumMenu(a *App, al subsonic.Album) []menuEntry {
	out := append(queueEntries(al.Name, albumSongs(al.ID)), starEntry(a, albumStar(al)))
	if al.ArtistID != "" {
		out = append(out, goToArtist(al.ArtistID, al.Artist))
	}
	return out
}

func artistMenu(a *App, ar subsonic.Artist) []menuEntry {
	return []menuEntry{
		{"Shuffle", func(a *App) {
			a.withSongs(artistSongs(ar.ID), func(s []subsonic.Song) { a.playSongs(s, 0, true) })
		}},
		starEntry(a, artistStar(ar)),
		goToArtist(ar.ID, ar.Name),
	}
}

func goToArtist(id subsonic.ID, name string) menuEntry {
	return menuEntry{"Go to artist", func(a *App) { a.Push(NewArtistScreen(subsonic.Artist{ID: id, Name: name})) }}
}
