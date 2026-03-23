package agent

import (
	"context"
	"path"
	"strings"
	"sync"
	"time"
)

// Session represents a user's access grant for this gateway.
type Session struct {
	Patterns []string
	Expiry   time.Time
}

// SessionStore is a thread-safe in-memory store of opaque session tokens
// mapped to access patterns. Populated via bidi SessionGrant messages,
// cleaned up periodically.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session // key: opaque token
}

// NewSessionStore creates a new empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

// Grant stores a session. Overwrites if token already exists (pattern update).
func (s *SessionStore) Grant(token string, patterns []string, expiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = &Session{
		Patterns: patterns,
		Expiry:   expiry,
	}
}

// Revoke removes a session immediately.
func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// Authorize checks if the given token grants access to the request path.
// Returns true if any pattern matches. Returns false if token not found or expired.
func (s *SessionStore) Authorize(token string, requestPath string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(sess.Expiry) {
		return false
	}
	for _, p := range sess.Patterns {
		if matchPattern(p, requestPath) {
			return true
		}
	}
	return false
}

// FlushExpired removes all sessions past their expiry time.
func (s *SessionStore) FlushExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if now.After(sess.Expiry) {
			delete(s.sessions, token)
		}
	}
}

// StartCleanup runs FlushExpired on a timer. Cancel via context.
func (s *SessionStore) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.FlushExpired()
		}
	}
}

// matchPattern checks whether requestPath matches the given pattern.
// If the pattern ends with /*, a prefix match is performed to support
// recursive wildcards. Otherwise path.Match is used for single-segment
// glob matching.
func matchPattern(pattern, requestPath string) bool {
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(requestPath, prefix+"/") || requestPath == prefix
	}
	matched, _ := path.Match(pattern, requestPath)
	return matched
}
