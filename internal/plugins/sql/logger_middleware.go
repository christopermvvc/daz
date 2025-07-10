package sql

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// LoggerMiddleware handles selective event logging based on rules
type LoggerMiddleware struct {
	plugin      *Plugin
	rules       []LoggerRule
	transforms  map[string]TransformFunc
	batchBuffer map[string][]LogEntry
	batchSize   int
	flushTicker *time.Ticker
	mu          sync.Mutex
}

// LogEntry represents a single log entry to be persisted
type LogEntry struct {
	Table  string
	Fields map[string]interface{}
}

// TransformFunc transforms event data into log fields
type TransformFunc func(event framework.Event, rule LoggerRule) (map[string]interface{}, error)

// NewLoggerMiddleware creates a new logger middleware instance
func NewLoggerMiddleware(plugin *Plugin, batchSize int) *LoggerMiddleware {
	lm := &LoggerMiddleware{
		plugin:      plugin,
		rules:       plugin.loggerRules,
		transforms:  make(map[string]TransformFunc),
		batchBuffer: make(map[string][]LogEntry),
		batchSize:   batchSize,
	}

	// Register default transforms
	lm.registerDefaultTransforms()

	// Start batch flush ticker
	if batchSize > 0 {
		lm.flushTicker = time.NewTicker(5 * time.Second)
		go lm.batchFlushWorker()
	}

	return lm
}

// registerDefaultTransforms sets up built-in transform functions
func (lm *LoggerMiddleware) registerDefaultTransforms() {
	// Generic transform - logs all available fields from event metadata
	lm.transforms["generic_transform"] = func(event framework.Event, rule LoggerRule) (map[string]interface{}, error) {
		fields := map[string]interface{}{
			"event_type": event.Type(),
			"timestamp":  event.Timestamp(),
		}

		// Extract metadata if it's a CytubeEvent
		if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
			// Add channel from metadata
			if channel, exists := cytubeEvent.Metadata["channel"]; exists {
				fields["channel"] = channel
			}

			// Add other common metadata fields
			for key, value := range cytubeEvent.Metadata {
				if key != "channel" && key != "eventType" {
					fields[key] = value
				}
			}
		}

		// Filter fields if specified
		if len(rule.Fields) > 0 {
			filtered := make(map[string]interface{})
			for _, field := range rule.Fields {
				if val, ok := fields[field]; ok {
					filtered[field] = val
				}
			}
			return filtered, nil
		}

		return fields, nil
	}

	// Chat message transform - extracts chat-specific fields
	lm.transforms["chat_transform"] = func(event framework.Event, rule LoggerRule) (map[string]interface{}, error) {
		fields := map[string]interface{}{
			"event_type": event.Type(),
			"timestamp":  event.Timestamp(),
		}

		// Extract from metadata
		if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
			metadata := cytubeEvent.Metadata

			// Extract chat-specific fields
			if username, ok := metadata["username"]; ok {
				fields["username"] = username
			}
			if message, ok := metadata["msg"]; ok {
				fields["message"] = message
			}
			if channel, ok := metadata["channel"]; ok {
				fields["channel"] = channel
			}
			if rank, ok := metadata["rank"]; ok {
				fields["user_rank"] = rank
			}
		}

		// Filter fields if specified
		if len(rule.Fields) > 0 {
			filtered := make(map[string]interface{})
			for _, field := range rule.Fields {
				if val, ok := fields[field]; ok {
					filtered[field] = val
				}
			}
			return filtered, nil
		}

		return fields, nil
	}

	// User activity transform
	lm.transforms["user_transform"] = func(event framework.Event, rule LoggerRule) (map[string]interface{}, error) {
		fields := map[string]interface{}{
			"event_type": event.Type(),
			"timestamp":  event.Timestamp(),
		}

		// Extract from metadata
		if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
			metadata := cytubeEvent.Metadata

			if username, ok := metadata["username"]; ok {
				fields["username"] = username
			}
			if channel, ok := metadata["channel"]; ok {
				fields["channel"] = channel
			}
			if action, ok := metadata["action"]; ok {
				fields["action"] = action
			}
		}

		return fields, nil
	}
}

// ProcessEvent checks if an event should be logged and processes it
func (lm *LoggerMiddleware) ProcessEvent(event framework.Event) error {
	eventType := event.Type()

	for _, rule := range lm.rules {
		if !rule.Enabled || rule.regex == nil {
			continue
		}

		// Check if event matches rule pattern
		if !rule.regex.MatchString(eventType) {
			continue
		}

		// Get transform function
		transform := lm.getTransform(rule.Transform)

		// Apply transform
		fields, err := transform(event, rule)
		if err != nil {
			log.Printf("[SQL Logger] Transform error for %s: %v", eventType, err)
			continue
		}

		// Create log entry
		entry := LogEntry{
			Table:  rule.Table,
			Fields: fields,
		}

		// Handle batching or immediate write
		if lm.batchSize > 0 {
			lm.addToBatch(rule.Table, entry)
		} else {
			if err := lm.writeEntry(entry); err != nil {
				log.Printf("[SQL Logger] Failed to write entry: %v", err)
			}
		}
	}

	return nil
}

// getTransform returns the appropriate transform function
func (lm *LoggerMiddleware) getTransform(name string) TransformFunc {
	if name == "" {
		return lm.transforms["generic_transform"]
	}

	if transform, ok := lm.transforms[name]; ok {
		return transform
	}

	log.Printf("[SQL Logger] Transform '%s' not found, using generic", name)
	return lm.transforms["generic_transform"]
}

// addToBatch adds an entry to the batch buffer
func (lm *LoggerMiddleware) addToBatch(table string, entry LogEntry) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.batchBuffer[table] = append(lm.batchBuffer[table], entry)

	// Flush if batch size reached
	if len(lm.batchBuffer[table]) >= lm.batchSize {
		go lm.flushTable(table)
	}
}

// flushTable writes all entries for a specific table
func (lm *LoggerMiddleware) flushTable(table string) {
	lm.mu.Lock()
	entries := lm.batchBuffer[table]
	lm.batchBuffer[table] = nil
	lm.mu.Unlock()

	if len(entries) == 0 {
		return
	}

	// Build batch insert query
	if err := lm.writeBatch(table, entries); err != nil {
		log.Printf("[SQL Logger] Failed to write batch for %s: %v", table, err)
	}
}

// writeEntry writes a single log entry
func (lm *LoggerMiddleware) writeEntry(entry LogEntry) error {
	// Build INSERT query dynamically based on fields
	query, params := lm.buildInsertQuery(entry.Table, entry.Fields)

	req := &framework.SQLExecRequest{
		ID:            lm.plugin.generateID(),
		CorrelationID: lm.plugin.generateID(),
		Query:         query,
		Params:        params,
		Timeout:       5 * time.Second,
		RequestBy:     "sql-logger",
	}

	data := &framework.EventData{
		SQLExecRequest: req,
	}

	// Send exec request
	lm.plugin.eventBus.Broadcast("sql.exec.request", data)

	return nil
}

// writeBatch writes multiple entries in a single query
func (lm *LoggerMiddleware) writeBatch(table string, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Build batch INSERT query
	query, params := lm.buildBatchInsertQuery(table, entries)

	req := &framework.SQLExecRequest{
		ID:            lm.plugin.generateID(),
		CorrelationID: lm.plugin.generateID(),
		Query:         query,
		Params:        params,
		Timeout:       10 * time.Second,
		RequestBy:     "sql-logger-batch",
	}

	data := &framework.EventData{
		SQLExecRequest: req,
	}

	// Send exec request
	lm.plugin.eventBus.Broadcast("sql.exec.request", data)

	log.Printf("[SQL Logger] Flushed %d entries to %s", len(entries), table)
	return nil
}

// buildInsertQuery creates an INSERT query from fields
func (lm *LoggerMiddleware) buildInsertQuery(table string, fields map[string]interface{}) (string, []framework.SQLParam) {
	var columns []string
	var values []string
	var params []framework.SQLParam
	paramIndex := 1

	for col, val := range fields {
		columns = append(columns, col)
		values = append(values, fmt.Sprintf("$%d", paramIndex))
		params = append(params, framework.NewSQLParam(val))
		paramIndex++
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(columns, ", "),
		strings.Join(values, ", "))

	return query, params
}

// buildBatchInsertQuery creates a batch INSERT query
func (lm *LoggerMiddleware) buildBatchInsertQuery(table string, entries []LogEntry) (string, []framework.SQLParam) {
	if len(entries) == 0 {
		return "", nil
	}

	// Use first entry to determine columns
	var columns []string
	for col := range entries[0].Fields {
		columns = append(columns, col)
	}

	var valueRows []string
	var params []framework.SQLParam
	paramIndex := 1

	for _, entry := range entries {
		var values []string
		for _, col := range columns {
			values = append(values, fmt.Sprintf("$%d", paramIndex))
			params = append(params, framework.NewSQLParam(entry.Fields[col]))
			paramIndex++
		}
		valueRows = append(valueRows, fmt.Sprintf("(%s)", strings.Join(values, ", ")))
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		table,
		strings.Join(columns, ", "),
		strings.Join(valueRows, ", "))

	return query, params
}

// batchFlushWorker periodically flushes batches
func (lm *LoggerMiddleware) batchFlushWorker() {
	for range lm.flushTicker.C {
		lm.flushAll()
	}
}

// flushAll flushes all pending batches
func (lm *LoggerMiddleware) flushAll() {
	lm.mu.Lock()
	tables := make([]string, 0, len(lm.batchBuffer))
	for table := range lm.batchBuffer {
		tables = append(tables, table)
	}
	lm.mu.Unlock()

	for _, table := range tables {
		lm.flushTable(table)
	}
}

// Stop cleanly shuts down the logger middleware
func (lm *LoggerMiddleware) Stop() {
	if lm.flushTicker != nil {
		lm.flushTicker.Stop()
		lm.flushAll()
	}
}
