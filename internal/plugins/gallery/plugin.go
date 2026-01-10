package gallery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
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

	// Rate limiting
	userRateLimit map[string]time.Time
	rateLimitMu   sync.RWMutex

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
			HTMLOutputPath:      "/opt/daz/galleries",
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

	if p.config.MaxImagesPerUser <= 0 {
		logger.Warn(p.name, "Invalid max_images_per_user %d, defaulting to 25", p.config.MaxImagesPerUser)
		p.config.MaxImagesPerUser = 25
	}

	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Initialize components
	p.detector = NewImageDetector()
	p.store = NewStore(bus, p.name, p.config.MaxImagesPerUser)
	p.health = NewHealthChecker(p.store, p.config)
	p.generator = NewHTMLGenerator(p.store, p.config)
	p.userRateLimit = make(map[string]time.Time)

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
	atomic.AddInt64(&p.eventsHandled, 1)
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
		EventsHandled: atomic.LoadInt64(&p.eventsHandled),
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

	// Check rate limit (1 image per user per 30 seconds)
	p.rateLimitMu.Lock()
	lastPost, exists := p.userRateLimit[msg.Username]
	now := time.Now()
	if exists && now.Sub(lastPost) < 30*time.Second {
		p.rateLimitMu.Unlock()
		logger.Debug(p.name, "Rate limit hit for user %s", msg.Username)
		return nil // Silently ignore, don't announce rate limit
	}
	p.rateLimitMu.Unlock()

	// Detect image URLs in the message
	urls := p.detector.DetectImages(msg.Message)
	if len(urls) == 0 {
		return nil
	}

	// Update rate limit timestamp
	p.rateLimitMu.Lock()
	p.userRateLimit[msg.Username] = now
	// Clean up old entries to prevent memory leak
	for user, timestamp := range p.userRateLimit {
		if now.Sub(timestamp) > 5*time.Minute {
			delete(p.userRateLimit, user)
		}
	}
	p.rateLimitMu.Unlock()

	// Add only the first image (prevent spam of multiple images)
	if len(urls) > 0 {
		url := urls[0]
		if err := p.store.AddImage(msg.Username, url, msg.Channel); err != nil {
			logger.Error(p.name, "Failed to add image to gallery: %v", err)
		} else {
			atomic.AddInt64(&p.imagesAdded, 1)
			logger.Debug(p.name, "Added image from %s: %s", msg.Username, url)
		}
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

	// Get the shared gallery URL (GitHub Pages)
	galleryURL := "https://hildolfr.github.io/daz/"

	// Get gallery stats for the user
	stats, err := p.store.GetUserStats(username, channel)
	if err != nil {
		logger.Error(p.name, "Failed to get gallery stats: %v", err)
		// If no stats, user has no gallery yet
		message := fmt.Sprintf("Gallery: %s (You have no images yet)", galleryURL)
		p.sendResponse(channel, username, message, isPM)
		return
	}

	// Format the message with user's stats
	message := fmt.Sprintf("Gallery: %s (%s has %d images)",
		galleryURL, username, stats.ActiveImages)

	if stats.IsLocked {
		message += " [Your gallery is LOCKED]"
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

	// Wait 30 seconds before first generation to offset from health checks
	time.Sleep(30 * time.Second)

	// Generate immediately after initial delay
	if err := p.generator.GenerateAllGalleries(); err != nil {
		logger.Warn(p.name, "Initial HTML generation had issues: %v", err)
	}

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.generator.GenerateAllGalleries(); err != nil {
				logger.Warn(p.name, "HTML generation had issues: %v", err)
			}
		}
	}
}
