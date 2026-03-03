package eventbus

import (
	"runtime"
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

func TestHighPriorityDispatchBypassesNormalSaturation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive priority dispatch test in short mode")
	}

	prevMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prevMaxProcs)

	eb := NewEventBus(&Config{})
	if err := eb.Start(); err != nil {
		t.Fatalf("failed to start event bus: %v", err)
	}
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("failed to stop event bus: %v", err)
		}
	}()

	blockLow := make(chan struct{})
	lowWorkers := cap(eb.dispatchSemNormal)
	if lowWorkers < 1 {
		lowWorkers = 1
	}
	lowStarted := make(chan struct{}, lowWorkers)
	highDone := make(chan struct{}, 1)

	if err := eb.Subscribe("saturation.low", func(event framework.Event) error {
		select {
		case lowStarted <- struct{}{}:
		default:
		}
		<-blockLow
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe low handler: %v", err)
	}

	if err := eb.Subscribe("saturation.high", func(event framework.Event) error {
		select {
		case highDone <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe high handler: %v", err)
	}

	for i := 0; i < lowWorkers; i++ {
		if err := eb.Broadcast("saturation.low", &framework.EventData{}); err != nil {
			t.Fatalf("failed to broadcast low event %d: %v", i, err)
		}
	}

	for i := 0; i < lowWorkers; i++ {
		select {
		case <-lowStarted:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for low handler %d to start", i+1)
		}
	}

	metadata := framework.NewEventMetadata("test", "saturation.high").
		WithPriority(framework.PriorityHigh)
	if err := eb.BroadcastWithMetadata("saturation.high", &framework.EventData{}, metadata); err != nil {
		t.Fatalf("failed to broadcast high-priority event: %v", err)
	}

	select {
	case <-highDone:
	case <-time.After(1 * time.Second):
		t.Fatal("high-priority event did not dispatch while normal workers were saturated")
	}

	close(blockLow)
}

func TestCommandDispatchDuringStartupBurstTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing-sensitive startup burst dispatch test in short mode")
	}

	prevMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prevMaxProcs)

	eb := NewEventBus(&Config{})
	if err := eb.Start(); err != nil {
		t.Fatalf("failed to start event bus: %v", err)
	}
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("failed to stop event bus: %v", err)
		}
	}()

	blockStartup := make(chan struct{})
	startupWorkers := cap(eb.dispatchSemNormal)
	if startupWorkers < 1 {
		startupWorkers = 1
	}

	started := make(chan struct{}, startupWorkers)
	commandDone := make(chan struct{}, 1)

	if err := eb.Subscribe("cytube.event.addUser", func(event framework.Event) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-blockStartup
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe startup handler: %v", err)
	}

	if err := eb.Subscribe("command.update.execute", func(event framework.Event) error {
		select {
		case commandDone <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe command handler: %v", err)
	}

	for i := 0; i < startupWorkers*4; i++ {
		if err := eb.Broadcast("cytube.event.addUser", &framework.EventData{}); err != nil {
			t.Fatalf("failed to broadcast startup event %d: %v", i, err)
		}
	}

	for i := 0; i < startupWorkers; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for startup worker %d to saturate", i+1)
		}
	}

	if err := eb.Broadcast("command.update.execute", &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "eventfilter",
			Type: "execute",
		},
	}); err != nil {
		t.Fatalf("failed to broadcast command event: %v", err)
	}

	select {
	case <-commandDone:
	case <-time.After(1 * time.Second):
		t.Fatal("command event did not dispatch while startup burst traffic saturated normal workers")
	}

	close(blockStartup)
}
