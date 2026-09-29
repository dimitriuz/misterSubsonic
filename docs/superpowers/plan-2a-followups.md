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
