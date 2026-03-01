package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hildolfr/daz/internal/config"
	"github.com/hildolfr/daz/internal/core"
	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/health"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/hildolfr/daz/internal/plugins/analytics"
	"github.com/hildolfr/daz/internal/plugins/commands/about"
	"github.com/hildolfr/daz/internal/plugins/commands/bong"
	"github.com/hildolfr/daz/internal/plugins/commands/clap"
	"github.com/hildolfr/daz/internal/plugins/commands/fortune"
	"github.com/hildolfr/daz/internal/plugins/commands/games"
	"github.com/hildolfr/daz/internal/plugins/commands/help"
	"github.com/hildolfr/daz/internal/plugins/commands/insult"
	"github.com/hildolfr/daz/internal/plugins/commands/ping"
	"github.com/hildolfr/daz/internal/plugins/commands/quote"
	"github.com/hildolfr/daz/internal/plugins/commands/random"
	"github.com/hildolfr/daz/internal/plugins/commands/remind"
	"github.com/hildolfr/daz/internal/plugins/commands/seen"
	"github.com/hildolfr/daz/internal/plugins/commands/signspinning"
	"github.com/hildolfr/daz/internal/plugins/commands/tell"
	"github.com/hildolfr/daz/internal/plugins/commands/uptime"
	"github.com/hildolfr/daz/internal/plugins/commands/weather"
	"github.com/hildolfr/daz/internal/plugins/economy"
	"github.com/hildolfr/daz/internal/plugins/eventfilter"
	"github.com/hildolfr/daz/internal/plugins/gallery"
	"github.com/hildolfr/daz/internal/plugins/greeter"
	"github.com/hildolfr/daz/internal/plugins/mediatracker"
	"github.com/hildolfr/daz/internal/plugins/ollama"
	"github.com/hildolfr/daz/internal/plugins/playlist"
	"github.com/hildolfr/daz/internal/plugins/retry"
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
	verbose := flag.Bool("verbose", false, "Enable verbose/debug logging")
	flag.Parse()

	// Configure logging
	if *verbose {
		logger.SetDebug(true)
		logger.SetStartupVerbose(true)
	}

	// Load configuration
	var cfg *config.Config
	var err error

	// Always use LoadFromFile which handles env vars
	cfg, err = config.LoadFromFile(*configFile)
	if err != nil {
		logger.Error("Main", "Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// Merge command-line flags with config (flags take precedence)
	cfg.MergeWithFlags(*channel, *username, *password, *dbHost, *dbPort, *dbName, *dbUser, *dbPass)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		logger.Error("Main", "Invalid configuration: %v", err)
		os.Exit(1)
	}

	// Track startup time
	startTime := time.Now()

	// Display startup header
	fmt.Printf("\n🤖 Daz - Modular Chat Bot v1.0.0\n")

	// Collect room information for concise display
	enabledRooms := 0
	var roomNames []string
	for _, room := range cfg.Core.Rooms {
		if room.Enabled {
			enabledRooms++
			roomNames = append(roomNames, room.Channel)
		}
	}

	// Show room information in verbose mode
	if logger.IsStartupVerbose() {
		for _, room := range cfg.Core.Rooms {
			if room.Enabled {
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
	}

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

	if err := run(coreConfig, cfg, *healthPort, startTime, roomNames); err != nil {
		logger.Error("Main", "Failed to start: %v", err)
		os.Exit(1)
	}
}

func run(coreConfig *core.Config, cfg *config.Config, healthPort int, startTime time.Time, roomNames []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track bot uptime
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				metrics.BotUptime.Set(time.Since(startTime).Seconds())
			}
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
			logger.Error("Main", "Error stopping event bus: %v", err)
		}
	}()

	// Create and initialize the core plugin
	corePlugin := core.NewPlugin(coreConfig)

	logger.Debug("Main", "Initializing core plugin...")
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
		{"retry", retry.NewPlugin()},
		{"eventfilter", eventfilter.New()},
		{"usertracker", usertracker.New()},
		{"mediatracker", mediatracker.New()},
		{"analytics", analytics.New()},
		{"economy", economy.New()},
		{"greeter", greeter.New()},
		{"gallery", gallery.New()},
		{"about", about.New()},
		{"bong", bong.New()},
		{"clap", clap.New()},
		{"fortune", fortune.New()},
		{"games", games.New()},
		{"help", help.New()},
		{"insult", insult.New()},
		{"ping", ping.New()},
		{"quote", quote.New()},
		{"uptime", uptime.New()},
		{"weather", weather.New()},
		{"random", random.New()},
		{"remind", remind.New()},
		{"tell", tell.New()},
		{"seen", seen.New()},
		{"signspinning", signspinning.New()},
		{"playlist", playlist.New()},
		{"ollama", ollama.New()},
	}

	// Register all plugins with the manager
	for _, p := range plugins {
		if err := pluginManager.RegisterPlugin(p.name, p.plugin); err != nil {
			logger.Error("Main", "Failed to register plugin %s: %v", p.name, err)
			os.Exit(1)
		}
	}

	// Prepare plugin configurations
	pluginConfigs := make(map[string]json.RawMessage)
	pluginConfigs["sql"] = cfg.GetPluginConfig("sql")
	pluginConfigs["retry"] = cfg.GetPluginConfig("retry")
	pluginConfigs["eventfilter"] = cfg.GetPluginConfig("eventfilter")
	pluginConfigs["usertracker"] = cfg.GetPluginConfig("usertracker")
	pluginConfigs["mediatracker"] = cfg.GetPluginConfig("mediatracker")
	pluginConfigs["analytics"] = cfg.GetPluginConfig("analytics")
	pluginConfigs["about"] = cfg.GetPluginConfig("about")
	pluginConfigs["bong"] = cfg.GetPluginConfig("bong")
	pluginConfigs["clap"] = cfg.GetPluginConfig("clap")
	pluginConfigs["insult"] = cfg.GetPluginConfig("insult")
	pluginConfigs["ping"] = cfg.GetPluginConfig("ping")
	pluginConfigs["fortune"] = cfg.GetPluginConfig("fortune")
	pluginConfigs["games"] = cfg.GetPluginConfig("games")
	pluginConfigs["help"] = cfg.GetPluginConfig("help")
	pluginConfigs["quote"] = cfg.GetPluginConfig("quote")
	pluginConfigs["uptime"] = cfg.GetPluginConfig("uptime")
	pluginConfigs["tell"] = cfg.GetPluginConfig("tell")
	pluginConfigs["economy"] = cfg.GetPluginConfig("economy")
	pluginConfigs["greeter"] = cfg.GetPluginConfig("greeter")
	pluginConfigs["weather"] = cfg.GetPluginConfig("weather")
	pluginConfigs["random"] = cfg.GetPluginConfig("random")
	pluginConfigs["remind"] = cfg.GetPluginConfig("remind")
	pluginConfigs["playlist"] = cfg.GetPluginConfig("playlist")
	pluginConfigs["gallery"] = cfg.GetPluginConfig("gallery")
	pluginConfigs["ollama"] = cfg.GetPluginConfig("ollama")
	pluginConfigs["signspinning"] = cfg.GetPluginConfig("signspinning")

	// Initialize all plugins (respects dependencies)
	if err := pluginManager.InitializeAll(pluginConfigs, bus); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}

	// Start core plugin first
	logger.Debug("Main", "Starting core plugin...")
	if err := corePlugin.Start(); err != nil {
		return fmt.Errorf("failed to start core plugin: %w", err)
	}
	defer func() {
		if err := corePlugin.Stop(); err != nil {
			logger.Error("Main", "Error stopping core plugin: %v", err)
		}
	}()

	// Start all plugins (respects dependencies and readiness)
	if err := pluginManager.StartAll(); err != nil {
		return fmt.Errorf("failed to start plugins: %w", err)
	}
	defer pluginManager.StopAll()

	// Now that all plugins are ready, start room connections
	logger.Debug("Main", "All plugins are ready, starting room connections...")
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
	go updatePluginMetrics(ctx, plugins, corePlugin)

	// Start periodic EventBus metrics updater
	go updateEventBusMetrics(ctx, bus)

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
		logger.Debug("Main", "Starting health check server on port %d", healthPort)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Main", "Health check server error: %v", err)
		}
	}()

	// Defer health server shutdown
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := healthServer.Shutdown(ctx); err != nil {
			logger.Error("Main", "Error shutting down health server: %v", err)
		}
	}()

	// Show startup summary in non-verbose mode
	if !logger.IsStartupVerbose() {
		roomList := ""
		if len(roomNames) > 3 {
			roomList = fmt.Sprintf("%s, %s, %s... (%d total)", roomNames[0], roomNames[1], roomNames[2], len(roomNames))
		} else if len(roomNames) > 0 {
			roomList = fmt.Sprintf("%s (%d total)", strings.Join(roomNames, ", "), len(roomNames))
		}

		fmt.Println() // Add spacing before summary
		fmt.Println("──────────────────────────────────────")
		fmt.Printf("📍 Active rooms: %s\n", roomList)
		fmt.Printf("🔌 Plugins: %d loaded, all ready\n", len(plugins)+1) // +1 for core plugin
		fmt.Println("💾 Database: Connected successfully")
		fmt.Printf("🏥 Health check: http://localhost:%d/health\n", healthPort)
		fmt.Printf("⏱️  Started in %v\n", time.Since(startTime))
		fmt.Println("──────────────────────────────────────")
		fmt.Println()
	}

	logger.Info("Main", "Bot is running! Press Ctrl+C to stop.")
	if logger.IsStartupVerbose() {
		logger.Info("Main", "Health check endpoints available at http://localhost:%d/health", healthPort)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("Main", "Received signal %v, shutting down...", sig)
	cancel()

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
func updatePluginMetrics(ctx context.Context, plugins []struct {
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
}

// updateEventBusMetrics periodically updates EventBus metrics
func updateEventBusMetrics(ctx context.Context, bus framework.EventBus) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastCounts := make(map[string]int64)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
}
