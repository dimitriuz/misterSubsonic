# MiSTer Subsonic — Plan 1: Playback Core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A tested, headless playback core. It connects to a Subsonic/Navidrome server, streams FLAC/MP3/WAV (or server transcodes), decodes, resamples to 48 kHz and plays gaplessly, with seek, scrobbling and resume. It is driven by a terminal dev tool (`mss-cli`) on a PC or on the MiSTer.

**Architecture:** This is a single Go module. Only `internal/audio` uses cgo, a thin C shim over miniaudio (decode and ALSA output) and the speexdsp resampler. Everything else is pure Go:
- `internal/stream`: a seekable, windowed HTTP Range reader.
- `internal/subsonic`: the API client (auth fallbacks, TLS options).
- `internal/player`: the queue and state machine on top of the audio engine.
- `internal/config`: TOML config.

**Tech Stack:**
- Go 1.25+ (dev machine has 1.27.1)
- cgo with vendored miniaudio 0.11.25 and speexdsp 1.2.1 `resample.c`
- `github.com/BurntSushi/toml`
- zig 0.16.0 as the ARM cross C compiler
- GitHub Actions

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`. Read it before starting. This plan implements spec §3–§7, §9 (config/build parts), §10 (for these modules) and §11 spikes #1–#3 (audio part).

**Scope / what's next:** The spec is split into three plans. Each one ships working, testable software.
1. **Plan 1 (this):** playback core and dev CLI.
2. **Plan 2:** UI. Framebuffer and gfx backends (fbdev, headless PNG, dev web viewer), input (evdev, MiSTer maps), screens, the wizard, and the spike #3 UI/font performance check.
3. **Plan 3:** MiSTer integration and distribution. Launcher script, `KD_GRAPHICS`, BGM/SAM pause, the app binary, the release zip, the Downloader `db.json`, and the README.

Plans 2 and 3 are written after this plan lands, because spike results can change them.

**How this plan's code was produced:** every code block below was built and run before the plan was written. The prototype passed:
- `go test -race ./...` (repeatedly)
- the silent end-to-end script
- the ARM cross-build, whose highest glibc requirement is 2.29

Copy the blocks exactly. If something doesn't compile, that's a transcription error, not a design gap: check the block again before improvising.

## Global Constraints

- **Module path** `mistersubsonic`, with `go 1.25` in `go.mod`. No other Go dependencies than `github.com/BurntSushi/toml`.
- **License:** GPL-3.0. Vendored C: miniaudio (public domain / MIT-0) and speexdsp resampler (BSD, `third_party/speexdsp/COPYING`). CA bundle: curl.se Mozilla bundle (MPL-2.0).
- **cgo** is allowed only in `internal/audio`.
- **ARM build:** `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"`. `scripts/check-glibc.sh` must pass (glibc ≤ 2.31).
- **Audio output** is fixed at **48 kHz, float32, stereo**. The device ring is 500 ms.
- **Subsonic protocol:** API `1.16.1`, `f=json`, `c=MiSTerSubsonic`, endpoint paths `/rest/<name>.view`.
- **Auth order:** API key, then token (`md5(password+salt)`), then plaintext `enc:` (only over https or with `allow_plaintext_password`).
- **Credentials never appear in logs or error text:** strip `*url.Error` URLs, and use `subsonic.RedactURL` for logging.
- **Defaults (spec §5–§7):**

| Setting | Value |
|---|---|
| Stream window | 32 MiB (behind 25%) |
| Prefetch | 4 MiB, started 20 s before the end |
| Stall timeout | 10 s |
| Retry backoff | 0.5 / 1 / 2 / 4 / 8 s, 30 s budget |
| Scrobble after | `min(duration/2, 240 s)` of *listened* time |
| Queue save | every 30 s |
| Consecutive failures before stop | 3 |
| Transcode | `mp3` at 320 kbps |
| `transcode_format` | ∈ {mp3, flac, wav} |

- **Tests** must pass with `go test -race ./...` and need **no network, no server and no sound hardware**. Audio tests use fakes or miniaudio's null backend.
- **🔇 Never play sound on the user's real devices without asking the user first, every time.** This covers their PC speakers and the MiSTer. Automated runs always use the null device (`-null`, `DeviceOptions{Null: true}`). Approved real-device checks run quietly, at about −30 dBFS or `-volume -30`.
- **Commits:** one per task, at the end of the task. Messages end with the trailer `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Review Focus

These failure modes are implied by the spec but no task's main tests would exercise them. Each one has a pinning test in the task that owns the code:

1. **Credentials leaking into error text or logs.** Go's `*url.Error` embeds the full URL, including `t`/`s`/`apiKey`. The user sees errors on screen and in `log.txt`, so no secret may appear. Tests: `TestErrorsDoNotLeakCredentials` (Task 6) and `TestErrorsDoNotIncludeURL` (Task 5).
2. **A config saved by Windows Notepad over SMB** (UTF-8 BOM, CRLF line endings) must load exactly like a Unix one. Test: `TestLoadToleratesBOMAndCRLF` (Task 8).
3. **The server returns an error document instead of audio** (transcoding not configured, song deleted). That must fail the track with the server's message and skip it, never feed JSON to the decoder. Tests: `TestCheckStreamResponse` (Task 6) and `TestOpenerRejectsErrorDocument` (Task 9).
4. **Songs with unknown duration (0)** get no prefetch, but the queue must still advance when the track ends. Test: `TestUnknownDurationStillAdvances` (Task 10).
5. **A long pause.** While paused the window is full and nothing is read, which must not count as a stall or break the stream on resume. Test: `TestLongPauseDoesNotBreakStream` (Task 5).

---

### Task 1: Spike #1 — how the user's Navidrome serves streams

This is a throwaway probe; its only output is a recorded answer. It needs the user's server.

**Files:**
- Create: `docs/spikes.md`

- [ ] **Step 1: Get access from the user**

Ask the user for their Navidrome URL, a username and a password, plus the ID of one FLAC song. The song ID is visible in Navidrome's web UI URL, or run `getAlbumList2` below and pick one. Don't write the credentials into any file in the repo. Use shell variables only:

```bash
export NAV_URL='http://192.168.1.10:4533'   # from the user
export NAV_USER='…' NAV_PASS='…'
SALT=$(openssl rand -hex 8)
TOKEN=$(printf '%s%s' "$NAV_PASS" "$SALT" | md5sum | cut -d' ' -f1)
AUTH="u=$NAV_USER&t=$TOKEN&s=$SALT&v=1.16.1&c=MiSTerSubsonic&f=json"
```

- [ ] **Step 2: Probe ping and extensions**

```bash
curl -s "$NAV_URL/rest/ping.view?$AUTH"
curl -s "$NAV_URL/rest/getOpenSubsonicExtensions.view?$AUTH"
curl -s "$NAV_URL/rest/getAlbumList2.view?$AUTH&type=newest&size=3" | head -c 1500
```

Expected: `"status":"ok"`, `"openSubsonic":true` and `"type":"navidrome"`. Record `serverVersion` and whether `apiKeyAuthentication` is in the extension list.

- [ ] **Step 3: Probe Range on a raw FLAC stream**

```bash
SONG='…'   # a FLAC song id
curl -s -D - -o /dev/null -H 'Range: bytes=0-' "$NAV_URL/rest/stream.view?$AUTH&id=$SONG&format=raw"
curl -s -D - -o /dev/null -H 'Range: bytes=1000000-' "$NAV_URL/rest/stream.view?$AUTH&id=$SONG&format=raw"
```

Expected: `HTTP/1.1 206`, `Content-Range: bytes 0-…/<size>`, `Accept-Ranges: bytes`, and a `Content-Type` of `audio/flac` (or `audio/x-flac`). Record the status lines and headers.

- [ ] **Step 4: Probe a transcoded stream and timeOffset**

```bash
curl -s -D - -o /tmp/t.mp3 --max-time 5 "$NAV_URL/rest/stream.view?$AUTH&id=$SONG&format=mp3&maxBitRate=320"
curl -s -D - -o /tmp/t2.mp3 --max-time 5 -H 'Range: bytes=0-' "$NAV_URL/rest/stream.view?$AUTH&id=$SONG&format=mp3&maxBitRate=320&timeOffset=60"
file /tmp/t.mp3 /tmp/t2.mp3
```

Record the status, `Content-Length` (it may be absent) and `Accept-Ranges`, and whether `file` reports MPEG audio. If the response is JSON, transcoding isn't configured on the server; note that too.

- [ ] **Step 5: Write `docs/spikes.md`**

```markdown
# Spikes

## Spike 1 — Navidrome stream behaviour (<date>, Navidrome <serverVersion>)

- ping: ok / openSubsonic: <yes/no> / apiKeyAuthentication: <yes/no>
- raw FLAC `Range: bytes=0-`: <status line>, Content-Range <…>, Accept-Ranges <…>, Content-Type <…>
- raw FLAC `Range: bytes=1000000-`: <status line>, Content-Range <…>
- transcoded mp3: <status>, Content-Length <…/absent>, Accept-Ranges <…/absent>, audio=<yes/no>
- timeOffset=60: <honoured / ignored / error>

**Decision:** <see below>
```

Decision rules:
- **206 for raw:** the stream design in spec §5 stands. Write "§5 confirmed".
- **200 without `Accept-Ranges` for raw:** the reader still works, because it detects this and goes sequential. Write "raw seeks will be slow on this server" and tell the user.
- **`timeOffset` ignored for transcoded streams:** write "transcoded seek falls back to restart-from-0". No code change in this plan; the player already reopens with `timeOffset`, and the result just starts from 0.

- [ ] **Step 6: Commit**

```bash
git add docs/spikes.md
git commit -m "docs: record Navidrome stream spike" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Repository scaffold, vendored C, decoder and resampler

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE`
- Create: `third_party/fetch.sh`, `third_party/SHA256SUMS`, `third_party/miniaudio/miniaudio.h`, `third_party/speexdsp/{resample.c,arch.h,speex_resampler.h,COPYING}`
- Create: `internal/audio/cgo.go`, `shim.h`, `shim.c`, `miniaudio_impl.c`, `speex_impl.c`, `format.go`, `decoder.go`, `resampler.go`
- Create: `internal/audio/testdata/gen.sh` and the generated fixtures
- Test: `internal/audio/decoder_test.go`, `internal/audio/resampler_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `audio.Format` (`FormatUnknown`, `FormatFLAC`, `FormatMP3`, `FormatWAV`), `audio.FormatFromSuffix(string) Format`
  - `audio.Decoder` interface: `SampleRate() int`, `LengthFrames() uint64`, `Read([]float32) (int, error)` (interleaved stereo; `io.EOF` at the end), `SeekFrame(uint64) error`, `Close() error`
  - `audio.OpenDecoderFunc`, `audio.OpenDecoder(io.ReadSeeker, Format) (Decoder, error)`
  - `audio.NewResampler(in, out, quality int) (*Resampler, error)` with `Process(in, out []float32) []float32`, `Flush(out) []float32`, `InRate() int`, `Close()`, and `audio.DefaultResampleQuality = 5`
  - C header `shim.h`, which also declares the device API that Task 3 implements

**Notes:**
- The C decoder calls back into Go (`mssGoRead`/`mssGoSeek`) through a `runtime/cgo.Handle`, so any `io.ReadSeeker`, including the network stream, can feed miniaudio.
- The decoder always outputs **stereo float32 at the source rate**. miniaudio upmixes mono.
- The speexdsp resampler is compiled standalone with `-DOUTSIDE_SPEEX -DFLOATING_POINT -DRANDOM_PREFIX=spx_mss`. The prefix keeps its symbols (`spx_mss_resampler_*`) from colliding with our `mss_resampler_*`.
- `shim.h` declares the device functions now, but `device.c` only arrives in Task 3. The package still links, because nothing calls them yet.

- [ ] **Step 1: Initialise the module and licence**

```bash
go mod init mistersubsonic
go mod edit -go=1.25
curl -fsSL --retry 3 -o LICENSE https://www.gnu.org/licenses/gpl-3.0.txt
head -2 LICENSE   # GNU GENERAL PUBLIC LICENSE / Version 3, 29 June 2007
```

`.gitignore`:

```gitignore
/bin/
*.test
*.tmp
/config.toml
```

- [ ] **Step 2: Vendor the C sources**

`third_party/fetch.sh`:

```sh
#!/bin/sh
# Re-downloads the vendored C sources at their pinned versions and verifies
# them against SHA256SUMS. The files are committed; run this only to audit
# them or when bumping a version (then regenerate SHA256SUMS).
set -eu
cd "$(dirname "$0")"

MINIAUDIO=0.11.25
SPEEXDSP=SpeexDSP-1.2.1
MA=https://raw.githubusercontent.com/mackron/miniaudio/$MINIAUDIO
SX=https://raw.githubusercontent.com/xiph/speexdsp/$SPEEXDSP

mkdir -p miniaudio speexdsp
curl -fsSL -o miniaudio/miniaudio.h        "$MA/miniaudio.h"
curl -fsSL -o speexdsp/resample.c          "$SX/libspeexdsp/resample.c"
curl -fsSL -o speexdsp/arch.h              "$SX/libspeexdsp/arch.h"
curl -fsSL -o speexdsp/speex_resampler.h   "$SX/include/speex/speex_resampler.h"
curl -fsSL -o speexdsp/COPYING             "$SX/COPYING"
sha256sum -c SHA256SUMS
```

`third_party/SHA256SUMS`:

```text
ac7af4de748b7e26b777f37e01cee313a308a7296a3eb080e2906b320cc55c89  miniaudio/miniaudio.h
c28fabfc082d0e7634eb678e2e3a2bc091148bbb8324e3d06669c9a9faf6793a  speexdsp/resample.c
102f6a14a95f8ae0bcfc69270f3a9e3fbba08b63bba688ca524f71e3faa48c23  speexdsp/arch.h
7e439ec0dd30c32216b3ced17135f8992e5aaf53389d3f5996a7d900c453e65f  speexdsp/speex_resampler.h
2654a4264b2bfe298dedc508748d140111840c315cc8eb646a3a68c13fa75b01  speexdsp/COPYING
```

Run:

```bash
chmod +x third_party/fetch.sh && ./third_party/fetch.sh
```

Expected: five lines ending in `OK`. If a checksum fails, stop: upstream changed a pinned file, so tell the user.

- [ ] **Step 3: Generate the audio fixtures**

`sox`, `flac` and `ffmpeg` (with libmp3lame) must be installed; they are on the dev machine.

`internal/audio/testdata/gen.sh`:

```sh
#!/bin/sh
# Regenerates the decoder fixtures. Needs sox and ffmpeg (with libmp3lame).
# Each FLAC has a WAV twin with identical samples so tests can compare them.
set -eu
cd "$(dirname "$0")"
sox -n -r 44100 -b 16 -c 2 tone-44k16.wav synth 0.5 sine 440 sine 660 gain -6
sox -n -r 96000 -b 24 -c 2 tone-96k24.wav synth 0.5 sine 1000 sine 1500 gain -6
sox -n -r 44100 -b 16 -c 1 mono-44k16.wav synth 0.5 sine 440 gain -6
for f in tone-44k16 tone-96k24 mono-44k16; do
  flac -s -f --best -o "$f.flac" "$f.wav"
done
ffmpeg -loglevel error -y -i tone-44k16.wav -c:a libmp3lame -b:a 320k tone-44k16.mp3
# Split point is deliberately not on a FLAC block boundary.
sox tone-44k16.wav half-a.wav trim 0 10007s
sox tone-44k16.wav half-b.wav trim 10007s
flac -s -f -o half-a.flac half-a.wav
flac -s -f -o half-b.flac half-b.wav
rm half-a.wav half-b.wav
```

```bash
chmod +x internal/audio/testdata/gen.sh && internal/audio/testdata/gen.sh && ls internal/audio/testdata
```

Expected: `half-a.flac half-b.flac mono-44k16.{flac,wav} tone-44k16.{flac,mp3,wav} tone-96k24.{flac,wav}` plus `gen.sh`, about 620 KB in total. A sox warning about a `fact` chunk is harmless. These files are committed.

- [ ] **Step 4: Write the failing tests**

`internal/audio/decoder_test.go`:

```go
package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func decodeAll(t *testing.T, name string, f Format) ([]float32, Decoder) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	d, err := OpenDecoder(bytes.NewReader(data), f)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	var all []float32
	buf := make([]float32, 4096*2)
	for {
		n, err := d.Read(buf)
		all = append(all, buf[:n*2]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
	return all, d
}

func TestDecodeFLACMatchesWAV(t *testing.T) {
	for _, base := range []string{"tone-44k16", "tone-96k24", "mono-44k16"} {
		t.Run(base, func(t *testing.T) {
			flac, fd := decodeAll(t, base+".flac", FormatFLAC)
			defer fd.Close()
			wav, wd := decodeAll(t, base+".wav", FormatWAV)
			defer wd.Close()
			if len(flac) == 0 || len(flac) != len(wav) {
				t.Fatalf("sample count flac=%d wav=%d", len(flac), len(wav))
			}
			for i := range flac {
				if flac[i] != wav[i] {
					t.Fatalf("sample %d differs: flac=%v wav=%v", i, flac[i], wav[i])
				}
			}
			if fd.SampleRate() != wd.SampleRate() {
				t.Fatalf("rate flac=%d wav=%d", fd.SampleRate(), wd.SampleRate())
			}
			if got := fd.LengthFrames(); got != uint64(len(flac)/2) {
				t.Fatalf("LengthFrames=%d, decoded %d", got, len(flac)/2)
			}
		})
	}
}

func TestDecodeRatesAndMonoUpmix(t *testing.T) {
	_, d := decodeAll(t, "tone-96k24.flac", FormatFLAC)
	defer d.Close()
	if d.SampleRate() != 96000 {
		t.Fatalf("rate = %d, want 96000", d.SampleRate())
	}
	mono, md := decodeAll(t, "mono-44k16.flac", FormatFLAC)
	defer md.Close()
	for i := 0; i < len(mono); i += 2 {
		if mono[i] != mono[i+1] {
			t.Fatalf("mono not duplicated to both channels at frame %d", i/2)
		}
	}
}

func TestDecodeMP3(t *testing.T) {
	pcm, d := decodeAll(t, "tone-44k16.mp3", FormatMP3)
	defer d.Close()
	if d.SampleRate() != 44100 {
		t.Fatalf("rate = %d", d.SampleRate())
	}
	frames := len(pcm) / 2
	if frames < 44100*4/10 || frames > 44100*6/10 {
		t.Fatalf("mp3 decoded %d frames, want about 22050", frames)
	}
	var peak float64
	for _, s := range pcm {
		peak = math.Max(peak, math.Abs(float64(s)))
	}
	if peak < 0.1 {
		t.Fatalf("mp3 decoded to near-silence (peak %v)", peak)
	}
}

func TestDecoderSeek(t *testing.T) {
	all, d0 := decodeAll(t, "tone-44k16.flac", FormatFLAC)
	d0.Close()
	data, _ := os.ReadFile("testdata/tone-44k16.flac")
	d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SeekFrame(12345); err != nil {
		t.Fatal(err)
	}
	buf := make([]float32, 64)
	n, err := d.Read(buf)
	if err != nil || n != 32 {
		t.Fatalf("read after seek: n=%d err=%v", n, err)
	}
	for i := 0; i < 64; i++ {
		if buf[i] != all[12345*2+i] {
			t.Fatalf("after seek sample %d = %v, want %v", i, buf[i], all[12345*2+i])
		}
	}
}

type failingReader struct{ io.ReadSeeker }

var errBoom = errors.New("boom")

func (failingReader) Read([]byte) (int, error) { return 0, errBoom }

func TestDecoderPropagatesSourceError(t *testing.T) {
	data, _ := os.ReadFile("testdata/tone-44k16.flac")
	_, err := OpenDecoder(failingReader{bytes.NewReader(data)}, FormatFLAC)
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want wrapping errBoom", err)
	}
}

func TestFormatFromSuffix(t *testing.T) {
	cases := map[string]Format{"flac": FormatFLAC, ".FLAC": FormatFLAC, "mp3": FormatMP3, "wav": FormatWAV, "m4a": FormatUnknown, "opus": FormatUnknown, "": FormatUnknown}
	for in, want := range cases {
		if got := FormatFromSuffix(in); got != want {
			t.Errorf("FormatFromSuffix(%q) = %v, want %v", in, got, want)
		}
	}
}
```

`internal/audio/resampler_test.go`:

```go
package audio

import (
	"math"
	"testing"
)

func sine(frames, rate int, hz float64) []float32 {
	out := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		v := float32(0.5 * math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))
		out[2*i], out[2*i+1] = v, v
	}
	return out
}

func TestResamplerOutputLength(t *testing.T) {
	r, err := NewResampler(44100, 48000, DefaultResampleQuality)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	in := sine(44100, 44100, 1000)
	var out []float32
	for i := 0; i < len(in); i += 4096 * 2 {
		end := min(i+4096*2, len(in))
		out = r.Process(in[i:end], out)
	}
	out = r.Flush(out)
	frames := len(out) / 2
	if frames < 47990 || frames > 48010 {
		t.Fatalf("1 s at 44.1k resampled to %d frames, want ~48000", frames)
	}
}

// A 1 kHz sine resampled 44.1k -> 48k must stay a clean 1 kHz sine.
func TestResamplerLowDistortion(t *testing.T) {
	r, err := NewResampler(44100, 48000, DefaultResampleQuality)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := r.Flush(r.Process(sine(44100, 44100, 1000), nil))
	// Compare the middle of the output against an ideal 48k sine with the best-fit phase.
	var num, den float64
	start, n := 4800, 38400
	for i := start; i < start+n; i++ {
		s := float64(out[2*i])
		num += s * math.Sin(2*math.Pi*1000*float64(i)/48000)
		den += s * math.Cos(2*math.Pi*1000*float64(i)/48000)
	}
	phase := math.Atan2(den, num)
	var errPow, sigPow float64
	for i := start; i < start+n; i++ {
		ideal := 0.5 * math.Sin(2*math.Pi*1000*float64(i)/48000+phase)
		d := float64(out[2*i]) - ideal
		errPow += d * d
		sigPow += ideal * ideal
	}
	snr := 10 * math.Log10(sigPow/errPow)
	if snr < 60 {
		t.Fatalf("resampled sine SNR = %.1f dB, want >= 60 dB", snr)
	}
}

func TestResamplerContinuityAcrossChunks(t *testing.T) {
	in := sine(20000, 44100, 440)
	whole, _ := NewResampler(44100, 48000, DefaultResampleQuality)
	defer whole.Close()
	a := whole.Process(in, nil)

	split, _ := NewResampler(44100, 48000, DefaultResampleQuality)
	defer split.Close()
	b := split.Process(in[:7777*2], nil)
	b = split.Process(in[7777*2:], b)

	if len(a) != len(b) {
		t.Fatalf("len whole=%d split=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("sample %d differs whole=%v split=%v", i, a[i], b[i])
		}
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/audio/`
Expected: build failure, e.g. `undefined: OpenDecoder`, `undefined: Format`, `undefined: NewResampler`.

- [ ] **Step 6: Write the C shim and Go bindings**

`internal/audio/shim.h`:

```c
#ifndef MSS_SHIM_H
#define MSS_SHIM_H

#include <stddef.h>
#include <stdint.h>

/* Encoding hints passed from Go (must match audio.Format). */
enum { MSS_FMT_UNKNOWN = 0, MSS_FMT_FLAC = 1, MSS_FMT_MP3 = 2, MSS_FMT_WAV = 3 };

/* Decoder: pulls bytes through the Go callbacks mssGoRead/mssGoSeek and
   always outputs interleaved stereo float32 at the source sample rate. */
typedef struct mss_decoder mss_decoder;

typedef struct {
    uint32_t sample_rate;
    uint64_t length_frames; /* 0 when unknown */
} mss_decoder_info;

mss_decoder* mss_decoder_open(uintptr_t handle, int format, int* result);
void mss_decoder_get_info(mss_decoder* d, mss_decoder_info* info);
/* Returns a miniaudio result code; MA_AT_END (-17) with *frames_read == 0 at end of stream. */
int mss_decoder_read(mss_decoder* d, float* out, uint64_t frames, uint64_t* frames_read);
int mss_decoder_seek(mss_decoder* d, uint64_t frame);
void mss_decoder_close(mss_decoder* d);

/* Resampler: interleaved stereo float32, speexdsp. */
typedef struct mss_resampler mss_resampler;

mss_resampler* mss_resampler_new(uint32_t in_rate, uint32_t out_rate, int quality, int* err);
int mss_resampler_process(mss_resampler* r, const float* in, uint32_t* in_frames, float* out, uint32_t* out_frames);
uint32_t mss_resampler_input_latency(mss_resampler* r);
void mss_resampler_free(mss_resampler* r);

/* Output device: one global 48 kHz stereo float32 device fed from a
   single-producer ring. The device callback never blocks. */
int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames);
void mss_device_close(void);
uint32_t mss_device_write(const float* frames, uint32_t frame_count);
uint32_t mss_device_space(void);
uint64_t mss_device_consumed(void);
void mss_device_set_paused(int paused);
void mss_device_set_volume(float volume);
void mss_device_flush(void);

#endif
```

`internal/audio/shim.c`:

```c
#include <stdlib.h>

#include "miniaudio.h"
#include "speex_resampler.h"
#include "shim.h"
#include "_cgo_export.h"

/* ---------- decoder ---------- */

struct mss_decoder {
    ma_decoder dec;
    uintptr_t handle;
};

static ma_result on_read(ma_decoder* dec, void* buf, size_t n, size_t* nread) {
    mss_decoder* d = (mss_decoder*)dec->pUserData;
    size_t got = 0;
    int rc = mssGoRead(d->handle, buf, n, &got);
    if (nread != NULL) {
        *nread = got;
    }
    if (rc != 0) {
        return MA_ERROR;
    }
    if (got == 0 && n > 0) {
        return MA_AT_END;
    }
    return MA_SUCCESS;
}

static ma_result on_seek(ma_decoder* dec, ma_int64 off, ma_seek_origin origin) {
    mss_decoder* d = (mss_decoder*)dec->pUserData;
    int whence = origin == ma_seek_origin_start ? 0 : (origin == ma_seek_origin_current ? 1 : 2);
    return mssGoSeek(d->handle, off, whence) == 0 ? MA_SUCCESS : MA_ERROR;
}

mss_decoder* mss_decoder_open(uintptr_t handle, int format, int* result) {
    mss_decoder* d = (mss_decoder*)calloc(1, sizeof(*d));
    if (d == NULL) {
        *result = MA_OUT_OF_MEMORY;
        return NULL;
    }
    d->handle = handle;
    ma_decoder_config cfg = ma_decoder_config_init(ma_format_f32, 2, 0);
    switch (format) {
    case MSS_FMT_FLAC: cfg.encodingFormat = ma_encoding_format_flac; break;
    case MSS_FMT_MP3:  cfg.encodingFormat = ma_encoding_format_mp3;  break;
    case MSS_FMT_WAV:  cfg.encodingFormat = ma_encoding_format_wav;  break;
    default:           cfg.encodingFormat = ma_encoding_format_unknown; break;
    }
    ma_result r = ma_decoder_init(on_read, on_seek, d, &cfg, &d->dec);
    *result = r;
    if (r != MA_SUCCESS) {
        free(d);
        return NULL;
    }
    return d;
}

void mss_decoder_get_info(mss_decoder* d, mss_decoder_info* info) {
    ma_format fmt;
    ma_uint32 ch, rate;
    ma_uint64 len = 0;
    ma_decoder_get_data_format(&d->dec, &fmt, &ch, &rate, NULL, 0);
    if (ma_decoder_get_length_in_pcm_frames(&d->dec, &len) != MA_SUCCESS) {
        len = 0;
    }
    info->sample_rate = rate;
    info->length_frames = len;
}

int mss_decoder_read(mss_decoder* d, float* out, uint64_t frames, uint64_t* frames_read) {
    ma_uint64 got = 0;
    ma_result r = ma_decoder_read_pcm_frames(&d->dec, out, frames, &got);
    *frames_read = got;
    return r;
}

int mss_decoder_seek(mss_decoder* d, uint64_t frame) {
    return ma_decoder_seek_to_pcm_frame(&d->dec, frame);
}

void mss_decoder_close(mss_decoder* d) {
    if (d == NULL) {
        return;
    }
    ma_decoder_uninit(&d->dec);
    free(d);
}

/* ---------- resampler ---------- */

struct mss_resampler {
    SpeexResamplerState* st;
};

mss_resampler* mss_resampler_new(uint32_t in_rate, uint32_t out_rate, int quality, int* err) {
    mss_resampler* r = (mss_resampler*)calloc(1, sizeof(*r));
    if (r == NULL) {
        *err = -1;
        return NULL;
    }
    r->st = speex_resampler_init(2, in_rate, out_rate, quality, err);
    if (r->st == NULL) {
        free(r);
        return NULL;
    }
    speex_resampler_skip_zeros(r->st);
    return r;
}

int mss_resampler_process(mss_resampler* r, const float* in, uint32_t* in_frames, float* out, uint32_t* out_frames) {
    return speex_resampler_process_interleaved_float(r->st, in, in_frames, out, out_frames);
}

uint32_t mss_resampler_input_latency(mss_resampler* r) {
    return (uint32_t)speex_resampler_get_input_latency(r->st);
}

void mss_resampler_free(mss_resampler* r) {
    if (r == NULL) {
        return;
    }
    speex_resampler_destroy(r->st);
    free(r);
}
```

`internal/audio/miniaudio_impl.c`:

```c
#define MINIAUDIO_IMPLEMENTATION
#include "miniaudio.h"
```

`internal/audio/speex_impl.c`:

```c
#include "resample.c"
```

`internal/audio/cgo.go`:

```go
package audio

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/../../third_party/miniaudio -I${SRCDIR}/../../third_party/speexdsp
#cgo CFLAGS: -DMA_NO_ENCODING -DMA_NO_GENERATION -DMA_NO_RESOURCE_MANAGER -DMA_NO_NODE_GRAPH -DMA_NO_ENGINE
#cgo CFLAGS: -DOUTSIDE_SPEEX -DFLOATING_POINT -DRANDOM_PREFIX=spx_mss -DEXPORT=
#cgo arm CFLAGS: -DMA_ENABLE_ONLY_SPECIFIC_BACKENDS -DMA_ENABLE_ALSA -DMA_ENABLE_NULL
#cgo linux LDFLAGS: -ldl -lpthread -lm
#include "shim.h"
*/
import "C"
```

`internal/audio/format.go`:

```go
package audio

import "strings"

// Format is the container/codec of a source stream. Values match MSS_FMT_* in shim.h.
type Format int

const (
	FormatUnknown Format = iota
	FormatFLAC
	FormatMP3
	FormatWAV
)

// FormatFromSuffix maps a file suffix ("flac", ".MP3") to a Format the
// device can decode natively. Anything else is FormatUnknown.
func FormatFromSuffix(suffix string) Format {
	switch strings.ToLower(strings.TrimPrefix(suffix, ".")) {
	case "flac":
		return FormatFLAC
	case "mp3":
		return FormatMP3
	case "wav", "wave":
		return FormatWAV
	}
	return FormatUnknown
}

func (f Format) String() string {
	switch f {
	case FormatFLAC:
		return "FLAC"
	case FormatMP3:
		return "MP3"
	case FormatWAV:
		return "WAV"
	}
	return "unknown"
}
```

`internal/audio/decoder.go`:

```go
package audio

/*
#include "shim.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"runtime/cgo"
	"unsafe"
)

// Decoder produces interleaved stereo float32 frames at the source's native sample rate.
type Decoder interface {
	SampleRate() int
	// LengthFrames is the total length in frames, or 0 when unknown.
	LengthFrames() uint64
	// Read fills dst (len must be even) and returns the number of frames
	// written. It returns io.EOF (with 0 frames) at the end of the stream.
	Read(dst []float32) (int, error)
	SeekFrame(frame uint64) error
	Close() error
}

// OpenDecoderFunc is how the engine opens decoders; tests substitute fakes.
type OpenDecoderFunc func(src io.ReadSeeker, f Format) (Decoder, error)

const maAtEnd = -17 // MA_AT_END

type source struct {
	r   io.ReadSeeker
	err error
}

type cDecoder struct {
	d      *C.mss_decoder
	h      cgo.Handle
	src    *source
	rate   int
	length uint64
}

// OpenDecoder opens a miniaudio decoder that pulls bytes from src.
func OpenDecoder(src io.ReadSeeker, f Format) (Decoder, error) {
	s := &source{r: src}
	h := cgo.NewHandle(s)
	var rc C.int
	d := C.mss_decoder_open(C.uintptr_t(h), C.int(f), &rc)
	if d == nil {
		h.Delete()
		if s.err != nil {
			return nil, fmt.Errorf("audio: open %s decoder: %w", f, s.err)
		}
		return nil, fmt.Errorf("audio: open %s decoder: miniaudio result %d", f, int(rc))
	}
	var info C.mss_decoder_info
	C.mss_decoder_get_info(d, &info)
	return &cDecoder{d: d, h: h, src: s, rate: int(info.sample_rate), length: uint64(info.length_frames)}, nil
}

func (c *cDecoder) SampleRate() int      { return c.rate }
func (c *cDecoder) LengthFrames() uint64 { return c.length }

func (c *cDecoder) Read(dst []float32) (int, error) {
	frames := len(dst) / 2
	if frames == 0 {
		return 0, nil
	}
	var got C.uint64_t
	rc := C.mss_decoder_read(c.d, (*C.float)(unsafe.Pointer(&dst[0])), C.uint64_t(frames), &got)
	if c.src.err != nil {
		return int(got), c.src.err
	}
	if got == 0 {
		if rc == maAtEnd || rc == 0 {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("audio: decode: miniaudio result %d", int(rc))
	}
	return int(got), nil
}

func (c *cDecoder) SeekFrame(frame uint64) error {
	if rc := C.mss_decoder_seek(c.d, C.uint64_t(frame)); rc != 0 {
		if c.src.err != nil {
			return c.src.err
		}
		return fmt.Errorf("audio: seek: miniaudio result %d", int(rc))
	}
	return nil
}

func (c *cDecoder) Close() error {
	if c.d == nil {
		return errors.New("audio: decoder already closed")
	}
	C.mss_decoder_close(c.d)
	c.d = nil
	c.h.Delete()
	return nil
}

//export mssGoRead
func mssGoRead(h C.uintptr_t, buf unsafe.Pointer, n C.size_t, nread *C.size_t) C.int {
	s := cgo.Handle(h).Value().(*source)
	dst := unsafe.Slice((*byte)(buf), int(n))
	total := 0
	for total < len(dst) {
		k, err := s.r.Read(dst[total:])
		total += k
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.err = err
			*nread = C.size_t(total)
			return 1
		}
	}
	*nread = C.size_t(total)
	return 0
}

//export mssGoSeek
func mssGoSeek(h C.uintptr_t, offset C.int64_t, whence C.int) C.int {
	s := cgo.Handle(h).Value().(*source)
	if _, err := s.r.Seek(int64(offset), int(whence)); err != nil {
		s.err = err
		return 1
	}
	return 0
}
```

`internal/audio/resampler.go`:

```go
package audio

/*
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// DefaultResampleQuality is the speexdsp quality level (0-10).
const DefaultResampleQuality = 5

// Resampler converts interleaved stereo float32 between sample rates.
// Feeding consecutive tracks of the same rate through one Resampler is
// seamless, which is what keeps gapless playback gapless.
type Resampler struct {
	r       *C.mss_resampler
	inRate  int
	outRate int
}

func NewResampler(inRate, outRate, quality int) (*Resampler, error) {
	var rc C.int
	r := C.mss_resampler_new(C.uint32_t(inRate), C.uint32_t(outRate), C.int(quality), &rc)
	if r == nil {
		return nil, fmt.Errorf("audio: resampler %d->%d: speex error %d", inRate, outRate, int(rc))
	}
	return &Resampler{r: r, inRate: inRate, outRate: outRate}, nil
}

func (r *Resampler) InRate() int { return r.inRate }

// Process resamples all of in and appends the output to out.
func (r *Resampler) Process(in []float32, out []float32) []float32 {
	for len(in) >= 2 {
		inFrames := len(in) / 2
		want := inFrames*r.outRate/r.inRate + 64
		if cap(out)-len(out) < want*2 {
			grown := make([]float32, len(out), len(out)+want*2)
			copy(grown, out)
			out = grown
		}
		dst := out[len(out):cap(out)]
		cin := C.uint32_t(inFrames)
		cout := C.uint32_t(len(dst) / 2)
		C.mss_resampler_process(r.r, (*C.float)(unsafe.Pointer(&in[0])), &cin, (*C.float)(unsafe.Pointer(&dst[0])), &cout)
		out = out[:len(out)+int(cout)*2]
		in = in[int(cin)*2:]
		if cin == 0 && cout == 0 {
			break
		}
	}
	return out
}

// Flush pushes the resampler's buffered tail out by feeding silence
// equal to its input latency, and appends the output to out.
func (r *Resampler) Flush(out []float32) []float32 {
	lat := int(C.mss_resampler_input_latency(r.r))
	return r.Process(make([]float32, lat*2), out)
}

func (r *Resampler) Close() {
	if r.r != nil {
		C.mss_resampler_free(r.r)
		r.r = nil
	}
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go vet ./internal/audio/ && go test -race ./internal/audio/`
Expected: `ok  mistersubsonic/internal/audio`. The first build takes 10–20 s while miniaudio compiles, then it is cached. The tests assert:
- FLAC decodes bit-exactly against its WAV twin (16/44.1, 24/96, mono)
- mono is upmixed to both channels
- MP3 decodes about 0.5 s of non-silence
- a seek lands on exactly the right frame
- source errors propagate
- 44.1→48 kHz keeps a clean sine (SNR ≥ 60 dB) and is continuous across chunk boundaries

- [ ] **Step 8: Commit**

```bash
git add go.mod .gitignore LICENSE third_party internal/audio
git commit -m "audio: vendored miniaudio + speexdsp, decoder and resampler" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Output device, ARM toolchain, spikes #2 and #3 (audio) on the MiSTer

**Files:**
- Create: `internal/audio/device.c`, `internal/audio/device.go`
- Create: `scripts/check-glibc.sh`, `Makefile` (the first version; Task 11 replaces it)
- Test: `internal/audio/device_test.go`, `internal/audio/device_manual_test.go` (opt-in, human-run), `internal/audio/bench_test.go`
- Modify: `docs/spikes.md` (append the spike #2/#3 results)

**Interfaces:**
- Consumes: `shim.h` (Task 2).
- Produces:
  - `audio.OutputRate = 48000`
  - `audio.Output` interface: `Write([]float32) int`, `Consumed() uint64`, `SetPaused(bool)`, `SetVolume(float32)`, `Flush()`, `Close() error`
  - `audio.DeviceOptions{Name string; Null bool; RingFrames int}`, `audio.OpenDevice(DeviceOptions) (Output, error)`

**Notes:**
- There is one global device.
- The C callback only drains a lock-free `ma_pcm_rb` ring and never blocks.
- **Pause** outputs silence without consuming the ring.
- **Volume** is applied in the callback, so it changes instantly.
- **`Flush` contract:** after `Flush` returns, `Consumed()` equals the total number of frames ever written. The engine's position bookkeeping (Task 4) relies on this.

- [ ] **Step 1: Write the failing test**

`internal/audio/device_test.go`:

```go
package audio

import (
	"testing"
	"time"
)

func TestNullDeviceConsumesAndFlushes(t *testing.T) {
	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	buf := make([]float32, 9600*2)
	if n := out.Write(buf); n != 4800 {
		t.Fatalf("Write into a 4800-frame ring took %d frames", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for out.Consumed() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("null device never consumed any frames")
		}
		time.Sleep(5 * time.Millisecond)
	}

	out.SetPaused(true)
	time.Sleep(50 * time.Millisecond)
	c1 := out.Consumed()
	time.Sleep(100 * time.Millisecond)
	if c2 := out.Consumed(); c2 != c1 {
		t.Fatalf("consumed advanced while paused: %d -> %d", c1, c2)
	}

	out.Flush()
	if got := out.Consumed(); got != 4800 {
		t.Fatalf("after Flush Consumed = %d, want 4800 (everything written)", got)
	}
	out.SetPaused(false)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run NullDevice ./internal/audio/`
Expected: build failure, `undefined: OpenDevice` / `undefined: DeviceOptions`.

- [ ] **Step 3: Implement the device**

`internal/audio/device.c`:

```c
#include <string.h>
#include <time.h>

#include "miniaudio.h"
#include "shim.h"

/* ---------- device ---------- */

static ma_device g_device;
static ma_context g_context;
static ma_pcm_rb g_ring;
static int g_open;
static volatile int g_paused;
static volatile int g_flush_req;
static volatile uint64_t g_consumed;
static volatile float g_volume = 1.0f;

static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames) {
    (void)dev;
    (void)in;
    float* dst = (float*)out;
    if (g_flush_req) {
        ma_uint32 avail = ma_pcm_rb_available_read(&g_ring);
        while (avail > 0) {
            ma_uint32 n = avail;
            void* p;
            if (ma_pcm_rb_acquire_read(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
                break;
            }
            ma_pcm_rb_commit_read(&g_ring, n);
            __atomic_add_fetch(&g_consumed, n, __ATOMIC_SEQ_CST);
            avail -= n;
        }
        __atomic_store_n(&g_flush_req, 0, __ATOMIC_SEQ_CST);
    }
    ma_uint32 done = 0;
    if (!g_paused) {
        while (done < frames) {
            ma_uint32 n = frames - done;
            void* p;
            if (ma_pcm_rb_acquire_read(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
                break;
            }
            memcpy(dst + done * 2, p, n * 2 * sizeof(float));
            ma_pcm_rb_commit_read(&g_ring, n);
            done += n;
        }
        __atomic_add_fetch(&g_consumed, done, __ATOMIC_SEQ_CST);
    }
    float vol = g_volume;
    if (vol != 1.0f) {
        for (ma_uint32 i = 0; i < done * 2; i++) {
            dst[i] *= vol;
        }
    }
    if (done < frames) {
        memset(dst + done * 2, 0, (frames - done) * 2 * sizeof(float));
    }
}

int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames) {
    if (g_open) {
        return MA_INVALID_OPERATION;
    }
    ma_backend null_only[1] = { ma_backend_null };
    ma_result r = ma_context_init(null_backend ? null_only : NULL, null_backend ? 1 : 0, NULL, &g_context);
    if (r != MA_SUCCESS) {
        return r;
    }
    r = ma_pcm_rb_init(ma_format_f32, 2, ring_frames, NULL, NULL, &g_ring);
    if (r != MA_SUCCESS) {
        ma_context_uninit(&g_context);
        return r;
    }
    ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
    cfg.playback.format = ma_format_f32;
    cfg.playback.channels = 2;
    cfg.sampleRate = 48000;
    cfg.periodSizeInMilliseconds = 20;
    cfg.dataCallback = data_cb;
    cfg.noPreSilencedOutputBuffer = MA_TRUE;

    ma_device_info* infos = NULL;
    ma_uint32 count = 0;
    ma_device_id* id = NULL;
    if (device_name != NULL && device_name[0] != '\0' &&
        ma_context_get_devices(&g_context, &infos, &count, NULL, NULL) == MA_SUCCESS) {
        for (ma_uint32 i = 0; i < count; i++) {
            if (strcmp(infos[i].name, device_name) == 0 ||
                (g_context.backend == ma_backend_alsa && strcmp(infos[i].id.alsa, device_name) == 0)) {
                id = &infos[i].id;
                break;
            }
        }
    }
    cfg.playback.pDeviceID = id;

    g_paused = 0;
    g_flush_req = 0;
    g_consumed = 0;
    r = ma_device_init(&g_context, &cfg, &g_device);
    if (r != MA_SUCCESS) {
        ma_pcm_rb_uninit(&g_ring);
        ma_context_uninit(&g_context);
        return r;
    }
    r = ma_device_start(&g_device);
    if (r != MA_SUCCESS) {
        ma_device_uninit(&g_device);
        ma_pcm_rb_uninit(&g_ring);
        ma_context_uninit(&g_context);
        return r;
    }
    g_open = 1;
    return MA_SUCCESS;
}

void mss_device_close(void) {
    if (!g_open) {
        return;
    }
    ma_device_uninit(&g_device);
    ma_pcm_rb_uninit(&g_ring);
    ma_context_uninit(&g_context);
    g_open = 0;
}

uint32_t mss_device_write(const float* frames, uint32_t frame_count) {
    uint32_t done = 0;
    while (done < frame_count) {
        ma_uint32 n = frame_count - done;
        void* p;
        if (ma_pcm_rb_acquire_write(&g_ring, &n, &p) != MA_SUCCESS || n == 0) {
            break;
        }
        memcpy(p, frames + done * 2, n * 2 * sizeof(float));
        ma_pcm_rb_commit_write(&g_ring, n);
        done += n;
    }
    return done;
}

uint32_t mss_device_space(void) {
    return ma_pcm_rb_available_write(&g_ring);
}

uint64_t mss_device_consumed(void) {
    return __atomic_load_n(&g_consumed, __ATOMIC_SEQ_CST);
}

void mss_device_set_paused(int paused) {
    __atomic_store_n(&g_paused, paused, __ATOMIC_SEQ_CST);
}

void mss_device_set_volume(float volume) {
    g_volume = volume;
}

void mss_device_flush(void) {
    __atomic_store_n(&g_flush_req, 1, __ATOMIC_SEQ_CST);
    while (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
        struct timespec ts = { 0, 1000000 };
        nanosleep(&ts, NULL);
    }
}
```

`internal/audio/device.go`:

```go
package audio

/*
#include <stdlib.h>
#include "shim.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// OutputRate is the fixed device rate. MiSTer's ALSA default device
// resamples to 48 kHz linearly (and crackles), so we always hand it 48 kHz.
const OutputRate = 48000

// Output is where the engine writes interleaved stereo float32 at OutputRate.
type Output interface {
	// Write copies as many frames as fit and returns how many were taken.
	Write(frames []float32) int
	// Consumed is the running total of frames taken out of the ring by the
	// device, including frames discarded by Flush.
	Consumed() uint64
	SetPaused(paused bool)
	SetVolume(linear float32)
	// Flush discards everything buffered. After it returns, Consumed equals
	// the total number of frames ever written.
	Flush()
	Close() error
}

type DeviceOptions struct {
	// Name selects a playback device by name or ALSA id; "" means default.
	Name string
	// Null uses miniaudio's null backend (for tests and headless runs).
	Null bool
	// RingFrames is the ring size; 0 means 500 ms.
	RingFrames int
}

type device struct{}

// OpenDevice opens the single global playback device.
func OpenDevice(o DeviceOptions) (Output, error) {
	ring := o.RingFrames
	if ring == 0 {
		ring = OutputRate / 2
	}
	var name *C.char
	if o.Name != "" {
		name = C.CString(o.Name)
		defer C.free(unsafe.Pointer(name))
	}
	null := 0
	if o.Null {
		null = 1
	}
	if rc := C.mss_device_open(name, C.int(null), C.uint32_t(ring)); rc != 0 {
		return nil, fmt.Errorf("audio: open device: miniaudio result %d", int(rc))
	}
	return device{}, nil
}

func (device) Write(frames []float32) int {
	if len(frames) < 2 {
		return 0
	}
	return int(C.mss_device_write((*C.float)(unsafe.Pointer(&frames[0])), C.uint32_t(len(frames)/2)))
}

func (device) Consumed() uint64 { return uint64(C.mss_device_consumed()) }

func (device) SetPaused(p bool) {
	v := 0
	if p {
		v = 1
	}
	C.mss_device_set_paused(C.int(v))
}

func (device) SetVolume(v float32) { C.mss_device_set_volume(C.float(v)) }
func (device) Flush()              { C.mss_device_flush() }
func (device) Close() error        { C.mss_device_close(); return nil }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/audio/ && go test -race ./internal/audio/`
Expected: `ok`. The device test uses miniaudio's **null** backend, so it makes no sound.

- [ ] **Step 5: Add the opt-in listening test and the benchmark**

`internal/audio/device_manual_test.go`:

```go
package audio

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"testing"
	"time"
)

// TestRealDeviceListening plays through the real sound device so a person
// can listen for glitches. It makes sound on real speakers, so only a human
// runs it, deliberately, after turning the volume down: it is skipped
// unless MSS_DEVICE_TEST=1 (optionally MSS_ALSA_DEVICE=<name>). On a MiSTer:
//
//	MSS_DEVICE_TEST=1 ./audio.test -test.run RealDevice -test.v
//
// Everything plays at about -30 dBFS (quiet). Expect: 2 s of a clean 440 Hz
// tone, then the 44.1k/16 and 96k/24 test tones (0.5 s each), with no
// clicks, crackle or pitch shift.
func TestRealDeviceListening(t *testing.T) {
	if os.Getenv("MSS_DEVICE_TEST") != "1" {
		t.Skip("set MSS_DEVICE_TEST=1 to play through the real device")
	}
	out, err := OpenDevice(DeviceOptions{Name: os.Getenv("MSS_ALSA_DEVICE")})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	const quiet = 0.03 // about -30 dBFS
	var written uint64
	write := func(pcm []float32) {
		for len(pcm) > 0 {
			n := out.Write(pcm)
			written += uint64(n)
			pcm = pcm[n*2:]
			if len(pcm) > 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}

	tone := make([]float32, 2*OutputRate*2)
	for i := 0; i < len(tone)/2; i++ {
		v := float32(quiet * math.Sin(2*math.Pi*440*float64(i)/OutputRate))
		tone[2*i], tone[2*i+1] = v, v
	}
	write(tone)

	// The fixtures peak at -6 dBFS; a gain of 0.06 brings them to about -30.
	for _, name := range []string{"tone-44k16.flac", "tone-96k24.flac"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
		if err != nil {
			t.Fatal(err)
		}
		rs, err := NewResampler(d.SampleRate(), OutputRate, DefaultResampleQuality)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]float32, 4096*2)
		var pcm []float32
		for {
			n, err := d.Read(buf)
			for i := range buf[:n*2] {
				buf[i] *= 0.06
			}
			pcm = rs.Process(buf[:n*2], pcm[:0])
			write(pcm)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		write(rs.Flush(nil))
		rs.Close()
		d.Close()
		t.Logf("played %s (%d Hz)", name, d.SampleRate())
	}
	deadline := time.Now().Add(5 * time.Second)
	for out.Consumed() < written && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
```

`internal/audio/bench_test.go`:

```go
package audio

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// BenchmarkDecodeResample measures FLAC decode + resample to 48 kHz, the
// per-track CPU cost on the device. Run on the MiSTer via a cross-compiled
// test binary (see docs/spikes.md). It reports the fraction of one core
// needed for real-time playback as "core%".
func BenchmarkDecodeResample(b *testing.B) {
	for _, c := range []struct {
		name    string
		file    string
		quality int
	}{
		{"44k16-q3", "tone-44k16.flac", 3},
		{"44k16-q5", "tone-44k16.flac", 5},
		{"44k16-q7", "tone-44k16.flac", 7},
		{"96k24-q5", "tone-96k24.flac", 5},
	} {
		b.Run(c.name, func(b *testing.B) {
			data, err := os.ReadFile("testdata/" + c.file)
			if err != nil {
				b.Fatal(err)
			}
			buf := make([]float32, 4096*2)
			var out []float32
			var audioSeconds float64
			start := time.Now()
			for i := 0; i < b.N; i++ {
				d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
				if err != nil {
					b.Fatal(err)
				}
				rs, err := NewResampler(d.SampleRate(), OutputRate, c.quality)
				if err != nil {
					b.Fatal(err)
				}
				frames := 0
				for {
					n, err := d.Read(buf)
					out = rs.Process(buf[:n*2], out[:0])
					frames += n
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				audioSeconds += float64(frames) / float64(d.SampleRate())
				rs.Close()
				d.Close()
			}
			b.ReportMetric(100*time.Since(start).Seconds()/audioSeconds, "core%")
		})
	}
}
```

Run: `go test -count=1 ./internal/audio/ && go test -run '^$' -bench DecodeResample -benchtime 20x ./internal/audio/`

Expected:
- `TestRealDeviceListening` is **skipped**. Do not set `MSS_DEVICE_TEST` yourself.
- The benchmark prints `core%` per case. On the dev PC every case is under about 1.1%.

- [ ] **Step 6: Cross-compile tooling**

`scripts/check-glibc.sh`:

```sh
#!/bin/sh
# Fails if an ELF binary needs a glibc symbol version newer than the MiSTer's.
# usage: check-glibc.sh <binary> [max-version]
set -eu
bin=$1
max=${2:-2.31}
need=$(readelf -V "$bin" | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1)
top=$(printf '%s\n%s\n' "$need" "$max" | sort -V | tail -1)
if [ "$top" != "$max" ]; then
  echo "$bin needs glibc $need, MiSTer has $max" >&2
  exit 1
fi
echo "$bin: needs glibc $need (<= $max) ok"
```

`Makefile` (first version):

```make
GO      ?= go
ZIG     ?= zig
BIN     := bin
MISTER  ?= mister.local
DEVDIR  := /media/fat/mistersubsonic/dev
ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
           CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"

.PHONY: test vet mister-test deploy-dev vendor-check clean

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Audio test binary for on-device checks and benchmarks.
mister-test:
	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
	./scripts/check-glibc.sh $(BIN)/arm/audio.test

# Copies the audio test binary and fixtures to the MiSTer (default root password is "1").
deploy-dev: mister-test
	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
	scp $(BIN)/arm/audio.test root@$(MISTER):$(DEVDIR)/
	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/

vendor-check:
	./third_party/fetch.sh

clean:
	rm -rf $(BIN)
```

Install zig 0.16.0 if `zig version` doesn't print `0.16.0`. The distro package is fine; otherwise use the official tarball, which needs no root:

```bash
curl -fsSLO https://ziglang.org/download/0.16.0/zig-x86_64-linux-0.16.0.tar.xz
echo "70e49664a74374b48b51e6f3fdfbf437f6395d42509050588bd49abe52ba3d00  zig-x86_64-linux-0.16.0.tar.xz" | sha256sum -c
tar xf zig-x86_64-linux-0.16.0.tar.xz -C ~/.local/  # then pass ZIG=~/.local/zig-x86_64-linux-0.16.0/zig to make
```

Run: `chmod +x scripts/check-glibc.sh && make mister-test` (add `ZIG=…` if zig isn't on PATH).
Expected: `bin/arm/audio.test: needs glibc 2.29 (<= 2.31) ok`. `file bin/arm/audio.test` shows `ELF 32-bit LSB executable, ARM, EABI5`.

- [ ] **Step 7: Commit the code before the on-device spike**

```bash
git add internal/audio scripts/check-glibc.sh Makefile
git commit -m "audio: 48 kHz output device, ARM cross-build, benchmarks" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 8: Spike #2 and #3 on the MiSTer (needs the user)**

Ask the user for the MiSTer's address (`mister.local` or an IP) and whether SSH as root works; the default password is `1`. Then run `make deploy-dev MISTER=<address>`.

First, the silent checks. These run with no sound, so no confirmation is needed:

```bash
ssh root@<address> 'cd /media/fat/mistersubsonic/dev && ./audio.test -test.run "Decode|Resampler|NullDevice" -test.v 2>&1 | tail -5'
ssh root@<address> 'cd /media/fat/mistersubsonic/dev && ./audio.test -test.run "^$" -test.bench DecodeResample -test.benchtime 10x'
```

Expected: the tests pass, which proves the binary runs on the MiSTer's glibc. Record every `core%` value.

Then the listening check. **Stop and ask the user first.** Say that it plays about 10 seconds of quiet test tones through the MiSTer's audio output, and that they should turn the TV or amp volume down. Run it only after they say yes:

```bash
ssh root@<address> 'cd /media/fat/mistersubsonic/dev && MSS_DEVICE_TEST=1 ./audio.test -test.run RealDevice -test.v'
```

Ask the user what they heard: a clean tone, then two tones, and whether there were clicks, crackle or wrong pitch. If ALSA fails to open, run `ssh root@<address> 'cat /proc/asound/cards; aplay -L 2>/dev/null | head'`, retry with `MSS_ALSA_DEVICE=<name>`, and record the working name.

Append to `docs/spikes.md`:

```markdown
## Spike 2 — toolchain + ALSA on the MiSTer (<date>)
- audio.test runs on device: <yes/no>; glibc on device: <`ldd --version | head -1`>
- ALSA device that works: <default / name>
- User listening report: <clean / clicks / crackle / pitch>

## Spike 3 (audio) — Cortex-A9 decode + resample cost
| case | core% |
|---|---|
| 44k16-q3 | … |
| 44k16-q5 | … |
| 44k16-q7 | … |
| 96k24-q5 | … |

**Decision:** <…>
```

Decision rules:
- **`96k24-q5` ≤ 25%:** keep quality 5 and write "§6 confirmed".
- **Above 25%, but `q3` meets the target:** change `DefaultResampleQuality` to 3 in `resampler.go` and re-run `go test ./internal/audio/`.
- **Neither meets the target:** stop and tell the user. The spec §6 design needs revisiting.
- **Crackle is reported:** stop and tell the user. Don't try to tune it blind.

- [ ] **Step 9: Commit the spike results**

```bash
git add docs/spikes.md internal/audio/resampler.go
git commit -m "docs: record MiSTer toolchain/audio spikes" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Audio engine (gapless chaining, seek, interruption)

**Files:**
- Create: `internal/audio/engine.go`
- Test: `internal/audio/fakes_test.go`, `internal/audio/engine_test.go`

**Interfaces:**
- Consumes: `Decoder`, `OpenDecoderFunc`, `OpenDecoder`, `Resampler`, `Output`, `OutputRate` (Tasks 2–3).
- Produces:
  - `audio.Track{ID uint64; Source io.ReadSeeker; Format Format; Gain float32; Offset time.Duration}`
  - `audio.EventKind` (`EventStarted`, `EventEnded`, `EventError`), `audio.Event{Kind; TrackID uint64; Err error}`
  - `audio.ErrNotCurrent`
  - `audio.EngineOptions{Output; OpenDecoder; ResampleQuality; ChunkFrames; Poll}`
  - `audio.NewEngine(EngineOptions) *Engine` with methods `Play(Track)`, `QueueNext(Track)`, `ClearNext()`, `Stop()`, `Seek(id uint64, pos time.Duration) error`, `SetPaused(bool)`, `SetVolume(float32)`, `Position() (uint64, time.Duration, bool)`, `Events() <-chan Event`, `Close()`

**Design points** (each has a test):
- **Gapless:**
  - One decode goroutine writes the next track's samples straight after the current one's.
  - Consecutive tracks at the **same sample rate share one resampler**, so a split file renders sample-identical to the whole file.
  - A track queued *after* the current one finished decoding still starts, as soon as it opens. The smoke run found this case: short tracks, or a slow network.
- **Position mapping:** a list of `segment{start (device frame), id, base}` maps the device's `Consumed()` counter to "which track, what time". A separate **monitor goroutine** turns boundary crossings into `Started`/`Ended` events, so events keep flowing while a decode is blocked on a slow network read.
- **Interruption:** `Play`/`Stop` queue their command, then **close the source being read** so a stalled read returns at once. They never close the *new* track's source; a race here was caught by `-race -count` runs.
- **Ownership:** the engine owns each `Track.Source` and closes it if it is an `io.Closer`.

- [ ] **Step 1: Write the test fakes**

`internal/audio/fakes_test.go`:

```go
package audio

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeOutput is a ring whose consumption the test controls.
type fakeOutput struct {
	mu       sync.Mutex
	ring     int
	data     []float32 // everything ever written
	consumed uint64
	flushes  int
}

func newFakeOutput(ringFrames int) *fakeOutput { return &fakeOutput{ring: ringFrames} }

func (f *fakeOutput) Write(frames []float32) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	space := f.ring - int(uint64(len(f.data)/2)-f.consumed)
	n := min(space, len(frames)/2)
	f.data = append(f.data, frames[:n*2]...)
	return n
}

func (f *fakeOutput) Consumed() uint64  { f.mu.Lock(); defer f.mu.Unlock(); return f.consumed }
func (f *fakeOutput) SetPaused(bool)    {}
func (f *fakeOutput) SetVolume(float32) {}
func (f *fakeOutput) Close() error      { return nil }

func (f *fakeOutput) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consumed = uint64(len(f.data) / 2)
	f.flushes++
}

// consume plays out up to n frames.
func (f *fakeOutput) consume(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	avail := uint64(len(f.data)/2) - f.consumed
	f.consumed += min(uint64(n), avail)
}

func (f *fakeOutput) written() []float32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]float32(nil), f.data...)
}

// fakeSource carries PCM for fakeDecoder. If block is set, reads wait on it
// until the source is closed.
type fakeSource struct {
	pcm     []float32
	rate    int
	openErr error
	block   chan struct{}
	once    sync.Once
	closed  chan struct{}
}

func newFakeSource(pcm []float32, rate int) *fakeSource {
	return &fakeSource{pcm: pcm, rate: rate, closed: make(chan struct{})}
}

func (s *fakeSource) Read([]byte) (int, error)       { return 0, io.EOF }
func (s *fakeSource) Seek(int64, int) (int64, error) { return 0, nil }
func (s *fakeSource) Close() error                   { s.once.Do(func() { close(s.closed) }); return nil }
func (s *fakeSource) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

type fakeDecoder struct {
	src *fakeSource
	pos int
}

func fakeOpen(src io.ReadSeeker, _ Format) (Decoder, error) {
	s := src.(*fakeSource)
	if s.openErr != nil {
		return nil, s.openErr
	}
	return &fakeDecoder{src: s}, nil
}

var errClosed = errors.New("source closed")

func (d *fakeDecoder) SampleRate() int      { return d.src.rate }
func (d *fakeDecoder) LengthFrames() uint64 { return uint64(len(d.src.pcm) / 2) }
func (d *fakeDecoder) Close() error         { return nil }
func (d *fakeDecoder) SeekFrame(f uint64) error {
	d.pos = int(f) * 2
	return nil
}
func (d *fakeDecoder) Read(dst []float32) (int, error) {
	if d.src.block != nil {
		select {
		case <-d.src.block:
		case <-d.src.closed:
			return 0, errClosed
		}
	}
	if d.src.isClosed() {
		return 0, errClosed
	}
	if d.pos >= len(d.src.pcm) {
		return 0, io.EOF
	}
	n := copy(dst, d.src.pcm[d.pos:])
	d.pos += n
	return n / 2, nil
}

// ramp returns frames whose left and right samples are start+i and -(start+i), scaled.
func ramp(frames int, start float32) []float32 {
	out := make([]float32, frames*2)
	for i := 0; i < frames; i++ {
		v := (start + float32(i)) / 1e6
		out[2*i], out[2*i+1] = v, -v
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// playOut consumes output until the engine has written want frames in total.
func playOut(t *testing.T, out *fakeOutput, want int) {
	t.Helper()
	waitFor(t, "output", func() bool {
		out.consume(1 << 30)
		return len(out.written())/2 >= want
	})
	out.consume(1 << 30)
}

func nextEvent(t *testing.T, e *Engine) Event {
	t.Helper()
	select {
	case ev := <-e.Events():
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for engine event")
	}
	return Event{}
}

func expectEvent(t *testing.T, e *Engine, kind EventKind, id uint64) Event {
	t.Helper()
	ev := nextEvent(t, e)
	if ev.Kind != kind || ev.TrackID != id {
		t.Fatalf("event = %+v, want kind %d track %d", ev, kind, id)
	}
	return ev
}

func noEvent(t *testing.T, e *Engine, d time.Duration) {
	t.Helper()
	select {
	case ev := <-e.Events():
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(d):
	}
}
```

- [ ] **Step 2: Write the failing tests**

`internal/audio/engine_test.go`:

```go
package audio

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func newTestEngine(out *fakeOutput) *Engine {
	return NewEngine(EngineOptions{Output: out, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
}

func TestEnginePlaysTrackWithGainThenEnds(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	pcm := ramp(1000, 0)
	e.Play(Track{ID: 1, Source: newFakeSource(pcm, OutputRate), Gain: 0.5})
	expectEvent(t, e, EventStarted, 1)
	playOut(t, out, 1000)
	expectEvent(t, e, EventEnded, 1)

	got := out.written()
	if len(got) != len(pcm) {
		t.Fatalf("wrote %d samples, want %d", len(got), len(pcm))
	}
	for i := range pcm {
		if got[i] != pcm[i]*0.5 {
			t.Fatalf("sample %d = %v, want %v", i, got[i], pcm[i]*0.5)
		}
	}
}

func TestEngineGaplessAt48k(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	a, b := ramp(700, 0), ramp(900, 700)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, OutputRate)})
	expectEvent(t, e, EventStarted, 1)

	waitFor(t, "both tracks written", func() bool { return len(out.written())/2 == 1600 })
	out.consume(699)
	noEvent(t, e, 30*time.Millisecond)
	out.consume(1)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)

	want := append(append([]float32(nil), a...), b...)
	got := out.written()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %v, want %v (gap or overlap at the boundary)", i, got[i], want[i])
		}
	}
	out.consume(900)
	expectEvent(t, e, EventEnded, 2)
}

// Two halves of a 44.1k file, played gaplessly through the resampler, must
// produce exactly the same 48k output as the whole file.
func TestEngineGaplessResampledMatchesWholeFile(t *testing.T) {
	read := func(name string) *bytes.Reader {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.NewReader(b)
	}
	render := func(tracks ...string) []float32 {
		out := newFakeOutput(1 << 20)
		e := NewEngine(EngineOptions{Output: out, Poll: time.Millisecond})
		defer e.Close()
		e.Play(Track{ID: 1, Source: read(tracks[0]), Format: FormatFLAC})
		for i, name := range tracks[1:] {
			e.QueueNext(Track{ID: uint64(i + 2), Source: read(name), Format: FormatFLAC})
		}
		last := uint64(len(tracks))
		deadline := time.After(3 * time.Second)
		for {
			out.consume(1 << 30)
			select {
			case ev := <-e.Events():
				if ev.Kind == EventError {
					t.Fatalf("engine error: %v", ev.Err)
				}
				if ev.Kind == EventEnded && ev.TrackID == last {
					return out.written()
				}
			case <-time.After(time.Millisecond):
			case <-deadline:
				t.Fatal("timed out rendering")
			}
		}
	}
	whole := render("tone-44k16.flac")
	split := render("half-a.flac", "half-b.flac")
	if len(whole) != len(split) {
		t.Fatalf("whole=%d samples split=%d samples", len(whole), len(split))
	}
	for i := range whole {
		if whole[i] != split[i] {
			t.Fatalf("sample %d differs: whole=%v split=%v", i, whole[i], split[i])
		}
	}
}

func TestEngineSeekFlushesAndContinuesFromTarget(t *testing.T) {
	out := newFakeOutput(1000)
	e := newTestEngine(out)
	defer e.Close()

	pcm := ramp(48000, 0)
	e.Play(Track{ID: 7, Source: newFakeSource(pcm, OutputRate), Offset: 2 * time.Second})
	expectEvent(t, e, EventStarted, 7)
	waitFor(t, "ring full", func() bool { return len(out.written())/2 == 1000 })

	if err := e.Seek(7, 2*time.Second+500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if out.flushes < 2 { // one from Play, one from Seek
		t.Fatalf("Seek did not flush the output (flushes=%d)", out.flushes)
	}
	id, pos, ok := e.Position()
	if !ok || id != 7 || pos != 2*time.Second+500*time.Millisecond {
		t.Fatalf("Position after seek = %d %v %v", id, pos, ok)
	}
	waitFor(t, "post-seek data", func() bool { return len(out.written())/2 >= 1480 })
	got := out.written()
	if got[1000*2] != pcm[24000*2] {
		t.Fatalf("first frame after seek = %v, want frame 24000 (%v)", got[1000*2], pcm[24000*2])
	}
	out.consume(480)
	if _, pos, _ := e.Position(); pos != 2*time.Second+510*time.Millisecond {
		t.Fatalf("Position after 480 frames = %v, want 2.51s", pos)
	}
	noEvent(t, e, 20*time.Millisecond) // a seek is not a new track
}

func TestEnginePlayInterruptsStalledRead(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	stalled := newFakeSource(ramp(1000, 0), OutputRate)
	stalled.block = make(chan struct{}) // never released
	e.Play(Track{ID: 1, Source: stalled})
	expectEvent(t, e, EventStarted, 1)

	e.Play(Track{ID: 2, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 2) // no EventError for track 1
	if !stalled.isClosed() {
		t.Fatal("stalled source was not closed")
	}
}

func TestEngineQueuedOpenFailureReportsError(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	bad := newFakeSource(nil, OutputRate)
	bad.openErr = errBoom
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(500, 0), OutputRate)})
	e.QueueNext(Track{ID: 2, Source: bad})
	// Started(1) and Error(2) are independent; either may come first.
	var sawStart, sawErr bool
	for !sawStart || !sawErr {
		ev := nextEvent(t, e)
		switch {
		case ev.Kind == EventStarted && ev.TrackID == 1:
			sawStart = true
		case ev.Kind == EventError && ev.TrackID == 2 && ev.Err == errBoom:
			sawErr = true
		default:
			t.Fatalf("unexpected event %+v", ev)
		}
	}
	playOut(t, out, 500)
	expectEvent(t, e, EventEnded, 1)
}

func TestEngineSeekWrongTrack(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(100000, 0), OutputRate)})
	if err := e.Seek(99, time.Second); err != ErrNotCurrent {
		t.Fatalf("err = %v, want ErrNotCurrent", err)
	}
}

func TestEngineStopSilences(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	src := newFakeSource(ramp(100000, 0), OutputRate)
	e.Play(Track{ID: 1, Source: src})
	expectEvent(t, e, EventStarted, 1)
	e.Stop()
	waitFor(t, "source closed", src.isClosed)
	waitFor(t, "position cleared", func() bool { _, _, ok := e.Position(); return !ok })
}

func TestEngineQueueNextAfterDecodeFinishedStillPlays(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()

	a, b := ramp(300, 0), ramp(400, 300)
	e.Play(Track{ID: 1, Source: newFakeSource(a, OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	waitFor(t, "a decoded", func() bool { return len(out.written())/2 == 300 })

	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, OutputRate)}) // late, but a is still buffered
	waitFor(t, "b appended", func() bool { return len(out.written())/2 == 700 })
	out.consume(300)
	expectEvent(t, e, EventEnded, 1)
	expectEvent(t, e, EventStarted, 2)
	got := out.written()
	for i := range b {
		if got[600+i] != b[i] {
			t.Fatalf("b sample %d = %v, want %v", i, got[600+i], b[i])
		}
	}

	// Even after everything has played out, a queued track still starts.
	out.consume(400)
	expectEvent(t, e, EventEnded, 2)
	e.QueueNext(Track{ID: 3, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 3)
}

func TestEngineQueueNextAfterStopDoesNotPlay(t *testing.T) {
	out := newFakeOutput(100000)
	e := newTestEngine(out)
	defer e.Close()
	e.Play(Track{ID: 1, Source: newFakeSource(ramp(100, 0), OutputRate)})
	expectEvent(t, e, EventStarted, 1)
	e.Stop()
	e.QueueNext(Track{ID: 2, Source: newFakeSource(ramp(100, 0), OutputRate)})
	noEvent(t, e, 50*time.Millisecond)
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/audio/`
Expected: build failure, `undefined: Engine`, `undefined: Track`, `undefined: EngineOptions`, ….

- [ ] **Step 4: Implement the engine**

`internal/audio/engine.go`:

```go
package audio

import (
	"errors"
	"io"
	"sync"
	"time"
)

// Track is one thing for the engine to play.
type Track struct {
	// ID is the caller's token; it comes back in Events and Position. Must be non-zero.
	ID uint64
	// Source is closed by the engine when it is done with it (if it is an io.Closer).
	Source io.ReadSeeker
	Format Format
	// Gain is a linear multiplier (ReplayGain). 0 means unity.
	Gain float32
	// Offset is the song position of Source's first frame (non-zero for
	// transcoded streams reopened with timeOffset).
	Offset time.Duration
}

type EventKind int

const (
	// EventStarted: the track's first frame became audible.
	EventStarted EventKind = iota + 1
	// EventEnded: the track's last frame was played out.
	EventEnded
	// EventError: the track failed to open or decode. For the current track
	// the engine then moves on as if it had ended.
	EventError
)

type Event struct {
	Kind    EventKind
	TrackID uint64
	Err     error
}

// ErrNotCurrent is returned by Seek when the track is not the one being decoded.
var ErrNotCurrent = errors.New("audio: track is not the one being decoded")

type EngineOptions struct {
	Output          Output
	OpenDecoder     OpenDecoderFunc // default OpenDecoder
	ResampleQuality int             // default DefaultResampleQuality
	ChunkFrames     int             // default 2048
	Poll            time.Duration   // default 5 ms
}

type voice struct {
	t    Track
	dec  Decoder
	gain float32
}

// segment marks where, in the device's frame count, a stretch of one track
// begins. id 0 means silence.
type segment struct {
	start uint64
	id    uint64
	base  time.Duration
}

type openResult struct {
	gen int
	v   *voice
	err error
	t   Track
}

// Engine decodes tracks into an Output. Decoder state is owned by the run
// goroutine and public methods post commands to it. A separate monitor
// goroutine turns device progress into Started/Ended events, so events keep
// flowing even while a decode is blocked on a slow network read.
type Engine struct {
	o      EngineOptions
	cmds   chan func()
	events chan Event
	openCh chan openResult
	quit   chan struct{}
	done   chan struct{}

	mu     sync.Mutex
	segs   []segment // guarded by mu
	evq    []Event   // guarded by mu
	busy   io.Closer // guarded by mu: source of the voice being decoded
	closed bool      // guarded by mu

	// Owned by the run goroutine.
	cur      *voice
	ended    bool // decoding reached the end of the queue naturally (not Stop)
	next     *voice
	opening  *Track
	gen      int
	rs       *Resampler
	chainOut uint64
	chainIn  uint64
	written  uint64
	pending  []float32
	scratch  []float32
}

func NewEngine(o EngineOptions) *Engine {
	if o.OpenDecoder == nil {
		o.OpenDecoder = OpenDecoder
	}
	if o.ResampleQuality == 0 {
		o.ResampleQuality = DefaultResampleQuality
	}
	if o.ChunkFrames == 0 {
		o.ChunkFrames = 2048
	}
	if o.Poll == 0 {
		o.Poll = 5 * time.Millisecond
	}
	e := &Engine{
		o:       o,
		cmds:    make(chan func(), 64),
		events:  make(chan Event, 256),
		openCh:  make(chan openResult, 4),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
		segs:    []segment{{start: 0, id: 0}},
		scratch: make([]float32, o.ChunkFrames*2),
	}
	go e.run()
	go e.monitor()
	return e
}

func (e *Engine) Events() <-chan Event { return e.events }

// Play stops whatever is playing and starts t immediately.
func (e *Engine) Play(t Track) {
	e.send(func() { e.doPlay(t) })
	e.interrupt(t.Source)
}

// QueueNext sets the track to continue with, gaplessly, after the current
// one. It replaces any previously queued track. If the current track has
// already finished decoding (or finished playing), the queued track starts
// as soon as it is open.
func (e *Engine) QueueNext(t Track) { e.send(func() { e.doQueueNext(t) }) }

// ClearNext drops the queued track.
func (e *Engine) ClearNext() { e.send(e.cancelNext) }

// Stop halts playback and discards everything buffered.
func (e *Engine) Stop() {
	e.send(e.doStop)
	e.interrupt(nil)
}

// Seek moves the track being decoded to pos (song time).
func (e *Engine) Seek(id uint64, pos time.Duration) error {
	reply := make(chan error, 1)
	if !e.send(func() { reply <- e.doSeek(id, pos) }) {
		return errors.New("audio: engine closed")
	}
	select {
	case err := <-reply:
		return err
	case <-e.done:
		return errors.New("audio: engine closed")
	}
}

func (e *Engine) SetPaused(p bool)    { e.o.Output.SetPaused(p) }
func (e *Engine) SetVolume(v float32) { e.o.Output.SetVolume(v) }

// Position reports which track is audible and where in it.
func (e *Engine) Position() (id uint64, pos time.Duration, ok bool) {
	c := e.o.Output.Consumed()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.segs) - 1; i >= 0; i-- {
		s := e.segs[i]
		if c >= s.start {
			if s.id == 0 {
				return 0, 0, false
			}
			return s.id, s.base + time.Duration(c-s.start)*time.Second/OutputRate, true
		}
	}
	return 0, 0, false
}

// Close stops the engine goroutine. The Output is not closed.
func (e *Engine) Close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	e.mu.Unlock()
	e.interrupt(nil)
	close(e.quit)
	<-e.done
}

func (e *Engine) send(f func()) bool {
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed {
		return false
	}
	select {
	case e.cmds <- f:
		return true
	case <-e.done:
		return false
	}
}

// interrupt closes the source currently being read so a stalled network
// read can't hold up Play/Stop. The command is always queued first, so the
// run loop sees it before it reacts to the read error. keep is the new
// track's source: if the run loop already picked the command up, busy is
// that source and must be left alone.
func (e *Engine) interrupt(keep io.ReadSeeker) {
	kc, _ := keep.(io.Closer)
	e.mu.Lock()
	c := e.busy
	if c == nil || (kc != nil && c == kc) {
		e.mu.Unlock()
		return
	}
	e.busy = nil
	e.mu.Unlock()
	c.Close()
}

func (e *Engine) setBusy(src io.ReadSeeker) {
	c, _ := src.(io.Closer)
	e.mu.Lock()
	e.busy = c
	e.mu.Unlock()
}

func (e *Engine) run() {
	defer close(e.done)
	defer e.doStop()
	for {
		if !e.poll() {
			return
		}
		switch {
		case len(e.pending) > 0:
			n := e.o.Output.Write(e.pending)
			e.written += uint64(n)
			e.pending = e.pending[n*2:]
			if len(e.pending) > 0 && !e.wait(e.o.Poll) {
				return
			}
		case e.cur != nil:
			e.decodeChunk()
		default:
			if !e.wait(4 * e.o.Poll) {
				return
			}
		}
	}
}

// poll runs queued commands and open results without blocking.
func (e *Engine) poll() bool {
	for {
		select {
		case f := <-e.cmds:
			f()
		case r := <-e.openCh:
			e.onOpened(r)
		case <-e.quit:
			return false
		default:
			return true
		}
	}
}

func (e *Engine) wait(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case f := <-e.cmds:
		f()
	case r := <-e.openCh:
		e.onOpened(r)
	case <-e.quit:
		return false
	case <-t.C:
	}
	return true
}

func (e *Engine) queueEvent(ev Event) {
	e.mu.Lock()
	e.evq = append(e.evq, ev)
	e.mu.Unlock()
}

func (e *Engine) monitor() {
	t := time.NewTicker(e.o.Poll)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-e.done:
			return
		}
		e.checkBoundaries()
		e.mu.Lock()
		evs := e.evq
		e.evq = nil
		e.mu.Unlock()
		for _, ev := range evs {
			select {
			case e.events <- ev:
			case <-e.done:
				return
			}
		}
	}
}

func (e *Engine) checkBoundaries() {
	c := e.o.Output.Consumed()
	e.mu.Lock()
	i := 0
	for i+1 < len(e.segs) && c >= e.segs[i+1].start {
		prev, nxt := e.segs[i], e.segs[i+1]
		if nxt.id != prev.id {
			if prev.id != 0 {
				e.evq = append(e.evq, Event{Kind: EventEnded, TrackID: prev.id})
			}
			if nxt.id != 0 {
				e.evq = append(e.evq, Event{Kind: EventStarted, TrackID: nxt.id})
			}
		}
		i++
	}
	e.segs = e.segs[i:]
	e.mu.Unlock()
}

func (e *Engine) addSegment(s segment) {
	e.mu.Lock()
	e.segs = append(e.segs, s)
	e.mu.Unlock()
}

func (e *Engine) resetSegments(s segment) {
	e.mu.Lock()
	e.segs = []segment{s}
	e.mu.Unlock()
}

func (e *Engine) decodeChunk() {
	v := e.cur
	n, err := v.dec.Read(e.scratch)
	if n > 0 {
		e.emit(e.scratch[:n*2], v)
	}
	if err == nil {
		return
	}
	if !e.poll() {
		return
	}
	if e.cur != v {
		return // a Play/Stop took over; the error came from the interrupted source
	}
	if !errors.Is(err, io.EOF) {
		e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
	}
	e.finishCur()
}

func (e *Engine) emit(samples []float32, v *voice) {
	if v.gain != 1 {
		for i := range samples {
			samples[i] *= v.gain
		}
	}
	if len(e.pending) == 0 {
		e.pending = e.pending[:0:0]
	}
	if e.rs != nil {
		e.pending = e.rs.Process(samples, e.pending)
		e.chainIn += uint64(len(samples) / 2)
	} else {
		e.pending = append(e.pending, samples...)
	}
}

func (e *Engine) finishCur() {
	closeVoice(e.cur)
	e.cur = nil
	e.setBusy(nil)
	for e.next == nil && e.opening != nil && e.cur == nil {
		if !e.wait(e.o.Poll) {
			return
		}
	}
	if e.cur != nil {
		return // a Play arrived while we waited
	}
	if e.next != nil {
		v := e.next
		e.next = nil
		e.startVoice(v, true)
		return
	}
	e.breakChain()
	e.addSegment(segment{start: e.written + uint64(len(e.pending)/2), id: 0})
	e.ended = true
}

// breakChain flushes the resampler tail into pending and drops the resampler.
func (e *Engine) breakChain() {
	if e.rs != nil {
		e.pending = e.rs.Flush(e.pending)
		e.rs.Close()
		e.rs = nil
	}
}

func (e *Engine) startVoice(v *voice, gapless bool) {
	rate := v.dec.SampleRate()
	var start uint64
	if gapless && e.rs != nil && e.rs.InRate() == rate {
		start = e.chainOut + e.chainIn*OutputRate/uint64(rate)
	} else {
		e.breakChain()
		start = e.written + uint64(len(e.pending)/2)
		if rate != OutputRate {
			rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
			if err != nil {
				e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
				closeVoice(v)
				e.addSegment(segment{start: start, id: 0})
				return
			}
			e.rs, e.chainOut, e.chainIn = rs, start, 0
		}
	}
	e.cur = v
	e.ended = false
	e.setBusy(v.t.Source)
	e.addSegment(segment{start: start, id: v.t.ID, base: v.t.Offset})
}

func (e *Engine) openVoice(t Track) (*voice, error) {
	dec, err := e.o.OpenDecoder(t.Source, t.Format)
	if err != nil {
		closeSource(t.Source)
		return nil, err
	}
	g := t.Gain
	if g == 0 {
		g = 1
	}
	return &voice{t: t, dec: dec, gain: g}, nil
}

func (e *Engine) doPlay(t Track) {
	e.doStop()
	e.setBusy(t.Source)
	v, err := e.openVoice(t)
	e.setBusy(nil)
	if err != nil {
		e.queueEvent(Event{Kind: EventError, TrackID: t.ID, Err: err})
		return
	}
	e.startVoice(v, false)
}

func (e *Engine) doStop() {
	closeVoice(e.cur)
	e.cur = nil
	e.ended = false
	e.setBusy(nil)
	e.cancelNext()
	if e.rs != nil {
		e.rs.Close()
		e.rs = nil
	}
	e.o.Output.Flush()
	e.pending = e.pending[:0]
	e.resetSegments(segment{start: e.written, id: 0})
}

func (e *Engine) doQueueNext(t Track) {
	e.cancelNext()
	e.gen++
	e.opening = &t
	gen := e.gen
	go func() {
		v, err := e.openVoice(t)
		e.openCh <- openResult{gen: gen, v: v, err: err, t: t}
	}()
}

func (e *Engine) cancelNext() {
	if e.next != nil {
		closeVoice(e.next)
		e.next = nil
	}
	if e.opening != nil {
		closeSource(e.opening.Source) // unblocks the opener; its result is dropped as stale
		e.opening = nil
		e.gen++
	}
}

func (e *Engine) onOpened(r openResult) {
	if r.gen != e.gen {
		closeVoice(r.v)
		return
	}
	e.opening = nil
	if r.err != nil {
		e.queueEvent(Event{Kind: EventError, TrackID: r.t.ID, Err: r.err})
		return
	}
	e.next = r.v
	if e.cur == nil && e.ended {
		// The current track finished decoding before its successor was
		// queued: start the successor right after whatever is still buffered.
		v := e.next
		e.next = nil
		e.startVoice(v, true)
	}
}

func (e *Engine) doSeek(id uint64, pos time.Duration) error {
	v := e.cur
	if v == nil || v.t.ID != id {
		return ErrNotCurrent
	}
	rel := pos - v.t.Offset
	if rel < 0 {
		rel = 0
	}
	rate := v.dec.SampleRate()
	if err := v.dec.SeekFrame(uint64(rel.Seconds() * float64(rate))); err != nil {
		return err
	}
	e.o.Output.Flush()
	e.pending = e.pending[:0]
	if e.rs != nil {
		e.rs.Close()
		e.rs = nil
		rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
		if err != nil {
			return err
		}
		e.rs, e.chainOut, e.chainIn = rs, e.written, 0
	}
	e.resetSegments(segment{start: e.written, id: id, base: pos})
	return nil
}

func closeVoice(v *voice) {
	if v == nil {
		return
	}
	v.dec.Close()
	closeSource(v.t.Source)
}

func closeSource(s io.ReadSeeker) {
	if c, ok := s.(io.Closer); ok {
		c.Close()
	}
}
```

- [ ] **Step 5: Run the tests, repeatedly, under the race detector**

Run: `go vet ./internal/audio/ && go test -race -count=30 ./internal/audio/`
Expected: `ok`. Use `-count=30`: two real bugs in this engine only showed up across repeated race runs. Any failure means a real bug. Investigate it, and don't raise timeouts.

- [ ] **Step 6: Commit**

```bash
git add internal/audio
git commit -m "audio: gapless engine with seek, interruption and position events" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: Windowed HTTP Range stream reader

**Files:**
- Create: `internal/stream/reader.go`
- Test: `internal/stream/reader_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (standard library only).
- Produces:
  - `stream.Options{Client *http.Client; Header http.Header; CheckResponse func(*http.Response) error; WindowBytes, BehindBytes, NearBytes, PrefetchBytes int64; StallTimeout time.Duration; Backoff []time.Duration; RetryBudget time.Duration}`
  - `stream.Open(ctx, url, Options) (*Reader, error)`
  - `*Reader` methods: `Read`, `Seek` (`io.ReadSeeker`), `Close`, `Size() int64` (−1 if unknown), `Seekable() bool`, `Buffered() int64`, `Promote()`
  - `stream.ErrClosed`, `*stream.HTTPError{StatusCode, Status}`

**Behaviour** (spec §5):
- **Window:** a ring of `WindowBytes`, keeping `BehindBytes` behind the read position.
- **Seeks:** a seek inside the window, or up to `NearBytes` ahead of it, is served from memory. Outside the window it reopens with `Range: bytes=N-`.
- **Servers without ranges:** a `200` without `Accept-Ranges` is sequential-only. A forward seek streams through; a backward seek replays from 0.
- **Recovery:** stalls (no bytes for `StallTimeout`) and dropped connections reconnect from the last byte received, with backoff. The reader gives up after `RetryBudget` without progress. `4xx` is terminal.
- **Prefetch:** `PrefetchBytes` caps the fetch until `Promote()`.
- **No secrets in errors:** error text never contains the URL, since stream URLs carry credentials.

- [ ] **Step 1: Write the failing tests**

`internal/stream/reader_test.go`:

```go
package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pattern is the byte at offset off of every virtual test file.
func pattern(off int64) byte { return byte(off*31 + off>>13) }

// virtualFile is an io.ReadSeeker over a file of any size that is never stored.
type virtualFile struct{ size, pos int64 }

func (v *virtualFile) Read(p []byte) (int, error) {
	if v.pos >= v.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), v.size-v.pos))
	for i := 0; i < n; i++ {
		p[i] = pattern(v.pos + int64(i))
	}
	v.pos += int64(n)
	return n, nil
}

func (v *virtualFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		v.pos = off
	case io.SeekCurrent:
		v.pos += off
	case io.SeekEnd:
		v.pos = v.size + off
	}
	return v.pos, nil
}

type server struct {
	*httptest.Server
	mu       sync.Mutex
	ranges   []string
	requests atomic.Int32
}

func (s *server) rangeHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

// newServer serves a virtual file. handler may override behaviour per request
// (1-based request number); returning false falls through to ServeContent.
func newServer(t *testing.T, size int64, handler func(n int, w http.ResponseWriter, r *http.Request) bool) *server {
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.requests.Add(1))
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		s.mu.Unlock()
		if handler != nil && handler(n, w, r) {
			return
		}
		w.Header().Set("Content-Type", "audio/flac")
		http.ServeContent(w, r, "", time.Time{}, &virtualFile{size: size})
	}))
	t.Cleanup(s.Close)
	return s
}

// serveNoRanges writes the virtual file from offset 0 as a plain 200 with no range support.
func serveNoRanges(size int64) func(int, http.ResponseWriter, *http.Request) bool {
	return func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, &virtualFile{size: size})
		return true
	}
}

func testOptions() Options {
	return Options{
		WindowBytes:  1 << 20,
		StallTimeout: 200 * time.Millisecond,
		Backoff:      []time.Duration{10 * time.Millisecond},
		RetryBudget:  500 * time.Millisecond,
	}
}

func open(t *testing.T, url string, o Options) *Reader {
	t.Helper()
	r, err := Open(context.Background(), url, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func checkBytes(t *testing.T, got []byte, off int64) {
	t.Helper()
	for i, b := range got {
		if b != pattern(off+int64(i)) {
			t.Fatalf("byte at offset %d = %d, want %d", off+int64(i), b, pattern(off+int64(i)))
		}
	}
}

func readAt(t *testing.T, r *Reader, off int64, n int) []byte {
	t.Helper()
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read %d bytes at %d: %v", n, off, err)
	}
	return buf
}

func TestSequentialReadLargerThanWindow(t *testing.T) {
	const size = 10 << 20
	s := newServer(t, size, nil)
	r := open(t, s.URL, testOptions())
	if !r.Seekable() || r.Size() != size {
		t.Fatalf("Seekable=%v Size=%d", r.Seekable(), r.Size())
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("read %d bytes, want %d", len(got), size)
	}
	checkBytes(t, got, 0)
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("sequential read used %d requests, want 1", n)
	}
}

func TestSeekInsideWindowUsesNoNewRequest(t *testing.T) {
	s := newServer(t, 4<<20, nil)
	r := open(t, s.URL, testOptions())
	checkBytes(t, readAt(t, r, 0, 200<<10), 0)
	checkBytes(t, readAt(t, r, 100<<10, 50<<10), 100<<10) // back-seek within BehindBytes
	checkBytes(t, readAt(t, r, 300<<10, 10), 300<<10)     // forward, within Near
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("seeks inside the window made %d requests, want 1", n)
	}
}

func TestSeekFarIntoHugeFileUsesRange(t *testing.T) {
	const size = 3 << 30 // 3 GiB, never materialised
	s := newServer(t, size, nil)
	r := open(t, s.URL, testOptions())
	const target = int64(2)<<30 + 12345
	checkBytes(t, readAt(t, r, target, 4096), target)
	hs := s.rangeHeaders()
	if len(hs) != 2 || hs[1] != "bytes="+strconv.FormatInt(target, 10)+"-" {
		t.Fatalf("Range headers = %q", hs)
	}
	checkBytes(t, readAt(t, r, 1000, 10), 1000) // and back to the start
}

func TestSeekEndAndPastEnd(t *testing.T) {
	s := newServer(t, 1<<20, nil)
	r := open(t, s.URL, testOptions())
	pos, err := r.Seek(-10, io.SeekEnd)
	if err != nil || pos != 1<<20-10 {
		t.Fatalf("SeekEnd = %d, %v", pos, err)
	}
	got, err := io.ReadAll(r)
	if err != nil || len(got) != 10 {
		t.Fatalf("tail read %d bytes, %v", len(got), err)
	}
	checkBytes(t, got, 1<<20-10)
	r.Seek(5<<20, io.SeekStart)
	if n, err := r.Read(make([]byte, 10)); n != 0 || err != io.EOF {
		t.Fatalf("read past end = %d, %v; want 0, EOF", n, err)
	}
}

func TestServerWithoutRanges(t *testing.T) {
	const size = 4 << 20
	s := newServer(t, size, serveNoRanges(size))
	r := open(t, s.URL, testOptions())
	if r.Seekable() {
		t.Fatal("Seekable() = true for a server without range support")
	}
	checkBytes(t, readAt(t, r, 3<<20, 1000), 3<<20) // forward: streams through
	checkBytes(t, readAt(t, r, 10, 1000), 10)       // backward: replays from 0
	if n := s.requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
}

func TestReconnectAfterDroppedConnection(t *testing.T) {
	const size = 2 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyN(w, &virtualFile{size: size}, 300<<10)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // drop the connection mid-body
	})
	r := open(t, s.URL, testOptions())
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("read %d bytes, want %d", len(got), size)
	}
	checkBytes(t, got, 0)
	hs := s.rangeHeaders()
	if len(hs) < 2 || hs[1] == "bytes=0-" {
		t.Fatalf("reconnect did not resume from the last byte: %q", hs)
	}
}

func TestReconnectAfterStall(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(size-1)+"/"+strconv.Itoa(size))
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.WriteHeader(http.StatusPartialContent)
		io.CopyN(w, &virtualFile{size: size}, 100<<10)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // go silent until the client gives up
		return true
	})
	r := open(t, s.URL, testOptions())
	start := time.Now()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	checkBytes(t, got, 0)
	if len(got) != size {
		t.Fatalf("read %d bytes", len(got))
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("stall recovery took %v", time.Since(start))
	}
}

func TestGivesUpAfterRetryBudget(t *testing.T) {
	const size = 1 << 20
	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(size))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			io.CopyN(w, &virtualFile{size: size}, 1000)
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	r := open(t, s.URL, testOptions())
	got, err := io.ReadAll(r)
	if err == nil {
		t.Fatal("ReadAll succeeded against a dead server")
	}
	if len(got) != 1000 {
		t.Fatalf("got %d bytes before the error, want the 1000 that arrived", len(got))
	}
	checkBytes(t, got, 0)
}

func TestOpenReportsHTTPError(t *testing.T) {
	s := newServer(t, 10, func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		http.NotFound(w, nil)
		return true
	})
	_, err := Open(context.Background(), s.URL, testOptions())
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 404 {
		t.Fatalf("err = %v, want HTTPError 404", err)
	}
}

func TestCheckResponseRejects(t *testing.T) {
	s := newServer(t, 10, func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
		return true
	})
	o := testOptions()
	bad := errors.New("error document")
	o.CheckResponse = func(resp *http.Response) error {
		if resp.Header.Get("Content-Type") == "application/json" {
			return bad
		}
		return nil
	}
	if _, err := Open(context.Background(), s.URL, o); !errors.Is(err, bad) {
		t.Fatalf("err = %v, want the CheckResponse error", err)
	}
}

func TestPrefetchLimitThenPromote(t *testing.T) {
	s := newServer(t, 8<<20, nil)
	o := testOptions()
	o.PrefetchBytes = 256 << 10
	r := open(t, s.URL, o)
	time.Sleep(100 * time.Millisecond)
	if b := r.Buffered(); b > 256<<10 {
		t.Fatalf("buffered %d bytes in prefetch mode, limit is %d", b, 256<<10)
	}
	r.Promote()
	deadline := time.Now().Add(2 * time.Second)
	for r.Buffered() <= 256<<10 {
		if time.Now().After(deadline) {
			t.Fatalf("buffer did not grow after Promote (buffered %d)", r.Buffered())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	s := newServer(t, 1<<20, func(_ int, w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		return true
	})
	o := testOptions()
	o.StallTimeout = time.Minute
	r := open(t, s.URL, o)
	errc := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 10))
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	r.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Read after Close = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
}

func TestWindowBoundsMemory(t *testing.T) {
	s := newServer(t, 64<<20, nil)
	r := open(t, s.URL, testOptions())
	time.Sleep(100 * time.Millisecond) // let it fill
	if b := r.Buffered(); b > 1<<20 {
		t.Fatalf("buffered %d bytes, window is %d", b, 1<<20)
	}
	// Reading still works across many window turnovers.
	buf := make([]byte, 5<<20)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	checkBytes(t, buf, 0)
}

func TestErrorsDoNotIncludeURL(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	_, err := Open(context.Background(), "http://"+addr+"/rest/stream?t=SECRET&s=SALT", testOptions())
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v; must exist and must not contain the URL's credentials", err)
	}
}

// While paused the ring stays full and nothing is read from the body; that
// must not count as a stall or kill the stream.
func TestLongPauseDoesNotBreakStream(t *testing.T) {
	const size = 4 << 20
	s := newServer(t, size, nil)
	o := testOptions() // StallTimeout 200 ms
	r := open(t, s.URL, o)
	checkBytes(t, readAt(t, r, 0, 1000), 0)
	time.Sleep(4 * o.StallTimeout) // "paused"
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size-1000 {
		t.Fatalf("read %d bytes after pause, want %d", len(got), size-1000)
	}
	checkBytes(t, got, 1000)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/stream/`
Expected: build failure, `undefined: Options`, `undefined: Open`, `undefined: Reader`, ….

- [ ] **Step 3: Implement the reader**

`internal/stream/reader.go`:

```go
// Package stream provides a seekable, memory-bounded reader over an HTTP
// resource. It keeps a sliding window of the file in RAM, fetches ahead with
// Range requests, reconnects on stalls and drops, and re-requests at the
// target offset when a seek leaves the window.
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Client *http.Client // default http.DefaultClient
	Header http.Header  // extra request headers
	// CheckResponse can reject a response (e.g. an error document served
	// with status 200). It must not read the body unless it returns an error.
	CheckResponse func(*http.Response) error

	WindowBytes   int64           // RAM per reader; default 32 MiB
	BehindBytes   int64           // kept behind the read position; default WindowBytes/4
	NearBytes     int64           // forward seeks within this distance of the window just wait; default 256 KiB
	PrefetchBytes int64           // if > 0, stop fetching at this offset until Promote
	StallTimeout  time.Duration   // no bytes for this long -> reconnect; default 10 s
	Backoff       []time.Duration // retry delays; default 0.5, 1, 2, 4, 8 s (last repeats)
	RetryBudget   time.Duration   // give up after this long without progress; default 30 s
}

var (
	ErrClosed  = errors.New("stream: reader closed")
	errStale   = errors.New("stream: stale fetch")
	errStalled = errors.New("stream: stalled")
)

// HTTPError is a non-success HTTP status.
type HTTPError struct {
	StatusCode int
	Status     string
}

func (e *HTTPError) Error() string { return "stream: HTTP " + e.Status }

type Reader struct {
	url string
	o   Options

	mu       sync.Mutex
	cond     *sync.Cond
	ring     []byte
	lo, hi   int64 // file offsets held in ring: [lo, hi)
	pos      int64
	size     int64 // -1 when unknown
	seekable bool
	eof      bool
	err      error
	closed   bool
	prefetch int64
	gen      int
	cancel   context.CancelFunc
}

// Open issues the first request and returns once response headers arrive.
// ctx bounds only that first request; the reader lives until Close.
func Open(ctx context.Context, url string, o Options) (*Reader, error) {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.WindowBytes <= 0 {
		o.WindowBytes = 32 << 20
	}
	if o.BehindBytes <= 0 || o.BehindBytes >= o.WindowBytes {
		o.BehindBytes = o.WindowBytes / 4
	}
	if o.NearBytes <= 0 {
		o.NearBytes = 256 << 10
	}
	if o.StallTimeout <= 0 {
		o.StallTimeout = 10 * time.Second
	}
	if len(o.Backoff) == 0 {
		o.Backoff = []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	}
	if o.RetryBudget <= 0 {
		o.RetryBudget = 30 * time.Second
	}
	r := &Reader{url: url, o: o, ring: make([]byte, o.WindowBytes), size: -1, prefetch: o.PrefetchBytes}
	r.cond = sync.NewCond(&r.mu)

	fctx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	resp, reqCancel, err := r.request(fctx, 0)
	stop()
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	r.seekable = resp.StatusCode == http.StatusPartialContent || strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
	r.size = responseSize(resp)
	r.cancel = cancel
	go r.fetch(fctx, r.gen, 0, resp, reqCancel)
	return r, nil
}

// Size is the total length in bytes, or -1 if the server didn't say.
func (r *Reader) Size() int64 { r.mu.Lock(); defer r.mu.Unlock(); return r.size }

// Seekable reports whether the server honours Range requests.
func (r *Reader) Seekable() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.seekable }

// Buffered is how many bytes ahead of the read position are in memory.
func (r *Reader) Buffered() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pos < r.lo || r.pos > r.hi {
		return 0
	}
	return r.hi - r.pos
}

// Promote lifts the prefetch limit.
func (r *Reader) Promote() {
	r.mu.Lock()
	r.prefetch = 0
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		switch {
		case r.closed:
			return 0, ErrClosed
		case r.size >= 0 && r.pos >= r.size:
			return 0, io.EOF
		case r.pos >= r.lo && r.pos < r.hi:
			n := r.copyOut(p)
			r.pos += int64(n)
			r.cond.Broadcast()
			return n, nil
		case r.eof && r.pos >= r.hi:
			return 0, io.EOF
		case r.err != nil:
			return 0, r.err
		}
		r.cond.Wait()
	}
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, ErrClosed
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		if r.size < 0 {
			return 0, errors.New("stream: SeekEnd with unknown size")
		}
		abs = r.size + offset
	default:
		return 0, errors.New("stream: bad whence")
	}
	if abs < 0 {
		return 0, errors.New("stream: negative position")
	}
	r.pos = abs
	inWindow := abs >= r.lo && abs <= r.hi+r.o.NearBytes
	if !inWindow && (abs < r.lo || r.seekable) {
		from := abs
		if !r.seekable {
			from = 0 // replay from the start and let the window skip forward
		}
		r.restartLocked(from)
	}
	r.cond.Broadcast()
	return abs, nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.cancel()
		r.cond.Broadcast()
	}
	return nil
}

func (r *Reader) restartLocked(from int64) {
	r.cancel()
	r.gen++
	r.lo, r.hi = from, from
	r.eof, r.err = false, nil
	if r.size >= 0 && from >= r.size {
		r.eof = true
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.fetch(ctx, r.gen, from, nil, nil)
}

// copyOut copies from the ring at pos; caller holds mu and ensured lo <= pos < hi.
func (r *Reader) copyOut(p []byte) int {
	n := int(min(int64(len(p)), r.hi-r.pos))
	c := int64(len(r.ring))
	start := int(r.pos % c)
	k := copy(p[:n], r.ring[start:])
	if k < n {
		copy(p[k:n], r.ring)
	}
	return n
}

// effectiveLo is the lowest offset we must keep: BehindBytes behind pos.
func (r *Reader) effectiveLo() int64 {
	lo := max(r.lo, r.pos-r.o.BehindBytes)
	return min(lo, r.hi)
}

// room is how many bytes the fetcher may append now.
func (r *Reader) room() int64 {
	room := int64(len(r.ring)) - (r.hi - r.effectiveLo())
	if r.prefetch > 0 {
		room = min(room, r.prefetch-r.hi)
	}
	return room
}

func (r *Reader) appendLocked(data []byte) {
	r.lo = r.effectiveLo()
	c := int64(len(r.ring))
	for len(data) > 0 {
		start := int(r.hi % c)
		k := copy(r.ring[start:], data)
		data = data[k:]
		r.hi += int64(k)
	}
}

func (r *Reader) request(ctx context.Context, off int64) (*http.Response, context.CancelFunc, error) {
	rctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, r.url, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	for k, v := range r.o.Header {
		req.Header[k] = v
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(off, 10)+"-")
	resp, err := r.o.Client.Do(req)
	if err != nil {
		cancel()
		// *url.Error embeds the URL, and stream URLs carry credentials.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = fmt.Errorf("stream: %s: %w", ue.Op, ue.Err)
		}
		return nil, nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
	default:
		resp.Body.Close()
		cancel()
		return nil, nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	if r.o.CheckResponse != nil {
		if err := r.o.CheckResponse(resp); err != nil {
			resp.Body.Close()
			cancel()
			return nil, nil, err
		}
	}
	return resp, cancel, nil
}

func responseSize(resp *http.Response) int64 {
	if resp.StatusCode == http.StatusPartialContent {
		// Content-Range: bytes 0-99/1234
		cr := resp.Header.Get("Content-Range")
		if i := strings.LastIndexByte(cr, '/'); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				return n
			}
		}
		return -1
	}
	return resp.ContentLength
}

// fetch fills the ring from off onwards, reconnecting as needed, until EOF,
// a terminal error, or the generation changes.
func (r *Reader) fetch(ctx context.Context, gen int, off int64, resp *http.Response, reqCancel context.CancelFunc) {
	buf := make([]byte, 64<<10)
	attempt := 0
	lastProgress := time.Now()
	for {
		if resp == nil {
			var err error
			resp, reqCancel, err = r.request(ctx, off)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				var he *HTTPError
				if errors.As(err, &he) && he.StatusCode == http.StatusRequestedRangeNotSatisfiable {
					r.finish(gen, nil)
					return
				}
				if errors.As(err, &he) && he.StatusCode < 500 {
					r.finish(gen, err)
					return
				}
				if !r.sleepBackoff(ctx, gen, &attempt, lastProgress, err) {
					return
				}
				continue
			}
		}
		skip := int64(0)
		if resp.StatusCode == http.StatusOK {
			skip = off // server ignored Range and started at 0
		}
		progressed, err := r.copyBody(gen, resp.Body, reqCancel, &off, skip, buf)
		resp.Body.Close()
		reqCancel()
		resp = nil
		if err == nil || errors.Is(err, errStale) || ctx.Err() != nil {
			return
		}
		if progressed {
			attempt = 0
			lastProgress = time.Now()
		}
		if !r.sleepBackoff(ctx, gen, &attempt, lastProgress, err) {
			return
		}
	}
}

func (r *Reader) copyBody(gen int, body io.Reader, reqCancel context.CancelFunc, off *int64, skip int64, buf []byte) (progressed bool, err error) {
	for {
		r.mu.Lock()
		for r.gen == gen && !r.closed && r.room() <= 0 {
			r.cond.Wait()
		}
		if r.gen != gen || r.closed {
			r.mu.Unlock()
			return progressed, errStale
		}
		room := r.room()
		r.mu.Unlock()

		n := int(min(int64(len(buf)), room))
		if skip > 0 {
			n = int(min(int64(len(buf)), skip))
		}
		var stalled atomic.Bool
		t := time.AfterFunc(r.o.StallTimeout, func() { stalled.Store(true); reqCancel() })
		k, rerr := body.Read(buf[:n])
		if !t.Stop() && stalled.Load() {
			rerr = errStalled
		}
		data := buf[:k]
		if skip > 0 {
			d := min(skip, int64(len(data)))
			data = data[d:]
			skip -= d
		}
		if len(data) > 0 {
			r.mu.Lock()
			if r.gen != gen || r.closed {
				r.mu.Unlock()
				return progressed, errStale
			}
			r.appendLocked(data)
			*off = r.hi
			r.cond.Broadcast()
			r.mu.Unlock()
			progressed = true
		}
		if errors.Is(rerr, io.EOF) {
			r.mu.Lock()
			short := r.size >= 0 && r.hi < r.size
			r.mu.Unlock()
			if short {
				return progressed, io.ErrUnexpectedEOF
			}
			r.finish(gen, nil)
			return progressed, nil
		}
		if rerr != nil {
			return progressed, rerr
		}
	}
}

// finish marks the end of the stream (err == nil) or a terminal error.
func (r *Reader) finish(gen int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gen != gen {
		return
	}
	if err != nil {
		r.err = err
	} else {
		r.eof = true
		if r.size < 0 {
			r.size = r.hi
		}
	}
	r.cond.Broadcast()
}

func (r *Reader) sleepBackoff(ctx context.Context, gen int, attempt *int, lastProgress time.Time, cause error) bool {
	if time.Since(lastProgress) > r.o.RetryBudget {
		r.finish(gen, fmt.Errorf("stream: giving up: %w", cause))
		return false
	}
	d := r.o.Backoff[min(*attempt, len(r.o.Backoff)-1)]
	*attempt++
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/stream/ && go test -race -count=5 ./internal/stream/`
Expected: `ok`, in about 2–3 s per run.

Coverage:
- a 10 MiB sequential read through a 1 MiB window, using one request
- in-window seeks making no new request
- a seek 2 GiB into a virtual 3 GiB file using `Range`
- `SeekEnd` and reads past the end
- a server without Range support
- drop and stall recovery
- giving up after the retry budget
- a 404 surfaced from `Open`
- `CheckResponse` rejection
- prefetch/promote
- `Close` unblocking a read
- the memory bound
- a long pause (Review Focus 5)
- no URL in errors (Review Focus 1)

- [ ] **Step 5: Commit**

```bash
git add internal/stream
git commit -m "stream: windowed HTTP Range reader with reconnect and prefetch" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Subsonic client core (auth, TLS, errors)

**Files:**
- Create: `internal/subsonic/types.go`, `errors.go`, `client.go`, `urls.go`, `certs/cacert.pem`
- Test: `internal/subsonic/fakeserver_test.go`, `internal/subsonic/client_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - **Model types:** `subsonic.ID` (a string that also decodes JSON numbers), `List[T]` (decodes an array *or* a lone object), `Artist`, `ArtistIndex`, `ArtistWithAlbums`, `Album`, `AlbumWithSongs`, `Song` (with `Suffix`, `ContentType`, `Duration`, `BitDepth`, `SamplingRate`, `ReplayGain *ReplayGain`), `ReplayGain{TrackGain, AlbumGain, TrackPeak, AlbumPeak *float64}`, `Genre`, `Playlist`, `PlaylistWithSongs`, `Starred`, `SearchResult`, `PlayQueue{Songs; Current ID; Position int64 /*ms*/}`, `Extension`, `ServerInfo{APIVersion, Type, ServerVersion string; OpenSubsonic bool; Extensions map[string][]int}` with `HasExtension`
  - **Construction:** `subsonic.Credentials{Username, Password, Token, Salt, APIKey string; AllowPlaintext bool}`, `subsonic.Options{BaseURL string; Credentials; CAFile string; InsecureSkipVerify bool; ClientName string; Timeout time.Duration; HTTPClient *http.Client}`, `subsonic.New(Options) (*Client, error)`, `subsonic.NewTokenPair(password) (token, salt string)`
  - **Client methods:** `Connect(ctx) (*ServerInfo, error)`, `AuthMethod() AuthMethod`, `Info() *ServerInfo`, `HTTPClient() *http.Client`, `StreamURL(ID, StreamOptions) string`, `CoverArtURL(ID, size int) string`
  - **Helpers:** `subsonic.StreamOptions{Format string; MaxBitRate, TimeOffset int}`, `subsonic.CheckStreamResponse(*http.Response) error` (fits `stream.Options.CheckResponse`), `subsonic.RedactURL(string) string`
  - **Errors:** `*APIError{Code, Message}` and the `Code*` constants, `ErrPlaintextRefused`, `ErrNoCredentials`, `ErrorKind`, `Classify(error) ErrorKind` (`KindUnreachable`, `KindTLS`, `KindAuth`, `KindPlaintextRefused`, `KindNotFound`, `KindTimeout`, `KindOther`)

**Notes:**
- `Connect` tries API key, then token, then plaintext (the last only if https or allowed). It falls through only on error 41 (token auth unsupported) or 42 (auth mechanism unsupported). A wrong password or an invalid key is reported as-is.
- An un-connected client uses the first available method.
- **TLS roots:** the system pool **plus** the embedded Mozilla bundle, plus the optional `CAFile`. The MiSTer's own CA store can't be trusted to be current.

- [ ] **Step 1: Fetch the CA bundle**

```bash
mkdir -p internal/subsonic/certs
curl -fsSL -o internal/subsonic/certs/cacert.pem https://curl.se/ca/cacert.pem
grep -c 'BEGIN CERTIFICATE' internal/subsonic/certs/cacert.pem   # >= 100 (about 120)
```

- [ ] **Step 2: Write the fake server and the failing tests**

`internal/subsonic/fakeserver_test.go`:

```go
package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
)

// fakeServer implements enough of the Subsonic auth rules to test the
// client's fallbacks, and serves testdata/<endpoint>.json for everything else.
type fakeServer struct {
	*httptest.Server
	user, pass       string
	apiKey           string // "" means the server doesn't support API keys (error 42)
	tokenUnsupported bool   // answer token auth with error 41 (LDAP-style users)
	override         map[string]string

	mu      sync.Mutex
	queries map[string]url.Values
}

func newFakeServer(t *testing.T, tls bool) *fakeServer {
	s := &fakeServer{user: "alice", pass: "sesame", override: map[string]string{}, queries: map[string]url.Values{}}
	h := http.HandlerFunc(s.serve)
	if tls {
		s.Server = httptest.NewTLSServer(h)
	} else {
		s.Server = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) query(endpoint string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queries[endpoint]
}

func fail(w http.ResponseWriter, code int, msg string) {
	fmt.Fprintf(w, `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":%d,"message":%q}}}`, code, msg)
}

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
	q := r.URL.Query()
	s.mu.Lock()
	s.queries[endpoint] = q
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")

	switch {
	case q.Get("apiKey") != "":
		if s.apiKey == "" {
			fail(w, CodeAuthMechUnsupported, "api keys not supported")
			return
		}
		if q.Get("apiKey") != s.apiKey {
			fail(w, CodeInvalidAPIKey, "invalid api key")
			return
		}
	case q.Get("t") != "":
		if s.tokenUnsupported {
			fail(w, CodeTokenAuthUnsupported, "token auth not supported for LDAP users")
			return
		}
		sum := md5.Sum([]byte(s.pass + q.Get("s")))
		if q.Get("u") != s.user || hex.EncodeToString(sum[:]) != q.Get("t") {
			fail(w, CodeWrongCredentials, "wrong username or password")
			return
		}
	case q.Get("p") != "":
		raw, _ := hex.DecodeString(strings.TrimPrefix(q.Get("p"), "enc:"))
		if q.Get("u") != s.user || string(raw) != s.pass {
			fail(w, CodeWrongCredentials, "wrong username or password")
			return
		}
	default:
		fail(w, CodeMissingParameter, "missing auth")
		return
	}

	if body, ok := s.override[endpoint]; ok {
		w.Write([]byte(body))
		return
	}
	switch endpoint {
	case "ping", "star", "unstar", "scrobble", "savePlayQueue":
		w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","serverVersion":"0.58.0","openSubsonic":true}}`))
	case "getOpenSubsonicExtensions":
		w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","openSubsonicExtensions":[{"name":"apiKeyAuthentication","versions":[1]},{"name":"transcodeOffset","versions":[1]}]}}`))
	default:
		b, err := os.ReadFile("testdata/" + endpoint + ".json")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}
}

func newTestClient(t *testing.T, s *fakeServer, c Credentials) *Client {
	t.Helper()
	cl, err := New(Options{BaseURL: s.URL, Credentials: c, HTTPClient: s.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return cl
}
```

`internal/subsonic/client_test.go`:

```go
package subsonic

import (
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func TestConnectWithPasswordUsesTokenAuth(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	info, err := c.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthToken {
		t.Fatalf("method = %v, want token", c.AuthMethod())
	}
	if info.Type != "navidrome" || !info.OpenSubsonic || !info.HasExtension("apiKeyAuthentication") {
		t.Fatalf("info = %+v", info)
	}
	if q := s.query("ping"); q.Has("p") || q.Get("v") != APIVersion || q.Get("c") != DefaultClientName || q.Get("f") != "json" {
		t.Fatalf("ping query = %v", q)
	}
}

func TestConnectWithStoredTokenPair(t *testing.T) {
	s := newFakeServer(t, false)
	tok, salt := NewTokenPair("sesame")
	c := newTestClient(t, s, Credentials{Username: "alice", Token: tok, Salt: salt})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if got := s.query("ping").Get("s"); got != salt {
		t.Fatalf("salt sent = %q, want the stored %q", got, salt)
	}
}

func TestConnectAPIKey(t *testing.T) {
	s := newFakeServer(t, false)
	s.apiKey = "key-123"
	c := newTestClient(t, s, Credentials{APIKey: "key-123"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthAPIKey {
		t.Fatalf("method = %v", c.AuthMethod())
	}
	if s.query("ping").Has("u") {
		t.Fatal("u must not be sent with apiKey (OpenSubsonic error 43)")
	}
}

func TestAPIKeyUnsupportedFallsBackToToken(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", APIKey: "key-123"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthToken {
		t.Fatalf("method = %v, want token", c.AuthMethod())
	}
}

func TestInvalidAPIKeyIsAnError(t *testing.T) {
	s := newFakeServer(t, false)
	s.apiKey = "key-123"
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", APIKey: "wrong"})
	_, err := c.Connect(ctx)
	if Classify(err) != KindAuth {
		t.Fatalf("err = %v (kind %v), want auth error", err, Classify(err))
	}
}

func TestTokenUnsupportedOverHTTPRefusesPlaintext(t *testing.T) {
	s := newFakeServer(t, false)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	_, err := c.Connect(ctx)
	if !errors.Is(err, ErrPlaintextRefused) || Classify(err) != KindPlaintextRefused {
		t.Fatalf("err = %v, want ErrPlaintextRefused", err)
	}
}

func TestTokenUnsupportedFallsBackToPlaintextWhenAllowed(t *testing.T) {
	s := newFakeServer(t, false)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame", AllowPlaintext: true})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthPlain || !strings.HasPrefix(s.query("ping").Get("p"), "enc:") {
		t.Fatalf("method = %v, p = %q", c.AuthMethod(), s.query("ping").Get("p"))
	}
}

func TestTokenUnsupportedFallsBackToPlaintextOverHTTPS(t *testing.T) {
	s := newFakeServer(t, true)
	s.tokenUnsupported = true
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if c.AuthMethod() != AuthPlain {
		t.Fatalf("method = %v, want plain", c.AuthMethod())
	}
}

func TestWrongPassword(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "nope"})
	_, err := c.Connect(ctx)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeWrongCredentials || Classify(err) != KindAuth {
		t.Fatalf("err = %v", err)
	}
}

func TestNoCredentials(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice"})
	if _, err := c.Connect(ctx); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidURL(t *testing.T) {
	for _, u := range []string{"", "music.local:4533", "ftp://x", "http://"} {
		if _, err := New(Options{BaseURL: u}); err == nil {
			t.Errorf("New(%q) succeeded", u)
		}
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	s := newFakeServer(t, false)
	c, _ := New(Options{BaseURL: s.URL + "/navidrome/", Credentials: Credentials{Username: "alice", Password: "sesame"}, HTTPClient: s.Client()})
	u := c.StreamURL("so-1", StreamOptions{Format: "raw"})
	if !strings.HasPrefix(u, s.URL+"/navidrome/rest/stream.view?") {
		t.Fatalf("stream URL = %s", u)
	}
}

func TestNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Credentials: Credentials{Username: "a", Password: "b"}})
	_, err := c.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v, want mention of HTTP 502", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL, Credentials: Credentials{Username: "a", Password: "b"}, Timeout: 100 * time.Millisecond})
	_, err := c.Connect(ctx)
	if Classify(err) != KindTimeout {
		t.Fatalf("err = %v (kind %v), want timeout", err, Classify(err))
	}
}

func TestUnreachable(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close() // nothing listens here now
	c, _ := New(Options{BaseURL: "http://" + addr, Credentials: Credentials{Username: "a", Password: "b"}})
	_, err := c.Connect(ctx)
	if Classify(err) != KindUnreachable {
		t.Fatalf("err = %v (kind %v), want unreachable", err, Classify(err))
	}
}

func writeServerCA(t *testing.T, s *fakeServer) string {
	p := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
	if err := os.WriteFile(p, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTLSSelfSignedRejectedByDefault(t *testing.T) {
	s := newFakeServer(t, true)
	c, _ := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}})
	_, err := c.Connect(ctx)
	if Classify(err) != KindTLS {
		t.Fatalf("err = %v (kind %v), want TLS", err, Classify(err))
	}
}

func TestTLSSelfSignedTrustedViaCAFile(t *testing.T) {
	s := newFakeServer(t, true)
	c, err := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}, CAFile: writeServerCA(t, s)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTLSInsecureSkipVerify(t *testing.T) {
	s := newFakeServer(t, true)
	c, _ := New(Options{BaseURL: s.URL, Credentials: Credentials{Username: "alice", Password: "sesame"}, InsecureSkipVerify: true})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTLSBadCAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.pem")
	os.WriteFile(p, []byte("not a certificate"), 0o600)
	if _, err := New(Options{BaseURL: "https://x.example", CAFile: p}); err == nil {
		t.Fatal("New accepted a CA file without certificates")
	}
	if _, err := New(Options{BaseURL: "https://x.example", CAFile: "/does/not/exist.pem"}); err == nil {
		t.Fatal("New accepted a missing CA file")
	}
}

func TestEmbeddedRootsParse(t *testing.T) {
	pool, err := tlsConfig("", false)
	if err != nil || pool.RootCAs == nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(embeddedRoots), "BEGIN CERTIFICATE"); n < 100 {
		t.Fatalf("embedded bundle has only %d certificates", n)
	}
}

func TestRedactURL(t *testing.T) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	red := RedactURL(c.StreamURL("so-1", StreamOptions{Format: "raw"}))
	if strings.Contains(red, c.creds.Token) || strings.Contains(red, c.creds.Salt) {
		t.Fatalf("token or salt leaked: %s", red)
	}
	if !strings.Contains(red, "id=so-1") {
		t.Fatalf("redaction removed non-secret params: %s", red)
	}
}

func TestCheckStreamResponse(t *testing.T) {
	mk := func(ct, body string) *http.Response {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", ct)
		rec.WriteString(body)
		return rec.Result()
	}
	if err := CheckStreamResponse(mk("audio/flac", "fLaC")); err != nil {
		t.Fatalf("audio rejected: %v", err)
	}
	err := CheckStreamResponse(mk("application/json", `{"subsonic-response":{"status":"failed","error":{"code":70,"message":"not found"}}}`))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeNotFound {
		t.Fatalf("err = %v, want APIError 70", err)
	}
	if err := CheckStreamResponse(mk("text/xml", `<subsonic-response status="failed"/>`)); err == nil {
		t.Fatal("xml error document accepted")
	}
}

func TestErrorsDoNotLeakCredentials(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	c, _ := New(Options{BaseURL: "http://" + addr, Credentials: Credentials{Username: "alice", Password: "sesame", APIKey: "key-123"}})
	_, err := c.Connect(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{c.creds.Token, c.creds.Salt, "key-123", "sesame"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error text leaks a credential: %v", err)
		}
	}
	if Classify(err) != KindUnreachable {
		t.Fatalf("redaction broke classification: %v", Classify(err))
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/subsonic/`
Expected: build failure, `undefined: Credentials`, `undefined: New`, ….

- [ ] **Step 4: Implement the types, errors, client and URL builders**

`internal/subsonic/types.go`:

```go
package subsonic

import (
	"bytes"
	"encoding/json"
)

// ID is a Subsonic identifier. Servers disagree on whether IDs are JSON
// strings or numbers, so it accepts both.
type ID string

func (id *ID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*id = ID(s)
		return nil
	}
	if bytes.Equal(b, []byte("null")) {
		*id = ""
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*id = ID(n.String())
	return nil
}

// List decodes a JSON array, and also a lone object, which some servers
// emit when a list has exactly one element.
type List[T any] []T

func (l *List[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		var one T
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*l = List[T]{one}
		return nil
	}
	var many []T
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*l = many
	return nil
}

type Artist struct {
	ID         ID     `json:"id"`
	Name       string `json:"name"`
	CoverArt   ID     `json:"coverArt"`
	AlbumCount int    `json:"albumCount"`
	Starred    string `json:"starred"`
}

type ArtistIndex struct {
	Name    string       `json:"name"`
	Artists List[Artist] `json:"artist"`
}

type ArtistWithAlbums struct {
	Artist
	Albums List[Album] `json:"album"`
}

type Album struct {
	ID        ID     `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  ID     `json:"artistId"`
	CoverArt  ID     `json:"coverArt"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	PlayCount int64  `json:"playCount"`
	Year      int    `json:"year"`
	Genre     string `json:"genre"`
	Created   string `json:"created"`
	Starred   string `json:"starred"`
}

type AlbumWithSongs struct {
	Album
	Songs List[Song] `json:"song"`
}

// ReplayGain is the OpenSubsonic replayGain object (values in dB / linear peak).
type ReplayGain struct {
	TrackGain *float64 `json:"trackGain"`
	AlbumGain *float64 `json:"albumGain"`
	TrackPeak *float64 `json:"trackPeak"`
	AlbumPeak *float64 `json:"albumPeak"`
}

type Song struct {
	ID           ID          `json:"id"`
	Parent       ID          `json:"parent"`
	Title        string      `json:"title"`
	Album        string      `json:"album"`
	Artist       string      `json:"artist"`
	AlbumID      ID          `json:"albumId"`
	ArtistID     ID          `json:"artistId"`
	CoverArt     ID          `json:"coverArt"`
	Track        int         `json:"track"`
	DiscNumber   int         `json:"discNumber"`
	Year         int         `json:"year"`
	Genre        string      `json:"genre"`
	Size         int64       `json:"size"`
	ContentType  string      `json:"contentType"`
	Suffix       string      `json:"suffix"`
	Duration     int         `json:"duration"` // seconds
	BitRate      int         `json:"bitRate"`  // kbps
	BitDepth     int         `json:"bitDepth"`
	SamplingRate int         `json:"samplingRate"`
	ChannelCount int         `json:"channelCount"`
	Starred      string      `json:"starred"`
	ReplayGain   *ReplayGain `json:"replayGain"`
}

type Genre struct {
	Name       string `json:"value"`
	SongCount  int    `json:"songCount"`
	AlbumCount int    `json:"albumCount"`
}

type Playlist struct {
	ID        ID     `json:"id"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Public    bool   `json:"public"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	CoverArt  ID     `json:"coverArt"`
	Comment   string `json:"comment"`
}

type PlaylistWithSongs struct {
	Playlist
	Songs List[Song] `json:"entry"`
}

type Starred struct {
	Artists List[Artist] `json:"artist"`
	Albums  List[Album]  `json:"album"`
	Songs   List[Song]   `json:"song"`
}

type SearchResult struct {
	Artists List[Artist] `json:"artist"`
	Albums  List[Album]  `json:"album"`
	Songs   List[Song]   `json:"song"`
}

type PlayQueue struct {
	Songs    List[Song] `json:"entry"`
	Current  ID         `json:"current"`
	Position int64      `json:"position"` // milliseconds
	Changed  string     `json:"changed"`
}

type Extension struct {
	Name     string `json:"name"`
	Versions []int  `json:"versions"`
}

// ServerInfo is what Connect learned about the server.
type ServerInfo struct {
	APIVersion    string
	Type          string // "navidrome", "gonic", ... (OpenSubsonic)
	ServerVersion string
	OpenSubsonic  bool
	Extensions    map[string][]int
}

func (s *ServerInfo) HasExtension(name string) bool {
	_, ok := s.Extensions[name]
	return ok
}

// response is the payload inside "subsonic-response".
type response struct {
	Status        string    `json:"status"`
	Version       string    `json:"version"`
	Type          string    `json:"type"`
	ServerVersion string    `json:"serverVersion"`
	OpenSubsonic  bool      `json:"openSubsonic"`
	Error         *APIError `json:"error"`

	Artists *struct {
		Index List[ArtistIndex] `json:"index"`
	} `json:"artists"`
	Artist     *ArtistWithAlbums `json:"artist"`
	Album      *AlbumWithSongs   `json:"album"`
	AlbumList2 *struct {
		Albums List[Album] `json:"album"`
	} `json:"albumList2"`
	Genres *struct {
		Genres List[Genre] `json:"genre"`
	} `json:"genres"`
	Playlists *struct {
		Playlists List[Playlist] `json:"playlist"`
	} `json:"playlists"`
	Playlist      *PlaylistWithSongs `json:"playlist"`
	Starred2      *Starred           `json:"starred2"`
	SearchResult3 *SearchResult      `json:"searchResult3"`
	PlayQueue     *PlayQueue         `json:"playQueue"`
	Extensions    List[Extension]    `json:"openSubsonicExtensions"`
}

func (s Song) IsStarred() bool   { return s.Starred != "" }
func (a Album) IsStarred() bool  { return a.Starred != "" }
func (a Artist) IsStarred() bool { return a.Starred != "" }
```

`internal/subsonic/errors.go`:

```go
package subsonic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
)

// Subsonic error codes.
const (
	CodeGeneric              = 0
	CodeMissingParameter     = 10
	CodeClientTooOld         = 20
	CodeServerTooOld         = 30
	CodeWrongCredentials     = 40
	CodeTokenAuthUnsupported = 41
	CodeAuthMechUnsupported  = 42
	CodeConflictingAuth      = 43
	CodeInvalidAPIKey        = 44
	CodeNotAuthorized        = 50
	CodeTrialExpired         = 60
	CodeNotFound             = 70
)

// APIError is a "failed" subsonic-response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return fmt.Sprintf("subsonic: error %d: %s", e.Code, e.Message) }

// ErrPlaintextRefused: the server only accepts the plain password, and the
// connection isn't HTTPS, and allow_plaintext_password is off.
var ErrPlaintextRefused = errors.New("subsonic: server needs the plain password; use https or set allow_plaintext_password")

// ErrNoCredentials: none of password, token+salt or API key was configured.
var ErrNoCredentials = errors.New("subsonic: no credentials configured")

type ErrorKind int

const (
	KindOther ErrorKind = iota
	KindUnreachable
	KindTLS
	KindAuth
	KindPlaintextRefused
	KindNotFound
	KindTimeout
)

func (k ErrorKind) String() string {
	return [...]string{"error", "server unreachable", "TLS/certificate error", "wrong credentials", "plain password refused", "not found", "timed out"}[k]
}

// Classify maps an error from this package to something a UI can explain.
func Classify(err error) ErrorKind {
	if err == nil {
		return KindOther
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Code {
		case CodeWrongCredentials, CodeInvalidAPIKey, CodeNotAuthorized, CodeTokenAuthUnsupported, CodeAuthMechUnsupported:
			return KindAuth
		case CodeNotFound:
			return KindNotFound
		}
		return KindOther
	}
	if errors.Is(err, ErrPlaintextRefused) {
		return KindPlaintextRefused
	}
	var (
		unknownCA *x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		invalid   x509.CertificateInvalidError
		verifyErr *tls.CertificateVerificationError
		recordErr tls.RecordHeaderError
	)
	if errors.As(err, &unknownCA) || errors.As(err, &hostErr) || errors.As(err, &invalid) ||
		errors.As(err, &verifyErr) || errors.As(err, &recordErr) {
		return KindTLS
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return KindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return KindTimeout
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return KindUnreachable
	}
	return KindOther
}
```

`internal/subsonic/client.go`:

```go
// Package subsonic is a client for the Subsonic REST API (1.16.1) with
// OpenSubsonic extensions, as served by Navidrome, gonic, Airsonic and others.
package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	APIVersion        = "1.16.1"
	DefaultClientName = "MiSTerSubsonic"
)

//go:embed certs/cacert.pem
var embeddedRoots []byte

type AuthMethod int

const (
	AuthNone AuthMethod = iota
	AuthAPIKey
	AuthToken
	AuthPlain
)

func (m AuthMethod) String() string {
	return [...]string{"none", "api key", "token", "plain password"}[m]
}

// Credentials: set any of APIKey, Token+Salt, or Password. Connect tries
// them in that order.
type Credentials struct {
	Username       string
	Password       string
	Token, Salt    string // precomputed md5(password+salt) pair
	APIKey         string
	AllowPlaintext bool // allow p=enc: over plain http
}

type Options struct {
	BaseURL string // e.g. "https://music.example.com" or "http://192.168.1.10:4533"
	Credentials
	CAFile             string // extra PEM roots (self-signed servers)
	InsecureSkipVerify bool
	ClientName         string        // default DefaultClientName
	Timeout            time.Duration // per metadata request; default 15 s
	HTTPClient         *http.Client  // overrides the built-in client (tests)
}

type Client struct {
	base    string
	creds   Credentials
	name    string
	timeout time.Duration
	hc      *http.Client

	mu     sync.Mutex
	method AuthMethod
	info   *ServerInfo
}

// NewTokenPair returns a random salt and md5(password+salt), for storing
// instead of the password.
func NewTokenPair(password string) (token, salt string) {
	b := make([]byte, 8)
	rand.Read(b)
	salt = hex.EncodeToString(b)
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:]), salt
}

func New(o Options) (*Client, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("subsonic: invalid server URL %q", o.BaseURL)
	}
	c := &Client{
		base:    strings.TrimRight(o.BaseURL, "/"),
		creds:   o.Credentials,
		name:    o.ClientName,
		timeout: o.Timeout,
		hc:      o.HTTPClient,
	}
	if c.name == "" {
		c.name = DefaultClientName
	}
	if c.timeout <= 0 {
		c.timeout = 15 * time.Second
	}
	if c.creds.Token == "" && c.creds.Password != "" {
		c.creds.Token, c.creds.Salt = NewTokenPair(c.creds.Password)
	}
	if c.hc == nil {
		tc, err := tlsConfig(o.CAFile, o.InsecureSkipVerify)
		if err != nil {
			return nil, err
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = tc
		tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		tr.ResponseHeaderTimeout = 15 * time.Second
		c.hc = &http.Client{Transport: tr}
	}
	c.method = c.candidates()[0]
	return c, nil
}

func tlsConfig(caFile string, insecure bool) (*tls.Config, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(embeddedRoots)
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("subsonic: ca_file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("subsonic: ca_file %s contains no PEM certificates", caFile)
		}
	}
	return &tls.Config{RootCAs: pool, InsecureSkipVerify: insecure}, nil
}

// HTTPClient is the client used for API calls, for sharing TLS settings
// with the stream reader. It has no overall timeout.
func (c *Client) HTTPClient() *http.Client { return c.hc }

// AuthMethod is the method in use (settled by Connect).
func (c *Client) AuthMethod() AuthMethod { c.mu.Lock(); defer c.mu.Unlock(); return c.method }

// Info is what the last successful Connect learned, or nil.
func (c *Client) Info() *ServerInfo { c.mu.Lock(); defer c.mu.Unlock(); return c.info }

func (c *Client) plaintextAllowed() bool {
	return strings.HasPrefix(c.base, "https://") || c.creds.AllowPlaintext
}

func (c *Client) candidates() []AuthMethod {
	var m []AuthMethod
	if c.creds.APIKey != "" {
		m = append(m, AuthAPIKey)
	}
	if c.creds.Token != "" && c.creds.Salt != "" {
		m = append(m, AuthToken)
	}
	if c.creds.Password != "" && c.plaintextAllowed() {
		m = append(m, AuthPlain)
	}
	if len(m) == 0 {
		m = append(m, AuthNone)
	}
	return m
}

// Connect pings the server, settling on the first auth method that works,
// and records the server's OpenSubsonic extensions.
func (c *Client) Connect(ctx context.Context) (*ServerInfo, error) {
	var lastErr error = ErrNoCredentials
	for _, m := range c.candidates() {
		if m == AuthNone {
			break
		}
		c.mu.Lock()
		c.method = m
		c.mu.Unlock()
		r, err := c.call(ctx, "ping", nil)
		if err == nil {
			info := &ServerInfo{APIVersion: r.Version, Type: r.Type, ServerVersion: r.ServerVersion, OpenSubsonic: r.OpenSubsonic, Extensions: map[string][]int{}}
			if r.OpenSubsonic {
				if er, err := c.call(ctx, "getOpenSubsonicExtensions", nil); err == nil {
					for _, e := range er.Extensions {
						info.Extensions[e.Name] = e.Versions
					}
				}
			}
			c.mu.Lock()
			c.info = info
			c.mu.Unlock()
			return info, nil
		}
		lastErr = err
		var ae *APIError
		if !errors.As(err, &ae) || (ae.Code != CodeTokenAuthUnsupported && ae.Code != CodeAuthMechUnsupported) {
			return nil, err
		}
	}
	var ae *APIError
	if errors.As(lastErr, &ae) && ae.Code == CodeTokenAuthUnsupported && c.creds.Password != "" && !c.plaintextAllowed() {
		return nil, fmt.Errorf("%w (%v)", ErrPlaintextRefused, lastErr)
	}
	return nil, lastErr
}

// params returns the auth and protocol query parameters.
func (c *Client) params() url.Values {
	c.mu.Lock()
	m := c.method
	c.mu.Unlock()
	v := url.Values{}
	v.Set("v", APIVersion)
	v.Set("c", c.name)
	v.Set("f", "json")
	switch m {
	case AuthAPIKey:
		v.Set("apiKey", c.creds.APIKey)
		return v
	case AuthToken:
		v.Set("t", c.creds.Token)
		v.Set("s", c.creds.Salt)
	case AuthPlain:
		v.Set("p", "enc:"+hex.EncodeToString([]byte(c.creds.Password)))
	}
	v.Set("u", c.creds.Username)
	return v
}

func (c *Client) endpointURL(endpoint string, extra url.Values) string {
	v := c.params()
	for k, vs := range extra {
		v[k] = vs
	}
	return c.base + "/rest/" + endpoint + ".view?" + v.Encode()
}

func (c *Client) call(ctx context.Context, endpoint string, extra url.Values) (*response, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointURL(endpoint, extra), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, stripURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, err)
	}
	r, derr := decodeResponse(body)
	if derr != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("subsonic: %s: HTTP %s", endpoint, resp.Status)
		}
		return nil, fmt.Errorf("subsonic: %s: %w", endpoint, derr)
	}
	if r.Status != "ok" {
		if r.Error == nil {
			return nil, fmt.Errorf("subsonic: %s: status %q", endpoint, r.Status)
		}
		return nil, r.Error
	}
	return r, nil
}

func decodeResponse(body []byte) (*response, error) {
	var env struct {
		R *response `json:"subsonic-response"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("bad response: %w", err)
	}
	if env.R == nil {
		return nil, errors.New("bad response: no subsonic-response")
	}
	return env.R, nil
}

// CheckStreamResponse rejects stream responses that are really error
// documents (servers answer failed stream requests with 200 + JSON/XML).
// It fits stream.Options.CheckResponse.
func CheckStreamResponse(resp *http.Response) error {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") && !strings.Contains(ct, "xml") {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if r, err := decodeResponse(body); err == nil && r.Error != nil {
		return r.Error
	}
	return fmt.Errorf("subsonic: stream returned %s instead of audio", ct)
}

// stripURL drops the request URL (which carries credentials) from
// net/http's *url.Error, keeping the underlying cause for errors.As/Is.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// RedactURL hides credentials in a URL built by this package, for logs.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable url>"
	}
	q := u.Query()
	for _, k := range []string{"t", "s", "p", "apiKey"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
```

`internal/subsonic/urls.go`:

```go
package subsonic

import (
	"net/url"
	"strconv"
)

type StreamOptions struct {
	Format     string // "raw" for the original file, or a transcode target like "mp3"
	MaxBitRate int    // kbps, 0 = server default
	TimeOffset int    // seconds; only honoured for transcoded streams
}

func (c *Client) StreamURL(id ID, o StreamOptions) string {
	v := url.Values{"id": {string(id)}}
	if o.Format != "" {
		v.Set("format", o.Format)
	}
	if o.MaxBitRate > 0 {
		v.Set("maxBitRate", strconv.Itoa(o.MaxBitRate))
	}
	if o.TimeOffset > 0 {
		v.Set("timeOffset", strconv.Itoa(o.TimeOffset))
	}
	return c.endpointURL("stream", v)
}

func (c *Client) CoverArtURL(id ID, size int) string {
	v := url.Values{"id": {string(id)}}
	if size > 0 {
		v.Set("size", strconv.Itoa(size))
	}
	return c.endpointURL("getCoverArt", v)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/subsonic/ && go test -race ./internal/subsonic/`
Expected: `ok`. A log line `http: TLS handshake error … bad certificate` from the self-signed test is expected noise.

- [ ] **Step 6: Commit**

```bash
git add internal/subsonic
git commit -m "subsonic: client with auth fallbacks, TLS options and error classification" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: Subsonic endpoints

**Files:**
- Create: `internal/subsonic/endpoints.go`
- Create: `internal/subsonic/testdata/*.json` (the recorded-shape fixtures)
- Test: `internal/subsonic/endpoints_test.go`

**Interfaces:**
- Consumes: `Client`, `call`, the types, `APIError` and the codes (Task 6).
- Produces these `*Client` methods:
  - `GetArtists(ctx) ([]ArtistIndex, error)`
  - `GetArtist(ctx, ID) (*ArtistWithAlbums, error)`
  - `GetAlbum(ctx, ID) (*AlbumWithSongs, error)`
  - `GetAlbumList2(ctx, AlbumListQuery{Type string; Size, Offset, FromYear, ToYear int; Genre string}) ([]Album, error)`, with the `List*` type constants
  - `GetGenres`
  - `GetPlaylists`
  - `GetPlaylist(ctx, ID)`
  - `GetStarred2`
  - `Search3(ctx, query, SearchQuery{ArtistCount, ArtistOffset, AlbumCount, AlbumOffset, SongCount, SongOffset int})`
  - `Star` / `Unstar(ctx, StarTarget{SongIDs, AlbumIDs, ArtistIDs []ID})`
  - `Scrobble(ctx, ID, time.Time, submission bool)`
  - `GetPlayQueue(ctx) (*PlayQueue, error)`: nil, nil when there is none
  - `SavePlayQueue(ctx, []ID, current ID, pos time.Duration)`

The fixtures deliberately include Cyrillic and accented names, a numeric song `id`, and a lone-object `artist` list (older Subsonic JSON serialisers do that).

- [ ] **Step 1: Write the fixtures**

`internal/subsonic/testdata/getAlbum.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","openSubsonic":true,
 "album":{"id":"al-10","name":"Радио Африка","artist":"Аквариум","artistId":"ar-2","coverArt":"al-10","songCount":2,"duration":400,"year":1983,
  "song":[
   {"id":"so-1","parent":"al-10","title":"Капитан Африка","album":"Радио Африка","artist":"Аквариум","albumId":"al-10","artistId":"ar-2","coverArt":"al-10",
    "track":1,"discNumber":1,"year":1983,"size":41943040,"contentType":"audio/flac","suffix":"flac","duration":240,"bitRate":1411,
    "bitDepth":24,"samplingRate":96000,"channelCount":2,"replayGain":{"trackGain":-6.5,"albumGain":-7.1,"trackPeak":0.98,"albumPeak":0.99}},
   {"id":1002,"parent":"al-10","title":"Время Луны","album":"Радио Африка","artist":"Аквариум","albumId":"al-10","artistId":"ar-2",
    "track":2,"size":5242880,"contentType":"audio/mp4","suffix":"m4a","duration":160,"bitRate":256,"starred":"2025-05-05T00:00:00Z"}]}}}
```

`internal/subsonic/testdata/getAlbumList2.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","albumList2":{"album":[
  {"id":"al-10","name":"Радио Африка","artist":"Аквариум","artistId":"ar-2","year":1983},
  {"id":"al-20","name":"Homogenic","artist":"Björk","artistId":"ar-3","year":1997}]}}}
```

`internal/subsonic/testdata/getArtist.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","openSubsonic":true,
 "artist":{"id":"ar-2","name":"Аквариум","albumCount":2,"album":[
   {"id":"al-10","name":"Радио Африка","artist":"Аквариум","artistId":"ar-2","coverArt":"al-10","songCount":11,"duration":2520,"year":1983,"genre":"Rock"},
   {"id":"al-11","name":"Треугольник","artist":"Аквариум","artistId":"ar-2","coverArt":"al-11","songCount":14,"duration":2100,"year":1981}]}}}
```

`internal/subsonic/testdata/getArtists.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","type":"navidrome","serverVersion":"0.58.0","openSubsonic":true,
 "artists":{"ignoredArticles":"The El La Los Las Le Les","index":[
  {"name":"A","artist":[
    {"id":"ar-1","name":"ABBA","coverArt":"ar-ar-1","albumCount":3},
    {"id":"ar-2","name":"Аквариум","coverArt":"ar-ar-2","albumCount":12,"starred":"2025-01-02T03:04:05Z"}]},
  {"name":"B","artist":{"id":"ar-3","name":"Björk","albumCount":1}}
 ]}}}
```

`internal/subsonic/testdata/getGenres.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","genres":{"genre":[{"value":"Rock","songCount":120,"albumCount":10},{"value":"Electronic","songCount":40,"albumCount":4}]}}}
```

`internal/subsonic/testdata/getPlayQueue.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","playQueue":{"current":"so-9","position":61500,"changed":"2026-09-01T10:00:00Z",
 "entry":[{"id":"so-1","title":"Капитан Африка","suffix":"flac","duration":240},{"id":"so-9","title":"Jóga","suffix":"mp3","duration":305}]}}}
```

`internal/subsonic/testdata/getPlaylist.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","playlist":{"id":"pl-1","name":"Evening","owner":"alice","songCount":2,"duration":400,
 "entry":[{"id":"so-1","title":"Капитан Африка","suffix":"flac","duration":240},{"id":"so-9","title":"Jóga","suffix":"mp3","duration":305}]}}}
```

`internal/subsonic/testdata/getPlaylists.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","playlists":{"playlist":[{"id":"pl-1","name":"Evening","owner":"alice","public":false,"songCount":2,"duration":400}]}}}
```

`internal/subsonic/testdata/getStarred2.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","starred2":{}}}
```

`internal/subsonic/testdata/search3.json`:

```json
{"subsonic-response":{"status":"ok","version":"1.16.1","searchResult3":{
 "artist":[{"id":"ar-3","name":"Björk"}],
 "album":[{"id":"al-20","name":"Homogenic","artist":"Björk"}],
 "song":[{"id":"so-9","title":"Jóga","artist":"Björk","album":"Homogenic","suffix":"mp3","duration":305}]}}}
```

- [ ] **Step 2: Write the failing tests**

`internal/subsonic/endpoints_test.go`:

```go
package subsonic

import (
	"strings"
	"testing"
	"time"
)

func connected(t *testing.T) (*fakeServer, *Client) {
	s := newFakeServer(t, false)
	c := newTestClient(t, s, Credentials{Username: "alice", Password: "sesame"})
	if _, err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestGetArtistsHandlesSingleObjectList(t *testing.T) {
	_, c := connected(t)
	idx, err := c.GetArtists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 2 || len(idx[0].Artists) != 2 || len(idx[1].Artists) != 1 {
		t.Fatalf("index = %+v", idx)
	}
	if idx[0].Artists[1].Name != "Аквариум" || !idx[0].Artists[1].IsStarred() {
		t.Fatalf("artist = %+v", idx[0].Artists[1])
	}
	if idx[1].Artists[0].Name != "Björk" {
		t.Fatalf("single-object index decoded as %+v", idx[1].Artists)
	}
}

func TestGetAlbumDecodesSongsAndNumericIDs(t *testing.T) {
	s, c := connected(t)
	al, err := c.GetAlbum(ctx, "al-10")
	if err != nil {
		t.Fatal(err)
	}
	if s.query("getAlbum").Get("id") != "al-10" {
		t.Fatal("album id not sent")
	}
	if len(al.Songs) != 2 {
		t.Fatalf("songs = %d", len(al.Songs))
	}
	a, b := al.Songs[0], al.Songs[1]
	if a.Suffix != "flac" || a.BitDepth != 24 || a.SamplingRate != 96000 || a.Duration != 240 || a.Size != 41943040 {
		t.Fatalf("song 1 = %+v", a)
	}
	if a.ReplayGain == nil || a.ReplayGain.TrackGain == nil || *a.ReplayGain.TrackGain != -6.5 || *a.ReplayGain.AlbumGain != -7.1 {
		t.Fatalf("replayGain = %+v", a.ReplayGain)
	}
	if b.ID != "1002" || b.ReplayGain != nil || !b.IsStarred() {
		t.Fatalf("song 2 = %+v", b)
	}
}

func TestGetArtist(t *testing.T) {
	_, c := connected(t)
	ar, err := c.GetArtist(ctx, "ar-2")
	if err != nil || len(ar.Albums) != 2 || ar.Albums[0].Year != 1983 {
		t.Fatalf("artist = %+v, %v", ar, err)
	}
}

func TestGetAlbumList2Params(t *testing.T) {
	s, c := connected(t)
	list, err := c.GetAlbumList2(ctx, AlbumListQuery{Type: ListByYear, FromYear: 1980, ToYear: 1999, Offset: 100})
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	q := s.query("getAlbumList2")
	if q.Get("type") != "byYear" || q.Get("fromYear") != "1980" || q.Get("toYear") != "1999" || q.Get("size") != "100" || q.Get("offset") != "100" {
		t.Fatalf("query = %v", q)
	}
	c.GetAlbumList2(ctx, AlbumListQuery{Type: ListByGenre, Genre: "Rock"})
	if q := s.query("getAlbumList2"); q.Get("genre") != "Rock" || q.Has("fromYear") {
		t.Fatalf("byGenre query = %v", q)
	}
}

func TestGenresPlaylistsStarredSearch(t *testing.T) {
	s, c := connected(t)
	if g, err := c.GetGenres(ctx); err != nil || len(g) != 2 || g[0].Name != "Rock" {
		t.Fatalf("genres = %v, %v", g, err)
	}
	if p, err := c.GetPlaylists(ctx); err != nil || len(p) != 1 || p[0].Name != "Evening" {
		t.Fatalf("playlists = %v, %v", p, err)
	}
	if p, err := c.GetPlaylist(ctx, "pl-1"); err != nil || len(p.Songs) != 2 {
		t.Fatalf("playlist = %v, %v", p, err)
	}
	if st, err := c.GetStarred2(ctx); err != nil || st == nil || len(st.Songs) != 0 {
		t.Fatalf("starred = %v, %v", st, err)
	}
	res, err := c.Search3(ctx, "björk", SearchQuery{ArtistCount: 5, AlbumCount: 10, SongCount: 20, SongOffset: 20})
	if err != nil || len(res.Artists) != 1 || len(res.Songs) != 1 {
		t.Fatalf("search = %+v, %v", res, err)
	}
	q := s.query("search3")
	if q.Get("query") != "björk" || q.Get("artistCount") != "5" || q.Get("songOffset") != "20" {
		t.Fatalf("search query = %v", q)
	}
}

func TestStarAndScrobbleParams(t *testing.T) {
	s, c := connected(t)
	if err := c.Star(ctx, StarTarget{SongIDs: []ID{"so-1", "so-2"}, AlbumIDs: []ID{"al-10"}}); err != nil {
		t.Fatal(err)
	}
	q := s.query("star")
	if strings.Join(q["id"], ",") != "so-1,so-2" || q.Get("albumId") != "al-10" {
		t.Fatalf("star query = %v", q)
	}
	at := time.UnixMilli(1790000000123)
	if err := c.Scrobble(ctx, "so-1", at, true); err != nil {
		t.Fatal(err)
	}
	q = s.query("scrobble")
	if q.Get("id") != "so-1" || q.Get("time") != "1790000000123" || q.Get("submission") != "true" {
		t.Fatalf("scrobble query = %v", q)
	}
}

func TestPlayQueueRoundTrip(t *testing.T) {
	s, c := connected(t)
	pq, err := c.GetPlayQueue(ctx)
	if err != nil || pq == nil || pq.Current != "so-9" || pq.Position != 61500 || len(pq.Songs) != 2 {
		t.Fatalf("play queue = %+v, %v", pq, err)
	}
	s.override["getPlayQueue"] = `{"subsonic-response":{"status":"ok","version":"1.16.1"}}`
	if pq, err := c.GetPlayQueue(ctx); err != nil || pq != nil {
		t.Fatalf("empty play queue = %+v, %v; want nil, nil", pq, err)
	}
	s.override["getPlayQueue"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"no queue"}}}`
	if pq, err := c.GetPlayQueue(ctx); err != nil || pq != nil {
		t.Fatalf("not-found play queue = %+v, %v; want nil, nil", pq, err)
	}
	if err := c.SavePlayQueue(ctx, []ID{"so-1", "so-9"}, "so-9", 61500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	q := s.query("savePlayQueue")
	if strings.Join(q["id"], ",") != "so-1,so-9" || q.Get("current") != "so-9" || q.Get("position") != "61500" {
		t.Fatalf("savePlayQueue query = %v", q)
	}
}

func TestStreamAndCoverURLs(t *testing.T) {
	_, c := connected(t)
	u := c.StreamURL("so-1", StreamOptions{Format: "mp3", MaxBitRate: 320, TimeOffset: 42})
	for _, want := range []string{"/rest/stream.view?", "id=so-1", "format=mp3", "maxBitRate=320", "timeOffset=42", "t=", "s=", "u=alice"} {
		if !strings.Contains(u, want) {
			t.Errorf("stream URL %s lacks %q", u, want)
		}
	}
	if u := c.CoverArtURL("al-10", 300); !strings.Contains(u, "/rest/getCoverArt.view?") || !strings.Contains(u, "size=300") {
		t.Errorf("cover URL = %s", u)
	}
}

func TestMissingEntityIsNotFound(t *testing.T) {
	s, c := connected(t)
	s.override["getAlbum"] = `{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":70,"message":"Album not found"}}}`
	_, err := c.GetAlbum(ctx, "nope")
	if Classify(err) != KindNotFound {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/subsonic/`
Expected: build failure, `c.GetArtists undefined`, ….

- [ ] **Step 4: Implement the endpoints**

`internal/subsonic/endpoints.go`:

```go
package subsonic

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"
)

func (c *Client) GetArtists(ctx context.Context) ([]ArtistIndex, error) {
	r, err := c.call(ctx, "getArtists", nil)
	if err != nil || r.Artists == nil {
		return nil, err
	}
	return r.Artists.Index, nil
}

func (c *Client) GetArtist(ctx context.Context, id ID) (*ArtistWithAlbums, error) {
	r, err := c.call(ctx, "getArtist", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Artist == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "artist not found"}
	}
	return r.Artist, nil
}

func (c *Client) GetAlbum(ctx context.Context, id ID) (*AlbumWithSongs, error) {
	r, err := c.call(ctx, "getAlbum", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Album == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "album not found"}
	}
	return r.Album, nil
}

// Album list types for GetAlbumList2.
const (
	ListNewest       = "newest"
	ListRecent       = "recent"
	ListFrequent     = "frequent"
	ListRandom       = "random"
	ListAlphabetical = "alphabeticalByName"
	ListByYear       = "byYear"
	ListByGenre      = "byGenre"
)

type AlbumListQuery struct {
	Type     string
	Size     int // default 100, max 500
	Offset   int
	FromYear int // ListByYear
	ToYear   int // ListByYear
	Genre    string
}

func (c *Client) GetAlbumList2(ctx context.Context, q AlbumListQuery) ([]Album, error) {
	if q.Size <= 0 {
		q.Size = 100
	}
	v := url.Values{"type": {q.Type}, "size": {strconv.Itoa(q.Size)}, "offset": {strconv.Itoa(q.Offset)}}
	switch q.Type {
	case ListByYear:
		v.Set("fromYear", strconv.Itoa(q.FromYear))
		v.Set("toYear", strconv.Itoa(q.ToYear))
	case ListByGenre:
		v.Set("genre", q.Genre)
	}
	r, err := c.call(ctx, "getAlbumList2", v)
	if err != nil || r.AlbumList2 == nil {
		return nil, err
	}
	return r.AlbumList2.Albums, nil
}

func (c *Client) GetGenres(ctx context.Context) ([]Genre, error) {
	r, err := c.call(ctx, "getGenres", nil)
	if err != nil || r.Genres == nil {
		return nil, err
	}
	return r.Genres.Genres, nil
}

func (c *Client) GetPlaylists(ctx context.Context) ([]Playlist, error) {
	r, err := c.call(ctx, "getPlaylists", nil)
	if err != nil || r.Playlists == nil {
		return nil, err
	}
	return r.Playlists.Playlists, nil
}

func (c *Client) GetPlaylist(ctx context.Context, id ID) (*PlaylistWithSongs, error) {
	r, err := c.call(ctx, "getPlaylist", url.Values{"id": {string(id)}})
	if err != nil {
		return nil, err
	}
	if r.Playlist == nil {
		return nil, &APIError{Code: CodeNotFound, Message: "playlist not found"}
	}
	return r.Playlist, nil
}

func (c *Client) GetStarred2(ctx context.Context) (*Starred, error) {
	r, err := c.call(ctx, "getStarred2", nil)
	if err != nil {
		return nil, err
	}
	if r.Starred2 == nil {
		return &Starred{}, nil
	}
	return r.Starred2, nil
}

type SearchQuery struct {
	ArtistCount, ArtistOffset int
	AlbumCount, AlbumOffset   int
	SongCount, SongOffset     int
}

func (c *Client) Search3(ctx context.Context, query string, q SearchQuery) (*SearchResult, error) {
	v := url.Values{
		"query":        {query},
		"artistCount":  {strconv.Itoa(q.ArtistCount)},
		"artistOffset": {strconv.Itoa(q.ArtistOffset)},
		"albumCount":   {strconv.Itoa(q.AlbumCount)},
		"albumOffset":  {strconv.Itoa(q.AlbumOffset)},
		"songCount":    {strconv.Itoa(q.SongCount)},
		"songOffset":   {strconv.Itoa(q.SongOffset)},
	}
	r, err := c.call(ctx, "search3", v)
	if err != nil {
		return nil, err
	}
	if r.SearchResult3 == nil {
		return &SearchResult{}, nil
	}
	return r.SearchResult3, nil
}

type StarTarget struct {
	SongIDs, AlbumIDs, ArtistIDs []ID
}

func (t StarTarget) values() url.Values {
	v := url.Values{}
	for _, id := range t.SongIDs {
		v.Add("id", string(id))
	}
	for _, id := range t.AlbumIDs {
		v.Add("albumId", string(id))
	}
	for _, id := range t.ArtistIDs {
		v.Add("artistId", string(id))
	}
	return v
}

func (c *Client) Star(ctx context.Context, t StarTarget) error {
	_, err := c.call(ctx, "star", t.values())
	return err
}

func (c *Client) Unstar(ctx context.Context, t StarTarget) error {
	_, err := c.call(ctx, "unstar", t.values())
	return err
}

// Scrobble reports a play. submission=false only updates "now playing".
func (c *Client) Scrobble(ctx context.Context, id ID, at time.Time, submission bool) error {
	v := url.Values{
		"id":         {string(id)},
		"time":       {strconv.FormatInt(at.UnixMilli(), 10)},
		"submission": {strconv.FormatBool(submission)},
	}
	_, err := c.call(ctx, "scrobble", v)
	return err
}

// GetPlayQueue returns the saved queue, or nil if there is none.
func (c *Client) GetPlayQueue(ctx context.Context) (*PlayQueue, error) {
	r, err := c.call(ctx, "getPlayQueue", nil)
	var ae *APIError
	if errors.As(err, &ae) && ae.Code == CodeNotFound {
		return nil, nil
	}
	if err != nil || r.PlayQueue == nil || len(r.PlayQueue.Songs) == 0 {
		return nil, err
	}
	return r.PlayQueue, nil
}

func (c *Client) SavePlayQueue(ctx context.Context, ids []ID, current ID, pos time.Duration) error {
	v := url.Values{}
	for _, id := range ids {
		v.Add("id", string(id))
	}
	if current != "" {
		v.Set("current", string(current))
		v.Set("position", strconv.FormatInt(pos.Milliseconds(), 10))
	}
	_, err := c.call(ctx, "savePlayQueue", v)
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go vet ./internal/subsonic/ && go test -race ./internal/subsonic/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/subsonic
git commit -m "subsonic: browse, search, star, scrobble and play-queue endpoints" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Config file

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `go.mod` / `go.sum` (add `github.com/BurntSushi/toml`)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `config.Config{DefaultServer; Servers []Server; Playback; Display; Cache}`
  - `config.Server{Name, URL, Username, Token, Salt, Password, APIKey, CAFile string; InsecureSkipVerify, AllowPlaintextPassword bool}`
  - `config.Playback{TranscodeFormat string; TranscodeBitrate int; ReplayGain string; VolumeDB float64; Scrobble bool; BufferMB int; ALSADevice string}`
  - `config.Display{Profile string; ScreensaverMinutes int}`, `config.Cache{CoverArtMB int}`
  - Functions: `config.Default()`, `config.Load(path) (*Config, []string /*warnings*/, error)`, `(*Config).Validate() error`, `(*Config).ActiveServer() (*Server, bool)`, `config.Save(path, *Config) error` (atomic, mode 0600), `config.BackupInvalid(path, time.Time) (string, error)`
  - Values: `config.ErrNotFound`, `config.DefaultPath = "/media/fat/mistersubsonic/config.toml"`

Keys and defaults are exactly spec §9, with one tightening: `transcode_format` must be `mp3`, `flac` or `wav`, since those are the only formats the device can decode.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/BurntSushi/toml@v1.6.0`

- [ ] **Step 2: Write the failing tests**

`internal/config/config_test.go`:

```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const minimal = `
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "alice"
password = "sesame"
`

func TestLoadMinimalFillsDefaults(t *testing.T) {
	cfg, warns, err := Load(write(t, minimal))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings = %v", warns)
	}
	want := Default().Playback
	if cfg.Playback != want || cfg.Display.Profile != "auto" || cfg.Cache.CoverArtMB != 200 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	s, ok := cfg.ActiveServer()
	if !ok || s.Name != "home" {
		t.Fatalf("active server = %+v", s)
	}
}

func TestExplicitFalseOverridesDefaultTrue(t *testing.T) {
	cfg, _, err := Load(write(t, minimal+"\n[playback]\nscrobble = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Playback.Scrobble {
		t.Fatal("scrobble = false was ignored")
	}
}

func TestMissingFile(t *testing.T) {
	_, _, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownKeysAreWarnings(t *testing.T) {
	_, warns, err := Load(write(t, minimal+"\n[playback]\nreplay_gain = \"track\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "replay_gain") {
		t.Fatalf("warnings = %v", warns)
	}
}

func TestSyntaxErrorLeavesFileAlone(t *testing.T) {
	p := write(t, "[[server]\nname=")
	before, _ := os.ReadFile(p)
	_, _, err := Load(p)
	if err == nil {
		t.Fatal("malformed file loaded")
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("Load modified a malformed file")
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]string{
		"bad url":          strings.Replace(minimal, "http://192.168.1.10:4533", "192.168.1.10:4533", 1),
		"no credentials":   strings.Replace(minimal, `password = "sesame"`, "", 1),
		"token w/o salt":   strings.Replace(minimal, `password = "sesame"`, `token = "abc"`, 1),
		"no username":      strings.Replace(minimal, `username = "alice"`, "", 1),
		"bad replaygain":   minimal + "\n[playback]\nreplaygain = \"loud\"\n",
		"raw transcode":    minimal + "\n[playback]\ntranscode_format = \"raw\"\n",
		"opus transcode":   minimal + "\n[playback]\ntranscode_format = \"opus\"\n",
		"tiny buffer":      minimal + "\n[playback]\nbuffer_mb = 1\n",
		"bad profile":      minimal + "\n[display]\nprofile = \"vga\"\n",
		"unknown default":  "default_server = \"work\"\n" + minimal,
		"duplicate server": minimal + minimal,
	}
	for name, body := range cases {
		if _, _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: loaded without error", name)
		}
	}
}

func TestAPIKeyOnlyServerIsValid(t *testing.T) {
	body := "[[server]]\nname = \"k\"\nurl = \"https://music.example.com\"\napi_key = \"key\"\n"
	if _, _, err := Load(write(t, body)); err != nil {
		t.Fatal(err)
	}
}

func TestMultipleServersAndDefault(t *testing.T) {
	body := "default_server = \"work\"\n" + minimal + strings.Replace(minimal, `"home"`, `"work"`, 1)
	cfg, _, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := cfg.ActiveServer(); s.Name != "work" {
		t.Fatalf("active = %q", s.Name)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.toml")
	cfg := Default()
	cfg.Servers = []Server{{Name: "home", URL: "https://music.example.com", Username: "alice", Token: "t0k", Salt: "s4lt", CAFile: "/media/fat/ca.pem"}}
	cfg.Playback.ReplayGain = "album"
	cfg.Playback.Scrobble = false
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, warns, err := Load(p)
	if err != nil || len(warns) != 0 {
		t.Fatalf("reload: %v %v", err, warns)
	}
	if got.Servers[0] != cfg.Servers[0] || got.Playback != cfg.Playback {
		t.Fatalf("round trip changed config:\n got %+v\nwant %+v", got, cfg)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), "password") {
		t.Fatal("empty password written to file")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600 (it holds credentials)", fi.Mode().Perm())
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	cfg := Default()
	cfg.Playback.BufferMB = 0
	if err := Save(filepath.Join(t.TempDir(), "c.toml"), cfg); err == nil {
		t.Fatal("saved an invalid config")
	}
}

func TestBackupInvalid(t *testing.T) {
	p := write(t, "garbage")
	dst, err := BackupInvalid(p, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dst, "config.toml.invalid-20260928T120000Z") {
		t.Fatalf("backup name = %s", dst)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("original still present")
	}
}

// Configs edited in Windows Notepad over SMB arrive with a BOM and CRLFs.
func TestLoadToleratesBOMAndCRLF(t *testing.T) {
	body := "\xef\xbb\xbf" + strings.ReplaceAll(minimal, "\n", "\r\n")
	cfg, _, err := Load(write(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := cfg.ActiveServer(); s.Password != "sesame" {
		t.Fatalf("password = %q", s.Password)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/config/`
Expected: build failure, `undefined: Load`, `undefined: Default`, ….

- [ ] **Step 4: Implement**

`internal/config/config.go`:

```go
// Package config loads and saves config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultPath is where the app looks on a MiSTer.
const DefaultPath = "/media/fat/mistersubsonic/config.toml"

type Config struct {
	DefaultServer string   `toml:"default_server"`
	Servers       []Server `toml:"server"`
	Playback      Playback `toml:"playback"`
	Display       Display  `toml:"display"`
	Cache         Cache    `toml:"cache"`
}

type Server struct {
	Name                   string `toml:"name"`
	URL                    string `toml:"url"`
	Username               string `toml:"username"`
	Token                  string `toml:"token,omitempty"`
	Salt                   string `toml:"salt,omitempty"`
	Password               string `toml:"password,omitempty"`
	APIKey                 string `toml:"api_key,omitempty"`
	CAFile                 string `toml:"ca_file,omitempty"`
	InsecureSkipVerify     bool   `toml:"insecure_skip_verify,omitempty"`
	AllowPlaintextPassword bool   `toml:"allow_plaintext_password,omitempty"`
}

type Playback struct {
	TranscodeFormat  string  `toml:"transcode_format"`
	TranscodeBitrate int     `toml:"transcode_bitrate"`
	ReplayGain       string  `toml:"replaygain"`
	VolumeDB         float64 `toml:"volume_db"`
	Scrobble         bool    `toml:"scrobble"`
	BufferMB         int     `toml:"buffer_mb"`
	ALSADevice       string  `toml:"alsa_device"`
}

type Display struct {
	Profile            string `toml:"profile"`
	ScreensaverMinutes int    `toml:"screensaver_minutes"`
}

type Cache struct {
	CoverArtMB int `toml:"cover_art_mb"`
}

// Default returns a config with every default filled in and no servers.
func Default() *Config {
	return &Config{
		Playback: Playback{TranscodeFormat: "mp3", TranscodeBitrate: 320, ReplayGain: "off", Scrobble: true, BufferMB: 32, ALSADevice: "default"},
		Display:  Display{Profile: "auto", ScreensaverMinutes: 5},
		Cache:    Cache{CoverArtMB: 200},
	}
}

// ErrNotFound means there is no config file yet (first run).
var ErrNotFound = errors.New("config: file not found")

// Load reads path over the defaults. Unknown keys are returned as warnings,
// not errors. A malformed or invalid file is an error; the file is left alone.
func Load(path string) (cfg *Config, warnings []string, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	cfg = Default()
	md, err := toml.Decode(string(b), cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		warnings = append(warnings, "unknown key "+k.String())
	}
	if err := cfg.Validate(); err != nil {
		return nil, warnings, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, warnings, nil
}

// Validate checks values a user could plausibly get wrong.
func (c *Config) Validate() error {
	var errs []error
	names := map[string]bool{}
	for i, s := range c.Servers {
		where := fmt.Sprintf("server %d (%q)", i+1, s.Name)
		if s.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is empty", where))
		}
		if names[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", where))
		}
		names[s.Name] = true
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s: url must look like http://host:port or https://host", where))
		}
		hasToken := s.Token != "" && s.Salt != ""
		if (s.Token != "") != (s.Salt != "") {
			errs = append(errs, fmt.Errorf("%s: token and salt must be set together", where))
		}
		if !hasToken && s.Password == "" && s.APIKey == "" {
			errs = append(errs, fmt.Errorf("%s: set password, token+salt, or api_key", where))
		}
		if s.APIKey == "" && s.Username == "" {
			errs = append(errs, fmt.Errorf("%s: username is empty", where))
		}
	}
	if c.DefaultServer != "" && !names[c.DefaultServer] {
		errs = append(errs, fmt.Errorf("default_server %q is not one of the servers", c.DefaultServer))
	}
	switch c.Playback.ReplayGain {
	case "off", "track", "album":
	default:
		errs = append(errs, fmt.Errorf("playback.replaygain must be off, track or album (got %q)", c.Playback.ReplayGain))
	}
	switch c.Playback.TranscodeFormat {
	case "mp3", "flac", "wav":
	default:
		errs = append(errs, fmt.Errorf("playback.transcode_format must be mp3, flac or wav (got %q)", c.Playback.TranscodeFormat))
	}
	if c.Playback.BufferMB < 4 || c.Playback.BufferMB > 512 {
		errs = append(errs, fmt.Errorf("playback.buffer_mb must be 4..512 (got %d)", c.Playback.BufferMB))
	}
	if c.Playback.VolumeDB > 0 || c.Playback.VolumeDB < -60 {
		errs = append(errs, fmt.Errorf("playback.volume_db must be -60..0 (got %v)", c.Playback.VolumeDB))
	}
	switch c.Display.Profile {
	case "auto", "hdmi", "crt":
	default:
		errs = append(errs, fmt.Errorf("display.profile must be auto, hdmi or crt (got %q)", c.Display.Profile))
	}
	if c.Display.ScreensaverMinutes < 0 {
		errs = append(errs, errors.New("display.screensaver_minutes must be >= 0"))
	}
	if c.Cache.CoverArtMB < 0 {
		errs = append(errs, errors.New("cache.cover_art_mb must be >= 0"))
	}
	return errors.Join(errs...)
}

// ActiveServer is the default server, or the first one.
func (c *Config) ActiveServer() (*Server, bool) {
	for i := range c.Servers {
		if c.Servers[i].Name == c.DefaultServer {
			return &c.Servers[i], true
		}
	}
	if len(c.Servers) > 0 {
		return &c.Servers[0], true
	}
	return nil, false
}

// Save writes cfg atomically (temp file + rename) after validating it.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config: refusing to save invalid config: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("# MiSTer Subsonic configuration. See config.example.toml for every option.\n")
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// BackupInvalid renames a broken config out of the way before the wizard
// writes a new one, returning the new name.
func BackupInvalid(path string, now time.Time) (string, error) {
	dst := path + ".invalid-" + strings.ReplaceAll(now.UTC().Format("20060102T150405Z"), ":", "")
	if err := os.Rename(path, dst); err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return dst, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go mod tidy && go vet ./internal/config/ && go test -race ./internal/config/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "config: TOML config with defaults, validation and atomic save" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 9: Player: stream planning, opener, ReplayGain, persistence

**Files:**
- Create: `internal/player/stream.go`, `internal/player/persist.go`
- Test: `internal/player/stream_test.go`, `internal/player/opener_test.go`, `internal/player/persist_test.go`

**Interfaces:**
- Consumes:
  - `audio.Format`, `audio.FormatFromSuffix` (Task 2)
  - `stream.Open`, `stream.Options` (Task 5)
  - `subsonic.Client.HTTPClient/StreamURL`, `subsonic.CheckStreamResponse`, `subsonic.Song`, `subsonic.StreamOptions`, `subsonic.ID` (Tasks 6–7)
- Produces:
  - `player.Opened{Source io.ReadSeeker; Format audio.Format; Transcoded bool; Offset time.Duration}`
  - `player.Opener func(ctx, subsonic.Song, offset time.Duration, prefetch bool) (Opened, error)`
  - `player.Promoter` interface
  - `player.StreamSettings{TranscodeFormat string; TranscodeBitrate int; WindowBytes, PrefetchBytes int64}`
  - `player.PlanStream(Song, StreamSettings) (subsonic.StreamOptions, audio.Format, bool)`
  - `player.NewOpener(*subsonic.Client, StreamSettings) Opener`
  - `player.ReplayGainFactor(Song, mode string) float32`
  - `player.Resume{Songs; Index; Position}`, `SaveResume`, `LoadResume`
  - `player.Scrobble{ID; At}`, `player.ScrobbleQueue` (`LoadScrobbleQueue(path)`, `Add`, `Flush(ctx, API)`, `Len`), `MaxQueuedScrobbles = 500`
  - `player.API` is declared in Task 10's `player.go`. `persist.go` references it, so **this task's package won't build until Task 10**. To keep Task 9 self-contained, Step 3 includes a temporary `api.go` holding the `API` interface. Task 10 deletes that file when `player.go` takes the interface over.

- [ ] **Step 1: Write the failing tests**

`internal/player/stream_test.go`:

```go
package player

import (
	"testing"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

func TestPlanStream(t *testing.T) {
	st := StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320}
	cases := []struct {
		song       subsonic.Song
		format     string
		bitrate    int
		f          audio.Format
		transcoded bool
	}{
		{subsonic.Song{Suffix: "flac"}, "raw", 0, audio.FormatFLAC, false},
		{subsonic.Song{Suffix: "MP3"}, "raw", 0, audio.FormatMP3, false},
		{subsonic.Song{Suffix: "wav"}, "raw", 0, audio.FormatWAV, false},
		{subsonic.Song{Suffix: "", ContentType: "audio/x-flac"}, "raw", 0, audio.FormatFLAC, false},
		{subsonic.Song{Suffix: "m4a", ContentType: "audio/mp4"}, "mp3", 320, audio.FormatMP3, true},
		{subsonic.Song{Suffix: "opus"}, "mp3", 320, audio.FormatMP3, true},
	}
	for _, c := range cases {
		o, f, tr := PlanStream(c.song, st)
		if o.Format != c.format || o.MaxBitRate != c.bitrate || f != c.f || tr != c.transcoded {
			t.Errorf("PlanStream(%+v) = %+v %v %v", c.song, o, f, tr)
		}
	}
	o, f, _ := PlanStream(subsonic.Song{Suffix: "m4a"}, StreamSettings{TranscodeFormat: "flac"})
	if o.Format != "flac" || o.MaxBitRate != 0 || f != audio.FormatFLAC {
		t.Errorf("flac transcode = %+v %v", o, f)
	}
}

func f64(v float64) *float64 { return &v }

func TestReplayGainFactor(t *testing.T) {
	both := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(-6), AlbumGain: f64(-3)}}
	near := func(got, want float32) bool { return got > want-0.005 && got < want+0.005 }
	if g := ReplayGainFactor(both, "off"); g != 1 {
		t.Errorf("off = %v", g)
	}
	if g := ReplayGainFactor(both, "track"); !near(g, 0.501) {
		t.Errorf("track = %v", g)
	}
	if g := ReplayGainFactor(both, "album"); !near(g, 0.708) {
		t.Errorf("album = %v", g)
	}
	onlyTrack := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(-6)}}
	if g := ReplayGainFactor(onlyTrack, "album"); !near(g, 0.501) {
		t.Errorf("album falls back to track: %v", g)
	}
	if g := ReplayGainFactor(subsonic.Song{}, "track"); g != 1 {
		t.Errorf("no data = %v", g)
	}
	loud := subsonic.Song{ReplayGain: &subsonic.ReplayGain{TrackGain: f64(6), TrackPeak: f64(0.9)}}
	if g := ReplayGainFactor(loud, "track"); !near(g, 1/0.9) {
		t.Errorf("peak-limited gain = %v, want %v", g, 1/0.9)
	}
}
```

`internal/player/opener_test.go`:

```go
package player

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mistersubsonic/internal/subsonic"
)

// When transcoding isn't configured on the server, stream answers with an
// error document instead of audio; that must fail the track, not play noise.
func TestOpenerRejectsErrorDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stream.view") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":0,"message":"transcoding not configured"}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c, err := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	open := NewOpener(c, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
	_, err = open(context.Background(), subsonic.Song{ID: "x", Suffix: "m4a"}, 0, false)
	var ae *subsonic.APIError
	if !errors.As(err, &ae) || !strings.Contains(ae.Message, "transcoding") {
		t.Fatalf("err = %v, want the server's APIError", err)
	}
}
```

`internal/player/persist_test.go`:

```go
package player

import (
	"strings"
	"testing"

	"mistersubsonic/internal/subsonic"
)

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	if r, err := LoadResume(dir + "/missing.json"); r != nil || err != nil {
		t.Fatalf("missing resume = %v, %v", r, err)
	}
	q := LoadScrobbleQueue(dir + "/scrobbles.json")
	for i := 0; i < MaxQueuedScrobbles+10; i++ {
		q.Add(Scrobble{ID: subsonic.ID(strings.Repeat("x", 1+i%3))})
	}
	if q.Len() != MaxQueuedScrobbles {
		t.Fatalf("queue len = %d, want cap %d", q.Len(), MaxQueuedScrobbles)
	}
	if again := LoadScrobbleQueue(dir + "/scrobbles.json"); again.Len() != MaxQueuedScrobbles {
		t.Fatalf("reloaded len = %d", again.Len())
	}
	writeJSONAtomic(dir+"/bad.json", "not a list")
	if bad := LoadScrobbleQueue(dir + "/bad.json"); bad.Len() != 0 {
		t.Fatal("corrupt queue file should load empty")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/player/`
Expected: build failure, `undefined: PlanStream`, `undefined: NewOpener`, `undefined: LoadScrobbleQueue`, ….

- [ ] **Step 3: Implement**

`internal/player/stream.go`:

```go
package player

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/stream"
	"mistersubsonic/internal/subsonic"
)

// Opened is a song's byte stream, ready for the engine.
type Opened struct {
	Source     io.ReadSeeker // also an io.Closer
	Format     audio.Format
	Transcoded bool
	Offset     time.Duration // song position of the first byte (transcoded seeks)
}

// Opener opens a song's stream. prefetch=true asks for a small read-ahead
// until the source is promoted (see Promoter).
type Opener func(ctx context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error)

// Promoter is implemented by sources opened in prefetch mode.
type Promoter interface{ Promote() }

type StreamSettings struct {
	TranscodeFormat  string // mp3 | flac | wav
	TranscodeBitrate int    // kbps, for lossy targets
	WindowBytes      int64
	PrefetchBytes    int64 // default 4 MiB
}

var contentTypes = map[string]audio.Format{
	"audio/flac": audio.FormatFLAC, "audio/x-flac": audio.FormatFLAC,
	"audio/mpeg": audio.FormatMP3, "audio/mp3": audio.FormatMP3,
	"audio/wav": audio.FormatWAV, "audio/x-wav": audio.FormatWAV, "audio/wave": audio.FormatWAV,
}

// PlanStream decides between the original file and a server transcode.
func PlanStream(s subsonic.Song, st StreamSettings) (subsonic.StreamOptions, audio.Format, bool) {
	f := audio.FormatFromSuffix(s.Suffix)
	if f == audio.FormatUnknown {
		f = contentTypes[strings.ToLower(s.ContentType)]
	}
	if f != audio.FormatUnknown {
		return subsonic.StreamOptions{Format: "raw"}, f, false
	}
	o := subsonic.StreamOptions{Format: st.TranscodeFormat}
	if st.TranscodeFormat == "mp3" {
		o.MaxBitRate = st.TranscodeBitrate
	}
	return o, audio.FormatFromSuffix(st.TranscodeFormat), true
}

// NewOpener opens songs through the Subsonic stream endpoint.
func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
	if st.PrefetchBytes <= 0 {
		st.PrefetchBytes = 4 << 20
	}
	return func(ctx context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error) {
		so, f, transcoded := PlanStream(s, st)
		if f == audio.FormatUnknown {
			return Opened{}, fmt.Errorf("player: transcode format %q can't be decoded", st.TranscodeFormat)
		}
		var start time.Duration
		if transcoded && offset > 0 {
			so.TimeOffset = int(offset / time.Second)
			start = time.Duration(so.TimeOffset) * time.Second
		}
		o := stream.Options{Client: c.HTTPClient(), CheckResponse: subsonic.CheckStreamResponse, WindowBytes: st.WindowBytes}
		if prefetch {
			o.PrefetchBytes = st.PrefetchBytes
		}
		r, err := stream.Open(ctx, c.StreamURL(s.ID, so), o)
		if err != nil {
			return Opened{}, err
		}
		return Opened{Source: r, Format: f, Transcoded: transcoded, Offset: start}, nil
	}
}

// ReplayGainFactor converts the song's ReplayGain to a linear factor for
// mode "track" or "album", falling back to the other value, and limits it
// so the known peak doesn't clip. No data or mode "off" gives 1.
func ReplayGainFactor(s subsonic.Song, mode string) float32 {
	rg := s.ReplayGain
	if rg == nil || mode == "off" || mode == "" {
		return 1
	}
	gain, peak := rg.TrackGain, rg.TrackPeak
	if mode == "album" {
		gain, peak = rg.AlbumGain, rg.AlbumPeak
	}
	if gain == nil {
		gain, peak = rg.AlbumGain, rg.AlbumPeak
		if mode == "album" {
			gain, peak = rg.TrackGain, rg.TrackPeak
		}
	}
	if gain == nil {
		return 1
	}
	f := math.Pow(10, *gain/20)
	if peak != nil && *peak > 0 && f**peak > 1 {
		f = 1 / *peak
	}
	return float32(f)
}
```

`internal/player/persist.go`:

```go
package player

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mistersubsonic/internal/subsonic"
)

// Resume is a saved queue position.
type Resume struct {
	Songs    []subsonic.Song `json:"songs"`
	Index    int             `json:"index"`
	Position time.Duration   `json:"position"`
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveResume writes the local resume file.
func SaveResume(path string, r Resume) error { return writeJSONAtomic(path, r) }

// LoadResume reads the local resume file; (nil, nil) if absent.
func LoadResume(path string) (*Resume, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Resume
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if len(r.Songs) == 0 || r.Index < 0 || r.Index >= len(r.Songs) {
		return nil, nil
	}
	return &r, nil
}

// Scrobble is a submission waiting to be retried.
type Scrobble struct {
	ID subsonic.ID `json:"id"`
	At time.Time   `json:"at"`
}

// MaxQueuedScrobbles caps the retry queue; the oldest entries are dropped.
const MaxQueuedScrobbles = 500

// ScrobbleQueue persists failed scrobble submissions.
type ScrobbleQueue struct {
	path  string
	mu    sync.Mutex
	items []Scrobble
}

// LoadScrobbleQueue reads path; a missing or corrupt file gives an empty queue.
func LoadScrobbleQueue(path string) *ScrobbleQueue {
	q := &ScrobbleQueue{path: path}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &q.items)
	}
	return q
}

func (q *ScrobbleQueue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }

func (q *ScrobbleQueue) Add(s Scrobble) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, s)
	if len(q.items) > MaxQueuedScrobbles {
		q.items = q.items[len(q.items)-MaxQueuedScrobbles:]
	}
	q.saveLocked()
}

// Flush submits queued scrobbles in order, stopping at the first failure.
func (q *ScrobbleQueue) Flush(ctx context.Context, api API) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	sent := 0
	var err error
	for _, s := range q.items {
		if err = api.Scrobble(ctx, s.ID, s.At, true); err != nil {
			break
		}
		sent++
	}
	if sent > 0 {
		q.items = q.items[sent:]
		q.saveLocked()
	}
	return err
}

func (q *ScrobbleQueue) saveLocked() {
	if q.path != "" {
		writeJSONAtomic(q.path, q.items)
	}
}
```

`internal/player/api.go` (temporary; deleted in Task 10):

```go
package player

import (
	"context"
	"time"

	"mistersubsonic/internal/subsonic"
)

// API is the subset of *subsonic.Client the player uses.
type API interface {
	Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error
	SavePlayQueue(ctx context.Context, ids []subsonic.ID, current subsonic.ID, pos time.Duration) error
	GetPlayQueue(ctx context.Context) (*subsonic.PlayQueue, error)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go vet ./internal/player/ && go test -race ./internal/player/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/player
git commit -m "player: stream planning, opener, ReplayGain and persistence" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 10: Player core (queue, gapless handover, scrobbling, resume)

**Files:**
- Create: `internal/player/player.go`
- Delete: `internal/player/api.go`, because `player.go` now declares `API`
- Test: `internal/player/fakes_test.go`, `internal/player/player_test.go`

**Interfaces:**
- Consumes:
  - `audio.Track`, `audio.Event*`, `audio.Engine`'s method set (Task 4)
  - `Opener`, `Opened`, `Promoter`, `ReplayGainFactor`, `Resume`, `SaveResume`, `LoadResume`, `ScrobbleQueue` (Task 9)
  - `subsonic.Song`, `ID`, `PlayQueue`
- Produces:
  - `player.Engine` interface (`*audio.Engine` satisfies it) and `player.API` interface (`*subsonic.Client` satisfies it)
  - `player.Status` (`Stopped`, `Loading`, `Playing`, `Paused`, `Buffering`), `player.Repeat` (`RepeatOff`, `RepeatAll`, `RepeatOne`)
  - `player.State{Queue; Index; Status; Position; Shuffle; Repeat; VolumeDB; Transcoded}` with `Current()`
  - `player.EventKind` (`TrackChanged`, `StatusChanged`, `QueueChanged`, `Error`), `player.Event{Kind; Song; Err}`
  - `player.Options{Engine; API; Open; ReplayGain; Scrobble; ResumePath; ScrobblePath; Tick; Now; Rand; OpenTimeout; VolumeDB; PrefetchAhead}`
  - `player.New(Options) *Player`
  - `*Player` methods: `Run(ctx)`, `State()`, `Events()`, `PlayNow(songs, start)`, `PlayNext`, `Enqueue`, `Remove(i)`, `Clear`, `Jump(i)`, `Next`, `Prev`, `TogglePause`, `Seek(d)`, `SetShuffle`, `SetRepeat`, `SetVolumeDB`, `Resumable(ctx) (*Resume, error)`, `ResumeFrom(*Resume)`

**Design points:**
- **Concurrency:** all state lives on the `Run` goroutine. Public methods run closures on it through `do`, and **publish the snapshot before returning**, so `State()` right after a command reflects it; the race tests caught this.
- **Queue storage:** `queue` is copy-on-write, so snapshots can share it.
- **Gapless handover:**
  - Prefetch starts `PrefetchAhead` (20 s) before the end, using the known duration.
  - A queued successor becomes current on its engine `Started` event; the engine's `Ended` for the old track is ignored then.
  - If the track ends before the prefetch was handed to the engine, the player falls back to a normal `startAt`.
- **Seeking:**
  - Raw seeks go through `engine.Seek`; if that fails, the song is reopened and seeked.
  - Transcoded seeks reopen with `timeOffset`.
- **Scrobbling:**
  - Listening time accumulates from position deltas under 2 s, so seeks don't count.
  - A submission is sent once, when `min(duration/2, 240 s)` has been listened.
  - A failed submission goes to the persistent retry queue, which is flushed after the next success and at start.
- **Buffering:** the status becomes `Buffering` when the position hasn't moved for more than 1 s while playing.
- **Saving:** the queue is saved every 30 s while playing or paused (locally, and on the server via `savePlayQueue`), and again synchronously on exit.

- [ ] **Step 1: Write the fakes and harness**

`internal/player/fakes_test.go`:

```go
package player

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

type seekCall struct {
	id  uint64
	pos time.Duration
}

type fakeEngine struct {
	mu      sync.Mutex
	played  []audio.Track
	queued  []audio.Track
	clears  int
	stops   int
	seeks   []seekCall
	seekErr error
	paused  bool
	volume  float32
	posID   uint64
	pos     time.Duration
	posOK   bool
	events  chan audio.Event
}

func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan audio.Event, 64)} }

func (e *fakeEngine) Play(t audio.Track) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.played = append(e.played, t)
	e.posID, e.pos, e.posOK = t.ID, t.Offset, true
}
func (e *fakeEngine) QueueNext(t audio.Track) {
	e.mu.Lock()
	e.queued = append(e.queued, t)
	e.mu.Unlock()
}
func (e *fakeEngine) ClearNext()                 { e.mu.Lock(); e.clears++; e.mu.Unlock() }
func (e *fakeEngine) Stop()                      { e.mu.Lock(); e.stops++; e.posOK = false; e.mu.Unlock() }
func (e *fakeEngine) SetPaused(p bool)           { e.mu.Lock(); e.paused = p; e.mu.Unlock() }
func (e *fakeEngine) SetVolume(v float32)        { e.mu.Lock(); e.volume = v; e.mu.Unlock() }
func (e *fakeEngine) Events() <-chan audio.Event { return e.events }
func (e *fakeEngine) Seek(id uint64, pos time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seeks = append(e.seeks, seekCall{id, pos})
	if e.seekErr != nil {
		return e.seekErr
	}
	e.pos = pos
	return nil
}
func (e *fakeEngine) Position() (uint64, time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.posID, e.pos, e.posOK
}
func (e *fakeEngine) setPos(id uint64, pos time.Duration) {
	e.mu.Lock()
	e.posID, e.pos, e.posOK = id, pos, true
	e.mu.Unlock()
}
func (e *fakeEngine) lastPlayed() audio.Track {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.played[len(e.played)-1]
}
func (e *fakeEngine) playCount() int  { e.mu.Lock(); defer e.mu.Unlock(); return len(e.played) }
func (e *fakeEngine) queueCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.queued) }

type scrobbleCall struct {
	id         subsonic.ID
	submission bool
}

type fakeAPI struct {
	mu        sync.Mutex
	scrobbles []scrobbleCall
	failNext  int // fail this many submission=true calls
	saves     [][]subsonic.ID
	saveCur   subsonic.ID
	savePos   time.Duration
	queue     *subsonic.PlayQueue
}

func (a *fakeAPI) Scrobble(_ context.Context, id subsonic.ID, _ time.Time, sub bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sub && a.failNext > 0 {
		a.failNext--
		return errors.New("offline")
	}
	a.scrobbles = append(a.scrobbles, scrobbleCall{id, sub})
	return nil
}
func (a *fakeAPI) SavePlayQueue(_ context.Context, ids []subsonic.ID, cur subsonic.ID, pos time.Duration) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saves = append(a.saves, ids)
	a.saveCur, a.savePos = cur, pos
	return nil
}
func (a *fakeAPI) GetPlayQueue(context.Context) (*subsonic.PlayQueue, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.queue, nil
}
func (a *fakeAPI) submissions() []subsonic.ID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []subsonic.ID
	for _, s := range a.scrobbles {
		if s.submission {
			ids = append(ids, s.id)
		}
	}
	return ids
}
func (a *fakeAPI) nowPlayings() []subsonic.ID {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []subsonic.ID
	for _, s := range a.scrobbles {
		if !s.submission {
			ids = append(ids, s.id)
		}
	}
	return ids
}

type fakeSource struct {
	song     subsonic.ID
	promoted bool
	closed   bool
	mu       sync.Mutex
}

func (s *fakeSource) Read([]byte) (int, error)       { return 0, io.EOF }
func (s *fakeSource) Seek(int64, int) (int64, error) { return 0, nil }
func (s *fakeSource) Close() error                   { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }
func (s *fakeSource) Promote()                       { s.mu.Lock(); s.promoted = true; s.mu.Unlock() }
func (s *fakeSource) isPromoted() bool               { s.mu.Lock(); defer s.mu.Unlock(); return s.promoted }

type openCall struct {
	id       subsonic.ID
	offset   time.Duration
	prefetch bool
}

type fakeOpener struct {
	mu    sync.Mutex
	calls []openCall
	fail  map[subsonic.ID]bool
}

func (o *fakeOpener) open(_ context.Context, s subsonic.Song, offset time.Duration, prefetch bool) (Opened, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, openCall{s.ID, offset, prefetch})
	if o.fail[s.ID] {
		return Opened{}, errors.New("cannot open " + string(s.ID))
	}
	_, f, transcoded := PlanStream(s, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
	op := Opened{Source: &fakeSource{song: s.ID}, Format: f, Transcoded: transcoded}
	if transcoded {
		op.Offset = offset
	}
	return op, nil
}
func (o *fakeOpener) callList() []openCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]openCall(nil), o.calls...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type harness struct {
	t      *testing.T
	p      *Player
	eng    *fakeEngine
	api    *fakeAPI
	opener *fakeOpener
	clock  *clock
	tick   chan time.Time
	cancel context.CancelFunc
	done   chan struct{}
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	h := &harness{t: t, eng: newFakeEngine(), api: &fakeAPI{}, opener: &fakeOpener{fail: map[subsonic.ID]bool{}},
		clock: &clock{t: time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)}, tick: make(chan time.Time), done: make(chan struct{})}
	o := Options{Engine: h.eng, API: h.api, Open: h.opener.open, Scrobble: true, Tick: h.tick, Now: h.clock.now,
		Rand: rand.New(rand.NewPCG(1, 2)), ScrobblePath: t.TempDir() + "/scrobbles.json", ResumePath: t.TempDir() + "/state.json"}
	if mutate != nil {
		mutate(&o)
	}
	h.p = New(o)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() { h.p.Run(ctx); close(h.done) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s (state %+v)", what, h.p.State())
		}
		time.Sleep(time.Millisecond)
	}
}

// started delivers the engine's Started event for the last played track.
func (h *harness) playAndStart(wantPlays int) audio.Track {
	h.t.Helper()
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() >= wantPlays })
	tr := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
	h.waitFor("playing", func() bool { return h.p.State().Status == Playing })
	return tr
}

// tickAt sets the engine position and runs one tick.
func (h *harness) tickAt(id uint64, pos time.Duration) {
	h.eng.setPos(id, pos)
	h.clock.advance(250 * time.Millisecond)
	h.tick <- h.clock.now()
	h.p.do(func() {})
}

func songs(n int, durationSec int) []subsonic.Song {
	out := make([]subsonic.Song, n)
	for i := range out {
		out[i] = subsonic.Song{ID: subsonic.ID("s" + string(rune('a'+i))), Title: "Song " + string(rune('A'+i)), Suffix: "flac", Duration: durationSec}
	}
	return out
}
```

- [ ] **Step 2: Write the failing tests**

`internal/player/player_test.go`:

```go
package player

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

func TestPlayNowOpensAndReportsNowPlaying(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.ReplayGain = "track" })
	q := songs(3, 200)
	g := -6.0
	q[1].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
	h.p.PlayNow(q, 1)
	tr := h.playAndStart(1)
	if tr.Format != audio.FormatFLAC || tr.Gain < 0.50 || tr.Gain > 0.51 {
		t.Fatalf("track = %+v, want FLAC with -6 dB gain", tr)
	}
	st := h.p.State()
	if cur, _ := st.Current(); cur.ID != "sb" || st.Index != 1 {
		t.Fatalf("current = %v index %d", cur.ID, st.Index)
	}
	h.waitFor("now playing", func() bool { return slices.Equal(h.api.nowPlayings(), []subsonic.ID{"sb"}) })
}

func TestGaplessPrefetchAndHandover(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 0)
	a := h.playAndStart(1)

	h.tickAt(a.ID, 50*time.Second)
	if calls := h.opener.callList(); len(calls) != 1 {
		t.Fatalf("prefetched too early: %v", calls)
	}
	h.tickAt(a.ID, 81*time.Second)
	h.waitFor("QueueNext", func() bool { return h.eng.queueCount() == 1 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.id != "sb" || !last.prefetch {
		t.Fatalf("prefetch call = %+v", last)
	}
	b := h.eng.queued[0]

	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: b.ID}
	h.waitFor("handover", func() bool { return h.p.State().Index == 1 })
	if h.eng.playCount() != 1 {
		t.Fatal("gapless handover must not call engine.Play")
	}
	if !b.Source.(*fakeSource).isPromoted() {
		t.Fatal("prefetched source not promoted once it started")
	}
	h.waitFor("now playing for b", func() bool { return len(h.api.nowPlayings()) == 2 })
}

func TestEndOfQueueStops(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 100), 0)
	a := h.playAndStart(1)
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("stopped", func() bool { return h.p.State().Status == Stopped })
}

func TestEndBeforePrefetchOpenedFallsBackToPlay(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 0)
	a := h.playAndStart(1)
	// Pretend a prefetch is in flight but not yet handed to the engine.
	h.p.do(func() { h.p.seq++; h.p.nextID, h.p.nextCursor, h.p.prefetched = h.p.seq, 1, a.ID })
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("second song played", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Index != 1 {
		t.Fatalf("index = %d", h.p.State().Index)
	}
}

func TestRepeatAllWrapsAndRepeatOneReplays(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 100), 1)
	h.p.SetRepeat(RepeatAll)
	b := h.playAndStart(1)
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: b.ID}
	h.waitFor("wrap to first", func() bool { return h.eng.playCount() == 2 && h.p.State().Index == 0 })

	h.p.SetRepeat(RepeatOne)
	a := h.playAndStart(2)
	h.tickAt(a.ID, 90*time.Second)
	if h.eng.queueCount() != 0 {
		t.Fatal("repeat-one must not prefetch")
	}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("replay", func() bool { return h.eng.playCount() == 3 && h.p.State().Index == 0 })
}

func TestShuffleKeepsCurrentFirstAndCoversQueue(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(8, 100), 3)
	h.playAndStart(1)
	h.p.SetShuffle(true)
	var order []int
	var cursor int
	h.p.do(func() { order, cursor = append([]int(nil), h.p.order...), h.p.cursor })
	if order[cursor] != 3 {
		t.Fatalf("current moved: order %v cursor %d", order, cursor)
	}
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []int{0, 1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("shuffled order %v is not a permutation", order)
	}
	h.p.SetShuffle(false)
	if st := h.p.State(); st.Index != 3 || st.Shuffle {
		t.Fatalf("after unshuffle index=%d shuffle=%v", st.Index, st.Shuffle)
	}
}

func TestScrobbleAfterHalfAndSeeksDontCount(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0) // threshold min(150s, 240s) = 150s
	a := h.playAndStart(1)
	pos := time.Duration(0)
	step := func(n int) {
		for i := 0; i < n; i++ {
			pos += 250 * time.Millisecond
			h.tickAt(a.ID, pos)
		}
	}
	step(300)               // 75 s listened
	pos += 60 * time.Second // a seek forward
	h.tickAt(a.ID, pos)     // jump is not listening
	step(4 * 74)            // 149 s listened
	time.Sleep(20 * time.Millisecond)
	if len(h.api.submissions()) != 0 {
		t.Fatal("scrobbled before 150 s of listening")
	}
	step(8)
	h.waitFor("scrobble", func() bool { return slices.Equal(h.api.submissions(), []subsonic.ID{"sa"}) })
	step(100)
	time.Sleep(20 * time.Millisecond)
	if len(h.api.submissions()) != 1 {
		t.Fatal("scrobbled twice")
	}
}

func TestFailedScrobbleIsQueuedAndRetried(t *testing.T) {
	h := newHarness(t, nil)
	h.api.failNext = 1
	q := songs(2, 8) // threshold 4 s
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	for i := 1; i <= 20; i++ {
		h.tickAt(a.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("queued", func() bool { return h.p.scrobbles.Len() == 1 })

	h.p.Next()
	b := h.playAndStart(2)
	for i := 1; i <= 20; i++ {
		h.tickAt(b.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("both submitted", func() bool {
		s := h.api.submissions()
		return len(s) == 2 && slices.Contains(s, "sa") && slices.Contains(s, "sb")
	})
	h.waitFor("queue drained", func() bool { return h.p.scrobbles.Len() == 0 })
}

func TestThreeFailuresInARowStop(t *testing.T) {
	h := newHarness(t, nil)
	for _, id := range []subsonic.ID{"sa", "sb", "sc"} {
		h.opener.fail[id] = true
	}
	h.p.PlayNow(songs(5, 100), 0)
	h.waitFor("stopped", func() bool { return h.p.State().Status == Stopped && len(h.opener.callList()) == 3 })
	errs := 0
	for len(h.p.Events()) > 0 {
		if ev := <-h.p.Events(); ev.Kind == Error {
			errs++
		}
	}
	if errs != 3 {
		t.Fatalf("error events = %d, want 3", errs)
	}
}

func TestPrevRestartsOrGoesBack(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 1)
	b := h.playAndStart(1)
	h.tickAt(b.ID, 10*time.Second)
	h.p.Prev()
	h.waitFor("restart", func() bool { return h.eng.playCount() == 2 })
	if h.p.State().Index != 1 {
		t.Fatal("Prev after 10 s should restart the same song")
	}
	b2 := h.playAndStart(2)
	h.tickAt(b2.ID, time.Second)
	h.p.Prev()
	h.waitFor("previous", func() bool { return h.eng.playCount() == 3 && h.p.State().Index == 0 })
}

func TestSeekRawUsesEngineAndTranscodedReopens(t *testing.T) {
	h := newHarness(t, nil)
	q := songs(2, 300)
	q[1].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	a := h.playAndStart(1)
	h.p.Seek(42 * time.Second)
	if len(h.eng.seeks) != 1 || h.eng.seeks[0] != (seekCall{a.ID, 42 * time.Second}) {
		t.Fatalf("seeks = %v", h.eng.seeks)
	}
	h.p.Next()
	h.playAndStart(2)
	if !h.p.State().Transcoded {
		t.Fatal("m4a should be transcoded")
	}
	h.p.Seek(90 * time.Second)
	h.waitFor("reopen", func() bool { return h.eng.playCount() == 3 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.id != "sb" || last.offset != 90*time.Second {
		t.Fatalf("reopen call = %+v", last)
	}
	if tr := h.eng.lastPlayed(); tr.Offset != 90*time.Second {
		t.Fatalf("reopened track offset = %v", tr.Offset)
	}
}

func TestQueueEditing(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 0) // sa sb sc
	a := h.playAndStart(1)
	h.tickAt(a.ID, 85*time.Second) // prefetches sb
	h.waitFor("queued", func() bool { return h.eng.queueCount() == 1 })

	h.p.PlayNext([]subsonic.Song{{ID: "x", Suffix: "flac", Duration: 100}})
	if h.eng.clears == 0 {
		t.Fatal("PlayNext must drop the prepared successor")
	}
	h.p.Enqueue([]subsonic.Song{{ID: "y", Suffix: "flac", Duration: 100}})
	ids := func() []subsonic.ID {
		var out []subsonic.ID
		for _, s := range h.p.State().Queue {
			out = append(out, s.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []subsonic.ID{"sa", "x", "sb", "sc", "y"}) {
		t.Fatalf("queue = %v", got)
	}
	h.p.Remove(0) // current: plays "x" next
	h.waitFor("x plays", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "x" {
		t.Fatalf("current after removing = %v", cur.ID)
	}
	h.p.Remove(3) // "y", not current
	if got := ids(); !slices.Equal(got, []subsonic.ID{"x", "sb", "sc"}) {
		t.Fatalf("queue = %v", got)
	}
	h.p.Clear()
	if st := h.p.State(); len(st.Queue) != 0 || st.Status != Stopped || st.Index != -1 {
		t.Fatalf("after Clear: %+v", st)
	}
}

func TestPauseResumeAndVolume(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 100), 0)
	h.playAndStart(1)
	h.p.TogglePause()
	if st := h.p.State(); st.Status != Paused || !h.eng.paused {
		t.Fatalf("status %v engine paused %v", st.Status, h.eng.paused)
	}
	h.p.TogglePause()
	if h.p.State().Status != Playing || h.eng.paused {
		t.Fatal("did not resume")
	}
	h.p.SetVolumeDB(-6)
	if v := h.eng.volume; v < 0.50 || v > 0.51 {
		t.Fatalf("volume = %v", v)
	}
	h.p.SetVolumeDB(12)
	if h.p.State().VolumeDB != 0 {
		t.Fatal("volume not clamped to 0 dB")
	}
}

func TestBufferingDetection(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	for i := 0; i < 6; i++ { // 1.5 s without movement
		h.tickAt(a.ID, time.Second)
	}
	if h.p.State().Status != Buffering {
		t.Fatalf("status = %v, want buffering", h.p.State().Status)
	}
	h.tickAt(a.ID, 1250*time.Millisecond)
	if h.p.State().Status != Playing {
		t.Fatalf("status = %v, want playing again", h.p.State().Status)
	}
}

func TestSavesQueueAndResumes(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 300), 1)
	b := h.playAndStart(1)
	for i := 1; i <= 121; i++ { // a little over 30 s
		h.tickAt(b.ID, time.Duration(i)*250*time.Millisecond)
	}
	h.waitFor("server save", func() bool {
		h.api.mu.Lock()
		defer h.api.mu.Unlock()
		return len(h.api.saves) > 0 && h.api.saveCur == "sb"
	})
	local, err := LoadResume(h.p.o.ResumePath)
	if err != nil || local == nil || local.Index != 1 || len(local.Songs) != 3 || local.Position < 30*time.Second {
		t.Fatalf("local resume = %+v, %v", local, err)
	}

	// Server queue wins over the local file.
	h.api.queue = &subsonic.PlayQueue{Songs: songs(3, 300), Current: "sc", Position: 12000}
	r, err := h.p.Resumable(t.Context())
	if err != nil || r.Index != 2 || r.Position != 12*time.Second {
		t.Fatalf("resumable = %+v, %v", r, err)
	}
	h.p.ResumeFrom(r)
	h.waitFor("resume play", func() bool { return h.eng.playCount() == 2 })
	h.waitFor("resume seek", func() bool {
		h.eng.mu.Lock()
		defer h.eng.mu.Unlock()
		return len(h.eng.seeks) == 1 && h.eng.seeks[0].pos == 12*time.Second
	})
}

func TestResumeFallsBackToLocalFile(t *testing.T) {
	h := newHarness(t, nil)
	if err := SaveResume(h.p.o.ResumePath, Resume{Songs: songs(2, 100), Index: 1, Position: 5 * time.Second}); err != nil {
		t.Fatal(err)
	}
	r, err := h.p.Resumable(t.Context())
	if err != nil || r == nil || r.Index != 1 {
		t.Fatalf("resumable = %+v, %v", r, err)
	}
}

// Some servers report duration 0; there is then no prefetch, but the queue
// must still advance when the engine reports the end.
func TestUnknownDurationStillAdvances(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(2, 0), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, 10*time.Minute)
	if h.eng.queueCount() != 0 {
		t.Fatal("prefetched without a known duration")
	}
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("second song", func() bool { return h.eng.playCount() == 2 && h.p.State().Index == 1 })
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/player/`
Expected: build failure, `undefined: Options`, `undefined: New`, `undefined: Playing`, ….

- [ ] **Step 4: Implement**

Delete the temporary `internal/player/api.go`, then create:

`internal/player/player.go`:

```go
// Package player owns the play queue and drives the audio engine: it opens
// streams, prefetches the next track for gapless playback, reports plays to
// the server and remembers where you were.
package player

import (
	"context"
	"errors"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

// Engine is the subset of *audio.Engine the player uses.
type Engine interface {
	Play(t audio.Track)
	QueueNext(t audio.Track)
	ClearNext()
	Stop()
	Seek(id uint64, pos time.Duration) error
	SetPaused(bool)
	SetVolume(float32)
	Position() (uint64, time.Duration, bool)
	Events() <-chan audio.Event
}

// API is the subset of *subsonic.Client the player uses.
type API interface {
	Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error
	SavePlayQueue(ctx context.Context, ids []subsonic.ID, current subsonic.ID, pos time.Duration) error
	GetPlayQueue(ctx context.Context) (*subsonic.PlayQueue, error)
}

type Status int

const (
	Stopped Status = iota
	Loading
	Playing
	Paused
	Buffering
)

func (s Status) String() string {
	return [...]string{"stopped", "loading", "playing", "paused", "buffering"}[s]
}

type Repeat int

const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// State is a snapshot for the UI. Queue must be treated as read-only.
type State struct {
	Queue      []subsonic.Song
	Index      int // into Queue; -1 when nothing is selected
	Status     Status
	Position   time.Duration
	Shuffle    bool
	Repeat     Repeat
	VolumeDB   float64
	Transcoded bool
}

func (s State) Current() (subsonic.Song, bool) {
	if s.Index < 0 || s.Index >= len(s.Queue) {
		return subsonic.Song{}, false
	}
	return s.Queue[s.Index], true
}

type EventKind int

const (
	TrackChanged EventKind = iota + 1
	StatusChanged
	QueueChanged
	Error
)

type Event struct {
	Kind EventKind
	Song subsonic.Song // Error: the song that failed
	Err  error
}

type Options struct {
	Engine        Engine
	API           API
	Open          Opener
	ReplayGain    string // off | track | album
	Scrobble      bool
	ResumePath    string           // local resume file; "" disables
	ScrobblePath  string           // retry queue file; "" keeps it in memory
	Tick          <-chan time.Time // default: 250 ms ticker
	Now           func() time.Time // default time.Now
	Rand          *rand.Rand
	OpenTimeout   time.Duration // default 15 s
	VolumeDB      float64
	PrefetchAhead time.Duration // default 20 s
}

const (
	maxConsecutiveFailures = 3
	saveInterval           = 30 * time.Second
	prevRestartThreshold   = 3 * time.Second
	stallThreshold         = time.Second
	maxTickJump            = 2 * time.Second
	scrobbleCap            = 240 * time.Second
)

type openResult struct {
	id     uint64
	song   subsonic.Song
	opened Opened
	err    error
	next   bool
	seek   time.Duration // raw seek to apply after Play
}

type Player struct {
	o         Options
	cmds      chan func()
	opens     chan openResult
	events    chan Event
	scrobbles *ScrobbleQueue
	ctx       context.Context

	mu   sync.Mutex
	snap State

	// Owned by the Run goroutine.
	queue      []subsonic.Song // copy-on-write
	order      []int           // play order: indices into queue
	cursor     int             // index into order; -1 none
	status     Status
	shuffle    bool
	repeat     Repeat
	volumeDB   float64
	seq        uint64
	curID      uint64
	curSrc     Opened
	nextID     uint64
	nextCursor int
	nextSrc    Opened
	prefetched uint64 // curID for which a prefetch was attempted
	position   time.Duration
	lastPos    time.Duration
	lastMove   time.Time
	listened   time.Duration
	scrobbled  bool
	startedAt  time.Time
	failures   int
	lastSave   time.Time
}

func New(o Options) *Player {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 1))
	}
	if o.OpenTimeout <= 0 {
		o.OpenTimeout = 15 * time.Second
	}
	if o.PrefetchAhead <= 0 {
		o.PrefetchAhead = 20 * time.Second
	}
	p := &Player{
		o:         o,
		cmds:      make(chan func(), 64),
		opens:     make(chan openResult, 8),
		events:    make(chan Event, 64),
		scrobbles: LoadScrobbleQueue(o.ScrobblePath),
		cursor:    -1,
		volumeDB:  o.VolumeDB,
	}
	p.publish()
	return p
}

// Events delivers notifications; if nobody reads, they are dropped.
func (p *Player) Events() <-chan Event { return p.events }

// State returns the latest snapshot.
func (p *Player) State() State { p.mu.Lock(); defer p.mu.Unlock(); return p.snap }

// Run drives the player until ctx is done. It saves the queue on the way out.
func (p *Player) Run(ctx context.Context) {
	p.ctx = ctx
	tick := p.o.Tick
	if tick == nil {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		tick = t.C
	}
	p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
	go p.scrobbles.Flush(ctx, p.o.API)
	for {
		select {
		case <-ctx.Done():
			p.saveNow(context.Background(), true)
			p.o.Engine.Stop()
			return
		case f := <-p.cmds:
			f()
		case r := <-p.opens:
			p.onOpened(r)
		case ev := <-p.o.Engine.Events():
			p.onEngine(ev)
		case <-tick:
			p.onTick()
		}
		p.publish()
	}
}

// do runs f on the player goroutine and waits for it. The snapshot is
// published before do returns, so State() reflects the command.
func (p *Player) do(f func()) {
	done := make(chan struct{})
	p.cmds <- func() { f(); p.publish(); close(done) }
	<-done
}

// ---- commands ----

// PlayNow replaces the queue and starts songs[start].
func (p *Player) PlayNow(songs []subsonic.Song, start int) {
	p.do(func() {
		if start < 0 || start >= len(songs) {
			return
		}
		p.queue = append([]subsonic.Song(nil), songs...)
		p.rebuildOrder(start)
		p.emit(Event{Kind: QueueChanged})
		p.startAt(p.cursor, 0)
	})
}

// PlayNext inserts songs right after the current one.
func (p *Player) PlayNext(songs []subsonic.Song) {
	p.do(func() {
		if len(p.queue) == 0 {
			p.queue = append([]subsonic.Song(nil), songs...)
			p.rebuildOrder(0)
			p.emit(Event{Kind: QueueChanged})
			p.startAt(0, 0)
			return
		}
		at := p.currentIndex() + 1
		q := make([]subsonic.Song, 0, len(p.queue)+len(songs))
		q = append(append(append(q, p.queue[:at]...), songs...), p.queue[at:]...)
		p.queue = q
		for i, qi := range p.order {
			if qi >= at {
				p.order[i] = qi + len(songs)
			}
		}
		ins := make([]int, len(songs))
		for i := range songs {
			ins[i] = at + i
		}
		p.order = append(p.order[:p.cursor+1], append(ins, p.order[p.cursor+1:]...)...)
		p.queueChanged()
	})
}

// Enqueue appends songs to the end of the queue.
func (p *Player) Enqueue(songs []subsonic.Song) {
	p.do(func() {
		if len(p.queue) == 0 {
			p.queue = append([]subsonic.Song(nil), songs...)
			p.rebuildOrder(0)
			p.emit(Event{Kind: QueueChanged})
			p.startAt(0, 0)
			return
		}
		base := len(p.queue)
		p.queue = append(append([]subsonic.Song(nil), p.queue...), songs...)
		for i := range songs {
			if p.shuffle {
				pos := p.cursor + 1 + p.o.Rand.IntN(len(p.order)-p.cursor)
				p.order = append(p.order[:pos], append([]int{base + i}, p.order[pos:]...)...)
			} else {
				p.order = append(p.order, base+i)
			}
		}
		p.queueChanged()
	})
}

// Remove deletes queue[i]. Removing the current song plays the next one.
func (p *Player) Remove(i int) {
	p.do(func() {
		if i < 0 || i >= len(p.queue) {
			return
		}
		wasCurrent := i == p.currentIndex()
		p.queue = append(append([]subsonic.Song(nil), p.queue[:i]...), p.queue[i+1:]...)
		var order []int
		newCursor := -1
		for oi, qi := range p.order {
			if qi == i {
				if oi <= p.cursor {
					newCursor = len(order) - 1
				}
				continue
			}
			if qi > i {
				qi--
			}
			if oi == p.cursor {
				newCursor = len(order)
			}
			order = append(order, qi)
		}
		p.order = order
		p.cursor = newCursor
		if !wasCurrent {
			p.queueChanged()
			return
		}
		p.emit(Event{Kind: QueueChanged})
		if next := p.cursor + 1; next < len(p.order) {
			p.startAt(next, 0)
		} else {
			p.stop()
		}
	})
}

// Clear empties the queue and stops.
func (p *Player) Clear() {
	p.do(func() {
		p.stop()
		p.queue, p.order, p.cursor = nil, nil, -1
		p.emit(Event{Kind: QueueChanged})
	})
}

// Jump plays queue[i].
func (p *Player) Jump(i int) {
	p.do(func() {
		for oi, qi := range p.order {
			if qi == i {
				p.startAt(oi, 0)
				return
			}
		}
	})
}

func (p *Player) Next() {
	p.do(func() {
		if c := p.followingCursor(false); c >= 0 {
			p.startAt(c, 0)
		} else {
			p.stop()
		}
	})
}

// Prev goes to the previous song, or restarts the current one if it has
// played for more than 3 seconds.
func (p *Player) Prev() {
	p.do(func() {
		if p.cursor < 0 {
			return
		}
		if p.position < prevRestartThreshold && p.cursor > 0 {
			p.startAt(p.cursor-1, 0)
			return
		}
		p.startAt(p.cursor, 0)
	})
}

func (p *Player) TogglePause() {
	p.do(func() {
		switch p.status {
		case Playing, Buffering, Loading:
			p.o.Engine.SetPaused(true)
			p.setStatus(Paused)
		case Paused:
			p.o.Engine.SetPaused(false)
			p.setStatus(Playing)
		case Stopped:
			if p.cursor >= 0 {
				p.startAt(p.cursor, 0)
			}
		}
	})
}

// Seek moves within the current song.
func (p *Player) Seek(pos time.Duration) {
	p.do(func() {
		song, ok := p.currentSong()
		if !ok || p.curID == 0 {
			return
		}
		if pos < 0 {
			pos = 0
		}
		if d := time.Duration(song.Duration) * time.Second; d > 0 && pos >= d {
			pos = d - time.Second
		}
		p.lastPos, p.position = pos, pos
		if p.curSrc.Transcoded {
			p.startAt(p.cursor, pos)
			return
		}
		if err := p.o.Engine.Seek(p.curID, pos); err != nil {
			p.startAt(p.cursor, pos)
		}
	})
}

func (p *Player) SetShuffle(on bool) {
	p.do(func() {
		if on == p.shuffle {
			return
		}
		p.shuffle = on
		p.rebuildOrder(p.currentIndex())
		p.queueChanged()
	})
}

func (p *Player) SetRepeat(r Repeat) {
	p.do(func() {
		p.repeat = r
		p.invalidateNext()
	})
}

// SetVolumeDB sets the software volume, clamped to -60..0 dB.
func (p *Player) SetVolumeDB(db float64) {
	p.do(func() {
		p.volumeDB = math.Max(-60, math.Min(0, db))
		p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
	})
}

// Resumable finds a saved queue: the server's first, then the local file.
func (p *Player) Resumable(ctx context.Context) (*Resume, error) {
	if pq, err := p.o.API.GetPlayQueue(ctx); err == nil && pq != nil {
		r := &Resume{Songs: pq.Songs, Position: time.Duration(pq.Position) * time.Millisecond}
		for i, s := range pq.Songs {
			if s.ID == pq.Current {
				r.Index = i
			}
		}
		return r, nil
	}
	if p.o.ResumePath == "" {
		return nil, nil
	}
	return LoadResume(p.o.ResumePath)
}

// ResumeFrom loads r into the queue and starts playing at its position.
func (p *Player) ResumeFrom(r *Resume) {
	p.do(func() {
		if r == nil || r.Index < 0 || r.Index >= len(r.Songs) {
			return
		}
		p.queue = append([]subsonic.Song(nil), r.Songs...)
		p.rebuildOrder(r.Index)
		p.emit(Event{Kind: QueueChanged})
		p.startAt(p.cursor, r.Position)
	})
}

// ---- internals ----

func dbToLinear(db float64) float32 { return float32(math.Pow(10, db/20)) }

func (p *Player) emit(ev Event) {
	select {
	case p.events <- ev:
	default:
	}
}

func (p *Player) publish() {
	s := State{Queue: p.queue, Index: p.currentIndex(), Status: p.status, Position: p.position,
		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Transcoded: p.curSrc.Transcoded}
	p.mu.Lock()
	p.snap = s
	p.mu.Unlock()
}

func (p *Player) setStatus(s Status) {
	if p.status != s {
		p.status = s
		p.emit(Event{Kind: StatusChanged})
	}
}

func (p *Player) currentIndex() int {
	if p.cursor < 0 || p.cursor >= len(p.order) {
		return -1
	}
	return p.order[p.cursor]
}

func (p *Player) currentSong() (subsonic.Song, bool) {
	i := p.currentIndex()
	if i < 0 {
		return subsonic.Song{}, false
	}
	return p.queue[i], true
}

// rebuildOrder resets the play order so that queue[current] is at the cursor.
func (p *Player) rebuildOrder(current int) {
	n := len(p.queue)
	p.order = make([]int, n)
	for i := range p.order {
		p.order[i] = i
	}
	if n == 0 {
		p.cursor = -1
		return
	}
	if current < 0 {
		current = 0
	}
	if !p.shuffle {
		p.cursor = current
		return
	}
	rest := make([]int, 0, n-1)
	for i := 0; i < n; i++ {
		if i != current {
			rest = append(rest, i)
		}
	}
	p.o.Rand.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
	p.order = append([]int{current}, rest...)
	p.cursor = 0
}

// followingCursor is the cursor after the current one. auto=true applies
// repeat-one (the natural end of a track); Next() passes false.
func (p *Player) followingCursor(auto bool) int {
	if p.cursor < 0 || len(p.order) == 0 {
		return -1
	}
	if auto && p.repeat == RepeatOne {
		return p.cursor
	}
	if p.cursor+1 < len(p.order) {
		return p.cursor + 1
	}
	if p.repeat == RepeatAll || p.repeat == RepeatOne {
		return 0
	}
	return -1
}

func (p *Player) queueChanged() {
	p.invalidateNext()
	p.emit(Event{Kind: QueueChanged})
}

// invalidateNext drops the prepared gapless successor after queue changes.
func (p *Player) invalidateNext() {
	if p.nextID != 0 {
		p.o.Engine.ClearNext()
		p.nextID = 0
		p.nextSrc = Opened{}
	}
	p.prefetched = 0
}

func (p *Player) stop() {
	p.o.Engine.Stop()
	p.o.Engine.SetPaused(false)
	p.curID, p.nextID = 0, 0
	p.curSrc, p.nextSrc = Opened{}, Opened{}
	p.position = 0
	p.setStatus(Stopped)
}

// startAt opens order[cursor] and plays it from offset.
func (p *Player) startAt(cursor int, offset time.Duration) {
	p.invalidateNext()
	p.cursor = cursor
	song, _ := p.currentSong()
	p.seq++
	id := p.seq
	p.curID = id
	p.curSrc = Opened{}
	p.position, p.lastPos = offset, offset
	p.resetListen()
	p.o.Engine.SetPaused(false)
	p.setStatus(Loading)
	p.emit(Event{Kind: TrackChanged, Song: song})
	p.openAsync(id, song, offset, false)
}

func (p *Player) openAsync(id uint64, song subsonic.Song, offset time.Duration, next bool) {
	ctx := p.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		octx, cancel := context.WithTimeout(ctx, p.o.OpenTimeout)
		defer cancel()
		op, err := p.o.Open(octx, song, offset, next)
		r := openResult{id: id, song: song, opened: op, err: err, next: next}
		if err == nil && !op.Transcoded {
			r.seek = offset
		}
		select {
		case p.opens <- r:
		case <-ctx.Done():
			closeOpened(op)
		}
	}()
}

func closeOpened(o Opened) {
	if c, ok := o.Source.(interface{ Close() error }); ok {
		c.Close()
	}
}

func (p *Player) onOpened(r openResult) {
	if r.next {
		if r.id != p.nextID {
			closeOpened(r.opened)
			return
		}
		if r.err != nil {
			p.nextID = 0
			p.emit(Event{Kind: Error, Song: r.song, Err: r.err})
			return
		}
		p.nextSrc = r.opened
		p.o.Engine.QueueNext(p.track(r.id, r.song, r.opened))
		return
	}
	if r.id != p.curID {
		closeOpened(r.opened)
		return
	}
	if r.err != nil {
		p.trackFailed(r.song, r.err)
		return
	}
	p.curSrc = r.opened
	p.o.Engine.Play(p.track(r.id, r.song, r.opened))
	if r.seek > 0 {
		if err := p.o.Engine.Seek(r.id, r.seek); err != nil {
			log.Printf("player: resume seek: %v", err)
		}
	}
}

func (p *Player) track(id uint64, s subsonic.Song, o Opened) audio.Track {
	return audio.Track{ID: id, Source: o.Source, Format: o.Format, Gain: ReplayGainFactor(s, p.o.ReplayGain), Offset: o.Offset}
}

// trackFailed reports err and moves on, stopping after too many in a row.
func (p *Player) trackFailed(song subsonic.Song, err error) {
	p.emit(Event{Kind: Error, Song: song, Err: err})
	p.failures++
	if p.failures >= maxConsecutiveFailures {
		p.failures = 0
		p.stop()
		return
	}
	if c := p.followingCursor(false); c >= 0 {
		p.startAt(c, 0)
	} else {
		p.stop()
	}
}

func (p *Player) onEngine(ev audio.Event) {
	switch ev.Kind {
	case audio.EventStarted:
		if ev.TrackID == p.nextID {
			p.cursor = p.nextCursor
			p.curID, p.curSrc = p.nextID, p.nextSrc
			p.nextID, p.nextSrc = 0, Opened{}
			p.prefetched = 0
			p.position, p.lastPos = 0, 0
			p.resetListen()
			song, _ := p.currentSong()
			p.emit(Event{Kind: TrackChanged, Song: song})
		}
		if ev.TrackID != p.curID {
			return
		}
		if pr, ok := p.curSrc.Source.(Promoter); ok {
			pr.Promote()
		}
		p.failures = 0
		p.lastMove = p.o.Now()
		if p.status != Paused {
			p.setStatus(Playing)
		}
		p.nowPlaying()
	case audio.EventEnded:
		if ev.TrackID != p.curID {
			return
		}
		if p.nextID != 0 && p.nextSrc.Source != nil {
			return // the engine has the successor; its Started will follow
		}
		if c := p.followingCursor(true); c >= 0 {
			p.startAt(c, 0)
		} else {
			p.curID = 0
			p.stop()
		}
	case audio.EventError:
		switch ev.TrackID {
		case p.nextID:
			song := p.queue[p.order[p.nextCursor]]
			p.nextID = 0
			p.emit(Event{Kind: Error, Song: song, Err: ev.Err})
		case p.curID:
			song, _ := p.currentSong()
			if p.nextID != 0 {
				p.emit(Event{Kind: Error, Song: song, Err: ev.Err}) // engine continues into the successor
				return
			}
			p.trackFailed(song, ev.Err)
		}
	}
}

func (p *Player) onTick() {
	now := p.o.Now()
	id, pos, ok := p.o.Engine.Position()
	if ok && id == p.curID && p.curID != 0 {
		delta := pos - p.lastPos
		if delta > 0 {
			if delta < maxTickJump && p.status == Playing {
				p.listened += delta
			}
			p.lastMove = now
			if p.status == Buffering {
				p.setStatus(Playing)
			}
		} else if p.status == Playing && now.Sub(p.lastMove) > stallThreshold {
			p.setStatus(Buffering)
		}
		p.lastPos, p.position = pos, pos
		p.maybeScrobble()
		p.maybePrefetch()
	}
	if (p.status == Playing || p.status == Paused) && now.Sub(p.lastSave) >= saveInterval {
		p.saveNow(p.ctx, false)
	}
}

func (p *Player) maybePrefetch() {
	if p.nextID != 0 || p.prefetched == p.curID || p.repeat == RepeatOne {
		return
	}
	song, _ := p.currentSong()
	d := time.Duration(song.Duration) * time.Second
	if d <= 0 || d-p.position > p.o.PrefetchAhead {
		return
	}
	p.prefetched = p.curID
	c := p.followingCursor(true)
	if c < 0 {
		return
	}
	p.seq++
	p.nextID, p.nextCursor = p.seq, c
	p.openAsync(p.nextID, p.queue[p.order[c]], 0, true)
}

func (p *Player) resetListen() {
	p.listened = 0
	p.scrobbled = false
	p.startedAt = p.o.Now()
}

func (p *Player) nowPlaying() {
	if !p.o.Scrobble {
		return
	}
	song, _ := p.currentSong()
	p.startedAt = p.o.Now()
	go p.o.API.Scrobble(p.ctx, song.ID, p.startedAt, false)
}

func (p *Player) maybeScrobble() {
	if !p.o.Scrobble || p.scrobbled {
		return
	}
	song, _ := p.currentSong()
	need := scrobbleCap
	if d := time.Duration(song.Duration) * time.Second; d > 0 && d/2 < need {
		need = d / 2
	}
	if p.listened < need {
		return
	}
	p.scrobbled = true
	s := Scrobble{ID: song.ID, At: p.startedAt}
	ctx := p.ctx
	go func() {
		if err := p.o.API.Scrobble(ctx, s.ID, s.At, true); err != nil {
			p.scrobbles.Add(s)
			return
		}
		p.scrobbles.Flush(ctx, p.o.API)
	}()
}

// saveNow stores the queue locally and on the server. sync waits for the
// server call (used on exit).
func (p *Player) saveNow(ctx context.Context, sync bool) {
	p.lastSave = p.o.Now()
	i := p.currentIndex()
	if i < 0 {
		return
	}
	songs := p.queue
	pos := p.position
	if p.o.ResumePath != "" {
		if err := SaveResume(p.o.ResumePath, Resume{Songs: songs, Index: i, Position: pos}); err != nil {
			log.Printf("player: save resume: %v", err)
		}
	}
	ids := make([]subsonic.ID, len(songs))
	for k, s := range songs {
		ids[k] = s.ID
	}
	save := func() {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.o.API.SavePlayQueue(sctx, ids, songs[i].ID, pos); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("player: savePlayQueue: %v", err)
		}
	}
	if sync {
		save()
	} else {
		go save()
	}
}
```

- [ ] **Step 5: Run the tests, repeatedly, under the race detector**

Run: `rm -f internal/player/api.go && go vet ./internal/player/ && go test -race -count=30 ./internal/player/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/player
git commit -m "player: queue, gapless handover, scrobbling and resume" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 11: Dev CLI, mock server, silent end-to-end check, on-device playback

**Files:**
- Create: `tools/mocksubsonic/main.go`, `cmd/mss-cli/main.go`, `scripts/e2e-smoke.sh`
- Modify: `Makefile` (replace it with the final version below)
- Modify: `docs/spikes.md` (append the on-device result)

**Interfaces:**
- Consumes: everything above. `config.Load`, `subsonic.New/Connect/GetAlbum/GetAlbumList2/Search3/Classify`, `audio.OpenDevice/NewEngine`, `player.New/NewOpener/StreamSettings`.
- Produces:
  - `mss-cli [-config path] [-null] [-volume dB] [-exit-at-end] ping | albums | search <q> | play-album <id> | play-song <id>…`
  - `mocksubsonic -dir <folder> -addr host:port`
  - `make e2e`, `make mister`, `make deploy-dev`

`mss-cli` is a development and on-device test tool, not the app. Plan 2 builds the real UI binary on the same packages.

- [ ] **Step 1: Write the mock server**

`tools/mocksubsonic/main.go`:

```go
// Command mocksubsonic serves a folder of audio files as a one-album
// Subsonic server, for exercising the client without a real Navidrome.
// It accepts any credentials and supports HTTP Range on stream.
//
//	go run ./tools/mocksubsonic -dir internal/audio/testdata -addr 127.0.0.1:4533
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mistersubsonic/internal/audio"
)

type song struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Album       string `json:"album"`
	Artist      string `json:"artist"`
	AlbumID     string `json:"albumId"`
	Track       int    `json:"track"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	Duration    int    `json:"duration"`
	Size        int64  `json:"size"`
	path        string
}

func main() {
	dir := flag.String("dir", ".", "folder of .flac/.mp3/.wav files")
	addr := flag.String("addr", "127.0.0.1:4533", "listen address")
	flag.Parse()

	songs, err := scan(*dir)
	if err != nil || len(songs) == 0 {
		log.Fatalf("no audio files in %s (%v)", *dir, err)
	}
	album := map[string]any{"id": "al-mock", "name": "Mock Album", "artist": "Mock Artist", "artistId": "ar-mock", "songCount": len(songs)}

	http.HandleFunc("/rest/", func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
		q := r.URL.Query()
		log.Printf("%s %s range=%q", endpoint, q.Get("id"), r.Header.Get("Range"))
		switch endpoint {
		case "stream":
			i, err := strconv.Atoi(strings.TrimPrefix(q.Get("id"), "so-"))
			if err != nil || i < 0 || i >= len(songs) {
				reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 70, "message": "not found"}})
				return
			}
			f, err := os.Open(songs[i].path)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", songs[i].ContentType)
			http.ServeContent(w, r, "", time.Time{}, f)
		case "ping", "scrobble", "savePlayQueue", "star", "unstar":
			reply(w, nil)
		case "getOpenSubsonicExtensions":
			reply(w, map[string]any{"openSubsonicExtensions": []any{}})
		case "getAlbum":
			a := map[string]any{"song": songs}
			for k, v := range album {
				a[k] = v
			}
			reply(w, map[string]any{"album": a})
		case "getAlbumList2":
			reply(w, map[string]any{"albumList2": map[string]any{"album": []any{album}}})
		case "getArtists":
			reply(w, map[string]any{"artists": map[string]any{"index": []any{map[string]any{"name": "M", "artist": []any{map[string]any{"id": "ar-mock", "name": "Mock Artist", "albumCount": 1}}}}}})
		case "search3":
			reply(w, map[string]any{"searchResult3": map[string]any{"song": songs}})
		case "getPlayQueue":
			reply(w, nil)
		default:
			reply(w, map[string]any{"status": "failed", "error": map[string]any{"code": 0, "message": "mock does not implement " + endpoint}})
		}
	})
	log.Printf("serving %d songs from %s on http://%s", len(songs), *dir, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func reply(w http.ResponseWriter, payload map[string]any) {
	body := map[string]any{"status": "ok", "version": "1.16.1", "type": "mock", "openSubsonic": true}
	for k, v := range payload {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
}

func scan(dir string) ([]song, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if audio.FormatFromSuffix(filepath.Ext(e.Name())) != audio.FormatUnknown {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	types := map[audio.Format]string{audio.FormatFLAC: "audio/flac", audio.FormatMP3: "audio/mpeg", audio.FormatWAV: "audio/wav"}
	var out []song
	for i, n := range names {
		p := filepath.Join(dir, n)
		f := audio.FormatFromSuffix(filepath.Ext(n))
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		out = append(out, song{
			ID: fmt.Sprintf("so-%d", i), Title: strings.TrimSuffix(n, filepath.Ext(n)), Album: "Mock Album", Artist: "Mock Artist",
			AlbumID: "al-mock", Track: i + 1, Suffix: strings.TrimPrefix(filepath.Ext(n), "."), ContentType: types[f],
			Duration: durationOf(p, f), Size: fi.Size(), path: p,
		})
	}
	return out, nil
}

func durationOf(p string, f audio.Format) int {
	file, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer file.Close()
	d, err := audio.OpenDecoder(file, f)
	if err != nil {
		return 0
	}
	defer d.Close()
	if d.SampleRate() == 0 {
		return 0
	}
	return int((d.LengthFrames() + uint64(d.SampleRate()) - 1) / uint64(d.SampleRate()))
}
```

- [ ] **Step 2: Write the end-to-end check (the failing test for this task)**

`scripts/e2e-smoke.sh`:

```sh
#!/bin/sh
# End-to-end check of the playback core: the mock server serves a
# three-track album and mss-cli plays it on the NULL audio device (silent).
# Passes if all three tracks start and the queue finishes.
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
cleanup() { [ -n "$mock" ] && kill "$mock" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

go build -o "$tmp/" ./cmd/mss-cli ./tools/mocksubsonic
mkdir "$tmp/music"
cp internal/audio/testdata/half-a.flac "$tmp/music/01-a.flac"
cp internal/audio/testdata/half-b.flac "$tmp/music/02-b.flac"
cp internal/audio/testdata/tone-44k16.mp3 "$tmp/music/03-c.mp3"
port=${E2E_PORT:-14533}
cat > "$tmp/config.toml" <<CFG
[[server]]
name = "mock"
url = "http://127.0.0.1:$port"
username = "test"
password = "test"
CFG

"$tmp/mocksubsonic" -dir "$tmp/music" -addr "127.0.0.1:$port" > "$tmp/mock.log" 2>&1 &
mock=$!
i=0
until "$tmp/mss-cli" -config "$tmp/config.toml" ping > /dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -lt 50 ] || { echo "mock server did not start" >&2; cat "$tmp/mock.log" >&2; exit 1; }
  sleep 0.1
done

timeout 30 "$tmp/mss-cli" -config "$tmp/config.toml" -null -exit-at-end play-album al-mock < /dev/null > "$tmp/cli.log" 2>&1
started=$(tr '\r' '\n' < "$tmp/cli.log" | grep -c '^▶')
if [ "$started" -ne 3 ] || ! grep -q 'queue finished' "$tmp/cli.log"; then
  echo "e2e failed: $started tracks started" >&2
  tr '\r' '\n' < "$tmp/cli.log" >&2
  exit 1
fi
echo "e2e ok: 3 tracks played through mock server -> stream -> decode -> resample -> engine (null device)"
```

Run: `chmod +x scripts/e2e-smoke.sh && ./scripts/e2e-smoke.sh`
Expected: FAIL. `go build` reports `no Go files in …/cmd/mss-cli` (or `directory not found`).

- [ ] **Step 3: Write the CLI**

`cmd/mss-cli/main.go`:

```go
// Command mss-cli is a terminal harness for the playback core: it connects
// to the configured server and plays albums with line-based controls.
// It is a development and on-device test tool, not the app.
//
//	mss-cli -config config.toml ping
//	mss-cli -config config.toml albums
//	mss-cli -config config.toml play-album <album-id>
//	mss-cli -config config.toml play-song <song-id>...
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func main() {
	cfgPath := flag.String("config", config.DefaultPath, "config file")
	null := flag.Bool("null", false, "use the null audio device (no sound; for testing)")
	quitAtEnd := flag.Bool("exit-at-end", false, "exit when the queue finishes")
	volume := flag.Float64("volume", math.NaN(), "start volume in dB (-60..0), overriding the config; use -30 for a quiet first listen")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mss-cli [flags] ping | albums | search <query> | play-album <id> | play-song <id>...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*cfgPath, *null, *quitAtEnd, *volume, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cfgPath string, null, quitAtEnd bool, volume float64, args []string) error {
	cfg, warns, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	for _, w := range warns {
		log.Printf("config: %s", w)
	}
	if !math.IsNaN(volume) {
		cfg.Playback.VolumeDB = volume
	}
	srv, ok := cfg.ActiveServer()
	if !ok {
		return errors.New("no [[server]] in config")
	}
	client, err := subsonic.New(subsonic.Options{
		BaseURL: srv.URL,
		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token, Salt: srv.Salt,
			APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	info, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("%v: %w", subsonic.Classify(err), err)
	}

	switch args[0] {
	case "ping":
		fmt.Printf("ok: %s %s, API %s, OpenSubsonic=%v, auth=%v, extensions=%v\n",
			info.Type, info.ServerVersion, info.APIVersion, info.OpenSubsonic, client.AuthMethod(), info.Extensions)
		return nil
	case "albums":
		list, err := client.GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: subsonic.ListNewest, Size: 50})
		if err != nil {
			return err
		}
		for _, a := range list {
			fmt.Printf("%-40s %s — %s (%d)\n", a.ID, a.Artist, a.Name, a.Year)
		}
		return nil
	case "search":
		res, err := client.Search3(ctx, strings.Join(args[1:], " "), subsonic.SearchQuery{AlbumCount: 20, SongCount: 20})
		if err != nil {
			return err
		}
		for _, a := range res.Albums {
			fmt.Printf("album %-36s %s — %s\n", a.ID, a.Artist, a.Name)
		}
		for _, s := range res.Songs {
			fmt.Printf("song  %-36s %s — %s [%s]\n", s.ID, s.Artist, s.Title, s.Suffix)
		}
		return nil
	case "play-album", "play-song":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}

	var songs []subsonic.Song
	if args[0] == "play-album" {
		if len(args) != 2 {
			return errors.New("play-album needs one album id")
		}
		al, err := client.GetAlbum(ctx, subsonic.ID(args[1]))
		if err != nil {
			return err
		}
		songs = al.Songs
	} else {
		for _, id := range args[1:] {
			songs = append(songs, subsonic.Song{ID: subsonic.ID(id), Suffix: "flac"})
		}
	}
	if len(songs) == 0 {
		return errors.New("nothing to play")
	}

	dev := cfg.Playback.ALSADevice
	if dev == "default" {
		dev = ""
	}
	out, err := audio.OpenDevice(audio.DeviceOptions{Name: dev, Null: null})
	if err != nil {
		return err
	}
	defer out.Close()
	eng := audio.NewEngine(audio.EngineOptions{Output: out})
	defer eng.Close()

	p := player.New(player.Options{
		Engine: eng, API: client,
		Open: player.NewOpener(client, player.StreamSettings{
			TranscodeFormat: cfg.Playback.TranscodeFormat, TranscodeBitrate: cfg.Playback.TranscodeBitrate,
			WindowBytes: int64(cfg.Playback.BufferMB) << 20,
		}),
		ReplayGain: cfg.Playback.ReplayGain, Scrobble: cfg.Playback.Scrobble, VolumeDB: cfg.Playback.VolumeDB,
	})
	pctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { p.Run(pctx); close(done) }()
	defer func() { cancel(); <-done }()

	p.PlayNow(songs, 0)
	fmt.Println("controls: p pause · n next · b prev · s <sec> seek · r repeat · z shuffle · + / - volume · q quit")

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- strings.TrimSpace(sc.Text())
		}
	}()
	status := time.NewTicker(time.Second)
	defer status.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-p.Events():
			switch ev.Kind {
			case player.Error:
				fmt.Printf("\n! %s: %v\n", ev.Song.Title, ev.Err)
			case player.TrackChanged:
				fmt.Printf("\n▶ %s — %s [%s]\n", ev.Song.Artist, ev.Song.Title, quality(ev.Song))
			case player.StatusChanged:
				if quitAtEnd && p.State().Status == player.Stopped {
					fmt.Println("\nqueue finished")
					return nil
				}
			}
		case <-status.C:
			st := p.State()
			cur, _ := st.Current()
			fmt.Printf("\r  %-9s %s / %s  vol %.0f dB   ", st.Status, clock(st.Position), clock(time.Duration(cur.Duration)*time.Second), st.VolumeDB)
		case l := <-lines:
			st := p.State()
			switch {
			case l == "q":
				return nil
			case l == "p":
				p.TogglePause()
			case l == "n":
				p.Next()
			case l == "b":
				p.Prev()
			case l == "r":
				p.SetRepeat((st.Repeat + 1) % 3)
				fmt.Printf("\nrepeat %d\n", (st.Repeat+1)%3)
			case l == "z":
				p.SetShuffle(!st.Shuffle)
			case l == "+":
				p.SetVolumeDB(st.VolumeDB + 3)
			case l == "-":
				p.SetVolumeDB(st.VolumeDB - 3)
			case strings.HasPrefix(l, "s "):
				if sec, err := strconv.Atoi(strings.TrimSpace(l[2:])); err == nil {
					p.Seek(time.Duration(sec) * time.Second)
				}
			}
		}
	}
}

func quality(s subsonic.Song) string {
	q := strings.ToUpper(s.Suffix)
	if s.BitDepth > 0 && s.SamplingRate > 0 {
		q += fmt.Sprintf(" %d/%g", s.BitDepth, float64(s.SamplingRate)/1000)
	} else if s.BitRate > 0 {
		q += fmt.Sprintf(" %d", s.BitRate)
	}
	return q
}

func clock(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
```

- [ ] **Step 4: Replace the Makefile with the final version**

`Makefile`:

```make
GO      ?= go
ZIG     ?= zig
BIN     := bin
MISTER  ?= mister.local
DEVDIR  := /media/fat/mistersubsonic/dev
ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
           CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"

.PHONY: build test vet e2e mister mister-test deploy-dev vendor-check clean

build:
	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

# Silent end-to-end run (mock server + CLI on the null audio device).
e2e:
	./scripts/e2e-smoke.sh

# Cross-compiled binaries for the MiSTer (ARMv7, glibc <= 2.31).
mister:
	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w" -o $(BIN)/arm/mss-cli ./cmd/mss-cli
	./scripts/check-glibc.sh $(BIN)/arm/mss-cli

# Audio test binary for on-device checks and benchmarks.
mister-test:
	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
	./scripts/check-glibc.sh $(BIN)/arm/audio.test

# Copies the dev tools to the MiSTer (default root password is "1").
deploy-dev: mister mister-test
	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
	scp $(BIN)/arm/mss-cli $(BIN)/arm/audio.test root@$(MISTER):$(DEVDIR)/
	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/

vendor-check:
	./third_party/fetch.sh

clean:
	rm -rf $(BIN)
```

- [ ] **Step 5: Run the end-to-end check and the ARM build**

Run: `go vet ./... && make e2e && make mister mister-test`
Expected:
- `e2e ok: 3 tracks played through mock server -> stream -> decode -> resample -> engine (null device)`
- `bin/arm/mss-cli: needs glibc 2.29 (<= 2.31) ok`
- the same line for `audio.test`

The e2e run is silent, because it always passes `-null`.

- [ ] **Step 6: Commit**

```bash
git add tools cmd scripts/e2e-smoke.sh Makefile
git commit -m "cli: mss-cli dev tool, mock server and silent e2e check" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: On-device check against the user's Navidrome (needs the user)**

This is the first time the whole core runs on the MiSTer against the real server.

**First, silent checks.** Ask the user to create `/media/fat/mistersubsonic/dev/config.toml` on the MiSTer with their server; you can offer the template below. Or they can give you the values to write, as long as it's never committed.

```toml
[[server]]
name = "home"
url = "http://192.168.1.10:4533"
username = "…"
password = "…"
```

Then run:

```bash
make deploy-dev MISTER=<address>
ssh root@<address> 'cd /media/fat/mistersubsonic/dev && ./mss-cli -config config.toml ping && ./mss-cli -config config.toml albums | head -5'
ssh root@<address> 'cd /media/fat/mistersubsonic/dev && timeout 60 ./mss-cli -config config.toml -null -exit-at-end play-album <album-id> < /dev/null | tr "\r" "\n" | grep -v "^  " | head -20'
```

Pick `<album-id>` from the `albums` output, preferably a FLAC album. Expected:
- `ping` shows `navidrome` and `auth=token`
- `albums` lists albums
- the null-device play shows `▶` lines advancing through tracks in real time (about 60 s). It is silent, but it proves streaming, decoding and resampling keep up on the A9.

**Then, audible checks. Stop and ask the user first.** Say that it plays an album through the MiSTer's audio output at −30 dB, and that they can press `+` to raise the volume. Only after they say yes, give them this command to run in their own terminal. It is interactive (`p`, `n`, `s 120`, `q`), so they should run it themselves:

```bash
ssh -t root@<address> 'cd /media/fat/mistersubsonic/dev && ./mss-cli -config config.toml -volume -30 play-album <album-id>'
```

Ask them to check four things:
1. Is it gapless between tracks on a gapless album?
2. Does `s 120` seek quickly?
3. Do `p`, `n` and `b` respond instantly?
4. After ≥ half a track, does the play appear in Navidrome's "recently played"?

If they have a single-file album image (FLAC + CUE), ask them to try `s 1800` on it; spec success criterion 3 says about 1 s.

Append the results to `docs/spikes.md` under `## On-device playback (<date>)`, including any problems, verbatim. Problems found here become bugs to fix before Plan 2: use superpowers:systematic-debugging, don't guess.

- [ ] **Step 8: Commit the results**

```bash
git add docs/spikes.md
git commit -m "docs: record on-device playback check" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 12: CI and developer README

**Files:**
- Create: `.github/workflows/ci.yml`, `README.md`

**Interfaces:**
- Consumes: `make e2e`, `make mister`, `make mister-test` (Task 11).
- Produces: CI that vets, tests (with race), runs e2e, cross-builds and uploads `bin/arm/` as an artifact on every push.

- [ ] **Step 1: Write the workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test -race ./...
      - run: make e2e
      - uses: mlugg/setup-zig@v2
        with:
          version: 0.16.0
      - run: make mister mister-test
      - uses: actions/upload-artifact@v4
        with:
          name: mister-dev-tools
          path: bin/arm/
```

- [ ] **Step 2: Write the README**

`README.md`:

````markdown
# MiSTer Subsonic

A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.

Status: **playback core only** (streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
scrobbling, resume) with a terminal dev tool. The TV UI and MiSTer launcher come next.

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
````

- [ ] **Step 3: Verify locally what CI will run**

Run: `go vet ./... && go test -race ./... && make e2e && make mister mister-test`
Expected: all pass.

After the user pushes to GitHub, check the Actions run. Pushing is the user's call; don't push unasked.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml README.md
git commit -m "ci: vet, race tests, e2e and ARM build; developer README" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

- `docs/spikes.md` holds the answers the spec's §11 spikes #1 and #2, and the audio half of #3, asked for.
- If any decision rule said "stop and tell the user", the spec gets revised before Plan 2 is written.
- Plan 2 (UI) starts with the other half of spike #3: pure-Go text rendering speed on the A9.
