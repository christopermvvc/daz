package priority

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// Plugin demonstrates priority-based event messaging
type Plugin struct {
	name     string
	eventBus framework.EventBus
}

// NewPlugin creates a new priority example plugin
func NewPlugin() framework.Plugin {
	return &Plugin{
		name: "priority-example",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	// Subscribe to our own test events
	if err := bus.Subscribe("priority.test", p.handleTestEvent); err != nil {
		return fmt.Errorf("failed to subscribe to test events: %w", err)
	}

	log.Printf("[%s] Initialized - demonstrating priority-based messaging", p.name)
	return nil
}

func (p *Plugin) Start() error {
	log.Printf("[%s] Started", p.name)

	// Demonstrate priority by sending events with different priorities
	go p.demonstratePriority()

	return nil
}

func (p *Plugin) Stop() error {
	log.Printf("[%s] Stopped", p.name)
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:  p.name,
		State: "running",
	}
}

// demonstratePriority sends events with different priorities to show ordering
func (p *Plugin) demonstratePriority() {
	// Wait a bit for everything to initialize
	time.Sleep(2 * time.Second)

	log.Printf("[%s] Sending events with different priorities...", p.name)

	// Send 10 events with varying priorities in reverse order
	// High priority events should be processed first
	for i := 0; i < 10; i++ {
		priority := i % 4 // Priorities 0-3

		metadata := framework.NewEventMetadata(p.name, "priority.test").
			WithPriority(priority).
			WithTags("test", "priority-demo")

		data := &framework.EventData{
			KeyValue: map[string]string{
				"message":  fmt.Sprintf("Event %d with priority %d", i, priority),
				"sequence": fmt.Sprintf("%d", i),
			},
		}

		// Send in order, but they should be processed by priority
		if err := p.eventBus.BroadcastWithMetadata("priority.test", data, metadata); err != nil {
			log.Printf("[%s] Failed to send test event: %v", p.name, err)
		}
	}

	// Send a batch of high-priority alerts
	time.Sleep(100 * time.Millisecond)
	log.Printf("[%s] Sending high-priority alerts...", p.name)

	for i := 0; i < 3; i++ {
		p.sendAlert(fmt.Sprintf("ALERT %d: System critical!", i), 3)
		time.Sleep(10 * time.Millisecond)
	}

	// Send some normal priority events
	for i := 0; i < 3; i++ {
		p.sendInfo(fmt.Sprintf("INFO %d: Normal operation", i), 0)
		time.Sleep(10 * time.Millisecond)
	}

	// Demonstrate system events with high priority
	p.sendSystemEvent("System shutdown initiated", 3)
	p.sendSystemEvent("User authentication required", 2)
	p.sendSystemEvent("Routine maintenance", 1)
	p.sendSystemEvent("Status update", 0)
}

// handleTestEvent logs the received events to show processing order
func (p *Plugin) handleTestEvent(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data != nil && dataEvent.Data.KeyValue != nil {
		message := dataEvent.Data.KeyValue["message"]
		sequence := dataEvent.Data.KeyValue["sequence"]
		log.Printf("[%s] Processed: %s (seq: %s)", p.name, message, sequence)
	}

	return nil
}

// sendAlert sends a high-priority alert
func (p *Plugin) sendAlert(message string, priority int) {
	metadata := framework.NewEventMetadata(p.name, "alert").
		WithPriority(priority).
		WithTags("alert", "high-priority").
		WithLogging("error")

	data := &framework.EventData{
		KeyValue: map[string]string{
			"message": message,
			"time":    time.Now().Format(time.RFC3339),
			"level":   "alert",
		},
	}

	if err := p.eventBus.BroadcastWithMetadata("priority.test", data, metadata); err != nil {
		log.Printf("[%s] Failed to send alert: %v", p.name, err)
	}
}

// sendInfo sends a normal priority info message
func (p *Plugin) sendInfo(message string, priority int) {
	metadata := framework.NewEventMetadata(p.name, "info").
		WithPriority(priority).
		WithTags("info", "normal-priority").
		WithLogging("info")

	data := &framework.EventData{
		KeyValue: map[string]string{
			"message": message,
			"time":    time.Now().Format(time.RFC3339),
			"level":   "info",
		},
	}

	if err := p.eventBus.BroadcastWithMetadata("priority.test", data, metadata); err != nil {
		log.Printf("[%s] Failed to send info: %v", p.name, err)
	}
}

// sendSystemEvent sends a system event with specified priority
func (p *Plugin) sendSystemEvent(message string, priority int) {
	metadata := framework.NewEventMetadata(p.name, "system").
		WithPriority(priority).
		WithTags("system", "priority-demo").
		WithLogging("info")

	data := &framework.EventData{
		KeyValue: map[string]string{
			"message":  message,
			"time":     time.Now().Format(time.RFC3339),
			"priority": fmt.Sprintf("%d", priority),
		},
	}

	if err := p.eventBus.BroadcastWithMetadata("priority.test", data, metadata); err != nil {
		log.Printf("[%s] Failed to send system event: %v", p.name, err)
	}

	log.Printf("[%s] Sent system event with priority %d: %s", p.name, priority, message)
}
