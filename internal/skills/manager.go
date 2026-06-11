package skills

import (
	"errors"
	"sync"

	"github.com/samcharles93/tau/internal/eventbus"
)

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

// Manager owns a workspace-scoped skill catalog and publishes refresh events
// through the event bus. Callers subscribe to [Event] on the bus directly.
type Manager struct {
	mu           sync.RWMutex
	allSkills    []*Skill
	activeSkills []*Skill
	diagnostics  []Diagnostic
	bus          *eventbus.Bus
	client       *eventbus.Client
	pub          *eventbus.Publisher[Event]
}

// NewManager creates a skill manager attached to the given event bus.
func NewManager(bus *eventbus.Bus) *Manager {
	client := bus.Client("skills")
	return &Manager{
		bus:    bus,
		client: client,
		pub:    eventbus.Publish[Event](client),
	}
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

	if m.pub != nil {
		m.pub.Publish(snapshot)
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

// Close closes the manager's event bus client.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.client.Close()
}
