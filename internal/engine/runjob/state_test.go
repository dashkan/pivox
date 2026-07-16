package runjob

import "testing"

func TestIsValidState(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{"pending", StatePending, true},
		{"running", StateRunning, true},
		{"waiting", StateWaiting, true},
		{"succeeded", StateSucceeded, true},
		{"failed", StateFailed, true},
		{"cancelled", StateCancelled, true},
		{"empty", "", false},
		{"unspecified", "STATE_UNSPECIFIED", false},
		{"lowercase", "running", false},
		{"unknown", "BOGUS", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidState(tt.state); got != tt.want {
				t.Errorf("IsValidState(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
