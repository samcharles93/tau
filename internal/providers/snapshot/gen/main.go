// Command gen builds tau's embedded provider+model snapshot from models.dev.
//
// It fetches the models.dev catalogue (or reads a local copy with -input),
// keeps only the providers in tau's built-in catalog (remapping ids where
// models.dev disagrees, e.g. gemini←google), and within each keeps only the
// models that advertise tool calling. The result is written in the models.dev
// JSON shape so the ai-sdk runtime can load it directly, and embedded into the
// binary so tau never depends on models.dev being reachable at runtime.
//
// Usage:
//
//	go run ./internal/providers/snapshot/gen            # fetch live
//	go run ./internal/providers/snapshot/gen -input f   # from a local file
//	go run ./internal/providers/snapshot/gen -o path    # custom output
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/samcharles93/tau/internal/providers"
)

const catalogURL = "https://models.dev/api.json"

// provider mirrors the subset of the models.dev provider object we carry into
// the snapshot. Models are kept as raw JSON so every field models.dev publishes
// (reasoning_options, cost, limit, modalities, …) is preserved verbatim.
type provider struct {
	ID     string                     `json:"id"`
	Models map[string]json.RawMessage `json:"models"`
}

type modelCaps struct {
	ToolCall bool `json:"tool_call"`
}

// nemotronModels are the nemotron model IDs that need reasoning effort levels
// added because models.dev only provides "toggle" type.
var nemotronModels = map[string]bool{
	"nemotron-3-ultra":                       true,
	"nemotron-3-super":                       true,
	"nemotron-3-nano:30b":                    true,
	"nvidia/nemotron-3-ultra-550b-a55b":      true,
	"nvidia/nemotron-3-super-120b-a12b":      true,
	"nvidia/nemotron-3-nano-30b-a3b":         true,
	"nvidia/nemotron-3-ultra-550b-a55b:free": true,
	"nvidia/nemotron-3-super-120b-a12b:free": true,
	"nvidia/nemotron-3-nano-30b-a3b:free":    true,
}

func main() {
	input := flag.String("input", "", "path to a local models.dev api.json (default: fetch live)")
	out := flag.String("o", "internal/providers/snapshot/models.json", "output path for the snapshot")
	flag.Parse()

	raw, err := loadCatalog(*input)
	if err != nil {
		fail(err)
	}

	var top map[string]provider
	if err := json.Unmarshal(raw, &top); err != nil {
		fail(fmt.Errorf("parse catalog: %w", err))
	}

	snapshot := make(map[string]provider)
	var kept, dropped int
	for _, entry := range providers.Catalog() {
		src, ok := top[entry.SnapshotID()]
		if !ok {
			continue
		}
		models := make(map[string]json.RawMessage)
		for id, rawModel := range src.Models {
			var caps modelCaps
			if err := json.Unmarshal(rawModel, &caps); err != nil {
				continue
			}
			if !caps.ToolCall {
				dropped++
				continue
			}
			// Fix known incorrect model limits from models.dev
			if id == "nemotron-3-ultra" {
				var model map[string]any
				if err := json.Unmarshal(rawModel, &model); err == nil {
					if limit, ok := model["limit"].(map[string]any); ok {
						limit["output"] = 65536
						if fixed, err := json.Marshal(model); err == nil {
							rawModel = fixed
						}
					}
				}
			}
			// Fix reasoning_options for nemotron models: models.dev only provides
			// "toggle" type, but tau needs "effort" type with values for the UI
			// to offer selectable reasoning levels. Add standard effort levels.
			if nemotronModels[id] {
				var model map[string]any
				if err := json.Unmarshal(rawModel, &model); err == nil {
					if reasoningOpts, ok := model["reasoning_options"].([]any); ok {
						hasEffort := false
						for _, opt := range reasoningOpts {
							if optMap, ok := opt.(map[string]any); ok {
								if optMap["type"] == "effort" {
									hasEffort = true
									break
								}
							}
						}
						if !hasEffort {
							reasoningOpts = append(reasoningOpts, map[string]any{
								"type":   "effort",
								"values": []string{"low", "medium", "high"},
							})
							model["reasoning_options"] = reasoningOpts
							if fixed, err := json.Marshal(model); err == nil {
								rawModel = fixed
							}
						}
					}
				}
			}
			models[id] = rawModel
			kept++
		}
		if len(models) == 0 {
			continue
		}
		// Re-key under tau's provider id so rt.Models(tauID) resolves.
		snapshot[entry.ID] = provider{ID: entry.ID, Models: models}
	}

	if err := writeSnapshot(*out, snapshot); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s: %d providers, %d tool-capable models (dropped %d non-tool)\n",
		*out, len(snapshot), kept, dropped)
}

func loadCatalog(input string) ([]byte, error) {
	if input != "" {
		return os.ReadFile(input)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", catalogURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", catalogURL, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// writeSnapshot marshals the snapshot deterministically (sorted keys via the
// encoder) so regenerating without changes produces no diff.
func writeSnapshot(path string, snapshot map[string]provider) error {
	// json.Marshal sorts map keys, but model maps inside are also sorted by the
	// encoder; build an ordered top-level for a stable, readable file.
	ids := make([]string, 0, len(snapshot))
	for id := range snapshot {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var buf []byte
	buf = append(buf, "{\n"...)
	for i, id := range ids {
		key, _ := json.Marshal(id)
		body, err := json.MarshalIndent(snapshot[id], "  ", "  ")
		if err != nil {
			return err
		}
		buf = append(buf, "  "...)
		buf = append(buf, key...)
		buf = append(buf, ": "...)
		buf = append(buf, body...)
		if i < len(ids)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, '\n')
	}
	buf = append(buf, "}\n"...)
	return os.WriteFile(path, buf, 0o644)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "snapshot gen:", err)
	os.Exit(1)
}
