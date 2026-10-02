package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/ui"
)

// idleCtl is a controller with nothing to say.
type idleCtl struct{}

func (idleCtl) State() remote.State                  { return remote.State{Status: "stopped", Repeat: "off", Index: -1} }
func (idleCtl) Queue() remote.QueueView              { return remote.QueueView{Index: -1} }
func (idleCtl) Do(remote.Command) error              { return nil }
func (idleCtl) Play(remote.PlayRequest) (int, error) { return 0, nil }
func (idleCtl) Library() remote.Library              { return nil }
func (idleCtl) Cover(context.Context, string, int) ([]byte, string, error) {
	return nil, "", fmt.Errorf("no cover")
}

// freePort returns a port nothing listens on (for a moment).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func stateStatus(port int) (int, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/state", port))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func TestRemoteHostStartsAndStops(t *testing.T) {
	port := freePort(t)
	h := &remoteHost{ctl: idleCtl{}, port: port}
	defer h.Close()
	if got, err := stateStatus(port); err == nil {
		t.Fatalf("answered before it was started (%d)", got)
	}
	if h.Running() {
		t.Fatal("running before it was started")
	}
	if err := h.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !h.Running() {
		t.Fatal("not running after a start")
	}
	if err := h.SetEnabled(true); err != nil { // again: nothing changes
		t.Fatal(err)
	}
	if code, err := stateStatus(port); err != nil || code != 200 {
		t.Fatalf("state: %d %v", code, err)
	}
	for _, u := range h.URLs() {
		if !strings.HasPrefix(u, "http://") || !strings.Contains(u, fmt.Sprintf(":%d", port)) {
			t.Errorf("url %q", u)
		}
	}
	h.Notify(remote.StateChanged) // reaches the running server
	if err := h.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := stateStatus(port); err == nil {
		t.Fatal("still answering after it was stopped")
	}
	if h.URLs() != nil {
		t.Fatalf("urls after stop: %v", h.URLs())
	}
	h.Notify(remote.StateChanged) // no server: nothing to tell, no crash
	if err := h.SetEnabled(true); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if code, err := stateStatus(port); err != nil || code != 200 {
		t.Fatalf("state after restart: %d %v", code, err)
	}
}

func TestRemoteHostPortInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := busy.Addr().(*net.TCPAddr).Port
	h := &remoteHost{ctl: idleCtl{}, port: port}
	defer h.Close()
	err = h.SetEnabled(true)
	if err == nil || err.Error() != fmt.Sprintf("port %d is in use", port) {
		t.Fatalf("got %v", err)
	}
	if h.URLs() != nil || h.Running() {
		t.Fatalf("a server that did not start: urls %v running %v", h.URLs(), h.Running())
	}
	h.Notify(remote.QueueChanged)
	busy.Close()
	if err := h.SetEnabled(true); err != nil {
		t.Fatalf("after the port was freed: %v", err)
	}
}

func TestRemoteHostClosesWhenClosed(t *testing.T) {
	port := freePort(t)
	h := &remoteHost{ctl: idleCtl{}, port: port}
	if err := h.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h.Close() // twice is fine
	if _, err := stateStatus(port); err == nil {
		t.Fatal("still answering after Close")
	}
}

type fakeToaster struct {
	mu     sync.Mutex
	toasts []string
}

func (f *fakeToaster) Post(fn func()) { fn() }
func (f *fakeToaster) Toast(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toasts = append(f.toasts, fmt.Sprintf(format, args...))
}

func TestStartRemoteBindFailureIsAToast(t *testing.T) {
	busy, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	h := &remoteHost{ctl: idleCtl{}, port: port}
	defer h.Close()
	ft := &fakeToaster{}
	startRemote(h, ft) // must not panic or exit
	if want := fmt.Sprintf("Remote: port %d is in use", port); len(ft.toasts) != 1 || ft.toasts[0] != want {
		t.Fatalf("toasts %q, want %q", ft.toasts, want)
	}
	// A start that works says nothing.
	ok := &remoteHost{ctl: idleCtl{}, port: freePort(t)}
	defer ok.Close()
	ft = &fakeToaster{}
	startRemote(ok, ft)
	if len(ft.toasts) != 0 {
		t.Fatalf("toasts %q", ft.toasts)
	}
}

func remoteConfig(t *testing.T, enabled bool, port int) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("[[server]]\nname = \"x\"\nurl = \"http://127.0.0.1:1\"\nusername = \"u\"\npassword = \"p\"\n[remote]\nenabled = %v\nport = %d\n", enabled, port)
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunStartsTheRemoteWhenEnabledAndStopsItOnExit(t *testing.T) {
	port := freePort(t)
	cfg := remoteConfig(t, true, port)
	var code int
	var getErr error
	old := beforeRun
	beforeRun = func(a *ui.App) { code, getErr = stateStatus(port) }
	defer func() { beforeRun = old }()
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond, log: filepath.Join(t.TempDir(), "log.txt")})
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	if getErr != nil || code != 200 {
		t.Fatalf("the remote did not answer while the app ran: %d %v", code, getErr)
	}
	if _, err := stateStatus(port); err == nil {
		t.Fatal("the remote still answers after the app exited")
	}
}

func TestRunWithTheRemoteOffListensNowhere(t *testing.T) {
	port := freePort(t)
	cfg := remoteConfig(t, false, port)
	var getErr error
	old := beforeRun
	beforeRun = func(a *ui.App) { _, getErr = stateStatus(port) }
	defer func() { beforeRun = old }()
	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond, log: filepath.Join(t.TempDir(), "log.txt")})
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	if getErr == nil {
		t.Fatal("something listens with the remote off")
	}
}

func TestRunCarriesOnWhenThePortIsInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	cfg := remoteConfig(t, true, port)
	logPath := filepath.Join(t.TempDir(), "log.txt")
	err = run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond, log: logPath})
	if err != nil {
		t.Fatalf("run returned %v", err)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "remote:") || !strings.Contains(string(b), "MiSTer Subsonic exiting") {
		t.Fatalf("log %q", b)
	}
}
