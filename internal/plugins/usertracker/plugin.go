package usertracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements user tracking functionality
type Plugin struct {
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	config    *Config
	running   bool
	mu        sync.RWMutex

	// Shutdown management
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	readyChan chan struct{}

	// In-memory user state cache
	users map[string]*UserState

	// Plugin status tracking
	status framework.PluginStatus

	// Userlist processing state
	processingUserlist map[string]bool
	userlistMutex      sync.RWMutex
}

// Config holds usertracker plugin configuration
type Config struct {
	// How long to wait before marking a user as inactive (in minutes)
	InactivityTimeoutMinutes int `json:"inactivity_timeout_minutes"`
	// How long to wait before marking a user as inactive
	InactivityTimeout time.Duration
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
		users:              make(map[string]*UserState),
		readyChan:          make(chan struct{}),
		processingUserlist: make(map[string]bool),
		status: framework.PluginStatus{
			Name:  "usertracker",
			State: "initialized",
		},
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql"} // UserTracker depends on SQL plugin for database operations
}

// Ready returns true when the plugin is ready to accept requests
func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
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
		name:               "usertracker",
		config:             config,
		users:              make(map[string]*UserState),
		readyChan:          make(chan struct{}),
		processingUserlist: make(map[string]bool),
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
		// Convert minutes to duration
		if cfg.InactivityTimeoutMinutes > 0 {
			cfg.InactivityTimeout = time.Duration(cfg.InactivityTimeoutMinutes) * time.Minute
		}
		p.config = &cfg
	}

	// Ensure default config if not set
	if p.config == nil {
		p.config = &Config{
			InactivityTimeout: 30 * time.Minute,
		}
	}

	// Ensure we have a valid timeout
	if p.config.InactivityTimeout <= 0 {
		p.config.InactivityTimeout = 30 * time.Minute
	}

	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	logger.Debug("UserTracker", "Initialized with inactivity timeout: %v", p.config.InactivityTimeout)
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

	// Create database tables in background to avoid blocking startup
	go func() {
		// Wait a moment to allow SQL plugin to fully initialize
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()

		select {
		case <-p.ctx.Done():
			return
		case <-timer.C:
			if err := p.createTables(); err != nil {
				logger.Error("UserTracker", "Failed to create tables: %v", err)
				p.status.LastError = err
				// Don't fail plugin startup, it can retry later
			}
		}
	}()

	// Subscribe to userlist events for bulk user updates
	if err := p.eventBus.Subscribe("cytube.event.userlist.start", p.handleUserListStart); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to userlist.start events: %w", err)
	}

	if err := p.eventBus.Subscribe("cytube.event.userlist.end", p.handleUserListEnd); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to userlist.end events: %w", err)
	}

	// Subscribe to user events
	// Note: CyTube sends "addUser" events for initial users and new joins
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to addUser events: %w", err)
	}

	// Also subscribe to userJoin for users joining after we're connected
	if err := p.eventBus.Subscribe("cytube.event.userJoin", p.handleUserJoin); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to userJoin events: %w", err)
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

	// Signal that the plugin is ready
	close(p.readyChan)

	logger.Debug("UserTracker", "Started user tracking")
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
	logger.Info("UserTracker", "Stopped user tracking")
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
	CREATE TABLE IF NOT EXISTS daz_user_tracker_sessions (
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
		ON daz_usertracker_sessions(channel, username) 
		WHERE is_active = TRUE;
	
	CREATE INDEX IF NOT EXISTS idx_usertracker_last_activity 
		ON daz_usertracker_sessions(channel, username, last_activity);
	`

	// User history table for long-term tracking
	historyTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_user_tracker_history (
		id BIGSERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		event_type VARCHAR(50) NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		metadata JSONB
	);

	CREATE INDEX IF NOT EXISTS idx_usertracker_history_user ON daz_usertracker_history(channel, username, timestamp);
	`

	// Execute table creation
	err := p.sqlClient.Exec(sessionsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	err = p.sqlClient.Exec(historyTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create history table: %w", err)
	}

	return nil
}

// handleUserJoin processes user join events (both addUser and userJoin)
func (p *Plugin) handleUserJoin(event framework.Event) error {
	var username string
	var userRank int
	var channel string

	// Handle direct AddUserEvent (from cytube.event.addUser)
	if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		username = addUserEvent.Username
		userRank = addUserEvent.UserRank
		channel = addUserEvent.ChannelName
	} else if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		// Handle wrapped events
		if dataEvent.Data.UserJoin != nil {
			username = dataEvent.Data.UserJoin.Username
			userRank = dataEvent.Data.UserJoin.UserRank
			channel = dataEvent.Data.UserJoin.Channel
		} else if dataEvent.Data.RawEvent != nil {
			// Try to extract from RawEvent
			if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				userRank = addUserEvent.UserRank
				channel = addUserEvent.ChannelName
			} else {
				return nil
			}
		} else {
			return nil
		}
	} else {
		return nil
	}

	if username == "" || channel == "" {
		logger.Warn("UserTracker", "Skipping user join event without username or channel")
		return nil
	}

	// Check if we're processing a userlist for this channel
	p.userlistMutex.RLock()
	processingUserlist := p.processingUserlist[channel]
	p.userlistMutex.RUnlock()

	now := time.Now()

	// Update in-memory state
	p.mu.Lock()
	p.users[username] = &UserState{
		Username:     username,
		Rank:         userRank,
		JoinedAt:     now,
		LastActivity: now,
		IsActive:     true,
	}
	p.mu.Unlock()

	// If we're processing a userlist, update the existing session instead of creating a new one
	if processingUserlist {
		// First try to reactivate the most recent inactive session
		// Note: We look for the most recent session by joined_at, not by whether it has left_at
		updateSQL := `
			UPDATE daz_user_tracker_sessions 
			SET is_active = TRUE, 
			    left_at = NULL,
			    last_activity = $1,
			    rank = $2
			WHERE id = (
			    SELECT id FROM daz_user_tracker_sessions
			    WHERE channel = $3 
			      AND username = $4
			      AND is_active = FALSE
			    ORDER BY joined_at DESC
			    LIMIT 1
			)
			RETURNING id
		`
		
		// Use a query to check if update affected any rows
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		
		sqlHelper := framework.NewSQLRequestHelper(p.eventBus, p.name)
		rows, err := sqlHelper.FastQuery(ctx, updateSQL, now, userRank, channel, username)
		if err != nil {
			logger.Error("UserTracker", "Error checking for existing session: %v", err)
		} else {
			defer rows.Close()
			
			// If we updated an existing session, we're done
			if rows.Next() {
				logger.Debug("UserTracker", "Reactivated existing session for %s", username)
				return nil
			}
		}
		
		// No existing session found, create a new one
		sessionSQL := `
			INSERT INTO daz_user_tracker_sessions 
				(channel, username, rank, joined_at, last_activity, is_active)
			VALUES ($1, $2, $3, $4, $5, TRUE)
			ON CONFLICT (channel, username, joined_at) 
			DO UPDATE SET 
				rank = EXCLUDED.rank,
				last_activity = EXCLUDED.last_activity,
				is_active = TRUE,
				left_at = NULL
		`
		err = p.sqlClient.Exec(sessionSQL,
			channel,
			username,
			userRank,
			now,
			now)
		if err != nil {
			logger.Error("UserTracker", "Error creating new session during userlist: %v", err)
		}
	} else {
		// Regular user join - first mark all existing active sessions as inactive
		// to prevent multiple active sessions for the same user
		// Use SKIP LOCKED to avoid deadlocks
		deactivateSQL := `
			UPDATE daz_user_tracker_sessions 
			SET is_active = FALSE, left_at = $1
			WHERE id IN (
				SELECT id 
				FROM daz_user_tracker_sessions 
				WHERE channel = $2 AND username = $3 AND is_active = TRUE
				FOR UPDATE SKIP LOCKED
			)
		`
		err := p.sqlClient.Exec(deactivateSQL, now, channel, username)
		if err != nil {
			logger.Error("UserTracker", "Error deactivating old sessions: %v", err)
		}

		// Now create new session with conflict handling for duplicate events
		sessionSQL := `
			INSERT INTO daz_user_tracker_sessions 
				(channel, username, rank, joined_at, last_activity, is_active)
			VALUES ($1, $2, $3, $4, $5, TRUE)
			ON CONFLICT (channel, username, joined_at) 
			DO UPDATE SET 
				rank = EXCLUDED.rank,
				last_activity = EXCLUDED.last_activity,
				is_active = TRUE
		`
		err = p.sqlClient.Exec(sessionSQL,
			channel,
			username,
			userRank,
			now,
			now)
		if err != nil {
			logger.Error("UserTracker", "Error recording user join: %v", err)
		}
	}

	// Record in history
	historySQL := `
		INSERT INTO daz_user_tracker_history 
			(channel, username, event_type, timestamp, metadata)
		VALUES ($1, $2, 'join', $3, $4)
	`
	metadata := fmt.Sprintf(`{"rank": %d, "from_userlist": %t}`, userRank, processingUserlist)
	err := p.sqlClient.Exec(historySQL,
		channel,
		username,
		now,
		metadata)
	if err != nil {
		logger.Error("UserTracker", "Error recording join history: %v", err)
	}

	logger.Info("UserTracker", "User joined: %s (rank %d)", username, userRank)
	p.status.EventsHandled++
	return nil
}

// handleUserListStart processes the start of a userlist event
func (p *Plugin) handleUserListStart(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.RawMessage == nil {
		return nil
	}

	channel := dataEvent.Data.RawMessage.Channel
	userCount := dataEvent.Data.RawMessage.Message

	p.userlistMutex.Lock()
	p.processingUserlist[channel] = true
	p.userlistMutex.Unlock()

	logger.Info("UserTracker", "Starting userlist processing for channel %s with %s users", channel, userCount)

	// Mark all existing users as inactive but preserve their join times
	// They will be reactivated if they appear in the userlist
	// This prevents duplicate greetings while preserving session history
	deactivateSQL := `
		UPDATE daz_user_tracker_sessions 
		SET is_active = FALSE
		WHERE id IN (
			SELECT id 
			FROM daz_user_tracker_sessions 
			WHERE channel = $1 AND is_active = TRUE
			FOR UPDATE SKIP LOCKED
		)
	`
	err := p.sqlClient.Exec(deactivateSQL, channel)
	if err != nil {
		logger.Error("UserTracker", "Error deactivating users before userlist: %v", err)
	}

	// Clear in-memory cache - we'll rebuild it from the userlist
	p.mu.Lock()
	p.users = make(map[string]*UserState)
	p.mu.Unlock()

	p.status.EventsHandled++
	return nil
}

// handleUserListEnd processes the end of a userlist event
func (p *Plugin) handleUserListEnd(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.RawMessage == nil {
		return nil
	}

	channel := dataEvent.Data.RawMessage.Channel
	userCount := dataEvent.Data.RawMessage.Message

	p.userlistMutex.Lock()
	delete(p.processingUserlist, channel)
	p.userlistMutex.Unlock()

	logger.Info("UserTracker", "Completed userlist processing for channel %s with %s users", channel, userCount)

	// Record userlist sync event in history
	historySQL := `
		INSERT INTO daz_user_tracker_history 
			(channel, username, event_type, timestamp, metadata)
		VALUES ($1, '_system', 'userlist_sync', $2, $3)
	`
	// Create proper JSON metadata
	metadataMap := map[string]interface{}{
		"user_count_message": userCount,
		"timestamp":          time.Now().Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadataMap)

	err := p.sqlClient.Exec(historySQL, channel, time.Now(), string(metadataJSON))
	if err != nil {
		logger.Error("UserTracker", "Error recording userlist sync history: %v", err)
	}

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
	// Get channel from event (must be present)
	channel := userLeave.Channel
	if channel == "" {
		logger.Warn("UserTracker", "Skipping user leave event without channel information")
		return nil
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
		UPDATE daz_user_tracker_sessions 
		SET left_at = $1, is_active = FALSE, last_activity = $1
		WHERE channel = $2 AND username = $3 AND is_active = TRUE
	`
	err := p.sqlClient.Exec(sessionSQL,
		now,
		channel,
		userLeave.Username)
	if err != nil {
		logger.Error("UserTracker", "Error recording user leave: %v", err)
	}

	// Record in history
	historySQL := `
		INSERT INTO daz_user_tracker_history 
			(channel, username, event_type, timestamp)
		VALUES ($1, $2, 'leave', $3)
	`
	err = p.sqlClient.Exec(historySQL,
		channel,
		userLeave.Username,
		now)
	if err != nil {
		logger.Error("UserTracker", "Error recording leave history: %v", err)
	}

	logger.Info("UserTracker", "User left: %s", userLeave.Username)
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
		UPDATE daz_user_tracker_sessions 
		SET last_activity = $1
		WHERE channel = $2 AND username = $3 AND is_active = TRUE
	`
	err := p.sqlClient.Exec(updateSQL,
		now,
		chat.Channel,
		chat.Username)
	if err != nil {
		logger.Error("UserTracker", "Error updating activity: %v", err)
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
	// Get channel from event context
	channel := ""
	if dataEvent.Data.ChatMessage != nil {
		channel = dataEvent.Data.ChatMessage.Channel
	}
	if channel == "" {
		logger.Warn("UserTracker", "Skipping seen request without channel context")
		return nil
	}

	// Extract correlation ID from plugin request if available
	correlationID := ""
	if dataEvent.Data.PluginRequest != nil {
		correlationID = dataEvent.Data.PluginRequest.ID
	}

	// Query database for user info
	query := `
		SELECT username, joined_at, left_at, last_activity, is_active
		FROM daz_user_tracker_sessions
		WHERE channel = $1 AND LOWER(username) = LOWER($2)
		ORDER BY last_activity DESC
		LIMIT 1
	`

	// Create context with timeout for the query
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use the new SQL request helper with retry logic for user info queries
	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, "usertracker")
	rows, err := sqlHelper.SlowQuery(ctx, query, channel, targetUser)
	if err != nil {
		return fmt.Errorf("failed to query user info: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("UserTracker", "Failed to close rows: %v", err)
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

		// Send proper plugin response
		return p.sendPluginResponse(correlationID, response, true, "")
	}

	// User not found
	notFoundResponse := fmt.Sprintf("I haven't seen %s", targetUser)
	return p.sendPluginResponse(correlationID, notFoundResponse, true, "")
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

	// Update database using SKIP LOCKED to avoid deadlocks
	updateSQL := `
		UPDATE daz_user_tracker_sessions 
		SET is_active = FALSE
		WHERE id IN (
			SELECT id 
			FROM daz_user_tracker_sessions 
			WHERE is_active = TRUE AND last_activity < $1
			FOR UPDATE SKIP LOCKED
		)
	`
	err := p.sqlClient.Exec(updateSQL,
		cutoffTime)
	if err != nil {
		logger.Error("UserTracker", "Error cleaning up inactive sessions: %v", err)
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

// sendPluginResponse sends a properly formatted plugin response
func (p *Plugin) sendPluginResponse(correlationID, message string, success bool, errorMsg string) error {
	// Create the response data structure
	responseData := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      correlationID,
			From:    p.name,
			Success: success,
			Error:   errorMsg,
			Data: &framework.ResponseData{
				CommandResult: &framework.CommandResultData{
					Success: success,
					Output:  message,
					Error:   errorMsg,
				},
			},
		},
	}

	// Broadcast the response using the proper event type
	return p.eventBus.Broadcast(eventbus.EventPluginResponse, responseData)
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
