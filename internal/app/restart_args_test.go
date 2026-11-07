package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResumeArgsFor(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		sessionID string
		want      []string
	}{
		{
			name:      "no resume flag appends --resume <id>",
			argv:      []string{"--model", "deepseek-flash"},
			sessionID: "sess-1",
			want:      []string{"--resume", "sess-1"},
		},
		{
			name:      "empty argv appends --resume <id>",
			argv:      nil,
			sessionID: "sess-2",
			want:      []string{"--resume", "sess-2"},
		},
		{
			name:      "existing --resume wins",
			argv:      []string{"--resume", "other"},
			sessionID: "sess-3",
			want:      nil,
		},
		{
			name:      "existing -r alias wins",
			argv:      []string{"-r", "other"},
			sessionID: "sess-3",
			want:      nil,
		},
		{
			name:      "existing --resume=value wins",
			argv:      []string{"--resume=other"},
			sessionID: "sess-3",
			want:      nil,
		},
		{
			name:      "existing -r=value wins",
			argv:      []string{"-r=other"},
			sessionID: "sess-3",
			want:      nil,
		},
		{
			name:      "unrelated flags do not match",
			argv:      []string{"--resume-foo", "--verbose"},
			sessionID: "sess-4",
			want:      []string{"--resume", "sess-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resumeArgsFor(tt.argv, tt.sessionID))
		})
	}
}
