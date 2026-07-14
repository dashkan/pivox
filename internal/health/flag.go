package health

import (
	"context"
	"errors"
	"sync/atomic"
)

// Flag is a readiness Check backed by a boolean the caller flips — for a
// dependency that is a long-lived connection rather than something you can
// probe on demand.
//
// The Storage Agent's control-plane stream is the motivating case: you cannot
// "ping" a bidi gRPC stream, but the agent knows exactly when it is up (after
// the handshake) and when it is gone (when the stream returns).
//
// Starts FALSE. A link that has never come up must not read as ready.
//
// Safe for concurrent use.
type Flag struct {
	name   string
	reason string
	ok     atomic.Bool
}

// NewFlag returns a Flag that reports not-ready with reason until Set(true).
func NewFlag(name, reason string) *Flag {
	return &Flag{name: name, reason: reason}
}

// Set records whether the dependency is currently up.
func (f *Flag) Set(ok bool) {
	f.ok.Store(ok)
}

// Check adapts the Flag to a readiness Check.
//
// Wire this into readiness ONLY — never into liveness. Liveness is
// dependency-free by construction (see the package doc): if a remote link fed
// liveness, one remote outage would fail liveness on every replica at once and
// the orchestrator would restart the whole fleet, amplifying someone else's
// outage into ours. Readiness pulls a disconnected instance out of rotation and
// leaves it running to reconnect.
func (f *Flag) Check() Check {
	return Check{
		Name: f.name,
		Func: func(context.Context) error {
			if f.ok.Load() {
				return nil
			}
			return errors.New(f.reason)
		},
	}
}
