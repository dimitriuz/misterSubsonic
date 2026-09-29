# MiSTer Subsonic

A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.

Status: **playback core plus the library interface**: a home feed with resume, artists (A–Z),
albums (A–Z, by year, by genre), playlists, starred items and search with an on-screen keyboard,
album pages, Now Playing and the queue, on HDMI (sidebar and cover grids) or CRT (lists)
layouts, driven by a controller or keyboard; a setup wizard, Settings (servers, playback,
display) and a screensaver. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
scrobbling and resume work. Still to come: the MiSTer launcher and releases. On-device (MiSTer)
checks are still pending; see `docs/spikes.md`.

## Setup

On first start the app runs a setup wizard: server address (`http://` / `https://` and `:4533`
are one key away), username, password and, for servers that hand them out, an API key. It
tests the login and saves `config.toml` next to the app. With token login (most servers) only a
token and salt are saved, never the password; servers that need the plain password (LDAP) get it
stored only after you allow it, and a self-signed certificate is skipped only after you allow
that (the safer way is `ca_file`, below). A config file the app can't read is kept as
`config.toml.invalid-<date>` when the wizard replaces it.

Settings (the last section) switches between servers, adds and removes them, and changes the
volume, ReplayGain, scrobbling, transcoding, the layout and the screensaver (dark screen with a
drifting cover after some idle minutes on Now Playing; off with 0). Each server keeps its resume
state, scrobble queue and cover cache in `servers/<name>/` next to the config.

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
| Select | shuffle-play the list | shuffle / repeat modes |

Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, PgUp/PgDn = L/R,
Space = Start. In Search, letters type.

## Developing

Requirements: Go 1.26+, a C compiler, and for MiSTer builds [zig 0.16.0](https://ziglang.org/download/).
Regenerating audio fixtures needs sox, flac and ffmpeg.

```sh
make test        # unit tests (race detector); no network, server or sound device needed
make e2e         # silent end-to-end runs (CLI and app) against a mock server
make viewer      # run the app in your browser (CONFIG=path/to/config.toml; starts at -30 dB)
make mister      # ARMv7 binaries in bin/arm/ (checks glibc <= 2.31)
make deploy-dev MISTER=mister.local   # copy dev tools to /media/fat/mistersubsonic/dev
```

Try it against a server (sound goes to your default device, starting at
-30 dB unless you pass `-volume`; `-null` plays silently):

```sh
go build -o bin/ ./cmd/mss-cli
bin/mss-cli -config config.toml ping
bin/mss-cli -config config.toml play-album <album-id>
```

`config.toml` can also be written by hand; it needs at least one server:

```toml
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "…"   # or token + salt, or api_key
```

The layout follows the framebuffer: `display.profile` is `auto` (default), `hdmi` or `crt`.
A 480i/576i CRT can't be told apart from a 480p/576p HDMI framebuffer, so `auto` picks HDMI;
on interlaced CRT modes set `profile = "crt"` under `[display]`.

## License

GPL-3.0. Bundles miniaudio (public domain / MIT-0), the speexdsp resampler (BSD, see
`third_party/speexdsp/COPYING`), the Mozilla CA bundle from curl.se (MPL-2.0) and the Noto Sans
fonts (SIL Open Font License 1.1, see `internal/gfx/fonts/OFL.txt`).
