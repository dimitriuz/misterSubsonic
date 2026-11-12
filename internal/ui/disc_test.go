package ui

import (
	"strings"
	"testing"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func TestDiscArtSwitchesTheNowPlayingArt(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	so := subsonic.Song{ID: "s", AlbumID: "AL", CoverArt: "mf-xyz", DiscNumber: 2}
	if got := ta.nowArt(so); got != "mf-xyz" {
		t.Fatalf("off: art %q", got)
	}
	ta.setDiscArt(true)
	if got := ta.nowArt(so); got != "dc-AL:2" {
		t.Fatalf("on: art %q", got)
	}
	if got := ta.nowArt(subsonic.Song{CoverArt: "mf-xyz"}); got != "mf-xyz" { // no album to ask for
		t.Fatalf("on: song without album %q", got)
	}
	if got := ta.nowArt(subsonic.Song{AlbumID: "AL", CoverArt: "12345"}); got != "12345" { // another server's ID
		t.Fatalf("on: foreign art %q", got)
	}
}

// Lists and cards keep covers whatever the setting says.
func TestListsKeepCoversWithDiscArtOn(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.setDiscArt(true)
	ta.Push(NewAlbumsScreen())
	ta.settle(t)
	for id := range ta.arts {
		if strings.HasPrefix(string(id), "dc-") {
			t.Fatalf("a list asked for the disc image %q", id)
		}
	}
	if len(ta.arts) == 0 {
		t.Fatal("no covers were drawn")
	}
}

func TestNowPlayingMenuSwitchesDiscArt(t *testing.T) {
	ta := npApp(t)
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	i := len(m.entries) - 1
	if got := menuLabels(m)[i]; got != "Art: Cover" {
		t.Fatalf("last entry %q", got)
	}
	m.entries[i].run(ta.App)
	if !ta.cfg.Display.DiscArt {
		t.Fatal("the choice wasn't kept in the config")
	}
	if got := discEntry(ta.App); got != "Art: Disc" {
		t.Fatalf("label %q", got)
	}
}

func TestSettingsHasDiscArt(t *testing.T) {
	if config.Default().Display.DiscArt {
		t.Fatal("disc art is on by default")
	}
}

// With disc art on, Now Playing turns the disc with the playback position,
// and every partial frame matches a full one (the test app's verify mode
// fails the test otherwise).
func TestNowPlayingDiscSpins(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.setDiscArt(true)
		ta.Push(NewNowPlayingScreen())
		first := ta.settle(t)
		if ta.spin.Empty() {
			t.Fatalf("%s: no spinning disc was drawn", p.Name)
		}
		r := ta.spin
		before := append([]uint32(nil), first.Pix...)
		ta.pl.st.Position += spinPeriod / 4
		ta.now = ta.now.Add(spinFrames[0])
		ta.onWake() // the spin frame is due: the disc's region is damaged
		after := ta.settle(t)
		changed := 0
		for y := r.Y; y < r.Bottom(); y++ {
			for x := r.X; x < r.Right(); x++ {
				if before[y*after.W+x] != after.Pix[y*after.W+x] {
					changed++
				}
			}
		}
		if changed == 0 {
			t.Fatalf("%s: a quarter turn left the disc as it was", p.Name)
		}
		ta.pl.st.Status = player.Paused // a stopped disc needs no frames
		ta.Redraw()
		ta.settle(t)
		if d := ta.untilWake(); d <= spinFrames[0] {
			t.Fatalf("%s: wakes in %v while paused", p.Name, d)
		}
	}
}

func TestACoverThatIsNotRoundDoesNotSpin(t *testing.T) {
	sq := gfx.NewImage(8, 8)
	for i := range sq.Pix {
		sq.Pix[i] = 0xFF123456
	}
	if isDisc(sq) {
		t.Fatal("a full square is not a disc")
	}
	if !isDisc(makeFakeArt(art.Key{ID: "dc-x:1", Size: 32})) {
		t.Fatal("a round image is a disc")
	}
}

func TestGoldenNowPlayingDisc(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.setDiscArt(true)
		ta.Push(NewNowPlayingScreen())
		golden(t, "nowplaying-disc-"+p.Name, ta.settle(t))
	}
}

func TestMiniBarDiscSpins(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.setDiscArt(true)
	ta.Push(NewHomeScreen())
	ta.settle(t)
	if ta.spin.Empty() || ta.spin.Y < ta.P.H/2 {
		t.Fatalf("the mini player's disc isn't spinning: %v", ta.spin)
	}
}

// With one art to show there is no menu entry to switch it.
func TestNowPlayingMenuHidesArtSwitchWithoutADisc(t *testing.T) {
	ta := npApp(t)
	q := append([]subsonic.Song(nil), ta.pl.st.Queue...)
	q[0].CoverArt = "12345" // another server's: no disc image to ask for
	ta.pl.st.Queue = q
	ta.Redraw()
	ta.settle(t)
	ta.press(input.BtnX)
	for _, l := range menuLabels(ta.Top().(*MenuScreen)) {
		if strings.HasPrefix(l, "Art:") {
			t.Fatalf("the menu offers %q", l)
		}
	}
}

// A cover whose album has a disc image gets a small disc on it, until the
// disc is what is shown.
func TestDiscBadgeShowsWhileTheCoverIsShown(t *testing.T) {
	badge := func(on bool) []uint32 {
		ta := newTestApp(t, ProfileHDMI)
		playingState(ta)
		if on {
			ta.setDiscArt(true)
		}
		ta.Push(NewNowPlayingScreen())
		f := ta.settle(t)
		return append([]uint32(nil), f.Pix...)
	}
	off := badge(false)
	// The badge is dark over the cover: compare with a cover that has none.
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	q := append([]subsonic.Song(nil), ta.pl.st.Queue...)
	q[0].CoverArt = "al-1"
	q[0].AlbumID = "" // no album: nothing to probe, so no badge
	ta.pl.st.Queue = q
	ta.Push(NewNowPlayingScreen())
	none := ta.settle(t).Pix
	diff := 0
	for i := range off {
		if off[i] != none[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("no badge on a cover whose album has a disc image")
	}
}
