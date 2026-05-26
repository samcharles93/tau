package extensions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/samcharles93/tau/internal/agent/tools"
)

type Config struct {
	WorkingDir string
	Sources    []Source
	Disabled   []string
	Registry   *tools.Registry
}

type Snapshot struct {
	Extensions  []Extension  `json:"extensions"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Manager struct {
	mu          sync.RWMutex
	registry    *tools.Registry
	sources     []Source
	disabled    map[string]struct{}
	extensions  []Extension
	diagnostics []Diagnostic
	loaded      map[string]*luaExtension
}

var ErrReloadWhileBusy = errors.New("extension reload requires idle runtime")

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Registry == nil {
		return nil, errors.New("tool registry is required")
	}
	sources := cfg.Sources
	if len(sources) == 0 {
		sources = DefaultSources(cfg.WorkingDir)
	}
	manager := &Manager{
		registry: cfg.Registry,
		sources:  append([]Source(nil), sources...),
		disabled: disabledSet(cfg.Disabled),
		loaded:   make(map[string]*luaExtension),
	}
	return manager, nil
}

func (m *Manager) Load(ctx context.Context) error {
	if m == nil {
		return errors.New("extension manager is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	oldLoaded := m.loaded
	m.loaded = make(map[string]*luaExtension)
	m.mu.Unlock()
	m.unloadLoaded(oldLoaded, nil)

	discovered, diagnostics := Discover(m.sources)
	loaded := make(map[string]*luaExtension)
	for _, ext := range discovered {
		if _, disabled := m.disabled[normalizeName(ext.Name)]; disabled {
			diagnostics = append(diagnostics, Diagnostic{
				Path:          ext.Path,
				ExtensionName: ext.Name,
				Severity:      SeverityWarning,
				Message:       fmt.Sprintf("extension %q is disabled by config", ext.Name),
			})
			continue
		}
		if err := ctx.Err(); err != nil {
			m.unloadLoaded(loaded, nil)
			return err
		}
		host, err := newLuaExtension(ext, m.registry, func(diagnostic Diagnostic) {
			diagnostics = append(diagnostics, diagnostic)
		})
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:          ext.Entry,
				ExtensionName: ext.Name,
				Severity:      SeverityError,
				Message:       err.Error(),
			})
			continue
		}
		loaded[normalizeName(ext.Name)] = host
		diagnostics = append(diagnostics, host.dispatch(EventManagerLoad, eventContext(EventManagerLoad, ext.Name))...)
	}

	m.mu.Lock()
	m.loaded = loaded
	m.extensions = extensionsFromLoaded(loaded)
	m.diagnostics = cloneDiagnostics(diagnostics)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Reload(ctx context.Context) error {
	if m == nil {
		return errors.New("extension manager is required")
	}
	m.Unload()
	return m.Load(ctx)
}

func (m *Manager) ReloadIfIdle(ctx context.Context, idle bool) error {
	if !idle {
		return ErrReloadWhileBusy
	}
	return m.Reload(ctx)
}

func (m *Manager) Unload() {
	if m == nil {
		return
	}
	m.mu.Lock()
	loaded := m.loaded
	m.loaded = make(map[string]*luaExtension)
	m.extensions = nil
	m.mu.Unlock()
	m.unloadLoaded(loaded, nil)
}

func (m *Manager) Dispatch(event Event, ctx map[string]any) {
	if m == nil {
		return
	}
	m.mu.RLock()
	loaded := make([]*luaExtension, 0, len(m.loaded))
	for _, host := range m.loaded {
		loaded = append(loaded, host)
	}
	m.mu.RUnlock()

	var diagnostics []Diagnostic
	for _, host := range loaded {
		diagnostics = append(diagnostics, host.dispatch(event, ctx)...)
	}
	if len(diagnostics) == 0 {
		return
	}
	m.mu.Lock()
	m.diagnostics = append(m.diagnostics, diagnostics...)
	m.mu.Unlock()
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Snapshot{
		Extensions:  cloneExtensions(m.extensions),
		Diagnostics: cloneDiagnostics(m.diagnostics),
	}
}

func (m *Manager) unloadLoaded(loaded map[string]*luaExtension, diagnostics *[]Diagnostic) {
	for _, host := range loaded {
		if diagnostics != nil {
			*diagnostics = append(*diagnostics, host.dispatch(EventManagerUnload, eventContext(EventManagerUnload, host.manifest.Name))...)
		} else {
			host.dispatch(EventManagerUnload, eventContext(EventManagerUnload, host.manifest.Name))
		}
		for _, toolName := range host.tools {
			m.registry.Unregister(toolName)
		}
		host.close()
	}
}

func extensionsFromLoaded(loaded map[string]*luaExtension) []Extension {
	extensions := make([]Extension, 0, len(loaded))
	for _, host := range loaded {
		extensions = append(extensions, host.manifest)
	}
	slices.SortFunc(extensions, func(left, right Extension) int {
		return strings.Compare(normalizeName(left.Name), normalizeName(right.Name))
	})
	return cloneExtensions(extensions)
}

func eventContext(event Event, extensionName string) map[string]any {
	return map[string]any{
		"event":     string(event),
		"extension": extensionName,
	}
}

func cloneExtensions(in []Extension) []Extension {
	out := make([]Extension, len(in))
	copy(out, in)
	return out
}

func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, len(in))
	copy(out, in)
	return out
}

func disabledSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized := normalizeName(name)
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}
