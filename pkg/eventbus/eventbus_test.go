package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewEventBus(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   int // expected buffer size for cytube.event
	}{
		{
			name:   "default config",
			config: nil,
			want:   1000,
		},
		{
			name: "custom buffer sizes",
			config: &Config{
				BufferSizes: map[string]int{
					"cytube.event": 500,
				},
			},
			want: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eb := NewEventBus(tt.config)
			if eb == nil {
				t.Fatal("NewEventBus returned nil")
			}

			// Check buffer size
			if got := eb.bufferSizes["cytube.event"]; got != tt.want {
				t.Errorf("buffer size = %d, want %d", got, tt.want)
			}

			// Cleanup
			if err := eb.Stop(); err != nil {
				t.Errorf("Stop failed: %v", err)
			}
		})
	}
}

func TestEventBus_RegisterPlugin(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Mock plugin
	var plugin framework.Plugin // nil is fine for registration test

	if err := eb.RegisterPlugin("test-plugin", plugin); err != nil {
		t.Fatalf("RegisterPlugin failed: %v", err)
	}

	// Verify plugin was registered
	eb.plugMu.RLock()
	defer eb.plugMu.RUnlock()
	if _, exists := eb.plugins["test-plugin"]; !exists {
		t.Error("plugin was not registered")
	}
}

func TestEventBus_Broadcast(t *testing.T) {
	eb := NewEventBus(&Config{
		BufferSizes: map[string]int{
			"test.event": 10,
		},
	})
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Test successful broadcast
	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}

	err := eb.Broadcast("test.event", data)
	if err != nil {
		t.Errorf("Broadcast failed: %v", err)
	}

	// Test buffer overflow (non-blocking)
	for i := 0; i < 20; i++ {
		err := eb.Broadcast("test.event", data)
		if err != nil {
			t.Errorf("Broadcast should not return error on overflow: %v", err)
		}
	}
}

func TestEventBus_Subscribe(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	received := make(chan bool, 1)
	handler := func(event framework.Event) error {
		received <- true
		return nil
	}

	// Subscribe to event
	err := eb.Subscribe("test.event", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Broadcast event
	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}
	err = eb.Broadcast("test.event", data)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Wait for handler to receive event
	select {
	case <-received:
		// Success
	case <-time.After(time.Second):
		t.Error("handler did not receive event")
	}
}

func TestEventBus_Send(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Test direct send
	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}

	err := eb.Send("target-plugin", "test.event", data)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestEventBus_ConcurrentOperations(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent broadcasts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			data := &framework.EventData{
				KeyValue: map[string]string{"id": string(rune(id))},
			}
			if err := eb.Broadcast("concurrent.test", data); err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent subscribes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			handler := func(event framework.Event) error {
				return nil
			}
			if err := eb.Subscribe("concurrent.test", handler); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent operation failed: %v", err)
	}
}

func TestEventBus_StopGracefully(t *testing.T) {
	eb := NewEventBus(nil)

	// Subscribe to create active routers
	handler := func(event framework.Event) error {
		return nil
	}
	if err := eb.Subscribe("test.event", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Stop should complete without hanging
	done := make(chan bool)
	go func() {
		err := eb.Stop()
		if err != nil {
			t.Errorf("Stop failed: %v", err)
		}
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Stop did not complete in time")
	}
}

func TestEventBus_GetBufferSize(t *testing.T) {
	eb := NewEventBus(&Config{
		BufferSizes: map[string]int{
			"cytube.event": 500,
			"sql.":         200,
		},
	})
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	tests := []struct {
		eventType string
		want      int
	}{
		{"cytube.event", 500},
		{"cytube.event.chatMsg", 500}, // Should match prefix
		{"sql.request", 200},          // Should match prefix
		{"unknown.type", 100},         // Default
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			if got := eb.getBufferSize(tt.eventType); got != tt.want {
				t.Errorf("getBufferSize(%q) = %d, want %d", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	count := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Register multiple subscribers
	wg.Add(3) // Expect 3 handlers to be called
	for i := 0; i < 3; i++ {
		handler := func(event framework.Event) error {
			defer wg.Done()
			mu.Lock()
			count++
			mu.Unlock()
			return nil
		}
		err := eb.Subscribe("multi.test", handler)
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
	}

	// Broadcast event
	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}
	err := eb.Broadcast("multi.test", data)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	// Wait for all handlers to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All handlers completed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for handlers to complete")
	}

	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Errorf("expected 3 handlers to be called, got %d", count)
	}
}

func BenchmarkEventBus_Broadcast(b *testing.B) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			b.Errorf("Stop failed: %v", err)
		}
	}()

	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eb.Broadcast("bench.event", data)
	}
}

func BenchmarkEventBus_ConcurrentBroadcast(b *testing.B) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			b.Errorf("Stop failed: %v", err)
		}
	}()

	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = eb.Broadcast("bench.event", data)
		}
	})
}

func TestEventBus_SubscribeWithTags(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Track which handlers received events
	handler1Received := make(chan bool, 1)
	handler2Received := make(chan bool, 1)
	handler3Received := make(chan bool, 1)

	// Handler 1: Subscribes with tags ["important", "urgent"]
	handler1 := func(event framework.Event) error {
		handler1Received <- true
		return nil
	}
	err := eb.SubscribeWithTags("tagged.event", handler1, []string{"important", "urgent"})
	if err != nil {
		t.Fatalf("SubscribeWithTags failed for handler1: %v", err)
	}

	// Handler 2: Subscribes with tag ["important"]
	handler2 := func(event framework.Event) error {
		handler2Received <- true
		return nil
	}
	err = eb.SubscribeWithTags("tagged.event", handler2, []string{"important"})
	if err != nil {
		t.Fatalf("SubscribeWithTags failed for handler2: %v", err)
	}

	// Handler 3: Subscribes with no tags (receives all events)
	handler3 := func(event framework.Event) error {
		handler3Received <- true
		return nil
	}
	err = eb.SubscribeWithTags("tagged.event", handler3, nil)
	if err != nil {
		t.Fatalf("SubscribeWithTags failed for handler3: %v", err)
	}

	t.Run("event with both tags", func(t *testing.T) {
		// Send event with tags ["important", "urgent"]
		data := &framework.EventData{
			KeyValue: map[string]string{"test": "value1"},
		}
		metadata := framework.NewEventMetadata("test", "tagged.event").
			WithTags("important", "urgent")

		err := eb.BroadcastWithMetadata("tagged.event", data, metadata)
		if err != nil {
			t.Fatalf("BroadcastWithMetadata failed: %v", err)
		}

		// All handlers should receive this event
		checkHandlerReceived(t, "handler1", handler1Received, true)
		checkHandlerReceived(t, "handler2", handler2Received, true)
		checkHandlerReceived(t, "handler3", handler3Received, true)
	})

	t.Run("event with one tag", func(t *testing.T) {
		// Clear channels
		clearChannel(handler1Received)
		clearChannel(handler2Received)
		clearChannel(handler3Received)

		// Send event with tag ["important"]
		data := &framework.EventData{
			KeyValue: map[string]string{"test": "value2"},
		}
		metadata := framework.NewEventMetadata("test", "tagged.event").
			WithTags("important")

		err := eb.BroadcastWithMetadata("tagged.event", data, metadata)
		if err != nil {
			t.Fatalf("BroadcastWithMetadata failed: %v", err)
		}

		// Only handler2 and handler3 should receive this event
		checkHandlerReceived(t, "handler1", handler1Received, false)
		checkHandlerReceived(t, "handler2", handler2Received, true)
		checkHandlerReceived(t, "handler3", handler3Received, true)
	})

	t.Run("event with no tags", func(t *testing.T) {
		// Clear channels
		clearChannel(handler1Received)
		clearChannel(handler2Received)
		clearChannel(handler3Received)

		// Send event with no tags
		data := &framework.EventData{
			KeyValue: map[string]string{"test": "value3"},
		}
		metadata := framework.NewEventMetadata("test", "tagged.event")

		err := eb.BroadcastWithMetadata("tagged.event", data, metadata)
		if err != nil {
			t.Fatalf("BroadcastWithMetadata failed: %v", err)
		}

		// Only handler3 should receive this event
		checkHandlerReceived(t, "handler1", handler1Received, false)
		checkHandlerReceived(t, "handler2", handler2Received, false)
		checkHandlerReceived(t, "handler3", handler3Received, true)
	})
}

func TestEventBus_SubscribeWithTagsPattern(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Track which handlers received events
	handler1Received := make(chan bool, 1)
	handler2Received := make(chan bool, 1)

	// Handler 1: Pattern subscriber with tags
	handler1 := func(event framework.Event) error {
		handler1Received <- true
		return nil
	}
	err := eb.SubscribeWithTags("system.*", handler1, []string{"critical"})
	if err != nil {
		t.Fatalf("SubscribeWithTags failed for pattern handler: %v", err)
	}

	// Handler 2: Pattern subscriber without tags
	handler2 := func(event framework.Event) error {
		handler2Received <- true
		return nil
	}
	err = eb.SubscribeWithTags("system.*", handler2, nil)
	if err != nil {
		t.Fatalf("SubscribeWithTags failed for pattern handler: %v", err)
	}

	t.Run("critical system event", func(t *testing.T) {
		// Send critical system event
		data := &framework.EventData{
			KeyValue: map[string]string{"severity": "critical"},
		}
		metadata := framework.NewEventMetadata("test", "system.error").
			WithTags("critical")

		err := eb.BroadcastWithMetadata("system.error", data, metadata)
		if err != nil {
			t.Fatalf("BroadcastWithMetadata failed: %v", err)
		}

		// Both handlers should receive this event
		checkHandlerReceived(t, "handler1", handler1Received, true)
		checkHandlerReceived(t, "handler2", handler2Received, true)
	})

	t.Run("non-critical system event", func(t *testing.T) {
		// Clear channels
		clearChannel(handler1Received)
		clearChannel(handler2Received)

		// Send non-critical system event
		data := &framework.EventData{
			KeyValue: map[string]string{"severity": "info"},
		}
		metadata := framework.NewEventMetadata("test", "system.info")

		err := eb.BroadcastWithMetadata("system.info", data, metadata)
		if err != nil {
			t.Fatalf("BroadcastWithMetadata failed: %v", err)
		}

		// Only handler2 should receive this event
		checkHandlerReceived(t, "handler1", handler1Received, false)
		checkHandlerReceived(t, "handler2", handler2Received, true)
	})
}

func TestEventBus_BackwardCompatibility(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	received := make(chan bool, 1)
	handler := func(event framework.Event) error {
		received <- true
		return nil
	}

	// Use the old Subscribe method (should work as before)
	err := eb.Subscribe("compat.event", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send event with tags (handler should still receive it since it has no tag filter)
	data := &framework.EventData{
		KeyValue: map[string]string{"test": "value"},
	}
	metadata := framework.NewEventMetadata("test", "compat.event").
		WithTags("some", "tags")

	err = eb.BroadcastWithMetadata("compat.event", data, metadata)
	if err != nil {
		t.Fatalf("BroadcastWithMetadata failed: %v", err)
	}

	checkHandlerReceived(t, "backward compatible handler", received, true)
}

// Helper functions
func checkHandlerReceived(t *testing.T, handlerName string, ch <-chan bool, shouldReceive bool) {
	t.Helper()
	select {
	case <-ch:
		if !shouldReceive {
			t.Errorf("%s received event but should not have", handlerName)
		}
	case <-time.After(100 * time.Millisecond):
		if shouldReceive {
			t.Errorf("%s did not receive event but should have", handlerName)
		}
	}
}

func clearChannel(ch chan bool) {
	for {
		select {
		case <-ch:
			// Keep draining
		default:
			return
		}
	}
}
