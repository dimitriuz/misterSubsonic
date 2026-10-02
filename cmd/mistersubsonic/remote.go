package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	"mistersubsonic/internal/remote"
	"mistersubsonic/internal/webguard"
)

// remoteHost starts and stops the web remote's server (Settings → Remote
// switches it, and the config starts it). It is the UI's RemoteSwitch and its
// RemoteNotifier: Notify goes to whichever server is running.
type remoteHost struct {
	ctl  remote.Controller // set once, before the first SetEnabled
	port int
	bind string // address to listen on; default 0.0.0.0 (tests and the e2e use 127.0.0.1)

	mu  sync.Mutex // serialises SetEnabled and Close
	srv *remote.Server
	cur atomic.Pointer[remote.Server] // srv, for Notify and URLs from any goroutine
}

// SetEnabled starts the server on <bind>:<port> (every interface unless
// bind says otherwise) or stops it. Starting a
// running server, or stopping a stopped one, does nothing. The error of a
// failed start is short and safe to show on the TV.
func (h *remoteHost) SetEnabled(on bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !on {
		h.stopLocked()
		return nil
	}
	if h.srv != nil {
		return nil
	}
	srv := remote.New(h.ctl, remote.Options{Hostnames: webguard.Hostnames(), Log: log.Printf})
	if err := srv.Listen(h.listenAddr()); err != nil {
		log.Printf("remote: %v", err)
		if errors.Is(err, syscall.EADDRINUSE) {
			return fmt.Errorf("port %d is in use", h.port)
		}
		return fmt.Errorf("can't listen on port %d", h.port)
	}
	h.srv = srv
	h.cur.Store(srv)
	log.Printf("remote: listening on %v", srv.URLs())
	return nil
}

func (h *remoteHost) listenAddr() string {
	bind := h.bind
	if bind == "" {
		bind = "0.0.0.0"
	}
	return net.JoinHostPort(bind, strconv.Itoa(h.port))
}

func (h *remoteHost) stopLocked() {
	if h.srv == nil {
		return
	}
	h.cur.Store(nil)
	if err := h.srv.Close(); err != nil {
		log.Printf("remote: %v", err)
	}
	h.srv = nil
	log.Printf("remote: stopped")
}

// URLs are the addresses the running server answers on (none when stopped).
func (h *remoteHost) URLs() []string {
	if s := h.cur.Load(); s != nil {
		return s.URLs()
	}
	return nil
}

// Running is whether the server is listening.
func (h *remoteHost) Running() bool { return h.cur.Load() != nil }

// Notify tells the running server that the player or the queue changed.
func (h *remoteHost) Notify(c remote.Change) {
	if s := h.cur.Load(); s != nil {
		s.Notify(c)
	}
}

// Close stops the server (on exit, before the player).
func (h *remoteHost) Close() { h.SetEnabled(false) }

// toaster is what startRemote needs of the UI (*ui.App).
type toaster interface {
	Post(f func())
	Toast(format string, args ...any)
}

// startRemote starts the server at launch. A failure (the port is taken) is
// logged and shown as a toast; the app carries on without the remote.
func startRemote(h *remoteHost, a toaster) {
	if err := h.SetEnabled(true); err != nil {
		a.Post(func() { a.Toast("Remote: %v", err) })
	}
}
