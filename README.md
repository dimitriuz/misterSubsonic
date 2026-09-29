# MiSTer Subsonic

A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.

Status: **playback core only** (streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
scrobbling, resume) with a terminal dev tool. The TV UI and MiSTer launcher come next.
On-device (MiSTer) checks are still pending; see `docs/spikes.md`.

## Developing

Requirements: Go 1.25+, a C compiler, and for MiSTer builds [zig 0.16.0](https://ziglang.org/download/).
Regenerating audio fixtures needs sox, flac and ffmpeg.

```sh
make test        # unit tests (race detector); no network, server or sound device needed
make e2e         # silent end-to-end run against a mock server
make mister      # ARMv7 binaries in bin/arm/ (checks glibc <= 2.31)
make deploy-dev MISTER=mister.local   # copy dev tools to /media/fat/mistersubsonic/dev
```

Try it against a server (sound goes to your default device; start quiet):

```sh
go build -o bin/ ./cmd/mss-cli
bin/mss-cli -config config.toml ping
bin/mss-cli -config config.toml -volume -30 play-album <album-id>
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
`third_party/speexdsp/COPYING`) and the Mozilla CA bundle from curl.se (MPL-2.0).
