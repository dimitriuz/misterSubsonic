# MiSTer Subsonic — backlog of future plans

Work planned after Plan 3b. Each entry becomes a plan of its own: brainstorm, then a spec addendum where the feature is new, then a plan written from a verified prototype like Plans 1–3b.

Suggested order:
1. The device plan (spikes 2 and 3, benchmarks, the checklist), once the MiSTer is back.
2. The first release: make the repository public, then tag `v1.0.0`.
3. Then the entries below, in any order.

---

## A. Small follow-ups

Plans 5 and 5b did most of this list; see "Resolved by Plan 5" and "Resolved by Plan 5b" in `docs/superpowers/plan-2a-followups.md`. What remains is in "Plan 5b leftovers" there. Still open here:

- **Faster list scrolling at full resolution.** Plan 4b made focus moves, the tick and the marquee partial (≤ 12 ms at 1920×1200). A scroll step still redraws the list area, which takes about 40 ms. The A9 is memory-bound: shifting the pixels measured slower than redrawing them. The next step is fewer bytes per frame, not a smarter redraw, and it needs measuring on the device.

(Faster JPEG decoding is no longer open: covers are already requested at display size, `getCoverArt` with `size=` in `internal/art/art.go`, so the 0.77 s decode of a 2000×2000 cover applies only to servers that ignore `size=`.)

---

## B. Visualizer

**What:** an optional visualizer on Now Playing: spectrum bars and/or an oscilloscope drawn from the music as it is heard. Spec §2 left it out of v1, and §13 lists it as a candidate.

**Sketch:**
- **Where the audio comes from:** the engine's output, after gain, at 48 kHz stereo. Copy a small window of it into a lock-free buffer the UI can read.
- **Keeping it in time with the sound:** the device ring holds about 500 ms, so choose the window by the device's `Consumed()` position, not by what was last decoded.
- **Analysis:**
  - Spectrum: a 1024-point FFT in pure Go, with log-spaced bands, peak hold and a little smoothing.
  - Oscilloscope: the waveform, decimated to the screen width.
- **Drawing:**
  - It is redrawn by the UI goroutine at a fixed cadence, 20–30 fps on HDMI and less on CRT, using the existing dirty-rectangle presents.
  - While it runs, the progress tick is folded into its frame.
- **Layouts:**
  - HDMI: bars under the cover, or full screen in place of the cover.
  - CRT: a narrower band that respects the title-safe area.
- **Settings:** Display → Visualizer: Off, Bars, Scope. Off is the default. The screensaver takes over after its idle time as now, or the visualizer could replace the screensaver on Now Playing (open question).

**Open questions:**
- Which styles, and full screen or a panel?
- The CPU budget on the Cortex-A9. Spike 3's numbers are needed first:
  - decode and resample must stay under 25% of one core;
  - a UI repaint must stay under 30 ms;
  - the visualizer must not cause underruns.
- Does it replace the screensaver on Now Playing, or sit alongside it?

**Needs:** spike 3 and the render benchmarks from the device plan. It must be tried on the TV to judge.

**Size:** about 6 tasks after a short spec addendum.

---

## C. Web remote (desktop and mobile browsers)

**What:** control the MiSTer from a phone or a computer on the same network. You see what's playing with its cover, and can play, pause, skip, seek, set the volume and mute, look at and edit the queue, and browse, search and play. It is a control page, not a copy of the TV screen (the dev viewer already does that). Spec §2 left a web or phone remote out of v1, and §13 lists it.

**Sketch:**
- **Server:** a new `internal/remote` package. It is an HTTP server inside the app, reusing the dev viewer's safeguards:
  - Host-header checks against DNS rebinding;
  - a loopback-first bind;
  - a warning when it is reachable from the network.
- **Threading:** every action goes through the UI goroutine (`App.Post`), so the app's single-goroutine model holds and the TV and the remote never disagree.
- **Live updates:** Server-Sent Events carrying the player's state and queue changes. Covers come from the art cache at a phone-friendly size.
- **The page:** one responsive page (phone and desktop) with the static files embedded. No framework is needed. Tabs: Now Playing, Queue, Browse (artists, albums, playlists, starred), Search.
- **Security:**
  - Off by default: Settings → Remote → On.
  - Pairing: the TV shows a short code; the browser enters it once and gets a token, kept in a cookie.
  - LAN only: no internet exposure, and no server credentials ever sent to the browser.
  - Plain http on the LAN, so treat the code as the only protection and say so in the README.
- **Config:** `[remote]` gets `enabled`, `port` (default 8080) and the paired tokens (revocable in Settings).

**Open questions:**
- Which features the first version needs (probably transport, volume, the queue and search), and which can wait (starring, playlist views).
- The pairing flow on a CRT.
- Whether several paired devices are allowed at once.
- Whether to show a small "remote connected" badge on the TV.

**Needs:** nothing on the device. It can be built and tested on the desktop against the viewer and the mock server, then checked on the MiSTer as part of the device plan.

**Size:** about 8–10 tasks after a spec addendum (API, pairing, page).
