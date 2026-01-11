package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements analytics functionality
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

	// Aggregation state
	lastHourlyRun time.Time
	lastDailyRun  time.Time
	messagesCount int64
	usersActive   map[string]bool

	// Plugin status tracking
	status    framework.PluginStatus
	startTime time.Time

	pendingMu        sync.Mutex
	pendingResponses map[string]chan *framework.PluginResponse
}

// Config holds analytics plugin configuration
type Config struct {
	// How often to run hourly aggregation (in hours)
	HourlyIntervalHours int `json:"hourly_interval_hours"`
	HourlyInterval      time.Duration
	// How often to run daily aggregation (in hours)
	DailyIntervalHours int `json:"daily_interval_hours"`
	DailyInterval      time.Duration
}

// StatsData holds aggregated statistics
type StatsData struct {
	TotalMessages   int64
	UniqueUsers     int
	TotalMediaPlays int64
	PopularMedia    []MediaStat
	ActiveHours     []HourStat
}

// MediaStat holds media play statistics
type MediaStat struct {
	Title     string
	PlayCount int64
}

// HourStat holds hourly activity statistics
type HourStat struct {
	Hour         int
	MessageCount int64
}

// New creates a new analytics plugin instance that implements framework.Plugin
func New() framework.Plugin {
	return &Plugin{
		name: "analytics",
		config: &Config{
			HourlyInterval: 1 * time.Hour,
			DailyInterval:  24 * time.Hour,
		},
		usersActive: make(map[string]bool),
		readyChan:   make(chan struct{}),
		status: framework.PluginStatus{
			Name:  "analytics",
			State: "initialized",
		},
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql", "mediatracker"} // Analytics depends on SQL and reads from mediatracker tables
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

// NewPlugin creates a new analytics plugin instance with custom configuration
// Used for testing and legacy initialization
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{
			HourlyInterval: 1 * time.Hour,
			DailyInterval:  24 * time.Hour,
		}
	}

	return &Plugin{
		name:        "analytics",
		config:      config,
		usersActive: make(map[string]bool),
		readyChan:   make(chan struct{}),
		status: framework.PluginStatus{
			Name:  "analytics",
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
		// Convert hours to duration
		if cfg.HourlyIntervalHours > 0 {
			cfg.HourlyInterval = time.Duration(cfg.HourlyIntervalHours) * time.Hour
		}
		if cfg.DailyIntervalHours > 0 {
			cfg.DailyInterval = time.Duration(cfg.DailyIntervalHours) * time.Hour
		}
		p.config = &cfg
	}

	// Ensure default config if not set
	if p.config == nil {
		p.config = &Config{
			HourlyInterval: 1 * time.Hour,
			DailyInterval:  24 * time.Hour,
		}
	}

	// Ensure we have valid intervals
	if p.config.HourlyInterval <= 0 {
		p.config.HourlyInterval = 1 * time.Hour
	}
	if p.config.DailyInterval <= 0 {
		p.config.DailyInterval = 24 * time.Hour
	}

	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.pendingResponses = make(map[string]chan *framework.PluginResponse)

	responseEvent := fmt.Sprintf("plugin.response.%s", p.name)
	if err := p.eventBus.Subscribe(responseEvent, p.handlePluginResponse); err != nil {
		return fmt.Errorf("failed to subscribe to response: %w", err)
	}

	logger.Debug("Analytics", "Initialized with hourly interval: %v, daily interval: %v",
		p.config.HourlyInterval, p.config.DailyInterval)
	return nil
}

// Initialize sets up the plugin with the event bus
// Backward compatibility, proxies to Init()
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	return p.Init(nil, eventBus)
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("analytics plugin already running")
	}

	// Defer table creation to avoid blocking during startup
	// Tables will be created lazily on first use
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		// Wait for SQL plugin to be ready using a timer
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			if err := p.createTables(); err != nil {
				logger.Error("Analytics", "Failed to create tables: %v (will retry on first use)", err)
				p.status.LastError = err
			}
		case <-p.ctx.Done():
			return
		}
	}()

	// Subscribe to chat events for real-time counting
	if err := p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to chat events: %w", err)
	}

	// Subscribe to stats requests
	if err := p.eventBus.Subscribe("plugin.analytics.stats", p.handleStatsRequest); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to stats requests: %w", err)
	}

	// Start aggregation routines
	p.wg.Add(2)
	go p.runHourlyAggregation()
	go p.runDailyAggregation()

	p.running = true
	p.status.State = "running"
	p.status.Uptime = 0
	p.startTime = time.Now()

	// Signal that the plugin is ready
	close(p.readyChan)

	logger.Debug("Analytics", "Started analytics tracking")
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
	logger.Info("Analytics", "Stopped analytics tracking")
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
	if p.running && !p.startTime.IsZero() {
		status.Uptime = time.Since(p.startTime)
	}
	return status
}

// createTables creates the necessary database tables
func (p *Plugin) createTables() error {
	// Hourly statistics table
	hourlyTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_analytics_hourly (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		hour_start TIMESTAMP NOT NULL,
		message_count BIGINT DEFAULT 0,
		unique_users INT DEFAULT 0,
		media_plays INT DEFAULT 0,
		commands_used INT DEFAULT 0,
		metadata JSONB,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel, hour_start)
	);
	
	CREATE INDEX IF NOT EXISTS idx_analytics_hourly_time 
		ON daz_analytics_hourly(channel, hour_start DESC);
	`

	// Daily statistics table
	dailyTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_analytics_daily (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		day_date DATE NOT NULL,
		total_messages BIGINT DEFAULT 0,
		unique_users INT DEFAULT 0,
		total_media_plays INT DEFAULT 0,
		peak_users INT DEFAULT 0,
		active_hours INT DEFAULT 0,
		metadata JSONB,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel, day_date)
	);
	
	CREATE INDEX IF NOT EXISTS idx_analytics_daily_date 
		ON daz_analytics_daily(channel, day_date DESC);
	`

	// User statistics table
	userStatsTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_analytics_user_stats (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		total_messages BIGINT DEFAULT 0,
		first_seen TIMESTAMP NOT NULL,
		last_seen TIMESTAMP NOT NULL,
		active_days INT DEFAULT 0,
		metadata JSONB,
		UNIQUE(channel, username)
	);
	
	CREATE INDEX IF NOT EXISTS idx_analytics_user_stats 
		ON daz_analytics_user_stats(channel, total_messages DESC);
	`

	// Execute table creation
	ctx := context.Background()
	for _, sql := range []string{hourlyTableSQL, dailyTableSQL, userStatsTableSQL} {
		if _, err := p.sqlClient.ExecSync(ctx, sql); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Add missing columns to existing tables
	alterHourlySQL := `
	ALTER TABLE daz_analytics_hourly 
	ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
	`

	alterDailySQL := `
	ALTER TABLE daz_analytics_daily 
	ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
	`

	// Execute ALTER statements to add missing columns
	for _, sql := range []string{alterHourlySQL, alterDailySQL} {
		if _, err := p.sqlClient.ExecSync(ctx, sql); err != nil {
			// Log but don't fail - column might already exist
			logger.Warn("Analytics", "Failed to alter table (column may already exist): %v", err)
		}
	}

	return nil
}

// handleChatMessage tracks real-time message counts
func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	chat := dataEvent.Data.ChatMessage

	// Get channel from event (must be present)
	channel := chat.Channel
	if channel == "" {
		logger.Warn("Analytics", "Skipping chat message without channel information")
		return nil
	}

	// Update real-time counters
	p.mu.Lock()
	p.messagesCount++
	p.usersActive[chat.Username] = true
	p.status.EventsHandled++
	p.mu.Unlock()

	// Update user stats
	userStatsSQL := `
		INSERT INTO daz_analytics_user_stats 
			(channel, username, total_messages, first_seen, last_seen)
		VALUES ($1, $2, 1, NOW(), NOW())
		ON CONFLICT (channel, username) 
		DO UPDATE SET 
			total_messages = daz_analytics_user_stats.total_messages + 1,
			last_seen = NOW()
	`
	// Use a timeout context for the SQL operation to prevent hanging
	// Analytics updates are low priority, so we can use a shorter timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.sqlClient.ExecSync(ctx, userStatsSQL, channel, chat.Username)
	if err != nil {
		// Log as warning instead of error since analytics is non-critical
		logger.Warn("Analytics", "Failed to update user stats (may retry later): %v", err)
	}

	return nil
}

// handleStatsRequest provides analytics data
func (p *Plugin) handleStatsRequest(event framework.Event) error {
	// Get channel from event context
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	channel := ""
	username := ""
	if dataEvent.Data.ChatMessage != nil {
		channel = dataEvent.Data.ChatMessage.Channel
		username = dataEvent.Data.ChatMessage.Username
	}
	if dataEvent.Data.PrivateMessage != nil {
		channel = dataEvent.Data.PrivateMessage.Channel
		if username == "" {
			username = dataEvent.Data.PrivateMessage.FromUser
		}
	}
	if dataEvent.Data.PluginRequest != nil && dataEvent.Data.PluginRequest.Data != nil && dataEvent.Data.PluginRequest.Data.Command != nil {
		params := dataEvent.Data.PluginRequest.Data.Command.Params
		if channel == "" {
			channel = params["channel"]
		}
		if username == "" {
			username = params["username"]
		}
	}
	if channel == "" {
		logger.Warn("Analytics", "Skipping stats request without channel context")
		return nil
	}

	// Get current hour stats
	currentHourQuery := `
		SELECT message_count, unique_users, media_plays
		FROM daz_analytics_hourly
		WHERE channel = $1 AND hour_start = date_trunc('hour', NOW())
	`

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	// Use the new SQL request helper with retry logic for critical analytics queries
	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, "analytics")
	rows, err := sqlHelper.NormalQuery(ctx, currentHourQuery, channel)
	if err != nil {
		return fmt.Errorf("failed to query current hour stats: %w", err)
	}
	if rows != nil {
		defer func() {
			if err := rows.Close(); err != nil {
				logger.Error("Analytics", "Failed to close rows: %v", err)
			}
		}()
	}

	var hourMessages, hourUsers, hourPlays int64
	if rows != nil && rows.Next() {
		err = rows.Scan(&hourMessages, &hourUsers, &hourPlays)
		if err != nil {
			logger.Error("Analytics", "Error scanning hour stats: %v", err)
		}
	}

	// Get today's stats
	todayQuery := `
		SELECT total_messages, unique_users, total_media_plays
		FROM daz_analytics_daily
		WHERE channel = $1 AND day_date = CURRENT_DATE
	`

	rows2, err := p.sqlClient.QuerySync(ctx, todayQuery, channel)
	if err != nil {
		return fmt.Errorf("failed to query today stats: %w", err)
	}
	if rows2 != nil {
		defer func() {
			if err := rows2.Close(); err != nil {
				logger.Error("Analytics", "Failed to close rows2: %v", err)
			}
		}()
	}

	var todayMessages, todayUsers, todayPlays int64
	if rows2 != nil && rows2.Next() {
		err = rows2.Scan(&todayMessages, &todayUsers, &todayPlays)
		if err != nil {
			logger.Error("Analytics", "Error scanning today stats: %v", err)
		}
	}

	// Get top chatters
	topChattersQuery := `
		SELECT username, total_messages
		FROM daz_analytics_user_stats
		WHERE channel = $1
		ORDER BY total_messages DESC
		LIMIT 3
	`

	rows3, err := p.sqlClient.QuerySync(ctx, topChattersQuery, channel)
	if err != nil {
		return fmt.Errorf("failed to query top chatters: %w", err)
	}
	if rows3 != nil {
		defer func() {
			if err := rows3.Close(); err != nil {
				logger.Error("Analytics", "Failed to close rows3: %v", err)
			}
		}()
	}

	var topChatters = "Top chatters: "
	position := 1
	for rows3 != nil && rows3.Next() {
		var username string
		var messages int64
		err = rows3.Scan(&username, &messages)
		if err == nil {
			if position > 1 {
				topChatters += ", "
			}
			topChatters += fmt.Sprintf("%d. %s (%d)", position, username, messages)
			position++
		}
	}

	// Format response
	response := fmt.Sprintf(
		"📊 Channel Stats:\n"+
			"This hour: %d messages, %d users, %d videos\n"+
			"Today: %d messages, %d users, %d videos\n"+
			"%s",
		hourMessages, hourUsers, hourPlays,
		todayMessages, todayUsers, todayPlays,
		topChatters)

	// Send response using proper plugin response broadcasting
	responseData := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      "stats-response",
			From:    p.name,
			Success: true,
			Data: &framework.ResponseData{
				KeyValue: map[string]string{
					"message":  response,
					"type":     "stats",
					"channel":  channel,
					"username": username,
				},
				CommandResult: &framework.CommandResultData{
					Success: true,
					Output:  response,
				},
			},
		},
	}
	p.status.EventsHandled++
	return p.eventBus.Broadcast("plugin.response", responseData)
}

// runHourlyAggregation performs hourly stats aggregation
func (p *Plugin) runHourlyAggregation() {
	defer p.wg.Done()

	// Calculate time until next hour
	now := time.Now()
	nextHour := now.Truncate(time.Hour).Add(time.Hour)
	waitTime := nextHour.Sub(now)

	// Wait until the next hour before starting
	logger.Debug("Analytics", "Waiting %v until first hourly aggregation", waitTime)

	select {
	case <-time.After(waitTime):
		// Run first aggregation at the hour mark
		p.doHourlyAggregation()
	case <-p.ctx.Done():
		return
	}

	// Then run every interval (default 1 hour)
	ticker := time.NewTicker(p.config.HourlyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.doHourlyAggregation()
		case <-p.ctx.Done():
			return
		}
	}
}

// handlePluginResponse routes responses to waiting requests
func (p *Plugin) handlePluginResponse(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginResponse == nil {
		return nil
	}

	resp := dataEvent.Data.PluginResponse

	p.pendingMu.Lock()
	responseChan, exists := p.pendingResponses[resp.ID]
	if exists {
		delete(p.pendingResponses, resp.ID)
	}
	p.pendingMu.Unlock()

	if exists {
		select {
		case responseChan <- resp:
		default:
		}
	}

	return nil
}

// getConfiguredChannels returns the currently connected channels from the core plugin
func (p *Plugin) getConfiguredChannels() ([]string, error) {
	// Create a unique request ID
	requestID := fmt.Sprintf("analytics-channels-%d", time.Now().UnixNano())

	// Create a channel to receive the response
	responseChan := make(chan *framework.PluginResponse, 1)
	errorChan := make(chan error, 1)

	p.pendingMu.Lock()
	p.pendingResponses[requestID] = responseChan
	p.pendingMu.Unlock()

	defer func() {
		p.pendingMu.Lock()
		delete(p.pendingResponses, requestID)
		p.pendingMu.Unlock()
	}()

	// Send the request in a goroutine to avoid blocking
	go func() {
		request := &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   requestID,
				To:   "core",
				From: p.name,
				Type: "get_configured_channels",
			},
		}

		if err := p.eventBus.Broadcast("plugin.request", request); err != nil {
			errorChan <- fmt.Errorf("failed to send request: %w", err)
		}
	}()

	// Wait for response with a shorter timeout
	select {
	case resp := <-responseChan:
		if !resp.Success {
			return nil, fmt.Errorf("core plugin returned error: %s", resp.Error)
		}

		// Parse response
		var responseData struct {
			Channels []struct {
				ID        string `json:"id"`
				Channel   string `json:"channel"`
				Enabled   bool   `json:"enabled"`
				Connected bool   `json:"connected"`
			} `json:"channels"`
		}

		if err := json.Unmarshal(resp.Data.RawJSON, &responseData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Extract enabled AND connected channel names
		var channels []string
		for _, ch := range responseData.Channels {
			if ch.Enabled && ch.Connected {
				channels = append(channels, ch.Channel)
			}
		}

		return channels, nil

	case err := <-errorChan:
		return nil, err

	case <-time.After(1 * time.Second):
		// If request times out, fall back to hardcoded list
		logger.Debug("Analytics", "Timeout getting channels from core, using fallback")
		return []string{
			"RIFFTRAX_MST3K",
			"fatpizza",
			"always_always_sunny",
		}, nil
	}
}

// doHourlyAggregation performs the actual hourly aggregation
func (p *Plugin) doHourlyAggregation() {
	// Get all channels that have been seen in the system
	channels, err := p.getConfiguredChannels()
	if err != nil {
		logger.Error("Analytics", "Failed to get channels: %v", err)
		return
	}

	if len(channels) == 0 {
		logger.Info("Analytics", "No active channels found for aggregation")
		p.lastHourlyRun = time.Now()
		return
	}

	logger.Info("Analytics", "Starting hourly aggregation for %d channels", len(channels))

	// Get the hour to aggregate (previous hour)
	now := time.Now()
	hourStart := now.Truncate(time.Hour).Add(-time.Hour)
	hourEnd := hourStart.Add(time.Hour)

	// Aggregate data for each channel
	successCount := 0
	for _, channel := range channels {
		if err := p.aggregateHourlyForChannel(channel, hourStart, hourEnd); err != nil {
			logger.Error("Analytics", "Failed to aggregate hourly data for channel %s: %v",
				channel, err)
			continue
		}
		successCount++
	}

	logger.Info("Analytics", "Hourly aggregation completed: %d/%d successful",
		successCount, len(channels))
	p.lastHourlyRun = time.Now()
}

// aggregateHourlyForChannel aggregates hourly statistics for a specific channel
func (p *Plugin) aggregateHourlyForChannel(channel string, hourStart, hourEnd time.Time) error {
	ctx := context.Background()

	// Count messages for this hour
	messageCountQuery := `
		SELECT COUNT(*) 
		FROM daz_core_events 
		WHERE channel_name = $1 
		AND timestamp >= $2 
		AND timestamp < $3
		AND event_type = 'chat.message'`

	var messageCount int64
	rows, err := p.sqlClient.QuerySync(ctx, messageCountQuery, channel, hourStart, hourEnd)
	if err != nil {
		return fmt.Errorf("failed to query message count: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Analytics", "Failed to close rows: %v", err)
		}
	}()

	if rows.Next() {
		if err := rows.Scan(&messageCount); err != nil {
			return fmt.Errorf("failed to scan message count: %w", err)
		}
	}

	// Count unique users for this hour
	uniqueUsersQuery := `
		SELECT COUNT(DISTINCT username) 
		FROM daz_core_events 
		WHERE channel_name = $1 
		AND timestamp >= $2 
		AND timestamp < $3
		AND event_type = 'chat.message'
		AND username IS NOT NULL`

	var uniqueUsers int
	rows2, err := p.sqlClient.QuerySync(ctx, uniqueUsersQuery, channel, hourStart, hourEnd)
	if err != nil {
		return fmt.Errorf("failed to query unique users: %w", err)
	}
	defer func() {
		if err := rows2.Close(); err != nil {
			logger.Error("Analytics", "Failed to close rows: %v", err)
		}
	}()

	if rows2.Next() {
		if err := rows2.Scan(&uniqueUsers); err != nil {
			return fmt.Errorf("failed to scan unique users: %w", err)
		}
	}

	// Count media plays for this hour
	mediaPlaysQuery := `
		SELECT COUNT(*) 
		FROM daz_mediatracker_plays 
		WHERE channel = $1 
		AND started_at >= $2 
		AND started_at < $3`

	var mediaPlays int
	rows3, err := p.sqlClient.QuerySync(ctx, mediaPlaysQuery, channel, hourStart, hourEnd)
	if err != nil {
		// Media tracker might not be enabled
		logger.Debug("Analytics", "Failed to query media plays, assuming 0: %v", err)
		mediaPlays = 0
	} else {
		defer func() {
			if err := rows3.Close(); err != nil {
				logger.Debug("Analytics", "Failed to close rows: %v", err)
			}
		}()

		if rows3.Next() {
			if err := rows3.Scan(&mediaPlays); err != nil {
				logger.Debug("Analytics", "Failed to scan media plays, assuming 0: %v", err)
				mediaPlays = 0
			}
		}
	}

	// Insert or update hourly aggregation
	insertQuery := `
		INSERT INTO daz_analytics_hourly 
		(channel, hour_start, message_count, unique_users, media_plays, commands_used, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (channel, hour_start) 
		DO UPDATE SET 
			message_count = EXCLUDED.message_count,
			unique_users = EXCLUDED.unique_users,
			media_plays = EXCLUDED.media_plays,
			commands_used = EXCLUDED.commands_used,
			metadata = EXCLUDED.metadata,
			updated_at = CURRENT_TIMESTAMP`

	metadata := map[string]interface{}{
		"aggregated_at": time.Now().UTC(),
	}
	metadataJSON, _ := json.Marshal(metadata)

	_, err = p.sqlClient.ExecSync(ctx, insertQuery,
		channel, hourStart, messageCount, uniqueUsers, mediaPlays, 0, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert hourly aggregation: %w", err)
	}

	logger.Debug("Analytics", "Hourly aggregation completed for channel %s at %s: %d messages, %d users, %d media plays",
		channel, hourStart.Format("2006-01-02 15:04"),
		messageCount, uniqueUsers, mediaPlays)

	return nil
}

// runDailyAggregation performs daily stats aggregation
func (p *Plugin) runDailyAggregation() {
	defer p.wg.Done()

	// Calculate time until next midnight
	now := time.Now()
	nextMidnight := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
	waitTime := nextMidnight.Sub(now)

	// Wait until midnight before starting
	logger.Debug("Analytics", "Waiting %v until first daily aggregation", waitTime)

	select {
	case <-time.After(waitTime):
		// Run first aggregation at midnight
		p.doDailyAggregation()
	case <-p.ctx.Done():
		return
	}

	// Then run every interval (default 24 hours)
	ticker := time.NewTicker(p.config.DailyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.doDailyAggregation()
		case <-p.ctx.Done():
			return
		}
	}
}

// doDailyAggregation performs the actual daily aggregation
func (p *Plugin) doDailyAggregation() {
	// Get all channels that have been seen in the system
	channels, err := p.getConfiguredChannels()
	if err != nil {
		logger.Error("Analytics", "Failed to get channels for daily aggregation: %v", err)
		return
	}

	if len(channels) == 0 {
		logger.Info("Analytics", "No active channels found for daily aggregation")
		p.lastDailyRun = time.Now()
		return
	}

	logger.Info("Analytics", "Starting daily aggregation for %d channels", len(channels))

	// Get the day to aggregate (yesterday)
	now := time.Now()
	dayStart := now.Truncate(24 * time.Hour).Add(-24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	// Aggregate data for each channel
	successCount := 0
	for _, channel := range channels {
		if err := p.aggregateDailyForChannel(channel, dayStart, dayEnd); err != nil {
			logger.Error("Analytics", "Failed to aggregate daily data for channel %s: %v",
				channel, err)
			continue
		}
		successCount++
	}

	logger.Info("Analytics", "Daily aggregation completed: %d/%d successful",
		successCount, len(channels))
	p.lastDailyRun = time.Now()
}

// aggregateDailyForChannel aggregates daily statistics for a specific channel
func (p *Plugin) aggregateDailyForChannel(channel string, dayStart, dayEnd time.Time) error {
	ctx := context.Background()

	// Aggregate from hourly data
	dailyStatsQuery := `
		SELECT 
			COALESCE(SUM(message_count), 0) as total_messages,
			COALESCE(SUM(media_plays), 0) as total_media_plays,
			COUNT(DISTINCT hour_start) as active_hours,
			COALESCE(MAX(unique_users), 0) as peak_users
		FROM daz_analytics_hourly
		WHERE channel = $1
		AND hour_start >= $2
		AND hour_start < $3`

	var totalMessages, totalMediaPlays int64
	var activeHours, peakUsers int

	rows, err := p.sqlClient.QuerySync(ctx, dailyStatsQuery, channel, dayStart, dayEnd)
	if err != nil {
		return fmt.Errorf("failed to query daily stats: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Analytics", "Failed to close rows: %v", err)
		}
	}()

	if rows.Next() {
		if err := rows.Scan(&totalMessages, &totalMediaPlays, &activeHours, &peakUsers); err != nil {
			return fmt.Errorf("failed to scan daily stats: %w", err)
		}
	}

	// Get unique users for the entire day
	uniqueUsersQuery := `
		SELECT COUNT(DISTINCT username)
		FROM daz_core_events
		WHERE channel_name = $1
		AND timestamp >= $2
		AND timestamp < $3
		AND event_type = 'chat.message'
		AND username IS NOT NULL`

	var uniqueUsers int
	rows2, err := p.sqlClient.QuerySync(ctx, uniqueUsersQuery, channel, dayStart, dayEnd)
	if err != nil {
		return fmt.Errorf("failed to query unique users: %w", err)
	}
	defer func() {
		if err := rows2.Close(); err != nil {
			logger.Error("Analytics", "Failed to close rows: %v", err)
		}
	}()

	if rows2.Next() {
		if err := rows2.Scan(&uniqueUsers); err != nil {
			return fmt.Errorf("failed to scan unique users: %w", err)
		}
	}

	// Insert or update daily aggregation
	insertQuery := `
		INSERT INTO daz_analytics_daily
		(channel, day_date, total_messages, unique_users, total_media_plays, 
		 peak_users, active_hours, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (channel, day_date)
		DO UPDATE SET
			total_messages = EXCLUDED.total_messages,
			unique_users = EXCLUDED.unique_users,
			total_media_plays = EXCLUDED.total_media_plays,
			peak_users = EXCLUDED.peak_users,
			active_hours = EXCLUDED.active_hours,
			metadata = EXCLUDED.metadata,
			updated_at = CURRENT_TIMESTAMP`

	metadata := map[string]interface{}{
		"aggregated_at": time.Now().UTC(),
	}
	metadataJSON, _ := json.Marshal(metadata)

	_, err = p.sqlClient.ExecSync(ctx, insertQuery,
		channel, dayStart.Format("2006-01-02"), totalMessages, uniqueUsers,
		totalMediaPlays, peakUsers, activeHours, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to insert daily aggregation: %w", err)
	}

	logger.Debug("Analytics", "Daily aggregation completed for channel %s on %s: %d messages, %d users, %d media plays",
		channel, dayStart.Format("2006-01-02"),
		totalMessages, uniqueUsers, totalMediaPlays)

	return nil
}
