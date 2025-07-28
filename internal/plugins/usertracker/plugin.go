package usertracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
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
	p.status.Uptime = time.Since(time.Now().UTC())

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
		status.Uptime = time.Since(time.Now().UTC().Add(-status.Uptime))
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

	// Create the stored function for atomic upserts
	if err := p.createStoredFunction(); err != nil {
		logger.Warn("UserTracker", "Failed to create stored function (will use fallback): %v", err)
		// Don't fail startup - we have a fallback
	}

	// Migrate existing data to UTC if needed
	if err := p.migrateToUTC(); err != nil {
		logger.Error("UserTracker", "Failed to migrate to UTC: %v", err)
		// Don't fail startup - new data will be UTC
	}

	return nil
}

// handleUserJoin processes user join events (both addUser and userJoin)
func (p *Plugin) handleUserJoin(event framework.Event) error {
	// Extract user information from the event
	username, userRank, channel := p.extractUserJoinInfo(event)
	if username == "" || channel == "" {
		logger.Warn("UserTracker", "Skipping user join event without username or channel")
		return nil
	}

	// Update in-memory state
	now := time.Now().UTC()
	p.updateInMemoryUser(username, userRank, now)

	// Handle database updates based on context
	p.userlistMutex.RLock()
	processingUserlist := p.processingUserlist[channel]
	p.userlistMutex.RUnlock()

	if processingUserlist {
		p.handleUserlistJoin(channel, username, userRank, now)
	} else {
		p.handleRegularJoin(channel, username, userRank, now)
	}

	// Record in history
	p.recordJoinHistory(channel, username, userRank, now, processingUserlist)

	logger.Info("UserTracker", "User joined: %s (rank %d)", username, userRank)
	p.status.EventsHandled++
	return nil
}

// extractUserJoinInfo extracts user information from various event types
func (p *Plugin) extractUserJoinInfo(event framework.Event) (username string, userRank int, channel string) {
	// Handle direct AddUserEvent (from cytube.event.addUser)
	if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		return addUserEvent.Username, addUserEvent.UserRank, addUserEvent.ChannelName
	}

	// Handle wrapped events
	if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		if dataEvent.Data.UserJoin != nil {
			return dataEvent.Data.UserJoin.Username,
				dataEvent.Data.UserJoin.UserRank,
				dataEvent.Data.UserJoin.Channel
		}

		if dataEvent.Data.RawEvent != nil {
			if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				return addUserEvent.Username, addUserEvent.UserRank, addUserEvent.ChannelName
			}
		}
	}

	return "", 0, ""
}

// updateInMemoryUser updates the in-memory user state
func (p *Plugin) updateInMemoryUser(username string, userRank int, now time.Time) {
	p.mu.Lock()
	p.users[username] = &UserState{
		Username:     username,
		Rank:         userRank,
		JoinedAt:     now,
		LastActivity: now,
		IsActive:     true,
	}
	p.mu.Unlock()
}

// handleUserlistJoin handles user join during userlist processing
func (p *Plugin) handleUserlistJoin(channel, username string, userRank int, now time.Time) {
	// Use atomic upsert to handle session reactivation or creation
	upsertSQL := `
		SELECT session_id, was_reactivated 
		FROM upsert_user_session($1, $2, $3, $4)
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := sqlHelper.FastQuery(ctx, upsertSQL, channel, username, userRank, now)
	if err != nil {
		// Fallback to non-atomic approach if function doesn't exist
		if strings.Contains(err.Error(), "upsert_user_session") || strings.Contains(err.Error(), "does not exist") {
			logger.Debug("UserTracker", "Stored function not available, using fallback approach")
			p.handleUserJoinFallback(channel, username, userRank, now)
		} else {
			logger.Error("UserTracker", "Error upserting user session: %v", err)
		}
		return
	}
	defer rows.Close()

	// Log whether we reactivated or created a new session
	if rows.Next() {
		var sessionID int
		var wasReactivated bool
		if err := rows.Scan(&sessionID, &wasReactivated); err == nil {
			if wasReactivated {
				logger.Debug("UserTracker", "Reactivated existing session for %s (ID: %d)", username, sessionID)
			} else {
				logger.Debug("UserTracker", "Created new session for %s (ID: %d)", username, sessionID)
			}
		}
	}
}

// handleRegularJoin handles regular user join events (not during userlist)
func (p *Plugin) handleRegularJoin(channel, username string, userRank int, now time.Time) {
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

// recordJoinHistory records user join event in history table
func (p *Plugin) recordJoinHistory(channel, username string, userRank int, now time.Time, fromUserlist bool) {
	historySQL := `
		INSERT INTO daz_user_tracker_history 
			(channel, username, event_type, timestamp, metadata)
		VALUES ($1, $2, 'join', $3, $4)
	`
	metadata := fmt.Sprintf(`{"rank": %d, "from_userlist": %t}`, userRank, fromUserlist)
	err := p.sqlClient.Exec(historySQL,
		channel,
		username,
		now,
		metadata)
	if err != nil {
		logger.Error("UserTracker", "Error recording join history: %v", err)
	}
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
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadataMap)

	err := p.sqlClient.Exec(historySQL, channel, time.Now().UTC(), string(metadataJSON))
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
	now := time.Now().UTC()

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
	now := time.Now().UTC()

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
	cutoffTime := time.Now().UTC().Add(-p.config.InactivityTimeout)

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

// createStoredFunction creates the upsert_user_session function in the database
func (p *Plugin) createStoredFunction() error {
	// First check if the function already exists
	checkSQL := `
		SELECT COUNT(*) 
		FROM pg_proc 
		WHERE proname = 'upsert_user_session'
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := sqlHelper.FastQuery(ctx, checkSQL)
	if err != nil {
		// Can't check, try to create anyway
		logger.Debug("UserTracker", "Could not check for existing function: %v", err)
	} else {
		defer rows.Close()
		var count int
		if rows.Next() {
			if err := rows.Scan(&count); err == nil && count > 0 {
				logger.Debug("UserTracker", "Stored function already exists, skipping creation")
				return nil
			}
		}
	}

	// Function doesn't exist, create it
	functionSQL := `
CREATE OR REPLACE FUNCTION upsert_user_session(
    p_channel VARCHAR(255),
    p_username VARCHAR(255),
    p_rank INT,
    p_now TIMESTAMP
) RETURNS TABLE (
    session_id INT,
    was_reactivated BOOLEAN
) AS $$
DECLARE
    v_session_id INT;
    v_was_reactivated BOOLEAN := FALSE;
BEGIN
    -- First try to reactivate the most recent inactive session
    UPDATE daz_user_tracker_sessions 
    SET is_active = TRUE, 
        left_at = NULL,
        last_activity = p_now,
        rank = p_rank
    WHERE id = (
        SELECT id FROM daz_user_tracker_sessions
        WHERE channel = p_channel 
          AND username = p_username
          AND is_active = FALSE
        ORDER BY joined_at DESC
        LIMIT 1
    )
    RETURNING id INTO v_session_id;
    
    -- If we found and reactivated a session, we're done
    IF FOUND THEN
        v_was_reactivated := TRUE;
        RETURN QUERY SELECT v_session_id, v_was_reactivated;
        RETURN;
    END IF;
    
    -- No existing inactive session found, ensure no active sessions exist
    UPDATE daz_user_tracker_sessions 
    SET is_active = FALSE
    WHERE channel = p_channel 
      AND username = p_username 
      AND is_active = TRUE;
    
    -- Create a new session
    INSERT INTO daz_user_tracker_sessions 
        (channel, username, rank, joined_at, last_activity, is_active)
    VALUES (p_channel, p_username, p_rank, p_now, p_now, TRUE)
    ON CONFLICT (channel, username, joined_at) 
    DO UPDATE SET 
        rank = EXCLUDED.rank,
        last_activity = EXCLUDED.last_activity,
        is_active = TRUE,
        left_at = NULL
    RETURNING id INTO v_session_id;
    
    RETURN QUERY SELECT v_session_id, v_was_reactivated;
END;
$$ LANGUAGE plpgsql;`

	err = p.sqlClient.Exec(functionSQL)
	if err != nil {
		return fmt.Errorf("failed to create stored function: %w", err)
	}

	logger.Info("UserTracker", "Successfully created upsert_user_session stored function")
	return nil
}

// migrateToUTC migrates existing timestamps to UTC
func (p *Plugin) migrateToUTC() error {
	// Check if we've already migrated by looking for a migration marker
	checkMigrationSQL := `
		SELECT COUNT(*) 
		FROM daz_user_tracker_history 
		WHERE event_type = '_utc_migration_complete'
		LIMIT 1
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := sqlHelper.FastQuery(ctx, checkMigrationSQL)
	if err != nil {
		// Table might not exist yet, that's ok
		return nil
	}
	defer rows.Close()

	var migrationDone int
	if rows.Next() {
		if err := rows.Scan(&migrationDone); err != nil {
			return fmt.Errorf("failed to scan migration status: %w", err)
		}
	}

	// Already migrated
	if migrationDone > 0 {
		logger.Debug("UserTracker", "UTC migration already completed")
		return nil
	}

	// Get the server's timezone offset dynamically
	_, offset := time.Now().Zone()
	offsetHours := offset / 3600

	// Calculate threshold based on offset
	// For UTC (offset 0), we need a different approach
	var thresholdSeconds int
	if offsetHours == 0 {
		// If server is already UTC, check if timestamps are in the future
		thresholdSeconds = 3600 // 1 hour in future suggests non-UTC
	} else {
		// Use half the offset as threshold to be more accurate
		absOffset := offset
		if absOffset < 0 {
			absOffset = -absOffset
		}
		thresholdSeconds = absOffset / 2
		if thresholdSeconds < 3600 {
			thresholdSeconds = 3600 // Minimum 1 hour threshold
		}
	}

	// Check if any timestamps need migration
	checkSQL := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM daz_user_tracker_sessions 
		WHERE ABS(EXTRACT(EPOCH FROM (joined_at - NOW()))) > %d
		LIMIT 1
	`, thresholdSeconds)

	rows2, err := sqlHelper.FastQuery(ctx, checkSQL)
	if err != nil {
		return fmt.Errorf("failed to check for UTC migration need: %w", err)
	}
	defer rows2.Close()

	var needsMigration int
	if rows2.Next() {
		if err := rows2.Scan(&needsMigration); err != nil {
			return fmt.Errorf("failed to scan count: %w", err)
		}
	}

	// If we need migration and we know the offset
	if needsMigration > 0 && offsetHours != 0 {
		logger.Info("UserTracker", "Migrating timestamps to UTC (offset: %d hours)", offsetHours)

		// Migrate sessions table by subtracting the offset
		sessionMigrationSQL := fmt.Sprintf(`
			UPDATE daz_user_tracker_sessions
			SET 
			    joined_at = joined_at - INTERVAL '%d hours',
			    last_activity = last_activity - INTERVAL '%d hours',
			    left_at = CASE 
			        WHEN left_at IS NOT NULL 
			        THEN left_at - INTERVAL '%d hours'
			        ELSE NULL 
			    END
			WHERE ABS(EXTRACT(EPOCH FROM (joined_at - NOW()))) > 7200
		`, offsetHours, offsetHours, offsetHours)

		err = p.sqlClient.Exec(sessionMigrationSQL)
		if err != nil {
			return fmt.Errorf("failed to migrate sessions to UTC: %w", err)
		}

		// Migrate history table
		historyMigrationSQL := fmt.Sprintf(`
			UPDATE daz_user_tracker_history
			SET timestamp = timestamp - INTERVAL '%d hours'
			WHERE ABS(EXTRACT(EPOCH FROM (timestamp - NOW()))) > 7200
		`, offsetHours)

		err = p.sqlClient.Exec(historyMigrationSQL)
		if err != nil {
			return fmt.Errorf("failed to migrate history to UTC: %w", err)
		}

		// Mark migration as complete
		markCompleteSQL := `
			INSERT INTO daz_user_tracker_history 
				(channel, username, event_type, timestamp, metadata)
			VALUES ('_system', '_system', '_utc_migration_complete', $1, $2)
		`
		metadata := fmt.Sprintf(`{"offset_hours": %d, "migrated_at": "%s"}`, offsetHours, time.Now().UTC().Format(time.RFC3339))
		err = p.sqlClient.Exec(markCompleteSQL, time.Now().UTC(), metadata)
		if err != nil {
			logger.Warn("UserTracker", "Failed to mark migration complete: %v", err)
		}

		logger.Info("UserTracker", "Successfully migrated timestamps to UTC")
	} else {
		logger.Debug("UserTracker", "No UTC migration needed")
	}

	return nil
}

// handleUserJoinFallback is the non-atomic fallback approach when stored function is not available
func (p *Plugin) handleUserJoinFallback(channel, username string, userRank int, now time.Time) {
	// First try to reactivate the most recent inactive session
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
			return
		}
	}

	// No existing session found, create a new one
	// First check if there's somehow still an active session we missed
	deactivateSQL := `
		UPDATE daz_user_tracker_sessions 
		SET is_active = FALSE
		WHERE channel = $1 AND username = $2 AND is_active = TRUE
	`
	err = p.sqlClient.Exec(deactivateSQL, channel, username)
	if err != nil {
		logger.Error("UserTracker", "Error deactivating stale sessions: %v", err)
	}

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
}
