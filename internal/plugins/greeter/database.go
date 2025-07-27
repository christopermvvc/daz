package greeter

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hildolfr/daz/internal/logger"
)

// createTables creates the necessary database tables for the greeter plugin
func (p *Plugin) createTables() error {
	// Greeter history table - tracks all greetings sent
	historyTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_greeter_history (
		id BIGSERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		greeting_type VARCHAR(50) NOT NULL,
		greeting_text TEXT NOT NULL,
		greeting_metadata JSONB,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_greeter_history_user 
		ON daz_greeter_history(channel, username, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_greeter_history_type 
		ON daz_greeter_history(greeting_type, created_at DESC);
	`

	// User preferences table - tracks opt-out status
	preferencesTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_greeter_preferences (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL,
		username VARCHAR(255) NOT NULL,
		opted_out BOOLEAN DEFAULT FALSE,
		opt_out_reason VARCHAR(255),
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel, username)
	);

	CREATE INDEX IF NOT EXISTS idx_greeter_preferences_opted_out 
		ON daz_greeter_preferences(channel, username) 
		WHERE opted_out = TRUE;
	`

	// Greeter state table - tracks last greeting per channel
	stateTableSQL := `
	CREATE TABLE IF NOT EXISTS daz_greeter_state (
		id SERIAL PRIMARY KEY,
		channel VARCHAR(255) NOT NULL UNIQUE,
		last_greeting_at TIMESTAMP,
		last_greeting_user VARCHAR(255),
		total_greetings_sent INT DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_greeter_state_channel 
		ON daz_greeter_state(channel);
	`

	// Execute table creation with proper error handling
	if err := p.sqlClient.Exec(historyTableSQL); err != nil {
		return fmt.Errorf("failed to create history table: %w", err)
	}

	if err := p.sqlClient.Exec(preferencesTableSQL); err != nil {
		return fmt.Errorf("failed to create preferences table: %w", err)
	}

	if err := p.sqlClient.Exec(stateTableSQL); err != nil {
		return fmt.Errorf("failed to create state table: %w", err)
	}

	logger.Info("Greeter", "Successfully created database tables")
	return nil
}

// isUserOptedOut checks if a user has opted out of greetings
func (p *Plugin) isUserOptedOut(ctx context.Context, channel, username string) (bool, error) {
	query := `
		SELECT opted_out 
		FROM daz_greeter_preferences 
		WHERE channel = $1 AND LOWER(username) = LOWER($2)
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return false, fmt.Errorf("failed to query opt-out status: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	// If no row exists, user hasn't opted out
	if !rows.Next() {
		return false, nil
	}

	var optedOut bool
	if err := rows.Scan(&optedOut); err != nil {
		return false, fmt.Errorf("failed to scan opt-out status: %w", err)
	}

	return optedOut, nil
}

// recordGreetingToDB saves a greeting to the history table
func (p *Plugin) recordGreetingToDB(ctx context.Context, channel, username, greetingType, greetingText string, metadata map[string]interface{}) error {
	// Convert metadata to JSON if provided
	var metadataJSON interface{}
	if metadata != nil {
		metadataJSON = metadata
	}

	query := `
		INSERT INTO daz_greeter_history 
			(channel, username, greeting_type, greeting_text, greeting_metadata)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := p.sqlClient.ExecContext(ctx, query, channel, username, greetingType, greetingText, metadataJSON)
	if err != nil {
		return fmt.Errorf("failed to record greeting: %w", err)
	}

	// Update greeter state
	if err := p.updateGreeterState(ctx, channel, username); err != nil {
		logger.Error("Greeter", "Failed to update greeter state: %v", err)
		// Don't fail the whole operation if state update fails
	}

	return nil
}

// getLastGreeting retrieves the most recent greeting for a user
func (p *Plugin) getLastGreeting(ctx context.Context, channel, username string) (*GreetingRecord, error) {
	query := `
		SELECT id, greeting_type, greeting_text, created_at
		FROM daz_greeter_history
		WHERE channel = $1 AND LOWER(username) = LOWER($2)
		ORDER BY created_at DESC
		LIMIT 1
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return nil, fmt.Errorf("failed to query last greeting: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		return nil, nil // No previous greeting
	}

	var record GreetingRecord
	if err := rows.Scan(&record.ID, &record.GreetingType, &record.GreetingText, &record.CreatedAt); err != nil {
		return nil, fmt.Errorf("failed to scan greeting record: %w", err)
	}

	return &record, nil
}

// wasUserRecentlyActive checks if a user left the channel in the last N minutes
// This prevents greeting users who just disconnected and reconnected
func (p *Plugin) wasUserRecentlyActive(ctx context.Context, channel, username string, withinMinutes int) (bool, error) {
	cutoffTime := time.Now().Add(-time.Duration(withinMinutes) * time.Minute)

	// Check if there was a user leave event in the recent past
	// This indicates the user was previously in the channel and left recently
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM daz_user_tracker_history
			WHERE channel = $1 
				AND LOWER(username) = LOWER($2)
				AND event_type = 'leave'
				AND timestamp >= $3
		)
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username, cutoffTime)
	if err != nil {
		return false, fmt.Errorf("failed to query user activity: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		return false, nil
	}

	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to scan activity result: %w", err)
	}

	return exists, nil
}

// wasUserRecentlyGreeted checks if a user was greeted in ANY channel within the specified time
func (p *Plugin) wasUserRecentlyGreeted(ctx context.Context, username string, withinMinutes int) (bool, error) {
	cutoffTime := time.Now().Add(-time.Duration(withinMinutes) * time.Minute)

	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM daz_greeter_history
			WHERE LOWER(username) = LOWER($1)
				AND created_at >= $2
		)
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, username, cutoffTime)
	if err != nil {
		return false, fmt.Errorf("failed to query recent greetings: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	// If no row exists, user hasn't been greeted recently
	if !rows.Next() {
		return false, nil
	}

	var wasGreeted bool
	if err := rows.Scan(&wasGreeted); err != nil {
		return false, fmt.Errorf("failed to scan recent greeting status: %w", err)
	}

	return wasGreeted, nil
}

// isUserInChannel checks if a user is currently active in the channel
func (p *Plugin) isUserInChannel(ctx context.Context, channel, username string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM daz_user_tracker_sessions
			WHERE channel = $1 
				AND LOWER(username) = LOWER($2)
				AND is_active = TRUE
		)
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return false, fmt.Errorf("failed to query user presence: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		return false, nil
	}

	var isPresent bool
	if err := rows.Scan(&isPresent); err != nil {
		return false, fmt.Errorf("failed to scan user presence: %w", err)
	}

	return isPresent, nil
}

// updateGreeterState updates the channel's greeter state
func (p *Plugin) updateGreeterState(ctx context.Context, channel, username string) error {
	query := `
		INSERT INTO daz_greeter_state 
			(channel, last_greeting_at, last_greeting_user, total_greetings_sent, updated_at)
		VALUES ($1, $2, $3, 1, $2)
		ON CONFLICT (channel) 
		DO UPDATE SET 
			last_greeting_at = EXCLUDED.last_greeting_at,
			last_greeting_user = EXCLUDED.last_greeting_user,
			total_greetings_sent = daz_greeter_state.total_greetings_sent + 1,
			updated_at = EXCLUDED.updated_at
	`

	now := time.Now()
	_, err := p.sqlClient.ExecContext(ctx, query, channel, now, username)
	if err != nil {
		return fmt.Errorf("failed to update greeter state: %w", err)
	}

	return nil
}

// getUserFirstSeenTime gets the first time a user was seen in a channel
func (p *Plugin) getUserFirstSeenTime(ctx context.Context, channel, username string) (*time.Time, error) {
	// Query user_tracker_history for the first join event
	query := `
		SELECT MIN(timestamp)
		FROM daz_user_tracker_history
		WHERE channel = $1 
			AND LOWER(username) = LOWER($2)
			AND event_type = 'join'
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return nil, fmt.Errorf("failed to query first seen time: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error("Greeter", "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		return nil, nil // User never seen before
	}

	var firstSeen sql.NullTime
	if err := rows.Scan(&firstSeen); err != nil {
		return nil, fmt.Errorf("failed to scan first seen time: %w", err)
	}

	if !firstSeen.Valid {
		return nil, nil
	}

	return &firstSeen.Time, nil
}

// GreetingRecord represents a greeting from the history table
type GreetingRecord struct {
	ID           int64
	GreetingType string
	GreetingText string
	CreatedAt    time.Time
}

// setUserOptOut updates a user's opt-out preference
func (p *Plugin) setUserOptOut(ctx context.Context, channel, username string, optOut bool, reason string) error {
	query := `
		INSERT INTO daz_greeter_preferences 
			(channel, username, opted_out, opt_out_reason, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (channel, username) 
		DO UPDATE SET 
			opted_out = EXCLUDED.opted_out,
			opt_out_reason = EXCLUDED.opt_out_reason,
			updated_at = EXCLUDED.updated_at
	`

	now := time.Now()
	_, err := p.sqlClient.ExecContext(ctx, query, channel, username, optOut, reason, now)
	if err != nil {
		return fmt.Errorf("failed to update opt-out preference: %w", err)
	}

	return nil
}
