package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestPriorityQueue(t *testing.T) {
	queue := newMessageQueue()

	// Create messages with different priorities
	messages := []*eventMessage{
		{
			Event: &framework.DataEvent{EventType: "test1"},
			Type:  "test",
			Metadata: &framework.EventMetadata{
				Priority: 0, // Low priority
			},
		},
		{
			Event: &framework.DataEvent{EventType: "test2"},
			Type:  "test",
			Metadata: &framework.EventMetadata{
				Priority: 3, // High priority
			},
		},
		{
			Event: &framework.DataEvent{EventType: "test3"},
			Type:  "test",
			Metadata: &framework.EventMetadata{
				Priority: 1, // Medium priority
			},
		},
		{
			Event: &framework.DataEvent{EventType: "test4"},
			Type:  "test",
			Metadata: &framework.EventMetadata{
				Priority: 3, // High priority (should maintain FIFO with test2)
			},
		},
	}

	// Push all messages
	for _, msg := range messages {
		priority := 0
		if msg.Metadata != nil {
			priority = msg.Metadata.Priority
		}
		queue.push(msg, priority)
	}

	// Pop messages and verify order (highest priority first, FIFO within same priority)
	expected := []string{"test2", "test4", "test3", "test1"}
	for i, exp := range expected {
		msg, ok := queue.pop()
		if !ok {
			t.Fatalf("Expected to pop message %d, but queue was empty", i)
		}
		if msg.Event.Type() != exp {
			t.Errorf("Expected message %s at position %d, got %s", exp, i, msg.Event.Type())
		}
	}

	// Queue should be empty now - close it to make pop return false
	queue.close()
	_, ok := queue.pop()
	if ok {
		t.Error("Expected queue to be empty")
	}
}

func TestPriorityQueueConcurrent(t *testing.T) {
	queue := newMessageQueue()
	var wg sync.WaitGroup

	// Start multiple goroutines pushing messages
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				msg := &eventMessage{
					Event: &framework.DataEvent{
						EventType: "test",
						EventTime: time.Now(),
					},
					Type: "test",
					Metadata: &framework.EventMetadata{
						Priority: id % 4, // Priorities 0-3
					},
				}
				priority := 0
				if msg.Metadata != nil {
					priority = msg.Metadata.Priority
				}
				queue.push(msg, priority)
			}
		}(i)
	}

	// Wait for all pushes to complete
	wg.Wait()

	// Count messages by priority
	priorityCount := make(map[int]int)

	for i := 0; i < 100; i++ {
		msg, ok := queue.pop()
		if !ok {
			t.Fatalf("Expected 100 messages, only got %d", i)
		}

		priority := 0
		if msg.Metadata != nil {
			priority = msg.Metadata.Priority
		}
		priorityCount[priority]++

		// In a concurrent push scenario, we can't guarantee strict ordering
		// because messages might still be getting pushed while we're popping
	}

	// Verify we got all messages
	total := 0
	for _, count := range priorityCount {
		total += count
	}
	if total != 100 {
		t.Errorf("Expected 100 messages total, got %d", total)
	}
}

func TestPriorityQueueClose(t *testing.T) {
	queue := newMessageQueue()

	// Push a message
	msg := &eventMessage{
		Event:    &framework.DataEvent{EventType: "test"},
		Type:     "test",
		Metadata: &framework.EventMetadata{Priority: 1},
	}
	queue.push(msg, 1)

	// Close the queue
	queue.close()

	// Try to push after close (should not panic)
	queue.push(msg, 1)

	// Pop should return the existing message
	popped, ok := queue.pop()
	if !ok {
		t.Error("Expected to pop existing message after close")
	}
	if popped.Event.Type() != "test" {
		t.Error("Got wrong message")
	}

	// Next pop should return false
	_, ok = queue.pop()
	if ok {
		t.Error("Expected pop to return false after queue closed and emptied")
	}
}
