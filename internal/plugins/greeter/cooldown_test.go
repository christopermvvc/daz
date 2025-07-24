package greeter

import (
	"testing"
	"time"
)

func TestCooldownManager_IsOnCooldown(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*CooldownManager)
		channel  string
		user     string
		expected bool
	}{
		{
			name:     "no cooldown set",
			setup:    func(cm *CooldownManager) {},
			channel:  "#test",
			user:     "user1",
			expected: false,
		},
		{
			name: "cooldown active",
			setup: func(cm *CooldownManager) {
				cm.SetCooldown("#test", "user1", 30*time.Minute)
			},
			channel:  "#test",
			user:     "user1",
			expected: true,
		},
		{
			name: "different channel not on cooldown",
			setup: func(cm *CooldownManager) {
				cm.SetCooldown("#test", "user1", 30*time.Minute)
			},
			channel:  "#other",
			user:     "user1",
			expected: false,
		},
		{
			name: "different user not on cooldown",
			setup: func(cm *CooldownManager) {
				cm.SetCooldown("#test", "user1", 30*time.Minute)
			},
			channel:  "#test",
			user:     "user2",
			expected: false,
		},
		{
			name: "expired cooldown",
			setup: func(cm *CooldownManager) {
				// Manually set an expired cooldown
				cm.mu.Lock()
				key := makeKey("#test", "user1")
				cm.cooldowns[key] = time.Now().Add(-1 * time.Hour)
				cm.mu.Unlock()
			},
			channel:  "#test",
			user:     "user1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewCooldownManager()
			defer cm.Close()

			tt.setup(cm)

			got := cm.IsOnCooldown(tt.channel, tt.user)
			if got != tt.expected {
				t.Errorf("IsOnCooldown() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCooldownManager_SetCooldown(t *testing.T) {
	cm := NewCooldownManager()
	defer cm.Close()

	channel := "#test"
	user := "testuser"

	// Verify no cooldown initially
	if cm.IsOnCooldown(channel, user) {
		t.Error("Expected no cooldown initially")
	}

	// Set random cooldown
	cm.SetRandomCooldown(channel, user)

	// Verify cooldown is active
	if !cm.IsOnCooldown(channel, user) {
		t.Error("Expected cooldown to be active after setting")
	}

	// Verify cooldown expiration time is within expected range
	cm.mu.RLock()
	key := makeKey(channel, user)
	expires := cm.cooldowns[key]
	cm.mu.RUnlock()

	minDuration := 45 * time.Minute
	maxDuration := 3 * time.Hour
	actualDuration := time.Until(expires)

	if actualDuration < minDuration || actualDuration > maxDuration {
		t.Errorf("Cooldown duration %v is outside expected range [%v, %v]",
			actualDuration, minDuration, maxDuration)
	}
}

func TestCooldownManager_Cleanup(t *testing.T) {
	cm := &CooldownManager{
		cooldowns: make(map[string]time.Time),
		ticker:    time.NewTicker(1 * time.Hour), // Won't trigger during test
		done:      make(chan struct{}),
	}
	defer cm.Close()

	// Add some cooldowns - both expired and active
	now := time.Now()
	cm.cooldowns["#test:expired1"] = now.Add(-2 * time.Hour)
	cm.cooldowns["#test:expired2"] = now.Add(-1 * time.Hour)
	cm.cooldowns["#test:active1"] = now.Add(1 * time.Hour)
	cm.cooldowns["#test:active2"] = now.Add(2 * time.Hour)

	// Run cleanup
	cm.removeExpired()

	// Verify expired entries were removed
	if _, exists := cm.cooldowns["#test:expired1"]; exists {
		t.Error("Expected expired1 to be removed")
	}
	if _, exists := cm.cooldowns["#test:expired2"]; exists {
		t.Error("Expected expired2 to be removed")
	}

	// Verify active entries remain
	if _, exists := cm.cooldowns["#test:active1"]; !exists {
		t.Error("Expected active1 to remain")
	}
	if _, exists := cm.cooldowns["#test:active2"]; !exists {
		t.Error("Expected active2 to remain")
	}

	// Verify count
	if len(cm.cooldowns) != 2 {
		t.Errorf("Expected 2 cooldowns remaining, got %d", len(cm.cooldowns))
	}
}

func TestCooldownManager_ConcurrentAccess(t *testing.T) {
	cm := NewCooldownManager()
	defer cm.Close()

	done := make(chan struct{})

	// Concurrent writers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				cm.SetCooldown("#test", string(rune('a'+id)), 30*time.Minute)
			}
			done <- struct{}{}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				cm.IsOnCooldown("#test", string(rune('a'+id)))
			}
			done <- struct{}{}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestMakeKey(t *testing.T) {
	tests := []struct {
		channel  string
		user     string
		expected string
	}{
		{"#test", "user1", "#test:user1"},
		{"#channel", "nick", "#channel:nick"},
		{"", "user", ":user"},
		{"#chan", "", "#chan:"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := makeKey(tt.channel, tt.user)
			if got != tt.expected {
				t.Errorf("makeKey(%q, %q) = %q, want %q",
					tt.channel, tt.user, got, tt.expected)
			}
		})
	}
}

func TestCooldownManager_Close(t *testing.T) {
	cm := NewCooldownManager()

	// Close should not panic
	cm.Close()

	// Verify ticker is stopped by checking done channel is closed
	select {
	case <-cm.done:
		// Expected - channel is closed
	default:
		t.Error("Expected done channel to be closed")
	}
}
