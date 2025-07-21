package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus"
)

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	config    Config
	pool      *pgxpool.Pool
	db        *sql.DB
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
	readyChan chan struct{}

	loggerRules   []LoggerRule
	eventsHandled int64
	idCounter     int64
}

type Config struct {
	Database    DatabaseConfig `json:"database"`
	LoggerRules []LoggerRule   `json:"logger_rules"`
}

type DatabaseConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	User            string `json:"user"`
	Password        string `json:"password"`
	MaxConnections  int    `json:"max_connections"`
	MaxConnLifetime int    `json:"max_conn_lifetime"`
	ConnectTimeout  int    `json:"connect_timeout"`
}

type LoggerRule struct {
	EventPattern string   `json:"event_pattern"`
	Enabled      bool     `json:"enabled"`
	Table        string   `json:"table"`
	Fields       []string `json:"fields,omitempty"`
	Transform    string   `json:"transform,omitempty"`
	regex        *regexp.Regexp
}

// LogFields represents all possible fields that can be logged from EventData
type LogFields struct {
	EventType   string    `json:"event_type"`
	Timestamp   time.Time `json:"timestamp"`
	Username    string    `json:"username,omitempty"`
	Message     string    `json:"message,omitempty"`
	UserRank    int       `json:"user_rank,omitempty"`
	UserID      string    `json:"user_id,omitempty"`
	Channel     string    `json:"channel,omitempty"`
	VideoID     string    `json:"video_id,omitempty"`
	VideoType   string    `json:"video_type,omitempty"`
	Title       string    `json:"title,omitempty"`
	Duration    int       `json:"duration,omitempty"`
	ToUser      string    `json:"to_user,omitempty"`
	MessageTime int64     `json:"message_time,omitempty"`
}

func NewPlugin() *Plugin {
	return &Plugin{
		name:      "sql",
		readyChan: make(chan struct{}),
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{} // SQL plugin has no dependencies
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

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	if err := json.Unmarshal(configData, &p.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	p.eventBus = bus

	for i := range p.config.LoggerRules {
		if p.config.LoggerRules[i].EventPattern != "" {
			pattern := p.config.LoggerRules[i].EventPattern
			pattern = strings.ReplaceAll(pattern, ".", "\\.")
			pattern = strings.ReplaceAll(pattern, "*", ".*")
			pattern = "^" + pattern + "$"

			regex, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("invalid event pattern %s: %w", p.config.LoggerRules[i].EventPattern, err)
			}
			p.config.LoggerRules[i].regex = regex
		}
	}

	p.loggerRules = p.config.LoggerRules

	logger.Debug("SQL", "Initialized with %d logger rules", len(p.loggerRules))
	return nil
}

func (p *Plugin) Start() error {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.startTime = time.Now()

	// Don't subscribe to events until database is connected
	// This prevents handling requests before we're ready

	// Connect to database in a goroutine to avoid blocking
	go func() {
		logger.Debug("SQL", "Starting database connection...")
		startTime := time.Now()

		if err := p.connectDatabase(); err != nil {
			logger.Error("SQL", "Failed to connect to database after %v: %v", time.Since(startTime), err)
			// Don't close readyChan on error, let handlers return errors
			return
		}

		logger.Debug("SQL", "Database connected successfully after %v", time.Since(startTime))

		// Subscribe to event handlers after database is connected
		logger.Debug("SQL", "Subscribing to event handlers...")

		if err := p.eventBus.Subscribe("log.request", p.handleLogRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.request: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.request")

		if err := p.eventBus.Subscribe("log.batch", p.handleBatchLogRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.batch: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.batch")

		if err := p.eventBus.Subscribe("log.configure", p.handleConfigureLogging); err != nil {
			logger.Error("SQL", "Failed to subscribe to log.configure: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to log.configure")

		// Subscribe to plugin.request for ALL synchronous requests
		// This is the standard pattern for handling eventBus.Request() calls
		if err := p.eventBus.Subscribe("plugin.request", p.handlePluginRequest); err != nil {
			logger.Error("SQL", "Failed to subscribe to plugin.request: %v", err)
			return
		}
		logger.Debug("SQL", "Subscribed to plugin.request")

		// NOTE: All SQL synchronous requests (query, exec, batch) are handled through plugin.request
		// The handlePluginRequest function inspects the EventData and routes to the appropriate handler:
		// - SQLQueryRequest -> handleSQLQuery
		// - SQLExecRequest -> handleSQLExec
		// - SQLBatchRequest -> handleBatchQueryRequest or handleBatchExecRequest

		// Subscribe to logger rules after database is connected
		for _, rule := range p.loggerRules {
			if rule.Enabled && rule.regex != nil {
				logger.Debug("SQL", "Subscribing to pattern: %s", rule.EventPattern)
				for _, eventType := range p.findMatchingEventTypes(rule.EventPattern) {
					if err := p.eventBus.Subscribe(eventType, p.createLoggerHandler(rule)); err != nil {
						logger.Error("SQL", "Failed to subscribe to %s: %v", eventType, err)
					}
				}
			}
		}

		// Signal that the plugin is ready after database is connected
		close(p.readyChan)
		logger.Debug("SQL", "Started successfully and ready to accept requests after %v total startup time", time.Since(startTime))
	}()

	logger.Debug("SQL", "Started (connecting to database in background)")
	return nil
}

func (p *Plugin) Stop() error {
	p.cancel()

	if p.pool != nil {
		p.pool.Close()
	}
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			logger.Error("SQL", "Failed to close database connection: %v", err)
		}
	}

	logger.Info("SQL", "Stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	p.eventsHandled++
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:          p.name,
		State:         "running",
		LastError:     nil,
		RetryCount:    0,
		EventsHandled: p.eventsHandled,
		Uptime:        time.Since(p.startTime),
	}
}

// emitFailureEvent emits a failure event that can be picked up by the retry plugin
func (p *Plugin) emitFailureEvent(eventType, correlationID, source, operationType string, err error) {
	failureData := &framework.EventData{
		KeyValue: map[string]string{
			"correlation_id": correlationID,
			"source":         source,
			"operation_type": operationType,
			"error":          err.Error(),
			"timestamp":      time.Now().Format(time.RFC3339),
		},
	}

	// Emit the failure event asynchronously
	go func() {
		if err := p.eventBus.Broadcast(eventType, failureData); err != nil {
			logger.Error("SQL", "Failed to emit failure event: %v", err)
		}
	}()
}

func (p *Plugin) connectDatabase() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.config.Database.ConnectTimeout)*time.Second)
	defer cancel()

	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		p.config.Database.Host,
		p.config.Database.Port,
		p.config.Database.User,
		p.config.Database.Password,
		p.config.Database.Database,
	)

	logger.Debug("SQL", "Connecting to database at %s:%d/%s",
		p.config.Database.Host, p.config.Database.Port, p.config.Database.Database)

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	poolConfig.MaxConns = int32(p.config.Database.MaxConnections)
	poolConfig.MaxConnLifetime = time.Duration(p.config.Database.MaxConnLifetime) * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	p.pool = pool

	connConfig, err := pgx.ParseConfig(connStr)
	if err != nil {
		pool.Close()
		return fmt.Errorf("failed to parse config for stdlib: %w", err)
	}

	stdlib.RegisterConnConfig(connConfig)
	db, err := sql.Open("pgx", stdlib.RegisterConnConfig(connConfig))
	if err != nil {
		pool.Close()
		return fmt.Errorf("failed to open stdlib connection: %w", err)
	}

	// Set connection pool settings for stdlib
	db.SetMaxOpenConns(p.config.Database.MaxConnections)
	db.SetMaxIdleConns(p.config.Database.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Duration(p.config.Database.MaxConnLifetime) * time.Second)

	if err := db.PingContext(ctx); err != nil {
		pool.Close()
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("SQL", "Failed to close database after ping error: %v", closeErr)
		}
		return fmt.Errorf("failed to ping stdlib database: %w", err)
	}

	p.db = db

	logger.Debug("SQL", "Initializing database schema...")
	schemaStart := time.Now()
	if err := p.initializeSchema(ctx); err != nil {
		pool.Close()
		if closeErr := db.Close(); closeErr != nil {
			logger.Error("SQL", "Failed to close database after schema init error: %v", closeErr)
		}
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	logger.Debug("SQL", "Schema initialization completed in %v", time.Since(schemaStart))

	logger.Debug("SQL", "Connected to database successfully")
	return nil
}

func (p *Plugin) initializeSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS daz_core_events (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			channel_name VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			username VARCHAR(100),
			message TEXT,
			video_id VARCHAR(255),
			video_type VARCHAR(50),
			title TEXT,
			duration INTEGER,
			raw_data JSONB NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON daz_core_events (timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_channel_type ON daz_core_events (channel_name, event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_username ON daz_core_events (username)`,

		`CREATE TABLE IF NOT EXISTS daz_chat_log (
			id BIGSERIAL PRIMARY KEY,
			username VARCHAR(100) NOT NULL,
			message TEXT NOT NULL,
			user_rank INTEGER,
			user_id VARCHAR(100),
			channel VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_channel ON daz_chat_log (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_username ON daz_chat_log (username)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON daz_chat_log (timestamp)`,

		`CREATE TABLE IF NOT EXISTS daz_user_activity (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			username VARCHAR(100) NOT NULL,
			user_rank INTEGER,
			channel VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			raw_data JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_event ON daz_user_activity (event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_channel ON daz_user_activity (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_username ON daz_user_activity (username)`,

		`CREATE TABLE IF NOT EXISTS daz_plugin_logs (
			id BIGSERIAL PRIMARY KEY,
			plugin_name VARCHAR(100) NOT NULL,
			event_type VARCHAR(100) NOT NULL,
			log_data JSONB NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_plugin ON daz_plugin_logs (plugin_name)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_event ON daz_plugin_logs (event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_logs_timestamp ON daz_plugin_logs (timestamp)`,

		`CREATE TABLE IF NOT EXISTS daz_private_messages (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			from_user VARCHAR(100) NOT NULL,
			to_user VARCHAR(100) NOT NULL,
			message TEXT NOT NULL,
			channel VARCHAR(100) NOT NULL,
			message_time BIGINT NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_users ON daz_private_messages (from_user, to_user)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_channel ON daz_private_messages (channel)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_time ON daz_private_messages (message_time)`,
		`CREATE INDEX IF NOT EXISTS idx_pm_timestamp ON daz_private_messages (timestamp)`,

		// Migrations to add missing columns
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS video_id VARCHAR(255)`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS video_type VARCHAR(50)`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS title TEXT`,
		`ALTER TABLE daz_core_events ADD COLUMN IF NOT EXISTS duration INTEGER`,
	}

	for _, query := range queries {
		if _, err := p.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema query: %w", err)
		}
	}

	logger.Debug("SQL", "Database schema initialized")
	return nil
}

func (p *Plugin) generateID() string {
	p.idCounter++
	return fmt.Sprintf("sql-%d-%d", time.Now().UnixNano(), p.idCounter)
}

func (p *Plugin) findMatchingEventTypes(pattern string) []string {
	var matches []string

	if strings.HasSuffix(pattern, "*") {
		if strings.HasPrefix(pattern, "cytube.event.") {
			base := strings.TrimSuffix(pattern, "*")
			commonTypes := []string{"chatMsg", "userJoin", "userLeave", "changeMedia", "queue", "mediaUpdate", "pm", "pm.sent", "playlist"}
			for _, t := range commonTypes {
				eventType := base + t
				matches = append(matches, eventType)
			}
		} else if pattern == "cytube.event.*" {
			matches = append(matches, "cytube.event")
		} else {
			matches = append(matches, strings.TrimSuffix(pattern, "*"))
		}
	} else {
		// Exact match
		matches = append(matches, pattern)
	}

	return matches
}

func (p *Plugin) createLoggerHandler(rule LoggerRule) framework.EventHandler {
	return func(event framework.Event) error {
		if dataEvent, ok := event.(*framework.DataEvent); ok {
			if dataEvent.Data != nil {
				return p.logEventData(rule, dataEvent)
			}
		}
		return nil
	}
}

func (p *Plugin) logEventData(rule LoggerRule, event *framework.DataEvent) error {
	p.eventsHandled++

	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	// Validate table name to prevent SQL injection
	if !isValidTableName(rule.Table) {
		return fmt.Errorf("invalid table name: %s", rule.Table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()

	fields := p.extractFieldsForRule(rule, event.Data)
	fields.EventType = event.EventType
	fields.Timestamp = event.EventTime

	// Convert struct to columns and values for SQL
	columns := []string{}
	values := []interface{}{}
	placeholders := []string{}

	// Always include event_type and timestamp
	columns = append(columns, "event_type", "timestamp")
	values = append(values, fields.EventType, fields.Timestamp)
	placeholders = append(placeholders, "$1", "$2")
	i := 3

	// Add non-empty fields
	if fields.Username != "" {
		// Use "from_user" for PM table, "username" for others
		columnName := "username"
		if rule.Table == "daz_private_messages" {
			columnName = "from_user"
		}
		columns = append(columns, columnName)
		values = append(values, fields.Username)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Message != "" {
		columns = append(columns, "message")
		values = append(values, fields.Message)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.UserRank != 0 {
		columns = append(columns, "user_rank")
		values = append(values, fields.UserRank)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.UserID != "" {
		columns = append(columns, "user_id")
		values = append(values, fields.UserID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Channel != "" {
		// Use "channel" for daz_chat_log and daz_private_messages tables, "channel_name" for others
		columnName := "channel_name"
		if rule.Table == "daz_chat_log" || rule.Table == "daz_private_messages" {
			columnName = "channel"
		}
		columns = append(columns, columnName)
		values = append(values, fields.Channel)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.VideoID != "" {
		columns = append(columns, "video_id")
		values = append(values, fields.VideoID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.VideoType != "" {
		columns = append(columns, "video_type")
		values = append(values, fields.VideoType)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Title != "" {
		columns = append(columns, "title")
		values = append(values, fields.Title)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.Duration != 0 {
		columns = append(columns, "duration")
		values = append(values, fields.Duration)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.ToUser != "" {
		columns = append(columns, "to_user")
		values = append(values, fields.ToUser)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}
	if fields.MessageTime != 0 {
		columns = append(columns, "message_time")
		values = append(values, fields.MessageTime)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
	}

	// Build query with ON CONFLICT for chat messages to prevent duplicates
	var query string
	if fields.EventType == "cytube.event.chatMsg" && rule.Table == "daz_core_events" {
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (channel_name, message_time, username) WHERE event_type = 'cytube.event.chatMsg' DO NOTHING",
			rule.Table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)
	} else {
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			rule.Table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)
	}

	_, err := p.pool.Exec(ctx, query, values...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
		logger.Error("SQL", "Failed to log event to %s: %v", rule.Table, err)
		return err
	}

	metrics.DatabaseQueries.WithLabelValues("insert").Inc()
	return nil
}

func (p *Plugin) extractFieldsForRule(rule LoggerRule, data *framework.EventData) *LogFields {
	result := &LogFields{}

	if data.ChatMessage != nil {
		result.Username = data.ChatMessage.Username
		result.Message = data.ChatMessage.Message
		result.UserRank = data.ChatMessage.UserRank
		result.UserID = data.ChatMessage.UserID
		result.Channel = data.ChatMessage.Channel
		result.MessageTime = data.ChatMessage.MessageTime
	} else if data.PrivateMessage != nil {
		result.Username = data.PrivateMessage.FromUser
		result.Message = data.PrivateMessage.Message
		result.ToUser = data.PrivateMessage.ToUser
		result.MessageTime = data.PrivateMessage.MessageTime
		result.Channel = data.PrivateMessage.Channel
	} else if data.UserJoin != nil {
		result.Username = data.UserJoin.Username
		result.UserRank = data.UserJoin.UserRank
		result.Channel = data.UserJoin.Channel
	} else if data.UserLeave != nil {
		result.Username = data.UserLeave.Username
		result.Channel = data.UserLeave.Channel
	} else if data.VideoChange != nil {
		result.VideoID = data.VideoChange.VideoID
		result.VideoType = data.VideoChange.VideoType
		result.Title = data.VideoChange.Title
		result.Duration = data.VideoChange.Duration
		result.Channel = data.VideoChange.Channel
	}

	// If specific fields are requested, create a new LogFields with only those fields
	if len(rule.Fields) > 0 {
		filtered := &LogFields{}
		for _, field := range rule.Fields {
			switch field {
			case "event_type":
				filtered.EventType = result.EventType
			case "timestamp":
				filtered.Timestamp = result.Timestamp
			case "username":
				filtered.Username = result.Username
			case "message":
				filtered.Message = result.Message
			case "user_rank":
				filtered.UserRank = result.UserRank
			case "user_id":
				filtered.UserID = result.UserID
			case "channel":
				filtered.Channel = result.Channel
			case "video_id":
				filtered.VideoID = result.VideoID
			case "video_type":
				filtered.VideoType = result.VideoType
			case "title":
				filtered.Title = result.Title
			case "duration":
				filtered.Duration = result.Duration
			case "to_user":
				filtered.ToUser = result.ToUser
			case "message_time":
				filtered.MessageTime = result.MessageTime
			}
		}
		return filtered
	}

	return result
}
