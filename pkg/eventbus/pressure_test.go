package eventbus

import (
	"context"
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

func TestEffectivePriorityPromotesCommandEvents(t *testing.T) {
	eb := NewEventBus(&Config{})

	msg := &eventMessage{
		Type: "command.update.execute",
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				From: "eventfilter",
				Type: "execute",
			},
		},
		Metadata: framework.NewEventMetadata("test", "command.update.execute"),
	}

	if got := eb.effectivePriority(msg); got != framework.PriorityHigh {
		t.Fatalf("expected command event effective priority high, got %d", got)
	}
}

func TestEffectivePriorityPromotesEventfilterPluginExecRequests(t *testing.T) {
	eb := NewEventBus(&Config{})

	msg := &eventMessage{
		Type: "plugin.request",
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				From: "eventfilter",
				Type: "execute",
			},
		},
		Metadata: framework.NewEventMetadata("test", "plugin.request"),
	}

	if got := eb.effectivePriority(msg); got != framework.PriorityHigh {
		t.Fatalf("expected eventfilter execute request effective priority high, got %d", got)
	}
}

func TestAcquireDispatchSlotUsesDedicatedCommandLane(t *testing.T) {
	eb := NewEventBus(&Config{})
	msg := &eventMessage{
		Type:     "command.update.execute",
		Metadata: framework.NewEventMetadata("test", "command.update.execute"),
	}

	release := eb.acquireDispatchSlot(msg)
	defer release()

	if got := len(eb.dispatchSemCommand); got != 1 {
		t.Fatalf("expected command dispatch semaphore to be used, got len=%d", got)
	}
	if got := len(eb.dispatchSemHigh); got != 0 {
		t.Fatalf("expected high-priority semaphore to remain unused, got len=%d", got)
	}
}

func TestAcquireDispatchSlotFallsBackWhenCommandLaneBusy(t *testing.T) {
	eb := NewEventBus(&Config{})
	// Saturate command lane so command dispatch must borrow another reserved lane.
	for i := 0; i < cap(eb.dispatchSemCommand); i++ {
		eb.dispatchSemCommand <- struct{}{}
	}
	defer func() {
		for len(eb.dispatchSemCommand) > 0 {
			<-eb.dispatchSemCommand
		}
	}()

	msg := &eventMessage{
		Type:     "command.update.execute",
		Metadata: framework.NewEventMetadata("test", "command.update.execute"),
	}

	release := eb.acquireDispatchSlot(msg)
	defer release()

	if len(eb.dispatchSemHigh) == 0 && len(eb.dispatchSemNormal) == 0 {
		t.Fatal("expected fallback lane (high or normal) to be used when command lane is busy")
	}
}

func TestComputeDispatchLaneSizes(t *testing.T) {
	tests := []struct {
		name        string
		gomaxprocs  int
		wantWorkers int
		wantCommand int
		wantHigh    int
		wantNormal  int
	}{
		{
			name:        "single cpu profile",
			gomaxprocs:  1,
			wantWorkers: 3,
			wantCommand: 1,
			wantHigh:    1,
			wantNormal:  1,
		},
		{
			name:        "dual cpu profile",
			gomaxprocs:  2,
			wantWorkers: 6,
			wantCommand: 1,
			wantHigh:    2,
			wantNormal:  3,
		},
		{
			name:        "multi cpu profile",
			gomaxprocs:  4,
			wantWorkers: 16,
			wantCommand: 2,
			wantHigh:    4,
			wantNormal:  10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workers, command, high, normal := computeDispatchLaneSizes(tc.gomaxprocs)
			if workers != tc.wantWorkers || command != tc.wantCommand || high != tc.wantHigh || normal != tc.wantNormal {
				t.Fatalf(
					"computeDispatchLaneSizes(%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					tc.gomaxprocs,
					workers, command, high, normal,
					tc.wantWorkers, tc.wantCommand, tc.wantHigh, tc.wantNormal,
				)
			}
			if command+high+normal != workers {
				t.Fatalf("lane sum mismatch: command=%d high=%d normal=%d workers=%d", command, high, normal, workers)
			}
		})
	}
}

func TestRequestWaitsForQueueCapacityInsteadOfDropping(t *testing.T) {
	eb := NewEventBus(&Config{
		BufferSizes: map[string]int{
			"plugin.request": 1,
		},
	})
	if err := eb.Start(); err != nil {
		t.Fatalf("failed to start event bus: %v", err)
	}
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("failed to stop event bus: %v", err)
		}
	}()

	releaseHandlers := make(chan struct{})
	var handledRequest int64

	if err := eb.Subscribe("plugin.request", func(event framework.Event) error {
		dataEvent, ok := event.(*framework.DataEvent)
		if ok && dataEvent.Data != nil && dataEvent.Data.KeyValue != nil && dataEvent.Data.KeyValue["kind"] == "request" {
			atomic.AddInt64(&handledRequest, 1)
			<-releaseHandlers
			eb.DeliverResponse("req-corr", &framework.EventData{
				KeyValue: map[string]string{"ok": "1"},
			}, nil)
			return nil
		}

		<-releaseHandlers
		return nil
	}); err != nil {
		t.Fatalf("failed to subscribe plugin lane: %v", err)
	}

	stopFlood := make(chan struct{})
	doneFlood := make(chan struct{})
	go func() {
		defer close(doneFlood)
		for {
			select {
			case <-stopFlood:
				return
			default:
				_ = eb.Send("flood", "flood.event", &framework.EventData{
					KeyValue: map[string]string{"kind": "flood"},
				})
			}
		}
	}()

	time.Sleep(60 * time.Millisecond)

	resultCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer cancel()

		_, err := eb.Request(
			ctx,
			"eventfilter",
			"execute",
			&framework.EventData{
				KeyValue: map[string]string{"kind": "request"},
			},
			&framework.EventMetadata{CorrelationID: "req-corr"},
		)
		resultCh <- err
	}()

	time.Sleep(120 * time.Millisecond)
	close(releaseHandlers)
	close(stopFlood)
	<-doneFlood

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("expected queued request to succeed once capacity was available, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request result")
	}

	if got := atomic.LoadInt64(&handledRequest); got != 1 {
		t.Fatalf("expected request handler to run once, got %d", got)
	}
}
