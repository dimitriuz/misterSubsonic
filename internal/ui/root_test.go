package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func TestRootPicksTheLayout(t *testing.T) {
	if _, ok := NewRootScreen(ProfileHDMI).(*SidebarRoot); !ok {
		t.Fatal("HDMI root is not the sidebar")
	}
	if _, ok := NewRootScreen(ProfileCRT240).(*HomeScreen); !ok {
		t.Fatal("CRT root is not the sections list")
	}
}

func TestSidebarSwitchesSections(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	root := newSidebarRoot()
	ta.Push(root)
	ta.settle(t)
	if _, ok := root.children[0].(*FeedScreen); !ok || root.inSidebar {
		t.Fatalf("starts in %T, sidebar %v", root.children[0], root.inSidebar)
	}
	ta.press(input.BtnLeft) // first cover: Left goes to the sidebar
	if !root.inSidebar {
		t.Fatal("Left on the first cover did not reach the sidebar")
	}
	ta.press(input.BtnDown) // Artists: opens once the selection rests
	dwell(ta)
	ta.settle(t)
	artists, ok := root.children[1].(*ArtistsScreen)
	if !ok || len(artists.view.artists) != 3 || root.Title() != "Artists · B" {
		t.Fatalf("section %T, title %q", root.children[1], root.Title())
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // in the grid
	if root.inSidebar || artists.view.cur.focus() != 1 {
		t.Fatalf("sidebar %v, focus %d", root.inSidebar, artists.view.cur.focus())
	}
	ta.press(input.BtnB) // B in a section: the sidebar, not an exit
	if !root.inSidebar || ta.Top() != root {
		t.Fatalf("after B: sidebar %v top %T", root.inSidebar, ta.Top())
	}
	ta.press(input.BtnUp)
	ta.press(input.BtnDown)
	if root.children[1] != artists {
		t.Fatal("switching back recreated the section")
	}
	for range 4 {
		ta.press(input.BtnDown) // Search
	}
	ta.press(input.BtnA)
	typeKeys(ta, "bj")
	if s := root.children[5].(*SearchScreen); string(s.query) != "bj" {
		t.Fatalf("typing reached %q", string(s.query))
	}
}

func TestFeedRowsAndResume(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	s := NewFeedScreen()
	ta.Push(s)
	ta.settle(t)
	var types []string
	for _, q := range ta.lib.calls {
		if q.Size != feedPage {
			t.Fatalf("row query %+v", q)
		}
		types = append(types, q.Type)
	}
	slices.Sort(types) // the rows load in parallel
	want := []string{subsonic.ListNewest, subsonic.ListRecent, subsonic.ListFrequent, subsonic.ListRandom}
	slices.Sort(want)
	if !slices.Equal(types, want) {
		t.Fatalf("rows loaded %v", types)
	}
	if s.current() != s.rows[0] {
		t.Fatal("the focus moved off the first row when Resume arrived")
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // stops at the last cover
	ta.press(input.BtnA)
	if al, ok := ta.Top().(*AlbumScreen); !ok || al.stub.ID != "al-3" {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnUp) // the Resume card
	ta.press(input.BtnA)
	if !slices.Equal(ta.pl.calls, []string{"resume"}) {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestCRTHomeOpensSections(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.Push(NewRootScreen(ta.P))
	ta.settle(t)
	for range 4 { // past the album lists
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnDown) // Albums
	ta.press(input.BtnA)
	if s, ok := ta.Top().(*TabbedScreen); !ok || s.Title() != "Albums" {
		t.Fatalf("A opened %T", ta.Top())
	}
}

func TestGoldenRoot(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	root := newSidebarRoot()
	ta.Push(root)
	ta.settle(t)
	golden(t, "root-feed-hdmi", ta.settle(t))
	ta.press(input.BtnLeft)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	dwell(ta)
	ta.settle(t)
	golden(t, "root-albums-hdmi", ta.settle(t))
}

// Every screen, in both layouts, with a normal, an empty and a failing
// library, before and after its loads finish: every button, pressed and
// held, must neither panic nor empty the stack.
func TestEveryScreenSurvivesEveryButton(t *testing.T) {
	screens := map[string]func(ta *testApp) Screen{
		"root":      func(ta *testApp) Screen { return NewRootScreen(ta.P) },
		"feed":      func(*testApp) Screen { return NewFeedScreen() },
		"artists":   func(*testApp) Screen { return NewArtistsScreen() },
		"artist":    func(*testApp) Screen { return NewArtistScreen(subsonic.Artist{ID: "ar-1", Name: "Аквариум"}) },
		"albums":    func(*testApp) Screen { return NewAlbumsScreen() },
		"albumlist": func(*testApp) Screen { return NewAlbumListScreen("Recently added", subsonic.ListNewest) },
		"album": func(ta *testApp) Screen {
			return NewAlbumScreen(subsonic.Album{ID: "al-1", Name: "Радио Африка"})
		},
		"genres":    func(*testApp) Screen { return NewGenresScreen() },
		"playlists": func(*testApp) Screen { return NewPlaylistsScreen() },
		"playlist": func(*testApp) Screen {
			return NewPlaylistScreen(subsonic.Playlist{ID: "pl-1", Name: "Дорога домой"})
		},
		"starred": func(*testApp) Screen { return NewStarredScreen() },
		"search":  func(*testApp) Screen { return NewSearchScreen() },
		"playing": func(*testApp) Screen { return NewNowPlayingScreen() },
		"queue":   func(*testApp) Screen { return NewQueueScreen() },
	}
	buttons := []input.Button{input.BtnUp, input.BtnDown, input.BtnLeft, input.BtnRight, input.BtnL, input.BtnR,
		input.BtnX, input.BtnSelect, input.BtnStart, input.BtnA, input.BtnY, input.BtnQueue, input.BtnB}
	libs := map[string]func(ta *testApp){
		"normal":  func(*testApp) {},
		"empty":   func(ta *testApp) { ta.lib.albums, ta.lib.artists, ta.lib.playlists, ta.lib.genres = nil, nil, nil, nil },
		"failing": func(ta *testApp) { ta.lib.err = errOffline },
	}
	for _, p := range profiles {
		for sname, mk := range screens {
			for lname, setup := range libs {
				for _, early := range []bool{true, false} {
					ta := newTestApp(t, p)
					setup(ta)
					ta.Push(NewHomeScreen())
					ta.Push(mk(ta))
					if !early {
						ta.settle(t)
					}
					for round := range 2 {
						for _, b := range buttons {
							ta.onInput(input.Event{Button: b, Kind: input.Press})
							ta.dispatch(input.Event{Button: b, Kind: input.Repeat})
							ta.onInput(input.Event{Button: b, Kind: input.Release})
							if len(ta.stack) == 0 || ta.Top() == nil {
								t.Fatalf("%s/%s/%s round %d: %v emptied the stack", p.Name, sname, lname, round, b)
							}
						}
						ta.settle(t)
					}
				}
			}
		}
	}
}
