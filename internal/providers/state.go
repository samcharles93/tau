package providers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/samcharles93/tau/internal/config"
)

// stateFileName is the tau-owned managed file holding enabled providers and
// OAuth credentials. It lives alongside config.yaml but is written exclusively
// by tau, so the user's hand-edited config is never disturbed.
const stateFileName = "auth.yaml"

const stateVersion = 1

// OAuthCredentials holds persisted credentials for an OAuth provider. Expires is
// a Unix timestamp in seconds; zero means "unknown / never expires". Extra
// carries provider-specific fields (e.g. a derived API base URL) without
// bloating the core schema.
type OAuthCredentials struct {
	Access  string            `yaml:"access"`
	Refresh string            `yaml:"refresh,omitempty"`
	Expires int64             `yaml:"expires,omitempty"`
	Extra   map[string]string `yaml:"extra,omitempty"`
}

// Expired reports whether the access token is past its expiry, treating tokens
// within the next 60 seconds as already expired to avoid races at the boundary.
func (c OAuthCredentials) Expired() bool {
	if c.Expires == 0 {
		return false
	}
	return time.Now().Add(60 * time.Second).After(time.Unix(c.Expires, 0))
}

// State is tau's managed provider state, persisted as auth.yaml.
//
// API-key catalog providers are auto-discovered: whenever their conventional
// environment variable is set they are active by default, so tau works out of
// the box without a config file. Enabled records providers the user turned on
// explicitly (useful for keyless providers); Disabled records auto-detected
// providers the user turned off. Disabled wins over auto-detection.
type State struct {
	Version  int                         `yaml:"version"`
	Enabled  []string                    `yaml:"enabled,omitempty"`
	Disabled []string                    `yaml:"disabled,omitempty"`
	OAuth    map[string]OAuthCredentials `yaml:"oauth,omitempty"`
	APIKeys  map[string]string           `yaml:"api_keys,omitempty"`

	// path records where the state was loaded from so Save can round-trip
	// without the caller re-deriving it. Not serialized.
	path string `yaml:"-"`
}

// StatePath returns the path to the managed state file.
func StatePath() string {
	return filepath.Join(config.Dir(), stateFileName)
}

// LoadState reads the managed state file. A missing file yields an empty,
// ready-to-use State (not an error) so first-run launches work seamlessly.
func LoadState() (State, error) {
	return loadStateFrom(StatePath())
}

func loadStateFrom(path string) (State, error) {
	s := State{Version: stateVersion, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return State{}, fmt.Errorf("read provider state %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return s, nil
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse provider state %s: %w", path, err)
	}
	if s.Version == 0 {
		s.Version = stateVersion
	}
	s.path = path
	if s.OAuth == nil {
		s.OAuth = map[string]OAuthCredentials{}
	}
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	return s, nil
}

// Save writes the state atomically (temp file + rename) with 0600 permissions
// so credentials are not world-readable. The parent directory is created if
// needed.
func (s *State) Save() error {
	path := s.path
	if path == "" {
		path = StatePath()
		s.path = path
	}
	if s.Version == 0 {
		s.Version = stateVersion
	}
	s.normalize()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal provider state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), stateFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(0o600); err != nil {
			tmp.Close()
			return fmt.Errorf("chmod temp state file: %w", err)
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}

// normalize keeps the serialized form stable: deduplicated, sorted enable/
// disable lists and a non-nil OAuth map dropped when empty.
func (s *State) normalize() {
	s.Enabled = dedupeSorted(s.Enabled)
	s.Disabled = dedupeSorted(s.Disabled)
	if len(s.OAuth) == 0 {
		s.OAuth = nil
	}
	if len(s.APIKeys) == 0 {
		s.APIKeys = nil
	}
}

func dedupeSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, name := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsEnabled reports whether the named provider has been explicitly enabled.
func (s *State) IsEnabled(name string) bool {
	return slices.Contains(s.Enabled, strings.TrimSpace(name))
}

// IsDisabled reports whether the named provider has been explicitly disabled,
// suppressing auto-detection.
func (s *State) IsDisabled(name string) bool {
	return slices.Contains(s.Disabled, strings.TrimSpace(name))
}

// Enable turns a provider on: it is added to the enabled set and removed from
// the disabled set. It does not persist; call Save.
func (s *State) Enable(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.Disabled = remove(s.Disabled, name)
	if !s.IsEnabled(name) {
		s.Enabled = append(s.Enabled, name)
	}
}

// Disable turns a provider off: it is added to the disabled set (suppressing
// auto-detection) and removed from the enabled set. It does not persist.
func (s *State) Disable(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.Enabled = remove(s.Enabled, name)
	if !s.IsDisabled(name) {
		s.Disabled = append(s.Disabled, name)
	}
}

func remove(in []string, name string) []string {
	out := in[:0]
	for _, n := range in {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

// SetOAuth stores credentials for an OAuth provider. It does not persist.
func (s *State) SetOAuth(name string, creds OAuthCredentials) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if s.OAuth == nil {
		s.OAuth = map[string]OAuthCredentials{}
	}
	s.OAuth[name] = creds
}

// OAuthFor returns stored credentials for an OAuth provider, if any.
func (s *State) OAuthFor(name string) (OAuthCredentials, bool) {
	creds, ok := s.OAuth[strings.TrimSpace(name)]
	return creds, ok
}

// RemoveOAuth deletes stored credentials for an OAuth provider. It does not
// persist.
func (s *State) RemoveOAuth(name string) {
	delete(s.OAuth, strings.TrimSpace(name))
}

// SetAPIKey stores a managed API key for a provider, entered explicitly by the
// user (e.g. via the setup wizard). It does not persist; call Save.
func (s *State) SetAPIKey(name, key string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	s.APIKeys[name] = key
}

// APIKeyFor returns the managed API key for a provider, if any.
func (s *State) APIKeyFor(name string) (string, bool) {
	key, ok := s.APIKeys[strings.TrimSpace(name)]
	return key, ok
}

// RemoveAPIKey deletes the managed API key for a provider. It does not
// persist.
func (s *State) RemoveAPIKey(name string) {
	delete(s.APIKeys, strings.TrimSpace(name))
}
