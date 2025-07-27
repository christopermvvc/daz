package chatfilter

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/hildolfr/daz/internal/framework"
)

// Plugin demonstrates tag-based event subscription
// It only receives public chat messages using tags
type Plugin struct {
	name            string
	eventBus        framework.EventBus
	publicMsgCount  int
	privateMsgCount int
}

// NewPlugin creates a new chat filter plugin
func NewPlugin() *Plugin {
	return &Plugin{
		name: "chatfilter",
	}
}

// Init initializes the plugin
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	log.Printf("[ChatFilter] Initialized - will filter for public chat messages only")
	return nil
}

// Start starts the plugin
func (p *Plugin) Start() error {
	// Subscribe to all chat events but filter for public messages using tags
	err := p.eventBus.SubscribeWithTags("cytube.event.chatMsg", p.handleChatMessage, []string{"public"})
	if err != nil {
		return fmt.Errorf("failed to subscribe to public chat: %w", err)
	}

	// Also subscribe to private messages separately
	err = p.eventBus.SubscribeWithTags("cytube.event.pm", p.handlePrivateMessage, []string{"private"})
	if err != nil {
		return fmt.Errorf("failed to subscribe to private messages: %w", err)
	}

	log.Printf("[ChatFilter] Started - subscribed to public and private messages separately")
	return nil
}

// Stop stops the plugin
func (p *Plugin) Stop() error {
	log.Printf("[ChatFilter] Stopped - processed %d public messages and %d private messages",
		p.publicMsgCount, p.privateMsgCount)
	return nil
}

// HandleEvent handles incoming events (not used when using specific subscriptions)
func (p *Plugin) HandleEvent(event framework.Event) error {
	return nil
}

// Status returns the plugin status
func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:  p.name,
		State: "running",
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// handleChatMessage handles public chat messages
func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	p.publicMsgCount++
	if p.publicMsgCount%10 == 0 {
		log.Printf("[ChatFilter] Processed %d public messages", p.publicMsgCount)
	}

	return nil
}

// handlePrivateMessage handles private messages
func (p *Plugin) handlePrivateMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PrivateMessage == nil {
		return nil
	}

	p.privateMsgCount++
	log.Printf("[ChatFilter] Received PM from %s", dataEvent.Data.PrivateMessage.FromUser)

	return nil
}
