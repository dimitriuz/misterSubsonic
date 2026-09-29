package main

import (
	"context"
	"os"
	"path/filepath"
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
	for _, name := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dir, "servers", name, "cache", "art")); err != nil {
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
	for name, want := range map[string]string{"home": "home", "a/b": "a_b", "..": "_", "c:\\x": "c__x"} {
		if got := filepath.Base(m.serverDir(name)); got != want {
			t.Errorf("serverDir(%q) = %q, want %q", name, got, want)
		}
	}
}
