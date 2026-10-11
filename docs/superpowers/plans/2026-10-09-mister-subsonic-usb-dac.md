# MiSTer Subsonic — USB DAC output: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** USB DACs as an output.
- Settings → Playback → Output picks the DAC.
- Each song plays at its own sample rate, with no resampler.
- The app's volume drives the DAC's own hardware volume.
- A missing or unplugged DAC falls back to MiSTer audio, with a notice.

**Architecture:**
- **`internal/audio`, native devices.** A `hw:` name is a *native* device: opened as is (no dmix), at the music's rate, in its most precise format. It can be reopened at another rate, and its card is watched in `/proc/asound`.
- **`internal/audio`, the mixer.** `libasound`'s simple mixer is loaded at run time and carries the volume.
- **The engine:** it lets the old song play out, reopens the device when the rate changes, keeps a per-stretch rate for the position clock, and gains `SwitchOutput` and `EventDeviceLost`.
- **The visualizer:** the tap and the analyzer follow the device's rate.
- **The UI:** an `AudioOutput` interface, which `cmd/mistersubsonic` implements on the engine. It drives the Output row and the fallbacks.

**Tech Stack:**
- Go 1.26+ and C through cgo (miniaudio, plus `dlopen` of `libasound.so.2`).
- No new modules and no new link-time libraries.

**Spec:** `docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md`, approved by the user. Its §10 (added in Task 6) records what the prototype changed. The spec extends the main spec, `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`, §6.

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:** `go test -race ./...`, `make e2e`, the launcher tests and the ARM release build (glibc 2.29 ≤ 2.31) pass.
- **Replay:** the blocks were replayed task by task on a fresh clone of `usb-dac`. At every task the new tests failed before the code and passed after, and the tree ended byte-identical to the prototype's.
- **On the MiSTer, with the user's Topping E30** (2026-10-09; results in `docs/spikes.md`, "USB DAC"):
  - A test binary probed rates and formats, silence only.
  - The decode benchmark ran.
  - The prototype app played a 44.1 kHz then a 24/96 album natively, its volume moved the E30's control, and output switching worked in both directions.
  - The unplug fallback worked, and so did the start with the DAC missing.

**Patches:** each step's block is a patch for `git apply` from the repository root. Copy blocks exactly. Patches must apply cleanly with `git apply`; nothing else needs typing.

## Global Constraints

- **Toolchain:**
  - Go 1.26+, with no new modules.
  - cgo only in `internal/audio`.
  - ARM build: `scripts/check-glibc.sh` must report ≤ 2.31.
  - `libasound` is loaded with `dlopen` at run time; the build links nothing new.
- **MiSTer audio is unchanged:**
  - `alsa_device = "default"`, and any name not starting with `hw:`, is a *fixed* device: 48 kHz (`OutputRate`), software volume, as before.
  - Only `hw:` names are native (`audio.Native`).
- **Config:** a DAC is saved as `alsa_device = "hw:CARD=<id>,DEV=0"`, which names the card by its ALSA id, not its number.
- **Never ask ALSA about the open PCM from another goroutine or thread.** On ARM, alsa-lib syncs the stream pointers on such calls, and that broke the stream (clicks) on the device. Loss detection reads `/proc/asound` instead.
- **🔇 Sound safety:**
  - Unit tests use fakes or miniaudio's null backend.
  - The manual tests (`TestRealDeviceListening`, `TestNativeProbeManual`, `TestMixerProbeManual`) are skipped unless their environment variable is set.
  - Nothing is played on the MiSTer without the user's go-ahead.
- **Secrets:** nothing from `.env` is printed, committed or written into docs.
- **Publishing:** nothing is pushed, tagged or released by this plan.

## Review Focus

The five situations most likely to bite the user, each pinned by tests in the task that owns the code:

1. **A DAC unplugged mid-song** leaving the app silent at "Buffering", with a thread spinning. The music must move to MiSTer audio within about a second.
   - Tests: `TestNativeDeviceIsLostWhenItsCardGoes` and `TestDeviceStoppedByItselfIsLostUntilReopened` (Task 1); `TestEngineReportsALostDeviceOncePerLoss` (Task 3); `TestOutputLostDACFallsBackToMiSTerAudio` (Task 5).
2. **The end of a song cut off, or the next song dropped, at a rate change.**
   - Tests (Task 3): `TestEngineRateChangeWaitsForTheOldSongToPlayOut`, `TestEngineStopDuringARateChangeDrainCancelsIt` and `TestEngineReopenFailureHoldsTheSongForSwitchOutput`.
3. **A volume blast:** the DAC playing a moment at its old level, a mute switch left off, or software gain above 1.
   - Tests (Task 2): `TestHWVolumeNeverAmplifies`, `TestHWVolumeTurnsOnASwitchLeftOff` and `TestDeviceVolumeMovesToTheDACsControl`.
4. **A wrong position** (progress bar, scrobbling, resume) after a rate change or a switch.
   - Tests (Task 3): the position checks in `TestEngineRateChangeWaitsForTheOldSongToPlayOut` and `TestEngineSwitchOutputCarriesOnFromWhatWasHeard`.
5. **No sound** for a rate the DAC refuses, or for a missing DAC at start.
   - Tests: `TestEngineRateRefusedAtReopenFallsBackTo48k` and `TestEngineRateTheDeviceLacksIsResampledTo48k` (Task 3); `TestOpenOutputFallsBackFromAMissingDAC` (Task 5).

## Decisions this plan makes (from the prototype; the spec's §10 records them)

- **`Output.Supports(rate)` replaces the spec's `Rates()`.** miniaudio's device info only lists a DAC under its own `:N,0` id, probed through dmix (48 kHz only).
  - A native device supports every rate until a reopen at that rate fails with `ErrRateUnsupported`. The refusal is remembered per device name.
  - The engine then reopens at 48 kHz and resamples.
- **The format:** a native open tries `S32`, `S24`, float and `S16` in turn and keeps the first the device really takes. miniaudio's own fallback would pick 16-bit first. The callback converts from float with `ma_pcm_convert`; scaling by a power of two keeps 16- and 24-bit samples exact.
- **A reopen:**
  - lets ALSA's buffer play out (period size × periods, plus 20 ms) before closing;
  - keeps `Consumed` counting, and keeps the pause and the volume;
  - keeps miniaudio's context alive across reopens.
- **Losing a device:**
  - miniaudio's stop notification covers a device that stopped by itself.
  - A native device whose card is gone from `/proc/asound` (checked at most every 200 ms) counts as lost too. miniaudio's ALSA loop just spins on such a device.
  - A failed reopen isn't "lost": the caller gets the error. In a rate change the engine then holds the song and sends `EventDeviceLost`, and `SwitchOutput` starts the held song.
- **`SwitchOutput(name)`** reopens and seeks the current song to the audible position. If the new device fails, it reopens the previous one and returns the error.
- **`doSeek` rounds the frame;** truncating lost one.
- **Hardware volume:**
  - The first playback element with a dB range on the card's simple mixer.
  - The control is set to the nearest step at or above the wanted level, and software makes up the rest (≤ 1).
  - Mute uses the switch when there is one.
  - A card missing from `/proc/asound` gets no mixer attempt; it's tried again when it's there.
- **The visualizer:**
  - The tap holds the same time at any rate.
  - `Window` returns `(n, rate)`, averaging 2, 4, 8 or 16 frames above 48 kHz.
  - `viz.Analyzer.SetRate` rebuilds the band tables on a change.
- **The UI:**
  - The Output row is the last row of Settings → Playback.
  - The fallback notices are "<id> isn't connected: using MiSTer audio" (start), "<name> didn't open (<error>): using MiSTer audio" (start, card present), "<name> was disconnected: using MiSTer audio" (unplugged while playing), and "Can't use <name>: <error>" (a failed switch).
- **The log:** each native open logs "audio: <name> at <rate> Hz, <bits>-bit (opened in <ms> ms)".

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/audio/device.c`, `shim.h`, `device.go`, `cards.go` | native devices, reopen, loss, the card list | 1 |
| `internal/audio/mixer.c`, `mixer.go` (+ `shim.h`, `device.go`) | hardware volume | 2 |
| `internal/audio/engine.go`, `cmd/mss-cli` | rates in the engine, `SwitchOutput`, `EventDeviceLost` | 3 |
| `internal/audio/tap.go`, `internal/viz/viz.go`, `internal/ui/viz.go`, `ui.Visual` | the visualizer at any rate | 4 |
| `internal/player`, `internal/ui/output.go`, Settings, `cmd/mistersubsonic/output.go`, `main.go` | the Output row, the fallbacks, the notices | 5 |
| README, `config.example.toml`, `docs/testing-on-mister.md`, `docs/spikes.md`, the spec, the follow-ups | docs | 6 |

---


### Task 1: Native devices: the music's rate, reopen, loss; the card list

The output gains what a USB DAC needs. A `hw:` name is opened as is, at the rate asked for, in the DAC's most precise format. It can be reopened at another rate without losing the frame count, and a lost device or a gone card is reported. `Cards()` lists the cards that can play, for the Output row. The engine is untouched here: it still runs everything at 48 kHz.

**Files:**
- Create: `internal/audio/cards.go`
- Modify: `internal/audio/device.c`
- Modify: `internal/audio/device.go`
- Modify: `internal/audio/shim.h`
- Modify: `internal/audio/tap.go`
- Test: `internal/audio/bench_test.go`
- Test: `internal/audio/cards_test.go`
- Test: `internal/audio/device_manual_test.go`
- Test: `internal/audio/device_test.go`
- Test: `internal/audio/fakes_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - **The `audio.Output` interface** gains `Rate() int`, `Supports(rate int) bool`, `Reopen(name string, rate int) error` and `Lost() bool`.
  - **Errors and names:** `audio.ErrRateUnsupported` and `audio.Native(name string) bool` (a `hw:` prefix).
  - **`DeviceOptions.Rate int`:** the starting rate of a native device.
  - **The card list:** `audio.Card{Index int; ID, Name string}` with `Device() string` (`"hw:CARD=<ID>,DEV=0"`), `audio.CardID(device string) string` and `audio.Cards() []Card`.
  - **Unexported, used by Task 2:** `device` fields `mu`, `name` and `refused`, and the variable `cardPresent func(device string) bool`.
  - **Test fakes** in `fakes_test.go`: `newNativeOutput(ring int, rates ...int)`; fields `name`, `rate`, `rates`, `refuse` and `reopenErr`; methods `reopenCalls()` and `setLost()`.
  - **`*Tap`** passes the new methods on; a reopen drops what it kept.

- [ ] **Step 1: Write the failing tests.** Apply with `git apply`:

```diff
diff --git a/internal/audio/bench_test.go b/internal/audio/bench_test.go
index 259f7e2..dd0aedc 100644
--- a/internal/audio/bench_test.go
+++ b/internal/audio/bench_test.go
@@ -5,6 +5,8 @@ import (
 	"errors"
 	"io"
 	"os"
+	"path/filepath"
+	"strings"
 	"testing"
 	"time"
 )
@@ -62,3 +64,64 @@ func BenchmarkDecodeResample(b *testing.B) {
 		})
 	}
 }
+
+// BenchmarkDecodeNative compares a native device's path (decode only, at
+// the file's rate) with a fixed device's (decode + resample to 48 kHz),
+// as the fraction of one core real-time playback needs ("core%"). Files
+// come from MSS_BENCH_FLAC (comma-separated paths), else the 96k/24 tone.
+func BenchmarkDecodeNative(b *testing.B) {
+	files := []string{"testdata/tone-96k24.flac"}
+	if env := os.Getenv("MSS_BENCH_FLAC"); env != "" {
+		files = strings.Split(env, ",")
+	}
+	for _, file := range files {
+		data, err := os.ReadFile(file)
+		if err != nil {
+			b.Fatal(err)
+		}
+		for _, resample := range []bool{false, true} {
+			name := filepath.Base(file) + "/native"
+			if resample {
+				name = filepath.Base(file) + "/resample48k"
+			}
+			b.Run(name, func(b *testing.B) {
+				buf := make([]float32, 4096*2)
+				var out []float32
+				var audioSeconds float64
+				start := time.Now()
+				for i := 0; i < b.N; i++ {
+					d, err := OpenDecoder(bytes.NewReader(data), FormatFLAC)
+					if err != nil {
+						b.Fatal(err)
+					}
+					var rs *Resampler
+					if resample {
+						if rs, err = NewResampler(d.SampleRate(), OutputRate, DefaultResampleQuality); err != nil {
+							b.Fatal(err)
+						}
+					}
+					frames := 0
+					for {
+						n, err := d.Read(buf)
+						if rs != nil {
+							out = rs.Process(buf[:n*2], out[:0])
+						}
+						frames += n
+						if errors.Is(err, io.EOF) {
+							break
+						}
+						if err != nil {
+							b.Fatal(err)
+						}
+					}
+					audioSeconds += float64(frames) / float64(d.SampleRate())
+					if rs != nil {
+						rs.Close()
+					}
+					d.Close()
+				}
+				b.ReportMetric(100*time.Since(start).Seconds()/audioSeconds, "core%")
+			})
+		}
+	}
+}
diff --git a/internal/audio/cards_test.go b/internal/audio/cards_test.go
new file mode 100644
index 0000000..ec9810e
--- /dev/null
+++ b/internal/audio/cards_test.go
@@ -0,0 +1,62 @@
+package audio
+
+import (
+	"reflect"
+	"testing"
+)
+
+func playsAll(int) bool { return true }
+
+func TestParseCardsMiSTerWithAUSBDAC(t *testing.T) {
+	const text = ` 0 [Dummy          ]: Dummy - Dummy
+                      Dummy 1
+ 1 [E30            ]: USB-Audio - E30
+                      Topping E30 at usb-ffb40000.usb-1.2, high speed
+`
+	got := parseCards(text, playsAll)
+	want := []Card{{Index: 1, ID: "E30", Name: "Topping E30"}}
+	if !reflect.DeepEqual(got, want) {
+		t.Fatalf("got %+v, want %+v", got, want)
+	}
+	if d := got[0].Device(); d != "hw:CARD=E30,DEV=0" {
+		t.Errorf("Device() = %q", d)
+	}
+}
+
+func TestParseCardsDesktop(t *testing.T) {
+	const text = ` 0 [HDMI           ]: HDA-Intel - HDA ATI HDMI
+                      HDA ATI HDMI at 0xfcd60000 irq 106
+ 1 [Generic        ]: HDA-Intel - HD-Audio Generic
+                      HD-Audio Generic at 0xfcd00000 irq 108
+ 2 [Mic            ]: USB-Audio - USB Microphone
+                      USB Microphone at usb-0000:0c:00.3-2, full speed
+`
+	got := parseCards(text, func(i int) bool { return i != 2 }) // the mic doesn't play
+	want := []Card{
+		{Index: 0, ID: "HDMI", Name: "HDA ATI HDMI"},
+		{Index: 1, ID: "Generic", Name: "HD-Audio Generic"},
+	}
+	if !reflect.DeepEqual(got, want) {
+		t.Fatalf("got %+v, want %+v", got, want)
+	}
+}
+
+func TestParseCardsSameNameShowsTheID(t *testing.T) {
+	const text = ` 1 [DAC            ]: USB-Audio - USB DAC
+                      Generic USB DAC at usb-1, high speed
+ 2 [DAC_1          ]: USB-Audio - USB DAC
+                      Generic USB DAC at usb-2, high speed
+`
+	got := parseCards(text, playsAll)
+	if len(got) != 2 || got[0].Name != "Generic USB DAC (DAC)" || got[1].Name != "Generic USB DAC (DAC_1)" {
+		t.Fatalf("got %+v", got)
+	}
+}
+
+func TestParseCardsEmptyAndJunk(t *testing.T) {
+	for _, text := range []string{"", "--- no soundcards ---\n", "garbage [x]: y\n"} {
+		if got := parseCards(text, playsAll); len(got) != 0 {
+			t.Errorf("%q: got %+v, want none", text, got)
+		}
+	}
+}
diff --git a/internal/audio/device_manual_test.go b/internal/audio/device_manual_test.go
index 44416ef..f0a21d3 100644
--- a/internal/audio/device_manual_test.go
+++ b/internal/audio/device_manual_test.go
@@ -6,6 +6,7 @@ import (
 	"io"
 	"math"
 	"os"
+	"strings"
 	"testing"
 	"time"
 )
@@ -90,3 +91,61 @@ func TestRealDeviceListening(t *testing.T) {
 		time.Sleep(10 * time.Millisecond)
 	}
 }
+
+// TestNativeProbeManual opens a native device and reopens it at several
+// rates, writing only zeros (silence), and logs what the device reports and
+// how long each reopen takes. Skipped unless MSS_NATIVE_PROBE names the
+// device, for example on a MiSTer with a USB DAC:
+//
+//	MSS_NATIVE_PROBE=hw:CARD=E30,DEV=0 ./audio.test -test.run NativeProbe -test.v
+func TestNativeProbeManual(t *testing.T) {
+	name := os.Getenv("MSS_NATIVE_PROBE")
+	if name == "" {
+		t.Skip("set MSS_NATIVE_PROBE=hw:CARD=<id>,DEV=0 to probe a native device (silence only)")
+	}
+	out, err := OpenDevice(DeviceOptions{Name: name, Rate: 48000})
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer out.Close()
+	d := out.(*device)
+	t.Logf("open: rate=%d bits=%d", d.Rate(), d.FormatBits())
+	silence := make([]float32, 2*4800)
+	play := func(dur time.Duration) {
+		end := time.Now().Add(dur)
+		for time.Now().Before(end) {
+			if out.Write(silence) == 0 {
+				time.Sleep(5 * time.Millisecond)
+			}
+		}
+	}
+	hwParams := func() string {
+		b, _ := os.ReadFile(os.Getenv("MSS_PROBE_HWPARAMS"))
+		return strings.Join(strings.Fields(string(b)), " ")
+	}
+	play(300 * time.Millisecond)
+	t.Logf("hw_params at 48000: %s", hwParams())
+	for _, rate := range []int{44100, 96000, 192000, 88200, 48000, 22050} {
+		t0 := time.Now()
+		err := out.Reopen(name, rate)
+		reopen := time.Since(t0)
+		if err != nil {
+			t.Logf("reopen %d: %v (lost=%v, supports=%v) after %v", rate, err, out.Lost(), out.Supports(rate), reopen)
+			if err := out.Reopen(name, 48000); err != nil {
+				t.Fatal(err)
+			}
+			continue
+		}
+		c0 := out.Consumed()
+		out.Write(silence)
+		t1 := time.Now()
+		for out.Consumed() == c0 && time.Since(t1) < time.Second {
+			time.Sleep(time.Millisecond)
+		}
+		first := time.Since(t1)
+		play(300 * time.Millisecond)
+		t.Logf("reopen %d: took %v, first frames consumed after %v; rate=%d bits=%d; hw_params: %s",
+			rate, reopen, first, out.Rate(), d.FormatBits(), hwParams())
+	}
+	t.Logf("lost=%v", out.Lost())
+}
diff --git a/internal/audio/device_test.go b/internal/audio/device_test.go
index 1a5d1b9..88b6c2b 100644
--- a/internal/audio/device_test.go
+++ b/internal/audio/device_test.go
@@ -87,3 +87,117 @@ func TestDeviceCallsAfterCloseAreSafe(t *testing.T) {
 	again.SetVolume(1)
 	again.Close()
 }
+
+func waitConsumed(t *testing.T, out Output, atLeast uint64) {
+	t.Helper()
+	deadline := time.Now().Add(2 * time.Second)
+	for out.Consumed() < atLeast {
+		if time.Now().After(deadline) {
+			t.Fatalf("Consumed = %d, never reached %d", out.Consumed(), atLeast)
+		}
+		time.Sleep(5 * time.Millisecond)
+	}
+}
+
+func TestDeviceReopenKeepsCountingAndPause(t *testing.T) {
+	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer out.Close()
+	out.Write(make([]float32, 2000*2))
+	waitConsumed(t, out, 2000)
+	out.Write(make([]float32, 1000*2)) // still in the ring at the reopen: it counts as consumed
+	if err := out.Reopen("", OutputRate); err != nil {
+		t.Fatal(err)
+	}
+	if got := out.Consumed(); got != 3000 {
+		t.Fatalf("Consumed = %d after the reopen, want 3000 (everything written)", got)
+	}
+	out.Write(make([]float32, 500*2))
+	waitConsumed(t, out, 3500)
+
+	out.SetPaused(true)
+	if err := out.Reopen("", OutputRate); err != nil {
+		t.Fatal(err)
+	}
+	out.Write(make([]float32, 100*2))
+	time.Sleep(60 * time.Millisecond)
+	if got := out.Consumed(); got != 3500 {
+		t.Fatalf("a paused device consumed after a reopen: %d", got)
+	}
+	out.SetPaused(false)
+	waitConsumed(t, out, 3600)
+}
+
+func TestDeviceStoppedByItselfIsLostUntilReopened(t *testing.T) {
+	out, err := OpenDevice(DeviceOptions{Null: true, RingFrames: 4800})
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer out.Close()
+	if out.Lost() {
+		t.Fatal("a fresh device reports lost")
+	}
+	stopDeviceForTest() // as an unplugged DAC would
+	deadline := time.Now().Add(time.Second)
+	for !out.Lost() {
+		if time.Now().After(deadline) {
+			t.Fatal("a stopped device never reported lost")
+		}
+		time.Sleep(5 * time.Millisecond)
+	}
+	if err := out.Reopen("", OutputRate); err != nil {
+		t.Fatal(err)
+	}
+	if out.Lost() {
+		t.Fatal("still lost after a reopen")
+	}
+	out.Close()
+	if out.Lost() {
+		t.Fatal("closing the device counts as losing it")
+	}
+}
+
+func TestFixedDeviceRunsAt48k(t *testing.T) {
+	out, err := OpenDevice(DeviceOptions{Null: true, Rate: 96000})
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer out.Close()
+	if out.Rate() != OutputRate || !out.Supports(OutputRate) || out.Supports(44100) {
+		t.Fatalf("rate %d, supports 48k %v, 44.1k %v: a fixed device runs at 48 kHz only",
+			out.Rate(), out.Supports(OutputRate), out.Supports(44100))
+	}
+	if err := out.Reopen("", 44100); err != nil || out.Rate() != OutputRate {
+		t.Fatalf("reopen at 44.1 kHz: %v, rate %d; a fixed device stays at 48 kHz", err, out.Rate())
+	}
+}
+
+func TestNativeDeviceIsLostWhenItsCardGoes(t *testing.T) {
+	present := true
+	var asked []string
+	old := cardPresent
+	cardPresent = func(device string) bool { asked = append(asked, device); return present }
+	defer func() { cardPresent = old }()
+
+	d := &device{name: "hw:CARD=E30,DEV=0"} // the card check only: no C device behind it
+	if d.Lost() {
+		t.Fatal("lost with the card there")
+	}
+	present = false
+	if d.Lost() {
+		t.Fatal("checked again within cardCheckEvery")
+	}
+	d.checked = time.Now().Add(-cardCheckEvery)
+	if !d.Lost() {
+		t.Fatal("not lost with the card gone")
+	}
+	if len(asked) != 2 || asked[0] != "hw:CARD=E30,DEV=0" {
+		t.Fatalf("checked %q", asked)
+	}
+	(&device{name: "default"}).Lost() // not native: no check
+	if len(asked) != 2 {
+		t.Fatalf("checked a non-native device: %q", asked)
+	}
+}
diff --git a/internal/audio/fakes_test.go b/internal/audio/fakes_test.go
index 0ee6b92..f89fbb5 100644
--- a/internal/audio/fakes_test.go
+++ b/internal/audio/fakes_test.go
@@ -2,7 +2,9 @@ package audio
 
 import (
 	"errors"
+	"fmt"
 	"io"
+	"slices"
 	"sync"
 	"sync/atomic"
 	"testing"
@@ -17,10 +19,76 @@ type fakeOutput struct {
 	consumed uint64
 	flushes  int
 	onFlush  func() // optional callback called after Flush completes (outside mu)
+
+	name      string // the device open now (see Native)
+	rate      int    // 0: OutputRate
+	rates     []int  // the rates a native fake takes; nil: a fixed device
+	reopens   []reopenCall
+	reopenErr error // the next Reopen fails with it
+	refuse    []int // rates Reopen turns down with ErrRateUnsupported, though rates lists them
+	lost      bool
+}
+
+// reopenCall records a Reopen and the state of the ring when it came.
+type reopenCall struct {
+	name     string
+	rate     int
+	written  uint64 // frames written so far
+	consumed uint64 // frames consumed so far
 }
 
 func newFakeOutput(ringFrames int) *fakeOutput { return &fakeOutput{ring: ringFrames} }
 
+// newNativeOutput is a fake native device that takes rates.
+func newNativeOutput(ringFrames int, rates ...int) *fakeOutput {
+	return &fakeOutput{ring: ringFrames, rates: rates, name: "hw:CARD=DAC,DEV=0"}
+}
+
+func (f *fakeOutput) Rate() int {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	if f.rate == 0 {
+		return OutputRate
+	}
+	return f.rate
+}
+
+func (f *fakeOutput) Supports(rate int) bool {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	if !Native(f.name) {
+		return rate == OutputRate
+	}
+	return slices.Contains(f.rates, rate)
+}
+
+func (f *fakeOutput) Reopen(name string, rate int) error {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	f.reopens = append(f.reopens, reopenCall{name: name, rate: rate, written: uint64(len(f.data) / 2), consumed: f.consumed})
+	f.consumed = uint64(len(f.data) / 2) // a reopen drops what the ring held
+	if err := f.reopenErr; err != nil {  // the device stays closed; the caller has the error
+		f.reopenErr = nil
+		return err
+	}
+	if slices.Contains(f.refuse, rate) {
+		f.rates = slices.DeleteFunc(f.rates, func(r int) bool { return r == rate })
+		return fmt.Errorf("%w: %d Hz", ErrRateUnsupported, rate)
+	}
+	f.name, f.rate, f.lost = name, rate, false
+	return nil
+}
+
+func (f *fakeOutput) Lost() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.lost }
+
+func (f *fakeOutput) setLost() { f.mu.Lock(); f.lost = true; f.mu.Unlock() }
+
+func (f *fakeOutput) reopenCalls() []reopenCall {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	return append([]reopenCall(nil), f.reopens...)
+}
+
 func (f *fakeOutput) Write(frames []float32) int {
 	f.mu.Lock()
 	defer f.mu.Unlock()
```

- [ ] **Step 2: Run the tests to see them fail.**

Run: `go test -race ./internal/audio`
Expected: FAIL, build failure, for example `undefined: parseCards`.

- [ ] **Step 3: Write the implementation.** Apply with `git apply`:

```diff
diff --git a/internal/audio/cards.go b/internal/audio/cards.go
new file mode 100644
index 0000000..818ae3b
--- /dev/null
+++ b/internal/audio/cards.go
@@ -0,0 +1,89 @@
+package audio
+
+import (
+	"os"
+	"path/filepath"
+	"strconv"
+	"strings"
+)
+
+// Card is a sound card that can play: a choice for the output.
+type Card struct {
+	Index int    // 1
+	ID    string // the ALSA id: "E30"
+	Name  string // what to show: "Topping E30"
+}
+
+// Device is the card's native device name (see Native). It names the card
+// by its id, so it survives replugging and reboots.
+func (c Card) Device() string { return "hw:CARD=" + c.ID + ",DEV=0" }
+
+// CardID is the card a native device name points at: "E30" for
+// "hw:CARD=E30,DEV=0" or "hw:E30,0".
+func CardID(device string) string {
+	card, _, _ := strings.Cut(strings.TrimPrefix(device, "hw:"), ",")
+	return strings.TrimPrefix(card, "CARD=")
+}
+
+// Cards lists the sound cards that can play, from /proc/asound. MiSTer's
+// own Dummy card (behind ALSA's default device) is left out. No /proc/asound
+// (not Linux) gives none.
+func Cards() []Card {
+	b, err := os.ReadFile("/proc/asound/cards")
+	if err != nil {
+		return nil
+	}
+	return parseCards(string(b), func(index int) bool {
+		m, _ := filepath.Glob("/proc/asound/card" + strconv.Itoa(index) + "/pcm*p")
+		return len(m) > 0
+	})
+}
+
+// parseCards reads /proc/asound/cards: each card is a line
+// " 1 [E30            ]: USB-Audio - E30" followed by its long name,
+// "Topping E30 at usb-ffb40000.usb-1.2, high speed".
+func parseCards(text string, plays func(index int) bool) []Card {
+	var out []Card
+	lines := strings.Split(text, "\n")
+	for i := 0; i < len(lines); i++ {
+		head := strings.TrimSpace(lines[i])
+		num, rest, ok := strings.Cut(head, " [")
+		if !ok {
+			continue
+		}
+		index, err := strconv.Atoi(num)
+		if err != nil {
+			continue
+		}
+		id, rest, ok := strings.Cut(rest, "]: ")
+		if !ok {
+			continue
+		}
+		driver, short, _ := strings.Cut(rest, " - ")
+		name := strings.TrimSpace(short)
+		if i+1 < len(lines) {
+			long := strings.TrimSpace(lines[i+1])
+			if l, _, found := strings.Cut(long, " at "); found && l != "" {
+				name = l
+			} else if long != "" && !strings.Contains(long, "[") {
+				name = long
+			}
+		}
+		id = strings.TrimSpace(id)
+		if id == "" || strings.TrimSpace(driver) == "Dummy" || !plays(index) {
+			continue
+		}
+		out = append(out, Card{Index: index, ID: id, Name: name})
+	}
+	// Two cards of the same name: the id tells them apart.
+	count := map[string]int{}
+	for _, c := range out {
+		count[c.Name]++
+	}
+	for i := range out {
+		if count[out[i].Name] > 1 {
+			out[i].Name += " (" + out[i].ID + ")"
+		}
+	}
+	return out
+}
diff --git a/internal/audio/device.c b/internal/audio/device.c
index d1e66d7..e9f12cc 100644
--- a/internal/audio/device.c
+++ b/internal/audio/device.c
@@ -14,6 +14,15 @@ static volatile int g_paused;
 static volatile int g_flush_req;
 static volatile uint64_t g_consumed;
 static volatile float g_volume = 1.0f;
+static volatile int g_lost;     /* the device stopped without being asked */
+static volatile int g_stopping; /* a stop we asked for is under way */
+
+/* Set at open, read by the callback. */
+static ma_format g_format = ma_format_f32; /* what the device takes */
+static uint32_t g_rate;
+static uint32_t g_buffer_frames; /* ALSA's buffer: frames still to be heard after the ring is empty */
+static int g_ctx_ready;          /* the context outlives reopens: initializing it costs ~250 ms on a MiSTer */
+static int g_ctx_null;           /* the context is the null backend's */
 
 /* Discards everything in the ring, counting it as consumed. Only the ring's
    consumer may call it: the callback, or the producer once the callback has
@@ -32,10 +41,8 @@ static void drain_ring(void) {
     }
 }
 
-static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames) {
-    (void)dev;
-    (void)in;
-    float* dst = (float*)out;
+/* fill writes frames of float output to dst: ring audio, then silence. */
+static void fill(float* dst, ma_uint32 frames) {
     if (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
         drain_ring();
         __atomic_store_n(&g_flush_req, 0, __ATOMIC_SEQ_CST);
@@ -66,36 +73,85 @@ static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames)
     }
 }
 
-int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames) {
-    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
-        return MA_INVALID_OPERATION;
+#define CONVERT_FRAMES 512
+
+static void data_cb(ma_device* dev, void* out, const void* in, ma_uint32 frames) {
+    (void)dev;
+    (void)in;
+    if (g_format == ma_format_f32) {
+        fill((float*)out, frames);
+        return;
     }
-    ma_backend null_only[1] = { ma_backend_null };
-    ma_result r = ma_context_init(null_backend ? null_only : NULL, null_backend ? 1 : 0, NULL, &g_context);
-    if (r != MA_SUCCESS) {
-        return r;
+    /* A native device: fill floats, then convert to its format. Scaling by a
+       power of two keeps 16- and 24-bit samples exact. */
+    static float buf[CONVERT_FRAMES * 2];
+    ma_uint32 bps = ma_get_bytes_per_sample(g_format) * 2;
+    unsigned char* dst = (unsigned char*)out;
+    while (frames > 0) {
+        ma_uint32 n = frames < CONVERT_FRAMES ? frames : CONVERT_FRAMES;
+        fill(buf, n);
+        ma_pcm_convert(dst, g_format, buf, ma_format_f32, (ma_uint64)n * 2, ma_dither_mode_none);
+        dst += n * bps;
+        frames -= n;
     }
-    r = ma_pcm_rb_init(ma_format_f32, 2, ring_frames, NULL, NULL, &g_ring);
-    if (r != MA_SUCCESS) {
+}
+
+static void notify_cb(const ma_device_notification* n) {
+    if (n->type == ma_device_notification_type_stopped &&
+        !__atomic_load_n(&g_stopping, __ATOMIC_SEQ_CST)) {
+        __atomic_store_n(&g_lost, 1, __ATOMIC_SEQ_CST);
+    }
+}
+
+static void sleep_ms(long ms) {
+    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
+    nanosleep(&ts, NULL);
+}
+
+/* open_device opens a device; the counters, pause and volume carry over.
+   native: name is an ALSA name used as is, the device runs at rate exactly
+   in its most precise format, or the open fails with MSS_RATE_UNSUPPORTED. */
+static int open_device(const char* name, int null_backend, uint32_t rate, uint32_t ring_frames, int native) {
+    /* The most precise formats first: 24-bit music stays 24-bit. */
+    static const ma_format native_formats[] = { ma_format_s32, ma_format_s24, ma_format_f32, ma_format_s16 };
+    ma_backend null_only[1] = { ma_backend_null };
+    ma_result r;
+    if (g_ctx_ready && g_ctx_null != null_backend) {
         ma_context_uninit(&g_context);
-        return r;
+        g_ctx_ready = 0;
+    }
+    if (!g_ctx_ready) {
+        r = ma_context_init(null_backend ? null_only : NULL, null_backend ? 1 : 0, NULL, &g_context);
+        if (r != MA_SUCCESS) {
+            return r;
+        }
+        g_ctx_ready = 1;
+        g_ctx_null = null_backend;
     }
     ma_device_config cfg = ma_device_config_init(ma_device_type_playback);
     cfg.playback.format = ma_format_f32;
     cfg.playback.channels = 2;
-    cfg.sampleRate = 48000;
+    cfg.sampleRate = rate;
     cfg.periodSizeInMilliseconds = 20;
     cfg.dataCallback = data_cb;
+    cfg.notificationCallback = notify_cb;
     cfg.noPreSilencedOutputBuffer = MA_TRUE;
 
     ma_device_info* infos = NULL;
     ma_uint32 count = 0;
+    ma_device_id named;
     ma_device_id* id = NULL;
-    if (device_name != NULL && device_name[0] != '\0' &&
-        ma_context_get_devices(&g_context, &infos, &count, NULL, NULL) == MA_SUCCESS) {
+    if (native) {
+        /* Used as is: miniaudio opens an id without a leading ':' unchanged
+           (no dmix), and doesn't look it up. */
+        memset(&named, 0, sizeof named);
+        strncpy(named.alsa, name, sizeof named.alsa - 1);
+        id = &named;
+    } else if (name != NULL && name[0] != '\0' &&
+               ma_context_get_devices(&g_context, &infos, &count, NULL, NULL) == MA_SUCCESS) {
         for (ma_uint32 i = 0; i < count; i++) {
-            if (strcmp(infos[i].name, device_name) == 0 ||
-                (g_context.backend == ma_backend_alsa && strcmp(infos[i].id.alsa, device_name) == 0)) {
+            if (strcmp(infos[i].name, name) == 0 ||
+                (g_context.backend == ma_backend_alsa && strcmp(infos[i].id.alsa, name) == 0)) {
                 id = &infos[i].id;
                 break;
             }
@@ -103,36 +159,106 @@ int mss_device_open(const char* device_name, int null_backend, uint32_t ring_fra
     }
     cfg.playback.pDeviceID = id;
 
-    g_paused = 0;
-    g_flush_req = 0;
-    g_consumed = 0;
-    r = ma_device_init(&g_context, &cfg, &g_device);
+    r = ma_pcm_rb_init(ma_format_f32, 2, ring_frames, NULL, NULL, &g_ring);
+    if (r != MA_SUCCESS) {
+        return r;
+    }
+    if (native) {
+        /* miniaudio falls back to a format of its own choosing (16-bit
+           first) when the one asked for is missing, so try ours in order
+           and keep the first the device really takes. A device that runs
+           at another rate than asked doesn't take this rate at all. */
+        r = MA_FORMAT_NOT_SUPPORTED;
+        for (size_t i = 0; i < sizeof native_formats / sizeof native_formats[0]; i++) {
+            cfg.playback.format = native_formats[i];
+            r = ma_device_init(&g_context, &cfg, &g_device);
+            if (r != MA_SUCCESS) {
+                break;
+            }
+            if (g_device.playback.internalSampleRate != rate) {
+                ma_device_uninit(&g_device);
+                r = MSS_RATE_UNSUPPORTED;
+                break;
+            }
+            if (g_device.playback.internalFormat == native_formats[i]) {
+                break;
+            }
+            ma_device_uninit(&g_device);
+            r = MA_FORMAT_NOT_SUPPORTED;
+        }
+        g_format = cfg.playback.format;
+    } else {
+        g_format = ma_format_f32;
+        r = ma_device_init(&g_context, &cfg, &g_device);
+    }
     if (r != MA_SUCCESS) {
         ma_pcm_rb_uninit(&g_ring);
-        ma_context_uninit(&g_context);
         return r;
     }
+    g_rate = rate;
+    g_buffer_frames = g_device.playback.internalPeriodSizeInFrames * g_device.playback.internalPeriods;
+    __atomic_store_n(&g_lost, 0, __ATOMIC_SEQ_CST);
+    __atomic_store_n(&g_stopping, 0, __ATOMIC_SEQ_CST);
     r = ma_device_start(&g_device);
     if (r != MA_SUCCESS) {
         ma_device_uninit(&g_device);
         ma_pcm_rb_uninit(&g_ring);
-        ma_context_uninit(&g_context);
         return r;
     }
     __atomic_store_n(&g_open, 1, __ATOMIC_SEQ_CST);
     return MA_SUCCESS;
 }
 
-void mss_device_close(void) {
-    if (!__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
-        return;
+/* close_device stops and frees the device. tail: first let ALSA's buffer
+   play out, so the last frames handed over are heard. What is left in the
+   ring counts as consumed. */
+static void close_device(int tail) {
+    if (tail && g_rate > 0 && !__atomic_load_n(&g_lost, __ATOMIC_SEQ_CST)) {
+        sleep_ms((long)g_buffer_frames * 1000 / g_rate + 20);
     }
+    __atomic_store_n(&g_stopping, 1, __ATOMIC_SEQ_CST);
+    __atomic_store_n(&g_open, 0, __ATOMIC_SEQ_CST);
     ma_device_uninit(&g_device);
+    drain_ring();
     ma_pcm_rb_uninit(&g_ring);
-    ma_context_uninit(&g_context);
-    __atomic_store_n(&g_open, 0, __ATOMIC_SEQ_CST);
 }
 
+int mss_device_open(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native) {
+    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
+        return MA_INVALID_OPERATION;
+    }
+    g_paused = 0;
+    g_flush_req = 0;
+    g_consumed = 0;
+    return open_device(device_name, null_backend, rate, ring_frames, native);
+}
+
+int mss_device_reopen(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native, int tail) {
+    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
+        close_device(tail);
+    }
+    g_flush_req = 0;
+    return open_device(device_name, null_backend, rate, ring_frames, native);
+}
+
+void mss_device_close(void) {
+    if (__atomic_load_n(&g_open, __ATOMIC_SEQ_CST)) {
+        close_device(0);
+    }
+    if (g_ctx_ready) {
+        ma_context_uninit(&g_context);
+        g_ctx_ready = 0;
+    }
+}
+
+uint32_t mss_device_rate(void) { return g_rate; }
+
+int mss_device_format_bits(void) {
+    return (int)ma_get_bytes_per_sample(g_format) * 8;
+}
+
+int mss_device_lost(void) { return __atomic_load_n(&g_lost, __ATOMIC_SEQ_CST); }
+
 /* After mss_device_close the ring and device are gone: every call below is
    a no-op then (write takes nothing, flush does nothing). */
 
@@ -191,8 +317,7 @@ void mss_device_flush(void) {
     /* The callback normally clears the request within one period (20 ms).
        Wait at most 200 ms so a stalled or stopped device can't hang us. */
     for (int i = 0; i < 200 && __atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST); i++) {
-        struct timespec ts = { 0, 1000000 };
-        nanosleep(&ts, NULL);
+        sleep_ms(1);
     }
     if (__atomic_load_n(&g_flush_req, __ATOMIC_SEQ_CST)) {
         if (device_stopped()) {
diff --git a/internal/audio/device.go b/internal/audio/device.go
index b0fbf6e..7952b85 100644
--- a/internal/audio/device.go
+++ b/internal/audio/device.go
@@ -7,20 +7,29 @@ package audio
 import "C"
 
 import (
+	"errors"
 	"fmt"
+	"log"
+	"os"
+	"strconv"
+	"strings"
+	"sync"
+	"time"
 	"unsafe"
 )
 
 // OutputRate is the fixed device rate. MiSTer's ALSA default device
-// resamples to 48 kHz linearly (and crackles), so we always hand it 48 kHz.
+// resamples to 48 kHz linearly (and crackles), so a fixed device always gets
+// 48 kHz. A native device runs at the music's rate instead (see Native).
 const OutputRate = 48000
 
-// Output is where the engine writes interleaved stereo float32 at OutputRate.
+// Output is where the engine writes interleaved stereo float32 at Rate().
 type Output interface {
 	// Write copies as many frames as fit and returns how many were taken.
 	Write(frames []float32) int
 	// Consumed is the running total of frames taken out of the ring by the
-	// device, including frames discarded by Flush.
+	// device, including frames discarded by Flush. It keeps counting across
+	// Reopen.
 	Consumed() uint64
 	SetPaused(paused bool)
 	SetVolume(linear float32)
@@ -28,50 +37,182 @@ type Output interface {
 	// the total number of frames ever written.
 	Flush()
 	Close() error
+	// Rate is the rate the device runs at now.
+	Rate() int
+	// Supports reports whether the device may run at rate: always for a
+	// native device until a Reopen found it doesn't, only OutputRate for
+	// the others.
+	Supports(rate int) bool
+	// Reopen closes the device, once what it holds has played, and opens
+	// name at rate. Pause and volume carry over. On error the output is
+	// closed: Lost reports true until a Reopen succeeds.
+	Reopen(name string, rate int) error
+	// Lost reports that the device stopped without being asked to (it was
+	// unplugged, say) or that a Reopen failed.
+	Lost() bool
 }
 
+// ErrRateUnsupported: a native device can't run at the rate asked for.
+var ErrRateUnsupported = errors.New("audio: the device does not take this sample rate")
+
+// Native reports whether name is a native device: an ALSA hw: name, opened as
+// is, run at the music's own rate.
+func Native(name string) bool { return strings.HasPrefix(name, "hw:") }
+
 type DeviceOptions struct {
 	// Name selects a playback device by name or ALSA id; "" means default.
+	// A hw: name (see Native) is opened as is.
 	Name string
 	// Null uses miniaudio's null backend (for tests and headless runs).
 	Null bool
-	// RingFrames is the ring size; 0 means 500 ms.
+	// Rate is the starting rate of a native device; 0 means OutputRate.
+	// Other devices always run at OutputRate.
+	Rate int
+	// RingFrames is the ring size; 0 means 500 ms at the device's rate.
 	RingFrames int
 }
 
-type device struct{}
+// device is the single global playback device. Its methods may be called
+// from several goroutines; mu orders Reopen against the rest.
+type device struct {
+	mu   sync.Mutex
+	null bool
+	ring int // RingFrames as asked; 0: half a second at each rate
+
+	name    string
+	refused map[string]map[int]bool // per device name, the rates it didn't take
+	checked time.Time               // when Lost last looked for the card
+	gone    bool                    // the card wasn't there then
+}
 
 // OpenDevice opens the single global playback device.
 func OpenDevice(o DeviceOptions) (Output, error) {
-	ring := o.RingFrames
-	if ring == 0 {
-		ring = OutputRate / 2
+	d := &device{null: o.Null, ring: o.RingFrames, refused: map[string]map[int]bool{}}
+	rate := deviceRate(o.Name, o.Rate)
+	if err := d.open(o.Name, rate, false); err != nil {
+		return nil, err
+	}
+	return d, nil
+}
+
+func deviceRate(name string, rate int) int {
+	if !Native(name) || rate <= 0 {
+		return OutputRate
+	}
+	return rate
+}
+
+func (d *device) ringFrames(rate int) int {
+	if d.ring > 0 {
+		return d.ring
 	}
-	var name *C.char
-	if o.Name != "" {
-		name = C.CString(o.Name)
-		defer C.free(unsafe.Pointer(name))
+	return rate / 2
+}
+
+// open opens name at rate; reopen: close the open device first.
+func (d *device) open(name string, rate int, reopen bool) error {
+	var cname *C.char
+	if name != "" {
+		cname = C.CString(name)
+		defer C.free(unsafe.Pointer(cname))
 	}
-	null := 0
-	if o.Null {
+	null, native := 0, 0
+	if d.null {
 		null = 1
 	}
-	if rc := C.mss_device_open(name, C.int(null), C.uint32_t(ring)); rc != 0 {
-		return nil, fmt.Errorf("audio: open device: miniaudio result %d", int(rc))
+	if Native(name) && !d.null {
+		native = 1
+	}
+	var rc C.int
+	start := time.Now()
+	if reopen {
+		rc = C.mss_device_reopen(cname, C.int(null), C.uint32_t(rate), C.uint32_t(d.ringFrames(rate)), C.int(native), 1)
+	} else {
+		rc = C.mss_device_open(cname, C.int(null), C.uint32_t(rate), C.uint32_t(d.ringFrames(rate)), C.int(native))
+	}
+	switch {
+	case rc == C.MSS_RATE_UNSUPPORTED:
+		if d.refused[name] == nil {
+			d.refused[name] = map[int]bool{}
+		}
+		d.refused[name][rate] = true
+		return fmt.Errorf("%w: %d Hz", ErrRateUnsupported, rate)
+	case rc != 0:
+		return fmt.Errorf("audio: open device: miniaudio result %d", int(rc))
+	}
+	d.name, d.gone, d.checked = name, false, time.Time{}
+	if native == 1 {
+		log.Printf("audio: %s at %d Hz, %d-bit (opened in %d ms)", name, rate, int(C.mss_device_format_bits()), time.Since(start).Milliseconds())
+	}
+	return nil
+}
+
+func (d *device) Reopen(name string, rate int) error {
+	d.mu.Lock()
+	defer d.mu.Unlock()
+	return d.open(name, deviceRate(name, rate), true)
+}
+
+func (d *device) Rate() int { return int(C.mss_device_rate()) }
+
+func (d *device) Supports(rate int) bool {
+	d.mu.Lock()
+	defer d.mu.Unlock()
+	if !Native(d.name) || d.null {
+		return rate == OutputRate
+	}
+	return !d.refused[d.name][rate]
+}
+
+// FormatBits is the sample size the device takes: 32 for float or S32.
+func (d *device) FormatBits() int { return int(C.mss_device_format_bits()) }
+
+// cardCheckEvery spaces the checks that a native device's card is still
+// there.
+const cardCheckEvery = 200 * time.Millisecond
+
+// Lost reports a device that stopped by itself, or a native device whose
+// card went away. miniaudio doesn't stop an ALSA device whose card is
+// unplugged (its wait loop spins on the error, and no stop notification
+// comes), and asking ALSA for the state from here would race with the
+// audio thread, so it looks for the card in /proc/asound instead.
+func (d *device) Lost() bool {
+	if C.mss_device_lost() != 0 {
+		return true
+	}
+	d.mu.Lock()
+	defer d.mu.Unlock()
+	if !Native(d.name) || d.null {
+		return false
+	}
+	if now := time.Now(); now.Sub(d.checked) >= cardCheckEvery {
+		d.checked = now
+		d.gone = !cardPresent(d.name)
 	}
-	return device{}, nil
+	return d.gone
 }
 
-func (device) Write(frames []float32) int {
+// cardPresent reports whether a native device's card is in /proc/asound:
+// "/proc/asound/E30" for hw:CARD=E30, "/proc/asound/card1" for hw:1.
+var cardPresent = func(device string) bool {
+	id := CardID(device)
+	if _, err := strconv.Atoi(id); err == nil {
+		id = "card" + id
+	}
+	_, err := os.Stat("/proc/asound/" + id)
+	return err == nil
+}
+
+func (d *device) Write(frames []float32) int {
 	if len(frames) < 2 {
 		return 0
 	}
 	return int(C.mss_device_write((*C.float)(unsafe.Pointer(&frames[0])), C.uint32_t(len(frames)/2)))
 }
 
-func (device) Consumed() uint64 { return uint64(C.mss_device_consumed()) }
+func (d *device) Consumed() uint64 { return uint64(C.mss_device_consumed()) }
 
-func (device) SetPaused(p bool) {
+func (d *device) SetPaused(p bool) {
 	v := 0
 	if p {
 		v = 1
@@ -79,9 +220,14 @@ func (device) SetPaused(p bool) {
 	C.mss_device_set_paused(C.int(v))
 }
 
-func (device) SetVolume(v float32) { C.mss_device_set_volume(C.float(v)) }
-func (device) Flush()              { C.mss_device_flush() }
-func (device) Close() error        { C.mss_device_close(); return nil }
+func (d *device) SetVolume(v float32) { C.mss_device_set_volume(C.float(v)) }
+func (d *device) Flush()              { C.mss_device_flush() }
+func (d *device) Close() error {
+	d.mu.Lock()
+	defer d.mu.Unlock()
+	C.mss_device_close()
+	return nil
+}
 
 // stopDeviceForTest is a test hook: it stops the device callback, as a
 // failed or unplugged device would, without closing the device.
diff --git a/internal/audio/shim.h b/internal/audio/shim.h
index 3a8e529..fd32e19 100644
--- a/internal/audio/shim.h
+++ b/internal/audio/shim.h
@@ -33,10 +33,21 @@ int mss_resampler_process(mss_resampler* r, const float* in, uint32_t* in_frames
 uint32_t mss_resampler_input_latency(mss_resampler* r);
 void mss_resampler_free(mss_resampler* r);
 
-/* Output device: one global 48 kHz stereo float32 device fed from a
-   single-producer ring. The device callback never blocks. */
-int mss_device_open(const char* device_name, int null_backend, uint32_t ring_frames);
+/* Output device: one global stereo device fed from a single-producer ring
+   of float32. The device callback never blocks. A native device (an ALSA
+   name used as is) runs at the rate asked for, in its most precise format;
+   other devices run at the rate asked for through miniaudio's conversion. */
+#define MSS_RATE_UNSUPPORTED (-1000)
+
+int mss_device_open(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native);
+/* Closes the open device (letting ALSA's buffer play out when tail) and
+   opens another; the consumed count, pause and volume carry over. */
+int mss_device_reopen(const char* device_name, int null_backend, uint32_t rate, uint32_t ring_frames, int native, int tail);
 void mss_device_close(void);
+uint32_t mss_device_rate(void);
+int mss_device_format_bits(void);
+/* 1 once the device stopped without being asked (unplugged, say). */
+int mss_device_lost(void);
 uint32_t mss_device_write(const float* frames, uint32_t frame_count);
 uint32_t mss_device_space(void);
 uint64_t mss_device_consumed(void);
diff --git a/internal/audio/tap.go b/internal/audio/tap.go
index cff551f..2d6296f 100644
--- a/internal/audio/tap.go
+++ b/internal/audio/tap.go
@@ -89,6 +89,9 @@ func (t *Tap) Window(dst []float32) int {
 }
 
 func (t *Tap) Consumed() uint64    { return t.out.Consumed() }
+func (t *Tap) Rate() int           { return t.out.Rate() }
+func (t *Tap) Supports(r int) bool { return t.out.Supports(r) }
+func (t *Tap) Lost() bool          { return t.out.Lost() }
 func (t *Tap) SetVolume(v float32) { t.out.SetVolume(v) }
 func (t *Tap) Close() error        { return t.out.Close() }
 
@@ -106,6 +109,15 @@ func (t *Tap) SetPaused(p bool) {
 	}
 }
 
+// Reopen passes the reopen on; what the tap kept is dropped.
+func (t *Tap) Reopen(name string, rate int) error {
+	err := t.out.Reopen(name, rate)
+	t.mu.Lock()
+	t.floor = t.written
+	t.mu.Unlock()
+	return err
+}
+
 // Flush discards what the device buffered and what the tap kept.
 func (t *Tap) Flush() {
 	t.out.Flush()
```

- [ ] **Step 4: Run the tests to see them pass.**

Run: `go vet ./... && go test -race ./internal/audio`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/audio/bench_test.go internal/audio/cards_test.go internal/audio/device_manual_test.go internal/audio/device_test.go internal/audio/fakes_test.go internal/audio/cards.go internal/audio/device.c internal/audio/device.go internal/audio/shim.h internal/audio/tap.go
git commit -m "audio: native devices at the music's rate, reopen and loss; the card list"
```


### Task 2: Hardware volume on the DAC's own control

A native device's volume moves to its card's mixer, through `libasound` loaded at run time. The control goes to the nearest step at or above the wanted level, software makes up the rest, and mute uses the switch. The level is set before the device starts.

**Files:**
- Modify: `internal/audio/device.go`
- Create: `internal/audio/mixer.c`
- Create: `internal/audio/mixer.go`
- Modify: `internal/audio/shim.h`
- Test: `internal/audio/device_manual_test.go`
- Test: `internal/audio/mixer_test.go`

**Interfaces:**
- Consumes (Task 1): `device`, `cardPresent`, `Native` and `CardID`.
- Produces (unexported):
  - **The control:** `hwControl` (`Range`, `Set`, `HasSwitch`, `SetSwitch`), and `hwVolume.apply(linear float32) (soft float32, err error)`.
  - **The device:** `device.useMixer(name)` and `device.applyVolume()`.
  - **Test seams:** the variable `openMixer func(card string) (hwControl, error)`, and `mixerCard(device string) string`.
  - **C:** `mss_mixer_*` in `shim.h`.

- [ ] **Step 1: Write the failing tests.** Apply with `git apply`:

```diff
diff --git a/internal/audio/device_manual_test.go b/internal/audio/device_manual_test.go
index f0a21d3..6bab8c1 100644
--- a/internal/audio/device_manual_test.go
+++ b/internal/audio/device_manual_test.go
@@ -149,3 +149,36 @@ func TestNativeProbeManual(t *testing.T) {
 	}
 	t.Logf("lost=%v", out.Lost())
 }
+
+// TestMixerProbeManual reads a card's hardware volume, moves it, mutes and
+// unmutes it, and puts it back. Nothing plays. Skipped unless
+// MSS_MIXER_PROBE names the card, for example: MSS_MIXER_PROBE=hw:E30
+func TestMixerProbeManual(t *testing.T) {
+	card := os.Getenv("MSS_MIXER_PROBE")
+	if card == "" {
+		t.Skip("set MSS_MIXER_PROBE=hw:<card> to probe a card's hardware volume (silent)")
+	}
+	ctl, err := openALSAMixer(card)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer closeALSAMixer()
+	m := ctl.(alsaMixer)
+	was, err := m.get()
+	if err != nil {
+		t.Fatal(err)
+	}
+	lo, hi := ctl.Range()
+	t.Logf("range %v..%v dB, switch %v, now %v dB", lo, hi, ctl.HasSwitch(), was)
+	defer func() {
+		got, err := ctl.Set(was)
+		t.Logf("restored to %v dB (%v)", got, err)
+	}()
+	for _, db := range []float64{-10, -10.4, -20, -59.5} {
+		got, err := ctl.Set(db)
+		t.Logf("set %v dB: got %v (%v)", db, got, err)
+	}
+	if ctl.HasSwitch() {
+		t.Logf("switch off: %v, on: %v", ctl.SetSwitch(false), ctl.SetSwitch(true))
+	}
+}
diff --git a/internal/audio/mixer_test.go b/internal/audio/mixer_test.go
new file mode 100644
index 0000000..11fd47f
--- /dev/null
+++ b/internal/audio/mixer_test.go
@@ -0,0 +1,179 @@
+package audio
+
+import (
+	"errors"
+	"math"
+	"testing"
+)
+
+// fakeControl is a hardware volume with steps of step dB from min to max.
+type fakeControl struct {
+	min, max, step float64
+	level          float64
+	hasSwitch      bool
+	on             bool
+	sets           []float64
+	switches       []bool
+	err            error
+}
+
+func (f *fakeControl) Range() (float64, float64) { return f.min, f.max }
+
+func (f *fakeControl) Set(db float64) (float64, error) {
+	if f.err != nil {
+		return 0, f.err
+	}
+	f.sets = append(f.sets, db)
+	steps := math.Ceil((db-f.min)/f.step - 1e-9) // at or above
+	f.level = math.Min(f.max, f.min+steps*f.step)
+	return f.level, nil
+}
+
+func (f *fakeControl) HasSwitch() bool { return f.hasSwitch }
+
+func (f *fakeControl) SetSwitch(on bool) error {
+	f.switches = append(f.switches, on)
+	f.on = on
+	return nil
+}
+
+func linearDB(db float64) float32 { return float32(math.Pow(10, db/20)) }
+
+func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }
+
+func TestHWVolumeWholeDBStepsLeaveSoftwareAtUnity(t *testing.T) {
+	c := &fakeControl{min: -127, max: 0, step: 1}
+	h := &hwVolume{ctl: c}
+	for _, db := range []float64{0, -1, -10, -37, -60} {
+		soft, err := h.apply(linearDB(db))
+		if err != nil {
+			t.Fatal(err)
+		}
+		if !near(soft, 1) || c.level != db {
+			t.Errorf("%v dB: hardware %v, software %v; want %v and 1", db, c.level, soft, db)
+		}
+	}
+}
+
+func TestHWVolumeCoarseStepsMakeUpTheRestInSoftware(t *testing.T) {
+	c := &fakeControl{min: -60, max: 0, step: 3}
+	h := &hwVolume{ctl: c}
+	soft, err := h.apply(linearDB(-10)) // the hardware goes to -9, software takes -1
+	if err != nil {
+		t.Fatal(err)
+	}
+	if c.level != -9 || !near(soft, linearDB(-1)) {
+		t.Errorf("hardware %v, software %v; want -9 and -1 dB", c.level, soft)
+	}
+}
+
+func TestHWVolumeBelowTheRangeGoesToSoftware(t *testing.T) {
+	c := &fakeControl{min: -40, max: 0, step: 0.5}
+	h := &hwVolume{ctl: c}
+	soft, err := h.apply(linearDB(-50))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if c.level != -40 || !near(soft, linearDB(-10)) {
+		t.Errorf("hardware %v, software %v; want -40 and -10 dB", c.level, soft)
+	}
+}
+
+func TestHWVolumeNeverAmplifies(t *testing.T) {
+	c := &fakeControl{min: -60, max: -6, step: 1} // a control that can't reach 0 dB
+	h := &hwVolume{ctl: c}
+	soft, err := h.apply(1)
+	if err != nil {
+		t.Fatal(err)
+	}
+	if c.level != -6 || !near(soft, 1) {
+		t.Errorf("hardware %v, software %v; want -6 and 1", c.level, soft)
+	}
+}
+
+func TestHWVolumeMuteUsesTheSwitchAndComesBack(t *testing.T) {
+	c := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true, on: true}
+	h := &hwVolume{ctl: c}
+	if soft, _ := h.apply(0); soft != 0 || c.on {
+		t.Fatalf("mute: switch on=%v, software %v", c.on, soft)
+	}
+	c.sets = nil
+	if soft, _ := h.apply(linearDB(-20)); !near(soft, 1) || !c.on || c.level != -20 {
+		t.Fatalf("unmute: switch on=%v, level %v, software %v", c.on, c.level, soft)
+	}
+	if len(c.sets) != 1 || len(c.switches) != 2 {
+		t.Errorf("the level is set before the switch goes on: sets %v, switches %v", c.sets, c.switches)
+	}
+}
+
+func TestHWVolumeMuteWithoutASwitchGoesToTheBottom(t *testing.T) {
+	c := &fakeControl{min: -90, max: 0, step: 1}
+	h := &hwVolume{ctl: c}
+	if soft, _ := h.apply(0); soft != 0 || c.level != -90 {
+		t.Errorf("hardware %v, software %v; want -90 and 0", c.level, soft)
+	}
+}
+
+func TestHWVolumeTurnsOnASwitchLeftOff(t *testing.T) {
+	c := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true, on: false}
+	h := &hwVolume{ctl: c, muted: c.HasSwitch()} // as the device makes it
+	if _, err := h.apply(1); err != nil || !c.on {
+		t.Errorf("switch on=%v, err %v", c.on, err)
+	}
+}
+
+func TestHWVolumeErrorsComeBack(t *testing.T) {
+	boom := errors.New("boom")
+	h := &hwVolume{ctl: &fakeControl{min: -60, max: 0, step: 1, err: boom}}
+	if _, err := h.apply(0.5); !errors.Is(err, boom) {
+		t.Errorf("err %v", err)
+	}
+}
+
+func TestMixerCard(t *testing.T) {
+	for in, want := range map[string]string{
+		"hw:CARD=E30,DEV=0": "hw:E30",
+		"hw:E30,0":          "hw:E30",
+		"hw:1,0":            "hw:1",
+		"hw:CARD=E30":       "hw:E30",
+	} {
+		if got := mixerCard(in); got != want {
+			t.Errorf("mixerCard(%q) = %q, want %q", in, got, want)
+		}
+	}
+}
+
+func TestDeviceVolumeMovesToTheDACsControl(t *testing.T) {
+	present := false
+	opened := 0
+	ctl := &fakeControl{min: -127, max: 0, step: 1, hasSwitch: true}
+	oldPresent, oldOpen := cardPresent, openMixer
+	cardPresent = func(string) bool { return present }
+	openMixer = func(card string) (hwControl, error) {
+		opened++
+		if card != "hw:E30" {
+			t.Errorf("mixer opened for %q, want hw:E30", card)
+		}
+		return ctl, nil
+	}
+	defer func() { cardPresent, openMixer = oldPresent, oldOpen }()
+
+	d := &device{linear: linearDB(-12)}
+	d.useMixer("hw:CARD=E30,DEV=0") // unplugged: no mixer, and no message about one
+	if opened != 0 || d.hw != nil {
+		t.Fatalf("a missing card's mixer was opened (%d)", opened)
+	}
+	present = true
+	d.useMixer("hw:CARD=E30,DEV=0") // plugged in now: tried again
+	if opened != 1 || d.hw == nil || ctl.level != -12 || !ctl.on {
+		t.Fatalf("opened %d, level %v, switch %v; want the DAC at -12 dB and on", opened, ctl.level, ctl.on)
+	}
+	d.SetVolume(linearDB(-30))
+	if ctl.level != -30 {
+		t.Fatalf("level %v after SetVolume, want -30", ctl.level)
+	}
+	d.useMixer("") // MiSTer audio: software volume, the DAC left as it is
+	if d.hw != nil || ctl.level != -30 {
+		t.Fatalf("hw %v, level %v", d.hw, ctl.level)
+	}
+}
```

- [ ] **Step 2: Run the tests to see them fail.**

Run: `go test -race ./internal/audio`
Expected: FAIL, build failure, for example `undefined: openALSAMixer`.

- [ ] **Step 3: Write the implementation.** Apply with `git apply`:

```diff
diff --git a/internal/audio/device.go b/internal/audio/device.go
index 7952b85..1a8d625 100644
--- a/internal/audio/device.go
+++ b/internal/audio/device.go
@@ -83,11 +83,15 @@ type device struct {
 	refused map[string]map[int]bool // per device name, the rates it didn't take
 	checked time.Time               // when Lost last looked for the card
 	gone    bool                    // the card wasn't there then
+
+	linear float32   // the volume asked for
+	hw     *hwVolume // the native device's hardware volume, if it has one
+	hwCard string    // the card hw drives
 }
 
 // OpenDevice opens the single global playback device.
 func OpenDevice(o DeviceOptions) (Output, error) {
-	d := &device{null: o.Null, ring: o.RingFrames, refused: map[string]map[int]bool{}}
+	d := &device{null: o.Null, ring: o.RingFrames, refused: map[string]map[int]bool{}, linear: 1}
 	rate := deviceRate(o.Name, o.Rate)
 	if err := d.open(o.Name, rate, false); err != nil {
 		return nil, err
@@ -109,8 +113,60 @@ func (d *device) ringFrames(rate int) int {
 	return rate / 2
 }
 
+// openMixer is the hardware volume for a card (a variable for tests).
+var openMixer = openALSAMixer
+
+// useMixer points the volume at name's card: its hardware volume for a
+// native device that has one, software otherwise. The level is set before
+// the device starts, so a DAC never plays a moment at its old level.
+func (d *device) useMixer(name string) {
+	card := ""
+	if Native(name) && !d.null {
+		card = mixerCard(name)
+	}
+	if card != d.hwCard {
+		if d.hw != nil {
+			closeALSAMixer()
+		}
+		d.hw, d.hwCard = nil, card
+		switch {
+		case card == "":
+		case !cardPresent(name):
+			d.hwCard = "" // not plugged in: the open fails anyway; try again next time
+		default:
+			ctl, err := openMixer(card)
+			if err != nil {
+				log.Printf("%v: the volume stays in software", err)
+			} else {
+				// muted: the switch may have been left off; the first level
+				// set turns it on.
+				d.hw = &hwVolume{ctl: ctl, muted: ctl.HasSwitch()}
+			}
+		}
+	}
+	d.applyVolume()
+}
+
+// applyVolume sets d.linear on the hardware control, if any, and the rest
+// in software.
+func (d *device) applyVolume() {
+	soft := d.linear
+	if d.hw != nil {
+		s, err := d.hw.apply(d.linear)
+		if err != nil {
+			log.Printf("%v: the volume goes back to software", err)
+			closeALSAMixer()
+			d.hw = nil
+		} else {
+			soft = s
+		}
+	}
+	C.mss_device_set_volume(C.float(soft))
+}
+
 // open opens name at rate; reopen: close the open device first.
 func (d *device) open(name string, rate int, reopen bool) error {
+	d.useMixer(name)
 	var cname *C.char
 	if name != "" {
 		cname = C.CString(name)
@@ -220,12 +276,21 @@ func (d *device) SetPaused(p bool) {
 	C.mss_device_set_paused(C.int(v))
 }
 
-func (d *device) SetVolume(v float32) { C.mss_device_set_volume(C.float(v)) }
-func (d *device) Flush()              { C.mss_device_flush() }
+func (d *device) SetVolume(v float32) {
+	d.mu.Lock()
+	defer d.mu.Unlock()
+	d.linear = v
+	d.applyVolume()
+}
+func (d *device) Flush() { C.mss_device_flush() }
 func (d *device) Close() error {
 	d.mu.Lock()
 	defer d.mu.Unlock()
 	C.mss_device_close()
+	if d.hw != nil {
+		closeALSAMixer()
+		d.hw, d.hwCard = nil, ""
+	}
 	return nil
 }
 
diff --git a/internal/audio/mixer.c b/internal/audio/mixer.c
new file mode 100644
index 0000000..cac8809
--- /dev/null
+++ b/internal/audio/mixer.c
@@ -0,0 +1,133 @@
+#include <dlfcn.h>
+#include <stddef.h>
+
+#include "shim.h"
+
+/* ---------- hardware volume ----------
+   ALSA's simple mixer, through libasound loaded at run time (as miniaudio
+   loads it), so the build needs no ALSA headers or libraries. One mixer at
+   a time: the card of the native device. */
+
+typedef struct snd_mixer snd_mixer_t;
+typedef struct snd_mixer_elem snd_mixer_elem_t;
+
+static struct {
+    void* lib;
+    int (*open)(snd_mixer_t**, int);
+    int (*attach)(snd_mixer_t*, const char*);
+    int (*selem_register)(snd_mixer_t*, void*, void**);
+    int (*load)(snd_mixer_t*);
+    int (*close)(snd_mixer_t*);
+    snd_mixer_elem_t* (*first_elem)(snd_mixer_t*);
+    snd_mixer_elem_t* (*elem_next)(snd_mixer_elem_t*);
+    int (*has_playback_volume)(snd_mixer_elem_t*);
+    int (*get_playback_dB_range)(snd_mixer_elem_t*, long*, long*);
+    int (*set_playback_dB_all)(snd_mixer_elem_t*, long, int);
+    int (*get_playback_dB)(snd_mixer_elem_t*, int, long*);
+    int (*has_playback_switch)(snd_mixer_elem_t*);
+    int (*set_playback_switch_all)(snd_mixer_elem_t*, int);
+} A;
+
+static snd_mixer_t* g_mixer;
+static snd_mixer_elem_t* g_elem;
+
+static int load_lib(void) {
+    if (A.lib != NULL) {
+        return 0;
+    }
+    void* lib = dlopen("libasound.so.2", RTLD_NOW);
+    if (lib == NULL) {
+        lib = dlopen("libasound.so", RTLD_NOW);
+    }
+    if (lib == NULL) {
+        return -1;
+    }
+#define SYM(field, name) \
+    if ((*(void**)&A.field = dlsym(lib, name)) == NULL) { dlclose(lib); return -1; }
+    SYM(open, "snd_mixer_open")
+    SYM(attach, "snd_mixer_attach")
+    SYM(selem_register, "snd_mixer_selem_register")
+    SYM(load, "snd_mixer_load")
+    SYM(close, "snd_mixer_close")
+    SYM(first_elem, "snd_mixer_first_elem")
+    SYM(elem_next, "snd_mixer_elem_next")
+    SYM(has_playback_volume, "snd_mixer_selem_has_playback_volume")
+    SYM(get_playback_dB_range, "snd_mixer_selem_get_playback_dB_range")
+    SYM(set_playback_dB_all, "snd_mixer_selem_set_playback_dB_all")
+    SYM(get_playback_dB, "snd_mixer_selem_get_playback_dB")
+    SYM(has_playback_switch, "snd_mixer_selem_has_playback_switch")
+    SYM(set_playback_switch_all, "snd_mixer_selem_set_playback_switch_all")
+#undef SYM
+    A.lib = lib;
+    return 0;
+}
+
+void mss_mixer_close(void) {
+    if (g_mixer != NULL) {
+        A.close(g_mixer);
+        g_mixer = NULL;
+        g_elem = NULL;
+    }
+}
+
+int mss_mixer_open(const char* card) {
+    mss_mixer_close();
+    if (load_lib() != 0) {
+        return -1;
+    }
+    snd_mixer_t* m;
+    if (A.open(&m, 0) < 0) {
+        return -2;
+    }
+    if (A.attach(m, card) < 0 || A.selem_register(m, NULL, NULL) < 0 || A.load(m) < 0) {
+        A.close(m);
+        return -3;
+    }
+    /* The first control with a playback volume in dB. */
+    for (snd_mixer_elem_t* e = A.first_elem(m); e != NULL; e = A.elem_next(e)) {
+        long lo, hi;
+        if (A.has_playback_volume(e) && A.get_playback_dB_range(e, &lo, &hi) == 0 && lo < hi) {
+            g_mixer = m;
+            g_elem = e;
+            return 0;
+        }
+    }
+    A.close(m);
+    return -4;
+}
+
+int mss_mixer_range(long* min_cdb, long* max_cdb) {
+    if (g_elem == NULL) {
+        return -1;
+    }
+    return A.get_playback_dB_range(g_elem, min_cdb, max_cdb);
+}
+
+int mss_mixer_set(long cdb, long* got_cdb) {
+    if (g_elem == NULL) {
+        return -1;
+    }
+    int r = A.set_playback_dB_all(g_elem, cdb, 1); /* the nearest step at or above */
+    if (r < 0) {
+        return r;
+    }
+    return A.get_playback_dB(g_elem, 0, got_cdb);
+}
+
+int mss_mixer_has_switch(void) {
+    return g_elem != NULL && A.has_playback_switch(g_elem);
+}
+
+int mss_mixer_set_switch(int on) {
+    if (g_elem == NULL) {
+        return -1;
+    }
+    return A.set_playback_switch_all(g_elem, on);
+}
+
+int mss_mixer_get(long* cdb) {
+    if (g_elem == NULL) {
+        return -1;
+    }
+    return A.get_playback_dB(g_elem, 0, cdb);
+}
diff --git a/internal/audio/mixer.go b/internal/audio/mixer.go
new file mode 100644
index 0000000..6712c35
--- /dev/null
+++ b/internal/audio/mixer.go
@@ -0,0 +1,120 @@
+package audio
+
+/*
+#include <stdlib.h>
+#include "shim.h"
+*/
+import "C"
+
+import (
+	"fmt"
+	"math"
+	"unsafe"
+)
+
+// hwControl is a card's playback volume in dB.
+type hwControl interface {
+	// Range is the control's lowest and highest level, in dB.
+	Range() (min, max float64)
+	// Set moves the control to the nearest step at or above db and returns
+	// the level it got.
+	Set(db float64) (got float64, err error)
+	HasSwitch() bool
+	SetSwitch(on bool) error
+}
+
+// hwVolume drives a hardware control from the app's linear volume.
+type hwVolume struct {
+	ctl   hwControl
+	muted bool // the switch is off
+}
+
+// apply sets the control for linear and returns the software gain still
+// to apply: 1 when the hardware reaches the level exactly, below 1 to make
+// up a step the hardware can't hit, 0 when muted.
+func (h *hwVolume) apply(linear float32) (float32, error) {
+	lo, hi := h.ctl.Range()
+	if linear <= 0 {
+		if h.ctl.HasSwitch() {
+			if err := h.ctl.SetSwitch(false); err != nil {
+				return 0, err
+			}
+			h.muted = true
+		} else if _, err := h.ctl.Set(lo); err != nil {
+			return 0, err
+		}
+		return 0, nil
+	}
+	// To the hundredth of a dB, ALSA's unit: -60 dB must not arrive as
+	// -59.99999996 and round up a step.
+	want := math.Round(2000*math.Log10(float64(linear))) / 100
+	got, err := h.ctl.Set(math.Max(lo, math.Min(hi, want)))
+	if err != nil {
+		return 0, err
+	}
+	if h.muted { // the level first, then sound: no blip at the old level
+		if err := h.ctl.SetSwitch(true); err != nil {
+			return 0, err
+		}
+		h.muted = false
+	}
+	soft := math.Pow(10, (want-got)/20)
+	if soft > 1 {
+		soft = 1 // the control can't go lower than got; never amplify
+	}
+	return float32(soft), nil
+}
+
+// mixerCard is the ALSA mixer name for a native device's card:
+// "hw:CARD=E30,DEV=0" and "hw:E30,0" give "hw:E30".
+func mixerCard(device string) string { return "hw:" + CardID(device) }
+
+// alsaMixer is the C mixer: one card at a time.
+type alsaMixer struct{}
+
+func openALSAMixer(card string) (hwControl, error) {
+	c := C.CString(card)
+	defer C.free(unsafe.Pointer(c))
+	if rc := C.mss_mixer_open(c); rc != 0 {
+		return nil, fmt.Errorf("audio: no hardware volume on %s (%d)", card, int(rc))
+	}
+	return alsaMixer{}, nil
+}
+
+func (alsaMixer) Range() (float64, float64) {
+	var lo, hi C.long
+	C.mss_mixer_range(&lo, &hi)
+	return float64(lo) / 100, float64(hi) / 100
+}
+
+func (alsaMixer) Set(db float64) (float64, error) {
+	var got C.long
+	if rc := C.mss_mixer_set(C.long(math.Round(db*100)), &got); rc < 0 {
+		return 0, fmt.Errorf("audio: set hardware volume: %d", int(rc))
+	}
+	return float64(got) / 100, nil
+}
+
+// get reads the level now (tests and probes).
+func (alsaMixer) get() (float64, error) {
+	var v C.long
+	if rc := C.mss_mixer_get(&v); rc < 0 {
+		return 0, fmt.Errorf("audio: read hardware volume: %d", int(rc))
+	}
+	return float64(v) / 100, nil
+}
+
+func (alsaMixer) HasSwitch() bool { return C.mss_mixer_has_switch() != 0 }
+
+func (alsaMixer) SetSwitch(on bool) error {
+	v := 0
+	if on {
+		v = 1
+	}
+	if rc := C.mss_mixer_set_switch(C.int(v)); rc < 0 {
+		return fmt.Errorf("audio: set hardware mute: %d", int(rc))
+	}
+	return nil
+}
+
+func closeALSAMixer() { C.mss_mixer_close() }
diff --git a/internal/audio/shim.h b/internal/audio/shim.h
index fd32e19..93cd543 100644
--- a/internal/audio/shim.h
+++ b/internal/audio/shim.h
@@ -56,4 +56,17 @@ void mss_device_set_volume(float volume);
 void mss_device_flush(void);
 void mss_device_stop_for_test(void);
 
+/* Hardware volume: the first playback volume in dB on a card's simple
+   mixer (card is an ALSA name such as "hw:E30"), through libasound loaded
+   at run time. Values are in hundredths of a dB. Negative results are
+   errors. */
+int mss_mixer_open(const char* card);
+void mss_mixer_close(void);
+int mss_mixer_range(long* min_cdb, long* max_cdb);
+/* Sets the nearest step at or above cdb and reads back the one set. */
+int mss_mixer_set(long cdb, long* got_cdb);
+int mss_mixer_get(long* cdb);
+int mss_mixer_has_switch(void);
+int mss_mixer_set_switch(int on);
+
 #endif
```

- [ ] **Step 4: Run the tests to see them pass.**

Run: `go vet ./... && go test -race ./internal/audio`
Expected: PASS.
With zig 0.16.0 on the PATH, also run `make mister mister-test`. Expected: the glibc check reports ≤ 2.31.

- [ ] **Step 5: Commit.**

```bash
git add internal/audio/device_manual_test.go internal/audio/mixer_test.go internal/audio/device.go internal/audio/mixer.c internal/audio/mixer.go internal/audio/shim.h
git commit -m "audio: the volume drives a DAC's own hardware control"
```


### Task 3: The engine: each song at its own rate, SwitchOutput, device loss

The engine picks a target rate per song: the song's own when the device supports it, else 48 kHz. When it differs from the device's rate, the engine lets everything buffered play out and reopens the device. Positions use each stretch's own rate. `SwitchOutput` moves playback to another device and carries on from what was heard. A lost device becomes `EventDeviceLost`.

**Files:**
- Modify: `cmd/mss-cli/main.go`
- Modify: `internal/audio/engine.go`
- Test: `internal/audio/engine_rate_test.go`

**Interfaces:**
- Consumes (Task 1): `Output.Rate/Supports/Reopen/Lost`, `ErrRateUnsupported`, `Native`, and the fakes.
- Produces:
  - **The options:** `audio.EngineOptions.OutputName string`, the device's name for reopening it.
  - **The event:** `audio.EventDeviceLost`, with TrackID 0 and `Err` set when a reopen failed.
  - **The switch:** `(*audio.Engine).SwitchOutput(name string) error`, blocking until it ran, and `audio.ErrEngineClosed`.
  - **Also:** `cmd/mss-cli` passes `OutputName`.

- [ ] **Step 1: Write the failing tests.** Apply with `git apply`:

```diff
diff --git a/internal/audio/engine_rate_test.go b/internal/audio/engine_rate_test.go
new file mode 100644
index 0000000..24a783d
--- /dev/null
+++ b/internal/audio/engine_rate_test.go
@@ -0,0 +1,218 @@
+package audio
+
+import (
+	"errors"
+	"testing"
+	"time"
+)
+
+const testDAC = "hw:CARD=DAC,DEV=0"
+
+func newRateEngine(out *fakeOutput) *Engine {
+	return NewEngine(EngineOptions{Output: out, OutputName: testDAC, OpenDecoder: fakeOpen, ChunkFrames: 256, Poll: time.Millisecond})
+}
+
+func TestEngineRateChangeWaitsForTheOldSongToPlayOut(t *testing.T) {
+	out := newNativeOutput(100000, 44100, 48000, 96000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	a, b := ramp(700, 0), ramp(900, 700)
+	e.Play(Track{ID: 1, Source: newFakeSource(a, 44100)})
+	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, 96000)})
+	waitFor(t, "the first song written", func() bool { return len(out.written())/2 == 700 })
+	if r := out.reopenCalls(); len(r) != 1 || r[0].rate != 44100 || r[0].name != testDAC {
+		t.Fatalf("reopens %+v, want one at 44100 before the first song", r)
+	}
+	expectEvent(t, e, EventStarted, 1)
+
+	out.consume(350)
+	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 350*time.Second/44100 {
+		t.Fatalf("position %v %v %v, want song 1 at 350 frames of 44.1 kHz", id, pos, ok)
+	}
+	out.consume(349)
+	time.Sleep(20 * time.Millisecond)
+	if r := out.reopenCalls(); len(r) != 1 {
+		t.Fatalf("reopened with a frame of the old song still to play: %+v", r)
+	}
+	out.consume(1)
+	waitFor(t, "the reopen at 96 kHz", func() bool { return len(out.reopenCalls()) == 2 })
+	r := out.reopenCalls()[1]
+	if r.rate != 96000 || r.written != 700 || r.consumed != 700 {
+		t.Fatalf("reopen %+v, want 96000 once all 700 frames played", r)
+	}
+	expectEvent(t, e, EventEnded, 1)
+	expectEvent(t, e, EventStarted, 2)
+
+	waitFor(t, "the second song written", func() bool { return len(out.written())/2 == 1600 })
+	out.consume(450)
+	if id, pos, ok := e.Position(); !ok || id != 2 || pos != 450*time.Second/96000 {
+		t.Fatalf("position %v %v %v, want song 2 at 450 frames of 96 kHz", id, pos, ok)
+	}
+	playOut(t, out, 1600)
+	got := out.written()
+	want := append(append([]float32(nil), a...), b...)
+	for i := range want {
+		if got[i] != want[i] {
+			t.Fatalf("sample %d = %v, want %v: native rates must not be resampled", i, got[i], want[i])
+		}
+	}
+}
+
+func TestEngineSameNativeRateStaysGapless(t *testing.T) {
+	out := newNativeOutput(100000, 48000, 96000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	a, b := ramp(700, 0), ramp(900, 700)
+	e.Play(Track{ID: 1, Source: newFakeSource(a, 96000)})
+	e.QueueNext(Track{ID: 2, Source: newFakeSource(b, 96000)})
+	waitFor(t, "both songs written", func() bool { return len(out.written())/2 == 1600 })
+	if r := out.reopenCalls(); len(r) != 1 || r[0].rate != 96000 {
+		t.Fatalf("reopens %+v, want just the one at 96000", r)
+	}
+	expectEvent(t, e, EventStarted, 1)
+	out.consume(700)
+	expectEvent(t, e, EventEnded, 1)
+	expectEvent(t, e, EventStarted, 2)
+}
+
+func TestEngineStopDuringARateChangeDrainCancelsIt(t *testing.T) {
+	out := newNativeOutput(100000, 44100, 96000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	src2 := newFakeSource(ramp(900, 0), 96000)
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(700, 0), 44100)})
+	e.QueueNext(Track{ID: 2, Source: src2})
+	waitFor(t, "the first song written", func() bool { return len(out.written())/2 == 700 })
+	time.Sleep(10 * time.Millisecond) // the engine now waits for the old song to play out
+	e.Stop()
+	waitFor(t, "the queued song closed", src2.isClosed)
+	time.Sleep(10 * time.Millisecond)
+	if r := out.reopenCalls(); len(r) != 1 {
+		t.Fatalf("reopens %+v: a stopped drain must not reopen", r)
+	}
+}
+
+func TestEngineRateTheDeviceLacksIsResampledTo48k(t *testing.T) {
+	out := newNativeOutput(100000, 44100, 48000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(2205, 0), 22050)})
+	expectEvent(t, e, EventStarted, 1)
+	playOut(t, out, 4700)
+	if r := out.reopenCalls(); len(r) != 0 {
+		t.Fatalf("reopens %+v, want none: the device already runs at 48 kHz", r)
+	}
+	if n := len(out.written()) / 2; n < 4700 || n > 4900 {
+		t.Fatalf("%d frames for 0.1 s, want about 4800 (resampled to 48 kHz)", n)
+	}
+}
+
+func TestEngineRateRefusedAtReopenFallsBackTo48k(t *testing.T) {
+	out := newNativeOutput(100000, 44100, 48000, 88200)
+	out.rate = 44100
+	out.refuse = []int{88200}
+	e := newRateEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(882, 0), 88200)})
+	expectEvent(t, e, EventStarted, 1)
+	r := out.reopenCalls()
+	if len(r) != 2 || r[0].rate != 88200 || r[1].rate != 48000 {
+		t.Fatalf("reopens %+v, want 88200 refused then 48000", r)
+	}
+	playOut(t, out, 470)
+	if out.Supports(88200) {
+		t.Error("a refused rate is still reported as supported")
+	}
+}
+
+func TestEngineReopenFailureHoldsTheSongForSwitchOutput(t *testing.T) {
+	out := newNativeOutput(100000, 44100, 48000)
+	out.reopenErr = errors.New("unplugged")
+	e := newRateEngine(out)
+	defer e.Close()
+
+	src := newFakeSource(ramp(441, 0), 44100)
+	e.Play(Track{ID: 1, Source: src})
+	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost {
+		t.Fatalf("event %+v, want EventDeviceLost", ev)
+	}
+	if src.isClosed() {
+		t.Fatal("the song was dropped; it must wait for a device")
+	}
+	if err := e.SwitchOutput("default"); err != nil {
+		t.Fatal(err)
+	}
+	expectEvent(t, e, EventStarted, 1)
+	playOut(t, out, 470) // 441 frames at 44.1 kHz, resampled to 48 kHz on the default device
+	expectEvent(t, e, EventEnded, 1)
+}
+
+func TestEngineReportsALostDeviceOncePerLoss(t *testing.T) {
+	out := newFakeOutput(1000)
+	e := newTestEngine(out)
+	defer e.Close()
+
+	out.setLost()
+	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost || ev.TrackID != 0 {
+		t.Fatalf("event %+v, want EventDeviceLost", ev)
+	}
+	noEvent(t, e, 20*time.Millisecond)
+	if err := e.SwitchOutput("default"); err != nil {
+		t.Fatal(err)
+	}
+	noEvent(t, e, 20*time.Millisecond)
+	out.setLost()
+	if ev := nextEvent(t, e); ev.Kind != EventDeviceLost {
+		t.Fatalf("event %+v, want a second EventDeviceLost", ev)
+	}
+}
+
+func TestEngineSwitchOutputCarriesOnFromWhatWasHeard(t *testing.T) {
+	out := newNativeOutput(2000, 48000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(10000, 0), 48000)})
+	expectEvent(t, e, EventStarted, 1)
+	waitFor(t, "the ring full", func() bool { return len(out.written())/2 == 2000 })
+	out.consume(500)
+	if err := e.SwitchOutput("default"); err != nil {
+		t.Fatal(err)
+	}
+	r := out.reopenCalls()
+	if len(r) != 1 || r[0].name != "default" || r[0].rate != 48000 {
+		t.Fatalf("reopens %+v, want default at 48000", r)
+	}
+	if id, pos, ok := e.Position(); !ok || id != 1 || pos != 500*time.Second/48000 {
+		t.Fatalf("position %v %v %v, want song 1 at frame 500", id, pos, ok)
+	}
+	cut := int(r[0].written)
+	waitFor(t, "more written after the switch", func() bool { return len(out.written())/2 > cut+10 })
+	if got, want := out.written()[2*cut], ramp(1, 500)[0]; got != want {
+		t.Fatalf("after the switch the song resumed at %v, want frame 500 (%v)", got, want)
+	}
+}
+
+func TestEngineSwitchOutputFailureKeepsThePreviousDevice(t *testing.T) {
+	out := newNativeOutput(2000, 48000)
+	e := newRateEngine(out)
+	defer e.Close()
+
+	e.Play(Track{ID: 1, Source: newFakeSource(ramp(10000, 0), 48000)})
+	expectEvent(t, e, EventStarted, 1)
+	out.consume(100)
+	out.reopenErr = errors.New("busy")
+	if err := e.SwitchOutput("hw:CARD=Other,DEV=0"); err == nil {
+		t.Fatal("no error from a device that won't open")
+	}
+	r := out.reopenCalls()
+	if len(r) != 2 || r[1].name != testDAC {
+		t.Fatalf("reopens %+v, want the failed one then %s again", r, testDAC)
+	}
+	playOut(t, out, 3000) // still playing
+}
```

- [ ] **Step 2: Run the tests to see them fail.**

Run: `go test -race ./internal/audio ./cmd/...`
Expected: FAIL, build failure, for example `undefined: EventDeviceLost`.

- [ ] **Step 3: Write the implementation.** Apply with `git apply`:

```diff
diff --git a/cmd/mss-cli/main.go b/cmd/mss-cli/main.go
index f0d3601..b789824 100644
--- a/cmd/mss-cli/main.go
+++ b/cmd/mss-cli/main.go
@@ -141,7 +141,7 @@ func run(cfgPath string, null, quitAtEnd bool, volume float64, args []string) er
 		return err
 	}
 	defer out.Close()
-	eng := audio.NewEngine(audio.EngineOptions{Output: out})
+	eng := audio.NewEngine(audio.EngineOptions{Output: out, OutputName: dev})
 	defer eng.Close()
 
 	p := player.New(player.Options{
diff --git a/internal/audio/engine.go b/internal/audio/engine.go
index 08b214c..00ce92c 100644
--- a/internal/audio/engine.go
+++ b/internal/audio/engine.go
@@ -3,6 +3,7 @@ package audio
 import (
 	"errors"
 	"io"
+	"math"
 	"sync"
 	"time"
 )
@@ -35,6 +36,10 @@ const (
 	// if the track wasn't the one being decoded). Playback carries on from
 	// wherever the decoder is; the caller decides whether to reopen.
 	EventSeekFailed
+	// EventDeviceLost: the output device stopped by itself (it was
+	// unplugged, say) or could not be reopened for a new sample rate.
+	// TrackID is 0. Nothing more is heard until SwitchOutput succeeds.
+	EventDeviceLost
 )
 
 type Event struct {
@@ -46,12 +51,18 @@ type Event struct {
 // ErrNotCurrent is the EventSeekFailed error when the track is not the one being decoded.
 var ErrNotCurrent = errors.New("audio: track is not the one being decoded")
 
+// ErrEngineClosed: the engine closed before the call could run.
+var ErrEngineClosed = errors.New("audio: engine closed")
+
 // ErrOpenTimeout is the EventError for a queued track that was still opening
 // when it was needed, openWait after the previous track finished decoding.
 var ErrOpenTimeout = errors.New("audio: next track took too long to open")
 
 type EngineOptions struct {
-	Output          Output
+	Output Output
+	// OutputName is the device Output was opened as: a sample rate change
+	// reopens it under this name (see Native).
+	OutputName      string
 	OpenDecoder     OpenDecoderFunc // default OpenDecoder
 	ResampleQuality int             // default DefaultResampleQuality
 	ChunkFrames     int             // default 2048
@@ -69,11 +80,12 @@ type voice struct {
 }
 
 // segment marks where, in the device's frame count, a stretch of one track
-// begins. id 0 means silence.
+// begins. id 0 means silence. rate is the device's rate for the stretch.
 type segment struct {
 	start uint64
 	id    uint64
 	base  time.Duration
+	rate  int
 }
 
 type openResult struct {
@@ -133,6 +145,9 @@ type Engine struct {
 	chainIn    uint64
 	written    uint64
 	finishedID uint64    // the track that ended (or was interrupted) while a Replace was pending, until the next track starts or stops
+	outName    string    // the device's name, for reopening it
+	held       *voice    // a track to start once the device is back (a reopen failed)
+	lostSent   bool      // EventDeviceLost went out for the current loss (monitor goroutine)
 	pending    []float32 // resampled output not yet taken by the device
 	pendBuf    []float32 // pending's backing array, reused so decoding doesn't allocate
 	scratch    []float32
@@ -170,7 +185,8 @@ func newEngine(o EngineOptions) *Engine {
 		quit:    make(chan struct{}),
 		closing: make(chan struct{}),
 		done:    make(chan struct{}),
-		segs:    []segment{{start: 0, id: 0}},
+		segs:    []segment{{start: 0, id: 0, rate: OutputRate}},
+		outName: o.OutputName,
 		scratch: make([]float32, o.ChunkFrames*2),
 	}
 	return e
@@ -323,7 +339,7 @@ func (e *Engine) Position() (id uint64, pos time.Duration, ok bool) {
 			if s.id == 0 {
 				return 0, 0, false
 			}
-			return s.id, s.base + time.Duration(c-s.start)*time.Second/OutputRate, true
+			return s.id, s.base + time.Duration(c-s.start)*time.Second/time.Duration(s.rate), true
 		}
 	}
 	return 0, 0, false
@@ -525,6 +541,7 @@ func (e *Engine) monitor() {
 			return
 		}
 		e.checkBoundaries()
+		e.checkLost()
 		e.mu.Lock()
 		evs := e.evq
 		e.evq = nil
@@ -559,6 +576,15 @@ func (e *Engine) checkBoundaries() {
 	e.mu.Unlock()
 }
 
+// checkLost reports a lost device once per loss (monitor goroutine).
+func (e *Engine) checkLost() {
+	lost := e.o.Output.Lost()
+	if lost && !e.lostSent {
+		e.queueEvent(Event{Kind: EventDeviceLost})
+	}
+	e.lostSent = lost
+}
+
 func (e *Engine) addSegment(s segment) {
 	e.mu.Lock()
 	e.segs = append(e.segs, s)
@@ -698,7 +724,7 @@ func (e *Engine) startSuccessorOrEnd() {
 		return
 	}
 	e.breakChain()
-	e.addSegment(segment{start: e.written + uint64(len(e.pending)/2), id: 0})
+	e.addSegment(segment{start: e.written + uint64(len(e.pending)/2), id: 0, rate: e.o.Output.Rate()})
 	e.ended = true
 }
 
@@ -714,20 +740,39 @@ func (e *Engine) breakChain() {
 	}
 }
 
+// targetRate is the device rate for music at rate: rate itself when the
+// device takes it (a native device), else OutputRate when it takes that,
+// else whatever it runs at.
+func (e *Engine) targetRate(rate int) int {
+	out := e.o.Output
+	switch {
+	case out.Supports(rate):
+		return rate
+	case out.Supports(OutputRate):
+		return OutputRate
+	}
+	return out.Rate()
+}
+
 func (e *Engine) startVoice(v *voice, gapless bool) {
 	rate := v.dec.SampleRate()
+	e.ended = false // a successor opening while we drain must wait its turn, not start
+	if e.targetRate(rate) != e.o.Output.Rate() && !e.changeRate(v, rate) {
+		return
+	}
+	devRate := e.o.Output.Rate()
 	var start uint64
 	if gapless && e.rs != nil && e.rs.InRate() == rate {
-		start = e.chainOut + e.chainIn*OutputRate/uint64(rate)
+		start = e.chainOut + e.chainIn*uint64(devRate)/uint64(rate)
 	} else {
 		e.breakChain()
 		start = e.written + uint64(len(e.pending)/2)
-		if rate != OutputRate {
-			rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
+		if rate != devRate {
+			rs, err := NewResampler(rate, devRate, e.o.ResampleQuality)
 			if err != nil {
 				e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
 				closeVoice(v)
-				e.addSegment(segment{start: start, id: 0})
+				e.addSegment(segment{start: start, id: 0, rate: devRate})
 				return
 			}
 			e.rs, e.chainOut, e.chainIn = rs, start, 0
@@ -737,7 +782,136 @@ func (e *Engine) startVoice(v *voice, gapless bool) {
 	e.finishedID = 0
 	e.ended = false
 	e.setBusy(v.t.Source, v.t.ID)
-	e.addSegment(segment{start: start, id: v.t.ID, base: v.t.Offset})
+	e.addSegment(segment{start: start, id: v.t.ID, base: v.t.Offset, rate: devRate})
+}
+
+// changeRate gets the device ready for v, music at rate: it lets everything
+// buffered play out, then reopens the device at the target rate. It reports
+// false, having closed v, when v must not start: a Stop or Play came while
+// it waited, the engine is closing, or the device was lost.
+func (e *Engine) changeRate(v *voice, rate int) bool {
+	e.breakChain()
+	stops := e.stops
+	for len(e.pending) > 0 || e.o.Output.Consumed() < e.written {
+		e.writePending()
+		if !e.wait(e.o.Poll) || e.stops != stops || e.cur != nil {
+			closeVoice(v)
+			return false
+		}
+	}
+	target := e.targetRate(rate)
+	if target == e.o.Output.Rate() {
+		return true // a SwitchOutput while we waited got there already
+	}
+	if err := e.reopen(e.outName, target); err != nil {
+		// Nothing can play: hold v until SwitchOutput brings a device back.
+		e.held = v
+		e.queueEvent(Event{Kind: EventDeviceLost, Err: err})
+		e.addSegment(segment{start: e.written, id: 0, rate: e.o.Output.Rate()})
+		return false
+	}
+	return true
+}
+
+// reopen reopens the device as name at rate, or at OutputRate when it
+// turns out not to take rate.
+func (e *Engine) reopen(name string, rate int) error {
+	err := e.o.Output.Reopen(name, rate)
+	if errors.Is(err, ErrRateUnsupported) && rate != OutputRate {
+		err = e.o.Output.Reopen(name, OutputRate) // Supports now says no to rate
+	}
+	return err
+}
+
+// SwitchOutput moves playback to the device name (see Native): the song
+// carries on from where it is heard. If name can't be opened, the previous
+// device is reopened and the error comes back.
+func (e *Engine) SwitchOutput(name string) error {
+	res := make(chan error, 1)
+	if !e.send(func() { res <- e.doSwitch(name) }, nil) {
+		return ErrEngineClosed
+	}
+	select {
+	case err := <-res:
+		return err
+	case <-e.done:
+		select {
+		case err := <-res:
+			return err
+		default:
+			return ErrEngineClosed
+		}
+	}
+}
+
+func (e *Engine) doSwitch(name string) error {
+	id, pos, audible := e.Position()
+	prevName, prevRate := e.outName, e.o.Output.Rate()
+	e.o.Output.Flush()
+	e.pending = e.pending[:0]
+	if e.rs != nil {
+		e.rs.Close()
+		e.rs = nil
+	}
+	rate := OutputRate
+	switch {
+	case e.cur != nil:
+		rate = e.cur.dec.SampleRate()
+	case e.held != nil:
+		rate = e.held.dec.SampleRate()
+	}
+	target := OutputRate
+	if Native(name) {
+		target = rate
+	}
+	err := e.reopen(name, target)
+	if err == nil {
+		e.outName = name
+	} else if e.reopen(prevName, prevRate) != nil {
+		// Neither opens: what plays waits for the next switch.
+		if e.cur != nil {
+			e.held, e.cur = e.cur, nil
+			e.setBusy(nil, 0)
+		}
+		e.resetSegments(segment{start: e.written, id: 0, rate: e.o.Output.Rate()})
+		return err
+	}
+	e.resume(id, pos, audible)
+	return err
+}
+
+// resume carries on after the device was reopened and flushed: the current
+// track from pos (what was heard), or the held one from its start.
+func (e *Engine) resume(id uint64, pos time.Duration, audible bool) {
+	devRate := e.o.Output.Rate()
+	if v := e.held; v != nil && e.cur == nil {
+		e.held = nil
+		e.startVoice(v, false)
+		return
+	}
+	v := e.cur
+	if v == nil {
+		e.resetSegments(segment{start: e.written, id: 0, rate: devRate})
+		return
+	}
+	if rate := v.dec.SampleRate(); rate != devRate {
+		rs, err := NewResampler(rate, devRate, e.o.ResampleQuality)
+		if err != nil {
+			e.queueEvent(Event{Kind: EventError, TrackID: v.t.ID, Err: err})
+			e.stopCurrent()
+			return
+		}
+		e.rs, e.chainOut, e.chainIn = rs, e.written, 0
+	}
+	if audible && id == v.t.ID && e.doSeek(id, pos) == nil {
+		return
+	}
+	// The decoder carries on from where it was: what the flush dropped is
+	// skipped (at most the ring, half a second).
+	if !audible || id != v.t.ID {
+		pos = v.t.Offset
+	}
+	e.resetSegments(segment{start: e.written, id: v.t.ID, base: pos, rate: devRate})
 }
 
 func (e *Engine) openVoice(t Track) (*voice, error) {
@@ -838,6 +1012,8 @@ func (e *Engine) stopCurrent() {
 	e.stops++
 	closeVoice(e.cur)
 	e.cur = nil
+	closeVoice(e.held)
+	e.held = nil
 	e.finishedID = 0
 	e.ended = false
 	e.setBusy(nil, 0)
@@ -845,7 +1021,7 @@ func (e *Engine) stopCurrent() {
 		e.rs.Close()
 		e.rs = nil
 	}
-	e.resetSegments(segment{start: e.written, id: 0})
+	e.resetSegments(segment{start: e.written, id: 0, rate: e.o.Output.Rate()})
 	e.o.Output.Flush()
 	e.pending = e.pending[:0]
 }
@@ -922,7 +1098,9 @@ func (e *Engine) doSeek(id uint64, pos time.Duration) error {
 		rel = 0
 	}
 	rate := v.dec.SampleRate()
-	if err := v.dec.SeekFrame(uint64(rel.Seconds() * float64(rate))); err != nil {
+	// Rounded: a position read from the device's frame count must come back
+	// to the same frame.
+	if err := v.dec.SeekFrame(uint64(math.Round(rel.Seconds() * float64(rate)))); err != nil {
 		return err
 	}
 	e.mu.Lock()
@@ -935,14 +1113,14 @@ func (e *Engine) doSeek(id uint64, pos time.Duration) error {
 		}
 		e.evq = append(e.evq, Event{Kind: EventStarted, TrackID: id})
 	}
-	e.segs = []segment{{start: e.written, id: id, base: pos}}
+	e.segs = []segment{{start: e.written, id: id, base: pos, rate: e.o.Output.Rate()}}
 	e.mu.Unlock()
 	e.o.Output.Flush()
 	e.pending = e.pending[:0]
 	if e.rs != nil {
 		e.rs.Close()
 		e.rs = nil
-		rs, err := NewResampler(rate, OutputRate, e.o.ResampleQuality)
+		rs, err := NewResampler(rate, e.o.Output.Rate(), e.o.ResampleQuality)
 		if err != nil {
 			return err
 		}
```

- [ ] **Step 4: Run the tests to see them pass.**

Run: `go vet ./... && go test -race ./internal/audio ./cmd/...`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/audio/engine_rate_test.go cmd/mss-cli/main.go internal/audio/engine.go
git commit -m "audio: the engine plays each song at its own rate on a native device"
```


### Task 4: The visualizer at any rate

Above 48 kHz the picture would be wrong: the bands are computed for 48 kHz, and the tap holds too little time. The tap now sizes its ring by time, and `Window` averages frames down to about 48 kHz and returns the rate. The analyzer rebuilds its band tables when the rate changes.

**Files:**
- Modify: `internal/audio/tap.go`
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/viz.go`
- Modify: `internal/viz/viz.go`
- Test: `internal/audio/engine_test.go`
- Test: `internal/audio/tap_test.go`
- Test: `internal/ui/vizdraw_test.go`
- Test: `internal/ui/vizprof_test.go`
- Test: `internal/viz/viz_test.go`

**Interfaces:**
- Consumes (Task 1): `Output.Rate()`.
- Produces:
  - **The tap:** `audio.Decimation(rate int) int`, and `(*audio.Tap).Window(dst []float32) (n, rate int)`, which was `int`.
  - **The analyzer:** `(*viz.Analyzer).SetRate(rate int)`.
  - **The UI:** `ui.Visual.Window(dst []float32) (n, rate int)`.

- [ ] **Step 1: Write the failing tests.** Apply with `git apply`:

```diff
diff --git a/internal/audio/engine_test.go b/internal/audio/engine_test.go
index e5a8364..e32742f 100644
--- a/internal/audio/engine_test.go
+++ b/internal/audio/engine_test.go
@@ -1441,7 +1441,7 @@ func TestEngineThroughTapFillsWindow(t *testing.T) {
 	dst := make([]float32, 2*512)
 	deadline := time.After(3 * time.Second)
 	for {
-		if n := tp.Window(dst); n == 512 {
+		if n, _ := tp.Window(dst); n == 512 {
 			return
 		}
 		select {
diff --git a/internal/audio/tap_test.go b/internal/audio/tap_test.go
index 0cd4833..0a30346 100644
--- a/internal/audio/tap_test.go
+++ b/internal/audio/tap_test.go
@@ -47,7 +47,7 @@ func TestTapKeepsOnlyTheFramesTheDeviceTook(t *testing.T) {
 	}
 	out.consume(10)
 	dst := make([]float32, 2*64)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	got := lefts(dst[:2*n])
 	if len(got) != 10 || got[0] != 1 || got[9] != 10 {
 		t.Fatalf("window = %v, want 1..10", got)
@@ -59,7 +59,7 @@ func TestTapWindowEndsAtConsumed(t *testing.T) {
 	tp.Write(stereo(1, 50))
 	out.consume(30)
 	dst := make([]float32, 2*8)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	got := lefts(dst[:2*n])
 	if n != 8 || got[0] != 23 || got[7] != 30 {
 		t.Fatalf("window = %v, want 23..30", got)
@@ -76,14 +76,14 @@ func TestTapRingWraps(t *testing.T) {
 	}
 	out.consume(35)
 	dst := make([]float32, 2*10)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	got := lefts(dst[:2*n])
 	if n != 10 || got[0] != 26 || got[9] != 35 {
 		t.Fatalf("window = %v, want 26..35", got)
 	}
 	// Asking for more than the ring holds gives the ring.
 	big := make([]float32, 2*40)
-	if n = tp.Window(big); n != 16 || lefts(big[:2*n])[0] != 20 {
+	if n, _ = tp.Window(big); n != 16 || lefts(big[:2*n])[0] != 20 {
 		t.Fatalf("big window n=%d first=%d, want 16 from 20", n, lefts(big[:2*n])[0])
 	}
 }
@@ -94,7 +94,7 @@ func TestTapUnplayedFramesAreSkipped(t *testing.T) {
 	out.consume(40)
 	tp.Write(stereo(41, 10)) // sit in the device ring, not heard yet
 	dst := make([]float32, 2*4)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	if got := lefts(dst[:2*n]); n != 4 || got[3] != 40 {
 		t.Fatalf("window = %v, want 37..40", got)
 	}
@@ -116,7 +116,7 @@ func TestTapPauseKeepsTheWindow(t *testing.T) {
 	tp.SetPaused(false)
 	out.consume(20)
 	dst := make([]float32, 2*16)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	if got := lefts(dst[:2*n]); n != 16 || got[0] != 45 || got[15] != 60 {
 		t.Fatalf("window = %v, want 45..60", got)
 	}
@@ -124,7 +124,7 @@ func TestTapPauseKeepsTheWindow(t *testing.T) {
 	clk.t = clk.t.Add(100 * time.Millisecond)
 	tp.Write(stereo(141, 20))
 	out.consume(20)
-	n = tp.Window(dst)
+	n, _ = tp.Window(dst)
 	if got := lefts(dst[:2*n]); n != 16 || got[0] != 65 || got[15] != 80 {
 		t.Fatalf("after refill window = %v, want 65..80", got)
 	}
@@ -135,7 +135,7 @@ func TestTapShortRingReturnsFewer(t *testing.T) {
 	tp.Write(stereo(1, 5))
 	out.consume(5)
 	dst := make([]float32, 2*32)
-	if n := tp.Window(dst); n != 5 {
+	if n, _ := tp.Window(dst); n != 5 {
 		t.Fatalf("n = %d, want 5", n)
 	}
 }
@@ -149,12 +149,12 @@ func TestTapFlushClears(t *testing.T) {
 		t.Fatal("Flush not passed through")
 	}
 	dst := make([]float32, 2*8)
-	if n := tp.Window(dst); n != 0 {
+	if n, _ := tp.Window(dst); n != 0 {
 		t.Fatalf("after Flush n = %d, want 0", n)
 	}
 	tp.Write(stereo(100, 3))
 	out.consume(3)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	if got := lefts(dst[:2*n]); n != 3 || got[0] != 100 {
 		t.Fatalf("after new writes window = %v, want 100..102", got)
 	}
@@ -165,18 +165,18 @@ func TestTapIdleSkipsTheCopyAndResumes(t *testing.T) {
 	dst := make([]float32, 2*8)
 	tp.Write(stereo(1, 10))
 	out.consume(10)
-	if n := tp.Window(dst); n != 8 { // reads at t0
+	if n, _ := tp.Window(dst); n != 8 { // reads at t0
 		t.Fatalf("n = %d, want 8", n)
 	}
 	clk.t = clk.t.Add(1500 * time.Millisecond) // nobody reads for over 1 s
 	tp.Write(stereo(11, 10))
 	out.consume(10)
-	if n := tp.Window(dst); n != 0 { // this read wakes it; nothing was copied
+	if n, _ := tp.Window(dst); n != 0 { // this read wakes it; nothing was copied
 		t.Fatalf("idle window n = %d, want 0", n)
 	}
 	tp.Write(stereo(21, 10))
 	out.consume(10)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	if got := lefts(dst[:2*n]); n != 8 || got[0] != 23 || got[7] != 30 {
 		t.Fatalf("resumed window = %v, want 23..30", got)
 	}
@@ -184,7 +184,7 @@ func TestTapIdleSkipsTheCopyAndResumes(t *testing.T) {
 	clk.t = clk.t.Add(900 * time.Millisecond)
 	tp.Write(stereo(31, 4))
 	out.consume(4)
-	if n = tp.Window(dst); n != 8 || lefts(dst[:2*n])[7] != 34 {
+	if n, _ = tp.Window(dst); n != 8 || lefts(dst[:2*n])[7] != 34 {
 		t.Fatalf("window after a short gap n = %d", n)
 	}
 }
@@ -227,7 +227,7 @@ func TestTapConcurrentWriteAndWindow(t *testing.T) {
 			case <-stop:
 				return
 			default:
-				n := tp.Window(dst)
+				n, _ := tp.Window(dst)
 				for i := 1; i < n; i++ { // frames are consecutive
 					if dst[2*i] != dst[2*i-2]+1 {
 						t.Errorf("window not consecutive at %d", i)
@@ -249,9 +249,61 @@ func TestTapOversizeWriteReturnsDeviceCount(t *testing.T) {
 	}
 	out.consume(40)
 	dst := make([]float32, 2*16)
-	n := tp.Window(dst)
+	n, _ := tp.Window(dst)
 	got := lefts(dst[:2*n])
 	if n != 16 || got[0] != 25 || got[15] != 40 {
 		t.Fatalf("window = %v, want 25..40", got)
 	}
 }
+
+func TestDecimation(t *testing.T) {
+	for rate, want := range map[int]int{
+		44100: 1, 48000: 1, 88200: 2, 96000: 2, 176400: 4, 192000: 4, 352800: 8, 384000: 8, 768000: 16,
+	} {
+		if got := Decimation(rate); got != want {
+			t.Errorf("Decimation(%d) = %d, want %d", rate, got, want)
+		}
+	}
+}
+
+func TestTapAveragesFramesAbove48k(t *testing.T) {
+	out := newNativeOutput(1000, 48000, 96000)
+	clk := &tapClock{t: time.Unix(1000, 0)}
+	tp := NewTap(out, 64)
+	tp.now = clk.now
+	if err := tp.Reopen("hw:CARD=DAC,DEV=0", 96000); err != nil {
+		t.Fatal(err)
+	}
+	tp.Window(make([]float32, 2)) // mark it read
+	tp.Write(stereo(1, 40))
+	out.consume(40)
+	dst := make([]float32, 2*8)
+	n, rate := tp.Window(dst)
+	if n != 8 || rate != 48000 {
+		t.Fatalf("window of %d frames at %d Hz, want 8 at 48000", n, rate)
+	}
+	// The last 16 device frames, 25..40, in pairs: (25+26)/2 = 25.5 ...
+	if dst[0] != 25.5 || dst[1] != -25.5 || dst[14] != 39.5 {
+		t.Fatalf("window = %v, want the means of pairs from 25.5 to 39.5", dst)
+	}
+}
+
+func TestTapReopenSizesTheRingForTheRate(t *testing.T) {
+	out := newNativeOutput(100000, 48000, 192000)
+	tp := NewTap(out, 100)
+	if tp.size != 100 {
+		t.Fatalf("size %d at 48 kHz, want 100", tp.size)
+	}
+	tp.Window(make([]float32, 2))
+	tp.Write(stereo(1, 50))
+	out.consume(50)
+	if err := tp.Reopen("hw:CARD=DAC,DEV=0", 192000); err != nil {
+		t.Fatal(err)
+	}
+	if tp.size != 400 {
+		t.Fatalf("size %d at 192 kHz, want 400 (the same time)", tp.size)
+	}
+	if n, _ := tp.Window(make([]float32, 2*8)); n != 0 {
+		t.Fatalf("window of %d frames right after a reopen, want 0: the old rate's frames are gone", n)
+	}
+}
diff --git a/internal/ui/vizdraw_test.go b/internal/ui/vizdraw_test.go
index a3b5597..83a5abe 100644
--- a/internal/ui/vizdraw_test.go
+++ b/internal/ui/vizdraw_test.go
@@ -19,7 +19,7 @@ import (
 // that moves from call to call so the waterfall has something to sweep.
 type fakeVisual struct{ pos, calls int }
 
-func (f *fakeVisual) Window(dst []float32) int {
+func (f *fakeVisual) Window(dst []float32) (int, int) {
 	n := len(dst) / 2
 	moving := 300 + 400*float64(f.calls%24)
 	for i := 0; i < n; i++ {
@@ -30,16 +30,16 @@ func (f *fakeVisual) Window(dst []float32) int {
 	}
 	f.pos += n
 	f.calls++
-	return n
+	return n, 48000
 }
 
 // cachedVisual hands out the same window every time (the benchmark's signal:
 // making sines would be most of what it measures).
 type cachedVisual struct{ buf []float32 }
 
-func (c *cachedVisual) Window(dst []float32) int {
+func (c *cachedVisual) Window(dst []float32) (int, int) {
 	copy(dst, c.buf)
-	return min(len(dst), len(c.buf)) / 2
+	return min(len(dst), len(c.buf)) / 2, 48000
 }
 
 // vizApp is Now Playing with the visualizer in style, playing, over a fake signal.
diff --git a/internal/ui/vizprof_test.go b/internal/ui/vizprof_test.go
index 95ec522..4be0594 100644
--- a/internal/ui/vizprof_test.go
+++ b/internal/ui/vizprof_test.go
@@ -94,11 +94,11 @@ func newCycleVisual(n int) *cycleVisual {
 	return c
 }
 
-func (c *cycleVisual) Window(dst []float32) int {
+func (c *cycleVisual) Window(dst []float32) (int, int) {
 	w := c.wins[c.i%len(c.wins)]
 	c.i++
 	copy(dst, w)
-	return min(len(dst), len(w)) / 2
+	return min(len(dst), len(w)) / 2, 48000
 }
 
 // BenchmarkVizRealFrame is one visualizer frame as the app runs it: advance
diff --git a/internal/viz/viz_test.go b/internal/viz/viz_test.go
index 2d5d8cb..ae1aa08 100644
--- a/internal/viz/viz_test.go
+++ b/internal/viz/viz_test.go
@@ -442,3 +442,29 @@ func BenchmarkAnalyzerUpdate(b *testing.B) {
 		})
 	}
 }
+
+func TestSetRatePutsASineInItsBand(t *testing.T) {
+	const f, rate = 12000.0, 44100
+	s := make([]float32, 2*2048)
+	for i := 0; i < 2048; i++ {
+		v := float32(math.Sin(2 * math.Pi * f * float64(i) / rate))
+		s[2*i], s[2*i+1] = v, v
+	}
+	want := bandOf(f, 64)
+
+	stale := New(HDMI()) // tables for 48 kHz: 12 kHz at 44.1 kHz reads high
+	settle(stale, s, 2048, 33*time.Millisecond, 10)
+	if got := argmax(stale.Column()); got == want {
+		t.Fatalf("the test can't tell: band %d either way", got)
+	}
+
+	a := New(HDMI())
+	a.SetRate(rate)
+	settle(a, s, 2048, 33*time.Millisecond, 10)
+	if got := argmax(a.Column()); got != want {
+		t.Fatalf("waterfall peak in band %d, want %d", got, want)
+	}
+	if allocs := testing.AllocsPerRun(10, func() { a.SetRate(rate) }); allocs != 0 {
+		t.Errorf("SetRate at the same rate allocates %v times", allocs)
+	}
+}
```

- [ ] **Step 2: Run the tests to see them fail.**

Run: `go test ./internal/audio ./internal/viz ./internal/ui`
Expected: FAIL, build failure, for example `a.SetRate undefined`.

- [ ] **Step 3: Write the implementation.** Apply with `git apply`:

```diff
diff --git a/internal/audio/tap.go b/internal/audio/tap.go
index 2d6296f..53e9968 100644
--- a/internal/audio/tap.go
+++ b/internal/audio/tap.go
@@ -6,8 +6,9 @@ import (
 	"time"
 )
 
-// TapFrames is the app's tap size: about 0.68 s, which holds the device ring
-// (up to 0.5 s not yet played) plus a window.
+// TapFrames is the app's tap size at 48 kHz: about 0.68 s, which holds the
+// device ring (up to 0.5 s not yet played) plus a window. At other rates the
+// tap holds the same time.
 const TapFrames = 32768
 
 // tapIdle is how long without a Window call before Write stops copying.
@@ -23,15 +24,41 @@ type Tap struct {
 	pausedAt atomic.Int64 // when the pause began, UnixNano (0: not paused)
 
 	mu      sync.Mutex
+	base    int       // the size asked for, in frames at OutputRate
 	ring    []float32 // interleaved stereo; frame i is at ring[2*(i%size)]
 	size    int       // frames
+	rate    int       // the device rate the ring is sized for
 	written uint64    // frames the device has taken, ever
 	floor   uint64    // frames before this are not in the ring (Flush, idle)
 }
 
-// NewTap wraps out; frames is the ring size in stereo frames.
+// NewTap wraps out; frames is the ring size in stereo frames at OutputRate.
 func NewTap(out Output, frames int) *Tap {
-	return &Tap{out: out, now: time.Now, ring: make([]float32, 2*frames), size: frames}
+	t := &Tap{out: out, now: time.Now, base: frames}
+	t.resize(out.Rate())
+	return t
+}
+
+// resize sizes the ring for rate, the same time as base frames at
+// OutputRate, and drops what it held. Callers hold mu (or own t).
+func (t *Tap) resize(rate int) {
+	size := int((int64(t.base)*int64(rate) + OutputRate - 1) / OutputRate)
+	if size != t.size {
+		t.ring, t.size = make([]float32, 2*size), size
+	}
+	t.rate = rate
+	t.floor = t.written
+}
+
+// Decimation is the factor Window averages frames by at rate: 1 up to
+// 48 kHz, 2 at 88.2 and 96, 4 at 176.4 and 192, and so on, so the
+// visualizer always sees about 44.1 to 48 kHz.
+func Decimation(rate int) int {
+	d := 1
+	for rate/(2*d) >= 44100 {
+		d *= 2
+	}
+	return d
 }
 
 // Write passes frames to the device, then keeps the ones it took, unless
@@ -64,28 +91,44 @@ func (t *Tap) Write(frames []float32) int {
 
 // Window fills dst (interleaved stereo, len(dst)/2 frames wanted) with the
 // latest frames that end at the device's Consumed position, the ones playing
-// now, and returns how many it filled. That is fewer when the tap doesn't
-// hold that many, for example right after Flush.
-func (t *Tap) Window(dst []float32) int {
+// now, and returns how many it filled and their rate. That is fewer frames
+// when the tap doesn't hold that many, for example right after Flush. Above
+// 48 kHz each frame is the mean of Decimation(rate) device frames, and rate
+// is the device rate divided by that.
+func (t *Tap) Window(dst []float32) (n, rate int) {
 	t.lastRead.Store(t.now().UnixNano())
 	end := t.out.Consumed()
-	want := min(len(dst)/2, t.size)
 	t.mu.Lock()
 	defer t.mu.Unlock()
+	d := Decimation(t.rate)
+	want := min(len(dst)/2, t.size/d) * d // device frames
 	end = min(end, t.written)
 	start := max(t.floor, t.written-min(t.written, uint64(t.size)))
 	if end < uint64(want) || end-uint64(want) < start {
 		if end <= start {
-			return 0
+			return 0, t.rate / d
 		}
-		want = int(end - start)
+		want = int(end-start) / d * d
 	}
 	from := end - uint64(want)
-	pos := int(from % uint64(t.size))
-	first := min(want, t.size-pos)
-	copy(dst, t.ring[2*pos:2*(pos+first)])
-	copy(dst[2*first:], t.ring[:2*(want-first)])
-	return want
+	if d == 1 {
+		pos := int(from % uint64(t.size))
+		first := min(want, t.size-pos)
+		copy(dst, t.ring[2*pos:2*(pos+first)])
+		copy(dst[2*first:], t.ring[:2*(want-first)])
+		return want, t.rate
+	}
+	inv := 1 / float32(d)
+	for i := 0; i < want/d; i++ {
+		var l, r float32
+		for j := 0; j < d; j++ {
+			k := 2 * int((from+uint64(i*d+j))%uint64(t.size))
+			l += t.ring[k]
+			r += t.ring[k+1]
+		}
+		dst[2*i], dst[2*i+1] = l*inv, r*inv
+	}
+	return want / d, t.rate / d
 }
 
 func (t *Tap) Consumed() uint64    { return t.out.Consumed() }
@@ -109,11 +152,12 @@ func (t *Tap) SetPaused(p bool) {
 	}
 }
 
-// Reopen passes the reopen on; what the tap kept is dropped.
+// Reopen passes the reopen on; the ring is sized for the new rate, and what
+// it kept is dropped.
 func (t *Tap) Reopen(name string, rate int) error {
 	err := t.out.Reopen(name, rate)
 	t.mu.Lock()
-	t.floor = t.written
+	t.resize(t.out.Rate())
 	t.mu.Unlock()
 	return err
 }
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 460686b..019aae3 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -144,9 +144,10 @@ type Options struct {
 
 // Visual gives the visualizer the audio that is playing: Window fills dst
 // (interleaved stereo float32) with the latest frames ending at the playback
-// position and returns how many frames it filled. *audio.Tap implements it.
+// position and returns how many frames it filled and their sample rate.
+// *audio.Tap implements it.
 type Visual interface {
-	Window(dst []float32) int
+	Window(dst []float32) (n, rate int)
 }
 
 // Visual returns Options.Visual.
diff --git a/internal/ui/viz.go b/internal/ui/viz.go
index 3d1763f..3ed383f 100644
--- a/internal/ui/viz.go
+++ b/internal/ui/viz.go
@@ -181,7 +181,9 @@ func (a *App) vizTick(now time.Time) {
 	}
 	n := 0
 	if a.state().Status == player.Playing { // a paused device stops consuming: let the levels fall
-		n = a.o.Visual.Window(v.win)
+		var rate int
+		n, rate = a.o.Visual.Window(v.win)
+		v.an.SetRate(rate)
 	}
 	v.an.Update(v.win, n, dt)
 	strip := stripOf{}
diff --git a/internal/viz/viz.go b/internal/viz/viz.go
index 855d377..9481e68 100644
--- a/internal/viz/viz.go
+++ b/internal/viz/viz.go
@@ -13,7 +13,7 @@ type Config struct {
 	FFTSize        int // power of two: 2048 on HDMI, 1024 on CRT
 	Bars           int // spectrum bars: 32 on HDMI, 16 on CRT
 	WaterfallBands int // waterfall rows: 64 on HDMI, 32 on CRT
-	SampleRate     int // 48000
+	SampleRate     int // 48000 at the start; see SetRate
 }
 
 func HDMI() Config { return Config{FFTSize: 2048, Bars: 32, WaterfallBands: 64, SampleRate: 48000} }
@@ -143,6 +143,18 @@ func New(cfg Config) *Analyzer {
 	return a
 }
 
+// SetRate tells the analyzer the sample rate of the windows that follow.
+// A new rate rebuilds the band tables (they map FFT bins to frequencies),
+// which allocates; the same rate does nothing.
+func (a *Analyzer) SetRate(rate int) {
+	if rate <= 0 || rate == a.cfg.SampleRate {
+		return
+	}
+	a.cfg.SampleRate = rate
+	a.bars = newBandSet(a.cfg.Bars, a.cfg.FFTSize, rate, true)
+	a.wfall = newBandSet(a.cfg.WaterfallBands, a.cfg.FFTSize, rate, false)
+}
+
 // Update analyses the latest window: stereo is interleaved L,R float32, n
 // frames (n may be less than FFTSize: zero-pad; 0 = silence), dt the real
 // time since the last Update (ballistics use it).
```

- [ ] **Step 4: Run the tests to see them pass.**

Run: `go vet ./... && go test ./internal/audio ./internal/viz ./internal/ui`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/audio/engine_test.go internal/audio/tap_test.go internal/ui/vizdraw_test.go internal/ui/vizprof_test.go internal/viz/viz_test.go internal/audio/tap.go internal/ui/app.go internal/ui/viz.go internal/viz/viz.go
git commit -m "visualizer: the tap and the analyzer follow the device's rate"
```


### Task 5: The app: Settings → Playback → Output, the fallbacks and their notices

The app wires it all together:
- **The Output row** switches between MiSTer audio and the cards, and saves the choice once the switch worked.
- **At start,** a DAC that won't open hands over to MiSTer audio with a notice.
- **Unplugged while playing:** `EventDeviceLost` reaches the UI through the player, which switches to MiSTer audio and says so.

**Files:**
- Modify: `cmd/mistersubsonic/main.go`
- Create: `cmd/mistersubsonic/output.go`
- Modify: `internal/player/player.go`
- Modify: `internal/ui/app.go`
- Create: `internal/ui/output.go`
- Modify: `internal/ui/screens_settings.go`
- Modify: `internal/ui/session.go`
- Test: `cmd/mistersubsonic/main_test.go`
- Test: `internal/player/player_test.go`
- Test: `internal/ui/output_test.go`
- Golden (regenerated): `internal/ui/testdata/golden/settings-playback-crt.png`
- Golden (regenerated): `internal/ui/testdata/golden/settings-playback-hdmi.png`

**Interfaces:**
- Consumes: `audio.Cards`, `Card`, `CardID`, `Native` (Task 1); `Engine.SwitchOutput`, `EngineOptions.OutputName`, `EventDeviceLost` (Task 3).
- Produces:
  - **The player:** `player.DeviceLost`, an `EventKind`.
  - **The UI interface:** `ui.AudioOutput` (`Cards() []audio.Card`, `Active() string`, `Switch(device string) error`).
  - **The UI options:** `ui.Options.AudioNotice string` and `ui.Options.AudioOutput AudioOutput`.
  - **`cmd/mistersubsonic`:** `openOutput(dev string, null bool) (out audio.Output, name, notice string, err error)` and `outputSwitcher`.

- [ ] **Step 1: Write the failing tests.** Apply with `git apply`:

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index 217e227..f480cd1 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -302,3 +302,37 @@ func TestUIOptionsGetTheAudioTap(t *testing.T) {
 		t.Fatalf("Options.Visual = %T, want *audio.Tap", got)
 	}
 }
+
+func TestOpenOutputFallsBackFromAMissingDAC(t *testing.T) {
+	old := openDevice
+	var asked []string
+	openDevice = func(o audio.DeviceOptions) (audio.Output, error) {
+		asked = append(asked, o.Name)
+		if audio.Native(o.Name) {
+			return nil, errors.New("no such device")
+		}
+		return audio.OpenDevice(audio.DeviceOptions{Null: true})
+	}
+	defer func() { openDevice = old }()
+
+	out, name, notice, err := openOutput("hw:CARD=NoSuchDAC,DEV=0", false)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer out.Close()
+	if name != "" || notice != "NoSuchDAC isn't connected: using MiSTer audio" {
+		t.Fatalf("name %q, notice %q", name, notice)
+	}
+	if len(asked) != 2 || asked[1] != "" {
+		t.Fatalf("opened %q, want the DAC then MiSTer audio", asked)
+	}
+}
+
+func TestOpenOutputDoesNotFallBackFromDefault(t *testing.T) {
+	old := openDevice
+	openDevice = func(audio.DeviceOptions) (audio.Output, error) { return nil, errors.New("boom") }
+	defer func() { openDevice = old }()
+	if _, _, _, err := openOutput("", false); err == nil {
+		t.Fatal("no error when MiSTer audio itself fails")
+	}
+}
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index 28d5e64..2dcac83 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -931,3 +931,21 @@ func TestAnEmptyServerQueueIsNotAResume(t *testing.T) {
 		t.Fatalf("resumable = %+v, %v; want the local file's", r, err)
 	}
 }
+
+func TestDeviceLostIsPassedOn(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.PlayNow(songs(2, 100), 0)
+	h.playAndStart(1)
+	for len(h.p.Events()) > 0 {
+		<-h.p.Events()
+	}
+	boom := errors.New("unplugged")
+	h.eng.events <- audio.Event{Kind: audio.EventDeviceLost, Err: boom}
+	h.waitFor("the event", func() bool { return len(h.p.Events()) > 0 })
+	if ev := <-h.p.Events(); ev.Kind != DeviceLost || !errors.Is(ev.Err, boom) {
+		t.Fatalf("event %+v, want DeviceLost with the engine's error", ev)
+	}
+	if st := h.p.State().Status; st == Stopped {
+		t.Fatal("a lost device stopped the player; the song must carry on after a switch")
+	}
+}
diff --git a/internal/ui/output_test.go b/internal/ui/output_test.go
new file mode 100644
index 0000000..7593f0b
--- /dev/null
+++ b/internal/ui/output_test.go
@@ -0,0 +1,204 @@
+package ui
+
+import (
+	"errors"
+	"slices"
+	"strings"
+	"sync"
+	"testing"
+	"time"
+
+	"mistersubsonic/internal/audio"
+	"mistersubsonic/internal/player"
+)
+
+const e30 = "hw:CARD=E30,DEV=0"
+
+// fakeAudioOutput is an output with cards; Switch records and may fail.
+type fakeAudioOutput struct {
+	mu       sync.Mutex
+	cards    []audio.Card
+	active   string
+	switches []string
+	err      error // the next Switch fails with it
+}
+
+func (f *fakeAudioOutput) Cards() []audio.Card { return f.cards }
+
+func (f *fakeAudioOutput) Active() string {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	return f.active
+}
+
+func (f *fakeAudioOutput) Switch(device string) error {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	f.switches = append(f.switches, device)
+	if err := f.err; err != nil {
+		f.err = nil
+		return err
+	}
+	f.active = device
+	return nil
+}
+
+func (f *fakeAudioOutput) calls() []string {
+	f.mu.Lock()
+	defer f.mu.Unlock()
+	return append([]string(nil), f.switches...)
+}
+
+func withE30() *fakeAudioOutput {
+	return &fakeAudioOutput{cards: []audio.Card{{Index: 1, ID: "E30", Name: "Topping E30"}}}
+}
+
+// waitSwitch runs posted work until the output switch has finished.
+func waitSwitch(t *testing.T, ta *testApp) {
+	t.Helper()
+	deadline := time.After(2 * time.Second)
+	for ta.outBusy {
+		select {
+		case f := <-ta.post:
+			f()
+		case <-deadline:
+			t.Fatal("the output switch did not finish")
+		}
+	}
+}
+
+func toastTexts(ta *testApp) []string {
+	var out []string
+	for _, t := range ta.toasts {
+		out = append(out, t.text)
+	}
+	return out
+}
+
+// outputRow is the Output row of Settings → Playback.
+func outputRow(t *testing.T, ta *testApp) setting {
+	t.Helper()
+	for _, r := range playbackSettings(ta.App) {
+		if r.label == "Output" {
+			return r
+		}
+	}
+	t.Fatal("no Output row in Settings → Playback")
+	return setting{}
+}
+
+func TestOutputSwitchesToTheDACAndSaves(t *testing.T) {
+	ta, _ := connectedApp(t)
+	out := withE30()
+	ta.o.AudioOutput = out
+	row := outputRow(t, ta)
+	if got := row.value(ta.App); got != "MiSTer audio" {
+		t.Fatalf("Output reads %q, want MiSTer audio", got)
+	}
+	row.change(ta.App, 1)
+	if got := row.value(ta.App); got != "Switching…" {
+		t.Fatalf("Output reads %q while switching", got)
+	}
+	waitSwitch(t, ta)
+	if got := out.calls(); !slices.Equal(got, []string{e30}) {
+		t.Fatalf("switches %v, want the E30", got)
+	}
+	if got := row.value(ta.App); got != "Topping E30" {
+		t.Fatalf("Output reads %q, want Topping E30", got)
+	}
+	if ta.cfg.Playback.ALSADevice != e30 {
+		t.Fatalf("alsa_device = %q, want %q", ta.cfg.Playback.ALSADevice, e30)
+	}
+	row.change(ta.App, 1) // back round to MiSTer audio
+	waitSwitch(t, ta)
+	if ta.cfg.Playback.ALSADevice != "default" || row.value(ta.App) != "MiSTer audio" {
+		t.Fatalf("alsa_device = %q, row %q", ta.cfg.Playback.ALSADevice, row.value(ta.App))
+	}
+}
+
+func TestOutputFailedSwitchKeepsTheConfig(t *testing.T) {
+	ta, _ := connectedApp(t)
+	out := withE30()
+	out.err = errors.New("device busy")
+	ta.o.AudioOutput = out
+	outputRow(t, ta).change(ta.App, 1)
+	waitSwitch(t, ta)
+	if ta.cfg.Playback.ALSADevice != "default" {
+		t.Fatalf("alsa_device = %q after a failed switch", ta.cfg.Playback.ALSADevice)
+	}
+	if got := toastTexts(ta); len(got) != 1 || got[0] != "Can't use Topping E30: device busy" {
+		t.Fatalf("toasts %q", got)
+	}
+}
+
+func TestOutputShowsAMissingDAC(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.cfg.Playback.ALSADevice = e30
+	ta.o.AudioOutput = &fakeAudioOutput{} // no cards: the E30 isn't plugged in
+	if got := outputRow(t, ta).value(ta.App); got != "MiSTer audio (E30 missing)" {
+		t.Fatalf("Output reads %q", got)
+	}
+}
+
+func TestOutputKeepsAHandWrittenDevice(t *testing.T) {
+	ta, _ := connectedApp(t)
+	out := withE30()
+	out.active = "E30, USB Audio"
+	ta.cfg.Playback.ALSADevice = out.active
+	ta.o.AudioOutput = out
+	row := outputRow(t, ta)
+	if got := row.value(ta.App); got != "E30, USB Audio" {
+		t.Fatalf("Output reads %q", got)
+	}
+	row.change(ta.App, 1) // from the hand-written name, on to the first choice
+	waitSwitch(t, ta)
+	if got := out.calls(); !slices.Equal(got, []string{""}) {
+		t.Fatalf("switches %v, want MiSTer audio", got)
+	}
+}
+
+func TestOutputLostDACFallsBackToMiSTerAudio(t *testing.T) {
+	ta, _ := connectedApp(t)
+	out := withE30()
+	ta.o.AudioOutput = out
+	row := outputRow(t, ta)
+	row.change(ta.App, 1) // learns the E30's name and switches to it
+	waitSwitch(t, ta)
+
+	ta.onPlayer(player.Event{Kind: player.DeviceLost})
+	waitSwitch(t, ta)
+	if got := out.calls(); !slices.Equal(got, []string{e30, ""}) {
+		t.Fatalf("switches %v, want back to MiSTer audio", got)
+	}
+	if got := toastTexts(ta); !slices.Contains(got, "Topping E30 was disconnected: using MiSTer audio") {
+		t.Fatalf("toasts %q", got)
+	}
+	if ta.cfg.Playback.ALSADevice != e30 {
+		t.Fatalf("alsa_device = %q: the config keeps the DAC for the next start", ta.cfg.Playback.ALSADevice)
+	}
+	if got := row.value(ta.App); got != "MiSTer audio (E30 missing)" {
+		t.Fatalf("Output reads %q", got)
+	}
+}
+
+func TestOutputLostOnMiSTerAudioOnlySaysSo(t *testing.T) {
+	ta, _ := connectedApp(t)
+	out := withE30()
+	ta.o.AudioOutput = out
+	ta.onPlayer(player.Event{Kind: player.DeviceLost})
+	if len(out.calls()) != 0 || ta.outBusy {
+		t.Fatalf("switches %v: nothing to fall back to", out.calls())
+	}
+	if got := toastTexts(ta); !slices.Contains(got, "Audio device stopped") {
+		t.Fatalf("toasts %q", got)
+	}
+}
+
+func TestAudioNoticeShowsAtStart(t *testing.T) {
+	ta, _ := connectedApp(t)
+	ta.o.AudioNotice = "E30 isn't connected: using MiSTer audio"
+	ta.start()
+	if got := toastTexts(ta); !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "E30 isn't connected") }) {
+		t.Fatalf("toasts %q", got)
+	}
+}
```

- [ ] **Step 2: Run the tests to see them fail.**

Run: `go test ./internal/player ./internal/ui ./cmd/...`
Expected: FAIL, build failure, for example `undefined: openOutput`.

- [ ] **Step 3: Write the implementation.** Apply with `git apply`:

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index fe26877..efa806c 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -397,13 +397,19 @@ func run(f flags) (err error) {
 	}
 	var eng *audio.Engine
 	var visual ui.Visual // the tap on what is playing (nil without a device)
-	out, audioErr := openDevice(audio.DeviceOptions{Name: dev, Null: f.null})
+	var output ui.AudioOutput
+	out, opened, audioNotice, audioErr := openOutput(dev, f.null)
 	if audioErr == nil {
 		defer out.Close()
 		tap := audio.NewTap(out, audio.TapFrames)
 		visual = tap
-		eng = audio.NewEngine(audio.EngineOptions{Output: tap})
+		eng = audio.NewEngine(audio.EngineOptions{Output: tap, OutputName: opened})
 		defer eng.Close()
+		cards := audio.Cards
+		if f.null {
+			cards = func() []audio.Card { return nil } // silent runs stay silent
+		}
+		output = &outputSwitcher{eng: eng, cards: cards, active: opened}
 	}
 
 	// Sessions (client, player, art) are built off the UI goroutine and
@@ -423,6 +429,7 @@ func run(f flags) (err error) {
 		Display: disp, Profile: prof, ProfileName: profileName, Inputs: inputs,
 		FallbackFonts: filepath.Join(dataDir, "fonts"),
 		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
+		AudioNotice: audioNotice, AudioOutput: output,
 		ScreenshotDir: screenshotDir(f.screenshots, f.display, dataDir),
 		VerifyRedraw:  f.verifyRedraw,
 		WatchDisplay:  watchDisplay,
diff --git a/cmd/mistersubsonic/output.go b/cmd/mistersubsonic/output.go
new file mode 100644
index 0000000..d8c3149
--- /dev/null
+++ b/cmd/mistersubsonic/output.go
@@ -0,0 +1,58 @@
+package main
+
+import (
+	"fmt"
+	"slices"
+	"sync"
+
+	"mistersubsonic/internal/audio"
+)
+
+// openOutput opens the configured device (dev; "" for MiSTer audio). A USB
+// DAC that won't open hands over to MiSTer audio, with a notice for the
+// screen; the config is left as it is, so the next start tries it again.
+// name is the device that opened.
+func openOutput(dev string, null bool) (out audio.Output, name, notice string, err error) {
+	out, err = openDevice(audio.DeviceOptions{Name: dev, Null: null})
+	if err == nil || !audio.Native(dev) {
+		return out, dev, "", err
+	}
+	fallback, ferr := openDevice(audio.DeviceOptions{Null: null})
+	if ferr != nil {
+		return nil, "", "", err
+	}
+	id := audio.CardID(dev)
+	if i := slices.IndexFunc(audio.Cards(), func(c audio.Card) bool { return c.ID == id }); i >= 0 {
+		notice = fmt.Sprintf("%s didn't open (%v): using MiSTer audio", audio.Cards()[i].Name, err)
+	} else {
+		notice = fmt.Sprintf("%s isn't connected: using MiSTer audio", id)
+	}
+	return fallback, "", notice, nil
+}
+
+// outputSwitcher is Settings → Playback → Output on the engine (ui.AudioOutput).
+type outputSwitcher struct {
+	eng   *audio.Engine
+	cards func() []audio.Card
+
+	mu     sync.Mutex
+	active string // the device open now
+}
+
+func (o *outputSwitcher) Cards() []audio.Card { return o.cards() }
+
+func (o *outputSwitcher) Active() string {
+	o.mu.Lock()
+	defer o.mu.Unlock()
+	return o.active
+}
+
+func (o *outputSwitcher) Switch(device string) error {
+	if err := o.eng.SwitchOutput(device); err != nil {
+		return err
+	}
+	o.mu.Lock()
+	o.active = device
+	o.mu.Unlock()
+	return nil
+}
diff --git a/internal/player/player.go b/internal/player/player.go
index 6d6db99..d305782 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -98,6 +98,9 @@ const (
 	StatusChanged
 	QueueChanged
 	Error
+	// DeviceLost: the sound device stopped (unplugged, say). Nothing is
+	// heard until the app switches the output; the song carries on then.
+	DeviceLost
 )
 
 type Event struct {
@@ -1049,6 +1052,8 @@ func (p *Player) onEngine(ev audio.Event) {
 		}
 		p.seekRetry = true
 		p.reopenAt(p.seekTarget) // I2: reopen preserves listen state and pause
+	case audio.EventDeviceLost:
+		p.emit(Event{Kind: DeviceLost, Err: ev.Err})
 	case audio.EventError:
 		switch ev.TrackID {
 		case p.nextID:
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 019aae3..83cbd55 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -118,8 +118,13 @@ type Options struct {
 	ConfigPath string
 	Config     *config.Config
 	ConfigErr  error
-	AudioErr   error  // the sound device couldn't be opened
-	Version    string // shown in Settings → About
+	AudioErr   error // the sound device couldn't be opened
+	// AudioNotice is shown once at the start: the DAC in the config isn't
+	// there, so MiSTer audio plays instead.
+	AudioNotice string
+	// AudioOutput switches the sound device (nil: no device).
+	AudioOutput AudioOutput
+	Version     string // shown in Settings → About
 	// ScreenshotDir is where the screenshot button saves PNGs ("": off).
 	ScreenshotDir string
 	// PadAtStart: a gamepad is connected, so the hint bar starts with
@@ -280,6 +285,9 @@ type App struct {
 	songStars  atomic.Pointer[map[subsonic.ID]bool]
 	seen       *songMemory
 	remoteURLs []string // the server's addresses, read when Settings → Remote opens
+
+	outBusy  bool              // an output switch is under way
+	outNames map[string]string // card names by device, as last listed
 }
 
 func New(o Options) (*App, error) {
@@ -721,6 +729,9 @@ func (a *App) onPlayer(ev player.Event) {
 		a.notifyRemote(remote.QueueChanged)
 	}
 	a.notifyRemote(remote.StateChanged) // the queue's index and the status change with it
+	if ev.Kind == player.DeviceLost {
+		a.outputLost()
+	}
 	if ev.Kind == player.Error {
 		title := ev.Song.Title
 		if title == "" {
diff --git a/internal/ui/output.go b/internal/ui/output.go
new file mode 100644
index 0000000..0e92cbf
--- /dev/null
+++ b/internal/ui/output.go
@@ -0,0 +1,150 @@
+package ui
+
+import (
+	"mistersubsonic/internal/audio"
+	"mistersubsonic/internal/config"
+)
+
+// AudioOutput switches where the sound goes (Settings → Playback → Output).
+// cmd/mistersubsonic implements it on the audio engine; it is nil without a
+// sound device.
+type AudioOutput interface {
+	// Cards lists the sound cards that can play now.
+	Cards() []audio.Card
+	// Active is the device playing now: "" for MiSTer audio, a hw: name for
+	// a card, or a name from the config as it was written.
+	Active() string
+	// Switch moves playback to device ("" for MiSTer audio). It takes up to
+	// about half a second: call it off the UI goroutine.
+	Switch(device string) error
+}
+
+const outputHelp = "Where the sound goes: MiSTer audio, or a USB DAC at each song's own sample rate and with its own volume."
+
+// outputSetting is the Output row of Settings → Playback.
+func outputSetting() setting {
+	return setting{label: "Output", value: (*App).outputValue, change: (*App).changeOutput, help: outputHelp}
+}
+
+// outputLabel names a device for the screen.
+func (a *App) outputLabel(device string) string {
+	switch {
+	case device == "" || device == "default":
+		return "MiSTer audio"
+	case !audio.Native(device):
+		return device
+	}
+	if name, ok := a.outNames[device]; ok {
+		return name
+	}
+	return audio.CardID(device)
+}
+
+// outputValue is what the Output row shows: what plays now, and the card
+// the config asks for when it is missing.
+func (a *App) outputValue() string {
+	out := a.o.AudioOutput
+	if out == nil {
+		return "None"
+	}
+	if a.outBusy {
+		return "Switching…"
+	}
+	act := out.Active()
+	label := a.outputLabel(act)
+	if want := a.wantedOutput(); audio.Native(want) && want != act {
+		label += " (" + audio.CardID(want) + " missing)"
+	}
+	return label
+}
+
+// wantedOutput is the device the config asks for ("" for MiSTer audio).
+func (a *App) wantedOutput() string {
+	if a.cfg == nil || a.cfg.Playback.ALSADevice == "default" {
+		return ""
+	}
+	return a.cfg.Playback.ALSADevice
+}
+
+// changeOutput steps to the next output and switches to it; the config
+// keeps the choice once the switch worked.
+func (a *App) changeOutput(dir int) {
+	out := a.o.AudioOutput
+	if out == nil || a.outBusy {
+		return
+	}
+	act := out.Active()
+	choices := []string{""}
+	if a.outNames == nil {
+		a.outNames = map[string]string{}
+	}
+	for _, c := range out.Cards() {
+		choices = append(choices, c.Device())
+		a.outNames[c.Device()] = c.Name
+	}
+	if !contains(choices, act) {
+		choices = append(choices, act) // a name from the config, written by hand
+	}
+	next := cycle(choices, act, dir)
+	if next == act {
+		return
+	}
+	a.switchOutput(next, func(err error) {
+		if err != nil {
+			a.Toast("Can't use %s: %v", a.outputLabel(next), err)
+			return
+		}
+		saved := next
+		if saved == "" {
+			saved = "default"
+		}
+		a.UpdateConfig(func(c *config.Config) { c.Playback.ALSADevice = saved }, false)
+	})
+}
+
+// switchOutput runs out.Switch off the UI goroutine; done runs on it after.
+func (a *App) switchOutput(device string, done func(error)) {
+	a.outBusy, a.dirty = true, true
+	out := a.o.AudioOutput
+	go func() {
+		err := out.Switch(device)
+		a.Post(func() {
+			a.outBusy, a.dirty = false, true
+			done(err)
+		})
+	}()
+}
+
+// outputLost is the player's DeviceLost: a card that went away hands the
+// sound to MiSTer audio. The config keeps the card for the next start.
+func (a *App) outputLost() {
+	out := a.o.AudioOutput
+	if out == nil {
+		return
+	}
+	act := out.Active()
+	if !audio.Native(act) {
+		a.Toast("Audio device stopped")
+		return
+	}
+	if a.outBusy {
+		return // a switch is under way; it reports for itself
+	}
+	name := a.outputLabel(act)
+	a.switchOutput("", func(err error) {
+		if err != nil {
+			a.Toast("%s was disconnected, and MiSTer audio didn't open: %v", name, err)
+			return
+		}
+		a.Toast("%s was disconnected: using MiSTer audio", name)
+	})
+}
+
+func contains(list []string, s string) bool {
+	for _, x := range list {
+		if x == s {
+			return true
+		}
+	}
+	return false
+}
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index 303fc6b..13a387f 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -288,7 +288,7 @@ func playbackSettings(a *App) []setting {
 				},
 				help: "Quality of those conversions to MP3: higher sounds better and uses more network.", toggle: false})
 	}
-	return rows
+	return append(rows, outputSetting())
 }
 
 var screensaverChoices = []int{0, 1, 2, 5, 10, 15, 30}
diff --git a/internal/ui/session.go b/internal/ui/session.go
index 2bd6538..3cd2997 100644
--- a/internal/ui/session.go
+++ b/internal/ui/session.go
@@ -38,6 +38,9 @@ func (a *App) Conn() (ConnInfo, bool) { return a.conn, a.conn.Server.Name != ""
 
 // start picks the first screen: a problem to explain, or connecting.
 func (a *App) start() {
+	if a.o.AudioNotice != "" {
+		a.Toast("%s", a.o.AudioNotice)
+	}
 	switch {
 	case a.o.AudioErr != nil:
 		a.Replace(NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", a.o.AudioErr, a.o.ConfigPath), nil))
```

- [ ] **Step 4: Regenerate the Playback settings goldens and look at them.**

Run: `go test -count=1 -run TestGoldenSettings ./internal/ui -update`
Then open `internal/ui/testdata/golden/settings-playback-hdmi.png` and `settings-playback-crt.png`. Expected: Playback lists ReplayGain, Scrobbling, Transcode to, Transcode bitrate and then **Output**, which reads "None" (the tests have no sound device). Nothing else moved.

- [ ] **Step 5: Run the tests to see them pass.**

Run: `go vet ./... && go test ./internal/player ./internal/ui ./cmd/...`
Expected: PASS.
With zig 0.16.0 on the PATH, also run `make mister mister-test`. Expected: the glibc check reports ≤ 2.31.
Then run `go vet ./...`, `go test -race ./...`, `./scripts/test-launcher.sh` and `make e2e`. Expected: all pass.

- [ ] **Step 6: Commit.**

```bash
git add cmd/mistersubsonic/main_test.go internal/player/player_test.go internal/ui/output_test.go cmd/mistersubsonic/main.go cmd/mistersubsonic/output.go internal/player/player.go internal/ui/app.go internal/ui/output.go internal/ui/screens_settings.go internal/ui/session.go internal/ui/testdata/golden/settings-playback-crt.png internal/ui/testdata/golden/settings-playback-hdmi.png
git commit -m "ui: Settings → Playback → Output, with fallbacks to MiSTer audio and their notices"
```


### Task 6: Docs

- **README:** a features line, and a short "USB DAC" section under Installation.
- **`config.example.toml`:** the forms of `alsa_device`.
- **`docs/testing-on-mister.md`:** item 41, the USB DAC checks. The log item becomes 42.
- **`docs/spikes.md`:** the measurements.
- **The spec:** §10, what the prototype changed.
- **The follow-ups:** the deferred minors.

**Files:**
- Modify: `README.md`
- Modify: `docs/spikes.md`
- Modify: `docs/superpowers/plan-2a-followups.md`
- Modify: `docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md`
- Modify: `docs/testing-on-mister.md`
- Modify: `sdcard/mistersubsonic/config.example.toml`

**Interfaces:**
- Consumes and produces nothing in code.

- [ ] **Step 1: Write the docs.** Apply with `git apply`:

```diff
diff --git a/README.md b/README.md
index 5dfa09d..706302a 100644
--- a/README.md
+++ b/README.md
@@ -21,6 +21,7 @@ A music player for the [MiSTer FPGA](https://github.com/MiSTer-devel) that strea
 - Web remote: control playback, edit the queue and browse your library from a phone on the same network
 - HDMI layout with a sidebar and cover grids, drawn at the TV's full resolution (up to 1920x1200), and a list layout for 15 kHz CRTs
 - Media keys on multimedia keyboards work
+- USB DACs: each song plays at its own sample rate, and the volume uses the DAC's own control
 
 ## Installation
 
@@ -64,6 +65,10 @@ For a TV on the component (YPbPr) input, the `[Menu]` section needs `vga_scaler=
 
 If the TV cuts off the edges, use Settings → Display → Overscan to move the picture.
 
+### USB DAC
+
+Plug the DAC into the MiSTer and pick it in Settings → Playback → Output. If it's unplugged, the music moves to the MiSTer's own audio until you pick the DAC again. If a DAC clicks after being plugged back in while the MiSTer is on, reboot the MiSTer.
+
 ### Web remote
 
 The remote is off by default. Turn it on in Settings → Remote, which also shows the address to open on your phone, something like `http://192.168.1.20:8080`.
diff --git a/docs/spikes.md b/docs/spikes.md
index 7652a15..5208289 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -275,3 +275,17 @@ Run it once with no phone connected and once with the remote page open on Now Pl
   - **With `vga_scaler=0`,** the framebuffer is not on the analog output at all: the app runs but nothing shows. The fix is a `[Menu]` section with `vga_scaler=1` and a `video_mode`.
   - **The picture was badly cropped at first,** but the cause was the TV's Wide mode set to 4:3 on that input. With Wide and overscan +1, both 720p (`video_mode=0`) and 480p fit.
   - **What's left** is a small cut on the left edge, which Plan 7c's Overscan settings cover.
+
+## USB DAC (2026-10-09)
+
+The user's Topping E30 on the MiSTer: card 1, ALSA id `E30`, `S32_LE`, 44.1 to 768 kHz, and a hardware volume (`E30 Playback Volume`, −127 to 0 dB in 1 dB steps, with a switch). The kernel has `CONFIG_SND_USB_AUDIO=y`.
+
+- **Before:** `alsa_device = "E30, USB Audio"` already played through the DAC, but at 48 kHz through ALSA's dmix.
+- **Opening `hw:CARD=E30,DEV=0` directly** (test binary, silence only): 44.1, 48, 88.2, 96 and 192 kHz open at exactly that rate, as `S32_LE`, `RW_INTERLEAVED`, with a 60 ms buffer (3 periods of 20 ms). 22.05 kHz is refused, and the app resamples that to 48 kHz.
+- **miniaudio's own device info** only knows the DAC as `:1,0`, which it probes through dmix (48 kHz only), so the app opens the `hw:` name as is, tries 32-bit, 24-bit, float and 16-bit in turn, and learns a refused rate on the first try.
+- **A reopen** takes about 0.3 s in the test binary: 80 ms for the last buffer to play, 25 ms to close, and about 220 ms in `ma_device_init` (the DAC changing its clock). In the app a rate change measured 0.58 to 0.63 s (log "opened in"), and a switch 0.25 to 0.3 s.
+- **CPU** (`BenchmarkDecodeNative`, 24-bit pink noise, one core): 44.1 kHz native 0.7%, resampled to 48 kHz 16.5%; 96 kHz 1.6% against 13.1%; 192 kHz 2.9% against 25.6%. The whole app played a 24/96 album at about 4% of the MiSTer's CPU (13–23% before, with the visualizer on).
+- **The app on the TV:** a 44.1 kHz album then a 24/96 one played at their own rates (the E30's display followed). The app's volume moved the E30's control exactly (app −12 dB, E30 −12.00 dB). Output switched to MiSTer audio and back while playing, with a short pause, and the song carried on. The E30 has no volume knob of its own.
+- **Unplugging:** miniaudio doesn't stop an ALSA device whose card goes away. Its wait loop spins on POLLERR (48% CPU) and sends no stop notification, so the app looks for the card in `/proc/asound` every 200 ms instead. Asking ALSA for the PCM state from another thread instead broke the stream on ARM (clicks; the DAC took 84,350 frames a second at 96 kHz), so the app doesn't. With the card check, the unplug moved the music to the TV with the notice, and picking the E30 again after replugging played cleanly (95,876 frames a second).
+- **A replug while a build without the card check held the dead device** left the E30's USB stream short of one slot in eight (clicks) until a reboot.
+- **Starting with the E30 unplugged:** the app played through MiSTer audio with "E30 isn't connected: using MiSTer audio".
diff --git a/docs/superpowers/plan-2a-followups.md b/docs/superpowers/plan-2a-followups.md
index 9a20871..3877505 100644
--- a/docs/superpowers/plan-2a-followups.md
+++ b/docs/superpowers/plan-2a-followups.md
@@ -534,3 +534,11 @@ Still open:
 - **README:**
   - the CRT wording "a whole number of lines" could say "an integer scale factor";
   - the note that a margin change refetches covers overstates it for CRT, where the cover size often stays the same.
+
+## USB DAC minors (deferred)
+
+- **The rate-change silence** is 0.58 to 0.63 s in the app against 0.3 s in a test binary. Where the other 0.3 s goes isn't measured.
+- **The device opens at 48 kHz** at start, then reopens at the first song's rate: about 0.6 s more before the first song. It could open at the resumed song's rate.
+- **SetGain for the song waiting in a rate-change drain** isn't applied to it (it is neither current nor queued then); its old gain plays.
+- **The web remote** shows no output setting.
+- **A replug while the app holds a dead device** (only possible with a build before the card check) left the E30's USB stream short until a reboot. The README tells users to reboot if a replugged DAC clicks.
diff --git a/docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md b/docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md
index 82ff90c..ec99b4c 100644
--- a/docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md
+++ b/docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md
@@ -223,3 +223,27 @@ Before the plan is final, the prototype is measured on the device with the E30.
 - Bit depth chosen per song. The device runs at its native format, and the samples are converted from float.
 - An exclusive or "bit-perfect" switch: ReplayGain and any software gain still apply when on.
 - Outputs other than ALSA `hw:` cards (Bluetooth, network).
+
+## 10. Changes from the prototype (2026-10-09)
+
+The prototype was built and measured on the MiSTer with the E30 before the plan. These replace the parts of §3 they name.
+
+- **§3.2, supported rates:** miniaudio's device info lists a DAC only under its own `:N,0` id, probed through dmix (48 kHz only). So `Rates() []int` is `Supports(rate int) bool` instead:
+  - A native device supports every rate until a reopen at it fails with `ErrRateUnsupported`; it is remembered per device name.
+  - A fixed device supports 48 kHz only.
+  - The engine reopens at 48 kHz after a refusal.
+- **§3.2, the format:** a native device tries `S32`, `S24`, float and `S16` in that order and keeps the first the device really takes. miniaudio's own fallback would pick 16-bit first. The callback converts from float with `ma_pcm_convert`.
+- **§3.2, lost devices:** miniaudio's ALSA backend doesn't stop a device whose card is unplugged (its wait loop spins on POLLERR), so no stop notification comes. `Lost()` also reports a native device whose card is gone from `/proc/asound`, checked at most every 200 ms. It never asks ALSA for the PCM state from another thread: on ARM that broke the stream.
+- **§3.2, a failed reopen** doesn't set `Lost()`; the error goes back to the caller. In a rate change the engine then holds the song (it isn't dropped) and sends `EventDeviceLost`, and `SwitchOutput` starts it on the new device.
+- **§3.2:** miniaudio's context lives across reopens.
+- **§3.3:** `doSeek` rounds the frame. Truncating it made a position read from the frame count come back one frame early.
+- **§3.4:** the mixer is skipped for a card that isn't in `/proc/asound`, and tried again when it is.
+- **§3.5:** `viz.Analyzer.SetRate(rate)` rebuilds the bands on a change; `Update` is unchanged.
+- **§3.6:**
+  - The Output row is the last row of Settings → Playback.
+  - `ui.Options` gains `AudioNotice` and `AudioOutput`, an interface with `Cards`, `Active` and `Switch`, which `cmd/mistersubsonic` implements on the engine.
+  - The player passes `EventDeviceLost` on as `player.DeviceLost`.
+- **The startup notice** for a card that is present but won't open is "<name> didn't open (<error>): using MiSTer audio".
+- **Measured:**
+  - a rate change costs 0.58 to 0.63 s of silence in the app, 0.3 s in a test binary;
+  - native playback saves the resampler, 13 to 25% of a core.
diff --git a/docs/testing-on-mister.md b/docs/testing-on-mister.md
index 89403fd..a476c66 100644
--- a/docs/testing-on-mister.md
+++ b/docs/testing-on-mister.md
@@ -136,7 +136,13 @@ starts at the config's `volume_db` (0 dB by default) on the MiSTer.
     - Settings, the hint bar, a toast, the volume panel, Now Playing with the visualizer in full screen and the screensaver all stay inside the margins, and the border stays black (no flicker at the edges).
     - Restart the app: the margins are kept, and `config.toml` has `overscan_left`, `overscan_right` and `overscan_y` under `[display]`. Run once with `-verify-redraw` and some margins: `log.txt` has no "partial redraw differs" line.
     - F12 saves a PNG of the inner picture (without the border).
-41. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
+41. **USB DAC.** Ask the user before playing anything, and keep the amp and the TV low. With a USB DAC plugged in:
+    - Settings → Playback → Output lists it by name. Picking it moves the music to it within a moment, and `config.toml` has `alsa_device = "hw:CARD=<id>,DEV=0"`.
+    - A 44.1 kHz album then a 96 kHz one: `log.txt` has "audio: hw:CARD=<id>,DEV=0 at 44100 Hz" then "at 96000 Hz", `/proc/asound/card1/pcm0p/sub0/hw_params` shows the song's rate and the DAC's own format, and the DAC's display follows. Songs at one rate stay gapless.
+    - The app's volume moves the DAC's control: `amixer -c 1` shows the same dB. Mute turns its switch off.
+    - Unplug the DAC while playing: within a second the music goes on through MiSTer audio, with "<name> was disconnected: using MiSTer audio". Plug it back in and pick it again: it plays cleanly (`hw_ptr` in `/proc/asound/card1/pcm0p/sub0/status` advances at the song's rate).
+    - Start the app with the DAC unplugged: "<id> isn't connected: using MiSTer audio", and the Output row reads "MiSTer audio (<id> missing)". The next start with it plugged in uses it.
+42. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
     "exiting" line for each run, and `crash.txt` beside it is empty.
 
 ## Benchmarks
diff --git a/sdcard/mistersubsonic/config.example.toml b/sdcard/mistersubsonic/config.example.toml
index c442992..9bd0663 100644
--- a/sdcard/mistersubsonic/config.example.toml
+++ b/sdcard/mistersubsonic/config.example.toml
@@ -29,7 +29,7 @@ replaygain = "off"         # off, track or album
 volume_db = 0              # -60 to 0; saved when you change the volume
 scrobble = true            # report plays to the server (and on to Last.fm/ListenBrainz)
 buffer_mb = 32             # memory for the playing stream, 4 to 512
-alsa_device = "default"
+alsa_device = "default"    # MiSTer audio; a USB DAC is "hw:CARD=<id>,DEV=0" (Settings → Playback → Output picks it)
 
 [display]
 profile = "auto"           # auto, hdmi or crt (set crt for a 480i/576i CRT)
```

- [ ] **Step 2: Check.**

Run: `go vet ./... && go test ./...`
Expected: PASS. The docs change no code.

- [ ] **Step 3: Commit.**

```bash
git add README.md docs/spikes.md docs/superpowers/plan-2a-followups.md docs/superpowers/specs/2026-10-09-mister-subsonic-usb-dac-design.md docs/testing-on-mister.md sdcard/mistersubsonic/config.example.toml
git commit -m "docs: the USB DAC in the README, the example config, the device checks, the spikes and the spec"
```


## After the tasks: on the MiSTer

The prototype already passed `docs/testing-on-mister.md` item 41 on the user's E30 (see `docs/spikes.md`, "USB DAC"). After the final review, deploy the branch and run item 41 again with the user, who gives the go-ahead before anything plays.
