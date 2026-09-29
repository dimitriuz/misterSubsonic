package ui

import (
	"fmt"
	"slices"
	"testing"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func TestArtistsLetterJumpsAndCache(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240) // a list: one artist per row
	ta.Push(NewHomeScreen())
	s := NewArtistsScreen()
	ta.Push(s)
	ta.settle(t)
	if s.Title() != "Artists · B" {
		t.Fatalf("title %q", s.Title())
	}
	ta.press(input.BtnR)
	ta.press(input.BtnR)
	if s.view.cur.focus() != 2 || s.Title() != "Artists · А" {
		t.Fatalf("after R R: focus %d, title %q", s.view.cur.focus(), s.Title())
	}
	ta.press(input.BtnR) // no letter after А
	ta.press(input.BtnL) // at the start of А: the previous letter
	if s.view.cur.focus() != 1 {
		t.Fatalf("after L: focus %d", s.view.cur.focus())
	}
	ta.press(input.BtnA)
	ar, ok := ta.Top().(*ArtistScreen)
	if !ok || ar.artist.ID != "ar-3" {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.settle(t)
	if len(ar.view.albums) != 1 || ar.view.albums[0].ID != "al-3" {
		t.Fatalf("artist albums %v", ar.view.albums)
	}
	// Artists are fetched once per connection.
	ta.lib.artists = nil
	again := NewArtistsScreen()
	ta.Push(again)
	ta.settle(t)
	if len(again.view.artists) != 3 {
		t.Fatalf("second Artists screen has %d artists, want the cached 3", len(again.view.artists))
	}
}

func TestArtistSelectShufflesItsAlbums(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewArtistScreen(subsonic.Artist{ID: "ar-1", Name: "Аквариум"}))
	ta.settle(t)
	ta.press(input.BtnSelect)
	ta.settle(t)
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 3 {
		t.Fatalf("shuffle %v, played %d songs", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("top %T", ta.Top())
	}
}

func TestAlbumsTabs(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewAlbumsScreen()
	ta.Push(s)
	ta.settle(t)
	if q := ta.lib.calls[0]; q.Type != subsonic.ListAlphabetical {
		t.Fatalf("first tab query %+v", q)
	}
	ta.press(input.BtnUp) // from the grid's top row to the tabs
	ta.press(input.BtnRight)
	ta.settle(t)
	if q := ta.lib.calls[1]; q.Type != subsonic.ListByYear || q.FromYear != 9999 || q.ToYear != 0 {
		t.Fatalf("by-year query %+v", q)
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.settle(t)
	g := s.children[2].(*GenresScreen)
	if len(g.genres) != 2 || g.genres[0].Name != "Electronic" {
		t.Fatalf("genres %v (want sorted by name, case-insensitive)", g.genres)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // rock
	ta.settle(t)
	if q := ta.lib.calls[2]; q.Type != subsonic.ListByGenre || q.Genre != "rock" {
		t.Fatalf("genre query %+v", q)
	}
	if ta.Top().Title() != "rock" {
		t.Fatalf("top %q", ta.Top().Title())
	}
}

func TestAlbumMenuActions(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok {
		t.Fatalf("X opened %T", ta.Top())
	}
	var labels []string
	for _, e := range m.entries {
		labels = append(labels, e.label)
	}
	if !slices.Equal(labels, []string{"Play now", "Shuffle", "Play next", "Add to queue", "Star", "Go to artist"}) {
		t.Fatalf("album menu %v", labels)
	}
	for range 3 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Add to queue
	ta.settle(t)
	if !slices.Equal(ta.pl.calls, []string{"enqueue"}) || len(ta.pl.played) != 3 {
		t.Fatalf("calls %v, songs %d", ta.pl.calls, len(ta.pl.played))
	}
	if ta.toasts[0].text != "Added to queue: Радио Африка" {
		t.Fatalf("toast %q", ta.toasts[0].text)
	}
	ta.press(input.BtnX)
	for range 5 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Go to artist
	if ar, ok := ta.Top().(*ArtistScreen); !ok || ar.artist.ID != "ar-1" {
		t.Fatalf("Go to artist opened %T", ta.Top())
	}
}

func TestSongMenuFromAlbumAndStarRow(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	album := NewAlbumScreen(ta.lib.albums[0])
	ta.Push(album)
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // Star
	ta.press(input.BtnA)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star al-1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.press(input.BtnDown) // first track
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if m.title != "Капитан Африка" || m.entries[len(m.entries)-2].label != "Go to album" {
		t.Fatalf("song menu %q %v", m.title, m.entries)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Play next
	if !slices.Equal(ta.pl.calls, []string{"playnext"}) || ta.pl.played[0].ID != "s1" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestAlbumListSelectShufflesASample(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnSelect)
	ta.settle(t)
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 3 {
		t.Fatalf("shuffle %v, %d songs (only al-1 has tracks)", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	many := make([]subsonic.Album, 40)
	for i := range many {
		many[i].ID = subsonic.ID(rune('a' + i))
	}
	if got := sampleAlbums(many); len(got) != maxShuffleAlbums {
		t.Fatalf("sample of %d", len(got))
	}
}

func TestGoldenBrowse(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewArtistsScreen())
		ta.settle(t)
		ta.press(input.BtnR)
		golden(t, "artists-"+p.Name, ta.settle(t))
		ta.Pop()
		ta.Push(NewAlbumsScreen())
		ta.settle(t)
		ta.press(input.BtnUp)
		golden(t, "albums-tabs-"+p.Name, ta.settle(t))
	}
}

// A big library: thousands of artists, letter jumps across all of them.
func TestArtistsLetterJumpInAHugeLibrary(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.artists = nil
	letters := "#ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, l := range letters {
		ix := subsonic.ArtistIndex{Name: string(l)}
		for i := range 200 {
			ix.Artists = append(ix.Artists, subsonic.Artist{ID: subsonic.ID(fmt.Sprint(string(l), i)), Name: fmt.Sprint(string(l), " artist ", i)})
		}
		ta.lib.artists = append(ta.lib.artists, ix)
	}
	ta.Push(NewHomeScreen())
	s := NewArtistsScreen()
	ta.Push(s)
	ta.settle(t)
	for range len(letters) + 3 {
		ta.press(input.BtnR)
	}
	if s.view.cur.focus() != 200*(len(letters)-1) || s.Title() != "Artists · Z" {
		t.Fatalf("focus %d title %q", s.view.cur.focus(), s.Title())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnL) // inside Z: back to its start
	ta.press(input.BtnL) // then Y
	if s.Title() != "Artists · Y" || s.view.cur.focus() != 200*(len(letters)-2) {
		t.Fatalf("after L L: focus %d title %q", s.view.cur.focus(), s.Title())
	}
	ta.settle(t)
}

// Holding Down through a long list fires repeats every 45 ms; each page must
// be requested once, not once per repeat.
func TestHeldScrollLoadsEachPageOnce(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	for i := range 250 {
		ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: subsonic.ID(fmt.Sprint("x", i)), Name: "Filler"})
	}
	ta.Push(NewHomeScreen())
	s := NewAlbumListScreen("Recently added", subsonic.ListNewest)
	ta.Push(s)
	ta.settle(t)
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Press})
	for i := range 300 {
		ta.dispatch(input.Event{Button: input.BtnDown, Kind: input.Repeat})
		if i%25 == 0 {
			ta.settle(t)
		}
	}
	ta.settle(t)
	var offsets []int
	for _, q := range ta.lib.calls {
		offsets = append(offsets, q.Offset)
	}
	if !slices.Equal(offsets, []int{0, 100, 200}) || len(s.view.albums) != 253 {
		t.Fatalf("page offsets %v, %d albums", offsets, len(s.view.albums))
	}
}

// Servers without OpenSubsonic, or items from playlists, can lack album and
// artist IDs: the menus must not offer to go where there is nothing.
func TestMenusWithoutIDs(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	for _, e := range songMenu(ta.App, subsonic.Song{ID: "s", Title: "Loose"}) {
		if e.label == "Go to album" || e.label == "Go to artist" {
			t.Fatalf("song menu offers %q without an ID", e.label)
		}
	}
	for _, e := range albumMenu(ta.App, subsonic.Album{ID: "a", Name: "Various"}) {
		if e.label == "Go to artist" {
			t.Fatal("album menu offers Go to artist without an artist ID")
		}
	}
}
