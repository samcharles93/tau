package cli

import "testing"

func TestSplitProviderModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		provider string
		model    string
		ok       bool
	}{
		{
			name:     "preferred colon syntax with nested model path",
			raw:      "openrouter:nvidia/nemotron-3-ultra",
			provider: "openrouter",
			model:    "nvidia/nemotron-3-ultra",
			ok:       true,
		},
		{
			name:     "legacy slash syntax remains supported",
			raw:      "openrouter/nvidia/nemotron-3-ultra",
			provider: "openrouter",
			model:    "nvidia/nemotron-3-ultra",
			ok:       true,
		},
		{
			name: "bare model is not split",
			raw:  "gpt-5.3",
			ok:   false,
		},
		{
			name: "empty is not split",
			raw:  "",
			ok:   false,
		},
		{
			name: "invalid colon format missing model",
			raw:  "openrouter:",
			ok:   false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			provider, model, ok := splitProviderModel(tc.raw)
			if ok != tc.ok {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.ok)
			}
			if provider != tc.provider {
				t.Fatalf("provider mismatch: got %q want %q", provider, tc.provider)
			}
			if model != tc.model {
				t.Fatalf("model mismatch: got %q want %q", model, tc.model)
			}
		})
	}
}
