package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aisdkchat "github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/runtime"
)

type codexClass struct{}

func (codexClass) Name() string { return "openai-codex" }

func (codexClass) Supports(cap runtime.Capability) bool {
	return cap == runtime.CapabilityChat
}

func (codexClass) New(ctx context.Context, cfg runtime.ProviderConfig, model runtime.ModelInfo) (runtime.ProviderSet, error) {
	auth, err := runtime.ResolveAPIKey(cfg)
	if err != nil {
		return runtime.ProviderSet{}, err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	if cfg.Insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in via --insecure
	}
	return runtime.ProviderSet{Chat: &codexProvider{
		apiKey:  auth,
		baseURL: strings.TrimRight(modelURL(model, cfg.BaseURL), "/"),
		headers: cfg.Headers,
		client:  client,
	}}, nil
}

type codexProvider struct {
	apiKey  string
	baseURL string
	headers map[string]string
	client  *http.Client
}

func (p *codexProvider) Name() string { return "openai-codex" }

func (p *codexProvider) Chat(ctx context.Context, req aisdkchat.Request) (aisdkchat.Response, error) {
	stream, err := p.ChatStream(ctx, req)
	if err != nil {
		return aisdkchat.Response{}, err
	}
	defer stream.Close()

	resp := aisdkchat.Response{Role: aisdkchat.RoleAssistant}
	var deltas []aisdkchat.ToolCallDelta
	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return resp, err
		}
		resp.Content += chunk.Delta
		resp.FinishReason = firstNonEmpty(resp.FinishReason, chunk.FinishReason)
		if chunk.Usage != nil {
			resp.Usage = *chunk.Usage
		}
		deltas = append(deltas, chunk.ToolCallDeltas...)
	}
	resp.ToolCalls = aisdkchat.AssembleToolCalls(deltas)
	return resp, nil
}

func (p *codexProvider) ChatStream(ctx context.Context, req aisdkchat.Request) (aisdkchat.Stream, error) {
	body, err := codexRequestBody(req)
	if err != nil {
		return nil, err
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("codex: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/codex/responses", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("codex: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range p.headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			httpReq.Header.Set(k, v)
		}
	}
	if headers, ok := aisdkchat.ContextHeaders(ctx); ok {
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("codex: http do: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("codex: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return &codexStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
}

func codexRequestBody(req aisdkchat.Request) (map[string]any, error) {
	body := map[string]any{
		"model":  req.Model,
		"stream": true,
		"input":  codexInput(req.Messages),
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			})
		}
		body["tools"] = tools
	}
	return body, nil
}

func codexInput(messages []aisdkchat.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		item := map[string]any{
			"role": string(msg.Role),
		}
		switch msg.Role {
		case aisdkchat.RoleTool:
			item["type"] = "function_call_output"
			item["call_id"] = msg.ToolCallID
			item["output"] = msg.Content
		default:
			item["content"] = msg.Content
		}
		if len(msg.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				calls = append(calls, map[string]any{
					"type":      "function_call",
					"call_id":   call.ID,
					"name":      call.Name,
					"arguments": call.Arguments,
				})
			}
			item["tool_calls"] = calls
		}
		out = append(out, item)
	}
	return out
}

type codexStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	closed  bool
}

func (s *codexStream) Next(_ context.Context) (aisdkchat.Chunk, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return aisdkchat.Chunk{Done: true}, nil
		}
		chunk, ok := codexSSEChunk([]byte(data))
		if ok {
			return chunk, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return aisdkchat.Chunk{}, err
	}
	return aisdkchat.Chunk{}, io.EOF
}

func (s *codexStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

func codexSSEChunk(data []byte) (aisdkchat.Chunk, bool) {
	var event struct {
		Type      string `json:"type"`
		Delta     string `json:"delta"`
		Text      string `json:"text"`
		Output    string `json:"output"`
		ItemID    string `json:"item_id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Index     int    `json:"output_index"`
		Usage     *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
		Response *struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return aisdkchat.Chunk{}, false
	}
	chunk := aisdkchat.Chunk{}
	switch event.Type {
	case "response.output_text.delta", "response.text.delta":
		chunk.Delta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.reasoning_text.delta", "response.reasoning.delta":
		chunk.ReasoningDelta = firstNonEmpty(event.Delta, event.Text, event.Output)
	case "response.function_call_arguments.delta":
		chunk.ToolCallDeltas = []aisdkchat.ToolCallDelta{{
			Index:     event.Index,
			ID:        firstNonEmpty(event.CallID, event.ItemID),
			Name:      event.Name,
			ArgsDelta: firstNonEmpty(event.Delta, event.Arguments),
		}}
	case "response.output_item.added":
		if event.Name != "" || event.CallID != "" {
			chunk.ToolCallDeltas = []aisdkchat.ToolCallDelta{{
				Index: event.Index,
				ID:    firstNonEmpty(event.CallID, event.ItemID),
				Name:  event.Name,
			}}
		}
	case "response.completed":
		chunk.Done = true
		chunk.FinishReason = "stop"
	}
	if event.Usage != nil {
		chunk.Usage = &aisdkchat.Usage{
			PromptTokens:     event.Usage.InputTokens,
			CompletionTokens: event.Usage.OutputTokens,
			TotalTokens:      event.Usage.TotalTokens,
		}
	}
	if chunk.Usage == nil && event.Response != nil && event.Response.Usage != nil {
		chunk.Usage = &aisdkchat.Usage{
			PromptTokens:     event.Response.Usage.InputTokens,
			CompletionTokens: event.Response.Usage.OutputTokens,
			TotalTokens:      event.Response.Usage.TotalTokens,
		}
	}
	return chunk, chunk.Delta != "" || chunk.ReasoningDelta != "" || len(chunk.ToolCallDeltas) > 0 || chunk.Done || chunk.Usage != nil
}

func modelURL(model runtime.ModelInfo, baseURL string) string {
	if strings.TrimSpace(model.URL) != "" {
		return model.URL
	}
	return baseURL
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
