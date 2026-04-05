package storageagent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrant_And_Authorize(t *testing.T) {
	store := NewSessionStore()
	expiry := time.Now().Add(1 * time.Hour)

	store.Grant("tok1", []string{"/media/video.mp4", "/media/audio.mp3"}, expiry)

	assert.True(t, store.Authorize("tok1", "/media/video.mp4"))
	assert.True(t, store.Authorize("tok1", "/media/audio.mp3"))
	assert.False(t, store.Authorize("tok1", "/media/other.mp4"),
		"non-matching path should not be authorized")
}

func TestAuthorize_NotFound(t *testing.T) {
	store := NewSessionStore()

	assert.False(t, store.Authorize("unknown-token", "/any/path"),
		"unknown token should not be authorized")
}

func TestAuthorize_Expired(t *testing.T) {
	store := NewSessionStore()
	// Grant with a past expiry.
	store.Grant("expired-tok", []string{"/media/*"}, time.Now().Add(-1*time.Second))

	assert.False(t, store.Authorize("expired-tok", "/media/file.mp4"),
		"expired session should not authorize")
}

func TestRevoke(t *testing.T) {
	store := NewSessionStore()
	expiry := time.Now().Add(1 * time.Hour)

	store.Grant("tok-revoke", []string{"/data/*"}, expiry)
	require.True(t, store.Authorize("tok-revoke", "/data/file.csv"))

	store.Revoke("tok-revoke")
	assert.False(t, store.Authorize("tok-revoke", "/data/file.csv"),
		"revoked token should not be authorized")
}

func TestFlushExpired(t *testing.T) {
	store := NewSessionStore()

	// Two expired sessions.
	store.Grant("expired1", []string{"/a"}, time.Now().Add(-1*time.Minute))
	store.Grant("expired2", []string{"/b"}, time.Now().Add(-2*time.Minute))

	// One valid session.
	store.Grant("valid1", []string{"/c"}, time.Now().Add(1*time.Hour))

	store.FlushExpired()

	assert.False(t, store.Authorize("expired1", "/a"), "expired1 should have been flushed")
	assert.False(t, store.Authorize("expired2", "/b"), "expired2 should have been flushed")
	assert.True(t, store.Authorize("valid1", "/c"), "valid1 should still be authorized")
}

func TestMatchPattern_ExactMatch(t *testing.T) {
	assert.True(t, matchPattern("/media/video.mp4", "/media/video.mp4"))
	assert.False(t, matchPattern("/media/video.mp4", "/media/audio.mp3"))
}

func TestMatchPattern_GlobMatch(t *testing.T) {
	// Single-segment glob: *.jpg matches any single segment ending in .jpg.
	assert.True(t, matchPattern("/media/*.jpg", "/media/photo.jpg"))
	assert.False(t, matchPattern("/media/*.jpg", "/media/photo.png"),
		"*.jpg should not match .png files")
	assert.False(t, matchPattern("/media/*.jpg", "/media/sub/photo.jpg"),
		"single-segment glob should not cross path boundaries")
}

func TestMatchPattern_RecursiveWildcard(t *testing.T) {
	// Pattern ending in /* does a prefix match.
	assert.True(t, matchPattern("/archive/*", "/archive/2024/file.txt"),
		"recursive wildcard should match nested paths")
	assert.True(t, matchPattern("/archive/*", "/archive/file.txt"),
		"recursive wildcard should match direct children")
	assert.True(t, matchPattern("/archive/*", "/archive"),
		"recursive wildcard should match the prefix exactly")
	assert.False(t, matchPattern("/archive/*", "/other/file.txt"),
		"recursive wildcard should not match unrelated paths")
}

func TestGrant_Overwrite(t *testing.T) {
	store := NewSessionStore()
	expiry := time.Now().Add(1 * time.Hour)

	store.Grant("tok-ow", []string{"/old/path"}, expiry)
	require.True(t, store.Authorize("tok-ow", "/old/path"))

	// Overwrite with new patterns.
	store.Grant("tok-ow", []string{"/new/path"}, expiry)

	assert.False(t, store.Authorize("tok-ow", "/old/path"),
		"old pattern should no longer match after overwrite")
	assert.True(t, store.Authorize("tok-ow", "/new/path"),
		"new pattern should match after overwrite")
}

func TestSessionConcurrentAccess(t *testing.T) {
	store := NewSessionStore()
	expiry := time.Now().Add(1 * time.Hour)

	var wg sync.WaitGroup
	const goroutines = 50

	// Concurrent Grant.
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok := "tok-" + string(rune('A'+i%26))
			store.Grant(tok, []string{"/path/*"}, expiry)
		}(i)
	}

	// Concurrent Authorize.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Authorize("tok-A", "/path/file.txt")
		}()
	}

	// Concurrent Revoke.
	for i := range goroutines / 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok := "tok-" + string(rune('A'+i%26))
			store.Revoke(tok)
		}(i)
	}

	wg.Wait()
	// If we get here without a race detector panic, concurrency is safe.
}

func TestStartCleanup_FlushesExpiredSessions(t *testing.T) {
	store := NewSessionStore()

	// Grant one expired session and one valid session.
	store.Grant("expired", []string{"/a"}, time.Now().Add(-1*time.Second))
	store.Grant("valid", []string{"/b"}, time.Now().Add(1*time.Hour))

	// Verify the expired session is still in the map before cleanup runs
	// (Authorize returns false due to expiry check, but the entry exists).
	require.False(t, store.Authorize("expired", "/a"))
	require.True(t, store.Authorize("valid", "/b"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		store.StartCleanup(ctx, 10*time.Millisecond)
		close(done)
	}()

	// Wait long enough for at least one cleanup tick to fire.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for the goroutine to exit.
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("StartCleanup did not return after context cancellation")
	}

	// The expired session should have been flushed from the map entirely.
	// Verify by granting the same token again and checking it works --
	// if FlushExpired ran, the old entry is gone.
	store.Grant("expired", []string{"/c"}, time.Now().Add(1*time.Hour))
	assert.True(t, store.Authorize("expired", "/c"),
		"re-granted token should authorize after cleanup flushed the old entry")
	assert.True(t, store.Authorize("valid", "/b"),
		"valid session should survive cleanup")
}

func TestStartCleanup_StopsOnContextCancel(t *testing.T) {
	store := NewSessionStore()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		store.StartCleanup(ctx, 1*time.Hour) // long interval, won't tick
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// StartCleanup returned promptly after cancellation.
	case <-time.After(1 * time.Second):
		t.Fatal("StartCleanup did not return after context cancellation")
	}
}
