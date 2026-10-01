package main

import (
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/ui"
)

func TestNoAudioDeviceShowsMessage(t *testing.T) {
	old := openDevice
	openDevice = func(audio.DeviceOptions) (audio.Output, error) { return nil, errors.New("boom") }
	defer func() { openDevice = old }()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	os.WriteFile(cfg, []byte("[[server]]\nname = \"x\"\nurl = \"http://127.0.0.1:1\"\nusername = \"u\"\npassword = \"p\"\n"), 0o600)
	frames := filepath.Join(dir, "frames", "nested")
	err := run(flags{config: cfg, display: "headless", frames: frames, volume: math.NaN(), exitAfter: 1e9})
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	pngs, _ := filepath.Glob(filepath.Join(frames, "*.png"))
	if len(pngs) == 0 {
		t.Fatal("no frame presented")
	}
}

func TestStartVolume(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name         string
		cfg, flag    float64
		null         bool
		display      string
		goos, goarch string
		want         float64
		quiet        bool
	}{
		{"desktop default", -6, nan, false, "viewer", "linux", "amd64", -30, true},
		{"desktop, any display", 0, nan, false, "fbdev", "darwin", "arm64", -30, true},
		{"desktop already quieter", -40, nan, false, "viewer", "linux", "amd64", -40, false},
		{"desktop explicit flag wins", -6, -10, false, "viewer", "linux", "amd64", -10, false},
		{"desktop null device", -6, nan, true, "viewer", "linux", "amd64", -6, false},
		{"mister fbdev keeps config", -6, nan, false, "fbdev", "linux", "arm", -6, false},
		{"mister fbdev flag wins", -6, -12, false, "fbdev", "linux", "arm", -12, false},
		{"arm viewer is quiet", -6, nan, false, "viewer", "linux", "arm", -30, true},
		{"arm headless is quiet", -6, nan, false, "headless", "linux", "arm", -30, true},
	}
	for _, c := range cases {
		got, quiet := startVolume(c.cfg, c.flag, c.null, c.display, c.goos, c.goarch)
		if got != c.want || quiet != c.quiet {
			t.Errorf("%s: got %v,%v want %v,%v", c.name, got, quiet, c.want, c.quiet)
		}
	}
}

func nullDevice(t *testing.T) {
	t.Helper()
	old := openDevice
	openDevice = func(audio.DeviceOptions) (audio.Output, error) {
		return audio.OpenDevice(audio.DeviceOptions{Null: true})
	}
	t.Cleanup(func() { openDevice = old })
}

func writeConfig(t *testing.T, url string) (dir, cfg string) {
	t.Helper()
	dir = t.TempDir()
	cfg = filepath.Join(dir, "config.toml")
	body := "[[server]]\nname = \"x\"\nurl = \"" + url + "\"\nusername = \"u\"\npassword = \"p\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

func pingServer(gate <-chan struct{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`)
	}))
}

// The player and art cache are built after a successful connect and torn
// down on exit (run under -race: construction happens off the UI goroutine).
func TestConnectBuildsSessionAndExitsCleanly(t *testing.T) {
	nullDevice(t)
	srv := pingServer(nil)
	defer srv.Close()
	dir, cfg := writeConfig(t, srv.URL)
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join((&sessions{dataDir: dir}).serverDir("x"), "cache", "art")); err != nil {
		t.Fatalf("art cache not created after connect: %v", err)
	}
}

// Exiting while the connect is still in flight must not build (or leak) a
// player afterwards.
func TestExitDuringConnectBuildsNothing(t *testing.T) {
	nullDevice(t)
	gate := make(chan struct{})
	srv := pingServer(gate)
	defer srv.Close()
	dir, cfg := writeConfig(t, srv.URL)
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	close(gate)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "servers")); err == nil {
		t.Fatal("a session was built after the app exited")
	}
}

func TestLogGoesToTheFileGiven(t *testing.T) {
	nullDevice(t)
	dir, cfg := writeConfig(t, "http://127.0.0.1:1")
	logPath := filepath.Join(dir, "app.log")
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond, log: logPath})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "MiSTer Subsonic dev starting") || !strings.Contains(string(b), "exiting") {
		t.Fatalf("log:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "crash.txt")); err != nil {
		t.Fatal("no crash.txt beside the log:", err)
	}
}

func TestLogDefaultsToLogTxtOnTheFramebuffer(t *testing.T) {
	dir := t.TempDir()
	closeLog := openLog("auto", "fbdev", dir)
	log.Print("hello")
	closeLog()
	if b, _ := os.ReadFile(filepath.Join(dir, "log.txt")); !strings.Contains(string(b), "hello") {
		t.Fatalf("log.txt: %q", b)
	}
	other := t.TempDir()
	openLog("auto", "viewer", other)() // elsewhere: stderr, no files
	if ents, _ := os.ReadDir(other); len(ents) != 0 {
		t.Fatalf("the viewer wrote %v", ents)
	}
}

func TestAnUnwritableLogFallsBackToStderr(t *testing.T) {
	closeLog := openLog(filepath.Join(t.TempDir(), "missing", "log.txt"), "fbdev", "")
	defer closeLog()
	if log.Writer() != os.Stderr {
		t.Fatal("the log went somewhere other than stderr")
	}
}

// A panic on the UI goroutine is recovered after the clean-ups ran (the
// console, input and framebuffer restored), logged with its stack and
// returned.
func TestAPanicInTheUIIsLoggedAndReturned(t *testing.T) {
	nullDevice(t)
	dir, cfg := writeConfig(t, "http://127.0.0.1:1")
	old := beforeRun
	beforeRun = func(*ui.App) { panic("boom") }
	defer func() { beforeRun = old }()
	logPath := filepath.Join(dir, "app.log")
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: time.Second, log: logPath})
	if err == nil || err.Error() != "panic: boom" {
		t.Fatalf("run returned %v", err)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "panic: boom") || !strings.Contains(string(b), "main_test.go") ||
		!strings.Contains(string(b), "error: panic: boom") {
		t.Fatalf("the log has no panic stack or final error:\n%s", b)
	}
}

func TestShutdownDeadlineFires(t *testing.T) {
	fired := make(chan struct{})
	old := forceExit
	forceExit = func() { close(fired) }
	defer func() { forceExit = old }()
	var d *time.Timer
	armDeadline(&d, 10*time.Millisecond)
	armDeadline(&d, time.Hour) // a second arm keeps the first deadline
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("the deadline didn't fire")
	}
}

func TestShutdownDeadlineIsStoppedAfterACleanExit(t *testing.T) {
	nullDevice(t)
	_, cfg := writeConfig(t, "http://127.0.0.1:1")
	fired := make(chan struct{}, 1)
	oldL, oldF := shutdownLimit, forceExit
	shutdownLimit, forceExit = 300*time.Millisecond, func() { fired <- struct{}{} }
	defer func() { shutdownLimit, forceExit = oldL, oldF }()
	if err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 200 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
		t.Fatal("the deadline fired after a clean exit")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestScreenshotDir(t *testing.T) {
	for _, c := range []struct{ flag, display, data, want string }{
		{"auto", "fbdev", "/media/fat/mistersubsonic", "/media/fat/screenshots/MiSTer_Subsonic"},
		{"auto", "viewer", "/home/u/mss", "/home/u/mss/screenshots"},
		{"/tmp/shots", "fbdev", "/media/fat/mistersubsonic", "/tmp/shots"},
		{"", "fbdev", "/media/fat/mistersubsonic", ""},
	} {
		if got := screenshotDir(c.flag, c.display, c.data); got != c.want {
			t.Errorf("screenshotDir(%q, %q, %q) = %q, want %q", c.flag, c.display, c.data, got, c.want)
		}
	}
}

// The final error line comes before "exiting", not after it.
func TestTheErrorLineComesBeforeExiting(t *testing.T) {
	nullDevice(t)
	dir, cfg := writeConfig(t, "http://127.0.0.1:1")
	old := beforeRun
	beforeRun = func(*ui.App) { panic("boom") }
	defer func() { beforeRun = old }()
	logPath := filepath.Join(dir, "app.log")
	run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: time.Second, log: logPath})
	b, _ := os.ReadFile(logPath)
	e, x := strings.Index(string(b), "error: panic: boom"), strings.Index(string(b), "MiSTer Subsonic exiting")
	if e < 0 || x < 0 || e > x {
		t.Fatalf("error at %d, exiting at %d:\n%s", e, x, b)
	}
}

// An oversized crash.txt is rotated to crash.txt.1, not deleted.
func TestAnOversizedCrashFileIsRotated(t *testing.T) {
	dir := t.TempDir()
	crash := filepath.Join(dir, "crash.txt")
	if err := os.WriteFile(crash, make([]byte, logMax+1), 0o644); err != nil {
		t.Fatal(err)
	}
	openLog(filepath.Join(dir, "log.txt"), "fbdev", dir)()
	if st, err := os.Stat(crash + ".1"); err != nil || st.Size() != logMax+1 {
		t.Fatalf("crash.txt.1: %v %v", st, err)
	}
	if st, err := os.Stat(crash); err != nil || st.Size() != 0 {
		t.Fatalf("crash.txt: %v %v", st, err)
	}
}

// A crash.txt that can't be opened is logged, not silent.
func TestAnUnopenableCrashFileIsLogged(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "crash.txt"), 0o755); err != nil { // a directory: open for append fails
		t.Fatal(err)
	}
	closeLog := openLog(filepath.Join(dir, "log.txt"), "fbdev", dir)
	closeLog()
	b, _ := os.ReadFile(filepath.Join(dir, "log.txt"))
	if !strings.Contains(string(b), "crash.txt") {
		t.Fatalf("log:\n%s", b)
	}
}
