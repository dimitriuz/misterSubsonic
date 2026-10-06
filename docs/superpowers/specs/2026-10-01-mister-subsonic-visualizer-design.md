# MiSTer Subsonic — Plan 6 design: the visualizer

Addendum to `2026-09-28-mister-subsonic-design.md` (the main spec). The main spec's §2 left a visualizer out of v1, and §13 lists it as a candidate. Where this document and the main spec differ, this one wins for the features below. The plan updates the main spec's §8 (UI) and §13 to match.

Backlog entry: `docs/superpowers/plans/backlog.md`, section B.

## 1. Why, and what success means

The user wants the music to look alive on the TV, in two ways (2026-10-01):
- **A panel on Now Playing:** something moving next to the cover and title when you glance at the TV.
- **A full-screen ambient mode:** the TV is left on as a music display, and the visualizer is the main picture.

Four styles: spectrum bars, oscilloscope, VU meters and a spectrogram (waterfall).

**Success means:**
- The picture follows the music as it is heard: what the speakers play, not what was just decoded.
- The visualizer never causes an audio dropout on the MiSTer.
- It works on HDMI at full resolution (1920×1200) and on a CRT (320×240).
- With the visualizer off, nothing costs CPU beyond today.
- It is off by default.

**The CPU is still unknown.** The MiSTer is off while this is designed. The frame rates are therefore constants plus an automatic safety valve (§5.4). The first device session measures them and sets the defaults (§9).

## 2. Controls and behaviour

### 2.1 Now Playing
- **Select:** a short press cycles Off → Bars → Scope → VU → Waterfall → Off.
  - A toast names the new style ("Visualizer: Bars").
  - The choice is saved as `[display] visualizer`.
  - Holding Select still mutes.
  - Shuffle and repeat move off Select (§2.3).
- **Start:** toggles the full-screen mode (§2.2). On every other screen Start still pauses. On Now Playing, A pauses.
- **X:**
  - A short press opens Now Playing's menu: Star/Unstar, Shuffle (on/off) and Repeat (off/all/one).
  - Holding X for 1 s stars or unstars the song at once. The press and hold work like Select's: every press starts a fresh hold, and a lost Release is recovered.
- **Unchanged:** A pause, B back, Left/Right seek, Up/Down volume, L/R previous/next, Y queue.

### 2.2 The full-screen mode
- **Layout:** the visualizer fills the screen, inside the title-safe area on a CRT. One small line in a corner shows the title, artist and elapsed time.
- **Burn-in:** the corner line moves to another corner once a minute, so nothing static stays on an OLED or plasma TV.
- **The hint bar** is hidden. It appears for 3 s after any press.
- **Controls:**
  - All of Now Playing's controls work, Select and X included.
  - B or Start leaves.
  - Entering with the visualizer Off shows Bars, and that choice is saved.
- **Leaving Now Playing ends it.** Y to the queue, B, or the track list ending all leave full screen with it. It is a screen pushed on Now Playing; it is not shown anywhere else.

### 2.3 Shuffle and repeat
- They move from Select to Now Playing's X menu.
- The queue screen's X menu also gets Shuffle and Repeat. Its existing "Remove <song>" and "Clear queue" entries stay.
- The behaviour of both modes is unchanged.

### 2.4 Hints
- Now Playing's hints become:
  - Pause/Play;
  - Seek, Volume, Prev/Next;
  - Menu (X), with "hold X Star";
  - Queue (Y);
  - Visualizer (Select), with "hold Select Mute";
  - Full screen (Start).
- The keyboard set gets the same entries where a key exists:
  - Start has a keyboard key: Space.
  - Select is V on a keyboard, so the keyboard hint bar shows V for it.
- `TestEveryHintedButtonDoesSomething` covers the new hints, the hold hints included.

### 2.5 The screensaver
- In full screen, the screensaver doesn't start while music plays.
- Paused or stopped, it starts after its idle time as today.
- On Now Playing with the panel, the screensaver works as today. The panel is small, and the cover and text are static.

## 3. Layout

### 3.1 The panel
- **HDMI:**
  - The panel sits in the empty part of the right-hand column, below the "Next:" line and down to the bottom edge of the cover.
  - It is as wide as the progress bar, about 1050×180 px at 1920×1200.
  - Nothing else on Now Playing moves.
- **CRT:**
  - A strip about 20 px high between the "Next:" line and the bottom of the cover.
  - Bars and VU are the useful styles there. Scope and Waterfall still draw, at that size.
- **Off:** with the panel Off, the area stays empty, as today.

### 3.2 The styles
- **Bars:**
  - log-spaced bands, 32 on HDMI and 16 on CRT, with a 1 px gap on CRT and a few px on HDMI;
  - each band has a falling peak cap;
  - the colour runs from the theme accent at the bottom to white at the top, and the peak cap is brighter.
- **Scope:**
  - the left and right channels mixed to one line;
  - lined up on a rising zero crossing so the picture doesn't jitter;
  - decimated to the area's width, and drawn with the accent colour.
- **VU:** two horizontal LED-style meters, L and R.
  - Each has a level bar, a peak marker that falls slowly, and a red zone above −3 dB.
  - The scale is −40 to +3 dB.
- **Waterfall:**
  - a sweeping spectrogram: a cursor column moves left to right, painting one new spectrum column per frame over the oldest one, like a radar screen;
  - the frequency runs bottom (low) to top (high);
  - the palette runs dark blue → cyan → yellow → white and is precomputed in 256 steps;
  - the picture never scrolls, because moving the whole area each frame costs too much on the A9 (§5.3);
  - a column blends the two nearest bands in the palette index, so a hot band fades off above and below it instead of showing a block per band;
  - a strip wider than one column blends from the previous frame's spectrum to this frame's across its width, so the picture doesn't step from frame to frame.

## 4. Data flow

### 4.1 The tap (`internal/audio/tap.go`)
- **What it is:** `Tap` wraps the output device (`audio.Output`), and the engine writes through it, unchanged.
- **Write:**
  - copies the frames the device actually took into the tap's own ring;
  - the ring holds 32768 stereo float32 frames (about 0.68 s, 256 KB);
  - it also counts the frames written.
- **`Consumed`, `SetPaused`, `SetVolume`, `Close`** pass through.
- **`Flush`** passes through and clears the tap.
- **`Window(dst []float32) (n int)`:**
  - fills dst with the latest frames that end at the device's `Consumed()` position, which is what is playing now;
  - returns fewer frames when the ring doesn't hold enough, for example after a flush;
  - the device ring holds up to about 0.5 s not yet played, so the tap must hold that plus the window.
- **Where it sits:**
  - after ReplayGain, which the engine applies;
  - before the volume and mute, which the device applies.
  - So the picture doesn't shrink with the volume.
- **Locking:**
  - A mutex is held only while copying.
  - The playback path gains a copy of a few KB per write.
  - Write never blocks on a reader for longer than that copy.
- **Idle:**
  - A reader marks the tap as read at each `Window` call.
  - If nothing has read for 1 s, Write skips the copy.
  - So a hidden visualizer costs nothing.

### 4.2 Analysis (`internal/viz`)
Pure Go, with no cgo and no allocations per frame after setup.
- **FFT:**
  - a radix-2, in-place float32 FFT;
  - 2048 points on HDMI and 1024 on CRT, with a Hann window;
  - left and right are mixed to mono first.
- **Bands:**
  - log-spaced from 40 Hz to 16 kHz;
  - the magnitude goes to dB, then a 0–1 level over a −70 to 0 dB range;
  - fast attack, a slow release of about 0.5 s to zero, and peak caps that hold 0.4 s and then fall.
- **VU:**
  - per-channel RMS over the window, plus the peak;
  - VU-style ballistics: about 300 ms integration, and the peak marker falls about 1.5 dB per 100 ms.
- **Scope:** the window from a rising zero crossing, downsampled to N points.
- **Waterfall:** a band vector with more bands (64 on HDMI, 32 on CRT), mapped through the palette.
- **Silence:** a floor at −70 dB, so noise doesn't twitch the bars.
- **Time:** smoothing uses the real frame time, so a lower frame rate doesn't change the speed of the motion.

### 4.3 Drawing (`internal/ui/viz.go`, `internal/ui/screens_viz.go`)
- **The styles:** each one draws into a rectangle (`drawViz(c, r, style, state)`). The panel and full screen share them.
- **Panel damage:** the visualizer damages only what changed in the panel (a bar's own small area, the waterfall's new strip), not the whole panel, so the rest of Now Playing isn't redrawn.
  - Its own small areas are drawn directly, without being merged into one bounding box, and presented as separate rectangles. Damage from anything else (a toast, the hint bar) still goes through `App.Damage` and the merged partial redraws.
- **Full-screen damage:** full screen damages its visualizer area.
  - The corner line is damaged only when its text changes (each second for the time) or when it moves.
- **Verify mode** (on in every UI test) must pass with the visualizer running.

## 5. Timing and CPU

### 5.1 The frame clock
- **When it runs:** while a visualizer is visible (the panel on, or full screen) and the player is Playing, the app wakes at the visualizer's rate:
  - 30 fps on HDMI;
  - 20 fps on CRT.
- **It joins the existing wake loop:** `onWake` gets one more deadline. No new goroutine.

### 5.2 Pause and stop
- The window freezes when the device stops consuming. The levels and peaks fall to zero over their release times.
- Once everything is at zero, the frame clock stops.
- It starts again on the next Playing state.

### 5.3 The cost model
- Partial redraws measured about 11 ms for a focus move at 1920×1200.
- Moving pixels measured slower than redrawing them on this memory bus. That rules out a scrolling waterfall: hence the sweep.
- A 2048-point FFT is a few tens of microseconds on a desktop, and expected well under 2 ms on the A9.
- Decode plus resample takes 18% of one core (spike 3), and the A9 has two cores.

### 5.4 The safety valve
- **Measuring:** the app measures each visualizer frame: analysis, draw and present.
- **Stepping down:** if the average over 1 s goes over 60% of the frame's budget (for example 20 ms at 30 fps), the rate steps down: 30 → 15 → 10 fps.
- **Stepping up:** after 10 s with the average under 60% of the faster level's budget (what would step it down again), it steps back up one level.
- **Resizing:** when the picture changes size (the panel to full screen and back), the valve starts again at the top rate with its measurements cleared, because what it learnt in one size isn't true in the other.
- **Log:** a step down is logged, and so is staying over budget at the lowest rate, each kind at most once a minute, so a valve that cycles doesn't write the SD card's log every few seconds.
- **Priority:** audio and input always go first.

### 5.5 Configuration
- **`[display] visualizer`:** "off", "bars", "scope", "vu" or "waterfall". The default is "off".
- **Settings → Display → Visualizer:** cycles the same values, with a help line.
- **Frame rates:** the two target rates are constants in `internal/ui`, not config. The device session (§9) may change them.

## 6. Edge cases

- **A seek, a skip or a new queue:** the engine flushes the device, and the tap clears with it. The visualizer drops to zero and follows the new audio within about one window, 40 ms.
- **A gapless track change:** no flush, so the picture runs straight through.
- **Mute and volume:** the picture keeps moving, because the tap sits before the volume. The mute state is shown as today.
- **Sources:** the engine always outputs 48 kHz stereo, so the visualizer sees one format only. A mono source gives both VU meters the same level.
- **No sound device** (dev viewer, tests): the tap works on miniaudio's null device, so the desktop viewer shows the visualizer.
- **Unknown song duration:** no effect on the visualizer.
- **Errors:** the visualizer has no error paths of its own. Analysis on a short window (fewer frames than the FFT size) zero-pads.

## 7. Code

| Where | What |
|---|---|
| `internal/audio/tap.go` | `Tap`: wraps `Output`, keeps the ring, `Window`, the idle skip |
| `internal/viz` | FFT, Hann window, bands, VU, scope, palette, smoothing; pure Go, tested with synthetic signals |
| `internal/ui/viz.go` | the four styles' drawing, the frame clock, the safety valve |
| `internal/ui/screens_viz.go` | the full-screen screen, the corner line, the hint auto-hide |
| `internal/ui/screens_play.go` | the panel; Select cycles; Start goes to full screen; X short is the menu and X hold is the star; the hints |
| `internal/ui/screens_play.go` (queue screen) | Shuffle and Repeat in the queue's X menu |
| `internal/ui/screens_settings.go` | Settings → Display → Visualizer |
| `internal/config` | `[display] visualizer`, also in `config.example.toml` |
| `cmd/mistersubsonic` | the `Tap` between the engine and the device; it is passed to the UI as `ui.Options.Visual` |

The UI sees the tap only through an interface (`Window(dst) int`), so tests feed it fixed signals.

## 8. Testing

- **`internal/viz`:**
  - a 1 kHz sine peaks in the right band;
  - a sweep moves across the bands in order;
  - silence gives all zeros;
  - the ballistics follow the frame time;
  - a left-only signal moves only the left VU meter;
  - the scope lines up on a rising zero crossing;
  - the FFT is checked against a slow DFT reference.
- **`internal/audio`, the tap:**
  - with a fake output, `Window` returns the frames that end at `Consumed()`;
  - a flush clears the window;
  - a short ring returns fewer frames;
  - the idle skip works;
  - writes are never blocked by a reader beyond the copy (race detector);
  - the engine tests run with the tap in place.
- **`internal/ui`:**
  - **goldens:** each style in the panel and in full screen, on HDMI 1280×720, 1920×1200 and CRT 320×240, fed a fixed test signal;
  - **controls:** Select cycles and saves the setting, hold Select mutes, Start toggles full screen, B leaves, short X opens the menu, and hold X stars;
  - **the queue menu:** it has Shuffle and Repeat;
  - **hints:** `TestEveryHintedButtonDoesSomething`, with the new hints;
  - **the screensaver** doesn't start in full screen while playing, and starts when paused;
  - **the frame clock** stops after pause once the levels are at zero;
  - **the safety valve** steps down and back up with a fake clock;
  - **the corner line** moves each minute;
  - **verify mode** is on throughout.
- **Benchmarks:** `BenchmarkVizFrame`, per style, for the panel and full screen at 1920×1200 and 320×240, in the ARM test binaries, for §9.
- **Sound:** no test plays sound. The engine tests use the null device.

## 9. On the MiSTer (when it is back)

- **Benchmarks:** run `ui.test -test.bench VizFrame` and record the numbers in `docs/spikes.md`.
- **Defaults:** set the rate defaults so that each style's frame fits in 60% of its budget.
- **Listening check:** play a FLAC album with each style in full screen for a few minutes. There must be no dropouts, and the log must show no "visualizer: slowing to" lines at the chosen defaults.
- **The TV check:**
  - the panel's place on HDMI and CRT;
  - that the motion feels in time with the sound;
  - the corner line moving;
  - the screensaver behaviour.
- **Ask the user each time** before anything plays or is shown on the TV.

## 10. Docs
- **README:** the visualizer, its controls (Select, Start, X short and hold), and the moved shuffle and repeat.
- **`docs/testing-on-mister.md`:** new items for §9.
- **The main spec:** §2 no longer lists the visualizer as a non-goal; §8's Now Playing controls; §13.
- **The backlog:** B is marked done, except the device numbers.

## 11. Out of scope
- Beat detection, effects, and colour themes beyond the one palette.
- The visualizer in the mini bar or on any other screen.
- A user-set frame rate.
- An EQ.
