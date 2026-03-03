package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// OllamaGenerateRequest represents a request to generate chat text via the ollama plugin.
type OllamaGenerateRequest struct {
	Channel        string            `json:"channel"`
	Username       string            `json:"username"`
	Message        string            `json:"message"`
	SystemPrompt   string            `json:"system_prompt,omitempty"`
	Model          string            `json:"model,omitempty"`
	Temperature    float64           `json:"temperature,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	IncludeHistory bool              `json:"include_history"`
	HistoryLimit   int               `json:"history_limit,omitempty"`
	ExtraContext   map[string]string `json:"extra_context,omitempty"`
	EnableFollowUp bool              `json:"enable_follow_up"`
}

// OllamaGenerateResponse is the shared response type for generated text.
type OllamaGenerateResponse struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
}

// OllamaError wraps an error returned from the ollama plugin.
type OllamaError struct {
	Message string `json:"error"`
}

func (e *OllamaError) Error() string {
	return fmt.Sprintf("ollama error: %s", e.Message)
}

// OllamaClient provides a shared API for plugins to request ollama completions.
type OllamaClient struct {
	source string
	helper *RequestHelper
}

// NewOllamaClient creates a new ollama client bound to the provided source plugin.
func NewOllamaClient(eventBus EventBus, source string) *OllamaClient {
	return &OllamaClient{
		source: source,
		helper: NewRequestHelper(eventBus, source),
	}
}

// Generate sends a message to the ollama plugin and returns generated text.
func (c *OllamaClient) Generate(ctx context.Context, req OllamaGenerateRequest) (OllamaGenerateResponse, error) {
	var response OllamaGenerateResponse

	rawPayload, err := json.Marshal(req)
	if err != nil {
		return response, fmt.Errorf("failed to marshal request: %w", err)
	}

	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())
	eventData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   correlationID,
			From: c.source,
			To:   "ollama",
			Type: "generate",
			Data: &RequestData{
				RawJSON: rawPayload,
			},
		},
	}

	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        c.source,
		Target:        "ollama",
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		metadata.Timeout = timeout
	}

	respEvent, err := c.helper.RequestWithConfig(ctx, "ollama", "plugin.request", eventData, configForTimeout(metadata.Timeout))
	if err != nil {
		return response, fmt.Errorf("ollama request failed: %w", err)
	}
	if respEvent == nil || respEvent.PluginResponse == nil {
		return response, fmt.Errorf("no plugin response received")
	}

	pluginResp := respEvent.PluginResponse
	if !pluginResp.Success {
		if pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
			var pluginErr OllamaError
			if err := json.Unmarshal(pluginResp.Data.RawJSON, &pluginErr); err == nil && pluginErr.Message != "" {
				return response, &pluginErr
			}
		}
		if pluginResp.Error != "" {
			return response, fmt.Errorf("plugin request failed: %s", pluginResp.Error)
		}
		return response, fmt.Errorf("plugin request failed")
	}

	if pluginResp.Data == nil || len(pluginResp.Data.RawJSON) == 0 {
		return response, fmt.Errorf("empty plugin response")
	}

	if err := json.Unmarshal(pluginResp.Data.RawJSON, &response); err != nil {
		return response, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return response, nil
}
