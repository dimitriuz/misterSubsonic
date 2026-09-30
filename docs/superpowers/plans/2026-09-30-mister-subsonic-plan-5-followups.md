# MiSTer Subsonic — Plan 5: small follow-ups

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** fix the small glitches, the misleading texts and the rare failures collected in `docs/superpowers/plan-2a-followups.md` and backlog A. The user chose four groups:
- UI glitches and misleading text;
- MP3 seeking;
- robustness of the stream, the player, full resolution, input, the logs and the launcher;
- config comments kept on save, and faster covers.

**Architecture:** no new subsystem. Each task changes the package that owns its items:
- `internal/ui`: Settings, the screensaver, hints, menus, the wizard, `popTo`.
- `internal/audio`: `Engine.Replace`.
- `internal/player`: the MP3 layout and the shared MP3 stream.
- `internal/stream`: `Holds`, `SeekIfBuffered`, `Promote`, Retry-After and Open.
- `internal/platform`: the full-resolution state.
- `internal/input`: `Close` and the device checks.
- `internal/logfile`, the launcher and `tools/mkdb`.
- `cmd/mistersubsonic`: the server folders and the forced exit.
- `internal/cache` and `internal/art`.
- `internal/config`: editing the file in place.
- `internal/gfx`: the box filter.

**Tech Stack:** Go 1.26+, cgo (miniaudio, speexdsp) in `internal/audio` only, no new modules. The launcher tests use bash and python3.

**Spec:** there is no separate design doc; the items are the follow-ups themselves. The main spec, `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`, still applies. Each item names its entry in `docs/superpowers/plan-2a-followups.md` or in `docs/superpowers/plans/backlog.md` (A). Task 9 moves the resolved items to "Resolved by Plan 5".

**Scope:**

| | |
|---|---|
| **Plans 1–4b and the hotfixes (done)** | Playback, the TV interface, setup, MiSTer integration, releases, native HDMI, full resolution, partial redraws, hints, the Settings help lines, quitting on a display reconnect |
| **This plan (5)** | The items above, and the TV checks for them |
| **Later** | Faster list scrolling at full resolution, a faster JPEG decoder or smaller covers, the remaining minors, the visualizer (B) and the web remote (C) |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:** `go test -race ./...`, the launcher tests, `make e2e` and the ARM builds pass. The ARM builds are `make mister mister-test`, with glibc 2.29 ≤ 2.31, and `gfx.test` needs no glibc.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-5`. At every task the new tests failed before the code and passed after, and the tree ended identical to the prototype's, goldens included.
- **On the MiSTer:** the cover benchmark ran as a test binary only. Nothing was drawn on the TV and no sound played. The numbers are in Task 9's `docs/spikes.md` patch.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:**
  - Go 1.26+ (`go.mod` says `go 1.26.0`).
  - No new modules.
  - cgo only in `internal/audio`.
  - ARM build: `scripts/check-glibc.sh` must report ≤ 2.31, or no glibc for static binaries.
- **The SD card:** nothing new writes to it more often than today.
  - A config save still writes one temporary file and renames it, with mode 0600.
  - The full-resolution state stays in `/tmp` (RAM).
- **The launcher** runs on the MiSTer's bash 5.0.18. It uses only tools that are there: flock, pidof, timeout and socat, and not pgrep.
- **The console:** every framebuffer size change happens while the console is in graphics mode.
- **Hotplug behaviour is unchanged:** a display reconnect still makes the app quit cleanly.
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device, never a real sound card.
  - Ask the user's go-ahead each time before a listening check, or before anything is shown on the TV.
- **Tests never touch real devices**, such as `/dev/tty*`, `/dev/fb0`, `/dev/input`, `/dev/MiSTer_cmd`, a sound card or the MiSTer. They use no network beyond `httptest`, and write only under `t.TempDir()`.
- **Secrets:**
  - Nothing from `.env` is printed, committed or written into docs.
  - Config tests use made-up values.
  - Token, salt and password handling is unchanged.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These are the five situations most likely to bite the user. Each gets its test in the task that owns the code:

1. **A seek that loses the next track, or plays the wrong one.** An MP3 seek inside the buffer swaps the decoder and keeps the queued track. This must hold even when the old track ends during the swap, or the new decoder fails to open.
   - Engine tests (Task 3): `TestEngineReplaceKeepsTheQueuedSuccessor`, `…OpenFailureContinuesIntoTheSuccessor`, `…FailureBeforeTheSuccessorOpenedStartsItLater`, `…WhileTheOldTrackEndsKeepsTheSuccessor`.
   - Player test (Task 3): `TestSeekingAnMP3InsideTheStreamWindowKeepsTheNextTrack`.
2. **A config file the editor doesn't understand.** Inline tables, multi-line strings, reordered servers, CRLF or garbage must never lose a setting. The edited text must load back to exactly the saved config; otherwise Save rewrites the file as before. Tests: `TestSaveFallsBackOnLayoutItCannotEdit`, `TestSaveOverGarbageWritesAFreshFile`, `TestSaveKeepsCRLF` (Task 8).
3. **The console size after a failure.** A stale state from a crashed run, a failed restore, or a forced exit must leave the menu at its own size.
   - Tests (Task 5): `TestRestoreForgetsStaleState`, `TestRestoreKeepsTheStateWhenTheRequestFails`, `TestForceExitRestoresTheSizeThenText`.
   - The hotplug tests stay green.
4. **The launcher killed mid-run.** TERM or INT reaches the app, and the restore runs to the end even if a second signal comes. Tests: the `term`, `int` and `int-restoring` cases in `scripts/test-launcher.sh` (Task 6).
5. **Covers that look different after the speed-up.** The new box filter must give exactly the old pixels for every source type, box size and origin. Tests (Task 8): `TestBoxFilterMatchesTheClosureVersion`, `TestBoxFilterBigBoxesMatchTheClosureVersion`, `TestOpaqueFastPathIsUsedAndExact`, `TestRecipsDivideExactly`. They compare against the old code, which is kept in `refbox_test.go`.

## Decisions this plan makes (from the prototype; the follow-ups left room)

- **Settings:**
  - On/Off rows (Scrobbling, Hints) change only on a press.
  - Rows with several choices keep repeating while held.
  - A hand-edited number (bitrate 160, screensaver 7) goes to the nearest choice in the pressed direction. Past either end it wraps.
- **The screensaver:**
  - A toast wakes it, so a failed track is seen.
  - "Screenshot saved" is not shown while it is on, so a screenshot still doesn't wake it.
  - It never starts over the exit prompt.
  - The press that wakes it has its Release swallowed too.
- **Hints on a keyboard:**
  - Letters type and Backspace deletes, so typing screens hint "Backspace Delete" (only when there is text).
  - The wizard hints "Enter Next".
  - Search doesn't hint Enter: there Enter presses the focused on-screen key.
  - Back is hinted where B really goes back: the root with the content focused, and the wizard after its first step.
- **CRT playlist page:** rows are one line ("Title — Artist") on the list layout, so several tracks fit under Play and Shuffle. HDMI is unchanged.
- **MP3 seeks:**
  - An MP3 of known size and duration always opens as a view over a shared, reference-counted reader, even at offset 0.
  - A seek whose byte lies in the reader's buffer gets a new view. The engine swaps the decoder with `Engine.Replace` and keeps the queued track.
  - A seek outside the buffer reopens as before.
  - The prototype found an old race in `stream.Reader`: a seek back during a fetch could count on bytes that the fetch was about to overwrite. The reader now gives those bytes up when the fetch starts.
  - The Xing/Info table's fractions apply to the audio bytes from the first frame, as in the standard. A tag over 256 KiB skips the table.
- **Stream:**
  - Retry-After is clamped to 24 h. On 32-bit ARM, `Atoi` overflowed to 0, which meant "retry at once".
  - A file that shrinks between requests and then ends cleanly ends at its new size, without an error.
  - Each Open request is bounded by the retry budget with a timer, not a deadline context, so the body that keeps streaming isn't cut later.
- **Full resolution:**
  - The state file holds `fromW fromH toW toH`.
  - `Restore` skips a state whose "to" isn't the current size. It still restores when the current size can't be read, and it still reads the old two-number file.
  - `RestoreAlways` is for openFB's "asked for X, got Y" path.
- **Logs:**
  - A failed rotation retries after every further cap's worth of bytes.
  - When the file can't be reopened, lines go to stderr.
- **Launcher:**
  - The app runs in the background, and INT and TERM are both forwarded to it as TERM.
  - `restore` ignores signals.
  - The tests start the launcher through python3, so that SIGINT isn't ignored.
- **Server folders:** a folder is `<safe name>-<8 hex digits of sha1(name)>`. An existing old folder (`<safe name>`) is kept, so an upgrade loses nothing.
- **The wizard** ignores B while it saves: backing out after the old config was moved aside left no config at all.
- **Config save:**
  - Save is a line-based edit of the values that changed.
  - Server tables are matched by name. A renamed server is removed and added.
  - Reordered servers, and layouts the editor doesn't know, fall back to a full rewrite.
- **Covers:**
  - The box filter divides with exact reciprocals: the A9 has no hardware divide.
  - Opaque sources (every JPEG) use 32-bit sums.
  - The output is bit-identical to before.
  - On the MiSTer, decode+scale went from 1.98 s to 1.38 s, and scale alone from 1.16 s to 0.61 s. What remains is Go's JPEG decoder.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/ui/screens_settings.go`, `screensaver.go`, `app.go`, `screenshot.go` | Settings and screensaver input | 1 |
| `internal/ui/grid.go`, `hints.go`, `hints_screens.go`, `menus.go`, `screens_play.go`, `screens_playlists.go`, `screens_search.go`, `screenshot.go`, `tabbed.go`, `wizard.go`, README, `config.example.toml` | small UI bugs and texts | 2 |
| `internal/audio/engine.go`, `internal/player/mp3.go`, `player.go`, `stream.go`, `internal/stream/reader.go` | MP3 seeks | 3 |
| `internal/stream/reader.go`, `internal/player/stream.go` | stream and player hardening | 4 |
| `internal/platform/fbmode.go`, `cmd/mistersubsonic/fullres.go`, `main.go`, `internal/input/evdev_linux.go` | full resolution and input | 5 |
| `internal/logfile/logfile.go`, `sdcard/Scripts/MiSTer_Subsonic.sh`, `scripts/test-launcher.sh`, `scripts/check-notices.sh`, `tools/mkdb/main.go`, Makefile | logs, launcher, tooling | 6 |
| `cmd/mistersubsonic/session.go`, `internal/cache/cache.go`, `internal/art/art.go`, `internal/ui/app.go`, `screens_starred.go`, `wizard.go` | per-server data, cache, art | 7 |
| `internal/config/edit.go`, `config.go`, `internal/gfx/image.go` | config edit in place, faster covers | 8 |
| README, `docs/spikes.md`, `docs/testing-on-mister.md`, followups, backlog | results and TV checks | 9 |

---

### Task 1: Settings and screensaver input

**Files:**
- Modify: `internal/ui/app.go`, `internal/ui/screens_settings.go`, `internal/ui/screensaver.go`, `internal/ui/screenshot.go`
- Test: `internal/ui/settings_test.go`, `internal/ui/screensaver_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **On/Off rows.** `setting.toggle` marks On/Off rows (Scrobbling, Hints). `SettingsListScreen.Handle` takes a Left or Right repeat on them without changing anything. Rows with several choices still repeat.
  - **Hand-edited numbers.** `cycleNear(choices []int, cur, dir int) int` steps from a value that isn't in the list to the nearest choice in the direction `dir`. Past either end it wraps: 500 → Right → 128, and 50 → Left → 320. The bitrate and screensaver rows use it.
  - **Toasts.**
    - `App.Toast` wakes the screensaver, and resets the idle clock so it doesn't start again at once.
    - "Screenshot saved" is not shown while the screensaver is on, so a screenshot still doesn't wake it. "Screenshot failed" does wake it.
  - **The exit prompt.** `saverDue` returns zero while the exit prompt (`a.confirm`) shows.
  - **The waking press.** `App.wakeKeys` records the button that woke the screensaver, and `onInput` drops that button's Release before any screen or Release hook sees it.
    - The Now Playing case already passed before the fix: the swallowed press never set `selDown`.
    - The spy-screen test (`TestScreensaverWakeReleaseReachesNoScreenHook`) is the one that fails without the fix.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t1-test.patch` and apply it from the repository root with `git apply /tmp/t1-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-5 branch)):

```diff
diff --git a/internal/ui/screensaver_test.go b/internal/ui/screensaver_test.go
index 1227642..ca783e4 100644
--- a/internal/ui/screensaver_test.go
+++ b/internal/ui/screensaver_test.go
@@ -5,7 +5,9 @@ import (
 	"time"
 
 	"mistersubsonic/internal/config"
+	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/player"
 )
 
 func saverApp(t *testing.T, p Profile, minutes int) *testApp {
@@ -93,3 +95,70 @@ func TestGoldenScreensaver(t *testing.T) {
 		golden(t, "screensaver-"+p.Name, ta.settle(t))
 	}
 }
+
+func TestToastWakesTheScreensaver(t *testing.T) {
+	ta := saverApp(t, ProfileHDMI, 1)
+	ta.now = ta.now.Add(time.Minute)
+	ta.onWake()
+	if !ta.saver {
+		t.Fatal("screensaver not on")
+	}
+	ta.Toast("Can't play x: offline")
+	if ta.saver {
+		t.Fatal("a toast left the screensaver on: it can't be seen")
+	}
+	ta.onWake()
+	if ta.saver {
+		t.Fatal("the screensaver came straight back over the toast")
+	}
+}
+
+func TestScreensaverNeverStartsOverTheExitPrompt(t *testing.T) {
+	ta := saverApp(t, ProfileHDMI, 1)
+	ta.confirm = true
+	ta.now = ta.now.Add(2 * time.Minute)
+	ta.onWake()
+	if ta.saver || !ta.saverDue().IsZero() {
+		t.Fatalf("screensaver over the exit prompt: saver %v, due %v", ta.saver, ta.saverDue())
+	}
+}
+
+func TestScreensaverWakeSwallowsTheReleaseToo(t *testing.T) {
+	ta := saverApp(t, ProfileHDMI, 1)
+	ta.now = ta.now.Add(time.Minute)
+	ta.onWake()
+	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Press}) // wakes
+	ta.onInput(input.Event{Button: input.BtnSelect, Kind: input.Release})
+	if len(ta.toasts) != 0 || ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatOff {
+		t.Fatalf("the wake press's release cycled the mode: toasts %v, %+v", ta.toasts, ta.pl.st)
+	}
+	ta.press(input.BtnSelect) // a real tap still cycles
+	if !ta.pl.st.Shuffle {
+		t.Fatal("the next tap didn't cycle the mode")
+	}
+}
+
+func TestScreensaverWakeReleaseReachesNoScreenHook(t *testing.T) {
+	ta := saverApp(t, ProfileHDMI, 1)
+	rel := &releaseSpy{}
+	ta.Push(rel)
+	ta.lastInput = ta.now
+	ta.saver = true
+	ta.onInput(input.Event{Button: input.BtnA, Kind: input.Press})
+	ta.onInput(input.Event{Button: input.BtnA, Kind: input.Release})
+	if rel.n != 0 {
+		t.Fatalf("Release hook called %d times for the wake press", rel.n)
+	}
+	ta.press(input.BtnA)
+	if rel.n != 1 {
+		t.Fatalf("Release hook called %d times for a normal press, want 1", rel.n)
+	}
+}
+
+type releaseSpy struct{ n int }
+
+func (s *releaseSpy) Title() string                    { return "spy" }
+func (s *releaseSpy) Enter(*App)                       {}
+func (s *releaseSpy) Handle(*App, input.Event) bool    { return true }
+func (s *releaseSpy) Draw(*App, *gfx.Canvas, gfx.Rect) {}
+func (s *releaseSpy) Release(*App, input.Button)       { s.n++ }
diff --git a/internal/ui/settings_test.go b/internal/ui/settings_test.go
index 42109f3..4f82b61 100644
--- a/internal/ui/settings_test.go
+++ b/internal/ui/settings_test.go
@@ -313,3 +313,74 @@ func TestSettingsHelpFitsTwoLines(t *testing.T) {
 		}
 	}
 }
+
+func TestHeldKeyFlipsAnOnOffRowOnce(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.Push(newSettingsList("Playback", playbackSettings))
+	ta.press(input.BtnDown) // Scrobbling (on)
+	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
+	for range 5 {
+		ta.dispatch(input.Event{Button: input.BtnRight, Kind: input.Repeat})
+	}
+	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Release})
+	if ta.cfg.Playback.Scrobble {
+		t.Fatal("holding Right flipped Scrobbling more than once (it is still on)")
+	}
+	if n := len(ta.pl.calls); n != 1 {
+		t.Fatalf("player calls %v, want one scrobble change", ta.pl.calls)
+	}
+}
+
+func TestHeldKeyKeepsCyclingAChoiceRow(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.Push(newSettingsList("Playback", playbackSettings))
+	ta.onInput(input.Event{Button: input.BtnRight, Kind: input.Press})
+	ta.dispatch(input.Event{Button: input.BtnRight, Kind: input.Repeat})
+	if got := ta.cfg.Playback.ReplayGain; got != "album" {
+		t.Fatalf("ReplayGain %q after Press+Repeat, want album", got)
+	}
+}
+
+func TestCycleFromAHandEditedValue(t *testing.T) {
+	bitrates := []int{128, 192, 256, 320}
+	for _, c := range []struct{ cur, dir, want int }{
+		{160, 1, 192}, {160, -1, 128},
+		{500, 1, 128}, {500, -1, 320}, // beyond the ends: wraps like the ends do
+		{50, 1, 128}, {50, -1, 320},
+		{192, 1, 256}, {128, -1, 320}, // listed values are unchanged
+	} {
+		if got := cycleNear(bitrates, c.cur, c.dir); got != c.want {
+			t.Errorf("cycleNear(%d, %+d) = %d, want %d", c.cur, c.dir, got, c.want)
+		}
+	}
+}
+
+func TestSettingsRowsStartFromTheNearestChoice(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.cfg.Playback.TranscodeFormat = "mp3"
+	ta.cfg.Playback.TranscodeBitrate = 160
+	ta.Push(newSettingsList("Playback", playbackSettings))
+	ta.press(input.BtnDown)
+	ta.press(input.BtnDown)
+	ta.press(input.BtnDown) // bitrate
+	ta.press(input.BtnRight)
+	if got := ta.cfg.Playback.TranscodeBitrate; got != 192 {
+		t.Fatalf("160 -> Right = %d, want 192", got)
+	}
+	ta.cfg.Playback.TranscodeBitrate = 160
+	ta.press(input.BtnLeft)
+	if got := ta.cfg.Playback.TranscodeBitrate; got != 128 {
+		t.Fatalf("160 -> Left = %d, want 128", got)
+	}
+}
+
+func TestScreensaverRowStartsFromTheNearestChoice(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.cfg.Display.ScreensaverMinutes = 7
+	ta.Push(newSettingsList("Display", displaySettings))
+	ta.press(input.BtnDown)
+	ta.press(input.BtnRight)
+	if got := ta.cfg.Display.ScreensaverMinutes; got != 10 {
+		t.Fatalf("7 -> Right = %d, want 10", got)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
undefined: cycleNear
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-5 branch)):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 73d5c47..70147b1 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -183,6 +183,7 @@ type App struct {
 	// swallowed holds buttons whose press was typed into a text field, so
 	// their releases are dropped too.
 	swallowed  map[input.Button]bool
+	wakeKeys   map[input.Button]bool // buttons whose press woke the screensaver: their release is ignored
 	insecure   bool
 	stars      map[starKey]bool       // star changes made in this session
 	starBusy   map[starKey]bool       // star requests in flight
@@ -230,7 +231,7 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
+		swallowed: map[input.Button]bool{}, wakeKeys: map[input.Button]bool{}, stars: map[starKey]bool{}, starBusy: map[starKey]bool{}, cfg: o.Config, lastInput: o.Now()}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -410,6 +411,9 @@ func (a *App) After(owner Screen, d time.Duration, f func()) {
 // Toast shows a short message over the current screen.
 func (a *App) Toast(format string, args ...any) {
 	text := fmt.Sprintf(format, args...)
+	if a.wake() { // a toast can't be seen over the screensaver (a failed track must be)
+		a.lastInput = a.o.Now() // else it would start again at once
+	}
 	until := a.o.Now().Add(toastTime)
 	a.Damage(a.toastsArea()) // the toasts move up for the new one
 	if n := len(a.toasts); n > 0 && a.toasts[n-1].text == text {
@@ -585,8 +589,13 @@ func (a *App) onInput(e input.Event) {
 	now := a.o.Now()
 	a.lastInput = now
 	if a.wake() && e.Kind == input.Press && !isMediaButton(e.Button) {
+		a.wakeKeys[e.Button] = true
 		return // the press that wakes the screensaver does nothing else (a media key still acts)
 	}
+	if e.Kind == input.Release && a.wakeKeys[e.Button] {
+		delete(a.wakeKeys, e.Button)
+		return // ...and neither does its release
+	}
 	if e.Rune != 0 && !a.confirm {
 		if e.Kind == input.Press {
 			if t, ok := a.Top().(TextInput); ok && t.Text(a, e.Rune) {
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index d3b4597..f6a4c6d 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -3,6 +3,7 @@ package ui
 import (
 	"fmt"
 	"math"
+	"slices"
 	"strings"
 
 	"mistersubsonic/internal/config"
@@ -53,12 +54,14 @@ func (s *SettingsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 }
 
 // setting is one row of a settings list: Left/Right (or A, forwards)
-// change it.
+// change it. A held key keeps cycling the choices, but an On/Off row
+// (toggle) changes once per press.
 type setting struct {
 	label  string
 	value  func(a *App) string
 	change func(a *App, dir int)
 	help   string // what the setting does, shown under the list while it is focused
+	toggle bool   // On/Off: ignores key repeat
 }
 
 // SettingsListScreen is a list of settings with their values.
@@ -88,6 +91,9 @@ func (s *SettingsListScreen) Handle(a *App, e input.Event) bool {
 		return false
 	}
 	r := rows[min(s.list.Focus, len(rows)-1)]
+	if r.toggle && e.Kind != input.Press {
+		return e.Button == input.BtnLeft || e.Button == input.BtnRight // a held key must not flicker it
+	}
 	switch {
 	case e.Button == input.BtnLeft:
 		r.change(a, -1)
@@ -152,6 +158,29 @@ func cycle[T comparable](choices []T, cur T, dir int) T {
 	return choices[(i+dir+len(choices))%len(choices)]
 }
 
+// cycleNear is cycle for ascending numbers, where cur may be a hand-edited
+// value that isn't a choice: the first step goes to the nearest choice in
+// that direction, wrapping when there is none (160 → 192 or 128).
+func cycleNear(choices []int, cur, dir int) int {
+	if slices.Contains(choices, cur) {
+		return cycle(choices, cur, dir)
+	}
+	if dir > 0 {
+		for _, c := range choices {
+			if c > cur {
+				return c
+			}
+		}
+		return choices[0]
+	}
+	for i := len(choices) - 1; i >= 0; i-- {
+		if choices[i] < cur {
+			return choices[i]
+		}
+	}
+	return choices[len(choices)-1]
+}
+
 func onOff(b bool) string {
 	if b {
 		return "On"
@@ -220,7 +249,7 @@ func playbackSettings(a *App) []setting {
 				}
 				a.UpdateConfig(func(c *config.Config) { c.Playback.ReplayGain = mode }, false)
 			},
-			"Evens out loudness: track levels each song, album keeps an album's own dynamics. Off plays files as they are."},
+			"Evens out loudness: track levels each song, album keeps an album's own dynamics. Off plays files as they are.", false},
 		{"Scrobbling", func(a *App) string { return onOff(pb().Scrobble) },
 			func(a *App, dir int) {
 				on := !pb().Scrobble
@@ -229,24 +258,24 @@ func playbackSettings(a *App) []setting {
 				}
 				a.UpdateConfig(func(c *config.Config) { c.Playback.Scrobble = on }, false)
 			},
-			"Tells the server (and Last.fm, if the server is set up for it) what you listen to."},
+			"Tells the server (and Last.fm, if the server is set up for it) what you listen to.", true},
 		{"Transcode to", func(a *App) string { return pb().TranscodeFormat },
 			func(a *App, dir int) {
 				f := cycle([]string{"mp3", "flac", "wav"}, pb().TranscodeFormat, dir)
 				a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeFormat = f }, false)
 				a.Toast("Used from the next connection")
 			},
-			"The server converts files the MiSTer can't play (AAC, OGG, Opus…) to this. FLAC, MP3 and WAV play as they are."},
+			"The server converts files the MiSTer can't play (AAC, OGG, Opus…) to this. FLAC, MP3 and WAV play as they are.", false},
 	}
 	if pb().TranscodeFormat == "mp3" { // the bitrate only matters for mp3
 		rows = append(rows,
 			setting{"Transcode bitrate", func(a *App) string { return fmt.Sprintf("%d kbps", pb().TranscodeBitrate) },
 				func(a *App, dir int) {
-					b := cycle([]int{128, 192, 256, 320}, pb().TranscodeBitrate, dir)
+					b := cycleNear([]int{128, 192, 256, 320}, pb().TranscodeBitrate, dir)
 					a.UpdateConfig(func(c *config.Config) { c.Playback.TranscodeBitrate = b }, false)
 					a.Toast("Used from the next connection")
 				},
-				"Quality of those conversions to MP3: higher sounds better and uses more network."})
+				"Quality of those conversions to MP3: higher sounds better and uses more network.", false})
 	}
 	return rows
 }
@@ -267,7 +296,7 @@ func displaySettings(a *App) []setting {
 				a.UpdateConfig(func(c *config.Config) { c.Display.Profile = p }, false)
 				a.Toast("The layout changes the next time the app starts")
 			},
-			"HDMI or CRT screen layout; auto picks by the screen's lines. Takes effect the next time the app starts."},
+			"HDMI or CRT screen layout; auto picks by the screen's lines. Takes effect the next time the app starts.", false},
 		{"Screensaver", func(a *App) string {
 			if m := d().ScreensaverMinutes; m > 0 {
 				return fmt.Sprintf("after %d min", m)
@@ -275,16 +304,16 @@ func displaySettings(a *App) []setting {
 			return "Off"
 		},
 			func(a *App, dir int) {
-				m := cycle(screensaverChoices, d().ScreensaverMinutes, dir)
+				m := cycleNear(screensaverChoices, d().ScreensaverMinutes, dir)
 				a.UpdateConfig(func(c *config.Config) { c.Display.ScreensaverMinutes = m }, false)
 			},
-			"Dims Now Playing and drifts the cover after this long without input."},
+			"Dims Now Playing and drifts the cover after this long without input.", false},
 		{"Hints", func(a *App) string { return onOff(d().Hints) },
 			func(a *App, dir int) {
 				on := !d().Hints
 				a.UpdateConfig(func(c *config.Config) { c.Display.Hints = on }, false)
 			},
-			"The bar of buttons along the bottom of every screen."},
+			"The bar of buttons along the bottom of every screen.", true},
 	}
 }
 
diff --git a/internal/ui/screensaver.go b/internal/ui/screensaver.go
index c6a1841..aeed7ad 100644
--- a/internal/ui/screensaver.go
+++ b/internal/ui/screensaver.go
@@ -24,10 +24,11 @@ func (a *App) saverMinutes() int {
 	return a.cfg.Display.ScreensaverMinutes
 }
 
-// saverDue is when the screensaver starts, or zero if it can't now.
+// saverDue is when the screensaver starts, or zero if it can't now (not on
+// Now Playing, off, on, or the exit prompt is up).
 func (a *App) saverDue() time.Time {
 	m := a.saverMinutes()
-	if _, np := a.Top().(*NowPlayingScreen); !np || m <= 0 || a.saver {
+	if _, np := a.Top().(*NowPlayingScreen); !np || m <= 0 || a.saver || a.confirm {
 		return time.Time{}
 	}
 	return a.lastInput.Add(time.Duration(m) * time.Minute)
diff --git a/internal/ui/screenshot.go b/internal/ui/screenshot.go
index 09434e5..3574659 100644
--- a/internal/ui/screenshot.go
+++ b/internal/ui/screenshot.go
@@ -35,7 +35,9 @@ func (a *App) screenshot() {
 				return
 			}
 			log.Printf("screenshot: saved %s", path)
-			a.Toast("Screenshot saved")
+			if !a.saver { // success is silent over the screensaver: a toast would wake it
+				a.Toast("Screenshot saved")
+			}
 		})
 	}()
 }
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/screens_settings.go internal/ui/screensaver.go internal/ui/screensaver_test.go internal/ui/screenshot.go internal/ui/settings_test.go
git commit -m "ui: On/Off rows ignore repeat, hand-edited values step to the nearest choice, toasts wake the screensaver" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Small UI bugs and misleading text

**Files:**
- Modify: `internal/ui/grid.go`, `internal/ui/hints.go`, `internal/ui/hints_screens.go`, `internal/ui/menus.go`, `internal/ui/screens_play.go`, `internal/ui/screens_playlists.go`, `internal/ui/screens_search.go`, `internal/ui/screenshot.go`, `internal/ui/tabbed.go`, `internal/ui/wizard.go`, `README.md`, `sdcard/mistersubsonic/config.example.toml`
- Test: `internal/ui/browse_test.go`, `fakes_test.go`, `hints_test.go`, `lists_test.go`, `play_test.go`, `screenshot_test.go`, `search_test.go`, `widgets_test.go`, `wizard_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **Queue menu.** The X menu on the queue is titled "Queue", with "Remove <song>" and "Clear queue".
  - **Grid R.** `(*Grid).pageDown(f, n)`: past the end, R lands in the last row in the same column when that cell exists, otherwise on the last item.
  - **Album loads.** `albumsSongs` returns the songs it got, plus a `*partialLoad{failed, total}` error. `withSongs` toasts "Couldn't load N of M albums" and still plays. When every album fails, the old single error toast is kept. The fake library gets `failAlbum`.
  - **Hints.**
    - `Hint` gains `Key` and `Rune`, for keys that exist only on a keyboard.
    - `typing(a, enter, text)`:
      - the gamepad set is unchanged (A Type, X Delete);
      - the keyboard set shows "Backspace Delete" only with text, and `enter` (the wizard's "Next") when it isn't empty.
    - `withBack(hs)` puts Back after the first hint.
    - The wizard hints Back after its first step. The root hints Back while the content is focused, unless the section uses B itself.
    - `TestEveryHintedButtonDoesSomething` presses `Rune` hints as real keys (Enter is A with `'\n'`, Backspace is B with `'\b'`).
  - **URLs.** `normalizeURL` drops `?query` and `#fragment` and keeps the path.
  - **Screenshots.** A press while a save runs toasts "Still saving the last screenshot".
  - **Texts:**
    - The README's screenshot paragraph (Scroll Lock alone, the MiSTer folder, `screenshots/` elsewhere, `-screenshots`), and the keyboard typing line.
    - `config.example.toml`: the header says to change url, username and password first. The `allow_plaintext_password` comment is clearer.
  - **The tab row.** While the tabs have the focus, the child is drawn dimmed, so its long titles stop scrolling. Search results do the same.
  - **CRT playlist page.** On the list layout (`SideW == 0`), every row is `RowH` and a track is one line, "Title — Artist", with no thumbnail.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/ui/browse_test.go b/internal/ui/browse_test.go
index 725b75b..45dbe4a 100644
--- a/internal/ui/browse_test.go
+++ b/internal/ui/browse_test.go
@@ -321,3 +321,40 @@ func TestAlbumListRetriesFailedPage(t *testing.T) {
 		t.Fatalf("after retry %d albums, want 150", len(s.view.albums))
 	}
 }
+
+// A shuffle of several albums plays what loaded and says how many didn't.
+func TestShuffleOfAlbumsToastsFailedLoads(t *testing.T) {
+	ta := newTestApp(t, ProfileHDMI)
+	ta.lib.tracks["al-2"] = ta.lib.tracks["al-1"][:1]
+	ta.lib.failAlbum = map[subsonic.ID]bool{"al-3": true}
+	ta.Push(NewHomeScreen())
+	ta.Push(NewAlbumListScreen("Recently added", "newest"))
+	ta.settle(t)
+	ta.press(input.BtnSelect)
+	ta.settle(t)
+	if len(ta.pl.played) != 4 {
+		t.Fatalf("played %d songs, want the 4 of the two albums that loaded", len(ta.pl.played))
+	}
+	if got := lastToast(ta); got != "Couldn't load 1 of 3 albums" {
+		t.Fatalf("toast %q", got)
+	}
+	// All of them failing is still the one error.
+	ta.lib.failAlbum = map[subsonic.ID]bool{"al-1": true, "al-2": true, "al-3": true}
+	ta.pl.played = nil
+	ta.press(input.BtnB)
+	ta.Push(NewAlbumListScreen("Recently added", "newest"))
+	ta.settle(t)
+	ta.press(input.BtnSelect)
+	ta.settle(t)
+	if len(ta.pl.played) != 0 || !strings.HasPrefix(lastToast(ta), "Couldn't load: ") {
+		t.Fatalf("played %d, toast %q", len(ta.pl.played), lastToast(ta))
+	}
+}
+
+// lastToast is the newest toast's text, "" when there is none.
+func lastToast(ta *testApp) string {
+	if len(ta.toasts) == 0 {
+		return ""
+	}
+	return ta.toasts[len(ta.toasts)-1].text
+}
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 3264ed3..0ff0d2f 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -26,6 +26,7 @@ type fakeLibrary struct {
 	plSongs   map[subsonic.ID][]subsonic.Song
 	starred   subsonic.Starred
 	err       error
+	failAlbum map[subsonic.ID]bool // GetAlbum fails for these
 	calls     []subsonic.AlbumListQuery
 	searches  []string
 	stars     []string      // "star al-1", "unstar s2"
@@ -53,6 +54,9 @@ func (l *fakeLibrary) GetAlbum(_ context.Context, id subsonic.ID) (*subsonic.Alb
 	if l.err != nil {
 		return nil, l.err
 	}
+	if l.failAlbum[id] {
+		return nil, errOffline
+	}
 	for _, al := range l.albums {
 		if al.ID == id {
 			return &subsonic.AlbumWithSongs{Album: al, Songs: l.tracks[id]}, nil
diff --git a/internal/ui/hints_test.go b/internal/ui/hints_test.go
index 4bdbccd..cae7967 100644
--- a/internal/ui/hints_test.go
+++ b/internal/ui/hints_test.go
@@ -3,6 +3,7 @@ package ui
 import (
 	"errors"
 	"fmt"
+	"slices"
 	"strings"
 	"testing"
 
@@ -33,6 +34,29 @@ var hintFixtures = map[string]func(ta *testApp) Screen{
 	"display":     func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
 	"servers":     func(ta *testApp) Screen { return NewServersScreen() },
 	"wizard":      func(ta *testApp) Screen { return NewWizardScreen(false, false) },
+	"wizard with text": func(ta *testApp) Screen {
+		w := NewWizardScreen(false, false)
+		w.fields[stepURL] = []rune("http://h:4533")
+		return w
+	},
+	"wizard second step": func(ta *testApp) Screen {
+		w := NewWizardScreen(true, false)
+		w.setStep(stepUser)
+		return w
+	},
+	"search with text": func(ta *testApp) Screen {
+		s := NewSearchScreen()
+		s.query = []rune("ab")
+		return s
+	},
+	"root content": func(ta *testApp) Screen {
+		r := NewRootScreen(ta.P)
+		if sr, ok := r.(*SidebarRoot); ok {
+			sr.open(ta.App)
+			sr.inSidebar = false
+		}
+		return r
+	},
 	"menu with a queue": func(ta *testApp) Screen {
 		playingState(ta)
 		return NewMenuScreen(ta.Top(), "Menu", []menuEntry{{"One", func(a *App) {}}, {"Two", func(a *App) {}}})
@@ -84,7 +108,11 @@ func TestEveryHintedButtonDoesSomething(t *testing.T) {
 					}
 					before := ta.settle(t).ToRGBA()
 					depth, calls, top := len(ta.stack), len(ta.pl.calls), ta.Top()
-					ta.press(b)
+					if h.Rune != 0 { // a typing key: Backspace, Enter
+						ta.pressKey(h.Rune)
+					} else {
+						ta.press(b)
+					}
 					after := ta.settle(t).ToRGBA()
 					if samePixels(before, after) && len(ta.stack) == depth && len(ta.pl.calls) == calls && ta.Top() == top {
 						t.Errorf("%s on %s: %v (%q) does nothing", name, p.Name, b, h.Label)
@@ -292,3 +320,92 @@ func TestTypingScreensHintNowPlayingOnlyForThePad(t *testing.T) {
 		t.Errorf("Search on a keyboard lost Back: %s", hintLabels(ta.screenHints()))
 	}
 }
+
+// pressKey is a keyboard key that types r: Enter and Backspace carry their
+// button too.
+func (ta *testApp) pressKey(r rune) {
+	b := input.BtnNone
+	switch r {
+	case '\n':
+		b = input.BtnA
+	case '\b':
+		b = input.BtnB
+	}
+	ta.onInput(input.Event{Button: b, Kind: input.Press, Rune: r})
+	ta.onInput(input.Event{Button: b, Kind: input.Release, Rune: r})
+}
+
+// On a keyboard letters type and Backspace deletes, so that is what a
+// typing screen hints (Enter only where it submits); a gamepad still types
+// with A and deletes with X.
+func TestTypingHintsFollowTheInput(t *testing.T) {
+	type key struct{ cap, label string }
+	caps := func(ta *testApp) []key {
+		var out []key
+		for _, h := range ta.screenHints() {
+			c, _ := ta.capFor(h.Button)
+			if h.Key != "" {
+				c = h.Key
+			}
+			out = append(out, key{c, h.Label})
+		}
+		return out
+	}
+	for _, c := range []struct {
+		screen string
+		pad    bool
+		want   []key
+	}{
+		{"wizard", false, []key{{"Enter", "Next"}, {"Esc", "Back"}}},
+		{"wizard with text", false, []key{{"Enter", "Next"}, {"Esc", "Back"}, {"Backspace", "Delete"}}},
+		{"wizard with text", true, []key{{"A", "Type"}, {"B", "Back"}, {"X", "Delete"}}},
+		{"search", false, []key{{"Esc", "Back"}}},
+		{"search with text", false, []key{{"Backspace", "Delete"}, {"Esc", "Back"}}},
+		{"search with text", true, []key{{"A", "Type"}, {"B", "Back"}, {"X", "Delete"}}},
+	} {
+		ta := hintApp(t, ProfileHDMI, hintFixtures[c.screen])
+		ta.pad = c.pad
+		if got := caps(ta); !slices.Equal(got, c.want) {
+			t.Errorf("%s, pad=%v: hints %v, want %v", c.screen, c.pad, got, c.want)
+		}
+	}
+}
+
+// B is hinted where it does something of its own: back a step in the
+// wizard, out of a section to the sidebar (both are the app's first screen,
+// where the app adds no Back).
+func TestBackIsHintedWhereBReturns(t *testing.T) {
+	first := func(s Screen) *testApp {
+		ta := newTestApp(t, ProfileHDMI)
+		ta.cfg = config.Default()
+		ta.pad = true
+		ta.Push(s)
+		ta.settle(t)
+		return ta
+	}
+	w := NewWizardScreen(true, false)
+	if got := hintLabels(first(w).screenHints()); strings.Contains(got, "Back") {
+		t.Errorf("first-run wizard, first step: %s", got) // B has nowhere to go
+	}
+	w = NewWizardScreen(true, false)
+	w.setStep(stepUser)
+	if got := hintLabels(first(w).screenHints()); !strings.Contains(got, "Back") {
+		t.Errorf("first-run wizard, second step: %s", got)
+	}
+	w.setStep(stepTest)
+	if got := hintLabels(first(w).screenHints()); !strings.Contains(got, "Back") {
+		t.Errorf("first-run wizard, test step: %s", got)
+	}
+	root := newSidebarRoot() // starts with the content focused
+	ta := first(root)
+	if got := hintLabels(ta.screenHints()); !strings.Contains(got, "Back") {
+		t.Errorf("root, content focused: %s", got)
+	}
+	ta.press(input.BtnB)
+	if !root.inSidebar {
+		t.Error("B in the content didn't go to the sidebar")
+	}
+	if got := hintLabels(ta.screenHints()); strings.Contains(got, "Back") {
+		t.Errorf("root, sidebar focused: %s", got)
+	}
+}
diff --git a/internal/ui/lists_test.go b/internal/ui/lists_test.go
index abd7c06..bbb1a16 100644
--- a/internal/ui/lists_test.go
+++ b/internal/ui/lists_test.go
@@ -3,6 +3,7 @@ package ui
 import (
 	"slices"
 	"testing"
+	"time"
 
 	"mistersubsonic/internal/input"
 )
@@ -96,3 +97,53 @@ func TestGoldenPlaylistsAndStarred(t *testing.T) {
 		golden(t, "starred-tracks-"+p.Name, ta.settle(t))
 	}
 }
+
+// Only the focused thing scrolls a long title: with the focus on the tab
+// row (or, in Search, on the keyboard or the result tabs) the list below
+// keeps its titles cut.
+func TestMarqueeStopsWhileTheTabsHaveTheFocus(t *testing.T) {
+	scrolls := func(ta *testApp) bool {
+		ta.settle(t)
+		ta.now = ta.now.Add(marqueeDelay + time.Second)
+		ta.settle(t)
+		return ta.animate
+	}
+	for _, p := range profiles {
+		ta := newTestApp(t, p)
+		ta.Push(NewHomeScreen())
+		s := NewAlbumsScreen()
+		ta.Push(s)
+		ta.settle(t)
+		for range 2 { // the long title is the third album, in a grid or a list
+			ta.press(input.BtnDown)
+			ta.press(input.BtnRight)
+		}
+		if !scrolls(ta) {
+			t.Fatalf("%s: the focused long title doesn't scroll (test setup)", p.Name)
+		}
+		for range 5 {
+			if !s.onTabs {
+				ta.press(input.BtnUp)
+			}
+		}
+		if !s.onTabs {
+			t.Fatalf("%s: the focus didn't reach the tabs", p.Name)
+		}
+		if scrolls(ta) {
+			t.Errorf("%s: a title scrolls while the tabs have the focus", p.Name)
+		}
+	}
+}
+
+// On a CRT the playlist page keeps room for more than one track under its
+// Play and Shuffle rows (they used to take two-line rows too).
+func TestPlaylistPageShowsSeveralTracksOnACRT(t *testing.T) {
+	ta := newTestApp(t, ProfileCRT240)
+	ta.Push(NewHomeScreen())
+	s := NewPlaylistScreen(ta.lib.playlists[0])
+	ta.Push(s)
+	ta.settle(t)
+	if want := playlistActionRows + 3; s.list.rows < want {
+		t.Fatalf("%d rows fit, want %d (Play, Shuffle and three tracks)", s.list.rows, want)
+	}
+}
diff --git a/internal/ui/play_test.go b/internal/ui/play_test.go
index 610d736..e62368a 100644
--- a/internal/ui/play_test.go
+++ b/internal/ui/play_test.go
@@ -55,6 +55,10 @@ func TestQueueMenuRemoveAndClear(t *testing.T) {
 	if _, ok := ta.Top().(*MenuScreen); !ok {
 		t.Fatalf("X opened %T, want the menu", ta.Top())
 	}
+	menu := ta.Top().(*MenuScreen)
+	if menu.title != "Queue" || menu.entries[0].label != "Remove Время Луны" || menu.entries[1].label != "Clear queue" {
+		t.Fatalf("menu %q with entries %q, %q", menu.title, menu.entries[0].label, menu.entries[1].label)
+	}
 	ta.press(input.BtnA) // Remove
 	if !slices.Equal(ta.pl.calls, []string{"remove"}) || ta.toasts[0].text != "Removed Время Луны" {
 		t.Fatalf("calls %v toasts %v", ta.pl.calls, ta.toasts)
diff --git a/internal/ui/screenshot_test.go b/internal/ui/screenshot_test.go
index bd26a97..7cf23e3 100644
--- a/internal/ui/screenshot_test.go
+++ b/internal/ui/screenshot_test.go
@@ -112,3 +112,23 @@ func TestScreenshotDoesNotWakeTheScreensaver(t *testing.T) {
 		t.Fatalf("saver %v, last input moved %v", ta.saver, ta.lastInput.Sub(idle))
 	}
 }
+
+// A press while a save is still running is refused with a toast, not lost
+// silently.
+func TestScreenshotWhileSavingSaysSo(t *testing.T) {
+	ta := newTestApp(t, ProfileHDMI)
+	ta.o.ScreenshotDir = t.TempDir()
+	ta.Push(NewHomeScreen())
+	ta.settle(t)
+	ta.press(input.BtnScreenshot)
+	ta.press(input.BtnScreenshot) // the first save hasn't reported back yet
+	if got := lastToast(ta); got != "Still saving the last screenshot" {
+		t.Fatalf("toast %q", got)
+	}
+	for ta.shooting {
+		(<-ta.post)()
+	}
+	if entries, _ := os.ReadDir(ta.o.ScreenshotDir); len(entries) != 1 {
+		t.Fatalf("%d files, want the one screenshot", len(entries))
+	}
+}
diff --git a/internal/ui/search_test.go b/internal/ui/search_test.go
index 00928f8..a4c73d6 100644
--- a/internal/ui/search_test.go
+++ b/internal/ui/search_test.go
@@ -353,3 +353,36 @@ func TestSearchEditClearsError(t *testing.T) {
 		t.Fatalf("error %v survived emptying the field", s.err)
 	}
 }
+
+// The result tabs having the focus stops the long title below from scrolling.
+func TestSearchResultTitleStopsScrollingOnTheTabs(t *testing.T) {
+	for _, p := range profiles {
+		ta := newTestApp(t, p)
+		ta.Push(NewHomeScreen())
+		s := NewSearchScreen()
+		ta.Push(s)
+		typeKeys(ta, "rather")
+		ta.now = ta.now.Add(searchDelay)
+		ta.onWake()
+		ta.settle(t)
+		for i := 0; !s.inResults && i < 11; i++ { // down the keyboard and past its bottom row: the long album
+			ta.press(input.BtnDown)
+		}
+		scrolls := func() bool {
+			ta.settle(t)
+			ta.now = ta.now.Add(marqueeDelay + time.Second)
+			ta.settle(t)
+			return ta.animate
+		}
+		if !s.inResults || !scrolls() {
+			t.Fatalf("%s: in results %v, scrolling %v (test setup)", p.Name, s.inResults, ta.animate)
+		}
+		ta.press(input.BtnUp)
+		if !s.onTabs {
+			t.Fatalf("%s: Up didn't reach the result tabs", p.Name)
+		}
+		if scrolls() {
+			t.Errorf("%s: a title scrolls while the result tabs have the focus", p.Name)
+		}
+	}
+}
diff --git a/internal/ui/widgets_test.go b/internal/ui/widgets_test.go
index 636fd27..7ce1c82 100644
--- a/internal/ui/widgets_test.go
+++ b/internal/ui/widgets_test.go
@@ -47,6 +47,26 @@ func TestGridNavigation(t *testing.T) {
 	}
 }
 
+// R on a short last row keeps the column when the last row has it.
+func TestGridPageDownKeepsTheColumn(t *testing.T) {
+	var g Grid
+	c := gfx.NewCanvas(300, 200)
+	g.Draw(c, c.Bounds(), 9, 100, 100, func(int, gfx.Rect, bool) {}) // 3 columns, 2 rows: 9 cells is 3 full rows
+	for _, s := range []struct{ n, from, want int }{
+		{8, 4, 7},   // column 1 exists in the short last row (6, 7)
+		{8, 5, 7},   // column 2 doesn't: its last cell
+		{7, 1, 6},   // the last row is just cell 6
+		{9, 0, 6},   // a whole page down is the last row already
+		{9, 7, 7},   // already there
+		{20, 4, 10}, // a page down within the grid is unchanged
+	} {
+		g.Focus = s.from
+		if !g.Handle(press(input.BtnR), s.n) || g.Focus != s.want {
+			t.Errorf("n=%d, R from %d: focus %d, want %d", s.n, s.from, g.Focus, s.want)
+		}
+	}
+}
+
 func TestListAndTabsLetNeighboursTakeFocus(t *testing.T) {
 	var l List
 	if l.Handle(press(input.BtnUp), 3) {
diff --git a/internal/ui/wizard_test.go b/internal/ui/wizard_test.go
index 0a90424..c42513b 100644
--- a/internal/ui/wizard_test.go
+++ b/internal/ui/wizard_test.go
@@ -253,13 +253,17 @@ func TestGoldenWizard(t *testing.T) {
 // People type addresses every which way.
 func TestNormalizeURL(t *testing.T) {
 	for in, want := range map[string]string{
-		"192.168.1.10:4533":                   "http://192.168.1.10:4533",
-		"  https://music.example.com/  ":      "https://music.example.com",
-		"https://music.example.com/rest":      "https://music.example.com",
-		"https://example.com/navidrome/rest/": "https://example.com/navidrome",
-		"HTTP://Music.Example.com:4533":       "http://Music.Example.com:4533",
-		"http://[::1]:4533":                   "http://[::1]:4533",
-		"https://alice:pw@music.example.com":  "https://music.example.com",
+		"192.168.1.10:4533":                    "http://192.168.1.10:4533",
+		"  https://music.example.com/  ":       "https://music.example.com",
+		"https://music.example.com/rest":       "https://music.example.com",
+		"https://example.com/navidrome/rest/":  "https://example.com/navidrome",
+		"HTTP://Music.Example.com:4533":        "http://Music.Example.com:4533",
+		"http://[::1]:4533":                    "http://[::1]:4533",
+		"https://alice:pw@music.example.com":   "https://music.example.com",
+		"https://music.example.com/?u=a#top":   "https://music.example.com",
+		"http://h:4533/navidrome?x=1":          "http://h:4533/navidrome",
+		"https://example.com/nav/rest?u=a&p=b": "https://example.com/nav",
+		"h:4533#frag":                          "http://h:4533",
 	} {
 		got, err := normalizeURL(in)
 		if err != nil || got != want {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
h.Rune undefined (type Hint has no field or method Rune)
h.Key undefined (type Hint has no field or method Key)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/README.md b/README.md
index 628e42b..604985e 100644
--- a/README.md
+++ b/README.md
@@ -102,7 +102,7 @@ app never changes the video mode or `MiSTer.ini` itself. Keep `fb_size=0` (autom
 | Select | shuffle-play the list | press: shuffle / repeat modes; hold 1 s: mute |
 
 Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = mute,
-PgUp/PgDn = L/R, Space = Start. In Search, letters type. With a controller, hold Select on
+PgUp/PgDn = L/R, Space = Start. In Search and the setup wizard, letters type and Backspace deletes. With a controller, hold Select on
 Now Playing to mute; changing the volume turns the sound back on. A multimedia keyboard's media
 keys work on every screen: volume up/down (held to repeat), mute, play/pause, next, previous, and
 fast-forward/rewind to seek. A volume panel shows the level for a moment whenever it changes.
@@ -110,9 +110,10 @@ fast-forward/rewind to seek. A volume panel shows the level for a moment wheneve
 **Hints:** the bar along the bottom of every screen shows what the buttons do there: as gamepad
 buttons after a gamepad press, as keys after a key press. Settings → Display → Hints turns it off.
 
-**Screenshots:** Print Screen, or Alt+Scroll Lock (MiSTer's own screenshot keys), saves the
-screen as a PNG in `/media/fat/screenshots/MiSTer_Subsonic/`. The MiSTer Companion remote's
-Capture screenshot button works too.
+**Screenshots:** Print Screen or Scroll Lock (alone; Alt+Scroll Lock is MiSTer's own key for it
+too) saves the screen as a PNG. On the MiSTer the folder is `/media/fat/screenshots/MiSTer_Subsonic/`;
+elsewhere it is `screenshots/` next to the config, and `-screenshots` changes it. A press while
+the last one is still saving says so. The MiSTer Companion remote's Capture screenshot button works too.
 
 ## Developing
 
diff --git a/internal/ui/grid.go b/internal/ui/grid.go
index 8b375ac..0c316dd 100644
--- a/internal/ui/grid.go
+++ b/internal/ui/grid.go
@@ -57,7 +57,7 @@ func (g *Grid) Handle(e input.Event, n int) bool {
 	case input.BtnL:
 		f = max(f-cols*max(g.rows, 1), f%cols)
 	case input.BtnR:
-		f = min(f+cols*max(g.rows, 1), n-1)
+		f = g.pageDown(f, n)
 	default:
 		return false
 	}
@@ -65,6 +65,16 @@ func (g *Grid) Handle(e input.Event, n int) bool {
 	return true
 }
 
+// pageDown is a page below f; past the end it is the last row, in f's
+// column when that row has it, else its last cell.
+func (g *Grid) pageDown(f, n int) int {
+	cols := g.columns()
+	if t := f + cols*max(g.rows, 1); t < n {
+		return t
+	}
+	return min((n-1)/cols*cols+f%cols, n-1)
+}
+
 // Draw lays out n cells of cellW×cellH in area (centred horizontally),
 // keeping the focused row visible, and calls cell for each visible one.
 func (g *Grid) Draw(c *gfx.Canvas, area gfx.Rect, n, cellW, cellH int, cell func(i int, r gfx.Rect, focused bool)) {
diff --git a/internal/ui/hints.go b/internal/ui/hints.go
index 8c4c575..035845c 100644
--- a/internal/ui/hints.go
+++ b/internal/ui/hints.go
@@ -13,10 +13,14 @@ import (
 // whichever was used last.
 
 // Hint is one entry: a button (and a second one for pairs such as L/R or
-// Left/Right) and what it does on this screen.
+// Left/Right) and what it does on this screen. A hint for a typing key
+// (Backspace, Enter) has Key, the cap drawn on a keyboard, and Rune, what
+// that key types; only a keyboard shows it.
 type Hint struct {
 	Button, Pair input.Button
 	Label        string
+	Key          string
+	Rune         rune
 }
 
 // Hinter is a screen with its own hints, most important first. The app adds
@@ -117,6 +121,13 @@ func (a *App) hintRect() gfx.Rect {
 	return gfx.R(0, a.P.H-a.P.SafeY-h, a.P.W, h)
 }
 
+// withBack puts Back right after the first hint, so a narrow bar (CRT)
+// keeps it: the bar drops what doesn't fit from the right.
+func withBack(hs []Hint) []Hint {
+	i := min(1, len(hs))
+	return append(hs[:i:i], append([]Hint{hk(input.BtnB, "Back")}, hs[i:]...)...)
+}
+
 // screenHints is the top screen's list plus Back and Now Playing when the
 // screen leaves B and Y to the app.
 func (a *App) screenHints() []Hint {
@@ -140,10 +151,7 @@ func (a *App) screenHints() []Hint {
 	}
 	out := append([]Hint(nil), hs...)
 	if len(a.stack) > 1 && !uses(input.BtnB) {
-		// Right after the first hint, so a narrow bar (CRT) keeps it: the
-		// bar drops what doesn't fit from the right.
-		back := hk(input.BtnB, "Back")
-		out = append(out[:min(1, len(out))], append([]Hint{back}, out[min(1, len(out)):]...)...)
+		out = withBack(out)
 	}
 	typing := false
 	if t, ok := top.(textTaker); ok {
@@ -178,6 +186,9 @@ func (a *App) drawHints(c *gfx.Canvas) {
 		ok := true
 		for _, b := range caps {
 			t, has := a.capFor(b)
+			if h.Key != "" {
+				t, has = h.Key, true
+			}
 			ok = ok && has
 			texts = append(texts, t)
 		}
@@ -268,7 +279,7 @@ func hintLabels(hs []Hint) string {
 func hintKey(hs []Hint) string {
 	var b strings.Builder
 	for _, h := range hs {
-		fmt.Fprintf(&b, "%d/%d/%s;", h.Button, h.Pair, h.Label)
+		fmt.Fprintf(&b, "%d/%d/%s/%s;", h.Button, h.Pair, h.Label, h.Key)
 	}
 	return b.String()
 }
diff --git a/internal/ui/hints_screens.go b/internal/ui/hints_screens.go
index e49eb44..a1b0d31 100644
--- a/internal/ui/hints_screens.go
+++ b/internal/ui/hints_screens.go
@@ -115,26 +115,45 @@ func (s *ServersScreen) Hints(a *App) []Hint {
 
 func (s *UnreachableScreen) Hints(*App) []Hint { return []Hint{hk(input.BtnA, "Choose")} }
 
-func (s *SearchScreen) Hints(*App) []Hint {
+func (s *SearchScreen) Hints(a *App) []Hint {
 	if s.inResults {
 		return []Hint{openHint, menuHint, hk(input.BtnB, "Keyboard")}
 	}
-	return typing(len(s.query) > 0)
+	return typing(a, "", len(s.query) > 0)
 }
 
-// typing is an on-screen keyboard's hints: Delete only when there is text.
-func typing(text bool) []Hint {
-	if !text {
-		return []Hint{hk(input.BtnA, "Type")}
+// typing is a text field's hints. A gamepad types with A on the on-screen
+// keyboard and deletes with X, only when there is text. On a keyboard the
+// letters type directly and Backspace deletes; Enter is hinted (as submit,
+// next) only on a screen where it does that.
+func typing(a *App, enter string, text bool) []Hint {
+	if a.pad {
+		if !text {
+			return []Hint{hk(input.BtnA, "Type")}
+		}
+		return []Hint{hk(input.BtnA, "Type"), hk(input.BtnX, "Delete")}
+	}
+	var hs []Hint
+	if enter != "" {
+		hs = append(hs, Hint{Button: input.BtnA, Key: "Enter", Rune: '\n', Label: enter})
+	}
+	if text {
+		hs = append(hs, Hint{Button: input.BtnX, Key: "Backspace", Rune: '\b', Label: "Delete"})
 	}
-	return []Hint{hk(input.BtnA, "Type"), hk(input.BtnX, "Delete")}
+	return hs
 }
 
-func (s *WizardScreen) Hints(*App) []Hint {
+func (s *WizardScreen) Hints(a *App) []Hint {
+	var hs []Hint
 	if s.step == stepTest {
-		return []Hint{hk(input.BtnA, "Choose")}
+		hs = []Hint{hk(input.BtnA, "Choose")}
+	} else {
+		hs = typing(a, "Next", len(s.fields[s.step]) > 0)
 	}
-	return typing(len(s.fields[s.step]) > 0)
+	if s.step > stepURL { // B goes back a step (on the first one the app adds Back, if it can)
+		hs = withBack(hs)
+	}
+	return hs
 }
 
 func (s *FeedScreen) Hints(*App) []Hint {
@@ -165,7 +184,14 @@ func (s *SidebarRoot) Hints(a *App) []Hint {
 		}
 		return []Hint{openHint}
 	}
-	return hintsOf(a, s.current())
+	// B in a section goes back to the sidebar, unless the section uses it.
+	hs := hintsOf(a, s.current())
+	for _, h := range hs {
+		if h.Button == input.BtnB || h.Pair == input.BtnB {
+			return hs
+		}
+	}
+	return withBack(hs)
 }
 
 func (s *TabbedScreen) Hints(a *App) []Hint {
diff --git a/internal/ui/menus.go b/internal/ui/menus.go
index 3104f53..773126c 100644
--- a/internal/ui/menus.go
+++ b/internal/ui/menus.go
@@ -2,6 +2,8 @@ package ui
 
 import (
 	"context"
+	"errors"
+	"fmt"
 	"math/rand/v2"
 
 	"mistersubsonic/internal/subsonic"
@@ -33,6 +35,14 @@ func (a *App) playSongs(songs []subsonic.Song, start int, shuffle bool) {
 // songLoader fetches the songs behind an item (an album, a playlist...).
 type songLoader func(ctx context.Context, lib Library) ([]subsonic.Song, error)
 
+// partialLoad is the error of a load that got some of its songs: how many
+// of its albums failed. withSongs plays what loaded and says so.
+type partialLoad struct{ failed, total int }
+
+func (e *partialLoad) Error() string {
+	return fmt.Sprintf("%d of %d albums failed", e.failed, e.total)
+}
+
 // withSongs loads songs off the UI goroutine, then calls f with them; an
 // error or an empty result is a toast instead.
 func (a *App) withSongs(load songLoader, f func([]subsonic.Song)) {
@@ -42,6 +52,11 @@ func (a *App) withSongs(load songLoader, f func([]subsonic.Song)) {
 	}
 	a.Load(a.Top(), func(ctx context.Context) (any, error) { return load(ctx, lib) }, func(v any, err error) {
 		songs, _ := v.([]subsonic.Song)
+		var part *partialLoad
+		if errors.As(err, &part) {
+			a.Toast("Couldn't load %d of %d albums", part.failed, part.total)
+			err = nil
+		}
 		switch {
 		case err != nil:
 			a.Toast("Couldn't load: %s", subsonic.Classify(err))
@@ -78,11 +93,13 @@ func playlistSongs(id subsonic.ID) songLoader {
 const maxShuffleAlbums = 10
 
 // albumsSongs loads the songs of albums, one request each. Albums that fail
-// are skipped unless all do.
+// are skipped unless all do; when some do, the error is a *partialLoad
+// beside the songs.
 func albumsSongs(albums []subsonic.Album) songLoader {
 	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
 		var songs []subsonic.Song
 		var firstErr error
+		failed := 0
 		for _, al := range albums {
 			got, err := albumSongs(al.ID)(ctx, lib)
 			if err != nil {
@@ -90,6 +107,7 @@ func albumsSongs(albums []subsonic.Album) songLoader {
 					return nil, ctx.Err()
 				}
 				firstErr = cmpErr(firstErr, err)
+				failed++
 				continue
 			}
 			songs = append(songs, got...)
@@ -97,6 +115,9 @@ func albumsSongs(albums []subsonic.Album) songLoader {
 		if len(songs) == 0 {
 			return nil, firstErr
 		}
+		if failed > 0 {
+			return songs, &partialLoad{failed, len(albums)}
+		}
 		return songs, nil
 	}
 }
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index d613235..4660f7d 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -312,8 +312,8 @@ func (s *QueueScreen) Handle(a *App, e input.Event) bool {
 	case input.BtnX:
 		i := s.list.Focus
 		title := st.Queue[i].Title
-		a.Push(NewMenuScreen(s, title, []menuEntry{
-			{"Remove", func(a *App) {
+		a.Push(NewMenuScreen(s, "Queue", []menuEntry{
+			{"Remove " + title, func(a *App) {
 				a.Player().Remove(i)
 				a.Toast("Removed %s", title)
 			}},
diff --git a/internal/ui/screens_playlists.go b/internal/ui/screens_playlists.go
index 23c98aa..0f48cf7 100644
--- a/internal/ui/screens_playlists.go
+++ b/internal/ui/screens_playlists.go
@@ -191,7 +191,13 @@ func (s *PlaylistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 		return
 	}
 	songs := s.songs()
-	s.list.Draw(c, body, playlistActionRows+len(songs), p.Row2H, func(i int, r gfx.Rect, focused bool) {
+	// Two-line rows (title over artist) where they fit; a CRT has no room for
+	// them under Play and Shuffle, so its tracks are one line, "Title — Artist".
+	rowH, oneLine := p.Row2H, p.SideW == 0
+	if oneLine {
+		rowH = p.RowH
+	}
+	s.list.Draw(c, body, playlistActionRows+len(songs), rowH, func(i int, r gfx.Rect, focused bool) {
 		switch i {
 		case 0:
 			f := a.F.Body
@@ -200,8 +206,15 @@ func (s *PlaylistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
 		default:
 			so := songs[i-playlistActionRows]
-			a.drawRow(c, r, row{cover: so.CoverArt, thumb: true, main: so.Title, sub: so.Artist, focused: focused,
-				starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))})
+			w := row{cover: so.CoverArt, thumb: true, main: so.Title, sub: so.Artist, focused: focused,
+				starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))}
+			if oneLine {
+				w.thumb, w.sub = false, ""
+				if so.Artist != "" {
+					w.main += " — " + so.Artist
+				}
+			}
+			a.drawRow(c, r, w)
 		}
 	})
 }
diff --git a/internal/ui/screens_search.go b/internal/ui/screens_search.go
index 1c09ebe..711c220 100644
--- a/internal/ui/screens_search.go
+++ b/internal/ui/screens_search.go
@@ -341,7 +341,7 @@ func (s *SearchScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	h := p.RowH
 	s.tabs.Draw(a, c, gfx.R(results.X, results.Y, results.W, h), s.inResults && s.onTabs)
 	view := gfx.R(results.X, results.Y+h, results.W, results.H-h)
-	a.drawDimmed(!s.inResults, func() {
+	a.drawDimmed(!s.inResults || s.onTabs, func() { // only the focused list scrolls
 		switch s.tabs.Sel {
 		case resArtists:
 			s.artists.Draw(a, c, view)
diff --git a/internal/ui/screenshot.go b/internal/ui/screenshot.go
index 3574659..b021e3f 100644
--- a/internal/ui/screenshot.go
+++ b/internal/ui/screenshot.go
@@ -15,10 +15,14 @@ import (
 // screenshot saves the frame on screen as a PNG in Options.ScreenshotDir,
 // named by the time (20260930_101500.png). The frame is copied here and
 // encoded off the UI goroutine; a toast says how it went. One runs at a
-// time: presses during a save are ignored.
+// time: a press during a save only toasts.
 func (a *App) screenshot() {
 	dir := a.o.ScreenshotDir
-	if dir == "" || a.shooting {
+	if dir == "" {
+		return
+	}
+	if a.shooting {
+		a.Toast("Still saving the last screenshot")
 		return
 	}
 	a.shooting = true
diff --git a/internal/ui/tabbed.go b/internal/ui/tabbed.go
index 7bd6f85..1a6b5cb 100644
--- a/internal/ui/tabbed.go
+++ b/internal/ui/tabbed.go
@@ -83,5 +83,7 @@ func (s *TabbedScreen) Handle(a *App, e input.Event) bool {
 func (s *TabbedScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	h := a.P.RowH
 	s.tabs.Draw(a, c, gfx.R(area.X, area.Y, area.W, h), s.onTabs)
-	s.children[s.tabs.Sel].Draw(a, c, gfx.R(area.X, area.Y+h, area.W, area.H-h))
+	a.drawDimmed(s.onTabs, func() { // only the focused thing scrolls a long title
+		s.children[s.tabs.Sel].Draw(a, c, gfx.R(area.X, area.Y+h, area.W, area.H-h))
+	})
 }
diff --git a/internal/ui/wizard.go b/internal/ui/wizard.go
index 9cd9436..cc68524 100644
--- a/internal/ui/wizard.go
+++ b/internal/ui/wizard.go
@@ -202,7 +202,9 @@ func (s *WizardScreen) next(a *App) {
 	}
 }
 
-// normalizeURL accepts "host:port" as http and trims a trailing "/rest".
+// normalizeURL accepts "host:port" as http, trims a trailing "/rest" and
+// drops a pasted ?query and #fragment (the path stays: a server may live
+// under a prefix).
 func normalizeURL(raw string) (string, error) {
 	raw = strings.TrimSpace(raw)
 	if raw == "" {
@@ -215,7 +217,7 @@ func normalizeURL(raw string) (string, error) {
 	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
 		return "", errors.New("That doesn't look like http://host:port or https://host")
 	}
-	u.User = nil
+	u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
 	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/rest")
 	return strings.TrimSuffix(u.String(), "/"), nil
 }
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index 8f09758..63d6e88 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -2,7 +2,8 @@
 #
 # You don't need this file: on first start the setup wizard writes
 # config.toml, and Settings saves your changes there. To write one by hand,
-# copy this file to config.toml in the same folder and edit it. Lines
+# copy this file to config.toml in the same folder and edit it: change the
+# url, username and password below first, they are only examples. Lines
 # starting with # are comments.
 
 # The server to connect to at start: one of the names below.
@@ -19,7 +20,7 @@ password = "change-me"             # the wizard saves a token and salt instead
 # api_key = "…"                    # an OpenSubsonic API key, for servers that give them out
 # ca_file = "/media/fat/mistersubsonic/myca.pem"   # trust a self-signed certificate
 # insecure_skip_verify = false     # true skips the certificate check (not recommended)
-# allow_plaintext_password = false # true sends the password itself (servers using LDAP)
+# allow_plaintext_password = false # true sends the password itself, not a token; only for servers that need it (LDAP), and best over https
 
 [playback]
 transcode_format = "mp3"   # for formats the MiSTer can't decode: mp3, flac or wav
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `playlist-crt`, `root-feed-hdmi-960x600`, `root-feed-hdmi`, `search-crt`, `search-hdmi`, `wizard-error-crt`, `wizard-error-hdmi`, `wizard-url-crt`, `wizard-url-hdmi`, `wizard-url-problem-crt`, `wizard-url-problem-hdmi`. Open each one and check it: `root-feed-hdmi` and `root-feed-hdmi-960x600` show the bar "Enter Open, Esc Back, Tab Menu". `search-hdmi` and `search-crt` show "Backspace Delete, Esc Back". The `wizard-url-*` and `wizard-url-problem-*` goldens show "Enter Next, Backspace Delete", and `wizard-error-*` shows "Enter Choose, Esc Back". `playlist-crt` shows Play, Shuffle, then both tracks as one-line "Title — Artist" rows with their durations.

- [ ] **Step 5: Commit**

```bash
git add README.md internal/ui/browse_test.go internal/ui/fakes_test.go internal/ui/grid.go internal/ui/hints.go internal/ui/hints_screens.go internal/ui/hints_test.go internal/ui/lists_test.go internal/ui/menus.go internal/ui/play_test.go internal/ui/screens_play.go internal/ui/screens_playlists.go internal/ui/screens_search.go internal/ui/screenshot.go internal/ui/screenshot_test.go internal/ui/search_test.go internal/ui/tabbed.go internal/ui/widgets_test.go internal/ui/wizard.go internal/ui/wizard_test.go sdcard/mistersubsonic/config.example.toml internal/ui/testdata/golden
git commit -m "ui: clearer queue menu and hints, grid R keeps its column, partial album loads and busy screenshots say so" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: MP3 seeks reuse the stream and use the Xing table

**Files:**
- Create: `internal/player/mp3.go`
- Modify: `internal/audio/engine.go`, `internal/player/player.go`, `internal/player/stream.go`, `internal/stream/reader.go`
- Test: `internal/player/mp3_test.go` (new); `internal/audio/engine_test.go`, `internal/player/fakes_test.go`, `internal/player/opener_test.go`, `internal/player/player_test.go`, `internal/stream/reader_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **`internal/audio`: `(*Engine).Replace(t Track, prep func())`** swaps the playing track for `t` and keeps the queued successor. `Play` drops it.
    - `doStop` is split into `stopCurrent` and `cancelNext`. `doPlay` becomes `startTrack(t, keepNext)`.
    - `prep` runs in the caller before the command is queued.
    - A `replacing` counter stops `finishCur` from starting the successor while a swap is pending.
    - If `t` fails to open, the successor starts, or starts when it has opened (`e.ended = true`).
  - **`internal/stream`:**
    - `(*Reader).Holds(off) bool`: `off` is inside the buffered window, and the reader isn't closed.
    - `(*Reader).SeekIfBuffered(off) bool`: it seeks only if `Holds(off)`, and wakes a waiting Read.
    - **Fix to an old race.** `copyBody` moves `lo` up to `effectiveLo()` before it releases the lock to read the body, because that read may overwrite everything below it. Before this, a seek back into that range during a fetch counted on bytes the append then dropped: the next Read had to restart. `TestHoldsLeavesOutWhatAFetchInFlightMayOverwrite` pins it.
  - **`internal/player`, `mp3.go`:**
    - `probeMP3(r io.ReadSeeker) mp3Layout{start, toc, bytes}` reads past the ID3v2 tag and up to 4 KiB of the first Layer III frame (`mpegLayer3`).
    - `parseXing` finds "Xing" or "Info" after the side info (32, 17 or 9 bytes, plus 2 for a CRC) and returns the 100-byte table.
    - `mp3Layout.offset(song, at) (int64, bool)` maps a time to a byte through the table, interpolating between entries. The fractions are of the audio bytes. Without a table it falls back to the old proportion.
    - A tag over 256 KiB skips the probe.
    - `mp3Offset` is removed.
  - **`internal/player`, `stream.go`:**
    - An MP3 of known size and duration opens as a `*fromOffset` view over a shared, reference-counted `mp3Stream{r, layout, refs}`. The reader closes when the last view closes, and a closed view returns `stream.ErrClosed`.
    - `retargeter` and `(*fromOffset).retarget(song, at) (Opened, func(), bool)` give a new view, plus the `prep` that moves the reader, when the reader `Holds` the byte.
    - A new view moves the reader on its first Read or Seek (`place`).
  - **`internal/player`, `player.go`:**
    - The `Engine` interface gains `Replace`.
    - `Player.Seek` tries `retargetAt` first, which gives a new id, sets `Loading` unless paused, and calls `Engine.Replace`. Otherwise it uses `reopenAt`, as before.
    - The successor, `nextID` and the prefetch are left alone.
  - **Unchanged:** transcoded streams, FLAC, and MP3s without a size or duration.
  - **Fakes:** `fakeEngine.Replace`, `fakeStreamSource` and `fakeOpener.window`.
  - **The opener test's window is only 8 KiB.** Probing the first frame reads past the 2 KiB kept behind the reader, so going back to the start may take a second request. `TestMP3StreamRetargetsWithinItsWindow` counts only what the retarget costs.

- [ ] **Step 1: Write the failing tests**

`internal/player/mp3_test.go` (new file):

```go
package player

import (
	"bytes"
	"os"
	"testing"
	"time"

	"mistersubsonic/internal/subsonic"
)

// xingFrame builds a first MPEG frame carrying a Xing/Info header: MPEG-1
// (or 2) Layer III at 44.1 (22.05) kHz, joint stereo or mono.
func xingFrame(mpeg2, mono, crc bool, flags uint32, frames, size uint32, toc []byte) []byte {
	b1 := byte(0xFB) // MPEG-1, layer III, no CRC
	if mpeg2 {
		b1 = 0xF3
	}
	if crc {
		b1 &^= 1
	}
	mode := byte(1) << 6
	side := 32
	switch {
	case mono && mpeg2:
		mode, side = 3<<6, 9
	case mono:
		mode, side = 3<<6, 17
	case mpeg2:
		side = 17
	}
	f := []byte{0xFF, b1, 0x90, mode}
	if crc {
		f = append(f, 0, 0)
	}
	f = append(f, make([]byte, side)...)
	f = append(f, "Xing"...)
	put := func(v uint32) { f = append(f, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)) }
	put(flags)
	if flags&1 != 0 {
		put(frames)
	}
	if flags&2 != 0 {
		put(size)
	}
	if flags&4 != 0 {
		f = append(f, toc...)
	}
	return f
}

// quadToc puts i% of the time at (i/100)^2 of the bytes: a bitrate that
// starts low and grows.
func quadToc() []byte {
	toc := make([]byte, 100)
	for i := range toc {
		toc[i] = byte(i * i * 256 / 10000)
	}
	return toc
}

func tagged(n int) []byte { // an ID3v2.4 tag of n bytes after its header
	return append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, byte(n >> 7 & 0x7F), byte(n & 0x7F)}, make([]byte, n)...)
}

func TestProbeMP3ReadsTheXingTable(t *testing.T) {
	toc := quadToc()
	for name, c := range map[string]struct {
		mpeg2, mono, crc bool
	}{"mpeg1 stereo": {}, "mpeg1 mono": {mono: true}, "mpeg2 stereo": {mpeg2: true},
		"mpeg2 mono": {mpeg2: true, mono: true}, "crc": {crc: true}} {
		data := append(tagged(300), xingFrame(c.mpeg2, c.mono, c.crc, 7, 3000, 25600, toc)...)
		data = append(data, make([]byte, 1000)...)
		l := probeMP3(bytes.NewReader(data))
		if l.start != 310 || l.bytes != 25600 || !bytes.Equal(l.toc, toc) {
			t.Errorf("%s: start %d bytes %d toc ok %v", name, l.start, l.bytes, bytes.Equal(l.toc, toc))
		}
	}
}

func TestProbeMP3WithoutATable(t *testing.T) {
	audio := append([]byte{0xFF, 0xFB, 0x90, 0x44}, make([]byte, 400)...) // a frame with no Xing/Info
	cases := map[string][]byte{
		"no header at all":  make([]byte, 64),
		"no table flag":     append(tagged(20), xingFrame(false, false, false, 3, 10, 999, nil)...),
		"plain first frame": append(tagged(20), audio...),
		"truncated table":   append(tagged(20), xingFrame(false, false, false, 7, 10, 999, make([]byte, 40))...),
	}
	for name, data := range cases {
		if l := probeMP3(bytes.NewReader(data)); l.toc != nil {
			t.Errorf("%s: toc %v, want none", name, l.toc)
		}
	}
	if l := probeMP3(bytes.NewReader(cases["plain first frame"])); l.start != 30 {
		t.Errorf("start %d, want the frame after the 30-byte tag", l.start)
	}
	if l := probeMP3(bytes.NewReader(cases["no header at all"])); l.start != 0 {
		t.Errorf("no tag: start %d, want 0", l.start)
	}
}

func TestProbeMP3TestdataXingHeader(t *testing.T) {
	data, err := os.ReadFile("../audio/testdata/tone-44k16.mp3") // ffmpeg's Info header, 44-byte tag
	if err != nil {
		t.Fatal(err)
	}
	l := probeMP3(bytes.NewReader(data))
	if l.start != 44 || l.bytes != int64(len(data))-44 || len(l.toc) != 100 || l.toc[1] != 0x17 || l.toc[99] != 0xff {
		t.Fatalf("layout start %d bytes %d toc %d entries (toc[1]=%#x)", l.start, l.bytes, len(l.toc), l.toc[1])
	}
	noxing, _ := os.ReadFile("../audio/testdata/tone-44k16-noxing.mp3")
	if l := probeMP3(bytes.NewReader(noxing)); l.toc != nil || l.start != 44 {
		t.Fatalf("no-Xing file: start %d, toc %v", l.start, l.toc)
	}
}

func TestMP3LayoutOffsetFollowsTheTable(t *testing.T) {
	l := mp3Layout{start: 100, toc: quadToc(), bytes: 25600}
	song := subsonic.Song{Size: 25700, Duration: 100}
	for name, c := range map[string]struct {
		at   time.Duration
		want int64
	}{
		"start":                             {0, 100},
		"halfway is a quarter of the bytes": {50 * time.Second, 100 + 6400},
		"between entries":                   {50500 * time.Millisecond, 100 + 6500},  // toc[50]=64, toc[51]=66
		"last entry":                        {99500 * time.Millisecond, 100 + 25300}, // toc[99]=250, then 256
		"past the end":                      {500 * time.Second, 25699},
	} {
		if got, ok := l.offset(song, c.at); got != c.want || !ok {
			t.Errorf("%s: %d,%v; want %d", name, got, ok, c.want)
		}
	}
	// Without a table the estimate stays proportional.
	l.toc = nil
	if got, _ := l.offset(song, 50*time.Second); got != 100+12800 {
		t.Errorf("no table: %d, want the proportional %d", got, 100+12800)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index db20fca..21213e0 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -1085,3 +1085,146 @@ func TestEngineCloseDuringDoStopMissesNoSource(t *testing.T) {
 		t.Error("the Play's source was left open")
 	}
 }
+
+// Replace swaps the current track like Play but keeps the queued successor,
+// so a seek that reopens the current song doesn't cost the gapless next one.
+func TestEngineReplaceKeepsTheQueuedSuccessor(t *testing.T) {
+	out := newFakeOutput(300) // small: the tracks stay mid-decode
+	e := newTestEngine(out)
+	defer e.Close()
+
+	next := newFakeSource(ramp(400, 5000), OutputRate)
+	next.openStarted = make(chan struct{})
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(2000, 0), OutputRate)})
+	expectEvent(t, e, EventStarted, 1)
+	e.QueueNext(Track{ID: 2, Source: next})
+	<-next.openStarted
+	time.Sleep(20 * time.Millisecond) // let it finish opening
+
+	ran := false
+	e.Replace(Track{ID: 3, Source: newFakeSource(ramp(500, 100), OutputRate)}, func() { ran = true })
+	if !ran {
+		t.Fatal("prep did not run before Replace returned")
+	}
+	expectEvent(t, e, EventStarted, 3)
+	playOut(t, out, 300+500+400)
+	expectEvent(t, e, EventEnded, 3)
+	expectEvent(t, e, EventStarted, 2)
+	expectEvent(t, e, EventEnded, 2)
+}
+
+// A replacement that can't open hands over to the successor instead of
+// leaving the engine silent with it queued.
+func TestEngineReplaceOpenFailureContinuesIntoTheSuccessor(t *testing.T) {
+	out := newFakeOutput(300)
+	e := newTestEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(2000, 0), OutputRate)})
+	expectEvent(t, e, EventStarted, 1)
+	e.QueueNext(Track{ID: 2, Source: newFakeSource(ramp(200, 5000), OutputRate)})
+	time.Sleep(20 * time.Millisecond)
+
+	bad := newFakeSource(nil, OutputRate)
+	bad.openErr = errBoom
+	e.Replace(Track{ID: 3, Source: bad}, nil)
+	var sawErr, sawStart bool
+	for !sawErr || !sawStart {
+		ev := nextEvent(t, e)
+		switch {
+		case ev.Kind == EventError && ev.TrackID == 3 && ev.Err == errBoom:
+			sawErr = true
+		case ev.Kind == EventStarted && ev.TrackID == 2:
+			sawStart = true
+		default:
+			t.Fatalf("unexpected event %+v", ev)
+		}
+		playOut(t, out, 0)
+	}
+	playOut(t, out, 200)
+	expectEvent(t, e, EventEnded, 2)
+}
+
+// Replace, like Play, doesn't wait for a stalled read of the old track.
+func TestEngineReplaceInterruptsStalledRead(t *testing.T) {
+	out := newFakeOutput(100000)
+	e := newTestEngine(out)
+	defer e.Close()
+
+	stalled := newFakeSource(ramp(1000, 0), OutputRate)
+	stalled.block = make(chan struct{})
+	e.Play(Track{ID: 1, Source: stalled})
+	expectEvent(t, e, EventStarted, 1)
+	e.Replace(Track{ID: 2, Source: newFakeSource(ramp(100, 0), OutputRate)}, nil)
+	expectEvent(t, e, EventStarted, 2)
+	if !stalled.isClosed() {
+		t.Fatal("stalled source was not closed")
+	}
+}
+
+// Same, when the successor is still opening as the replacement fails: it
+// starts as soon as it is open.
+func TestEngineReplaceFailureBeforeTheSuccessorOpenedStartsItLater(t *testing.T) {
+	out := newFakeOutput(300)
+	e := newTestEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(2000, 0), OutputRate)})
+	expectEvent(t, e, EventStarted, 1)
+	next := newFakeSource(ramp(200, 5000), OutputRate)
+	next.openBlock = make(chan struct{})
+	next.openStarted = make(chan struct{})
+	e.QueueNext(Track{ID: 2, Source: next})
+	<-next.openStarted
+
+	bad := newFakeSource(nil, OutputRate)
+	bad.openErr = errBoom
+	e.Replace(Track{ID: 3, Source: bad}, nil)
+	ev := nextEvent(t, e)
+	if ev.Kind != EventError || ev.TrackID != 3 {
+		t.Fatalf("event %+v, want the replacement's error", ev)
+	}
+	close(next.openBlock)
+	go func() {
+		for i := 0; i < 500; i++ {
+			out.consume(1 << 30)
+			time.Sleep(time.Millisecond)
+		}
+	}()
+	expectEvent(t, e, EventStarted, 2)
+}
+
+// If the old track runs out between prep and the swap (prep moved its
+// stream to the end, say), the successor must not start in its place only to
+// be dropped by the swap.
+func TestEngineReplaceWhileTheOldTrackEndsKeepsTheSuccessor(t *testing.T) {
+	out := newFakeOutput(300)
+	e := newTestEngine(out)
+	defer e.Close()
+
+	old := newFakeSource(ramp(100, 0), OutputRate)
+	old.block = make(chan struct{})
+	old.readStarted = make(chan struct{})
+	e.Play(Track{ID: 1, Source: old})
+	<-old.readStarted
+	next := newFakeSource(ramp(200, 5000), OutputRate)
+	e.QueueNext(Track{ID: 2, Source: next}) // waits in the queue behind the blocked read
+
+	e.Replace(Track{ID: 3, Source: newFakeSource(ramp(100, 100), OutputRate)}, func() {
+		close(old.block)                  // the old track reads on and ends
+		time.Sleep(50 * time.Millisecond) // before Replace's command is queued
+	})
+	go func() {
+		for i := 0; i < 1000; i++ {
+			out.consume(1 << 30)
+			time.Sleep(time.Millisecond)
+		}
+	}()
+	for ev := nextEvent(t, e); ev.TrackID != 3; ev = nextEvent(t, e) {
+		if ev.TrackID != 1 {
+			t.Fatalf("event %+v before the replacement started", ev) // the old track's own may show
+		}
+	}
+	expectEvent(t, e, EventEnded, 3)
+	expectEvent(t, e, EventStarted, 2)
+}
diff --git a/internal/player/fakes_test.go b/internal/player/fakes_test.go
index 2ad8cc9..b6b5865 100644
--- a/internal/player/fakes_test.go
+++ b/internal/player/fakes_test.go
@@ -22,6 +22,7 @@ type seekCall struct {
 type fakeEngine struct {
 	mu        sync.Mutex
 	played    []audio.Track
+	replaced  []audio.Track // Replace calls
 	queued    []audio.Track
 	clears    int
 	stops     int
@@ -46,6 +47,15 @@ func (e *fakeEngine) Play(t audio.Track) {
 	e.played = append(e.played, t)
 	e.posID, e.pos, e.posOK = t.ID, t.Offset, true
 }
+func (e *fakeEngine) Replace(t audio.Track, prep func()) {
+	if prep != nil {
+		prep()
+	}
+	e.mu.Lock()
+	defer e.mu.Unlock()
+	e.replaced = append(e.replaced, t)
+	e.posID, e.pos, e.posOK = t.ID, t.Offset, true
+}
 func (e *fakeEngine) QueueNext(t audio.Track) {
 	e.mu.Lock()
 	e.queued = append(e.queued, t)
@@ -117,7 +127,13 @@ func (e *fakeEngine) lastPlayed() audio.Track {
 	defer e.mu.Unlock()
 	return e.played[len(e.played)-1]
 }
-func (e *fakeEngine) playCount() int  { e.mu.Lock(); defer e.mu.Unlock(); return len(e.played) }
+func (e *fakeEngine) playCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.played) }
+func (e *fakeEngine) replaceList() []audio.Track {
+	e.mu.Lock()
+	defer e.mu.Unlock()
+	return append([]audio.Track(nil), e.replaced...)
+}
+func (e *fakeEngine) clearCount() int { e.mu.Lock(); defer e.mu.Unlock(); return e.clears }
 func (e *fakeEngine) queueCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.queued) }
 
 type scrobbleCall struct {
@@ -193,6 +209,21 @@ func (s *fakeSource) Close() error                   { s.mu.Lock(); s.closed = t
 func (s *fakeSource) Promote()                       { s.mu.Lock(); s.promoted = true; s.mu.Unlock() }
 func (s *fakeSource) isPromoted() bool               { s.mu.Lock(); defer s.mu.Unlock(); return s.promoted }
 
+// fakeStreamSource is a sized MP3's stream that can seek within itself, as
+// NewOpener's do: retarget answers for positions inside window.
+type fakeStreamSource struct {
+	fakeSource
+	window func(time.Duration) bool
+	preps  atomic.Int32
+}
+
+func (s *fakeStreamSource) retarget(_ subsonic.Song, at time.Duration) (Opened, func(), bool) {
+	if !s.window(at) {
+		return Opened{}, nil, false
+	}
+	return Opened{Source: &fakeSource{song: s.song}, Format: audio.FormatMP3, Offset: at}, func() { s.preps.Add(1) }, true
+}
+
 type openCall struct {
 	id       subsonic.ID
 	offset   time.Duration
@@ -204,6 +235,9 @@ type fakeOpener struct {
 	calls []openCall
 	fail  map[subsonic.ID]bool
 	gate  chan struct{} // if set, open blocks (after recording the call) until it is closed
+	// window, if set, makes sized MP3s open as streams that seek themselves
+	// within it (see fakeStreamSource).
+	window func(time.Duration) bool
 }
 
 func (o *fakeOpener) open(_ context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error) {
@@ -221,6 +255,9 @@ func (o *fakeOpener) open(_ context.Context, s subsonic.Song, offset time.Durati
 	}
 	_, f, transcoded := PlanStream(s, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
 	op := Opened{Source: &fakeSource{song: s.ID}, Format: f, Transcoded: transcoded}
+	if o.window != nil && f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0 && !prefetch {
+		op.Source = &fakeStreamSource{fakeSource: fakeSource{song: s.ID}, window: o.window}
+	}
 	if transcoded || (f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0) {
 		op.Offset = offset // as NewOpener starts these at offset
 	}
diff --git a/internal/player/opener_test.go b/internal/player/opener_test.go
index 8bb055e..83373d1 100644
--- a/internal/player/opener_test.go
+++ b/internal/player/opener_test.go
@@ -9,6 +9,7 @@ import (
 	"net/http/httptest"
 	"os"
 	"strings"
+	"sync/atomic"
 	"testing"
 	"time"
 
@@ -40,8 +41,9 @@ func TestOpenerRejectsErrorDocument(t *testing.T) {
 	}
 }
 
-// An MP3 opened at an offset starts near it (after the ID3v2 tag, in
-// proportion to time), and the decoder plays from there.
+// An MP3 opened at an offset starts near it (after the ID3v2 tag, where the
+// Xing table puts it: toc[50] is 139/256 of the audio), and the decoder
+// plays from there.
 func TestOpenerStartsAnMP3NearTheOffset(t *testing.T) {
 	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3") // 44-byte ID3v2 tag
 	if err != nil {
@@ -62,7 +64,7 @@ func TestOpenerStartsAnMP3NearTheOffset(t *testing.T) {
 	if op.Offset != time.Second {
 		t.Fatalf("Offset %v, want 1s: the stream starts there", op.Offset)
 	}
-	base := 44 + (len(mp3)-44)/2
+	base := 44 + int64(float64(len(mp3)-44)*139/256)
 	head := make([]byte, 16)
 	if _, err := io.ReadFull(op.Source, head); err != nil || !bytes.Equal(head, mp3[base:base+16]) {
 		t.Fatalf("stream starts with %x, want the bytes at %d (%v)", head, base, err)
@@ -115,9 +117,105 @@ func TestMP3Offset(t *testing.T) {
 		"no duration":   {make([]byte, 16), subsonic.Song{Size: 1000}, time.Second, 0, false},
 		"tag past size": {tagged, subsonic.Song{Size: 100, Duration: 100}, time.Second, 0, false},
 	} {
-		got, ok := mp3Offset(bytes.NewReader(c.data), c.song, c.at)
+		got, ok := probeMP3(bytes.NewReader(c.data)).offset(c.song, c.at)
 		if got != c.want || ok != c.ok {
 			t.Errorf("%s: %d,%v; want %d,%v", name, got, ok, c.want, c.ok)
 		}
 	}
 }
+
+// A VBR MP3 opened at an offset starts where its Xing table says, not where
+// the proportion says.
+func TestOpenerUsesTheXingTable(t *testing.T) {
+	audioBytes := 25600
+	mp3 := append(xingFrame(false, false, false, 6, 0, uint32(audioBytes), quadToc()), make([]byte, audioBytes)...)
+	for i := range mp3 {
+		if i >= 200 {
+			mp3[i] = byte(i) // so the first bytes read say where the stream began
+		}
+	}
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
+	}))
+	defer srv.Close()
+	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
+	song := subsonic.Song{ID: "v", Suffix: "mp3", Size: int64(len(mp3)), Duration: 100}
+	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), song, 50*time.Second, false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer closeOpened(op)
+	want := int64(audioBytes) / 4 // halfway in time is a quarter of the bytes (toc[50] = 64)
+	head := make([]byte, 8)
+	io.ReadFull(op.Source, head)
+	if !bytes.Equal(head, mp3[want:want+8]) {
+		t.Fatalf("stream starts with %x, want the bytes at %d (%x)", head, want, mp3[want:want+8])
+	}
+}
+
+// A seek inside the bytes the stream holds moves the same stream: the new
+// source shares it, so no new request goes out, and closing one of the two
+// sources doesn't end the other.
+func TestMP3StreamRetargetsWithinItsWindow(t *testing.T) {
+	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3")
+	if err != nil {
+		t.Fatal(err)
+	}
+	var requests atomic.Int32
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		requests.Add(1)
+		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
+	}))
+	defer srv.Close()
+	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
+	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
+	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3", WindowBytes: 8192})(context.Background(), song, 0, false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	rt, ok := op.Source.(retargeter)
+	if !ok {
+		t.Fatalf("a sized MP3 opens as %T, which can't be retargeted", op.Source)
+	}
+	old := op.Source.(*fromOffset)
+	waitBuffered := func(n int64) {
+		for i := 0; old.s.r.Buffered() < n; i++ {
+			if i > 2000 {
+				t.Fatalf("stream never buffered %d bytes", n)
+			}
+			time.Sleep(time.Millisecond)
+		}
+	}
+	waitBuffered(4096)
+	// Probing the first frame reads past the 2 KiB this small window keeps
+	// behind the reader, so going back to the start may have taken a second
+	// request. Only what the retarget costs counts.
+	opened := requests.Load()
+	if _, _, ok := rt.retarget(song, time.Second); ok {
+		t.Fatal("1s is ~11 KB in, beyond the 8 KiB window: it must not retarget")
+	}
+	next, prep, ok := rt.retarget(song, 100*time.Millisecond)
+	if !ok || next.Offset != 100*time.Millisecond || next.Format != audio.FormatMP3 {
+		t.Fatalf("retarget = %+v, %v", next, ok)
+	}
+	prep()
+	base := 44 + int64(float64(len(mp3)-44)*34/256) // toc[5]
+	head := make([]byte, 16)
+	if _, err := io.ReadFull(next.Source, head); err != nil || !bytes.Equal(head, mp3[base:base+16]) {
+		t.Fatalf("new source starts with %x, want the bytes at %d (%v)", head, base, err)
+	}
+	closeOpened(op) // the engine drops the old track
+	if _, err := old.Read(head); err == nil {
+		t.Fatal("a closed source still reads")
+	}
+	if _, err := next.Source.Read(head); err != nil {
+		t.Fatalf("closing the old source ended the new one: %v", err)
+	}
+	if n := requests.Load(); n != opened {
+		t.Fatalf("the retarget made %d requests, want none", n-opened)
+	}
+	closeOpened(next)
+	if _, err := old.s.r.Read(head); err == nil {
+		t.Fatal("the stream stayed open after both sources closed")
+	}
+}
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index b26e1b4..07ba330 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -752,6 +752,63 @@ func TestSeekingAnMP3ReopensNearTheTarget(t *testing.T) {
 	}
 }
 
+// A seek inside what the stream already holds swaps in a decoder on the same
+// stream and keeps the queued successor; a seek beyond it reopens as before.
+func TestSeekingAnMP3InsideTheStreamWindowKeepsTheNextTrack(t *testing.T) {
+	h := newHarness(t, nil)
+	h.opener.window = func(at time.Duration) bool { return at < 200*time.Second }
+	q := songs(2, 300)
+	q[0].Suffix, q[0].Size = "mp3", 9_600_000
+	h.p.PlayNow(q, 0)
+	a := h.playAndStart(1)
+	h.tickAt(a.ID, 285*time.Second)
+	h.waitFor("successor queued", func() bool { return h.eng.queueCount() == 1 })
+	opens := len(h.opener.callList())
+
+	h.p.Seek(100 * time.Second)
+	h.waitFor("replace", func() bool { return len(h.eng.replaceList()) == 1 })
+	tr := h.eng.replaceList()[0]
+	if tr.Offset != 100*time.Second || tr.ID == a.ID || tr.Format != audio.FormatMP3 {
+		t.Fatalf("replacement = %+v, want a new track starting at 100s", tr)
+	}
+	if n, c := h.eng.playCount(), h.eng.clearCount(); n != 1 || c != 0 {
+		t.Fatalf("plays %d, clears %d: the seek must not restart playback or drop the successor", n, c)
+	}
+	if n := len(h.opener.callList()); n != opens || len(h.eng.seekList()) != 0 {
+		t.Fatalf("opens %d -> %d, engine seeks %v: the stream should be reused", opens, n, h.eng.seekList())
+	}
+	if h.p.State().NextIndex != 1 {
+		t.Fatal("the successor is no longer next")
+	}
+	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
+	h.waitFor("playing", func() bool { return h.p.State().Status == Playing })
+	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: tr.ID}
+	h.waitFor("handover to the queued successor", func() bool { return h.p.State().Index == 1 })
+	if n := len(h.opener.callList()); n != opens {
+		t.Fatalf("the successor was opened again (%d opens)", n)
+	}
+
+}
+
+func TestSeekingAnMP3BeyondTheStreamWindowReopens(t *testing.T) {
+	h := newHarness(t, nil)
+	h.opener.window = func(at time.Duration) bool { return at < 200*time.Second }
+	q := songs(2, 300)
+	q[0].Suffix, q[0].Size = "mp3", 9_600_000
+	h.p.PlayNow(q, 0)
+	a := h.playAndStart(1)
+	h.tickAt(a.ID, 285*time.Second)
+	h.waitFor("successor queued", func() bool { return h.eng.queueCount() == 1 })
+	h.p.Seek(250 * time.Second)
+	h.waitFor("reopen", func() bool { return h.eng.playCount() == 2 })
+	if last := h.opener.callList()[len(h.opener.callList())-1]; last.id != "sa" || last.offset != 250*time.Second {
+		t.Fatalf("reopen call = %+v", last)
+	}
+	if n := len(h.eng.replaceList()); n != 0 || h.eng.clearCount() != 1 {
+		t.Fatalf("replaces %d, clears %d; want a plain reopen that drops the successor", n, h.eng.clearCount())
+	}
+}
+
 // A seek that lands while a sized MP3 is still opening reopens it at the
 // target: the open's stream starts at its own offset, so the engine can't
 // seek it (a seek to 0 would even be dropped).
diff --git a/internal/stream/reader_test.go b/internal/stream/reader_test.go
index 8c5fee1..27e9f3a 100644
--- a/internal/stream/reader_test.go
+++ b/internal/stream/reader_test.go
@@ -158,6 +158,93 @@ func TestSeekInsideWindowUsesNoNewRequest(t *testing.T) {
 	}
 }
 
+func TestSeekIfBufferedOnlyMovesInsideTheBuffer(t *testing.T) {
+	s := newServer(t, 8<<20, nil)
+	o := testOptions()
+	o.WindowBytes = 1 << 20
+	r := open(t, s.URL, o)
+	checkBytes(t, readAt(t, r, 0, 100<<10), 0)
+	deadline := time.Now().Add(2 * time.Second)
+	for r.Buffered() < 512<<10 && time.Now().Before(deadline) {
+		time.Sleep(time.Millisecond)
+	}
+	if !r.SeekIfBuffered(400 << 10) {
+		t.Fatal("400 KiB is buffered but the seek said no")
+	}
+	buf := make([]byte, 100)
+	io.ReadFull(r, buf)
+	checkBytes(t, buf, 400<<10)
+	if r.SeekIfBuffered(6 << 20) {
+		t.Fatal("6 MiB is beyond the window but the seek said yes")
+	}
+	if r.SeekIfBuffered(-1) {
+		t.Fatal("a negative offset is not buffered")
+	}
+	io.ReadFull(r, buf) // the refused seek left the position alone
+	checkBytes(t, buf, 400<<10+100)
+	if n := s.requests.Load(); n != 1 {
+		t.Fatalf("%d requests, want 1", n)
+	}
+	r.Close()
+	if r.SeekIfBuffered(400 << 10) {
+		t.Fatal("a closed reader seeks")
+	}
+}
+
+// A fetch in flight may overwrite what lies more than BehindBytes behind the
+// read position, so those bytes no longer count as held.
+func TestHoldsLeavesOutWhatAFetchInFlightMayOverwrite(t *testing.T) {
+	const size = 64 << 10
+	part := func(from, to int) []byte {
+		b := make([]byte, to-from)
+		for i := range b {
+			b[i] = pattern(int64(from + i))
+		}
+		return b
+	}
+	release := make(chan struct{})
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n != 1 {
+			return false
+		}
+		w.Header().Set("Content-Length", strconv.Itoa(size))
+		w.Header().Set("Accept-Ranges", "bytes")
+		w.Write(part(0, 8<<10))
+		w.(http.Flusher).Flush()
+		select {
+		case <-release:
+			w.Write(part(8<<10, size))
+		case <-r.Context().Done():
+		}
+		return true
+	})
+	o := testOptions()
+	o.WindowBytes, o.BehindBytes, o.StallTimeout = 8<<10, 2<<10, 5*time.Second
+	r := open(t, s.URL, o)
+	checkBytes(t, readAt(t, r, 0, 4<<10), 0)
+	// The ring is full, so the fetcher now has room for the 2 KiB that lie
+	// more than BehindBytes behind the reader, and waits for the server.
+	deadline := time.Now().Add(2 * time.Second)
+	for r.Holds(1<<10) && time.Now().Before(deadline) {
+		time.Sleep(time.Millisecond)
+	}
+	if r.Holds(1<<10) || r.SeekIfBuffered(1<<10) {
+		t.Fatal("byte 1024 is about to be overwritten, but the reader still counts it as held")
+	}
+	close(release)
+	if !r.SeekIfBuffered(3 << 10) {
+		t.Fatal("byte 3072 is within BehindBytes and held, but the seek said no")
+	}
+	buf := make([]byte, 8<<10)
+	if _, err := io.ReadFull(r, buf); err != nil {
+		t.Fatal(err)
+	}
+	checkBytes(t, buf, 3<<10)
+	if n := s.requests.Load(); n != 1 {
+		t.Fatalf("%d requests, want 1", n)
+	}
+}
+
 func TestSeekFarIntoHugeFileUsesRange(t *testing.T) {
 	const size = 3 << 30 // 3 GiB, never materialised
 	s := newServer(t, size, nil)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/audio ./internal/player ./internal/stream`

Expected: FAIL, e.g.:

```
r.SeekIfBuffered undefined (type *Reader has no field or method SeekIfBuffered)
r.Holds undefined (type *Reader has no field or method Holds)
undefined: probeMP3
undefined: mp3Layout
undefined: retargeter
old.s undefined (type *fromOffset has no field or method s)
e.Replace undefined (type *Engine has no field or method Replace)
```

- [ ] **Step 3: Implement**

`internal/player/mp3.go` (new file):

```go
package player

import (
	"io"
	"time"

	"mistersubsonic/internal/subsonic"
)

// mp3Layout is what the start of an MP3 says about where its audio is.
type mp3Layout struct {
	start int64  // the first frame, after the ID3v2 tag
	toc   []byte // the Xing/Info seek table: 100 entries, or nil
	bytes int64  // audio bytes the table's fractions of 256 refer to; 0 if unknown
}

// maxFrameProbe is how far past the tag to look for the first frame, and
// the largest tag probed for one: reading further would restart the stream
// for a header that isn't worth it.
const maxFrameProbe = 4096
const maxProbedTag = 256 << 10

// probeMP3 reads the ID3v2 tag size and, after it, the first MPEG frame's
// Xing/Info header. It moves r; the caller seeks it afterwards. Whatever it
// can't find stays zero: without a tag start is 0, without a table toc is nil.
func probeMP3(r io.ReadSeeker) mp3Layout {
	var l mp3Layout
	var h [10]byte
	if _, err := io.ReadFull(r, h[:]); err == nil && string(h[:3]) == "ID3" {
		size := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
		l.start = 10 + size
		if h[5]&0x10 != 0 {
			l.start += 10 // a footer
		}
	}
	if l.start > maxProbedTag {
		return l
	}
	if _, err := r.Seek(l.start, io.SeekStart); err != nil {
		return l
	}
	buf := make([]byte, maxFrameProbe)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		if toc, size, ok := parseXing(buf[i:]); ok {
			l.start += int64(i)
			l.toc, l.bytes = toc, size
			return l
		}
		if mpegLayer3(buf[i:]) {
			l.start += int64(i) // the first frame, without a Xing header
			return l
		}
	}
	return l
}

// mpegLayer3 reports whether f starts with a valid Layer III frame header.
func mpegLayer3(f []byte) bool {
	version, layer, rate, bitrate := f[1]>>3&3, f[1]>>1&3, f[2]>>2&3, f[2]>>4
	return version != 1 && layer == 1 && rate != 3 && bitrate != 0 && bitrate != 15
}

// parseXing reads the Xing/Info header of the frame f starts with: its seek
// table (nil if the header has none) and the audio bytes it counts (0 if
// not said). ok is false if f isn't a frame with such a header.
func parseXing(f []byte) (toc []byte, size int64, ok bool) {
	if !mpegLayer3(f) {
		return nil, 0, false
	}
	mpeg1, mono := f[1]>>3&3 == 3, f[3]>>6 == 3
	side := 32 // the side info's length, by MPEG version and channel mode
	switch {
	case mpeg1 && mono:
		side = 17
	case !mpeg1 && mono:
		side = 9
	case !mpeg1:
		side = 17
	}
	at := 4 + side
	if f[1]&1 == 0 {
		at += 2 // a CRC after the header
	}
	if len(f) < at+8 || (string(f[at:at+4]) != "Xing" && string(f[at:at+4]) != "Info") {
		return nil, 0, false
	}
	be := func(b []byte) int64 { return int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3]) }
	flags := be(f[at+4:])
	at += 8
	if flags&1 != 0 { // the frame count
		at += 4
	}
	if flags&2 != 0 { // the audio bytes
		if len(f) >= at+4 {
			size = be(f[at:])
		}
		at += 4
	}
	if flags&4 != 0 && len(f) >= at+100 {
		toc = append([]byte(nil), f[at:at+100]...)
	}
	return toc, size, true
}

// offset estimates the byte where at falls in an MP3 of s.Size bytes lasting
// s.Duration. With a Xing table it follows the table, which knows the
// bitrate varies; without one, it is in proportion to time after the tag:
// exact for a constant bitrate.
func (l mp3Layout) offset(s subsonic.Song, at time.Duration) (int64, bool) {
	d := time.Duration(s.Duration) * time.Second
	if s.Size <= 0 || d <= 0 || l.start >= s.Size {
		return 0, false
	}
	audio := s.Size - l.start
	if l.toc != nil && l.bytes > 0 && l.bytes <= audio {
		audio = l.bytes
	}
	frac := min(max(float64(at)/float64(d), 0), 1)
	if l.toc != nil {
		p := frac * 100
		i := min(int(p), 99)
		lo, hi := float64(l.toc[i]), 256.0
		if i < 99 {
			hi = float64(l.toc[i+1])
		}
		frac = (lo + (hi-lo)*(p-float64(i))) / 256
	}
	return min(l.start+int64(float64(audio)*frac), s.Size-1), true
}
```

Then Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index 15fac2d..e22afff 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -114,6 +114,8 @@ type Engine struct {
 	busy    io.Closer // guarded by mu: source of the voice being decoded
 	killed  io.Closer // guarded by mu: source closed by interrupt, reason for failed open
 	seekReq *seekReq  // guarded by mu: latest requested seek, not yet run
+	// Replace calls whose command hasn't run yet, guarded by mu.
+	replacing int
 
 	// Owned by the run goroutine.
 	cur      *voice
@@ -173,6 +175,32 @@ func (e *Engine) Play(t Track) {
 	e.interrupt(t.Source)
 }
 
+// Replace swaps the current track for t like Play, but keeps the queued
+// successor: a seek that reopens the current song (an MP3 estimated by
+// byte, say) must not cost the gapless next one. prep, if not nil, runs
+// first and may move a stream the old track is still reading (to wake a
+// stalled read, say); the old track ending meanwhile doesn't start the
+// successor.
+func (e *Engine) Replace(t Track, prep func()) {
+	e.mu.Lock()
+	e.replacing++
+	e.mu.Unlock()
+	if prep != nil {
+		prep()
+	}
+	e.dropSeekReq()
+	if !e.send(func() { e.doReplace(t) }, t.Source) {
+		e.doneReplacing()
+	}
+	e.interrupt(t.Source)
+}
+
+func (e *Engine) doneReplacing() {
+	e.mu.Lock()
+	e.replacing--
+	e.mu.Unlock()
+}
+
 // QueueNext sets the track to continue with, gaplessly, after the current
 // one. It replaces any previously queued track. If the current track has
 // already finished decoding (or finished playing), the queued track starts
@@ -587,6 +615,12 @@ func (e *Engine) finishCur() {
 	closeVoice(e.cur)
 	e.cur = nil
 	e.setBusy(nil)
+	e.mu.Lock()
+	replacing := e.replacing > 0
+	e.mu.Unlock()
+	if replacing {
+		return // a Replace is on its way: the successor stays queued for after it
+	}
 	stops := e.stops
 	deadline := time.Now().Add(e.o.openWait)
 	for e.next == nil && e.opening != nil && e.cur == nil {
@@ -667,6 +701,18 @@ func (e *Engine) openVoice(t Track) (*voice, error) {
 
 func (e *Engine) doPlay(t Track) {
 	e.doStop()
+	e.startTrack(t, false)
+}
+
+func (e *Engine) doReplace(t Track) {
+	e.doneReplacing()
+	e.stopCurrent()
+	e.startTrack(t, true)
+}
+
+// startTrack opens t and makes it the current track. If it can't open and
+// keepNext, the queued successor starts instead.
+func (e *Engine) startTrack(t Track, keepNext bool) {
 	e.setBusy(t.Source)
 	if e.quitting() { // Close closes quit before it reads busy: if this misses quit, interrupt sees this source
 		e.setBusy(nil)
@@ -683,6 +729,13 @@ func (e *Engine) doPlay(t Track) {
 		e.mu.Unlock()
 		if !interrupted {
 			e.queueEvent(Event{Kind: EventError, TrackID: t.ID, Err: err})
+			if keepNext && e.next != nil {
+				v := e.next
+				e.next = nil
+				e.startVoice(v, false)
+			} else if keepNext && e.opening != nil {
+				e.ended = true // the successor starts as soon as it is open
+			}
 		}
 		return
 	}
@@ -690,12 +743,17 @@ func (e *Engine) doPlay(t Track) {
 }
 
 func (e *Engine) doStop() {
+	e.stopCurrent()
+	e.cancelNext()
+}
+
+// stopCurrent drops the current track and whatever is buffered of it.
+func (e *Engine) stopCurrent() {
 	e.stops++
 	closeVoice(e.cur)
 	e.cur = nil
 	e.ended = false
 	e.setBusy(nil)
-	e.cancelNext()
 	if e.rs != nil {
 		e.rs.Close()
 		e.rs = nil
diff --git a/internal/player/player.go b/internal/player/player.go
index 23f6b22..6076ed4 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -19,6 +19,9 @@ import (
 // Engine is the subset of *audio.Engine the player uses.
 type Engine interface {
 	Play(t audio.Track)
+	// Replace swaps the current track for t and keeps the queued next one.
+	// prep runs first (see audio.Engine.Replace).
+	Replace(t audio.Track, prep func())
 	QueueNext(t audio.Track)
 	ClearNext()
 	Stop()
@@ -480,7 +483,9 @@ func (p *Player) Seek(pos time.Duration) {
 			return
 		}
 		if seeksByReopening(p.curSrc, song) {
-			p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
+			if !p.retargetAt(song, pos) {
+				p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
+			}
 			return
 		}
 		// R16: fire and forget. A failure (e.g. ErrNotCurrent while the
@@ -741,6 +746,31 @@ func (p *Player) reopenAt(offset time.Duration) {
 	p.openAsync(id, song, offset, false)
 }
 
+// retargetAt seeks the current track within the stream it already has open,
+// if that holds the target: the engine swaps in a decoder on the same stream
+// and the queued successor stays. It reports false when the stream doesn't
+// hold it (or isn't one that can): the caller reopens.
+func (p *Player) retargetAt(song subsonic.Song, offset time.Duration) bool {
+	rt, ok := p.curSrc.Source.(retargeter)
+	if !ok {
+		return false
+	}
+	op, prep, ok := rt.retarget(song, offset)
+	if !ok {
+		return false
+	}
+	p.seq++
+	id := p.seq
+	p.curID, p.curSrc = id, op
+	p.position, p.lastPos = offset, offset
+	p.hasOpenSeek = false
+	if p.status != Paused {
+		p.setStatus(Loading)
+	}
+	p.o.Engine.Replace(p.track(id, song, op), prep)
+	return true
+}
+
 func (p *Player) openAsync(id uint64, song subsonic.Song, offset time.Duration, next bool) {
 	ctx := p.ctx
 	if ctx == nil {
diff --git a/internal/player/stream.go b/internal/player/stream.go
index 97eab6a..431f010 100644
--- a/internal/player/stream.go
+++ b/internal/player/stream.go
@@ -6,6 +6,7 @@ import (
 	"io"
 	"math"
 	"strings"
+	"sync/atomic"
 	"time"
 
 	"mistersubsonic/internal/audio"
@@ -81,12 +82,20 @@ func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
 			return Opened{}, err
 		}
 		var src io.ReadSeeker = r
-		if !transcoded && offset > 0 && f == audio.FormatMP3 {
+		if !transcoded && f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0 {
 			// An MP3 without a seek table seeks by decoding from the start,
 			// which takes seconds on the MiSTer: start near the target instead.
-			if base, ok := mp3Offset(r, s, offset); ok {
-				if _, err := r.Seek(base, io.SeekStart); err == nil {
-					src, start = &fromOffset{r: r, base: base}, offset
+			layout := probeMP3(r)
+			base, ok := int64(0), false
+			if offset > 0 {
+				base, ok = layout.offset(s, offset)
+			}
+			if _, err := r.Seek(base, io.SeekStart); err == nil && (ok || offset == 0) {
+				m := &mp3Stream{r: r, layout: layout}
+				m.refs.Store(1)
+				src = &fromOffset{s: m, base: base, placed: true}
+				if ok {
+					start = offset
 				}
 			} else {
 				r.Seek(0, io.SeekStart)
@@ -96,50 +105,87 @@ func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
 	}
 }
 
-// mp3Offset estimates where offset falls in the MP3 r of s.Size bytes lasting
-// s.Duration: after the ID3v2 tag, in proportion to time. That is exact for
-// a constant bitrate and close for a variable one. It reads the first bytes
-// of r to find the tag.
-func mp3Offset(r io.Reader, s subsonic.Song, offset time.Duration) (int64, bool) {
-	d := time.Duration(s.Duration) * time.Second
-	if s.Size <= 0 || d <= 0 {
-		return 0, false
-	}
-	var h [10]byte
-	tag := int64(0)
-	if _, err := io.ReadFull(r, h[:]); err == nil && string(h[:3]) == "ID3" {
-		size := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
-		tag = 10 + size
-		if h[5]&0x10 != 0 {
-			tag += 10 // a footer
+// mp3Stream is an MP3's stream shared by the sources of successive seeks:
+// it closes with the last of them.
+type mp3Stream struct {
+	r      *stream.Reader
+	layout mp3Layout
+	refs   atomic.Int32
+}
+
+// fromOffset shows an mp3Stream from byte base on as if it began there, so
+// the decoder starts at the estimated seek point (MP3 frames resync on their
+// own).
+type fromOffset struct {
+	s      *mp3Stream
+	base   int64
+	placed bool // the stream is at base or beyond; only the engine's goroutine touches it
+	closed atomic.Bool
+}
+
+// place moves a source made by retarget to its base on first use, when the
+// source before it has been dropped and no longer reads.
+func (f *fromOffset) place() error {
+	if f.closed.Load() {
+		return stream.ErrClosed
+	}
+	if !f.placed {
+		f.placed = true
+		if !f.s.r.SeekIfBuffered(f.base) {
+			_, err := f.s.r.Seek(f.base, io.SeekStart)
+			return err
 		}
 	}
-	if tag >= s.Size {
-		return 0, false
+	return nil
+}
+
+func (f *fromOffset) Read(p []byte) (int, error) {
+	if err := f.place(); err != nil {
+		return 0, err
 	}
-	off := tag + int64(float64(s.Size-tag)*float64(offset)/float64(d))
-	return min(off, s.Size-1), true
+	return f.s.r.Read(p)
 }
 
-// fromOffset shows a stream from byte base on as if it began there, so the
-// decoder starts at the estimated seek point (MP3 frames resync on their own).
-type fromOffset struct {
-	r    *stream.Reader
-	base int64
+func (f *fromOffset) Close() error {
+	if f.closed.CompareAndSwap(false, true) && f.s.refs.Add(-1) == 0 {
+		return f.s.r.Close()
+	}
+	return nil
 }
 
-func (f *fromOffset) Read(p []byte) (int, error) { return f.r.Read(p) }
-func (f *fromOffset) Close() error               { return f.r.Close() }
-func (f *fromOffset) Promote()                   { f.r.Promote() }
+func (f *fromOffset) Promote() { f.s.r.Promote() }
 
 func (f *fromOffset) Seek(off int64, whence int) (int64, error) {
+	if err := f.place(); err != nil {
+		return 0, err
+	}
 	if whence == io.SeekStart {
 		off += f.base
 	}
-	abs, err := f.r.Seek(off, whence)
+	abs, err := f.s.r.Seek(off, whence)
 	return abs - f.base, err
 }
 
+// retargeter is a source that can move to another position of its stream
+// without opening it again.
+type retargeter interface {
+	// retarget returns the song at position at as a new Opened over the same
+	// stream, if the stream holds that byte. The old source stays valid until
+	// closed. prep, run right before the engine swaps the two, moves the
+	// stream so a read of the old source that is waiting for data wakes.
+	retarget(s subsonic.Song, at time.Duration) (o Opened, prep func(), ok bool)
+}
+
+func (f *fromOffset) retarget(s subsonic.Song, at time.Duration) (Opened, func(), bool) {
+	base, ok := f.s.layout.offset(s, at)
+	if !ok || !f.s.r.Holds(base) {
+		return Opened{}, nil, false
+	}
+	f.s.refs.Add(1)
+	return Opened{Source: &fromOffset{s: f.s, base: base}, Format: audio.FormatMP3, Offset: at},
+		func() { f.s.r.SeekIfBuffered(base) }, true
+}
+
 // ReplayGainFactor converts the song's ReplayGain to a linear factor for
 // mode "track" or "album", falling back to the other value, and limits it
 // so the known peak doesn't clip. No data or mode "off" gives 1.
diff --git a/internal/stream/reader.go b/internal/stream/reader.go
index 3468f96..8557f37 100644
--- a/internal/stream/reader.go
+++ b/internal/stream/reader.go
@@ -162,6 +162,28 @@ func (r *Reader) Buffered() int64 {
 	return r.hi - r.pos
 }
 
+// Holds reports whether byte off is in memory, so that seeking there starts
+// no request (unless the window moves on first).
+func (r *Reader) Holds(off int64) bool {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return !r.closed && off >= r.lo && off < r.hi
+}
+
+// SeekIfBuffered moves the read position to off if that byte is in memory,
+// so it never starts a request, and reports whether it did. A Read waiting
+// for data wakes and continues from there.
+func (r *Reader) SeekIfBuffered(off int64) bool {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	if r.closed || off < r.lo || off >= r.hi {
+		return false
+	}
+	r.pos = off
+	r.cond.Broadcast()
+	return true
+}
+
 // Promote lifts the prefetch limit and grows the ring to the full window.
 func (r *Reader) Promote() {
 	r.mu.Lock()
@@ -492,6 +514,10 @@ func (r *Reader) copyBody(gen int, body io.Reader, reqCancel context.CancelFunc,
 			r.mu.Unlock()
 			return progressed, errStale
 		}
+		// The read below may overwrite everything under effectiveLo, so
+		// those bytes are gone from now on: Holds and a seek back there must
+		// not count on them while the lock is released.
+		r.lo = r.effectiveLo()
 		room := r.room()
 		r.mu.Unlock()
 
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/audio ./internal/player ./internal/stream`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9" go vet ./...` with zig on `PATH`. Expected: no output, which means the ARM build of the new code vets clean. `internal/audio` changed, so also run `make mister mister-test`. Expected: the glibc lines end in `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/engine.go internal/audio/engine_test.go internal/player/fakes_test.go internal/player/mp3.go internal/player/mp3_test.go internal/player/opener_test.go internal/player/player.go internal/player/player_test.go internal/player/stream.go internal/stream/reader.go internal/stream/reader_test.go
git commit -m "audio, player, stream: an MP3 seek inside the buffer keeps the stream and the next track; VBR seeks use the Xing table" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Stream and player hardening

**Files:**
- Modify: `internal/stream/reader.go`, `internal/player/stream.go`
- Test: `internal/stream/reader_test.go`, `internal/player/opener_test.go` (modified)

**Interfaces:**
- **Consumes:** `probeMP3`, `mp3Layout` and `fromOffset` (Task 3).
- **Produces:**
  - **`Promote`:**
    - On a closed reader it does nothing.
    - Otherwise it sets `prefetch = 0` and unlocks. It allocates the window through `r.alloc` (a `Reader` field that defaults to `make`, and is a test hook). Then it locks again and copies the buffered bytes in: `growLocked(ring)` checks again for closed or already grown.
  - **Retry-After.** `maxRetryAfter = 24 * time.Hour`. `parseRetryAfter` clamps huge numbers, `ErrRange` and far dates.
  - **The latest size.** `fetch` stores the size of every successful response of the current generation, so the 416 and short-EOF checks use the latest size. A file that shrank and then ends cleanly ends at its new size.
  - **Open.** `budgetedRequest(ctx, limit)` bounds each Open request by the retry budget with a timer. When the budget cuts a request short after a "come back later" answer, Open returns that answer.
  - **`fromOffset.Seek`** refuses any seek that lands before its base, for all three whences, and moves nothing.
  - **`positionMP3(r, song, offset) (layout, base, at, placed, err)`:**
    - It holds the MP3 opening logic.
    - The fallback rewinds to 0.
    - A failed rewind is now an error: the opener closes the reader and fails.
  - **New tests for paths that already worked:**
    - `TestOpenBudgetRunsOut`, `TestOpenCancelDuringTheRetryWait`, `TestOpenFailsAtOnceOnServerError`, `TestAbsurdRetryAfterGivesUp`;
    - `TestPositionMP3FallsBackToTheStart`.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/player/opener_test.go b/internal/player/opener_test.go
index 83373d1..6537402 100644
--- a/internal/player/opener_test.go
+++ b/internal/player/opener_test.go
@@ -219,3 +219,98 @@ func TestMP3StreamRetargetsWithinItsWindow(t *testing.T) {
 		t.Fatal("the stream stayed open after both sources closed")
 	}
 }
+
+// flakySeeker is a ReadSeeker whose seeks can be made to fail.
+type flakySeeker struct {
+	*bytes.Reader
+	fail func(off int64) bool
+}
+
+func (f *flakySeeker) Seek(off int64, whence int) (int64, error) {
+	if whence == io.SeekStart && f.fail(off) {
+		return 0, errors.New("seek refused")
+	}
+	return f.Reader.Seek(off, whence)
+}
+
+func tonemp3(t *testing.T) []byte {
+	t.Helper()
+	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3")
+	if err != nil {
+		t.Fatal(err)
+	}
+	return mp3
+}
+
+// When the estimated start can't be sought to, the stream is back at its
+// first byte, not left wherever the header probe stopped.
+func TestPositionMP3FallsBackToTheStart(t *testing.T) {
+	mp3 := tonemp3(t)
+	song := subsonic.Song{Size: int64(len(mp3)), Duration: 2}
+	for name, c := range map[string]struct {
+		song   subsonic.Song
+		offset time.Duration
+		fail   func(int64) bool
+	}{
+		"estimate seek fails": {song, time.Second, func(off int64) bool { return off > 100 }},
+		"no size to estimate": {subsonic.Song{Duration: 2}, time.Second, func(int64) bool { return false }},
+	} {
+		r := &flakySeeker{Reader: bytes.NewReader(mp3), fail: c.fail}
+		_, _, at, placed, err := positionMP3(r, c.song, c.offset)
+		if err != nil || placed || at != 0 {
+			t.Fatalf("%s: placed %v at %v err %v", name, placed, at, err)
+		}
+		if pos, _ := r.Reader.Seek(0, io.SeekCurrent); pos != 0 {
+			t.Errorf("%s: stream left at byte %d, want 0", name, pos)
+		}
+	}
+}
+
+// If even the rewind fails the stream can't be used: an error, not a source
+// that reads from the middle of the header.
+func TestPositionMP3ReportsAFailedRewind(t *testing.T) {
+	mp3 := tonemp3(t)
+	r := &flakySeeker{Reader: bytes.NewReader(mp3), fail: func(off int64) bool { return off > 100 || off == 0 }}
+	song := subsonic.Song{Size: int64(len(mp3)), Duration: 2}
+	if _, _, _, placed, err := positionMP3(r, song, time.Second); err == nil || placed {
+		t.Fatalf("placed %v, err %v; want an error", placed, err)
+	}
+}
+
+// A source that starts at byte base can't be seeked before it.
+func TestFromOffsetRejectsSeeksBeforeItsBase(t *testing.T) {
+	mp3 := tonemp3(t)
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
+	}))
+	defer srv.Close()
+	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
+	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
+	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), song, time.Second, false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer closeOpened(op)
+	src := op.Source
+	base := op.Source.(*fromOffset).base
+	head := make([]byte, 8)
+	io.ReadFull(src, head) // now 8 bytes in
+	for name, seek := range map[string]func() (int64, error){
+		"start":   func() (int64, error) { return src.Seek(-1, io.SeekStart) },
+		"current": func() (int64, error) { return src.Seek(-9, io.SeekCurrent) },
+		"end":     func() (int64, error) { return src.Seek(-int64(len(mp3))+base-1, io.SeekEnd) },
+	} {
+		if _, err := seek(); err == nil {
+			t.Errorf("seek before the base from %s succeeded", name)
+		}
+		if pos, _ := src.Seek(0, io.SeekCurrent); pos != 8 {
+			t.Fatalf("after the refused seek from %s the position is %d, want 8", name, pos)
+		}
+	}
+	if pos, err := src.Seek(-8, io.SeekCurrent); err != nil || pos != 0 {
+		t.Fatalf("seek to the base = %d, %v", pos, err)
+	}
+	if pos, err := src.Seek(-int64(len(mp3))+base, io.SeekEnd); err != nil || pos != 0 {
+		t.Fatalf("seek to the base from the end = %d, %v", pos, err)
+	}
+}
diff --git a/internal/stream/reader_test.go b/internal/stream/reader_test.go
index 27e9f3a..3945e70 100644
--- a/internal/stream/reader_test.go
+++ b/internal/stream/reader_test.go
@@ -848,3 +848,195 @@ func TestOpenRetryAfterBeyondTheDeadlineReportsTheServer(t *testing.T) {
 		t.Fatalf("Open took %v", d)
 	}
 }
+
+// A huge Retry-After is clamped, not overflowed into a negative or tiny wait.
+func TestParseRetryAfterClampsAbsurdValues(t *testing.T) {
+	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
+	for _, h := range []string{"99999999999", "9223372036854775807", "99999999999999999999", "Fri, 31 Dec 9999 23:59:59 GMT"} {
+		if got := parseRetryAfter(h, now); got != maxRetryAfter {
+			t.Errorf("parseRetryAfter(%q) = %v, want the clamp %v", h, got, maxRetryAfter)
+		}
+	}
+}
+
+// An absurd Retry-After still gives up at once (it is beyond the budget).
+func TestAbsurdRetryAfterGivesUp(t *testing.T) {
+	const size = 1 << 20
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n == 1 {
+			dropAfter(w, size, 100<<10)
+		}
+		w.Header().Set("Retry-After", "99999999999")
+		w.WriteHeader(http.StatusTooManyRequests)
+		return true
+	})
+	r := open(t, s.URL, testOptions())
+	start := time.Now()
+	if _, err := io.ReadAll(r); err == nil {
+		t.Fatal("ReadAll succeeded")
+	}
+	if d := time.Since(start); d > time.Second {
+		t.Fatalf("gave up after %v", d)
+	}
+}
+
+// Promote on a closed reader allocates nothing and changes nothing.
+func TestPromoteOnAClosedReaderIsANoOp(t *testing.T) {
+	s := newServer(t, 8<<20, nil)
+	o := testOptions()
+	o.PrefetchBytes = 64 << 10
+	r := open(t, s.URL, o)
+	r.Close()
+	r.alloc = func(int64) []byte { t.Error("Promote allocated on a closed reader"); return nil }
+	r.Promote()
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	if len(r.ring) != 80<<10 || r.prefetch == 0 {
+		t.Fatalf("ring %d, prefetch %d: a closed reader changed", len(r.ring), r.prefetch)
+	}
+}
+
+// The ring is allocated without the lock, so a Read during the allocation
+// isn't stalled by it; a Close that lands meanwhile keeps the old ring.
+func TestPromoteAllocatesOutsideTheLock(t *testing.T) {
+	s := newServer(t, 8<<20, nil)
+	o := testOptions()
+	o.PrefetchBytes = 64 << 10
+	r := open(t, s.URL, o)
+	buf := make([]byte, 10<<10)
+	if _, err := io.ReadFull(r, buf); err != nil {
+		t.Fatal(err)
+	}
+	locked := make(chan bool, 1)
+	r.alloc = func(n int64) []byte {
+		if r.mu.TryLock() {
+			r.mu.Unlock()
+			locked <- false
+		} else {
+			locked <- true
+		}
+		// A Read now must go through, not wait for this allocation.
+		done := make(chan struct{})
+		go func() { r.Read(buf); close(done) }()
+		select {
+		case <-done:
+		case <-time.After(2 * time.Second):
+			t.Error("Read blocked during the allocation")
+		}
+		return make([]byte, n)
+	}
+	r.Promote()
+	select {
+	case held := <-locked:
+		if held {
+			t.Fatal("the lock was held during the allocation")
+		}
+	default:
+		t.Fatal("Promote never called the allocation hook")
+	}
+	r.mu.Lock()
+	big := len(r.ring)
+	r.mu.Unlock()
+	if big != 1<<20 {
+		t.Fatalf("promoted ring %d bytes", big)
+	}
+	checkBytes(t, readAt(t, r, 0, 64<<10), 0)
+}
+
+// A file that is shorter on reconnect ends at its new size: the 416-versus-
+// size check goes by the latest response, not the first.
+func TestShrunkFileEndsAtTheNewSize(t *testing.T) {
+	const size, shrunk = 1 << 20, 300 << 10
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n == 1 {
+			dropAfter(w, size, 100<<10)
+		}
+		http.ServeContent(w, r, "", time.Time{}, &virtualFile{size: shrunk})
+		return true
+	})
+	r := open(t, s.URL, testOptions())
+	got, err := io.ReadAll(r)
+	if err != nil || len(got) != shrunk {
+		t.Fatalf("ReadAll = %d bytes, %v; want %d bytes", len(got), err, shrunk)
+	}
+	checkBytes(t, got, 0)
+}
+
+// Open's whole retry, waits and requests alike, ends with the budget.
+func TestOpenBudgetRunsOut(t *testing.T) {
+	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		w.WriteHeader(http.StatusServiceUnavailable)
+		return true
+	})
+	o := testOptions()
+	o.RetryBudget = 100 * time.Millisecond
+	start := time.Now()
+	_, err := Open(context.Background(), s.URL, o)
+	var he *HTTPError
+	if !errors.As(err, &he) || he.StatusCode != http.StatusServiceUnavailable {
+		t.Fatalf("Open error = %v, want the 503", err)
+	}
+	if d := time.Since(start); d > time.Second || s.requests.Load() < 2 {
+		t.Fatalf("Open took %v over %d requests", d, s.requests.Load())
+	}
+}
+
+// A retry request that hangs is cut off by the budget, not the stall timeout.
+func TestOpenBudgetBoundsTheRetryRequest(t *testing.T) {
+	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n == 1 {
+			w.WriteHeader(http.StatusServiceUnavailable)
+			return true
+		}
+		<-r.Context().Done() // never answers
+		return true
+	})
+	o := testOptions()
+	o.StallTimeout = 10 * time.Second
+	o.RetryBudget = 300 * time.Millisecond
+	start := time.Now()
+	_, err := Open(context.Background(), s.URL, o)
+	if err == nil {
+		t.Fatal("Open succeeded")
+	}
+	if d := time.Since(start); d > 2*time.Second {
+		t.Fatalf("Open took %v, the budget is 300ms", d)
+	}
+}
+
+// A cancelled context ends Open's wait between retries.
+func TestOpenCancelDuringTheRetryWait(t *testing.T) {
+	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		w.WriteHeader(http.StatusServiceUnavailable)
+		return true
+	})
+	o := testOptions()
+	o.Backoff = []time.Duration{10 * time.Second}
+	o.RetryBudget = time.Minute
+	ctx, cancel := context.WithCancel(context.Background())
+	time.AfterFunc(100*time.Millisecond, cancel)
+	start := time.Now()
+	_, err := Open(ctx, s.URL, o)
+	if !errors.Is(err, context.Canceled) {
+		t.Fatalf("Open error = %v, want context.Canceled", err)
+	}
+	if d := time.Since(start); d > time.Second {
+		t.Fatalf("Open took %v after the cancel", d)
+	}
+}
+
+// A 500 on the first request is not retried.
+func TestOpenFailsAtOnceOnServerError(t *testing.T) {
+	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		w.WriteHeader(http.StatusInternalServerError)
+		return true
+	})
+	_, err := Open(context.Background(), s.URL, testOptions())
+	var he *HTTPError
+	if !errors.As(err, &he) || he.StatusCode != http.StatusInternalServerError {
+		t.Fatalf("Open error = %v, want the 500", err)
+	}
+	if n := s.requests.Load(); n != 1 {
+		t.Fatalf("%d requests, want 1", n)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/player ./internal/stream`

Expected: FAIL, e.g.:

```
undefined: maxRetryAfter
r.alloc undefined (type *Reader has no field or method alloc)
undefined: positionMP3
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/player/stream.go b/internal/player/stream.go
index 431f010..8979f4f 100644
--- a/internal/player/stream.go
+++ b/internal/player/stream.go
@@ -2,6 +2,7 @@ package player
 
 import (
 	"context"
+	"errors"
 	"fmt"
 	"io"
 	"math"
@@ -85,26 +86,44 @@ func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
 		if !transcoded && f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0 {
 			// An MP3 without a seek table seeks by decoding from the start,
 			// which takes seconds on the MiSTer: start near the target instead.
-			layout := probeMP3(r)
-			base, ok := int64(0), false
-			if offset > 0 {
-				base, ok = layout.offset(s, offset)
+			layout, base, at, placed, err := positionMP3(r, s, offset)
+			if err != nil {
+				r.Close()
+				return Opened{}, err
 			}
-			if _, err := r.Seek(base, io.SeekStart); err == nil && (ok || offset == 0) {
+			if placed {
 				m := &mp3Stream{r: r, layout: layout}
 				m.refs.Store(1)
 				src = &fromOffset{s: m, base: base, placed: true}
-				if ok {
-					start = offset
-				}
-			} else {
-				r.Seek(0, io.SeekStart)
+				start = at
 			}
 		}
 		return Opened{Source: src, Format: f, Transcoded: transcoded, Offset: start}, nil
 	}
 }
 
+// positionMP3 probes r, an MP3 of song s, and moves it to the byte estimated
+// for offset (the start for offset 0). placed says the estimate is in use: r
+// is at base and the song's position there is at. Otherwise r is back at its
+// start, for the engine to seek; err is set only when r can't be put there.
+func positionMP3(r io.ReadSeeker, s subsonic.Song, offset time.Duration) (layout mp3Layout, base int64, at time.Duration, placed bool, err error) {
+	layout = probeMP3(r)
+	ok := false
+	if offset > 0 {
+		base, ok = layout.offset(s, offset)
+	}
+	if _, err := r.Seek(base, io.SeekStart); err == nil && (ok || offset == 0) {
+		if ok {
+			at = offset
+		}
+		return layout, base, at, true, nil
+	}
+	if _, err := r.Seek(0, io.SeekStart); err != nil {
+		return layout, 0, 0, false, err
+	}
+	return layout, 0, 0, false, nil
+}
+
 // mp3Stream is an MP3's stream shared by the sources of successive seeks:
 // it closes with the last of them.
 type mp3Stream struct {
@@ -159,8 +178,24 @@ func (f *fromOffset) Seek(off int64, whence int) (int64, error) {
 	if err := f.place(); err != nil {
 		return 0, err
 	}
-	if whence == io.SeekStart {
+	// The source begins at base: a seek before it is refused, and refused
+	// before anything moves.
+	var target int64
+	switch whence {
+	case io.SeekStart:
+		target = off
 		off += f.base
+	case io.SeekCurrent:
+		cur, err := f.s.r.Seek(0, io.SeekCurrent)
+		if err != nil {
+			return 0, err
+		}
+		target = cur - f.base + off
+	case io.SeekEnd:
+		target = f.s.r.Size() + off - f.base // an unknown size, -1, is refused here too
+	}
+	if target < 0 {
+		return 0, errors.New("player: seek before the start of the stream")
 	}
 	abs, err := f.s.r.Seek(off, whence)
 	return abs - f.base, err
diff --git a/internal/stream/reader.go b/internal/stream/reader.go
index 8557f37..df9b613 100644
--- a/internal/stream/reader.go
+++ b/internal/stream/reader.go
@@ -60,17 +60,23 @@ func retryLater(code int) bool {
 	return false
 }
 
+// maxRetryAfter caps a Retry-After: far beyond any retry budget, and far
+// from overflowing a Duration.
+const maxRetryAfter = 24 * time.Hour
+
 // parseRetryAfter reads a Retry-After header: seconds or an HTTP date.
 func parseRetryAfter(h string, now time.Time) time.Duration {
 	h = strings.TrimSpace(h)
 	if h == "" {
 		return 0
 	}
-	if s, err := strconv.Atoi(h); err == nil && s >= 0 {
-		return time.Duration(s) * time.Second
+	if s, err := strconv.ParseInt(h, 10, 64); err == nil && s >= 0 {
+		return time.Duration(min(s, int64(maxRetryAfter/time.Second))) * time.Second
+	} else if errors.Is(err, strconv.ErrRange) && h[0] != '-' {
+		return maxRetryAfter
 	}
 	if t, err := http.ParseTime(h); err == nil && t.After(now) {
-		return t.Sub(now)
+		return min(t.Sub(now), maxRetryAfter)
 	}
 	return 0
 }
@@ -92,6 +98,7 @@ type Reader struct {
 	prefetch int64
 	gen      int
 	cancel   context.CancelFunc
+	alloc    func(n int64) []byte // makes the ring Promote grows to; a test hook
 }
 
 // Open issues the first request and returns once response headers arrive.
@@ -124,7 +131,8 @@ func Open(ctx context.Context, url string, o Options) (*Reader, error) {
 		// full window comes with Promote.
 		ringBytes = min(o.WindowBytes, o.PrefetchBytes+o.PrefetchBytes/4)
 	}
-	r := &Reader{url: url, o: o, ring: make([]byte, ringBytes), size: -1, prefetch: o.PrefetchBytes}
+	r := &Reader{url: url, o: o, ring: make([]byte, ringBytes), size: -1, prefetch: o.PrefetchBytes,
+		alloc: func(n int64) []byte { return make([]byte, n) }}
 	r.cond = sync.NewCond(&r.mu)
 
 	fctx, cancel := context.WithCancel(context.Background())
@@ -185,19 +193,34 @@ func (r *Reader) SeekIfBuffered(off int64) bool {
 }
 
 // Promote lifts the prefetch limit and grows the ring to the full window.
+// The new ring is allocated without the lock (a Read meanwhile isn't held up
+// by up to 32 MiB of zeroing); what the fetcher added during that is copied
+// over under it.
 func (r *Reader) Promote() {
 	r.mu.Lock()
-	r.prefetch = 0
-	if int64(len(r.ring)) < r.o.WindowBytes {
-		r.growLocked(r.o.WindowBytes)
+	if r.closed {
+		r.mu.Unlock()
+		return
 	}
+	r.prefetch = 0
 	r.cond.Broadcast()
+	grow := int64(len(r.ring)) < r.o.WindowBytes
+	r.mu.Unlock()
+	if !grow {
+		return
+	}
+	ring := r.alloc(r.o.WindowBytes)
+	r.mu.Lock()
+	if !r.closed && int64(len(r.ring)) < int64(len(ring)) {
+		r.growLocked(ring)
+		r.cond.Broadcast()
+	}
 	r.mu.Unlock()
 }
 
-// growLocked moves the buffered bytes [lo, hi) into a new ring of n bytes.
-func (r *Reader) growLocked(n int64) {
-	ring := make([]byte, n)
+// growLocked moves the buffered bytes [lo, hi) into ring, which is larger.
+func (r *Reader) growLocked(ring []byte) {
+	n := int64(len(ring))
 	old := int64(len(r.ring))
 	for off := r.lo; off < r.hi; {
 		s := off % old
@@ -366,12 +389,17 @@ func stripURL(err error) error {
 // not taken: the server's answer is returned instead.
 func (r *Reader) openRequest(ctx context.Context, deadline time.Time) (*http.Response, context.CancelFunc, error) {
 	start := time.Now()
+	var last error // the latest "come back later"
 	for attempt := 0; ; attempt++ {
-		resp, reqCancel, err := r.request(ctx, 0)
+		resp, reqCancel, err := r.budgetedRequest(ctx, start.Add(r.o.RetryBudget))
 		var he *HTTPError
 		if err == nil || !errors.As(err, &he) || !retryLater(he.StatusCode) {
+			if err != nil && last != nil && time.Since(start) >= r.o.RetryBudget {
+				err = last // the budget cut this request short: the server's answer says more
+			}
 			return resp, reqCancel, err
 		}
+		last = err
 		d := max(r.o.Backoff[min(attempt, len(r.o.Backoff)-1)], he.RetryAfter)
 		if time.Since(start)+d > r.o.RetryBudget {
 			return nil, nil, err
@@ -389,6 +417,30 @@ func (r *Reader) openRequest(ctx context.Context, deadline time.Time) (*http.Res
 	}
 }
 
+// budgetedRequest is request, cut off at limit: Open's retries end with the
+// budget even when a request hangs.
+func (r *Reader) budgetedRequest(ctx context.Context, limit time.Time) (*http.Response, context.CancelFunc, error) {
+	lctx, lcancel := context.WithCancel(ctx)
+	t := time.AfterFunc(time.Until(limit), lcancel)
+	resp, reqCancel, err := r.request(lctx, 0)
+	if !t.Stop() { // the limit passed: whatever came back is too late
+		if err == nil {
+			resp.Body.Close()
+			reqCancel()
+			err = context.DeadlineExceeded
+		}
+		lcancel()
+		return nil, nil, err
+	}
+	if err != nil {
+		lcancel()
+		return nil, nil, err
+	}
+	// The body outlives this call, so the limit's timer is already stopped;
+	// the caller's cancel ends the request.
+	return resp, func() { reqCancel(); lcancel() }, nil
+}
+
 func (r *Reader) request(ctx context.Context, off int64) (*http.Response, context.CancelFunc, error) {
 	rctx, cancel := context.WithCancel(ctx)
 	req, err := http.NewRequestWithContext(rctx, http.MethodGet, r.url, nil)
@@ -482,6 +534,15 @@ func (r *Reader) fetch(ctx context.Context, gen int, off int64, resp *http.Respo
 				}
 				continue
 			}
+			// The file may have changed since the last connection: the
+			// latest size is what a later 416 and the end are judged by.
+			if n := responseSize(resp); n >= 0 {
+				r.mu.Lock()
+				if r.gen == gen {
+					r.size = n
+				}
+				r.mu.Unlock()
+			}
 		}
 		skip := int64(0)
 		if resp.StatusCode == http.StatusOK {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/player ./internal/stream`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/player/opener_test.go internal/player/stream.go internal/stream/reader.go internal/stream/reader_test.go
git commit -m "stream, player: Promote allocates outside the lock, bounded Retry-After and Open, the latest size, no seeks before a view's base" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: Full resolution and input robustness

**Files:**
- Modify: `internal/platform/fbmode.go`, `cmd/mistersubsonic/fullres.go`, `cmd/mistersubsonic/main.go`, `internal/input/evdev_linux.go`
- Test: `internal/platform/fbmode_test.go`, `cmd/mistersubsonic/fullres_test.go`, `internal/input/evdev_linux_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **`request()`** fails at once when `ResCount()` is empty, before sending anything.
  - **The state file** holds `fromW fromH toW toH`.
    - `Restore()` is `restore(true)`. With a four-number state, and `Current()` readable and not equal to "to", it forgets the state and sends no command.
    - The old two-number state is restored as before.
    - When `Current()` can't be read, it restores.
    - The state is removed only on success, on a stale state, or on a bad one. A failed request keeps it.
  - **`RestoreAlways()`** is `restore(false)`, with no stale check. openFB's "asked for X, got Y" path uses it.
  - **`restoreOnForcedExit()`** in `cmd/mistersubsonic` runs `fbControl().Restore()`, then `restoreText()`. `forceExit` calls it. The console is still in graphics mode there, as a comment explains.
  - **Test helper.** The fullres tests get a fake menu (`answeringMenu`), because a `/sys` directory without `res_count` now fails fast.
  - **`internal/input`:**
    - `Manager.Close` clears `pads`.
    - `isGamepad` logs a failed EVIOCGBIT, once per opened device.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/cmd/mistersubsonic/fullres_test.go b/cmd/mistersubsonic/fullres_test.go
index d9a65d1..26f34ea 100644
--- a/cmd/mistersubsonic/fullres_test.go
+++ b/cmd/mistersubsonic/fullres_test.go
@@ -37,9 +37,10 @@ func TestFullSize(t *testing.T) {
 // puts back the framebuffer size the app saved.
 func TestRestoreConsoleRestoresTheFramebuffer(t *testing.T) {
 	dir := t.TempDir()
-	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
+	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
 	os.WriteFile(c.Cmd, nil, 0o644)
 	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
+	answeringMenu(t, c)
 	old := fbControl
 	fbControl = func() platform.FBControl { return c }
 	defer func() { fbControl = old }()
@@ -53,6 +54,28 @@ func TestRestoreConsoleRestoresTheFramebuffer(t *testing.T) {
 	}
 }
 
+// answeringMenu stands in for Main_MiSTer: res_count goes up when a command
+// arrives in the command file.
+func answeringMenu(t *testing.T, c platform.FBControl) {
+	t.Helper()
+	os.WriteFile(filepath.Join(c.Sys, "res_count"), []byte("1\n"), 0o644)
+	done := make(chan struct{})
+	t.Cleanup(func() { close(done) })
+	go func() {
+		for {
+			select {
+			case <-done:
+				return
+			case <-time.After(5 * time.Millisecond):
+			}
+			if b, _ := os.ReadFile(c.Cmd); len(b) > 0 {
+				os.WriteFile(filepath.Join(c.Sys, "res_count"), []byte("2\n"), 0o644)
+				return
+			}
+		}
+	}()
+}
+
 // fakeConsole replaces the console calls for the test; g and r run when the
 // app enters graphics mode and returns to text mode.
 func fakeConsole(t *testing.T, g, r func()) {
@@ -77,9 +100,10 @@ func fakeConsole(t *testing.T, g, r func()) {
 // fbcon redraws its text into the new framebuffer and crashes the kernel).
 func TestRestoreConsoleKeepsGraphicsModeAroundTheSizeChange(t *testing.T) {
 	dir := t.TempDir()
-	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
+	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
 	os.WriteFile(c.Cmd, nil, 0o644)
 	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
+	answeringMenu(t, c)
 	old := fbControl
 	fbControl = func() platform.FBControl { return c }
 	defer func() { fbControl = old }()
@@ -105,7 +129,7 @@ func TestRestoreConsoleKeepsGraphicsModeAroundTheSizeChange(t *testing.T) {
 
 func TestRestoreConsoleWithoutSavedSizeOnlyRestoresText(t *testing.T) {
 	dir := t.TempDir()
-	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
+	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
 	old := fbControl
 	fbControl = func() platform.FBControl { return c }
 	defer func() { fbControl = old }()
@@ -117,6 +141,36 @@ func TestRestoreConsoleWithoutSavedSizeOnlyRestoresText(t *testing.T) {
 	}
 }
 
+// The shutdown deadline puts the size back too, then the text mode. The
+// console is still in graphics mode then (the app entered it at start and
+// only the deferred exit path leaves it), which the size change needs.
+func TestForceExitRestoresTheSizeThenText(t *testing.T) {
+	dir := t.TempDir()
+	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 500 * time.Millisecond}
+	os.WriteFile(c.Cmd, nil, 0o644)
+	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
+	answeringMenu(t, c)
+	os.WriteFile(filepath.Join(dir, "width"), []byte("1920\n"), 0o644)
+	os.WriteFile(filepath.Join(dir, "height"), []byte("1200\n"), 0o644)
+	old := fbControl
+	fbControl = func() platform.FBControl { return c }
+	defer func() { fbControl = old }()
+	text := false
+	fakeConsole(t, nil, func() {
+		if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
+			t.Errorf("text mode before the size was restored: %q", b)
+		}
+		text = true
+	})
+	restoreOnForcedExit()
+	if !text {
+		t.Fatal("text mode not restored")
+	}
+	if c.Saved() {
+		t.Fatal("the saved size is still there")
+	}
+}
+
 func TestAllowFullRes(t *testing.T) {
 	con := &platform.Console{}
 	if !allowFullRes(true, con) {
diff --git a/internal/input/evdev_linux_test.go b/internal/input/evdev_linux_test.go
index 1e15fbc..417b838 100644
--- a/internal/input/evdev_linux_test.go
+++ b/internal/input/evdev_linux_test.go
@@ -95,6 +95,21 @@ func TestCloseTwiceAndAttachAfterClose(t *testing.T) {
 	}
 }
 
+// Close forgets the gamepads too: nothing is connected any more.
+func TestCloseForgetsThePads(t *testing.T) {
+	m := newIdleManager(time.Hour)
+	m.mu.Lock()
+	m.pads["event3"] = true
+	m.mu.Unlock()
+	if !m.HasPad() {
+		t.Fatal("no pad before Close")
+	}
+	m.Close()
+	if m.HasPad() {
+		t.Fatal("HasPad still true after Close")
+	}
+}
+
 func TestScanPrunesStalePlaceholders(t *testing.T) {
 	dir := t.TempDir()
 	p := filepath.Join(dir, "event0")
diff --git a/internal/platform/fbmode_test.go b/internal/platform/fbmode_test.go
index 7dab043..3a7008e 100644
--- a/internal/platform/fbmode_test.go
+++ b/internal/platform/fbmode_test.go
@@ -96,7 +96,7 @@ func TestSwitchAndRestore(t *testing.T) {
 	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err != nil {
 		t.Fatal(err)
 	}
-	if b, _ := os.ReadFile(c.State); string(b) != "960 600\n" {
+	if b, _ := os.ReadFile(c.State); string(b) != "960 600 1920 1200\n" {
 		t.Fatalf("state %q", b)
 	}
 	if err := c.Restore(); err != nil {
@@ -212,3 +212,105 @@ func TestRequestForTheCurrentSizeIsDone(t *testing.T) {
 		t.Fatal("the state file is still there")
 	}
 }
+
+// Without a readable res_count nothing can confirm a switch: fail at once,
+// send nothing.
+func TestRequestWithoutResCountFailsAtOnce(t *testing.T) {
+	c := fakeMenu(t, false)
+	c.Wait = 5 * time.Second
+	os.Remove(filepath.Join(c.Sys, "res_count"))
+	start := time.Now()
+	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err == nil {
+		t.Fatal("no error")
+	}
+	if time.Since(start) > time.Second {
+		t.Fatalf("took %v", time.Since(start))
+	}
+	if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
+		t.Fatalf("commands sent: %q", b)
+	}
+	if c.Saved() {
+		t.Fatal("a failed switch left a state file")
+	}
+}
+
+func setSize(c FBControl, s Size) {
+	os.WriteFile(filepath.Join(c.Sys, "width"), []byte(strconv.Itoa(s.W)+"\n"), 0o644)
+	os.WriteFile(filepath.Join(c.Sys, "height"), []byte(strconv.Itoa(s.H)+"\n"), 0o644)
+}
+
+// A state file left by an earlier run, while the framebuffer is at neither
+// size it names as "to", is forgotten without a command.
+func TestRestoreForgetsStaleState(t *testing.T) {
+	c := fakeMenu(t, false)
+	setSize(c, Size{960, 600})
+	os.WriteFile(c.State, []byte("1280 720 1920 1200\n"), 0o644)
+	if err := c.Restore(); err != nil {
+		t.Fatal(err)
+	}
+	if b, _ := os.ReadFile(c.Cmd); len(b) != 0 {
+		t.Fatalf("commands sent: %q", b)
+	}
+	if c.Saved() {
+		t.Fatal("stale state kept")
+	}
+}
+
+// The framebuffer is at "to": "from" goes back.
+func TestRestoreWhenAtTheSwitchedSize(t *testing.T) {
+	c := fakeMenu(t, false)
+	setSize(c, Size{1920, 1200})
+	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
+	if err := c.Restore(); err != nil {
+		t.Fatal(err)
+	}
+	if got := commands(t, c); len(got) != 1 || got[0] != "fb_cmd1 8888 1 960 600" {
+		t.Fatalf("commands %q", got)
+	}
+	if c.Saved() {
+		t.Fatal("state kept after a restore")
+	}
+}
+
+// A file from the previous build holds one size: restore it as before.
+func TestRestoreReadsTheOldOneSizeState(t *testing.T) {
+	c := fakeMenu(t, false)
+	setSize(c, Size{1920, 1200})
+	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
+	if err := c.Restore(); err != nil {
+		t.Fatal(err)
+	}
+	if got := commands(t, c); len(got) != 1 || got[0] != "fb_cmd1 8888 1 960 600" {
+		t.Fatalf("commands %q", got)
+	}
+}
+
+// A failed request keeps the state, so -restore-console can try again.
+func TestRestoreKeepsTheStateWhenTheRequestFails(t *testing.T) {
+	c := fakeMenu(t, true)
+	setSize(c, Size{1920, 1200})
+	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
+	if err := c.Restore(); err == nil {
+		t.Fatal("no error")
+	}
+	if !c.Saved() {
+		t.Fatal("state forgotten after a failed restore")
+	}
+}
+
+// After a switch the menu gave a third size: RestoreAlways still asks for
+// "from", where Restore would take the state for stale.
+func TestRestoreAlwaysIgnoresTheStaleCheck(t *testing.T) {
+	c := fakeMenu(t, false)
+	setSize(c, Size{1280, 720}) // neither from nor to
+	os.WriteFile(c.State, []byte("960 600 1920 1200\n"), 0o644)
+	if err := c.RestoreAlways(); err != nil {
+		t.Fatal(err)
+	}
+	if got := commands(t, c); len(got) != 1 || got[0] != "fb_cmd1 8888 1 960 600" {
+		t.Fatalf("commands %q", got)
+	}
+	if c.Saved() {
+		t.Fatal("state kept")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/platform ./internal/input ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
--- FAIL: TestCloseForgetsThePads (0.00s)
evdev_linux_test.go:109: HasPad still true after Close
c.RestoreAlways undefined (type FBControl has no field or method RestoreAlways)
undefined: restoreOnForcedExit
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/cmd/mistersubsonic/fullres.go b/cmd/mistersubsonic/fullres.go
index ae4a41b..d33a48b 100644
--- a/cmd/mistersubsonic/fullres.go
+++ b/cmd/mistersubsonic/fullres.go
@@ -94,7 +94,8 @@ func openFB(path, profileName, dataDir string, enabled bool) (*gfx.FB, *keeper,
 		log.Printf("display: asked for %v, got %dx%d; keeping %v", to, w, h, from)
 		fb.Close()
 	}
-	if err := ctl.Restore(); err != nil {
+	// The framebuffer is at neither from nor to: ask for from, whatever it has.
+	if err := ctl.RestoreAlways(); err != nil {
 		log.Printf("display: %v", err)
 	}
 	fb, err = gfx.OpenFB(path)
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 10c550b..92fffa3 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -25,7 +25,6 @@ import (
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
 	"mistersubsonic/internal/logfile"
-	"mistersubsonic/internal/platform"
 	"mistersubsonic/internal/ui"
 )
 
@@ -155,10 +154,23 @@ var shutdownLimit = 10 * time.Second
 // forceExit ends a shutdown that took too long; tests replace it.
 var forceExit = func() {
 	log.Printf("shutdown took longer than %v; exiting", shutdownLimit)
-	platform.RestoreText()
+	restoreOnForcedExit()
 	os.Exit(3)
 }
 
+// restoreOnForcedExit puts the framebuffer size back, then the console's
+// text mode. The console is still in graphics mode here (run entered it at
+// start, and only its deferred exit path leaves it), as the size change
+// needs. If that path was already past the size, there is nothing saved.
+func restoreOnForcedExit() {
+	if err := fbControl().Restore(); err != nil {
+		log.Printf("display: %v", err)
+	}
+	if err := restoreText(); err != nil {
+		log.Printf("console: %v", err)
+	}
+}
+
 // armDeadline starts the shutdown deadline (once).
 func armDeadline(t **time.Timer, d time.Duration) {
 	if *t == nil {
diff --git a/internal/input/evdev_linux.go b/internal/input/evdev_linux.go
index 1004bb7..f92239e 100644
--- a/internal/input/evdev_linux.go
+++ b/internal/input/evdev_linux.go
@@ -148,6 +148,7 @@ func (m *Manager) Close() {
 		}
 		f.Close()
 	}
+	clear(m.pads)
 	m.mu.Unlock()
 	m.wg.Wait()
 }
@@ -223,6 +224,7 @@ func padKeys(bits []byte) bool {
 func isGamepad(f *os.File) bool {
 	bits := make([]byte, keyBitsLen)
 	if err := fileIoctlPtr(f, eviocgbit(evKey, keyBitsLen), unsafe.Pointer(&bits[0])); err != nil {
+		log.Printf("input: %s: reading the key bits failed (%v); treating it as a keyboard", f.Name(), err)
 		return false
 	}
 	return padKeys(bits)
diff --git a/internal/platform/fbmode.go b/internal/platform/fbmode.go
index 597b10c..436471a 100644
--- a/internal/platform/fbmode.go
+++ b/internal/platform/fbmode.go
@@ -110,10 +110,10 @@ func DefaultFBControl() FBControl {
 		State: "/tmp/mistersubsonic.fb", Wait: time.Second}
 }
 
-// Switch saves from as the size to restore, then asks for to and waits for
-// the menu to apply it. On failure it asks for from again.
+// Switch saves from (and to) as the size to restore, then asks for to and
+// waits for the menu to apply it. On failure it asks for from again.
 func (c FBControl) Switch(from, to Size) error {
-	if err := os.WriteFile(c.State, []byte(fmt.Sprintf("%d %d\n", from.W, from.H)), 0o644); err != nil {
+	if err := os.WriteFile(c.State, []byte(fmt.Sprintf("%d %d %d %d\n", from.W, from.H, to.W, to.H)), 0o644); err != nil {
 		return fmt.Errorf("platform: save the framebuffer size: %w", err)
 	}
 	if err := c.request(to); err != nil {
@@ -132,9 +132,20 @@ func (c FBControl) Saved() bool {
 	return err == nil
 }
 
-// Restore puts back the size Switch saved, if any, and forgets it. Without
-// a saved size it does nothing.
-func (c FBControl) Restore() error {
+// Restore puts back the size Switch saved, if any. The state is forgotten
+// once it is restored or stale (the framebuffer is not at the size Switch
+// asked for, so this run never switched it); when the request fails it
+// stays, so a later Restore can try again. Without a saved size it does
+// nothing. A state file from the previous build holds only the old size:
+// it is restored without the check.
+func (c FBControl) Restore() error { return c.restore(true) }
+
+// RestoreAlways is Restore without the stale check: it asks for the saved
+// size whatever the framebuffer has now. For a caller that has just switched
+// and knows the menu gave another size than asked for.
+func (c FBControl) RestoreAlways() error { return c.restore(false) }
+
+func (c FBControl) restore(checkStale bool) error {
 	b, err := os.ReadFile(c.State)
 	if errors.Is(err, fs.ErrNotExist) {
 		return nil
@@ -142,23 +153,36 @@ func (c FBControl) Restore() error {
 	if err != nil {
 		return fmt.Errorf("platform: %w", err)
 	}
-	var s Size
-	if _, err := fmt.Sscanf(string(b), "%d %d", &s.W, &s.H); err != nil || s.W <= 0 || s.H <= 0 {
+	var from, to Size
+	n, _ := fmt.Sscanf(string(b), "%d %d %d %d", &from.W, &from.H, &to.W, &to.H)
+	if (n != 2 && n != 4) || from.W <= 0 || from.H <= 0 || (n == 4 && (to.W <= 0 || to.H <= 0)) {
 		os.Remove(c.State)
 		return fmt.Errorf("platform: bad framebuffer state %q", strings.TrimSpace(string(b)))
 	}
-	err = c.request(s)
+	if n == 4 && checkStale {
+		if cur, ok := c.Current(); ok && cur != to {
+			os.Remove(c.State)
+			return nil
+		}
+	}
+	if err := c.request(from); err != nil {
+		return err
+	}
 	os.Remove(c.State)
-	return err
+	return nil
 }
 
-// request sends fb_cmd1 for s (32 bpp) and waits until res_count changes.
+// request sends fb_cmd1 for s (32 bpp) and waits until res_count changes;
+// without a readable res_count it fails before sending anything.
 // If the driver already has size s, it is done.
 func (c FBControl) request(s Size) error {
 	if cur, ok := c.Current(); ok && cur == s {
 		return nil // the menu doesn't count a change to the size it already has
 	}
 	count := c.ResCount()
+	if count == "" {
+		return fmt.Errorf("platform: %s/res_count is unreadable, a switch to %v couldn't be confirmed", c.Sys, s)
+	}
 	f, err := os.OpenFile(c.Cmd, os.O_WRONLY|os.O_APPEND|syscall.O_NONBLOCK, 0) // never block: with no reader a FIFO open fails with ENXIO
 	if err != nil {
 		return fmt.Errorf("platform: %w", err)
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/platform ./internal/input ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9" go vet ./...` with zig on `PATH`. Expected: no output, which means the ARM build of the new code vets clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/fullres.go cmd/mistersubsonic/fullres_test.go cmd/mistersubsonic/main.go internal/input/evdev_linux.go internal/input/evdev_linux_test.go internal/platform/fbmode.go internal/platform/fbmode_test.go
git commit -m "platform, input: full-resolution state knows both sizes and survives failures; forced exits restore; Close forgets the pads" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Logs, launcher and release tooling

**Files:**
- Modify: `internal/logfile/logfile.go`, `sdcard/Scripts/MiSTer_Subsonic.sh`, `scripts/check-notices.sh`, `tools/mkdb/main.go`, `Makefile`
- Test: `internal/logfile/logfile_test.go`, `scripts/test-launcher.sh`, `tools/mkdb/main_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **`internal/logfile`:**
    - `rename` and `openFile` are package variables, used as test hooks.
    - After a failed rename, the log keeps appending, and `retryAt = size + max`, so it retries after every further cap's worth of bytes.
    - When the file can't be reopened, lines go to `os.Stderr`, and each write tries the open again. A `closed` flag replaces `f == nil` as the closed state.
  - **The launcher:**
    - The app runs in the background with `<&0`, and a wait loop runs until it really ends.
    - INT and TERM both forward TERM to the app, because background jobs ignore INT. Before the app starts, they `exit 130`.
    - `restore()` starts with `trap '' INT TERM`.
    - The restore and the traps are set up before BGM and SAM are changed.
    - A failed `exec 9>` for the lock prints a message and stops.
  - **`scripts/test-launcher.sh`:**
    - New cases: `term`, `int` (rewritten), `int-restoring`, `sam-missing` and `lock-unopenable`.
    - `spawn()` starts the launcher through python3 with SIGINT set back to the default.
  - **`scripts/check-notices.sh`:** `set -u -o pipefail`. It checks the union of the host's dependency graph and the ARM one (`GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1`).
  - **`tools/mkdb`:**
    - `checkBaseURL(u) (string, error)`: http or https with a host, no query or fragment. It adds the trailing slash. A bad URL exits with 2.
    - `writeFile(out, data)` creates the parent folder, and refuses an existing `-o` that isn't a regular file.
  - **Makefile:** `mister-test` removes `bin/arm/tests` first.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t6-test.patch` and apply it from the repository root with `git apply /tmp/t6-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/logfile/logfile_test.go b/internal/logfile/logfile_test.go
index 4dae687..30a2c58 100644
--- a/internal/logfile/logfile_test.go
+++ b/internal/logfile/logfile_test.go
@@ -1,6 +1,7 @@
 package logfile
 
 import (
+	"io"
 	"os"
 	"path/filepath"
 	"strings"
@@ -103,3 +104,96 @@ func TestOpenFailsInAMissingDir(t *testing.T) {
 		t.Fatal("opened a log in a missing directory")
 	}
 }
+
+// blockRotation makes log.txt.1 a non-empty directory, so renaming onto it fails.
+func blockRotation(t *testing.T, p string) {
+	t.Helper()
+	if err := os.MkdirAll(filepath.Join(p+".1", "x"), 0o755); err != nil {
+		t.Fatal(err)
+	}
+}
+
+func TestAFailedRotationBacksOff(t *testing.T) {
+	p := filepath.Join(t.TempDir(), "log.txt")
+	l, _ := Open(p, 10)
+	defer l.Close()
+	blockRotation(t, p)
+	renames := 0
+	rename = func(a, b string) error { renames++; return os.Rename(a, b) }
+	defer func() { rename = os.Rename }()
+	line := []byte("aaaa\n")
+	for i := 0; i < 10; i++ { // 50 bytes: well past the cap
+		if _, err := l.Write(line); err != nil {
+			t.Fatal(err)
+		}
+	}
+	if got := read(t, p); got != strings.Repeat("aaaa\n", 10) {
+		t.Errorf("lines were lost: %q", got)
+	}
+	// A try at 10 bytes, the next only after the file has grown by the cap again.
+	if renames > 4 {
+		t.Errorf("%d rename attempts for 50 bytes; want a back-off", renames)
+	}
+}
+
+func TestRotationResumesAfterAFailure(t *testing.T) {
+	p := filepath.Join(t.TempDir(), "log.txt")
+	l, _ := Open(p, 10)
+	defer l.Close()
+	blockRotation(t, p)
+	l.Write([]byte("aaaa\n"))
+	l.Write([]byte("bbbb\n"))
+	l.Write([]byte("cccc\n")) // fails to rotate
+	os.RemoveAll(p + ".1")
+	for _, s := range []string{"dddd\n", "eeee\n", "ffff\n"} {
+		l.Write([]byte(s))
+	}
+	if _, err := os.Stat(p + ".1"); err != nil {
+		t.Fatalf("never rotated after the block went away: %v", err)
+	}
+	if got := read(t, p); len(got) > 10 {
+		t.Errorf("log.txt still %d bytes", len(got))
+	}
+}
+
+func TestReopenFailureFallsBackToStderr(t *testing.T) {
+	p := filepath.Join(t.TempDir(), "log.txt")
+	l, _ := Open(p, 10)
+	defer l.Close()
+	l.Write([]byte("aaaa\n"))
+	l.Write([]byte("bbbb\n"))
+	errFile, w := mustPipe(t)
+	oldStderr := os.Stderr
+	os.Stderr = w
+	fail := true
+	openFile = func(n string, f int, m os.FileMode) (*os.File, error) {
+		if fail {
+			return nil, os.ErrPermission
+		}
+		return os.OpenFile(n, f, m)
+	}
+	defer func() { os.Stderr = oldStderr; openFile = os.OpenFile }()
+	n, err := l.Write([]byte("cccc\n")) // rotates, can't reopen
+	if err != nil || n != 5 {
+		t.Fatalf("Write = %d, %v", n, err)
+	}
+	fail = false
+	l.Write([]byte("dddd\n")) // the file comes back
+	w.Close()
+	b, _ := io.ReadAll(errFile)
+	if string(b) != "cccc\n" {
+		t.Errorf("stderr got %q", b)
+	}
+	if got := read(t, p); got != "dddd\n" {
+		t.Errorf("log.txt = %q", got)
+	}
+}
+
+func mustPipe(t *testing.T) (*os.File, *os.File) {
+	t.Helper()
+	r, w, err := os.Pipe()
+	if err != nil {
+		t.Fatal(err)
+	}
+	return r, w
+}
diff --git a/scripts/test-launcher.sh b/scripts/test-launcher.sh
index 84a8057..aada7ed 100755
--- a/scripts/test-launcher.sh
+++ b/scripts/test-launcher.sh
@@ -131,32 +131,70 @@ run_case "restores everything after a crash" 2 "bgm status" "bgm stop" "app -vol
 grep -q "log.txt" "$c/out" || { echo "FAIL crash: no pointer to the log"; failures=$((failures + 1)); }
 grep -q "crash.txt" "$c/out" || { echo "FAIL crash: no pointer to crash.txt"; failures=$((failures + 1)); }
 
-sandbox term
+# spawn starts the launcher in the background with INT and TERM at their
+# defaults: a background job of a script inherits INT ignored, which a
+# shell can't trap.
+spawn() {
+	python3 -c 'import os,signal,sys; signal.signal(signal.SIGINT, signal.SIG_DFL); os.execv(sys.argv[1], sys.argv[1:])' "$launcher" -volume -20 >"$c/out" 2>&1 &
+	lpid=$!
+}
+# waitlog PATTERN: until the log has a line matching.
+waitlog() { for _ in $(seq 50); do grep -q "$1" "$LOG" && return 0; sleep 0.1; done; return 1; }
+
+# A signal case: the app runs until it is signalled; SIGNAL goes to the launcher.
+signal_case() {
+	name=$1 sig=$2
+	sandbox "$name"
+	socket
+	BGM_STATUS=$'yes\trandom\tall\tx'
+	echo $$ >"$PIDS.MiSTer_SAM_MCP"
+	cat >"$MSS_DIR/mistersubsonic" <<'S'
+#!/bin/bash
+echo "app $*" >>"$LOG"
+[ "$1" = -restore-console ] && exit 0
+# A clean close on TERM, like the app.
+sleep 30 9>&- &
+trap 'echo "app got TERM" >>"$LOG"; kill $!; exit 0' TERM
+wait
+S
+	spawn
+	waitlog "^app -volume"
+	kill -"$sig" "$lpid"
+	wait "$lpid"
+	code=$?
+	want=$(printf '%s\n' "bgm status" "bgm stop" "sam disable" "app -volume -20" "app got TERM" "app -restore-console" "bgm play" "sam enable")
+	if [ "$code" -ne 0 ] || [ "$(cat "$LOG")" != "$want" ]; then
+		echo "FAIL $name: exit $code"; sed 's/^/    /' "$LOG"
+		failures=$((failures + 1))
+	else
+		echo "ok   $sig goes to the app, then everything is restored"
+	fi
+	flock -n "$MSS_LOCK" true || { echo "FAIL $name: the lock is still held"; failures=$((failures + 1)); }
+}
+signal_case term TERM
+signal_case int INT
+
+sandbox int-restoring
 socket
 BGM_STATUS=$'yes\trandom\tall\tx'
 echo $$ >"$PIDS.MiSTer_SAM_MCP"
 cat >"$MSS_DIR/mistersubsonic" <<'S'
 #!/bin/bash
 echo "app $*" >>"$LOG"
-[ "$1" = -restore-console ] && exit 0
-# Bash runs the launcher's trap once this returns: end on our own soon.
-sleep 2
+[ "$1" = -restore-console ] && sleep 1
 exit 0
 S
-"$launcher" -volume -20 >"$c/out" 2>&1 &
-lpid=$!
-for _ in $(seq 50); do grep -q "^app -volume" "$LOG" && break; sleep 0.1; done
-kill -TERM "$lpid"
+spawn
+waitlog "^app -restore-console"
+kill -INT "$lpid"
 wait "$lpid"
-code=$?
 want=$(printf '%s\n' "bgm status" "bgm stop" "sam disable" "app -volume -20" "app -restore-console" "bgm play" "sam enable")
-if [ "$code" -eq 0 ] || [ "$(cat "$LOG")" != "$want" ]; then
-	echo "FAIL term: exit $code"; sed 's/^/    /' "$LOG"
+if [ "$(cat "$LOG")" != "$want" ]; then
+	echo "FAIL int-restoring: restore was cut short"; sed 's/^/    /' "$LOG"
 	failures=$((failures + 1))
 else
-	echo "ok   restores everything when the launcher is terminated"
+	echo "ok   INT during the restore doesn't skip the rest of it"
 fi
-flock -n "$MSS_LOCK" true || { echo "FAIL term: the lock is still held"; failures=$((failures + 1)); }
 
 sandbox locked
 flock "$MSS_LOCK" sleep 30 &
@@ -167,6 +205,16 @@ run_case "won't start while another launcher holds the lock" 1
 # BusyBox's flock has no -w.
 if grep -q 'flock -w' "$launcher"; then echo "FAIL: the launcher uses flock -w"; failures=$((failures + 1)); fi
 
+sandbox sam-missing
+echo $$ >"$PIDS.MiSTer_SAM_MCP"
+rm "$MSS_SAM"
+run_case "runs without SAM's script when SAM is running" 0 "app -volume -20" "app -restore-console"
+
+sandbox lock-unopenable
+MSS_LOCK=$c/no/such/dir/lock
+run_case "stops when it can't open the lock file" 1
+grep -q "lock file" "$c/out" || { echo "FAIL lock-unopenable: no message"; failures=$((failures + 1)); }
+
 sandbox missing
 rm "$MSS_DIR/mistersubsonic"
 run_case "says when the app is missing" 1
diff --git a/tools/mkdb/main_test.go b/tools/mkdb/main_test.go
index 5562183..f305b41 100644
--- a/tools/mkdb/main_test.go
+++ b/tools/mkdb/main_test.go
@@ -67,3 +67,51 @@ func TestBuildListsNestedFolders(t *testing.T) {
 		t.Fatalf("folders %v", db.Folders)
 	}
 }
+
+func TestCheckBaseURL(t *testing.T) {
+	for _, c := range []struct{ in, want string }{
+		{"https://example.com/v1/", "https://example.com/v1/"},
+		{"https://example.com/v1", "https://example.com/v1/"}, // names are appended: it needs the slash
+		{"http://example.com/", "http://example.com/"},
+	} {
+		got, err := checkBaseURL(c.in)
+		if err != nil || got != c.want {
+			t.Errorf("checkBaseURL(%q) = %q, %v; want %q", c.in, got, err, c.want)
+		}
+	}
+	for _, bad := range []string{"", "example.com/v1/", "ftp://example.com/", "https:///v1/", "https://example.com/v1/?x=1", "https://example.com/#top", "https://exa mple.com/"} {
+		if got, err := checkBaseURL(bad); err == nil {
+			t.Errorf("checkBaseURL(%q) accepted as %q", bad, got)
+		}
+	}
+}
+
+func TestWriteFileMakesTheParentDir(t *testing.T) {
+	out := filepath.Join(t.TempDir(), "a", "b", "db.json")
+	if err := writeFile(out, []byte("x")); err != nil {
+		t.Fatal(err)
+	}
+	if b, _ := os.ReadFile(out); string(b) != "x" {
+		t.Fatalf("got %q", b)
+	}
+	if err := writeFile(out, []byte("y")); err != nil { // a database is replaced
+		t.Fatal(err)
+	}
+	if b, _ := os.ReadFile(out); string(b) != "y" {
+		t.Fatalf("got %q", b)
+	}
+}
+
+func TestWriteFileRefusesANonFile(t *testing.T) {
+	dir := t.TempDir()
+	if err := writeFile(dir, []byte("x")); err == nil {
+		t.Fatal("overwrote a directory")
+	}
+	link := filepath.Join(t.TempDir(), "db.json")
+	if err := os.Symlink(dir, link); err != nil {
+		t.Skip(err)
+	}
+	if err := writeFile(link, []byte("x")); err == nil {
+		t.Fatal("wrote through a link to a directory")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/logfile ./tools/mkdb && ./scripts/test-launcher.sh`

Expected: FAIL, e.g.:

```
undefined: rename
undefined: openFile
undefined: checkBaseURL
undefined: writeFile
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/Makefile b/Makefile
index ec04311..3c6fdf4 100644
--- a/Makefile
+++ b/Makefile
@@ -76,8 +76,9 @@ deploy: release
 # Audio test binary for on-device checks and benchmarks.
 # Every package's tests are built for ARM too, so 32-bit-only breakage (an
 # int overflow, say) fails here and in CI; the ones run on the device are
-# copied by deploy-dev.
+# copied by deploy-dev. Old test binaries (of removed packages) are cleared first.
 mister-test:
+	rm -rf $(BIN)/arm/tests
 	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/tests/ ./...
 	cp $(BIN)/arm/tests/audio.test $(BIN)/arm/tests/ui.test $(BIN)/arm/tests/gfx.test $(BIN)/arm/
 	./scripts/check-glibc.sh $(BIN)/arm/audio.test
diff --git a/internal/logfile/logfile.go b/internal/logfile/logfile.go
index f265053..3737bd3 100644
--- a/internal/logfile/logfile.go
+++ b/internal/logfile/logfile.go
@@ -10,6 +10,12 @@ import (
 	"sync"
 )
 
+// Stand-ins for the tests.
+var (
+	rename   = os.Rename
+	openFile = os.OpenFile
+)
+
 // File is a size-capped log file. It is safe for concurrent use.
 type File struct {
 	mu   sync.Mutex
@@ -17,6 +23,9 @@ type File struct {
 	max  int64
 	f    *os.File
 	size int64
+
+	retryAt int64 // after a failed rotation: the size to try again at
+	closed  bool
 }
 
 // Open appends to the log at path, rotating it when it passes max bytes.
@@ -29,7 +38,7 @@ func Open(path string, max int64) (*File, error) {
 }
 
 func (l *File) open() error {
-	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
+	f, err := openFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
 	if err != nil {
 		return fmt.Errorf("logfile: %w", err)
 	}
@@ -43,16 +52,22 @@ func (l *File) open() error {
 }
 
 // Write appends p, rotating first if p would take the file past the cap.
-// A line longer than the cap still goes in whole, into a fresh file.
+// A line longer than the cap still goes in whole, into a fresh file. While
+// the file can't be reopened, lines go to stderr instead and each write
+// tries the file again.
 func (l *File) Write(p []byte) (int, error) {
 	l.mu.Lock()
 	defer l.mu.Unlock()
-	if l.f == nil {
+	if l.closed {
 		return 0, os.ErrClosed
 	}
-	if l.size > 0 && l.size+int64(len(p)) > l.max {
-		if err := l.rotate(); err != nil {
-			return 0, err
+	if l.f == nil && l.open() != nil {
+		return os.Stderr.Write(p)
+	}
+	if l.size > 0 && l.size+int64(len(p)) > l.max && l.size >= l.retryAt {
+		l.rotate()
+		if l.f == nil {
+			return os.Stderr.Write(p)
 		}
 	}
 	n, err := l.f.Write(p)
@@ -60,19 +75,26 @@ func (l *File) Write(p []byte) (int, error) {
 	return n, err
 }
 
-// rotate moves log.txt to log.txt.1. If the rename fails the log goes on
-// growing rather than losing lines.
-func (l *File) rotate() error {
+// rotate moves log.txt to log.txt.1 and starts a new log.txt. If the rename
+// fails the log goes on growing rather than losing lines, and the next try
+// comes when it has grown by the cap again. If the new file can't be opened
+// l.f stays nil.
+func (l *File) rotate() {
+	if err := rename(l.path, l.path+".1"); err != nil {
+		l.retryAt = l.size + l.max
+		return
+	}
+	l.retryAt = 0
 	l.f.Close()
 	l.f = nil
-	os.Rename(l.path, l.path+".1")
-	return l.open()
+	l.open()
 }
 
 // Close closes the file. Later writes fail with os.ErrClosed.
 func (l *File) Close() error {
 	l.mu.Lock()
 	defer l.mu.Unlock()
+	l.closed = true
 	if l.f == nil {
 		return nil
 	}
diff --git a/scripts/check-notices.sh b/scripts/check-notices.sh
index 72300e2..0c52edd 100755
--- a/scripts/check-notices.sh
+++ b/scripts/check-notices.sh
@@ -1,7 +1,7 @@
 #!/bin/bash
 # Fails when the app links a module whose licence notice isn't shipped:
 # every non-main module must map to a file in third_party/licenses/.
-set -u
+set -u -o pipefail
 root=$(cd "$(dirname "$0")/.." && pwd)
 cd "$root" || exit 1
 
@@ -14,7 +14,11 @@ notice_for() {
 	esac
 }
 
-mods=$(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/mistersubsonic | sort -u) || exit 1
+# The host's build and the MiSTer's (ARM) build can link different modules.
+deps() { go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/mistersubsonic; }
+host=$(deps) || exit 1
+arm=$(GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 deps) || exit 1
+mods=$(printf '%s\n%s\n' "$host" "$arm" | sort -u) || exit 1
 bad=0
 for m in $mods; do
 	f=$(notice_for "$m")
diff --git a/sdcard/Scripts/MiSTer_Subsonic.sh b/sdcard/Scripts/MiSTer_Subsonic.sh
index 3221b4c..2a7ad2e 100755
--- a/sdcard/Scripts/MiSTer_Subsonic.sh
+++ b/sdcard/Scripts/MiSTer_Subsonic.sh
@@ -40,7 +40,7 @@ stop_leftovers
 # One launcher at a time. Only the app inherits the lock (fd 9) and holds it
 # while it runs; everything else closes it, so nothing outlives the launcher
 # holding the lock.
-exec 9>"$LOCK"
+exec 9>"$LOCK" || { echo "MiSTer Subsonic can't open its lock file: $LOCK"; exit 1; }
 # BusyBox's flock has no -w: poll with -n.
 locked=
 for ((i = 0; i < LOCK_WAIT; i++)); do
@@ -49,9 +49,29 @@ for ((i = 0; i < LOCK_WAIT; i++)); do
 done
 [ -n "$locked" ] || flock -n 9 || { echo "MiSTer Subsonic is already being started."; exit 1; }
 
+# What the launcher has changed, put back on any way out. A signal during
+# the restore must not cut it short, so it ignores them.
+bgm_stopped=
+sam_disabled=
+restore() {
+	trap '' INT TERM
+	"$APP" -restore-console >/dev/null 2>&1 9>&- # text mode again, even after a crash
+	printf '\033[?25h\033[2J\033[H'          # the cursor back, the screen cleared
+	[ -n "$bgm_stopped" ] && bgm play
+	[ -n "$sam_disabled" ] && "$SAM" enable >/dev/null 2>&1 9>&-
+	return 0
+}
+# INT and TERM go on to the app, which closes cleanly; the launcher then
+# restores as usual. Before the app runs there is nothing to pass them to.
+app=
+forward() {
+	if [ -n "$app" ]; then kill -TERM "$app" 2>/dev/null; else exit 130; fi
+}
+trap restore EXIT
+trap forward INT TERM
+
 # BGM takes commands on its socket. It can't pause: stop it, play it again after.
 bgm() { printf '%s' "$1" | socat -t 2 - "UNIX-CONNECT:$BGM_SOCK" 2>/dev/null 9>&-; }
-bgm_stopped=
 if [ -S "$BGM_SOCK" ]; then
 	status=$(bgm status)
 	if [ -n "$status" ] && [ "$(printf '%s' "$status" | cut -f2)" != disabled ]; then
@@ -61,25 +81,22 @@ if [ -S "$BGM_SOCK" ]; then
 fi
 
 # SAM would start a game over the app once it thinks the MiSTer is idle.
-sam_disabled=
 if [ -x "$SAM" ] && { pidof MiSTer_SAM_MCP >/dev/null || ps | grep -q '[M]iSTer_SAM_MCP'; }; then
 	"$SAM" disable >/dev/null 2>&1 9>&-
 	sam_disabled=1
 fi
 
-restore() {
-	"$APP" -restore-console >/dev/null 2>&1 9>&- # text mode again, even after a crash
-	printf '\033[?25h\033[2J\033[H'          # the cursor back, the screen cleared
-	[ -n "$bgm_stopped" ] && bgm play
-	[ -n "$sam_disabled" ] && "$SAM" enable >/dev/null 2>&1 9>&-
-	return 0
-}
-trap restore EXIT
-trap 'exit 130' INT TERM
-
 printf '\033[?25l' # hide the cursor
-"$APP" "$@"
+# In the background so a signal reaches the trap at once (<&0 keeps its
+# stdin); wait again after each one until the app has really ended.
+"$APP" "$@" <&0 &
+app=$!
+wait "$app"
 code=$?
+while kill -0 "$app" 2>/dev/null; do
+	wait "$app"
+	code=$?
+done
 trap - EXIT
 restore
 if [ "$code" -eq 0 ]; then
diff --git a/tools/mkdb/main.go b/tools/mkdb/main.go
index f8445f8..79c8312 100644
--- a/tools/mkdb/main.go
+++ b/tools/mkdb/main.go
@@ -15,9 +15,11 @@ import (
 	"flag"
 	"fmt"
 	"io/fs"
+	"net/url"
 	"os"
 	"path"
 	"path/filepath"
+	"strings"
 	"time"
 )
 
@@ -77,6 +79,37 @@ func build(dir, id, baseURL string, ts int64) (*DB, error) {
 	return db, nil
 }
 
+// checkBaseURL returns baseURL with the trailing slash the file names need,
+// or an error if it isn't an http(s) address without a query or fragment.
+func checkBaseURL(baseURL string) (string, error) {
+	u, err := url.Parse(baseURL)
+	if err != nil {
+		return "", fmt.Errorf("-base-url: %w", err)
+	}
+	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
+		return "", fmt.Errorf("-base-url %q: want an http:// or https:// address", baseURL)
+	}
+	if u.RawQuery != "" || u.Fragment != "" {
+		return "", fmt.Errorf("-base-url %q: a query or fragment would break the file names appended to it", baseURL)
+	}
+	if !strings.HasSuffix(baseURL, "/") {
+		baseURL += "/"
+	}
+	return baseURL, nil
+}
+
+// writeFile writes data to out, making its parent directories. It replaces a
+// file but refuses anything else (a directory, a device).
+func writeFile(out string, data []byte) error {
+	if st, err := os.Stat(out); err == nil && !st.Mode().IsRegular() {
+		return fmt.Errorf("-o %s exists and isn't a file", out)
+	}
+	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
+		return err
+	}
+	return os.WriteFile(out, data, 0o644)
+}
+
 func main() {
 	dir := flag.String("dir", "bin/release/sdcard", "the SD card tree to describe")
 	id := flag.String("id", "mistersubsonic", "db_id: the section name users put in downloader.ini")
@@ -88,16 +121,21 @@ func main() {
 		fmt.Fprintln(os.Stderr, "mkdb: -base-url is required")
 		os.Exit(2)
 	}
+	base, err := checkBaseURL(*baseURL)
+	if err != nil {
+		fmt.Fprintln(os.Stderr, "mkdb:", err)
+		os.Exit(2)
+	}
 	if *ts == 0 {
 		*ts = time.Now().Unix()
 	}
-	db, err := build(*dir, *id, *baseURL, *ts)
+	db, err := build(*dir, *id, base, *ts)
 	if err != nil {
 		fmt.Fprintln(os.Stderr, "mkdb:", err)
 		os.Exit(1)
 	}
 	b, _ := json.MarshalIndent(db, "", "  ")
-	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
+	if err := writeFile(*out, append(b, '\n')); err != nil {
 		fmt.Fprintln(os.Stderr, "mkdb:", err)
 		os.Exit(1)
 	}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -count=1 ./internal/logfile ./tools/mkdb && ./scripts/test-launcher.sh`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `make release` (zig on `PATH`). Expected: it ends without an error; `scripts/check-notices.sh` passes for both graphs.

- [ ] **Step 5: Commit**

```bash
git add Makefile internal/logfile/logfile.go internal/logfile/logfile_test.go scripts/check-notices.sh scripts/test-launcher.sh sdcard/Scripts/MiSTer_Subsonic.sh tools/mkdb/main.go tools/mkdb/main_test.go
git commit -m "logfile, launcher, tools: rotation backs off, signals reach the app and the restore finishes, pipefail and ARM notices, mkdb guards" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: Per-server data, cache and art

**Files:**
- Modify: `cmd/mistersubsonic/session.go`, `internal/cache/cache.go`, `internal/art/art.go`, `internal/ui/app.go`, `internal/ui/screens_starred.go`, `internal/ui/wizard.go`
- Test: `cmd/mistersubsonic/main_test.go`, `cmd/mistersubsonic/session_test.go`, `internal/art/art_test.go`, `internal/cache/cache_test.go`, `internal/ui/app_test.go`, `internal/ui/lists_test.go`, `internal/ui/wizard_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **`serverDir(name)`** is `<safe>-<first 8 hex digits of sha1(name)>`.
    - If the old bare `<safe>` folder exists, it is used. The first colliding name that asks for it gets it.
    - `MkdirAll` errors are logged with the path.
    - Two older tests now build the folder with `serverDir`.
  - **`internal/cache`:**
    - The eviction walk counts into a local, and the count is kept only after a successful walk.
    - A failed walk logs and leaves the size and the files alone.
    - Any error other than `ErrNotExist` fails the walk.
    - `walkDir` is a test hook.
    - Open logs errors from removing `.tmp` files.
  - **`internal/art`:** `maxFailed = 512`. `noteFailedLocked` drops expired entries first, then the oldest.
  - **The wizard:** `WizardScreen.back` takes B without leaving while a save runs.
  - **`internal/ui`, `app.go`:** Pop is split into `drop` and `shown`. `popTo` drops the screens in between and calls `Shown` only on the target.
  - **Starred:** `Enter` and `Shown` go through `autoLoad`, which does nothing while `d.err` is set. Only A retries.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t7-test.patch` and apply it from the repository root with `git apply /tmp/t7-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index 87c4f57..088d055 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -110,7 +110,7 @@ func TestConnectBuildsSessionAndExitsCleanly(t *testing.T) {
 	if err != nil {
 		t.Fatalf("run: %v", err)
 	}
-	if _, err := os.Stat(filepath.Join(dir, "servers", "x", "cache", "art")); err != nil {
+	if _, err := os.Stat(filepath.Join((&sessions{dataDir: dir}).serverDir("x"), "cache", "art")); err != nil {
 		t.Fatalf("art cache not created after connect: %v", err)
 	}
 }
diff --git a/cmd/mistersubsonic/session_test.go b/cmd/mistersubsonic/session_test.go
index 7b5bcf4..34d3c73 100644
--- a/cmd/mistersubsonic/session_test.go
+++ b/cmd/mistersubsonic/session_test.go
@@ -4,6 +4,7 @@ import (
 	"context"
 	"os"
 	"path/filepath"
+	"strings"
 	"sync"
 	"testing"
 	"time"
@@ -105,8 +106,9 @@ func TestSwitchStopsTheOldSessionAndKeepsTheVolume(t *testing.T) {
 	if got := f.players[1].State().VolumeDB; got != -17 {
 		t.Fatalf("new player volume %v, want the old one's -17", got)
 	}
+	check := &sessions{dataDir: dir}
 	for _, name := range []string{"a", "b"} {
-		if _, err := os.Stat(filepath.Join(dir, "servers", name, "cache", "art")); err != nil {
+		if _, err := os.Stat(filepath.Join(check.serverDir(name), "cache", "art")); err != nil {
 			t.Errorf("server %s has no data folder: %v", name, err)
 		}
 	}
@@ -175,13 +177,44 @@ func TestCloseStopsTheSessionAndLaterConnects(t *testing.T) {
 
 func TestServerDirIsSafe(t *testing.T) {
 	m := &sessions{dataDir: t.TempDir()}
-	for name, want := range map[string]string{"home": "home", "a/b": "a_b", "..": "_", "c:\\x": "c__x"} {
-		if got := filepath.Base(m.serverDir(name)); got != want {
-			t.Errorf("serverDir(%q) = %q, want %q", name, got, want)
+	for name, want := range map[string]string{"home": "home-", "a/b": "a_b-", "..": "_-", "c:\\x": "c__x-"} {
+		got := filepath.Base(m.serverDir(name))
+		if !strings.HasPrefix(got, want) || len(got) != len(want)+8 {
+			t.Errorf("serverDir(%q) = %q, want %q and 8 hex digits", name, got, want)
 		}
 	}
 }
 
+// Names that sanitize alike (or differ only in case, which exFAT folds) must
+// not share a folder: IDs mean different songs on different servers.
+func TestServerDirIsUniquePerServer(t *testing.T) {
+	m := &sessions{dataDir: t.TempDir()}
+	seen := map[string]string{}
+	for _, name := range []string{"a/b", "a_b", "a\\b", "Home", "home", "HOME"} {
+		dir := strings.ToLower(filepath.Base(m.serverDir(name)))
+		if other, ok := seen[dir]; ok {
+			t.Errorf("%q and %q share the folder %q", other, name, dir)
+		}
+		seen[dir] = name
+	}
+	if m.serverDir("home") != m.serverDir("home") {
+		t.Error("the folder isn't stable")
+	}
+}
+
+// A folder made by an older version (the plain sanitized name) keeps being
+// used, so an upgrade doesn't lose the cache and resume state.
+func TestServerDirKeepsAnOldFolder(t *testing.T) {
+	m := &sessions{dataDir: t.TempDir()}
+	old := filepath.Join(m.dataDir, "servers", "a_b")
+	if err := os.MkdirAll(old, 0o755); err != nil {
+		t.Fatal(err)
+	}
+	if got := m.serverDir("a/b"); got != old {
+		t.Errorf("serverDir = %q, want the old folder %q", got, old)
+	}
+}
+
 // slowStops makes every stop wait for release (or, if release is nil, d).
 func slowStops(m *sessions, release chan struct{}, d time.Duration) {
 	m.beforeStop = func(*session) {
diff --git a/internal/art/art_test.go b/internal/art/art_test.go
index 243cf50..4a713c4 100644
--- a/internal/art/art_test.go
+++ b/internal/art/art_test.go
@@ -107,6 +107,30 @@ func TestFailuresAreNotRetriedImmediately(t *testing.T) {
 	}
 }
 
+func TestFailedKeysAreBounded(t *testing.T) {
+	now := time.Unix(1000, 0)
+	var mu sync.Mutex
+	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("404") }, Options{
+		Workers: 1,
+		Now:     func() time.Time { mu.Lock(); defer mu.Unlock(); n := now; now = now.Add(time.Millisecond); return n },
+	})
+	for i := 0; i < maxFailed+100; i++ {
+		h.l.Get(Key{subsonic.ID(fmt.Sprintf("al-%d", i)), 64})
+		h.waitReady(t)
+	}
+	h.l.mu.Lock()
+	n := len(h.l.failed)
+	_, newest := h.l.failed[Key{subsonic.ID(fmt.Sprintf("al-%d", maxFailed+99)), 64}]
+	_, oldest := h.l.failed[Key{"al-0", 64}]
+	h.l.mu.Unlock()
+	if n > maxFailed {
+		t.Fatalf("%d failed keys kept, want at most %d", n, maxFailed)
+	}
+	if !newest || oldest {
+		t.Fatalf("newest kept %v, oldest kept %v; want the newest kept and the oldest forgotten", newest, oldest)
+	}
+}
+
 func TestMemoryBudgetEvictsLRU(t *testing.T) {
 	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(10, 10), nil }, Options{MemBytes: 900, Workers: 1})
 	for _, id := range []subsonic.ID{"a", "b", "c"} { // 400 bytes each decoded
diff --git a/internal/cache/cache_test.go b/internal/cache/cache_test.go
index d659273..0027162 100644
--- a/internal/cache/cache_test.go
+++ b/internal/cache/cache_test.go
@@ -3,6 +3,7 @@ package cache
 import (
 	"bytes"
 	"errors"
+	"io/fs"
 	"os"
 	"path/filepath"
 	"testing"
@@ -158,3 +159,41 @@ func TestEvictionRecountsAfterOutsideDeletes(t *testing.T) {
 		t.Fatalf("size %d, want 600", d.Size())
 	}
 }
+
+// A rename that fails (say the target can't be replaced) is reported, and
+// leaves no .tmp file and no change in the counted size.
+func TestAFailedRenameLeavesNothingBehind(t *testing.T) {
+	dir := t.TempDir()
+	d, _ := Open(dir, 1000)
+	blocker := d.path("a") // a non-empty folder where the entry should go
+	os.MkdirAll(filepath.Join(blocker, "x"), 0o755)
+	if err := d.Put("a", bytes.Repeat([]byte{1}, 100)); err == nil {
+		t.Fatal("Put hid the rename error")
+	}
+	if _, err := os.Stat(blocker + ".tmp"); !os.IsNotExist(err) {
+		t.Fatalf("the .tmp is still there: %v", err)
+	}
+	if d.Size() != 0 {
+		t.Fatalf("size %d after a failed rename", d.Size())
+	}
+}
+
+// A walk that fails must not leave the size at 0 (the cache would then
+// think it is empty and grow past its budget): the old count stays.
+func TestFailedEvictionWalkKeepsTheSize(t *testing.T) {
+	dir := t.TempDir()
+	d, _ := Open(dir, 1000)
+	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
+		d.Put(k, bytes.Repeat([]byte{1}, 150))
+	}
+	old := walkDir
+	walkDir = func(root string, fn fs.WalkDirFunc) error {
+		return fn(root, nil, errors.New("input/output error"))
+	}
+	defer func() { walkDir = old }()
+	before := d.Size()
+	d.Put("g", bytes.Repeat([]byte{1}, 150))
+	if got := d.Size(); got < before {
+		t.Fatalf("size fell to %d (was %d) after a failed walk", got, before)
+	}
+}
diff --git a/internal/ui/app_test.go b/internal/ui/app_test.go
index 7b4d3d7..59e642f 100644
--- a/internal/ui/app_test.go
+++ b/internal/ui/app_test.go
@@ -271,3 +271,33 @@ func TestScreenDrawsOnlyInsideItsArea(t *testing.T) {
 		t.Errorf("body pixel %06x: the screen lost its own area", uint32(got)&0xFFFFFF)
 	}
 }
+
+// shownProbe counts how often it is told it is visible again.
+type shownProbe struct {
+	probe
+	shown int
+}
+
+func (s *shownProbe) Shown(*App) { s.shown++ }
+
+// popTo passes through the screens between the top and the target: only the
+// one that ends up on top is told (a refresh on each would be wasted work,
+// and a network call for some).
+func TestPopToShowsOnlyTheTarget(t *testing.T) {
+	ta := newTestApp(t, ProfileHDMI)
+	root, mid1, mid2, target := &probe{}, &shownProbe{}, &shownProbe{}, &shownProbe{}
+	ta.Push(root)
+	ta.Push(target)
+	ta.Push(mid1)
+	ta.Push(mid2)
+	ta.Push(&probe{})
+	if !ta.popTo(func(s Screen) bool { return s == target }) {
+		t.Fatal("popTo found nothing")
+	}
+	if ta.Top() != Screen(target) {
+		t.Fatalf("top is %T", ta.Top())
+	}
+	if mid1.shown != 0 || mid2.shown != 0 || target.shown != 1 {
+		t.Fatalf("shown: mid1 %d, mid2 %d, target %d; want 0, 0, 1", mid1.shown, mid2.shown, target.shown)
+	}
+}
diff --git a/internal/ui/lists_test.go b/internal/ui/lists_test.go
index bbb1a16..99c6e35 100644
--- a/internal/ui/lists_test.go
+++ b/internal/ui/lists_test.go
@@ -77,6 +77,31 @@ func TestStarredErrorRetries(t *testing.T) {
 	}
 }
 
+// The error screen says "A to retry": coming back to it (Pop, a tab switch)
+// must not fetch again by itself.
+func TestStarredErrorRetriesOnlyOnA(t *testing.T) {
+	ta := newTestApp(t, ProfileHDMI)
+	ta.lib.err = errOffline
+	ta.Push(NewHomeScreen())
+	ta.Push(NewStarredScreen())
+	ta.settle(t)
+	ta.Push(&probe{})
+	ta.Pop() // back on Starred
+	ta.press(input.BtnUp)
+	ta.press(input.BtnRight) // another tab is entered for the first time
+	ta.settle(t)
+	if ta.lib.starCalls != 1 {
+		t.Fatalf("calls %d after coming back, want no retry", ta.lib.starCalls)
+	}
+	ta.lib.err = nil
+	ta.press(input.BtnDown) // off the tabs
+	ta.press(input.BtnA)
+	ta.settle(t)
+	if ta.lib.starCalls != 2 {
+		t.Fatalf("calls %d, want a retry on A", ta.lib.starCalls)
+	}
+}
+
 func TestGoldenPlaylistsAndStarred(t *testing.T) {
 	for _, p := range profiles {
 		ta := newTestApp(t, p)
diff --git a/internal/ui/wizard_test.go b/internal/ui/wizard_test.go
index c42513b..c1e5001 100644
--- a/internal/ui/wizard_test.go
+++ b/internal/ui/wizard_test.go
@@ -232,6 +232,31 @@ func TestWizardReplacesAnInvalidConfig(t *testing.T) {
 	}
 }
 
+// Leaving the wizard while the old config is being moved aside would drop
+// the save (its result is discarded with the screen): the old file would be
+// gone and no new one written. Back does nothing until the save is done.
+func TestWizardStaysWhileTheBackupIsMade(t *testing.T) {
+	srv := fakeServer("ok", false)
+	defer srv.Close()
+	ta, rec := sessionApp(t, nil)
+	os.WriteFile(ta.o.ConfigPath, []byte("this is = not toml ["), 0o600)
+	ta.o.ConfigErr = errors.New("config: invalid TOML syntax at line 1")
+	ta.start()
+	ta.press(input.BtnA) // set up again
+	fill(t, ta, srv.URL, "alice", "pw", "")
+	ta.press(input.BtnA) // save
+	for i := 0; i < 8; i++ {
+		ta.press(input.BtnB) // back out before the backup is done
+	}
+	ta.settle(t)
+	if _, _, err := config.Load(ta.o.ConfigPath); err != nil {
+		t.Fatalf("no new config after leaving during the save: %v", err)
+	}
+	if len(rec.got) != 1 {
+		t.Fatalf("connects %d", len(rec.got))
+	}
+}
+
 func TestGoldenWizard(t *testing.T) {
 	for _, p := range profiles {
 		ta := newTestApp(t, p)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/art ./internal/cache ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
--- FAIL: TestPopToShowsOnlyTheTarget (0.00s)
app_test.go:301: shown: mid1 1, mid2 1, target 1; want 0, 0, 1
--- FAIL: TestStarredErrorRetriesOnlyOnA (0.00s)
lists_test.go:94: calls 2 after coming back, want no retry
--- FAIL: TestWizardStaysWhileTheBackupIsMade (0.00s)
wizard_test.go:253: no new config after leaving during the save: config: file not found
--- FAIL: TestServerDirIsSafe (0.00s)
session_test.go:183: serverDir("home") = "home", want "home-" and 8 hex digits
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/cmd/mistersubsonic/session.go b/cmd/mistersubsonic/session.go
index 1cffd9e..b8b3768 100644
--- a/cmd/mistersubsonic/session.go
+++ b/cmd/mistersubsonic/session.go
@@ -2,6 +2,8 @@ package main
 
 import (
 	"context"
+	"crypto/sha1"
+	"encoding/hex"
 	"log"
 	"os"
 	"path/filepath"
@@ -160,8 +162,12 @@ func (m *sessions) build(a sessionUI, c *subsonic.Client, srv config.Server, pb
 	return s
 }
 
-// serverDir is where a server's data lives: servers/<name> in the data
-// folder, with the name made safe for a file name.
+// serverDir is where a server's data lives: servers/<name>-<hash> in the
+// data folder, the name made safe for a file name and the hash of the exact
+// name keeping servers apart when they sanitize alike ("a/b", "a_b") or
+// differ only in case (exFAT folds it). Older versions used the bare safe
+// name; a server whose folder is there keeps using it, so an upgrade keeps
+// its cache and resume state.
 func (m *sessions) serverDir(name string) string {
 	safe := strings.Map(func(r rune) rune {
 		switch {
@@ -173,9 +179,14 @@ func (m *sessions) serverDir(name string) string {
 	if safe == "" || safe == "." || safe == ".." {
 		safe = "_"
 	}
-	dir := filepath.Join(m.dataDir, "servers", safe)
+	servers := filepath.Join(m.dataDir, "servers")
+	dir := filepath.Join(servers, safe)
+	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
+		h := sha1.Sum([]byte(name))
+		dir = filepath.Join(servers, safe+"-"+hex.EncodeToString(h[:4]))
+	}
 	if err := os.MkdirAll(dir, 0o755); err != nil {
-		log.Printf("server data folder: %v", err)
+		log.Printf("server data folder %s: %v", dir, err)
 	}
 	return dir
 }
diff --git a/internal/art/art.go b/internal/art/art.go
index a44ed5a..d72bf8c 100644
--- a/internal/art/art.go
+++ b/internal/art/art.go
@@ -64,6 +64,10 @@ type Options struct {
 // maxPending bounds the request stack; the oldest requests are dropped.
 const maxPending = 64
 
+// maxFailed bounds the failed-key map (a server with thousands of missing
+// covers must not grow it forever); the oldest failures are forgotten first.
+const maxFailed = 512
+
 type entry struct {
 	key   Key
 	img   *gfx.Image
@@ -186,7 +190,7 @@ func (l *Loader) worker() {
 		l.mu.Lock()
 		delete(l.inflight, k)
 		if err != nil {
-			l.failed[k] = l.o.Now()
+			l.noteFailedLocked(k)
 		} else {
 			delete(l.failed, k)
 			l.insertLocked(k, img)
@@ -198,6 +202,32 @@ func (l *Loader) worker() {
 	}
 }
 
+// noteFailedLocked remembers a failure. Past maxFailed, the ones that may
+// be retried anyway go first, then the oldest.
+func (l *Loader) noteFailedLocked(k Key) {
+	now := l.o.Now()
+	l.failed[k] = now
+	if len(l.failed) <= maxFailed {
+		return
+	}
+	for fk, t := range l.failed {
+		if now.Sub(t) >= l.o.RetryAfter {
+			delete(l.failed, fk)
+		}
+	}
+	for len(l.failed) > maxFailed {
+		var oldest Key
+		var ot time.Time
+		first := true
+		for fk, t := range l.failed {
+			if first || t.Before(ot) {
+				oldest, ot, first = fk, t, false
+			}
+		}
+		delete(l.failed, oldest)
+	}
+}
+
 func (l *Loader) load(k Key) (*gfx.Image, error) {
 	if l.o.Disk != nil {
 		if data, ok := l.o.Disk.Get(k.String()); ok {
diff --git a/internal/cache/cache.go b/internal/cache/cache.go
index 656c71f..4da24b9 100644
--- a/internal/cache/cache.go
+++ b/internal/cache/cache.go
@@ -8,6 +8,7 @@ import (
 	"encoding/hex"
 	"errors"
 	"io/fs"
+	"log"
 	"os"
 	"path/filepath"
 	"sort"
@@ -19,6 +20,9 @@ import (
 // writeFile is replaceable in tests (a full SD card fails mid-write).
 var writeFile = os.WriteFile
 
+// walkDir is replaceable in tests (a walk that fails).
+var walkDir = filepath.WalkDir
+
 type Disk struct {
 	dir string
 	max int64
@@ -42,7 +46,9 @@ func Open(dir string, maxBytes int64) (*Disk, error) {
 			continue
 		}
 		if strings.HasSuffix(e.Name(), ".tmp") {
-			os.Remove(filepath.Join(dir, e.Name()))
+			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
+				log.Printf("art cache: removing %s: %v", e.Name(), err)
+			}
 			continue
 		}
 		d.size += info.Size()
@@ -125,7 +131,8 @@ var evictions int
 // evictLocked removes the oldest entries down to a low-water mark of 90% of
 // the budget, so the directory walk happens once per ~10% of new data. The
 // walk also recounts the size, which drifts if files are deleted behind the
-// cache's back.
+// cache's back. A walk that fails leaves the size and the files alone: a
+// partial count would let the cache outgrow its budget.
 func (d *Disk) evictLocked(keep string) {
 	evictions++
 	target := d.max * 9 / 10
@@ -135,21 +142,32 @@ func (d *Disk) evictLocked(keep string) {
 		mod  time.Time
 	}
 	var all []ent
-	d.size = 0
-	filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
-		if err != nil || e.IsDir() {
+	var total int64
+	walkErr := walkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
+		if err != nil {
+			return err
+		}
+		if e.IsDir() {
 			return nil
 		}
 		info, err := e.Info()
+		if errors.Is(err, fs.ErrNotExist) {
+			return nil // removed since the listing
+		}
 		if err != nil {
-			return nil
+			return err
 		}
-		d.size += info.Size()
+		total += info.Size()
 		if p != keep {
 			all = append(all, ent{p, info.Size(), info.ModTime()})
 		}
 		return nil
 	})
+	if walkErr != nil {
+		log.Printf("art cache: eviction walk: %v", walkErr)
+		return
+	}
+	d.size = total
 	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
 	for _, e := range all {
 		if d.size <= target {
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 70147b1..4316697 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -321,22 +321,36 @@ func (a *App) Pop() {
 	if len(a.stack) <= 1 {
 		return
 	}
+	a.drop()
+	a.shown()
+}
+
+// drop closes the top screen without telling the one below.
+func (a *App) drop() {
 	top := a.stack[len(a.stack)-1]
 	top.cancel()
 	a.stack = a.stack[:len(a.stack)-1]
 	a.dirty = true
+}
+
+// shown tells the top screen it is visible again.
+func (a *App) shown() {
 	if sh, ok := a.Top().(shower); ok {
 		sh.Shown(a)
 	}
 }
 
 // popTo pops screens until pred matches the top; if no screen in the stack
-// matches, the stack is left unchanged and false is returned.
+// matches, the stack is left unchanged and false is returned. Only the
+// screen that ends up on top is told (Shown), not the ones passed through.
 func (a *App) popTo(pred func(Screen) bool) bool {
 	for i := len(a.stack) - 1; i >= 0; i-- {
 		if pred(a.stack[i].s) {
-			for len(a.stack) > i+1 {
-				a.Pop()
+			if len(a.stack) > i+1 {
+				for len(a.stack) > i+1 {
+					a.drop()
+				}
+				a.shown()
 			}
 			return true
 		}
diff --git a/internal/ui/screens_starred.go b/internal/ui/screens_starred.go
index 259e0b9..aea7014 100644
--- a/internal/ui/screens_starred.go
+++ b/internal/ui/screens_starred.go
@@ -53,10 +53,18 @@ type starredTab struct {
 }
 
 func (s *starredTab) Title() string { return "Starred" }
-func (s *starredTab) Enter(a *App)  { s.d.load(a, s) }
+func (s *starredTab) Enter(a *App)  { s.autoLoad(a) }
 
 // Shown reloads the list if a star changed while another screen was up.
-func (s *starredTab) Shown(a *App) { s.d.load(a, s) }
+func (s *starredTab) Shown(a *App) { s.autoLoad(a) }
+
+// autoLoad loads on its own, but not again after a failure: the error
+// screen says "A to retry", and Handle does.
+func (s *starredTab) autoLoad(a *App) {
+	if s.d.err == nil {
+		s.d.load(a, s)
+	}
+}
 
 // sync copies the shared result into this tab's view.
 func (s *starredTab) sync() int {
diff --git a/internal/ui/wizard.go b/internal/ui/wizard.go
index cc68524..2abbe0c 100644
--- a/internal/ui/wizard.go
+++ b/internal/ui/wizard.go
@@ -164,6 +164,9 @@ func (s *WizardScreen) Handle(a *App, e input.Event) bool {
 // back goes to the previous step; false (the app pops the wizard) on the
 // first step, unless the wizard is the app's first screen.
 func (s *WizardScreen) back(a *App) bool {
+	if s.saving {
+		return true // the save's result is dropped with the screen: wait for it
+	}
 	if s.step > stepURL {
 		s.cancelTest()
 		s.setStep(s.step - 1)
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/art ./internal/cache ./internal/ui ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main_test.go cmd/mistersubsonic/session.go cmd/mistersubsonic/session_test.go internal/art/art.go internal/art/art_test.go internal/cache/cache.go internal/cache/cache_test.go internal/ui/app.go internal/ui/app_test.go internal/ui/lists_test.go internal/ui/screens_starred.go internal/ui/wizard.go internal/ui/wizard_test.go
git commit -m "session, cache, art, ui: unique server folders, a failed eviction walk keeps the size, a bounded failure map, no stray Shown or reloads" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Config comments kept on save, and faster covers

**Files:**
- Create: `internal/config/edit.go`
- Modify: `internal/config/config.go`, `internal/gfx/image.go`
- Test: `internal/config/edit_test.go`, `internal/gfx/decode_bench_test.go`, `internal/gfx/refbox_test.go` (new); `internal/config/config_test.go` (modified)

**Interfaces:**
- **Produces:**
  - **`internal/config`:**
    - **`Save`** reads the existing file and calls `editConfig(text, cfg) (string, bool)`.
      - If it succeeds, Save writes that text. Otherwise it re-encodes the whole file with its header, as before.
      - The temporary file, the rename, mode 0600 and the validation are unchanged.
    - **`editConfig`** decodes the old file over `Default()`.
      - Each setting whose value changed (`scalars`) is replaced in place, keeping its trailing comment. A missing key is added at the end of its table, and a missing table is appended.
      - `[[server]]` tables are matched to the old servers by name:
        - unchanged tables keep their lines;
        - changed ones are re-encoded (`encodeServer`);
        - removed ones are dropped;
        - new ones are added after the last server.
      - CRLF and a BOM are kept.
    - **The safety net:** the edited text must decode to a config that encodes exactly like `cfg`. Otherwise, and for inline tables, multi-line strings, an old file that doesn't parse or reordered servers, `editConfig` reports false.
  - **`internal/gfx`:**
    - **Row readers.** `rowReader` and `rowsOf(m)` replace the per-pixel closure. YCbCr uses a chroma column table built once per image.
    - **`boxFilter(sw, sh, rows, opaque, w, h)`** sums whole source rows.
      - `recips` divides exactly with a multiply and a shift, for `s ≤ 255·k` and `k ≤ 4104`. `k == 1` is special-cased, and a larger `k` divides.
      - `isOpaque(m)` sends opaque sources to `boxFilterOpaque`, with 32-bit sums, when `opaqueSumsFit`.
    - **`Resize`** reads `src.Pix` rows directly, and passes `opaque = false`.
    - **`refbox_test.go`** keeps the old closure code as the reference, and checks exact equality for every source type.
    - **`BenchmarkDecodeCover`** decodes a 2000×2000 JPEG and scales it to 600×600.

- [ ] **Step 1: Write the failing tests**

`internal/config/edit_test.go` (new file):

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commented = `# My own notes on this file.
default_server = "home"   # the one at home

# --- playback ---
[playback]
volume_db = -12.0   # keep it quiet at night
replaygain = "off"
# scrobble stays on

[display]
hints = true

# my servers
[[server]]
# the NAS
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "sesame"   # TODO: use a token

[extra]
custom = 1 # not ours
`

func saveEdited(t *testing.T, body string, change func(*Config)) (string, *Config) {
	t.Helper()
	p := write(t, body)
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	change(cfg)
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	got, _, err := Load(p)
	if err != nil {
		t.Fatalf("saved file does not load: %v", err)
	}
	if enc1, enc2 := encodeForCompare(got), encodeForCompare(cfg); enc1 != enc2 {
		t.Fatalf("reload differs from what was saved:\n%s\nvs\n%s", enc1, enc2)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	return string(b), cfg
}

func wantAll(t *testing.T, got string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(got, p) {
			t.Errorf("saved file lost %q:\n%s", p, got)
		}
	}
}

func TestSaveKeepsCommentsOnVolumeChange(t *testing.T) {
	got, _ := saveEdited(t, commented, func(c *Config) { c.Playback.VolumeDB = -6 })
	wantAll(t, got, "# My own notes on this file.", "default_server = \"home\"   # the one at home", "# --- playback ---",
		"volume_db = -6.0   # keep it quiet at night", "# scrobble stays on", "# my servers", "# the NAS",
		"password = \"sesame\"   # TODO: use a token", "[extra]", "custom = 1 # not ours")
	if strings.Contains(got, "-12") {
		t.Errorf("old volume left in file:\n%s", got)
	}
}

func TestSaveKeepsCommentsOnServerAdd(t *testing.T) {
	got, cfg := saveEdited(t, commented, func(c *Config) {
		c.AddServer(Server{Name: "work", URL: "https://music.example.com", Username: "bob", Token: "t0k", Salt: "s4lt"})
	})
	wantAll(t, got, "# My own notes on this file.", "# --- playback ---", "volume_db = -12.0   # keep it quiet at night",
		"# the NAS", "password = \"sesame\"   # TODO: use a token", "custom = 1 # not ours", `name = "work"`)
	if !strings.Contains(got, `default_server = "work"   # the one at home`) || cfg.DefaultServer != "work" {
		t.Errorf("default_server not updated in place:\n%s", got)
	}
	if strings.Index(got, `name = "work"`) > strings.Index(got, "[extra]") {
		t.Errorf("new server landed after [extra]:\n%s", got)
	}
}

func TestSaveKeepsCommentsOnServerRemove(t *testing.T) {
	body := commented + "\n[[server]]\nname = \"work\"\nurl = \"http://w:4533\"\nusername = \"bob\"\npassword = \"pw\"\n"
	got, _ := saveEdited(t, body, func(c *Config) { c.RemoveServer("home") })
	wantAll(t, got, "# My own notes on this file.", "# --- playback ---", "[extra]", "custom = 1 # not ours", `name = "work"`, `default_server = "work"`)
	if strings.Contains(got, `"home"`) || strings.Contains(got, "sesame") {
		t.Errorf("removed server still in file:\n%s", got)
	}
}

func TestSaveEditsAChangedServerInPlace(t *testing.T) {
	got, _ := saveEdited(t, commented, func(c *Config) { c.Servers[0].Password = ""; c.Servers[0].Token, c.Servers[0].Salt = "tok", "salt" })
	wantAll(t, got, "# my servers", "token = \"tok\"", "[extra]", "custom = 1 # not ours")
	if strings.Contains(got, "sesame") {
		t.Errorf("old password left:\n%s", got)
	}
}

func TestSaveAddsMissingKeysAndSections(t *testing.T) {
	got, _ := saveEdited(t, minimal, func(c *Config) {
		c.Playback.ReplayGain = "album"
		c.Display.Hints = false
		c.Cache.CoverArtMB = 50
	})
	wantAll(t, got, `replaygain = "album"`, "hints = false", "cover_art_mb = 50", `password = "sesame"`)
}

func TestSaveWithoutChangesLeavesTheFileAlone(t *testing.T) {
	p := write(t, commented)
	cfg, _, _ := Load(p)
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Fatalf("file changed:\n%s", b)
	}
}

func TestSaveKeepsCRLF(t *testing.T) {
	body := strings.ReplaceAll(commented, "\n", "\r\n")
	got, _ := saveEdited(t, body, func(c *Config) {
		c.Playback.VolumeDB = -3
		c.Display.FullResolution = false
	})
	wantAll(t, got, "# --- playback ---", "volume_db = -3.0   # keep it quiet at night")
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("bare LF in a CRLF file:\n%q", got)
	}
}

func TestSaveFallsBackOnLayoutItCannotEdit(t *testing.T) {
	// an inline table: the line editor cannot change one key in it
	got, _ := saveEdited(t, "playback = { volume_db = -5.0 }\n"+minimal, func(c *Config) { c.Playback.VolumeDB = -9 })
	if !strings.HasPrefix(got, "# MiSTer Subsonic configuration") {
		t.Fatalf("expected a fresh file:\n%s", got)
	}
}

func TestSaveOverGarbageWritesAFreshFile(t *testing.T) {
	p := write(t, "not toml [[[")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") {
		t.Fatalf("no header:\n%s", b)
	}
}

func TestSaveCreatesAFreshFileWhenThereIsNone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err != nil {
		t.Fatal(err)
	}
}
```

`internal/gfx/decode_bench_test.go` (new file):

```go
package gfx

import (
	"bytes"
	"image"
	"image/jpeg"
	"math/rand"
	"testing"
)

// coverJPEG is a 2000×2000 photo-like JPEG (a gradient with noise).
func coverJPEG(tb testing.TB) []byte {
	tb.Helper()
	r := rand.New(rand.NewSource(1))
	m := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 2000; x++ {
			i := m.PixOffset(x, y)
			m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = uint8(x/8+r.Intn(16)), uint8(y/8+r.Intn(16)), uint8((x+y)/16+r.Intn(16)), 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: 85}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// BenchmarkDecodeCover measures what the art workers do with a big cover:
// decode it and scale it to fit the screen. "scale" is the scaling alone,
// from the decoder's own image (4:2:0 YCbCr, as JPEGs come).
func BenchmarkDecodeCover(b *testing.B) {
	data := coverJPEG(b)
	b.Run("decode+scale", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := DecodeImage(data, 600, 600); err != nil {
				b.Fatal(err)
			}
		}
	})
	m, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		b.Fatal(err)
	}
	bnd := m.Bounds()
	b.Run("scale", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			boxFilter(bnd.Dx(), bnd.Dy(), rowsOf(m), isOpaque(m), 600, 600)
		}
	})
}
```

`internal/gfx/refbox_test.go` (new file):

```go
package gfx

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// The reference implementations below are the per-pixel closure versions the
// row loops replaced.

// refPixels reads m's pixel (x, y), counted from its top-left corner, as
// non-premultiplied ARGB. The common decoder outputs are read directly; other
// types go through At.
func refPixels(m image.Image) func(x, y int) uint32 {
	b := m.Bounds()
	switch m := m.(type) {
	case *image.YCbCr:
		return func(x, y int) uint32 {
			yi, ci := m.YOffset(b.Min.X+x, b.Min.Y+y), m.COffset(b.Min.X+x, b.Min.Y+y)
			r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
			return 0xFF<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
		}
	case *image.NRGBA:
		return func(x, y int) uint32 {
			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
			return uint32(p[3])<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
		}
	case *image.RGBA:
		return func(x, y int) uint32 {
			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
			a := uint32(p[3])
			switch a {
			case 0:
				return 0
			case 0xFF:
				return 0xFF<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
			}
			un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
			return a<<24 | un(p[0])<<16 | un(p[1])<<8 | un(p[2])
		}
	case *image.Gray:
		return func(x, y int) uint32 {
			g := uint32(m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y)])
			return 0xFF<<24 | g<<16 | g<<8 | g
		}
	}
	return func(x, y int) uint32 {
		c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
		return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	}
}

// refBoxFilter averages the sw×sh source read by px down to w×h, weighting
// colour by alpha. Sums are 64-bit: a box can hold any number of pixels.
func refBoxFilter(sw, sh int, px func(x, y int) uint32, w, h int) *Image {
	out := NewImage(w, h)
	if w == sw && h == sh {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.Pix[y*w+x] = px(x, y)
			}
		}
		return out
	}
	for y := 0; y < h; y++ {
		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			sx0, sx1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var a, r, g, b, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					p := px(sx, sy)
					pa := uint64(p >> 24)
					a += pa
					r += uint64(p>>16&0xFF) * pa
					g += uint64(p>>8&0xFF) * pa
					b += uint64(p&0xFF) * pa
					n++
				}
			}
			if a == 0 {
				continue
			}
			out.Pix[y*w+x] = uint32(a/n)<<24 | uint32(r/a)<<16 | uint32(g/a)<<8 | uint32(b/a)
		}
	}
	return out
}

// randomImages has one random image of every source type, with a non-zero
// origin and every chroma subsampling for YCbCr.
func randomImages(r *rand.Rand, w, h int) map[string]image.Image {
	rect := image.Rect(3, 5, 3+w, 5+h)
	fill := func(pix []uint8) {
		for i := range pix {
			pix[i] = uint8(r.Intn(256))
		}
	}
	out := map[string]image.Image{}
	nrgba, rgba, gray := image.NewNRGBA(rect), image.NewRGBA(rect), image.NewGray(rect)
	fill(nrgba.Pix)
	fill(gray.Pix)
	fill(rgba.Pix)
	for i := 0; i < len(rgba.Pix); i += 4 { // premultiplied: colour <= alpha
		for c := 0; c < 3; c++ {
			rgba.Pix[i+c] = uint8(int(rgba.Pix[i+c]) * int(rgba.Pix[i+3]) / 255)
		}
		if r.Intn(4) == 0 {
			rgba.Pix[i+3], nrgba.Pix[i+3] = 255, 255
		}
	}
	out["nrgba"], out["rgba"], out["gray"] = nrgba, rgba, gray
	nrgbaO, rgbaO := image.NewNRGBA(rect), image.NewRGBA(rect)
	fill(nrgbaO.Pix)
	fill(rgbaO.Pix)
	for i := 3; i < len(nrgbaO.Pix); i += 4 {
		nrgbaO.Pix[i], rgbaO.Pix[i] = 255, 255
	}
	out["nrgbaOpaque"], out["rgbaOpaque"] = nrgbaO, rgbaO
	g16, cmyk := image.NewGray16(rect), image.NewCMYK(rect)
	fill(g16.Pix)
	fill(cmyk.Pix)
	out["gray16"], out["cmyk"] = g16, cmyk
	for _, sr := range []image.YCbCrSubsampleRatio{image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440, image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410} {
		y := image.NewYCbCr(rect, sr)
		fill(y.Y)
		fill(y.Cb)
		fill(y.Cr)
		out["ycbcr"+sr.String()] = y
	}
	pal := image.NewPaletted(rect, color.Palette{color.Black, color.White, color.NRGBA{200, 10, 90, 128}, color.Transparent})
	for i := range pal.Pix {
		pal.Pix[i] = uint8(r.Intn(4))
	}
	out["paletted"] = pal
	return out
}

func TestBoxFilterMatchesTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, sz := range [][2]int{{61, 47}, {8, 8}, {1, 30}, {30, 1}} {
		for name, m := range randomImages(r, sz[0], sz[1]) {
			b := m.Bounds()
			sw, sh := b.Dx(), b.Dy()
			for _, to := range [][2]int{{sw, sh}, {1, 1}, {7, 5}, {sw/2 + 1, sh/3 + 1}, {sw, 1}, {1, sh}, {sw + 9, sh + 4}} {
				got := boxFilter(sw, sh, rowsOf(m), isOpaque(m), to[0], to[1])
				want := refBoxFilter(sw, sh, refPixels(m), to[0], to[1])
				for i := range want.Pix {
					if got.Pix[i] != want.Pix[i] {
						t.Fatalf("%s %dx%d -> %dx%d: pixel %d = %08x, want %08x", name, sw, sh, to[0], to[1], i, got.Pix[i], want.Pix[i])
					}
				}
			}
		}
	}
}

func TestResizeMatchesTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	src := FromImage(randomImages(r, 53, 41)["nrgba"])
	for _, to := range [][2]int{{20, 10}, {53, 41}, {1, 1}, {90, 60}} {
		got := Resize(src, to[0], to[1])
		want := refBoxFilter(src.W, src.H, func(x, y int) uint32 { return src.Pix[y*src.W+x] }, to[0], to[1])
		for i := range want.Pix {
			if got.Pix[i] != want.Pix[i] {
				t.Fatalf("-> %dx%d: pixel %d = %08x, want %08x", to[0], to[1], i, got.Pix[i], want.Pix[i])
			}
		}
	}
}

func TestRecipsDivideExactly(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	var rc recips
	for k := uint64(1); k <= maxRecip+3; k++ {
		check := func(s uint64) {
			if s > 255*k {
				return
			}
			if got, want := rc.div(s, k), s/k; got != want {
				t.Fatalf("%d/%d = %d, want %d", s, k, got, want)
			}
		}
		for q := uint64(0); q <= 255; q++ { // the edges of every quotient
			check(q * k)
			check(q*k + k - 1)
			if q > 0 {
				check(q*k - 1)
			}
		}
		for range 2000 {
			check(uint64(r.Int63n(int64(255*k + 1))))
		}
	}
}

func TestBoxFilterBigBoxesMatchTheClosureVersion(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for name, m := range randomImages(r, 150, 130) {
		for _, to := range [][2]int{{1, 1}, {2, 3}, {5, 4}} { // boxes of hundreds to thousands of pixels
			got := boxFilter(150, 130, rowsOf(m), isOpaque(m), to[0], to[1])
			want := refBoxFilter(150, 130, refPixels(m), to[0], to[1])
			for i := range want.Pix {
				if got.Pix[i] != want.Pix[i] {
					t.Fatalf("%s -> %dx%d: pixel %d = %08x, want %08x", name, to[0], to[1], i, got.Pix[i], want.Pix[i])
				}
			}
		}
	}
}

// TestOpaqueFastPathIsUsedAndExact runs each opaque source through the
// opaque loop explicitly (and through the general loop) against the reference.
func TestOpaqueFastPathIsUsedAndExact(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	opaque := 0
	for _, sz := range [][2]int{{61, 47}, {150, 130}, {1, 30}, {30, 1}} {
		for name, m := range randomImages(r, sz[0], sz[1]) {
			b := m.Bounds()
			sw, sh := b.Dx(), b.Dy()
			if isOpaque(m) {
				opaque++
			} else if name != "nrgba" && name != "rgba" && name != "paletted" {
				t.Errorf("%s is not detected as opaque", name)
			}
			for _, to := range [][2]int{{1, 1}, {7, 5}, {sw/2 + 1, sh/3 + 1}, {sw + 5, sh + 3}, {sw, sh}} {
				want := refBoxFilter(sw, sh, refPixels(m), to[0], to[1])
				for _, fast := range []bool{false, isOpaque(m)} {
					got := boxFilter(sw, sh, rowsOf(m), fast, to[0], to[1])
					for i := range want.Pix {
						if got.Pix[i] != want.Pix[i] {
							t.Fatalf("%s opaque=%v %dx%d -> %dx%d: pixel %d = %08x, want %08x", name, fast, sw, sh, to[0], to[1], i, got.Pix[i], want.Pix[i])
						}
					}
				}
			}
		}
	}
	if opaque == 0 {
		t.Fatal("no opaque image tested")
	}
}

func TestOpaqueSumsFit(t *testing.T) {
	cols := func(sw, w int) (a, b []int) {
		a, b = make([]int, w), make([]int, w)
		for x := range w {
			a[x], b[x] = x*sw/w, max((x+1)*sw/w, x*sw/w+1)
		}
		return
	}
	for _, c := range []struct {
		sw, sh, w, h int
		fit          bool
	}{{2000, 2000, 600, 600, true}, {4096, 4096, 1, 1, true}, {5000, 4000, 1, 1, false}, {8000, 6000, 1, 1, false}, {8000, 6000, 8, 6, true}} {
		a, b := cols(c.sw, c.w)
		if got := opaqueSumsFit(c.sw, c.sh, c.w, c.h, a, b); got != c.fit {
			t.Errorf("%dx%d -> %dx%d: fit = %v, want %v", c.sw, c.sh, c.w, c.h, got, c.fit)
		}
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t8-test.patch` and apply it from the repository root with `git apply /tmp/t8-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/config/config_test.go b/internal/config/config_test.go
index 7b2120d..6bce238 100644
--- a/internal/config/config_test.go
+++ b/internal/config/config_test.go
@@ -7,6 +7,8 @@ import (
 	"strings"
 	"testing"
 	"time"
+
+	"github.com/BurntSushi/toml"
 )
 
 func write(t *testing.T, body string) string {
@@ -196,3 +198,12 @@ func TestParseErrorDoesNotEchoSecrets(t *testing.T) {
 		t.Fatalf("error does not say where: %v", err)
 	}
 }
+
+// encodeForCompare renders c through the TOML encoder: equal text means equal values.
+func encodeForCompare(c *Config) string {
+	var b strings.Builder
+	if err := toml.NewEncoder(&b).Encode(c); err != nil {
+		return err.Error()
+	}
+	return b.String()
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/config ./internal/gfx`

Expected: FAIL, e.g.:

```
--- FAIL: TestSaveKeepsCommentsOnVolumeChange (0.00s)
--- FAIL: TestSaveKeepsCommentsOnServerAdd (0.00s)
--- FAIL: TestSaveKeepsCommentsOnServerRemove (0.00s)
--- FAIL: TestSaveEditsAChangedServerInPlace (0.00s)
--- FAIL: TestSaveWithoutChangesLeavesTheFileAlone (0.00s)
--- FAIL: TestSaveKeepsCRLF (0.00s)
undefined: rowsOf
undefined: isOpaque
```

- [ ] **Step 3: Implement**

`internal/config/edit.go` (new file):

```go
package config

import (
	"bytes"
	"strings"

	"github.com/BurntSushi/toml"
)

// editConfig returns text (an existing config.toml) changed to hold cfg while
// keeping the user's comments, blank lines, key order and unknown keys: only
// the values that differ are touched. It reports false when the file has a
// layout it cannot edit line by line (inline tables, multi-line strings...) or
// when the edited text does not decode back to cfg; Save then rewrites the file.
func editConfig(text string, cfg *Config) (string, bool) {
	old := Default()
	if _, err := toml.Decode(text, old); err != nil {
		return "", false
	}
	eol := "\n"
	if strings.Contains(text, "\r\n") {
		eol = "\r\n"
	}
	e := &editor{lines: strings.Split(text, "\n"), eol: eol}
	o, n := scalars(old), scalars(cfg)
	for i := range n {
		if o[i].val == n[i].val {
			continue
		}
		v, ok := tomlValue(n[i].key, n[i].val)
		if !ok || !e.set(n[i].sec, n[i].key, v) {
			return "", false
		}
	}
	if !e.servers(old.Servers, cfg.Servers) {
		return "", false
	}
	out := strings.Join(e.lines, "\n")
	got := Default()
	if _, err := toml.Decode(out, got); err != nil || encodeAll(got) != encodeAll(cfg) {
		return "", false
	}
	return out, true
}

func encodeAll(c *Config) string {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(c); err != nil {
		return err.Error()
	}
	return b.String()
}

// bom may start the file.
const bom = "\uFEFF"

type scalar struct {
	sec, key string
	val      any
}

// scalars lists every setting outside the [[server]] tables.
func scalars(c *Config) []scalar {
	return []scalar{
		{"", "default_server", c.DefaultServer},
		{"playback", "transcode_format", c.Playback.TranscodeFormat},
		{"playback", "transcode_bitrate", c.Playback.TranscodeBitrate},
		{"playback", "replaygain", c.Playback.ReplayGain},
		{"playback", "volume_db", c.Playback.VolumeDB},
		{"playback", "scrobble", c.Playback.Scrobble},
		{"playback", "buffer_mb", c.Playback.BufferMB},
		{"playback", "alsa_device", c.Playback.ALSADevice},
		{"display", "profile", c.Display.Profile},
		{"display", "screensaver_minutes", c.Display.ScreensaverMinutes},
		{"display", "full_resolution", c.Display.FullResolution},
		{"display", "hints", c.Display.Hints},
		{"cache", "cover_art_mb", c.Cache.CoverArtMB},
	}
}

// tomlValue renders v the way the TOML encoder would.
func tomlValue(key string, v any) (string, bool) {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(map[string]any{key: v}); err != nil {
		return "", false
	}
	s := strings.TrimSpace(b.String())
	_, val, ok := strings.Cut(s, "= ")
	return val, ok
}

type editor struct {
	lines []string // the file split at "\n"; a trailing "" stands for the final newline
	eol   string
}

// end is the index where lines can be appended (before the final newline).
func (e *editor) end() int {
	if n := len(e.lines); n > 0 && e.lines[n-1] == "" {
		return n - 1
	}
	return len(e.lines)
}

func (e *editor) insert(at int, add ...string) {
	for i := range add {
		if e.eol == "\r\n" {
			add[i] += "\r"
		}
	}
	e.lines = append(e.lines[:at:at], append(add, e.lines[at:]...)...)
}

// header returns the table a line opens ("" if it is not a table header).
func header(line string) string {
	t := strings.TrimSpace(strings.TrimPrefix(line, bom))
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	t = strings.TrimSpace(strings.Trim(strings.SplitN(t, "#", 2)[0], "[] \t\r"))
	return t
}

func isHeader(line string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(line, bom))
	return strings.HasPrefix(t, "[")
}

// keyLine reports whether line assigns key, and where its value starts.
func keyLine(line, key string) (int, bool) {
	t := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(t, key) {
		return 0, false
	}
	rest := strings.TrimLeft(t[len(key):], " \t")
	if !strings.HasPrefix(rest, "=") {
		return 0, false
	}
	i := len(line) - len(rest) + 1
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i, true
}

// valueLen is the length of the scalar at the start of s, up to its trailing
// comment; 0 if it is a multi-line string or unterminated.
func valueLen(s string) int {
	switch {
	case strings.HasPrefix(s, `"""`), strings.HasPrefix(s, `'''`), s == "":
		return 0
	case s[0] == '"':
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
		return 0
	case s[0] == '\'':
		if i := strings.IndexByte(s[1:], '\''); i >= 0 {
			return i + 2
		}
		return 0
	}
	return strings.IndexAny(s+" ", " \t#\r")
}

// set gives key in table sec the value v: in place if the key is there, else
// as a new line in the table (created at the end of the file if need be).
func (e *editor) set(sec, key, v string) bool {
	cur, last, first := "", -1, -1 // last: the table's last key line, or its header
	for i, l := range e.lines {
		if isHeader(l) {
			cur = header(l)
			if cur == sec {
				last = i
			}
			continue
		}
		if cur != sec {
			continue
		}
		if j, ok := keyLine(l, key); ok {
			n := valueLen(l[j:])
			if n == 0 {
				return false
			}
			e.lines[i] = l[:j] + v + l[j+n:]
			return true
		}
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			last = i
		}
	}
	if sec == "" { // before the first table: after the leading comments if there are no keys yet
		if last < 0 {
			first = 0
			for first < len(e.lines) && strings.HasPrefix(strings.TrimSpace(e.lines[first]), "#") {
				first++
			}
			e.insert(first, key+" = "+v)
			if first < e.end() && strings.TrimSpace(e.lines[first+1]) != "" {
				e.insert(first+1, "")
			}
			return true
		}
		e.insert(last+1, key+" = "+v)
		return true
	}
	if last >= 0 {
		e.insert(last+1, key+" = "+v)
		return true
	}
	at := e.end()
	add := []string{"[" + sec + "]", key + " = " + v}
	if at > 0 && strings.TrimSpace(e.lines[at-1]) != "" {
		add = append([]string{""}, add...)
	}
	e.insert(at, add...)
	return true
}

type block struct{ start, end int } // lines [start, end) of one [[server]] table

// serverBlocks finds the [[server]] tables. Comments and blank lines right
// before the next table belong to it, not to this one.
func (e *editor) serverBlocks() []block {
	var bs []block
	for i, l := range e.lines {
		if !isHeader(l) {
			continue
		}
		if n := len(bs); n > 0 && bs[n-1].end < 0 {
			bs[n-1].end = i
		}
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, bom)), "[[") && header(l) == "server" {
			bs = append(bs, block{i, -1})
		}
	}
	for i := range bs {
		if bs[i].end < 0 {
			bs[i].end = e.end()
		}
		for bs[i].end > bs[i].start+1 {
			t := strings.TrimSpace(e.lines[bs[i].end-1])
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			bs[i].end--
		}
	}
	return bs
}

func encodeServer(s Server, eol string) []string {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(struct {
		Server []Server `toml:"server"`
	}{[]Server{s}}); err != nil {
		return nil
	}
	ls := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if eol == "\r\n" {
		for i := range ls {
			ls[i] += "\r"
		}
	}
	return ls
}

// servers brings the [[server]] tables from old to now: an unchanged server
// keeps its lines, a changed one is rewritten, a removed one is dropped and
// a new one is added after the last.
func (e *editor) servers(old, now []Server) bool {
	same := len(old) == len(now)
	for i := 0; same && i < len(old); i++ {
		same = old[i] == now[i]
	}
	if same {
		return true
	}
	bs := e.serverBlocks()
	if len(bs) != len(old) {
		return false
	}
	byName := map[string]Server{}
	for _, s := range now {
		byName[s.Name] = s
	}
	kept := map[string]bool{}
	var out []string
	pos := 0
	var added []string
	addNew := func() {
		for _, s := range now {
			if !kept[s.Name] {
				ls := encodeServer(s, e.eol)
				if ls == nil {
					added = nil
					return
				}
				blank := ""
				if e.eol == "\r\n" {
					blank = "\r"
				}
				added = append(added, append([]string{blank}, ls...)...)
			}
		}
	}
	for i, b := range bs {
		out = append(out, e.lines[pos:b.start]...)
		pos = b.end
		s, ok := byName[old[i].Name]
		switch {
		case !ok: // removed: drop the blank line that separated it, too
			if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" && n > 1 {
				out = out[:n-1]
			}
		case s == old[i]:
			kept[s.Name] = true
			out = append(out, e.lines[b.start:b.end]...)
		default:
			kept[s.Name] = true
			ls := encodeServer(s, e.eol)
			if ls == nil {
				return false
			}
			out = append(out, ls...)
		}
		if i == len(bs)-1 {
			addNew()
			out = append(out, added...)
			added = nil
		}
	}
	out = append(out, e.lines[pos:]...)
	e.lines = out
	if len(bs) == 0 {
		addNew()
		if len(added) == 0 {
			return len(now) == 0
		}
		at := e.end()
		if at > 0 && strings.TrimSpace(e.lines[at-1]) == "" {
			added = added[1:] // already a blank line before
		}
		e.insert(at, added...)
	}
	return true
}
```

Then Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index a39493a..0c0dda1 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -185,21 +185,32 @@ func (c *Config) ActiveServer() (*Server, bool) {
 	return nil, false
 }
 
-// Save writes cfg atomically (temp file + rename) after validating it.
+// Save writes cfg atomically (temp file + rename) after validating it. An
+// existing file is edited in place, so the user's comments survive; a missing
+// or unusual one is written afresh.
 func Save(path string, cfg *Config) error {
 	if err := cfg.Validate(); err != nil {
 		return fmt.Errorf("config: refusing to save invalid config: %w", err)
 	}
-	var buf bytes.Buffer
-	buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in config.example.toml, next to this file.\n")
-	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
-		return fmt.Errorf("config: encode: %w", err)
+	var out []byte
+	if prev, err := os.ReadFile(path); err == nil {
+		if s, ok := editConfig(string(prev), cfg); ok {
+			out = []byte(s) // the user's comments and layout stay
+		}
+	}
+	if out == nil {
+		var buf bytes.Buffer
+		buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in config.example.toml, next to this file.\n")
+		if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
+			return fmt.Errorf("config: encode: %w", err)
+		}
+		out = buf.Bytes()
 	}
 	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
 		return fmt.Errorf("config: %w", err)
 	}
 	tmp := path + ".tmp"
-	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
+	if err := os.WriteFile(tmp, out, 0o600); err != nil {
 		return fmt.Errorf("config: %w", err)
 	}
 	if err := os.Rename(tmp, path); err != nil {
diff --git a/internal/gfx/image.go b/internal/gfx/image.go
index 4334563..5c7467e 100644
--- a/internal/gfx/image.go
+++ b/internal/gfx/image.go
@@ -7,6 +7,7 @@ import (
 	"image/color"
 	_ "image/jpeg" // cover art formats
 	_ "image/png"
+	"math"
 )
 
 // MaxDecodeBytes bounds the memory a cover may take while it is decoded,
@@ -126,54 +127,112 @@ func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
 	}
 	b := m.Bounds()
 	w, h := fitSize(b.Dx(), b.Dy(), maxW, maxH)
-	return boxFilter(b.Dx(), b.Dy(), pixels(m), w, h), nil
+	return boxFilter(b.Dx(), b.Dy(), rowsOf(m), isOpaque(m), w, h), nil
 }
 
 // FromImage converts any image.Image to an Image.
 func FromImage(m image.Image) *Image {
 	b := m.Bounds()
-	return boxFilter(b.Dx(), b.Dy(), pixels(m), b.Dx(), b.Dy())
+	return boxFilter(b.Dx(), b.Dy(), rowsOf(m), isOpaque(m), b.Dx(), b.Dy())
 }
 
-// pixels reads m's pixel (x, y), counted from its top-left corner, as
-// non-premultiplied ARGB. The common decoder outputs are read directly; other
-// types go through At.
-func pixels(m image.Image) func(x, y int) uint32 {
+// rowReader returns source row y as non-premultiplied ARGB. It may fill buf
+// (sw long) and return it, or return its own storage, which must not be changed.
+type rowReader func(y int, buf []uint32) []uint32
+
+// rowsOf reads m's rows, counted from its top-left corner. The common decoder
+// outputs are read directly, a row per call; other types go through At.
+func rowsOf(m image.Image) rowReader {
 	b := m.Bounds()
+	w := b.Dx()
 	switch m := m.(type) {
 	case *image.YCbCr:
-		return func(x, y int) uint32 {
-			yi, ci := m.YOffset(b.Min.X+x, b.Min.Y+y), m.COffset(b.Min.X+x, b.Min.Y+y)
-			r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
-			return 0xFF<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
+		var sh uint // horizontal chroma subsampling: one chroma sample per 1<<sh pixels
+		switch m.SubsampleRatio {
+		case image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420:
+			sh = 1
+		case image.YCbCrSubsampleRatio411, image.YCbCrSubsampleRatio410:
+			sh = 2
+		}
+		cx := make([]int, w) // chroma column of each pixel, relative to the row's first
+		for x := range cx {
+			cx[x] = (b.Min.X+x)/(1<<sh) - b.Min.X/(1<<sh) // once, not per pixel: the A9 has no divide instruction
+		}
+		return func(y int, buf []uint32) []uint32 {
+			y += b.Min.Y
+			yp := m.Y[m.YOffset(b.Min.X, y):]
+			c0 := m.COffset(b.Min.X, y)
+			for x := range w {
+				ci := c0 + cx[x]
+				// color.YCbCrToRGB, inlined
+				yy1 := int32(yp[x]) * 0x10101
+				cb1 := int32(m.Cb[ci]) - 128
+				cr1 := int32(m.Cr[ci]) - 128
+				r := yy1 + 91881*cr1
+				if uint32(r)&0xff000000 == 0 {
+					r >>= 16
+				} else {
+					r = ^(r >> 31)
+				}
+				g := yy1 - 22554*cb1 - 46802*cr1
+				if uint32(g)&0xff000000 == 0 {
+					g >>= 16
+				} else {
+					g = ^(g >> 31)
+				}
+				bl := yy1 + 116130*cb1
+				if uint32(bl)&0xff000000 == 0 {
+					bl >>= 16
+				} else {
+					bl = ^(bl >> 31)
+				}
+				buf[x] = 0xFF<<24 | uint32(uint8(r))<<16 | uint32(uint8(g))<<8 | uint32(uint8(bl))
+			}
+			return buf
 		}
 	case *image.NRGBA:
-		return func(x, y int) uint32 {
-			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
-			return uint32(p[3])<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
+		return func(y int, buf []uint32) []uint32 {
+			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
+			for x := range w {
+				q := p[4*x : 4*x+4 : 4*x+4]
+				buf[x] = uint32(q[3])<<24 | uint32(q[0])<<16 | uint32(q[1])<<8 | uint32(q[2])
+			}
+			return buf
 		}
 	case *image.RGBA:
-		return func(x, y int) uint32 {
-			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
-			a := uint32(p[3])
-			switch a {
-			case 0:
-				return 0
-			case 0xFF:
-				return 0xFF<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
+		return func(y int, buf []uint32) []uint32 {
+			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
+			for x := range w {
+				q := p[4*x : 4*x+4 : 4*x+4]
+				a := uint32(q[3])
+				switch a {
+				case 0:
+					buf[x] = 0
+				case 0xFF:
+					buf[x] = 0xFF<<24 | uint32(q[0])<<16 | uint32(q[1])<<8 | uint32(q[2])
+				default:
+					un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
+					buf[x] = a<<24 | un(q[0])<<16 | un(q[1])<<8 | un(q[2])
+				}
 			}
-			un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
-			return a<<24 | un(p[0])<<16 | un(p[1])<<8 | un(p[2])
+			return buf
 		}
 	case *image.Gray:
-		return func(x, y int) uint32 {
-			g := uint32(m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y)])
-			return 0xFF<<24 | g<<16 | g<<8 | g
+		return func(y int, buf []uint32) []uint32 {
+			p := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
+			for x := range w {
+				g := uint32(p[x])
+				buf[x] = 0xFF<<24 | g<<16 | g<<8 | g
+			}
+			return buf
 		}
 	}
-	return func(x, y int) uint32 {
-		c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
-		return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
+	return func(y int, buf []uint32) []uint32 {
+		for x := range w {
+			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
+			buf[x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
+		}
+		return buf
 	}
 }
 
@@ -200,42 +259,140 @@ func Fit(src *Image, maxW, maxH int) *Image {
 
 // Resize box-filters src down to w×h (use Canvas.Blit for upscaling).
 func Resize(src *Image, w, h int) *Image {
-	return boxFilter(src.W, src.H, func(x, y int) uint32 { return src.Pix[y*src.W+x] }, w, h)
+	return boxFilter(src.W, src.H, func(y int, _ []uint32) []uint32 { return src.Pix[y*src.W : (y+1)*src.W] }, false, w, h)
+}
+
+// isOpaque reports whether every pixel of m has alpha 0xFF (then rowsOf
+// yields 0xFF alpha for all of them). JPEG covers are: YCbCr, Gray.
+func isOpaque(m image.Image) bool {
+	o, ok := m.(interface{ Opaque() bool })
+	return ok && o.Opaque()
 }
 
-// boxFilter averages the sw×sh source read by px down to w×h, weighting
-// colour by alpha. Sums are 64-bit: a box can hold any number of pixels.
-func boxFilter(sw, sh int, px func(x, y int) uint32, w, h int) *Image {
+// boxFilter averages the sw×sh source read by rows down to w×h, weighting
+// colour by alpha; opaque says every source pixel has alpha 0xFF, which
+// allows a cheaper loop. Sums are 64-bit: a box can hold any number of pixels.
+func boxFilter(sw, sh int, rows rowReader, opaque bool, w, h int) *Image {
 	out := NewImage(w, h)
+	buf := make([]uint32, sw)
 	if w == sw && h == sh {
 		for y := 0; y < h; y++ {
-			for x := 0; x < w; x++ {
-				out.Pix[y*w+x] = px(x, y)
-			}
+			copy(out.Pix[y*w:(y+1)*w], rows(y, buf))
 		}
 		return out
 	}
+	sx0, sx1 := make([]int, w), make([]int, w)
+	for x := range w {
+		sx0[x], sx1[x] = x*sw/w, max((x+1)*sw/w, x*sw/w+1)
+	}
+	if opaque && opaqueSumsFit(sw, sh, w, h, sx0, sx1) {
+		boxFilterOpaque(sw, sh, rows, w, h, sx0, sx1, out)
+		return out
+	}
+	// per output column: alpha sum and alpha-weighted colour sums
+	acc := make([][4]uint64, w)
+	var rc recips
 	for y := 0; y < h; y++ {
 		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
-		for x := 0; x < w; x++ {
-			sx0, sx1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
-			var a, r, g, b, n uint64
-			for sy := sy0; sy < sy1; sy++ {
-				for sx := sx0; sx < sx1; sx++ {
-					p := px(sx, sy)
+		clear(acc)
+		for sy := sy0; sy < sy1; sy++ {
+			row := rows(sy, buf)
+			for x := range w {
+				var a, r, g, b uint64
+				for _, p := range row[sx0[x]:sx1[x]] {
 					pa := uint64(p >> 24)
 					a += pa
 					r += uint64(p>>16&0xFF) * pa
 					g += uint64(p>>8&0xFF) * pa
 					b += uint64(p&0xFF) * pa
-					n++
 				}
+				c := &acc[x]
+				c[0] += a
+				c[1] += r
+				c[2] += g
+				c[3] += b
 			}
+		}
+		for x := range w {
+			a, r, g, b := acc[x][0], acc[x][1], acc[x][2], acc[x][3]
 			if a == 0 {
 				continue
 			}
-			out.Pix[y*w+x] = uint32(a/n)<<24 | uint32(r/a)<<16 | uint32(g/a)<<8 | uint32(b/a)
+			n := uint64(sy1-sy0) * uint64(sx1[x]-sx0[x])
+			out.Pix[y*w+x] = uint32(rc.div(a, n))<<24 | uint32(rc.div(r, a))<<16 | uint32(rc.div(g, a))<<8 | uint32(rc.div(b, a))
 		}
 	}
 	return out
 }
+
+// maxRecip is the largest divisor recips handles: 255*k*k must stay below 1<<32.
+const maxRecip = 4104
+
+// recips divides by small numbers with a multiply and a shift, which the
+// MiSTer's Cortex-A9 (no divide instruction: every / is a library call, 64-bit
+// ones a slow one) does much faster. inv[k] is 1<<32/k+1, filled on first use.
+type recips struct{ inv [maxRecip + 1]uint32 }
+
+// div returns s/k for s <= 255*k. With inv = (1<<32+e)/k, 1 <= e <= k, the
+// product s*inv>>32 is s/k plus s*e/(k<<32), which is below 1/k when
+// s*e < 1<<32, and that holds because s*e <= 255*k*k <= 255*maxRecip² < 1<<32:
+// so the floor is exact. Larger divisors are divided the slow way.
+func (r *recips) div(s, k uint64) uint64 {
+	if k == 1 { // 1<<32+1 does not fit inv
+		return s
+	}
+	if k > maxRecip {
+		return s / k
+	}
+	if r.inv[k] == 0 {
+		r.inv[k] = uint32((1<<32)/k + 1)
+	}
+	return uint64(uint32(s)) * uint64(r.inv[k]) >> 32
+}
+
+// opaqueSumsFit reports whether the colour sums of the largest box fit in 32
+// bits: 255 per pixel of the box. A 4:2:0 cover of MaxDecodeBytes (48 MB) is
+// about 32M pixels, so a Gray one can exceed it when scaled to a few pixels.
+func opaqueSumsFit(sw, sh, w, h int, sx0, sx1 []int) bool {
+	cols, rows := 0, 0
+	for x := range sx0 {
+		cols = max(cols, sx1[x]-sx0[x])
+	}
+	for y := range h {
+		rows = max(rows, max((y+1)*sh/h, y*sh/h+1)-y*sh/h)
+	}
+	return 255*int64(cols)*int64(rows) <= math.MaxUint32
+}
+
+// boxFilterOpaque is boxFilter for a source without transparency: with alpha
+// 255 everywhere the alpha-weighted average is the plain one (r*255/(n*255) is
+// the floor of the plain sum over n) and the result's alpha is 255, so the
+// output is the same, with 32-bit sums and no multiply per pixel.
+func boxFilterOpaque(sw, sh int, rows rowReader, w, h int, sx0, sx1 []int, out *Image) {
+	buf := make([]uint32, sw)
+	acc := make([][3]uint32, w)
+	var rc recips
+	for y := 0; y < h; y++ {
+		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
+		clear(acc)
+		for sy := sy0; sy < sy1; sy++ {
+			row := rows(sy, buf)
+			for x := range w {
+				var r, g, b uint32
+				for _, p := range row[sx0[x]:sx1[x]] {
+					r += p >> 16 & 0xFF
+					g += p >> 8 & 0xFF
+					b += p & 0xFF
+				}
+				c := &acc[x]
+				c[0] += r
+				c[1] += g
+				c[2] += b
+			}
+		}
+		for x := range w {
+			n := uint64(sy1-sy0) * uint64(sx1[x]-sx0[x])
+			out.Pix[y*w+x] = 0xFF<<24 | uint32(rc.div(uint64(acc[x][0]), n))<<16 | uint32(rc.div(uint64(acc[x][1]), n))<<8 | uint32(rc.div(uint64(acc[x][2]), n))
+		}
+	}
+}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/config ./internal/gfx`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `go test -count=1 -run '^$' -bench DecodeCover -benchtime 5x ./internal/gfx`. Expected: two `BenchmarkDecodeCover/…` lines. On the PC, scaling alone is about half the old 33 ms.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/config/edit.go internal/config/edit_test.go internal/gfx/decode_bench_test.go internal/gfx/image.go internal/gfx/refbox_test.go
git commit -m "config, gfx: a save edits config.toml in place and keeps its comments; cover scaling without per-pixel division" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 9: Results, docs, and the check on the TV

**Files:**
- Modify:
  - `README.md`
  - `docs/spikes.md`
  - `docs/testing-on-mister.md`
  - `docs/superpowers/plan-2a-followups.md`
  - `docs/superpowers/plans/backlog.md`

**Interfaces:**
- **Consumes:** everything above.
- **Produces:**
  - The README says that a save keeps the comments in `config.toml`.
  - `docs/spikes.md`: "Plan 5 on the MiSTer", with the cover numbers, and "On the TV: pending".
  - Checklist:
    - new items 26 (Settings and the screensaver), 27 (MP3 seeking), 28 (config comments) and 29 (launcher signals);
    - Log becomes 30.
  - The follow-ups doc gains "Resolved by Plan 5".
  - Backlog A keeps only what's left: faster list scrolling at full resolution, faster JPEG decoding, and the remaining minors.

- [ ] **Step 1: Record the results and the new checks**

Save this patch as `/tmp/t9-code.patch` and apply it from the repository root with `git apply /tmp/t9-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 8):

```diff
diff --git a/README.md b/README.md
index 604985e..b0a6ab3 100644
--- a/README.md
+++ b/README.md
@@ -29,7 +29,9 @@ stopped and Super Attract Mode (SAM) is disabled; both come back when you exit (
 or hold B on the home screen). Updates never touch your settings.
 
 Everything the app keeps is in `/media/fat/mistersubsonic/`:
-- `config.toml` holds your settings. `config.example.toml` explains every option.
+- `config.toml` holds your settings. `config.example.toml` explains every option. When a setting
+  is changed in the app, only the values that changed are rewritten; your comments, blank lines
+  and key order stay.
 - `servers/<name>/` holds each server's resume state, scrobble queue and cover cache.
 - `log.txt` and `log.txt.1` hold at most 1 MB each. `crash.txt` records crashes.
 - `fonts/` is optional: `.ttf` or `.otf` fonts put there are used for characters the built-in
diff --git a/docs/spikes.md b/docs/spikes.md
index c3f379e..941680c 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -177,3 +177,20 @@ Measured with the test binaries only (`ui.test -test.bench 'Partial|Repaint'`, `
 - **Crash restore (item 22):** after `kill -9` of the app at 1920×1200, the launcher said "stopped with an error". Its `-restore-console` put the framebuffer back to 960×600 and removed `/tmp/mistersubsonic.fb`, with no kernel errors.
 - **No stale pixels (item 25):** in a 2½-minute run with `-verify-redraw` at 1920×1200, with music playing, the user browsed grids and lists, opened Now Playing and used the volume keys. `log.txt` has 0 "partial redraw differs" lines.
 - **Caution for tools:** reading `/dev/fb0` with `read()` (`head`, `dd`, `cat`) while the app is at full resolution oopses the kernel (`mmiocpy`), because the driver's read path keeps the old window. Use the app's own screenshot (Print Screen) instead.
+
+## Plan 5 on the MiSTer (2026-09-30)
+
+**Cover decoding** (`gfx.test -test.bench DecodeCover`: a 2000×2000 JPEG scaled to 600×600; test binaries only, nothing on the TV):
+
+| | Decode + scale | Scale only |
+|---|---|---|
+| Before Plan 5 | 1.98 s | 1.16 s |
+| Row loops instead of a per-pixel closure | 1.93 s | — |
+| No per-pixel division | 1.76 s | 0.99 s |
+| Opaque fast path (32-bit sums, no alpha weighting) | **1.38 s** | **0.61 s** |
+
+- The Cortex-A9 has no hardware integer divide, so each per-pixel `/` was a call to the software `runtime.udiv`: 17% of the scaling time in a device profile. The box filter now divides with exact multiply-and-shift reciprocals, and the YCbCr row conversion uses a column table.
+- JPEG covers are always opaque, so their averages use plain 32-bit sums. The output is bit-identical to before, and the tests check that exactly.
+- What remains is Go's standard JPEG decoder, about 0.77 s for this size.
+
+**On the TV:** pending. Run `docs/testing-on-mister.md` items 26–29 and record them here.
diff --git a/docs/superpowers/plan-2a-followups.md b/docs/superpowers/plan-2a-followups.md
index ad57aa8..1a56c1c 100644
--- a/docs/superpowers/plan-2a-followups.md
+++ b/docs/superpowers/plan-2a-followups.md
@@ -328,3 +328,55 @@ Still open:
   - The fixtures omit the message, unreachable, feed-with-Resume and search-results screens.
   - The pixel-change check can't judge whether a label is right.
   - On a physical keyboard, the typing hints read "Enter Type" and "Tab Delete", with no "Done". They are true but unidiomatic.
+
+## Resolved by Plan 5
+
+- **Settings and screensaver:**
+  - On/Off rows no longer flip on key repeat.
+  - Hand-edited values continue to the nearest choice.
+  - A toast wakes the screensaver.
+  - No screensaver over the exit prompt.
+  - The waking press's Release is swallowed.
+- **UI text:**
+  - The queue menu is titled "Queue".
+  - Grid R keeps the column.
+  - A partial album-load failure is announced.
+  - Keyboard typing hints and Back hints are right.
+  - `normalizeURL` drops `?query` and `#fragment`.
+  - A screenshot press during a save says so.
+  - The README's screenshot text and the example config's first-run text are fixed.
+  - The tab-row marquee is fixed.
+  - The CRT playlist rows are fixed.
+- **MP3:** seeks inside the buffered window reuse the stream and keep the queued track, and VBR seeks use the Xing/Info TOC.
+- **Stream and player:**
+  - `Promote` allocates outside the lock.
+  - Retry-After is clamped.
+  - 416 and short-EOF use the latest size.
+  - Open is bounded by the budget.
+  - `fromOffset` refuses seeks before its base.
+  - The MP3 estimate's failed rewind is an error.
+  - The missing Open and opener tests are added.
+- **Full resolution and input:**
+  - An unreadable `res_count` fails fast.
+  - The state file holds both sizes, and a stale state is ignored.
+  - The state is kept when a restore fails.
+  - `forceExit` restores the size.
+  - `pads` is cleared on Close.
+  - A failed EVIOCGBIT is logged.
+- **Logs, launcher and tools:**
+  - The log backs off after a failed rotation.
+  - The launcher forwards TERM and INT and finishes its restore.
+  - The lock fd is checked.
+  - `check-notices.sh` uses pipefail and the ARM graph.
+  - `mkdb` guards its flags.
+  - `mister-test` clears `bin/arm/tests`.
+- **Per-server data:**
+  - Folder names are unique per server, and old folders are kept.
+  - The cache size survives a failed walk.
+  - The art failure map is bounded.
+  - The wizard can't be left mid-save.
+  - `popTo` shows only its target.
+  - Starred retries only on A.
+- **Config and covers:**
+  - Saving keeps comments, blank lines and unknown keys.
+  - Cover scaling is 30% faster on the A9 (see `docs/spikes.md`).
diff --git a/docs/superpowers/plans/backlog.md b/docs/superpowers/plans/backlog.md
index c7eb16d..8c29570 100644
--- a/docs/superpowers/plans/backlog.md
+++ b/docs/superpowers/plans/backlog.md
@@ -11,25 +11,11 @@ Suggested order:
 
 ## A. Small follow-ups
 
-**What:** tidy up what the reviews of Plans 2a–3b deferred. No new features.
-
-**Scope:**
-- **Seeks in a sized MP3 reuse the stream.** Every seek in a raw MP3 of known size reopens the stream: a few HTTP requests, and the queued next track is dropped. When the byte estimate falls inside the reader's window, seek that reader and open a fresh decoder on it instead.
-- **Accurate VBR MP3 seeks.** Read the Xing/Info header's 100-point table of contents (in the first frame after the ID3v2 tag), and use it instead of pure proportion. Without a table, keep the proportional estimate.
-- **Text fields trim by width, not by rune** (`drawField`).
-- **Faster list scrolling at full resolution.** Plan 4b made focus moves, the tick and the marquee partial (≤ 12 ms at 1920×1200); a scroll step still redraws the list area, about 40 ms. The A9 is memory-bound (shifting the pixels measured slower than redrawing them), so the next step would be fewer bytes per frame, not a smarter redraw.
-- **The deferred minors** in `docs/superpowers/plan-2a-followups.md`, under "Plan 2c minors", "Plan 3a minors", "Plan 3b minors" and "Plan 4 minors". Pick the ones that still matter, for example:
-  - `Promote` allocating under the stream lock;
-  - the launcher's TERM handling;
-  - the log's rename-failure loop;
-  - toggle rows flipping on key repeat;
-  - `check-notices.sh` without `pipefail`;
-  - the volume panel covering Settings → Playback → Volume on CRT (Plan 4 minors);
-  - the missing tests those reviews listed.
-
-**Needs:** nothing new. It can run on the host, except anything the device plan finds.
-
-**Size:** one plan of about 6–8 tasks.
+Plan 5 did most of this list; see "Resolved by Plan 5" in `docs/superpowers/plan-2a-followups.md`. Still open:
+
+- **Faster list scrolling at full resolution.** Plan 4b made focus moves, the tick and the marquee partial (≤ 12 ms at 1920×1200). A scroll step still redraws the list area, which takes about 40 ms. The A9 is memory-bound: shifting the pixels measured slower than redrawing them. The next step is fewer bytes per frame, not a smarter redraw, and it needs measuring on the device.
+- **Faster JPEG decoding.** Go's standard decoder takes about 0.77 s for a 2000×2000 cover on the A9. Asking the server for smaller covers (it resizes them with `size=`) or a faster decoder would help.
+- **The minors still listed** under "Plan 2c minors" to "Plan 4b minors" in `docs/superpowers/plan-2a-followups.md` that Plan 5 left (cosmetic ones and test gaps).
 
 ---
 
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index fc94a4f..8681370 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -103,7 +103,14 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
     the mini bar are checked too. Browsing is slower in this mode, because
     every partial frame is checked against a full one. After a minute, exit:
     `log.txt` has no "partial redraw differs" line.
-26. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+26. **Settings and the screensaver.**
+    - Hold Right on Settings → Playback → Scrobbling: it flips once.
+    - Let the screensaver start while a track plays, then stop the server or unplug the network: the error's toast wakes the screen.
+    - A key that wakes the screensaver does nothing else. On Now Playing, a Select press that wakes it neither mutes nor changes the mode.
+27. **MP3 seeking.** In a long MP3 album, seek a little forward and back with Left and Right on Now Playing: the next track stays queued (no gap at the end of the track). A VBR MP3 lands near the time shown.
+28. **Config comments.** Add a comment line to `config.toml`, change a setting in the app, and exit: the comment is still there.
+29. **Launcher signals.** Over ssh, run `pkill -TERM -f Scripts/MiSTer_Subsonic.sh` while the app runs: the app exits, and the screen and menu come back as after a normal exit.
+30. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
```

The cover numbers in `docs/spikes.md` were measured on the user's MiSTer while this plan was written, with the test binary only. If the MiSTer answers at Step 4 and the user agrees, run it again after `make deploy-dev`. Correct any figure off by more than 10%.

```bash
./gfx.test -test.run '^$' -test.bench DecodeCover -test.benchtime 5x
```

- [ ] **Step 2: Check the tree**

Run: `go vet ./... && make test && make e2e && make mister mister-test` (zig on `PATH`)

Expected:
- `test-launcher ok`, and `ok` for every Go package.
- `e2e ok: …` and `e2e-ui ok: …`.
- The glibc lines:
  - `mss-cli`, `mistersubsonic`, `audio.test` and `ui.test` need 2.29;
  - `gfx.test` needs no glibc.

- [ ] **Step 3: Commit**

```bash
git add README.md docs/spikes.md docs/superpowers/plan-2a-followups.md docs/superpowers/plans/backlog.md docs/testing-on-mister.md
git commit -m "docs: Plan 5 results; TV checks for settings, MP3 seeks, config comments and the launcher" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 4: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" line and stop here.

- [ ] **Step 5: If it answers, deploy and hand over to the user (ask first)**

Ask the user before doing anything on the device.
- `make deploy MISTER=<ip>` replaces the installed app. Their `config.toml` is kept, and from now on its comments survive a save.
- The checks run on their TV and may play sound. Turn the volume down first.
- Never read `/dev/fb0` on the device. For a picture of the screen, ask the user to press Print Screen and fetch the PNG.

With their go-ahead, deploy. Then ask them to run checklist items 26–29 of `docs/testing-on-mister.md`:
- a held Right flips an On/Off row once;
- a toast wakes the screensaver;
- an MP3 seek keeps the next track;
- a config comment survives a settings change;
- `pkill -TERM` on the launcher restores everything.

Afterwards:
- Record what they report under "Plan 5 on the MiSTer" in `docs/spikes.md`, replacing "pending".
- Commit: `git commit -am "docs: Plan 5 on the TV" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"`.
- Anything that isn't a small fix goes to `docs/superpowers/plans/backlog.md`.

---

## After this plan

- The visualizer (backlog B) and the web or mobile remote (backlog C): the user asked for these to be planned next.
- Faster list scrolling at full resolution, and faster JPEG decoding or smaller covers (backlog A).
- The rest of the TV checklist (CRT margins, BGM and SAM, long files and memory).
- The first release, only when the user asks.
