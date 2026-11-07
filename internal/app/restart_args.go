package app

import (
	"os"
	"strings"
)

// resumeRestartArgs returns the extra argv to prepend when re-exec'ing the
// just-updated binary after /update, so the restarted process resumes the
// session that was persisted on shutdown instead of opening a fresh one.
// It returns nil when the original invocation already carries --resume/-r
// (the user's explicit choice wins). Callers must skip this for ephemeral
// sessions, which are never persisted and would fail to resume.
func resumeRestartArgs(sessionID string) []string {
	return resumeArgsFor(os.Args[1:], sessionID)
}

// resumeArgsFor is the pure form of resumeRestartArgs, split out for tests.
func resumeArgsFor(argv []string, sessionID string) []string {
	for _, arg := range argv {
		if arg == "--resume" || arg == "-r" ||
			strings.HasPrefix(arg, "--resume=") || strings.HasPrefix(arg, "-r=") {
			return nil
		}
	}
	return []string{"--resume", sessionID}
}
