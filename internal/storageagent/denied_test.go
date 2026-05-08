package storageagent

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeniedPatterns_Empty(t *testing.T) {
	t.Parallel()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})

	assert.False(t, dp.IsDenied("/any/path"))
	assert.False(t, dp.IsDenied(""))
	assert.False(t, dp.IsDenied("file.txt"))
}

func TestIsDenied_Match(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})
	require.NoError(t, dp.Update(ctx, []string{"secret.txt", "*.key"}))

	assert.True(t, dp.IsDenied("secret.txt"))
	assert.True(t, dp.IsDenied("server.key"))
}

func TestIsDenied_NoMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})
	require.NoError(t, dp.Update(ctx, []string{"secret.txt", "*.key"}))

	assert.False(t, dp.IsDenied("public.txt"))
	assert.False(t, dp.IsDenied("readme.md"))
	assert.False(t, dp.IsDenied("/some/path/file.mp4"))
}

func TestUpdate_Replace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})

	require.NoError(t, dp.Update(ctx, []string{"old_pattern.txt"}))
	assert.True(t, dp.IsDenied("old_pattern.txt"))

	// Replace with entirely new patterns.
	require.NoError(t, dp.Update(ctx, []string{"new_pattern.txt"}))

	assert.False(t, dp.IsDenied("old_pattern.txt"),
		"old pattern should no longer be denied after update")
	assert.True(t, dp.IsDenied("new_pattern.txt"),
		"new pattern should be denied after update")
}

func TestIsDenied_GlobPatterns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})
	require.NoError(t, dp.Update(ctx,
		[]string{"*.tmp", "archive-*", "backup_???"}))

	tests := []struct {
		path    string
		denied  bool
		comment string
	}{
		{"upload.tmp", true, "*.tmp should match"},
		{"data.tmp", true, "*.tmp should match"},
		{"data.txt", false, "*.tmp should not match .txt"},
		{"archive-2024", true, "archive-* should match"},
		{"archive-old", true, "archive-* should match"},
		{"myarchive-2024", false, "archive-* should not match without prefix"},
		{"backup_001", true, "backup_??? should match 3-char suffix"},
		{"backup_ab", false, "backup_??? should not match 2-char suffix"},
		{"backup_abcd", false, "backup_??? should not match 4-char suffix"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.denied, dp.IsDenied(tt.path), tt.comment)
		})
	}
}

func TestDeniedConcurrentAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dp := NewDeniedPatterns(DeniedPatternsConfig{})
	require.NoError(t, dp.Update(ctx, []string{"*.tmp", "secret.*"}))

	var wg sync.WaitGroup
	const goroutines = 100

	// Concurrent reads.
	for range goroutines {
		wg.Go(func() {
			dp.IsDenied("file.tmp")
			dp.IsDenied("public.txt")
		})
	}

	// Concurrent writes. Errors discarded — no Store attached so the
	// only failure path can't fire. Same convention as
	// TestSessionConcurrentAccess.
	for i := range goroutines / 2 {
		wg.Go(func() {
			if i%2 == 0 {
				_ = dp.Update(ctx, []string{"*.tmp", "secret.*"})
			} else {
				_ = dp.Update(ctx, []string{"*.log"})
			}
		})
	}

	wg.Wait()
	// If we get here without a data race, concurrency is safe.
}
