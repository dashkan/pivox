package health

import (
	"context"
	"errors"
	"sync/atomic"
)

// Flag is a readiness Check backed by a boolean the caller flips, for a
// dependency that is a long-lived connection rather than something you can probe
// on demand — e.g. the Storage Agent's bidi control stream.
//
// Starts FALSE: a link that never came up must not read as ready. Concurrent-safe.
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
// Readiness ONLY, never liveness: a remote link feeding liveness would restart
// the whole fleet on one remote outage.
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
