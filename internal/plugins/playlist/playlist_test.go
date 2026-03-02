package playlist

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type playlistMockBus struct {
	mu         sync.Mutex
	broadcasts []mockBroadcast
}

type mockBroadcast struct {
	eventType string
	data      *framework.EventData
}

func (b *playlistMockBus) Broadcast(eventType string, data *framework.EventData) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcasts = append(b.broadcasts, mockBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *playlistMockBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *playlistMockBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *playlistMockBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *playlistMockBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, nil
}

func (b *playlistMockBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *playlistMockBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *playlistMockBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *playlistMockBus) RegisterPlugin(name string, plugin framework.Plugin) error { return nil }

func (b *playlistMockBus) UnregisterPlugin(name string) error { return nil }

func (b *playlistMockBus) GetDroppedEventCounts() map[string]int64 { return map[string]int64{} }

func (b *playlistMockBus) GetDroppedEventCount(eventType string) int64 { return 0 }

func TestInitDefaultsToAdminOnlyWhenEmptyConfig(t *testing.T) {
	t.Parallel()

	plugin := New().(*Plugin)

	if err := plugin.Init(json.RawMessage("{}"), &playlistMockBus{}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if plugin.adminOnly != true {
		t.Fatalf("adminOnly = %v, want true", plugin.adminOnly)
	}
}

func TestInitRespectsExplicitAdminOnlyFalse(t *testing.T) {
	t.Parallel()

	plugin := New().(*Plugin)
	config := json.RawMessage(`{"admin_only": false}`)

	if err := plugin.Init(config, &playlistMockBus{}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if plugin.adminOnly != false {
		t.Fatalf("adminOnly = %v, want false", plugin.adminOnly)
	}
}

func TestRegisterCommandRespectsAdminOnlyFlag(t *testing.T) {
	t.Parallel()

	plugin := New().(*Plugin)
	bus := &playlistMockBus{}

	if err := plugin.Init(json.RawMessage(`{}`), bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := plugin.registerCommand(); err != nil {
		t.Fatalf("registerCommand() error = %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bus.broadcasts))
	}
	req := bus.broadcasts[0].data.PluginRequest
	if req == nil || req.Data == nil {
		t.Fatal("missing plugin request payload for registration")
	}
	if got := req.Data.KeyValue["admin_only"]; got != "true" {
		t.Fatalf("admin_only = %q, want %q", got, "true")
	}
}

func TestRegisterCommandRespectsExplicitAdminOnlyFalse(t *testing.T) {
	t.Parallel()

	plugin := New().(*Plugin)
	bus := &playlistMockBus{}

	if err := plugin.Init(json.RawMessage(`{"admin_only": false}`), bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := plugin.registerCommand(); err != nil {
		t.Fatalf("registerCommand() error = %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bus.broadcasts))
	}
	req := bus.broadcasts[0].data.PluginRequest
	if req == nil || req.Data == nil {
		t.Fatal("missing plugin request payload for registration")
	}
	if got := req.Data.KeyValue["admin_only"]; got != "" {
		t.Fatalf("admin_only = %q, want empty string", got)
	}
}
