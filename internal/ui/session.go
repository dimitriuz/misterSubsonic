package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"time"

	"mistersubsonic/internal/config"
	"mistersubsonic/internal/input"
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
	case a.cfg == nil && errors.Is(a.o.ConfigErr, config.ErrNotFound):
		a.Replace(NewMessageScreen("Setup needed", "No config file yet. Create "+a.o.ConfigPath+" with a [[server]] section (name, url, username, password), then restart.", nil))
	case a.cfg == nil:
		a.Replace(NewMessageScreen("Setup needed", "The config file has a problem:\n"+fmt.Sprint(a.o.ConfigErr), nil))
	case len(a.cfg.Servers) == 0:
		a.Replace(NewMessageScreen("Setup needed", "Add a [[server]] to "+a.o.ConfigPath+".", nil))
	default:
		a.Connect()
	}
}

// Connect drops the current connection and asks for one to the config's
// active server. Call on the UI goroutine.
func (a *App) Connect() {
	srv, ok := a.cfg.ActiveServer()
	if !ok || a.o.Connect == nil {
		return
	}
	a.Detach()
	a.Replace(NewMessageScreen("Connecting…", "Connecting to "+displayURL(srv.URL)+"…", nil))
	a.o.Connect(a, a.cfg.Clone())
}

// Detach forgets the library, player and art of the old connection.
func (a *App) Detach() {
	a.o.Library, a.o.Player, a.o.Art = nil, nil, nil
	a.conn = ConnInfo{}
	a.insecure = false
	a.dirty = true
}

// Connected installs a new connection and shows the library. The caches
// of the previous connection (artists, star changes) are dropped.
func (a *App) Connected(info ConnInfo, lib Library, pl Player, art ArtSource) {
	a.Attach(lib, pl, art)
	a.conn = info
	a.SetInsecure(info.Server.InsecureSkipVerify)
	a.artists, a.stars, a.starGen = nil, map[starKey]bool{}, 0
	a.Replace(NewRootScreen(a.P))
	a.drainInput()
}

// ConnectFailed shows why the server couldn't be reached and what to do.
func (a *App) ConnectFailed(srv config.Server, err error) {
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
	a.saveMu.Lock()
	defer a.saveMu.Unlock()
	if err := config.Save(a.o.ConfigPath, a.cfg.Clone()); err != nil {
		log.Printf("config: %v", err)
	}
	a.saveAt = time.Time{}
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
