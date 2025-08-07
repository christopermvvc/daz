package gallery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

// Plugin implements automatic image gallery collection and management
type Plugin struct {
	name      string
	eventBus  framework.EventBus
	config    *Config
	running   bool
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startTime time.Time

	// Gallery components
	detector  *ImageDetector
	store     *Store
	health    *HealthChecker
	generator *HTMLGenerator

	// Tracking
	eventsHandled int64
	imagesAdded   int64
}

// Config holds plugin configuration
type Config struct {
	MaxImagesPerUser    int           `json:"max_images_per_user"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	GenerateHTML        bool          `json:"generate_html"`
	HTMLOutputPath      string        `json:"html_output_path"`
	EnableHealthCheck   bool          `json:"enable_health_check"`
	AdminOnly           bool          `json:"admin_only"`
}

// New creates a new gallery plugin instance
func New() framework.Plugin {
	return &Plugin{
		name: "gallery",
		config: &Config{
			MaxImagesPerUser:    25,
			HealthCheckInterval: 5 * time.Minute,
			GenerateHTML:        true,
			HTMLOutputPath:      "./galleries-output",
			EnableHealthCheck:   true,
			AdminOnly:           false,
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Init initializes the plugin with configuration
func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	// Parse configuration if provided
	if len(configData) > 0 {
		if err := json.Unmarshal(configData, &p.config); err != nil {
			logger.Error(p.name, "Failed to unmarshal config: %v", err)
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Initialize components
	p.detector = NewImageDetector()
	p.store = NewStore(bus, p.name)
	p.health = NewHealthChecker(p.store, p.config)
	p.generator = NewHTMLGenerator(p.store, p.config)

	logger.Info(p.name, "Gallery plugin initialized with max %d images per user", p.config.MaxImagesPerUser)
	return nil
}

// Start starts the plugin
func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()

	// Initialize database schema
	if err := p.store.InitializeSchema(); err != nil {
		logger.Error(p.name, "Failed to initialize database schema: %v", err)
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Register commands with EventFilter
	if err := p.registerCommands(); err != nil {
		logger.Error(p.name, "Failed to register commands: %v", err)
		return err
	}

	// Subscribe to events
	if err := p.subscribeToEvents(); err != nil {
		logger.Error(p.name, "Failed to subscribe to events: %v", err)
		return err
	}

	// Start background workers
	if p.config.EnableHealthCheck {
		p.wg.Add(1)
		go p.runHealthChecker()
	}

	if p.config.GenerateHTML {
		p.wg.Add(1)
		go p.runHTMLGenerator()
	}

	logger.Info(p.name, "Gallery plugin started")
	return nil
}

// Stop stops the plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin not running")
	}
	p.running = false
	p.mu.Unlock()

	// Cancel context to stop background workers
	if p.cancel != nil {
		p.cancel()
	}

	// Wait for all workers to finish
	p.wg.Wait()

	logger.Info(p.name, "Gallery plugin stopped")
	return nil
}

// HandleEvent handles incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	p.eventsHandled++
	return nil
}

// Status returns the plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state := "stopped"
	if p.running {
		state = "running"
	}

	return framework.PluginStatus{
		Name:          p.name,
		State:         state,
		EventsHandled: p.eventsHandled,
		Uptime:        time.Since(p.startTime),
	}
}

// Dependencies returns plugin dependencies
func (p *Plugin) Dependencies() []string {
	return []string{"eventfilter", "sql"}
}

// Ready returns whether the plugin is ready
func (p *Plugin) Ready() bool {
	return p.running
}

func (p *Plugin) registerCommands() error {
	// Register gallery commands with EventFilter
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "gallery,gallery_lock,gallery_unlock,gallery_check",
					"min_rank": "0", // Allow all users, we'll check permissions in handler
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		return fmt.Errorf("failed to register commands: %w", err)
	}

	logger.Info(p.name, "Registered gallery commands with EventFilter")
	return nil
}

func (p *Plugin) subscribeToEvents() error {
	// Subscribe to chat messages for image detection
	if err := p.eventBus.Subscribe("cytube.event.chatMsg", p.handleChatMessage); err != nil {
		return fmt.Errorf("failed to subscribe to chat messages: %w", err)
	}

	// Subscribe to command execution events
	// IMPORTANT: EventFilter sends to command.{PLUGIN_NAME}.execute, not command name!
	if err := p.eventBus.Subscribe("command.gallery.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command execution: %w", err)
	}

	logger.Debug(p.name, "Subscribed to events")
	return nil
}

func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	msg := dataEvent.Data.ChatMessage

	// Detect image URLs in the message
	urls := p.detector.DetectImages(msg.Message)
	if len(urls) == 0 {
		return nil
	}

	// Add each detected image to the gallery
	for _, url := range urls {
		if err := p.store.AddImage(msg.Username, url, msg.Channel); err != nil {
			logger.Error(p.name, "Failed to add image to gallery: %v", err)
			continue
		}
		p.imagesAdded++
		logger.Debug(p.name, "Added image from %s: %s", msg.Username, url)
	}

	return nil
}

func (p *Plugin) handleCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.Command == nil {
		return nil
	}

	cmd := req.Data.Command
	params := cmd.Params

	// Handle commands asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		switch cmd.Name {
		case "gallery":
			p.handleGalleryCommand(params)
		case "gallery_lock":
			p.handleLockCommand(params)
		case "gallery_unlock":
			p.handleUnlockCommand(params)
		case "gallery_check":
			p.handleCheckCommand(params)
		default:
			logger.Warn(p.name, "Unknown command: %s", cmd.Name)
		}
	}()

	return nil
}

func (p *Plugin) handleGalleryCommand(params map[string]string) {
	username := params["username"]
	channel := params["channel"]
	isPM := params["is_pm"] == "true"

	// Get gallery URL for the user (GitHub Pages)
	galleryURL := fmt.Sprintf("https://hildolfr.github.io/daz/%s/", username)

	// Get gallery stats
	stats, err := p.store.GetUserStats(username, channel)
	if err != nil {
		logger.Error(p.name, "Failed to get gallery stats: %v", err)
		p.sendResponse(channel, username, "Failed to retrieve gallery information.", isPM)
		return
	}

	message := fmt.Sprintf("%s's gallery: %s (%d active images)",
		username, galleryURL, stats.ActiveImages)

	if stats.IsLocked {
		message += " [LOCKED]"
	}

	p.sendResponse(channel, username, message, isPM)
}

func (p *Plugin) handleLockCommand(params map[string]string) {
	username := params["username"]
	channel := params["channel"]
	isPM := params["is_pm"] == "true"

	// Lock the user's gallery
	if err := p.store.LockGallery(username, channel); err != nil {
		logger.Error(p.name, "Failed to lock gallery: %v", err)
		p.sendResponse(channel, username, "Failed to lock gallery.", isPM)
		return
	}

	p.sendResponse(channel, username, "Your gallery has been locked.", isPM)
}

func (p *Plugin) handleUnlockCommand(params map[string]string) {
	username := params["username"]
	channel := params["channel"]
	isPM := params["is_pm"] == "true"

	// Unlock the user's gallery
	if err := p.store.UnlockGallery(username, channel); err != nil {
		logger.Error(p.name, "Failed to unlock gallery: %v", err)
		p.sendResponse(channel, username, "Failed to unlock gallery.", isPM)
		return
	}

	p.sendResponse(channel, username, "Your gallery has been unlocked.", isPM)
}

func (p *Plugin) handleCheckCommand(params map[string]string) {
	username := params["username"]
	channel := params["channel"]
	isPM := params["is_pm"] == "true"
	isAdmin := params["is_admin"] == "true"

	// Only admins can trigger manual health checks
	if !isAdmin {
		p.sendResponse(channel, username, "Only admins can trigger manual health checks.", isPM)
		return
	}

	// Trigger health check asynchronously
	go func() {
		if err := p.health.CheckAllImages(); err != nil {
			logger.Error(p.name, "Manual health check failed: %v", err)
		}
	}()

	p.sendResponse(channel, username, "Manual health check started. Results will be logged.", isPM)
}

func (p *Plugin) sendResponse(channel, username, message string, isPM bool) {
	if isPM {
		// Send as PM using plugin response system
		responseData := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					CommandResult: &framework.CommandResultData{
						Success: true,
						Output:  message,
					},
					KeyValue: map[string]string{
						"username": username,
						"channel":  channel,
					},
				},
			},
		}
		if err := p.eventBus.Broadcast("plugin.response", responseData); err != nil {
			logger.Error(p.name, "Failed to send PM response: %v", err)
		}
	} else {
		// Send as public chat message
		chatData := &framework.EventData{
			RawMessage: &framework.RawMessageData{
				Message: message,
				Channel: channel,
			},
		}
		if err := p.eventBus.Broadcast("cytube.send", chatData); err != nil {
			logger.Error(p.name, "Failed to send chat response: %v", err)
		}
	}
}

func (p *Plugin) runHealthChecker() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.health.CheckPendingImages(); err != nil {
				logger.Error(p.name, "Health check failed: %v", err)
			}
		}
	}
}

func (p *Plugin) runHTMLGenerator() {
	defer p.wg.Done()

	// Generate HTML every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Generate immediately on start
	if err := p.generator.GenerateAllGalleries(); err != nil {
		logger.Error(p.name, "Initial HTML generation failed: %v", err)
	}

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.generator.GenerateAllGalleries(); err != nil {
				logger.Error(p.name, "HTML generation failed: %v", err)
			}
		}
	}
}
