package greeter

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

const (
	pluginName        = "greeter"
	defaultCooldown   = 30 * time.Minute
	greetingQueueSize = 100
	minGreetingDelay  = 15 * time.Second
	maxGreetingDelay  = 3 * time.Minute
)

// Config holds greeter plugin configuration
type Config struct {
	// Global cooldown period before greeting the same user again
	CooldownMinutes int `json:"cooldown_minutes"`
	// Whether to enable greetings
	Enabled bool `json:"enabled"`
	// Default greeting template
	DefaultGreeting string `json:"default_greeting"`
	// Whether to use the greeting engine for dynamic greetings
	UseGreetingEngine bool `json:"use_greeting_engine"`
}

// Plugin implements the greeter functionality
type Plugin struct {
	ctx       context.Context
	cancel    context.CancelFunc
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	name      string
	running   bool
	mu        sync.RWMutex
	config    *Config
	wg        sync.WaitGroup

	// Greeting queue for managing greeting delivery
	greetingQueue chan *greetingRequest

	// Cooldown tracking
	cooldownManager *CooldownManager

	// Greeting manager
	greetingManager *GreetingManager

	// Skip probability (40-60%)
	skipProbability float64

	// Track last greeting per channel
	lastGreeting map[string]*lastGreetingInfo

	// Ready channel
	readyChan chan struct{}
}

// greetingRequest represents a pending greeting
type greetingRequest struct {
	channel  string
	username string
	rank     int
	joinedAt time.Time
}

// lastGreetingInfo tracks the last greeting sent in a channel
type lastGreetingInfo struct {
	username string
	text     string
	sentAt   time.Time
}

// New creates a new greeter plugin instance
func New() framework.Plugin {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())

	return &Plugin{
		name:            pluginName,
		greetingQueue:   make(chan *greetingRequest, greetingQueueSize),
		readyChan:       make(chan struct{}),
		lastGreeting:    make(map[string]*lastGreetingInfo),
		skipProbability: 0.4 + rand.Float64()*0.2, // 40-60% skip rate
		config: &Config{
			CooldownMinutes:   30,
			Enabled:           true,
			DefaultGreeting:   "Welcome back, %s!",
			UseGreetingEngine: false,
		},
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	deps := []string{"sql"}
	if p.config != nil && p.config.UseGreetingEngine {
		deps = append(deps, "greetingengine")
	}
	return deps
}

// Ready returns true when the plugin is ready to accept requests
func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

// Init initializes the plugin with configuration and event bus
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	// Parse configuration if provided
	if len(config) > 0 {
		var cfg Config
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
		p.config = &cfg
	}

	// Ensure default config values
	if p.config.CooldownMinutes <= 0 {
		p.config.CooldownMinutes = 30
	}
	if p.config.DefaultGreeting == "" {
		p.config.DefaultGreeting = "Welcome back, %s!"
	}

	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Initialize managers
	p.cooldownManager = NewCooldownManager()

	var err error
	p.greetingManager, err = NewGreetingManager()
	if err != nil {
		logger.Warn(p.name, "Failed to initialize greeting manager: %v", err)
		// We can continue without the greeting manager, using default greetings
	}

	logger.Info(p.name, "Initialized with config: enabled=%v, cooldown=%dm, engine=%v",
		p.config.Enabled, p.config.CooldownMinutes, p.config.UseGreetingEngine)
	return nil
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	p.running = true

	// Create database tables
	if err := p.createTables(); err != nil {
		logger.Error(p.name, "Failed to create tables: %v", err)
		return err
	}

	// Subscribe to events
	logger.Debug(p.name, "Subscribing to cytube.event.addUser")
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		logger.Error(p.name, "Failed to subscribe to addUser: %v", err)
		return err
	}

	logger.Debug(p.name, "Subscribing to cytube.event.userLeave")
	if err := p.eventBus.Subscribe("cytube.event.userLeave", p.handleUserLeave); err != nil {
		logger.Error(p.name, "Failed to subscribe to userLeave: %v", err)
		return err
	}

	// Register !greeter command
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "greeter",
					"min_rank": "0", // Anyone can use the command
				},
			},
		},
	}
	p.eventBus.Broadcast("command.register", regEvent)

	// Subscribe to command execution
	if err := p.eventBus.Subscribe("command.greeter.execute", p.handleGreeterCommand); err != nil {
		logger.Error(p.name, "Failed to subscribe to greeter command: %v", err)
		return err
	}

	// Start greeting processor
	p.wg.Add(1)
	go p.processGreetingQueue()

	// Load cooldowns from database
	p.wg.Add(1)
	go p.loadCooldowns()

	// Mark as ready after initialization
	close(p.readyChan)

	logger.Info(p.name, "Greeter plugin started")
	return nil
}

// Stop halts the plugin operation
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.cancel()
	p.running = false

	// Close greeting queue
	close(p.greetingQueue)

	// Close cooldown manager
	if p.cooldownManager != nil {
		p.cooldownManager.Close()
	}

	// Wait for goroutines to finish
	p.wg.Wait()

	logger.Info(p.name, "Greeter plugin stopped")
	return nil
}

// HandleEvent processes incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	// Event handling is done through subscriptions
	return nil
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "stopped"
	if p.running {
		status = "running"
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: status,
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// handleUserJoin processes user join events
func (p *Plugin) handleUserJoin(event framework.Event) error {
	if !p.config.Enabled {
		return nil
	}

	logger.Debug(p.name, "handleUserJoin called with event type: %T", event)

	var username, channel string
	var rank int

	// Handle direct AddUserEvent
	if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		username = addUserEvent.Username
		channel = addUserEvent.ChannelName
		rank = addUserEvent.UserRank
		logger.Debug(p.name, "User joined: %s in channel %s with rank %d", username, channel, rank)
	} else if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		// Handle wrapped in DataEvent
		if dataEvent.Data.UserJoin != nil {
			channel = dataEvent.Data.UserJoin.Channel
			username = dataEvent.Data.UserJoin.Username
			rank = dataEvent.Data.UserJoin.UserRank
			logger.Debug(p.name, "User joined (from UserJoin): %s in channel %s with rank %d", username, channel, rank)
		} else if dataEvent.Data.RawEvent != nil {
			// Try to extract from RawEvent
			if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				channel = addUserEvent.ChannelName
				rank = addUserEvent.UserRank
				logger.Debug(p.name, "User joined (from RawEvent): %s in channel %s with rank %d", username, channel, rank)
			}
		}
	}

	if username == "" || channel == "" {
		return nil
	}

	// Apply skip probability (40-60%)
	if rand.Float64() < p.skipProbability {
		logger.Debug(p.name, "Randomly skipping greeting for %s (skip probability: %.2f)", username, p.skipProbability)
		return nil
	}

	// Check if user opted out
	ctx := context.Background()
	optedOut, err := p.isUserOptedOut(ctx, channel, username)
	if err != nil {
		logger.Warn(p.name, "Failed to check opt-out status: %v", err)
	}
	if optedOut {
		logger.Debug(p.name, "User %s has opted out of greetings", username)
		return nil
	}

	// Check if user was recently active in this channel
	recentlyActive, err := p.wasUserRecentlyActive(ctx, channel, username, 60) // 60 minutes
	if err != nil {
		logger.Warn(p.name, "Failed to check recent activity: %v", err)
	}
	if recentlyActive {
		logger.Debug(p.name, "User %s was recently active in channel %s, skipping greeting", username, channel)
		return nil
	}

	// Check cooldown
	if !p.shouldGreet(channel, username) {
		logger.Debug(p.name, "User %s in channel %s is on cooldown, skipping greeting", username, channel)
		return nil
	}

	// Check if last message in chat was a greeting to this user
	p.mu.RLock()
	lastGreeting, exists := p.lastGreeting[channel]
	p.mu.RUnlock()

	if exists && lastGreeting.username == username && time.Since(lastGreeting.sentAt) < 5*time.Minute {
		logger.Debug(p.name, "Last message was already a greeting to %s, skipping", username)
		return nil
	}

	// Queue greeting request
	select {
	case p.greetingQueue <- &greetingRequest{
		channel:  channel,
		username: username,
		rank:     rank,
		joinedAt: time.Now(),
	}:
		logger.Debug(p.name, "Queued greeting for %s in channel %s", username, channel)
	default:
		logger.Warn(p.name, "Greeting queue full, dropping greeting for %s", username)
	}

	return nil
}

// handleUserLeave processes user leave events
func (p *Plugin) handleUserLeave(event framework.Event) error {
	// Currently we don't need to do anything on user leave
	return nil
}

// shouldGreet checks if a user should be greeted based on cooldown
func (p *Plugin) shouldGreet(channel, username string) bool {
	return !p.cooldownManager.IsOnCooldown(channel, username)
}

// updateCooldown updates the cooldown for a user
func (p *Plugin) updateCooldown(channel, username string) {
	p.cooldownManager.SetCooldown(channel, username)
}

// processGreetingQueue processes pending greetings
func (p *Plugin) processGreetingQueue() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case req, ok := <-p.greetingQueue:
			if !ok {
				return
			}

			// Random delay between 15s and 3min
			delay := minGreetingDelay + time.Duration(rand.Int63n(int64(maxGreetingDelay-minGreetingDelay)))
			timer := time.NewTimer(delay)
			select {
			case <-p.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				p.sendGreeting(req)
			}
		}
	}
}

// sendGreeting sends a greeting message
func (p *Plugin) sendGreeting(req *greetingRequest) {
	// Double-check cooldown
	if !p.shouldGreet(req.channel, req.username) {
		return
	}

	ctx := context.Background()

	// Check if this is a first-time user
	firstSeenTime, err := p.getUserFirstSeenTime(ctx, req.channel, req.username)
	isFirstTime := err == nil && firstSeenTime == nil

	var greeting string
	if p.config.UseGreetingEngine {
		// Request greeting from greeting engine
		greeting = p.getGreetingFromEngine(req.channel, req.username, req.rank)
	} else if p.greetingManager != nil {
		// Use greeting manager with time-based and first-time logic
		greeting = p.greetingManager.GetGreeting(req.username, isFirstTime)
	}

	// Fallback to default greeting if all else fails
	if greeting == "" {
		greeting = fmt.Sprintf(p.config.DefaultGreeting, req.username)
	}

	// Check if this greeting was just used
	lastGreeting, err := p.getLastGreeting(ctx, req.channel, req.username)
	if err == nil && lastGreeting != nil && lastGreeting.GreetingText == greeting {
		// Try to get a different greeting
		if p.greetingManager != nil {
			for i := 0; i < 3; i++ {
				alternative := p.greetingManager.GetGreeting(req.username, isFirstTime)
				if alternative != greeting {
					greeting = alternative
					break
				}
			}
		}
	}

	// Send the greeting
	msgData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: req.channel,
			Message: greeting,
		},
	}

	if err := p.eventBus.Broadcast("cytube.send", msgData); err != nil {
		logger.Error(p.name, "Failed to send greeting: %v", err)
		return
	}

	// Update cooldown
	p.updateCooldown(req.channel, req.username)

	// Update last greeting info
	p.mu.Lock()
	p.lastGreeting[req.channel] = &lastGreetingInfo{
		username: req.username,
		text:     greeting,
		sentAt:   time.Now(),
	}
	p.mu.Unlock()

	// Update channel state in database
	if err := p.updateGreeterState(ctx, req.channel, req.username); err != nil {
		logger.Warn(p.name, "Failed to update greeter state: %v", err)
	}

	// Record in database
	if err := p.recordGreetingToDB(ctx, req.channel, req.username, "standard", greeting, nil); err != nil {
		logger.Error(p.name, "Failed to record greeting: %v", err)
	}
}

// getGreetingFromEngine requests a greeting from the greeting engine
func (p *Plugin) getGreetingFromEngine(channel, username string, rank int) string {
	// Create request to greeting engine
	reqData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "greetingengine",
			From: p.name,
			Type: "generate",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"channel":  channel,
					"username": username,
					"rank":     fmt.Sprintf("%d", rank),
				},
			},
		},
	}

	// Use a channel to receive the response
	responseChan := make(chan string, 1)
	timeoutChan := time.After(5 * time.Second)

	// Subscribe to response
	responseHandler := func(event framework.Event) error {
		if dataEvent, ok := event.(*framework.DataEvent); ok {
			if dataEvent.Data != nil && dataEvent.Data.PluginResponse != nil {
				resp := dataEvent.Data.PluginResponse
				if resp.From == "greetingengine" {
					if greeting, ok := resp.Data.KeyValue["greeting"]; ok {
						select {
						case responseChan <- greeting:
						default:
						}
					}
				}
			}
		}
		return nil
	}

	// Subscribe to response
	topic := fmt.Sprintf("plugin.response.%s", p.name)
	if err := p.eventBus.Subscribe(topic, responseHandler); err != nil {
		logger.Warn(p.name, "Failed to subscribe to response topic: %v", err)
	}

	// Send request
	p.eventBus.Broadcast("plugin.request.greetingengine", reqData)

	// Wait for response
	select {
	case greeting := <-responseChan:
		return greeting
	case <-timeoutChan:
		logger.Warn(p.name, "Timeout waiting for greeting engine response")
		return ""
	}
}

// recordGreeting records a greeting in the database
func (p *Plugin) recordGreeting(channel, username, greeting string) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	// Call the database method with proper parameters
	err := p.recordGreetingToDB(ctx, channel, username, "standard", greeting, nil)
	if err != nil {
		logger.Error(p.name, "Failed to record greeting: %v", err)
	}
}

// loadCooldowns loads recent cooldowns from the database
func (p *Plugin) loadCooldowns() {
	defer p.wg.Done()

	// Wait a bit for database to be ready
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case <-p.ctx.Done():
		return
	case <-timer.C:
	}

	query := `
		SELECT channel, username, greeted_at
		FROM daz_greeter_history
		WHERE greeted_at > $1
	`

	cooldownDuration := time.Duration(p.config.CooldownMinutes) * time.Minute
	cutoffTime := time.Now().Add(-cooldownDuration)

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	rows, err := p.sqlClient.QueryContext(ctx, query, cutoffTime)
	if err != nil {
		logger.Error(p.name, "Failed to load cooldowns: %v", err)
		return
	}
	defer rows.Close()

	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for rows.Next() {
		var channel, username string
		var greetedAt time.Time

		if err := rows.Scan(&channel, &username, &greetedAt); err != nil {
			logger.Error(p.name, "Failed to scan cooldown row: %v", err)
			continue
		}

		// Set cooldown based on when they were last greeted
		if time.Since(greetedAt) < time.Duration(p.config.CooldownMinutes)*time.Minute {
			p.cooldownManager.SetCooldown(channel, username)
			count++
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error(p.name, "Error iterating cooldown rows: %v", err)
	}

	logger.Info(p.name, "Loaded %d cooldowns from database", count)
}

// handleGreeterCommand handles the !greeter command
func (p *Plugin) handleGreeterCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	cmd := req.Data.Command

	// Extract command context
	channel := cmd.Params["channel"]
	username := cmd.Params["username"]
	isPM := cmd.Params["is_pm"] == "true"

	// Parse subcommand from args
	if len(cmd.Args) == 0 {
		p.sendResponse(channel, username, "Usage: !greeter <on|off>", isPM)
		return nil
	}

	subcommand := cmd.Args[0]

	switch subcommand {
	case "on":
		return p.handleToggle(channel, username, false, isPM)
	case "off":
		return p.handleToggle(channel, username, true, isPM)
	default:
		p.sendResponse(channel, username, "Usage: !greeter <on|off>", isPM)
	}

	return nil
}

// handleToggle handles toggling greetings on/off for a user
func (p *Plugin) handleToggle(channel, username string, optOut bool, isPM bool) error {
	ctx := context.Background()

	var reason string
	if optOut {
		reason = "User requested via !greeter command"
	}
	err := p.setUserOptOut(ctx, channel, username, optOut, reason)
	if err != nil {
		logger.Error(p.name, "Failed to update opt-out preference: %v", err)
		p.sendResponse(channel, username, "Failed to update your preference. Please try again.", isPM)
		return err
	}

	var message string
	if optOut {
		message = fmt.Sprintf("Greetings disabled for %s in this channel.", username)
	} else {
		message = fmt.Sprintf("Greetings enabled for %s in this channel.", username)
	}

	p.sendResponse(channel, username, message, isPM)
	return nil
}

// sendResponse sends a response either as a PM or channel message
func (p *Plugin) sendResponse(channel, username, message string, isPM bool) {
	if isPM {
		p.sendPM(channel, username, message)
	} else {
		p.sendChannelMessage(channel, message)
	}
}

// sendChannelMessage sends a message to the channel
func (p *Plugin) sendChannelMessage(channel, message string) {
	msgData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: channel,
			Message: message,
		},
	}
	p.eventBus.Broadcast("cytube.send", msgData)
}

// sendPM sends a private message
func (p *Plugin) sendPM(channel, toUser, message string) {
	pmData := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  toUser,
			Message: message,
			Channel: channel,
		},
	}
	p.eventBus.Broadcast("cytube.send.pm", pmData)
}
