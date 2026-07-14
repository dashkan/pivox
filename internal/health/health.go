// Package health serves the liveness/readiness surface shared by every Pivox
// Go binary (Cloud Controller, Worker Process, Storage Agent).
//
// # Liveness vs readiness
//
// The split is load-bearing, not decorative:
//
//   - /healthz — LIVENESS. "The process is alive and its event loop is not
//     wedged." It NEVER touches a dependency. If liveness checked Postgres, one
//     DB blip would fail liveness on every replica simultaneously and the
//     orchestrator would restart the whole fleet — turning a brief dependency
//     hiccup into a full outage. Restarting a process does not fix someone
//     else's database.
//   - /readyz — READINESS. "I can serve traffic right now": the dependencies
//     THIS process actually needs are reachable. Failing readiness pulls the
//     instance out of the load balancer; it does not restart it.
//
// Readiness is per-process by design — the Storage Agent has no Postgres, so it
// must not be asked to prove it can reach one. Each binary installs its own
// Checks.
//
// # Bind early
//
// Start the server BEFORE constructing dependencies, then SetChecks once they
// are up. A fresh State reports not-ready with a reason ("starting"), so a
// process wedged during startup answers "not ready, and here is why" instead of
// refusing connections. A closed port is a black hole: it is indistinguishable
// from a crashed process, a firewall, and a misconfigured port — which is
// exactly how a non-serving API once looked "Healthy" for 25 minutes.
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

// checkTimeout bounds a single readiness probe. Without it, a hung dependency
// (a TCP connect into a blackhole) would hang the readiness handler itself, and
// a prober that never gets an answer learns nothing — the very failure this
// package exists to make visible.
const checkTimeout = 3 * time.Second

// Check is one named readiness dependency. Name appears in the /readyz body, so
// a failing probe says WHICH dependency is down.
type Check struct {
	Name string
	Func func(ctx context.Context) error
}

// State is the single source of truth for readiness. It is shared by every
// adapter that reports it (the HTTP handlers here, and the gRPC health service
// in grpc.go) so the two can never disagree.
//
// Safe for concurrent use.
type State struct {
	mu sync.RWMutex
	// configured distinguishes "no checks because we haven't booted yet" (not
	// ready) from "no checks because this process genuinely has no external
	// dependencies" (ready) — the Storage Agent's case. A bare len(checks)==0
	// test would conflate them and report a still-booting process as ready.
	configured bool
	checks     []Check
}

// NewState returns a State that is NOT ready until SetChecks is called.
func NewState() *State {
	return &State{}
}

// SetChecks installs the readiness dependencies and marks the process
// configured. Calling it with no checks is an explicit "ready once serving",
// which is the correct answer for a process with no external dependencies.
func (s *State) SetChecks(checks ...Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = checks
	s.configured = true
}

// NotReadyError reports which dependencies failed.
//
// Names and payloads are kept SEPARATE on purpose. A dependency's raw error is
// not safe to put in a response body: pgconn's connect error embeds the DSN —
// database user, database name, internal host, port. Names() is what a body may
// show; the wrapped error (the full text) is for the LOG, where the operator can
// see it and nobody else can.
//
// This holds even though /readyz is no longer routed through the public ingress
// (configs/agentgateway.yaml exposes /healthz only). It WAS routed there, which
// is how this came to light — so this is the inner layer of a two-layer defense,
// and it must survive someone re-adding the route.
type NotReadyError struct {
	// names of the checks that failed, in declaration order.
	names []string
	// err is the joined underlying errors — detail for logs only.
	err error
}

// Names returns the failing dependency names. Safe to expose.
func (e *NotReadyError) Names() []string { return e.names }

// Error is the name list only — so an accidental %v in a public surface still
// leaks nothing. Use Unwrap()/errors.Unwrap for the detail.
func (e *NotReadyError) Error() string {
	return "not ready: " + strings.Join(e.names, ", ")
}

// Unwrap exposes the underlying check errors, with their full text, for logging.
func (e *NotReadyError) Unwrap() error { return e.err }

// Ready runs every check and returns nil when all pass, otherwise a
// *NotReadyError. It aggregates ALL failures rather than returning the first:
// when a process is unready, knowing that both Postgres and Kafka are down is
// materially different from knowing only about Postgres.
func (s *State) Ready(ctx context.Context) error {
	s.mu.RLock()
	configured, checks := s.configured, s.checks
	s.mu.RUnlock()

	if !configured {
		return ErrStarting
	}

	// Checks run WITHOUT the lock held (the slice was copied above), so a slow
	// dependency cannot wedge SetChecks or another reader.
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

// NewServer builds the debug/health server. Panics on missing required config —
// a startup-time programmer error should fail loudly on boot, not mid-incident
// when someone finally probes the endpoint.
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
	// Liveness. Deliberately dependency-free — see the package doc.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Best-effort write: a client that hung up before we finished is not
		// actionable for a health endpoint.
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

		// Echo the failing dependency NAMES — enough to make an incident legible —
		// and nothing else. A dependency's raw error is not safe in a body: pgconn's
		// connect error embeds the DSN (db user, db name, internal host, port).
		//
		// /readyz is no longer routed through the public ingress (it was, which is
		// how this was caught), but this stays: it is the layer that survives someone
		// re-adding the route. The full detail goes to the LOG instead.
		var notReady *NotReadyError
		switch {
		case errors.As(err, &notReady):
			s.logger.WarnContext(r.Context(), "not ready",
				"failed", notReady.Names(),
				// Unwrap() carries the full check errors — log-only.
				"error", errors.Unwrap(notReady),
			)
			w.WriteHeader(http.StatusServiceUnavailable)
			// notReady.Error() is the name list only, by construction.
			_, _ = fmt.Fprintf(w, "%s\n", oneLine(notReady.Error()))
		default:
			// ErrStarting (or anything else): a fixed, payload-free string.
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintf(w, "not ready: %s\n", oneLine(err.Error()))
		}
	})

	s.http = &http.Server{
		Handler: mux,
		// A health endpoint must never be the thing that leaks connections.
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.http.Addr = cfg.Addr
	return s
}

// Start binds the listener SYNCHRONOUSLY and then serves in the background. The
// bind is synchronous on purpose: a port conflict must be a loud boot error, not
// a silently-absent health endpoint that the operator only discovers when a
// probe times out during an outage.
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

// Addr is the bound address. Only meaningful after Start (and the only way to
// learn the port when Addr was given as :0).
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

// oneLine flattens newlines so a dependency's multi-line error can't inject
// extra lines into the response body. errors.Join separates with "\n".
func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ")
}
