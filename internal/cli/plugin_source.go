package cli

import (
	"fmt"
	"strings"
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

	plugin := pluginRef
	version := ""
	if at := strings.Index(pluginRef, "@"); at >= 0 {
		plugin = strings.TrimSpace(pluginRef[:at])
		version = strings.TrimSpace(pluginRef[at+1:])
		if version == "" {
			return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: version cannot be empty", raw)
		}
	}

	if plugin == "" {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin source %q: plugin name cannot be empty", raw)
	}
	if strings.Contains(plugin, "/") {
		return PluginSourceSpec{}, fmt.Errorf("invalid plugin name %q: must not contain '/'", plugin)
	}

	return PluginSourceSpec{
		Owner:   owner,
		Repo:    repo,
		Plugin:  plugin,
		Version: version,
	}, nil
}
