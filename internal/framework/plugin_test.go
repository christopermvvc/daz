package framework

import (
	"encoding/json"
	"testing"
	"time"
)

type mockPlugin struct {
	initCalled    bool
	startCalled   bool
	stopCalled    bool
	eventsHandled int
}

func (m *mockPlugin) Init(config json.RawMessage, bus EventBus) error {
	m.initCalled = true
	return nil
}

func (m *mockPlugin) Start() error {
	m.startCalled = true
	return nil
}

func (m *mockPlugin) Stop() error {
	m.stopCalled = true
	return nil
}

func (m *mockPlugin) HandleEvent(event Event) error {
	m.eventsHandled++
	return nil
}

func (m *mockPlugin) Status() PluginStatus {
	return PluginStatus{
		Name:          "mock",
		State:         "running",
		EventsHandled: int64(m.eventsHandled),
	}
}

func TestPluginInterface(t *testing.T) {
	var _ Plugin = &mockPlugin{}

	plugin := &mockPlugin{}

	if err := plugin.Init(nil, nil); err != nil {
		t.Errorf("Init() returned error: %v", err)
	}

	if !plugin.initCalled {
		t.Error("Init() was not called")
	}

	if err := plugin.Start(); err != nil {
		t.Errorf("Start() returned error: %v", err)
	}

	if !plugin.startCalled {
		t.Error("Start() was not called")
	}

	if err := plugin.Stop(); err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}

	if !plugin.stopCalled {
		t.Error("Stop() was not called")
	}
}

type mockEvent struct {
	eventType string
	timestamp time.Time
}

func (m *mockEvent) Type() string {
	return m.eventType
}

func (m *mockEvent) Timestamp() time.Time {
	return m.timestamp
}

func TestEventInterface(t *testing.T) {
	now := time.Now()
	event := &mockEvent{
		eventType: "test.event",
		timestamp: now,
	}

	if event.Type() != "test.event" {
		t.Errorf("Type() = %v, want %v", event.Type(), "test.event")
	}

	if event.Timestamp() != now {
		t.Errorf("Timestamp() = %v, want %v", event.Timestamp(), now)
	}
}
