package about

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
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
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	// Register our command with the command router
	p.registerCommand()

	log.Printf("[INFO] About plugin started: %s", p.name)
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

	log.Printf("[INFO] About plugin stopped: %s", p.name)
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
			To:   "commandrouter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "about,version,info",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		log.Printf("[ERROR] Failed to register about command: %v", err)
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
	log.Printf("[About] Handling about command from %s", req.Data.Command.Params["username"])

	// Build about message
	var lines []string
	lines = append(lines, fmt.Sprintf("%s v%s", p.config.Description, p.config.Version))
	lines = append(lines, "")
	lines = append(lines, "A modular Go chat bot with plugin-based architecture")
	lines = append(lines, fmt.Sprintf("Author: %s", p.config.Author))
	lines = append(lines, fmt.Sprintf("Repository: %s", p.config.Repository))
	lines = append(lines, "")
	lines = append(lines, "Features:")
	lines = append(lines, "• WebSocket connection to Cytube")
	lines = append(lines, "• PostgreSQL persistence")
	lines = append(lines, "• Event-driven plugin system")
	lines = append(lines, "• Modular command architecture")
	lines = append(lines, "")
	lines = append(lines, "Type !help for available commands")

	message := ""
	for _, line := range lines {
		message += line + "\n"
	}

	p.sendResponse(req, message)
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	// Create a simple text response to send back to chat
	channel := req.Data.Command.Params["channel"]
	log.Printf("[About] Sending response to channel %s: %d chars", channel, len(message))

	response := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}

	// Broadcast to cytube.send event
	if err := p.eventBus.Broadcast("cytube.send", response); err != nil {
		log.Printf("[ERROR] Failed to send about response: %v", err)
	} else {
		log.Printf("[About] Response sent successfully via cytube.send")
	}
}
