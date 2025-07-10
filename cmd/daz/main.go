package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hildolfr/daz/internal/config"
	"github.com/hildolfr/daz/internal/core"
	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/health"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/hildolfr/daz/internal/plugins/analytics"
	"github.com/hildolfr/daz/internal/plugins/commands/about"
	"github.com/hildolfr/daz/internal/plugins/commands/debug"
	"github.com/hildolfr/daz/internal/plugins/commands/help"
	"github.com/hildolfr/daz/internal/plugins/commands/uptime"
	"github.com/hildolfr/daz/internal/plugins/eventfilter"
	"github.com/hildolfr/daz/internal/plugins/mediatracker"
	"github.com/hildolfr/daz/internal/plugins/sql"
	"github.com/hildolfr/daz/internal/plugins/usertracker"
	"github.com/hildolfr/daz/pkg/eventbus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Define command-line flags
	configFile := flag.String("config", "config.json", "Path to configuration file")
	channel := flag.String("channel", "", "Cytube channel to join")
	username := flag.String("username", "", "Username for authentication")
	password := flag.String("password", "", "Password for authentication")
	dbHost := flag.String("db-host", "", "PostgreSQL host")
	dbPort := flag.Int("db-port", 0, "PostgreSQL port")
	dbName := flag.String("db-name", "", "Database name")
	dbUser := flag.String("db-user", "", "Database user")
	dbPass := flag.String("db-pass", "", "Database password")
	healthPort := flag.Int("health-port", 8080, "Port for health check endpoints")
	flag.Parse()

	// Load configuration
	var cfg *config.Config
	var err error

	// Always use LoadFromFile which handles env vars
	cfg, err = config.LoadFromFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Merge command-line flags with config (flags take precedence)
	cfg.MergeWithFlags(*channel, *username, *password, *dbHost, *dbPort, *dbName, *dbUser, *dbPass)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	fmt.Println("Daz - Modular Go Chat Bot for Cytube")

	// Display room information
	enabledRooms := 0
	for _, room := range cfg.Core.Rooms {
		if room.Enabled {
			enabledRooms++
			// Use channel name if ID is empty (it will be auto-generated later)
			identifier := room.ID
			if identifier == "" {
				identifier = room.Channel
			}
			fmt.Printf("Room '%s': Joining channel %s", identifier, room.Channel)
			if room.Username != "" {
				fmt.Printf(" as %s", room.Username)
			}
			fmt.Println()
		}
	}
	fmt.Printf("Managing %d room(s) with WebSocket transport and PostgreSQL persistence\n", enabledRooms)

	// Create core plugin configuration from loaded config
	coreConfig := &core.Config{
		Rooms: make([]core.RoomConfig, len(cfg.Core.Rooms)),
	}
	for i, room := range cfg.Core.Rooms {
		coreConfig.Rooms[i] = core.RoomConfig{
			ID:                room.ID,
			Channel:           room.Channel,
			Username:          room.Username,
			Password:          room.Password,
			Enabled:           room.Enabled,
			ReconnectAttempts: room.ReconnectAttempts,
			CooldownMinutes:   room.CooldownMinutes,
		}
	}

	if err := run(coreConfig, cfg, *healthPort); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
}

func run(coreConfig *core.Config, cfg *config.Config, healthPort int) error {
	// Track bot uptime
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			metrics.BotUptime.Set(time.Since(startTime).Seconds())
		}
	}()

	// Create the event bus with configuration
	eventBusConfig := &eventbus.Config{
		BufferSizes: cfg.EventBus.BufferSizes,
	}
	// Apply defaults for any missing buffer sizes
	if eventBusConfig.BufferSizes == nil {
		eventBusConfig.BufferSizes = make(map[string]int)
	}
	defaults := map[string]int{
		"cytube.event":              5000,
		"sql.":                      100,
		"plugin.":                   100,
		"plugin.request":            200,
		eventbus.EventPluginCommand: 100,
	}
	for key, value := range defaults {
		if _, exists := eventBusConfig.BufferSizes[key]; !exists {
			eventBusConfig.BufferSizes[key] = value
		}
	}
	bus := eventbus.NewEventBus(eventBusConfig)

	// Start the event bus
	if err := bus.Start(); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}
	defer func() {
		if err := bus.Stop(); err != nil {
			log.Printf("Error stopping event bus: %v", err)
		}
	}()

	// Create and initialize the core plugin
	corePlugin := core.NewPlugin(coreConfig)

	log.Println("Initializing core plugin...")
	if err := corePlugin.Initialize(bus); err != nil {
		return fmt.Errorf("failed to initialize core plugin: %w", err)
	}

	// Create plugin manager
	pluginManager := framework.NewPluginManager()

	// Create plugins and store in array for health service
	plugins := []struct {
		name   string
		plugin framework.Plugin
	}{
		{"sql", sql.NewPlugin()},
		{"eventfilter", eventfilter.New()},
		{"usertracker", usertracker.New()},
		{"mediatracker", mediatracker.New()},
		{"analytics", analytics.New()},
		{"about", about.New()},
		{"debug", debug.New()},
		{"help", help.New()},
		{"uptime", uptime.New()},
	}

	// Register all plugins with the manager
	for _, p := range plugins {
		pluginManager.RegisterPlugin(p.name, p.plugin)
	}

	// Prepare plugin configurations
	pluginConfigs := make(map[string]interface{})
	pluginConfigs["sql"] = cfg.GetPluginConfig("sql")
	pluginConfigs["eventfilter"] = cfg.GetPluginConfig("eventfilter")
	pluginConfigs["usertracker"] = cfg.GetPluginConfig("usertracker")
	pluginConfigs["mediatracker"] = cfg.GetPluginConfig("mediatracker")
	pluginConfigs["analytics"] = cfg.GetPluginConfig("analytics")
	pluginConfigs["about"] = cfg.GetPluginConfig("about")
	pluginConfigs["help"] = cfg.GetPluginConfig("help")
	pluginConfigs["uptime"] = cfg.GetPluginConfig("uptime")

	// Initialize all plugins (respects dependencies)
	if err := pluginManager.InitializeAll(pluginConfigs, bus); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}

	// Start core plugin first
	log.Println("Starting core plugin...")
	if err := corePlugin.Start(); err != nil {
		return fmt.Errorf("failed to start core plugin: %w", err)
	}
	defer func() {
		if err := corePlugin.Stop(); err != nil {
			log.Printf("Error stopping core plugin: %v", err)
		}
	}()

	// Start all plugins (respects dependencies and readiness)
	if err := pluginManager.StartAll(); err != nil {
		return fmt.Errorf("failed to start plugins: %w", err)
	}
	defer pluginManager.StopAll()

	// Now that all plugins are ready, start room connections
	log.Println("All plugins are ready, starting room connections...")
	corePlugin.StartRoomConnections()

	// Create health check service
	healthService := health.NewService(bus)

	// Register core plugin
	healthService.RegisterPlugin(corePlugin)

	// Register all other plugins
	for _, p := range plugins {
		healthService.RegisterPlugin(p.plugin)
	}

	// Register database health checker
	dbChecker := health.NewDatabaseChecker(bus)
	healthService.RegisterChecker("database", dbChecker)

	// Start periodic metrics updater for plugins
	go updatePluginMetrics(plugins, corePlugin)

	// Start periodic EventBus metrics updater
	go updateEventBusMetrics(bus)

	// Start health check HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", wrapMetrics("health", healthService.Handler()))
	mux.HandleFunc("/health/live", wrapMetrics("health_live", healthService.LivenessHandler()))
	mux.HandleFunc("/health/ready", wrapMetrics("health_ready", healthService.ReadinessHandler()))

	// Add Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	healthServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", healthPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start health server in background
	go func() {
		log.Printf("Starting health check server on port %d", healthPort)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Health check server error: %v", err)
		}
	}()

	// Defer health server shutdown
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := healthServer.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down health server: %v", err)
		}
	}()

	log.Println("Bot is running! Press Ctrl+C to stop.")
	log.Printf("Health check endpoints available at http://localhost:%d/health", healthPort)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)

	return nil
}

// wrapMetrics wraps an HTTP handler to track metrics
func wrapMetrics(endpoint string, handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Create a custom response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Call the actual handler
		handler(wrapped, r)

		// Track metrics
		metrics.HealthCheckRequests.WithLabelValues(endpoint, fmt.Sprintf("%d", wrapped.statusCode)).Inc()
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// updatePluginMetrics periodically updates Prometheus metrics for all plugins
func updatePluginMetrics(plugins []struct {
	name   string
	plugin framework.Plugin
}, corePlugin framework.Plugin) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// All plugins including core
	allPlugins := make([]framework.Plugin, 0, len(plugins)+1)
	allPlugins = append(allPlugins, corePlugin)
	for _, p := range plugins {
		allPlugins = append(allPlugins, p.plugin)
	}

	for range ticker.C {
		for _, plugin := range allPlugins {
			status := plugin.Status()

			// Determine if plugin is running
			running := status.State == "running"

			// Calculate uptime in seconds
			uptimeSeconds := status.Uptime.Seconds()

			// For now, we don't track incremental events/errors, just set current values
			metrics.UpdatePluginMetrics(plugin.Name(), running, 0, 0, uptimeSeconds)
		}
	}
}

// updateEventBusMetrics periodically updates EventBus metrics
func updateEventBusMetrics(bus framework.EventBus) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastCounts := make(map[string]int64)

	for range ticker.C {
		// Get current dropped event counts
		currentCounts := bus.GetDroppedEventCounts()

		// Calculate deltas and update metrics
		deltas := make(map[string]int64)
		for eventType, count := range currentCounts {
			if lastCount, exists := lastCounts[eventType]; exists {
				delta := count - lastCount
				if delta > 0 {
					deltas[eventType] = delta
				}
			} else if count > 0 {
				// First time seeing this event type
				deltas[eventType] = count
			}
		}

		// Update metrics with deltas
		if len(deltas) > 0 {
			metrics.UpdateEventBusMetrics(deltas)
		}

		// Store current counts for next iteration
		lastCounts = currentCounts
	}
}
