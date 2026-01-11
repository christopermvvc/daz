package eventfilter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the unified event filter plugin for event routing and command processing
type Plugin struct {
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	config    *Config
	running   bool
	mu        sync.RWMutex

	// Command registry
	commandRegistry map[string]*CommandInfo
	cooldowns       map[string]time.Time
	registryLoaded  bool

	// Admin users loaded from file
	adminUsers map[string]bool

	// Shutdown management
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	readyChan chan struct{}
}

// Config holds eventfilter plugin configuration
type Config struct {
	// From filter plugin
	CommandPrefix string        `json:"command_prefix"`
	RoutingRules  []RoutingRule `json:"routing_rules,omitempty"`

	// From commandrouter plugin
	DefaultCooldown int      `json:"default_cooldown"`
	AdminUsers      []string `json:"admin_users"`
}

// RoutingRule defines how to route events
type RoutingRule struct {
	EventType    string
	TargetPlugin string
	Priority     int
	Enabled      bool
}

// CommandInfo stores command registration details
type CommandInfo struct {
	PluginName     string
	PrimaryCommand string
	IsAlias        bool
	MinRank        int
	Enabled        bool
}

// New creates a new eventfilter plugin instance
func New() framework.Plugin {
	return &Plugin{
		name: "eventfilter",
		config: &Config{
			CommandPrefix:   "!",
			RoutingRules:    getDefaultRoutingRules(),
			DefaultCooldown: 5,
			AdminUsers:      []string{},
		},
		running:         false,
		commandRegistry: make(map[string]*CommandInfo),
		cooldowns:       make(map[string]time.Time),
		registryLoaded:  false,
		readyChan:       make(chan struct{}),
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql"} // EventFilter depends on SQL plugin for schema creation and data persistence
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

// NewPlugin creates a new eventfilter plugin instance with custom configuration
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{
			CommandPrefix:   "!",
			RoutingRules:    getDefaultRoutingRules(),
			DefaultCooldown: 5,
			AdminUsers:      []string{},
		}
	}

	return &Plugin{
		name:            "eventfilter",
		config:          config,
		running:         false,
		commandRegistry: make(map[string]*CommandInfo),
		cooldowns:       make(map[string]time.Time),
		registryLoaded:  false,
		readyChan:       make(chan struct{}),
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Initialize sets up the plugin with the event bus (backward compatibility, proxies to Init)
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	return p.Init(nil, eventBus)
}

// Init implements the framework.Plugin interface
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	if len(config) > 0 {
		newConfig := &Config{
			CommandPrefix:   "!",
			RoutingRules:    getDefaultRoutingRules(),
			DefaultCooldown: 5,
			AdminUsers:      []string{},
		}
		if err := json.Unmarshal(config, newConfig); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		p.config = newConfig
	}

	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	logger.Debug("EventFilter", "Initialized with command prefix: %s", p.config.CommandPrefix)
	return nil
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.RLock()
	if p.running {
		p.mu.RUnlock()
		return fmt.Errorf("eventfilter plugin already running")
	}
	p.mu.RUnlock()

	// Create database schema in background to avoid blocking startup
	go func() {
		// Wait a moment to allow other plugins to start first
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()

		select {
		case <-p.ctx.Done():
			return
		case <-timer.C:
			if err := p.createSchema(); err != nil {
				logger.Error("EventFilter", "Failed to create schema: %v", err)
				// Don't fail plugin startup, it can retry later
			}
		}
	}()

	// Subscribe to command registration events first
	if err := p.eventBus.Subscribe("command.register", p.handleRegisterEvent); err != nil {
		return fmt.Errorf("failed to subscribe to register events: %w", err)
	}

	// Use a channel to signal readiness instead of sleep
	ready := make(chan struct{})
	go func() {
		// Signal readiness after a brief moment for registrations
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		close(ready)
	}()
	<-ready

	// Subscribe to all Cytube events for routing
	err := p.eventBus.Subscribe("cytube.event", p.handleCytubeEvent)
	if err != nil {
		return fmt.Errorf("failed to subscribe to cytube events: %w", err)
	}

	// Subscribe to chat messages specifically for command parsing
	err = p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to chat events: %w", err)
	}

	// Subscribe to PM messages for command parsing
	err = p.eventBus.Subscribe(eventbus.EventCytubePM, p.handlePMMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to PM events: %w", err)
	}

	// Subscribe to plugin responses for routing
	err = p.eventBus.Subscribe(eventbus.EventPluginResponse, p.handlePluginResponse)
	if err != nil {
		return fmt.Errorf("failed to subscribe to plugin response events: %w", err)
	}

	// Load admin users from file
	p.loadAdminUsers()

	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	// Signal that the plugin is ready
	close(p.readyChan)

	logger.Debug("EventFilter", "Started unified event filtering and command routing")
	return nil
}

// Stop gracefully shuts down the plugin
func (p *Plugin) Stop() error {
	p.mu.RLock()
	if !p.running {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	logger.Info("EventFilter", "Stopping eventfilter plugin")
	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	logger.Info("EventFilter", "EventFilter plugin stopped")
	return nil
}

// HandleEvent processes incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	// This is called by the event bus when events arrive
	// The actual handling is done in the specific handler methods
	return nil
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state := "stopped"
	if p.running {
		state = "running"
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: state,
	}
}

// createSchema creates the database tables for the eventfilter plugin
func (p *Plugin) createSchema() error {
	schema := `
		-- Event routing rules table
		CREATE TABLE IF NOT EXISTS daz_eventfilter_rules (
			id SERIAL PRIMARY KEY,
			event_type VARCHAR(255) NOT NULL,
			target_plugin VARCHAR(255) NOT NULL,
			conditions JSONB,
			priority INT DEFAULT 0,
			enabled BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_event_type ON daz_eventfilter_rules(event_type);

		-- Command registry table (from commandrouter)
		CREATE TABLE IF NOT EXISTS daz_eventfilter_commands (
			id SERIAL PRIMARY KEY,
			command VARCHAR(100) NOT NULL,
			plugin_name VARCHAR(100) NOT NULL,
			is_alias BOOLEAN DEFAULT FALSE,
			primary_command VARCHAR(100),
			min_rank INT DEFAULT 0,
			enabled BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(command)
		);

		-- Command execution history (from commandrouter)
		CREATE TABLE IF NOT EXISTS daz_eventfilter_history (
			id BIGSERIAL PRIMARY KEY,
			command VARCHAR(100) NOT NULL,
			username VARCHAR(255) NOT NULL,
			channel VARCHAR(255) NOT NULL,
			args TEXT[],
			executed_at TIMESTAMP DEFAULT NOW(),
			success BOOLEAN DEFAULT TRUE,
			error_message TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_eventfilter_history_time ON daz_eventfilter_history(executed_at);
		CREATE INDEX IF NOT EXISTS idx_eventfilter_history_user ON daz_eventfilter_history(username, executed_at);
	`

	if err := p.sqlClient.Exec(schema); err != nil {
		// Emit failure event for retry
		p.emitFailureEvent("eventfilter.database.failed", "schema-creation", "database_schema", err)
		return err
	}
	return nil
}

// handleCytubeEvent routes Cytube events to appropriate plugins
func (p *Plugin) handleCytubeEvent(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return fmt.Errorf("invalid event type for cytube event")
	}

	// Apply routing rules
	for _, rule := range p.config.RoutingRules {
		if !rule.Enabled {
			continue
		}

		if matchesEventType(dataEvent.EventType, rule.EventType) {
			p.routeToPlugin(rule.TargetPlugin, dataEvent)
		}
	}

	return nil
}

// handleChatMessage specifically handles chat messages for command detection
func (p *Plugin) handleChatMessage(event framework.Event) error {
	// Extract the DataEvent which contains EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return fmt.Errorf("invalid event type for chat message")
	}

	// Check if EventData and ChatMessage are present
	if dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return fmt.Errorf("no chat message data in event")
	}

	chatData := dataEvent.Data.ChatMessage

	// Ignore messages older than 10 seconds to prevent processing historical messages
	if chatData.MessageTime > 0 {
		messageAge := messageAgeMillis(chatData.MessageTime)
		if messageAge > 10000 { // 10 seconds in milliseconds
			logger.Debug("EventFilter", "Ignoring old message from %s (age: %dms)",
				chatData.Username, messageAge)
			return nil
		}
	}

	logger.Debug("EventFilter", "Received chat message from user: %s",
		chatData.Username)

	// Check if message is a command
	if strings.HasPrefix(chatData.Message, p.config.CommandPrefix) {
		p.handleCommand(dataEvent, *chatData)
	}

	return nil
}

// handleCommand processes detected commands
func (p *Plugin) handleCommand(event *framework.DataEvent, chatData framework.ChatMessageData) {
	// Call the new context-aware handler with isFromPM = false
	p.handleCommandWithContext(event, chatData, false)
}

// handleRegisterEvent handles command registration from plugins
func (p *Plugin) handleRegisterEvent(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.KeyValue == nil {
		return nil
	}

	// Extract registration data
	commands := req.Data.KeyValue["commands"]
	pluginName := req.From
	minRankStr := req.Data.KeyValue["min_rank"]

	if commands == "" || pluginName == "" {
		return nil
	}

	minRank := 0
	if minRankStr != "" {
		var err error
		minRank, err = parseRank(minRankStr)
		if err != nil {
			logger.Warn("EventFilter", "Invalid min_rank: %v", err)
		}
	}

	// Register each command
	cmdList := strings.Split(commands, ",")
	for i, cmd := range cmdList {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		info := &CommandInfo{
			PluginName: pluginName,
			MinRank:    minRank,
			Enabled:    true,
		}

		// First command is primary, rest are aliases
		if i > 0 {
			info.IsAlias = true
			info.PrimaryCommand = strings.TrimSpace(cmdList[0])
		}

		p.mu.Lock()
		p.commandRegistry[cmd] = info
		p.mu.Unlock()

		// Save to database
		if err := p.saveCommand(cmd, info); err != nil {
			logger.Error("EventFilter", "Failed to save command %s: %v", cmd, err)
			// Emit failure event for retry
			p.emitFailureEvent("eventfilter.database.failed", req.ID, "command_registration", err)
		}

		logger.Info("EventFilter", "Registered command '%s' for plugin '%s'", cmd, pluginName)
	}

	return nil
}

// routeToPlugin forwards an event to a specific plugin
func (p *Plugin) routeToPlugin(targetPlugin string, event *framework.DataEvent) {
	// Forward the event data directly
	eventData := event.Data

	// If no existing data, create new
	if eventData == nil {
		eventData = &framework.EventData{}
	}

	// Add routing metadata
	if eventData.KeyValue == nil {
		eventData.KeyValue = make(map[string]string)
	}
	eventData.KeyValue["event_type"] = event.EventType
	eventData.KeyValue["timestamp"] = event.EventTime.Format(time.RFC3339)

	// Determine appropriate event type for target
	eventType := fmt.Sprintf("plugin.%s.event", targetPlugin)

	err := p.eventBus.Send(targetPlugin, eventType, eventData)
	if err != nil {
		logger.Error("EventFilter", "Failed to route event to %s: %v", targetPlugin, err)
		// Emit failure event for retry
		correlationID := fmt.Sprintf("route-%s-%d", targetPlugin, time.Now().UnixNano())
		p.emitFailureEvent("eventfilter.routing.failed", correlationID, "event_routing", err)
	}
}

// Database operations

func (p *Plugin) saveCommand(command string, info *CommandInfo) error {
	query := `
		INSERT INTO daz_eventfilter_commands (command, plugin_name, is_alias, primary_command, min_rank, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (command) DO UPDATE SET
			plugin_name = EXCLUDED.plugin_name,
			is_alias = EXCLUDED.is_alias,
			primary_command = EXCLUDED.primary_command,
			min_rank = EXCLUDED.min_rank,
			enabled = EXCLUDED.enabled
	`

	primaryCmd := sql.NullString{
		String: info.PrimaryCommand,
		Valid:  info.IsAlias,
	}

	return p.sqlClient.Exec(query,
		command,
		info.PluginName,
		info.IsAlias,
		primaryCmd,
		info.MinRank,
		info.Enabled)
}

func (p *Plugin) logCommand(command, username, channel string, args []string) error {
	query := `
		INSERT INTO daz_eventfilter_history (command, username, channel, args)
		VALUES ($1, $2, $3, $4)
	`

	return p.sqlClient.Exec(query,
		command,
		username,
		channel,
		args)
}

// Helper functions

// matchesEventType checks if an event type matches a pattern
func matchesEventType(eventType, pattern string) bool {
	// Simple pattern matching with wildcard support
	if pattern == "*" {
		return true
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(eventType, prefix)
	}

	return eventType == pattern
}

// parseRank converts a string rank to integer
func parseRank(rankStr string) (int, error) {
	var rank int
	_, err := fmt.Sscanf(rankStr, "%d", &rank)
	return rank, err
}

// getDefaultRoutingRules returns the default routing configuration
func getDefaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		// Route user events to user tracking plugin
		{
			EventType:    "cytube.event.userJoin",
			TargetPlugin: "usertracker",
			Priority:     10,
			Enabled:      true,
		},
		{
			EventType:    "cytube.event.userLeave",
			TargetPlugin: "usertracker",
			Priority:     10,
			Enabled:      true,
		},
		// Route media events to media tracking plugin
		{
			EventType:    "cytube.event.changeMedia",
			TargetPlugin: "mediatracker",
			Priority:     10,
			Enabled:      true,
		},
		// Route all events to analytics plugin
		{
			EventType:    "cytube.event.*",
			TargetPlugin: "analytics",
			Priority:     5,
			Enabled:      true,
		},
	}
}

// loadAdminUsers loads admin users from the admin_users.json file
func (p *Plugin) loadAdminUsers() {
	// Try to load from admin_users.json
	data, err := os.ReadFile("admin_users.json")
	if err != nil {
		// If file doesn't exist, fall back to config
		if os.IsNotExist(err) {
			logger.Info("EventFilter", "admin_users.json not found, using config admin_users")
			p.addConfiguredAdmins()
			return
		}
		logger.Warn("EventFilter", "Failed to read admin_users.json: %v", err)
		p.addConfiguredAdmins()
		return
	}

	// Parse the JSON file
	var adminConfig struct {
		AdminUsers []string `json:"admin_users"`
	}
	if err := json.Unmarshal(data, &adminConfig); err != nil {
		logger.Error("EventFilter", "Failed to parse admin_users.json: %v", err)
		p.addConfiguredAdmins()
		return
	}

	// Populate admin users map
	for _, user := range adminConfig.AdminUsers {
		p.adminUsers[strings.ToLower(user)] = true
	}

	logger.Info("EventFilter", "Loaded %d admin users from admin_users.json", len(p.adminUsers))
}

func (p *Plugin) addConfiguredAdmins() {
	for _, user := range p.config.AdminUsers {
		p.adminUsers[strings.ToLower(user)] = true
	}
}

func normalizeMessageTimeMillis(messageTime int64) int64 {
	if messageTime == 0 {
		return 0
	}
	if messageTime < 1_000_000_000_000 {
		return messageTime * 1000
	}
	return messageTime
}

func messageAgeMillis(messageTime int64) int64 {
	normalized := normalizeMessageTimeMillis(messageTime)
	if normalized == 0 {
		return 0
	}
	return time.Now().UnixMilli() - normalized
}

// isAdmin checks if a user is an admin
func (p *Plugin) isAdmin(username string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.adminUsers[strings.ToLower(username)]
}

// handlePMMessage handles private messages for command detection
func (p *Plugin) handlePMMessage(event framework.Event) error {
	// Extract the DataEvent which contains EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return fmt.Errorf("invalid event type for PM message")
	}

	// Check if EventData and PrivateMessage are present
	if dataEvent.Data == nil || dataEvent.Data.PrivateMessage == nil {
		return fmt.Errorf("no PM message data in event")
	}

	pmData := dataEvent.Data.PrivateMessage

	// Ignore messages older than 10 seconds
	if pmData.MessageTime > 0 {
		messageAge := messageAgeMillis(pmData.MessageTime)
		if messageAge > 10000 {
			logger.Debug("EventFilter", "Ignoring old PM from %s (age: %dms)",
				pmData.FromUser, messageAge)
			return nil
		}
	}

	logger.Debug("EventFilter", "Received PM: '%s' from user: %s",
		pmData.Message, pmData.FromUser)

	// Check if message is a command
	if strings.HasPrefix(pmData.Message, p.config.CommandPrefix) {
		// Create a ChatMessageData from PM data to reuse handleCommand
		chatData := framework.ChatMessageData{
			Username:    pmData.FromUser,
			Message:     pmData.Message,
			UserRank:    0, // PMs don't include rank, assume 0
			Channel:     pmData.Channel,
			MessageTime: normalizeMessageTimeMillis(pmData.MessageTime),
		}

		// Handle the command with PM flag
		p.handleCommandWithContext(dataEvent, chatData, true)
	}

	return nil
}

// handleCommandWithContext processes commands with context about their source
func (p *Plugin) handleCommandWithContext(event *framework.DataEvent, chatData framework.ChatMessageData, isFromPM bool) {
	// Remove command prefix and parse
	cmdText := strings.TrimPrefix(chatData.Message, p.config.CommandPrefix)
	parts := strings.Fields(cmdText)

	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	args := parts[1:]

	logger.Debug("EventFilter", "Detected command: %s with args: %v from user: %s (PM: %v)",
		cmdName, args, chatData.Username, isFromPM)

	// Load registry if needed
	p.mu.RLock()
	if !p.registryLoaded {
		p.mu.RUnlock()
		p.mu.Lock()
		p.registryLoaded = true
		p.mu.Unlock()
		p.mu.RLock()
	}
	cmdInfo, exists := p.commandRegistry[cmdName]
	p.mu.RUnlock()

	if !exists {
		logger.Warn("EventFilter", "Unknown command: %s", cmdName)
		// Emit failure event for unknown command
		correlationID := fmt.Sprintf("unknown-%s-%d", cmdName, time.Now().UnixNano())
		err := fmt.Errorf("unknown command: %s", cmdName)
		p.emitFailureEvent("eventfilter.command.error", correlationID, "unknown_command", err)
		return
	}

	// Check if command is enabled
	if !cmdInfo.Enabled {
		logger.Debug("EventFilter", "Command disabled: %s", cmdName)
		// Emit failure event for disabled command
		correlationID := fmt.Sprintf("disabled-%s-%d", cmdName, time.Now().UnixNano())
		err := fmt.Errorf("command disabled: %s", cmdName)
		p.emitFailureEvent("eventfilter.command.error", correlationID, "disabled_command", err)
		return
	}

	// Check user rank (skip for PMs from admins)
	if !isFromPM || !p.isAdmin(chatData.Username) {
		if chatData.UserRank < cmdInfo.MinRank {
			logger.Debug("EventFilter", "User %s lacks rank for command %s (has: %d, needs: %d)",
				chatData.Username, cmdName, chatData.UserRank, cmdInfo.MinRank)
			// Emit failure event for insufficient rank
			correlationID := fmt.Sprintf("rank-%s-%d", cmdName, time.Now().UnixNano())
			err := fmt.Errorf("insufficient rank for command %s: user has %d, needs %d", cmdName, chatData.UserRank, cmdInfo.MinRank)
			p.emitFailureEvent("eventfilter.command.error", correlationID, "insufficient_rank", err)
			return
		}
	}

	// Get target plugin
	targetPlugin := cmdInfo.PluginName
	if cmdInfo.IsAlias && cmdInfo.PrimaryCommand != "" {
		// For aliases, route to the primary command's plugin
		if primaryInfo, ok := p.commandRegistry[cmdInfo.PrimaryCommand]; ok {
			targetPlugin = primaryInfo.PluginName
		}
	}

	// Create command execution event for the target plugin
	cmdData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "eventfilter",
			To:   targetPlugin,
			Type: "execute",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: cmdName,
					Args: args,
					Params: map[string]string{
						"username": chatData.Username,
						"rank":     fmt.Sprintf("%d", chatData.UserRank),
						"channel":  chatData.Channel,
						"is_pm":    fmt.Sprintf("%t", isFromPM),
						"is_admin": fmt.Sprintf("%t", p.isAdmin(chatData.Username)),
					},
				},
			},
		},
	}

	// Send to specific plugin
	eventType := fmt.Sprintf("command.%s.execute", targetPlugin)
	logger.Debug("EventFilter", "Routing command %s to %s via %s", cmdName, targetPlugin, eventType)

	// Log command execution
	go func() {
		if err := p.logCommand(cmdName, chatData.Username, chatData.Channel, args); err != nil {
			logger.Warn("EventFilter", "Failed to log command execution: %v", err)
			// Emit failure event for retry
			correlationID := fmt.Sprintf("cmdlog-%s-%d", cmdName, time.Now().UnixNano())
			p.emitFailureEvent("eventfilter.database.failed", correlationID, "command_logging", err)
		}
	}()

	err := p.eventBus.Broadcast(eventType, cmdData)
	if err != nil {
		logger.Error("EventFilter", "Failed to route command: %v", err)
		// Emit failure event for retry
		correlationID := fmt.Sprintf("cmd-%s-%d", cmdName, time.Now().UnixNano())
		p.emitFailureEvent("eventfilter.command.failed", correlationID, "command_routing", err)
	}
}

// handlePluginResponse processes plugin response events and routes them appropriately
func (p *Plugin) handlePluginResponse(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return fmt.Errorf("invalid event type for plugin response")
	}

	// Check if EventData and PluginResponse are present
	if dataEvent.Data == nil || dataEvent.Data.PluginResponse == nil {
		return fmt.Errorf("no plugin response data in event")
	}

	response := dataEvent.Data.PluginResponse

	logger.Debug("EventFilter", "Processing plugin response from %s (success: %v)",
		response.From, response.Success)

	// Handle different types of plugin responses
	if response.Data != nil && response.Data.CommandResult != nil {
		// This is a command response, route it as a PM
		return p.handleCommandResponse(response)
	}

	// Handle error responses
	if !response.Success && response.Error != "" {
		logger.Warn("EventFilter", "Plugin response error from %s: %s",
			response.From, response.Error)
		return p.handleErrorResponse(response)
	}

	// Log other response types for debugging
	if response.Data != nil && response.Data.KeyValue != nil {
		logger.Debug("EventFilter", "Plugin response from %s with key-value data: %v",
			response.From, response.Data.KeyValue)
	}

	return nil
}

// handleCommandResponse processes command responses and sends them as PMs
func (p *Plugin) handleCommandResponse(response *framework.PluginResponse) error {
	cmdResult := response.Data.CommandResult
	if cmdResult == nil {
		return fmt.Errorf("no command result in response")
	}

	// Extract user information from response data
	var username, channel string
	if response.Data.KeyValue != nil {
		username = response.Data.KeyValue["username"]
		channel = response.Data.KeyValue["channel"]
	}

	// If we don't have user info, try to extract from command result
	if username == "" {
		logger.Warn("EventFilter", "No username found in command response from %s", response.From)
		return nil
	}

	// Prepare the message
	var message string
	if cmdResult.Success {
		message = cmdResult.Output
	} else {
		message = fmt.Sprintf("Command failed: %s", cmdResult.Error)
	}

	// Send as PM
	pmData := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  username,
			Message: message,
			Channel: channel,
		},
	}

	logger.Debug("EventFilter", "Routing command response from %s to PM for user %s",
		response.From, username)

	if err := p.eventBus.Broadcast("cytube.send.pm", pmData); err != nil {
		logger.Error("EventFilter", "Failed to send command response PM: %v", err)
		// Emit failure event for retry
		p.emitFailureEvent("eventfilter.pm.failed", response.ID, "pm_response", err)
		return err
	}

	return nil
}

// handleErrorResponse processes error responses from plugins
func (p *Plugin) handleErrorResponse(response *framework.PluginResponse) error {
	// Extract user information if available
	var username, channel string
	if response.Data != nil && response.Data.KeyValue != nil {
		username = response.Data.KeyValue["username"]
		channel = response.Data.KeyValue["channel"]
	}

	// If we have user information, send error as PM
	if username != "" {
		errorMessage := fmt.Sprintf("Error from %s: %s", response.From, response.Error)

		pmData := &framework.EventData{
			PrivateMessage: &framework.PrivateMessageData{
				ToUser:  username,
				Message: errorMessage,
				Channel: channel,
			},
		}

		logger.Debug("EventFilter", "Routing error response from %s to PM for user %s",
			response.From, username)

		if err := p.eventBus.Broadcast("cytube.send.pm", pmData); err != nil {
			logger.Error("EventFilter", "Failed to send error response PM: %v", err)
			// Emit failure event for retry
			p.emitFailureEvent("eventfilter.pm.failed", response.ID, "pm_response", err)
			return err
		}
	}

	return nil
}

// emitFailureEvent emits a failure event for the retry mechanism
func (p *Plugin) emitFailureEvent(eventType, correlationID, operationType string, err error) {
	failureData := &framework.EventData{
		KeyValue: map[string]string{
			"correlation_id": correlationID,
			"source":         p.name,
			"operation_type": operationType,
			"error":          err.Error(),
			"timestamp":      time.Now().Format(time.RFC3339),
		},
	}

	// Emit failure event asynchronously
	go func() {
		if err := p.eventBus.Broadcast(eventType, failureData); err != nil {
			logger.Warn("EventFilter", "Failed to emit failure event: %v", err)
		}
	}()
}
