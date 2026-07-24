// Package attachlink provides a short-lived, loopback-only capability for
// focusing an existing tmux client on one exact pane. It intentionally exposes
// no general command or remote-control surface.
package attachlink

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

const (
	DefaultLifetime = 15 * time.Minute
	tokenBytes      = 16 // 128 bits of entropy
	attachPrefix    = "/attach/"
	maxPageBytes    = 16 * 1024
)

var (
	ErrInvalidTarget = errors.New("invalid attach target")
	ErrInvalidConfig = errors.New("invalid attach server configuration")
)

// Tmux is the deliberately narrow tmux capability required by an attach link.
// Implementations must not expose arbitrary command execution to this package.
type Tmux interface {
	PaneExists(string) (bool, error)
	ListClients() ([]tmuxcli.Client, error)
	SwitchClient(string, string) error
}

// Server is one single-target, single-token loopback listener.
type Server struct {
	listener net.Listener
	http     *http.Server
	tmux     Tmux
	target   string
	token    string
	route    string
	url      string

	activationMu sync.Mutex
	consumed     bool
	stopOnce     sync.Once
	done         chan struct{}
}

// Listen binds an ephemeral IPv4 loopback port and starts a bounded attach
// server. The returned URL contains only the loopback address and opaque token.
func Listen(ctx context.Context, target string, tmux Tmux, lifetime time.Duration) (*Server, error) {
	if !tmuxcli.ValidPaneID(target) {
		return nil, ErrInvalidTarget
	}
	if tmux == nil || lifetime <= 0 {
		return nil, ErrInvalidConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("attach listener: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	s := &Server{
		listener: listener,
		tmux:     tmux,
		target:   target,
		token:    token,
		route:    attachPrefix + token,
		url:      "http://" + listener.Addr().String() + attachPrefix + token,
		done:     make(chan struct{}),
	}
	s.http = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 * 1024,
	}

	go s.serve()
	go s.expire(ctx, lifetime)
	return s, nil
}

func randomToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("attach token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// URL returns the complete opaque loopback URL to place in a notification.
func (s *Server) URL() string {
	if s == nil {
		return ""
	}
	return s.url
}

// Close stops the listener. It is safe to call more than once.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		_ = s.listener.Close()
		_ = s.http.Close()
		close(s.done)
	})
}

// Wait blocks until the listener has stopped.
func (s *Server) Wait() {
	if s != nil {
		<-s.done
	}
}

func (s *Server) serve() {
	if err := s.http.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		// The listener is already unusable. Stop still closes done exactly once.
		s.Close()
	}
}

func (s *Server) expire(ctx context.Context, lifetime time.Duration) {
	timer := time.NewTimer(lifetime)
	defer timer.Stop()
	select {
	case <-timer.C:
		s.Close()
	case <-ctx.Done():
		s.Close()
	case <-s.done:
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.Host != s.listener.Addr().String() || r.URL.RawQuery != "" {
		writePlain(w, http.StatusNotFound, "Not found")
		return
	}

	token, ok := requestToken(r.URL.Path)
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		writePlain(w, http.StatusNotFound, "Not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGET(w)
	case http.MethodPost:
		s.handlePOST(w)
	default:
		w.Header().Set("Allow", "GET, POST")
		writePlain(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func requestToken(path string) (string, bool) {
	if !strings.HasPrefix(path, attachPrefix) {
		return "", false
	}
	token := strings.TrimPrefix(path, attachPrefix)
	if token == "" || strings.Contains(token, "/") || !isURLSafeToken(token) {
		return "", false
	}
	return token, true
}

func isURLSafeToken(token string) bool {
	for _, r := range token {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) handleGET(w http.ResponseWriter) {
	nonce, err := randomToken()
	if err != nil {
		writePlain(w, http.StatusServiceUnavailable, "Attach is temporarily unavailable")
		return
	}
	// GET is intentionally inert. The form and script issue the state-changing
	// POST only after a browser has loaded the page.
	formAction := html.EscapeString(s.route)
	nonceHTML := html.EscapeString(nonce)
	body := "<!doctype html><html><head><meta charset=\"utf-8\"><title>Attach in tmux</title></head>" +
		"<body><p>Attach this browser's existing tmux client?</p>" +
		"<form method=\"post\" action=\"" + formAction + "\"><button type=\"submit\">Attach in tmux</button></form>" +
		"<script nonce=\"" + nonceHTML + "\">document.forms[0].submit()</script></body></html>"
	if len(body) > maxPageBytes {
		writePlain(w, http.StatusInternalServerError, "Attach is temporarily unavailable")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; script-src 'nonce-"+nonce+"'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (s *Server) handlePOST(w http.ResponseWriter) {
	s.activationMu.Lock()
	defer s.activationMu.Unlock()

	if s.consumed {
		writePlain(w, http.StatusGone, "This attach link is no longer available")
		return
	}

	exists, err := s.tmux.PaneExists(s.target)
	if err != nil {
		writePlain(w, http.StatusServiceUnavailable, "tmux is temporarily unavailable")
		return
	}
	if !exists {
		writePlain(w, http.StatusGone, "The tmux session is no longer available")
		s.consumed = true
		go s.Close()
		return
	}

	clients, err := s.tmux.ListClients()
	if err != nil {
		writePlain(w, http.StatusServiceUnavailable, "tmux is temporarily unavailable")
		return
	}
	client, ok := tmuxcli.MostRecentClient(clients)
	if !ok {
		writePlain(w, http.StatusConflict, "An existing tmux client is required")
		return
	}
	if err := s.tmux.SwitchClient(client.Name, s.target); err != nil {
		writePlain(w, http.StatusServiceUnavailable, "tmux is temporarily unavailable")
		return
	}

	s.consumed = true
	writePlain(w, http.StatusOK, "Attached. Return to Ghostty.")
	go s.Close()
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func writePlain(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ProductionTmux returns the narrow system tmux capability used by the
// helper process. It cannot execute arbitrary commands through this package.
func ProductionTmux() Tmux { return tmuxAdapter{} }

// Ensure the compile-time contract remains explicit if tmuxcli changes its
// wrappers in a future refactor.
var _ Tmux = tmuxAdapter{}

type tmuxAdapter struct{}

func (tmuxAdapter) PaneExists(target string) (bool, error) { return tmuxcli.PaneExists(target) }
func (tmuxAdapter) ListClients() ([]tmuxcli.Client, error) { return tmuxcli.ListClients() }
func (tmuxAdapter) SwitchClient(client, pane string) error {
	return tmuxcli.SwitchClient(client, pane)
}
