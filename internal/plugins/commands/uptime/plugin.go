package uptime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	running   bool
	startTime time.Time
}

func New() framework.Plugin {
	return &Plugin{
		name: "uptime",
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())
	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()

	// Subscribe to uptime command execution events
	if err := p.eventBus.Subscribe("command.uptime.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	// Register our command with the command router
	p.registerCommand()

	log.Printf("[INFO] Uptime plugin started: %s", p.name)
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

	log.Printf("[INFO] Uptime plugin stopped: %s", p.name)
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
	return "uptime"
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
					"commands": "uptime,up",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		log.Printf("[ERROR] Failed to register uptime command: %v", err)
	}
}

func (p *Plugin) handleCommand(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
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
		p.handleUptimeCommand(dataEvent.Data.PluginRequest)
	}()

	return nil
}

func (p *Plugin) handleUptimeCommand(req *framework.PluginRequest) {
	p.mu.RLock()
	uptime := time.Since(p.startTime)
	p.mu.RUnlock()

	// Format uptime message
	message := formatUptime(uptime)

	// Skip database query for now due to sync query limitations

	p.sendResponse(req, message)
}

func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d day", days))
		if days > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d hour", hours))
		if hours > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d minute", minutes))
		if minutes > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d second", seconds))
		if seconds != 1 {
			parts[len(parts)-1] += "s"
		}
	}

	result := "Bot uptime: "
	for i, part := range parts {
		if i > 0 {
			if i == len(parts)-1 {
				result += " and "
			} else {
				result += ", "
			}
		}
		result += part
	}

	return result
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	// Create a simple text response to send back to chat
	response := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: req.Data.Command.Params["channel"],
		},
	}

	// Broadcast to cytube.send event
	if err := p.eventBus.Broadcast("cytube.send", response); err != nil {
		log.Printf("[ERROR] Failed to send uptime response: %v", err)
	}
}
