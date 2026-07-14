// Package health serves the liveness/readiness surface shared by the Pivox Go
// binaries.
//
//   - /healthz — LIVENESS. Never touches a dependency: if it checked Postgres, a
//     DB blip would fail liveness on every replica at once and the orchestrator
//     would restart the whole fleet.
//   - /readyz — READINESS. The dependencies THIS process needs (they differ; the
//     Storage Agent has no Postgres). Failing it pulls the instance from the load
//     balancer without restarting it.
//
// Start the server BEFORE constructing dependencies, then SetChecks once they
// are up — a fresh State reports not-ready with a reason, so a wedged startup
// answers "not ready, and why" rather than refusing connections.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrStarting is the readiness error of a State whose checks have not been
// installed yet — i.e. the process is still booting.
var ErrStarting = errors.New("starting")

// checkTimeout bounds a single readiness probe, so a hung dependency cannot hang
// the readiness handler itself.
const checkTimeout = 3 * time.Second

// Check is one named readiness dependency. Name appears in the /readyz body.
type Check struct {
	Name string
	Func func(ctx context.Context) error
}

// State is the single source of truth for readiness, shared by every adapter that
// reports it (the HTTP handlers here and the gRPC service in grpc.go) so the two
// cannot disagree. Safe for concurrent use.
type State struct {
	mu sync.RWMutex
	// configured separates "not booted yet" (not ready) from "no external
	// dependencies" (ready). len(checks)==0 would conflate them.
	configured bool
	checks     []Check
}

// NewState returns a State that is NOT ready until SetChecks is called.
func NewState() *State {
	return &State{}
}

// SetChecks installs the readiness dependencies. With no checks it means "ready
// once serving" — correct for a process with no external dependencies.
func (s *State) SetChecks(checks ...Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = checks
	s.configured = true
}

// NotReadyError reports which dependencies failed.
//
// Names and payloads are kept separate: a dependency's raw error is unsafe in a
// response body (pgconn's connect error embeds the DSN — db user, db name,
// internal host, port). Names() is what a body may show; the wrapped error is for
// the log. /readyz is no longer ingressed, but this must survive someone
// re-adding the route.
type NotReadyError struct {
	names []string
	err   error
}

// Names returns the failing dependency names. Safe to expose.
func (e *NotReadyError) Names() []string { return e.names }

// Error is the name list only, so an accidental %v leaks nothing.
func (e *NotReadyError) Error() string {
	return "not ready: " + strings.Join(e.names, ", ")
}

// Unwrap exposes the underlying check errors, with their full text, for logging.
func (e *NotReadyError) Unwrap() error { return e.err }

// Ready runs every check and returns nil when all pass, otherwise a
// *NotReadyError aggregating ALL failures (not just the first).
func (s *State) Ready(ctx context.Context) error {
	s.mu.RLock()
	configured, checks := s.configured, s.checks
	s.mu.RUnlock()

	if !configured {
		return ErrStarting
	}

	// Checks run WITHOUT the lock held, so a slow dependency cannot wedge
	// SetChecks or another reader.
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	var (
		names    []string
		failures []error
	)
	for _, c := range checks {
		if err := c.Func(ctx); err != nil {
			names = append(names, c.Name)
			failures = append(failures, fmt.Errorf("%s: %w", c.Name, err))
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &NotReadyError{names: names, err: errors.Join(failures...)}
}

// Config configures a Server.
type Config struct {
	// Addr is the listen address (e.g. ":9090"). Required.
	Addr string
	// State is the readiness source. Required. Share it with any other adapter
	// reporting the same process's health.
	State *State
	// Logger is optional; slog.Default() is used when nil.
	Logger *slog.Logger
}

// Server serves /healthz and /readyz.
type Server struct {
	state  *State
	logger *slog.Logger
	http   *http.Server
	ln     net.Listener
}

// NewServer builds the health server. Panics on missing required config.
func NewServer(cfg Config) *Server {
	if cfg.Addr == "" {
		panic("health: Config.Addr is required")
	}
	if cfg.State == nil {
		panic("health: Config.State is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{state: cfg.State, logger: logger}

	mux := http.NewServeMux()
	// Liveness. Dependency-free by construction — see the package doc.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	// Readiness.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		err := s.state.Ready(r.Context())
		if err == nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}

		// Names only in the body; full detail to the log. See NotReadyError.
		var notReady *NotReadyError
		switch {
		case errors.As(err, &notReady):
			s.logger.WarnContext(r.Context(), "not ready",
				"failed", notReady.Names(),
				"error", errors.Unwrap(notReady),
			)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, "%s\n", oneLine(notReady.Error()))
		default:
			// ErrStarting: a fixed, payload-free string.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, "not ready: %s\n", oneLine(err.Error()))
		}
	})

	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.http.Addr = cfg.Addr
	return s
}

// Start binds SYNCHRONOUSLY, then serves in the background — a port conflict must
// be a loud boot error, not a silently-absent health endpoint.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("health: listen on %s: %w", s.http.Addr, err)
	}
	s.ln = ln

	go func() {
		s.logger.Info("health server listening", "addr", ln.Addr().String())
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("health server stopped", "error", err)
		}
	}()
	return nil
}

// Addr is the bound address; only meaningful after Start.
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.http.Addr
	}
	return s.ln.Addr().String()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// oneLine flattens newlines so nothing can inject extra lines into the response.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ")
}
