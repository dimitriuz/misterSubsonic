# MiSTer Subsonic — Plan 4b design: full resolution, partial redraws, hotkey hints

Addendum to `2026-09-28-mister-subsonic-design.md` (the main spec). Where this document and the main spec differ, this one wins for the features below, and the main spec's §8.1 and §8.3 are updated by the plan to match.

## 1. Why

On the user's TV (Plan 4's check, 2026-09-30):
- **Text looks blurry.** The HDMI output is 1920×1200, so the MiSTer menu gives Linux a framebuffer of half that size, 960×600. It then scales the framebuffer up 2×. Smooth, antialiased text smears when stretched.
  - MiSTerFin looks sharper with an even smaller picture because it uses pre-rendered bitmap fonts.
- **A full-resolution framebuffer is possible.** A test card showed that the menu accepts `vmode -r 1920 1200 rgb32`, and `vmode -r 960 600 rgb32` puts it back.
  - At 1920×1200 the app's current text rendering looked best of five variants: smooth (as now), hinted, higher-contrast, no antialiasing, and hinted plus higher-contrast. So the text rendering stays as it is.
- **Full resolution is slow without partial redraws.** A full redraw at 1920×1200 takes about 75 ms on the Cortex-A9.
  - A device profile of a native 1080p frame shows `Canvas.Clear` at about 37% and cover `Blit` at about 25%. The framebuffer copy takes about 19 ms more.
  - The CPU is bound by memory bandwidth: every full-frame pass costs about 20 ms.
  - The user wants browsing to stay smooth, so the app must stop touching every pixel on every change.
- **The user wants hotkey hints** for the gamepad and keyboard on every screen.
- **The user doesn't like Now Playing's small volume indicator.** The volume panel stays.

**Success means:**
- sharp text at 1920×1200 on the user's TV;
- a focus move, the progress tick and the marquee cost ≤ 10 ms a frame, and a list scroll step ≤ 40 ms, on the MiSTer at 1920×1200;
- the menu and other scripts get their framebuffer back after the app exits or crashes;
- every screen shows the hints for the input device used last.

## 2. Full-resolution framebuffer

### 2.1 When the app switches

At start, on the framebuffer display, the app looks for an HDMI output that the MiSTer halved. It does nothing when any of these holds:
- the layout is CRT;
- `[display] full_resolution = false`;
- the display isn't `fbdev`.

1. **Read the current framebuffer size** `w×h` from `/dev/fb0`, as today.
2. **Read the output size** `W×H` from `video_mode` in `/media/fat/MiSTer.ini`. The file is only read, never written.
   - The `[Menu]` section's value wins over `[MiSTer]`. Section names are matched case-insensitively. Comments after `;` or `#` are ignored.
   - **Custom mode** `W,H,refresh`, for example `1920,1200,60`: `W×H` as given.
   - **Full modeline** of 9 or more numbers, `hact,hfp,hs,hbp,vact,vfp,vs,vbp,pclk[,…]`: `W = hact`, `H = vact`.
   - **A single preset number** (for example `8`) never causes a switch, so no preset table is needed. The common presets are 1080p or smaller, and the menu doesn't halve those. The few larger ones are above the 1920×1200 limit anyway.
   - A missing file, a missing key or an unparsable value means the output size is unknown.
3. **Switch only when** `W == 2w`, `H == 2h`, and `W×H ≤ 1920×1200`, the largest size proven on the device. Otherwise keep the framebuffer as it is. This covers an unknown size, a mismatch (an alternative ini, for example), 1440p and 4K.

### 2.2 Switching and restoring

- **Save the old size first.** Write the current size to `/tmp/mistersubsonic.fb` (`/tmp` is RAM) as `w h`, before changing anything.
- **Send the command.** Write `fb_cmd1 8888 1 W H\n` to `/dev/MiSTer_cmd`, the menu's command pipe, as `vmode -r` does.
- **Wait for confirmation.** `/sys/module/MiSTer_fb/parameters/res_count` must change within 1 s, polled every 50 ms.
  - Then reopen `/dev/fb0`. Its size must be `W×H`.
  - If not, send `fb_cmd1 8888 1 w h` to put it back, and carry on at `w×h` with a log line.
- **The layout** is `PickProfile(W, H)`: Plan 4's HDMI layout scaled to the framebuffer. At 1920×1200 the scale is 1.5, so the body text is 36 px.
- **Normal exit:** after the display is closed, the app sends `fb_cmd1 8888 1 w h`, waits for `res_count` in the same way, and deletes `/tmp/mistersubsonic.fb`.
- **After a crash:** `mistersubsonic -restore-console`, which the launcher already runs after every exit, reads `/tmp/mistersubsonic.fb`. When the file exists, it restores that size the same way and deletes the file. With no file it does nothing.
- **Nothing is written to the SD card.** `MiSTer.ini` is read once. `/dev/MiSTer_cmd` is a pipe and `/tmp` is RAM.

### 2.3 Code

- `internal/platform/fbmode.go` (Linux only, with a no-op elsewhere):
  - `OutputMode(iniPath) (w, h int, ok bool)`;
  - `SwitchFB(cmdPath, sysDir, statePath string, from, to Size) error`;
  - `RestoreFB(cmdPath, sysDir, statePath string) error`.
  - The paths are parameters, so tests use a fake command file, a fake `/sys` directory and a temporary state file.
- `cmd/mistersubsonic`:
  - It decides and switches before opening the display for good, then opens it at the new size.
  - It restores on exit.
  - `-restore-console` also calls `RestoreFB`.
- `config`: `[display] full_resolution = true` (default) turns the switch on.

## 3. Redraw only what changed

### 3.1 Principle

Screens keep drawing exactly as today: the same code and the same state changes. The app only restricts which pixels are written, and which are copied to the framebuffer. Because the drawing logic always runs in full (focus, marquee state, lazy loading), a partial frame and a full frame differ only in pixels outside the damage.

### 3.2 gfx

- **Clip rectangle.** `Canvas` gets `SetClip(r Rect)` and `ClearClip()`. `Fill`, `Blit`, `Font.Draw` and the polygon helpers in `ui` write only inside the clip. They reject work that lies wholly outside it before touching pixels.
- **Partial presents.** A new optional display interface `PartialPresenter`, with `PresentRects(c *Canvas, rs []Rect) error`, copies only the given rectangles.
  - `FB` implements it, for all bpp paths, and keeps its watchdog mirror correct.
  - `Headless` and the dev viewer don't implement it. They get full frames, so tests and the viewer see complete pictures.

### 3.3 Damage tracking in ui

- **State.** `App` keeps `damage []gfx.Rect` next to `dirty`, which still means a full frame. The call `a.Damage(r)` adds a rectangle.
- **`render` with damage only:**
  1. Merge the rectangles: overlapping or touching ones are joined.
  2. If the union covers more than half the canvas, draw a full frame instead.
  3. Otherwise, for each rectangle: set the clip, fill the background colour, and run the normal draw sequence (header, mini bar, hint bar, screen, toasts, volume panel, confirm) clipped to it. Then `PresentRects`, or `Present` when the display can't take rectangles.
- **Redraw regions recorded while drawing.** Anything that can later change on its own records its rectangle during each draw:
  - `a.markTick(r)`: regions that change with playback position (Now Playing's bar and times, the mini bar's bar);
  - `a.markMarquee(r)`: the scrolling title;
  - `a.markArt(id, r)`: each cover cell or thumbnail, by art ID.
  - Each full draw rebuilds these records.

### 3.4 Damage sources

| Change | Damage |
|---|---|
| `List` focus moves, no scroll | old and new row |
| `List` scrolls | the list's area |
| Cover grid focus moves, no scroll | old and new cell |
| Cover grid scrolls | the grid's area |
| Progress tick (0.5 s) | the `markTick` regions |
| Marquee frame (100 ms) | the `markMarquee` region |
| A cover arrives (`ArtReady`) | the `markArt` regions for that ID (a full frame if the ID is unknown) |
| Volume panel shows, changes or hides | its box (the union of old and new) |
| Toast appears or expires | the toast area |
| The hint bar switches between gamepad and keyboard | the hint bar |
| Anything else: a screen push or pop, data loaded, player status, the screensaver, config changes, the wizard | a full frame, as today |

### 3.5 Verify mode

`App.verify` is on in every UI test through the test helper, and on the device with `mistersubsonic -verify-redraw`. After each partial frame, it renders a full frame into a second canvas and compares the two.
- **In tests** a difference fails the test and names the first differing pixel.
- **On the device** it logs the difference and presents the full frame.

### 3.6 Targets and fallback

Measured on the MiSTer at 1920×1200, with `ui.test` benchmarks:
- a focus move, the tick, the marquee or a cover arrival: ≤ 10 ms;
- a list scroll step: ≤ 40 ms.

If a scroll step misses its target in the prototype, add scroll-by-shift for `List`:
1. Move the list area's pixels by the scroll distance in the canvas.
2. Draw only the newly revealed rows and the changed focus rows.
3. Present the list area.

Moving the rows reserved for the second core is not done: the cores share the memory bandwidth, so it gains little.

## 4. Hotkey hints

### 4.1 Look and place

- **The bar.** A one-line footer at the very bottom of every screen, including Now Playing, below the mini bar. Its height is the small font's height plus a margin. The content area shrinks by that height.
- **Where it sits.**
  - On CRT it sits inside the title-safe area (`SafeY`).
  - When the screensaver is on, the bar is hidden.
- **Each hint.** A chip, then a label in `colDim`.
  - The chip is a rounded `colArtBg` box with the button in `colText`, in the bold small font.
  - Gamepad chips show `A`, `B`, `X`, `Y`, `L`, `R`, `Start`, `Select`, and `◀▶` or `▲▼` for the D-pad.
  - Keyboard chips show key caps: `Enter`, `Esc`, `Tab`, `N`, `Q`, `M`, `PgUp`/`PgDn`, `Space`, `S`, and `◀▶`/`▲▼` for the arrows.
- **Order and overflow.** Hints are laid out left to right in order of importance. A hint that doesn't fit is dropped, along with every hint after it.

### 4.2 Input source

- `input.Event` gets `Pad bool`.
  - Each device's translator sets it. A device counts as a gamepad when it reports gamepad buttons (`BTN_SOUTH`…`BTN_THUMBR`, `BTN_DPAD_*`) or absolute axes. Otherwise it is a keyboard.
  - The Companion's virtual controller is therefore a gamepad.
  - The dev viewer sends keyboard events.
- **Which set shows.**
  - At start the bar shows the gamepad set if a gamepad is connected. `Options.PadAtStart` is set by `cmd/mistersubsonic` from the devices it opened. Otherwise it shows the keyboard set.
  - After that, the first press from the other kind of device switches the set, and only the hint bar is damaged.

### 4.3 Content

- **The interface.** Screens may implement `Hints(a *App) []Hint`, where `Hint{Button input.Button; Pair input.Button; Label string}`. `Pair` is for two-button hints such as L/R, or ◀▶ with Left and Right.
  - Labels may depend on state, for example `Pause` while playing and `Play` otherwise.
  - Screens without the method get the common set: A Open, B Back, X Menu, Y Now Playing.
- **Keyboard caps.** They come from one table, derived from the default key map, so each label is written once.
- **Per screen.** Every screen type gets its own list, taken from what its `Handle` actually does. For example:
  - **Browse lists and grids:** Open/Play, Back, Menu, Now Playing, Shuffle (Select), Page (L/R).
  - **Now Playing:** Play/Pause, Seek (◀▶), Volume (▲▼), Prev/Next (L/R), Star (X), Queue (Y), Modes (Select).
  - **Settings lists:** Change, Adjust (◀▶), Back.
  - **The search keyboard and the wizard:** Type, Delete, Done, Back.
  - **The X menu and message screens:** their own short lists.
- **Setting.** Settings → Display → Hints, On or Off; On by default. It is saved as `[display] hints = true`.

## 5. Now Playing without the volume indicator

- The status line goes back to the status and modes only: `▶ Playing` plus shuffle and repeat.
- The small speaker and bar (`drawVolumeInline`) are removed, and the status line shows no volume text.
- The volume panel stays as in Plan 4. Settings keeps showing dB.

## 6. Testing

- **platform:**
  - `OutputMode` over custom modes, modelines, preset numbers, `[Menu]` overriding `[MiSTer]`, comments, broken and missing files.
  - The switch rule.
  - `SwitchFB` and `RestoreFB` against a fake command file and a fake `res_count`, including a confirmation that never comes.
  - The state file's lifecycle.
- **cmd:** `-restore-console` restores from the state file, and does nothing without one. `scripts/test-launcher.sh` covers the launcher path.
- **gfx:**
  - The clip on every primitive, at the canvas edges and outside them.
  - `PresentRects` against a full `Present` on every bpp.
- **ui:**
  - Verify mode on in every UI test.
  - A test per damage source that checks both the damage and the result.
  - The fallback to a full frame.
  - Hint content per screen, where every hinted button changes something on that screen.
  - The input-source switch.
  - The hints setting.
- **Goldens:**
  - the hint bar on every layout (HDMI 1280×720, 960×600, 1920×1200, CRT 240 and 288), in the gamepad and keyboard sets;
  - Now Playing without the indicator.
- **Benchmarks on the MiSTer:** `Repaint` cases at native 1920×1200 for a full frame, a focus move, the tick, the marquee and a scroll step. `Pack` cases for partial rectangles.
- **Sound safety as before:** tests use fakes or the null device, and touch no real device.

## 7. Docs

- **README:** full resolution (what it does, `full_resolution = false`, and that it restores on exit), and the hints.
- **`docs/testing-on-mister.md`:**
  - full resolution, restored after exit and after `kill -9`;
  - smooth scrolling;
  - the hints following the input;
  - no stale pixels, checked with `-verify-redraw`.
- **Main spec:** §8.1 (full resolution, partial redraws) and §8.3 (hints).
- **Backlog:** the 1080p redraw item is done.

## 8. Out of scope

- Changing `MiSTer.ini`, or any video mode other than the framebuffer's size.
- Framebuffers larger than 1920×1200 (1440p, 4K). They keep the halved framebuffer.
- Cached layers, and drawing on the second core.
- A help overlay listing every control. The bar shows the important ones.
