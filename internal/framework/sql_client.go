package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SQLClient provides a convenient interface for event-based SQL operations
type SQLClient struct {
	eventBus  EventBus
	source    string
	sqlHelper *SQLRequestHelper
}

// NewSQLClient creates a new SQL client
func NewSQLClient(eventBus EventBus, source string) *SQLClient {
	return &SQLClient{
		eventBus:  eventBus,
		source:    source,
		sqlHelper: NewSQLRequestHelper(eventBus, source),
	}
}

// ExecContext executes a SQL statement with context
func (c *SQLClient) ExecContext(ctx context.Context, query string, args ...interface{}) (int64, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.ExecWithTimeout(ctx, query, timeout, args...)
	}

	// Use normal exec if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalExec(ctx, query, args...)
}

// Exec executes a SQL statement (convenience method)
func (c *SQLClient) Exec(query string, args ...interface{}) error {
	ctx := context.Background()

	// Use slow exec for convenience method (30s timeout, 2 retries)
	_, err := c.sqlHelper.SlowExec(ctx, query, args...)
	return err
}

// QueryContext executes a SQL query with context
func (c *SQLClient) QueryContext(ctx context.Context, query string, args ...interface{}) (*QueryRows, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.QueryWithTimeout(ctx, query, timeout, args...)
	}

	// Use normal query if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalQuery(ctx, query, args...)
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
