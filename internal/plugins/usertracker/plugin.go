package usertracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements user tracking functionality
type Plugin struct {
	name     string
	eventBus framework.EventBus
	config   *Config
	running  bool
	mu       sync.RWMutex

	// Shutdown management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// In-memory user state cache
	users map[string]*UserState

	// Plugin status tracking
	status framework.PluginStatus
}

// Config holds usertracker plugin configuration
type Config struct {
	// The channel to track users for
	Channel string `json:"channel"`
	// How long to wait before marking a user as inactive
	InactivityTimeout time.Duration `json:"inactivity_timeout"`
}

// UserState tracks current state of a user
type UserState struct {
	Username     string
	Rank         int
	JoinedAt     time.Time
	LastActivity time.Time
	IsActive     bool
}

// New creates a new usertracker plugin instance that implements framework.Plugin
func New() framework.Plugin {
	return &Plugin{
		name: "usertracker",
		config: &Config{
			InactivityTimeout: 30 * time.Minute,
		},
		users: make(map[string]*UserState),
		status: framework.PluginStatus{
			Name:  "usertracker",
			State: "initialized",
		},
	}
}

// NewPlugin creates a new usertracker plugin instance
// Deprecated: Use New() instead
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{
			InactivityTimeout: 30 * time.Minute,
		}
	}

	return &Plugin{
		name:   "usertracker",
		config: config,
		users:  make(map[string]*UserState),
		status: framework.PluginStatus{
			Name:  "usertracker",
			State: "initialized",
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Init initializes the plugin with configuration and event bus
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	// Parse configuration if provided
	if len(config) > 0 {
		var cfg Config
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
		p.config = &cfg
	}

	// Ensure default config if not set
	if p.config == nil {
		p.config = &Config{
			InactivityTimeout: 30 * time.Minute,
		}
	}

	// Set default channel if not specified
	if p.config.Channel == "" {
		p.config.Channel = "default"
	}

	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	log.Printf("[UserTracker] Initialized with inactivity timeout: %v", p.config.InactivityTimeout)
	return nil
}

// Initialize sets up the plugin with the event bus
// Deprecated: Use Init() instead
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	return p.Init(nil, eventBus)
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("usertracker plugin already running")
	}

	// Create database tables now that database is connected
	if err := p.createTables(); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Subscribe to user events
	if err := p.eventBus.Subscribe(eventbus.EventCytubeUserJoin, p.handleUserJoin); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to user join events: %w", err)
	}

	if err := p.eventBus.Subscribe(eventbus.EventCytubeUserLeave, p.handleUserLeave); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to user leave events: %w", err)
	}

	// Subscribe to chat messages to track activity
	if err := p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to chat events: %w", err)
	}

	// Subscribe to command requests for user info
	if err := p.eventBus.Subscribe("plugin.usertracker.seen", p.handleSeenRequest); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to seen requests: %w", err)
	}

	// Start periodic cleanup of inactive sessions
	p.wg.Add(1)
	go p.cleanupInactiveSessions()

	p.running = true
	p.status.State = "running"
	p.status.Uptime = time.Since(time.Now())
	log.Println("[UserTracker] Started user tracking")
	return nil
}

// Stop halts the plugin operation
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	// Cancel context to stop goroutines
	p.cancel()

	// Wait for goroutines to finish
	p.wg.Wait()

	p.running = false
	p.status.State = "stopped"
	log.Println("[UserTracker] Stopped user tracking")
	return nil
}

// HandleEvent processes incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	// This plugin uses specific event subscriptions
	p.status.EventsHandled++
	return nil
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := p.status
	if p.running {
		status.Uptime = time.Since(time.Now().Add(-status.Uptime))
	}
	return status
}

// createTables creates the necessary database tables
func (p *Plugin) createTables() error {
	// User sessions table
	sessionsTableSQL := `
	CREATE TABLE IF NOT EXISTS ***REMOVED***tracker_sessions (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		rank INT DEFAULT 0,
		joined_at TIMESTAMP NOT NULL,
		left_at TIMESTAMP,
		last_activity TIMESTAMP NOT NULL,
		is_active BOOLEAN DEFAULT TRUE,
		UNIQUE(channel, username, joined_at)
	);
	
	CREATE INDEX IF NOT EXISTS idx_usertracker_active_sessions 
		ON ***REMOVED***tracker_sessions(channel, username) 
		WHERE is_active = TRUE;
	
	CREATE INDEX IF NOT EXISTS idx_usertracker_last_activity 
		ON ***REMOVED***tracker_sessions(channel, username, last_activity);
	`

	// User history table for long-term tracking
	historyTableSQL := `
	CREATE TABLE IF NOT EXISTS ***REMOVED***tracker_history (
		id BIGSERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		event_type VARCHAR(50) NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		metadata JSONB
	);

	CREATE INDEX IF NOT EXISTS idx_usertracker_history_user ON ***REMOVED***tracker_history(channel, username, timestamp);
	`

	// Execute table creation
	err := p.eventBus.Exec(sessionsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	err = p.eventBus.Exec(historyTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create history table: %w", err)
	}

	return nil
}

// handleUserJoin processes user join events
func (p *Plugin) handleUserJoin(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.UserJoin == nil {
		return nil
	}

	userJoin := dataEvent.Data.UserJoin
	// Use channel from event if available, otherwise fall back to config
	channel := userJoin.Channel
	if channel == "" {
		channel = p.config.Channel
	}
	now := time.Now()

	// Update in-memory state
	p.mu.Lock()
	p.users[userJoin.Username] = &UserState{
		Username:     userJoin.Username,
		Rank:         userJoin.UserRank,
		JoinedAt:     now,
		LastActivity: now,
		IsActive:     true,
	}
	p.mu.Unlock()

	// Record in database
	sessionSQL := `
		INSERT INTO ***REMOVED***tracker_sessions 
			(channel, username, rank, joined_at, last_activity, is_active)
		VALUES ($1, $2, $3, $4, $5, TRUE)
	`
	err := p.eventBus.Exec(sessionSQL, channel, userJoin.Username, userJoin.UserRank, now, now)
	if err != nil {
		log.Printf("[UserTracker] Error recording user join: %v", err)
	}

	// Record in history
	historySQL := `
		INSERT INTO ***REMOVED***tracker_history 
			(channel, username, event_type, timestamp, metadata)
		VALUES ($1, $2, 'join', $3, $4)
	`
	metadata := fmt.Sprintf(`{"rank": %d}`, userJoin.UserRank)
	err = p.eventBus.Exec(historySQL, channel, userJoin.Username, now, metadata)
	if err != nil {
		log.Printf("[UserTracker] Error recording join history: %v", err)
	}

	log.Printf("[UserTracker] User joined: %s (rank %d)", userJoin.Username, userJoin.UserRank)
	p.status.EventsHandled++
	return nil
}

// handleUserLeave processes user leave events
func (p *Plugin) handleUserLeave(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.UserLeave == nil {
		return nil
	}

	userLeave := dataEvent.Data.UserLeave
	// Use channel from event if available, otherwise fall back to config
	channel := userLeave.Channel
	if channel == "" {
		channel = p.config.Channel
	}
	now := time.Now()

	// Update in-memory state
	p.mu.Lock()
	if user, exists := p.users[userLeave.Username]; exists {
		user.IsActive = false
		user.LastActivity = now
	}
	p.mu.Unlock()

	// Update session in database
	sessionSQL := `
		UPDATE ***REMOVED***tracker_sessions 
		SET left_at = $1, is_active = FALSE, last_activity = $1
		WHERE channel = $2 AND username = $3 AND is_active = TRUE
	`
	err := p.eventBus.Exec(sessionSQL, now, channel, userLeave.Username)
	if err != nil {
		log.Printf("[UserTracker] Error recording user leave: %v", err)
	}

	// Record in history
	historySQL := `
		INSERT INTO ***REMOVED***tracker_history 
			(channel, username, event_type, timestamp)
		VALUES ($1, $2, 'leave', $3)
	`
	err = p.eventBus.Exec(historySQL, channel, userLeave.Username, now)
	if err != nil {
		log.Printf("[UserTracker] Error recording leave history: %v", err)
	}

	log.Printf("[UserTracker] User left: %s", userLeave.Username)
	p.status.EventsHandled++
	return nil
}

// handleChatMessage updates user activity on chat
func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	chat := dataEvent.Data.ChatMessage
	now := time.Now()

	// Update in-memory state
	p.mu.Lock()
	if user, exists := p.users[chat.Username]; exists {
		user.LastActivity = now
	}
	p.mu.Unlock()

	// Update last activity in database
	updateSQL := `
		UPDATE ***REMOVED***tracker_sessions 
		SET last_activity = $1
		WHERE channel = $2 AND username = $3 AND is_active = TRUE
	`
	err := p.eventBus.Exec(updateSQL, now, chat.Channel, chat.Username)
	if err != nil {
		log.Printf("[UserTracker] Error updating activity: %v", err)
	}

	p.status.EventsHandled++
	return nil
}

// handleSeenRequest handles requests for user last seen info
func (p *Plugin) handleSeenRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.RawMessage == nil {
		return nil
	}

	targetUser := dataEvent.Data.RawMessage.Message
	channel := p.config.Channel

	// Query database for user info
	query := `
		SELECT username, joined_at, left_at, last_activity, is_active
		FROM ***REMOVED***tracker_sessions
		WHERE channel = $1 AND LOWER(username) = LOWER($2)
		ORDER BY last_activity DESC
		LIMIT 1
	`

	// Create context with timeout for the query
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := p.eventBus.QuerySync(ctx, query, channel, targetUser)
	if err != nil {
		return fmt.Errorf("failed to query user info: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	// Parse result
	var username string
	var joinedAt, lastActivity time.Time
	var leftAt sql.NullTime
	var isActive bool

	if rows.Next() {
		err := rows.Scan(&username, &joinedAt, &leftAt, &lastActivity, &isActive)
		if err != nil {
			return fmt.Errorf("failed to scan user info: %w", err)
		}

		// Format response
		var response string
		if isActive {
			duration := time.Since(lastActivity)
			response = fmt.Sprintf("%s is currently online (last active %s ago)", username, formatDuration(duration))
		} else if leftAt.Valid {
			duration := time.Since(leftAt.Time)
			response = fmt.Sprintf("%s was last seen %s ago", username, formatDuration(duration))
		} else {
			duration := time.Since(lastActivity)
			response = fmt.Sprintf("%s was last seen %s ago", username, formatDuration(duration))
		}

		// Send response
		responseData := &framework.EventData{
			RawMessage: &framework.RawMessageData{
				Message: response,
			},
		}
		return p.eventBus.Send("commandrouter", "plugin.response", responseData)
	}

	// User not found
	responseData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: fmt.Sprintf("I haven't seen %s", targetUser),
		},
	}
	return p.eventBus.Send("commandrouter", "plugin.response", responseData)
}

// cleanupInactiveSessions periodically marks inactive sessions
func (p *Plugin) cleanupInactiveSessions() {
	defer p.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.doCleanup()
		case <-p.ctx.Done():
			return
		}
	}
}

// doCleanup performs the actual cleanup
func (p *Plugin) doCleanup() {
	cutoffTime := time.Now().Add(-p.config.InactivityTimeout)

	// Update database
	updateSQL := `
		UPDATE ***REMOVED***tracker_sessions 
		SET is_active = FALSE
		WHERE is_active = TRUE AND last_activity < $1
	`
	err := p.eventBus.Exec(updateSQL, cutoffTime)
	if err != nil {
		log.Printf("[UserTracker] Error cleaning up inactive sessions: %v", err)
	}

	// Clean up in-memory state
	p.mu.Lock()
	for username, user := range p.users {
		if !user.IsActive || user.LastActivity.Before(cutoffTime) {
			delete(p.users, username)
		}
	}
	p.mu.Unlock()
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	} else {
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%d days", days)
	}
}
