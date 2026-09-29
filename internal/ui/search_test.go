package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func TestKeyboardNavigation(t *testing.T) {
	var k Keyboard
	move := func(b input.Button) bool { return k.Move(press(b)) }
	if move(input.BtnUp) || move(input.BtnLeft) {
		t.Fatal("moved past the top-left corner")
	}
	move(input.BtnDown) // "1" -> "a"
	if got := k.Focused(); got.r != 'a' {
		t.Fatalf("focused %q", got.label)
	}
	for range 9 {
		move(input.BtnRight)
	}
	if move(input.BtnRight) || k.Focused().r != 'j' {
		t.Fatalf("right edge: %q", k.Focused().label)
	}
	move(input.BtnDown)
	move(input.BtnDown)
	move(input.BtnDown) // bottom row, nearest to "j" (the far right): Clear
	if got := k.Focused(); got.action != keyClear {
		t.Fatalf("under j: %q", got.label)
	}
	move(input.BtnLeft)
	k.Switch(k.Focused().to)
	if got := k.Focused(); got.label != "ABC" || k.cur != 1 {
		t.Fatalf("after toggle: %q", got.label)
	}
	move(input.BtnUp) // "эюя-'.&": the key nearest the middle of the switch key
	if got := k.Focused(); got.r != '&' {
		t.Fatalf("above the switch key: %q", got.label)
	}
	if move(input.BtnDown); move(input.BtnDown) {
		t.Fatal("moved below the bottom row")
	}
}

func typeKeys(ta *testApp, s string) {
	for _, r := range s {
		ta.onInput(input.Event{Kind: input.Press, Rune: r})
		ta.onInput(input.Event{Kind: input.Release, Rune: r})
	}
}

func TestSearchDebouncesAndCancels(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.block = make(chan struct{})
	typeKeys(ta, "bj")
	ta.settle(t)
	if len(ta.lib.searches) != 0 {
		t.Fatalf("searched before the pause: %v", ta.lib.searches)
	}
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	waitSearches(t, ta, 1)
	typeKeys(ta, "ö") // cancels "bj" still in flight
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	waitSearches(t, ta, 2)
	close(ta.lib.block)
	ta.settle(t)
	if !slices.Equal(ta.lib.searches, []string{"bj", "bjö"}) || s.searched != "bjö" {
		t.Fatalf("searches %v, showing %q", ta.lib.searches, s.searched)
	}
	if len(s.artists.artists) != 1 || s.artists.artists[0].Name != "Björk" || s.searching {
		t.Fatalf("artists %v searching %v", s.artists.artists, s.searching)
	}
	// Backspace edits; the empty field clears the results and then goes back.
	for range 3 {
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
	}
	if len(s.query) != 0 || ta.Top() != s {
		t.Fatalf("query %q top %T", string(s.query), ta.Top())
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
	if ta.Top() == s {
		t.Fatal("Backspace in the empty field did not go back")
	}
}

// waitSearches waits for n Search3 calls to have started.
func waitSearches(t *testing.T, ta *testApp, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ta.lib.mu.Lock()
		got := len(ta.lib.searches)
		ta.lib.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d searches started, want %d", got, n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSearchWithTheOnScreenKeyboard(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.press(input.BtnDown) // "a"
	ta.press(input.BtnA)
	ta.press(input.BtnA)
	ta.press(input.BtnX) // Del
	if string(s.query) != "a" {
		t.Fatalf("query %q", string(s.query))
	}
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	// Latin "a": the artist "Some Artist" and its album "A Rather Long…".
	if c := s.counts(); c != [3]int{1, 1, 0} || s.tabs.Sel != resArtists {
		t.Fatalf("counts %v tab %d", c, s.tabs.Sel)
	}
	for range 9 {
		ta.press(input.BtnRight) // to the right edge, then into the results
	}
	ta.press(input.BtnRight)
	if !s.inResults {
		t.Fatal("Right at the keyboard's edge did not enter the results")
	}
	ta.press(input.BtnUp) // tabs
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if al, ok := ta.Top().(*AlbumScreen); !ok || al.stub.ID != "al-3" {
		t.Fatalf("A in album results opened %T", ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnB) // results -> keyboard
	if s.inResults || ta.Top() != s {
		t.Fatalf("B in the results: inResults %v top %T", s.inResults, ta.Top())
	}
}

func TestSearchPagesTracks(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	var many []subsonic.Song
	for i := range 70 {
		many = append(many, subsonic.Song{ID: subsonic.ID(fmt.Sprint("t", i)), Title: fmt.Sprint("Tune ", i), Duration: 60})
	}
	ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: "al-9", Name: "Filler"})
	ta.lib.tracks["al-9"] = many
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	typeKeys(ta, "tune")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if len(s.songs.songs) != searchPage || s.tabs.Sel != resTracks || !s.more[resTracks] {
		t.Fatalf("%d songs, tab %d, more %v", len(s.songs.songs), s.tabs.Sel, s.more)
	}
	for i := 0; !s.inResults; i++ { // down the keyboard and past its bottom row
		if i > 10 {
			t.Fatal("Down never reached the results")
		}
		ta.press(input.BtnDown)
	}
	for range 60 {
		ta.press(input.BtnDown)
	}
	ta.settle(t)
	if len(s.songs.songs) != 70 || s.more[resTracks] {
		t.Fatalf("after scrolling: %d songs, more %v", len(s.songs.songs), s.more[resTracks])
	}
}

func TestGoldenSearch(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		s := NewSearchScreen()
		ta.Push(s)
		typeKeys(ta, "a")
		ta.now = ta.now.Add(searchDelay)
		ta.onWake()
		ta.press(input.BtnDown)
		ta.press(input.BtnRight)
		golden(t, "search-"+p.Name, ta.settle(t))
		s.inResults = true
		golden(t, "search-results-"+p.Name, ta.settle(t))
	}
}

// The network can drop while searching: the error shows, and the next
// keystroke searches again.
func TestSearchErrorThenRetry(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.err = errOffline
	typeKeys(ta, "b")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.err == nil || s.total() != 0 {
		t.Fatalf("err %v, %d results", s.err, s.total())
	}
	ta.lib.err = nil
	typeKeys(ta, "j")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.err != nil || len(s.artists.artists) != 1 {
		t.Fatalf("after retry: err %v, artists %v", s.err, s.artists.artists)
	}
}

// pump runs posted UI callbacks until cond holds (loads may stay in flight).
func pump(t *testing.T, ta *testApp, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !cond() {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("condition not reached")
		}
	}
}

func tuneSearch(t *testing.T, p Profile) (*testApp, *SearchScreen) {
	ta := newTestApp(t, p)
	var many []subsonic.Song
	for i := range 70 {
		many = append(many, subsonic.Song{ID: subsonic.ID(fmt.Sprint("t", i)), Title: fmt.Sprint("Tune ", i), Duration: 60})
	}
	ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: "al-9", Name: "Filler"})
	ta.lib.tracks["al-9"] = many
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	typeKeys(ta, "tune")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	for i := 0; !s.inResults && i < 11; i++ {
		ta.press(input.BtnDown)
	}
	return ta, s
}

// A next page requested for the old query must not land on the new results.
func TestSearchDropsStalePage(t *testing.T) {
	ta, s := tuneSearch(t, ProfileCRT240)
	old := make(chan struct{})
	ta.lib.mu.Lock()
	ta.lib.block = old
	ta.lib.mu.Unlock()
	typeKeys(ta, " 1")                   // "tune 1": 11 tracks; the search is still pending
	for i := 0; s.more[resTracks]; i++ { // scroll until the next page is requested
		if i > 60 {
			t.Fatal("no next page requested")
		}
		ta.press(input.BtnDown)
	}
	waitSearches(t, ta, 2)
	ta.lib.mu.Lock()
	ta.lib.block = nil
	ta.lib.mu.Unlock()
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	pump(t, ta, func() bool { return s.searched == "tune 1" })
	close(old) // the old page arrives after the new results
	pump(t, ta, func() bool { return ta.loads == 0 })
	if len(s.songs.songs) != 11 {
		t.Fatalf("%d songs after a stale page, want 11", len(s.songs.songs))
	}
}

func TestSearchErrorOverResultsToasts(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	typeKeys(ta, "a")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.total() == 0 {
		t.Fatal("no first results")
	}
	ta.lib.err = errOffline
	typeKeys(ta, "b")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	want := "Search failed: " + subsonic.Classify(errOffline).String()
	if s.err == nil || s.total() == 0 || len(ta.toasts) == 0 || ta.toasts[len(ta.toasts)-1].text != want {
		t.Fatalf("err %v, %d results, toasts %v; want toast %q", s.err, s.total(), ta.toasts, want)
	}
}

func TestSearchFailedPageIsReportedAndRetried(t *testing.T) {
	ta, s := tuneSearch(t, ProfileCRT240)
	ta.lib.mu.Lock()
	ta.lib.err = errOffline
	ta.lib.mu.Unlock()
	for range 60 {
		ta.press(input.BtnDown)
	}
	ta.settle(t)
	want := "Couldn't load more: " + subsonic.Classify(errOffline).String()
	if len(s.songs.songs) != searchPage || !s.more[resTracks] || len(ta.toasts) == 0 || ta.toasts[len(ta.toasts)-1].text != want {
		t.Fatalf("%d songs, more %v, toasts %v; want toast %q", len(s.songs.songs), s.more, ta.toasts, want)
	}
	ta.lib.mu.Lock()
	ta.lib.err = nil
	ta.lib.mu.Unlock()
	ta.press(input.BtnUp)
	ta.press(input.BtnDown)
	ta.settle(t)
	if len(s.songs.songs) != 70 || s.more[resTracks] {
		t.Fatalf("after retry: %d songs, more %v", len(s.songs.songs), s.more)
	}
}

func TestSearchEditClearsError(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.err = errOffline
	typeKeys(ta, "b")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.err == nil {
		t.Fatal("no error to clear")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
	if s.err != nil {
		t.Fatalf("error %v survived emptying the field", s.err)
	}
}
