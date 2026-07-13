package main

import (
	"testing"
	"time"
)

func TestFormatVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 14, 8, 32, 7, 0, time.UTC)
	tests := []struct {
		name    string
		version string
		date    string
		want    string
	}{
		{
			name:    "release build",
			version: "0.18.1",
			date:    "2026-07-12T07:32:07Z",
			want:    "0.18.1 (built 2026-07-12T07:32:07Z, 2d ago)",
		},
		{
			name:    "normalizes timestamp to UTC",
			version: "0.18.1",
			date:    "2026-07-14T17:02:07+10:00",
			want:    "0.18.1 (built 2026-07-14T07:02:07Z, 1h ago)",
		},
		{
			name:    "development build",
			version: "dev",
			date:    "unknown",
			want:    "dev (built unknown)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatVersion(tt.version, tt.date, now); got != tt.want {
				t.Fatalf("formatVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelativeAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 14, 8, 32, 7, 0, time.UTC)
	tests := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "clock skew", elapsed: -time.Minute, want: "just now"},
		{name: "seconds", elapsed: 59 * time.Second, want: "just now"},
		{name: "minutes", elapsed: 42 * time.Minute, want: "42m ago"},
		{name: "hours", elapsed: 23 * time.Hour, want: "23h ago"},
		{name: "days", elapsed: 29 * 24 * time.Hour, want: "29d ago"},
		{name: "months", elapsed: 11 * 30 * 24 * time.Hour, want: "11mo ago"},
		{name: "years", elapsed: 2 * 365 * 24 * time.Hour, want: "2y ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := relativeAge(now, now.Add(-tt.elapsed)); got != tt.want {
				t.Fatalf("relativeAge() = %q, want %q", got, tt.want)
			}
		})
	}
}
