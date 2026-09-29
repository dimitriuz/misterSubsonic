# Plan 1 follow-ups

Items the Plan 1 reviews triaged as "fix later". Plans 2 and 3 should pick these up. Plan 1 itself is complete and reviewed (branch `plan-1-playback-core`).

## Still owed: on-device and listening checks (MiSTer was unavailable)

- **Spike 2:** toolchain + ALSA on the MiSTer. Run `make deploy-dev`, run the silent `audio.test` checks, then the opt-in quiet listening test. The listening test runs only with the user's go-ahead.
- **Spike 3 (audio):** Cortex-A9 decode + resample cost, run with `audio.test -test.bench DecodeResample`. Its decision rules are in plan Task 3.
- **Task 11 Step 7:** `mss-cli` against the real Navidrome on the device, silent first, then audible at −30 dB with the user's go-ahead. Include a transcoded (non-FLAC/MP3) song this time.

## Plan 2 (UI) must handle

- **Seeking:** coalesce seeks (latest wins). Key-repeat seeking must not queue more than 64 engine commands behind a slow HTTP restart, or `player.do` blocks again.
- **Resume-offset race:** a user seek while a resume-offset open is still loading can reopen at the resume offset instead of the requested position (`seekTarget` is overwritten in `onOpened`).
- **Failure counting:** user actions (PlayNow/Next/Jump) should reset the consecutive-failure counter. Today only `Started` resets it.
- **Queue edge cases:**
  - Removing the current track when it is last stops playback even under repeat-all.
  - After removing the current track, `Index` points at the previous song.
  - Removing the current track while paused starts playback.
  - PlayNext/Enqueue with an empty list on an empty queue opens a zero Song.
- **Buffering flicker:** the status flickers to Buffering right after unpause, because `lastMove` isn't refreshed.
- **Toasts:**
  - A mid-track decode error while a prefetch is in flight isn't counted as a failure.
  - A failed prefetch that is retried as the current track gives two Error toasts.
  - A late successor timeout after handover skips the song instead of reopening it once.
- **A stale gapless successor** can be heard briefly if the queue changes in the last ~0.5 s of a track.
- **State gaps (additive):**
  - no shuffle play order or next index (needed for the "Next: …" line)
  - no "seek available" flag
  - no error status
  - no PositionTick event (the UI polls `State()`)
- **Seek at EOF:** a seek handled inside the engine's `decodeChunk` poll right at EOF is lost.
- **Scrobble queue:** a corrupt `scrobbles.json` is silently overwritten on the next Add. Log it at least. Also add a FIFO-eviction test.
- **Stream open race:** if the ctx is cancelled between `Do` returning and `stop()`, a Read blocks until Close.
- **Tests:**
  - `TestStateIsCurrentWhenEventArrives` builds 2000 harnesses (~5 s under -race). Reuse one harness instead.
  - `TestSeekWhileLoadingStillAnnounces` uses a fixed sleep; switch it to `waitFor`.
- **Test gaps:**
  - the 240 s scrobble cap
  - pause not counting as listened time
  - Remove before the cursor
  - PlayNext under shuffle
  - a tighter prefetch boundary
- **Engine lifecycle:** leftover open results after `Run` exits aren't closed. This matters if server switching recreates the player.

## Plan 3 (MiSTer integration) must handle

- **Raw MP3 seek:** dr_mp3 without a seek table decodes forward, which takes seconds on the A9. Estimate a byte offset for CBR files from `BitRate`/`Size` and reopen there.
- **Memory:** each stream reader allocates its full 32 MiB ring, including the 4 MiB-capped prefetch reader, so steady state is about 64 MiB against the spec's ~36 MB. Size prefetch rings small and grow them on Promote.
- **Device:**
  - Guard `mss_device_flush` after close with `g_open`.
  - `pending` isn't written while `finishCur` waits for a successor, so the ring can underrun, bounded to 10 s.
  - Measure the per-chunk allocations in the engine during spike 3.
- **Clean shutdown:**
  - Opener goroutines can block on `openCh` after Close.
  - Queued commands' sources are never closed.
  - `Events()` is never closed.
  - Spec §9 needs a clean exit to restore `KD_TEXT`.
- **Stream hardening:**
  - A 416 on reconnect when `off < size` is treated as EOF.
  - 429 is currently terminal.
  - Document that the effective open timeout is 10 s (the header timer).
- **Config:** ship `config.example.toml`, since `Save` already references it.
- **Mock server:** `serveTranscode` ignores `timeOffset`.

## Known intermittent failure (resolved in Plan 3b)

- On 2026-09-29, one full `go test -race ./...` run reported `FAIL mistersubsonic/internal/audio 0.863s`. The failure came early, which suggests an assertion or a race report rather than a timeout. The output was not captured.
- It did not reproduce in 20 further full-suite runs or 95 audio-package runs under CPU stress (`-cpu 1,2` alongside player/stream load).
- Suspects:
  - the device tests, which share the global miniaudio device (`TestNullDeviceConsumesAndFlushes`, `TestFlushAfterDeviceStopped`)
  - the timing of the new `openWait`/seek tests
- Next time it appears, keep the full `-v` log. CI runs with `-race` on every push, so it will show up there if it recurs.

Found in Plan 3b: `TestEngineGaplessResampledMatchesWholeFile` raced. Under load the short first track finished decoding before its successor's QueueNext ran, so the chain broke and the resampler flush added a frame (48002 vs 48000 samples). Test-only fix: the first track's reads wait until the successor is queued. The engine is right — the player queues successors 20 s early.
