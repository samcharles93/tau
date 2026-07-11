package main

import (
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
)

func TestClassifyLegacyErrorContentAvoidsSourceFalsePositive(t *testing.T) {
	got := classifyLegacyErrorContent("grep", `file.go:12: return errors.New("path is required")`)
	if got != "" {
		t.Fatalf("classified source output as %q", got)
	}
}

func TestClassifyLegacyErrorContentRecognizesToolPrefix(t *testing.T) {
	if got := classifyLegacyErrorContent("read", "path is required (file is accepted as an alias)"); got != "invalid_args" {
		t.Fatalf("classification = %q", got)
	}
}

func TestOverlapRatio(t *testing.T) {
	if got := overlapRatio(100, 75); got != 0.25 {
		t.Fatalf("overlap ratio = %v", got)
	}
}

func TestPercentileUsesNearestRank(t *testing.T) {
	samples := make([]int, 100)
	for i := range samples {
		samples[i] = i + 1
	}
	if got := percentile(samples, 95); got != 95 {
		t.Fatalf("p95 = %d, want 95", got)
	}
	if got := percentile(samples[:20], 95); got != 19 {
		t.Fatalf("20-sample p95 = %d, want 19", got)
	}
	if got := percentile([]int{7}, 99); got != 7 {
		t.Fatalf("single-sample p99 = %d, want 7", got)
	}
}

func TestReadArgsUsesEffectiveDefaultLimit(t *testing.T) {
	_, _, limit := readArgs(`{"path":"large.go"}`)
	if limit != tools.DefaultReadLines {
		t.Fatalf("limit = %d, want %d", limit, tools.DefaultReadLines)
	}
}
