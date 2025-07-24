package mediatracker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements media tracking functionality
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

	// Current media state
	currentMedia *MediaState

	// Plugin status tracking
	status framework.PluginStatus

	// Deduplication tracking per channel
	lastPlaylistInfo map[string]*playlistInfo
}

type playlistInfo struct {
	time  time.Time
	count int
}

// Config holds mediatracker plugin configuration
type Config struct {
	// How often to update aggregated statistics
	StatsUpdateIntervalMinutes int `json:"stats_update_interval_minutes"`
	StatsUpdateInterval        time.Duration
}

// MediaState tracks current media playing
type MediaState struct {
	ID          string
	Type        string
	Title       string
	Duration    int
	StartedAt   time.Time
	Position    int
	QueuedBy    string
	PlayCount   int64
	TotalPlayed time.Duration
}

// New creates a new mediatracker plugin instance that implements framework.Plugin
func New() framework.Plugin {
	return &Plugin{
		name: "mediatracker",
		config: &Config{
			StatsUpdateInterval: 5 * time.Minute,
		},
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  "mediatracker",
			State: "initialized",
		},
		lastPlaylistInfo: make(map[string]*playlistInfo),
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql"} // MediaTracker depends on SQL plugin for database operations
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

// NewPlugin creates a new mediatracker plugin instance
// Deprecated: Use New() instead
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{
			StatsUpdateInterval: 5 * time.Minute,
		}
	}

	return &Plugin{
		name:      "mediatracker",
		config:    config,
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  "mediatracker",
			State: "initialized",
		},
		lastPlaylistInfo: make(map[string]*playlistInfo),
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
		if cfg.StatsUpdateIntervalMinutes > 0 {
			cfg.StatsUpdateInterval = time.Duration(cfg.StatsUpdateIntervalMinutes) * time.Minute
		}
		p.config = &cfg
	}

	// Ensure default config if not set
	if p.config == nil {
		p.config = &Config{
			StatsUpdateInterval: 5 * time.Minute,
		}
	}

	// Ensure we have a valid interval
	if p.config.StatsUpdateInterval <= 0 {
		p.config.StatsUpdateInterval = 5 * time.Minute
	}

	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	logger.Debug("MediaTracker", "Initialized with stats update interval: %v", p.config.StatsUpdateInterval)
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
		return fmt.Errorf("mediatracker plugin already running")
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
				logger.Warn("MediaTracker", "Failed to create tables: %v (will retry on first use)", err)
				p.status.LastError = err
			}
		case <-p.ctx.Done():
			return
		}
	}()

	// Subscribe to media change events
	if err := p.eventBus.Subscribe(eventbus.EventCytubeVideoChange, p.handleMediaChange); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to media change events: %w", err)
	}

	// Subscribe to queue events
	if err := p.eventBus.Subscribe("cytube.event.queue", p.handleQueueUpdate); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to queue events: %w", err)
	}

	// Subscribe to media update events (for position tracking)
	if err := p.eventBus.Subscribe(eventbus.EventCytubeMediaUpdate, p.handleMediaUpdate); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to media update events: %w", err)
	}

	// Subscribe to command execution events
	if err := p.eventBus.Subscribe("command.mediatracker.execute", p.handleCommand); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to command execution events: %w", err)
	}

	// Subscribe to playlist events to populate library from initial playlist load
	if err := p.eventBus.Subscribe("cytube.event.playlist", p.handlePlaylistEvent); err != nil {
		p.status.LastError = err
		return fmt.Errorf("failed to subscribe to playlist events: %w", err)
	}

	// Start periodic stats updater
	p.wg.Add(1)
	go p.updateStats()

	// Load current media state from database in background
	// Don't block startup if the query hangs
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.loadCurrentState(); err != nil {
			logger.Error("MediaTracker", "Error loading current state: %v", err)
		}
	}()

	p.running = true
	p.status.State = "running"
	p.status.Uptime = time.Since(time.Now())

	// Register commands with the eventfilter plugin
	p.registerCommands()

	// Signal that the plugin is ready
	close(p.readyChan)

	logger.Debug("MediaTracker", "Started media tracking")
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
	logger.Info("MediaTracker", "Stopped media tracking")
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
	// Media plays table
	playsTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_mediatracker_plays (
		id BIGSERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		media_id VARCHAR(255) NOT NULL,
		media_type VARCHAR(50) NOT NULL,
		title TEXT NOT NULL,
		duration INT NOT NULL,
		started_at TIMESTAMP NOT NULL,
		ended_at TIMESTAMP,
		queued_by VARCHAR(255),
		completed BOOLEAN DEFAULT FALSE,
		metadata JSONB
	);

	CREATE INDEX IF NOT EXISTS idx_mediatracker_plays_time ON daz_mediatracker_plays(channel, started_at);
	CREATE INDEX IF NOT EXISTS idx_mediatracker_plays_media ON daz_mediatracker_plays(channel, media_id);
	`

	// Current queue table
	queueTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_mediatracker_queue (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		position INT NOT NULL,
		media_id VARCHAR(255) NOT NULL,
		media_type VARCHAR(50) NOT NULL,
		title TEXT NOT NULL,
		duration INT NOT NULL,
		queued_by VARCHAR(255),
		queued_at TIMESTAMP NOT NULL,
		UNIQUE(channel, position)
	);
	`

	// Aggregated statistics table
	statsTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_mediatracker_stats (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		media_id VARCHAR(255) NOT NULL,
		media_type VARCHAR(50) NOT NULL,
		title TEXT NOT NULL,
		play_count BIGINT DEFAULT 0,
		total_duration BIGINT DEFAULT 0,
		last_played TIMESTAMP,
		first_played TIMESTAMP,
		UNIQUE(channel, media_id)
	);
	
	CREATE INDEX IF NOT EXISTS idx_mediatracker_stats_popular 
		ON daz_mediatracker_stats(channel, play_count DESC);
	`

	// Media library table - tracks all unique media by URL
	libraryTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_mediatracker_library (
		id BIGSERIAL PRIMARY KEY,
		url TEXT UNIQUE NOT NULL,
		media_id VARCHAR(255) NOT NULL,
		media_type VARCHAR(50) NOT NULL,
		title TEXT NOT NULL,
		duration INTEGER,
		first_seen TIMESTAMP NOT NULL,
		last_played TIMESTAMP,
		play_count INTEGER DEFAULT 0,
		added_by VARCHAR(100),
		channel VARCHAR(100) NOT NULL,
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(media_id, media_type)
	);
	
	CREATE INDEX IF NOT EXISTS idx_library_url ON daz_mediatracker_library(url);
	CREATE INDEX IF NOT EXISTS idx_library_media ON daz_mediatracker_library(media_id, media_type);
	CREATE INDEX IF NOT EXISTS idx_library_channel ON daz_mediatracker_library(channel);
	`

	// Execute table creation
	for _, sql := range []string{playsTableSQL, queueTableSQL, statsTableSQL, libraryTableSQL} {
		if err := p.sqlClient.Exec(sql); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

// handleMediaUpdate processes media update events (position/pause state)
func (p *Plugin) handleMediaUpdate(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.MediaUpdate == nil {
		return nil
	}

	update := dataEvent.Data.MediaUpdate

	// Temporary logging to verify events are being received
	logger.Debug("MediaTracker", "Media update received - currentTime: %.2f, paused: %v",
		update.CurrentTime, update.Paused)

	p.status.EventsHandled++
	return nil
}

// handleMediaChange processes media change events
func (p *Plugin) handleMediaChange(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.VideoChange == nil {
		return nil
	}

	media := dataEvent.Data.VideoChange
	now := time.Now()

	// Get channel from event (must be present)
	channel := media.Channel
	if channel == "" {
		logger.Warn("MediaTracker", "Skipping video change event without channel information")
		return nil
	}

	// End the previous media play if any
	if p.currentMedia != nil {
		if err := p.endMediaPlay(p.currentMedia, now, channel); err != nil {
			logger.Error("MediaTracker", "Error ending previous media: %v", err)
		}
	}

	// Start tracking new media
	p.mu.Lock()
	p.currentMedia = &MediaState{
		ID:        media.VideoID,
		Type:      media.VideoType,
		Title:     media.Title,
		Duration:  media.Duration,
		StartedAt: now,
	}
	p.mu.Unlock()

	// Add to library (will update play count if already exists)
	if err := p.addToLibrary(
		media.VideoID,
		media.VideoType,
		media.Title,
		media.Duration,
		"", // addedBy unknown for now
		channel,
		nil, // metadata
	); err != nil {
		logger.Error("MediaTracker", "Error adding to library: %v", err)
	}

	// Record new media play
	playSQL := `
		INSERT INTO daz_mediatracker_plays 
			(channel, media_id, media_type, title, duration, started_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	err := p.sqlClient.Exec(playSQL,
		channel,
		media.VideoID,
		media.VideoType,
		media.Title,
		media.Duration,
		now)
	if err != nil {
		logger.Error("MediaTracker", "Error recording media play: %v", err)
	}

	// Update stats
	statsSQL := `
		INSERT INTO daz_mediatracker_stats 
			(channel, media_id, media_type, title, play_count, total_duration, first_played, last_played)
		VALUES ($1, $2, $3, $4, 1, 0, $5, $5)
		ON CONFLICT (channel, media_id) 
		DO UPDATE SET 
			play_count = daz_mediatracker_stats.play_count + 1,
			last_played = EXCLUDED.last_played,
			title = EXCLUDED.title
	`
	err = p.sqlClient.Exec(statsSQL,
		channel,
		media.VideoID,
		media.VideoType,
		media.Title,
		now)
	if err != nil {
		logger.Error("MediaTracker", "Error updating stats: %v", err)
	}

	logger.Info("MediaTracker", "Now playing: %s (%s, %ds)", media.Title, media.VideoType, media.Duration)
	p.status.EventsHandled++
	return nil
}

// handleQueueUpdate processes queue update events
func (p *Plugin) handleQueueUpdate(event framework.Event) error {
	// Check if this is a DataEvent with queue data
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.QueueUpdate == nil {
		// Try to handle as a QueueEvent directly
		queueEvent, ok := event.(*framework.QueueEvent)
		if !ok {
			logger.Warn("MediaTracker", "Received queue event but couldn't parse it")
			return nil
		}
		// Convert QueueEvent to QueueUpdateData
		return p.processQueueUpdate(&framework.QueueUpdateData{
			Channel:     queueEvent.ChannelName,
			Action:      queueEvent.Action,
			Items:       queueEvent.Items,
			Position:    queueEvent.Position,
			NewPosition: queueEvent.NewPosition,
		})
	}

	return p.processQueueUpdate(dataEvent.Data.QueueUpdate)
}

// processQueueUpdate handles the actual queue update logic
func (p *Plugin) processQueueUpdate(queueData *framework.QueueUpdateData) error {
	p.status.EventsHandled++

	// Get channel from event (must be present)
	channel := queueData.Channel
	if channel == "" {
		logger.Warn("MediaTracker", "Skipping queue update without channel information")
		return nil
	}

	logger.Debug("MediaTracker", "Processing queue %s for channel %s", queueData.Action, channel)

	switch queueData.Action {
	case "clear":
		// Clear the entire queue
		return p.sqlClient.Exec(
			"DELETE FROM daz_mediatracker_queue WHERE channel = $1",
			channel)

	case "full":
		// Replace entire queue with new items
		logger.Info("MediaTracker", "Starting to process full playlist with %d items (queue event)", len(queueData.Items))
		startTime := time.Now()

		// First clear existing queue
		if err := p.sqlClient.Exec(
			"DELETE FROM daz_mediatracker_queue WHERE channel = $1",
			channel); err != nil {
			return fmt.Errorf("failed to clear queue: %w", err)
		}

		// Bulk insert all new items
		if len(queueData.Items) > 0 {
			if err := p.bulkInsertQueueItems(channel, queueData.Items); err != nil {
				return fmt.Errorf("failed to bulk insert queue items: %w", err)
			}
		}

		logger.Info("MediaTracker", "Finished processing %d playlist items in %v", len(queueData.Items), time.Since(startTime))

	case "add":
		// Add item at position
		if len(queueData.Items) > 0 {
			// Shift existing items down if necessary
			if err := p.sqlClient.Exec(
				"UPDATE daz_mediatracker_queue SET position = position + 1 WHERE channel = $1 AND position >= $2",
				channel,
				queueData.Position); err != nil {
				return fmt.Errorf("failed to shift queue items: %w", err)
			}

			// Insert new item
			item := queueData.Items[0]
			item.Position = queueData.Position
			if err := p.insertQueueItem(channel, &item); err != nil {
				return fmt.Errorf("failed to add queue item: %w", err)
			}
		}

	case "remove":
		// Remove item at position
		if err := p.sqlClient.Exec(
			"DELETE FROM daz_mediatracker_queue WHERE channel = $1 AND position = $2",
			channel,
			queueData.Position); err != nil {
			return fmt.Errorf("failed to remove queue item: %w", err)
		}

		// Shift remaining items up
		if err := p.sqlClient.Exec(
			"UPDATE daz_mediatracker_queue SET position = position - 1 WHERE channel = $1 AND position > $2",
			channel,
			queueData.Position); err != nil {
			return fmt.Errorf("failed to shift queue items: %w", err)
		}

	case "move":
		// Move item from position to newPosition
		// This is complex - need to handle in a transaction-like manner
		// For now, log it as unimplemented
		logger.Warn("MediaTracker", "Queue move operation not fully implemented yet")

	default:
		logger.Warn("MediaTracker", "Unknown queue action: %s", queueData.Action)
	}

	return nil
}

// insertQueueItem inserts a single queue item into the database
func (p *Plugin) insertQueueItem(channel string, item *framework.QueueItem) error {
	queuedAt := time.Unix(item.QueuedAt, 0)

	// Add to library when items are queued
	if err := p.addToLibrary(
		item.MediaID,
		item.MediaType,
		item.Title,
		item.Duration,
		item.QueuedBy,
		channel,
		nil, // metadata
	); err != nil {
		logger.Error("MediaTracker", "Error adding queued item to library: %v", err)
	}

	return p.sqlClient.Exec(`
		INSERT INTO daz_mediatracker_queue 
		(channel, position, media_id, media_type, title, duration, queued_by, queued_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (channel, position) DO UPDATE SET
			media_id = EXCLUDED.media_id,
			media_type = EXCLUDED.media_type,
			title = EXCLUDED.title,
			duration = EXCLUDED.duration,
			queued_by = EXCLUDED.queued_by,
			queued_at = EXCLUDED.queued_at
	`, channel,
		item.Position,
		item.MediaID,
		item.MediaType,
		item.Title,
		item.Duration,
		item.QueuedBy,
		queuedAt)
}

// bulkInsertQueueItems performs a bulk insert of queue items to minimize database operations
func (p *Plugin) bulkInsertQueueItems(channel string, items []framework.QueueItem) error {
	if len(items) == 0 {
		return nil
	}

	// Build the SQL for bulk insert
	var sqlStr strings.Builder
	sqlStr.WriteString(`
		INSERT INTO daz_mediatracker_queue 
		(channel, position, media_id, media_type, title, duration, queued_by, queued_at)
		VALUES `)

	// Build value placeholders and collect all parameters
	var params []interface{}
	for i, item := range items {
		if i > 0 {
			sqlStr.WriteString(", ")
		}
		base := i * 8
		sqlStr.WriteString(fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8))

		queuedAt := time.Unix(item.QueuedAt, 0)
		params = append(params,
			channel,
			item.Position,
			item.MediaID,
			item.MediaType,
			item.Title,
			item.Duration,
			item.QueuedBy,
			queuedAt)
	}

	// Execute the bulk insert
	if err := p.sqlClient.Exec(sqlStr.String(), params...); err != nil {
		return fmt.Errorf("bulk insert failed: %w", err)
	}

	// Bulk add to library (single operation for all items)
	if err := p.bulkAddToLibrary(channel, items); err != nil {
		logger.Error("MediaTracker", "Error bulk adding to library: %v", err)
	}

	logger.Debug("MediaTracker", "Bulk inserted %d queue items", len(items))
	return nil
}

// bulkAddToLibrary adds multiple items to the library in a single operation
func (p *Plugin) bulkAddToLibrary(channel string, items []framework.QueueItem) error {
	if len(items) == 0 {
		return nil
	}

	var sqlStr strings.Builder
	sqlStr.WriteString(`
		INSERT INTO daz_mediatracker_library 
		(channel, media_id, media_type, title, duration, added_by, added_at, play_count)
		VALUES `)

	var params []interface{}
	now := time.Now()

	for i, item := range items {
		if i > 0 {
			sqlStr.WriteString(", ")
		}
		base := i * 8
		sqlStr.WriteString(fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, 0)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7))

		params = append(params,
			channel,
			item.MediaID,
			item.MediaType,
			item.Title,
			item.Duration,
			item.QueuedBy,
			now)
	}

	sqlStr.WriteString(` ON CONFLICT (channel, media_id) DO UPDATE SET
		title = EXCLUDED.title,
		duration = EXCLUDED.duration,
		last_queued = EXCLUDED.added_at`)

	return p.sqlClient.Exec(sqlStr.String(), params...)
}

// registerCommands registers commands with the eventfilter plugin
func (p *Plugin) registerCommands() {
	// Register nowplaying and stats commands
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "nowplaying,np,stats,mediastats",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error("MediaTracker", "Failed to register mediatracker commands: %v", err)
	}
}

// handleCommand handles plugin command requests
func (p *Plugin) handleCommand(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	// Handle DataEvent
	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	// Handle asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.handleMediaTrackerCommand(dataEvent.Data.PluginRequest)
	}()

	return nil
}

// handleMediaTrackerCommand processes the actual command execution
func (p *Plugin) handleMediaTrackerCommand(req *framework.PluginRequest) {
	if req.Data == nil || req.Data.Command == nil {
		return
	}

	cmdName := req.Data.Command.Name
	switch cmdName {
	case "nowplaying", "np":
		p.handleNowPlayingCommand(req)
	case "stats", "mediastats":
		p.handleStatsCommand(req)
	default:
		logger.Warn("MediaTracker", "Unknown command: %s", cmdName)
	}
}

// handleNowPlayingCommand handles nowplaying command requests
func (p *Plugin) handleNowPlayingCommand(req *framework.PluginRequest) {
	p.mu.RLock()
	current := p.currentMedia
	p.mu.RUnlock()

	var response string
	if current == nil {
		response = "Nothing is currently playing"
	} else {
		elapsed := time.Since(current.StartedAt)
		remaining := time.Duration(current.Duration)*time.Second - elapsed

		if remaining < 0 {
			remaining = 0
		}

		response = fmt.Sprintf("Now playing: %s [%s] (%s elapsed, %s remaining)",
			current.Title,
			current.Type,
			formatDuration(elapsed),
			formatDuration(remaining))
	}

	p.sendResponse(req, response)
	p.status.EventsHandled++
}

// handleStatsCommand handles stats command requests
func (p *Plugin) handleStatsCommand(req *framework.PluginRequest) {
	// Get channel from request context
	channel := req.Data.Command.Params["channel"]
	if channel == "" {
		logger.Warn("MediaTracker", "Skipping stats request without channel context")
		return
	}

	// Query most played media
	query := `
		SELECT title, media_type, play_count, last_played
		FROM daz_mediatracker_stats
		WHERE channel = $1
		ORDER BY play_count DESC
		LIMIT 5
	`

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	// Use the new SQL request helper with retry logic for media statistics queries
	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, "mediatracker")
	rows, err := sqlHelper.NormalQuery(ctx, query, channel)
	if err != nil {
		logger.Error("MediaTracker", "Failed to query stats: %v", err)
		p.sendResponse(req, "Failed to retrieve media statistics")
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("MediaTracker", "Failed to close rows: %v", err)
		}
	}()

	var response string
	response = "Top 5 most played videos:\n"

	position := 1
	for rows.Next() {
		var title, mediaType string
		var playCount int64
		var lastPlayed time.Time

		err := rows.Scan(&title, &mediaType, &playCount, &lastPlayed)
		if err != nil {
			continue
		}

		response += fmt.Sprintf("%d. %s [%s] - %d plays\n",
			position, title, mediaType, playCount)
		position++
	}

	if position == 1 {
		response = "No media statistics available yet"
	}

	p.sendResponse(req, response)
	p.status.EventsHandled++
}

// sendResponse sends a response as a private message to the requesting user
func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	username := req.Data.Command.Params["username"]
	channel := req.Data.Command.Params["channel"]

	response := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  username,
			Message: message,
			Channel: channel,
		},
	}

	// Broadcast to cytube.send.pm event
	if err := p.eventBus.Broadcast("cytube.send.pm", response); err != nil {
		logger.Error("MediaTracker", "Failed to send MediaTracker PM response: %v", err)
	}
}

// endMediaPlay marks a media play as ended
func (p *Plugin) endMediaPlay(media *MediaState, endTime time.Time, channel string) error {
	duration := endTime.Sub(media.StartedAt)
	completed := duration >= time.Duration(media.Duration)*time.Second*9/10 // 90% watched

	updateSQL := `
		UPDATE daz_mediatracker_plays 
		SET ended_at = $1, completed = $2
		WHERE channel = $3 AND media_id = $4 AND started_at = $5
	`
	err := p.sqlClient.Exec(updateSQL,
		endTime,
		completed,
		channel,
		media.ID,
		media.StartedAt)
	if err != nil {
		return fmt.Errorf("failed to end media play: %w", err)
	}

	// Update total duration in stats with actual watched duration for all videos
	statsSQL := `
		UPDATE daz_mediatracker_stats 
		SET total_duration = total_duration + $1
		WHERE channel = $2 AND media_id = $3
	`
	err = p.sqlClient.Exec(statsSQL,
		int(duration.Seconds()),
		channel,
		media.ID)
	if err != nil {
		return fmt.Errorf("failed to update total duration: %w", err)
	}

	return nil
}

// loadCurrentState loads the current media state from database
func (p *Plugin) loadCurrentState() error {
	// Since this is called at startup, we can't determine channel from events
	// Skip loading current state for now in multi-room setup
	logger.Debug("MediaTracker", "Skipping current state load in multi-room mode")
	return nil
}

// updateStats periodically updates aggregated statistics
func (p *Plugin) updateStats() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.StatsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.doStatsUpdate()
		case <-p.ctx.Done():
			return
		}
	}
}

// doStatsUpdate performs the actual stats update
func (p *Plugin) doStatsUpdate() {
	// This is a placeholder for more complex aggregation logic
	// For now, the stats are updated in real-time during media changes
	logger.Debug("MediaTracker", "Running periodic stats update")
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "0:00"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// constructMediaURL builds a URL from media ID and type
func constructMediaURL(mediaID, mediaType string) string {
	switch mediaType {
	case "yt", "youtube":
		return fmt.Sprintf("https://www.youtube.com/watch?v=%s", mediaID)
	case "gd", "googledrive":
		return fmt.Sprintf("https://drive.google.com/file/d/%s/view", mediaID)
	case "vm", "vimeo":
		return fmt.Sprintf("https://vimeo.com/%s", mediaID)
	case "dm", "dailymotion":
		return fmt.Sprintf("https://www.dailymotion.com/video/%s", mediaID)
	case "sc", "soundcloud":
		// SoundCloud URLs are more complex, this is a simplified version
		return fmt.Sprintf("https://soundcloud.com/tracks/%s", mediaID)
	case "li", "livestream":
		// Livestream/Twitch
		return fmt.Sprintf("https://www.twitch.tv/videos/%s", mediaID)
	case "tw", "twitch":
		return fmt.Sprintf("https://www.twitch.tv/videos/%s", mediaID)
	case "cu", "custom":
		// Custom/direct URLs might already be the full URL
		return mediaID
	default:
		// For unknown types, return a generic format
		return fmt.Sprintf("%s:%s", mediaType, mediaID)
	}
}

// handlePlaylistEvent processes playlist events to populate the media library
func (p *Plugin) handlePlaylistEvent(event framework.Event) error {
	// When RawEvent is set in EventData, the event bus delivers the raw event directly
	// So we only need to check for direct event types, not wrapped in DataEvent

	// Check if it's a PlaylistArrayEvent (full playlist load)
	if playlistArray, ok := event.(*framework.PlaylistArrayEvent); ok {
		// Deduplication: ignore duplicate playlist events within 2 seconds for the same channel
		channel := playlistArray.ChannelName
		if channel == "" {
			logger.Warn("MediaTracker", "Skipping playlist event without channel information")
			return nil
		}

		p.mu.Lock()
		if p.lastPlaylistInfo == nil {
			p.lastPlaylistInfo = make(map[string]*playlistInfo)
		}

		if lastInfo, exists := p.lastPlaylistInfo[channel]; exists {
			if time.Since(lastInfo.time) < 2*time.Second && lastInfo.count == len(playlistArray.Items) {
				p.mu.Unlock()
				logger.Debug("MediaTracker", "Ignoring duplicate playlist event for channel %s (%d items, %v since last)",
					channel, len(playlistArray.Items), time.Since(lastInfo.time))
				return nil
			}
		}

		p.lastPlaylistInfo[channel] = &playlistInfo{
			time:  time.Now(),
			count: len(playlistArray.Items),
		}
		p.mu.Unlock()

		logger.Info("MediaTracker", "Starting to process full playlist with %d items (playlist event)", len(playlistArray.Items))
		startTime := time.Now()

		// Process all items in batches to avoid overwhelming the database
		// Reduced batch size for better timeout handling
		batchSize := 25
		for i := 0; i < len(playlistArray.Items); i += batchSize {
			end := i + batchSize
			if end > len(playlistArray.Items) {
				end = len(playlistArray.Items)
			}

			batch := playlistArray.Items[i:end]

			// Use bulk insert for better performance with extended timeout for large batches
			if err := p.bulkAddPlaylistToLibraryWithTimeout(batch, channel); err != nil {
				logger.Error("MediaTracker", "Failed to bulk add playlist items to library: %v", err)
				// Fall back to individual inserts on bulk failure
				for _, item := range batch {
					if err := p.addToLibrary(
						item.MediaID,
						item.MediaType,
						item.Title,
						item.Duration,
						item.QueuedBy,
						channel,
						item.Metadata,
					); err != nil {
						logger.Error("MediaTracker", "Failed to add playlist item to library: %v", err)
					}
				}
			}

			// Also update the queue table
			for _, item := range batch {
				if err := p.insertQueueItem(channel, &framework.QueueItem{
					Position:  item.Position,
					MediaID:   item.MediaID,
					MediaType: item.MediaType,
					Title:     item.Title,
					Duration:  item.Duration,
					QueuedBy:  item.QueuedBy,
					QueuedAt:  time.Now().Unix(),
				}); err != nil {
					logger.Error("MediaTracker", "Failed to update queue: %v", err)
				}
			}
		}

		logger.Info("MediaTracker", "Finished processing %d playlist items in %v", len(playlistArray.Items), time.Since(startTime))
		return nil
	}

	// Check if it's a PlaylistEvent (playlist modification - add/remove/move)
	if playlistEvent, ok := event.(*framework.PlaylistEvent); ok {
		logger.Debug("MediaTracker", "Received playlist modification event: %s", playlistEvent.Action)
		// Existing playlist modification handling can be added here if needed
		return nil
	}

	return nil
}

// addToLibrary adds a media item to the library if it doesn't already exist
func (p *Plugin) addToLibrary(mediaID, mediaType, title string, duration int, addedBy, channel string, metadata *framework.MediaMetadata) error {
	url := constructMediaURL(mediaID, mediaType)

	// Convert metadata to JSON
	var metaJSON interface{}
	if metadata != nil {
		// Convert MediaMetadata struct to JSON for storage
		metaJSON = metadata
	}

	query := `
		INSERT INTO daz_mediatracker_library 
		(url, media_id, media_type, title, duration, first_seen, added_by, channel, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (url) DO UPDATE SET
			last_played = CASE 
				WHEN EXCLUDED.channel = daz_mediatracker_library.channel 
				THEN CURRENT_TIMESTAMP 
				ELSE daz_mediatracker_library.last_played 
			END,
			play_count = CASE 
				WHEN EXCLUDED.channel = daz_mediatracker_library.channel 
				THEN daz_mediatracker_library.play_count + 1
				ELSE daz_mediatracker_library.play_count
			END
	`

	err := p.sqlClient.Exec(query,
		url,
		mediaID,
		mediaType,
		title,
		duration,
		time.Now(),
		addedBy,
		channel,
		metaJSON)
	if err != nil {
		return fmt.Errorf("failed to add to library: %w", err)
	}

	return nil
}

// bulkAddPlaylistToLibraryWithTimeout adds items with an extended timeout for large operations
func (p *Plugin) bulkAddPlaylistToLibraryWithTimeout(items []framework.PlaylistItem, channel string) error {
	// Use a longer timeout for bulk operations (60 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return p.bulkAddPlaylistToLibraryWithContext(ctx, items, channel)
}

// bulkAddPlaylistToLibraryWithContext adds multiple playlist items to the library with a specific context
func (p *Plugin) bulkAddPlaylistToLibraryWithContext(ctx context.Context, items []framework.PlaylistItem, channel string) error {
	if len(items) == 0 {
		return nil
	}

	// Build bulk insert query
	valueStrings := make([]string, 0, len(items))
	valueArgs := make([]interface{}, 0, len(items)*9)

	for i, item := range items {
		valueStrings = append(valueStrings, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			i*9+1, i*9+2, i*9+3, i*9+4, i*9+5, i*9+6, i*9+7, i*9+8, i*9+9,
		))

		url := constructMediaURL(item.MediaID, item.MediaType)

		// Convert metadata to JSON
		var metaJSON interface{}
		if item.Metadata != nil {
			if data, err := json.Marshal(item.Metadata); err == nil {
				metaJSON = data
			}
		}

		valueArgs = append(valueArgs,
			url,
			item.MediaID,
			item.MediaType,
			item.Title,
			item.Duration,
			time.Now(),
			item.QueuedBy,
			channel,
			metaJSON)
	}

	query := fmt.Sprintf(`
		INSERT INTO daz_mediatracker_library 
		(url, media_id, media_type, title, duration, first_seen, added_by, channel, metadata)
		VALUES %s
		ON CONFLICT (url) DO UPDATE SET
			last_played = CASE 
				WHEN EXCLUDED.channel = daz_mediatracker_library.channel 
				THEN CURRENT_TIMESTAMP 
				ELSE daz_mediatracker_library.last_played 
			END,
			play_count = CASE 
				WHEN EXCLUDED.channel = daz_mediatracker_library.channel 
				THEN daz_mediatracker_library.play_count + 1 
				ELSE daz_mediatracker_library.play_count 
			END
	`, strings.Join(valueStrings, ","))

	// Use ExecContext to respect the provided timeout
	if _, err := p.sqlClient.ExecContext(ctx, query, valueArgs...); err != nil {
		return fmt.Errorf("bulk insert failed: %w", err)
	}

	// Successfully bulk added items to library
	return nil
}
