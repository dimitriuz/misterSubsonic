package ui

import (
	"testing"

	"mistersubsonic/internal/subsonic"
)

// Subsonic IDs are per table: artist "7", album "7" and song "7" coexist.
func TestStarStateIsPerKind(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	artist := starItem{kind: starArtist, id: "7", name: "Artist seven"}
	album := starItem{kind: starAlbum, id: "7", name: "Album seven"}
	ta.toggleStar(s, artist)
	ta.settle(t)
	if !ta.isStarred(artist) {
		t.Fatal("artist 7 not starred")
	}
	if ta.isStarred(album) || ta.isStarred(starItem{kind: starSong, id: "7"}) {
		t.Fatal("starring artist 7 starred album/song 7")
	}
	if m := albumMenu(ta.App, subsonic.Album{ID: "7", Name: "Album seven"}); m[starMenuIndex(m)].label != "Star" {
		t.Fatalf("album menu offers %q", m[starMenuIndex(m)].label)
	}
}

func starMenuIndex(m []menuEntry) int {
	for i, e := range m {
		if e.label == "Star" || e.label == "Unstar" {
			return i
		}
	}
	return 0
}
