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
	status framework.PluginStatus
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

// NewPlugin creates a new analytics plugin instance
// Deprecated: Use New() instead
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

	logger.Info("Analytics", "Initialized with hourly interval: %v, daily interval: %v",
		p.config.HourlyInterval, p.config.DailyInterval)
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
	p.status.Uptime = time.Since(time.Now())

	// Signal that the plugin is ready
	close(p.readyChan)

	logger.Info("Analytics", "Started analytics tracking")
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
	if p.running {
		status.Uptime = time.Since(time.Now().Add(-status.Uptime))
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
	ctx := context.Background()
	_, err := p.sqlClient.ExecSync(ctx, userStatsSQL, channel, chat.Username)
	if err != nil {
		logger.Error("Analytics", "Error updating user stats: %v", err)
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
	if dataEvent.Data.ChatMessage != nil {
		channel = dataEvent.Data.ChatMessage.Channel
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
					"message": response,
					"type":    "stats",
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

	// Run immediately on startup
	p.doHourlyAggregation()

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

// doHourlyAggregation performs the actual hourly aggregation
func (p *Plugin) doHourlyAggregation() {
	// Skip aggregation in multi-room mode for now
	logger.Info("Analytics", "Skipping hourly aggregation in multi-room mode")
	p.lastHourlyRun = time.Now()
}

// runDailyAggregation performs daily stats aggregation
func (p *Plugin) runDailyAggregation() {
	defer p.wg.Done()

	// Wait before first run
	select {
	case <-time.After(5 * time.Minute):
	case <-p.ctx.Done():
		return
	}

	// Run immediately
	p.doDailyAggregation()

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
	// Skip aggregation in multi-room mode for now
	logger.Info("Analytics", "Skipping daily aggregation in multi-room mode")
	p.lastDailyRun = time.Now()
}
