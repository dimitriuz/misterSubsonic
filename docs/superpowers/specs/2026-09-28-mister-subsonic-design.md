# MiSTer Subsonic — Design Spec

- **Date:** 2026-09-28
- **Status:** Approved in brainstorming, pending written-spec review
- **License:** GPL-3.0

## 1. Intent

**What the user asked for**

- A MiSTer FPGA app that plays music from a Subsonic-API server. The primary target is Navidrome on the LAN.
- The main use is **couch listening**: the MiSTer is on a TV, the user browses the library with a controller, and a proper now-playing screen with album art is shown.
- **HDMI is the primary display, but the app must also work on a CRT** (240p/288p analog output).
- **Lossless**: most of the library is FLAC, and it must be played as FLAC and decoded on the device, not transcoded.
- v1 features: core browsing, playlists and starred items, search, and scrobbling / now-playing reporting.
- Connections over plain `http://` (LAN IP), `https://` with a valid certificate, and `https://` with a self-signed certificate. All must work because this is a **public project**.
- First-run setup through a config file **or** an on-screen wizard.
- Written **from scratch**. MiSTer Hi-Fi and MiSTerFin are references for lessons only; no code or design is copied from them.

**Assumptions**

- Users run a stock MiSTer (DE10-Nano, Cortex-A9, 1 GB RAM, glibc-based Linux) and launch the app from the Scripts menu.
- Target servers are Navidrome first, then other Subsonic/OpenSubsonic servers (gonic, Airsonic-Advanced, and so on).
- UI text is English only in v1. Metadata can be any script.

**Success criteria**

1. A new user edits one file, or completes the wizard, and is browsing their library on the TV within minutes.
2. FLAC plays bit-accurately up to the resampler, gaplessly between album tracks, with no crackle.
3. Seeking works instantly within the buffered range, and within about a second anywhere in a 1 GB+ single-file album rip.
4. The UI stays responsive at all times. Network calls never block rendering or input.
5. The same build is usable on HDMI 720p/1080p and on a 240p CRT.
6. Plays show up in Navidrome (play counts, "now playing", and Last.fm/ListenBrainz forwarding when the server has it configured).

## 2. Non-goals for v1

- EQ, lyrics, internet radio, local or SMB files, CD playback. (The visualizer was a non-goal for v1; Plan 6 added it, see `2026-10-01-mister-subsonic-visualizer-design.md`.)
- Chapter navigation inside single-file album FLACs using the embedded CUESHEET. The file plays as one track with full seek; chapters are the first item for v2.
- Playing music in the background while another MiSTer core runs. Exiting the app stops playback.
- A web remote or phone control.
- An in-app self-updater. Updates come through MiSTer Downloader or update_all instead (§9).
- Playlist editing beyond starring (no create, rename or reorder).
- Video of any kind.

## 3. Architecture overview

The app is a single Go binary. The only native code is a thin C audio shim compiled through cgo: miniaudio for decoding and ALSA output, plus the speexdsp resampler (BSD). Everything else is pure Go.

```
cmd/mistersubsonic/   main: flags, wiring, lifecycle, signal handling
internal/subsonic     Subsonic/OpenSubsonic API client (net/http + crypto/tls)
internal/stream       seekable windowed HTTP-Range reader (io.ReadSeeker)
internal/audio        cgo shim: decoder, resampler, ALSA device + PCM ring
internal/player       queue, playback state machine, gapless, scrobble timing, resume
internal/ui           screens, widgets, navigation stack, layout profiles
internal/gfx          canvas, text (pure-Go OpenType), image scaling
                        backends: fbdev (/dev/fb0) | headless PNG | dev web viewer
internal/input        evdev reader, MiSTer .map loader, hotplug, key repeat
internal/config       TOML config load/save, validation, defaults
internal/cache        on-disk LRU for cover art (and the scrobble retry queue)
internal/platform     MiSTer specifics: console KD_GRAPHICS, BGM/SAM pause, lock
```

**Data flow for playing a track**

```
ui ──action──▶ player ──track URL──▶ stream (Range reader, sliding window)
                  │                         │ Read/Seek
                  │                         ▼
                  │            audio: decode loop (Go goroutine)
                  │              ├─ C decoder (dr_flac/dr_mp3/dr_wav via miniaudio)
                  │              ├─ ReplayGain + volume (float)
                  │              ├─ speexdsp resample → 48 kHz f32 stereo
                  │              └─ push into lock-free PCM ring (C)
                  │                         ▼
                  │            ALSA device callback (C thread) drains ring
                  └──events◀── position / track-boundary / underrun / error
```

**Rules for dependencies between modules**

- `ui` depends on `player`, `subsonic`, `gfx`, `input` and `cache`.
- `player` depends on `stream`, `audio` (through an interface) and `subsonic`.
- Nothing depends on `ui`.
- `audio` and `stream` know nothing about Subsonic.

## 4. Subsonic client (`internal/subsonic`)

**Protocol**
- Subsonic REST API 1.16.1 with `f=json`, `c=MiSTerSubsonic` and `v=1.16.1`.
- Every call goes through one `do(ctx, endpoint, params) (*Response, error)`. It decodes the `subsonic-response` envelope and maps `status:"failed"` to a typed `*APIError{Code, Message}`.
- `ping` runs at connect time. If the response has `openSubsonic: true`, the client calls `getOpenSubsonicExtensions` and records which extensions are present.

**Auth**, with the method chosen at setup and stored per server:
1. **API key** (the OpenSubsonic `apiKeyAuthentication` extension), used when the server advertises it and the user supplied a key.
2. **Token auth**: `t = md5(password + salt)`, `s = salt`.
   - The wizard computes one random salt and token and stores that pair (`token`, `salt`), so the plaintext password is never written to disk.
   - A user who edits the file by hand can put in `password` instead. The client then derives a fresh salt per session.
3. **Plaintext `p=enc:<hex>`**: the fallback when the server returns error 41 ("token auth not supported", as with LDAP or reverse-proxy-authenticated users).
   - Allowed only over `https://`, unless `allow_plaintext_password = true` is set explicitly.
   - Error 41 over http without that flag is shown as an actionable error message.

**TLS**
- The trust store is the system roots **plus** Mozilla roots embedded in the binary (`golang.org/x/crypto/x509roots/fallback`), because the MiSTer's CA store is incomplete or outdated.
- Per server, `ca_file` (PEM) adds a trusted CA or a self-signed certificate. This is the recommended route for self-signed setups.
- Per server, `insecure_skip_verify = true` is a last resort. While it is active the UI shows a persistent "⚠ insecure" badge.
- Plain `http://` URLs work unchanged.

**Endpoints used**

| Purpose | Endpoint |
|---|---|
| Connect/detect | `ping`, `getOpenSubsonicExtensions` |
| Browse | `getArtists`, `getArtist`, `getAlbum`, `getGenres`, `getAlbumList2` (newest, recent, frequent, random, alphabeticalByName, byYear, byGenre) |
| Playlists | `getPlaylists`, `getPlaylist` |
| Starred | `getStarred2`, `star`, `unstar` |
| Search | `search3` (artists, albums, songs, each paged) |
| Playback | `stream` (raw, or `format=`/`maxBitRate=`/`timeOffset=`), `getCoverArt` (`size=`) |
| Reporting | `scrobble` (`submission=false` for now playing, `true` for the actual scrobble) |
| Resume | `getPlayQueue`, `savePlayQueue` |

**Timeouts and cancellation**
- Every call takes a `context.Context`. The UI cancels in-flight requests when the user navigates away.
- The connect timeout is 5 s. The metadata request timeout is 15 s.
- Stream requests have no total timeout. They use a stall detector instead (§5).

**Paging**
- Lists (album lists, search) are fetched in pages of 100 as the user scrolls.
- `getArtists` is fetched once and cached in memory per session.

## 5. Stream layer (`internal/stream`)

**Problem:** tracks range from 3 MB MP3s to single-file album FLACs over 1 GB. A whole track cannot be held in RAM, but the decoder needs a seekable source.

**Design:** `stream.Open(ctx, url, opts) (*Reader, error)` returns an `io.ReadSeeker` with these properties:

- **Sliding window.** Each reader keeps a ring of at most `buffer_mb` (default 32 MB). The region behind the read position is capped at 25% of the window, which keeps small back-seeks instant.
- **Read-ahead.** One long-lived `GET` with `Range: bytes=<off>-` fills the window ahead of the decoder. When the window is full, the fetch pauses through backpressure. The connection is not closed.
- **Seek inside the window** is served from memory.
- **Seek outside the window** discards the buffer, cancels the current GET and opens a new Range GET at the target offset.
- **Range capability detection.**
  - The first response is checked for `206` or `Accept-Ranges: bytes`, and `Content-Length` is recorded.
  - If the server returns `200` without range support, the reader is **sequential-only**. A seek forward reads and discards data up to the target; a seek backward reopens the stream from byte 0. The player shows "seek unavailable" rather than blocking.
- **Transcoded streams** (§6) are never byte-seeked. The player seeks them by reopening the URL with `timeOffset=<seconds>`.
- **Reconnect.** A stall of more than 10 s with no bytes, or a mid-body connection error, reopens the stream at `bytes=<last received offset>-`. Retries back off at 0.5, 1, 2, 4 and 8 s, capped at 30 s total, before the reader returns an error.
- **Prefetch for gapless.** The player opens the next track's reader in prefetch mode about 20 s before the current track ends. Prefetch mode fills up to 4 MB, then waits until the reader is promoted to active.
- **Memory ceiling:** 1 active reader plus 1 prefetch reader, about 36 MB in total at the defaults.

**Decoder byte source.** The audio shim's read and seek callbacks call into `Reader.Read` / `Reader.Seek` through Go-exported functions. The dr_flac seek path uses the SEEKTABLE when present and a binary search otherwise; both go through these callbacks, so seeking works across any file size.

## 6. Audio (`internal/audio`) and formats

**The C shim is kept thin. All control flow lives in Go.**

- `decoder_open(read_cb, seek_cb, user)` → a handle exposing `rate`, `channels`, `bit_depth` and `length_frames`. It wraps `ma_decoder` with a custom VFS callback, and the decoding backends are dr_flac, dr_mp3 and dr_wav.
- `decoder_read_f32(h, buf, frames)`, `decoder_seek_frame(h, frame)`, `decoder_close(h)`.
- `resampler_*`: the speexdsp resampler, going from the source rate to 48 kHz. Quality level 5 is the starting point and is tuned by spike #3.
- `device_open(alsa_name)`: miniaudio ALSA device fixed at **48 kHz, f32, stereo**, with a lock-free PCM ring (`ma_pcm_rb`) of about 500 ms. The C callback only drains the ring and never blocks. Mono sources are upmixed and surround sources are downmixed.
- `device_ring_write`, `device_ring_available`, `device_frames_played`, `device_pause`, `device_resume` and `device_flush`.

**Go decode loop**, one goroutine per active track:

1. Read a chunk from the decoder.
2. Apply ReplayGain and volume as one float multiply. When the gain is above unity, apply a soft clip: samples up to ±0.9 pass unchanged and larger ones bend smoothly toward ±1.0 (a continuous, monotonic knee; unity and attenuating gains stay bit-exact). The resampler can still overshoot slightly afterwards.
3. Resample.
4. Write into the ring, blocking on space.

At the end of a track, if the next track's decoder is already open (prefetched), the loop keeps writing its samples into the same ring with no flush. That is gapless playback.

The loop records the **frame index where each track starts in the ring's output**. The player compares it to `device_frames_played` to switch the UI to the new track at the exact audible moment.

**Formats**

| Source suffix / content type | Handling |
|---|---|
| FLAC (any bit depth or rate), MP3, WAV | Direct: `stream?id=..&format=raw` |
| Anything else (ALAC/M4A, AAC, Opus, Vorbis, WMA, DSD, …) | Server transcode: `stream?id=..&format=<transcode_format>&maxBitRate=<transcode_bitrate>` |

- The choice is made from the song's `suffix` and `contentType` in the metadata before the stream is opened.
- `transcode_format` defaults to `mp3` at 320 kbps. It must be one the device can decode: `mp3`, `flac` or `wav`. The user can set `flac` if their server has a FLAC transcoder configured, which gives lossless ALAC.

**ReplayGain** is a setting with three values: `off`, `track` or `album`.
- The gain comes from the OpenSubsonic `replayGain` object when it is present. Otherwise no gain is applied; the app does not parse tags itself in v1.
- A preamp of 0 dB is applied, with peak-based clipping protection when a peak value is available.

**Volume** is software volume in 1 dB steps with a mute toggle, and is persisted in the config.

## 7. Player (`internal/player`)

**State and actions**
- **State:** `queue []Song`, `index`, `status` (stopped, loading, playing, paused, buffering, error), `position`, `shuffle`, and `repeat` (off, all, one).
- **Queue actions:**
  - `PlayNow(songs, startIndex)` replaces the queue.
  - `PlayNext(songs)` inserts after the current track.
  - `Enqueue(songs)` appends.
  - `Remove(i)`, `Clear()` and `Jump(i)` edit and navigate the queue.
- **Shuffle** produces a permuted play order while keeping the original queue for display and un-shuffle. Gapless transitions still apply between consecutive tracks in the shuffled order.
- **Repeat:** repeat-one loops the current track (reopened, not gapless). Repeat-all wraps at the end of the queue.

**Controls**
- **Seek:** within the window or by Range for raw streams, and by `timeOffset` reopen for transcoded ones.
- **Previous:** goes to the previous track if the position is under 3 s, otherwise restarts the current one.

**Events** are delivered to the UI on a channel: `TrackChanged`, `PositionTick` (4 Hz), `StatusChanged`, `Error{song, err}` and `QueueChanged`.

**Scrobbling**
- At track start the player sends `scrobble(id, submission=false)`.
- When accumulated *listened* time reaches `min(duration/2, 240 s)`, it sends `scrobble(id, submission=true, time=<start epoch ms>)` once. Seeking does not count as listening.
- Scrobbling can be turned off with `scrobble = false`.
- A failed submission is appended to a persistent retry queue (`cache/scrobbles.json`, capped at 500 entries). The queue is flushed on the next success and at startup.

**Resume**
- On exit, and every 30 s while playing, the player calls `savePlayQueue(ids, current, position)` and also writes the same data locally to `state.json`.
- At startup, `getPlayQueue` is used when available, with `state.json` as the fallback.
- If a saved queue exists, Home shows a "Resume: <title> — <artist>" item.

**Errors**
- **Decode or open failure on a track:** show a toast, then skip to the next track. If 3 tracks fail in a row, stop and show an error.
- **Ring underrun:** set status to `buffering` and show an indicator.

## 8. UI (`internal/ui`, `internal/gfx`, `internal/input`)

### 8.1 Rendering

- **Framebuffer.** `gfx` opens `/dev/fb0`, reads the geometry with the `FBIOGET_*SCREENINFO` ioctls and mmaps it (16 or 32 bpp). If the fbdev mmap fails, it falls back to mapping `/dev/mem` at `smem_start`.
- **Double buffering.** All drawing goes to a back buffer. The UI redraws on events, not continuously.
- **Partial redraws** (Plan 4b, `docs/superpowers/specs/2026-09-30-mister-subsonic-plan-4b-design.md`). A change that knows its area damages it; the frame then runs the normal drawing clipped to the damage and copies only those rectangles to the framebuffer. Focus moves in lists and grids, the position tick, the marquee, arriving covers, toasts and the volume panel are partial; anything else redraws the whole frame. A verify mode (every UI test, and `-verify-redraw` on the device) checks each partial frame against a full one.
- **Full resolution** (Plan 4b). When the menu halved an HDMI framebuffer (the output in `MiSTer.ini` is exactly twice its size, and no bigger than 1920×1200), the app asks for the full size on `/dev/MiSTer_cmd` at start and restores the old size on exit; `-restore-console` restores it after a crash. `display.full_resolution = false` turns it off.
- **Canvas per profile:**
  - **HDMI** draws at the framebuffer's own size and fills it, with no scaler. The 1280×720 layout is the design size: every length is scaled by `min(W/1280, H/720)`, and fonts stay at 15/12/10 px or more (title, body, small). A 16:10 screen gets more rows instead of bars. The MiSTer menu halves framebuffers above 1920×1080 (1920×1200 gives 960×600); the app switches those back to full size (see Full resolution above).
  - **CRT** keeps its logical size and is scaled to the framebuffer with nearest neighbour (integer scale where possible) and letterboxed.

| Profile | Canvas | Picked when |
|---|---|---|
| HDMI | the framebuffer's size (layout designed at 1280×720) | `auto` and fb height > 288, or `display.profile = "hdmi"` |
| CRT | 320×240 or 320×288, with pixel aspect correction; 480/576-line framebuffers are line-doubled | `auto` and fb height ≤ 288, or `display.profile = "crt"` |

**Repaint target:** on the Cortex-A9, a full repaint takes under 30 ms at 960×600 and on CRT. At 1920×1080 and 1920×1200 a full frame takes about 60 ms, so those rely on partial redraws: a focus move, the tick and the marquee cost ≤ 12 ms, and a list scroll step about 40 ms (Plan 4b's measurements in `docs/spikes.md`).

`display.profile` accepts `auto`, `hdmi` or `crt`. A 480i/576i CRT can't be told apart from a 480p/576p HDMI framebuffer, so `auto` picks HDMI; CRT users on interlaced modes set `crt` explicitly, and the README says so.

- **Overwrite watchdog.** Every 2 s the app samples a few regions against the back buffer and does a full repaint if Main_MiSTer or the tty has drawn over the screen.

**Text**
- Rendered in pure Go with `golang.org/x/image/font/opentype`, through a glyph cache keyed by (font, size, rune).
- Bundled font: Noto Sans (Latin, Latin-Ext, Cyrillic, Greek), in Regular and Bold, under the OFL license.
- Fallback fonts: any `.ttf` or `.otf` in `/media/fat/mistersubsonic/fonts/` is used for runes the bundled font lacks, for example CJK.
- Long titles are truncated with an ellipsis. The focused row marquee-scrolls.

**Images**
- Cover art is decoded with the stdlib `image/jpeg` and `image/png`, downscaled once to the size needed, and kept in an in-memory LRU of about 48 MB decoded.
- The server resizes art: requests use `getCoverArt?size=<px>` with the exact size each profile draws.
- Fetched art is cached on disk (`internal/cache`, LRU, `cache.cover_art_mb`, default 200 MB).

### 8.2 Screens

```
Home ─┬─ Home feed: [Resume] · Recently Added · Recently Played · Most Played · Random
      ├─ Artists (A–Z, L/R jumps by letter) → Artist → Album → Tracks
      ├─ Albums (A–Z / by year / by genre) → Album → Tracks
      ├─ Playlists → Playlist → Tracks
      ├─ Starred (Albums / Artists / Tracks tabs)
      ├─ Search (on-screen keyboard + live results: Artists / Albums / Tracks)
      └─ Settings (Servers · Playback · Display · About)
Now Playing ⇄ Queue        (reachable from anywhere)
Wizard: Server URL → Username → Password → (API key, optional) → Test → Home
```

**Layout and screen contents**
- **HDMI:** a left sidebar for sections and the content area on the right. The home feed uses horizontal rows of album covers. Album and artist lists are cover grids.
- **CRT:** a single column with no sidebar. Sections are a top-level list, lists carry small thumbnails, and fonts are larger. All content stays within title-safe margins (about 5%).
- **Mini bar:** a now-playing bar at the bottom of every browse screen shows the art thumbnail, title, artist and a progress line.
- **Album screen:** header with art, title, artist, year and a "FLAC 24/96"-style format summary. Buttons: Play · Shuffle · Star. Below that, the track list with durations.
- **Now Playing:**
  - Large art with title, artist and album.
  - Progress bar with elapsed and total time.
  - Quality badge, for example `FLAC 16/44.1` or `MP3 320 · transcoded`.
  - Star state and a "Next: …" line.
  - Shuffle, repeat and volume indicators, and the insecure-TLS badge when it applies.
- **Queue:** a list with the current track highlighted. A selects a track to jump to it; X opens a menu with Remove and Clear queue.
- **Search:**
  - The on-screen keyboard is a grid of A–Z, 0–9, space, backspace and a Cyrillic layout toggle. A physical USB keyboard also works.
  - Results update 300 ms after the last keystroke, through `search3` with the previous request cancelled.
- **Wizard:** uses the same on-screen keyboard.
  - The URL field offers `http://` and `https://` presets and a `:4533` shortcut.
  - **Test** runs `ping` using the auth order in §4 and shows a specific error: unreachable, TLS or certificate error (with a hint to set `ca_file` or the insecure option), wrong credentials, or token auth not supported.
  - On success it writes `config.toml`.
- **Server unreachable at launch:** a screen with Retry · Switch server · Settings.
- **Toasts** show non-blocking errors such as a skipped track or a scrobble queued.
- **Screensaver:** after `display.screensaver_minutes` (default 5) idle on Now Playing, the screen dims and the art drifts slowly to protect against burn-in. Any input wakes it.

**Async rule:** every screen loads through a goroutine and shows a spinner, and results come back as messages to the UI loop. The UI loop never does I/O.

### 8.3 Controls

| Button | Browsing | Now Playing |
|---|---|---|
| D-pad | move focus | ←/→ seek ±10 s (hold: ±30 s) |
| A | open / play | play/pause |
| B | back | back to previous screen |
| X | context menu: Play now · Play next · Add to queue · Star/Unstar · Go to artist · Go to album | press: menu (Star/Unstar · Shuffle · Repeat), closed after a choice; hold 1 s: star/unstar |
| Y | open Now Playing | open Queue |
| L / R | page up/down (letter jump on Artists) | previous / next track |
| Start | play/pause | full-screen visualizer when something is playing, else play/pause |
| Select | shuffle-play current list | press: next visualizer style (Off → Bars → Scope → VU → Waterfall; in full screen without Off); hold 1 s: mute |

Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, Space = play/pause, PgUp/PgDn = L/R, `n` = Now Playing, `q` = Queue.

**Shuffle and repeat** are independent and can be on together; they live in the X menu on Now Playing and in the queue's X menu. **Visualizer:** see the Plan 6 spec (`2026-10-01-mister-subsonic-visualizer-design.md`).

**Mute** is M on a keyboard, the media Mute key, or holding Select on Now Playing for one second (the release after that hold does nothing). Settings → Playback has no Volume or Mute rows; volume is Up/Down on Now Playing and the media keys. **Settings help:** the focused setting's help text (one or two dim lines) shows under a settings list, and Transcode bitrate is listed only while Transcode to is mp3.

**Media keys** (volume up and down, mute, play/pause, next, previous, fast-forward and rewind) work on every screen. The top screen gets each key first, so a text field or a screen with its own meaning for it keeps it; the X menu, the exit prompt and the screensaver let them through (a media key wakes the screensaver and acts on the first press). Next and previous need a queue. Seeking needs a current track, moves ±10 s (held: ±30 s, at most four seeks a second) and stops a second before the end.

**Screenshot key:** Print Screen or Scroll Lock (MiSTer's Alt+Scroll Lock, which the MiSTer Companion remote sends) saves the frame on screen as `YYYYMMDD_HHMMSS.png` to `/media/fat/screenshots/MiSTer_Subsonic`. It is handled before everything else, the screensaver included, and does not count as activity.

**Hint bar:** a line along the bottom of every screen shows the buttons that matter there (the screen's own list, plus Back and Now Playing where the app handles B and Y), drawn as gamepad buttons or keyboard keys after the last press of either kind. Every hinted button does something on its screen. Exceptions: the X menu shows only Choose and Close; Now Playing isn't hinted while typing on a keyboard (N types there); Select has no key, so a keyboard shows no Select hint; the screensaver hides the bar; a screen that failed to load hints Retry (A), and one that is loading or empty hints nothing of its own. Settings → Display → Hints turns it off (`display.hints`).

Quitting the app is Exit in the main menu (the sidebar's last entry on HDMI, the home list's last item on CRT), or holding B for 2 s on the Home root, with a confirmation.

### 8.4 Input (`internal/input`)

- **Devices:**
  - Enumerates `/dev/input/event*`, keeping devices that report keys or absolute axes.
  - Grabs each one exclusively with `EVIOCGRAB` so Main_MiSTer does not react, and releases the grabs on exit.
  - Rescans every 2 s for hotplugged devices.
  - Ignores the "MiSTer virtual input" echo device.
- **Mapping:**
  - Gamepads are mapped through the user's MiSTer map `/media/fat/config/inputs/input_<VID>_<PID>_v3.map` when present.
  - Otherwise it uses Linux gamepad defaults: `BTN_SOUTH`=B, `BTN_EAST`=A, `BTN_NORTH`=X, `BTN_WEST`=Y, `BTN_TL`/`BTN_TR`, `BTN_START`/`BTN_SELECT`, and hat or stick axes with a 50% threshold.
- **Key repeat** for navigation: 350 ms delay, then 110 ms, accelerating to 45 ms after 6 repeats. It is based on the real held state, not on the kernel's repeat events. Volume and seek keys repeat the same way when held; the other media keys don't.
- **Drain:** queued input is drained at startup and after returning from the wizard.

## 9. MiSTer platform, config, distribution

**Install layout**
```
/media/fat/Scripts/MiSTer_Subsonic.sh        launcher (appears in the Scripts menu)
/media/fat/mistersubsonic/mistersubsonic     binary
/media/fat/mistersubsonic/config.toml        created by wizard or by hand (example shipped as config.example.toml)
/media/fat/mistersubsonic/fonts/             optional fallback fonts
/media/fat/mistersubsonic/cache/             cover art LRU, scrobble retry queue
/media/fat/mistersubsonic/state.json         local resume state
/media/fat/mistersubsonic/log.txt            rotating log (1 MB × 2)
```

**Launcher** (`MiSTer_Subsonic.sh`):
- Takes a single-instance lock (`/tmp/mistersubsonic.lock`).
- Pauses the BGM script (through its socket) and SAM if they are running, and hides the cursor.
- Runs the binary, then restores BGM, SAM and the cursor on exit.

The binary itself sets the console to `KD_GRAPHICS` and restores `KD_TEXT`, input grabs and the framebuffer on normal exit, on SIGINT/SIGTERM and after a recovered panic.

**Config** (`config.toml`):
```toml
default_server = "home"

[[server]]
name = "home"
url = "https://music.example.com"   # or http://192.168.1.10:4533
username = "alice"
token = "…"                          # written by the wizard (md5(password+salt))
salt = "…"
# password = "…"                     # alternative to token/salt when editing by hand
# api_key = "…"                      # OpenSubsonic API key, if the server supports it
# ca_file = "/media/fat/mistersubsonic/myca.pem"
# insecure_skip_verify = false
# allow_plaintext_password = false

[playback]
transcode_format = "mp3"   # used only for formats the device can't decode
transcode_bitrate = 320
replaygain = "off"         # off | track | album
volume_db = 0
scrobble = true
buffer_mb = 32
alsa_device = "default"

[display]
profile = "auto"           # auto | hdmi | crt
screensaver_minutes = 5

[cache]
cover_art_mb = 200
```

**Config loading**
- Unknown keys produce a log warning.
- If the file is invalid, the app shows an error screen with the parse error and an offer to run the wizard. The bad file is never overwritten silently; it is renamed `config.toml.invalid-<ts>` only when the wizard saves a replacement.
- Multiple `[[server]]` entries are allowed, and Settings → Servers switches between them.

**Recommended `MiSTer.ini`** (documented in the README):
- HDMI: `fb_size`/`video_mode` values for 720p/1080p output.
- CRT: a `[Menu]` `video_mode` for 240p/288p and the related `composite_sync`/`ypbpr` notes.
- The app never changes the video mode itself.

**Build**
- Cross-compile with `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"` (zig 0.16.0), using `-trimpath -ldflags "-s -w"`. `scripts/check-glibc.sh` fails the build if the binary needs glibc newer than 2.31; the prototype needed 2.29.
- The binary is dynamically linked only against the MiSTer's glibc. miniaudio loads libasound with `dlopen` at runtime.
- The C sources are vendored at pinned versions and SHA-checked: miniaudio, and speexdsp `resample.c` and its headers.
- **Desktop build:** the same code for linux/amd64 and darwin, with the miniaudio default backend (PipeWire, Pulse or CoreAudio). A build tag selects the `gfx` backend.
- A `Makefile` provides the targets `build`, `mister`, `test`, `deploy MISTER=host` (scp to the device) and `release`.

**Dev loop**
- `mistersubsonic -dev-viewer :8090` on the desktop serves a page at `localhost:8090`. The page streams the framebuffer (PNG frames on UI change) and forwards keyboard events.
- This gives the full app against a real server with real audio, without the MiSTer.
- `-headless -frame-out dir/` renders frames to PNG files for tests.

**CI/CD (GitHub Actions)**
- Every push runs `go vet`, the tests and both builds.
- A `v*` tag builds the release zip (`Scripts/MiSTer_Subsonic.sh` + `mistersubsonic/`) and publishes the GitHub Release.
- The same tag regenerates a MiSTer Downloader database (`db.json` with file hashes and sizes), so users can add the repo to `downloader.ini` or update_all and get updates.

## 10. Testing

| Area | Approach |
|---|---|
| `subsonic` | An `httptest` fake server replaying recorded Navidrome JSON fixtures. Covers auth fallbacks (API key, token, error 41 then enc), API error mapping, paging, context cancellation, and a TLS matrix: http, https with a trusted test CA, https self-signed with `ca_file`, self-signed rejected without it, and `insecure_skip_verify`. |
| `stream` | An `httptest` Range server backed by a synthetic virtual file (content computed from the offset, reported size ≥ 2 GB, never stored). Covers sequential reads, seeks inside, behind and outside the window, a `200`-only server, mid-body disconnect with resume, stall detection, and prefetch caps. |
| `audio` | Decodes fixture FLAC (16/44.1, 24/96), MP3 and WAV to PCM and compares hashes. Also checks resampler output rate, frame count and a sine-wave THD bound, and that the gapless boundary is sample-accurate (concatenated split fixture versus the original). Uses the miniaudio null device, so no audio hardware is needed. |
| `player` | A fake audio engine and fake clock. Covers queue operations, shuffle and un-shuffle, the repeat modes, prev-track logic, scrobble thresholds (including that seeks don't count), the retry queue, resume save and load, and skip after 3 consecutive failures. |
| `ui` | Golden-image tests: each screen in the HDMI and CRT profiles, rendered headless with fake data and compared to PNGs in `testdata/golden/`, with an update flag to regenerate them. Navigation tests drive scripted inputs and assert the screen stack. |
| `input` | A `.map` file parser against fixture maps, the key-repeat state machine with a fake clock, and axis-to-direction mapping. |
| `config` | Round trip, defaults, invalid-file handling, and the multi-server setup. |
| On-device | A manual checklist in `docs/testing-on-mister.md`: HDMI 1080p, CRT 240p, a hotplugged pad, a 1 GB album FLAC seek, a gapless album, wifi drop mid-track, and exit/restore of the Scripts menu. |
| Sound safety | Automated tests never open a real sound device; they use fakes or miniaudio's null backend. Listening checks are opt-in, quiet (about −30 dBFS), and run only after the user confirms each time. |

## 11. Early spikes

These are throwaway probes. The results are recorded in `docs/spikes.md`.

1. **Range on Navidrome.** Does `stream?format=raw` for a FLAC return `206` for `Range:` requests on the user's server, and what do `Content-Length` and `Accept-Ranges` look like? Also check how a transcoded stream responds to Range and to `timeOffset`.
2. **Toolchain + audio on device.** Build a minimal zig-cgo miniaudio binary, confirm it runs on the MiSTer's glibc, and play a 48 kHz test tone plus a FLAC through ALSA with no crackle. Also confirm the ALSA device name to use.
3. **Performance on the Cortex-A9.** Measure the CPU cost of decoding FLAC 24/96, resampling to 48 kHz with speexdsp (compare quality levels 3, 5 and 7), rendering a full 1280×720 list screen with the pure-Go glyph cache, and presenting it. Target: decode + resample < 25% of one core, full UI repaint < 30 ms (§8.1: 1080p is the exception).

If a spike fails, the design section it tests gets revised before implementation: §5 for #1, §6/§9 for #2, and §6/§8.1 for #3.

## 12. Risks and open points

- **Framebuffer size on HDMI** depends on the user's `MiSTer.ini`. With small `fb_size` values the layout scales down; the README must make the recommended settings clear.
- **Exclusive input grab** keeps Main_MiSTer from reacting to presses, but a crash that skips cleanup could leave the grabs in place. The launcher kills any leftover process on start, and the kernel releases grabs when the process exits.
- **Pure-Go text performance** is the main CPU risk, and spike #3 measures it. The fallback is pre-rasterized glyph atlases per profile font size.
- **Servers without OpenSubsonic** will have no ReplayGain data. That is acceptable, since the setting then does nothing.

## 13. Future (post-v1 candidates)

- CUESHEET chapters in single-file album FLACs.
- Internet radio stations from Navidrome's `getInternetRadioStations`.
- EQ. (The visualizer is done, Plan 6.)
- Lyrics (`getLyricsBySongId`).
- A phone web remote.
- An in-app updater.
- Playlist editing.
