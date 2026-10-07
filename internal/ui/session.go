package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/subsonic"
)

// The session: the UI owns the configuration (edited by the wizard and
// Settings, saved off the UI goroutine) and asks the app around it to
// connect with it (Options.Connect). The app answers with Connected or
// ConnectFailed on the UI goroutine.

// ConnInfo describes the live connection (for the insecure badge and About).
type ConnInfo struct {
	Server config.Server
	Info   *subsonic.ServerInfo
	Auth   subsonic.AuthMethod
}

const saveDelay = 1500 * time.Millisecond // volume steps are saved together

// Config is the current configuration (nil before a valid one exists).
// Callers must not keep it across screens; change it with UpdateConfig.
func (a *App) Config() *config.Config { return a.cfg }

// Conn is the live connection, if any.
func (a *App) Conn() (ConnInfo, bool) { return a.conn, a.conn.Server.Name != "" }

// start picks the first screen: a problem to explain, or connecting.
func (a *App) start() {
	switch {
	case a.o.AudioErr != nil:
		a.Replace(NewMessageScreen("No audio device", fmt.Sprintf("Couldn't open the audio device (%v).\nCheck alsa_device in %s, or that nothing else is using the sound card.", a.o.AudioErr, a.o.ConfigPath), nil))
	case a.cfg == nil && errors.Is(a.o.ConfigErr, config.ErrNotFound), a.cfg != nil && len(a.cfg.Servers) == 0:
		a.Replace(NewWizardScreen(true, false))
	case a.cfg == nil:
		a.Replace(NewMessageAction("The config file has a problem",
			fmt.Sprintf("%v\n\nFix %s and restart, or set up again: the file is then kept as %s.invalid-<date>.", a.o.ConfigErr, a.o.ConfigPath, a.o.ConfigPath),
			"Press A to set up again", func() { a.Push(NewWizardScreen(false, true)) }))
	default:
		a.Connect()
	}
}

// Connect drops the current connection and asks for one to the config's
// active server. Call on the UI goroutine.
func (a *App) Connect() {
	if a.cfg == nil {
		return
	}
	srv, ok := a.cfg.ActiveServer()
	if !ok || a.o.Connect == nil {
		return
	}
	a.Detach()
	a.connecting = true
	a.Replace(NewMessageScreen("Connecting…", "Connecting to "+displayURL(srv.URL)+"…", nil))
	a.o.Connect(a, a.cfg.Clone())
}

// Detach forgets the library, player and art of the old connection.
func (a *App) Detach() {
	a.o.Library, a.o.Player, a.o.Art = nil, nil, nil
	a.conn = ConnInfo{}
	a.connecting = false
	a.insecure = false
	a.stars, a.starBusy, a.starGen = map[starKey]bool{}, map[starKey]bool{}, 0 // they belong to the old connection
	a.publishLive()
	a.publishStars()
	a.seen.reset()
	a.notifyRemote(remote.QueueChanged)
	a.notifyRemote(remote.StateChanged)
	a.dirty = true
}

// Connected installs a new connection and shows the library. The caches
// of the previous connection (artists, star changes) are dropped.
func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
	a.Attach(lib, pl, art)
	if a.volumePending && pl != nil {
		pl.SetVolumeDB(a.pendingDB) // changed while disconnected
		if a.cfg != nil {
			a.cfg.Playback.VolumeDB = a.pendingDB
		}
	}
	a.volumePending = false
	if a.muted && pl != nil {
		pl.SetMuted(true) // a new server's player starts muted too
	}
	a.conn = info
	a.connecting = false
	a.SetInsecure(info.Server.InsecureSkipVerify)
	a.artists, a.stars, a.starBusy, a.starGen = nil, map[starKey]bool{}, map[starKey]bool{}, 0
	a.publishStars()
	a.Replace(NewRootScreen(a.P))
	a.drainInput()
	a.notifyRemote(remote.QueueChanged) // open pages show the new connection's queue and state
	a.notifyRemote(remote.StateChanged)
}

// ConnectFailed shows why the server couldn't be reached and what to do.
func (a *App) ConnectFailed(srv config.Server, err error) {
	a.connecting = false
	a.Replace(NewUnreachableScreen(srv, err))
}

// drainInput drops queued key events and held-button state (spec §8.4:
// after the wizard, presses meant for it must not act on the next screen).
func (a *App) drainInput() {
	for {
		select {
		case <-a.in:
		default:
			a.rep = input.Repeater{}
			a.swallowed = map[input.Button]bool{}
			a.wakeKeys = map[input.Button]bool{}
			a.bDown = time.Time{}
			return
		}
	}
}

// UpdateConfig changes the configuration and saves it: now, or with soon
// after a short pause (so a run of volume steps is one write).
func (a *App) UpdateConfig(change func(c *config.Config), soon bool) {
	if a.cfg == nil {
		a.cfg = config.Default()
	}
	change(a.cfg)
	if d := a.cfg.Display; a.laidOut != [3]int{d.OverscanLeft, d.OverscanRight, d.OverscanY} {
		a.relayout() // live: the picture moves in from the edges at once
	}
	if soon {
		a.saveAt = a.o.Now().Add(saveDelay)
		return
	}
	a.saveAt = time.Time{}
	a.saveConfig()
}

// saveConfig writes a copy of the configuration off the UI goroutine. The
// writes are serialized; a change during a write is saved right after it.
func (a *App) saveConfig() {
	if a.o.ConfigPath == "" || a.cfg == nil {
		return
	}
	if a.saving {
		a.saveAgain = true
		return
	}
	a.saving = true
	cfg, path := a.cfg.Clone(), a.o.ConfigPath
	go func() {
		a.saveMu.Lock()
		err := config.Save(path, cfg)
		a.saveMu.Unlock()
		a.Post(func() {
			a.saving = false
			if err != nil {
				log.Printf("config: %v", err)
				a.Toast("Couldn't save the settings: %s", saveProblem(err))
			}
			if a.saveAgain {
				a.saveAgain = false
				a.saveConfig()
			}
		})
	}()
}

// saveProblem is the short, secret-free reason a save failed.
func saveProblem(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "invalid settings"
}

// flushConfig saves a pending change before exit (on the UI goroutine;
// waits for a write in progress).
func (a *App) flushConfig() {
	if a.saveAt.IsZero() && !a.saveAgain && !a.saving {
		return
	}
	if a.o.ConfigPath == "" || a.cfg == nil {
		return
	}
	// A write in progress holds an older copy: let it (and the save it
	// queues for a change made meanwhile) finish, or it would land after ours.
	for a.saving {
		(<-a.post)()
	}
	if a.saveAt.IsZero() {
		return // those writes already saved the latest change
	}
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if err := config.Save(a.o.ConfigPath, a.cfg.Clone()); err != nil {
		log.Printf("config: %v", err)
	}
	a.saveAt = time.Time{}
}

// ConnectTimeout bounds Dial.
const ConnectTimeout = 10 * time.Second

// Dial makes a client for srv and checks that the server answers (ping,
// with the auth fallbacks of spec §4). It does network I/O: call it off the
// UI goroutine.
func Dial(ctx context.Context, srv config.Server) (*subsonic.Client, *subsonic.ServerInfo, error) {
	c, err := subsonic.New(subsonic.Options{
		BaseURL: srv.URL,
		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
			Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
	})
	if err != nil {
		return nil, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	info, err := c.Connect(cctx)
	if err != nil {
		return nil, nil, err
	}
	return c, info, nil
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

// hint is what to try for a connection error.
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
