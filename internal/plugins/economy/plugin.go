package economy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName               = "economy"
	errorCodeInvalidArg      = "INVALID_ARGUMENT"
	errorCodeInvalidAmount   = "INVALID_AMOUNT"
	errorCodeInsufficient    = "INSUFFICIENT_FUNDS"
	errorCodeIdempotency     = "IDEMPOTENCY_CONFLICT"
	errorCodeDBUnavailable   = "DB_UNAVAILABLE"
	errorCodeDBError         = "DB_ERROR"
	errorCodeInternal        = "INTERNAL"
	operationGetBalance      = "economy.get_balance"
	operationCredit          = "economy.credit"
	operationDebit           = "economy.debit"
	operationTransfer        = "economy.transfer"
	stateInitialized         = "initialized"
	stateRunning             = "running"
	stateStopped             = "stopped"
	nilResponseErrorTemplate = "operation %s failed"
)

type Plugin struct {
	mu        sync.RWMutex
	eventBus  framework.EventBus
	store     Store
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	readyChan chan struct{}
	status    framework.PluginStatus
}

type getBalanceRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
}

type creditRequest struct {
	Channel        string                 `json:"channel"`
	Username       string                 `json:"username"`
	Amount         int64                  `json:"amount"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Reason         string                 `json:"reason"`
	Metadata       map[string]interface{} `json:"metadata"`
}

type debitRequest struct {
	Channel        string                 `json:"channel"`
	Username       string                 `json:"username"`
	Amount         int64                  `json:"amount"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Reason         string                 `json:"reason"`
	Metadata       map[string]interface{} `json:"metadata"`
}

type transferRequest struct {
	Channel        string                 `json:"channel"`
	FromUsername   string                 `json:"from_username"`
	ToUsername     string                 `json:"to_username"`
	Amount         int64                  `json:"amount"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Reason         string                 `json:"reason"`
	Metadata       map[string]interface{} `json:"metadata"`
}

func New() framework.Plugin {
	return &Plugin{
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  pluginName,
			State: stateInitialized,
		},
	}
}

func (p *Plugin) Name() string {
	return pluginName
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	_ = config
	p.eventBus = bus
	if p.store == nil {
		sqlHelper := framework.NewSQLRequestHelper(bus, pluginName)
		p.store = NewSQLStore(sqlHelper)
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.status = framework.PluginStatus{
		Name:  pluginName,
		State: stateInitialized,
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	if err := p.eventBus.Subscribe(eventbus.EventPluginRequest, p.handlePluginRequest); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to plugin.request: %w", err)
		return p.status.LastError
	}

	p.running = true
	p.status.State = stateRunning
	close(p.readyChan)

	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		p.status.State = stateStopped
		return nil
	}

	p.running = false
	p.status.State = stateStopped
	if p.cancel != nil {
		p.cancel()
	}

	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	_ = event
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

func (p *Plugin) handlePluginRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req == nil {
		return nil
	}

	if req.To != pluginName {
		return nil
	}

	if req.ID == "" {
		return nil
	}

	switch req.Type {
	case operationGetBalance:
		p.handleGetBalance(req)
		return nil
	case operationCredit:
		p.handleCredit(req)
		return nil
	case operationDebit:
		p.handleDebit(req)
		return nil
	case operationTransfer:
		p.handleTransfer(req)
		return nil
	default:
		p.deliverError(req, errorCodeInvalidArg, fmt.Sprintf("unknown operation: %s", req.Type), nil)
		return nil
	}
}

func (p *Plugin) handleGetBalance(req *framework.PluginRequest) {
	var payload getBalanceRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}

	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field username", map[string]interface{}{"field": "username"})
		return
	}

	result, err := p.store.GetBalance(p.operationContext(), payload.Channel, payload.Username)
	if p.deliverStoreError(req, operationGetBalance, err) {
		return
	}

	if !result.OK {
		p.deliverOperationError(req, result.ErrorCode, operationGetBalance)
		return
	}

	p.deliverSuccess(req, map[string]interface{}{
		"channel":  payload.Channel,
		"username": payload.Username,
		"balance":  result.Balance,
	})
}

func (p *Plugin) handleCredit(req *framework.PluginRequest) {
	var payload creditRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)
	payload.IdempotencyKey = normalizeOptional(payload.IdempotencyKey)
	payload.Reason = normalizeOptional(payload.Reason)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}

	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field username", map[string]interface{}{"field": "username"})
		return
	}

	if payload.Amount <= 0 {
		p.deliverError(req, errorCodeInvalidAmount, "amount must be greater than 0", map[string]interface{}{"field": "amount"})
		return
	}

	if payload.IdempotencyKey == "" {
		payload.IdempotencyKey = req.ID
	}

	result, err := p.store.Credit(
		p.operationContext(),
		payload.Channel,
		payload.Username,
		payload.Amount,
		payload.IdempotencyKey,
		normalizeOptional(req.From),
		payload.Reason,
		payload.Metadata,
	)
	if p.deliverStoreError(req, operationCredit, err) {
		return
	}

	if !result.OK {
		p.deliverOperationError(req, result.ErrorCode, operationCredit)
		return
	}

	p.deliverSuccess(req, map[string]interface{}{
		"channel":         payload.Channel,
		"username":        payload.Username,
		"amount":          payload.Amount,
		"balance_before":  result.Balance - payload.Amount,
		"balance_after":   result.Balance,
		"idempotency_key": payload.IdempotencyKey,
		"already_applied": result.AlreadyApplied,
	})
}

func (p *Plugin) handleDebit(req *framework.PluginRequest) {
	var payload debitRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)
	payload.IdempotencyKey = normalizeOptional(payload.IdempotencyKey)
	payload.Reason = normalizeOptional(payload.Reason)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}

	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field username", map[string]interface{}{"field": "username"})
		return
	}

	if payload.Amount <= 0 {
		p.deliverError(req, errorCodeInvalidAmount, "amount must be greater than 0", map[string]interface{}{"field": "amount"})
		return
	}

	if payload.IdempotencyKey == "" {
		payload.IdempotencyKey = req.ID
	}

	result, err := p.store.Debit(
		p.operationContext(),
		payload.Channel,
		payload.Username,
		payload.Amount,
		payload.IdempotencyKey,
		normalizeOptional(req.From),
		payload.Reason,
		payload.Metadata,
	)
	if p.deliverStoreError(req, operationDebit, err) {
		return
	}

	if !result.OK {
		p.deliverOperationError(req, result.ErrorCode, operationDebit)
		return
	}

	p.deliverSuccess(req, map[string]interface{}{
		"channel":         payload.Channel,
		"username":        payload.Username,
		"amount":          payload.Amount,
		"balance_before":  result.Balance + payload.Amount,
		"balance_after":   result.Balance,
		"idempotency_key": payload.IdempotencyKey,
		"already_applied": result.AlreadyApplied,
	})
}

func (p *Plugin) handleTransfer(req *framework.PluginRequest) {
	var payload transferRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.FromUsername = normalizeUsername(payload.FromUsername)
	payload.ToUsername = normalizeUsername(payload.ToUsername)
	payload.IdempotencyKey = normalizeOptional(payload.IdempotencyKey)
	payload.Reason = normalizeOptional(payload.Reason)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}

	if payload.FromUsername == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field from_username", map[string]interface{}{"field": "from_username"})
		return
	}

	if payload.ToUsername == "" {
		p.deliverError(req, errorCodeInvalidArg, "missing field to_username", map[string]interface{}{"field": "to_username"})
		return
	}

	if payload.Amount <= 0 {
		p.deliverError(req, errorCodeInvalidAmount, "amount must be greater than 0", map[string]interface{}{"field": "amount"})
		return
	}

	if payload.IdempotencyKey == "" {
		payload.IdempotencyKey = req.ID
	}

	result, err := p.store.Transfer(
		p.operationContext(),
		payload.Channel,
		payload.FromUsername,
		payload.ToUsername,
		payload.Amount,
		payload.IdempotencyKey,
		normalizeOptional(req.From),
		payload.Reason,
		payload.Metadata,
	)
	if p.deliverStoreError(req, operationTransfer, err) {
		return
	}

	if !result.OK {
		p.deliverOperationError(req, result.ErrorCode, operationTransfer)
		return
	}

	p.deliverSuccess(req, map[string]interface{}{
		"channel":             payload.Channel,
		"from_username":       payload.FromUsername,
		"to_username":         payload.ToUsername,
		"amount":              payload.Amount,
		"from_balance_before": result.FromBalance + payload.Amount,
		"from_balance_after":  result.FromBalance,
		"to_balance_before":   result.ToBalance - payload.Amount,
		"to_balance_after":    result.ToBalance,
		"idempotency_key":     payload.IdempotencyKey,
		"already_applied":     result.AlreadyApplied,
	})
}

func (p *Plugin) parseRequest(req *framework.PluginRequest, payload interface{}) bool {
	if req.Data == nil || len(req.Data.RawJSON) == 0 {
		p.deliverError(req, errorCodeInvalidArg, "missing data.raw_json", map[string]interface{}{"field": "data.raw_json"})
		return false
	}

	if err := json.Unmarshal(req.Data.RawJSON, payload); err != nil {
		p.deliverError(req, errorCodeInvalidArg, "invalid request payload", nil)
		return false
	}

	return true
}

func (p *Plugin) operationContext() context.Context {
	if p.ctx != nil {
		return p.ctx
	}

	return context.Background()
}

func (p *Plugin) deliverStoreError(req *framework.PluginRequest, operation string, err error) bool {
	if err == nil {
		return false
	}

	errorCode := mapStoreErrorCode(err)
	p.deliverError(req, errorCode, operationErrorMessage(errorCode, operation), nil)
	return true
}

func (p *Plugin) deliverOperationError(req *framework.PluginRequest, errorCode string, operation string) {
	if errorCode == "" {
		errorCode = errorCodeDBError
	}

	p.deliverError(req, errorCode, operationErrorMessage(errorCode, operation), nil)
}

func (p *Plugin) deliverSuccess(req *framework.PluginRequest, payload map[string]interface{}) {
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		p.deliverError(req, errorCodeInternal, "failed to marshal response payload", nil)
		return
	}

	resp := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    pluginName,
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: rawJSON,
			},
		},
	}

	p.eventBus.DeliverResponse(req.ID, resp, nil)
}

func normalizeChannel(channel string) string {
	return strings.TrimSpace(channel)
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func normalizeOptional(value string) string {
	return strings.TrimSpace(value)
}

func operationErrorMessage(errorCode, operation string) string {
	switch errorCode {
	case errorCodeInvalidArg:
		return "invalid argument"
	case errorCodeInvalidAmount:
		return "invalid amount"
	case errorCodeInsufficient:
		return "insufficient funds"
	case errorCodeIdempotency:
		return "idempotency conflict"
	case errorCodeDBUnavailable:
		return "database unavailable"
	case errorCodeDBError:
		return "database error"
	case errorCodeInternal:
		return "internal error"
	default:
		return fmt.Sprintf(nilResponseErrorTemplate, operation)
	}
}

func (p *Plugin) deliverError(req *framework.PluginRequest, errorCode, message string, details map[string]interface{}) {
	errorPayload := map[string]interface{}{
		"error_code": errorCode,
		"message":    message,
	}
	if details != nil {
		errorPayload["details"] = details
	}

	rawJSON, err := json.Marshal(errorPayload)
	if err != nil {
		rawJSON = []byte(`{"error_code":"INTERNAL","message":"failed to marshal error payload"}`)
		message = "failed to marshal error payload"
	}

	resp := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    pluginName,
			Success: false,
			Error:   message,
			Data: &framework.ResponseData{
				RawJSON: rawJSON,
			},
		},
	}

	p.eventBus.DeliverResponse(req.ID, resp, nil)
}
