package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClampRiverPollInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"below min clamps up", 5 * time.Second, minRiverPollInterval},
		{"zero clamps up", 0, minRiverPollInterval},
		{"negative clamps up", -1 * time.Minute, minRiverPollInterval},
		{"exactly min", minRiverPollInterval, minRiverPollInterval},
		{"in range (default)", defaultRiverPollInterval, defaultRiverPollInterval},
		{"exactly max", maxRiverPollInterval, maxRiverPollInterval},
		{"above max clamps down", 30 * time.Minute, maxRiverPollInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, clampRiverPollInterval(tt.in))
		})
	}
}

// Guard the invariant the caps encode: default sits within [min, max].
func TestRiverPollIntervalBounds(t *testing.T) {
	t.Parallel()
	assert.LessOrEqual(t, minRiverPollInterval, defaultRiverPollInterval)
	assert.LessOrEqual(t, defaultRiverPollInterval, maxRiverPollInterval)
	assert.Equal(t, 30*time.Second, minRiverPollInterval)
	assert.Equal(t, 10*time.Minute, maxRiverPollInterval)
	assert.Equal(t, 5*time.Minute, defaultRiverPollInterval)
}
