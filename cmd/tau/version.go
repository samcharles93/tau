package main

import (
	"fmt"
	"time"
)

func formatVersion(version, date string, now time.Time) string {
	buildTime, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return fmt.Sprintf("%s (built %s)", version, date)
	}

	return fmt.Sprintf(
		"%s (built %s, %s)",
		version,
		buildTime.UTC().Format(time.RFC3339),
		relativeAge(now, buildTime),
	)
}

func relativeAge(now, then time.Time) string {
	elapsed := now.Sub(then)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	case elapsed < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
	case elapsed < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(elapsed/(30*24*time.Hour)))
	default:
		return fmt.Sprintf("%dy ago", int(elapsed/(365*24*time.Hour)))
	}
}
