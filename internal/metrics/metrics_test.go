package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewCollector(t *testing.T) {
	collector := NewCollector()
	if collector == nil {
		t.Fatal("Expected non-nil collector")
		return
	}
	if collector.registry == nil {
		t.Error("Expected non-nil registry")
	}
}

func TestUpdatePluginMetrics(t *testing.T) {
	// Test running plugin
	UpdatePluginMetrics("test-plugin", true, 100, 5, 3600.5)

	// Check plugin status
	status := testutil.ToFloat64(PluginStatus.WithLabelValues("test-plugin"))
	if status != 1 {
		t.Errorf("Expected plugin status 1, got %f", status)
	}

	// Test stopped plugin
	UpdatePluginMetrics("test-plugin", false, 0, 0, 0)
	status = testutil.ToFloat64(PluginStatus.WithLabelValues("test-plugin"))
	if status != 0 {
		t.Errorf("Expected plugin status 0, got %f", status)
	}

	// Check uptime
	uptime := testutil.ToFloat64(PluginUptime.WithLabelValues("test-plugin"))
	if uptime != 0 {
		t.Errorf("Expected uptime 0, got %f", uptime)
	}
}

func TestUpdateEventBusMetrics(t *testing.T) {
	// Reset metrics for clean test
	EventsDropped.Reset()

	droppedCounts := map[string]int64{
		"cytube.event": 10,
		"plugin.event": 5,
	}

	UpdateEventBusMetrics(droppedCounts)

	// Check cytube events
	cytubeDropped := testutil.ToFloat64(EventsDropped.WithLabelValues("cytube.event"))
	if cytubeDropped != 10 {
		t.Errorf("Expected 10 dropped cytube events, got %f", cytubeDropped)
	}

	// Check plugin events
	pluginDropped := testutil.ToFloat64(EventsDropped.WithLabelValues("plugin.event"))
	if pluginDropped != 5 {
		t.Errorf("Expected 5 dropped plugin events, got %f", pluginDropped)
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Test that metrics are properly registered
	metrics := []struct {
		name   string
		metric prometheus.Collector
	}{
		{"EventsProcessed", EventsProcessed},
		{"EventsDropped", EventsDropped},
		{"EventProcessingDuration", EventProcessingDuration},
		{"PluginStatus", PluginStatus},
		{"PluginEventsHandled", PluginEventsHandled},
		{"PluginErrors", PluginErrors},
		{"PluginUptime", PluginUptime},
		{"DatabaseQueries", DatabaseQueries},
		{"DatabaseErrors", DatabaseErrors},
		{"DatabaseQueryDuration", DatabaseQueryDuration},
		{"CytubeConnectionStatus", CytubeConnectionStatus},
		{"CytubeReconnects", CytubeReconnects},
		{"CytubeMessagesSent", CytubeMessagesSent},
		{"CytubeMessagesReceived", CytubeMessagesReceived},
		{"HealthCheckRequests", HealthCheckRequests},
		{"BotUptime", BotUptime},
	}

	for _, m := range metrics {
		if m.metric == nil {
			t.Errorf("Metric %s is nil", m.name)
		}
	}
}

func TestCounterIncrement(t *testing.T) {
	// Reset for clean test
	CytubeReconnects.Add(0) // This ensures the metric is initialized

	initialValue := testutil.ToFloat64(CytubeReconnects)
	CytubeReconnects.Inc()
	newValue := testutil.ToFloat64(CytubeReconnects)

	if newValue != initialValue+1 {
		t.Errorf("Expected counter to increment by 1, got %f -> %f", initialValue, newValue)
	}
}

func TestGaugeSet(t *testing.T) {
	testValue := 42.5
	BotUptime.Set(testValue)

	value := testutil.ToFloat64(BotUptime)
	if value != testValue {
		t.Errorf("Expected gauge value %f, got %f", testValue, value)
	}
}

func TestDatabaseMetrics(t *testing.T) {
	// Test query counter
	DatabaseQueries.WithLabelValues("query").Inc()
	queries := testutil.ToFloat64(DatabaseQueries.WithLabelValues("query"))
	if queries < 1 {
		t.Errorf("Expected at least 1 query, got %f", queries)
	}

	// Test exec counter
	DatabaseQueries.WithLabelValues("exec").Inc()
	execs := testutil.ToFloat64(DatabaseQueries.WithLabelValues("exec"))
	if execs < 1 {
		t.Errorf("Expected at least 1 exec, got %f", execs)
	}

	// Test error counter
	initialErrors := testutil.ToFloat64(DatabaseErrors)
	DatabaseErrors.Inc()
	newErrors := testutil.ToFloat64(DatabaseErrors)
	if newErrors != initialErrors+1 {
		t.Errorf("Expected errors to increment by 1, got %f -> %f", initialErrors, newErrors)
	}
}

func TestHealthCheckMetrics(t *testing.T) {
	// Test health check requests
	HealthCheckRequests.WithLabelValues("/health", "200").Inc()
	HealthCheckRequests.WithLabelValues("/health/ready", "503").Inc()

	healthRequests := testutil.ToFloat64(HealthCheckRequests.WithLabelValues("/health", "200"))
	if healthRequests < 1 {
		t.Errorf("Expected at least 1 health request, got %f", healthRequests)
	}

	readyRequests := testutil.ToFloat64(HealthCheckRequests.WithLabelValues("/health/ready", "503"))
	if readyRequests < 1 {
		t.Errorf("Expected at least 1 ready request, got %f", readyRequests)
	}
}

func TestCytubeMetrics(t *testing.T) {
	// Test connection status
	CytubeConnectionStatus.Set(1)
	status := testutil.ToFloat64(CytubeConnectionStatus)
	if status != 1 {
		t.Errorf("Expected connection status 1, got %f", status)
	}

	CytubeConnectionStatus.Set(0)
	status = testutil.ToFloat64(CytubeConnectionStatus)
	if status != 0 {
		t.Errorf("Expected connection status 0, got %f", status)
	}

	// Test message counters
	initialSent := testutil.ToFloat64(CytubeMessagesSent)
	CytubeMessagesSent.Inc()
	newSent := testutil.ToFloat64(CytubeMessagesSent)
	if newSent != initialSent+1 {
		t.Errorf("Expected messages sent to increment by 1, got %f -> %f", initialSent, newSent)
	}

	initialReceived := testutil.ToFloat64(CytubeMessagesReceived)
	CytubeMessagesReceived.Inc()
	newReceived := testutil.ToFloat64(CytubeMessagesReceived)
	if newReceived != initialReceived+1 {
		t.Errorf("Expected messages received to increment by 1, got %f -> %f", initialReceived, newReceived)
	}
}
