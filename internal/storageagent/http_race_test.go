package storageagent

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestHTTPServer_ConfigRace exercises the real production interleaving: the
// Connect goroutine calls SetSigningKey / SetCORSOrigin on every successful
// (re-)handshake, while HTTP handler goroutines read those fields to validate
// session JWTs and to write the CORS header.
//
// Unsynchronized, this is a genuine data race — and its consequence is worse
// than a race detector warning: a torn read of the key slice means the HMAC is
// computed over garbage, so every session is rejected while the agent still
// reports itself ready. Reconnects make it live in production, not theoretical.
//
// Run with -race.
func TestHTTPServer_ConfigRace(t *testing.T) {
	t.Parallel()

	// Reuse the shared fixture helper rather than re-wiring the stores here.
	srv, _, _, _ := newTestHTTPServer(t)

	var wg sync.WaitGroup

	// Writers: the reconnect loop re-applying handshake config.
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				srv.SetSigningKey([]byte{byte(i), 'k', 'e', 'y'})
				srv.SetCORSOrigin("https://example.test")
			}
		}()
	}

	// Readers: concurrent requests validating sessions + writing CORS headers.
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				req := httptest.NewRequest(http.MethodGet, "/files/x?token=not.a.jwt", nil)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
			}
		}()
	}

	wg.Wait()
}
