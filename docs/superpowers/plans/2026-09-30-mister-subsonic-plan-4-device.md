# MiSTer Subsonic — Plan 4: on the device (sharp HDMI, media keys, volume panel, screenshots)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** fix what the first days on a real MiSTer showed:
- **Blurry text and covers on HDMI.** The app drew a fixed 1280×720 picture and resampled it to the framebuffer: 960×600 on the user's 1920×1200 screen. It now draws at the framebuffer's own size.
- **Slow redraws.** About 52 ms per frame at 960×600, against a 30 ms target. It is now about 20 ms.
- **Media keys did nothing** on a multimedia keyboard (Logitech K400 Plus): volume, mute, play/pause, next, previous. They now work on every screen.
- **The volume showed only as "Vol −12 dB" text.** It becomes a panel with a speaker and a segmented bar, shown for a moment on every change, plus a small speaker and bar on Now Playing.
- **Screenshots from the MiSTer Companion remote failed with HTTP 500.** The remote sends Alt+Scroll Lock to MiSTer's menu and waits for a new PNG in `/media/fat/screenshots`. The app holds the keyboard, so nothing came. The app now saves the screenshot itself.
- **The ARM test binaries didn't build** (an int constant that overflows on 32 bits). `make mister-test` now builds every package's tests for ARM, so this is caught on the PC.

**Architecture:**
- **`internal/gfx`:**
  - The framebuffer pack is a plain copy for 32 bpp little-endian.
  - `Blit` has a one-to-one path.
  - `check-glibc.sh` accepts static binaries.
- **`internal/ui`:**
  - `PickProfile` scales the HDMI layout to the framebuffer (`scaleProfile`), and `App` skips the scaler when nothing needs scaling.
  - `mediakeys.go` handles the media buttons after the top screen.
  - `volume.go` draws the panel and the inline indicator.
  - `screenshot.go` saves PNGs off the UI goroutine.
- **`internal/input`:** the media and screenshot key codes, and their buttons.
- **`cmd/mistersubsonic`:**
  - `-screenshots` gives the folder; its `auto` default is `/media/fat/screenshots/MiSTer_Subsonic` on the framebuffer.
  - The `-keys` names are added.
- **`internal/devview`:** the browser keys for the same buttons.

**Tech Stack:** Go 1.26+, cgo (miniaudio, speexdsp) in `internal/audio` only, no new modules.

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`:
- §8.1 (the layouts, and the 30 ms repaint target)
- §8.3–8.4 (keys and repeat)
- §10 (testing: no real devices in tests)
- §11 (spike 3's render target)

Also read `docs/spikes.md` (spikes 2 and 3, and the device facts) and `docs/testing-on-mister.md`.

**Scope:**

| | |
|---|---|
| **Plans 1–3b (done)** | Playback, the TV interface, setup, Settings, MiSTer integration, releases, device hardening |
| **This plan (4)** | Everything in the Goal above, with the results recorded, and the new checks for the TV |
| **Later** | Faster redraws at 1080p (partial redraws; in the backlog), the rest of the checklist on the TV, the first release |

**How this plan's code was produced:** every block was built and run before the plan was written.
- `go test -race ./...`, the launcher tests, `make e2e` and the ARM builds (`make mister mister-test`: glibc 2.29 ≤ 2.31) pass.
- The blocks were replayed task by task on a fresh clone of `device-checks`.
  - At every task the tests failed before the code and passed after. Two exceptions: Task 1's failure is the ARM build itself, and Task 2's tests pin behaviour the old code already has.
  - The tree ended identical to the prototype's, goldens included.
- **On the MiSTer** (test binaries only; nothing was drawn on the TV and no sound played):
  - render and pack benchmarks before and after;
  - the PNG save time.
  - The numbers are in Task 7.
- The prototype saw the new golden screenshots and checked them. They are the volume panel on each layout, plus Now Playing and the native 960×600 screens.

Copy blocks exactly. Patches must apply cleanly with `git apply`.

## Global Constraints

- **Toolchain:** Go 1.26+ (`go.mod` says `go 1.26.0`). No new modules. cgo only in `internal/audio`. ARM build: `scripts/check-glibc.sh` must report ≤ 2.31, or no glibc for static binaries.
- **Rendering (spec §8.1):**
  - A full repaint stays under 30 ms on the Cortex-A9 at 960×600 and on CRT.
  - 1080p is allowed to be slower; that work is in the backlog.
  - Nearest-neighbour scaling stays for the CRT layouts only.
- **Input (spec §8.3):**
  - Media keys act on every screen, after the top screen had its chance.
  - The screenshot key is handled before everything else, the screensaver included, and is not user activity.
  - Volume and seek repeat when held; the other media keys don't.
- **🔇 Sound safety:**
  - Tests use fakes or miniaudio's null device, never a real sound card.
  - No listening checks and nothing shown on the user's TV without their go-ahead each time.
- **Tests never touch real devices** (`/dev/tty*`, `/dev/fb0`, `/dev/input`, the MiSTer). Screenshots in tests go to `t.TempDir()`.
- **Secrets:** nothing from `.env` is printed, committed or written into docs.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

These five situations are the ones the user hit, or will hit next, on the device. Each gets its test in the task that owns the code:

1. **A 16:10 or small HDMI framebuffer.** The layout fills 960×600 or 1920×1080 with every length scaled, and fonts never go below readable sizes. The canvas is presented without a scaler when it matches the display. Tests: `TestHDMIProfileScalesToTheFramebuffer`, `TestNativeCanvasIsPresentedUnscaled`, `TestGoldenNative960x600` (Task 3).
2. **Media keys on a screen that doesn't know them** (a list, Settings, Search) and with nothing queued. Volume and play/pause work; next and previous do nothing without a queue. Tests: `TestMediaKeysActEverywhere`, `TestMediaKeysWithoutAQueue` (Task 4).
3. **Holding volume up.** The panel stays up while the key repeats, and hides 1.5 s after the last change. Muted shows grey segments and "Muted". Tests: `TestVolumePanelHides`, `TestGoldenVolumePanel` (Task 5).
4. **The Companion remote taking a screenshot while the screensaver runs.** The PNG is the frame on screen, the screensaver keeps running, and the remote never sees a half-written file. Tests: `TestScreenshotSavesTheFrameOnScreen`, `TestScreenshotDoesNotWakeTheScreensaver` (Task 6).
5. **32-bit-only breakage.** Every package's tests build for ARM in `make mister-test`. The gfx tests accept image/png's own 32-bit refusal of huge sizes (Task 1).

## Decisions this plan makes (the spec is silent or leaves room)

- **Native HDMI rendering.** `scaleProfile` scales every length of the 1280×720 layout by `min(W/1280, H/720)`. The canvas takes the whole framebuffer, so a 16:10 screen gets more rows rather than bars. Fonts stop at 15/12/10 px (title, body, small). CRT layouts keep their logical size and the scaler, because their pixels aren't square.
- **Framebuffers above 1920×1080.** MiSTer's menu halves those, whatever `fb_size` says (1920×1200 gives 960×600). The app can't change that, so it draws 960×600 sharply and the README explains it.
- **Media keys** go to the top screen first, so a text field or a screen with its own meaning for them keeps it. Next and previous need a queue; seek needs a current track and stays one second short of the end.
- **Volume panel.** The bar is linear in dB from −60 (empty) to 0 dB (full), in 20 segments. The number shown is that 0–100 level, not dB. Settings keeps showing dB. Mute no longer shows a toast, because the panel shows it.
- **Screenshots.**
  - Print Screen or Scroll Lock (MiSTer's Alt+Scroll Lock) saves `YYYYMMDD_HHMMSS.png` (with `-2`, `-3`… within the same second) into the `-screenshots` folder.
  - On the framebuffer that is `/media/fat/screenshots/MiSTer_Subsonic`: next to Main's screenshots, where the Companion remote looks.
  - The file is written as `.png.tmp` and renamed. The remote only reads `*.png`, and waits for the IEND chunk and a stable size.
  - Encoding runs off the UI goroutine at `png.BestSpeed`: 0.2 s at 960×600 and 0.7 s at 1080p on the A9. One save at a time.
- **Kept out, and why:**
  - `memmove`-based `Clear` and `Fill`, and a per-pixel blend helper. Both were measured slower on the A9.
  - Partial redraws, for 1080p. They go to the backlog.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `Makefile`, `scripts/check-glibc.sh`, `internal/gfx/*_test.go` | ARM test builds for every package; 32-bit test fixes | 1 |
| `internal/gfx/fbpack.go`, `canvas.go`, `internal/ui/bench_test.go`, `fakes_test.go` | copy pack, one-to-one blit, a benchmark that measures the app's costs | 2 |
| `internal/ui/theme.go`, `app.go` | the HDMI layout scaled to the framebuffer, no scaler at 1:1 | 3 |
| `internal/input/*.go`, `internal/ui/mediakeys.go`, `cmd/mistersubsonic/keys.go`, `internal/devview/*` | media keys | 4 |
| `internal/ui/volume.go`, `screens_settings.go`, `screens_play.go`, `app.go` | the volume panel and the Now Playing indicator | 5 |
| `internal/ui/screenshot.go`, `app.go`, `internal/input/*.go`, `cmd/mistersubsonic/main.go` | screenshots | 6 |
| `README.md`, `docs/spikes.md`, `docs/testing-on-mister.md`, `docs/superpowers/plans/backlog.md` | results and the TV checks | 7 |

---

### Task 1: Every package's tests build for ARM

**Files:**
- Modify: `Makefile` (`mister-test`, `deploy-dev`), `scripts/check-glibc.sh`
- Test: `internal/gfx/fbdev_linux_test.go`, `internal/gfx/gfx_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `make mister-test` builds every package's test binary for ARM into `bin/arm/tests/`. It copies `audio.test`, `ui.test` and `gfx.test` to `bin/arm/`, and checks their glibc.
  - `make deploy-dev` also copies `gfx.test`.
  - `check-glibc.sh` reports a statically linked binary as `needs no glibc ok`, instead of an empty version.
  - Two test fixes for 32-bit:
    - The watchdog test's pattern multiplies in `uint32`.
    - The decompression-bomb test also accepts image/png's own "dimension overflow".

- [ ] **Step 1: See the ARM build fail**

Run: `GOOS=linux GOARCH=arm GOARM=7 go vet ./internal/gfx`

Expected: FAIL (the device's test binary couldn't be built):

```
vet: internal/gfx/fbdev_linux_test.go:88:24: 2654435761 (untyped int constant) overflows int
```

- [ ] **Step 2: Fix the tests for 32-bit**

Save this patch as `/tmp/t1-test.patch` and apply it from the repository root with `git apply /tmp/t1-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the device-checks branch)):

```diff
diff --git a/internal/gfx/fbdev_linux_test.go b/internal/gfx/fbdev_linux_test.go
index e1ea44d..1942bc1 100644
--- a/internal/gfx/fbdev_linux_test.go
+++ b/internal/gfx/fbdev_linux_test.go
@@ -85,7 +85,7 @@ func TestIntactNoticesAnOverwrite(t *testing.T) {
 		}
 		c := NewCanvas(64, 48)
 		for i := range c.Pix {
-			c.Pix[i] = uint32(i*2654435761) & 0xFFFFFF
+			c.Pix[i] = uint32(i) * 2654435761 & 0xFFFFFF
 		}
 		b.Present(c)
 		if !b.Intact() {
diff --git a/internal/gfx/gfx_test.go b/internal/gfx/gfx_test.go
index 81a3738..9196328 100644
--- a/internal/gfx/gfx_test.go
+++ b/internal/gfx/gfx_test.go
@@ -249,7 +249,8 @@ func TestDecodeRejectsDecompressionBomb(t *testing.T) {
 		binary.BigEndian.PutUint32(b[20:], dim[1])
 		binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
 		_, err := DecodeImage(b, 100, 100)
-		if err == nil || !strings.Contains(err.Error(), "too large") {
+		// On 32-bit (the MiSTer) image/png refuses these sizes itself.
+		if err == nil || !strings.Contains(err.Error(), "too large") && !strings.Contains(err.Error(), "dimension overflow") {
 			t.Fatalf("%dx%d: err = %v, want \"too large\"", dim[0], dim[1], err)
 		}
 	}
```

- [ ] **Step 3: Build every package's tests for ARM**

Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the start (the device-checks branch)):

```diff
diff --git a/Makefile b/Makefile
index b6c06ff..ec04311 100644
--- a/Makefile
+++ b/Makefile
@@ -74,16 +74,20 @@ deploy: release
 	scp $(REL)/sdcard/mistersubsonic/* root@$(MISTER):/media/fat/mistersubsonic/
 
 # Audio test binary for on-device checks and benchmarks.
+# Every package's tests are built for ARM too, so 32-bit-only breakage (an
+# int overflow, say) fails here and in CI; the ones run on the device are
+# copied by deploy-dev.
 mister-test:
-	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
-	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/ui.test ./internal/ui
+	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/tests/ ./...
+	cp $(BIN)/arm/tests/audio.test $(BIN)/arm/tests/ui.test $(BIN)/arm/tests/gfx.test $(BIN)/arm/
 	./scripts/check-glibc.sh $(BIN)/arm/audio.test
 	./scripts/check-glibc.sh $(BIN)/arm/ui.test
+	./scripts/check-glibc.sh $(BIN)/arm/gfx.test
 
 # Copies the dev tools to the MiSTer (default root password is "1").
 deploy-dev: mister mister-test
 	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
-	scp $(BIN)/arm/mss-cli $(BIN)/arm/mistersubsonic $(BIN)/arm/audio.test $(BIN)/arm/ui.test root@$(MISTER):$(DEVDIR)/
+	scp $(BIN)/arm/mss-cli $(BIN)/arm/mistersubsonic $(BIN)/arm/audio.test $(BIN)/arm/ui.test $(BIN)/arm/gfx.test root@$(MISTER):$(DEVDIR)/
 	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/
 
 vendor-check:
diff --git a/scripts/check-glibc.sh b/scripts/check-glibc.sh
index fab8589..ddc8397 100755
--- a/scripts/check-glibc.sh
+++ b/scripts/check-glibc.sh
@@ -5,6 +5,10 @@ set -eu
 bin=$1
 max=${2:-2.31}
 need=$(readelf -V "$bin" | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1)
+if [ -z "$need" ]; then # pure Go, statically linked
+  echo "$bin: needs no glibc ok"
+  exit 0
+fi
 top=$(printf '%s\n%s\n' "$need" "$max" | sort -V | tail -1)
 if [ "$top" != "$max" ]; then
   echo "$bin needs glibc $need, MiSTer has $max" >&2
```

- [ ] **Step 4: Check**

Run: `GOOS=linux GOARCH=arm GOARM=7 go vet ./internal/gfx && go vet ./... && go test -race -count=1 ./internal/gfx && make mister-test` (zig on `PATH`, as for `make mister`)

Expected: `ok`, then these lines at the end:

```
bin/arm/audio.test: needs glibc 2.29 (<= 2.31) ok
bin/arm/ui.test: needs glibc 2.29 (<= 2.31) ok
bin/arm/gfx.test: needs no glibc ok
```

- [ ] **Step 5: Commit**

```bash
git add Makefile internal/gfx/fbdev_linux_test.go internal/gfx/gfx_test.go scripts/check-glibc.sh
git commit -m "test: build every package's tests for ARM; fix two 32-bit test failures" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Render speed: a copy for 32 bpp, a one-to-one Blit, an honest benchmark

**Files:**
- Modify: `internal/gfx/fbpack.go`, `internal/gfx/canvas.go`
- Test: `internal/gfx/fast_test.go`, `internal/gfx/pack_bench_test.go` (new); `internal/ui/bench_test.go`, `internal/ui/fakes_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `fbFormat.pack` copies each line for 32 bpp on a little-endian CPU. A canvas pixel `0x00RRGGBB` is already XRGB8888 in memory.
  - `Canvas.Blit` has a one-to-one path when the source is drawn at its own size (covers are decoded to fit). There is no index table, and the alpha switch stays inline.
  - Benchmarks:
    - `BenchmarkPack` (gfx) measures the framebuffer copy.
    - `BenchmarkRepaint` (ui) measures what the app pays: covers come from a cache (`fakeArtCache`), and `nullDisplay` drops frames instead of copying them. 960×600 cases are added.
  - Measured on the MiSTer (Task 7 records it): the pack went from 17.5 to 5.4 ms at 960×600. The one-to-one Blit saves about 3 ms a frame.

- [ ] **Step 1: Write the tests**

These tests pin behaviour the fast paths must keep: `Clear` and `Fill` against a pixel loop, the one-to-one `Blit` against a reference blend, and the 32 bpp copy against byte packing.

`internal/gfx/fast_test.go` (new file):

```go
package gfx

import (
	"math/rand/v2"
	"testing"
)

// The fast drawing paths give exactly what a plain per-pixel loop gives.

func randomCanvas(r *rand.Rand, w, h int) *Canvas {
	c := NewCanvas(w, h)
	for i := range c.Pix {
		c.Pix[i] = r.Uint32() & 0xFFFFFF
	}
	return c
}

func TestClearAndFillMatchAPixelLoop(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for n := range 200 {
		c := randomCanvas(r, 37, 23)
		want := &Canvas{W: c.W, H: c.H, Pix: append([]uint32(nil), c.Pix...)}
		rect := R(r.IntN(50)-10, r.IntN(40)-10, r.IntN(50), r.IntN(40))
		col := Color(r.Uint32())
		if n%2 == 0 {
			col |= 0xFF000000 // opaque: the copied-rows path
		}
		c.Fill(rect, col)
		cl := rect.Intersect(want.Bounds())
		for y := cl.Y; y < cl.Bottom(); y++ {
			for x := cl.X; x < cl.Right(); x++ {
				i := y*want.W + x
				if a := col.A(); a == 255 {
					want.Pix[i] = uint32(col) & 0xFFFFFF
				} else if a > 0 {
					want.Pix[i] = blend(want.Pix[i], uint32(col)&0xFFFFFF, a)
				}
			}
		}
		for i := range want.Pix {
			if c.Pix[i] != want.Pix[i] {
				t.Fatalf("fill %v %08x: pixel %d = %06x, want %06x", rect, uint32(col), i, c.Pix[i], want.Pix[i])
			}
		}
	}
	c := randomCanvas(r, 1000, 3)
	c.Clear(0xFF123456)
	for i, p := range c.Pix {
		if p != 0x123456 {
			t.Fatalf("clear: pixel %d = %06x", i, p)
		}
	}
}

// A one-to-one blit, clipped or not, equals the general scaled path.
func TestOneToOneBlitMatchesTheScaledPath(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	img := NewImage(19, 13)
	for i := range img.Pix {
		img.Pix[i] = r.Uint32()
		if i%3 == 0 {
			img.Pix[i] |= 0xFF000000 // opaque
		}
		if i%7 == 0 {
			img.Pix[i] &= 0x00FFFFFF // transparent
		}
	}
	for _, at := range []Rect{R(5, 4, 19, 13), R(-6, -3, 19, 13), R(30, 15, 19, 13), R(0, 0, 19, 13)} {
		base := randomCanvas(r, 40, 25)
		fast := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
		fast.Blit(img, at)
		ref := &Canvas{W: base.W, H: base.H, Pix: append([]uint32(nil), base.Pix...)}
		cl := at.Intersect(ref.Bounds())
		for y := cl.Y; y < cl.Bottom(); y++ {
			for x := cl.X; x < cl.Right(); x++ {
				i := y*ref.W + x
				ref.Pix[i] = refOver(ref.Pix[i], img.Pix[(y-at.Y)*img.W+x-at.X])
			}
		}
		for i := range ref.Pix {
			if fast.Pix[i] != ref.Pix[i] {
				t.Fatalf("blit at %v: pixel %d = %06x, want %06x", at, i, fast.Pix[i], ref.Pix[i])
			}
		}
	}
}

// The 32 bpp copy writes what the byte-by-byte packing wrote, padding untouched.
func TestPack32CopyMatchesBytePacking(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	c := randomCanvas(r, 13, 7)
	f := fbFormat{width: 13, height: 7, stride: 13*4 + 12, bpp: 32}
	got := make([]byte, f.stride*f.height)
	for i := range got {
		got[i] = 0xEE
	}
	want := append([]byte(nil), got...)
	f.pack(got, c)
	for y := 0; y < f.height; y++ {
		for x := 0; x < f.width; x++ {
			p, o := c.Pix[y*c.W+x], y*f.stride+4*x
			want[o], want[o+1], want[o+2], want[o+3] = byte(p), byte(p>>8), byte(p>>16), 0
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

// refOver is the straight-alpha pixel p drawn over the opaque d.
func refOver(d, p uint32) uint32 {
	switch a := p >> 24; a {
	case 0:
		return d
	case 255:
		return p & 0xFFFFFF
	default:
		return blend(d, p&0xFFFFFF, a)
	}
}
```

`internal/gfx/pack_bench_test.go` (new file):

```go
package gfx

import "testing"

// BenchmarkPack measures presenting one frame into framebuffer memory.
func BenchmarkPack(b *testing.B) {
	for _, c := range []struct {
		name      string
		w, h, bpp int
	}{{"960x600x32", 960, 600, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1080x16", 1920, 1080, 16}} {
		b.Run(c.name, func(b *testing.B) {
			f := fbFormat{width: c.w, height: c.h, stride: c.w * c.bpp / 8, bpp: c.bpp}
			mem := make([]byte, f.stride*f.height)
			cv := NewCanvas(c.w, c.h)
			for i := range cv.Pix {
				cv.Pix[i] = uint32(i) * 2654435761
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f.pack(mem, cv)
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
```

Then update the benchmark and its fakes. Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 36a53ed..4ee968e 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -6,8 +6,9 @@ import (
 	"mistersubsonic/internal/gfx"
 )
 
-// BenchmarkRepaint measures a full frame: draw the screen, scale it to the
-// physical framebuffer and present it (headless = one frame copy). Spec §11
+// BenchmarkRepaint measures a full frame: draw the screen and scale it to the
+// physical framebuffer. Presenting (packing into the framebuffer) is
+// BenchmarkPack in gfx; covers come from memory, as in the app. Spec §11
 // spike 3 targets < 30 ms per repaint on the MiSTer's Cortex-A9; run it on the
 // device with the cross-compiled test binary:
 //
@@ -33,13 +34,16 @@ func BenchmarkRepaint(b *testing.B) {
 			ta.now = ta.now.Add(2 * marqueeDelay)
 		}},
 		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
+		// The framebuffer a 1920x1200 display gets (fb_size halves modes above 1080p).
+		{"albums-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
+		{"feed-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
+		{"nowplaying-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
 	}
 	for _, c := range cases {
 		b.Run(c.name, func(b *testing.B) {
 			t := &testing.T{}
 			ta := newTestApp(t, c.prof)
-			ta.disp = gfx.NewHeadless(c.fbW, c.fbH, "")
-			ta.o.Display = ta.disp
+			ta.o.Display = nullDisplay{c.fbW, c.fbH} // the framebuffer's pack is measured in gfx
 			ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
 			for i := 0; i < 40; i++ { // a long list, so every row is drawn
 				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
@@ -64,3 +68,10 @@ func BenchmarkRepaint(b *testing.B) {
 		})
 	}
 }
+
+// nullDisplay takes frames without doing anything with them.
+type nullDisplay struct{ w, h int }
+
+func (d nullDisplay) Size() (int, int)          { return d.w, d.h }
+func (d nullDisplay) Present(*gfx.Canvas) error { return nil }
+func (d nullDisplay) Close() error              { return nil }
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 94b7bcb..f1c54df 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -257,10 +257,23 @@ func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
 // fakeArt makes a deterministic two-tone square per cover id.
 type fakeArt struct{}
 
+// fakeArtCache keeps each generated cover, as the real art source keeps
+// decoded covers in memory: a repaint must not pay for making them.
+var fakeArtCache sync.Map // art.Key -> *gfx.Image
+
 func (fakeArt) Get(k art.Key) (*gfx.Image, bool) {
 	if k.ID == "" {
 		return nil, false
 	}
+	if img, ok := fakeArtCache.Load(k); ok {
+		return img.(*gfx.Image), true
+	}
+	img := makeFakeArt(k)
+	fakeArtCache.Store(k, img)
+	return img, true
+}
+
+func makeFakeArt(k art.Key) *gfx.Image {
 	h := fnv.New32a()
 	h.Write([]byte(k.ID))
 	v := h.Sum32()
@@ -274,7 +287,7 @@ func (fakeArt) Get(k art.Key) (*gfx.Image, bool) {
 			img.Pix[y*k.Size+x] = c
 		}
 	}
-	return img, true
+	return img
 }
 
 func sampleLibrary() *fakeLibrary {
```

- [ ] **Step 2: Run them on the old code**

Run: `go test -count=1 ./internal/gfx ./internal/ui`

Expected: `ok` for both. The old code already behaves this way. The tests are there so the fast paths in Step 3 can't change a pixel.

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/gfx/canvas.go b/internal/gfx/canvas.go
index d97137c..fd5304b 100644
--- a/internal/gfx/canvas.go
+++ b/internal/gfx/canvas.go
@@ -101,6 +101,26 @@ func (c *Canvas) Blit(src *Image, dst Rect) {
 	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 {
 		return
 	}
+	if src.W == dst.W && src.H == dst.H {
+		// Drawn at its own size (covers are decoded to fit): no index
+		// table, no division. The pixel logic stays inline; a helper
+		// call per pixel costs more than it saves on the A9.
+		for y := clip.Y; y < clip.Bottom(); y++ {
+			sy := y - dst.Y
+			srow := src.Pix[sy*src.W+clip.X-dst.X : sy*src.W+clip.Right()-dst.X]
+			drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
+			for i, p := range srow {
+				switch a := p >> 24; a {
+				case 0:
+				case 255:
+					drow[i] = p & 0xFFFFFF
+				default:
+					drow[i] = blend(drow[i], p&0xFFFFFF, a)
+				}
+			}
+		}
+		return
+	}
 	xmap := make([]int, clip.W)
 	for i := range xmap {
 		xmap[i] = (clip.X + i - dst.X) * src.W / dst.W
diff --git a/internal/gfx/fbpack.go b/internal/gfx/fbpack.go
index a4b87b7..f51628f 100644
--- a/internal/gfx/fbpack.go
+++ b/internal/gfx/fbpack.go
@@ -1,6 +1,13 @@
 package gfx
 
-import "fmt"
+import (
+	"fmt"
+	"unsafe"
+)
+
+// littleEndian: a canvas pixel 0x00RRGGBB then lies in memory exactly as
+// XRGB8888 stores it (B, G, R, X), so a 32 bpp frame is a plain copy.
+var littleEndian = func() bool { v := uint16(1); return *(*byte)(unsafe.Pointer(&v)) == 1 }()
 
 // fbFormat describes a linear framebuffer's memory layout.
 type fbFormat struct {
@@ -24,6 +31,10 @@ func (f fbFormat) pack(mem []byte, c *Canvas) {
 	for y := 0; y < f.height; y++ {
 		src := c.Pix[y*c.W : y*c.W+f.width]
 		line := mem[y*f.stride:]
+		if f.bpp == 32 && littleEndian {
+			copy(line[:4*f.width], unsafe.Slice((*byte)(unsafe.Pointer(&src[0])), 4*f.width))
+			continue
+		}
 		if f.bpp == 32 {
 			for x, p := range src {
 				o := 4 * x
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/gfx ./internal/ui && go test -run '^$' -bench Pack -benchtime 20x ./internal/gfx`

Expected: `ok`, and the three `BenchmarkPack` cases run. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Don't swap `Clear` or `Fill` for `copy`-based fills, and don't move the blend into a helper. Both were measured slower on the Cortex-A9.

- [ ] **Step 5: Commit**

```bash
git add internal/gfx/canvas.go internal/gfx/fast_test.go internal/gfx/fbpack.go internal/gfx/pack_bench_test.go internal/ui/bench_test.go internal/ui/fakes_test.go
git commit -m "gfx: a plain copy for 32 bpp framebuffers and a one-to-one Blit; ui: the benchmark measures the app's costs" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Draw the HDMI layout at the framebuffer's size

**Files:**
- Modify: `internal/ui/theme.go`, `internal/ui/app.go`
- Test: `internal/ui/native_test.go` (new); `internal/ui/ui_test.go`, `internal/ui/bench_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `PickProfile(fbW, fbH, override)` returns `scaleProfile(ProfileHDMI, fbW, fbH)` for HDMI. CRT profiles are unchanged.
  - `scaleProfile(p, w, h)`:
    - The canvas is `w×h`.
    - Every length is scaled by `k = min(w/p.W, h/p.H)`, rounded, and at least 1.
    - Fonts are at least `minTitle, minBody, minSmall = 15, 12, 10`.
  - `App` makes a scaler only when the canvas and the display differ. `(*App).present(c)` presents through it, or directly.
  - Goldens at 960×600: `root-feed`, `albums`, `album`, `nowplaying`.
  - `BenchmarkRepaint` gains the native 960×600 and 1920×1080 cases.

- [ ] **Step 1: Write the failing tests**

`internal/ui/native_test.go` (new file):

```go
package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/player"
)

// Screens at 960x600, the framebuffer a 1920x1200 display gets: the HDMI
// layout scaled by 0.75 and drawn at that size.
func TestGoldenNative960x600(t *testing.T) {
	prof := PickProfile(960, 600, "auto")
	ta := newTestApp(t, prof)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	ta.Push(newSidebarRoot())
	ta.settle(t)
	golden(t, "root-feed-hdmi-960x600", ta.settle(t))

	ta = newTestApp(t, prof)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", "newest"))
	golden(t, "albums-hdmi-960x600", ta.settle(t))
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	golden(t, "album-hdmi-960x600", ta.settle(t))

	ta = newTestApp(t, prof)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	golden(t, "nowplaying-hdmi-960x600", ta.settle(t))
}
```

Then update the existing tests. Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 4ee968e..4c68d70 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -34,17 +34,24 @@ func BenchmarkRepaint(b *testing.B) {
 			ta.now = ta.now.Add(2 * marqueeDelay)
 		}},
 		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
-		// The framebuffer a 1920x1200 display gets (fb_size halves modes above 1080p).
-		{"albums-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
-		{"feed-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
-		{"nowplaying-hdmi-960x600", ProfileHDMI, 960, 600, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
+		// The layout scaled to the framebuffer and drawn at its size (no
+		// scaling pass): 960x600 is what a 1920x1200 display gets (the
+		// MiSTer halves framebuffers above 1920x1080).
+		{"albums-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
+		{"feed-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
+		{"nowplaying-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
+		{"albums-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
+		{"feed-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
 	}
 	for _, c := range cases {
 		b.Run(c.name, func(b *testing.B) {
 			t := &testing.T{}
 			ta := newTestApp(t, c.prof)
 			ta.o.Display = nullDisplay{c.fbW, c.fbH} // the framebuffer's pack is measured in gfx
-			ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
+			ta.scaler = nil
+			if c.prof.W != c.fbW || c.prof.H != c.fbH {
+				ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
+			}
 			for i := 0; i < 40; i++ { // a long list, so every row is drawn
 				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
 			}
diff --git a/internal/ui/ui_test.go b/internal/ui/ui_test.go
index 4699e5c..df52f2d 100644
--- a/internal/ui/ui_test.go
+++ b/internal/ui/ui_test.go
@@ -321,6 +321,41 @@ func TestPickProfile(t *testing.T) {
 	}
 }
 
+// The HDMI layout is scaled to the framebuffer and drawn at its size.
+func TestHDMIProfileScalesToTheFramebuffer(t *testing.T) {
+	if p := PickProfile(1280, 720, "auto"); p != ProfileHDMI {
+		t.Fatalf("1280x720 changed the layout: %+v", p)
+	}
+	p := PickProfile(960, 600, "auto") // a 1920x1200 display's framebuffer
+	if p.Name != "hdmi" || p.W != 960 || p.H != 600 || p.Body != 18 || p.Title != 26 || p.Cover != 128 ||
+		p.SideW != 188 || p.RowH != 42 || p.ArtNow != 300 || p.Margin != 27 {
+		t.Fatalf("960x600: %+v", p)
+	}
+	if p := PickProfile(1920, 1080, "auto"); p.W != 1920 || p.Body != 36 || p.Cover != 255 {
+		t.Fatalf("1920x1080: %+v", p)
+	}
+	if p := PickProfile(640, 360, "hdmi"); p.Title != 17 || p.Body != 12 || p.Small != minSmall { // half size; 9 px would be too small
+		t.Fatalf("a tiny framebuffer's fonts: %+v", p)
+	}
+	if p := PickProfile(640, 240, "auto"); p.W != 320 {
+		t.Fatalf("a CRT keeps its logical size: %+v", p)
+	}
+}
+
+// A canvas the display's own size is presented as it is: no scaling pass.
+func TestNativeCanvasIsPresentedUnscaled(t *testing.T) {
+	prof := PickProfile(960, 600, "auto")
+	ta := newTestApp(t, prof)
+	if ta.scaler != nil {
+		t.Fatal("a scaler was set up for a canvas the size of the display")
+	}
+	ta.Push(NewHomeScreen())
+	got := ta.settle(t)
+	if got.W != 960 || got.H != 600 {
+		t.Fatalf("presented %dx%d", got.W, got.H)
+	}
+}
+
 func TestFormatHelpers(t *testing.T) {
 	if clock(3*time.Minute+5*time.Second) != "3:05" || clock(time.Hour+time.Minute) != "1:01:00" {
 		t.Fatal("clock format")
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
undefined: minSmall
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 1a9cf28..ba81dcc 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -221,7 +221,9 @@ func New(o Options) (*App, error) {
 	}
 	a.canvas = gfx.NewCanvas(a.P.W, a.P.H)
 	pw, ph := o.Display.Size()
-	a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
+	if pw != a.P.W || ph != a.P.H { // drawn at the display's size: nothing to scale
+		a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
+	}
 	if _, ok := o.Display.(gfx.Checker); ok {
 		a.checkAt = o.Now().Add(watchdogEvery)
 	}
@@ -626,6 +628,15 @@ func (a *App) hasCurrent() bool {
 	return ok
 }
 
+// present shows the logical canvas c on the display, scaled when their
+// sizes differ.
+func (a *App) present(c *gfx.Canvas) error {
+	if a.scaler == nil {
+		return a.o.Display.Present(c)
+	}
+	return a.o.Display.Present(a.scaler.Scale(c))
+}
+
 func (a *App) render() error {
 	a.dirty = false
 	if _, np := a.Top().(*NowPlayingScreen); !np {
@@ -633,7 +644,7 @@ func (a *App) render() error {
 	}
 	if a.saver {
 		a.drawSaver(a.canvas)
-		return a.o.Display.Present(a.scaler.Scale(a.canvas))
+		return a.present(a.canvas)
 	}
 	a.animate, a.mq.seen, a.mqWake, a.dim = false, false, time.Time{}, false
 	c := a.canvas
@@ -662,7 +673,7 @@ func (a *App) render() error {
 	if a.confirm {
 		a.drawConfirm(c)
 	}
-	return a.o.Display.Present(a.scaler.Scale(c))
+	return a.present(c)
 }
 
 func (a *App) drawHeader(c *gfx.Canvas, title string) {
diff --git a/internal/ui/theme.go b/internal/ui/theme.go
index 825d93c..afd3779 100644
--- a/internal/ui/theme.go
+++ b/internal/ui/theme.go
@@ -5,6 +5,8 @@
 package ui
 
 import (
+	"math"
+
 	"mistersubsonic/internal/gfx"
 )
 
@@ -39,11 +41,14 @@ var (
 )
 
 // PickProfile chooses the layout for a framebuffer (spec §8.1): "auto" picks
-// CRT at 288 lines or fewer, otherwise HDMI.
+// CRT at 288 lines or fewer, otherwise HDMI. The HDMI layout is scaled to
+// the framebuffer, so the picture is drawn at its own resolution: sharp
+// text and covers at 960x600 or 1920x1080 rather than a 1280x720 picture
+// resampled. A CRT layout keeps its logical size (its pixels aren't square).
 func PickProfile(fbW, fbH int, override string) Profile {
 	crt := override == "crt" || (override != "hdmi" && fbH <= gfx.CRTMaxLines)
 	if !crt {
-		return ProfileHDMI
+		return scaleProfile(ProfileHDMI, fbW, fbH)
 	}
 	p := ProfileCRT240
 	if fbH == 288 || fbH == 576 {
@@ -52,6 +57,33 @@ func PickProfile(fbW, fbH int, override string) Profile {
 	return p
 }
 
+// Smallest font sizes a scaled layout uses, so a small HDMI framebuffer
+// stays readable.
+const minTitle, minBody, minSmall = 15, 12, 10
+
+// scaleProfile is p resized to a w×h canvas: every length is scaled by the
+// factor that fits p's canvas into w×h, and the canvas takes the whole of
+// w×h (a 16:10 screen gets more rows, not black bars).
+func scaleProfile(p Profile, w, h int) Profile {
+	if w == p.W && h == p.H || w <= 0 || h <= 0 {
+		return p
+	}
+	k := min(float64(w)/float64(p.W), float64(h)/float64(p.H))
+	s := func(v int) int {
+		if v == 0 {
+			return 0
+		}
+		return max(int(math.Round(float64(v)*k)), 1)
+	}
+	q := p
+	q.W, q.H = w, h
+	q.Margin, q.HeaderH, q.RowH, q.Row2H, q.Thumb = s(p.Margin), s(p.HeaderH), s(p.RowH), s(p.Row2H), s(p.Thumb)
+	q.Title, q.Body, q.Small = max(s(p.Title), minTitle), max(s(p.Body), minBody), max(s(p.Small), minSmall)
+	q.ArtNow, q.ArtAlbum, q.MiniBarH, q.SafeY = s(p.ArtNow), s(p.ArtAlbum), s(p.MiniBarH), s(p.SafeY)
+	q.MarqueeSpeed, q.Cover, q.SideW = s(p.MarqueeSpeed), s(p.Cover), s(p.SideW)
+	return q
+}
+
 // Colors.
 var (
 	colBg      = gfx.RGB(0x10, 0x14, 0x18)
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `album-hdmi-960x600`, `albums-hdmi-960x600`, `nowplaying-hdmi-960x600`, `root-feed-hdmi-960x600`. Open each one and check it: the four `*-hdmi-960x600` screens fill 960×600 with nothing cut off or overlapping, the text is crisp (no resampling blur), and the grids show more covers rather than black bars.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/bench_test.go internal/ui/native_test.go internal/ui/theme.go internal/ui/ui_test.go internal/ui/testdata/golden
git commit -m "ui: draw the HDMI layout at the framebuffer's size" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Media keys on every screen

**Files:**
- Create: `internal/ui/mediakeys.go`
- Modify: `internal/input/input.go`, `internal/input/evmap.go`, `internal/ui/app.go`, `cmd/mistersubsonic/keys.go`, `internal/devview/devview.go`, `internal/devview/page.html`, `README.md`
- Test: `internal/ui/mediakeys_test.go` (new); `internal/input/input_test.go` (modified)

**Interfaces:**
- **Produces:**
  - Buttons `BtnVolUp`, `BtnVolDown`, `BtnPlayPause`, `BtnNextTrack`, `BtnPrevTrack`, `BtnSeekFwd` and `BtnSeekBack`, after `BtnMute`.
    - Their names are `volup`, `voldown`, `playpause`, `next`, `prev`, `ffwd` and `rewind`.
    - Volume and seek repeat when held.
  - `DefaultKeys` maps these codes:
    - `KEY_MUTE` (113), `KEY_VOLUMEDOWN` (114), `KEY_VOLUMEUP` (115);
    - `KEY_NEXTSONG` (163), `KEY_PLAYPAUSE` (164), `KEY_PREVIOUSSONG` (165);
    - `KEY_REWIND` (168), `KEY_PLAYCD` (200), `KEY_PAUSECD` (201), `KEY_PLAY` (207), `KEY_FASTFORWARD` (208).
  - `(*App).mediaKey(e) bool` runs in `dispatch` after the top screen's `Handle`:
    - volume ±`volStep`;
    - `TogglePause`;
    - `Next`/`Prev` when there is a queue;
    - seek ±`seekStep` (`seekHoldStep` on repeat), clamped to the track.
  - The `-keys` script names and the dev viewer's browser keys (`AudioVolumeUp`, `MediaPlayPause`, …) cover the new buttons.

- [ ] **Step 1: Write the failing tests**

`internal/ui/mediakeys_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

// Media keys act on every screen: volume (saved, held to repeat, unmuting),
// play/pause, next, previous and seeking, even while Search takes letters.
func TestMediaKeysActEverywhere(t *testing.T) {
	ta, _ := connectedApp(t)
	playingState(ta)
	ta.pl.st.VolumeDB = -20
	ta.Push(NewSearchScreen()) // a text screen: the media keys type nothing
	ta.press(input.BtnVolUp)
	ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Repeat})
	if ta.pl.st.VolumeDB != -18 || ta.cfg.Playback.VolumeDB != -18 {
		t.Fatalf("volume %v (config %v), want -18", ta.pl.st.VolumeDB, ta.cfg.Playback.VolumeDB)
	}
	ta.press(input.BtnMute)
	ta.press(input.BtnVolDown)
	if ta.muted || ta.pl.st.VolumeDB != -19 {
		t.Fatalf("volume down while muted: muted %v at %v", ta.muted, ta.pl.st.VolumeDB)
	}
	ta.pl.calls = nil
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack} {
		ta.press(b)
	}
	if !slices.Equal(ta.pl.calls, []string{"toggle", "next", "prev"}) {
		t.Fatalf("player calls %v", ta.pl.calls)
	}
	ta.pl.st.Position = time.Minute
	ta.press(input.BtnSeekFwd)
	if ta.pl.seekPos != time.Minute+seekStep {
		t.Fatalf("seek to %v", ta.pl.seekPos)
	}
	ta.press(input.BtnSeekBack)
	if ta.pl.seekPos != time.Minute-seekStep {
		t.Fatalf("seek back to %v", ta.pl.seekPos)
	}
	if s, ok := ta.Top().(*SearchScreen); !ok || len(s.query) != 0 {
		t.Fatal("a media key typed into Search or left it")
	}
}

// With nothing playing, the transport keys do nothing (and don't panic).
func TestMediaKeysWithoutAQueue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	for _, b := range []input.Button{input.BtnPlayPause, input.BtnNextTrack, input.BtnPrevTrack, input.BtnSeekFwd} {
		ta.press(b)
	}
	if slices.Contains(ta.pl.calls, "next") || slices.Contains(ta.pl.calls, "seek") {
		t.Fatalf("player calls %v with no queue", ta.pl.calls)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index 722cf4d..06e3890 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -202,3 +202,27 @@ func TestEnterTypesNewlineOnce(t *testing.T) {
 		t.Fatalf("held enter repeats: %v", got)
 	}
 }
+
+// A multimedia keyboard's media keys (a Logitech K400 sends them on its
+// main keyboard device) map to the media buttons; volume and seek repeat.
+func TestMediaKeys(t *testing.T) {
+	tr := newTranslator(nil, nil)
+	for code, want := range map[uint16]Button{
+		keyMute: BtnMute, keyVolumeUp: BtnVolUp, keyVolumeDown: BtnVolDown,
+		keyPlayPause: BtnPlayPause, keyPlay: BtnPlayPause, keyPlayCD: BtnPlayPause, keyPauseCD: BtnPlayPause,
+		keyNextSong: BtnNextTrack, keyPreviousSong: BtnPrevTrack, keyFastForward: BtnSeekFwd, keyRewind: BtnSeekBack,
+	} {
+		got := tr.handle(evKey, code, 1)
+		if len(got) != 1 || got[0].Button != want || got[0].Kind != Press || got[0].Rune != 0 {
+			t.Errorf("key %d = %v, want %v", code, got, want)
+		}
+	}
+	for _, b := range []Button{BtnVolUp, BtnVolDown, BtnSeekFwd, BtnSeekBack} {
+		if !repeats(b) {
+			t.Errorf("%v doesn't repeat when held", b)
+		}
+	}
+	if repeats(BtnPlayPause) || repeats(BtnNextTrack) {
+		t.Error("play/pause or next repeats when held")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/input ./internal/ui ./internal/devview ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: keyMute
undefined: keyVolumeUp
undefined: keyVolumeDown
undefined: keyPlayPause
undefined: keyPlay
undefined: keyPlayCD
undefined: keyPauseCD
undefined: keyNextSong
```

- [ ] **Step 3: Implement**

`internal/ui/mediakeys.go` (new file):

```go
package ui

import (
	"time"

	"mistersubsonic/internal/input"
)

// mediaKey handles a multimedia keyboard's (or remote's) media keys, on
// every screen: volume (held to repeat), play/pause, next and previous
// track, and seeking. It reports whether e was one.
func (a *App) mediaKey(e input.Event) bool {
	if e.Kind == input.Release {
		switch e.Button {
		case input.BtnVolUp, input.BtnVolDown, input.BtnPlayPause, input.BtnNextTrack,
			input.BtnPrevTrack, input.BtnSeekFwd, input.BtnSeekBack:
			return true
		}
		return false
	}
	pl := a.Player()
	switch e.Button {
	case input.BtnVolUp, input.BtnVolDown:
		step := volStep
		if e.Button == input.BtnVolDown {
			step = -step
		}
		a.setVolume(a.volumeDB() + step)
	case input.BtnPlayPause:
		if pl != nil && e.Kind == input.Press {
			pl.TogglePause()
		}
	case input.BtnNextTrack, input.BtnPrevTrack:
		if pl != nil && e.Kind == input.Press && a.hasQueue() {
			if e.Button == input.BtnNextTrack {
				pl.Next()
			} else {
				pl.Prev()
			}
		}
	case input.BtnSeekFwd, input.BtnSeekBack:
		if pl != nil && a.hasCurrent() {
			st := pl.State()
			step := seekStep
			if e.Kind == input.Repeat {
				step = seekHoldStep
			}
			if e.Button == input.BtnSeekBack {
				step = -step
			}
			pos := st.Position + step
			if song, ok := st.Current(); ok && song.Duration > 0 {
				pos = min(pos, time.Duration(song.Duration)*time.Second-time.Second)
			}
			pl.Seek(max(pos, 0))
		}
	default:
		return false
	}
	a.dirty = true
	return true
}
```

Then Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/README.md b/README.md
index f1fc991..29a0b1b 100644
--- a/README.md
+++ b/README.md
@@ -92,7 +92,9 @@ the defaults).
 
 Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = mute,
 PgUp/PgDn = L/R, Space = Start. In Search, letters type. With a controller, mute is in
-Settings → Playback; changing the volume turns the sound back on.
+Settings → Playback; changing the volume turns the sound back on. A multimedia keyboard's media
+keys work on every screen: volume up/down (held to repeat), mute, play/pause, next, previous, and
+fast-forward/rewind to seek.
 
 ## Developing
 
diff --git a/cmd/mistersubsonic/keys.go b/cmd/mistersubsonic/keys.go
index 9f1e48c..f3325c5 100644
--- a/cmd/mistersubsonic/keys.go
+++ b/cmd/mistersubsonic/keys.go
@@ -13,6 +13,8 @@ var keyNames = map[string]input.Button{
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
 	"queue": input.BtnQueue, "mute": input.BtnMute,
+	"volup": input.BtnVolUp, "voldown": input.BtnVolDown, "playpause": input.BtnPlayPause,
+	"next": input.BtnNextTrack, "prev": input.BtnPrevTrack, "ffwd": input.BtnSeekFwd, "rewind": input.BtnSeekBack,
 }
 
 type scripted struct {
diff --git a/internal/devview/devview.go b/internal/devview/devview.go
index 3cc771e..5a2d117 100644
--- a/internal/devview/devview.go
+++ b/internal/devview/devview.go
@@ -29,6 +29,8 @@ var buttons = map[string]input.Button{
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
 	"queue": input.BtnQueue, "mute": input.BtnMute,
+	"volup": input.BtnVolUp, "voldown": input.BtnVolDown, "playpause": input.BtnPlayPause,
+	"next": input.BtnNextTrack, "prev": input.BtnPrevTrack, "ffwd": input.BtnSeekFwd, "rewind": input.BtnSeekBack,
 }
 
 // Viewer is a gfx.Display that browsers watch.
diff --git a/internal/devview/page.html b/internal/devview/page.html
index 78540c6..8ee4e26 100644
--- a/internal/devview/page.html
+++ b/internal/devview/page.html
@@ -14,7 +14,9 @@
 // t= so text fields can use letters that are also buttons.
 const keys = {ArrowUp:"up", ArrowDown:"down", ArrowLeft:"left", ArrowRight:"right",
   Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"queue", KeyM:"mute",
-  PageUp:"l", PageDown:"r", Space:"start", KeyS:"select"};
+  PageUp:"l", PageDown:"r", Space:"start", KeyS:"select",
+  AudioVolumeUp:"volup", AudioVolumeDown:"voldown", AudioVolumeMute:"mute", MediaPlayPause:"playpause",
+  MediaTrackNext:"next", MediaTrackPrevious:"prev"};
 const held = new Set();
 function send(e, down) {
   if (down && (e.ctrlKey || e.altKey || e.metaKey)) return; // leave browser shortcuts alone
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index 54e7ecd..3a58c68 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -29,12 +29,25 @@ const (
 	keyRShift    = 54
 	keySpace     = 57
 	keyPageUp    = 104
-	keyUp        = 103
-	keyLeft      = 105
-	keyRight     = 106
-	keyPageDown  = 109
-	keyDown      = 108
-	keyKPEnter   = 96
+
+	// Media keys.
+	keyMute         = 113
+	keyVolumeDown   = 114
+	keyVolumeUp     = 115
+	keyNextSong     = 163
+	keyPlayPause    = 164
+	keyPreviousSong = 165
+	keyRewind       = 168
+	keyPlayCD       = 200
+	keyPauseCD      = 201
+	keyPlay         = 207
+	keyFastForward  = 208
+	keyUp           = 103
+	keyLeft         = 105
+	keyRight        = 106
+	keyPageDown     = 109
+	keyDown         = 108
+	keyKPEnter      = 96
 
 	btnSouth  = 0x130
 	btnEast   = 0x131
@@ -57,6 +70,9 @@ var DefaultKeys = map[uint16]Button{
 	keyEnter: BtnA, keyKPEnter: BtnA, keyEsc: BtnB, keyBackspace: BtnB,
 	keyTab: BtnX, keySpace: BtnStart, keyPageUp: BtnL, keyPageDown: BtnR,
 	keyN: BtnY, keyQ: BtnQueue, keyM: BtnMute,
+	keyMute: BtnMute, keyVolumeUp: BtnVolUp, keyVolumeDown: BtnVolDown,
+	keyPlayPause: BtnPlayPause, keyPlay: BtnPlayPause, keyPlayCD: BtnPlayPause, keyPauseCD: BtnPlayPause,
+	keyNextSong: BtnNextTrack, keyPreviousSong: BtnPrevTrack, keyFastForward: BtnSeekFwd, keyRewind: BtnSeekBack,
 
 	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
 	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
diff --git a/internal/input/input.go b/internal/input/input.go
index 1b91bc1..876905e 100644
--- a/internal/input/input.go
+++ b/internal/input/input.go
@@ -24,9 +24,19 @@ const (
 	BtnStart
 	BtnQueue // keyboard only (Q): open the queue
 	BtnMute  // keyboard only (M): mute or unmute
+
+	// Media keys (multimedia keyboards, remotes): they act on every screen.
+	BtnVolUp
+	BtnVolDown
+	BtnPlayPause
+	BtnNextTrack
+	BtnPrevTrack
+	BtnSeekFwd
+	BtnSeekBack
 )
 
-var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue", "mute"}
+var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue", "mute",
+	"volup", "voldown", "playpause", "next", "prev", "ffwd", "rewind"}
 
 func (b Button) String() string {
 	if int(b) < len(buttonNames) {
@@ -65,7 +75,8 @@ const (
 // repeats reports whether b auto-repeats when held.
 func repeats(b Button) bool {
 	switch b {
-	case BtnUp, BtnDown, BtnLeft, BtnRight, BtnL, BtnR:
+	case BtnUp, BtnDown, BtnLeft, BtnRight, BtnL, BtnR,
+		BtnVolUp, BtnVolDown, BtnSeekFwd, BtnSeekBack:
 		return true
 	}
 	return false
diff --git a/internal/ui/app.go b/internal/ui/app.go
index ba81dcc..faf2089 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -580,6 +580,9 @@ func (a *App) dispatch(e input.Event) {
 	if top := a.Top(); top != nil && top.Handle(a, e) {
 		return
 	}
+	if a.mediaKey(e) {
+		return
+	}
 	if e.Kind != input.Press {
 		return
 	}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/input ./internal/ui ./internal/devview ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add README.md cmd/mistersubsonic/keys.go internal/devview/devview.go internal/devview/page.html internal/input/evmap.go internal/input/input.go internal/input/input_test.go internal/ui/app.go internal/ui/mediakeys.go internal/ui/mediakeys_test.go
git commit -m "input, ui: media keys (volume, mute, play/pause, next, previous, seek) on every screen" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: The volume panel

**Files:**
- Create: `internal/ui/volume.go`
- Modify: `internal/ui/app.go`, `internal/ui/screens_settings.go`, `internal/ui/screens_play.go`
- Test: `internal/ui/volume_test.go` (new)

**Interfaces:**
- **Produces:**
  - `volumeShowTime` (1.5 s) and `volumeSegments` (20).
  - `volumeLevel(db)` is `(db+60)/60`, clamped to 0..1.
  - `(*App).showVolume()` sets `volumeUntil`. `setVolume` and `setMuted` call it. `toggleMute` is `setMuted(!a.muted)`, and its toasts are gone.
  - `drawVolumePanel(c)` draws the panel, centred under the header:
    - a rounded `colPanel` box;
    - a speaker with 1–3 waves by level, or a cross when muted;
    - the segmented bar (accent, or grey when muted);
    - the level 0–100, or "Muted".
  - `drawVolumeInline` draws a small speaker and a 10-segment bar on Now Playing's status line, in place of "Vol −x dB".
  - `render` draws the panel after the toasts. `untilWake` wakes for its end, and `onWake` hides it.
  - Helpers: `drawSpeaker`, `drawVolumeBar`, `fillLine`, `fillRoundRect`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/volume_test.go` (new file):

```go
package ui

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

// A volume change shows the panel for a moment; muting shows it muted.
func TestGoldenVolumePanel(t *testing.T) {
	for _, p := range append(profiles, PickProfile(960, 600, "auto")) {
		name := p.Name
		if p.W == 960 {
			name += "-960x600"
		}
		ta := newTestApp(t, p)
		playingState(ta)
		ta.pl.st.VolumeDB = -19
		ta.Push(NewHomeScreen())
		ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Press}) // → -18 dB
		golden(t, "volume-panel-"+name, ta.settle(t))
		ta.press(input.BtnMute)
		golden(t, "volume-panel-muted-"+name, ta.settle(t))
	}
}

// The panel goes after volumeShowTime without another change.
func TestVolumePanelHides(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.setVolume(-10)
	if ta.volumeUntil.IsZero() {
		t.Fatal("no panel after a volume change")
	}
	if w := ta.untilWake(); w > volumeShowTime {
		t.Fatalf("next wake %v, want the panel's end within %v", w, volumeShowTime)
	}
	// Holding volume up keeps it up: every repeat restarts the time.
	ta.now = ta.now.Add(volumeShowTime - 100*time.Millisecond)
	ta.onInput(input.Event{Button: input.BtnVolUp, Kind: input.Repeat})
	ta.now = ta.now.Add(volumeShowTime - 100*time.Millisecond)
	ta.onWake()
	if ta.volumeUntil.IsZero() {
		t.Fatal("the panel went while the key repeated")
	}
	ta.now = ta.now.Add(100 * time.Millisecond)
	ta.dirty = false
	ta.onWake()
	if !ta.volumeUntil.IsZero() || !ta.dirty {
		t.Fatal("the panel didn't go, or the screen wasn't redrawn without it")
	}
}

func TestVolumeLevel(t *testing.T) {
	for db, want := range map[float64]float64{0: 1, -60: 0, -30: 0.5, -70: 0, 5: 1} {
		if got := volumeLevel(db); got != want {
			t.Errorf("volumeLevel(%v) = %v, want %v", db, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: FAIL, e.g.:

```
ta.volumeUntil undefined (type *testApp has no field or method volumeUntil)
undefined: volumeShowTime
undefined: volumeLevel
```

- [ ] **Step 3: Implement**

`internal/ui/volume.go` (new file):

```go
package ui

import (
	"fmt"
	"math"
	"time"

	"mistersubsonic/internal/gfx"
)

// The volume indicator: a panel that shows the level for a moment whenever
// the volume or mute changes (from any screen or key), and a compact
// speaker and bar on Now Playing.

// volumeShowTime is how long the panel stays after the last change.
const volumeShowTime = 1500 * time.Millisecond

// volumeSegments is how many steps the bar is drawn in.
const volumeSegments = 20

// volumeLevel is the volume as 0..1. The bar is linear in dB (−60 dB is
// empty, 0 dB full), so every 1 dB step moves it by the same amount.
func volumeLevel(db float64) float64 { return max(0, min(1, (db+60)/60)) }

// showVolume brings up the panel (again) for volumeShowTime.
func (a *App) showVolume() {
	a.volumeUntil = a.o.Now().Add(volumeShowTime)
	a.dirty = true
}

// drawSpeaker draws a speaker filling the square r: the box and cone, then
// one to three waves by level, or a cross when muted.
func drawSpeaker(c *gfx.Canvas, r gfx.Rect, level float64, muted bool, col gfx.Color) {
	x, y, s := float64(r.X), float64(r.Y), float64(min(r.W, r.H))
	cy := y + s/2
	fillPolygon(c, [][2]float64{ // box and cone as one outline
		{x, cy - s*0.16}, {x + s*0.2, cy - s*0.16}, {x + s*0.45, cy - s*0.4},
		{x + s*0.45, cy + s*0.4}, {x + s*0.2, cy + s*0.16}, {x, cy + s*0.16},
	}, col)
	if muted {
		t := max(s*0.09, 1.5) // an X to the right of the cone
		fillLine(c, x+s*0.6, cy-s*0.17, x+s*0.94, cy+s*0.17, t, col)
		fillLine(c, x+s*0.6, cy+s*0.17, x+s*0.94, cy-s*0.17, t, col)
		return
	}
	waves := 0
	switch {
	case level > 0.67:
		waves = 3
	case level > 0.34:
		waves = 2
	case level > 0:
		waves = 1
	}
	cx, t := x+s*0.45, max(s*0.07, 1)
	for i := range waves {
		rad := s * (0.2 + 0.16*float64(i))
		var pts [][2]float64
		const n = 8
		for j := 0; j <= n; j++ { // outer edge, top to bottom
			ang := -math.Pi/4 + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + (rad+t)*math.Cos(ang), cy + (rad+t)*math.Sin(ang)})
		}
		for j := n; j >= 0; j-- { // inner edge back up
			ang := -math.Pi/4 + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + rad*math.Cos(ang), cy + rad*math.Sin(ang)})
		}
		fillPolygon(c, pts, col)
	}
}

// drawVolumeBar draws the level as segs segments across r: lit up to the
// level, dark after it, and lit in grey when muted.
func drawVolumeBar(c *gfx.Canvas, r gfx.Rect, level float64, muted bool, segs int) {
	lit := int(math.Round(level * float64(segs)))
	gap := max(r.W/(segs*5), 1)
	for i := range segs {
		x0 := r.X + r.W*i/segs
		x1 := r.X + r.W*(i+1)/segs - gap
		col := colArtBg
		if i < lit {
			col = colAccent
			if muted {
				col = colDim
			}
		}
		c.Fill(gfx.R(x0, r.Y, max(x1-x0, 1), r.H), col)
	}
}

// fillLine draws a straight stroke t wide from (x0, y0) to (x1, y1).
func fillLine(c *gfx.Canvas, x0, y0, x1, y1, t float64, col gfx.Color) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	nx, ny := -dy/l*t/2, dx/l*t/2
	fillPolygon(c, [][2]float64{{x0 + nx, y0 + ny}, {x1 + nx, y1 + ny}, {x1 - nx, y1 - ny}, {x0 - nx, y0 - ny}}, col)
}

// fillRoundRect fills r with corners rounded by rad.
func fillRoundRect(c *gfx.Canvas, r gfx.Rect, rad int, col gfx.Color) {
	rad = min(rad, r.W/2, r.H/2)
	if rad <= 0 {
		c.Fill(r, col)
		return
	}
	var pts [][2]float64
	const n = 6
	corner := func(cx, cy, from float64) {
		for j := 0; j <= n; j++ {
			ang := from + math.Pi/2*float64(j)/n
			pts = append(pts, [2]float64{cx + float64(rad)*math.Cos(ang), cy + float64(rad)*math.Sin(ang)})
		}
	}
	x0, y0 := float64(r.X+rad), float64(r.Y+rad)
	x1, y1 := float64(r.Right()-rad), float64(r.Bottom()-rad)
	corner(x1, y0, -math.Pi/2)
	corner(x1, y1, 0)
	corner(x0, y1, math.Pi/2)
	corner(x0, y0, math.Pi)
	fillPolygon(c, pts, col)
}

// drawVolumePanel draws the volume panel centred under the header while it
// is due: the speaker, the bar, and the level (0–100) or "Muted".
func (a *App) drawVolumePanel(c *gfx.Canvas) {
	if a.volumeUntil.IsZero() || !a.o.Now().Before(a.volumeUntil) {
		return
	}
	p, f := a.P, a.F.Body
	db := a.volumeDB()
	level := volumeLevel(db)
	label := fmt.Sprintf("%d", int(math.Round(level*100)))
	if a.muted {
		label = "Muted"
	}
	icon := f.Height()
	barW := max(p.W/4, 6*icon)
	labelW := f.Measure("Muted")
	pad := p.Margin / 2
	w := pad + icon + pad + barW + pad + labelW + pad
	h := icon + 2*pad
	panel := gfx.R((p.W-w)/2, p.SafeY+p.HeaderH+p.Margin/2, w, h)
	fillRoundRect(c, panel, pad, colPanel.WithAlpha(0xF0))
	x := panel.X + pad
	drawSpeaker(c, gfx.R(x, panel.Y+pad, icon, icon), level, a.muted, colText)
	x += icon + pad
	bh := max(icon*2/5, 3)
	drawVolumeBar(c, gfx.R(x, panel.Y+pad+(icon-bh)/2, barW, bh), level, a.muted, volumeSegments)
	x += barW + pad
	col := colText
	if a.muted {
		col = colDim
	}
	f.Draw(c, x, panel.Y+pad+(icon+f.Ascent()-f.Descent())/2, label, col, c.Bounds())
}

// drawVolumeInline draws the compact indicator at x on the text line at
// baseline (Now Playing): a small speaker and a short bar, within maxW.
func (a *App) drawVolumeInline(c *gfx.Canvas, f *gfx.Font, x, baseline, maxW int) {
	s := f.Ascent()
	barW := 4 * s
	if s+s/3+barW > maxW {
		return
	}
	db := a.volumeDB()
	level := volumeLevel(db)
	col := colDim
	drawSpeaker(c, gfx.R(x, baseline-s, s, s), level, a.muted, col)
	bh := max(s*2/5, 3)
	drawVolumeBar(c, gfx.R(x+s+s/3, baseline-(s+bh)/2, barW, bh), level, a.muted, volumeSegments/2)
}
```

Then Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index faf2089..26ffede 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -192,6 +192,7 @@ type App struct {
 	confirm    bool      // exit confirmation shown
 
 	muted       bool      // the sound is off (not saved: the app starts with sound)
+	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
 	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
 	overwritten bool      // the last check found the screen drawn over
 }
@@ -447,6 +448,9 @@ func (a *App) untilWake() time.Duration {
 	consider(a.mqWake)
 	consider(a.saveAt)
 	consider(a.checkAt)
+	if !a.volumeUntil.IsZero() {
+		consider(a.volumeUntil)
+	}
 	consider(a.saverDue())
 	if a.saver {
 		consider(now.Add(saverStep)) // the drift; nothing else moves
@@ -504,6 +508,9 @@ func (a *App) onWake() {
 		a.saveConfig()
 	}
 	a.checkScreen(now)
+	if !a.volumeUntil.IsZero() && !now.Before(a.volumeUntil) {
+		a.volumeUntil, a.dirty = time.Time{}, true // the panel goes
+	}
 	if a.animate || (!a.mqWake.IsZero() && !now.Before(a.mqWake)) {
 		a.dirty = true
 	}
@@ -673,6 +680,7 @@ func (a *App) render() error {
 		a.mq = marquee{}
 	}
 	a.drawToasts(c)
+	a.drawVolumePanel(c)
 	if a.confirm {
 		a.drawConfirm(c)
 	}
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 0f4cd29..5696750 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -222,8 +222,9 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			mode += "  ·  " + m.label
 		}
 	}
-	mode += "  ·  " + a.volumeText(st.VolumeDB)
-	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
+	mode += "  ·  "
+	end := iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
+	a.drawVolumeInline(c, fb, end, y+fb.Ascent(), text.Right()-end)
 	y += fb.Height()
 	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
 		next := st.Queue[st.NextIndex]
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index 9420f87..7928877 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -141,6 +141,7 @@ func (a *App) setVolume(db float64) {
 		a.setMuted(false) // changing the volume brings the sound back
 	}
 	db = math.Max(-60, math.Min(0, math.Round(db)))
+	a.showVolume()
 	if pl := a.Player(); pl != nil {
 		pl.SetVolumeDB(db)
 	} else {
@@ -153,19 +154,13 @@ func (a *App) setVolume(db float64) {
 // app always starts with the sound on.
 func (a *App) setMuted(on bool) {
 	a.muted, a.dirty = on, true
+	a.showVolume()
 	if pl := a.Player(); pl != nil {
 		pl.SetMuted(on)
 	}
 }
 
-func (a *App) toggleMute() {
-	a.setMuted(!a.muted)
-	if a.muted {
-		a.Toast("Muted")
-	} else {
-		a.Toast("Sound on")
-	}
-}
+func (a *App) toggleMute() { a.setMuted(!a.muted) } // the volume panel shows it
 
 // volumeText is the volume as shown: "Muted" while the sound is off.
 func (a *App) volumeText(db float64) string {
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui`

Expected: `ok`. Golden screenshots written or changed: `nowplaying-crt`, `nowplaying-hdmi-960x600`, `nowplaying-hdmi`, `nowplaying-starred-crt`, `nowplaying-starred-hdmi`, `volume-panel-crt`, `volume-panel-hdmi-960x600`, `volume-panel-hdmi`, `volume-panel-muted-crt`, `volume-panel-muted-hdmi-960x600`, `volume-panel-muted-hdmi`. Open each one and check it: the `volume-panel-*` screens show a rounded panel under the header with the speaker, a bar 70% lit in the accent colour and "70"; the `-muted` ones a speaker with a clean ×, grey segments and "Muted"; the Now Playing screens end the status line with a small speaker and a bar instead of "Vol … dB".

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/screens_play.go internal/ui/screens_settings.go internal/ui/volume.go internal/ui/volume_test.go internal/ui/testdata/golden
git commit -m "ui: a volume panel with a speaker and a segmented bar replaces the dB text" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Screenshots for MiSTer tools

**Files:**
- Create: `internal/ui/screenshot.go`
- Modify: `internal/input/input.go`, `internal/input/evmap.go`, `internal/ui/app.go`, `cmd/mistersubsonic/main.go`, `cmd/mistersubsonic/keys.go`, `internal/devview/devview.go`, `internal/devview/page.html`
- Test: `internal/ui/screenshot_test.go` (new); `internal/input/input_test.go`, `internal/ui/bench_test.go`, `cmd/mistersubsonic/main_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `input.BtnScreenshot` (named `screenshot`), mapped from `KEY_SYSRQ` (99, Print Screen) and `KEY_SCROLLLOCK` (70). The Companion remote sends Alt+Scroll Lock.
  - `ui.Options.ScreenshotDir`. An empty folder turns the button off.
  - `(*App).onInput` takes the button before anything else, the screensaver included:
    - It isn't activity.
    - It doesn't redraw.
    - It calls `screenshot()`.
  - `screenshot()`:
    - It copies the canvas, and `saveScreenshot(dir, base, c)` encodes it off the UI goroutine.
    - The file is `base.png`, or `base-2.png` and so on within the same second. It is written as `.png.tmp`, then renamed.
    - `png.BestSpeed`.
    - The result is a toast, "Screenshot saved" or "Screenshot failed" (the error goes to the log).
    - `shooting` allows one save at a time.
  - `mistersubsonic -screenshots` (default `auto`) is resolved by `screenshotDir(flag, display, dataDir)`:
    - on the framebuffer, `<dataDir>/../screenshots/MiSTer_Subsonic`, which is `/media/fat/screenshots/MiSTer_Subsonic`;
    - elsewhere, `<dataDir>/screenshots`.
  - `BenchmarkScreenshot` measures the save at 960×600 and 1920×1080.

- [ ] **Step 1: Write the failing tests**

`internal/ui/screenshot_test.go` (new file):

```go
package ui

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

// shoot presses the screenshot button and runs posted work until the save
// reports back.
func shoot(t *testing.T, ta *testApp) {
	t.Helper()
	ta.press(input.BtnScreenshot)
	if !ta.shooting {
		t.Fatal("no screenshot started")
	}
	deadline := time.After(5 * time.Second)
	for ta.shooting {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("the screenshot never finished")
		}
	}
}

// The screenshot button saves the frame on screen as a PNG, named by the
// time, without redrawing; a second one in the same second gets its own name.
func TestScreenshotSavesTheFrameOnScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.o.ScreenshotDir = filepath.Join(t.TempDir(), "screenshots", "MiSTer_Subsonic")
	ta.Push(NewHomeScreen())
	frame := ta.settle(t).ToRGBA()
	frames := ta.disp.Frames()

	shoot(t, ta)
	if got := ta.toasts[len(ta.toasts)-1].text; got != "Screenshot saved" {
		t.Fatalf("toast %q", got)
	}
	if ta.disp.Frames() != frames {
		t.Fatal("the screenshot redrew the screen")
	}
	name := time.Unix(1_800_000_000, 0).Format("20060102_150405")
	f, err := os.Open(filepath.Join(ta.o.ScreenshotDir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	got := image.NewRGBA(img.Bounds())
	draw.Draw(got, got.Rect, img, image.Point{}, draw.Src)
	if !samePixels(frame, got) {
		t.Fatal("the PNG isn't the frame on screen")
	}

	shoot(t, ta)
	entries, _ := os.ReadDir(ta.o.ScreenshotDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != name+"-2.png" || names[1] != name+".png" {
		t.Fatalf("files %v, want %s.png and %s-2.png (and no .tmp)", names, name, name)
	}
}

// A folder that can't be made shows a toast; with no folder the button does
// nothing.
func TestScreenshotFailureAndOff(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o644)
	ta.o.ScreenshotDir = filepath.Join(file, "shots")
	ta.Push(NewHomeScreen())
	ta.settle(t)
	shoot(t, ta)
	if got := ta.toasts[len(ta.toasts)-1].text; got != "Screenshot failed" {
		t.Fatalf("toast %q", got)
	}

	ta.o.ScreenshotDir = ""
	ta.press(input.BtnScreenshot)
	if ta.shooting || len(ta.post) != 0 {
		t.Fatal("a screenshot started with no folder")
	}
}

// The screensaver can be captured too: the button doesn't wake it, and
// doesn't count as activity.
func TestScreenshotDoesNotWakeTheScreensaver(t *testing.T) {
	ta := saverApp(t, ProfileHDMI, 1)
	ta.o.ScreenshotDir = t.TempDir()
	ta.now = ta.now.Add(time.Minute)
	ta.onWake()
	if !ta.saver {
		t.Fatal("the screensaver didn't start")
	}
	idle := ta.lastInput
	ta.settle(t)
	shoot(t, ta)
	if !ta.saver || ta.lastInput != idle {
		t.Fatalf("saver %v, last input moved %v", ta.saver, ta.lastInput.Sub(idle))
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t6-test.patch` and apply it from the repository root with `git apply /tmp/t6-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index dc752ff..87c4f57 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -226,3 +226,16 @@ func TestShutdownDeadlineIsStoppedAfterACleanExit(t *testing.T) {
 	case <-time.After(400 * time.Millisecond):
 	}
 }
+
+func TestScreenshotDir(t *testing.T) {
+	for _, c := range []struct{ flag, display, data, want string }{
+		{"auto", "fbdev", "/media/fat/mistersubsonic", "/media/fat/screenshots/MiSTer_Subsonic"},
+		{"auto", "viewer", "/home/u/mss", "/home/u/mss/screenshots"},
+		{"/tmp/shots", "fbdev", "/media/fat/mistersubsonic", "/tmp/shots"},
+		{"", "fbdev", "/media/fat/mistersubsonic", ""},
+	} {
+		if got := screenshotDir(c.flag, c.display, c.data); got != c.want {
+			t.Errorf("screenshotDir(%q, %q, %q) = %q, want %q", c.flag, c.display, c.data, got, c.want)
+		}
+	}
+}
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index 06e3890..dc9822d 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -211,6 +211,7 @@ func TestMediaKeys(t *testing.T) {
 		keyMute: BtnMute, keyVolumeUp: BtnVolUp, keyVolumeDown: BtnVolDown,
 		keyPlayPause: BtnPlayPause, keyPlay: BtnPlayPause, keyPlayCD: BtnPlayPause, keyPauseCD: BtnPlayPause,
 		keyNextSong: BtnNextTrack, keyPreviousSong: BtnPrevTrack, keyFastForward: BtnSeekFwd, keyRewind: BtnSeekBack,
+		keySysRq: BtnScreenshot, keyScrollLock: BtnScreenshot,
 	} {
 		got := tr.handle(evKey, code, 1)
 		if len(got) != 1 || got[0].Button != want || got[0].Kind != Press || got[0].Rune != 0 {
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 4c68d70..2df92d2 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -1,6 +1,7 @@
 package ui
 
 import (
+	"fmt"
 	"testing"
 
 	"mistersubsonic/internal/gfx"
@@ -82,3 +83,24 @@ type nullDisplay struct{ w, h int }
 func (d nullDisplay) Size() (int, int)          { return d.w, d.h }
 func (d nullDisplay) Present(*gfx.Canvas) error { return nil }
 func (d nullDisplay) Close() error              { return nil }
+
+// BenchmarkScreenshot measures saving a frame as a PNG (off the UI
+// goroutine in the app), at the MiSTer's usual framebuffer sizes:
+//
+//	./ui.test -test.run '^$' -test.bench Screenshot -test.benchtime 5x
+func BenchmarkScreenshot(b *testing.B) {
+	for _, s := range []struct{ w, h int }{{960, 600}, {1920, 1080}} {
+		b.Run(fmt.Sprintf("%dx%d", s.w, s.h), func(b *testing.B) {
+			ta := newTestApp(&testing.T{}, scaleProfile(ProfileHDMI, s.w, s.h))
+			ta.Push(newSidebarRoot())
+			ta.settle(&testing.T{})
+			dir := b.TempDir()
+			b.ResetTimer()
+			for i := range b.N {
+				if _, err := saveScreenshot(dir, fmt.Sprint(i), ta.canvas); err != nil {
+					b.Fatal(err)
+				}
+			}
+		})
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/input ./internal/ui ./internal/devview ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: keySysRq
undefined: keyScrollLock
undefined: screenshotDir
undefined: saveScreenshot
undefined: input.BtnScreenshot
ta.shooting undefined (type *testApp has no field or method shooting)
ta.o.ScreenshotDir undefined (type Options has no field or method ScreenshotDir)
```

- [ ] **Step 3: Implement**

`internal/ui/screenshot.go` (new file):

```go
package ui

import (
	"errors"
	"fmt"
	"image/png"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"mistersubsonic/internal/gfx"
)

// screenshot saves the frame on screen as a PNG in Options.ScreenshotDir,
// named by the time (20260930_101500.png). The frame is copied here and
// encoded off the UI goroutine; a toast says how it went. One runs at a
// time: presses during a save are ignored.
func (a *App) screenshot() {
	dir := a.o.ScreenshotDir
	if dir == "" || a.shooting {
		return
	}
	a.shooting = true
	snap := gfx.NewCanvas(a.canvas.W, a.canvas.H)
	copy(snap.Pix, a.canvas.Pix)
	base := a.o.Now().Format("20060102_150405")
	go func() {
		path, err := saveScreenshot(dir, base, snap)
		a.Post(func() {
			a.shooting = false
			if err != nil {
				log.Printf("screenshot: %v", err)
				a.Toast("Screenshot failed")
				return
			}
			log.Printf("screenshot: saved %s", path)
			a.Toast("Screenshot saved")
		})
	}()
}

// saveScreenshot writes c to dir/base.png (base-2.png, … if taken), through
// a .tmp file renamed into place, so a reader that looks for the newest
// .png never sees a half-written one.
func saveScreenshot(dir, base string, c *gfx.Canvas) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, base+".png")
	for n := 2; ; n++ {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if n > 99 {
			return "", fmt.Errorf("%s: too many screenshots this second", base)
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, n))
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed} // the MiSTer's CPU is slow
	err = enc.Encode(f, c.ToRGBA())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}
```

Then Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/cmd/mistersubsonic/keys.go b/cmd/mistersubsonic/keys.go
index f3325c5..e709728 100644
--- a/cmd/mistersubsonic/keys.go
+++ b/cmd/mistersubsonic/keys.go
@@ -15,6 +15,7 @@ var keyNames = map[string]input.Button{
 	"queue": input.BtnQueue, "mute": input.BtnMute,
 	"volup": input.BtnVolUp, "voldown": input.BtnVolDown, "playpause": input.BtnPlayPause,
 	"next": input.BtnNextTrack, "prev": input.BtnPrevTrack, "ffwd": input.BtnSeekFwd, "rewind": input.BtnSeekBack,
+	"screenshot": input.BtnScreenshot,
 }
 
 type scripted struct {
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 3b0c4a0..98a839c 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -36,10 +36,10 @@ var openDevice = audio.OpenDevice
 var version = "dev"
 
 type flags struct {
-	config, display, viewerAddr, frames, profile, fbdev, keys, log string
-	null, restoreConsole                                           bool
-	volume                                                         float64
-	exitAfter                                                      time.Duration
+	config, display, viewerAddr, frames, profile, fbdev, keys, log, screenshots string
+	null, restoreConsole                                                        bool
+	volume                                                                      float64
+	exitAfter                                                                   time.Duration
 }
 
 func main() {
@@ -53,6 +53,7 @@ func main() {
 	flag.BoolVar(&f.null, "null", false, "use the null audio device (silent)")
 	flag.StringVar(&f.keys, "keys", "", `scripted button presses for testing, e.g. "a:2s,a,a" (see keys.go)`)
 	flag.StringVar(&f.log, "log", "auto", "log file: auto (log.txt next to the config on the framebuffer, stderr elsewhere), - (stderr) or a path")
+	flag.StringVar(&f.screenshots, "screenshots", "auto", "screenshot folder: auto (/media/fat/screenshots/MiSTer_Subsonic beside the config on the framebuffer, screenshots/ next to the config elsewhere), a path, or \"\" for none")
 	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
 	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
 	flag.BoolVar(&f.restoreConsole, "restore-console", false, "put the console back in text mode and exit (the launcher runs this after the app)")
@@ -94,6 +95,20 @@ func startVolume(cfgDB, flagDB float64, null bool, display, goos, goarch string)
 // logMax caps log.txt (and crash.txt): two files of 1 MB at most (spec §9).
 const logMax = 1 << 20
 
+// screenshotDir is where the screenshot button saves. On the framebuffer
+// (the MiSTer, config in /media/fat/mistersubsonic) that is MiSTer's own
+// screenshots folder, /media/fat/screenshots/MiSTer_Subsonic, where Main's
+// screenshots go and where the Companion remote looks for the newest one.
+func screenshotDir(flagDir, display, dataDir string) string {
+	if flagDir != "auto" {
+		return flagDir
+	}
+	if display == "fbdev" {
+		return filepath.Join(filepath.Dir(dataDir), "screenshots", "MiSTer_Subsonic")
+	}
+	return filepath.Join(dataDir, "screenshots")
+}
+
 // openLog sends the log to log.txt next to the config when the app runs on
 // the framebuffer (the MiSTer: nobody sees stderr there), or where -log
 // says. Crashes the app can't catch (runtime errors, panics off the UI
@@ -287,7 +302,8 @@ func run(f flags) (err error) {
 		Display: disp, Profile: prof, Inputs: inputs,
 		FallbackFonts: filepath.Join(dataDir, "fonts"),
 		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
-		Connect: func(a *ui.App, c *config.Config) { sess.connect(a, c) },
+		ScreenshotDir: screenshotDir(f.screenshots, f.display, dataDir),
+		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
 	})
 	if err != nil {
 		return err
diff --git a/internal/devview/devview.go b/internal/devview/devview.go
index 5a2d117..ccc758b 100644
--- a/internal/devview/devview.go
+++ b/internal/devview/devview.go
@@ -31,6 +31,7 @@ var buttons = map[string]input.Button{
 	"queue": input.BtnQueue, "mute": input.BtnMute,
 	"volup": input.BtnVolUp, "voldown": input.BtnVolDown, "playpause": input.BtnPlayPause,
 	"next": input.BtnNextTrack, "prev": input.BtnPrevTrack, "ffwd": input.BtnSeekFwd, "rewind": input.BtnSeekBack,
+	"screenshot": input.BtnScreenshot,
 }
 
 // Viewer is a gfx.Display that browsers watch.
diff --git a/internal/devview/page.html b/internal/devview/page.html
index 8ee4e26..cbb2652 100644
--- a/internal/devview/page.html
+++ b/internal/devview/page.html
@@ -16,7 +16,7 @@ const keys = {ArrowUp:"up", ArrowDown:"down", ArrowLeft:"left", ArrowRight:"righ
   Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"queue", KeyM:"mute",
   PageUp:"l", PageDown:"r", Space:"start", KeyS:"select",
   AudioVolumeUp:"volup", AudioVolumeDown:"voldown", AudioVolumeMute:"mute", MediaPlayPause:"playpause",
-  MediaTrackNext:"next", MediaTrackPrevious:"prev"};
+  MediaTrackNext:"next", MediaTrackPrevious:"prev", ScrollLock:"screenshot", PrintScreen:"screenshot"};
 const held = new Set();
 function send(e, down) {
   if (down && (e.ctrlKey || e.altKey || e.metaKey)) return; // leave browser shortcuts alone
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index 3a58c68..86eabc3 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -18,17 +18,19 @@ const (
 
 // Keyboard and gamepad key codes (linux/input-event-codes.h).
 const (
-	keyEsc       = 1
-	keyBackspace = 14
-	keyTab       = 15
-	keyQ         = 16
-	keyEnter     = 28
-	keyLShift    = 42
-	keyN         = 49
-	keyM         = 50
-	keyRShift    = 54
-	keySpace     = 57
-	keyPageUp    = 104
+	keyEsc        = 1
+	keyBackspace  = 14
+	keyTab        = 15
+	keyQ          = 16
+	keyEnter      = 28
+	keyLShift     = 42
+	keyN          = 49
+	keyM          = 50
+	keyRShift     = 54
+	keySpace      = 57
+	keyScrollLock = 70
+	keySysRq      = 99
+	keyPageUp     = 104
 
 	// Media keys.
 	keyMute         = 113
@@ -73,6 +75,7 @@ var DefaultKeys = map[uint16]Button{
 	keyMute: BtnMute, keyVolumeUp: BtnVolUp, keyVolumeDown: BtnVolDown,
 	keyPlayPause: BtnPlayPause, keyPlay: BtnPlayPause, keyPlayCD: BtnPlayPause, keyPauseCD: BtnPlayPause,
 	keyNextSong: BtnNextTrack, keyPreviousSong: BtnPrevTrack, keyFastForward: BtnSeekFwd, keyRewind: BtnSeekBack,
+	keySysRq: BtnScreenshot, keyScrollLock: BtnScreenshot,
 
 	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
 	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
diff --git a/internal/input/input.go b/internal/input/input.go
index 876905e..50cd69a 100644
--- a/internal/input/input.go
+++ b/internal/input/input.go
@@ -33,10 +33,15 @@ const (
 	BtnPrevTrack
 	BtnSeekFwd
 	BtnSeekBack
+
+	// BtnScreenshot saves the screen as a PNG (Print Screen, or Scroll Lock,
+	// which MiSTer tools such as the Companion remote send with Alt).
+	BtnScreenshot
 )
 
 var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue", "mute",
-	"volup", "voldown", "playpause", "next", "prev", "ffwd", "rewind"}
+	"volup", "voldown", "playpause", "next", "prev", "ffwd", "rewind",
+	"screenshot"}
 
 func (b Button) String() string {
 	if int(b) < len(buttonNames) {
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 26ffede..3dbe743 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -100,6 +100,8 @@ type Options struct {
 	ConfigErr  error
 	AudioErr   error  // the sound device couldn't be opened
 	Version    string // shown in Settings → About
+	// ScreenshotDir is where the screenshot button saves PNGs ("": off).
+	ScreenshotDir string
 	// Connect starts a connection to cfg's active server, off the UI
 	// goroutine, replacing any previous one; it answers with
 	// a.Connected or a.ConnectFailed (through a.Post).
@@ -193,6 +195,7 @@ type App struct {
 
 	muted       bool      // the sound is off (not saved: the app starts with sound)
 	volumeUntil time.Time // the volume panel shows until then (zero: hidden)
+	shooting    bool      // a screenshot is being saved
 	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
 	overwritten bool      // the last check found the screen drawn over
 }
@@ -536,6 +539,14 @@ func (a *App) onPlayer(ev player.Event) {
 }
 
 func (a *App) onInput(e input.Event) {
+	if e.Button == input.BtnScreenshot {
+		// On every screen, the screensaver too: it captures the frame as
+		// it is, so it neither wakes the screen nor counts as activity.
+		if e.Kind == input.Press {
+			a.screenshot()
+		}
+		return
+	}
 	now := a.o.Now()
 	a.lastInput = now
 	if a.wake() && e.Kind == input.Press {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/input ./internal/ui ./internal/devview ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/keys.go cmd/mistersubsonic/main.go cmd/mistersubsonic/main_test.go internal/devview/devview.go internal/devview/page.html internal/input/evmap.go internal/input/input.go internal/input/input_test.go internal/ui/app.go internal/ui/bench_test.go internal/ui/screenshot.go internal/ui/screenshot_test.go
git commit -m "ui: Print Screen and Scroll Lock save a screenshot where MiSTer tools look for one" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: Results, docs, and the check on the TV

**Files:**
- Modify: `README.md`, `docs/spikes.md`, `docs/testing-on-mister.md`, `docs/superpowers/plans/backlog.md`
- Test: `internal/ui/bench_test.go`, `internal/gfx/pack_bench_test.go` (modified: 1280×720 cases)

**Interfaces:**
- **Consumes:** everything above.
- **Produces:**
  - `docs/spikes.md` gains "Plan 4 on the MiSTer": the render, pack and screenshot measurements, with "On the TV: pending".
  - The README's video settings describe native rendering and the halved framebuffer above 1080p. The controls gain the volume panel and screenshots.
  - The checklist:
    - Item 3 covers modes above 1080p.
    - The new items are 19 (media keys), 20 (volume panel) and 21 (screenshots). The Log item becomes 22.
  - The backlog's small follow-ups gain faster redraws at 1080p.

- [ ] **Step 1: Add the 720p benchmark cases**

Save this patch as `/tmp/t7-test.patch` and apply it from the repository root with `git apply /tmp/t7-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/gfx/pack_bench_test.go b/internal/gfx/pack_bench_test.go
index e65c831..081c966 100644
--- a/internal/gfx/pack_bench_test.go
+++ b/internal/gfx/pack_bench_test.go
@@ -7,7 +7,7 @@ func BenchmarkPack(b *testing.B) {
 	for _, c := range []struct {
 		name      string
 		w, h, bpp int
-	}{{"960x600x32", 960, 600, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1080x16", 1920, 1080, 16}} {
+	}{{"960x600x32", 960, 600, 32}, {"1280x720x32", 1280, 720, 32}, {"1920x1080x32", 1920, 1080, 32}, {"1920x1080x16", 1920, 1080, 16}} {
 		b.Run(c.name, func(b *testing.B) {
 			f := fbFormat{width: c.w, height: c.h, stride: c.w * c.bpp / 8, bpp: c.bpp}
 			mem := make([]byte, f.stride*f.height)
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 2df92d2..4487299 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -41,6 +41,8 @@ func BenchmarkRepaint(b *testing.B) {
 		{"albums-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
 		{"feed-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
 		{"nowplaying-native-960x600", PickProfile(960, 600, "auto"), 960, 600, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }, nil},
+		{"albums-native-1280x720", PickProfile(1280, 720, "auto"), 1280, 720, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
+		{"feed-native-1280x720", PickProfile(1280, 720, "auto"), 1280, 720, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
 		{"albums-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }, nil},
 		{"feed-native-1920x1080", PickProfile(1920, 1080, "auto"), 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }, nil},
 	}
```

- [ ] **Step 2: Record the results and the new checks**

Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/README.md b/README.md
index 29a0b1b..369c5b2 100644
--- a/README.md
+++ b/README.md
@@ -58,10 +58,14 @@ The app draws on the MiSTer's Linux framebuffer. That framebuffer is sized from
 app never changes the video mode itself. Keep `fb_size=0` (automatic) and `fb_terminal=1` (both
 the defaults).
 
-- **HDMI:**
-  - `video_mode=0` (1280x720@60) or `7` (1280x720@50) gives a 1280x720 framebuffer, which the
-    interface fills pixel for pixel.
-  - `video_mode=8` (1920x1080@60) works too, scaled up by 1.5.
+- **HDMI:** the interface is drawn at the framebuffer's own size, pixel for pixel, so text and
+  covers stay sharp at any resolution.
+  - `video_mode=0` (1280x720@60) or `7` (1280x720@50) gives a 1280x720 framebuffer.
+  - `video_mode=8` (1920x1080@60) gives 1920x1080. It is the sharpest, but a full redraw takes
+    about 70 ms on the MiSTer, so scrolling is less smooth than at 720p (about 30 ms).
+  - Modes above 1920x1080 (for example `video_mode=1920,1200,60`) get a framebuffer of half the
+    size (960x600): MiSTer's menu halves it, whatever `fb_size` says. The interface fits that
+    size, and the display scales it up.
 - **CRT (15 kHz):** add a `[Menu]` section, for example the 240p mode SAM uses for its CRT
   video:
 
@@ -94,7 +98,11 @@ Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = m
 PgUp/PgDn = L/R, Space = Start. In Search, letters type. With a controller, mute is in
 Settings → Playback; changing the volume turns the sound back on. A multimedia keyboard's media
 keys work on every screen: volume up/down (held to repeat), mute, play/pause, next, previous, and
-fast-forward/rewind to seek.
+fast-forward/rewind to seek. A volume panel shows the level for a moment whenever it changes.
+
+**Screenshots:** Print Screen, or Alt+Scroll Lock (MiSTer's own screenshot keys), saves the
+screen as a PNG in `/media/fat/screenshots/MiSTer_Subsonic/`. The MiSTer Companion remote's
+Capture screenshot button works too.
 
 ## Developing
 
diff --git a/docs/spikes.md b/docs/spikes.md
index a9c53f2..77275af 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -100,3 +100,27 @@ pending — MiSTer unavailable. Run the checklist in `docs/testing-on-mister.md`
 ## Plan 3b on the MiSTer
 
 pending — MiSTer unavailable. Plan 3b's fixes are verified on the host. On the device, run `docs/testing-on-mister.md` items 6 (long FLAC), 16 (long MP3), 17 (ReplayGain) and 18 (memory), together with spikes 2 and 3 and the `Repaint` benchmarks.
+
+## Plan 4 on the MiSTer (2026-09-30)
+
+This MiSTer: HDMI at `video_mode=1920,1200,60`, so the framebuffer is 960×600×32. The app used to draw 1280×720 and scale it down, which blurred text. It now draws at the framebuffer's size.
+
+**Render speed** (`ui.test -test.bench Repaint`, fixed benchmark: cached covers, a display that drops frames; `gfx.test -test.bench Pack` for the framebuffer copy), per frame:
+
+| Case | Draw | Pack | Total |
+|---|---|---|---|
+| Before: 1280×720 scaled to 960×600 | ~35 ms | 17.5 ms | ~52 ms |
+| Native 960×600: albums / feed / Now Playing | 13.5 / 14.7 / 8.9 ms | 5.4 ms | **≈ 20 ms** |
+| Native 1280×720: albums / feed | 22.2 / 22.5 ms | 8.6 ms | ≈ 31 ms |
+| Native 1920×1080: albums / feed | 49.4 / 48.1 ms | 19.2 ms | ≈ 68 ms |
+| 1280×720 scaled to 1920×1080 (the old 1080p path): albums / feed | 51.7 / 49.2 ms | 19.2 ms | ≈ 70 ms |
+| CRT 240p: albums | 4.7 ms | — | — |
+
+- The pack is a straight copy for 32 bpp little-endian framebuffers (was a per-byte loop: 17.5 → 5.4 ms at 960×600).
+- `Blit` has a one-to-one path for covers drawn at their size.
+- On the Cortex-A9, Go's `memmove` is slower than the store loop for `Clear` and `Fill`, and a non-inlined per-pixel helper slows `Blit`; both were tried and left out.
+- **Target (30 ms):** met at 960×600 and on CRT, and just about at 720p (31 ms). Native 1080p is about 68 ms: sharp, but held scrolling redraws at about 15 frames a second. Partial redraws are in the backlog.
+
+**Screenshots** (`ui.test -test.bench Screenshot`): saving a PNG takes 0.21 s at 960×600 and 0.68 s at 1920×1080, off the UI goroutine. The MiSTer Companion remote waits up to 6 s.
+
+**On the TV:** pending. Run `docs/testing-on-mister.md` items 3, 19, 20 and 21 and record them here.
diff --git a/docs/superpowers/plans/backlog.md b/docs/superpowers/plans/backlog.md
index 35c4bc6..0fc4aa8 100644
--- a/docs/superpowers/plans/backlog.md
+++ b/docs/superpowers/plans/backlog.md
@@ -17,6 +17,7 @@ Suggested order:
 - **Seeks in a sized MP3 reuse the stream.** Every seek in a raw MP3 of known size reopens the stream: a few HTTP requests, and the queued next track is dropped. When the byte estimate falls inside the reader's window, seek that reader and open a fresh decoder on it instead.
 - **Accurate VBR MP3 seeks.** Read the Xing/Info header's 100-point table of contents (in the first frame after the ID3v2 tag), and use it instead of pure proportion. Without a table, keep the proportional estimate.
 - **Text fields trim by width, not by rune** (`drawField`).
+- **Faster redraws at 1080p.** A native 1920×1080 frame takes about 68 ms on the MiSTer (Plan 4's measurements in `docs/spikes.md`). Redraw only what changed for the common cases: the focus moving in a list or grid, the progress bar, the marquee, the volume panel. Present only the changed rows.
 - **The deferred minors** in `docs/superpowers/plan-2a-followups.md`, under "Plan 2c minors", "Plan 3a minors" and "Plan 3b minors". Pick the ones that still matter, for example:
   - `Promote` allocating under the stream lock;
   - the launcher's TERM handling;
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index 7cce7b4..eac92e4 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -21,8 +21,10 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
 2. **Setup wizard.** With no `config.toml`, the wizard starts. Enter a server
    with the controller, then again with a USB keyboard. Afterwards
    `config.toml` has `token` and `salt` and no `password`.
-3. **HDMI.** At 720p and 1080p (`video_mode`, see the README): the image fills
-   the screen and the text is sharp. Covers load in the grids.
+3. **HDMI.** At 720p, 1080p and a mode above 1080p such as 1920x1200
+   (`video_mode`, see the README): the image fills the screen and the text is
+   sharp. Covers load in the grids. Scrolling a long list keeps up with a held
+   D-pad.
 4. **CRT.** At 240p and 288p: the CRT layout, with nothing important cut off
    by overscan. Tune the title-safe margins if needed.
 5. **Controllers.** The user's MiSTer mapping works. Unplug the pad while the
@@ -62,7 +64,18 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
     `grep VmRSS /proc/$(pidof mistersubsonic)/status` over ssh after each of
     the first five track changes, and record the values. They level off
     rather than keep growing.
-19. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+19. **Media keys.** On a multimedia keyboard (for example a Logitech K400
+    Plus), on a browse screen and on Now Playing: volume up and down (held
+    too), mute, play/pause, next, previous. Each works without leaving the
+    screen.
+20. **Volume panel.** Every volume or mute change shows the panel for about
+    1.5 s: speaker, bar and level, or "Muted". Now Playing shows the small
+    speaker and bar in its status line.
+21. **Screenshots.** Press Print Screen, then use the MiSTer Companion
+    remote's Capture screenshot button. Each shows "Screenshot saved", adds a
+    PNG to `/media/fat/screenshots/MiSTer_Subsonic/`, and the Companion shows
+    the picture.
+22. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
```

The numbers in `docs/spikes.md` were measured on the user's MiSTer while the plan was written, using the test binaries (`ui.test`, `gfx.test`), with nothing shown on the TV. If the MiSTer answers at Step 5 and the user agrees, run them again after `make deploy-dev` and correct any figure that is off by more than 10%:

```bash
./ui.test -test.run '^$' -test.bench 'Repaint|Screenshot' -test.benchtime 30x
./gfx.test -test.run '^$' -test.bench Pack -test.benchtime 30x
```

- [ ] **Step 3: Check the tree**

Run: `go vet ./... && make test && make e2e && make mister mister-test` (zig on `PATH`)

Expected:
- `test-launcher ok`, and `ok` for every Go package.
- `e2e ok: …` and `e2e-ui ok: …`.
- The glibc lines: `mss-cli`, `mistersubsonic`, `audio.test` and `ui.test` need 2.29, and `gfx.test` needs no glibc.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/spikes.md docs/superpowers/plans/backlog.md docs/testing-on-mister.md internal/gfx/pack_bench_test.go internal/ui/bench_test.go
git commit -m "docs: Plan 4 results on the MiSTer; TV checks for media keys, the volume panel and screenshots" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 5: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" line that Step 2 added and stop here.

- [ ] **Step 6: If it answers, deploy and hand over to the user (ask first)**

Ask the user before doing anything on the device.
- `make deploy MISTER=<ip>` replaces the installed app. Their `config.toml` is kept.
- The checks run on their TV, and may play sound. Turn the volume down first.

With their go-ahead, deploy, and ask them to run checklist items 3, 19, 20 and 21 of `docs/testing-on-mister.md`: sharp text, the K400's media keys, the volume panel, and Companion screenshots. Then:
- Record what they report under "Plan 4 on the MiSTer" in `docs/spikes.md`, replacing "pending".
- Commit it: `git commit -am "docs: Plan 4 on the TV" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"`.

Anything they find that isn't a small fix goes to `docs/superpowers/plans/backlog.md`.

---

## After this plan

- Faster redraws at 1080p (backlog A).
- The rest of the checklist on the TV:
  - CRT margins;
  - BGM and SAM;
  - long files and memory;
  - Plan 3b's items.
- The first release: make the repository public and tag `v1.0.0`, only when the user asks.
- The backlog's visualizer and web remote.
