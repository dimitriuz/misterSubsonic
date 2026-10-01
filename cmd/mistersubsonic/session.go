package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mistersubsonic/internal/art"
	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/cache"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
	"mistersubsonic/internal/ui"
)

// sessionUI is what sessions needs from the UI (*ui.App; a fake in tests).
type sessionUI interface {
	Post(f func())
	ArtReady(k art.Key)
	Connected(info ui.ConnInfo, lib ui.Library, pl ui.Player, art ui.ArtSource)
	ConnectFailed(srv config.Server, err error)
}

// sessions owns the connection to a server: the client, the player and the
// art loader. connect replaces the current session with a new one for the
// config's active server; the newest request wins, and every session it
// replaces is stopped (the play queue saved) and closed. Each server keeps
// its data (resume state, scrobble queue, cover cache) in its own folder:
// IDs mean different songs on different servers.
type sessions struct {
	ctx     context.Context // the app's: connects and players end with it
	eng     *audio.Engine
	dataDir string

	mu      sync.Mutex
	gen     int // bumped by each connect; older ones are stale
	cur     *session
	volume  float64 // start volume of the next player (the last one's, after the first)
	closing bool
	retired []*session // replaced, waiting to be stopped (by whoever holds swap next)

	// swap serialises everything that stops or starts a player (draining
	// retired, build and install, close), so a new player never starts
	// before the old one has saved its queue and volume. It is never held
	// while dialling. Lock order: swap, then mu.
	swap sync.Mutex

	beforeStop func(*session) // test hook, nil in production
	afterBuild func(*session) // test hook: a session is built, not yet installed
}

type session struct {
	pl     *player.Player
	loader *art.Loader
	cancel context.CancelFunc
	done   chan struct{} // closed when pl.Run returns
}

func newSessions(ctx context.Context, eng *audio.Engine, dataDir string, volume float64) *sessions {
	return &sessions{ctx: ctx, eng: eng, dataDir: dataDir, volume: volume}
}

// connect is ui.Options.Connect.
func (m *sessions) connect(a sessionUI, cfg *config.Config) {
	srv, ok := cfg.ActiveServer()
	m.mu.Lock()
	m.gen++
	gen := m.gen
	if m.cur != nil {
		m.retired = append(m.retired, m.cur)
		m.cur = nil
	}
	m.mu.Unlock()
	if !ok { // no server left: just end the current session
		go m.drain()
		return
	}
	server := *srv
	go func() {
		m.drain() // free the engine before the (slow) dial
		c, info, err := ui.Dial(m.ctx, server)
		if err != nil {
			a.Post(func() {
				if m.current(gen) {
					a.ConnectFailed(server, err)
				}
			})
			return
		}
		m.swap.Lock()
		m.drainLocked()
		if !m.current(gen) {
			m.swap.Unlock()
			return // superseded or shutting down: build nothing
		}
		s := m.build(a, c, server, cfg.Playback, cfg.Cache)
		if m.afterBuild != nil {
			m.afterBuild(s)
		}
		m.mu.Lock()
		stale := gen != m.gen || m.closing // connect and close change these without swap
		if stale {
			m.retired = append(m.retired, s)
		} else {
			m.cur = s
		}
		m.mu.Unlock()
		if stale {
			m.drainLocked() // superseded while building: stop it, never show it
			m.swap.Unlock()
			return
		}
		m.swap.Unlock()
		a.Post(func() {
			if m.current(gen) {
				a.Connected(ui.ConnInfo{Server: server, Info: info, Auth: c.AuthMethod()}, c, s.pl, s.loader)
			}
		})
	}()
}

func (m *sessions) current(gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return gen == m.gen && !m.closing
}

// build makes and starts the player and art loader (disk I/O: off the UI
// goroutine).
func (m *sessions) build(a sessionUI, c *subsonic.Client, srv config.Server, pb config.Playback, cc config.Cache) *session {
	dir := m.serverDir(srv.Name)
	disk, err := cache.Open(filepath.Join(dir, "cache", "art"), int64(cc.CoverArtMB)<<20)
	if err != nil {
		log.Printf("art cache disabled: %v", err)
		disk = nil
	}
	m.mu.Lock()
	vol := m.volume
	m.mu.Unlock()
	ctx, cancel := context.WithCancel(m.ctx)
	s := &session{
		loader: art.New(art.Options{Fetch: art.HTTPFetcher(c), Disk: disk, Ready: a.ArtReady}),
		pl: player.New(player.Options{
			Engine: m.eng, API: c,
			Open: player.NewOpener(c, player.StreamSettings{
				TranscodeFormat: pb.TranscodeFormat, TranscodeBitrate: pb.TranscodeBitrate,
				WindowBytes: int64(pb.BufferMB) << 20,
			}),
			ReplayGain: pb.ReplayGain, Scrobble: pb.Scrobble, VolumeDB: vol,
			ResumePath: filepath.Join(dir, "state.json"), ScrobblePath: filepath.Join(dir, "cache", "scrobbles.json"),
		}),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() { s.pl.Run(ctx); close(s.done) }()
	return s
}

// serverDir is where a server's data lives: servers/<name>-<hash> in the
// data folder, the name made safe for a file name and the hash of the exact
// name keeping servers apart when they sanitize alike ("a/b", "a_b") or
// differ only in case (exFAT folds it). Older versions used the bare safe
// name; a server whose folder is there keeps using it, so an upgrade keeps
// its cache and resume state.
func (m *sessions) serverDir(name string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r < ' ':
			return '_'
		}
		return r
	}, name)
	if safe == "" || safe == "." || safe == ".." {
		safe = "_"
	}
	servers := filepath.Join(m.dataDir, "servers")
	dir := filepath.Join(servers, safe)
	if !claimLegacy(dir, name) {
		h := sha1.Sum([]byte(name))
		dir = filepath.Join(servers, safe+"-"+hex.EncodeToString(h[:4]))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("server data folder %s: %v", dir, err)
	}
	return dir
}

// claimLegacy says whether name may use the old bare folder dir. The folder
// has an owner marker (.server, the exact server name); a folder without one
// predates this version and goes to the first server that asks, which writes
// the marker. Any other name (one that sanitizes alike, or differs only in
// case on a case-folding card) gets its own hashed folder.
func claimLegacy(dir, name string) bool {
	fi, err := os.Stat(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("server data folder %s: %v", dir, err)
		}
		return false
	}
	if !fi.IsDir() {
		return false
	}
	marker := filepath.Join(dir, ".server")
	owner, err := os.ReadFile(marker)
	switch {
	case err == nil:
		return string(owner) == name
	case errors.Is(err, fs.ErrNotExist):
		if err := os.WriteFile(marker, []byte(name), 0o644); err != nil {
			log.Printf("server data folder %s: writing the owner marker: %v", dir, err)
		}
		return true
	}
	log.Printf("server data folder %s: reading the owner marker: %v", dir, err)
	return false
}

// stop ends a session: the player saves its queue and stops the engine,
// then the art loader closes. The session's volume carries to the next.
func (m *sessions) stop(s *session) {
	if m.beforeStop != nil {
		m.beforeStop(s)
	}
	vol := s.pl.State().VolumeDB
	s.cancel()
	<-s.done
	s.loader.Close()
	m.mu.Lock()
	m.volume = vol
	m.mu.Unlock()
}

// drain stops every retired session (waiting for any swap in progress).
func (m *sessions) drain() {
	m.swap.Lock()
	defer m.swap.Unlock()
	m.drainLocked()
}

// drainLocked is drain with swap held.
func (m *sessions) drainLocked() {
	for {
		m.mu.Lock()
		if len(m.retired) == 0 {
			m.mu.Unlock()
			return
		}
		s := m.retired[0]
		m.retired = m.retired[1:]
		m.mu.Unlock()
		m.stop(s)
	}
}

// close stops the current session and any connect still in flight from
// starting one (on exit). It returns only when every stop and build in
// progress has finished, so the engine can be closed after it.
func (m *sessions) close() {
	m.mu.Lock()
	m.closing = true
	m.mu.Unlock()
	m.swap.Lock()
	defer m.swap.Unlock()
	m.mu.Lock()
	if m.cur != nil {
		m.retired = append(m.retired, m.cur)
		m.cur = nil
	}
	m.mu.Unlock()
	m.drainLocked()
}
