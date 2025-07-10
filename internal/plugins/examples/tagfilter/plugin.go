package tagfilter

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// Plugin demonstrates tag-based event filtering
type Plugin struct {
	name          string
	eventBus      framework.EventBus
	chatCount     int
	mediaCount    int
	userCount     int
	lastEventTime time.Time
}

// NewPlugin creates a new tag filter example plugin
func NewPlugin() framework.Plugin {
	return &Plugin{
		name: "tagfilter-example",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	// Example 1: Subscribe to all chat events using tags
	if err := bus.SubscribeWithTags("cytube.event.*", p.handleChatEvents, []string{"chat"}); err != nil {
		return fmt.Errorf("failed to subscribe to chat events: %w", err)
	}

	// Example 2: Subscribe to media change events using tags
	if err := bus.SubscribeWithTags("cytube.event.*", p.handleMediaEvents, []string{"media", "change"}); err != nil {
		return fmt.Errorf("failed to subscribe to media events: %w", err)
	}

	// Example 3: Subscribe to user presence events
	if err := bus.SubscribeWithTags("cytube.event.*", p.handleUserEvents, []string{"user", "presence"}); err != nil {
		return fmt.Errorf("failed to subscribe to user events: %w", err)
	}

	// Example 4: Subscribe to high-priority system events
	if err := bus.SubscribeWithTags("cytube.event.*", p.handleSystemEvents, []string{"system"}); err != nil {
		return fmt.Errorf("failed to subscribe to system events: %w", err)
	}

	// Example 5: Subscribe to all events from a specific room
	if err := bus.SubscribeWithTags("cytube.event.*", p.handleRoomEvents, []string{"room:testroom"}); err != nil {
		return fmt.Errorf("failed to subscribe to room events: %w", err)
	}

	log.Printf("[%s] Initialized with tag-based subscriptions", p.name)
	return nil
}

func (p *Plugin) Start() error {
	log.Printf("[%s] Started - demonstrating tag-based event filtering", p.name)

	// Start a periodic status reporter
	go p.statusReporter()

	return nil
}

func (p *Plugin) Stop() error {
	log.Printf("[%s] Stopped - processed %d chat, %d media, %d user events",
		p.name, p.chatCount, p.mediaCount, p.userCount)
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// This method won't be called for tag-based subscriptions
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:          p.name,
		State:         "running",
		EventsHandled: int64(p.chatCount + p.mediaCount + p.userCount),
	}
}

// Handler for chat events (public and private messages)
func (p *Plugin) handleChatEvents(event framework.Event) error {
	p.chatCount++
	p.lastEventTime = time.Now()

	// The event has already been filtered by tags at the EventBus level
	// This handler will only receive events tagged with "chat"

	// Check the event type to determine if it's a PM
	if event.Type() == "cytube.event.pm" {
		log.Printf("[%s] Private message event", p.name)
	} else if event.Type() == "cytube.event.chatMsg" {
		log.Printf("[%s] Public chat event", p.name)
	}

	return nil
}

// Handler for media events
func (p *Plugin) handleMediaEvents(event framework.Event) error {
	p.mediaCount++
	p.lastEventTime = time.Now()

	log.Printf("[%s] Media change event detected", p.name)
	return nil
}

// Handler for user presence events
func (p *Plugin) handleUserEvents(event framework.Event) error {
	p.userCount++
	p.lastEventTime = time.Now()

	// Check event type to determine if it's a join or leave
	if event.Type() == "cytube.event.userJoin" {
		log.Printf("[%s] User joined", p.name)
	} else if event.Type() == "cytube.event.userLeave" {
		log.Printf("[%s] User left", p.name)
	}

	return nil
}

// Handler for system events
func (p *Plugin) handleSystemEvents(event framework.Event) error {
	// System events are already filtered by the "system" tag
	log.Printf("[%s] System event: %s", p.name, event.Type())
	return nil
}

// Handler for room-specific events
func (p *Plugin) handleRoomEvents(event framework.Event) error {
	// This handler will only receive events tagged with "room:testroom"
	// The room filtering has already been done by the EventBus
	log.Printf("[%s] Event from testroom: %s", p.name, event.Type())
	return nil
}

// statusReporter periodically logs statistics
func (p *Plugin) statusReporter() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if p.chatCount > 0 || p.mediaCount > 0 || p.userCount > 0 {
			log.Printf("[%s] Stats - Chat: %d, Media: %d, User: %d events processed",
				p.name, p.chatCount, p.mediaCount, p.userCount)
		}
	}
}

// Example of how to use priority in your own events
func (p *Plugin) sendHighPriorityAlert(message string) {
	metadata := framework.NewEventMetadata(p.name, "alert.high").
		WithPriority(3).
		WithTags("alert", "system", "high-priority").
		WithLogging("error")

	data := &framework.EventData{
		KeyValue: map[string]string{
			"message": message,
			"time":    time.Now().Format(time.RFC3339),
		},
	}

	if err := p.eventBus.BroadcastWithMetadata("alert.high", data, metadata); err != nil {
		log.Printf("[%s] Failed to send high priority alert: %v", p.name, err)
	}
}
