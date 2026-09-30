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
