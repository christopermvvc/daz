package filter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the filter plugin for event routing and command parsing
type Plugin struct {
	name     string
	eventBus framework.EventBus
	config   *Config
	running  bool
	mu       sync.RWMutex

	// Shutdown management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds filter plugin configuration
type Config struct {
	CommandPrefix string
	RoutingRules  []RoutingRule
}

// RoutingRule defines how to route events
type RoutingRule struct {
	EventType    string
	TargetPlugin string
	Priority     int
	Enabled      bool
}

// NewPlugin creates a new filter plugin instance
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{
			CommandPrefix: "!",
			RoutingRules:  getDefaultRoutingRules(),
		}
	}

	return &Plugin{
		name:    "filter",
		config:  config,
		running: false,
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Initialize sets up the plugin with the event bus
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	p.eventBus = eventBus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	log.Printf("[Filter] Initialized with command prefix: %s", p.config.CommandPrefix)
	return nil
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.RLock()
	if p.running {
		p.mu.RUnlock()
		return fmt.Errorf("filter plugin already running")
	}
	p.mu.RUnlock()

	// Subscribe to all Cytube events
	err := p.eventBus.Subscribe("cytube.event", p.handleCytubeEvent)
	if err != nil {
		return fmt.Errorf("failed to subscribe to cytube events: %w", err)
	}

	// Subscribe to chat messages specifically for command parsing
	err = p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to chat events: %w", err)
	}

	p.mu.Lock()
	p.running = true
	p.mu.Unlock()
	log.Println("[Filter] Started event routing")
	return nil
}

// Stop gracefully shuts down the plugin
func (p *Plugin) Stop() error {
	p.mu.RLock()
	if !p.running {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	log.Println("[Filter] Stopping filter plugin")
	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	log.Println("[Filter] Filter plugin stopped")
	return nil
}

// HandleEvent processes incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	// This is called by the event bus when events arrive
	// The actual handling is done in the specific handler methods
	return nil
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state := "stopped"
	if p.running {
		state = "running"
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: state,
	}
}

// handleCytubeEvent routes Cytube events to appropriate plugins
func (p *Plugin) handleCytubeEvent(event framework.Event) error {
	cytubeEvent, ok := event.(*framework.CytubeEvent)
	if !ok {
		return fmt.Errorf("invalid event type for cytube event")
	}

	// Apply routing rules
	for _, rule := range p.config.RoutingRules {
		if !rule.Enabled {
			continue
		}

		if matchesEventType(cytubeEvent.EventType, rule.EventType) {
			p.routeToPlugin(rule.TargetPlugin, cytubeEvent)
		}
	}

	return nil
}

// handleChatMessage specifically handles chat messages for command detection
func (p *Plugin) handleChatMessage(event framework.Event) error {
	// For now, we'll need to parse the raw data from the event
	// In a real implementation, the core plugin would send us EventData
	// with the ChatMessage field populated

	cytubeEvent, ok := event.(*framework.CytubeEvent)
	if !ok {
		return fmt.Errorf("invalid event type for chat message")
	}

	// Parse the raw data to get chat message details
	var chatData struct {
		Username string `json:"username"`
		Msg      string `json:"msg"`
	}

	if err := json.Unmarshal(cytubeEvent.RawData, &chatData); err != nil {
		log.Printf("[Filter] Failed to parse chat message data: %v", err)
		return nil
	}

	// Check if message is a command
	if strings.HasPrefix(chatData.Msg, p.config.CommandPrefix) {
		p.handleCommand(cytubeEvent, framework.ChatMessageData{
			Username: chatData.Username,
			Message:  chatData.Msg,
		})
	}

	return nil
}

// handleCommand processes detected commands
func (p *Plugin) handleCommand(event *framework.CytubeEvent, chatData framework.ChatMessageData) {
	// Remove command prefix and parse
	cmdText := strings.TrimPrefix(chatData.Message, p.config.CommandPrefix)
	parts := strings.Fields(cmdText)

	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	args := parts[1:]

	log.Printf("[Filter] Detected command: %s with args: %v", cmdName, args)

	// Create command event data
	cmdData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   fmt.Sprintf("cmd_%d", time.Now().UnixNano()),
			From: p.name,
			To:   "commands", // Target the commands plugin
			Type: "command.execute",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: cmdName,
					Args: args,
					Params: map[string]string{
						"username": chatData.Username,
						"channel":  event.ChannelName,
						"raw_msg":  chatData.Message,
					},
				},
			},
		},
	}

	// Send to command handler plugin
	err := p.eventBus.Send("commands", eventbus.EventPluginCommand, cmdData)
	if err != nil {
		log.Printf("[Filter] Failed to route command: %v", err)
	}
}

// routeToPlugin forwards an event to a specific plugin
func (p *Plugin) routeToPlugin(targetPlugin string, event *framework.CytubeEvent) {
	// Create forwarded event data with raw message
	eventData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: string(event.RawData),
			Channel: event.ChannelName,
		},
		KeyValue: map[string]string{
			"event_type": event.EventType,
			"timestamp":  event.EventTime.Format(time.RFC3339),
		},
	}

	// Determine appropriate event type for target
	eventType := fmt.Sprintf("plugin.%s.event", targetPlugin)

	err := p.eventBus.Send(targetPlugin, eventType, eventData)
	if err != nil {
		log.Printf("[Filter] Failed to route event to %s: %v", targetPlugin, err)
	}
}

// matchesEventType checks if an event type matches a pattern
func matchesEventType(eventType, pattern string) bool {
	// Simple pattern matching with wildcard support
	if pattern == "*" {
		return true
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(eventType, prefix)
	}

	return eventType == pattern
}

// getDefaultRoutingRules returns the default routing configuration
func getDefaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		// Route user events to user tracking plugin
		{
			EventType:    "cytube.event.userJoin",
			TargetPlugin: "usertracker",
			Priority:     10,
			Enabled:      true,
		},
		{
			EventType:    "cytube.event.userLeave",
			TargetPlugin: "usertracker",
			Priority:     10,
			Enabled:      true,
		},
		// Route media events to media tracking plugin
		{
			EventType:    "cytube.event.changeMedia",
			TargetPlugin: "mediatracker",
			Priority:     10,
			Enabled:      true,
		},
		// Route all events to analytics plugin
		{
			EventType:    "cytube.event.*",
			TargetPlugin: "analytics",
			Priority:     5,
			Enabled:      true,
		},
	}
}
