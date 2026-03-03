package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// PlayerStateError represents a structured error from the playerstate plugin.
type PlayerStateError struct {
	ErrorCode string          `json:"error_code"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
}

func (e *PlayerStateError) Error() string {
	return fmt.Sprintf("playerstate error (%s): %s", e.ErrorCode, e.Message)
}

// PlayerState represents bladder/alcohol/weed/food/lust metrics.
type PlayerState struct {
	Bladder int64 `json:"bladder"`
	Alcohol int64 `json:"alcohol"`
	Weed    int64 `json:"weed"`
	Food    int64 `json:"food"`
	Lust    int64 `json:"lust"`
}

// PlayerStateRequest represents a request for player state.
type PlayerStateRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
}

// PlayerStateMutationRequest represents a set/adjust request payload.
type PlayerStateMutationRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`

	Bladder *int64 `json:"bladder"`
	Alcohol *int64 `json:"alcohol"`
	Weed    *int64 `json:"weed"`
	Food    *int64 `json:"food"`
	Lust    *int64 `json:"lust"`
}

// PlayerStateResponse mirrors state returned from the playerstate plugin.
type PlayerStateResponse struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
	PlayerState
}

// PlayerStateClient provides a high-level API for playerstate operations.
type PlayerStateClient struct {
	source string
	helper *RequestHelper
}

// NewPlayerStateClient creates a new playerstate client.
func NewPlayerStateClient(eventBus EventBus, source string) *PlayerStateClient {
	return &PlayerStateClient{
		source: source,
		helper: NewRequestHelper(eventBus, source),
	}
}

// Get retrieves player state for a channel/username.
func (c *PlayerStateClient) Get(ctx context.Context, channel, username string) (PlayerState, error) {
	reqPayload := PlayerStateRequest{
		Channel:  channel,
		Username: username,
	}

	var resp PlayerStateResponse
	if err := c.doRequest(ctx, "playerstate.get", reqPayload, &resp); err != nil {
		return PlayerState{}, err
	}

	return resp.PlayerState, nil
}

// Set overwrites provided player state fields and leaves unspecified fields untouched.
func (c *PlayerStateClient) Set(ctx context.Context, req PlayerStateMutationRequest) (*PlayerState, error) {
	var resp PlayerStateResponse
	if err := c.doRequest(ctx, "playerstate.set", req, &resp); err != nil {
		return nil, err
	}
	return &resp.PlayerState, nil
}

// Adjust increments/decrements provided fields; values are clamped to non-negative internally.
func (c *PlayerStateClient) Adjust(ctx context.Context, req PlayerStateMutationRequest) (*PlayerState, error) {
	var resp PlayerStateResponse
	if err := c.doRequest(ctx, "playerstate.adjust", req, &resp); err != nil {
		return nil, err
	}
	return &resp.PlayerState, nil
}

// SetBladder overwrites only bladder value.
func (c *PlayerStateClient) SetBladder(ctx context.Context, channel, username string, value int64) (*PlayerState, error) {
	return c.Set(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Bladder:  int64Ptr(value),
	})
}

// AdjustBladder increments or decrements bladder and clamps at zero.
func (c *PlayerStateClient) AdjustBladder(ctx context.Context, channel, username string, delta int64) (*PlayerState, error) {
	return c.Adjust(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Bladder:  int64Ptr(delta),
	})
}

// SetAlcohol overwrites only alcohol value.
func (c *PlayerStateClient) SetAlcohol(ctx context.Context, channel, username string, value int64) (*PlayerState, error) {
	return c.Set(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Alcohol:  int64Ptr(value),
	})
}

// AdjustAlcohol increments or decrements alcohol and clamps at zero.
func (c *PlayerStateClient) AdjustAlcohol(ctx context.Context, channel, username string, delta int64) (*PlayerState, error) {
	return c.Adjust(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Alcohol:  int64Ptr(delta),
	})
}

// SetWeed overwrites only weed value.
func (c *PlayerStateClient) SetWeed(ctx context.Context, channel, username string, value int64) (*PlayerState, error) {
	return c.Set(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Weed:     int64Ptr(value),
	})
}

// AdjustWeed increments or decrements weed and clamps at zero.
func (c *PlayerStateClient) AdjustWeed(ctx context.Context, channel, username string, delta int64) (*PlayerState, error) {
	return c.Adjust(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Weed:     int64Ptr(delta),
	})
}

// SetFood overwrites only food value.
func (c *PlayerStateClient) SetFood(ctx context.Context, channel, username string, value int64) (*PlayerState, error) {
	return c.Set(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Food:     int64Ptr(value),
	})
}

// AdjustFood increments or decrements food and clamps at zero.
func (c *PlayerStateClient) AdjustFood(ctx context.Context, channel, username string, delta int64) (*PlayerState, error) {
	return c.Adjust(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Food:     int64Ptr(delta),
	})
}

// SetLust overwrites only lust value.
func (c *PlayerStateClient) SetLust(ctx context.Context, channel, username string, value int64) (*PlayerState, error) {
	return c.Set(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Lust:     int64Ptr(value),
	})
}

// AdjustLust increments or decrements lust and clamps at zero.
func (c *PlayerStateClient) AdjustLust(ctx context.Context, channel, username string, delta int64) (*PlayerState, error) {
	return c.Adjust(ctx, PlayerStateMutationRequest{
		Channel:  channel,
		Username: username,
		Lust:     int64Ptr(delta),
	})
}

func (c *PlayerStateClient) doRequest(ctx context.Context, opType string, payload any, result any) error {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())
	eventData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   correlationID,
			From: c.source,
			To:   "playerstate",
			Type: opType,
			Data: &RequestData{
				RawJSON: rawPayload,
			},
		},
	}

	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        c.source,
		Target:        "playerstate",
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		metadata.Timeout = timeout
	}

	respEvent, err := c.helper.RequestWithConfig(ctx, "playerstate", "plugin.request", eventData, configForTimeout(metadata.Timeout))
	if err != nil {
		return err
	}
	if respEvent == nil || respEvent.PluginResponse == nil {
		return fmt.Errorf("no plugin response received")
	}
	pluginResp := respEvent.PluginResponse
	if !pluginResp.Success {
		if pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
			var stateErr PlayerStateError
			if err := json.Unmarshal(pluginResp.Data.RawJSON, &stateErr); err == nil && stateErr.ErrorCode != "" {
				return &stateErr
			}
		}
		return fmt.Errorf("plugin request failed: %s", pluginResp.Error)
	}

	if result != nil && pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
		if err := json.Unmarshal(pluginResp.Data.RawJSON, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

func configForTimeout(timeout time.Duration) string {
	switch {
	case timeout <= 0:
		return "normal"
	case timeout <= 5*time.Second:
		return "fast"
	case timeout <= 15*time.Second:
		return "normal"
	case timeout <= 30*time.Second:
		return "slow"
	default:
		return "critical"
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
