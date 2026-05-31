package components

import (
	"maps"
	"slices"
)

var debugRegistry = map[string]DebugFactory{}

// Register adds a component to the debug registry. Call from init()
// functions in component .gsx or companion .go files.
func Register(name string, factory DebugFactory) {
	debugRegistry[name] = factory
}

// ListNames returns all registered component names sorted alphabetically.
func ListNames() []string {
	names := slices.Collect(maps.Keys(debugRegistry))
	slices.Sort(names)
	return names
}

// Get returns the DebugFactory for a named component, or nil.
func Get(name string) DebugFactory {
	return debugRegistry[name]
}
