# MiSTer Subsonic: USB DAC output

Addendum to `2026-09-28-mister-subsonic-design.md` (the main spec). The main spec fixes the output at 48 kHz on ALSA's default device (§6). This document adds USB DACs as a second kind of output: chosen in Settings, played at each song's own sample rate, with the DAC's hardware volume. Where the two differ, this one wins for the features below.

## 1. Why, and what success means

The user has a Topping E30 DAC on the MiSTer (2026-10-09) and wants the music to go through it, at the song's own rate, with no resampling. A first test with `alsa_device = "E30, USB Audio"` already played through the DAC, but at 48 kHz through ALSA's dmix mixer.

What the MiSTer reports for the E30:
- Card 1, ALSA id `E30`, "Topping E30 at usb-ffb40000.usb-1.2, high speed".
- `S32_LE` (32 and 24 bit), 2 channels, rates 44100 to 768000.
- Mixer: `E30 Playback Volume`, 0 to 127, −127 to 0 dB in 1 dB steps, and `E30 Playback Switch`.
- The kernel has `CONFIG_SND_USB_AUDIO=y`, and `libasound.so.2` is installed.
- MiSTer's own audio is ALSA `default`, a plug into `/dev/MrAudio` at 48 kHz, on the `Dummy` card 0.

**Success means:**
- Settings → Playback → Output switches between "MiSTer audio" and the connected DACs, while music plays.
- On the E30, a 44.1 kHz FLAC runs the device at 44.1 kHz, and a 96 kHz FLAC at 96 kHz, with no resampler in the path.
- Songs at the same rate stay gapless. A rate change costs a short silence, measured on the device, and never cuts the end of the previous song.
- The app's volume and mute drive the DAC's hardware volume, so the samples reach the DAC unchanged at any volume (bit-perfect with ReplayGain off).
- A missing or unplugged DAC never leaves the app silent and stuck: it falls back to MiSTer audio with a notice.
- MiSTer audio works exactly as before.

## 2. What the user sees

- **Settings → Playback → Output.** Left and Right step through "MiSTer audio" and each connected sound card that can play, by its name ("Topping E30"). The change applies at once and is saved.
- **The config:** the choice is saved in `[playback] alsa_device`.
  - `"default"` is MiSTer audio, as today.
  - A DAC is saved as `"hw:CARD=<id>,DEV=0"`, for example `"hw:CARD=E30,DEV=0"`. It names the card by its ALSA id, not its number, so it survives replugging and reboots.
  - Any other value keeps today's meaning: a device by name or ALSA id, at 48 kHz, with software volume. The Output row shows it as written until the user picks another.
- **A missing DAC at start:** the app plays through MiSTer audio and shows "Topping E30 isn't connected: using MiSTer audio". When the card is gone, its name comes from the config ("E30"). The config isn't changed. The Output row shows "MiSTer audio (E30 missing)".
- **A DAC unplugged while playing:** the app switches to MiSTer audio, continues the song from where it was heard, and shows "Topping E30 was disconnected: using MiSTer audio". It does not switch back by itself when the DAC returns; picking it in Settings does.
- **A failed switch** (the DAC won't open): the previous output stays, the setting isn't saved, and a notice says why.

## 3. Architecture

### 3.1 Listing outputs (`internal/audio`)

`audio.Cards() ([]Card, error)` reads `/proc/asound/cards` and keeps the cards that have a playback PCM (`/proc/asound/card<N>/pcm*p`). Cards whose driver is `Dummy` are skipped.

```go
type Card struct {
	Index int    // 1
	ID    string // "E30"
	Name  string // "Topping E30" (the long name up to " at ")
}
```

The Settings row lists "MiSTer audio" first, then each card. A card's device string is `"hw:CARD=" + ID + ",DEV=0"`. The parser takes the file's text, so tests feed it samples (the MiSTer's, a desktop's, empty).

### 3.2 The output (`internal/audio/device.*`)

The C device stays one global device, with one ring. These are the changes.

- **Two kinds of device, picked by the name:**
  - A name starting with `hw:` is a **native** device: the rate follows the music and the volume uses the hardware mixer when there is one.
  - Anything else (`""`, `default`, other names) is a **fixed** device: 48 kHz and software volume, as today.
- **Opening a native device:**
  - The name is passed to ALSA as is: miniaudio uses an id that doesn't start with `:` unchanged, so no dmix.
  - The format is the device's native one (`S32_LE` on the E30), and miniaudio converts from float. Float32 holds 16- and 24-bit samples exactly, so at unity gain the conversion changes no bits.
  - The rate is the one asked for. After init, the device's internal rate must equal it. If it doesn't, the open counts as "rate not supported".
- **Supported rates:** read from the device's native data formats (`ma_context_get_device_info`) at open, kept in the output, and returned by `Rates() []int`. A fixed device returns `[48000]`.
- **`Rate() int`** is the rate the device runs at now.
- **`Reopen(name string, rate int) error`** closes the device and opens `name` at `rate`.
  - The ring is re-made at `rate/2` frames (0.5 s, as today).
  - `Consumed` keeps counting across the reopen; it is never reset.
  - Pause and volume carry over.
- **The tail at close:** before a reopen for a rate change, the engine waits until the ring is empty (§3.3). Then the device waits for its own buffer to play out (period size × periods, from the device) before it stops, so the last frames are heard. miniaudio's stop drops what ALSA still holds.
- **Lost devices:** miniaudio's notification callback reports a stop the app didn't ask for (`ma_device_notification_type_stopped` without a pending stop). That sets a flag, read by `Lost() bool`. A failed reopen sets it too.
- **The `Output` interface** gains `Rate() int`, `Rates() []int`, `Reopen(name string, rate int) error` and `Lost() bool`. The fakes and the `Tap` implement them.

### 3.3 Sample rates in the engine (`internal/audio/engine.go`)

- **The target rate** for a voice of rate `r`:
  - `r`, when the output lists it in `Rates()`;
  - otherwise 48000, when listed;
  - otherwise the output's first rate.
  - A fixed device always gives 48000, so MiSTer audio behaves as today.
- **The resampler** runs from `r` to the target when they differ. Everywhere `OutputRate` is used today (chaining, `startVoice`, `doSeek`), the current device rate is used instead.
- **Gapless:** a successor whose target equals the device's rate chains exactly as today.
- **A rate change** happens when a voice's target differs from the device's rate:
  1. Break the chain: flush the resampler's tail into pending.
  2. Drain: keep writing pending, and wait until `Consumed() >= written`. The wait runs commands like `finishCur`'s wait does: a Stop, Play or Close ends it, and a pause just makes it longer.
  3. `Reopen(name, target)`. On error, the engine queues `EventDeviceLost` (below) and stops the voice. The app's fallback (§3.6) then takes over.
  4. Start the voice at the new rate.

  When nothing is buffered (the first song, after a Stop), the reopen happens at once.
- **The position clock:** `segment` gains `rate`. `Position` uses `s.rate`, not `OutputRate`. The frame counts stay continuous across reopens, so segments still line up.
- **Seek:** `doSeek` re-creates the resampler, when there is one, to the device's rate.
- **Device loss:** the monitor checks `Output.Lost()` every poll. When the device is lost, it queues `Event{Kind: EventDeviceLost}` (TrackID 0), once per loss.
- **Switching outputs:** `Engine.SwitchOutput(name string) error` runs on the run goroutine and returns its result to the caller.
  1. It notes the audible `(id, pos)` from `Position`.
  2. It flushes and calls `Reopen(name, target for the current voice)`. The rate is 48000 when nothing plays.
  3. If a voice is current, it seeks it to `pos`, without events. If the seek fails, decoding carries on from where it was: at most the buffered 0.5 s is skipped.

  If the reopen fails, it reopens the previous name and returns the error.
- `OutputRate` stays as the fixed device's rate. Its comment says so.

### 3.4 Hardware volume (`internal/audio/mixer.c`)

- **Loading:** `libasound.so.2` is loaded with `dlopen`, the same way miniaudio loads it, and the simple mixer functions are looked up: `snd_mixer_open`, `attach`, `selem_register`, `load`, `find_selem`/`first_elem`, `selem_get_playback_dB_range`, `selem_set_playback_dB_all`, `selem_get_playback_dB`, `selem_set_playback_switch_all` and `close`. The build needs no new libraries.
- **Choosing the control:** when a native device opens, the mixer of its card (`hw:CARD=<id>`) is opened. The first simple element with a playback volume that has a dB range is used. If there is none, or loading fails, volume stays in software and the log says so once.
- **Setting the volume:** `SetVolume(linear)` on a device with a hardware control:
  - `want = 20·log10(linear)` dB, clamped to the control's range.
  - The control is set to the nearest step at or above `want` (`dir = +1`). The device reads back the step it got.
  - Software gain = `10^((want − got)/20)`, always ≤ 1. On the E30, with 1 dB steps and the app's whole-dB volume, it is exactly 1.
- **Mute:** `linear == 0` turns the playback switch off, when there is one. Otherwise the control goes to its minimum and the software gain to 0. Any later non-zero volume turns the switch back on.
- **Timing:**
  - The volume is applied right after the device opens and before it starts, so the DAC never plays a moment at its old level.
  - Mixer calls run on the caller's goroutine, never in the audio callback.
  - Setting the volume can take a few ms, because it is a USB control transfer.
- **Fixed devices** keep software volume, unchanged.

### 3.5 The visualizer tap

- `Tap` sizes its ring for the device's rate: 0.68 s, the same `TapFrames` time as today. It is re-made, and cleared, on a reopen.
- `Window(dst) (n, rate int)` returns frames decimated to near 48 kHz:
  - factor `d = max(1, deviceRate / 48000)`, rounded down to 1, 2, 4 or 8;
  - each output frame is the mean of `d` frames;
  - `rate` is `deviceRate / d`.
- `viz.Analyzer` takes the rate with each update and rebuilds its band tables when it changes. Today it takes 48000 from `viz.Config.SampleRate`.

The bars and the waterfall look the same at 44.1, 48, 96 and 192 kHz. Only the picture is decimated, never the sound.

### 3.6 The app (`cmd/mistersubsonic`, `internal/ui`)

- **At start:**
  - The configured `alsa_device` opens.
  - If it is a `hw:` name and fails, `default` opens instead, and the UI gets a startup notice naming the card.
  - If `default` fails too, today's "No audio device" screen shows.
- **The Output row** lists outputs from `audio.Cards()` each time Settings → Playback opens. A change calls `Engine.SwitchOutput` off the UI goroutine. The result is posted back:
  - on success, the config is saved;
  - on failure, a notice shows and nothing is saved.
- **Device loss:**
  - The app learns of `EventDeviceLost` through the player, which passes it on as it does other engine events.
  - The app then calls `SwitchOutput("default")` and shows the "disconnected" notice. The config keeps the DAC.
  - A loss on `default` itself shows the notice "Audio device stopped" and does nothing more.
- **The player** needs no change for rate changes. During the drain the old song is still playing, so the position moves. The reopen's silence is well under the player's 1 s stall threshold, so it never shows "Buffering".

## 4. Edge cases

- **A song rate the DAC lacks** (22.05 kHz, say): it is resampled to 48 kHz, as on MiSTer audio.
- **A pause during a rate-change drain:** the drain waits, and the reopen happens after resume, when the last frames have played.
- **Stop, Play or a server switch during a drain:** the drain ends, and the new command runs as today.
- **A seek on the previous song during a drain:** it gets `ErrNotCurrent`, and the player reopens, as today.
- **A rate change between songs of a gapless album:** it can't happen; an album with mixed rates gets a short gap at the change.
- **Volume and mute changes during a reopen:** they are kept by the output and applied to the new device before it starts.
- **The DAC is unplugged while closed** (MiSTer audio selected): nothing happens.
- **Two cards with the same name:** the row shows the ALSA id in brackets after the name.
- **Desktop runs:** cards from `/proc/asound/cards` are listed. A `hw:` card that is busy (owned by PipeWire, say) fails to open, and the failed-switch notice says so. `-null` runs list nothing and keep the null device.
- **The web remote:** volume and mute work as before, through the player. The remote shows no output setting.

## 5. Code

| File | Change |
|---|---|
| `internal/audio/cards.go` (new) | `Cards`, the `/proc/asound/cards` parser |
| `internal/audio/device.c`, `shim.h`, `device.go` | native devices, rates, `Reopen`, `Lost`, buffer drain at close |
| `internal/audio/mixer.c` (new), `mixer.go` | hardware volume through `libasound` |
| `internal/audio/engine.go` | target rate, rate-change drain, per-segment rate, `SwitchOutput`, `EventDeviceLost` |
| `internal/audio/tap.go`, `internal/viz/viz.go` | rate-sized tap, decimated window, rate-aware bands |
| `internal/player` | passes `EventDeviceLost` on |
| `internal/ui/screens_settings.go`, `internal/ui/app.go` | the Output row, the notices, the switch |
| `cmd/mistersubsonic/main.go` | open with fallback, the startup notice |
| `config.example.toml`, README | the `alsa_device` forms; a features line |

## 6. Testing

Everything runs on the null backend or with fakes. No test plays sound.

- **`Cards`:** the MiSTer's file (Dummy skipped, E30 kept), a desktop sample, an empty file, a card with no playback PCM.
- **Target rate:** a supported rate, an unsupported one (48 kHz), an output without 48 kHz, a fixed output.
- **A rate change**, with a fake output that records `Reopen`:
  - no reopen until `Consumed` reaches `written`;
  - the old song's last frame is written before the reopen;
  - Stop during the drain cancels it with no reopen;
  - pause makes the drain wait.
- **Gapless:** two songs at one native rate chain with no reopen and no gap in the frame count.
- **Position:** across a 44.1 → 96 kHz change, positions of both songs are right (the per-segment rate).
- **Seek:** at a native rate it uses no resampler; on a fixed device it resamples to 48 kHz.
- **`SwitchOutput`:**
  - reopens and seeks to the audible position;
  - a failed reopen restores the old name and returns the error;
  - a failed seek carries on.
- **Device loss:** a fake `Lost()` gives one `EventDeviceLost`; the app switches to `default` and shows the notice (a UI test with a fake engine side).
- **Mixer:** the dB mapping on a fake control:
  - 1 dB steps give software gain 1;
  - 0.5 dB steps, a control in odd steps, and clamping at the range ends;
  - mute with and without a switch, then un-mute.
- **Tap and viz:**
  - the decimation factor for each rate;
  - the mean of `d` frames;
  - band tables rebuilt on a rate change;
  - the same bar output for a 1 kHz sine at 48 and 96 kHz.
- **Settings:** the Output row's values, a missing configured DAC, a custom `alsa_device` value.
- **Config:** `alsa_device` round-trips its three forms.

## 7. On the MiSTer

Before the plan is final, the prototype is measured on the device with the E30. Every check that makes sound is announced to the user first, at low volume.
- Opening `hw:CARD=E30,DEV=0` at 44.1, 48, 96 and 192 kHz: the format and internal rate miniaudio reports, and `/proc/asound/card1/pcm0p/sub0/hw_params` while playing.
- The silence at a rate change, from the last frame of one song to the first of the next, from timestamps in a test binary.
- CPU while playing a 24/96 and a 24/192 FLAC, with the visualizer off and on.
- Hardware volume:
  - the E30's control follows the app;
  - mute uses the switch;
  - whether the E30's knob or remote changes the same control (read with `amixer` after turning it).
- Unplugging during playback: the fallback and the notice, and that the app keeps playing.
- Starting with the DAC unplugged.

## 8. Docs

- README: a features line, "USB DACs, at each song's own sample rate, with the DAC's volume".
- `config.example.toml`: the three forms of `alsa_device`.
- `docs/testing-on-mister.md`: items for the checks in §7.
- `docs/spikes.md`: the measurements.

## 9. Out of scope

- Switching back to the DAC automatically when it returns.
- DSD and DoP.
- Multichannel.
- Bit depth chosen per song. The device runs at its native format, and the samples are converted from float.
- An exclusive or "bit-perfect" switch: ReplayGain and any software gain still apply when on.
- Outputs other than ALSA `hw:` cards (Bluetooth, network).
