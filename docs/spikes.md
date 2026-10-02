# Spikes

## Spike 1 — Navidrome stream behaviour (2026-09-28, Navidrome 0.59.0 (cc3cca60))

- ping: ok / openSubsonic: yes / apiKeyAuthentication: no (advertised extensions: `transcodeOffset`, `formPost`, `songLyrics`, `indexBasedQueue`)
- raw FLAC `Range: bytes=0-`: `HTTP/2 206`, Content-Range `bytes 0-55163944/55163945`, Accept-Ranges `bytes`, Content-Type `audio/flac`
- raw FLAC `Range: bytes=1000000-`: `HTTP/2 206`, Content-Range `bytes 1000000-55163944/55163945`
- transcoded mp3 (`format=mp3&maxBitRate=320`): `HTTP/2 200`, Content-Length absent (chunked), Accept-Ranges `none`, audio=yes (`file`/ffprobe confirm MPEG layer III, 320 kbps, 44.1 kHz)
- timeOffset=60: honoured — verified by decoding the transcoded stream to PCM and cross-correlating its RMS energy envelope against reference clips decoded from the raw FLAC at t=0 and t=60 (no audio played). The `timeOffset=60` stream correlates 0.95 with the raw t=60 reference (and -0.17 with t=0), while the plain stream correlates 0.91 with the raw t=0 reference (and -0.16 with t=60) — a clean, unambiguous match confirming the server seeks the transcoder rather than restarting from 0. This is consistent with the server advertising the `transcodeOffset` OpenSubsonic extension.

Test song used: FLAC id `b75pBS5s0xAnICU7XJuXju` ("Wake Circling Above", 55,163,945 bytes, 414.82 s, 1057 kbps), fetched via `getAlbumList2` → `getAlbum`.

Commands used (with `$AUTH` standing in for `u=...&t=...&s=...&v=1.16.1&c=MiSTerSubsonic&f=json`, loaded from `.env` and never printed):

```bash
curl -s "$NAVIDROME_URL/rest/ping.view?$AUTH"
curl -s "$NAVIDROME_URL/rest/getOpenSubsonicExtensions.view?$AUTH"
curl -s "$NAVIDROME_URL/rest/getAlbumList2.view?$AUTH&type=newest&size=10"
curl -s "$NAVIDROME_URL/rest/getAlbum.view?$AUTH&id=<albumId>"

SONG='b75pBS5s0xAnICU7XJuXju'
curl -s -D - -o /dev/null -H 'Range: bytes=0-' "$NAVIDROME_URL/rest/stream.view?$AUTH&id=$SONG&format=raw"
curl -s -D - -o /dev/null -H 'Range: bytes=1000000-' "$NAVIDROME_URL/rest/stream.view?$AUTH&id=$SONG&format=raw"

curl -s -D - -o /tmp/t.mp3 --max-time 5 "$NAVIDROME_URL/rest/stream.view?$AUTH&id=$SONG&format=mp3&maxBitRate=320"
curl -s -D - -o /tmp/t2.mp3 --max-time 5 -H 'Range: bytes=0-' "$NAVIDROME_URL/rest/stream.view?$AUTH&id=$SONG&format=mp3&maxBitRate=320&timeOffset=60"
file /tmp/t.mp3 /tmp/t2.mp3
```

**Decision:** §5 confirmed. Raw FLAC streams return `206` with `Accept-Ranges: bytes` and correct `Content-Range`, so the range-based stream reader design in spec §5 stands as designed for this server. Transcoded streams are `200`, chunked (no `Content-Length`), `Accept-Ranges: none` — as expected for on-the-fly transcoding, and consistent with §5's sequential-read path for transcoded audio. `timeOffset` is honoured by this server (verified above), so seeking on a transcoded stream reopens the connection at the requested offset rather than restarting from 0.

## Spike 2 — toolchain and audio on the device (2026-09-30, silent part)

- **Device:** ARMv7 (Cortex-A9), Buildroot, glibc 2.31, bash 5.0.18, util-linux `flock`. `socat`, `pidof` and `timeout` are present; `pgrep` is not. About 490 MB RAM, 427 MB available.
- **Result:** the zig-built `audio.test` (needs glibc 2.29) runs, and every test passes on the device through miniaudio's null backend. That covers decoders, resampler, engine, gapless and the null device.
- **Listening test (with the user's go-ahead, into headphones):**
  - The ALSA `default` device is `plug → rate (S16_LE, 48 kHz) → file "/dev/MrAudio"`, with a `Dummy` hw slave for timing. `/proc/<pid>/fd` confirms that miniaudio's default opens `/dev/MrAudio` and `pcmC0D0p`, the same path as `aplay -D default`.
  - The first run of `MSS_DEVICE_TEST=1 ./audio.test -test.run RealDevice` wasn't heard.
  - A quiet `aplay -D default` tone (−30 dBFS) was heard.
  - A second run of the test was heard clean, with no clicks or crackle reported.
  - The first silence did not reproduce. Watch for it on the app's first sound after a reboot.
  - `alsa_device = "default"` stays.

## Spike 3 — Cortex-A9 costs (2026-09-30)

**Decode + resample to 48 kHz (`audio.test -test.bench DecodeResample`), share of one core:**

| Source | q3 | q5 | q7 |
|---|---|---|---|
| FLAC 44.1k/16 | 12.0% | **18.0%** | 26.5% |
| FLAC 96k/24 | — | **14.7%** | — |

- **Decision:** keep quality 5 (the default). It is under the 25% target.

**UI repaint (`ui.test -test.bench Repaint`), per frame:**

| Screen | Time |
|---|---|
| albums-hdmi-1080p | 165 ms |
| nowplaying-hdmi-1080p | 108 ms |
| feed-hdmi-1080p | 176 ms |
| search-hdmi-1080p | 74 ms |
| marquee-hdmi-1080p | 158 ms |
| albums-crt-240p | **9 ms** |

- **Target:** 30 ms per frame.
  - CRT meets it.
  - HDMI at 1080p does not.
- **Profile of feed-hdmi-1080p on the device:**
  - 46% goes to the benchmark's `fakeArt.Get`, which makes a gradient image per call with integer division (`runtime.udiv`). The real art source returns cached images.
  - 19% goes to `Headless.Present`, which copies the frame; the framebuffer packs it instead.
  - 15% goes to `Scaler.Scale` (1280×720 → 1920×1080).
  - The rest is `Canvas.Clear`, `Blit` and text.
- **Estimate for the real app:** excluding the benchmark's own costs, a 1080p frame is roughly 60–90 ms. That is still over the target.
- **Next:**
  - Fix the benchmark (cached fake art, a display without the copy).
  - Profile again.
  - Work on the scaler, clear and pack paths.
- **This MiSTer's framebuffer is 960×600 at 32 bpp**, from `video_mode=1920,1200,60`; with `fb_size=0` the framebuffer is halved above 1920×1080. The 1280×720 HDMI layout is therefore scaled down to 0.75. That makes a frame cheaper, but thin text loses rows and columns (nearest neighbour).

## Plan 2a on the MiSTer

pending — MiSTer unavailable. Still to do (plan 2a Task 10): run the `Repaint` benchmark on the device (`albums-hdmi-1080p` ≤ 30 ms confirms §8.1), then have the user try the app on the TV (screen fill on HDMI/CRT, controller mapping, B-hold exit, covers).

## Plan 2b on the MiSTer

pending — MiSTer unavailable. Still to do:
- Run `ui.test -test.bench Repaint` on the device. The new cases are feed-hdmi-1080p and search-hdmi-1080p.
- Tune the CRT title-safe margins on a real CRT.
- Have the user try the sidebar, grids and search with the controller and a USB keyboard.

## Plan 2c on the MiSTer

pending — MiSTer unavailable. Still to do: run the setup wizard on the TV with the controller and a USB keyboard; Settings → Servers switch; check the screensaver on HDMI and CRT; confirm the config lands in /media/fat/mistersubsonic (exFAT can't store the 0600 mode; it only protects on the desktop).

## Plan 3a on the MiSTer

pending — MiSTer unavailable. Run the checklist in `docs/testing-on-mister.md` and record the results here: install with `make deploy`, then start from the Scripts menu; check the console and cursor, BGM and SAM, exit and crash recovery, the watchdog, and the log.

## Plan 3b on the MiSTer

pending — MiSTer unavailable. Plan 3b's fixes are verified on the host. On the device, run `docs/testing-on-mister.md` items 6 (long FLAC), 16 (long MP3), 17 (ReplayGain) and 18 (memory), together with spikes 2 and 3 and the `Repaint` benchmarks.

## Plan 4 on the MiSTer (2026-09-30)

This MiSTer: HDMI at `video_mode=1920,1200,60`, so the framebuffer is 960×600×32. The app used to draw 1280×720 and scale it down, which blurred text. It now draws at the framebuffer's size.

**Render speed** (`ui.test -test.bench Repaint`, fixed benchmark: cached covers, a display that drops frames; `gfx.test -test.bench Pack` for the framebuffer copy), per frame:

| Case | Draw | Pack | Total |
|---|---|---|---|
| Before: 1280×720 scaled to 960×600 | ~35 ms | 17.5 ms | ~52 ms |
| Native 960×600: albums / feed / Now Playing | 13.5 / 14.7 / 8.9 ms | 5.4 ms | **≈ 20 ms** |
| Native 1280×720: albums / feed | 22.2 / 22.5 ms | 8.6 ms | ≈ 31 ms |
| Native 1920×1080: albums / feed | 49.4 / 48.1 ms | 19.2 ms | ≈ 68 ms |
| 1280×720 scaled to 1920×1080 (the old 1080p path): albums / feed | 51.7 / 49.2 ms | 19.2 ms | ≈ 70 ms |
| CRT 240p: albums | 4.7 ms | — | — |

- The pack is a straight copy for 32 bpp little-endian framebuffers (was a per-byte loop: 17.5 → 5.4 ms at 960×600).
- `Blit` has a one-to-one path for covers drawn at their size.
- On the Cortex-A9, Go's `memmove` is slower than the store loop for `Clear` and `Fill`, and a non-inlined per-pixel helper slows `Blit`; both were tried and left out.
- **Target (30 ms):** met at 960×600 and on CRT, and just about at 720p (31 ms). Native 1080p is about 68 ms: sharp, but held scrolling redraws at about 15 frames a second. Partial redraws are in the backlog.

**Screenshots** (`ui.test -test.bench Screenshot`): saving a PNG takes 0.21 s at 960×600 and 0.68 s at 1920×1080, off the UI goroutine. The MiSTer Companion remote waits up to 6 s.

**On the TV (2026-09-30, build 83bd059, the user's report):**
- **Screenshots (item 21):** work. Print Screen saved a 960×600 PNG in `/media/fat/screenshots/MiSTer_Subsonic/`. The menu's own screenshot of a Linux app is 1920×1200 noise, because it saves the output buffer, not the framebuffer.
- **Sharpness (item 3):** still blurry next to MiSTerHiFi and MiSTerFin. MiSTerFin draws an even smaller picture (its log says `fb: 640x288 (real 960x600)`), but with pre-rendered bitmap fonts, whose hard edges survive the 2× upscale.
- **Font test card (a throwaway test program, `vmode -r`):**
  - The menu accepts `vmode -r 1920 1200 rgb32`: the framebuffer becomes 1920×1200, and `vmode -r 960 600 rgb32` puts it back. `vmode` exits 1 even when it works.
  - At full resolution the current smooth rendering looks best, better than hinted, higher-contrast or unsmoothed text.
  - Plan 4b therefore draws at the output resolution.
- **Now Playing's small volume indicator:** the user doesn't like it. Plan 4b removes it and keeps the volume panel.
- **Hotkey hints:** the user wants hints for the gamepad and keyboard on every screen (Plan 4b).
- **Media keys and the volume panel (items 19–20):** not reported yet.

## Plan 4b on the MiSTer (2026-09-30)

Measured with the test binaries only (`ui.test -test.bench 'Partial|Repaint'`, `gfx.test -test.bench 'Pack'`); nothing was drawn on the TV.

**A full-frame pass is memory-bound on the Cortex-A9.** A CPU profile of a native 1080p frame had `Canvas.Clear` at 37%, cover `Blit` at 25%, and the framebuffer copy took another 19 ms. `Clear` indexed `c.Pix` inside its loop, which reloads the slice on every store; a local slice made it about 3× faster:

| Full frame (draw only) | Before | After |
|---|---|---|
| 960×600 albums / feed | 13.5 / 14.7 ms | 10.7 / 11.8 ms |
| 1280×720 albums / feed | 22.2 / 22.5 ms | 17.3 / 18.4 ms |
| 1920×1080 albums / feed | 49.4 / 48.1 ms | 37.3 / 37.6 ms |
| 1920×1200 albums | 50.2 ms | 38.0 ms |

**Partial frames at 1920×1200** (draw, then the copy of the damaged rectangles):

| Case | Draw | Copy | Total |
|---|---|---|---|
| Full frame | 38.0 ms | 21.8 ms | ≈ 60 ms |
| Focus move in a cover grid (two cells) | 9.7 ms | 2.2 ms | ≈ 12 ms |
| Focus move in a list (two rows) | 8.6 ms | 2.1 ms | ≈ 11 ms |
| List scroll step (the list area) | 20.6 ms | ≈ 20 ms | ≈ 41 ms |
| Progress tick (bar and times) | 0.7 ms | < 1 ms | ≈ 1 ms |
| Marquee frame (one line) | 1.1 ms | < 1 ms | ≈ 2 ms |

- **Targets** (Plan 4b spec §3.6): focus moves, the tick and the marquee ≤ 10 ms: met for the tick and marquee, and within 2 ms for focus moves. A list scroll step ≤ 40 ms: about 41 ms, where a full frame was ≈ 72 ms before this plan.
- **Scroll-by-shift was measured and dropped.** Moving a 1640×1000 area up one row takes 15.4 ms, filling it 6.2 ms: on this memory bus shifting pixels costs more than redrawing them.
- **The framebuffer copy stays a `copy`.** A 32-bit store loop was 30% slower (11.2 against 8.7 ms in the same test).
- **No size threshold for partial frames.** A partial frame measured never slower than a full one, even at 92% of the screen, so any damaged area is drawn partially.

**Full resolution:** the menu accepts `fb_cmd1 8888 1 1920 1200` (the font test card, see "Plan 4 on the MiSTer"). The app's switch and restore were tested against a fake command pipe, then on the TV (below).

**On the TV (2026-09-30, the user's report):**
- **First run (build 8379160):** the switch to 1920×1200 worked, then the kernel oopsed in fbcon (`sys_imageblit`): the console was still in text mode while the size changed. Fixed in b421a7c: the console enters graphics mode before the switch and stays in it until the size is back. b0c9575 skips the switch if graphics mode can't be entered.
- **Build b0c9575:**
  - **Full resolution (items 3, 22):** works; `log.txt` shows "framebuffer 1920x1200, full resolution (was 960x600)". Text is as sharp as MiSTerHiFi, and exit returns to the menu correctly.
  - **Smooth browsing (item 23):** OK.
  - **Gamepad R on an 8BitDo M30 (X-input, `045e:028e`):** didn't work. Its MiSTer map sends R as an analog axis (`0x305` = `ABS_Z` high), which the map reader skipped. Fixed in d2d8336, which decodes axis entries the way Main_MiSTer does.
  - **Screenshot:** the home feed's next row of covers overflowed onto the mini bar. Fixed in f9b9027: screens are clipped to their own area.
  - **Exit:** the user asked for it in the main menu (6ec2cc5).
- **Build f9b9027:** all of the above works: R on the M30, Exit in the main menu, no overflow, and the hints (item 24).
- **Crash restore (item 22):** after `kill -9` of the app at 1920×1200, the launcher said "stopped with an error". Its `-restore-console` put the framebuffer back to 960×600 and removed `/tmp/mistersubsonic.fb`, with no kernel errors.
- **No stale pixels (item 25):** in a 2½-minute run with `-verify-redraw` at 1920×1200, with music playing, the user browsed grids and lists, opened Now Playing and used the volume keys. `log.txt` has 0 "partial redraw differs" lines.
- **Caution for tools:** reading `/dev/fb0` with `read()` (`head`, `dd`, `cat`) while the app is at full resolution oopses the kernel (`mmiocpy`), because the driver's read path keeps the old window. Use the app's own screenshot (Print Screen) instead.

## Plan 5 on the MiSTer (2026-09-30)

**Cover decoding** (`gfx.test -test.bench DecodeCover`: a 2000×2000 JPEG scaled to 600×600; test binaries only, nothing on the TV):

| | Decode + scale | Scale only |
|---|---|---|
| Before Plan 5 | 1.98 s | 1.16 s |
| Row loops instead of a per-pixel closure | 1.93 s | — |
| No per-pixel division | 1.76 s | 0.99 s |
| Opaque fast path (32-bit sums, no alpha weighting) | **1.38 s** | **0.61 s** |

- The Cortex-A9 has no hardware integer divide, so each per-pixel `/` was a call to the software `runtime.udiv`: 17% of the scaling time in a device profile. The box filter now divides with exact multiply-and-shift reciprocals, and the YCbCr row conversion uses a column table.
- JPEG covers are always opaque, so their averages use plain 32-bit sums. The output is bit-identical to before, and the tests check that exactly.
- What remains is Go's standard JPEG decoder, about 0.77 s for this size.

**On the TV:** pending. Run `docs/testing-on-mister.md` items 26–29 and record them here.

## Plan 6 on the MiSTer

**Visualizer cost.** Desktop reference (Ryzen 9 6900HS, 0 allocs/op), measured as analysis plus drawing into the panel or the full body, without the present:

| Case | Bars | Scope | VU | Waterfall |
|---|---|---|---|---|
| Panel 1920×1200 (1050×153) | 88 µs | 99 µs | 111 µs | 157 µs |
| Panel 320×240 (162×34) | 23 µs | 22 µs | 24 µs | 23 µs |
| Full screen 1920×1200 | 670 µs | 683 µs | 1.13 ms | 1.97 ms |
| Full screen 320×240 | 48 µs | 52 µs | 61 µs | 73 µs |

The analysis alone (`internal/viz`, `BenchmarkAnalyzerUpdate`): 49 µs on HDMI settings, 18 µs on CRT settings. The cost on the A9 will be mostly the present of up to 1920×1200 per frame, which these numbers leave out.

**On the MiSTer:** pending. After `make deploy-dev MISTER=<ip>`, on the device:

```
./ui.test -test.run '^$' -test.bench VizFrame -test.benchtime 30x
./viz.test -test.run '^$' -test.bench AnalyzerUpdate
```

| Case | Bars | Scope | VU | Waterfall |
|---|---|---|---|---|
| Panel 1920×1200 | pending | pending | pending | pending |
| Panel 320×240 | pending | pending | pending | pending |
| Full screen 1920×1200 | pending | pending | pending | pending |
| Full screen 320×240 | pending | pending | pending | pending |

Then run `docs/testing-on-mister.md` items 31–35 and record them here, with the frame-rate defaults that fit 60% of each budget.

## Plan 7 on the MiSTer

**Remote CPU:** pending. Ask the user before playing anything. Play a FLAC album with the visualizer off, and over ssh run:

```
top -b -n 12 -d 5 | grep mistersubsonic
```

Run it once with no phone connected and once with the remote page open on Now Playing (it gets a state event up to 8 times a second while playing). The difference must be under 5% of one core.

| Case | CPU |
|---|---|
| Playing, no phone | pending |
| Playing, phone on Now Playing | pending |
| Playing, phone browsing and searching | pending |

Then run `docs/testing-on-mister.md` items 35–38 and record them here.
