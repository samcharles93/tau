// Package extensions provides Tau's narrow extension manager and Lua host.
package extensions

import (
	"path/filepath"
	"strings"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeExtra   Scope = "extra"
)

type DiagnosticSeverity string

const (
	SeverityInfo    DiagnosticSeverity = "info"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityError   DiagnosticSeverity = "error"
)

type Diagnostic struct {
	Path          string             `json:"path,omitempty"`
	ExtensionName string             `json:"extension_name,omitempty"`
	Severity      DiagnosticSeverity `json:"severity"`
	Message       string             `json:"message"`
}

type Source struct {
	Root  string
	Scope Scope
}

type Extension struct {
	Name       string `json:"name"`
	Runtime    string `json:"runtime"`
	Entry      string `json:"entry"`
	Path       string `json:"path"`
	SourceRoot string `json:"source_root"`
	Scope      Scope  `json:"scope"`
}

type Event string

const (
	EventManagerLoad     Event = "manager_load"
	EventManagerUnload   Event = "manager_unload"
	EventSessionStart    Event = "session_start"
	EventSessionShutdown Event = "session_shutdown"
)

var safeEvents = map[Event]struct{}{
	EventManagerLoad:     {},
	EventManagerUnload:   {},
	EventSessionStart:    {},
	EventSessionShutdown: {},
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func extensionNameFromLuaPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}
