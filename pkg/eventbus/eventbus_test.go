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

	eb.RegisterPlugin("test-plugin", plugin)

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

func TestEventBus_SQLHandlers(t *testing.T) {
	eb := NewEventBus(nil)
	defer func() {
		if err := eb.Stop(); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	// Test Query without handler
	_, err := eb.Query("SELECT 1", nil)
	if err == nil {
		t.Error("Query should fail without handler")
	}

	// Test Exec without handler
	err = eb.Exec("INSERT INTO test VALUES (1)", nil)
	if err == nil {
		t.Error("Exec should fail without handler")
	}

	// Set handlers
	queryHandler := func(event framework.Event) error {
		return nil
	}
	execHandler := func(event framework.Event) error {
		return nil
	}
	eb.SetSQLHandlers(queryHandler, execHandler)

	// Test Query with handler (will still fail due to placeholder implementation)
	_, err = eb.Query("SELECT 1", nil)
	if err == nil {
		t.Error("Query should fail with placeholder implementation")
	}

	// Test Exec with handler
	err = eb.Exec("INSERT INTO test VALUES (1)", nil)
	if err != nil {
		t.Errorf("Exec failed: %v", err)
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
