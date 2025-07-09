package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// EventBus metrics
	EventsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_eventbus_events_processed_total",
			Help: "Total number of events processed by the event bus",
		},
		[]string{"event_type"},
	)

	EventsDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_eventbus_events_dropped_total",
			Help: "Total number of events dropped due to full buffers",
		},
		[]string{"event_type"},
	)

	EventProcessingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "daz_eventbus_event_processing_duration_seconds",
			Help:    "Duration of event processing in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"event_type"},
	)

	// Plugin metrics
	PluginStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "daz_plugin_status",
			Help: "Current status of plugins (1=running, 0=stopped)",
		},
		[]string{"plugin_name"},
	)

	PluginEventsHandled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_plugin_events_handled_total",
			Help: "Total number of events handled by each plugin",
		},
		[]string{"plugin_name"},
	)

	PluginErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_plugin_errors_total",
			Help: "Total number of errors encountered by plugins",
		},
		[]string{"plugin_name"},
	)

	PluginUptime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "daz_plugin_uptime_seconds",
			Help: "Uptime of each plugin in seconds",
		},
		[]string{"plugin_name"},
	)

	// Database metrics
	DatabaseQueries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_database_queries_total",
			Help: "Total number of database queries executed",
		},
		[]string{"query_type"}, // "query" or "exec"
	)

	DatabaseErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "daz_database_errors_total",
			Help: "Total number of database errors",
		},
	)

	DatabaseQueryDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "daz_database_query_duration_seconds",
			Help:    "Duration of database queries in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Cytube connection metrics
	CytubeConnectionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "daz_cytube_connection_status",
			Help: "Status of Cytube connection (1=connected, 0=disconnected)",
		},
	)

	CytubeReconnects = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "daz_cytube_reconnects_total",
			Help: "Total number of Cytube reconnection attempts",
		},
	)

	CytubeMessagesSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "daz_cytube_messages_sent_total",
			Help: "Total number of messages sent to Cytube",
		},
	)

	CytubeMessagesReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "daz_cytube_messages_received_total",
			Help: "Total number of messages received from Cytube",
		},
	)

	// Health check metrics
	HealthCheckRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_health_check_requests_total",
			Help: "Total number of health check requests",
		},
		[]string{"endpoint", "status"},
	)

	// General application metrics
	BotUptime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "daz_bot_uptime_seconds",
			Help: "Uptime of the bot in seconds",
		},
	)
)

// Collector wraps metrics collection functionality
type Collector struct {
	registry *prometheus.Registry
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		registry: prometheus.DefaultRegisterer.(*prometheus.Registry),
	}
}

// UpdatePluginMetrics updates metrics for a specific plugin
func UpdatePluginMetrics(pluginName string, running bool, eventsHandled int64, errors int64, uptimeSeconds float64) {
	if running {
		PluginStatus.WithLabelValues(pluginName).Set(1)
	} else {
		PluginStatus.WithLabelValues(pluginName).Set(0)
	}

	PluginEventsHandled.WithLabelValues(pluginName).Add(float64(eventsHandled))
	PluginErrors.WithLabelValues(pluginName).Add(float64(errors))
	PluginUptime.WithLabelValues(pluginName).Set(uptimeSeconds)
}

// UpdateEventBusMetrics updates EventBus-related metrics
func UpdateEventBusMetrics(droppedCounts map[string]int64) {
	for eventType, count := range droppedCounts {
		EventsDropped.WithLabelValues(eventType).Add(float64(count))
	}
}
