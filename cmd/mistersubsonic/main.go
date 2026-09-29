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
	"syscall"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/devview"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/ui"
)

// openDevice is replaceable so tests never touch a sound card.
var openDevice = audio.OpenDevice

// version is set by the release build (-ldflags "-X main.version=v1.0.0").
var version = "dev"

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
	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 anywhere but the MiSTer")
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

	var loaded *config.Config
	if cfgErr == nil {
		loaded = cfg
	}
	app, err := ui.New(ui.Options{
		Display: disp, Profile: prof, Inputs: inputs,
		FallbackFonts: filepath.Join(dataDir, "fonts"),
		ConfigPath:    f.config, Config: loaded, ConfigErr: cfgErr, AudioErr: audioErr, Version: version,
		Connect: func(a *ui.App, c *config.Config) { sess.connect(a, c) },
	})
	if err != nil {
		return err
	}
	err = app.Run(ctx)
	stop() // a second Ctrl-C now kills the process instead of waiting out the shutdown below
	return err
}
