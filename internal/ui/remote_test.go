package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/subsonic"
)

// fakeRemote records what the app tells the server.
type fakeRemote struct {
	mu      sync.Mutex
	changes []remote.Change
}

func (f *fakeRemote) Notify(c remote.Change) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *fakeRemote) got() []remote.Change {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remote.Change(nil), f.changes...)
}

func (f *fakeRemote) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = nil
}

// remoteApp is a test app whose remote controller can be driven: serve runs a
// controller call on its own goroutine while this one plays the UI loop.
func remoteApp(t *testing.T) (*testApp, remote.Controller, *fakeRemote) {
	t.Helper()
	ta := newTestApp(t, ProfileHDMI)
	ta.cfg = config.Default()
	fr := &fakeRemote{}
	ta.o.Remote = fr
	ta.Push(NewHomeScreen()) // the screen under the requests that run off the UI goroutine
	playingState(ta)
	return ta, ta.RemoteController(), fr
}

func (ta *testApp) serve(t *testing.T, f func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f() }()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case fn := <-ta.post:
			fn()
		case err := <-done:
			return err
		case <-deadline:
			t.Fatal("the controller call never returned")
		}
	}
}

func (ta *testApp) do(t *testing.T, ctl remote.Controller, c remote.Command) error {
	t.Helper()
	return ta.serve(t, func() error { return ctl.Do(c) })
}

func TestRemoteTransport(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	for _, c := range []remote.Command{{Do: "toggle"}, {Do: "next"}, {Do: "prev"}, {Do: "seek", PositionMS: 90_000}} {
		if err := ta.do(t, ctl, c); err != nil {
			t.Fatalf("%v: %v", c.Do, err)
		}
	}
	if want := []string{"toggle", "next", "prev", "seek"}; !slices.Equal(ta.pl.calls, want) {
		t.Fatalf("player calls %v, want %v", ta.pl.calls, want)
	}
	if ta.pl.seekPos != 90*time.Second {
		t.Fatalf("seek to %v", ta.pl.seekPos)
	}
}

func TestRemoteNextPrevNeedAQueueAndSeekASong(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.pl.st = player.State{Index: -1, NextIndex: -1}
	for _, c := range []remote.Command{{Do: "next"}, {Do: "prev"}, {Do: "seek", PositionMS: 5000}} {
		if err := ta.do(t, ctl, c); err != nil {
			t.Fatalf("%v: %v", c.Do, err)
		}
	}
	if len(ta.pl.calls) != 0 {
		t.Fatalf("nothing is playing, yet the player got %v", ta.pl.calls)
	}
}

func TestRemoteSeekStaysInsideTheSong(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	if err := ta.do(t, ctl, remote.Command{Do: "seek", PositionMS: 10_000_000}); err != nil {
		t.Fatal(err)
	}
	// s1 lasts 240 s: like the media keys, a second short of the end.
	if ta.pl.seekPos != 239*time.Second {
		t.Fatalf("seek to %v", ta.pl.seekPos)
	}
}

// Volume does what Up and Down do: the panel shows on the TV, and the level
// is saved after a pause.
func TestRemoteVolumeShowsThePanelAndSaves(t *testing.T) {
	ta, ctl, fr := remoteApp(t)
	if err := ta.do(t, ctl, remote.Command{Do: "volume", DB: -20}); err != nil {
		t.Fatal(err)
	}
	if ta.pl.st.VolumeDB != -20 {
		t.Fatalf("player volume %v", ta.pl.st.VolumeDB)
	}
	if ta.volumeUntil.IsZero() {
		t.Fatal("the volume panel is not showing")
	}
	if ta.cfg.Playback.VolumeDB != -20 || ta.saveAt.IsZero() {
		t.Fatalf("config volume %v, save pending %v", ta.cfg.Playback.VolumeDB, !ta.saveAt.IsZero())
	}
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("the remote was not told: %v", fr.got())
	}
}

func TestRemoteVolumeUnmutes(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.setMuted(true)
	if err := ta.do(t, ctl, remote.Command{Do: "volume", DB: -25}); err != nil {
		t.Fatal(err)
	}
	if ta.muted || ta.pl.st.Muted {
		t.Fatal("changing the volume must bring the sound back, as the buttons do")
	}
}

func TestRemoteMute(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	if err := ta.do(t, ctl, remote.Command{Do: "mute", On: true}); err != nil {
		t.Fatal(err)
	}
	if !ta.muted || !ta.pl.st.Muted || ta.volumeUntil.IsZero() {
		t.Fatalf("muted %v, player muted %v, panel %v", ta.muted, ta.pl.st.Muted, !ta.volumeUntil.IsZero())
	}
	if err := ta.do(t, ctl, remote.Command{Do: "mute", On: false}); err != nil {
		t.Fatal(err)
	}
	if ta.muted || ta.pl.st.Muted {
		t.Fatal("still muted")
	}
}

func TestRemoteShuffleAndRepeat(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	if err := ta.do(t, ctl, remote.Command{Do: "shuffle", On: true}); err != nil {
		t.Fatal(err)
	}
	if !ta.pl.st.Shuffle {
		t.Fatal("shuffle is off")
	}
	for mode, want := range map[string]player.Repeat{"all": player.RepeatAll, "one": player.RepeatOne, "off": player.RepeatOff} {
		if err := ta.do(t, ctl, remote.Command{Do: "repeat", Mode: mode}); err != nil {
			t.Fatal(err)
		}
		if ta.pl.st.Repeat != want {
			t.Fatalf("repeat %s gave %v", mode, ta.pl.st.Repeat)
		}
	}
}

func TestRemoteStarUpdatesTheCache(t *testing.T) {
	ta, ctl, fr := remoteApp(t)
	if ctl.State().Song.Starred {
		t.Fatal("s1 starts unstarred")
	}
	if err := ta.do(t, ctl, remote.Command{Do: "star", On: true}); err != nil {
		t.Fatal(err)
	}
	ta.settle(t) // the request runs off the UI goroutine
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) {
		t.Fatalf("server calls %v", ta.lib.stars)
	}
	if !ta.isStarred(starItem{kind: starSong, id: "s1"}) {
		t.Fatal("the star cache missed it")
	}
	if !ctl.State().Song.Starred || !ctl.Queue().Songs[0].Starred {
		t.Fatal("the state does not show the star")
	}
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("the remote was not told: %v", fr.got())
	}
	// Starring again changes nothing; off unstars.
	if err := ta.do(t, ctl, remote.Command{Do: "star", On: true}); err != nil {
		t.Fatal(err)
	}
	ta.settle(t)
	if len(ta.lib.stars) != 1 {
		t.Fatalf("a second star asked the server again: %v", ta.lib.stars)
	}
	if err := ta.do(t, ctl, remote.Command{Do: "star", On: false}); err != nil {
		t.Fatal(err)
	}
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) || ctl.State().Song.Starred {
		t.Fatalf("server calls %v, starred %v", ta.lib.stars, ctl.State().Song.Starred)
	}
}

func TestRemoteStarShowsTheServersStar(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.pl.st.Queue = []subsonic.Song{{ID: "x", Title: "X", Starred: "2026-01-01T00:00:00Z"}}
	ta.pl.st.Index = 0
	if !ctl.State().Song.Starred {
		t.Fatal("a song the server lists as starred reads unstarred")
	}
}

func TestRemoteQueueCommands(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	steps := []remote.Command{
		{Do: "jump", Index: 2, SongID: "s3"},
		{Do: "remove", Index: 1, SongID: "s2"},
		{Do: "move", From: 0, To: 2, SongID: "s1"},
		{Do: "remove", Index: 0}, // no song id: no staleness check
		{Do: "clear"},
	}
	for _, c := range steps {
		if err := ta.do(t, ctl, c); err != nil {
			t.Fatalf("%v: %v", c.Do, err)
		}
	}
	if want := []string{"jump", "remove", "move", "remove", "clear"}; !slices.Equal(ta.pl.calls, want) {
		t.Fatalf("player calls %v, want %v", ta.pl.calls, want)
	}
	if !slices.Equal(ta.pl.removed, []int{1, 0}) || !slices.Equal(ta.pl.moves, [][2]int{{0, 2}}) {
		t.Fatalf("removed %v moves %v", ta.pl.removed, ta.pl.moves)
	}
}

func TestRemoteStale(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	for _, c := range []remote.Command{
		{Do: "jump", Index: 1, SongID: "s3"},
		{Do: "remove", Index: 0, SongID: "s2"},
		{Do: "move", From: 2, To: 0, SongID: "s1"},
		{Do: "jump", Index: 3, SongID: "s3"},
		{Do: "remove", Index: 7},
		{Do: "move", From: 0, To: 3, SongID: "s1"},
		{Do: "move", From: 5, To: 0},
	} {
		if err := ta.do(t, ctl, c); !errors.Is(err, remote.ErrStale) {
			t.Errorf("%+v: got %v, want ErrStale", c, err)
		}
	}
	if len(ta.pl.calls) != 0 {
		t.Fatalf("a stale command reached the player: %v", ta.pl.calls)
	}
}

func TestRemoteBusy(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	defer func(d time.Duration) { remoteTimeout = d }(remoteTimeout)
	remoteTimeout = 30 * time.Millisecond
	// Nobody serves the UI's work: the call gives up.
	if err := ctl.Do(remote.Command{Do: "toggle"}); !errors.Is(err, remote.ErrBusy) {
		t.Fatalf("Do: got %v, want ErrBusy", err)
	}
	if _, err := ctl.Play(remote.PlayRequest{What: "album", ID: "al-1", How: "now"}); !errors.Is(err, remote.ErrBusy) {
		t.Fatalf("Play: got %v, want ErrBusy", err)
	}
	// The work that was left behind must not run later: the caller was told no.
	for len(ta.post) > 0 {
		(<-ta.post)()
	}
	if len(ta.pl.calls) != 0 {
		t.Fatalf("an abandoned command ran: %v", ta.pl.calls)
	}
}

func TestRemoteUnknownCommand(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	if err := ta.do(t, ctl, remote.Command{Do: "explode"}); err == nil {
		t.Fatal("an unknown command succeeded")
	}
}

func TestRemotePlay(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	play := func(p remote.PlayRequest) int {
		t.Helper()
		var n int
		err := ta.serve(t, func() (err error) { n, err = ctl.Play(p); return })
		if err != nil {
			t.Fatalf("%+v: %v", p, err)
		}
		return n
	}
	ids := func(songs []subsonic.Song) (out []subsonic.ID) {
		for _, s := range songs {
			out = append(out, s.ID)
		}
		return
	}
	if n := play(remote.PlayRequest{What: "album", ID: "al-1", How: "now", Start: 1}); n != 3 {
		t.Fatalf("album added %d", n)
	}
	if !slices.Equal(ids(ta.pl.played), []subsonic.ID{"s1", "s2", "s3"}) || ta.pl.start != 1 || ta.pl.calls[len(ta.pl.calls)-1] != "playnow" {
		t.Fatalf("album: %v start %d calls %v", ids(ta.pl.played), ta.pl.start, ta.pl.calls)
	}
	if n := play(remote.PlayRequest{What: "playlist", ID: "pl-1", How: "next"}); n != 2 {
		t.Fatalf("playlist added %d", n)
	}
	if !slices.Equal(ids(ta.pl.played), []subsonic.ID{"s3", "s2"}) || ta.pl.calls[len(ta.pl.calls)-1] != "playnext" {
		t.Fatalf("playlist: %v calls %v", ids(ta.pl.played), ta.pl.calls)
	}
	if n := play(remote.PlayRequest{What: "artist", ID: "ar-1", How: "end"}); n != 3 {
		t.Fatalf("artist added %d", n)
	}
	if ta.pl.calls[len(ta.pl.calls)-1] != "enqueue" || len(ta.pl.played) != 3 {
		t.Fatalf("artist: %v calls %v", ids(ta.pl.played), ta.pl.calls)
	}
}

// A big artist plays up to the cap the server puts on a play request, in
// album order, however the albums were fetched.
func TestRemotePlayBigArtistIsCapped(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.lib.albums, ta.lib.tracks = nil, map[subsonic.ID][]subsonic.Song{}
	for i := range 30 {
		id := subsonic.ID(fmt.Sprintf("big-%02d", i))
		ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: id, ArtistID: "ar-1"})
		for j := range 50 {
			ta.lib.tracks[id] = append(ta.lib.tracks[id], subsonic.Song{ID: subsonic.ID(fmt.Sprintf("%s-%02d", id, j)), Title: "t"})
		}
	}
	var n int
	err := ta.serve(t, func() (err error) {
		n, err = ctl.Play(remote.PlayRequest{What: "artist", ID: "ar-1", How: "end"})
		return
	})
	if err != nil || n != remote.MaxPlay || len(ta.pl.played) != remote.MaxPlay {
		t.Fatalf("n %d, queued %d, err %v; want %d", n, len(ta.pl.played), err, remote.MaxPlay)
	}
	if first, last := ta.pl.played[0].ID, ta.pl.played[len(ta.pl.played)-1].ID; first != "big-00-00" || last != "big-19-49" {
		t.Fatalf("first %s last %s", first, last)
	}
}

// A connect lands pages that are already open on the new server's queue and state.
func TestConnectedNotifiesTheRemote(t *testing.T) {
	ta, _, fr := remoteApp(t)
	fr.reset()
	ta.Connected(ConnInfo{Info: &subsonic.ServerInfo{}}, ta.lib, ta.pl, newFakeArt())
	got := fr.got()
	if !slices.Contains(got, remote.QueueChanged) || !slices.Contains(got, remote.StateChanged) {
		t.Fatalf("told %v", got)
	}
}

// Songs by id come from what the page browsed and the queue, and anything
// else from the server (getSong): the page may show results from before an
// app restart. An id the server doesn't know is a not-found error.
func TestRemotePlaySongs(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.pl.st = player.State{Index: -1, NextIndex: -1}
	play := func(how string, start int, ids ...string) (int, error) {
		var n int
		err := ta.serve(t, func() (err error) {
			n, err = ctl.Play(remote.PlayRequest{What: "songs", IDs: ids, How: how, Start: start})
			return
		})
		return n, err
	}
	// Never seen: fetched from the server.
	n, err := play("end", 0, "s2")
	if err != nil || n != 1 || ta.pl.played[0].ID != "s2" || ta.pl.played[0].Title != "Время Луны" {
		t.Fatalf("added %d, err %v, played %v", n, err, ta.pl.played)
	}
	if !slices.Equal(ta.lib.songCalls, []subsonic.ID{"s2"}) {
		t.Fatalf("getSong calls %v", ta.lib.songCalls)
	}
	// ...and remembered: the second time asks nobody.
	if _, err := play("end", 0, "s2"); err != nil || len(ta.lib.songCalls) != 1 {
		t.Fatalf("err %v, getSong calls %v", err, ta.lib.songCalls)
	}
	// What the page browsed is remembered the same way.
	lib := ctl.Library()
	if _, err := lib.GetAlbum(context.Background(), "al-1"); err != nil {
		t.Fatal(err)
	}
	n, err = play("now", 1, "s3", "s1")
	if err != nil || n != 2 || ta.pl.played[0].ID != "s3" || ta.pl.played[1].ID != "s1" || ta.pl.start != 1 {
		t.Fatalf("added %d, err %v, played %v from %d", n, err, ta.pl.played, ta.pl.start)
	}
	if len(ta.lib.songCalls) != 1 {
		t.Fatalf("browsed songs went to the server: %v", ta.lib.songCalls)
	}
	// The queue counts too.
	ta.pl.st.Queue = []subsonic.Song{{ID: "q1", Title: "Queued"}}
	if n, err = play("end", 0, "q1"); err != nil || n != 1 || len(ta.lib.songCalls) != 1 {
		t.Fatalf("queued song: added %d, err %v, calls %v", n, err, ta.lib.songCalls)
	}
	// An id the server doesn't know: not found, and nothing is played.
	ta.pl.calls = nil
	if _, err := play("now", 0, "s1", "zzz"); subsonic.Classify(err) != subsonic.KindNotFound {
		t.Fatalf("unknown id: %v", err)
	}
	if len(ta.pl.calls) != 0 {
		t.Fatalf("a partly unknown list reached the player: %v", ta.pl.calls)
	}
	// A server that fails is its own error, not "unknown".
	ta.lib.err = errOffline
	if _, err := play("end", 0, "s9"); !errors.Is(err, errOffline) {
		t.Fatalf("err %v", err)
	}
}

func TestRemotePlayStartIsClamped(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	err := ta.serve(t, func() error {
		_, err := ctl.Play(remote.PlayRequest{What: "album", ID: "al-1", How: "now", Start: 99})
		return err
	})
	if err != nil || ta.pl.start != 0 {
		t.Fatalf("start %d err %v", ta.pl.start, err)
	}
}

func TestRemotePlayErrors(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	err := ta.serve(t, func() error {
		_, err := ctl.Play(remote.PlayRequest{What: "album", ID: "nope", How: "now"})
		return err
	})
	if err == nil {
		t.Fatal("a missing album played")
	}
	if len(ta.pl.calls) != 0 {
		t.Fatalf("the player got %v", ta.pl.calls)
	}
}

func TestRemoteStateMapping(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 1, NextIndex: 2, Status: player.Buffering,
		Position: 12 * time.Second, Shuffle: true, Repeat: player.RepeatOne, VolumeDB: -18, Muted: true}
	st := ctl.State()
	if st.Song == nil || st.Song.ID != "s2" || st.Song.Title != "Время Луны" || st.Song.DurationMS != 160_000 || st.Song.CoverID != "al-1" {
		t.Fatalf("song %+v", st.Song)
	}
	if st.Status != "buffering" || st.PositionMS != 12_000 || st.DurationMS != 160_000 || st.VolumeDB != -18 ||
		!st.Muted || !st.Shuffle || st.Repeat != "one" || st.Index != 1 {
		t.Fatalf("state %+v", st)
	}
	q := ctl.Queue()
	if q.Index != 1 || len(q.Songs) != 3 || q.Songs[2].ID != "s3" {
		t.Fatalf("queue %+v", q)
	}
	ta.pl.st = player.State{Index: -1, NextIndex: -1}
	if st := ctl.State(); st.Song != nil || st.Status != "stopped" || st.Repeat != "off" || st.Index != -1 {
		t.Fatalf("empty state %+v", st)
	}
	if q := ctl.Queue(); q.Songs == nil || len(q.Songs) != 0 {
		t.Fatalf("empty queue %+v", q)
	}
}

func TestRemoteWithoutAConnection(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.Detach()
	if st := ctl.State(); st.Song != nil || st.Status != "stopped" {
		t.Fatalf("state %+v", st)
	}
	if ctl.Library() != nil {
		t.Fatal("a library without a connection")
	}
	if _, _, err := ctl.Cover(context.Background(), "al-1", 300); err == nil {
		t.Fatal("a cover without a connection")
	}
	if err := ta.do(t, ctl, remote.Command{Do: "toggle"}); err != nil {
		t.Fatal(err) // nothing to control is not an error
	}
	ta.Attach(ta.lib, ta.pl, nil)
	if ctl.Library() == nil || ctl.State().Song == nil {
		t.Fatal("a new connection is not seen")
	}
}

type bytesArt struct {
	fakeArt
	data []byte
	err  error
	got  art.Key
}

func (b *bytesArt) Bytes(_ context.Context, k art.Key) ([]byte, error) {
	b.got = k
	return b.data, b.err
}

func TestRemoteCover(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	png := []byte("\x89PNG\r\n\x1a\n rest of the image")
	ba := &bytesArt{fakeArt: newFakeArt(), data: png}
	ta.Attach(ta.lib, ta.pl, ba)
	b, ct, err := ctl.Cover(context.Background(), "al-1", 300)
	if err != nil || string(b) != string(png) || ct != "image/png" {
		t.Fatalf("got %q %q %v", b, ct, err)
	}
	if ba.got != (art.Key{ID: "al-1", Size: 300}) {
		t.Fatalf("asked for %+v", ba.got)
	}
	ba.err = errors.New("art: al-1@300: HTTP 404 text/html")
	if _, _, err := ctl.Cover(context.Background(), "al-1", 300); err == nil {
		t.Fatal("a failed fetch is no error")
	}
	// An art source that cannot return bytes (the test fake) has no cover.
	ta.Attach(ta.lib, ta.pl, newFakeArt())
	if _, _, err := ctl.Cover(context.Background(), "al-1", 300); err == nil {
		t.Fatal("a cover from an art source without bytes")
	}
}

func TestRemoteNotifiedOfPlayerEvents(t *testing.T) {
	ta, _, fr := remoteApp(t)
	for _, c := range []struct {
		ev   player.Event
		want []remote.Change
	}{
		{player.Event{Kind: player.TrackChanged}, []remote.Change{remote.StateChanged}},
		{player.Event{Kind: player.StatusChanged}, []remote.Change{remote.StateChanged}},
		{player.Event{Kind: player.QueueChanged}, []remote.Change{remote.QueueChanged, remote.StateChanged}},
		{player.Event{Kind: player.Error, Err: errors.New("boom")}, []remote.Change{remote.StateChanged}},
	} {
		fr.reset()
		ta.onPlayer(c.ev)
		if got := fr.got(); !slices.Equal(got, c.want) {
			t.Errorf("event %v: told %v, want %v", c.ev.Kind, got, c.want)
		}
	}
}

func TestRemoteNotifiedOfTheButtons(t *testing.T) {
	ta, _, fr := remoteApp(t)
	ta.setVolume(-10)
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("volume: told %v", fr.got())
	}
	fr.reset()
	ta.toggleMute()
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("mute: told %v", fr.got())
	}
}

// A seek made on the TV (the Now Playing screen's left and right, or the
// keyboard's media keys) moves the position the pages show.
func TestRemoteNotifiedOfASeekOnTheTV(t *testing.T) {
	ta, _, fr := remoteApp(t)
	ta.Push(NewNowPlayingScreen())
	fr.reset()
	ta.press(input.BtnRight)
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("Now Playing seek: told %v", fr.got())
	}
	fr.reset()
	ta.pl.st.Position = time.Minute
	ta.press(input.BtnSeekFwd)
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("media-key seek: told %v", fr.got())
	}
}

func TestNoRemoteNoNotify(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.onPlayer(player.Event{Kind: player.QueueChanged}) // must not crash without a server
	ta.setVolume(-10)
}

// fakeSwitch stands in for the code that starts and stops the server.
type fakeSwitch struct {
	calls []bool
	err   error
	urls  []string
	// running is whether the server is up; a start that fails leaves it as it was.
	running bool
}

func (s *fakeSwitch) SetEnabled(on bool) error {
	s.calls = append(s.calls, on)
	if s.err == nil {
		s.running = on
	}
	return s.err
}
func (s *fakeSwitch) URLs() []string { return s.urls }
func (s *fakeSwitch) Running() bool  { return s.running }

func openRemoteSettings(t *testing.T, sw *fakeSwitch) (*testApp, *SettingsListScreen) {
	t.Helper()
	ta, _ := connectedApp(t)
	ta.o.RemoteSwitch = sw
	ta.Push(NewSettingsScreen())
	for range 3 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA)
	s, ok := ta.Top().(*SettingsListScreen)
	if !ok || s.Title() != "Remote" {
		t.Fatalf("the fourth Settings item opened %T", ta.Top())
	}
	return ta, s
}

func TestSettingsListsRemoteAfterDisplay(t *testing.T) {
	if want := []string{"Servers", "Playback", "Display", "Remote", "About"}; !slices.Equal(settingsItems, want) {
		t.Fatalf("settings items %v", settingsItems)
	}
}

func TestSettingsRemoteOnOffStartsStopsAndSaves(t *testing.T) {
	sw := &fakeSwitch{urls: []string{"http://192.168.1.50:8080/"}}
	ta, s := openRemoteSettings(t, sw)
	rows := s.rows(ta.App)
	if rows[0].label != "Remote" || rows[0].value(ta.App) != "Off" {
		t.Fatalf("first row %q = %q", rows[0].label, rows[0].value(ta.App))
	}
	if got := rows[1].value(ta.App); got != "Off" {
		t.Fatalf("the address row of a stopped remote reads %q", got)
	}
	ta.press(input.BtnA) // on
	if !slices.Equal(sw.calls, []bool{true}) || !ta.cfg.Remote.Enabled {
		t.Fatalf("switch calls %v, enabled %v", sw.calls, ta.cfg.Remote.Enabled)
	}
	ta.flushConfig()
	waitSaves(ta)
	waitFile(t, ta.o.ConfigPath, "enabled = true")
	rows = s.rows(ta.App)
	if rows[0].value(ta.App) != "On" || rows[1].value(ta.App) != "http://192.168.1.50:8080" {
		t.Fatalf("rows now %q / %q", rows[0].value(ta.App), rows[1].value(ta.App))
	}
	ta.press(input.BtnA) // off
	if !slices.Equal(sw.calls, []bool{true, false}) || ta.cfg.Remote.Enabled {
		t.Fatalf("switch calls %v, enabled %v", sw.calls, ta.cfg.Remote.Enabled)
	}
}

func TestSettingsRemoteNoNetwork(t *testing.T) {
	sw := &fakeSwitch{}
	ta, s := openRemoteSettings(t, sw)
	ta.press(input.BtnA)
	if got := s.rows(ta.App)[1].value(ta.App); got != "no network" {
		t.Fatalf("address row %q", got)
	}
}

func TestSettingsRemoteStartFailureIsAToast(t *testing.T) {
	sw := &fakeSwitch{err: errors.New("port 8080 is in use")}
	ta, s := openRemoteSettings(t, sw)
	ta.press(input.BtnA)
	if ta.cfg.Remote.Enabled {
		t.Fatal("enabled saved although the server did not start")
	}
	if got := ta.toasts[len(ta.toasts)-1].text; got != "Remote: port 8080 is in use" {
		t.Fatalf("toast %q", got)
	}
	if s.rows(ta.App)[0].value(ta.App) != "Off" {
		t.Fatal("the row says On")
	}
}

func TestSettingsRemoteHelpNamesTheRisk(t *testing.T) {
	ta, s := openRemoteSettings(t, &fakeSwitch{})
	help := s.helpText(ta.App)
	for _, w := range []string{"anyone", "http"} {
		if !strings.Contains(strings.ToLower(help), w) {
			t.Errorf("help %q lacks %q", help, w)
		}
	}
}

func TestSettingsRemoteWithoutASwitch(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Remote", remoteSettings))
	ta.press(input.BtnA)
	if ta.cfg.Remote.Enabled || len(ta.toasts) == 0 {
		t.Fatalf("turned on with nothing to start (toasts %v)", ta.toasts)
	}
}

// A server that failed to start at launch leaves the config on but nothing
// listening: the row says Off, and the first press tries to start again.
func TestSettingsRemoteShowsTheRealState(t *testing.T) {
	sw := &fakeSwitch{err: errors.New("port 8080 is in use")}
	ta, s := openRemoteSettings(t, sw)
	ta.cfg.Remote.Enabled = true // as the user set it
	if got := s.rows(ta.App)[0].value(ta.App); got != "Off" {
		t.Fatalf("row reads %q although nothing listens", got)
	}
	if got := s.rows(ta.App)[1].value(ta.App); got != "Off" {
		t.Fatalf("address reads %q although nothing listens", got)
	}
	sw.err = nil
	ta.press(input.BtnA)
	if !slices.Equal(sw.calls, []bool{true}) || !sw.running || !ta.cfg.Remote.Enabled {
		t.Fatalf("first press: calls %v running %v enabled %v", sw.calls, sw.running, ta.cfg.Remote.Enabled)
	}
	if got := s.rows(ta.App)[0].value(ta.App); got != "On" {
		t.Fatalf("row reads %q", got)
	}
}

// Shuffle and repeat from the TV's menus send no player event: the app tells
// the remote itself.
func TestRemoteNotifiedOfTVModeChanges(t *testing.T) {
	ta, _, fr := remoteApp(t)
	entries := modeEntries(ta.App)
	for i, name := range []string{"shuffle", "repeat"} {
		fr.reset()
		entries[i].run(ta.App)
		if !slices.Contains(fr.got(), remote.StateChanged) {
			t.Fatalf("%s: told %v", name, fr.got())
		}
	}
	fr.reset()
	ta.playSongs([]subsonic.Song{{ID: "s1"}}, 0, true)
	if !slices.Contains(fr.got(), remote.StateChanged) {
		t.Fatalf("play shuffled: told %v", fr.got())
	}
}

// gateLib holds GetAlbum until released, so a play can be in flight while the
// connection changes.
type gateLib struct {
	*fakeLibrary
	entered chan struct{}
	release chan struct{}
}

func (g gateLib) GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	g.entered <- struct{}{}
	<-g.release
	return g.fakeLibrary.GetAlbum(ctx, id)
}

// A play resolved against one server must not land on the next one's player.
func TestRemotePlayNeverCrossesAServerSwitch(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	g := gateLib{ta.lib, make(chan struct{}, 1), make(chan struct{})}
	ta.Attach(g, ta.pl, nil)
	old := ta.pl
	done := make(chan error, 1)
	go func() {
		_, err := ctl.Play(remote.PlayRequest{What: "album", ID: "al-1", How: "now"})
		done <- err
	}()
	<-g.entered
	ta.Detach()
	other := newFakePlayer()
	ta.Attach(ta.lib, other, nil)
	close(g.release)
	var err error
loop:
	for deadline := time.After(3 * time.Second); ; {
		select {
		case fn := <-ta.post:
			fn()
		case err = <-done:
			break loop
		case <-deadline:
			t.Fatal("the play never returned")
		}
	}
	if !errors.Is(err, remote.ErrNoConnection) {
		t.Fatalf("err %v", err)
	}
	for _, pl := range []*fakePlayer{old, other} {
		for _, c := range pl.calls {
			if c == "playnow" || c == "playnext" || c == "enqueue" {
				t.Fatalf("queued on a player: %v", pl.calls)
			}
		}
	}
}

// The remote's screenshot command takes the same screenshot as the button:
// a PNG in the folder and the toast on the TV.
func TestRemoteScreenshot(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.o.ScreenshotDir = t.TempDir()
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	if !ta.shooting {
		t.Fatal("no screenshot started")
	}
	// A second one while the first saves is a conflict, not a toast.
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); !errors.Is(err, remote.ErrShooting) {
		t.Fatalf("second screenshot: %v, want ErrShooting", err)
	}
	for ta.shooting {
		(<-ta.post)()
	}
	if got := lastToast(ta); got != "Screenshot saved" {
		t.Fatalf("toast %q", got)
	}
	if entries, _ := os.ReadDir(ta.o.ScreenshotDir); len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".png") {
		t.Fatalf("screenshots folder holds %v", entries)
	}
}

// A phone that taps the button over and over would write a PNG per tap to
// the SD card: the remote's screenshots keep a second apart. A saved one is
// answered as one still being saved.
func TestRemoteScreenshotsKeepASecondApart(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.o.ScreenshotDir = t.TempDir()
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); err != nil {
		t.Fatal(err)
	}
	for ta.shooting {
		(<-ta.post)()
	}
	ta.now = ta.now.Add(900 * time.Millisecond)
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); !errors.Is(err, remote.ErrShooting) {
		t.Fatalf("a second screenshot inside the interval: %v, want ErrShooting", err)
	}
	if ta.shooting {
		t.Fatal("a refused screenshot started saving")
	}
	ta.now = ta.now.Add(200 * time.Millisecond)
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); err != nil {
		t.Fatalf("a screenshot after the interval: %v", err)
	}
	for ta.shooting {
		(<-ta.post)()
	}
	if entries, _ := os.ReadDir(ta.o.ScreenshotDir); len(entries) != 2 {
		t.Fatalf("screenshots folder holds %d files, want 2", len(entries))
	}
}

func TestRemoteScreenshotNeedsAFolder(t *testing.T) {
	ta, ctl, _ := remoteApp(t)
	ta.o.ScreenshotDir = ""
	if err := ta.do(t, ctl, remote.Command{Do: "screenshot"}); !errors.Is(err, remote.ErrNoScreenshots) {
		t.Fatalf("got %v, want ErrNoScreenshots", err)
	}
	if ta.shooting {
		t.Fatal("a screenshot started with no folder")
	}
}
