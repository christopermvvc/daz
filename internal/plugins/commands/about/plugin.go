package about

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Repository  string `json:"repository"`
}

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	config    *Config
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	running   bool
	startTime time.Time
}

func New() framework.Plugin {
	return &Plugin{
		name: "about",
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	} else {
		p.config = &Config{
			Version:     "0.1.0",
			Description: "Daz - A modular chat bot for Cytube",
			Author:      "hildolfr",
			Repository:  "https://github.com/hildolfr/daz",
		}
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.startTime = time.Now()

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.mu.Unlock()

	// Subscribe to about command execution events
	if err := p.eventBus.Subscribe("command.about.execute", p.handleCommand); err != nil {
		// Emit failure event for retry
		p.emitFailureEvent("command.about.failed", "subscription", "event_subscription", err)
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	// Register our command with the command router
	p.registerCommand()

	logger.Debug(p.name, "Started")
	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()

	logger.Debug(p.name, "Stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// This is called by the event bus when events arrive
	// The actual handling is done in the handleCommand method
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "stopped"
	if p.running {
		status = "running"
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: status,
	}
}

func (p *Plugin) Name() string {
	return "about"
}

func (p *Plugin) registerCommand() {
	// Send registration event to command router
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands":    "about,version,info",
					"min_rank":    "0",
					"admin_only":  "true",
					"description": "show bot status details",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register about command: %v", err)
		// Emit failure event for retry
		p.emitFailureEvent("command.about.failed", "registration", "command_registration", err)
	}
}

func (p *Plugin) handleCommand(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		// Try old format for backward compatibility
		cytubeEvent, ok := event.(*framework.CytubeEvent)
		if !ok {
			return fmt.Errorf("invalid event type for command")
		}

		// Parse the event data
		var data framework.EventData
		if err := json.Unmarshal(cytubeEvent.RawData, &data); err != nil {
			return fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		if data.PluginRequest == nil || data.PluginRequest.Data == nil ||
			data.PluginRequest.Data.Command == nil {
			return nil
		}

		// Handle asynchronously
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.handleAboutCommand(data.PluginRequest)
		}()

		return nil
	}

	// Handle DataEvent
	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	// Handle asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.handleAboutCommand(dataEvent.Data.PluginRequest)
	}()

	return nil
}

func (p *Plugin) handleAboutCommand(req *framework.PluginRequest) {
	logger.Debug(p.name, "Handling about command from %s", req.Data.Command.Params["username"])

	// Check if user is admin
	isAdmin := req.Data.Command.Params["is_admin"] == "true"

	var message string
	if !isAdmin {
		message = "This command is admin-only."
	} else {
		// Get memory statistics
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		// Get system state
		uptime := time.Since(p.startTime)

		// Build about message with memory usage and system state
		var lines []string
		lines = append(lines, "=== System State ===")
		lines = append(lines, fmt.Sprintf("Uptime: %s", formatDuration(uptime)))
		lines = append(lines, fmt.Sprintf("Goroutines: %d", runtime.NumGoroutine()))
		lines = append(lines, "")
		lines = append(lines, "=== Memory Usage ===")
		lines = append(lines, fmt.Sprintf("Allocated: %.2f MB", float64(m.Alloc)/1024/1024))
		lines = append(lines, fmt.Sprintf("Total Allocated: %.2f MB", float64(m.TotalAlloc)/1024/1024))
		lines = append(lines, fmt.Sprintf("System Memory: %.2f MB", float64(m.Sys)/1024/1024))
		lines = append(lines, fmt.Sprintf("GC Cycles: %d", m.NumGC))
		lines = append(lines, "")
		lines = append(lines, "=== Runtime Info ===")
		lines = append(lines, fmt.Sprintf("Go Version: %s", runtime.Version()))
		lines = append(lines, fmt.Sprintf("GOOS: %s", runtime.GOOS))
		lines = append(lines, fmt.Sprintf("GOARCH: %s", runtime.GOARCH))
		lines = append(lines, fmt.Sprintf("CPU Cores: %d", runtime.NumCPU()))

		message = ""
		for _, line := range lines {
			message += line + "\n"
		}
	}

	p.sendResponse(req, message)
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += " "
		}
		result += part
	}
	return result
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	// Send response via plugin response system
	username := req.Data.Command.Params["username"]
	channel := req.Data.Command.Params["channel"]

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    p.name,
			Success: true,
			Data: &framework.ResponseData{
				CommandResult: &framework.CommandResultData{
					Success: true,
					Output:  message,
				},
				KeyValue: map[string]string{
					"username": username,
					"channel":  channel,
				},
			},
		},
	}

	// Broadcast to plugin.response event for routing
	if err := p.eventBus.Broadcast("plugin.response", response); err != nil {
		logger.Error(p.name, "Failed to send about plugin response: %v", err)
		// Emit failure event for retry
		p.emitFailureEvent("command.about.failed", req.ID, "response_delivery", err)
	}
}

// emitFailureEvent emits a failure event for the retry mechanism
func (p *Plugin) emitFailureEvent(eventType, correlationID, operationType string, err error) {
	failureData := &framework.EventData{
		KeyValue: map[string]string{
			"correlation_id": correlationID,
			"source":         p.name,
			"operation_type": operationType,
			"error":          err.Error(),
			"timestamp":      time.Now().Format(time.RFC3339),
		},
	}

	// Emit failure event asynchronously
	go func() {
		if err := p.eventBus.Broadcast(eventType, failureData); err != nil {
			logger.Debug(p.name, "Failed to emit failure event: %v", err)
		}
	}()
}
