package ui

import (
	"context"
	"fmt"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// SidebarRoot is the HDMI root: a sidebar of sections on the left and the
// selected section on the right. Left from a section's first column, or B,
// moves the focus to the sidebar; Right or A goes back into the section.
// Sections are created and entered the first time they are opened (Right or
// A, or the selection resting on them for sidebarDwell, so scrolling past a
// section doesn't load it), and keep their state while the user switches.
type SidebarRoot struct {
	labels    []string
	makers    []func() Screen
	children  []Screen
	sel       int
	inSidebar bool
	dwelling  int // bumped by every selection change; an older dwell timer is stale
}

// shower is a screen that wants to know when it becomes visible again
// (a tab or a sidebar section that was entered earlier).
type shower interface {
	Shown(a *App)
}

func newSidebarRoot() *SidebarRoot {
	s := &SidebarRoot{labels: []string{"Home"}, makers: []func() Screen{func() Screen { return NewFeedScreen() }}}
	for _, it := range sections {
		s.labels = append(s.labels, it.label)
		s.makers = append(s.makers, it.open)
	}
	s.children = make([]Screen, len(s.makers))
	return s
}

func (s *SidebarRoot) Title() string {
	if s.sel == 0 || s.children[s.sel] == nil {
		return "MiSTer Subsonic"
	}
	return s.children[s.sel].Title()
}

func (s *SidebarRoot) Owns(x Screen) bool {
	for _, c := range s.children {
		if c == nil {
			continue
		}
		if c == x {
			return true
		}
		if o, ok := c.(owner); ok && o.Owns(x) {
			return true
		}
	}
	return false
}

func (s *SidebarRoot) Enter(a *App) { s.open(a) }

// open makes the selected section the visible one: it is created and
// entered on first use, told it is shown again otherwise.
func (s *SidebarRoot) open(a *App) Screen {
	if c := s.children[s.sel]; c != nil {
		if sh, ok := c.(shower); ok {
			sh.Shown(a)
		}
		return c
	}
	s.children[s.sel] = s.makers[s.sel]()
	s.children[s.sel].Enter(a)
	return s.children[s.sel]
}

// current is the selected section, nil while it hasn't been opened yet.
func (s *SidebarRoot) current() Screen { return s.children[s.sel] }

// Text forwards typing to the section (Search) when it has the focus.
func (s *SidebarRoot) Text(a *App, r rune) bool {
	if t, ok := s.current().(TextInput); ok && !s.inSidebar {
		return t.Text(a, r)
	}
	return false
}

func (s *SidebarRoot) Handle(a *App, e input.Event) bool {
	if s.inSidebar {
		switch e.Button {
		case input.BtnUp, input.BtnDown:
			if e.Kind == input.Release {
				return false
			}
			if e.Button == input.BtnUp {
				s.sel = max(s.sel-1, 0)
			} else {
				s.sel = min(s.sel+1, len(s.labels)-1)
			}
			s.dwelling++
			n := s.dwelling
			a.After(s, sidebarDwell, func() {
				if n == s.dwelling {
					s.open(a)
				}
			})
			return true
		case input.BtnRight, input.BtnA:
			if e.Kind == input.Press {
				s.inSidebar = false
				s.dwelling++
				s.open(a)
				return true
			}
		}
		return false
	}
	if c := s.current(); c != nil && c.Handle(a, e) {
		return true
	}
	if e.Kind == input.Release {
		return false
	}
	if e.Button == input.BtnLeft || (e.Button == input.BtnB && e.Kind == input.Press) {
		s.inSidebar = true
		return true
	}
	return false
}

func (s *SidebarRoot) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	side := gfx.R(area.X, area.Y, p.SideW, area.H)
	c.Fill(side, colPanel)
	f := a.F.Body
	for i, l := range s.labels {
		r := gfx.R(side.X, side.Y+p.Margin/2+i*p.RowH, side.W, p.RowH)
		col := colDim
		if i == s.sel {
			col = colText
			if s.inSidebar {
				c.Fill(r, colFocus)
			}
			c.Fill(gfx.R(r.X, r.Y, max(p.Margin/8, 3), r.H), colAccent)
		}
		f.Draw(c, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, l, col, r)
	}
	content := gfx.R(side.Right(), area.Y, area.W-side.W, area.H)
	if child := s.current(); child != nil {
		a.drawDimmed(s.inSidebar, func() { child.Draw(a, c, content) })
	} else {
		a.drawCentered(c, content, "…", colDim)
	}
}

// FeedScreen is the HDMI home: a Resume card when a saved queue exists,
// then rows of covers (recently added, recently played, most played,
// random). Up/Down pick a row, Left/Right move along it.
type FeedScreen struct {
	rows   []*feedRow
	resume *player.Resume
	row    int // focused row; the Resume card is row 0 when present
	top    int // first visible row
}

type feedRow struct {
	label, listType string
	albums          []subsonic.Album
	loaded          bool
	err             error
	focus, first    int // focused and first visible cover
	visible         int // covers that fit, at the last Draw
}

const feedPage = 20 // covers per row

func NewFeedScreen() *FeedScreen {
	s := &FeedScreen{}
	for _, it := range homeLists {
		s.rows = append(s.rows, &feedRow{label: it.label, listType: it.listType})
	}
	return s
}

func (s *FeedScreen) Title() string { return "MiSTer Subsonic" }

func (s *FeedScreen) Enter(a *App) {
	for _, r := range s.rows {
		s.load(a, r)
	}
	if a.Player() == nil {
		return
	}
	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
			s.resume = r
			s.row++ // keep the focus on the same row of covers
		}
	})
}

func (s *FeedScreen) load(a *App, r *feedRow) {
	r.err = nil
	a.Load(s, func(ctx context.Context) (any, error) {
		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: r.listType, Size: feedPage})
	}, func(v any, err error) {
		if err != nil {
			r.err = err
			return
		}
		r.albums, _ = v.([]subsonic.Album)
		r.loaded = true
	})
}

func (s *FeedScreen) count() int {
	if s.resume != nil {
		return len(s.rows) + 1
	}
	return len(s.rows)
}

// current is the focused row of covers, or nil on the Resume card.
func (s *FeedScreen) current() *feedRow {
	i := s.row
	if s.resume != nil {
		if i == 0 {
			return nil
		}
		i--
	}
	return s.rows[i]
}

func (s *FeedScreen) Handle(a *App, e input.Event) bool {
	r := s.current()
	switch e.Button {
	case input.BtnUp, input.BtnDown:
		if e.Kind == input.Release {
			return false
		}
		next := s.row - 1
		if e.Button == input.BtnDown {
			next = s.row + 1
		}
		if next < 0 || next >= s.count() {
			return false
		}
		s.row = next
		return true
	case input.BtnLeft, input.BtnRight:
		if e.Kind == input.Release || r == nil {
			return false
		}
		if e.Button == input.BtnLeft {
			if r.focus == 0 {
				return false
			}
			r.focus--
		} else if r.focus+1 < len(r.albums) {
			r.focus++
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	if r == nil { // the Resume card
		if e.Button == input.BtnA {
			a.Player().ResumeFrom(s.resume)
			s.resume, s.row = nil, 0
			a.Push(NewNowPlayingScreen())
			return true
		}
		return false
	}
	switch {
	case e.Button == input.BtnA && r.err != nil:
		s.load(a, r)
	case len(r.albums) == 0:
		return false
	case e.Button == input.BtnA:
		a.Push(NewAlbumScreen(r.albums[r.focus]))
	case e.Button == input.BtnX:
		al := r.albums[r.focus]
		a.openMenu(al.Name, albumMenu(a, al))
	case e.Button == input.BtnSelect:
		a.withSongs(albumsSongs(sampleAlbums(r.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
	default:
		return false
	}
	return true
}

func (s *FeedScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb := a.F.Body
	cellW, cellH := a.coverCell()
	labelH := fb.Height() + p.Margin/3
	rowH := labelH + cellH
	resumeH := labelH + p.Thumb + p.Margin
	// Scroll so the focused row is visible (rows are the same height, but
	// the Resume card is shorter; treating it as a full row is fine).
	visible := max(area.H/rowH, 1)
	if s.row < s.top {
		s.top = s.row
	}
	if s.row >= s.top+visible {
		s.top = s.row - visible + 1
	}
	y := area.Y + p.Margin/3
	for i := s.top; i < s.count() && y < area.Bottom(); i++ {
		focused := i == s.row
		if s.resume != nil && i == 0 {
			s.drawResume(a, c, gfx.R(area.X, y, area.W, resumeH), focused)
			y += resumeH
			continue
		}
		r := s.rows[i]
		if s.resume != nil {
			r = s.rows[i-1]
		}
		col := colDim
		if focused {
			col = colText
		}
		fb.Draw(c, area.X+p.Margin, y+fb.Ascent(), r.label, col, area)
		strip := gfx.R(area.X+p.Margin-cellW/10, y+labelH, area.W-p.Margin, cellH)
		s.drawStrip(a, c, r, strip, cellW, focused)
		y += rowH
	}
}

func (s *FeedScreen) drawResume(a *App, c *gfx.Canvas, r gfx.Rect, focused bool) {
	p := a.P
	fb := a.F.Body
	fb.Draw(c, r.X+p.Margin, r.Y+fb.Ascent(), "Resume", colDim, r)
	card := gfx.R(r.X+p.Margin/2, r.Y+fb.Height()+p.Margin/3, r.W-p.Margin, p.Thumb+p.Margin/2)
	if focused {
		c.Fill(card, colFocus)
	}
	song := s.resume.Songs[s.resume.Index]
	sub := song.Artist
	if s.resume.Position > 0 {
		sub = fmt.Sprintf("%s · from %s", song.Artist, clock(s.resume.Position))
	}
	a.drawRow(c, card, row{cover: song.CoverArt, thumb: true, main: song.Title, sub: sub, col: colAccent, focused: focused})
}

// drawStrip draws one row of covers, scrolled so its focus is visible.
func (s *FeedScreen) drawStrip(a *App, c *gfx.Canvas, r *feedRow, strip gfx.Rect, cellW int, focused bool) {
	switch {
	case r.err != nil:
		a.drawCentered(c, strip, "Couldn't load: "+subsonic.Classify(r.err).String()+" — A to retry", colError)
		return
	case !r.loaded:
		a.drawCentered(c, strip, "Loading…", colDim)
		return
	case len(r.albums) == 0:
		a.drawCentered(c, strip, "Nothing here yet", colDim)
		return
	}
	r.visible = max(strip.W/cellW, 1)
	if r.focus < r.first {
		r.first = r.focus
	}
	if r.focus >= r.first+r.visible {
		r.first = r.focus - r.visible + 1
	}
	for i := r.first; i < len(r.albums) && i < r.first+r.visible; i++ {
		al := r.albums[i]
		cell := gfx.R(strip.X+(i-r.first)*cellW, strip.Y, cellW, strip.H)
		a.drawCoverCell(c, cell, al.CoverArt, al.Name, al.Artist, focused && i == r.focus, a.isStarred(albumStar(al)))
	}
}

const sidebarDwell = 400 * time.Millisecond // a sidebar selection opens its section after resting this long
