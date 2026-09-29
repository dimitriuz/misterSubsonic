# MiSTer Subsonic — Plan 3a: MiSTer integration and releases

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the app installs on a MiSTer and behaves there:
- **Launcher:** a Scripts-menu launcher that runs one instance at a time, stops BGM and SAM while the app runs, and puts everything back afterwards.
- **Console:** in graphics mode while the app draws, restored after any exit, crashes included.
- **Robust display:**
  - a framebuffer that follows the console's pan offset;
  - a watchdog that repaints when something draws over the screen;
  - a log on the SD card.
- **Mute** (spec §6).
- **Releases:** `config.example.toml`, a release zip and a MiSTer Downloader database built by a tag workflow.
- **Docs:** install, `MiSTer.ini`, and the on-device checklist.

**Architecture:** the app handles what only it can do:
- the console mode (`internal/platform`, the ioctls behind replaceable functions);
- the framebuffer checks;
- its log (`internal/logfile`);
- a bounded, panic-safe shutdown in `main`.

The bash launcher (`sdcard/Scripts/MiSTer_Subsonic.sh`) handles what lives outside the process:
- the lock;
- leftover processes;
- BGM and SAM;
- the cursor;
- restoring the console after a crash, which it does by running `mistersubsonic -restore-console`.

`sdcard/` in the repository mirrors the SD card. `make release` fills it with the binary and zips it, and `tools/mkdb` describes it for MiSTer Downloader.

**Tech Stack:** Go 1.26+, no new modules; bash for the launcher (stock MiSTer Linux tools only); zig 0.16.0 for the ARM build; GitHub Actions for releases.

**Spec:** `docs/superpowers/specs/2026-09-28-mister-subsonic-design.md`: §3 (`internal/platform`), §6 (mute), §8.1 (overwrite watchdog, fbdev), §9 (install layout, launcher, console restore, config example, build, CI/CD), §10 (on-device checklist), §12 (grabs and leftovers). Also read the "Plan 3 must handle" sections of `docs/superpowers/plan-1-followups.md` and `docs/superpowers/plan-2a-followups.md`.

**Scope:**

| | |
|---|---|
| **Plans 1, 2a–2c (done)** | Playback, the TV interface, the setup wizard, Settings |
| **This plan (3a)** | Everything in the Goal above |
| **Plan 3b** | Work that needs measurements on the device: memory (stream rings, cover decode size), raw MP3 seek, the engine's shutdown internals, stream hardening, input checks with Main_MiSTer, render speed, spikes 2 and 3 |

**How this plan's code was produced:** every block was built and run before the plan was written.
- **Checks:**
  - `go test -race ./...` passes.
  - `./scripts/test-launcher.sh` passes, against stand-ins for BGM, SAM and the app.
  - `make e2e` passes.
  - `make release` built the zip, and the ARM binary passed the glibc check (2.29 ≤ 2.31).
- **Replay:** the blocks were replayed task by task on a fresh clone of `main`. At every task the tests failed before the code and passed after, and the tree ended identical to the prototype's, byte for byte.
- **MiSTer facts:** the Scripts menu, BGM, SAM, `KDSETMODE`, the Downloader format, `MiSTer.ini` and the stock tools come from the upstream sources:
  - Main_MiSTer `menu.cpp`, `video.cpp` and `MiSTer.ini`;
  - mrext `docs/bgm.md`;
  - MiSTer_SAM `MiSTer_SAM_on.sh`;
  - MiSTerFin and MiSTer_Hi-Fi;
  - Downloader_MiSTer `docs/custom-databases.md`;
  - the Buildroot_MiSTer stock inventory.
- **Not yet tried on a MiSTer**, which was unavailable. Task 8 records the checks still to run.

Copy blocks exactly. Patches must apply cleanly with `git apply`. New shell scripts must be executable.

## Global Constraints

- **Toolchain:** Go 1.26+ (`go.mod` says `go 1.26.0`). No new modules. No cgo outside `internal/audio`. ARM build: `GOARCH=arm GOARM=7` with `zig cc -target arm-linux-gnueabihf.2.31`; `scripts/check-glibc.sh` must report ≤ 2.31.
- **Install layout (spec §9):**

  ```
  /media/fat/Scripts/MiSTer_Subsonic.sh
  /media/fat/mistersubsonic/mistersubsonic
  /media/fat/mistersubsonic/config.toml
  /media/fat/mistersubsonic/config.example.toml
  /media/fat/mistersubsonic/fonts/
  /media/fat/mistersubsonic/log.txt       (1 MB × 2)
  /media/fat/mistersubsonic/servers/<name>/
  ```

  Plan 2c added `servers/<name>/`, which holds `state.json` and `cache/`. This plan adds `crash.txt`.
- **Launcher (bash):** Main_MiSTer runs Scripts in bash, as root, on tty2, from the script's folder.
  - Use only tools a stock MiSTer has: `flock`, `socat`, `pidof`, `ps`, `grep`, `cut`, `kill`, `sleep`. There is **no** `timeout`, `pgrep` or `pkill`.
  - BGM has no pause command: `stop`, then `play` afterwards.
- **Console:** `KD_GRAPHICS` while the app draws on the framebuffer. The console is restored on a normal exit, on SIGINT/SIGTERM, after a recovered panic, and by `-restore-console` from the launcher after every exit.
- **Tests never touch real devices:**
  - no `/dev/tty*` or `/dev/fb0` (the ioctls and paths are replaceable);
  - the null audio device or fakes;
  - no network beyond `httptest` and the mock server.
- **🔇 Sound safety:**
  - Never play sound on the user's devices, or on the MiSTer, without asking first.
  - Desktop runs start at −30 dB.
  - `make deploy` and the on-device checks run only with the user's go-ahead.
- **No secrets** in logs, the config example, commits or docs. `.env` is never read by scripts or tests.
- **Publishing:** nothing is pushed, tagged or released by this plan. The release workflow runs when the user pushes a `v*` tag.
- **Golden screenshots are exact pixels.** Regenerate them with `go test ./internal/ui -update` only in the step that says so, and look at every changed PNG.

## Review Focus

These five situations are implied by the spec but no feature test covers them. They are the most likely to bite a real user, and each gets its test in the task that owns the code:

1. **A crash the app can't catch** (SIGKILL, a Go runtime fatal error, a panic off the UI goroutine). The console comes back to text, and BGM and SAM come back. Tests: the launcher case "restores everything after a crash" (Task 6); `TestRestoreTextSetsTextMode` (Task 4).
2. **A clean-up that hangs**, such as a queue save to a server that stopped answering, or an audio close that never returns. The app still exits, restoring the console, instead of leaving the TV frozen. Tests: `TestShutdownDeadlineFires`, `TestShutdownDeadlineIsStoppedAfterACleanExit` (Task 4).
3. **A missing or read-only data folder.** The app still starts, logging to stderr. Test: `TestAnUnwritableLogFallsBackToStderr` (Task 1).
4. **Typing an "m"** in Search or the wizard types it; it doesn't mute. Mute survives a server switch, and a volume change brings the sound back. Tests: `TestTypingMDoesNotMute`, `TestMuteCarriesToTheNextServersPlayer`, `TestMuteKeyTogglesAndTheVolumeUnmutes` (Task 3).
5. **A second app already running** (started by hand, or left by a crash) is stopped before the new one starts. A second launcher at the same moment is refused. Tests: the launcher cases "stops a leftover app first" and "won't start while another launcher holds the lock" (Task 6).

## Decisions this plan makes (the spec is silent or leaves room)

- **Where things live:** BGM, SAM and the single-instance lock are in the launcher, as spec §9 says, not in `internal/platform` as §3's module list suggests. Only a process outside the app can put them back after the app is killed.
- **Plan 3 is split.** 3a (this plan) makes the app installable and safe on the device. 3b is the work that needs measurements on the MiSTer.
- **BGM** can't pause. If `/tmp/bgm.sock` answers `status` and its playback isn't `disabled`, the launcher sends `stop`, then `play` after the app exits. `play` starts the playlist again rather than resuming the same track. This is what MiSTerFin does.
- **SAM** has no suspend that leaves things alone. While SAM's `MiSTer_SAM_MCP` runs, the launcher calls `MiSTer_SAM_on.sh disable`, then `enable` after the app exits. This is what MiSTer_Hi-Fi does. If the power is cut while the app runs, SAM stays disabled until it is enabled again.
- **One instance:**
  - Leftover `mistersubsonic` processes are stopped by name: TERM, then KILL after 5 s.
  - Then the launcher takes `flock` on `/tmp/mistersubsonic.lock`. The app inherits the lock.
  - A second launcher waits 5 s for the lock, then gives up.
- **Console:** `/dev/tty0` (the active virtual terminal) is tried first, then `/dev/tty`. The console is restored to the mode it was found in. The launcher hides the cursor (`\e[?25l`) and afterwards shows it again and clears the screen.
- **Watchdog:** every 2 s a 17×17 grid of framebuffer pixels is compared with the last frame. A mismatch means a full repaint, logged once per episode. Only the framebuffer display checks itself.
- **Mute:**
  - It isn't saved: the app always starts with the sound on.
  - It is on M on a keyboard, and Settings → Playback → Mute for controllers.
  - Changing the volume unmutes.
  - Mute carries over to a new server's player.
  - Now Playing shows "Muted" where the volume goes.
- **Crash output:** `debug.SetCrashOutput` writes to `crash.txt` beside the log. If the file is over 1 MB at start, it is deleted.
- **Shutdown:** once the UI has quit, the clean-up has 10 s. After that the app restores the console and exits with code 3.
- **Releases:**
  - `sdcard/` holds the launcher and the example config.
  - `make release` adds the ARM binary (with `-X main.version`), `LICENSE` and `THIRD_PARTY.txt`. The third-party file carries the notices the BSD and OFL licenses require for binaries.
  - The Downloader database uses `db_id` `mistersubsonic` and one release asset per file. It is published as plain `db.json` on the `db` branch, force-pushed by each release.
  - The README's `db_url` has `OWNER/REPO` placeholders until the GitHub repository exists.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/logfile/logfile.go` | size-capped `log.txt` with one rotation | 1 |
| `cmd/mistersubsonic/main.go` | `-log`, `crash.txt`; console mode, panic recovery, shutdown deadline, `-restore-console` | 1, 4 |
| `internal/gfx/fbdev_linux.go`, `fbpack.go`, `present.go` | pan offset, page-aligned `/dev/mem`, `Intact`, `gfx.Checker` | 2 |
| `internal/ui/watchdog.go` | the 2 s overwrite check | 2 |
| `internal/player/player.go`, `internal/input/*`, `internal/devview/*`, `internal/ui/screens_settings.go` … | mute | 3 |
| `internal/platform/console_linux.go`, `console_other.go` | `KD_GRAPHICS` and restore | 4 |
| `sdcard/mistersubsonic/config.example.toml` | every option, explained | 5 |
| `sdcard/Scripts/MiSTer_Subsonic.sh`, `scripts/test-launcher.sh` | the launcher and its tests | 6 |
| `tools/mkdb/main.go`, `Makefile`, `.github/workflows/release.yml` | releases and the Downloader database | 7 |
| `README.md`, `docs/testing-on-mister.md`, `docs/spikes.md`, `docs/superpowers/plan-2a-followups.md` | install, `MiSTer.ini`, the checklist, follow-ups | 8 |

---

### Task 1: The log file

**Files:**
- Create: `internal/logfile/logfile.go`
- Modify: `cmd/mistersubsonic/main.go`
- Test: `internal/logfile/logfile_test.go` (new); `cmd/mistersubsonic/main_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `logfile.Open(path string, max int64) (*logfile.File, error)`. `*File` is an `io.Writer`, safe for concurrent use. When a write would take the file past `max`, `log.txt` becomes `log.txt.1` (replacing the older one) and a new file starts. `Close()` is idempotent, and later writes return `os.ErrClosed`.
  - `main`:
    - a new `-log` flag: `auto`, `-` or a path. `auto` means `log.txt` next to the config with `-display fbdev`, and stderr otherwise.
    - `openLog(flagPath, display, dataDir) func()`, and `const logMax = 1 << 20`.
    - runtime crashes go to `crash.txt` beside the log (`debug.SetCrashOutput`).
    - "starting" and "exiting" lines are logged.
    - an empty `flags.log` (the tests' zero value) means stderr.

- [ ] **Step 1: Write the failing tests**

`internal/logfile/logfile_test.go` (new file):

```go
package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRotatesAtTheCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, err := Open(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, s := range []string{"aaaa\n", "bbbb\n", "cccc\n", "dddd\n", "eeee\n"} {
		if _, err := l.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	// 10 bytes each: a+b fill log.txt, c+d the next one, e starts a third;
	// only the newest two files are kept.
	if got := read(t, p); got != "eeee\n" {
		t.Errorf("log.txt = %q", got)
	}
	if got := read(t, p+".1"); got != "cccc\ndddd\n" {
		t.Errorf("log.txt.1 = %q", got)
	}
}

func TestAppendsToAnExistingLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	os.WriteFile(p, []byte("12345678\n"), 0o644)
	l, err := Open(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("x\n")) // 9 + 2 > 10: the old content moves to .1
	l.Close()
	if read(t, p) != "x\n" || read(t, p+".1") != "12345678\n" {
		t.Fatalf("log %q, .1 %q", read(t, p), read(t, p+".1"))
	}
}

func TestALongLineStillGoesIn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 4)
	long := strings.Repeat("z", 20) + "\n"
	l.Write([]byte(long))
	l.Write([]byte(long))
	l.Close()
	if read(t, p) != long || read(t, p+".1") != long {
		t.Fatal("a line longer than the cap was split or dropped")
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	l, _ := Open(filepath.Join(t.TempDir(), "log.txt"), 100)
	l.Close()
	if _, err := l.Write([]byte("x")); err != os.ErrClosed {
		t.Fatalf("err %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
}

func TestConcurrentWritesKeepWholeLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log.txt")
	l, _ := Open(p, 1<<20)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				l.Write([]byte("0123456789\n"))
			}
		}()
	}
	wg.Wait()
	l.Close()
	for _, line := range strings.Split(strings.TrimSuffix(read(t, p), "\n"), "\n") {
		if line != "0123456789" {
			t.Fatalf("mixed line %q", line)
		}
	}
}

func TestOpenFailsInAMissingDir(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "no", "log.txt"), 10); err == nil {
		t.Fatal("opened a log in a missing directory")
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t1-test.patch` and apply it from the repository root with `git apply /tmp/t1-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 0):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index 84dc69a..e3d26f6 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -3,11 +3,13 @@ package main
 import (
 	"errors"
 	"io"
+	"log"
 	"math"
 	"net/http"
 	"net/http/httptest"
 	"os"
 	"path/filepath"
+	"strings"
 	"testing"
 	"time"
 
@@ -130,3 +132,43 @@ func TestExitDuringConnectBuildsNothing(t *testing.T) {
 		t.Fatal("a session was built after the app exited")
 	}
 }
+
+func TestLogGoesToTheFileGiven(t *testing.T) {
+	nullDevice(t)
+	dir, cfg := writeConfig(t, "http://127.0.0.1:1")
+	logPath := filepath.Join(dir, "app.log")
+	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 300 * time.Millisecond, log: logPath})
+	if err != nil {
+		t.Fatalf("run: %v", err)
+	}
+	b, _ := os.ReadFile(logPath)
+	if !strings.Contains(string(b), "MiSTer Subsonic dev starting") || !strings.Contains(string(b), "exiting") {
+		t.Fatalf("log:\n%s", b)
+	}
+	if _, err := os.Stat(filepath.Join(dir, "crash.txt")); err != nil {
+		t.Fatal("no crash.txt beside the log:", err)
+	}
+}
+
+func TestLogDefaultsToLogTxtOnTheFramebuffer(t *testing.T) {
+	dir := t.TempDir()
+	closeLog := openLog("auto", "fbdev", dir)
+	log.Print("hello")
+	closeLog()
+	if b, _ := os.ReadFile(filepath.Join(dir, "log.txt")); !strings.Contains(string(b), "hello") {
+		t.Fatalf("log.txt: %q", b)
+	}
+	other := t.TempDir()
+	openLog("auto", "viewer", other)() // elsewhere: stderr, no files
+	if ents, _ := os.ReadDir(other); len(ents) != 0 {
+		t.Fatalf("the viewer wrote %v", ents)
+	}
+}
+
+func TestAnUnwritableLogFallsBackToStderr(t *testing.T) {
+	closeLog := openLog(filepath.Join(t.TempDir(), "missing", "log.txt"), "fbdev", "")
+	defer closeLog()
+	if log.Writer() != os.Stderr {
+		t.Fatal("the log went somewhere other than stderr")
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/logfile ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: Open
unknown field log in struct literal of type flags
undefined: openLog
```

- [ ] **Step 3: Implement**

`internal/logfile/logfile.go` (new file):

```go
// Package logfile is the app's log on the SD card: log.txt, capped in size.
// When a write would take log.txt past the cap, log.txt becomes log.txt.1
// (replacing the older one) and a new log.txt starts, so the log never takes
// more than twice the cap (spec §9: 1 MB × 2).
package logfile

import (
	"fmt"
	"os"
	"sync"
)

// File is a size-capped log file. It is safe for concurrent use.
type File struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// Open appends to the log at path, rotating it when it passes max bytes.
func Open(path string, max int64) (*File, error) {
	l := &File{path: path, max: max}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("logfile: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logfile: %w", err)
	}
	l.f, l.size = f, st.Size()
	return nil
}

// Write appends p, rotating first if p would take the file past the cap.
// A line longer than the cap still goes in whole, into a fresh file.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate moves log.txt to log.txt.1. If the rename fails the log goes on
// growing rather than losing lines.
func (l *File) rotate() error {
	l.f.Close()
	l.f = nil
	os.Rename(l.path, l.path+".1")
	return l.open()
}

// Close closes the file. Later writes fail with os.ErrClosed.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
```

Then Save this patch as `/tmp/t1-code.patch` and apply it from the repository root with `git apply /tmp/t1-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 0):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index b8cd232..6c68139 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -15,6 +15,7 @@ import (
 	"os/signal"
 	"path/filepath"
 	"runtime"
+	"runtime/debug"
 	"syscall"
 	"time"
 
@@ -23,6 +24,7 @@ import (
 	"mistersubsonic/internal/devview"
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
+	"mistersubsonic/internal/logfile"
 	"mistersubsonic/internal/ui"
 )
 
@@ -33,10 +35,10 @@ var openDevice = audio.OpenDevice
 var version = "dev"
 
 type flags struct {
-	config, display, viewerAddr, frames, profile, fbdev, keys string
-	null                                                      bool
-	volume                                                    float64
-	exitAfter                                                 time.Duration
+	config, display, viewerAddr, frames, profile, fbdev, keys, log string
+	null                                                           bool
+	volume                                                         float64
+	exitAfter                                                      time.Duration
 }
 
 func main() {
@@ -49,6 +51,7 @@ func main() {
 	flag.StringVar(&f.fbdev, "fb", "/dev/fb0", "framebuffer device")
 	flag.BoolVar(&f.null, "null", false, "use the null audio device (silent)")
 	flag.StringVar(&f.keys, "keys", "", `scripted button presses for testing, e.g. "a:2s,a,a" (see keys.go)`)
+	flag.StringVar(&f.log, "log", "auto", "log file: auto (log.txt next to the config on the framebuffer, stderr elsewhere), - (stderr) or a path")
 	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
 	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
 	flag.Parse()
@@ -79,6 +82,46 @@ func startVolume(cfgDB, flagDB float64, null bool, display, goos, goarch string)
 	return cfgDB, false
 }
 
+// logMax caps log.txt (and crash.txt): two files of 1 MB at most (spec §9).
+const logMax = 1 << 20
+
+// openLog sends the log to log.txt next to the config when the app runs on
+// the framebuffer (the MiSTer: nobody sees stderr there), or where -log
+// says. Crashes the app can't catch (runtime errors, panics off the UI
+// goroutine) go to crash.txt beside it. A log that can't be opened falls
+// back to stderr. The returned func restores stderr and closes the files.
+func openLog(flagPath, display, dataDir string) func() {
+	path := flagPath
+	if path == "auto" {
+		path = "-"
+		if display == "fbdev" {
+			path = filepath.Join(dataDir, "log.txt")
+		}
+	}
+	if path == "-" || path == "" {
+		return func() {}
+	}
+	lf, err := logfile.Open(path, logMax)
+	if err != nil {
+		log.Printf("log: %v (logging to stderr)", err)
+		return func() {}
+	}
+	log.SetOutput(lf)
+	crash := filepath.Join(filepath.Dir(path), "crash.txt")
+	if st, err := os.Stat(crash); err == nil && st.Size() > logMax {
+		os.Remove(crash)
+	}
+	if cf, err := os.OpenFile(crash, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644); err == nil {
+		debug.SetCrashOutput(cf, debug.CrashOptions{}) // keeps its own copy of the file
+		cf.Close()
+	}
+	return func() {
+		debug.SetCrashOutput(nil, debug.CrashOptions{})
+		log.SetOutput(os.Stderr)
+		lf.Close()
+	}
+}
+
 func run(f flags) error {
 	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
 	defer stop()
@@ -95,6 +138,12 @@ func run(f flags) error {
 		}
 	}
 
+	dataDir := filepath.Dir(f.config)
+	closeLog := openLog(f.log, f.display, dataDir)
+	defer closeLog()
+	log.Printf("MiSTer Subsonic %s starting (%s, config %s)", version, f.display, f.config)
+	defer log.Printf("MiSTer Subsonic exiting")
+
 	cfg, warns, cfgErr := config.Load(f.config)
 	if cfg == nil {
 		cfg = config.Default()
@@ -102,7 +151,6 @@ func run(f flags) error {
 	for _, w := range warns {
 		log.Printf("config: %s", w)
 	}
-	dataDir := filepath.Dir(f.config)
 
 	// Display and layout.
 	profileName := cfg.Display.Profile
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/logfile ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go cmd/mistersubsonic/main_test.go internal/logfile/logfile.go internal/logfile/logfile_test.go
git commit -m "app: log to log.txt (1 MB x 2) on the MiSTer, crashes to crash.txt" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 2: The framebuffer: pan offset, /dev/mem alignment, overwrite watchdog

**Files:**
- Create: `internal/ui/watchdog.go`
- Modify: `internal/gfx/fbdev_linux.go`, `internal/gfx/fbpack.go`, `internal/gfx/present.go`, `internal/ui/app.go`
- Test: `internal/ui/watchdog_test.go` (new); `internal/gfx/fbdev_linux_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `fbLayout(v, fx) (fbFormat, off, end int, err error)`: the screen starts at the pan offset (`yoffset*stride + xoffset*bpp/8`) and must end inside `smem_len`.
  - `pageAlign(phys uintptr, page int) (base uintptr, delta int)`: the `/dev/mem` fallback maps from the page start and slices from `delta`.
  - `FB` keeps `mapping` (for `Munmap`) apart from `mem` (the visible screen). `(*FB).Intact() bool` compares a 17×17 grid of sampled pixels with the frame last presented.
  - `fbFormat.matches`, `encode` and `read`.
  - `gfx.Checker` is an interface: `Intact() bool`.
  - `App` fields `checkAt` and `overwritten`, `checkScreen(now)`, and `const watchdogEvery = 2 * time.Second`. The check is scheduled only for displays that implement `gfx.Checker`, and an overwrite is logged once per episode.

- [ ] **Step 1: Write the failing tests**

`internal/ui/watchdog_test.go` (new file):

```go
package ui

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"mistersubsonic/internal/gfx"
)

// checkDisplay is a display that reports whether its frame is intact.
type checkDisplay struct {
	*gfx.Headless
	intact bool
	checks int
}

func (d *checkDisplay) Intact() bool { d.checks++; return d.intact }

func TestWatchdogRepaintsAnOverwrittenScreen(t *testing.T) {
	log.SetOutput(io.Discard) // the repaint is logged
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	d := &checkDisplay{Headless: gfx.NewHeadless(ProfileCRT240.W, ProfileCRT240.H, ""), intact: true}
	now := time.Unix(1_800_000_000, 0)
	a, err := New(Options{Display: d, Profile: ProfileCRT240, Library: sampleLibrary(), Player: newFakePlayer(), Art: fakeArt{},
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	a.render()
	if w := a.untilWake(); w > watchdogEvery {
		t.Fatalf("next wake in %v, want the watchdog within %v", w, watchdogEvery)
	}
	now = now.Add(time.Second)
	a.onWake()
	if d.checks != 0 {
		t.Fatal("checked before it was due")
	}
	now = now.Add(time.Second)
	a.onWake()
	if d.checks != 1 || a.dirty {
		t.Fatalf("checks %d dirty %v: an intact screen must not be repainted", d.checks, a.dirty)
	}
	d.intact = false
	now = now.Add(watchdogEvery)
	a.onWake()
	if d.checks != 2 || !a.dirty {
		t.Fatalf("checks %d dirty %v: an overwritten screen must be repainted", d.checks, a.dirty)
	}
	frames := d.Frames()
	a.render()
	if d.Frames() != frames+1 {
		t.Fatal("no frame presented after the overwrite")
	}
}

func TestNoWatchdogWithoutAChecker(t *testing.T) {
	ta := newTestApp(t, ProfileHDMI)
	ta.render()
	ta.now = ta.now.Add(time.Minute)
	ta.onWake() // must not panic on a display that can't check itself
	if !ta.checkAt.IsZero() {
		t.Fatal("a watchdog was scheduled for the headless display")
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t2-test.patch` and apply it from the repository root with `git apply /tmp/t2-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/gfx/fbdev_linux_test.go b/internal/gfx/fbdev_linux_test.go
index 330dc02..e1ea44d 100644
--- a/internal/gfx/fbdev_linux_test.go
+++ b/internal/gfx/fbdev_linux_test.go
@@ -5,7 +5,8 @@ package gfx
 import "testing"
 
 func TestFBClosedIsSafe(t *testing.T) {
-	b := &FB{mem: make([]byte, 16), fmt: fbFormat{2, 2, 8, 16}}
+	mem := make([]byte, 16)
+	b := &FB{mapping: mem, mem: mem, fmt: fbFormat{2, 2, 8, 16}}
 	b.Close() // Munmap of a heap slice errors; the fields must still be cleared
 	b.Close()
 	if err := b.Present(NewCanvas(2, 2)); err == nil {
@@ -28,3 +29,78 @@ func TestCheckLayout(t *testing.T) {
 		t.Fatal("32bpp BGR accepted")
 	}
 }
+
+func xrgb(xres, yres, xoff, yoff, stride, smem uint32) (fbVarScreenInfo, fbFixScreenInfo) {
+	v := fbVarScreenInfo{Xres: xres, Yres: yres, Xoffset: xoff, Yoffset: yoff, BitsPerPixel: 32,
+		Red: fbBitfield{16, 8, 0}, Green: fbBitfield{8, 8, 0}, Blue: fbBitfield{0, 8, 0}}
+	return v, fbFixScreenInfo{LineLength: stride, SmemLen: smem}
+}
+
+func TestFBLayoutFollowsThePanOffset(t *testing.T) {
+	// A console panned to its second page: the screen starts yoffset lines in.
+	v, fx := xrgb(640, 480, 0, 480, 2560, 2560*960)
+	_, off, end, err := fbLayout(&v, &fx)
+	if err != nil || off != 2560*480 || end != 2560*960 {
+		t.Fatalf("off %d end %d err %v", off, end, err)
+	}
+	v, fx = xrgb(640, 480, 8, 2, 2560, 2560*960)
+	if _, off, _, _ := fbLayout(&v, &fx); off != 2*2560+8*4 {
+		t.Fatalf("x/y pan: off %d", off)
+	}
+}
+
+func TestFBLayoutNeedsTheScreenInsideSmem(t *testing.T) {
+	v, fx := xrgb(640, 480, 0, 0, 2560, 2560*480)
+	if _, _, _, err := fbLayout(&v, &fx); err != nil {
+		t.Fatalf("an exact fit was refused: %v", err)
+	}
+	v, fx = xrgb(640, 480, 0, 1, 2560, 2560*480) // panned one line past the end
+	if _, _, _, err := fbLayout(&v, &fx); err == nil {
+		t.Fatal("a screen ending past smem_len was accepted")
+	}
+	v, fx = xrgb(640, 480, 0, 0, 100, 1<<24) // stride shorter than a line
+	if _, _, _, err := fbLayout(&v, &fx); err == nil {
+		t.Fatal("a stride shorter than the line was accepted")
+	}
+}
+
+func TestPageAlign(t *testing.T) {
+	for _, c := range []struct {
+		phys  uintptr
+		base  uintptr
+		delta int
+	}{{0x1E000000, 0x1E000000, 0}, {0x1E000800, 0x1E000000, 0x800}, {0x1E000FFF, 0x1E000000, 0xFFF}, {0x1E001000, 0x1E001000, 0}} {
+		if b, d := pageAlign(c.phys, 4096); b != c.base || d != c.delta {
+			t.Errorf("pageAlign(%#x) = %#x,%#x; want %#x,%#x", c.phys, b, d, c.base, c.delta)
+		}
+	}
+}
+
+func TestIntactNoticesAnOverwrite(t *testing.T) {
+	for _, bpp := range []int{32, 16} {
+		f := fbFormat{width: 64, height: 48, stride: 64 * bpp / 8, bpp: bpp}
+		b := &FB{mem: make([]byte, f.stride*f.height), fmt: f}
+		if !b.Intact() {
+			t.Fatalf("%d bpp: not intact before the first frame", bpp)
+		}
+		c := NewCanvas(64, 48)
+		for i := range c.Pix {
+			c.Pix[i] = uint32(i*2654435761) & 0xFFFFFF
+		}
+		b.Present(c)
+		if !b.Intact() {
+			t.Fatalf("%d bpp: the frame just presented doesn't match", bpp)
+		}
+		for i := range b.mem[:f.stride*4] { // the console writes over the top lines
+			b.mem[i] ^= 0xFF
+		}
+		if b.Intact() {
+			t.Fatalf("%d bpp: an overwritten top missed", bpp)
+		}
+		b.Present(c)
+		copy(b.mem[len(b.mem)-f.stride:], make([]byte, f.stride)) // and the last line
+		if b.Intact() {
+			t.Fatalf("%d bpp: an overwritten bottom line missed", bpp)
+		}
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/gfx ./internal/ui`

Expected: FAIL, e.g.:

```
unknown field mapping in struct literal of type FB
undefined: fbLayout
undefined: pageAlign
b.Intact undefined (type *FB has no field or method Intact)
undefined: watchdogEvery
ta.checkAt undefined (type *testApp has no field or method checkAt)
```

- [ ] **Step 3: Implement**

`internal/ui/watchdog.go` (new file):

```go
package ui

import (
	"log"
	"time"

	"mistersubsonic/internal/gfx"
)

// The overwrite watchdog (spec §8.1): every watchdogEvery, a display that
// can check itself (the MiSTer framebuffer) is asked whether its last frame
// is still on screen. If Main_MiSTer or the console drew over it, the whole
// screen is repainted.
const watchdogEvery = 2 * time.Second

// checkScreen runs the watchdog when it is due.
func (a *App) checkScreen(now time.Time) {
	chk, ok := a.o.Display.(gfx.Checker)
	if !ok || a.checkAt.IsZero() || now.Before(a.checkAt) {
		return
	}
	a.checkAt = now.Add(watchdogEvery)
	if chk.Intact() {
		a.overwritten = false
		return
	}
	if !a.overwritten { // once per overwrite, not every 2 s while it lasts
		log.Print("ui: something drew over the screen; repainting")
	}
	a.overwritten, a.dirty = true, true
}
```

Then Save this patch as `/tmp/t2-code.patch` and apply it from the repository root with `git apply /tmp/t2-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 1):

```diff
diff --git a/internal/gfx/fbdev_linux.go b/internal/gfx/fbdev_linux.go
index 7b01ae1..b986439 100644
--- a/internal/gfx/fbdev_linux.go
+++ b/internal/gfx/fbdev_linux.go
@@ -44,9 +44,11 @@ type fbFixScreenInfo struct {
 
 // FB is the Linux framebuffer display.
 type FB struct {
-	f   *os.File
-	mem []byte
-	fmt fbFormat
+	f       *os.File
+	mapping []byte // the whole mmap, for Munmap
+	mem     []byte // the visible screen inside it
+	fmt     fbFormat
+	last    *Canvas // the frame last presented, for Intact
 }
 
 func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
@@ -73,29 +75,43 @@ func OpenFB(path string) (*FB, error) {
 		f.Close()
 		return nil, fmt.Errorf("gfx: FBIOGET_FSCREENINFO: %w", err)
 	}
-	ff := fbFormat{width: int(v.Xres), height: int(v.Yres), stride: int(fx.LineLength), bpp: int(v.BitsPerPixel)}
-	if err := ff.validate(); err != nil {
-		f.Close()
-		return nil, err
-	}
-	if err := v.checkLayout(); err != nil {
+	ff, off, end, err := fbLayout(&v, &fx)
+	if err != nil {
 		f.Close()
 		return nil, err
 	}
-	size := ff.stride * ff.height
-	if uint64(size) > uint64(fx.SmemLen) {
-		f.Close()
-		return nil, fmt.Errorf("gfx: framebuffer memory %d < %d needed", fx.SmemLen, size)
-	}
-	mem, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
+	mapping, err := syscall.Mmap(int(f.Fd()), 0, end, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
+	start := off
 	if err != nil {
-		mem, err = mmapDevMem(fx.SmemStart, size)
+		var delta int
+		mapping, delta, err = mmapDevMem(fx.SmemStart, end)
 		if err != nil {
 			f.Close()
 			return nil, err
 		}
+		start += delta
 	}
-	return &FB{f: f, mem: mem, fmt: ff}, nil
+	return &FB{f: f, mapping: mapping, mem: mapping[start : start+end-off], fmt: ff}, nil
+}
+
+// fbLayout checks the screen format and finds the visible screen in
+// framebuffer memory: it starts at the pan offset (the console may have
+// panned to another page) and must end inside smem_len. It returns the
+// screen's byte offset and where it ends.
+func fbLayout(v *fbVarScreenInfo, fx *fbFixScreenInfo) (ff fbFormat, off, end int, err error) {
+	ff = fbFormat{width: int(v.Xres), height: int(v.Yres), stride: int(fx.LineLength), bpp: int(v.BitsPerPixel)}
+	if err := ff.validate(); err != nil {
+		return ff, 0, 0, err
+	}
+	if err := v.checkLayout(); err != nil {
+		return ff, 0, 0, err
+	}
+	off = int(v.Yoffset)*ff.stride + int(v.Xoffset)*ff.bpp/8
+	end = off + ff.stride*ff.height
+	if uint64(end) > uint64(fx.SmemLen) {
+		return ff, 0, 0, fmt.Errorf("gfx: framebuffer memory %d < %d needed", fx.SmemLen, end)
+	}
+	return ff, off, end, nil
 }
 
 // checkLayout accepts only XRGB8888 (32 bpp) and RGB565 (16 bpp) channel layouts.
@@ -115,17 +131,28 @@ func (v *fbVarScreenInfo) checkLayout() error {
 	return nil
 }
 
-func mmapDevMem(phys uintptr, size int) ([]byte, error) {
+// mmapDevMem maps size bytes of physical memory at phys through /dev/mem.
+// mmap needs a page-aligned offset, so the mapping starts at phys's page;
+// delta is where phys lies inside it.
+func mmapDevMem(phys uintptr, size int) (mapping []byte, delta int, err error) {
 	m, err := os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
 	if err != nil {
-		return nil, fmt.Errorf("gfx: fb mmap failed and /dev/mem unavailable: %w", err)
+		return nil, 0, fmt.Errorf("gfx: fb mmap failed and /dev/mem unavailable: %w", err)
 	}
 	defer m.Close()
-	mem, err := syscall.Mmap(int(m.Fd()), int64(phys), size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
+	base, delta := pageAlign(phys, os.Getpagesize())
+	mapping, err = syscall.Mmap(int(m.Fd()), int64(base), size+delta, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
 	if err != nil {
-		return nil, fmt.Errorf("gfx: mmap /dev/mem at %#x: %w", phys, err)
+		return nil, 0, fmt.Errorf("gfx: mmap /dev/mem at %#x: %w", phys, err)
 	}
-	return mem, nil
+	return mapping, delta, nil
+}
+
+// pageAlign splits a physical address into the start of its page and the
+// offset inside that page.
+func pageAlign(phys uintptr, page int) (base uintptr, delta int) {
+	delta = int(phys % uintptr(page))
+	return phys - uintptr(delta), delta
 }
 
 func (b *FB) Size() (int, int) { return b.fmt.width, b.fmt.height }
@@ -138,9 +165,20 @@ func (b *FB) Present(c *Canvas) error {
 		return fmt.Errorf("gfx: frame %dx%d != framebuffer %dx%d", c.W, c.H, b.fmt.width, b.fmt.height)
 	}
 	b.fmt.pack(b.mem, c)
+	b.last = c
 	return nil
 }
 
+// Intact reports whether the screen still shows the frame last presented,
+// from a grid of sampled pixels: Main_MiSTer or the console may have drawn
+// over it (spec §8.1). It is true before the first Present and after Close.
+func (b *FB) Intact() bool {
+	if b.mem == nil || b.last == nil {
+		return true
+	}
+	return b.fmt.matches(b.mem, b.last)
+}
+
 // Blank clears the framebuffer to black (used on exit).
 func (b *FB) Blank() {
 	for i := range b.mem {
@@ -154,8 +192,8 @@ func (b *FB) Close() error {
 		return nil
 	}
 	b.Blank()
-	err := syscall.Munmap(b.mem)
-	b.mem = nil
+	err := syscall.Munmap(b.mapping)
+	b.mem, b.mapping, b.last = nil, nil, nil
 	if b.f != nil {
 		if e := b.f.Close(); err == nil {
 			err = e
diff --git a/internal/gfx/fbpack.go b/internal/gfx/fbpack.go
index 43d5155..a4b87b7 100644
--- a/internal/gfx/fbpack.go
+++ b/internal/gfx/fbpack.go
@@ -41,3 +41,38 @@ func (f fbFormat) pack(mem []byte, c *Canvas) {
 		}
 	}
 }
+
+// Sampling grid for matches: 17×17 points, corners and edges included.
+const matchGrid = 16
+
+// matches reports whether mem still holds c at a grid of sample points.
+func (f fbFormat) matches(mem []byte, c *Canvas) bool {
+	for gy := 0; gy <= matchGrid; gy++ {
+		y := min(gy*f.height/matchGrid, f.height-1)
+		for gx := 0; gx <= matchGrid; gx++ {
+			x := min(gx*f.width/matchGrid, f.width-1)
+			if f.read(mem, x, y) != f.encode(c.Pix[y*c.W+x]) {
+				return false
+			}
+		}
+	}
+	return true
+}
+
+// encode is a canvas pixel as the framebuffer stores it (XRGB8888 with X
+// zero, or RGB565).
+func (f fbFormat) encode(p uint32) uint32 {
+	if f.bpp == 32 {
+		return p & 0xFFFFFF
+	}
+	return p>>8&0xF800 | p>>5&0x07E0 | p>>3&0x001F
+}
+
+// read is the stored pixel at (x, y), in encode's form.
+func (f fbFormat) read(mem []byte, x, y int) uint32 {
+	o := y*f.stride + x*f.bpp/8
+	if f.bpp == 32 {
+		return uint32(mem[o]) | uint32(mem[o+1])<<8 | uint32(mem[o+2])<<16
+	}
+	return uint32(mem[o]) | uint32(mem[o+1])<<8
+}
diff --git a/internal/gfx/present.go b/internal/gfx/present.go
index 1f16479..0f3857a 100644
--- a/internal/gfx/present.go
+++ b/internal/gfx/present.go
@@ -10,6 +10,12 @@ type Display interface {
 	Close() error
 }
 
+// Checker is a Display that can tell whether something else drew over its
+// last frame (the MiSTer framebuffer: Main_MiSTer or the console).
+type Checker interface {
+	Intact() bool
+}
+
 // Scaler maps the logical UI canvas onto the physical framebuffer.
 // Framebuffers of 288 lines or fewer are CRT modes: the image fills the
 // screen and pixels may be non-square by design (a 320x240 UI on a 640x240
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 8dc5715..89c0051 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -189,6 +189,9 @@ type App struct {
 	quit       bool
 	bDown      time.Time // when B went down on the root screen (zero if not held)
 	confirm    bool      // exit confirmation shown
+
+	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
+	overwritten bool      // the last check found the screen drawn over
 }
 
 func New(o Options) (*App, error) {
@@ -217,6 +220,9 @@ func New(o Options) (*App, error) {
 	a.canvas = gfx.NewCanvas(a.P.W, a.P.H)
 	pw, ph := o.Display.Size()
 	a.scaler = gfx.NewScaler(a.P.W, a.P.H, pw, ph)
+	if _, ok := o.Display.(gfx.Checker); ok {
+		a.checkAt = o.Now().Add(watchdogEvery)
+	}
 	for _, ch := range o.Inputs {
 		go func(ch <-chan input.Event) {
 			for e := range ch {
@@ -436,6 +442,7 @@ func (a *App) untilWake() time.Duration {
 	}
 	consider(a.mqWake)
 	consider(a.saveAt)
+	consider(a.checkAt)
 	consider(a.saverDue())
 	if a.saver {
 		consider(now.Add(saverStep)) // the drift; nothing else moves
@@ -492,6 +499,7 @@ func (a *App) onWake() {
 		a.saveAt = time.Time{}
 		a.saveConfig()
 	}
+	a.checkScreen(now)
 	if a.animate || (!a.mqWake.IsZero() && !now.Before(a.mqWake)) {
 		a.dirty = true
 	}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/gfx ./internal/ui`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/gfx/fbdev_linux.go internal/gfx/fbdev_linux_test.go internal/gfx/fbpack.go internal/gfx/present.go internal/ui/app.go internal/ui/watchdog.go internal/ui/watchdog_test.go
git commit -m "gfx: follow the pan offset, page-align /dev/mem; ui: repaint when something draws over the screen" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 3: Mute

**Files:**
- Modify: `internal/player/player.go`, `internal/input/input.go`, `internal/input/evmap.go`, `internal/devview/devview.go`, `internal/devview/page.html`, `cmd/mistersubsonic/keys.go`, `internal/ui/app.go`, `internal/ui/screens_settings.go`, `internal/ui/screens_play.go`, `internal/ui/session.go`, `README.md`
- Test: `internal/ui/mute_test.go` (new); `internal/player/player_test.go`, `internal/input/input_test.go`, `internal/devview/devview_test.go`, `internal/ui/fakes_test.go`, `internal/ui/settings_test.go` (modified)

**Interfaces:**
- **Produces:**
  - Player:
    - `(*player.Player).SetMuted(on bool)`: the engine gain is 0 while muted, and the volume is kept.
    - `player.State.Muted`.
    - `applyVolume()`.
  - Input:
    - `input.BtnMute` (keyboard only: M, `keyM = 50`; it still types `'m'`).
    - the button name `mute` in the dev viewer (`KeyM`) and in `-keys`.
  - UI:
    - `ui.Player` gains `SetMuted`.
    - `App.muted` (not saved), `setMuted(on)`, `toggleMute()` (with a toast) and `volumeText(db)` ("Muted" while muted).
    - Settings → Playback gains a Mute row after Volume.
    - A volume change unmutes. `Connected` mutes a new player when the app is muted.

- [ ] **Step 1: Write the failing tests**

`internal/ui/mute_test.go` (new file):

```go
package ui

import (
	"testing"

	"mistersubsonic/internal/input"
)

func TestMuteKeyTogglesAndTheVolumeUnmutes(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.pl.st.VolumeDB = -10
	ta.press(input.BtnMute)
	if !ta.muted || !ta.pl.st.Muted || ta.pl.st.VolumeDB != -10 {
		t.Fatalf("muted %v, player muted %v at %v dB", ta.muted, ta.pl.st.Muted, ta.pl.st.VolumeDB)
	}
	if got := ta.volumeText(-10); got != "Muted" {
		t.Fatalf("volume shown as %q while muted", got)
	}
	ta.press(input.BtnMute)
	if ta.muted || ta.pl.st.Muted || ta.volumeText(-10) != "Vol −10 dB" {
		t.Fatalf("the second press didn't unmute: %v %v %q", ta.muted, ta.pl.st.Muted, ta.volumeText(-10))
	}
	ta.press(input.BtnMute)
	ta.setVolume(-9) // Up or Down on Now Playing
	if ta.muted || ta.pl.st.Muted || ta.pl.st.VolumeDB != -9 {
		t.Fatalf("changing the volume kept the sound off: %v %v %v", ta.muted, ta.pl.st.Muted, ta.pl.st.VolumeDB)
	}
}

func TestMuteCarriesToTheNextServersPlayer(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.press(input.BtnMute)
	pl := newFakePlayer()
	ta.Connected(ConnInfo{Server: ta.cfg.Servers[1]}, ta.lib, pl, fakeArt{})
	if !pl.st.Muted {
		t.Fatal("the new server's player plays while the app shows Muted")
	}
}

func TestMuteIsNotSaved(t *testing.T) {
	ta, _ := connectedApp(t)
	before := ta.saveAt
	ta.press(input.BtnMute)
	if ta.saveAt != before || ta.saving {
		t.Fatal("muting saved the config")
	}
}

func TestTypingMDoesNotMute(t *testing.T) {
	for name, s := range map[string]Screen{"Search": NewSearchScreen(), "the wizard": NewWizardScreen(false, false)} {
		ta, _ := connectedApp(t)
		ta.Push(s)
		ta.onInput(input.Event{Button: input.BtnMute, Kind: input.Press, Rune: 'm'})
		ta.onInput(input.Event{Button: input.BtnMute, Kind: input.Release, Rune: 'm'})
		if ta.muted {
			t.Fatalf("typing m in %s muted the sound", name)
		}
	}
}

func TestMuteSettingsRow(t *testing.T) {
	ta, _ := connectedApp(t)
	ta.Push(newSettingsList("Playback", playbackSettings))
	ta.press(input.BtnDown)
	ta.press(input.BtnA)
	rows := playbackSettings(ta.App)
	if !ta.muted || rows[0].value(ta.App) != "Muted" || rows[1].value(ta.App) != "On" {
		t.Fatalf("muted %v: volume row %q, mute row %q", ta.muted, rows[0].value(ta.App), rows[1].value(ta.App))
	}
	ta.press(input.BtnLeft)
	if ta.muted {
		t.Fatal("Left on the Mute row didn't turn the sound back on")
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t3-test.patch` and apply it from the repository root with `git apply /tmp/t3-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/internal/devview/devview_test.go b/internal/devview/devview_test.go
index e5411e8..3eb6daf 100644
--- a/internal/devview/devview_test.go
+++ b/internal/devview/devview_test.go
@@ -252,6 +252,7 @@ func TestTextKeys(t *testing.T) {
 		"t=%D0%B6&down=1":  {Kind: input.Press, Rune: 'ж'},
 		"b=b&t=%08&down=1": {Button: input.BtnB, Kind: input.Press, Rune: '\b'},
 		"b=queue&down=1":   {Button: input.BtnQueue, Kind: input.Press},
+		"b=mute&down=1":    {Button: input.BtnMute, Kind: input.Press},
 	} {
 		if c := post(q); c != http.StatusNoContent {
 			t.Fatalf("%s: status %d", q, c)
diff --git a/internal/input/input_test.go b/internal/input/input_test.go
index c650e71..722cf4d 100644
--- a/internal/input/input_test.go
+++ b/internal/input/input_test.go
@@ -160,6 +160,9 @@ func TestTranslatorTypesText(t *testing.T) {
 	if got := tr.handle(evKey, keyQ, 1); got[0] != (Event{Button: BtnQueue, Kind: Press, Rune: 'q'}) {
 		t.Fatalf("q = %v", got)
 	}
+	if got := tr.handle(evKey, keyM, 1); got[0] != (Event{Button: BtnMute, Kind: Press, Rune: 'm'}) {
+		t.Fatalf("m = %v", got)
+	}
 	if got := tr.handle(evKey, keySpace, 1); got[0] != (Event{Button: BtnStart, Kind: Press, Rune: ' '}) {
 		t.Fatalf("space = %v", got)
 	}
diff --git a/internal/player/player_test.go b/internal/player/player_test.go
index a3d425a..8702d47 100644
--- a/internal/player/player_test.go
+++ b/internal/player/player_test.go
@@ -694,3 +694,20 @@ func TestUserActionResetsFailureCount(t *testing.T) {
 		return false
 	})
 }
+
+func TestMuteKeepsTheVolume(t *testing.T) {
+	h := newHarness(t, nil)
+	h.p.SetVolumeDB(-6)
+	h.p.SetMuted(true)
+	if st := h.p.State(); !st.Muted || st.VolumeDB != -6 || h.eng.volume != 0 {
+		t.Fatalf("muted %v at %v dB, engine gain %v", st.Muted, st.VolumeDB, h.eng.volume)
+	}
+	h.p.SetVolumeDB(-10) // the volume can change while muted; the sound stays off
+	if h.eng.volume != 0 || h.p.State().VolumeDB != -10 {
+		t.Fatalf("engine gain %v at %v dB while muted", h.eng.volume, h.p.State().VolumeDB)
+	}
+	h.p.SetMuted(false)
+	if v := h.eng.volume; v < 0.31 || v > 0.32 { // -10 dB
+		t.Fatalf("unmuted gain %v, want the -10 dB volume", v)
+	}
+}
diff --git a/internal/ui/fakes_test.go b/internal/ui/fakes_test.go
index c3702da..94b7bcb 100644
--- a/internal/ui/fakes_test.go
+++ b/internal/ui/fakes_test.go
@@ -239,6 +239,7 @@ func (p *fakePlayer) Clear() {
 	p.st.Queue, p.st.Index, p.st.Status = nil, -1, player.Stopped
 }
 func (p *fakePlayer) SetVolumeDB(db float64)    { p.st.VolumeDB = max(-60, min(0, db)) }
+func (p *fakePlayer) SetMuted(on bool)          { p.st.Muted = on }
 func (p *fakePlayer) SetReplayGain(mode string) { p.call("replaygain " + mode) }
 func (p *fakePlayer) SetScrobble(on bool) {
 	if on {
diff --git a/internal/ui/settings_test.go b/internal/ui/settings_test.go
index 61a6133..b63420f 100644
--- a/internal/ui/settings_test.go
+++ b/internal/ui/settings_test.go
@@ -41,6 +41,7 @@ func TestPlaybackSettingsApplyAndSave(t *testing.T) {
 	ta.pl.st.VolumeDB = -10
 	ta.Push(newSettingsList("Playback", playbackSettings))
 	ta.press(input.BtnRight) // volume +1
+	ta.press(input.BtnDown)  // past Mute
 	ta.press(input.BtnDown)
 	ta.press(input.BtnRight) // ReplayGain off -> track
 	ta.press(input.BtnDown)
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/ui ./internal/input ./internal/devview ./internal/player ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: input.BtnMute
undefined: keyM
undefined: BtnMute
h.p.SetMuted undefined (type *Player has no field or method SetMuted)
st.Muted undefined (type State has no field or method Muted)
p.st.Muted undefined (type player.State has no field or method Muted)
ta.muted undefined (type *testApp has no field or method muted)
ta.pl.st.Muted undefined (type player.State has no field or method Muted)
```

- [ ] **Step 3: Implement**

Save this patch as `/tmp/t3-code.patch` and apply it from the repository root with `git apply /tmp/t3-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 2):

```diff
diff --git a/README.md b/README.md
index 51f10bd..ca6ba0d 100644
--- a/README.md
+++ b/README.md
@@ -41,8 +41,9 @@ applies per server (each server's cache gets that much).
 | Start | play/pause | play/pause |
 | Select | shuffle-play the list | shuffle / repeat modes |
 
-Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, PgUp/PgDn = L/R,
-Space = Start. In Search, letters type.
+Keyboard: arrows, Enter = A, Esc/Backspace = B, Tab = X, N = Y, Q = queue, M = mute,
+PgUp/PgDn = L/R, Space = Start. In Search, letters type. With a controller, mute is in
+Settings → Playback; changing the volume turns the sound back on.
 
 ## Developing
 
diff --git a/cmd/mistersubsonic/keys.go b/cmd/mistersubsonic/keys.go
index aac0397..9f1e48c 100644
--- a/cmd/mistersubsonic/keys.go
+++ b/cmd/mistersubsonic/keys.go
@@ -12,7 +12,7 @@ var keyNames = map[string]input.Button{
 	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
-	"queue": input.BtnQueue,
+	"queue": input.BtnQueue, "mute": input.BtnMute,
 }
 
 type scripted struct {
diff --git a/internal/devview/devview.go b/internal/devview/devview.go
index c2c6ca7..3cc771e 100644
--- a/internal/devview/devview.go
+++ b/internal/devview/devview.go
@@ -28,7 +28,7 @@ var buttons = map[string]input.Button{
 	"up": input.BtnUp, "down": input.BtnDown, "left": input.BtnLeft, "right": input.BtnRight,
 	"a": input.BtnA, "b": input.BtnB, "x": input.BtnX, "y": input.BtnY,
 	"l": input.BtnL, "r": input.BtnR, "select": input.BtnSelect, "start": input.BtnStart,
-	"queue": input.BtnQueue,
+	"queue": input.BtnQueue, "mute": input.BtnMute,
 }
 
 // Viewer is a gfx.Display that browsers watch.
diff --git a/internal/devview/page.html b/internal/devview/page.html
index e922fed..78540c6 100644
--- a/internal/devview/page.html
+++ b/internal/devview/page.html
@@ -13,7 +13,7 @@
 // "S") between keydown and keyup. The typed character (e.key) goes along as
 // t= so text fields can use letters that are also buttons.
 const keys = {ArrowUp:"up", ArrowDown:"down", ArrowLeft:"left", ArrowRight:"right",
-  Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"queue",
+  Enter:"a", NumpadEnter:"a", Escape:"b", Backspace:"b", Tab:"x", KeyN:"y", KeyQ:"queue", KeyM:"mute",
   PageUp:"l", PageDown:"r", Space:"start", KeyS:"select"};
 const held = new Set();
 function send(e, down) {
diff --git a/internal/input/evmap.go b/internal/input/evmap.go
index 095e575..54e7ecd 100644
--- a/internal/input/evmap.go
+++ b/internal/input/evmap.go
@@ -25,6 +25,7 @@ const (
 	keyEnter     = 28
 	keyLShift    = 42
 	keyN         = 49
+	keyM         = 50
 	keyRShift    = 54
 	keySpace     = 57
 	keyPageUp    = 104
@@ -55,7 +56,7 @@ var DefaultKeys = map[uint16]Button{
 	keyUp: BtnUp, keyDown: BtnDown, keyLeft: BtnLeft, keyRight: BtnRight,
 	keyEnter: BtnA, keyKPEnter: BtnA, keyEsc: BtnB, keyBackspace: BtnB,
 	keyTab: BtnX, keySpace: BtnStart, keyPageUp: BtnL, keyPageDown: BtnR,
-	keyN: BtnY, keyQ: BtnQueue,
+	keyN: BtnY, keyQ: BtnQueue, keyM: BtnMute,
 
 	btnEast: BtnA, btnSouth: BtnB, btnNorth: BtnX, btnWest: BtnY,
 	btnTL: BtnL, btnTR: BtnR, btnSelect: BtnSelect, btnStart: BtnStart,
diff --git a/internal/input/input.go b/internal/input/input.go
index 3644218..1b91bc1 100644
--- a/internal/input/input.go
+++ b/internal/input/input.go
@@ -23,9 +23,10 @@ const (
 	BtnSelect
 	BtnStart
 	BtnQueue // keyboard only (Q): open the queue
+	BtnMute  // keyboard only (M): mute or unmute
 )
 
-var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue"}
+var buttonNames = [...]string{"none", "up", "down", "left", "right", "A", "B", "X", "Y", "L", "R", "select", "start", "queue", "mute"}
 
 func (b Button) String() string {
 	if int(b) < len(buttonNames) {
diff --git a/internal/player/player.go b/internal/player/player.go
index eb1191d..9705c52 100644
--- a/internal/player/player.go
+++ b/internal/player/player.go
@@ -68,6 +68,7 @@ type State struct {
 	Shuffle    bool
 	Repeat     Repeat
 	VolumeDB   float64
+	Muted      bool // the output is silenced; VolumeDB is kept for unmuting
 	Transcoded bool
 	// NextIndex is the queue index that plays after the current song
 	// (honouring shuffle and repeat), or -1.
@@ -150,6 +151,7 @@ type Player struct {
 	shuffle    bool
 	repeat     Repeat
 	volumeDB   float64
+	muted      bool
 	seq        uint64
 	curID      uint64
 	curSrc     Opened
@@ -217,7 +219,7 @@ func (p *Player) Run(ctx context.Context) {
 		defer t.Stop()
 		tick = t.C
 	}
-	p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
+	p.applyVolume()
 	go p.scrobbles.Flush(ctx, p.o.API)
 	for {
 		select {
@@ -503,10 +505,27 @@ func (p *Player) SetRepeat(r Repeat) {
 func (p *Player) SetVolumeDB(db float64) {
 	p.do(func() {
 		p.volumeDB = math.Max(-60, math.Min(0, db))
-		p.o.Engine.SetVolume(dbToLinear(p.volumeDB))
+		p.applyVolume()
 	})
 }
 
+// SetMuted silences the output or brings it back at the volume set.
+func (p *Player) SetMuted(on bool) {
+	p.do(func() {
+		p.muted = on
+		p.applyVolume()
+	})
+}
+
+// applyVolume hands the engine the gain for the volume and mute state.
+func (p *Player) applyVolume() {
+	g := dbToLinear(p.volumeDB)
+	if p.muted {
+		g = 0
+	}
+	p.o.Engine.SetVolume(g)
+}
+
 // SetReplayGain sets the ReplayGain mode (off, track or album) for the
 // tracks opened from now on.
 func (p *Player) SetReplayGain(mode string) {
@@ -565,7 +584,7 @@ func (p *Player) emit(ev Event) {
 
 func (p *Player) publish() {
 	s := State{Queue: p.queue, Index: p.currentIndex(), Status: p.status, Position: p.position,
-		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Transcoded: p.curSrc.Transcoded, NextIndex: -1}
+		Shuffle: p.shuffle, Repeat: p.repeat, VolumeDB: p.volumeDB, Muted: p.muted, Transcoded: p.curSrc.Transcoded, NextIndex: -1}
 	if c := p.followingCursor(true); c >= 0 {
 		s.NextIndex = p.order[c]
 	}
diff --git a/internal/ui/app.go b/internal/ui/app.go
index 89c0051..1a9cf28 100644
--- a/internal/ui/app.go
+++ b/internal/ui/app.go
@@ -39,6 +39,7 @@ type Player interface {
 	Enqueue(songs []subsonic.Song)
 	Clear()
 	SetVolumeDB(db float64)
+	SetMuted(on bool)
 	SetReplayGain(mode string)
 	SetScrobble(on bool)
 	TogglePause()
@@ -190,6 +191,7 @@ type App struct {
 	bDown      time.Time // when B went down on the root screen (zero if not held)
 	confirm    bool      // exit confirmation shown
 
+	muted       bool      // the sound is off (not saved: the app starts with sound)
 	checkAt     time.Time // the next watchdog check (zero: the display can't check itself)
 	overwritten bool      // the last check found the screen drawn over
 }
@@ -596,6 +598,8 @@ func (a *App) dispatch(e input.Event) {
 		if !a.popTo(func(s Screen) bool { _, ok := s.(*NowPlayingScreen); return ok }) {
 			a.Push(NewNowPlayingScreen())
 		}
+	case input.BtnMute:
+		a.toggleMute()
 	case input.BtnQueue:
 		if !a.hasQueue() {
 			break
diff --git a/internal/ui/screens_play.go b/internal/ui/screens_play.go
index 1cc776f..0f4cd29 100644
--- a/internal/ui/screens_play.go
+++ b/internal/ui/screens_play.go
@@ -222,7 +222,7 @@ func (s *NowPlayingScreen) Draw(a *App, c *gfx.Canvas, area gfx.Rect) {
 			mode += "  ·  " + m.label
 		}
 	}
-	mode += "  ·  " + volumeLabel(st.VolumeDB)
+	mode += "  ·  " + a.volumeText(st.VolumeDB)
 	iconText(c, fb, statusIcon(st.Status), text.X, y+fb.Ascent(), fb.Truncate(mode, text.W-fb.Ascent()), colText, c.Bounds())
 	y += fb.Height()
 	if st.NextIndex >= 0 && st.NextIndex < len(st.Queue) {
diff --git a/internal/ui/screens_settings.go b/internal/ui/screens_settings.go
index 0a1a58c..9420f87 100644
--- a/internal/ui/screens_settings.go
+++ b/internal/ui/screens_settings.go
@@ -137,6 +137,9 @@ func (a *App) volumeDB() float64 {
 // setVolume changes the volume now and saves it (after a pause, so a held
 // key is one write).
 func (a *App) setVolume(db float64) {
+	if a.muted {
+		a.setMuted(false) // changing the volume brings the sound back
+	}
 	db = math.Max(-60, math.Min(0, math.Round(db)))
 	if pl := a.Player(); pl != nil {
 		pl.SetVolumeDB(db)
@@ -146,6 +149,32 @@ func (a *App) setVolume(db float64) {
 	a.UpdateConfig(func(c *config.Config) { c.Playback.VolumeDB = db }, true)
 }
 
+// setMuted turns the sound off or back on (spec §6). It isn't saved, so the
+// app always starts with the sound on.
+func (a *App) setMuted(on bool) {
+	a.muted, a.dirty = on, true
+	if pl := a.Player(); pl != nil {
+		pl.SetMuted(on)
+	}
+}
+
+func (a *App) toggleMute() {
+	a.setMuted(!a.muted)
+	if a.muted {
+		a.Toast("Muted")
+	} else {
+		a.Toast("Sound on")
+	}
+}
+
+// volumeText is the volume as shown: "Muted" while the sound is off.
+func (a *App) volumeText(db float64) string {
+	if a.muted {
+		return "Muted"
+	}
+	return volumeLabel(db)
+}
+
 func playbackSettings(a *App) []setting {
 	pb := func() config.Playback {
 		if a.cfg == nil {
@@ -154,8 +183,10 @@ func playbackSettings(a *App) []setting {
 		return a.cfg.Playback
 	}
 	return []setting{
-		{"Volume", func(a *App) string { return volumeLabel(a.volumeDB()) },
+		{"Volume", func(a *App) string { return a.volumeText(a.volumeDB()) },
 			func(a *App, dir int) { a.setVolume(a.volumeDB() + float64(dir)) }},
+		{"Mute", func(a *App) string { return onOff(a.muted) },
+			func(a *App, dir int) { a.toggleMute() }},
 		{"ReplayGain", func(a *App) string { return pb().ReplayGain },
 			func(a *App, dir int) {
 				mode := cycle([]string{"off", "track", "album"}, pb().ReplayGain, dir)
diff --git a/internal/ui/session.go b/internal/ui/session.go
index f4bf3b1..3b874f0 100644
--- a/internal/ui/session.go
+++ b/internal/ui/session.go
@@ -82,6 +82,9 @@ func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
 		pl.SetVolumeDB(a.cfg.Playback.VolumeDB) // changed while disconnected
 	}
 	a.volumePending = false
+	if a.muted && pl != nil {
+		pl.SetMuted(true) // a new server's player starts muted too
+	}
 	a.conn = info
 	a.SetInsecure(info.Server.InsecureSkipVerify)
 	a.artists, a.stars, a.starBusy, a.starGen = nil, map[starKey]bool{}, map[starKey]bool{}, 0
```

- [ ] **Step 4: Run the tests**

Run: `go test -count=1 ./internal/ui -update && go vet ./... && go test -race -count=1 ./internal/ui ./internal/input ./internal/devview ./internal/player ./cmd/mistersubsonic`

Expected: `ok`. Golden screenshots written or changed: `settings-playback-crt`, `settings-playback-hdmi`. Open each one and check it: `settings-playback-*`: Volume (‹ Vol 0 dB › focused), then **Mute Off**, ReplayGain off, Scrobbling On, Transcode to mp3, Transcode bitrate 320 kbps.

- [ ] **Step 5: Commit**

```bash
git add README.md cmd/mistersubsonic/keys.go internal/devview/devview.go internal/devview/devview_test.go internal/devview/page.html internal/input/evmap.go internal/input/input.go internal/input/input_test.go internal/player/player.go internal/player/player_test.go internal/ui/app.go internal/ui/fakes_test.go internal/ui/mute_test.go internal/ui/screens_play.go internal/ui/screens_settings.go internal/ui/session.go internal/ui/settings_test.go internal/ui/testdata/golden
git commit -m "ui: mute (M, Settings → Playback); a volume change unmutes" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 4: The console in graphics mode, and a safe shutdown

**Files:**
- Create: `internal/platform/console_linux.go`, `internal/platform/console_other.go`
- Modify: `cmd/mistersubsonic/main.go`
- Test: `internal/platform/console_linux_test.go` (new); `cmd/mistersubsonic/main_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `platform.GraphicsMode() (*platform.Console, error)` tries `/dev/tty0`, then `/dev/tty`. It saves the mode it finds (`KDGETMODE`) and sets `KD_GRAPHICS`.
  - `(*Console).Restore() error` restores the saved mode. It is idempotent and nil-safe.
  - `platform.RestoreText() error` sets `KD_TEXT` whatever state was left.
  - The ioctls are the replaceable vars `getMode` and `setMode`. On non-Linux builds the functions are stubs.
  - `main`:
    - `-restore-console` runs `RestoreText` and exits; the launcher calls it after every run.
    - With `-display fbdev`, the console is in graphics mode while the app runs and is restored after the framebuffer is blanked.
    - `run` has a named `err`. Its first deferred function recovers a panic on the UI goroutine after every other clean-up has run, logs it with its stack, and logs any error `run` returns.
    - `shutdownLimit` (10 s), `forceExit` (restore the console, exit 3) and `armDeadline(&t, d)` bound the clean-up once the UI has quit.
    - `beforeRun(app)` is a test hook.

- [ ] **Step 1: Write the failing tests**

`internal/platform/console_linux_test.go` (new file):

```go
//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// call is one console ioctl: KDGETMODE (mode unused) or KDSETMODE.
type call struct {
	req  uintptr
	mode uint32
}

// fakeConsoles points the console list at plain files and records the
// ioctls; found is what KDGETMODE reports, fail makes every ioctl fail.
func fakeConsoles(t *testing.T, n int, found uint32, fail bool) (paths []string, calls *[]call) {
	t.Helper()
	dir := t.TempDir()
	for i := range n {
		p := filepath.Join(dir, "tty"+string(rune('0'+i)))
		os.WriteFile(p, nil, 0o600)
		paths = append(paths, p)
	}
	calls = &[]call{}
	oldC, oldG, oldS := consoles, getMode, setMode
	consoles = paths
	getMode = func(uintptr) (uint32, error) {
		if fail {
			return 0, syscall.ENOTTY
		}
		*calls = append(*calls, call{kdGetMode, 0})
		return found, nil
	}
	setMode = func(_ uintptr, mode uint32) error {
		if fail {
			return syscall.ENOTTY
		}
		*calls = append(*calls, call{kdSetMode, mode})
		return nil
	}
	t.Cleanup(func() { consoles, getMode, setMode = oldC, oldG, oldS })
	return paths, calls
}

func TestGraphicsModeAndRestore(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdText, false)
	c, err := GraphicsMode()
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[1] != (call{kdSetMode, kdGraphics}) {
		t.Fatalf("calls %v", *calls)
	}
	if err := c.Restore(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 || (*calls)[2] != (call{kdSetMode, kdText}) {
		t.Fatalf("restore calls %v", *calls)
	}
	c.Restore() // again: nothing more
	var nilc *Console
	nilc.Restore()
	if len(*calls) != 3 {
		t.Fatalf("a second Restore issued %v", (*calls)[3:])
	}
}

func TestRestoreGoesBackToTheModeItFound(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdGraphics, false) // someone else's graphics mode
	c, _ := GraphicsMode()
	c.Restore()
	if last := (*calls)[len(*calls)-1]; last != (call{kdSetMode, kdGraphics}) {
		t.Fatalf("restored to %v, want the mode found (graphics)", last)
	}
}

func TestGraphicsModeTriesTheNextConsole(t *testing.T) {
	paths, calls := fakeConsoles(t, 2, kdText, false)
	os.Remove(paths[0]) // no tty0 here
	c, err := GraphicsMode()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Restore()
	if c.f.Name() != paths[1] || len(*calls) != 2 {
		t.Fatalf("opened %s, calls %v", c.f.Name(), *calls)
	}
}

func TestGraphicsModeFailsWithoutAVirtualTerminal(t *testing.T) {
	fakeConsoles(t, 2, kdText, true)
	if c, err := GraphicsMode(); err == nil || c != nil {
		t.Fatalf("got %v, %v; want an error", c, err)
	}
	if err := RestoreText(); err == nil {
		t.Fatal("RestoreText succeeded with no virtual terminal")
	}
}

func TestRestoreTextSetsTextMode(t *testing.T) {
	_, calls := fakeConsoles(t, 1, kdGraphics, false)
	if err := RestoreText(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != (call{kdSetMode, kdText}) {
		t.Fatalf("calls %v", *calls)
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t4-test.patch` and apply it from the repository root with `git apply /tmp/t4-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main_test.go b/cmd/mistersubsonic/main_test.go
index e3d26f6..dc752ff 100644
--- a/cmd/mistersubsonic/main_test.go
+++ b/cmd/mistersubsonic/main_test.go
@@ -14,6 +14,7 @@ import (
 	"time"
 
 	"mistersubsonic/internal/audio"
+	"mistersubsonic/internal/ui"
 )
 
 func TestNoAudioDeviceShowsMessage(t *testing.T) {
@@ -172,3 +173,56 @@ func TestAnUnwritableLogFallsBackToStderr(t *testing.T) {
 		t.Fatal("the log went somewhere other than stderr")
 	}
 }
+
+// A panic on the UI goroutine is recovered after the clean-ups ran (the
+// console, input and framebuffer restored), logged with its stack and
+// returned.
+func TestAPanicInTheUIIsLoggedAndReturned(t *testing.T) {
+	nullDevice(t)
+	dir, cfg := writeConfig(t, "http://127.0.0.1:1")
+	old := beforeRun
+	beforeRun = func(*ui.App) { panic("boom") }
+	defer func() { beforeRun = old }()
+	logPath := filepath.Join(dir, "app.log")
+	err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: time.Second, log: logPath})
+	if err == nil || err.Error() != "panic: boom" {
+		t.Fatalf("run returned %v", err)
+	}
+	b, _ := os.ReadFile(logPath)
+	if !strings.Contains(string(b), "panic: boom") || !strings.Contains(string(b), "main_test.go") ||
+		!strings.Contains(string(b), "error: panic: boom") {
+		t.Fatalf("the log has no panic stack or final error:\n%s", b)
+	}
+}
+
+func TestShutdownDeadlineFires(t *testing.T) {
+	fired := make(chan struct{})
+	old := forceExit
+	forceExit = func() { close(fired) }
+	defer func() { forceExit = old }()
+	var d *time.Timer
+	armDeadline(&d, 10*time.Millisecond)
+	armDeadline(&d, time.Hour) // a second arm keeps the first deadline
+	select {
+	case <-fired:
+	case <-time.After(time.Second):
+		t.Fatal("the deadline didn't fire")
+	}
+}
+
+func TestShutdownDeadlineIsStoppedAfterACleanExit(t *testing.T) {
+	nullDevice(t)
+	_, cfg := writeConfig(t, "http://127.0.0.1:1")
+	fired := make(chan struct{}, 1)
+	oldL, oldF := shutdownLimit, forceExit
+	shutdownLimit, forceExit = 300*time.Millisecond, func() { fired <- struct{}{} }
+	defer func() { shutdownLimit, forceExit = oldL, oldF }()
+	if err := run(flags{config: cfg, display: "headless", null: true, volume: math.NaN(), exitAfter: 200 * time.Millisecond}); err != nil {
+		t.Fatal(err)
+	}
+	select {
+	case <-fired:
+		t.Fatal("the deadline fired after a clean exit")
+	case <-time.After(400 * time.Millisecond):
+	}
+}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/platform ./cmd/mistersubsonic`

Expected: FAIL, e.g.:

```
undefined: consoles
undefined: getMode
undefined: setMode
undefined: kdGetMode
undefined: kdSetMode
undefined: beforeRun
undefined: forceExit
undefined: armDeadline
```

- [ ] **Step 3: Implement**

`internal/platform/console_linux.go` (new file):

```go
//go:build linux

// Package platform holds the MiSTer specifics the app handles itself: while
// it draws on the framebuffer, the Linux console is put in graphics mode so
// its text and cursor stay off the screen, and put back on exit (spec §9).
// Pausing BGM and SAM and the single-instance lock are the launcher's.
package platform

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// linux/kd.h
const (
	kdSetMode  = 0x4B3A
	kdGetMode  = 0x4B3B
	kdText     = 0
	kdGraphics = 1
)

// Consoles are tried in order: the active virtual terminal (the Scripts
// menu runs scripts on tty2 and switches to it), then the app's own
// terminal.
var consoles = []string{"/dev/tty0", "/dev/tty"}

// getMode and setMode are the console ioctls; tests replace them.
var (
	getMode = func(fd uintptr) (uint32, error) {
		var mode uint32
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, kdGetMode, uintptr(unsafe.Pointer(&mode))); e != 0 {
			return 0, e
		}
		return mode, nil
	}
	setMode = func(fd uintptr, mode uint32) error {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, kdSetMode, uintptr(mode)); e != 0 {
			return e
		}
		return nil
	}
)

// Console is a console switched to graphics mode.
type Console struct {
	mu   sync.Mutex
	f    *os.File
	prev uint32 // the mode it was in
}

// GraphicsMode switches the console to KD_GRAPHICS. It fails where there
// is no virtual terminal (over ssh, in a desktop terminal).
func GraphicsMode() (*Console, error) {
	var errs []error
	for _, path := range consoles {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		mode, err := getMode(f.Fd())
		if err != nil {
			f.Close()
			errs = append(errs, fmt.Errorf("%s: KDGETMODE: %w", path, err))
			continue
		}
		if err := setMode(f.Fd(), kdGraphics); err != nil {
			f.Close()
			errs = append(errs, fmt.Errorf("%s: KDSETMODE: %w", path, err))
			continue
		}
		return &Console{f: f, prev: mode}, nil
	}
	return nil, fmt.Errorf("platform: no console for graphics mode: %w", errors.Join(errs...))
}

// Restore puts the console back in the mode it had. It is safe to call
// more than once and on a nil Console.
func (c *Console) Restore() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return nil
	}
	err := setMode(c.f.Fd(), c.prev)
	c.f.Close()
	c.f = nil
	return err
}

// RestoreText puts the console in text mode whatever a crashed run left
// behind. The launcher runs it (mistersubsonic -restore-console) after
// every exit.
func RestoreText() error {
	var errs []error
	for _, path := range consoles {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = setMode(f.Fd(), kdText)
		f.Close()
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: KDSETMODE: %w", path, err))
	}
	return fmt.Errorf("platform: console not restored: %w", errors.Join(errs...))
}
```

`internal/platform/console_other.go` (new file):

```go
//go:build !linux

package platform

import "errors"

// Console is a console switched to graphics mode (Linux only).
type Console struct{}

// GraphicsMode needs a Linux virtual terminal.
func GraphicsMode() (*Console, error) { return nil, errors.New("platform: no Linux console") }

// Restore does nothing here.
func (c *Console) Restore() error { return nil }

// RestoreText needs a Linux virtual terminal.
func RestoreText() error { return errors.New("platform: no Linux console") }
```

Then Save this patch as `/tmp/t4-code.patch` and apply it from the repository root with `git apply /tmp/t4-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 3):

```diff
diff --git a/cmd/mistersubsonic/main.go b/cmd/mistersubsonic/main.go
index 6c68139..ec8bc8d 100644
--- a/cmd/mistersubsonic/main.go
+++ b/cmd/mistersubsonic/main.go
@@ -25,6 +25,7 @@ import (
 	"mistersubsonic/internal/gfx"
 	"mistersubsonic/internal/input"
 	"mistersubsonic/internal/logfile"
+	"mistersubsonic/internal/platform"
 	"mistersubsonic/internal/ui"
 )
 
@@ -36,7 +37,7 @@ var version = "dev"
 
 type flags struct {
 	config, display, viewerAddr, frames, profile, fbdev, keys, log string
-	null                                                           bool
+	null, restoreConsole                                           bool
 	volume                                                         float64
 	exitAfter                                                      time.Duration
 }
@@ -54,7 +55,15 @@ func main() {
 	flag.StringVar(&f.log, "log", "auto", "log file: auto (log.txt next to the config on the framebuffer, stderr elsewhere), - (stderr) or a path")
 	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
 	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
+	flag.BoolVar(&f.restoreConsole, "restore-console", false, "put the console back in text mode and exit (the launcher runs this after the app)")
 	flag.Parse()
+	if f.restoreConsole {
+		if err := platform.RestoreText(); err != nil {
+			fmt.Fprintln(os.Stderr, "mistersubsonic:", err)
+			os.Exit(1)
+		}
+		return
+	}
 	if err := run(f); err != nil {
 		fmt.Fprintln(os.Stderr, "mistersubsonic:", err)
 		os.Exit(1)
@@ -122,7 +131,29 @@ func openLog(flagPath, display, dataDir string) func() {
 	}
 }
 
-func run(f flags) error {
+// shutdownLimit bounds the clean-up after the UI quits (saving the queue,
+// closing audio): past it the app restores the console and exits anyway, so
+// a stuck close never leaves the TV on a frozen screen.
+var shutdownLimit = 10 * time.Second
+
+// forceExit ends a shutdown that took too long; tests replace it.
+var forceExit = func() {
+	log.Printf("shutdown took longer than %v; exiting", shutdownLimit)
+	platform.RestoreText()
+	os.Exit(3)
+}
+
+// armDeadline starts the shutdown deadline (once).
+func armDeadline(t **time.Timer, d time.Duration) {
+	if *t == nil {
+		*t = time.AfterFunc(d, forceExit)
+	}
+}
+
+// beforeRun runs just before the UI loop; tests use it.
+var beforeRun = func(*ui.App) {}
+
+func run(f flags) (err error) {
 	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
 	defer stop()
 	if f.exitAfter > 0 {
@@ -141,6 +172,22 @@ func run(f flags) error {
 	dataDir := filepath.Dir(f.config)
 	closeLog := openLog(f.log, f.display, dataDir)
 	defer closeLog()
+	var deadline *time.Timer
+	defer func() {
+		// Runs after every other clean-up below, a panic's included: a
+		// panic on the UI goroutine still restores the console, input and
+		// framebuffer, and is logged with its stack.
+		if deadline != nil {
+			deadline.Stop()
+		}
+		if r := recover(); r != nil {
+			log.Printf("panic: %v\n%s", r, debug.Stack())
+			err = fmt.Errorf("panic: %v", r)
+		}
+		if err != nil {
+			log.Printf("error: %v", err) // stderr isn't seen on the MiSTer
+		}
+	}()
 	log.Printf("MiSTer Subsonic %s starting (%s, config %s)", version, f.display, f.config)
 	defer log.Printf("MiSTer Subsonic exiting")
 
@@ -165,6 +212,11 @@ func run(f flags) error {
 		if err != nil {
 			return err
 		}
+		con, err := platform.GraphicsMode()
+		if err != nil {
+			log.Printf("console: %v (its text may show over the app)", err)
+		}
+		defer con.Restore() // after the framebuffer is blanked (defers run last-in first-out)
 		disp = fb
 		mgr := input.NewManager(input.ManagerOptions{Grab: true})
 		defer mgr.Close()
@@ -222,6 +274,7 @@ func run(f flags) error {
 	// engine and device close (defers run last-in first-out).
 	sess := newSessions(ctx, eng, dataDir, vol)
 	defer sess.close()
+	defer armDeadline(&deadline, shutdownLimit) // runs first: bounds the clean-ups above
 
 	var loaded *config.Config
 	if cfgErr == nil {
@@ -236,6 +289,7 @@ func run(f flags) error {
 	if err != nil {
 		return err
 	}
+	beforeRun(app)
 	err = app.Run(ctx)
 	stop() // a second Ctrl-C now kills the process instead of waiting out the shutdown below
 	return err
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/platform ./cmd/mistersubsonic`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then check the other platforms still build: `GOOS=darwin GOARCH=arm64 go vet ./internal/platform`. Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add cmd/mistersubsonic/main.go cmd/mistersubsonic/main_test.go internal/platform/console_linux.go internal/platform/console_linux_test.go internal/platform/console_other.go
git commit -m "app: console in graphics mode while drawing; restore on exit, panic, or -restore-console; bounded shutdown" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 5: The example config

**Files:**
- Create: `sdcard/mistersubsonic/config.example.toml`
- Modify: `internal/config/config.go` (the header `Save` writes)
- Test: `internal/config/example_test.go` (new); `internal/config/servers_test.go` (modified)

**Interfaces:**
- **Produces:**
  - `sdcard/` is the SD card tree the release ships; Tasks 6 and 7 add to it. `config.example.toml` loads with no error or warning, shows the defaults, and mentions every option (commented out where it's optional).
  - The saved config's header points at `config.example.toml`.

- [ ] **Step 1: Write the failing tests**

`internal/config/example_test.go` (new file):

```go
package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

const examplePath = "../../sdcard/mistersubsonic/config.example.toml"

// The shipped example loads cleanly and shows the defaults.
func TestExampleConfigLoads(t *testing.T) {
	c, warns, err := Load(examplePath)
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v, warnings %v", err, warns)
	}
	d := Default()
	if c.Playback != d.Playback || c.Display != d.Display || c.Cache != d.Cache {
		t.Fatalf("the example's settings differ from the defaults:\n%+v %+v %+v\n%+v %+v %+v",
			c.Playback, c.Display, c.Cache, d.Playback, d.Display, d.Cache)
	}
	if s, ok := c.ActiveServer(); !ok || s.Name != "home" {
		t.Fatalf("server %+v", s)
	}
}

// Every option is in the example, at least as a comment.
func TestExampleConfigShowsEveryOption(t *testing.T) {
	b, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, typ := range []reflect.Type{reflect.TypeOf(Config{}), reflect.TypeOf(Server{}), reflect.TypeOf(Playback{}),
		reflect.TypeOf(Display{}), reflect.TypeOf(Cache{})} {
		for i := range typ.NumField() {
			key := strings.Split(typ.Field(i).Tag.Get("toml"), ",")[0]
			if typ.Field(i).Type.Kind() == reflect.Struct || typ.Field(i).Type.Kind() == reflect.Slice {
				continue // tables: [playback], [[server]]
			}
			if !strings.Contains(text, key+" = ") {
				t.Errorf("%s isn't in the example", key)
			}
		}
	}
}
```

Then update the existing tests. Save this patch as `/tmp/t5-test.patch` and apply it from the repository root with `git apply /tmp/t5-test.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/config/servers_test.go b/internal/config/servers_test.go
index 20f0c71..eeb7c00 100644
--- a/internal/config/servers_test.go
+++ b/internal/config/servers_test.go
@@ -52,7 +52,7 @@ func TestRemoveServerMovesTheDefault(t *testing.T) {
 	}
 }
 
-func TestSaveHeaderPointsAtTheREADME(t *testing.T) {
+func TestSaveHeaderPointsAtTheExample(t *testing.T) {
 	p := filepath.Join(t.TempDir(), "config.toml")
 	c := Default()
 	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
@@ -60,7 +60,7 @@ func TestSaveHeaderPointsAtTheREADME(t *testing.T) {
 		t.Fatal(err)
 	}
 	b, _ := os.ReadFile(p)
-	if !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") || strings.Contains(string(b), "config.example.toml") {
+	if !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") || !strings.Contains(string(b), "config.example.toml") {
 		t.Fatalf("header: %q", strings.SplitN(string(b), "\n", 2)[0])
 	}
 }
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./internal/config`

Expected: FAIL, e.g.:

```
--- FAIL: TestExampleConfigLoads (0.00s)
example_test.go:16: err config: file not found, warnings []
--- FAIL: TestExampleConfigShowsEveryOption (0.00s)
example_test.go:32: open ../../sdcard/mistersubsonic/config.example.toml: no such file or directory
--- FAIL: TestSaveHeaderPointsAtTheExample (0.00s)
servers_test.go:64: header: "# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings)."
```

- [ ] **Step 3: Implement**

`sdcard/mistersubsonic/config.example.toml` (new file):

```toml
# MiSTer Subsonic: example configuration.
#
# You don't need this file: on first start the setup wizard writes
# config.toml, and Settings saves your changes there. To write one by hand,
# copy this file to config.toml in the same folder and edit it. Lines
# starting with # are comments.

# The server to connect to at start: one of the names below.
default_server = "home"

# One [[server]] block per server. Settings → Servers switches between them.
[[server]]
name = "home"
url = "http://192.168.1.10:4533"   # or https://music.example.com
username = "alice"
password = "change-me"             # the wizard saves a token and salt instead
# token = "…"                      # md5(password + salt); written by the wizard
# salt = "…"
# api_key = "…"                    # an OpenSubsonic API key, for servers that give them out
# ca_file = "/media/fat/mistersubsonic/myca.pem"   # trust a self-signed certificate
# insecure_skip_verify = false     # true skips the certificate check (not recommended)
# allow_plaintext_password = false # true sends the password itself (servers using LDAP)

[playback]
transcode_format = "mp3"   # for formats the MiSTer can't decode: mp3, flac or wav
transcode_bitrate = 320    # kbps
replaygain = "off"         # off, track or album
volume_db = 0              # -60 to 0; saved when you change the volume
scrobble = true            # report plays to the server (and on to Last.fm/ListenBrainz)
buffer_mb = 32             # memory for the playing stream, 4 to 512
alsa_device = "default"

[display]
profile = "auto"           # auto, hdmi or crt (set crt for a 480i/576i CRT)
screensaver_minutes = 5    # minutes idle on Now Playing; 0 turns it off

[cache]
cover_art_mb = 200         # cover art kept on the SD card, per server
```

Then Save this patch as `/tmp/t5-code.patch` and apply it from the repository root with `git apply /tmp/t5-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 4):

```diff
diff --git a/internal/config/config.go b/internal/config/config.go
index a86e924..8838360 100644
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -189,7 +189,7 @@ func Save(path string, cfg *Config) error {
 		return fmt.Errorf("config: refusing to save invalid config: %w", err)
 	}
 	var buf bytes.Buffer
-	buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in the README.\n")
+	buf.WriteString("# MiSTer Subsonic configuration, saved by the app (the setup wizard or Settings).\n# Every option is described in config.example.toml, next to this file.\n")
 	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
 		return fmt.Errorf("config: encode: %w", err)
 	}
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./internal/config`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/example_test.go internal/config/servers_test.go sdcard/mistersubsonic/config.example.toml
git commit -m "config: ship config.example.toml; the saved header points to it" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 6: The Scripts-menu launcher

**Files:**
- Create: `sdcard/Scripts/MiSTer_Subsonic.sh`
- Modify: `Makefile` (`launcher-test`, run by `make test`), `.github/workflows/ci.yml`
- Test: `scripts/test-launcher.sh` (new)

**Interfaces:**
- **Consumes:** `mistersubsonic -restore-console` (Task 4).
- **Produces:** `MiSTer_Subsonic.sh` (bash), which:
  - stops leftover `mistersubsonic` processes (TERM, then KILL after 5 s);
  - takes `flock` on `/tmp/mistersubsonic.lock` (fd 9, inherited by the app), waiting `MSS_LOCK_WAIT` seconds (default 5);
  - stops BGM through `/tmp/bgm.sock` unless its playback is `disabled`, and sends `play` afterwards;
  - disables SAM (`MiSTer_SAM_on.sh disable`) while `MiSTer_SAM_MCP` runs, and enables it afterwards;
  - hides the cursor and runs the app with the script's arguments;
  - afterwards, restores the console (`-restore-console`), the cursor and a cleared screen, prints a closing line (or the log's path after an error), and exits with the app's code.
  - The restore also runs if the launcher itself is interrupted.
  - The `MSS_*` variables are only for tests.

- [ ] **Step 1: Write the failing tests**

The test gives the launcher stand-ins for everything on a MiSTer. Each is a script on `PATH`: the app, `socat` (BGM's socket), `pidof`, `ps` and SAM. It needs `bash`, `flock` and `python3`, which are on Linux desktops and GitHub's runners.

`scripts/test-launcher.sh` (new file; make it executable: `chmod +x scripts/test-launcher.sh`):

```bash
#!/bin/bash
# Tests the Scripts-menu launcher with stand-ins for what it talks to on a
# MiSTer: the app, BGM's socket (socat), SAM, pidof and ps.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
launcher=$root/sdcard/Scripts/MiSTer_Subsonic.sh
tmp=$(mktemp -d)
bg=()
cleanup() {
	for p in "${bg[@]}"; do kill "$p" 2>/dev/null; done
	rm -rf "$tmp"
}
trap cleanup EXIT
failures=0

# sandbox makes a fresh case directory with the stand-ins on PATH.
sandbox() {
	c=$tmp/$1
	mkdir -p "$c/bin" "$c/app"
	export MSS_DIR=$c/app MSS_LOCK=$c/lock MSS_BGM_SOCK=$c/bgm.sock MSS_SAM=$c/sam.sh MSS_LOCK_WAIT=1
	export LOG=$c/log PIDS=$c/pids APP_EXIT=0 BGM_STATUS="" PS_OUT=""
	: >"$LOG"
	cat >"$MSS_DIR/mistersubsonic" <<'S'
#!/bin/bash
echo "app $*" >>"$LOG"
exit "$APP_EXIT"
S
	cat >"$c/bin/socat" <<'S'
#!/bin/bash
cmd=$(cat)
echo "bgm $cmd" >>"$LOG"
[ "$cmd" = status ] && printf '%s' "$BGM_STATUS"
exit 0
S
	cat >"$c/bin/pidof" <<'S'
#!/bin/bash
# The live pids listed in $PIDS.<name>.
f=$PIDS.$1
[ -s "$f" ] || exit 1
live=
for p in $(cat "$f"); do kill -0 "$p" 2>/dev/null && live="$live $p"; done
[ -n "$live" ] || exit 1
echo $live
S
	cat >"$c/bin/ps" <<'S'
#!/bin/bash
printf '%s\n' "  PID USER COMMAND" "$PS_OUT"
S
	cat >"$MSS_SAM" <<'S'
#!/bin/bash
echo "sam $1" >>"$LOG"
S
	chmod +x "$c/bin/"* "$MSS_DIR/mistersubsonic" "$MSS_SAM"
	PATH=$c/bin:$ORIG_PATH
}
ORIG_PATH=$PATH

socket() { python3 -c 'import socket,sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$MSS_BGM_SOCK"; }

# expect NAME CODE LINES...: the launcher's exit code and the log, in order.
run_case() {
	name=$1 want_code=$2
	shift 2
	"$launcher" -volume -20 >"$c/out" 2>&1
	code=$?
	want=$(printf '%s\n' "$@")
	got=$(cat "$LOG")
	if [ "$code" != "$want_code" ] || [ "$got" != "$want" ]; then
		echo "FAIL $name: exit $code (want $want_code)"
		echo "  log:"; sed 's/^/    /' "$LOG"
		echo "  want:"; printf '    %s\n' "$@"
		echo "  output:"; sed 's/^/    /' "$c/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

sandbox plain
run_case "runs the app, then restores the console" 0 "app -volume -20" "app -restore-console"
grep -q "MiSTer Subsonic closed." "$c/out" || { echo "FAIL plain: no closing message"; failures=$((failures + 1)); }

sandbox bgm
socket
BGM_STATUS=$'yes\trandom\tall\t/media/fat/music/x.mp3'
run_case "stops BGM and plays it again after" 0 "bgm status" "bgm stop" "app -volume -20" "app -restore-console" "bgm play"

sandbox bgm-disabled
socket
BGM_STATUS=$'no\tdisabled\tall\t'
run_case "leaves a disabled BGM alone" 0 "bgm status" "app -volume -20" "app -restore-console"

sandbox bgm-stale
touch "$MSS_BGM_SOCK" # a plain file, not a socket
run_case "ignores a BGM socket that isn't one" 0 "app -volume -20" "app -restore-console"

sandbox sam
echo $$ >"$PIDS.MiSTer_SAM_MCP"
run_case "disables SAM and enables it after" 0 "sam disable" "app -volume -20" "app -restore-console" "sam enable"

sandbox sam-old
PS_OUT="  812 root python /media/fat/Scripts/.MiSTer_SAM/MiSTer_SAM_MCP.py"
run_case "finds an older SAM by its process" 0 "sam disable" "app -volume -20" "app -restore-console" "sam enable"

sandbox leftover
sleep 60 &
left=$!
bg+=("$left")
echo "$left" >"$PIDS.mistersubsonic"
run_case "stops a leftover app first" 0 "app -volume -20" "app -restore-console"
if kill -0 "$left" 2>/dev/null; then echo "FAIL leftover: still running"; failures=$((failures + 1)); fi

sandbox crash
socket
BGM_STATUS=$'yes\trandom\tall\tx'
APP_EXIT=2
run_case "restores everything after a crash" 2 "bgm status" "bgm stop" "app -volume -20" "app -restore-console" "bgm play"
grep -q "log.txt" "$c/out" || { echo "FAIL crash: no pointer to the log"; failures=$((failures + 1)); }

sandbox locked
flock "$MSS_LOCK" sleep 30 &
bg+=("$!")
sleep 0.2
run_case "won't start while another launcher holds the lock" 1

sandbox missing
rm "$MSS_DIR/mistersubsonic"
run_case "says when the app is missing" 1

if [ "$failures" -gt 0 ]; then
	echo "test-launcher: $failures failed"
	exit 1
fi
echo "test-launcher ok"
```

- [ ] **Step 2: Run them and watch them fail**

Run: `./scripts/test-launcher.sh`

Expected: FAIL, e.g.:

```
FAIL runs the app, then restores the console: exit 127 (want 0)
FAIL plain: no closing message
FAIL stops BGM and plays it again after: exit 127 (want 0)
FAIL leaves a disabled BGM alone: exit 127 (want 0)
FAIL ignores a BGM socket that isn't one: exit 127 (want 0)
FAIL disables SAM and enables it after: exit 127 (want 0)
FAIL finds an older SAM by its process: exit 127 (want 0)
FAIL stops a leftover app first: exit 127 (want 0)
```

- [ ] **Step 3: Implement**

`sdcard/Scripts/MiSTer_Subsonic.sh` (new file; make it executable: `chmod +x sdcard/Scripts/MiSTer_Subsonic.sh`):

```bash
#!/bin/bash
# MiSTer Subsonic: plays music from a Subsonic or Navidrome server.
#
# This launcher runs the app full screen and comes back here when you exit
# it (Settings → Exit, or hold B on the home screen). While the app runs,
# background music (BGM) is stopped and Super Attract Mode (SAM) is
# disabled; both come back afterwards. The app keeps its settings and log
# in /media/fat/mistersubsonic.
#
# The MSS_* variables are for testing.

DIR=${MSS_DIR:-/media/fat/mistersubsonic}
APP=$DIR/mistersubsonic
LOCK=${MSS_LOCK:-/tmp/mistersubsonic.lock}
LOCK_WAIT=${MSS_LOCK_WAIT:-5}
BGM_SOCK=${MSS_BGM_SOCK:-/tmp/bgm.sock}
SAM=${MSS_SAM:-/media/fat/Scripts/MiSTer_SAM_on.sh}

if [ ! -x "$APP" ]; then
	echo "MiSTer Subsonic isn't installed: $APP is missing."
	exit 1
fi

# An app left running (after a crash, or started by hand) would fight this
# one for the screen and the controllers: stop it first.
stop_leftovers() {
	local pids
	pids=$(pidof mistersubsonic) || return 0
	echo "Stopping a MiSTer Subsonic left running..."
	kill $pids 2>/dev/null
	for _ in 1 2 3 4 5; do
		pidof mistersubsonic >/dev/null || return 0
		sleep 1
	done
	pids=$(pidof mistersubsonic) && kill -9 $pids 2>/dev/null
	sleep 1
}
stop_leftovers

# One launcher at a time. The app inherits the lock and holds it while it runs.
exec 9>"$LOCK"
if ! flock -w "$LOCK_WAIT" 9; then
	echo "MiSTer Subsonic is already being started."
	exit 1
fi

# BGM takes commands on its socket. It can't pause: stop it, play it again after.
bgm() { printf '%s' "$1" | socat -t 2 - "UNIX-CONNECT:$BGM_SOCK" 2>/dev/null; }
bgm_stopped=
if [ -S "$BGM_SOCK" ]; then
	status=$(bgm status)
	if [ -n "$status" ] && [ "$(printf '%s' "$status" | cut -f2)" != disabled ]; then
		bgm stop
		bgm_stopped=1
	fi
fi

# SAM would start a game over the app once it thinks the MiSTer is idle.
sam_disabled=
if [ -x "$SAM" ] && { pidof MiSTer_SAM_MCP >/dev/null || ps | grep -q '[M]iSTer_SAM_MCP'; }; then
	"$SAM" disable >/dev/null 2>&1
	sam_disabled=1
fi

restore() {
	"$APP" -restore-console >/dev/null 2>&1 # text mode again, even after a crash
	printf '\033[?25h\033[2J\033[H'          # the cursor back, the screen cleared
	[ -n "$bgm_stopped" ] && bgm play
	[ -n "$sam_disabled" ] && "$SAM" enable >/dev/null 2>&1
	return 0
}
trap restore EXIT
trap 'exit 130' INT TERM

printf '\033[?25l' # hide the cursor
"$APP" "$@"
code=$?
trap - EXIT
restore
if [ "$code" -eq 0 ]; then
	echo "MiSTer Subsonic closed."
else
	echo "MiSTer Subsonic stopped with an error ($code). The log is in $DIR/log.txt."
fi
exit "$code"
```

Then Save this patch as `/tmp/t6-code.patch` and apply it from the repository root with `git apply /tmp/t6-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 5):

```diff
diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
index bd856fe..ea0318a 100644
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -13,6 +13,7 @@ jobs:
           go-version-file: go.mod
       - run: go vet ./...
       - run: go test -race ./...
+      - run: ./scripts/test-launcher.sh
       - run: make e2e
       - uses: mlugg/setup-zig@v2
         with:
diff --git a/Makefile b/Makefile
index 8dab075..b1c6eeb 100644
--- a/Makefile
+++ b/Makefile
@@ -7,14 +7,18 @@ ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
            CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"
 
 
-.PHONY: build test vet e2e viewer mister mister-test deploy-dev vendor-check clean
+.PHONY: build test launcher-test vet e2e viewer mister mister-test deploy-dev vendor-check clean
 
 build:
 	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...
 
-test:
+test: launcher-test
 	$(GO) test -race ./...
 
+# The Scripts-menu launcher, against stand-ins for BGM, SAM and the app.
+launcher-test:
+	./scripts/test-launcher.sh
+
 vet:
 	$(GO) vet ./...
 
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && ./scripts/test-launcher.sh`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Make sure the tests can fail: temporarily change `[ -n "$bgm_stopped" ] && bgm play` in the launcher to `:` and run `./scripts/test-launcher.sh`. Expected: `FAIL stops BGM and plays it again after …` and `test-launcher: … failed`. Then restore the line.

Then run `make test`. Expected: `test-launcher ok`, then `ok` for every Go package.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml Makefile scripts/test-launcher.sh sdcard/Scripts/MiSTer_Subsonic.sh
git commit -m "launcher: Scripts-menu entry (lock, leftovers, BGM and SAM paused, console restored)" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 7: Releases and the Downloader database

**Files:**
- Create: `tools/mkdb/main.go`, `.github/workflows/release.yml`
- Modify: `Makefile` (`release`, `db`, `deploy`, `VERSION`)
- Test: `tools/mkdb/main_test.go` (new)

**Interfaces:**
- **Consumes:** `sdcard/` (Tasks 5, 6); `var version` in `main` (set with `-ldflags -X main.version=…`).
- **Produces:**
  - `mkdb`:
    - `build(dir, id, baseURL string, ts int64) (*DB, error)`: MiSTer Downloader's `db.json` with `v: 1`, `db_id`, `timestamp`, `files` (MD5 `hash`, `size`, and `url` = `baseURL` + the file name) and `folders`.
    - It refuses two files with one download name, and an empty tree.
    - Flags: `-dir`, `-id` (default `mistersubsonic`), `-base-url`, `-o`, `-timestamp`.
  - `make release VERSION=v…` builds `bin/release/sdcard/`, containing:
    - `Scripts/MiSTer_Subsonic.sh`
    - `mistersubsonic/mistersubsonic` (ARM, with the version, glibc checked)
    - `config.example.toml`, `LICENSE`, `THIRD_PARTY.txt`
    It also zips that as `bin/release/MiSTer_Subsonic-$(VERSION).zip`.
  - `make db BASE_URL=…` writes `bin/release/db.json`.
  - `make deploy MISTER=host` installs the release over ssh.
  - The `release` workflow runs on `v*` tags:
    - it tests;
    - builds;
    - publishes the zip and the loose files as release assets;
    - force-pushes `db.json` to the `db` branch.

- [ ] **Step 1: Write the failing tests**

`tools/mkdb/main_test.go` (new file):

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildDescribesEveryFile(t *testing.T) {
	dir := tree(t, map[string]string{
		"Scripts/MiSTer_Subsonic.sh":    "#!/bin/bash\n",
		"mistersubsonic/mistersubsonic": "ELF",
	})
	db, err := build(dir, "mistersubsonic", "https://example.com/v1/", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(db)
	// The hashes are md5sum of the file bodies.
	want := `{"v":1,"db_id":"mistersubsonic","timestamp":1790000000,"files":{` +
		`"Scripts/MiSTer_Subsonic.sh":{"hash":"677da3bdd8fbd16d4b8917a9fe0f6f89","size":12,"url":"https://example.com/v1/MiSTer_Subsonic.sh"},` +
		`"mistersubsonic/mistersubsonic":{"hash":"b61b2d6c6fa62903b882aaa53452c111","size":3,"url":"https://example.com/v1/mistersubsonic"}},` +
		`"folders":{"Scripts":{},"mistersubsonic":{}}}`
	if string(b) != want {
		t.Fatalf("got\n%s\nwant\n%s", b, want)
	}
}

func TestBuildRefusesTwoFilesWithOneDownloadName(t *testing.T) {
	dir := tree(t, map[string]string{"a/LICENSE": "x", "b/LICENSE": "y"})
	if _, err := build(dir, "id", "u/", 1); err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Fatalf("err %v", err)
	}
}

func TestBuildNeedsFiles(t *testing.T) {
	if _, err := build(t.TempDir(), "id", "u/", 1); err == nil {
		t.Fatal("an empty tree made a database")
	}
	if _, err := build(filepath.Join(t.TempDir(), "missing"), "id", "u/", 1); err == nil {
		t.Fatal("a missing tree made a database")
	}
}

func TestBuildListsNestedFolders(t *testing.T) {
	dir := tree(t, map[string]string{"mistersubsonic/fonts/x.ttf": "f"})
	db, err := build(dir, "id", "u/", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.Folders["mistersubsonic"]; !ok || len(db.Folders) != 2 {
		t.Fatalf("folders %v", db.Folders)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test -count=1 ./tools/mkdb`

Expected: FAIL, e.g.:

```
undefined: build
```

- [ ] **Step 3: Implement**

`.github/workflows/release.yml` (new file):

```yaml
name: release
# A v* tag builds the release: the zip and its files as GitHub release
# assets, and the MiSTer Downloader database on the db branch (spec §9).
on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    env:
      TAG: ${{ github.ref_name }}
      GH_TOKEN: ${{ github.token }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: mlugg/setup-zig@v2
        with:
          version: 0.16.0
      - run: go vet ./...
      - run: go test -race ./...
      - run: ./scripts/test-launcher.sh
      - run: make release VERSION="$TAG"
      - name: Publish the GitHub release
        run: |
          r=bin/release/sdcard
          gh release create "$TAG" --title "MiSTer Subsonic $TAG" --generate-notes \
            "bin/release/MiSTer_Subsonic-$TAG.zip" \
            "$r/Scripts/MiSTer_Subsonic.sh" \
            "$r/mistersubsonic/mistersubsonic" \
            "$r/mistersubsonic/config.example.toml" \
            "$r/mistersubsonic/LICENSE" \
            "$r/mistersubsonic/THIRD_PARTY.txt"
      - name: Publish the Downloader database on the db branch
        run: |
          make db BASE_URL="https://github.com/${{ github.repository }}/releases/download/$TAG/"
          cp bin/release/db.json "$RUNNER_TEMP/db.json"
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git checkout --orphan db-publish
          git rm -rfq .
          cp "$RUNNER_TEMP/db.json" db.json
          git add db.json
          git commit -qm "db: $TAG"
          git push -f origin HEAD:db
```

`tools/mkdb/main.go` (new file):

```go
// Command mkdb writes the MiSTer Downloader database for a release: every
// file under -dir (the SD card tree: Scripts/, mistersubsonic/) with its
// MD5 and size, downloaded from -base-url plus the file's name (the
// release's assets). Users add the database to downloader.ini, and
// Downloader or update_all then install and update the app.
//
//	go run ./tools/mkdb -dir bin/release/sdcard -base-url https://github.com/OWNER/REPO/releases/download/v1.0.0/ -o db.json
package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// DB is Downloader's database format (docs/custom-databases.md in
// MiSTer-devel/Downloader_MiSTer).
type DB struct {
	V         int                 `json:"v"`
	ID        string              `json:"db_id"`
	Timestamp int64               `json:"timestamp"`
	Files     map[string]File     `json:"files"`
	Folders   map[string]struct{} `json:"folders"`
}

// File is one file of the database.
type File struct {
	Hash string `json:"hash"` // MD5, hex
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// build walks dir and describes every file in it. Each file is downloaded
// from baseURL + its name, so names must be unique across folders.
func build(dir, id, baseURL string, ts int64) (*DB, error) {
	db := &DB{V: 1, ID: id, Timestamp: ts, Files: map[string]File{}, Folders: map[string]struct{}{}}
	names := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := path.Base(rel)
		if other, dup := names[name]; dup {
			return fmt.Errorf("%s and %s share the download name %q", other, rel, name)
		}
		names[name] = rel
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := md5.Sum(b)
		db.Files[rel] = File{Hash: hex.EncodeToString(sum[:]), Size: int64(len(b)), URL: baseURL + name}
		for f := path.Dir(rel); f != "."; f = path.Dir(f) {
			db.Folders[f] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(db.Files) == 0 {
		return nil, errors.New("no files in " + dir)
	}
	return db, nil
}

func main() {
	dir := flag.String("dir", "bin/release/sdcard", "the SD card tree to describe")
	id := flag.String("id", "mistersubsonic", "db_id: the section name users put in downloader.ini")
	baseURL := flag.String("base-url", "", "where the files are downloaded from (ends in /)")
	out := flag.String("o", "db.json", "output file")
	ts := flag.Int64("timestamp", 0, "the database timestamp (default: now)")
	flag.Parse()
	if *baseURL == "" {
		fmt.Fprintln(os.Stderr, "mkdb: -base-url is required")
		os.Exit(2)
	}
	if *ts == 0 {
		*ts = time.Now().Unix()
	}
	db, err := build(*dir, *id, *baseURL, *ts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdb:", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(db, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "mkdb:", err)
		os.Exit(1)
	}
}
```

Then Save this patch as `/tmp/t7-code.patch` and apply it from the repository root with `git apply /tmp/t7-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 6):

```diff
diff --git a/Makefile b/Makefile
index b1c6eeb..41bd9d8 100644
--- a/Makefile
+++ b/Makefile
@@ -3,11 +3,13 @@ ZIG     ?= zig
 BIN     := bin
 MISTER  ?= mister.local
 DEVDIR  := /media/fat/mistersubsonic/dev
+REL     := $(BIN)/release
+VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
 ARM_ENV := GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 \
            CC="$(ZIG) cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9"
 
 
-.PHONY: build test launcher-test vet e2e viewer mister mister-test deploy-dev vendor-check clean
+.PHONY: build test launcher-test vet e2e viewer mister mister-test release db deploy deploy-dev vendor-check clean
 
 build:
 	$(GO) build -o $(BIN)/ ./cmd/... ./tools/...
@@ -38,6 +40,35 @@ mister:
 	./scripts/check-glibc.sh $(BIN)/arm/mss-cli
 	./scripts/check-glibc.sh $(BIN)/arm/mistersubsonic
 
+# The release: the SD card tree (Scripts/ and mistersubsonic/) in bin/release/sdcard,
+# zipped as bin/release/MiSTer_Subsonic-$(VERSION).zip.
+release:
+	rm -rf $(REL)
+	mkdir -p $(REL)/sdcard/Scripts $(REL)/sdcard/mistersubsonic
+	$(ARM_ENV) $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" \
+		-o $(REL)/sdcard/mistersubsonic/mistersubsonic ./cmd/mistersubsonic
+	./scripts/check-glibc.sh $(REL)/sdcard/mistersubsonic/mistersubsonic
+	cp sdcard/Scripts/MiSTer_Subsonic.sh $(REL)/sdcard/Scripts/
+	cp sdcard/mistersubsonic/config.example.toml LICENSE $(REL)/sdcard/mistersubsonic/
+	{ echo "MiSTer Subsonic includes this third-party software."; \
+	  echo; echo "== speexdsp resampler (BSD) =="; cat third_party/speexdsp/COPYING; \
+	  echo; echo "== Noto Sans fonts (SIL Open Font License 1.1) =="; cat internal/gfx/fonts/OFL.txt; \
+	  echo; echo "== miniaudio (public domain or MIT No Attribution): https://miniaud.io =="; \
+	  echo; echo "== Mozilla CA certificate bundle (MPL-2.0): https://curl.se/docs/caextract.html =="; \
+	} > $(REL)/sdcard/mistersubsonic/THIRD_PARTY.txt
+	cd $(REL)/sdcard && zip -qrX ../MiSTer_Subsonic-$(VERSION).zip Scripts mistersubsonic
+
+# The MiSTer Downloader database for a release whose files are downloaded from
+# BASE_URL (the GitHub release's assets): bin/release/db.json.
+db:
+	$(GO) run ./tools/mkdb -dir $(REL)/sdcard -base-url $(BASE_URL) -o $(REL)/db.json
+
+# Installs the release on the MiSTer over ssh (default root password "1").
+deploy: release
+	ssh root@$(MISTER) mkdir -p /media/fat/mistersubsonic
+	scp $(REL)/sdcard/Scripts/MiSTer_Subsonic.sh root@$(MISTER):/media/fat/Scripts/
+	scp $(REL)/sdcard/mistersubsonic/* root@$(MISTER):/media/fat/mistersubsonic/
+
 # Audio test binary for on-device checks and benchmarks.
 mister-test:
 	$(ARM_ENV) $(GO) test -c -o $(BIN)/arm/audio.test ./internal/audio
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./... && go test -race -count=1 ./tools/mkdb`

Expected: every check passes (`ok`); `gofmt -l internal cmd tools` prints nothing. No golden screenshot changes (`git status internal/ui/testdata` is clean).

Then build a release locally (the ARM toolchain as in `make mister`):

```bash
PATH=<zig 0.16.0 dir>:$PATH make release VERSION=v0.0.0-test
unzip -l bin/release/MiSTer_Subsonic-v0.0.0-test.zip
make db BASE_URL=https://example.com/rel/ && head -20 bin/release/db.json
strings bin/release/sdcard/mistersubsonic/mistersubsonic | grep -c v0.0.0-test
```

Expected:
- `needs glibc 2.29 (<= 2.31) ok`.
- The zip lists `Scripts/MiSTer_Subsonic.sh`, `mistersubsonic/mistersubsonic`, `mistersubsonic/config.example.toml`, `mistersubsonic/LICENSE` and `mistersubsonic/THIRD_PARTY.txt`, 7 entries with the two folders. Unzipped, the launcher is executable.
- `db.json` lists the same five files with `https://example.com/rel/<name>` URLs.
- The count is `1` or more.

Don't push, tag or create a release: the workflow runs when the user pushes a tag.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml Makefile tools/mkdb/main.go tools/mkdb/main_test.go
git commit -m "release: make release/db/deploy, tools/mkdb (Downloader database), tag workflow" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

### Task 8: Docs, and the on-device check (when the MiSTer is available)

**Files:**
- Create: `docs/testing-on-mister.md`
- Modify: `README.md`, `docs/spikes.md`, `docs/superpowers/plan-2a-followups.md`

**Interfaces:**
- **Consumes:** everything above. The docs describe:
  - the install layout;
  - the launcher's behaviour (Task 6);
  - `make release`, `make deploy` and the Downloader database (Task 7);
  - mute (Task 3).
- **Produces:**
  - README sections: Install and Video settings (`MiSTer.ini`).
  - The on-device checklist in `docs/testing-on-mister.md` (spec §10).
  - "Resolved by Plan 3a" and "Plan 3b must handle" in the follow-ups.
  - A pending entry in the spikes log.

- [ ] **Step 1: Write the on-device checklist**

`docs/testing-on-mister.md` (new file):

```markdown
# Testing on the MiSTer

A manual checklist for a real MiSTer (spec §10). Run it before a release and
after changes to the launcher, the console handling, input or the display.
Record the results, with the date and the app version (Settings → About), in
`docs/spikes.md`.

**Sound:** turn the TV or amplifier down before the first start. The app
starts at the config's `volume_db` (0 dB by default) on the MiSTer.

## Install

- From this repository: `make deploy MISTER=<ip>` builds the release and
  copies it over ssh (the default root password is `1`).
- From a release: unzip `MiSTer_Subsonic-<version>.zip` onto the SD card root.

## Checklist

1. **Start.** Scripts → MiSTer_Subsonic. The app fills the screen, with no
   console text or cursor over it.
2. **Setup wizard.** With no `config.toml`, the wizard starts. Enter a server
   with the controller, then again with a USB keyboard. Afterwards
   `config.toml` has `token` and `salt` and no `password`.
3. **HDMI.** At 720p and 1080p (`video_mode`, see the README): the image fills
   the screen and the text is sharp. Covers load in the grids.
4. **CRT.** At 240p and 288p: the CRT layout, with nothing important cut off
   by overscan. Tune the title-safe margins if needed.
5. **Controllers.** The user's MiSTer mapping works. Unplug the pad while the
   app runs and plug it back in: it works again within about 2 s.
6. **Long FLAC.** Play a single-file album rip over 1 GB and seek to about
   80%. Playback resumes within about a second.
7. **Gapless.** Play a gapless album (a live album or a mix): no gap between
   tracks.
8. **Network drop.** Unplug the network or turn off Wi-Fi mid-track. The app
   shows buffering, then plays on when the network is back, or shows a clear
   error.
9. **Mute.** Press M on a keyboard, and use Settings → Playback → Mute. The
   volume keys bring the sound back.
10. **Exit.** Use Settings → Exit, and holding B for 2 s on the home screen.
    Both return to the Scripts menu with the text readable and "MiSTer
    Subsonic closed." shown. The controller works in the menu afterwards.
11. **BGM.** With BGM playing in the menu, start the app: the music stops.
    Exit: BGM plays again.
12. **SAM.** With Super Attract Mode enabled, leave the app idle past SAM's
    timeout: no game starts. After exit, SAM is enabled again.
13. **Crash recovery.** Over ssh, run `kill -9 $(pidof mistersubsonic)` while
    the app runs. The launcher puts the console back and returns to the
    menu, and the next start works.
14. **Leftover app.** Start the app over ssh
    (`/media/fat/mistersubsonic/mistersubsonic &`), then from the Scripts
    menu. The first one is stopped and the new one takes over.
15. **Overwrite watchdog.** Over ssh, run
    `head -c 200000 /dev/urandom > /dev/fb0` while the app shows a still
    screen. The noise is gone within about 2 s.
16. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
    "exiting" line for each run, and `crash.txt` beside it is empty.

## Benchmarks

`make deploy-dev MISTER=<ip>` copies the test binaries. The spikes and the
render benchmark (`ui.test -test.bench Repaint`) are described in
`docs/spikes.md`; record their results there.
```

- [ ] **Step 2: Update the README, the spikes log and the follow-ups**

Save this patch as `/tmp/t8-code.patch` and apply it from the repository root with `git apply /tmp/t8-code.patch` (it must apply cleanly; if it doesn't, the tree is not at the end of Task 7):

````diff
diff --git a/README.md b/README.md
index ca6ba0d..d44c886 100644
--- a/README.md
+++ b/README.md
@@ -8,8 +8,32 @@ albums (A–Z, by year, by genre), playlists, starred items and search with an o
 album pages, Now Playing and the queue, on HDMI (sidebar and cover grids) or CRT (lists)
 layouts, driven by a controller or keyboard; a setup wizard, Settings (servers, playback,
 display) and a screensaver. Streaming, FLAC/MP3/WAV decoding, gapless playback, seek,
-scrobbling and resume work. Still to come: the MiSTer launcher and releases. On-device (MiSTer)
-checks are still pending; see `docs/spikes.md`.
+scrobbling and resume work, and it installs on a MiSTer with a Scripts-menu launcher. Still to
+come: tuning on the device. On-device (MiSTer) checks are still pending; see `docs/spikes.md` and
+`docs/testing-on-mister.md`.
+
+## Install
+
+With MiSTer Downloader or update_all, add this to `/media/fat/downloader.ini` and run an update:
+
+```ini
+[mistersubsonic]
+db_url = https://raw.githubusercontent.com/OWNER/REPO/db/db.json
+```
+
+Or unzip a release's `MiSTer_Subsonic-<version>.zip` onto the SD card: it holds
+`Scripts/MiSTer_Subsonic.sh` and the `mistersubsonic/` folder.
+
+Start it from the Scripts menu: **MiSTer_Subsonic**. While it runs, background music (BGM) is
+stopped and Super Attract Mode (SAM) is disabled; both come back when you exit (Settings → Exit,
+or hold B on the home screen). Updates never touch your settings.
+
+Everything the app keeps is in `/media/fat/mistersubsonic/`:
+- `config.toml` holds your settings. `config.example.toml` explains every option.
+- `servers/<name>/` holds each server's resume state, scrobble queue and cover cache.
+- `log.txt` and `log.txt.1` hold at most 1 MB each. `crash.txt` records crashes.
+- `fonts/` is optional: `.ttf` or `.otf` fonts put there are used for characters the built-in
+  font lacks, such as CJK.
 
 ## Setup
 
@@ -28,6 +52,31 @@ drifting cover after some idle minutes on Now Playing; off with 0). Each server
 state, scrobble queue and cover cache in `servers/<name>/` next to the config, so `cover_art_mb`
 applies per server (each server's cache gets that much).
 
+## Video settings (MiSTer.ini)
+
+The app draws on the MiSTer's Linux framebuffer. That framebuffer is sized from `MiSTer.ini`; the
+app never changes the video mode itself. Keep `fb_size=0` (automatic) and `fb_terminal=1` (both
+the defaults).
+
+- **HDMI:**
+  - `video_mode=0` (1280x720@60) or `7` (1280x720@50) gives a 1280x720 framebuffer, which the
+    interface fills pixel for pixel.
+  - `video_mode=8` (1920x1080@60) works too, scaled up by 1.5.
+- **CRT (15 kHz):** add a `[Menu]` section, for example the 240p mode SAM uses for its CRT
+  video:
+
+  ```ini
+  [Menu]
+  video_mode=640,16,64,80,240,1,3,14,12380
+  vga_scaler=1
+  composite_sync=1
+  ```
+
+  This gives a 640x240 framebuffer, which the CRT layout fills. It also changes how the MiSTer
+  menu itself is shown. A 288-line mode (for 50 Hz TVs) works the same way.
+- **Interlaced CRT modes:** a 480i/576i mode looks like HDMI 480p/576p to the app, so set
+  `profile = "crt"` under `[display]` in `config.toml`.
+
 ## Controls
 
 | Button | Browsing | Now Playing |
@@ -54,10 +103,18 @@ Regenerating audio fixtures needs sox, flac and ffmpeg.
 make test        # unit tests (race detector); no network, server or sound device needed
 make e2e         # silent end-to-end runs (CLI and app) against a mock server
 make viewer      # run the app in your browser (CONFIG=path/to/config.toml; starts at -30 dB)
+make launcher-test   # the Scripts-menu launcher, against stand-ins for BGM, SAM and the app
 make mister      # ARMv7 binaries in bin/arm/ (checks glibc <= 2.31)
+make release VERSION=v1.0.0   # the SD card files and zip in bin/release/
+make deploy MISTER=mister.local       # install the release on a MiSTer over ssh
 make deploy-dev MISTER=mister.local   # copy dev tools to /media/fat/mistersubsonic/dev
 ```
 
+A `v*` tag builds a GitHub release (`.github/workflows/release.yml`). The release has:
+- the zip;
+- its files as assets;
+- the Downloader database, published on the `db` branch (`tools/mkdb`).
+
 Try it against a server (sound goes to your default device, starting at
 -30 dB unless you pass `-volume`; `-null` plays silently):
 
@@ -67,7 +124,8 @@ bin/mss-cli -config config.toml ping
 bin/mss-cli -config config.toml play-album <album-id>
 ```
 
-`config.toml` can also be written by hand; it needs at least one server:
+`config.toml` can also be written by hand (`sdcard/mistersubsonic/config.example.toml` explains
+every option); it needs at least one server:
 
 ```toml
 [[server]]
@@ -77,9 +135,8 @@ username = "alice"
 password = "…"   # or token + salt, or api_key
 ```
 
-The layout follows the framebuffer: `display.profile` is `auto` (default), `hdmi` or `crt`.
-A 480i/576i CRT can't be told apart from a 480p/576p HDMI framebuffer, so `auto` picks HDMI;
-on interlaced CRT modes set `profile = "crt"` under `[display]`.
+The layout follows the framebuffer: `display.profile` is `auto` (default), `hdmi` or `crt`
+(see Video settings above).
 
 ## License
 
diff --git a/docs/spikes.md b/docs/spikes.md
index 12efcbc..d67ad46 100644
--- a/docs/spikes.md
+++ b/docs/spikes.md
@@ -47,3 +47,7 @@ pending — MiSTer unavailable. Still to do:
 ## Plan 2c on the MiSTer
 
 pending — MiSTer unavailable. Still to do: run the setup wizard on the TV with the controller and a USB keyboard; Settings → Servers switch; check the screensaver on HDMI and CRT; confirm the config lands in /media/fat/mistersubsonic (exFAT can't store the 0600 mode; it only protects on the desktop).
+
+## Plan 3a on the MiSTer
+
+pending — MiSTer unavailable. Run the checklist in `docs/testing-on-mister.md` and record the results here: install with `make deploy`, then start from the Scripts menu; check the console and cursor, BGM and SAM, exit and crash recovery, the watchdog, and the log.
diff --git a/docs/superpowers/plan-2a-followups.md b/docs/superpowers/plan-2a-followups.md
index b20c237..8af6f33 100644
--- a/docs/superpowers/plan-2a-followups.md
+++ b/docs/superpowers/plan-2a-followups.md
@@ -158,3 +158,25 @@ Also still open: ReplayGain changes take effect from the track after the prefetc
   - `playKeys`;
   - the wizard keyboard-fit test uses the safe area rather than Draw's area, and doesn't cover the API-key step or a mini bar;
   - the config flush race test is probabilistic.
+
+## Resolved by Plan 3a
+
+- The framebuffer follows the console's pan offset (x/yoffset). The `/dev/mem` fallback maps from a page boundary. `smem_len` is checked against the panned screen, and the layout has tests.
+- Clean shutdown restores `KD_TEXT` (plan 1): on exit, on SIGINT/SIGTERM and after a recovered panic. The launcher also runs `mistersubsonic -restore-console` after every exit, and a clean-up that takes more than 10 s is cut short.
+- `config.example.toml` ships, and the saved config's header points to it.
+- The mute toggle (spec §6).
+
+## Plan 3b must handle
+
+Everything under "Plan 3 must handle" above, plus the Plan 3 items in `plan-1-followups.md`:
+- memory: stream rings, cover decode size
+- raw MP3 seek
+- the device guards
+- the clean-shutdown internals of the engine
+- stream hardening
+- the mock server's `timeOffset`
+
+And also:
+- **Input:** check that default keys fire only for codes a user's map omits, and that devices whose grab returns EBUSY are handled, alongside Main_MiSTer on the device.
+- **ReplayGain:** re-queue the prefetched track on a change.
+- **Performance:** whatever the device benchmarks and spikes 2 and 3 call for.
````

The `MiSTer.ini` values come from upstream Main_MiSTer:
- The `video_mode` indexes are in `MiSTer.ini`.
- The framebuffer size follows from `video_fb_config()` in `video.cpp`: with `fb_size=0` it is halved only above 1920×1080, so 720p gives 1280×720 and 1080p gives 1920×1080.
- The CRT `[Menu]` example is the 240p mode MiSTer_SAM uses for its CRT video, which gives a 640×240 framebuffer.

Keep these values as they are.

- [ ] **Step 3: Check the docs build nothing broken**

Run: `make test && make e2e`
Expected: `test-launcher ok`, `ok` for every Go package, `e2e ok: …` and `e2e-ui ok: …`.

Run: `grep -n "OWNER/REPO" README.md`
Expected: one line, the `db_url`. It stays a placeholder until the repository is published. Tell the user so in your report.

- [ ] **Step 4: Check whether the MiSTer answers**

Check without printing `.env`. If the MiSTer's SSH port doesn't answer, leave the "pending" entry that Step 2 added to `docs/spikes.md` and go to Step 6.

- [ ] **Step 5: If the MiSTer answers, run the checklist (ask the user first)**

Ask the user before doing anything on the device. `make deploy MISTER=<ip>` installs the app, and the checklist plays sound on their TV.

With their go-ahead:
1. Install.
2. Ask them to run `docs/testing-on-mister.md` item by item, with the volume low.
3. Record the answers under "Plan 3a on the MiSTer" in `docs/spikes.md`, replacing "pending", with the date and the version shown in Settings → About.

- [ ] **Step 6: Commit**

```bash
git add README.md docs/spikes.md docs/superpowers/plan-2a-followups.md docs/testing-on-mister.md
git commit -m "docs: install, MiSTer.ini, the on-device checklist; Plan 3a follow-ups" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## After this plan

**Plan 3b covers** the device-side work, which needs the MiSTer to measure:
- spikes 2 and 3 (audio and toolchain on the device, and the Cortex-A9 costs);
- the `Repaint` benchmarks;
- memory: the stream rings, and the cover decode size (2048²);
- the per-pixel `FromImage` fast paths;
- raw MP3 seek for CBR files;
- the device guards, and the engine's shutdown internals;
- stream hardening (416 on reconnect, 429);
- the disk-cache nits;
- ReplayGain re-queueing the prefetched track;
- input checks with real maps and grabs alongside Main_MiSTer;
- whatever the Plan 3a checklist turns up.

The list is in `docs/superpowers/plan-2a-followups.md`, under "Plan 3b must handle".
