// Package batchops demonstrates how to use SQL batch operations in Daz plugins.
// This example shows both batch queries and batch executions, with both atomic
// and non-atomic modes, as well as comprehensive error handling.
package batchops

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/plugins/sql"
)

// Plugin demonstrates batch SQL operations
type Plugin struct {
	name     string
	eventBus framework.EventBus
}

// NewPlugin creates a new batch operations example plugin
func NewPlugin() *Plugin {
	return &Plugin{
		name: "batchops-example",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

// Init initializes the plugin
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	// Initialize our example tables on startup
	if err := p.initializeTables(); err != nil {
		return fmt.Errorf("failed to initialize tables: %w", err)
	}

	log.Printf("[%s] Initialized - type !batchdemo in chat to see examples", p.name)
	return nil
}

func (p *Plugin) Start() error {
	// Subscribe to chat messages to trigger demos
	err := p.eventBus.Subscribe("cytube.event.chatMsg", p.handleChatMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to chat: %w", err)
	}

	log.Printf("[%s] Started", p.name)
	return nil
}

func (p *Plugin) Stop() error {
	log.Printf("[%s] Stopped", p.name)
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:  p.name,
		State: "running",
	}
}

// HandleEvent implements the framework.Plugin interface
func (p *Plugin) HandleEvent(event framework.Event) error {
	// This plugin handles events through specific subscriptions
	return nil
}

// initializeTables creates example tables for demonstrating batch operations
func (p *Plugin) initializeTables() error {
	// Create example tables using individual exec requests
	tables := []string{
		`CREATE TABLE IF NOT EXISTS batch_users (
			id SERIAL PRIMARY KEY,
			username VARCHAR(255) UNIQUE NOT NULL,
			email VARCHAR(255),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS batch_products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			price DECIMAL(10,2),
			stock INT DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS batch_orders (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES batch_users(id),
			product_id INT REFERENCES batch_products(id),
			quantity INT NOT NULL,
			total_price DECIMAL(10,2),
			order_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS batch_logs (
			id SERIAL PRIMARY KEY,
			event_type VARCHAR(100),
			message TEXT,
			metadata JSONB,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, createTable := range tables {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		correlationID := uuid.New().String()
		request := &framework.EventData{
			SQLExecRequest: &framework.SQLExecRequest{
				ID:            uuid.New().String(),
				CorrelationID: correlationID,
				Query:         createTable,
				Timeout:       5 * time.Second,
				RequestBy:     p.name,
			},
		}

		response, err := p.eventBus.Request(ctx, "sql", "sql.exec", request, nil)
		if err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}

		if response.SQLExecResponse == nil || !response.SQLExecResponse.Success {
			errMsg := "unknown error"
			if response.SQLExecResponse != nil && response.SQLExecResponse.Error != "" {
				errMsg = response.SQLExecResponse.Error
			}
			return fmt.Errorf("failed to create table: %s", errMsg)
		}
	}

	log.Printf("[%s] Tables initialized successfully", p.name)
	return nil
}

func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	if dataEvent.Data.ChatMessage.Message != "!batchdemo" {
		return nil
	}

	log.Printf("[%s] Running batch operations demo", p.name)

	// Run all examples
	examples := []struct {
		name string
		fn   func() error
	}{
		{"Batch Query - Read Multiple Tables", p.demonstrateBatchQuery},
		{"Batch Exec - Bulk Insert", p.demonstrateBatchExec},
		{"Atomic Batch Operations", p.demonstrateAtomicBatch},
		{"Non-Atomic Batch with Error Handling", p.demonstrateNonAtomicBatch},
		{"Mixed Batch Operations", p.demonstrateMixedBatch},
	}

	for _, example := range examples {
		log.Printf("[%s] Running example: %s", p.name, example.name)
		if err := example.fn(); err != nil {
			log.Printf("[%s] Example failed: %s - %v", p.name, example.name, err)
		} else {
			log.Printf("[%s] Example completed: %s", p.name, example.name)
		}
	}

	return nil
}

// demonstrateBatchQuery shows how to read from multiple tables in a single batch
func (p *Plugin) demonstrateBatchQuery() error {
	// Prepare batch query request to read from multiple tables
	batchReq := sql.BatchQueryRequest{
		Queries: []framework.SQLQueryRequest{
			{
				ID:        uuid.New().String(),
				Query:     "SELECT id, username, email FROM batch_users ORDER BY id LIMIT 5",
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "SELECT id, name, price, stock FROM batch_products WHERE stock > $1 ORDER BY price",
				Params:    []framework.SQLParam{framework.NewSQLParam(0)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "SELECT COUNT(*) as total_orders, SUM(total_price) as revenue FROM batch_orders",
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: false, // Non-atomic - each query runs independently
	}

	// Marshal the batch request
	reqData, err := json.Marshal(batchReq)
	if err != nil {
		return fmt.Errorf("failed to marshal batch query request: %w", err)
	}

	// Send batch query request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   uuid.New().String(),
			From: p.name,
			To:   "sql",
			Type: "batch_query",
			Data: &framework.RequestData{
				RawJSON: reqData,
			},
			ReplyTo: p.name,
		},
	}

	// Use Request method for synchronous response
	response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
	if err != nil {
		return fmt.Errorf("failed to send batch query request: %w", err)
	}

	if response.PluginResponse == nil {
		return fmt.Errorf("invalid response format")
	}

	if !response.PluginResponse.Success {
		return fmt.Errorf("batch query failed: %s", response.PluginResponse.Error)
	}

	// Parse the batch responses
	var responses []*framework.SQLQueryResponse
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &responses); err != nil {
		return fmt.Errorf("failed to parse batch responses: %w", err)
	}

	// Log results from each query
	for i, resp := range responses {
		if resp.Success {
			log.Printf("[%s] Query %d returned %d rows", p.name, i+1, len(resp.Rows))
		} else {
			log.Printf("[%s] Query %d failed: %s", p.name, i+1, resp.Error)
		}
	}

	return nil
}

// demonstrateBatchExec shows how to perform bulk inserts
func (p *Plugin) demonstrateBatchExec() error {
	// Prepare batch exec request for bulk inserts
	batchReq := sql.BatchExecRequest{
		Execs: []framework.SQLExecRequest{
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_users (username, email) VALUES ($1, $2) ON CONFLICT (username) DO NOTHING",
				Params:    []framework.SQLParam{framework.NewSQLParam("alice"), framework.NewSQLParam("alice@example.com")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_users (username, email) VALUES ($1, $2) ON CONFLICT (username) DO NOTHING",
				Params:    []framework.SQLParam{framework.NewSQLParam("bob"), framework.NewSQLParam("bob@example.com")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_products (name, price, stock) VALUES ($1, $2, $3)",
				Params:    []framework.SQLParam{framework.NewSQLParam("Widget"), framework.NewSQLParam(19.99), framework.NewSQLParam(100)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_products (name, price, stock) VALUES ($1, $2, $3)",
				Params:    []framework.SQLParam{framework.NewSQLParam("Gadget"), framework.NewSQLParam(29.99), framework.NewSQLParam(50)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: true, // Atomic - all inserts in a single transaction
	}

	// Marshal the batch request
	reqData, err := json.Marshal(batchReq)
	if err != nil {
		return fmt.Errorf("failed to marshal batch exec request: %w", err)
	}

	// Send batch exec request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   uuid.New().String(),
			From: p.name,
			To:   "sql",
			Type: "batch_exec",
			Data: &framework.RequestData{
				RawJSON: reqData,
			},
			ReplyTo: p.name,
		},
	}

	// Use Request method for synchronous response
	response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
	if err != nil {
		return fmt.Errorf("failed to send batch exec request: %w", err)
	}

	if response.PluginResponse == nil {
		return fmt.Errorf("invalid response format")
	}

	if !response.PluginResponse.Success {
		return fmt.Errorf("batch exec failed: %s", response.PluginResponse.Error)
	}

	// Parse the batch responses
	var responses []*framework.SQLExecResponse
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &responses); err != nil {
		return fmt.Errorf("failed to parse batch responses: %w", err)
	}

	// Log results from each exec
	totalRowsAffected := int64(0)
	for i, resp := range responses {
		if resp.Success {
			log.Printf("[%s] Exec %d affected %d rows", p.name, i+1, resp.RowsAffected)
			totalRowsAffected += resp.RowsAffected
		} else {
			log.Printf("[%s] Exec %d failed: %s", p.name, i+1, resp.Error)
		}
	}
	log.Printf("[%s] Total rows affected: %d", p.name, totalRowsAffected)

	return nil
}

// demonstrateAtomicBatch shows atomic batch operations that all succeed or all fail
func (p *Plugin) demonstrateAtomicBatch() error {
	// Simulate an order transaction - all operations must succeed
	batchReq := sql.BatchExecRequest{
		Execs: []framework.SQLExecRequest{
			// 1. Create a new order
			{
				ID: uuid.New().String(),
				Query: `INSERT INTO batch_orders (user_id, product_id, quantity, total_price) 
				        VALUES (
				            (SELECT id FROM batch_users WHERE username = $1),
				            (SELECT id FROM batch_products WHERE name = $2),
				            $3,
				            $4
				        )`,
				Params:    []framework.SQLParam{framework.NewSQLParam("alice"), framework.NewSQLParam("Widget"), framework.NewSQLParam(2), framework.NewSQLParam(39.98)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			// 2. Update product stock
			{
				ID:        uuid.New().String(),
				Query:     "UPDATE batch_products SET stock = stock - $1 WHERE name = $2 AND stock >= $1",
				Params:    []framework.SQLParam{framework.NewSQLParam(2), framework.NewSQLParam("Widget")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			// 3. Log the transaction
			{
				ID:    uuid.New().String(),
				Query: "INSERT INTO batch_logs (event_type, message, metadata) VALUES ($1, $2, $3)",
				Params: []framework.SQLParam{
					framework.NewSQLParam("order_placed"),
					framework.NewSQLParam("Order placed for 2 Widgets"),
					framework.NewSQLParam(json.RawMessage(`{"user":"alice","product":"Widget","quantity":2}`)),
				},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: true, // All or nothing - if one operation fails, all are rolled back
	}

	// Send and process the atomic batch
	return p.sendBatchExec(batchReq, "atomic batch transaction")
}

// demonstrateNonAtomicBatch shows non-atomic operations with individual error handling
func (p *Plugin) demonstrateNonAtomicBatch() error {
	// Mix of operations where some might fail but others should succeed
	batchReq := sql.BatchExecRequest{
		Execs: []framework.SQLExecRequest{
			// This should succeed
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_logs (event_type, message) VALUES ($1, $2)",
				Params:    []framework.SQLParam{framework.NewSQLParam("test_success"), framework.NewSQLParam("This should succeed")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			// This will fail due to constraint violation (negative stock)
			{
				ID:        uuid.New().String(),
				Query:     "UPDATE batch_products SET stock = -10 WHERE name = $1",
				Params:    []framework.SQLParam{framework.NewSQLParam("Widget")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			// This should still succeed despite previous failure
			{
				ID:        uuid.New().String(),
				Query:     "INSERT INTO batch_logs (event_type, message) VALUES ($1, $2)",
				Params:    []framework.SQLParam{framework.NewSQLParam("test_after_error"), framework.NewSQLParam("This executes after an error")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: false, // Non-atomic - each operation is independent
	}

	// Send and process the non-atomic batch
	return p.sendBatchExec(batchReq, "non-atomic batch with errors")
}

// demonstrateMixedBatch shows a batch with both queries and execs
func (p *Plugin) demonstrateMixedBatch() error {
	// First, use batch query to get data
	queryBatch := sql.BatchQueryRequest{
		Queries: []framework.SQLQueryRequest{
			{
				ID:        uuid.New().String(),
				Query:     "SELECT id, username FROM batch_users WHERE username IN ($1, $2)",
				Params:    []framework.SQLParam{framework.NewSQLParam("alice"), framework.NewSQLParam("bob")},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:        uuid.New().String(),
				Query:     "SELECT id, name, stock FROM batch_products WHERE stock < $1",
				Params:    []framework.SQLParam{framework.NewSQLParam(20)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: false,
	}

	// Send batch query first
	if err := p.sendBatchQuery(queryBatch, "mixed batch queries"); err != nil {
		return fmt.Errorf("query phase failed: %w", err)
	}

	// Then use batch exec to update based on query results
	execBatch := sql.BatchExecRequest{
		Execs: []framework.SQLExecRequest{
			{
				ID:        uuid.New().String(),
				Query:     "UPDATE batch_products SET stock = stock + $1 WHERE stock < $2",
				Params:    []framework.SQLParam{framework.NewSQLParam(50), framework.NewSQLParam(20)},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
			{
				ID:    uuid.New().String(),
				Query: "INSERT INTO batch_logs (event_type, message, metadata) VALUES ($1, $2, $3)",
				Params: []framework.SQLParam{
					framework.NewSQLParam("stock_replenished"),
					framework.NewSQLParam("Low stock items replenished"),
					framework.NewSQLParam(json.RawMessage(`{"added_stock":50,"threshold":20}`)),
				},
				Timeout:   5 * time.Second,
				RequestBy: p.name,
			},
		},
		Atomic: true,
	}

	// Send batch exec
	return p.sendBatchExec(execBatch, "mixed batch execs")
}

// Helper function to send batch query requests
func (p *Plugin) sendBatchQuery(batchReq sql.BatchQueryRequest, description string) error {
	reqData, err := json.Marshal(batchReq)
	if err != nil {
		return fmt.Errorf("failed to marshal batch query: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   uuid.New().String(),
			From: p.name,
			To:   "sql",
			Type: "batch_query",
			Data: &framework.RequestData{
				RawJSON: reqData,
			},
			ReplyTo: p.name,
		},
	}

	response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
	if err != nil {
		return fmt.Errorf("failed to send %s: %w", description, err)
	}

	if response.PluginResponse == nil {
		return fmt.Errorf("invalid response format for %s", description)
	}

	if !response.PluginResponse.Success {
		return fmt.Errorf("%s failed: %s", description, response.PluginResponse.Error)
	}

	log.Printf("[%s] %s completed successfully", p.name, description)
	return nil
}

// Helper function to send batch exec requests
func (p *Plugin) sendBatchExec(batchReq sql.BatchExecRequest, description string) error {
	reqData, err := json.Marshal(batchReq)
	if err != nil {
		return fmt.Errorf("failed to marshal batch exec: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   uuid.New().String(),
			From: p.name,
			To:   "sql",
			Type: "batch_exec",
			Data: &framework.RequestData{
				RawJSON: reqData,
			},
			ReplyTo: p.name,
		},
	}

	response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
	if err != nil {
		return fmt.Errorf("failed to send %s: %w", description, err)
	}

	if response.PluginResponse == nil {
		return fmt.Errorf("invalid response format for %s", description)
	}

	// For non-atomic operations, we might have partial success
	if batchReq.Atomic && !response.PluginResponse.Success {
		return fmt.Errorf("%s failed (atomic): %s", description, response.PluginResponse.Error)
	}

	// Parse individual responses to show which succeeded/failed
	var responses []*framework.SQLExecResponse
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &responses); err != nil {
		log.Printf("[%s] Warning: couldn't parse individual responses: %v", p.name, err)
	} else {
		successCount := 0
		for i, resp := range responses {
			if resp.Success {
				successCount++
			} else {
				log.Printf("[%s] Operation %d in %s failed: %s", p.name, i+1, description, resp.Error)
			}
		}
		log.Printf("[%s] %s: %d/%d operations succeeded", p.name, description, successCount, len(responses))
	}

	return nil
}
