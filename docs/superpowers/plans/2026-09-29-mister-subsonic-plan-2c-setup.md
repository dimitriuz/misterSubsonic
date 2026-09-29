# MiSTer Subsonic — Plan 2c: setup wizard, settings, screensaver

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a new user sets the app up on the TV and manages it there:
- **Setup wizard:** server address (with `http://`, `https://` and `:4533` keys), username, password, optional API key, a test, then save. It runs on first start, for a broken config file, and to add servers.
- **Settings:**
  - Servers: switch, add, remove.
  - Playback: volume (saved to the config), ReplayGain, scrobbling, transcoding.
  - Display: layout, screensaver.
  - About and Exit.
- **Screensaver** on Now Playing.
- **"Can't reach the server" screen:** Try again · Switch server · Settings.
- **Follow-ups** left over from the Plan 2b reviews.

**Architecture:** the UI now owns the configuration. Wizard and Settings changes are saved off the UI goroutine: debounced for volume steps, and flushed on exit. The UI asks the app around it to connect with a copy of the config (`Options.Connect`). The app's session manager (`cmd/mistersubsonic/session.go`):
- replaces the running session (client, player, art loader) with a new one; the newest request wins;
- stops the old player, which saves its queue;
- keeps each server's data in its own folder;
- answers with `App.Connected` or `App.ConnectFailed`.

The wizard tests a login with the same `ui.Dial` the app connects with.

**Tech Stack:** Go 1.26+, no new dependencies. zig 0.16.0 for the ARM build.

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`: §4 (auth, TLS), §6 (volume persisted), §8.2 (wizard, Settings, screensaver, unreachable screen), §8.4 (input drain), §9 (config loading, multiple servers). Also read the "Plan 2c must handle" section of `docs/superpowers/plan-2a-followups.md`.

**Scope:**

| | |
|---|---|
| **Plans 2a, 2b (done)** | TV interface: library, search, Now Playing, queue, menus |
| **This plan (2c)** | Everything in the Goal above |
| **Plan 3** | The launcher, KD_GRAPHICS, BGM/SAM, the framebuffer watchdog, the log file, `config.example.toml`, releases, device performance work |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:**
  - `go test -race ./...` passes.
  - `make e2e` passes. The UI check now also sets up a fresh install with the wizard against the mock server.
  - The ARM build passes the glibc check (2.29 ≤ 2.31).
- **Real server:** a silent run against a real Navidrome connected through the new session manager and opened Settings.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-2c-setup`. At every task the tests failed before the code and passed after, and the tree ended identical to the prototype's, byte for byte, golden screenshots included.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:** Go 1.26+ (`go.mod` says `go 1.26.0`). No new modules. No cgo outside `internal/audio`.
- **Threading:** the UI goroutine never does I/O.
  - Server calls go through `App.Load` and `App.LoadCancel`.
  - Config writes go through `App.UpdateConfig`, which saves a copy on a goroutine, one writer at a time (`saveMu`).
  - Loads capture `a.Library()` and `a.Player()` on the UI goroutine before they start.
- **Config ownership:** the UI owns the configuration after `ui.New`. `Options.Connect` always gets a clone, and the connect code never touches the UI's copy.
- **Credentials (spec §4):**
  - With token auth the wizard stores `token` + `salt`, never the password.
  - The password is stored only when the server needs the plain password. Over http that also needs the user's explicit consent, stored as `allow_plaintext_password`.
  - `insecure_skip_verify` is set only after the user chooses it following a TLS error.
  - Credentials never appear in error text, toasts, logs, About or screenshots. Server URLs are shown through `displayURL`.
- **Saving:** config saves are atomic (`config.Save`: temp file + rename, mode 0600).
  - Volume changes are saved 1.5 s after the last step.
  - A pending save is flushed when `App.Run` returns.
  - A broken config is renamed to `config.toml.invalid-<UTC time>` only when the wizard saves its replacement.
- **Per-server data:** `servers/<name>/` next to the config holds `state.json`, `cache/scrobbles.json` and `cache/art/`. The name is made safe for a file name.
- **Controls:**
  - Wizard keyboard: D-pad and A. Enter or the Next key moves to the next step, X deletes, and B goes back a step.
  - Settings rows: Left/Right change the value, and A changes it forwards.
  - Everything else is as in Plan 2b.
- **Screensaver:** Now Playing only. It starts after `display.screensaver_minutes` without input (0 means off), redraws every 2 s, and the press that wakes it does nothing else.
- **🔇 Sound safety:**
  - Tests and scripts use fakes, `-null` or the null device.
  - Never play sound on the user's devices without asking.
  - Desktop runs start at −30 dB.
- **Golden screenshots are exact pixels.** Regenerate them with `go test ./internal/ui -update` only in the step that says so, and look at every changed PNG.

## Review Focus

These five situations are implied by the spec but no feature test covers them. They are the most likely to bite a real user, and each gets its test in the task that owns the code:

1. **Switching servers quickly, or while a connect is still in flight.** The newest request wins. Every replaced player is stopped with its queue saved, and a late answer never shows the wrong server. Tests: `TestNewestConnectWins`, `TestSwitchStopsTheOldSessionAndKeepsTheVolume` (Task 4).
2. **A read-only or full SD card while saving settings.** The user gets a toast with the reason and no secrets, and the app keeps working. Test: `TestSaveFailureIsAToastWithoutSecrets` (Task 3).
3. **Server addresses typed every which way:** `host:port`, a trailing `/`, `/rest`, upper-case schemes, IPv6, `user:pw@` in the URL. They are normalized, or refused with a clear message. Test: `TestNormalizeURL` (Task 5).
4. **Passwords with spaces, symbols or any script.** They are kept exactly as typed, and the stored token matches them. Test: `TestWizardPasswordIsKeptExactly` (Task 5).
5. **Power off or exit right after a volume change.** The change is flushed on exit, and the file is never half-written. Test: `TestVolumeSavesAreDebouncedAndFlushedOnExit` (Task 3).

## Decisions this plan makes (the spec is silent or leaves room)

- **Per-server data folders.** IDs mean different songs on different servers, so resume state, scrobble queue and cover cache are per server. Nothing has been released yet, so there is no migration of the old single `state.json`.
- **Server names** are the URL's host, made unique (`host-2`, …). There is no rename in 2c.
- **The wizard's error recovery:**
  - A TLS error offers "Connect without checking the certificate (insecure)".
  - A token-refused-over-http error offers "Allow sending the password in plain text".
  - A wrong login offers "Change the username or password".
  - Every error offers "Try again" and "Change the address".
- **B on the wizard's first step** does nothing when the wizard is the app's first screen, because there is nowhere to go back to. Holding B still offers Exit.
- **When settings take effect:**
  - Volume, ReplayGain and scrobbling apply at once.
  - Transcode format and bitrate apply from the next connection.
  - The layout applies from the next start, because fonts and the canvas are made at start.
- **Removing servers:** removing the connected server connects to the next default. Removing the last one disconnects and starts the wizard.
- **Exit** in Settings uses the same confirmation as holding B.
- **Enter types `'\n'`**, which is Next in the wizard. A held Enter doesn't repeat. `-keys` gains `enter`, `pause:<d>`, and verbatim `'text` items (they may contain `:`).
- **The screensaver** is a dark screen with the cover dimmed, drifting 6 px every 2 s, and the title under it.
- **Stars:** a star/unstar request runs under the root screen, so leaving the screen doesn't lose the answer, and a second toggle while one is in flight is ignored. Screens shown again after a Pop get `Shown` (Starred reloads there).

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/config/servers.go` | `Clone`, `AddServer`, `RemoveServer` | 1 |
| `internal/player/player.go` | `SetReplayGain`, `SetScrobble` | 1 |
| `internal/ui/keyboard.go`, `textfield.go` | layout sets (search; text: abc/ABC/#+=), presets, Next/Show keys; shared text field | 2 |
| `internal/input/evmap.go`, `internal/devview/page.html` | Enter types `'\n'` | 2 |
| `internal/ui/session.go`, `screens_setup.go` | config ownership and saving, the start flow, Connect/Connected/ConnectFailed/Detach, input drain, `Dial`, the unreachable screen | 3 (Dial: 5) |
| `cmd/mistersubsonic/session.go`, `main.go` | the session manager; `main` wires it | 4 |
| `internal/ui/wizard.go` | the setup wizard | 5 |
| `internal/ui/screens_settings.go` | Settings, Servers, Playback/Display lists, About | 6 |
| `internal/ui/screensaver.go` | the screensaver | 7 |
| `internal/ui/star.go`, `screens_home.go`, `screens_root.go`, `app.go` | follow-ups: stars under the root, CRT Home without a stale Resume, `Shown` after Pop | 8 |
| `cmd/mistersubsonic/keys.go`, `scripts/e2e-ui.sh`, `README.md` | `-keys` enter/pause/text, the e2e wizard run, docs | 9 |

---

### Task 1: Config helpers and live player settings

**Files:**
- Create: `internal/config/servers.go`
- Modify: `internal/config/config.go` (the header `Save` writes), `internal/player/player.go`
- Test: `internal/config/servers_test.go`, `internal/player/settings_test.go` (new)

**Interfaces:**
- **Produces:**
  - `(*config.Config).Clone() *config.Config` (a deep copy).
  - `AddServer(s) string`: a unique name, which also becomes the default.
  - `RemoveServer(name) bool`: the default moves to the first remaining server.
  - `(*player.Player).SetReplayGain(mode string)` applies to tracks opened from then on. `SetScrobble(on bool)`.
  - `config.Save` writes a header that points to the README instead of the missing `config.example.toml`.

- [ ] **Step 1: Write the failing tests**

`internal/config/servers_test.go` (new file):

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneIsDeep(t *testing.T) {
	c := Default()
	c.Servers = []Server{{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"}}
	d := c.Clone()
	d.Servers[0].Name = "changed"
	d.Playback.VolumeDB = -10
	if c.Servers[0].Name != "home" || c.Playback.VolumeDB != 0 {
		t.Fatal("Clone shares state with the original")
	}
}

func TestAddServerMakesNamesUniqueAndDefault(t *testing.T) {
	c := Default()
	if got := c.AddServer(Server{Name: "music.example.com", URL: "https://music.example.com", Username: "a", Password: "p"}); got != "music.example.com" {
		t.Fatalf("first name %q", got)
	}
	if got := c.AddServer(Server{Name: "music.example.com", URL: "https://music.example.com", Username: "b", Password: "p"}); got != "music.example.com-2" {
		t.Fatalf("second name %q", got)
	}
	if c.DefaultServer != "music.example.com-2" || len(c.Servers) != 2 {
		t.Fatalf("default %q, %d servers", c.DefaultServer, len(c.Servers))
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveServerMovesTheDefault(t *testing.T) {
	c := Default()
	c.AddServer(Server{Name: "a", URL: "http://a", Username: "u", Password: "p"})
	c.AddServer(Server{Name: "b", URL: "http://b", Username: "u", Password: "p"})
	if !c.RemoveServer("b") || c.DefaultServer != "a" || len(c.Servers) != 1 {
		t.Fatalf("after removing the default: %+v", c)
	}
	if c.RemoveServer("zzz") {
		t.Fatal("removed a server that isn't there")
	}
	c.RemoveServer("a")
	if c.DefaultServer != "" || len(c.Servers) != 0 {
		t.Fatalf("after removing the last: %+v", c)
	}
}

func TestSaveHeaderPointsAtTheREADME(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") || strings.Contains(string(b), "config.example.toml") {
		t.Fatalf("header: %q", strings.SplitN(string(b), "\n", 2)[0])
	}
}
```

`internal/player/settings_test.go` (new file):

```go
package player

import (
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

func TestSetReplayGainAppliesToTheNextTrack(t *testing.T) {
	h := newHarness(t, nil) // ReplayGain off
	q := songs(2, 100)
	g := -6.0
	for i := range q {
		q[i].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
	}
	h.p.PlayNow(q, 0)
	if tr := h.playAndStart(1); tr.Gain != 1 {
		t.Fatalf("gain %v with replaygain off", tr.Gain)
	}
	h.p.SetReplayGain("track")
	h.p.Next()
	h.waitFor("second song", func() bool { return h.eng.playCount() == 2 })
	if tr := h.eng.lastPlayed(); tr.Gain < 0.50 || tr.Gain > 0.51 {
		t.Fatalf("gain %v after SetReplayGain(track), want -6 dB", tr.Gain)
	}
}

func TestSetScrobbleOff(t *testing.T) {
	h := newHarness(t, nil)
	h.p.SetScrobble(false)
	h.p.PlayNow(songs(1, 100), 0)
	a := h.playAndStart(1)
	pos := time.Duration(0)
	for range 4 * 60 {
		pos += 250 * time.Millisecond
		h.tickAt(a.ID, pos)
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(h.api.nowPlayings()) + len(h.api.submissions()); n != 0 {
		t.Fatalf("%d scrobble calls with scrobbling off", n)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/config ./internal/player`

Expected: FAIL, e.g.:

```
c.Clone undefined (type *Config has no field or method Clone)
c.AddServer undefined (type *Config has no field or method AddServer)
c.RemoveServer undefined (type *Config has no field or method RemoveServer)
h.p.SetReplayGain undefined (type *Player has no field or method SetReplayGain)
h.p.SetScrobble undefined (type *Player has no field or method SetScrobble)
```

- [ ] **Step 3: Implement**

`internal/config/servers.go` (new file):

```go
package config

import "fmt"

// Clone returns a deep copy (the UI edits a copy and saves it, while the
// connection code keeps the one it started with).
func (c *Config) Clone() *Config {
	d := *c
	d.Servers = append([]Server(nil), c.Servers...)
	return &d
}

// AddServer appends s under a unique name (s.Name, then "s.Name-2", ...)
// and makes it the default. It returns the name used.
func (c *Config) AddServer(s Server) string {
	base, name := s.Name, s.Name
	for n := 2; c.server(name) >= 0; n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	s.Name = name
	c.Servers = append(c.Servers, s)
	c.DefaultServer = name
	return name
}

// RemoveServer deletes the named server. If it was the default, the first
// remaining server becomes the default (none if it was the last).
func (c *Config) RemoveServer(name string) bool {
	i := c.server(name)
	if i < 0 {
		return false
	}
	c.Servers = append(c.Servers[:i:i], c.Servers[i+1:]...)
	if c.DefaultServer == name {
		c.DefaultServer = ""
		if len(c.Servers) > 0 {
			c.DefaultServer = c.Servers[0].Name
		}
	}
	return true
}

func (c *Config) server(name string) int {
	for i, s := range c.Servers {
		if s.Name == name {
			return i
		}
	}
	return -1
}
```

Then Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 0):

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index 3f41a2b..6f3096b 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -189,7 +189,7 @@ func Save(path string, cfg *Config) error {
 		return fmt.Errorf("config: refusing to save invalid config: %w", err)
 	}
 	var buf bytes.Buffer
-	buf.WriteString("# MiSTer Subsonic configuration. See config.example.toml for every option.\n")
+	buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in the README.\n")
 	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
 		return fmt.Errorf("config: encode: %w", err)
 	}
diff --git a/internal/player/player.go b/internal/player/player.go
index 10e2def..eb1191d 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -507,6 +507,17 @@ func (p *Player) SetVolumeDB(db float64) {
 	})
 }
 
+// SetReplayGain sets the ReplayGain mode (off, track or album) for the
+// tracks opened from now on.
+func (p *Player) SetReplayGain(mode string) {
+	p.do(func() { p.o.ReplayGain = mode })
+}
+
+// SetScrobble turns now-playing and scrobble reports on or off.
+func (p *Player) SetScrobble(on bool) {
+	p.do(func() { p.o.Scrobble = on })
+}
+
 // Resumable finds a saved queue: the server's first, then the local file.
 func (p *Player) Resumable(ctx context.Context) (*Resume, error) {
 	if pq, err := p.o.API.GetPlayQueue(ctx); err == nil && pq != nil {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/config ./internal/player`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/servers.go internal/config/servers_test.go internal/player/player.go internal/player/settings_test.go
git commit -m "config: server helpers; player: SetReplayGain, SetScrobble" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Keyboard layouts for text entry, Enter as Next, a shared text field

**Files:**
- Create: `internal/ui/textfield.go`
- Modify: `internal/ui/keyboard.go`, `internal/ui/screens_search.go`, `internal/input/evmap.go`, `internal/devview/page.html`
- Test: `internal/ui/keyboard_test.go` (new); `internal/ui/search_test.go`, `internal/ui/fixwave_test.go`, `internal/input/input_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `Keyboard` keeps its zero value as the search layouts (Latin/Cyrillic). `NewTextKeyboard(extra ...kbKey)` gives lower case, upper case and symbols, each with Space · Del · two layout keys · Next, and an optional preset row on top.
  - `(*Keyboard).Switch(i)` replaces `ToggleLayout`. On the bottom row the cursor stays on the bottom row.
  - Key actions `keyNext`, `keyInsert` (`kbKey.text`) and `keyShow`. `kbKey.to` is the target layout of a layout key.
  - `urlPresets`: `http://`, `https://`, `:4533`, `:` and `/`.
  - `(*App).drawTextField(c, r, value []rune, hint string, masked bool)`. Search uses it.
  - Enter and keypad Enter type `'\n'` (evdev and the dev viewer). Kernel autorepeat never repeats `'\n'`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/keyboard_test.go` (new file):

```go
package ui

import (
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func TestTextKeyboardLayoutsAndPresets(t *testing.T) {
	k := NewTextKeyboard(urlPresets...)
	if got := k.Focused(); got.action != keyInsert || got.text != "http://" {
		t.Fatalf("top-left key %+v, want the http:// preset", got)
	}
	k.Move(press(input.BtnDown))
	k.Move(press(input.BtnDown)) // presets -> digits -> letters, under the middle of "http://"
	if got := k.Focused(); got.r != 'b' {
		t.Fatalf("focused %q, want b", got.label)
	}
	k.Move(press(input.BtnLeft))
	for range 4 {
		k.Move(press(input.BtnDown))
	}
	if got := k.Focused(); got.action != keySpace {
		t.Fatalf("bottom-left %q, want Space", got.label)
	}
	for range 2 {
		k.Move(press(input.BtnRight))
	}
	up := k.Focused()
	if up.action != keyLayout || up.label != "ABC" {
		t.Fatalf("third bottom key %q", up.label)
	}
	k.Switch(up.to)
	if got := k.Focused(); got.label != "abc" {
		t.Fatalf("after switching to upper case the key reads %q", got.label)
	}
	k.Move(press(input.BtnUp))
	k.Move(press(input.BtnUp))
	if got := k.Focused(); got.r < 'A' || got.r > 'Z' {
		t.Fatalf("upper-case layout has %q", got.label)
	}
	rows := k.rows()
	sym := rows[len(rows)-1][3] // bottom row, fourth key
	if sym.label != "#+=" {
		t.Fatalf("fourth bottom key %q", sym.label)
	}
	k.Switch(sym.to)
	found := map[rune]bool{}
	for _, row := range k.rows() {
		for _, key := range row {
			found[key.r] = true
		}
	}
	for _, r := range `!@#$%^&*()-_=+[]{}\|;:'",.<>/?` + "`~" {
		if !found[r] {
			t.Errorf("symbols layout lacks %q", r)
		}
	}
	for _, row := range k.rows() {
		units := 0
		for _, key := range row {
			units += key.units
		}
		if units > kbUnits {
			t.Errorf("row %v is %d units wide", row, units)
		}
	}
}

func TestMaskedTextFieldHidesTheValue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	draw := func(v string, masked bool) *gfx.Canvas {
		c := gfx.NewCanvas(400, 60)
		ta.drawTextField(c, c.Bounds(), []rune(v), "hint", masked)
		return c
	}
	if !samePixels(draw("secret", true).ToRGBA(), draw("abcdef", true).ToRGBA()) {
		t.Fatal("a masked field shows something of its value")
	}
	if samePixels(draw("secret", false).ToRGBA(), draw("abcdef", false).ToRGBA()) {
		t.Fatal("an unmasked field doesn't show its value")
	}
	if samePixels(draw("", false).ToRGBA(), draw("x", false).ToRGBA()) {
		t.Fatal("an empty field looks like a filled one")
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index e58eaae..c650e71 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -80,7 +80,7 @@ func TestTranslatorKeysAndMapOverride(t *testing.T) {
 	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{Button: BtnA, Kind: Press}) {
 		t.Fatalf("default east = %v", got)
 	}
-	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{Button: BtnA, Kind: Release}) {
+	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{Button: BtnA, Kind: Release, Rune: '\n'}) {
 		t.Fatalf("enter release = %v", got)
 	}
 	if got := tr.handle(evKey, keyDown, 2); got != nil {
@@ -184,3 +184,18 @@ func TestTranslatorTypesText(t *testing.T) {
 		t.Fatalf("enter autorepeat = %v", got)
 	}
 }
+
+// Enter is A and also types '\n' (the wizard's "next field"); holding it
+// must not confirm again and again.
+func TestEnterTypesNewlineOnce(t *testing.T) {
+	tr := newTranslator(nil, nil)
+	if got := tr.handle(evKey, keyEnter, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press, Rune: '\n'}) {
+		t.Fatalf("enter = %v", got)
+	}
+	if got := tr.handle(evKey, keyKPEnter, 1); got[0].Rune != '\n' {
+		t.Fatalf("keypad enter = %v", got)
+	}
+	if got := tr.handle(evKey, keyEnter, 2); got != nil {
+		t.Fatalf("held enter repeats: %v", got)
+	}
+}
diff --git a/internal/ui/fixwave_test.go b/internal/ui/fixwave_test.go
index bb70a39..c43afa7 100644
--- a/internal/ui/fixwave_test.go
+++ b/internal/ui/fixwave_test.go
@@ -302,7 +302,7 @@ func TestKeyboardLayoutsAreBuiltOnce(t *testing.T) {
 	if &a[0][0] != &b[0][0] {
 		t.Fatal("rows() rebuilt the layout")
 	}
-	k.ToggleLayout()
+	k.Switch(1)
 	c, d := k.rows(), k.rows()
 	if &c[0][0] != &d[0][0] || &a[0][0] == &c[0][0] {
 		t.Fatal("the Cyrillic layout is not cached separately")
diff --git a/internal/ui/search_test.go b/internal/ui/search_test.go
index 1f095fc..00928f8 100644
--- a/internal/ui/search_test.go
+++ b/internal/ui/search_test.go
@@ -33,8 +33,8 @@ func TestKeyboardNavigation(t *testing.T) {
 		t.Fatalf("under j: %q", got.label)
 	}
 	move(input.BtnLeft)
-	k.ToggleLayout()
-	if got := k.Focused(); got.label != "ABC" || !k.cyrillic {
+	k.Switch(k.Focused().to)
+	if got := k.Focused(); got.label != "ABC" || k.cur != 1 {
 		t.Fatalf("after toggle: %q", got.label)
 	}
 	move(input.BtnUp) // "эюя-'.&": the key nearest the middle of the switch key
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/input ./internal/devview`

Expected: FAIL, e.g.:

```
--- FAIL: TestTranslatorKeysAndMapOverride (0.00s)
input_test.go:84: enter release = [{A 2 0}]
--- FAIL: TestEnterTypesNewlineOnce (0.00s)
k.Switch undefined (type Keyboard has no field or method Switch)
undefined: NewTextKeyboard
undefined: urlPresets
undefined: keyInsert
ta.drawTextField undefined (type *testApp has no field or method drawTextField)
```

- [ ] **Step 3: Implement**

`internal/ui/textfield.go` (new file):

```go
package ui

import (
	"strings"

	"mistersubsonic/internal/gfx"
)

// drawTextField draws a one-line text field in r: the value with a caret
// (dots instead of characters when masked), or a dim hint when it is
// empty. A long value shows its end, where the typing is.
func (a *App) drawTextField(c *gfx.Canvas, r gfx.Rect, value []rune, hint string, masked bool) {
	p := a.P
	f := a.F.Body
	c.Fill(r, colPanel)
	x := r.X + p.Margin/2
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	inner := r.Inset(p.Margin / 4)
	if len(value) == 0 {
		f.Draw(c, x, y, f.Truncate(hint, r.W-p.Margin), colDim, inner)
		return
	}
	text := string(value)
	if masked {
		text = strings.Repeat("•", len(value))
	}
	for f.Measure(text) > r.W-p.Margin-f.Ascent()/2 && len(text) > 0 {
		_, size := firstRune(text)
		text = text[size:]
	}
	end := f.Draw(c, x, y, text, colText, inner)
	c.Fill(gfx.R(end+1, y-f.Ascent(), max(f.Ascent()/8, 2), f.Ascent()+f.Descent()), colAccent)
}

// firstRune returns the second rune's offset (the first rune's size).
func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}
```

Then Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/devview/page.html b/internal/devview/page.html
index eb62546..e922fed 100644
--- a/internal/devview/page.html
+++ b/internal/devview/page.html
@@ -19,7 +19,7 @@ const held = new Set();
 function send(e, down) {
   if (down && (e.ctrlKey || e.altKey || e.metaKey)) return; // leave browser shortcuts alone
   const b = keys[e.code] || "";
-  const t = !down ? "" : e.key.length === 1 ? e.key : e.code === "Backspace" ? "\b" : "";
+  const t = !down ? "" : e.key.length === 1 ? e.key : e.code === "Backspace" ? "\b" : e.key === "Enter" ? "\n" : "";
   if (!b && !t) return;
   if (!down && !held.has(b)) return;
   e.preventDefault();
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index 7683ab4..095e575 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -64,7 +64,8 @@ var DefaultKeys = map[uint16]Button{
 
 // usLayout is what each key types on a US keyboard: {plain, with Shift}.
 var usLayout = func() map[uint16][2]rune {
-	m := map[uint16][2]rune{keySpace: {' ', ' '}, keyBackspace: {'\b', '\b'}}
+	m := map[uint16][2]rune{keySpace: {' ', ' '}, keyBackspace: {'\b', '\b'},
+		keyEnter: {'\n', '\n'}, keyKPEnter: {'\n', '\n'}}
 	rows := []struct {
 		first          uint16
 		plain, shifted string
@@ -151,7 +152,7 @@ func (t *translator) handle(typ, code uint16, value int32) []Event {
 		case 0:
 			return []Event{{Button: b, Kind: Release, Rune: r}}
 		}
-		if r != 0 {
+		if r != 0 && r != '\n' { // a held Enter must not confirm again and again
 			return []Event{{Kind: Press, Rune: r}}
 		}
 		return nil
diff --git a/internal/ui/keyboard.go b/internal/ui/keyboard.go
index 867aeca..d7f4143 100644
--- a/internal/ui/keyboard.go
+++ b/internal/ui/keyboard.go
@@ -5,13 +5,16 @@ import (
 	"mistersubsonic/internal/input"
 )
 
-// Keyboard is the on-screen keyboard (spec §8.2): letter rows for the
-// current layout (Latin or Cyrillic), digits, and a row of Space, Del,
-// the layout switch and Clear. Keys are measured in units (a letter is one
-// unit, a row is ten); Up/Down move to the key nearest the same place in
-// the next row.
+// Keyboard is the on-screen keyboard (spec §8.2): rows of characters for
+// the current layout and a bottom row of actions. Search uses Latin and
+// Cyrillic layouts (the zero Keyboard); the setup wizard uses lower case,
+// upper case and symbols (NewTextKeyboard), optionally with a row of
+// presets on top. Keys are measured in units (a character is one unit, a
+// row is ten); Up/Down move to the key nearest the same place in the next
+// row.
 type Keyboard struct {
-	cyrillic bool
+	layouts  [][][]kbKey // nil: the search layouts
+	cur      int         // current layout
 	row, col int
 }
 
@@ -21,8 +24,11 @@ const (
 	keyChar keyAction = iota
 	keySpace
 	keyDel
-	keyLayout
+	keyLayout // switch to layout kbKey.to
 	keyClear
+	keyNext   // wizard: done with this field
+	keyInsert // type kbKey.text (URL presets)
+	keyShow   // wizard: show or hide the password
 )
 
 type kbKey struct {
@@ -30,6 +36,8 @@ type kbKey struct {
 	r      rune
 	action keyAction
 	units  int
+	to     int    // keyLayout: the layout to switch to
+	text   string // keyInsert
 }
 
 const kbUnits = 10 // units per row
@@ -37,14 +45,25 @@ const kbUnits = 10 // units per row
 var (
 	latinRows    = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz-'.&"}
 	cyrillicRows = []string{"1234567890", "абвгдеёжзи", "йклмнопрст", "уфхцчшщъыь", "эюя-'.&"}
+
+	lowerRows  = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz.-_@"}
+	upperRows  = []string{"1234567890", "ABCDEFGHIJ", "KLMNOPQRST", "UVWXYZ.-_@"}
+	symbolRows = []string{"!@#$%^&*()", "-_=+[]{}\\|", ";:'\",.<>/?", "`~"}
 )
 
-// The two layouts, built once (rows is called several times per frame).
-var latinKeys, cyrillicKeys = buildKeys(latinRows, "АБВ"), buildKeys(cyrillicRows, "ABC")
+// The layouts are built once (rows is called several times per frame).
+var searchLayouts = [][][]kbKey{
+	buildKeys(latinRows, []kbKey{space(4), del(2), layoutKey("АБВ", 1), clear(2)}),
+	buildKeys(cyrillicRows, []kbKey{space(4), del(2), layoutKey("ABC", 0), clear(2)}),
+}
+
+func space(u int) kbKey                { return kbKey{label: "Space", action: keySpace, units: u} }
+func del(u int) kbKey                  { return kbKey{label: "Del", action: keyDel, units: u} }
+func clear(u int) kbKey                { return kbKey{label: "Clear", action: keyClear, units: u} }
+func layoutKey(l string, to int) kbKey { return kbKey{label: l, action: keyLayout, units: 2, to: to} }
 
-// buildKeys makes the key rows of a layout; sw labels the layout switch key
-// (it names the other layout).
-func buildKeys(src []string, sw string) [][]kbKey {
+// buildKeys makes a layout: one row of keys per string, then the bottom row.
+func buildKeys(src []string, bottom []kbKey) [][]kbKey {
 	out := make([][]kbKey, 0, len(src)+1)
 	for _, s := range src {
 		var row []kbKey
@@ -53,20 +72,54 @@ func buildKeys(src []string, sw string) [][]kbKey {
 		}
 		out = append(out, row)
 	}
-	return append(out, []kbKey{
-		{label: "Space", action: keySpace, units: 4},
-		{label: "Del", action: keyDel, units: 2},
-		{label: sw, action: keyLayout, units: 2},
-		{label: "Clear", action: keyClear, units: 2},
-	})
+	return append(out, bottom)
+}
+
+// NewTextKeyboard is the wizard's keyboard: lower case, upper case and
+// symbols, each with Space, Del, the two switches and Next. A non-empty
+// extra row (presets like "https://") goes on top of every layout.
+func NewTextKeyboard(extra ...kbKey) Keyboard {
+	next := kbKey{label: "Next", action: keyNext, units: 2}
+	layouts := [][][]kbKey{
+		buildKeys(lowerRows, []kbKey{space(2), del(2), layoutKey("ABC", 1), layoutKey("#+=", 2), next}),
+		buildKeys(upperRows, []kbKey{space(2), del(2), layoutKey("abc", 0), layoutKey("#+=", 2), next}),
+		buildKeys(symbolRows, []kbKey{space(2), del(2), layoutKey("abc", 0), layoutKey("ABC", 1), next}),
+	}
+	if len(extra) > 0 {
+		for i, l := range layouts {
+			layouts[i] = append([][]kbKey{extra}, l...)
+		}
+	}
+	return Keyboard{layouts: layouts}
+}
+
+// Presets for the URL field.
+var urlPresets = []kbKey{
+	{label: "http://", action: keyInsert, text: "http://", units: 3},
+	{label: "https://", action: keyInsert, text: "https://", units: 3},
+	{label: ":4533", action: keyInsert, text: ":4533", units: 2},
+	{label: ":", r: ':', units: 1},
+	{label: "/", r: '/', units: 1},
 }
 
 // rows is the current layout. Callers must not modify it.
 func (k *Keyboard) rows() [][]kbKey {
-	if k.cyrillic {
-		return cyrillicKeys
+	if k.layouts == nil {
+		return searchLayouts[k.cur]
+	}
+	return k.layouts[k.cur]
+}
+
+// Switch shows layout i (a layout key's target). A cursor on the bottom
+// row stays on it (layouts differ in height; the switch keys sit in the
+// same place of every bottom row).
+func (k *Keyboard) Switch(i int) {
+	bottom := k.row == len(k.rows())-1
+	k.cur = i
+	if bottom {
+		k.row = len(k.rows()) - 1
 	}
-	return latinKeys
+	k.Focused()
 }
 
 // Focused is the key under the cursor.
@@ -129,13 +182,6 @@ func (k *Keyboard) Move(e input.Event) bool {
 	return true
 }
 
-// ToggleLayout switches Latin/Cyrillic, keeping the cursor on the switch key.
-func (k *Keyboard) ToggleLayout() {
-	k.cyrillic = !k.cyrillic
-	rows := k.rows()
-	k.row, k.col = len(rows)-1, 2
-}
-
 // Height is the keyboard's height at key height h.
 func (k *Keyboard) Height(h int) int { return len(k.rows()) * h }
 
diff --git a/internal/ui/screens_search.go b/internal/ui/screens_search.go
index ffb624c..12f0013 100644
--- a/internal/ui/screens_search.go
+++ b/internal/ui/screens_search.go
@@ -230,7 +230,7 @@ func (s *SearchScreen) Handle(a *App, e input.Event) bool {
 				s.edit(a, s.query[:len(s.query)-1])
 			}
 		case keyLayout:
-			s.kb.ToggleLayout()
+			s.kb.Switch(k.to)
 		case keyClear:
 			s.edit(a, nil)
 		}
@@ -318,7 +318,7 @@ func (s *SearchScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 		kbArea = gfx.R(field.X, field.Bottom()+p.Margin/4, field.W, 0)
 		results = gfx.R(area.X, field.Bottom(), area.W, area.Bottom()-field.Bottom())
 	}
-	s.drawField(a, c, field)
+	a.drawTextField(c, field, s.query, "Type or pick letters", false)
 	keyH := kbArea.W / kbUnits
 	if !wide {
 		keyH = p.RowH
@@ -354,37 +354,6 @@ func (s *SearchScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	}
 }
 
-// drawField draws the query with a caret, or a hint when it is empty.
-func (s *SearchScreen) drawField(a *App, c *gfx.Canvas, r gfx.Rect) {
-	p := a.P
-	f := a.F.Body
-	c.Fill(r, colPanel)
-	x := r.X + p.Margin/2
-	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
-	inner := r.Inset(p.Margin / 4)
-	if len(s.query) == 0 {
-		f.Draw(c, x, y, f.Truncate("Type or pick letters", r.W-p.Margin), colDim, inner)
-		return
-	}
-	text := string(s.query)
-	// Keep the end of a long query (where the typing is) visible.
-	for f.Measure(text) > r.W-p.Margin-f.Ascent()/2 && len(text) > 0 {
-		_, size := firstRune(text)
-		text = text[size:]
-	}
-	end := f.Draw(c, x, y, text, colText, inner)
-	c.Fill(gfx.R(end+1, y-f.Ascent(), max(f.Ascent()/8, 2), f.Ascent()+f.Descent()), colAccent)
-}
-
-func firstRune(s string) (rune, int) {
-	for i, r := range s {
-		if i > 0 {
-			return r, i
-		}
-	}
-	return 0, len(s)
-}
-
 // drawStatus explains the result state: searching, an error, nothing
 // found, or (on a CRT under the keyboard) the counts.
 func (s *SearchScreen) drawStatus(a *App, c *gfx.Canvas, r gfx.Rect) {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui ./internal/input ./internal/devview`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/devview/page.html internal/input/evmap.go internal/input/input_test.go internal/ui/fixwave_test.go internal/ui/keyboard.go internal/ui/keyboard_test.go internal/ui/screens_search.go internal/ui/search_test.go internal/ui/textfield.go
git commit -m "ui: text keyboard layouts and presets, Enter types a newline, shared text field" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: The UI owns the config and asks to connect

**Files:**
- Create: `internal/ui/session.go`, `internal/ui/screens_setup.go`
- Modify: `internal/ui/app.go`, and the screens whose loads read the library or player (`screens_album.go`, `screens_artists.go`, `screens_home.go`, `screens_playlists.go`, `screens_root.go`, `screens_search.go`, `screens_starred.go`)
- Test: `internal/ui/session_test.go` (new)

**Interfaces:**
- **Consumes:** `config.Clone` (Task 1).
- **Produces:**
  - New `ui.Options` fields:
    - `ConfigPath`, `Config` (nil if missing or invalid), `ConfigErr`, `AudioErr`, `Version`.
    - `Connect func(a *App, cfg *config.Config)`. It runs off the UI goroutine and replaces any previous connection.
    - `Start` stays until Task 4 removes it.
  - `ui.ConnInfo{Server, Info, Auth}`.
  - App methods:
    - `Config()` and `Conn()`.
    - `Connect()`: detach, show "Connecting…", then call `Options.Connect` with a clone.
    - `Detach()`.
    - `Connected(info, lib, pl, art)`: installs the connection, resets the artists and star caches, shows the root screen and drains queued input.
    - `ConnectFailed(srv, err)`: shows `UnreachableScreen` with Try again, plus Switch server when there are other servers.
    - `UpdateConfig(change, soon)` and `flushConfig()`, which `Run` calls on return.
  - `displayURL` and `hint` move from `main` to `ui`.
  - Loads capture `a.Library()` and `a.Player()` on the UI goroutine.

- [ ] **Step 1: Write the failing tests**

`internal/ui/session_test.go` (new file):

```go
package ui

import (
	"errors"
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
	if messageTitle(ta) != "Setup needed" {
		t.Fatalf("no config: %q", messageTitle(ta))
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
	if !ok || len(s.actions) != 2 {
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
	if s := ta.Top().(*UnreachableScreen); len(s.actions) != 1 {
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
ta.cfg undefined (type *testApp has no field or method cfg)
ta.o.ConfigPath undefined (type Options has no field or method ConfigPath)
ta.o.Connect undefined (type Options has no field or method Connect)
ta.Detach undefined (type *testApp has no field or method Detach)
ta.o.AudioErr undefined (type Options has no field or method AudioErr)
ta.start undefined (type *testApp has no field or method start)
ta.o.ConfigErr undefined (type Options has no field or method ConfigErr)
```

- [ ] **Step 3: Implement**

`internal/ui/screens_setup.go` (new file):

```go
package ui

import (
	"fmt"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// UnreachableScreen explains why the server couldn't be reached and offers
// Try again, and Switch server when the config has another one.
type UnreachableScreen struct {
	srv     config.Server
	err     error
	actions []menuEntry
	list    List
}

func NewUnreachableScreen(srv config.Server, err error) *UnreachableScreen {
	return &UnreachableScreen{srv: srv, err: err}
}

func (s *UnreachableScreen) Title() string { return "Can't reach the server" }

func (s *UnreachableScreen) Enter(a *App) {
	s.actions = []menuEntry{{"Try again", func(a *App) { a.Connect() }}}
	if cfg := a.Config(); cfg != nil && len(cfg.Servers) > 1 {
		s.actions = append(s.actions, menuEntry{"Switch server", func(a *App) { a.openMenu("Switch to", switchEntries(a, s.srv.Name)) }})
	}
}

// switchEntries connects to one of the other servers, making it the default.
func switchEntries(a *App, current string) []menuEntry {
	var out []menuEntry
	for _, srv := range a.Config().Servers {
		if srv.Name == current {
			continue
		}
		name := srv.Name
		out = append(out, menuEntry{name + " — " + displayURL(srv.URL), func(a *App) {
			a.UpdateConfig(func(c *config.Config) { c.DefaultServer = name }, false)
			a.Connect()
		}})
	}
	return out
}

func (s *UnreachableScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.actions)) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && len(s.actions) > 0 {
		s.actions[s.list.Focus].run(a)
		return true
	}
	return false
}

func (s *UnreachableScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	msg := fmt.Sprintf("%s: %s.", displayURL(s.srv.URL), subsonic.Classify(s.err))
	if h := hint(s.err); h != "" {
		msg += "\n" + h
	}
	y := area.Y + p.Margin
	for _, line := range wrap(f, msg, area.W-2*p.Margin) {
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), line, colText, area)
		y += f.Height()
	}
	y += p.Margin / 2
	s.list.Draw(c, gfx.R(area.X, y, area.W, area.Bottom()-y), len(s.actions), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: s.actions[i].label, col: colAccent, focused: focused})
	})
}
```

`internal/ui/session.go` (new file):

```go
package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// The session: the UI owns the configuration (edited by the wizard and
// Settings, saved off the UI goroutine) and asks the app around it to
// connect with it (Options.Connect). The app answers with Connected or
// ConnectFailed on the UI goroutine.

// ConnInfo describes the live connection (for the insecure badge and About).
type ConnInfo struct {
	Server config.Server
	Info   *subsonic.ServerInfo
	Auth   subsonic.AuthMethod
}

const saveDelay = 1500 * time.Millisecond // volume steps are saved together

// Config is the current configuration (nil before a valid one exists).
// Callers must not keep it across screens; change it with UpdateConfig.
func (a *App) Config() *config.Config { return a.cfg }

// Conn is the live connection, if any.
func (a *App) Conn() (ConnInfo, bool) { return a.conn, a.conn.Server.Name != "" }

// start picks the first screen: a problem to explain, or connecting.
func (a *App) start() {
	switch {
	case a.o.AudioErr != nil:
		a.Replace(NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", a.o.AudioErr, a.o.ConfigPath), nil))
	case a.cfg == nil && errors.Is(a.o.ConfigErr, config.ErrNotFound):
		a.Replace(NewMessageScreen("Setup needed", "No config file yet. Create "+a.o.ConfigPath+" with a [[server]] section (name, url, username, password), then restart.", nil))
	case a.cfg == nil:
		a.Replace(NewMessageScreen("Setup needed", "The config file has a problem:\n"+fmt.Sprint(a.o.ConfigErr), nil))
	case len(a.cfg.Servers) == 0:
		a.Replace(NewMessageScreen("Setup needed", "Add a [[server]] to "+a.o.ConfigPath+".", nil))
	default:
		a.Connect()
	}
}

// Connect drops the current connection and asks for one to the config's
// active server. Call on the UI goroutine.
func (a *App) Connect() {
	srv, ok := a.cfg.ActiveServer()
	if !ok || a.o.Connect == nil {
		return
	}
	a.Detach()
	a.Replace(NewMessageScreen("Connecting…", "Connecting to "+displayURL(srv.URL)+"…", nil))
	a.o.Connect(a, a.cfg.Clone())
}

// Detach forgets the library, player and art of the old connection.
func (a *App) Detach() {
	a.o.Library, a.o.Player, a.o.Art = nil, nil, nil
	a.conn = ConnInfo{}
	a.insecure = false
	a.dirty = true
}

// Connected installs a new connection and shows the library. The caches
// of the previous connection (artists, star changes) are dropped.
func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
	a.Attach(lib, pl, art)
	a.conn = info
	a.SetInsecure(info.Server.InsecureSkipVerify)
	a.artists, a.stars, a.starGen = nil, map[starKey]bool{}, 0
	a.Replace(NewRootScreen(a.P))
	a.drainInput()
}

// ConnectFailed shows why the server couldn't be reached and what to do.
func (a *App) ConnectFailed(srv config.Server, err error) {
	a.Replace(NewUnreachableScreen(srv, err))
}

// drainInput drops queued key events and held-button state (spec §8.4:
// after the wizard, presses meant for it must not act on the next screen).
func (a *App) drainInput() {
	for {
		select {
		case <-a.in:
		default:
			a.rep = input.Repeater{}
			a.swallowed = map[input.Button]bool{}
			a.bDown = time.Time{}
			return
		}
	}
}

// UpdateConfig changes the configuration and saves it: now, or with soon
// after a short pause (so a run of volume steps is one write).
func (a *App) UpdateConfig(change func(c *config.Config), soon bool) {
	if a.cfg == nil {
		a.cfg = config.Default()
	}
	change(a.cfg)
	if soon {
		a.saveAt = a.o.Now().Add(saveDelay)
		return
	}
	a.saveAt = time.Time{}
	a.saveConfig()
}

// saveConfig writes a copy of the configuration off the UI goroutine. The
// writes are serialized; a change during a write is saved right after it.
func (a *App) saveConfig() {
	if a.o.ConfigPath == "" || a.cfg == nil {
		return
	}
	if a.saving {
		a.saveAgain = true
		return
	}
	a.saving = true
	cfg, path := a.cfg.Clone(), a.o.ConfigPath
	go func() {
		a.saveMu.Lock()
		err := config.Save(path, cfg)
		a.saveMu.Unlock()
		a.Post(func() {
			a.saving = false
			if err != nil {
				log.Printf("config: %v", err)
				a.Toast("Couldn't save the settings: %s", saveProblem(err))
			}
			if a.saveAgain {
				a.saveAgain = false
				a.saveConfig()
			}
		})
	}()
}

// saveProblem is the short, secret-free reason a save failed.
func saveProblem(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "invalid settings"
}

// flushConfig saves a pending change before exit (on the UI goroutine;
// waits for a write in progress).
func (a *App) flushConfig() {
	if a.saveAt.IsZero() && !a.saveAgain && !a.saving {
		return
	}
	if a.o.ConfigPath == "" || a.cfg == nil {
		return
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if err := config.Save(a.o.ConfigPath, a.cfg.Clone()); err != nil {
		log.Printf("config: %v", err)
	}
	a.saveAt = time.Time{}
}

// displayURL is raw without credentials, safe to show on screen or log.
func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<server>"
	}
	u.User = nil
	return u.String()
}

// hint is what to try for a connection error.
func hint(err error) string {
	switch subsonic.Classify(err) {
	case subsonic.KindTLS:
		return "For a self-signed certificate set ca_file (or insecure_skip_verify) in the config."
	case subsonic.KindAuth:
		return "Check the username and password in the config."
	case subsonic.KindPlaintextRefused:
		return "This server needs the plain password: use https, or set allow_plaintext_password."
	case subsonic.KindUnreachable, subsonic.KindTimeout:
		return "Check that the server is running and the URL is right."
	}
	return ""
}
```

Then Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index f52d70c..c128b13 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -4,9 +4,11 @@ import (
 	"context"
 	"fmt"
 	"strings"
+	"sync"
 	"time"
 
 	"mistersubsonic/internal/art"
+	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
 	"mistersubsonic/internal/player"
@@ -85,11 +87,23 @@ type Options struct {
 	Player  Player
 	Art     ArtSource
 	Inputs  []<-chan input.Event
-	// Start runs on the UI goroutine before the first frame; it pushes the
-	// first screen (main uses it for the connect-or-error flow).
+	// Start, if set, pushes the first screen instead of the built-in flow
+	// (a problem to explain, or connecting with Config).
 	Start         func(a *App)
 	FallbackFonts string
 	Now           func() time.Time
+
+	// ConfigPath is where the configuration is saved. Config is the loaded
+	// one (nil if missing or invalid: ConfigErr says why); the UI owns it.
+	ConfigPath string
+	Config     *config.Config
+	ConfigErr  error
+	AudioErr   error  // the sound device couldn't be opened
+	Version    string // shown in Settings → About
+	// Connect starts a connection to cfg's active server, off the UI
+	// goroutine, replacing any previous one; it answers with
+	// a.Connected or a.ConnectFailed (through a.Post).
+	Connect func(a *App, cfg *config.Config)
 }
 
 // Fonts used by the screens.
@@ -157,6 +171,12 @@ type App struct {
 	stars     map[starKey]bool       // star changes made in this session
 	starGen   int                    // bumped by every successful star change
 	artists   []subsonic.ArtistIndex // getArtists, fetched once per connection
+	cfg       *config.Config
+	conn      ConnInfo
+	saveAt    time.Time  // a debounced config save is due (zero: none)
+	saving    bool       // a save is being written
+	saveAgain bool       // the config changed during that write
+	saveMu    sync.Mutex // one writer of the config file at a time
 	rep       input.Repeater
 	in        chan input.Event
 	post      chan func()
@@ -172,7 +192,7 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}}
+		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, cfg: o.Config}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -354,10 +374,14 @@ func (a *App) Toast(format string, args ...any) {
 	a.dirty = true
 }
 
-// Run drives the UI until ctx ends or the user exits.
+// Run drives the UI until ctx ends or the user exits. A pending
+// configuration change is saved before it returns.
 func (a *App) Run(ctx context.Context) error {
+	defer a.flushConfig()
 	if a.o.Start != nil {
 		a.o.Start(a)
+	} else {
+		a.start()
 	}
 	timer := time.NewTimer(time.Hour)
 	defer timer.Stop()
@@ -407,6 +431,7 @@ func (a *App) untilWake() time.Duration {
 		consider(now.Add(marqueeFrame))
 	}
 	consider(a.mqWake)
+	consider(a.saveAt)
 	if !a.bDown.IsZero() {
 		consider(a.bDown.Add(exitHold))
 	}
@@ -449,6 +474,10 @@ func (a *App) onWake() {
 			a.dirty = true
 		}
 	}
+	if !a.saveAt.IsZero() && !now.Before(a.saveAt) {
+		a.saveAt = time.Time{}
+		a.saveConfig()
+	}
 	if a.animate || (!a.mqWake.IsZero() && !now.Before(a.mqWake)) {
 		a.dirty = true
 	}
diff --git a/internal/ui/screens_album.go b/internal/ui/screens_album.go
index 3dee638..31c192f 100644
--- a/internal/ui/screens_album.go
+++ b/internal/ui/screens_album.go
@@ -30,7 +30,8 @@ func (s *AlbumScreen) Enter(a *App) { s.load(a) }
 
 func (s *AlbumScreen) load(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetAlbum(ctx, s.stub.ID) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetAlbum(ctx, s.stub.ID) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
diff --git a/internal/ui/screens_artists.go b/internal/ui/screens_artists.go
index 37b57bc..51fbab1 100644
--- a/internal/ui/screens_artists.go
+++ b/internal/ui/screens_artists.go
@@ -54,7 +54,8 @@ func (s *ArtistsScreen) Enter(a *App) {
 
 func (s *ArtistsScreen) load(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtists(ctx) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetArtists(ctx) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
@@ -137,7 +138,8 @@ func (s *ArtistScreen) Enter(a *App) { s.load(a) }
 
 func (s *ArtistScreen) load(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtist(ctx, s.artist.ID) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetArtist(ctx, s.artist.ID) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index 580c112..f9f6621 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -60,7 +60,8 @@ func (s *HomeScreen) Enter(a *App) {
 	if a.Player() == nil {
 		return
 	}
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
+	pl := a.Player() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return pl.Resumable(ctx) }, func(v any, err error) {
 		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
 			s.resume = r
 			if s.list.Focus > 0 {
@@ -143,8 +144,9 @@ func (s *AlbumListScreen) loadMore(a *App) {
 	s.loading, s.err = true, nil
 	q := s.q
 	q.Size, q.Offset = albumPage, len(s.view.albums)
+	lib := a.Library() // read on the UI goroutine; the load runs off it
 	a.Load(s, func(ctx context.Context) (any, error) {
-		return a.Library().GetAlbumList2(ctx, q)
+		return lib.GetAlbumList2(ctx, q)
 	}, func(v any, err error) {
 		s.loading = false
 		if err != nil {
@@ -222,7 +224,8 @@ func (s *GenresScreen) Title() string { return "Genres" }
 
 func (s *GenresScreen) Enter(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetGenres(ctx) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetGenres(ctx) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
diff --git a/internal/ui/screens_playlists.go b/internal/ui/screens_playlists.go
index 0b7f451..fd2bd8f 100644
--- a/internal/ui/screens_playlists.go
+++ b/internal/ui/screens_playlists.go
@@ -23,7 +23,8 @@ func (s *PlaylistsScreen) Title() string { return "Playlists" }
 
 func (s *PlaylistsScreen) Enter(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetPlaylists(ctx) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetPlaylists(ctx) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
@@ -110,7 +111,8 @@ func (s *PlaylistScreen) Title() string { return "Playlist" }
 
 func (s *PlaylistScreen) Enter(a *App) {
 	s.err = nil
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetPlaylist(ctx, s.stub.ID) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetPlaylist(ctx, s.stub.ID) }, func(v any, err error) {
 		if err != nil {
 			s.err = err
 			return
diff --git a/internal/ui/screens_root.go b/internal/ui/screens_root.go
index 18e2c88..9e6a417 100644
--- a/internal/ui/screens_root.go
+++ b/internal/ui/screens_root.go
@@ -197,7 +197,8 @@ func (s *FeedScreen) Enter(a *App) {
 	if a.Player() == nil {
 		return
 	}
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
+	pl := a.Player() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return pl.Resumable(ctx) }, func(v any, err error) {
 		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
 			s.resume = r
 			s.row++ // keep the focus on the same row of covers
@@ -207,8 +208,9 @@ func (s *FeedScreen) Enter(a *App) {
 
 func (s *FeedScreen) load(a *App, r *feedRow) {
 	r.err = nil
+	lib, q := a.Library(), subsonic.AlbumListQuery{Type: r.listType, Size: feedPage} // read on the UI goroutine
 	a.Load(s, func(ctx context.Context) (any, error) {
-		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: r.listType, Size: feedPage})
+		return lib.GetAlbumList2(ctx, q)
 	}, func(v any, err error) {
 		if err != nil {
 			r.err = err
diff --git a/internal/ui/screens_search.go b/internal/ui/screens_search.go
index 12f0013..1c09ebe 100644
--- a/internal/ui/screens_search.go
+++ b/internal/ui/screens_search.go
@@ -111,8 +111,9 @@ func (s *SearchScreen) search(a *App) {
 	q := strings.TrimSpace(string(s.query))
 	gen := s.gen
 	s.searching, s.err = true, nil
+	lib := a.Library() // read on the UI goroutine; the load runs off it
 	s.cancel = a.LoadCancel(s, func(ctx context.Context) (any, error) {
-		return a.Library().Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchPage, AlbumCount: searchPage, SongCount: searchPage})
+		return lib.Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchPage, AlbumCount: searchPage, SongCount: searchPage})
 	}, func(v any, err error) {
 		if gen != s.gen {
 			return
@@ -177,7 +178,8 @@ func (s *SearchScreen) loadMore(a *App) {
 	default:
 		sq.SongCount, sq.SongOffset = searchPage, len(s.songs.songs)
 	}
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().Search3(ctx, q, sq) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.Search3(ctx, q, sq) }, func(v any, err error) {
 		if shown != s.shown { // the results this page belongs to are gone
 			return
 		}
diff --git a/internal/ui/screens_starred.go b/internal/ui/screens_starred.go
index 16df5fe..259e0b9 100644
--- a/internal/ui/screens_starred.go
+++ b/internal/ui/screens_starred.go
@@ -32,7 +32,8 @@ func (d *starredData) load(a *App, s Screen) {
 		return
 	}
 	d.loading, d.err, d.gen = true, nil, a.starGen
-	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetStarred2(ctx) }, func(v any, err error) {
+	lib := a.Library() // read on the UI goroutine; the load runs off it
+	a.Load(s, func(ctx context.Context) (any, error) { return lib.GetStarred2(ctx) }, func(v any, err error) {
 		d.loading = false
 		if err != nil {
 			d.err = err
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/screens_album.go internal/ui/screens_artists.go internal/ui/screens_home.go internal/ui/screens_playlists.go internal/ui/screens_root.go internal/ui/screens_search.go internal/ui/screens_setup.go internal/ui/screens_starred.go internal/ui/session.go internal/ui/session_test.go
git commit -m "ui: own the config, save it off the UI goroutine, connect through Options.Connect" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: The session manager in the app

**Files:**
- Create: `cmd/mistersubsonic/session.go`
- Modify: `cmd/mistersubsonic/main.go`, `internal/ui/app.go` (drop `Options.Start`)
- Test: `cmd/mistersubsonic/session_test.go` (new); `cmd/mistersubsonic/main_test.go` (modified)

**Interfaces:**
- **Consumes:** `ui.Options.Connect`, `App.Connected` and `App.ConnectFailed` (Task 3).
- **Produces:**
  - `newSessions(ctx, eng, dataDir, volume)`, with the methods `connect(sessionUI, *config.Config)` and `close()`.
    - The newest connect wins: a superseded connect builds nothing.
    - A replaced player is stopped (it saves the queue and stops the engine), and its loader is closed.
    - The next player starts at the last one's volume.
  - `serverDir(name)` makes `servers/<safe name>/`.
  - `main` passes the config, its error, the audio error and `version` to `ui.New`, and connects through the manager. `var version = "dev"` is set by `-ldflags -X main.version=…`.
  - `ui.Options.Start` is removed.

- [ ] **Step 1: Write the failing tests**

`cmd/mistersubsonic/session_test.go` (new file):

```go
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
```

Then update the existing tests. Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index 2627917..84dc69a 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -8,27 +8,12 @@ import (
 	"net/http/httptest"
 	"os"
 	"path/filepath"
-	"strings"
 	"testing"
 	"time"
 
 	"mistersubsonic/internal/audio"
-	"mistersubsonic/internal/config"
 )
 
-func TestDisplayURLHidesCredentials(t *testing.T) {
-	got := displayURL("http://alice:s3cret@host:4533/nav")
-	if strings.Contains(got, "s3cret") || strings.Contains(got, "alice") || !strings.Contains(got, "host:4533") {
-		t.Fatalf("displayURL leaked or lost host: %q", got)
-	}
-	if got := displayURL("http://host:4533/nav"); got != "http://host:4533/nav" {
-		t.Fatalf("plain URL changed: %q", got)
-	}
-	if got := displayURL("http://[bad"); got != "<server>" {
-		t.Fatalf("garbage: %q", got)
-	}
-}
-
 func TestNoAudioDeviceShowsMessage(t *testing.T) {
 	old := openDevice
 	openDevice = func(audio.DeviceOptions) (audio.Output, error) { return nil, errors.New("boom") }
@@ -77,13 +62,6 @@ func TestStartVolume(t *testing.T) {
 	}
 }
 
-func TestConfigMessageNamesNoMissingFile(t *testing.T) {
-	msg := configMessage("/x/config.toml", config.ErrNotFound)
-	if strings.Contains(msg, "config.example.toml") || !strings.Contains(msg, "/x/config.toml") {
-		t.Fatalf("message %q", msg)
-	}
-}
-
 func nullDevice(t *testing.T) {
 	t.Helper()
 	old := openDevice
@@ -129,7 +107,7 @@ func TestConnectBuildsSessionAndExitsCleanly(t *testing.T) {
 	if err != nil {
 		t.Fatalf("run: %v", err)
 	}
-	if _, err := os.Stat(filepath.Join(dir, "cache", "art")); err != nil {
+	if _, err := os.Stat(filepath.Join(dir, "servers", "x", "cache", "art")); err != nil {
 		t.Fatalf("art cache not created after connect: %v", err)
 	}
 }
@@ -148,7 +126,7 @@ func TestExitDuringConnectBuildsNothing(t *testing.T) {
 	}
 	close(gate)
 	time.Sleep(200 * time.Millisecond)
-	if _, err := os.Stat(filepath.Join(dir, "cache", "art")); err == nil {
-		t.Fatal("art cache built after the app exited")
+	if _, err := os.Stat(filepath.Join(dir, "servers")); err == nil {
+		t.Fatal("a session was built after the app exited")
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./cmd/mistersubsonic ./internal/ui`

Expected: FAIL, e.g.:

```
undefined: sessions
undefined: newSessions
```

- [ ] **Step 3: Implement**

`cmd/mistersubsonic/session.go` (new file):

```go
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
	"mistersubsonic/internal/ui"
)

// sessionUI is what sessions needs from the UI (*ui.App; a fake in tests).
type sessionUI interface {
	Post(f func())
	ArtReady(k art.Key)
	Connected(info ui.ConnInfo, lib ui.Library, pl ui.Player, art ui.ArtSource)
	ConnectFailed(srv config.Server, err error)
}

// sessions owns the connection to a server: the client, the player and the
// art loader. connect replaces the current session with a new one for the
// config's active server; the newest request wins, and every session it
// replaces is stopped (the play queue saved) and closed. Each server keeps
// its data (resume state, scrobble queue, cover cache) in its own folder:
// IDs mean different songs on different servers.
type sessions struct {
	ctx     context.Context // the app's: connects and players end with it
	eng     *audio.Engine
	dataDir string

	mu      sync.Mutex
	gen     int // bumped by each connect; older ones are stale
	cur     *session
	volume  float64 // start volume of the next player (the last one's, after the first)
	closing bool
}

type session struct {
	pl     *player.Player
	loader *art.Loader
	cancel context.CancelFunc
	done   chan struct{} // closed when pl.Run returns
}

const connectTimeout = 10 * time.Second

func newSessions(ctx context.Context, eng *audio.Engine, dataDir string, volume float64) *sessions {
	return &sessions{ctx: ctx, eng: eng, dataDir: dataDir, volume: volume}
}

// connect is ui.Options.Connect.
func (m *sessions) connect(a sessionUI, cfg *config.Config) {
	srv, ok := cfg.ActiveServer()
	if !ok {
		return
	}
	server := *srv
	m.mu.Lock()
	m.gen++
	gen, old := m.gen, m.cur
	m.cur = nil
	m.mu.Unlock()
	go func() {
		if old != nil {
			m.stop(old)
		}
		c, info, err := dial(m.ctx, server)
		if err != nil {
			a.Post(func() {
				if m.current(gen) {
					a.ConnectFailed(server, err)
				}
			})
			return
		}
		if !m.current(gen) {
			return // superseded or shutting down: build nothing
		}
		s := m.build(a, c, server, cfg.Playback, cfg.Cache)
		m.mu.Lock()
		if m.closing || gen != m.gen {
			m.mu.Unlock()
			m.stop(s) // superseded before it was shown
			return
		}
		m.cur = s
		m.mu.Unlock()
		a.Post(func() {
			if m.current(gen) {
				a.Connected(ui.ConnInfo{Server: server, Info: info, Auth: c.AuthMethod()}, c, s.pl, s.loader)
			}
		})
	}()
}

func (m *sessions) current(gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return gen == m.gen && !m.closing
}

// dial makes a client for srv and checks it answers (ping, auth fallback).
func dial(ctx context.Context, srv config.Server) (*subsonic.Client, *subsonic.ServerInfo, error) {
	c, err := subsonic.New(subsonic.Options{
		BaseURL: srv.URL,
		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
			Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
	})
	if err != nil {
		return nil, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	info, err := c.Connect(cctx)
	if err != nil {
		return nil, nil, err
	}
	return c, info, nil
}

// build makes and starts the player and art loader (disk I/O: off the UI
// goroutine).
func (m *sessions) build(a sessionUI, c *subsonic.Client, srv config.Server, pb config.Playback, cc config.Cache) *session {
	dir := m.serverDir(srv.Name)
	disk, err := cache.Open(filepath.Join(dir, "cache", "art"), int64(cc.CoverArtMB)<<20)
	if err != nil {
		log.Printf("art cache disabled: %v", err)
		disk = nil
	}
	m.mu.Lock()
	vol := m.volume
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(m.ctx)
	s := &session{
		loader: art.New(art.Options{Fetch: art.HTTPFetcher(c), Disk: disk, Ready: a.ArtReady}),
		pl: player.New(player.Options{
			Engine: m.eng, API: c,
			Open: player.NewOpener(c, player.StreamSettings{
				TranscodeFormat: pb.TranscodeFormat, TranscodeBitrate: pb.TranscodeBitrate,
				WindowBytes: int64(pb.BufferMB) << 20,
			}),
			ReplayGain: pb.ReplayGain, Scrobble: pb.Scrobble, VolumeDB: vol,
			ResumePath: filepath.Join(dir, "state.json"), ScrobblePath: filepath.Join(dir, "cache", "scrobbles.json"),
		}),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() { s.pl.Run(ctx); close(s.done) }()
	return s
}

// serverDir is where a server's data lives: servers/<name> in the data
// folder, with the name made safe for a file name.
func (m *sessions) serverDir(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r < ' ':
			return '_'
		}
		return r
	}, name)
	if safe == "" || safe == "." || safe == ".." {
		safe = "_"
	}
	dir := filepath.Join(m.dataDir, "servers", safe)
	os.MkdirAll(dir, 0o755)
	return dir
}

// stop ends a session: the player saves its queue and stops the engine,
// then the art loader closes. The session's volume carries to the next.
func (m *sessions) stop(s *session) {
	vol := s.pl.State().VolumeDB
	s.cancel()
	<-s.done
	s.loader.Close()
	m.mu.Lock()
	m.volume = vol
	m.mu.Unlock()
}

// close stops the current session and any connect still in flight from
// starting one (on exit).
func (m *sessions) close() {
	m.mu.Lock()
	m.closing = true
	s := m.cur
	m.cur = nil
	m.mu.Unlock()
	if s != nil {
		m.stop(s)
	}
}
```

Then Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index d21fbaf..b8cd232 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -7,35 +7,31 @@ package main
 
 import (
 	"context"
-	"errors"
 	"flag"
 	"fmt"
 	"log"
 	"math"
-	"net/url"
 	"os"
 	"os/signal"
 	"path/filepath"
 	"runtime"
-	"sync"
 	"syscall"
 	"time"
 
-	"mistersubsonic/internal/art"
 	"mistersubsonic/internal/audio"
-	"mistersubsonic/internal/cache"
 	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/devview"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
-	"mistersubsonic/internal/player"
-	"mistersubsonic/internal/subsonic"
 	"mistersubsonic/internal/ui"
 )
 
 // openDevice is replaceable so tests never touch a sound card.
 var openDevice = audio.OpenDevice
 
+// version is set by the release build (-ldflags "-X main.version=v1.0.0").
+var version = "dev"
+
 type flags struct {
 	config, display, viewerAddr, frames, profile, fbdev, keys string
 	null                                                      bool
@@ -161,21 +157,6 @@ func run(f flags) error {
 		fmt.Println("starting at -30 dB (use -volume to change)")
 	}
 
-	// The player and art loader are built by the connect goroutine (disk I/O
-	// stays off the UI goroutine); sess is guarded by smu, and closing stops
-	// a connect that outlives the app from building anything.
-	type session struct {
-		pl      *player.Player
-		loader  *art.Loader
-		started bool // pl.Run launched and app attached
-	}
-	var (
-		smu     sync.Mutex
-		sess    *session
-		closing bool
-		app     *ui.App
-	)
-
 	dev := cfg.Playback.ALSADevice
 	if dev == "default" {
 		dev = ""
@@ -188,134 +169,21 @@ func run(f flags) error {
 		defer eng.Close()
 	}
 
-	pctx, cancelPlayer := context.WithCancel(context.Background())
-	playerDone := make(chan struct{})
-	defer func() {
-		smu.Lock()
-		closing = true
-		s := sess
-		smu.Unlock()
-		cancelPlayer()
-		if s != nil {
-			if s.started { // only then is pl.Run running to close playerDone
-				<-playerDone
-			}
-			s.loader.Close()
-		}
-	}()
+	// Sessions (client, player, art) are built off the UI goroutine and
+	// replaced on a server switch; the last one is stopped before the
+	// engine and device close (defers run last-in first-out).
+	sess := newSessions(ctx, eng, dataDir, vol)
+	defer sess.close()
 
-	// connectServer runs off the UI goroutine; the caller attaches the result.
-	connectServer := func() (*subsonic.Client, error) {
-		srv, _ := cfg.ActiveServer()
-		c, err := subsonic.New(subsonic.Options{
-			BaseURL: srv.URL,
-			Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
-				Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
-			CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
-		})
-		if err != nil {
-			return nil, err
-		}
-		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
-		defer cancel()
-		if _, err := c.Connect(cctx); err != nil {
-			return nil, err
-		}
-		return c, nil
-	}
-	// buildSession runs on the connect goroutine: cache.Open and player.New
-	// touch the disk. It returns nil if the app is already shutting down.
-	buildSession := func(a *ui.App, c *subsonic.Client) *session {
-		smu.Lock()
-		s, down := sess, closing
-		smu.Unlock()
-		if down || s != nil {
-			return s
-		}
-		disk, err := cache.Open(filepath.Join(dataDir, "cache", "art"), int64(cfg.Cache.CoverArtMB)<<20)
-		if err != nil {
-			log.Printf("art cache disabled: %v", err)
-			disk = nil
-		}
-		s = &session{
-			loader: art.New(art.Options{Fetch: art.HTTPFetcher(c), Disk: disk, Ready: a.ArtReady}),
-			pl: player.New(player.Options{
-				Engine: eng, API: c,
-				Open: player.NewOpener(c, player.StreamSettings{
-					TranscodeFormat: cfg.Playback.TranscodeFormat, TranscodeBitrate: cfg.Playback.TranscodeBitrate,
-					WindowBytes: int64(cfg.Playback.BufferMB) << 20,
-				}),
-				ReplayGain: cfg.Playback.ReplayGain, Scrobble: cfg.Playback.Scrobble, VolumeDB: vol,
-				ResumePath: filepath.Join(dataDir, "state.json"), ScrobblePath: filepath.Join(dataDir, "cache", "scrobbles.json"),
-			}),
-		}
-		smu.Lock()
-		if closing || sess != nil {
-			existing := sess
-			smu.Unlock()
-			s.loader.Close()
-			return existing
-		}
-		sess = s
-		smu.Unlock()
-		return s
-	}
-	// startSession runs on the UI goroutine and only does the cheap part.
-	startSession := func(a *ui.App, c *subsonic.Client, s *session) {
-		smu.Lock()
-		defer smu.Unlock()
-		if closing || s.started {
-			return
-		}
-		s.started = true
-		go func() { s.pl.Run(pctx); close(playerDone) }()
-		a.Attach(c, s.pl, s.loader)
-		srv, _ := cfg.ActiveServer()
-		a.SetInsecure(srv.InsecureSkipVerify)
+	var loaded *config.Config
+	if cfgErr == nil {
+		loaded = cfg
 	}
-
 	app, err := ui.New(ui.Options{
 		Display: disp, Profile: prof, Inputs: inputs,
 		FallbackFonts: filepath.Join(dataDir, "fonts"),
-		Start: func(a *ui.App) {
-			var connect func()
-			connect = func() {
-				if audioErr != nil {
-					a.Replace(ui.NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", audioErr, f.config), nil))
-					return
-				}
-				if cfgErr != nil {
-					a.Replace(ui.NewMessageScreen("Setup needed", configMessage(f.config, cfgErr), nil))
-					return
-				}
-				if _, ok := cfg.ActiveServer(); !ok {
-					a.Replace(ui.NewMessageScreen("Setup needed", "Add a [[server]] to "+f.config+".", nil))
-					return
-				}
-				a.Replace(ui.NewMessageScreen("Connecting…", "Connecting to the server…", nil))
-				go func() {
-					c, err := connectServer()
-					var s *session
-					if err == nil {
-						s = buildSession(a, c)
-					}
-					a.Post(func() {
-						if err != nil {
-							srv, _ := cfg.ActiveServer()
-							a.Replace(ui.NewMessageScreen("Can't reach the server",
-								fmt.Sprintf("%s: %s.\n%s", displayURL(srv.URL), subsonic.Classify(err), hint(err)), connect))
-							return
-						}
-						if s == nil { // shutting down
-							return
-						}
-						startSession(a, c, s)
-						a.Replace(ui.NewRootScreen(a.P))
-					})
-				}()
-			}
-			connect()
-		},
+		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
+		Connect: func(a *ui.App, c *config.Config) { sess.connect(a, c) },
 	})
 	if err != nil {
 		return err
@@ -324,34 +192,3 @@ func run(f flags) error {
 	stop() // a second Ctrl-C now kills the process instead of waiting out the shutdown below
 	return err
 }
-
-// displayURL is raw without credentials, safe to show on screen or log.
-func displayURL(raw string) string {
-	u, err := url.Parse(raw)
-	if err != nil || u.Host == "" {
-		return "<server>"
-	}
-	u.User = nil
-	return u.String()
-}
-
-func configMessage(path string, err error) string {
-	if errors.Is(err, config.ErrNotFound) {
-		return "No config file yet. Create " + path + " with a [[server]] section (name, url, username, password), then restart."
-	}
-	return "The config file has a problem:\n" + err.Error()
-}
-
-func hint(err error) string {
-	switch subsonic.Classify(err) {
-	case subsonic.KindTLS:
-		return "For a self-signed certificate set ca_file (or insecure_skip_verify) in the config."
-	case subsonic.KindAuth:
-		return "Check the username and password in the config."
-	case subsonic.KindPlaintextRefused:
-		return "This server needs the plain password: use https, or set allow_plaintext_password."
-	case subsonic.KindUnreachable, subsonic.KindTimeout:
-		return "Check that the server is running and the URL is right."
-	}
-	return ""
-}
diff --git a/internal/ui/app.go b/internal/ui/app.go
index c128b13..1616960 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -81,15 +81,12 @@ type owner interface {
 }
 
 type Options struct {
-	Display gfx.Display
-	Profile Profile
-	Library Library
-	Player  Player
-	Art     ArtSource
-	Inputs  []<-chan input.Event
-	// Start, if set, pushes the first screen instead of the built-in flow
-	// (a problem to explain, or connecting with Config).
-	Start         func(a *App)
+	Display       gfx.Display
+	Profile       Profile
+	Library       Library
+	Player        Player
+	Art           ArtSource
+	Inputs        []<-chan input.Event
 	FallbackFonts string
 	Now           func() time.Time
 
@@ -378,11 +375,7 @@ func (a *App) Toast(format string, args ...any) {
 // configuration change is saved before it returns.
 func (a *App) Run(ctx context.Context) error {
 	defer a.flushConfig()
-	if a.o.Start != nil {
-		a.o.Start(a)
-	} else {
-		a.start()
-	}
+	a.start()
 	timer := time.NewTimer(time.Hour)
 	defer timer.Stop()
 	for !a.quit {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./cmd/mistersubsonic ./internal/ui`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `make e2e`. Expected: `e2e ok: …` and `e2e-ui ok: …`: the app connects through the manager.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go cmd/mistersubsonic/main_test.go cmd/mistersubsonic/session.go cmd/mistersubsonic/session_test.go internal/ui/app.go
git commit -m "app: session manager (newest connect wins, per-server data); main wires it" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: The setup wizard

**Files:**
- Create: `internal/ui/wizard.go`
- Modify: `internal/ui/session.go` (the start flow, `Dial`), `internal/ui/screens_play.go` (`NewMessageAction`), `cmd/mistersubsonic/session.go` (uses `ui.Dial`)
- Test: `internal/ui/wizard_test.go` (new); `internal/ui/session_test.go` (modified)

**Interfaces:**
- **Consumes:** `NewTextKeyboard`, `urlPresets`, `drawTextField` and `Switch` (Task 2); `UpdateConfig`, `Connect` and `Config` (Task 3); `config.AddServer` and `config.BackupInvalid`.
- **Produces:**
  - `NewWizardScreen(firstRun, backup bool)`. It goes URL → Username → Password (masked, with Show/hide) → API key → Test, and is also a `TextInput`.
  - `normalizeURL`: `host:port` becomes http, a trailing `/` or `/rest` is dropped, and `user:pw@` is removed.
  - The result step:
    - On success: Save and continue, or Back.
    - On an error: the offers described in the Decisions section, plus Try again and Change the address.
  - `save` stores:
    - token + salt with token auth;
    - the password with plain-password auth;
    - the API key with API-key auth.
    It calls `AddServer`, saves, and connects. `backup` first moves the invalid file aside.
  - `ui.Dial(ctx, srv)`: a client plus `Connect`, with the 10 s `ConnectTimeout`.
  - `NewMessageAction(title, message, label, action)`.
  - The start flow:
    - No config, or a config with no servers, starts the wizard.
    - An invalid config shows a message where A sets up again (`backup`).

- [ ] **Step 1: Write the failing tests**

`internal/ui/wizard_test.go` (new file):

```go
package ui

import (
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// fakeServer answers every Subsonic call. mode "auth" refuses the login;
// "token41" refuses token auth (error 41) but takes the plain password.
func fakeServer(mode string, tls bool) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case mode == "auth":
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`)
		case mode == "token41" && q.Get("t") != "":
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":41,"message":"Token authentication not supported"}}}`)
		default:
			io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","serverVersion":"0.59.0"}}`)
		}
	})
	if tls {
		srv := httptest.NewUnstartedServer(h)
		srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshake is expected
		srv.StartTLS()
		return srv
	}
	return httptest.NewServer(h)
}

func wizardApp(t *testing.T) (*testApp, *connectRecorder, *WizardScreen) {
	t.Helper()
	ta, rec := sessionApp(t, nil)
	ta.o.ConfigErr = config.ErrNotFound
	ta.start()
	w, ok := ta.Top().(*WizardScreen)
	if !ok {
		t.Fatalf("first screen %T, want the wizard", ta.Top())
	}
	return ta, rec, w
}

// fill types the four fields (physical keyboard; Enter is Next) and waits for the test.
func fill(t *testing.T, ta *testApp, url, user, pass, key string) {
	t.Helper()
	typeKeys(ta, url+"\n"+user+"\n"+pass+"\n"+key+"\n")
	ta.settle(t)
}

func readConfig(t *testing.T, ta *testApp) string {
	t.Helper()
	waitFile(t, ta.o.ConfigPath, "[[server]]")
	b, _ := os.ReadFile(ta.o.ConfigPath)
	return string(b)
}

func TestWizardFirstRunStoresATokenAndConnects(t *testing.T) {
	srv := fakeServer("ok", false)
	defer srv.Close()
	ta, rec, w := wizardApp(t)
	fill(t, ta, strings.TrimPrefix(srv.URL, "http://"), "alice", "s3cret", "")
	if w.step != stepTest || w.err != nil || w.auth != subsonic.AuthToken {
		t.Fatalf("step %d err %v auth %v", w.step, w.err, w.auth)
	}
	if w.actions[0].label != "Save and continue" {
		t.Fatalf("actions %v", w.actions)
	}
	ta.press(input.BtnA)
	body := readConfig(t, ta)
	if strings.Contains(body, "s3cret") || !strings.Contains(body, "token = ") || !strings.Contains(body, "salt = ") {
		t.Fatalf("saved config:\n%s", body)
	}
	if !strings.Contains(body, `url = "`+srv.URL+`"`) || !strings.Contains(body, `default_server = "127.0.0.1"`) {
		t.Fatalf("saved config:\n%s", body)
	}
	if len(rec.got) != 1 || rec.got[0].Servers[0].Token == "" {
		t.Fatalf("connects %v", rec.got)
	}
	if messageTitle(ta) != "Connecting…" {
		t.Fatalf("after saving: %q", messageTitle(ta))
	}
}

func TestWizardPlainPasswordOverHTTPNeedsConsent(t *testing.T) {
	srv := fakeServer("token41", false)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "ldapuser", "pw", "")
	if subsonic.Classify(w.err) != subsonic.KindPlaintextRefused || !strings.HasPrefix(w.actions[0].label, "Allow sending the password") {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA) // allow
	ta.settle(t)
	if w.err != nil || w.auth != subsonic.AuthPlain {
		t.Fatalf("after allowing: err %v auth %v", w.err, w.auth)
	}
	ta.press(input.BtnA) // save
	body := readConfig(t, ta)
	if !strings.Contains(body, `password = "pw"`) || !strings.Contains(body, "allow_plaintext_password = true") {
		t.Fatalf("saved config:\n%s", body)
	}
}

func TestWizardSelfSignedCertificateOffersInsecure(t *testing.T) {
	srv := fakeServer("ok", true)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", "pw", "")
	if subsonic.Classify(w.err) != subsonic.KindTLS || !strings.Contains(w.actions[0].label, "insecure") {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA)
	ta.settle(t)
	if w.err != nil {
		t.Fatalf("insecure retry: %v", w.err)
	}
	ta.press(input.BtnA)
	if body := readConfig(t, ta); !strings.Contains(body, "insecure_skip_verify = true") {
		t.Fatalf("saved config:\n%s", body)
	}
}

func TestWizardWrongPasswordGoesBackToTheLogin(t *testing.T) {
	srv := fakeServer("auth", false)
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", "wrong", "")
	if subsonic.Classify(w.err) != subsonic.KindAuth || w.actions[0].label != "Change the username or password" {
		t.Fatalf("err %v actions %v", w.err, w.actions)
	}
	ta.press(input.BtnA)
	if w.step != stepUser || string(w.fields[stepUser]) != "alice" {
		t.Fatalf("step %d, user %q", w.step, string(w.fields[stepUser]))
	}
	if _, err := os.Stat(ta.o.ConfigPath); err == nil {
		t.Fatal("a failed test saved the config")
	}
}

func TestWizardChecksTheFields(t *testing.T) {
	ta, _, w := wizardApp(t)
	typeKeys(ta, "ftp://x\n")
	if w.step != stepURL || w.problem == "" {
		t.Fatalf("ftp accepted: step %d problem %q", w.step, w.problem)
	}
	for range len("ftp://x") {
		typeKeys(ta, "\b")
	}
	typeKeys(ta, "music.example.com/rest/\n\n\n\n") // no user, password or key
	if w.step != stepAPIKey || !strings.Contains(w.problem, "password") {
		t.Fatalf("step %d problem %q", w.step, w.problem)
	}
	if got := string(w.fields[stepURL]); got != "http://music.example.com" {
		t.Fatalf("url normalized to %q", got)
	}
}

func TestWizardOnScreenKeysAndBack(t *testing.T) {
	ta, _, w := wizardApp(t)
	ta.press(input.BtnRight) // presets row: https://
	ta.press(input.BtnA)
	if got := string(w.fields[stepURL]); got != "https://" {
		t.Fatalf("preset typed %q", got)
	}
	typeKeys(ta, "music.example.com\nalice")
	ta.press(input.BtnB) // back to the address, fields kept
	if w.step != stepURL || string(w.fields[stepUser]) != "alice" {
		t.Fatalf("B: step %d user %q", w.step, string(w.fields[stepUser]))
	}
	ta.press(input.BtnB) // the first screen: B has nowhere to go
	if ta.Top() != w {
		t.Fatalf("B on the first step left the wizard: %T", ta.Top())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	typeKeys(ta, "\n")
	for range 5 {
		ta.press(input.BtnDown) // the bottom row
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // ABC
	ta.press(input.BtnA)
	ta.press(input.BtnUp)
	ta.press(input.BtnUp)
	ta.press(input.BtnA)
	got := []rune(string(w.fields[stepUser]))
	if len(got) != 6 || !unicode.IsUpper(got[5]) {
		t.Fatalf("after switching to upper case the key typed %q", string(got))
	}
}

func TestWizardReplacesAnInvalidConfig(t *testing.T) {
	srv := fakeServer("ok", false)
	defer srv.Close()
	ta, rec := sessionApp(t, nil)
	os.WriteFile(ta.o.ConfigPath, []byte("this is = not toml ["), 0o600)
	ta.o.ConfigErr = errors.New("config: invalid TOML syntax at line 1")
	ta.start()
	ta.press(input.BtnA) // set up again
	fill(t, ta, srv.URL, "alice", "pw", "")
	ta.press(input.BtnA) // save
	ta.settle(t)
	olds, _ := filepath.Glob(ta.o.ConfigPath + ".invalid-*")
	if len(olds) != 1 {
		t.Fatalf("backups %v", olds)
	}
	if b, _ := os.ReadFile(olds[0]); string(b) != "this is = not toml [" {
		t.Fatalf("backup holds %q", b)
	}
	if _, _, err := config.Load(ta.o.ConfigPath); err != nil {
		t.Fatalf("new config: %v", err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("connects %d", len(rec.got))
	}
}

func TestGoldenWizard(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		w := NewWizardScreen(true, false)
		ta.Push(w)
		typeKeys(ta, "http://192.168.1.10:4533")
		golden(t, "wizard-url-"+p.Name, ta.settle(t))
		w.fields[stepURL] = []rune("https://music.example.com")
		w.step, w.err = stepTest, x509.UnknownAuthorityError{}
		w.actions = w.resultActions()
		golden(t, "wizard-error-"+p.Name, ta.settle(t))
	}
}

// People type addresses every which way.
func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.10:4533":                   "http://192.168.1.10:4533",
		"  https://music.example.com/  ":      "https://music.example.com",
		"https://music.example.com/rest":      "https://music.example.com",
		"https://example.com/navidrome/rest/": "https://example.com/navidrome",
		"HTTP://Music.Example.com:4533":       "http://Music.Example.com:4533",
		"http://[::1]:4533":                   "http://[::1]:4533",
		"https://alice:pw@music.example.com":  "https://music.example.com",
	} {
		got, err := normalizeURL(in)
		if err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "http://", "://x"} {
		if _, err := normalizeURL(bad); err == nil {
			t.Errorf("normalizeURL(%q) accepted", bad)
		}
	}
}

// A password is kept exactly as typed (spaces, symbols, any script): the
// token the server checks is md5(password + salt) of those bytes.
func TestWizardPasswordIsKeptExactly(t *testing.T) {
	const pw = " pä$$ wörd:密码 "
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		sum := md5.Sum([]byte(pw + q.Get("s")))
		if q.Get("t") != hex.EncodeToString(sum[:]) {
			io.WriteString(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`)
			return
		}
		io.WriteString(w, `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`)
	}))
	defer srv.Close()
	ta, _, w := wizardApp(t)
	fill(t, ta, srv.URL, "alice", pw, "")
	if w.err != nil {
		t.Fatalf("the server rejected the typed password: %v", w.err)
	}
	ta.press(input.BtnA)
	body := readConfig(t, ta)
	cfg, _, err := config.Load(ta.o.ConfigPath)
	if err != nil {
		t.Fatalf("saved config doesn't load: %v\n%s", err, body)
	}
	s := cfg.Servers[0]
	sum := md5.Sum([]byte(pw + s.Salt))
	if s.Token != hex.EncodeToString(sum[:]) || s.Password != "" {
		t.Fatalf("saved token doesn't match the typed password: %+v", s)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/session_test.go b/internal/ui/session_test.go
index 2d0c36a..255e35a 100644
--- a/internal/ui/session_test.go
+++ b/internal/ui/session_test.go
@@ -2,6 +2,8 @@ package ui
 
 import (
 	"errors"
+	"io"
+	"log"
 	"os"
 	"path/filepath"
 	"strings"
@@ -53,8 +55,23 @@ func TestStartExplainsProblemsOrConnects(t *testing.T) {
 	ta.o.AudioErr = nil
 	ta.o.ConfigErr = config.ErrNotFound
 	ta.start()
-	if messageTitle(ta) != "Setup needed" {
-		t.Fatalf("no config: %q", messageTitle(ta))
+	if w, ok := ta.Top().(*WizardScreen); !ok || !w.firstRun || w.backup {
+		t.Fatalf("no config: top %T", ta.Top())
+	}
+	ta.o.ConfigErr = errors.New("config: line 3: invalid TOML")
+	ta.start()
+	if messageTitle(ta) != "The config file has a problem" {
+		t.Fatalf("invalid config: %q", messageTitle(ta))
+	}
+	ta.press(input.BtnA)
+	if w, ok := ta.Top().(*WizardScreen); !ok || w.firstRun || !w.backup {
+		t.Fatalf("A on the invalid-config message: top %T", ta.Top())
+	}
+	ta.o.ConfigErr = nil
+	ta.cfg = config.Default() // valid, but no servers
+	ta.start()
+	if _, ok := ta.Top().(*WizardScreen); !ok {
+		t.Fatalf("no servers: top %T", ta.Top())
 	}
 	ta.cfg = twoServers()
 	ta.start()
@@ -168,6 +185,8 @@ func TestSaveFailureIsAToastWithoutSecrets(t *testing.T) {
 		t.Skip("root ignores the directory permissions this test relies on")
 	}
 	ta, _ := sessionApp(t, twoServers())
+	log.SetOutput(io.Discard) // the failure is logged too; keep the test output clean
+	t.Cleanup(func() { log.SetOutput(os.Stderr) })
 	dir := t.TempDir()
 	os.Chmod(dir, 0o500)
 	t.Cleanup(func() { os.Chmod(dir, 0o700) })
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: WizardScreen
undefined: stepTest
undefined: stepUser
undefined: stepURL
undefined: stepAPIKey
```

- [ ] **Step 3: Implement**

`internal/ui/wizard.go` (new file):

```go
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// The setup wizard (spec §8.2): server URL → username → password → API key
// (optional) → test → save. It runs on first start, when the config file
// is broken (the old file is kept as config.toml.invalid-<time>), and from
// Settings → Servers → Add server.

type wizardStep int

const (
	stepURL wizardStep = iota
	stepUser
	stepPassword
	stepAPIKey
	stepTest
	wizardFields = stepTest
)

var stepText = [wizardFields]struct{ prompt, help string }{
	{"Server address", "Like http://192.168.1.10:4533 or https://music.example.com"},
	{"Username", "Leave it empty if you log in with an API key"},
	{"Password", "It is not stored: the app keeps a token instead"},
	{"API key (optional)", "Only for servers that give out API keys; leave it empty otherwise"},
}

// WizardScreen collects a server's address and login, tests them, and
// saves the server to the config as the default.
type WizardScreen struct {
	step     wizardStep
	fields   [wizardFields][]rune
	kb       Keyboard
	show     bool   // the password is shown
	problem  string // why Next didn't advance
	firstRun bool   // the app's first screen: B on the first step has nowhere to go
	backup   bool   // the config file is invalid: move it aside when saving

	// The test step.
	insecure, plaintext bool // the user allowed these after an error
	testing             bool
	err                 error
	info                *subsonic.ServerInfo
	auth                subsonic.AuthMethod
	actions             []menuEntry
	list                List
	cancel              func()
	saving              bool
}

// NewWizardScreen starts the wizard. firstRun: it is the app's first
// screen. backup: the existing config file is invalid and is kept aside.
func NewWizardScreen(firstRun, backup bool) *WizardScreen {
	s := &WizardScreen{firstRun: firstRun, backup: backup}
	s.setStep(stepURL)
	return s
}

func (s *WizardScreen) Title() string {
	if s.firstRun {
		return "Set up MiSTer Subsonic"
	}
	return "Add a server"
}

func (s *WizardScreen) Enter(a *App) {}

func (s *WizardScreen) setStep(st wizardStep) {
	s.step, s.problem = st, ""
	switch st {
	case stepURL:
		s.kb = NewTextKeyboard(urlPresets...)
	case stepPassword:
		s.kb = NewTextKeyboard(kbKey{label: "Show / hide", action: keyShow, units: 4})
	default:
		s.kb = NewTextKeyboard()
	}
}

// Text takes physical keyboard input: Enter is Next, Backspace edits (on
// an empty field it goes back a step, as B).
func (s *WizardScreen) Text(a *App, r rune) bool {
	if s.step == stepTest {
		return false
	}
	f := &s.fields[s.step]
	switch {
	case r == '\n':
		s.next(a)
	case r == '\b':
		if len(*f) == 0 {
			return false
		}
		*f = (*f)[:len(*f)-1]
	case unicode.IsPrint(r):
		*f = append(*f, r)
	default:
		return false
	}
	return true
}

func (s *WizardScreen) Handle(a *App, e input.Event) bool {
	if s.step == stepTest {
		return s.handleTest(a, e)
	}
	if e.Kind == input.Release {
		return false
	}
	if s.kb.Move(e) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	f := &s.fields[s.step]
	switch e.Button {
	case input.BtnA:
		k := s.kb.Focused()
		switch k.action {
		case keyChar:
			*f = append(*f, k.r)
		case keySpace:
			*f = append(*f, ' ')
		case keyDel:
			if len(*f) > 0 {
				*f = (*f)[:len(*f)-1]
			}
		case keyLayout:
			s.kb.Switch(k.to)
		case keyInsert:
			*f = append(*f, []rune(k.text)...)
		case keyShow:
			s.show = !s.show
		case keyNext:
			s.next(a)
		}
		return true
	case input.BtnX: // shortcut for Del
		if len(*f) > 0 {
			*f = (*f)[:len(*f)-1]
		}
		return true
	case input.BtnB:
		return s.back(a)
	}
	return false
}

// back goes to the previous step; false (the app pops the wizard) on the
// first step, unless the wizard is the app's first screen.
func (s *WizardScreen) back(a *App) bool {
	if s.step > stepURL {
		s.cancelTest()
		s.setStep(s.step - 1)
		return true
	}
	return s.firstRun
}

// next checks the field and moves on; after the last field it tests.
func (s *WizardScreen) next(a *App) {
	switch s.step {
	case stepURL:
		u, err := normalizeURL(string(s.fields[stepURL]))
		if err != nil {
			s.problem = err.Error()
			return
		}
		s.fields[stepURL] = []rune(u)
	case stepAPIKey:
		pw, key := string(s.fields[stepPassword]), strings.TrimSpace(string(s.fields[stepAPIKey]))
		switch {
		case pw == "" && key == "":
			s.problem = "Enter a password (step 3) or an API key"
			return
		case key == "" && strings.TrimSpace(string(s.fields[stepUser])) == "":
			s.problem = "Enter a username (step 2), or an API key"
			return
		}
	}
	s.setStep(s.step + 1)
	if s.step == stepTest {
		s.test(a)
	}
}

// normalizeURL accepts "host:port" as http and trims a trailing "/rest".
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Enter the server's address")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("That doesn't look like http://host:port or https://host")
	}
	u.User = nil
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/rest")
	return strings.TrimSuffix(u.String(), "/"), nil
}

// server is the server being set up, from the fields and the options the
// user allowed after an error.
func (s *WizardScreen) server() config.Server {
	u, _ := url.Parse(string(s.fields[stepURL]))
	return config.Server{
		Name:     u.Hostname(),
		URL:      string(s.fields[stepURL]),
		Username: strings.TrimSpace(string(s.fields[stepUser])),
		Password: string(s.fields[stepPassword]),
		APIKey:   strings.TrimSpace(string(s.fields[stepAPIKey])),

		InsecureSkipVerify:     s.insecure,
		AllowPlaintextPassword: s.plaintext,
	}
}

func (s *WizardScreen) cancelTest() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.testing = false
}

// test pings the server with the entered login (off the UI goroutine).
func (s *WizardScreen) test(a *App) {
	s.cancelTest()
	s.testing, s.err, s.info, s.actions = true, nil, nil, nil
	srv := s.server()
	type result struct {
		info *subsonic.ServerInfo
		auth subsonic.AuthMethod
	}
	s.cancel = a.LoadCancel(s, func(ctx context.Context) (any, error) {
		c, info, err := Dial(ctx, srv)
		if err != nil {
			return nil, err
		}
		return result{info, c.AuthMethod()}, nil
	}, func(v any, err error) {
		s.testing, s.cancel = false, nil
		if err != nil {
			s.err = err
		} else {
			r := v.(result)
			s.info, s.auth = r.info, r.auth
		}
		s.list = List{}
		s.actions = s.resultActions()
	})
}

// resultActions are the choices after a test: save, or ways to fix it.
func (s *WizardScreen) resultActions() []menuEntry {
	if s.err == nil {
		return []menuEntry{
			{"Save and continue", func(a *App) { s.save(a) }},
			{"Back", func(a *App) { s.setStep(stepAPIKey) }},
		}
	}
	var out []menuEntry
	switch subsonic.Classify(s.err) {
	case subsonic.KindTLS:
		if !s.insecure {
			out = append(out, menuEntry{"Connect without checking the certificate (insecure)", func(a *App) { s.insecure = true; s.test(a) }})
		}
	case subsonic.KindPlaintextRefused:
		out = append(out, menuEntry{"Allow sending the password in plain text", func(a *App) { s.plaintext = true; s.test(a) }})
	case subsonic.KindAuth:
		out = append(out, menuEntry{"Change the username or password", func(a *App) { s.setStep(stepUser) }})
	}
	return append(out,
		menuEntry{"Try again", func(a *App) { s.test(a) }},
		menuEntry{"Change the address", func(a *App) { s.setStep(stepURL) }},
	)
}

// save writes the tested server as the default and connects to it. The
// password is stored only when the server needs the plain password; with
// token auth a token and salt are stored instead (spec §4).
func (s *WizardScreen) save(a *App) {
	if s.saving {
		return
	}
	s.saving = true
	srv := s.server()
	switch s.auth {
	case subsonic.AuthToken:
		srv.Token, srv.Salt = subsonic.NewTokenPair(srv.Password)
		srv.Password, srv.APIKey = "", ""
	case subsonic.AuthAPIKey:
		srv.Password = ""
	}
	finish := func() {
		a.UpdateConfig(func(c *config.Config) { c.AddServer(srv) }, false)
		a.Connect()
	}
	if !s.backup {
		finish()
		return
	}
	path := a.o.ConfigPath
	now := a.o.Now()
	a.Load(s, func(context.Context) (any, error) { return config.BackupInvalid(path, now) }, func(v any, err error) {
		s.saving = false
		if err != nil {
			a.Toast("Couldn't move the old config aside: %s", saveProblem(err))
			return
		}
		a.Toast("The old config is kept as %s", v)
		a.cfg = nil // start over from the defaults
		finish()
	})
}

func (s *WizardScreen) handleTest(a *App, e input.Event) bool {
	if e.Kind == input.Press && e.Button == input.BtnB {
		return s.back(a)
	}
	if s.list.Handle(e, len(s.actions)) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && len(s.actions) > 0 {
		s.actions[s.list.Focus].run(a)
		return true
	}
	return false
}

func (s *WizardScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb, fs := a.F.Body, a.F.Small
	x, w := area.X+p.Margin, area.W-2*p.Margin
	y := area.Y + p.Margin/2
	line := func(f *gfx.Font, text string, col gfx.Color) {
		for _, l := range wrap(f, text, w) {
			f.Draw(c, x, y+f.Ascent(), l, col, area)
			y += f.Height()
		}
	}
	if s.step == stepTest {
		s.drawTest(a, c, area, line, &y)
		return
	}
	line(fb, fmt.Sprintf("%d of %d · %s", s.step+1, wizardFields, stepText[s.step].prompt), colText)
	line(fs, stepText[s.step].help, colDim)
	y += p.Margin / 4
	field := gfx.R(x, y, w, fb.Height()+p.Margin/2)
	masked := s.step == stepPassword && !s.show
	a.drawTextField(c, field, s.fields[s.step], "", masked)
	y = field.Bottom() + p.Margin/4
	if s.problem != "" {
		line(fs, s.problem, colError)
	}
	y += p.Margin / 4
	keyH := p.RowH
	kw := min(w, 12*p.RowH)
	s.kb.Draw(a, c, gfx.R(x, y, kw, s.kb.Height(keyH)), keyH, true)
}

func (s *WizardScreen) drawTest(a *App, c *gfx.Canvas, area gfx.Rect, line func(*gfx.Font, string, gfx.Color), y *int) {
	p := a.P
	fb, fs := a.F.Body, a.F.Small
	srv := s.server()
	switch {
	case s.testing:
		line(fb, "Connecting to "+displayURL(srv.URL)+"…", colDim)
		return
	case s.err == nil && s.info != nil:
		who := srv.Username
		if who == "" {
			who = "your API key"
		}
		line(fb, fmt.Sprintf("Connected to %s as %s.", serverName(s.info), who), colText)
		line(fs, "Login: "+s.auth.String()+". "+displayURL(srv.URL), colDim)
	case s.err != nil:
		line(fb, fmt.Sprintf("%s: %s.", displayURL(srv.URL), subsonic.Classify(s.err)), colError)
		if h := wizardHint(s.err); h != "" {
			line(fs, h, colDim)
		}
	}
	*y += p.Margin / 2
	s.list.Draw(c, gfx.R(area.X, *y, area.W, area.Bottom()-*y), len(s.actions), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: s.actions[i].label, col: colAccent, focused: focused})
	})
}

// serverName is e.g. "Navidrome 0.59.0", or "the server".
func serverName(info *subsonic.ServerInfo) string {
	if info == nil || info.Type == "" {
		return "the server"
	}
	name := strings.ToUpper(info.Type[:1]) + info.Type[1:]
	if info.ServerVersion != "" {
		name += " " + info.ServerVersion
	}
	return name
}

// wizardHint explains an error in the wizard's terms.
func wizardHint(err error) string {
	switch subsonic.Classify(err) {
	case subsonic.KindTLS:
		return "The server's certificate isn't trusted (self-signed?). The safe fix is ca_file in the config; skipping the check works but anyone on the network could pose as the server."
	case subsonic.KindAuth:
		return "The server didn't accept this username and password (or API key)."
	case subsonic.KindPlaintextRefused:
		return "This server can't use tokens (LDAP or proxy login?) and wants the plain password, which over http anyone on the network can read. Use https, or allow it."
	case subsonic.KindUnreachable, subsonic.KindTimeout:
		return "Check the address and port, and that the server is running."
	case subsonic.KindNotFound:
		return "Something answered, but not a Subsonic server: check the address and port."
	}
	return ""
}
```

Then Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/cmd/mistersubsonic/session.go b/cmd/mistersubsonic/session.go
index 4f2dc5d..9f7ffef 100644
--- a/cmd/mistersubsonic/session.go
+++ b/cmd/mistersubsonic/session.go
@@ -7,7 +7,6 @@ import (
 	"path/filepath"
 	"strings"
 	"sync"
-	"time"
 
 	"mistersubsonic/internal/art"
 	"mistersubsonic/internal/audio"
@@ -51,8 +50,6 @@ type session struct {
 	done   chan struct{} // closed when pl.Run returns
 }
 
-const connectTimeout = 10 * time.Second
-
 func newSessions(ctx context.Context, eng *audio.Engine, dataDir string, volume float64) *sessions {
 	return &sessions{ctx: ctx, eng: eng, dataDir: dataDir, volume: volume}
 }
@@ -73,7 +70,7 @@ func (m *sessions) connect(a sessionUI, cfg *config.Config) {
 		if old != nil {
 			m.stop(old)
 		}
-		c, info, err := dial(m.ctx, server)
+		c, info, err := ui.Dial(m.ctx, server)
 		if err != nil {
 			a.Post(func() {
 				if m.current(gen) {
@@ -108,26 +105,6 @@ func (m *sessions) current(gen int) bool {
 	return gen == m.gen && !m.closing
 }
 
-// dial makes a client for srv and checks it answers (ping, auth fallback).
-func dial(ctx context.Context, srv config.Server) (*subsonic.Client, *subsonic.ServerInfo, error) {
-	c, err := subsonic.New(subsonic.Options{
-		BaseURL: srv.URL,
-		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
-			Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
-		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
-	})
-	if err != nil {
-		return nil, nil, err
-	}
-	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
-	defer cancel()
-	info, err := c.Connect(cctx)
-	if err != nil {
-		return nil, nil, err
-	}
-	return c, info, nil
-}
-
 // build makes and starts the player and art loader (disk I/O: off the UI
 // goroutine).
 func (m *sessions) build(a sessionUI, c *subsonic.Client, srv config.Server, pb config.Playback, cc config.Cache) *session {
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 11fa885..7836966 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -334,10 +334,16 @@ func isPlayScreen(s Screen) bool {
 type MessageScreen struct {
 	title, message string
 	retry          func()
+	label          string // what A does
 }
 
 func NewMessageScreen(title, message string, retry func()) *MessageScreen {
-	return &MessageScreen{title: title, message: message, retry: retry}
+	return &MessageScreen{title: title, message: message, retry: retry, label: "Press A to try again"}
+}
+
+// NewMessageAction is a message whose A action is described by label.
+func NewMessageAction(title, message, label string, action func()) *MessageScreen {
+	return &MessageScreen{title: title, message: message, retry: action, label: label}
 }
 
 func (s *MessageScreen) Title() string { return s.title }
@@ -361,7 +367,7 @@ func (s *MessageScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	}
 	if s.retry != nil {
 		y += p.Margin
-		f.Draw(c, area.X+p.Margin, y+f.Ascent(), "Press A to try again", colAccent, area)
+		f.Draw(c, area.X+p.Margin, y+f.Ascent(), s.label, colAccent, area)
 	}
 }
 
diff --git a/internal/ui/session.go b/internal/ui/session.go
index 2982b75..f33ead9 100644
--- a/internal/ui/session.go
+++ b/internal/ui/session.go
@@ -1,6 +1,7 @@
 package ui
 
 import (
+	"context"
 	"errors"
 	"fmt"
 	"io/fs"
@@ -39,12 +40,12 @@ func (a *App) start() {
 	switch {
 	case a.o.AudioErr != nil:
 		a.Replace(NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", a.o.AudioErr, a.o.ConfigPath), nil))
-	case a.cfg == nil && errors.Is(a.o.ConfigErr, config.ErrNotFound):
-		a.Replace(NewMessageScreen("Setup needed", "No config file yet. Create "+a.o.ConfigPath+" with a [[server]] section (name, url, username, password), then restart.", nil))
+	case a.cfg == nil && errors.Is(a.o.ConfigErr, config.ErrNotFound), a.cfg != nil && len(a.cfg.Servers) == 0:
+		a.Replace(NewWizardScreen(true, false))
 	case a.cfg == nil:
-		a.Replace(NewMessageScreen("Setup needed", "The config file has a problem:\n"+fmt.Sprint(a.o.ConfigErr), nil))
-	case len(a.cfg.Servers) == 0:
-		a.Replace(NewMessageScreen("Setup needed", "Add a [[server]] to "+a.o.ConfigPath+".", nil))
+		a.Replace(NewMessageAction("The config file has a problem",
+			fmt.Sprintf("%v\n\nFix %s and restart, or set up again: the file is then kept as %s.invalid-<date>.", a.o.ConfigErr, a.o.ConfigPath, a.o.ConfigPath),
+			"Press A to set up again", func() { a.Push(NewWizardScreen(false, true)) }))
 	default:
 		a.Connect()
 	}
@@ -172,6 +173,31 @@ func (a *App) flushConfig() {
 	a.saveAt = time.Time{}
 }
 
+// ConnectTimeout bounds Dial.
+const ConnectTimeout = 10 * time.Second
+
+// Dial makes a client for srv and checks that the server answers (ping,
+// with the auth fallbacks of spec §4). It does network I/O: call it off the
+// UI goroutine.
+func Dial(ctx context.Context, srv config.Server) (*subsonic.Client, *subsonic.ServerInfo, error) {
+	c, err := subsonic.New(subsonic.Options{
+		BaseURL: srv.URL,
+		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
+			Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
+		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
+	})
+	if err != nil {
+		return nil, nil, err
+	}
+	cctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
+	defer cancel()
+	info, err := c.Connect(cctx)
+	if err != nil {
+		return nil, nil, err
+	}
+	return c, info, nil
+}
+
 // displayURL is raw without credentials, safe to show on screen or log.
 func displayURL(raw string) string {
 	u, err := url.Parse(raw)
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: `ok`. Golden screenshots written or changed: `wizard-error-crt`, `wizard-error-hdmi`, `wizard-url-crt`, `wizard-url-hdmi`. Open each one and check it: the address step shows "1 of 4 · Server address", its help line, the field with `http://192.168.1.10:4533` and a caret, and the keyboard with the presets row on top (`http://` focused). The error step shows the red TLS message, the hint, and three choices, with the insecure one first and highlighted.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/session.go internal/ui/screens_play.go internal/ui/session.go internal/ui/session_test.go internal/ui/wizard.go internal/ui/wizard_test.go internal/ui/testdata/golden
git commit -m "ui: setup wizard (test, save token not password, TLS/plaintext consent)" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Settings

**Files:**
- Create: `internal/ui/screens_settings.go`
- Modify: `internal/ui/app.go` (`Player` gains `SetReplayGain` and `SetScrobble`), `internal/ui/screens_home.go` (a Settings section), `internal/ui/screens_setup.go` (Settings on the unreachable screen), `internal/ui/screens_play.go` (Now Playing volume is saved), `cmd/mistersubsonic/session.go` (a connect with no server stops the session)
- Test: `internal/ui/settings_test.go` (new); `internal/ui/fakes_test.go`, `internal/ui/session_test.go` (modified)

**Interfaces:**
- **Consumes:** `SetReplayGain` and `SetScrobble` (Task 1), `UpdateConfig` (Task 3), `NewWizardScreen` (Task 5).
- **Produces:**
  - `NewSettingsScreen()`: Servers · Playback · Display · About · Exit.
  - `NewServersScreen()`: A switches to a server; X offers Switch or Remove (with confirmation); the last row is "Add a server".
  - `newSettingsList(title, rows)`, with `playbackSettings` and `displaySettings`.
  - `AboutScreen`.
  - `(*App).setVolume(db)` applies the volume and saves it after a pause.
  - `switchServer(name)` and `removeServer(name)`.
  - The section is "Settings", last in the HDMI sidebar and the CRT list.

- [ ] **Step 1: Write the failing tests**

`internal/ui/settings_test.go` (new file):

```go
package ui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func connectedApp(t *testing.T) (*testApp, *connectRecorder) {
	t.Helper()
	ta, rec := sessionApp(t, twoServers())
	srv := ta.cfg.Servers[0]
	ta.Connected(ConnInfo{Server: srv, Info: &subsonic.ServerInfo{Type: "navidrome", ServerVersion: "0.59.0"}, Auth: subsonic.AuthToken}, ta.lib, ta.pl, fakeArt{})
	return ta, rec
}

func TestSettingsExitAsks(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(NewSettingsScreen())
	for range 4 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Exit
	if !ta.confirm {
		t.Fatal("Exit did not ask")
	}
	ta.press(input.BtnB)
	if ta.confirm || ta.quit {
		t.Fatal("B did not cancel the exit prompt")
	}
}

func TestPlaybackSettingsApplyAndSave(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.pl.st.VolumeDB = -10
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnRight) // volume +1
	ta.press(input.BtnDown)
	ta.press(input.BtnRight) // ReplayGain off -> track
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // scrobbling off
	ta.press(input.BtnDown)
	ta.press(input.BtnLeft) // transcode mp3 -> wav
	if ta.pl.st.VolumeDB != -9 || !slices.Equal(ta.pl.calls, []string{"replaygain track", "scrobble off"}) {
		t.Fatalf("volume %v, player calls %v", ta.pl.st.VolumeDB, ta.pl.calls)
	}
	pb := ta.cfg.Playback
	if pb.VolumeDB != -9 || pb.ReplayGain != "track" || pb.Scrobble || pb.TranscodeFormat != "wav" {
		t.Fatalf("config %+v", pb)
	}
	ta.flushConfig()
	waitFile(t, ta.o.ConfigPath, `transcode_format = "wav"`)
}

func TestDisplaySettings(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Display", displaySettings))
	ta.press(input.BtnRight) // auto -> hdmi
	ta.press(input.BtnDown)
	ta.press(input.BtnLeft) // 5 min -> 2 min
	if d := ta.cfg.Display; d.Profile != "hdmi" || d.ScreensaverMinutes != 2 {
		t.Fatalf("display %+v", d)
	}
	if !strings.Contains(ta.toasts[0].text, "next time") {
		t.Fatalf("toast %q", ta.toasts[0].text)
	}
}

func TestNowPlayingVolumeIsSaved(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	if ta.cfg.Playback.VolumeDB != -2 || ta.saveAt.IsZero() {
		t.Fatalf("config volume %v, save pending %v", ta.cfg.Playback.VolumeDB, !ta.saveAt.IsZero())
	}
}

func TestServersSwitchAndRemove(t *testing.T) {
	ta, rec := connectedApp(t) // on "home"
	ta.Push(NewServersScreen())
	ta.press(input.BtnA) // home: already connected
	if len(rec.got) != 0 || !strings.HasPrefix(ta.toasts[0].text, "Already connected") {
		t.Fatalf("connects %v toasts %v", rec.got, ta.toasts)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // cloud
	if len(rec.got) != 1 || rec.got[0].DefaultServer != "cloud" {
		t.Fatalf("switch connects %v", rec.got)
	}
	// Remove the connected server: the next default connects.
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[1]}, ta.lib, ta.pl, fakeArt{})
	ta.Push(NewServersScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Remove
	ta.press(input.BtnA) // confirm
	if names := serverNames(ta.cfg); !slices.Equal(names, []string{"home"}) || len(rec.got) != 2 || rec.got[1].DefaultServer != "home" {
		t.Fatalf("servers %v, connects %d", names, len(rec.got))
	}
	// Remove the last: disconnect and set up again.
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[0]}, ta.lib, ta.pl, fakeArt{})
	ta.Push(NewServersScreen())
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	ta.press(input.BtnA)
	if _, ok := ta.Top().(*WizardScreen); !ok || ta.Player() != nil || len(rec.got[2].Servers) != 0 {
		t.Fatalf("after removing the last: top %T, player %v", ta.Top(), ta.Player())
	}
}

func serverNames(c *config.Config) []string {
	var out []string
	for _, s := range c.Servers {
		out = append(out, s.Name)
	}
	return out
}

func TestServersAddOpensTheWizard(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(NewServersScreen())
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // Add a server
	ta.press(input.BtnA)
	w, ok := ta.Top().(*WizardScreen)
	if !ok || w.firstRun || w.Title() != "Add a server" {
		t.Fatalf("top %T", ta.Top())
	}
	ta.press(input.BtnB) // the first step: back to the list
	if _, ok := ta.Top().(*ServersScreen); !ok {
		t.Fatalf("B from the wizard: %T", ta.Top())
	}
}

func TestAboutDescribesTheConnection(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.o.Version = "v0.2.0"
	lines := strings.Join((&AboutScreen{}).lines(ta.App), "\n")
	for _, want := range []string{"MiSTer Subsonic v0.2.0", "home — http://192.168.1.10:4533", "Navidrome 0.59.0, login by token"} {
		if !strings.Contains(lines, want) {
			t.Errorf("About lacks %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "pw") {
		t.Fatal("About shows the password")
	}
	ta.Detach()
	if l := (&AboutScreen{}).lines(ta.App); l[1] != "Not connected" {
		t.Fatalf("disconnected About %v", l)
	}
}

func TestSettingsWorkWithoutAConnection(t *testing.T) {
	ta, _ := sessionApp(t, twoServers()) // detached: no player
	ta.ConnectFailed(ta.cfg.Servers[0], errUnreachable)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Settings
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Playback
	ta.press(input.BtnLeft)
	if ta.cfg.Playback.VolumeDB != -1 {
		t.Fatalf("volume %v", ta.cfg.Playback.VolumeDB)
	}
}

func TestGoldenSettings(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.cfg, ta.o.ConfigPath = twoServers(), filepath.Join(t.TempDir(), "config.toml")
		ta.Connected(ConnInfo{Server: ta.cfg.Servers[0], Info: &subsonic.ServerInfo{Type: "navidrome", ServerVersion: "0.59.0"}, Auth: subsonic.AuthToken}, ta.lib, ta.pl, fakeArt{})
		ta.Push(newSettingsList("Playback", playbackSettings))
		golden(t, "settings-playback-"+p.Name, ta.settle(t))
		ta.Replace(NewServersScreen())
		golden(t, "settings-servers-"+p.Name, ta.settle(t))
	}
}

var errUnreachable = errors.New("dial tcp 192.168.1.10:4533: connection refused")
```

Then update the existing tests. Save this patch as `/tmp/t6-test.patch` and apply it from the repository root with `git apply /tmp/t6-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index cd0eaf1..c3702da 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -238,7 +238,15 @@ func (p *fakePlayer) Clear() {
 	p.call("clear")
 	p.st.Queue, p.st.Index, p.st.Status = nil, -1, player.Stopped
 }
-func (p *fakePlayer) SetVolumeDB(db float64) { p.st.VolumeDB = max(-60, min(0, db)) }
+func (p *fakePlayer) SetVolumeDB(db float64)    { p.st.VolumeDB = max(-60, min(0, db)) }
+func (p *fakePlayer) SetReplayGain(mode string) { p.call("replaygain " + mode) }
+func (p *fakePlayer) SetScrobble(on bool) {
+	if on {
+		p.call("scrobble on")
+	} else {
+		p.call("scrobble off")
+	}
+}
 func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
 	p.call("playnow")
 	p.played, p.start = songs, start
diff --git a/internal/ui/session_test.go b/internal/ui/session_test.go
index 255e35a..2d02a88 100644
--- a/internal/ui/session_test.go
+++ b/internal/ui/session_test.go
@@ -111,7 +111,7 @@ func TestUnreachableOffersRetryAndSwitch(t *testing.T) {
 	ta, rec := sessionApp(t, twoServers())
 	ta.ConnectFailed(ta.cfg.Servers[0], errors.New("dial tcp: refused"))
 	s, ok := ta.Top().(*UnreachableScreen)
-	if !ok || len(s.actions) != 2 {
+	if !ok || len(s.actions) != 3 { // Try again, Switch server, Settings
 		t.Fatalf("top %T with %d actions", ta.Top(), len(s.actions))
 	}
 	ta.press(input.BtnA) // Try again
@@ -137,7 +137,7 @@ func TestUnreachableWithOneServerOnlyRetries(t *testing.T) {
 	c.AddServer(config.Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
 	ta, _ := sessionApp(t, c)
 	ta.ConnectFailed(c.Servers[0], errors.New("x"))
-	if s := ta.Top().(*UnreachableScreen); len(s.actions) != 1 {
+	if s := ta.Top().(*UnreachableScreen); len(s.actions) != 2 { // Try again, Settings
 		t.Fatalf("%d actions", len(s.actions))
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: NewSettingsScreen
undefined: newSettingsList
undefined: playbackSettings
undefined: displaySettings
undefined: NewServersScreen
undefined: ServersScreen
```

- [ ] **Step 3: Implement**

`internal/ui/screens_settings.go` (new file):

```go
package ui

import (
	"fmt"
	"math"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// SettingsScreen is Settings (spec §8.2): Servers · Playback · Display ·
// About · Exit. It works without a connection (from the unreachable
// screen): changes are saved to the config and applied to the player when
// there is one.
type SettingsScreen struct {
	list List
}

var settingsItems = []string{"Servers", "Playback", "Display", "About", "Exit"}

func NewSettingsScreen() *SettingsScreen { return &SettingsScreen{} }

func (s *SettingsScreen) Title() string { return "Settings" }
func (s *SettingsScreen) Enter(a *App)  {}

func (s *SettingsScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(settingsItems)) {
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA {
		return false
	}
	switch settingsItems[s.list.Focus] {
	case "Servers":
		a.Push(NewServersScreen())
	case "Playback":
		a.Push(newSettingsList("Playback", playbackSettings))
	case "Display":
		a.Push(newSettingsList("Display", displaySettings))
	case "About":
		a.Push(&AboutScreen{})
	case "Exit":
		a.confirm = true // the same prompt as holding B
	}
	return true
}

func (s *SettingsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.list.Draw(c, area, len(settingsItems), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawRow(c, r, row{main: settingsItems[i], focused: focused})
	})
}

// setting is one row of a settings list: Left/Right (or A, forwards)
// change it.
type setting struct {
	label  string
	value  func(a *App) string
	change func(a *App, dir int)
}

// SettingsListScreen is a list of settings with their values.
type SettingsListScreen struct {
	title string
	rows  func(a *App) []setting
	list  List
}

func newSettingsList(title string, rows func(a *App) []setting) *SettingsListScreen {
	return &SettingsListScreen{title: title, rows: rows}
}

func (s *SettingsListScreen) Title() string { return s.title }
func (s *SettingsListScreen) Enter(a *App)  {}

func (s *SettingsListScreen) Handle(a *App, e input.Event) bool {
	rows := s.rows(a)
	if s.list.Handle(e, len(rows)) {
		return true
	}
	if e.Kind == input.Release || len(rows) == 0 {
		return false
	}
	r := rows[min(s.list.Focus, len(rows)-1)]
	switch {
	case e.Button == input.BtnLeft:
		r.change(a, -1)
	case e.Button == input.BtnRight, e.Button == input.BtnA && e.Kind == input.Press:
		r.change(a, 1)
	default:
		return false
	}
	return true
}

func (s *SettingsListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	rows := s.rows(a)
	s.list.Draw(c, area, len(rows), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		v := rows[i].value(a)
		if focused {
			v = "‹ " + v + " ›"
		}
		a.drawRow(c, r, row{main: rows[i].label, focused: focused, right: v})
	})
}

// cycle moves through choices by dir, wrapping.
func cycle[T comparable](choices []T, cur T, dir int) T {
	i := 0
	for k, c := range choices {
		if c == cur {
			i = k
		}
	}
	return choices[(i+dir+len(choices))%len(choices)]
}

func onOff(b bool) string {
	if b {
		return "On"
	}
	return "Off"
}

// volumeDB is the current volume: the player's, else the config's.
func (a *App) volumeDB() float64 {
	if pl := a.Player(); pl != nil {
		return pl.State().VolumeDB
	}
	if a.cfg != nil {
		return a.cfg.Playback.VolumeDB
	}
	return 0
}

// setVolume changes the volume now and saves it (after a pause, so a held
// key is one write).
func (a *App) setVolume(db float64) {
	db = math.Max(-60, math.Min(0, math.Round(db)))
	if pl := a.Player(); pl != nil {
		pl.SetVolumeDB(db)
	}
	a.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = db }, true)
}

func playbackSettings(a *App) []setting {
	pb := func() config.Playback {
		if a.cfg == nil {
			return config.Default().Playback
		}
		return a.cfg.Playback
	}
	return []setting{
		{"Volume", func(a *App) string { return volumeLabel(a.volumeDB()) },
			func(a *App, dir int) { a.setVolume(a.volumeDB() + float64(dir)) }},
		{"ReplayGain", func(a *App) string { return pb().ReplayGain },
			func(a *App, dir int) {
				mode := cycle([]string{"off", "track", "album"}, pb().ReplayGain, dir)
				if pl := a.Player(); pl != nil {
					pl.SetReplayGain(mode)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.ReplayGain = mode }, false)
			}},
		{"Scrobbling", func(a *App) string { return onOff(pb().Scrobble) },
			func(a *App, dir int) {
				on := !pb().Scrobble
				if pl := a.Player(); pl != nil {
					pl.SetScrobble(on)
				}
				a.UpdateConfig(func(c *config.Config) { c.Playback.Scrobble = on }, false)
			}},
		{"Transcode to", func(a *App) string { return pb().TranscodeFormat },
			func(a *App, dir int) {
				f := cycle([]string{"mp3", "flac", "wav"}, pb().TranscodeFormat, dir)
				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeFormat = f }, false)
				a.Toast("Used from the next connection")
			}},
		{"Transcode bitrate", func(a *App) string { return fmt.Sprintf("%d kbps", pb().TranscodeBitrate) },
			func(a *App, dir int) {
				b := cycle([]int{128, 192, 256, 320}, pb().TranscodeBitrate, dir)
				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeBitrate = b }, false)
				a.Toast("Used from the next connection")
			}},
	}
}

var screensaverChoices = []int{0, 1, 2, 5, 10, 15, 30}

func displaySettings(a *App) []setting {
	d := func() config.Display {
		if a.cfg == nil {
			return config.Default().Display
		}
		return a.cfg.Display
	}
	return []setting{
		{"Layout", func(a *App) string { return d().Profile },
			func(a *App, dir int) {
				p := cycle([]string{"auto", "hdmi", "crt"}, d().Profile, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.Profile = p }, false)
				a.Toast("The layout changes the next time the app starts")
			}},
		{"Screensaver", func(a *App) string {
			if m := d().ScreensaverMinutes; m > 0 {
				return fmt.Sprintf("after %d min", m)
			}
			return "Off"
		},
			func(a *App, dir int) {
				m := cycle(screensaverChoices, d().ScreensaverMinutes, dir)
				a.UpdateConfig(func(c *config.Config) { c.Display.ScreensaverMinutes = m }, false)
			}},
	}
}

// ServersScreen lists the configured servers: A switches to one, X opens
// its menu (switch, remove); the last row adds a server with the wizard.
type ServersScreen struct {
	list List
}

func NewServersScreen() *ServersScreen { return &ServersScreen{} }

func (s *ServersScreen) Title() string { return "Servers" }
func (s *ServersScreen) Enter(a *App)  {}

func (s *ServersScreen) servers(a *App) []config.Server {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Servers
}

func (s *ServersScreen) Handle(a *App, e input.Event) bool {
	srvs := s.servers(a)
	if s.list.Handle(e, len(srvs)+1) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	i := s.list.Focus
	if i >= len(srvs) { // Add a server
		if e.Button == input.BtnA {
			a.Push(NewWizardScreen(false, false))
			return true
		}
		return false
	}
	name := srvs[i].Name
	switch e.Button {
	case input.BtnA:
		a.switchServer(name)
		return true
	case input.BtnX:
		a.openMenu(name, []menuEntry{
			{"Switch to this server", func(a *App) { a.switchServer(name) }},
			{"Remove", func(a *App) {
				a.openMenu("Remove "+name+"?", []menuEntry{
					{"Remove", func(a *App) { a.removeServer(name) }},
					{"Cancel", func(*App) {}},
				})
			}},
		})
		return true
	}
	return false
}

// switchServer makes name the default and connects to it.
func (a *App) switchServer(name string) {
	if info, ok := a.Conn(); ok && info.Server.Name == name {
		a.Toast("Already connected to %s", name)
		return
	}
	a.UpdateConfig(func(c *config.Config) { c.DefaultServer = name }, false)
	a.Connect()
}

// removeServer deletes a server from the config. Removing the connected
// one connects to the next default, or, with none left, disconnects and
// starts the wizard.
func (a *App) removeServer(name string) {
	a.UpdateConfig(func(c *config.Config) { c.RemoveServer(name) }, false)
	a.Toast("Removed %s", name)
	info, connected := a.Conn()
	switch {
	case len(a.cfg.Servers) == 0:
		a.Detach()
		if a.o.Connect != nil {
			a.o.Connect(a, a.cfg.Clone()) // no server: the old session stops
		}
		a.Replace(NewWizardScreen(true, false))
	case connected && info.Server.Name == name:
		a.Connect()
	}
}

func (s *ServersScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	srvs := s.servers(a)
	info, connected := a.Conn()
	s.list.Draw(c, area, len(srvs)+1, a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		if i == len(srvs) {
			a.drawRow(c, r, row{main: "Add a server", col: colAccent, focused: focused})
			return
		}
		srv := srvs[i]
		right := ""
		switch {
		case connected && info.Server.Name == srv.Name:
			right = "connected"
		case srv.Name == a.cfg.DefaultServer:
			right = "default"
		}
		sub := displayURL(srv.URL)
		if srv.Username != "" {
			sub += " · " + srv.Username
		}
		a.drawRow(c, r, row{main: srv.Name, sub: sub, focused: focused, right: right})
	})
}

// AboutScreen shows the app version and what the connection is.
type AboutScreen struct{}

func (s *AboutScreen) Title() string                 { return "About" }
func (s *AboutScreen) Enter(a *App)                  {}
func (s *AboutScreen) Handle(*App, input.Event) bool { return false }
func (s *AboutScreen) lines(a *App) []string {
	v := a.o.Version
	if v == "" {
		v = "dev"
	}
	out := []string{"MiSTer Subsonic " + v}
	info, ok := a.Conn()
	if !ok {
		return append(out, "Not connected", "Config: "+a.o.ConfigPath)
	}
	out = append(out,
		"Server: "+info.Server.Name+" — "+displayURL(info.Server.URL),
		serverName(info.Info)+", login by "+info.Auth.String())
	if info.Info != nil && info.Info.OpenSubsonic {
		out = append(out, fmt.Sprintf("OpenSubsonic: %d extensions", len(info.Info.Extensions)))
	}
	if info.Server.InsecureSkipVerify {
		out = append(out, "Certificate check: off (insecure)")
	}
	return append(out, "Config: "+a.o.ConfigPath)
}

func (s *AboutScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	y := area.Y + p.Margin
	for _, l := range s.lines(a) {
		for _, w := range wrap(f, l, area.W-2*p.Margin) {
			f.Draw(c, area.X+p.Margin, y+f.Ascent(), w, colText, area)
			y += f.Height()
		}
	}
}
```

Then Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/cmd/mistersubsonic/session.go b/cmd/mistersubsonic/session.go
index 9f7ffef..17dbd1e 100644
--- a/cmd/mistersubsonic/session.go
+++ b/cmd/mistersubsonic/session.go
@@ -57,15 +57,18 @@ func newSessions(ctx context.Context, eng *audio.Engine, dataDir string, volume
 // connect is ui.Options.Connect.
 func (m *sessions) connect(a sessionUI, cfg *config.Config) {
 	srv, ok := cfg.ActiveServer()
-	if !ok {
-		return
-	}
-	server := *srv
 	m.mu.Lock()
 	m.gen++
 	gen, old := m.gen, m.cur
 	m.cur = nil
 	m.mu.Unlock()
+	if !ok { // no server left: just end the current session
+		if old != nil {
+			go m.stop(old)
+		}
+		return
+	}
+	server := *srv
 	go func() {
 		if old != nil {
 			m.stop(old)
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 1616960..178aedc 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -39,6 +39,8 @@ type Player interface {
 	Enqueue(songs []subsonic.Song)
 	Clear()
 	SetVolumeDB(db float64)
+	SetReplayGain(mode string)
+	SetScrobble(on bool)
 	TogglePause()
 	Next()
 	Prev()
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index f9f6621..180c572 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -34,6 +34,7 @@ var sections = []homeItem{
 	{label: "Playlists", open: func() Screen { return NewPlaylistsScreen() }},
 	{label: "Starred", open: func() Screen { return NewStarredScreen() }},
 	{label: "Search", open: func() Screen { return NewSearchScreen() }},
+	{label: "Settings", open: func() Screen { return NewSettingsScreen() }},
 }
 
 // NewRootScreen is the first screen after connecting: the sidebar and home
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 7836966..d3bdfbf 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -122,7 +122,7 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 		if e.Button == input.BtnDown {
 			step = -step
 		}
-		pl.SetVolumeDB(st.VolumeDB + step)
+		a.setVolume(st.VolumeDB + step) // and saved to the config
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/screens_setup.go b/internal/ui/screens_setup.go
index 6d93af4..a5a8b35 100644
--- a/internal/ui/screens_setup.go
+++ b/internal/ui/screens_setup.go
@@ -29,6 +29,7 @@ func (s *UnreachableScreen) Enter(a *App) {
 	if cfg := a.Config(); cfg != nil && len(cfg.Servers) > 1 {
 		s.actions = append(s.actions, menuEntry{"Switch server", func(a *App) { a.openMenu("Switch to", switchEntries(a, s.srv.Name)) }})
 	}
+	s.actions = append(s.actions, menuEntry{"Settings", func(a *App) { a.Push(NewSettingsScreen()) }})
 }
 
 // switchEntries connects to one of the other servers, making it the default.
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: `ok`. Golden screenshots written or changed: `home-hdmi`, `home-minibar-hdmi`, `root-albums-hdmi`, `root-feed-hdmi`, `settings-playback-crt`, `settings-playback-hdmi`, `settings-servers-crt`, `settings-servers-hdmi`. Open each one and check it: `settings-playback-*`: Volume with ‹ Vol 0 dB › focused, then ReplayGain off, Scrobbling On, Transcode to mp3, Transcode bitrate 320 kbps. `settings-servers-*`: home (connected) and cloud, each with URL · user, then "Add a server". `home-*` and `root-*`: the sidebar (or the list) now ends with Settings.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/session.go internal/ui/app.go internal/ui/fakes_test.go internal/ui/screens_home.go internal/ui/screens_play.go internal/ui/screens_settings.go internal/ui/screens_setup.go internal/ui/session_test.go internal/ui/settings_test.go internal/ui/testdata/golden
git commit -m "ui: Settings (servers, playback, display, about, exit); volume saved" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: The screensaver

**Files:**
- Create: `internal/ui/screensaver.go`
- Modify: `internal/ui/app.go`
- Test: `internal/ui/screensaver_test.go` (new)

**Interfaces:**
- **Produces:** `App` fields `lastInput`, `saver` and `saverSince`, plus `saverDue()`, `wake()`, `drawSaver()` and `bounce()`.
  - It starts after the configured minutes without input on Now Playing.
  - It wakes on any input, and the waking press is swallowed.
  - While it runs, the redraw cadence is `saverStep` = 2 s and the 500 ms progress tick is suppressed.

- [ ] **Step 1: Write the failing tests**

`internal/ui/screensaver_test.go` (new file):

```go
package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
)

func saverApp(t *testing.T, p Profile, minutes int) *testApp {
	t.Helper()
	ta := newTestApp(t, p)
	ta.cfg = config.Default()
	ta.cfg.Display.ScreensaverMinutes = minutes
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.lastInput = ta.now
	return ta
}

func TestScreensaverStartsAfterIdleOnNowPlaying(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 5)
	ta.now = ta.now.Add(5*time.Minute - time.Second)
	ta.onWake()
	if ta.saver {
		t.Fatal("started early")
	}
	if d := ta.untilWake(); d > progressTick {
		t.Fatalf("next wake %v while playing", d)
	}
	ta.now = ta.now.Add(time.Second)
	ta.onWake()
	if !ta.saver {
		t.Fatal("not started after 5 idle minutes")
	}
	if d := ta.untilWake(); d != saverStep {
		t.Fatalf("next wake %v with the screensaver on, want only the drift (%v)", d, saverStep)
	}
	first := ta.settle(t).ToRGBA()
	ta.now = ta.now.Add(10 * saverStep)
	ta.onWake()
	if samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("the cover doesn't drift")
	}
}

func TestScreensaverWakePressDoesNothingElse(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	ta.press(input.BtnA) // would toggle pause
	if ta.saver || len(ta.pl.calls) != 0 {
		t.Fatalf("saver %v, player calls %v", ta.saver, ta.pl.calls)
	}
	ta.press(input.BtnA)
	if len(ta.pl.calls) != 1 {
		t.Fatalf("the next press did nothing: %v", ta.pl.calls)
	}
}

func TestScreensaverOnlyOnNowPlayingAndNotWhenOff(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.Pop() // Home
	ta.now = ta.now.Add(time.Hour)
	ta.onWake()
	if ta.saver {
		t.Fatal("screensaver on a browse screen")
	}
	off := saverApp(t, ProfileHDMI, 0)
	off.now = off.now.Add(time.Hour)
	off.onWake()
	if off.saver {
		t.Fatal("screensaver while it is off")
	}
}

func TestBounce(t *testing.T) {
	for _, c := range [][3]int{{0, 10, 0}, {7, 10, 7}, {10, 10, 10}, {13, 10, 7}, {20, 10, 0}, {25, 10, 5}, {5, 0, 0}} {
		if got := bounce(c[0], c[1]); got != c[2] {
			t.Errorf("bounce(%d,%d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestGoldenScreensaver(t *testing.T) {
	for _, p := range profiles {
		ta := saverApp(t, p, 1)
		ta.now = ta.now.Add(time.Minute)
		ta.onWake()
		ta.now = ta.now.Add(7 * saverStep)
		golden(t, "screensaver-"+p.Name, ta.settle(t))
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
ta.lastInput undefined (type *testApp has no field or method lastInput)
ta.saver undefined (type *testApp has no field or method saver)
undefined: saverStep
off.saver undefined (type *testApp has no field or method saver)
```

- [ ] **Step 3: Implement**

`internal/ui/screensaver.go` (new file):

```go
package ui

import (
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
)

// The screensaver (spec §8.2): after display.screensaver_minutes without
// input on Now Playing, the screen goes dark and the cover drifts slowly
// (burn-in on plasma and OLED TVs, and CRTs). Any input wakes it, and that
// press does nothing else. It redraws only every saverStep.

const (
	saverStep  = 2 * time.Second
	saverDrift = 6 // px per step (logical)
)

func (a *App) saverMinutes() int {
	if a.cfg == nil {
		return config.Default().Display.ScreensaverMinutes
	}
	return a.cfg.Display.ScreensaverMinutes
}

// saverDue is when the screensaver starts, or zero if it can't now.
func (a *App) saverDue() time.Time {
	m := a.saverMinutes()
	if _, np := a.Top().(*NowPlayingScreen); !np || m <= 0 || a.saver {
		return time.Time{}
	}
	return a.lastInput.Add(time.Duration(m) * time.Minute)
}

// wake ends the screensaver; it reports whether it was on.
func (a *App) wake() bool {
	on := a.saver
	a.saver = false
	if on {
		a.dirty = true
	}
	return on
}

// bounce folds x into 0..span, going back and forth.
func bounce(x, span int) int {
	if span <= 0 {
		return 0
	}
	x %= 2 * span
	if x > span {
		x = 2*span - x
	}
	return x
}

func (a *App) drawSaver(c *gfx.Canvas) {
	c.Clear(gfx.RGB(0, 0, 0))
	pl := a.Player()
	if pl == nil {
		return
	}
	song, ok := pl.State().Current()
	if !ok {
		return
	}
	p := a.P
	art := p.ArtNow / 2
	steps := int(a.o.Now().Sub(a.saverSince) / saverStep)
	x := bounce(steps*saverDrift, p.W-art)
	y := bounce(steps*saverDrift*2/3, p.H-art-a.F.Small.Height())
	r := gfx.R(x, y, art, art)
	a.drawArt(c, song.CoverArt, r)
	c.Fill(r, gfx.RGBA(0, 0, 0, 0x90)) // dimmed
	fs := a.F.Small
	fs.Draw(c, x, r.Bottom()+fs.Ascent(), fs.Truncate(song.Title, art), colDim.WithAlpha(0x80), c.Bounds())
}
```

Then Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 178aedc..abeb890 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -165,25 +165,28 @@ type App struct {
 	dim bool
 	// swallowed holds buttons whose press was typed into a text field, so
 	// their releases are dropped too.
-	swallowed map[input.Button]bool
-	insecure  bool
-	stars     map[starKey]bool       // star changes made in this session
-	starGen   int                    // bumped by every successful star change
-	artists   []subsonic.ArtistIndex // getArtists, fetched once per connection
-	cfg       *config.Config
-	conn      ConnInfo
-	saveAt    time.Time  // a debounced config save is due (zero: none)
-	saving    bool       // a save is being written
-	saveAgain bool       // the config changed during that write
-	saveMu    sync.Mutex // one writer of the config file at a time
-	rep       input.Repeater
-	in        chan input.Event
-	post      chan func()
-	loads     int
-	dirty     bool
-	quit      bool
-	bDown     time.Time // when B went down on the root screen (zero if not held)
-	confirm   bool      // exit confirmation shown
+	swallowed  map[input.Button]bool
+	insecure   bool
+	stars      map[starKey]bool       // star changes made in this session
+	starGen    int                    // bumped by every successful star change
+	artists    []subsonic.ArtistIndex // getArtists, fetched once per connection
+	cfg        *config.Config
+	conn       ConnInfo
+	saveAt     time.Time  // a debounced config save is due (zero: none)
+	saving     bool       // a save is being written
+	saveAgain  bool       // the config changed during that write
+	saveMu     sync.Mutex // one writer of the config file at a time
+	lastInput  time.Time  // for the screensaver
+	saver      bool       // the screensaver is on
+	saverSince time.Time
+	rep        input.Repeater
+	in         chan input.Event
+	post       chan func()
+	loads      int
+	dirty      bool
+	quit       bool
+	bDown      time.Time // when B went down on the root screen (zero if not held)
+	confirm    bool      // exit confirmation shown
 }
 
 func New(o Options) (*App, error) {
@@ -191,7 +194,7 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, cfg: o.Config}
+		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -427,10 +430,14 @@ func (a *App) untilWake() time.Duration {
 	}
 	consider(a.mqWake)
 	consider(a.saveAt)
+	consider(a.saverDue())
+	if a.saver {
+		consider(now.Add(saverStep)) // the drift; nothing else moves
+	}
 	if !a.bDown.IsZero() {
 		consider(a.bDown.Add(exitHold))
 	}
-	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
+	if a.o.Player != nil && a.o.Player.State().Status == player.Playing && !a.saver {
 		consider(now.Add(progressTick))
 	}
 	if d := next.Sub(now); d > 0 {
@@ -469,6 +476,12 @@ func (a *App) onWake() {
 			a.dirty = true
 		}
 	}
+	if due := a.saverDue(); !due.IsZero() && !now.Before(due) {
+		a.saver, a.saverSince, a.dirty = true, now, true
+	}
+	if a.saver {
+		a.dirty = true // the drift
+	}
 	if !a.saveAt.IsZero() && !now.Before(a.saveAt) {
 		a.saveAt = time.Time{}
 		a.saveConfig()
@@ -499,6 +512,10 @@ func (a *App) onPlayer(ev player.Event) {
 
 func (a *App) onInput(e input.Event) {
 	now := a.o.Now()
+	a.lastInput = now
+	if a.wake() && e.Kind == input.Press {
+		return // the press that wakes the screensaver does nothing else
+	}
 	if e.Rune != 0 && !a.confirm {
 		if e.Kind == input.Press {
 			if t, ok := a.Top().(TextInput); ok && t.Text(a, e.Rune) {
@@ -593,6 +610,13 @@ func (a *App) hasCurrent() bool {
 
 func (a *App) render() error {
 	a.dirty = false
+	if _, np := a.Top().(*NowPlayingScreen); !np {
+		a.saver = false
+	}
+	if a.saver {
+		a.drawSaver(a.canvas)
+		return a.o.Display.Present(a.scaler.Scale(a.canvas))
+	}
 	a.animate, a.mq.seen, a.mqWake, a.dim = false, false, time.Time{}, false
 	c := a.canvas
 	c.Clear(colBg)
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `screensaver-crt`, `screensaver-hdmi`. Open each one and check it: black, a dimmed cover (half the Now Playing size) near the top-left, drifted from the corner, and the title under it in dim text.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/screensaver.go internal/ui/screensaver_test.go internal/ui/testdata/golden
git commit -m "ui: screensaver on Now Playing" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Follow-ups from the Plan 2b reviews

**Files:**
- Modify: `internal/ui/star.go`, `internal/ui/app.go`, `internal/ui/screens_home.go`, `internal/ui/screens_root.go`, `internal/ui/session.go`, `internal/ui/menus.go`, `internal/ui/screens_album.go`, `internal/ui/screens_play.go`
- Test: `internal/ui/followups_test.go` (new); `internal/ui/fixwave_test.go`, `internal/ui/widgets_test.go`, `internal/ui/session_test.go` (modified: `toggleStar` loses its owner argument)

**Interfaces:**
- **Produces:**
  - `(*App).toggleStar(it)`. The owner argument is gone: the request runs under the root screen, and a second toggle while one is in flight is ignored (`App.starBusy`).
  - `App.Pop` calls `Shown` on the screen shown again. `SidebarRoot.Shown` passes it on to the section, so Starred reloads after a star change.
  - `HomeScreen` (CRT) drops its Resume row once a queue exists.

- [ ] **Step 1: Write the failing tests**

`internal/ui/followups_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// CRT Home: once something plays, Resume would replace the live queue.
func TestCRTHomeDropsResumeOncePlaying(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	home := NewHomeScreen()
	ta.Push(home)
	ta.settle(t)
	if home.resume == nil {
		t.Fatal("no Resume row")
	}
	playingState(ta) // e.g. an album was played and B brought us back
	ta.settle(t)
	ta.press(input.BtnA)
	if slices.Contains(ta.pl.calls, "resume") {
		t.Fatal("A resumed the saved queue over the live one")
	}
	if l, ok := ta.Top().(*AlbumListScreen); !ok || l.title != "Recently added" {
		t.Fatalf("A opened %T", ta.Top())
	}
}

// Starred refreshes when a screen above it closes (Go to album, star, B).
func TestStarredRefreshesWhenShownAgainAfterPop(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewStarredScreen())
	ta.settle(t)
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	ta.settle(t)
	ta.lib.starred.Albums = append(ta.lib.starred.Albums, ta.lib.albums[0])
	ta.toggleStar(albumStar(ta.lib.albums[0]))
	ta.settle(t)
	ta.press(input.BtnB)
	ta.settle(t)
	if ta.lib.starCalls != 2 {
		t.Fatalf("getStarred2 called %d times, want a reload", ta.lib.starCalls)
	}
}

// Leaving the screen before the server answers doesn't lose the star.
func TestStarSurvivesLeavingTheScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	it := albumStar(ta.lib.albums[0])
	ta.toggleStar(it)
	ta.toggleStar(it) // a second press while the first is in flight
	ta.Pop()
	ta.settle(t)
	if !ta.isStarred(it) || !slices.Equal(ta.lib.stars, []string{"star al-1"}) {
		t.Fatalf("starred %v, server calls %v", ta.isStarred(it), ta.lib.stars)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t8-test.patch` and apply it from the repository root with `git apply /tmp/t8-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/ui/fixwave_test.go b/internal/ui/fixwave_test.go
index c43afa7..55a2164 100644
--- a/internal/ui/fixwave_test.go
+++ b/internal/ui/fixwave_test.go
@@ -19,7 +19,7 @@ func TestStarStateIsPerKind(t *testing.T) {
 	ta.Push(s)
 	artist := starItem{kind: starArtist, id: "7", name: "Artist seven"}
 	album := starItem{kind: starAlbum, id: "7", name: "Album seven"}
-	ta.toggleStar(s, artist)
+	ta.toggleStar(artist)
 	ta.settle(t)
 	if !ta.isStarred(artist) {
 		t.Fatal("artist 7 not starred")
@@ -206,7 +206,7 @@ func TestStarredRefreshesAfterAStarChange(t *testing.T) {
 	}
 	star := func(t *testing.T, ta *testApp, owner Screen) {
 		ta.lib.starred.Albums = append(ta.lib.starred.Albums, ta.lib.albums[0]) // the server's view
-		ta.toggleStar(owner, albumStar(ta.lib.albums[0]))
+		ta.toggleStar(albumStar(ta.lib.albums[0]))
 		ta.settle(t)
 	}
 	t.Run("sidebar", func(t *testing.T) {
diff --git a/internal/ui/session_test.go b/internal/ui/session_test.go
index 2d02a88..57ff101 100644
--- a/internal/ui/session_test.go
+++ b/internal/ui/session_test.go
@@ -35,9 +35,22 @@ func sessionApp(t *testing.T, cfg *config.Config) (*testApp, *connectRecorder) {
 	ta.o.ConfigPath = filepath.Join(t.TempDir(), "config.toml")
 	ta.o.Connect = rec.connect
 	ta.Detach()
+	t.Cleanup(func() { waitSaves(ta) }) // before the temp dir goes
 	return ta, rec
 }
 
+// waitSaves lets config writes in flight finish (they post back when done).
+func waitSaves(ta *testApp) {
+	deadline := time.Now().Add(2 * time.Second)
+	for (ta.saving || len(ta.post) > 0) && time.Now().Before(deadline) {
+		select {
+		case f := <-ta.post:
+			f()
+		case <-time.After(5 * time.Millisecond):
+		}
+	}
+}
+
 func messageTitle(ta *testApp) string {
 	if m, ok := ta.Top().(*MessageScreen); ok {
 		return m.title
diff --git a/internal/ui/widgets_test.go b/internal/ui/widgets_test.go
index 88c50dd..636fd27 100644
--- a/internal/ui/widgets_test.go
+++ b/internal/ui/widgets_test.go
@@ -81,18 +81,18 @@ func TestToggleStarRemembersAndToasts(t *testing.T) {
 	s := &probe{}
 	ta.Push(s)
 	it := albumStar(ta.lib.albums[1])
-	ta.toggleStar(s, it)
+	ta.toggleStar(it)
 	ta.settle(t)
 	if !ta.isStarred(it) || ta.toasts[0].text != "Starred Homogenic" {
 		t.Fatalf("starred %v, toasts %v", ta.isStarred(it), ta.toasts)
 	}
-	ta.toggleStar(s, it)
+	ta.toggleStar(it)
 	ta.settle(t)
 	if ta.isStarred(it) || !slices.Equal(ta.lib.stars, []string{"star al-2", "unstar al-2"}) {
 		t.Fatalf("starred %v, calls %v", ta.isStarred(it), ta.lib.stars)
 	}
 	ta.lib.err = errOffline
-	ta.toggleStar(s, it)
+	ta.toggleStar(it)
 	ta.settle(t)
 	if ta.isStarred(it) {
 		t.Fatal("a failed star changed the state")
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
not enough arguments in call to ta.toggleStar
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index abeb890..d3fb09b 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -168,6 +168,7 @@ type App struct {
 	swallowed  map[input.Button]bool
 	insecure   bool
 	stars      map[starKey]bool       // star changes made in this session
+	starBusy   map[starKey]bool       // star requests in flight
 	starGen    int                    // bumped by every successful star change
 	artists    []subsonic.ArtistIndex // getArtists, fetched once per connection
 	cfg        *config.Config
@@ -194,7 +195,7 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
+		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -273,6 +274,7 @@ func (a *App) Push(s Screen) {
 }
 
 // Pop closes the top screen (never the last one) and cancels its loads.
+// The screen shown again is told (Shown), so it can refresh.
 func (a *App) Pop() {
 	if len(a.stack) <= 1 {
 		return
@@ -281,6 +283,9 @@ func (a *App) Pop() {
 	top.cancel()
 	a.stack = a.stack[:len(a.stack)-1]
 	a.dirty = true
+	if sh, ok := a.Top().(shower); ok {
+		sh.Shown(a)
+	}
 }
 
 // popTo pops screens until pred matches the top; if no screen in the stack
diff --git a/internal/ui/menus.go b/internal/ui/menus.go
index 4825375..3104f53 100644
--- a/internal/ui/menus.go
+++ b/internal/ui/menus.go
@@ -154,7 +154,7 @@ func starEntry(a *App, it starItem) menuEntry {
 	if a.isStarred(it) {
 		label = "Unstar"
 	}
-	return menuEntry{label, func(a *App) { a.toggleStar(a.Top(), it) }}
+	return menuEntry{label, func(a *App) { a.toggleStar(it) }}
 }
 
 // The menu builders take the App to label Star/Unstar.
diff --git a/internal/ui/screens_album.go b/internal/ui/screens_album.go
index 31c192f..8e67273 100644
--- a/internal/ui/screens_album.go
+++ b/internal/ui/screens_album.go
@@ -81,7 +81,7 @@ func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
 		case s.list.Focus == 1:
 			s.play(a, 0, true)
 		case s.list.Focus == 2:
-			a.toggleStar(s, albumStar(s.info()))
+			a.toggleStar(albumStar(s.info()))
 		default:
 			s.play(a, s.list.Focus-albumActionRows, false)
 		}
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index 180c572..f390edc 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -72,6 +72,17 @@ func (s *HomeScreen) Enter(a *App) {
 	})
 }
 
+// sync drops the Resume row once something is playing: resuming then
+// would replace the live queue.
+func (s *HomeScreen) sync(a *App) {
+	if s.resume != nil && a.hasQueue() {
+		s.resume = nil
+		if s.list.Focus > 0 {
+			s.list.Focus-- // keep the focus on the same item
+		}
+	}
+}
+
 func (s *HomeScreen) items() []homeItem {
 	var out []homeItem
 	if s.resume != nil {
@@ -82,6 +93,7 @@ func (s *HomeScreen) items() []homeItem {
 }
 
 func (s *HomeScreen) Handle(a *App, e input.Event) bool {
+	s.sync(a)
 	items := s.items()
 	if s.list.Handle(e, len(items)) {
 		return true
@@ -104,6 +116,7 @@ func (s *HomeScreen) Handle(a *App, e input.Event) bool {
 }
 
 func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
+	s.sync(a)
 	items := s.items()
 	s.list.Draw(c, area, len(items), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
 		col := colText
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index d3bdfbf..1cc776f 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -133,7 +133,7 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 		pl.TogglePause()
 	case input.BtnX:
 		if song, ok := st.Current(); ok {
-			a.toggleStar(s, songStar(song))
+			a.toggleStar(songStar(song))
 		}
 	case input.BtnL:
 		pl.Prev()
diff --git a/internal/ui/screens_root.go b/internal/ui/screens_root.go
index 9e6a417..a4b2f82 100644
--- a/internal/ui/screens_root.go
+++ b/internal/ui/screens_root.go
@@ -80,6 +80,14 @@ func (s *SidebarRoot) open(a *App) Screen {
 	return s.children[s.sel]
 }
 
+// Shown passes on to the section when the root is visible again (a screen
+// above it was closed).
+func (s *SidebarRoot) Shown(a *App) {
+	if sh, ok := s.current().(shower); ok {
+		sh.Shown(a)
+	}
+}
+
 // current is the selected section, nil while it hasn't been opened yet.
 func (s *SidebarRoot) current() Screen { return s.children[s.sel] }
 
diff --git a/internal/ui/session.go b/internal/ui/session.go
index f33ead9..522f284 100644
--- a/internal/ui/session.go
+++ b/internal/ui/session.go
@@ -77,7 +77,7 @@ func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
 	a.Attach(lib, pl, art)
 	a.conn = info
 	a.SetInsecure(info.Server.InsecureSkipVerify)
-	a.artists, a.stars, a.starGen = nil, map[starKey]bool{}, 0
+	a.artists, a.stars, a.starBusy, a.starGen = nil, map[starKey]bool{}, map[starKey]bool{}, 0
 	a.Replace(NewRootScreen(a.P))
 	a.drainInput()
 }
diff --git a/internal/ui/star.go b/internal/ui/star.go
index a0fa94b..ee72984 100644
--- a/internal/ui/star.go
+++ b/internal/ui/star.go
@@ -60,20 +60,24 @@ func (a *App) isStarred(it starItem) bool {
 	return it.server
 }
 
-// toggleStar stars or unstars it on the server (off the UI goroutine,
-// under owner) and remembers the new state once the server agrees.
-func (a *App) toggleStar(owner Screen, it starItem) {
+// toggleStar stars or unstars it on the server (off the UI goroutine) and
+// remembers the new state once the server agrees. The request runs under
+// the root screen, so leaving the screen it came from doesn't lose the
+// answer; a second toggle while one is in flight is ignored.
+func (a *App) toggleStar(it starItem) {
 	lib := a.Library()
-	if lib == nil {
+	if lib == nil || len(a.stack) == 0 || a.starBusy[it.key()] {
 		return
 	}
 	on := !a.isStarred(it)
-	a.Load(owner, func(ctx context.Context) (any, error) {
+	a.starBusy[it.key()] = true
+	a.Load(a.stack[0].s, func(ctx context.Context) (any, error) {
 		if on {
 			return nil, lib.Star(ctx, it.target())
 		}
 		return nil, lib.Unstar(ctx, it.target())
 	}, func(_ any, err error) {
+		delete(a.starBusy, it.key())
 		verb := "unstar"
 		if on {
 			verb = "star"
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/fixwave_test.go internal/ui/followups_test.go internal/ui/menus.go internal/ui/screens_album.go internal/ui/screens_home.go internal/ui/screens_play.go internal/ui/screens_root.go internal/ui/session.go internal/ui/session_test.go internal/ui/star.go internal/ui/widgets_test.go
git commit -m "ui: stars survive leaving the screen, Starred refreshes after a pop, CRT Home drops a stale Resume" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 9: Scripted keys for the wizard, the e2e setup run, README

**Files:**
- Modify: `cmd/mistersubsonic/keys.go`, `scripts/e2e-ui.sh`, `README.md`
- Test: `cmd/mistersubsonic/keys_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `-keys` items: `enter` is A with `'\n'`, `pause:<d>` only waits, and `'text` is typed verbatim (it may contain `:`).
  - `make e2e` gains a second UI run with no config. The wizard is typed against the mock server, then Save, then the feed's first cover is played. The check passes only if `config.toml` has a token and salt and no password, and a stream follows.
  - README: a Setup section covering the wizard, Settings and the `servers/` folders.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t9-test.patch` and apply it from the repository root with `git apply /tmp/t9-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 8):

```diff
diff --git a/cmd/mistersubsonic/keys_test.go b/cmd/mistersubsonic/keys_test.go
index ffadca2..4458dfe 100644
--- a/cmd/mistersubsonic/keys_test.go
+++ b/cmd/mistersubsonic/keys_test.go
@@ -8,14 +8,16 @@ import (
 )
 
 func TestParseKeys(t *testing.T) {
-	got, err := parseKeys("a:2s, down ,'abba:1s,queue")
+	got, err := parseKeys("a:2s, down ,'127.0.0.1:4533,pause:1s,enter,queue")
 	if err != nil {
 		t.Fatal(err)
 	}
 	want := []scripted{
 		{b: input.BtnA, wait: 2 * time.Second},
 		{b: input.BtnDown, wait: 400 * time.Millisecond},
-		{text: "abba", wait: time.Second},
+		{text: "127.0.0.1:4533", wait: 400 * time.Millisecond},
+		{pause: true, wait: time.Second},
+		{b: input.BtnA, r: '\n', wait: 400 * time.Millisecond},
 		{b: input.BtnQueue, wait: 400 * time.Millisecond},
 	}
 	if len(got) != len(want) {
@@ -23,10 +25,10 @@ func TestParseKeys(t *testing.T) {
 	}
 	for i := range want {
 		if got[i] != want[i] {
-			t.Fatalf("got %v, want %v", got, want)
+			t.Fatalf("item %d: got %+v, want %+v", i, got[i], want[i])
 		}
 	}
-	for _, bad := range []string{"jump", "a:soon", "'"} {
+	for _, bad := range []string{"jump", "a:soon", "'", "pause:x"} {
 		if _, err := parseKeys(bad); err == nil {
 			t.Fatalf("%q accepted", bad)
 		}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
unknown field pause in struct literal of type scripted
unknown field r in struct literal of type scripted
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t9-code.patch` and apply it from the repository root with `git apply /tmp/t9-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 8):

````diff
diff --git a/README.md b/README.md
index ed3dd27..b246483 100644
--- a/README.md
+++ b/README.md
@@ -6,9 +6,25 @@ Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.
 Status: **playback core plus the library interface**: a home feed with resume, artists (A–Z),
 albums (A–Z, by year, by genre), playlists, starred items and search with an on-screen keyboard,
 album pages, Now Playing and the queue, on HDMI (sidebar and cover grids) or CRT (lists)
-layouts, driven by a controller or keyboard. Streaming, FLAC/MP3/WAV decoding, gapless playback,
-seek, scrobbling and resume work. Still to come: the setup wizard, settings, the screensaver and
-the MiSTer launcher. On-device (MiSTer) checks are still pending; see `docs/spikes.md`.
+layouts, driven by a controller or keyboard; a setup wizard, Settings (servers, playback,
+display) and a screensaver. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
+scrobbling and resume work. Still to come: the MiSTer launcher and releases. On-device (MiSTer)
+checks are still pending; see `docs/spikes.md`.
+
+## Setup
+
+On first start the app runs a setup wizard: server address (`http://` / `https://` and `:4533`
+are one key away), username, password and, for servers that hand them out, an API key. It
+tests the login and saves `config.toml` next to the app. With token login (most servers) only a
+token and salt are saved, never the password; servers that need the plain password (LDAP) get it
+stored only after you allow it, and a self-signed certificate is skipped only after you allow
+that (the safer way is `ca_file`, below). A config file the app can't read is kept as
+`config.toml.invalid-<date>` when the wizard replaces it.
+
+Settings (the last section) switches between servers, adds and removes them, and changes the
+volume, ReplayGain, scrobbling, transcoding, the layout and the screensaver (dark screen with a
+drifting cover after some idle minutes on Now Playing; off with 0). Each server keeps its resume
+state, scrobble queue and cover cache in `servers/<name>/` next to the config.
 
 ## Controls
 
@@ -48,7 +64,7 @@ bin/mss-cli -config config.toml ping
 bin/mss-cli -config config.toml play-album <album-id>
 ```
 
-`config.toml` needs at least one server:
+`config.toml` can also be written by hand; it needs at least one server:
 
 ```toml
 [[server]]
diff --git a/cmd/mistersubsonic/keys.go b/cmd/mistersubsonic/keys.go
index f249a40..aac0397 100644
--- a/cmd/mistersubsonic/keys.go
+++ b/cmd/mistersubsonic/keys.go
@@ -16,15 +16,19 @@ var keyNames = map[string]input.Button{
 }
 
 type scripted struct {
-	b    input.Button
-	text string        // typed instead of a button press when set
-	wait time.Duration // pause before this press
+	b     input.Button
+	r     rune          // typed with the press (Enter types '\n')
+	text  string        // typed instead of a button press when set
+	pause bool          // only waits
+	wait  time.Duration // pause before this press
 }
 
 // parseKeys reads a -keys script: comma-separated button names, each
 // optionally followed by ":<duration>" to wait before pressing it, e.g.
-// "a:2s,a:1s,a,a:1s". An item starting with ' types the rest as keyboard
-// text ("'abba:1s"). The default wait is 400 ms.
+// "a:2s,a:1s,a,a:1s". "enter" is the Enter key (A, also typing a newline),
+// "pause:<duration>" only waits, and an item starting with ' types the
+// rest as keyboard text, verbatim ("'127.0.0.1:4533"). The default wait
+// is 400 ms.
 func parseKeys(s string) ([]scripted, error) {
 	var out []scripted
 	for _, part := range strings.Split(s, ",") {
@@ -32,12 +36,26 @@ func parseKeys(s string) ([]scripted, error) {
 		if part == "" {
 			continue
 		}
-		name, wait, found := strings.Cut(part, ":")
 		k := scripted{wait: 400 * time.Millisecond}
-		if text, ok := strings.CutPrefix(name, "'"); ok && text != "" {
+		if text, ok := strings.CutPrefix(part, "'"); ok {
+			if text == "" {
+				return nil, fmt.Errorf("-keys: empty text item")
+			}
 			k.text = text
-		} else if k.b, ok = keyNames[name]; !ok {
-			return nil, fmt.Errorf("-keys: unknown button %q", name)
+			out = append(out, k)
+			continue
+		}
+		name, wait, found := strings.Cut(part, ":")
+		switch name {
+		case "pause":
+			k.pause = true
+		case "enter":
+			k.b, k.r = input.BtnA, '\n'
+		default:
+			var ok bool
+			if k.b, ok = keyNames[name]; !ok {
+				return nil, fmt.Errorf("-keys: unknown button %q", name)
+			}
 		}
 		if found {
 			var err error
@@ -56,15 +74,17 @@ func playKeys(script []scripted) <-chan input.Event {
 	go func() {
 		for _, k := range script {
 			time.Sleep(k.wait)
-			if k.text != "" {
+			switch {
+			case k.pause:
+			case k.text != "":
 				for _, r := range k.text {
 					ch <- input.Event{Kind: input.Press, Rune: r}
 					ch <- input.Event{Kind: input.Release, Rune: r}
 				}
-				continue
+			default:
+				ch <- input.Event{Button: k.b, Kind: input.Press, Rune: k.r}
+				ch <- input.Event{Button: k.b, Kind: input.Release, Rune: k.r}
 			}
-			ch <- input.Event{Button: k.b, Kind: input.Press}
-			ch <- input.Event{Button: k.b, Kind: input.Release}
 		}
 	}()
 	return ch
diff --git a/scripts/e2e-ui.sh b/scripts/e2e-ui.sh
index 80da703..811c4f5 100755
--- a/scripts/e2e-ui.sh
+++ b/scripts/e2e-ui.sh
@@ -54,4 +54,25 @@ if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log" || ! searched_
   cat "$tmp/mock.log" >&2
   exit 1
 fi
-echo "e2e-ui ok: $frames frames rendered; the feed and Search both played (null device)"
+
+# Second run, no config yet: the setup wizard (typed as keyboard text)
+# tests the mock server, saves the config (a token, not the password) and
+# connects; then the feed's first cover plays.
+setup="$tmp/setup"
+mkdir "$setup"
+before=$(wc -l < "$tmp/mock.log")
+"$tmp/mistersubsonic" -config "$setup/config.toml" -display headless -null \
+  -keys "pause:1500ms,'127.0.0.1:$port,enter,'test,enter,'test,enter,enter,a:2s,a:3s,a:1s" \
+  -exit-after 14s > "$tmp/setup.log" 2>&1 || {
+  echo "e2e-ui failed: the setup run exited with an error" >&2
+  cat "$tmp/setup.log" >&2
+  exit 1
+}
+if ! grep -q '^ *token = ' "$setup/config.toml" 2>/dev/null || ! grep -q '^ *salt = ' "$setup/config.toml" ||
+  grep -q 'password' "$setup/config.toml" || ! tail -n +"$((before + 1))" "$tmp/mock.log" | grep -q 'stream so-'; then
+  echo "e2e-ui failed: the wizard didn't save a token config or nothing played after it; config:" >&2
+  cat "$setup/config.toml" >&2 2>/dev/null
+  tail -n +"$((before + 1))" "$tmp/mock.log" >&2
+  exit 1
+fi
+echo "e2e-ui ok: $frames frames rendered; the feed and Search played; the setup wizard saved a token config and played (null device)"
````

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./cmd/mistersubsonic`

Expected: `ok` for every package; `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run the checks:

```bash
make e2e
PATH=<zig 0.16.0 dir>:$PATH make mister
```

Expected:
- `e2e-ui ok: … the setup wizard saved a token config and played (null device)`
- `bin/arm/mistersubsonic: needs glibc 2.29 (<= 2.31) ok`

Make sure the new check can fail: temporarily delete the final `,a:1s` (Play) from the setup run's `-keys` in `scripts/e2e-ui.sh` and run `make e2e`. Expected: `e2e-ui failed: the wizard didn't save a token config or nothing played after it`. Then restore the line.

- [ ] **Step 5: Commit**

```bash
git add README.md cmd/mistersubsonic/keys.go cmd/mistersubsonic/keys_test.go scripts/e2e-ui.sh
git commit -m "app: -keys enter/pause/verbatim text; e2e sets up with the wizard; README setup" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 10: Docs and the on-device check (when the MiSTer is available)

**Files:**
- Modify: `docs/spikes.md`, `docs/superpowers/plan-2a-followups.md`

- [ ] **Step 1: Record what this plan resolved**

Append to `docs/superpowers/plan-2a-followups.md`:

```markdown

## Resolved by Plan 2c

- CRT Home drops its Resume row once something plays.
- Server switching:
  - Loads capture the library and player on the UI goroutine.
  - The artist and star caches reset on every connection.
  - The session manager replaces sessions safely: the newest request wins, and old players are stopped with their queue saved.
- Starred reloads when shown again after a Pop.
- A star request survives leaving its screen, and a double press is ignored while one is in flight.
- `config.Save` no longer names the missing `config.example.toml`.

Still open: the Plan 3 items, the marquee and tab-row nit, the input and UX nits, and the test gaps listed above.
```

- [ ] **Step 2: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, add this to the end of `docs/spikes.md`, commit (Step 4), and stop:

```markdown

## Plan 2c on the MiSTer

pending — MiSTer unavailable. Still to do: run the setup wizard on the TV with the controller and a USB keyboard; Settings → Servers switch; check the screensaver on HDMI and CRT; confirm the config lands in /media/fat/mistersubsonic with mode 0600.
```

- [ ] **Step 3: If the MiSTer answers, try it on the TV (ask the user first)**

Ask the user to run the app on the TV:
1. Move `config.toml` aside first. The user does this; it's their file.
2. Pressing Start plays sound, so the volume should be low.

Ask them to report:
- Does the wizard work with the controller and with a USB keyboard?
- Does the saved config hold a token and no password?
- Does switching servers work?
- Does the screensaver start and wake?

Record the answers.

- [ ] **Step 4: Commit**

```bash
git add docs/spikes.md docs/superpowers/plan-2a-followups.md
git commit -m "docs: record Plan 2c results and follow-ups" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

Plan 3 covers:
- the MiSTer launcher and the Scripts entry
- KD_GRAPHICS and console restore
- the BGM and SAM pause
- the framebuffer-overwrite watchdog
- `log.txt` rotation
- `config.example.toml`
- releases and the Downloader database
- the device performance work (render cost, memory) and the remaining Plan 3 follow-ups

Not covered by 2a–2c: the mute toggle in spec §6. It is left for Plan 3, next to the volume work: a Settings row and/or a Now Playing key.
