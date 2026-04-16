package aichat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"ab", 1},
		{"abc", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"Hello, world!", 4},       // 13 chars -> (13+3)/4 = 4
		{"The quick brown fox", 5}, // 19 chars -> (19+3)/4 = 5
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, estimateTokens(tt.input))
		})
	}
}
