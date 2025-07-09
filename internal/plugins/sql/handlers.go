package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

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
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLRequest
	p.eventsHandled++

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("query").Inc()

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")
		if req.IsSync && req.ResponseCh != "" {
			// Use type assertion to access DeliverQueryResponse
			if eventBus, ok := p.eventBus.(interface {
				DeliverQueryResponse(string, *sql.Rows, error)
			}); ok {
				eventBus.DeliverQueryResponse(req.ResponseCh, nil, err)
			}
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
		if req.IsSync && req.ResponseCh != "" {
			// Use type assertion to access DeliverQueryResponse
			if eventBus, ok := p.eventBus.(interface {
				DeliverQueryResponse(string, *sql.Rows, error)
			}); ok {
				eventBus.DeliverQueryResponse(req.ResponseCh, nil, err)
			}
		}
		return fmt.Errorf("query failed: %w", err)
	}

	// For sync queries, deliver the rows directly
	if req.IsSync && req.ResponseCh != "" {
		// Use type assertion to access DeliverQueryResponse
		if eventBus, ok := p.eventBus.(interface {
			DeliverQueryResponse(string, *sql.Rows, error)
		}); ok {
			eventBus.DeliverQueryResponse(req.ResponseCh, rows, nil)
		}
		return nil
	}

	// For async queries, we'd need to process and return results differently
	// For now, just close the rows
	if err := rows.Close(); err != nil {
		log.Printf("[SQL Plugin] Failed to close rows: %v", err)
	}

	return nil
}

// handleSQLExec handles SQL exec requests
func (p *Plugin) handleSQLExec(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.SQLRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLRequest
	p.eventsHandled++

	// Track metrics
	timer := prometheus.NewTimer(metrics.DatabaseQueryDuration)
	defer timer.ObserveDuration()
	metrics.DatabaseQueries.WithLabelValues("exec").Inc()

	if p.pool == nil {
		metrics.DatabaseErrors.Inc()
		err := fmt.Errorf("database not connected")
		if req.IsSync && req.ResponseCh != "" {
			// Use type assertion to access DeliverExecResponse
			if eventBus, ok := p.eventBus.(interface {
				DeliverExecResponse(string, sql.Result, error)
			}); ok {
				eventBus.DeliverExecResponse(req.ResponseCh, nil, err)
			}
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
		if req.IsSync && req.ResponseCh != "" {
			// Use type assertion to access DeliverExecResponse
			if eventBus, ok := p.eventBus.(interface {
				DeliverExecResponse(string, sql.Result, error)
			}); ok {
				eventBus.DeliverExecResponse(req.ResponseCh, nil, err)
			}
		}
		return fmt.Errorf("exec failed: %w", err)
	}

	// For sync requests, deliver the result
	if req.IsSync && req.ResponseCh != "" {
		// Use type assertion to access DeliverExecResponse
		if eventBus, ok := p.eventBus.(interface {
			DeliverExecResponse(string, sql.Result, error)
		}); ok {
			eventBus.DeliverExecResponse(req.ResponseCh, result, nil)
		}
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
