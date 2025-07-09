package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Plugin interface {
	Init(config json.RawMessage, bus EventBus) error
	Start() error
	Stop() error
	HandleEvent(event Event) error
	Status() PluginStatus
	Name() string
}

type PluginStatus struct {
	Name          string
	State         string
	LastError     error
	RetryCount    int
	EventsHandled int64
	Uptime        time.Duration
}

type Event interface {
	Type() string
	Timestamp() time.Time
}

type EventBus interface {
	// Core broadcasting with metadata support
	Broadcast(eventType string, data *EventData) error
	BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error

	// Direct plugin communication
	Send(target string, eventType string, data *EventData) error
	SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error

	// Request/Response pattern
	Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error)
	DeliverResponse(correlationID string, response *EventData, err error)

	// SQL operations (legacy)
	Query(sql string, params ...SQLParam) (QueryResult, error)
	Exec(sql string, params ...SQLParam) error
	QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error)
	ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error)

	// Event subscription
	Subscribe(eventType string, handler EventHandler) error
	SetSQLHandlers(queryHandler, execHandler EventHandler)

	// Plugin lifecycle
	RegisterPlugin(name string, plugin Plugin) error
	UnregisterPlugin(name string) error

	// Metrics
	GetDroppedEventCounts() map[string]int64
	GetDroppedEventCount(eventType string) int64
}

type EventHandler func(event Event) error

type QueryResult interface {
	Scan(dest ...interface{}) error
	Next() bool
	Close() error
}
