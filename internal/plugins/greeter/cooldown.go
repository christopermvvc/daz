package greeter

import (
	"math/rand"
	"sync"
	"time"
)

// CooldownManager manages per-user, per-channel cooldowns for greetings
type CooldownManager struct {
	mu        sync.RWMutex
	cooldowns map[string]time.Time // key format: "channel:user"
	ticker    *time.Ticker
	done      chan struct{}
}

// NewCooldownManager creates a new cooldown manager with automatic cleanup
func NewCooldownManager() *CooldownManager {
	cm := &CooldownManager{
		cooldowns: make(map[string]time.Time),
		ticker:    time.NewTicker(30 * time.Minute), // cleanup every 30 minutes
		done:      make(chan struct{}),
	}

	// Start cleanup goroutine
	go cm.cleanup()

	return cm
}

// IsOnCooldown checks if a user is on cooldown for a specific channel
func (cm *CooldownManager) IsOnCooldown(channel, user string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	key := makeKey(channel, user)
	expires, exists := cm.cooldowns[key]
	if !exists {
		return false
	}

	// Check if cooldown has expired
	return time.Now().Before(expires)
}

// SetCooldown sets a random cooldown between 45 minutes and 3 hours for a user in a channel
func (cm *CooldownManager) SetCooldown(channel, user string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Generate random cooldown duration between 45 minutes (2700 seconds) and 3 hours (10800 seconds)
	minSeconds := 45 * 60     // 45 minutes
	maxSeconds := 3 * 60 * 60 // 3 hours
	randomSeconds := rand.Intn(maxSeconds-minSeconds+1) + minSeconds

	duration := time.Duration(randomSeconds) * time.Second
	key := makeKey(channel, user)
	cm.cooldowns[key] = time.Now().Add(duration)
}

// cleanup periodically removes expired cooldowns from memory
func (cm *CooldownManager) cleanup() {
	for {
		select {
		case <-cm.ticker.C:
			cm.removeExpired()
		case <-cm.done:
			cm.ticker.Stop()
			return
		}
	}
}

// removeExpired removes all expired cooldowns from the map
func (cm *CooldownManager) removeExpired() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	for key, expires := range cm.cooldowns {
		if now.After(expires) {
			delete(cm.cooldowns, key)
		}
	}
}

// Close stops the cleanup goroutine and releases resources
func (cm *CooldownManager) Close() {
	close(cm.done)
}

// makeKey creates a unique key for channel and user combination
func makeKey(channel, user string) string {
	return channel + ":" + user
}
