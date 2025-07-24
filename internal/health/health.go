package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
)

// Status represents the health status of a component
type Status string

const (
	StatusUp       Status = "UP"
	StatusDown     Status = "DOWN"
	StatusDegraded Status = "DEGRADED"
)

// HealthDetails contains specific health check details
type HealthDetails struct {
	State          string           `json:"state,omitempty"`
	EventsHandled  int64            `json:"events_handled,omitempty"`
	RetryCount     int              `json:"retry_count,omitempty"`
	Uptime         string           `json:"uptime,omitempty"`
	ResponseTimeMS int64            `json:"response_time_ms,omitempty"`
	DroppedEvents  int64            `json:"dropped_events_total,omitempty"`
	Warning        string           `json:"warning,omitempty"`
	DroppedByType  map[string]int64 `json:"dropped_by_type,omitempty"`
}

// ComponentHealth represents the health of a single component
type ComponentHealth struct {
	Name      string         `json:"name"`
	Status    Status         `json:"status"`
	Details   *HealthDetails `json:"details,omitempty"`
	Error     string         `json:"error,omitempty"`
	CheckTime time.Time      `json:"check_time"`
}

// HealthStatus represents the overall system health
type HealthStatus struct {
	Status     Status                     `json:"status"`
	Timestamp  time.Time                  `json:"timestamp"`
	Uptime     string                     `json:"uptime"`
	Components map[string]ComponentHealth `json:"components"`
}

// LivenessResponse represents the liveness check response
type LivenessResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// ReadinessResponse represents the readiness check response
type ReadinessResponse struct {
	Ready     bool   `json:"ready"`
	Status    Status `json:"status,omitempty"`
	Timestamp string `json:"timestamp"`
}

// Checker interface for components that can report health
type Checker interface {
	HealthCheck(ctx context.Context) ComponentHealth
}

// Service manages health checks for the system
type Service struct {
	startTime time.Time
	plugins   []framework.Plugin
	checkers  map[string]Checker
	eventBus  framework.EventBus
	mu        sync.RWMutex
}

// NewService creates a new health check service
func NewService(eventBus framework.EventBus) *Service {
	return &Service{
		startTime: time.Now(),
		plugins:   make([]framework.Plugin, 0),
		checkers:  make(map[string]Checker),
		eventBus:  eventBus,
	}
}

// RegisterPlugin registers a plugin for health checking
func (s *Service) RegisterPlugin(plugin framework.Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plugins = append(s.plugins, plugin)
}

// RegisterChecker registers a custom health checker
func (s *Service) RegisterChecker(name string, checker Checker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkers[name] = checker
}

// GetHealth returns the current health status
func (s *Service) GetHealth(ctx context.Context, detailed bool) HealthStatus {
	status := HealthStatus{
		Timestamp:  time.Now(),
		Uptime:     time.Since(s.startTime).Round(time.Second).String(),
		Components: make(map[string]ComponentHealth),
	}

	// Check all registered plugins
	s.mu.RLock()
	plugins := make([]framework.Plugin, len(s.plugins))
	copy(plugins, s.plugins)
	checkers := make(map[string]Checker)
	for k, v := range s.checkers {
		checkers[k] = v
	}
	s.mu.RUnlock()

	// Collect plugin statuses
	overallStatus := StatusUp
	for _, plugin := range plugins {
		pluginStatus := plugin.Status()
		componentHealth := ComponentHealth{
			Name:      plugin.Name(),
			CheckTime: time.Now(),
			Details:   &HealthDetails{},
		}

		// Determine component status based on plugin state
		switch pluginStatus.State {
		case "running":
			componentHealth.Status = StatusUp
		case "stopped", "error":
			componentHealth.Status = StatusDown
			overallStatus = StatusDegraded
		case "reconnecting", "initialized":
			componentHealth.Status = StatusDegraded
			if overallStatus == StatusUp {
				overallStatus = StatusDegraded
			}
		default:
			componentHealth.Status = StatusDegraded
			if overallStatus == StatusUp {
				overallStatus = StatusDegraded
			}
		}

		// Add details if requested
		if detailed {
			componentHealth.Details.State = pluginStatus.State
			componentHealth.Details.EventsHandled = pluginStatus.EventsHandled
			componentHealth.Details.RetryCount = pluginStatus.RetryCount
			if pluginStatus.Uptime > 0 {
				componentHealth.Details.Uptime = pluginStatus.Uptime.String()
			}
		}

		// Update Prometheus metrics
		running := pluginStatus.State == "running"
		uptimeSeconds := pluginStatus.Uptime.Seconds()
		metrics.UpdatePluginMetrics(plugin.Name(), running, 0, 0, uptimeSeconds)

		// Add error if present
		if pluginStatus.LastError != nil {
			componentHealth.Error = pluginStatus.LastError.Error()
			componentHealth.Status = StatusDown
			overallStatus = StatusDegraded
		}

		status.Components[plugin.Name()] = componentHealth
	}

	// Check custom health checkers
	for name, checker := range checkers {
		health := checker.HealthCheck(ctx)
		status.Components[name] = health

		if health.Status == StatusDown {
			overallStatus = StatusDegraded
		} else if health.Status == StatusDegraded && overallStatus == StatusUp {
			overallStatus = StatusDegraded
		}
	}

	// Check EventBus health
	eventBusHealth := s.checkEventBus()
	status.Components["eventbus"] = eventBusHealth
	if eventBusHealth.Status != StatusUp && overallStatus == StatusUp {
		overallStatus = StatusDegraded
	}

	status.Status = overallStatus
	return status
}

// checkEventBus checks the health of the event bus
func (s *Service) checkEventBus() ComponentHealth {
	health := ComponentHealth{
		Name:      "eventbus",
		CheckTime: time.Now(),
		Status:    StatusUp,
		Details:   &HealthDetails{},
	}

	// Get dropped event counts
	droppedCounts := s.eventBus.GetDroppedEventCounts()
	var totalDropped int64
	for _, count := range droppedCounts {
		totalDropped += count
	}

	health.Details.DroppedEvents = totalDropped

	// If too many events are being dropped, mark as degraded
	if totalDropped > 1000 {
		health.Status = StatusDegraded
		health.Details.Warning = "High number of dropped events"
	}

	// Add top dropped event types
	if len(droppedCounts) > 0 {
		topDropped := make(map[string]int64)
		for eventType, count := range droppedCounts {
			if count > 0 {
				topDropped[eventType] = count
			}
		}
		if len(topDropped) > 0 {
			health.Details.DroppedByType = topDropped
		}
	}

	return health
}

// DatabaseChecker checks database connectivity
type DatabaseChecker struct {
	eventBus framework.EventBus
}

// NewDatabaseChecker creates a new database health checker
func NewDatabaseChecker(eventBus framework.EventBus) *DatabaseChecker {
	return &DatabaseChecker{eventBus: eventBus}
}

// HealthCheck checks database health
func (d *DatabaseChecker) HealthCheck(ctx context.Context) ComponentHealth {
	health := ComponentHealth{
		Name:      "database",
		CheckTime: time.Now(),
		Status:    StatusUp,
		Details:   &HealthDetails{},
	}

	// Use the new SQL request helper with retry logic for critical health checks
	start := time.Now()
	sqlHelper := framework.NewSQLRequestHelper(d.eventBus, "health-check")

	// Try a simple query using the critical request helper (5 retries, 45s timeout)
	rows, err := sqlHelper.CriticalQuery(ctx, "SELECT 1")
	if err != nil {
		health.Status = StatusDown
		health.Error = fmt.Sprintf("connection check failed: %v", err)
		return health
	}

	// Check if we got a result
	if rows != nil {
		defer func() {
			_ = rows.Close()
		}()
		if !rows.Next() {
			health.Status = StatusDown
			health.Error = "query returned no results"
			return health
		}

		var result int
		if err := rows.Scan(&result); err != nil {
			health.Status = StatusDown
			health.Error = fmt.Sprintf("failed to scan result: %v", err)
			return health
		}

		if result != 1 {
			health.Status = StatusDown
			health.Error = fmt.Sprintf("unexpected result: got %d, expected 1", result)
			return health
		}
	}

	health.Details.ResponseTimeMS = time.Since(start).Milliseconds()
	return health
}

// Handler returns an HTTP handler for health checks
func (s *Service) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Determine level of detail requested
		detailed := r.URL.Query().Get("detailed") == "true"

		// Get health status
		health := s.GetHealth(ctx, detailed)

		// Set response headers
		w.Header().Set("Content-Type", "application/json")

		// Set appropriate status code
		switch health.Status {
		case StatusUp:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK) // Still return 200 for degraded
		case StatusDown:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		// Encode response
		if err := json.NewEncoder(w).Encode(health); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

// LivenessHandler returns a simple liveness check handler
func (s *Service) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Simple check - if we can handle requests, we're alive
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := LivenessResponse{
			Status:    "alive",
			Timestamp: time.Now().Format(time.RFC3339),
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			// Log error but don't change status since we already wrote the header
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

// ReadinessHandler returns a readiness check handler
func (s *Service) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		health := s.GetHealth(ctx, false)

		w.Header().Set("Content-Type", "application/json")

		// Only ready if status is UP
		if health.Status == StatusUp {
			w.WriteHeader(http.StatusOK)
			response := ReadinessResponse{
				Ready:     true,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				// Log error but don't change status since we already wrote the header
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			}
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			response := ReadinessResponse{
				Ready:     false,
				Status:    health.Status,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			if err := json.NewEncoder(w).Encode(response); err != nil {
				// Log error but don't change status since we already wrote the header
				http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			}
		}
	}
}
