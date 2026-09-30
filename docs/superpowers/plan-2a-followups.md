# Plan 2a follow-ups

Items the Plan 2a reviews triaged as "fix later". Plan 2a itself is complete and reviewed (branch `plan-2a-tv-ui`). The remaining items in `plan-1-followups.md` still apply.

## Resolved by Plan 2a (from plan-1-followups.md)

- **Seeking:** coalesced in the engine, per track. The UI throttles held seeks with a pending target, sending one every 250 ms plus one on release.
- **Shuffle next index:** `State.NextIndex` gives the "Next: …" line.
- **Seek of a gapless successor dropping Started:** `doSeek` announces a pending boundary.

## Still owed on the MiSTer (Task 10 of Plan 2a)

- **Benchmark:** run `ui.test -test.bench Repaint` on the device. If `albums-hdmi-1080p` takes more than 30 ms per frame, Plan 2b starts with the render-cost fixes.
- **TV check:** HDMI and CRT screen fill, the controller with the user's MiSTer mapping, the B-hold exit, and covers.
- **CRT title-safe margin (spec §8.2, about 5%):** in the 320×240 layout the header sits at about y=4 and the mini-bar at about y=222–236, both inside the overscan band. Add a vertical safe inset to the CRT profile and tune it on a real CRT.

## Plan 2b must handle

- **Queue edge cases**, reachable now through Queue X:
  - Removing the current track while paused starts playback.
  - Removing the current track when it is last leaves `Index` on the previous song.
- **Spec §8 gaps:**
  - The keyboard's `q` should open the Queue, not Now Playing.
  - Now Playing has no volume indicator, star state or insecure-TLS badge.
  - Queue X should open a Remove/Clear menu.
  - Select on album lists should shuffle-play.
- **devview:**
  - A `0.0.0.0` or `:port` bind gets 403 for every request. Accept IP-literal Host headers, since rebinding always uses a hostname, and print a loopback URL.
  - Warn when bound to a non-loopback address.
  - Tests are missing for channel-full drop, the cancel path, and an immediate return when seq > after.
- **Seek during Loading:** a seek sent before `Play` reaches the engine fails with ErrNotCurrent, and the resulting `EventSeekFailed` triggers a spurious `reopenAt(seekTarget)`.
- **Duration 0:** a held seek has no upper clamp.
- **Text:**
  - `Marquee` returns "" past one period.
  - Tofu boxes are drawn for zero-width and C1 control characters.
- **Layout:**
  - The CRT Now Playing art size can go negative with larger fonts.
  - The Now Playing text block has no bottom guard.
- **Home:** focus shifts when the Resume row arrives.
- **Input:**
  - A non-repeating button press stops a held direction's repeat.
  - Releasing the newer nav button doesn't resume the older one.
- **Art:** the failed-key map is unbounded; revisit it with cover grids.
- **Tests:**
  - a `DefaultKeys` table test
  - art LIFO order, move-to-top, Close during a blocked fetch, the 16 MB cap, reopen with a stray .tmp
  - gfx at the 288/289 boundary, alpha over non-black, marquee wrap, the fallback font dir
  - e2e-ui asserting the navigation path, not just stream and frame counts
  - `TestExitDuringConnectBuildsNothing` never reaches the "build after closing" branch

## Plan 3 must handle

- **Memory:** `MaxDecodePixels` 4096² allows about 128 MB of transient memory per cover decode, times 2 workers. Use 2048².
- **Performance:** `gfx.FromImage` uses per-pixel `At`. Add YCbCr and RGBA fast paths.
- **Framebuffer:**
  - x/yoffset panning is ignored.
  - The `/dev/mem` offset isn't page-aligned (fallback path).
  - `smem_len` and `OpenFB` have no tests.
- **Input:**
  - Default keys fire for codes the user's map omits. Check with real maps.
  - On an EBUSY grab the device is still read. Needs checking alongside Main_MiSTer and the launcher.
- **Disk cache:**
  - Size drifts if files are deleted externally.
  - A partial `.tmp` is left behind when WriteFile fails.
- **`gfx.Resize`:** the uint32 accumulators overflow for boxes over 66k pixels. This can't happen at current sizes.

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
- Album lists: a failed next page shows a toast and a new press retries it; Artists R at the last letter stays put.
- Search: a stale page is dropped, errors show over existing results, a failed page retries, editing clears an old error.

Still open from the lists above: the Plan 3 items, the input nits, the art and cache nits, and the HomeScreen (CRT) focus shift when the Resume row arrives.

## Plan 2c must handle (from the Plan 2b reviews)

- **CRT Home keeps its Resume row after playback starts.** A on it replaces the live queue with the old saved one. The HDMI feed hides its card once a queue exists; do the same in `HomeScreen`.
- **Server switching:**
  - Load closures read `a.Library()`/`a.Player()` on worker goroutines. Capture them on the UI goroutine, as `withSongs` does, before the client can change.
  - Reset `App.artists`, `App.stars` and `starGen` on a switch.
  - `startSession` ignores `ActiveServer`'s ok.
- **Starred freshness:**
  - Returning to Starred by B (a pop) doesn't reload it after a star change.
  - A `toggleStar` whose screen was popped before the server answered is lost locally (the server has it).
- **Marquee:** a focused tab row still scrolls the child list's long title. Only one marquee slot exists.
- **Input and UX nits:**
  - `toggleStar` double press before the reply.
  - Grid R loses the column on a short last row.
  - The queue menu is titled with the song while Clear acts on the queue.
  - `albumsSongs` is silent on a partial failure.
  - A held key at a failing search list's end re-requests once per round trip.
  - The CRT playlist page shows one track under Play/Shuffle.
- **Tests and docs:**
  - The every-button sweep never runs the root at depth 1, with resume and the clock advanced.
  - Test gaps listed in the Plan 2b reviews: openSeek reset, Shift leaves Button unchanged, tab child load after pop, playlist and search edge cases.
  - The e2e awk check and timing are loose.
  - The README controls are terser than the plan's table.
- **Plan 3:** `drawField` trims by rune.

## Resolved by Plan 2c

- CRT Home drops its Resume row once something plays.
- Server switching:
  - Loads capture the library and player on the UI goroutine.
  - The artist and star caches reset on every connection.
  - The session manager replaces sessions safely: the newest request wins, and old players are stopped with their queue saved.
- Starred reloads when shown again after a Pop.
- A star request survives leaving its screen, and a double press is ignored while one is in flight.
- `config.Save` no longer names the missing `config.example.toml`.
- `startSession` ignoring `ActiveServer`'s ok: the new session manager checks it.

Still open: the Plan 3 items, the marquee and tab-row nit, the input and UX nits, and the test gaps listed above.

Also still open: ReplayGain changes take effect from the track after the prefetched one (the next track is queued with the old gain); Plan 3 could re-queue it.

## Plan 2c minors (deferred)

- Screensaver:
  - toasts (player errors) are hidden while it's on and don't wake it;
  - it can start over the exit prompt on a root Now Playing;
  - the waking press's Release isn't swallowed.
- Settings:
  - `cycle()` starts from the first choice for a hand-edited value (bitrate 160);
  - switching to a default that is still connecting re-dials it.
- Wizard:
  - a config backup whose wizard was left during the save leaves no new file (the next start runs the wizard again);
  - `normalizeURL` keeps `?query` and `#fragment`.
- Sessions:
  - `serverDir` names can collide after sanitising (`a/b` vs `a_b`; names that differ only in case, on FAT);
  - the `MkdirAll` error is ignored;
  - folders of removed servers stay.
- Stars: the `starBusy` reset lives in `Connected` (`Detach` would be the safer home); `popTo` calls `Shown` on intermediate screens; a Starred list in error retries on every Pop.
- `HomeScreen.sync` mutates state from `Draw` and doesn't adjust the list's scroll offset.
- The keyboard's `clear` helper shadows the builtin.
- `-keys` text items can't contain a comma or edge spaces.
- Every config save re-encodes the file, so the comments of a hand-edited `config.toml` are dropped.
- Test gaps:
  - AddServer with a third name collision;
  - Enter as A in Search;
  - that consent is kept when the same address is retyped in another form;
  - the screensaver waking on a typed rune or a toast;
  - Connected clearing `starBusy`;
  - `playKeys`;
  - the wizard keyboard-fit test uses the safe area rather than Draw's area, and doesn't cover the API-key step or a mini bar;
  - the config flush race test is probabilistic.

## Resolved by Plan 3a

- The framebuffer follows the console's pan offset (x/yoffset). The `/dev/mem` fallback maps from a page boundary. `smem_len` is checked against the panned screen, and the layout has tests.
- Clean shutdown restores `KD_TEXT` (plan 1): on exit, on SIGINT/SIGTERM and after a recovered panic. The launcher also runs `mistersubsonic -restore-console` after every exit, and a clean-up that takes more than 10 s is cut short.
- `config.example.toml` ships, and the saved config's header points to it.
- The mute toggle (spec §6).

## Plan 3b must handle

Everything under "Plan 3 must handle" above, plus the Plan 3 items in `plan-1-followups.md`, except what "Resolved by Plan 3a" lists (the framebuffer panning, `/dev/mem` alignment and `smem_len` check, `config.example.toml`, and restoring `KD_TEXT`). `OpenFB` itself still has no test.
- memory: stream rings, cover decode size
- raw MP3 seek
- the device guards
- the clean-shutdown internals of the engine
- stream hardening
- the mock server's `timeOffset`

And also:
- **Input:** check that default keys fire only for codes a user's map omits, and that devices whose grab returns EBUSY are handled, alongside Main_MiSTer on the device.
- **ReplayGain:** re-queue the prefetched track on a change.
- **Performance:** whatever the device benchmarks and spikes 2 and 3 call for.

## Plan 3a minors (deferred)

- **Log:**
  - After a failed rename, every later line rotates the log and it can pass 2× the cap.
  - A failed reopen quietly ends logging.
  - A failed open of `crash.txt` is silent. An oversized one is deleted, not rotated.
- **Framebuffer:**
  - `fbLayout`'s end overstates by `xoffset` bytes.
  - The 32-bit int maths isn't range-checked.
  - `OpenFB`'s slicing has no test.
- **Mute and Settings:** holding Left/Right on a toggle row (Mute, Scrobbling) flips it on every repeat.
- **Shutdown:** errors from `con.Restore` and `forceExit`'s `RestoreText` aren't logged. The final `error:` line comes after "exiting".
- **Example config:**
  - The test's key match is loose.
  - It doesn't say to edit url/username/password first.
  - The `allow_plaintext_password` comment should say it only matters for http://.
- **Launcher:**
  - TERM to the launcher alone waits for the app to exit, then exits 130.
  - An INT during the explicit restore skips the rest of it.
  - A failed `exec 9>` isn't checked.
  - The harness's socat stand-in doesn't check the missing newline or the timeout.
  - The locked case's `flock … sleep 30` leaves its `sleep` running after cleanup.
  - No case covers a missing SAM script.
- **Releases:**
  - `mkdb` doesn't normalize `-base-url` or guard `-o` inside `-dir` or non-regular files.
  - With no tags, `VERSION` is a bare hash.
  - `check-notices.sh` passes when `go list` fails (no `pipefail`) and checks the host dependency graph, not the ARM one.
- **Docs:** the README's Install section doesn't list `config.example.toml`, `LICENSE` and `THIRD_PARTY.txt`.
- **To check on the device:** `flock --help` and `bash --version`; `pidof`/`ps` with SAM running; what Main_MiSTer sends a script it cancels; `RestoreText` on `/dev/tty0` if Main_MiSTer switched terminals; whether Downloader accepts `"v": 1` and the `mistersubsonic/` folder.

## Resolved by Plan 3b

- **Memory:**
  - A prefetching stream (the queued next track) holds a ring of 1.25× its prefetch (5 MiB) until it plays, then grows to the full window.
  - Covers are scaled straight from the decoder's output, so the full-size picture isn't converted.
  - A 48 MiB decode budget replaces the 4096² pixel cap. A 4096² baseline JPEG still decodes (progressive and CMYK cost more and are refused sooner), but not an RGBA PNG of that size.
- **Covers:**
  - `FromImage` reads YCbCr, RGBA, NRGBA and Gray directly.
  - `Resize` sums are 64-bit.
- **Raw MP3:** seeking and resuming reopen the stream at a byte estimate (past the ID3v2 tag, in proportion to time), not a decode from the start. MP3s of unknown size still seek through the decoder.
- **Device:**
  - Calls after close are no-ops.
  - The end of a track keeps feeding the device while its successor opens.
  - Steady-state decoding doesn't allocate: the pending buffer is reused, and the resampler's counters no longer escape.
- **Clean shutdown:**
  - Openers that finish after Close close what they opened.
  - Sources of commands still queued at Close are closed.
  - `Events()` closes, and the player stops reading a closed stream.
- **Stream:**
  - A 416 before the end is an error.
  - 408, 429 and 502–504 are retried, honouring Retry-After, on the first request too.
  - The open timeout (`StallTimeout`) is documented.
- **Disk cache:**
  - Stray `.tmp` files are removed at open and after a failed write.
  - The eviction walk recounts the size.
- **ReplayGain:** changes apply at once: to the playing track, the queued one, and one still opening.
- **Mock server:** honours `timeOffset` for MP3.

Still open:
- On the device: spikes 2 and 3, the `Repaint` benchmarks, input with real maps and grabs next to Main_MiSTer, and the CRT margins (see `docs/spikes.md` and `docs/testing-on-mister.md`).
- `drawField` trims by rune.
- The minor lists above.
- A sized MP3 reopens its stream on every seek, even inside the buffered window (a few HTTP requests; still far faster than dr_mp3 decoding from byte 0). Reuse the reader when the estimate falls inside its window.
- VBR MP3 seeks land by proportion; reading the Xing TOC (100 points) after the ID3v2 tag would make them accurate.

## Plan 3b minors (deferred)

- **Covers:**
  - A JPEG whose marker walk fails (stray bytes before SOF) falls back to the weaker 3 B/px estimate. Only crafted files do this.
  - Adobe RGB (APP14 transform 0) isn't counted.
  - The RGBA fast path's fully transparent, opaque and clamp branches, and the CMYK, Gray16 and NRGBA64 fallbacks, have no tests.
  - The per-pixel closure costs about 0.2–0.5 s for a 2000² cover on the A9. A row loop would be faster.
- **Disk cache:**
  - `evictLocked` zeroes the size before the walk, so a failed walk leaves 0.
  - Errors removing stray `.tmp` files are ignored.
  - There is no test for a rename failure.
- **Stream:**
  - `Promote` allocates the 32 MiB ring under the lock (about 50–80 ms on the A9, against a 500 ms device ring), and still allocates on a closed reader.
  - `parseRetryAfter` overflows for absurd values.
  - The 416 check uses the first response's size.
  - The Open deadline check covers the wait but not the retry request.
  - There are no tests for Open's budget running out, a ctx cancel during Open's sleep, or a 500 failing Open at once.
- **Audio:**
  - `breakChain`'s Flush can grow `pending` without updating `pendBuf`: one allocation per track break.
  - `g_open` is a plain int. That is fine for the current shutdown order; an atomic would be cheap.
  - `Close`'s comment says `Events()` closes afterwards, but it closes asynchronously.
  - The gapless test's gate isn't closed in a defer.
- **Player:**
  - No test covers a ReplayGain change while the prefetch is still opening. It is correct by construction.
  - In an MP3 byte-estimate open, the Seek failure paths don't rewind after the header read (practically unreachable).
  - `fromOffset` allows seeks before its base.
  - No opener-level test covers the estimate-failure fallback.

## Plan 4 minors (deferred)

- **Build:**
  - `bin/arm/tests/` is only removed by `make clean`, so a test binary for a deleted package can linger.
  - `mister-test` repeats the list of copied binaries in the `cp` and `check-glibc.sh` lines.
- **Render:**
  - The 32 bpp pack copies the canvas's top byte into X. The canvas keeps `0x00RRGGBB`, and XRGB ignores X.
  - `fakeArtCache` is shared by every ui test (images are never mutated today).
- **Media keys:**
  - The dev viewer's help line doesn't list the media keys.
  - `evmap.go`'s media-key constants sit between `keyPageUp` and the arrow keys.
- **Volume panel:**
  - Muted, the bar keeps the level lit in grey.
  - `setVolume` while muted calls `showVolume` twice.
  - There is no test for `drawVolumeInline`'s too-narrow return.
  - On CRT (320×240) the panel partly covers the value of Settings → Playback → Volume while it shows.
- **Screenshots:**
  - The free-name check and the `Create` are not atomic across processes (`O_EXCL` would close it; the app saves one at a time).
  - The failure test ignores its `os.WriteFile` error.
  - A press during a save is dropped without a word.
- **Docs:**
  - The README names Alt+Scroll Lock; Scroll Lock alone works too.
  - The README's screenshot folder is the MiSTer one; elsewhere it is `screenshots/` next to the config, or `-screenshots`.

## Plan 4b minors (deferred)

- **Input:**
  - `Manager.pads` isn't cleared on `Close`, so `HasPad` can stay true after `Close`; this has no effect in practice.
  - `isGamepad` treats a failed EV_KEY query as a keyboard.
- **Partial redraws:**
  - Each damaged rectangle reruns the whole `drawFrame` logic (at most 4 passes). This was measured and is cheap.
  - `HomeScreen.Handle` syncs before `list.Handle`. If the sync drops the Resume row, the two-row damage would miss the shift. This is thought unreachable.
  - While playing, a screen with no tick regions (no mini bar, or an unknown duration) redraws in full at every tick.
  - The verify pass reads the player's state separately from the partial passes. While music plays, `-verify-redraw` can rarely log a one-pixel difference in the progress line. A per-frame state snapshot, like the per-frame clock, would close it.
  - Missing tests:
    - the CRT scaler's full-frame fallback;
    - `Damage` clipped to the canvas;
    - damage plus dirty;
    - Now Playing's exact volume keys;
    - header damage on a title change.
- **Full resolution:**
  - An unreadable `res_count` makes every switch time out and flip back. It could fail fast.
  - `forceExit` skips the size restore, which the launcher's `-restore-console` covers.
  - A stale `/tmp` state from a crashed run is applied at the next exit.
  - `Restore` forgets the saved size even when the request fails.
  - `openFB`'s fallback branches aren't tested. That would need an injectable framebuffer opener.
- **Hints:**
  - The sidebar root and the first-run wizard handle B without hinting Back.
  - The bar under the exit prompt still shows the screen's hints, but it is covered by the overlay.
  - `NowPlayingScreen.Hints` checks for a nil player only once.
  - `SidebarRoot.Typing` has no direct test.
  - The fixtures omit the message, unreachable, feed-with-Resume and search-results screens.
  - The pixel-change check can't judge whether a label is right.
  - On a physical keyboard, the typing hints read "Enter Type" and "Tab Delete", with no "Done". They are true but unidiomatic.

## Resolved by Plan 5

- **Settings and screensaver:**
  - On/Off rows no longer flip on key repeat.
  - Hand-edited values continue to the nearest choice.
  - A toast wakes the screensaver.
  - No screensaver over the exit prompt.
  - The waking press's Release is swallowed.
- **UI text:**
  - The queue menu is titled "Queue".
  - Grid R keeps the column.
  - A partial album-load failure is announced.
  - Keyboard typing hints and Back hints are right.
  - `normalizeURL` drops `?query` and `#fragment`.
  - A screenshot press during a save says so.
  - The README's screenshot text and the example config's first-run text are fixed.
  - The tab-row marquee is fixed.
  - The CRT playlist rows are fixed.
- **MP3:** seeks inside the buffered window reuse the stream and keep the queued track, and VBR seeks use the Xing/Info TOC.
  - A seek that lands during a gapless handover is refused by the engine (`Replace` checks the track it replaces), and the player reopens instead.
  - `stream.Reader` no longer counts on bytes a fetch in flight may overwrite.
- **Stream and player:**
  - `Promote` allocates outside the lock.
  - Retry-After is clamped.
  - 416 and short-EOF use the latest size.
  - Open is bounded by the budget.
  - `fromOffset` refuses seeks before its base.
  - The MP3 estimate's failed rewind is an error.
  - The missing Open and opener tests are added.
- **Full resolution and input:**
  - An unreadable `res_count` fails fast.
  - The state file holds both sizes, and a stale state is ignored.
  - The state is kept when a restore fails.
  - `forceExit` restores the size.
  - The forced exit and the normal exit restore share one guard, so the framebuffer size never changes in text mode.
  - A stale saved size is logged.
  - `pads` is cleared on Close.
  - A failed EVIOCGBIT is logged.
- **Logs, launcher and tools:**
  - The log backs off after a failed rotation.
  - The launcher forwards TERM and INT and finishes its restore.
  - After a forwarded signal, the launcher reports the app's own exit code.
  - The lock fd is checked.
  - `check-notices.sh` uses pipefail and the ARM graph.
  - `mkdb` guards its flags.
  - `mister-test` clears `bin/arm/tests`.
- **Per-server data:**
  - Folder names are unique per server, and old folders are kept.
  - An old folder belongs to the first server that claims it (`.server` marker).
  - The cache size survives a failed walk.
  - The art failure map is bounded.
  - The wizard can't be left mid-save.
  - `popTo` shows only its target.
  - Starred retries only on A.
- **Config and covers:**
  - Saving keeps comments, blank lines and unknown keys.
  - An edit that trips over the file falls back to a full rewrite, and a BOM before the first key is kept.
  - Cover scaling is 30% faster on the A9 (see `docs/spikes.md`).
