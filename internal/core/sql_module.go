package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLModule handles database operations for the core plugin
type SQLModule struct {
	config   DatabaseConfig
	pool     *pgxpool.Pool
	eventBus framework.EventBus
}

// NewSQLModule creates a new SQL module instance
func NewSQLModule(config DatabaseConfig, eventBus framework.EventBus) (*SQLModule, error) {
	return &SQLModule{
		config:   config,
		eventBus: eventBus,
	}, nil
}

// Start initializes the database connection
func (s *SQLModule) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.ConnectTimeout)*time.Second)
	defer cancel()

	// Build connection string
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		s.config.Host,
		s.config.Port,
		s.config.User,
		s.config.Password,
		s.config.Database,
	)

	// Configure pool
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	poolConfig.MaxConns = int32(s.config.MaxConnections)
	poolConfig.MaxConnLifetime = time.Duration(s.config.MaxConnLifetime) * time.Second

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	s.pool = pool

	// Initialize schema
	if err := s.initializeSchema(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Printf("[SQLModule] Connected to database successfully")
	return nil
}

// Stop closes the database connection
func (s *SQLModule) Stop() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// LogEvent stores a Cytube event in the database
func (s *SQLModule) LogEvent(ctx context.Context, event framework.Event) error {
	if s.pool == nil {
		return fmt.Errorf("database not connected")
	}

	query := `
		INSERT INTO daz_core_events (
			event_type, channel_name, timestamp, 
			username, message, raw_data
		) VALUES ($1, $2, $3, $4, $5, $6)
	`

	// Extract common fields
	eventType := event.Type()
	timestamp := event.Timestamp()
	channelName := ""
	var rawData []byte

	// Extract type-specific fields
	var username, message string
	switch e := event.(type) {
	case *framework.ChatMessageEvent:
		channelName = e.ChannelName
		username = e.Username
		message = e.Message
		rawData = e.RawData
	case *framework.UserJoinEvent:
		channelName = e.ChannelName
		username = e.Username
		rawData = e.RawData
	case *framework.UserLeaveEvent:
		channelName = e.ChannelName
		username = e.Username
		rawData = e.RawData
	case *framework.VideoChangeEvent:
		channelName = e.ChannelName
		message = e.Title
		rawData = e.RawData
	}

	_, err := s.pool.Exec(ctx, query,
		eventType, channelName, timestamp,
		username, message, rawData,
	)

	if err != nil {
		return fmt.Errorf("failed to log event: %w", err)
	}

	return nil
}

// initializeSchema creates the necessary database tables
func (s *SQLModule) initializeSchema(ctx context.Context) error {
	queries := []string{
		// Core events table
		`CREATE TABLE IF NOT EXISTS daz_core_events (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(50) NOT NULL,
			channel_name VARCHAR(100) NOT NULL,
			timestamp TIMESTAMP NOT NULL,
			username VARCHAR(100),
			message TEXT,
			raw_data JSONB NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		// Create indexes separately
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON daz_core_events (timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_channel_type ON daz_core_events (channel_name, event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_username ON daz_core_events (username)`,

		// Connection state table
		`CREATE TABLE IF NOT EXISTS daz_core_connection_state (
			id SERIAL PRIMARY KEY,
			channel_name VARCHAR(100) NOT NULL,
			retry_count INTEGER DEFAULT 0,
			last_attempt TIMESTAMP,
			last_success TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(channel_name)
		)`,

		// User activity table
		`CREATE TABLE IF NOT EXISTS daz_core_user_activity (
			id BIGSERIAL PRIMARY KEY,
			channel_name VARCHAR(100) NOT NULL,
			username VARCHAR(100) NOT NULL,
			join_time TIMESTAMP NOT NULL,
			leave_time TIMESTAMP,
			session_duration INTEGER,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		// Create indexes for user activity
		`CREATE INDEX IF NOT EXISTS idx_user_activity_channel ON daz_core_user_activity (channel_name)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_username ON daz_core_user_activity (username)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_join_time ON daz_core_user_activity (join_time)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema query: %w", err)
		}
	}

	log.Printf("[SQLModule] Database schema initialized")
	return nil
}

// HandleSQLExec executes a SQL statement that doesn't return rows (INSERT, UPDATE, DELETE)
func (s *SQLModule) HandleSQLExec(ctx context.Context, req framework.SQLRequest) error {
	if s.pool == nil {
		return fmt.Errorf("database not connected")
	}

	// Validate that the query is for this plugin's tables
	// In a real implementation, we'd parse the SQL to ensure it only accesses allowed tables

	_, err := s.pool.Exec(ctx, req.Query, req.Params...)
	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}

	return nil
}

// HandleSQLRequest processes SQL requests from other plugins
func (s *SQLModule) HandleSQLRequest(ctx context.Context, req framework.SQLRequest) (framework.QueryResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database not connected")
	}

	// Validate that the query is for this plugin's tables
	// In a real implementation, we'd parse the SQL to ensure it only accesses allowed tables

	rows, err := s.pool.Query(ctx, req.Query, req.Params...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return &pgxQueryResult{rows: rows}, nil
}

// pgxQueryResult wraps pgx rows to implement framework.QueryResult
type pgxQueryResult struct {
	rows pgx.Rows
}

func (r *pgxQueryResult) Next() bool {
	return r.rows.Next()
}

func (r *pgxQueryResult) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *pgxQueryResult) Close() error {
	r.rows.Close()
	return nil
}
