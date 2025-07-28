package greeter

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
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
	// Whether to enable fortune messages after greetings
	EnableFortune bool `json:"enable_fortune"`
	// Probability of sending a fortune after a successful greeting (0.0-1.0)
	FortuneProbability float64 `json:"fortune_probability"`
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

	// Skip probability (60-80%)
	skipProbability float64

	// Track last greeting per channel
	lastGreeting map[string]*lastGreetingInfo

	// Ready channel
	readyChan chan struct{}

	// Startup time tracking
	startupTime time.Time

	// Channel join time tracking (for reconnection grace period)
	channelJoinTimes map[string]time.Time
	channelMu        sync.RWMutex
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
	return &Plugin{
		name:             pluginName,
		greetingQueue:    make(chan *greetingRequest, greetingQueueSize),
		readyChan:        make(chan struct{}),
		lastGreeting:     make(map[string]*lastGreetingInfo),
		skipProbability:  0.6 + rand.Float64()*0.2, // 60-80% skip rate
		channelJoinTimes: make(map[string]time.Time),
		config: &Config{
			CooldownMinutes:    30,
			Enabled:            true,
			DefaultGreeting:    "Welcome back, %s!",
			UseGreetingEngine:  false,
			EnableFortune:      true,
			FortuneProbability: 0.25,
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
		// Only override specific fields from config, preserving defaults
		var cfg Config
		if err := json.Unmarshal(config, &cfg); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}

		// Merge with existing config, only overriding provided values
		if cfg.CooldownMinutes > 0 {
			p.config.CooldownMinutes = cfg.CooldownMinutes
		}
		// Only override Enabled if explicitly set in config
		// Since JSON unmarshaling sets bool to false by default, we can't distinguish
		// between "not set" and "set to false". For now, we'll preserve the default.
		// In production, you'd want to use a pointer or a custom unmarshaler.
		if len(config) > 2 { // More than just "{}"
			var rawConfig map[string]interface{}
			if err := json.Unmarshal(config, &rawConfig); err == nil {
				if enabled, ok := rawConfig["enabled"].(bool); ok {
					p.config.Enabled = enabled
				}
			}
		}
		if cfg.DefaultGreeting != "" {
			p.config.DefaultGreeting = cfg.DefaultGreeting
		}
		if cfg.UseGreetingEngine {
			p.config.UseGreetingEngine = cfg.UseGreetingEngine
		}
	}

	// Ensure default config values (already set in New(), but double-check)
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
	p.startupTime = time.Now()

	// Create database tables
	if err := p.createTables(); err != nil {
		logger.Error(p.name, "Failed to create tables: %v", err)
		return err
	}

	// Subscribe to events
	// Note: CyTube sends "addUser" events for both initial userlist AND when users join
	// We'll subscribe to both addUser and userJoin to catch all cases
	logger.Debug(p.name, "Subscribing to cytube.event.addUser with enabled=%v", p.config.Enabled)
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		logger.Error(p.name, "Failed to subscribe to addUser: %v", err)
		return err
	}

	logger.Debug(p.name, "Subscribing to cytube.event.userJoin with enabled=%v", p.config.Enabled)
	if err := p.eventBus.Subscribe("cytube.event.userJoin", p.handleUserJoin); err != nil {
		logger.Error(p.name, "Failed to subscribe to userJoin: %v", err)
		return err
	}

	logger.Debug(p.name, "Subscribing to cytube.event.userLeave")
	if err := p.eventBus.Subscribe("cytube.event.userLeave", p.handleUserLeave); err != nil {
		logger.Error(p.name, "Failed to subscribe to userLeave: %v", err)
		return err
	}

	// Subscribe to userlist.start to track channel joins/reconnections
	logger.Debug(p.name, "Subscribing to cytube.event.userlist.start")
	if err := p.eventBus.Subscribe("cytube.event.userlist.start", p.handleUserListStart); err != nil {
		logger.Error(p.name, "Failed to subscribe to userlist.start: %v", err)
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
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register greeter command: %v", err)
	}

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
	logger.Debug(p.name, "=== GREETER: handleUserJoin called ===")
	logger.Info(p.name, "Received event type: %T", event)
	logger.Debug(p.name, "Greeter enabled: %v, skip probability: %.2f%%", p.config.Enabled, p.skipProbability*100)

	if !p.config.Enabled {
		logger.Debug(p.name, "SKIP REASON: Greeter is disabled in config")
		return nil
	}

	// Skip greetings for the first 20 seconds after startup
	if time.Since(p.startupTime) < 20*time.Second {
		logger.Debug(p.name, "SKIP REASON: Bot startup grace period (%.1f seconds since startup)", time.Since(p.startupTime).Seconds())
		return nil
	}

	logger.Info(p.name, "Processing event after startup grace period")

	var username, channel string
	var rank int

	// Handle direct UserJoinEvent (actual user joins)
	if userJoinEvent, ok := event.(*framework.UserJoinEvent); ok {
		username = userJoinEvent.Username
		channel = userJoinEvent.ChannelName
		rank = userJoinEvent.UserRank
		logger.Info(p.name, "Processing UserJoinEvent (real join) for username=%s, channel=%s, rank=%d", username, channel, rank)
	} else if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		// Handle direct AddUserEvent (initial userlist population)
		username = addUserEvent.Username
		channel = addUserEvent.ChannelName
		rank = addUserEvent.UserRank
		logger.Info(p.name, "Processing AddUserEvent for username=%s, channel=%s, rank=%d", username, channel, rank)

		// Check if this is from bulk userlist population (not a real join)
		if addUserEvent.Metadata != nil {
			logger.Info(p.name, "AddUserEvent metadata for %s: %+v", username, addUserEvent.Metadata)
			if addUserEvent.Metadata["from_userlist"] == "true" {
				logger.Info(p.name, "SKIP REASON: User %s is from initial userlist, not a real join", username)
				return nil
			}
		} else {
			logger.Info(p.name, "AddUserEvent for %s has no metadata", username)
		}
	} else if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		// Handle wrapped in DataEvent
		logger.Debug(p.name, "DataEvent received - Type: %s, Has UserJoin: %v, Has RawEvent: %v",
			dataEvent.EventType, dataEvent.Data.UserJoin != nil, dataEvent.Data.RawEvent != nil)

		if dataEvent.Data.UserJoin != nil {
			channel = dataEvent.Data.UserJoin.Channel
			username = dataEvent.Data.UserJoin.Username
			rank = dataEvent.Data.UserJoin.UserRank
			logger.Debug(p.name, "Extracted from DataEvent.UserJoin: username=%s, channel=%s, rank=%d", username, channel, rank)
		} else if dataEvent.Data.RawEvent != nil {
			// Try to extract from RawEvent
			logger.Debug(p.name, "RawEvent type: %T", dataEvent.Data.RawEvent)

			if userJoinEvent, ok := dataEvent.Data.RawEvent.(*framework.UserJoinEvent); ok {
				username = userJoinEvent.Username
				channel = userJoinEvent.ChannelName
				rank = userJoinEvent.UserRank
				logger.Debug(p.name, "Extracted from DataEvent.RawEvent (UserJoinEvent): username=%s, channel=%s, rank=%d", username, channel, rank)
			} else if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				channel = addUserEvent.ChannelName
				rank = addUserEvent.UserRank
				logger.Debug(p.name, "Extracted from DataEvent.RawEvent (AddUserEvent): username=%s, channel=%s, rank=%d", username, channel, rank)

				// Check if this is from bulk userlist population (not a real join)
				if addUserEvent.Metadata != nil {
					logger.Info(p.name, "DataEvent.RawEvent.AddUserEvent metadata for %s: %+v", username, addUserEvent.Metadata)
					if addUserEvent.Metadata["from_userlist"] == "true" {
						logger.Info(p.name, "SKIP REASON: User %s is from initial userlist, not a real join", username)
						return nil
					}
				} else {
					logger.Info(p.name, "DataEvent.RawEvent.AddUserEvent for %s has no metadata", username)
				}
			}
		}
	} else {
		// Log unhandled event type
		logger.Warn(p.name, "Unhandled event type: %T", event)
	}

	if username == "" || channel == "" {
		logger.Debug(p.name, "SKIP REASON: Empty username or channel - username=%q, channel=%q", username, channel)
		return nil
	}

	logger.Debug(p.name, "Processing join event for %s in channel %s", username, channel)

	// Skip greetings for 20 seconds after joining/reconnecting to a channel
	// Use a function to ensure proper lock handling
	if p.isInChannelGracePeriod(channel) {
		logger.Debug(p.name, "SKIP REASON: Channel grace period active for %s, skipping greeting for %s", channel, username)
		return nil
	}

	// Don't greet ourselves - check against bot name from environment
	botName := os.Getenv("DAZ_BOT_NAME")
	if botName == "" {
		botName = "Dazza" // Default fallback
	}

	if strings.EqualFold(username, botName) {
		logger.Debug(p.name, "SKIP REASON: Self-greeting detected - username=%s, botName=%s", username, botName)
		return nil
	}

	// Check if user opted out
	optedOut, err := p.isUserOptedOut(p.ctx, channel, username)
	if err != nil {
		logger.Warn(p.name, "Failed to check opt-out status: %v", err)
	}
	if optedOut {
		logger.Debug(p.name, "SKIP REASON: User %s has opted out of greetings", username)
		return nil
	}
	logger.Debug(p.name, "Opt-out check passed for %s", username)

	// Check if user was recently active in this channel
	recentlyActive, err := p.wasUserRecentlyActive(p.ctx, channel, username, 60) // 60 minutes
	if err != nil {
		logger.Warn(p.name, "Failed to check recent activity: %v", err)
	}
	if recentlyActive {
		logger.Debug(p.name, "SKIP REASON: User %s was recently active (within 60 min) in channel %s", username, channel)
		return nil
	}
	logger.Debug(p.name, "Recent activity check passed for %s (not active in last 60 min)", username)

	// Check if user was recently greeted in ANY channel (global cooldown)
	recentlyGreeted, err := p.wasUserRecentlyGreeted(p.ctx, username, 30) // 30 minutes
	if err != nil {
		logger.Warn(p.name, "Failed to check recent greetings: %v", err)
	}
	if recentlyGreeted {
		logger.Debug(p.name, "SKIP REASON: User %s was greeted in another channel within 30 min", username)
		return nil
	}

	// Check cooldown and last greeting atomically
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.shouldGreet(channel, username) {
		logger.Debug(p.name, "SKIP REASON: User %s in channel %s is on cooldown", username, channel)
		return nil
	}
	logger.Debug(p.name, "Cooldown check passed for %s", username)

	// Check if last message in chat was a greeting to this user
	lastGreeting, exists := p.lastGreeting[channel]
	if exists && lastGreeting.username == username && time.Since(lastGreeting.sentAt) < 5*time.Minute {
		logger.Debug(p.name, "SKIP REASON: Last greeting was to %s, sent %v ago", username, time.Since(lastGreeting.sentAt))
		return nil
	}
	logger.Debug(p.name, "Last greeting check passed for %s", username)

	// Apply skip probability (60-80%) as final check
	skipRoll := rand.Float64()
	if skipRoll < p.skipProbability {
		logger.Debug(p.name, "SKIP REASON: Random skip - roll=%.3f < probability=%.3f for user %s", skipRoll, p.skipProbability, username)
		return nil
	}
	logger.Debug(p.name, "Random skip check passed - roll=%.3f >= probability=%.3f", skipRoll, p.skipProbability)

	// Queue greeting request
	logger.Debug(p.name, "Attempting to queue greeting for %s in channel %s", username, channel)
	select {
	case p.greetingQueue <- &greetingRequest{
		channel:  channel,
		username: username,
		rank:     rank,
		joinedAt: time.Now(),
	}:
		logger.Debug(p.name, "Successfully queued greeting for %s in channel %s", username, channel)
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

// handleUserListStart processes userlist start events (channel join/reconnect)
func (p *Plugin) handleUserListStart(event framework.Event) error {
	// Extract channel from the event
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Check RawMessage exists and has a channel
	if dataEvent.Data.RawMessage != nil && dataEvent.Data.RawMessage.Channel != "" {
		channel := dataEvent.Data.RawMessage.Channel
		// Update the channel join time
		p.channelMu.Lock()
		p.channelJoinTimes[channel] = time.Now()
		p.channelMu.Unlock()

		logger.Info(p.name, "Detected channel join/reconnect for %s, starting 20-second grace period", channel)
	}

	return nil
}

// shouldGreet checks if a user should be greeted based on cooldown
func (p *Plugin) shouldGreet(channel, username string) bool {
	return !p.cooldownManager.IsOnCooldown(channel, username)
}

// updateCooldown updates the cooldown for a user
func (p *Plugin) updateCooldown(channel, username string) {
	cooldownDuration := time.Duration(p.config.CooldownMinutes) * time.Minute
	p.cooldownManager.SetCooldown(channel, username, cooldownDuration)
}

// processGreetingQueue processes pending greetings
func (p *Plugin) processGreetingQueue() {
	defer p.wg.Done()
	logger.Debug(p.name, "Greeting queue processor started")

	for {
		select {
		case <-p.ctx.Done():
			logger.Debug(p.name, "Greeting queue processor stopping")
			return
		case req, ok := <-p.greetingQueue:
			if !ok {
				logger.Debug(p.name, "Greeting queue closed")
				return
			}
			logger.Debug(p.name, "=== PROCESSING GREETING REQUEST ===")
			logger.Debug(p.name, "User: %s, Channel: %s, Queued: %v ago",
				req.username, req.channel, time.Since(req.joinedAt))

			// Random delay between 15s and 3min
			delay := minGreetingDelay + time.Duration(rand.Int63n(int64(maxGreetingDelay-minGreetingDelay)))
			logger.Debug(p.name, "Waiting %v before sending greeting to %s", delay, req.username)
			timer := time.NewTimer(delay)
			select {
			case <-p.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				logger.Debug(p.name, "=== SENDING GREETING NOW ===")
				p.sendGreeting(req)
			}
		}
	}
}

// sendGreeting sends a greeting message
func (p *Plugin) sendGreeting(req *greetingRequest) {
	logger.Debug(p.name, "sendGreeting called for %s in channel %s", req.username, req.channel)

	// Check if we're in a channel grace period (recent reconnection)
	if p.isInChannelGracePeriod(req.channel) {
		logger.Debug(p.name, "SKIP GREETING: Still in channel grace period for %s", req.channel)
		return
	}

	// Check if user is still in the channel
	isPresent, err := p.isUserInChannel(p.ctx, req.channel, req.username)
	if err != nil {
		logger.Warn(p.name, "Failed to check user presence: %v", err)
		// Continue anyway in case of database error
	} else if !isPresent {
		logger.Debug(p.name, "SKIP GREETING: User %s is no longer in channel %s", req.username, req.channel)
		return
	}

	// Double-check cooldown
	if !p.shouldGreet(req.channel, req.username) {
		logger.Debug(p.name, "User %s still on cooldown, skipping greeting", req.username)
		return
	}

	// Check if this is a first-time user
	firstSeenTime, err := p.getUserFirstSeenTime(p.ctx, req.channel, req.username)
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
	lastGreeting, err := p.getLastGreeting(p.ctx, req.channel, req.username)
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

	logger.Info(p.name, "Sending greeting to %s in channel %s: %s", req.username, req.channel, greeting)
	if err := p.eventBus.Broadcast("cytube.send", msgData); err != nil {
		logger.Error(p.name, "Failed to send greeting: %v", err)
		return
	}
	logger.Info(p.name, "Successfully sent greeting to %s in channel %s", req.username, req.channel)

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
	if err := p.updateGreeterState(p.ctx, req.channel, req.username); err != nil {
		logger.Warn(p.name, "Failed to update greeter state: %v", err)
	}

	// Record in database
	if err := p.recordGreetingToDB(p.ctx, req.channel, req.username, "standard", greeting, nil); err != nil {
		logger.Error(p.name, "Failed to record greeting: %v", err)
	}

	// Roll for fortune (25% chance)
	if p.config.EnableFortune {
		fortuneRoll := rand.Float64()
		logger.Debug(p.name, "Fortune roll for %s: %.3f (need < %.3f)", req.username, fortuneRoll, p.config.FortuneProbability)
		
		if fortuneRoll < p.config.FortuneProbability {
			logger.Info(p.name, "Fortune roll successful for %s in channel %s", req.username, req.channel)
			// Launch goroutine to send delayed fortune
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				p.sendDelayedFortune(req.channel, 10*time.Second)
			}()
		} else {
			logger.Debug(p.name, "Fortune roll failed for %s (%.3f >= %.3f)", req.username, fortuneRoll, p.config.FortuneProbability)
		}
	} else {
		logger.Debug(p.name, "Fortune feature disabled")
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

	// Use context with timeout for cleaner cancellation
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	// Use the event bus Request method instead of Subscribe/Broadcast pattern
	// This avoids the memory leak from subscriptions that can't be unsubscribed
	resp, err := p.eventBus.Request(ctx, "greetingengine", "generate", reqData, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Warn(p.name, "Timeout waiting for greeting engine response")
		} else {
			logger.Warn(p.name, "Failed to get greeting from engine: %v", err)
		}
		return ""
	}

	// Extract greeting from response
	if resp != nil && resp.PluginResponse != nil {
		if greeting, ok := resp.PluginResponse.Data.KeyValue["greeting"]; ok {
			return greeting
		}
	}

	return ""
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
		SELECT channel, username, created_at
		FROM daz_greeter_history
		WHERE created_at > $1
	`

	cooldownDuration := time.Duration(p.config.CooldownMinutes) * time.Minute
	cutoffTime := time.Now().Add(-cooldownDuration)

	// Create a timeout context that also respects plugin cancellation
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	// Check if plugin context is already cancelled
	select {
	case <-p.ctx.Done():
		return
	default:
	}

	rows, err := p.sqlClient.QueryContext(ctx, query, cutoffTime)
	if err != nil {
		logger.Error(p.name, "Failed to load cooldowns: %v", err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

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
		remainingCooldown := time.Duration(p.config.CooldownMinutes)*time.Minute - time.Since(greetedAt)
		if remainingCooldown > 0 {
			p.cooldownManager.SetCooldown(channel, username, remainingCooldown)
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
	case "test":
		// Test command to manually trigger a greeting
		if len(cmd.Args) < 2 {
			p.sendResponse(channel, username, "Usage: !greeter test <username>", isPM)
			return nil
		}
		testUser := cmd.Args[1]
		logger.Info(p.name, "Manual greeting test requested for user %s by %s", testUser, username)
		p.sendGreeting(&greetingRequest{
			channel:  channel,
			username: testUser,
			rank:     0,
			joinedAt: time.Now(),
		})
		return nil
	default:
		p.sendResponse(channel, username, "Usage: !greeter <on|off|test <username>>", isPM)
	}

	return nil
}

// handleToggle handles toggling greetings on/off for a user
func (p *Plugin) handleToggle(channel, username string, optOut bool, isPM bool) error {

	var reason string
	if optOut {
		reason = "User requested via !greeter command"
	}
	err := p.setUserOptOut(p.ctx, channel, username, optOut, reason)
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
	_ = p.eventBus.Broadcast("cytube.send", msgData)
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
	_ = p.eventBus.Broadcast("cytube.send.pm", pmData)
}

// isInChannelGracePeriod checks if we're still in the grace period after joining a channel
func (p *Plugin) isInChannelGracePeriod(channel string) bool {
	p.channelMu.RLock()
	channelJoinTime, exists := p.channelJoinTimes[channel]
	p.channelMu.RUnlock()

	if !exists {
		logger.Debug(p.name, "No channel join time recorded for %s", channel)
		return false
	}

	timeSinceJoin := time.Since(channelJoinTime)
	if timeSinceJoin < 20*time.Second {
		logger.Debug(p.name, "SKIP REASON: Channel reconnection grace period (%.1f seconds since joining %s)",
			timeSinceJoin.Seconds(), channel)
		return true
	}

	logger.Debug(p.name, "Channel grace period expired for %s (%.1f seconds since join)", channel, timeSinceJoin.Seconds())
	return false
}

// getFortune executes the fortune command and returns the output
func (p *Plugin) getFortune() (string, error) {
	// Create context with 1 second timeout
	ctx, cancel := context.WithTimeout(p.ctx, 1*time.Second)
	defer cancel()

	// Execute fortune command with -aos flags
	cmd := exec.CommandContext(ctx, "fortune", "-aos")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute fortune: %w", err)
	}

	// Trim whitespace and check length
	fortune := strings.TrimSpace(string(output))
	if len(fortune) > 160 {
		// Truncate if somehow we got a long fortune despite -s flag
		fortune = fortune[:157] + "..."
	}

	return fortune, nil
}

// sendDelayedFortune sends a fortune message after a delay
func (p *Plugin) sendDelayedFortune(channel string, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-p.ctx.Done():
		return
	case <-timer.C:
		fortune, err := p.getFortune()
		if err != nil {
			logger.Debug(p.name, "Failed to get fortune: %v", err)
			return
		}

		// Send fortune with prefix
		msgData := &framework.EventData{
			RawMessage: &framework.RawMessageData{
				Channel: channel,
				Message: fmt.Sprintf("🔮[FORTUNE]🔮: \"%s\"", fortune),
			},
		}

		if err := p.eventBus.Broadcast("cytube.send", msgData); err != nil {
			logger.Error(p.name, "Failed to send fortune: %v", err)
		} else {
			logger.Info(p.name, "Sent fortune to channel %s: %s", channel, fortune)
		}
	}
}
