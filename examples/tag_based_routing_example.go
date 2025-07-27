package main

import (
	"fmt"
	"log"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Example showing how to use tag-based routing and wildcard subscriptions
func main() {
	// Create event bus
	eb := eventbus.NewEventBus(&eventbus.Config{
		BufferSizes: map[string]int{
			"cytube.event": 1000,
		},
	})

	// Start the event bus
	if err := eb.Start(); err != nil {
		log.Fatal(err)
	}

	// Example 1: Subscribe to all chat events using tags
	chatHandler := func(event framework.Event) error {
		fmt.Printf("Chat event received: %s\n", event.Type())
		return nil
	}
	if err := eb.SubscribeWithTags("cytube.event.*", chatHandler, []string{"chat"}); err != nil {
		log.Fatalf("Failed to subscribe with chat tags: %v", err)
	}

	// Example 2: Subscribe to all user presence events
	presenceHandler := func(event framework.Event) error {
		fmt.Printf("User presence event: %s\n", event.Type())
		return nil
	}
	if err := eb.SubscribeWithTags("cytube.event.*", presenceHandler, []string{"user", "presence"}); err != nil {
		log.Fatalf("Failed to subscribe with presence tags: %v", err)
	}

	// Example 3: Subscribe to all media events
	mediaHandler := func(event framework.Event) error {
		fmt.Printf("Media event: %s\n", event.Type())
		return nil
	}
	if err := eb.SubscribeWithTags("cytube.event.*", mediaHandler, []string{"media"}); err != nil {
		log.Fatalf("Failed to subscribe with media tags: %v", err)
	}

	// Example 4: Subscribe to high-priority system events
	systemHandler := func(event framework.Event) error {
		fmt.Printf("System event: %s\n", event.Type())
		return nil
	}
	if err := eb.SubscribeWithTags("cytube.event.*", systemHandler, []string{"system"}); err != nil {
		log.Fatalf("Failed to subscribe with system tags: %v", err)
	}

	// Example 5: Subscribe to all cytube events (no tag filter)
	allEventsHandler := func(event framework.Event) error {
		fmt.Printf("Any cytube event: %s\n", event.Type())
		return nil
	}
	if err := eb.SubscribeWithTags("cytube.event.*", allEventsHandler, nil); err != nil {
		log.Fatalf("Failed to subscribe to all events: %v", err)
	}

	// Example 6: Traditional exact match subscription still works
	specificHandler := func(event framework.Event) error {
		fmt.Printf("Specific chatMsg event\n")
		return nil
	}
	if err := eb.Subscribe("cytube.event.chatMsg", specificHandler); err != nil {
		log.Fatalf("Failed to subscribe to specific event: %v", err)
	}

	// Simulate some events with metadata
	// Chat message event
	chatMetadata := framework.NewEventMetadata("cytube", "cytube.event.chatMsg").
		WithTags("chat", "public", "user-content").
		WithLogging("info")
	if err := eb.BroadcastWithMetadata("cytube.event.chatMsg", &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: "testuser",
			Message:  "Hello, world!",
			Channel:  "test-channel",
		},
	}, chatMetadata); err != nil {
		log.Printf("Failed to broadcast chat message: %v", err)
	}

	// User join event
	joinMetadata := framework.NewEventMetadata("cytube", "cytube.event.userJoin").
		WithTags("user", "presence", "join").
		WithLogging("info")
	if err := eb.BroadcastWithMetadata("cytube.event.userJoin", &framework.EventData{
		UserJoin: &framework.UserJoinData{
			Username: "newuser",
			UserRank: 1,
			Channel:  "test-channel",
		},
	}, joinMetadata); err != nil {
		log.Printf("Failed to broadcast user join: %v", err)
	}

	// Media change event
	mediaMetadata := framework.NewEventMetadata("cytube", "cytube.event.changeMedia").
		WithTags("media", "playlist", "change").
		WithLogging("info")
	if err := eb.BroadcastWithMetadata("cytube.event.changeMedia", &framework.EventData{
		VideoChange: &framework.VideoChangeData{
			VideoID:   "dQw4w9WgXcQ",
			VideoType: "yt",
			Title:     "Example Video",
			Duration:  212,
			Channel:   "test-channel",
		},
	}, mediaMetadata); err != nil {
		log.Printf("Failed to broadcast media change: %v", err)
	}

	// System event using KeyValue for generic data
	systemMetadata := framework.NewEventMetadata("cytube", "cytube.event.disconnect").
		WithTags("system", "connection", "disconnect").
		WithLogging("warn").
		WithPriority(2)
	if err := eb.BroadcastWithMetadata("cytube.event.disconnect", &framework.EventData{
		KeyValue: map[string]string{
			"reason": "server restart",
		},
	}, systemMetadata); err != nil {
		log.Printf("Failed to broadcast disconnect event: %v", err)
	}

	// Give handlers time to process
	// In a real application, you would have proper lifecycle management
	select {}
}
