package framework

import (
	"time"
)

// DataEvent is an event type that can carry EventData
// This allows us to pass data through the event system without modifying the core EventBus
type DataEvent struct {
	EventType string
	EventTime time.Time
	Data      *EventData
}

func (e *DataEvent) Type() string {
	return e.EventType
}

func (e *DataEvent) Timestamp() time.Time {
	return e.EventTime
}

// NewDataEvent creates a new DataEvent with the given type and data
func NewDataEvent(eventType string, data *EventData) *DataEvent {
	return &DataEvent{
		EventType: eventType,
		EventTime: time.Now(),
		Data:      data,
	}
}
