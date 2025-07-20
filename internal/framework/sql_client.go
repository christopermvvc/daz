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
		// Try to handle numeric values that might be stored as strings
		if err := json.Unmarshal(val, dest[i]); err != nil {
			// If unmarshaling fails, check if we're trying to unmarshal a string into a numeric type
			var strVal string
			if json.Unmarshal(val, &strVal) == nil {
				// Successfully unmarshaled as string, now check destination type
				switch dest[i].(type) {
				case *int64, *int32, *int16, *int8, *int:
					// Try to parse string as integer
					var intVal int64
					if _, parseErr := fmt.Sscanf(strVal, "%d", &intVal); parseErr == nil {
						// Successfully parsed, now unmarshal the integer
						intBytes, _ := json.Marshal(intVal)
						if unmarshalErr := json.Unmarshal(intBytes, dest[i]); unmarshalErr == nil {
							continue // Success, move to next column
						}
					}
				case *float64, *float32:
					// Try to parse string as float
					var floatVal float64
					if _, parseErr := fmt.Sscanf(strVal, "%f", &floatVal); parseErr == nil {
						// Successfully parsed, now unmarshal the float
						floatBytes, _ := json.Marshal(floatVal)
						if unmarshalErr := json.Unmarshal(floatBytes, dest[i]); unmarshalErr == nil {
							continue // Success, move to next column
						}
					}
				}
			}
			// If all conversion attempts fail, return the original error
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

// BatchQuery executes multiple queries in a batch (non-atomic)
func (c *SQLClient) BatchQuery(queries []BatchOperation) (*SQLBatchResponse, error) {
	ctx := context.Background()
	return c.sqlHelper.NormalBatch(ctx, queries, false)
}

// BatchExec executes multiple exec operations in a batch (non-atomic)
func (c *SQLClient) BatchExec(operations []BatchOperation) (*SQLBatchResponse, error) {
	ctx := context.Background()
	return c.sqlHelper.NormalBatch(ctx, operations, false)
}

// BatchQueryAtomic executes multiple queries in a single transaction
func (c *SQLClient) BatchQueryAtomic(queries []BatchOperation) (*SQLBatchResponse, error) {
	ctx := context.Background()
	return c.sqlHelper.NormalBatch(ctx, queries, true)
}

// BatchExecAtomic executes multiple exec operations in a single transaction
func (c *SQLClient) BatchExecAtomic(operations []BatchOperation) (*SQLBatchResponse, error) {
	ctx := context.Background()
	return c.sqlHelper.NormalBatch(ctx, operations, true)
}

// BatchQueryContext executes multiple queries with context (non-atomic)
func (c *SQLClient) BatchQueryContext(ctx context.Context, queries []BatchOperation) (*SQLBatchResponse, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.BatchWithTimeout(ctx, queries, timeout, false)
	}

	// Use normal batch if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalBatch(ctx, queries, false)
}

// BatchExecContext executes multiple exec operations with context (non-atomic)
func (c *SQLClient) BatchExecContext(ctx context.Context, operations []BatchOperation) (*SQLBatchResponse, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.BatchWithTimeout(ctx, operations, timeout, false)
	}

	// Use normal batch if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalBatch(ctx, operations, false)
}

// BatchQueryAtomicContext executes multiple queries in a single transaction with context
func (c *SQLClient) BatchQueryAtomicContext(ctx context.Context, queries []BatchOperation) (*SQLBatchResponse, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.BatchWithTimeout(ctx, queries, timeout, true)
	}

	// Use normal batch if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalBatch(ctx, queries, true)
}

// BatchExecAtomicContext executes multiple exec operations in a single transaction with context
func (c *SQLClient) BatchExecAtomicContext(ctx context.Context, operations []BatchOperation) (*SQLBatchResponse, error) {
	// Determine timeout from context and use appropriate helper method
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		return c.sqlHelper.BatchWithTimeout(ctx, operations, timeout, true)
	}

	// Use normal batch if no deadline is set (15s timeout, 3 retries)
	return c.sqlHelper.NormalBatch(ctx, operations, true)
}

// BatchOperationBuilder provides a fluent interface for building batch operations
type BatchOperationBuilder struct {
	operations []BatchOperation
}

// NewBatchOperationBuilder creates a new batch operation builder
func NewBatchOperationBuilder() *BatchOperationBuilder {
	return &BatchOperationBuilder{
		operations: make([]BatchOperation, 0),
	}
}

// AddQuery adds a query operation to the batch
func (b *BatchOperationBuilder) AddQuery(query string, args ...interface{}) *BatchOperationBuilder {
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	b.operations = append(b.operations, BatchOperation{
		ID:            fmt.Sprintf("query-%d-%d", len(b.operations), time.Now().UnixNano()),
		OperationType: "query",
		Query:         query,
		Params:        params,
	})
	return b
}

// AddExec adds an exec operation to the batch
func (b *BatchOperationBuilder) AddExec(query string, args ...interface{}) *BatchOperationBuilder {
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	b.operations = append(b.operations, BatchOperation{
		ID:            fmt.Sprintf("exec-%d-%d", len(b.operations), time.Now().UnixNano()),
		OperationType: "exec",
		Query:         query,
		Params:        params,
	})
	return b
}

// Build returns the built batch operations
func (b *BatchOperationBuilder) Build() []BatchOperation {
	return b.operations
}
