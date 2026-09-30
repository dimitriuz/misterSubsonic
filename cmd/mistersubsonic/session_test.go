package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/ui"
)

// fakeUI records what sessions tells the UI.
type fakeUI struct {
	mu        sync.Mutex
	connected []ui.ConnInfo
	players   []ui.Player
	failed    []string
}

func (f *fakeUI) Post(fn func())   { fn() }
func (f *fakeUI) ArtReady(art.Key) {}
func (f *fakeUI) Connected(info ui.ConnInfo, _ ui.Library, pl ui.Player, _ ui.ArtSource) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = append(f.connected, info)
	f.players = append(f.players, pl)
}
func (f *fakeUI) ConnectFailed(srv config.Server, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, srv.Name)
}

func (f *fakeUI) wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		ok := cond()
		f.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func testSessions(t *testing.T) (*sessions, string) {
	t.Helper()
	out, err := audio.OpenDevice(audio.DeviceOptions{Null: true}) // silent
	if err != nil {
		t.Fatal(err)
	}
	eng := audio.NewEngine(audio.EngineOptions{Output: out})
	dir := t.TempDir()
	m := newSessions(context.Background(), eng, dir, -20)
	t.Cleanup(func() { m.close(); eng.Close(); out.Close() })
	return m, dir
}

func cfgFor(urls map[string]string, def string) *config.Config {
	c := config.Default()
	for _, name := range []string{"a", "b"} {
		if u, ok := urls[name]; ok {
			c.AddServer(config.Server{Name: name, URL: u, Username: "u", Password: "p"})
		}
	}
	c.DefaultServer = def
	return c
}

func TestSwitchStopsTheOldSessionAndKeepsTheVolume(t *testing.T) {
	a, b := pingServer(nil), pingServer(nil)
	defer a.Close()
	defer b.Close()
	m, dir := testSessions(t)
	f := &fakeUI{}
	cfg := cfgFor(map[string]string{"a": a.URL, "b": b.URL}, "a")
	m.connect(f, cfg)
	f.wait(t, "connected to a", func() bool { return len(f.connected) == 1 })
	if f.connected[0].Server.Name != "a" {
		t.Fatalf("connected %+v", f.connected[0])
	}
	m.mu.Lock()
	first := m.cur
	m.mu.Unlock()
	f.players[0].SetVolumeDB(-17)

	cfg.DefaultServer = "b"
	m.connect(f, cfg)
	f.wait(t, "connected to b", func() bool { return len(f.connected) == 2 })
	select {
	case <-first.done:
	default:
		t.Fatal("the old player is still running")
	}
	if got := f.players[1].State().VolumeDB; got != -17 {
		t.Fatalf("new player volume %v, want the old one's -17", got)
	}
	check := &sessions{dataDir: dir}
	for _, name := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(check.serverDir(name), "cache", "art")); err != nil {
			t.Errorf("server %s has no data folder: %v", name, err)
		}
	}
}

func TestNewestConnectWins(t *testing.T) {
	gate := make(chan struct{})
	slow, fast := pingServer(gate), pingServer(nil)
	defer slow.Close()
	defer fast.Close()
	m, dir := testSessions(t)
	f := &fakeUI{}
	m.connect(f, cfgFor(map[string]string{"a": slow.URL}, "a"))
	m.connect(f, cfgFor(map[string]string{"b": fast.URL}, "b"))
	f.wait(t, "connected to b", func() bool { return len(f.connected) == 1 })
	close(gate) // a answers late
	time.Sleep(200 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.connected) != 1 || f.connected[0].Server.Name != "b" || len(f.failed) != 0 {
		t.Fatalf("connected %+v failed %v", f.connected, f.failed)
	}
	if _, err := os.Stat(filepath.Join(dir, "servers", "a")); err == nil {
		t.Fatal("the superseded connect built a session")
	}
}

func TestConnectFailureIsReported(t *testing.T) {
	dead := pingServer(nil)
	url := dead.URL
	dead.Close()
	m, _ := testSessions(t)
	f := &fakeUI{}
	m.connect(f, cfgFor(map[string]string{"a": url}, "a"))
	f.wait(t, "failure", func() bool { return len(f.failed) == 1 })
	if f.failed[0] != "a" || len(f.connected) != 0 {
		t.Fatalf("failed %v connected %v", f.failed, f.connected)
	}
}

func TestCloseStopsTheSessionAndLaterConnects(t *testing.T) {
	a := pingServer(nil)
	defer a.Close()
	m, _ := testSessions(t)
	f := &fakeUI{}
	cfg := cfgFor(map[string]string{"a": a.URL}, "a")
	m.connect(f, cfg)
	f.wait(t, "connected", func() bool { return len(f.connected) == 1 })
	m.mu.Lock()
	s := m.cur
	m.mu.Unlock()
	m.close()
	select {
	case <-s.done:
	default:
		t.Fatal("close left the player running")
	}
	m.connect(f, cfg)
	time.Sleep(200 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.connected) != 1 {
		t.Fatal("connected after close")
	}
}

func TestServerDirIsSafe(t *testing.T) {
	m := &sessions{dataDir: t.TempDir()}
	for name, want := range map[string]string{"home": "home-", "a/b": "a_b-", "..": "_-", "c:\\x": "c__x-"} {
		got := filepath.Base(m.serverDir(name))
		if !strings.HasPrefix(got, want) || len(got) != len(want)+8 {
			t.Errorf("serverDir(%q) = %q, want %q and 8 hex digits", name, got, want)
		}
	}
}

// Names that sanitize alike (or differ only in case, which exFAT folds) must
// not share a folder: IDs mean different songs on different servers.
func TestServerDirIsUniquePerServer(t *testing.T) {
	m := &sessions{dataDir: t.TempDir()}
	seen := map[string]string{}
	for _, name := range []string{"a/b", "a_b", "a\\b", "Home", "home", "HOME"} {
		dir := strings.ToLower(filepath.Base(m.serverDir(name)))
		if other, ok := seen[dir]; ok {
			t.Errorf("%q and %q share the folder %q", other, name, dir)
		}
		seen[dir] = name
	}
	if m.serverDir("home") != m.serverDir("home") {
		t.Error("the folder isn't stable")
	}
}

// A folder made by an older version (the plain sanitized name) keeps being
// used, so an upgrade doesn't lose the cache and resume state.
func TestServerDirKeepsAnOldFolder(t *testing.T) {
	m := &sessions{dataDir: t.TempDir()}
	old := filepath.Join(m.dataDir, "servers", "a_b")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := m.serverDir("a/b"); got != old {
		t.Errorf("serverDir = %q, want the old folder %q", got, old)
	}
}

// slowStops makes every stop wait for release (or, if release is nil, d).
func slowStops(m *sessions, release chan struct{}, d time.Duration) {
	m.beforeStop = func(*session) {
		if release != nil {
			<-release
		} else {
			time.Sleep(d)
		}
	}
}

func TestCloseWaitsForASwitchInProgress(t *testing.T) {
	a, b := pingServer(nil), pingServer(nil)
	defer a.Close()
	defer b.Close()
	m, _ := testSessions(t)
	f := &fakeUI{}
	cfg := cfgFor(map[string]string{"a": a.URL, "b": b.URL}, "a")
	m.connect(f, cfg)
	f.wait(t, "connected to a", func() bool { return len(f.connected) == 1 })
	m.mu.Lock()
	first := m.cur
	m.mu.Unlock()

	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock) // a failed test must not leave stops blocked
	slowStops(m, release, 0)
	cfg.DefaultServer = "b"
	m.connect(f, cfg) // its goroutine is now stuck stopping a
	returned := make(chan struct{})
	go func() { m.close(); close(returned) }()
	select {
	case <-returned:
		t.Fatal("close returned while the old session was still stopping")
	case <-time.After(150 * time.Millisecond):
	}
	unblock()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("close never returned")
	}
	select {
	case <-first.done:
	default:
		t.Fatal("the old player is still running after close")
	}
	time.Sleep(100 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if m.cur != nil || len(f.connected) != 1 {
		t.Fatalf("session left after close: cur %v connected %d", m.cur, len(f.connected))
	}
}

func TestNewerConnectWaitsForTheOlderStop(t *testing.T) {
	a, b, c := pingServer(nil), pingServer(nil), pingServer(nil)
	defer a.Close()
	defer b.Close()
	defer c.Close()
	m, _ := testSessions(t)
	f := &fakeUI{}
	cfg := cfgFor(map[string]string{"a": a.URL, "b": b.URL}, "a")
	m.connect(f, cfg)
	f.wait(t, "connected to a", func() bool { return len(f.connected) == 1 })
	m.mu.Lock()
	first := m.cur
	m.mu.Unlock()
	f.players[0].SetVolumeDB(-17)

	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock) // a failed test must not leave stops blocked
	slowStops(m, release, 0)
	cfg.DefaultServer = "b"
	m.connect(f, cfg)
	cfg2 := config.Default()
	cfg2.AddServer(config.Server{Name: "c", URL: c.URL, Username: "u", Password: "p"})
	cfg2.DefaultServer = "c"
	m.connect(f, cfg2)
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	n := len(f.connected)
	f.mu.Unlock()
	if n != 1 {
		t.Fatal("a newer connect started a player before the old one finished stopping")
	}
	unblock()
	f.wait(t, "connected to c", func() bool { return len(f.connected) == 2 })
	select {
	case <-first.done:
	default:
		t.Fatal("a is still running")
	}
	if f.connected[1].Server.Name != "c" || f.players[1].State().VolumeDB != -17 {
		t.Fatalf("c: %+v volume %v, want a's -17", f.connected[1].Server, f.players[1].State().VolumeDB)
	}
}

func TestCloseWaitsForTheStopWhenNoServerIsLeft(t *testing.T) {
	a := pingServer(nil)
	defer a.Close()
	m, _ := testSessions(t)
	f := &fakeUI{}
	m.connect(f, cfgFor(map[string]string{"a": a.URL}, "a"))
	f.wait(t, "connected to a", func() bool { return len(f.connected) == 1 })
	m.mu.Lock()
	first := m.cur
	m.mu.Unlock()
	slowStops(m, nil, 150*time.Millisecond)
	m.connect(f, config.Default()) // no servers: ends the session
	m.close()
	select {
	case <-first.done:
	default:
		t.Fatal("close returned before the session was stopped")
	}
}

// A connect that is superseded while its session is being built must stop
// that session, not install it (by a newer connect, or by a no-server one).
func TestSupersededDuringBuildStopsTheSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		next  func(a, b string) *config.Config
		wantB bool
	}{
		{"another server", func(a, b string) *config.Config { return cfgFor(map[string]string{"b": b}, "b") }, true},
		{"no server", func(a, b string) *config.Config { return config.Default() }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := pingServer(nil), pingServer(nil)
			defer a.Close()
			defer b.Close()
			m, _ := testSessions(t)
			f := &fakeUI{}
			built := make(chan *session, 1)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var first sync.Once
			m.afterBuild = func(s *session) {
				first.Do(func() { built <- s; <-release })
			}
			m.connect(f, cfgFor(map[string]string{"a": a.URL}, "a"))
			var s *session
			select {
			case s = <-built:
			case <-time.After(3 * time.Second):
				t.Fatal("a was never built")
			}
			m.connect(f, tc.next(a.URL, b.URL)) // arrives mid-build
			unblock()
			if tc.wantB {
				f.wait(t, "connected to b", func() bool { return len(f.connected) == 1 })
			}
			if !tc.wantB { // nothing may be installed, without help from close
				time.Sleep(100 * time.Millisecond)
				m.swap.Lock()
				m.mu.Lock()
				left := m.cur
				m.mu.Unlock()
				m.swap.Unlock()
				if left != nil {
					t.Fatal("the superseded session was installed")
				}
			}
			m.close() // waits for everything in flight
			select {
			case <-s.done:
			default:
				t.Fatal("the superseded session is still running")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, c := range f.connected {
				if c.Server.Name == "a" {
					t.Fatal("the superseded session was shown")
				}
			}
		})
	}
}
