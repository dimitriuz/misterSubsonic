package ui

import (
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

var update = flag.Bool("update", false, "rewrite golden screenshots in testdata/golden")

// golden compares c with testdata/golden/<name>.png (exact pixels).
func golden(t *testing.T, name string, c *gfx.Canvas) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".png")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := gfx.SavePNG(path, c); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test ./internal/ui -update): %v", path, err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if want.Bounds().Dx() != c.W || want.Bounds().Dy() != c.H {
		t.Fatalf("%s: size %v, want %dx%d", name, want.Bounds(), c.W, c.H)
	}
	diff := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			r, g, b, _ := want.At(x, y).RGBA()
			if gfx.RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8)) != c.At(x, y) {
				diff++
			}
		}
	}
	if diff > 0 {
		out := filepath.Join(t.TempDir(), name+"-got.png")
		gfx.SavePNG(out, c)
		t.Fatalf("%s: %d pixels differ from the golden; got image at %s (run with -update if the change is intended)", name, diff, out)
	}
}

var profiles = []Profile{ProfileHDMI, ProfileCRT240}

func TestGoldenHome(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
		ta.Push(NewHomeScreen())
		golden(t, "home-"+p.Name, ta.settle(t))
	}
}

func TestGoldenAlbumList(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
		golden(t, "albums-"+p.Name, ta.settle(t))
	}
}

func TestGoldenAlbum(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewAlbumScreen(ta.lib.albums[0]))
		golden(t, "album-"+p.Name, ta.settle(t))
	}
}

func playingState(ta *testApp) {
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: 1, Status: player.Playing, Position: 75 * time.Second}
}

func TestGoldenNowPlayingAndMiniBar(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.Push(NewHomeScreen())
		golden(t, "home-minibar-"+p.Name, ta.settle(t))
		ta.Push(NewNowPlayingScreen())
		golden(t, "nowplaying-"+p.Name, ta.settle(t))
	}
}

func TestGoldenQueueAndMessageAndToast(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.Push(NewHomeScreen())
		ta.Push(NewQueueScreen())
		ta.Toast("Removed Время Луны")
		golden(t, "queue-toast-"+p.Name, ta.settle(t))
		ta.Replace(NewMessageScreen("Can't reach the server", "http://192.168.1.10:4533 did not answer (server unreachable).\nCheck that Navidrome is running.", func() {}))
		ta.toasts = nil
		golden(t, "message-"+p.Name, ta.settle(t))
	}
}

func TestNavigationHomeToAlbumToPlay(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.settle(t)
	ta.press(input.BtnA) // "Recently added"
	if _, ok := ta.Top().(*AlbumListScreen); !ok {
		t.Fatalf("top = %T", ta.Top())
	}
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Homogenic
	as, ok := ta.Top().(*AlbumScreen)
	if !ok || as.stub.ID != "al-2" {
		t.Fatalf("top = %T %+v", ta.Top(), ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnUp)
	ta.press(input.BtnA) // Радио Африка
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // row 3 = track 2
	ta.press(input.BtnA)
	if ta.pl.start != 1 || len(ta.pl.played) != 3 {
		t.Fatalf("played start %d of %d", ta.pl.start, len(ta.pl.played))
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("after play, top = %T", ta.Top())
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*AlbumScreen); !ok {
		t.Fatalf("B from Now Playing should return to the album, got %T", ta.Top())
	}
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("Y should open Now Playing, got %T", ta.Top())
	}
}

func TestNowPlayingControls(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnA)
	ta.press(input.BtnR)
	ta.press(input.BtnL)
	ta.press(input.BtnRight)
	if ta.pl.seekPos != 85*time.Second {
		t.Fatalf("seek to %v, want 85s", ta.pl.seekPos)
	}
	want := []string{"toggle", "next", "prev", "seek"}
	if len(ta.pl.calls) != len(want) {
		t.Fatalf("calls %v, want %v", ta.pl.calls, want)
	}
	for i := range want {
		if ta.pl.calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", ta.pl.calls, want)
		}
	}
	ta.press(input.BtnSelect)
	if !ta.pl.st.Shuffle {
		t.Fatal("Select should switch to shuffle")
	}
	ta.press(input.BtnSelect)
	if ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatAll {
		t.Fatalf("second Select: shuffle %v repeat %v", ta.pl.st.Shuffle, ta.pl.st.Repeat)
	}
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Fatalf("Y in Now Playing should open the queue, got %T", ta.Top())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if ta.pl.st.Index != 1 {
		t.Fatalf("jump index %d", ta.pl.st.Index)
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatal("A in queue should jump and return to Now Playing")
	}
}

func TestStartTogglesPauseGlobally(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.press(input.BtnStart)
	if len(ta.pl.calls) != 1 || ta.pl.calls[0] != "toggle" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestResumeFromHome(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 2}
	ta.Push(NewHomeScreen())
	ta.settle(t)
	ta.press(input.BtnA)
	if len(ta.pl.calls) != 1 || ta.pl.calls[0] != "resume" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("top %T", ta.Top())
	}
}

func TestHoldBAtRootAsksToExit(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(exitHold)
	ta.onWake()
	if !ta.confirm {
		t.Fatal("holding B for 2 s at the root should ask to exit")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release})
	ta.press(input.BtnB) // B = stay
	if ta.confirm || ta.quit {
		t.Fatal("B should cancel the exit prompt")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(time.Second) // released early: no prompt
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release})
	ta.now = ta.now.Add(2 * time.Second)
	ta.onWake()
	if ta.confirm {
		t.Fatal("a short B press must not ask to exit")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(exitHold)
	ta.onWake()
	ta.press(input.BtnA)
	if !ta.quit {
		t.Fatal("A in the prompt should exit")
	}
}

func TestLoadErrorShowsRetry(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.err = errOffline
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	s := ta.Top().(*AlbumListScreen)
	if s.err == nil {
		t.Fatal("error not recorded")
	}
	ta.lib.err = nil
	ta.press(input.BtnA)
	ta.settle(t)
	if len(s.albums) != 3 {
		t.Fatalf("retry loaded %d albums", len(s.albums))
	}
}

func TestPoppedScreenIgnoresLateLoad(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	al := NewAlbumScreen(ta.lib.albums[0])
	ta.Push(al)
	ta.Pop() // before the load finishes
	ta.settle(t)
	if al.album != nil {
		t.Fatal("a popped screen received its load result")
	}
}

func TestAlbumListPagesOnScroll(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	for i := 0; i < 150; i++ {
		ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: subsonic.ID("x" + string(rune('a'+i%26))), Name: "Filler"})
	}
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	s := ta.Top().(*AlbumListScreen)
	if len(s.albums) != albumPage {
		t.Fatalf("first page %d", len(s.albums))
	}
	for i := 0; i < 20; i++ {
		ta.press(input.BtnR) // page down
	}
	ta.settle(t)
	if len(s.albums) != 153 {
		t.Fatalf("after scrolling, %d albums loaded, want 153", len(s.albums))
	}
	if q := ta.lib.calls[len(ta.lib.calls)-1]; q.Offset != albumPage {
		t.Fatalf("second page offset %d", q.Offset)
	}
}

func TestPickProfile(t *testing.T) {
	if p := PickProfile(1920, 1080, "auto"); p.Name != "hdmi" {
		t.Fatalf("1080p → %s", p.Name)
	}
	if p := PickProfile(640, 240, "auto"); p.Name != "crt" || p.H != 240 {
		t.Fatalf("240p → %s %d", p.Name, p.H)
	}
	if p := PickProfile(640, 288, "auto"); p.H != 288 {
		t.Fatalf("288p height %d", p.H)
	}
	if p := PickProfile(640, 480, "crt"); p.Name != "crt" {
		t.Fatalf("forced crt → %s", p.Name)
	}
	if p := PickProfile(640, 240, "hdmi"); p.Name != "hdmi" {
		t.Fatalf("forced hdmi → %s", p.Name)
	}
}

func TestFormatHelpers(t *testing.T) {
	if clock(3*time.Minute+5*time.Second) != "3:05" || clock(time.Hour+time.Minute) != "1:01:00" {
		t.Fatal("clock format")
	}
	s := subsonic.Song{Suffix: "flac", BitDepth: 24, SamplingRate: 96000}
	if q := quality(s, false); q != "FLAC 24/96" {
		t.Fatalf("quality %q", q)
	}
	if q := quality(subsonic.Song{Suffix: "flac", BitDepth: 16, SamplingRate: 44100}, false); q != "FLAC 16/44.1" {
		t.Fatalf("quality %q", q)
	}
	if q := quality(subsonic.Song{Suffix: "m4a", BitRate: 256}, true); q != "M4A 256 · transcoded" {
		t.Fatalf("quality %q", q)
	}
}

func TestEmptyLibraryAndEmptyAlbum(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.albums = nil
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnA) // nothing to open: must not panic
	ta.press(input.BtnDown)
	if _, ok := ta.Top().(*AlbumListScreen); !ok {
		t.Fatalf("top = %T", ta.Top())
	}
	ta.lib.albums = []subsonic.Album{{ID: "empty", Name: "Silence", Artist: "Nobody"}}
	ta.Replace(NewHomeScreen())
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	ta.settle(t)
	ta.press(input.BtnA)      // "Play" on an album without tracks
	ta.press(input.BtnSelect) // shuffle-play too
	if len(ta.pl.calls) != 0 {
		t.Fatalf("playing an empty album called %v", ta.pl.calls)
	}
}

// Before the server connects there is no player: keys must be harmless.
func TestKeysBeforePlayerAttached(t *testing.T) {
	disp := gfx.NewHeadless(ProfileHDMI.W, ProfileHDMI.H, "")
	a, err := New(Options{Display: disp, Profile: ProfileHDMI})
	if err != nil {
		t.Fatal(err)
	}
	a.Push(NewMessageScreen("Connecting…", "Connecting to the server…", nil))
	for _, b := range []input.Button{input.BtnStart, input.BtnY, input.BtnA, input.BtnB, input.BtnSelect} {
		a.onInput(input.Event{Button: b, Kind: input.Press})
		a.onInput(input.Event{Button: b, Kind: input.Release})
	}
	if err := a.render(); err != nil {
		t.Fatal(err)
	}
	a.Attach(sampleLibrary(), newFakePlayer(), fakeArt{})
	a.Replace(NewHomeScreen())
	if _, ok := a.Top().(*HomeScreen); !ok {
		t.Fatalf("top = %T", a.Top())
	}
}

func TestYTogglesWithoutGrowingStack(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	for i := 0; i < 5; i++ {
		ta.press(input.BtnY)
		if len(ta.stack) > 3 {
			t.Fatalf("stack depth %d after %d Y presses", len(ta.stack), i+1)
		}
		_, np := ta.Top().(*NowPlayingScreen)
		_, q := ta.Top().(*QueueScreen)
		if (i%2 == 0 && !np) || (i%2 == 1 && !q) {
			t.Fatalf("press %d: top = %T", i+1, ta.Top())
		}
	}
	ta.press(input.BtnB)
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*HomeScreen); !ok || len(ta.stack) != 1 {
		t.Fatalf("after two B: top %T depth %d", ta.Top(), len(ta.stack))
	}
}

func TestQueueRemoveAfterShrinkDoesNotPanic(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewQueueScreen())
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.pl.st.Queue = ta.pl.st.Queue[:1]
	ta.press(input.BtnX)
	ta.press(input.BtnA)
}

func TestBHoldClearedWhenScreenPushed(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.Push(NewNowPlayingScreen())
	ta.now = ta.now.Add(exitHold)
	ta.onWake()
	if ta.confirm {
		t.Fatal("exit prompt appeared on a non-root screen")
	}
}

func TestToastsAreCappedAndCoalesced(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	for i := 0; i < 5; i++ {
		ta.Toast("t%d", i)
	}
	if len(ta.toasts) != 3 || ta.toasts[0].text != "t2" {
		t.Fatalf("toasts %+v", ta.toasts)
	}
	ta.toasts = nil
	ta.Toast("same")
	ta.Toast("same")
	if len(ta.toasts) != 1 {
		t.Fatalf("toasts %+v", ta.toasts)
	}
}

func TestProgressW(t *testing.T) {
	cases := []struct {
		w      int
		pos, d time.Duration
		want   int
	}{
		{1000, 90 * time.Second, 180 * time.Second, 500},
		{1200, 30 * time.Minute, time.Hour, 600},
		{1200, 2 * time.Hour, time.Hour, 1200},
		{1200, -time.Second, time.Hour, 0},
		{1200, time.Second, 0, 0},
	}
	for _, c := range cases {
		if got := progressW(c.w, c.pos, c.d); got != c.want {
			t.Errorf("progressW(%d,%v,%v)=%d, want %d", c.w, c.pos, c.d, got, c.want)
		}
	}
}

// holdSeek presses btn, feeds repeats every 50 ms until hold has passed, then
// releases; the fake player never moves, like a seek that hasn't landed yet.
func holdSeek(ta *testApp, btn input.Button, hold time.Duration) {
	ta.onInput(input.Event{Button: btn, Kind: input.Press})
	for el := 50 * time.Millisecond; el <= hold; el += 50 * time.Millisecond {
		ta.now = ta.now.Add(50 * time.Millisecond)
		ta.onInput(input.Event{Button: btn, Kind: input.Repeat})
	}
	ta.onInput(input.Event{Button: btn, Kind: input.Release})
}

func seekCount(ta *testApp) int {
	n := 0
	for _, c := range ta.pl.calls {
		if c == "seek" {
			n++
		}
	}
	return n
}

func TestNowPlayingTapSeeksOnce(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnRight)
	if n := seekCount(ta); n != 1 || ta.pl.seekPos != 85*time.Second {
		t.Fatalf("tap: %d seeks, last %v; want 1 seek to 85s", n, ta.pl.seekPos)
	}
}

func TestNowPlayingHoldThrottlesSeeks(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.pl.st.Queue = []subsonic.Song{{ID: "long", Title: "Long", Duration: 3600}}
	ta.Push(NewNowPlayingScreen())
	holdSeek(ta, input.BtnRight, 950*time.Millisecond)
	if n := seekCount(ta); n > 5 {
		t.Fatalf("%d seeks while held, want <= 5", n)
	}
	// press +10s, 19 repeats of +30s.
	if want := 75*time.Second + 10*time.Second + 19*30*time.Second; ta.pl.seekPos != want {
		t.Fatalf("final seek %v, want %v (release must flush the pending target)", ta.pl.seekPos, want)
	}
}

func TestNowPlayingHoldClampsAtEnd(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta) // 240 s track
	ta.Push(NewNowPlayingScreen())
	holdSeek(ta, input.BtnRight, time.Second)
	if want := 239 * time.Second; ta.pl.seekPos != want {
		t.Fatalf("seek %v, want %v", ta.pl.seekPos, want)
	}
	ta.render() // pending target shown without panicking
	holdSeek(ta, input.BtnLeft, time.Second)
	if ta.pl.seekPos != 0 {
		t.Fatalf("seek %v, want 0", ta.pl.seekPos)
	}
}

func TestNowPlayingPendingSeekDoesNotOutliveDetour(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	np := NewNowPlayingScreen()
	ta.Push(np)
	// Hold Right; a seek is sent, then Y opens the queue while still held.
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
	ta.now = ta.now.Add(50 * time.Millisecond)
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Repeat}) // unsent
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Press})
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Fatalf("Y should open the queue, got %T", ta.Top())
	}
	if ta.pl.seekPos != 115*time.Second { // 75+10+30 flushed by the Y press
		t.Fatalf("unsent target lost on Y: last seek %v", ta.pl.seekPos)
	}
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Release}) // goes to the queue
	ta.press(input.BtnB)
	if ta.Top() != Screen(np) {
		t.Fatalf("B should return to Now Playing, got %T", ta.Top())
	}
	ta.now = ta.now.Add(30 * time.Second)
	ta.pl.st.Position = 120 * time.Second
	if got := np.position(ta.App, ta.pl.st, ta.now); got != 120*time.Second {
		t.Fatalf("position %v, want the player's 2m0s", got)
	}
	ta.press(input.BtnRight)
	if ta.pl.seekPos != 130*time.Second {
		t.Fatalf("next tap sought %v, want 2m10s (player position + 10s)", ta.pl.seekPos)
	}
}

func TestNowPlayingNextWhileSeekHeldDropsStaleRelease(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
	ta.now = ta.now.Add(50 * time.Millisecond)
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Repeat}) // unsent
	ta.onInput(input.Event{Button: input.BtnR, Kind: input.Press})
	ta.pl.st.Index = 1 // the player moved to the next track
	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Release})
	last := ta.pl.calls[len(ta.pl.calls)-1]
	if last != "next" {
		t.Fatalf("calls %v: nothing may be sent after Next", ta.pl.calls)
	}
}
