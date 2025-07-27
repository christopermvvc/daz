package eventbus

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

// mockPlugin implements framework.Plugin for testing
type mockPlugin struct {
	name string
}

func (m *mockPlugin) Init(config json.RawMessage, bus framework.EventBus) error {
	return nil
}

func (m *mockPlugin) Start() error {
	return nil
}

func (m *mockPlugin) Stop() error {
	return nil
}

func (m *mockPlugin) HandleEvent(event framework.Event) error {
	return nil
}

func (m *mockPlugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:  m.name,
		State: "running",
	}
}

func (m *mockPlugin) Name() string {
	return "mock"
}

func TestNewPluginRegistry(t *testing.T) {
	pr := NewPluginRegistry()
	if pr == nil {
		t.Fatal("NewPluginRegistry returned nil")
	}
	if pr.plugins == nil {
		t.Fatal("plugins map not initialized")
	}
}

func TestPluginRegistry_Register(t *testing.T) {
	pr := NewPluginRegistry()
	plugin := &mockPlugin{name: "test"}

	// Test successful registration
	err := pr.Register("test", plugin)
	if err != nil {
		t.Errorf("Register failed: %v", err)
	}

	// Test duplicate registration
	err = pr.Register("test", plugin)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}
}

func TestPluginRegistry_Get(t *testing.T) {
	pr := NewPluginRegistry()
	plugin := &mockPlugin{name: "test"}

	// Register plugin
	if err := pr.Register("test", plugin); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test getting existing plugin
	got, exists := pr.Get("test")
	if !exists {
		t.Error("Plugin should exist")
	}
	if got != plugin {
		t.Error("Retrieved plugin doesn't match")
	}

	// Test getting non-existent plugin
	_, exists = pr.Get("nonexistent")
	if exists {
		t.Error("Non-existent plugin should not exist")
	}
}

func TestPluginRegistry_GetAll(t *testing.T) {
	pr := NewPluginRegistry()
	plugin1 := &mockPlugin{name: "plugin1"}
	plugin2 := &mockPlugin{name: "plugin2"}

	if err := pr.Register("plugin1", plugin1); err != nil {
		t.Fatalf("Register plugin1 failed: %v", err)
	}
	if err := pr.Register("plugin2", plugin2); err != nil {
		t.Fatalf("Register plugin2 failed: %v", err)
	}

	all := pr.GetAll()
	if len(all) != 2 {
		t.Errorf("Expected 2 plugins, got %d", len(all))
	}

	// Verify both plugins are present
	if all["plugin1"] != plugin1 {
		t.Error("plugin1 not found or doesn't match")
	}
	if all["plugin2"] != plugin2 {
		t.Error("plugin2 not found or doesn't match")
	}
}

func TestPluginRegistry_Remove(t *testing.T) {
	pr := NewPluginRegistry()
	plugin := &mockPlugin{name: "test"}

	// Register plugin
	if err := pr.Register("test", plugin); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test successful removal
	err := pr.Remove("test")
	if err != nil {
		t.Errorf("Remove failed: %v", err)
	}

	// Verify plugin is removed
	_, exists := pr.Get("test")
	if exists {
		t.Error("Plugin should not exist after removal")
	}

	// Test removing non-existent plugin
	err = pr.Remove("nonexistent")
	if err == nil {
		t.Error("Expected error for removing non-existent plugin")
	}
}

func TestPluginRegistry_ConcurrentAccess(t *testing.T) {
	pr := NewPluginRegistry()
	var wg sync.WaitGroup

	// Concurrent registrations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := string(rune('a' + id))
			plugin := &mockPlugin{name: name}
			_ = pr.Register(name, plugin)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pr.GetAll()
		}()
	}

	wg.Wait()

	// Verify all plugins were registered
	all := pr.GetAll()
	if len(all) != 10 {
		t.Errorf("Expected 10 plugins, got %d", len(all))
	}
}
