package eventbus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestSendWithMetadataRoutesSQLFastLanes(t *testing.T) {
	eb := NewEventBus(&Config{})
	if err := eb.Start(); err != nil {
		t.Fatalf("failed to start event bus: %v", err)
	}
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("failed to stop event bus: %v", err)
		}
	}()

	var sqlLaneCalls int64
	var pluginLaneCalls int64

	if err := eb.Subscribe("sql.exec.request", func(event framework.Event) error {
		atomic.AddInt64(&sqlLaneCalls, 1)
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe sql lane: %v", err)
	}

	if err := eb.Subscribe("plugin.request", func(event framework.Event) error {
		atomic.AddInt64(&pluginLaneCalls, 1)
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe plugin lane: %v", err)
	}

	metadata := framework.NewEventMetadata("test", "sql.exec.request")
	if err := eb.SendWithMetadata("sql", "sql.exec.request", &framework.EventData{}, metadata); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt64(&sqlLaneCalls); got != 1 {
		t.Fatalf("expected sql lane to receive 1 event, got %d", got)
	}
	if got := atomic.LoadInt64(&pluginLaneCalls); got != 0 {
		t.Fatalf("expected plugin lane to receive 0 events, got %d", got)
	}
}

func TestShouldDropForQueuePressureLowValueEvent(t *testing.T) {
	eb := NewEventBus(&Config{})
	queue := newMessageQueue(2)

	msg := &eventMessage{
		Type:     "cytube.event.mediaUpdate",
		Metadata: framework.NewEventMetadata("test", "cytube.event.mediaUpdate"),
	}

	if ok := queue.push(msg, framework.PriorityNormal); !ok {
		t.Fatal("expected queue push to succeed")
	}

	if !eb.shouldDropForQueuePressure(msg.Type, msg.Metadata, queue) {
		t.Fatal("expected low-value event to be dropped under queue pressure")
	}
}

func TestShouldNotDropHighPriorityEvents(t *testing.T) {
	eb := NewEventBus(&Config{})
	queue := newMessageQueue(1)

	metadata := framework.NewEventMetadata("test", "cytube.event.chatMsg").WithPriority(framework.PriorityHigh)
	msg := &eventMessage{Type: "cytube.event.chatMsg", Metadata: metadata}
	if ok := queue.push(msg, metadata.Priority); !ok {
		t.Fatal("expected queue push to succeed")
	}

	if eb.shouldDropForQueuePressure(msg.Type, msg.Metadata, queue) {
		t.Fatal("did not expect high-priority event to be dropped")
	}
}
