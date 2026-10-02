# MiSTer Subsonic — Plan 7: the web remote

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a web remote for any phone or computer on the home network. It shows what's playing with the cover, controls playback and volume, edits the queue, and browses, searches and plays. It stays live alongside the TV, works with the TV off, and is off by default.

**Architecture:**
- **`internal/webguard`:** the dev viewer's Host and Origin checks, shared.
- **`internal/remote`:** an HTTP server with a JSON API, Server-Sent Events, browse, search and cover routes, and an embedded page. It reaches the app only through `remote.Controller`.
- **`internal/ui/remote.go`:** implements `Controller`. Commands are posted to the UI goroutine and run the same code as the buttons. Reads come from atomically published snapshots.
- **`cmd/mistersubsonic`:** starts and stops the server, from the config and from Settings → Remote.
- **`internal/player`:** gains `Move`.
- **`internal/subsonic`:** gains `GetSong`.

**Tech Stack:** Go 1.26+ (`net/http`, `embed`), plain JS (ES2020) and CSS, no new modules. The page's JS tests run with `node --test` when node is installed.

**Spec:** `docs/superpowers/specs/2026-10-02-mister-subsonic-web-remote-design.md`, approved by the user. It extends the main spec, `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`: §2, §8 and §13.

**Scope:**

| | |
|---|---|
| **Plans 1–6 (done)** | Playback, the TV interface, setup, MiSTer integration, releases, full resolution, partial redraws, hints, the follow-ups, the visualizer |
| **This plan (7)** | Everything in the spec, the e2e remote run, and the TV checks written down |
| **Later** | The device check (the MiSTer is off while this plan is written), faster list scrolling |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:** `go test -race ./...`, the launcher tests, `make e2e` (now with a remote run) and the ARM builds pass. The ARM builds are `make mister mister-test`, with glibc 2.29 ≤ 2.31.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-7`. At every task the new tests failed before the code and passed after, and the tree ended identical to the prototype's.
- **The page was checked by eye.** Headless Firefox screenshots were taken at phone size (390×844) and desktop size (1280×800), of Now Playing, Queue, Browse, Album and Search, and of the Reconnecting banner. The app ran with the mock Subsonic server and the null audio device.
- **The MiSTer** was off, so nothing ran on it.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:**
  - Go 1.26+ (`go.mod` says `go 1.26.0`).
  - No new modules.
  - cgo only in `internal/audio`.
  - ARM build: `scripts/check-glibc.sh` must report ≤ 2.31.
- **Off by default:** `[remote] enabled = false` and `port = 8080`. With it off, there is no listener.
- **No pairing (the user's choice), so these guards are binding:**
  - Every route checks the Host: IP literals, `localhost`, and this machine's hostname, with or without `.local` (`internal/webguard`).
  - POSTs need `Content-Type: application/json`, and a same-origin `Origin` when one is sent. Bodies are capped at 64 KiB.
  - The API exposes no settings, servers, credentials, files or logs.
  - Error bodies carry only the error kind's text, never the error text, which can hold the server URL.
  - Rejected requests are logged at most once a minute per reason, without header values.
- **The TV and the remote never disagree:** every command runs on the UI goroutine, through the same code as the matching button.
- **No new load when nobody is connected:** `Notify` is a flag set plus a non-blocking wake. Ticks run per client, and only while playing.
- **The page is self-contained:** no external URLs, fonts or CDNs, and under 60 KB embedded.
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device.
  - The by-eye check runs the app on the null device, against the local mock server only.
  - Ask the user's go-ahead before any listening check on the MiSTer.
- **Tests never touch real devices or the network beyond localhost** (`httptest`, the mock server).
- **Secrets:** nothing from `.env` is printed, committed or written into docs.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These are the five situations most likely to bite the user. Each gets its test in the task that owns the code:

1. **A tap that acts on the wrong song** because the queue changed under the phone. `jump`, `remove` and `move` carry the song id; a mismatch gives 409, and the page reloads the queue.
   - Tests: `TestCmdErrorMapping`, where `ErrStale` gives 409 (Task 2); `TestRemoteStale` (Task 4); the page's 409 handling in `web/app_test.js` (Task 5); and the e2e stale `song_id` (Task 4).
2. **A malicious website driving the MiSTer through the user's own browser.** DNS rebinding and CSRF must both be refused.
   - Tests: `TestGuards` and `TestBrowseGuarded` (Tasks 2–3), and `TestCheckHostRefusesForeignNames` and `TestSameOrigin` (Task 1).
3. **A slow or vanished phone stalling the app.** `Notify` must never block, a stalled stream is dropped, and the cap holds.
   - Tests: `TestNotifyNeverBlocksOnASlowClient`, `TestSSECapAndDisconnect` and `TestSSECloseEndsStreams`, run under `-race` (Task 2); and `TestRemoteBusy` (Task 4).
4. **Credentials or the server URL leaking to the browser.**
   - Tests: `TestBrowseErrors`, which checks there is no `u=` or `t=` in any error body (Task 3); and `TestPlayLibraryErrorMapping` (Task 4).
5. **A song that can't be played after an app restart.** A page still showing old results must play a song by id: the adapter falls back to `GetSong`.
   - Tests (Task 4): `TestRemotePlaySongs`, `TestGetSong` and `TestGetSongMissingIsNotFound`, and the e2e playing `so-1` before any browse.

## Decisions this plan makes (from the prototype; the spec left room)

- **`Player.Move`** works on queue indices, like `Remove` and `Jump`. With shuffle on, the play order is kept and relabelled. The prefetch is dropped only when the next song changed.
- **SSE:**
  - `Notify` sets per-client dirty flags and wakes each stream without blocking. Each stream reads `State()` and `Queue()` when it writes, so a slow client skips straight to the latest state.
  - A queue change sends a queue event, and a state change a state event.
  - Each stream has a 10 s write deadline.
  - Each client ticks every second, and only while playing.
- **Commands:**
  - `song_id` is optional on the server, so curl works without it. The page always sends it.
  - An oversized body gets 413, and success is `{"ok":true}`.
  - `how` defaults to "now".
  - At most 1000 song ids per play.
- **Browse:**
  - The handlers live in `browse.go`.
  - Pages are 50, fetched as 51 to know whether there is `more`.
  - Search needs 2 characters, and returns 20 artists, 20 albums and 50 songs.
  - Random album pages may repeat albums, because the server picks them.
- **Errors:** the body is the kind text: "not found" gives 404; "server unreachable", "timed out" and auth give 503; anything else gives 500. The spec said "the server's message"; that was changed on purpose so nothing can leak.
- **Covers:**
  - `art.Loader.Bytes` returns the raw bytes, from the disk cache or the server, cached under `id@size`.
  - The content type is sniffed.
  - `Cache-Control` is set only on success.
- **The adapter:**
  - The library, player and art are republished in an atomic copy whenever they change, and so are the stars. Reads never touch UI state.
  - Play resolves songs on the request goroutine and posts only the PlayNow, PlayNext or Enqueue.
  - A song id comes from songs already seen (up to 4096), then the queue, then `GetSong`.
  - A call that timed out is never run later.
  - There is no TV toast for remote actions, because a toast wakes the screensaver. Only errors toast.
- **Settings → Remote:**
  - Two rows: Remote On/Off, and Address (the first URL, "no network", or "Off").
  - A failed start leaves the setting off and toasts.
  - The port is read at launch.
- **The page:**
  - Hash routes: `#queue`, `#browse/album/ID` and so on.
  - Menus open inline under the row.
  - In Browse and Search, a tap on a song opens its menu; in the Queue, a tap plays from it.
  - The seek bar moves at once, then the next event corrects it.
  - The page runs its own reconnect back-off: 1, 2, 4 … 30 s.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/config`, `internal/player/player.go`, `internal/webguard`, `internal/devview`, `internal/ui/app.go`, `config.example.toml` | `[remote]`, `Move`, the shared guards | 1 |
| `internal/remote/server.go`, `api.go` | the server, guards, SSE, state, commands, play | 2 |
| `internal/remote/browse.go` | browse, search, covers | 3 |
| `internal/subsonic`, `tools/mocksubsonic`, `internal/art`, `internal/ui/remote.go`, `screens_settings.go`, `cmd/mistersubsonic`, `scripts/e2e-ui.sh` | `GetSong`, cover bytes, the adapter, Settings → Remote, wiring, the e2e run | 4 |
| `internal/remote/web/*`, `internal/remote/web.go` | the page | 5 |
| README, `docs/spikes.md`, `docs/testing-on-mister.md`, main spec, backlog | docs, the TV checks | 6 |

---

### Task 1: Groundwork: `[remote]` config, `Player.Move`, the shared Host check

**Files:**
- Create: `internal/webguard/webguard.go`
- Modify: `internal/config/config.go`, `internal/config/edit.go`, `internal/devview/devview.go`, `internal/player/player.go`, `internal/ui/app.go`, `sdcard/mistersubsonic/config.example.toml`
- Test: `internal/webguard/webguard_test.go` (new); `internal/config/config_test.go`, `internal/config/edit_test.go`, `internal/config/example_test.go`, `internal/devview/devview_test.go`, `internal/player/edges_test.go`, `internal/ui/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **Config.** `config.Remote{Enabled bool, Port int}` as `[remote]` (`enabled`, `port`). The defaults are false and 8080. A port outside 1–65535 loads as 8080, with a warning. The editor knows both keys, and the example config has the section with its safety note.
  - **`(*player.Player).Move(from, to int)`.** It works on queue indices and keeps the current song playing, with its index following the move.
    - Shuffle keeps the play order.
    - The prefetch is dropped only when the next song changed.
    - Out of range does nothing.
    - It joins `ui.Player` and the fakes.
  - **`internal/webguard`:**
    - `HostAllowed(hostport string, extra ...string) bool`: IP literals, `localhost`, and the extra names, case-insensitive.
    - `CheckHost(next http.Handler, extra ...string) http.Handler`.
    - `SameOrigin(r *http.Request) bool`.
    - `Hostnames() []string`: the hostname plus `<name>.local`, or nil.
    - `internal/devview` uses them, with its behaviour unchanged.

- [ ] **Step 1: Write the failing tests**

`internal/webguard/webguard_test.go` (new file):

```go
package webguard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHostAllowedIPLiteralsAndLocalhost(t *testing.T) {
	for host, want := range map[string]bool{
		"192.168.1.5:8090": true, "[fe80::1]:8090": true, "10.0.0.2": true, "localhost:8090": true, "[::1]:80": true, "::1": true,
		"evil.example:8090": false, "localhost.": false, "127.0.0.1.nip.io:8090": false, "": false,
	} {
		if got := HostAllowed(host); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestHostAllowedExtraNames(t *testing.T) {
	extra := []string{"mister", "mister.local"}
	for host, want := range map[string]bool{
		"mister:8080": true, "mister.local:8080": true, "mister.local": true, "MISTER.local:8080": true,
		"mister.example.com:8080": false, "other.local:8080": false, "mister.local.evil.example": false, "xmister": false,
	} {
		if got := HostAllowed(host, extra...); got != want {
			t.Errorf("HostAllowed(%q, %v) = %v, want %v", host, extra, got, want)
		}
	}
	if HostAllowed("mister:8080") {
		t.Error("a hostname is allowed only when the caller names it")
	}
}

func TestHostnamesAreTheMachinesWithAndWithoutLocal(t *testing.T) {
	name, err := os.Hostname()
	if err != nil || name == "" {
		t.Skip("no hostname on this machine")
	}
	got := Hostnames()
	if len(got) != 2 || got[0] != name || got[1] != name+".local" {
		t.Fatalf("Hostnames() = %v, want [%s %s.local]", got, name, name)
	}
	if !HostAllowed(name+":8080", got...) || !HostAllowed(name+".local", got...) {
		t.Fatal("the machine's own names must pass")
	}
}

func TestCheckHostRefusesForeignNames(t *testing.T) {
	reached := 0
	h := CheckHost(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ }), "mister.local")
	for host, want := range map[string]int{
		"127.0.0.1:8080": 200, "localhost:8080": 200, "mister.local:8080": 200, "192.168.1.9": 200,
		"evil.example:8080": 403, "mister.local.evil.example": 403,
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}
	if reached != 4 {
		t.Fatalf("handler reached %d times, want 4", reached)
	}
}

func TestSameOrigin(t *testing.T) {
	for name, tc := range map[string]struct {
		hdr  map[string]string
		want bool
	}{
		"no headers (curl)":    {nil, true},
		"same origin":          {map[string]string{"Origin": "http://192.168.1.5:8080", "Sec-Fetch-Site": "same-origin"}, true},
		"typed in the address": {map[string]string{"Sec-Fetch-Site": "none"}, true},
		"origin of the host":   {map[string]string{"Origin": "http://192.168.1.5:8080"}, true},
		"foreign origin":       {map[string]string{"Origin": "http://evil.example"}, false},
		"other port":           {map[string]string{"Origin": "http://192.168.1.5:9999"}, false},
		"cross-site fetch":     {map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		"same-site fetch":      {map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		"garbage origin":       {map[string]string{"Origin": "http://%zz"}, false},
		"null origin":          {map[string]string{"Origin": "null"}, false},
	} {
		req := httptest.NewRequest("POST", "http://192.168.1.5:8080/x", strings.NewReader(""))
		for k, v := range tc.hdr {
			req.Header.Set(k, v)
		}
		if got := SameOrigin(req); got != tc.want {
			t.Errorf("%s: SameOrigin = %v, want %v", name, got, tc.want)
		}
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t1-test.patch` and apply it from the repository root with `git apply /tmp/t1-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-7 branch)):

```diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
index 79c6a22..dadc056 100644
--- a/internal/config/config_test.go
+++ b/internal/config/config_test.go
@@ -4,6 +4,7 @@ import (
 	"errors"
 	"os"
 	"path/filepath"
+	"strconv"
 	"strings"
 	"testing"
 	"time"
@@ -233,3 +234,38 @@ func TestUnknownVisualizerLoadsAsOffWithAWarning(t *testing.T) {
 		t.Fatalf("warnings = %v", warns)
 	}
 }
+
+func TestRemoteDefaultsToOffOnPort8080(t *testing.T) {
+	if d := Default().Remote; d.Enabled || d.Port != 8080 {
+		t.Fatalf("default remote = %+v", d)
+	}
+	cfg, warns, err := Load(write(t, minimal))
+	if err != nil || len(warns) != 0 || cfg.Remote.Enabled || cfg.Remote.Port != 8080 {
+		t.Fatalf("minimal: %+v, %v, %v", cfg.Remote, warns, err)
+	}
+	cfg, warns, err = Load(write(t, minimal+"\n[remote]\nenabled = true\nport = 9000\n"))
+	if err != nil || len(warns) != 0 || !cfg.Remote.Enabled || cfg.Remote.Port != 9000 {
+		t.Fatalf("set: %+v, %v, %v", cfg.Remote, warns, err)
+	}
+}
+
+func TestBadRemotePortLoadsAs8080WithAWarning(t *testing.T) {
+	for _, port := range []string{"0", "-1", "65536", "99999"} {
+		cfg, warns, err := Load(write(t, minimal+"\n[remote]\nenabled = true\nport = "+port+"\n"))
+		if err != nil {
+			t.Fatal(err)
+		}
+		if cfg.Remote.Port != 8080 || !cfg.Remote.Enabled {
+			t.Errorf("port %s loaded as %+v, want 8080 and still enabled", port, cfg.Remote)
+		}
+		if len(warns) != 1 || !strings.Contains(warns[0], "remote.port") || !strings.Contains(warns[0], port) {
+			t.Errorf("port %s: warnings = %v", port, warns)
+		}
+	}
+	for _, port := range []string{"1", "65535"} {
+		cfg, warns, _ := Load(write(t, minimal+"\n[remote]\nport = "+port+"\n"))
+		if n, _ := strconv.Atoi(port); len(warns) != 0 || cfg.Remote.Port != n {
+			t.Errorf("port %s: %+v %v", port, cfg.Remote, warns)
+		}
+	}
+}
diff --git a/internal/config/edit_test.go b/internal/config/edit_test.go
index 4122134..243a0a8 100644
--- a/internal/config/edit_test.go
+++ b/internal/config/edit_test.go
@@ -274,3 +274,16 @@ func TestEditSetsTheVisualizerInPlace(t *testing.T) {
 		t.Fatalf("visualizer written twice:\n%s", again)
 	}
 }
+
+func TestEditSetsTheRemoteInPlace(t *testing.T) {
+	got, _ := saveEdited(t, commented, func(c *Config) { c.Remote.Enabled = true })
+	wantAll(t, got, "[remote]\nenabled = true", "# My own notes on this file.", "keep it quiet at night")
+	again, cfg := saveEdited(t, got, func(c *Config) { c.Remote.Port = 9090; c.Remote.Enabled = false })
+	wantAll(t, again, "enabled = false", "port = 9090")
+	if strings.Count(again, "enabled") != 1 || strings.Count(again, "[remote]") != 1 {
+		t.Fatalf("remote keys written twice:\n%s", again)
+	}
+	if cfg.Remote.Enabled || cfg.Remote.Port != 9090 {
+		t.Fatal(cfg.Remote)
+	}
+}
diff --git a/internal/config/example_test.go b/internal/config/example_test.go
index 62c681d..60df0e3 100644
--- a/internal/config/example_test.go
+++ b/internal/config/example_test.go
@@ -16,7 +16,7 @@ func TestExampleConfigLoads(t *testing.T) {
 		t.Fatalf("err %v, warnings %v", err, warns)
 	}
 	d := Default()
-	if c.Playback != d.Playback || c.Display != d.Display || c.Cache != d.Cache {
+	if c.Playback != d.Playback || c.Display != d.Display || c.Cache != d.Cache || c.Remote != d.Remote {
 		t.Fatalf("the example's settings differ from the defaults:\n%+v %+v %+v\n%+v %+v %+v",
 			c.Playback, c.Display, c.Cache, d.Playback, d.Display, d.Cache)
 	}
@@ -41,7 +41,7 @@ func TestExampleConfigShowsEveryOption(t *testing.T) {
 		}
 	}
 	for _, typ := range []reflect.Type{reflect.TypeOf(Config{}), reflect.TypeOf(Server{}), reflect.TypeOf(Playback{}),
-		reflect.TypeOf(Display{}), reflect.TypeOf(Cache{})} {
+		reflect.TypeOf(Display{}), reflect.TypeOf(Cache{}), reflect.TypeOf(Remote{})} {
 		for i := range typ.NumField() {
 			key := strings.Split(typ.Field(i).Tag.Get("toml"), ",")[0]
 			if typ.Field(i).Type.Kind() == reflect.Struct || typ.Field(i).Type.Kind() == reflect.Slice {
diff --git a/internal/devview/devview_test.go b/internal/devview/devview_test.go
index 3eb6daf..61a23a7 100644
--- a/internal/devview/devview_test.go
+++ b/internal/devview/devview_test.go
@@ -211,20 +211,6 @@ func TestRejectsForeignHost(t *testing.T) {
 	}
 }
 
-// A wildcard bind (for a phone or another PC) is reached by IP address;
-// only hostnames other than localhost are refused.
-func TestHostCheckAllowsIPLiterals(t *testing.T) {
-	v := New(1, 1)
-	for host, want := range map[string]bool{
-		"192.168.1.5:8090": true, "[fe80::1]:8090": true, "10.0.0.2": true, "localhost:8090": true,
-		"evil.example:8090": false, "localhost.": false, "127.0.0.1.nip.io:8090": false, "": false,
-	} {
-		if got := v.hostAllowed(host); got != want {
-			t.Errorf("hostAllowed(%q) = %v, want %v", host, got, want)
-		}
-	}
-}
-
 func TestURLForWildcardBindIsLoopback(t *testing.T) {
 	if got := pageURL(&net.TCPAddr{IP: net.IPv6unspecified, Port: 8090}); got != "http://127.0.0.1:8090/" {
 		t.Fatalf("wildcard URL = %q", got)
diff --git a/internal/player/edges_test.go b/internal/player/edges_test.go
index 18045ee..31dfe0d 100644
--- a/internal/player/edges_test.go
+++ b/internal/player/edges_test.go
@@ -2,6 +2,7 @@ package player
 
 import (
 	"context"
+	"slices"
 	"testing"
 	"time"
 
@@ -151,3 +152,144 @@ func TestFailedPrefetchReportsOnce(t *testing.T) {
 		t.Fatalf("error events = %d, want 1", errs)
 	}
 }
+
+// Move (the web remote's reorder): the queue order changes, the current song
+// and its playback do not.
+
+func queueIDs(p *Player) []subsonic.ID {
+	var out []subsonic.ID
+	for _, s := range p.State().Queue {
+		out = append(out, s.ID)
+	}
+	return out
+}
+
+func TestMoveReordersAndCurrentIndexFollows(t *testing.T) {
+	for _, tc := range []struct {
+		name      string
+		cur       int
+		from, to  int
+		wantQueue []subsonic.ID
+		wantIndex int
+	}{
+		{"entry before current crosses it", 2, 0, 3, []subsonic.ID{"sb", "sc", "sd", "sa", "se"}, 1},
+		{"entry after current crosses it", 1, 3, 0, []subsonic.ID{"sd", "sa", "sb", "sc", "se"}, 2},
+		{"current itself moves down", 1, 1, 3, []subsonic.ID{"sa", "sc", "sd", "sb", "se"}, 3},
+		{"current itself moves up", 3, 3, 0, []subsonic.ID{"sd", "sa", "sb", "sc", "se"}, 0},
+		{"entries on one side only", 0, 3, 4, []subsonic.ID{"sa", "sb", "sc", "se", "sd"}, 0},
+		{"first to last", 2, 0, 4, []subsonic.ID{"sb", "sc", "sd", "se", "sa"}, 1},
+		{"last to first", 2, 4, 0, []subsonic.ID{"se", "sa", "sb", "sc", "sd"}, 3},
+		{"same place", 2, 2, 2, []subsonic.ID{"sa", "sb", "sc", "sd", "se"}, 2},
+	} {
+		t.Run(tc.name, func(t *testing.T) {
+			h := newHarness(t, nil)
+			h.p.PlayNow(songs(5, 100), tc.cur)
+			h.playAndStart(1)
+			h.p.Move(tc.from, tc.to)
+			if got := queueIDs(h.p); !slices.Equal(got, tc.wantQueue) {
+				t.Fatalf("queue = %v, want %v", got, tc.wantQueue)
+			}
+			st := h.p.State()
+			if st.Index != tc.wantIndex {
+				t.Fatalf("index = %d, want %d", st.Index, tc.wantIndex)
+			}
+			if cur, _ := st.Current(); cur.ID != subsonic.ID("s"+string(rune('a'+tc.cur))) {
+				t.Fatalf("current song = %v after the move", cur.ID)
+			}
+			if h.eng.playCount() != 1 || h.eng.stops != 0 {
+				t.Fatalf("playback was interrupted: %d plays, %d stops", h.eng.playCount(), h.eng.stops)
+			}
+		})
+	}
+}
+
+func TestMoveOutOfRangeDoesNothing(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(3, 100), 1)
+	h.playAndStart(1)
+	for _, m := range [][2]int{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {9, 9}} {
+		h.p.Move(m[0], m[1])
+	}
+	if got := queueIDs(h.p); !slices.Equal(got, []subsonic.ID{"sa", "sb", "sc"}) {
+		t.Fatalf("queue = %v", got)
+	}
+	if st := h.p.State(); st.Index != 1 {
+		t.Fatalf("index = %d", st.Index)
+	}
+	newHarness(t, nil).p.Move(0, 1) // an empty queue
+}
+
+// During shuffle the visible queue moves like any other; the play order
+// (which song comes next) is kept, only relabelled.
+func TestMoveDuringShuffleKeepsPlayOrder(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(6, 100), 2)
+	h.playAndStart(1)
+	h.p.SetShuffle(true)
+	seq := func() []subsonic.ID { // ids in play order from the cursor on
+		var out []subsonic.ID
+		h.p.do(func() {
+			for _, qi := range h.p.order[h.p.cursor:] {
+				out = append(out, h.p.queue[qi].ID)
+			}
+		})
+		return out
+	}
+	before := seq()
+	h.p.Move(5, 0)
+	h.p.Move(1, 4)
+	if got := seq(); !slices.Equal(got, before) {
+		t.Fatalf("play order %v, was %v", got, before)
+	}
+	st := h.p.State()
+	if cur, _ := st.Current(); cur.ID != "sc" {
+		t.Fatalf("current = %v", cur.ID)
+	}
+	if next := st.Queue[st.NextIndex].ID; next != before[1] {
+		t.Fatalf("next = %v, want %v", next, before[1])
+	}
+	if h.eng.playCount() != 1 {
+		t.Fatal("shuffled Move restarted playback")
+	}
+}
+
+func TestMoveRePrefetchesOnlyWhenNextChanged(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(5, 100), 0) // sa sb sc sd se
+	a := h.playAndStart(1)
+	h.tickAt(a.ID, 85*time.Second) // prefetches sb
+	h.waitFor("queued", func() bool { return h.eng.queueCount() == 1 })
+	clears := h.eng.clearCount()
+
+	h.p.Move(3, 4) // behind the next song: sb stays next
+	if h.eng.clearCount() != clears {
+		t.Fatal("a move away from the next song dropped the prefetch")
+	}
+	if st := h.p.State(); st.Queue[st.NextIndex].ID != "sb" {
+		t.Fatalf("next = %v", st.Queue[st.NextIndex].ID)
+	}
+
+	h.p.Move(1, 3) // the prefetched song leaves: sc is next now
+	if h.eng.clearCount() == clears {
+		t.Fatal("the prefetched song moved but the prefetch was kept")
+	}
+	h.tickAt(a.ID, 86*time.Second)
+	h.waitFor("re-prefetched", func() bool { return h.eng.queueCount() == 2 })
+	calls := h.opener.callList()
+	if last := calls[len(calls)-1]; last.id != "sc" || !last.prefetch {
+		t.Fatalf("re-prefetch call = %+v, want sc", last)
+	}
+}
+
+// Without shuffle Next follows the moved queue, not the old order.
+func TestMoveThenNextPlaysTheNewNeighbour(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(4, 100), 0) // sa sb sc sd
+	h.playAndStart(1)
+	h.p.Move(3, 1) // sa sd sb sc
+	h.p.Next()
+	h.waitFor("next opened", func() bool { return h.eng.playCount() == 2 })
+	if cur, _ := h.p.State().Current(); cur.ID != "sd" {
+		t.Fatalf("Next played %v, want sd", cur.ID)
+	}
+}
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 6f072ac..0163a93 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -221,6 +221,7 @@ func (p *fakePlayer) Prev()                       { p.call("prev") }
 func (p *fakePlayer) Seek(d time.Duration)        { p.call("seek"); p.seekPos, p.st.Position = d, d }
 func (p *fakePlayer) Jump(i int)                  { p.call("jump"); p.st.Index = i }
 func (p *fakePlayer) Remove(i int)                { p.call("remove") }
+func (p *fakePlayer) Move(from, to int)           { p.call("move") }
 func (p *fakePlayer) SetShuffle(on bool)          { p.st.Shuffle = on }
 func (p *fakePlayer) SetRepeat(r player.Repeat)   { p.st.Repeat = r }
 func (p *fakePlayer) ResumeFrom(r *player.Resume) {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/config ./internal/player ./internal/webguard ./internal/devview ./internal/ui`

Expected: FAIL, e.g.:

```
Default().Remote undefined (type *Config has no field or method Remote)
cfg.Remote undefined (type *Config has no field or method Remote)
c.Remote undefined (type *Config has no field or method Remote)
undefined: HostAllowed
undefined: Hostnames
undefined: CheckHost
undefined: SameOrigin
h.p.Move undefined (type *Player has no field or method Move)
```

- [ ] **Step 3: Implement**

`internal/webguard/webguard.go` (new file):

```go
// Package webguard holds the checks both of the app's small web servers (the
// development viewer and the web remote) put in front of their handlers.
package webguard

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// CheckHost refuses requests whose Host header is a name other than
// localhost or one of extra. A DNS-rebinding page is same-origin with its own
// hostname, so without this it could press keys and read frames; it can't
// make the browser send an IP address as Host, so IP literals (the LAN
// address of a wildcard bind, say) are fine.
func CheckHost(next http.Handler, extra ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !HostAllowed(r.Host, extra...) {
			http.Error(w, "unexpected Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HostAllowed reports whether a Host header value (with or without a port)
// is an IP literal, localhost, or one of the extra names.
func HostAllowed(hostport string, extra ...string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "localhost" || net.ParseIP(host) != nil {
		return true
	}
	for _, e := range extra {
		if e != "" && strings.EqualFold(host, e) {
			return true
		}
	}
	return false
}

// Hostnames is this machine's name and the same with ".local" (mDNS), the
// names a phone on the home network may use to reach it. It is empty when
// the machine has no name.
func Hostnames() []string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return nil
	}
	return []string{name, name + ".local"}
}

// SameOrigin refuses requests from other web pages, which could otherwise
// press buttons (and start playback) through the user's browser.
func SameOrigin(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			return false
		}
	}
	return true
}
```

Then Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-7 branch)):

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index 028f61e..ddcc9d0 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -24,6 +24,7 @@ type Config struct {
 	Playback      Playback `toml:"playback"`
 	Display       Display  `toml:"display"`
 	Cache         Cache    `toml:"cache"`
+	Remote        Remote   `toml:"remote"`
 }
 
 type Server struct {
@@ -61,12 +62,19 @@ type Cache struct {
 	CoverArtMB int `toml:"cover_art_mb"`
 }
 
+// Remote is the web remote: a page on the home network that controls playback.
+type Remote struct {
+	Enabled bool `toml:"enabled"`
+	Port    int  `toml:"port"`
+}
+
 // Default returns a config with every default filled in and no servers.
 func Default() *Config {
 	return &Config{
 		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
 		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true, Hints: true, Visualizer: "off"},
 		Cache:    Cache{CoverArtMB: 200},
+		Remote:   Remote{Port: 8080},
 	}
 }
 
@@ -97,6 +105,10 @@ func Load(path string) (cfg *Config, warnings []string, err error) {
 		warnings = append(warnings, fmt.Sprintf("display.visualizer: unknown value %q, using off", cfg.Display.Visualizer))
 		cfg.Display.Visualizer = "off"
 	}
+	if cfg.Remote.Port < 1 || cfg.Remote.Port > 65535 { // as a typo above: carry on with the default
+		warnings = append(warnings, fmt.Sprintf("remote.port: %d is not 1..65535, using 8080", cfg.Remote.Port))
+		cfg.Remote.Port = 8080
+	}
 	if err := cfg.Validate(); err != nil {
 		return nil, warnings, fmt.Errorf("config: %s: %w", path, err)
 	}
diff --git a/internal/config/edit.go b/internal/config/edit.go
index 7604449..94941bf 100644
--- a/internal/config/edit.go
+++ b/internal/config/edit.go
@@ -83,6 +83,8 @@ func scalars(c *Config) []scalar {
 		{"display", "hints", c.Display.Hints},
 		{"display", "visualizer", c.Display.Visualizer},
 		{"cache", "cover_art_mb", c.Cache.CoverArtMB},
+		{"remote", "enabled", c.Remote.Enabled},
+		{"remote", "port", c.Remote.Port},
 	}
 }
 
diff --git a/internal/devview/devview.go b/internal/devview/devview.go
index ccc758b..4fd889e 100644
--- a/internal/devview/devview.go
+++ b/internal/devview/devview.go
@@ -11,14 +11,13 @@ import (
 	"image/png"
 	"net"
 	"net/http"
-	"net/url"
 	"strconv"
-	"strings"
 	"sync"
 	"time"
 
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/webguard"
 )
 
 //go:embed page.html
@@ -129,7 +128,7 @@ func (v *Viewer) Handler() http.Handler {
 	})
 	mux.HandleFunc("GET /frame", v.serveFrame)
 	mux.HandleFunc("POST /key", func(w http.ResponseWriter, r *http.Request) {
-		if !sameOrigin(r) {
+		if !webguard.SameOrigin(r) {
 			http.Error(w, "cross-origin request refused", http.StatusForbidden)
 			return
 		}
@@ -168,46 +167,7 @@ func (v *Viewer) Handler() http.Handler {
 		}
 		w.WriteHeader(http.StatusNoContent)
 	})
-	return v.checkHost(mux)
-}
-
-// checkHost refuses requests whose Host header is a name other than
-// localhost. A DNS-rebinding page is same-origin with its own hostname, so
-// without this it could press keys and read frames; it can't make the
-// browser send an IP address as Host, so IP literals (the LAN address of a
-// wildcard bind, say) are fine.
-func (v *Viewer) checkHost(next http.Handler) http.Handler {
-	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
-		if !v.hostAllowed(r.Host) {
-			http.Error(w, "unexpected Host header", http.StatusForbidden)
-			return
-		}
-		next.ServeHTTP(w, r)
-	})
-}
-
-func (v *Viewer) hostAllowed(hostport string) bool {
-	host := hostport
-	if h, _, err := net.SplitHostPort(hostport); err == nil {
-		host = h
-	}
-	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
-	return host == "localhost" || net.ParseIP(host) != nil
-}
-
-// sameOrigin refuses requests from other web pages, which could otherwise
-// press buttons (and start playback) through the user's browser.
-func sameOrigin(r *http.Request) bool {
-	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
-		return false
-	}
-	if o := r.Header.Get("Origin"); o != "" {
-		u, err := url.Parse(o)
-		if err != nil || u.Host != r.Host {
-			return false
-		}
-	}
-	return true
+	return webguard.CheckHost(mux)
 }
 
 var errGone = errors.New("viewer closed")
diff --git a/internal/player/player.go b/internal/player/player.go
index 45637b1..882a9b9 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -393,6 +393,60 @@ func (p *Player) Remove(i int) {
 	})
 }
 
+// Move moves queue[from] to position to, shifting the entries between. The
+// current song keeps playing and the index follows it. The play order (what
+// comes next under shuffle) is kept, only relabelled to the new positions.
+// Without shuffle the order is the queue's own. The prefetched successor is
+// dropped only when the next song changed.
+func (p *Player) Move(from, to int) {
+	p.do(func() {
+		n := len(p.queue)
+		if from < 0 || from >= n || to < 0 || to >= n || from == to {
+			return
+		}
+		moved := func(i int) int { // where old position i ends up
+			switch {
+			case i == from:
+				return to
+			case from < to && i > from && i <= to:
+				return i - 1
+			case to < from && i >= to && i < from:
+				return i + 1
+			}
+			return i
+		}
+		oldNext := -1
+		if c := p.followingCursor(true); c >= 0 {
+			oldNext = moved(p.order[c])
+		}
+		q := make([]subsonic.Song, n)
+		for i, s := range p.queue {
+			q[moved(i)] = s
+		}
+		p.queue = q
+		if p.shuffle { // the same sequence of songs, under their new positions
+			for oi, qi := range p.order {
+				p.order[oi] = moved(qi)
+			}
+		} else { // the order is the queue's: the cursor follows the current song
+			if p.cursor >= 0 {
+				p.cursor = moved(p.cursor)
+			}
+			for i := range p.order {
+				p.order[i] = i
+			}
+		}
+		newNext := -1
+		if c := p.followingCursor(true); c >= 0 {
+			newNext = p.order[c]
+		}
+		if newNext != oldNext {
+			p.invalidateNext()
+		}
+		p.emit(Event{Kind: QueueChanged})
+	})
+}
+
 // Clear empties the queue and stops.
 func (p *Player) Clear() {
 	p.do(func() {
diff --git a/internal/ui/app.go b/internal/ui/app.go
index bbfe4b1..08d01dd 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -48,6 +48,7 @@ type Player interface {
 	Seek(pos time.Duration)
 	Jump(i int)
 	Remove(i int)
+	Move(from, to int)
 	SetShuffle(on bool)
 	SetRepeat(r player.Repeat)
 	Resumable(ctx context.Context) (*player.Resume, error)
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index 9c1138d..399535a 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -40,3 +40,11 @@ visualizer = "off"         # moving picture on Now Playing: off, bars, scope, vu
 
 [cache]
 cover_art_mb = 200         # cover art kept on the SD card, per server
+
+[remote]
+# A web page for your phone or computer: see what is playing, control it, edit
+# the queue, browse and search. Settings → Remote shows the address to open.
+# There is no password: anyone on your home network can control playback while
+# it is on. It uses plain http, so keep it off networks you do not trust.
+enabled = false
+port = 8080                # 1 to 65535
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/config ./internal/player ./internal/webguard ./internal/devview ./internal/ui`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/edit.go internal/config/edit_test.go internal/config/example_test.go internal/devview/devview.go internal/devview/devview_test.go internal/player/edges_test.go internal/player/player.go internal/ui/app.go internal/ui/fakes_test.go internal/webguard/webguard.go internal/webguard/webguard_test.go sdcard/mistersubsonic/config.example.toml
git commit -m "config, player, webguard: [remote] settings, moving a queue entry, the dev viewer's Host check shared" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: The remote server: state, live events, commands, play

**Files:**
- Create: `internal/remote/server.go`, `internal/remote/api.go`
- Test: `internal/remote/remote_test.go` (new)

**Interfaces:**
- **Consumes:** `webguard.HostAllowed` and `webguard.SameOrigin` (Task 1).
- **Produces** (`internal/remote`):
  - **The server.**
    - `Options{MaxClients, Tick, Heartbeat, Hostnames, Log}`.
    - `New(ctl Controller, opts) *Server`, with the methods `Handler()`, `Listen(addr)`, `Addr()`, `URLs()`, `Close()` and `Notify(Change)`.
    - `Change` has the values `StateChanged` and `QueueChanged`.
  - **`Controller`:** `State() State`, `Queue() QueueView`, `Do(Command) error`, `Play(PlayRequest) (int, error)`, `Library() Library` and `Cover(ctx, id, size) ([]byte, string, error)`.
  - **`Library`:** the read-only subset of the Subsonic client that Task 3 uses.
  - **The JSON types:** `Song`, `State`, `QueueView`, `Command` and `PlayRequest`, with snake_case tags.
  - **The sentinel errors:** `ErrStale` gives 409 and `ErrBusy` gives 503.
  - **Routes:**
    - `GET /api/state` and `GET /api/queue`;
    - `GET /api/events` (SSE): the first state and queue, events on `Notify`, a tick each second while playing, a heartbeat every 15 s, a cap of 8 with 503 beyond it, a 10 s write deadline, and `Close` ends every stream;
    - `POST /api/cmd`: a validation table; volume clamped to −60..0; `{"ok":true}` on success;
    - `POST /api/play`: `{added: N}`.
  - **Guards, on every route:**
    - the Host check, then for POSTs a JSON content type and same-origin;
    - a 64 KiB body cap (413);
    - rejections logged at most once a minute per reason.

- [ ] **Step 1: Write the failing tests**

`internal/remote/remote_test.go` (new file):

```go
package remote

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCtl struct {
	mu     sync.Mutex
	state  State
	queue  QueueView
	cmds   []Command
	plays  []PlayRequest
	doErr  error
	played int
}

func (f *fakeCtl) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
func (f *fakeCtl) setState(st State) { f.mu.Lock(); f.state = st; f.mu.Unlock() }
func (f *fakeCtl) Queue() QueueView {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queue
}
func (f *fakeCtl) Do(c Command) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, c)
	return f.doErr
}
func (f *fakeCtl) Play(p PlayRequest) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plays = append(f.plays, p)
	return f.played, f.doErr
}
func (f *fakeCtl) Library() Library { return nil }
func (f *fakeCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	return nil, "", errors.New("none")
}
func (f *fakeCtl) lastCmd(t *testing.T) Command {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cmds) == 0 {
		t.Fatal("no command reached the controller")
	}
	return f.cmds[len(f.cmds)-1]
}
func (f *fakeCtl) ncmds() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.cmds) }

var song = Song{ID: "s1", Title: "Title", Artist: "Artist", Album: "Album", CoverID: "c1", DurationMS: 200000, Starred: true}

func newFake() *fakeCtl {
	return &fakeCtl{
		state: State{Song: &song, Status: "paused", PositionMS: 1500, DurationMS: 200000, VolumeDB: -12.5, Repeat: "off", Index: 1},
		queue: QueueView{Index: 1, Songs: []Song{song, song}},
	}
}

func do(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "192.168.1.50:8080"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestStateJSON(t *testing.T) {
	s := New(newFake(), Options{})
	w := do(s.Handler(), "GET", "/api/state", "", nil)
	want := `{"song":{"id":"s1","title":"Title","artist":"Artist","album":"Album","cover_id":"c1","duration_ms":200000,"starred":true},"status":"paused","position_ms":1500,"duration_ms":200000,"volume_db":-12.5,"muted":false,"shuffle":false,"repeat":"off","index":1}`
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
}

func TestStateNoSong(t *testing.T) {
	f := newFake()
	f.state = State{Status: "stopped", Repeat: "off"}
	w := do(New(f, Options{}).Handler(), "GET", "/api/state", "", nil)
	if !strings.HasPrefix(w.Body.String(), `{"song":null,"status":"stopped"`) {
		t.Fatal(w.Body)
	}
}

func TestQueueJSON(t *testing.T) {
	f := newFake()
	w := do(New(f, Options{}).Handler(), "GET", "/api/queue", "", nil)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), `{"index":1,"songs":[{"id":"s1"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	f.queue = QueueView{}
	w = do(New(f, Options{}).Handler(), "GET", "/api/queue", "", nil)
	if strings.TrimSpace(w.Body.String()) != `{"index":0,"songs":[]}` {
		t.Fatalf("empty queue: %s", w.Body)
	}
}

func TestCmdValidation(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		want       *Command // what the controller must receive, nil if it must not be called
	}{
		{"toggle", `{"do":"toggle"}`, 200, &Command{Do: "toggle"}},
		{"next", `{"do":"next"}`, 200, &Command{Do: "next"}},
		{"prev", `{"do":"prev"}`, 200, &Command{Do: "prev"}},
		{"clear", `{"do":"clear"}`, 200, &Command{Do: "clear"}},
		{"seek", `{"do":"seek","position_ms":4000}`, 200, &Command{Do: "seek", PositionMS: 4000}},
		{"seek negative", `{"do":"seek","position_ms":-1}`, 400, nil},
		{"volume", `{"do":"volume","db":-20}`, 200, &Command{Do: "volume", DB: -20}},
		{"volume clamped low", `{"do":"volume","db":-90}`, 200, &Command{Do: "volume", DB: -60}},
		{"volume clamped high", `{"do":"volume","db":6}`, 200, &Command{Do: "volume", DB: 0}},
		{"mute", `{"do":"mute","on":true}`, 200, &Command{Do: "mute", On: true}},
		{"shuffle", `{"do":"shuffle","on":true}`, 200, &Command{Do: "shuffle", On: true}},
		{"star", `{"do":"star","on":true}`, 200, &Command{Do: "star", On: true}},
		{"repeat all", `{"do":"repeat","mode":"all"}`, 200, &Command{Do: "repeat", Mode: "all"}},
		{"repeat bad", `{"do":"repeat","mode":"twice"}`, 400, nil},
		{"repeat missing", `{"do":"repeat"}`, 400, nil},
		{"jump", `{"do":"jump","index":3,"song_id":"x"}`, 200, &Command{Do: "jump", Index: 3, SongID: "x"}},
		{"jump negative", `{"do":"jump","index":-1}`, 400, nil},
		{"remove", `{"do":"remove","index":0,"song_id":"x"}`, 200, &Command{Do: "remove", SongID: "x"}},
		{"remove negative", `{"do":"remove","index":-2}`, 400, nil},
		{"move", `{"do":"move","from":2,"to":0,"song_id":"x"}`, 200, &Command{Do: "move", From: 2, SongID: "x"}},
		{"move negative", `{"do":"move","from":1,"to":-1}`, 400, nil},
		{"unknown", `{"do":"reboot"}`, 400, nil},
		{"empty do", `{}`, 400, nil},
		{"bad json", `{"do":`, 400, nil},
		{"not an object", `[1]`, 400, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			w := do(New(f, Options{}).Handler(), "POST", "/api/cmd", tc.body, nil)
			if w.Code != tc.code {
				t.Fatalf("code %d, want %d: %s", w.Code, tc.code, w.Body)
			}
			if tc.want == nil {
				if f.ncmds() != 0 {
					t.Fatalf("controller was called: %+v", f.cmds)
				}
				if !strings.HasPrefix(w.Body.String(), `{"error":"`) {
					t.Fatalf("error body %s", w.Body)
				}
				return
			}
			if got := f.lastCmd(t); got != *tc.want {
				t.Fatalf("got %+v, want %+v", got, *tc.want)
			}
		})
	}
}

func TestCmdErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{ErrStale, 409},
		{ErrBusy, 503},
		{errors.New("boom"), 500},
	} {
		f := newFake()
		f.doErr = tc.err
		w := do(New(f, Options{}).Handler(), "POST", "/api/cmd", `{"do":"toggle"}`, nil)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), `{"error":"`+tc.err.Error()+`"}`) {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
	}
}

func TestPlay(t *testing.T) {
	tests := []struct {
		name, body string
		code       int
		want       PlayRequest
	}{
		{"album", `{"what":"album","id":"a1","start":2,"how":"now"}`, 200, PlayRequest{What: "album", ID: "a1", Start: 2, How: "now"}},
		{"default how", `{"what":"playlist","id":"p1"}`, 200, PlayRequest{What: "playlist", ID: "p1", How: "now"}},
		{"artist end", `{"what":"artist","id":"r1","how":"end"}`, 200, PlayRequest{What: "artist", ID: "r1", How: "end"}},
		{"songs next", `{"what":"songs","ids":["a","b"],"how":"next"}`, 200, PlayRequest{What: "songs", IDs: []string{"a", "b"}, How: "next"}},
		{"album no id", `{"what":"album"}`, 400, PlayRequest{}},
		{"songs no ids", `{"what":"songs","how":"now"}`, 400, PlayRequest{}},
		{"unknown what", `{"what":"genre","id":"x"}`, 400, PlayRequest{}},
		{"unknown how", `{"what":"album","id":"x","how":"later"}`, 400, PlayRequest{}},
		{"negative start", `{"what":"album","id":"x","start":-1}`, 400, PlayRequest{}},
		{"bad json", `nope`, 400, PlayRequest{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.played = 12
			w := do(New(f, Options{}).Handler(), "POST", "/api/play", tc.body, nil)
			if w.Code != tc.code {
				t.Fatalf("code %d: %s", w.Code, w.Body)
			}
			if tc.code != 200 {
				if len(f.plays) != 0 {
					t.Fatal("controller was called")
				}
				return
			}
			if strings.TrimSpace(w.Body.String()) != `{"added":12}` {
				t.Fatal(w.Body)
			}
			if got := f.plays[0]; got.What != tc.want.What || got.ID != tc.want.ID || got.Start != tc.want.Start || got.How != tc.want.How || strings.Join(got.IDs, ",") != strings.Join(tc.want.IDs, ",") {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	f := newFake()
	f.doErr = ErrBusy
	if w := do(New(f, Options{}).Handler(), "POST", "/api/play", `{"what":"album","id":"a"}`, nil); w.Code != 503 {
		t.Fatalf("busy play: %d", w.Code)
	}
}

func TestGuards(t *testing.T) {
	f := newFake()
	var logs []string
	s := New(f, Options{Hostnames: []string{"mister"}, Log: func(format string, args ...any) { logs = append(logs, format) }})
	h := s.Handler()
	req := func(method, path, host, ctype, origin string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"do":"toggle"}`))
		r.Host = host
		if ctype != "" {
			r.Header.Set("Content-Type", ctype)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	tests := []struct {
		name                              string
		method, path, host, ctype, origin string
		code                              int
	}{
		{"ip host", "GET", "/api/state", "192.168.1.50:8080", "", "", 200},
		{"localhost", "GET", "/api/state", "localhost:8080", "", "", 200},
		{"own name", "GET", "/api/state", "mister:8080", "", "", 200},
		{"dns name", "GET", "/api/state", "evil.example:8080", "", "", 403},
		{"dns name on events", "GET", "/api/events", "evil.example", "", "", 403},
		{"post ok", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "", 200},
		{"post json with charset", "POST", "/api/cmd", "192.168.1.50:8080", "application/json; charset=utf-8", "", 200},
		{"post same origin", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "http://192.168.1.50:8080", 200},
		{"post foreign origin", "POST", "/api/cmd", "192.168.1.50:8080", "application/json", "http://evil.example", 403},
		{"post form", "POST", "/api/cmd", "192.168.1.50:8080", "application/x-www-form-urlencoded", "", 403},
		{"post text/plain", "POST", "/api/cmd", "192.168.1.50:8080", "text/plain", "", 403},
		{"post no type", "POST", "/api/cmd", "192.168.1.50:8080", "", "", 403},
		{"post dns host", "POST", "/api/play", "evil.example", "application/json", "", 403},
	}
	for _, tc := range tests {
		before := f.ncmds()
		if got := req(tc.method, tc.path, tc.host, tc.ctype, tc.origin); got != tc.code {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.code)
		}
		if tc.code == 403 && f.ncmds() != before {
			t.Errorf("%s: refused request reached the controller", tc.name)
		}
	}
	for _, l := range logs {
		if strings.Contains(l, "evil") {
			t.Errorf("log carries request data: %q", l)
		}
	}
}

func TestBodyCap(t *testing.T) {
	f := newFake()
	h := New(f, Options{}).Handler()
	big := `{"do":"toggle","mode":"` + strings.Repeat("x", 70<<10) + `"}`
	if w := do(h, "POST", "/api/cmd", big, nil); w.Code != 413 {
		t.Fatalf("oversized body: %d", w.Code)
	}
	if f.ncmds() != 0 {
		t.Fatal("oversized body reached the controller")
	}
	ok := `{"do":"toggle","mode":"` + strings.Repeat("x", 60<<10) + `"}`
	if w := do(h, "POST", "/api/cmd", ok, nil); w.Code != 200 {
		t.Fatalf("body under the cap: %d", w.Code)
	}
}

func TestRejectLogRateLimit(t *testing.T) {
	var logs []string
	s := New(newFake(), Options{Log: func(format string, args ...any) { logs = append(logs, format) }})
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	bad := func() { do(s.Handler(), "POST", "/api/cmd", "{}", map[string]string{"Content-Type": "text/plain"}) }
	bad()
	bad()
	if len(logs) != 1 {
		t.Fatalf("%d log lines for one reason within a minute", len(logs))
	}
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "evil.example"
	s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	if len(logs) != 2 {
		t.Fatalf("a second reason should log: %d", len(logs))
	}
	now = now.Add(61 * time.Second)
	bad()
	if len(logs) != 3 {
		t.Fatalf("after a minute it should log again: %d", len(logs))
	}
}

func TestUnknownRoutes(t *testing.T) {
	h := New(newFake(), Options{}).Handler()
	if w := do(h, "GET", "/api/nothing", "", nil); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if w := do(h, "GET", "/api/cmd", "", nil); w.Code != 405 {
		t.Fatalf("GET on cmd: %d", w.Code)
	}
}

func TestURLs(t *testing.T) {
	s := New(newFake(), Options{})
	if len(s.URLs()) != 0 {
		t.Fatal("URLs before Listen")
	}
	s.ifaces = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
			&net.IPNet{IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("10.0.0.7"), Mask: net.CIDRMask(8, 32)},
			&net.UnixAddr{Name: "x", Net: "unix"},
		}, nil
	}
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	port := s.Addr().(*net.TCPAddr).Port
	got := strings.Join(s.URLs(), " ")
	want := "http://192.168.1.50:" + strconv.Itoa(port) + "/ http://10.0.0.7:" + strconv.Itoa(port) + "/"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	s.ifaces = func() ([]net.Addr, error) { return nil, errors.New("no interfaces") }
	if len(s.URLs()) != 0 {
		t.Fatal("URLs with a failing lister")
	}
}

func TestListenServes(t *testing.T) {
	s := New(newFake(), Options{})
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://" + s.Addr().String() + "/api/state")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp)
	}
	resp.Body.Close()
	if err := s.Listen("127.0.0.1:0"); err == nil {
		t.Fatal("second Listen should fail")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + s.Addr().String() + "/api/state"); err == nil {
		t.Fatal("still serving after Close")
	}
	if err := s.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
}

// ---- SSE ----

type event struct{ name, data string }

type stream struct {
	resp *http.Response
	ev   chan event
	all  chan string // every raw line
}

func open(t *testing.T, url string) *stream {
	t.Helper()
	resp, err := http.Get(url + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	st := &stream{resp: resp, ev: make(chan event, 100), all: make(chan string, 1000)}
	go func() {
		defer close(st.ev)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 4<<20)
		var name, data string
		for sc.Scan() {
			l := sc.Text()
			select {
			case st.all <- l:
			default:
			}
			switch {
			case strings.HasPrefix(l, "event: "):
				name = l[7:]
			case strings.HasPrefix(l, "data: "):
				data = l[6:]
			case l == "":
				if name != "" {
					st.ev <- event{name, data}
				}
				name, data = "", ""
			}
		}
	}()
	t.Cleanup(func() { resp.Body.Close() })
	return st
}

func (s *stream) next(t *testing.T) event {
	t.Helper()
	select {
	case e, ok := <-s.ev:
		if !ok {
			t.Fatal("stream ended")
		}
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
	return event{}
}

func (s *stream) none(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case e := <-s.ev:
		t.Fatalf("unexpected event %+v", e)
	case <-time.After(d):
	}
}

func startServer(t *testing.T, f *fakeCtl, o Options) (*Server, string) {
	t.Helper()
	s := New(f, o)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { s.Close() })
	return s, ts.URL
}

func waitClients(t *testing.T, s *Server, n int) {
	t.Helper()
	for i := 0; i < 300; i++ {
		s.mu.Lock()
		c := len(s.clients)
		s.mu.Unlock()
		if c == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("clients never reached %d", n)
}

func TestSSEInitialAndNotify(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})
	st := open(t, url)
	if ct := st.resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"status":"paused"`) {
		t.Fatalf("first: %+v", e)
	}
	if e := st.next(t); e.name != "queue" || !strings.Contains(e.data, `"songs":[{"id":"s1"`) {
		t.Fatalf("second: %+v", e)
	}
	waitClients(t, s, 1)

	f.setState(State{Status: "playing", Repeat: "off"})
	s.Notify(StateChanged)
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"status":"playing"`) {
		t.Fatalf("after notify: %+v", e)
	}
	f.mu.Lock()
	f.queue = QueueView{Index: 0, Songs: []Song{}}
	f.mu.Unlock()
	s.Notify(QueueChanged)
	if e := st.next(t); e.name != "queue" || e.data != `{"index":0,"songs":[]}` {
		t.Fatalf("queue notify: %+v", e)
	}
	st.none(t, 50*time.Millisecond) // a queue change sends no state event
}

func TestSSEBroadcastToAll(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})
	a, b := open(t, url), open(t, url)
	for _, st := range []*stream{a, b} {
		st.next(t)
		st.next(t)
	}
	waitClients(t, s, 2)
	s.Notify(StateChanged)
	if a.next(t).name != "state" || b.next(t).name != "state" {
		t.Fatal("not every client got the event")
	}
}

func TestSSETickOnlyWhilePlaying(t *testing.T) {
	f := newFake() // paused
	s, url := startServer(t, f, Options{Tick: 20 * time.Millisecond, Heartbeat: time.Hour})
	st := open(t, url)
	st.next(t)
	st.next(t)
	waitClients(t, s, 1)
	st.none(t, 150*time.Millisecond)

	f.setState(State{Status: "playing", PositionMS: 42, Repeat: "off"})
	if e := st.next(t); e.name != "state" || !strings.Contains(e.data, `"position_ms":42`) {
		t.Fatalf("tick: %+v", e)
	}
	st.next(t)
	st.next(t)

	f.setState(State{Status: "paused", Repeat: "off"})
	for len(st.ev) > 0 { // drop ticks already in flight
		<-st.ev
	}
	time.Sleep(50 * time.Millisecond)
	for len(st.ev) > 0 {
		<-st.ev
	}
	st.none(t, 150*time.Millisecond)
}

func TestSSEHeartbeat(t *testing.T) {
	_, url := startServer(t, newFake(), Options{Tick: time.Hour, Heartbeat: 20 * time.Millisecond})
	st := open(t, url)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case l := <-st.all:
			if strings.HasPrefix(l, ":") {
				return
			}
		case <-deadline:
			t.Fatal("no heartbeat comment")
		}
	}
}

func TestSSECapAndDisconnect(t *testing.T) {
	f := newFake()
	s, url := startServer(t, f, Options{MaxClients: 2, Tick: time.Hour, Heartbeat: time.Hour})
	a, b := open(t, url), open(t, url)
	a.next(t)
	b.next(t)
	waitClients(t, s, 2)

	resp, err := http.Get(url + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 503 || !strings.Contains(string(body), `"error"`) {
		t.Fatalf("over the cap: %d %s", resp.StatusCode, body)
	}

	a.resp.Body.Close() // a disconnects
	waitClients(t, s, 1)
	c := open(t, url)
	if e := c.next(t); e.name != "state" {
		t.Fatalf("the freed slot: %+v", e)
	}
}

func TestSSECloseEndsStreams(t *testing.T) {
	s, url := startServer(t, newFake(), Options{Tick: time.Hour, Heartbeat: time.Hour})
	st := open(t, url)
	st.next(t)
	st.next(t)
	waitClients(t, s, 1)
	s.Close()
	select {
	case _, ok := <-st.ev:
		if ok {
			t.Fatal("event after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open after Close")
	}
	waitClients(t, s, 0)
	if resp, err := http.Get(url + "/api/events"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Fatalf("stream opened after Close: %d", resp.StatusCode)
		}
	}
}

func TestNotifyNeverBlocksOnASlowClient(t *testing.T) {
	f := newFake()
	f.queue = QueueView{Songs: make([]Song, 2000)} // big events fill the socket buffers quickly
	s, url := startServer(t, f, Options{Tick: time.Hour, Heartbeat: time.Hour})

	conn, err := net.Dial("tcp", strings.TrimPrefix(url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(*net.TCPConn).SetReadBuffer(1024)
	io.WriteString(conn, "GET /api/events HTTP/1.1\r\nHost: localhost\r\n\r\n") // and never read
	waitClients(t, s, 1)

	fast := open(t, url)
	fast.next(t)
	fast.next(t)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			s.Notify(QueueChanged)
			s.Notify(StateChanged)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Notify blocked on a client that is not reading")
	}
	s.Notify(StateChanged)
	for { // the reading client still gets the latest state
		if e := fast.next(t); e.name == "state" {
			break
		}
	}
}

func TestNotifyWithoutClientsIsCheap(t *testing.T) {
	s := New(newFake(), Options{})
	for i := 0; i < 100000; i++ {
		s.Notify(StateChanged)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/remote`

Expected: FAIL, e.g.:

```
undefined: State
undefined: QueueView
undefined: Command
undefined: PlayRequest
undefined: Library
```

- [ ] **Step 3: Implement**

`internal/remote/api.go` (new file):

```go
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"mistersubsonic/internal/subsonic"
)

// ErrStale is what Controller.Do returns when a queue command names an index
// that is out of range or no longer holds the song the page saw (409).
var ErrStale = errors.New("remote: the queue changed")

// ErrBusy is what the Controller returns when the UI did not answer in time (503).
var ErrBusy = errors.New("remote: the MiSTer is busy")

// Controller is everything the server needs from the app. Tests use fakes.
type Controller interface {
	State() State
	Queue() QueueView
	// Do runs a playback or queue command and returns when it has run or been refused.
	Do(c Command) error
	// Play resolves the request to songs, starts or queues them, and returns how many.
	Play(p PlayRequest) (int, error)
	Library() Library
	// Cover returns the image bytes and content type; the bytes pass through undecoded.
	Cover(ctx context.Context, id string, size int) ([]byte, string, error)
}

// Library is the read-only part of the app's Subsonic client the browse and
// search routes use.
type Library interface {
	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
	GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error)
	GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error)
	GetGenres(ctx context.Context) ([]subsonic.Genre, error)
	GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error)
	GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error)
	GetStarred2(ctx context.Context) (*subsonic.Starred, error)
	Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error)
}

// Song is what the page knows about a song, and nothing else.
type Song struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	CoverID    string `json:"cover_id"`
	DurationMS int64  `json:"duration_ms"`
	Starred    bool   `json:"starred"`
}

// State is a snapshot of the player. Song is nil when nothing is loaded;
// Status is playing, paused, loading, buffering or stopped; Repeat is off,
// all or one.
type State struct {
	Song       *Song   `json:"song"`
	Status     string  `json:"status"`
	PositionMS int64   `json:"position_ms"`
	DurationMS int64   `json:"duration_ms"`
	VolumeDB   float64 `json:"volume_db"`
	Muted      bool    `json:"muted"`
	Shuffle    bool    `json:"shuffle"`
	Repeat     string  `json:"repeat"`
	Index      int     `json:"index"`
}

// QueueView is the queue and the index of the current song in it.
type QueueView struct {
	Index int    `json:"index"`
	Songs []Song `json:"songs"`
}

// Command is one POST /api/cmd body. Which fields count depends on Do.
// SongID is the id of the song the page saw at Index (jump, remove) or at
// From (move); the Controller answers ErrStale when it no longer matches.
type Command struct {
	Do         string  `json:"do"`
	PositionMS int64   `json:"position_ms"`
	DB         float64 `json:"db"`
	On         bool    `json:"on"`
	Mode       string  `json:"mode"`
	Index      int     `json:"index"`
	From       int     `json:"from"`
	To         int     `json:"to"`
	SongID     string  `json:"song_id"`
}

// PlayRequest is one POST /api/play body. What is album, playlist, artist
// (all its albums in order) or songs; How is now, next or end; Start is the
// song to begin at, for now.
type PlayRequest struct {
	What  string   `json:"what"`
	ID    string   `json:"id"`
	IDs   []string `json:"ids"`
	Start int      `json:"start"`
	How   string   `json:"how"`
}

// Volume range of a command, in dB.
const (
	minVolumeDB = -60
	maxPlayIDs  = 1000
)

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ctl.State())
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, queueOrEmpty(s.ctl.Queue()))
}

// queueOrEmpty makes the songs an empty list, not null, for the page.
func queueOrEmpty(q QueueView) QueueView {
	if q.Songs == nil {
		q.Songs = []Song{}
	}
	return q
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	var c Command
	if !readBody(w, r, &c) {
		return
	}
	if err := validate(&c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.ctl.Do(c); err != nil {
		writeCtlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// validate checks the fields the command uses and clamps the volume.
func validate(c *Command) error {
	switch c.Do {
	case "toggle", "next", "prev", "mute", "shuffle", "star", "clear":
	case "seek":
		if c.PositionMS < 0 {
			return errors.New("position_ms must not be negative")
		}
	case "volume":
		if math.IsNaN(c.DB) {
			return errors.New("db is not a number")
		}
		c.DB = math.Max(minVolumeDB, math.Min(0, c.DB))
	case "repeat":
		if c.Mode != "off" && c.Mode != "all" && c.Mode != "one" {
			return errors.New("mode must be off, all or one")
		}
	case "jump", "remove":
		if c.Index < 0 {
			return errors.New("index must not be negative")
		}
	case "move":
		if c.From < 0 || c.To < 0 {
			return errors.New("from and to must not be negative")
		}
	default:
		return fmt.Errorf("unknown command %q", c.Do)
	}
	return nil
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	var p PlayRequest
	if !readBody(w, r, &p) {
		return
	}
	if p.How == "" {
		p.How = "now"
	}
	if err := validatePlay(&p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.ctl.Play(p)
	if err != nil {
		writeCtlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": n})
}

func validatePlay(p *PlayRequest) error {
	switch p.What {
	case "album", "playlist", "artist":
		if p.ID == "" {
			return errors.New("id is required")
		}
	case "songs":
		if len(p.IDs) == 0 || len(p.IDs) > maxPlayIDs {
			return fmt.Errorf("ids must hold 1 to %d song ids", maxPlayIDs)
		}
	default:
		return fmt.Errorf("unknown what %q", p.What)
	}
	switch p.How {
	case "now", "next", "end":
	default:
		return fmt.Errorf("unknown how %q", p.How)
	}
	if p.Start < 0 {
		return errors.New("start must not be negative")
	}
	return nil
}

// readBody decodes the JSON body into v, or answers 400 (413 when the body is
// over the cap) and reports false.
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

func writeCtlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrStale):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrBusy):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		code, b = http.StatusInternalServerError, []byte(`{"error":"encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(append(b, '\n'))
}
```

`internal/remote/server.go` (new file):

```go
// Package remote is the web remote: a small HTTP server that shows what is
// playing, takes playback and queue commands, and streams live updates to
// every open page. It reaches the app only through Controller.
package remote

import (
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"mistersubsonic/internal/webguard"
)

// Options configure a Server; zero values take the defaults.
type Options struct {
	MaxClients int           // open event streams allowed; default 8
	Tick       time.Duration // position event period while playing; default 1 s
	Heartbeat  time.Duration // comment line period on idle streams; default 15 s
	Hostnames  []string      // extra Host names accepted besides IPs and localhost
	Log        func(format string, args ...any)
}

// Change says what Notify is telling the streams about.
type Change int

const (
	StateChanged Change = iota
	QueueChanged
)

const (
	maxBody      = 64 << 10
	writeTimeout = 10 * time.Second // a stalled stream is dropped after this
	logEvery     = time.Minute
)

// Server is the web remote.
type Server struct {
	ctl  Controller
	opts Options
	mux  *http.ServeMux

	mu      sync.Mutex
	srv     *http.Server
	ln      net.Listener
	clients map[*client]struct{}
	nclient atomic.Int32 // len(clients), for Notify's cheap path
	done    chan struct{}
	closed  bool

	logMu   sync.Mutex
	lastLog map[string]time.Time

	now    func() time.Time
	ifaces func() ([]net.Addr, error)
}

// client is one event stream. Notify only sets flags and wakes it; the
// stream's own goroutine reads the latest state when it writes, so a slow
// client skips intermediate events and never blocks anyone else.
type client struct {
	wake  chan struct{}
	state atomic.Bool
	queue atomic.Bool
}

// New returns a Server for ctl. Nothing listens until Listen.
func New(ctl Controller, opts Options) *Server {
	if opts.MaxClients <= 0 {
		opts.MaxClients = 8
	}
	if opts.Tick <= 0 {
		opts.Tick = time.Second
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}
	s := &Server{
		ctl:     ctl,
		opts:    opts,
		mux:     http.NewServeMux(),
		clients: map[*client]struct{}{},
		done:    make(chan struct{}),
		lastLog: map[string]time.Time{},
		now:     time.Now,
		ifaces:  net.InterfaceAddrs,
	}
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /api/queue", s.handleQueue)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/cmd", s.handleCmd)
	s.mux.HandleFunc("POST /api/play", s.handlePlay)
	return s
}

// Handler is the whole server, guards included.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.guarded) }

// guarded applies the checks every route needs: the Host header against DNS
// rebinding, and for POSTs a JSON content type and a same-origin Origin
// against cross-site requests, then caps the body.
func (s *Server) guarded(w http.ResponseWriter, r *http.Request) {
	if !webguard.HostAllowed(r.Host, s.opts.Hostnames...) {
		s.reject(w, "unexpected Host header")
		return
	}
	if r.Method == http.MethodPost {
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			s.reject(w, "POST without a JSON content type")
			return
		}
		if !webguard.SameOrigin(r) {
			s.reject(w, "POST from another origin")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	s.mux.ServeHTTP(w, r)
}

// reject answers 403 and logs the reason, at most once a minute per reason.
// It never logs anything the request carried.
func (s *Server) reject(w http.ResponseWriter, reason string) {
	writeError(w, http.StatusForbidden, reason)
	if s.opts.Log == nil {
		return
	}
	s.logMu.Lock()
	now := s.now()
	quiet := now.Sub(s.lastLog[reason]) < logEvery
	if !quiet {
		s.lastLog[reason] = now
	}
	s.logMu.Unlock()
	if !quiet {
		s.opts.Log("remote: refused a request: %s", reason)
	}
}

// Listen starts serving on addr in a goroutine.
func (s *Server) Listen(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return http.ErrServerClosed
	}
	if s.ln != nil {
		return fmt.Errorf("remote: already listening on %s", s.ln.Addr())
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	s.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go s.srv.Serve(ln)
	return nil
}

// Addr is the listening address, or nil before Listen.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// URLs lists http://<ip>:<port>/ for each non-loopback IPv4 address of the
// machine; it is empty before Listen.
func (s *Server) URLs() []string {
	a, ok := s.Addr().(*net.TCPAddr)
	if !ok {
		return nil
	}
	addrs, err := s.ifaces()
	if err != nil {
		return nil
	}
	var urls []string
	for _, ad := range addrs {
		ipn, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipn.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		urls = append(urls, "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(a.Port))+"/")
	}
	return urls
}

// Close stops listening and ends every event stream. It is safe to call twice.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	if s.srv != nil {
		return s.srv.Close()
	}
	return nil
}

// Notify tells every open stream that the state or queue changed. It never
// blocks, and costs one atomic load when nobody is connected.
func (s *Server) Notify(c Change) {
	if s.nclient.Load() == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for cl := range s.clients {
		if c == QueueChanged {
			cl.queue.Store(true)
		} else {
			cl.state.Store(true)
		}
		select {
		case cl.wake <- struct{}{}:
		default: // already woken; it will see the flag
		}
	}
}

// join registers a stream, or reports false when the server is full or closed.
func (s *Server) join() (*client, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.clients) >= s.opts.MaxClients {
		return nil, false
	}
	c := &client{wake: make(chan struct{}, 1)}
	s.clients[c] = struct{}{}
	s.nclient.Store(int32(len(s.clients)))
	return c, true
}

func (s *Server) leave(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.nclient.Store(int32(len(s.clients)))
	s.mu.Unlock()
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	c, ok := s.join()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "too many open remotes")
		return
	}
	defer s.leave(c)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	sendState := func() bool { return send("state", s.ctl.State()) }
	sendQueue := func() bool { return send("queue", queueOrEmpty(s.ctl.Queue())) }

	if !sendState() || !sendQueue() {
		return
	}
	tick := time.NewTicker(s.opts.Tick)
	defer tick.Stop()
	beat := time.NewTicker(s.opts.Heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-c.wake:
			if c.queue.Swap(false) && !sendQueue() {
				return
			}
			if c.state.Swap(false) && !sendState() {
				return
			}
		case <-tick.C:
			if st := s.ctl.State(); st.Status == "playing" && !send("state", st) {
				return
			}
		case <-beat.C:
			rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/remote`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/remote/api.go internal/remote/remote_test.go internal/remote/server.go
git commit -m "remote: an HTTP server for the web remote: state, live events, commands and play, behind Host and Origin guards" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Browse, search, covers

**Files:**
- Create: `internal/remote/browse.go`
- Modify: `internal/remote/server.go`
- Test: `internal/remote/browse_test.go` (new)

**Interfaces:**
- **Consumes:** `Controller.Library()` and `Controller.Cover` (Task 2).
- **Produces:**
  - **Browse routes:**
    - `GET /api/artists`, grouped by letter, and `/api/artist/{id}`;
    - `/api/albums?list=recent|random|newest&offset=N`, 50 per page with `more`, and `/api/album/{id}`;
    - `/api/playlists` and `/api/playlist/{id}`;
    - `/api/starred`;
    - `/api/genres` and `/api/genre/{name}?offset=N`.
  - **`GET /api/search?q=`:** at least 2 characters, returning 20 artists, 20 albums and 50 songs.
  - **`GET /api/cover/{id}?size=N`:** size clamped to 64–600 (default 300). The bytes and type pass through, with `max-age=86400` and nosniff on success.
  - **Library calls** have a 10 s timeout from the request.
  - **Errors:** the body is the kind text only. Not found gives 404; unreachable, timeout, TLS or auth gives 503; anything else gives 500 "the library failed". No library gives 503.

- [ ] **Step 1: Write the failing tests**

`internal/remote/browse_test.go` (new file):

```go
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

// fakeLib answers with fixed data, records the queries it got, and returns err
// (when set) from every call. block makes a call wait for its context.
type fakeLib struct {
	mu     sync.Mutex
	err    error
	block  bool
	albums []subsonic.Album
	lastAL subsonic.AlbumListQuery
	lastQ  string
	lastSQ subsonic.SearchQuery
}

func (f *fakeLib) fail(ctx context.Context) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

var (
	sSong  = subsonic.Song{ID: "s1", Title: "T", Artist: "A", Album: "B", CoverArt: "c9", Duration: 200, Starred: "2024-01-01"}
	sAlbum = subsonic.Album{ID: "al1", Name: "Alb", Artist: "Art", ArtistID: "ar1", CoverArt: "c1", Year: 1999, SongCount: 2}
	sArt   = subsonic.Artist{ID: "ar1", Name: "Art", AlbumCount: 3, CoverArt: "c2"}
)

func (f *fakeLib) GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
	f.mu.Lock()
	f.lastAL = q
	f.mu.Unlock()
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	if f.albums != nil {
		return f.albums, nil
	}
	return []subsonic.Album{sAlbum}, nil
}
func (f *fakeLib) GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.AlbumWithSongs{Album: sAlbum, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.ArtistIndex{{Name: "A", Artists: subsonic.List[subsonic.Artist]{sArt}}, {Name: "B"}}, nil
}
func (f *fakeLib) GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.ArtistWithAlbums{Artist: sArt, Albums: subsonic.List[subsonic.Album]{sAlbum}}, nil
}
func (f *fakeLib) GetGenres(ctx context.Context) ([]subsonic.Genre, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.Genre{{Name: "Rock", SongCount: 10, AlbumCount: 2}}, nil
}
func (f *fakeLib) GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return []subsonic.Playlist{{ID: "p1", Name: "Mix", SongCount: 4, CoverArt: "c3"}}, nil
}
func (f *fakeLib) GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.PlaylistWithSongs{Playlist: subsonic.Playlist{ID: "p1", Name: "Mix", SongCount: 1}, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) GetStarred2(ctx context.Context) (*subsonic.Starred, error) {
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.Starred{Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}
func (f *fakeLib) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
	f.mu.Lock()
	f.lastQ, f.lastSQ = query, q
	f.mu.Unlock()
	if err := f.fail(ctx); err != nil {
		return nil, err
	}
	return &subsonic.SearchResult{Artists: subsonic.List[subsonic.Artist]{sArt}, Songs: subsonic.List[subsonic.Song]{sSong}}, nil
}

// libCtl is a fakeCtl with a Library and a Cover.
type libCtl struct {
	*fakeCtl
	lib   Library
	cover func(ctx context.Context, id string, size int) ([]byte, string, error)
	size  int
}

func (c *libCtl) Library() Library { return c.lib }
func (c *libCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	c.size = size
	return c.cover(ctx, id, size)
}

func browse(lib Library) (*Server, *libCtl) {
	c := &libCtl{fakeCtl: newFake(), lib: lib}
	s := New(c, Options{})
	s.libTimeout = 200 * time.Millisecond
	return s, c
}

func get(s *Server, path string) (int, string) {
	w := do(s.Handler(), "GET", path, "", nil)
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestBrowseJSON(t *testing.T) {
	s, _ := browse(&fakeLib{})
	songJSON := `{"id":"s1","title":"T","artist":"A","album":"B","cover_id":"c9","duration_ms":200000,"starred":true}`
	albJSON := `{"id":"al1","name":"Alb","artist":"Art","artist_id":"ar1","year":1999,"cover_id":"c1","song_count":2}`
	cases := []struct{ path, want string }{
		{"/api/artists", `[{"letter":"A","artists":[{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2"}]},{"letter":"B","artists":[]}]`},
		{"/api/artist/ar1", `{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2","albums":[` + albJSON + `]}`},
		{"/api/albums?list=recent", `{"albums":[` + albJSON + `],"more":false}`},
		{"/api/album/al1", `{"id":"al1","name":"Alb","artist":"Art","artist_id":"ar1","year":1999,"cover_id":"c1","song_count":2,"songs":[` + songJSON + `]}`},
		{"/api/playlists", `[{"id":"p1","name":"Mix","song_count":4,"cover_id":"c3"}]`},
		{"/api/playlist/p1", `{"id":"p1","name":"Mix","song_count":1,"cover_id":"","songs":[` + songJSON + `]}`},
		{"/api/starred", `{"artists":[],"albums":[],"songs":[` + songJSON + `]}`},
		{"/api/genres", `[{"name":"Rock","album_count":2,"song_count":10}]`},
		{"/api/genre/Rock", `{"albums":[` + albJSON + `],"more":false}`},
		{"/api/search?q=ab", `{"artists":[{"id":"ar1","name":"Art","album_count":3,"cover_id":"c2"}],"albums":[],"songs":[` + songJSON + `]}`},
	}
	for _, c := range cases {
		if code, body := get(s, c.path); code != 200 || body != c.want {
			t.Errorf("%s: %d\n got  %s\n want %s", c.path, code, body, c.want)
		}
	}
}

func TestAlbumListTypesAndPaging(t *testing.T) {
	lib := &fakeLib{}
	s, _ := browse(lib)
	for list, typ := range map[string]string{"recent": subsonic.ListRecent, "random": subsonic.ListRandom, "newest": subsonic.ListNewest} {
		if code, _ := get(s, "/api/albums?list="+list+"&offset=100"); code != 200 {
			t.Fatalf("%s: %d", list, code)
		}
		if q := lib.lastAL; q.Type != typ || q.Offset != 100 || q.Size != 51 {
			t.Errorf("%s: query %+v", list, q)
		}
	}
	if code, _ := get(s, "/api/genre/Hip%20Hop?offset=50"); code != 200 {
		t.Fatal(code)
	}
	if q := lib.lastAL; q.Type != subsonic.ListByGenre || q.Genre != "Hip Hop" || q.Offset != 50 {
		t.Errorf("genre query %+v", q)
	}
	// 51 back means a next page: the 51st is not sent.
	for i := 0; i < 51; i++ {
		lib.albums = append(lib.albums, subsonic.Album{ID: subsonic.ID(fmt.Sprint(i))})
	}
	_, body := get(s, "/api/albums?list=newest")
	if !strings.HasSuffix(body, `"more":true}`) || strings.Count(body, `"id"`) != 50 {
		t.Fatalf("full page: %d ids, %s", strings.Count(body, `"id"`), body[len(body)-20:])
	}
	lib.albums = lib.albums[:50]
	if _, body := get(s, "/api/albums?list=newest"); !strings.HasSuffix(body, `"more":false}`) {
		t.Fatal("exactly 50 must not say more")
	}
	lib.albums = []subsonic.Album{}
	if _, body := get(s, "/api/albums?list=newest"); body != `{"albums":[],"more":false}` {
		t.Fatal(body)
	}
}

func TestBrowseBadParams(t *testing.T) {
	s, _ := browse(&fakeLib{})
	for _, p := range []string{
		"/api/albums", "/api/albums?list=frequent", "/api/albums?list=recent&offset=-1",
		"/api/albums?list=recent&offset=x", "/api/genre/Rock?offset=-5",
		"/api/search", "/api/search?q=a", "/api/search?q=%20a%20", "/api/search?q=%C3%A9",
	} {
		if code, body := get(s, p); code != 400 || !strings.HasPrefix(body, `{"error":`) {
			t.Errorf("%s: %d %s", p, code, body)
		}
	}
}

func TestSearchCounts(t *testing.T) {
	lib := &fakeLib{}
	s, _ := browse(lib)
	if code, _ := get(s, "/api/search?q=%20%20Bj%C3%B6rk%20"); code != 200 {
		t.Fatal(code)
	}
	if lib.lastQ != "Björk" || lib.lastSQ != (subsonic.SearchQuery{ArtistCount: 20, AlbumCount: 20, SongCount: 50}) {
		t.Fatalf("%q %+v", lib.lastQ, lib.lastSQ)
	}
	// two characters, counted as characters, are enough
	if code, _ := get(s, "/api/search?q=%C3%A9%C3%A9"); code != 200 {
		t.Fatal(code)
	}
}

func TestBrowseErrors(t *testing.T) {
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused to https://music.example/rest/ping?u=bob&t=abc&s=xyz")}
	cases := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{"unreachable", netErr, 503, "server unreachable"},
		{"not found", &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no such thing at ?u=bob&t=abc"}, 404, "not found"},
		{"auth", &subsonic.APIError{Code: subsonic.CodeWrongCredentials, Message: "bad u=bob t=abc"}, 503, "wrong credentials"},
		{"other", errors.New("Get \"https://h/rest/getAlbum?u=bob&t=abc&s=xyz&id=1\": weird"), 500, "the library failed"},
	}
	paths := []string{"/api/artists", "/api/artist/x", "/api/albums?list=recent", "/api/album/x", "/api/playlists",
		"/api/playlist/x", "/api/starred", "/api/genres", "/api/genre/x", "/api/search?q=abc"}
	for _, c := range cases {
		s, _ := browse(&fakeLib{err: c.err})
		for _, p := range paths {
			code, body := get(s, p)
			if code != c.code || body != `{"error":"`+c.msg+`"}` {
				t.Errorf("%s %s: %d %s", c.name, p, code, body)
			}
			for _, leak := range []string{"u=", "t=", "s=", "http", "bob", "abc"} {
				if strings.Contains(body, leak) {
					t.Errorf("%s %s leaks %q: %s", c.name, p, leak, body)
				}
			}
		}
	}
}

func TestBrowseTimeoutAndNoLibrary(t *testing.T) {
	s, _ := browse(&fakeLib{block: true})
	start := time.Now()
	code, body := get(s, "/api/artists")
	if code != 503 || body != `{"error":"timed out"}` {
		t.Fatalf("%d %s", code, body)
	}
	if d := time.Since(start); d < 150*time.Millisecond || d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	s, _ = browse(nil)
	if code, body := get(s, "/api/artists"); code != 503 || !strings.Contains(body, "no library") {
		t.Fatalf("%d %s", code, body)
	}
	if New(newFake(), Options{}).libTimeout != 10*time.Second {
		t.Fatal("default timeout is not 10 s")
	}
}

func TestCover(t *testing.T) {
	s, c := browse(&fakeLib{})
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		if id == "none" {
			return nil, "", &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "u=bob"}
		}
		return []byte("\xff\xd8jpeg"), "image/jpeg", nil
	}
	w := do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 200 || w.Body.String() != "\xff\xd8jpeg" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("%d %q %v", w.Code, w.Body, w.Header())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "max-age=86400" {
		t.Fatalf("Cache-Control %q", cc)
	}
	for q, want := range map[string]int{"": 300, "?size=10": 64, "?size=64": 64, "?size=200": 200, "?size=600": 600, "?size=5000": 600, "?size=-3": 64, "?size=abc": 300} {
		do(s.Handler(), "GET", "/api/cover/c1"+q, "", nil)
		if c.size != want {
			t.Errorf("size%s: got %d want %d", q, c.size, want)
		}
	}
	w = do(s.Handler(), "GET", "/api/cover/none", "", nil)
	if w.Code != 404 || strings.TrimSpace(w.Body.String()) != `{"error":"no cover"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") == "max-age=86400" {
		t.Fatal("an error must not be cached")
	}
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return nil, "", errors.New("Get https://h/rest/getCoverArt?u=bob&t=abc")
	}
	w = do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 404 || strings.Contains(w.Body.String(), "u=") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	c.cover = func(ctx context.Context, id string, size int) ([]byte, string, error) {
		return nil, "", &net.OpError{Op: "dial", Err: errors.New("u=bob")}
	}
	w = do(s.Handler(), "GET", "/api/cover/c1", "", nil)
	if w.Code != 503 || strings.Contains(w.Body.String(), "u=") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestBrowseGuarded(t *testing.T) {
	s, _ := browse(&fakeLib{})
	r := do(s.Handler(), "GET", "/api/artists", "", nil)
	if r.Code != http.StatusOK {
		t.Fatal(r.Code)
	}
	r = do(s.Handler(), "GET", "/", "", nil)
	if r.Code != 404 {
		t.Fatalf("page route: %d", r.Code)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/remote`

Expected: FAIL, e.g.:

```
s.libTimeout undefined (type *Server has no field or method libTimeout)
New(newFake(), Options{}).libTimeout undefined (type *Server has no field or method libTimeout)
```

- [ ] **Step 3: Implement**

`internal/remote/browse.go` (new file):

```go
package remote

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"mistersubsonic/internal/subsonic"
)

// The browse, search and cover routes. They are read-only and call the
// Library from the request goroutine, each call under libTimeout.
const (
	defaultLibTimeout = 10 * time.Second
	albumPage         = 50
	minSearch         = 2
	searchArtists     = 20
	searchAlbums      = 20
	searchSongs       = 50
	minCover          = 64
	maxCover          = 600
	defaultCover      = 300
)

type artistJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
	CoverID    string `json:"cover_id"`
}

type albumJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artist_id"`
	Year      int    `json:"year"`
	CoverID   string `json:"cover_id"`
	SongCount int    `json:"song_count"`
}

type playlistJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"song_count"`
	CoverID   string `json:"cover_id"`
}

type letterJSON struct {
	Letter  string       `json:"letter"`
	Artists []artistJSON `json:"artists"`
}

type genreJSON struct {
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
	SongCount  int    `json:"song_count"`
}

func artistOf(a subsonic.Artist) artistJSON {
	return artistJSON{string(a.ID), a.Name, a.AlbumCount, string(a.CoverArt)}
}

func albumOf(a subsonic.Album) albumJSON {
	return albumJSON{string(a.ID), a.Name, a.Artist, string(a.ArtistID), a.Year, string(a.CoverArt), a.SongCount}
}

func songOf(s subsonic.Song) Song {
	return Song{
		ID: string(s.ID), Title: s.Title, Artist: s.Artist, Album: s.Album,
		CoverID: string(s.CoverArt), DurationMS: int64(s.Duration) * 1000, Starred: s.Starred != "",
	}
}

// Each of these returns a non-nil slice, so the page gets [] and never null.
func artistsOf(l []subsonic.Artist) []artistJSON {
	out := make([]artistJSON, len(l))
	for i, a := range l {
		out[i] = artistOf(a)
	}
	return out
}

func albumsOf(l []subsonic.Album) []albumJSON {
	out := make([]albumJSON, len(l))
	for i, a := range l {
		out[i] = albumOf(a)
	}
	return out
}

func songsOf(l []subsonic.Song) []Song {
	out := make([]Song, len(l))
	for i, s := range l {
		out[i] = songOf(s)
	}
	return out
}

// lib returns the Library and a context with the library timeout, or answers
// 503 and reports false when the app has no Subsonic client.
func (s *Server) lib(w http.ResponseWriter, r *http.Request) (Library, context.Context, context.CancelFunc, bool) {
	l := s.ctl.Library()
	if l == nil {
		writeError(w, http.StatusServiceUnavailable, "no library")
		return nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.libTimeout)
	return l, ctx, cancel, true
}

// writeLibError answers a failed library call. The message is only ever the
// error's kind, never the error text, which can hold the server URL and the
// credentials the client put in it.
func writeLibError(w http.ResponseWriter, err error) {
	switch k := subsonic.Classify(err); k {
	case subsonic.KindNotFound:
		writeError(w, http.StatusNotFound, k.String())
	case subsonic.KindOther:
		writeError(w, http.StatusInternalServerError, "the library failed")
	default:
		writeError(w, http.StatusServiceUnavailable, k.String())
	}
}

// libJSON runs one library call and writes its result, or the error.
func libJSON[T any](s *Server, w http.ResponseWriter, r *http.Request, call func(context.Context, Library) (T, error)) {
	l, ctx, cancel, ok := s.lib(w, r)
	if !ok {
		return
	}
	defer cancel()
	v, err := call(ctx, l)
	if err != nil {
		writeLibError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]letterJSON, error) {
		idx, err := l.GetArtists(ctx)
		out := make([]letterJSON, len(idx))
		for i, x := range idx {
			out[i] = letterJSON{x.Name, artistsOf(x.Artists)}
		}
		return out, err
	})
}

func (s *Server) handleArtist(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		a, err := l.GetArtist(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			artistJSON
			Albums []albumJSON `json:"albums"`
		}{artistOf(a.Artist), albumsOf(a.Albums)}, nil
	})
}

// albumPageJSON is one page of albums and whether another follows.
type albumPageJSON struct {
	Albums []albumJSON `json:"albums"`
	More   bool        `json:"more"`
}

// listAlbums writes one page of the query: it asks for one album more than a
// page, which is how it knows there is a next page, and does not send it.
func (s *Server) listAlbums(w http.ResponseWriter, r *http.Request, q subsonic.AlbumListQuery) {
	off := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a number, 0 or more")
			return
		}
		off = n
	}
	q.Size, q.Offset = albumPage+1, off
	libJSON(s, w, r, func(ctx context.Context, l Library) (albumPageJSON, error) {
		al, err := l.GetAlbumList2(ctx, q)
		more := len(al) > albumPage
		if more {
			al = al[:albumPage]
		}
		return albumPageJSON{albumsOf(al), more}, err
	})
}

func (s *Server) handleAlbums(w http.ResponseWriter, r *http.Request) {
	typ := map[string]string{"recent": subsonic.ListRecent, "random": subsonic.ListRandom, "newest": subsonic.ListNewest}[r.URL.Query().Get("list")]
	if typ == "" {
		writeError(w, http.StatusBadRequest, "list must be recent, random or newest")
		return
	}
	s.listAlbums(w, r, subsonic.AlbumListQuery{Type: typ})
}

func (s *Server) handleGenre(w http.ResponseWriter, r *http.Request) {
	s.listAlbums(w, r, subsonic.AlbumListQuery{Type: subsonic.ListByGenre, Genre: r.PathValue("name")})
}

func (s *Server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		a, err := l.GetAlbum(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			albumJSON
			Songs []Song `json:"songs"`
		}{albumOf(a.Album), songsOf(a.Songs)}, nil
	})
}

func playlistOf(p subsonic.Playlist) playlistJSON {
	return playlistJSON{string(p.ID), p.Name, p.SongCount, string(p.CoverArt)}
}

func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]playlistJSON, error) {
		pl, err := l.GetPlaylists(ctx)
		out := make([]playlistJSON, len(pl))
		for i, p := range pl {
			out[i] = playlistOf(p)
		}
		return out, err
	})
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		p, err := l.GetPlaylist(ctx, subsonic.ID(r.PathValue("id")))
		if err != nil {
			return nil, err
		}
		return struct {
			playlistJSON
			Songs []Song `json:"songs"`
		}{playlistOf(p.Playlist), songsOf(p.Songs)}, nil
	})
}

// found is the artists, albums and songs of a starred or search answer.
type found struct {
	Artists []artistJSON `json:"artists"`
	Albums  []albumJSON  `json:"albums"`
	Songs   []Song       `json:"songs"`
}

func (s *Server) handleStarred(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		st, err := l.GetStarred2(ctx)
		if err != nil {
			return nil, err
		}
		return found{artistsOf(st.Artists), albumsOf(st.Albums), songsOf(st.Songs)}, nil
	})
}

func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	libJSON(s, w, r, func(ctx context.Context, l Library) ([]genreJSON, error) {
		gl, err := l.GetGenres(ctx)
		out := make([]genreJSON, len(gl))
		for i, g := range gl {
			out[i] = genreJSON{g.Name, g.AlbumCount, g.SongCount}
		}
		return out, err
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) < minSearch {
		writeError(w, http.StatusBadRequest, "type at least 2 characters")
		return
	}
	libJSON(s, w, r, func(ctx context.Context, l Library) (any, error) {
		res, err := l.Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchArtists, AlbumCount: searchAlbums, SongCount: searchSongs})
		if err != nil {
			return nil, err
		}
		return found{artistsOf(res.Artists), albumsOf(res.Albums), songsOf(res.Songs)}, nil
	})
}

func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	size := defaultCover
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = max(minCover, min(maxCover, n))
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.libTimeout)
	defer cancel()
	b, ct, err := s.ctl.Cover(ctx, r.PathValue("id"), size)
	if err != nil {
		// A server that is down is 503; anything else means it has no such cover.
		if k := subsonic.Classify(err); k != subsonic.KindOther && k != subsonic.KindNotFound && !errors.Is(err, context.Canceled) {
			writeError(w, http.StatusServiceUnavailable, k.String())
		} else {
			writeError(w, http.StatusNotFound, "no cover")
		}
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	w.Write(b)
}
```

Then Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/remote/server.go b/internal/remote/server.go
index 8d97542..4e8acf4 100644
--- a/internal/remote/server.go
+++ b/internal/remote/server.go
@@ -54,6 +54,8 @@ type Server struct {
 	done    chan struct{}
 	closed  bool
 
+	libTimeout time.Duration // per library or cover call
+
 	logMu   sync.Mutex
 	lastLog map[string]time.Time
 
@@ -82,20 +84,32 @@ func New(ctl Controller, opts Options) *Server {
 		opts.Heartbeat = 15 * time.Second
 	}
 	s := &Server{
-		ctl:     ctl,
-		opts:    opts,
-		mux:     http.NewServeMux(),
-		clients: map[*client]struct{}{},
-		done:    make(chan struct{}),
-		lastLog: map[string]time.Time{},
-		now:     time.Now,
-		ifaces:  net.InterfaceAddrs,
+		ctl:        ctl,
+		opts:       opts,
+		mux:        http.NewServeMux(),
+		clients:    map[*client]struct{}{},
+		done:       make(chan struct{}),
+		lastLog:    map[string]time.Time{},
+		libTimeout: defaultLibTimeout,
+		now:        time.Now,
+		ifaces:     net.InterfaceAddrs,
 	}
 	s.mux.HandleFunc("GET /api/state", s.handleState)
 	s.mux.HandleFunc("GET /api/queue", s.handleQueue)
 	s.mux.HandleFunc("GET /api/events", s.handleEvents)
 	s.mux.HandleFunc("POST /api/cmd", s.handleCmd)
 	s.mux.HandleFunc("POST /api/play", s.handlePlay)
+	s.mux.HandleFunc("GET /api/artists", s.handleArtists)
+	s.mux.HandleFunc("GET /api/artist/{id}", s.handleArtist)
+	s.mux.HandleFunc("GET /api/albums", s.handleAlbums)
+	s.mux.HandleFunc("GET /api/album/{id}", s.handleAlbum)
+	s.mux.HandleFunc("GET /api/playlists", s.handlePlaylists)
+	s.mux.HandleFunc("GET /api/playlist/{id}", s.handlePlaylist)
+	s.mux.HandleFunc("GET /api/starred", s.handleStarred)
+	s.mux.HandleFunc("GET /api/genres", s.handleGenres)
+	s.mux.HandleFunc("GET /api/genre/{name}", s.handleGenre)
+	s.mux.HandleFunc("GET /api/search", s.handleSearch)
+	s.mux.HandleFunc("GET /api/cover/{id}", s.handleCover)
 	return s
 }
 
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/remote`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/remote/browse.go internal/remote/browse_test.go internal/remote/server.go
git commit -m "remote: browse, search and covers for the web remote, with errors that never carry the server's text" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: The app side: the controller, Settings → Remote, starting and stopping

**Files:**
- Create: `internal/ui/remote.go`, `cmd/mistersubsonic/remote.go`
- Modify: `internal/subsonic/endpoints.go`, `internal/subsonic/types.go`, `tools/mocksubsonic/main.go`, `internal/art/art.go`, `internal/remote/api.go`, `internal/ui/app.go`, `internal/ui/screens_settings.go`, `internal/ui/session.go`, `internal/ui/star.go`, `cmd/mistersubsonic/main.go`, `scripts/e2e-ui.sh`
- Test: `internal/ui/remote_test.go`, `cmd/mistersubsonic/remote_test.go`, `internal/subsonic/testdata/getSong.json` (new); `internal/art/art_test.go`, `internal/remote/remote_test.go`, `internal/subsonic/endpoints_test.go`, `internal/ui/fakes_test.go`, `internal/ui/hints_test.go` (modified)

**Interfaces:**
- **Consumes:** everything in `internal/remote` (Tasks 2–3), `Player.Move` (Task 1), and `webguard.Hostnames` (Task 1).
- **Produces:**
  - **The Subsonic client.** `(*subsonic.Client).GetSong(ctx, id)`: a failed reply, or an OK reply with no song, is not found. The mock server answers `getSong` for `so-N`, and code 70 otherwise. `ui.Library` gains `GetSong`.
  - **Covers.** `(*art.Loader).Bytes(ctx, art.Key) ([]byte, error)` returns the raw cover from the disk cache, else the server, cached under `id@size`.
  - **`(*ui.App).RemoteController() remote.Controller`.**
    - **Thread safety:** reads come from atomically published copies of the library, player, art and stars.
    - **`Do`:** posts to the UI goroutine and waits 5 s (`remoteTimeout`), then gives `ErrBusy`; a call that timed out never runs later. Each command runs the button's code:
      - volume = `setVolume`, which shows the panel and saves debounced;
      - mute = `setMuted`;
      - toggle, next, prev and seek act as the media keys;
      - star = `toggleStar`;
      - shuffle and repeat use the player setters;
      - jump, remove and move check the index and `song_id`, else `ErrStale`.
    - **`Play`:**
      - It resolves songs on the request goroutine: albums, playlists, artists (album by album), or songs by id. For a song id it looks at the songs it has seen, then the queue, then calls `GetSong`.
      - It then posts PlayNow, PlayNext or Enqueue.
  - **Server error mapping.** `writeCtlError` maps errors that `subsonic.Classify` knows: not found gives 404, and unreachable, timeout, TLS or auth gives 503, with the kind text.
  - **Notify.** `ui.Options.Remote` (`interface{ Notify(remote.Change) }`) is called on player events, the volume, mute, stars, every successful `Do`, and `Detach`.
  - **Settings → Remote.** `ui.Options.RemoteSwitch` is `interface{ SetEnabled(bool) error; URLs() []string }`.
    - The rows are Remote On/Off and Address.
    - A failed start leaves the setting off and toasts.
  - **`cmd/mistersubsonic` (`remoteHost`).**
    - It binds `0.0.0.0:<port>` with `webguard.Hostnames()`, for every display.
    - A bind failure gives the toast "Remote: port N is in use", and the app carries on.
    - It closes after the UI quits and before the player stops.
  - **The e2e.** A third run of `scripts/e2e-ui.sh`, with the remote on and curl on 127.0.0.1. It checks:
    - the 403 guards;
    - a play by id before any browse, through `getSong`, and that `so-99` gets 404;
    - a search, a play of an album, the queue and a toggle;
    - a stale `song_id` gets 409;
    - the port closes on SIGTERM.

- [ ] **Step 1: Write the failing tests**

`cmd/mistersubsonic/remote_test.go` (new file):

```go
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
	if err := h.SetEnabled(true); err != nil {
		t.Fatal(err)
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
	if h.URLs() != nil {
		t.Fatalf("urls of a server that did not start: %v", h.URLs())
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
```

`internal/subsonic/testdata/getSong.json` (new file):

```
{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","openSubsonic":true,
 "song":{"id":"so-1","parent":"al-10","title":"Капитан Африка","album":"Радио Африка","artist":"Аквариум","albumId":"al-10","artistId":"ar-2","coverArt":"al-10",
  "track":1,"discNumber":1,"year":1983,"size":41943040,"contentType":"audio/flac","suffix":"flac","duration":240,"bitRate":1411,
  "bitDepth":24,"samplingRate":96000,"channelCount":2}}}
```

`internal/ui/remote_test.go` (new file):

```go
package ui

import (
	"context"
	"errors"
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
}

func (s *fakeSwitch) SetEnabled(on bool) error {
	s.calls = append(s.calls, on)
	return s.err
}
func (s *fakeSwitch) URLs() []string { return s.urls }

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
```

Then update the existing tests. Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/art/art_test.go b/internal/art/art_test.go
index 47ddbb9..10591c7 100644
--- a/internal/art/art_test.go
+++ b/internal/art/art_test.go
@@ -303,3 +303,42 @@ func TestHTTPFetcherErrorsDoNotLeakCredentials(t *testing.T) {
 		}
 	}
 }
+
+// Bytes hands out the encoded cover from the disk cache, else the server
+// (and caches it), without decoding.
+func TestBytesFromDiskThenServer(t *testing.T) {
+	disk, _ := cache.Open(t.TempDir(), 1<<20)
+	raw := []byte("not decodable, and not decoded")
+	k := Key{"al-1", 300}
+	disk.Put(k.String(), raw)
+	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("offline") }, Options{Disk: disk})
+	got, err := h.l.Bytes(context.Background(), k)
+	if err != nil || !bytes.Equal(got, raw) || h.calls.Load() != 0 {
+		t.Fatalf("disk hit: %q %v, fetched %d", got, err, h.calls.Load())
+	}
+	// A miss fetches once and fills the cache.
+	k2 := Key{"al-2", 64}
+	png := pngBytes(8, 8)
+	h2 := newHarness(t, func(Key) ([]byte, error) { return png, nil }, Options{Disk: disk})
+	for range 2 {
+		got, err = h2.l.Bytes(context.Background(), k2)
+		if err != nil || !bytes.Equal(got, png) {
+			t.Fatalf("miss: %v", err)
+		}
+	}
+	if h2.calls.Load() != 1 {
+		t.Fatalf("fetched %d times", h2.calls.Load())
+	}
+	// Errors pass; an empty id or size never fetches.
+	h3 := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("offline") }, Options{})
+	if _, err := h3.l.Bytes(context.Background(), Key{"x", 100}); err == nil {
+		t.Fatal("a failed fetch is no error")
+	}
+	n := h3.calls.Load()
+	if _, err := h3.l.Bytes(context.Background(), Key{"", 100}); err == nil || h3.calls.Load() != n {
+		t.Fatalf("empty id: %v", err)
+	}
+	if _, err := h3.l.Bytes(context.Background(), Key{"x", 0}); err == nil || h3.calls.Load() != n {
+		t.Fatalf("zero size: %v", err)
+	}
+}
diff --git a/internal/remote/remote_test.go b/internal/remote/remote_test.go
index a530dc3..abf2cca 100644
--- a/internal/remote/remote_test.go
+++ b/internal/remote/remote_test.go
@@ -4,6 +4,7 @@ import (
 	"bufio"
 	"context"
 	"errors"
+	"fmt"
 	"io"
 	"net"
 	"net/http"
@@ -13,6 +14,8 @@ import (
 	"sync"
 	"testing"
 	"time"
+
+	"mistersubsonic/internal/subsonic"
 )
 
 type fakeCtl struct {
@@ -193,6 +196,28 @@ func TestCmdErrorMapping(t *testing.T) {
 	}
 }
 
+// A library error out of Play or Do is told by its kind, as in the browse
+// routes: not found is 404, an unreachable server 503, and the error text
+// (it can hold the server's URL) never reaches the page.
+func TestPlayLibraryErrorMapping(t *testing.T) {
+	for _, tc := range []struct {
+		err  error
+		code int
+		body string
+	}{
+		{&subsonic.APIError{Code: subsonic.CodeNotFound, Message: "song not found"}, 404, `{"error":"not found"}`},
+		{fmt.Errorf("get http://u:secret@host/rest: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}), 503, `{"error":"server unreachable"}`},
+		{context.DeadlineExceeded, 503, `{"error":"timed out"}`},
+	} {
+		f := newFake()
+		f.doErr = tc.err
+		w := do(New(f, Options{}).Handler(), "POST", "/api/play", `{"what":"songs","ids":["x"]}`, nil)
+		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.body {
+			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
+		}
+	}
+}
+
 func TestPlay(t *testing.T) {
 	tests := []struct {
 		name, body string
diff --git a/internal/subsonic/endpoints_test.go b/internal/subsonic/endpoints_test.go
index 1381a8e..b299751 100644
--- a/internal/subsonic/endpoints_test.go
+++ b/internal/subsonic/endpoints_test.go
@@ -167,3 +167,30 @@ func TestMissingEntityIsNotFound(t *testing.T) {
 		t.Fatalf("err = %v", err)
 	}
 }
+
+func TestGetSong(t *testing.T) {
+	s, c := connected(t)
+	so, err := c.GetSong(ctx, "so-1")
+	if err != nil {
+		t.Fatal(err)
+	}
+	if s.query("getSong").Get("id") != "so-1" {
+		t.Fatal("song id not sent")
+	}
+	if so.ID != "so-1" || so.Title != "Капитан Африка" || so.AlbumID != "al-10" || so.Suffix != "flac" || so.Duration != 240 {
+		t.Fatalf("song = %+v", so)
+	}
+}
+
+func TestGetSongMissingIsNotFound(t *testing.T) {
+	s, c := connected(t)
+	s.override["getSong"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"Song not found"}}}`
+	if _, err := c.GetSong(ctx, "nope"); Classify(err) != KindNotFound {
+		t.Fatalf("err = %v", err)
+	}
+	// An ok reply without a song is not found too.
+	s.override["getSong"] = `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
+	if _, err := c.GetSong(ctx, "nope"); Classify(err) != KindNotFound {
+		t.Fatalf("empty reply: err = %v", err)
+	}
+}
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 0163a93..5cfdf68 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -32,6 +32,25 @@ type fakeLibrary struct {
 	stars     []string      // "star al-1", "unstar s2"
 	block     chan struct{} // if set, Search3 waits for it (or ctx)
 	starCalls int           // GetStarred2 calls
+	songCalls []subsonic.ID // GetSong calls
+}
+
+// GetSong finds a song of any album (the server knows them all).
+func (l *fakeLibrary) GetSong(_ context.Context, id subsonic.ID) (*subsonic.Song, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	l.songCalls = append(l.songCalls, id)
+	if l.err != nil {
+		return nil, l.err
+	}
+	for _, songs := range l.tracks {
+		for _, so := range songs {
+			if so.ID == id {
+				return &so, nil
+			}
+		}
+	}
+	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "song not found"}
 }
 
 func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
@@ -206,6 +225,8 @@ type fakePlayer struct {
 	played  []subsonic.Song
 	start   int
 	seekPos time.Duration
+	moves   [][2]int // Move(from, to) calls
+	removed []int    // Remove(i) calls
 }
 
 func newFakePlayer() *fakePlayer {
@@ -220,8 +241,8 @@ func (p *fakePlayer) Next()                       { p.call("next") }
 func (p *fakePlayer) Prev()                       { p.call("prev") }
 func (p *fakePlayer) Seek(d time.Duration)        { p.call("seek"); p.seekPos, p.st.Position = d, d }
 func (p *fakePlayer) Jump(i int)                  { p.call("jump"); p.st.Index = i }
-func (p *fakePlayer) Remove(i int)                { p.call("remove") }
-func (p *fakePlayer) Move(from, to int)           { p.call("move") }
+func (p *fakePlayer) Remove(i int)                { p.call("remove"); p.removed = append(p.removed, i) }
+func (p *fakePlayer) Move(from, to int)           { p.call("move"); p.moves = append(p.moves, [2]int{from, to}) }
 func (p *fakePlayer) SetShuffle(on bool)          { p.st.Shuffle = on }
 func (p *fakePlayer) SetRepeat(r player.Repeat)   { p.st.Repeat = r }
 func (p *fakePlayer) ResumeFrom(r *player.Resume) {
diff --git a/internal/ui/hints_test.go b/internal/ui/hints_test.go
index 42f031a..74006b0 100644
--- a/internal/ui/hints_test.go
+++ b/internal/ui/hints_test.go
@@ -46,8 +46,12 @@ var hintFixtures = map[string]func(ta *testApp) Screen{
 	"settings": func(ta *testApp) Screen { return NewSettingsScreen() },
 	"playback": func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
 	"display":  func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
-	"servers":  func(ta *testApp) Screen { return NewServersScreen() },
-	"wizard":   func(ta *testApp) Screen { return NewWizardScreen(false, false) },
+	"remote": func(ta *testApp) Screen {
+		ta.o.RemoteSwitch = &fakeSwitch{urls: []string{"http://192.168.1.50:8080/"}}
+		return newSettingsList("Remote", remoteSettings)
+	},
+	"servers": func(ta *testApp) Screen { return NewServersScreen() },
+	"wizard":  func(ta *testApp) Screen { return NewWizardScreen(false, false) },
 	"wizard with text": func(ta *testApp) Screen {
 		w := NewWizardScreen(false, false)
 		w.fields[stepURL] = []rune("http://h:4533")
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/subsonic ./internal/art ./internal/remote ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
--- FAIL: TestPlayLibraryErrorMapping (0.00s)
remote_test.go:216: subsonic: error 70: song not found: 500 {"error":"subsonic: error 70: song not found"}
remote_test.go:216: get http://u:secret@host/rest: dial: refused: 500 {"error":"get http://u:secret@host/rest: dial: refused"}
remote_test.go:216: context deadline exceeded: 500 {"error":"context deadline exceeded"}
h.l.Bytes undefined (type *Loader has no field or method Bytes)
h2.l.Bytes undefined (type *Loader has no field or method Bytes)
h3.l.Bytes undefined (type *Loader has no field or method Bytes)
c.GetSong undefined (type *Client has no field or method GetSong)
```

- [ ] **Step 3: Implement**

`cmd/mistersubsonic/remote.go` (new file):

```go
package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/webguard"
)

// remoteHost starts and stops the web remote's server (Settings → Remote
// switches it, and the config starts it). It is the UI's RemoteSwitch and its
// RemoteNotifier: Notify goes to whichever server is running.
type remoteHost struct {
	ctl  remote.Controller // set once, before the first SetEnabled
	port int

	mu  sync.Mutex // serialises SetEnabled and Close
	srv *remote.Server
	cur atomic.Pointer[remote.Server] // srv, for Notify and URLs from any goroutine
}

// SetEnabled starts the server on 0.0.0.0:<port> or stops it. Starting a
// running server, or stopping a stopped one, does nothing. The error of a
// failed start is short and safe to show on the TV.
func (h *remoteHost) SetEnabled(on bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !on {
		h.stopLocked()
		return nil
	}
	if h.srv != nil {
		return nil
	}
	srv := remote.New(h.ctl, remote.Options{Hostnames: webguard.Hostnames(), Log: log.Printf})
	if err := srv.Listen(net.JoinHostPort("0.0.0.0", strconv.Itoa(h.port))); err != nil {
		log.Printf("remote: %v", err)
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("port %d is in use", h.port)
		}
		return fmt.Errorf("can't listen on port %d", h.port)
	}
	h.srv = srv
	h.cur.Store(srv)
	log.Printf("remote: listening on %v", srv.URLs())
	return nil
}

func (h *remoteHost) stopLocked() {
	if h.srv == nil {
		return
	}
	h.cur.Store(nil)
	if err := h.srv.Close(); err != nil {
		log.Printf("remote: %v", err)
	}
	h.srv = nil
	log.Printf("remote: stopped")
}

// URLs are the addresses the running server answers on (none when stopped).
func (h *remoteHost) URLs() []string {
	if s := h.cur.Load(); s != nil {
		return s.URLs()
	}
	return nil
}

// Notify tells the running server that the player or the queue changed.
func (h *remoteHost) Notify(c remote.Change) {
	if s := h.cur.Load(); s != nil {
		s.Notify(c)
	}
}

// Close stops the server (on exit, before the player).
func (h *remoteHost) Close() { h.SetEnabled(false) }

// toaster is what startRemote needs of the UI (*ui.App).
type toaster interface {
	Post(f func())
	Toast(format string, args ...any)
}

// startRemote starts the server at launch. A failure (the port is taken) is
// logged and shown as a toast; the app carries on without the remote.
func startRemote(h *remoteHost, a toaster) {
	if err := h.SetEnabled(true); err != nil {
		a.Post(func() { a.Toast("Remote: %v", err) })
	}
}
```

`internal/ui/remote.go` (new file):

```go
package ui

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/subsonic"
)

// The web remote's side of the app: Controller is the adapter the remote
// server drives. Commands are posted to the UI goroutine and run the same
// code as the buttons; reads (state, library, covers) never touch UI state.

// RemoteNotifier is told when the player state or the queue changed
// (*remote.Server through cmd's host; nil: no remote).
type RemoteNotifier interface {
	Notify(remote.Change)
}

// RemoteSwitch starts and stops the remote server (Settings → Remote).
// cmd implements it, so the UI doesn't need the server.
type RemoteSwitch interface {
	SetEnabled(on bool) error
	URLs() []string
}

// remoteTimeout is how long a command waits for the UI goroutine.
var remoteTimeout = 5 * time.Second

// remoteLibTimeout bounds the library calls that resolve a play request.
const remoteLibTimeout = 10 * time.Second

// maxSeenSongs bounds the songs remembered for play requests by id.
const maxSeenSongs = 4096

var (
	errNoConnection = errors.New("remote: not connected to a server")
	errNoCover      = errors.New("remote: no cover")
)

// liveRefs are the connection's parts, published for goroutines other than
// the UI's (the UI changes them in Attach and Detach).
type liveRefs struct {
	lib Library
	pl  Player
	art ArtSource
}

// publishLive makes the current library, player and art visible to the
// remote. Call on the UI goroutine after changing them.
func (a *App) publishLive() {
	a.live.Store(&liveRefs{lib: a.o.Library, pl: a.o.Player, art: a.o.Art})
}

// publishStars copies this session's star changes on songs for the remote.
// Call on the UI goroutine after a.stars changed.
func (a *App) publishStars() {
	m := map[subsonic.ID]bool{}
	for k, on := range a.stars {
		if k.kind == starSong {
			m[k.id] = on
		}
	}
	a.songStars.Store(&m)
}

// notifyRemote tells the remote server, if there is one.
func (a *App) notifyRemote(c remote.Change) {
	if a.o.Remote != nil {
		a.o.Remote.Notify(c)
	}
}

// RemoteController is the adapter the remote server uses to read and drive
// the app. Its methods are safe from any goroutine.
func (a *App) RemoteController() remote.Controller { return remoteCtl{a} }

type remoteCtl struct{ a *App }

func (c remoteCtl) State() remote.State {
	st := remote.State{Status: "stopped", Repeat: "off", Index: -1}
	r := c.a.live.Load()
	if r == nil || r.pl == nil {
		return st
	}
	ps := r.pl.State()
	st.Status = ps.Status.String()
	st.PositionMS = ps.Position.Milliseconds()
	st.VolumeDB, st.Muted, st.Shuffle, st.Index = ps.VolumeDB, ps.Muted, ps.Shuffle, ps.Index
	st.Repeat = [...]string{"off", "all", "one"}[ps.Repeat]
	if song, ok := ps.Current(); ok {
		rs := c.a.remoteSong(song)
		st.Song, st.DurationMS = &rs, rs.DurationMS
	}
	return st
}

func (c remoteCtl) Queue() remote.QueueView {
	q := remote.QueueView{Index: -1, Songs: []remote.Song{}}
	r := c.a.live.Load()
	if r == nil || r.pl == nil {
		return q
	}
	ps := r.pl.State()
	q.Index = ps.Index
	for _, s := range ps.Queue {
		q.Songs = append(q.Songs, c.a.remoteSong(s))
	}
	return q
}

// remoteSong is s as the page sees it; the star is this session's change if
// there is one, else the server's.
func (a *App) remoteSong(s subsonic.Song) remote.Song {
	starred := s.IsStarred()
	if m := a.songStars.Load(); m != nil {
		if on, ok := (*m)[s.ID]; ok {
			starred = on
		}
	}
	return remote.Song{ID: string(s.ID), Title: s.Title, Artist: s.Artist, Album: s.Album,
		CoverID: string(s.CoverArt), DurationMS: int64(s.Duration) * 1000, Starred: starred}
}

// Library is the connection's library; every song its albums, playlists,
// starred and searches return is remembered, so a later play request can name
// songs by id. nil without a connection.
func (c remoteCtl) Library() remote.Library {
	r := c.a.live.Load()
	if r == nil || r.lib == nil {
		return nil
	}
	return seenLibrary{r.lib, c.a.seen}
}

// Cover returns the encoded cover as the art loader cached or fetched it.
func (c remoteCtl) Cover(ctx context.Context, id string, size int) ([]byte, string, error) {
	r := c.a.live.Load()
	if r == nil {
		return nil, "", errNoCover
	}
	src, ok := r.art.(interface {
		Bytes(context.Context, art.Key) ([]byte, error)
	})
	if !ok {
		return nil, "", errNoCover
	}
	data, err := src.Bytes(ctx, art.Key{ID: subsonic.ID(id), Size: size})
	if err != nil {
		return nil, "", err
	}
	return data, sniffImage(data), nil
}

// sniffImage names the type of encoded image data.
func sniffImage(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF8")):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return "application/octet-stream"
}

// onUI runs f on the UI goroutine and waits for it. If the UI doesn't get to
// it within remoteTimeout the caller gets ErrBusy and f never runs.
func (a *App) onUI(f func() error) error {
	const (
		waiting = iota
		running
		abandoned
	)
	var state atomic.Int32
	res := make(chan error, 1)
	a.Post(func() {
		if state.CompareAndSwap(waiting, running) {
			res <- f()
		}
	})
	t := time.NewTimer(remoteTimeout)
	defer t.Stop()
	select {
	case err := <-res:
		return err
	case <-t.C:
		if state.CompareAndSwap(waiting, abandoned) {
			return remote.ErrBusy
		}
		return <-res // it started just now: it is quick
	}
}

func (c remoteCtl) Do(cmd remote.Command) error {
	return c.a.onUI(func() error { return c.a.remoteDo(cmd) })
}

// remoteDo runs a command on the UI goroutine, as the matching button does.
func (a *App) remoteDo(c remote.Command) error {
	pl := a.Player()
	queue := func(i int, id string) error { // the entry the page saw is still at i
		if pl == nil {
			return remote.ErrStale
		}
		q := pl.State().Queue
		if i < 0 || i >= len(q) || (id != "" && string(q[i].ID) != id) {
			return remote.ErrStale
		}
		return nil
	}
	panel := false // only the volume panel changes: it damages its own area
	switch c.Do {
	case "volume":
		a.setVolume(c.DB)
		panel = true
	case "mute":
		a.setMuted(c.On)
		panel = true
	case "toggle":
		if pl != nil {
			pl.TogglePause()
		}
	case "next", "prev":
		if pl != nil && a.hasQueue() {
			if c.Do == "next" {
				pl.Next()
			} else {
				pl.Prev()
			}
		}
	case "seek":
		if pl != nil && a.hasCurrent() {
			pos := time.Duration(c.PositionMS) * time.Millisecond
			if song, ok := pl.State().Current(); ok && song.Duration > 0 {
				pos = min(pos, time.Duration(song.Duration)*time.Second-time.Second) // as the media keys
			}
			pl.Seek(max(pos, 0))
		}
	case "shuffle":
		if pl != nil {
			pl.SetShuffle(c.On)
		}
	case "repeat":
		if pl != nil {
			pl.SetRepeat(map[string]player.Repeat{"off": player.RepeatOff, "all": player.RepeatAll, "one": player.RepeatOne}[c.Mode])
		}
	case "star":
		if pl != nil {
			if song, ok := pl.State().Current(); ok {
				if it := songStar(song); a.isStarred(it) != c.On {
					a.toggleStar(it)
				}
			}
		}
	case "jump":
		if err := queue(c.Index, c.SongID); err != nil {
			return err
		}
		pl.Jump(c.Index)
	case "remove":
		if err := queue(c.Index, c.SongID); err != nil {
			return err
		}
		pl.Remove(c.Index)
	case "move":
		if err := queue(c.From, c.SongID); err != nil {
			return err
		}
		if err := queue(c.To, ""); err != nil {
			return err
		}
		pl.Move(c.From, c.To)
	case "clear":
		if pl != nil {
			pl.Clear()
		}
	default:
		return errors.New("remote: unknown command " + c.Do)
	}
	if !panel {
		a.dirty = true
	}
	a.notifyRemote(remote.StateChanged) // volume, repeat and the like send no player event
	return nil
}

func (c remoteCtl) Play(p remote.PlayRequest) (int, error) {
	r := c.a.live.Load()
	if r == nil || r.lib == nil || r.pl == nil {
		return 0, errNoConnection
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteLibTimeout)
	defer cancel()
	songs, err := c.a.resolve(ctx, r, p)
	if err != nil || len(songs) == 0 {
		return 0, err
	}
	err = c.a.onUI(func() error {
		pl := c.a.Player() // the connection may have changed meanwhile
		if pl == nil {
			return errNoConnection
		}
		switch p.How {
		case "next":
			pl.PlayNext(songs)
		case "end":
			pl.Enqueue(songs)
		default:
			start := p.Start
			if start < 0 || start >= len(songs) {
				start = 0
			}
			pl.PlayNow(songs, start)
		}
		c.a.dirty = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(songs), nil
}

// resolve turns a play request into songs, off the UI goroutine.
func (a *App) resolve(ctx context.Context, r *liveRefs, p remote.PlayRequest) ([]subsonic.Song, error) {
	switch p.What {
	case "album":
		al, err := r.lib.GetAlbum(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		a.seen.add(al.Songs)
		return al.Songs, nil
	case "playlist":
		pl, err := r.lib.GetPlaylist(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		a.seen.add(pl.Songs)
		return pl.Songs, nil
	case "artist":
		ar, err := r.lib.GetArtist(ctx, subsonic.ID(p.ID))
		if err != nil {
			return nil, err
		}
		var songs []subsonic.Song
		for _, al := range ar.Albums {
			full, err := r.lib.GetAlbum(ctx, al.ID)
			if err != nil {
				return nil, err
			}
			songs = append(songs, full.Songs...)
		}
		a.seen.add(songs)
		return songs, nil
	case "songs":
		var queued []subsonic.Song
		if r.pl != nil {
			queued = r.pl.State().Queue
		}
		out := make([]subsonic.Song, 0, len(p.IDs))
		for _, id := range p.IDs {
			s, ok := a.seen.get(subsonic.ID(id))
			if !ok {
				for _, q := range queued {
					if string(q.ID) == id {
						s, ok = q, true
						break
					}
				}
			}
			if !ok { // not browsed since the app started (the page may be older): ask the server
				so, err := r.lib.GetSong(ctx, subsonic.ID(id))
				if err != nil {
					return nil, err
				}
				a.seen.add([]subsonic.Song{*so})
				s = *so
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, errors.New("remote: unknown play request " + p.What)
}

// songMemory keeps the songs the remote's library calls returned, so a play
// request can name songs by id (the page only has what it browsed). Bounded:
// the oldest go first.
type songMemory struct {
	mu    sync.Mutex
	songs map[subsonic.ID]subsonic.Song
	order []subsonic.ID
}

func (m *songMemory) add(songs []subsonic.Song) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.songs == nil {
		m.songs = map[subsonic.ID]subsonic.Song{}
	}
	for _, s := range songs {
		if _, ok := m.songs[s.ID]; !ok {
			m.order = append(m.order, s.ID)
		}
		m.songs[s.ID] = s
	}
	if over := len(m.order) - maxSeenSongs; over > 0 {
		for _, id := range m.order[:over] {
			delete(m.songs, id)
		}
		m.order = append(m.order[:0], m.order[over:]...)
	}
}

func (m *songMemory) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.songs, m.order = nil, nil
}

func (m *songMemory) get(id subsonic.ID) (subsonic.Song, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.songs[id]
	return s, ok
}

// seenLibrary is the library the remote browses; it remembers the songs.
type seenLibrary struct {
	Library
	seen *songMemory
}

func (l seenLibrary) GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	al, err := l.Library.GetAlbum(ctx, id)
	if err == nil {
		l.seen.add(al.Songs)
	}
	return al, err
}

func (l seenLibrary) GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
	pl, err := l.Library.GetPlaylist(ctx, id)
	if err == nil {
		l.seen.add(pl.Songs)
	}
	return pl, err
}

func (l seenLibrary) GetStarred2(ctx context.Context) (*subsonic.Starred, error) {
	st, err := l.Library.GetStarred2(ctx)
	if err == nil {
		l.seen.add(st.Songs)
	}
	return st, err
}

func (l seenLibrary) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
	res, err := l.Library.Search3(ctx, query, q)
	if err == nil {
		l.seen.add(res.Songs)
	}
	return res, err
}
```

Then Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 8f95a17..41ce018 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -410,6 +410,8 @@ func run(f flags) (err error) {
 	// engine and device close (defers run last-in first-out).
 	sess := newSessions(ctx, eng, dataDir, vol)
 	defer sess.close()
+	host := &remoteHost{port: cfg.Remote.Port}
+	defer host.Close()                          // before the player stops, after the UI has quit
 	defer armDeadline(&deadline, shutdownLimit) // runs first: bounds the clean-ups above
 
 	var loaded *config.Config
@@ -426,10 +428,15 @@ func run(f flags) (err error) {
 		PadAtStart:    padAtStart,
 		Visual:        visual,
 		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
+		Remote:        host, RemoteSwitch: host,
 	})
 	if err != nil {
 		return err
 	}
+	host.ctl = app.RemoteController()
+	if cfg.Remote.Enabled {
+		startRemote(host, app)
+	}
 	beforeRun(app)
 	err = app.Run(ctx)
 	stop() // a second Ctrl-C now kills the process instead of waiting out the shutdown below
diff --git a/internal/art/art.go b/internal/art/art.go
index d72bf8c..fa40924 100644
--- a/internal/art/art.go
+++ b/internal/art/art.go
@@ -158,6 +158,29 @@ func (l *Loader) Get(k Key) (*gfx.Image, bool) {
 	return nil, false
 }
 
+// Bytes returns the encoded cover as the disk cache has it, else as the
+// server sends it (and caches that), for callers that pass the image on
+// undecoded. It runs on the caller's goroutine and doesn't touch the memory
+// LRU.
+func (l *Loader) Bytes(ctx context.Context, k Key) ([]byte, error) {
+	if k.ID == "" || k.Size <= 0 {
+		return nil, fmt.Errorf("art: no cover %s", k)
+	}
+	if l.o.Disk != nil {
+		if data, ok := l.o.Disk.Get(k.String()); ok {
+			return data, nil
+		}
+	}
+	data, err := l.o.Fetch(ctx, k)
+	if err != nil {
+		return nil, err
+	}
+	if l.o.Disk != nil {
+		l.o.Disk.Put(k.String(), data)
+	}
+	return data, nil
+}
+
 // Close stops the workers.
 func (l *Loader) Close() {
 	l.mu.Lock()
diff --git a/internal/remote/api.go b/internal/remote/api.go
index feaaa4d..ec754f1 100644
--- a/internal/remote/api.go
+++ b/internal/remote/api.go
@@ -238,6 +238,12 @@ func writeCtlError(w http.ResponseWriter, err error) {
 	case errors.Is(err, ErrBusy):
 		writeError(w, http.StatusServiceUnavailable, err.Error())
 	default:
+		// A library error out of Play: told by its kind, never by its text,
+		// which can hold the server URL and credentials (as in browse.go).
+		if k := subsonic.Classify(err); k != subsonic.KindOther {
+			writeLibError(w, err)
+			return
+		}
 		writeError(w, http.StatusInternalServerError, err.Error())
 	}
 }
diff --git a/internal/subsonic/endpoints.go b/internal/subsonic/endpoints.go
index 04c2552..ab65e54 100644
--- a/internal/subsonic/endpoints.go
+++ b/internal/subsonic/endpoints.go
@@ -38,6 +38,17 @@ func (c *Client) GetAlbum(ctx context.Context, id ID) (*AlbumWithSongs, error) {
 	return r.Album, nil
 }
 
+func (c *Client) GetSong(ctx context.Context, id ID) (*Song, error) {
+	r, err := c.call(ctx, "getSong", url.Values{"id": {string(id)}})
+	if err != nil {
+		return nil, err
+	}
+	if r.Song == nil {
+		return nil, &APIError{Code: CodeNotFound, Message: "song not found"}
+	}
+	return r.Song, nil
+}
+
 // Album list types for GetAlbumList2.
 const (
 	ListNewest       = "newest"
diff --git a/internal/subsonic/types.go b/internal/subsonic/types.go
index e830648..b2d7696 100644
--- a/internal/subsonic/types.go
+++ b/internal/subsonic/types.go
@@ -198,6 +198,7 @@ type response struct {
 	} `json:"artists"`
 	Artist     *ArtistWithAlbums `json:"artist"`
 	Album      *AlbumWithSongs   `json:"album"`
+	Song       *Song             `json:"song"`
 	AlbumList2 *struct {
 		Albums List[Album] `json:"album"`
 	} `json:"albumList2"`
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 08d01dd..86c18fe 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -5,6 +5,7 @@ import (
 	"fmt"
 	"strings"
 	"sync"
+	"sync/atomic"
 	"time"
 
 	"mistersubsonic/internal/art"
@@ -12,6 +13,7 @@ import (
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
 	"mistersubsonic/internal/player"
+	"mistersubsonic/internal/remote"
 	"mistersubsonic/internal/subsonic"
 )
 
@@ -25,6 +27,7 @@ type Library interface {
 	GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error)
 	GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error)
 	GetStarred2(ctx context.Context) (*subsonic.Starred, error)
+	GetSong(ctx context.Context, id subsonic.ID) (*subsonic.Song, error)
 	Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error)
 	Star(ctx context.Context, t subsonic.StarTarget) error
 	Unstar(ctx context.Context, t subsonic.StarTarget) error
@@ -127,6 +130,11 @@ type Options struct {
 	// Visual is what the visualizer reads: the frames being heard right now
 	// (nil: no sound device, so nothing to show).
 	Visual Visual
+	// Remote hears about player and queue changes for the web remote;
+	// RemoteSwitch starts and stops its server (Settings → Remote). Both
+	// are nil without the remote code (tests).
+	Remote       RemoteNotifier
+	RemoteSwitch RemoteSwitch
 }
 
 // Visual gives the visualizer the audio that is playing: Window fills dst
@@ -247,6 +255,13 @@ type App struct {
 	overwritten  bool      // the last check found the screen drawn over
 	viz          vizState
 	mergeBuf     []gfx.Rect // renderDamage's merged rectangles, reused
+
+	// For the web remote, which reads from other goroutines: the live
+	// connection, this session's song stars, and the songs it has browsed.
+	live       atomic.Pointer[liveRefs]
+	songStars  atomic.Pointer[map[subsonic.ID]bool]
+	seen       *songMemory
+	remoteURLs []string // the server's addresses, read when Settings → Remote opens
 }
 
 func New(o Options) (*App, error) {
@@ -254,7 +269,9 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}, wakeKeys: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
+		swallowed: map[input.Button]bool{}, wakeKeys: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now(), seen: &songMemory{}}
+	a.publishLive()
+	a.publishStars()
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -296,6 +313,7 @@ func New(o Options) (*App, error) {
 // Call on the UI goroutine.
 func (a *App) Attach(lib Library, pl Player, art ArtSource) {
 	a.o.Library, a.o.Player, a.o.Art = lib, pl, art
+	a.publishLive()
 	a.dirty = true
 }
 
@@ -614,6 +632,10 @@ func (a *App) onWake() {
 
 func (a *App) onPlayer(ev player.Event) {
 	a.dirty = true
+	if ev.Kind == player.QueueChanged {
+		a.notifyRemote(remote.QueueChanged)
+	}
+	a.notifyRemote(remote.StateChanged) // the queue's index and the status change with it
 	if ev.Kind == player.Error {
 		title := ev.Song.Title
 		if title == "" {
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index fa1f031..bd2f2eb 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -9,9 +9,10 @@ import (
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/remote"
 )
 
-// SettingsScreen is Settings (spec §8.2): Servers · Playback · Display ·
+// SettingsScreen is Settings (spec §8.2): Servers · Playback · Display · Remote ·
 // About. It works without a connection (from the unreachable
 // screen): changes are saved to the config and applied to the player when
 // there is one.
@@ -19,7 +20,7 @@ type SettingsScreen struct {
 	list List
 }
 
-var settingsItems = []string{"Servers", "Playback", "Display", "About"}
+var settingsItems = []string{"Servers", "Playback", "Display", "Remote", "About"}
 
 func NewSettingsScreen() *SettingsScreen { return &SettingsScreen{} }
 
@@ -41,6 +42,12 @@ func (s *SettingsScreen) Handle(a *App, e input.Event) bool {
 		a.Push(newSettingsList("Playback", playbackSettings))
 	case "Display":
 		a.Push(newSettingsList("Display", displaySettings))
+	case "Remote":
+		a.remoteURLs = nil
+		if sw := a.o.RemoteSwitch; sw != nil {
+			a.remoteURLs = sw.URLs()
+		}
+		a.Push(newSettingsList("Remote", remoteSettings))
 	case "About":
 		a.Push(&AboutScreen{})
 	}
@@ -62,6 +69,7 @@ type setting struct {
 	change func(a *App, dir int)
 	help   string // what the setting does, shown under the list while it is focused
 	toggle bool   // On/Off: ignores key repeat
+	info   bool   // shows a value only: no arrows, nothing to change
 }
 
 // SettingsListScreen is a list of settings with their values.
@@ -113,7 +121,7 @@ func (s *SettingsListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	s.help = gfx.R(area.X, list.Y+list.H, area.W, area.Y+area.H-list.Y-list.H)
 	s.list.Draw(c, list, len(rows), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
 		v := rows[i].value(a)
-		if focused {
+		if focused && !rows[i].info {
 			v = "‹ " + v + " ›"
 		}
 		a.drawRow(c, r, row{main: rows[i].label, focused: focused, right: v})
@@ -211,6 +219,7 @@ func (a *App) setVolume(db float64) {
 		a.showVolume()
 	}
 	db = math.Max(-60, math.Min(0, math.Round(db)))
+	defer a.notifyRemote(remote.StateChanged)
 	if pl := a.Player(); pl != nil {
 		pl.SetVolumeDB(db)
 	} else {
@@ -227,6 +236,7 @@ func (a *App) setVolume(db float64) {
 func (a *App) setMuted(on bool) {
 	a.muted = on
 	a.showVolume()
+	a.notifyRemote(remote.StateChanged)
 	if pl := a.Player(); pl != nil {
 		pl.SetMuted(on)
 	}
@@ -323,6 +333,45 @@ func displaySettings(a *App) []setting {
 	}
 }
 
+// remoteNote is the safety note shown with the remote's settings (spec §2).
+const remoteNote = "Anyone on your network can control playback: there is no password, and it uses plain http."
+
+func remoteSettings(a *App) []setting {
+	enabled := func() bool { return a.cfg != nil && a.cfg.Remote.Enabled }
+	return []setting{
+		{label: "Remote", value: func(a *App) string { return onOff(enabled()) },
+			change: func(a *App, dir int) { a.setRemote(!enabled()) },
+			help:   "Control playback from a phone or computer's browser. " + remoteNote, toggle: true},
+		{label: "Address", value: func(a *App) string {
+			switch {
+			case !enabled():
+				return "Off"
+			case len(a.remoteURLs) == 0:
+				return "no network"
+			}
+			return strings.TrimSuffix(a.remoteURLs[0], "/")
+		},
+			change: func(*App, int) {}, info: true,
+			help: "Open this address in a browser on the same network. " + remoteNote},
+	}
+}
+
+// setRemote starts or stops the remote server and, once that worked, saves
+// the choice.
+func (a *App) setRemote(on bool) {
+	sw := a.o.RemoteSwitch
+	if sw == nil {
+		a.Toast("Remote: not available")
+		return
+	}
+	if err := sw.SetEnabled(on); err != nil {
+		a.Toast("Remote: %v", err)
+		return
+	}
+	a.UpdateConfig(func(c *config.Config) { c.Remote.Enabled = on }, false)
+	a.remoteURLs = sw.URLs()
+}
+
 // ServersScreen lists the configured servers: A switches to one, X opens
 // its menu (switch, remove); the last row adds a server with the wizard.
 type ServersScreen struct {
diff --git a/internal/ui/session.go b/internal/ui/session.go
index 00712b2..d2bb425 100644
--- a/internal/ui/session.go
+++ b/internal/ui/session.go
@@ -11,6 +11,7 @@ import (
 
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/remote"
 	"mistersubsonic/internal/subsonic"
 )
 
@@ -74,6 +75,11 @@ func (a *App) Detach() {
 	a.connecting = false
 	a.insecure = false
 	a.stars, a.starBusy, a.starGen = map[starKey]bool{}, map[starKey]bool{}, 0 // they belong to the old connection
+	a.publishLive()
+	a.publishStars()
+	a.seen.reset()
+	a.notifyRemote(remote.QueueChanged)
+	a.notifyRemote(remote.StateChanged)
 	a.dirty = true
 }
 
@@ -95,6 +101,7 @@ func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
 	a.connecting = false
 	a.SetInsecure(info.Server.InsecureSkipVerify)
 	a.artists, a.stars, a.starBusy, a.starGen = nil, map[starKey]bool{}, map[starKey]bool{}, 0
+	a.publishStars()
 	a.Replace(NewRootScreen(a.P))
 	a.drainInput()
 }
diff --git a/internal/ui/star.go b/internal/ui/star.go
index ee72984..ccf10c0 100644
--- a/internal/ui/star.go
+++ b/internal/ui/star.go
@@ -3,6 +3,7 @@ package ui
 import (
 	"context"
 
+	"mistersubsonic/internal/remote"
 	"mistersubsonic/internal/subsonic"
 )
 
@@ -88,6 +89,8 @@ func (a *App) toggleStar(it starItem) {
 		}
 		a.stars[it.key()] = on
 		a.starGen++
+		a.publishStars()
+		a.notifyRemote(remote.StateChanged)
 		if on {
 			a.Toast("Starred %s", it.name)
 		} else {
diff --git a/scripts/e2e-ui.sh b/scripts/e2e-ui.sh
index 59f98ae..dcb4ab6 100755
--- a/scripts/e2e-ui.sh
+++ b/scripts/e2e-ui.sh
@@ -5,11 +5,17 @@
 # first track); back to the root, the sidebar -> Search, type "mock", into
 # the results -> Tracks -> the second track (streams it).
 # Passes if the app exits cleanly, drew frames, searched and streamed both.
+# A third run turns the web remote on and drives it with curl (localhost only).
 set -eu
 cd "$(dirname "$0")/.."
 tmp=$(mktemp -d)
 mock=""
-cleanup() { if [ -n "$mock" ]; then kill "$mock" 2>/dev/null; fi; rm -rf "$tmp"; }
+app=""
+cleanup() {
+  if [ -n "$mock" ]; then kill "$mock" 2>/dev/null; fi
+  if [ -n "$app" ]; then kill "$app" 2>/dev/null; fi
+  rm -rf "$tmp"
+}
 trap cleanup EXIT
 
 go build -o "$tmp/" ./cmd/mistersubsonic ./tools/mocksubsonic
@@ -75,4 +81,67 @@ if ! grep -q '^ *token = ' "$setup/config.toml" 2>/dev/null || ! grep -q '^ *sal
   tail -n +"$((before + 1))" "$tmp/mock.log" >&2
   exit 1
 fi
-echo "e2e-ui ok: $frames frames rendered; the feed and Search played; the setup wizard saved a token config and played (null device)"
+
+# Third run: the web remote. The app runs headless on the null device with
+# [remote] on, on its own port; curl reads the state, is refused without the
+# right headers, searches, plays the album and toggles pause.
+rport=${E2E_REMOTE_PORT:-14537}
+base="http://127.0.0.1:$rport"
+mkdir "$tmp/remote"
+cat > "$tmp/remote/config.toml" <<CFG
+[[server]]
+name = "mock"
+url = "http://127.0.0.1:$port"
+username = "test"
+password = "test"
+
+[remote]
+enabled = true
+port = $rport
+CFG
+remote_fail() {
+  echo "e2e-ui failed: the web remote: $1; app log:" >&2
+  cat "$tmp/remote.log" >&2
+  exit 1
+}
+rget() { curl -fs --max-time 5 "$base$1"; }
+rpost() { curl -fs --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d "$2" "$base$1"; }
+# rwait PATH PATTERN: poll a GET until its body matches (5 s at most).
+rwait() { for _ in $(seq 50); do rget "$1" 2>/dev/null | grep -q "$2" && return 0; sleep 0.1; done; return 1; }
+rbefore=$(wc -l < "$tmp/mock.log")
+"$tmp/mistersubsonic" -config "$tmp/remote/config.toml" -display headless -null -exit-after 20s > "$tmp/remote.log" 2>&1 &
+app=$!
+rwait /api/state '"status":"stopped"' || remote_fail "/api/state never answered"
+# Without a JSON content type, or from another site, or for another host name: refused.
+[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a POST without a JSON content type was not refused"
+[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H 'Origin: http://evil.example' -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a foreign Origin was not refused"
+[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Host: evil.example' "$base/api/state")" = 403 ] || remote_fail "a foreign Host was not refused"
+# A song by its id alone, before anything was browsed: the app asks the server
+# (getSong). Retried until the app has connected to the mock server.
+one=""
+for _ in $(seq 50); do
+  one=$(rpost /api/play '{"what":"songs","ids":["so-1"],"how":"now"}' 2>/dev/null) && break
+  sleep 0.1
+done
+case "$one" in *'"added":1'*) ;; *) remote_fail "playing one song by id answered '$one'" ;; esac
+rwait /api/queue '"id":"so-1"' || remote_fail "the song played by id is not in the queue"
+tail -n +"$((rbefore + 1))" "$tmp/mock.log" | grep -q 'getSong so-1' || remote_fail "the app never asked the server for the song"
+[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d '{"what":"songs","ids":["so-99"],"how":"end"}' "$base/api/play")" = 404 ] || remote_fail "an unknown song id was not a 404"
+# The search works too.
+rwait '/api/search?q=mock' '"id":"al-mock"' || remote_fail "the search found no album"
+added=$(rpost /api/play '{"what":"album","id":"al-mock","how":"now"}') || remote_fail "play failed"
+case "$added" in *'"added":2'*) ;; *) remote_fail "play answered $added" ;; esac
+rwait /api/state '"status":"playing"' || remote_fail "nothing played after the play request"
+[ "$(rget /api/queue | grep -o '"id":"so-[0-9]*"' | wc -l)" = 2 ] || remote_fail "the queue does not hold the album"
+rget /api/state | grep -q '"title":"' || remote_fail "the state has no song"
+rpost /api/cmd '{"do":"toggle"}' >/dev/null || remote_fail "toggle failed"
+rwait /api/state '"status":"paused"' || remote_fail "toggle did not pause"
+# A stale song id is a conflict.
+[ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H "Origin: $base" -d '{"do":"jump","index":0,"song_id":"nope"}' "$base/api/cmd")" = 409 ] || remote_fail "a stale jump was not a conflict"
+# The app ends cleanly and the remote closes with it.
+kill -TERM "$app"
+wait "$app" || remote_fail "the app exited with an error"
+app=""
+if curl -s --max-time 2 "$base/api/state" >/dev/null 2>&1; then remote_fail "the remote still answers after the app exited"; fi
+tail -n +"$((rbefore + 1))" "$tmp/mock.log" | grep -q 'stream so-' || remote_fail "the mock server streamed nothing"
+echo "e2e-ui ok: $frames frames rendered; the feed and Search played; the setup wizard saved a token config and played; the web remote searched, played and paused (null device)"
diff --git a/tools/mocksubsonic/main.go b/tools/mocksubsonic/main.go
index b66e217..92aa9ed 100644
--- a/tools/mocksubsonic/main.go
+++ b/tools/mocksubsonic/main.go
@@ -94,6 +94,13 @@ func main() {
 				a[k] = v
 			}
 			reply(w, map[string]any{"album": a})
+		case "getSong":
+			i, err := strconv.Atoi(strings.TrimPrefix(q.Get("id"), "so-"))
+			if err != nil || !strings.HasPrefix(q.Get("id"), "so-") || i < 0 || i >= len(songs) {
+				reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 70, "message": "song not found"}})
+				return
+			}
+			reply(w, map[string]any{"song": songs[i]})
 		case "getAlbumList2":
 			reply(w, map[string]any{"albumList2": map[string]any{"album": []any{album}}})
 		case "getArtists":
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -count=1 ./internal/subsonic ./internal/art ./internal/remote ./internal/ui ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `make e2e`. Expected: `e2e ok: …`, and `e2e-ui ok: …`, which now ends with "the web remote searched, played and paused (null device)".

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go cmd/mistersubsonic/remote.go cmd/mistersubsonic/remote_test.go internal/art/art.go internal/art/art_test.go internal/remote/api.go internal/remote/remote_test.go internal/subsonic/endpoints.go internal/subsonic/endpoints_test.go internal/subsonic/testdata/getSong.json internal/subsonic/types.go internal/ui/app.go internal/ui/fakes_test.go internal/ui/hints_test.go internal/ui/remote.go internal/ui/remote_test.go internal/ui/screens_settings.go internal/ui/session.go internal/ui/star.go scripts/e2e-ui.sh tools/mocksubsonic/main.go
git commit -m "ui, cmd: the web remote runs the TV's own controls; Settings → Remote turns it on and shows the address" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: The page

**Files:**
- Create: `internal/remote/web.go`, `internal/remote/web/index.html`, `internal/remote/web/app.js`, `internal/remote/web/app.css`
- Modify: `internal/remote/server.go`, `scripts/e2e-ui.sh`
- Test: `internal/remote/page_test.go`, `internal/remote/web/app_test.js` (new); `internal/remote/browse_test.go` (modified)

**Interfaces:**
- **Consumes:** every route of Tasks 2–4.
- **Produces:**
  - **Serving.** `index.html`, `app.js` and `app.css` are embedded (`webFS`) and served at `GET /{$}`, `/app.js` and `/app.css` with `Cache-Control: no-cache` and the right types. Other paths stay 404.
  - **The page itself** is plain ES2020 and CSS, with no external resources, and is about 38 KB.
    - **Layout:** a phone tab bar, and two columns at 900 px or wider.
    - **Now Playing:** seek by tap or drag, prev/toggle/next, volume, mute, star, shuffle and repeat (off, all, one).
    - **Queue:** a tap plays from a song. Inline menus offer Remove, Move up and Move down, and send `song_id`. Clear asks for confirmation.
    - **Browse:** Artists A–Z, Albums (Recent, Random, Newest, More), Playlists, Starred and Genres. Album, playlist and artist pages offer Play, Play next and Add to queue. A tap on a song opens its menu.
    - **Search:** a 300 ms debounce, and at least 2 characters.
    - **Feedback:** toasts.
    - **Live updates:** SSE with interpolation between ticks, a "Reconnecting…" banner with back-off of 1, 2, 4 … 30 s, and a reload on reconnect.
    - **Stale taps:** a 409 reloads the queue and shows "The queue changed".
    - **Hash routes:** `#queue`, `#browse/album/ID`, `#search` and so on.
  - **Pure functions** (the reducer, interpolation, back-off and API wrapper) are exported for node under `if (typeof module !== 'undefined')`. `TestPageJS` runs `node --test web/app_test.js`, and skips without node.
  - **The e2e** also fetches the page, the JS and the CSS.

- [ ] **Step 1: Write the failing tests**

`internal/remote/page_test.go` (new file):

```go
package remote

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestPageServed(t *testing.T) {
	s, _ := browse(&fakeLib{})
	for _, c := range []struct{ path, typ, sniff string }{
		{"/", "text/html; charset=utf-8", "<!doctype html>"},
		{"/app.js", "text/javascript; charset=utf-8", "use strict"},
		{"/app.css", "text/css; charset=utf-8", "--accent"},
	} {
		r := do(s.Handler(), "GET", c.path, "", nil)
		if r.Code != 200 {
			t.Fatalf("%s: %d", c.path, r.Code)
		}
		if got := r.Header().Get("Content-Type"); got != c.typ {
			t.Errorf("%s content type %q, want %q", c.path, got, c.typ)
		}
		if got := r.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s cache control %q", c.path, got)
		}
		if got := r.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s nosniff %q", c.path, got)
		}
		if !strings.Contains(r.Body.String(), c.sniff) {
			t.Errorf("%s does not contain %q", c.path, c.sniff)
		}
	}
	// Only those paths, only GET; anything else is still a 404 or 405.
	if r := do(s.Handler(), "GET", "/nope", "", nil); r.Code != 404 {
		t.Errorf("/nope: %d", r.Code)
	}
	if r := do(s.Handler(), "GET", "/app_test.js", "", nil); r.Code != 404 {
		t.Errorf("/app_test.js: %d", r.Code)
	}
}

func TestPageIsSelfContained(t *testing.T) {
	ext := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]|https?://`)
	total := 0
	for _, f := range []string{"index.html", "app.js", "app.css"} {
		b, err := webFS.ReadFile("web/" + f)
		if err != nil {
			t.Fatal(err)
		}
		total += len(b)
		text := strings.ReplaceAll(string(b), "http://www.w3.org/2000/svg", "")
		if m := ext.FindString(text); m != "" {
			t.Errorf("%s references an external URL: %q", f, m)
		}
	}
	if total >= 60<<10 {
		t.Errorf("embedded page is %d bytes, want under 60 KB", total)
	}
	t.Logf("embedded page: %d bytes", total)
}

// TestPageJS runs the page's own unit tests with node, when node is installed.
func TestPageJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("web/app_test.js"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--test", "web/app_test.js")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}
```

`internal/remote/web/app_test.js` (new file):

```javascript
'use strict';
// Run with: node --test app_test.js
const test = require('node:test');
const assert = require('node:assert');
const A = require('./app.js');

const song = (id) => ({ id, title: 't' + id, artist: 'a', album: 'b', cover_id: 'c' + id, duration_ms: 200000, starred: false });
const st = (o) => Object.assign({ song: song('s1'), status: 'playing', position_ms: 10000, duration_ms: 200000,
  volume_db: -10, muted: false, shuffle: false, repeat: 'off', index: 0 }, o);

test('reduce: state event stores the state and when it came', () => {
  const s0 = A.initial();
  assert.strictEqual(s0.state, null);
  const s1 = A.reduce(s0, { type: 'state', data: st(), now: 5000 });
  assert.strictEqual(s1.state.status, 'playing');
  assert.strictEqual(s1.at, 5000);
  assert.strictEqual(s0.state, null, 'the old state is not changed');
});

test('reduce: queue event, and a state with a new index follows the queue index', () => {
  let s = A.reduce(A.initial(), { type: 'queue', data: { index: 1, songs: [song('a'), song('b')] } });
  assert.strictEqual(s.queue.songs.length, 2);
  assert.strictEqual(s.queue.index, 1);
  s = A.reduce(s, { type: 'state', data: st({ index: 0 }), now: 1 });
  assert.strictEqual(s.queue.index, 0, 'the highlighted row moves with the state');
});

test('reduce: a null songs list becomes empty', () => {
  const s = A.reduce(A.initial(), { type: 'queue', data: { index: 0, songs: null } });
  assert.deepStrictEqual(s.queue.songs, []);
});

test('reduce: connection events', () => {
  let s = A.reduce(A.initial(), { type: 'conn', up: true });
  assert.strictEqual(s.connected, true);
  s = A.reduce(s, { type: 'conn', up: false });
  assert.strictEqual(s.connected, false);
});

test('reduce: unknown events change nothing', () => {
  const s = A.initial();
  assert.strictEqual(A.reduce(s, { type: 'bogus' }), s);
});

test('position: moves while playing, clamps to the duration', () => {
  const s = A.reduce(A.initial(), { type: 'state', data: st(), now: 1000 });
  assert.strictEqual(A.position(s, 1000), 10000);
  assert.strictEqual(A.position(s, 3500), 12500);
  assert.strictEqual(A.position(s, 1e9), 200000);
  assert.strictEqual(A.position(s, 500), 10000, 'never goes back before the tick');
});

test('position: stands still unless playing', () => {
  for (const status of ['paused', 'loading', 'buffering', 'stopped']) {
    const s = A.reduce(A.initial(), { type: 'state', data: st({ status }), now: 1000 });
    assert.strictEqual(A.position(s, 9000), 10000, status);
  }
  assert.strictEqual(A.position(A.initial(), 9000), 0);
});

test('position: no duration means no clamp', () => {
  const s = A.reduce(A.initial(), { type: 'state', data: st({ duration_ms: 0 }), now: 0 });
  assert.strictEqual(A.position(s, 4000), 14000);
});

test('backoff: 1, 2, 4 ... capped at 30 s', () => {
  const got = [0, 1, 2, 3, 4, 5, 6, 20].map(A.backoff);
  assert.deepStrictEqual(got, [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
});

test('formatTime', () => {
  assert.strictEqual(A.fmtTime(0), '0:00');
  assert.strictEqual(A.fmtTime(65400), '1:05');
  assert.strictEqual(A.fmtTime(3600000 + 61000), '1:01:01');
  assert.strictEqual(A.fmtTime(-5), '0:00');
  assert.strictEqual(A.fmtTime(NaN), '0:00');
});

const reply = (status, body) => async () => ({ ok: status >= 200 && status < 300, status, json: async () => body });

test('api: get returns the JSON and calls the right URL', async () => {
  let seen;
  const api = A.makeApi(async (url, opt) => { seen = [url, opt]; return reply(200, { a: 1 })(); });
  assert.deepStrictEqual(await api.get('/api/state'), { a: 1 });
  assert.strictEqual(seen[0], '/api/state');
});

test('api: post sends JSON with the JSON content type', async () => {
  let seen;
  const api = A.makeApi(async (url, opt) => { seen = [url, opt]; return reply(200, { ok: true })(); });
  await api.post('/api/cmd', { do: 'toggle' });
  assert.strictEqual(seen[1].method, 'POST');
  assert.strictEqual(seen[1].headers['Content-Type'], 'application/json');
  assert.strictEqual(seen[1].body, '{"do":"toggle"}');
});

test('api: errors carry the status and the server message', async () => {
  const api = A.makeApi(reply(409, { error: 'remote: the queue changed' }));
  await assert.rejects(api.post('/api/cmd', {}), (e) => e.status === 409 && e.stale === true && /queue changed/.test(e.message));
  const api2 = A.makeApi(reply(503, { error: 'server unreachable' }));
  await assert.rejects(api2.get('/api/artists'), (e) => e.status === 503 && !e.stale && e.message === 'server unreachable');
});

test('api: a failed fetch or a non-JSON error body is an error with status 0 or the code', async () => {
  const api = A.makeApi(async () => { throw new TypeError('network'); });
  await assert.rejects(api.get('/x'), (e) => e.status === 0);
  const api2 = A.makeApi(async () => ({ ok: false, status: 500, json: async () => { throw new Error('bad json'); } }));
  await assert.rejects(api2.get('/x'), (e) => e.status === 500 && e.message.length > 0);
});

test('plural and added messages', () => {
  assert.strictEqual(A.addedMessage('end', 12), 'Added 12 songs');
  assert.strictEqual(A.addedMessage('end', 1), 'Added 1 song');
  assert.strictEqual(A.addedMessage('next', 3), 'Playing next');
  assert.strictEqual(A.addedMessage('now', 3), 'Playing');
});

test('plural', () => {
  assert.strictEqual(A.plural(1, 'album'), '1 album');
  assert.strictEqual(A.plural(0, 'album'), '0 albums');
  assert.strictEqual(A.plural(2, 'song'), '2 songs');
});

test('queue move: the target index and its bounds', () => {
  assert.deepStrictEqual(A.moveTarget(2, 5, -1), { from: 2, to: 1 });
  assert.deepStrictEqual(A.moveTarget(2, 5, 1), { from: 2, to: 3 });
  assert.strictEqual(A.moveTarget(0, 5, -1), null);
  assert.strictEqual(A.moveTarget(4, 5, 1), null);
});

test('route: hash to tab and browse path', () => {
  assert.deepStrictEqual(A.parseHash(''), { tab: 'now', rest: '' });
  assert.deepStrictEqual(A.parseHash('#queue'), { tab: 'queue', rest: '' });
  assert.deepStrictEqual(A.parseHash('#browse/album/al-1'), { tab: 'browse', rest: 'album/al-1' });
  assert.deepStrictEqual(A.parseHash('#search'), { tab: 'search', rest: '' });
  assert.deepStrictEqual(A.parseHash('#nonsense'), { tab: 'now', rest: '' });
});
```

Then update the existing tests. Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/remote/browse_test.go b/internal/remote/browse_test.go
index 3897e11..8927607 100644
--- a/internal/remote/browse_test.go
+++ b/internal/remote/browse_test.go
@@ -316,8 +316,8 @@ func TestBrowseGuarded(t *testing.T) {
 	if r.Code != http.StatusOK {
 		t.Fatal(r.Code)
 	}
-	r = do(s.Handler(), "GET", "/", "", nil)
+	r = do(s.Handler(), "GET", "/nothing-here", "", nil)
 	if r.Code != 404 {
-		t.Fatalf("page route: %d", r.Code)
+		t.Fatalf("unknown route: %d", r.Code)
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/remote`

Expected: FAIL, e.g.:

```
undefined: webFS
```

- [ ] **Step 3: Implement**

`internal/remote/web.go` (new file):

```go
package remote

import (
	"embed"
	"net/http"
)

// The page: three embedded files, no build step. app_test.js sits beside them
// but is not embedded.
//
//go:embed web/index.html web/app.js web/app.css
var webFS embed.FS

// static serves one embedded file. no-cache makes the browser ask every time
// (a 304 costs nothing), so an app update shows at once.
func static(name, contentType string) http.HandlerFunc {
	b, err := webFS.ReadFile("web/" + name)
	if err != nil {
		panic(err) // the file is embedded: a build-time mistake
	}
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		w.Write(b)
	}
}
```

`internal/remote/web/app.css` (new file):

```css
:root {
  --bg: #101418; --panel: #1b2128; --art: #2a313a; --text: #e8eaed; --dim: #9aa0a6;
  --accent: #4fc3f7; --focus: rgba(79,195,247,.25); --error: #ef5350; --line: #2a313a;
  --tabs: 60px;
}
* { box-sizing: border-box; -webkit-tap-highlight-color: transparent; }
html, body { margin: 0; background: var(--bg); color: var(--text); }
body { font: 16px/1.4 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; min-height: 100vh; }
a { color: inherit; text-decoration: none; }
h1, h2, h3, p { margin: 0; }
button, input { font: inherit; color: inherit; }
button { background: none; border: 0; cursor: pointer; padding: 0; }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
svg { width: 24px; height: 24px; fill: currentColor; flex: none; }
.dim { color: var(--dim); }
[hidden] { display: none !important; }

/* Views: one at a time on a phone. */
.view { display: none; padding: 16px 16px calc(var(--tabs) + 24px + env(safe-area-inset-bottom)); max-width: 720px; margin: 0 auto; }
body[data-tab=now] #now, body[data-tab=queue] #queue, body[data-tab=browse] #browse, body[data-tab=search] #search { display: block; }

#tabs { position: fixed; left: 0; right: 0; bottom: 0; display: flex; background: var(--panel); border-top: 1px solid var(--line);
  padding-bottom: env(safe-area-inset-bottom); z-index: 5; }
#tabs a { flex: 1; min-height: var(--tabs); display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; font-size: 12px; color: var(--dim); }
#tabs i { display: block; height: 24px; }
body[data-tab=now] #tabs [data-tab=now], body[data-tab=queue] #tabs [data-tab=queue],
body[data-tab=browse] #tabs [data-tab=browse], body[data-tab=search] #tabs [data-tab=search] { color: var(--accent); }

#banner { position: fixed; top: 0; left: 0; right: 0; z-index: 20; background: var(--error); color: #fff; text-align: center; padding: 8px; font-weight: 600; }
#toasts { position: fixed; left: 0; right: 0; bottom: calc(var(--tabs) + 16px + env(safe-area-inset-bottom)); display: flex; flex-direction: column; align-items: center; gap: 8px; z-index: 30; pointer-events: none; }
.toast { background: #2f3842; color: var(--text); border-radius: 22px; padding: 10px 18px; box-shadow: 0 4px 16px rgba(0,0,0,.5); max-width: 90vw; }
.toast.err { background: #5c2523; }

/* Buttons */
.btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: 44px; padding: 0 18px; border-radius: 22px; background: var(--panel); border: 1px solid var(--line); font-weight: 600; }
.btn.primary { background: var(--accent); color: #06232f; border-color: var(--accent); }
.btn.danger { color: var(--error); }
.btn:active, .ic:active { opacity: .7; }
.ic { width: 48px; height: 48px; min-width: 44px; min-height: 44px; border-radius: 50%; display: inline-flex; align-items: center; justify-content: center; position: relative; color: var(--text); }
.ic.big { width: 72px; height: 72px; background: var(--accent); color: #06232f; }
.ic.big svg { width: 36px; height: 36px; }
.ic.on { color: var(--accent); }
.ic.dimmed { color: var(--dim); }
#b-star:not(.on) svg { fill: none; stroke: currentColor; stroke-width: 2; }
#b-repeat b { position: absolute; right: 6px; bottom: 6px; font-size: 11px; color: var(--accent); }

/* Now Playing */
#player { display: flex; flex-direction: column; gap: 16px; align-items: center; }
.cover { width: min(100%, 44vh, 420px); aspect-ratio: 1; background: var(--art); border-radius: 12px; overflow: hidden; position: relative; display: flex; align-items: center; justify-content: center; margin-top: 8px; }
.cover img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.ph { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; }
.ph svg, .idle-art svg { width: 30%; height: 30%; color: var(--dim); }
.meta { width: 100%; text-align: center; }
.meta h1 { font-size: 22px; line-height: 1.25; overflow-wrap: anywhere; }
.meta p { font-size: 16px; }
.seek { width: 100%; display: flex; align-items: center; gap: 10px; font-size: 13px; color: var(--dim); font-variant-numeric: tabular-nums; }
.seek span { min-width: 40px; }
.seek span:first-child { text-align: right; }
#bar { flex: 1; height: 44px; position: relative; cursor: pointer; touch-action: none; }
#bar .track { position: absolute; left: 0; right: 0; top: 50%; height: 6px; margin-top: -3px; background: var(--art); border-radius: 3px; overflow: hidden; }
#fill { height: 100%; width: 0; background: var(--accent); }
#knob { position: absolute; top: 50%; left: 0; width: 16px; height: 16px; margin: -8px 0 0 -8px; border-radius: 50%; background: var(--text); }
.transport, .extras { display: flex; align-items: center; justify-content: center; gap: 20px; }
.extras { gap: 28px; }
.vol { width: 100%; max-width: 420px; display: flex; align-items: center; gap: 8px; }
.vol span { min-width: 56px; text-align: right; font-size: 13px; }
input[type=range] { flex: 1; height: 44px; background: transparent; accent-color: var(--accent); margin: 0; }
.vol.muted input { opacity: .4; }
.idle { text-align: center; display: flex; flex-direction: column; align-items: center; gap: 16px; padding: 40px 0; }
.idle-art { width: 160px; height: 160px; background: var(--art); border-radius: 12px; display: flex; align-items: center; justify-content: center; }
.idle p { font-size: 20px; color: var(--dim); }

/* Lists */
.head { display: flex; align-items: center; gap: 8px; min-height: 44px; margin-bottom: 8px; }
.head h2 { flex: 1; font-size: 20px; overflow-wrap: anywhere; }
.head .back { width: 44px; height: 44px; display: inline-flex; align-items: center; justify-content: center; margin-left: -10px; }
.bar { display: flex; flex-wrap: wrap; gap: 8px; margin: 4px 0 12px; }
.sub { font-size: 13px; color: var(--dim); margin: 16px 0 6px; text-transform: uppercase; letter-spacing: .05em; }
ul.list { list-style: none; margin: 0; padding: 0; }
.list > li { border-bottom: 1px solid var(--line); }
.row { display: flex; align-items: center; gap: 12px; min-height: 60px; padding: 6px 0; cursor: pointer; width: 100%; text-align: left; }
.row .thumb { width: 48px; height: 48px; border-radius: 6px; background: var(--art); flex: none; overflow: hidden; position: relative; }
.row .thumb img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.row .txt { flex: 1; min-width: 0; }
.row .t1, .row .t2 { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.row .t2 { font-size: 13px; color: var(--dim); }
.row .end { font-size: 13px; color: var(--dim); font-variant-numeric: tabular-nums; }
li.cur .t1 { color: var(--accent); font-weight: 600; }
.more { width: 44px; height: 44px; display: inline-flex; align-items: center; justify-content: center; color: var(--dim); flex: none; margin-right: -8px; }
.menu { display: none; gap: 8px; flex-wrap: wrap; padding: 0 0 10px 60px; }
li.open > .menu { display: flex; }
.menu .btn { min-height: 44px; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 14px; }
.card { display: block; min-width: 0; }
.card .thumb { aspect-ratio: 1; background: var(--art); border-radius: 8px; overflow: hidden; position: relative; }
.card .thumb img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.card .t1 { margin-top: 6px; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.card .t2 { font-size: 12px; color: var(--dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 12px; }
.tile { background: var(--panel); border-radius: 10px; min-height: 76px; padding: 14px; display: flex; flex-direction: column; justify-content: center; font-weight: 600; border: 1px solid var(--line); }
.tile span { font-weight: 400; font-size: 13px; color: var(--dim); }
.seg { display: flex; gap: 6px; margin-bottom: 14px; flex-wrap: wrap; }
.seg a { min-height: 44px; padding: 0 16px; display: inline-flex; align-items: center; border-radius: 22px; background: var(--panel); border: 1px solid var(--line); }
.seg a.on { background: var(--accent); color: #06232f; border-color: var(--accent); font-weight: 600; }
.letters { display: flex; flex-wrap: wrap; gap: 2px; margin-bottom: 8px; }
.letters a { min-width: 44px; height: 44px; display: inline-flex; align-items: center; justify-content: center; border-radius: 8px; color: var(--dim); }
.letters a.on { background: var(--accent); color: #06232f; font-weight: 700; }
.empty, .error { color: var(--dim); padding: 24px 0; text-align: center; }
.error { color: var(--error); }
#q { width: 100%; height: 48px; border-radius: 24px; border: 1px solid var(--line); background: var(--panel); padding: 0 18px; margin-bottom: 8px; }
#q:focus { outline: 2px solid var(--accent); }
.confirm { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

/* Desktop: Now Playing on the left; Queue, Browse and Search as tabs on the right. */
@media (min-width: 900px) {
  :root { --tabs: 0px; }
  body { display: grid; grid-template-columns: 420px 1fr; grid-template-rows: auto 1fr; height: 100vh; overflow: hidden; }
  #now { display: block; grid-area: 1 / 1 / 3 / 2; max-width: none; margin: 0; width: 100%; padding: 32px 28px; overflow-y: auto; border-right: 1px solid var(--line); }
  #tabs { position: static; grid-area: 1 / 2; border: 0; border-bottom: 1px solid var(--line); background: none; padding: 0 24px; gap: 8px; }
  #tabs a { flex: 0 0 auto; flex-direction: row; min-height: 56px; padding: 0 16px; gap: 8px; font-size: 15px; }
  #tabs [data-tab=now] { display: none; }
  body[data-tab=now] #queue { display: block; }
  body[data-tab=now] #tabs [data-tab=queue] { color: var(--accent); }
  .view:not(#now) { grid-area: 2 / 2; overflow-y: auto; max-width: none; margin: 0; padding: 20px 24px 40px; }
  .view:not(#now) > * { max-width: 760px; }
  #toasts { bottom: 24px; left: 420px; }
  .cover { width: min(100%, 360px); }
}
```

`internal/remote/web/app.js` (new file):

```javascript
'use strict';
// The web remote page. The first half is pure (page state, interpolation,
// the API wrapper, the back-off) and is tested with node; the second half
// is the DOM and only runs in a browser.

// ---- pure ----

function initial() {
  return { state: null, at: 0, queue: { index: -1, songs: [] }, connected: false };
}

// reduce applies one event to the page state and returns the new state.
// Events: {type:'state', data, now}, {type:'queue', data}, {type:'conn', up}.
function reduce(s, ev) {
  switch (ev.type) {
    case 'state': {
      const q = ev.data.index === s.queue.index ? s.queue : { index: ev.data.index, songs: s.queue.songs };
      return Object.assign({}, s, { state: ev.data, at: ev.now || 0, queue: q });
    }
    case 'queue':
      return Object.assign({}, s, { queue: { index: ev.data.index, songs: ev.data.songs || [] } });
    case 'conn':
      return Object.assign({}, s, { connected: ev.up });
  }
  return s;
}

// position is the song position in ms at time now, moved on from the last
// tick while playing.
function position(s, now) {
  const st = s.state;
  if (!st) return 0;
  let p = st.position_ms;
  if (st.status === 'playing') p += Math.max(0, now - s.at);
  return st.duration_ms > 0 ? Math.min(p, st.duration_ms) : p;
}

// backoff is the wait before reconnect number n (from 0): 1, 2, 4 ... 30 s.
function backoff(n) { return Math.min(30000, 1000 * Math.pow(2, n)); }

function fmtTime(ms) {
  const t = Math.floor(Math.max(0, ms || 0) / 1000);
  const h = Math.floor(t / 3600), m = Math.floor(t / 60) % 60, sec = t % 60;
  const ss = (sec < 10 ? '0' : '') + sec;
  return h > 0 ? h + ':' + (m < 10 ? '0' : '') + m + ':' + ss : m + ':' + ss;
}

// makeApi wraps fetch: JSON in and out, and one error type with the status
// (0 when the network failed) and the server's message.
function makeApi(fetchFn) {
  const fail = (status, message) => Object.assign(new Error(message), { status, stale: status === 409 });
  async function call(url, opt) {
    let r;
    try { r = await fetchFn(url, opt); } catch (e) { throw fail(0, 'No connection'); }
    let body = null;
    try { body = await r.json(); } catch (e) { /* not JSON */ }
    if (!r.ok) throw fail(r.status, body && body.error ? body.error : 'Request failed (' + r.status + ')');
    return body;
  }
  return {
    get: (url) => call(url),
    post: (url, body) => call(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  };
}

const plural = (n, w) => n + ' ' + w + (n === 1 ? '' : 's');

function addedMessage(how, n) {
  if (how === 'next') return 'Playing next';
  if (how === 'now') return 'Playing';
  return 'Added ' + plural(n, 'song');
}

// moveTarget is the {from, to} of moving row i one step (dir -1 or 1), or null at the ends.
function moveTarget(i, n, dir) {
  const to = i + dir;
  return to < 0 || to >= n ? null : { from: i, to };
}

function parseHash(h) {
  const parts = (h || '').replace(/^#/, '').split('/');
  const tab = ['now', 'queue', 'browse', 'search'].includes(parts[0]) ? parts[0] : 'now';
  return { tab, rest: tab === parts[0] ? parts.slice(1).join('/') : '' };
}

if (typeof module !== 'undefined') {
  module.exports = { initial, reduce, position, backoff, fmtTime, makeApi, addedMessage, plural, moveTarget, parseHash };
}

// ---- the page ----

if (typeof document !== 'undefined') (function () {
  const api = makeApi((u, o) => fetch(u, o));
  const $ = (id) => document.getElementById(id);
  let S = initial();

  const ICONS = {
    play: 'M8 5v14l11-7z', pause: 'M6 19h4V5H6v14zm8-14v14h4V5h-4z',
    next: 'M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z', prev: 'M6 6h2v12H6zm3.5 6l8.5 6V6z',
    volume: 'M3 9v6h4l5 5V4L7 9H3zm13.5 3A4.5 4.5 0 0 0 14 7.97v8.05c1.48-.73 2.5-2.25 2.5-4.02z',
    mute: 'M16.5 12A4.5 4.5 0 0 0 14 7.97v2.21l2.45 2.45c.03-.2.05-.41.05-.63zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06a8.99 8.99 0 0 0 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z',
    star: 'M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z',
    shuffle: 'M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z',
    repeat: 'M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z',
    more: 'M6 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm12 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm-6 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4z',
    note: 'M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6z',
    queue: 'M3 13h2v-2H3v2zm0 4h2v-2H3v2zm0-8h2V7H3v2zm4 4h14v-2H7v2zm0 4h14v-2H7v2zM7 7v2h14V7H7z',
    browse: 'M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-1 9H9V9h10v2zm-4 4H9v-2h6v2zm4-8H9V5h10v2z',
    search: 'M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z',
    back: 'M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z',
  };
  function svg(name) {
    const NS = 'http://www.w3.org/2000/svg';
    const s = document.createElementNS(NS, 'svg'), p = document.createElementNS(NS, 'path');
    s.setAttribute('viewBox', '0 0 24 24');
    s.setAttribute('aria-hidden', 'true');
    p.setAttribute('d', ICONS[name]);
    s.appendChild(p);
    return s;
  }
  function setIcon(el, name) { el.replaceChildren(svg(name), ...Array.from(el.children).filter((c) => c.tagName === 'B')); }

  // h builds an element. Text is always set as text, never as HTML.
  function h(tag, props, ...kids) {
    const el = document.createElement(tag);
    for (const k in props || {}) {
      const v = props[k];
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else el.setAttribute(k, v === true ? '' : v);
    }
    for (const k of kids.flat()) if (k != null && k !== false) el.append(k);
    return el;
  }
  const iconBtn = (name, label, fn, cls) => {
    const b = h('button', { class: cls || 'more', 'aria-label': label, onclick: fn });
    b.append(svg(name));
    return b;
  };

  // ---- feedback ----
  function toast(msg, err) {
    const t = h('div', { class: 'toast' + (err ? ' err' : '') }, msg);
    $('toasts').append(t);
    while ($('toasts').children.length > 3) $('toasts').firstChild.remove();
    setTimeout(() => t.remove(), 2600);
  }
  function fail(e) {
    if (e.stale) { toast('The queue changed', true); loadQueue(); } else toast(e.message, true);
  }
  async function attempt(fn) { try { return await fn(); } catch (e) { fail(e); } }
  const cmd = (body) => attempt(() => api.post('/api/cmd', body));
  const coverURL = (id, size) => '/api/cover/' + encodeURIComponent(id) + '?size=' + size;

  async function play(body) {
    const r = await attempt(() => api.post('/api/play', body));
    if (r) toast(r.added ? addedMessage(body.how, r.added) : 'Nothing to add');
  }

  // ---- state ----
  function dispatch(ev) {
    const prev = S;
    S = reduce(S, ev);
    if (ev.type === 'state') {
      renderNow();
      if (prev.queue.index !== S.queue.index) markCurrent();
    } else if (ev.type === 'queue') {
      renderQueue();
    } else if (ev.type === 'conn') {
      $('banner').hidden = ev.up;
    }
  }
  const now = () => performance.now();
  async function loadState() { const st = await attempt(() => api.get('/api/state')); if (st) dispatch({ type: 'state', data: st, now: now() }); }
  async function loadQueue() { const q = await attempt(() => api.get('/api/queue')); if (q) dispatch({ type: 'queue', data: q }); }

  let es = null, tries = 0, wasDown = false;
  function connect() {
    es = new EventSource('/api/events');
    es.onopen = () => {
      tries = 0;
      dispatch({ type: 'conn', up: true });
      if (wasDown) { wasDown = false; loadState(); loadQueue(); }
    };
    es.addEventListener('state', (e) => dispatch({ type: 'state', data: JSON.parse(e.data), now: now() }));
    es.addEventListener('queue', (e) => dispatch({ type: 'queue', data: JSON.parse(e.data) }));
    es.onerror = () => {
      es.close();
      wasDown = true;
      dispatch({ type: 'conn', up: false });
      setTimeout(connect, backoff(tries++));
    };
  }

  // ---- Now Playing ----
  let coverID = null, dragging = null, volTimer = 0, volHold = 0;
  function renderNow() {
    const st = S.state, song = st && st.song;
    $('idle').hidden = !!song;
    $('player').hidden = !song;
    document.title = song ? song.title + ' · ' + song.artist : 'MiSTer Subsonic';
    if (!st) return;
    if (song) {
      $('title').textContent = song.title;
      $('artist').textContent = song.artist;
      $('album').textContent = song.album;
      if (song.cover_id !== coverID) {
        coverID = song.cover_id;
        const img = $('coverimg');
        img.hidden = true;
        if (coverID) img.src = coverURL(coverID, 500);
      }
      $('dur').textContent = fmtTime(st.duration_ms);
      $('bar').setAttribute('aria-valuemax', st.duration_ms);
      $('b-star').classList.toggle('on', song.starred);
      $('b-star').setAttribute('aria-pressed', song.starred);
    }
    setIcon($('b-toggle'), st.status === 'playing' ? 'pause' : 'play');
    $('b-shuffle').classList.toggle('on', st.shuffle);
    $('b-repeat').classList.toggle('on', st.repeat !== 'off');
    $('rep1').hidden = st.repeat !== 'one';
    $('b-mute').classList.toggle('dimmed', st.muted);
    setIcon($('b-mute'), st.muted ? 'mute' : 'volume');
    document.querySelector('.vol').classList.toggle('muted', st.muted);
    if (!volHold) setVolume(st.volume_db);
    drawPosition();
  }
  function setVolume(db) {
    $('volume').value = db;
    $('voltxt').textContent = Math.round(db) + ' dB';
  }
  function drawPosition() {
    const st = S.state;
    if (!st || !st.song || dragging != null) return;
    const p = position(S, now());
    showPosition(p, st.duration_ms);
  }
  function showPosition(p, d) {
    const f = d > 0 ? Math.min(1, p / d) * 100 : 0;
    $('fill').style.width = f + '%';
    $('knob').style.left = f + '%';
    $('pos').textContent = fmtTime(p);
    $('bar').setAttribute('aria-valuenow', Math.round(p));
  }
  function seekTo(ms) {
    const st = S.state;
    if (!st || !st.song) return;
    ms = Math.round(Math.max(0, Math.min(ms, st.duration_ms)));
    dispatch({ type: 'state', data: Object.assign({}, st, { position_ms: ms }), now: now() });
    cmd({ do: 'seek', position_ms: ms });
  }
  function wireNow() {
    $('coverimg').addEventListener('load', () => { $('coverimg').hidden = false; });
    $('coverimg').addEventListener('error', () => { $('coverimg').hidden = true; });
    const bar = $('bar');
    const at = (e) => {
      const r = bar.getBoundingClientRect();
      return Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * (S.state ? S.state.duration_ms : 0);
    };
    bar.addEventListener('pointerdown', (e) => {
      if (!S.state || !S.state.song) return;
      bar.setPointerCapture(e.pointerId);
      dragging = at(e);
      showPosition(dragging, S.state.duration_ms);
    });
    bar.addEventListener('pointermove', (e) => {
      if (dragging == null) return;
      dragging = at(e);
      showPosition(dragging, S.state.duration_ms);
    });
    bar.addEventListener('pointerup', (e) => {
      if (dragging == null) return;
      const ms = at(e);
      dragging = null;
      seekTo(ms);
    });
    bar.addEventListener('pointercancel', () => { dragging = null; });
    bar.addEventListener('keydown', (e) => {
      const d = { ArrowLeft: -5000, ArrowRight: 5000 }[e.key];
      if (d == null || !S.state) return;
      e.preventDefault();
      seekTo(position(S, now()) + d);
    });
    $('b-toggle').onclick = () => cmd({ do: 'toggle' });
    $('b-prev').onclick = () => cmd({ do: 'prev' });
    $('b-next').onclick = () => cmd({ do: 'next' });
    $('b-star').onclick = () => S.state && S.state.song && cmd({ do: 'star', on: !S.state.song.starred });
    $('b-shuffle').onclick = () => S.state && cmd({ do: 'shuffle', on: !S.state.shuffle });
    $('b-repeat').onclick = () => S.state && cmd({ do: 'repeat', mode: { off: 'all', all: 'one', one: 'off' }[S.state.repeat] || 'off' });
    $('b-mute').onclick = () => S.state && cmd({ do: 'mute', on: !S.state.muted });
    $('volume').addEventListener('input', (e) => {
      const db = Number(e.target.value);
      setVolume(db);
      volHold = Date.now();
      if (!volTimer) volTimer = setTimeout(() => { volTimer = 0; cmd({ do: 'volume', db: Number($('volume').value) }); }, 80);
    });
    // Keep the slider where the finger left it until the server has caught up.
    $('volume').addEventListener('change', () => setTimeout(() => { volHold = 0; }, 600));
    setInterval(() => { if (!document.hidden) drawPosition(); }, 250);
  }

  // ---- rows and menus ----
  function thumb(coverId, size) {
    const t = h('div', { class: 'thumb' });
    if (coverId) {
      const img = h('img', { src: coverURL(coverId, size || 96), alt: '', loading: 'lazy' });
      img.addEventListener('error', () => img.remove());
      t.append(img);
    }
    return t;
  }
  function closeMenus(except) {
    document.querySelectorAll('li.open').forEach((l) => { if (l !== except) l.classList.remove('open'); });
  }
  // item is one list entry: a row (a link, or a button when it has onTap),
  // plus a ⋯ button that opens its menu of {label, fn}.
  function item(o) {
    const li = h('li', { class: o.cur ? 'cur' : null });
    const inner = [
      thumb(o.cover),
      h('div', { class: 'txt' }, h('div', { class: 't1' }, o.title), o.sub ? h('div', { class: 't2' }, o.sub) : null),
      o.end ? h('div', { class: 'end' }, o.end) : null,
    ];
    const toggle = () => { closeMenus(li); li.classList.toggle('open'); };
    const row = o.href ? h('a', { class: 'row', href: o.href }, inner)
      : h('div', { class: 'row', role: 'button', tabindex: 0, onclick: o.onTap || toggle,
        onkeydown: (e) => { if (e.key === 'Enter') (o.onTap || toggle)(); } }, inner);
    const top = h('div', { style: 'display:flex;align-items:center' }, row);
    row.style.flex = '1';
    row.style.minWidth = '0';
    if (o.menu) {
      top.append(iconBtn('more', 'More', toggle));
      li.append(top, h('div', { class: 'menu' }, o.menu.map((m) =>
        h('button', { class: 'btn' + (m.danger ? ' danger' : ''), onclick: () => { li.classList.remove('open'); m.fn(); } }, m.label))));
    } else li.append(top);
    return li;
  }
  const playMenu = (what, id) => [
    { label: 'Play', fn: () => play({ what, id, how: 'now' }) },
    { label: 'Play next', fn: () => play({ what, id, how: 'next' }) },
    { label: 'Add to queue', fn: () => play({ what, id, how: 'end' }) },
  ];
  // songMenu: Play starts from this song when it sits in an album or a playlist.
  const songMenu = (song, ctx, i) => [
    { label: 'Play', fn: () => play(ctx ? { what: ctx.what, id: ctx.id, start: i, how: 'now' } : { what: 'songs', ids: [song.id], how: 'now' }) },
    { label: 'Play next', fn: () => play({ what: 'songs', ids: [song.id], how: 'next' }) },
    { label: 'Add to queue', fn: () => play({ what: 'songs', ids: [song.id], how: 'end' }) },
  ];
  const songItems = (songs, ctx) => songs.map((s, i) =>
    item({ cover: s.cover_id, title: s.title, sub: s.artist + ' · ' + s.album, end: fmtTime(s.duration_ms), menu: songMenu(s, ctx, i) }));
  const bar = (what, id) => h('div', { class: 'bar' }, [['Play', 'now'], ['Play next', 'next'], ['Add to queue', 'end']].map(([label, how], i) =>
    h('button', { class: 'btn' + (i === 0 ? ' primary' : ''), onclick: () => play({ what, id, how }) }, label)));

  // ---- Queue ----
  let openRow = null, confirming = false, shownIndex = -2;
  function renderQueue() {
    const q = S.queue, root = $('queue');
    const list = h('ul', { class: 'list' });
    q.songs.forEach((s, i) => {
      const menu = [
        { label: 'Remove', danger: true, fn: () => cmd({ do: 'remove', index: i, song_id: s.id }) },
        { label: 'Move up', fn: () => moveRow(i, -1) },
        { label: 'Move down', fn: () => moveRow(i, 1) },
      ];
      const li = item({ cover: s.cover_id, title: s.title, sub: s.artist, end: fmtTime(s.duration_ms), cur: i === q.index, menu,
        onTap: () => cmd({ do: 'jump', index: i, song_id: s.id }) });
      li.dataset.i = i;
      if (openRow === i + ':' + s.id) li.classList.add('open');
      li.querySelector('.more').addEventListener('click', () => { openRow = li.classList.contains('open') ? i + ':' + s.id : null; });
      list.append(li);
    });
    const clear = confirming
      ? h('span', { class: 'confirm' }, 'Clear the queue?',
        h('button', { class: 'btn danger', onclick: () => { confirming = false; cmd({ do: 'clear' }); renderQueue(); } }, 'Clear'),
        h('button', { class: 'btn', onclick: () => { confirming = false; renderQueue(); } }, 'Cancel'))
      : h('button', { class: 'btn', onclick: () => { confirming = true; renderQueue(); } }, 'Clear queue');
    root.replaceChildren(
      h('div', { class: 'head' }, h('h2', null, 'Queue'), q.songs.length ? clear : null),
      q.songs.length ? list : h('div', { class: 'empty' }, 'The queue is empty'));
    shownIndex = q.index;
  }
  function moveRow(i, dir) {
    const t = moveTarget(i, S.queue.songs.length, dir);
    if (t) cmd({ do: 'move', from: t.from, to: t.to, song_id: S.queue.songs[i].id });
  }
  // markCurrent moves the highlight when only the index changed.
  function markCurrent() {
    document.querySelectorAll('#queue li.cur').forEach((l) => l.classList.remove('cur'));
    const li = document.querySelector('#queue li[data-i="' + S.queue.index + '"]');
    if (li) li.classList.add('cur');
  }

  // ---- Browse ----
  let nav = 0, artistsCache = null;
  const enc = encodeURIComponent;
  const albumCard = (a) => h('a', { class: 'card', href: '#browse/album/' + enc(a.id) },
    thumb(a.cover_id, 200), h('div', { class: 't1' }, a.name), h('div', { class: 't2' }, [a.artist, a.year || ''].filter(Boolean).join(' · ')));
  const head = (title, back, extra) => h('div', { class: 'head' },
    back ? h('a', { class: 'back', href: back, 'aria-label': 'Back' }, svg('back')) : null, h('h2', null, title), extra);
  const msg = (cls, text) => h('div', { class: cls }, text);

  async function renderBrowse(rest) {
    const my = ++nav, root = $('browse');
    const p = rest.split('/').map(decodeURIComponent);
    const show = (...kids) => { if (my === nav) { root.replaceChildren(...kids); } };
    const get = async (url) => {
      try { return await api.get(url); } catch (e) { show(head('Browse', '#browse'), msg('error', e.message)); return null; }
    };
    switch (p[0]) {
      case 'artists': {
        if (!artistsCache) artistsCache = await get('/api/artists');
        if (!artistsCache) { artistsCache = null; return; }
        const groups = artistsCache.filter((g) => g.artists.length);
        const cur = groups.find((g) => g.letter === p[1]) || groups[0];
        show(head('Artists', '#browse'),
          h('div', { class: 'letters' }, groups.map((g) => h('a', { href: '#browse/artists/' + enc(g.letter), class: g === cur ? 'on' : null }, g.letter))),
          cur ? h('ul', { class: 'list' }, cur.artists.map((a) => item({
            cover: a.cover_id, title: a.name, sub: plural(a.album_count, 'album'), href: '#browse/artist/' + enc(a.id) }))) : msg('empty', 'No artists'));
        return;
      }
      case 'artist': {
        const a = await get('/api/artist/' + enc(p[1]));
        if (a) show(head(a.name, '#browse/artists'), bar('artist', a.id), a.albums.length ? h('div', { class: 'grid' }, a.albums.map(albumCard)) : msg('empty', 'No albums'));
        return;
      }
      case 'albums': case 'genre': {
        const isGenre = p[0] === 'genre', list = isGenre ? 'genre' : p[1] || 'recent';
        const base = isGenre ? '/api/genre/' + enc(p[1]) + '?' : '/api/albums?list=' + list + '&';
        const grid = h('div', { class: 'grid' }), more = h('button', { class: 'btn', hidden: true }, 'More');
        let off = 0;
        const load = async () => {
          more.disabled = true;
          let r;
          try { r = await api.get(base + 'offset=' + off); } catch (e) { more.disabled = false; return toast(e.message, true); }
          if (my !== nav) return;
          off += r.albums.length;
          grid.append(...r.albums.map(albumCard));
          more.hidden = !r.more;
          more.disabled = false;
          if (!off) grid.replaceWith(msg('empty', 'No albums'));
        };
        more.onclick = load;
        show(isGenre ? head(p[1], '#browse/genres') : head('Albums', '#browse'),
          isGenre ? null : h('div', { class: 'seg' }, [['recent', 'Recent'], ['random', 'Random'], ['newest', 'Newest']].map(([k, label]) =>
            h('a', { href: '#browse/albums/' + k, class: k === list ? 'on' : null }, label))),
          grid, h('div', { class: 'bar' }, more));
        await load();
        return;
      }
      case 'album': {
        const a = await get('/api/album/' + enc(p[1]));
        if (a) show(head(a.name, a.artist_id ? '#browse/artist/' + enc(a.artist_id) : '#browse'),
          h('p', { class: 'dim' }, [a.artist, a.year || ''].filter(Boolean).join(' · ')), bar('album', a.id),
          h('ul', { class: 'list' }, songItems(a.songs, { what: 'album', id: a.id })));
        return;
      }
      case 'playlists': {
        const l = await get('/api/playlists');
        if (l) show(head('Playlists', '#browse'), l.length ? h('ul', { class: 'list' }, l.map((x) => item({
          cover: x.cover_id, title: x.name, sub: plural(x.song_count, 'song'), href: '#browse/playlist/' + enc(x.id), menu: playMenu('playlist', x.id) }))) : msg('empty', 'No playlists'));
        return;
      }
      case 'playlist': {
        const pl = await get('/api/playlist/' + enc(p[1]));
        if (pl) show(head(pl.name, '#browse/playlists'), bar('playlist', pl.id), h('ul', { class: 'list' }, songItems(pl.songs, { what: 'playlist', id: pl.id })));
        return;
      }
      case 'starred': {
        const r = await get('/api/starred');
        if (r) show(head('Starred', '#browse'), foundBlock(r, 'Nothing starred yet'));
        return;
      }
      case 'genres': {
        const l = await get('/api/genres');
        if (l) show(head('Genres', '#browse'), l.length ? h('ul', { class: 'list' }, l.map((g) => item({
          title: g.name, sub: plural(g.album_count, 'album') + ' · ' + plural(g.song_count, 'song'), href: '#browse/genre/' + enc(g.name) }))) : msg('empty', 'No genres'));
        return;
      }
    }
    const tile = (href, label, sub) => h('a', { class: 'tile', href }, label, h('span', null, sub));
    show(head('Browse'), h('div', { class: 'tiles' },
      tile('#browse/artists', 'Artists', 'A to Z'), tile('#browse/albums/recent', 'Albums', 'Recent, random, newest'),
      tile('#browse/playlists', 'Playlists', 'Your playlists'), tile('#browse/starred', 'Starred', 'Your favourites'),
      tile('#browse/genres', 'Genres', 'By genre')));
  }

  // foundBlock lists search or starred results, grouped.
  function foundBlock(r, none) {
    if (!r.artists.length && !r.albums.length && !r.songs.length) return msg('empty', none);
    const group = (title, items) => items.length ? [h('div', { class: 'sub' }, title), h('ul', { class: 'list' }, items)] : [];
    return h('div', null,
      group('Artists', r.artists.map((a) => item({ cover: a.cover_id, title: a.name, sub: plural(a.album_count, 'album'), href: '#browse/artist/' + enc(a.id), menu: playMenu('artist', a.id) }))),
      group('Albums', r.albums.map((a) => item({ cover: a.cover_id, title: a.name, sub: a.artist, href: '#browse/album/' + enc(a.id), menu: playMenu('album', a.id) }))),
      group('Songs', songItems(r.songs, null)));
  }

  // ---- Search ----
  let searchTimer = 0, searchSeq = 0;
  function wireSearch() {
    $('q').addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(runSearch, 300);
    });
  }
  async function runSearch() {
    const q = $('q').value.trim(), my = ++searchSeq, out = $('results');
    if (q.length < 2) { out.replaceChildren(q ? msg('empty', 'Type at least 2 characters') : ''); return; }
    try {
      const r = await api.get('/api/search?q=' + enc(q));
      if (my === searchSeq) out.replaceChildren(foundBlock(r, 'Nothing found'));
    } catch (e) {
      if (my === searchSeq) out.replaceChildren(msg('error', e.message));
    }
  }

  // ---- routing ----
  function route() {
    const r = parseHash(location.hash);
    document.body.dataset.tab = r.tab;
    closeMenus();
    if (r.tab === 'browse') renderBrowse(r.rest);
    if (r.tab === 'queue' || (r.tab === 'now' && matchMedia('(min-width: 900px)').matches)) {
      const cur = document.querySelector('#queue li.cur');
      if (cur) cur.scrollIntoView({ block: 'center' });
    }
    if (r.tab === 'search' && matchMedia('(min-width: 900px)').matches) $('q').focus();
  }

  document.querySelectorAll('[data-icon]').forEach((el) => setIcon(el, el.dataset.icon));
  window.addEventListener('hashchange', route);
  wireNow();
  wireSearch();
  renderQueue();
  route();
  loadState();
  loadQueue();
  connect();
})();
```

`internal/remote/web/index.html` (new file):

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#101418">
<title>MiSTer Subsonic</title>
<link rel="stylesheet" href="/app.css">
</head>
<body data-tab="now">
<div id="banner" hidden>Reconnecting…</div>

<section id="now" class="view" aria-label="Now Playing">
  <div id="idle" class="idle" hidden>
    <div class="idle-art" data-icon="note"></div>
    <p>Nothing playing</p>
    <a class="btn primary" href="#browse">Browse</a>
  </div>
  <div id="player" hidden>
    <div class="cover"><img id="coverimg" alt="" hidden><div class="ph" data-icon="note"></div></div>
    <div class="meta">
      <h1 id="title"></h1>
      <p id="artist"></p>
      <p id="album" class="dim"></p>
    </div>
    <div class="seek">
      <span id="pos">0:00</span>
      <div id="bar" role="slider" tabindex="0" aria-label="Position" aria-valuemin="0" aria-valuemax="0" aria-valuenow="0">
        <div class="track"><div id="fill"></div></div><div id="knob"></div>
      </div>
      <span id="dur">0:00</span>
    </div>
    <div class="transport">
      <button id="b-prev" class="ic" data-icon="prev" aria-label="Previous"></button>
      <button id="b-toggle" class="ic big" data-icon="play" aria-label="Play or pause"></button>
      <button id="b-next" class="ic" data-icon="next" aria-label="Next"></button>
    </div>
    <div class="extras">
      <button id="b-star" class="ic" data-icon="star" aria-label="Star"></button>
      <button id="b-shuffle" class="ic" data-icon="shuffle" aria-label="Shuffle"></button>
      <button id="b-repeat" class="ic" data-icon="repeat" aria-label="Repeat"><b id="rep1" hidden>1</b></button>
    </div>
    <div class="vol">
      <button id="b-mute" class="ic" data-icon="volume" aria-label="Mute"></button>
      <input id="volume" type="range" min="-60" max="0" step="1" value="-20" aria-label="Volume">
      <span id="voltxt" class="dim">-20 dB</span>
    </div>
  </div>
</section>

<section id="queue" class="view" aria-label="Queue"></section>
<section id="browse" class="view" aria-label="Browse"></section>
<section id="search" class="view" aria-label="Search">
  <input id="q" type="search" placeholder="Search artists, albums, songs" autocomplete="off" autocapitalize="off" spellcheck="false" aria-label="Search">
  <div id="results"></div>
</section>

<nav id="tabs">
  <a href="#now" data-tab="now"><i data-icon="note"></i>Now Playing</a>
  <a href="#queue" data-tab="queue"><i data-icon="queue"></i>Queue</a>
  <a href="#browse" data-tab="browse"><i data-icon="browse"></i>Browse</a>
  <a href="#search" data-tab="search"><i data-icon="search"></i>Search</a>
</nav>
<div id="toasts" aria-live="polite"></div>
<script src="/app.js"></script>
</body>
</html>
```

Then Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/remote/server.go b/internal/remote/server.go
index 4e8acf4..cf696d7 100644
--- a/internal/remote/server.go
+++ b/internal/remote/server.go
@@ -110,6 +110,9 @@ func New(ctl Controller, opts Options) *Server {
 	s.mux.HandleFunc("GET /api/genre/{name}", s.handleGenre)
 	s.mux.HandleFunc("GET /api/search", s.handleSearch)
 	s.mux.HandleFunc("GET /api/cover/{id}", s.handleCover)
+	s.mux.HandleFunc("GET /{$}", static("index.html", "text/html; charset=utf-8"))
+	s.mux.HandleFunc("GET /app.js", static("app.js", "text/javascript; charset=utf-8"))
+	s.mux.HandleFunc("GET /app.css", static("app.css", "text/css; charset=utf-8"))
 	return s
 }
 
diff --git a/scripts/e2e-ui.sh b/scripts/e2e-ui.sh
index dcb4ab6..8e1cfe8 100755
--- a/scripts/e2e-ui.sh
+++ b/scripts/e2e-ui.sh
@@ -112,6 +112,10 @@ rbefore=$(wc -l < "$tmp/mock.log")
 "$tmp/mistersubsonic" -config "$tmp/remote/config.toml" -display headless -null -exit-after 20s > "$tmp/remote.log" 2>&1 &
 app=$!
 rwait /api/state '"status":"stopped"' || remote_fail "/api/state never answered"
+# The page and its script and style are served.
+rget / | grep -q '<title>MiSTer Subsonic</title>' || remote_fail "the page was not served"
+rget /app.js | grep -q 'use strict' || remote_fail "app.js was not served"
+rget /app.css | grep -q -- '--accent' || remote_fail "app.css was not served"
 # Without a JSON content type, or from another site, or for another host name: refused.
 [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a POST without a JSON content type was not refused"
 [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -H 'Content-Type: application/json' -H 'Origin: http://evil.example' -d '{"do":"toggle"}' "$base/api/cmd")" = 403 ] || remote_fail "a foreign Origin was not refused"
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/remote`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then check the page by eye, if a desktop browser is at hand:
- run the app headless with the mock server and the remote on, as the e2e's remote run does, with the null audio device;
- open `http://127.0.0.1:<port>/` at phone width (390 px) and desktop width (1280 px);
- check Now Playing, Queue, Browse, an album, and Search.

The prototype's screenshots showed:
- the dark theme with the accent blue;
- a centred note in the empty cover;
- the bottom tab bar on a phone;
- Now Playing on the left and the tabs on the right on a desktop.

- [ ] **Step 5: Commit**

```bash
git add internal/remote/browse_test.go internal/remote/page_test.go internal/remote/server.go internal/remote/web.go internal/remote/web/app.css internal/remote/web/app.js internal/remote/web/app_test.js internal/remote/web/index.html scripts/e2e-ui.sh
git commit -m "remote: the web remote's page for phone and desktop, embedded in the app" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Docs and the TV checks

**Files:**
- Modify:
  - `README.md`
  - `docs/spikes.md`
  - `docs/testing-on-mister.md`
  - `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`
  - `docs/superpowers/plans/backlog.md`

**Interfaces:**
- **Consumes:** everything above.
- **Produces:**
  - **The README:** a "Web remote" section covering:
    - turning it on, with Settings → Remote or `[remote] enabled` and the port;
    - where the address is shown;
    - what each tab does;
    - the safety note: anyone on the home network, no password, plain http.
  - **The checklist:** new items 35–38, from spec §9. Log becomes 39.
  - **`docs/spikes.md`:** "Plan 7 on the MiSTer", pending, with the `top` CPU check.
  - **The main spec:** §2, §8 (Settings → Remote) and §13 updated.
  - **The backlog:** C done, except the device check.

- [ ] **Step 1: Record the new checks**

Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

````diff
diff --git a/README.md b/README.md
index af55ae7..dfbcc9a 100644
--- a/README.md
+++ b/README.md
@@ -58,6 +58,25 @@ drifting cover after some idle minutes on Now Playing; off with 0). Each server
 state, scrobble queue and cover cache in `servers/<name>-<id>/` next to the config, so `cover_art_mb`
 applies per server (each server's cache gets that much).
 
+## Web remote
+
+A phone or computer on the same network can control the player from a web page. It is off by
+default. Turn it on in Settings → Remote, or with `enabled = true` under `[remote]` in
+`config.toml`; `port` (default 8080) sets the port and needs a restart. Settings → Remote shows
+the address to open, such as `http://192.168.1.20:8080`, and a failed start (for example a port
+in use) leaves the setting off.
+
+The page has four tabs. Now Playing shows the cover and the song, with play/pause, previous,
+next, seek, volume, mute, star, shuffle and repeat. Queue lists the songs: tap one to play from
+it, and remove, move or clear from the menu on each row. Browse and Search find artists, albums,
+playlists, starred songs and genres, and play an album, playlist, artist or song now, next or at the
+end of the queue (a tap on a song opens its menu). It works while the TV is off, needs no internet,
+and the TV and every open page show the same state. The server login never reaches the browser.
+
+**Safety:** while the remote is on, anyone on your home network can control playback. There is no
+password, and the page uses plain http. Keep it on the LAN only: do not forward the port to the
+internet. Turn it off in Settings when you don't need it.
+
 ## Video settings (MiSTer.ini)
 
 The app draws on the MiSTer's Linux framebuffer. That framebuffer is sized from `MiSTer.ini`; the
diff --git a/docs/spikes.md b/docs/spikes.md
index 5966328..b9f0f7c 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -223,3 +223,21 @@ The analysis alone (`internal/viz`, `BenchmarkAnalyzerUpdate`): 49 µs on HDMI s
 | Full screen 320×240 | pending | pending | pending | pending |
 
 Then run `docs/testing-on-mister.md` items 31–35 and record them here, with the frame-rate defaults that fit 60% of each budget.
+
+## Plan 7 on the MiSTer
+
+**Remote CPU:** pending. Ask the user before playing anything. Play a FLAC album with the visualizer off, and over ssh run:
+
+```
+top -b -n 12 -d 5 | grep mistersubsonic
+```
+
+Run it once with no phone connected and once with the remote page open on Now Playing (it gets a state event up to 8 times a second while playing). The difference must be under 5% of one core.
+
+| Case | CPU |
+|---|---|
+| Playing, no phone | pending |
+| Playing, phone on Now Playing | pending |
+| Playing, phone browsing and searching | pending |
+
+Then run `docs/testing-on-mister.md` items 35–38 and record them here.
diff --git a/docs/superpowers/plans/backlog.md b/docs/superpowers/plans/backlog.md
index f2b1436..d9ad27a 100644
--- a/docs/superpowers/plans/backlog.md
+++ b/docs/superpowers/plans/backlog.md
@@ -53,7 +53,9 @@ Plans 5 and 5b did most of this list; see "Resolved by Plan 5" and "Resolved by
 
 ---
 
-## C. Web remote (desktop and mobile browsers)
+## C. Web remote (done in Plan 7, except the device check)
+
+**Status:** built in Plan 7 (spec `docs/superpowers/specs/2026-10-02-mister-subsonic-web-remote-design.md`), without pairing: the remote is open to the home network while it is on, and the README says so. The phone checks, the queue editing and the CPU number wait for the MiSTer; see "Plan 7 on the MiSTer" in `docs/spikes.md`. The text below is the original sketch.
 
 **What:** control the MiSTer from a phone or a computer on the same network. You see what's playing with its cover, and can play, pause, skip, seek, set the volume and mute, look at and edit the queue, and browse, search and play. It is a control page, not a copy of the TV screen (the dev viewer already does that). Spec §2 left a web or phone remote out of v1, and §13 lists it.
 
diff --git a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
index 99d41b1..42ec8d1 100644
--- a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
+++ b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
@@ -37,7 +37,7 @@
 - EQ, lyrics, internet radio, local or SMB files, CD playback. (The visualizer was a non-goal for v1; Plan 6 added it, see `2026-10-01-mister-subsonic-visualizer-design.md`.)
 - Chapter navigation inside single-file album FLACs using the embedded CUESHEET. The file plays as one track with full seek; chapters are the first item for v2.
 - Playing music in the background while another MiSTer core runs. Exiting the app stops playback.
-- A web remote or phone control.
+- A web remote or phone control. (Plan 7 added one; see `2026-10-02-mister-subsonic-web-remote-design.md`.)
 - An in-app self-updater. Updates come through MiSTer Downloader or update_all instead (§9).
 - Playlist editing beyond starring (no create, rename or reorder).
 - Video of any kind.
@@ -261,7 +261,7 @@ Home ─┬─ Home feed: [Resume] · Recently Added · Recently Played · Most
       ├─ Playlists → Playlist → Tracks
       ├─ Starred (Albums / Artists / Tracks tabs)
       ├─ Search (on-screen keyboard + live results: Artists / Albums / Tracks)
-      └─ Settings (Servers · Playback · Display · About)
+      └─ Settings (Servers · Playback · Display · Remote · About)
 Now Playing ⇄ Queue        (reachable from anywhere)
 Wizard: Server URL → Username → Password → (API key, optional) → Test → Home
 ```
@@ -314,6 +314,8 @@ Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, Space = Start (play/pau
 
 **Screenshot key:** Print Screen or Scroll Lock (MiSTer's Alt+Scroll Lock, which the MiSTer Companion remote sends) saves the frame on screen as `YYYYMMDD_HHMMSS.png` to `/media/fat/screenshots/MiSTer_Subsonic`. It is handled before everything else, the screensaver included, and does not count as activity.
 
+**Remote (Settings → Remote, Plan 7):** two rows. "Remote" turns the web remote On or Off (`remote.enabled`, saved at once; a failed start, such as a port in use, leaves it off and shows a toast). "Address" is an info row showing the URL to open ("no network" when on without an address, "Off" when off). The port is `remote.port` and needs a restart. The help text says anyone on the network can control playback, over plain http. Details: `2026-10-02-mister-subsonic-web-remote-design.md`.
+
 **Hint bar:** a line along the bottom of every screen shows the buttons that matter there (the screen's own list, plus Back and Now Playing where the app handles B and Y), drawn as gamepad buttons or keyboard keys after the last press of either kind. Every hinted button does something on its screen. Exceptions: the X menu shows only Choose and Close; Now Playing isn't hinted while typing on a keyboard (N types there); Select has no key, so a keyboard shows no Select hint; the screensaver hides the bar; a screen that failed to load hints Retry (A), and one that is loading or empty hints nothing of its own. Settings → Display → Hints turns it off (`display.hints`).
 
 Quitting the app is Exit in the main menu (the sidebar's last entry on HDMI, the home list's last item on CRT), or holding B for 2 s on the Home root, with a confirmation.
@@ -448,6 +450,6 @@ If a spike fails, the design section it tests gets revised before implementation
 - Internet radio stations from Navidrome's `getInternetRadioStations`.
 - EQ. (The visualizer is done, Plan 6.)
 - Lyrics (`getLyricsBySongId`).
-- A phone web remote.
+- (Done, Plan 7) A phone web remote.
 - An in-app updater.
 - Playlist editing.
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index 1509fb2..d5b7c46 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -115,7 +115,11 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
 32. **Visualizer on the TV.** Ask the user before showing anything. The panel under the track info is in a sensible place on HDMI and on a CRT (inside the title-safe area, and on a CRT the text block moves up about 10 px when the visualizer is on, by design). The motion is in time with the sound: bass hits land with the beat, and after a seek or a skip the picture follows within a moment.
 33. **Full screen and the corner line.** After a minute in full screen the title and time line moves to the next corner (top left, top right, bottom right, bottom left). The info line is the part that moves; VU (the L/R glyphs and the segment grid) and Scope (its midline) have a fixed frame by design. The hint bar shows after a press and hides after 3 s. Select in full screen cycles the styles without Off, and Start or B leaves.
 34. **Visualizer and the screensaver.** In full screen, while music plays, loads or buffers, the screensaver does not start. With the panel on Now Playing the screensaver works as usual. Pause in full screen and wait: it starts after its idle time over the full screen, and a key wakes it without doing anything else.
-35. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+35. **Web remote, TV on and off.** Ask the user before playing anything, and keep the volume low. Turn Settings → Remote on and open the address it shows on a phone. Play, pause, skip, seek, change the volume, mute, star and switch shuffle and repeat from the page: the TV follows within a moment, and the page follows the gamepad. Then turn the TV off (the music goes on) and do the same from the phone. A bad port in `config.toml` or one already in use leaves Remote off with a toast.
+36. **Remote queue editing.** Ask the user before playing anything. While music plays, from the phone: tap a queue row to play from it, move a song up and down (the current song keeps playing, and the TV queue matches), remove one, and clear the queue. Do the same on the TV at the same time: a stale tap on the phone shows "The queue changed" and reloads.
+37. **Remote search and play.** Ask the user before playing anything. On the phone, search for an artist, an album and a song, then play an album, play a song next and add one to the end. Covers show. The Queue tab and the TV agree. Pull the network cable or turn off the phone's wifi for a moment: the page shows "Reconnecting…" and recovers by itself.
+38. **Remote CPU.** Ask the user before playing anything. With a phone connected (Now Playing open) and music playing, run `top` over ssh for a minute. The phone adds less than 5% of one core to the app's CPU compared with the same playback before it connected (record both numbers in `docs/spikes.md`, "Plan 7 on the MiSTer").
+39. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
````

- [ ] **Step 2: Check the tree**

Run: `go vet ./... && make test && make e2e && make mister mister-test` (zig on `PATH`)

Expected:
- `test-launcher ok`, and `ok` for every Go package.
- `e2e ok: …` and `e2e-ui ok: …`, the latter ending with "the web remote searched, played and paused (null device)".
- Every glibc line ends in `ok`.

- [ ] **Step 3: Commit**

```bash
git add README.md docs/spikes.md docs/superpowers/plans/backlog.md docs/superpowers/specs/2026-09-28-mister-subsonic-design.md docs/testing-on-mister.md
git commit -m "docs: the web remote; TV checks and the CPU check to run" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 4: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" section and stop here.

- [ ] **Step 5: If it answers, deploy and hand over to the user (ask first)**

Ask the user before doing anything on the device.
- `make deploy MISTER=<ip>` replaces the installed app. Their `config.toml` is kept, and the remote stays off until they turn it on.
- The checks play music on their TV. Ask them to turn the volume down first.

With their go-ahead, ask them to:
1. turn the remote on in Settings → Remote;
2. open the address on a phone;
3. run checklist items 35–38. Measure the CPU with `top` over ssh while a phone is connected.

Afterwards:
- Record what they report under "Plan 7 on the MiSTer", replacing "pending".
- Commit with `git commit -am "docs: Plan 7 on the TV" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"`.
- Anything that isn't a small fix goes to `docs/superpowers/plans/backlog.md`.

---

## After this plan

- The device checks for Plans 5, 5b, 6 and 7, when the MiSTer is back.
- Faster list scrolling at full resolution, which needs measuring on the device.
- The first release, only when the user asks.
