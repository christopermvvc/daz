package framework

import (
	"encoding/json"
	"time"
)

type Plugin interface {
	Init(config json.RawMessage, bus EventBus) error
	Start() error
	Stop() error
	HandleEvent(event Event) error
	Status() PluginStatus
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
	Broadcast(eventType string, data *EventData) error
	Send(target string, eventType string, data *EventData) error
	Query(sql string, params ...interface{}) (QueryResult, error)
	Exec(sql string, params ...interface{}) error
	Subscribe(eventType string, handler EventHandler) error
}

type EventHandler func(event Event) error

type QueryResult interface {
	Scan(dest ...interface{}) error
	Next() bool
	Close() error
}
