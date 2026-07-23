package cli

import "testing"

func TestParsePluginSourceSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    PluginSourceSpec
		wantErr bool
	}{
		{
			name: "valid without version",
			raw:  "samcharles93/tau-plugins:mcp",
			want: PluginSourceSpec{
				Owner:  "samcharles93",
				Repo:   "tau-plugins",
				Plugin: "mcp",
			},
		},
		{
			name: "valid with version",
			raw:  "samcharles93/tau-plugins:mcp@v1.2.3",
			want: PluginSourceSpec{
				Owner:   "samcharles93",
				Repo:    "tau-plugins",
				Plugin:  "mcp",
				Version: "v1.2.3",
			},
		},
		{
			name: "trims outer spaces",
			raw:  "  samcharles93/tau-plugins:mcp@main  ",
			want: PluginSourceSpec{
				Owner:   "samcharles93",
				Repo:    "tau-plugins",
				Plugin:  "mcp",
				Version: "main",
			},
		},
		{name: "empty source", raw: "", wantErr: true},
		{name: "missing colon", raw: "samcharles93/tau-plugins", wantErr: true},
		{name: "missing slash in repo", raw: "samcharles93:mcp", wantErr: true},
		{name: "empty plugin", raw: "samcharles93/tau-plugins:", wantErr: true},
		{name: "plugin contains slash", raw: "samcharles93/tau-plugins:m/cp", wantErr: true},
		{name: "plugin is dot", raw: "samcharles93/tau-plugins:.", wantErr: true},
		{name: "plugin is dot-dot", raw: "samcharles93/tau-plugins:..", wantErr: true},
		{name: "plugin contains backslash", raw: `samcharles93/tau-plugins:m\cp`, wantErr: true},
		{name: "plugin is absolute path", raw: "samcharles93/tau-plugins:/etc/passwd", wantErr: true},
		{name: "plugin is volume-qualified", raw: "samcharles93/tau-plugins:C:evil", wantErr: true},
		{name: "empty version", raw: "samcharles93/tau-plugins:mcp@", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePluginSourceSpec(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParsePluginSourceSpec(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
