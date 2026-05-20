package skills

import (
	"context"
	"errors"
	"sync"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/pubsub"
)

const managerEventTopic = "skills.manager.events"

// Event represents a refreshed skill snapshot.
type Event struct {
	AllSkills    []*Skill
	ActiveSkills []*Skill
	Diagnostics  []Diagnostic
}

// DiscoveryConfig controls a refresh pass.
type DiscoveryConfig struct {
	WorkingDir     string
	ExtraPaths     []string
	DisabledSkills []string
}

// Manager owns a workspace-scoped skill catalog and publishes refresh events.
type Manager struct {
	mu           sync.RWMutex
	allSkills    []*Skill
	activeSkills []*Skill
	diagnostics  []Diagnostic
	bus          *pubsub.Bus[Event]
}

func NewManager() *Manager {
	return &Manager{bus: pubsub.New[Event]()}
}

func (m *Manager) Refresh(cfg DiscoveryConfig) (Event, error) {
	if m == nil {
		return Event{}, errors.New("skills manager is required")
	}

	sources := DefaultSources(cfg.WorkingDir)
	sources = append(sources, AdditionalSources(cfg.ExtraPaths, ScopeUser)...)

	allSkills, diagnostics := Discover(sources)
	activeSkills := FilterDisabled(allSkills, cfg.DisabledSkills)
	snapshot := Event{
		AllSkills:    cloneSkills(allSkills),
		ActiveSkills: cloneSkills(activeSkills),
		Diagnostics:  cloneDiagnostics(diagnostics),
	}

	m.mu.Lock()
	m.allSkills = cloneSkills(snapshot.AllSkills)
	m.activeSkills = cloneSkills(snapshot.ActiveSkills)
	m.diagnostics = cloneDiagnostics(snapshot.Diagnostics)
	m.mu.Unlock()

	if m.bus != nil {
		_ = m.bus.Publish(context.Background(), managerEventTopic, snapshot)
	}

	return snapshot, nil
}

func (m *Manager) Snapshot() Event {
	if m == nil {
		return Event{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return Event{
		AllSkills:    cloneSkills(m.allSkills),
		ActiveSkills: cloneSkills(m.activeSkills),
		Diagnostics:  cloneDiagnostics(m.diagnostics),
	}
}

func (m *Manager) Subscribe(buffer int) (*pubsub.Subscription[Event], error) {
	if m == nil || m.bus == nil {
		return nil, errors.New("skills manager bus is not available")
	}
	return m.bus.Subscribe(managerEventTopic, buffer)
}

func (m *Manager) Close() {
	if m == nil || m.bus == nil {
		return
	}
	m.bus.Close()
}
