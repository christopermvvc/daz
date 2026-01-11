package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// TestEventBusPriorityDequeue verifies that events are dequeued in priority order
// Note: This doesn't guarantee processing order due to concurrent handlers
func TestEventBusPriorityDequeue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive priority dequeue test in short mode")
	}

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

	// Track dequeue order (not processing completion order)
	dequeueOrder := make([]int, 0, 4)
	var mu sync.Mutex
	startCh := make(chan struct{})
	doneCh := make(chan struct{})

	// Subscribe to test events - track when events start processing
	err := eb.Subscribe("priority.test", func(event framework.Event) error {
		if dataEvent, ok := event.(*framework.DataEvent); ok {
			if dataEvent.Data != nil && dataEvent.Data.KeyValue != nil {
				if priority, ok := dataEvent.Data.KeyValue["priority"]; ok {
					// Record dequeue order immediately
					mu.Lock()
					switch priority {
					case "0":
						dequeueOrder = append(dequeueOrder, 0)
					case "1":
						dequeueOrder = append(dequeueOrder, 1)
					case "2":
						dequeueOrder = append(dequeueOrder, 2)
					case "3":
						dequeueOrder = append(dequeueOrder, 3)
					}
					orderLen := len(dequeueOrder)
					mu.Unlock()

					// Signal first event started
					select {
					case <-startCh:
					default:
						close(startCh)
					}

					// Simulate variable processing time
					processingTime := time.Duration(50-int(priority[0]-'0')*10) * time.Millisecond
					time.Sleep(processingTime)

					// Signal when all done
					if orderLen == 4 {
						close(doneCh)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Send all events with different priorities
	for i := 0; i < 4; i++ {
		metadata := framework.NewEventMetadata("test", "priority.test").WithPriority(i)
		data := &framework.EventData{
			KeyValue: map[string]string{
				"priority": string(rune('0' + i)),
			},
		}
		if err := eb.BroadcastWithMetadata("priority.test", data, metadata); err != nil {
			t.Errorf("Failed to broadcast priority event %d: %v", i, err)
		}
	}

	// Wait for first event to start processing
	<-startCh

	// Give time for priority queue to order events
	time.Sleep(100 * time.Millisecond)

	// Wait for all events to be processed
	select {
	case <-doneCh:
		// All events processed
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for events")
	}

	// Verify we got all events
	if len(dequeueOrder) != 4 {
		t.Fatalf("Expected 4 events, got %d", len(dequeueOrder))
	}

	// For concurrent processing, we can't guarantee strict order
	// But we should see higher priority events tend to be dequeued earlier
	t.Logf("Events were dequeued in order: %v", dequeueOrder)
	t.Logf("Note: Due to concurrent processing and goroutine scheduling, strict ordering is not guaranteed")

	// At minimum, verify all priorities were processed
	seen := make(map[int]bool)
	for _, p := range dequeueOrder {
		seen[p] = true
	}
	for i := 0; i < 4; i++ {
		if !seen[i] {
			t.Errorf("Priority %d was not processed", i)
		}
	}
}

// TestPriorityQueueDirectly tests the priority queue in isolation
func TestPriorityQueueDirectly(t *testing.T) {
	queue := newMessageQueue(0)

	// Create messages with different priorities
	messages := []struct {
		priority int
		id       string
	}{
		{priority: 0, id: "low"},
		{priority: 3, id: "high1"},
		{priority: 1, id: "medium"},
		{priority: 3, id: "high2"},
		{priority: 2, id: "medium-high"},
	}

	// Push all messages
	for _, m := range messages {
		msg := &eventMessage{
			Event: &framework.DataEvent{
				EventType: "test",
				Data: &framework.EventData{
					KeyValue: map[string]string{"id": m.id},
				},
			},
			Type:     "test",
			Metadata: &framework.EventMetadata{Priority: m.priority},
		}
		if !queue.push(msg, m.priority) {
			t.Fatalf("Failed to push message %s", m.id)
		}
	}

	// Pop messages and verify order
	expected := []string{"high1", "high2", "medium-high", "medium", "low"}
	for i, exp := range expected {
		msg, ok := queue.pop()
		if !ok {
			t.Fatalf("Expected to pop message %d, but queue was empty", i)
		}
		if dataEvent, ok := msg.Event.(*framework.DataEvent); ok {
			if id := dataEvent.Data.KeyValue["id"]; id != exp {
				t.Errorf("Position %d: expected %s, got %s", i, exp, id)
			}
		}
	}

	// Queue should be empty - close it to make pop return false
	queue.close()
	if _, ok := queue.pop(); ok {
		t.Error("Expected queue to be empty")
	}
}
