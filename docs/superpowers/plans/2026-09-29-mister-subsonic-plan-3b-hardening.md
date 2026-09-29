# MiSTer Subsonic — Plan 3b: device hardening (verified on the host)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** fix the device-side weaknesses the Plan 1 and 2a reviews left for Plan 3, in the parts that can be proven on a PC:
- memory (the prefetch stream ring, cover decoding);
- MP3 seeking;
- the audio device and the engine's shutdown;
- stream errors (416, 429);
- the disk cache;
- ReplayGain taking effect at once.

The measurements that need the MiSTer stay pending, and the checklist gains the new device checks.

**Architecture:** each fix stays inside the package that owns the problem:
- `internal/gfx`: decode and scale in one pass.
- `internal/cache`: temp files, size recount.
- `internal/stream`: ring sizing, retry rules.
- `internal/audio`: device guards, the pending buffer, command and opener bookkeeping, `SetGain`.
- `internal/player`: the MP3 byte estimate, the closed event stream, ReplayGain.

No interface changes reach the UI. `player.Engine` gains `SetGain`.

**Tech Stack:** Go 1.26+, cgo (miniaudio, speexdsp) in `internal/audio` only, no new modules.

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`:
- §5 (the stream: sliding window, Range, reconnect, backoff, retry budget)
- §6 (the device, the decode loop, ReplayGain)
- §7 (seek: Range for raw streams, timeOffset for transcoded ones)
- §8.1 (covers: about 48 MB decoded in memory)
- §10 (testing: no real sound device in tests)

Also read "Plan 3 must handle" in `docs/superpowers/plan-1-followups.md` and `docs/superpowers/plan-2a-followups.md`, and "Plan 3b must handle" in the latter.

**Scope:**

| | |
|---|---|
| **Plans 1–3a (done)** | Playback, the TV interface, setup, Settings, MiSTer integration, releases |
| **This plan (3b)** | Everything in the Goal above |
| **Later, with the MiSTer** | Spikes 2 and 3; the `Repaint` benchmarks; input with real maps and grabs next to Main_MiSTer; CRT margins; the checklist in `docs/testing-on-mister.md` |

**How this plan's code was produced:** every block was built and run before the plan was written.
- `go test -race ./...`, the launcher tests, `make e2e` and the ARM build (glibc 2.29 ≤ 2.31) pass.
- The blocks were replayed task by task on a fresh clone of `main`. At every task the tests failed before the code and passed after, and the tree ended identical to the prototype's.
- A check before planning: the old cover path allocated 51 MB for one 2000×2000 JPEG, and the new one stays under 10 MB. `dr_mp3` decodes the test MP3s correctly from a third and from half of the file in.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:** Go 1.26+ (`go.mod` says `go 1.26.0`). No new modules. cgo only in `internal/audio`. ARM build: `scripts/check-glibc.sh` must report ≤ 2.31.
- **Memory (spec §8.1, §5):**
  - The cover cache holds about 48 MB decoded.
  - A stream reader holds `buffer_mb` (32 MB by default) while it plays.
  - The MiSTer has about 500 MB for Linux programs.
- **Stream behaviour (spec §5):**
  - Reconnect on drops and stalls with backoff (0.5, 1, 2, 4, 8 s).
  - Give up after the retry budget (30 s) without progress.
  - Range for seeks on raw files. Transcoded streams are sequential and sought with `timeOffset`.
- **Audio (spec §6):**
  - Output is 48 kHz stereo f32 through a ring of about 500 ms.
  - ReplayGain is `off`, `track` or `album`, with a soft clip above unity only.
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device, never a real sound card.
  - No listening checks without the user's go-ahead each time.
- **Tests never touch real devices** (`/dev/tty*`, `/dev/fb0`, the MiSTer), and use only `httptest` servers for the network.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These five situations are implied by the spec but no feature test covers them. They are the most likely to bite a real user, and each gets its test in the task that owns the code:

1. **A server that rate-limits or restarts mid-song** (429 or 503 with Retry-After). Playback carries on after the wait. A Retry-After longer than the retry budget ends the track with an error promptly, instead of hanging. Tests: `TestTooManyRequestsIsRetriedAfterRetryAfter`, `TestRetryAfterBeyondTheBudgetGivesUp`, `TestOpenRetriesWhenTheServerIsBusy` (Task 4).
2. **A file replaced on the server while it plays** (now shorter, so the server answers 416). The track ends with an error, not silently early as if it were over. Test: `TestRangeRefusedMidFileIsAnError` (Task 4).
3. **Huge embedded cover art**, such as a 4000×4000 PNG. It is refused without exhausting memory, while big JPEG covers still show. Tests: `TestDecodeRefusesImagesTooBigToDecode`, `TestDecodeDoesNotConvertAtFullSize` (Task 1).
4. **Seeking far into a long MP3.** It is quick, by reopening at a byte estimate. An MP3 whose size the server doesn't report still seeks. Tests: `TestSeekingAnMP3ReopensNearTheTarget`, `TestOpenerStartsAnMP3NearTheOffset` (Task 8).
5. **Quitting while the next track is still opening, or with commands still queued.** Nothing is left open and the app exits. Tests: `TestEngineCloseClosesSourcesItNeverRan`, `TestEngineOpenerFinishingAfterCloseIsClosed` (Task 6).

## Decisions this plan makes (the spec is silent or leaves room)

- **Cover budget.** The follow-ups suggested capping covers at 2048² pixels. This plan instead scales covers straight from the decoder's output, which removes the second full-size copy (most of the old ~128 MB). It then bounds the decoder's own allocation at 48 MiB. A 2048² cap would drop the common 3000² JPEG covers whenever the server doesn't resize them.
- **Prefetch ring.** It is 1.25 × `PrefetchBytes` (5 MiB for the default 4 MiB). While it is that small, at most a quarter of it is kept behind the reader. It grows to the full window on `Promote`, when the track starts playing.
- **Retry rules:**
  - 408, 429, 502, 503 and 504 are retried, on the first request too, honouring Retry-After.
  - Other 4xx are terminal.
  - A connection error on the first request still fails at once, so a dead server shows its error quickly.
  - A 416 before the known end is an error. At or after the end it is the end.
- **MP3 byte estimate.** The target is the ID3v2 tag size plus the rest in proportion to time. That is exact for a constant bitrate. For a variable bitrate it is close, and the shown position may be off by the estimate's error. MP3s without a size or duration still seek through the decoder.
- **ReplayGain** changes apply at once, to the playing, queued and opening tracks. The half second already buffered plays at the old gain.
- **Engine shutdown:**
  - `Close` waits for openers still running. Their decoder opens return promptly once their sources are closed.
  - `Events()` closes after `Close`. Readers must handle a closed channel; the player does.
- **The on-device measurements** stay pending while the MiSTer is offline. Task 9 adds the new checks to `docs/testing-on-mister.md`.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/gfx/image.go`, `canvas.go` | decode budget, scale from the decoder, fast pixel paths, 64-bit box sums | 1 |
| `internal/cache/cache.go` | `.tmp` cleanup, size recount | 2 |
| `internal/stream/reader.go` | small prefetch ring grown on Promote (3); 416, 429 and Retry-After, retries on Open (4) | 3, 4 |
| `internal/audio/device.c`, `engine.go`, `resampler.go` | guards after close, the tail while waiting, buffer reuse (5); shutdown bookkeeping, `Events()` closed (6); `SetGain` (7) | 5, 6, 7 |
| `internal/player/player.go`, `stream.go` | closed event stream (6); ReplayGain at once (7); MP3 byte estimate (8) | 6, 7, 8 |
| `tools/mocksubsonic/main.go` | `timeOffset` for MP3 | 8 |
| `docs/superpowers/plan-2a-followups.md`, `docs/spikes.md`, `docs/testing-on-mister.md` | results and device checks | 9 |

---

### Task 1: Covers: scale straight from the decoder, a decode budget, 64-bit box sums

**Files:**
- Modify: `internal/gfx/image.go`, `internal/gfx/canvas.go` (`FromImage` moves to `image.go`)
- Test: `internal/gfx/decode_test.go` (new)

**Interfaces:**
- **Produces:**
  - `gfx.MaxDecodeBytes` (48 MiB) replaces `MaxDecodePixels`.
    - `decodedBytes(image.Config)` estimates the decoder's allocation: YCbCr counts as 3 bytes a pixel, RGBA as 4, 16-bit as 8, gray and paletted as 1.
  - `DecodeImage` box-filters straight from the decoded `image.Image`, through `pixels(m)`, which has fast paths for YCbCr, NRGBA, RGBA and Gray and falls back to `At` for other types.
  - `FromImage` uses the same fast paths.
  - `fitSize` and `boxFilter` are shared by `Fit` and `Resize`. Their sums are 64-bit.

- [ ] **Step 1: Write the failing tests**

`internal/gfx/decode_test.go` (new file):

```go
package gfx

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"runtime"
	"strings"
	"testing"
)

// slowFromImage is the reference conversion: every pixel through At.
func slowFromImage(m image.Image) *Image {
	b := m.Bounds()
	out := NewImage(b.Dx(), b.Dy())
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			out.Pix[y*out.W+x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
	}
	return out
}

func near(a, b uint32) bool {
	for s := 0; s < 32; s += 8 {
		d := int(a>>s&0xFF) - int(b>>s&0xFF)
		if d < -1 || d > 1 {
			return false
		}
	}
	return true
}

func samePixels(t *testing.T, name string, got, want *Image) {
	t.Helper()
	if got.W != want.W || got.H != want.H {
		t.Fatalf("%s: %dx%d, want %dx%d", name, got.W, got.H, want.W, want.H)
	}
	for i := range want.Pix {
		if !near(got.Pix[i], want.Pix[i]) {
			t.Fatalf("%s: pixel %d = %08x, want %08x", name, i, got.Pix[i], want.Pix[i])
		}
	}
}

// testImages covers every fast path and the At fallback, each with a
// non-zero origin (sub-images) and translucent pixels where the type has alpha.
func testImages() map[string]image.Image {
	const w, h = 37, 23
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	gray := image.NewGray(image.Rect(0, 0, w, h))
	pal := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White, color.NRGBA{200, 10, 90, 128}})
	for y := range h {
		for x := range w {
			c := color.NRGBA{uint8(x * 7), uint8(y * 11), uint8(x*y + 3), uint8(40 + x*5)}
			nrgba.SetNRGBA(x, y, c)
			rgba.Set(x, y, c)
			gray.SetGray(x, y, color.Gray{uint8(x*6 + y)})
			pal.SetColorIndex(x, y, uint8((x+y)%3))
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, nrgba, &jpeg.Options{Quality: 90})
	ycc, _ := jpeg.Decode(&buf)
	sub := func(m image.Image) image.Image {
		return m.(interface {
			SubImage(image.Rectangle) image.Image
		}).SubImage(image.Rect(3, 2, w, h))
	}
	return map[string]image.Image{"ycbcr": ycc, "nrgba": nrgba, "rgba": rgba, "gray": gray, "paletted": pal,
		"ycbcr sub": sub(ycc), "nrgba sub": sub(nrgba), "rgba sub": sub(rgba), "gray sub": sub(gray)}
}

func TestFromImageMatchesAt(t *testing.T) {
	for name, m := range testImages() {
		samePixels(t, name, FromImage(m), slowFromImage(m))
	}
}

func TestDecodeScalesStraightFromTheDecoder(t *testing.T) {
	for name, m := range testImages() {
		var buf bytes.Buffer
		if err := png.Encode(&buf, m); err != nil {
			t.Fatal(err)
		}
		got, err := DecodeImage(buf.Bytes(), 10, 10)
		if err != nil {
			t.Fatal(name, err)
		}
		decoded, _ := png.Decode(bytes.NewReader(buf.Bytes()))
		samePixels(t, name, got, Fit(slowFromImage(decoded), 10, 10))
	}
}

// A big cover is scaled without converting it at full size first.
func TestDecodeDoesNotConvertAtFullSize(t *testing.T) {
	big := image.NewNRGBA(image.Rect(0, 0, 2000, 2000))
	for i := range big.Pix {
		big.Pix[i] = uint8(i)
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, big, &jpeg.Options{Quality: 80})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	img, err := DecodeImage(buf.Bytes(), 100, 100)
	runtime.ReadMemStats(&after)
	if err != nil || img.W != 100 || img.H != 100 {
		t.Fatalf("%v %v", img, err)
	}
	// The JPEG decoder's own YCbCr (4:2:0) is 6 MB; a full-size Image would add 16 MB more.
	if got := after.TotalAlloc - before.TotalAlloc; got > 10<<20 {
		t.Fatalf("decoding allocated %d MB", got>>20)
	}
}

// pngHeader is a PNG with only its signature and IHDR, which is all
// DecodeConfig reads: a picture of any size without its pixels.
func pngHeader(w, h uint32, colorType byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, colorType // bit depth 8
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestDecodeRefusesImagesTooBigToDecode(t *testing.T) {
	// 3600² RGBA is 52 MB decoded.
	if _, err := DecodeImage(pngHeader(3600, 3600, 6), 100, 100); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err %v", err)
	}
	// A 4096² JPEG (YCbCr, 48 MiB as 4:4:4) is still accepted, and so is 3400² RGBA.
	for _, cfg := range []image.Config{{ColorModel: color.YCbCrModel, Width: 4096, Height: 4096},
		{ColorModel: color.NRGBAModel, Width: 3400, Height: 3400}} {
		if decodedBytes(cfg) > MaxDecodeBytes {
			t.Errorf("%dx%d %T refused", cfg.Width, cfg.Height, cfg.ColorModel)
		}
	}
	if decodedBytes(image.Config{ColorModel: color.NRGBA64Model, Width: 2600, Height: 2600}) <= MaxDecodeBytes {
		t.Error("a 2600² 16-bit PNG (54 MB) accepted")
	}
}

// Box sums are 64-bit: shrinking 90000 opaque white pixels into one stays white.
func TestResizeBigBoxesDoNotOverflow(t *testing.T) {
	src := NewImage(300, 300)
	for i := range src.Pix {
		src.Pix[i] = 0xFFFFFFFF
	}
	if got := Resize(src, 1, 1).Pix[0]; got != 0xFFFFFFFF {
		t.Fatalf("got %08x", got)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/gfx`

Expected: FAIL, e.g.:

```
undefined: decodedBytes
undefined: MaxDecodeBytes
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (main)):

```diff
diff --git a/internal/gfx/canvas.go b/internal/gfx/canvas.go
index a8d784d..d97137c 100644
--- a/internal/gfx/canvas.go
+++ b/internal/gfx/canvas.go
@@ -4,7 +4,6 @@ package gfx
 
 import (
 	"image"
-	"image/color"
 )
 
 // Color is 0xAARRGGBB. Alpha 0xFF is opaque.
@@ -134,16 +133,3 @@ func (c *Canvas) ToRGBA() *image.RGBA {
 	}
 	return img
 }
-
-// FromImage converts any image.Image to an Image.
-func FromImage(m image.Image) *Image {
-	b := m.Bounds()
-	out := NewImage(b.Dx(), b.Dy())
-	for y := 0; y < out.H; y++ {
-		for x := 0; x < out.W; x++ {
-			c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
-			out.Pix[y*out.W+x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
-		}
-	}
-	return out
-}
diff --git a/internal/gfx/image.go b/internal/gfx/image.go
index d9c4fab..1b09c74 100644
--- a/internal/gfx/image.go
+++ b/internal/gfx/image.go
@@ -4,65 +4,164 @@ import (
 	"bytes"
 	"fmt"
 	"image"
+	"image/color"
 	_ "image/jpeg" // cover art formats
 	_ "image/png"
 )
 
-// MaxDecodePixels guards against decompression bombs in cover art.
-const MaxDecodePixels = 4096 * 4096
+// MaxDecodeBytes bounds the memory a cover may take while it is decoded,
+// before it is scaled down: a 4096×4096 JPEG fits, a 4096×4096 RGBA PNG
+// doesn't. It guards against decompression bombs and keeps two art workers
+// within about 100 MB on the MiSTer.
+const MaxDecodeBytes = 48 << 20
+
+// decodedBytes estimates what the decoder allocates for an image of cfg.
+// YCbCr counts as 4:4:4 (the worst case); 4:2:0 needs half of that.
+func decodedBytes(cfg image.Config) int64 {
+	px := int64(cfg.Width) * int64(cfg.Height)
+	if _, ok := cfg.ColorModel.(color.Palette); ok {
+		return px
+	}
+	switch cfg.ColorModel {
+	case color.GrayModel, color.AlphaModel:
+		return px
+	case color.Gray16Model, color.Alpha16Model:
+		return 2 * px
+	case color.YCbCrModel:
+		return 3 * px
+	case color.RGBA64Model, color.NRGBA64Model:
+		return 8 * px
+	}
+	return 4 * px // RGBA, NRGBA, CMYK
+}
 
 // DecodeImage decodes JPEG or PNG bytes and scales the result to fit within
-// maxW×maxH (aspect preserved, never upscaled) with a box filter.
+// maxW×maxH (aspect preserved, never upscaled) with a box filter. Only the
+// scaled result is converted: the full-size picture exists once, in the
+// decoder's own format.
 func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
 	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
 	if err != nil {
 		return nil, fmt.Errorf("gfx: decode image: %w", err)
 	}
-	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxDecodePixels {
+	if cfg.Width <= 0 || cfg.Height <= 0 || decodedBytes(cfg) > MaxDecodeBytes {
 		return nil, fmt.Errorf("gfx: image %dx%d too large", cfg.Width, cfg.Height)
 	}
 	m, _, err := image.Decode(bytes.NewReader(data))
 	if err != nil {
 		return nil, fmt.Errorf("gfx: decode image: %w", err)
 	}
-	return Fit(FromImage(m), maxW, maxH), nil
+	b := m.Bounds()
+	w, h := fitSize(b.Dx(), b.Dy(), maxW, maxH)
+	return boxFilter(b.Dx(), b.Dy(), pixels(m), w, h), nil
+}
+
+// FromImage converts any image.Image to an Image.
+func FromImage(m image.Image) *Image {
+	b := m.Bounds()
+	return boxFilter(b.Dx(), b.Dy(), pixels(m), b.Dx(), b.Dy())
+}
+
+// pixels reads m's pixel (x, y), counted from its top-left corner, as
+// non-premultiplied ARGB. The common decoder outputs are read directly; other
+// types go through At.
+func pixels(m image.Image) func(x, y int) uint32 {
+	b := m.Bounds()
+	switch m := m.(type) {
+	case *image.YCbCr:
+		return func(x, y int) uint32 {
+			yi, ci := m.YOffset(b.Min.X+x, b.Min.Y+y), m.COffset(b.Min.X+x, b.Min.Y+y)
+			r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
+			return 0xFF<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(bl)
+		}
+	case *image.NRGBA:
+		return func(x, y int) uint32 {
+			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
+			return uint32(p[3])<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
+		}
+	case *image.RGBA:
+		return func(x, y int) uint32 {
+			p := m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y):]
+			a := uint32(p[3])
+			switch a {
+			case 0:
+				return 0
+			case 0xFF:
+				return 0xFF<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
+			}
+			un := func(c uint8) uint32 { return min((uint32(c)*0xFF+a/2)/a, 0xFF) }
+			return a<<24 | un(p[0])<<16 | un(p[1])<<8 | un(p[2])
+		}
+	case *image.Gray:
+		return func(x, y int) uint32 {
+			g := uint32(m.Pix[m.PixOffset(b.Min.X+x, b.Min.Y+y)])
+			return 0xFF<<24 | g<<16 | g<<8 | g
+		}
+	}
+	return func(x, y int) uint32 {
+		c := color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
+		return uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
+	}
+}
+
+// fitSize is sw×sh scaled down to fit maxW×maxH, preserving aspect ratio.
+func fitSize(sw, sh, maxW, maxH int) (int, int) {
+	if sw <= maxW && sh <= maxH {
+		return sw, sh
+	}
+	w, h := maxW, sh*maxW/sw
+	if h > maxH {
+		h, w = maxH, sw*maxH/sh
+	}
+	return max(w, 1), max(h, 1)
 }
 
 // Fit returns src scaled down to fit maxW×maxH, preserving aspect ratio.
 func Fit(src *Image, maxW, maxH int) *Image {
-	if src.W <= maxW && src.H <= maxH {
+	w, h := fitSize(src.W, src.H, maxW, maxH)
+	if w == src.W && h == src.H {
 		return src
 	}
-	w, h := maxW, src.H*maxW/src.W
-	if h > maxH {
-		h, w = maxH, src.W*maxH/src.H
-	}
-	return Resize(src, max(w, 1), max(h, 1))
+	return Resize(src, w, h)
 }
 
 // Resize box-filters src down to w×h (use Canvas.Blit for upscaling).
 func Resize(src *Image, w, h int) *Image {
+	return boxFilter(src.W, src.H, func(x, y int) uint32 { return src.Pix[y*src.W+x] }, w, h)
+}
+
+// boxFilter averages the sw×sh source read by px down to w×h, weighting
+// colour by alpha. Sums are 64-bit: a box can hold any number of pixels.
+func boxFilter(sw, sh int, px func(x, y int) uint32, w, h int) *Image {
 	out := NewImage(w, h)
+	if w == sw && h == sh {
+		for y := 0; y < h; y++ {
+			for x := 0; x < w; x++ {
+				out.Pix[y*w+x] = px(x, y)
+			}
+		}
+		return out
+	}
 	for y := 0; y < h; y++ {
-		sy0, sy1 := y*src.H/h, max((y+1)*src.H/h, y*src.H/h+1)
+		sy0, sy1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
 		for x := 0; x < w; x++ {
-			sx0, sx1 := x*src.W/w, max((x+1)*src.W/w, x*src.W/w+1)
-			var a, r, g, b, n uint32
+			sx0, sx1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
+			var a, r, g, b, n uint64
 			for sy := sy0; sy < sy1; sy++ {
 				for sx := sx0; sx < sx1; sx++ {
-					p := src.Pix[sy*src.W+sx]
-					pa := p >> 24
+					p := px(sx, sy)
+					pa := uint64(p >> 24)
 					a += pa
-					r += (p >> 16 & 0xFF) * pa
-					g += (p >> 8 & 0xFF) * pa
-					b += (p & 0xFF) * pa
+					r += uint64(p>>16&0xFF) * pa
+					g += uint64(p>>8&0xFF) * pa
+					b += uint64(p&0xFF) * pa
 					n++
 				}
 			}
 			if a == 0 {
 				continue
 			}
-			out.Pix[y*w+x] = (a/n)<<24 | (r/a)<<16 | (g/a)<<8 | b/a
+			out.Pix[y*w+x] = uint32(a/n)<<24 | uint32(r/a)<<16 | uint32(g/a)<<8 | uint32(b/a)
 		}
 	}
 	return out
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/gfx`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/gfx/canvas.go internal/gfx/decode_test.go internal/gfx/image.go
git commit -m "gfx: covers scaled straight from the decoder with a 48 MiB decode budget; fast pixel paths; 64-bit box sums" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Disk cache: no half-written entries, a size that recovers

**Files:**
- Modify: `internal/cache/cache.go`
- Test: `internal/cache/cache_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `Open` deletes stray `*.tmp` files (from a crash mid-write) and doesn't count them.
  - `Put` removes its `.tmp` when the write fails. `var writeFile = os.WriteFile` is a test hook.
  - The eviction walk recounts `size` from the files on disk, so deletions made outside the cache stop causing needless evictions.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/cache/cache_test.go b/internal/cache/cache_test.go
index b2df22e..d659273 100644
--- a/internal/cache/cache_test.go
+++ b/internal/cache/cache_test.go
@@ -2,6 +2,9 @@ package cache
 
 import (
 	"bytes"
+	"errors"
+	"os"
+	"path/filepath"
 	"testing"
 	"time"
 )
@@ -95,3 +98,63 @@ func TestDeleteAdjustsSize(t *testing.T) {
 		t.Fatal("a still present")
 	}
 }
+
+func TestOpenRemovesHalfWrittenEntries(t *testing.T) {
+	dir := t.TempDir()
+	os.WriteFile(filepath.Join(dir, "0123.tmp"), bytes.Repeat([]byte{9}, 100), 0o644)
+	os.WriteFile(filepath.Join(dir, "4567"), bytes.Repeat([]byte{1}, 50), 0o644)
+	d, err := Open(dir, 1000)
+	if err != nil {
+		t.Fatal(err)
+	}
+	if d.Size() != 50 {
+		t.Fatalf("size %d, want 50 (the .tmp isn't an entry)", d.Size())
+	}
+	if _, err := os.Stat(filepath.Join(dir, "0123.tmp")); !os.IsNotExist(err) {
+		t.Fatal("the half-written entry is still there")
+	}
+}
+
+func TestAFailedWriteLeavesNothingBehind(t *testing.T) {
+	dir := t.TempDir()
+	d, _ := Open(dir, 1000)
+	old := writeFile
+	writeFile = func(name string, data []byte, perm os.FileMode) error {
+		os.WriteFile(name, data[:len(data)/2], perm) // the card filled up halfway
+		return errors.New("no space left on device")
+	}
+	defer func() { writeFile = old }()
+	if err := d.Put("a", bytes.Repeat([]byte{1}, 100)); err == nil {
+		t.Fatal("Put hid the write error")
+	}
+	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
+		t.Fatalf("left behind %v", ents)
+	}
+	if d.Size() != 0 {
+		t.Fatalf("size %d after a failed write", d.Size())
+	}
+}
+
+// Files deleted behind the cache's back are noticed at the next eviction,
+// instead of evicting live entries to make room that is already free.
+func TestEvictionRecountsAfterOutsideDeletes(t *testing.T) {
+	dir := t.TempDir()
+	d, _ := Open(dir, 1000)
+	clock := time.Unix(1000, 0)
+	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
+	for _, k := range []string{"a", "b", "c", "d", "e"} {
+		d.Put(k, bytes.Repeat([]byte{1}, 150)) // 750 bytes
+	}
+	for _, k := range []string{"c", "d", "e"} {
+		os.Remove(d.path(k)) // cleaned up by hand; the cache still counts 750
+	}
+	d.Put("f", bytes.Repeat([]byte{2}, 300)) // counted 1050 > 1000: an eviction walk
+	for _, k := range []string{"a", "b", "f"} {
+		if _, ok := d.Get(k); !ok {
+			t.Errorf("%s was evicted, though only 600 bytes are on disk", k)
+		}
+	}
+	if d.Size() != 600 {
+		t.Fatalf("size %d, want 600", d.Size())
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/cache`

Expected: FAIL, e.g.:

```
undefined: writeFile
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/cache/cache.go b/internal/cache/cache.go
index 42fcba4..656c71f 100644
--- a/internal/cache/cache.go
+++ b/internal/cache/cache.go
@@ -11,10 +11,14 @@ import (
 	"os"
 	"path/filepath"
 	"sort"
+	"strings"
 	"sync"
 	"time"
 )
 
+// writeFile is replaceable in tests (a full SD card fails mid-write).
+var writeFile = os.WriteFile
+
 type Disk struct {
 	dir string
 	max int64
@@ -25,6 +29,7 @@ type Disk struct {
 }
 
 // Open uses dir (created if needed) with a byte budget. maxBytes <= 0 disables caching.
+// Half-written entries left by a crash (*.tmp) are removed.
 func Open(dir string, maxBytes int64) (*Disk, error) {
 	if err := os.MkdirAll(dir, 0o755); err != nil {
 		return nil, err
@@ -32,9 +37,15 @@ func Open(dir string, maxBytes int64) (*Disk, error) {
 	d := &Disk{dir: dir, max: maxBytes, now: time.Now}
 	entries, _ := os.ReadDir(dir)
 	for _, e := range entries {
-		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
-			d.size += info.Size()
+		info, err := e.Info()
+		if err != nil || !info.Mode().IsRegular() {
+			continue
+		}
+		if strings.HasSuffix(e.Name(), ".tmp") {
+			os.Remove(filepath.Join(dir, e.Name()))
+			continue
 		}
+		d.size += info.Size()
 	}
 	return d, nil
 }
@@ -73,7 +84,8 @@ func (d *Disk) Put(key string, data []byte) error {
 		old = info.Size()
 	}
 	tmp := p + ".tmp"
-	if err := os.WriteFile(tmp, data, 0o644); err != nil {
+	if err := writeFile(tmp, data, 0o644); err != nil {
+		os.Remove(tmp) // don't leave a partial entry behind (a full card)
 		return err
 	}
 	if err := os.Rename(tmp, p); err != nil {
@@ -111,7 +123,9 @@ func (d *Disk) Delete(key string) {
 var evictions int
 
 // evictLocked removes the oldest entries down to a low-water mark of 90% of
-// the budget, so the directory walk happens once per ~10% of new data.
+// the budget, so the directory walk happens once per ~10% of new data. The
+// walk also recounts the size, which drifts if files are deleted behind the
+// cache's back.
 func (d *Disk) evictLocked(keep string) {
 	evictions++
 	target := d.max * 9 / 10
@@ -121,11 +135,17 @@ func (d *Disk) evictLocked(keep string) {
 		mod  time.Time
 	}
 	var all []ent
+	d.size = 0
 	filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
-		if err != nil || e.IsDir() || p == keep {
+		if err != nil || e.IsDir() {
+			return nil
+		}
+		info, err := e.Info()
+		if err != nil {
 			return nil
 		}
-		if info, err := e.Info(); err == nil {
+		d.size += info.Size()
+		if p != keep {
 			all = append(all, ent{p, info.Size(), info.ModTime()})
 		}
 		return nil
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/cache`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/cache.go internal/cache/cache_test.go
git commit -m "cache: remove half-written entries; the eviction walk recounts the size" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Stream memory: a small ring until the track plays

**Files:**
- Modify: `internal/stream/reader.go`
- Test: `internal/stream/reader_test.go` (modified)

**Interfaces:**
- **Produces:**
  - A reader opened with `PrefetchBytes > 0` allocates `min(WindowBytes, 1.25 × PrefetchBytes)`.
  - `Promote` grows the ring to `WindowBytes` with `growLocked`, keeping every buffered byte.
  - `effectiveLo` keeps at most a quarter of the current ring behind the reader. Without that cap, the small ring would leave the fetcher no room.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/stream/reader_test.go b/internal/stream/reader_test.go
index b3baac1..0db05a2 100644
--- a/internal/stream/reader_test.go
+++ b/internal/stream/reader_test.go
@@ -580,3 +580,52 @@ func TestReconnectWhenHeadersNeverArrive(t *testing.T) {
 		t.Fatalf("reconnect took %v, should be ~3s", elapsed)
 	}
 }
+
+// A prefetching reader (a queued next track) holds a small ring until it is
+// promoted, then grows to the full window without losing what it buffered.
+func TestPrefetchRingIsSmallUntilPromoted(t *testing.T) {
+	s := newServer(t, 8<<20, nil)
+	o := testOptions()
+	o.PrefetchBytes = 64 << 10
+	r := open(t, s.URL, o)
+	r.mu.Lock()
+	small := len(r.ring)
+	r.mu.Unlock()
+	if small != 80<<10 {
+		t.Fatalf("prefetch ring %d bytes, want %d", small, 80<<10)
+	}
+	buf := make([]byte, 200<<10) // wraps the small ring more than twice
+	if _, err := io.ReadFull(r, buf); err != nil {
+		t.Fatal(err)
+	}
+	checkBytes(t, buf, 0)
+	time.Sleep(50 * time.Millisecond) // let the fetcher fill up to the cap
+	r.Promote()
+	r.mu.Lock()
+	big := len(r.ring)
+	r.mu.Unlock()
+	if big != 1<<20 {
+		t.Fatalf("promoted ring %d bytes, want %d", big, 1<<20)
+	}
+	buf = make([]byte, 300<<10)
+	if _, err := io.ReadFull(r, buf); err != nil {
+		t.Fatal(err)
+	}
+	checkBytes(t, buf, 200<<10) // what was buffered before Promote came across intact
+	reqs := s.requests.Load()
+	checkBytes(t, readAt(t, r, 400<<10, 64<<10), 400<<10) // behind the reader, inside the grown window
+	if s.requests.Load() != reqs {
+		t.Fatal("a seek inside the grown window made a new request")
+	}
+}
+
+// A reader opened without a prefetch limit gets the whole window at once.
+func TestPlainReaderHasTheFullWindow(t *testing.T) {
+	s := newServer(t, 1<<20, nil)
+	r := open(t, s.URL, testOptions())
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	if len(r.ring) != 1<<20 {
+		t.Fatalf("ring %d bytes", len(r.ring))
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/stream`

Expected: FAIL, e.g.:

```
--- FAIL: TestPrefetchRingIsSmallUntilPromoted (0.00s)
reader_test.go:595: prefetch ring 1048576 bytes, want 81920
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/stream/reader.go b/internal/stream/reader.go
index 588db74..fe0d8af 100644
--- a/internal/stream/reader.go
+++ b/internal/stream/reader.go
@@ -28,7 +28,7 @@ type Options struct {
 	WindowBytes   int64           // RAM per reader; default 32 MiB
 	BehindBytes   int64           // kept behind the read position; default WindowBytes/4
 	NearBytes     int64           // forward seeks within this distance of the window just wait; default 256 KiB
-	PrefetchBytes int64           // if > 0, fetch at most this far ahead of the read position until Promote
+	PrefetchBytes int64           // if > 0, fetch at most this far ahead of the read position until Promote (ring 1.25×, grown to WindowBytes then)
 	StallTimeout  time.Duration   // no bytes for this long -> reconnect; default 10 s
 	Backoff       []time.Duration // retry delays; default 0.5, 1, 2, 4, 8 s (last repeats)
 	RetryBudget   time.Duration   // give up after this long without progress; default 30 s
@@ -91,7 +91,13 @@ func Open(ctx context.Context, url string, o Options) (*Reader, error) {
 	if o.RetryBudget <= 0 {
 		o.RetryBudget = 30 * time.Second
 	}
-	r := &Reader{url: url, o: o, ring: make([]byte, o.WindowBytes), size: -1, prefetch: o.PrefetchBytes}
+	ringBytes := o.WindowBytes
+	if o.PrefetchBytes > 0 {
+		// A queued successor needs only its prefetch until it plays; the
+		// full window comes with Promote.
+		ringBytes = min(o.WindowBytes, o.PrefetchBytes+o.PrefetchBytes/4)
+	}
+	r := &Reader{url: url, o: o, ring: make([]byte, ringBytes), size: -1, prefetch: o.PrefetchBytes}
 	r.cond = sync.NewCond(&r.mu)
 
 	fctx, cancel := context.WithCancel(context.Background())
@@ -128,14 +134,32 @@ func (r *Reader) Buffered() int64 {
 	return r.hi - r.pos
 }
 
-// Promote lifts the prefetch limit.
+// Promote lifts the prefetch limit and grows the ring to the full window.
 func (r *Reader) Promote() {
 	r.mu.Lock()
 	r.prefetch = 0
+	if int64(len(r.ring)) < r.o.WindowBytes {
+		r.growLocked(r.o.WindowBytes)
+	}
 	r.cond.Broadcast()
 	r.mu.Unlock()
 }
 
+// growLocked moves the buffered bytes [lo, hi) into a new ring of n bytes.
+func (r *Reader) growLocked(n int64) {
+	ring := make([]byte, n)
+	old := int64(len(r.ring))
+	for off := r.lo; off < r.hi; {
+		s := off % old
+		k := min(old-s, r.hi-off)
+		d := off % n
+		c := int64(copy(ring[d:], r.ring[s:s+k]))
+		copy(ring, r.ring[s+c:s+k]) // the part that wraps in the new ring, if any
+		off += k
+	}
+	r.ring = ring
+}
+
 func (r *Reader) Read(p []byte) (int, error) {
 	if len(p) == 0 {
 		return 0, nil
@@ -243,9 +267,12 @@ func (r *Reader) copyOut(p []byte) int {
 	return n
 }
 
-// effectiveLo is the lowest offset we must keep: BehindBytes behind pos.
+// effectiveLo is the lowest offset we must keep: BehindBytes behind pos, or
+// a quarter of the ring while it is still the small prefetch ring (keeping
+// more than the ring holds would leave the fetcher no room at all).
 func (r *Reader) effectiveLo() int64 {
-	lo := max(r.lo, r.pos-r.o.BehindBytes)
+	behind := min(r.o.BehindBytes, int64(len(r.ring))/4)
+	lo := max(r.lo, r.pos-behind)
 	return min(lo, r.hi)
 }
 
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/stream`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/stream/reader.go internal/stream/reader_test.go
git commit -m "stream: a prefetching reader holds a small ring until promoted" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Stream hardening: 416 mid-file, 429 and Retry-After

**Files:**
- Modify: `internal/stream/reader.go`
- Test: `internal/stream/reader_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `HTTPError.RetryAfter` and `parseRetryAfter(h, now)`, which accepts seconds or an HTTP date.
  - `retryLater(code)` covers 408, 429, 502, 503 and 504.
  - `openRequest` retries the first request for those codes within `RetryBudget` and ctx. A 404 still fails `Open` at once.
  - In `fetch`:
    - A 416 before the known size is a terminal error that says the file changed. At or after the size it is still the end.
    - Other 4xx stay terminal.
  - `sleepBackoff` waits at least the Retry-After, and gives up at once when that is past the budget.
  - The `StallTimeout` comment says it bounds the wait for headers, so it is the effective open timeout.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/stream/reader_test.go b/internal/stream/reader_test.go
index 0db05a2..afea8f9 100644
--- a/internal/stream/reader_test.go
+++ b/internal/stream/reader_test.go
@@ -629,3 +629,112 @@ func TestPlainReaderHasTheFullWindow(t *testing.T) {
 		t.Fatalf("ring %d bytes", len(r.ring))
 	}
 }
+
+// dropAfter serves the first n bytes of the file as a 206, then drops the connection.
+func dropAfter(w http.ResponseWriter, size, n int64) {
+	w.Header().Set("Content-Range", "bytes 0-"+strconv.FormatInt(size-1, 10)+"/"+strconv.FormatInt(size, 10))
+	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
+	w.WriteHeader(http.StatusPartialContent)
+	io.CopyN(w, &virtualFile{size: size}, n)
+	w.(http.Flusher).Flush()
+	panic(http.ErrAbortHandler)
+}
+
+// A 416 on reconnect before the end means the file changed: an error, not
+// a quiet early end of the track.
+func TestRangeRefusedMidFileIsAnError(t *testing.T) {
+	const size = 1 << 20
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n == 1 {
+			dropAfter(w, size, 100<<10)
+		}
+		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
+		return true
+	})
+	r := open(t, s.URL, testOptions())
+	got, err := io.ReadAll(r)
+	var he *HTTPError
+	if !errors.As(err, &he) || he.StatusCode != http.StatusRequestedRangeNotSatisfiable {
+		t.Fatalf("ReadAll = %d bytes, %v; want the 416 as an error", len(got), err)
+	}
+	checkBytes(t, got, 0)
+}
+
+// Mid-stream, a 429 is retried after the server's Retry-After.
+func TestTooManyRequestsIsRetriedAfterRetryAfter(t *testing.T) {
+	const size = 1 << 20
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		switch n {
+		case 1:
+			dropAfter(w, size, 100<<10)
+		case 2:
+			w.Header().Set("Retry-After", "1")
+			w.WriteHeader(http.StatusTooManyRequests)
+			return true
+		}
+		return false
+	})
+	o := testOptions()
+	o.RetryBudget = 3 * time.Second
+	r := open(t, s.URL, o)
+	start := time.Now()
+	got, err := io.ReadAll(r)
+	if err != nil || len(got) != size {
+		t.Fatalf("ReadAll = %d bytes, %v", len(got), err)
+	}
+	checkBytes(t, got, 0)
+	if d := time.Since(start); d < 900*time.Millisecond {
+		t.Fatalf("retried after %v, before the server's Retry-After of 1 s", d)
+	}
+}
+
+// A Retry-After longer than the retry budget gives up at once instead of
+// sleeping through it.
+func TestRetryAfterBeyondTheBudgetGivesUp(t *testing.T) {
+	const size = 1 << 20
+	s := newServer(t, size, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n == 1 {
+			dropAfter(w, size, 100<<10)
+		}
+		w.Header().Set("Retry-After", "3600")
+		w.WriteHeader(http.StatusTooManyRequests)
+		return true
+	})
+	r := open(t, s.URL, testOptions())
+	start := time.Now()
+	if _, err := io.ReadAll(r); err == nil {
+		t.Fatal("ReadAll succeeded")
+	}
+	if d := time.Since(start); d > time.Second {
+		t.Fatalf("gave up after %v", d)
+	}
+}
+
+// The first request is retried too when the server asks to come back later;
+// other errors still fail Open at once.
+func TestOpenRetriesWhenTheServerIsBusy(t *testing.T) {
+	s := newServer(t, 1<<20, func(n int, w http.ResponseWriter, r *http.Request) bool {
+		if n <= 2 {
+			w.WriteHeader(http.StatusServiceUnavailable)
+			return true
+		}
+		return false
+	})
+	r := open(t, s.URL, testOptions())
+	checkBytes(t, readAt(t, r, 0, 1000), 0)
+	if n := s.requests.Load(); n != 3 {
+		t.Fatalf("%d requests, want 3", n)
+	}
+}
+
+func TestParseRetryAfter(t *testing.T) {
+	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
+	for h, want := range map[string]time.Duration{
+		"": 0, "5": 5 * time.Second, " 2 ": 2 * time.Second, "-1": 0, "soon": 0,
+		"Tue, 29 Sep 2026 12:00:30 GMT": 30 * time.Second, "Tue, 29 Sep 2026 11:00:00 GMT": 0,
+	} {
+		if got := parseRetryAfter(h, now); got != want {
+			t.Errorf("parseRetryAfter(%q) = %v, want %v", h, got, want)
+		}
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/stream`

Expected: FAIL, e.g.:

```
undefined: parseRetryAfter
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/stream/reader.go b/internal/stream/reader.go
index fe0d8af..b27f2c6 100644
--- a/internal/stream/reader.go
+++ b/internal/stream/reader.go
@@ -29,7 +29,7 @@ type Options struct {
 	BehindBytes   int64           // kept behind the read position; default WindowBytes/4
 	NearBytes     int64           // forward seeks within this distance of the window just wait; default 256 KiB
 	PrefetchBytes int64           // if > 0, fetch at most this far ahead of the read position until Promote (ring 1.25×, grown to WindowBytes then)
-	StallTimeout  time.Duration   // no bytes for this long -> reconnect; default 10 s
+	StallTimeout  time.Duration   // no bytes (or no response headers) for this long -> reconnect; Open fails after it; default 10 s
 	Backoff       []time.Duration // retry delays; default 0.5, 1, 2, 4, 8 s (last repeats)
 	RetryBudget   time.Duration   // give up after this long without progress; default 30 s
 }
@@ -44,10 +44,37 @@ var (
 type HTTPError struct {
 	StatusCode int
 	Status     string
+	RetryAfter time.Duration // the server's Retry-After, if it sent one
 }
 
 func (e *HTTPError) Error() string { return "stream: HTTP " + e.Status }
 
+// retryLater: statuses where the server asks to come back (busy, rate
+// limited, a gateway in the way), even on the first request.
+func retryLater(code int) bool {
+	switch code {
+	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway,
+		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
+		return true
+	}
+	return false
+}
+
+// parseRetryAfter reads a Retry-After header: seconds or an HTTP date.
+func parseRetryAfter(h string, now time.Time) time.Duration {
+	h = strings.TrimSpace(h)
+	if h == "" {
+		return 0
+	}
+	if s, err := strconv.Atoi(h); err == nil && s >= 0 {
+		return time.Duration(s) * time.Second
+	}
+	if t, err := http.ParseTime(h); err == nil && t.After(now) {
+		return t.Sub(now)
+	}
+	return 0
+}
+
 type Reader struct {
 	url string
 	o   Options
@@ -102,7 +129,7 @@ func Open(ctx context.Context, url string, o Options) (*Reader, error) {
 
 	fctx, cancel := context.WithCancel(context.Background())
 	stop := context.AfterFunc(ctx, cancel)
-	resp, reqCancel, err := r.request(fctx, 0)
+	resp, reqCancel, err := r.openRequest(fctx)
 	stop()
 	if err != nil {
 		cancel()
@@ -310,6 +337,31 @@ func stripURL(err error) error {
 	return err
 }
 
+// openRequest is the first request. A server that asks to come back later
+// (429 and the like) is retried within RetryBudget and ctx; anything else
+// fails Open at once.
+func (r *Reader) openRequest(ctx context.Context) (*http.Response, context.CancelFunc, error) {
+	start := time.Now()
+	for attempt := 0; ; attempt++ {
+		resp, reqCancel, err := r.request(ctx, 0)
+		var he *HTTPError
+		if err == nil || !errors.As(err, &he) || !retryLater(he.StatusCode) {
+			return resp, reqCancel, err
+		}
+		d := max(r.o.Backoff[min(attempt, len(r.o.Backoff)-1)], he.RetryAfter)
+		if time.Since(start)+d > r.o.RetryBudget {
+			return nil, nil, err
+		}
+		t := time.NewTimer(d)
+		select {
+		case <-t.C:
+		case <-ctx.Done():
+			t.Stop()
+			return nil, nil, err
+		}
+	}
+}
+
 func (r *Reader) request(ctx context.Context, off int64) (*http.Response, context.CancelFunc, error) {
 	rctx, cancel := context.WithCancel(ctx)
 	req, err := http.NewRequestWithContext(rctx, http.MethodGet, r.url, nil)
@@ -339,7 +391,8 @@ func (r *Reader) request(ctx context.Context, off int64) (*http.Response, contex
 	default:
 		resp.Body.Close()
 		cancel()
-		return nil, nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
+		return nil, nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status,
+			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
 	}
 	if r.o.CheckResponse != nil {
 		if err := r.o.CheckResponse(resp); err != nil {
@@ -381,10 +434,19 @@ func (r *Reader) fetch(ctx context.Context, gen int, off int64, resp *http.Respo
 				}
 				var he *HTTPError
 				if errors.As(err, &he) && he.StatusCode == http.StatusRequestedRangeNotSatisfiable {
-					r.finish(gen, nil)
+					r.mu.Lock()
+					size := r.size
+					r.mu.Unlock()
+					if size >= 0 && off < size {
+						// Not the end: the file is shorter than it was (changed on
+						// the server). Say so rather than end the track early.
+						r.finish(gen, fmt.Errorf("stream: the server refused byte %d of %d (did the file change?): %w", off, size, err))
+					} else {
+						r.finish(gen, nil)
+					}
 					return
 				}
-				if errors.As(err, &he) && he.StatusCode < 500 {
+				if errors.As(err, &he) && he.StatusCode < 500 && !retryLater(he.StatusCode) {
 					r.finish(gen, err)
 					return
 				}
@@ -497,6 +559,14 @@ func (r *Reader) sleepBackoff(ctx context.Context, gen int, attempt *int, lastPr
 	}
 	d := r.o.Backoff[min(*attempt, len(r.o.Backoff)-1)]
 	*attempt++
+	var he *HTTPError
+	if errors.As(cause, &he) && he.RetryAfter > d {
+		if time.Since(lastProgress)+he.RetryAfter > r.o.RetryBudget {
+			r.finish(gen, fmt.Errorf("stream: giving up: %w", cause))
+			return false
+		}
+		d = he.RetryAfter
+	}
 	t := time.NewTimer(d)
 	defer t.Stop()
 	select {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/stream`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/stream/reader.go internal/stream/reader_test.go
git commit -m "stream: 416 before the end is an error; 408/429/5xx retried with Retry-After, on Open too" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: Audio: device calls after close, the tail while the next track opens, no per-chunk allocations

**Files:**
- Modify: `internal/audio/device.c`, `internal/audio/engine.go`, `internal/audio/resampler.go`
- Test: `internal/audio/device_test.go`, `internal/audio/engine_test.go`, `internal/audio/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `mss_device_write`, `mss_device_space`, `mss_device_flush` and `mss_device_stop_for_test` do nothing once the device is closed.
  - Engine:
    - `writePending()` is used by the run loop and by `finishCur` while it waits for a successor.
    - `pendBuf` backs `pending`, and `emit` reuses it.
  - `Resampler` keeps its C frame counters in the struct (`cin`, `cout`) so they don't escape to the heap.
  - The fake decoder gains `eofWithData`.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/audio/device_test.go b/internal/audio/device_test.go
index 67e47e1..1a5d1b9 100644
--- a/internal/audio/device_test.go
+++ b/internal/audio/device_test.go
@@ -62,3 +62,28 @@ func TestFlushAfterDeviceStopped(t *testing.T) {
 		t.Fatalf("after Flush Consumed = %d, want 4800 (everything written)", got)
 	}
 }
+
+// After Close, the device calls are harmless no-ops (a late Flush or Write
+// from a shutting-down engine must not touch the freed ring).
+func TestDeviceCallsAfterCloseAreSafe(t *testing.T) {
+	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
+	if err != nil {
+		t.Fatal(err)
+	}
+	out.Close()
+	if n := out.Write(make([]float32, 100*2)); n != 0 {
+		t.Fatalf("Write after Close took %d frames", n)
+	}
+	out.Flush()
+	out.SetPaused(true)
+	out.SetVolume(0.5)
+	out.Close()
+	stopDeviceForTest()
+	again, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
+	if err != nil {
+		t.Fatal("the device can't be opened again:", err)
+	}
+	again.SetPaused(false)
+	again.SetVolume(1)
+	again.Close()
+}
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index cb5921d..4c4f007 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -793,3 +793,50 @@ func TestEngineSeekAfterPlayOfSameTrackQueuesBehindPlay(t *testing.T) {
 		return false
 	})
 }
+
+// While the next track is still opening, the end of the current one keeps
+// going to the device; it isn't held back until the successor arrives.
+func TestEngineKeepsFeedingTheTailWhileTheNextTrackOpens(t *testing.T) {
+	out := newFakeOutput(100000)
+	e := newTestEngine(out)
+	defer e.Close()
+	a := newFakeSource(ramp(300, 0), OutputRate)
+	a.eofWithData = true // 256 frames, then the last 44 with the EOF
+	b := newFakeSource(ramp(100, 300), OutputRate)
+	b.openBlock = make(chan struct{})
+	defer close(b.openBlock)
+	e.Play(Track{ID: 1, Source: a})
+	e.QueueNext(Track{ID: 2, Source: b})
+	expectEvent(t, e, EventStarted, 1)
+	waitFor(t, "all of track 1 written while track 2 opens", func() bool {
+		out.consume(1 << 30)
+		return len(out.written())/2 == 300
+	})
+}
+
+// Steady-state decoding doesn't allocate: the pending buffer is reused.
+func TestEmitDoesNotAllocate(t *testing.T) {
+	for _, rate := range []int{OutputRate, 44100} {
+		e := &Engine{}
+		if rate != OutputRate {
+			rs, err := NewResampler(rate, OutputRate, DefaultResampleQuality)
+			if err != nil {
+				t.Fatal(err)
+			}
+			defer rs.Close()
+			e.rs = rs
+		}
+		v := &voice{gain: 0.5}
+		chunk := ramp(2048, 0)
+		samples := make([]float32, len(chunk))
+		run := func() {
+			copy(samples, chunk)
+			e.emit(samples, v)
+			e.pending = e.pending[len(e.pending):] // the device took it all
+		}
+		run() // the first chunk sizes the buffer
+		if n := testing.AllocsPerRun(50, run); n != 0 {
+			t.Errorf("%d Hz: %v allocations per chunk", rate, n)
+		}
+	}
+}
diff --git a/internal/audio/fakes_test.go b/internal/audio/fakes_test.go
index 4aef1bf..9d749ea 100644
--- a/internal/audio/fakes_test.go
+++ b/internal/audio/fakes_test.go
@@ -69,6 +69,7 @@ type fakeSource struct {
 	openStarted     chan struct{} // closed when fakeOpen begins
 	openStartedOnce sync.Once     // protects closing openStarted
 	seekBlock       chan struct{} // blocks SeekFrame if set, until closed
+	eofWithData     bool          // the last Read returns its frames together with io.EOF
 	once            sync.Once
 	closed          chan struct{}
 }
@@ -144,6 +145,9 @@ func (d *fakeDecoder) Read(dst []float32) (int, error) {
 	}
 	n := copy(dst, d.src.pcm[d.pos:])
 	d.pos += n
+	if d.src.eofWithData && d.pos >= len(d.src.pcm) {
+		return n / 2, io.EOF // as a stream error arrives with the last frames
+	}
 	return n / 2, nil
 }
 
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/audio`

Expected: FAIL, e.g.:

```
--- FAIL: TestDeviceCallsAfterCloseAreSafe (0.00s)
device_test.go:75: Write after Close took 100 frames
--- FAIL: TestEngineKeepsFeedingTheTailWhileTheNextTrackOpens (3.00s)
engine_test.go:811: timed out waiting for all of track 1 written while track 2 opens
--- FAIL: TestEmitDoesNotAllocate (0.02s)
engine_test.go:839: 48000 Hz: 1 allocations per chunk
engine_test.go:839: 44100 Hz: 2 allocations per chunk
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/audio/device.c b/internal/audio/device.c
index c479d53..0787f8d 100644
--- a/internal/audio/device.c
+++ b/internal/audio/device.c
@@ -133,7 +133,13 @@ void mss_device_close(void) {
     g_open = 0;
 }
 
+/* After mss_device_close the ring and device are gone: every call below is
+   a no-op then (write takes nothing, flush does nothing). */
+
 uint32_t mss_device_write(const float* frames, uint32_t frame_count) {
+    if (!g_open) {
+        return 0;
+    }
     uint32_t done = 0;
     while (done < frame_count) {
         ma_uint32 n = frame_count - done;
@@ -149,6 +155,9 @@ uint32_t mss_device_write(const float* frames, uint32_t frame_count) {
 }
 
 uint32_t mss_device_space(void) {
+    if (!g_open) {
+        return 0;
+    }
     return ma_pcm_rb_available_write(&g_ring);
 }
 
@@ -171,6 +180,9 @@ static int device_stopped(void) {
 }
 
 void mss_device_flush(void) {
+    if (!g_open) {
+        return;
+    }
     if (device_stopped()) {
         drain_ring();
         return;
@@ -192,5 +204,7 @@ void mss_device_flush(void) {
 
 /* Test hook: stops the device callback as a failed or unplugged device would. */
 void mss_device_stop_for_test(void) {
-    ma_device_stop(&g_device);
+    if (g_open) {
+        ma_device_stop(&g_device);
+    }
 }
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index 98a2e5d..656debe 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -115,7 +115,8 @@ type Engine struct {
 	chainOut uint64
 	chainIn  uint64
 	written  uint64
-	pending  []float32
+	pending  []float32 // resampled output not yet taken by the device
+	pendBuf  []float32 // pending's backing array, reused so decoding doesn't allocate
 	scratch  []float32
 }
 
@@ -308,9 +309,7 @@ func (e *Engine) run() {
 		}
 		switch {
 		case len(e.pending) > 0:
-			n := e.o.Output.Write(e.pending)
-			e.written += uint64(n)
-			e.pending = e.pending[n*2:]
+			e.writePending()
 			if len(e.pending) > 0 && !e.wait(e.o.Poll) {
 				return
 			}
@@ -438,6 +437,13 @@ func (e *Engine) decodeChunk() {
 	e.finishCur()
 }
 
+// writePending hands the device as much of pending as it takes.
+func (e *Engine) writePending() {
+	n := e.o.Output.Write(e.pending)
+	e.written += uint64(n)
+	e.pending = e.pending[n*2:]
+}
+
 func (e *Engine) emit(samples []float32, v *voice) {
 	switch {
 	case v.gain > 1:
@@ -450,7 +456,7 @@ func (e *Engine) emit(samples []float32, v *voice) {
 		}
 	}
 	if len(e.pending) == 0 {
-		e.pending = e.pending[:0:0]
+		e.pending = e.pendBuf[:0] // start over at the front of the buffer
 	}
 	if e.rs != nil {
 		e.pending = e.rs.Process(samples, e.pending)
@@ -458,6 +464,9 @@ func (e *Engine) emit(samples []float32, v *voice) {
 	} else {
 		e.pending = append(e.pending, samples...)
 	}
+	if cap(e.pending) > cap(e.pendBuf) {
+		e.pendBuf = e.pending[:0] // it grew: keep the bigger buffer
+	}
 }
 
 // clipKnee is where softClip starts bending: below it samples pass exactly.
@@ -494,6 +503,7 @@ func (e *Engine) finishCur() {
 			e.abandonOpening()
 			break
 		}
+		e.writePending() // the end of this track still plays while the next one opens
 		if !e.wait(e.o.Poll) {
 			return
 		}
diff --git a/internal/audio/resampler.go b/internal/audio/resampler.go
index c9e2b78..3431322 100644
--- a/internal/audio/resampler.go
+++ b/internal/audio/resampler.go
@@ -20,6 +20,9 @@ type Resampler struct {
 	r       *C.mss_resampler
 	inRate  int
 	outRate int
+	// The frame counts passed to C by pointer live here, so a Process call
+	// doesn't move two locals to the heap.
+	cin, cout C.uint32_t
 }
 
 func NewResampler(inRate, outRate, quality int) (*Resampler, error) {
@@ -44,12 +47,11 @@ func (r *Resampler) Process(in []float32, out []float32) []float32 {
 			out = grown
 		}
 		dst := out[len(out):cap(out)]
-		cin := C.uint32_t(inFrames)
-		cout := C.uint32_t(len(dst) / 2)
-		C.mss_resampler_process(r.r, (*C.float)(unsafe.Pointer(&in[0])), &cin, (*C.float)(unsafe.Pointer(&dst[0])), &cout)
-		out = out[:len(out)+int(cout)*2]
-		in = in[int(cin)*2:]
-		if cin == 0 && cout == 0 {
+		r.cin, r.cout = C.uint32_t(inFrames), C.uint32_t(len(dst)/2)
+		C.mss_resampler_process(r.r, (*C.float)(unsafe.Pointer(&in[0])), &r.cin, (*C.float)(unsafe.Pointer(&dst[0])), &r.cout)
+		out = out[:len(out)+int(r.cout)*2]
+		in = in[int(r.cin)*2:]
+		if r.cin == 0 && r.cout == 0 {
 			break
 		}
 	}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/audio`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/audio/device.c internal/audio/device_test.go internal/audio/engine.go internal/audio/engine_test.go internal/audio/fakes_test.go internal/audio/resampler.go
git commit -m "audio: device calls after close are no-ops; the tail plays while the next track opens; decoding doesn't allocate" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Engine shutdown: nothing left open, events closed

**Files:**
- Modify: `internal/audio/engine.go`, `internal/player/player.go`
- Test: `internal/audio/engine_test.go`, `internal/audio/fakes_test.go`, `internal/player/player_test.go`, `internal/player/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - Commands are `command{run, src}`. `send(f, src)` closes `src` when the engine is closed. `sendMu` orders sends against `Close`.
  - `Close`:
    - it waits for openers (`openers` WaitGroup), closes the voices they deliver late, and closes the sources of commands that never ran;
    - an opener finishing after the run loop stopped closes its voice.
  - `Events()` is closed when the engine closes.
  - The player reads the event channel it got at `Run` start and stops listening when it closes.
  - Fakes:
    - `fakeSource` gains `openHold` and `decClosed`.
    - The player's fake engine counts `Events()` calls.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t6-test.patch` and apply it from the repository root with `git apply /tmp/t6-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index 4c4f007..edf75ed 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -840,3 +840,73 @@ func TestEmitDoesNotAllocate(t *testing.T) {
 		}
 	}
 }
+
+// Close closes the sources of commands the engine never ran, and of tracks
+// handed to it after it closed.
+func TestEngineCloseClosesSourcesItNeverRan(t *testing.T) {
+	out := newFakeOutput(100000)
+	e := newTestEngine(out)
+	a := newFakeSource(ramp(1000, 0), OutputRate)
+	a.block = make(chan struct{}) // the run goroutine sits in a read
+	e.Play(Track{ID: 1, Source: a})
+	waitFor(t, "a being read", func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.busy != nil })
+	var queued []*fakeSource
+	for i := range 20 {
+		s := newFakeSource(ramp(10, 0), OutputRate)
+		queued = append(queued, s)
+		e.QueueNext(Track{ID: uint64(10 + i), Source: s})
+	}
+	e.Close()
+	late := newFakeSource(ramp(10, 0), OutputRate)
+	e.Play(Track{ID: 99, Source: late})
+	for i, s := range append(queued, a, late) {
+		if !s.isClosed() {
+			t.Errorf("source %d left open", i)
+		}
+	}
+}
+
+// A successor that finishes opening after Close has its decoder and source
+// closed too; its goroutine doesn't wait forever to hand it over.
+func TestEngineOpenerFinishingAfterCloseIsClosed(t *testing.T) {
+	out := newFakeOutput(100000)
+	e := newTestEngine(out)
+	hold := make(chan struct{})
+	var srcs []*fakeSource
+	for i := range 6 { // more than the result buffer holds
+		s := newFakeSource(ramp(10, 0), OutputRate)
+		s.openHold = hold
+		s.openStarted = make(chan struct{})
+		srcs = append(srcs, s)
+		e.QueueNext(Track{ID: uint64(1 + i), Source: s})
+	}
+	for _, s := range srcs {
+		<-s.openStarted // every open is under way, stuck until hold
+	}
+	go func() { time.Sleep(50 * time.Millisecond); close(hold) }()
+	e.Close()
+	waitFor(t, "every late decoder closed", func() bool {
+		for _, s := range srcs {
+			if s.decClosed.Load() != 1 {
+				return false
+			}
+		}
+		return true
+	})
+}
+
+func TestEngineEventsCloseAfterClose(t *testing.T) {
+	e := newTestEngine(newFakeOutput(1000))
+	e.Close()
+	done := make(chan struct{})
+	go func() {
+		for range e.Events() {
+		}
+		close(done)
+	}()
+	select {
+	case <-done:
+	case <-time.After(time.Second):
+		t.Fatal("Events() stayed open after Close")
+	}
+}
diff --git a/internal/audio/fakes_test.go b/internal/audio/fakes_test.go
index 9d749ea..19424a7 100644
--- a/internal/audio/fakes_test.go
+++ b/internal/audio/fakes_test.go
@@ -4,6 +4,7 @@ import (
 	"errors"
 	"io"
 	"sync"
+	"sync/atomic"
 	"testing"
 	"time"
 )
@@ -70,6 +71,8 @@ type fakeSource struct {
 	openStartedOnce sync.Once     // protects closing openStarted
 	seekBlock       chan struct{} // blocks SeekFrame if set, until closed
 	eofWithData     bool          // the last Read returns its frames together with io.EOF
+	openHold        chan struct{} // fakeOpen waits for it even after the source is closed
+	decClosed       atomic.Int32  // decoders of this source closed
 	once            sync.Once
 	closed          chan struct{}
 }
@@ -110,6 +113,9 @@ func fakeOpen(src io.ReadSeeker, _ Format) (Decoder, error) {
 			return nil, errClosed
 		}
 	}
+	if s.openHold != nil {
+		<-s.openHold // a slow open that notices nothing until it finishes
+	}
 	return &fakeDecoder{src: s}, nil
 }
 
@@ -117,7 +123,7 @@ var errClosed = errors.New("source closed")
 
 func (d *fakeDecoder) SampleRate() int      { return d.src.rate }
 func (d *fakeDecoder) LengthFrames() uint64 { return uint64(len(d.src.pcm) / 2) }
-func (d *fakeDecoder) Close() error         { return nil }
+func (d *fakeDecoder) Close() error         { d.src.decClosed.Add(1); return nil }
 func (d *fakeDecoder) SeekFrame(f uint64) error {
 	if d.src.seekBlock != nil {
 		select {
diff --git a/internal/player/fakes_test.go b/internal/player/fakes_test.go
index b9b7f50..e110259 100644
--- a/internal/player/fakes_test.go
+++ b/internal/player/fakes_test.go
@@ -6,6 +6,7 @@ import (
 	"io"
 	"math/rand/v2"
 	"sync"
+	"sync/atomic"
 	"testing"
 	"time"
 
@@ -33,6 +34,7 @@ type fakeEngine struct {
 	pos       time.Duration
 	posOK     bool
 	events    chan audio.Event
+	eventReqs atomic.Int32 // Events() calls
 }
 
 func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan audio.Event, 64)} }
@@ -53,7 +55,7 @@ func (e *fakeEngine) Stop()                      { e.mu.Lock(); e.stops++; e.pos
 func (e *fakeEngine) SetPaused(p bool)           { e.mu.Lock(); e.paused = p; e.mu.Unlock() }
 func (e *fakeEngine) SetVolume(v float32)        { e.mu.Lock(); e.volume = v; e.mu.Unlock() }
 func (e *fakeEngine) getVolume() float32         { e.mu.Lock(); defer e.mu.Unlock(); return e.volume }
-func (e *fakeEngine) Events() <-chan audio.Event { return e.events }
+func (e *fakeEngine) Events() <-chan audio.Event { e.eventReqs.Add(1); return e.events }
 
 // Seek records the call and returns at once, like audio.Engine. The seek
 // completes (position moves, or EventSeekFailed if seekErr is set) right
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index 8702d47..6b93874 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -711,3 +711,17 @@ func TestMuteKeepsTheVolume(t *testing.T) {
 		t.Fatalf("unmuted gain %v, want the -10 dB volume", v)
 	}
 }
+
+// When the engine closes its event stream, the player stops reading it
+// rather than spinning on the closed channel, and still takes commands.
+func TestPlayerStopsReadingAClosedEngineStream(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.do(func() {})
+	close(h.eng.events)
+	time.Sleep(50 * time.Millisecond)
+	h.p.PlayNow(songs(1, 300), 0)
+	h.waitFor("engine.Play", func() bool { return h.eng.playCount() == 1 })
+	if n := h.eng.eventReqs.Load(); n > 2 {
+		t.Fatalf("Events() read %d times: the player spun on the closed stream", n)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/audio ./internal/player`

Expected: FAIL, e.g.:

```
--- FAIL: TestEngineCloseClosesSourcesItNeverRan (0.00s)
--- FAIL: TestEngineOpenerFinishingAfterCloseIsClosed (3.00s)
--- FAIL: TestEngineEventsCloseAfterClose (1.00s)
--- FAIL: TestPlayerStopsReadingAClosedEngineStream (0.05s)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index 656debe..e3a23bb 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -83,24 +83,34 @@ type openResult struct {
 	t   Track
 }
 
+// command is one call for the run goroutine. src is the track source it
+// hands over: if the engine closes before running it, Close closes src.
+type command struct {
+	run func()
+	src io.ReadSeeker
+}
+
 // Engine decodes tracks into an Output. Decoder state is owned by the run
 // goroutine and public methods post commands to it. A separate monitor
 // goroutine turns device progress into Started/Ended events, so events keep
 // flowing even while a decode is blocked on a slow network read.
 type Engine struct {
-	o      EngineOptions
-	cmds   chan func()
-	events chan Event
-	openCh chan openResult
-	quit   chan struct{}
-	done   chan struct{}
+	o       EngineOptions
+	cmds    chan command
+	events  chan Event // closed once the engine has closed
+	openCh  chan openResult
+	quit    chan struct{}
+	done    chan struct{}
+	openers sync.WaitGroup // queued tracks still opening
+
+	sendMu sync.Mutex // orders commands against Close
+	closed bool       // guarded by sendMu
 
 	mu      sync.Mutex
 	segs    []segment // guarded by mu
 	evq     []Event   // guarded by mu
 	busy    io.Closer // guarded by mu: source of the voice being decoded
 	killed  io.Closer // guarded by mu: source closed by interrupt, reason for failed open
-	closed  bool      // guarded by mu
 	seekReq *seekReq  // guarded by mu: latest requested seek, not yet run
 
 	// Owned by the run goroutine.
@@ -138,7 +148,7 @@ func NewEngine(o EngineOptions) *Engine {
 	}
 	e := &Engine{
 		o:       o,
-		cmds:    make(chan func(), 64),
+		cmds:    make(chan command, 64),
 		events:  make(chan Event, 256),
 		openCh:  make(chan openResult, 4),
 		quit:    make(chan struct{}),
@@ -156,7 +166,7 @@ func (e *Engine) Events() <-chan Event { return e.events }
 // Play stops whatever is playing and starts t immediately.
 func (e *Engine) Play(t Track) {
 	e.dropSeekReq()
-	e.send(func() { e.doPlay(t) })
+	e.send(func() { e.doPlay(t) }, t.Source)
 	e.interrupt(t.Source)
 }
 
@@ -165,15 +175,15 @@ func (e *Engine) Play(t Track) {
 // already finished decoding (or finished playing), the queued track starts
 // as soon as it is open. A track still opening 10 s after it is needed is
 // dropped with EventError (ErrOpenTimeout) and the queue ends.
-func (e *Engine) QueueNext(t Track) { e.send(func() { e.doQueueNext(t) }) }
+func (e *Engine) QueueNext(t Track) { e.send(func() { e.doQueueNext(t) }, t.Source) }
 
 // ClearNext drops the queued track.
-func (e *Engine) ClearNext() { e.send(e.cancelNext) }
+func (e *Engine) ClearNext() { e.send(e.cancelNext, nil) }
 
 // Stop halts playback and discards everything buffered.
 func (e *Engine) Stop() {
 	e.dropSeekReq()
-	e.send(e.doStop)
+	e.send(e.doStop, nil)
 	e.interrupt(nil)
 }
 
@@ -219,7 +229,7 @@ func (e *Engine) Seek(id uint64, pos time.Duration) {
 		if err := e.doSeek(target.id, target.pos); err != nil {
 			e.queueEvent(Event{Kind: EventSeekFailed, TrackID: target.id, Err: err})
 		}
-	})
+	}, nil)
 }
 
 func (e *Engine) SetPaused(p bool)    { e.o.Output.SetPaused(p) }
@@ -242,31 +252,49 @@ func (e *Engine) Position() (id uint64, pos time.Duration, ok bool) {
 	return 0, 0, false
 }
 
-// Close stops the engine goroutine. The Output is not closed.
+// Close stops the engine and closes every track source it still holds,
+// including those of commands it never got to and of successors that
+// finished opening too late. Events() is closed afterwards. The Output is
+// not closed.
 func (e *Engine) Close() {
-	e.mu.Lock()
+	e.sendMu.Lock()
 	if e.closed {
-		e.mu.Unlock()
+		e.sendMu.Unlock()
 		return
 	}
 	e.closed = true
-	e.mu.Unlock()
+	e.sendMu.Unlock()
 	e.interrupt(nil)
 	close(e.quit)
 	<-e.done
+	go func() { e.openers.Wait(); close(e.openCh) }()
+	for r := range e.openCh {
+		closeVoice(r.v)
+	}
+	for {
+		select {
+		case c := <-e.cmds:
+			closeSource(c.src)
+		default:
+			return
+		}
+	}
 }
 
-func (e *Engine) send(f func()) bool {
-	e.mu.Lock()
-	closed := e.closed
-	e.mu.Unlock()
-	if closed {
+// send queues f for the run goroutine. src is the track source f takes over;
+// if the engine is closed, src is closed instead.
+func (e *Engine) send(f func(), src io.ReadSeeker) bool {
+	e.sendMu.Lock()
+	defer e.sendMu.Unlock()
+	if e.closed {
+		closeSource(src)
 		return false
 	}
 	select {
-	case e.cmds <- f:
+	case e.cmds <- command{run: f, src: src}:
 		return true
 	case <-e.done:
+		closeSource(src)
 		return false
 	}
 }
@@ -327,8 +355,8 @@ func (e *Engine) run() {
 func (e *Engine) poll() bool {
 	for {
 		select {
-		case f := <-e.cmds:
-			f()
+		case c := <-e.cmds:
+			c.run()
 		case r := <-e.openCh:
 			e.onOpened(r)
 		case <-e.quit:
@@ -343,8 +371,8 @@ func (e *Engine) wait(d time.Duration) bool {
 	t := time.NewTimer(d)
 	defer t.Stop()
 	select {
-	case f := <-e.cmds:
-		f()
+	case c := <-e.cmds:
+		c.run()
 	case r := <-e.openCh:
 		e.onOpened(r)
 	case <-e.quit:
@@ -361,6 +389,7 @@ func (e *Engine) queueEvent(ev Event) {
 }
 
 func (e *Engine) monitor() {
+	defer close(e.events)
 	t := time.NewTicker(e.o.Poll)
 	defer t.Stop()
 	for {
@@ -613,9 +642,15 @@ func (e *Engine) doQueueNext(t Track) {
 	e.opening = &t
 	e.openedAt = time.Now()
 	gen := e.gen
+	e.openers.Add(1)
 	go func() {
+		defer e.openers.Done()
 		v, err := e.openVoice(t)
-		e.openCh <- openResult{gen: gen, v: v, err: err, t: t}
+		select {
+		case e.openCh <- openResult{gen: gen, v: v, err: err, t: t}:
+		case <-e.done:
+			closeVoice(v) // the engine stopped: nobody else will
+		}
 	}()
 }
 
diff --git a/internal/player/player.go b/internal/player/player.go
index 9705c52..33e7094 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -221,6 +221,7 @@ func (p *Player) Run(ctx context.Context) {
 	}
 	p.applyVolume()
 	go p.scrobbles.Flush(ctx, p.o.API)
+	events := p.o.Engine.Events()
 	for {
 		select {
 		case <-ctx.Done():
@@ -231,7 +232,11 @@ func (p *Player) Run(ctx context.Context) {
 			f()
 		case r := <-p.opens:
 			p.onOpened(r)
-		case ev := <-p.o.Engine.Events():
+		case ev, ok := <-events:
+			if !ok {
+				events = nil // the engine closed; stop listening instead of spinning
+				continue
+			}
 			p.onEngine(ev)
 		case <-tick:
 			p.onTick()
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/audio ./internal/player`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/audio/engine.go internal/audio/engine_test.go internal/audio/fakes_test.go internal/player/fakes_test.go internal/player/player.go internal/player/player_test.go
git commit -m "audio: Close closes every source it holds and Events(); player: stop reading a closed event stream" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: ReplayGain applies at once

**Files:**
- Modify: `internal/audio/engine.go`, `internal/player/player.go`
- Test: `internal/audio/engine_test.go`, `internal/player/settings_test.go`, `internal/player/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `(*audio.Engine).SetGain(id uint64, gain float32)` applies to the track being decoded, the queued successor, or a successor still opening. The opening case is applied in `onOpened`, and `doQueueNext` keeps its own copy of the track for this.
  - `player.Engine` gains `SetGain`.
  - `(*Player).SetReplayGain` sets the new gain on the current and queued tracks.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t7-test.patch` and apply it from the repository root with `git apply /tmp/t7-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index edf75ed..bf70988 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -910,3 +910,50 @@ func TestEngineEventsCloseAfterClose(t *testing.T) {
 		t.Fatal("Events() stayed open after Close")
 	}
 }
+
+// SetGain changes the gain of the track being played from the next chunk on.
+func TestEngineSetGainAppliesNow(t *testing.T) {
+	out := newFakeOutput(512) // a small ring: most of the track is still to decode
+	e := newTestEngine(out)
+	defer e.Close()
+	pcm := ramp(5000, 1)
+	e.Play(Track{ID: 1, Source: newFakeSource(pcm, OutputRate)})
+	expectEvent(t, e, EventStarted, 1)
+	e.SetGain(1, 0.5)
+	playOut(t, out, 5000)
+	got := out.written()
+	if got[0] != pcm[0] {
+		t.Fatalf("first sample %v, want the unity-gain %v", got[0], pcm[0])
+	}
+	if last := len(pcm) - 2; got[last] != pcm[last]*0.5 {
+		t.Fatalf("last sample %v, want %v at the new gain", got[last], pcm[last]*0.5)
+	}
+}
+
+// SetGain also reaches a successor that is queued or still opening.
+func TestEngineSetGainReachesTheSuccessor(t *testing.T) {
+	for _, stillOpening := range []bool{false, true} {
+		out := newFakeOutput(100000)
+		e := newTestEngine(out)
+		b := newFakeSource(ramp(100, 1), OutputRate)
+		if stillOpening {
+			b.openBlock = make(chan struct{})
+			b.openStarted = make(chan struct{})
+		}
+		e.Play(Track{ID: 1, Source: newFakeSource(ramp(100, 1), OutputRate)})
+		e.QueueNext(Track{ID: 2, Source: b})
+		if stillOpening {
+			<-b.openStarted
+		}
+		e.SetGain(2, 0.25)
+		if stillOpening {
+			close(b.openBlock)
+		}
+		playOut(t, out, 200)
+		got := out.written()
+		if want := ramp(100, 1)[99*2] * 0.25; got[199*2] != want {
+			t.Errorf("opening %v: successor's last sample %v, want %v", stillOpening, got[199*2], want)
+		}
+		e.Close()
+	}
+}
diff --git a/internal/player/fakes_test.go b/internal/player/fakes_test.go
index e110259..6e7e579 100644
--- a/internal/player/fakes_test.go
+++ b/internal/player/fakes_test.go
@@ -35,6 +35,7 @@ type fakeEngine struct {
 	posOK     bool
 	events    chan audio.Event
 	eventReqs atomic.Int32 // Events() calls
+	gains     map[uint64]float32
 }
 
 func newFakeEngine() *fakeEngine { return &fakeEngine{events: make(chan audio.Event, 64)} }
@@ -50,10 +51,24 @@ func (e *fakeEngine) QueueNext(t audio.Track) {
 	e.queued = append(e.queued, t)
 	e.mu.Unlock()
 }
-func (e *fakeEngine) ClearNext()                 { e.mu.Lock(); e.clears++; e.mu.Unlock() }
-func (e *fakeEngine) Stop()                      { e.mu.Lock(); e.stops++; e.posOK = false; e.mu.Unlock() }
-func (e *fakeEngine) SetPaused(p bool)           { e.mu.Lock(); e.paused = p; e.mu.Unlock() }
-func (e *fakeEngine) SetVolume(v float32)        { e.mu.Lock(); e.volume = v; e.mu.Unlock() }
+func (e *fakeEngine) ClearNext()          { e.mu.Lock(); e.clears++; e.mu.Unlock() }
+func (e *fakeEngine) Stop()               { e.mu.Lock(); e.stops++; e.posOK = false; e.mu.Unlock() }
+func (e *fakeEngine) SetPaused(p bool)    { e.mu.Lock(); e.paused = p; e.mu.Unlock() }
+func (e *fakeEngine) SetVolume(v float32) { e.mu.Lock(); e.volume = v; e.mu.Unlock() }
+func (e *fakeEngine) SetGain(id uint64, g float32) {
+	e.mu.Lock()
+	if e.gains == nil {
+		e.gains = map[uint64]float32{}
+	}
+	e.gains[id] = g
+	e.mu.Unlock()
+}
+func (e *fakeEngine) gainOf(id uint64) (float32, bool) {
+	e.mu.Lock()
+	defer e.mu.Unlock()
+	g, ok := e.gains[id]
+	return g, ok
+}
 func (e *fakeEngine) getVolume() float32         { e.mu.Lock(); defer e.mu.Unlock(); return e.volume }
 func (e *fakeEngine) Events() <-chan audio.Event { e.eventReqs.Add(1); return e.events }
 
diff --git a/internal/player/settings_test.go b/internal/player/settings_test.go
index e356d75..0654f21 100644
--- a/internal/player/settings_test.go
+++ b/internal/player/settings_test.go
@@ -41,3 +41,27 @@ func TestSetScrobbleOff(t *testing.T) {
 		t.Fatalf("%d scrobble calls with scrobbling off", n)
 	}
 }
+
+// A ReplayGain change reaches the playing track and the queued successor
+// at once, not only the tracks opened after it.
+func TestSetReplayGainAppliesNow(t *testing.T) {
+	h := newHarness(t, nil) // ReplayGain off
+	q := songs(2, 100)
+	g, g2 := -6.0, -12.0
+	q[0].ReplayGain = &subsonic.ReplayGain{TrackGain: &g}
+	q[1].ReplayGain = &subsonic.ReplayGain{TrackGain: &g2}
+	h.p.PlayNow(q, 0)
+	a := h.playAndStart(1)
+	h.tickAt(a.ID, 90*time.Second) // near the end: the next song is prefetched
+	h.waitFor("the successor queued", func() bool { return h.eng.queueCount() == 1 })
+	h.p.SetReplayGain("track")
+	if got, ok := h.eng.gainOf(a.ID); !ok || got < 0.50 || got > 0.51 {
+		t.Fatalf("playing track's gain %v (set %v), want -6 dB", got, ok)
+	}
+	h.eng.mu.Lock()
+	next := h.eng.queued[0].ID
+	h.eng.mu.Unlock()
+	if got, ok := h.eng.gainOf(next); !ok || got < 0.25 || got > 0.26 {
+		t.Fatalf("queued track's gain %v (set %v), want -12 dB", got, ok)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/audio ./internal/player`

Expected: FAIL, e.g.:

```
e.SetGain undefined (type *Engine has no field or method SetGain)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index e3a23bb..d74ec45 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -232,6 +232,25 @@ func (e *Engine) Seek(id uint64, pos time.Duration) {
 	}, nil)
 }
 
+// SetGain changes a track's gain (ReplayGain) at once: the track being
+// decoded, the queued successor, or one still opening. What is already
+// buffered (about half a second) plays at the old gain. 0 means unity.
+func (e *Engine) SetGain(id uint64, gain float32) {
+	if gain == 0 {
+		gain = 1
+	}
+	e.send(func() {
+		for _, v := range []*voice{e.cur, e.next} {
+			if v != nil && v.t.ID == id {
+				v.gain = gain
+			}
+		}
+		if e.opening != nil && e.opening.ID == id {
+			e.opening.Gain = gain // applied when it opens
+		}
+	}, nil)
+}
+
 func (e *Engine) SetPaused(p bool)    { e.o.Output.SetPaused(p) }
 func (e *Engine) SetVolume(v float32) { e.o.Output.SetVolume(v) }
 
@@ -639,7 +658,8 @@ func (e *Engine) doStop() {
 func (e *Engine) doQueueNext(t Track) {
 	e.cancelNext()
 	e.gen++
-	e.opening = &t
+	op := t // SetGain may change op; the opener goroutine reads t
+	e.opening = &op
 	e.openedAt = time.Now()
 	gen := e.gen
 	e.openers.Add(1)
@@ -678,11 +698,15 @@ func (e *Engine) onOpened(r openResult) {
 		closeVoice(r.v)
 		return
 	}
+	gain := e.opening.Gain
 	e.opening = nil
 	if r.err != nil {
 		e.queueEvent(Event{Kind: EventError, TrackID: r.t.ID, Err: r.err})
 		return
 	}
+	if gain != 0 {
+		r.v.gain = gain // a SetGain that came while it opened
+	}
 	e.next = r.v
 	if e.cur == nil && e.ended {
 		// The current track finished decoding before its successor was
diff --git a/internal/player/player.go b/internal/player/player.go
index 33e7094..958bb0b 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -26,6 +26,8 @@ type Engine interface {
 	Seek(id uint64, pos time.Duration)
 	SetPaused(bool)
 	SetVolume(float32)
+	// SetGain changes a playing or queued track's gain at once.
+	SetGain(id uint64, gain float32)
 	Position() (uint64, time.Duration, bool)
 	Events() <-chan audio.Event
 }
@@ -531,10 +533,19 @@ func (p *Player) applyVolume() {
 	p.o.Engine.SetVolume(g)
 }
 
-// SetReplayGain sets the ReplayGain mode (off, track or album) for the
-// tracks opened from now on.
+// SetReplayGain sets the ReplayGain mode (off, track or album). It applies
+// at once: to the playing track, the queued successor, and every track
+// opened from now on.
 func (p *Player) SetReplayGain(mode string) {
-	p.do(func() { p.o.ReplayGain = mode })
+	p.do(func() {
+		p.o.ReplayGain = mode
+		if song, ok := p.currentSong(); ok && p.curID != 0 {
+			p.o.Engine.SetGain(p.curID, ReplayGainFactor(song, mode))
+		}
+		if p.nextID != 0 {
+			p.o.Engine.SetGain(p.nextID, ReplayGainFactor(p.queue[p.order[p.nextCursor]], mode))
+		}
+	})
 }
 
 // SetScrobble turns now-playing and scrobble reports on or off.
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/audio ./internal/player`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/audio/engine.go internal/audio/engine_test.go internal/player/fakes_test.go internal/player/player.go internal/player/settings_test.go
git commit -m "audio, player: a ReplayGain change applies to the playing and queued tracks at once" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Raw MP3 seek by byte estimate; the mock honours timeOffset

**Files:**
- Modify: `internal/player/stream.go`, `internal/player/player.go`, `tools/mocksubsonic/main.go`
- Test: `internal/player/opener_test.go`, `internal/player/player_test.go`, `internal/player/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `mp3Offset(r, song, offset) (int64, bool)`: the ID3v2 tag size, plus the rest of the file in proportion to time.
  - `fromOffset` is a view of the stream from `base` on. It passes `Promote` through.
  - `NewOpener` starts a raw MP3 of known size and duration at the estimate. `Opened.Offset` is then the target, and the stream is back at 0 when there is no estimate.
  - Player:
    - `openAsync` asks the engine to seek only when `!Transcoded && Offset == 0`.
    - `seeksByReopening(opened, song)` sends transcoded streams and sized MP3s through `reopenAt`.
  - Mock server: `timeOffset` skips its share of an MP3 transcode.

- [ ] **Step 1: Write the failing tests**

Save this patch as `/tmp/t8-test.patch` and apply it from the repository root with `git apply /tmp/t8-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/player/fakes_test.go b/internal/player/fakes_test.go
index 6e7e579..1a399aa 100644
--- a/internal/player/fakes_test.go
+++ b/internal/player/fakes_test.go
@@ -214,8 +214,8 @@ func (o *fakeOpener) open(_ context.Context, s subsonic.Song, offset time.Durati
 	}
 	_, f, transcoded := PlanStream(s, StreamSettings{TranscodeFormat: "mp3", TranscodeBitrate: 320})
 	op := Opened{Source: &fakeSource{song: s.ID}, Format: f, Transcoded: transcoded}
-	if transcoded {
-		op.Offset = offset
+	if transcoded || (f == audio.FormatMP3 && s.Size > 0 && s.Duration > 0) {
+		op.Offset = offset // as NewOpener starts these at offset
 	}
 	return op, nil
 }
diff --git a/internal/player/opener_test.go b/internal/player/opener_test.go
index bf3400f..8bb055e 100644
--- a/internal/player/opener_test.go
+++ b/internal/player/opener_test.go
@@ -1,13 +1,18 @@
 package player
 
 import (
+	"bytes"
 	"context"
 	"errors"
+	"io"
 	"net/http"
 	"net/http/httptest"
+	"os"
 	"strings"
 	"testing"
+	"time"
 
+	"mistersubsonic/internal/audio"
 	"mistersubsonic/internal/subsonic"
 )
 
@@ -34,3 +39,85 @@ func TestOpenerRejectsErrorDocument(t *testing.T) {
 		t.Fatalf("err = %v, want the server's APIError", err)
 	}
 }
+
+// An MP3 opened at an offset starts near it (after the ID3v2 tag, in
+// proportion to time), and the decoder plays from there.
+func TestOpenerStartsAnMP3NearTheOffset(t *testing.T) {
+	mp3, err := os.ReadFile("../audio/testdata/tone-44k16.mp3") // 44-byte ID3v2 tag
+	if err != nil {
+		t.Fatal(err)
+	}
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
+	}))
+	defer srv.Close()
+	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
+	open := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})
+	song := subsonic.Song{ID: "m", Suffix: "mp3", Size: int64(len(mp3)), Duration: 2}
+	op, err := open(context.Background(), song, time.Second, false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer closeOpened(op)
+	if op.Offset != time.Second {
+		t.Fatalf("Offset %v, want 1s: the stream starts there", op.Offset)
+	}
+	base := 44 + (len(mp3)-44)/2
+	head := make([]byte, 16)
+	if _, err := io.ReadFull(op.Source, head); err != nil || !bytes.Equal(head, mp3[base:base+16]) {
+		t.Fatalf("stream starts with %x, want the bytes at %d (%v)", head, base, err)
+	}
+	op.Source.Seek(0, io.SeekStart)
+	dec, err := audio.OpenDecoder(op.Source, audio.FormatMP3)
+	if err != nil {
+		t.Fatal("the decoder can't start mid-file:", err)
+	}
+	defer dec.Close()
+	if n, err := dec.Read(make([]float32, 2048)); n == 0 {
+		t.Fatalf("decoded nothing from the middle: %v", err)
+	}
+}
+
+// Without the size or length there's nothing to estimate from: the MP3
+// opens at the start and the engine seeks.
+func TestOpenerOpensAnMP3OfUnknownSizeAtTheStart(t *testing.T) {
+	mp3, _ := os.ReadFile("../audio/testdata/tone-44k16.mp3")
+	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(mp3))
+	}))
+	defer srv.Close()
+	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "a", Password: "b"}})
+	op, err := NewOpener(c, StreamSettings{TranscodeFormat: "mp3"})(context.Background(), subsonic.Song{ID: "m", Suffix: "mp3"}, time.Second, false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer closeOpened(op)
+	head := make([]byte, 3)
+	io.ReadFull(op.Source, head)
+	if op.Offset != 0 || string(head) != "ID3" {
+		t.Fatalf("Offset %v, starts with %q", op.Offset, head)
+	}
+}
+
+func TestMP3Offset(t *testing.T) {
+	tagged := append([]byte("ID3\x04\x00\x00\x00\x00\x01\x00"), make([]byte, 128)...) // a 128-byte tag
+	for name, c := range map[string]struct {
+		data []byte
+		song subsonic.Song
+		at   time.Duration
+		want int64
+		ok   bool
+	}{
+		"no tag":        {make([]byte, 16), subsonic.Song{Size: 1000, Duration: 100}, 50 * time.Second, 500, true},
+		"tag":           {tagged, subsonic.Song{Size: 1138, Duration: 100}, 50 * time.Second, 138 + 500, true},
+		"at the end":    {make([]byte, 16), subsonic.Song{Size: 1000, Duration: 100}, 200 * time.Second, 999, true},
+		"no size":       {make([]byte, 16), subsonic.Song{Duration: 100}, time.Second, 0, false},
+		"no duration":   {make([]byte, 16), subsonic.Song{Size: 1000}, time.Second, 0, false},
+		"tag past size": {tagged, subsonic.Song{Size: 100, Duration: 100}, time.Second, 0, false},
+	} {
+		got, ok := mp3Offset(bytes.NewReader(c.data), c.song, c.at)
+		if got != c.want || ok != c.ok {
+			t.Errorf("%s: %d,%v; want %d,%v", name, got, ok, c.want, c.ok)
+		}
+	}
+}
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index 6b93874..99d373d 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -725,3 +725,29 @@ func TestPlayerStopsReadingAClosedEngineStream(t *testing.T) {
 		t.Fatalf("Events() read %d times: the player spun on the closed stream", n)
 	}
 }
+
+// An MP3 of known size and length seeks by reopening the stream near the
+// target instead of asking the decoder, which would decode from the start.
+func TestSeekingAnMP3ReopensNearTheTarget(t *testing.T) {
+	h := newHarness(t, nil)
+	q := songs(2, 300)
+	q[0].Suffix, q[0].Size = "mp3", 9_600_000
+	q[1].Suffix = "mp3" // no size: the decoder seeks
+	h.p.PlayNow(q, 0)
+	h.playAndStart(1)
+	h.p.Seek(100 * time.Second)
+	h.waitFor("reopen", func() bool { return h.eng.playCount() == 2 })
+	calls := h.opener.callList()
+	if last := calls[len(calls)-1]; last.id != "sa" || last.offset != 100*time.Second {
+		t.Fatalf("reopen call = %+v", last)
+	}
+	if tr := h.eng.lastPlayed(); tr.Offset != 100*time.Second || len(h.eng.seekList()) != 0 {
+		t.Fatalf("reopened at %v with engine seeks %v; want the stream started there and no seek", tr.Offset, h.eng.seekList())
+	}
+	h.p.Next()
+	b := h.playAndStart(3)
+	h.p.Seek(42 * time.Second)
+	if seeks := h.eng.seekList(); len(seeks) != 1 || seeks[0] != (seekCall{b.ID, 42 * time.Second}) {
+		t.Fatalf("an MP3 without a size: seeks = %v, want the engine's", seeks)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/player`

Expected: FAIL, e.g.:

```
undefined: mp3Offset
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/player/player.go b/internal/player/player.go
index 958bb0b..8b45d3a 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -479,7 +479,7 @@ func (p *Player) Seek(pos time.Duration) {
 			p.openSeek, p.hasOpenSeek = pos, true
 			return
 		}
-		if p.curSrc.Transcoded {
+		if seeksByReopening(p.curSrc, song) {
 			p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
 			return
 		}
@@ -751,8 +751,8 @@ func (p *Player) openAsync(id uint64, song subsonic.Song, offset time.Duration,
 		defer cancel()
 		op, err := p.o.Open(octx, song, offset, next)
 		r := openResult{id: id, song: song, opened: op, err: err, next: next}
-		if err == nil && !op.Transcoded {
-			r.seek = offset
+		if err == nil && !op.Transcoded && op.Offset == 0 {
+			r.seek = offset // the opener didn't start the stream at offset: the engine seeks
 		}
 		select {
 		case p.opens <- r:
@@ -762,6 +762,13 @@ func (p *Player) openAsync(id uint64, song subsonic.Song, offset time.Duration,
 	}()
 }
 
+// seeksByReopening: transcoded streams (timeOffset) and MP3s of known size
+// and length (a byte estimate, see mp3Offset) are sought by opening the
+// stream again at the target; everything else by the engine.
+func seeksByReopening(o Opened, s subsonic.Song) bool {
+	return o.Transcoded || (o.Format == audio.FormatMP3 && s.Size > 0 && s.Duration > 0)
+}
+
 func closeOpened(o Opened) {
 	if c, ok := o.Source.(interface{ Close() error }); ok {
 		c.Close()
diff --git a/internal/player/stream.go b/internal/player/stream.go
index 0023bc1..97eab6a 100644
--- a/internal/player/stream.go
+++ b/internal/player/stream.go
@@ -80,8 +80,64 @@ func NewOpener(c *subsonic.Client, st StreamSettings) Opener {
 		if err != nil {
 			return Opened{}, err
 		}
-		return Opened{Source: r, Format: f, Transcoded: transcoded, Offset: start}, nil
+		var src io.ReadSeeker = r
+		if !transcoded && offset > 0 && f == audio.FormatMP3 {
+			// An MP3 without a seek table seeks by decoding from the start,
+			// which takes seconds on the MiSTer: start near the target instead.
+			if base, ok := mp3Offset(r, s, offset); ok {
+				if _, err := r.Seek(base, io.SeekStart); err == nil {
+					src, start = &fromOffset{r: r, base: base}, offset
+				}
+			} else {
+				r.Seek(0, io.SeekStart)
+			}
+		}
+		return Opened{Source: src, Format: f, Transcoded: transcoded, Offset: start}, nil
+	}
+}
+
+// mp3Offset estimates where offset falls in the MP3 r of s.Size bytes lasting
+// s.Duration: after the ID3v2 tag, in proportion to time. That is exact for
+// a constant bitrate and close for a variable one. It reads the first bytes
+// of r to find the tag.
+func mp3Offset(r io.Reader, s subsonic.Song, offset time.Duration) (int64, bool) {
+	d := time.Duration(s.Duration) * time.Second
+	if s.Size <= 0 || d <= 0 {
+		return 0, false
+	}
+	var h [10]byte
+	tag := int64(0)
+	if _, err := io.ReadFull(r, h[:]); err == nil && string(h[:3]) == "ID3" {
+		size := int64(h[6]&0x7F)<<21 | int64(h[7]&0x7F)<<14 | int64(h[8]&0x7F)<<7 | int64(h[9]&0x7F)
+		tag = 10 + size
+		if h[5]&0x10 != 0 {
+			tag += 10 // a footer
+		}
+	}
+	if tag >= s.Size {
+		return 0, false
+	}
+	off := tag + int64(float64(s.Size-tag)*float64(offset)/float64(d))
+	return min(off, s.Size-1), true
+}
+
+// fromOffset shows a stream from byte base on as if it began there, so the
+// decoder starts at the estimated seek point (MP3 frames resync on their own).
+type fromOffset struct {
+	r    *stream.Reader
+	base int64
+}
+
+func (f *fromOffset) Read(p []byte) (int, error) { return f.r.Read(p) }
+func (f *fromOffset) Close() error               { return f.r.Close() }
+func (f *fromOffset) Promote()                   { f.r.Promote() }
+
+func (f *fromOffset) Seek(off int64, whence int) (int64, error) {
+	if whence == io.SeekStart {
+		off += f.base
 	}
+	abs, err := f.r.Seek(off, whence)
+	return abs - f.base, err
 }
 
 // ReplayGainFactor converts the song's ReplayGain to a linear factor for
diff --git a/tools/mocksubsonic/main.go b/tools/mocksubsonic/main.go
index f25b176..b66e217 100644
--- a/tools/mocksubsonic/main.go
+++ b/tools/mocksubsonic/main.go
@@ -7,7 +7,8 @@
 // a format other than "raw" is answered like a Navidrome transcode: HTTP
 // 200, chunked, no Content-Length, no Accept-Ranges, Range ignored. The
 // file's bytes are sent as they are, so a transcoded file must already be
-// in the requested format.
+// in the requested format. timeOffset is honoured for MP3 (the stream
+// starts that share of the file in; frames resync) and ignored otherwise.
 //
 //	go run ./tools/mocksubsonic -dir internal/audio/testdata -addr 127.0.0.1:4533
 package main
@@ -16,6 +17,7 @@ import (
 	"encoding/json"
 	"flag"
 	"fmt"
+	"io"
 	"log"
 	"net/http"
 	"os"
@@ -74,6 +76,9 @@ func main() {
 			}
 			defer f.Close()
 			if format := q.Get("format"); format != "raw" {
+				if t, _ := strconv.Atoi(q.Get("timeOffset")); t > 0 && format == "mp3" && songs[i].Duration > 0 {
+					f.Seek(songs[i].Size*int64(min(t, songs[i].Duration))/int64(songs[i].Duration), io.SeekStart)
+				}
 				serveTranscode(w, f, format)
 				return
 			}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/player`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then run `make e2e`. Expected: `e2e ok: …` and `e2e-ui ok: …`.

- [ ] **Step 5: Commit**

```bash
git add internal/player/fakes_test.go internal/player/opener_test.go internal/player/player.go internal/player/player_test.go internal/player/stream.go tools/mocksubsonic/main.go
git commit -m "player: raw MP3 seeks and resumes start the stream at a byte estimate; mock: timeOffset for MP3" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 9: Docs, and the on-device check (when the MiSTer is available)

**Files:**
- Modify: `docs/superpowers/plan-2a-followups.md`, `docs/spikes.md`, `docs/testing-on-mister.md`

**Interfaces:**
- **Consumes:** everything above.
- **Produces:**
  - "Resolved by Plan 3b" in the follow-ups.
  - Checklist items 16–18 (long MP3, ReplayGain, memory). The Log item is renumbered 19.
  - A pending entry in the spikes log.

- [ ] **Step 1: Record the results and the new device checks**

Save this patch as `/tmp/t9-code.patch` and apply it from the repository root with `git apply /tmp/t9-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 8):

```diff
diff --git a/docs/spikes.md b/docs/spikes.md
index d67ad46..6866fd0 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -51,3 +51,7 @@ pending — MiSTer unavailable. Still to do: run the setup wizard on the TV with
 ## Plan 3a on the MiSTer
 
 pending — MiSTer unavailable. Run the checklist in `docs/testing-on-mister.md` and record the results here: install with `make deploy`, then start from the Scripts menu; check the console and cursor, BGM and SAM, exit and crash recovery, the watchdog, and the log.
+
+## Plan 3b on the MiSTer
+
+pending — MiSTer unavailable. Plan 3b's fixes are verified on the host. On the device, run `docs/testing-on-mister.md` items 6 (long FLAC), 16 (long MP3), 17 (ReplayGain) and 18 (memory), together with spikes 2 and 3 and the `Repaint` benchmarks.
diff --git a/docs/superpowers/plan-2a-followups.md b/docs/superpowers/plan-2a-followups.md
index 024b03b..165ff7c 100644
--- a/docs/superpowers/plan-2a-followups.md
+++ b/docs/superpowers/plan-2a-followups.md
@@ -210,3 +210,36 @@ And also:
   - `check-notices.sh` passes when `go list` fails (no `pipefail`) and checks the host dependency graph, not the ARM one.
 - **Docs:** the README's Install section doesn't list `config.example.toml`, `LICENSE` and `THIRD_PARTY.txt`.
 - **To check on the device:** `flock --help` and `bash --version`; `pidof`/`ps` with SAM running; what Main_MiSTer sends a script it cancels; `RestoreText` on `/dev/tty0` if Main_MiSTer switched terminals; whether Downloader accepts `"v": 1` and the `mistersubsonic/` folder.
+
+## Resolved by Plan 3b
+
+- **Memory:**
+  - A prefetching stream (the queued next track) holds a ring of 1.25× its prefetch (5 MiB) until it plays, then grows to the full window.
+  - Covers are scaled straight from the decoder's output, so the full-size picture isn't converted.
+  - A 48 MiB decode budget replaces the 4096² pixel cap. A 4096² JPEG still decodes, but not an RGBA PNG of that size.
+- **Covers:**
+  - `FromImage` reads YCbCr, RGBA, NRGBA and Gray directly.
+  - `Resize` sums are 64-bit.
+- **Raw MP3:** seeking and resuming reopen the stream at a byte estimate (past the ID3v2 tag, in proportion to time), not a decode from the start. MP3s of unknown size still seek through the decoder.
+- **Device:**
+  - Calls after close are no-ops.
+  - The end of a track keeps feeding the device while its successor opens.
+  - Steady-state decoding doesn't allocate: the pending buffer is reused, and the resampler's counters no longer escape.
+- **Clean shutdown:**
+  - Openers that finish after Close close what they opened.
+  - Sources of commands still queued at Close are closed.
+  - `Events()` closes, and the player stops reading a closed stream.
+- **Stream:**
+  - A 416 before the end is an error.
+  - 408, 429 and 502–504 are retried, honouring Retry-After, on the first request too.
+  - The open timeout (`StallTimeout`) is documented.
+- **Disk cache:**
+  - Stray `.tmp` files are removed at open and after a failed write.
+  - The eviction walk recounts the size.
+- **ReplayGain:** changes apply at once: to the playing track, the queued one, and one still opening.
+- **Mock server:** honours `timeOffset` for MP3.
+
+Still open:
+- On the device: spikes 2 and 3, the `Repaint` benchmarks, input with real maps and grabs next to Main_MiSTer, and the CRT margins (see `docs/spikes.md` and `docs/testing-on-mister.md`).
+- `drawField` trims by rune.
+- The minor lists above.
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index 17633d6..7cce7b4 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -52,7 +52,17 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
 15. **Overwrite watchdog.** Over ssh, run
     `head -c 200000 /dev/urandom > /dev/fb0` while the app shows a still
     screen. The noise is gone within about 2 s.
-16. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+16. **Long MP3.** Seek to about 80% of a long constant-bitrate MP3 (a
+    podcast or a mix). Playback resumes within about a second, near the
+    target.
+17. **ReplayGain.** With a quiet and a loud album, switch Settings → Playback
+    → ReplayGain between off and track while a song plays: the level changes
+    within about half a second.
+18. **Memory.** During a gapless album, check
+    `grep VmRSS /proc/$(pidof mistersubsonic)/status` over ssh after each of
+    the first five track changes, and record the values. They level off
+    rather than keep growing.
+19. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
```

- [ ] **Step 2: Check the tree**

Run: `make test && make e2e`
Expected: `test-launcher ok`, `ok` for every Go package, `e2e ok: …` and `e2e-ui ok: …`.

- [ ] **Step 3: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" entry that Step 1 added and go to Step 5.

- [ ] **Step 4: If the MiSTer answers, run the new checks (ask the user first)**

Ask the user before doing anything on the device: `make deploy MISTER=<ip>` installs the app, and the checks play sound on their TV. With their go-ahead, have them run items 6, 16, 17 and 18 of `docs/testing-on-mister.md` with the volume low. Record the results under "Plan 3b on the MiSTer" in `docs/spikes.md`, replacing "pending".

- [ ] **Step 5: Commit**

```bash
git add docs/spikes.md docs/superpowers/plan-2a-followups.md docs/testing-on-mister.md
git commit -m "docs: Plan 3b results; device checks for MP3 seek, ReplayGain and memory" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

Everything left needs the MiSTer:
- spikes 2 and 3;
- the `Repaint` benchmarks, and any render-speed work they call for;
- input with real maps and grabs next to Main_MiSTer;
- tuning the CRT margins;
- the full `docs/testing-on-mister.md` checklist.

The small items that remain are listed in `docs/superpowers/plan-2a-followups.md`.
