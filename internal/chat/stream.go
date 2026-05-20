package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/platform"
)

const defaultStreamTimeout = 120 * time.Second

type OpenAIStreamer struct {
	Insecure bool
	Timeout  time.Duration
}

func (s OpenAIStreamer) StreamChatCompletion(
	ctx context.Context,
	session ChatSessionState,
	maasToken string,
	onDelta func(string) error,
) (CompletionResult, error) {
	requestBody, err := json.Marshal(session.CompletionRequest())
	if err != nil {
		return CompletionResult{}, fmt.Errorf("encoding chat request: %w", err)
	}

	url := strings.TrimRight(session.Model.URL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return CompletionResult{}, fmt.Errorf("creating chat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+maasToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := platform.NewHTTPClientWithTimeout(s.Insecure, s.timeout())
	resp, err := client.Do(req)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return CompletionResult{}, fmt.Errorf("chat endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return s.readStream(ctx, resp.Body, onDelta)
}

func (s OpenAIStreamer) timeout() time.Duration {
	if s.Timeout <= 0 {
		return defaultStreamTimeout
	}
	return s.Timeout
}

func (s OpenAIStreamer) readStream(
	ctx context.Context,
	body io.Reader,
	onDelta func(string) error,
) (CompletionResult, error) {
	result := CompletionResult{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return result, nil
		}

		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return result, fmt.Errorf("invalid chat stream chunk: %w", err)
		}

		if chunk.Usage != nil {
			result.Usage = *chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				result.FinishReason = *choice.FinishReason
			}
			if choice.Delta.Content == "" {
				continue
			}
			if err := onDelta(choice.Delta.Content); err != nil {
				return result, err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("reading chat stream: %w", err)
	}

	return result, nil
}
