// Command mistersubsonic is the MiSTer Subsonic app: the TV interface on the
// framebuffer (on a MiSTer) or in a browser (-display viewer, for development).
//
//	mistersubsonic                                  # MiSTer: /dev/fb0, evdev, config in /media/fat/mistersubsonic
//	mistersubsonic -config config.toml -display viewer   # PC: open the printed URL
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/devview"
	"mistersubsonic/internal/gfx"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
	"mistersubsonic/internal/ui"
)

// openDevice is replaceable so tests never touch a sound card.
var openDevice = audio.OpenDevice

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
	flag.Float64Var(&f.volume, "volume", math.NaN(), "start volume in dB (-60..0); default: config, or -30 with -display viewer")
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
	vol := cfg.Playback.VolumeDB
	switch {
	case !math.IsNaN(f.volume):
		vol = f.volume
	case f.display == "viewer" && !f.null && vol > -30:
		vol = -30
		fmt.Println("starting at -30 dB (use -volume to change)")
	}

	// The player and art loader are created once the server connects.
	var (
		pl     *player.Player
		loader *art.Loader
		app    *ui.App
	)

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

	pctx, cancelPlayer := context.WithCancel(context.Background())
	playerDone := make(chan struct{})
	defer func() {
		cancelPlayer()
		if pl != nil {
			<-playerDone
		}
		if loader != nil {
			loader.Close()
		}
	}()

	// connectServer runs off the UI goroutine; the caller attaches the result.
	connectServer := func() (*subsonic.Client, error) {
		srv, _ := cfg.ActiveServer()
		c, err := subsonic.New(subsonic.Options{
			BaseURL: srv.URL,
			Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
				Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
			CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
		})
		if err != nil {
			return nil, err
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if _, err := c.Connect(cctx); err != nil {
			return nil, err
		}
		return c, nil
	}
	// startPlayer runs on the UI goroutine after a successful connect.
	startPlayer := func(a *ui.App, c *subsonic.Client) {
		disk, err := cache.Open(filepath.Join(dataDir, "cache", "art"), int64(cfg.Cache.CoverArtMB)<<20)
		if err != nil {
			log.Printf("art cache disabled: %v", err)
			disk = nil
		}
		loader = art.New(art.Options{Fetch: art.HTTPFetcher(c), Disk: disk, Ready: a.ArtReady})
		pl = player.New(player.Options{
			Engine: eng, API: c,
			Open: player.NewOpener(c, player.StreamSettings{
				TranscodeFormat: cfg.Playback.TranscodeFormat, TranscodeBitrate: cfg.Playback.TranscodeBitrate,
				WindowBytes: int64(cfg.Playback.BufferMB) << 20,
			}),
			ReplayGain: cfg.Playback.ReplayGain, Scrobble: cfg.Playback.Scrobble, VolumeDB: vol,
			ResumePath: filepath.Join(dataDir, "state.json"), ScrobblePath: filepath.Join(dataDir, "cache", "scrobbles.json"),
		})
		go func() { pl.Run(pctx); close(playerDone) }()
		a.Attach(c, pl, loader)
	}

	app, err := ui.New(ui.Options{
		Display: disp, Profile: prof, Inputs: inputs,
		FallbackFonts: filepath.Join(dataDir, "fonts"),
		Start: func(a *ui.App) {
			var connect func()
			connect = func() {
				if audioErr != nil {
					a.Replace(ui.NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", audioErr, f.config), nil))
					return
				}
				if cfgErr != nil {
					a.Replace(ui.NewMessageScreen("Setup needed", configMessage(f.config, cfgErr), nil))
					return
				}
				if _, ok := cfg.ActiveServer(); !ok {
					a.Replace(ui.NewMessageScreen("Setup needed", "Add a [[server]] to "+f.config+".", nil))
					return
				}
				a.Replace(ui.NewMessageScreen("Connecting…", "Connecting to the server…", nil))
				go func() {
					c, err := connectServer()
					a.Post(func() {
						if err != nil {
							srv, _ := cfg.ActiveServer()
							a.Replace(ui.NewMessageScreen("Can't reach the server",
								fmt.Sprintf("%s: %s.\n%s", displayURL(srv.URL), subsonic.Classify(err), hint(err)), connect))
							return
						}
						if pl == nil { // first successful connect
							startPlayer(a, c)
						}
						a.Replace(ui.NewHomeScreen())
					})
				}()
			}
			connect()
		},
	})
	if err != nil {
		return err
	}
	return app.Run(ctx)
}

// displayURL is raw without credentials, safe to show on screen or log.
func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<server>"
	}
	u.User = nil
	return u.String()
}

func configMessage(path string, err error) string {
	if errors.Is(err, config.ErrNotFound) {
		return "No config file yet. Create " + path + " with your server (see config.example.toml), then restart."
	}
	return "The config file has a problem:\n" + err.Error()
}

func hint(err error) string {
	switch subsonic.Classify(err) {
	case subsonic.KindTLS:
		return "For a self-signed certificate set ca_file (or insecure_skip_verify) in the config."
	case subsonic.KindAuth:
		return "Check the username and password in the config."
	case subsonic.KindPlaintextRefused:
		return "This server needs the plain password: use https, or set allow_plaintext_password."
	case subsonic.KindUnreachable, subsonic.KindTimeout:
		return "Check that the server is running and the URL is right."
	}
	return ""
}
