# MiSTer Subsonic — Plan 4b: full resolution, partial redraws, hotkey hints

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** sharp text at the TV's real resolution, browsing that stays smooth there, and a hint bar on every screen. Also, Now Playing loses its small volume indicator. These come from the user's TV check of Plan 4.

**Architecture:**
- **`internal/platform`:** `fbmode.go` reads `MiSTer.ini` (read only). It asks the menu for the full framebuffer size on `/dev/MiSTer_cmd`, and restores the old size from `/tmp`.
- **`cmd/mistersubsonic`:** `fullres.go` decides whether to switch, opens the framebuffer at the new size, and restores it on exit and in `-restore-console`.
- **`internal/gfx`:** the canvas gets a clip rectangle, which every primitive respects. The framebuffer gets `PresentRects`.
- **`internal/ui`:**
  - **Partial redraws:** `damage.go` keeps a list of damaged rectangles and redraws only those, clipped. The same drawing code runs as for a full frame. A verify mode checks every partial frame against a full one; it is on in every UI test and behind `-verify-redraw` on the device.
  - **Damage sources:**
    - focus moves in lists and grids;
    - the progress tick and the marquee;
    - arriving covers;
    - toasts and the volume panel.
  - **Hint bar:** `hints.go` and `hints_screens.go`.
- **`internal/input`:** events say whether they came from a gamepad.

**Tech Stack:** Go 1.26+, cgo (miniaudio, speexdsp) in `internal/audio` only, no new modules.

**Spec:** `docs/superpowers/specs/2026-09-30-mister-subsonic-plan-4b-design.md` (Plan 4b's design, approved by the user). It extends the main spec, `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`: §8.1 rendering, §8.3 controls, §8.4 input, §10 testing.

**Scope:**

| | |
|---|---|
| **Plans 1–4 (done)** | Playback, the TV interface, setup, MiSTer integration, releases, device hardening, native HDMI, media keys, the volume panel, screenshots |
| **This plan (4b)** | Everything in the Goal, the measurements recorded, and the new TV checks |
| **Later** | Faster list scrolling at full resolution (backlog A), the remaining TV checklist, the first release, the visualizer and the web remote |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:** `go test -race ./...`, the launcher tests, `make e2e` and the ARM builds pass. The ARM builds are `make mister mister-test`, with glibc 2.29 ≤ 2.31, and `gfx.test` needs no glibc.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-4b`. At every task the tests failed before the code and passed after, and the tree ended identical to the prototype's, goldens included.
- **On the MiSTer:** the test binaries only; nothing was drawn on the TV and no sound played.
  - **Profiled:** a full frame.
  - **Measured:**
    - the `Clear` fix;
    - partial frames at 1920×1200;
    - partial framebuffer copies;
    - two rejected ideas, scroll-by-shift and a store-loop copy.
  - **Where the numbers are:** in Task 8's `docs/spikes.md` patch.
- **Goldens:** the prototype looked at the new golden screenshots. They are the hint bar on HDMI 1280×720, 960×600 and 1920×1200 and on CRT 240/288, in the gamepad and keyboard sets, plus Now Playing without the indicator.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:**
  - Go 1.26+ (`go.mod` says `go 1.26.0`).
  - No new modules.
  - cgo only in `internal/audio`.
  - ARM build: `scripts/check-glibc.sh` must report ≤ 2.31, or no glibc for static binaries.
- **The SD card:** nothing new writes to it.
  - `MiSTer.ini` is only read.
  - `/dev/MiSTer_cmd` is a pipe.
  - The saved framebuffer size lives in `/tmp/mistersubsonic.fb`, and `/tmp` is RAM.
- **Full resolution (spec §2):**
  - The app switches only when the output in `MiSTer.ini` is exactly twice the current HDMI framebuffer, and no bigger than 1920×1200.
  - It restores the old size on exit, and `-restore-console` restores it after a crash.
  - It never changes `MiSTer.ini` or any other video setting.
- **Partial redraws (spec §3):**
  - Screens draw as before. Only the pixels written and presented are limited.
  - Anything whose change isn't known exactly redraws the whole frame, as today.
  - The verify mode is on in every UI test.
- **Hints (spec §4):**
  - The bar sits along the bottom of every screen, inside the title-safe area. The screensaver hides it.
  - It shows gamepad buttons or keyboard keys after the last press of each kind.
  - Every hinted button does something on its screen.
  - Settings → Display → Hints turns it off (`[display] hints`).
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device, never a real sound card.
  - Ask the user's go-ahead each time before a listening check, or before anything is shown on the TV.
- **Tests never touch real devices**, such as `/dev/tty*`, `/dev/fb0`, `/dev/input`, `/dev/MiSTer_cmd` or the MiSTer. The platform tests use a fake command file and `/sys` directory in `t.TempDir()`.
- **Secrets:** nothing from `.env` is printed, committed or written into docs.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These are the five situations most likely to bite the user. Each gets its test in the task that owns the code:

1. **Stale pixels after a partial redraw.** On any screen, a focus move, tick, marquee, cover or toast leaves the screen exactly as a full redraw would.
   - Tests: `TestPartialRedrawsMatchFullFramesEverywhere` (Task 5) walks every screen on every layout.
   - It runs with the verify mode (Task 4) on, as every UI test does.
   - `TestVerifyCatchesAMissedDamage` (Task 4) proves the net works.
2. **The app crashes or is killed at full resolution.** The menu and other scripts must get 960×600 back. Test: `TestRestoreConsoleRestoresTheFramebuffer` (Task 6).
3. **An unusual `MiSTer.ini`.** This covers an alternative ini, a preset number, a mode above 1920×1200, and a menu that never confirms. In each case the app keeps the framebuffer it was given and carries on. Tests: `TestOutputMode`, `TestShouldSwitch`, `TestSwitchThatIsNeverConfirmed` (Task 6).
4. **A hint that lies:** a hinted button does nothing on its screen, or a keyboard hint names a key that doesn't do it (Select has none). Tests: `TestEveryHintedButtonDoesSomething`, `TestHintBarFollowsTheInput` (Task 7).
5. **A gamepad and a keyboard used together.** Each press shows its own set, and switching redraws only the bar. Tests: `TestEventsSayWhetherAGamepadSentThem` (Task 2), `TestHintBarFollowsTheInput` (Task 7).

## Decisions this plan makes (from the prototype's measurements; the spec left room)

- **Partial frames have no size limit.** The spec suggested drawing a full frame when the damage covers "about half" the screen. On the MiSTer a partial frame was never slower than a full one, even at 92% of the screen, so every damaged area is drawn partially.
- **Scroll-by-shift is not done.** The spec allowed it if a scroll step missed 40 ms.
  - A scroll step now measures about 41 ms, where a full frame took about 72 ms before.
  - Shifting a 1640×1000 area measured 15.4 ms, against 6.2 ms for redrawing it: on this memory bus it costs more.
- **`Canvas.Clear` uses a local slice.** Profiling showed `Clear` at 37% of a frame, because indexing `c.Pix` reloads the slice on every store. The fix makes full frames 25–30% faster at every size.
- **Gamepad or keyboard is decided per event.** BTN_* codes and axes count as gamepad, and KEY_* codes as keyboard. The devices only decide the starting set (`Manager.HasPad`, from their EV_KEY bits), so a combined device such as the K400's keyboard and touchpad is judged by what each press actually sends.
- **Only the widgets report exact damage.** Lists and grids report it through `Moved()`, but only on screens that opt in with `a.moved(...)`: the views, the album, the playlists, the queue, the genres, the Settings lists and the X menu. The sidebar root, the feed, Search and the wizard keep full frames.
  - `dispatch` adds two generic checks.
    - A changed title damages the header (Artists · B).
    - A changed hint list damages the bar (Servers: the Add row has no menu).
  - Pushing or popping a screen always redraws everything.
- **Volume keys** damage only the panel, except on Settings lists, which show the level in a row.
- **Hints:**
  - Select has no keyboard key, so its hints are left out in the keyboard set.
  - Delete is shown only when there is text to delete.
  - The Servers screen's hints follow the focused row.
- **CRT** still presents full, scaled frames. Its partial drawing is cheap anyway, and the scaler has no partial path.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/ui/screens_play.go`, `volume.go` | Now Playing without the volume indicator | 1 |
| `internal/input/input.go`, `evmap.go`, `evdev_linux.go` | `Event.Pad`, `Manager.HasPad` | 2 |
| `internal/gfx/canvas.go`, `text.go`, `fbpack.go`, `fbdev_linux.go`, `present.go` | clip, the `Clear` fix, `packRect`, `PresentRects`, `PartialPresenter` | 3 |
| `internal/ui/damage.go`, `app.go`, `cmd/mistersubsonic/main.go` | damage list, partial render, verify mode, `-verify-redraw` | 4 |
| `internal/ui/list.go`, `grid.go`, `views.go`, screens, `damage.go`, `app.go`, `volume.go`, `mediakeys.go` | damage sources, benchmarks | 5 |
| `internal/platform/fbmode.go`, `cmd/mistersubsonic/fullres.go`, `main.go`, `internal/config/config.go`, `config.example.toml` | full-resolution framebuffer | 6 |
| `internal/ui/hints.go`, `hints_screens.go`, `app.go`, `screens_settings.go`, `internal/config`, `cmd/mistersubsonic/main.go` | hint bar | 7 |
| `README.md`, `docs/spikes.md`, `docs/testing-on-mister.md`, main spec, backlog | results and TV checks | 8 |

---

### Task 1: Now Playing without the volume indicator

**Files:**
- Modify: `internal/ui/screens_play.go`, `internal/ui/volume.go`
- Test: `internal/ui/volume_test.go` (modified)

**Interfaces:**
- **Produces:**
  - Now Playing's status line is the status plus the shuffle and repeat modes. `drawVolumeInline` is removed.
  - The volume panel is unchanged.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t1-test.patch` and apply it from the repository root with `git apply /tmp/t1-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-4b branch)):

```diff
diff --git a/internal/ui/volume_test.go b/internal/ui/volume_test.go
index c0db220..b9faf1e 100644
--- a/internal/ui/volume_test.go
+++ b/internal/ui/volume_test.go
@@ -59,3 +59,18 @@ func TestVolumeLevel(t *testing.T) {
 		}
 	}
 }
+
+// Now Playing shows no volume of its own: the frame is the same at any
+// level (the panel shows changes; Settings shows the dB).
+func TestNowPlayingDoesNotShowTheVolume(t *testing.T) {
+	ta := newTestApp(t, ProfileHDMI)
+	playingState(ta)
+	ta.Push(NewNowPlayingScreen())
+	ta.pl.st.VolumeDB = -10
+	loud := ta.settle(t).ToRGBA()
+	ta.pl.st.VolumeDB = -45
+	ta.dirty = true
+	if !samePixels(loud, ta.settle(t).ToRGBA()) {
+		t.Fatal("Now Playing changed with the volume")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
--- FAIL: TestNowPlayingDoesNotShowTheVolume (0.01s)
volume_test.go:74: Now Playing changed with the volume
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the plan-4b branch)):

```diff
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 5ebf9ec..1cf2395 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -227,9 +227,7 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			mode += "  ·  " + m.label
 		}
 	}
-	mode += "  ·  "
-	end := iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
-	a.drawVolumeInline(c, fb, end, y+fb.Ascent(), text.Right()-end)
+	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
 	y += fb.Height()
 	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
 		next := st.Queue[st.NextIndex]
diff --git a/internal/ui/volume.go b/internal/ui/volume.go
index 5d67403..f63f45c 100644
--- a/internal/ui/volume.go
+++ b/internal/ui/volume.go
@@ -9,8 +9,7 @@ import (
 )
 
 // The volume indicator: a panel that shows the level for a moment whenever
-// the volume or mute changes (from any screen or key), and a compact
-// speaker and bar on Now Playing.
+// the volume or mute changes (from any screen or key).
 
 // volumeShowTime is how long the panel stays after the last change.
 const volumeShowTime = 1500 * time.Millisecond
@@ -156,19 +155,3 @@ func (a *App) drawVolumePanel(c *gfx.Canvas) {
 	}
 	f.Draw(c, x+(labelW-f.Measure(label))/2, panel.Y+pad+(icon+f.Ascent()-f.Descent())/2, label, col, c.Bounds())
 }
-
-// drawVolumeInline draws the compact indicator at x on the text line at
-// baseline (Now Playing): a small speaker and a short bar, within maxW.
-func (a *App) drawVolumeInline(c *gfx.Canvas, f *gfx.Font, x, baseline, maxW int) {
-	s := f.Ascent()
-	barW := 4 * s
-	if s+s/3+barW > maxW {
-		return
-	}
-	db := a.volumeDB()
-	level := volumeLevel(db)
-	col := colDim
-	drawSpeaker(c, gfx.R(x, baseline-s, s, s), level, a.muted, col)
-	bh := max(s*2/5, 3)
-	drawVolumeBar(c, gfx.R(x+s+s/3, baseline-(s+bh)/2, barW, bh), level, a.muted, volumeSegments/2)
-}
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `nowplaying-crt`, `nowplaying-hdmi-960x600`, `nowplaying-hdmi`, `nowplaying-starred-crt`, `nowplaying-starred-hdmi`. Open each one and check it: the `nowplaying-*` screens end the status line at `▶ Playing` (plus the modes), with no speaker or bar.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/screens_play.go internal/ui/volume.go internal/ui/volume_test.go internal/ui/testdata/golden
git commit -m "ui: Now Playing shows no volume of its own; the panel shows changes" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Input events say whether a gamepad sent them

**Files:**
- Modify: `internal/input/input.go`, `internal/input/evmap.go`, `internal/input/evdev_linux.go`
- Test: `internal/input/input_test.go`, `internal/input/evdev_linux_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `input.Event.Pad bool`. The translator sets it on every event: true for BTN_* codes (`code >= btnMisc`, 0x100) and for axes and hats, false for KEY_* codes, media keys included.
  - `Repeater` repeats keep the `Pad` of the held press.
  - `(*Manager).HasPad() bool`: a gamepad is connected. A device is a gamepad when its EV_KEY bits (`padKeys`, `keyBitsLen`) include BTN_JOYSTICK..BTN_THUMBR or the D-pad buttons. An unplugged device is forgotten.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/input/evdev_linux_test.go b/internal/input/evdev_linux_test.go
index d5f89e3..1e15fbc 100644
--- a/internal/input/evdev_linux_test.go
+++ b/internal/input/evdev_linux_test.go
@@ -116,3 +116,31 @@ func TestScanPrunesStalePlaceholders(t *testing.T) {
 		t.Fatal("stale placeholder not pruned")
 	}
 }
+
+// A device is a gamepad when its key bits include the joystick or gamepad
+// buttons (BTN_JOYSTICK..BTN_THUMBR, or the D-pad buttons); a keyboard
+// with mouse buttons or media keys is not.
+func TestGamepadKeyBits(t *testing.T) {
+	bits := func(codes ...uint16) []byte {
+		b := make([]byte, keyBitsLen)
+		for _, c := range codes {
+			b[c/8] |= 1 << (c % 8)
+		}
+		return b
+	}
+	for _, c := range []struct {
+		name  string
+		codes []uint16
+		want  bool
+	}{
+		{"keyboard", []uint16{keyEnter, keyEsc, keyVolumeUp, keyPlayPause}, false},
+		{"keyboard with a touchpad's mouse buttons", []uint16{keyEnter, 0x110, 0x111}, false},
+		{"gamepad", []uint16{btnSouth, btnEast, btnStart}, true},
+		{"joystick", []uint16{0x120}, true},
+		{"d-pad buttons only", []uint16{btnDpadUp}, true},
+	} {
+		if got := padKeys(bits(c.codes...)); got != c.want {
+			t.Errorf("%s: padKeys = %v, want %v", c.name, got, c.want)
+		}
+	}
+}
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index dc9822d..b9889ff 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -74,10 +74,10 @@ func TestParseMisterMap(t *testing.T) {
 
 func TestTranslatorKeysAndMapOverride(t *testing.T) {
 	tr := newTranslator(map[uint16]Button{btnSouth: BtnA}, nil)
-	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press}) {
+	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press, Pad: true}) {
 		t.Fatalf("mapped south = %v", got)
 	}
-	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{Button: BtnA, Kind: Press}) {
+	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{Button: BtnA, Kind: Press, Pad: true}) {
 		t.Fatalf("default east = %v", got)
 	}
 	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{Button: BtnA, Kind: Release, Rune: '\n'}) {
@@ -93,22 +93,22 @@ func TestTranslatorKeysAndMapOverride(t *testing.T) {
 
 func TestTranslatorHatAndStick(t *testing.T) {
 	tr := newTranslator(nil, map[uint16]AbsRange{absX: {0, 255}})
-	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{Button: BtnUp, Kind: Press}) {
+	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{Button: BtnUp, Kind: Press, Pad: true}) {
 		t.Fatalf("hat up = %v", got)
 	}
-	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{Button: BtnUp, Kind: Release}) || got[1] != (Event{Button: BtnDown, Kind: Press}) {
+	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{Button: BtnUp, Kind: Release, Pad: true}) || got[1] != (Event{Button: BtnDown, Kind: Press, Pad: true}) {
 		t.Fatalf("hat up→down = %v", got)
 	}
-	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Release}) {
+	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Release, Pad: true}) {
 		t.Fatalf("hat centre = %v", got)
 	}
 	if got := tr.handle(evAbs, absX, 150); got != nil {
 		t.Fatalf("small stick movement = %v", got)
 	}
-	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Press}) {
+	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Press, Pad: true}) {
 		t.Fatalf("stick right = %v", got)
 	}
-	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Release}) {
+	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Release, Pad: true}) {
 		t.Fatalf("stick centre = %v", got)
 	}
 	if got := tr.handle(evAbs, absY, 0); got != nil {
@@ -227,3 +227,42 @@ func TestMediaKeys(t *testing.T) {
 		t.Error("play/pause or next repeats when held")
 	}
 }
+
+// Events say whether a gamepad sent them (buttons at BTN_* codes, sticks
+// and hats) or a keyboard (every KEY_* code, media keys too), so the UI can
+// show the matching hints. Repeats keep the source of the held press.
+func TestEventsSayWhetherAGamepadSentThem(t *testing.T) {
+	tr := newTranslator(map[uint16]Button{btnEast: BtnA}, map[uint16]AbsRange{absX: {0, 255}})
+	for _, c := range []struct {
+		typ, code uint16
+		value     int32
+		pad       bool
+	}{
+		{evKey, keyEnter, 1, false},
+		{evKey, keyVolumeUp, 1, false},
+		{evKey, btnEast, 1, true},
+		{evKey, btnDpadUp, 1, true},
+		{evAbs, absHat0X, -1, true},
+		{evAbs, absX, 0, true},
+	} {
+		got := tr.handle(c.typ, c.code, c.value)
+		if len(got) == 0 {
+			t.Fatalf("%d/%d gave no event", c.typ, c.code)
+		}
+		for _, e := range got {
+			if e.Pad != c.pad {
+				t.Errorf("%d/%d: Pad = %v, want %v", c.typ, c.code, e.Pad, c.pad)
+			}
+		}
+	}
+	var r Repeater
+	now := time.Unix(0, 0)
+	r.Feed(Event{Button: BtnDown, Kind: Press, Pad: true}, now)
+	if got := r.Due(now.Add(RepeatDelay)); len(got) != 1 || !got[0].Pad {
+		t.Fatalf("repeat of a gamepad press = %+v", got)
+	}
+	r.Feed(Event{Button: BtnDown, Kind: Press}, now)
+	if got := r.Due(now.Add(RepeatDelay)); len(got) != 1 || got[0].Pad {
+		t.Fatalf("repeat of a key press = %+v", got)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/input`

Expected: FAIL, e.g.:

```
undefined: keyBitsLen
undefined: padKeys
unknown field Pad in struct literal of type Event
e.Pad undefined (type Event has no field or method Pad)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/input/evdev_linux.go b/internal/input/evdev_linux.go
index b29def6..d245199 100644
--- a/internal/input/evdev_linux.go
+++ b/internal/input/evdev_linux.go
@@ -103,6 +103,7 @@ type Manager struct {
 	mu     sync.Mutex
 	closed bool
 	devs   map[string]*os.File
+	pads   map[string]bool // devices that are gamepads
 }
 
 // NewManager starts scanning immediately.
@@ -116,7 +117,7 @@ func NewManager(o ManagerOptions) *Manager {
 	if o.Rescan <= 0 {
 		o.Rescan = 2 * time.Second
 	}
-	m := &Manager{o: o, events: make(chan Event, 64), quit: make(chan struct{}), devs: map[string]*os.File{}}
+	m := &Manager{o: o, events: make(chan Event, 64), quit: make(chan struct{}), devs: map[string]*os.File{}, pads: map[string]bool{}}
 	m.scan()
 	m.loopWg.Add(1)
 	go m.rescanLoop()
@@ -204,6 +205,36 @@ func hasEventType(f *os.File, typ uint) bool {
 	return bits[typ/8]&(1<<(typ%8)) != 0
 }
 
+// keyBitsLen covers every key code up to KEY_MAX (0x2ff).
+const keyBitsLen = 0x300 / 8
+
+// padKeys reports whether a device's EV_KEY bits include joystick or gamepad
+// buttons (BTN_JOYSTICK 0x120 .. BTN_THUMBR 0x13e) or the D-pad buttons.
+func padKeys(bits []byte) bool {
+	has := func(c uint16) bool { return int(c/8) < len(bits) && bits[c/8]&(1<<(c%8)) != 0 }
+	for c := uint16(0x120); c <= 0x13e; c++ {
+		if has(c) {
+			return true
+		}
+	}
+	return has(btnDpadUp) || has(btnDpadDn) || has(btnDpadL) || has(btnDpadR)
+}
+
+func isGamepad(f *os.File) bool {
+	bits := make([]byte, keyBitsLen)
+	if err := fileIoctlPtr(f, eviocgbit(evKey, keyBitsLen), unsafe.Pointer(&bits[0])); err != nil {
+		return false
+	}
+	return padKeys(bits)
+}
+
+// HasPad reports whether a gamepad is connected.
+func (m *Manager) HasPad() bool {
+	m.mu.Lock()
+	defer m.mu.Unlock()
+	return len(m.pads) > 0
+}
+
 func absRange(f *os.File, code uint16) (AbsRange, bool) {
 	var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
 	if err := fileIoctlPtr(f, eviocgabs(uintptr(code)), unsafe.Pointer(&info[0])); err != nil {
@@ -246,6 +277,11 @@ func (m *Manager) open(path string) {
 			log.Printf("input: grab %s (%s): %v", path, name, err)
 		}
 	}
+	if isGamepad(f) {
+		m.mu.Lock()
+		m.pads[path] = true
+		m.mu.Unlock()
+	}
 	m.attach(path, f, newTranslator(keys, abs))
 }
 
@@ -275,6 +311,7 @@ func (m *Manager) read(path string, f *os.File, tr *translator) {
 			m.mu.Lock()
 			if m.devs[path] == f {
 				delete(m.devs, path) // unplugged: rescan may pick it up again
+				delete(m.pads, path)
 				f.Close()
 			}
 			m.mu.Unlock()
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index 86eabc3..deee477 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -51,6 +51,7 @@ const (
 	keyDown         = 108
 	keyKPEnter      = 96
 
+	btnMisc   = 0x100 // the first BTN_* code: below it are keyboard keys
 	btnSouth  = 0x130
 	btnEast   = 0x131
 	btnNorth  = 0x133
@@ -146,6 +147,15 @@ func newTranslator(keys map[uint16]Button, abs map[uint16]AbsRange) *translator
 // handle converts a raw event. value: 1 press, 0 release, 2 kernel
 // autorepeat (buttons ignore it, the app repeats them itself; typing repeats).
 func (t *translator) handle(typ, code uint16, value int32) []Event {
+	evs := t.translate(typ, code, value)
+	pad := typ == evAbs || code >= btnMisc // gamepad buttons and sticks; keyboard keys are below BTN_MISC
+	for i := range evs {
+		evs[i].Pad = pad
+	}
+	return evs
+}
+
+func (t *translator) translate(typ, code uint16, value int32) []Event {
 	switch typ {
 	case evKey:
 		b, ok := t.keys[code]
diff --git a/internal/input/input.go b/internal/input/input.go
index 50cd69a..7636b12 100644
--- a/internal/input/input.go
+++ b/internal/input/input.go
@@ -67,6 +67,7 @@ type Event struct {
 	Button Button
 	Kind   Kind
 	Rune   rune
+	Pad    bool // sent by a gamepad (its buttons, stick or hat), not a keyboard
 }
 
 // Repeat timing (spec §8.4).
@@ -92,6 +93,7 @@ func repeats(b Button) bool {
 // Due whenever NextDeadline passes.
 type Repeater struct {
 	held  Button
+	pad   bool // the held press came from a gamepad
 	next  time.Time
 	count int
 }
@@ -101,7 +103,7 @@ func (r *Repeater) Feed(e Event, now time.Time) {
 	switch e.Kind {
 	case Press:
 		if repeats(e.Button) {
-			r.held, r.next, r.count = e.Button, now.Add(RepeatDelay), 0
+			r.held, r.pad, r.next, r.count = e.Button, e.Pad, now.Add(RepeatDelay), 0
 		} else {
 			r.held = BtnNone
 		}
@@ -136,5 +138,5 @@ func (r *Repeater) Due(now time.Time) []Event {
 	if !r.next.After(now) {
 		r.next = now.Add(step)
 	}
-	return []Event{{Button: r.held, Kind: Repeat}}
+	return []Event{{Button: r.held, Kind: Repeat, Pad: r.pad}}
 }
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/input`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `GOOS=linux GOARCH=arm GOARM=7 go vet ./internal/gfx ./internal/input ./internal/platform`. Expected: no output (the ARM build of the new code vets clean).

- [ ] **Step 5: Commit**

```bash
git add internal/input/evdev_linux.go internal/input/evdev_linux_test.go internal/input/evmap.go internal/input/input.go internal/input/input_test.go
git commit -m "input: events and devices tell a gamepad from a keyboard" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: gfx: a clip rectangle, partial presents, and a faster Clear

**Files:**
- Modify: `internal/gfx/canvas.go`, `internal/gfx/text.go`, `internal/gfx/fbpack.go`, `internal/gfx/fbdev_linux.go`, `internal/gfx/present.go`
- Test: `internal/gfx/clip_test.go` (new); `internal/gfx/fbdev_linux_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `(*Canvas).SetClip(r)`, `ClearClip()` and `Clip() Rect`. The clip is cut to the canvas, and is the whole canvas when unset.
  - `Fill`, `Blit`, `Font.Draw` and `Clear` write only inside the clip. A clipped `Clear` fills the clip.
  - `Clear` loops over a local slice. Indexing `c.Pix` reloads the slice on every store, and this change makes it about 3× faster on the A9.
  - `fbFormat.packRect(mem, c, r)` copies only r. `pack` is `packRect` over the whole frame.
  - `(*FB).PresentRects(c, rs)`, and the `gfx.PartialPresenter` interface.

- [ ] **Step 1: Write the failing tests**

`internal/gfx/clip_test.go` (new file):

```go
package gfx

import (
	"math/rand/v2"
	"testing"
)

// Every drawing call writes only inside the clip: inside it the result is
// what an unclipped call gives, outside it the canvas is untouched. This is
// what lets the UI redraw just the parts of a frame that changed.
func TestClipLimitsEveryPrimitive(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	f := testFont(t, 18)
	img := NewImage(40, 30)
	for i := range img.Pix {
		img.Pix[i] = r.Uint32() // every alpha, opaque and transparent included
	}
	draws := map[string]func(c *Canvas){
		"fill opaque":  func(c *Canvas) { c.Fill(R(5, 5, 90, 50), RGB(10, 200, 30)) },
		"fill blended": func(c *Canvas) { c.Fill(R(-10, 20, 200, 30), RGBA(200, 10, 30, 0x80)) },
		"blit 1:1":     func(c *Canvas) { c.Blit(img, R(20, 10, 40, 30)) },
		"blit scaled":  func(c *Canvas) { c.Blit(img, R(0, 0, 100, 70)) },
		"text":         func(c *Canvas) { f.Draw(c, 3, 40, "Капитан Africa", RGB(250, 250, 250), c.Bounds()) },
		"text blended": func(c *Canvas) { f.Draw(c, 3, 60, "Капитан Africa", RGBA(250, 250, 250, 0x90), c.Bounds()) },
		"clear":        func(c *Canvas) { c.Clear(RGB(1, 2, 3)) },
	}
	clips := []Rect{R(30, 15, 25, 20), R(-5, -5, 20, 20), R(90, 60, 50, 50), R(0, 0, 0, 0)}
	for name, draw := range draws {
		for _, clip := range clips {
			base := randomCanvas(r, 100, 70)
			want := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
			draw(want)
			got := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
			got.SetClip(clip)
			draw(got)
			in := clip.Intersect(base.Bounds())
			for y := range base.H {
				for x := range base.W {
					exp := base.Pix[y*base.W+x]
					if in.Contains(x, y) {
						exp = want.Pix[y*base.W+x]
					}
					if g := got.Pix[y*base.W+x]; g != exp {
						t.Fatalf("%s, clip %v: pixel (%d,%d) = %06x, want %06x", name, clip, x, y, g, exp)
					}
				}
			}
		}
	}
}

func TestClipStaysInsideTheCanvas(t *testing.T) {
	c := NewCanvas(100, 50)
	if c.Clip() != c.Bounds() {
		t.Fatalf("a new canvas clips to %v", c.Clip())
	}
	c.SetClip(R(80, -10, 50, 30))
	if c.Clip() != R(80, 0, 20, 20) {
		t.Fatalf("clip %v, want it cut to the canvas", c.Clip())
	}
	c.ClearClip()
	if c.Clip() != c.Bounds() {
		t.Fatalf("after ClearClip the clip is %v", c.Clip())
	}
}

// packRect copies only its rectangle: inside, the framebuffer holds what a
// full pack gives; outside (and the line padding) it keeps what it had.
func TestPackRectCopiesOnlyItsRectangle(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, bpp := range []int{32, 16} {
		f := fbFormat{width: 50, height: 30, stride: 50*bpp/8 + 12, bpp: bpp}
		old, next := randomCanvas(r, 50, 30), randomCanvas(r, 50, 30)
		for _, rect := range []Rect{R(10, 5, 20, 10), R(-5, 25, 100, 20), R(0, 0, 50, 30), R(49, 0, 1, 1)} {
			mem := make([]byte, f.stride*f.height)
			for i := range mem {
				mem[i] = 0xEE
			}
			f.pack(mem, old)
			f.packRect(mem, next, rect)
			full := make([]byte, len(mem))
			for i := range full {
				full[i] = 0xEE
			}
			f.pack(full, next)
			in := rect.Intersect(R(0, 0, 50, 30))
			for y := range f.height {
				for x := range f.width {
					want := f.encode(old.Pix[y*50+x])
					if in.Contains(x, y) {
						want = f.read(full, x, y)
					}
					if got := f.read(mem, x, y); got != want {
						t.Fatalf("%d bpp, rect %v: pixel (%d,%d) = %x, want %x", bpp, rect, x, y, got, want)
					}
				}
				for i := f.width * bpp / 8; i < f.stride; i++ {
					if mem[y*f.stride+i] != 0xEE {
						t.Fatalf("%d bpp: line %d padding written", bpp, y)
					}
				}
			}
		}
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/gfx/fbdev_linux_test.go b/internal/gfx/fbdev_linux_test.go
index 1942bc1..4b19793 100644
--- a/internal/gfx/fbdev_linux_test.go
+++ b/internal/gfx/fbdev_linux_test.go
@@ -104,3 +104,27 @@ func TestIntactNoticesAnOverwrite(t *testing.T) {
 		}
 	}
 }
+
+// PresentRects updates the framebuffer in the rectangles only, and the
+// watchdog then compares against the new frame.
+func TestPresentRectsUpdatesOnlyTheRectangles(t *testing.T) {
+	f := fbFormat{width: 64, height: 48, stride: 64 * 4, bpp: 32}
+	b := &FB{mem: make([]byte, f.stride*f.height), fmt: f}
+	old, next := NewCanvas(64, 48), NewCanvas(64, 48)
+	old.Clear(RGB(1, 1, 1))
+	next.Clear(RGB(1, 1, 1))
+	next.Fill(R(10, 10, 5, 5), RGB(200, 0, 0))
+	b.Present(old)
+	if err := b.PresentRects(next, []Rect{R(10, 10, 5, 5)}); err != nil {
+		t.Fatal(err)
+	}
+	if got := f.read(b.mem, 12, 12); got != 0xC80000 {
+		t.Fatalf("inside = %06x", got)
+	}
+	if !b.Intact() {
+		t.Fatal("the watchdog doesn't see the updated frame")
+	}
+	if err := b.PresentRects(NewCanvas(10, 10), nil); err == nil {
+		t.Fatal("a frame of the wrong size was accepted")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/gfx`

Expected: FAIL, e.g.:

```
got.SetClip undefined (type *Canvas has no field or method SetClip)
c.Clip undefined (type *Canvas has no field or method Clip)
c.SetClip undefined (type *Canvas has no field or method SetClip)
c.ClearClip undefined (type *Canvas has no field or method ClearClip)
f.packRect undefined (type fbFormat has no field or method packRect)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/gfx/canvas.go b/internal/gfx/canvas.go
index fd5304b..69de1d8 100644
--- a/internal/gfx/canvas.go
+++ b/internal/gfx/canvas.go
@@ -38,28 +38,53 @@ func (r Rect) Intersect(o Rect) Rect {
 func (r Rect) Inset(d int) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }
 
 // Canvas is an opaque XRGB8888 pixel buffer (0x00RRGGBB, row-major).
+// Drawing writes only inside its clip (the whole canvas unless SetClip
+// narrowed it), so a frame can be redrawn in parts.
 type Canvas struct {
 	W, H int
 	Pix  []uint32
+
+	clip    Rect
+	clipped bool
 }
 
 func NewCanvas(w, h int) *Canvas { return &Canvas{W: w, H: h, Pix: make([]uint32, w*h)} }
 
 func (c *Canvas) Bounds() Rect { return Rect{0, 0, c.W, c.H} }
 
+// Clip is where drawing may write.
+func (c *Canvas) Clip() Rect {
+	if c.clipped {
+		return c.clip
+	}
+	return c.Bounds()
+}
+
+// SetClip limits every drawing call to r (cut to the canvas) until
+// ClearClip.
+func (c *Canvas) SetClip(r Rect) { c.clip, c.clipped = r.Intersect(c.Bounds()), true }
+
+// ClearClip lets drawing write anywhere on the canvas again.
+func (c *Canvas) ClearClip() { c.clipped = false }
+
 func (c *Canvas) At(x, y int) Color { return Color(c.Pix[y*c.W+x]) | 0xFF000000 }
 
-// Clear fills the whole canvas with an opaque color.
+// Clear fills the canvas (its clip, when one is set) with an opaque color.
 func (c *Canvas) Clear(col Color) {
+	if c.clipped {
+		c.Fill(c.clip, col|0xFF000000)
+		return
+	}
 	v := uint32(col) & 0xFFFFFF
-	for i := range c.Pix {
-		c.Pix[i] = v
+	pix := c.Pix // a local slice: indexing c.Pix reloads it on every store (3× slower on the A9)
+	for i := range pix {
+		pix[i] = v
 	}
 }
 
 // Fill paints r, blending when col has alpha < 255.
 func (c *Canvas) Fill(r Rect, col Color) {
-	r = r.Intersect(c.Bounds())
+	r = r.Intersect(c.Clip())
 	a := col.A()
 	if r.Empty() || a == 0 {
 		return
@@ -95,9 +120,9 @@ type Image struct {
 
 func NewImage(w, h int) *Image { return &Image{W: w, H: h, Pix: make([]uint32, w*h)} }
 
-// Blit draws src scaled (nearest neighbour) into dst, clipped to the canvas.
+// Blit draws src scaled (nearest neighbour) into dst, within the clip.
 func (c *Canvas) Blit(src *Image, dst Rect) {
-	clip := dst.Intersect(c.Bounds())
+	clip := dst.Intersect(c.Clip())
 	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 {
 		return
 	}
diff --git a/internal/gfx/fbdev_linux.go b/internal/gfx/fbdev_linux.go
index b986439..6ac33d6 100644
--- a/internal/gfx/fbdev_linux.go
+++ b/internal/gfx/fbdev_linux.go
@@ -169,6 +169,22 @@ func (b *FB) Present(c *Canvas) error {
 	return nil
 }
 
+// PresentRects shows the rectangles rs of c, a full-size frame whose other
+// pixels are already on screen.
+func (b *FB) PresentRects(c *Canvas, rs []Rect) error {
+	if b.mem == nil {
+		return errors.New("gfx: framebuffer closed")
+	}
+	if c.W != b.fmt.width || c.H != b.fmt.height {
+		return fmt.Errorf("gfx: frame %dx%d != framebuffer %dx%d", c.W, c.H, b.fmt.width, b.fmt.height)
+	}
+	for _, r := range rs {
+		b.fmt.packRect(b.mem, c, r)
+	}
+	b.last = c
+	return nil
+}
+
 // Intact reports whether the screen still shows the frame last presented,
 // from a grid of sampled pixels: Main_MiSTer or the console may have drawn
 // over it (spec §8.1). It is true before the first Present and after Close.
diff --git a/internal/gfx/fbpack.go b/internal/gfx/fbpack.go
index f51628f..b469b8d 100644
--- a/internal/gfx/fbpack.go
+++ b/internal/gfx/fbpack.go
@@ -27,12 +27,19 @@ func (f fbFormat) validate() error {
 }
 
 // pack writes c (same size as the framebuffer) into mem.
-func (f fbFormat) pack(mem []byte, c *Canvas) {
-	for y := 0; y < f.height; y++ {
-		src := c.Pix[y*c.W : y*c.W+f.width]
-		line := mem[y*f.stride:]
+func (f fbFormat) pack(mem []byte, c *Canvas) { f.packRect(mem, c, Rect{0, 0, f.width, f.height}) }
+
+// packRect writes the part of c inside r into mem, leaving the rest as it is.
+func (f fbFormat) packRect(mem []byte, c *Canvas, r Rect) {
+	r = r.Intersect(Rect{0, 0, f.width, f.height})
+	if r.Empty() {
+		return
+	}
+	for y := r.Y; y < r.Bottom(); y++ {
+		src := c.Pix[y*c.W+r.X : y*c.W+r.Right()]
+		line := mem[y*f.stride+r.X*f.bpp/8:]
 		if f.bpp == 32 && littleEndian {
-			copy(line[:4*f.width], unsafe.Slice((*byte)(unsafe.Pointer(&src[0])), 4*f.width))
+			copy(line[:4*r.W], unsafe.Slice((*byte)(unsafe.Pointer(&src[0])), 4*r.W))
 			continue
 		}
 		if f.bpp == 32 {
diff --git a/internal/gfx/present.go b/internal/gfx/present.go
index 0f3857a..cf12293 100644
--- a/internal/gfx/present.go
+++ b/internal/gfx/present.go
@@ -10,6 +10,13 @@ type Display interface {
 	Close() error
 }
 
+// PartialPresenter is a Display that can update parts of the screen.
+type PartialPresenter interface {
+	// PresentRects shows the rectangles rs of c, a full-size frame whose
+	// other pixels are already on screen.
+	PresentRects(c *Canvas, rs []Rect) error
+}
+
 // Checker is a Display that can tell whether something else drew over its
 // last frame (the MiSTer framebuffer: Main_MiSTer or the console).
 type Checker interface {
diff --git a/internal/gfx/text.go b/internal/gfx/text.go
index c5ab504..ca4dc1a 100644
--- a/internal/gfx/text.go
+++ b/internal/gfx/text.go
@@ -164,7 +164,7 @@ func (f *Font) Measure(s string) int {
 // Draw renders s with its baseline at y, starting at x, clipped to clip.
 // It returns the x after the last glyph.
 func (f *Font) Draw(c *Canvas, x, y int, s string, col Color, clip Rect) int {
-	clip = clip.Intersect(c.Bounds())
+	clip = clip.Intersect(c.Clip())
 	v := uint32(col) & 0xFFFFFF
 	alpha := col.A()
 	for _, r := range s {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/gfx`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/gfx/canvas.go internal/gfx/clip_test.go internal/gfx/fbdev_linux.go internal/gfx/fbdev_linux_test.go internal/gfx/fbpack.go internal/gfx/present.go internal/gfx/text.go
git commit -m "gfx: a clip for partial redraws, PresentRects, and a Clear that doesn't reload its slice" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Partial frames and the verify mode

**Files:**
- Create: `internal/ui/damage.go`
- Modify: `internal/ui/app.go`, `cmd/mistersubsonic/main.go`
- Test: `internal/ui/damage_test.go` (new); `internal/ui/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `(*App).Damage(r gfx.Rect)`, and `App.damage []gfx.Rect` beside `dirty`, which still means the whole frame.
  - `render()`:
    - When nothing is marked dirty and there is damage, `renderDamage` runs:
      - it merges the rectangles (`mergeRects`, at most `maxDamageRects` = 4, else their bounding box);
      - it runs `drawFrame` clipped to each one;
      - it presents them with `presentRects`, through `PartialPresenter` when the display has it and no scaler is in use, and in full otherwise.
    - With no damage it draws the full frame, as today.
  - `drawFrame(c)` is the old body of `render`.
  - The verify mode:
    - `App.verify` and `App.verifyFail`.
    - `checkPartial()` draws a full frame on the side and compares the two.
    - On a mismatch, tests get `verifyFail`, the device gets a log line, and the full frame is drawn.
    - `newTestApp` turns it on in every UI test.
  - `ui.Options.VerifyRedraw` and `mistersubsonic -verify-redraw`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/damage_test.go` (new file):

```go
package ui

import (
	"strings"
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// rectDisplay is a headless display that takes partial frames, and records
// what it was given.
type rectDisplay struct {
	*gfx.Headless
	rects [][]gfx.Rect // one entry per PresentRects
	fulls int
}

func (d *rectDisplay) Present(c *gfx.Canvas) error { d.fulls++; return d.Headless.Present(c) }

func (d *rectDisplay) PresentRects(c *gfx.Canvas, rs []gfx.Rect) error {
	d.rects = append(d.rects, append([]gfx.Rect(nil), rs...))
	return d.Headless.Present(c)
}

// boxScreen paints one coloured box on its background.
type boxScreen struct {
	box gfx.Rect
	col gfx.Color
}

func (s *boxScreen) Title() string                          { return "Box" }
func (s *boxScreen) Enter(*App)                             {}
func (s *boxScreen) Handle(*App, input.Event) bool          { return false }
func (s *boxScreen) Draw(a *App, c *gfx.Canvas, _ gfx.Rect) { c.Fill(s.box, s.col) }

func newRectApp(t *testing.T) (*testApp, *rectDisplay, *boxScreen) {
	t.Helper()
	ta := newTestApp(t, ProfileHDMI)
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	s := &boxScreen{box: gfx.R(100, 200, 40, 30), col: gfx.RGB(200, 0, 0)}
	ta.Push(s)
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	return ta, d, s
}

// A change that damages its area is redrawn and presented there only; the
// frame then equals a full redraw (the tests' verify mode checks that).
func TestDamageRedrawsAndPresentsOnlyTheChangedArea(t *testing.T) {
	ta, d, s := newRectApp(t)
	s.col = gfx.RGB(0, 200, 0)
	ta.Damage(s.box)
	c := ta.settle(t)
	if d.fulls != 0 || len(d.rects) != 1 || len(d.rects[0]) != 1 || d.rects[0][0] != s.box {
		t.Fatalf("presented %v rects and %d full frames, want just %v", d.rects, d.fulls, s.box)
	}
	if got := c.At(110, 210); got != gfx.RGB(0, 200, 0) {
		t.Fatalf("box = %08x", got)
	}
}

// Damage over more than half the screen draws the full frame instead.
func TestLargeDamageDrawsTheFullFrame(t *testing.T) {
	ta, d, _ := newRectApp(t)
	ta.Damage(gfx.R(0, 0, ta.P.W, ta.P.H*3/4))
	ta.settle(t)
	if d.fulls != 1 || len(d.rects) != 0 {
		t.Fatalf("presented %v rects and %d full frames, want one full frame", d.rects, d.fulls)
	}
}

// The verify mode catches a change whose damage misses it: the frame is
// then drawn in full, so the screen never keeps stale pixels.
func TestVerifyCatchesAMissedDamage(t *testing.T) {
	ta, d, s := newRectApp(t)
	var failed string
	ta.verifyFail = func(m string) { failed = m }
	s.col = gfx.RGB(0, 0, 200)
	ta.Damage(gfx.R(0, 0, 10, 10)) // not the box
	c := ta.settle(t)
	if !strings.Contains(failed, "partial redraw differs") {
		t.Fatalf("verify said %q", failed)
	}
	if d.fulls != 1 || c.At(110, 210) != gfx.RGB(0, 0, 200) {
		t.Fatalf("after a failed check: %d full frames, box %08x", d.fulls, c.At(110, 210))
	}
}

func TestMergeRects(t *testing.T) {
	got := mergeRects([]gfx.Rect{gfx.R(0, 0, 10, 10), gfx.R(10, 0, 10, 10), gfx.R(50, 50, 5, 5)}, 4)
	if len(got) != 2 || got[0] != gfx.R(0, 0, 20, 10) || got[1] != gfx.R(50, 50, 5, 5) {
		t.Fatalf("merged %v", got)
	}
	got = mergeRects([]gfx.Rect{gfx.R(0, 0, 1, 1), gfx.R(10, 0, 1, 1), gfx.R(20, 0, 1, 1)}, 2)
	if len(got) != 1 || got[0] != gfx.R(0, 0, 21, 1) {
		t.Fatalf("over the limit: %v", got)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index e3d3f5d..e9b7364 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -334,6 +334,8 @@ func newTestApp(t *testing.T, prof Profile) *testApp {
 		t.Fatal(err)
 	}
 	ta.App = a
+	a.verify = true // every partial frame must equal a full one
+	a.verifyFail = func(m string) { t.Errorf("%s", m) }
 	return ta
 }
 
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
ta.Damage undefined (type *testApp has no field or method Damage)
ta.verifyFail undefined (type *testApp has no field or method verifyFail)
undefined: mergeRects
a.verify undefined (type *App has no field or method verify)
a.verifyFail undefined (type *App has no field or method verifyFail)
```

- [ ] **Step 3: Implement**

`internal/ui/damage.go` (new file):

```go
package ui

import (
	"fmt"
	"log"

	"mistersubsonic/internal/gfx"
)

// Partial redraws (Plan 4b). A change that knows exactly which part of the
// screen it affects calls Damage instead of marking the whole frame dirty.
// render then runs the normal drawing code clipped to the damaged areas and
// copies only those areas to the framebuffer. The drawing logic always runs
// in full, so a partial frame differs from a full one only outside the
// damage.

// maxDamageRects is how many separate areas a partial frame redraws; more
// than that and they are merged into their bounding box.
const maxDamageRects = 4

// Damage marks r as changed; the next frame redraws it.
func (a *App) Damage(r gfx.Rect) {
	r = r.Intersect(a.canvas.Bounds())
	if !r.Empty() {
		a.damage = append(a.damage, r)
	}
}

// mergeRects joins overlapping or touching rectangles, and folds the lot
// into its bounding box when more than max are left.
func mergeRects(rs []gfx.Rect, max int) []gfx.Rect {
	out := append([]gfx.Rect(nil), rs...)
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out) && !merged; i++ {
			for j := i + 1; j < len(out); j++ {
				if touches(out[i], out[j]) {
					out[i] = union(out[i], out[j])
					out = append(out[:j], out[j+1:]...)
					merged = true
					break
				}
			}
		}
	}
	if len(out) > max {
		box := out[0]
		for _, r := range out[1:] {
			box = union(box, r)
		}
		out = []gfx.Rect{box}
	}
	return out
}

func touches(a, b gfx.Rect) bool {
	return a.X <= b.Right() && b.X <= a.Right() && a.Y <= b.Bottom() && b.Y <= a.Bottom()
}

func union(a, b gfx.Rect) gfx.Rect {
	x, y := min(a.X, b.X), min(a.Y, b.Y)
	return gfx.R(x, y, max(a.Right(), b.Right())-x, max(a.Bottom(), b.Bottom())-y)
}

func area(rs []gfx.Rect) int {
	n := 0
	for _, r := range rs {
		n += r.W * r.H
	}
	return n
}

// renderDamage draws and presents the damaged areas only. It reports false
// when a full frame is better (the areas cover more than half the canvas).
func (a *App) renderDamage() (bool, error) {
	rs := mergeRects(a.damage, maxDamageRects)
	a.damage = a.damage[:0]
	c := a.canvas
	if 2*area(rs) > c.W*c.H {
		return false, nil
	}
	for _, r := range rs {
		c.SetClip(r)
		a.drawFrame(c)
	}
	c.ClearClip()
	if a.verify {
		if err := a.checkPartial(); err != nil {
			if a.verifyFail != nil {
				a.verifyFail(err.Error())
			} else {
				log.Printf("ui: %v; drawing the full frame", err)
			}
			return false, nil
		}
	}
	return true, a.presentRects(c, rs)
}

// checkPartial draws a full frame on the side and compares it with the
// canvas: a partial frame must match it pixel for pixel.
func (a *App) checkPartial() error {
	c := a.canvas
	if a.verifyCanvas == nil || a.verifyCanvas.W != c.W || a.verifyCanvas.H != c.H {
		a.verifyCanvas = gfx.NewCanvas(c.W, c.H)
	}
	a.drawFrame(a.verifyCanvas)
	for i, p := range c.Pix {
		if p != a.verifyCanvas.Pix[i] {
			return fmt.Errorf("partial redraw differs from a full one at (%d,%d): %06x, want %06x",
				i%c.W, i/c.W, p, a.verifyCanvas.Pix[i])
		}
	}
	return nil
}

// presentRects shows the rectangles rs of c: only those areas when the
// display can take them, otherwise the whole frame (scaled on CRT).
func (a *App) presentRects(c *gfx.Canvas, rs []gfx.Rect) error {
	if pp, ok := a.o.Display.(gfx.PartialPresenter); ok && a.scaler == nil {
		return pp.PresentRects(c, rs)
	}
	return a.present(c)
}
```

Then Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 98a839c..a063dc3 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -37,7 +37,7 @@ var version = "dev"
 
 type flags struct {
 	config, display, viewerAddr, frames, profile, fbdev, keys, log, screenshots string
-	null, restoreConsole                                                        bool
+	null, restoreConsole, verifyRedraw                                          bool
 	volume                                                                      float64
 	exitAfter                                                                   time.Duration
 }
@@ -54,6 +54,7 @@ func main() {
 	flag.StringVar(&f.keys, "keys", "", `scripted button presses for testing, e.g. "a:2s,a,a" (see keys.go)`)
 	flag.StringVar(&f.log, "log", "auto", "log file: auto (log.txt next to the config on the framebuffer, stderr elsewhere), - (stderr) or a path")
 	flag.StringVar(&f.screenshots, "screenshots", "auto", "screenshot folder: auto (/media/fat/screenshots/MiSTer_Subsonic beside the config on the framebuffer, screenshots/ next to the config elsewhere), a path, or \"\" for none")
+	flag.BoolVar(&f.verifyRedraw, "verify-redraw", false, "check every partial redraw against a full one and log any difference (debugging)")
 	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
 	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
 	flag.BoolVar(&f.restoreConsole, "restore-console", false, "put the console back in text mode and exit (the launcher runs this after the app)")
@@ -303,6 +304,7 @@ func run(f flags) (err error) {
 		FallbackFonts: filepath.Join(dataDir, "fonts"),
 		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
 		ScreenshotDir: screenshotDir(f.screenshots, f.display, dataDir),
+		VerifyRedraw:  f.verifyRedraw,
 		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
 	})
 	if err != nil {
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 11e7751..c1cfeab 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -102,6 +102,9 @@ type Options struct {
 	Version    string // shown in Settings → About
 	// ScreenshotDir is where the screenshot button saves PNGs ("": off).
 	ScreenshotDir string
+	// VerifyRedraw checks every partial frame against a full one and logs
+	// any difference (a debugging aid on the device).
+	VerifyRedraw bool
 	// Connect starts a connection to cfg's active server, off the UI
 	// goroutine, replacing any previous one; it answers with
 	// a.Connected or a.ConnectFailed (through a.Post).
@@ -198,8 +201,13 @@ type App struct {
 	muted       bool      // the sound is off (not saved: the app starts with sound)
 	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
 	shooting    bool      // a screenshot is being saved
-	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
-	overwritten bool      // the last check found the screen drawn over
+
+	damage       []gfx.Rect   // changed areas for the next frame (dirty means the whole frame)
+	verify       bool         // check partial frames against full ones
+	verifyFail   func(string) // called when that check fails (tests)
+	verifyCanvas *gfx.Canvas
+	checkAt      time.Time // the next watchdog check (zero: the display can't check itself)
+	overwritten  bool      // the last check found the screen drawn over
 }
 
 func New(o Options) (*App, error) {
@@ -230,6 +238,7 @@ func New(o Options) (*App, error) {
 	if pw != a.P.W || ph != a.P.H { // drawn at the display's size: nothing to scale
 		a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
 	}
+	a.verify = o.VerifyRedraw
 	if _, ok := o.Display.(gfx.Checker); ok {
 		a.checkAt = o.Now().Add(watchdogEvery)
 	}
@@ -406,7 +415,7 @@ func (a *App) Run(ctx context.Context) error {
 	timer := time.NewTimer(time.Hour)
 	defer timer.Stop()
 	for !a.quit {
-		if a.dirty {
+		if a.dirty || len(a.damage) > 0 {
 			if err := a.render(); err != nil {
 				return err
 			}
@@ -660,17 +669,32 @@ func (a *App) present(c *gfx.Canvas) error {
 	return a.o.Display.Present(a.scaler.Scale(c))
 }
 
+// render draws the next frame: only the damaged areas when nothing else
+// changed, otherwise the whole frame.
 func (a *App) render() error {
+	full := a.dirty
 	a.dirty = false
 	if _, np := a.Top().(*NowPlayingScreen); !np {
 		a.saver = false
 	}
 	if a.saver {
+		a.damage = a.damage[:0]
 		a.drawSaver(a.canvas)
 		return a.present(a.canvas)
 	}
+	if !full && len(a.damage) > 0 {
+		if done, err := a.renderDamage(); done || err != nil {
+			return err
+		}
+	}
+	a.damage = a.damage[:0]
+	a.drawFrame(a.canvas)
+	return a.present(a.canvas)
+}
+
+// drawFrame draws everything on c (within its clip).
+func (a *App) drawFrame(c *gfx.Canvas) {
 	a.animate, a.mq.seen, a.mqWake, a.dim = false, false, time.Time{}, false
-	c := a.canvas
 	c.Clear(colBg)
 	top := a.Top()
 	p := a.P
@@ -697,7 +721,6 @@ func (a *App) render() error {
 	if a.confirm {
 		a.drawConfirm(c)
 	}
-	return a.present(c)
 }
 
 func (a *App) drawHeader(c *gfx.Canvas, title string) {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go internal/ui/app.go internal/ui/damage.go internal/ui/damage_test.go internal/ui/fakes_test.go
git commit -m "ui: partial frames drawn and presented by damage, checked against full ones in tests" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: Damage sources: focus moves, the tick, the marquee, covers, toasts, the panel

**Files:**
- Modify: `internal/ui/list.go`, `internal/ui/grid.go`, `internal/ui/views.go`, `internal/ui/damage.go`, `internal/ui/app.go`, `internal/ui/volume.go`, `internal/ui/mediakeys.go`, `internal/ui/menu.go`, `internal/ui/screens_album.go`, `internal/ui/screens_home.go`, `internal/ui/screens_play.go`, `internal/ui/screens_playlists.go`, `internal/ui/screens_settings.go`
- Test: `internal/ui/damage_sources_test.go` (new); `internal/ui/damage_test.go`, `internal/ui/fakes_test.go`, `internal/ui/fixwave_test.go`, `internal/ui/volume_test.go`, `internal/ui/bench_test.go`, `internal/gfx/pack_bench_test.go` (modified)

**Interfaces:**
- **Consumes:** `Damage`, the partial render and the verify mode (Task 4); the clip (Task 3).
- **Produces:**
  - **Widget moves.**
    - `(*List).Moved()` and `(*Grid).Moved()` return the old and the new row or cell, or the whole area when the move scrolls. They return nil before the widget is drawn.
    - The widgets remember `area`, `rowH`/`cellW`/`cellH`/`x0` from their last `Draw`, and `prev` from their last `Handle`.
  - **Exact keys.**
    - `(*App).moved(rs)` damages `rs` and sets `App.exact`.
    - `dispatch` marks the whole frame dirty unless the key was exact. It also damages the header when the top screen's title changed, and redraws everything when the top screen changed.
    - Screens opt in: the views (`cursor.handle`, `songsView`), the album, the playlists, the queue, the home list, the genres, the Settings lists and the X menu.
  - **Regions recorded while drawing, rebuilt each frame (`resetMarks`):**
    - `markTick(r)`: Now Playing's bar and times, and the mini bar's line;
    - `markMarquee(r)`: the scrolling title;
    - `markArt(id, r)`: from `drawArt`.
  - **Wake-ups.** `onWake` damages the tick regions while playing, the marquee line, the toast area when a toast expires, and the volume panel when it goes.
  - **Covers.** `ArtReady` damages that cover's cells (`coverArrived`), or the whole frame when the cover wasn't drawn.
  - **Toasts.** `Toast` damages the toast area (`toastsArea`, through `eachToast`).
  - **Volume.**
    - `showVolume` damages `volumePanelRect()`.
    - Media volume keys and mute are exact except on a Settings list (`onlyPanelShowsVolume`). Now Playing's Up and Down are exact too.
    - `setMuted` and `mediaKey` no longer set `dirty` themselves.
  - **Benchmarks.** `BenchmarkPartial` (ui) runs at 1920×1200, and `BenchmarkPackRects` (gfx) is added along with a 1920×1200 `Pack` case.

- [ ] **Step 1: Write the failing tests**

`internal/ui/damage_sources_test.go` (new file):

```go
package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// partialApp is a test app on a display that takes partial frames.
func partialApp(t *testing.T, p Profile, s Screen) (*testApp, *rectDisplay) {
	t.Helper()
	ta := newTestApp(t, p)
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	ta.Push(NewHomeScreen())
	ta.Push(s)
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	return ta, d
}

// presented is the one partial frame d got since the last reset.
func presented(t *testing.T, d *rectDisplay) []gfx.Rect {
	t.Helper()
	if d.fulls != 0 || len(d.rects) != 1 {
		t.Fatalf("got %d full frames and partial frames %v, want one partial frame", d.fulls, d.rects)
	}
	rs := d.rects[0]
	d.rects, d.fulls = nil, 0
	return rs
}

// A focus move that doesn't scroll redraws the two rows it changed; one
// that scrolls redraws the list, not the header or the mini bar.
func TestListMovesRedrawTheirRows(t *testing.T) {
	s := NewAlbumScreen(sampleLibrary().albums[0])
	ta, d := partialApp(t, ProfileCRT240, s)
	ta.press(input.BtnDown)
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].H != 2*s.list.rowH || rs[0].Y != s.list.area.Y {
		t.Fatalf("a move down redrew %v, want rows 0 and 1 (height %d at y %d)", rs, 2*s.list.rowH, s.list.area.Y)
	}
}

// Moved names the old and new row, or the whole list when the move scrolls.
func TestListAndGridMoved(t *testing.T) {
	c := gfx.NewCanvas(400, 300)
	var l List
	if l.Moved() != nil {
		t.Fatal("an undrawn list reported damage")
	}
	area := gfx.R(10, 20, 300, 100) // 5 rows of 20
	l.Draw(c, area, 50, 20, func(int, gfx.Rect, bool) {})
	l.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 50)
	if got := l.Moved(); len(got) != 2 || got[0] != gfx.R(10, 20, 300, 20) || got[1] != gfx.R(10, 40, 300, 20) {
		t.Fatalf("down one = %v", got)
	}
	l.Handle(input.Event{Button: input.BtnR, Kind: input.Press}, 50) // a page: scrolls
	if got := l.Moved(); len(got) != 1 || got[0] != area {
		t.Fatalf("a page down = %v, want the list %v", got, area)
	}

	var g Grid
	garea := gfx.R(0, 0, 300, 200) // 3 columns of 100, 2 rows of 100
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnRight, Kind: input.Press}, 20)
	if got := g.Moved(); len(got) != 2 || got[0] != gfx.R(0, 0, 100, 100) || got[1] != gfx.R(100, 0, 100, 100) {
		t.Fatalf("right one = %v", got)
	}
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 20)
	g.Draw(c, garea, 20, 100, 100, func(int, gfx.Rect, bool) {})
	g.Handle(input.Event{Button: input.BtnDown, Kind: input.Press}, 20) // into row 3: scrolls
	if got := g.Moved(); len(got) != 1 || got[0] != garea {
		t.Fatalf("down past the last visible row = %v, want the grid", got)
	}
}

// The progress tick while playing redraws the bar and the times only.
func TestProgressTickRedrawsTheBarAndTimes(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewNowPlayingScreen())
	playingState(ta)
	ta.dirty = true
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	ta.pl.st.Position += time.Second
	ta.now = ta.now.Add(progressTick)
	ta.onWake()
	ta.settle(t)
	rs := presented(t, d)
	if area(rs) >= ta.P.W*ta.P.H/10 {
		t.Fatalf("the tick redrew %v (%d px), want just the bar and times", rs, area(rs))
	}
}

// A scrolling title redraws its own line each frame, nothing else.
func TestMarqueeRedrawsItsLine(t *testing.T) {
	p := &probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, longTitle, colText, area, true)
	}}
	ta, d := partialApp(t, ProfileHDMI, p)
	ta.now = ta.now.Add(marqueeDelay + time.Second)
	ta.onWake()
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].W != 100 || rs[0].H != ta.F.Body.Height() {
		t.Fatalf("the marquee redrew %v, want its 100 px line", rs)
	}
}

// slowArt has a cover only once it is marked ready.
type slowArt struct{ ready map[subsonic.ID]bool }

func (s slowArt) Get(k art.Key) (*gfx.Image, bool) {
	if !s.ready[k.ID] {
		return nil, false
	}
	return fakeArt{}.Get(k)
}

// A cover that arrives redraws the cells that show it.
func TestArrivingCoverRedrawsItsCells(t *testing.T) {
	sa := slowArt{ready: map[subsonic.ID]bool{}}
	ta := newTestApp(t, ProfileHDMI)
	ta.o.Art = sa
	d := &rectDisplay{Headless: ta.disp}
	ta.o.Display = d
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", "newest"))
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	id := ta.lib.albums[1].CoverArt
	sa.ready[id] = true
	ta.ArtReady(art.Key{ID: id, Size: ta.P.Cover})
	ta.settle(t)
	rs := presented(t, d)
	if len(rs) != 1 || rs[0].W > ta.P.Cover+ta.P.Margin {
		t.Fatalf("the cover redrew %v, want its cell", rs)
	}
}

// A media volume key redraws the volume panel only, except on a settings
// list, which shows the level in a row as well.
func TestVolumeKeyRedrawsThePanel(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	ta.press(input.BtnVolUp)
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != ta.volumePanelRect() {
		t.Fatalf("redrew %v, want the panel %v", rs, ta.volumePanelRect())
	}
	ta.now = ta.now.Add(volumeShowTime)
	ta.onWake()
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != ta.volumePanelRect() {
		t.Fatalf("the panel going redrew %v", rs)
	}
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.settle(t)
	d.rects, d.fulls = nil, 0
	ta.press(input.BtnVolDown)
	ta.settle(t)
	if d.fulls != 1 {
		t.Fatalf("on Settings → Playback: %d full frames, partial %v", d.fulls, d.rects)
	}
}

// Toasts redraw their own area when they appear and when they go.
func TestToastsRedrawTheirArea(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	ta.Toast("Added to queue: %s", "Радио Африка")
	ta.settle(t)
	shown := ta.toastsArea()
	if rs := presented(t, d); len(rs) != 1 || rs[0] != shown {
		t.Fatalf("a toast redrew %v, want %v", rs, shown)
	}
	ta.now = ta.now.Add(toastTime)
	ta.onWake()
	ta.settle(t)
	if rs := presented(t, d); len(rs) != 1 || rs[0] != shown {
		t.Fatalf("the toast going redrew %v, want %v", rs, shown)
	}
}

func area(rs []gfx.Rect) int {
	n := 0
	for _, r := range rs {
		n += r.W * r.H
	}
	return n
}
```

Then update the existing tests. Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/gfx/pack_bench_test.go b/internal/gfx/pack_bench_test.go
index 081c966..b2f049e 100644
--- a/internal/gfx/pack_bench_test.go
+++ b/internal/gfx/pack_bench_test.go
@@ -7,7 +7,7 @@ func BenchmarkPack(b *testing.B) {
 	for _, c := range []struct {
 		name      string
 		w, h, bpp int
-	}{{"960x600x32", 960, 600, 32}, {"1280x720x32", 1280, 720, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1080x16", 1920, 1080, 16}} {
+	}{{"960x600x32", 960, 600, 32}, {"1280x720x32", 1280, 720, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1200x32", 1920, 1200, 32}, {"1920x1080x16", 1920, 1080, 16}} {
 		b.Run(c.name, func(b *testing.B) {
 			f := fbFormat{width: c.w, height: c.h, stride: c.w * c.bpp / 8, bpp: c.bpp}
 			mem := make([]byte, f.stride*f.height)
@@ -23,3 +23,29 @@ func BenchmarkPack(b *testing.B) {
 		})
 	}
 }
+
+// BenchmarkPackRects measures presenting parts of a 1920x1200 frame (Plan
+// 4b's partial redraws): two list rows, a cover cell, a list area.
+func BenchmarkPackRects(b *testing.B) {
+	f := fbFormat{width: 1920, height: 1200, stride: 1920 * 4, bpp: 32}
+	mem := make([]byte, f.stride*f.height)
+	cv := NewCanvas(1920, 1200)
+	for _, c := range []struct {
+		name string
+		rs   []Rect
+	}{
+		{"rows", []Rect{R(280, 300, 1640, 126)}},
+		{"cell", []Rect{R(400, 200, 380, 470)}},
+		{"list", []Rect{R(280, 96, 1640, 1000)}},
+		{"full", []Rect{R(0, 0, 1920, 1200)}},
+	} {
+		b.Run(c.name, func(b *testing.B) {
+			for i := 0; i < b.N; i++ {
+				for _, r := range c.rs {
+					f.packRect(mem, cv, r)
+				}
+			}
+			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
+		})
+	}
+}
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 4487299..81616ed 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -5,6 +5,8 @@ import (
 	"testing"
 
 	"mistersubsonic/internal/gfx"
+	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/subsonic"
 )
 
 // BenchmarkRepaint measures a full frame: draw the screen and scale it to the
@@ -50,6 +52,7 @@ func BenchmarkRepaint(b *testing.B) {
 		b.Run(c.name, func(b *testing.B) {
 			t := &testing.T{}
 			ta := newTestApp(t, c.prof)
+			ta.verify = false
 			ta.o.Display = nullDisplay{c.fbW, c.fbH} // the framebuffer's pack is measured in gfx
 			ta.scaler = nil
 			if c.prof.W != c.fbW || c.prof.H != c.fbH {
@@ -106,3 +109,85 @@ func BenchmarkScreenshot(b *testing.B) {
 		})
 	}
 }
+
+// BenchmarkPartial measures frames at the full resolution of a 1920x1200
+// display (Plan 4b): a full frame, then the partial frames that make
+// browsing smooth. Packing the rectangles into the framebuffer is
+// BenchmarkPackRects in gfx.
+//
+//	./ui.test -test.run '^$' -test.bench Partial -test.benchtime 50x
+func BenchmarkPartial(b *testing.B) {
+	down := input.Event{Button: input.BtnDown, Kind: input.Press}
+	up := input.Event{Button: input.BtnUp, Kind: input.Press}
+	right := input.Event{Button: input.BtnRight, Kind: input.Press}
+	left := input.Event{Button: input.BtnLeft, Kind: input.Press}
+	albums := func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }
+	cases := []struct {
+		name   string
+		screen func(ta *testApp) Screen
+		step   func(ta *testApp, i int)
+	}{
+		{"full-albums", albums, func(ta *testApp, i int) { ta.dirty = true }},
+		{"move-grid", albums, func(ta *testApp, i int) {
+			ta.dispatch([]input.Event{right, left}[i%2])
+		}},
+		{"move-list", func(ta *testApp) Screen { return NewGenresScreen() }, func(ta *testApp, i int) {
+			ta.dispatch([]input.Event{down, up}[i%2])
+		}},
+		{"scroll-list", func(ta *testApp) Screen { return NewGenresScreen() }, func(ta *testApp, i int) {
+			ta.dispatch(down) // at the bottom of the view every step scrolls
+		}},
+		{"tick-nowplaying", func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, func(ta *testApp, i int) {
+			ta.pl.st.Position += progressTick
+			ta.now = ta.now.Add(progressTick)
+			ta.onWake()
+		}},
+		{"marquee", func(ta *testApp) Screen {
+			ta.lib.albums[0].Name = "A Rather Long Album Title That Will Need Truncating Somewhere"
+			return albums(ta)
+		}, func(ta *testApp, i int) {
+			ta.now = ta.now.Add(marqueeFrame)
+			ta.onWake()
+		}},
+	}
+	p := PickProfile(1920, 1200, "auto")
+	for _, c := range cases {
+		b.Run(c.name, func(b *testing.B) {
+			t := &testing.T{}
+			ta := newTestApp(t, p)
+			ta.verify = false
+			ta.o.Display = nullDisplay{p.W, p.H}
+			for i := 0; i < 40; i++ {
+				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
+			}
+			for i := range 2000 { // a long list to scroll
+				ta.lib.genres = append(ta.lib.genres, subsonic.Genre{Name: fmt.Sprintf("Genre %d", i), AlbumCount: i})
+			}
+			ta.Push(NewHomeScreen())
+			ta.Push(c.screen(ta))
+			ta.settle(t)
+			if c.name == "scroll-list" {
+				for range 60 { // to the bottom of the view
+					ta.dispatch(down)
+				}
+			}
+			if c.name == "marquee" {
+				ta.now = ta.now.Add(2 * marqueeDelay)
+				ta.settle(t)
+			}
+			ta.settle(t)
+			b.ResetTimer()
+			for i := 0; i < b.N; i++ {
+				ta.dirty, ta.damage = false, ta.damage[:0]
+				c.step(ta, i)
+				if !ta.redrawDue() {
+					b.Fatal("the step changed nothing")
+				}
+				if err := ta.render(); err != nil {
+					b.Fatal(err)
+				}
+			}
+			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
+		})
+	}
+}
diff --git a/internal/ui/damage_test.go b/internal/ui/damage_test.go
index 4deaff7..a47f8e6 100644
--- a/internal/ui/damage_test.go
+++ b/internal/ui/damage_test.go
@@ -4,6 +4,7 @@ import (
 	"strings"
 	"testing"
 
+	"mistersubsonic/internal/config"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
 )
@@ -61,16 +62,6 @@ func TestDamageRedrawsAndPresentsOnlyTheChangedArea(t *testing.T) {
 	}
 }
 
-// Damage over more than half the screen draws the full frame instead.
-func TestLargeDamageDrawsTheFullFrame(t *testing.T) {
-	ta, d, _ := newRectApp(t)
-	ta.Damage(gfx.R(0, 0, ta.P.W, ta.P.H*3/4))
-	ta.settle(t)
-	if d.fulls != 1 || len(d.rects) != 0 {
-		t.Fatalf("presented %v rects and %d full frames, want one full frame", d.rects, d.fulls)
-	}
-}
-
 // The verify mode catches a change whose damage misses it: the frame is
 // then drawn in full, so the screen never keeps stale pixels.
 func TestVerifyCatchesAMissedDamage(t *testing.T) {
@@ -98,3 +89,60 @@ func TestMergeRects(t *testing.T) {
 		t.Fatalf("over the limit: %v", got)
 	}
 }
+
+// Every screen, on every layout, stays exactly as a full redraw would draw
+// it while the focus moves around (the verify mode compares each partial
+// frame with a full one and fails the test on any difference).
+func TestPartialRedrawsMatchFullFramesEverywhere(t *testing.T) {
+	screens := map[string]func(ta *testApp) Screen{
+		"root":        func(ta *testApp) Screen { return NewRootScreen(ta.P) },
+		"albums":      func(ta *testApp) Screen { return NewAlbumsScreen() },
+		"album list":  func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") },
+		"artists":     func(ta *testApp) Screen { return NewArtistsScreen() },
+		"artist":      func(ta *testApp) Screen { return NewArtistScreen(ta.lib.artists[2].Artists[0]) },
+		"album":       func(ta *testApp) Screen { return NewAlbumScreen(ta.lib.albums[0]) },
+		"genres":      func(ta *testApp) Screen { return NewGenresScreen() },
+		"playlists":   func(ta *testApp) Screen { return NewPlaylistsScreen() },
+		"playlist":    func(ta *testApp) Screen { return NewPlaylistScreen(ta.lib.playlists[0]) },
+		"starred":     func(ta *testApp) Screen { return NewStarredScreen() },
+		"search":      func(ta *testApp) Screen { return NewSearchScreen() },
+		"queue":       func(ta *testApp) Screen { playingState(ta); return NewQueueScreen() },
+		"now playing": func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() },
+		"settings":    func(ta *testApp) Screen { return NewSettingsScreen() },
+		"playback":    func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
+		"display":     func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
+		"servers":     func(ta *testApp) Screen { return NewServersScreen() },
+	}
+	keys := []input.Button{input.BtnDown, input.BtnDown, input.BtnRight, input.BtnRight, input.BtnDown,
+		input.BtnR, input.BtnUp, input.BtnLeft, input.BtnL, input.BtnUp, input.BtnDown}
+	partial := map[string]int{}
+	for _, p := range append(profiles, PickProfile(960, 600, "auto")) {
+		for name, mk := range screens {
+			ta := newTestApp(t, p)
+			d := &rectDisplay{Headless: ta.disp}
+			ta.o.Display = d
+			ta.cfg = config.Default()
+			ta.Push(NewHomeScreen())
+			ta.Push(mk(ta))
+			ta.settle(t)
+			for _, b := range keys {
+				ta.onInput(input.Event{Button: b, Kind: input.Press})
+				ta.settle(t)
+				ta.onInput(input.Event{Button: b, Kind: input.Repeat})
+				ta.settle(t)
+				ta.onInput(input.Event{Button: b, Kind: input.Release})
+			}
+			if t.Failed() {
+				t.Fatalf("%s on %s", name, p.Name)
+			}
+			partial[name] += len(d.rects)
+		}
+	}
+	// The walk must exercise partial frames where screens opted in.
+	for _, name := range []string{"albums", "artists", "album", "genres", "playlists", "queue", "settings", "playback"} {
+		if partial[name] == 0 {
+			t.Errorf("%s: no partial frame was drawn", name)
+		}
+	}
+	t.Logf("partial frames: %v", partial)
+}
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index e9b7364..3264ed3 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -357,6 +357,11 @@ func (ta *testApp) settle(t *testing.T) *gfx.Canvas {
 	return ta.disp.Last()
 }
 
+// redrawDue reports whether the next frame redraws anything (all of it, or
+// damaged parts); clean forgets both.
+func (ta *testApp) redrawDue() bool { return ta.dirty || len(ta.damage) > 0 }
+func (ta *testApp) clean()          { ta.dirty, ta.damage = false, nil }
+
 func (ta *testApp) press(b input.Button) {
 	ta.onInput(input.Event{Button: b, Kind: input.Press})
 	ta.onInput(input.Event{Button: b, Kind: input.Release})
diff --git a/internal/ui/fixwave_test.go b/internal/ui/fixwave_test.go
index 55a2164..009595a 100644
--- a/internal/ui/fixwave_test.go
+++ b/internal/ui/fixwave_test.go
@@ -64,9 +64,9 @@ func TestMarqueeWaitsQuietlyForItsDelay(t *testing.T) {
 		t.Fatalf("half way: next wake in %v, want %v", d, marqueeDelay/2)
 	}
 	ta.now = ta.now.Add(marqueeDelay / 2)
-	ta.dirty = false
+	ta.clean()
 	ta.onWake()
-	if !ta.dirty {
+	if !ta.redrawDue() {
 		t.Fatal("the wake at the end of the delay did not redraw")
 	}
 	ta.settle(t)
diff --git a/internal/ui/volume_test.go b/internal/ui/volume_test.go
index b9faf1e..6f8774e 100644
--- a/internal/ui/volume_test.go
+++ b/internal/ui/volume_test.go
@@ -45,9 +45,9 @@ func TestVolumePanelHides(t *testing.T) {
 		t.Fatal("the panel went while the key repeated")
 	}
 	ta.now = ta.now.Add(100 * time.Millisecond)
-	ta.dirty = false
+	ta.clean()
 	ta.onWake()
-	if !ta.volumeUntil.IsZero() || !ta.dirty {
+	if !ta.volumeUntil.IsZero() || !ta.redrawDue() {
 		t.Fatal("the panel didn't go, or the screen wasn't redrawn without it")
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/gfx`

Expected: FAIL, e.g.:

```
s.list.rowH undefined (type List has no field or method rowH)
s.list.area undefined (type List has no field or method area)
l.Moved undefined (type List has no field or method Moved)
g.Moved undefined (type Grid has no field or method Moved)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index c1cfeab..2fa5a4c 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -202,9 +202,13 @@ type App struct {
 	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
 	shooting    bool      // a screenshot is being saved
 
-	damage       []gfx.Rect   // changed areas for the next frame (dirty means the whole frame)
-	verify       bool         // check partial frames against full ones
-	verifyFail   func(string) // called when that check fails (tests)
+	damage       []gfx.Rect                 // changed areas for the next frame (dirty means the whole frame)
+	exact        bool                       // the key being handled damaged exactly what it changed
+	ticks        []gfx.Rect                 // drawn from the playback position
+	mqRect       gfx.Rect                   // the scrolling title
+	arts         map[subsonic.ID][]gfx.Rect // where each cover was drawn
+	verify       bool                       // check partial frames against full ones
+	verifyFail   func(string)               // called when that check fails (tests)
 	verifyCanvas *gfx.Canvas
 	checkAt      time.Time // the next watchdog check (zero: the display can't check itself)
 	overwritten  bool      // the last check found the screen drawn over
@@ -276,8 +280,8 @@ func (a *App) Post(f func()) {
 // Redraw marks the frame dirty.
 func (a *App) Redraw() { a.dirty = true }
 
-// ArtReady is the art loader's Ready callback: redraw when a cover arrives.
-func (a *App) ArtReady(art.Key) { a.Post(a.Redraw) }
+// ArtReady is the art loader's Ready callback: redraw where the cover goes.
+func (a *App) ArtReady(k art.Key) { a.Post(func() { a.coverArrived(k.ID) }) }
 
 func (a *App) Library() Library { return a.o.Library }
 func (a *App) Player() Player   { return a.o.Player }
@@ -396,6 +400,7 @@ func (a *App) After(owner Screen, d time.Duration, f func()) {
 func (a *App) Toast(format string, args ...any) {
 	text := fmt.Sprintf(format, args...)
 	until := a.o.Now().Add(toastTime)
+	a.Damage(a.toastsArea()) // the toasts move up for the new one
 	if n := len(a.toasts); n > 0 && a.toasts[n-1].text == text {
 		a.toasts[n-1].until = until
 	} else {
@@ -404,7 +409,7 @@ func (a *App) Toast(format string, args ...any) {
 			a.toasts = a.toasts[len(a.toasts)-maxToasts:]
 		}
 	}
-	a.dirty = true
+	a.Damage(a.toastsArea())
 }
 
 // Run drives the UI until ctx ends or the user exits. A pending
@@ -486,14 +491,16 @@ func (a *App) onWake() {
 	for _, e := range a.rep.Due(now) {
 		a.dispatch(e)
 	}
+	shown := a.toastsArea()
 	kept := a.toasts[:0]
 	for _, t := range a.toasts {
 		if now.Before(t.until) {
 			kept = append(kept, t)
-		} else {
-			a.dirty = true
 		}
 	}
+	if len(kept) < len(a.toasts) {
+		a.Damage(shown) // the rest stay where they were
+	}
 	a.toasts = kept
 	var due []timer
 	pending := a.timers[:0]
@@ -523,10 +530,15 @@ func (a *App) onWake() {
 	}
 	a.checkScreen(now)
 	if !a.volumeUntil.IsZero() && !now.Before(a.volumeUntil) {
-		a.volumeUntil, a.dirty = time.Time{}, true // the panel goes
+		a.Damage(a.volumePanelRect()) // the panel goes
+		a.volumeUntil = time.Time{}
 	}
 	if a.animate || (!a.mqWake.IsZero() && !now.Before(a.mqWake)) {
-		a.dirty = true
+		if a.mqRect.Empty() {
+			a.dirty = true
+		} else {
+			a.Damage(a.mqRect)
+		}
 	}
 	if !a.bDown.IsZero() && len(a.stack) == 1 && !now.Before(a.bDown.Add(exitHold)) {
 		a.bDown = time.Time{}
@@ -534,7 +546,7 @@ func (a *App) onWake() {
 		a.dirty = true
 	}
 	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
-		a.dirty = true // progress
+		a.damageAll(a.ticks) // progress
 	}
 }
 
@@ -596,7 +608,19 @@ func (a *App) onInput(e input.Event) {
 }
 
 func (a *App) dispatch(e input.Event) {
-	a.dirty = true
+	a.exact = false
+	top, title := a.Top(), ""
+	if top != nil {
+		title = top.Title()
+	}
+	defer func() {
+		switch {
+		case !a.exact || a.Top() != top:
+			a.dirty = true // what the key changed is unknown: redraw it all
+		case top != nil && top.Title() != title:
+			a.Damage(a.headerRect()) // e.g. Artists · B follows the focus
+		}
+	}()
 	if a.confirm {
 		switch e.Button {
 		case input.BtnA:
@@ -695,6 +719,7 @@ func (a *App) render() error {
 // drawFrame draws everything on c (within its clip).
 func (a *App) drawFrame(c *gfx.Canvas) {
 	a.animate, a.mq.seen, a.mqWake, a.dim = false, false, time.Time{}, false
+	a.resetMarks()
 	c.Clear(colBg)
 	top := a.Top()
 	p := a.P
@@ -723,9 +748,12 @@ func (a *App) drawFrame(c *gfx.Canvas) {
 	}
 }
 
+// headerRect is the title bar across the top.
+func (a *App) headerRect() gfx.Rect { return gfx.R(0, 0, a.P.W, a.P.SafeY+a.P.HeaderH) }
+
 func (a *App) drawHeader(c *gfx.Canvas, title string) {
 	p := a.P
-	c.Fill(gfx.R(0, 0, p.W, p.SafeY+p.HeaderH), colPanel)
+	c.Fill(a.headerRect(), colPanel)
 	f := a.F.Title
 	y := p.SafeY + (p.HeaderH+f.Ascent()-f.Descent())/2
 	w := p.W - 2*p.Margin
@@ -769,6 +797,7 @@ func (a *App) drawFit(c *gfx.Canvas, f *gfx.Font, x, y, w int, s string, col gfx
 	}
 	vis, dx := f.Marquee(s, off)
 	area := gfx.R(x, y-f.Ascent(), w, f.Height()).Intersect(clip)
+	a.markMarquee(area)
 	f.Draw(c, x+dx, y, vis, col, area)
 }
 
@@ -792,6 +821,7 @@ func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
 	iconText(c, f, statusIcon(st.Status), x, base, f.Truncate(line, w-f.Ascent()), colText, c.Bounds())
 	if d := time.Duration(song.Duration) * time.Second; d > 0 {
 		bw := progressW(w, st.Position, d)
+		a.markTick(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, w, 2))
 		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, w, 2), colArtBg)
 		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, bw, 2), colAccent)
 	}
@@ -800,14 +830,22 @@ func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
 func (a *App) drawToasts(c *gfx.Canvas) {
 	f := a.F.Body
 	p := a.P
-	y := p.H - p.SafeY - p.MiniBarH - p.Margin
-	for i := len(a.toasts) - 1; i >= 0; i-- {
-		text := f.Truncate(a.toasts[i].text, p.W-4*p.Margin)
-		w := f.Measure(text) + p.Margin
-		h := f.Height() + p.Margin/2
-		r := gfx.R((p.W-w)/2, y-h, w, h)
+	a.eachToast(func(r gfx.Rect, text string) {
 		c.Fill(r, colOverlay)
 		f.Draw(c, r.X+p.Margin/2, r.Y+p.Margin/4+f.Ascent(), text, colText, c.Bounds())
+	})
+}
+
+// eachToast calls f with each toast's box and text, newest at the bottom.
+func (a *App) eachToast(f func(r gfx.Rect, text string)) {
+	fb := a.F.Body
+	p := a.P
+	y := p.H - p.SafeY - p.MiniBarH - p.Margin
+	for i := len(a.toasts) - 1; i >= 0; i-- {
+		text := fb.Truncate(a.toasts[i].text, p.W-4*p.Margin)
+		w := fb.Measure(text) + p.Margin
+		h := fb.Height() + p.Margin/2
+		f(gfx.R((p.W-w)/2, y-h, w, h), text)
 		y -= h + p.Margin/4
 	}
 }
@@ -829,6 +867,7 @@ func (a *App) drawConfirm(c *gfx.Canvas) {
 
 // drawArt draws a cover (or a placeholder while it loads) into r.
 func (a *App) drawArt(c *gfx.Canvas, id subsonic.ID, r gfx.Rect) {
+	a.markArt(id, r)
 	c.Fill(r, colArtBg)
 	if img, ok := a.Art(id, r.W); ok {
 		// Centre non-square art inside the square.
diff --git a/internal/ui/damage.go b/internal/ui/damage.go
index 62ffb26..361c2b6 100644
--- a/internal/ui/damage.go
+++ b/internal/ui/damage.go
@@ -5,6 +5,7 @@ import (
 	"log"
 
 	"mistersubsonic/internal/gfx"
+	"mistersubsonic/internal/subsonic"
 )
 
 // Partial redraws (Plan 4b). A change that knows exactly which part of the
@@ -62,23 +63,15 @@ func union(a, b gfx.Rect) gfx.Rect {
 	return gfx.R(x, y, max(a.Right(), b.Right())-x, max(a.Bottom(), b.Bottom())-y)
 }
 
-func area(rs []gfx.Rect) int {
-	n := 0
-	for _, r := range rs {
-		n += r.W * r.H
-	}
-	return n
-}
-
-// renderDamage draws and presents the damaged areas only. It reports false
-// when a full frame is better (the areas cover more than half the canvas).
+// renderDamage draws and presents the damaged areas only. A partial frame
+// costs about its area (measured on the MiSTer: never more than a full one,
+// even for a list scroll that covers most of the screen), so there is no
+// size above which a full frame is drawn instead. It reports false when the
+// verify check failed and the full frame is needed.
 func (a *App) renderDamage() (bool, error) {
 	rs := mergeRects(a.damage, maxDamageRects)
 	a.damage = a.damage[:0]
 	c := a.canvas
-	if 2*area(rs) > c.W*c.H {
-		return false, nil
-	}
 	for _, r := range rs {
 		c.SetClip(r)
 		a.drawFrame(c)
@@ -122,3 +115,64 @@ func (a *App) presentRects(c *gfx.Canvas, rs []gfx.Rect) error {
 	}
 	return a.present(c)
 }
+
+// moved records a key's whole visual effect as rs (a list or grid focus
+// move: Moved's rectangles). The event then redraws only those; nil means
+// unknown, and the frame is redrawn in full as for any other key.
+func (a *App) moved(rs []gfx.Rect) {
+	if rs == nil {
+		return
+	}
+	for _, r := range rs {
+		a.Damage(r)
+	}
+	a.exact = true
+}
+
+// Regions that change on their own, recorded while drawing (every frame
+// rebuilds them): the playback position, the scrolling title, and where each
+// cover was drawn.
+
+func (a *App) markTick(r gfx.Rect)    { a.ticks = append(a.ticks, r) }
+func (a *App) markMarquee(r gfx.Rect) { a.mqRect = r }
+
+func (a *App) markArt(id subsonic.ID, r gfx.Rect) {
+	if a.arts == nil {
+		a.arts = map[subsonic.ID][]gfx.Rect{}
+	}
+	a.arts[id] = append(a.arts[id], r)
+}
+
+func (a *App) resetMarks() {
+	a.ticks, a.mqRect = a.ticks[:0], gfx.Rect{}
+	for id := range a.arts {
+		delete(a.arts, id)
+	}
+}
+
+// damageAll damages rs, or redraws the whole frame when there are none.
+func (a *App) damageAll(rs []gfx.Rect) {
+	if len(rs) == 0 {
+		a.dirty = true
+		return
+	}
+	for _, r := range rs {
+		a.Damage(r)
+	}
+}
+
+// coverArrived redraws where the cover id was drawn.
+func (a *App) coverArrived(id subsonic.ID) { a.damageAll(a.arts[id]) }
+
+// toastsArea is where the toasts are drawn now (empty when there are none).
+func (a *App) toastsArea() gfx.Rect {
+	var box gfx.Rect
+	a.eachToast(func(r gfx.Rect, _ string) {
+		if box.Empty() {
+			box = r
+		} else {
+			box = union(box, r)
+		}
+	})
+	return box
+}
diff --git a/internal/ui/grid.go b/internal/ui/grid.go
index 1436849..8b375ac 100644
--- a/internal/ui/grid.go
+++ b/internal/ui/grid.go
@@ -14,6 +14,13 @@ type Grid struct {
 	top   int // first visible row
 	cols  int // at the last Draw
 	rows  int
+
+	// Where the last Draw put the cells, and the focus before the last
+	// Handle (for Moved).
+	area         gfx.Rect
+	x0           int
+	cellW, cellH int
+	prev         int
 }
 
 func (g *Grid) columns() int { return max(g.cols, 1) }
@@ -25,6 +32,7 @@ func (g *Grid) Handle(e input.Event, n int) bool {
 	}
 	cols := g.columns()
 	f := g.Focus
+	g.prev = f
 	switch e.Button {
 	case input.BtnUp:
 		if f < cols {
@@ -71,12 +79,30 @@ func (g *Grid) Draw(c *gfx.Canvas, area gfx.Rect, n, cellW, cellH int, cell func
 		g.top = row - g.rows + 1
 	}
 	x0 := area.X + (area.W-g.cols*cellW)/2
+	g.area, g.x0, g.cellW, g.cellH = area, x0, cellW, cellH
 	for i := g.top * g.cols; i < n && i < (g.top+g.rows)*g.cols; i++ {
 		r := gfx.R(x0+i%g.cols*cellW, area.Y+(i/g.cols-g.top)*cellH, cellW, cellH)
 		cell(i, r, i == g.Focus)
 	}
 }
 
+// Moved is what the last Handle changed on screen: the old and the new
+// focused cell, or the whole grid when the move scrolls it. It is nil
+// before the grid was drawn.
+func (g *Grid) Moved() []gfx.Rect {
+	if g.cellH == 0 || g.area.Empty() {
+		return nil
+	}
+	cols := g.columns()
+	if row := g.Focus / cols; row < g.top || row >= g.top+g.rows {
+		return []gfx.Rect{g.area}
+	}
+	cell := func(i int) gfx.Rect {
+		return gfx.R(g.x0+i%cols*g.cellW, g.area.Y+(i/cols-g.top)*g.cellH, g.cellW, g.cellH)
+	}
+	return []gfx.Rect{cell(g.prev), cell(g.Focus)}
+}
+
 // NearEnd reports whether the focus is within a page of the end (to load more).
 func (g *Grid) NearEnd(n int) bool {
 	return n > 0 && g.Focus >= n-g.columns()*max(g.rows, 1)
diff --git a/internal/ui/list.go b/internal/ui/list.go
index 2d82070..701c4bc 100644
--- a/internal/ui/list.go
+++ b/internal/ui/list.go
@@ -10,6 +10,12 @@ type List struct {
 	Focus int
 	top   int
 	rows  int // visible rows at the last Draw
+
+	// Where the last Draw put the rows, and the focus before the last
+	// Handle: what Moved needs to tell which rows a move changed.
+	area gfx.Rect
+	rowH int
+	prev int
 }
 
 // Handle moves the focus: Up/Down by one, L/R by a page. It reports whether
@@ -20,6 +26,7 @@ func (l *List) Handle(e input.Event, n int) bool {
 		return false
 	}
 	page := max(l.rows-1, 1)
+	l.prev = l.Focus
 	switch e.Button {
 	case input.BtnUp:
 		if l.Focus <= 0 {
@@ -45,6 +52,7 @@ func (l *List) Handle(e input.Event, n int) bool {
 // Draw lays out n rows of height rowH in area, keeping the focus visible,
 // and calls row for each visible index.
 func (l *List) Draw(c *gfx.Canvas, area gfx.Rect, n, rowH int, row func(i int, r gfx.Rect, focused bool)) {
+	l.area, l.rowH = area, rowH
 	l.rows = max(area.H/rowH, 1)
 	l.Focus = min(max(l.Focus, 0), max(n-1, 0))
 	if l.Focus < l.top {
@@ -63,5 +71,19 @@ func (l *List) Draw(c *gfx.Canvas, area gfx.Rect, n, rowH int, row func(i int, r
 	}
 }
 
+// Moved is what the last Handle changed on screen: the old and the new
+// focused row, or the whole list when the move scrolls it. It is nil before
+// the list was drawn.
+func (l *List) Moved() []gfx.Rect {
+	if l.rowH == 0 || l.area.Empty() {
+		return nil
+	}
+	if l.Focus < l.top || l.Focus >= l.top+l.rows {
+		return []gfx.Rect{l.area}
+	}
+	row := func(i int) gfx.Rect { return gfx.R(l.area.X, l.area.Y+(i-l.top)*l.rowH, l.area.W, l.rowH) }
+	return []gfx.Rect{row(l.prev), row(l.Focus)}
+}
+
 // NearEnd reports whether the focus is within a page of the end (to load more).
 func (l *List) NearEnd(n int) bool { return n > 0 && l.Focus >= n-max(l.rows, 1) }
diff --git a/internal/ui/mediakeys.go b/internal/ui/mediakeys.go
index 4d154b5..11ec850 100644
--- a/internal/ui/mediakeys.go
+++ b/internal/ui/mediakeys.go
@@ -28,10 +28,12 @@ func (a *App) mediaKey(e input.Event) bool {
 			step = -step
 		}
 		a.setVolume(a.volumeDB() + step)
+		a.exact = a.onlyPanelShowsVolume()
 	case input.BtnMute:
 		if e.Kind == input.Press {
 			a.toggleMute()
 		}
+		a.exact = a.onlyPanelShowsVolume()
 	case input.BtnPlayPause:
 		if pl != nil && e.Kind == input.Press {
 			pl.TogglePause()
@@ -68,6 +70,13 @@ func (a *App) mediaKey(e input.Event) bool {
 	default:
 		return false
 	}
-	a.dirty = true
-	return true
+	return true // dispatch redraws what changed
+}
+
+// onlyPanelShowsVolume reports whether a volume change shows on screen only
+// in the volume panel (whose area it damages), so nothing else needs a
+// redraw. Settings lists show the level in a row too.
+func (a *App) onlyPanelShowsVolume() bool {
+	_, settings := a.Top().(*SettingsListScreen)
+	return !settings
 }
diff --git a/internal/ui/menu.go b/internal/ui/menu.go
index 1e83d47..0e17781 100644
--- a/internal/ui/menu.go
+++ b/internal/ui/menu.go
@@ -31,6 +31,7 @@ func (s *MenuScreen) Handle(a *App, e input.Event) bool {
 		return false // volume, play/pause and the rest work under the menu
 	}
 	if s.list.Handle(e, len(s.entries)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/screens_album.go b/internal/ui/screens_album.go
index 8e67273..18747df 100644
--- a/internal/ui/screens_album.go
+++ b/internal/ui/screens_album.go
@@ -65,6 +65,7 @@ func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
 		n = 0
 	}
 	if s.list.Handle(e, n) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index f390edc..7bd7e1e 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -96,6 +96,7 @@ func (s *HomeScreen) Handle(a *App, e input.Event) bool {
 	s.sync(a)
 	items := s.items()
 	if s.list.Handle(e, len(items)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press || e.Button != input.BtnA || len(items) == 0 {
@@ -254,6 +255,7 @@ func (s *GenresScreen) Enter(a *App) {
 
 func (s *GenresScreen) Handle(a *App, e input.Event) bool {
 	if s.list.Handle(e, len(s.genres)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press || e.Button != input.BtnA {
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 1cf2395..285c590 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -128,6 +128,7 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 			step = -step
 		}
 		a.setVolume(st.VolumeDB + step) // and saved to the config
+		a.exact = true                  // Now Playing shows no volume: only the panel changes
 		return true
 	}
 	if e.Kind != input.Press {
@@ -209,6 +210,7 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	y += p.Margin / 2
 	d := secs(song.Duration)
 	pos := s.position(a, st, a.o.Now())
+	a.markTick(gfx.R(text.X, y, text.W, barH))
 	c.Fill(gfx.R(text.X, y, text.W, barH), colArtBg)
 	if d > 0 {
 		c.Fill(gfx.R(text.X, y, progressW(text.W, pos, d), barH), colAccent)
@@ -218,6 +220,7 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	if d > 0 {
 		times += " / " + clock(d)
 	}
+	a.markTick(gfx.R(text.X, y, text.W, fs.Height()))
 	fs.Draw(c, text.X, y+fs.Ascent(), times, colDim, c.Bounds())
 	y += fs.Height() + p.Margin/2
 
@@ -261,6 +264,7 @@ func (s *QueueScreen) Enter(a *App)  {}
 func (s *QueueScreen) Handle(a *App, e input.Event) bool {
 	st := a.Player().State()
 	if s.list.Handle(e, len(st.Queue)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/screens_playlists.go b/internal/ui/screens_playlists.go
index fd2bd8f..23c98aa 100644
--- a/internal/ui/screens_playlists.go
+++ b/internal/ui/screens_playlists.go
@@ -135,6 +135,7 @@ func (s *PlaylistScreen) Handle(a *App, e input.Event) bool {
 		n = playlistActionRows + len(s.songs())
 	}
 	if s.list.Handle(e, n) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index 1d1b1c9..795f004 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -26,6 +26,7 @@ func (s *SettingsScreen) Enter(a *App)  {}
 
 func (s *SettingsScreen) Handle(a *App, e input.Event) bool {
 	if s.list.Handle(e, len(settingsItems)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press || e.Button != input.BtnA {
@@ -77,6 +78,7 @@ func (s *SettingsListScreen) Enter(a *App)  {}
 func (s *SettingsListScreen) Handle(a *App, e input.Event) bool {
 	rows := s.rows(a)
 	if s.list.Handle(e, len(rows)) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind == input.Release || len(rows) == 0 {
@@ -159,8 +161,8 @@ func (a *App) setVolume(db float64) {
 // setMuted turns the sound off or back on (spec §6). It isn't saved, so the
 // app always starts with the sound on.
 func (a *App) setMuted(on bool) {
-	a.muted, a.dirty = on, true
-	a.showVolume()
+	a.muted = on
+	a.showVolume() // keys that show it elsewhere too (Settings) redraw it all
 	if pl := a.Player(); pl != nil {
 		pl.SetMuted(on)
 	}
@@ -269,6 +271,7 @@ func (s *ServersScreen) servers(a *App) []config.Server {
 func (s *ServersScreen) Handle(a *App, e input.Event) bool {
 	srvs := s.servers(a)
 	if s.list.Handle(e, len(srvs)+1) {
+		a.moved(s.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press {
diff --git a/internal/ui/views.go b/internal/ui/views.go
index 08a0040..222b541 100644
--- a/internal/ui/views.go
+++ b/internal/ui/views.go
@@ -27,9 +27,11 @@ func (k *cursor) setFocus(i int) { k.grid.Focus, k.list.Focus = i, i }
 func (k *cursor) handle(a *App, e input.Event, n int) bool {
 	var ok bool
 	if a.P.Cover > 0 {
-		ok = k.grid.Handle(e, n)
-	} else {
-		ok = k.list.Handle(e, n)
+		if ok = k.grid.Handle(e, n); ok {
+			a.moved(k.grid.Moved())
+		}
+	} else if ok = k.list.Handle(e, n); ok {
+		a.moved(k.list.Moved())
 	}
 	k.sync(a)
 	return ok
@@ -154,6 +156,7 @@ type songsView struct {
 
 func (v *songsView) Handle(a *App, e input.Event) bool {
 	if v.list.Handle(e, len(v.songs)) {
+		a.moved(v.list.Moved())
 		return true
 	}
 	if e.Kind != input.Press || len(v.songs) == 0 {
diff --git a/internal/ui/volume.go b/internal/ui/volume.go
index f63f45c..6b0d774 100644
--- a/internal/ui/volume.go
+++ b/internal/ui/volume.go
@@ -24,7 +24,17 @@ func volumeLevel(db float64) float64 { return max(0, min(1, (db+60)/60)) }
 // showVolume brings up the panel (again) for volumeShowTime.
 func (a *App) showVolume() {
 	a.volumeUntil = a.o.Now().Add(volumeShowTime)
-	a.dirty = true
+	a.Damage(a.volumePanelRect())
+}
+
+// volumePanelRect is where the volume panel is drawn: centred under the
+// header, sized for the widest label ("Muted").
+func (a *App) volumePanelRect() gfx.Rect {
+	p, f := a.P, a.F.Body
+	icon := f.Height()
+	pad := p.Margin / 2
+	w := pad + icon + pad + max(p.W/4, 6*icon) + pad + f.Measure("Muted") + pad
+	return gfx.R((p.W-w)/2, p.SafeY+p.HeaderH+p.Margin/2, w, icon+2*pad)
 }
 
 // drawSpeaker draws a speaker filling the square r: the box and cone, then
@@ -139,9 +149,7 @@ func (a *App) drawVolumePanel(c *gfx.Canvas) {
 	barW := max(p.W/4, 6*icon)
 	labelW := f.Measure("Muted")
 	pad := p.Margin / 2
-	w := pad + icon + pad + barW + pad + labelW + pad
-	h := icon + 2*pad
-	panel := gfx.R((p.W-w)/2, p.SafeY+p.HeaderH+p.Margin/2, w, h)
+	panel := a.volumePanelRect()
 	fillRoundRect(c, panel, pad, colPanel.WithAlpha(0xF0))
 	x := panel.X + pad
 	drawSpeaker(c, gfx.R(x, panel.Y+pad, icon, icon), level, a.muted, colText)
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/ui ./internal/gfx`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `go test -count=1 -run '^$' -bench Partial -benchtime 10x ./internal/ui`. Expected: six `BenchmarkPartial/…` lines. On the PC the partial cases are a fraction of `full-albums`.

- [ ] **Step 5: Commit**

```bash
git add internal/gfx/pack_bench_test.go internal/ui/app.go internal/ui/bench_test.go internal/ui/damage.go internal/ui/damage_sources_test.go internal/ui/damage_test.go internal/ui/fakes_test.go internal/ui/fixwave_test.go internal/ui/grid.go internal/ui/list.go internal/ui/mediakeys.go internal/ui/menu.go internal/ui/screens_album.go internal/ui/screens_home.go internal/ui/screens_play.go internal/ui/screens_playlists.go internal/ui/screens_settings.go internal/ui/views.go internal/ui/volume.go internal/ui/volume_test.go
git commit -m "ui: focus moves, the tick, the marquee, covers, toasts and the volume panel redraw only their areas" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Full-resolution framebuffer

**Files:**
- Create: `internal/platform/fbmode.go`, `cmd/mistersubsonic/fullres.go`
- Modify: `cmd/mistersubsonic/main.go`, `internal/config/config.go`, `sdcard/mistersubsonic/config.example.toml`
- Test: `internal/platform/fbmode_test.go`, `cmd/mistersubsonic/fullres_test.go` (new)

**Interfaces:**
- **Produces:**
  - `platform.Size{W, H}` and `platform.MaxFullFB` (1920×1200).
  - `platform.OutputMode(iniPath) (Size, bool)`:
    - `video_mode` in `[Menu]` wins over `[MiSTer]`;
    - a custom `W,H,refresh` or a modeline (`hact`, `vact`) gives the size;
    - a preset number gives none.
  - `platform.ShouldSwitch(fb, out)`: `out` is exactly `2×fb` and at most `MaxFullFB`.
  - `platform.FBControl{Cmd, Sys, State, Wait}` and `DefaultFBControl()`, for `/dev/MiSTer_cmd`, `/sys/module/MiSTer_fb/parameters` and `/tmp/mistersubsonic.fb`, with a 1 s wait.
    - `Switch(from, to)` saves `from`, sends `fb_cmd1 8888 1 W H`, and waits for `res_count` to change. On failure it asks for `from` again and forgets the state.
    - `Restore()` puts back the saved size and forgets it. With nothing saved it does nothing.
  - `cmd/mistersubsonic`:
    - `fbControl` is replaceable, so tests never touch the real menu.
    - `fullSize(fb, profileName, dataDir, enabled)` applies to HDMI layouts only. It reads `MiSTer.ini` from beside the app's folder.
    - `openFB(path, profileName, dataDir, enabled)` switches, reopens and checks the size, falling back to the old framebuffer on any failure.
    - `run` restores after the framebuffer closes. `restoreConsole()` (`-restore-console`) restores the framebuffer, then the console.
  - `config.Display.FullResolution` (`full_resolution`, default true), also in the example config.

- [ ] **Step 1: Write the failing tests**

`cmd/mistersubsonic/fullres_test.go` (new file):

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistersubsonic/internal/platform"
)

func TestFullSize(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "mistersubsonic")
	os.WriteFile(filepath.Join(root, "MiSTer.ini"), []byte("[MiSTer]\nvideo_mode=1920,1200,60\n"), 0o644)
	half := platform.Size{W: 960, H: 600}
	if got, ok := fullSize(half, "auto", data, true); !ok || got != (platform.Size{W: 1920, H: 1200}) {
		t.Fatalf("halved HDMI: %v %v", got, ok)
	}
	if _, ok := fullSize(half, "auto", data, false); ok {
		t.Error("switched with full_resolution = false")
	}
	if _, ok := fullSize(half, "crt", data, true); ok {
		t.Error("switched for a CRT layout")
	}
	if _, ok := fullSize(platform.Size{W: 640, H: 240}, "auto", data, true); ok {
		t.Error("switched a 240-line framebuffer")
	}
	if _, ok := fullSize(half, "auto", filepath.Join(t.TempDir(), "x"), true); ok {
		t.Error("switched without a MiSTer.ini")
	}
}

// -restore-console (run by the launcher after every exit, crashes too)
// puts back the framebuffer size the app saved.
func TestRestoreConsoleRestoresTheFramebuffer(t *testing.T) {
	dir := t.TempDir()
	c := platform.FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 50 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(c.State, []byte("960 600\n"), 0o644)
	old := fbControl
	fbControl = func() platform.FBControl { return c }
	defer func() { fbControl = old }()
	restoreConsole() // the console part fails off the MiSTer; the framebuffer part must still run
	if b, _ := os.ReadFile(c.Cmd); strings.TrimSpace(string(b)) != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", b)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("the saved size is still there")
	}
}
```

`internal/platform/fbmode_test.go` (new file):

```go
package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOutputMode(t *testing.T) {
	for _, c := range []struct {
		name, ini string
		want      Size
		ok        bool
	}{
		{"custom mode", "[MiSTer]\nvideo_mode=1920,1200,60\n", Size{1920, 1200}, true},
		{"spaces and a comment", "[MiSTer]\n video_mode = 1920, 1200, 60   ; my TV\n", Size{1920, 1200}, true},
		{"modeline", "[MiSTer]\nvideo_mode=1920,48,32,80,1200,3,6,26,154000\n", Size{1920, 1200}, true},
		{"menu section wins", "[MiSTer]\nvideo_mode=8\n[Menu]\nvideo_mode=640,16,64,80,240,1,3,14,12380\n", Size{640, 240}, true},
		{"sections any case", "[mister]\nvideo_mode=2560,1440,60\n", Size{2560, 1440}, true},
		{"another core's section is ignored", "[MiSTer]\nvideo_mode=1920,1200,60\n[SNES]\nvideo_mode=1280,720,60\n", Size{1920, 1200}, true},
		{"a preset number", "[MiSTer]\nvideo_mode=8\n", Size{}, false},
		{"no video_mode", "[MiSTer]\nvga_scaler=1\n", Size{}, false},
		{"garbage", "[MiSTer]\nvideo_mode=1920,x,60\n", Size{}, false},
		{"commented out", "[MiSTer]\n;video_mode=1920,1200,60\n", Size{}, false},
	} {
		p := filepath.Join(t.TempDir(), "MiSTer.ini")
		os.WriteFile(p, []byte(c.ini), 0o644)
		got, ok := OutputMode(p)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %v %v, want %v %v", c.name, got, ok, c.want, c.ok)
		}
	}
	if _, ok := OutputMode(filepath.Join(t.TempDir(), "missing.ini")); ok {
		t.Error("a missing MiSTer.ini gave a mode")
	}
}

func TestShouldSwitch(t *testing.T) {
	for _, c := range []struct {
		fb, out Size
		want    bool
	}{
		{Size{960, 600}, Size{1920, 1200}, true},   // the user's TV
		{Size{960, 720}, Size{1920, 1440}, false},  // too big to draw
		{Size{1280, 720}, Size{2560, 1440}, false}, // too big
		{Size{1280, 720}, Size{1280, 720}, false},  // not halved
		{Size{960, 600}, Size{1920, 1080}, false},  // a mismatch (another ini?)
		{Size{0, 0}, Size{1920, 1200}, false},
	} {
		if got := ShouldSwitch(c.fb, c.out); got != c.want {
			t.Errorf("ShouldSwitch(%v, %v) = %v", c.fb, c.out, got)
		}
	}
}

// fakeMenu stands in for Main_MiSTer: a command file, and a res_count that
// goes up when a command arrives (unless it is deaf).
func fakeMenu(t *testing.T, deaf bool) FBControl {
	t.Helper()
	dir := t.TempDir()
	c := FBControl{Cmd: filepath.Join(dir, "cmd"), Sys: dir, State: filepath.Join(dir, "state"), Wait: 300 * time.Millisecond}
	os.WriteFile(c.Cmd, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "res_count"), []byte("8\n"), 0o644)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		seen := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
			}
			b, _ := os.ReadFile(c.Cmd)
			if n := strings.Count(string(b), "\n"); n > seen && !deaf {
				seen = n
				os.WriteFile(filepath.Join(dir, "res_count"), []byte(strconv.Itoa(8+n)+"\n"), 0o644)
			}
		}
	}()
	return c
}

func commands(t *testing.T, c FBControl) []string {
	b, _ := os.ReadFile(c.Cmd)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Switching saves the old size, asks for the new one and waits for the
// menu; restoring asks for the old size again and forgets it.
func TestSwitchAndRestore(t *testing.T) {
	c := fakeMenu(t, false)
	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.State); string(b) != "960 600\n" {
		t.Fatalf("state %q", b)
	}
	if err := c.Restore(); err != nil {
		t.Fatal(err)
	}
	if got := commands(t, c); len(got) != 2 || got[0] != "fb_cmd1 8888 1 1920 1200" || got[1] != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", got)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("the state file is still there")
	}
	if err := c.Restore(); err != nil { // nothing saved: nothing to do
		t.Fatal(err)
	}
	if got := commands(t, c); len(got) != 2 {
		t.Fatalf("a restore without state sent %q", got)
	}
}

// A menu that never confirms: Switch gives up, asks for the old size again
// and leaves nothing to restore.
func TestSwitchThatIsNeverConfirmed(t *testing.T) {
	c := fakeMenu(t, true)
	if err := c.Switch(Size{960, 600}, Size{1920, 1200}); err == nil {
		t.Fatal("no error")
	}
	if got := commands(t, c); len(got) != 2 || got[1] != "fb_cmd1 8888 1 960 600" {
		t.Fatalf("commands %q", got)
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("a failed switch left a state file")
	}
}

func TestRestoreWithBadState(t *testing.T) {
	c := fakeMenu(t, false)
	os.WriteFile(c.State, []byte("junk"), 0o644)
	if err := c.Restore(); err == nil {
		t.Fatal("bad state accepted")
	}
	if _, err := os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("bad state kept")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/platform ./internal/config ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: Size
undefined: FBControl
undefined: platform.Size
undefined: fullSize
undefined: platform.FBControl
undefined: fbControl
```

- [ ] **Step 3: Implement**

`cmd/mistersubsonic/fullres.go` (new file):

```go
package main

import (
	"log"
	"path/filepath"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/platform"
	"mistersubsonic/internal/ui"
)

// fbControl is replaceable so tests never touch the real menu.
var fbControl = platform.DefaultFBControl

// fullSize is the framebuffer size to ask for, when the HDMI framebuffer fb
// is the menu's halved output (spec Plan 4b §2): the output size from
// MiSTer.ini, which sits next to the app's folder (/media/fat).
func fullSize(fb platform.Size, profileName, dataDir string, enabled bool) (platform.Size, bool) {
	if !enabled || ui.PickProfile(fb.W, fb.H, profileName).Name != "hdmi" {
		return platform.Size{}, false
	}
	out, ok := platform.OutputMode(filepath.Join(filepath.Dir(dataDir), "MiSTer.ini"))
	if !ok || !platform.ShouldSwitch(fb, out) {
		return platform.Size{}, false
	}
	return out, true
}

// openFB opens the framebuffer, switched to the full output resolution when
// fullSize says so. Any failure falls back to the framebuffer as it was.
func openFB(path, profileName, dataDir string, enabled bool) (*gfx.FB, error) {
	fb, err := gfx.OpenFB(path)
	if err != nil {
		return nil, err
	}
	w, h := fb.Size()
	from := platform.Size{W: w, H: h}
	to, ok := fullSize(from, profileName, dataDir, enabled)
	if !ok {
		return fb, nil
	}
	fb.Close()
	ctl := fbControl()
	if err := ctl.Switch(from, to); err != nil {
		log.Printf("display: full resolution: %v", err)
		return gfx.OpenFB(path)
	}
	if fb, err = gfx.OpenFB(path); err == nil {
		if w, h := fb.Size(); w == to.W && h == to.H {
			log.Printf("display: framebuffer %v, full resolution (was %v)", to, from)
			return fb, nil
		}
		log.Printf("display: asked for %v, got %dx%d; keeping %v", to, w, h, from)
		fb.Close()
	}
	if err := ctl.Restore(); err != nil {
		log.Printf("display: %v", err)
	}
	return gfx.OpenFB(path)
}

// restoreConsole is -restore-console, which the launcher runs after every
// exit: the framebuffer size the app switched from (after a crash too),
// then the console's text mode.
func restoreConsole() error {
	if err := fbControl().Restore(); err != nil {
		log.Printf("display: %v", err)
	}
	return platform.RestoreText()
}
```

`internal/platform/fbmode.go` (new file):

```go
package platform

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Full-resolution framebuffer (Plan 4b). Above 1920x1080 the MiSTer menu
// gives Linux a framebuffer of half the output size (1920x1200 → 960x600),
// which the scaler then doubles: text goes soft. The menu accepts a request
// for another size on its command pipe (what `vmode -r` does), so the app
// asks for the output size and puts the old size back when it exits.
// Nothing here writes to the SD card: MiSTer.ini is only read, the command
// pipe is a FIFO and the state file lives in /tmp (RAM).

// Size is a framebuffer or video mode size in pixels.
type Size struct{ W, H int }

func (s Size) String() string { return fmt.Sprintf("%dx%d", s.W, s.H) }

// MaxFullFB is the largest framebuffer the app asks for (proven on the
// device; larger ones cost too much to draw).
var MaxFullFB = Size{1920, 1200}

// OutputMode reads the menu's video mode from MiSTer.ini: video_mode in
// [Menu] if set, otherwise in [MiSTer]. It knows custom modes (W,H,refresh)
// and full modelines (hact,hfp,hs,hbp,vact,…). A single preset number is
// not decoded: presets are 1080p or smaller (never halved) or larger than
// MaxFullFB, so none can lead to a switch.
func OutputMode(iniPath string) (Size, bool) {
	f, err := os.Open(iniPath)
	if err != nil {
		return Size{}, false
	}
	defer f.Close()
	var section, global, menu string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexAny(line, ";#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.ToLower(strings.TrimSpace(k)) != "video_mode" {
			continue
		}
		switch section {
		case "mister":
			global = strings.TrimSpace(v)
		case "menu":
			menu = strings.TrimSpace(v)
		}
	}
	if menu != "" {
		return parseVideoMode(menu)
	}
	return parseVideoMode(global)
}

func parseVideoMode(v string) (Size, bool) {
	parts := strings.Split(v, ",")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return Size{}, false
		}
		nums[i] = n
	}
	switch {
	case len(nums) == 3: // W,H,refresh
		return Size{nums[0], nums[1]}, true
	case len(nums) >= 9: // hact,hfp,hs,hbp,vact,vfp,vs,vbp,pclk[,…]
		return Size{nums[0], nums[4]}, true
	}
	return Size{}, false
}

// ShouldSwitch reports whether the framebuffer fb is the halved output out
// and out is small enough to draw at: then the app asks for out.
func ShouldSwitch(fb, out Size) bool {
	return fb.W > 0 && fb.H > 0 && out.W == 2*fb.W && out.H == 2*fb.H &&
		out.W*out.H <= MaxFullFB.W*MaxFullFB.H
}

// FBControl changes the framebuffer size through the menu.
type FBControl struct {
	Cmd   string // the menu's command pipe
	Sys   string // the MiSTer_fb module's parameters (res_count counts mode changes)
	State string // where the size to restore is kept
	Wait  time.Duration
}

// DefaultFBControl is the MiSTer's.
func DefaultFBControl() FBControl {
	return FBControl{Cmd: "/dev/MiSTer_cmd", Sys: "/sys/module/MiSTer_fb/parameters",
		State: "/tmp/mistersubsonic.fb", Wait: time.Second}
}

// Switch saves from as the size to restore, then asks for to and waits for
// the menu to apply it. On failure it asks for from again.
func (c FBControl) Switch(from, to Size) error {
	if err := os.WriteFile(c.State, []byte(fmt.Sprintf("%d %d\n", from.W, from.H)), 0o644); err != nil {
		return fmt.Errorf("platform: save the framebuffer size: %w", err)
	}
	if err := c.request(to); err != nil {
		c.request(from)
		os.Remove(c.State)
		return err
	}
	return nil
}

// Restore puts back the size Switch saved, if any, and forgets it. Without
// a saved size it does nothing.
func (c FBControl) Restore() error {
	b, err := os.ReadFile(c.State)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("platform: %w", err)
	}
	var s Size
	if _, err := fmt.Sscanf(string(b), "%d %d", &s.W, &s.H); err != nil || s.W <= 0 || s.H <= 0 {
		os.Remove(c.State)
		return fmt.Errorf("platform: bad framebuffer state %q", strings.TrimSpace(string(b)))
	}
	err = c.request(s)
	os.Remove(c.State)
	return err
}

// request sends fb_cmd1 for s (32 bpp) and waits until res_count changes.
func (c FBControl) request(s Size) error {
	count := c.resCount()
	f, err := os.OpenFile(c.Cmd, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("platform: %w", err)
	}
	_, err = fmt.Fprintf(f, "fb_cmd1 8888 1 %d %d\n", s.W, s.H)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("platform: %s: %w", c.Cmd, err)
	}
	wait := c.Wait
	if wait <= 0 {
		wait = time.Second
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if c.resCount() != count {
			return nil
		}
	}
	return fmt.Errorf("platform: the menu didn't switch the framebuffer to %v", s)
}

func (c FBControl) resCount() string {
	b, _ := os.ReadFile(filepath.Join(c.Sys, "res_count"))
	return strings.TrimSpace(string(b))
}
```

Then Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index a063dc3..2baf87e 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -60,7 +60,7 @@ func main() {
 	flag.BoolVar(&f.restoreConsole, "restore-console", false, "put the console back in text mode and exit (the launcher runs this after the app)")
 	flag.Parse()
 	if f.restoreConsole {
-		if err := platform.RestoreText(); err != nil {
+		if err := restoreConsole(); err != nil {
 			fmt.Fprintln(os.Stderr, "mistersubsonic:", err)
 			os.Exit(1)
 		}
@@ -227,10 +227,15 @@ func run(f flags) (err error) {
 	var inputs []<-chan input.Event
 	switch f.display {
 	case "fbdev":
-		fb, err := gfx.OpenFB(f.fbdev)
+		fb, err := openFB(f.fbdev, profileName, dataDir, cfg.Display.FullResolution)
 		if err != nil {
 			return err
 		}
+		defer func() { // after the framebuffer is closed: its old size back
+			if err := fbControl().Restore(); err != nil {
+				log.Printf("display: %v", err)
+			}
+		}()
 		con, err := platform.GraphicsMode()
 		if err != nil {
 			log.Printf("console: %v (its text may show over the app)", err)
diff --git a/internal/config/config.go b/internal/config/config.go
index 8838360..4530f77 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -52,6 +52,7 @@ type Playback struct {
 type Display struct {
 	Profile            string `toml:"profile"`
 	ScreensaverMinutes int    `toml:"screensaver_minutes"`
+	FullResolution     bool   `toml:"full_resolution"`
 }
 
 type Cache struct {
@@ -62,7 +63,7 @@ type Cache struct {
 func Default() *Config {
 	return &Config{
 		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
-		Display:  Display{Profile: "auto", ScreensaverMinutes: 5},
+		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true},
 		Cache:    Cache{CoverArtMB: 200},
 	}
 }
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index 9913bb0..cae4e5c 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -33,6 +33,7 @@ alsa_device = "default"
 [display]
 profile = "auto"           # auto, hdmi or crt (set crt for a 480i/576i CRT)
 screensaver_minutes = 5    # minutes idle on Now Playing; 0 turns it off
+full_resolution = true     # HDMI above 1080p (e.g. 1920x1200): draw at the output's size, not half
 
 [cache]
 cover_art_mb = 200         # cover art kept on the SD card, per server
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/platform ./internal/config ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `GOOS=linux GOARCH=arm GOARM=7 go vet ./internal/gfx ./internal/input ./internal/platform && ./scripts/test-launcher.sh`. Expected: `test-launcher ok`.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/fullres.go cmd/mistersubsonic/fullres_test.go cmd/mistersubsonic/main.go internal/config/config.go internal/platform/fbmode.go internal/platform/fbmode_test.go sdcard/mistersubsonic/config.example.toml
git commit -m "platform, cmd: ask the menu for the full framebuffer size when it halved it, and put it back on exit" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: The hint bar

**Files:**
- Create: `internal/ui/hints.go`, `internal/ui/hints_screens.go`
- Modify: `internal/ui/app.go`, `internal/ui/screens_settings.go`, `internal/config/config.go`, `sdcard/mistersubsonic/config.example.toml`, `cmd/mistersubsonic/main.go`
- Test: `internal/ui/hints_test.go` (new); `internal/ui/app_test.go`, `internal/ui/browse_test.go` (modified)

**Interfaces:**
- **Consumes:** `Event.Pad` and `Manager.HasPad` (Task 2); `Damage`, `exact` and the verify mode (Tasks 4–5).
- **Produces:**
  - **Types:** `ui.Hint{Button, Pair, Label}`, and `ui.Hinter` (`Hints(a) []Hint`).
    - Helpers: `hk`, `hkPair`, `commonHints` (Open, Menu), `hintLabels`, `hintKey`.
  - **Caps:**
    - `padCap` gives the button names A, B, X, Y, L, R, Start and Select.
    - `keyCap` gives Enter, Esc, Tab, N, PgUp, PgDn, Space, Q and M. Arrows are drawn as triangles. Select has no key.
    - `(*App).capFor(b)` picks the cap for the current input.
  - **Layout:**
    - `hintsOn()`, `hintH()` and `hintRect()`: the bar is the last line inside the title-safe area.
    - `drawFrame` shrinks the body by `hintH()`, draws the bar after the screen, and moves the toasts up.
  - **Content:** `screenHints()` is the screen's list, plus Back and Now Playing where the app handles B and Y. `drawHints` lays it out from the left while it fits.
  - **Source:** `noteSource(e)` sets `App.pad` from each press, and damages the bar when it switches. `ui.Options.PadAtStart` comes from `Manager.HasPad()` in `cmd/mistersubsonic`.
  - **`dispatch`:** an exact key that changes the hint list also damages the bar.
  - **Per-screen hints** (`hints_screens.go`):
    - Now Playing: Pause/Play, Seek, Volume, Prev/Next, Star/Unstar, Queue, Mode.
    - Containers (the sidebar root, tabs) show their focused part's hints.
    - Search and the wizard show Delete only when there is text.
    - Servers follows the focused row.
  - **Setting:** `config.Display.Hints` (`hints`, default true) and Settings → Display → Hints.

- [ ] **Step 1: Write the failing tests**

`internal/ui/hints_test.go` (new file):

```go
package ui

import (
	"fmt"
	"testing"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// hintFixtures are the screens whose hints are checked, as the user meets
// them.
var hintFixtures = map[string]func(ta *testApp) Screen{
	"root":        func(ta *testApp) Screen { return NewRootScreen(ta.P) },
	"albums":      func(ta *testApp) Screen { return NewAlbumsScreen() },
	"album list":  func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") },
	"artists":     func(ta *testApp) Screen { return NewArtistsScreen() },
	"artist":      func(ta *testApp) Screen { return NewArtistScreen(ta.lib.artists[2].Artists[0]) },
	"album":       func(ta *testApp) Screen { return NewAlbumScreen(ta.lib.albums[0]) },
	"genres":      func(ta *testApp) Screen { return NewGenresScreen() },
	"playlists":   func(ta *testApp) Screen { return NewPlaylistsScreen() },
	"playlist":    func(ta *testApp) Screen { return NewPlaylistScreen(ta.lib.playlists[0]) },
	"starred":     func(ta *testApp) Screen { return NewStarredScreen() },
	"search":      func(ta *testApp) Screen { return NewSearchScreen() },
	"queue":       func(ta *testApp) Screen { playingState(ta); return NewQueueScreen() },
	"now playing": func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() },
	"settings":    func(ta *testApp) Screen { return NewSettingsScreen() },
	"playback":    func(ta *testApp) Screen { return newSettingsList("Playback", playbackSettings) },
	"display":     func(ta *testApp) Screen { return newSettingsList("Display", displaySettings) },
	"servers":     func(ta *testApp) Screen { return NewServersScreen() },
	"wizard":      func(ta *testApp) Screen { return NewWizardScreen(false, false) },
}

func hintApp(t *testing.T, p Profile, mk func(ta *testApp) Screen) *testApp {
	t.Helper()
	ta := newTestApp(t, p)
	ta.cfg = config.Default()
	for i := range 40 { // grids with several pages
		ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
	}
	ta.Push(NewHomeScreen())
	ta.Push(mk(ta))
	ta.settle(t)
	return ta
}

// Every hinted button does something on its screen: the frame, the screen
// stack or the player changes when it is pressed.
func TestEveryHintedButtonDoesSomething(t *testing.T) {
	for _, p := range profiles {
		for name, mk := range hintFixtures {
			hs := hintApp(t, p, mk).screenHints()
			if len(hs) == 0 && name != "about" {
				t.Errorf("%s on %s: no hints", name, p.Name)
			}
			for _, h := range hs {
				// A pair is pressed second button first (R, Right, Down), so
				// the first one (L: back a page) then has somewhere to go.
				ta := hintApp(t, p, mk)
				for _, b := range []input.Button{h.Pair, h.Button} {
					if b == input.BtnNone {
						continue
					}
					if h.Pair == input.BtnNone || b == h.Pair {
						ta = hintApp(t, p, mk)
					}
					before := ta.settle(t).ToRGBA()
					depth, calls, top := len(ta.stack), len(ta.pl.calls), ta.Top()
					ta.press(b)
					after := ta.settle(t).ToRGBA()
					if samePixels(before, after) && len(ta.stack) == depth && len(ta.pl.calls) == calls && ta.Top() == top {
						t.Errorf("%s on %s: %v (%q) does nothing", name, p.Name, b, h.Label)
					}
				}
			}
		}
	}
}

// Back and Now Playing are added where the screen leaves B and Y to the app.
func TestCommonHintsAreAddedWhereTheyApply(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["album"])
	if got := hintLabels(ta.screenHints()); got != "Play, Menu, Shuffle, Back" {
		t.Fatalf("album without a queue: %s", got)
	}
	playingState(ta)
	if got := hintLabels(ta.screenHints()); got != "Play, Menu, Shuffle, Back, Now Playing" {
		t.Fatalf("album with a queue: %s", got)
	}
	ta.Push(NewNowPlayingScreen())
	for _, h := range ta.screenHints() {
		if h.Label == "Now Playing" {
			t.Fatal("Now Playing offers Now Playing")
		}
	}
}

// The bar shows gamepad buttons after a gamepad press and keyboard keys
// after a key press, redrawing only itself when it switches.
func TestHintBarFollowsTheInput(t *testing.T) {
	ta, d := partialApp(t, ProfileHDMI, NewAlbumListScreen("Recently added", "newest"))
	if cp, _ := ta.capFor(input.BtnA); cp != "Enter" {
		t.Fatalf("with no gamepad at start A shows %q", cp)
	}
	for _, e := range []input.Event{
		{Button: input.BtnRight, Kind: input.Press, Pad: true}, // the grid focus moves, and the bar switches
		{Button: input.BtnLeft, Kind: input.Press},             // back with a key
	} {
		ta.onInput(e)
		ta.onInput(input.Event{Button: e.Button, Kind: input.Release, Pad: e.Pad})
		ta.settle(t)
		if cp, _ := ta.capFor(input.BtnA); (cp == "A") != e.Pad {
			t.Fatalf("after a press with Pad=%v A shows %q", e.Pad, cp)
		}
		found := false
		for _, r := range presented(t, d) {
			found = found || r.Contains(ta.hintRect().X, ta.hintRect().Y)
		}
		if !found {
			t.Fatalf("switching the bar (Pad=%v) didn't redraw it", e.Pad)
		}
	}
	if _, ok := ta.capFor(input.BtnSelect); ok {
		t.Fatal("a keyboard hint for Select, which no key does")
	}
}

// Settings → Display → Hints turns the bar off and gives its line back.
func TestHintsCanBeTurnedOff(t *testing.T) {
	ta := hintApp(t, ProfileHDMI, hintFixtures["display"])
	if ta.hintH() == 0 {
		t.Fatal("no bar by default")
	}
	s := ta.Top().(*SettingsListScreen)
	for i, r := range s.rows(ta.App) {
		if r.label == "Hints" {
			s.list.Focus = i
		}
	}
	ta.press(input.BtnA)
	ta.settle(t)
	if ta.hintH() != 0 || ta.cfg.Display.Hints {
		t.Fatalf("after Hints → Off: bar %d px, config %v", ta.hintH(), ta.cfg.Display.Hints)
	}
}

func TestGoldenHintBar(t *testing.T) {
	for _, p := range []Profile{ProfileHDMI, PickProfile(960, 600, "auto"), PickProfile(1920, 1200, "auto"), ProfileCRT240, PickProfile(640, 288, "auto")} {
		for _, pad := range []bool{true, false} {
			ta := newTestApp(t, p)
			ta.pad = pad
			playingState(ta)
			ta.Push(NewHomeScreen())
			ta.Push(NewAlbumListScreen("Recently added", "newest"))
			kind := "keys"
			if pad {
				kind = "pad"
			}
			name := p.Name
			if p.W != 1280 && p.Name == "hdmi" {
				name = fmt.Sprintf("hdmi-%dx%d", p.W, p.H)
			} else if p.H == 288 {
				name = "crt288"
			}
			c := ta.settle(t)
			golden(t, "hints-"+kind+"-"+name, cropBottom(c, ta.hintRect().Y-ta.P.MiniBarH))
		}
	}
}

// cropBottom is the canvas from line y down (the goldens keep only the
// bottom of the screen).
func cropBottom(c *gfx.Canvas, y int) *gfx.Canvas {
	out := gfx.NewCanvas(c.W, c.H-y)
	copy(out.Pix, c.Pix[y*c.W:])
	return out
}
```

Then update the existing tests. Save this patch as `/tmp/t7-test.patch` and apply it from the repository root with `git apply /tmp/t7-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/ui/app_test.go b/internal/ui/app_test.go
index 65c933c..25e674b 100644
--- a/internal/ui/app_test.go
+++ b/internal/ui/app_test.go
@@ -156,18 +156,19 @@ func TestSafeAreaAndMiniBarNeedACurrentSong(t *testing.T) {
 	ta.Push(s)
 	ta.settle(t)
 	p := ta.P
-	if s.area.Y != p.SafeY+p.HeaderH || s.area.Bottom() != p.H-p.SafeY {
-		t.Fatalf("body %+v: want from %d to %d", s.area, p.SafeY+p.HeaderH, p.H-p.SafeY)
+	bottom := p.H - p.SafeY - ta.hintH() // the hint bar is the last line
+	if s.area.Y != p.SafeY+p.HeaderH || s.area.Bottom() != bottom {
+		t.Fatalf("body %+v: want from %d to %d", s.area, p.SafeY+p.HeaderH, bottom)
 	}
 	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: -1, NextIndex: -1}
 	ta.settle(t)
-	if s.area.Bottom() != p.H-p.SafeY {
+	if s.area.Bottom() != bottom {
 		t.Fatal("mini bar shown with nothing selected")
 	}
 	ta.pl.st.Index = 0
 	ta.settle(t)
-	if s.area.Bottom() != p.H-p.SafeY-p.MiniBarH {
-		t.Fatalf("body bottom %d with a current song, want %d", s.area.Bottom(), p.H-p.SafeY-p.MiniBarH)
+	if s.area.Bottom() != bottom-p.MiniBarH {
+		t.Fatalf("body bottom %d with a current song, want %d", s.area.Bottom(), bottom-p.MiniBarH)
 	}
 }
 
diff --git a/internal/ui/browse_test.go b/internal/ui/browse_test.go
index 93c0cc1..725b75b 100644
--- a/internal/ui/browse_test.go
+++ b/internal/ui/browse_test.go
@@ -307,7 +307,7 @@ func TestAlbumListRetriesFailedPage(t *testing.T) {
 		t.Fatalf("first page %d", len(s.view.albums))
 	}
 	ta.lib.err = errors.New("boom")
-	for range 20 {
+	for range albumPage { // page down to the end of the first page, whatever the page size
 		ta.press(input.BtnR)
 	}
 	ta.settle(t)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/config ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
ta.hintH undefined (type *testApp has no field or method hintH)
hintApp(t, p, mk).screenHints undefined (type *testApp has no field or method screenHints)
undefined: hintLabels
ta.screenHints undefined (type *testApp has no field or method screenHints)
ta.capFor undefined (type *testApp has no field or method capFor)
ta.hintRect undefined (type *testApp has no field or method hintRect)
```

- [ ] **Step 3: Implement**

`internal/ui/hints.go` (new file):

```go
package ui

import (
	"fmt"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// The hint bar (Plan 4b): one line along the bottom of every screen with
// the buttons that matter there, drawn as gamepad buttons or keyboard keys,
// whichever was used last.

// Hint is one entry: a button (and a second one for pairs such as L/R or
// Left/Right) and what it does on this screen.
type Hint struct {
	Button, Pair input.Button
	Label        string
}

// Hinter is a screen with its own hints, most important first. The app adds
// Back and Now Playing when the screen leaves B and Y to it.
type Hinter interface {
	Hints(a *App) []Hint
}

func hk(b input.Button, label string) Hint        { return Hint{Button: b, Label: label} }
func hkPair(b, p input.Button, label string) Hint { return Hint{Button: b, Pair: p, Label: label} }

// commonHints are for screens without their own list.
var commonHints = []Hint{hk(input.BtnA, "Open"), hk(input.BtnX, "Menu")}

// padCap is a button's name on a gamepad ("" for the D-pad, drawn as arrows).
func padCap(b input.Button) string {
	switch b {
	case input.BtnA, input.BtnB, input.BtnX, input.BtnY, input.BtnL, input.BtnR:
		return b.String()
	case input.BtnStart:
		return "Start"
	case input.BtnSelect:
		return "Select"
	}
	return ""
}

// keyCap is the key for a button on a keyboard (the default key map); ok is
// false when no key does it (Select).
func keyCap(b input.Button) (string, bool) {
	switch b {
	case input.BtnA:
		return "Enter", true
	case input.BtnB:
		return "Esc", true
	case input.BtnX:
		return "Tab", true
	case input.BtnY:
		return "N", true
	case input.BtnL:
		return "PgUp", true
	case input.BtnR:
		return "PgDn", true
	case input.BtnStart:
		return "Space", true
	case input.BtnQueue:
		return "Q", true
	case input.BtnMute:
		return "M", true
	case input.BtnUp, input.BtnDown, input.BtnLeft, input.BtnRight:
		return "", true
	}
	return "", false
}

// capFor is b's cap for the current input, or false when it has none there.
func (a *App) capFor(b input.Button) (string, bool) {
	if a.pad {
		switch b {
		case input.BtnQueue, input.BtnMute:
			return "", false // keyboard only
		}
		return padCap(b), true
	}
	return keyCap(b)
}

func isArrow(b input.Button) bool {
	return b == input.BtnUp || b == input.BtnDown || b == input.BtnLeft || b == input.BtnRight
}

// hintsOn reports whether the bar is shown (Settings → Display → Hints).
func (a *App) hintsOn() bool { return a.cfg == nil || a.cfg.Display.Hints }

// hintH is the bar's height, 0 when it is off.
func (a *App) hintH() int {
	if !a.hintsOn() {
		return 0
	}
	return a.F.Small.Height() + a.P.Margin/2
}

// hintRect is the bar: the last line inside the title-safe area.
func (a *App) hintRect() gfx.Rect {
	h := a.hintH()
	return gfx.R(0, a.P.H-a.P.SafeY-h, a.P.W, h)
}

// screenHints is the top screen's list plus Back and Now Playing when the
// screen leaves B and Y to the app.
func (a *App) screenHints() []Hint {
	top := a.Top()
	var hs []Hint
	if h, ok := top.(Hinter); ok {
		hs = h.Hints(a)
	} else {
		hs = commonHints
	}
	uses := func(b input.Button) bool {
		for _, h := range hs {
			if h.Button == b || h.Pair == b {
				return true
			}
		}
		return false
	}
	out := append([]Hint(nil), hs...)
	if len(a.stack) > 1 && !uses(input.BtnB) {
		out = append(out, hk(input.BtnB, "Back"))
	}
	if _, np := top.(*NowPlayingScreen); !np && a.hasQueue() && !uses(input.BtnY) {
		out = append(out, hk(input.BtnY, "Now Playing"))
	}
	return out
}

// drawHints draws the bar: chips and labels from the left while they fit.
func (a *App) drawHints(c *gfx.Canvas) {
	r := a.hintRect()
	if r.Empty() {
		return
	}
	c.Fill(gfx.R(r.X, r.Y, r.W, a.P.H-r.Y), colPanel) // to the bottom edge, past the safe area
	f := a.F.Small
	p := a.P
	pad := max(p.Margin/4, 2)
	chipH := f.Height()
	chipY := r.Y + (r.H-chipH)/2
	base := chipY + (chipH+f.Ascent()-f.Descent())/2
	x, end := p.Margin, r.Right()-p.Margin
	for _, h := range a.screenHints() {
		caps := []input.Button{h.Button}
		if h.Pair != input.BtnNone {
			caps = append(caps, h.Pair)
		}
		var texts []string
		ok := true
		for _, b := range caps {
			t, has := a.capFor(b)
			ok = ok && has
			texts = append(texts, t)
		}
		if !ok {
			continue
		}
		// Width: one chip for a lone button or a pair of arrows, two for a
		// pair of named buttons (L/R).
		arrows := isArrow(caps[0])
		cw := 0
		if arrows {
			cw = 2*pad + len(caps)*chipH*3/5 + (len(caps)-1)*pad
		} else {
			for _, t := range texts {
				cw += 2*pad + max(f.Measure(t), chipH-2*pad)
			}
			cw += (len(caps) - 1) * pad
		}
		need := cw + pad + f.Measure(h.Label)
		if x+need > end {
			break
		}
		if arrows {
			chip := gfx.R(x, chipY, cw, chipH)
			fillRoundRect(c, chip, pad, colArtBg)
			ax := x + pad
			for _, b := range caps {
				drawArrow(c, gfx.R(ax, chipY+chipH/5, chipH*3/5, chipH*3/5), b, colText)
				ax += chipH*3/5 + pad
			}
			x += cw
		} else {
			for i, t := range texts {
				w := 2*pad + max(f.Measure(t), chipH-2*pad)
				fillRoundRect(c, gfx.R(x, chipY, w, chipH), pad, colArtBg)
				f.Draw(c, x+(w-f.Measure(t))/2, base, t, colText, c.Bounds())
				x += w
				if i < len(texts)-1 {
					x += pad
				}
			}
		}
		x += pad
		f.Draw(c, x, base, h.Label, colDim, c.Bounds())
		x += f.Measure(h.Label) + p.Margin*2/3
	}
}

// drawArrow draws a triangle pointing b's way (Up, Down, Left, Right) in r.
func drawArrow(c *gfx.Canvas, r gfx.Rect, b input.Button, col gfx.Color) {
	x0, y0 := float64(r.X), float64(r.Y)
	x1, y1 := float64(r.Right()), float64(r.Bottom())
	xm, ym := (x0+x1)/2, (y0+y1)/2
	var pts [][2]float64
	switch b {
	case input.BtnUp:
		pts = [][2]float64{{xm, y0}, {x1, y1}, {x0, y1}}
	case input.BtnDown:
		pts = [][2]float64{{x0, y0}, {x1, y0}, {xm, y1}}
	case input.BtnLeft:
		pts = [][2]float64{{x0, ym}, {x1, y0}, {x1, y1}}
	default:
		pts = [][2]float64{{x0, y0}, {x1, ym}, {x0, y1}}
	}
	fillPolygon(c, pts, col)
}

// noteSource follows the input: a press from the other kind of device
// switches the bar between gamepad buttons and keyboard keys.
func (a *App) noteSource(e input.Event) {
	if e.Kind != input.Press || e.Pad == a.pad {
		return
	}
	a.pad = e.Pad
	a.Damage(a.hintRect())
}

// hintLabels joins the labels ("Open, Menu").
func hintLabels(hs []Hint) string {
	var s []string
	for _, h := range hs {
		s = append(s, h.Label)
	}
	return strings.Join(s, ", ")
}

// hintKey identifies a hint list, to tell whether the bar changed.
func hintKey(hs []Hint) string {
	var b strings.Builder
	for _, h := range hs {
		fmt.Fprintf(&b, "%d/%d/%s;", h.Button, h.Pair, h.Label)
	}
	return b.String()
}
```

`internal/ui/hints_screens.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// Each screen's hints, taken from what its Handle does (hints_test.go
// presses every one and checks that something happens). The app adds Back
// and Now Playing (hints.go).

var (
	openHint    = hk(input.BtnA, "Open")
	playHint    = hk(input.BtnA, "Play")
	menuHint    = hk(input.BtnX, "Menu")
	shuffleHint = hk(input.BtnSelect, "Shuffle")
	pageHint    = hkPair(input.BtnL, input.BtnR, "Page")
)

func (s *AlbumScreen) Hints(*App) []Hint     { return []Hint{playHint, menuHint, shuffleHint} }
func (s *ArtistScreen) Hints(*App) []Hint    { return []Hint{openHint, menuHint, shuffleHint} }
func (s *PlaylistScreen) Hints(*App) []Hint  { return []Hint{playHint, menuHint, shuffleHint} }
func (s *PlaylistsScreen) Hints(*App) []Hint { return []Hint{openHint, menuHint} }
func (s *GenresScreen) Hints(*App) []Hint    { return []Hint{openHint} }
func (s *HomeScreen) Hints(*App) []Hint      { return []Hint{openHint} }
func (s *SettingsScreen) Hints(*App) []Hint  { return []Hint{openHint} }
func (s *AboutScreen) Hints(*App) []Hint     { return nil }

func (s *AlbumListScreen) Hints(*App) []Hint {
	return []Hint{openHint, menuHint, shuffleHint, pageHint}
}

func (s *ArtistsScreen) Hints(*App) []Hint {
	return []Hint{openHint, menuHint, hkPair(input.BtnL, input.BtnR, "Letter")}
}

func (s *NowPlayingScreen) Hints(a *App) []Hint {
	play := "Play"
	if pl := a.Player(); pl != nil && pl.State().Status == player.Playing {
		play = "Pause"
	}
	star := "Star"
	if song, ok := a.Player().State().Current(); ok && a.isStarred(songStar(song)) {
		star = "Unstar"
	}
	return []Hint{hk(input.BtnA, play), hkPair(input.BtnLeft, input.BtnRight, "Seek"),
		hkPair(input.BtnUp, input.BtnDown, "Volume"), hkPair(input.BtnL, input.BtnR, "Prev/Next"),
		hk(input.BtnX, star), hk(input.BtnY, "Queue"), hk(input.BtnSelect, "Mode")}
}

func (s *QueueScreen) Hints(*App) []Hint {
	return []Hint{playHint, menuHint, hk(input.BtnY, "Now Playing")}
}

func (s *MenuScreen) Hints(*App) []Hint {
	return []Hint{hk(input.BtnA, "Choose"), hk(input.BtnB, "Close")}
}

func (s *MessageScreen) Hints(*App) []Hint {
	if s.retry == nil {
		return nil
	}
	return []Hint{hk(input.BtnA, s.label)}
}

func (s *SettingsListScreen) Hints(*App) []Hint {
	return []Hint{hkPair(input.BtnLeft, input.BtnRight, "Change")}
}

func (s *ServersScreen) Hints(a *App) []Hint {
	if s.list.Focus >= len(s.servers(a)) {
		return []Hint{hk(input.BtnA, "Add")}
	}
	return []Hint{hk(input.BtnA, "Switch"), menuHint}
}

func (s *UnreachableScreen) Hints(*App) []Hint { return []Hint{hk(input.BtnA, "Choose")} }

func (s *SearchScreen) Hints(*App) []Hint {
	if s.inResults {
		return []Hint{openHint, menuHint, hk(input.BtnB, "Keyboard")}
	}
	return typing(len(s.query) > 0)
}

// typing is an on-screen keyboard's hints: Delete only when there is text.
func typing(text bool) []Hint {
	if !text {
		return []Hint{hk(input.BtnA, "Type")}
	}
	return []Hint{hk(input.BtnA, "Type"), hk(input.BtnX, "Delete")}
}

func (s *WizardScreen) Hints(*App) []Hint {
	if s.step == stepTest {
		return []Hint{hk(input.BtnA, "Choose")}
	}
	return typing(len(s.fields[s.step]) > 0)
}

func (s *FeedScreen) Hints(*App) []Hint {
	if s.resume != nil && s.row == 0 { // the Resume card
		return []Hint{hk(input.BtnA, "Resume")}
	}
	return []Hint{openHint, menuHint, shuffleHint}
}

func (s *starredTab) Hints(a *App) []Hint {
	if s.kind == starSong {
		return []Hint{playHint, menuHint, shuffleHint}
	}
	return []Hint{openHint, menuHint}
}

// Containers show their focused part's hints.

func (s *SidebarRoot) Hints(a *App) []Hint {
	if s.inSidebar {
		return []Hint{openHint}
	}
	return hintsOf(a, s.current())
}

func (s *TabbedScreen) Hints(a *App) []Hint {
	if s.onTabs {
		return []Hint{hkPair(input.BtnLeft, input.BtnRight, "Tab"), openHint}
	}
	return hintsOf(a, s.children[s.tabs.Sel])
}

func hintsOf(a *App, s Screen) []Hint {
	if h, ok := s.(Hinter); ok {
		return h.Hints(a)
	}
	return commonHints
}
```

Then Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 2baf87e..f0c3ff7 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -225,6 +225,7 @@ func run(f flags) (err error) {
 	}
 	var disp gfx.Display
 	var inputs []<-chan input.Event
+	padAtStart := false
 	switch f.display {
 	case "fbdev":
 		fb, err := openFB(f.fbdev, profileName, dataDir, cfg.Display.FullResolution)
@@ -245,6 +246,7 @@ func run(f flags) (err error) {
 		mgr := input.NewManager(input.ManagerOptions{Grab: true})
 		defer mgr.Close()
 		inputs = append(inputs, mgr.Events())
+		padAtStart = mgr.HasPad()
 	case "viewer":
 		prof := ui.PickProfile(1280, 720, profileName)
 		v := devview.New(prof.W, prof.H)
@@ -310,6 +312,7 @@ func run(f flags) (err error) {
 		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
 		ScreenshotDir: screenshotDir(f.screenshots, f.display, dataDir),
 		VerifyRedraw:  f.verifyRedraw,
+		PadAtStart:    padAtStart,
 		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
 	})
 	if err != nil {
diff --git a/internal/config/config.go b/internal/config/config.go
index 4530f77..a39493a 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -53,6 +53,7 @@ type Display struct {
 	Profile            string `toml:"profile"`
 	ScreensaverMinutes int    `toml:"screensaver_minutes"`
 	FullResolution     bool   `toml:"full_resolution"`
+	Hints              bool   `toml:"hints"`
 }
 
 type Cache struct {
@@ -63,7 +64,7 @@ type Cache struct {
 func Default() *Config {
 	return &Config{
 		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
-		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true},
+		Display:  Display{Profile: "auto", ScreensaverMinutes: 5, FullResolution: true, Hints: true},
 		Cache:    Cache{CoverArtMB: 200},
 	}
 }
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 2fa5a4c..dc24e03 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -102,6 +102,9 @@ type Options struct {
 	Version    string // shown in Settings → About
 	// ScreenshotDir is where the screenshot button saves PNGs ("": off).
 	ScreenshotDir string
+	// PadAtStart: a gamepad is connected, so the hint bar starts with
+	// gamepad buttons (otherwise keyboard keys) until the first press.
+	PadAtStart bool
 	// VerifyRedraw checks every partial frame against a full one and logs
 	// any difference (a debugging aid on the device).
 	VerifyRedraw bool
@@ -201,6 +204,7 @@ type App struct {
 	muted       bool      // the sound is off (not saved: the app starts with sound)
 	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
 	shooting    bool      // a screenshot is being saved
+	pad         bool      // the last press came from a gamepad (the hint bar follows it)
 
 	damage       []gfx.Rect                 // changed areas for the next frame (dirty means the whole frame)
 	exact        bool                       // the key being handled damaged exactly what it changed
@@ -242,7 +246,7 @@ func New(o Options) (*App, error) {
 	if pw != a.P.W || ph != a.P.H { // drawn at the display's size: nothing to scale
 		a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
 	}
-	a.verify = o.VerifyRedraw
+	a.verify, a.pad = o.VerifyRedraw, o.PadAtStart
 	if _, ok := o.Display.(gfx.Checker); ok {
 		a.checkAt = o.Now().Add(watchdogEvery)
 	}
@@ -570,6 +574,7 @@ func (a *App) onInput(e input.Event) {
 		}
 		return
 	}
+	a.noteSource(e)
 	now := a.o.Now()
 	a.lastInput = now
 	if a.wake() && e.Kind == input.Press && !isMediaButton(e.Button) {
@@ -609,16 +614,21 @@ func (a *App) onInput(e input.Event) {
 
 func (a *App) dispatch(e input.Event) {
 	a.exact = false
-	top, title := a.Top(), ""
+	top, title, hints := a.Top(), "", ""
 	if top != nil {
-		title = top.Title()
+		title, hints = top.Title(), hintKey(a.screenHints())
 	}
 	defer func() {
 		switch {
 		case !a.exact || a.Top() != top:
 			a.dirty = true // what the key changed is unknown: redraw it all
-		case top != nil && top.Title() != title:
-			a.Damage(a.headerRect()) // e.g. Artists · B follows the focus
+		case top != nil:
+			if top.Title() != title {
+				a.Damage(a.headerRect()) // e.g. Artists · B follows the focus
+			}
+			if hintKey(a.screenHints()) != hints {
+				a.Damage(a.hintRect()) // e.g. Servers: the Add row has no menu
+			}
 		}
 	}()
 	if a.confirm {
@@ -725,18 +735,19 @@ func (a *App) drawFrame(c *gfx.Canvas) {
 	p := a.P
 	// Content stays inside the title-safe area (SafeY lines top and bottom;
 	// panels still run to the edges).
-	body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY)
+	body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY-a.hintH())
 	if top != nil {
 		_, fullscreen := top.(*NowPlayingScreen)
 		if !fullscreen {
 			a.drawHeader(c, top.Title())
-			body = gfx.R(0, p.SafeY+p.HeaderH, p.W, p.H-2*p.SafeY-p.HeaderH)
+			body = gfx.R(0, p.SafeY+p.HeaderH, p.W, body.Bottom()-p.SafeY-p.HeaderH)
 			if a.hasCurrent() {
 				body.H -= p.MiniBarH
 				a.drawMiniBar(c, gfx.R(0, body.Bottom(), p.W, p.MiniBarH))
 			}
 		}
 		top.Draw(a, c, body)
+		a.drawHints(c)
 	}
 	if !a.mq.seen {
 		a.mq = marquee{}
@@ -840,7 +851,7 @@ func (a *App) drawToasts(c *gfx.Canvas) {
 func (a *App) eachToast(f func(r gfx.Rect, text string)) {
 	fb := a.F.Body
 	p := a.P
-	y := p.H - p.SafeY - p.MiniBarH - p.Margin
+	y := p.H - p.SafeY - a.hintH() - p.MiniBarH - p.Margin
 	for i := len(a.toasts) - 1; i >= 0; i-- {
 		text := fb.Truncate(a.toasts[i].text, p.W-4*p.Margin)
 		w := fb.Measure(text) + p.Margin
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index 795f004..639c70c 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -247,6 +247,11 @@ func displaySettings(a *App) []setting {
 				m := cycle(screensaverChoices, d().ScreensaverMinutes, dir)
 				a.UpdateConfig(func(c *config.Config) { c.Display.ScreensaverMinutes = m }, false)
 			}},
+		{"Hints", func(a *App) string { return onOff(d().Hints) },
+			func(a *App, dir int) {
+				on := !d().Hints
+				a.UpdateConfig(func(c *config.Config) { c.Display.Hints = on }, false)
+			}},
 	}
 }
 
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index cae4e5c..8f09758 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -34,6 +34,7 @@ alsa_device = "default"
 profile = "auto"           # auto, hdmi or crt (set crt for a 480i/576i CRT)
 screensaver_minutes = 5    # minutes idle on Now Playing; 0 turns it off
 full_resolution = true     # HDMI above 1080p (e.g. 1920x1200): draw at the output's size, not half
+hints = true               # the bar of buttons (or keys) along the bottom of every screen
 
 [cache]
 cover_art_mb = 200         # cover art kept on the SD card, per server
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./internal/config ./cmd/mistersubsonic`

Expected: `ok`. Golden screenshots written or changed: `album-crt`, `album-hdmi-960x600`, `album-hdmi`, `albums-crt`, `albums-hdmi-960x600`, `albums-hdmi`, `albums-tabs-crt`, `albums-tabs-hdmi`, `artists-crt`, `artists-hdmi`, `hints-keys-crt`, `hints-keys-crt288`, `hints-keys-hdmi-1920x1200`, `hints-keys-hdmi-960x600`, `hints-keys-hdmi`, `hints-pad-crt`, `hints-pad-crt288`, `hints-pad-hdmi-1920x1200`, `hints-pad-hdmi-960x600`, `hints-pad-hdmi`, `home-crt`, `home-hdmi`, `home-minibar-crt`, `home-minibar-hdmi`, `menu-crt`, `menu-hdmi`, `message-crt`, `message-hdmi`, `nowplaying-crt`, `nowplaying-hdmi-960x600`, `nowplaying-hdmi`, `nowplaying-starred-crt`, `nowplaying-starred-hdmi`, `playlist-crt`, `playlist-hdmi`, `playlists-crt`, `playlists-hdmi`, `queue-toast-crt`, `queue-toast-hdmi`, `root-albums-hdmi`, `root-feed-hdmi-960x600`, `root-feed-hdmi`, `search-crt`, `search-hdmi`, `search-results-crt`, `search-results-hdmi`, `settings-playback-crt`, `settings-playback-hdmi`, `settings-servers-crt`, `settings-servers-hdmi`, `starred-tracks-crt`, `starred-tracks-hdmi`, `volume-panel-crt`, `volume-panel-hdmi-960x600`, `volume-panel-hdmi`, `volume-panel-muted-crt`, `volume-panel-muted-hdmi-960x600`, `volume-panel-muted-hdmi`, `wizard-error-crt`, `wizard-error-hdmi`, `wizard-url-crt`, `wizard-url-hdmi`, `wizard-url-problem-crt`, `wizard-url-problem-hdmi`. Open each one and check it: the `hints-pad-*` goldens show rounded chips `A` `X` `Select` `L` `R` `B` `Y` with dim labels (Open, Menu, Shuffle, Page, Back, Now Playing) under the mini bar. The `hints-keys-*` goldens show `Enter` `Tab` `PgUp` `PgDn` `Esc` `N`, with no Select hint. The CRT goldens drop what doesn't fit, whole hints only. Every other screen golden is shorter by the bar, which sits along its bottom.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go internal/config/config.go internal/ui/app.go internal/ui/app_test.go internal/ui/browse_test.go internal/ui/hints.go internal/ui/hints_screens.go internal/ui/hints_test.go internal/ui/screens_settings.go sdcard/mistersubsonic/config.example.toml internal/ui/testdata/golden
git commit -m "ui: a hint bar on every screen, for the gamepad or the keyboard, whichever was used last" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Results, docs, and the check on the TV

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
  - `docs/spikes.md`: "Plan 4b on the MiSTer", with the measurements, and "On the TV: pending".
  - README:
    - the video settings describe full resolution and `full_resolution = false`;
    - the controls describe the hint bar.
  - The main spec:
    - §8.1 gains partial redraws and full resolution, and the new repaint targets;
    - §8.3 gains the hint bar.
  - Checklist:
    - Item 3 no longer calls slow 1080p scrolling expected.
    - New items: 22 (full resolution and its restore), 23 (smooth browsing), 24 (hints), 25 (no stale pixels, with `-verify-redraw`).
    - Log becomes 26.
  - The backlog's 1080p item becomes "faster list scrolling at full resolution".

- [ ] **Step 1: Record the results and the new checks**

Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/README.md b/README.md
index 369c5b2..23424f6 100644
--- a/README.md
+++ b/README.md
@@ -55,17 +55,21 @@ applies per server (each server's cache gets that much).
 ## Video settings (MiSTer.ini)
 
 The app draws on the MiSTer's Linux framebuffer. That framebuffer is sized from `MiSTer.ini`; the
-app never changes the video mode itself. Keep `fb_size=0` (automatic) and `fb_terminal=1` (both
-the defaults).
+app never changes the video mode or `MiSTer.ini` itself. Keep `fb_size=0` (automatic) and
+`fb_terminal=1` (both the defaults).
 
 - **HDMI:** the interface is drawn at the framebuffer's own size, pixel for pixel, so text and
   covers stay sharp at any resolution.
   - `video_mode=0` (1280x720@60) or `7` (1280x720@50) gives a 1280x720 framebuffer.
-  - `video_mode=8` (1920x1080@60) gives 1920x1080. It is the sharpest, but a full redraw takes
-    about 70 ms on the MiSTer, so scrolling is less smooth than at 720p (about 30 ms).
-  - Modes above 1920x1080 (for example `video_mode=1920,1200,60`) get a framebuffer of half the
-    size (960x600): MiSTer's menu halves it, whatever `fb_size` says. The interface fits that
-    size, and the display scales it up.
+  - `video_mode=8` (1920x1080@60) gives 1920x1080.
+  - Modes above 1920x1080 get a framebuffer of half the size from MiSTer's menu, whatever
+    `fb_size` says. For a custom mode up to 1920x1200 (`video_mode=1920,1200,60`, say) the app
+    asks the menu for the full size when it starts, and puts the half size back when it exits
+    (after a crash too, through the launcher). Nothing is written to the SD card. Set
+    `full_resolution = false` under `[display]` in `config.toml` to keep the half size.
+  - Redraws only touch what changed: moving the focus, the progress and scrolling titles cost a
+    few milliseconds even at 1920x1200. Scrolling a long list redraws the list, about 40 ms a
+    step at 1920x1200 (about 25 steps a second).
 - **CRT (15 kHz):** add a `[Menu]` section, for example the 240p mode SAM uses for its CRT
   video:
 
@@ -100,6 +104,9 @@ Settings → Playback; changing the volume turns the sound back on. A multimedia
 keys work on every screen: volume up/down (held to repeat), mute, play/pause, next, previous, and
 fast-forward/rewind to seek. A volume panel shows the level for a moment whenever it changes.
 
+**Hints:** the bar along the bottom of every screen shows what the buttons do there: as gamepad
+buttons after a gamepad press, as keys after a key press. Settings → Display → Hints turns it off.
+
 **Screenshots:** Print Screen, or Alt+Scroll Lock (MiSTer's own screenshot keys), saves the
 screen as a PNG in `/media/fat/screenshots/MiSTer_Subsonic/`. The MiSTer Companion remote's
 Capture screenshot button works too.
diff --git a/docs/spikes.md b/docs/spikes.md
index f291933..a7de5f1 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -133,3 +133,36 @@ This MiSTer: HDMI at `video_mode=1920,1200,60`, so the framebuffer is 960×600×
 - **Now Playing's small volume indicator:** the user doesn't like it. Plan 4b removes it and keeps the volume panel.
 - **Hotkey hints:** the user wants hints for the gamepad and keyboard on every screen (Plan 4b).
 - **Media keys and the volume panel (items 19–20):** not reported yet.
+
+## Plan 4b on the MiSTer (2026-09-30)
+
+Measured with the test binaries only (`ui.test -test.bench 'Partial|Repaint'`, `gfx.test -test.bench 'Pack'`); nothing was drawn on the TV.
+
+**A full-frame pass is memory-bound on the Cortex-A9.** A CPU profile of a native 1080p frame had `Canvas.Clear` at 37%, cover `Blit` at 25%, and the framebuffer copy took another 19 ms. `Clear` indexed `c.Pix` inside its loop, which reloads the slice on every store; a local slice made it about 3× faster:
+
+| Full frame (draw only) | Before | After |
+|---|---|---|
+| 960×600 albums / feed | 13.5 / 14.7 ms | 10.7 / 11.8 ms |
+| 1280×720 albums / feed | 22.2 / 22.5 ms | 17.3 / 18.4 ms |
+| 1920×1080 albums / feed | 49.4 / 48.1 ms | 37.3 / 37.6 ms |
+| 1920×1200 albums | 50.2 ms | 38.0 ms |
+
+**Partial frames at 1920×1200** (draw, then the copy of the damaged rectangles):
+
+| Case | Draw | Copy | Total |
+|---|---|---|---|
+| Full frame | 38.0 ms | 21.8 ms | ≈ 60 ms |
+| Focus move in a cover grid (two cells) | 9.7 ms | 2.2 ms | ≈ 12 ms |
+| Focus move in a list (two rows) | 8.6 ms | 2.1 ms | ≈ 11 ms |
+| List scroll step (the list area) | 20.6 ms | ≈ 20 ms | ≈ 41 ms |
+| Progress tick (bar and times) | 0.7 ms | < 1 ms | ≈ 1 ms |
+| Marquee frame (one line) | 1.1 ms | < 1 ms | ≈ 2 ms |
+
+- **Targets** (Plan 4b spec §3.6): focus moves, the tick and the marquee ≤ 10 ms: met for the tick and marquee, and within 2 ms for focus moves. A list scroll step ≤ 40 ms: about 41 ms, where a full frame was ≈ 72 ms before this plan.
+- **Scroll-by-shift was measured and dropped.** Moving a 1640×1000 area up one row takes 15.4 ms, filling it 6.2 ms: on this memory bus shifting pixels costs more than redrawing them.
+- **The framebuffer copy stays a `copy`.** A 32-bit store loop was 30% slower (11.2 against 8.7 ms in the same test).
+- **No size threshold for partial frames.** A partial frame measured never slower than a full one, even at 92% of the screen, so any damaged area is drawn partially.
+
+**Full resolution:** the menu accepts `fb_cmd1 8888 1 1920 1200` (the font test card, see "Plan 4 on the MiSTer"). The app's switch and restore were tested against a fake command pipe; on the TV: pending.
+
+**On the TV:** pending. Run `docs/testing-on-mister.md` items 3 and 22–25 and record them here.
diff --git a/docs/superpowers/plans/backlog.md b/docs/superpowers/plans/backlog.md
index 249ece8..c7eb16d 100644
--- a/docs/superpowers/plans/backlog.md
+++ b/docs/superpowers/plans/backlog.md
@@ -17,7 +17,7 @@ Suggested order:
 - **Seeks in a sized MP3 reuse the stream.** Every seek in a raw MP3 of known size reopens the stream: a few HTTP requests, and the queued next track is dropped. When the byte estimate falls inside the reader's window, seek that reader and open a fresh decoder on it instead.
 - **Accurate VBR MP3 seeks.** Read the Xing/Info header's 100-point table of contents (in the first frame after the ID3v2 tag), and use it instead of pure proportion. Without a table, keep the proportional estimate.
 - **Text fields trim by width, not by rune** (`drawField`).
-- **Faster redraws at 1080p.** A native 1920×1080 frame takes about 68 ms on the MiSTer (Plan 4's measurements in `docs/spikes.md`). Redraw only what changed for the common cases: the focus moving in a list or grid, the progress bar, the marquee, the volume panel. Present only the changed rows.
+- **Faster list scrolling at full resolution.** Plan 4b made focus moves, the tick and the marquee partial (≤ 12 ms at 1920×1200); a scroll step still redraws the list area, about 40 ms. The A9 is memory-bound (shifting the pixels measured slower than redrawing them), so the next step would be fewer bytes per frame, not a smarter redraw.
 - **The deferred minors** in `docs/superpowers/plan-2a-followups.md`, under "Plan 2c minors", "Plan 3a minors", "Plan 3b minors" and "Plan 4 minors". Pick the ones that still matter, for example:
   - `Promote` allocating under the stream lock;
   - the launcher's TERM handling;
diff --git a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
index be79786..a885402 100644
--- a/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
+++ b/docs/superpowers/specs/2026-09-28-mister-subsonic-design.md
@@ -223,9 +223,11 @@ The loop records the **frame index where each track starts in the ring's output*
 ### 8.1 Rendering
 
 - **Framebuffer.** `gfx` opens `/dev/fb0`, reads the geometry with the `FBIOGET_*SCREENINFO` ioctls and mmaps it (16 or 32 bpp). If the fbdev mmap fails, it falls back to mapping `/dev/mem` at `smem_start`.
-- **Double buffering.** All drawing goes to a back buffer. Presenting copies only the dirty rectangles. The UI redraws on events, not continuously; the only periodic redraw is the position tick.
+- **Double buffering.** All drawing goes to a back buffer. The UI redraws on events, not continuously.
+- **Partial redraws** (Plan 4b, `docs/superpowers/specs/2026-09-30-mister-subsonic-plan-4b-design.md`). A change that knows its area damages it; the frame then runs the normal drawing clipped to the damage and copies only those rectangles to the framebuffer. Focus moves in lists and grids, the position tick, the marquee, arriving covers, toasts and the volume panel are partial; anything else redraws the whole frame. A verify mode (every UI test, and `-verify-redraw` on the device) checks each partial frame against a full one.
+- **Full resolution** (Plan 4b). When the menu halved an HDMI framebuffer (the output in `MiSTer.ini` is exactly twice its size, and no bigger than 1920×1200), the app asks for the full size on `/dev/MiSTer_cmd` at start and restores the old size on exit; `-restore-console` restores it after a crash. `display.full_resolution = false` turns it off.
 - **Canvas per profile:**
-  - **HDMI** draws at the framebuffer's own size and fills it, with no scaler. The 1280×720 layout is the design size: every length is scaled by `min(W/1280, H/720)`, and fonts stay at 15/12/10 px or more (title, body, small). A 16:10 screen gets more rows instead of bars. The MiSTer menu halves framebuffers above 1920×1080 (1920×1200 gives 960×600), and the app draws that size.
+  - **HDMI** draws at the framebuffer's own size and fills it, with no scaler. The 1280×720 layout is the design size: every length is scaled by `min(W/1280, H/720)`, and fonts stay at 15/12/10 px or more (title, body, small). A 16:10 screen gets more rows instead of bars. The MiSTer menu halves framebuffers above 1920×1080 (1920×1200 gives 960×600); the app switches those back to full size (see Full resolution above).
   - **CRT** keeps its logical size and is scaled to the framebuffer with nearest neighbour (integer scale where possible) and letterboxed.
 
 | Profile | Canvas | Picked when |
@@ -233,7 +235,7 @@ The loop records the **frame index where each track starts in the ring's output*
 | HDMI | the framebuffer's size (layout designed at 1280×720) | `auto` and fb height > 288, or `display.profile = "hdmi"` |
 | CRT | 320×240 or 320×288, with pixel aspect correction; 480/576-line framebuffers are line-doubled | `auto` and fb height ≤ 288, or `display.profile = "crt"` |
 
-**Repaint target:** a full repaint takes under 30 ms on the Cortex-A9 at 960×600 and on CRT. Native 1080p is the exception: about 68 ms, sharp, but held scrolling redraws at about 15 frames a second. Partial redraws for it are in the backlog (`docs/superpowers/plans/backlog.md`).
+**Repaint target:** on the Cortex-A9, a full repaint takes under 30 ms at 960×600 and on CRT. At 1920×1080 and 1920×1200 a full frame takes about 60 ms, so those rely on partial redraws: a focus move, the tick and the marquee cost ≤ 12 ms, and a list scroll step about 40 ms (Plan 4b's measurements in `docs/spikes.md`).
 
 `display.profile` accepts `auto`, `hdmi` or `crt`. A 480i/576i CRT can't be told apart from a 480p/576p HDMI framebuffer, so `auto` picks HDMI; CRT users on interlaced modes set `crt` explicitly, and the README says so.
 
@@ -308,6 +310,8 @@ Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, Space = play/pause, PgU
 
 **Screenshot key:** Print Screen or Scroll Lock (MiSTer's Alt+Scroll Lock, which the MiSTer Companion remote sends) saves the frame on screen as `YYYYMMDD_HHMMSS.png` to `/media/fat/screenshots/MiSTer_Subsonic`. It is handled before everything else, the screensaver included, and does not count as activity.
 
+**Hint bar:** a line along the bottom of every screen shows the buttons that matter there (the screen's own list, plus Back and Now Playing where the app handles B and Y), drawn as gamepad buttons or keyboard keys after the last press of either kind. Settings → Display → Hints turns it off (`display.hints`).
+
 Quitting the app is Settings → Exit, or holding B for 2 s on the Home root, with a confirmation.
 
 ### 8.4 Input (`internal/input`)
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index d588a20..9071dc2 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -24,8 +24,7 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
 3. **HDMI.** At 720p, 1080p and a mode above 1080p such as 1920x1200
    (`video_mode`, see the README): the image fills the screen and the text is
    sharp. Covers load in the grids. Scrolling a long list keeps up with a held
-   D-pad. At 1080p the text is sharp, but held scrolling redraws at about 15
-   frames a second: that is expected, not a failure.
+   D-pad, at 1080p and 1920x1200 too.
 4. **CRT.** At 240p and 288p: the CRT layout, with nothing important cut off
    by overscan. Tune the title-safe margins if needed.
 5. **Controllers.** The user's MiSTer mapping works. Unplug the pad while the
@@ -76,7 +75,20 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
     remote's Capture screenshot button. Each shows "Screenshot saved", adds a
     PNG to `/media/fat/screenshots/MiSTer_Subsonic/`, and the Companion shows
     the picture.
-22. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+22. **Full resolution.** With `video_mode=1920,1200,60`: `log.txt` says
+    "framebuffer 1920x1200, full resolution (was 960x600)", and the text is as
+    sharp as MiSTerHiFi's. After exit, and after `kill -9 $(pidof
+    mistersubsonic)` over ssh, `cat /sys/module/MiSTer_fb/parameters/mode`
+    shows 960 600 again and the menu and other scripts look as before.
+23. **Smooth browsing.** Hold the D-pad in a cover grid and in a long list
+    (Artists): the focus keeps up. The progress bar and a scrolling title move
+    smoothly.
+24. **Hints.** Every screen shows a hint bar. It shows A/B/X/Y after a
+    gamepad press and Enter/Esc/Tab/N after a key press. Settings → Display →
+    Hints → Off removes it.
+25. **No stale pixels.** Start once over ssh with `-verify-redraw`, browse
+    for a minute, exit: `log.txt` has no "partial redraw differs" line.
+26. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
```

The numbers in `docs/spikes.md` were measured on the user's MiSTer while this plan was written, with the test binaries only. If the MiSTer answers at Step 4 and the user agrees, run them again after `make deploy-dev`. Correct any figure off by more than 10%.

```bash
./ui.test -test.run '^$' -test.bench 'Partial|Repaint/.*native' -test.benchtime 30x
./gfx.test -test.run '^$' -test.bench 'Pack' -test.benchtime 30x
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
git add README.md docs/spikes.md docs/superpowers/plans/backlog.md docs/superpowers/specs/2026-09-28-mister-subsonic-design.md docs/testing-on-mister.md
git commit -m "docs: Plan 4b results on the MiSTer; TV checks for full resolution, smooth browsing and hints" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 4: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" line and stop here.

- [ ] **Step 5: If it answers, deploy and hand over to the user (ask first)**

Ask the user before doing anything on the device.
- `make deploy MISTER=<ip>` replaces the installed app. Their `config.toml` is kept.
- The checks run on their TV and switch its framebuffer, and they may play sound. Turn the volume down first.

With their go-ahead, deploy. Then ask them to run checklist items 3 and 22–25 of `docs/testing-on-mister.md`:
- sharp text at 1920×1200;
- the framebuffer back after exit and after `kill -9`;
- smooth browsing;
- the hints;
- a `-verify-redraw` run.

Afterwards:
- Record what they report under "Plan 4b on the MiSTer" in `docs/spikes.md`, replacing "pending".
- Commit: `git commit -am "docs: Plan 4b on the TV" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"`.
- Anything that isn't a small fix goes to `docs/superpowers/plans/backlog.md`.

---

## After this plan

- Faster list scrolling at full resolution (backlog A), if the TV check calls for it.
- The rest of the TV checklist (CRT margins, BGM and SAM, long files and memory).
- The first release, only when the user asks.
- The backlog's small follow-ups, the visualizer and the web remote: the user asked for these to be planned next.
