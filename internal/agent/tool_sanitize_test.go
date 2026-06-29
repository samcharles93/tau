package agent

import (
	"testing"

	"github.com/samcharles93/tau/internal/chat"
)

func TestSanitizeToolCallArguments(t *testing.T) {
	tests := []struct {
		name     string
		calls    []chat.ChatToolCall
		wantN    int
		wantArgs []string
	}{
		{
			name:  "empty calls",
			calls: nil,
			wantN: 0,
		},
		{
			name: "valid JSON",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `{"command":"git log"}`,
					},
				},
			},
			wantN: 0,
			wantArgs: []string{
				`{"command":"git log"}`,
			},
		},
		{
			name: "empty arguments",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: "",
					},
				},
			},
			wantN: 1,
			wantArgs: []string{
				"{}",
			},
		},
		{
			name: "whitespace-only arguments",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: "   ",
					},
				},
			},
			wantN: 1,
			wantArgs: []string{
				"{}",
			},
		},
		{
			name: "concatenated JSON objects (model hallucination)",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `{"command":"git show 9a130eb --stat"}{"command":"git show 7607305 --stat"}`,
					},
				},
			},
			wantN: 1,
			wantArgs: []string{
				"{}",
			},
		},
		{
			name: "bare string (not JSON)",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `just a string`,
					},
				},
			},
			wantN: 1,
			wantArgs: []string{
				"{}",
			},
		},
		{
			name: "mixed valid and invalid",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "read",
						Arguments: `{"path":"/tmp/file.txt"}`,
					},
				},
				{
					ID: "call_2",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `{"cmd":"a"}{"cmd":"b"}`,
					},
				},
				{
					ID: "call_3",
					Function: chat.ChatFunctionCall{
						Name:      "ls",
						Arguments: `{"path":"."}`,
					},
				},
			},
			wantN: 1,
			wantArgs: []string{
				`{"path":"/tmp/file.txt"}`,
				"{}",
				`{"path":"."}`,
			},
		},
		{
			name: "all invalid",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `not json`,
					},
				},
				{
					ID: "call_2",
					Function: chat.ChatFunctionCall{
						Name:      "read",
						Arguments: ``,
					},
				},
			},
			wantN: 2,
			wantArgs: []string{
				"{}",
				"{}",
			},
		},
		{
			name: "with leading/trailing whitespace (still valid JSON)",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "bash",
						Arguments: `  {"command":"echo hi"}  `,
					},
				},
			},
			wantN: 0,
			wantArgs: []string{
				`  {"command":"echo hi"}  `,
			},
		},
		{
			name: "valid array",
			calls: []chat.ChatToolCall{
				{
					ID: "call_1",
					Function: chat.ChatFunctionCall{
						Name:      "edit",
						Arguments: `[{"oldText":"a","newText":"b"}]`,
					},
				},
			},
			wantN: 0,
			wantArgs: []string{
				`[{"oldText":"a","newText":"b"}]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := make([]chat.ChatToolCall, len(tt.calls))
			copy(calls, tt.calls)

			got := sanitizeToolCallArguments(calls)
			if got != tt.wantN {
				t.Errorf("sanitizeToolCallArguments() = %d, want %d", got, tt.wantN)
			}

			if tt.wantArgs != nil {
				for i, want := range tt.wantArgs {
					if i >= len(calls) {
						t.Fatalf("call index %d missing, want %q", i, want)
					}
					if calls[i].Function.Arguments != want {
						t.Errorf("call[%d].Arguments = %q, want %q", i, calls[i].Function.Arguments, want)
					}
				}
			}
		})
	}
}
