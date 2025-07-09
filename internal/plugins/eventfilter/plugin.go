package eventfilter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the unified event filter plugin for event routing and command processing
type Plugin struct {
	name     string
	eventBus framework.EventBus
	config   *Config
	running  bool
	mu       sync.RWMutex

	// Command registry
	commandRegistry map[string]*CommandInfo
	cooldowns       map[string]time.Time
	registryLoaded  bool

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

// NewPlugin creates a new eventfilter plugin instance (deprecated - use New)
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

// Initialize sets up the plugin with the event bus (deprecated - use Init)
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
	p.ctx, p.cancel = context.WithCancel(context.Background())

	log.Printf("[EventFilter] Initialized with command prefix: %s", p.config.CommandPrefix)
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

	// Create database schema now that database is connected
	if err := p.createSchema(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

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

	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	// Signal that the plugin is ready
	close(p.readyChan)

	log.Println("[EventFilter] Started unified event filtering and command routing")
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

	log.Println("[EventFilter] Stopping eventfilter plugin")
	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	log.Println("[EventFilter] EventFilter plugin stopped")
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

	err := p.eventBus.Exec(schema)
	return err
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

	log.Printf("[EventFilter] Received chat message: '%s' from user: %s (prefix: '%s')",
		chatData.Message, chatData.Username, p.config.CommandPrefix)

	// Check if message is a command
	if strings.HasPrefix(chatData.Message, p.config.CommandPrefix) {
		p.handleCommand(dataEvent, *chatData)
	}

	return nil
}

// handleCommand processes detected commands
func (p *Plugin) handleCommand(event *framework.DataEvent, chatData framework.ChatMessageData) {
	// Remove command prefix and parse
	cmdText := strings.TrimPrefix(chatData.Message, p.config.CommandPrefix)
	parts := strings.Fields(cmdText)

	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	args := parts[1:]

	log.Printf("[EventFilter] Detected command: %s with args: %v from user: %s", cmdName, args, chatData.Username)

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
		log.Printf("[EventFilter] Unknown command: %s", cmdName)
		return
	}

	// Check if command is enabled
	if !cmdInfo.Enabled {
		log.Printf("[EventFilter] Command disabled: %s", cmdName)
		return
	}

	// Check user rank
	if chatData.UserRank < cmdInfo.MinRank {
		log.Printf("[EventFilter] User %s lacks rank for command %s (has: %d, needs: %d)",
			chatData.Username, cmdName, chatData.UserRank, cmdInfo.MinRank)
		return
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
					},
				},
			},
		},
	}

	// Send to specific plugin
	eventType := fmt.Sprintf("command.%s.execute", targetPlugin)
	log.Printf("[EventFilter] Routing command %s to %s via %s", cmdName, targetPlugin, eventType)

	// Log command execution
	go func() {
		if err := p.logCommand(cmdName, chatData.Username, chatData.Channel, args); err != nil {
			log.Printf("[EventFilter] Failed to log command execution: %v", err)
		}
	}()

	err := p.eventBus.Broadcast(eventType, cmdData)
	if err != nil {
		log.Printf("[EventFilter] Failed to route command: %v", err)
	}
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
			log.Printf("[EventFilter] Invalid min_rank: %v", err)
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
			log.Printf("[EventFilter] Failed to save command %s: %v", cmd, err)
		}

		log.Printf("[EventFilter] Registered command '%s' for plugin '%s'", cmd, pluginName)
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
		log.Printf("[EventFilter] Failed to route event to %s: %v", targetPlugin, err)
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

	err := p.eventBus.Exec(query,
		framework.SQLParam{Value: command},
		framework.SQLParam{Value: info.PluginName},
		framework.SQLParam{Value: info.IsAlias},
		framework.SQLParam{Value: primaryCmd},
		framework.SQLParam{Value: info.MinRank},
		framework.SQLParam{Value: info.Enabled})
	return err
}

func (p *Plugin) logCommand(command, username, channel string, args []string) error {
	query := `
		INSERT INTO daz_eventfilter_history (command, username, channel, args)
		VALUES ($1, $2, $3, $4)
	`

	err := p.eventBus.Exec(query,
		framework.SQLParam{Value: command},
		framework.SQLParam{Value: username},
		framework.SQLParam{Value: channel},
		framework.SQLParam{Value: args})
	return err
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
