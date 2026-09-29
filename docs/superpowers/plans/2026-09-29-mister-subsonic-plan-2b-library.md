# MiSTer Subsonic — Plan 2b: library screens and actions

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the whole library on the TV:
- **Browsing:** a home feed; artists A–Z with letter jumps; albums A–Z, by year and by genre; playlists; starred items; search with an on-screen keyboard.
- **Actions:** the X context menu (play now, play next, add to queue, star, go to artist or album) and Select to shuffle a list.
- **Now Playing:** volume, star and the insecure badge.

On HDMI it has a sidebar and cover grids; on a CRT, lists that stay inside the title-safe area.

**Architecture:** all new code is in `internal/ui`, on top of Plan 2a's event loop, except for:
- Task 1: player fixes in `internal/player`
- Task 2: typed text and a Queue key in `internal/input` and `internal/devview`
- Task 10: wiring in `cmd/mistersubsonic` and `tools/mocksubsonic`

The main pieces:
- **Views.** Reusable album, artist and song views that draw as a grid (HDMI) or a list (CRT) and own A and X for their items.
- **Containers.** Screens can show other screens inside themselves: the sidebar root holds the sections, the tabbed screen holds its tabs. Loads run under the owner through `App.entry`.
- **Timers and loads.** Timers (`App.After`) and cancellable loads (`App.LoadCancel`) give Search its 300 ms debounce without any I/O on the UI goroutine.

**Tech Stack:** Go 1.26+, no new dependencies. zig 0.16.0 for the ARM build.

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`, mostly §8 (UI). Also read these two files. This plan picks up their Plan 2 items; the rest are listed under "After this plan".
- `docs/superpowers/plan-2a-followups.md`
- `docs/superpowers/plan-1-followups.md`

**Scope:**

| | |
|---|---|
| **Plan 2a (done)** | The event loop, HDMI/CRT profiles, Home, album pages, Now Playing, the queue, the dev viewer |
| **This plan (2b)** | Everything in the Goal above, plus Plan 2 player fixes (queue edges, seeking while a track opens, one toast per failed song) |
| **Plan 2c** | The setup wizard, Settings (servers and switching, playback, display, about, Exit), saving volume to the config, the screensaver, and the "server unreachable" screen with Retry · Switch server · Settings |
| **Plan 3** | The launcher, KD_GRAPHICS, BGM/SAM, the framebuffer watchdog, memory and speed work on the device, releases |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks passed:** `go test -race ./...`, `make e2e` (the Plan 1 CLI check plus the UI check, which now also searches) and the ARM build (glibc 2.29 ≤ 2.31).
- **Real-server run:** a silent headless run against a real Navidrome. The feed, artists, albums, playlists, starred items and search all showed the real library.
- **Replay:** the blocks were replayed task by task on a fresh clone of `plan-2b-screens`. Each task's tests failed before its code and passed after, and each task ended with the prototype's tree, golden screenshots included, byte for byte.

Copy blocks exactly. Patches must apply cleanly with `git apply`; if one doesn't, the tree is not where the previous task left it.

## Global Constraints

- **Toolchain:** Go 1.26+ (`go.mod` says `go 1.26.0`). No new modules. No cgo outside `internal/audio`.
- **The UI goroutine never does I/O** (spec §8.2):
  - Server calls go through `App.Load` or `App.LoadCancel`, and results for a popped screen are dropped.
  - Delays go through `App.After`.
  - Screens shown inside other screens load under their owner (the `owner` interface).
- **Layout profiles** (exact values in `theme.go`):
  - **HDMI:** 1280×720 logical, a 250 px sidebar, 170 px covers in grids.
  - **CRT:** 320×240 (or 288) lists with thumbnails, no sidebar. Content stays inside the title-safe area: 12 lines top and bottom (`SafeY`) and 16 px left and right (`Margin`), about 5% (spec §8.2). Panels still run to the edges.
- **Controls** (spec §8.3):

  | Button | Browsing | Now Playing |
  |---|---|---|
  | D-pad | move; Left from a section's first column enters the HDMI sidebar | ←/→ seek ±10 s (held ±30 s), ↑/↓ volume ±1 dB |
  | A | open / play | play/pause |
  | B | back; in an HDMI section: the sidebar; hold 2 s on the root: exit prompt | back |
  | X | item menu | star/unstar the song |
  | Y | Now Playing | queue |
  | L/R | page; letter jump on Artists | previous/next track |
  | Start | play/pause | play/pause |
  | Select | shuffle the list | shuffle/repeat modes |

- **Keyboard:** arrows; Enter = A; Esc/Backspace = B; Tab = X; N = Y; **Q = queue**; PgUp/PgDn = L/R; Space = Start. In Search, typed characters go into the query (N, Q, Space and Backspace included), and Backspace in an empty query goes back.
- **Paging and loads:**
  - Album lists load 100 per page.
  - Feed rows load 20 albums each.
  - Search loads 50 per kind per request, starts 300 ms after the last keystroke, and cancels the previous request.
  - `getArtists` is fetched once per connection and kept in `App`.
- **Credentials** never appear in error text. Screens show errors only as `subsonic.Classify(err)`.
- **🔇 Sound safety:**
  - Tests and scripts use fakes or `-null`.
  - Never play sound on the user's devices without asking first.
  - Desktop runs start at −30 dB unless `-volume` is given.
- **Secrets** from `.env` are never printed, committed or written into reports. Temporary configs go outside the repo and are deleted afterwards.
- **Golden screenshots are exact pixels.** Regenerate them with `go test ./internal/ui -update` only in the step that says so, and look at every changed PNG before committing.

## Review Focus

The spec implies these five situations but says nothing about them. Each one is the most likely way the app would break for a real user, so each gets its own test in the task that owns the code:

1. **A big library:** thousands of artists and hundreds of albums, browsed at key-repeat speed. Letter jumps land on the right letter, and each page of a list is requested once. Tests: `TestArtistsLetterJumpInAHugeLibrary` and `TestHeldScrollLoadsEachPageOnce` (Task 6).
2. **Titles in any script:** CJK, Turkish, combining marks, emoji and ZWJ sequences, bidi controls. The marquee steps through them without breaking a rune and never draws outside its width. Test: `TestMarqueeHandlesAnyScript` (Task 3).
3. **The network dropping mid-browse:** the screen shows the problem and the next action tries again. Search is the case the user is most likely to hit. Test: `TestSearchErrorThenRetry` (Task 8), alongside each screen's own error-and-retry test.
4. **Buttons pressed at the wrong time:** every button on every screen, in both layouts, with a normal, an empty and a failing library, before and after its loads finish. Nothing may panic or empty the screen stack. Test: `TestEveryScreenSurvivesEveryButton` (Task 9).
5. **Items without IDs** (servers without OpenSubsonic, playlist entries): menus never offer "Go to album/artist" when there is nowhere to go. Test: `TestMenusWithoutIDs` (Task 6).

## Decisions this plan makes (the spec is silent or leaves room)

- **Volume in Now Playing.** Up/Down change the volume by 1 dB. The spec asks for a volume indicator but assigns no button, and on the MiSTer there is no other volume control. It isn't saved yet; Plan 2c's Settings saves it.
- **The HDMI sidebar focus model:**
  - Left from a section's first column, or B, enters the sidebar.
  - Right or A goes back into the section.
  - Sections keep their state while the user switches.
  - The app starts in the Home section's content.
- **Select on album lists, feed rows and artists** shuffles a random sample of up to 10 albums (one `getAlbum` each), not the whole library.
- **The on-screen keyboard:**
  - Alphabetical Latin and Cyrillic layouts (easier to hunt with a D-pad than QWERTY/ЙЦУКЕН), a digit row, and Space · Del · layout switch · Clear.
  - X is a Del shortcut.
  - Right at the right edge, or Down past the bottom row, enters the results; B in the results comes back.
- **Star state:** a star/unstar made in this session wins over what a screen loaded earlier (`App.stars`). Starred lists keep an unstarred item until they reload.
- **Playlists** have no Star action (Subsonic can't star playlists).
- **Removing the playing song:**
  - While paused, the next song stays paused.
  - At the end of the queue under repeat-all, it wraps to the top; otherwise it stops with nothing selected, and Start then plays the queue from the top.
- **A failed prefetch** shows no toast. The song is opened again when its turn comes, and that attempt reports.
- **The dev viewer** accepts any IP-literal Host header. DNS rebinding always uses a hostname, and this makes `-viewer-addr :8090` usable from another device. A non-loopback bind prints a warning.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/player/player.go` | queue edges, seek while opening, one toast per failed song | 1 |
| `internal/input/{input,evmap}.go` | `Event.Rune` (typed text, US layout with Shift), `BtnQueue` | 2 |
| `internal/devview/{devview.go,page.html}` | text keys (`t=`), Q key, IP-literal hosts, loopback URL, `Exposed` | 2 |
| `cmd/mistersubsonic/keys.go` | `-keys` typed text (`'abc`), `queue` | 2 |
| `internal/ui/app.go` | the wider `Library`/`Player`, `TextInput`, `owner`, `After`, `LoadCancel`, the safe area, the marquee (`drawFit`), the insecure badge, the Q key | 3 |
| `internal/ui/{grid,tabs,menu,star}.go` | the cover grid, tab row, context menu overlay, star state | 4 |
| `internal/ui/screens_play.go` | Now Playing volume/star/badge and CRT layout; the queue's X menu | 5 |
| `internal/ui/{views,menus,tabbed,screens_artists}.go` | the album/artist/song views, item menus and play helpers, the tabbed container, Artists/Artist, Albums (tabs), Genres | 6 |
| `internal/ui/{screens_playlists,screens_starred}.go` | Playlists, a playlist, Starred | 7 |
| `internal/ui/{keyboard,screens_search}.go` | the on-screen keyboard, Search | 8 |
| `internal/ui/screens_root.go` | the HDMI sidebar root and home feed | 9 |
| `cmd/mistersubsonic/main.go`, `tools/mocksubsonic/main.go`, `scripts/e2e-ui.sh`, `README.md` | wiring, mock endpoints, the e2e path through Search, docs | 10 |

---

### Task 1: Player queue edges and seeking while a track opens

**Files:**
- Modify: `internal/player/player.go`
- Test: `internal/player/edges_test.go` (new)

**Interfaces:**
- **Consumes:** Plan 1/2a `Player` internals (`startAt`, `reopenAt`, `onOpened`, `curSrc`).
- **Produces:** the same API, with new behaviour:
  - `Remove` of the playing song keeps a paused player paused. It wraps under repeat-all, or stops with `Index == -1`.
  - `TogglePause` on a stopped queue with nothing selected plays from the top.
  - `PlayNext`/`Enqueue` ignore empty lists.
  - Unpausing doesn't flash "Buffering".
  - A `Seek` while the track is still opening lands where the user asked. Raw streams get the seek once they open; a transcoded stream reopens at the target.
  - A failed prefetch no longer emits an `Error`: the retry reports it, so one toast per song.

These are the plan-1 follow-ups the queue and Now Playing screens now reach (`docs/superpowers/plan-1-followups.md`, "Plan 2 (UI) must handle").

- [ ] **Step 1: Write the failing tests**

`internal/player/edges_test.go` (new file):

```go
package player

import (
	"context"
	"testing"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/subsonic"
)

// Queue edits the UI's Queue screen can make (plan-1 follow-ups).

func TestRemoveCurrentWhilePausedStaysPaused(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 0)
	h.playAndStart(1)
	h.p.TogglePause()
	h.p.Remove(0)
	h.waitFor("next song opened", func() bool { return h.eng.playCount() == 2 })
	if st := h.p.State(); st.Status != Paused || !h.eng.paused {
		t.Fatalf("status %v, engine paused %v; want paused", st.Status, h.eng.paused)
	}
	tr := h.eng.lastPlayed()
	h.eng.events <- audio.Event{Kind: audio.EventStarted, TrackID: tr.ID}
	h.p.do(func() {})
	if st := h.p.State(); st.Status != Paused {
		t.Fatalf("Started turned a paused player into %v", st.Status)
	}
}

func TestRemoveLastCurrentStopsWithNothingSelected(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 2)
	h.playAndStart(1)
	h.p.Remove(2)
	st := h.p.State()
	if st.Status != Stopped || st.Index != -1 || len(st.Queue) != 2 {
		t.Fatalf("after removing the last, current song: %+v", st)
	}
	h.p.TogglePause() // Start on a stopped queue plays it from the top
	h.waitFor("first song opened", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "sa" {
		t.Fatalf("playing %v, want sa", cur.ID)
	}
}

func TestRemoveLastCurrentWrapsUnderRepeatAll(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(3, 100), 2)
	h.playAndStart(1)
	h.p.SetRepeat(RepeatAll)
	h.p.Remove(2)
	h.waitFor("wrapped to the first song", func() bool { return h.eng.playCount() == 2 })
	if cur, _ := h.p.State().Current(); cur.ID != "sa" {
		t.Fatalf("playing %v, want sa", cur.ID)
	}
}

func TestEmptyPlayNextAndEnqueueAreIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNext(nil)
	h.p.Enqueue([]subsonic.Song{})
	if st := h.p.State(); len(st.Queue) != 0 || st.Status != Stopped || st.Index != -1 {
		t.Fatalf("state after empty adds: %+v", st)
	}
	if len(h.opener.callList()) != 0 {
		t.Fatal("an empty add opened a stream")
	}
}

func TestUnpauseDoesNotFlickerToBuffering(t *testing.T) {
	h := newHarness(t, nil)
	h.p.PlayNow(songs(1, 300), 0)
	a := h.playAndStart(1)
	h.tickAt(a.ID, time.Second)
	h.p.TogglePause()
	for i := 0; i < 8; i++ { // 2 s paused: the position doesn't move
		h.tickAt(a.ID, time.Second)
	}
	h.p.TogglePause()
	h.tickAt(a.ID, time.Second) // first tick after unpause: audio not moved yet
	if st := h.p.State(); st.Status != Playing {
		t.Fatalf("status right after unpause = %v, want playing", st.Status)
	}
}

// A seek while the track is still opening (resume, or a seek right after
// Play) must land where the user asked, not where the open started.
func TestSeekWhileOpeningAimsTheOpen(t *testing.T) {
	h := newHarness(t, nil)
	block := make(chan struct{})
	open := h.opener.open
	h.p.o.Open = func(ctx context.Context, s subsonic.Song, off time.Duration, pre bool) (Opened, error) {
		<-block
		return open(ctx, s, off, pre)
	}
	h.p.ResumeFrom(&Resume{Songs: songs(1, 300), Index: 0, Position: 60 * time.Second})
	h.p.Seek(120 * time.Second)
	close(block)
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() == 1 })
	h.waitFor("seek sent", func() bool { return len(h.eng.seekList()) == 1 })
	if s := h.eng.seekList()[0]; s.pos != 120*time.Second {
		t.Fatalf("seeked to %v, want 120s (the user's seek, not the resume offset)", s.pos)
	}
}

func TestSeekWhileOpeningTranscodedReopensAtTarget(t *testing.T) {
	h := newHarness(t, nil)
	block := make(chan struct{})
	open := h.opener.open
	h.p.o.Open = func(ctx context.Context, s subsonic.Song, off time.Duration, pre bool) (Opened, error) {
		<-block
		return open(ctx, s, off, pre)
	}
	q := songs(1, 300)
	q[0].Suffix = "m4a"
	h.p.PlayNow(q, 0)
	h.p.Seek(90 * time.Second)
	close(block)
	h.waitFor("engine.Play", func() bool { return h.eng.playCount() == 1 })
	calls := h.opener.callList()
	if last := calls[len(calls)-1]; last.offset != 90*time.Second {
		t.Fatalf("played an open at %v, want 90s; calls %+v", last.offset, calls)
	}
	if tr := h.eng.lastPlayed(); tr.Offset != 90*time.Second {
		t.Fatalf("engine got offset %v, want 90s", tr.Offset)
	}
}

// A successor that fails to prefetch is retried when its turn comes; only
// that attempt reports, so the user sees one toast, not two.
func TestFailedPrefetchReportsOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.opener.fail["sb"] = true
	h.p.PlayNow(songs(3, 100), 0)
	a := h.playAndStart(1)
	drainEvents(h)
	h.tickAt(a.ID, 85*time.Second) // prefetch of sb fails
	h.waitFor("prefetch attempted", func() bool { return len(h.opener.callList()) == 2 })
	h.p.do(func() {})
	h.eng.events <- audio.Event{Kind: audio.EventEnded, TrackID: a.ID}
	h.waitFor("sc played after sb failed again", func() bool { return h.eng.playCount() == 2 })
	errs := 0
	for len(h.p.Events()) > 0 {
		if ev := <-h.p.Events(); ev.Kind == Error {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("error events = %d, want 1", errs)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/player/`

Expected: FAIL. All eight new tests fail, starting with:

```
--- FAIL: TestRemoveCurrentWhilePausedStaysPaused (0.00s)
--- FAIL: TestRemoveLastCurrentStopsWithNothingSelected (0.00s)
--- FAIL: TestRemoveLastCurrentWrapsUnderRepeatAll (2.00s)
--- FAIL: TestEmptyPlayNextAndEnqueueAreIgnored (0.00s)
--- FAIL: TestUnpauseDoesNotFlickerToBuffering (0.00s)
--- FAIL: TestSeekWhileOpeningAimsTheOpen (2.00s)
--- FAIL: TestSeekWhileOpeningTranscodedReopensAtTarget (0.00s)
--- FAIL: TestFailedPrefetchReportsOnce (0.00s)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 0):

```diff
diff --git a/internal/player/player.go b/internal/player/player.go
index 593627e..10e2def 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -168,6 +168,10 @@ type Player struct {
 	announced  bool          // true after now-playing sent for current play
 	seekTarget time.Duration // position of the last raw seek sent to the engine
 	seekRetry  bool          // a failed seek already caused one reopen
+	// A seek that arrived while the current track was still opening; the
+	// open applies it when it lands (openSeek is valid when hasOpenSeek).
+	openSeek    time.Duration
+	hasOpenSeek bool
 }
 
 func New(o Options) *Player {
@@ -268,6 +272,9 @@ func (p *Player) PlayNow(songs []subsonic.Song, start int) {
 // PlayNext inserts songs right after the current one.
 func (p *Player) PlayNext(songs []subsonic.Song) {
 	p.do(func() {
+		if len(songs) == 0 {
+			return
+		}
 		if len(p.queue) == 0 {
 			p.queue = append([]subsonic.Song(nil), songs...)
 			p.rebuildOrder(0)
@@ -296,6 +303,9 @@ func (p *Player) PlayNext(songs []subsonic.Song) {
 // Enqueue appends songs to the end of the queue.
 func (p *Player) Enqueue(songs []subsonic.Song) {
 	p.do(func() {
+		if len(songs) == 0 {
+			return
+		}
 		if len(p.queue) == 0 {
 			p.queue = append([]subsonic.Song(nil), songs...)
 			p.rebuildOrder(0)
@@ -317,7 +327,9 @@ func (p *Player) Enqueue(songs []subsonic.Song) {
 	})
 }
 
-// Remove deletes queue[i]. Removing the current song plays the next one.
+// Remove deletes queue[i]. Removing the current song plays the next one
+// (wrapping under repeat-all) and keeps a paused player paused; with nothing
+// left to play it stops with no song selected.
 func (p *Player) Remove(i int) {
 	p.do(func() {
 		if i < 0 || i >= len(p.queue) {
@@ -348,11 +360,22 @@ func (p *Player) Remove(i int) {
 			p.queueChanged()
 			return
 		}
-		p.emit(Event{Kind: QueueChanged})
-		if next := p.cursor + 1; next < len(p.order) {
-			p.startAt(next, 0)
-		} else {
+		wasPaused := p.status == Paused
+		next := p.cursor + 1
+		if next >= len(p.order) && p.repeat == RepeatAll {
+			next = 0
+		}
+		if next >= len(p.order) {
+			p.cursor = -1
+			p.emit(Event{Kind: QueueChanged})
 			p.stop()
+			return
+		}
+		p.emit(Event{Kind: QueueChanged})
+		p.startAt(next, 0)
+		if wasPaused {
+			p.o.Engine.SetPaused(true)
+			p.setStatus(Paused)
 		}
 	})
 }
@@ -414,8 +437,12 @@ func (p *Player) TogglePause() {
 			p.setStatus(Paused)
 		case Paused:
 			p.o.Engine.SetPaused(false)
+			p.lastMove = p.o.Now() // the paused time isn't a stall
 			p.setStatus(Playing)
 		case Stopped:
+			if p.cursor < 0 && len(p.order) > 0 {
+				p.cursor = 0 // nothing selected: play the queue from the top
+			}
 			if p.cursor >= 0 {
 				p.startAt(p.cursor, 0)
 			}
@@ -438,6 +465,11 @@ func (p *Player) Seek(pos time.Duration) {
 		}
 		p.lastPos, p.position = pos, pos
 		p.seekRetry = false
+		if p.curSrc.Source == nil {
+			// Still opening: the open applies it when it lands.
+			p.openSeek, p.hasOpenSeek = pos, true
+			return
+		}
 		if p.curSrc.Transcoded {
 			p.reopenAt(pos) // I2: use reopenAt to preserve listen state and pause
 			return
@@ -635,6 +667,7 @@ func (p *Player) startAt(cursor int, offset time.Duration) {
 	p.curSrc = Opened{}
 	p.position, p.lastPos = offset, offset
 	p.seekRetry = false
+	p.hasOpenSeek = false
 	p.resetListen()
 	p.o.Engine.SetPaused(false)
 	p.setStatus(Loading)
@@ -654,6 +687,7 @@ func (p *Player) reopenAt(offset time.Duration) {
 	p.curID = id
 	p.curSrc = Opened{}
 	p.position, p.lastPos = offset, offset
+	p.hasOpenSeek = false
 	// Don't reset listen state or pause state; just reopening at a new offset
 	if p.status != Paused {
 		p.setStatus(Loading)
@@ -695,8 +729,9 @@ func (p *Player) onOpened(r openResult) {
 			return
 		}
 		if r.err != nil {
+			// No toast: the song is opened again when its turn comes, and
+			// that attempt reports if it fails too.
 			p.nextID = 0
-			p.emit(Event{Kind: Error, Song: r.song, Err: r.err})
 			return
 		}
 		p.nextSrc = r.opened
@@ -711,6 +746,15 @@ func (p *Player) onOpened(r openResult) {
 		p.trackFailed(r.song, r.err)
 		return
 	}
+	if p.hasOpenSeek {
+		p.hasOpenSeek = false
+		if r.opened.Transcoded {
+			closeOpened(r.opened) // opened at the old offset
+			p.reopenAt(p.openSeek)
+			return
+		}
+		r.seek = p.openSeek
+	}
 	p.curSrc = r.opened
 	p.o.Engine.Play(p.track(r.id, r.song, r.opened))
 	if r.seek > 0 {
@@ -789,10 +833,9 @@ func (p *Player) onEngine(ev audio.Event) {
 		switch ev.TrackID {
 		case p.nextID:
 			// The engine dropped the successor (failed or timed out opening;
-			// it closed the source). Ended(cur) then falls back to startAt.
-			song := p.queue[p.order[p.nextCursor]]
+			// it closed the source). Ended(cur) then falls back to startAt,
+			// which reports if the song fails again.
 			p.nextID, p.nextSrc = 0, Opened{}
-			p.emit(Event{Kind: Error, Song: song, Err: ev.Err})
 		case p.curID:
 			song, _ := p.currentSong()
 			if p.nextID != 0 {
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/player && go test -race -count=1 ./internal/player/`

Expected: `ok`. The Plan 1 tests still pass. `TestSuccessorOpenTimeoutFallsBackToPlay` and `TestThreeFailuresInARowStop` are unaffected: the first doesn't count events, and the second's failures are current-track failures.

- [ ] **Step 5: Commit**

```bash
git add internal/player/edges_test.go internal/player/player.go
git commit -m "player: queue edges, seek while opening, one toast per failed song" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 2: Typed text, the Queue key and dev viewer hosts

**Files:**
- Modify: `internal/input/input.go`, `internal/input/evmap.go`, `internal/devview/devview.go`, `internal/devview/page.html`, `cmd/mistersubsonic/keys.go`, `cmd/mistersubsonic/main.go`
- Test: `internal/input/input_test.go`, `internal/devview/devview_test.go`, `cmd/mistersubsonic/keys_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `input.Event` gains `Rune rune`: the character a keyboard key types, with `'\b'` for Backspace and 0 for none. Keys that only type have `Button == BtnNone`. Positional `Event{b, k}` literals no longer compile; use field names.
  - `input.BtnQueue`, the Q key.
  - The evdev translator:
    - types the US layout with Shift
    - repeats typing on kernel autorepeat (value 2), never buttons
  - The dev viewer:
    - `POST /key?b=<button>&t=<char>&down=1|0` (b may be empty when t is set)
    - accepts any IP-literal Host header
    - `URL()` names 127.0.0.1 for a wildcard bind
    - adds `Exposed() bool`
  - `-keys` items starting with `'` type text (`'abba:1s`), and `queue` is a button name.

- [ ] **Step 1: Update the tests**

Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/cmd/mistersubsonic/keys_test.go b/cmd/mistersubsonic/keys_test.go
index 64a89f9..ffadca2 100644
--- a/cmd/mistersubsonic/keys_test.go
+++ b/cmd/mistersubsonic/keys_test.go
@@ -8,11 +8,16 @@ import (
 )
 
 func TestParseKeys(t *testing.T) {
-	got, err := parseKeys("a:2s, down ,a")
+	got, err := parseKeys("a:2s, down ,'abba:1s,queue")
 	if err != nil {
 		t.Fatal(err)
 	}
-	want := []scripted{{input.BtnA, 2 * time.Second}, {input.BtnDown, 400 * time.Millisecond}, {input.BtnA, 400 * time.Millisecond}}
+	want := []scripted{
+		{b: input.BtnA, wait: 2 * time.Second},
+		{b: input.BtnDown, wait: 400 * time.Millisecond},
+		{text: "abba", wait: time.Second},
+		{b: input.BtnQueue, wait: 400 * time.Millisecond},
+	}
 	if len(got) != len(want) {
 		t.Fatalf("got %v", got)
 	}
@@ -21,10 +26,9 @@ func TestParseKeys(t *testing.T) {
 			t.Fatalf("got %v, want %v", got, want)
 		}
 	}
-	if _, err := parseKeys("jump"); err == nil {
-		t.Fatal("unknown button accepted")
-	}
-	if _, err := parseKeys("a:soon"); err == nil {
-		t.Fatal("bad duration accepted")
+	for _, bad := range []string{"jump", "a:soon", "'"} {
+		if _, err := parseKeys(bad); err == nil {
+			t.Fatalf("%q accepted", bad)
+		}
 	}
 }
diff --git a/internal/devview/devview_test.go b/internal/devview/devview_test.go
index da7e363..e5411e8 100644
--- a/internal/devview/devview_test.go
+++ b/internal/devview/devview_test.go
@@ -211,20 +211,57 @@ func TestRejectsForeignHost(t *testing.T) {
 	}
 }
 
-func TestAllowsConfiguredListenHost(t *testing.T) {
+// A wildcard bind (for a phone or another PC) is reached by IP address;
+// only hostnames other than localhost are refused.
+func TestHostCheckAllowsIPLiterals(t *testing.T) {
+	v := New(1, 1)
+	for host, want := range map[string]bool{
+		"192.168.1.5:8090": true, "[fe80::1]:8090": true, "10.0.0.2": true, "localhost:8090": true,
+		"evil.example:8090": false, "localhost.": false, "127.0.0.1.nip.io:8090": false, "": false,
+	} {
+		if got := v.hostAllowed(host); got != want {
+			t.Errorf("hostAllowed(%q) = %v, want %v", host, got, want)
+		}
+	}
+}
+
+func TestURLForWildcardBindIsLoopback(t *testing.T) {
+	if got := pageURL(&net.TCPAddr{IP: net.IPv6unspecified, Port: 8090}); got != "http://127.0.0.1:8090/" {
+		t.Fatalf("wildcard URL = %q", got)
+	}
+	if got := pageURL(&net.TCPAddr{IP: net.IPv4(192, 168, 1, 5), Port: 8090}); got != "http://192.168.1.5:8090/" {
+		t.Fatalf("LAN URL = %q", got)
+	}
+}
+
+func TestTextKeys(t *testing.T) {
 	v := New(1, 1)
-	v.allowHost = "192.168.1.5"
 	srv := httptest.NewServer(v.Handler())
 	defer srv.Close()
-	req, _ := http.NewRequest("GET", srv.URL+"/", nil)
-	req.Host = "192.168.1.5:8090"
-	resp, err := http.DefaultClient.Do(req)
-	if err != nil {
-		t.Fatal(err)
+	post := func(q string) int {
+		resp, err := http.Post(srv.URL+"/key?"+q, "", nil)
+		if err != nil {
+			t.Fatal(err)
+		}
+		resp.Body.Close()
+		return resp.StatusCode
 	}
-	resp.Body.Close()
-	if resp.StatusCode != http.StatusOK {
-		t.Fatalf("status %d for the listen address", resp.StatusCode)
+	for q, want := range map[string]input.Event{
+		"t=a&down=1":       {Kind: input.Press, Rune: 'a'},
+		"b=y&t=n&down=1":   {Button: input.BtnY, Kind: input.Press, Rune: 'n'},
+		"t=%D0%B6&down=1":  {Kind: input.Press, Rune: 'ж'},
+		"b=b&t=%08&down=1": {Button: input.BtnB, Kind: input.Press, Rune: '\b'},
+		"b=queue&down=1":   {Button: input.BtnQueue, Kind: input.Press},
+	} {
+		if c := post(q); c != http.StatusNoContent {
+			t.Fatalf("%s: status %d", q, c)
+		}
+		if e := <-v.Events(); e != want {
+			t.Fatalf("%s: event %+v, want %+v", q, e, want)
+		}
+	}
+	if c := post("down=1"); c != http.StatusBadRequest {
+		t.Fatalf("no button or text: status %d", c)
 	}
 }
 
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index ebbc582..e58eaae 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -9,11 +9,11 @@ import (
 func TestRepeaterTiming(t *testing.T) {
 	var r Repeater
 	t0 := time.Unix(1000, 0)
-	r.Feed(Event{BtnDown, Press}, t0)
+	r.Feed(Event{Button: BtnDown, Kind: Press}, t0)
 	if got := r.Due(t0.Add(349 * time.Millisecond)); len(got) != 0 {
 		t.Fatalf("repeat before the 350 ms delay: %v", got)
 	}
-	if got := r.Due(t0.Add(350 * time.Millisecond)); len(got) != 1 || got[0] != (Event{BtnDown, Repeat}) {
+	if got := r.Due(t0.Add(350 * time.Millisecond)); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Repeat}) {
 		t.Fatalf("first repeat = %v", got)
 	}
 	// 5 more at 110 ms, then 45 ms.
@@ -28,7 +28,7 @@ func TestRepeaterTiming(t *testing.T) {
 	if n := len(r.Due(at)); n != 1 {
 		t.Fatalf("fast repeat: %d events", n)
 	}
-	r.Feed(Event{BtnDown, Release}, at)
+	r.Feed(Event{Button: BtnDown, Kind: Release}, at)
 	if !r.NextDeadline().IsZero() || len(r.Due(at.Add(time.Second))) != 0 {
 		t.Fatal("repeats continued after release")
 	}
@@ -37,13 +37,13 @@ func TestRepeaterTiming(t *testing.T) {
 func TestRepeaterOnlyNavigationButtons(t *testing.T) {
 	var r Repeater
 	t0 := time.Unix(0, 0)
-	r.Feed(Event{BtnA, Press}, t0)
+	r.Feed(Event{Button: BtnA, Kind: Press}, t0)
 	if len(r.Due(t0.Add(time.Second))) != 0 {
 		t.Fatal("A must not auto-repeat")
 	}
-	r.Feed(Event{BtnUp, Press}, t0)
-	r.Feed(Event{BtnDown, Press}, t0) // the newer button takes over
-	r.Feed(Event{BtnUp, Release}, t0) // releasing the old one changes nothing
+	r.Feed(Event{Button: BtnUp, Kind: Press}, t0)
+	r.Feed(Event{Button: BtnDown, Kind: Press}, t0) // the newer button takes over
+	r.Feed(Event{Button: BtnUp, Kind: Release}, t0) // releasing the old one changes nothing
 	if got := r.Due(t0.Add(RepeatDelay)); len(got) != 1 || got[0].Button != BtnDown {
 		t.Fatalf("got %v, want a Down repeat", got)
 	}
@@ -74,13 +74,13 @@ func TestParseMisterMap(t *testing.T) {
 
 func TestTranslatorKeysAndMapOverride(t *testing.T) {
 	tr := newTranslator(map[uint16]Button{btnSouth: BtnA}, nil)
-	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{BtnA, Press}) {
+	if got := tr.handle(evKey, btnSouth, 1); len(got) != 1 || got[0] != (Event{Button: BtnA, Kind: Press}) {
 		t.Fatalf("mapped south = %v", got)
 	}
-	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{BtnA, Press}) {
+	if got := tr.handle(evKey, btnEast, 1); got[0] != (Event{Button: BtnA, Kind: Press}) {
 		t.Fatalf("default east = %v", got)
 	}
-	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{BtnA, Release}) {
+	if got := tr.handle(evKey, keyEnter, 0); got[0] != (Event{Button: BtnA, Kind: Release}) {
 		t.Fatalf("enter release = %v", got)
 	}
 	if got := tr.handle(evKey, keyDown, 2); got != nil {
@@ -93,22 +93,22 @@ func TestTranslatorKeysAndMapOverride(t *testing.T) {
 
 func TestTranslatorHatAndStick(t *testing.T) {
 	tr := newTranslator(nil, map[uint16]AbsRange{absX: {0, 255}})
-	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{BtnUp, Press}) {
+	if got := tr.handle(evAbs, absHat0Y, -1); len(got) != 1 || got[0] != (Event{Button: BtnUp, Kind: Press}) {
 		t.Fatalf("hat up = %v", got)
 	}
-	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{BtnUp, Release}) || got[1] != (Event{BtnDown, Press}) {
+	if got := tr.handle(evAbs, absHat0Y, 1); len(got) != 2 || got[0] != (Event{Button: BtnUp, Kind: Release}) || got[1] != (Event{Button: BtnDown, Kind: Press}) {
 		t.Fatalf("hat up→down = %v", got)
 	}
-	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{BtnDown, Release}) {
+	if got := tr.handle(evAbs, absHat0Y, 0); len(got) != 1 || got[0] != (Event{Button: BtnDown, Kind: Release}) {
 		t.Fatalf("hat centre = %v", got)
 	}
 	if got := tr.handle(evAbs, absX, 150); got != nil {
 		t.Fatalf("small stick movement = %v", got)
 	}
-	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{BtnRight, Press}) {
+	if got := tr.handle(evAbs, absX, 250); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Press}) {
 		t.Fatalf("stick right = %v", got)
 	}
-	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{BtnRight, Release}) {
+	if got := tr.handle(evAbs, absX, 128); len(got) != 1 || got[0] != (Event{Button: BtnRight, Kind: Release}) {
 		t.Fatalf("stick centre = %v", got)
 	}
 	if got := tr.handle(evAbs, absY, 0); got != nil {
@@ -119,7 +119,7 @@ func TestTranslatorHatAndStick(t *testing.T) {
 func TestRepeaterNoBurstAfterStall(t *testing.T) {
 	var r Repeater
 	t0 := time.Unix(1000, 0)
-	r.Feed(Event{BtnDown, Press}, t0)
+	r.Feed(Event{Button: BtnDown, Kind: Press}, t0)
 	now := t0.Add(5 * time.Second)
 	if got := r.Due(now); len(got) != 1 {
 		t.Fatalf("Due after a stall = %d events, want 1", len(got))
@@ -143,3 +143,44 @@ func TestParseMisterMapMasksHighBits(t *testing.T) {
 		t.Fatalf("map = %v, want only code 1 -> BtnA", m)
 	}
 }
+
+func TestTranslatorTypesText(t *testing.T) {
+	tr := newTranslator(nil, nil)
+	const keyA, keyZ = 30, 44
+	if got := tr.handle(evKey, keyA, 1); len(got) != 1 || got[0] != (Event{Button: BtnNone, Kind: Press, Rune: 'a'}) {
+		t.Fatalf("a = %v", got)
+	}
+	if got := tr.handle(evKey, keyA, 0); len(got) != 1 || got[0] != (Event{Button: BtnNone, Kind: Release, Rune: 'a'}) {
+		t.Fatalf("a release = %v", got)
+	}
+	// Mapped keys keep their button and also type.
+	if got := tr.handle(evKey, keyN, 1); got[0] != (Event{Button: BtnY, Kind: Press, Rune: 'n'}) {
+		t.Fatalf("n = %v", got)
+	}
+	if got := tr.handle(evKey, keyQ, 1); got[0] != (Event{Button: BtnQueue, Kind: Press, Rune: 'q'}) {
+		t.Fatalf("q = %v", got)
+	}
+	if got := tr.handle(evKey, keySpace, 1); got[0] != (Event{Button: BtnStart, Kind: Press, Rune: ' '}) {
+		t.Fatalf("space = %v", got)
+	}
+	if got := tr.handle(evKey, keyLShift, 1); got != nil {
+		t.Fatalf("shift alone = %v", got)
+	}
+	if got := tr.handle(evKey, keyZ, 1); got[0].Rune != 'Z' {
+		t.Fatalf("shift+z = %v", got)
+	}
+	if got := tr.handle(evKey, 3, 1); got[0].Rune != '@' { // Shift+2
+		t.Fatalf("shift+2 = %v", got)
+	}
+	tr.handle(evKey, keyLShift, 0)
+	if got := tr.handle(evKey, 53, 1); got[0].Rune != '/' {
+		t.Fatalf("slash = %v", got)
+	}
+	// Kernel autorepeat repeats typing (Backspace held), never buttons.
+	if got := tr.handle(evKey, keyBackspace, 2); len(got) != 1 || got[0] != (Event{Kind: Press, Rune: '\b'}) {
+		t.Fatalf("backspace autorepeat = %v", got)
+	}
+	if got := tr.handle(evKey, keyEnter, 2); got != nil {
+		t.Fatalf("enter autorepeat = %v", got)
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/input ./internal/devview ./cmd/mistersubsonic`

Expected: build failures, for example:

```
unknown field Rune in struct literal of type Event
undefined: BtnQueue
undefined: keyLShift
got[0].Rune undefined (type Event has no field or method Rune)
undefined: pageURL
unknown field Rune in struct literal of type input.Event
undefined: input.BtnQueue
unknown field text in struct literal of type scripted
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/cmd/mistersubsonic/keys.go b/cmd/mistersubsonic/keys.go
index 1e2566f..f249a40 100644
--- a/cmd/mistersubsonic/keys.go
+++ b/cmd/mistersubsonic/keys.go
@@ -12,16 +12,19 @@ var keyNames = map[string]input.Button{
 	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
+	"queue": input.BtnQueue,
 }
 
 type scripted struct {
 	b    input.Button
+	text string        // typed instead of a button press when set
 	wait time.Duration // pause before this press
 }
 
 // parseKeys reads a -keys script: comma-separated button names, each
 // optionally followed by ":<duration>" to wait before pressing it, e.g.
-// "a:2s,a:1s,a,a:1s". The default wait is 400 ms.
+// "a:2s,a:1s,a,a:1s". An item starting with ' types the rest as keyboard
+// text ("'abba:1s"). The default wait is 400 ms.
 func parseKeys(s string) ([]scripted, error) {
 	var out []scripted
 	for _, part := range strings.Split(s, ",") {
@@ -30,18 +33,19 @@ func parseKeys(s string) ([]scripted, error) {
 			continue
 		}
 		name, wait, found := strings.Cut(part, ":")
-		b, ok := keyNames[name]
-		if !ok {
+		k := scripted{wait: 400 * time.Millisecond}
+		if text, ok := strings.CutPrefix(name, "'"); ok && text != "" {
+			k.text = text
+		} else if k.b, ok = keyNames[name]; !ok {
 			return nil, fmt.Errorf("-keys: unknown button %q", name)
 		}
-		d := 400 * time.Millisecond
 		if found {
 			var err error
-			if d, err = time.ParseDuration(wait); err != nil {
+			if k.wait, err = time.ParseDuration(wait); err != nil {
 				return nil, fmt.Errorf("-keys: %q: %v", part, err)
 			}
 		}
-		out = append(out, scripted{b, d})
+		out = append(out, k)
 	}
 	return out, nil
 }
@@ -52,6 +56,13 @@ func playKeys(script []scripted) <-chan input.Event {
 	go func() {
 		for _, k := range script {
 			time.Sleep(k.wait)
+			if k.text != "" {
+				for _, r := range k.text {
+					ch <- input.Event{Kind: input.Press, Rune: r}
+					ch <- input.Event{Kind: input.Release, Rune: r}
+				}
+				continue
+			}
 			ch <- input.Event{Button: k.b, Kind: input.Press}
 			ch <- input.Event{Button: k.b, Kind: input.Release}
 		}
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 98372f7..31a29ee 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -132,6 +132,9 @@ func run(f flags) error {
 			return err
 		}
 		fmt.Println("MiSTer Subsonic dev viewer:", v.URL())
+		if v.Exposed() {
+			fmt.Println("warning: the viewer is open to your network: anyone on it can see the screen and press keys")
+		}
 		disp = v
 		inputs = append(inputs, v.Events())
 	case "headless":
diff --git a/internal/devview/devview.go b/internal/devview/devview.go
index 50dce91..c2c6ca7 100644
--- a/internal/devview/devview.go
+++ b/internal/devview/devview.go
@@ -28,6 +28,7 @@ var buttons = map[string]input.Button{
 	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
+	"queue": input.BtnQueue,
 }
 
 // Viewer is a gfx.Display that browsers watch.
@@ -36,9 +37,6 @@ type Viewer struct {
 	events chan input.Event
 	srv    *http.Server
 	ln     net.Listener
-	// allowHost is the listen address host when bound to a specific
-	// non-loopback IP; Host headers naming it are accepted too.
-	allowHost string
 
 	mu    sync.Mutex
 	cond  *sync.Cond
@@ -61,22 +59,32 @@ func (v *Viewer) Listen(addr string) error {
 		return err
 	}
 	v.ln = ln
-	if host, _, err := net.SplitHostPort(ln.Addr().String()); err == nil {
-		if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
-			v.allowHost = host
-		}
-	}
 	v.srv = &http.Server{Handler: v.Handler(), ReadHeaderTimeout: 5 * time.Second}
 	go v.srv.Serve(ln)
 	return nil
 }
 
-// URL is the page address once Listen succeeded.
+// URL is the page address once Listen succeeded. For a wildcard bind
+// (":8090", "0.0.0.0:8090") it names this machine's loopback address.
 func (v *Viewer) URL() string {
 	if v.ln == nil {
 		return ""
 	}
-	return "http://" + v.ln.Addr().String() + "/"
+	return pageURL(v.ln.Addr().(*net.TCPAddr))
+}
+
+func pageURL(addr *net.TCPAddr) string {
+	host := addr.IP.String()
+	if addr.IP.IsUnspecified() {
+		host = "127.0.0.1"
+	}
+	return "http://" + net.JoinHostPort(host, strconv.Itoa(addr.Port)) + "/"
+}
+
+// Exposed reports whether the viewer listens beyond loopback: anyone on
+// the network can then watch the screen and press keys.
+func (v *Viewer) Exposed() bool {
+	return v.ln != nil && !v.ln.Addr().(*net.TCPAddr).IP.IsLoopback()
 }
 
 func (v *Viewer) Events() <-chan input.Event { return v.events }
@@ -108,7 +116,8 @@ func (v *Viewer) Close() error {
 	return nil
 }
 
-// Handler serves the page (/), frames (/frame?after=N, long-poll) and keys (POST /key?b=a&down=1).
+// Handler serves the page (/), frames (/frame?after=N, long-poll) and keys
+// (POST /key?b=a&down=1, with t=<character> for keys that type).
 func (v *Viewer) Handler() http.Handler {
 	mux := http.NewServeMux()
 	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
@@ -121,17 +130,28 @@ func (v *Viewer) Handler() http.Handler {
 			http.Error(w, "cross-origin request refused", http.StatusForbidden)
 			return
 		}
-		b, ok := buttons[r.URL.Query().Get("b")]
-		if !ok {
-			http.Error(w, "unknown button", http.StatusBadRequest)
+		q := r.URL.Query()
+		var e input.Event
+		if name := q.Get("b"); name != "" {
+			b, ok := buttons[name]
+			if !ok {
+				http.Error(w, "unknown button", http.StatusBadRequest)
+				return
+			}
+			e.Button = b
+		}
+		if t := []rune(q.Get("t")); len(t) == 1 {
+			e.Rune = t[0]
+		}
+		if e.Button == input.BtnNone && e.Rune == 0 {
+			http.Error(w, "no button or text", http.StatusBadRequest)
 			return
 		}
-		kind := input.Release
-		if r.URL.Query().Get("down") == "1" {
-			kind = input.Press
+		e.Kind = input.Release
+		if q.Get("down") == "1" {
+			e.Kind = input.Press
 		}
-		e := input.Event{Button: b, Kind: kind}
-		if kind == input.Release {
+		if e.Kind == input.Release {
 			// A lost Release would leave the button stuck (auto-repeating).
 			select {
 			case v.events <- e:
@@ -148,9 +168,11 @@ func (v *Viewer) Handler() http.Handler {
 	return v.checkHost(mux)
 }
 
-// checkHost refuses requests whose Host header isn't this machine's loopback
-// (or the configured listen address). Without it a DNS-rebinding page, which
-// is same-origin with its own hostname, could press keys and read frames.
+// checkHost refuses requests whose Host header is a name other than
+// localhost. A DNS-rebinding page is same-origin with its own hostname, so
+// without this it could press keys and read frames; it can't make the
+// browser send an IP address as Host, so IP literals (the LAN address of a
+// wildcard bind, say) are fine.
 func (v *Viewer) checkHost(next http.Handler) http.Handler {
 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
 		if !v.hostAllowed(r.Host) {
@@ -167,11 +189,7 @@ func (v *Viewer) hostAllowed(hostport string) bool {
 		host = h
 	}
 	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
-	if host == "localhost" || (v.allowHost != "" && host == v.allowHost) {
-		return true
-	}
-	ip := net.ParseIP(host)
-	return ip != nil && ip.IsLoopback()
+	return host == "localhost" || net.ParseIP(host) != nil
 }
 
 // sameOrigin refuses requests from other web pages, which could otherwise
diff --git a/internal/devview/page.html b/internal/devview/page.html
index 4b210cb..eb62546 100644
--- a/internal/devview/page.html
+++ b/internal/devview/page.html
@@ -7,21 +7,25 @@
 </style></head>
 <body>
 <img id="f" alt="waiting for the first frame…">
-<p>Arrows move · Enter = A · Esc/Backspace = B · Tab = X · N/Q = Y · PgUp/PgDn = L/R · Space = Start · S = Select</p>
+<p>Arrows move · Enter = A · Esc/Backspace = B · Tab = X · N = Y · Q = Queue · PgUp/PgDn = L/R · Space = Start · S = Select · letters type in Search</p>
 <script>
-// Mapped on e.code, not e.key: Shift/CapsLock change key ("s" vs "S") between keydown and keyup.
+// Buttons are mapped on e.code, not e.key: Shift/CapsLock change key ("s" vs
+// "S") between keydown and keyup. The typed character (e.key) goes along as
+// t= so text fields can use letters that are also buttons.
 const keys = {ArrowUp:"up", ArrowDown:"down", ArrowLeft:"left", ArrowRight:"right",
-  Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"y",
+  Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"queue",
   PageUp:"l", PageDown:"r", Space:"start", KeyS:"select"};
 const held = new Set();
 function send(e, down) {
-  const b = keys[e.code]; if (!b) return;
   if (down && (e.ctrlKey || e.altKey || e.metaKey)) return; // leave browser shortcuts alone
+  const b = keys[e.code] || "";
+  const t = !down ? "" : e.key.length === 1 ? e.key : e.code === "Backspace" ? "\b" : "";
+  if (!b && !t) return;
   if (!down && !held.has(b)) return;
   e.preventDefault();
-  if (down && held.has(b)) return; // the app does its own key repeat
-  down ? held.add(b) : held.delete(b);
-  fetch("/key?b=" + b + "&down=" + (down ? 1 : 0), {method: "POST"});
+  if (b && down && held.has(b)) return; // the app does its own key repeat
+  if (b) down ? held.add(b) : held.delete(b);
+  fetch("/key?b=" + b + "&down=" + (down ? 1 : 0) + (t ? "&t=" + encodeURIComponent(t) : ""), {method: "POST"});
 }
 addEventListener("keydown", e => send(e, true));
 addEventListener("keyup", e => send(e, false));
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index df091e3..7683ab4 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -23,7 +23,9 @@ const (
 	keyTab       = 15
 	keyQ         = 16
 	keyEnter     = 28
+	keyLShift    = 42
 	keyN         = 49
+	keyRShift    = 54
 	keySpace     = 57
 	keyPageUp    = 104
 	keyUp        = 103
@@ -53,13 +55,34 @@ var DefaultKeys = map[uint16]Button{
 	keyUp: BtnUp, keyDown: BtnDown, keyLeft: BtnLeft, keyRight: BtnRight,
 	keyEnter: BtnA, keyKPEnter: BtnA, keyEsc: BtnB, keyBackspace: BtnB,
 	keyTab: BtnX, keySpace: BtnStart, keyPageUp: BtnL, keyPageDown: BtnR,
-	keyN: BtnY, keyQ: BtnY,
+	keyN: BtnY, keyQ: BtnQueue,
 
 	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
 	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
 	btnDpadUp: BtnUp, btnDpadDn: BtnDown, btnDpadL: BtnLeft, btnDpadR: BtnRight,
 }
 
+// usLayout is what each key types on a US keyboard: {plain, with Shift}.
+var usLayout = func() map[uint16][2]rune {
+	m := map[uint16][2]rune{keySpace: {' ', ' '}, keyBackspace: {'\b', '\b'}}
+	rows := []struct {
+		first          uint16
+		plain, shifted string
+	}{
+		{2, "1234567890-=", "!@#$%^&*()_+"},
+		{16, "qwertyuiop[]", "QWERTYUIOP{}"},
+		{30, "asdfghjkl;'`", "ASDFGHJKL:\"~"},
+		{43, "\\zxcvbnm,./", "|ZXCVBNM<>?"},
+	}
+	for _, r := range rows {
+		plain, shifted := []rune(r.plain), []rune(r.shifted)
+		for i := range plain {
+			m[r.first+uint16(i)] = [2]rune{plain[i], shifted[i]}
+		}
+	}
+	return m
+}()
+
 // MiSTer .map slots (Main_MiSTer): 32 little-endian uint32, low 16 bits = key code.
 var misterSlots = [...]Button{BtnRight, BtnLeft, BtnDown, BtnUp, BtnA, BtnB, BtnX, BtnY, BtnL, BtnR, BtnSelect, BtnStart}
 
@@ -91,28 +114,47 @@ type AbsRange struct{ Min, Max int32 }
 type translator struct {
 	keys  map[uint16]Button
 	abs   map[uint16]AbsRange
-	state map[uint16]int // axis -> -1, 0, +1
+	state map[uint16]int  // axis -> -1, 0, +1
+	shift map[uint16]bool // Shift keys held
 }
 
 func newTranslator(keys map[uint16]Button, abs map[uint16]AbsRange) *translator {
-	return &translator{keys: keys, abs: abs, state: map[uint16]int{}}
+	return &translator{keys: keys, abs: abs, state: map[uint16]int{}, shift: map[uint16]bool{}}
 }
 
-// handle converts a raw event. value: 1 press, 0 release, 2 kernel autorepeat (ignored).
+// handle converts a raw event. value: 1 press, 0 release, 2 kernel
+// autorepeat (buttons ignore it, the app repeats them itself; typing repeats).
 func (t *translator) handle(typ, code uint16, value int32) []Event {
 	switch typ {
 	case evKey:
 		b, ok := t.keys[code]
+		if !ok && (code == keyLShift || code == keyRShift) {
+			t.shift[code] = value != 0
+			return nil
+		}
 		if !ok {
 			b, ok = DefaultKeys[code]
 		}
-		if !ok || value == 2 {
+		var r rune
+		if l, typed := usLayout[code]; typed {
+			r = l[0]
+			if t.shift[keyLShift] || t.shift[keyRShift] {
+				r = l[1]
+			}
+		}
+		if !ok && r == 0 {
 			return nil
 		}
-		if value == 1 {
-			return []Event{{b, Press}}
+		switch value {
+		case 1:
+			return []Event{{Button: b, Kind: Press, Rune: r}}
+		case 0:
+			return []Event{{Button: b, Kind: Release, Rune: r}}
+		}
+		if r != 0 {
+			return []Event{{Kind: Press, Rune: r}}
 		}
-		return []Event{{b, Release}}
+		return nil
 	case evAbs:
 		var neg, pos Button
 		switch code {
@@ -150,15 +192,15 @@ func (t *translator) handle(typ, code uint16, value int32) []Event {
 		var out []Event
 		switch prev {
 		case -1:
-			out = append(out, Event{neg, Release})
+			out = append(out, Event{Button: neg, Kind: Release})
 		case 1:
-			out = append(out, Event{pos, Release})
+			out = append(out, Event{Button: pos, Kind: Release})
 		}
 		switch dir {
 		case -1:
-			out = append(out, Event{neg, Press})
+			out = append(out, Event{Button: neg, Kind: Press})
 		case 1:
-			out = append(out, Event{pos, Press})
+			out = append(out, Event{Button: pos, Kind: Press})
 		}
 		return out
 	}
diff --git a/internal/input/input.go b/internal/input/input.go
index aeb0936..3644218 100644
--- a/internal/input/input.go
+++ b/internal/input/input.go
@@ -22,9 +22,10 @@ const (
 	BtnR
 	BtnSelect
 	BtnStart
+	BtnQueue // keyboard only (Q): open the queue
 )
 
-var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start"}
+var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue"}
 
 func (b Button) String() string {
 	if int(b) < len(buttonNames) {
@@ -42,10 +43,14 @@ const (
 	Repeat // generated while a navigation button is held
 )
 
-// Event is one button transition.
+// Event is one button transition. Keyboard keys also carry the character
+// they type in Rune ('\b' for Backspace, 0 for keys that type nothing), so a
+// text field can take letters that are mapped to buttons too (N = Y,
+// Space = Start). Keys that only type have Button BtnNone.
 type Event struct {
 	Button Button
 	Kind   Kind
+	Rune   rune
 }
 
 // Repeat timing (spec §8.4).
```

- [ ] **Step 4: Run the tests and the e2e checks**

Run: `go vet ./... && go test -race -count=1 ./internal/input ./internal/devview ./cmd/mistersubsonic && make e2e`

Expected: `ok` for each package, then `e2e ok: …` and `e2e-ui ok: …`. The UI doesn't use the new fields yet, and Q now sends `BtnQueue`, which the UI learns in Task 3.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/keys.go cmd/mistersubsonic/keys_test.go cmd/mistersubsonic/main.go internal/devview/devview.go internal/devview/devview_test.go internal/devview/page.html internal/input/evmap.go internal/input/input.go internal/input/input_test.go
git commit -m "input: typed text and a Queue key; devview: text keys, IP hosts, wildcard URL" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 3: UI plumbing — timers, cancellable loads, typing, the safe area, the marquee

**Files:**
- Modify: `internal/ui/app.go`, `internal/ui/theme.go`
- Test: `internal/ui/app_test.go` (new), `internal/ui/fakes_test.go` (modified: fakes for the wider interfaces and extra sample data fields)

**Interfaces:**
- **Consumes:** `input.Event.Rune`, `input.BtnQueue` (Task 2); `player.Player.PlayNext/Enqueue/Clear/SetVolumeDB` (Plan 1).
- **Produces:**
  - **Wider interfaces:**
    - `ui.Library` adds `GetArtists`, `GetArtist`, `GetGenres`, `GetPlaylists`, `GetPlaylist`, `GetStarred2`, `Search3`, `Star`, `Unstar`. `*subsonic.Client` already has them all.
    - `ui.Player` adds `PlayNext`, `Enqueue`, `Clear`, `SetVolumeDB`.
  - **Screen interfaces:**
    - `TextInput{ Text(a *App, r rune) bool }`. The top screen gets typed characters; returning false lets the key act as its button.
    - `owner{ Owns(Screen) bool }`. Loads and timers of a screen shown inside another run under the outer one's stack entry.
  - **Timers and loads:**
    - `(*App).After(owner Screen, d time.Duration, f func())`
    - `(*App).LoadCancel(s Screen, fn, done) (cancel func())`. `Load` now calls it.
  - **Drawing and badge:**
    - `(*App).drawFit(c, f, x, y, w, s, col, clip, focused)` truncates, or when focused scrolls text that doesn't fit, after 1.2 s, at `Profile.MarqueeSpeed` px/s.
    - `(*App).SetInsecure(bool)` puts an "insecure" badge in the header.
  - **Profile fields:** `SafeY` (CRT 12) and `MarqueeSpeed`; CRT `Margin` becomes 16.
  - **Global keys and the mini bar:**
    - The Q key opens the queue (the queue itself, not Now Playing).
    - The mini bar needs a current song, not just a non-empty queue.

- [ ] **Step 1: Write the failing tests**

`internal/ui/app_test.go` (new file):

```go
package ui

import (
	"bytes"
	"context"
	"image"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
)

// probe is a test screen: it records what it is given and types into text.
type probe struct {
	area    gfx.Rect
	text    string
	handled []input.Event
	draw    func(a *App, c *gfx.Canvas, area gfx.Rect)
}

func (s *probe) Title() string { return "Probe" }
func (s *probe) Enter(*App)    {}
func (s *probe) Handle(a *App, e input.Event) bool {
	s.handled = append(s.handled, e)
	return false
}
func (s *probe) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.area = area
	if s.draw != nil {
		s.draw(a, c, area)
	}
}

// typer is a probe that takes text; Backspace in an empty field isn't text.
type typer struct{ probe }

func (s *typer) Text(a *App, r rune) bool {
	if r == '\b' {
		if s.text == "" {
			return false
		}
		r := []rune(s.text)
		s.text = string(r[:len(r)-1])
		return true
	}
	s.text += string(r)
	return true
}

func TestAfterRunsAndIsDroppedWithItsScreen(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	root, top := &probe{}, &probe{}
	ta.Push(root)
	ta.Push(top)
	var ran []string
	ta.After(root, 300*time.Millisecond, func() { ran = append(ran, "root") })
	ta.After(top, 300*time.Millisecond, func() { ran = append(ran, "top") })
	if d := ta.untilWake(); d != 300*time.Millisecond {
		t.Fatalf("next wake in %v, want 300ms", d)
	}
	ta.Pop()
	ta.now = ta.now.Add(299 * time.Millisecond)
	ta.onWake()
	if len(ran) != 0 {
		t.Fatalf("ran early: %v", ran)
	}
	ta.now = ta.now.Add(time.Millisecond)
	ta.onWake()
	if len(ran) != 1 || ran[0] != "root" {
		t.Fatalf("ran %v, want only root (top was popped)", ran)
	}
	if len(ta.timers) != 0 {
		t.Fatalf("%d timers left", len(ta.timers))
	}
}

func TestLoadCancelDropsTheResult(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	started := make(chan struct{})
	var sawCancel bool
	delivered := false
	cancel := ta.LoadCancel(s, func(ctx context.Context) (any, error) {
		close(started)
		<-ctx.Done()
		sawCancel = true
		return nil, ctx.Err()
	}, func(any, error) { delivered = true })
	<-started
	cancel()
	ta.settle(t)
	if delivered || !sawCancel {
		t.Fatalf("delivered %v, fn saw cancel %v", delivered, sawCancel)
	}
}

func TestTextGoesToTextScreensAndSwallowsItsButtons(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: 0, NextIndex: -1}
	root := &probe{}
	s := &typer{}
	ta.Push(root)
	ta.Push(s)
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Press, Rune: 'n'}) // N is also Y
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Release, Rune: 'n'})
	ta.onInput(input.Event{Kind: input.Press, Rune: 'ж'})
	ta.onInput(input.Event{Button: input.BtnStart, Kind: input.Press, Rune: ' '})
	ta.onInput(input.Event{Button: input.BtnStart, Kind: input.Release, Rune: ' '})
	if s.text != "nж " {
		t.Fatalf("typed %q", s.text)
	}
	if ta.Top() != s || len(ta.pl.calls) != 0 || len(s.handled) != 0 {
		t.Fatalf("typing acted as buttons: top %T, player %v, handled %v", ta.Top(), ta.pl.calls, s.handled)
	}
	for range 3 {
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Release, Rune: '\b'})
	}
	if s.text != "" || ta.Top() != s {
		t.Fatalf("after 3 backspaces: text %q, top %T", s.text, ta.Top())
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'}) // empty: acts as B
	if ta.Top() != root {
		t.Fatalf("Backspace in an empty field did not go back (top %T)", ta.Top())
	}
	// Screens without text input get the buttons as usual; text-only keys do nothing.
	ta.onInput(input.Event{Kind: input.Press, Rune: 'x'})
	ta.onInput(input.Event{Button: input.BtnY, Kind: input.Press, Rune: 'n'})
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("N outside a text field: top %T, want Now Playing", ta.Top())
	}
}

func TestQueueKeyOpensTheQueue(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(&probe{})
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*probe); !ok {
		t.Fatal("Q with an empty queue opened something")
	}
	playingState(ta)
	ta.press(input.BtnY)
	ta.press(input.BtnQueue)
	ta.press(input.BtnQueue)
	if _, ok := ta.Top().(*QueueScreen); !ok || len(ta.stack) != 3 {
		t.Fatalf("top %T, %d screens; want the queue over Now Playing", ta.Top(), len(ta.stack))
	}
}

func TestSafeAreaAndMiniBarNeedACurrentSong(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	s := &probe{}
	ta.Push(s)
	ta.settle(t)
	p := ta.P
	if s.area.Y != p.SafeY+p.HeaderH || s.area.Bottom() != p.H-p.SafeY {
		t.Fatalf("body %+v: want from %d to %d", s.area, p.SafeY+p.HeaderH, p.H-p.SafeY)
	}
	ta.pl.st = player.State{Queue: ta.lib.tracks["al-1"], Index: -1, NextIndex: -1}
	ta.settle(t)
	if s.area.Bottom() != p.H-p.SafeY {
		t.Fatal("mini bar shown with nothing selected")
	}
	ta.pl.st.Index = 0
	ta.settle(t)
	if s.area.Bottom() != p.H-p.SafeY-p.MiniBarH {
		t.Fatalf("body bottom %d with a current song, want %d", s.area.Bottom(), p.H-p.SafeY-p.MiniBarH)
	}
}

func TestMarqueeScrollsFocusedLongText(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	long := "A Rather Long Album Title That Will Need Truncating Somewhere"
	focused := true
	ta.Push(&probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
		f := a.F.Body
		a.drawFit(c, f, area.X, area.Y+f.Ascent(), 100, long, colText, area, focused)
	}})
	first := ta.settle(t).ToRGBA()
	if d := ta.untilWake(); d > marqueeFrame {
		t.Fatalf("next wake in %v while a marquee runs", d)
	}
	ta.now = ta.now.Add(marqueeDelay / 2)
	if !samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("marquee moved before its delay")
	}
	ta.now = ta.now.Add(2 * time.Second)
	if samePixels(first, ta.settle(t).ToRGBA()) {
		t.Fatal("focused long text did not scroll")
	}
	focused = false
	ta.settle(t)
	if ta.animate || ta.mq.text != "" {
		t.Fatal("marquee still running after focus moved")
	}
}

func TestInsecureBadge(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(&probe{})
	ta.SetInsecure(true)
	c := ta.settle(t)
	red := 0
	for y := 0; y < ta.P.HeaderH; y++ {
		for x := ta.P.W / 2; x < ta.P.W; x++ {
			if c.At(x, y) == colError {
				red++
			}
		}
	}
	if red == 0 {
		t.Fatal("no insecure badge in the header")
	}
}

func samePixels(a, b *image.RGBA) bool { return bytes.Equal(a.Pix, b.Pix) }

// Metadata can be in any script, with combining marks, emoji and ZWJ
// sequences; the marquee must step through it without breaking runes and
// never draw outside its width.
func TestMarqueeHandlesAnyScript(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	for _, s := range []string{
		"東京事変 — 群青日和 (Live at 日本武道館) 🎸🎸🎸",
		"Şarkı ğüşiöç ĞÜŞİÖÇ — İstanbul’da bir akşam, uzun bir başlık",
		"ééé combining marks, ZWJ 👨‍👩‍👧 and bidi ‮abc‬ end",
	} {
		const x, w = 20, 60
		ta.Replace(&probe{draw: func(a *App, c *gfx.Canvas, area gfx.Rect) {
			a.drawFit(c, a.F.Body, x, area.Y+a.F.Body.Ascent(), w, s, colText, area, true)
		}})
		for step := 0; step < 40; step++ {
			ta.now = ta.now.Add(170 * time.Millisecond)
			c := ta.settle(t)
			top := ta.P.SafeY + ta.P.HeaderH
			for y := top; y < top+ta.F.Body.Height(); y++ {
				for _, px := range []int{x - 1, x + w, x + w + 5} {
					if c.At(px, y) != colBg {
						t.Fatalf("%q step %d: drew outside its width at (%d,%d)", s, step, px, y)
					}
				}
			}
		}
	}
}
```

Update the fakes:

Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 67afb83..3fb58d5 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -4,6 +4,7 @@ import (
 	"context"
 	"errors"
 	"hash/fnv"
+	"strings"
 	"sync"
 	"testing"
 	"time"
@@ -16,11 +17,19 @@ import (
 )
 
 type fakeLibrary struct {
-	mu     sync.Mutex
-	albums []subsonic.Album
-	tracks map[subsonic.ID][]subsonic.Song
-	err    error
-	calls  []subsonic.AlbumListQuery
+	mu        sync.Mutex
+	albums    []subsonic.Album
+	tracks    map[subsonic.ID][]subsonic.Song
+	artists   []subsonic.ArtistIndex
+	genres    []subsonic.Genre
+	playlists []subsonic.Playlist
+	plSongs   map[subsonic.ID][]subsonic.Song
+	starred   subsonic.Starred
+	err       error
+	calls     []subsonic.AlbumListQuery
+	searches  []string
+	stars     []string      // "star al-1", "unstar s2"
+	block     chan struct{} // if set, Search3 waits for it (or ctx)
 }
 
 func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
@@ -51,6 +60,138 @@ func (l *fakeLibrary) GetAlbum(_ context.Context, id subsonic.ID) (*subsonic.Alb
 	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no album"}
 }
 
+func (l *fakeLibrary) GetArtists(context.Context) ([]subsonic.ArtistIndex, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	return l.artists, l.err
+}
+
+func (l *fakeLibrary) GetArtist(_ context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	if l.err != nil {
+		return nil, l.err
+	}
+	for _, ix := range l.artists {
+		for _, ar := range ix.Artists {
+			if ar.ID == id {
+				out := &subsonic.ArtistWithAlbums{Artist: ar}
+				for _, al := range l.albums {
+					if al.ArtistID == id {
+						out.Albums = append(out.Albums, al)
+					}
+				}
+				return out, nil
+			}
+		}
+	}
+	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no artist"}
+}
+
+func (l *fakeLibrary) GetGenres(context.Context) ([]subsonic.Genre, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	return l.genres, l.err
+}
+
+func (l *fakeLibrary) GetPlaylists(context.Context) ([]subsonic.Playlist, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	return l.playlists, l.err
+}
+
+func (l *fakeLibrary) GetPlaylist(_ context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	if l.err != nil {
+		return nil, l.err
+	}
+	for _, pl := range l.playlists {
+		if pl.ID == id {
+			return &subsonic.PlaylistWithSongs{Playlist: pl, Songs: l.plSongs[id]}, nil
+		}
+	}
+	return nil, &subsonic.APIError{Code: subsonic.CodeNotFound, Message: "no playlist"}
+}
+
+func (l *fakeLibrary) GetStarred2(context.Context) (*subsonic.Starred, error) {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	if l.err != nil {
+		return nil, l.err
+	}
+	s := l.starred
+	return &s, nil
+}
+
+// Search3 matches names containing the query (case-insensitive).
+func (l *fakeLibrary) Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error) {
+	l.mu.Lock()
+	l.searches = append(l.searches, query)
+	block := l.block
+	l.mu.Unlock()
+	if block != nil {
+		select {
+		case <-block:
+		case <-ctx.Done():
+			return nil, ctx.Err()
+		}
+	}
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	if l.err != nil {
+		return nil, l.err
+	}
+	has := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(query)) }
+	r := &subsonic.SearchResult{}
+	for _, ix := range l.artists {
+		for _, ar := range ix.Artists {
+			if has(ar.Name) {
+				r.Artists = append(r.Artists, ar)
+			}
+		}
+	}
+	for _, al := range l.albums {
+		if has(al.Name) || has(al.Artist) {
+			r.Albums = append(r.Albums, al)
+		}
+	}
+	for _, al := range l.albums {
+		for _, so := range l.tracks[al.ID] {
+			if has(so.Title) {
+				r.Songs = append(r.Songs, so)
+			}
+		}
+	}
+	page := func(n, off, count int) (int, int) { return min(off, n), min(off+count, n) }
+	a0, a1 := page(len(r.Artists), q.ArtistOffset, q.ArtistCount)
+	b0, b1 := page(len(r.Albums), q.AlbumOffset, q.AlbumCount)
+	c0, c1 := page(len(r.Songs), q.SongOffset, q.SongCount)
+	r.Artists, r.Albums, r.Songs = r.Artists[a0:a1], r.Albums[b0:b1], r.Songs[c0:c1]
+	return r, nil
+}
+
+func (l *fakeLibrary) starCall(verb string, t subsonic.StarTarget) error {
+	l.mu.Lock()
+	defer l.mu.Unlock()
+	if l.err != nil {
+		return l.err
+	}
+	for _, ids := range [][]subsonic.ID{t.SongIDs, t.AlbumIDs, t.ArtistIDs} {
+		for _, id := range ids {
+			l.stars = append(l.stars, verb+" "+string(id))
+		}
+	}
+	return nil
+}
+
+func (l *fakeLibrary) Star(_ context.Context, t subsonic.StarTarget) error {
+	return l.starCall("star", t)
+}
+func (l *fakeLibrary) Unstar(_ context.Context, t subsonic.StarTarget) error {
+	return l.starCall("unstar", t)
+}
+
 type fakePlayer struct {
 	st      player.State
 	events  chan player.Event
@@ -83,6 +224,19 @@ func (p *fakePlayer) ResumeFrom(r *player.Resume) {
 func (p *fakePlayer) Resumable(context.Context) (*player.Resume, error) {
 	return p.resume, nil
 }
+func (p *fakePlayer) PlayNext(songs []subsonic.Song) {
+	p.call("playnext")
+	p.played = songs
+}
+func (p *fakePlayer) Enqueue(songs []subsonic.Song) {
+	p.call("enqueue")
+	p.played = songs
+}
+func (p *fakePlayer) Clear() {
+	p.call("clear")
+	p.st.Queue, p.st.Index, p.st.Status = nil, -1, player.Stopped
+}
+func (p *fakePlayer) SetVolumeDB(db float64) { p.st.VolumeDB = max(-60, min(0, db)) }
 func (p *fakePlayer) PlayNow(songs []subsonic.Song, start int) {
 	p.call("playnow")
 	p.played, p.start = songs, start
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
ta.After undefined (type *testApp has no field or method After)
ta.timers undefined (type *testApp has no field or method timers)
ta.LoadCancel undefined (type *testApp has no field or method LoadCancel)
p.SafeY undefined (type Profile has no field or method SafeY)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 0ec9f24..1314e25 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -17,6 +17,15 @@ import (
 type Library interface {
 	GetAlbumList2(ctx context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error)
 	GetAlbum(ctx context.Context, id subsonic.ID) (*subsonic.AlbumWithSongs, error)
+	GetArtists(ctx context.Context) ([]subsonic.ArtistIndex, error)
+	GetArtist(ctx context.Context, id subsonic.ID) (*subsonic.ArtistWithAlbums, error)
+	GetGenres(ctx context.Context) ([]subsonic.Genre, error)
+	GetPlaylists(ctx context.Context) ([]subsonic.Playlist, error)
+	GetPlaylist(ctx context.Context, id subsonic.ID) (*subsonic.PlaylistWithSongs, error)
+	GetStarred2(ctx context.Context) (*subsonic.Starred, error)
+	Search3(ctx context.Context, query string, q subsonic.SearchQuery) (*subsonic.SearchResult, error)
+	Star(ctx context.Context, t subsonic.StarTarget) error
+	Unstar(ctx context.Context, t subsonic.StarTarget) error
 }
 
 // Player is the playback API the screens use (subset of *player.Player).
@@ -24,6 +33,10 @@ type Player interface {
 	State() player.State
 	Events() <-chan player.Event
 	PlayNow(songs []subsonic.Song, start int)
+	PlayNext(songs []subsonic.Song)
+	Enqueue(songs []subsonic.Song)
+	Clear()
+	SetVolumeDB(db float64)
 	TogglePause()
 	Next()
 	Prev()
@@ -52,6 +65,19 @@ type Screen interface {
 	Draw(a *App, c *gfx.Canvas, area gfx.Rect)
 }
 
+// TextInput is a screen that takes typed characters (physical keyboards).
+// Text returns false to let the key act as its button instead (Backspace
+// in an empty field goes back).
+type TextInput interface {
+	Text(a *App, r rune) bool
+}
+
+// owner is a screen that shows other screens inside itself (the HDMI root
+// shows the selected section); their loads run under the owner's entry.
+type owner interface {
+	Owns(s Screen) bool
+}
+
 type Options struct {
 	Display gfx.Display
 	Profile Profile
@@ -82,11 +108,27 @@ type toast struct {
 	until time.Time
 }
 
+type timer struct {
+	at    time.Time
+	owner Screen
+	f     func()
+}
+
+// marquee tracks the one focused text that scrolls because it doesn't fit.
+type marquee struct {
+	text  string
+	since time.Time
+	seen  bool // drawn in the current frame
+}
+
 const (
 	toastTime    = 3 * time.Second
 	maxToasts    = 3
 	exitHold     = 2 * time.Second
 	progressTick = 500 * time.Millisecond
+	marqueeDelay = 1200 * time.Millisecond // before a focused long title starts to scroll
+	marqueeFrame = 50 * time.Millisecond
+	marqueeGap   = "     "
 )
 
 // App owns the screen stack and the event loop. All methods except Post
@@ -99,21 +141,29 @@ type App struct {
 	scaler  *gfx.Scaler
 	stack   []screenEntry
 	toasts  []toast
-	rep     input.Repeater
-	in      chan input.Event
-	post    chan func()
-	loads   int
-	dirty   bool
-	quit    bool
-	bDown   time.Time // when B went down on the root screen (zero if not held)
-	confirm bool      // exit confirmation shown
+	timers  []timer
+	mq      marquee
+	animate bool // something on screen moves (marquee): redraw soon
+	// swallowed holds buttons whose press was typed into a text field, so
+	// their releases are dropped too.
+	swallowed map[input.Button]bool
+	insecure  bool
+	rep       input.Repeater
+	in        chan input.Event
+	post      chan func()
+	loads     int
+	dirty     bool
+	quit      bool
+	bDown     time.Time // when B went down on the root screen (zero if not held)
+	confirm   bool      // exit confirmation shown
 }
 
 func New(o Options) (*App, error) {
 	if o.Now == nil {
 		o.Now = time.Now
 	}
-	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true}
+	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
+		swallowed: map[input.Button]bool{}}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
@@ -152,6 +202,10 @@ func (a *App) Attach(lib Library, pl Player, art ArtSource) {
 	a.dirty = true
 }
 
+// SetInsecure shows the "insecure" badge (the server's certificate isn't
+// checked: insecure_skip_verify). Call on the UI goroutine.
+func (a *App) SetInsecure(on bool) { a.insecure, a.dirty = on, true }
+
 // Post runs f on the UI goroutine. Safe from any goroutine.
 func (a *App) Post(f func()) {
 	select {
@@ -229,11 +283,15 @@ func (a *App) Top() Screen {
 	return a.stack[len(a.stack)-1].s
 }
 
+// entry finds the stack entry of s, or of the screen that shows s inside itself.
 func (a *App) entry(s Screen) *screenEntry {
 	for i := range a.stack {
 		if a.stack[i].s == s {
 			return &a.stack[i]
 		}
+		if o, ok := a.stack[i].s.(owner); ok && o.Owns(s) {
+			return &a.stack[i]
+		}
 	}
 	return nil
 }
@@ -241,11 +299,17 @@ func (a *App) entry(s Screen) *screenEntry {
 // Load runs fn off the UI goroutine and delivers its result to done on the
 // UI goroutine, unless screen s has been popped by then.
 func (a *App) Load(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) {
+	a.LoadCancel(s, fn, done)
+}
+
+// LoadCancel is Load that can be called off: after cancel, fn's context is
+// done and done is never called (search cancels the previous query).
+func (a *App) LoadCancel(s Screen, fn func(ctx context.Context) (any, error), done func(any, error)) (cancel func()) {
 	e := a.entry(s)
 	if e == nil {
-		return
+		return func() {}
 	}
-	ctx := e.ctx
+	ctx, cancel := context.WithCancel(e.ctx)
 	a.loads++
 	go func() {
 		v, err := fn(ctx)
@@ -255,8 +319,15 @@ func (a *App) Load(s Screen, fn func(ctx context.Context) (any, error), done fun
 				done(v, err)
 				a.dirty = true
 			}
+			cancel()
 		})
 	}()
+	return cancel
+}
+
+// After runs f on the UI goroutine after d, unless owner has been popped.
+func (a *App) After(owner Screen, d time.Duration, f func()) {
+	a.timers = append(a.timers, timer{a.o.Now().Add(d), owner, f})
 }
 
 // Toast shows a short message over the current screen.
@@ -320,6 +391,12 @@ func (a *App) untilWake() time.Duration {
 	for _, t := range a.toasts {
 		consider(t.until)
 	}
+	for _, t := range a.timers {
+		consider(t.at)
+	}
+	if a.animate {
+		consider(now.Add(marqueeFrame))
+	}
 	if !a.bDown.IsZero() {
 		consider(a.bDown.Add(exitHold))
 	}
@@ -346,6 +423,25 @@ func (a *App) onWake() {
 		}
 	}
 	a.toasts = kept
+	var due []timer
+	pending := a.timers[:0]
+	for _, t := range a.timers {
+		if now.Before(t.at) {
+			pending = append(pending, t)
+		} else {
+			due = append(due, t)
+		}
+	}
+	a.timers = pending
+	for _, t := range due {
+		if a.entry(t.owner) != nil {
+			t.f()
+			a.dirty = true
+		}
+	}
+	if a.animate {
+		a.dirty = true
+	}
 	if !a.bDown.IsZero() && len(a.stack) == 1 && !now.Before(a.bDown.Add(exitHold)) {
 		a.bDown = time.Time{}
 		a.confirm = true
@@ -369,6 +465,23 @@ func (a *App) onPlayer(ev player.Event) {
 
 func (a *App) onInput(e input.Event) {
 	now := a.o.Now()
+	if e.Rune != 0 && !a.confirm {
+		if e.Kind == input.Press {
+			if t, ok := a.Top().(TextInput); ok && t.Text(a, e.Rune) {
+				a.dirty = true
+				if e.Button != input.BtnNone {
+					a.swallowed[e.Button] = true
+				}
+				return
+			}
+		} else if a.swallowed[e.Button] {
+			delete(a.swallowed, e.Button)
+			return
+		}
+	}
+	if e.Button == input.BtnNone {
+		return
+	}
 	a.rep.Feed(e, now)
 	if e.Button == input.BtnB {
 		if e.Kind == input.Press && len(a.stack) == 1 && !a.confirm {
@@ -418,6 +531,16 @@ func (a *App) dispatch(e input.Event) {
 		if !a.popTo(func(s Screen) bool { _, ok := s.(*NowPlayingScreen); return ok }) {
 			a.Push(NewNowPlayingScreen())
 		}
+	case input.BtnQueue:
+		if !a.hasQueue() {
+			break
+		}
+		if _, ok := a.Top().(*QueueScreen); ok {
+			break
+		}
+		if !a.popTo(func(s Screen) bool { _, ok := s.(*QueueScreen); return ok }) {
+			a.Push(NewQueueScreen())
+		}
 	}
 }
 
@@ -425,25 +548,40 @@ func (a *App) hasQueue() bool {
 	return a.o.Player != nil && len(a.o.Player.State().Queue) > 0
 }
 
+// hasCurrent reports whether a song is selected (the mini bar shows it).
+func (a *App) hasCurrent() bool {
+	if a.o.Player == nil {
+		return false
+	}
+	_, ok := a.o.Player.State().Current()
+	return ok
+}
+
 func (a *App) render() error {
 	a.dirty = false
+	a.animate, a.mq.seen = false, false
 	c := a.canvas
 	c.Clear(colBg)
 	top := a.Top()
 	p := a.P
-	body := gfx.R(0, 0, p.W, p.H)
+	// Content stays inside the title-safe area (SafeY lines top and bottom;
+	// panels still run to the edges).
+	body := gfx.R(0, p.SafeY, p.W, p.H-2*p.SafeY)
 	if top != nil {
 		_, fullscreen := top.(*NowPlayingScreen)
 		if !fullscreen {
 			a.drawHeader(c, top.Title())
-			body = gfx.R(0, p.HeaderH, p.W, p.H-p.HeaderH)
-			if a.hasQueue() {
+			body = gfx.R(0, p.SafeY+p.HeaderH, p.W, p.H-2*p.SafeY-p.HeaderH)
+			if a.hasCurrent() {
 				body.H -= p.MiniBarH
-				a.drawMiniBar(c, gfx.R(0, p.H-p.MiniBarH, p.W, p.MiniBarH))
+				a.drawMiniBar(c, gfx.R(0, body.Bottom(), p.W, p.MiniBarH))
 			}
 		}
 		top.Draw(a, c, body)
 	}
+	if !a.mq.seen {
+		a.mq = marquee{}
+	}
 	a.drawToasts(c)
 	if a.confirm {
 		a.drawConfirm(c)
@@ -453,16 +591,46 @@ func (a *App) render() error {
 
 func (a *App) drawHeader(c *gfx.Canvas, title string) {
 	p := a.P
-	c.Fill(gfx.R(0, 0, p.W, p.HeaderH), colPanel)
+	c.Fill(gfx.R(0, 0, p.W, p.SafeY+p.HeaderH), colPanel)
 	f := a.F.Title
-	y := (p.HeaderH + f.Ascent() - f.Descent()) / 2
-	f.Draw(c, p.Margin, y, f.Truncate(title, p.W-2*p.Margin), colText, c.Bounds())
+	y := p.SafeY + (p.HeaderH+f.Ascent()-f.Descent())/2
+	w := p.W - 2*p.Margin
+	if a.insecure {
+		fs := a.F.Small
+		badge := "insecure"
+		bw := fs.Measure(badge)
+		fs.Draw(c, p.W-p.Margin-bw, y, badge, colError, c.Bounds())
+		w -= bw + p.Margin/2
+	}
+	f.Draw(c, p.Margin, y, f.Truncate(title, w), colText, c.Bounds())
+}
+
+// drawFit draws s at (x, baseline y) within w pixels: cut with "…" when it
+// doesn't fit, or, when focused, scrolling after a short pause (marquee).
+func (a *App) drawFit(c *gfx.Canvas, f *gfx.Font, x, y, w int, s string, col gfx.Color, clip gfx.Rect, focused bool) {
+	if !focused || f.Measure(s) <= w {
+		f.Draw(c, x, y, f.Truncate(s, w), col, clip)
+		return
+	}
+	now := a.o.Now()
+	if a.mq.text != s {
+		a.mq = marquee{text: s, since: now}
+	}
+	a.mq.seen, a.animate = true, true
+	off := 0
+	if run := now.Sub(a.mq.since) - marqueeDelay; run > 0 {
+		off = int(int64(run) * int64(a.P.MarqueeSpeed) / int64(time.Second))
+		off %= f.Measure(s + marqueeGap)
+	}
+	vis, dx := f.Marquee(s, off)
+	area := gfx.R(x, y-f.Ascent(), w, f.Height()).Intersect(clip)
+	f.Draw(c, x+dx, y, vis, col, area)
 }
 
 func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
 	st := a.o.Player.State()
 	song, _ := st.Current()
-	c.Fill(r, colPanel)
+	c.Fill(gfx.R(r.X, r.Y, r.W, a.P.H-r.Y), colPanel) // to the bottom edge, past the safe area
 	p := a.P
 	x := p.Margin
 	art := r.H - 2*max(p.Margin/3, 2)
@@ -487,7 +655,7 @@ func (a *App) drawMiniBar(c *gfx.Canvas, r gfx.Rect) {
 func (a *App) drawToasts(c *gfx.Canvas) {
 	f := a.F.Body
 	p := a.P
-	y := p.H - p.MiniBarH - p.Margin
+	y := p.H - p.SafeY - p.MiniBarH - p.Margin
 	for i := len(a.toasts) - 1; i >= 0; i-- {
 		text := f.Truncate(a.toasts[i].text, p.W-4*p.Margin)
 		w := f.Measure(text) + p.Margin
diff --git a/internal/ui/theme.go b/internal/ui/theme.go
index d845701..cf39558 100644
--- a/internal/ui/theme.go
+++ b/internal/ui/theme.go
@@ -23,13 +23,17 @@ type Profile struct {
 	ArtNow   int // Now Playing cover size
 	ArtAlbum int // album header cover size
 	MiniBarH int
+	// SafeY keeps content off the top and bottom lines a CRT's overscan
+	// hides (title-safe, spec §8.2); panels still run to the edge.
+	SafeY        int
+	MarqueeSpeed int // px per second
 }
 
 var (
 	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
-		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72}
-	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 12, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
-		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24}
+		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60}
+	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 16, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
+		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24, SafeY: 12, MarqueeSpeed: 24}
 )
 
 // PickProfile chooses the layout for a framebuffer (spec §8.1): "auto" picks
```

- [ ] **Step 4: Regenerate the CRT screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`. The CRT goldens change: `album-crt`, `albums-crt`, `home-crt`, `home-minibar-crt`, `message-crt`, `nowplaying-crt`, `queue-toast-crt`. Open each one. The header text and the mini bar now sit 12 lines inside the top and bottom edges, text starts 16 px from the sides, and the header and mini bar panels still reach the edges. The HDMI goldens must not change: `git status internal/ui/testdata` lists only `-crt` files.

The CRT Now Playing cover is small in this task's golden; Task 5 puts it beside the text.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go internal/ui/fakes_test.go internal/ui/theme.go internal/ui/testdata/golden
git commit -m "ui: timers, cancellable loads, typed text, Q key, CRT title-safe area, marquee" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 4: Widgets — cover grid, tabs, star state, the menu overlay

**Files:**
- Create: `internal/ui/grid.go`, `internal/ui/tabs.go`, `internal/ui/star.go`, `internal/ui/menu.go`
- Modify: `internal/ui/list.go`, `internal/ui/icons.go`, `internal/ui/app.go`
- Test: `internal/ui/widgets_test.go` (new)

**Interfaces:**
- **Produces:**
  - **`Grid`** (`Focus`; `Handle(e, n) bool`; `Draw(c, area, n, cellW, cellH, cell)`; `NearEnd(n)`).
    - It returns false for moves it can't make: Left in the first column (the sidebar takes it) and Up in the first row (tabs take it).
  - **`List`** now also returns false for Up on its first row and Down on its last.
  - **`Tabs`** (`Labels`, `Sel`; `Handle(e)`, false for Left on the first tab; `Draw(a, c, r, focused)`).
  - **Star icon:** `iconStar`, with `starPoints` and `fillPolygon`.
  - **Star state:**
    - `starItem`, built with `songStar`, `albumStar` or `artistStar`.
    - `(*App).isStarred(it)`: this session's changes, else the server's.
    - `(*App).toggleStar(owner, it)` stars on the server, then remembers the result and shows a toast.
  - **`MenuScreen`** (`NewMenuScreen(parent, title, []menuEntry)`):
    - drawn over its parent
    - A runs the entry after closing the menu
    - B or X closes it
    - Start stays global

- [ ] **Step 1: Write the failing tests**

`internal/ui/widgets_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

func press(b input.Button) input.Event { return input.Event{Button: b, Kind: input.Press} }

func TestGridNavigation(t *testing.T) {
	var g Grid
	c := gfx.NewCanvas(300, 200)
	var drawn []int
	draw := func() {
		drawn = nil
		g.Draw(c, c.Bounds(), 7, 100, 100, func(i int, r gfx.Rect, focused bool) { drawn = append(drawn, i) })
	}
	draw() // 3 columns, 2 rows visible
	steps := []struct {
		b    input.Button
		ok   bool
		want int
	}{
		{input.BtnRight, true, 1}, {input.BtnRight, true, 2}, {input.BtnRight, false, 2}, // right edge
		{input.BtnUp, false, 2},                            // first row: tabs can take it
		{input.BtnDown, true, 5}, {input.BtnDown, true, 6}, // into the short last row
		{input.BtnDown, false, 6}, {input.BtnLeft, false, 6}, // first column: the sidebar can take it
		{input.BtnUp, true, 3}, {input.BtnL, true, 0}, {input.BtnR, true, 6},
	}
	for i, s := range steps {
		if ok := g.Handle(press(s.b), 7); ok != s.ok || g.Focus != s.want {
			t.Fatalf("step %d (%v): ok %v focus %d, want %v %d", i, s.b, ok, g.Focus, s.ok, s.want)
		}
	}
	draw()
	if !slices.Equal(drawn, []int{3, 4, 5, 6}) {
		t.Fatalf("drawn %v, want rows 1-2 (the focused row is visible)", drawn)
	}
	if !g.NearEnd(7) {
		t.Fatal("NearEnd false on the last row")
	}
	if (&Grid{}).Handle(press(input.BtnDown), 0) {
		t.Fatal("empty grid used a key")
	}
}

func TestListAndTabsLetNeighboursTakeFocus(t *testing.T) {
	var l List
	if l.Handle(press(input.BtnUp), 3) {
		t.Fatal("Up on the first row was used")
	}
	l.Focus = 2
	if l.Handle(press(input.BtnDown), 3) {
		t.Fatal("Down on the last row was used")
	}
	tabs := Tabs{Labels: []string{"A", "B"}}
	if tabs.Handle(press(input.BtnLeft)) {
		t.Fatal("Left on the first tab was used")
	}
	if !tabs.Handle(press(input.BtnRight)) || !tabs.Handle(press(input.BtnRight)) || tabs.Sel != 1 {
		t.Fatalf("tabs: sel %d", tabs.Sel)
	}
}

func TestStarIcon(t *testing.T) {
	c := gfx.NewCanvas(21, 21)
	drawIcon(c, iconStar, c.Bounds(), colAccent)
	if c.At(10, 10) != colAccent || c.At(10, 1) != colAccent {
		t.Fatal("star centre or top point not filled")
	}
	if c.At(0, 0) != gfx.RGB(0, 0, 0) || c.At(20, 20) != gfx.RGB(0, 0, 0) || c.At(10, 20) != gfx.RGB(0, 0, 0) {
		t.Fatal("star filled a corner or between the lower points")
	}
}

func TestToggleStarRemembersAndToasts(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	s := &probe{}
	ta.Push(s)
	it := albumStar(ta.lib.albums[1])
	ta.toggleStar(s, it)
	ta.settle(t)
	if !ta.isStarred(it) || ta.toasts[0].text != "Starred Homogenic" {
		t.Fatalf("starred %v, toasts %v", ta.isStarred(it), ta.toasts)
	}
	ta.toggleStar(s, it)
	ta.settle(t)
	if ta.isStarred(it) || !slices.Equal(ta.lib.stars, []string{"star al-2", "unstar al-2"}) {
		t.Fatalf("starred %v, calls %v", ta.isStarred(it), ta.lib.stars)
	}
	ta.lib.err = errOffline
	ta.toggleStar(s, it)
	ta.settle(t)
	if ta.isStarred(it) {
		t.Fatal("a failed star changed the state")
	}
	if last := ta.toasts[len(ta.toasts)-1].text; last != "Couldn't star Homogenic: error" {
		t.Fatalf("toast %q", last)
	}
}

func TestMenuRunsEntryAndCloses(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	parent := &probe{}
	ta.Push(parent)
	var ran []string
	entries := []menuEntry{
		{"One", func(a *App) { ran = append(ran, "one") }},
		{"Two", func(a *App) { ran = append(ran, "two:"+a.Top().Title()) }},
	}
	ta.Push(NewMenuScreen(parent, "Item", entries))
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // stays on the last entry
	ta.press(input.BtnY)    // swallowed: the menu is modal
	ta.press(input.BtnA)
	if !slices.Equal(ran, []string{"two:Probe"}) || ta.Top() != parent {
		t.Fatalf("ran %v, top %T", ran, ta.Top())
	}
	ta.Push(NewMenuScreen(parent, "Item", entries))
	ta.press(input.BtnStart) // global play/pause still works
	ta.press(input.BtnX)
	if ta.Top() != parent || len(ran) != 1 || !slices.Equal(ta.pl.calls, []string{"toggle"}) {
		t.Fatalf("top %T, ran %v, player %v", ta.Top(), ran, ta.pl.calls)
	}
}

func TestGoldenMenu(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		album := NewAlbumScreen(ta.lib.albums[0])
		ta.Push(album)
		ta.settle(t)
		noop := func(*App) {}
		ta.Push(NewMenuScreen(album, "Радио Африка", []menuEntry{
			{"Play now", noop}, {"Play next", noop}, {"Add to queue", noop}, {"Star", noop}, {"Go to artist", noop},
		}))
		ta.press(input.BtnDown)
		golden(t, "menu-"+p.Name, ta.settle(t))
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: Grid
undefined: Tabs
undefined: iconStar
undefined: albumStar
ta.toggleStar undefined (type *testApp has no field or method toggleStar)
ta.isStarred undefined (type *testApp has no field or method isStarred)
```

- [ ] **Step 3: Implement**

`internal/ui/grid.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Grid is a focusable grid of equal cells that scrolls by rows (HDMI cover
// grids). Like List, it returns false for moves it can't make, so a
// neighbour can take the focus: Left in the first column (the sidebar), Up
// in the first row (tabs).
type Grid struct {
	Focus int
	top   int // first visible row
	cols  int // at the last Draw
	rows  int
}

func (g *Grid) columns() int { return max(g.cols, 1) }

// Handle moves the focus: arrows by one cell, L/R by a page.
func (g *Grid) Handle(e input.Event, n int) bool {
	if n == 0 {
		return false
	}
	cols := g.columns()
	f := g.Focus
	switch e.Button {
	case input.BtnUp:
		if f < cols {
			return false
		}
		f -= cols
	case input.BtnDown:
		if f/cols == (n-1)/cols {
			return false
		}
		f = min(f+cols, n-1) // into a shorter last row: its last cell
	case input.BtnLeft:
		if f%cols == 0 {
			return false
		}
		f--
	case input.BtnRight:
		if f%cols == cols-1 || f == n-1 {
			return false
		}
		f++
	case input.BtnL:
		f = max(f-cols*max(g.rows, 1), f%cols)
	case input.BtnR:
		f = min(f+cols*max(g.rows, 1), n-1)
	default:
		return false
	}
	g.Focus = f
	return true
}

// Draw lays out n cells of cellW×cellH in area (centred horizontally),
// keeping the focused row visible, and calls cell for each visible one.
func (g *Grid) Draw(c *gfx.Canvas, area gfx.Rect, n, cellW, cellH int, cell func(i int, r gfx.Rect, focused bool)) {
	g.cols = max(area.W/cellW, 1)
	g.rows = max(area.H/cellH, 1)
	g.Focus = min(max(g.Focus, 0), max(n-1, 0))
	row := g.Focus / g.cols
	if row < g.top {
		g.top = row
	}
	if row >= g.top+g.rows {
		g.top = row - g.rows + 1
	}
	x0 := area.X + (area.W-g.cols*cellW)/2
	for i := g.top * g.cols; i < n && i < (g.top+g.rows)*g.cols; i++ {
		r := gfx.R(x0+i%g.cols*cellW, area.Y+(i/g.cols-g.top)*cellH, cellW, cellH)
		cell(i, r, i == g.Focus)
	}
}

// NearEnd reports whether the focus is within a page of the end (to load more).
func (g *Grid) NearEnd(n int) bool {
	return n > 0 && g.Focus >= n-g.columns()*max(g.rows, 1)
}
```

`internal/ui/tabs.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Tabs is a row of labels above a screen's content (Albums: A–Z / by year /
// by genre; Starred; search results). Left/Right switch while it has focus.
type Tabs struct {
	Labels []string
	Sel    int
}

// Handle switches tabs; false at the left end so the sidebar can take focus.
func (t *Tabs) Handle(e input.Event) bool {
	switch e.Button {
	case input.BtnLeft:
		if t.Sel == 0 {
			return false
		}
		t.Sel--
	case input.BtnRight:
		t.Sel = min(t.Sel+1, len(t.Labels)-1)
	default:
		return false
	}
	return true
}

// Draw draws the labels left to right in r; the selected one is underlined,
// and highlighted when the row has the focus.
func (t *Tabs) Draw(a *App, c *gfx.Canvas, r gfx.Rect, focused bool) {
	p := a.P
	f := a.F.Body
	x := r.X + p.Margin
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	for i, l := range t.Labels {
		w := f.Measure(l)
		col := colDim
		if i == t.Sel {
			col = colText
			if focused {
				c.Fill(gfx.R(x-p.Margin/4, r.Y, w+p.Margin/2, r.H), colFocus)
			}
			c.Fill(gfx.R(x, r.Bottom()-max(p.Margin/8, 2), w, max(p.Margin/8, 2)), colAccent)
		}
		f.Draw(c, x, y, l, col, r)
		x += w + p.Margin
	}
}
```

`internal/ui/star.go` (new file):

```go
package ui

import (
	"context"

	"mistersubsonic/internal/subsonic"
)

type starKind int

const (
	starSong starKind = iota
	starAlbum
	starArtist
)

// starItem is something the user can star: a song, an album or an artist.
type starItem struct {
	kind   starKind
	id     subsonic.ID
	name   string
	server bool // starred according to the server's data
}

func songStar(s subsonic.Song) starItem {
	return starItem{starSong, s.ID, s.Title, s.IsStarred()}
}
func albumStar(al subsonic.Album) starItem {
	return starItem{starAlbum, al.ID, al.Name, al.IsStarred()}
}
func artistStar(ar subsonic.Artist) starItem {
	return starItem{starArtist, ar.ID, ar.Name, ar.IsStarred()}
}

func (it starItem) target() subsonic.StarTarget {
	switch it.kind {
	case starAlbum:
		return subsonic.StarTarget{AlbumIDs: []subsonic.ID{it.id}}
	case starArtist:
		return subsonic.StarTarget{ArtistIDs: []subsonic.ID{it.id}}
	}
	return subsonic.StarTarget{SongIDs: []subsonic.ID{it.id}}
}

// isStarred is the star state of it: changes made in this session win over
// the (possibly older) server data a screen loaded.
func (a *App) isStarred(it starItem) bool {
	if on, ok := a.stars[it.id]; ok {
		return on
	}
	return it.server
}

// toggleStar stars or unstars it on the server (off the UI goroutine,
// under owner) and remembers the new state once the server agrees.
func (a *App) toggleStar(owner Screen, it starItem) {
	lib := a.Library()
	if lib == nil {
		return
	}
	on := !a.isStarred(it)
	a.Load(owner, func(ctx context.Context) (any, error) {
		if on {
			return nil, lib.Star(ctx, it.target())
		}
		return nil, lib.Unstar(ctx, it.target())
	}, func(_ any, err error) {
		verb := "unstar"
		if on {
			verb = "star"
		}
		if err != nil {
			a.Toast("Couldn't %s %s: %s", verb, it.name, subsonic.Classify(err))
			return
		}
		a.stars[it.id] = on
		if on {
			a.Toast("Starred %s", it.name)
		} else {
			a.Toast("Unstarred %s", it.name)
		}
	})
}
```

`internal/ui/menu.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

type menuEntry struct {
	label string
	run   func(a *App)
}

// MenuScreen is a context menu (X) drawn over the screen it was opened
// from. A runs the focused entry, B or X closes it; Start stays global.
type MenuScreen struct {
	parent  Screen
	title   string
	entries []menuEntry
	list    List
}

func NewMenuScreen(parent Screen, title string, entries []menuEntry) *MenuScreen {
	return &MenuScreen{parent: parent, title: title, entries: entries}
}

func (s *MenuScreen) Title() string { return s.parent.Title() }
func (s *MenuScreen) Enter(a *App)  {}

func (s *MenuScreen) Handle(a *App, e input.Event) bool {
	if s.list.Handle(e, len(s.entries)) {
		return true
	}
	if e.Kind != input.Press {
		return true
	}
	switch e.Button {
	case input.BtnA:
		// Close first: the entry may open a screen or load under the parent.
		a.Pop()
		if len(s.entries) > 0 {
			s.entries[s.list.Focus].run(a)
		}
	case input.BtnB, input.BtnX:
		a.Pop()
	case input.BtnStart:
		return false
	}
	return true
}

func (s *MenuScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	s.parent.Draw(a, c, area)
	c.Fill(area, colOverlay)
	p := a.P
	fs := a.F.Small
	w := min(area.W-2*p.Margin, p.W*2/5+2*p.Margin)
	titleH := fs.Height() + p.Margin/2
	h := min(titleH+len(s.entries)*p.RowH+p.Margin/2, area.H-p.Margin)
	panel := gfx.R(area.X+(area.W-w)/2, area.Y+(area.H-h)/2, w, h)
	c.Fill(panel, colPanel)
	fs.Draw(c, panel.X+p.Margin, panel.Y+p.Margin/4+fs.Ascent(), fs.Truncate(s.title, w-2*p.Margin), colDim, panel)
	rows := gfx.R(panel.X, panel.Y+titleH, panel.W, panel.H-titleH-p.Margin/4)
	s.list.Draw(c, rows, len(s.entries), p.RowH, func(i int, r gfx.Rect, focused bool) {
		a.drawTextRow(c, r, "", false, s.entries[i].label, "", colText)
	})
}
```

Then the list boundaries, the star icon and the star map:

Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 1314e25..d76b0d4 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -148,6 +148,7 @@ type App struct {
 	// their releases are dropped too.
 	swallowed map[input.Button]bool
 	insecure  bool
+	stars     map[subsonic.ID]bool // star changes made in this session
 	rep       input.Repeater
 	in        chan input.Event
 	post      chan func()
@@ -163,7 +164,7 @@ func New(o Options) (*App, error) {
 		o.Now = time.Now
 	}
 	a := &App{o: o, P: o.Profile, in: make(chan input.Event, 64), post: make(chan func(), 256), dirty: true,
-		swallowed: map[input.Button]bool{}}
+		swallowed: map[input.Button]bool{}, stars: map[subsonic.ID]bool{}}
 	regular, err := gfx.LoadTypeface(false, o.FallbackFonts)
 	if err != nil {
 		return nil, err
diff --git a/internal/ui/icons.go b/internal/ui/icons.go
index 4471910..e92fc0b 100644
--- a/internal/ui/icons.go
+++ b/internal/ui/icons.go
@@ -1,6 +1,9 @@
 package ui
 
 import (
+	"math"
+	"sort"
+
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/player"
 )
@@ -12,6 +15,7 @@ const (
 	iconPause
 	iconStop
 	iconBusy
+	iconStar
 )
 
 // drawIcon draws a simple vector icon filling the square r. Icons are drawn
@@ -39,6 +43,47 @@ func drawIcon(c *gfx.Canvas, ic icon, r gfx.Rect, col gfx.Color) {
 		for i := 0; i < 3; i++ {
 			c.Fill(gfx.R(r.X+i*2*d, r.Y+(r.H-d)/2, d, d), col)
 		}
+	case iconStar:
+		fillPolygon(c, starPoints(r), col)
+	}
+}
+
+// starPoints is a five-pointed star in r: ten vertices, outer and inner.
+func starPoints(r gfx.Rect) [][2]float64 {
+	cx, cy := float64(r.X)+float64(r.W)/2, float64(r.Y)+float64(r.H)/2
+	outer := float64(min(r.W, r.H)) / 2
+	pts := make([][2]float64, 10)
+	for i := range pts {
+		rad := outer
+		if i%2 == 1 {
+			rad *= 0.4
+		}
+		a := -math.Pi/2 + float64(i)*math.Pi/5
+		pts[i] = [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)}
+	}
+	return pts
+}
+
+// fillPolygon fills the pixels whose centres are inside pts (even-odd rule).
+func fillPolygon(c *gfx.Canvas, pts [][2]float64, col gfx.Color) {
+	minY, maxY := pts[0][1], pts[0][1]
+	for _, p := range pts {
+		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
+	}
+	for y := int(minY); y <= int(maxY); y++ {
+		py := float64(y) + 0.5
+		var xs []float64
+		for i := range pts {
+			a, b := pts[i], pts[(i+1)%len(pts)]
+			if (a[1] <= py) != (b[1] <= py) {
+				xs = append(xs, a[0]+(py-a[1])*(b[0]-a[0])/(b[1]-a[1]))
+			}
+		}
+		sort.Float64s(xs)
+		for i := 0; i+1 < len(xs); i += 2 {
+			x0, x1 := int(math.Round(xs[i])), int(math.Round(xs[i+1]))
+			c.Fill(gfx.R(x0, y, x1-x0, 1), col)
+		}
 	}
 }
 
diff --git a/internal/ui/list.go b/internal/ui/list.go
index 7e9a4ef..2d82070 100644
--- a/internal/ui/list.go
+++ b/internal/ui/list.go
@@ -13,7 +13,8 @@ type List struct {
 }
 
 // Handle moves the focus: Up/Down by one, L/R by a page. It reports whether
-// the event was used.
+// the event was used; Up on the first row and Down on the last aren't, so a
+// neighbour (tabs above, a keyboard) can take the focus.
 func (l *List) Handle(e input.Event, n int) bool {
 	if n == 0 {
 		return false
@@ -21,8 +22,14 @@ func (l *List) Handle(e input.Event, n int) bool {
 	page := max(l.rows-1, 1)
 	switch e.Button {
 	case input.BtnUp:
+		if l.Focus <= 0 {
+			return false
+		}
 		l.Focus--
 	case input.BtnDown:
+		if l.Focus >= n-1 {
+			return false
+		}
 		l.Focus++
 	case input.BtnL:
 		l.Focus -= page
```

- [ ] **Step 4: Generate the menu screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`, and two new goldens: `menu-crt`, `menu-hdmi`. The album page is dimmed and a centred panel is titled "Радио Африка". The panel holds five entries, with the second ("Play next") highlighted. No other golden changes.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/grid.go internal/ui/icons.go internal/ui/list.go internal/ui/menu.go internal/ui/star.go internal/ui/tabs.go internal/ui/widgets_test.go internal/ui/testdata/golden
git commit -m "ui: cover grid, tabs, star state, context menu overlay" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 5: Now Playing volume, star and badge; the queue's X menu

**Files:**
- Modify: `internal/ui/screens_play.go`
- Test: `internal/ui/play_test.go` (new)

**Interfaces:**
- **Consumes:** `toggleStar`, `songStar`, `iconStar`, `MenuScreen` (Task 4); `Player.SetVolumeDB`, `Player.Clear` (Task 3).
- **Produces:**
  - **Now Playing keys:** Up/Down change the volume by ±1 dB (held to repeat). X stars or unstars the song.
  - **Now Playing display:**
    - a star after a starred title
    - "Vol −12 dB" in the status line (`volumeLabel`)
    - "insecure: certificate not checked" when `SetInsecure(true)`
  - **CRT layout:** the cover sits beside the text, as on HDMI, which fixes the tiny CRT cover.
  - **Queue:** X opens Remove · Clear queue. Clear leaves Queue and Now Playing.

- [ ] **Step 1: Write the failing tests**

`internal/ui/play_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/input"
)

func TestNowPlayingVolumeKeys(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnDown)
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Press})
	ta.dispatch(input.Event{Button: input.BtnDown, Kind: input.Repeat})
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Release})
	if v := ta.pl.st.VolumeDB; v != -3 {
		t.Fatalf("volume %v after three steps down, want -3", v)
	}
	for range 5 {
		ta.press(input.BtnUp)
	}
	if v := ta.pl.st.VolumeDB; v != 0 {
		t.Fatalf("volume %v, want clamped to 0", v)
	}
	if volumeLabel(-12.4) != "Vol −12 dB" || volumeLabel(0) != "Vol 0 dB" {
		t.Fatalf("labels %q %q", volumeLabel(-12.4), volumeLabel(0))
	}
}

func TestNowPlayingXStarsTheSong(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	ta.Push(NewNowPlayingScreen())
	ta.press(input.BtnX)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1"}) || !ta.isStarred(songStar(ta.pl.st.Queue[0])) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.press(input.BtnX)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star s1", "unstar s1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
}

func TestQueueMenuRemoveAndClear(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	playingState(ta)
	home := NewHomeScreen()
	ta.Push(home)
	ta.Push(NewNowPlayingScreen())
	ta.Push(NewQueueScreen())
	ta.settle(t)
	ta.press(input.BtnDown) // "Время Луны"
	ta.press(input.BtnX)
	if _, ok := ta.Top().(*MenuScreen); !ok {
		t.Fatalf("X opened %T, want the menu", ta.Top())
	}
	ta.press(input.BtnA) // Remove
	if !slices.Equal(ta.pl.calls, []string{"remove"}) || ta.toasts[0].text != "Removed Время Луны" {
		t.Fatalf("calls %v toasts %v", ta.pl.calls, ta.toasts)
	}
	ta.press(input.BtnX)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Clear queue
	if !slices.Equal(ta.pl.calls, []string{"remove", "clear"}) || ta.Top() != home {
		t.Fatalf("calls %v, top %T", ta.pl.calls, ta.Top())
	}
}

func TestGoldenNowPlayingStarredInsecure(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		playingState(ta)
		ta.pl.st.VolumeDB = -12
		ta.pl.st.Queue[0].Starred = "2026-09-29T10:00:00Z"
		ta.SetInsecure(true)
		ta.Push(NewNowPlayingScreen())
		golden(t, "nowplaying-starred-"+p.Name, ta.settle(t))
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: volumeLabel
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index d865fb3..11fa885 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -1,6 +1,8 @@
 package ui
 
 import (
+	"fmt"
+	"math"
 	"time"
 
 	"mistersubsonic/internal/gfx"
@@ -20,6 +22,7 @@ const (
 	// The pending target stays on screen this long after its last Seek, until
 	// the player's position catches up.
 	seekShow = time.Second
+	volStep  = 1.0 // dB per Up/Down
 )
 
 // NowPlayingScreen is the full-screen player (spec §8.2).
@@ -113,12 +116,25 @@ func (s *NowPlayingScreen) Handle(a *App, e input.Event) bool {
 		return true
 	}
 	s.settle(a, st)
+	switch e.Button {
+	case input.BtnUp, input.BtnDown: // volume, held to repeat
+		step := volStep
+		if e.Button == input.BtnDown {
+			step = -step
+		}
+		pl.SetVolumeDB(st.VolumeDB + step)
+		return true
+	}
 	if e.Kind != input.Press {
 		return false
 	}
 	switch e.Button {
 	case input.BtnA:
 		pl.TogglePause()
+	case input.BtnX:
+		if song, ok := st.Current(); ok {
+			a.toggleStar(s, songStar(song))
+		}
 	case input.BtnL:
 		pl.Prev()
 	case input.BtnR:
@@ -153,25 +169,32 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	ft, fb, fs := a.F.Title, a.F.Body, a.F.Small
 	barH := max(p.Margin/6, 3)
 	// Height of the text block, used to centre everything vertically.
-	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + fs.Height()
+	textH := ft.Height() + 2*fb.Height() + fs.Height() + p.Margin + barH + p.Margin/4 + fs.Height() + p.Margin/2 + fb.Height() + 2*fs.Height()
+	// Art left, text right, centred as a block. The gap is narrower on a
+	// CRT, where the text needs the width.
+	gap := 2 * p.Margin
 	art := min(p.ArtNow, area.H-2*p.Margin)
-	var text gfx.Rect
-	if p.W >= p.H*3/2 { // wide: art left, text right, both centred
-		a.drawArt(c, song.CoverArt, gfx.R(area.X+p.Margin*2, area.Y+(area.H-art)/2, art, art))
-		x := area.X + p.Margin*3 + art
-		text = gfx.R(x, area.Y+(area.H-max(art, textH))/2, area.Right()-p.Margin*2-x, max(art, textH))
-	} else { // narrow (CRT): art on top, text below, centred as a block
-		art = min(art, area.H-textH-2*p.Margin)
-		top := area.Y + (area.H-art-p.Margin/2-textH)/2
-		a.drawArt(c, song.CoverArt, gfx.R(area.X+(area.W-art)/2, top, art, art))
-		text = gfx.R(area.X+p.Margin, top+art+p.Margin/2, area.W-2*p.Margin, textH)
+	if p.W < p.H*3/2 {
+		gap = p.Margin
+		art = min(art, area.W*2/5)
 	}
+	a.drawArt(c, song.CoverArt, gfx.R(area.X+gap, area.Y+(area.H-art)/2, art, art))
+	x := area.X + gap + art + p.Margin
+	text := gfx.R(x, area.Y+(area.H-max(art, textH))/2, area.Right()-gap-x, max(art, textH))
 	y := text.Y
 	line := func(f *gfx.Font, s string, col gfx.Color) {
 		f.Draw(c, text.X, y+f.Ascent(), f.Truncate(s, text.W), col, c.Bounds())
 		y += f.Height()
 	}
-	line(ft, song.Title, colText)
+	title := song.Title
+	if a.isStarred(songStar(song)) {
+		st := ft.Ascent() * 3 / 4
+		drawIcon(c, iconStar, gfx.R(text.Right()-st, y+(ft.Ascent()-st)/2+ft.Descent()/2, st, st), colAccent)
+		ft.Draw(c, text.X, y+ft.Ascent(), ft.Truncate(title, text.W-st-p.Margin/2), colText, c.Bounds())
+		y += ft.Height()
+	} else {
+		line(ft, title, colText)
+	}
 	line(fb, song.Artist, colDim)
 	line(fb, song.Album, colDim)
 	y += p.Margin / 2
@@ -199,12 +222,25 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			mode += "  ·  " + m.label
 		}
 	}
+	mode += "  ·  " + volumeLabel(st.VolumeDB)
 	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
 	y += fb.Height()
 	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
 		next := st.Queue[st.NextIndex]
 		line(fs, "Next: "+next.Title+" — "+next.Artist, colDim)
 	}
+	if a.insecure {
+		line(fs, "insecure: certificate not checked", colError)
+	}
+}
+
+// volumeLabel is e.g. "Vol −12 dB" (a real minus sign).
+func volumeLabel(db float64) string {
+	v := int(math.Round(db))
+	if v < 0 {
+		return fmt.Sprintf("Vol −%d dB", -v)
+	}
+	return "Vol 0 dB"
 }
 
 // QueueScreen lists the play queue.
@@ -246,9 +282,21 @@ func (s *QueueScreen) Handle(a *App, e input.Event) bool {
 		a.Pop()
 		return true
 	case input.BtnX:
-		title := st.Queue[s.list.Focus].Title
-		a.Player().Remove(s.list.Focus)
-		a.Toast("Removed %s", title)
+		i := s.list.Focus
+		title := st.Queue[i].Title
+		a.Push(NewMenuScreen(s, title, []menuEntry{
+			{"Remove", func(a *App) {
+				a.Player().Remove(i)
+				a.Toast("Removed %s", title)
+			}},
+			{"Clear queue", func(a *App) {
+				a.Player().Clear()
+				for len(a.stack) > 1 && isPlayScreen(a.Top()) {
+					a.Pop() // nothing left to show here or in Now Playing
+				}
+				a.Toast("Queue cleared")
+			}},
+		}))
 		return true
 	}
 	return false
@@ -273,6 +321,14 @@ func (s *QueueScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	})
 }
 
+func isPlayScreen(s Screen) bool {
+	switch s.(type) {
+	case *QueueScreen, *NowPlayingScreen:
+		return true
+	}
+	return false
+}
+
 // MessageScreen shows a problem (no config, server unreachable) with an
 // optional A-to-retry action.
 type MessageScreen struct {
```

- [ ] **Step 4: Regenerate the Now Playing screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`. The goldens that change are `nowplaying-crt`, `nowplaying-hdmi`, `nowplaying-starred-crt`, `nowplaying-starred-hdmi`.
- **`nowplaying-crt`:** the cover (110 px) is on the left and the text on the right.
- **Both layouts:** "Playing · Vol 0 dB".
- **`nowplaying-starred-*`:** a star to the right of the title, "Vol −12 dB", and a red "insecure: certificate not checked" line.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/play_test.go internal/ui/screens_play.go internal/ui/testdata/golden
git commit -m "ui: Now Playing volume, star and insecure badge; queue Remove/Clear menu" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 6: Album and artist browsing, item menus, Select to shuffle

**Files:**
- Create: `internal/ui/views.go`, `internal/ui/menus.go`, `internal/ui/tabbed.go`, `internal/ui/screens_artists.go`
- Modify: `internal/ui/rows.go`, `internal/ui/theme.go`, `internal/ui/screens_home.go`, `internal/ui/screens_album.go`, `internal/ui/app.go`
- Test: `internal/ui/browse_test.go` (new); `internal/ui/fakes_test.go` and `internal/ui/ui_test.go` (modified)

**Interfaces:**
- **Consumes:** `Grid`, `Tabs`, `MenuScreen`, star state (Task 4); `drawFit`, `owner`, `Library` (Task 3).
- **Produces:**
  - **Profile:** `Cover` (HDMI 170; 0 means lists).
  - **Row drawing:** `row` and `(*App).drawRow` (thumbnail, marquee main line, dim sub line, star, right-aligned text); `coverCell`; `drawCoverCell`.
  - **Views:**
    - `cursor` (a `Grid` or a `List` per profile, same focus)
    - `albumsView`, `artistsView`, `songsView`: A opens or plays; X opens the item menu; `songsView` also handles Select.
  - **Play helpers:**
    - `(*App).openMenu(title, entries)`
    - `(*App).playSongs(songs, start, shuffle)`: replaces the queue and opens Now Playing
    - `(*App).withSongs(load songLoader, f)`
    - loaders: `albumSongs`, `playlistSongs`, `albumsSongs`, `artistSongs`
    - `sampleAlbums` (at most `maxShuffleAlbums` = 10)
  - **Menus:**
    - `queueEntries(name, load)` gives Play now · Shuffle · Play next · Add to queue.
    - `songMenu`, `albumMenu`, `artistMenu` each take `(a, item)`.
  - **`TabbedScreen`:** tabs over child screens, which are entered lazily. Up from a child's top goes to the tabs; Down or A comes back. `NewAlbumsScreen()` gives A–Z · By year (9999→0) · By genre.
  - **Artist screens:**
    - `ArtistsScreen`: `getArtists` once per connection, in `App.artists`. L/R jump by letter; the title reads "Artists · B".
    - `ArtistScreen`: an artist's albums; Select shuffles them.
  - **`GenresScreen`**, sorted by name case-insensitively. A opens `NewAlbumQueryScreen(genre, byGenre)`.
  - **`AlbumListScreen`:** now uses `albumsView` (a grid on HDMI) and takes any query (`NewAlbumQueryScreen`). Select shuffles a sample.
  - **`AlbumScreen`:** a Star/Unstar row after Play and Shuffle (`albumActionRows = 3`). X opens the focused track's menu, or the album's; starred tracks show a star.

- [ ] **Step 1: Write the failing tests**

`internal/ui/browse_test.go` (new file):

```go
package ui

import (
	"fmt"
	"slices"
	"testing"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func TestArtistsLetterJumpsAndCache(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240) // a list: one artist per row
	ta.Push(NewHomeScreen())
	s := NewArtistsScreen()
	ta.Push(s)
	ta.settle(t)
	if s.Title() != "Artists · B" {
		t.Fatalf("title %q", s.Title())
	}
	ta.press(input.BtnR)
	ta.press(input.BtnR)
	if s.view.cur.focus() != 2 || s.Title() != "Artists · А" {
		t.Fatalf("after R R: focus %d, title %q", s.view.cur.focus(), s.Title())
	}
	ta.press(input.BtnR) // no letter after А
	ta.press(input.BtnL) // at the start of А: the previous letter
	if s.view.cur.focus() != 1 {
		t.Fatalf("after L: focus %d", s.view.cur.focus())
	}
	ta.press(input.BtnA)
	ar, ok := ta.Top().(*ArtistScreen)
	if !ok || ar.artist.ID != "ar-3" {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.settle(t)
	if len(ar.view.albums) != 1 || ar.view.albums[0].ID != "al-3" {
		t.Fatalf("artist albums %v", ar.view.albums)
	}
	// Artists are fetched once per connection.
	ta.lib.artists = nil
	again := NewArtistsScreen()
	ta.Push(again)
	ta.settle(t)
	if len(again.view.artists) != 3 {
		t.Fatalf("second Artists screen has %d artists, want the cached 3", len(again.view.artists))
	}
}

func TestArtistSelectShufflesItsAlbums(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewArtistScreen(subsonic.Artist{ID: "ar-1", Name: "Аквариум"}))
	ta.settle(t)
	ta.press(input.BtnSelect)
	ta.settle(t)
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 3 {
		t.Fatalf("shuffle %v, played %d songs", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	if _, ok := ta.Top().(*NowPlayingScreen); !ok {
		t.Fatalf("top %T", ta.Top())
	}
}

func TestAlbumsTabs(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewAlbumsScreen()
	ta.Push(s)
	ta.settle(t)
	if q := ta.lib.calls[0]; q.Type != subsonic.ListAlphabetical {
		t.Fatalf("first tab query %+v", q)
	}
	ta.press(input.BtnUp) // from the grid's top row to the tabs
	ta.press(input.BtnRight)
	ta.settle(t)
	if q := ta.lib.calls[1]; q.Type != subsonic.ListByYear || q.FromYear != 9999 || q.ToYear != 0 {
		t.Fatalf("by-year query %+v", q)
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.settle(t)
	g := s.children[2].(*GenresScreen)
	if len(g.genres) != 2 || g.genres[0].Name != "Electronic" {
		t.Fatalf("genres %v (want sorted by name, case-insensitive)", g.genres)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // rock
	ta.settle(t)
	if q := ta.lib.calls[2]; q.Type != subsonic.ListByGenre || q.Genre != "rock" {
		t.Fatalf("genre query %+v", q)
	}
	if ta.Top().Title() != "rock" {
		t.Fatalf("top %q", ta.Top().Title())
	}
}

func TestAlbumMenuActions(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnX)
	m, ok := ta.Top().(*MenuScreen)
	if !ok {
		t.Fatalf("X opened %T", ta.Top())
	}
	var labels []string
	for _, e := range m.entries {
		labels = append(labels, e.label)
	}
	if !slices.Equal(labels, []string{"Play now", "Shuffle", "Play next", "Add to queue", "Star", "Go to artist"}) {
		t.Fatalf("album menu %v", labels)
	}
	for range 3 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Add to queue
	ta.settle(t)
	if !slices.Equal(ta.pl.calls, []string{"enqueue"}) || len(ta.pl.played) != 3 {
		t.Fatalf("calls %v, songs %d", ta.pl.calls, len(ta.pl.played))
	}
	if ta.toasts[0].text != "Added to queue: Радио Африка" {
		t.Fatalf("toast %q", ta.toasts[0].text)
	}
	ta.press(input.BtnX)
	for range 5 {
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnA) // Go to artist
	if ar, ok := ta.Top().(*ArtistScreen); !ok || ar.artist.ID != "ar-1" {
		t.Fatalf("Go to artist opened %T", ta.Top())
	}
}

func TestSongMenuFromAlbumAndStarRow(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	album := NewAlbumScreen(ta.lib.albums[0])
	ta.Push(album)
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // Star
	ta.press(input.BtnA)
	ta.settle(t)
	if !slices.Equal(ta.lib.stars, []string{"star al-1"}) {
		t.Fatalf("stars %v", ta.lib.stars)
	}
	ta.press(input.BtnDown) // first track
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if m.title != "Капитан Африка" || m.entries[len(m.entries)-2].label != "Go to album" {
		t.Fatalf("song menu %q %v", m.title, m.entries)
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Play next
	if !slices.Equal(ta.pl.calls, []string{"playnext"}) || ta.pl.played[0].ID != "s1" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestAlbumListSelectShufflesASample(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
	ta.settle(t)
	ta.press(input.BtnSelect)
	ta.settle(t)
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 3 {
		t.Fatalf("shuffle %v, %d songs (only al-1 has tracks)", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	many := make([]subsonic.Album, 40)
	for i := range many {
		many[i].ID = subsonic.ID(rune('a' + i))
	}
	if got := sampleAlbums(many); len(got) != maxShuffleAlbums {
		t.Fatalf("sample of %d", len(got))
	}
}

func TestGoldenBrowse(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewArtistsScreen())
		ta.settle(t)
		ta.press(input.BtnR)
		golden(t, "artists-"+p.Name, ta.settle(t))
		ta.Pop()
		ta.Push(NewAlbumsScreen())
		ta.settle(t)
		ta.press(input.BtnUp)
		golden(t, "albums-tabs-"+p.Name, ta.settle(t))
	}
}

// A big library: thousands of artists, letter jumps across all of them.
func TestArtistsLetterJumpInAHugeLibrary(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.artists = nil
	letters := "#ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, l := range letters {
		ix := subsonic.ArtistIndex{Name: string(l)}
		for i := range 200 {
			ix.Artists = append(ix.Artists, subsonic.Artist{ID: subsonic.ID(fmt.Sprint(string(l), i)), Name: fmt.Sprint(string(l), " artist ", i)})
		}
		ta.lib.artists = append(ta.lib.artists, ix)
	}
	ta.Push(NewHomeScreen())
	s := NewArtistsScreen()
	ta.Push(s)
	ta.settle(t)
	for range len(letters) + 3 {
		ta.press(input.BtnR)
	}
	if s.view.cur.focus() != 200*(len(letters)-1) || s.Title() != "Artists · Z" {
		t.Fatalf("focus %d title %q", s.view.cur.focus(), s.Title())
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnL) // inside Z: back to its start
	ta.press(input.BtnL) // then Y
	if s.Title() != "Artists · Y" || s.view.cur.focus() != 200*(len(letters)-2) {
		t.Fatalf("after L L: focus %d title %q", s.view.cur.focus(), s.Title())
	}
	ta.settle(t)
}

// Holding Down through a long list fires repeats every 45 ms; each page must
// be requested once, not once per repeat.
func TestHeldScrollLoadsEachPageOnce(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	for i := range 250 {
		ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: subsonic.ID(fmt.Sprint("x", i)), Name: "Filler"})
	}
	ta.Push(NewHomeScreen())
	s := NewAlbumListScreen("Recently added", subsonic.ListNewest)
	ta.Push(s)
	ta.settle(t)
	ta.onInput(input.Event{Button: input.BtnDown, Kind: input.Press})
	for i := range 300 {
		ta.dispatch(input.Event{Button: input.BtnDown, Kind: input.Repeat})
		if i%25 == 0 {
			ta.settle(t)
		}
	}
	ta.settle(t)
	var offsets []int
	for _, q := range ta.lib.calls {
		offsets = append(offsets, q.Offset)
	}
	if !slices.Equal(offsets, []int{0, 100, 200}) || len(s.view.albums) != 253 {
		t.Fatalf("page offsets %v, %d albums", offsets, len(s.view.albums))
	}
}

// Servers without OpenSubsonic, or items from playlists, can lack album and
// artist IDs: the menus must not offer to go where there is nothing.
func TestMenusWithoutIDs(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	for _, e := range songMenu(ta.App, subsonic.Song{ID: "s", Title: "Loose"}) {
		if e.label == "Go to album" || e.label == "Go to artist" {
			t.Fatalf("song menu offers %q without an ID", e.label)
		}
	}
	for _, e := range albumMenu(ta.App, subsonic.Album{ID: "a", Name: "Various"}) {
		if e.label == "Go to artist" {
			t.Fatal("album menu offers Go to artist without an artist ID")
		}
	}
}
```

Update the fakes (artists, genres, artist IDs) and Plan 2a's navigation tests. The album list is a grid on HDMI now, and album pages have a Star row:

Save this patch as `/tmp/t6-test.patch` and apply it from the repository root with `git apply /tmp/t6-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index 3fb58d5..e088a06 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -269,12 +269,18 @@ func (fakeArt) Get(k art.Key) (*gfx.Image, bool) {
 func sampleLibrary() *fakeLibrary {
 	l := &fakeLibrary{tracks: map[subsonic.ID][]subsonic.Song{}}
 	l.albums = []subsonic.Album{
-		{ID: "al-1", Name: "Радио Африка", Artist: "Аквариум", CoverArt: "al-1", Year: 1983},
-		{ID: "al-2", Name: "Homogenic", Artist: "Björk", CoverArt: "al-2", Year: 1997},
-		{ID: "al-3", Name: "A Rather Long Album Title That Will Need Truncating Somewhere", Artist: "Some Artist", CoverArt: "al-3", Year: 2001},
+		{ID: "al-1", Name: "Радио Африка", Artist: "Аквариум", ArtistID: "ar-1", CoverArt: "al-1", Year: 1983},
+		{ID: "al-2", Name: "Homogenic", Artist: "Björk", ArtistID: "ar-2", CoverArt: "al-2", Year: 1997},
+		{ID: "al-3", Name: "A Rather Long Album Title That Will Need Truncating Somewhere", Artist: "Some Artist", ArtistID: "ar-3", CoverArt: "al-3", Year: 2001},
 	}
+	l.artists = []subsonic.ArtistIndex{
+		{Name: "B", Artists: []subsonic.Artist{{ID: "ar-2", Name: "Björk", CoverArt: "ar-2", AlbumCount: 1}}},
+		{Name: "S", Artists: []subsonic.Artist{{ID: "ar-3", Name: "Some Artist", CoverArt: "ar-3", AlbumCount: 1}}},
+		{Name: "А", Artists: []subsonic.Artist{{ID: "ar-1", Name: "Аквариум", CoverArt: "ar-1", AlbumCount: 1, Starred: "2026-01-01T00:00:00Z"}}},
+	}
+	l.genres = []subsonic.Genre{{Name: "rock", AlbumCount: 2}, {Name: "Electronic", AlbumCount: 1}}
 	l.tracks["al-1"] = []subsonic.Song{
-		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
+		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", ArtistID: "ar-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
 		{ID: "s2", Title: "Время Луны", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 2, Duration: 160, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
 		{ID: "s3", Title: "Рок-н-ролл мёртв", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 3, Duration: 215, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
 	}
diff --git a/internal/ui/ui_test.go b/internal/ui/ui_test.go
index 956cbfa..4699e5c 100644
--- a/internal/ui/ui_test.go
+++ b/internal/ui/ui_test.go
@@ -122,19 +122,19 @@ func TestNavigationHomeToAlbumToPlay(t *testing.T) {
 		t.Fatalf("top = %T", ta.Top())
 	}
 	ta.settle(t)
-	ta.press(input.BtnDown)
-	ta.press(input.BtnA) // Homogenic
+	ta.press(input.BtnRight) // HDMI: a cover grid
+	ta.press(input.BtnA)     // Homogenic
 	as, ok := ta.Top().(*AlbumScreen)
 	if !ok || as.stub.ID != "al-2" {
 		t.Fatalf("top = %T %+v", ta.Top(), ta.Top())
 	}
 	ta.Pop()
-	ta.press(input.BtnUp)
+	ta.press(input.BtnLeft)
 	ta.press(input.BtnA) // Радио Африка
 	ta.settle(t)
-	ta.press(input.BtnDown)
-	ta.press(input.BtnDown)
-	ta.press(input.BtnDown) // row 3 = track 2
+	for range albumActionRows + 1 { // past Play, Shuffle, Star to track 2
+		ta.press(input.BtnDown)
+	}
 	ta.press(input.BtnA)
 	if ta.pl.start != 1 || len(ta.pl.played) != 3 {
 		t.Fatalf("played start %d of %d", ta.pl.start, len(ta.pl.played))
@@ -262,8 +262,8 @@ func TestLoadErrorShowsRetry(t *testing.T) {
 	ta.lib.err = nil
 	ta.press(input.BtnA)
 	ta.settle(t)
-	if len(s.albums) != 3 {
-		t.Fatalf("retry loaded %d albums", len(s.albums))
+	if len(s.view.albums) != 3 {
+		t.Fatalf("retry loaded %d albums", len(s.view.albums))
 	}
 }
 
@@ -288,15 +288,15 @@ func TestAlbumListPagesOnScroll(t *testing.T) {
 	ta.Push(NewAlbumListScreen("Recently added", subsonic.ListNewest))
 	ta.settle(t)
 	s := ta.Top().(*AlbumListScreen)
-	if len(s.albums) != albumPage {
-		t.Fatalf("first page %d", len(s.albums))
+	if len(s.view.albums) != albumPage {
+		t.Fatalf("first page %d", len(s.view.albums))
 	}
 	for i := 0; i < 20; i++ {
 		ta.press(input.BtnR) // page down
 	}
 	ta.settle(t)
-	if len(s.albums) != 153 {
-		t.Fatalf("after scrolling, %d albums loaded, want 153", len(s.albums))
+	if len(s.view.albums) != 153 {
+		t.Fatalf("after scrolling, %d albums loaded, want 153", len(s.view.albums))
 	}
 	if q := ta.lib.calls[len(ta.lib.calls)-1]; q.Offset != albumPage {
 		t.Fatalf("second page offset %d", q.Offset)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: NewArtistsScreen
undefined: ArtistScreen
undefined: NewArtistScreen
undefined: NewAlbumsScreen
undefined: sampleAlbums
undefined: maxShuffleAlbums
```

- [ ] **Step 3: Implement**

`internal/ui/views.go` (new file):

```go
package ui

import (
	"fmt"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// Views show a slice of library items and handle focus, A (open or play)
// and X (the item's menu). Screens own the loading. On HDMI albums and
// artists are cover grids; on a CRT (Profile.Cover == 0) they are lists
// with thumbnails.

// cursor is a Grid or a List, whichever the profile uses; both keep the
// same focus index.
type cursor struct {
	grid Grid
	list List
}

func (k *cursor) focus() int { return k.list.Focus }

func (k *cursor) setFocus(i int) { k.grid.Focus, k.list.Focus = i, i }

func (k *cursor) handle(a *App, e input.Event, n int) bool {
	var ok bool
	if a.P.Cover > 0 {
		ok = k.grid.Handle(e, n)
	} else {
		ok = k.list.Handle(e, n)
	}
	k.sync(a)
	return ok
}

func (k *cursor) sync(a *App) {
	if a.P.Cover > 0 {
		k.list.Focus = k.grid.Focus
	} else {
		k.grid.Focus = k.list.Focus
	}
}

func (k *cursor) nearEnd(a *App, n int) bool {
	if a.P.Cover > 0 {
		return k.grid.NearEnd(n)
	}
	return k.list.NearEnd(n)
}

// draw lays out n items as grid cells or list rows.
func (k *cursor) draw(a *App, c *gfx.Canvas, area gfx.Rect, n int, cell, rowFn func(i int, r gfx.Rect, focused bool)) {
	if a.P.Cover > 0 {
		w, h := a.coverCell()
		k.grid.Draw(c, gfx.R(area.X, area.Y+a.P.Margin/3, area.W, area.H-a.P.Margin/3), n, w, h, cell)
	} else {
		k.list.Draw(c, area, n, a.P.Row2H, rowFn)
	}
	k.sync(a) // Draw clamps the focus
}

type albumsView struct {
	albums []subsonic.Album
	cur    cursor
}

func (v *albumsView) focused(a *App) (subsonic.Album, bool) {
	if len(v.albums) == 0 {
		return subsonic.Album{}, false
	}
	return v.albums[min(v.cur.focus(), len(v.albums)-1)], true
}

func (v *albumsView) Handle(a *App, e input.Event) bool {
	if v.cur.handle(a, e, len(v.albums)) {
		return true
	}
	al, ok := v.focused(a)
	if e.Kind != input.Press || !ok {
		return false
	}
	switch e.Button {
	case input.BtnA:
		a.Push(NewAlbumScreen(al))
		return true
	case input.BtnX:
		a.openMenu(al.Name, albumMenu(a, al))
		return true
	}
	return false
}

func (v *albumsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.cur.draw(a, c, area, len(v.albums), func(i int, r gfx.Rect, focused bool) {
		al := v.albums[i]
		a.drawCoverCell(c, r, al.CoverArt, al.Name, al.Artist, focused, a.isStarred(albumStar(al)))
	}, func(i int, r gfx.Rect, focused bool) {
		al := v.albums[i]
		a.drawRow(c, r, row{cover: al.CoverArt, thumb: true, main: al.Name, sub: albumSub(al), focused: focused, starred: a.isStarred(albumStar(al))})
	})
}

// albumSub is "Artist · 1997".
func albumSub(al subsonic.Album) string {
	if al.Year > 0 {
		return fmt.Sprintf("%s · %d", al.Artist, al.Year)
	}
	return al.Artist
}

type artistsView struct {
	artists []subsonic.Artist
	cur     cursor
}

func (v *artistsView) Handle(a *App, e input.Event) bool {
	if v.cur.handle(a, e, len(v.artists)) {
		return true
	}
	if e.Kind != input.Press || len(v.artists) == 0 {
		return false
	}
	ar := v.artists[min(v.cur.focus(), len(v.artists)-1)]
	switch e.Button {
	case input.BtnA:
		a.Push(NewArtistScreen(ar))
		return true
	case input.BtnX:
		a.openMenu(ar.Name, artistMenu(a, ar))
		return true
	}
	return false
}

func (v *artistsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.cur.draw(a, c, area, len(v.artists), func(i int, r gfx.Rect, focused bool) {
		ar := v.artists[i]
		a.drawCoverCell(c, r, ar.CoverArt, ar.Name, albumCount(ar.AlbumCount), focused, a.isStarred(artistStar(ar)))
	}, func(i int, r gfx.Rect, focused bool) {
		ar := v.artists[i]
		a.drawRow(c, r, row{cover: ar.CoverArt, thumb: true, main: ar.Name, sub: albumCount(ar.AlbumCount), focused: focused, starred: a.isStarred(artistStar(ar))})
	})
}

func albumCount(n int) string {
	if n == 1 {
		return "1 album"
	}
	return fmt.Sprintf("%d albums", n)
}

// songsView is a track list: A plays the whole list from the focused song.
type songsView struct {
	songs []subsonic.Song
	list  List
}

func (v *songsView) Handle(a *App, e input.Event) bool {
	if v.list.Handle(e, len(v.songs)) {
		return true
	}
	if e.Kind != input.Press || len(v.songs) == 0 {
		return false
	}
	i := min(v.list.Focus, len(v.songs)-1)
	switch e.Button {
	case input.BtnA:
		a.playSongs(v.songs, i, false)
		return true
	case input.BtnX:
		a.openMenu(v.songs[i].Title, songMenu(a, v.songs[i]))
		return true
	case input.BtnSelect:
		a.playSongs(v.songs, 0, true)
		return true
	}
	return false
}

func (v *songsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	v.list.Draw(c, area, len(v.songs), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
		so := v.songs[i]
		sub := so.Artist
		if so.Album != "" {
			sub += " · " + so.Album
		}
		a.drawRow(c, r, row{cover: so.CoverArt, thumb: true, main: so.Title, sub: sub, focused: focused,
			starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))})
	})
}
```

`internal/ui/menus.go` (new file):

```go
package ui

import (
	"context"
	"math/rand/v2"

	"mistersubsonic/internal/subsonic"
)

// Item menus (X) and the play actions behind them. Entries run after the
// menu has closed, so a.Top() is the screen the menu was opened from; loads
// run under it.

// openMenu shows entries over the current screen.
func (a *App) openMenu(title string, entries []menuEntry) {
	a.Push(NewMenuScreen(a.Top(), title, entries))
}

// playSongs replaces the queue with songs, starting at start (or at a
// random song, shuffled), and opens Now Playing.
func (a *App) playSongs(songs []subsonic.Song, start int, shuffle bool) {
	if len(songs) == 0 || a.Player() == nil {
		return
	}
	a.Player().SetShuffle(shuffle)
	if shuffle {
		start = shuffleStart(len(songs))
	}
	a.Player().PlayNow(songs, start)
	a.Push(NewNowPlayingScreen())
}

// songLoader fetches the songs behind an item (an album, a playlist...).
type songLoader func(ctx context.Context, lib Library) ([]subsonic.Song, error)

// withSongs loads songs off the UI goroutine, then calls f with them; an
// error or an empty result is a toast instead.
func (a *App) withSongs(load songLoader, f func([]subsonic.Song)) {
	lib := a.Library()
	if lib == nil {
		return
	}
	a.Load(a.Top(), func(ctx context.Context) (any, error) { return load(ctx, lib) }, func(v any, err error) {
		songs, _ := v.([]subsonic.Song)
		switch {
		case err != nil:
			a.Toast("Couldn't load: %s", subsonic.Classify(err))
		case len(songs) == 0:
			a.Toast("Nothing to play")
		default:
			f(songs)
		}
	})
}

func albumSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		al, err := lib.GetAlbum(ctx, id)
		if err != nil {
			return nil, err
		}
		return al.Songs, nil
	}
}

func playlistSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		pl, err := lib.GetPlaylist(ctx, id)
		if err != nil {
			return nil, err
		}
		return pl.Songs, nil
	}
}

// maxShuffleAlbums caps how many albums a shuffle of a list or an artist
// loads (one getAlbum each).
const maxShuffleAlbums = 10

// albumsSongs loads the songs of albums, one request each. Albums that fail
// are skipped unless all do.
func albumsSongs(albums []subsonic.Album) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		var songs []subsonic.Song
		var firstErr error
		for _, al := range albums {
			got, err := albumSongs(al.ID)(ctx, lib)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				firstErr = cmpErr(firstErr, err)
				continue
			}
			songs = append(songs, got...)
		}
		if len(songs) == 0 {
			return nil, firstErr
		}
		return songs, nil
	}
}

func cmpErr(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

// sampleAlbums picks up to maxShuffleAlbums albums at random (Select on a
// long album list shuffles a sample, not the whole library).
func sampleAlbums(albums []subsonic.Album) []subsonic.Album {
	if len(albums) <= maxShuffleAlbums {
		return albums
	}
	out := make([]subsonic.Album, 0, maxShuffleAlbums)
	for _, i := range rand.Perm(len(albums))[:maxShuffleAlbums] {
		out = append(out, albums[i])
	}
	return out
}

func artistSongs(id subsonic.ID) songLoader {
	return func(ctx context.Context, lib Library) ([]subsonic.Song, error) {
		ar, err := lib.GetArtist(ctx, id)
		if err != nil {
			return nil, err
		}
		return albumsSongs(sampleAlbums(ar.Albums))(ctx, lib)
	}
}

// queueEntries are the play actions for a group of songs loaded on demand.
func queueEntries(name string, load songLoader) []menuEntry {
	return []menuEntry{
		{"Play now", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.playSongs(s, 0, false) })
		}},
		{"Shuffle", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.playSongs(s, 0, true) })
		}},
		{"Play next", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.Player().PlayNext(s); a.Toast("Playing next: %s", name) })
		}},
		{"Add to queue", func(a *App) {
			a.withSongs(load, func(s []subsonic.Song) { a.Player().Enqueue(s); a.Toast("Added to queue: %s", name) })
		}},
	}
}

func starEntry(a *App, it starItem) menuEntry {
	label := "Star"
	if a.isStarred(it) {
		label = "Unstar"
	}
	return menuEntry{label, func(a *App) { a.toggleStar(a.Top(), it) }}
}

// The menu builders take the App to label Star/Unstar.

func songMenu(a *App, so subsonic.Song) []menuEntry {
	one := []subsonic.Song{so}
	out := []menuEntry{
		{"Play now", func(a *App) { a.playSongs(one, 0, false) }},
		{"Play next", func(a *App) { a.Player().PlayNext(one); a.Toast("Playing next: %s", so.Title) }},
		{"Add to queue", func(a *App) { a.Player().Enqueue(one); a.Toast("Added to queue: %s", so.Title) }},
		starEntry(a, songStar(so)),
	}
	if so.AlbumID != "" {
		out = append(out, menuEntry{"Go to album", func(a *App) {
			a.Push(NewAlbumScreen(subsonic.Album{ID: so.AlbumID, Name: so.Album, Artist: so.Artist,
				ArtistID: so.ArtistID, CoverArt: so.CoverArt, Year: so.Year}))
		}})
	}
	if so.ArtistID != "" {
		out = append(out, goToArtist(so.ArtistID, so.Artist))
	}
	return out
}

func albumMenu(a *App, al subsonic.Album) []menuEntry {
	out := append(queueEntries(al.Name, albumSongs(al.ID)), starEntry(a, albumStar(al)))
	if al.ArtistID != "" {
		out = append(out, goToArtist(al.ArtistID, al.Artist))
	}
	return out
}

func artistMenu(a *App, ar subsonic.Artist) []menuEntry {
	return []menuEntry{
		{"Shuffle", func(a *App) {
			a.withSongs(artistSongs(ar.ID), func(s []subsonic.Song) { a.playSongs(s, 0, true) })
		}},
		starEntry(a, artistStar(ar)),
		goToArtist(ar.ID, ar.Name),
	}
}

func goToArtist(id subsonic.ID, name string) menuEntry {
	return menuEntry{"Go to artist", func(a *App) { a.Push(NewArtistScreen(subsonic.Artist{ID: id, Name: name})) }}
}
```

`internal/ui/tabbed.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// TabbedScreen shows one of several child screens under a row of tabs
// (Albums: A–Z / by year / by genre; Starred). Up from the top of a child
// moves the focus to the tabs, Down or A goes back. A child is entered
// (starts loading) the first time its tab is shown.
type TabbedScreen struct {
	title    string
	tabs     Tabs
	children []Screen
	entered  []bool
	onTabs   bool
}

func newTabbed(title string, labels []string, children ...Screen) *TabbedScreen {
	return &TabbedScreen{title: title, tabs: Tabs{Labels: labels}, children: children, entered: make([]bool, len(children))}
}

// NewAlbumsScreen is the Albums section: A–Z, newest year first, by genre.
func NewAlbumsScreen() *TabbedScreen {
	return newTabbed("Albums", []string{"A–Z", "By year", "By genre"},
		NewAlbumListScreen("Albums A–Z", subsonic.ListAlphabetical),
		NewAlbumQueryScreen("Albums by year", subsonic.AlbumListQuery{Type: subsonic.ListByYear, FromYear: 9999, ToYear: 0}),
		NewGenresScreen())
}

func (s *TabbedScreen) Title() string { return s.title }

func (s *TabbedScreen) Owns(x Screen) bool {
	for _, c := range s.children {
		if c == x {
			return true
		}
		if o, ok := c.(owner); ok && o.Owns(x) {
			return true
		}
	}
	return false
}

func (s *TabbedScreen) Enter(a *App) { s.enter(a) }

func (s *TabbedScreen) enter(a *App) {
	if !s.entered[s.tabs.Sel] {
		s.entered[s.tabs.Sel] = true
		s.children[s.tabs.Sel].Enter(a)
	}
}

func (s *TabbedScreen) Handle(a *App, e input.Event) bool {
	if s.onTabs {
		if s.tabs.Handle(e) {
			s.enter(a)
			return true
		}
		if (e.Button == input.BtnDown && e.Kind != input.Release) || (e.Button == input.BtnA && e.Kind == input.Press) {
			s.onTabs = false
			return true
		}
		return false
	}
	if s.children[s.tabs.Sel].Handle(a, e) {
		return true
	}
	if e.Button == input.BtnUp && e.Kind != input.Release {
		s.onTabs = true
		return true
	}
	return false
}

func (s *TabbedScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	h := a.P.RowH
	s.tabs.Draw(a, c, gfx.R(area.X, area.Y, area.W, h), s.onTabs)
	s.children[s.tabs.Sel].Draw(a, c, gfx.R(area.X, area.Y+h, area.W, area.H-h))
}
```

`internal/ui/screens_artists.go` (new file):

```go
package ui

import (
	"context"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// ArtistsScreen lists every artist A–Z (getArtists, fetched once per
// connection and kept); L/R jump to the previous or next letter.
type ArtistsScreen struct {
	view    artistsView
	letters []letterStart
	loaded  bool
	err     error
}

// letterStart is where one index letter's artists begin in the flat list.
type letterStart struct {
	name string
	at   int
}

func NewArtistsScreen() *ArtistsScreen { return &ArtistsScreen{} }

func (s *ArtistsScreen) Title() string {
	if l, ok := s.letter(); ok {
		return "Artists · " + l.name
	}
	return "Artists"
}

// letter is the index letter of the focused artist.
func (s *ArtistsScreen) letter() (letterStart, bool) {
	var cur letterStart
	found := false
	for _, l := range s.letters {
		if l.at <= s.view.cur.focus() {
			cur, found = l, true
		}
	}
	return cur, found
}

func (s *ArtistsScreen) Enter(a *App) {
	if a.artists != nil {
		s.set(a.artists)
		return
	}
	s.load(a)
}

func (s *ArtistsScreen) load(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtists(ctx) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		idx, _ := v.([]subsonic.ArtistIndex)
		a.artists = idx
		s.set(idx)
	})
}

func (s *ArtistsScreen) set(idx []subsonic.ArtistIndex) {
	s.loaded = true
	s.view.artists, s.letters = nil, nil
	for _, ix := range idx {
		if len(ix.Artists) == 0 {
			continue
		}
		s.letters = append(s.letters, letterStart{ix.Name, len(s.view.artists)})
		s.view.artists = append(s.view.artists, ix.Artists...)
	}
}

func (s *ArtistsScreen) Handle(a *App, e input.Event) bool {
	if (e.Button == input.BtnL || e.Button == input.BtnR) && e.Kind != input.Release && len(s.letters) > 0 {
		cur, _ := s.letter()
		target := cur.at // L: the start of this letter, or of the previous one
		for i, l := range s.letters {
			if l.name != cur.name {
				continue
			}
			switch {
			case e.Button == input.BtnR && i+1 < len(s.letters):
				target = s.letters[i+1].at
			case e.Button == input.BtnL && s.view.cur.focus() == cur.at && i > 0:
				target = s.letters[i-1].at
			}
		}
		s.view.cur.setFocus(target)
		return true
	}
	if s.view.Handle(a, e) {
		return true
	}
	if e.Kind == input.Press && e.Button == input.BtnA && s.err != nil {
		s.load(a)
		return true
	}
	return false
}

func (s *ArtistsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load artists: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.view.artists) == 0:
		a.drawCentered(c, area, "No artists", colDim)
	default:
		s.view.Draw(a, c, area)
	}
}

// ArtistScreen shows an artist's albums; Select shuffles them.
type ArtistScreen struct {
	artist subsonic.Artist
	view   albumsView
	loaded bool
	err    error
}

func NewArtistScreen(ar subsonic.Artist) *ArtistScreen { return &ArtistScreen{artist: ar} }

func (s *ArtistScreen) Title() string { return s.artist.Name }

func (s *ArtistScreen) Enter(a *App) { s.load(a) }

func (s *ArtistScreen) load(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetArtist(ctx, s.artist.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		ar := v.(*subsonic.ArtistWithAlbums)
		s.artist, s.view.albums, s.loaded = ar.Artist, ar.Albums, true
	})
}

func (s *ArtistScreen) Handle(a *App, e input.Event) bool {
	if s.view.Handle(a, e) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch {
	case e.Button == input.BtnA && s.err != nil:
		s.load(a)
		return true
	case e.Button == input.BtnSelect && len(s.view.albums) > 0:
		a.withSongs(albumsSongs(sampleAlbums(s.view.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
		return true
	}
	return false
}

func (s *ArtistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load artist: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
	case len(s.view.albums) == 0:
		a.drawCentered(c, area, "No albums", colDim)
	default:
		s.view.Draw(a, c, area)
	}
}
```

Then the rows, the profile, the album list and genres, and the album page:

Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/internal/ui/app.go b/internal/ui/app.go
index d76b0d4..0ba8161 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -148,7 +148,8 @@ type App struct {
 	// their releases are dropped too.
 	swallowed map[input.Button]bool
 	insecure  bool
-	stars     map[subsonic.ID]bool // star changes made in this session
+	stars     map[subsonic.ID]bool   // star changes made in this session
+	artists   []subsonic.ArtistIndex // getArtists, fetched once per connection
 	rep       input.Repeater
 	in        chan input.Event
 	post      chan func()
diff --git a/internal/ui/rows.go b/internal/ui/rows.go
index 1e3ba96..74cb3b0 100644
--- a/internal/ui/rows.go
+++ b/internal/ui/rows.go
@@ -8,23 +8,85 @@ import (
 // drawTextRow draws a thumbnail (if id != "" and the profile has thumbs),
 // a main line and an optional dim second line into r.
 func (a *App) drawTextRow(c *gfx.Canvas, r gfx.Rect, coverID subsonic.ID, withThumb bool, main, sub string, mainCol gfx.Color) {
+	a.drawRow(c, r, row{cover: coverID, thumb: withThumb, main: main, sub: sub, col: mainCol})
+}
+
+// row is one list row: an optional thumbnail, a main line (scrolling when
+// focused and too long), an optional dim second line, and on the right an
+// optional star and dim text (a duration).
+type row struct {
+	cover     subsonic.ID
+	thumb     bool
+	main, sub string
+	col       gfx.Color // main line; 0 = colText
+	focused   bool
+	starred   bool
+	right     string
+}
+
+func (a *App) drawRow(c *gfx.Canvas, r gfx.Rect, w row) {
 	p := a.P
+	if w.col == 0 {
+		w.col = colText
+	}
 	x := r.X + p.Margin
-	if withThumb && p.Thumb > 0 {
+	if w.thumb && p.Thumb > 0 {
 		t := min(p.Thumb, r.H-2)
-		a.drawArt(c, coverID, gfx.R(x, r.Y+(r.H-t)/2, t, t))
+		a.drawArt(c, w.cover, gfx.R(x, r.Y+(r.H-t)/2, t, t))
 		x += t + p.Margin/2
 	}
-	w := r.Right() - p.Margin - x
+	right := r.Right() - p.Margin
 	fb, fs := a.F.Body, a.F.Small
-	if sub == "" {
-		fb.Draw(c, x, r.Y+(r.H+fb.Ascent()-fb.Descent())/2, fb.Truncate(main, w), mainCol, r)
+	mid := r.Y + (r.H+fb.Ascent()-fb.Descent())/2
+	if w.right != "" {
+		rw := fb.Measure(w.right)
+		fb.Draw(c, right-rw, mid, w.right, colDim, r)
+		right -= rw + p.Margin/2
+	}
+	if w.starred {
+		s := fb.Ascent() * 3 / 4
+		drawIcon(c, iconStar, gfx.R(right-s, mid-fb.Ascent()/2-s/2, s, s), colAccent)
+		right -= s + p.Margin/2
+	}
+	width := right - x
+	if w.sub == "" {
+		a.drawFit(c, fb, x, mid, width, w.main, w.col, r, w.focused)
 		return
 	}
 	total := fb.Height() + fs.Height()
 	y := r.Y + (r.H-total)/2
-	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(main, w), mainCol, r)
-	fs.Draw(c, x, y+fb.Height()+fs.Ascent(), fs.Truncate(sub, w), colDim, r)
+	a.drawFit(c, fb, x, y+fb.Ascent(), width, w.main, w.col, r, w.focused)
+	fs.Draw(c, x, y+fb.Height()+fs.Ascent(), fs.Truncate(w.sub, width), colDim, r)
+}
+
+// coverCell is the size of one cell in a cover grid.
+func (a *App) coverCell() (w, h int) {
+	p := a.P
+	pad := p.Margin / 3
+	return p.Cover + 2*pad, pad + p.Cover + pad/2 + 2*a.F.Small.Height() + pad
+}
+
+// drawCoverCell draws a grid cell: the cover, a title (scrolling when
+// focused) and a dim second line.
+func (a *App) drawCoverCell(c *gfx.Canvas, r gfx.Rect, cover subsonic.ID, title, sub string, focused, starred bool) {
+	p := a.P
+	pad := p.Margin / 3
+	if focused {
+		c.Fill(r, colFocus)
+	}
+	art := p.Cover
+	x := r.X + (r.W-art)/2
+	a.drawArt(c, cover, gfx.R(x, r.Y+pad, art, art))
+	if starred {
+		s := art / 7
+		badge := gfx.R(x+art-s-pad, r.Y+2*pad, s+pad/2, s+pad/2)
+		c.Fill(badge, colOverlay)
+		drawIcon(c, iconStar, badge.Inset(pad/4), colAccent)
+	}
+	fs := a.F.Small
+	y := r.Y + pad + art + pad/2 + fs.Ascent()
+	a.drawFit(c, fs, x, y, art, title, colText, r, focused)
+	fs.Draw(c, x, y+fs.Height(), fs.Truncate(sub, art), colDim, r)
 }
 
 // drawCentered draws a dim message in the middle of area (loading, empty, error).
diff --git a/internal/ui/screens_album.go b/internal/ui/screens_album.go
index 1ec6b63..3dee638 100644
--- a/internal/ui/screens_album.go
+++ b/internal/ui/screens_album.go
@@ -10,7 +10,8 @@ import (
 	"mistersubsonic/internal/subsonic"
 )
 
-// AlbumScreen shows an album header and its tracks, with Play and Shuffle rows.
+// AlbumScreen shows an album header and its tracks, with Play, Shuffle and
+// Star rows. X opens the menu of the focused track, or of the album.
 type AlbumScreen struct {
 	stub  subsonic.Album // from the list, shown while loading
 	album *subsonic.AlbumWithSongs
@@ -18,7 +19,7 @@ type AlbumScreen struct {
 	err   error
 }
 
-const albumActionRows = 2 // Play, Shuffle
+const albumActionRows = 3 // Play, Shuffle, Star
 
 func NewAlbumScreen(a subsonic.Album) *AlbumScreen { return &AlbumScreen{stub: a} }
 
@@ -38,6 +39,14 @@ func (s *AlbumScreen) load(a *App) {
 	})
 }
 
+// info is the loaded album, or the stub from the list until then.
+func (s *AlbumScreen) info() subsonic.Album {
+	if s.album != nil {
+		return s.album.Album
+	}
+	return s.stub
+}
+
 func (s *AlbumScreen) songs() []subsonic.Song {
 	if s.album == nil {
 		return nil
@@ -46,16 +55,7 @@ func (s *AlbumScreen) songs() []subsonic.Song {
 }
 
 func (s *AlbumScreen) play(a *App, start int, shuffle bool) {
-	songs := s.songs()
-	if len(songs) == 0 {
-		return
-	}
-	a.Player().SetShuffle(shuffle)
-	if shuffle {
-		start = shuffleStart(len(songs))
-	}
-	a.Player().PlayNow(songs, start)
-	a.Push(NewNowPlayingScreen())
+	a.playSongs(s.songs(), start, shuffle)
 }
 
 func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
@@ -79,10 +79,20 @@ func (s *AlbumScreen) Handle(a *App, e input.Event) bool {
 			s.play(a, 0, false)
 		case s.list.Focus == 1:
 			s.play(a, 0, true)
+		case s.list.Focus == 2:
+			a.toggleStar(s, albumStar(s.info()))
 		default:
 			s.play(a, s.list.Focus-albumActionRows, false)
 		}
 		return true
+	case input.BtnX:
+		if i := s.list.Focus - albumActionRows; s.album != nil && i >= 0 && i < len(s.songs()) {
+			so := s.songs()[i]
+			a.openMenu(so.Title, songMenu(a, so))
+		} else {
+			a.openMenu(s.info().Name, albumMenu(a, s.info()))
+		}
+		return true
 	case input.BtnSelect:
 		s.play(a, 0, true)
 		return true
@@ -140,15 +150,21 @@ func (s *AlbumScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			iconText(c, f, iconPlay, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, "Play", colAccent, r)
 		case 1:
 			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
+		case 2:
+			label := "Star"
+			if a.isStarred(albumStar(s.info())) {
+				label = "Unstar"
+			}
+			a.drawTextRow(c, r, "", false, label, "", colAccent)
 		default:
 			so := songs[i-albumActionRows]
-			a.drawTrackRow(c, r, so, false)
+			a.drawTrackRow(c, r, so, false, focused)
 		}
 	})
 }
 
-// drawTrackRow: "03  Title ...  3:45".
-func (a *App) drawTrackRow(c *gfx.Canvas, r gfx.Rect, so subsonic.Song, current bool) {
+// drawTrackRow: "03  Title ...  ★ 3:45".
+func (a *App) drawTrackRow(c *gfx.Canvas, r gfx.Rect, so subsonic.Song, current, focused bool) {
 	p := a.P
 	f := a.F.Body
 	col := colText
@@ -166,5 +182,11 @@ func (a *App) drawTrackRow(c *gfx.Canvas, r gfx.Rect, so subsonic.Song, current
 	dur := clock(secs(so.Duration))
 	dw := f.Measure(dur)
 	f.Draw(c, r.Right()-p.Margin-dw, y, dur, colDim, r)
-	f.Draw(c, x, y, f.Truncate(so.Title, r.Right()-p.Margin-dw-p.Margin/2-x), col, r)
+	right := r.Right() - p.Margin - dw - p.Margin/2
+	if a.isStarred(songStar(so)) {
+		s := f.Ascent() * 3 / 4
+		drawIcon(c, iconStar, gfx.R(right-s, y-f.Ascent()/2-s/2, s, s), colAccent)
+		right -= s + p.Margin/2
+	}
+	a.drawFit(c, f, x, y, right-x, so.Title, col, r, focused)
 }
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index 3c624e9..e9790c8 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -4,6 +4,8 @@ import (
 	"context"
 	"fmt"
 	"math/rand/v2"
+	"slices"
+	"strings"
 
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
@@ -85,17 +87,22 @@ func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 
 // AlbumListScreen pages through getAlbumList2 results.
 type AlbumListScreen struct {
-	title, listType string
-	list            List
-	albums          []subsonic.Album
-	loading, more   bool
-	err             error
+	title         string
+	q             subsonic.AlbumListQuery
+	view          albumsView
+	loading, more bool
+	err           error
 }
 
 const albumPage = 100
 
 func NewAlbumListScreen(title, listType string) *AlbumListScreen {
-	return &AlbumListScreen{title: title, listType: listType, more: true}
+	return NewAlbumQueryScreen(title, subsonic.AlbumListQuery{Type: listType})
+}
+
+// NewAlbumQueryScreen lists albums for any getAlbumList2 query (by year, by genre).
+func NewAlbumQueryScreen(title string, q subsonic.AlbumListQuery) *AlbumListScreen {
+	return &AlbumListScreen{title: title, q: q, more: true}
 }
 
 func (s *AlbumListScreen) Title() string { return s.title }
@@ -107,9 +114,10 @@ func (s *AlbumListScreen) loadMore(a *App) {
 		return
 	}
 	s.loading, s.err = true, nil
-	offset := len(s.albums)
+	q := s.q
+	q.Size, q.Offset = albumPage, len(s.view.albums)
 	a.Load(s, func(ctx context.Context) (any, error) {
-		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: s.listType, Size: albumPage, Offset: offset})
+		return a.Library().GetAlbumList2(ctx, q)
 	}, func(v any, err error) {
 		s.loading = false
 		if err != nil {
@@ -117,15 +125,15 @@ func (s *AlbumListScreen) loadMore(a *App) {
 			return
 		}
 		page, _ := v.([]subsonic.Album)
-		s.albums = append(s.albums, page...)
+		s.view.albums = append(s.view.albums, page...)
 		// The random list has no end; one page is plenty.
-		s.more = len(page) == albumPage && s.listType != subsonic.ListRandom
+		s.more = len(page) == albumPage && s.q.Type != subsonic.ListRandom
 	})
 }
 
 func (s *AlbumListScreen) Handle(a *App, e input.Event) bool {
-	if s.list.Handle(e, len(s.albums)) {
-		if s.list.NearEnd(len(s.albums)) {
+	if s.view.Handle(a, e) {
+		if s.view.cur.nearEnd(a, len(s.view.albums)) {
 			s.loadMore(a)
 		}
 		return true
@@ -135,41 +143,93 @@ func (s *AlbumListScreen) Handle(a *App, e input.Event) bool {
 	}
 	switch e.Button {
 	case input.BtnA:
-		if s.err != nil && len(s.albums) == 0 {
+		if s.err != nil && len(s.view.albums) == 0 {
 			s.more = true
 			s.loadMore(a)
 			return true
 		}
-		if len(s.albums) > 0 {
-			a.Push(NewAlbumScreen(s.albums[s.list.Focus]))
+	case input.BtnSelect:
+		if len(s.view.albums) > 0 {
+			a.withSongs(albumsSongs(sampleAlbums(s.view.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
+			return true
 		}
-		return true
 	}
 	return false
 }
 
 func (s *AlbumListScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	switch {
-	case len(s.albums) == 0 && s.loading:
+	case len(s.view.albums) == 0 && s.loading:
 		a.drawCentered(c, area, "Loading…", colDim)
-		return
-	case len(s.albums) == 0 && s.err != nil:
+	case len(s.view.albums) == 0 && s.err != nil:
 		a.drawCentered(c, area, "Couldn't load: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
-		return
-	case len(s.albums) == 0:
+	case len(s.view.albums) == 0:
 		a.drawCentered(c, area, "No albums", colDim)
-		return
+	default:
+		s.view.Draw(a, c, area)
 	}
-	s.list.Draw(c, area, len(s.albums), a.P.Row2H, func(i int, r gfx.Rect, focused bool) {
-		al := s.albums[i]
-		sub := al.Artist
-		if al.Year > 0 {
-			sub += fmt.Sprintf(" · %d", al.Year)
+}
+
+// GenresScreen lists genres by name; A opens a genre's albums.
+type GenresScreen struct {
+	genres []subsonic.Genre
+	list   List
+	loaded bool
+	err    error
+}
+
+func NewGenresScreen() *GenresScreen { return &GenresScreen{} }
+
+func (s *GenresScreen) Title() string { return "Genres" }
+
+func (s *GenresScreen) Enter(a *App) {
+	s.err = nil
+	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetGenres(ctx) }, func(v any, err error) {
+		if err != nil {
+			s.err = err
+			return
 		}
-		a.drawTextRow(c, r, al.CoverArt, true, al.Name, sub, colText)
+		g, _ := v.([]subsonic.Genre)
+		slices.SortFunc(g, func(x, y subsonic.Genre) int {
+			return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
+		})
+		s.genres, s.loaded = g, true
 	})
 }
 
+func (s *GenresScreen) Handle(a *App, e input.Event) bool {
+	if s.list.Handle(e, len(s.genres)) {
+		return true
+	}
+	if e.Kind != input.Press || e.Button != input.BtnA {
+		return false
+	}
+	switch {
+	case s.err != nil:
+		s.Enter(a)
+	case len(s.genres) > 0:
+		g := s.genres[s.list.Focus]
+		a.Push(NewAlbumQueryScreen(g.Name, subsonic.AlbumListQuery{Type: subsonic.ListByGenre, Genre: g.Name}))
+	}
+	return true
+}
+
+func (s *GenresScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
+	switch {
+	case s.err != nil:
+		a.drawCentered(c, area, "Couldn't load genres: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
+	case !s.loaded:
+		a.drawCentered(c, area, "Loading…", colDim)
+	case len(s.genres) == 0:
+		a.drawCentered(c, area, "No genres", colDim)
+	default:
+		s.list.Draw(c, area, len(s.genres), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
+			g := s.genres[i]
+			a.drawRow(c, r, row{main: g.Name, focused: focused, right: albumCount(g.AlbumCount)})
+		})
+	}
+}
+
 // shuffleStart picks a random first track for shuffle play.
 func shuffleStart(n int) int {
 	if n <= 1 {
diff --git a/internal/ui/theme.go b/internal/ui/theme.go
index cf39558..4b5eeb4 100644
--- a/internal/ui/theme.go
+++ b/internal/ui/theme.go
@@ -27,11 +27,12 @@ type Profile struct {
 	// hides (title-safe, spec §8.2); panels still run to the edge.
 	SafeY        int
 	MarqueeSpeed int // px per second
+	Cover        int // cover size in grids; 0 = lists with thumbnails instead (CRT)
 }
 
 var (
 	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
-		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60}
+		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60, Cover: 170}
 	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 16, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
 		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24, SafeY: 12, MarqueeSpeed: 24}
 )
```

- [ ] **Step 4: Regenerate the screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`. Changed or new goldens: `album-crt`, `album-hdmi`, `albums-hdmi`, `albums-tabs-crt`, `albums-tabs-hdmi`, `artists-crt`, `artists-hdmi`, `menu-crt`, `menu-hdmi`.
- **`albums-hdmi`:** a grid of three covers with name and artist under each; the first cell is highlighted.
- **`artists-*`:** Björk, Some Artist and Аквариум, with a star on Аквариум. The title is "Artists · S" after R.
- **`albums-tabs-*`:** "A–Z · By year · By genre", with A–Z underlined and highlighted.
- **`album-*`:** the album page gains a "Star" row.
- **`menu-*`:** these change only because the album page behind the menu gained that row.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/browse_test.go internal/ui/fakes_test.go internal/ui/menus.go internal/ui/rows.go internal/ui/screens_album.go internal/ui/screens_artists.go internal/ui/screens_home.go internal/ui/tabbed.go internal/ui/theme.go internal/ui/ui_test.go internal/ui/views.go internal/ui/testdata/golden
git commit -m "ui: artists, albums tabs and genres, cover grids, item menus, Select to shuffle" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 7: Playlists and Starred

**Files:**
- Create: `internal/ui/screens_playlists.go`, `internal/ui/screens_starred.go`
- Test: `internal/ui/lists_test.go` (new), `internal/ui/fakes_test.go` (modified: playlists, starred, a call counter)

**Interfaces:**
- **Consumes:** `cursor`, the views, `queueEntries`, `playlistSongs`, `songMenu`, `newTabbed` (Task 6).
- **Produces:**
  - `PlaylistsScreen` (covers on HDMI, "N tracks · 48:10 · by owner" on CRT). X gives the playlist's play actions.
  - `PlaylistScreen`: Play and Shuffle rows, then tracks. X opens a track's menu; Select shuffles.
  - `NewStarredScreen()`: a `TabbedScreen` with Albums · Artists · Tracks over one shared `getStarred2` call (`starredData`). A retries after an error.
  - `trackCount`, `playlistSummary`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/lists_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"

	"mistersubsonic/internal/input"
)

func TestPlaylistsOpenAndPlay(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	ta.Push(NewPlaylistsScreen())
	ta.settle(t)
	ta.press(input.BtnX)
	m := ta.Top().(*MenuScreen)
	if m.title != "Дорога домой" || len(m.entries) != 4 {
		t.Fatalf("playlist menu %q %v", m.title, m.entries)
	}
	ta.press(input.BtnB)
	ta.press(input.BtnA)
	pl, ok := ta.Top().(*PlaylistScreen)
	if !ok {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.settle(t)
	ta.press(input.BtnDown)
	ta.press(input.BtnA) // Shuffle
	if !ta.pl.st.Shuffle || len(ta.pl.played) != 2 {
		t.Fatalf("shuffle %v, %d songs", ta.pl.st.Shuffle, len(ta.pl.played))
	}
	ta.Pop()
	if ta.Top() != pl {
		t.Fatal("B from Now Playing did not return to the playlist")
	}
	ta.press(input.BtnDown)
	ta.press(input.BtnDown) // second track
	ta.press(input.BtnA)
	if ta.pl.start != 1 || ta.pl.played[1].ID != "s2" {
		t.Fatalf("start %d, played %v", ta.pl.start, ta.pl.played)
	}
}

func TestStarredTabsShareOneCall(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewStarredScreen()
	ta.Push(s)
	ta.settle(t)
	ta.press(input.BtnUp)
	ta.press(input.BtnRight)
	ta.settle(t)
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.settle(t)
	if ta.lib.starCalls != 1 {
		t.Fatalf("getStarred2 called %d times, want 1", ta.lib.starCalls)
	}
	ta.press(input.BtnA) // play the starred tracks
	if !slices.Equal(ta.pl.calls, []string{"playnow"}) || ta.pl.played[0].ID != "s1" {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestStarredErrorRetries(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.lib.err = errOffline
	ta.Push(NewHomeScreen())
	ta.Push(NewStarredScreen())
	ta.settle(t)
	ta.lib.err = nil
	ta.press(input.BtnA)
	ta.settle(t)
	if ta.lib.starCalls != 2 {
		t.Fatalf("calls %d, want a retry", ta.lib.starCalls)
	}
}

func TestGoldenPlaylistsAndStarred(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		ta.Push(NewPlaylistsScreen())
		ta.settle(t)
		golden(t, "playlists-"+p.Name, ta.settle(t))
		ta.press(input.BtnA)
		ta.settle(t)
		golden(t, "playlist-"+p.Name, ta.settle(t))
		ta.Replace(NewHomeScreen())
		ta.Push(NewStarredScreen())
		ta.settle(t)
		ta.press(input.BtnUp)
		ta.press(input.BtnRight)
		ta.press(input.BtnRight)
		ta.press(input.BtnDown)
		golden(t, "starred-tracks-"+p.Name, ta.settle(t))
	}
}
```

Save this patch as `/tmp/t7-test.patch` and apply it from the repository root with `git apply /tmp/t7-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index e088a06..cd0eaf1 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -30,6 +30,7 @@ type fakeLibrary struct {
 	searches  []string
 	stars     []string      // "star al-1", "unstar s2"
 	block     chan struct{} // if set, Search3 waits for it (or ctx)
+	starCalls int           // GetStarred2 calls
 }
 
 func (l *fakeLibrary) GetAlbumList2(_ context.Context, q subsonic.AlbumListQuery) ([]subsonic.Album, error) {
@@ -117,6 +118,7 @@ func (l *fakeLibrary) GetPlaylist(_ context.Context, id subsonic.ID) (*subsonic.
 func (l *fakeLibrary) GetStarred2(context.Context) (*subsonic.Starred, error) {
 	l.mu.Lock()
 	defer l.mu.Unlock()
+	l.starCalls++
 	if l.err != nil {
 		return nil, l.err
 	}
@@ -279,11 +281,17 @@ func sampleLibrary() *fakeLibrary {
 		{Name: "А", Artists: []subsonic.Artist{{ID: "ar-1", Name: "Аквариум", CoverArt: "ar-1", AlbumCount: 1, Starred: "2026-01-01T00:00:00Z"}}},
 	}
 	l.genres = []subsonic.Genre{{Name: "rock", AlbumCount: 2}, {Name: "Electronic", AlbumCount: 1}}
+	l.playlists = []subsonic.Playlist{
+		{ID: "pl-1", Name: "Дорога домой", Owner: "alice", SongCount: 2, Duration: 375, CoverArt: "pl-1"},
+		{ID: "pl-2", Name: "Empty", Owner: "alice", CoverArt: "pl-2"},
+	}
 	l.tracks["al-1"] = []subsonic.Song{
 		{ID: "s1", Title: "Капитан Африка", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", ArtistID: "ar-1", CoverArt: "al-1", Track: 1, Duration: 240, Suffix: "flac", BitDepth: 24, SamplingRate: 96000},
 		{ID: "s2", Title: "Время Луны", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 2, Duration: 160, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
 		{ID: "s3", Title: "Рок-н-ролл мёртв", Artist: "Аквариум", Album: "Радио Африка", AlbumID: "al-1", CoverArt: "al-1", Track: 3, Duration: 215, Suffix: "flac", BitDepth: 16, SamplingRate: 44100},
 	}
+	l.plSongs = map[subsonic.ID][]subsonic.Song{"pl-1": {l.tracks["al-1"][2], l.tracks["al-1"][1]}}
+	l.starred = subsonic.Starred{Albums: l.albums[1:2], Artists: l.artists[2].Artists, Songs: l.tracks["al-1"][:1]}
 	return l
 }
 
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: NewPlaylistsScreen
undefined: PlaylistScreen
undefined: NewStarredScreen
```

- [ ] **Step 3: Implement**

`internal/ui/screens_playlists.go` (new file):

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

// PlaylistsScreen lists the user's playlists (covers on HDMI).
type PlaylistsScreen struct {
	playlists []subsonic.Playlist
	cur       cursor
	loaded    bool
	err       error
}

func NewPlaylistsScreen() *PlaylistsScreen { return &PlaylistsScreen{} }

func (s *PlaylistsScreen) Title() string { return "Playlists" }

func (s *PlaylistsScreen) Enter(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetPlaylists(ctx) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.playlists, _ = v.([]subsonic.Playlist)
		s.loaded = true
	})
}

func (s *PlaylistsScreen) Handle(a *App, e input.Event) bool {
	if s.cur.handle(a, e, len(s.playlists)) {
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	if e.Button == input.BtnA && s.err != nil {
		s.Enter(a)
		return true
	}
	if len(s.playlists) == 0 {
		return false
	}
	pl := s.playlists[min(s.cur.focus(), len(s.playlists)-1)]
	switch e.Button {
	case input.BtnA:
		a.Push(NewPlaylistScreen(pl))
		return true
	case input.BtnX:
		a.openMenu(pl.Name, queueEntries(pl.Name, playlistSongs(pl.ID)))
		return true
	}
	return false
}

func (s *PlaylistsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	switch {
	case s.err != nil:
		a.drawCentered(c, area, "Couldn't load playlists: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case !s.loaded:
		a.drawCentered(c, area, "Loading…", colDim)
		return
	case len(s.playlists) == 0:
		a.drawCentered(c, area, "No playlists", colDim)
		return
	}
	s.cur.draw(a, c, area, len(s.playlists), func(i int, r gfx.Rect, focused bool) {
		pl := s.playlists[i]
		a.drawCoverCell(c, r, pl.CoverArt, pl.Name, trackCount(pl.SongCount), focused, false)
	}, func(i int, r gfx.Rect, focused bool) {
		pl := s.playlists[i]
		a.drawRow(c, r, row{cover: pl.CoverArt, thumb: true, main: pl.Name, sub: playlistSummary(pl), focused: focused})
	})
}

func trackCount(n int) string {
	if n == 1 {
		return "1 track"
	}
	return fmt.Sprintf("%d tracks", n)
}

// playlistSummary is "12 tracks · 48:10 · by alice".
func playlistSummary(pl subsonic.Playlist) string {
	parts := []string{trackCount(pl.SongCount)}
	if pl.Duration > 0 {
		parts = append(parts, clock(secs(pl.Duration)))
	}
	if pl.Owner != "" {
		parts = append(parts, "by "+pl.Owner)
	}
	return strings.Join(parts, " · ")
}

// PlaylistScreen shows a playlist's tracks under Play and Shuffle rows.
type PlaylistScreen struct {
	stub subsonic.Playlist
	pl   *subsonic.PlaylistWithSongs
	list List
	err  error
}

const playlistActionRows = 2 // Play, Shuffle

func NewPlaylistScreen(pl subsonic.Playlist) *PlaylistScreen { return &PlaylistScreen{stub: pl} }

func (s *PlaylistScreen) Title() string { return "Playlist" }

func (s *PlaylistScreen) Enter(a *App) {
	s.err = nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetPlaylist(ctx, s.stub.ID) }, func(v any, err error) {
		if err != nil {
			s.err = err
			return
		}
		s.pl = v.(*subsonic.PlaylistWithSongs)
		s.stub = s.pl.Playlist
	})
}

func (s *PlaylistScreen) songs() []subsonic.Song {
	if s.pl == nil {
		return nil
	}
	return s.pl.Songs
}

func (s *PlaylistScreen) Handle(a *App, e input.Event) bool {
	n := 0
	if s.pl != nil {
		n = playlistActionRows + len(s.songs())
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
		case s.pl == nil && s.err != nil:
			s.Enter(a)
		case s.pl == nil:
		case s.list.Focus < playlistActionRows:
			a.playSongs(s.songs(), 0, s.list.Focus == 1)
		default:
			a.playSongs(s.songs(), s.list.Focus-playlistActionRows, false)
		}
		return true
	case input.BtnX:
		if i := s.list.Focus - playlistActionRows; i >= 0 && i < len(s.songs()) {
			so := s.songs()[i]
			a.openMenu(so.Title, songMenu(a, so))
		} else {
			a.openMenu(s.stub.Name, queueEntries(s.stub.Name, playlistSongs(s.stub.ID)))
		}
		return true
	case input.BtnSelect:
		a.playSongs(s.songs(), 0, true)
		return true
	}
	return false
}

func (s *PlaylistScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	art := p.ArtAlbum
	header := gfx.R(area.X, area.Y, area.W, art+p.Margin)
	a.drawArt(c, s.stub.CoverArt, gfx.R(area.X+p.Margin, area.Y+p.Margin/2, art, art))
	x := area.X + p.Margin + art + p.Margin
	w := area.Right() - p.Margin - x
	y := area.Y + p.Margin/2
	ft, fb := a.F.Title, a.F.Body
	ft.Draw(c, x, y+ft.Ascent(), ft.Truncate(s.stub.Name, w), colText, header)
	y += ft.Height()
	fb.Draw(c, x, y+fb.Ascent(), fb.Truncate(playlistSummary(s.stub), w), colDim, header)

	body := gfx.R(area.X, header.Bottom(), area.W, area.H-header.H)
	switch {
	case s.pl == nil && s.err != nil:
		a.drawCentered(c, body, "Couldn't load playlist: "+subsonic.Classify(s.err).String()+" — A to retry", colError)
		return
	case s.pl == nil:
		a.drawCentered(c, body, "Loading…", colDim)
		return
	}
	songs := s.songs()
	s.list.Draw(c, body, playlistActionRows+len(songs), p.Row2H, func(i int, r gfx.Rect, focused bool) {
		switch i {
		case 0:
			f := a.F.Body
			iconText(c, f, iconPlay, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, "Play", colAccent, r)
		case 1:
			a.drawTextRow(c, r, "", false, "Shuffle", "", colAccent)
		default:
			so := songs[i-playlistActionRows]
			a.drawRow(c, r, row{cover: so.CoverArt, thumb: true, main: so.Title, sub: so.Artist, focused: focused,
				starred: a.isStarred(songStar(so)), right: clock(secs(so.Duration))})
		}
	})
}
```

`internal/ui/screens_starred.go` (new file):

```go
package ui

import (
	"context"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

// NewStarredScreen is the Starred section: albums, artists and tracks tabs
// over one getStarred2 call.
func NewStarredScreen() *TabbedScreen {
	d := &starredData{}
	return newTabbed("Starred", []string{"Albums", "Artists", "Tracks"},
		&starredTab{d: d, kind: starAlbum}, &starredTab{d: d, kind: starArtist}, &starredTab{d: d, kind: starSong})
}

// starredData is the getStarred2 result the three tabs share.
type starredData struct {
	res     *subsonic.Starred
	loading bool
	err     error
}

func (d *starredData) load(a *App, s Screen) {
	if d.res != nil || d.loading {
		return
	}
	d.loading, d.err = true, nil
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().GetStarred2(ctx) }, func(v any, err error) {
		d.loading = false
		if err != nil {
			d.err = err
			return
		}
		d.res = v.(*subsonic.Starred)
	})
}

// starredTab is one tab of Starred; kind picks albums, artists or tracks.
type starredTab struct {
	d       *starredData
	kind    starKind
	albums  albumsView
	artists artistsView
	songs   songsView
}

func (s *starredTab) Title() string { return "Starred" }
func (s *starredTab) Enter(a *App)  { s.d.load(a, s) }

// sync copies the shared result into this tab's view.
func (s *starredTab) sync() int {
	if s.d.res == nil {
		return 0
	}
	switch s.kind {
	case starAlbum:
		s.albums.albums = s.d.res.Albums
		return len(s.albums.albums)
	case starArtist:
		s.artists.artists = s.d.res.Artists
		return len(s.artists.artists)
	}
	s.songs.songs = s.d.res.Songs
	return len(s.songs.songs)
}

func (s *starredTab) Handle(a *App, e input.Event) bool {
	s.sync()
	if e.Kind == input.Press && e.Button == input.BtnA && s.d.err != nil {
		s.d.load(a, s)
		return true
	}
	switch s.kind {
	case starAlbum:
		return s.albums.Handle(a, e)
	case starArtist:
		return s.artists.Handle(a, e)
	}
	return s.songs.Handle(a, e)
}

func (s *starredTab) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	n := s.sync()
	switch {
	case s.d.err != nil:
		a.drawCentered(c, area, "Couldn't load starred items: "+subsonic.Classify(s.d.err).String()+" — A to retry", colError)
	case s.d.res == nil:
		a.drawCentered(c, area, "Loading…", colDim)
	case n == 0:
		a.drawCentered(c, area, "Nothing starred here yet — X on an item stars it", colDim)
	case s.kind == starAlbum:
		s.albums.Draw(a, c, area)
	case s.kind == starArtist:
		s.artists.Draw(a, c, area)
	default:
		s.songs.Draw(a, c, area)
	}
}
```

- [ ] **Step 4: Generate the screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`, and new goldens: `playlist-crt`, `playlist-hdmi`, `playlists-crt`, `playlists-hdmi`, `starred-tracks-crt`, `starred-tracks-hdmi`.
- **`playlists-*`:** "Дорога домой" (2 tracks) and "Empty".
- **`playlist-*`:** a header with cover, name and "2 tracks · 6:15 · by alice", then Play, Shuffle and two tracks.
- **`starred-tracks-*`:** the Tracks tab underlined, and "Капитан Африка" with its album.

No existing golden changes.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/fakes_test.go internal/ui/lists_test.go internal/ui/screens_playlists.go internal/ui/screens_starred.go internal/ui/testdata/golden
git commit -m "ui: playlists and starred" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 8: Search with the on-screen keyboard

**Files:**
- Create: `internal/ui/keyboard.go`, `internal/ui/screens_search.go`
- Modify: `internal/ui/views.go`, `internal/ui/screens_playlists.go` (their count labels use the new `plural`)
- Test: `internal/ui/search_test.go` (new)

**Interfaces:**
- **Consumes:** `After`, `LoadCancel`, `TextInput` (Task 3); `Tabs` (Task 4); the views (Task 6).
- **Produces:**
  - **`Keyboard`:** Latin or Cyrillic rows plus Space · Del · АБВ/ABC · Clear.
    - `Move(e) bool` is false at an edge; Up/Down go to the nearest key.
    - `Focused()`, `ToggleLayout()`, `Height(h)`, `Draw(a, c, r, h, focused)`.
  - **`SearchScreen`** (`NewSearchScreen()`, also a `TextInput`):
    - **Searching:** starts `searchDelay` = 300 ms after the last edit, through `LoadCancel`, which cancels the previous request. Results arrive in pages of `searchPage` = 50 per kind, and each tab loads more near its end.
    - **Moving:** Right or Down at the keyboard's edge enters the results, which open on the first kind that has any. B or Left in the results goes back to the keyboard.
    - **Layout:** HDMI puts the results beside the keyboard. A CRT shows a summary under the keyboard and swaps in the results when they have the focus.
  - `plural(n, word)`.

- [ ] **Step 1: Write the failing tests**

`internal/ui/search_test.go` (new file):

```go
package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

func TestKeyboardNavigation(t *testing.T) {
	var k Keyboard
	move := func(b input.Button) bool { return k.Move(press(b)) }
	if move(input.BtnUp) || move(input.BtnLeft) {
		t.Fatal("moved past the top-left corner")
	}
	move(input.BtnDown) // "1" -> "a"
	if got := k.Focused(); got.r != 'a' {
		t.Fatalf("focused %q", got.label)
	}
	for range 9 {
		move(input.BtnRight)
	}
	if move(input.BtnRight) || k.Focused().r != 'j' {
		t.Fatalf("right edge: %q", k.Focused().label)
	}
	move(input.BtnDown)
	move(input.BtnDown)
	move(input.BtnDown) // bottom row, nearest to "j" (the far right): Clear
	if got := k.Focused(); got.action != keyClear {
		t.Fatalf("under j: %q", got.label)
	}
	move(input.BtnLeft)
	k.ToggleLayout()
	if got := k.Focused(); got.label != "ABC" || !k.cyrillic {
		t.Fatalf("after toggle: %q", got.label)
	}
	move(input.BtnUp) // "эюя-'.&": the key nearest the middle of the switch key
	if got := k.Focused(); got.r != '&' {
		t.Fatalf("above the switch key: %q", got.label)
	}
	if move(input.BtnDown); move(input.BtnDown) {
		t.Fatal("moved below the bottom row")
	}
}

func typeKeys(ta *testApp, s string) {
	for _, r := range s {
		ta.onInput(input.Event{Kind: input.Press, Rune: r})
		ta.onInput(input.Event{Kind: input.Release, Rune: r})
	}
}

func TestSearchDebouncesAndCancels(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.block = make(chan struct{})
	typeKeys(ta, "bj")
	ta.settle(t)
	if len(ta.lib.searches) != 0 {
		t.Fatalf("searched before the pause: %v", ta.lib.searches)
	}
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	waitSearches(t, ta, 1)
	typeKeys(ta, "ö") // cancels "bj" still in flight
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	waitSearches(t, ta, 2)
	close(ta.lib.block)
	ta.settle(t)
	if !slices.Equal(ta.lib.searches, []string{"bj", "bjö"}) || s.searched != "bjö" {
		t.Fatalf("searches %v, showing %q", ta.lib.searches, s.searched)
	}
	if len(s.artists.artists) != 1 || s.artists.artists[0].Name != "Björk" || s.searching {
		t.Fatalf("artists %v searching %v", s.artists.artists, s.searching)
	}
	// Backspace edits; the empty field clears the results and then goes back.
	for range 3 {
		ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
	}
	if len(s.query) != 0 || ta.Top() != s {
		t.Fatalf("query %q top %T", string(s.query), ta.Top())
	}
	ta.onInput(input.Event{Button: input.BtnB, Kind: input.Press, Rune: '\b'})
	if ta.Top() == s {
		t.Fatal("Backspace in the empty field did not go back")
	}
}

// waitSearches waits for n Search3 calls to have started.
func waitSearches(t *testing.T, ta *testApp, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ta.lib.mu.Lock()
		got := len(ta.lib.searches)
		ta.lib.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d searches started, want %d", got, n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSearchWithTheOnScreenKeyboard(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.press(input.BtnDown) // "a"
	ta.press(input.BtnA)
	ta.press(input.BtnA)
	ta.press(input.BtnX) // Del
	if string(s.query) != "a" {
		t.Fatalf("query %q", string(s.query))
	}
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	// Latin "a": the artist "Some Artist" and its album "A Rather Long…".
	if c := s.counts(); c != [3]int{1, 1, 0} || s.tabs.Sel != resArtists {
		t.Fatalf("counts %v tab %d", c, s.tabs.Sel)
	}
	for range 9 {
		ta.press(input.BtnRight) // to the right edge, then into the results
	}
	ta.press(input.BtnRight)
	if !s.inResults {
		t.Fatal("Right at the keyboard's edge did not enter the results")
	}
	ta.press(input.BtnUp) // tabs
	ta.press(input.BtnRight)
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	if al, ok := ta.Top().(*AlbumScreen); !ok || al.stub.ID != "al-3" {
		t.Fatalf("A in album results opened %T", ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnB) // results -> keyboard
	if s.inResults || ta.Top() != s {
		t.Fatalf("B in the results: inResults %v top %T", s.inResults, ta.Top())
	}
}

func TestSearchPagesTracks(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	var many []subsonic.Song
	for i := range 70 {
		many = append(many, subsonic.Song{ID: subsonic.ID(fmt.Sprint("t", i)), Title: fmt.Sprint("Tune ", i), Duration: 60})
	}
	ta.lib.albums = append(ta.lib.albums, subsonic.Album{ID: "al-9", Name: "Filler"})
	ta.lib.tracks["al-9"] = many
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	typeKeys(ta, "tune")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if len(s.songs.songs) != searchPage || s.tabs.Sel != resTracks || !s.more[resTracks] {
		t.Fatalf("%d songs, tab %d, more %v", len(s.songs.songs), s.tabs.Sel, s.more)
	}
	for i := 0; !s.inResults; i++ { // down the keyboard and past its bottom row
		if i > 10 {
			t.Fatal("Down never reached the results")
		}
		ta.press(input.BtnDown)
	}
	for range 60 {
		ta.press(input.BtnDown)
	}
	ta.settle(t)
	if len(s.songs.songs) != 70 || s.more[resTracks] {
		t.Fatalf("after scrolling: %d songs, more %v", len(s.songs.songs), s.more[resTracks])
	}
}

func TestGoldenSearch(t *testing.T) {
	for _, p := range profiles {
		ta := newTestApp(t, p)
		ta.Push(NewHomeScreen())
		s := NewSearchScreen()
		ta.Push(s)
		typeKeys(ta, "a")
		ta.now = ta.now.Add(searchDelay)
		ta.onWake()
		ta.press(input.BtnDown)
		ta.press(input.BtnRight)
		golden(t, "search-"+p.Name, ta.settle(t))
		s.inResults = true
		golden(t, "search-results-"+p.Name, ta.settle(t))
	}
}

// The network can drop while searching: the error shows, and the next
// keystroke searches again.
func TestSearchErrorThenRetry(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.Push(NewHomeScreen())
	s := NewSearchScreen()
	ta.Push(s)
	ta.lib.err = errOffline
	typeKeys(ta, "b")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.err == nil || s.total() != 0 {
		t.Fatalf("err %v, %d results", s.err, s.total())
	}
	ta.lib.err = nil
	typeKeys(ta, "j")
	ta.now = ta.now.Add(searchDelay)
	ta.onWake()
	ta.settle(t)
	if s.err != nil || len(s.artists.artists) != 1 {
		t.Fatalf("after retry: err %v, artists %v", s.err, s.artists.artists)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: Keyboard
undefined: keyClear
undefined: NewSearchScreen
undefined: searchDelay
undefined: resArtists
```

- [ ] **Step 3: Implement**

`internal/ui/keyboard.go` (new file):

```go
package ui

import (
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
)

// Keyboard is the on-screen keyboard (spec §8.2): letter rows for the
// current layout (Latin or Cyrillic), digits, and a row of Space, Del,
// the layout switch and Clear. Keys are measured in units (a letter is one
// unit, a row is ten); Up/Down move to the key nearest the same place in
// the next row.
type Keyboard struct {
	cyrillic bool
	row, col int
}

type keyAction int

const (
	keyChar keyAction = iota
	keySpace
	keyDel
	keyLayout
	keyClear
)

type kbKey struct {
	label  string
	r      rune
	action keyAction
	units  int
}

const kbUnits = 10 // units per row

var (
	latinRows    = []string{"1234567890", "abcdefghij", "klmnopqrst", "uvwxyz-'.&"}
	cyrillicRows = []string{"1234567890", "абвгдеёжзи", "йклмнопрст", "уфхцчшщъыь", "эюя-'.&"}
)

// rows is the current layout.
func (k *Keyboard) rows() [][]kbKey {
	src := latinRows
	sw := "АБВ"
	if k.cyrillic {
		src, sw = cyrillicRows, "ABC"
	}
	out := make([][]kbKey, 0, len(src)+1)
	for _, s := range src {
		var row []kbKey
		for _, r := range s {
			row = append(row, kbKey{label: string(r), r: r, units: 1})
		}
		out = append(out, row)
	}
	return append(out, []kbKey{
		{label: "Space", action: keySpace, units: 4},
		{label: "Del", action: keyDel, units: 2},
		{label: sw, action: keyLayout, units: 2},
		{label: "Clear", action: keyClear, units: 2},
	})
}

// Focused is the key under the cursor.
func (k *Keyboard) Focused() kbKey {
	rows := k.rows()
	k.row = min(k.row, len(rows)-1)
	k.col = min(k.col, len(rows[k.row])-1)
	return rows[k.row][k.col]
}

// centre is the middle of key (row, col) in half-units, for Up/Down.
func centre(row []kbKey, col int) int {
	x := 0
	for _, key := range row[:col] {
		x += key.units
	}
	return 2*x + row[col].units
}

// Move handles the arrows; it returns false at an edge (Up on the top row,
// Down on the bottom one, Left or Right at a row's end) so the screen can
// move the focus elsewhere.
func (k *Keyboard) Move(e input.Event) bool {
	rows := k.rows()
	k.Focused() // clamp
	switch e.Button {
	case input.BtnLeft:
		if k.col == 0 {
			return false
		}
		k.col--
	case input.BtnRight:
		if k.col == len(rows[k.row])-1 {
			return false
		}
		k.col++
	case input.BtnUp, input.BtnDown:
		next := k.row - 1
		if e.Button == input.BtnDown {
			next = k.row + 1
		}
		if next < 0 || next >= len(rows) {
			return false
		}
		at := centre(rows[k.row], k.col)
		best, bestD := 0, 1<<30
		for i := range rows[next] {
			d := centre(rows[next], i) - at
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = i, d
			}
		}
		k.row, k.col = next, best
	default:
		return false
	}
	return true
}

// ToggleLayout switches Latin/Cyrillic, keeping the cursor on the switch key.
func (k *Keyboard) ToggleLayout() {
	k.cyrillic = !k.cyrillic
	rows := k.rows()
	k.row, k.col = len(rows)-1, 2
}

// Height is the keyboard's height at key height h.
func (k *Keyboard) Height(h int) int { return len(k.rows()) * h }

// Draw draws the keys into r, unit = r.W/10 wide and h high each; the
// focused key is highlighted when the keyboard has the focus.
func (k *Keyboard) Draw(a *App, c *gfx.Canvas, r gfx.Rect, h int, focused bool) {
	unit := r.W / kbUnits
	gap := max(unit/12, 1)
	f := a.F.Body
	if unit < f.Measure("Space")/3 {
		f = a.F.Small
	}
	k.Focused()
	for ri, row := range k.rows() {
		x := r.X
		for ci, key := range row {
			w := key.units * unit
			cell := gfx.R(x+gap, r.Y+ri*h+gap, w-2*gap, h-2*gap)
			bg, fg := colPanel, colText
			if focused && ri == k.row && ci == k.col {
				bg, fg = colAccent, colBg
			} else if key.action != keyChar {
				fg = colAccent
			}
			c.Fill(cell, bg)
			lw := f.Measure(key.label)
			f.Draw(c, cell.X+(cell.W-lw)/2, cell.Y+(cell.H+f.Ascent()-f.Descent())/2, key.label, fg, cell)
			x += w
		}
	}
}
```

`internal/ui/screens_search.go` (new file):

```go
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/subsonic"
)

const (
	searchDelay = 300 * time.Millisecond // after the last keystroke (spec §8.2)
	searchPage  = 50                     // results per kind and request
)

// Result kinds, in tab order.
const (
	resArtists = iota
	resAlbums
	resTracks
)

// SearchScreen is the on-screen keyboard with live search3 results in
// Artists / Albums / Tracks tabs. On HDMI the results sit beside the
// keyboard; on a CRT they replace it while they have the focus. A physical
// keyboard types too. B in the results goes back to the keyboard.
type SearchScreen struct {
	query     []rune
	kb        Keyboard
	inResults bool
	onTabs    bool
	tabs      Tabs
	artists   artistsView
	albums    albumsView
	songs     songsView
	more      [3]bool // a full page came back for this kind
	gen       int     // bumped by every edit; older timers and results are stale
	cancel    func()
	searched  string // query of the results shown
	searching bool
	err       error
}

func NewSearchScreen() *SearchScreen {
	return &SearchScreen{tabs: Tabs{Labels: []string{"Artists", "Albums", "Tracks"}}}
}

func (s *SearchScreen) Title() string { return "Search" }
func (s *SearchScreen) Enter(a *App)  {}

// Text takes physical keyboard input.
func (s *SearchScreen) Text(a *App, r rune) bool {
	if r == '\b' {
		if len(s.query) == 0 {
			return false // an empty field: Backspace goes back
		}
		s.edit(a, s.query[:len(s.query)-1])
		return true
	}
	if !unicode.IsPrint(r) {
		return false
	}
	s.edit(a, append(s.query, r))
	return true
}

// edit replaces the query and schedules a search after searchDelay; the
// previous request, if any, is cancelled.
func (s *SearchScreen) edit(a *App, q []rune) {
	s.query = q
	s.gen++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.searching = false
	if strings.TrimSpace(string(q)) == "" {
		s.setResults(&subsonic.SearchResult{}, "")
		return
	}
	gen := s.gen
	a.After(s, searchDelay, func() {
		if gen == s.gen {
			s.search(a)
		}
	})
}

func (s *SearchScreen) search(a *App) {
	q := strings.TrimSpace(string(s.query))
	gen := s.gen
	s.searching, s.err = true, nil
	s.cancel = a.LoadCancel(s, func(ctx context.Context) (any, error) {
		return a.Library().Search3(ctx, q, subsonic.SearchQuery{ArtistCount: searchPage, AlbumCount: searchPage, SongCount: searchPage})
	}, func(v any, err error) {
		if gen != s.gen {
			return
		}
		s.searching, s.cancel = false, nil
		if err != nil {
			s.err = err
			return
		}
		s.setResults(v.(*subsonic.SearchResult), q)
	})
}

func (s *SearchScreen) setResults(r *subsonic.SearchResult, q string) {
	s.searched = q
	s.artists = artistsView{artists: r.Artists}
	s.albums = albumsView{albums: r.Albums}
	s.songs = songsView{songs: r.Songs}
	s.more = [3]bool{len(r.Artists) == searchPage, len(r.Albums) == searchPage, len(r.Songs) == searchPage}
	counts := s.counts()
	if counts[s.tabs.Sel] == 0 { // show the first kind that has results
		for i, n := range counts {
			if n > 0 {
				s.tabs.Sel = i
				break
			}
		}
	}
	if s.total() == 0 {
		s.inResults = false
	}
}

func (s *SearchScreen) counts() [3]int {
	return [3]int{len(s.artists.artists), len(s.albums.albums), len(s.songs.songs)}
}

func (s *SearchScreen) total() int {
	c := s.counts()
	return c[0] + c[1] + c[2]
}

// loadMore fetches the next page of the current tab's kind.
func (s *SearchScreen) loadMore(a *App) {
	kind := s.tabs.Sel
	if !s.more[kind] || s.searching {
		return
	}
	s.more[kind] = false
	q, gen := s.searched, s.gen
	var sq subsonic.SearchQuery
	switch kind {
	case resArtists:
		sq.ArtistCount, sq.ArtistOffset = searchPage, len(s.artists.artists)
	case resAlbums:
		sq.AlbumCount, sq.AlbumOffset = searchPage, len(s.albums.albums)
	default:
		sq.SongCount, sq.SongOffset = searchPage, len(s.songs.songs)
	}
	a.Load(s, func(ctx context.Context) (any, error) { return a.Library().Search3(ctx, q, sq) }, func(v any, err error) {
		if gen != s.gen || err != nil {
			return
		}
		r := v.(*subsonic.SearchResult)
		switch kind {
		case resArtists:
			s.artists.artists = append(s.artists.artists, r.Artists...)
			s.more[kind] = len(r.Artists) == searchPage
		case resAlbums:
			s.albums.albums = append(s.albums.albums, r.Albums...)
			s.more[kind] = len(r.Albums) == searchPage
		default:
			s.songs.songs = append(s.songs.songs, r.Songs...)
			s.more[kind] = len(r.Songs) == searchPage
		}
	})
}

func (s *SearchScreen) Handle(a *App, e input.Event) bool {
	if s.inResults {
		return s.handleResults(a, e)
	}
	if s.kb.Move(e) {
		return true
	}
	if (e.Button == input.BtnRight || e.Button == input.BtnDown) && e.Kind != input.Release && s.total() > 0 {
		s.inResults, s.onTabs = true, false
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	switch e.Button {
	case input.BtnA:
		k := s.kb.Focused()
		switch k.action {
		case keyChar:
			s.edit(a, append(s.query, k.r))
		case keySpace:
			s.edit(a, append(s.query, ' '))
		case keyDel:
			if len(s.query) > 0 {
				s.edit(a, s.query[:len(s.query)-1])
			}
		case keyLayout:
			s.kb.ToggleLayout()
		case keyClear:
			s.edit(a, nil)
		}
		return true
	case input.BtnX: // shortcut for Del
		if len(s.query) > 0 {
			s.edit(a, s.query[:len(s.query)-1])
		}
		return true
	}
	return false
}

func (s *SearchScreen) handleResults(a *App, e input.Event) bool {
	back := func() bool { s.inResults = false; return true }
	if e.Kind == input.Press && e.Button == input.BtnB {
		return back()
	}
	if s.onTabs {
		if s.tabs.Handle(e) {
			return true
		}
		switch {
		case e.Kind == input.Release:
			return false
		case e.Button == input.BtnLeft || e.Button == input.BtnUp:
			return back()
		case e.Button == input.BtnDown || e.Button == input.BtnA:
			s.onTabs = false
			return true
		}
		return false
	}
	var used bool
	switch s.tabs.Sel {
	case resArtists:
		used = s.artists.Handle(a, e)
		if used && s.artists.cur.nearEnd(a, len(s.artists.artists)) {
			s.loadMore(a)
		}
	case resAlbums:
		used = s.albums.Handle(a, e)
		if used && s.albums.cur.nearEnd(a, len(s.albums.albums)) {
			s.loadMore(a)
		}
	default:
		used = s.songs.Handle(a, e)
		if used && s.songs.list.NearEnd(len(s.songs.songs)) {
			s.loadMore(a)
		}
	}
	if used || e.Kind == input.Release {
		return used
	}
	switch e.Button {
	case input.BtnUp:
		s.onTabs = true
		return true
	case input.BtnLeft:
		return back()
	}
	return false
}

func (s *SearchScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb := a.F.Body
	field := gfx.R(area.X+p.Margin, area.Y+p.Margin/4, 0, fb.Height()+p.Margin/2)
	wide := p.Cover > 0
	var kbArea, results gfx.Rect
	if wide { // keyboard left, results right
		kw := min(area.W*2/5, 12*p.RowH)
		field.W = kw - p.Margin
		kbArea = gfx.R(field.X, field.Bottom()+p.Margin/2, field.W, 0)
		results = gfx.R(area.X+kw+p.Margin/2, area.Y, area.W-kw-p.Margin/2, area.H)
	} else {
		field.W = area.W - 2*p.Margin
		kbArea = gfx.R(field.X, field.Bottom()+p.Margin/4, field.W, 0)
		results = gfx.R(area.X, field.Bottom(), area.W, area.Bottom()-field.Bottom())
	}
	s.drawField(a, c, field)
	keyH := kbArea.W / kbUnits
	if !wide {
		keyH = p.RowH
	}
	if wide || !s.inResults {
		s.kb.Draw(a, c, gfx.R(kbArea.X, kbArea.Y, kbArea.W, s.kb.Height(keyH)), keyH, !s.inResults)
	}
	if !wide && !s.inResults {
		// Below the keyboard on a CRT: what the results hold.
		y := kbArea.Y + s.kb.Height(keyH) + p.Margin/4
		s.drawStatus(a, c, gfx.R(area.X+p.Margin, y, area.W-2*p.Margin, area.Bottom()-y))
		return
	}
	if s.total() == 0 {
		s.drawStatus(a, c, results)
		return
	}
	h := p.RowH
	counts := s.counts()
	for i, base := range []string{"Artists", "Albums", "Tracks"} {
		s.tabs.Labels[i] = fmt.Sprintf("%s %d", base, counts[i])
		if s.more[i] {
			s.tabs.Labels[i] += "+"
		}
	}
	s.tabs.Draw(a, c, gfx.R(results.X, results.Y, results.W, h), s.inResults && s.onTabs)
	view := gfx.R(results.X, results.Y+h, results.W, results.H-h)
	switch s.tabs.Sel {
	case resArtists:
		s.artists.Draw(a, c, view)
	case resAlbums:
		s.albums.Draw(a, c, view)
	default:
		s.songs.Draw(a, c, view)
	}
	if !s.inResults { // the focus is on the keyboard: dim the results
		c.Fill(view, colBg.WithAlpha(0x60))
	}
}

// drawField draws the query with a caret, or a hint when it is empty.
func (s *SearchScreen) drawField(a *App, c *gfx.Canvas, r gfx.Rect) {
	p := a.P
	f := a.F.Body
	c.Fill(r, colPanel)
	x := r.X + p.Margin/2
	y := r.Y + (r.H+f.Ascent()-f.Descent())/2
	inner := r.Inset(p.Margin / 4)
	if len(s.query) == 0 {
		f.Draw(c, x, y, f.Truncate("Type or pick letters", r.W-p.Margin), colDim, inner)
		return
	}
	text := string(s.query)
	// Keep the end of a long query (where the typing is) visible.
	for f.Measure(text) > r.W-p.Margin-f.Ascent()/2 && len(text) > 0 {
		_, size := firstRune(text)
		text = text[size:]
	}
	end := f.Draw(c, x, y, text, colText, inner)
	c.Fill(gfx.R(end+1, y-f.Ascent(), max(f.Ascent()/8, 2), f.Ascent()+f.Descent()), colAccent)
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}

// drawStatus explains the result state: searching, an error, nothing
// found, or (on a CRT under the keyboard) the counts.
func (s *SearchScreen) drawStatus(a *App, c *gfx.Canvas, r gfx.Rect) {
	f := a.F.Small
	var text string
	col := colDim
	switch {
	case s.err != nil:
		text, col = "Search failed: "+subsonic.Classify(s.err).String(), colError
	case s.searching:
		text = "Searching…"
	case s.searched != "" && s.total() == 0:
		text = "Nothing found for “" + s.searched + "”"
	case s.total() > 0:
		c := s.counts()
		text = plural(c[0], "artist") + " · " + plural(c[1], "album") + " · " + plural(c[2], "track") + " — Down for results"
	case len(s.query) == 0:
		text = "Results appear as you type"
	default:
		return
	}
	f.Draw(c, r.X+a.P.Margin/2, r.Y+f.Ascent(), f.Truncate(text, r.W-a.P.Margin), col, r)
}

// plural is "1 album", "3 albums".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
```

Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

```diff
diff --git a/internal/ui/screens_playlists.go b/internal/ui/screens_playlists.go
index 2d69998..0b7f451 100644
--- a/internal/ui/screens_playlists.go
+++ b/internal/ui/screens_playlists.go
@@ -2,7 +2,6 @@ package ui
 
 import (
 	"context"
-	"fmt"
 	"strings"
 
 	"mistersubsonic/internal/gfx"
@@ -81,12 +80,7 @@ func (s *PlaylistsScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	})
 }
 
-func trackCount(n int) string {
-	if n == 1 {
-		return "1 track"
-	}
-	return fmt.Sprintf("%d tracks", n)
-}
+func trackCount(n int) string { return plural(n, "track") }
 
 // playlistSummary is "12 tracks · 48:10 · by alice".
 func playlistSummary(pl subsonic.Playlist) string {
diff --git a/internal/ui/views.go b/internal/ui/views.go
index 77d1cd7..08a0040 100644
--- a/internal/ui/views.go
+++ b/internal/ui/views.go
@@ -144,12 +144,7 @@ func (v *artistsView) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	})
 }
 
-func albumCount(n int) string {
-	if n == 1 {
-		return "1 album"
-	}
-	return fmt.Sprintf("%d albums", n)
-}
+func albumCount(n int) string { return plural(n, "album") }
 
 // songsView is a track list: A plays the whole list from the focused song.
 type songsView struct {
```

- [ ] **Step 4: Generate the screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`, and new goldens: `search-crt`, `search-hdmi`, `search-results-crt`, `search-results-hdmi`.
- **`search-hdmi`:** the query "a" with a caret, the keyboard with "b" highlighted, and on the right the dimmed results. The results show the tabs "Artists 1 · Albums 1 · Tracks 0" and the "Some Artist" cover.
- **`search-results-hdmi`:** the same with the keyboard not highlighted and the results bright.
- **`search-crt`:** the keyboard and "1 artist · 1 album · 0 tracks — Down for results".
- **`search-results-crt`:** the query, the tabs and the artist row, without the keyboard.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/keyboard.go internal/ui/screens_playlists.go internal/ui/screens_search.go internal/ui/search_test.go internal/ui/views.go internal/ui/testdata/golden
git commit -m "ui: search with the on-screen keyboard, debounced and cancellable" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 9: The HDMI sidebar and home feed; CRT sections

**Files:**
- Create: `internal/ui/screens_root.go`
- Modify: `internal/ui/screens_home.go`, `internal/ui/theme.go`
- Test: `internal/ui/root_test.go` (new)

**Interfaces:**
- **Consumes:** every section screen (Tasks 6–8); `owner`, `TextInput` (Task 3).
- **Produces:**
  - **`NewRootScreen(p Profile) Screen`:** a `SidebarRoot` when `p.SideW > 0` (HDMI, 250), else `HomeScreen` (CRT).
  - **`SidebarRoot`:**
    - Sections are Home, Artists, Albums, Playlists, Starred and Search. Each is created and entered on first view and kept afterwards.
    - Focus moves between the sidebar and the section (see Decisions).
    - The title is the section's ("Artists · B"), or "MiSTer Subsonic" on Home.
    - It forwards typed text to Search when that has the focus.
  - **`FeedScreen`:**
    - A Resume card when a saved queue exists. The focus stays on the covers when it arrives.
    - Then four rows of up to `feedPage` = 20 covers.
    - Up/Down pick a row and Left/Right move along it. A opens, X gives the menu, Select shuffles the row.
  - **`HomeScreen`:** on a CRT it lists the sections after the album lists (`sections`, `homeItem.open`).

- [ ] **Step 1: Write the failing tests**

`internal/ui/root_test.go` (new file):

```go
package ui

import (
	"slices"
	"testing"
	"time"

	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func TestRootPicksTheLayout(t *testing.T) {
	if _, ok := NewRootScreen(ProfileHDMI).(*SidebarRoot); !ok {
		t.Fatal("HDMI root is not the sidebar")
	}
	if _, ok := NewRootScreen(ProfileCRT240).(*HomeScreen); !ok {
		t.Fatal("CRT root is not the sections list")
	}
}

func TestSidebarSwitchesSections(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	root := newSidebarRoot()
	ta.Push(root)
	ta.settle(t)
	if _, ok := root.children[0].(*FeedScreen); !ok || root.inSidebar {
		t.Fatalf("starts in %T, sidebar %v", root.children[0], root.inSidebar)
	}
	ta.press(input.BtnLeft) // first cover: Left goes to the sidebar
	if !root.inSidebar {
		t.Fatal("Left on the first cover did not reach the sidebar")
	}
	ta.press(input.BtnDown) // Artists: created and loaded now
	ta.settle(t)
	artists, ok := root.children[1].(*ArtistsScreen)
	if !ok || len(artists.view.artists) != 3 || root.Title() != "Artists · B" {
		t.Fatalf("section %T, title %q", root.children[1], root.Title())
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // in the grid
	if root.inSidebar || artists.view.cur.focus() != 1 {
		t.Fatalf("sidebar %v, focus %d", root.inSidebar, artists.view.cur.focus())
	}
	ta.press(input.BtnB) // B in a section: the sidebar, not an exit
	if !root.inSidebar || ta.Top() != root {
		t.Fatalf("after B: sidebar %v top %T", root.inSidebar, ta.Top())
	}
	ta.press(input.BtnUp)
	ta.press(input.BtnDown)
	if root.children[1] != artists {
		t.Fatal("switching back recreated the section")
	}
	for range 4 {
		ta.press(input.BtnDown) // Search
	}
	ta.press(input.BtnA)
	typeKeys(ta, "bj")
	if s := root.children[5].(*SearchScreen); string(s.query) != "bj" {
		t.Fatalf("typing reached %q", string(s.query))
	}
}

func TestFeedRowsAndResume(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	s := NewFeedScreen()
	ta.Push(s)
	ta.settle(t)
	var types []string
	for _, q := range ta.lib.calls {
		if q.Size != feedPage {
			t.Fatalf("row query %+v", q)
		}
		types = append(types, q.Type)
	}
	slices.Sort(types) // the rows load in parallel
	want := []string{subsonic.ListNewest, subsonic.ListRecent, subsonic.ListFrequent, subsonic.ListRandom}
	slices.Sort(want)
	if !slices.Equal(types, want) {
		t.Fatalf("rows loaded %v", types)
	}
	if s.current() != s.rows[0] {
		t.Fatal("the focus moved off the first row when Resume arrived")
	}
	ta.press(input.BtnRight)
	ta.press(input.BtnRight)
	ta.press(input.BtnRight) // stops at the last cover
	ta.press(input.BtnA)
	if al, ok := ta.Top().(*AlbumScreen); !ok || al.stub.ID != "al-3" {
		t.Fatalf("A opened %T", ta.Top())
	}
	ta.Pop()
	ta.press(input.BtnUp) // the Resume card
	ta.press(input.BtnA)
	if !slices.Equal(ta.pl.calls, []string{"resume"}) {
		t.Fatalf("calls %v", ta.pl.calls)
	}
}

func TestCRTHomeOpensSections(t *testing.T) {
	ta := newTestApp(t, ProfileCRT240)
	ta.Push(NewRootScreen(ta.P))
	ta.settle(t)
	for range 4 { // past the album lists
		ta.press(input.BtnDown)
	}
	ta.press(input.BtnDown) // Albums
	ta.press(input.BtnA)
	if s, ok := ta.Top().(*TabbedScreen); !ok || s.Title() != "Albums" {
		t.Fatalf("A opened %T", ta.Top())
	}
}

func TestGoldenRoot(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.pl.resume = &player.Resume{Songs: ta.lib.tracks["al-1"], Index: 1, Position: 30 * time.Second}
	root := newSidebarRoot()
	ta.Push(root)
	ta.settle(t)
	golden(t, "root-feed-hdmi", ta.settle(t))
	ta.press(input.BtnLeft)
	ta.press(input.BtnDown)
	ta.press(input.BtnDown)
	ta.settle(t)
	golden(t, "root-albums-hdmi", ta.settle(t))
}

// Every screen, in both layouts, with a normal, an empty and a failing
// library, before and after its loads finish: every button, pressed and
// held, must neither panic nor empty the stack.
func TestEveryScreenSurvivesEveryButton(t *testing.T) {
	screens := map[string]func(ta *testApp) Screen{
		"root":      func(ta *testApp) Screen { return NewRootScreen(ta.P) },
		"feed":      func(*testApp) Screen { return NewFeedScreen() },
		"artists":   func(*testApp) Screen { return NewArtistsScreen() },
		"artist":    func(*testApp) Screen { return NewArtistScreen(subsonic.Artist{ID: "ar-1", Name: "Аквариум"}) },
		"albums":    func(*testApp) Screen { return NewAlbumsScreen() },
		"albumlist": func(*testApp) Screen { return NewAlbumListScreen("Recently added", subsonic.ListNewest) },
		"album": func(ta *testApp) Screen {
			return NewAlbumScreen(subsonic.Album{ID: "al-1", Name: "Радио Африка"})
		},
		"genres":    func(*testApp) Screen { return NewGenresScreen() },
		"playlists": func(*testApp) Screen { return NewPlaylistsScreen() },
		"playlist": func(*testApp) Screen {
			return NewPlaylistScreen(subsonic.Playlist{ID: "pl-1", Name: "Дорога домой"})
		},
		"starred": func(*testApp) Screen { return NewStarredScreen() },
		"search":  func(*testApp) Screen { return NewSearchScreen() },
		"playing": func(*testApp) Screen { return NewNowPlayingScreen() },
		"queue":   func(*testApp) Screen { return NewQueueScreen() },
	}
	buttons := []input.Button{input.BtnUp, input.BtnDown, input.BtnLeft, input.BtnRight, input.BtnL, input.BtnR,
		input.BtnX, input.BtnSelect, input.BtnStart, input.BtnA, input.BtnY, input.BtnQueue, input.BtnB}
	libs := map[string]func(ta *testApp){
		"normal":  func(*testApp) {},
		"empty":   func(ta *testApp) { ta.lib.albums, ta.lib.artists, ta.lib.playlists, ta.lib.genres = nil, nil, nil, nil },
		"failing": func(ta *testApp) { ta.lib.err = errOffline },
	}
	for _, p := range profiles {
		for sname, mk := range screens {
			for lname, setup := range libs {
				for _, early := range []bool{true, false} {
					ta := newTestApp(t, p)
					setup(ta)
					ta.Push(NewHomeScreen())
					ta.Push(mk(ta))
					if !early {
						ta.settle(t)
					}
					for round := range 2 {
						for _, b := range buttons {
							ta.onInput(input.Event{Button: b, Kind: input.Press})
							ta.dispatch(input.Event{Button: b, Kind: input.Repeat})
							ta.onInput(input.Event{Button: b, Kind: input.Release})
							if len(ta.stack) == 0 || ta.Top() == nil {
								t.Fatalf("%s/%s/%s round %d: %v emptied the stack", p.Name, sname, lname, round, b)
							}
						}
						ta.settle(t)
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui`

Expected: a build failure:

```
undefined: NewRootScreen
undefined: newSidebarRoot
undefined: NewFeedScreen
undefined: feedPage
```

- [ ] **Step 3: Implement**

`internal/ui/screens_root.go` (new file):

```go
package ui

import (
	"context"
	"fmt"

	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

// SidebarRoot is the HDMI root: a sidebar of sections on the left and the
// selected section on the right. Left from a section's first column, or B,
// moves the focus to the sidebar; Right or A goes back into the section.
// Sections are created and entered the first time they are shown, and keep
// their state while the user switches.
type SidebarRoot struct {
	labels    []string
	makers    []func() Screen
	children  []Screen
	sel       int
	inSidebar bool
}

func newSidebarRoot() *SidebarRoot {
	s := &SidebarRoot{labels: []string{"Home"}, makers: []func() Screen{func() Screen { return NewFeedScreen() }}}
	for _, it := range sections {
		s.labels = append(s.labels, it.label)
		s.makers = append(s.makers, it.open)
	}
	s.children = make([]Screen, len(s.makers))
	return s
}

func (s *SidebarRoot) Title() string {
	if s.sel == 0 || s.children[s.sel] == nil {
		return "MiSTer Subsonic"
	}
	return s.children[s.sel].Title()
}

func (s *SidebarRoot) Owns(x Screen) bool {
	for _, c := range s.children {
		if c == nil {
			continue
		}
		if c == x {
			return true
		}
		if o, ok := c.(owner); ok && o.Owns(x) {
			return true
		}
	}
	return false
}

func (s *SidebarRoot) Enter(a *App) { s.show(a) }

// show creates and enters the selected section on first use.
func (s *SidebarRoot) show(a *App) Screen {
	if s.children[s.sel] == nil {
		s.children[s.sel] = s.makers[s.sel]()
		s.children[s.sel].Enter(a)
	}
	return s.children[s.sel]
}

// Text forwards typing to the section (Search) when it has the focus.
func (s *SidebarRoot) Text(a *App, r rune) bool {
	if t, ok := s.show(a).(TextInput); ok && !s.inSidebar {
		return t.Text(a, r)
	}
	return false
}

func (s *SidebarRoot) Handle(a *App, e input.Event) bool {
	if s.inSidebar {
		switch e.Button {
		case input.BtnUp, input.BtnDown:
			if e.Kind == input.Release {
				return false
			}
			if e.Button == input.BtnUp {
				s.sel = max(s.sel-1, 0)
			} else {
				s.sel = min(s.sel+1, len(s.labels)-1)
			}
			s.show(a)
			return true
		case input.BtnRight, input.BtnA:
			if e.Kind == input.Press {
				s.inSidebar = false
				return true
			}
		}
		return false
	}
	if s.show(a).Handle(a, e) {
		return true
	}
	if e.Kind == input.Release {
		return false
	}
	if e.Button == input.BtnLeft || (e.Button == input.BtnB && e.Kind == input.Press) {
		s.inSidebar = true
		return true
	}
	return false
}

func (s *SidebarRoot) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	side := gfx.R(area.X, area.Y, p.SideW, area.H)
	c.Fill(side, colPanel)
	f := a.F.Body
	for i, l := range s.labels {
		r := gfx.R(side.X, side.Y+p.Margin/2+i*p.RowH, side.W, p.RowH)
		col := colDim
		if i == s.sel {
			col = colText
			if s.inSidebar {
				c.Fill(r, colFocus)
			}
			c.Fill(gfx.R(r.X, r.Y, max(p.Margin/8, 3), r.H), colAccent)
		}
		f.Draw(c, r.X+p.Margin, r.Y+(r.H+f.Ascent()-f.Descent())/2, l, col, r)
	}
	s.show(a).Draw(a, c, gfx.R(side.Right(), area.Y, area.W-side.W, area.H))
}

// FeedScreen is the HDMI home: a Resume card when a saved queue exists,
// then rows of covers (recently added, recently played, most played,
// random). Up/Down pick a row, Left/Right move along it.
type FeedScreen struct {
	rows   []*feedRow
	resume *player.Resume
	row    int // focused row; the Resume card is row 0 when present
	top    int // first visible row
}

type feedRow struct {
	label, listType string
	albums          []subsonic.Album
	loaded          bool
	err             error
	focus, first    int // focused and first visible cover
	visible         int // covers that fit, at the last Draw
}

const feedPage = 20 // covers per row

func NewFeedScreen() *FeedScreen {
	s := &FeedScreen{}
	for _, it := range homeLists {
		s.rows = append(s.rows, &feedRow{label: it.label, listType: it.listType})
	}
	return s
}

func (s *FeedScreen) Title() string { return "MiSTer Subsonic" }

func (s *FeedScreen) Enter(a *App) {
	for _, r := range s.rows {
		s.load(a, r)
	}
	if a.Player() == nil {
		return
	}
	a.Load(s, func(ctx context.Context) (any, error) { return a.Player().Resumable(ctx) }, func(v any, err error) {
		if r, ok := v.(*player.Resume); ok && err == nil && r != nil && len(a.Player().State().Queue) == 0 {
			s.resume = r
			s.row++ // keep the focus on the same row of covers
		}
	})
}

func (s *FeedScreen) load(a *App, r *feedRow) {
	r.err = nil
	a.Load(s, func(ctx context.Context) (any, error) {
		return a.Library().GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: r.listType, Size: feedPage})
	}, func(v any, err error) {
		if err != nil {
			r.err = err
			return
		}
		r.albums, _ = v.([]subsonic.Album)
		r.loaded = true
	})
}

func (s *FeedScreen) count() int {
	if s.resume != nil {
		return len(s.rows) + 1
	}
	return len(s.rows)
}

// current is the focused row of covers, or nil on the Resume card.
func (s *FeedScreen) current() *feedRow {
	i := s.row
	if s.resume != nil {
		if i == 0 {
			return nil
		}
		i--
	}
	return s.rows[i]
}

func (s *FeedScreen) Handle(a *App, e input.Event) bool {
	r := s.current()
	switch e.Button {
	case input.BtnUp, input.BtnDown:
		if e.Kind == input.Release {
			return false
		}
		next := s.row - 1
		if e.Button == input.BtnDown {
			next = s.row + 1
		}
		if next < 0 || next >= s.count() {
			return false
		}
		s.row = next
		return true
	case input.BtnLeft, input.BtnRight:
		if e.Kind == input.Release || r == nil {
			return false
		}
		if e.Button == input.BtnLeft {
			if r.focus == 0 {
				return false
			}
			r.focus--
		} else if r.focus+1 < len(r.albums) {
			r.focus++
		}
		return true
	}
	if e.Kind != input.Press {
		return false
	}
	if r == nil { // the Resume card
		if e.Button == input.BtnA {
			a.Player().ResumeFrom(s.resume)
			s.resume, s.row = nil, 0
			a.Push(NewNowPlayingScreen())
			return true
		}
		return false
	}
	switch {
	case e.Button == input.BtnA && r.err != nil:
		s.load(a, r)
	case len(r.albums) == 0:
		return false
	case e.Button == input.BtnA:
		a.Push(NewAlbumScreen(r.albums[r.focus]))
	case e.Button == input.BtnX:
		al := r.albums[r.focus]
		a.openMenu(al.Name, albumMenu(a, al))
	case e.Button == input.BtnSelect:
		a.withSongs(albumsSongs(sampleAlbums(r.albums)), func(songs []subsonic.Song) { a.playSongs(songs, 0, true) })
	default:
		return false
	}
	return true
}

func (s *FeedScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
	p := a.P
	fb := a.F.Body
	cellW, cellH := a.coverCell()
	labelH := fb.Height() + p.Margin/3
	rowH := labelH + cellH
	resumeH := labelH + p.Thumb + p.Margin
	// Scroll so the focused row is visible (rows are the same height, but
	// the Resume card is shorter; treating it as a full row is fine).
	visible := max(area.H/rowH, 1)
	if s.row < s.top {
		s.top = s.row
	}
	if s.row >= s.top+visible {
		s.top = s.row - visible + 1
	}
	y := area.Y + p.Margin/3
	for i := s.top; i < s.count() && y < area.Bottom(); i++ {
		focused := i == s.row
		if s.resume != nil && i == 0 {
			s.drawResume(a, c, gfx.R(area.X, y, area.W, resumeH), focused)
			y += resumeH
			continue
		}
		r := s.rows[i]
		if s.resume != nil {
			r = s.rows[i-1]
		}
		col := colDim
		if focused {
			col = colText
		}
		fb.Draw(c, area.X+p.Margin, y+fb.Ascent(), r.label, col, area)
		strip := gfx.R(area.X+p.Margin-cellW/10, y+labelH, area.W-p.Margin, cellH)
		s.drawStrip(a, c, r, strip, cellW, focused)
		y += rowH
	}
}

func (s *FeedScreen) drawResume(a *App, c *gfx.Canvas, r gfx.Rect, focused bool) {
	p := a.P
	fb := a.F.Body
	fb.Draw(c, r.X+p.Margin, r.Y+fb.Ascent(), "Resume", colDim, r)
	card := gfx.R(r.X+p.Margin/2, r.Y+fb.Height()+p.Margin/3, r.W-p.Margin, p.Thumb+p.Margin/2)
	if focused {
		c.Fill(card, colFocus)
	}
	song := s.resume.Songs[s.resume.Index]
	sub := song.Artist
	if s.resume.Position > 0 {
		sub = fmt.Sprintf("%s · from %s", song.Artist, clock(s.resume.Position))
	}
	a.drawRow(c, card, row{cover: song.CoverArt, thumb: true, main: song.Title, sub: sub, col: colAccent, focused: focused})
}

// drawStrip draws one row of covers, scrolled so its focus is visible.
func (s *FeedScreen) drawStrip(a *App, c *gfx.Canvas, r *feedRow, strip gfx.Rect, cellW int, focused bool) {
	switch {
	case r.err != nil:
		a.drawCentered(c, strip, "Couldn't load: "+subsonic.Classify(r.err).String()+" — A to retry", colError)
		return
	case !r.loaded:
		a.drawCentered(c, strip, "Loading…", colDim)
		return
	case len(r.albums) == 0:
		a.drawCentered(c, strip, "Nothing here yet", colDim)
		return
	}
	r.visible = max(strip.W/cellW, 1)
	if r.focus < r.first {
		r.first = r.focus
	}
	if r.focus >= r.first+r.visible {
		r.first = r.focus - r.visible + 1
	}
	for i := r.first; i < len(r.albums) && i < r.first+r.visible; i++ {
		al := r.albums[i]
		cell := gfx.R(strip.X+(i-r.first)*cellW, strip.Y, cellW, strip.H)
		a.drawCoverCell(c, cell, al.CoverArt, al.Name, al.Artist, focused && i == r.focus, a.isStarred(albumStar(al)))
	}
}
```

Save this patch as `/tmp/t9-code.patch` and apply it from the repository root with `git apply /tmp/t9-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 8):

```diff
diff --git a/internal/ui/screens_home.go b/internal/ui/screens_home.go
index e9790c8..f28c129 100644
--- a/internal/ui/screens_home.go
+++ b/internal/ui/screens_home.go
@@ -15,17 +15,38 @@ import (
 
 type homeItem struct {
 	label    string
-	listType string // album list type; "" for Resume
+	listType string        // album list type
+	open     func() Screen // a section; nil for album lists and Resume
 }
 
 var homeLists = []homeItem{
-	{"Recently added", subsonic.ListNewest},
-	{"Recently played", subsonic.ListRecent},
-	{"Most played", subsonic.ListFrequent},
-	{"Random albums", subsonic.ListRandom},
+	{label: "Recently added", listType: subsonic.ListNewest},
+	{label: "Recently played", listType: subsonic.ListRecent},
+	{label: "Most played", listType: subsonic.ListFrequent},
+	{label: "Random albums", listType: subsonic.ListRandom},
 }
 
-// HomeScreen is the root: Resume (if a saved queue exists) and album lists.
+// sections are the library's parts: the HDMI sidebar after Home, and the
+// CRT root list after the album lists.
+var sections = []homeItem{
+	{label: "Artists", open: func() Screen { return NewArtistsScreen() }},
+	{label: "Albums", open: func() Screen { return NewAlbumsScreen() }},
+	{label: "Playlists", open: func() Screen { return NewPlaylistsScreen() }},
+	{label: "Starred", open: func() Screen { return NewStarredScreen() }},
+	{label: "Search", open: func() Screen { return NewSearchScreen() }},
+}
+
+// NewRootScreen is the first screen after connecting: the sidebar and home
+// feed on HDMI, the sections list on a CRT.
+func NewRootScreen(p Profile) Screen {
+	if p.SideW > 0 {
+		return newSidebarRoot()
+	}
+	return NewHomeScreen()
+}
+
+// HomeScreen is the CRT root (and the fallback): Resume (if a saved queue
+// exists), the album lists and the sections.
 type HomeScreen struct {
 	list   List
 	resume *player.Resume
@@ -52,7 +73,7 @@ func (s *HomeScreen) items() []homeItem {
 		song := s.resume.Songs[s.resume.Index]
 		out = append(out, homeItem{label: fmt.Sprintf("Resume: %s — %s", song.Title, song.Artist)})
 	}
-	return append(out, homeLists...)
+	return append(append(out, homeLists...), sections...)
 }
 
 func (s *HomeScreen) Handle(a *App, e input.Event) bool {
@@ -64,13 +85,16 @@ func (s *HomeScreen) Handle(a *App, e input.Event) bool {
 		return false
 	}
 	it := items[s.list.Focus]
-	if it.listType == "" {
+	switch {
+	case it.open != nil:
+		a.Push(it.open())
+	case it.listType == "":
 		a.Player().ResumeFrom(s.resume)
 		s.resume = nil
 		a.Push(NewNowPlayingScreen())
-		return true
+	default:
+		a.Push(NewAlbumListScreen(it.label, it.listType))
 	}
-	a.Push(NewAlbumListScreen(it.label, it.listType))
 	return true
 }
 
@@ -78,10 +102,10 @@ func (s *HomeScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 	items := s.items()
 	s.list.Draw(c, area, len(items), a.P.RowH, func(i int, r gfx.Rect, focused bool) {
 		col := colText
-		if items[i].listType == "" {
+		if items[i].listType == "" && items[i].open == nil {
 			col = colAccent
 		}
-		a.drawTextRow(c, r, "", false, items[i].label, "", col)
+		a.drawRow(c, r, row{main: items[i].label, col: col, focused: focused})
 	})
 }
 
diff --git a/internal/ui/theme.go b/internal/ui/theme.go
index 4b5eeb4..825d93c 100644
--- a/internal/ui/theme.go
+++ b/internal/ui/theme.go
@@ -28,11 +28,12 @@ type Profile struct {
 	SafeY        int
 	MarqueeSpeed int // px per second
 	Cover        int // cover size in grids; 0 = lists with thumbnails instead (CRT)
+	SideW        int // sidebar width; 0 = no sidebar (CRT: sections are a list)
 }
 
 var (
 	ProfileHDMI = Profile{Name: "hdmi", W: 1280, H: 720, Margin: 36, HeaderH: 64, RowH: 56, Row2H: 76, Thumb: 60,
-		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60, Cover: 170}
+		Title: 34, Body: 24, Small: 18, ArtNow: 400, ArtAlbum: 200, MiniBarH: 72, MarqueeSpeed: 60, Cover: 170, SideW: 250}
 	ProfileCRT240 = Profile{Name: "crt", W: 320, H: 240, Margin: 16, HeaderH: 22, RowH: 18, Row2H: 32, Thumb: 26,
 		Title: 15, Body: 12, Small: 10, ArtNow: 110, ArtAlbum: 56, MiniBarH: 24, SafeY: 12, MarqueeSpeed: 24}
 )
```

- [ ] **Step 4: Regenerate the screenshots and look at them**

Run: `go test -count=1 ./internal/ui -update && go test -race -count=1 ./internal/ui`

Expected: `ok`. Changed or new goldens: `home-crt`, `home-hdmi`, `home-minibar-crt`, `home-minibar-hdmi`, `root-albums-hdmi`, `root-feed-hdmi`.
- **`root-feed-hdmi`:** a sidebar with Home selected (accent bar). Beside it the Resume card ("Время Луны", "Аквариум · from 0:30"), a "Recently added" row with the first cover highlighted, and the next row cut off at the bottom.
- **`root-albums-hdmi`:** the sidebar with Albums highlighted (focused) and the Albums tabs and grid beside it.
- **`home-*`:** the list now continues with Artists, Albums, Playlists, Starred and Search.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/root_test.go internal/ui/screens_home.go internal/ui/screens_root.go internal/ui/theme.go internal/ui/testdata/golden
git commit -m "ui: HDMI sidebar root and home feed, CRT sections list" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 10: The app, the mock server, the UI e2e path through Search, README

**Files:**
- Modify: `cmd/mistersubsonic/main.go`, `tools/mocksubsonic/main.go`, `scripts/e2e-ui.sh`, `README.md`, `internal/ui/bench_test.go`

**Interfaces:**
- **Consumes:** `ui.NewRootScreen`, `(*App).SetInsecure`.
- **Produces:**
  - **The app** starts on the root screen and shows the insecure badge when the active server has `insecure_skip_verify`.
  - **The mock server** answers `getArtist`, `getGenres`, `getPlaylists`, `getPlaylist`, `getStarred2` and a paged `search3`, and logs `query=`.
  - **`make e2e`:**
    - plays the first cover of the feed
    - then goes to the sidebar → Search
    - types "mock" as keyboard text
    - enters the results → Tracks → the second track
    - passes only if a stream follows the search
  - **The benchmark** gains `feed-hdmi-1080p` and `search-hdmi-1080p`.

- [ ] **Step 1: Apply the changes**

Save this patch as `/tmp/t10-code.patch` and apply it from the repository root with `git apply /tmp/t10-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 9):

```diff
diff --git a/README.md b/README.md
index 9db71b6..ed3dd27 100644
--- a/README.md
+++ b/README.md
@@ -3,11 +3,28 @@
 A Subsonic / Navidrome music player for [MiSTer FPGA](https://github.com/MiSTer-devel), in progress.
 Design: `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`.
 
-Status: **playback core plus a first TV interface**: Home (resume, recent/most played/random
-albums), album pages, Now Playing and the queue, on HDMI or CRT layouts, driven by a controller
-or keyboard. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek, scrobbling and resume work.
-Still to come: artists, playlists, starred, search, the setup wizard and the MiSTer launcher.
-On-device (MiSTer) checks are still pending; see `docs/spikes.md`.
+Status: **playback core plus the library interface**: a home feed with resume, artists (A–Z),
+albums (A–Z, by year, by genre), playlists, starred items and search with an on-screen keyboard,
+album pages, Now Playing and the queue, on HDMI (sidebar and cover grids) or CRT (lists)
+layouts, driven by a controller or keyboard. Streaming, FLAC/MP3/WAV decoding, gapless playback,
+seek, scrobbling and resume work. Still to come: the setup wizard, settings, the screensaver and
+the MiSTer launcher. On-device (MiSTer) checks are still pending; see `docs/spikes.md`.
+
+## Controls
+
+| Button | Browsing | Now Playing |
+|---|---|---|
+| D-pad | move (Left into the HDMI sidebar) | ←/→ seek ±10 s (held: ±30 s), ↑/↓ volume |
+| A | open / play | play/pause |
+| B | back (hold 2 s on the root to exit) | back |
+| X | menu: play now/next, add to queue, star, go to artist/album | star/unstar |
+| Y | Now Playing | queue |
+| L / R | page (letter jump on Artists) | previous / next track |
+| Start | play/pause | play/pause |
+| Select | shuffle-play the list | shuffle / repeat modes |
+
+Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, PgUp/PgDn = L/R,
+Space = Start. In Search, letters type.
 
 ## Developing
 
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 31a29ee..d21fbaf 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -270,6 +270,8 @@ func run(f flags) error {
 		s.started = true
 		go func() { s.pl.Run(pctx); close(playerDone) }()
 		a.Attach(c, s.pl, s.loader)
+		srv, _ := cfg.ActiveServer()
+		a.SetInsecure(srv.InsecureSkipVerify)
 	}
 
 	app, err := ui.New(ui.Options{
@@ -308,7 +310,7 @@ func run(f flags) error {
 							return
 						}
 						startSession(a, c, s)
-						a.Replace(ui.NewHomeScreen())
+						a.Replace(ui.NewRootScreen(a.P))
 					})
 				}()
 			}
diff --git a/scripts/e2e-ui.sh b/scripts/e2e-ui.sh
index c485fe9..80da703 100755
--- a/scripts/e2e-ui.sh
+++ b/scripts/e2e-ui.sh
@@ -1,8 +1,10 @@
 #!/bin/sh
 # End-to-end check of the app: the mock server serves an album, the app runs
-# headless on the NULL audio device (silent), scripted keys go
-# Home -> Recently added -> album -> Play, and every frame is saved as PNG.
-# Passes if the app exits cleanly, drew frames and streamed the first track.
+# headless on the NULL audio device (silent), and every frame is saved as PNG.
+# Scripted keys: the home feed's first cover -> album -> Play (streams the
+# first track); back to the root, the sidebar -> Search, type "mock", into
+# the results -> Tracks -> the second track (streams it).
+# Passes if the app exits cleanly, drew frames, searched and streamed both.
 set -eu
 cd "$(dirname "$0")/.."
 tmp=$(mktemp -d)
@@ -37,15 +39,19 @@ if [ -z "$up" ]; then
 fi
 
 "$tmp/mistersubsonic" -config "$tmp/config.toml" -display headless -frames "$tmp/frames" -null \
-  -keys "a:1500ms,a:800ms,a:800ms" -exit-after 6s > "$tmp/app.log" 2>&1 || {
+  -keys "a:1500ms,a:800ms,b:800ms,b,left,down,down,down,down,down,right,'mock,right:1s$(printf ',right:100ms%.0s' 1 2 3 4 5 6 7 8 9),up,right,right,down,down,a" \
+  -exit-after 14s > "$tmp/app.log" 2>&1 || {
   echo "e2e-ui failed: app exited with an error" >&2
   cat "$tmp/app.log" >&2
   exit 1
 }
 frames=$(ls "$tmp/frames" | wc -l)
-if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log"; then
+# A stream after the search3 line comes from the search results (the album's
+# tracks, including the gapless prefetch, were streamed before it).
+searched_then_streamed() { awk '/search3 .*query="mock"/ { s = 1 } s && /stream so-/ { ok = 1 } END { exit !ok }' "$tmp/mock.log"; }
+if [ "$frames" -lt 5 ] || ! grep -q 'stream so-0' "$tmp/mock.log" || ! searched_then_streamed; then
   echo "e2e-ui failed: $frames frames; mock log:" >&2
   cat "$tmp/mock.log" >&2
   exit 1
 fi
-echo "e2e-ui ok: $frames frames rendered; Home -> album -> Play streamed the first track (null device)"
+echo "e2e-ui ok: $frames frames rendered; the feed and Search both played (null device)"
diff --git a/tools/mocksubsonic/main.go b/tools/mocksubsonic/main.go
index c46d300..f25b176 100644
--- a/tools/mocksubsonic/main.go
+++ b/tools/mocksubsonic/main.go
@@ -53,11 +53,13 @@ func main() {
 		log.Fatalf("no audio files in %s (%v)", *dir, err)
 	}
 	album := map[string]any{"id": "al-mock", "name": "Mock Album", "artist": "Mock Artist", "artistId": "ar-mock", "songCount": len(songs)}
+	artist := map[string]any{"id": "ar-mock", "name": "Mock Artist", "albumCount": 1}
+	playlist := map[string]any{"id": "pl-mock", "name": "Mock Playlist", "owner": "test", "songCount": len(songs)}
 
 	http.HandleFunc("/rest/", func(w http.ResponseWriter, r *http.Request) {
 		endpoint := strings.TrimSuffix(path.Base(r.URL.Path), ".view")
 		q := r.URL.Query()
-		log.Printf("%s %s format=%q range=%q", endpoint, q.Get("id"), q.Get("format"), r.Header.Get("Range"))
+		log.Printf("%s %s format=%q range=%q query=%q", endpoint, q.Get("id"), q.Get("format"), r.Header.Get("Range"), q.Get("query"))
 		switch endpoint {
 		case "stream":
 			i, err := strconv.Atoi(strings.TrimPrefix(q.Get("id"), "so-"))
@@ -90,9 +92,38 @@ func main() {
 		case "getAlbumList2":
 			reply(w, map[string]any{"albumList2": map[string]any{"album": []any{album}}})
 		case "getArtists":
-			reply(w, map[string]any{"artists": map[string]any{"index": []any{map[string]any{"name": "M", "artist": []any{map[string]any{"id": "ar-mock", "name": "Mock Artist", "albumCount": 1}}}}}})
+			reply(w, map[string]any{"artists": map[string]any{"index": []any{map[string]any{"name": "M", "artist": []any{artist}}}}})
+		case "getArtist":
+			a := map[string]any{"album": []any{album}}
+			for k, v := range artist {
+				a[k] = v
+			}
+			reply(w, map[string]any{"artist": a})
+		case "getGenres":
+			reply(w, map[string]any{"genres": map[string]any{"genre": []any{map[string]any{"value": "Test", "albumCount": 1, "songCount": len(songs)}}}})
+		case "getPlaylists":
+			reply(w, map[string]any{"playlists": map[string]any{"playlist": []any{playlist}}})
+		case "getPlaylist":
+			p := map[string]any{"entry": songs}
+			for k, v := range playlist {
+				p[k] = v
+			}
+			reply(w, map[string]any{"playlist": p})
+		case "getStarred2":
+			reply(w, map[string]any{"starred2": map[string]any{}})
 		case "search3":
-			reply(w, map[string]any{"searchResult3": map[string]any{"song": songs}})
+			// Everything matches; only the first page has results.
+			res := map[string]any{}
+			if q.Get("artistOffset") == "0" && q.Get("artistCount") != "0" {
+				res["artist"] = []any{artist}
+			}
+			if q.Get("albumOffset") == "0" && q.Get("albumCount") != "0" {
+				res["album"] = []any{album}
+			}
+			if q.Get("songOffset") == "0" && q.Get("songCount") != "0" {
+				res["song"] = songs
+			}
+			reply(w, map[string]any{"searchResult3": res})
 		case "getPlayQueue":
 			reply(w, nil)
 		default:
```

And the benchmark cases:

Save this patch as `/tmp/t10-test.patch` and apply it from the repository root with `git apply /tmp/t10-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 9):

```diff
diff --git a/internal/ui/bench_test.go b/internal/ui/bench_test.go
index 1c3d6c6..341a943 100644
--- a/internal/ui/bench_test.go
+++ b/internal/ui/bench_test.go
@@ -22,6 +22,8 @@ func BenchmarkRepaint(b *testing.B) {
 	}{
 		{"albums-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
 		{"nowplaying-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { playingState(ta); return NewNowPlayingScreen() }},
+		{"feed-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return newSidebarRoot() }},
+		{"search-hdmi-1080p", ProfileHDMI, 1920, 1080, func(ta *testApp) Screen { return NewSearchScreen() }},
 		{"albums-crt-240p", ProfileCRT240, 640, 240, func(ta *testApp) Screen { return NewAlbumListScreen("Recently added", "newest") }},
 	}
 	for _, c := range cases {
```

- [ ] **Step 2: Run everything**

Run:

```bash
go vet ./... && go test -race -count=1 ./...
make e2e
make mister
```

Expected:
- All packages `ok`.
- `e2e ok: 4 tracks …` and `e2e-ui ok: … frames rendered; the feed and Search both played (null device)`.
- `bin/arm/mistersubsonic: needs glibc 2.29 (<= 2.31) ok`.

If `make mister` can't find zig, put zig 0.16.0 on the PATH; the build uses `zig cc`.

- [ ] **Step 3: Check that the e2e fails when it should**

The new check reads the mock log, so make sure it can fail. Temporarily delete the final `,a` from the `-keys` line in `scripts/e2e-ui.sh`, so the search results are never played, and run `make e2e`.

Expected: `e2e-ui failed: …` with the mock log printed. Restore the line and run `make e2e` again: `e2e-ui ok`.

- [ ] **Step 4: Silent check against the real server**

This step needs the user's server. It is silent, headless and uses `-null`.
1. Build a temporary config outside the repo from `.env` (`NAVIDROME_URL`, `NAVIDROME_USERNAME`, `NAVIDROME_TOKEN`, `NAVIDROME_SALT`), readable only by the user (`umask 077`).
2. Run:

   ```bash
   bin/mistersubsonic -config /tmp/…/config.toml -display headless -frames /tmp/…/frames -null \
     -keys "left:3s,down,down,down,down,down,right,'a:300ms,right:2500ms" -exit-after 10s
   ```
3. Look at the last frames. They should show the Search section with real results.
4. **Delete the frames and the config** (they show the user's library and hold credentials).
5. Never print the config.

- [ ] **Step 5: Commit**

```bash
git add README.md cmd/mistersubsonic/main.go internal/ui/bench_test.go scripts/e2e-ui.sh tools/mocksubsonic/main.go
git commit -m "app: root screen and insecure badge; mock endpoints; e2e searches; README controls" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 11: Docs and the on-device check (when the MiSTer is available)

**Files:**
- Modify: `docs/spikes.md`, `docs/superpowers/plan-2a-followups.md`

- [ ] **Step 1: Record what this plan resolved**

Append to `docs/superpowers/plan-2a-followups.md`:

```markdown

## Resolved by Plan 2b

- Queue edge cases:
  - Removing the playing song while paused keeps it paused.
  - Removing the last one stops with nothing selected, or wraps under repeat-all.
  - Empty PlayNext/Enqueue are ignored.
- Seeking while a track opens lands where asked, including the resume-offset race.
- Unpausing no longer flashes Buffering.
- A failed prefetch shows one toast, not two.
- `q` opens the queue.
- Now Playing shows volume (Up/Down), star state (X) and the insecure badge. The CRT cover is beside the text.
- Queue X opens Remove / Clear queue.
- Select on album lists, feed rows and artists shuffles a sample of up to 10 albums.
- CRT title-safe area: 12 lines top and bottom, 16 px sides. Tune it on a real CRT (below).
- Dev viewer:
  - IP-literal Host headers work, so `:8090` binds are usable.
  - It warns when open to the network.
  - The tests cover the text keys.

Still open from the lists above: the Plan 3 items, the input nits, the art and cache nits, and the HomeScreen (CRT) focus shift when the Resume row arrives.
```

- [ ] **Step 2: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, add this to the end of `docs/spikes.md`, commit, and stop:

```markdown

## Plan 2b on the MiSTer

pending — MiSTer unavailable. Still to do:
- Run `ui.test -test.bench Repaint` on the device. The new cases are feed-hdmi-1080p and search-hdmi-1080p.
- Tune the CRT title-safe margins on a real CRT.
- Have the user try the sidebar, grids and search with the controller and a USB keyboard.
```

- [ ] **Step 3: If the MiSTer answers — benchmark (silent)**

Run `make deploy-dev MISTER=<address>`, then on the device:

```bash
cd /media/fat/mistersubsonic/dev && ./ui.test -test.run '^$' -test.bench Repaint -test.benchtime 20x
```

Record ms/frame for each case under `## Plan 2b on the MiSTer`.

Decision rules:
- **`albums-hdmi-1080p` and `feed-hdmi-1080p` ≤ 30 ms:** write "§8.1 confirmed for grids".
- **Above 30 ms:** write the numbers down and tell the user. The fixes, which belong in Plan 3, are:
  - dirty-rectangle presents
  - no full redraw for the marquee (redraw only its row)
  - `fb_size=2`

- [ ] **Step 4: If the MiSTer answers — on the TV (ask the user first)**

Ask the user to run the app on the TV from SSH. Pressing Start plays sound, so they should keep the volume low. Ask them to report:
- Do the sidebar, the cover grids and the Albums tabs work with their controller?
- Does Search type with a USB keyboard, including N, Q and Space?
- On a CRT: is anything cut off at the top, bottom or sides?
- Does the marquee scroll smoothly, and is the menu still responsive while it runs?

Record the answers.

- [ ] **Step 5: Commit**

```bash
git add docs/spikes.md docs/superpowers/plan-2a-followups.md
git commit -m "docs: record Plan 2b results and follow-ups" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

- **Plan 2c:**
  - the setup wizard, with the same keyboard, `http://`/`https://` presets and `:4533`
  - Settings: servers and switching (recreate the client, player and art loader), playback (volume saved to the config), display, about, Exit
  - the screensaver
  - the "server unreachable" screen with Retry · Switch server · Settings
  - the invalid-config wizard offer
  - draining input after the wizard
- **Plan 3:** the launcher and everything on the device, plus the remaining Plan 3 follow-ups. If Task 11's benchmark exceeds 30 ms, the render-cost fixes come first.
