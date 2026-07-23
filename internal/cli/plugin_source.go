package cli

import (
	"fmt"
	"strings"

	"github.com/samcharles93/tau/internal/plugin"
)

// PluginSourceSpec represents a plugin install source in the form
// "owner/repo:plugin[@version]".
type PluginSourceSpec struct {
	Owner   string
	Repo    string
	Plugin  string
	Version string
}

// ParsePluginSourceSpec parses source specs such as:
//   - owner/repo:plugin
//   - owner/repo:plugin@v1.2.0
func ParsePluginSourceSpec(raw string) (PluginSourceSpec, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return PluginSourceSpec{}, fmt.Errorf("plugin source cannot be empty")
	}

	parts := strings.Split(spec, ":")
	if len(parts) != 2 {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: expected owner/repo:plugin[@version]", raw)
	}

	repoRef := strings.TrimSpace(parts[0])
	pluginRef := strings.TrimSpace(parts[1])
	if repoRef == "" || pluginRef == "" {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: expected owner/repo:plugin[@version]", raw)
	}

	repoParts := strings.Split(repoRef, "/")
	if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
		return PluginSourceSpec{}, fmt.Errorf("invalid repository %q: expected owner/repo", repoRef)
	}

	owner := repoParts[0]
	repo := repoParts[1]

	pluginName := pluginRef
	version := ""
	if before, after, ok := strings.Cut(pluginRef, "@"); ok {
		pluginName = strings.TrimSpace(before)
		version = strings.TrimSpace(after)
		if version == "" {
			return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: version cannot be empty", raw)
		}
	}

	if err := plugin.ValidatePluginName(pluginName); err != nil {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: %w", raw, err)
	}

	return PluginSourceSpec{
		Owner:   owner,
		Repo:    repo,
		Plugin:  pluginName,
		Version: version,
	}, nil
}
