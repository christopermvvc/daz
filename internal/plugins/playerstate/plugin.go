package playerstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName                 = "playerstate"
	stateInitialized           = "initialized"
	stateRunning               = "running"
	stateStopped               = "stopped"
	operationGetPlayerState    = "playerstate.get"
	operationSetPlayerState    = "playerstate.set"
	operationAdjustPlayerState = "playerstate.adjust"
	errorCodeInvalidArgument   = "INVALID_ARGUMENT"
	errorCodeDBUnavailable     = "DB_UNAVAILABLE"
	errorCodeDBError           = "DB_ERROR"
	errorCodeInternal          = "INTERNAL"
	nilResponseErrorTemplate   = "operation %s failed"
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
	_ = config
	p.mu.Lock()
	defer p.mu.Unlock()

	p.eventBus = bus
	if p.store == nil {
		sqlClient := framework.NewSQLClient(bus, pluginName)
		p.store = NewStore(sqlClient)
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
	case operationGetPlayerState:
		p.handleGet(req)
		return nil
	case operationSetPlayerState:
		p.handleSet(req)
		return nil
	case operationAdjustPlayerState:
		p.handleAdjust(req)
		return nil
	default:
		p.deliverError(req, errorCodeInvalidArgument, fmt.Sprintf("unknown operation: %s", req.Type), nil)
		return nil
	}
}

func (p *Plugin) handleGet(req *framework.PluginRequest) {
	var payload playerStateRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}
	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field username", map[string]interface{}{"field": "username"})
		return
	}

	state, err := p.store.Get(p.operationContext(), payload.Channel, payload.Username)
	if p.deliverStoreError(req, operationGetPlayerState, err) {
		return
	}

	p.deliverSuccess(req, playerStateToPayload(payload.Channel, payload.Username, state))
}

func (p *Plugin) handleSet(req *framework.PluginRequest) {
	var payload playerStateMutationRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}
	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field username", map[string]interface{}{"field": "username"})
		return
	}
	if !payload.hasAnyValue() {
		p.deliverError(req, errorCodeInvalidArgument, "at least one field is required", map[string]interface{}{"fields": []string{"bladder", "alcohol", "weed", "food", "lust"}})
		return
	}

	state, err := p.store.SetFields(
		p.operationContext(),
		payload.Channel,
		payload.Username,
		payload.Bladder,
		payload.Alcohol,
		payload.Weed,
		payload.Food,
		payload.Lust,
	)
	if p.deliverStoreError(req, operationSetPlayerState, err) {
		return
	}

	p.deliverSuccess(req, playerStateToPayload(payload.Channel, payload.Username, state))
}

func (p *Plugin) handleAdjust(req *framework.PluginRequest) {
	var payload playerStateMutationRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	payload.Channel = normalizeChannel(payload.Channel)
	payload.Username = normalizeUsername(payload.Username)

	if payload.Channel == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel", map[string]interface{}{"field": "channel"})
		return
	}
	if payload.Username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field username", map[string]interface{}{"field": "username"})
		return
	}
	if !payload.hasAnyValue() {
		p.deliverError(req, errorCodeInvalidArgument, "at least one field is required", map[string]interface{}{"fields": []string{"bladder", "alcohol", "weed", "food", "lust"}})
		return
	}

	state, err := p.store.AdjustFields(
		p.operationContext(),
		payload.Channel,
		payload.Username,
		payload.Bladder,
		payload.Alcohol,
		payload.Weed,
		payload.Food,
		payload.Lust,
	)
	if p.deliverStoreError(req, operationAdjustPlayerState, err) {
		return
	}

	p.deliverSuccess(req, playerStateToPayload(payload.Channel, payload.Username, state))
}

func (p *Plugin) parseRequest(req *framework.PluginRequest, payload interface{}) bool {
	if req.Data == nil || len(req.Data.RawJSON) == 0 {
		p.deliverError(req, errorCodeInvalidArgument, "missing data.raw_json", map[string]interface{}{"field": "data.raw_json"})
		return false
	}

	if err := json.Unmarshal(req.Data.RawJSON, payload); err != nil {
		p.deliverError(req, errorCodeInvalidArgument, "invalid request payload", nil)
		return false
	}

	return true
}

func (p *Plugin) deliverStoreError(req *framework.PluginRequest, operation string, err error) bool {
	if err == nil {
		return false
	}

	errorCode := mapStoreErrorCode(err)
	p.deliverError(req, errorCode, operationErrorMessage(errorCode, operation), nil)
	return true
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

func (p *Plugin) operationContext() context.Context {
	if p.ctx != nil {
		return p.ctx
	}
	return context.Background()
}

func normalizeChannel(channel string) string {
	return strings.TrimSpace(channel)
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func playerStateToPayload(channel, username string, state PlayerState) map[string]interface{} {
	return map[string]interface{}{
		"channel":  channel,
		"username": username,
		"bladder":  state.Bladder,
		"alcohol":  state.Alcohol,
		"weed":     state.Weed,
		"food":     state.Food,
		"lust":     state.Lust,
	}
}

func operationErrorMessage(errorCode, operation string) string {
	switch errorCode {
	case errorCodeInvalidArgument:
		return "invalid argument"
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

func mapStoreErrorCode(err error) string {
	if err == nil {
		return ""
	}

	var storeErr *StoreError
	if errors.As(err, &storeErr) && storeErr.Code != "" {
		return storeErr.Code
	}

	return errorCodeDBUnavailable
}
