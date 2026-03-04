package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const speechFlavorTarget = "speechflavor"

// SpeechFlavorRewriteRequest describes a rewrite request to the speechflavor plugin.
type SpeechFlavorRewriteRequest struct {
	Channel        string   `json:"channel,omitempty"`
	Username       string   `json:"username,omitempty"`
	Text           string   `json:"text"`
	PreserveTokens *bool    `json:"preserve_tokens,omitempty"`
	TimeoutMS      int      `json:"timeout_ms,omitempty"`
	Model          string   `json:"model,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
}

// SpeechFlavorRewriteResponse returns rewritten text and guard fallback metadata.
type SpeechFlavorRewriteResponse struct {
	Text         string `json:"text"`
	Model        string `json:"model,omitempty"`
	FallbackUsed bool   `json:"fallback_used"`
	Reason       string `json:"reason,omitempty"`
}

// SpeechFlavorError wraps structured plugin errors.
type SpeechFlavorError struct {
	ErrorCode string          `json:"error_code"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
}

func (e *SpeechFlavorError) Error() string {
	return fmt.Sprintf("speechflavor error (%s): %s", e.ErrorCode, e.Message)
}

// SpeechFlavorClient provides request/reply helpers for the speechflavor plugin.
type SpeechFlavorClient struct {
	source string
	helper *RequestHelper
}

// NewSpeechFlavorClient creates a new speechflavor client.
func NewSpeechFlavorClient(eventBus EventBus, source string) *SpeechFlavorClient {
	return &SpeechFlavorClient{
		source: source,
		helper: NewRequestHelper(eventBus, source),
	}
}

// Rewrite rewrites text in character style while preserving meaning/guards.
func (c *SpeechFlavorClient) Rewrite(ctx context.Context, req SpeechFlavorRewriteRequest) (SpeechFlavorRewriteResponse, error) {
	var result SpeechFlavorRewriteResponse

	rawPayload, err := json.Marshal(req)
	if err != nil {
		return result, fmt.Errorf("failed to marshal payload: %w", err)
	}

	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())
	eventData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   correlationID,
			From: c.source,
			To:   speechFlavorTarget,
			Type: "rewrite",
			Data: &RequestData{
				RawJSON: rawPayload,
			},
		},
	}

	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        c.source,
		Target:        speechFlavorTarget,
	}
	if deadline, ok := ctx.Deadline(); ok {
		metadata.Timeout = time.Until(deadline)
	}

	respEvent, err := c.helper.RequestWithConfig(
		ctx,
		speechFlavorTarget,
		"plugin.request",
		eventData,
		configForTimeout(metadata.Timeout),
	)
	if err != nil {
		return result, err
	}
	if respEvent == nil || respEvent.PluginResponse == nil {
		return result, fmt.Errorf("no plugin response received")
	}

	pluginResp := respEvent.PluginResponse
	if !pluginResp.Success {
		if pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
			var flErr SpeechFlavorError
			if err := json.Unmarshal(pluginResp.Data.RawJSON, &flErr); err == nil && flErr.ErrorCode != "" {
				return result, &flErr
			}
		}
		return result, fmt.Errorf("plugin request failed: %s", pluginResp.Error)
	}

	if pluginResp.Data == nil || len(pluginResp.Data.RawJSON) == 0 {
		return result, fmt.Errorf("empty plugin response")
	}
	if err := json.Unmarshal(pluginResp.Data.RawJSON, &result); err != nil {
		return result, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return result, nil
}
