package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
}

type session struct {
	pl     *player.Player
	loader *art.Loader
	cancel context.CancelFunc
	done   chan struct{} // closed when pl.Run returns
}

const connectTimeout = 10 * time.Second

func newSessions(ctx context.Context, eng *audio.Engine, dataDir string, volume float64) *sessions {
	return &sessions{ctx: ctx, eng: eng, dataDir: dataDir, volume: volume}
}

// connect is ui.Options.Connect.
func (m *sessions) connect(a sessionUI, cfg *config.Config) {
	srv, ok := cfg.ActiveServer()
	if !ok {
		return
	}
	server := *srv
	m.mu.Lock()
	m.gen++
	gen, old := m.gen, m.cur
	m.cur = nil
	m.mu.Unlock()
	go func() {
		if old != nil {
			m.stop(old)
		}
		c, info, err := dial(m.ctx, server)
		if err != nil {
			a.Post(func() {
				if m.current(gen) {
					a.ConnectFailed(server, err)
				}
			})
			return
		}
		if !m.current(gen) {
			return // superseded or shutting down: build nothing
		}
		s := m.build(a, c, server, cfg.Playback, cfg.Cache)
		m.mu.Lock()
		if m.closing || gen != m.gen {
			m.mu.Unlock()
			m.stop(s) // superseded before it was shown
			return
		}
		m.cur = s
		m.mu.Unlock()
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

// dial makes a client for srv and checks it answers (ping, auth fallback).
func dial(ctx context.Context, srv config.Server) (*subsonic.Client, *subsonic.ServerInfo, error) {
	c, err := subsonic.New(subsonic.Options{
		BaseURL: srv.URL,
		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token,
			Salt: srv.Salt, APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
	})
	if err != nil {
		return nil, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	info, err := c.Connect(cctx)
	if err != nil {
		return nil, nil, err
	}
	return c, info, nil
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

// serverDir is where a server's data lives: servers/<name> in the data
// folder, with the name made safe for a file name.
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
	dir := filepath.Join(m.dataDir, "servers", safe)
	os.MkdirAll(dir, 0o755)
	return dir
}

// stop ends a session: the player saves its queue and stops the engine,
// then the art loader closes. The session's volume carries to the next.
func (m *sessions) stop(s *session) {
	vol := s.pl.State().VolumeDB
	s.cancel()
	<-s.done
	s.loader.Close()
	m.mu.Lock()
	m.volume = vol
	m.mu.Unlock()
}

// close stops the current session and any connect still in flight from
// starting one (on exit).
func (m *sessions) close() {
	m.mu.Lock()
	m.closing = true
	s := m.cur
	m.cur = nil
	m.mu.Unlock()
	if s != nil {
		m.stop(s)
	}
}
