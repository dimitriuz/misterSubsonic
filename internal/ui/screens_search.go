package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

const (
	searchDelay = 300 * time.Millisecond // after the last keystroke (spec §8.2)
	searchPage  = 50                     // results per kind and request
)

// Result kinds, in tab order.
const (
	resArtists = iota
	resAlbums
	resTracks
)

// SearchScreen is the on-screen keyboard with live search3 results in
// Artists / Albums / Tracks tabs. On HDMI the results sit beside the
// keyboard; on a CRT they replace it while they have the focus. A physical
// keyboard types too. B in the results goes back to the keyboard.
type SearchScreen struct {
	query     []rune
	kb        Keyboard
	inResults bool
	onTabs    bool
	tabs      Tabs
	artists   artistsView
	albums    albumsView
	songs     songsView
	more      [3]bool // a full page came back for this kind
	gen       int     // bumped by every edit; older timers and results are stale
	shown     int     // bumped whenever the results change; a next page is for one of them
	cancel    func()
	searched  string // query of the results shown
	searching bool
	err       error
}

func NewSearchScreen() *SearchScreen {
	return &SearchScreen{tabs: Tabs{Labels: []string{"Artists", "Albums", "Tracks"}}}
}

func (s *SearchScreen) Title() string { return "Search" }
func (s *SearchScreen) Enter(a *App)  {}

// Text takes physical keyboard input.
func (s *SearchScreen) Text(a *App, r rune) bool {
	if r == '\b' {
		if len(s.query) == 0 {
			return false // an empty field: Backspace goes back
		}
		s.edit(a, s.query[:len(s.query)-1])
		return true
	}
	if !unicode.IsPrint(r) {
		return false
	}
	s.edit(a, append(s.query, r))
	return true
}

// edit replaces the query and schedules a search after searchDelay; the
// previous request, if any, is cancelled.
func (s *SearchScreen) edit(a *App, q []rune) {
	s.query = q
	s.gen++
	s.err = nil
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.searching = false
	if strings.TrimSpace(string(q)) == "" {
		s.setResults(&subsonic.SearchResult{}, "")
		return
	}
	gen := s.gen
	a.After(s, searchDelay, func() {
		if gen == s.gen {
			s.search(a)
		}
	})
}

func (s *SearchScreen) search(a *App) {
	q := strings.TrimSpace(string(s.query))
	gen := s.gen
	s.searching, s.err = true, nil
	s.cancel = a.LoadCancel(s, func(ctx context.Context) (any, error) {
		return a.Library().Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchPage, AlbumCount: searchPage, SongCount: searchPage})
	}, func(v any, err error) {
		if gen != s.gen {
			return
		}
		s.searching, s.cancel = false, nil
		if err != nil {
			s.err = err
			if s.total() > 0 { // the old results stay up: say why they are stale
				a.Toast("Search failed: %s", subsonic.Classify(err).String())
			}
			return
		}
		s.setResults(v.(*subsonic.SearchResult), q)
	})
}

func (s *SearchScreen) setResults(r *subsonic.SearchResult, q string) {
	s.searched = q
	s.shown++
	s.artists = artistsView{artists: r.Artists}
	s.albums = albumsView{albums: r.Albums}
	s.songs = songsView{songs: r.Songs}
	s.more = [3]bool{len(r.Artists) == searchPage, len(r.Albums) == searchPage, len(r.Songs) == searchPage}
	counts := s.counts()
	if counts[s.tabs.Sel] == 0 { // show the first kind that has results
		for i, n := range counts {
			if n > 0 {
				s.tabs.Sel = i
				break
			}
		}
	}
	if s.total() == 0 {
		s.inResults = false
	}
}

func (s *SearchScreen) counts() [3]int {
	return [3]int{len(s.artists.artists), len(s.albums.albums), len(s.songs.songs)}
}

func (s *SearchScreen) total() int {
	c := s.counts()
	return c[0] + c[1] + c[2]
}

// loadMore fetches the next page of the current tab's kind.
func (s *SearchScreen) loadMore(a *App) {
	kind := s.tabs.Sel
	if !s.more[kind] || s.searching {
		return
	}
	s.more[kind] = false
	q, shown := s.searched, s.shown
	var sq subsonic.SearchQuery
	switch kind {
	case resArtists:
		sq.ArtistCount, sq.ArtistOffset = searchPage, len(s.artists.artists)
	case resAlbums:
		sq.AlbumCount, sq.AlbumOffset = searchPage, len(s.albums.albums)
	default:
		sq.SongCount, sq.SongOffset = searchPage, len(s.songs.songs)
	}
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().Search3(ctx, q, sq) }, func(v any, err error) {
		if shown != s.shown { // the results this page belongs to are gone
			return
		}
		if err != nil {
			s.more[kind] = true // the next move near the end tries again
			a.Toast("Couldn't load more: %s", subsonic.Classify(err).String())
			return
		}
		r := v.(*subsonic.SearchResult)
		switch kind {
		case resArtists:
			s.artists.artists = append(s.artists.artists, r.Artists...)
			s.more[kind] = len(r.Artists) == searchPage
		case resAlbums:
			s.albums.albums = append(s.albums.albums, r.Albums...)
			s.more[kind] = len(r.Albums) == searchPage
		default:
			s.songs.songs = append(s.songs.songs, r.Songs...)
			s.more[kind] = len(r.Songs) == searchPage
		}
	})
}

func (s *SearchScreen) Handle(a *App, e input.Event) bool {
	if s.inResults {
		return s.handleResults(a, e)
	}
	if s.kb.Move(e) {
		return true
	}
	if (e.Button == input.BtnRight || e.Button == input.BtnDown) && e.Kind != input.Release && s.total() > 0 {
		s.inResults, s.onTabs = true, false
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		k := s.kb.Focused()
		switch k.action {
		case keyChar:
			s.edit(a, append(s.query, k.r))
		case keySpace:
			s.edit(a, append(s.query, ' '))
		case keyDel:
			if len(s.query) > 0 {
				s.edit(a, s.query[:len(s.query)-1])
			}
		case keyLayout:
			s.kb.ToggleLayout()
		case keyClear:
			s.edit(a, nil)
		}
		return true
	case input.BtnX: // shortcut for Del
		if len(s.query) > 0 {
			s.edit(a, s.query[:len(s.query)-1])
		}
		return true
	}
	return false
}

func (s *SearchScreen) handleResults(a *App, e input.Event) bool {
	back := func() bool { s.inResults = false; return true }
	if e.Kind == input.Press && e.Button == input.BtnB {
		return back()
	}
	if s.onTabs {
		if s.tabs.Handle(e) {
			return true
		}
		switch {
		case e.Kind == input.Release:
			return false
		case e.Button == input.BtnLeft || e.Button == input.BtnUp:
			return back()
		case e.Button == input.BtnDown || e.Button == input.BtnA:
			s.onTabs = false
			return true
		}
		return false
	}
	var used bool
	switch s.tabs.Sel {
	case resArtists:
		used = s.artists.Handle(a, e)
		if used && s.artists.cur.nearEnd(a, len(s.artists.artists)) {
			s.loadMore(a)
		}
	case resAlbums:
		used = s.albums.Handle(a, e)
		if used && s.albums.cur.nearEnd(a, len(s.albums.albums)) {
			s.loadMore(a)
		}
	default:
		used = s.songs.Handle(a, e)
		if used && s.songs.list.NearEnd(len(s.songs.songs)) {
			s.loadMore(a)
		}
	}
	if used || e.Kind == input.Release {
		return used
	}
	switch e.Button {
	case input.BtnUp:
		s.onTabs = true
		return true
	case input.BtnLeft:
		return back()
	}
	return false
}

func (s *SearchScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb := a.F.Body
	field := gfx.R(area.X+p.Margin, area.Y+p.Margin/4, 0, fb.Height()+p.Margin/2)
	wide := p.Cover > 0
	var kbArea, results gfx.Rect
	if wide { // keyboard left, results right
		kw := min(area.W*2/5, 12*p.RowH)
		field.W = kw - p.Margin
		kbArea = gfx.R(field.X, field.Bottom()+p.Margin/2, field.W, 0)
		results = gfx.R(area.X+kw+p.Margin/2, area.Y, area.W-kw-p.Margin/2, area.H)
	} else {
		field.W = area.W - 2*p.Margin
		kbArea = gfx.R(field.X, field.Bottom()+p.Margin/4, field.W, 0)
		results = gfx.R(area.X, field.Bottom(), area.W, area.Bottom()-field.Bottom())
	}
	s.drawField(a, c, field)
	keyH := kbArea.W / kbUnits
	if !wide {
		keyH = p.RowH
	}
	if wide || !s.inResults {
		s.kb.Draw(a, c, gfx.R(kbArea.X, kbArea.Y, kbArea.W, s.kb.Height(keyH)), keyH, !s.inResults)
	}
	if !wide && !s.inResults {
		// Below the keyboard on a CRT: what the results hold.
		y := kbArea.Y + s.kb.Height(keyH) + p.Margin/4
		s.drawStatus(a, c, gfx.R(area.X+p.Margin, y, area.W-2*p.Margin, area.Bottom()-y))
		return
	}
	if s.total() == 0 {
		s.drawStatus(a, c, results)
		return
	}
	h := p.RowH
	counts := s.counts()
	for i, base := range []string{"Artists", "Albums", "Tracks"} {
		s.tabs.Labels[i] = fmt.Sprintf("%s %d", base, counts[i])
		if s.more[i] {
			s.tabs.Labels[i] += "+"
		}
	}
	s.tabs.Draw(a, c, gfx.R(results.X, results.Y, results.W, h), s.inResults && s.onTabs)
	view := gfx.R(results.X, results.Y+h, results.W, results.H-h)
	a.drawDimmed(!s.inResults, func() {
		switch s.tabs.Sel {
		case resArtists:
			s.artists.Draw(a, c, view)
		case resAlbums:
			s.albums.Draw(a, c, view)
		default:
			s.songs.Draw(a, c, view)
		}
	})
	if !s.inResults { // the focus is on the keyboard: dim the results
		c.Fill(view, colBg.WithAlpha(0x60))
	}
}

// drawField draws the query with a caret, or a hint when it is empty.
func (s *SearchScreen) drawField(a *App, c *gfx.Canvas, r gfx.Rect) {
	p := a.P
	f := a.F.Body
	c.Fill(r, colPanel)
	x := r.X + p.Margin/2
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	inner := r.Inset(p.Margin / 4)
	if len(s.query) == 0 {
		f.Draw(c, x, y, f.Truncate("Type or pick letters", r.W-p.Margin), colDim, inner)
		return
	}
	text := string(s.query)
	// Keep the end of a long query (where the typing is) visible.
	for f.Measure(text) > r.W-p.Margin-f.Ascent()/2 && len(text) > 0 {
		_, size := firstRune(text)
		text = text[size:]
	}
	end := f.Draw(c, x, y, text, colText, inner)
	c.Fill(gfx.R(end+1, y-f.Ascent(), max(f.Ascent()/8, 2), f.Ascent()+f.Descent()), colAccent)
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}

// drawStatus explains the result state: searching, an error, nothing
// found, or (on a CRT under the keyboard) the counts.
func (s *SearchScreen) drawStatus(a *App, c *gfx.Canvas, r gfx.Rect) {
	f := a.F.Small
	var text string
	col := colDim
	switch {
	case s.err != nil:
		text, col = "Search failed: "+subsonic.Classify(s.err).String(), colError
	case s.searching:
		text = "Searching…"
	case s.searched != "" && s.total() == 0:
		text = "Nothing found for “" + s.searched + "”"
	case s.total() > 0:
		c := s.counts()
		text = plural(c[0], "artist") + " · " + plural(c[1], "album") + " · " + plural(c[2], "track") + " — Down for results"
	case len(s.query) == 0:
		text = "Results appear as you type"
	default:
		return
	}
	f.Draw(c, r.X+a.P.Margin/2, r.Y+f.Ascent(), f.Truncate(text, r.W-a.P.Margin), col, r)
}

// plural is "1 album", "3 albums".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
