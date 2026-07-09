package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

const codexModelsTimeout = 8 * time.Second

func codexModelRefs(ctx context.Context, provider tauconfig.ProviderConfig, insecure bool) ([]tauchat.ChatModelRef, error) {
	models, err := codexModels(ctx, provider, insecure)
	if err != nil {
		return nil, err
	}
	return codexModelRefsFromInfos(provider, models), nil
}

func codexModelRefsFromInfos(provider tauconfig.ProviderConfig, models []codexModelInfo) []tauchat.ChatModelRef {
	refs := make([]tauchat.ChatModelRef, 0, len(models))
	for _, m := range models {
		id := strings.TrimSpace(firstNonEmpty(m.Slug, m.ID, m.Model))
		if id == "" {
			continue
		}
		cfg := tauconfig.ModelConfig{
			ID:               id,
			Name:             firstNonEmpty(m.DisplayName, m.Name, id),
			ContextWindow:    firstNonZero(m.ContextWindow, m.MaxContextWindow),
			DefaultMaxTokens: firstNonZero(m.MaxOutputTokens, m.OutputTokens),
			MaxTokens:        firstNonZero(m.MaxOutputTokens, m.OutputTokens),
			Reasoning:        true,
			ReasoningEffort:  normalizeCodexEffort(m.DefaultReasoningLevel),
			ReasoningEfforts: normalizeCodexEfforts(m.SupportedReasoningLevels),
		}
		refs = append(refs, tauchat.ChatModelRef{
			ID:       id,
			URL:      strings.TrimRight(provider.BaseURL, "/"),
			Provider: provider.Name,
			Config:   cfg,
		})
	}
	return refs
}

func codexModels(ctx context.Context, provider tauconfig.ProviderConfig, insecure bool) ([]codexModelInfo, error) {
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/codex/models"
	ctx, cancel := context.WithTimeout(ctx, codexModelsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if token := providerAPIKey(provider); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tau")
	for k, v := range provider.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}

	client := &http.Client{Timeout: codexModelsTimeout}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in via --insecure
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list Codex models: status %d", resp.StatusCode)
	}

	var payload codexModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode Codex models: %w", err)
	}
	return payload.Models, nil
}

type codexModelsResponse struct {
	Models []codexModelInfo `json:"models"`
}

type codexModelInfo struct {
	ID                       string   `json:"id"`
	Slug                     string   `json:"slug"`
	Model                    string   `json:"model"`
	Name                     string   `json:"name"`
	DisplayName              string   `json:"display_name"`
	ContextWindow            int      `json:"context_window"`
	MaxContextWindow         int      `json:"max_context_window"`
	MaxOutputTokens          int      `json:"max_output_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	DefaultReasoningLevel    string   `json:"default_reasoning_level"`
	SupportedReasoningLevels []string `json:"supported_reasoning_levels"`
}

func normalizeCodexEfforts(levels []string) []string {
	if len(levels) == 0 {
		return []string{"low", "medium", "high", "xhigh"}
	}
	out := make([]string, 0, len(levels))
	for _, level := range levels {
		level = normalizeCodexEffort(level)
		if level == "" {
			continue
		}
		out = append(out, level)
	}
	if len(out) == 0 {
		return []string{"low", "medium", "high", "xhigh"}
	}
	return out
}

func normalizeCodexEffort(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "default":
		return ""
	case "minimal":
		return "low"
	case "max", "ultra", "extra-high", "extra_high", "x-high":
		return "xhigh"
	default:
		return strings.ToLower(strings.TrimSpace(level))
	}
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
