package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestEventBusTagRouting(t *testing.T) {
	// Create event bus
	eb := NewEventBus(&Config{})

	// Start the event bus
	if err := eb.Start(); err != nil {
		t.Fatalf("Failed to start event bus: %v", err)
	}
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Failed to stop event bus: %v", err)
		}
	}()

	// Track received events using atomic counters
	var chatEvents int64
	var mediaEvents int64
	var userEvents int64

	// Subscribe with tags
	err := eb.SubscribeWithTags("test.*", func(event framework.Event) error {
		atomic.AddInt64(&chatEvents, 1)
		return nil
	}, []string{"chat"})
	if err != nil {
		t.Fatalf("Failed to subscribe to chat events: %v", err)
	}

	err = eb.SubscribeWithTags("test.*", func(event framework.Event) error {
		atomic.AddInt64(&mediaEvents, 1)
		return nil
	}, []string{"media"})
	if err != nil {
		t.Fatalf("Failed to subscribe to media events: %v", err)
	}

	err = eb.SubscribeWithTags("test.event", func(event framework.Event) error {
		atomic.AddInt64(&userEvents, 1)
		return nil
	}, []string{"user"})
	if err != nil {
		t.Fatalf("Failed to subscribe to user events: %v", err)
	}

	// Send events with different tags
	metadata1 := framework.NewEventMetadata("test", "test.event").WithTags("chat", "public")
	metadata2 := framework.NewEventMetadata("test", "test.event").WithTags("media", "change")
	metadata3 := framework.NewEventMetadata("test", "test.event").WithTags("user", "join")
	metadata4 := framework.NewEventMetadata("test", "test.event").WithTags("other")

	data := &framework.EventData{KeyValue: map[string]string{"test": "data"}}

	// Send events
	if err := eb.BroadcastWithMetadata("test.event", data, metadata1); err != nil {
		t.Errorf("Failed to broadcast event 1: %v", err)
	}
	if err := eb.BroadcastWithMetadata("test.event", data, metadata2); err != nil {
		t.Errorf("Failed to broadcast event 2: %v", err)
	}
	if err := eb.BroadcastWithMetadata("test.event", data, metadata3); err != nil {
		t.Errorf("Failed to broadcast event 3: %v", err)
	}
	if err := eb.BroadcastWithMetadata("test.event", data, metadata4); err != nil {
		t.Errorf("Failed to broadcast event 4: %v", err)
	} // Should not match any

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify counts using atomic loads
	if atomic.LoadInt64(&chatEvents) != 1 {
		t.Errorf("Expected 1 chat event, got %d", atomic.LoadInt64(&chatEvents))
	}
	if atomic.LoadInt64(&mediaEvents) != 1 {
		t.Errorf("Expected 1 media event, got %d", atomic.LoadInt64(&mediaEvents))
	}
	if atomic.LoadInt64(&userEvents) != 1 {
		t.Errorf("Expected 1 user event, got %d", atomic.LoadInt64(&userEvents))
	}
}

// Priority testing is now in priority_integration_test.go with better test coverage
