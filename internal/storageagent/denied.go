package storageagent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// DeniedPatterns holds glob patterns for denied storage paths.
// Requests matching any pattern are rejected with 404.
// Thread-safe for concurrent reads (HTTP requests) and writes (bidi
// updates).
//
// When constructed with a non-nil DeniedPatternsConfig.Store, every
// Update call mirrors to the SQLite store atomically with the
// in-memory replacement. Boot-time reload is the caller's
// responsibility via LoadFromStore — see #79 for the broader
// crash-resilience flow. The package only exposes the contract; the
// boot wiring lives in cmd/pivox-agent/.
//
// Persistence semantics: writes are atomic with the in-memory update.
// If the SQLite write fails, the in-memory set is NOT replaced and
// the caller receives an error. This matches SessionStore.Grant's
// shape — divergence between in-memory and disk would silently break
// crash resilience.
type DeniedPatterns struct {
	mu       sync.RWMutex
	patterns []string

	// persist is set at construction via DeniedPatternsConfig.Store.
	// nil = in-memory only.
	persist *Store
}

// DeniedPatternsConfig is the constructor input for DeniedPatterns.
type DeniedPatternsConfig struct {
	// Store, if non-nil, makes every Update mirror to SQLite
	// atomically with the in-memory replacement. Optional.
	// Zero-value config = in-memory only.
	Store *Store
}

// NewDeniedPatterns constructs a DeniedPatterns from cfg. A nil
// cfg.Store produces an in-memory-only set.
func NewDeniedPatterns(cfg DeniedPatternsConfig) *DeniedPatterns {
	return &DeniedPatterns{
		persist: cfg.Store,
	}
}

// LoadFromStore reads the persisted denied set into memory. Used at
// agent boot, before the HTTP listener starts. No-op without an
// attached store.
func (d *DeniedPatterns) LoadFromStore(ctx context.Context) error {
	if d.persist == nil {
		return nil
	}
	rows, err := d.persist.LoadDeniedPatterns(ctx)
	if err != nil {
		return fmt.Errorf("load denied patterns: %w", err)
	}
	d.mu.Lock()
	d.patterns = rows
	d.mu.Unlock()
	return nil
}

// Update replaces the entire denied-patterns set. Atomic with
// persistence: if a store is attached and the persist call fails,
// the in-memory set is left untouched and the error is returned.
//
// nil and an empty slice both clear the set.
func (d *DeniedPatterns) Update(ctx context.Context, patterns []string) error {
	// Persist FIRST under the write lock — same pattern as
	// SessionStore.Grant. The Store layer's ReplaceDeniedPatterns
	// runs in its own SQLite transaction (see persist.go); the Go
	// lock here only serializes Update vs. the in-memory swap.
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.persist != nil {
		if err := d.persist.ReplaceDeniedPatterns(ctx, patterns); err != nil {
			return fmt.Errorf("update denied patterns: %w", err)
		}
	}
	d.patterns = patterns
	return nil
}

// IsDenied checks if the given path matches any denied pattern.
func (d *DeniedPatterns) IsDenied(path string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, pattern := range d.patterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
	}
	return false
}
