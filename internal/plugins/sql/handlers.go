package sql

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

// handlePluginRequest handles targeted plugin requests sent via eventBus.Request
func (p *Plugin) handlePluginRequest(event framework.Event) error {
	// This handler receives all "plugin.request" events and routes them based on metadata
	// For now, we'll just pass through to the appropriate handler based on the event data
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Check what type of request this is and route accordingly
	if dataEvent.Data.SQLExecRequest != nil {
		return p.handleSQLExec(event)
	}
	if dataEvent.Data.SQLQueryRequest != nil {
		return p.handleSQLQuery(event)
	}

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
		log.Printf("[SQL Plugin] Failed to process log request: %v", err)
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
			log.Printf("[SQL Plugin] Failed to send response: %v", err)
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
			log.Printf("[SQL Plugin] Failed to rollback transaction: %v", err)
		}
	}()

	for _, logReq := range batchReq.Logs {
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
			log.Printf("[SQL Plugin] Failed to send response: %v", err)
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
	log.Printf("[SQL Plugin] Updated logger rules: %d rules", len(newRules))

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
			log.Printf("[SQL Plugin] Failed to send response: %v", err)
		}
	}

	return nil
}

// handleSQLQuery handles SQL query requests
func (p *Plugin) handleSQLQuery(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLQueryRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLQueryRequest
	p.eventsHandled++

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("query").Inc()

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")
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
	rows, err := p.db.QueryContext(ctx, req.Query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
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
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
			return nil
		}
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	// Convert rows to response format
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
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
			return fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert values to JSON
		row := make([]json.RawMessage, len(columns))
		for i, val := range values {
			if val == nil {
				row[i] = json.RawMessage("null")
			} else {
				jsonVal, err := json.Marshal(val)
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
		p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
		return nil
	}

	return nil
}

// handleSQLExec handles SQL exec requests
func (p *Plugin) handleSQLExec(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLExecRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLExecRequest
	p.eventsHandled++

	// Debug logging removed - request/response flow is working correctly

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("exec").Inc()

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")
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
	result, err := p.db.ExecContext(ctx, req.Query, params...)
	if err != nil {
		metrics.DatabaseErrors.Inc()
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
			p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
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
		p.eventBus.DeliverResponse(req.CorrelationID, resp, nil)
		return nil
	}

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

// BatchQueryRequest represents multiple query requests
type BatchQueryRequest struct {
	Queries []framework.SQLQueryRequest `json:"queries"`
	Atomic  bool                        `json:"atomic"`
}

// handleBatchQueryRequest handles batch query requests
func (p *Plugin) handleBatchQueryRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
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
				log.Printf("[SQL Plugin] Failed to rollback transaction: %v", err)
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
						jsonVal, err := json.Marshal(val)
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
						jsonVal, err := json.Marshal(val)
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
			log.Printf("[SQL Plugin] Failed to send response: %v", err)
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
