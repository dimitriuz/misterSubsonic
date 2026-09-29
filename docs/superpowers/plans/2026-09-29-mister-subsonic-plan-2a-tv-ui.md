# MiSTer Subsonic — Plan 2a: TV interface (vertical slice)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The first real app, `cmd/mistersubsonic`, which you drive with a controller or keyboard:
- **Screens:** Home (Resume plus the recently added, recently played, most played and random album lists), album pages, Now Playing and the queue.
- **Display:** it renders to the MiSTer framebuffer, or to a browser on a PC through the dev viewer, in HDMI and CRT layouts.

**Architecture:** new packages on top of Plan 1's playback core.
- `internal/gfx`: a software renderer with a 32-bit canvas, pure-Go OpenType text with bundled Noto Sans, image decode and scaling, and the fbdev and headless displays.
- `internal/input`: evdev devices, MiSTer controller maps, hotplug and key repeat.
- `internal/cache` and `internal/art`: cover art with a memory LRU, a disk LRU and `getCoverArt`.
- `internal/devview`: the browser display plus keyboard.
- `internal/ui`: a single-goroutine event loop, a screen stack, layout profiles and the screens.

The app wires them together with Plan 1's `subsonic`, `player` and `audio`.

**Tech Stack:**
- Go 1.25+
- `golang.org/x/image` v0.46.0 (opentype, font, sfnt)
- Noto Sans Regular and Bold, hinted TTF (SIL OFL 1.1)
- Plan 1's cgo audio core
- zig 0.16.0 for the ARM build

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`, §8 (UI) and §9 (dev loop). Also read `docs/superpowers/plan-1-followups.md`: this plan takes the Plan 2 items the UI needs now, and leaves the rest for Plan 2b.

**Scope:**

| | |
|---|---|
| **This plan (2a)** | Home, AlbumList, Album, Now Playing, Queue, Message screens; the mini bar; toasts; the exit prompt (hold B for 2 s on the root); HDMI and CRT profiles; the dev viewer; the headless screenshot mode; scripted keys |
| **Plan 2b** | Artists, albums A–Z/year/genre, playlists, starred, search with the on-screen keyboard, the setup wizard, settings, the screensaver, the context menu (X), cover grids and rows, marquee; dirty-rectangle presents if Task 10's A9 benchmark needs them (2a redraws whole frames, and only when something changed) |
| **Plan 3** | The launcher, KD_GRAPHICS, BGM/SAM, the framebuffer-overwrite watchdog (spec §8.1), releases |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks passed:** `go test -race ./...` (13 packages), `make e2e` (the Plan 1 CLI check plus a new headless UI run against the mock server), and the ARM builds (glibc ≤ 2.29 for all four binaries).
- **Real-server run:** a silent headless run against the user's real Navidrome. It showed real cover art, the FLAC badge and live progress.
- **Replay:** the task boundaries below were replayed on a fresh clone of `main`, with each task's tests run at its own stage. The final tree matched the prototype byte for byte, and the golden screenshots regenerate identically.

Copy blocks exactly. If something doesn't compile, it's a transcription error.

## Global Constraints

- **Dependencies:** the only new one is `golang.org/x/image` v0.46.0. Its indirect deps `x/text` and `x/sys` come in through `go mod tidy`. Fonts are embedded with `go:embed`.
- **Rendering:** pure Go, with no cgo outside `internal/audio`. The canvas is XRGB8888 (`0x00RRGGBB`). The framebuffer may be 16 bpp (RGB565) or 32 bpp.
- **Layout profiles (exact values in `theme.go`):**
  - HDMI: 1280×720 logical.
  - CRT: 320×240, or 320×288 on 288- or 576-line framebuffers.
  - `auto` means CRT when the framebuffer is 288 lines or fewer (`gfx.CRTMaxLines`).
- **Scaling:** framebuffers of 288 lines or fewer fill the screen, and non-square pixels are intended. Taller ones get a uniform nearest-neighbour scale with black bars.
- **Key repeat** (spec §8.4): 350 ms delay, then 110 ms, then 45 ms after 6 repeats. It applies to Up/Down/Left/Right/L/R only.
- **Controls** (spec §8.3):

  | Button | Action |
  |---|---|
  | A | open / play |
  | B | back; hold it 2 s on the root to get the exit prompt |
  | Y | Now Playing (in Now Playing: the queue) |
  | Start | play/pause everywhere |
  | L/R | page in lists; previous/next track in Now Playing |
  | Left/Right in Now Playing | seek ±10 s, or ±30 s while held |
  | Select in Now Playing | cycles In order → Shuffle → Repeat all → Repeat one |
  | Select on an album | shuffle-play |
  | X in the queue | remove |

- **Keyboard:** arrows move; Enter = A; Esc/Backspace = B; Tab = X; N and Q = Y; PgUp/PgDn = L/R; Space = Start; S = Select (dev viewer only).
- **Threading:** the UI loop never does I/O. Loads go through `App.Load` and are dropped if their screen was popped. Cover art comes from background workers.
- **Credentials** never appear in error text. The art fetcher never echoes the URL.
- **🔇 Sound safety:**
  - Never play sound on the user's real devices without asking first.
  - Tests and scripts always use `-null` or fakes.
  - `-display viewer` without `-null` starts at −30 dB unless `-volume` is given.
- **Tests** need no network, sound device, framebuffer or input device. The golden screenshots are exact-pixel; regenerate them with `go test ./internal/ui -update` and look at them.
- **Commits:** one per task. The trailer `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` goes in the body, as a second `-m`.

## Review Focus

1. **Characters the bundled font lacks** (Japanese or emoji titles) must still show something. Noto's own .notdef glyph is empty, so without a fix those titles vanish. The prototype found this. Test: `TestMissingGlyphsDrawBoxes` (Task 2).
2. **Truncating Cyrillic or Greek titles** must never split a UTF-8 sequence. Test: `TestTruncateKeepsValidUTF8` (Task 2).
3. **Malicious or enormous cover art** (a decompression bomb) must be rejected before decoding, not exhaust the MiSTer's RAM. Test: `TestDecodeRejectsDecompressionBomb` (Task 2).
4. **An empty library or an album with no tracks:** "No albums", and Play does nothing, without a panic. Test: `TestEmptyLibraryAndEmptyAlbum` (Task 8).
5. **Pressing buttons on the "Connecting…" screen**, before a player exists, must be harmless. Test: `TestKeysBeforePlayerAttached` (Task 8).

---

### Task 1: Player and engine additions the UI needs

These come from `docs/superpowers/plan-1-followups.md`:
- `State.NextIndex`, for the "Next: …" line.
- Latest-wins seek coalescing in the engine, so key-repeat seeking can never fill the engine's 64-slot command queue and block the player.
- User actions (PlayNow/Jump/Next/Prev/ResumeFrom) reset the consecutive-failure counter.

**Files:**
- Modify: `internal/audio/engine.go`, `internal/player/player.go`
- Test: `internal/audio/engine_test.go`, `internal/player/player_test.go` (tests appended)

**Interfaces:**
- Produces:
  - `player.State.NextIndex int`: the queue index that plays next, honouring shuffle and repeat, or −1.
  - `audio.Engine.Seek`: same signature. It now coalesces, so a new seek replaces one that is still queued.

- [ ] **Step 1: Apply the change as a patch**

The patch holds the tests and the implementation together.
1. Save the diff block below to a file outside the repo, e.g. `"$TMPDIR/task1.patch"`, without the fence lines.
2. Apply only its TEST hunks: `git apply --include='*_test.go' "$TMPDIR/task1.patch"`.

That sets up the failing tests for Step 2.

```diff
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index 9d54a7d..f9c3f5a 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -95,12 +95,13 @@ type Engine struct {
 	quit   chan struct{}
 	done   chan struct{}
 
-	mu     sync.Mutex
-	segs   []segment // guarded by mu
-	evq    []Event   // guarded by mu
-	busy   io.Closer // guarded by mu: source of the voice being decoded
-	killed io.Closer // guarded by mu: source closed by interrupt, reason for failed open
-	closed bool      // guarded by mu
+	mu      sync.Mutex
+	segs    []segment // guarded by mu
+	evq     []Event   // guarded by mu
+	busy    io.Closer // guarded by mu: source of the voice being decoded
+	killed  io.Closer // guarded by mu: source closed by interrupt, reason for failed open
+	closed  bool      // guarded by mu
+	seekReq *seekReq  // guarded by mu: latest requested seek, not yet run
 
 	// Owned by the run goroutine.
 	cur      *voice
@@ -177,10 +178,32 @@ func (e *Engine) Stop() {
 // command: the decoder seek may restart an HTTP request, and the caller
 // must not wait for that. A failure arrives as EventSeekFailed; success
 // shows in Position.
+type seekReq struct {
+	id  uint64
+	pos time.Duration
+}
+
+// Seek asks for a seek and returns at once. Seeks coalesce: if one is already
+// waiting to run, it is replaced by this one (latest wins), so a burst of
+// seeks behind a slow HTTP restart never fills the command queue.
 func (e *Engine) Seek(id uint64, pos time.Duration) {
+	e.mu.Lock()
+	queued := e.seekReq != nil
+	e.seekReq = &seekReq{id, pos}
+	e.mu.Unlock()
+	if queued {
+		return
+	}
 	e.send(func() {
-		if err := e.doSeek(id, pos); err != nil {
-			e.queueEvent(Event{Kind: EventSeekFailed, TrackID: id, Err: err})
+		e.mu.Lock()
+		r := e.seekReq
+		e.seekReq = nil
+		e.mu.Unlock()
+		if r == nil {
+			return
+		}
+		if err := e.doSeek(r.id, r.pos); err != nil {
+			e.queueEvent(Event{Kind: EventSeekFailed, TrackID: r.id, Err: err})
 		}
 	})
 }
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index 5e61d41..a59e06d 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -2,7 +2,9 @@ package audio
 
 import (
 	"bytes"
+	"io"
 	"os"
+	"sync/atomic"
 	"testing"
 	"time"
 )
@@ -562,3 +564,51 @@ func TestEngineSoftClipsBoostedSamples(t *testing.T) {
 		t.Fatalf("peak %v: the loud end was squashed, not clipped", peak)
 	}
 }
+
+// blockingSeekDecoder blocks inside SeekFrame until released, like a raw
+// FLAC seek waiting on an HTTP Range restart.
+type blockingSeekDecoder struct {
+	fakeDecoder
+	release chan struct{}
+	seeks   *atomic.Int32
+	last    *atomic.Uint64
+}
+
+func (d *blockingSeekDecoder) SeekFrame(f uint64) error {
+	d.seeks.Add(1)
+	<-d.release
+	d.last.Store(f)
+	return d.fakeDecoder.SeekFrame(f)
+}
+
+func TestEngineSeeksCoalesceLatestWins(t *testing.T) {
+	out := newFakeOutput(1000)
+	release := make(chan struct{})
+	var seeks atomic.Int32
+	var last atomic.Uint64
+	open := func(src io.ReadSeeker, f Format) (Decoder, error) {
+		d, err := fakeOpen(src, f)
+		if err != nil {
+			return nil, err
+		}
+		return &blockingSeekDecoder{fakeDecoder: *d.(*fakeDecoder), release: release, seeks: &seeks, last: &last}, nil
+	}
+	e := NewEngine(EngineOptions{Output: out, OpenDecoder: open, ChunkFrames: 256, Poll: time.Millisecond})
+	defer e.Close()
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(480000, 0), OutputRate)})
+	expectEvent(t, e, EventStarted, 1)
+
+	start := time.Now()
+	for i := 1; i <= 200; i++ { // far more than the 64-slot command queue
+		e.Seek(1, time.Duration(i)*10*time.Millisecond)
+	}
+	if d := time.Since(start); d > 200*time.Millisecond {
+		t.Fatalf("200 seeks took %v; Seek must not block", d)
+	}
+	waitFor(t, "first seek running", func() bool { return seeks.Load() >= 1 })
+	close(release)
+	waitFor(t, "latest seek applied", func() bool { return last.Load() == uint64(2*OutputRate) })
+	if n := seeks.Load(); n > 2 {
+		t.Fatalf("%d decoder seeks for a burst of 200; want at most 2 (running + latest)", n)
+	}
+}
diff --git a/internal/player/player.go b/internal/player/player.go
index 0d5e9a6..593627e 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -69,6 +69,9 @@ type State struct {
 	Repeat     Repeat
 	VolumeDB   float64
 	Transcoded bool
+	// NextIndex is the queue index that plays after the current song
+	// (honouring shuffle and repeat), or -1.
+	NextIndex int
 }
 
 func (s State) Current() (subsonic.Song, bool) {
@@ -251,6 +254,7 @@ func (p *Player) do(f func()) {
 // PlayNow replaces the queue and starts songs[start].
 func (p *Player) PlayNow(songs []subsonic.Song, start int) {
 	p.do(func() {
+		p.failures = 0 // a user action starts a fresh failure count
 		if start < 0 || start >= len(songs) {
 			return
 		}
@@ -365,6 +369,7 @@ func (p *Player) Clear() {
 // Jump plays queue[i].
 func (p *Player) Jump(i int) {
 	p.do(func() {
+		p.failures = 0 // a user action starts a fresh failure count
 		for oi, qi := range p.order {
 			if qi == i {
 				p.startAt(oi, 0)
@@ -376,6 +381,7 @@ func (p *Player) Jump(i int) {
 
 func (p *Player) Next() {
 	p.do(func() {
+		p.failures = 0 // a user action starts a fresh failure count
 		if c := p.followingCursor(false); c >= 0 {
 			p.startAt(c, 0)
 		} else {
@@ -388,6 +394,7 @@ func (p *Player) Next() {
 // played for more than 3 seconds.
 func (p *Player) Prev() {
 	p.do(func() {
+		p.failures = 0 // a user action starts a fresh failure count
 		if p.cursor < 0 {
 			return
 		}
@@ -488,6 +495,7 @@ func (p *Player) Resumable(ctx context.Context) (*Resume, error) {
 // ResumeFrom loads r into the queue and starts playing at its position.
 func (p *Player) ResumeFrom(r *Resume) {
 	p.do(func() {
+		p.failures = 0 // a user action starts a fresh failure count
 		if r == nil || r.Index < 0 || r.Index >= len(r.Songs) {
 			return
 		}
@@ -514,7 +522,10 @@ func (p *Player) emit(ev Event) {
 
 func (p *Player) publish() {
 	s := State{Queue: p.queue, Index: p.currentIndex(), Status: p.status, Position: p.position,
-		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Transcoded: p.curSrc.Transcoded}
+		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Transcoded: p.curSrc.Transcoded, NextIndex: -1}
+	if c := p.followingCursor(true); c >= 0 {
+		s.NextIndex = p.order[c]
+	}
 	p.mu.Lock()
 	p.snap = s
 	p.mu.Unlock()
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index 92d699d..a3d425a 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -646,3 +646,51 @@ func TestNewClampsVolume(t *testing.T) {
 		t.Fatalf("VolumeDB = %v, want -60 (clamped from -100)", got)
 	}
 }
+
+func TestStateNextIndexFollowsRepeatAndShuffle(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(3, 100), 0)
+	if n := h.p.State().NextIndex; n != 1 {
+		t.Fatalf("NextIndex = %d, want 1", n)
+	}
+	h.p.Jump(2)
+	if n := h.p.State().NextIndex; n != -1 {
+		t.Fatalf("at the end with repeat off: NextIndex = %d, want -1", n)
+	}
+	h.p.SetRepeat(RepeatAll)
+	if n := h.p.State().NextIndex; n != 0 {
+		t.Fatalf("repeat-all wrap: NextIndex = %d, want 0", n)
+	}
+	h.p.SetRepeat(RepeatOne)
+	if n := h.p.State().NextIndex; n != 2 {
+		t.Fatalf("repeat-one: NextIndex = %d, want 2", n)
+	}
+	h.p.SetRepeat(RepeatOff)
+	h.p.Jump(0)
+	h.p.SetShuffle(true)
+	st := h.p.State()
+	var order []int
+	h.p.do(func() { order = append([]int(nil), h.p.order...) })
+	if st.NextIndex != order[1] {
+		t.Fatalf("shuffled NextIndex = %d, want order[1] = %d", st.NextIndex, order[1])
+	}
+}
+
+func TestUserActionResetsFailureCount(t *testing.T) {
+	h := newHarness(t, nil)
+	h.opener.fail["sa"], h.opener.fail["sb"], h.opener.fail["x1"] = true, true, true
+	h.p.PlayNow(songs(2, 100), 0) // both fail: 2 failures, then the queue ends
+	h.waitFor("stopped after two failures", func() bool {
+		return h.p.State().Status == Stopped && len(h.opener.callList()) == 2
+	})
+	fresh := []subsonic.Song{{ID: "x1", Suffix: "flac", Duration: 100}, {ID: "x2", Suffix: "flac", Duration: 100}}
+	h.p.PlayNow(fresh, 0) // x1 fails; that must be failure 1, not 3
+	h.waitFor("x2 opened", func() bool {
+		for _, c := range h.opener.callList() {
+			if c.id == "x2" {
+				return true
+			}
+		}
+		return false
+	})
+}
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `go test -race -count=1 -timeout 60s -run 'NextIndex|ResetsFailure|Coalesce' ./internal/player/ ./internal/audio/`

Expected failures:
- **Build failure:** `unknown field NextIndex` / `h.p.State().NextIndex undefined`.
- **Timeout:** if the build succeeds, `TestEngineSeeksCoalesceLatestWins` hangs until the timeout, because 200 uncoalesced seeks block the command queue.

- [ ] **Step 3: Apply the implementation hunks**

Run: `git apply --exclude='*_test.go' "$TMPDIR/task1.patch"`

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/audio/ ./internal/player/ && go test -race -count=10 ./internal/audio/ ./internal/player/`
Expected: `ok` for both.

- [ ] **Step 5: Commit**

```bash
git add internal/audio internal/player
git commit -m "player: NextIndex, coalesced seeks, failure reset on user actions" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: Renderer core — canvas, images, text, scaling, headless display

**Files:**
- Create: `internal/gfx/{canvas.go,image.go,text.go,present.go,headless.go}`
- Create: `internal/gfx/fonts/{NotoSans-Regular.ttf,NotoSans-Bold.ttf,OFL.txt}`
- Test: `internal/gfx/gfx_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces, basics:
  - `gfx.Color` (0xAARRGGBB), `RGB`, `RGBA`, `Color.WithAlpha`
  - `gfx.Rect{X,Y,W,H}`, `R`, `Empty`, `Right`, `Bottom`, `Contains`, `Intersect`, `Inset`
- Produces, drawing surfaces:
  - `gfx.Canvas{W,H; Pix []uint32}`, `NewCanvas`, `Bounds`, `At`, `Clear`, `Fill` (blends when alpha < 255), `Blit(*Image, Rect)` (nearest scaling, alpha), `ToRGBA`
  - `gfx.Image{W,H; Pix []uint32}` (straight alpha), `NewImage`, `FromImage`
- Produces, images:
  - `DecodeImage(data, maxW, maxH) (*Image, error)`, which rejects anything over `MaxDecodePixels` = 4096²
  - `Fit`, `Resize` (box filter)
- Produces, text:
  - `LoadTypeface(bold bool, fallbackDir string) (*Typeface, error)`, `(*Typeface).Face(px) (*Font, error)`
  - `Font.Ascent/Descent/Height/Measure/Draw(c, x, baseline, s, col, clip) int/Truncate(s, maxW)/Marquee(s, offset)`
- Produces, displays:
  - `gfx.Display` interface `{Size() (w,h int); Present(*Canvas) error; Close() error}`
  - `gfx.Scaler`, `NewScaler(lw,lh,pw,ph)`, `Area()`, `Scale(*Canvas) *Canvas`, and `CRTMaxLines` = 288
  - `gfx.Headless`, `NewHeadless(w,h,dir)`, `Last()`, `Frames()`; `SavePNG(path, *Canvas)`

**Notes:**
- The glyph cache is per `Font` and not goroutine-safe; only the UI goroutine draws.
- A rune no font covers draws an outlined box ("tofu"), so a Japanese title in a Latin-only setup shows boxes instead of vanishing (Review Focus 1). Users can drop a CJK `.ttf` into the fallback directory.

- [ ] **Step 1: Add the dependency and the fonts**

```bash
go get golang.org/x/image@v0.46.0
mkdir -p internal/gfx/fonts
B=https://github.com/notofonts/notofonts.github.io/raw/main/fonts/NotoSans/hinted/ttf
curl -fsSL -o internal/gfx/fonts/NotoSans-Regular.ttf $B/NotoSans-Regular.ttf
curl -fsSL -o internal/gfx/fonts/NotoSans-Bold.ttf $B/NotoSans-Bold.ttf
curl -fsSL -o internal/gfx/fonts/OFL.txt https://raw.githubusercontent.com/notofonts/latin-greek-cyrillic/main/OFL.txt
file internal/gfx/fonts/*.ttf   # TrueType Font data … Noto
```

Expected sizes: Regular about 620 KB, Bold about 630 KB.

- [ ] **Step 2: Write the failing tests**

`internal/gfx/gfx_test.go`:

```go
package gfx

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unicode/utf8"
)

func TestFillOpaqueAndBlend(t *testing.T) {
	c := NewCanvas(4, 4)
	c.Clear(RGB(0, 0, 0))
	c.Fill(R(1, 1, 2, 2), RGB(255, 0, 0))
	if c.At(1, 1) != RGB(255, 0, 0) || c.At(0, 0) != RGB(0, 0, 0) || c.At(3, 3) != RGB(0, 0, 0) {
		t.Fatalf("opaque fill wrong: %08x %08x %08x", c.At(1, 1), c.At(0, 0), c.At(3, 3))
	}
	c.Fill(R(0, 0, 4, 4), RGBA(255, 255, 255, 128))
	if got := c.At(0, 0); got != RGB(128, 128, 128) {
		t.Fatalf("50%% white over black = %08x, want ff808080", got)
	}
	c.Fill(R(-10, -10, 100, 100), RGB(1, 2, 3)) // clipped, must not panic
	if c.At(3, 3) != RGB(1, 2, 3) {
		t.Fatal("clipped fill did not paint")
	}
}

func TestRectIntersect(t *testing.T) {
	if got := R(0, 0, 10, 10).Intersect(R(5, 5, 10, 10)); got != R(5, 5, 5, 5) {
		t.Fatalf("intersect = %+v", got)
	}
	if !R(0, 0, 2, 2).Intersect(R(5, 5, 1, 1)).Empty() {
		t.Fatal("disjoint rects should not intersect")
	}
}

func TestBlitScalesAndRespectsAlpha(t *testing.T) {
	src := NewImage(2, 1)
	src.Pix[0] = 0xFFFF0000 // opaque red
	src.Pix[1] = 0x00000000 // transparent
	c := NewCanvas(4, 2)
	c.Clear(RGB(0, 0, 255))
	c.Blit(src, R(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		if c.At(0, y) != RGB(255, 0, 0) || c.At(1, y) != RGB(255, 0, 0) {
			t.Fatalf("left half not red at row %d", y)
		}
		if c.At(2, y) != RGB(0, 0, 255) || c.At(3, y) != RGB(0, 0, 255) {
			t.Fatalf("transparent half painted at row %d", y)
		}
	}
	c.Blit(src, R(-3, 0, 4, 2)) // partially off-canvas: must not panic
}

func TestResizeAveragesAndFitKeepsAspect(t *testing.T) {
	src := NewImage(2, 2)
	src.Pix = []uint32{0xFF000000, 0xFFFFFFFF, 0xFFFFFFFF, 0xFF000000}
	out := Resize(src, 1, 1)
	if out.Pix[0] != 0xFF7F7F7F {
		t.Fatalf("average = %08x, want ff7f7f7f", out.Pix[0])
	}
	wide := NewImage(400, 200)
	if f := Fit(wide, 100, 100); f.W != 100 || f.H != 50 {
		t.Fatalf("Fit 400x200 into 100x100 = %dx%d", f.W, f.H)
	}
	small := NewImage(10, 10)
	if Fit(small, 100, 100) != small {
		t.Fatal("Fit must not upscale")
	}
}

func TestDecodeImagePNG(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for i := range m.Pix {
		m.Pix[i] = 0xFF
	}
	m.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, m)
	img, err := DecodeImage(buf.Bytes(), 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	if img.W != 32 || img.H != 16 {
		t.Fatalf("decoded to %dx%d, want 32x16", img.W, img.H)
	}
	if _, err := DecodeImage([]byte("not an image"), 10, 10); err == nil {
		t.Fatal("garbage decoded")
	}
}

func testFont(t *testing.T, px int) *Font {
	t.Helper()
	tf, err := LoadTypeface(false, "")
	if err != nil {
		t.Fatal(err)
	}
	f, err := tf.Face(px)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func inkIn(c *Canvas, r Rect) int {
	n := 0
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X; x < r.Right(); x++ {
			if c.At(x, y) != RGB(0, 0, 0) {
				n++
			}
		}
	}
	return n
}

func TestTextDrawsLatinCyrillicGreek(t *testing.T) {
	f := testFont(t, 24)
	for _, s := range []string{"Björk", "Аквариум", "Ελλάδα"} {
		c := NewCanvas(300, 40)
		w := f.Measure(s)
		end := f.Draw(c, 10, 30, s, RGB(255, 255, 255), c.Bounds())
		if end != 10+w {
			t.Fatalf("%q: Draw returned %d, want %d", s, end, 10+w)
		}
		if inkIn(c, R(10, 0, w, 40)) < 50 {
			t.Fatalf("%q rendered almost nothing", s)
		}
		if inkIn(c, R(10+w+2, 0, 300-(10+w+2), 40)) != 0 {
			t.Fatalf("%q drew past its measured width", s)
		}
	}
	if f.Height() < 24 || f.Ascent() <= 0 {
		t.Fatalf("metrics: ascent %d height %d", f.Ascent(), f.Height())
	}
}

func TestTextClip(t *testing.T) {
	f := testFont(t, 24)
	c := NewCanvas(200, 40)
	f.Draw(c, 0, 30, "Hello world", RGB(255, 255, 255), R(0, 0, 30, 40))
	if inkIn(c, R(30, 0, 170, 40)) != 0 {
		t.Fatal("text escaped its clip rect")
	}
}

func TestTruncate(t *testing.T) {
	f := testFont(t, 20)
	s := "A very long album title that will not fit"
	got := f.Truncate(s, 100)
	if f.Measure(got) > 100 || got[len(got)-len("…"):] != "…" {
		t.Fatalf("Truncate = %q (%d px)", got, f.Measure(got))
	}
	if f.Truncate("Short", 1000) != "Short" {
		t.Fatal("short text changed")
	}
}

func TestScalerHDMIAndCRT(t *testing.T) {
	// 1280x720 UI on a 1920x1080 framebuffer: 1.5x, full screen.
	s := NewScaler(1280, 720, 1920, 1080)
	if s.Area() != R(0, 0, 1920, 1080) {
		t.Fatalf("1080p area = %+v", s.Area())
	}
	// On 1280x1024 it letterboxes vertically.
	s = NewScaler(1280, 720, 1280, 1024)
	if a := s.Area(); a.W != 1280 || a.H != 720 || a.Y != 152 {
		t.Fatalf("1280x1024 area = %+v", a)
	}
	// 320x240 UI on a 640x240 CRT framebuffer: fills, 2x horizontally.
	s = NewScaler(320, 240, 640, 240)
	src := NewCanvas(320, 240)
	src.Pix[0] = 0xFF0000
	out := s.Scale(src)
	if out.At(0, 0) != RGB(255, 0, 0) || out.At(1, 0) != RGB(255, 0, 0) || out.At(2, 0) == RGB(255, 0, 0) {
		t.Fatal("CRT horizontal doubling wrong")
	}
	// 320x240 UI on 640x480: uniform 2x (line doubled).
	s = NewScaler(320, 240, 640, 480)
	out = s.Scale(src)
	if s.Area() != R(0, 0, 640, 480) || out.At(1, 1) != RGB(255, 0, 0) || out.At(0, 2) == RGB(255, 0, 0) {
		t.Fatalf("480-line doubling wrong (area %+v)", s.Area())
	}
}

func TestHeadlessKeepsLastFrame(t *testing.T) {
	d := NewHeadless(4, 4, t.TempDir())
	c := NewCanvas(4, 4)
	c.Clear(RGB(9, 9, 9))
	if err := d.Present(c); err != nil {
		t.Fatal(err)
	}
	c.Clear(RGB(1, 1, 1)) // later changes must not affect the stored frame
	if d.Last().At(0, 0) != RGB(9, 9, 9) || d.Frames() != 1 {
		t.Fatal("headless did not keep a copy of the frame")
	}
}

func TestMarqueeScrollsAndLoops(t *testing.T) {
	f := testFont(t, 20)
	s, dx := f.Marquee("Hello", 0)
	if dx != 0 || s[:5] != "Hello" {
		t.Fatalf("offset 0 = %q, %d", s, dx)
	}
	w := f.Measure("H")
	s, dx = f.Marquee("Hello", w+1)
	if s[0] != 'e' || dx != -1 {
		t.Fatalf("offset past first glyph = %q, %d", s[:3], dx)
	}
}

// Titles in scripts the bundled font lacks (CJK, emoji) draw .notdef boxes
// instead of crashing or vanishing.
func TestMissingGlyphsDrawBoxes(t *testing.T) {
	f := testFont(t, 24)
	c := NewCanvas(200, 40)
	w := f.Measure("日本語")
	f.Draw(c, 0, 30, "日本語", RGB(255, 255, 255), c.Bounds())
	if w <= 0 || inkIn(c, R(0, 0, w, 40)) == 0 {
		t.Fatalf("missing glyphs rendered nothing (width %d)", w)
	}
}

func TestTruncateKeepsValidUTF8(t *testing.T) {
	f := testFont(t, 20)
	for _, s := range []string{"Рок-н-ролл мёртв и снова жив", "Ελληνική μουσική παράδοση"} {
		for w := 10; w < f.Measure(s); w += 7 {
			if got := f.Truncate(s, w); !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q is not valid UTF-8", s, w, got)
			}
		}
	}
}

// A PNG header claiming a gigantic image is rejected before decoding.
func TestDecodeRejectsDecompressionBomb(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	png.Encode(&buf, m)
	b := buf.Bytes()
	// IHDR width/height live at bytes 16..23; claim 20000x20000.
	binary.BigEndian.PutUint32(b[16:], 20000)
	binary.BigEndian.PutUint32(b[20:], 20000)
	if _, err := DecodeImage(b, 100, 100); err == nil {
		t.Fatal("20000x20000 image accepted")
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/gfx/`
Expected: build failure (`undefined: NewCanvas`, `undefined: LoadTypeface`, …).

- [ ] **Step 4: Implement**

`internal/gfx/canvas.go`:

```go
// Package gfx is the software renderer: a 32-bit canvas with fills, image
// blits and text, plus the displays that show it (fbdev, headless, viewer).
package gfx

import (
	"image"
	"image/color"
)

// Color is 0xAARRGGBB. Alpha 0xFF is opaque.
type Color uint32

func RGB(r, g, b uint8) Color           { return 0xFF000000 | Color(r)<<16 | Color(g)<<8 | Color(b) }
func RGBA(r, g, b, a uint8) Color       { return Color(a)<<24 | Color(r)<<16 | Color(g)<<8 | Color(b) }
func (c Color) A() uint32               { return uint32(c) >> 24 }
func (c Color) WithAlpha(a uint8) Color { return Color(a)<<24 | c&0xFFFFFF }

type Rect struct{ X, Y, W, H int }

func R(x, y, w, h int) Rect { return Rect{x, y, w, h} }
func (r Rect) Empty() bool  { return r.W <= 0 || r.H <= 0 }
func (r Rect) Right() int   { return r.X + r.W }
func (r Rect) Bottom() int  { return r.Y + r.H }
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && y >= r.Y && x < r.Right() && y < r.Bottom()
}

// Intersect returns the overlap of r and o (empty if none).
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.Right(), o.Right()), min(r.Bottom(), o.Bottom())
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// Inset shrinks r by d on every side.
func (r Rect) Inset(d int) Rect { return Rect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

// Canvas is an opaque XRGB8888 pixel buffer (0x00RRGGBB, row-major).
type Canvas struct {
	W, H int
	Pix  []uint32
}

func NewCanvas(w, h int) *Canvas { return &Canvas{W: w, H: h, Pix: make([]uint32, w*h)} }

func (c *Canvas) Bounds() Rect { return Rect{0, 0, c.W, c.H} }

func (c *Canvas) At(x, y int) Color { return Color(c.Pix[y*c.W+x]) | 0xFF000000 }

// Clear fills the whole canvas with an opaque color.
func (c *Canvas) Clear(col Color) {
	v := uint32(col) & 0xFFFFFF
	for i := range c.Pix {
		c.Pix[i] = v
	}
}

// Fill paints r, blending when col has alpha < 255.
func (c *Canvas) Fill(r Rect, col Color) {
	r = r.Intersect(c.Bounds())
	a := col.A()
	if r.Empty() || a == 0 {
		return
	}
	v := uint32(col) & 0xFFFFFF
	for y := r.Y; y < r.Bottom(); y++ {
		row := c.Pix[y*c.W+r.X : y*c.W+r.Right()]
		if a == 255 {
			for i := range row {
				row[i] = v
			}
			continue
		}
		for i := range row {
			row[i] = blend(row[i], v, a)
		}
	}
}

// blend mixes src over dst with coverage a (0..255).
func blend(dst, src, a uint32) uint32 {
	inv := 255 - a
	rb := ((src&0xFF00FF)*a + (dst&0xFF00FF)*inv + 0x800080) >> 8 & 0xFF00FF
	g := ((src&0x00FF00)*a + (dst&0x00FF00)*inv + 0x008000) >> 8 & 0x00FF00
	return rb | g
}

// Image is a straight-alpha ARGB image (0xAARRGGBB).
type Image struct {
	W, H int
	Pix  []uint32
}

func NewImage(w, h int) *Image { return &Image{W: w, H: h, Pix: make([]uint32, w*h)} }

// Blit draws src scaled (nearest neighbour) into dst, clipped to the canvas.
func (c *Canvas) Blit(src *Image, dst Rect) {
	clip := dst.Intersect(c.Bounds())
	if clip.Empty() || src == nil || src.W == 0 || src.H == 0 {
		return
	}
	xmap := make([]int, clip.W)
	for i := range xmap {
		xmap[i] = (clip.X + i - dst.X) * src.W / dst.W
	}
	for y := clip.Y; y < clip.Bottom(); y++ {
		sy := (y - dst.Y) * src.H / dst.H
		srow := src.Pix[sy*src.W : (sy+1)*src.W]
		drow := c.Pix[y*c.W+clip.X : y*c.W+clip.Right()]
		for i, sx := range xmap {
			p := srow[sx]
			switch a := p >> 24; a {
			case 0:
			case 255:
				drow[i] = p & 0xFFFFFF
			default:
				drow[i] = blend(drow[i], p&0xFFFFFF, a)
			}
		}
	}
}

// ToRGBA converts the canvas to a standard image (for PNG output).
func (c *Canvas) ToRGBA() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for i, p := range c.Pix {
		img.Pix[4*i] = uint8(p >> 16)
		img.Pix[4*i+1] = uint8(p >> 8)
		img.Pix[4*i+2] = uint8(p)
		img.Pix[4*i+3] = 0xFF
	}
	return img
}

// FromImage converts any image.Image to an Image.
func FromImage(m image.Image) *Image {
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
```

`internal/gfx/image.go`:

```go
package gfx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // cover art formats
	_ "image/png"
)

// MaxDecodePixels guards against decompression bombs in cover art.
const MaxDecodePixels = 4096 * 4096

// DecodeImage decodes JPEG or PNG bytes and scales the result to fit within
// maxW×maxH (aspect preserved, never upscaled) with a box filter.
func DecodeImage(data []byte, maxW, maxH int) (*Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	if cfg.Width*cfg.Height > MaxDecodePixels {
		return nil, fmt.Errorf("gfx: image %dx%d too large", cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gfx: decode image: %w", err)
	}
	return Fit(FromImage(m), maxW, maxH), nil
}

// Fit returns src scaled down to fit maxW×maxH, preserving aspect ratio.
func Fit(src *Image, maxW, maxH int) *Image {
	if src.W <= maxW && src.H <= maxH {
		return src
	}
	w, h := maxW, src.H*maxW/src.W
	if h > maxH {
		h, w = maxH, src.W*maxH/src.H
	}
	return Resize(src, max(w, 1), max(h, 1))
}

// Resize box-filters src down to w×h (use Canvas.Blit for upscaling).
func Resize(src *Image, w, h int) *Image {
	out := NewImage(w, h)
	for y := 0; y < h; y++ {
		sy0, sy1 := y*src.H/h, max((y+1)*src.H/h, y*src.H/h+1)
		for x := 0; x < w; x++ {
			sx0, sx1 := x*src.W/w, max((x+1)*src.W/w, x*src.W/w+1)
			var a, r, g, b, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					p := src.Pix[sy*src.W+sx]
					pa := p >> 24
					a += pa
					r += (p >> 16 & 0xFF) * pa
					g += (p >> 8 & 0xFF) * pa
					b += (p & 0xFF) * pa
					n++
				}
			}
			if a == 0 {
				continue
			}
			out.Pix[y*w+x] = (a/n)<<24 | (r/a)<<16 | (g/a)<<8 | b/a
		}
	}
	return out
}
```

`internal/gfx/text.go`:

```go
package gfx

import (
	"embed"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/NotoSans-Regular.ttf fonts/NotoSans-Bold.ttf
var fontFS embed.FS

// Typeface is a list of fonts searched in order for each rune (the bundled
// Noto Sans first, then user fallbacks such as a CJK font).
type Typeface struct {
	fonts []*sfnt.Font
}

// LoadTypeface parses the bundled font (bold or regular) plus every .ttf /
// .otf in fallbackDir (which may be "" or missing).
func LoadTypeface(bold bool, fallbackDir string) (*Typeface, error) {
	name := "fonts/NotoSans-Regular.ttf"
	if bold {
		name = "fonts/NotoSans-Bold.ttf"
	}
	b, err := fontFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	f, err := opentype.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("gfx: parse %s: %w", name, err)
	}
	t := &Typeface{fonts: []*sfnt.Font{f}}
	if fallbackDir != "" {
		entries, _ := os.ReadDir(fallbackDir)
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".ttf" && ext != ".otf" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(fallbackDir, e.Name()))
			if err != nil {
				continue
			}
			if ff, err := opentype.Parse(data); err == nil {
				t.fonts = append(t.fonts, ff)
			}
		}
	}
	return t, nil
}

type glyph struct {
	mask    *image.Alpha
	off     image.Point // mask origin relative to the pen position on the baseline
	advance int
}

// Font is a Typeface at one pixel size, with a glyph cache. Not safe for
// concurrent use; the UI draws from one goroutine.
type Font struct {
	faces   []font.Face
	fonts   []*sfnt.Font
	cache   map[rune]*glyph
	ascent  int
	descent int
	buf     sfnt.Buffer
}

// Face returns t at size px (pixel em height).
func (t *Typeface) Face(px int) (*Font, error) {
	f := &Font{fonts: t.fonts, cache: map[rune]*glyph{}}
	for _, sf := range t.fonts {
		face, err := opentype.NewFace(sf, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
		f.faces = append(f.faces, face)
	}
	m := f.faces[0].Metrics()
	f.ascent, f.descent = m.Ascent.Ceil(), m.Descent.Ceil()
	return f, nil
}

// Ascent, Descent and Height are in pixels.
func (f *Font) Ascent() int  { return f.ascent }
func (f *Font) Descent() int { return f.descent }
func (f *Font) Height() int  { return f.ascent + f.descent }

// faceFor returns the first face that has r, or nil if none does.
func (f *Font) faceFor(r rune) font.Face {
	for i, sf := range f.fonts {
		if idx, err := sf.GlyphIndex(&f.buf, r); err == nil && idx != 0 {
			return f.faces[i]
		}
	}
	return nil
}

// tofu is the outlined box drawn for characters no font covers (Noto's own
// .notdef glyph is empty, which would make e.g. Japanese titles vanish).
func (f *Font) tofu() *glyph {
	w, h := max(f.ascent*5/9, 3), max(f.ascent*3/4, 4)
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				m.Pix[y*m.Stride+x] = 0xFF
			}
		}
	}
	return &glyph{mask: m, off: image.Pt(1, -h), advance: w + 2}
}

func (f *Font) glyph(r rune) *glyph {
	if g, ok := f.cache[r]; ok {
		return g
	}
	face := f.faceFor(r)
	if face == nil {
		if r == ' ' || r < 0x20 {
			face = f.faces[0]
		} else {
			g := f.tofu()
			f.cache[r] = g
			return g
		}
	}
	g := &glyph{}
	dr, mask, maskp, adv, ok := face.Glyph(fixed.P(0, 0), r)
	g.advance = adv.Round()
	if ok && !dr.Empty() {
		m := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
		draw.Draw(m, m.Bounds(), mask, maskp, draw.Src)
		g.mask, g.off = m, dr.Min
	}
	f.cache[r] = g
	return g
}

// Measure returns the advance width of s in pixels.
func (f *Font) Measure(s string) int {
	w := 0
	for _, r := range s {
		w += f.glyph(r).advance
	}
	return w
}

// Draw renders s with its baseline at y, starting at x, clipped to clip.
// It returns the x after the last glyph.
func (f *Font) Draw(c *Canvas, x, y int, s string, col Color, clip Rect) int {
	clip = clip.Intersect(c.Bounds())
	v := uint32(col) & 0xFFFFFF
	alpha := col.A()
	for _, r := range s {
		g := f.glyph(r)
		if g.mask != nil {
			gx, gy := x+g.off.X, y+g.off.Y
			area := Rect{gx, gy, g.mask.Rect.Dx(), g.mask.Rect.Dy()}.Intersect(clip)
			for py := area.Y; py < area.Bottom(); py++ {
				mrow := g.mask.Pix[(py-gy)*g.mask.Stride:]
				crow := c.Pix[py*c.W:]
				for px := area.X; px < area.Right(); px++ {
					a := uint32(mrow[px-gx]) * alpha / 255
					if a != 0 {
						crow[px] = blend(crow[px], v, a)
					}
				}
			}
		}
		x += g.advance
	}
	return x
}

// Truncate shortens s with a trailing "…" so it fits in maxW pixels.
func (f *Font) Truncate(s string, maxW int) string {
	if f.Measure(s) <= maxW {
		return s
	}
	ell := f.Measure("…")
	w := 0
	for i, r := range s {
		adv := f.glyph(r).advance
		if w+adv+ell > maxW {
			return s[:i] + "…"
		}
		w += adv
	}
	return s
}

// Marquee returns the visible part of s scrolled by offset pixels, for
// focused rows whose text doesn't fit. The text loops with a gap.
func (f *Font) Marquee(s string, offset int) (string, int) {
	loop := s + "     " + s
	skip := 0
	for len(loop) > 0 {
		r, size := utf8.DecodeRuneInString(loop)
		adv := f.glyph(r).advance
		if skip+adv > offset {
			break
		}
		skip += adv
		loop = loop[size:]
	}
	return loop, skip - offset
}
```

`internal/gfx/present.go`:

```go
package gfx

// Display shows frames. Implementations: fbdev (/dev/fb0), headless (tests),
// devview (browser viewer).
type Display interface {
	// Size is the physical pixel size.
	Size() (w, h int)
	// Present shows a full frame of the physical size.
	Present(c *Canvas) error
	Close() error
}

// Scaler maps the logical UI canvas onto the physical framebuffer.
// Framebuffers of 288 lines or fewer are CRT modes: the image fills the
// screen and pixels may be non-square by design (a 320x240 UI on a 640x240
// framebuffer). Taller framebuffers get a uniform nearest-neighbour scale,
// centred with black bars.
type Scaler struct {
	dst        *Canvas
	area       Rect
	xmap, ymap []int
}

// CRTMaxLines is the tallest framebuffer treated as a CRT mode.
const CRTMaxLines = 288

func NewScaler(lw, lh, pw, ph int) *Scaler {
	s := &Scaler{dst: NewCanvas(pw, ph)}
	w, h := pw, ph
	if ph > CRTMaxLines {
		scale := min(float64(pw)/float64(lw), float64(ph)/float64(lh))
		w, h = max(int(float64(lw)*scale), 1), max(int(float64(lh)*scale), 1)
	}
	s.area = Rect{(pw - w) / 2, (ph - h) / 2, w, h}
	s.xmap = make([]int, w)
	for i := range s.xmap {
		s.xmap[i] = i * lw / w
	}
	s.ymap = make([]int, h)
	for i := range s.ymap {
		s.ymap[i] = i * lh / h
	}
	return s
}

// Area is where the logical image lands on the physical canvas.
func (s *Scaler) Area() Rect { return s.area }

// Scale renders src (logical) into the physical canvas and returns it.
func (s *Scaler) Scale(src *Canvas) *Canvas {
	d := s.dst
	for y, sy := range s.ymap {
		srow := src.Pix[sy*src.W : (sy+1)*src.W]
		drow := d.Pix[(s.area.Y+y)*d.W+s.area.X:]
		if y > 0 && s.ymap[y-1] == sy {
			prev := d.Pix[(s.area.Y+y-1)*d.W+s.area.X:]
			copy(drow[:len(s.xmap)], prev[:len(s.xmap)])
			continue
		}
		for x, sx := range s.xmap {
			drow[x] = srow[sx]
		}
	}
	return d
}
```

`internal/gfx/headless.go`:

```go
package gfx

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sync"
)

// Headless is an in-memory Display for tests and screenshots. If Dir is set,
// every presented frame is also written there as frame-NNNNN.png.
type Headless struct {
	W, H int
	Dir  string

	mu     sync.Mutex
	last   *Canvas
	frames int
}

func NewHeadless(w, h int, dir string) *Headless { return &Headless{W: w, H: h, Dir: dir} }

func (d *Headless) Size() (int, int) { return d.W, d.H }
func (d *Headless) Close() error     { return nil }

func (d *Headless) Present(c *Canvas) error {
	cp := &Canvas{W: c.W, H: c.H, Pix: append([]uint32(nil), c.Pix...)}
	d.mu.Lock()
	d.last = cp
	d.frames++
	n := d.frames
	d.mu.Unlock()
	if d.Dir == "" {
		return nil
	}
	return SavePNG(filepath.Join(d.Dir, fmt.Sprintf("frame-%05d.png", n)), cp)
}

// Last returns the most recent frame (nil before the first Present).
func (d *Headless) Last() *Canvas { d.mu.Lock(); defer d.mu.Unlock(); return d.last }

// Frames is how many frames were presented.
func (d *Headless) Frames() int { d.mu.Lock(); defer d.mu.Unlock(); return d.frames }

// SavePNG writes c to path.
func SavePNG(path string, c *Canvas) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, c.ToRGBA()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

- [ ] **Step 5: Run the tests**

Run: `go mod tidy && go vet ./internal/gfx/ && go test -race -count=1 ./internal/gfx/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/gfx
git commit -m "gfx: canvas, images, text with Noto Sans, scaler, headless display" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Framebuffer display

**Files:**
- Create: `internal/gfx/fbpack.go`, `internal/gfx/fbdev_linux.go`
- Test: `internal/gfx/fbpack_test.go`

**Interfaces:**
- Consumes: `Canvas`, `Display` (Task 2).
- Produces: `gfx.OpenFB(path) (*FB, error)`, where `*FB` implements `Display` and adds `Blank()`.
  - The display's size comes from the framebuffer's `xres`×`yres`. It never changes the video mode (spec §9).
  - If mmap on the device fails, it falls back to `/dev/mem` at `smem_start`.

**Notes:**
- The ioctl structs mirror the kernel's. `unsigned long` is written as `uintptr`, so they are 68 bytes on ARMv7 and 80 on amd64; a test pins this.
- The pixel packing is tested against a byte slice, including stride padding and RGB565.

- [ ] **Step 1: Write the failing tests**

`internal/gfx/fbpack_test.go`:

```go
package gfx

import (
	"testing"
	"unsafe"
)

func TestPackXRGB8888WithStride(t *testing.T) {
	f := fbFormat{width: 2, height: 2, stride: 12, bpp: 32} // 4 bytes padding per line
	c := NewCanvas(2, 2)
	c.Pix = []uint32{0x112233, 0x445566, 0x778899, 0xAABBCC}
	mem := make([]byte, 24)
	for i := range mem {
		mem[i] = 0xEE
	}
	f.pack(mem, c)
	want := []byte{0x33, 0x22, 0x11, 0, 0x66, 0x55, 0x44, 0, 0xEE, 0xEE, 0xEE, 0xEE,
		0x99, 0x88, 0x77, 0, 0xCC, 0xBB, 0xAA, 0, 0xEE, 0xEE, 0xEE, 0xEE}
	for i := range want {
		if mem[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x (padding must be untouched)", i, mem[i], want[i])
		}
	}
}

func TestPackRGB565(t *testing.T) {
	f := fbFormat{width: 3, height: 1, stride: 6, bpp: 16}
	c := NewCanvas(3, 1)
	c.Pix = []uint32{0xFF0000, 0x00FF00, 0x0000FF}
	mem := make([]byte, 6)
	f.pack(mem, c)
	got := []uint16{uint16(mem[0]) | uint16(mem[1])<<8, uint16(mem[2]) | uint16(mem[3])<<8, uint16(mem[4]) | uint16(mem[5])<<8}
	if got[0] != 0xF800 || got[1] != 0x07E0 || got[2] != 0x001F {
		t.Fatalf("RGB565 = %04x %04x %04x", got[0], got[1], got[2])
	}
}

func TestFBFormatValidate(t *testing.T) {
	if (fbFormat{640, 480, 2560, 32}).validate() != nil {
		t.Fatal("valid 32bpp rejected")
	}
	if (fbFormat{640, 480, 1280, 24}).validate() == nil {
		t.Fatal("24bpp accepted")
	}
	if (fbFormat{640, 480, 100, 32}).validate() == nil {
		t.Fatal("stride smaller than a line accepted")
	}
}

// The ioctl structs must match the kernel's sizes on this platform.
func TestFBStructSizes(t *testing.T) {
	if s := unsafe.Sizeof(fbVarScreenInfo{}); s != 160 {
		t.Fatalf("fb_var_screeninfo = %d bytes, want 160", s)
	}
	want := uintptr(68) // 32-bit ARM
	if unsafe.Sizeof(uintptr(0)) == 8 {
		want = 80
	}
	if s := unsafe.Sizeof(fbFixScreenInfo{}); s != want {
		t.Fatalf("fb_fix_screeninfo = %d bytes, want %d", s, want)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/gfx/`
Expected: build failure (`undefined: fbFormat`, `undefined: fbVarScreenInfo`).

- [ ] **Step 3: Implement**

`internal/gfx/fbpack.go`:

```go
package gfx

import "fmt"

// fbFormat describes a linear framebuffer's memory layout.
type fbFormat struct {
	width, height int
	stride        int // bytes per line
	bpp           int // 16 (RGB565) or 32 (XRGB8888, little-endian)
}

func (f fbFormat) validate() error {
	if f.bpp != 16 && f.bpp != 32 {
		return fmt.Errorf("gfx: unsupported framebuffer depth %d bpp (need 16 or 32)", f.bpp)
	}
	if f.width <= 0 || f.height <= 0 || f.stride < f.width*f.bpp/8 {
		return fmt.Errorf("gfx: bad framebuffer geometry %dx%d stride %d", f.width, f.height, f.stride)
	}
	return nil
}

// pack writes c (same size as the framebuffer) into mem.
func (f fbFormat) pack(mem []byte, c *Canvas) {
	for y := 0; y < f.height; y++ {
		src := c.Pix[y*c.W : y*c.W+f.width]
		line := mem[y*f.stride:]
		if f.bpp == 32 {
			for x, p := range src {
				o := 4 * x
				line[o] = byte(p)
				line[o+1] = byte(p >> 8)
				line[o+2] = byte(p >> 16)
				line[o+3] = 0
			}
			continue
		}
		for x, p := range src {
			v := uint16(p>>8&0xF800) | uint16(p>>5&0x07E0) | uint16(p>>3&0x001F)
			line[2*x] = byte(v)
			line[2*x+1] = byte(v >> 8)
		}
	}
}
```

`internal/gfx/fbdev_linux.go`:

```go
//go:build linux

package gfx

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
)

type fbBitfield struct{ Offset, Length, MsbRight uint32 }

// fbVarScreenInfo mirrors struct fb_var_screeninfo.
type fbVarScreenInfo struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset uint32
	BitsPerPixel, Grayscale                                uint32
	Red, Green, Blue, Transp                               fbBitfield
	Nonstd, Activate, Height, Width, AccelFlags            uint32
	Pixclock, LeftMargin, RightMargin, UpperMargin         uint32
	LowerMargin, HsyncLen, VsyncLen, Sync, Vmode, Rotate   uint32
	Colorspace                                             uint32
	Reserved                                               [4]uint32
}

// fbFixScreenInfo mirrors struct fb_fix_screeninfo (unsigned long = uintptr).
type fbFixScreenInfo struct {
	ID                             [16]byte
	SmemStart                      uintptr
	SmemLen, Type, TypeAux, Visual uint32
	XPanStep, YPanStep, YWrapStep  uint16
	LineLength                     uint32
	MmioStart                      uintptr
	MmioLen, Accel                 uint32
	Capabilities                   uint16
	Reserved                       [2]uint16
}

// FB is the Linux framebuffer display.
type FB struct {
	f   *os.File
	mem []byte
	fmt fbFormat
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// OpenFB maps the framebuffer device (normally /dev/fb0). If mmap on the
// device fails, it maps the same memory through /dev/mem.
func OpenFB(path string) (*FB, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("gfx: open %s: %w", path, err)
	}
	var v fbVarScreenInfo
	var fx fbFixScreenInfo
	if err := ioctl(f.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&v)); err != nil {
		f.Close()
		return nil, fmt.Errorf("gfx: FBIOGET_VSCREENINFO: %w", err)
	}
	if err := ioctl(f.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&fx)); err != nil {
		f.Close()
		return nil, fmt.Errorf("gfx: FBIOGET_FSCREENINFO: %w", err)
	}
	ff := fbFormat{width: int(v.Xres), height: int(v.Yres), stride: int(fx.LineLength), bpp: int(v.BitsPerPixel)}
	if err := ff.validate(); err != nil {
		f.Close()
		return nil, err
	}
	size := ff.stride * ff.height
	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		mem, err = mmapDevMem(fx.SmemStart, size)
		if err != nil {
			f.Close()
			return nil, err
		}
	}
	return &FB{f: f, mem: mem, fmt: ff}, nil
}

func mmapDevMem(phys uintptr, size int) ([]byte, error) {
	m, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, fmt.Errorf("gfx: fb mmap failed and /dev/mem unavailable: %w", err)
	}
	defer m.Close()
	mem, err := syscall.Mmap(int(m.Fd()), int64(phys), size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("gfx: mmap /dev/mem at %#x: %w", phys, err)
	}
	return mem, nil
}

func (b *FB) Size() (int, int) { return b.fmt.width, b.fmt.height }

func (b *FB) Present(c *Canvas) error {
	if c.W != b.fmt.width || c.H != b.fmt.height {
		return fmt.Errorf("gfx: frame %dx%d != framebuffer %dx%d", c.W, c.H, b.fmt.width, b.fmt.height)
	}
	b.fmt.pack(b.mem, c)
	return nil
}

// Blank clears the framebuffer to black (used on exit).
func (b *FB) Blank() {
	for i := range b.mem {
		b.mem[i] = 0
	}
}

func (b *FB) Close() error {
	b.Blank()
	syscall.Munmap(b.mem)
	return b.f.Close()
}
```

- [ ] **Step 4: Run the tests, and vet for ARM**

Run: `go vet ./internal/gfx/ && GOARCH=arm GOARM=7 go vet ./internal/gfx/ && go test -race -count=1 ./internal/gfx/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/gfx
git commit -m "gfx: Linux framebuffer display (RGB565 and XRGB8888)" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: Input — buttons, key repeat, MiSTer maps, event translation

**Files:**
- Create: `internal/input/input.go`, `internal/input/evmap.go`
- Test: `internal/input/input_test.go`

**Interfaces:**
- Produces:
  - `input.Button` (`BtnUp`, `BtnDown`, `BtnLeft`, `BtnRight`, `BtnA`, `BtnB`, `BtnX`, `BtnY`, `BtnL`, `BtnR`, `BtnSelect`, `BtnStart`)
  - `input.Kind` (`Press`, `Release`, `Repeat`), `input.Event{Button; Kind}`
  - `input.Repeater`, with `Feed(Event, now)`, `NextDeadline() time.Time` and `Due(now) []Event`. It is a pure state machine, so the UI drives it with its own clock.
  - The timing constants `RepeatDelay`, `RepeatRate`, `RepeatFast`, `FastAfter`
  - `input.DefaultKeys` (keyboard plus Linux gamepad defaults, with A on the east face button, SNES layout)
  - `input.ParseMisterMap([]byte) (map[uint16]Button, error)`
  - `input.AbsRange{Min, Max}`
  - an unexported `translator`, which Task 5 uses

**MiSTer map format.** The map is 32 little-endian uint32 slots; the low 16 bits hold the evdev key code. Slot order: right, left, down, up, A, B, X, Y, L, R, select, start. Hi-Fi uses the same layout. Directions on hats and sticks are handled generically: a hat is ±1, and a stick past half its range.

- [ ] **Step 1: Write the failing tests**

`internal/input/input_test.go`:

```go
package input

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestRepeaterTiming(t *testing.T) {
	var r Repeater
	t0 := time.Unix(1000, 0)
	r.Feed(Event{BtnDown, Press}, t0)
	if got := r.Due(t0.Add(349 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("repeat before the 350 ms delay: %v", got)
	}
	if got := r.Due(t0.Add(350 * time.Millisecond)); len(got) != 1 || got[0] != (Event{BtnDown, Repeat}) {
		t.Fatalf("first repeat = %v", got)
	}
	// 5 more at 110 ms, then 45 ms.
	at := t0.Add(350 * time.Millisecond)
	for i := 0; i < 5; i++ {
		at = at.Add(RepeatRate)
		if n := len(r.Due(at)); n != 1 {
			t.Fatalf("repeat %d: %d events", i+2, n)
		}
	}
	at = at.Add(RepeatFast)
	if n := len(r.Due(at)); n != 1 {
		t.Fatalf("fast repeat: %d events", n)
	}
	r.Feed(Event{BtnDown, Release}, at)
	if !r.NextDeadline().IsZero() || len(r.Due(at.Add(time.Second))) != 0 {
		t.Fatal("repeats continued after release")
	}
}

func TestRepeaterOnlyNavigationButtons(t *testing.T) {
	var r Repeater
	t0 := time.Unix(0, 0)
	r.Feed(Event{BtnA, Press}, t0)
	if len(r.Due(t0.Add(time.Second))) != 0 {
		t.Fatal("A must not auto-repeat")
	}
	r.Feed(Event{BtnUp, Press}, t0)
	r.Feed(Event{BtnDown, Press}, t0) // the newer button takes over
	r.Feed(Event{BtnUp, Release}, t0) // releasing the old one changes nothing
	if got := r.Due(t0.Add(RepeatDelay)); len(got) != 1 || got[0].Button != BtnDown {
		t.Fatalf("got %v, want a Down repeat", got)
	}
}

func TestParseMisterMap(t *testing.T) {
	b := make([]byte, 128)
	put := func(slot int, v uint32) { binary.LittleEndian.PutUint32(b[4*slot:], v) }
	put(4, btnSouth) // A on the south button (user swapped)
	put(5, btnEast)  // B on east
	put(11, 0x13b)   // start
	put(0, 0x10000)  // an axis entry (no key code in the low bits)
	put(7, 0x305)    // above KEY_MAX-ish range: skipped
	m, err := ParseMisterMap(b)
	if err != nil {
		t.Fatal(err)
	}
	if m[btnSouth] != BtnA || m[btnEast] != BtnB || m[0x13b] != BtnStart {
		t.Fatalf("map = %v", m)
	}
	if len(m) != 3 {
		t.Fatalf("map has %d entries, want 3: %v", len(m), m)
	}
	if _, err := ParseMisterMap(b[:10]); err == nil {
		t.Fatal("short map accepted")
	}
}

func TestTranslatorKeysAndMapOverride(t *testing.T) {
	tr := newTranslator(map[uint16]Button{btnSouth: BtnA}, nil)
	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{BtnA, Press}) {
		t.Fatalf("mapped south = %v", got)
	}
	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{BtnA, Press}) {
		t.Fatalf("default east = %v", got)
	}
	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{BtnA, Release}) {
		t.Fatalf("enter release = %v", got)
	}
	if got := tr.handle(evKey, keyDown, 2); got != nil {
		t.Fatalf("kernel autorepeat must be ignored, got %v", got)
	}
	if got := tr.handle(evKey, 0x2fe, 1); got != nil {
		t.Fatalf("unknown key produced %v", got)
	}
}

func TestTranslatorHatAndStick(t *testing.T) {
	tr := newTranslator(nil, map[uint16]AbsRange{absX: {0, 255}})
	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{BtnUp, Press}) {
		t.Fatalf("hat up = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{BtnUp, Release}) || got[1] != (Event{BtnDown, Press}) {
		t.Fatalf("hat up→down = %v", got)
	}
	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{BtnDown, Release}) {
		t.Fatalf("hat centre = %v", got)
	}
	if got := tr.handle(evAbs, absX, 150); got != nil {
		t.Fatalf("small stick movement = %v", got)
	}
	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{BtnRight, Press}) {
		t.Fatalf("stick right = %v", got)
	}
	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{BtnRight, Release}) {
		t.Fatalf("stick centre = %v", got)
	}
	if got := tr.handle(evAbs, absY, 0); got != nil {
		t.Fatalf("uncalibrated axis = %v", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/input/`
Expected: build failure.

- [ ] **Step 3: Implement**

`internal/input/input.go`:

```go
// Package input turns controllers and keyboards into UI buttons: it reads
// evdev devices (grabbing them so MiSTer Main doesn't react), applies the
// user's MiSTer controller maps, and generates key repeat for navigation.
package input

import "time"

// Button is a logical controller button.
type Button int

const (
	BtnNone Button = iota
	BtnUp
	BtnDown
	BtnLeft
	BtnRight
	BtnA
	BtnB
	BtnX
	BtnY
	BtnL
	BtnR
	BtnSelect
	BtnStart
)

var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start"}

func (b Button) String() string {
	if int(b) < len(buttonNames) {
		return buttonNames[b]
	}
	return "?"
}

// Kind is what happened to a button.
type Kind int

const (
	Press Kind = iota + 1
	Release
	Repeat // generated while a navigation button is held
)

// Event is one button transition.
type Event struct {
	Button Button
	Kind   Kind
}

// Repeat timing (spec §8.4).
const (
	RepeatDelay = 350 * time.Millisecond
	RepeatRate  = 110 * time.Millisecond
	RepeatFast  = 45 * time.Millisecond
	FastAfter   = 6 // repeats before switching to RepeatFast
)

// repeats reports whether b auto-repeats when held.
func repeats(b Button) bool {
	switch b {
	case BtnUp, BtnDown, BtnLeft, BtnRight, BtnL, BtnR:
		return true
	}
	return false
}

// Repeater generates Repeat events for held navigation buttons. It is a pure
// state machine driven by the caller's clock: Feed each Press/Release, call
// Due whenever NextDeadline passes.
type Repeater struct {
	held  Button
	next  time.Time
	count int
}

// Feed records a press or release. Pressing another button replaces the held one.
func (r *Repeater) Feed(e Event, now time.Time) {
	switch e.Kind {
	case Press:
		if repeats(e.Button) {
			r.held, r.next, r.count = e.Button, now.Add(RepeatDelay), 0
		} else {
			r.held = BtnNone
		}
	case Release:
		if e.Button == r.held {
			r.held = BtnNone
		}
	}
}

// NextDeadline is when Due should be called next (zero if nothing is held).
func (r *Repeater) NextDeadline() time.Time {
	if r.held == BtnNone {
		return time.Time{}
	}
	return r.next
}

// Due returns the repeats that are due at now.
func (r *Repeater) Due(now time.Time) []Event {
	var out []Event
	for r.held != BtnNone && !now.Before(r.next) {
		out = append(out, Event{Button: r.held, Kind: Repeat})
		r.count++
		step := RepeatRate
		if r.count >= FastAfter {
			step = RepeatFast
		}
		r.next = r.next.Add(step)
	}
	return out
}
```

`internal/input/evmap.go`:

```go
package input

import (
	"encoding/binary"
	"fmt"
)

// Linux input event types and codes used here.
const (
	evKey = 0x01
	evAbs = 0x03

	absX     = 0x00
	absY     = 0x01
	absHat0X = 0x10
	absHat0Y = 0x11
)

// Keyboard and gamepad key codes (linux/input-event-codes.h).
const (
	keyEsc       = 1
	keyBackspace = 14
	keyTab       = 15
	keyQ         = 16
	keyEnter     = 28
	keyN         = 49
	keySpace     = 57
	keyPageUp    = 104
	keyUp        = 103
	keyLeft      = 105
	keyRight     = 106
	keyPageDown  = 109
	keyDown      = 108
	keyKPEnter   = 96

	btnSouth  = 0x130
	btnEast   = 0x131
	btnNorth  = 0x133
	btnWest   = 0x134
	btnTL     = 0x136
	btnTR     = 0x137
	btnSelect = 0x13a
	btnStart  = 0x13b
	btnDpadUp = 0x220
	btnDpadDn = 0x221
	btnDpadL  = 0x222
	btnDpadR  = 0x223
)

// DefaultKeys is the built-in key-code map: keyboard (spec §8.3) plus Linux
// gamepad defaults. SNES layout: A is the east face button, B the south one.
var DefaultKeys = map[uint16]Button{
	keyUp: BtnUp, keyDown: BtnDown, keyLeft: BtnLeft, keyRight: BtnRight,
	keyEnter: BtnA, keyKPEnter: BtnA, keyEsc: BtnB, keyBackspace: BtnB,
	keyTab: BtnX, keySpace: BtnStart, keyPageUp: BtnL, keyPageDown: BtnR,
	keyN: BtnY, keyQ: BtnY,

	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
	btnDpadUp: BtnUp, btnDpadDn: BtnDown, btnDpadL: BtnLeft, btnDpadR: BtnRight,
}

// MiSTer .map slots (Main_MiSTer): 32 little-endian uint32, low 16 bits = key code.
var misterSlots = [...]Button{BtnRight, BtnLeft, BtnDown, BtnUp, BtnA, BtnB, BtnX, BtnY, BtnL, BtnR, BtnSelect, BtnStart}

// ParseMisterMap reads an input_VID_PID_v3.map file into a key-code map.
// Slots without a key code (0, or axis entries above 0x2ff) are skipped;
// directions on axes/hats are handled generically.
func ParseMisterMap(b []byte) (map[uint16]Button, error) {
	if len(b) < 4*len(misterSlots) {
		return nil, fmt.Errorf("input: map file too short (%d bytes)", len(b))
	}
	m := map[uint16]Button{}
	for i, btn := range misterSlots {
		code := uint16(binary.LittleEndian.Uint32(b[4*i:]) & 0xFFFF)
		if code != 0 && code <= 0x2ff {
			m[code] = btn
		}
	}
	return m, nil
}

// AbsRange is an absolute axis's calibration (from EVIOCGABS).
type AbsRange struct{ Min, Max int32 }

// translator converts one device's raw events into button events.
type translator struct {
	keys  map[uint16]Button
	abs   map[uint16]AbsRange
	state map[uint16]int // axis -> -1, 0, +1
}

func newTranslator(keys map[uint16]Button, abs map[uint16]AbsRange) *translator {
	return &translator{keys: keys, abs: abs, state: map[uint16]int{}}
}

// handle converts a raw event. value: 1 press, 0 release, 2 kernel autorepeat (ignored).
func (t *translator) handle(typ, code uint16, value int32) []Event {
	switch typ {
	case evKey:
		b, ok := t.keys[code]
		if !ok {
			b, ok = DefaultKeys[code]
		}
		if !ok || value == 2 {
			return nil
		}
		if value == 1 {
			return []Event{{b, Press}}
		}
		return []Event{{b, Release}}
	case evAbs:
		var neg, pos Button
		switch code {
		case absX, absHat0X:
			neg, pos = BtnLeft, BtnRight
		case absY, absHat0Y:
			neg, pos = BtnUp, BtnDown
		default:
			return nil
		}
		dir := 0
		if code == absHat0X || code == absHat0Y {
			if value < 0 {
				dir = -1
			} else if value > 0 {
				dir = 1
			}
		} else {
			r, ok := t.abs[code]
			if !ok || r.Max <= r.Min {
				return nil
			}
			center, half := (r.Min+r.Max)/2, (r.Max-r.Min)/2
			if value <= center-half/2 {
				dir = -1
			} else if value >= center+half/2 {
				dir = 1
			}
		}
		prev := t.state[code]
		if dir == prev {
			return nil
		}
		t.state[code] = dir
		var out []Event
		switch prev {
		case -1:
			out = append(out, Event{neg, Release})
		case 1:
			out = append(out, Event{pos, Release})
		}
		switch dir {
		case -1:
			out = append(out, Event{neg, Press})
		case 1:
			out = append(out, Event{pos, Press})
		}
		return out
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/input/ && go test -race -count=1 ./internal/input/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/input
git commit -m "input: buttons, key repeat, MiSTer maps and event translation" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: Input — evdev device manager

**Files:**
- Create: `internal/input/evdev_linux.go`
- Test: `internal/input/evdev_linux_test.go`

**Interfaces:**
- Consumes: `translator`, `ParseMisterMap`, `AbsRange`, `Event` (Task 4).
- Produces:
  - `input.ManagerOptions{Glob, MapDir string; Grab bool; Rescan time.Duration}`, with the defaults `/dev/input/event*`, `/media/fat/config/inputs` and 2 s.
  - `input.NewManager(o) *Manager`, with `Events() <-chan Event` and `Close()`. Close releases every grab.

**Behaviour** (spec §8.4):
- **Grabbing:** the manager grabs each keyboard or gamepad device (`EVIOCGRAB`) so MiSTer Main ignores it.
- **Skipped devices:** anything without key or abs events, and the "MiSTer virtual input" echo device.
- **Controller maps:** it loads the user's `input_VID_PID_v3.map` (VID/PID read from sysfs), or a suffixed variant.
- **Hotplug:** it rescans every 2 s. An unplugged device is dropped and picked up again when it comes back.

`input_event` is decoded with C `long` timeval fields, which is Go's `int` on Linux: 16 bytes on ARMv7, 24 on amd64. `EVIOCGRAB` takes its flag by value (`ioctlVal`).

- [ ] **Step 1: Write the failing tests**

`internal/input/evdev_linux_test.go`:

```go
//go:build linux

package input

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestIoctlNumbers(t *testing.T) {
	// Values from <linux/input.h> as compiled by gcc.
	if eviocgrab != 0x40044590 {
		t.Fatalf("EVIOCGRAB = %#x", eviocgrab)
	}
	if got := eviocgname(256); got != 0x81004506 {
		t.Fatalf("EVIOCGNAME(256) = %#x", got)
	}
	if got := eviocgabs(absX); got != 0x80184540 {
		t.Fatalf("EVIOCGABS(ABS_X) = %#x", got)
	}
	if got := eviocgbit(0, 4); got != 0x80044520 {
		t.Fatalf("EVIOCGBIT(0,4) = %#x", got)
	}
}

func TestDecodeEvents(t *testing.T) {
	evs := []rawEvent{{Type: evKey, Code: keyEnter, Value: 1}, {Type: evAbs, Code: absHat0X, Value: -1}}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&evs[0])), 2*rawEventSize)
	got := decodeEvents(append(b, 1, 2, 3)) // a trailing partial event is ignored
	if len(got) != 2 || got[0].Code != keyEnter || got[1].Value != -1 {
		t.Fatalf("decoded %+v", got)
	}
}

func TestFindMapFileAndIDs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "input_045e_028e_v3.map"), make([]byte, 128), 0o644)
	os.WriteFile(filepath.Join(dir, "input_2dc8_6001_btaddr_v3.map"), make([]byte, 128), 0o644)
	if got := findMapFile(dir, 0x045e, 0x028e); filepath.Base(got) != "input_045e_028e_v3.map" {
		t.Fatalf("exact map = %q", got)
	}
	if got := findMapFile(dir, 0x2dc8, 0x6001); filepath.Base(got) != "input_2dc8_6001_btaddr_v3.map" {
		t.Fatalf("suffixed map = %q", got)
	}
	if findMapFile(dir, 1, 2) != "" {
		t.Fatal("found a map for an unknown device")
	}
	ids := t.TempDir()
	os.WriteFile(filepath.Join(ids, "vendor"), []byte("045e\n"), 0o644)
	os.WriteFile(filepath.Join(ids, "product"), []byte("028e\n"), 0o644)
	if v, p, ok := readIDs(ids); !ok || v != 0x045e || p != 0x028e {
		t.Fatalf("ids = %x %x %v", v, p, ok)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/input/`
Expected: build failure (`undefined: eviocgrab`, `undefined: decodeEvents`, …).

- [ ] **Step 3: Implement**

`internal/input/evdev_linux.go`:

```go
//go:build linux

package input

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// rawEvent mirrors struct input_event; the timeval fields are C longs, which
// match Go's int on Linux (4 bytes on ARMv7, 8 on amd64).
type rawEvent struct {
	Sec, Usec int
	Type      uint16
	Code      uint16
	Value     int32
}

const rawEventSize = int(unsafe.Sizeof(rawEvent{}))

// ioctl request numbers (asm-generic encoding: dir<<30 | size<<16 | type<<8 | nr).
const (
	iocRead  = 2
	iocWrite = 1
)

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'E'<<8 | nr }

var (
	eviocgrab = ioc(iocWrite, 0x90, 4)
)

func eviocgname(n uintptr) uintptr    { return ioc(iocRead, 0x06, n) }
func eviocgbit(ev, n uintptr) uintptr { return ioc(iocRead, 0x20+ev, n) }
func eviocgabs(abs uintptr) uintptr   { return ioc(iocRead, 0x40+abs, 24) }

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// ioctlVal passes an integer argument (EVIOCGRAB takes the grab flag by value).
func ioctlVal(fd, req, val uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, val); e != 0 {
		return e
	}
	return nil
}

// ManagerOptions configures device handling.
type ManagerOptions struct {
	// Glob of device nodes; default /dev/input/event*.
	Glob string
	// MapDir holds MiSTer controller maps; default /media/fat/config/inputs.
	MapDir string
	// Grab takes devices exclusively so MiSTer Main ignores them.
	Grab bool
	// Rescan interval for hotplugged devices; default 2 s.
	Rescan time.Duration
}

// Manager reads all input devices and delivers button events.
type Manager struct {
	o      ManagerOptions
	events chan Event
	quit   chan struct{}
	wg     sync.WaitGroup

	mu   sync.Mutex
	devs map[string]*os.File
}

// NewManager starts scanning immediately.
func NewManager(o ManagerOptions) *Manager {
	if o.Glob == "" {
		o.Glob = "/dev/input/event*"
	}
	if o.MapDir == "" {
		o.MapDir = "/media/fat/config/inputs"
	}
	if o.Rescan <= 0 {
		o.Rescan = 2 * time.Second
	}
	m := &Manager{o: o, events: make(chan Event, 64), quit: make(chan struct{}), devs: map[string]*os.File{}}
	m.scan()
	m.wg.Add(1)
	go m.rescanLoop()
	return m
}

func (m *Manager) Events() <-chan Event { return m.events }

// Close releases every grab and stops all readers.
func (m *Manager) Close() {
	close(m.quit)
	m.mu.Lock()
	for path, f := range m.devs {
		if f == nil {
			delete(m.devs, path)
			continue
		}
		if m.o.Grab {
			ioctlVal(f.Fd(), eviocgrab, 0)
		}
		f.Close()
		delete(m.devs, path)
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) rescanLoop() {
	defer m.wg.Done()
	t := time.NewTicker(m.o.Rescan)
	defer t.Stop()
	for {
		select {
		case <-m.quit:
			return
		case <-t.C:
			m.scan()
		}
	}
}

func (m *Manager) scan() {
	paths, _ := filepath.Glob(m.o.Glob)
	for _, p := range paths {
		m.mu.Lock()
		_, known := m.devs[p]
		m.mu.Unlock()
		if !known {
			m.open(p)
		}
	}
}

func deviceName(f *os.File) string {
	buf := make([]byte, 256)
	if err := ioctl(f.Fd(), eviocgname(uintptr(len(buf))), unsafe.Pointer(&buf[0])); err != nil {
		return ""
	}
	return string(bytes.TrimRight(buf, "\x00"))
}

func hasEventType(f *os.File, typ uint) bool {
	var bits [4]byte
	if err := ioctl(f.Fd(), eviocgbit(0, uintptr(len(bits))), unsafe.Pointer(&bits[0])); err != nil {
		return false
	}
	return bits[typ/8]&(1<<(typ%8)) != 0
}

func absRange(f *os.File, code uint16) (AbsRange, bool) {
	var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
	if err := ioctl(f.Fd(), eviocgabs(uintptr(code)), unsafe.Pointer(&info[0])); err != nil {
		return AbsRange{}, false
	}
	return AbsRange{Min: info[1], Max: info[2]}, true
}

func (m *Manager) open(path string) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	name := deviceName(f)
	if strings.Contains(name, "MiSTer virtual input") || !(hasEventType(f, evKey) || hasEventType(f, evAbs)) {
		f.Close()
		m.mu.Lock()
		m.devs[path] = nil // remember so we don't reopen it every rescan
		m.mu.Unlock()
		return
	}
	keys := map[uint16]Button{}
	if vid, pid, ok := sysfsIDs(path); ok {
		if mp := findMapFile(m.o.MapDir, vid, pid); mp != "" {
			if b, err := os.ReadFile(mp); err == nil {
				if km, err := ParseMisterMap(b); err == nil {
					keys = km
				}
			}
		}
	}
	abs := map[uint16]AbsRange{}
	for _, c := range []uint16{absX, absY} {
		if r, ok := absRange(f, c); ok {
			abs[c] = r
		}
	}
	if m.o.Grab {
		if err := ioctlVal(f.Fd(), eviocgrab, 1); err != nil {
			log.Printf("input: grab %s (%s): %v", path, name, err)
		}
	}
	m.mu.Lock()
	m.devs[path] = f
	m.mu.Unlock()
	m.wg.Add(1)
	go m.read(path, f, newTranslator(keys, abs))
}

func (m *Manager) read(path string, f *os.File, tr *translator) {
	defer m.wg.Done()
	buf := make([]byte, rawEventSize*32)
	for {
		n, err := f.Read(buf)
		if err != nil {
			m.mu.Lock()
			if m.devs[path] == f {
				delete(m.devs, path) // unplugged: rescan may pick it up again
				f.Close()
			}
			m.mu.Unlock()
			return
		}
		for _, ev := range decodeEvents(buf[:n]) {
			for _, e := range tr.handle(ev.Type, ev.Code, ev.Value) {
				select {
				case m.events <- e:
				case <-m.quit:
					return
				}
			}
		}
	}
}

func decodeEvents(b []byte) []rawEvent {
	var out []rawEvent
	for len(b) >= rawEventSize {
		out = append(out, *(*rawEvent)(unsafe.Pointer(&b[0])))
		b = b[rawEventSize:]
	}
	return out
}

// sysfsIDs reads the vendor/product of /dev/input/eventN.
func sysfsIDs(devPath string) (vid, pid uint16, ok bool) {
	return readIDs(filepath.Join("/sys/class/input", filepath.Base(devPath), "device/id"))
}

func readIDs(dir string) (vid, pid uint16, ok bool) {
	read := func(name string) (uint16, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
		return uint16(v), err == nil
	}
	v, ok1 := read("vendor")
	p, ok2 := read("product")
	return v, p, ok1 && ok2
}

// findMapFile locates MiSTer's map for vid:pid, preferring the exact name.
func findMapFile(dir string, vid, pid uint16) string {
	exact := filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_v3.map", vid, pid))
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	matches, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("input_%04x_%04x_*_v3.map", vid, pid)))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}
```

- [ ] **Step 4: Run the tests, and vet for ARM**

Run: `go vet ./internal/input/ && GOARCH=arm GOARM=7 go vet ./internal/input/ && go test -race -count=1 ./internal/input/`
Expected: `ok`. The ioctl numbers are pinned to the values `<linux/input.h>` produces.

- [ ] **Step 5: Commit**

```bash
git add internal/input
git commit -m "input: evdev manager with grabs, MiSTer maps and hotplug" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: Cover art — disk LRU and loader

**Files:**
- Create: `internal/cache/cache.go`, `internal/art/art.go`
- Test: `internal/cache/cache_test.go`, `internal/art/art_test.go`

**Interfaces:**
- Consumes:
  - `subsonic.Client.CoverArtURL/HTTPClient`, `subsonic.ID` (Plan 1)
  - `gfx.DecodeImage`, `gfx.Image` (Task 2)
- Produces:
  - `cache.Open(dir, maxBytes) (*Disk, error)`, with `Get(key) ([]byte, bool)`, `Put(key, data) error` and `Size()`. Files are named by SHA-1; recency is the mtime, refreshed on hits, because exFAT has no reliable atime. `maxBytes <= 0` disables the cache.
  - `art.Key{ID subsonic.ID; Size int}`, `art.Fetcher`, `art.HTTPFetcher(*subsonic.Client)`
  - `art.Options{Fetch; Disk; MemBytes (48 MiB); Workers (2); Ready func(Key); RetryAfter (1 min); Now}`
  - `art.New(o) *Loader`, with `Get(Key) (*gfx.Image, bool)` and `Close()`

**Behaviour:**
- **Get** never blocks. On a miss it queues a load, once. The queue is a stack, so the rows on screen now are served first.
- **Ready** fires after each load, successful or failed.
- **Failures** aren't retried for `RetryAfter`.
- **Error documents:** `HTTPFetcher` rejects non-`image/*` responses, which covers Subsonic JSON error documents. Its errors never include the URL.

- [ ] **Step 1: Write the failing tests**

`internal/cache/cache_test.go`:

```go
package cache

import (
	"bytes"
	"testing"
	"time"
)

func TestPutGetAndLRUEviction(t *testing.T) {
	dir := t.TempDir()
	d, err := Open(dir, 25)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	d.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	d.Put("a", bytes.Repeat([]byte{1}, 10))
	d.Put("b", bytes.Repeat([]byte{2}, 10))
	if _, ok := d.Get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	d.Put("c", bytes.Repeat([]byte{3}, 10)) // over budget: evict b (oldest)
	if _, ok := d.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := d.Get(k); !ok {
			t.Fatalf("%s evicted wrongly", k)
		}
	}
	if d.Size() != 20 {
		t.Fatalf("size = %d, want 20", d.Size())
	}
	// A reopened cache sees the same size.
	d2, _ := Open(dir, 25)
	if d2.Size() != 20 {
		t.Fatalf("reopened size = %d", d2.Size())
	}
	if got, ok := d2.Get("c"); !ok || got[0] != 3 {
		t.Fatal("c not readable after reopen")
	}
}

func TestOverwriteAndOversizeAndDisabled(t *testing.T) {
	d, _ := Open(t.TempDir(), 100)
	d.Put("k", make([]byte, 40))
	d.Put("k", make([]byte, 10))
	if d.Size() != 10 {
		t.Fatalf("size after overwrite = %d", d.Size())
	}
	d.Put("huge", make([]byte, 1000))
	if _, ok := d.Get("huge"); ok {
		t.Fatal("item larger than the budget was stored")
	}
	off, _ := Open(t.TempDir(), 0)
	off.Put("x", []byte("y"))
	if _, ok := off.Get("x"); ok {
		t.Fatal("disabled cache stored data")
	}
}
```

`internal/art/art_test.go`:

```go
package art

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/subsonic"
)

func pngBytes(w, h int) []byte {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = 0xFF
	}
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

type harness struct {
	l     *Loader
	calls atomic.Int32
	ready chan Key
}

func newHarness(t *testing.T, fetch func(Key) ([]byte, error), o Options) *harness {
	h := &harness{ready: make(chan Key, 64)}
	o.Fetch = func(_ context.Context, k Key) ([]byte, error) { h.calls.Add(1); return fetch(k) }
	o.Ready = func(k Key) { h.ready <- k }
	h.l = New(o)
	t.Cleanup(h.l.Close)
	return h
}

func (h *harness) waitReady(t *testing.T) Key {
	t.Helper()
	select {
	case k := <-h.ready:
		return k
	case <-time.After(2 * time.Second):
		t.Fatal("no Ready callback")
	}
	return Key{}
}

func TestLoadDecodesScalesAndCaches(t *testing.T) {
	disk, _ := cache.Open(t.TempDir(), 1<<20)
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(600, 300), nil }, Options{Disk: disk})
	k := Key{"al-1", 100}
	if _, ok := h.l.Get(k); ok {
		t.Fatal("first Get hit")
	}
	h.l.Get(k) // a second request while loading must not fetch twice
	if h.waitReady(t) != k {
		t.Fatal("wrong key ready")
	}
	img, ok := h.l.Get(k)
	if !ok || img.W != 100 || img.H != 50 {
		t.Fatalf("image = %v %v", img, ok)
	}
	if h.calls.Load() != 1 {
		t.Fatalf("fetched %d times", h.calls.Load())
	}
	// A fresh loader over the same disk cache doesn't hit the network.
	h2 := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("offline") }, Options{Disk: disk})
	h2.l.Get(k)
	h2.waitReady(t)
	if _, ok := h2.l.Get(k); !ok || h2.calls.Load() != 0 {
		t.Fatalf("disk cache not used (calls %d)", h2.calls.Load())
	}
}

func TestFailuresAreNotRetriedImmediately(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	h := newHarness(t, func(Key) ([]byte, error) { return nil, errors.New("404") }, Options{
		Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})
	k := Key{"al-x", 64}
	h.l.Get(k)
	h.waitReady(t)
	h.l.Get(k)
	time.Sleep(20 * time.Millisecond)
	if h.calls.Load() != 1 {
		t.Fatalf("retried immediately (%d calls)", h.calls.Load())
	}
	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	h.l.Get(k)
	h.waitReady(t)
	if h.calls.Load() != 2 {
		t.Fatalf("not retried after RetryAfter (%d calls)", h.calls.Load())
	}
}

func TestMemoryBudgetEvictsLRU(t *testing.T) {
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(10, 10), nil }, Options{MemBytes: 900, Workers: 1})
	for _, id := range []subsonic.ID{"a", "b", "c"} { // 400 bytes each decoded
		h.l.Get(Key{id, 10})
		h.waitReady(t)
	}
	if _, ok := h.l.Get(Key{"a", 10}); ok {
		t.Fatal("a should have been evicted")
	}
	if _, ok := h.l.Get(Key{"c", 10}); !ok {
		t.Fatal("c missing")
	}
}

func TestEmptyIDNeverLoads(t *testing.T) {
	h := newHarness(t, func(Key) ([]byte, error) { return pngBytes(1, 1), nil }, Options{})
	h.l.Get(Key{"", 100})
	time.Sleep(20 * time.Millisecond)
	if h.calls.Load() != 0 {
		t.Fatal("fetched an empty cover id")
	}
}

func TestHTTPFetcherRejectsNonImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "ok" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes(2, 2))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"subsonic-response":{"status":"failed"}}`))
	}))
	defer srv.Close()
	c, _ := subsonic.New(subsonic.Options{BaseURL: srv.URL, Credentials: subsonic.Credentials{Username: "u", Password: "secret-pw"}})
	f := HTTPFetcher(c)
	if b, err := f(context.Background(), Key{"ok", 2}); err != nil || len(b) == 0 {
		t.Fatalf("image fetch: %v", err)
	}
	_, err := f(context.Background(), Key{"missing", 2})
	if err == nil {
		t.Fatal("JSON error document accepted as an image")
	}
	if bytes.Contains([]byte(err.Error()), []byte("t=")) {
		t.Fatalf("error leaks the URL: %v", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cache/ ./internal/art/`
Expected: build failures.

- [ ] **Step 3: Implement**

`internal/cache/cache.go`:

```go
// Package cache is a small on-disk LRU for cover art bytes. Entries are
// files named by a hash of the key; recency is the file's mtime, which is
// refreshed on every hit (the SD card's exFAT has no reliable atime).
package cache

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Disk struct {
	dir string
	max int64
	now func() time.Time

	mu   sync.Mutex
	size int64 // bytes on disk, maintained incrementally
}

// Open uses dir (created if needed) with a byte budget. maxBytes <= 0 disables caching.
func Open(dir string, maxBytes int64) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	d := &Disk{dir: dir, max: maxBytes, now: time.Now}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			d.size += info.Size()
		}
	}
	return d, nil
}

func (d *Disk) path(key string) string {
	h := sha1.Sum([]byte(key))
	return filepath.Join(d.dir, hex.EncodeToString(h[:]))
}

// Get returns the cached bytes for key.
func (d *Disk) Get(key string) ([]byte, bool) {
	if d.max <= 0 {
		return nil, false
	}
	p := d.path(key)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	t := d.now()
	os.Chtimes(p, t, t)
	return b, true
}

// Put stores data under key, evicting least-recently-used entries to stay
// within the budget. Items larger than the whole budget are not stored.
func (d *Disk) Put(key string, data []byte) error {
	if d.max <= 0 || int64(len(data)) > d.max {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.path(key)
	var old int64
	if info, err := os.Stat(p); err == nil {
		old = info.Size()
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	t := d.now()
	os.Chtimes(p, t, t)
	d.size += int64(len(data)) - old
	if d.size > d.max {
		d.evictLocked(p)
	}
	return nil
}

// Size is the bytes currently cached.
func (d *Disk) Size() int64 { d.mu.Lock(); defer d.mu.Unlock(); return d.size }

func (d *Disk) evictLocked(keep string) {
	type ent struct {
		path string
		size int64
		mod  time.Time
	}
	var all []ent
	filepath.WalkDir(d.dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || p == keep {
			return nil
		}
		if info, err := e.Info(); err == nil {
			all = append(all, ent{p, info.Size(), info.ModTime()})
		}
		return nil
	})
	sort.Slice(all, func(i, j int) bool { return all[i].mod.Before(all[j].mod) })
	for _, e := range all {
		if d.size <= d.max {
			return
		}
		if err := os.Remove(e.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			d.size -= e.size
		}
	}
}
```

`internal/art/art.go`:

```go
// Package art loads album cover images for the UI: memory LRU of decoded
// images, then the on-disk cache, then the server's getCoverArt. Loads run
// on background workers, newest request first (what's on screen now).
package art

import (
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// Key identifies one cover at one pixel size (the server resizes).
type Key struct {
	ID   subsonic.ID
	Size int
}

func (k Key) String() string { return fmt.Sprintf("%s@%d", k.ID, k.Size) }

// Fetcher downloads encoded image bytes.
type Fetcher func(ctx context.Context, k Key) ([]byte, error)

// HTTPFetcher fetches from the server through c's authenticated URL.
func HTTPFetcher(c *subsonic.Client) Fetcher {
	return func(ctx context.Context, k Key) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.CoverArtURL(k.ID, k.Size), nil)
		if err != nil {
			return nil, fmt.Errorf("art: %s: bad request", k)
		}
		resp, err := c.HTTPClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("art: %s: request failed", k) // never echo the URL (credentials)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			return nil, fmt.Errorf("art: %s: HTTP %d %s", k, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	}
}

type Options struct {
	Fetch    Fetcher
	Disk     *cache.Disk // may be nil
	MemBytes int64       // decoded-image budget; default 48 MiB
	Workers  int         // default 2
	// Ready is called (from a worker goroutine) when a requested image
	// becomes available or fails; the UI uses it to schedule a redraw.
	Ready func(Key)
	// RetryAfter is how long a failed key is not retried; default 1 min.
	RetryAfter time.Duration
	Now        func() time.Time
}

type entry struct {
	key   Key
	img   *gfx.Image
	bytes int64
}

type Loader struct {
	o      Options
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	cond     *sync.Cond
	lru      *list.List // front = most recent
	items    map[Key]*list.Element
	memBytes int64
	pending  []Key // stack: newest last
	inflight map[Key]bool
	failed   map[Key]time.Time
	closed   bool
}

func New(o Options) *Loader {
	if o.MemBytes <= 0 {
		o.MemBytes = 48 << 20
	}
	if o.Workers <= 0 {
		o.Workers = 2
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	l := &Loader{o: o, lru: list.New(), items: map[Key]*list.Element{}, inflight: map[Key]bool{}, failed: map[Key]time.Time{}}
	l.cond = sync.NewCond(&l.mu)
	l.ctx, l.cancel = context.WithCancel(context.Background())
	for i := 0; i < o.Workers; i++ {
		l.wg.Add(1)
		go l.worker()
	}
	return l
}

// Get returns the image if it is in memory; otherwise it queues a load (once)
// and returns false. An empty ID never loads.
func (l *Loader) Get(k Key) (*gfx.Image, bool) {
	if k.ID == "" || k.Size <= 0 {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[k]; ok {
		l.lru.MoveToFront(el)
		return el.Value.(*entry).img, true
	}
	if l.closed || l.inflight[k] {
		return nil, false
	}
	if t, ok := l.failed[k]; ok && l.o.Now().Sub(t) < l.o.RetryAfter {
		return nil, false
	}
	for i, p := range l.pending {
		if p == k { // already queued: move to the top of the stack
			l.pending = append(append(l.pending[:i:i], l.pending[i+1:]...), k)
			return nil, false
		}
	}
	l.pending = append(l.pending, k)
	l.cond.Signal()
	return nil, false
}

// Close stops the workers.
func (l *Loader) Close() {
	l.mu.Lock()
	l.closed = true
	l.cond.Broadcast()
	l.mu.Unlock()
	l.cancel()
	l.wg.Wait()
}

func (l *Loader) worker() {
	defer l.wg.Done()
	for {
		l.mu.Lock()
		for len(l.pending) == 0 && !l.closed {
			l.cond.Wait()
		}
		if l.closed {
			l.mu.Unlock()
			return
		}
		k := l.pending[len(l.pending)-1]
		l.pending = l.pending[:len(l.pending)-1]
		l.inflight[k] = true
		l.mu.Unlock()

		img, err := l.load(k)

		l.mu.Lock()
		delete(l.inflight, k)
		if err != nil {
			l.failed[k] = l.o.Now()
		} else {
			delete(l.failed, k)
			l.insertLocked(k, img)
		}
		l.mu.Unlock()
		if l.o.Ready != nil && l.ctx.Err() == nil {
			l.o.Ready(k)
		}
	}
}

func (l *Loader) load(k Key) (*gfx.Image, error) {
	data, ok := []byte(nil), false
	if l.o.Disk != nil {
		data, ok = l.o.Disk.Get(k.String())
	}
	if !ok {
		ctx, cancel := context.WithTimeout(l.ctx, 15*time.Second)
		defer cancel()
		var err error
		if data, err = l.o.Fetch(ctx, k); err != nil {
			return nil, err
		}
	}
	img, err := gfx.DecodeImage(data, k.Size, k.Size)
	if err != nil {
		return nil, err
	}
	if !ok && l.o.Disk != nil {
		l.o.Disk.Put(k.String(), data)
	}
	return img, nil
}

func (l *Loader) insertLocked(k Key, img *gfx.Image) {
	e := &entry{key: k, img: img, bytes: int64(4 * img.W * img.H)}
	l.items[k] = l.lru.PushFront(e)
	l.memBytes += e.bytes
	for l.memBytes > l.o.MemBytes && l.lru.Len() > 1 {
		old := l.lru.Back()
		oe := old.Value.(*entry)
		l.lru.Remove(old)
		delete(l.items, oe.key)
		l.memBytes -= oe.bytes
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/cache/ ./internal/art/ && go test -race -count=3 ./internal/cache/ ./internal/art/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/cache internal/art
git commit -m "art: cover art loader with memory and disk LRU caches" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: Dev viewer (browser display and keyboard)

**Files:**
- Create: `internal/devview/devview.go`, `internal/devview/page.html`
- Test: `internal/devview/devview_test.go`

**Interfaces:**
- Consumes: `gfx.Canvas`, `gfx.Display` (Task 2); `input.Event`, `input.Button` (Task 4).
- Produces: `devview.New(w, h) *Viewer`, implementing `gfx.Display`. It also has:
  - `Listen(addr) error`, `URL() string`
  - `Events() <-chan input.Event`
  - `Handler() http.Handler`, which serves:
    - `GET /`: the page
    - `GET /frame?after=N`: long-polls until a frame newer than N exists, returns a PNG with an `X-Seq` header, and answers 204 on a timeout or Close
    - `POST /key?b=<button>&down=1|0`

The page sends only the first keydown of a held key (it drops the browser's own auto-repeat), because the app does its own key repeat. On blur, it releases every held key.

- [ ] **Step 1: Write the failing tests**

`internal/devview/devview_test.go`:

```go
package devview

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func TestFrameLongPollAndKeys(t *testing.T) {
	v := New(8, 4)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	defer v.Close()

	got := make(chan *http.Response, 1)
	go func() {
		r, err := http.Get(srv.URL + "/frame?after=0")
		if err == nil {
			got <- r
		}
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("frame returned before anything was presented")
	default:
	}
	c := gfx.NewCanvas(8, 4)
	c.Clear(gfx.RGB(10, 20, 30))
	v.Present(c)
	var r *http.Response
	select {
	case r = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("long poll not woken by Present")
	}
	defer r.Body.Close()
	if r.Header.Get("X-Seq") != "1" || r.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("headers %v", r.Header)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	img, err := png.Decode(&buf)
	if err != nil || img.Bounds().Dx() != 8 {
		t.Fatalf("frame png: %v", err)
	}

	resp, _ := http.Post(srv.URL+"/key?b=a&down=1", "", nil)
	resp.Body.Close()
	resp, _ = http.Post(srv.URL+"/key?b=a&down=0", "", nil)
	resp.Body.Close()
	if e := <-v.Events(); e != (input.Event{Button: input.BtnA, Kind: input.Press}) {
		t.Fatalf("event %+v", e)
	}
	if e := <-v.Events(); e.Kind != input.Release {
		t.Fatalf("event %+v", e)
	}
	resp, _ = http.Post(srv.URL+"/key?b=bogus&down=1", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown button status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestPageServed(t *testing.T) {
	v := New(1, 1)
	rec := httptest.NewRecorder()
	v.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("/frame?after=")) {
		t.Fatalf("page: %d", rec.Code)
	}
}

func TestCloseReleasesWaiters(t *testing.T) {
	v := New(1, 1)
	srv := httptest.NewServer(v.Handler())
	defer srv.Close()
	done := make(chan int, 1)
	go func() {
		r, err := http.Get(srv.URL + "/frame?after=0")
		if err == nil {
			r.Body.Close()
			done <- r.StatusCode
		}
	}()
	time.Sleep(30 * time.Millisecond)
	v.Close()
	select {
	case code := <-done:
		if code != http.StatusNoContent {
			t.Fatalf("status %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter not released on Close")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/devview/`
Expected: build failure.

- [ ] **Step 3: Implement**

`internal/devview/devview.go`:

```go
// Package devview is the development display: it serves the UI's frames to
// a browser page and turns the browser's key presses into input events, so
// the whole app runs on a PC without a MiSTer.
package devview

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"image/png"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

//go:embed page.html
var page []byte

var buttons = map[string]input.Button{
	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
}

// Viewer is a gfx.Display that browsers watch.
type Viewer struct {
	w, h   int
	events chan input.Event
	srv    *http.Server
	ln     net.Listener

	mu    sync.Mutex
	cond  *sync.Cond
	frame []byte
	seq   int
	done  bool
}

// New creates a viewer for w×h frames without starting a server (tests use Handler).
func New(w, h int) *Viewer {
	v := &Viewer{w: w, h: h, events: make(chan input.Event, 64)}
	v.cond = sync.NewCond(&v.mu)
	return v
}

// Listen starts serving on addr (e.g. "127.0.0.1:8090").
func (v *Viewer) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	v.ln = ln
	v.srv = &http.Server{Handler: v.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go v.srv.Serve(ln)
	return nil
}

// URL is the page address once Listen succeeded.
func (v *Viewer) URL() string {
	if v.ln == nil {
		return ""
	}
	return "http://" + v.ln.Addr().String() + "/"
}

func (v *Viewer) Events() <-chan input.Event { return v.events }
func (v *Viewer) Size() (int, int)           { return v.w, v.h }

func (v *Viewer) Present(c *gfx.Canvas) error {
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, c.ToRGBA()); err != nil {
		return err
	}
	v.mu.Lock()
	v.frame = b.Bytes()
	v.seq++
	v.cond.Broadcast()
	v.mu.Unlock()
	return nil
}

func (v *Viewer) Close() error {
	v.mu.Lock()
	v.done = true
	v.cond.Broadcast()
	v.mu.Unlock()
	if v.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return v.srv.Shutdown(ctx)
	}
	return nil
}

// Handler serves the page (/), frames (/frame?after=N, long-poll) and keys (POST /key?b=a&down=1).
func (v *Viewer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /frame", v.serveFrame)
	mux.HandleFunc("POST /key", func(w http.ResponseWriter, r *http.Request) {
		b, ok := buttons[r.URL.Query().Get("b")]
		if !ok {
			http.Error(w, "unknown button", http.StatusBadRequest)
			return
		}
		kind := input.Release
		if r.URL.Query().Get("down") == "1" {
			kind = input.Press
		}
		select {
		case v.events <- input.Event{Button: b, Kind: kind}:
		default: // UI not keeping up; drop rather than block the browser
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

var errGone = errors.New("viewer closed")

func (v *Viewer) waitFrame(ctx context.Context, after int) ([]byte, int, error) {
	stop := context.AfterFunc(ctx, func() { v.mu.Lock(); v.cond.Broadcast(); v.mu.Unlock() })
	defer stop()
	v.mu.Lock()
	defer v.mu.Unlock()
	for v.seq <= after && !v.done && ctx.Err() == nil {
		v.cond.Wait()
	}
	if v.done {
		return nil, 0, errGone
	}
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	return v.frame, v.seq, nil
}

func (v *Viewer) serveFrame(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	frame, seq, err := v.waitFrame(ctx, after)
	if err != nil {
		w.WriteHeader(http.StatusNoContent) // client just polls again
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Seq", strconv.Itoa(seq))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(frame)
}
```

`internal/devview/page.html`:

```html
<!doctype html>
<html><head><meta charset="utf-8"><title>MiSTer Subsonic — dev viewer</title>
<style>
  body { margin: 0; background: #111; color: #aaa; font: 13px sans-serif; display: flex; flex-direction: column; align-items: center; }
  img { max-width: 100vw; max-height: 90vh; image-rendering: pixelated; margin-top: 8px; background: #000; }
  p { margin: 6px; }
</style></head>
<body>
<img id="f" alt="waiting for the first frame…">
<p>Arrows move · Enter = A · Esc/Backspace = B · Tab = X · N/Q = Y · PgUp/PgDn = L/R · Space = Start · S = Select</p>
<script>
const keys = {ArrowUp:"up", ArrowDown:"down", ArrowLeft:"left", ArrowRight:"right",
  Enter:"a", Escape:"b", Backspace:"b", Tab:"x", n:"y", q:"y", PageUp:"l", PageDown:"r", " ":"start", s:"select"};
const held = new Set();
function send(e, down) {
  const b = keys[e.key]; if (!b) return;
  e.preventDefault();
  if (down && held.has(b)) return; // the app does its own key repeat
  down ? held.add(b) : held.delete(b);
  fetch("/key?b=" + b + "&down=" + (down ? 1 : 0), {method: "POST"});
}
addEventListener("keydown", e => send(e, true));
addEventListener("keyup", e => send(e, false));
addEventListener("blur", () => { for (const b of held) fetch("/key?b=" + b + "&down=0", {method: "POST"}); held.clear(); });
let seq = 0;
async function poll() {
  for (;;) {
    try {
      const r = await fetch("/frame?after=" + seq);
      if (r.status === 200) {
        seq = +r.headers.get("X-Seq");
        const url = URL.createObjectURL(await r.blob());
        const img = document.getElementById("f"), old = img.src;
        img.src = url; if (old.startsWith("blob:")) URL.revokeObjectURL(old);
      }
    } catch (e) { await new Promise(res => setTimeout(res, 1000)); }
  }
}
poll();
</script></body></html>
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/devview/ && go test -race -count=3 ./internal/devview/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/devview
git commit -m "devview: browser display with keyboard input for development" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: UI — app loop, profiles, widgets and screens

**Files:**
- Create: `internal/ui/app.go`, `internal/ui/icons.go`, `internal/ui/list.go`, `internal/ui/rows.go`, `internal/ui/screens_album.go`, `internal/ui/screens_home.go`, `internal/ui/screens_play.go`, `internal/ui/text.go`, `internal/ui/theme.go`
- Test: `internal/ui/bench_test.go`, `internal/ui/fakes_test.go`, `internal/ui/ui_test.go`
- Create: `internal/ui/testdata/golden/*.png` (generated in Step 4)

**Interfaces:**
- Consumes:
  - `gfx.*` (Tasks 2–3), `input.*` (Task 4), `art.Key` (Task 6)
  - `player.State` / `Status` / `Repeat` / `Event` / `Resume`, including `NextIndex` (Task 1)
  - `subsonic.Album` / `Song` / `AlbumListQuery` / `List*` / `Classify`
- Produces, layout:
  - `ui.Profile` with `ProfileHDMI` and `ProfileCRT240`
  - `ui.PickProfile(fbW, fbH, override) Profile`
- Produces, backend interfaces:
  - `ui.Library` (`GetAlbumList2`, `GetAlbum`)
  - `ui.Player` (the `*player.Player` subset)
  - `ui.ArtSource` (`Get(art.Key)`)
- Produces, screens:
  - `ui.Screen` interface `{Title(); Enter(*App); Handle(*App, input.Event) bool; Draw(*App, *gfx.Canvas, gfx.Rect)}`
  - `ui.NewHomeScreen()`, `NewAlbumListScreen(title, listType)`, `NewAlbumScreen(subsonic.Album)`, `NewNowPlayingScreen()`, `NewQueueScreen()`, `NewMessageScreen(title, message, retry func())`
- Produces, the app:
  - `ui.Options{Display; Profile; Library; Player; Art; Inputs []<-chan input.Event; Start func(*App); FallbackFonts; Now}`
  - `ui.New(o) (*App, error)`
  - `*App` methods: `Run(ctx) error`, `Attach(lib, player, art)`, `Post(func())`, `ArtReady(art.Key)`, `Push/Pop/Replace/Top`, `Load(screen, fn, done)`, `Toast(format, args...)`, `Redraw()`

**Design:**
- **Event loop.** One goroutine does everything. It pulls from:
  - input channels, merged;
  - player events;
  - posted closures (load results, art-ready);
  - a wake timer for key repeat, toast expiry, the 2 s B-hold and 500 ms progress redraws while playing.

  It renders only when something is dirty.
- **Loads.** `Load` results are dropped if their screen was popped.
- **Frame layout.**
  - A header with the screen title.
  - The screen's own area.
  - The mini bar, while a queue exists.
  - Toasts.
  - The exit prompt.

  Now Playing is full-screen.
- **Icons** (play, pause, stop, busy) are drawn as shapes, because Noto Sans has no media symbols.
- **Profile values** were tuned against the rendered screenshots:
  - Two-line rows use `Row2H`.
  - On CRT, Now Playing stacks the art above the text, centred.

- [ ] **Step 1: Write the fakes and tests**

`internal/ui/bench_test.go`:

```go
package ui

import (
	"testing"

	"mistersubsonic/internal/gfx"
)

// BenchmarkRepaint measures a full frame: draw the screen, scale it to the
// physical framebuffer and present it (headless = one frame copy). Spec §11
// spike 3 targets < 30 ms per repaint on the MiSTer's Cortex-A9; run it on the
// device with the cross-compiled test binary:
//
//	./ui.test -test.run '^$' -test.bench Repaint -test.benchtime 50x
func BenchmarkRepaint(b *testing.B) {
	cases := []struct {
		name   string
		prof   Profile
		fbW    int
		fbH    int
		screen func(ta *testApp) Screen
	}{
		{"albums-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
		{"nowplaying-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }},
		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			t := &testing.T{}
			ta := newTestApp(t, c.prof)
			ta.disp = gfx.NewHeadless(c.fbW, c.fbH, "")
			ta.o.Display = ta.disp
			ta.scaler = gfx.NewScaler(c.prof.W, c.prof.H, c.fbW, c.fbH)
			for i := 0; i < 40; i++ { // a long list, so every row is drawn
				ta.lib.albums = append(ta.lib.albums, ta.lib.albums[i%3])
			}
			ta.Push(NewHomeScreen())
			ta.Push(c.screen(ta))
			ta.settle(t)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ta.dirty = true
				if err := ta.render(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
```

`internal/ui/fakes_test.go`:

```go
package ui

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"testing"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type fakeLibrary struct {
	mu     sync.Mutex
	albums []subsonic.Album
	tracks map[subsonic.ID][]subsonic.Song
	err    error
	calls  []subsonic.AlbumListQuery
}

func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, q)
	if l.err != nil {
		return nil, l.err
	}
	end := min(q.Offset+q.Size, len(l.albums))
	if q.Offset >= end {
		return nil, nil
	}
	return append([]subsonic.Album(nil), l.albums[q.Offset:end]...), nil
}

func (l *fakeLibrary) GetAlbum(_ context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	for _, al := range l.albums {
		if al.ID == id {
			return &subsonic.AlbumWithSongs{Album: al, Songs: l.tracks[id]}, nil
		}
	}
	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no album"}
}

type fakePlayer struct {
	st      player.State
	events  chan player.Event
	resume  *player.Resume
	calls   []string
	played  []subsonic.Song
	start   int
	seekPos time.Duration
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{st: player.State{Index: -1, NextIndex: -1}, events: make(chan player.Event, 8)}
}

func (p *fakePlayer) State() player.State         { return p.st }
func (p *fakePlayer) Events() <-chan player.Event { return p.events }
func (p *fakePlayer) call(s string)               { p.calls = append(p.calls, s) }
func (p *fakePlayer) TogglePause()                { p.call("toggle") }
func (p *fakePlayer) Next()                       { p.call("next") }
func (p *fakePlayer) Prev()                       { p.call("prev") }
func (p *fakePlayer) Seek(d time.Duration)        { p.call("seek"); p.seekPos = d }
func (p *fakePlayer) Jump(i int)                  { p.call("jump"); p.st.Index = i }
func (p *fakePlayer) Remove(i int)                { p.call("remove") }
func (p *fakePlayer) SetShuffle(on bool)          { p.st.Shuffle = on }
func (p *fakePlayer) SetRepeat(r player.Repeat)   { p.st.Repeat = r }
func (p *fakePlayer) ResumeFrom(r *player.Resume) {
	p.call("resume")
	p.st.Queue, p.st.Index = r.Songs, r.Index
}
func (p *fakePlayer) Resumable(context.Context) (*player.Resume, error) {
	return p.resume, nil
}
func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
	p.call("playnow")
	p.played, p.start = songs, start
	p.st.Queue, p.st.Index, p.st.Status = songs, start, player.Playing
}

// fakeArt makes a deterministic two-tone square per cover id.
type fakeArt struct{}

func (fakeArt) Get(k art.Key) (*gfx.Image, bool) {
	if k.ID == "" {
		return nil, false
	}
	h := fnv.New32a()
	h.Write([]byte(k.ID))
	v := h.Sum32()
	img := gfx.NewImage(k.Size, k.Size)
	for y := 0; y < k.Size; y++ {
		for x := 0; x < k.Size; x++ {
			c := 0xFF000000 | v&0xFFFFFF
			if (x/(k.Size/4+1)+y/(k.Size/4+1))%2 == 0 {
				c = 0xFF000000 | ^v&0xFFFFFF
			}
			img.Pix[y*k.Size+x] = c
		}
	}
	return img, true
}

func sampleLibrary() *fakeLibrary {
	l := &fakeLibrary{tracks: map[subsonic.ID][]subsonic.Song{}}
	l.albums = []subsonic.Album{
		{ID: "al-1", Name: "Радио Африка", Artist: "Аквариум", CoverArt: "al-1", Year: 1983},
		{ID: "al-2", Name: "Homogenic", Artist: "Björk", CoverArt: "al-2", Year: 1997},
		{ID: "al-3", Name: "A Rather Long Album Title That Will Need Truncating Somewhere", Artist: "Some Artist", CoverArt: "al-3", Year: 2001},
	}
	l.tracks["al-1"] = []subsonic.Song{
		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
		{ID: "s2", Title: "Время Луны", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 2, Duration: 160, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
		{ID: "s3", Title: "Рок-н-ролл мёртв", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 3, Duration: 215, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
	}
	return l
}

type testApp struct {
	*App
	disp *gfx.Headless
	lib  *fakeLibrary
	pl   *fakePlayer
	now  time.Time
}

func newTestApp(t *testing.T, prof Profile) *testApp {
	t.Helper()
	ta := &testApp{disp: gfx.NewHeadless(prof.W, prof.H, ""), lib: sampleLibrary(), pl: newFakePlayer(), now: time.Unix(1_800_000_000, 0)}
	a, err := New(Options{Display: ta.disp, Profile: prof, Library: ta.lib, Player: ta.pl, Art: fakeArt{},
		Now: func() time.Time { return ta.now }})
	if err != nil {
		t.Fatal(err)
	}
	ta.App = a
	return ta
}

// settle runs posted work until no loads are pending, then renders.
func (ta *testApp) settle(t *testing.T) *gfx.Canvas {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for ta.loads > 0 || len(ta.post) > 0 {
		select {
		case f := <-ta.post:
			f()
		case <-deadline:
			t.Fatal("loads did not finish")
		}
	}
	if err := ta.render(); err != nil {
		t.Fatal(err)
	}
	return ta.disp.Last()
}

func (ta *testApp) press(b input.Button) {
	ta.onInput(input.Event{Button: b, Kind: input.Press})
	ta.onInput(input.Event{Button: b, Kind: input.Release})
}

var errOffline = errors.New("offline")
```

`internal/ui/ui_test.go`:

```go
package ui

import (
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

var update = flag.Bool("update", false, "rewrite golden screenshots in testdata/golden")

// golden compares c with testdata/golden/<name>.png (exact pixels).
func golden(t *testing.T, name string, c *gfx.Canvas) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".png")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := gfx.SavePNG(path, c); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test ./internal/ui -update): %v", path, err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if want.Bounds().Dx() != c.W || want.Bounds().Dy() != c.H {
		t.Fatalf("%s: size %v, want %dx%d", name, want.Bounds(), c.W, c.H)
	}
	diff := 0
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			r, g, b, _ := want.At(x, y).RGBA()
			if gfx.RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8)) != c.At(x, y) {
				diff++
			}
		}
	}
	if diff > 0 {
		out := filepath.Join(t.TempDir(), name+"-got.png")
		gfx.SavePNG(out, c)
		t.Fatalf("%s: %d pixels differ from the golden; got image at %s (run with -update if the change is intended)", name, diff, out)
	}
}

var profiles = []Profile{ProfileHDMI, ProfileCRT240}

func TestGoldenHome(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
		ta.Push(NewHomeScreen())
		golden(t, "home-"+p.Name, ta.settle(t))
	}
}

func TestGoldenAlbumList(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
		golden(t, "albums-"+p.Name, ta.settle(t))
	}
}

func TestGoldenAlbum(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewAlbumScreen(ta.lib.albums[0]))
		golden(t, "album-"+p.Name, ta.settle(t))
	}
}

func playingState(ta *testApp) {
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: 1, Status: player.Playing, Position: 75 * time.Second}
}

func TestGoldenNowPlayingAndMiniBar(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.Push(NewHomeScreen())
		golden(t, "home-minibar-"+p.Name, ta.settle(t))
		ta.Push(NewNowPlayingScreen())
		golden(t, "nowplaying-"+p.Name, ta.settle(t))
	}
}

func TestGoldenQueueAndMessageAndToast(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.Push(NewHomeScreen())
		ta.Push(NewQueueScreen())
		ta.Toast("Removed Время Луны")
		golden(t, "queue-toast-"+p.Name, ta.settle(t))
		ta.Replace(NewMessageScreen("Can't reach the server", "http://192.168.1.10:4533 did not answer (server unreachable).\nCheck that Navidrome is running.", func() {}))
		ta.toasts = nil
		golden(t, "message-"+p.Name, ta.settle(t))
	}
}

func TestNavigationHomeToAlbumToPlay(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.settle(t)
	ta.press(input.BtnA) // "Recently added"
	if _, ok := ta.Top().(*AlbumListScreen); !ok {
		t.Fatalf("top = %T", ta.Top())
	}
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Homogenic
	as, ok := ta.Top().(*AlbumScreen)
	if !ok || as.stub.ID != "al-2" {
		t.Fatalf("top = %T %+v", ta.Top(), ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnUp)
	ta.press(input.BtnA) // Радио Африка
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // row 3 = track 2
	ta.press(input.BtnA)
	if ta.pl.start != 1 || len(ta.pl.played) != 3 {
		t.Fatalf("played start %d of %d", ta.pl.start, len(ta.pl.played))
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("after play, top = %T", ta.Top())
	}
	ta.press(input.BtnB)
	if _, ok := ta.Top().(*AlbumScreen); !ok {
		t.Fatalf("B from Now Playing should return to the album, got %T", ta.Top())
	}
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("Y should open Now Playing, got %T", ta.Top())
	}
}

func TestNowPlayingControls(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewHomeScreen())
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnA)
	ta.press(input.BtnR)
	ta.press(input.BtnL)
	ta.press(input.BtnRight)
	if ta.pl.seekPos != 85*time.Second {
		t.Fatalf("seek to %v, want 85s", ta.pl.seekPos)
	}
	ta.onInput(input.Event{Button: input.BtnLeft, Kind: input.Repeat})
	if ta.pl.seekPos != 45*time.Second {
		t.Fatalf("held seek to %v, want 45s", ta.pl.seekPos)
	}
	want := []string{"toggle", "next", "prev", "seek", "seek"}
	if len(ta.pl.calls) != len(want) {
		t.Fatalf("calls %v, want %v", ta.pl.calls, want)
	}
	for i := range want {
		if ta.pl.calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", ta.pl.calls, want)
		}
	}
	ta.press(input.BtnSelect)
	if !ta.pl.st.Shuffle {
		t.Fatal("Select should switch to shuffle")
	}
	ta.press(input.BtnSelect)
	if ta.pl.st.Shuffle || ta.pl.st.Repeat != player.RepeatAll {
		t.Fatalf("second Select: shuffle %v repeat %v", ta.pl.st.Shuffle, ta.pl.st.Repeat)
	}
	ta.press(input.BtnY)
	if _, ok := ta.Top().(*QueueScreen); !ok {
		t.Fatalf("Y in Now Playing should open the queue, got %T", ta.Top())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if ta.pl.st.Index != 1 {
		t.Fatalf("jump index %d", ta.pl.st.Index)
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatal("A in queue should jump and return to Now Playing")
	}
}

func TestStartTogglesPauseGlobally(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.press(input.BtnStart)
	if len(ta.pl.calls) != 1 || ta.pl.calls[0] != "toggle" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestResumeFromHome(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 2}
	ta.Push(NewHomeScreen())
	ta.settle(t)
	ta.press(input.BtnA)
	if len(ta.pl.calls) != 1 || ta.pl.calls[0] != "resume" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("top %T", ta.Top())
	}
}

func TestHoldBAtRootAsksToExit(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(exitHold)
	ta.onWake()
	if !ta.confirm {
		t.Fatal("holding B for 2 s at the root should ask to exit")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release})
	ta.press(input.BtnB) // B = stay
	if ta.confirm || ta.quit {
		t.Fatal("B should cancel the exit prompt")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(time.Second) // released early: no prompt
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release})
	ta.now = ta.now.Add(2 * time.Second)
	ta.onWake()
	if ta.confirm {
		t.Fatal("a short B press must not ask to exit")
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press})
	ta.now = ta.now.Add(exitHold)
	ta.onWake()
	ta.press(input.BtnA)
	if !ta.quit {
		t.Fatal("A in the prompt should exit")
	}
}

func TestLoadErrorShowsRetry(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.err = errOffline
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	s := ta.Top().(*AlbumListScreen)
	if s.err == nil {
		t.Fatal("error not recorded")
	}
	ta.lib.err = nil
	ta.press(input.BtnA)
	ta.settle(t)
	if len(s.albums) != 3 {
		t.Fatalf("retry loaded %d albums", len(s.albums))
	}
}

func TestPoppedScreenIgnoresLateLoad(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	al := NewAlbumScreen(ta.lib.albums[0])
	ta.Push(al)
	ta.Pop() // before the load finishes
	ta.settle(t)
	if al.album != nil {
		t.Fatal("a popped screen received its load result")
	}
}

func TestAlbumListPagesOnScroll(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	for i := 0; i < 150; i++ {
		ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: subsonic.ID("x" + string(rune('a'+i%26))), Name: "Filler"})
	}
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	s := ta.Top().(*AlbumListScreen)
	if len(s.albums) != albumPage {
		t.Fatalf("first page %d", len(s.albums))
	}
	for i := 0; i < 20; i++ {
		ta.press(input.BtnR) // page down
	}
	ta.settle(t)
	if len(s.albums) != 153 {
		t.Fatalf("after scrolling, %d albums loaded, want 153", len(s.albums))
	}
	if q := ta.lib.calls[len(ta.lib.calls)-1]; q.Offset != albumPage {
		t.Fatalf("second page offset %d", q.Offset)
	}
}

func TestPickProfile(t *testing.T) {
	if p := PickProfile(1920, 1080, "auto"); p.Name != "hdmi" {
		t.Fatalf("1080p → %s", p.Name)
	}
	if p := PickProfile(640, 240, "auto"); p.Name != "crt" || p.H != 240 {
		t.Fatalf("240p → %s %d", p.Name, p.H)
	}
	if p := PickProfile(640, 288, "auto"); p.H != 288 {
		t.Fatalf("288p height %d", p.H)
	}
	if p := PickProfile(640, 480, "crt"); p.Name != "crt" {
		t.Fatalf("forced crt → %s", p.Name)
	}
	if p := PickProfile(640, 240, "hdmi"); p.Name != "hdmi" {
		t.Fatalf("forced hdmi → %s", p.Name)
	}
}

func TestFormatHelpers(t *testing.T) {
	if clock(3*time.Minute+5*time.Second) != "3:05" || clock(time.Hour+time.Minute) != "1:01:00" {
		t.Fatal("clock format")
	}
	s := subsonic.Song{Suffix: "flac", BitDepth: 24, SamplingRate: 96000}
	if q := quality(s, false); q != "FLAC 24/96" {
		t.Fatalf("quality %q", q)
	}
	if q := quality(subsonic.Song{Suffix: "flac", BitDepth: 16, SamplingRate: 44100}, false); q != "FLAC 16/44.1" {
		t.Fatalf("quality %q", q)
	}
	if q := quality(subsonic.Song{Suffix: "m4a", BitRate: 256}, true); q != "M4A 256 · transcoded" {
		t.Fatalf("quality %q", q)
	}
}

func TestEmptyLibraryAndEmptyAlbum(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.albums = nil
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnA) // nothing to open: must not panic
	ta.press(input.BtnDown)
	if _, ok := ta.Top().(*AlbumListScreen); !ok {
		t.Fatalf("top = %T", ta.Top())
	}
	ta.lib.albums = []subsonic.Album{{ID: "empty", Name: "Silence", Artist: "Nobody"}}
	ta.Replace(NewHomeScreen())
	ta.Push(NewAlbumScreen(ta.lib.albums[0]))
	ta.settle(t)
	ta.press(input.BtnA)      // "Play" on an album without tracks
	ta.press(input.BtnSelect) // shuffle-play too
	if len(ta.pl.calls) != 0 {
		t.Fatalf("playing an empty album called %v", ta.pl.calls)
	}
}

// Before the server connects there is no player: keys must be harmless.
func TestKeysBeforePlayerAttached(t *testing.T) {
	disp := gfx.NewHeadless(ProfileHDMI.W, ProfileHDMI.H, "")
	a, err := New(Options{Display: disp, Profile: ProfileHDMI})
	if err != nil {
		t.Fatal(err)
	}
	a.Push(NewMessageScreen("Connecting…", "Connecting to the server…", nil))
	for _, b := range []input.Button{input.BtnStart, input.BtnY, input.BtnA, input.BtnB, input.BtnSelect} {
		a.onInput(input.Event{Button: b, Kind: input.Press})
		a.onInput(input.Event{Button: b, Kind: input.Release})
	}
	if err := a.render(); err != nil {
		t.Fatal(err)
	}
	a.Attach(sampleLibrary(), newFakePlayer(), fakeArt{})
	a.Replace(NewHomeScreen())
	if _, ok := a.Top().(*HomeScreen); !ok {
		t.Fatalf("top = %T", a.Top())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/ui/`
Expected: build failure (`undefined: New`, `undefined: ProfileHDMI`, …).

- [ ] **Step 3: Implement**

`internal/ui/app.go`:

```go
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// Library is the server API the screens use (subset of *subsonic.Client).
type Library interface {
	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
}

// Player is the playback API the screens use (subset of *player.Player).
type Player interface {
	State() player.State
	Events() <-chan player.Event
	PlayNow(songs []subsonic.Song, start int)
	TogglePause()
	Next()
	Prev()
	Seek(pos time.Duration)
	Jump(i int)
	Remove(i int)
	SetShuffle(on bool)
	SetRepeat(r player.Repeat)
	Resumable(ctx context.Context) (*player.Resume, error)
	ResumeFrom(r *player.Resume)
}

// ArtSource returns cover images already in memory, queueing a load otherwise.
type ArtSource interface {
	Get(k art.Key) (*gfx.Image, bool)
}

// Screen is one page of the UI.
type Screen interface {
	Title() string
	// Enter runs when the screen is pushed; start loads here.
	Enter(a *App)
	// Handle gets Press and Repeat events; return false to let the app
	// apply global keys (B = back, Y = Now Playing, Start = play/pause).
	Handle(a *App, e input.Event) bool
	Draw(a *App, c *gfx.Canvas, area gfx.Rect)
}

type Options struct {
	Display gfx.Display
	Profile Profile
	Library Library
	Player  Player
	Art     ArtSource
	Inputs  []<-chan input.Event
	// Start runs on the UI goroutine before the first frame; it pushes the
	// first screen (main uses it for the connect-or-error flow).
	Start         func(a *App)
	FallbackFonts string
	Now           func() time.Time
}

// Fonts used by the screens.
type Fonts struct {
	Title, Body, Small *gfx.Font
}

type screenEntry struct {
	s      Screen
	ctx    context.Context
	cancel context.CancelFunc
}

type toast struct {
	text  string
	until time.Time
}

const (
	toastTime    = 3 * time.Second
	exitHold     = 2 * time.Second
	progressTick = 500 * time.Millisecond
)

// App owns the screen stack and the event loop. All methods except Post
// must be called on the UI goroutine.
type App struct {
	o       Options
	P       Profile
	F       Fonts
	canvas  *gfx.Canvas
	scaler  *gfx.Scaler
	stack   []screenEntry
	toasts  []toast
	rep     input.Repeater
	in      chan input.Event
	post    chan func()
	loads   int
	dirty   bool
	quit    bool
	bDown   time.Time // when B went down on the root screen (zero if not held)
	confirm bool      // exit confirmation shown
}

func New(o Options) (*App, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true}
	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
	if err != nil {
		return nil, err
	}
	bold, err := gfx.LoadTypeface(true, o.FallbackFonts)
	if err != nil {
		return nil, err
	}
	if a.F.Title, err = bold.Face(a.P.Title); err != nil {
		return nil, err
	}
	if a.F.Body, err = regular.Face(a.P.Body); err != nil {
		return nil, err
	}
	if a.F.Small, err = regular.Face(a.P.Small); err != nil {
		return nil, err
	}
	a.canvas = gfx.NewCanvas(a.P.W, a.P.H)
	pw, ph := o.Display.Size()
	a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
	for _, ch := range o.Inputs {
		go func(ch <-chan input.Event) {
			for e := range ch {
				a.in <- e
			}
		}(ch)
	}
	return a, nil
}

// Attach sets the library, player and art source once the server connection
// exists (the UI starts before it, to show "Connecting…" and errors).
// Call on the UI goroutine.
func (a *App) Attach(lib Library, pl Player, art ArtSource) {
	a.o.Library, a.o.Player, a.o.Art = lib, pl, art
	a.dirty = true
}

// Post runs f on the UI goroutine. Safe from any goroutine.
func (a *App) Post(f func()) {
	select {
	case a.post <- f:
	default:
		go func() { a.post <- f }() // never block a worker on a busy UI
	}
}

// Redraw marks the frame dirty.
func (a *App) Redraw() { a.dirty = true }

// ArtReady is the art loader's Ready callback: redraw when a cover arrives.
func (a *App) ArtReady(art.Key) { a.Post(a.Redraw) }

func (a *App) Library() Library { return a.o.Library }
func (a *App) Player() Player   { return a.o.Player }

// Art returns a cover at size px if loaded.
func (a *App) Art(id subsonic.ID, px int) (*gfx.Image, bool) {
	if a.o.Art == nil || id == "" {
		return nil, false
	}
	return a.o.Art.Get(art.Key{ID: id, Size: px})
}

// Push opens a screen on top.
func (a *App) Push(s Screen) {
	ctx, cancel := context.WithCancel(context.Background())
	a.stack = append(a.stack, screenEntry{s, ctx, cancel})
	a.dirty = true
	s.Enter(a)
}

// Pop closes the top screen (never the last one) and cancels its loads.
func (a *App) Pop() {
	if len(a.stack) <= 1 {
		return
	}
	top := a.stack[len(a.stack)-1]
	top.cancel()
	a.stack = a.stack[:len(a.stack)-1]
	a.dirty = true
}

// Replace swaps the whole stack for one root screen.
func (a *App) Replace(s Screen) {
	for _, e := range a.stack {
		e.cancel()
	}
	a.stack = nil
	a.Push(s)
}

// Top is the visible screen.
func (a *App) Top() Screen {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1].s
}

func (a *App) entry(s Screen) *screenEntry {
	for i := range a.stack {
		if a.stack[i].s == s {
			return &a.stack[i]
		}
	}
	return nil
}

// Load runs fn off the UI goroutine and delivers its result to done on the
// UI goroutine, unless screen s has been popped by then.
func (a *App) Load(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) {
	e := a.entry(s)
	if e == nil {
		return
	}
	ctx := e.ctx
	a.loads++
	go func() {
		v, err := fn(ctx)
		a.Post(func() {
			a.loads--
			if ctx.Err() == nil {
				done(v, err)
				a.dirty = true
			}
		})
	}()
}

// Toast shows a short message over the current screen.
func (a *App) Toast(format string, args ...any) {
	a.toasts = append(a.toasts, toast{fmt.Sprintf(format, args...), a.o.Now().Add(toastTime)})
	a.dirty = true
}

// Run drives the UI until ctx ends or the user exits.
func (a *App) Run(ctx context.Context) error {
	if a.o.Start != nil {
		a.o.Start(a)
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for !a.quit {
		if a.dirty {
			if err := a.render(); err != nil {
				return err
			}
		}
		timer.Reset(a.untilWake())
		var pevents <-chan player.Event // nil until a player is attached
		if a.o.Player != nil {
			pevents = a.o.Player.Events()
		}
		select {
		case <-ctx.Done():
			return nil
		case e := <-a.in:
			a.onInput(e)
		case ev := <-pevents:
			a.onPlayer(ev)
		case f := <-a.post:
			f()
		case <-timer.C:
			a.onWake()
		}
	}
	return nil
}

func (a *App) untilWake() time.Duration {
	now := a.o.Now()
	next := now.Add(time.Hour)
	consider := func(t time.Time) {
		if !t.IsZero() && t.Before(next) {
			next = t
		}
	}
	consider(a.rep.NextDeadline())
	for _, t := range a.toasts {
		consider(t.until)
	}
	if !a.bDown.IsZero() {
		consider(a.bDown.Add(exitHold))
	}
	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
		consider(now.Add(progressTick))
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return time.Millisecond
}

func (a *App) onWake() {
	now := a.o.Now()
	for _, e := range a.rep.Due(now) {
		a.dispatch(e)
	}
	kept := a.toasts[:0]
	for _, t := range a.toasts {
		if now.Before(t.until) {
			kept = append(kept, t)
		} else {
			a.dirty = true
		}
	}
	a.toasts = kept
	if !a.bDown.IsZero() && !now.Before(a.bDown.Add(exitHold)) {
		a.bDown = time.Time{}
		a.confirm = true
		a.dirty = true
	}
	if a.o.Player != nil && a.o.Player.State().Status == player.Playing {
		a.dirty = true // progress
	}
}

func (a *App) onPlayer(ev player.Event) {
	a.dirty = true
	if ev.Kind == player.Error {
		title := ev.Song.Title
		if title == "" {
			title = "track"
		}
		a.Toast("Can't play %s: %s", title, subsonic.Classify(ev.Err))
	}
}

func (a *App) onInput(e input.Event) {
	now := a.o.Now()
	a.rep.Feed(e, now)
	if e.Button == input.BtnB {
		if e.Kind == input.Press && len(a.stack) == 1 && !a.confirm {
			a.bDown = now
		} else if e.Kind == input.Release {
			a.bDown = time.Time{}
		}
	}
	if e.Kind != input.Release {
		a.dispatch(e)
	}
}

func (a *App) dispatch(e input.Event) {
	a.dirty = true
	if a.confirm {
		switch e.Button {
		case input.BtnA:
			a.quit = true
		case input.BtnB:
			a.confirm = false
		}
		return
	}
	if top := a.Top(); top != nil && top.Handle(a, e) {
		return
	}
	if e.Kind != input.Press {
		return
	}
	switch e.Button {
	case input.BtnB:
		a.Pop()
	case input.BtnStart:
		if a.o.Player != nil {
			a.o.Player.TogglePause()
		}
	case input.BtnY:
		if _, ok := a.Top().(*NowPlayingScreen); !ok && a.hasQueue() {
			a.Push(NewNowPlayingScreen())
		}
	}
}

func (a *App) hasQueue() bool {
	return a.o.Player != nil && len(a.o.Player.State().Queue) > 0
}

func (a *App) render() error {
	a.dirty = false
	c := a.canvas
	c.Clear(colBg)
	top := a.Top()
	p := a.P
	body := gfx.R(0, 0, p.W, p.H)
	if top != nil {
		_, fullscreen := top.(*NowPlayingScreen)
		if !fullscreen {
			a.drawHeader(c, top.Title())
			body = gfx.R(0, p.HeaderH, p.W, p.H-p.HeaderH)
			if a.hasQueue() {
				body.H -= p.MiniBarH
				a.drawMiniBar(c, gfx.R(0, p.H-p.MiniBarH, p.W, p.MiniBarH))
			}
		}
		top.Draw(a, c, body)
	}
	a.drawToasts(c)
	if a.confirm {
		a.drawConfirm(c)
	}
	return a.o.Display.Present(a.scaler.Scale(c))
}

func (a *App) drawHeader(c *gfx.Canvas, title string) {
	p := a.P
	c.Fill(gfx.R(0, 0, p.W, p.HeaderH), colPanel)
	f := a.F.Title
	y := (p.HeaderH + f.Ascent() - f.Descent()) / 2
	f.Draw(c, p.Margin, y, f.Truncate(title, p.W-2*p.Margin), colText, c.Bounds())
}

func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
	st := a.o.Player.State()
	song, _ := st.Current()
	c.Fill(r, colPanel)
	p := a.P
	x := p.Margin
	art := r.H - 2*max(p.Margin/3, 2)
	ay := r.Y + (r.H-art)/2
	a.drawArt(c, song.CoverArt, gfx.R(x, ay, art, art))
	x += art + p.Margin/2
	w := r.Right() - p.Margin - x
	line := song.Title
	if song.Artist != "" {
		line += " — " + song.Artist
	}
	f := a.F.Body
	base := r.Y + r.H/2 + f.Ascent()/3
	iconText(c, f, statusIcon(st.Status), x, base, f.Truncate(line, w-f.Ascent()), colText, c.Bounds())
	if d := time.Duration(song.Duration) * time.Second; d > 0 {
		bw := w * int(min(st.Position, d)) / int(d)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, w, 2), colArtBg)
		c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/6, 2)-2, bw, 2), colAccent)
	}
}

func (a *App) drawToasts(c *gfx.Canvas) {
	f := a.F.Body
	p := a.P
	y := p.H - p.MiniBarH - p.Margin
	for i := len(a.toasts) - 1; i >= 0; i-- {
		text := f.Truncate(a.toasts[i].text, p.W-4*p.Margin)
		w := f.Measure(text) + p.Margin
		h := f.Height() + p.Margin/2
		r := gfx.R((p.W-w)/2, y-h, w, h)
		c.Fill(r, colOverlay)
		f.Draw(c, r.X+p.Margin/2, r.Y+p.Margin/4+f.Ascent(), text, colText, c.Bounds())
		y -= h + p.Margin/4
	}
}

func (a *App) drawConfirm(c *gfx.Canvas) {
	p := a.P
	c.Fill(c.Bounds(), colOverlay)
	lines := []string{"Exit MiSTer Subsonic?", "A = exit    B = stay"}
	f := a.F.Title
	y := p.H/2 - f.Height()
	for i, l := range lines {
		if i == 1 {
			f = a.F.Body
		}
		f.Draw(c, (p.W-f.Measure(l))/2, y+f.Ascent(), l, colText, c.Bounds())
		y += f.Height() + p.Margin/2
	}
}

// drawArt draws a cover (or a placeholder while it loads) into r.
func (a *App) drawArt(c *gfx.Canvas, id subsonic.ID, r gfx.Rect) {
	c.Fill(r, colArtBg)
	if img, ok := a.Art(id, r.W); ok {
		// Centre non-square art inside the square.
		w, h := r.W, r.H
		if img.W > img.H {
			h = r.H * img.H / img.W
		} else if img.H > img.W {
			w = r.W * img.W / img.H
		}
		c.Blit(img, gfx.R(r.X+(r.W-w)/2, r.Y+(r.H-h)/2, w, h))
	}
}

// clock formats d as m:ss or h:mm:ss.
func clock(d time.Duration) string {
	s := int(d / time.Second)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// quality is the badge for a song, e.g. "FLAC 24/96" or "MP3 320 · transcoded".
func quality(s subsonic.Song, transcoded bool) string {
	q := strings.ToUpper(s.Suffix)
	switch {
	case s.BitDepth > 0 && s.SamplingRate > 0:
		q += fmt.Sprintf(" %d/%s", s.BitDepth, khz(s.SamplingRate))
	case s.BitRate > 0:
		q += fmt.Sprintf(" %d", s.BitRate)
	}
	if transcoded {
		q += " · transcoded"
	}
	return strings.TrimSpace(q)
}

func khz(hz int) string {
	if hz%1000 == 0 {
		return fmt.Sprint(hz / 1000)
	}
	return fmt.Sprintf("%.1f", float64(hz)/1000)
}
```

`internal/ui/icons.go`:

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/player"
)

type icon int

const (
	iconPlay icon = iota
	iconPause
	iconStop
	iconBusy
)

// drawIcon draws a simple vector icon filling the square r. Icons are drawn
// rather than taken from the font: Noto Sans has no media symbols.
func drawIcon(c *gfx.Canvas, ic icon, r gfx.Rect, col gfx.Color) {
	switch ic {
	case iconPlay: // right-pointing triangle
		mid := r.H / 2
		for y := 0; y < r.H; y++ {
			d := y - mid
			if d < 0 {
				d = -d
			}
			w := r.W * (mid - d) / max(mid, 1)
			c.Fill(gfx.R(r.X, r.Y+y, w, 1), col)
		}
	case iconPause:
		bw := max(r.W/3, 1)
		c.Fill(gfx.R(r.X, r.Y, bw, r.H), col)
		c.Fill(gfx.R(r.Right()-bw, r.Y, bw, r.H), col)
	case iconStop:
		c.Fill(r, col)
	case iconBusy: // three dots
		d := max(r.W/5, 1)
		for i := 0; i < 3; i++ {
			c.Fill(gfx.R(r.X+i*2*d, r.Y+(r.H-d)/2, d, d), col)
		}
	}
}

func statusIcon(s player.Status) icon {
	switch s {
	case player.Playing:
		return iconPlay
	case player.Paused:
		return iconPause
	case player.Loading, player.Buffering:
		return iconBusy
	}
	return iconStop
}

func statusLabel(s player.Status) string {
	switch s {
	case player.Playing:
		return "Playing"
	case player.Paused:
		return "Paused"
	case player.Loading:
		return "Loading…"
	case player.Buffering:
		return "Buffering…"
	}
	return "Stopped"
}

// iconText draws an icon sized to font f followed by text; returns the end x.
func iconText(c *gfx.Canvas, f *gfx.Font, ic icon, x, baseline int, text string, col gfx.Color, clip gfx.Rect) int {
	s := f.Ascent() * 2 / 3
	drawIcon(c, ic, gfx.R(x, baseline-s, s, s), col)
	return f.Draw(c, x+s+s/2, baseline, text, col, clip)
}
```

`internal/ui/list.go`:

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// List is a vertical, focusable, scrolling list. Rows are drawn by the owner.
type List struct {
	Focus int
	top   int
	rows  int // visible rows at the last Draw
}

// Handle moves the focus: Up/Down by one, L/R by a page. It reports whether
// the event was used.
func (l *List) Handle(e input.Event, n int) bool {
	if n == 0 {
		return false
	}
	page := max(l.rows-1, 1)
	switch e.Button {
	case input.BtnUp:
		l.Focus--
	case input.BtnDown:
		l.Focus++
	case input.BtnL:
		l.Focus -= page
	case input.BtnR:
		l.Focus += page
	default:
		return false
	}
	l.Focus = min(max(l.Focus, 0), n-1)
	return true
}

// Draw lays out n rows of height rowH in area, keeping the focus visible,
// and calls row for each visible index.
func (l *List) Draw(c *gfx.Canvas, area gfx.Rect, n, rowH int, row func(i int, r gfx.Rect, focused bool)) {
	l.rows = max(area.H/rowH, 1)
	l.Focus = min(max(l.Focus, 0), max(n-1, 0))
	if l.Focus < l.top {
		l.top = l.Focus
	}
	if l.Focus >= l.top+l.rows {
		l.top = l.Focus - l.rows + 1
	}
	l.top = max(min(l.top, n-l.rows), 0)
	for i := l.top; i < n && i < l.top+l.rows; i++ {
		r := gfx.R(area.X, area.Y+(i-l.top)*rowH, area.W, rowH)
		if i == l.Focus {
			c.Fill(r, colFocus)
		}
		row(i, r, i == l.Focus)
	}
}

// NearEnd reports whether the focus is within a page of the end (to load more).
func (l *List) NearEnd(n int) bool { return n > 0 && l.Focus >= n-max(l.rows, 1) }
```

`internal/ui/rows.go`:

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/subsonic"
)

// drawTextRow draws a thumbnail (if id != "" and the profile has thumbs),
// a main line and an optional dim second line into r.
func (a *App) drawTextRow(c *gfx.Canvas, r gfx.Rect, coverID subsonic.ID, withThumb bool, main, sub string, mainCol gfx.Color) {
	p := a.P
	x := r.X + p.Margin
	if withThumb && p.Thumb > 0 {
		t := min(p.Thumb, r.H-2)
		a.drawArt(c, coverID, gfx.R(x, r.Y+(r.H-t)/2, t, t))
		x += t + p.Margin/2
	}
	w := r.Right() - p.Margin - x
	fb, fs := a.F.Body, a.F.Small
	if sub == "" {
		fb.Draw(c, x, r.Y+(r.H+fb.Ascent()-fb.Descent())/2, fb.Truncate(main, w), mainCol, r)
		return
	}
	total := fb.Height() + fs.Height()
	y := r.Y + (r.H-total)/2
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(main, w), mainCol, r)
	fs.Draw(c, x, y+fb.Height()+fs.Ascent(), fs.Truncate(sub, w), colDim, r)
}

// drawCentered draws a dim message in the middle of area (loading, empty, error).
func (a *App) drawCentered(c *gfx.Canvas, area gfx.Rect, text string, col gfx.Color) {
	f := a.F.Body
	t := f.Truncate(text, area.W-2*a.P.Margin)
	f.Draw(c, area.X+(area.W-f.Measure(t))/2, area.Y+area.H/2+f.Ascent()/2, t, col, area)
}
```

`internal/ui/screens_album.go`:

```go
package ui

import (
	"context"
	"fmt"
	"strings"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// AlbumScreen shows an album header and its tracks, with Play and Shuffle rows.
type AlbumScreen struct {
	stub  subsonic.Album // from the list, shown while loading
	album *subsonic.AlbumWithSongs
	list  List
	err   error
}

const albumActionRows = 2 // Play, Shuffle

func NewAlbumScreen(a subsonic.Album) *AlbumScreen { return &AlbumScreen{stub: a} }

// Title is the artist: the album name is already the header block's headline.
func (s *AlbumScreen) Title() string { return s.stub.Artist }

func (s *AlbumScreen) Enter(a *App) { s.load(a) }

func (s *AlbumScreen) load(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetAlbum(ctx, s.stub.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.album = v.(*subsonic.AlbumWithSongs)
	})
}

func (s *AlbumScreen) songs() []subsonic.Song {
	if s.album == nil {
		return nil
	}
	return s.album.Songs
}

func (s *AlbumScreen) play(a *App, start int, shuffle bool) {
	songs := s.songs()
	if len(songs) == 0 {
		return
	}
	a.Player().SetShuffle(shuffle)
	if shuffle {
		start = shuffleStart(len(songs))
	}
	a.Player().PlayNow(songs, start)
	a.Push(NewNowPlayingScreen())
}

func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
	n := albumActionRows + len(s.songs())
	if s.album == nil {
		n = 0
	}
	if s.list.Handle(e, n) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		switch {
		case s.album == nil && s.err != nil:
			s.load(a)
		case s.album == nil:
		case s.list.Focus == 0:
			s.play(a, 0, false)
		case s.list.Focus == 1:
			s.play(a, 0, true)
		default:
			s.play(a, s.list.Focus-albumActionRows, false)
		}
		return true
	case input.BtnSelect:
		s.play(a, 0, true)
		return true
	}
	return false
}

// summary is e.g. "1983 · 11 tracks · 42:00 · FLAC 24/96".
func (s *AlbumScreen) summary() string {
	var parts []string
	if s.stub.Year > 0 {
		parts = append(parts, fmt.Sprint(s.stub.Year))
	}
	songs := s.songs()
	if len(songs) > 0 {
		total := 0
		for _, so := range songs {
			total += so.Duration
		}
		parts = append(parts, fmt.Sprintf("%d tracks", len(songs)), clock(secs(total)))
		parts = append(parts, quality(songs[0], false))
	}
	return strings.Join(parts, " · ")
}

func (s *AlbumScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	art := p.ArtAlbum
	header := gfx.R(area.X, area.Y, area.W, art+p.Margin)
	a.drawArt(c, s.stub.CoverArt, gfx.R(area.X+p.Margin, area.Y+p.Margin/2, art, art))
	x := area.X + p.Margin + art + p.Margin
	w := area.Right() - p.Margin - x
	y := area.Y + p.Margin/2
	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
	ft.Draw(c, x, y+ft.Ascent(), ft.Truncate(s.stub.Name, w), colText, header)
	y += ft.Height()
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(s.stub.Artist, w), colDim, header)
	y += fb.Height()
	fs.Draw(c, x, y+fs.Ascent(), fs.Truncate(s.summary(), w), colDim, header)

	body := gfx.R(area.X, header.Bottom(), area.W, area.H-header.H)
	switch {
	case s.album == nil && s.err != nil:
		a.drawCentered(c, body, "Couldn't load album: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case s.album == nil:
		a.drawCentered(c, body, "Loading…", colDim)
		return
	}
	songs := s.songs()
	s.list.Draw(c, body, albumActionRows+len(songs), p.RowH, func(i int, r gfx.Rect, focused bool) {
		switch i {
		case 0:
			f := a.F.Body
			iconText(c, f, iconPlay, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, "Play", colAccent, r)
		case 1:
			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
		default:
			so := songs[i-albumActionRows]
			a.drawTrackRow(c, r, so, false)
		}
	})
}

// drawTrackRow: "03  Title ...  3:45".
func (a *App) drawTrackRow(c *gfx.Canvas, r gfx.Rect, so subsonic.Song, current bool) {
	p := a.P
	f := a.F.Body
	col := colText
	if current {
		col = colAccent
	}
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	num := fmt.Sprintf("%02d", so.Track)
	if so.Track == 0 {
		num = "  "
	}
	x := r.X + p.Margin
	f.Draw(c, x, y, num, colDim, r)
	x += f.Measure("00") + p.Margin/2
	dur := clock(secs(so.Duration))
	dw := f.Measure(dur)
	f.Draw(c, r.Right()-p.Margin-dw, y, dur, colDim, r)
	f.Draw(c, x, y, f.Truncate(so.Title, r.Right()-p.Margin-dw-p.Margin/2-x), col, r)
}
```

`internal/ui/screens_home.go`:

```go
package ui

import (
	"context"
	"fmt"
	"math/rand/v2"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

type homeItem struct {
	label    string
	listType string // album list type; "" for Resume
}

var homeLists = []homeItem{
	{"Recently added", subsonic.ListNewest},
	{"Recently played", subsonic.ListRecent},
	{"Most played", subsonic.ListFrequent},
	{"Random albums", subsonic.ListRandom},
}

// HomeScreen is the root: Resume (if a saved queue exists) and album lists.
type HomeScreen struct {
	list   List
	resume *player.Resume
}

func NewHomeScreen() *HomeScreen { return &HomeScreen{} }

func (s *HomeScreen) Title() string { return "MiSTer Subsonic" }

func (s *HomeScreen) Enter(a *App) {
	if a.Player() == nil {
		return
	}
	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
			s.resume = r
		}
	})
}

func (s *HomeScreen) items() []homeItem {
	var out []homeItem
	if s.resume != nil {
		song := s.resume.Songs[s.resume.Index]
		out = append(out, homeItem{label: fmt.Sprintf("Resume: %s — %s", song.Title, song.Artist)})
	}
	return append(out, homeLists...)
}

func (s *HomeScreen) Handle(a *App, e input.Event) bool {
	items := s.items()
	if s.list.Handle(e, len(items)) {
		return true
	}
	if e.Kind != input.Press || e.Button != input.BtnA || len(items) == 0 {
		return false
	}
	it := items[s.list.Focus]
	if it.listType == "" {
		a.Player().ResumeFrom(s.resume)
		s.resume = nil
		a.Push(NewNowPlayingScreen())
		return true
	}
	a.Push(NewAlbumListScreen(it.label, it.listType))
	return true
}

func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	items := s.items()
	s.list.Draw(c, area, len(items), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
		col := colText
		if items[i].listType == "" {
			col = colAccent
		}
		a.drawTextRow(c, r, "", false, items[i].label, "", col)
	})
}

// AlbumListScreen pages through getAlbumList2 results.
type AlbumListScreen struct {
	title, listType string
	list            List
	albums          []subsonic.Album
	loading, more   bool
	err             error
}

const albumPage = 100

func NewAlbumListScreen(title, listType string) *AlbumListScreen {
	return &AlbumListScreen{title: title, listType: listType, more: true}
}

func (s *AlbumListScreen) Title() string { return s.title }

func (s *AlbumListScreen) Enter(a *App) { s.loadMore(a) }

func (s *AlbumListScreen) loadMore(a *App) {
	if s.loading || !s.more {
		return
	}
	s.loading, s.err = true, nil
	offset := len(s.albums)
	a.Load(s, func(ctx context.Context) (any, error) {
		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: s.listType, Size: albumPage, Offset: offset})
	}, func(v any, err error) {
		s.loading = false
		if err != nil {
			s.err = err
			return
		}
		page, _ := v.([]subsonic.Album)
		s.albums = append(s.albums, page...)
		// The random list has no end; one page is plenty.
		s.more = len(page) == albumPage && s.listType != subsonic.ListRandom
	})
}

func (s *AlbumListScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.albums)) {
		if s.list.NearEnd(len(s.albums)) {
			s.loadMore(a)
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		if s.err != nil && len(s.albums) == 0 {
			s.more = true
			s.loadMore(a)
			return true
		}
		if len(s.albums) > 0 {
			a.Push(NewAlbumScreen(s.albums[s.list.Focus]))
		}
		return true
	}
	return false
}

func (s *AlbumListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case len(s.albums) == 0 && s.loading:
		a.drawCentered(c, area, "Loading…", colDim)
		return
	case len(s.albums) == 0 && s.err != nil:
		a.drawCentered(c, area, "Couldn't load: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case len(s.albums) == 0:
		a.drawCentered(c, area, "No albums", colDim)
		return
	}
	s.list.Draw(c, area, len(s.albums), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		al := s.albums[i]
		sub := al.Artist
		if al.Year > 0 {
			sub += fmt.Sprintf(" · %d", al.Year)
		}
		a.drawTextRow(c, r, al.CoverArt, true, al.Name, sub, colText)
	})
}

// shuffleStart picks a random first track for shuffle play.
func shuffleStart(n int) int {
	if n <= 1 {
		return 0
	}
	return rand.IntN(n)
}
```

`internal/ui/screens_play.go`:

```go
package ui

import (
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

const (
	seekStep     = 10 * time.Second
	seekHoldStep = 30 * time.Second
)

// NowPlayingScreen is the full-screen player (spec §8.2).
type NowPlayingScreen struct{}

func NewNowPlayingScreen() *NowPlayingScreen { return &NowPlayingScreen{} }

func (s *NowPlayingScreen) Title() string { return "Now Playing" }
func (s *NowPlayingScreen) Enter(a *App)  {}

// playModes is the Select cycle: (shuffle, repeat).
var playModes = []struct {
	shuffle bool
	repeat  player.Repeat
	label   string
}{
	{false, player.RepeatOff, "In order"},
	{true, player.RepeatOff, "Shuffle"},
	{false, player.RepeatAll, "Repeat all"},
	{false, player.RepeatOne, "Repeat one"},
}

func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
	pl := a.Player()
	st := pl.State()
	switch e.Button {
	case input.BtnLeft, input.BtnRight:
		step := seekStep
		if e.Kind == input.Repeat {
			step = seekHoldStep
		}
		if e.Button == input.BtnLeft {
			step = -step
		}
		pl.Seek(st.Position + step)
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		pl.TogglePause()
	case input.BtnL:
		pl.Prev()
	case input.BtnR:
		pl.Next()
	case input.BtnY:
		a.Push(NewQueueScreen())
	case input.BtnSelect:
		cur := 0
		for i, m := range playModes {
			if m.shuffle == st.Shuffle && m.repeat == st.Repeat {
				cur = i
			}
		}
		m := playModes[(cur+1)%len(playModes)]
		pl.SetShuffle(m.shuffle)
		pl.SetRepeat(m.repeat)
		a.Toast("%s", m.label)
	default:
		return false
	}
	return true
}

func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	st := a.Player().State()
	song, ok := st.Current()
	if !ok {
		a.drawCentered(c, area, "Nothing playing", colDim)
		return
	}
	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
	barH := max(p.Margin/6, 3)
	// Height of the text block, used to centre everything vertically.
	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + fs.Height()
	art := min(p.ArtNow, area.H-2*p.Margin)
	var text gfx.Rect
	if p.W >= p.H*3/2 { // wide: art left, text right, both centred
		a.drawArt(c, song.CoverArt, gfx.R(area.X+p.Margin*2, area.Y+(area.H-art)/2, art, art))
		x := area.X + p.Margin*3 + art
		text = gfx.R(x, area.Y+(area.H-max(art, textH))/2, area.Right()-p.Margin*2-x, max(art, textH))
	} else { // narrow (CRT): art on top, text below, centred as a block
		art = min(art, area.H-textH-2*p.Margin)
		top := area.Y + (area.H-art-p.Margin/2-textH)/2
		a.drawArt(c, song.CoverArt, gfx.R(area.X+(area.W-art)/2, top, art, art))
		text = gfx.R(area.X+p.Margin, top+art+p.Margin/2, area.W-2*p.Margin, textH)
	}
	y := text.Y
	line := func(f *gfx.Font, s string, col gfx.Color) {
		f.Draw(c, text.X, y+f.Ascent(), f.Truncate(s, text.W), col, c.Bounds())
		y += f.Height()
	}
	line(ft, song.Title, colText)
	line(fb, song.Artist, colDim)
	line(fb, song.Album, colDim)
	y += p.Margin / 2
	line(fs, quality(song, st.Transcoded), colAccent)

	// Progress.
	y += p.Margin / 2
	d := secs(song.Duration)
	c.Fill(gfx.R(text.X, y, text.W, barH), colArtBg)
	if d > 0 {
		c.Fill(gfx.R(text.X, y, text.W*int(min(st.Position, d))/int(d), barH), colAccent)
	}
	y += barH + p.Margin/4
	times := clock(st.Position)
	if d > 0 {
		times += " / " + clock(d)
	}
	fs.Draw(c, text.X, y+fs.Ascent(), times, colDim, c.Bounds())
	y += fs.Height() + p.Margin/2

	mode := statusLabel(st.Status)
	for _, m := range playModes {
		if m.shuffle == st.Shuffle && m.repeat == st.Repeat && m.label != "In order" {
			mode += "  ·  " + m.label
		}
	}
	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
	y += fb.Height()
	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
		next := st.Queue[st.NextIndex]
		line(fs, "Next: "+next.Title+" — "+next.Artist, colDim)
	}
}

// QueueScreen lists the play queue.
type QueueScreen struct {
	list    List
	focused bool // focus placed on the current song once
}

func NewQueueScreen() *QueueScreen { return &QueueScreen{} }

func (s *QueueScreen) Title() string { return "Queue" }
func (s *QueueScreen) Enter(a *App)  {}

func (s *QueueScreen) Handle(a *App, e input.Event) bool {
	st := a.Player().State()
	if s.list.Handle(e, len(st.Queue)) {
		return true
	}
	if e.Kind != input.Press || len(st.Queue) == 0 {
		return false
	}
	switch e.Button {
	case input.BtnA:
		a.Player().Jump(s.list.Focus)
		a.Pop()
		return true
	case input.BtnX:
		title := st.Queue[s.list.Focus].Title
		a.Player().Remove(s.list.Focus)
		a.Toast("Removed %s", title)
		return true
	}
	return false
}

func (s *QueueScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	st := a.Player().State()
	if len(st.Queue) == 0 {
		a.drawCentered(c, area, "The queue is empty", colDim)
		return
	}
	if !s.focused && st.Index >= 0 {
		s.list.Focus, s.focused = st.Index, true
	}
	s.list.Draw(c, area, len(st.Queue), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		so := st.Queue[i]
		col := colText
		if i == st.Index {
			col = colAccent
		}
		a.drawTextRow(c, r, so.CoverArt, true, so.Title, so.Artist+" · "+clock(secs(so.Duration)), col)
	})
}

// MessageScreen shows a problem (no config, server unreachable) with an
// optional A-to-retry action.
type MessageScreen struct {
	title, message string
	retry          func()
}

func NewMessageScreen(title, message string, retry func()) *MessageScreen {
	return &MessageScreen{title: title, message: message, retry: retry}
}

func (s *MessageScreen) Title() string { return s.title }
func (s *MessageScreen) Enter(a *App)  {}

func (s *MessageScreen) Handle(a *App, e input.Event) bool {
	if e.Kind == input.Press && e.Button == input.BtnA && s.retry != nil {
		s.retry()
		return true
	}
	return false
}

func (s *MessageScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	f := a.F.Body
	y := area.Y + p.Margin
	for _, para := range wrap(f, s.message, area.W-2*p.Margin) {
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), para, colText, area)
		y += f.Height()
	}
	if s.retry != nil {
		y += p.Margin
		f.Draw(c, area.X+p.Margin, y+f.Ascent(), "Press A to try again", colAccent, area)
	}
}

// wrap breaks text into lines no wider than w (explicit newlines kept).
func wrap(f *gfx.Font, text string, w int) []string {
	var out []string
	for _, para := range splitLines(text) {
		line := ""
		for _, word := range splitWords(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if f.Measure(try) <= w || line == "" {
				line = try
				continue
			}
			out = append(out, line)
			line = word
		}
		out = append(out, line)
	}
	return out
}
```

`internal/ui/text.go`:

```go
package ui

import "strings"

func splitLines(s string) []string { return strings.Split(s, "\n") }
func splitWords(s string) []string { return strings.Fields(s) }
```

`internal/ui/theme.go`:

```go
// Package ui is the TV interface: a single-goroutine event loop that owns
// a stack of screens and renders them into a logical canvas, scaled to the
// display. Screens talk to the library, player and cover art through small
// interfaces so they can be tested with fakes and golden screenshots.
package ui

import (
	"mistersubsonic/internal/gfx"
)

// Profile holds the layout numbers for one kind of display (spec §8.1).
type Profile struct {
	Name     string
	W, H     int // logical canvas
	Margin   int // title-safe margin
	HeaderH  int
	RowH     int // single-line rows
	Row2H    int // rows with a second, dim line
	Thumb    int // list thumbnail size (0 = none)
	Title    int // font px
	Body     int
	Small    int
	ArtNow   int // Now Playing cover size
	ArtAlbum int // album header cover size
	MiniBarH int
}

var (
	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72}
	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 12, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24}
)

// PickProfile chooses the layout for a framebuffer (spec §8.1): "auto" picks
// CRT at 288 lines or fewer, otherwise HDMI.
func PickProfile(fbW, fbH int, override string) Profile {
	crt := override == "crt" || (override != "hdmi" && fbH <= gfx.CRTMaxLines)
	if !crt {
		return ProfileHDMI
	}
	p := ProfileCRT240
	if fbH == 288 || fbH == 576 {
		p.H = 288
	}
	return p
}

// Colors.
var (
	colBg      = gfx.RGB(0x10, 0x14, 0x18)
	colPanel   = gfx.RGB(0x1b, 0x21, 0x28)
	colText    = gfx.RGB(0xe8, 0xea, 0xed)
	colDim     = gfx.RGB(0x9a, 0xa0, 0xa6)
	colAccent  = gfx.RGB(0x4f, 0xc3, 0xf7)
	colFocus   = gfx.RGBA(0x4f, 0xc3, 0xf7, 0x40)
	colError   = gfx.RGB(0xef, 0x53, 0x50)
	colOverlay = gfx.RGBA(0, 0, 0, 0xB0)
	colArtBg   = gfx.RGB(0x2a, 0x31, 0x3a)
)
```

- [ ] **Step 4: Generate the golden screenshots, then LOOK at them**

Run: `go test -count=1 ./internal/ui/ -update && ls internal/ui/testdata/golden/`
Expected: 14 PNGs. The HDMI and CRT variants of home, albums, album, home-minibar, nowplaying, queue-toast and message.

Open at least `nowplaying-hdmi.png`, `nowplaying-crt.png`, `album-hdmi.png` and `queue-toast-hdmi.png` (a coding agent can read PNGs). Check each against this description:
- **Now Playing HDMI:**
  - a 400 px checkerboard cover on the left;
  - bold "Капитан Африка", then "Аквариум" and "Радио Африка" dim;
  - a cyan "FLAC 24/96";
  - a progress bar about a third full, with "1:15 / 4:00" below it;
  - a ▶ triangle and "Playing";
  - "Next: Время Луны — Аквариум".
- **CRT:** the same content stacked, cover centred on top.
- **Album:** the header reads "Аквариум"; a cover and a bold album name; the "1983 · 3 tracks · 10:15 · FLAC 24/96" line; a focused "▶ Play" row, then "Shuffle", then three numbered tracks with durations.
- **Queue:** two-line rows with thumbnails; a centred toast "Removed Время Луны"; the mini bar at the bottom.

Report anything that differs. The PNGs are committed.

- [ ] **Step 5: Run the tests**

Run: `go vet ./internal/ui/ && go test -race -count=3 ./internal/ui/ && go test -run '^$' -bench Repaint -benchtime 30x ./internal/ui/`
Expected:
- `ok`.
- The benchmark prints ms/frame. On the dev PC it's about 2–3 ms for HDMI at 1080p and about 0.3 ms for CRT. The on-device run is Task 10.

- [ ] **Step 6: Commit**

```bash
git add internal/ui
git commit -m "ui: event loop, HDMI/CRT profiles, Home/Album/Now Playing/Queue screens" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 9: The app binary, UI end-to-end check, Makefile and README

**Files:**
- Create: `cmd/mistersubsonic/main.go`, `cmd/mistersubsonic/keys.go`, `scripts/e2e-ui.sh`
- Test: `cmd/mistersubsonic/keys_test.go`
- Modify: `Makefile`, `README.md` (patch)

**Interfaces:**
- Consumes: everything above, plus Plan 1's `config`, `subsonic`, `audio`, `player`.
- Produces: `mistersubsonic`, with these flags:
  - `[-config path] [-display fbdev|viewer|headless] [-viewer-addr host:port] [-frames dir] [-profile auto|hdmi|crt] [-fb /dev/fb0] [-null] [-volume dB] [-keys script] [-exit-after dur]`
  - The default display is `fbdev` on linux/arm and `viewer` elsewhere.

**Flow:**
1. Open the display and inputs, and start the UI.
2. Show "Connecting…" while the server connects off the UI goroutine.
3. On success, start the art loader and the player, `Attach` them, and show Home.
4. On failure, show a Message screen with a hint and "A to try again".
5. With no config or an invalid one, show a Message screen pointing at the config file. The wizard comes in Plan 2b.

Player data lives next to the config: `state.json`, `cache/scrobbles.json` and `cache/art/`. Fallback fonts come from `<config dir>/fonts`.

- [ ] **Step 1: Write the failing test**

`cmd/mistersubsonic/keys_test.go`:

```go
package main

import (
	"testing"
	"time"

	"mistersubsonic/internal/input"
)

func TestParseKeys(t *testing.T) {
	got, err := parseKeys("a:2s, down ,a")
	if err != nil {
		t.Fatal(err)
	}
	want := []scripted{{input.BtnA, 2 * time.Second}, {input.BtnDown, 400 * time.Millisecond}, {input.BtnA, 400 * time.Millisecond}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if _, err := parseKeys("jump"); err == nil {
		t.Fatal("unknown button accepted")
	}
	if _, err := parseKeys("a:soon"); err == nil {
		t.Fatal("bad duration accepted")
	}
}
```

Run: `go test ./cmd/mistersubsonic/`
Expected: build failure (no Go files / `undefined: parseKeys`).

- [ ] **Step 2: Implement**

`cmd/mistersubsonic/main.go`:

```go
// Command mistersubsonic is the MiSTer Subsonic app: the TV interface on the
// framebuffer (on a MiSTer) or in a browser (-display viewer, for development).
//
//	mistersubsonic                                  # MiSTer: /dev/fb0, evdev, config in /media/fat/mistersubsonic
//	mistersubsonic -config config.toml -display viewer   # PC: open the printed URL
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/devview"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
	"mistersubsonic/internal/ui"
)

type flags struct {
	config, display, viewerAddr, frames, profile, fbdev, keys string
	null                                                      bool
	volume                                                    float64
	exitAfter                                                 time.Duration
}

func main() {
	var f flags
	flag.StringVar(&f.config, "config", config.DefaultPath, "config file")
	flag.StringVar(&f.display, "display", defaultDisplay(), "fbdev | viewer | headless")
	flag.StringVar(&f.viewerAddr, "viewer-addr", "127.0.0.1:8090", "listen address for -display viewer")
	flag.StringVar(&f.frames, "frames", "", "with -display headless: write every frame as a PNG here")
	flag.StringVar(&f.profile, "profile", "", "layout: auto | hdmi | crt (default: config display.profile)")
	flag.StringVar(&f.fbdev, "fb", "/dev/fb0", "framebuffer device")
	flag.BoolVar(&f.null, "null", false, "use the null audio device (silent)")
	flag.StringVar(&f.keys, "keys", "", `scripted button presses for testing, e.g. "a:2s,a,a" (see keys.go)`)
	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 with -display viewer")
	flag.Parse()
	if err := run(f); err != nil {
		fmt.Fprintln(os.Stderr, "mistersubsonic:", err)
		os.Exit(1)
	}
}

// defaultDisplay: the framebuffer on the MiSTer (linux/arm), the browser viewer elsewhere.
func defaultDisplay() string {
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm" {
		return "fbdev"
	}
	return "viewer"
}

func run(f flags) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if f.exitAfter > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.exitAfter)
		defer cancel()
	}
	var script []scripted
	if f.keys != "" {
		var err error
		if script, err = parseKeys(f.keys); err != nil {
			return err
		}
	}

	cfg, warns, cfgErr := config.Load(f.config)
	if cfg == nil {
		cfg = config.Default()
	}
	for _, w := range warns {
		log.Printf("config: %s", w)
	}
	dataDir := filepath.Dir(f.config)

	// Display and layout.
	profileName := cfg.Display.Profile
	if f.profile != "" {
		profileName = f.profile
	}
	var disp gfx.Display
	var inputs []<-chan input.Event
	switch f.display {
	case "fbdev":
		fb, err := gfx.OpenFB(f.fbdev)
		if err != nil {
			return err
		}
		disp = fb
		mgr := input.NewManager(input.ManagerOptions{Grab: true})
		defer mgr.Close()
		inputs = append(inputs, mgr.Events())
	case "viewer":
		prof := ui.PickProfile(1280, 720, profileName)
		v := devview.New(prof.W, prof.H)
		if err := v.Listen(f.viewerAddr); err != nil {
			return err
		}
		fmt.Println("MiSTer Subsonic dev viewer:", v.URL())
		disp = v
		inputs = append(inputs, v.Events())
	case "headless":
		prof := ui.PickProfile(1280, 720, profileName)
		disp = gfx.NewHeadless(prof.W, prof.H, f.frames)
	default:
		return fmt.Errorf("unknown -display %q", f.display)
	}
	defer disp.Close()
	if len(script) > 0 {
		inputs = append(inputs, playKeys(script))
	}
	pw, ph := disp.Size()
	prof := ui.PickProfile(pw, ph, profileName)

	// Volume: sound safety on desktop runs (start quiet unless asked).
	vol := cfg.Playback.VolumeDB
	switch {
	case !math.IsNaN(f.volume):
		vol = f.volume
	case f.display == "viewer" && !f.null && vol > -30:
		vol = -30
		fmt.Println("starting at -30 dB (use -volume to change)")
	}

	// The player and art loader are created once the server connects.
	var (
		pl     *player.Player
		loader *art.Loader
		app    *ui.App
	)

	dev := cfg.Playback.ALSADevice
	if dev == "default" {
		dev = ""
	}
	out, err := audio.OpenDevice(audio.DeviceOptions{Name: dev, Null: f.null})
	if err != nil {
		return err
	}
	defer out.Close()
	eng := audio.NewEngine(audio.EngineOptions{Output: out})
	defer eng.Close()

	pctx, cancelPlayer := context.WithCancel(context.Background())
	playerDone := make(chan struct{})
	defer func() {
		cancelPlayer()
		if pl != nil {
			<-playerDone
		}
		if loader != nil {
			loader.Close()
		}
	}()

	// connectServer runs off the UI goroutine; the caller attaches the result.
	connectServer := func() (*subsonic.Client, error) {
		srv, _ := cfg.ActiveServer()
		c, err := subsonic.New(subsonic.Options{
			BaseURL: srv.URL,
			Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
				Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
			CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
		})
		if err != nil {
			return nil, err
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if _, err := c.Connect(cctx); err != nil {
			return nil, err
		}
		return c, nil
	}
	// startPlayer runs on the UI goroutine after a successful connect.
	startPlayer := func(a *ui.App, c *subsonic.Client) {
		disk, err := cache.Open(filepath.Join(dataDir, "cache", "art"), int64(cfg.Cache.CoverArtMB)<<20)
		if err != nil {
			log.Printf("art cache disabled: %v", err)
			disk = nil
		}
		loader = art.New(art.Options{Fetch: art.HTTPFetcher(c), Disk: disk, Ready: a.ArtReady})
		pl = player.New(player.Options{
			Engine: eng, API: c,
			Open: player.NewOpener(c, player.StreamSettings{
				TranscodeFormat: cfg.Playback.TranscodeFormat, TranscodeBitrate: cfg.Playback.TranscodeBitrate,
				WindowBytes: int64(cfg.Playback.BufferMB) << 20,
			}),
			ReplayGain: cfg.Playback.ReplayGain, Scrobble: cfg.Playback.Scrobble, VolumeDB: vol,
			ResumePath: filepath.Join(dataDir, "state.json"), ScrobblePath: filepath.Join(dataDir, "cache", "scrobbles.json"),
		})
		go func() { pl.Run(pctx); close(playerDone) }()
		a.Attach(c, pl, loader)
	}

	app, err = ui.New(ui.Options{
		Display: disp, Profile: prof, Inputs: inputs,
		FallbackFonts: filepath.Join(dataDir, "fonts"),
		Start: func(a *ui.App) {
			var connect func()
			connect = func() {
				if cfgErr != nil {
					a.Replace(ui.NewMessageScreen("Setup needed", configMessage(f.config, cfgErr), nil))
					return
				}
				if _, ok := cfg.ActiveServer(); !ok {
					a.Replace(ui.NewMessageScreen("Setup needed", "Add a [[server]] to "+f.config+".", nil))
					return
				}
				a.Replace(ui.NewMessageScreen("Connecting…", "Connecting to the server…", nil))
				go func() {
					c, err := connectServer()
					a.Post(func() {
						if err != nil {
							srv, _ := cfg.ActiveServer()
							a.Replace(ui.NewMessageScreen("Can't reach the server",
								fmt.Sprintf("%s: %s.\n%s", srv.URL, subsonic.Classify(err), hint(err)), connect))
							return
						}
						if pl == nil { // first successful connect
							startPlayer(a, c)
						}
						a.Replace(ui.NewHomeScreen())
					})
				}()
			}
			connect()
		},
	})
	if err != nil {
		return err
	}
	return app.Run(ctx)
}

func configMessage(path string, err error) string {
	if errors.Is(err, config.ErrNotFound) {
		return "No config file yet. Create " + path + " with your server (see config.example.toml), then restart."
	}
	return "The config file has a problem:\n" + err.Error()
}

func hint(err error) string {
	switch subsonic.Classify(err) {
	case subsonic.KindTLS:
		return "For a self-signed certificate set ca_file (or insecure_skip_verify) in the config."
	case subsonic.KindAuth:
		return "Check the username and password in the config."
	case subsonic.KindPlaintextRefused:
		return "This server needs the plain password: use https, or set allow_plaintext_password."
	case subsonic.KindUnreachable, subsonic.KindTimeout:
		return "Check that the server is running and the URL is right."
	}
	return ""
}
```

`cmd/mistersubsonic/keys.go`:

```go
package main

import (
	"fmt"
	"strings"
	"time"

	"mistersubsonic/internal/input"
)

var keyNames = map[string]input.Button{
	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
}

type scripted struct {
	b    input.Button
	wait time.Duration // pause before this press
}

// parseKeys reads a -keys script: comma-separated button names, each
// optionally followed by ":<duration>" to wait before pressing it, e.g.
// "a:2s,a:1s,a,a:1s". The default wait is 400 ms.
func parseKeys(s string) ([]scripted, error) {
	var out []scripted
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, wait, found := strings.Cut(part, ":")
		b, ok := keyNames[name]
		if !ok {
			return nil, fmt.Errorf("-keys: unknown button %q", name)
		}
		d := 400 * time.Millisecond
		if found {
			var err error
			if d, err = time.ParseDuration(wait); err != nil {
				return nil, fmt.Errorf("-keys: %q: %v", part, err)
			}
		}
		out = append(out, scripted{b, d})
	}
	return out, nil
}

// playKeys sends the script as press/release pairs.
func playKeys(script []scripted) <-chan input.Event {
	ch := make(chan input.Event, 4)
	go func() {
		for _, k := range script {
			time.Sleep(k.wait)
			ch <- input.Event{Button: k.b, Kind: input.Press}
			ch <- input.Event{Button: k.b, Kind: input.Release}
		}
	}()
	return ch
}
```

- [ ] **Step 3: UI end-to-end script, Makefile and README**

`scripts/e2e-ui.sh`:

```sh
#!/bin/sh
# End-to-end check of the app: the mock server serves an album, the app runs
# headless on the NULL audio device (silent), scripted keys go
# Home -> Recently added -> album -> Play, and every frame is saved as PNG.
# Passes if the app exits cleanly, drew frames and streamed the first track.
set -eu
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
mock=""
cleanup() { [ -n "$mock" ] && kill "$mock" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

go build -o "$tmp/" ./cmd/mistersubsonic ./tools/mocksubsonic
mkdir "$tmp/music" "$tmp/frames"
cp internal/audio/testdata/tone-44k16.flac "$tmp/music/01-a.flac"
cp internal/audio/testdata/tone-96k24.flac "$tmp/music/02-b.flac"
port=${E2E_UI_PORT:-14535}
cat > "$tmp/config.toml" <<CFG
[[server]]
name = "mock"
url = "http://127.0.0.1:$port"
username = "test"
password = "test"
CFG

"$tmp/mocksubsonic" -dir "$tmp/music" -addr "127.0.0.1:$port" > "$tmp/mock.log" 2>&1 &
mock=$!
sleep 0.5

"$tmp/mistersubsonic" -config "$tmp/config.toml" -display headless -frames "$tmp/frames" -null \
  -keys "a:1500ms,a:800ms,a:800ms" -exit-after 6s > "$tmp/app.log" 2>&1 || {
  echo "e2e-ui failed: app exited with an error" >&2
  cat "$tmp/app.log" >&2
  exit 1
}
frames=$(ls "$tmp/frames" | wc -l)
if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log"; then
  echo "e2e-ui failed: $frames frames; mock log:" >&2
  cat "$tmp/mock.log" >&2
  exit 1
fi
echo "e2e-ui ok: $frames frames rendered; Home -> album -> Play streamed the first track (null device)"
```

Apply the Makefile and README changes. The Makefile gains `e2e` running both scripts, a `viewer` target, the ARM app and `ui.test` builds, and deploy-dev copying them. The README gains the app status, dev viewer usage and the Noto OFL credit.

````diff
diff --git a/Makefile b/Makefile
index bd281f6..8dab075 100644
--- a/Makefile
+++ b/Makefile
@@ -6,7 +6,8 @@ DEVDIR  := /media/fat/mistersubsonic/dev
 ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
            CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"
 
-.PHONY: build test vet e2e mister mister-test deploy-dev vendor-check clean
+
+.PHONY: build test vet e2e viewer mister mister-test deploy-dev vendor-check clean
 
 build:
 	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...
@@ -20,21 +21,30 @@ vet:
 # Silent end-to-end run (mock server + CLI on the null audio device).
 e2e:
 	./scripts/e2e-smoke.sh
+	./scripts/e2e-ui.sh
+
+# Run the app on this PC in the browser viewer (starts at -30 dB).
+viewer:
+	$(GO) run ./cmd/mistersubsonic -config $(or $(CONFIG),config.toml) -display viewer
 
 # Cross-compiled binaries for the MiSTer (ARMv7, glibc <= 2.31).
 mister:
 	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w" -o $(BIN)/arm/mss-cli ./cmd/mss-cli
+	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w" -o $(BIN)/arm/mistersubsonic ./cmd/mistersubsonic
 	./scripts/check-glibc.sh $(BIN)/arm/mss-cli
+	./scripts/check-glibc.sh $(BIN)/arm/mistersubsonic
 
 # Audio test binary for on-device checks and benchmarks.
 mister-test:
 	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
+	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/ui.test ./internal/ui
 	./scripts/check-glibc.sh $(BIN)/arm/audio.test
+	./scripts/check-glibc.sh $(BIN)/arm/ui.test
 
 # Copies the dev tools to the MiSTer (default root password is "1").
 deploy-dev: mister mister-test
 	ssh root@$(MISTER) mkdir -p $(DEVDIR)/testdata
-	scp $(BIN)/arm/mss-cli $(BIN)/arm/audio.test root@$(MISTER):$(DEVDIR)/
+	scp $(BIN)/arm/mss-cli $(BIN)/arm/mistersubsonic $(BIN)/arm/audio.test $(BIN)/arm/ui.test root@$(MISTER):$(DEVDIR)/
 	scp internal/audio/testdata/*.flac internal/audio/testdata/*.wav internal/audio/testdata/*.mp3 root@$(MISTER):$(DEVDIR)/testdata/
 
 vendor-check:
diff --git a/README.md b/README.md
index 0ded1a7..255bd75 100644
--- a/README.md
+++ b/README.md
@@ -3,8 +3,10 @@
 A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
 Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.
 
-Status: **playback core only** (streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
-scrobbling, resume) with a terminal dev tool. The TV UI and MiSTer launcher come next.
+Status: **playback core plus a first TV interface**: Home (resume, recent/most played/random
+albums), album pages, Now Playing and the queue, on HDMI or CRT layouts, driven by a controller
+or keyboard. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek, scrobbling and resume work.
+Still to come: artists, playlists, starred, search, the setup wizard and the MiSTer launcher.
 On-device (MiSTer) checks are still pending; see `docs/spikes.md`.
 
 ## Developing
@@ -14,7 +16,8 @@ Regenerating audio fixtures needs sox, flac and ffmpeg.
 
 ```sh
 make test        # unit tests (race detector); no network, server or sound device needed
-make e2e         # silent end-to-end run against a mock server
+make e2e         # silent end-to-end runs (CLI and app) against a mock server
+make viewer      # run the app in your browser (CONFIG=path/to/config.toml; starts at -30 dB)
 make mister      # ARMv7 binaries in bin/arm/ (checks glibc <= 2.31)
 make deploy-dev MISTER=mister.local   # copy dev tools to /media/fat/mistersubsonic/dev
 ```
@@ -41,4 +44,5 @@ password = "…"   # or token + salt, or api_key
 ## License
 
 GPL-3.0. Bundles miniaudio (public domain / MIT-0), the speexdsp resampler (BSD, see
-`third_party/speexdsp/COPYING`) and the Mozilla CA bundle from curl.se (MPL-2.0).
+`third_party/speexdsp/COPYING`), the Mozilla CA bundle from curl.se (MPL-2.0) and the Noto Sans
+fonts (SIL Open Font License 1.1, see `internal/gfx/fonts/OFL.txt`).
````

Save the diff block above to `"$TMPDIR/task9.patch"`, without the fence lines, and apply it:

```bash
chmod +x scripts/e2e-ui.sh
git apply "$TMPDIR/task9.patch"
```

- [ ] **Step 4: Verify**

Run: `go mod tidy && go vet ./... && go test -race -count=1 ./... && make e2e && make mister mister-test` (with `ZIG=` pointing at zig 0.16.0 if it isn't on PATH).

Expected:
- All packages report `ok`.
- `e2e ok: 4 tracks …`.
- `e2e-ui ok: N frames rendered; Home -> album -> Play streamed the first track (null device)`, with N ≥ 5.
- Four `needs glibc 2.29 (<= 2.31) ok` lines, including `bin/arm/mistersubsonic` and `bin/arm/ui.test`.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic scripts/e2e-ui.sh Makefile README.md go.mod go.sum
git commit -m "app: mistersubsonic binary, headless UI e2e, dev viewer target" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 6: Silent check against the user's real server**

Build a temporary config OUTSIDE the repo from `.env`, as Plan 1's R10 did. Never print it and never commit it:
- `NAVIDROME_URL`
- `NAVIDROME_USERNAME`
- `NAVIDROME_TOKEN`
- `NAVIDROME_SALT`, using `token`/`salt` keys in the config.

Then run:

```bash
bin/mistersubsonic -config $TMP/config.toml -display headless -frames $TMP/frames -null -keys "a:3s,a:3s,a:3s" -exit-after 20s
```

Look at the last frame. It should show Now Playing with the real cover and a FLAC badge. Delete `$TMP`, because the frames contain the user's library. Record "real server: OK/problem" in the task report only; it doesn't go in the repo.

- [ ] **Step 7: Offer the user a real try (audible; ask first)**

Tell the user they can run `make viewer CONFIG=<their config>` and open the printed URL. It plays sound through their default device, starting at −30 dB. Don't run it yourself.

### Task 10: On-device check (when the MiSTer is available)

This task needs the MiSTer. If it isn't reachable, record "pending — MiSTer unavailable" in `docs/spikes.md` under a new heading `## Plan 2a on the MiSTer`, and stop.

**Files:**
- Modify: `docs/spikes.md`

- [ ] **Step 1: Deploy and benchmark (silent)**

Run `make deploy-dev MISTER=<address>`, then on the device:

```bash
cd /media/fat/mistersubsonic/dev && ./ui.test -test.run '^$' -test.bench Repaint -test.benchtime 20x
```

Record the ms/frame for each case.

Decision rules:
- **`albums-hdmi-1080p` ≤ 30 ms:** write "§8.1 confirmed".
- **Above 30 ms:** write the number down and tell the user. The fixes belong in Plan 2b: dirty-rectangle presents, integer-only scaling, or a smaller HDMI framebuffer via `fb_size=2` in `MiSTer.ini`.

- [ ] **Step 2: Run the app on the TV (ask the user first)**

Ask the user to start `/media/fat/mistersubsonic/dev/mistersubsonic -config <their config>` from SSH while watching the TV. It takes over the framebuffer and grabs the controllers. Pressing Start plays sound, so they should keep the volume low.

Ask them to report:
- Does the screen fill correctly on HDMI and on CRT (if they use one)?
- Does the controller work, including their MiSTer mapping?
- Does it exit with B held for 2 s and then A?
- Do covers appear?

Record the answers.

- [ ] **Step 3: Commit**

```bash
git add docs/spikes.md
git commit -m "docs: record Plan 2a on-device results" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

- Plan 2b adds the remaining screens and the setup wizard, and picks up the remaining Plan 2 items from `docs/superpowers/plan-1-followups.md`.
- If Task 10's benchmark exceeds 30 ms, Plan 2b starts with the render-cost fixes.
