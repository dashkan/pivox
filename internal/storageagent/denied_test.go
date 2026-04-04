package storageagent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDeniedPatterns_Empty(t *testing.T) {
	dp := NewDeniedPatterns()

	assert.False(t, dp.IsDenied("/any/path"))
	assert.False(t, dp.IsDenied(""))
	assert.False(t, dp.IsDenied("file.txt"))
}

func TestIsDenied_Match(t *testing.T) {
	dp := NewDeniedPatterns()
	dp.Update([]string{"secret.txt", "*.key"})

	assert.True(t, dp.IsDenied("secret.txt"))
	assert.True(t, dp.IsDenied("server.key"))
}

func TestIsDenied_NoMatch(t *testing.T) {
	dp := NewDeniedPatterns()
	dp.Update([]string{"secret.txt", "*.key"})

	assert.False(t, dp.IsDenied("public.txt"))
	assert.False(t, dp.IsDenied("readme.md"))
	assert.False(t, dp.IsDenied("/some/path/file.mp4"))
}

func TestUpdate_Replace(t *testing.T) {
	dp := NewDeniedPatterns()

	dp.Update([]string{"old_pattern.txt"})
	assert.True(t, dp.IsDenied("old_pattern.txt"))

	// Replace with entirely new patterns.
	dp.Update([]string{"new_pattern.txt"})

	assert.False(t, dp.IsDenied("old_pattern.txt"),
		"old pattern should no longer be denied after update")
	assert.True(t, dp.IsDenied("new_pattern.txt"),
		"new pattern should be denied after update")
}

func TestIsDenied_GlobPatterns(t *testing.T) {
	dp := NewDeniedPatterns()
	dp.Update([]string{"*.tmp", "archive-*", "backup_???"})

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
			assert.Equal(t, tt.denied, dp.IsDenied(tt.path), tt.comment)
		})
	}
}

func TestDeniedConcurrentAccess(t *testing.T) {
	dp := NewDeniedPatterns()
	dp.Update([]string{"*.tmp", "secret.*"})

	var wg sync.WaitGroup
	const goroutines = 100

	// Concurrent reads.
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dp.IsDenied("file.tmp")
			dp.IsDenied("public.txt")
		}()
	}

	// Concurrent writes.
	for i := range goroutines / 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				dp.Update([]string{"*.tmp", "secret.*"})
			} else {
				dp.Update([]string{"*.log"})
			}
		}(i)
	}

	wg.Wait()
	// If we get here without a data race, concurrency is safe.
}
