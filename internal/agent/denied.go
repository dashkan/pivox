package agent

import (
	"path/filepath"
	"sync"
)

// DeniedPatterns holds glob patterns for denied storage paths.
// Requests matching any pattern are rejected with 404.
// Thread-safe for concurrent reads (HTTP requests) and writes (bidi updates).
//
// Currently in-memory only. SQLite persistence will be added for crash
// resilience when the agent local store is implemented.
type DeniedPatterns struct {
	mu       sync.RWMutex
	patterns []string
}

// NewDeniedPatterns creates an empty DeniedPatterns store.
func NewDeniedPatterns() *DeniedPatterns {
	return &DeniedPatterns{}
}

// Update replaces the entire denied patterns set.
func (d *DeniedPatterns) Update(patterns []string) {
	d.mu.Lock()
	d.patterns = patterns
	d.mu.Unlock()
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
