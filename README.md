# MiSTer Subsonic

A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.

Status: **playback core plus the library interface**: a home feed with resume, artists (A–Z),
albums (A–Z, by year, by genre), playlists, starred items and search with an on-screen keyboard,
album pages, Now Playing and the queue, on HDMI (sidebar and cover grids) or CRT (lists)
layouts, driven by a controller or keyboard; a setup wizard, Settings (servers, playback,
display) and a screensaver. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
scrobbling and resume work, and it installs on a MiSTer with a Scripts-menu launcher. Still to
come: tuning on the device. On-device (MiSTer) checks are still pending; see `docs/spikes.md` and
`docs/testing-on-mister.md`.

## Install

With MiSTer Downloader or update_all, add this to `/media/fat/downloader.ini` and run an update:

```ini
[mistersubsonic]
db_url = https://raw.githubusercontent.com/dimitriuz/misterSubsonic/db/db.json
```

Or unzip a release's `MiSTer_Subsonic-<version>.zip` onto the SD card: it holds
`Scripts/MiSTer_Subsonic.sh` and the `mistersubsonic/` folder.

Start it from the Scripts menu: **MiSTer_Subsonic**. While it runs, background music (BGM) is
stopped and Super Attract Mode (SAM) is disabled; both come back when you exit (Exit in the main menu,
or hold B on the home screen). Updates never touch your settings.

Everything the app keeps is in `/media/fat/mistersubsonic/`:
- `config.toml` holds your settings. `config.example.toml` explains every option. When a setting
  is changed in the app, only the values that changed are rewritten; your comments, blank lines
  and key order stay.
- `servers/<name>-<id>/` holds each server's resume state, scrobble queue and cover cache (`<id>` is
  8 hex digits from a hash of the server name). An older `servers/<name>/` folder stays with the
  first server that claims it, recorded in its `.server` file.
- `log.txt` and `log.txt.1` hold at most 1 MB each. `crash.txt` records crashes.
- `fonts/` is optional: `.ttf` or `.otf` fonts put there are used for characters the built-in
  font lacks, such as CJK.

## Setup

On first start the app runs a setup wizard: server address (`http://` / `https://` and `:4533`
are one key away), username, password and, for servers that hand them out, an API key. It
tests the login and saves `config.toml` next to the app. With token login (most servers) only a
token and salt are saved, never the password. For servers that need the plain password (LDAP),
you're asked before it is sent over http, and it is stored only if you allow it; over https it is
stored as the server requires. A self-signed certificate is skipped only after you allow that (the
safer way is `ca_file`, below). A config file the app can't read is kept as
`config.toml.invalid-<date>` when the wizard replaces it.

Settings (the last section) switches between servers, adds and removes them, and changes the
volume, ReplayGain, scrobbling, transcoding, the layout and the screensaver (dark screen with a
drifting cover after some idle minutes on Now Playing; off with 0). Each server keeps its resume
state, scrobble queue and cover cache in `servers/<name>-<id>/` next to the config, so `cover_art_mb`
applies per server (each server's cache gets that much).

## Video settings (MiSTer.ini)

The app draws on the MiSTer's Linux framebuffer. That framebuffer is sized from `MiSTer.ini`; the
app never changes the video mode or `MiSTer.ini` itself. Keep `fb_size=0` (automatic) and
`fb_terminal=1` (both the defaults).

- **HDMI:** the interface is drawn at the framebuffer's own size, pixel for pixel, so text and
  covers stay sharp at any resolution.
  - `video_mode=0` (1280x720@60) or `7` (1280x720@50) gives a 1280x720 framebuffer.
  - `video_mode=8` (1920x1080@60) gives 1920x1080.
  - Modes above 1920x1080 get a framebuffer of half the size from MiSTer's menu, whatever
    `fb_size` says. For a custom mode up to 1920x1200 (`video_mode=1920,1200,60`, say) the app
    asks the menu for the full size when it starts, and puts the half size back when it exits
    (after a crash too, through the launcher). Nothing is written to the SD card. Set
    `full_resolution = false` under `[display]` in `config.toml` to keep the half size. If the
    display is unplugged and replugged (or the TV power-cycled) while the app runs, the MiSTer
    menu takes the screen back and cannot be asked for the full size again: the app quits within
    about 2 seconds (the music stops) and you start it again.
  - Redraws only touch what changed: a focus move costs about 11-12 ms at 1920x1200, and the
    progress bar and scrolling titles less. Scrolling a long list redraws the list, about 40 ms a
    step at 1920x1200 (about 25 steps a second).
- **CRT (15 kHz):** add a `[Menu]` section, for example the 240p mode SAM uses for its CRT
  video:

  ```ini
  [Menu]
  video_mode=640,16,64,80,240,1,3,14,12380
  vga_scaler=1
  composite_sync=1
  ```

  This gives a 640x240 framebuffer, which the CRT layout fills. It also changes how the MiSTer
  menu itself is shown. A 288-line mode (for 50 Hz TVs) works the same way.
- **Interlaced CRT modes:** a 480i/576i mode looks like HDMI 480p/576p to the app, so set
  `profile = "crt"` under `[display]` in `config.toml`.

## Controls

| Button | Browsing | Now Playing |
|---|---|---|
| D-pad | move (Left into the HDMI sidebar) | ←/→ seek ±10 s (held: ±30 s), ↑/↓ volume |
| A | open / play | play/pause |
| B | back (hold 2 s on the root to exit) | back |
| X | menu: play now/next, add to queue, star, go to artist/album | star/unstar |
| Y | Now Playing | queue |
| L / R | page (letter jump on Artists) | previous / next track |
| Start | play/pause | play/pause |
| Select | shuffle-play the list | press: shuffle / repeat modes; hold 1 s: mute |

Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = mute,
PgUp/PgDn = L/R, Space = Start. In Search and the setup wizard, letters type and Backspace deletes. With a controller, hold Select on
Now Playing to mute; changing the volume turns the sound back on. A multimedia keyboard's media
keys work on every screen: volume up/down (held to repeat), mute, play/pause, next, previous, and
fast-forward/rewind to seek. A volume panel shows the level for a moment whenever it changes.

**Hints:** the bar along the bottom of every screen shows what the buttons do there: as gamepad
buttons after a gamepad press, as keys after a key press. Settings → Display → Hints turns it off.

**Screenshots:** Print Screen or Scroll Lock (alone; Alt+Scroll Lock is MiSTer's own key for it
too) saves the screen as a PNG. On the MiSTer the folder is `/media/fat/screenshots/MiSTer_Subsonic/`;
elsewhere it is `screenshots/` next to the config, and `-screenshots` changes it. A press while
the last one is still saving says so. The MiSTer Companion remote's Capture screenshot button works too.

## Developing

Requirements: Go 1.26+, a C compiler, and for MiSTer builds [zig 0.16.0](https://ziglang.org/download/).
Regenerating audio fixtures needs sox, flac and ffmpeg.

```sh
make test        # unit tests (race detector); no network, server or sound device needed
make e2e         # silent end-to-end runs (CLI and app) against a mock server
make viewer      # run the app in your browser (CONFIG=path/to/config.toml; starts at -30 dB)
make launcher-test   # the Scripts-menu launcher, against stand-ins for BGM, SAM and the app
make mister      # ARMv7 binaries in bin/arm/ (checks glibc <= 2.31)
make release VERSION=v1.0.0   # the SD card files and zip in bin/release/
make deploy MISTER=mister.local       # install the release on a MiSTer over ssh
make deploy-dev MISTER=mister.local   # copy dev tools to /media/fat/mistersubsonic/dev
```

A `v*` tag builds a GitHub release (`.github/workflows/release.yml`). The release has:
- the zip;
- its files as assets;
- the Downloader database, published on the `db` branch (`tools/mkdb`).

Try it against a server (sound goes to your default device, starting at
-30 dB unless you pass `-volume`; `-null` plays silently):

```sh
go build -o bin/ ./cmd/mss-cli
bin/mss-cli -config config.toml ping
bin/mss-cli -config config.toml play-album <album-id>
```

`config.toml` can also be written by hand (`sdcard/mistersubsonic/config.example.toml` explains
every option); it needs at least one server:

```toml
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "…"   # or token + salt, or api_key
```

The layout follows the framebuffer: `display.profile` is `auto` (default), `hdmi` or `crt`
(see Video settings above).

## License

GPL-3.0. Bundles miniaudio (public domain / MIT-0), the speexdsp resampler (BSD, see
`third_party/speexdsp/COPYING`), the Mozilla CA bundle from curl.se (MPL-2.0) and the Noto Sans
fonts (SIL Open Font License 1.1, see `internal/gfx/fonts/OFL.txt`).
