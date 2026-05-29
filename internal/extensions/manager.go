package extensions

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/samcharles93/tau/internal/agent/tools"
)

type Config struct {
	WorkingDir       string
	Sources          []Source
	DefaultSources   bool
	Disabled         []string
	ReservedCommands []string
	Registry         *tools.Registry
}

type Snapshot struct {
	Extensions  []Extension  `json:"extensions"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Commands    []Command    `json:"commands,omitempty"`
}

type Manager struct {
	mu               sync.RWMutex
	registry         *tools.Registry
	sources          []Source
	disabled         map[string]struct{}
	extensions       []Extension
	diagnostics      []Diagnostic
	commands         []Command
	loaded           map[string]*luaExtension
	commandHost      map[string]string
	reservedCommands map[string]string
}

var ErrReloadWhileBusy = errors.New("extension reload requires idle runtime")

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Registry == nil {
		return nil, errors.New("tool registry is required")
	}
	sources := cfg.Sources
	if len(sources) == 0 && cfg.DefaultSources {
		sources = DefaultSources(cfg.WorkingDir)
	}
	manager := &Manager{
		registry:         cfg.Registry,
		sources:          append([]Source(nil), sources...),
		disabled:         disabledSet(cfg.Disabled),
		reservedCommands: commandOwnerSet(cfg.ReservedCommands, "built-in command"),
		loaded:           make(map[string]*luaExtension),
		commandHost:      make(map[string]string),
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

	loaded, diagnostics, commandHost, err := m.loadFresh(ctx)
	if err != nil {
		m.unloadLoaded(loaded, nil)
		return err
	}

	m.mu.Lock()
	m.loaded = loaded
	m.extensions = extensionsFromLoaded(loaded)
	m.commands = commandsFromLoaded(loaded)
	m.commandHost = commandHost
	m.diagnostics = cloneDiagnostics(diagnostics)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Reload(ctx context.Context) error {
	if m == nil {
		return errors.New("extension manager is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.RLock()
	oldLoaded := m.loaded
	oldExtensions := cloneExtensions(m.extensions)
	oldDiagnostics := cloneDiagnostics(m.diagnostics)
	oldCommands := cloneCommands(m.commands)
	oldCommandHost := cloneStringMap(m.commandHost)
	m.mu.RUnlock()

	m.unregisterLoaded(oldLoaded)
	loaded, diagnostics, commandHost, err := m.loadFresh(ctx)
	if err != nil {
		m.unloadLoaded(loaded, nil)
		if restoreErr := m.registerLoaded(oldLoaded); restoreErr != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Severity: SeverityError,
				Message:  fmt.Sprintf("restore previous extensions after reload failure: %v", restoreErr),
			})
		}
		m.mu.Lock()
		m.loaded = oldLoaded
		m.extensions = oldExtensions
		m.diagnostics = append(oldDiagnostics, diagnostics...)
		m.commands = oldCommands
		m.commandHost = oldCommandHost
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	m.loaded = loaded
	m.extensions = extensionsFromLoaded(loaded)
	m.commands = commandsFromLoaded(loaded)
	m.commandHost = commandHost
	m.diagnostics = cloneDiagnostics(diagnostics)
	m.mu.Unlock()
	m.closeLoaded(oldLoaded, nil)
	return nil
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
	m.commands = nil
	m.commandHost = make(map[string]string)
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
		Commands:    cloneCommands(m.commands),
	}
}

func (m *Manager) ExecuteCommand(ctx context.Context, name, args string, ui tools.UIBridge) (string, error) {
	if m == nil {
		return "", errors.New("extension manager is required")
	}
	normalized := normalizeName(name)
	if normalized == "" {
		return "", errors.New("extension command name is required")
	}
	m.mu.RLock()
	extensionName := m.commandHost[normalized]
	selected := m.loaded[extensionName]
	if selected != nil && !selected.hasCommand(normalized) {
		selected = nil
	}
	m.mu.RUnlock()
	if selected == nil {
		return "", fmt.Errorf("extension command %q not found", name)
	}
	return selected.executeCommand(ctx, normalized, args, ui)
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

func (m *Manager) closeLoaded(loaded map[string]*luaExtension, diagnostics *[]Diagnostic) {
	for _, host := range loaded {
		toolCount := len(host.tools)
		if diagnostics != nil {
			*diagnostics = append(*diagnostics, host.dispatch(EventManagerUnload, eventContext(EventManagerUnload, host.manifest.Name))...)
		} else {
			host.dispatch(EventManagerUnload, eventContext(EventManagerUnload, host.manifest.Name))
		}
		for _, toolName := range host.tools[toolCount:] {
			m.registry.Unregister(toolName)
		}
		host.close()
	}
}

func (m *Manager) unregisterLoaded(loaded map[string]*luaExtension) {
	for _, host := range loaded {
		for _, toolName := range host.tools {
			m.registry.Unregister(toolName)
		}
	}
}

func (m *Manager) registerLoaded(loaded map[string]*luaExtension) error {
	for _, host := range loaded {
		for _, tool := range host.toolDefs {
			if err := m.registry.Register(tool); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) loadFresh(ctx context.Context) (map[string]*luaExtension, []Diagnostic, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	discovered, diagnostics := Discover(m.sources)
	loaded := make(map[string]*luaExtension)
	commandOwners := cloneStringMap(m.reservedCommands)
	commandHost := make(map[string]string)
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
			return loaded, diagnostics, commandHost, err
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
		diagnostics = append(diagnostics, host.dispatch(EventManagerLoad, eventContext(EventManagerLoad, ext.Name))...)
		if diagnostic, hasCollision := commandCollision(host, commandOwners); hasCollision {
			diagnostics = append(diagnostics, diagnostic)
			m.unloadLoaded(map[string]*luaExtension{normalizeName(ext.Name): host}, &diagnostics)
			continue
		}
		extensionName := normalizeName(ext.Name)
		loaded[extensionName] = host
		for _, command := range host.commandsSnapshot() {
			commandName := normalizeName(command.Name)
			commandOwners[commandName] = ext.Name
			commandHost[commandName] = extensionName
		}
	}
	return loaded, diagnostics, commandHost, nil
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

func commandsFromLoaded(loaded map[string]*luaExtension) []Command {
	var commands []Command
	for _, host := range loaded {
		commands = append(commands, host.commandsSnapshot()...)
	}
	slices.SortFunc(commands, func(left, right Command) int {
		return strings.Compare(normalizeName(left.Name), normalizeName(right.Name))
	})
	return cloneCommands(commands)
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

func cloneCommands(in []Command) []Command {
	out := make([]Command, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
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

func commandOwnerSet(names []string, owner string) map[string]string {
	out := make(map[string]string, len(names))
	for _, name := range names {
		normalized := normalizeName(strings.TrimPrefix(name, "/"))
		if normalized == "" {
			continue
		}
		out[normalized] = owner
	}
	return out
}

func commandCollision(host *luaExtension, owners map[string]string) (Diagnostic, bool) {
	for _, command := range host.commandsSnapshot() {
		name := normalizeName(command.Name)
		if name == "" {
			continue
		}
		if owner, exists := owners[name]; exists {
			return Diagnostic{
				Path:          host.manifest.Entry,
				ExtensionName: host.manifest.Name,
				Severity:      SeverityError,
				Message:       fmt.Sprintf("command %q is already registered by %s", name, owner),
			}, true
		}
	}
	return Diagnostic{}, false
}
