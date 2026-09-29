# MiSTer Subsonic

A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.

Status: **playback core plus a first TV interface**: Home (resume, recent/most played/random
albums), album pages, Now Playing and the queue, on HDMI or CRT layouts, driven by a controller
or keyboard. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek, scrobbling and resume work.
Still to come: artists, playlists, starred, search, the setup wizard and the MiSTer launcher.
On-device (MiSTer) checks are still pending; see `docs/spikes.md`.

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

`config.toml` needs at least one server:

```toml
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "…"   # or token + salt, or api_key
```

## License

GPL-3.0. Bundles miniaudio (public domain / MIT-0), the speexdsp resampler (BSD, see
`third_party/speexdsp/COPYING`), the Mozilla CA bundle from curl.se (MPL-2.0) and the Noto Sans
fonts (SIL Open Font License 1.1, see `internal/gfx/fonts/OFL.txt`).
