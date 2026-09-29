package ui

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func twoServers() *config.Config {
	c := config.Default()
	c.AddServer(config.Server{Name: "home", URL: "http://192.168.1.10:4533", Username: "alice", Password: "pw"})
	c.AddServer(config.Server{Name: "cloud", URL: "https://music.example.com", Username: "alice", Password: "pw"})
	c.DefaultServer = "home"
	return c
}

// connectRecorder is an Options.Connect that records what it was asked.
type connectRecorder struct{ got []*config.Config }

func (r *connectRecorder) connect(a *App, cfg *config.Config) { r.got = append(r.got, cfg) }

func sessionApp(t *testing.T, cfg *config.Config) (*testApp, *connectRecorder) {
	ta := newTestApp(t, ProfileHDMI)
	rec := &connectRecorder{}
	ta.cfg = cfg
	ta.o.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
	ta.o.Connect = rec.connect
	ta.Detach()
	return ta, rec
}

func messageTitle(ta *testApp) string {
	if m, ok := ta.Top().(*MessageScreen); ok {
		return m.title
	}
	return ""
}

func TestStartExplainsProblemsOrConnects(t *testing.T) {
	ta, rec := sessionApp(t, nil)
	ta.o.AudioErr = errors.New("no card")
	ta.start()
	if messageTitle(ta) != "No audio device" || len(rec.got) != 0 {
		t.Fatalf("audio error: %q, %d connects", messageTitle(ta), len(rec.got))
	}
	ta.o.AudioErr = nil
	ta.o.ConfigErr = config.ErrNotFound
	ta.start()
	if w, ok := ta.Top().(*WizardScreen); !ok || !w.firstRun || w.backup {
		t.Fatalf("no config: top %T", ta.Top())
	}
	ta.o.ConfigErr = errors.New("config: line 3: invalid TOML")
	ta.start()
	if messageTitle(ta) != "The config file has a problem" {
		t.Fatalf("invalid config: %q", messageTitle(ta))
	}
	ta.press(input.BtnA)
	if w, ok := ta.Top().(*WizardScreen); !ok || w.firstRun || !w.backup {
		t.Fatalf("A on the invalid-config message: top %T", ta.Top())
	}
	ta.o.ConfigErr = nil
	ta.cfg = config.Default() // valid, but no servers
	ta.start()
	if _, ok := ta.Top().(*WizardScreen); !ok {
		t.Fatalf("no servers: top %T", ta.Top())
	}
	ta.cfg = twoServers()
	ta.start()
	if messageTitle(ta) != "Connecting…" || len(rec.got) != 1 || rec.got[0].DefaultServer != "home" {
		t.Fatalf("valid config: %q, connects %v", messageTitle(ta), rec.got)
	}
	rec.got[0].DefaultServer = "changed"
	if ta.cfg.DefaultServer != "home" {
		t.Fatal("Connect was given the UI's own config, not a copy")
	}
}

func TestConnectedResetsCachesAndShowsTheLibrary(t *testing.T) {
	ta, _ := sessionApp(t, twoServers())
	ta.artists = ta.lib.artists
	ta.stars[albumStar(ta.lib.albums[0]).key()] = true
	ta.starGen = 3
	ta.in <- input.Event{Button: input.BtnA, Kind: input.Press} // queued before the connection
	srv := ta.cfg.Servers[1]
	srv.InsecureSkipVerify = true
	ta.Connected(ConnInfo{Server: srv, Auth: subsonic.AuthToken}, ta.lib, ta.pl, fakeArt{})
	if _, ok := ta.Top().(*SidebarRoot); !ok {
		t.Fatalf("top %T, want the root", ta.Top())
	}
	if ta.artists != nil || len(ta.stars) != 0 || ta.starGen != 0 || len(ta.in) != 0 {
		t.Fatalf("caches kept: artists %d, stars %d, gen %d, queued %d", len(ta.artists), len(ta.stars), ta.starGen, len(ta.in))
	}
	if !ta.insecure || ta.Player() == nil || ta.Library() == nil {
		t.Fatal("connection not installed")
	}
	if info, ok := ta.Conn(); !ok || info.Server.Name != "cloud" {
		t.Fatalf("conn %+v", info)
	}
}

func TestUnreachableOffersRetryAndSwitch(t *testing.T) {
	ta, rec := sessionApp(t, twoServers())
	ta.ConnectFailed(ta.cfg.Servers[0], errors.New("dial tcp: refused"))
	s, ok := ta.Top().(*UnreachableScreen)
	if !ok || len(s.actions) != 3 { // Try again, Switch server, Settings
		t.Fatalf("top %T with %d actions", ta.Top(), len(s.actions))
	}
	ta.press(input.BtnA) // Try again
	if len(rec.got) != 1 || rec.got[0].DefaultServer != "home" {
		t.Fatalf("retry connects %v", rec.got)
	}
	ta.ConnectFailed(ta.cfg.Servers[0], errors.New("dial tcp: refused"))
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Switch server
	m, ok := ta.Top().(*MenuScreen)
	if !ok || len(m.entries) != 1 || !strings.HasPrefix(m.entries[0].label, "cloud") {
		t.Fatalf("switch menu %T %+v", ta.Top(), m)
	}
	ta.press(input.BtnA)
	if len(rec.got) != 2 || rec.got[1].DefaultServer != "cloud" || ta.cfg.DefaultServer != "cloud" {
		t.Fatalf("switch: connects %v, default %q", rec.got, ta.cfg.DefaultServer)
	}
	waitFile(t, ta.o.ConfigPath, `default_server = "cloud"`)
}

func TestUnreachableWithOneServerOnlyRetries(t *testing.T) {
	c := config.Default()
	c.AddServer(config.Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	ta, _ := sessionApp(t, c)
	ta.ConnectFailed(c.Servers[0], errors.New("x"))
	if s := ta.Top().(*UnreachableScreen); len(s.actions) != 2 { // Try again, Settings
		t.Fatalf("%d actions", len(s.actions))
	}
}

// waitFile waits for path to exist and contain want (saves run off the UI goroutine).
func waitFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never contained %q (err %v, content %q)", path, want, err, b)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestVolumeSavesAreDebouncedAndFlushedOnExit(t *testing.T) {
	ta, _ := sessionApp(t, twoServers())
	for db := -1.0; db >= -5; db-- {
		ta.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = db }, true)
	}
	if _, err := os.Stat(ta.o.ConfigPath); err == nil {
		t.Fatal("saved before the pause")
	}
	if d := ta.untilWake(); d > saveDelay {
		t.Fatalf("next wake %v, want the save", d)
	}
	ta.now = ta.now.Add(saveDelay)
	ta.onWake()
	waitFile(t, ta.o.ConfigPath, "volume_db = -5.0")
	ta.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = -9 }, true)
	ta.flushConfig() // Run's exit path
	b, _ := os.ReadFile(ta.o.ConfigPath)
	if !strings.Contains(string(b), "volume_db = -9.0") {
		t.Fatalf("flush didn't save: %s", b)
	}
}

func TestSaveFailureIsAToastWithoutSecrets(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores the directory permissions this test relies on")
	}
	ta, _ := sessionApp(t, twoServers())
	log.SetOutput(io.Discard) // the failure is logged too; keep the test output clean
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	dir := t.TempDir()
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	ta.o.ConfigPath = filepath.Join(dir, "sub", "config.toml")
	ta.UpdateConfig(func(c *config.Config) { c.Playback.Scrobble = false }, false)
	deadline := time.Now().Add(2 * time.Second)
	for len(ta.toasts) == 0 && time.Now().Before(deadline) {
		select {
		case f := <-ta.post:
			f()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if len(ta.toasts) != 1 || !strings.HasPrefix(ta.toasts[0].text, "Couldn't save the settings: ") || strings.Contains(ta.toasts[0].text, "pw") {
		t.Fatalf("toasts %v", ta.toasts)
	}
}

func TestDisplayURLHidesCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"https://alice:secret@music.example.com/": "https://music.example.com/",
		"http://192.168.1.10:4533":                "http://192.168.1.10:4533",
		"http://[bad":                             "<server>",
	} {
		if got := displayURL(in); got != want {
			t.Errorf("displayURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlushWaitsForASaveInFlight(t *testing.T) {
	for i := 0; i < 200; i++ {
		ta, _ := sessionApp(t, twoServers())
		ta.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = -1 }, false) // save goroutine starts with -1
		ta.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = -9 }, true)  // a newer change is pending
		ta.flushConfig()
		b, _ := os.ReadFile(ta.o.ConfigPath)
		if !strings.Contains(string(b), "volume_db = -9.0") {
			t.Fatalf("round %d: an older save overtook the flush: %s", i, b)
		}
	}
}
