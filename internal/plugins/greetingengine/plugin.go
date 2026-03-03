package greetingengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName        = "greetingengine"
	stateInitialized  = "initialized"
	stateRunning      = "running"
	stateStopped      = "stopped"
	operationGenerate = "generate"
)

const (
	defaultGreetingTokens      = 96
	defaultGreetingTemperature = 0.7
)

type Config struct {
	Enabled     bool    `json:"enabled"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

type Plugin struct {
	ctx          context.Context
	cancel       context.CancelFunc
	eventBus     framework.EventBus
	ollamaClient *framework.OllamaClient
	name         string
	running      bool
	mu           sync.RWMutex
	readyChan    chan struct{}
	status       framework.PluginStatus
	config       *Config
}

type generateRequest struct {
	Channel      string            `json:"channel"`
	Username     string            `json:"username"`
	Rank         int               `json:"rank"`
	Message      string            `json:"message"`
	SystemPrompt string            `json:"system_prompt"`
	Model        string            `json:"model"`
	Temperature  float64           `json:"temperature"`
	MaxTokens    int               `json:"max_tokens"`
	ExtraContext map[string]string `json:"extra_context"`
}

func New() framework.Plugin {
	return &Plugin{
		name: pluginName,
		config: &Config{
			Enabled:     true,
			MaxTokens:   defaultGreetingTokens,
			Temperature: defaultGreetingTemperature,
		},
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  pluginName,
			State: stateInitialized,
		},
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Dependencies() []string {
	return []string{"ollama"}
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

	if len(config) > 0 {
		if err := json.Unmarshal(config, p.config); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
	}

	if p.config.MaxTokens <= 0 {
		p.config.MaxTokens = defaultGreetingTokens
	}
	if p.config.Temperature == 0 {
		p.config.Temperature = defaultGreetingTemperature
	}

	p.eventBus = bus
	p.ollamaClient = framework.NewOllamaClient(bus, p.name)
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

	if !p.config.Enabled {
		p.running = true
		p.status.State = stateStopped
		close(p.readyChan)
		return nil
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

	if req.To != p.name || req.ID == "" {
		return nil
	}

	switch req.Type {
	case operationGenerate:
		p.handleGenerateRequest(req)
	default:
		p.deliverError(req, "UNSUPPORTED_OPERATION", fmt.Sprintf("unknown operation: %s", req.Type), nil)
	}

	return nil
}

func (p *Plugin) handleGenerateRequest(req *framework.PluginRequest) {
	payload := &generateRequest{
		MaxTokens:   p.config.MaxTokens,
		Temperature: p.config.Temperature,
	}

	if !p.parseGenerateRequest(req, payload) {
		p.deliverError(req, "INVALID_REQUEST", "invalid generate request", map[string]interface{}{"reason": "missing or malformed payload"})
		return
	}

	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = "Create a short, casual one-line welcome greeting for this user in a laid-back Australian Dazza tone."
	}

	if payload.Username != "" {
		message = strings.ReplaceAll(message, "<user>", payload.Username)
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	ollamaReq := framework.OllamaGenerateRequest{
		Channel:      payload.Channel,
		Username:     payload.Username,
		Message:      message,
		SystemPrompt: payload.SystemPrompt,
		Model:        payload.Model,
		Temperature:  payload.Temperature,
		MaxTokens:    payload.MaxTokens,
		ExtraContext: payload.ExtraContext,
	}

	if ollamaReq.ExtraContext == nil {
		ollamaReq.ExtraContext = map[string]string{}
	}
	ollamaReq.ExtraContext["rank"] = fmt.Sprintf("%d", payload.Rank)
	ollamaReq.ExtraContext["source"] = "greetingengine"
	if payload.Channel != "" {
		ollamaReq.ExtraContext["channel"] = payload.Channel
	}
	if payload.Username != "" {
		ollamaReq.ExtraContext["username"] = payload.Username
	}

	resp, err := p.ollamaClient.Generate(ctx, ollamaReq)
	if err != nil {
		logger.Warn(p.name, "Failed to generate greeting via ollama: %v", err)
		p.deliverError(req, "GENERATION_FAILED", err.Error(), nil)
		return
	}

	greeting := strings.TrimSpace(resp.Text)
	if greeting == "" {
		p.deliverError(req, "GENERATION_FAILED", "empty greeting response", nil)
		return
	}

	p.deliverSuccess(req, greeting)
}

func (p *Plugin) parseGenerateRequest(req *framework.PluginRequest, payload *generateRequest) bool {
	if req.Data == nil {
		return false
	}

	// Parse JSON payload if present.
	if len(req.Data.RawJSON) > 0 {
		if err := json.Unmarshal(req.Data.RawJSON, payload); err != nil {
			return false
		}
	}

	// Backward-compatible support for key/value payloads.
	if len(req.Data.KeyValue) > 0 {
		if payload.Channel == "" {
			payload.Channel = req.Data.KeyValue["channel"]
		}
		if payload.Username == "" {
			payload.Username = req.Data.KeyValue["username"]
		}
		if payload.Rank == 0 && req.Data.KeyValue["rank"] != "" {
			if rank, err := strconv.Atoi(req.Data.KeyValue["rank"]); err == nil {
				payload.Rank = rank
			}
		}
	}

	if payload.Temperature == 0 {
		payload.Temperature = p.config.Temperature
	}
	if payload.MaxTokens <= 0 {
		payload.MaxTokens = p.config.MaxTokens
	}

	return true
}

func (p *Plugin) deliverSuccess(req *framework.PluginRequest, greeting string) {
	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    p.name,
			Success: true,
			Data: &framework.ResponseData{
				KeyValue: map[string]string{
					"greeting": greeting,
				},
			},
		},
	}
	p.eventBus.DeliverResponse(req.ID, response, nil)
}

func (p *Plugin) deliverError(req *framework.PluginRequest, errorCode, message string, details map[string]interface{}) {
	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    p.name,
			Success: false,
			Error:   message,
		},
	}

	if details != nil {
		if raw, err := json.Marshal(details); err == nil {
			response.PluginResponse.Data = &framework.ResponseData{
				RawJSON: raw,
			}
		}
	}

	p.eventBus.DeliverResponse(req.ID, response, nil)
}
