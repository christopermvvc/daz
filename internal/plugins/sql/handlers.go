package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/prometheus/client_golang/prometheus"
)

// tableNameRegex validates table names to prevent SQL injection
var tableNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// isValidTableName checks if a table name is safe to use in SQL queries
func isValidTableName(tableName string) bool {
	return tableNameRegex.MatchString(tableName) && len(tableName) <= 63 // PostgreSQL limit
}

// convertToJSONCompatible converts database driver types to JSON-compatible types
func convertToJSONCompatible(val interface{}) interface{} {
	switch v := val.(type) {
	case []byte:
		// Try to convert byte arrays to string
		return string(v)
	case int64, int32, int16, int8, int:
		// Convert all integer types to int64 for consistency
		return v
	case float64, float32:
		// Floats are already JSON-compatible
		return v
	case bool:
		// Booleans are already JSON-compatible
		return v
	case string:
		// Strings are already JSON-compatible
		return v
	case time.Time:
		// Convert time to ISO 8601 string
		return v.Format(time.RFC3339Nano)
	case nil:
		// nil is JSON-compatible as null
		return nil
	case pgtype.Numeric:
		// Handle PostgreSQL NUMERIC type
		if v.Valid {
			// Try to convert to float64
			f64, err := v.Float64Value()
			if err == nil && f64.Valid {
				return f64.Float64
			}
			// If float conversion fails, convert to string using Value()
			val, err := v.Value()
			if err == nil && val != nil {
				return fmt.Sprintf("%v", val)
			}
		}
		return nil
	default:
		// Check if it's a pointer to pgtype.Numeric
		if numPtr, ok := val.(*pgtype.Numeric); ok && numPtr != nil {
			if numPtr.Valid {
				f64, err := numPtr.Float64Value()
				if err == nil && f64.Valid {
					return f64.Float64
				}
				// If float conversion fails, convert to string
				val, err := numPtr.Value()
				if err == nil && val != nil {
					return fmt.Sprintf("%v", val)
				}
			}
			return nil
		}
		// For other types, convert to string representation
		return fmt.Sprintf("%v", val)
	}
}

// handlePluginRequest handles targeted plugin requests sent via eventBus.Request
func (p *Plugin) handlePluginRequest(event framework.Event) error {
	// This handler receives all "plugin.request" events and routes them based on metadata
	// For now, we'll just pass through to the appropriate handler based on the event data
	logger.Debug("SQL", "Received plugin.request event")

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		logger.Debug("SQL", "plugin.request event has no data")
		return nil
	}

	// Check what type of request this is and route accordingly
	if dataEvent.Data.SQLExecRequest != nil {
		logger.Debug("SQL", "Routing to SQL exec handler")
		return p.handleSQLExec(event)
	}
	if dataEvent.Data.SQLQueryRequest != nil {
		logger.Debug("SQL", "Routing to SQL query handler")
		return p.handleSQLQuery(event)
	}
	if dataEvent.Data.SQLBatchRequest != nil {
		logger.Debug("SQL", "Routing to SQL batch handler")
		// Determine if it's a query or exec batch
		if len(dataEvent.Data.SQLBatchRequest.Operations) > 0 {
			firstOp := dataEvent.Data.SQLBatchRequest.Operations[0]
			if firstOp.OperationType == "query" {
				return p.handleBatchQueryRequest(event)
			}
			return p.handleBatchExecRequest(event)
		}
	}

	logger.Debug("SQL", "plugin.request event has no SQL request data")
	return nil
}

// handleLogRequest handles explicit logging requests from other plugins
func (p *Plugin) handleLogRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.RawJSON == nil {
		return nil
	}

	var logReq LogRequest
	if err := json.Unmarshal(req.Data.RawJSON, &logReq); err != nil {
		return fmt.Errorf("invalid log request: %w", err)
	}

	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	// Validate table name to prevent SQL injection
	if !isValidTableName(logReq.Table) {
		return fmt.Errorf("invalid table name: %s", logReq.Table)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert into specified table
	query := fmt.Sprintf(
		"INSERT INTO %s (plugin_name, event_type, log_data, timestamp, created_at) VALUES ($1, $2, $3, $4, $5)",
		logReq.Table,
	)

	_, err := p.pool.Exec(ctx, query,
		req.From,
		logReq.EventType,
		logReq.Data,
		time.Now(),
		time.Now(),
	)

	if err != nil {
		logger.Error("SQL", "Failed to process log request: %v", err)
		return err
	}

	// Send response if requested
	if req.ID != "" && req.ReplyTo != "" {
		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      req.ID,
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					KeyValue: map[string]string{
						"status": "success",
					},
				},
			},
		}
		if err := p.eventBus.Send(req.ReplyTo, "plugin.response", resp); err != nil {
			logger.Error("SQL", "Failed to send response: %v", err)
		}
	}

	return nil
}

// handleBatchLogRequest handles batch logging requests
func (p *Plugin) handleBatchLogRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.RawJSON == nil {
		return nil
	}

	var batchReq BatchLogRequest
	if err := json.Unmarshal(req.Data.RawJSON, &batchReq); err != nil {
		return fmt.Errorf("invalid batch log request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			logger.Error("SQL", "Failed to rollback transaction: %v", err)
		}
	}()

	for _, logReq := range batchReq.Logs {
		// Validate table name to prevent SQL injection
		if !isValidTableName(logReq.Table) {
			return fmt.Errorf("invalid table name: %s", logReq.Table)
		}

		query := fmt.Sprintf(
			"INSERT INTO %s (plugin_name, event_type, log_data, timestamp, created_at) VALUES ($1, $2, $3, $4, $5)",
			logReq.Table,
		)

		_, err := tx.Exec(ctx, query,
			req.From,
			logReq.EventType,
			logReq.Data,
			time.Now(),
			time.Now(),
		)

		if err != nil {
			return fmt.Errorf("failed to insert batch log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit batch logs: %w", err)
	}

	// Send response if requested
	if req.ID != "" && req.ReplyTo != "" {
		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      req.ID,
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					KeyValue: map[string]string{
						"status": "success",
						"count":  fmt.Sprintf("%d", len(batchReq.Logs)),
					},
				},
			},
		}
		if err := p.eventBus.Send(req.ReplyTo, "plugin.response", resp); err != nil {
			logger.Error("SQL", "Failed to send response: %v", err)
		}
	}

	return nil
}

// handleConfigureLogging handles dynamic logger configuration updates
func (p *Plugin) handleConfigureLogging(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.ConfigUpdate == nil {
		return nil
	}

	if req.Data.ConfigUpdate.Section != "logger_rules" {
		return nil
	}

	var newRules []LoggerRule
	if err := json.Unmarshal(req.Data.ConfigUpdate.Values, &newRules); err != nil {
		return fmt.Errorf("invalid logger rules: %w", err)
	}

	// Update logger rules
	p.loggerRules = newRules
	logger.Info("SQL", "Updated logger rules: %d rules", len(newRules))

	// Send response if requested
	if req.ID != "" && req.ReplyTo != "" {
		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      req.ID,
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					KeyValue: map[string]string{
						"status": "updated",
						"rules":  fmt.Sprintf("%d", len(newRules)),
					},
				},
			},
		}
		if err := p.eventBus.Send(req.ReplyTo, "plugin.response", resp); err != nil {
			logger.Error("SQL", "Failed to send response: %v", err)
		}
	}

	return nil
}

// handleSQLQuery handles SQL query requests
func (p *Plugin) handleSQLQuery(event framework.Event) error {
	// startTime := time.Now()
	// log.Printf("[SQL Plugin] handleSQLQuery called at %v", startTime)

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLQueryRequest == nil {
		logger.Warn("SQL", "Invalid SQL query event data")
		return nil
	}

	req := dataEvent.Data.SQLQueryRequest
	p.eventsHandled++

	// log.Printf("[SQL Plugin] Processing SQL query request: ID=%s, CorrelationID=%s, Query=%s at %v", req.ID, req.CorrelationID, req.Query, time.Now())

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("query").Inc()

	// Check if plugin is ready
	if !p.Ready() {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("SQL plugin not ready - database connection in progress")

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.query.failed", req.CorrelationID, req.RequestBy, "sql.query", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			logger.Debug("SQL", "Delivering error response for query CorrelationID: %s, error: %v", req.CorrelationID, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			return nil
		}
		return err
	}

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.query.failed", req.CorrelationID, req.RequestBy, "sql.query", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			logger.Debug("SQL", "Delivering error response for query CorrelationID: %s, error: %v", req.CorrelationID, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			return nil
		}
		return err
	}

	ctx := context.Background()
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Convert SQLParam to interface{} for database operation
	params := make([]interface{}, len(req.Params))
	for i, p := range req.Params {
		params[i] = p.Value
	}

	// Execute query
	// queryStartTime := time.Now()
	// log.Printf("[SQL Plugin] Starting database query at %v", queryStartTime)
	rows, err := p.db.QueryContext(ctx, req.Query, params...)
	// queryEndTime := time.Now()
	// queryDuration := queryEndTime.Sub(queryStartTime)
	// log.Printf("[SQL Plugin] Database query completed at %v (took %v)", queryEndTime, queryDuration)

	if err != nil {
		metrics.DatabaseErrors.Inc()
		logger.Error("SQL", "Query failed: %v", err)

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.query.failed", req.CorrelationID, req.RequestBy, "sql.query", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			// deliverStartTime := time.Now()
			// log.Printf("[SQL Plugin] Delivering error response for query CorrelationID: %s at %v, error: %v", req.CorrelationID, deliverStartTime, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			// log.Printf("[SQL Plugin] Error response delivered at %v (took %v)", time.Now(), time.Since(deliverStartTime))
			return nil
		}
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Convert rows to response format
	// rowProcessingStartTime := time.Now()
	// log.Printf("[SQL Plugin] Starting row processing at %v", rowProcessingStartTime)

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	var resultRows [][]json.RawMessage
	rowCount := 0
	for rows.Next() {
		rowCount++
		// Create slice of interface{} to hold column values
		values := make([]interface{}, len(columns))
		scanArgs := make([]interface{}, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert values to JSON
		row := make([]json.RawMessage, len(columns))
		for i, val := range values {
			if val == nil {
				row[i] = json.RawMessage("null")
			} else {
				// Convert database types to JSON-compatible types
				jsonCompatibleVal := convertToJSONCompatible(val)
				jsonVal, err := json.Marshal(jsonCompatibleVal)
				if err != nil {
					return fmt.Errorf("failed to marshal value: %w", err)
				}
				row[i] = jsonVal
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration error: %w", err)
	}

	// rowProcessingEndTime := time.Now()
	// rowProcessingDuration := rowProcessingEndTime.Sub(rowProcessingStartTime)
	// log.Printf("[SQL Plugin] Row processing completed at %v (took %v, processed %d rows)", rowProcessingEndTime, rowProcessingDuration, rowCount)

	// Send successful response
	if req.CorrelationID != "" {
		resp := &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				ID:            req.ID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				Columns:       columns,
				Rows:          resultRows,
			},
		}
		// deliverStartTime := time.Now()
		// log.Printf("[SQL Plugin] Delivering query response for CorrelationID: %s at %v", req.CorrelationID, deliverStartTime)
		p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
		// deliverEndTime := time.Now()
		// deliverDuration := deliverEndTime.Sub(deliverStartTime)
		// log.Printf("[SQL Plugin] Successfully delivered query response for CorrelationID: %s at %v (took %v)", req.CorrelationID, deliverEndTime, deliverDuration)

		// totalDuration := time.Since(startTime)
		// log.Printf("[SQL Plugin] Total handleSQLQuery execution time: %v", totalDuration)
		return nil
	}

	logger.Warn("SQL", "WARNING: No CorrelationID provided for query response")
	return nil
}

// handleSQLExec handles SQL exec requests
func (p *Plugin) handleSQLExec(event framework.Event) error {
	// startTime := time.Now()
	// log.Printf("[SQL Plugin] handleSQLExec called at %v", startTime)

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLExecRequest == nil {
		logger.Warn("SQL", "Invalid SQL exec event data")
		return nil
	}

	req := dataEvent.Data.SQLExecRequest
	p.eventsHandled++

	// log.Printf("[SQL Plugin] Processing SQL exec request: ID=%s, CorrelationID=%s, Query=%s at %v", req.ID, req.CorrelationID, req.Query, time.Now())

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("exec").Inc()

	// Check if plugin is ready
	if !p.Ready() {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("SQL plugin not ready - database connection in progress")

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.exec.failed", req.CorrelationID, req.RequestBy, "sql.exec", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLExecResponse: &framework.SQLExecResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			logger.Debug("SQL", "Delivering error response for exec CorrelationID: %s, error: %v", req.CorrelationID, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			return nil
		}
		return err
	}

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.exec.failed", req.CorrelationID, req.RequestBy, "sql.exec", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLExecResponse: &framework.SQLExecResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			logger.Debug("SQL", "Delivering error response for exec CorrelationID: %s, error: %v", req.CorrelationID, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			return nil
		}
		return err
	}

	ctx := context.Background()
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Convert SQLParam to interface{} for database operation
	params := make([]interface{}, len(req.Params))
	for i, p := range req.Params {
		params[i] = p.Value
	}

	// Execute statement
	// execStartTime := time.Now()
	// log.Printf("[SQL Plugin] Starting database exec at %v", execStartTime)
	result, err := p.db.ExecContext(ctx, req.Query, params...)
	// execEndTime := time.Now()
	// execDuration := execEndTime.Sub(execStartTime)
	// log.Printf("[SQL Plugin] Database exec completed at %v (took %v)", execEndTime, execDuration)

	if err != nil {
		metrics.DatabaseErrors.Inc()
		logger.Error("SQL", "Exec failed: %v", err)

		// Emit failure event for retry mechanism
		p.emitFailureEvent("sql.exec.failed", req.CorrelationID, req.RequestBy, "sql.exec", err)

		// Send error response via event
		if req.CorrelationID != "" {
			resp := &framework.EventData{
				SQLExecResponse: &framework.SQLExecResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				},
			}
			// deliverStartTime := time.Now()
			// log.Printf("[SQL Plugin] Delivering error response for exec CorrelationID: %s at %v, error: %v", req.CorrelationID, deliverStartTime, err)
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			// log.Printf("[SQL Plugin] Error response delivered at %v (took %v)", time.Now(), time.Since(deliverStartTime))
			return nil
		}
		return fmt.Errorf("exec failed: %w", err)
	}

	// Get rows affected and last insert ID
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	// Send successful response
	if req.CorrelationID != "" {
		resp := &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				ID:            req.ID,
				CorrelationID: req.CorrelationID,
				Success:       true,
				RowsAffected:  rowsAffected,
				LastInsertID:  lastInsertID,
			},
		}
		// deliverStartTime := time.Now()
		// log.Printf("[SQL Plugin] Delivering exec response for CorrelationID: %s at %v", req.CorrelationID, deliverStartTime)
		p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
		// deliverEndTime := time.Now()
		// deliverDuration := deliverEndTime.Sub(deliverStartTime)
		// log.Printf("[SQL Plugin] Successfully delivered exec response for CorrelationID: %s at %v (took %v)", req.CorrelationID, deliverEndTime, deliverDuration)

		// totalDuration := time.Since(startTime)
		// log.Printf("[SQL Plugin] Total handleSQLExec execution time: %v", totalDuration)
		return nil
	}

	logger.Warn("SQL", "WARNING: No CorrelationID provided for exec response")
	return nil
}

// LogRequest represents a logging request from a plugin
type LogRequest struct {
	EventType string          `json:"event_type"`
	Table     string          `json:"table"`
	Data      json.RawMessage `json:"data"`
}

// BatchLogRequest represents multiple log requests
type BatchLogRequest struct {
	Logs []LogRequest `json:"logs"`
}

// BatchQueryRequest represents multiple query requests (deprecated - use framework.SQLBatchRequest)
type BatchQueryRequest struct {
	Queries []framework.SQLQueryRequest `json:"queries"`
	Atomic  bool                        `json:"atomic"`
}

// BatchExecRequest represents multiple exec requests (deprecated - use framework.SQLBatchRequest)
type BatchExecRequest struct {
	Execs  []framework.SQLExecRequest `json:"execs"`
	Atomic bool                       `json:"atomic"`
}

// handleBatchQueryRequest handles batch query requests
func (p *Plugin) handleBatchQueryRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Check for new framework batch request type first
	if dataEvent.Data.SQLBatchRequest != nil {
		return p.handleFrameworkBatchRequest(dataEvent.Data.SQLBatchRequest)
	}

	// Handle legacy plugin request format for backward compatibility
	if dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.RawJSON == nil {
		return nil
	}

	var batchReq BatchQueryRequest
	if err := json.Unmarshal(req.Data.RawJSON, &batchReq); err != nil {
		return fmt.Errorf("invalid batch query request: %w", err)
	}

	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var responses []*framework.SQLQueryResponse

	if batchReq.Atomic {
		// Execute all queries in a transaction
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
				logger.Error("SQL", "Failed to rollback transaction: %v", err)
			}
		}()

		for _, queryReq := range batchReq.Queries {
			// Convert SQLParam to interface{} for database operation
			params := make([]interface{}, len(queryReq.Params))
			for i, p := range queryReq.Params {
				params[i] = p.Value
			}

			// Execute query in transaction
			rows, err := tx.Query(ctx, queryReq.Query, params...)
			if err != nil {
				// Return error response for this query
				responses = append(responses, &framework.SQLQueryResponse{
					ID:            queryReq.ID,
					CorrelationID: queryReq.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				})
				continue
			}

			// Process rows
			columns := rows.FieldDescriptions()
			columnNames := make([]string, len(columns))
			for i, col := range columns {
				columnNames[i] = string(col.Name)
			}

			var resultRows [][]json.RawMessage
			for rows.Next() {
				values, err := rows.Values()
				if err != nil {
					rows.Close()
					responses = append(responses, &framework.SQLQueryResponse{
						ID:            queryReq.ID,
						CorrelationID: queryReq.CorrelationID,
						Success:       false,
						Error:         fmt.Sprintf("failed to get values: %v", err),
					})
					continue
				}

				// Convert values to JSON
				row := make([]json.RawMessage, len(values))
				for i, val := range values {
					if val == nil {
						row[i] = json.RawMessage("null")
					} else {
						jsonCompatibleVal := convertToJSONCompatible(val)
						jsonVal, err := json.Marshal(jsonCompatibleVal)
						if err != nil {
							row[i] = json.RawMessage("null")
						} else {
							row[i] = jsonVal
						}
					}
				}
				resultRows = append(resultRows, row)
			}
			rows.Close()

			if err := rows.Err(); err != nil {
				responses = append(responses, &framework.SQLQueryResponse{
					ID:            queryReq.ID,
					CorrelationID: queryReq.CorrelationID,
					Success:       false,
					Error:         fmt.Sprintf("rows error: %v", err),
				})
				continue
			}

			// Add successful response
			responses = append(responses, &framework.SQLQueryResponse{
				ID:            queryReq.ID,
				CorrelationID: queryReq.CorrelationID,
				Success:       true,
				Columns:       columnNames,
				Rows:          resultRows,
			})
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	} else {
		// Execute queries independently
		for _, queryReq := range batchReq.Queries {
			// Convert SQLParam to interface{} for database operation
			params := make([]interface{}, len(queryReq.Params))
			for i, p := range queryReq.Params {
				params[i] = p.Value
			}

			// Execute query
			rows, err := p.db.QueryContext(ctx, queryReq.Query, params...)
			if err != nil {
				responses = append(responses, &framework.SQLQueryResponse{
					ID:            queryReq.ID,
					CorrelationID: queryReq.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				})
				continue
			}

			// Process rows
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				responses = append(responses, &framework.SQLQueryResponse{
					ID:            queryReq.ID,
					CorrelationID: queryReq.CorrelationID,
					Success:       false,
					Error:         fmt.Sprintf("failed to get columns: %v", err),
				})
				continue
			}

			var resultRows [][]json.RawMessage
			for rows.Next() {
				// Create slice of interface{} to hold column values
				values := make([]interface{}, len(columns))
				scanArgs := make([]interface{}, len(columns))
				for i := range values {
					scanArgs[i] = &values[i]
				}

				if err := rows.Scan(scanArgs...); err != nil {
					rows.Close()
					responses = append(responses, &framework.SQLQueryResponse{
						ID:            queryReq.ID,
						CorrelationID: queryReq.CorrelationID,
						Success:       false,
						Error:         fmt.Sprintf("failed to scan row: %v", err),
					})
					break
				}

				// Convert values to JSON
				row := make([]json.RawMessage, len(columns))
				for i, val := range values {
					if val == nil {
						row[i] = json.RawMessage("null")
					} else {
						jsonCompatibleVal := convertToJSONCompatible(val)
						jsonVal, err := json.Marshal(jsonCompatibleVal)
						if err != nil {
							row[i] = json.RawMessage("null")
						} else {
							row[i] = jsonVal
						}
					}
				}
				resultRows = append(resultRows, row)
			}
			rows.Close()

			if err := rows.Err(); err != nil {
				responses = append(responses, &framework.SQLQueryResponse{
					ID:            queryReq.ID,
					CorrelationID: queryReq.CorrelationID,
					Success:       false,
					Error:         fmt.Sprintf("rows error: %v", err),
				})
				continue
			}

			// Add successful response
			responses = append(responses, &framework.SQLQueryResponse{
				ID:            queryReq.ID,
				CorrelationID: queryReq.CorrelationID,
				Success:       true,
				Columns:       columns,
				Rows:          resultRows,
			})
		}
	}

	// Send response if requested
	if req.ID != "" && req.ReplyTo != "" {
		jsonResponses, err := json.Marshal(responses)
		if err != nil {
			return fmt.Errorf("failed to marshal responses: %w", err)
		}

		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      req.ID,
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: jsonResponses,
					KeyValue: map[string]string{
						"status": "success",
						"count":  fmt.Sprintf("%d", len(responses)),
					},
				},
			},
		}
		if err := p.eventBus.Send(req.ReplyTo, "plugin.response", resp); err != nil {
			logger.Error("SQL", "Failed to send response: %v", err)
		}
	}

	// Also broadcast individual responses for correlation ID matching
	for _, response := range responses {
		if response.CorrelationID != "" {
			data := &framework.EventData{
				SQLQueryResponse: response,
			}
			p.eventBus.DeliverResponse(response.CorrelationID, data, nil)
		}
	}

	return nil
}

// handleBatchExecRequest handles batch exec requests
func (p *Plugin) handleBatchExecRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Check for new framework batch request type first
	if dataEvent.Data.SQLBatchRequest != nil {
		return p.handleFrameworkBatchRequest(dataEvent.Data.SQLBatchRequest)
	}

	// Handle legacy plugin request format for backward compatibility
	if dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.RawJSON == nil {
		return nil
	}

	var batchReq BatchExecRequest
	if err := json.Unmarshal(req.Data.RawJSON, &batchReq); err != nil {
		return fmt.Errorf("invalid batch exec request: %w", err)
	}

	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var responses []*framework.SQLExecResponse

	if batchReq.Atomic {
		// Execute all execs in a transaction
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
				logger.Error("SQL", "Failed to rollback transaction: %v", err)
			}
		}()

		for _, execReq := range batchReq.Execs {
			// Track metrics
			timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
			metrics.DatabaseQueries.WithLabelValues("exec").Inc()

			// Convert SQLParam to interface{} for database operation
			params := make([]interface{}, len(execReq.Params))
			for i, p := range execReq.Params {
				params[i] = p.Value
			}

			// Execute statement in transaction
			result, err := tx.Exec(ctx, execReq.Query, params...)
			timer.ObserveDuration()

			if err != nil {
				metrics.DatabaseErrors.Inc()
				// Return error response for this exec
				responses = append(responses, &framework.SQLExecResponse{
					ID:            execReq.ID,
					CorrelationID: execReq.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				})
				continue
			}

			// Get rows affected
			rowsAffected := result.RowsAffected()

			// Add successful response
			responses = append(responses, &framework.SQLExecResponse{
				ID:            execReq.ID,
				CorrelationID: execReq.CorrelationID,
				Success:       true,
				RowsAffected:  rowsAffected,
				LastInsertID:  0, // PostgreSQL doesn't support LastInsertId
			})
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	} else {
		// Execute execs independently
		for _, execReq := range batchReq.Execs {
			// Track metrics
			timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
			metrics.DatabaseQueries.WithLabelValues("exec").Inc()

			// Convert SQLParam to interface{} for database operation
			params := make([]interface{}, len(execReq.Params))
			for i, p := range execReq.Params {
				params[i] = p.Value
			}

			// Execute statement
			result, err := p.db.ExecContext(ctx, execReq.Query, params...)
			timer.ObserveDuration()

			if err != nil {
				metrics.DatabaseErrors.Inc()
				responses = append(responses, &framework.SQLExecResponse{
					ID:            execReq.ID,
					CorrelationID: execReq.CorrelationID,
					Success:       false,
					Error:         err.Error(),
				})
				continue
			}

			// Get rows affected and last insert ID
			rowsAffected, _ := result.RowsAffected()
			lastInsertID, _ := result.LastInsertId()

			// Add successful response
			responses = append(responses, &framework.SQLExecResponse{
				ID:            execReq.ID,
				CorrelationID: execReq.CorrelationID,
				Success:       true,
				RowsAffected:  rowsAffected,
				LastInsertID:  lastInsertID,
			})
		}
	}

	// Send response if requested
	if req.ID != "" && req.ReplyTo != "" {
		jsonResponses, err := json.Marshal(responses)
		if err != nil {
			return fmt.Errorf("failed to marshal responses: %w", err)
		}

		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      req.ID,
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: jsonResponses,
					KeyValue: map[string]string{
						"status": "success",
						"count":  fmt.Sprintf("%d", len(responses)),
					},
				},
			},
		}
		if err := p.eventBus.Send(req.ReplyTo, "plugin.response", resp); err != nil {
			logger.Error("SQL", "Failed to send response: %v", err)
		}
	}

	// Also broadcast individual responses for correlation ID matching
	for _, response := range responses {
		if response.CorrelationID != "" {
			data := &framework.EventData{
				SQLExecResponse: response,
			}
			p.eventBus.DeliverResponse(response.CorrelationID, data, nil)
		}
	}

	return nil
}

// handleFrameworkBatchRequest handles the new framework SQLBatchRequest type
func (p *Plugin) handleFrameworkBatchRequest(batchReq *framework.SQLBatchRequest) error {
	if p.pool == nil {
		return fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), batchReq.Timeout)
	defer cancel()

	var results []framework.BatchOperationResult

	if batchReq.Atomic {
		// Execute all operations in a transaction
		tx, err := p.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
				logger.Error("SQL", "Failed to rollback transaction: %v", err)
			}
		}()

		for _, op := range batchReq.Operations {
			result := p.executeBatchOperationInTx(ctx, tx, op)
			results = append(results, result)
			// If one operation fails in atomic mode, stop processing
			if !result.Success && batchReq.Atomic {
				break
			}
		}

		// Only commit if all operations succeeded
		allSuccess := true
		for _, result := range results {
			if !result.Success {
				allSuccess = false
				break
			}
		}

		if allSuccess {
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("failed to commit transaction: %w", err)
			}
		}
	} else {
		// Execute operations independently
		for _, op := range batchReq.Operations {
			result := p.executeBatchOperation(ctx, op)
			results = append(results, result)
		}
	}

	// Determine overall success
	overallSuccess := true
	var overallError string
	for _, result := range results {
		if !result.Success {
			overallSuccess = false
			if overallError == "" {
				overallError = fmt.Sprintf("operation %s failed: %s", result.ID, result.Error)
			}
			break
		}
	}

	// Send response
	if batchReq.CorrelationID != "" {
		resp := &framework.EventData{
			SQLBatchResponse: &framework.SQLBatchResponse{
				ID:            batchReq.ID,
				CorrelationID: batchReq.CorrelationID,
				Success:       overallSuccess,
				Error:         overallError,
				Results:       results,
			},
		}
		p.eventBus.DeliverResponse(batchReq.CorrelationID, resp, nil)
	}

	return nil
}

// executeBatchOperation executes a single batch operation outside of a transaction
func (p *Plugin) executeBatchOperation(ctx context.Context, op framework.BatchOperation) framework.BatchOperationResult {
	// Convert SQLParam to interface{} for database operation
	params := make([]interface{}, len(op.Params))
	for i, p := range op.Params {
		params[i] = p.Value
	}

	// Use operation-specific timeout if provided
	opCtx := ctx
	if op.Timeout > 0 {
		var cancel context.CancelFunc
		opCtx, cancel = context.WithTimeout(ctx, op.Timeout)
		defer cancel()
	}

	switch op.OperationType {
	case "query":
		return p.executeBatchQuery(opCtx, op.ID, op.Query, params)
	case "exec":
		return p.executeBatchExec(opCtx, op.ID, op.Query, params)
	default:
		return framework.BatchOperationResult{
			ID:            op.ID,
			OperationType: op.OperationType,
			Success:       false,
			Error:         fmt.Sprintf("unknown operation type: %s", op.OperationType),
		}
	}
}

// executeBatchOperationInTx executes a single batch operation within a transaction
func (p *Plugin) executeBatchOperationInTx(ctx context.Context, tx pgx.Tx, op framework.BatchOperation) framework.BatchOperationResult {
	// Convert SQLParam to interface{} for database operation
	params := make([]interface{}, len(op.Params))
	for i, p := range op.Params {
		params[i] = p.Value
	}

	// Use operation-specific timeout if provided
	opCtx := ctx
	if op.Timeout > 0 {
		var cancel context.CancelFunc
		opCtx, cancel = context.WithTimeout(ctx, op.Timeout)
		defer cancel()
	}

	switch op.OperationType {
	case "query":
		return p.executeBatchQueryInTx(opCtx, tx, op.ID, op.Query, params)
	case "exec":
		return p.executeBatchExecInTx(opCtx, tx, op.ID, op.Query, params)
	default:
		return framework.BatchOperationResult{
			ID:            op.ID,
			OperationType: op.OperationType,
			Success:       false,
			Error:         fmt.Sprintf("unknown operation type: %s", op.OperationType),
		}
	}
}

// executeBatchQuery executes a query operation and returns the result
func (p *Plugin) executeBatchQuery(ctx context.Context, id, query string, params []interface{}) framework.BatchOperationResult {
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("batch_query").Inc()

	rows, err := p.db.QueryContext(ctx, query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "query",
			Success:       false,
			Error:         err.Error(),
		}
	}
	defer rows.Close()

	// Convert rows to response format
	columns, err := rows.Columns()
	if err != nil {
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "query",
			Success:       false,
			Error:         fmt.Sprintf("failed to get columns: %v", err),
		}
	}

	var resultRows [][]json.RawMessage
	for rows.Next() {
		// Create slice of interface{} to hold column values
		values := make([]interface{}, len(columns))
		scanArgs := make([]interface{}, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return framework.BatchOperationResult{
				ID:            id,
				OperationType: "query",
				Success:       false,
				Error:         fmt.Sprintf("failed to scan row: %v", err),
			}
		}

		// Convert values to JSON
		row := make([]json.RawMessage, len(columns))
		for i, val := range values {
			if val == nil {
				row[i] = json.RawMessage("null")
			} else {
				jsonCompatibleVal := convertToJSONCompatible(val)
				jsonVal, err := json.Marshal(jsonCompatibleVal)
				if err != nil {
					row[i] = json.RawMessage("null")
				} else {
					row[i] = jsonVal
				}
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "query",
			Success:       false,
			Error:         fmt.Sprintf("rows error: %v", err),
		}
	}

	return framework.BatchOperationResult{
		ID:            id,
		OperationType: "query",
		Success:       true,
		Columns:       columns,
		Rows:          resultRows,
	}
}

// executeBatchQueryInTx executes a query operation within a transaction
func (p *Plugin) executeBatchQueryInTx(ctx context.Context, tx pgx.Tx, id, query string, params []interface{}) framework.BatchOperationResult {
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("batch_query").Inc()

	rows, err := tx.Query(ctx, query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "query",
			Success:       false,
			Error:         err.Error(),
		}
	}
	defer rows.Close()

	// Process rows
	columns := rows.FieldDescriptions()
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = string(col.Name)
	}

	var resultRows [][]json.RawMessage
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return framework.BatchOperationResult{
				ID:            id,
				OperationType: "query",
				Success:       false,
				Error:         fmt.Sprintf("failed to get values: %v", err),
			}
		}

		// Convert values to JSON
		row := make([]json.RawMessage, len(values))
		for i, val := range values {
			if val == nil {
				row[i] = json.RawMessage("null")
			} else {
				jsonCompatibleVal := convertToJSONCompatible(val)
				jsonVal, err := json.Marshal(jsonCompatibleVal)
				if err != nil {
					row[i] = json.RawMessage("null")
				} else {
					row[i] = jsonVal
				}
			}
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "query",
			Success:       false,
			Error:         fmt.Sprintf("rows error: %v", err),
		}
	}

	return framework.BatchOperationResult{
		ID:            id,
		OperationType: "query",
		Success:       true,
		Columns:       columnNames,
		Rows:          resultRows,
	}
}

// executeBatchExec executes an exec operation and returns the result
func (p *Plugin) executeBatchExec(ctx context.Context, id, query string, params []interface{}) framework.BatchOperationResult {
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("batch_exec").Inc()

	result, err := p.db.ExecContext(ctx, query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "exec",
			Success:       false,
			Error:         err.Error(),
		}
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	return framework.BatchOperationResult{
		ID:            id,
		OperationType: "exec",
		Success:       true,
		RowsAffected:  rowsAffected,
		LastInsertID:  lastInsertID,
	}
}

// executeBatchExecInTx executes an exec operation within a transaction
func (p *Plugin) executeBatchExecInTx(ctx context.Context, tx pgx.Tx, id, query string, params []interface{}) framework.BatchOperationResult {
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("batch_exec").Inc()

	result, err := tx.Exec(ctx, query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
		return framework.BatchOperationResult{
			ID:            id,
			OperationType: "exec",
			Success:       false,
			Error:         err.Error(),
		}
	}

	rowsAffected := result.RowsAffected()

	return framework.BatchOperationResult{
		ID:            id,
		OperationType: "exec",
		Success:       true,
		RowsAffected:  rowsAffected,
		LastInsertID:  0, // PostgreSQL doesn't support LastInsertId
	}
}
