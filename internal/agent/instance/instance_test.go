package instance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

type resumeCaptureStore struct {
	store.SessionStore
	sessionID string
	instance  store.AgentInstance
}

func (s *resumeCaptureStore) ResumeSession(_ context.Context, sessionID string, inst store.AgentInstance) error {
	s.sessionID = sessionID
	s.instance = inst
	return nil
}

func TestInstantiateRootAndChildSemantics(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantTools   []string
		wantDepth   int
		wantModel   string
		wantErrPart string
	}{
		{
			name: "root uses spec tools and model",
			cfg: Config{
				Definition: &spec.Definition{
					Name:     "tau",
					Provider: "anthropic",
					Model:    "claude-root",
					Tools:    []string{"read", "grep"},
				},
			},
			wantTools: []string{"read", "grep"},
			wantModel: "claude-root",
		},
		{
			name: "unrestricted child is narrowed by parent",
			cfg: Config{
				Child:                true,
				Definition:           &spec.Definition{Name: "task"},
				ParentInstanceID:     "tau#parent",
				ParentDepth:          0,
				ParentEffectiveTools: []string{"read", "grep"},
			},
			wantTools: []string{"read", "grep"},
			wantDepth: 1,
		},
		{
			name: "spawn narrows child below spec and parent",
			cfg: Config{
				Child:                true,
				Definition:           &spec.Definition{Name: "task", Tools: []string{"read", "grep", "write"}},
				ParentInstanceID:     "tau#parent",
				ParentDepth:          1,
				ParentEffectiveTools: []string{"read", "grep"},
				SpawnTools:           []string{"read"},
				Agents:               config.AgentsConfig{DefaultMaxDepth: 3, DepthCeiling: 4},
			},
			wantTools: []string{"read"},
			wantDepth: 2,
		},
		{
			name: "default depth cap rejects child",
			cfg: Config{
				Child:            true,
				Definition:       &spec.Definition{Name: "task"},
				ParentInstanceID: "tau#parent",
				ParentDepth:      1,
				Agents:           config.AgentsConfig{DefaultMaxDepth: 1, DepthCeiling: 4},
			},
			wantErrPart: "depth 2 exceeds cap 1",
		},
		{
			name: "hard depth ceiling rejects child",
			cfg: Config{
				Child:            true,
				Definition:       &spec.Definition{Name: "task"},
				ParentInstanceID: "tau#parent",
				ParentDepth:      2,
				Agents:           config.AgentsConfig{DefaultMaxDepth: 4, DepthCeiling: 2},
			},
			wantErrPart: "depth 3 exceeds ceiling 2",
		},
		{
			name: "child with missing parent identity still attenuates",
			cfg: Config{
				Child:                true,
				Definition:           &spec.Definition{Name: "task", Tools: []string{"read", "write"}},
				ParentEffectiveTools: []string{"read"},
				SpawnTools:           []string{"read"},
			},
			wantTools: []string{"read"},
			wantDepth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.MintInstanceID = func(name string) string { return name + "#test01" }

			got, err := Instantiate(context.Background(), cfg)
			if tt.wantErrPart != "" {
				require.ErrorContains(t, err, tt.wantErrPart)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTools, got.EffectiveTools)
			require.Equal(t, tt.wantDepth, got.Depth)
			require.Equal(t, tt.wantModel, got.ResolvedModel)
		})
	}
}

func TestInstantiatePersistsSnapshotAndRetriesIDCollisions(t *testing.T) {
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "sessions.db"), dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	def := &spec.Definition{
		Name:        "research",
		Description: "research carefully",
		Provider:    "openai",
		Model:       "gpt-test",
		Tools:       []string{"read"},
		Body:        "You research.",
	}
	require.NoError(t, s.SaveAgentInstance(context.Background(), store.AgentInstance{
		ID:           "research#taken1",
		SpecName:     def.Name,
		SpecHash:     "existing",
		SpecSnapshot: "{}",
	}))

	ids := []string{"research#taken1", "research#fresh1"}
	mintCalls := 0
	got, err := Instantiate(context.Background(), Config{
		Definition: def,
		Store:      s,
		MintInstanceID: func(string) string {
			id := ids[mintCalls]
			mintCalls++
			return id
		},
	})
	require.NoError(t, err)
	require.Equal(t, "research#fresh1", got.InstanceID)
	require.Equal(t, 2, mintCalls)

	saved, err := s.GetAgentInstance(context.Background(), got.InstanceID)
	require.NoError(t, err)
	require.Equal(t, spec.BuildSpecSnapshot(def, "openai", "gpt-test", []string{"read"}), saved.SpecSnapshot)
	require.Equal(t, spec.HashSpecSnapshot(saved.SpecSnapshot), saved.SpecHash)
	require.Equal(t, `["read"]`, saved.EffectiveTools)
}

func TestResumeCentralizesChildInstantiationSemantics(t *testing.T) {
	def := &spec.Definition{
		Name:     "research",
		Provider: "openai",
		Model:    "gpt-old",
		Tools:    []string{"read", "grep", "write"},
		MaxTurns: 7,
		Timeout:  3 * time.Minute,
		Body:     "Research.",
	}
	originalSnapshot := spec.BuildSpecSnapshot(def, "openai", "gpt-old", def.Tools)
	capture := &resumeCaptureStore{}

	got, err := Resume(context.Background(), ResumeConfig{
		Original: store.AgentInstance{
			SpecName:       def.Name,
			SpecScope:      "builtin",
			SpecSnapshot:   originalSnapshot,
			EffectiveTools: spec.ToolsToJSON(def.Tools),
		},
		SessionID:            "session-1",
		ParentInstanceID:     "tau#parent",
		ParentDepth:          1,
		ParentEffectiveTools: []string{"read", "grep"},
		SpawnTools:           []string{"read"},
		ModelOverride:        "fast",
		ModelModes: map[string]config.ModeConfig{
			"fast": {Provider: "anthropic", Model: "claude-fast"},
		},
		Agents:         config.AgentsConfig{DefaultMaxDepth: 3, DepthCeiling: 4},
		Store:          capture,
		MintInstanceID: func(string) string { return "research#resume" },
	})
	require.NoError(t, err)
	require.Equal(t, "research#resume", got.InstanceID)
	require.Equal(t, "anthropic", got.ResolvedProvider)
	require.Equal(t, "claude-fast", got.ResolvedModel)
	require.Equal(t, []string{"read"}, got.EffectiveTools)
	require.Equal(t, 2, got.Depth)
	require.Equal(t, 7, got.MaxTurns)
	require.Equal(t, 3*time.Minute, got.Timeout)

	require.Equal(t, "session-1", capture.sessionID)
	require.Equal(t, "research#resume", capture.instance.ID)
	require.Equal(t, "tau#parent", capture.instance.ParentInstanceID)
	require.Equal(t, spec.HashSpecSnapshot(capture.instance.SpecSnapshot), capture.instance.SpecHash)
	require.Equal(t, `["read"]`, capture.instance.EffectiveTools)
	require.Contains(t, capture.instance.SpecSnapshot, `"resolved":{"model":"claude-fast","provider":"anthropic"}`)
}

func TestPersistWithIDRetryFailureModes(t *testing.T) {
	t.Run("non-unique error stops immediately", func(t *testing.T) {
		calls := 0
		wantErr := errors.New("storage unavailable")
		_, err := PersistWithIDRetry(
			&store.AgentInstance{},
			"task",
			func(string) string { return "task#first" },
			func(store.AgentInstance) error {
				calls++
				return wantErr
			},
		)
		require.ErrorIs(t, err, wantErr)
		require.Equal(t, 1, calls)
	})

	t.Run("unique collisions exhaust the bounded retry budget", func(t *testing.T) {
		dir := t.TempDir()
		s, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "sessions.db"), dir)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, s.Close()) })
		require.NoError(t, s.SaveAgentInstance(context.Background(), store.AgentInstance{
			ID:           "task#taken",
			SpecName:     "task",
			SpecHash:     "existing",
			SpecSnapshot: "{}",
		}))

		calls := 0
		_, err = PersistWithIDRetry(
			&store.AgentInstance{SpecName: "task", SpecHash: "new", SpecSnapshot: "{}"},
			"task",
			func(string) string { return "task#taken" },
			func(inst store.AgentInstance) error {
				calls++
				return s.SaveAgentInstance(context.Background(), inst)
			},
		)
		require.ErrorContains(t, err, "id collision after")
		require.Equal(t, spec.MaxInstanceIDCollisionRetries, calls)
	})
}
