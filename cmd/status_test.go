package cmd

import (
	"testing"
	"time"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 10, ""},
		{"string shorter than n", "hi", 10, "hi"},
		{"string exactly n", "hello", 5, "hello"},
		{"string longer than n", "toolong", 6, "too..."},
		{"unicode multi-byte runes", "héllo wörld", 8, "héllo..."},
		{"edge case n=0", "toolong", 0, "toolong"},
		{"edge case n=1", "toolong", 1, "toolong"},
		{"edge case n=2", "toolong", 2, "toolong"},
		{"edge case n=3", "toolong", 3, "toolong"},
		{"n=4 truncates", "toolong", 4, "t..."},
		{"long string", "this is a very long string", 10, "this is..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero duration", 0, "0s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"59 seconds", 59 * time.Second, "59s"},
		{"1 minute", time.Minute, "1m"},
		{"59 minutes", 59 * time.Minute, "59m"},
		{"1 hour", time.Hour, "1.0h"},
		{"23.9 hours", time.Duration(23.9 * float64(time.Hour)), "23.9h"},
		{"24 hours", 24 * time.Hour, "1.0d"},
		{"48 hours", 48 * time.Hour, "2.0d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanDuration(tt.d)
			if got != tt.want {
				t.Errorf("humanDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
