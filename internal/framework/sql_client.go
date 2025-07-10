package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SQLClient provides a convenient interface for event-based SQL operations
type SQLClient struct {
	eventBus EventBus
	source   string
}

// NewSQLClient creates a new SQL client
func NewSQLClient(eventBus EventBus, source string) *SQLClient {
	return &SQLClient{
		eventBus: eventBus,
		source:   source,
	}
}

// ExecContext executes a SQL statement with context
func (c *SQLClient) ExecContext(ctx context.Context, query string, args ...interface{}) (int64, error) {
	// Convert args to SQLParam
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	// Generate correlation ID
	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())

	// Create exec request
	request := &EventData{
		SQLExecRequest: &SQLExecRequest{
			ID:            correlationID,
			CorrelationID: correlationID,
			Query:         query,
			Params:        params,
			Timeout:       30 * time.Second,
			RequestBy:     c.source,
		},
	}

	// Create metadata
	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        c.source,
		Target:        "sql",
	}

	// Send request and wait for response
	// Note: This sends to the sql plugin directly, not via broadcast
	response, err := c.eventBus.Request(ctx, "sql", "sql.exec.request", request, metadata)
	if err != nil {
		return 0, fmt.Errorf("exec request failed: %w", err)
	}

	// Check response
	if response != nil && response.SQLExecResponse != nil {
		if !response.SQLExecResponse.Success {
			return 0, fmt.Errorf("exec failed: %s", response.SQLExecResponse.Error)
		}
		return response.SQLExecResponse.RowsAffected, nil
	}

	return 0, fmt.Errorf("no response received")
}

// Exec executes a SQL statement (convenience method)
func (c *SQLClient) Exec(query string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := c.ExecContext(ctx, query, args...)
	return err
}

// QueryContext executes a SQL query with context
func (c *SQLClient) QueryContext(ctx context.Context, query string, args ...interface{}) (*QueryRows, error) {
	// Convert args to SQLParam
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	// Generate correlation ID
	correlationID := fmt.Sprintf("%s-%d", c.source, time.Now().UnixNano())

	// Create query request
	request := &EventData{
		SQLQueryRequest: &SQLQueryRequest{
			ID:            correlationID,
			CorrelationID: correlationID,
			Query:         query,
			Params:        params,
			Timeout:       30 * time.Second,
			RequestBy:     c.source,
		},
	}

	// Create metadata
	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        c.source,
		Target:        "sql",
	}

	// Send request and wait for response
	response, err := c.eventBus.Request(ctx, "sql", "sql.query.request", request, metadata)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}

	// Check response
	if response != nil && response.SQLQueryResponse != nil {
		if !response.SQLQueryResponse.Success {
			return nil, fmt.Errorf("query failed: %s", response.SQLQueryResponse.Error)
		}
		return &QueryRows{
			columns: response.SQLQueryResponse.Columns,
			rows:    response.SQLQueryResponse.Rows,
			current: -1,
		}, nil
	}

	return nil, fmt.Errorf("no response received")
}

// QuerySync performs a synchronous query (compatibility method)
func (c *SQLClient) QuerySync(ctx context.Context, query string, args ...interface{}) (*QueryRows, error) {
	return c.QueryContext(ctx, query, args...)
}

// ExecSync performs a synchronous exec (compatibility method)
func (c *SQLClient) ExecSync(ctx context.Context, query string, args ...interface{}) (int64, error) {
	return c.ExecContext(ctx, query, args...)
}

// QueryRows represents the result of a query
type QueryRows struct {
	columns []string
	rows    [][]json.RawMessage
	current int
	closed  bool
}

// Next advances to the next row
func (r *QueryRows) Next() bool {
	if r.closed {
		return false
	}
	r.current++
	return r.current < len(r.rows)
}

// Scan scans the current row into dest
func (r *QueryRows) Scan(dest ...interface{}) error {
	if r.closed {
		return fmt.Errorf("rows closed")
	}
	if r.current < 0 || r.current >= len(r.rows) {
		return fmt.Errorf("no row available")
	}

	row := r.rows[r.current]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count mismatch: got %d, want %d", len(dest), len(row))
	}

	for i, val := range row {
		if err := json.Unmarshal(val, dest[i]); err != nil {
			return fmt.Errorf("failed to unmarshal column %d: %w", i, err)
		}
	}

	return nil
}

// Close closes the rows
func (r *QueryRows) Close() error {
	r.closed = true
	return nil
}

// Columns returns the column names
func (r *QueryRows) Columns() ([]string, error) {
	if r.closed {
		return nil, fmt.Errorf("rows closed")
	}
	return r.columns, nil
}

// Err returns any error that occurred during iteration
func (r *QueryRows) Err() error {
	return nil
}
