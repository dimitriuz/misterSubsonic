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

## Spike 2 — pending — MiSTer unavailable (user can't boot it yet)

## Spike 3 — pending — MiSTer unavailable (user can't boot it yet)

## Plan 2a on the MiSTer

pending — MiSTer unavailable. Still to do (plan 2a Task 10): run the `Repaint` benchmark on the device (`albums-hdmi-1080p` ≤ 30 ms confirms §8.1), then have the user try the app on the TV (screen fill on HDMI/CRT, controller mapping, B-hold exit, covers).

## Plan 2b on the MiSTer

pending — MiSTer unavailable. Still to do:
- Run `ui.test -test.bench Repaint` on the device. The new cases are feed-hdmi-1080p and search-hdmi-1080p.
- Tune the CRT title-safe margins on a real CRT.
- Have the user try the sidebar, grids and search with the controller and a USB keyboard.
