// Command mistersubsonic is the MiSTer Subsonic app: the TV interface on the
// framebuffer (on a MiSTer) or in a browser (-display viewer, for development).
//
//	mistersubsonic                                  # MiSTer: /dev/fb0, evdev, config in /media/fat/mistersubsonic
//	mistersubsonic -config config.toml -display viewer   # PC: open the printed URL
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/devview"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/logfile"
	"mistersubsonic/internal/platform"
	"mistersubsonic/internal/ui"
)

// openDevice is replaceable so tests never touch a sound card.
var openDevice = audio.OpenDevice

// version is set by the release build (-ldflags "-X main.version=v1.0.0").
var version = "dev"

type flags struct {
	config, display, viewerAddr, frames, profile, fbdev, keys, log, screenshots string
	null, restoreConsole, verifyRedraw                                          bool
	volume                                                                      float64
	exitAfter                                                                   time.Duration
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
	flag.StringVar(&f.log, "log", "auto", "log file: auto (log.txt next to the config on the framebuffer, stderr elsewhere), - (stderr) or a path")
	flag.StringVar(&f.screenshots, "screenshots", "auto", "screenshot folder: auto (/media/fat/screenshots/MiSTer_Subsonic beside the config on the framebuffer, screenshots/ next to the config elsewhere), a path, or \"\" for none")
	flag.BoolVar(&f.verifyRedraw, "verify-redraw", false, "check every partial redraw against a full one and log any difference (debugging)")
	flag.DurationVar(&f.exitAfter, "exit-after", 0, "quit after this long (testing)")
	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
	flag.BoolVar(&f.restoreConsole, "restore-console", false, "put the console back in text mode and exit (the launcher runs this after the app)")
	flag.Parse()
	if f.restoreConsole {
		if err := restoreConsole(); err != nil {
			fmt.Fprintln(os.Stderr, "mistersubsonic:", err)
			os.Exit(1)
		}
		return
	}
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

// startVolume picks the start volume in dB. Anywhere but the MiSTer
// (linux/arm with the fbdev display), it starts at -30 dB unless -volume is
// given or the audio is null; quiet reports that the default was applied.
func startVolume(cfgDB, flagDB float64, null bool, display, goos, goarch string) (db float64, quiet bool) {
	switch {
	case !math.IsNaN(flagDB):
		return flagDB, false
	case !(goos == "linux" && goarch == "arm" && display == "fbdev") && !null && cfgDB > -30:
		return -30, true
	}
	return cfgDB, false
}

// logMax caps log.txt (and crash.txt): two files of 1 MB at most (spec §9).
const logMax = 1 << 20

// screenshotDir is where the screenshot button saves. On the framebuffer
// (the MiSTer, config in /media/fat/mistersubsonic) that is MiSTer's own
// screenshots folder, /media/fat/screenshots/MiSTer_Subsonic, where Main's
// screenshots go and where the Companion remote looks for the newest one.
func screenshotDir(flagDir, display, dataDir string) string {
	if flagDir != "auto" {
		return flagDir
	}
	if display == "fbdev" {
		return filepath.Join(filepath.Dir(dataDir), "screenshots", "MiSTer_Subsonic")
	}
	return filepath.Join(dataDir, "screenshots")
}

// openLog sends the log to log.txt next to the config when the app runs on
// the framebuffer (the MiSTer: nobody sees stderr there), or where -log
// says. Crashes the app can't catch (runtime errors, panics off the UI
// goroutine) go to crash.txt beside it. A log that can't be opened falls
// back to stderr. The returned func restores stderr and closes the files.
func openLog(flagPath, display, dataDir string) func() {
	path := flagPath
	if path == "auto" {
		path = "-"
		if display == "fbdev" {
			path = filepath.Join(dataDir, "log.txt")
		}
	}
	if path == "-" || path == "" {
		return func() {}
	}
	lf, err := logfile.Open(path, logMax)
	if err != nil {
		log.Printf("log: %v (logging to stderr)", err)
		return func() {}
	}
	log.SetOutput(lf)
	crash := filepath.Join(filepath.Dir(path), "crash.txt")
	if st, err := os.Stat(crash); err == nil && st.Size() > logMax {
		os.Remove(crash)
	}
	if cf, err := os.OpenFile(crash, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644); err == nil {
		debug.SetCrashOutput(cf, debug.CrashOptions{}) // keeps its own copy of the file
		cf.Close()
	}
	return func() {
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		log.SetOutput(os.Stderr)
		lf.Close()
	}
}

// shutdownLimit bounds the clean-up after the UI quits (saving the queue,
// closing audio): past it the app restores the console and exits anyway, so
// a stuck close never leaves the TV on a frozen screen.
var shutdownLimit = 10 * time.Second

// forceExit ends a shutdown that took too long; tests replace it.
var forceExit = func() {
	log.Printf("shutdown took longer than %v; exiting", shutdownLimit)
	platform.RestoreText()
	os.Exit(3)
}

// armDeadline starts the shutdown deadline (once).
func armDeadline(t **time.Timer, d time.Duration) {
	if *t == nil {
		*t = time.AfterFunc(d, forceExit)
	}
}

// shutdownSignals end the app cleanly (SIGHUP: the terminal went away).
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// beforeRun runs just before the UI loop; tests use it.
var beforeRun = func(*ui.App) {}

func run(f flags) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals...)
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

	dataDir := filepath.Dir(f.config)
	closeLog := openLog(f.log, f.display, dataDir)
	defer closeLog()
	var deadline *time.Timer
	defer func() {
		// Runs after every other clean-up below, a panic's included: a
		// panic on the UI goroutine still restores the console, input and
		// framebuffer, and is logged with its stack.
		if deadline != nil {
			deadline.Stop()
		}
		if r := recover(); r != nil {
			log.Printf("panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("panic: %v", r)
		}
		if err != nil {
			log.Printf("error: %v", err) // stderr isn't seen on the MiSTer
		}
	}()
	log.Printf("MiSTer Subsonic %s starting (%s, config %s)", version, f.display, f.config)
	defer log.Printf("MiSTer Subsonic exiting")

	cfg, warns, cfgErr := config.Load(f.config)
	if cfg == nil {
		cfg = config.Default()
	}
	for _, w := range warns {
		log.Printf("config: %s", w)
	}

	// Display and layout.
	profileName := cfg.Display.Profile
	if f.profile != "" {
		profileName = f.profile
	}
	var disp gfx.Display
	var inputs []<-chan input.Event
	switch f.display {
	case "fbdev":
		fb, err := openFB(f.fbdev, profileName, dataDir, cfg.Display.FullResolution)
		if err != nil {
			return err
		}
		defer func() { // after the framebuffer is closed: its old size back
			if err := fbControl().Restore(); err != nil {
				log.Printf("display: %v", err)
			}
		}()
		con, err := platform.GraphicsMode()
		if err != nil {
			log.Printf("console: %v (its text may show over the app)", err)
		}
		defer con.Restore() // after the framebuffer is blanked (defers run last-in first-out)
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
		if v.Exposed() {
			fmt.Println("warning: the viewer is open to your network: anyone on it can see the screen and press keys")
		}
		disp = v
		inputs = append(inputs, v.Events())
	case "headless":
		prof := ui.PickProfile(1280, 720, profileName)
		if f.frames != "" {
			if err := os.MkdirAll(f.frames, 0o755); err != nil {
				return err
			}
		}
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
	vol, quiet := startVolume(cfg.Playback.VolumeDB, f.volume, f.null, f.display, runtime.GOOS, runtime.GOARCH)
	if quiet {
		fmt.Println("starting at -30 dB (use -volume to change)")
	}

	dev := cfg.Playback.ALSADevice
	if dev == "default" {
		dev = ""
	}
	var eng *audio.Engine
	out, audioErr := openDevice(audio.DeviceOptions{Name: dev, Null: f.null})
	if audioErr == nil {
		defer out.Close()
		eng = audio.NewEngine(audio.EngineOptions{Output: out})
		defer eng.Close()
	}

	// Sessions (client, player, art) are built off the UI goroutine and
	// replaced on a server switch; the last one is stopped before the
	// engine and device close (defers run last-in first-out).
	sess := newSessions(ctx, eng, dataDir, vol)
	defer sess.close()
	defer armDeadline(&deadline, shutdownLimit) // runs first: bounds the clean-ups above

	var loaded *config.Config
	if cfgErr == nil {
		loaded = cfg
	}
	app, err := ui.New(ui.Options{
		Display: disp, Profile: prof, Inputs: inputs,
		FallbackFonts: filepath.Join(dataDir, "fonts"),
		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
		ScreenshotDir: screenshotDir(f.screenshots, f.display, dataDir),
		VerifyRedraw:  f.verifyRedraw,
		Connect:       func(a *ui.App, c *config.Config) { sess.connect(a, c) },
	})
	if err != nil {
		return err
	}
	beforeRun(app)
	err = app.Run(ctx)
	stop() // a second Ctrl-C now kills the process instead of waiting out the shutdown below
	return err
}
