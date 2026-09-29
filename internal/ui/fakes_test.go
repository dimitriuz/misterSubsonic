package ui

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type fakeLibrary struct {
	mu     sync.Mutex
	albums []subsonic.Album
	tracks map[subsonic.ID][]subsonic.Song
	err    error
	calls  []subsonic.AlbumListQuery
}

func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, q)
	if l.err != nil {
		return nil, l.err
	}
	end := min(q.Offset+q.Size, len(l.albums))
	if q.Offset >= end {
		return nil, nil
	}
	return append([]subsonic.Album(nil), l.albums[q.Offset:end]...), nil
}

func (l *fakeLibrary) GetAlbum(_ context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	for _, al := range l.albums {
		if al.ID == id {
			return &subsonic.AlbumWithSongs{Album: al, Songs: l.tracks[id]}, nil
		}
	}
	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no album"}
}

type fakePlayer struct {
	st      player.State
	events  chan player.Event
	resume  *player.Resume
	calls   []string
	played  []subsonic.Song
	start   int
	seekPos time.Duration
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{st: player.State{Index: -1, NextIndex: -1}, events: make(chan player.Event, 8)}
}

func (p *fakePlayer) State() player.State         { return p.st }
func (p *fakePlayer) Events() <-chan player.Event { return p.events }
func (p *fakePlayer) call(s string)               { p.calls = append(p.calls, s) }
func (p *fakePlayer) TogglePause()                { p.call("toggle") }
func (p *fakePlayer) Next()                       { p.call("next") }
func (p *fakePlayer) Prev()                       { p.call("prev") }
func (p *fakePlayer) Seek(d time.Duration)        { p.call("seek"); p.seekPos = d }
func (p *fakePlayer) Jump(i int)                  { p.call("jump"); p.st.Index = i }
func (p *fakePlayer) Remove(i int)                { p.call("remove") }
func (p *fakePlayer) SetShuffle(on bool)          { p.st.Shuffle = on }
func (p *fakePlayer) SetRepeat(r player.Repeat)   { p.st.Repeat = r }
func (p *fakePlayer) ResumeFrom(r *player.Resume) {
	p.call("resume")
	p.st.Queue, p.st.Index = r.Songs, r.Index
}
func (p *fakePlayer) Resumable(context.Context) (*player.Resume, error) {
	return p.resume, nil
}
func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
	p.call("playnow")
	p.played, p.start = songs, start
	p.st.Queue, p.st.Index, p.st.Status = songs, start, player.Playing
}

// fakeArt makes a deterministic two-tone square per cover id.
type fakeArt struct{}

func (fakeArt) Get(k art.Key) (*gfx.Image, bool) {
	if k.ID == "" {
		return nil, false
	}
	h := fnv.New32a()
	h.Write([]byte(k.ID))
	v := h.Sum32()
	img := gfx.NewImage(k.Size, k.Size)
	for y := 0; y < k.Size; y++ {
		for x := 0; x < k.Size; x++ {
			c := 0xFF000000 | v&0xFFFFFF
			if (x/(k.Size/4+1)+y/(k.Size/4+1))%2 == 0 {
				c = 0xFF000000 | ^v&0xFFFFFF
			}
			img.Pix[y*k.Size+x] = c
		}
	}
	return img, true
}

func sampleLibrary() *fakeLibrary {
	l := &fakeLibrary{tracks: map[subsonic.ID][]subsonic.Song{}}
	l.albums = []subsonic.Album{
		{ID: "al-1", Name: "Радио Африка", Artist: "Аквариум", CoverArt: "al-1", Year: 1983},
		{ID: "al-2", Name: "Homogenic", Artist: "Björk", CoverArt: "al-2", Year: 1997},
		{ID: "al-3", Name: "A Rather Long Album Title That Will Need Truncating Somewhere", Artist: "Some Artist", CoverArt: "al-3", Year: 2001},
	}
	l.tracks["al-1"] = []subsonic.Song{
		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
		{ID: "s2", Title: "Время Луны", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 2, Duration: 160, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
		{ID: "s3", Title: "Рок-н-ролл мёртв", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 3, Duration: 215, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
	}
	return l
}

type testApp struct {
	*App
	disp *gfx.Headless
	lib  *fakeLibrary
	pl   *fakePlayer
	now  time.Time
}

func newTestApp(t *testing.T, prof Profile) *testApp {
	t.Helper()
	ta := &testApp{disp: gfx.NewHeadless(prof.W, prof.H, ""), lib: sampleLibrary(), pl: newFakePlayer(), now: time.Unix(1_800_000_000, 0)}
	a, err := New(Options{Display: ta.disp, Profile: prof, Library: ta.lib, Player: ta.pl, Art: fakeArt{},
		Now: func() time.Time { return ta.now }})
	if err != nil {
		t.Fatal(err)
	}
	ta.App = a
	return ta
}

// settle runs posted work until no loads are pending, then renders.
func (ta *testApp) settle(t *testing.T) *gfx.Canvas {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for ta.loads > 0 || len(ta.post) > 0 {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("loads did not finish")
		}
	}
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	return ta.disp.Last()
}

func (ta *testApp) press(b input.Button) {
	ta.onInput(input.Event{Button: b, Kind: input.Press})
	ta.onInput(input.Event{Button: b, Kind: input.Release})
}

var errOffline = errors.New("offline")
