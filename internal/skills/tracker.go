package skills

import (
	"strings"
	"sync"
)

// Tracker remembers which skills have been activated in the current session.
type Tracker struct {
	mu        sync.RWMutex
	activated map[string]struct{}
	order     []string
}

func NewTracker() *Tracker {
	return &Tracker{activated: make(map[string]struct{})}
}

func (t *Tracker) Activate(skill *Skill) {
	if t == nil || skill == nil {
		return
	}

	name := strings.TrimSpace(skill.Name)
	if name == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	key := strings.ToLower(name)
	if _, exists := t.activated[key]; exists {
		return
	}
	t.activated[key] = struct{}{}
	t.order = append(t.order, name)
}

func (t *Tracker) Activated() []string {
	if t == nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]string, len(t.order))
	copy(result, t.order)
	return result
}

func (t *Tracker) IsActivated(name string) bool {
	if t == nil {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.activated[strings.ToLower(strings.TrimSpace(name))]
	return exists
}

func (t *Tracker) Reset() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.activated = make(map[string]struct{})
	t.order = nil
}
