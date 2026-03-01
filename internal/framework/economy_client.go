package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// EconomyError represents a structured error from the economy plugin
type EconomyError struct {
	ErrorCode string          `json:"error_code"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
}

func (e *EconomyError) Error() string {
	return fmt.Sprintf("economy error (%s): %s", e.ErrorCode, e.Message)
}

// EconomyClient provides a high-level API for interacting with the economy plugin
type EconomyClient struct {
	eventBus EventBus
	source   string
	helper   *RequestHelper
}

// NewEconomyClient creates a new economy client
func NewEconomyClient(eventBus EventBus, source string) *EconomyClient {
	return &EconomyClient{
		eventBus: eventBus,
		source:   source,
		helper:   NewRequestHelper(eventBus, source),
	}
}

// GetBalanceRequest represents the request payload for economy.get_balance
type GetBalanceRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
}

// GetBalanceResponse represents the response payload for economy.get_balance
type GetBalanceResponse struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
	Balance  int64  `json:"balance"`
}

// CreditRequest represents the request payload for economy.credit
type CreditRequest struct {
	Channel        string          `json:"channel"`
	Username       string          `json:"username"`
	Amount         int64           `json:"amount"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// CreditResponse represents the response payload for economy.credit
type CreditResponse struct {
	Channel        string `json:"channel"`
	Username       string `json:"username"`
	Amount         int64  `json:"amount"`
	BalanceBefore  int64  `json:"balance_before"`
	BalanceAfter   int64  `json:"balance_after"`
	IdempotencyKey string `json:"idempotency_key"`
	AlreadyApplied bool   `json:"already_applied"`
}

// DebitRequest represents the request payload for economy.debit
type DebitRequest struct {
	Channel        string          `json:"channel"`
	Username       string          `json:"username"`
	Amount         int64           `json:"amount"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// DebitResponse represents the response payload for economy.debit
type DebitResponse struct {
	Channel        string `json:"channel"`
	Username       string `json:"username"`
	Amount         int64  `json:"amount"`
	BalanceBefore  int64  `json:"balance_before"`
	BalanceAfter   int64  `json:"balance_after"`
	IdempotencyKey string `json:"idempotency_key"`
	AlreadyApplied bool   `json:"already_applied"`
}

// TransferRequest represents the request payload for economy.transfer
type TransferRequest struct {
	Channel        string          `json:"channel"`
	FromUsername   string          `json:"from_username"`
	ToUsername     string          `json:"to_username"`
	Amount         int64           `json:"amount"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// TransferResponse represents the response payload for economy.transfer
type TransferResponse struct {
	Channel           string `json:"channel"`
	FromUsername      string `json:"from_username"`
	ToUsername        string `json:"to_username"`
	Amount            int64  `json:"amount"`
	FromBalanceBefore int64  `json:"from_balance_before"`
	FromBalanceAfter  int64  `json:"from_balance_after"`
	ToBalanceBefore   int64  `json:"to_balance_before"`
	ToBalanceAfter    int64  `json:"to_balance_after"`
	IdempotencyKey    string `json:"idempotency_key"`
	AlreadyApplied    bool   `json:"already_applied"`
}

// GetBalance retrieves the balance for a user in a channel
func (c *EconomyClient) GetBalance(ctx context.Context, channel, username string) (int64, error) {
	reqPayload := GetBalanceRequest{
		Channel:  channel,
		Username: username,
	}

	var respPayload GetBalanceResponse
	err := c.doRequest(ctx, "economy.get_balance", reqPayload, &respPayload)
	if err != nil {
		return 0, err
	}

	return respPayload.Balance, nil
}

// Credit adds funds to a user's balance
func (c *EconomyClient) Credit(ctx context.Context, req CreditRequest) (*CreditResponse, error) {
	var resp CreditResponse
	err := c.doRequest(ctx, "economy.credit", &req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Debit removes funds from a user's balance
func (c *EconomyClient) Debit(ctx context.Context, req DebitRequest) (*DebitResponse, error) {
	var resp DebitResponse
	err := c.doRequest(ctx, "economy.debit", &req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Transfer moves funds from one user to another
func (c *EconomyClient) Transfer(ctx context.Context, req TransferRequest) (*TransferResponse, error) {
	var resp TransferResponse
	err := c.doRequest(ctx, "economy.transfer", &req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *EconomyClient) doRequest(ctx context.Context, opType string, payload any, result any) error {
	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())

	// Handle idempotency defaults for mutation operations
	if opType != "economy.get_balance" {
		switch p := payload.(type) {
		case *CreditRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
			}
		case *DebitRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
			}
		case *TransferRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
			}
		case CreditRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
				payload = p
			}
		case DebitRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
				payload = p
			}
		case TransferRequest:
			if p.IdempotencyKey == "" {
				p.IdempotencyKey = correlationID
				payload = p
			}
		}
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	data := &EventData{
		PluginRequest: &PluginRequest{
			ID:   correlationID,
			From: c.source,
			To:   "economy",
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
		Target:        "economy",
	}

	// Determine timeout from context
	configName := "normal"
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		switch {
		case timeout <= 5*time.Second:
			configName = "fast"
		case timeout <= 15*time.Second:
			configName = "normal"
		case timeout <= 30*time.Second:
			configName = "slow"
		default:
			configName = "critical"
		}
		metadata.Timeout = timeout
	}

	respEvent, err := c.helper.RequestWithConfig(ctx, "economy", "plugin.request", data, configName)
	if err != nil {
		return err
	}

	if respEvent == nil || respEvent.PluginResponse == nil {
		return fmt.Errorf("no plugin response received")
	}

	pluginResp := respEvent.PluginResponse
	if !pluginResp.Success {
		// Try to parse structured error from RawJSON
		if pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
			var econErr EconomyError
			if json.Unmarshal(pluginResp.Data.RawJSON, &econErr) == nil && econErr.ErrorCode != "" {
				return &econErr
			}
		}
		return fmt.Errorf("plugin request failed: %s", pluginResp.Error)
	}

	if result != nil && pluginResp.Data != nil && len(pluginResp.Data.RawJSON) > 0 {
		if err := json.Unmarshal(pluginResp.Data.RawJSON, result); err != nil {
			return fmt.Errorf("failed to unmarshal response data: %w", err)
		}
	}

	return nil
}
