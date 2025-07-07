package commandrouter

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

type Config struct {
	DefaultCooldown int      `json:"default_cooldown"`
	AdminUsers      []string `json:"admin_users"`
}

type Plugin struct {
	name           string
	eventBus       framework.EventBus
	config         *Config
	registry       map[string]*CommandInfo
	cooldowns      map[string]time.Time
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	running        bool
	registryLoaded bool
}

type CommandInfo struct {
	PluginName     string
	PrimaryCommand string
	IsAlias        bool
	MinRank        int
	Enabled        bool
}

func New() framework.Plugin {
	return &Plugin{
		name:      "commandrouter",
		registry:  make(map[string]*CommandInfo),
		cooldowns: make(map[string]time.Time),
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	} else {
		p.config = &Config{
			DefaultCooldown: 5,
			AdminUsers:      []string{},
		}
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

	if err := p.createSchema(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Skip loading registry during init - will be loaded when needed
	// The EventBus doesn't support synchronous queries yet

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.mu.Unlock()

	if err := p.eventBus.Subscribe(eventbus.EventPluginCommand, p.HandleEvent); err != nil {
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	if err := p.eventBus.Subscribe("command.register", p.HandleEvent); err != nil {
		return fmt.Errorf("failed to subscribe to register events: %w", err)
	}

	log.Printf("[INFO] Command router plugin started: %s", p.name)
	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()

	log.Printf("[INFO] Command router plugin stopped: %s", p.name)
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		// Not a DataEvent, skip
		return nil
	}

	// Check event type
	switch event.Type() {
	case eventbus.EventPluginCommand:
		return p.handleCommandEvent(dataEvent)
	case "command.register":
		return p.handleRegisterEvent(dataEvent)
	}

	return nil
}

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

func (p *Plugin) createSchema() error {
	schema := `
		CREATE TABLE IF NOT EXISTS daz_command_registry (
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

		CREATE TABLE IF NOT EXISTS daz_command_history (
			id BIGSERIAL PRIMARY KEY,
			command VARCHAR(100) NOT NULL,
			username VARCHAR(255) NOT NULL,
			channel VARCHAR(255) NOT NULL,
			args TEXT[],
			executed_at TIMESTAMP DEFAULT NOW(),
			success BOOLEAN DEFAULT TRUE,
			error_message TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_command_history_time ON daz_command_history(executed_at);
		CREATE INDEX IF NOT EXISTS idx_command_history_user ON daz_command_history(username, executed_at);
	`

	err := p.eventBus.Exec(schema)
	return err
}

func (p *Plugin) loadRegistry() error {
	query := `
		SELECT command, plugin_name, is_alias, primary_command, min_rank, enabled
		FROM daz_command_registry
		WHERE enabled = true
	`

	rows, err := p.eventBus.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query registry: %w", err)
	}

	for rows.Next() {
		var cmd string
		var info CommandInfo
		var primaryCmd sql.NullString

		err := rows.Scan(&cmd, &info.PluginName, &info.IsAlias, &primaryCmd, &info.MinRank, &info.Enabled)
		if err != nil {
			log.Printf("[ERROR] Failed to scan command: %v", err)
			continue
		}

		if primaryCmd.Valid {
			info.PrimaryCommand = primaryCmd.String
		}

		p.registry[cmd] = &info
	}

	log.Printf("[INFO] Loaded %d commands from database", len(p.registry))
	return nil
}

func (p *Plugin) saveCommand(command string, info *CommandInfo) error {
	query := `
		INSERT INTO daz_command_registry (command, plugin_name, is_alias, primary_command, min_rank, enabled)
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

	err := p.eventBus.Exec(query, command, info.PluginName, info.IsAlias, primaryCmd, info.MinRank, info.Enabled)
	return err
}

func (p *Plugin) logCommand(command, username, channel string, args []string) error {
	query := `
		INSERT INTO daz_command_history (command, username, channel, args)
		VALUES ($1, $2, $3, $4)
	`

	err := p.eventBus.Exec(query, command, username, channel, args)
	return err
}

func parseRank(rankStr string) (int, error) {
	var rank int
	_, err := fmt.Sscanf(rankStr, "%d", &rank)
	return rank, err
}

func (p *Plugin) handleCommandEvent(event *framework.DataEvent) error {
	if event.Data == nil || event.Data.ChatMessage == nil {
		return nil
	}

	chatMsg := event.Data.ChatMessage
	log.Printf("[CommandRouter] Processing command from %s: %s", chatMsg.Username, chatMsg.Message)

	// Parse the command
	parts := strings.Fields(chatMsg.Message)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "!") {
		return nil
	}

	cmdName := strings.TrimPrefix(parts[0], "!")
	args := parts[1:]

	// Check if registry is loaded (for now we'll skip loading from DB)
	p.mu.RLock()
	if !p.registryLoaded {
		// For now, we'll just mark it as loaded and use the in-memory registry
		// This avoids the synchronous query issue
		p.mu.RUnlock()
		p.mu.Lock()
		p.registryLoaded = true
		p.mu.Unlock()
		p.mu.RLock()
	}
	cmdInfo, exists := p.registry[cmdName]
	p.mu.RUnlock()

	if !exists {
		log.Printf("[CommandRouter] Unknown command: %s", cmdName)
		return nil
	}

	// Check if command is enabled
	if !cmdInfo.Enabled {
		log.Printf("[CommandRouter] Command disabled: %s", cmdName)
		return nil
	}

	// Check user rank
	if chatMsg.UserRank < cmdInfo.MinRank {
		log.Printf("[CommandRouter] User %s lacks rank for command %s (has: %d, needs: %d)",
			chatMsg.Username, cmdName, chatMsg.UserRank, cmdInfo.MinRank)
		return nil
	}

	// Route to the appropriate plugin
	targetPlugin := cmdInfo.PluginName
	if cmdInfo.IsAlias && cmdInfo.PrimaryCommand != "" {
		// For aliases, route to the primary command's plugin
		if primaryInfo, ok := p.registry[cmdInfo.PrimaryCommand]; ok {
			targetPlugin = primaryInfo.PluginName
		}
	}

	// Create command execution event for the target plugin
	cmdData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "commandrouter",
			To:   targetPlugin,
			Type: "execute",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: cmdName,
					Args: args,
					Params: map[string]string{
						"username": chatMsg.Username,
						"rank":     fmt.Sprintf("%d", chatMsg.UserRank),
						"channel":  event.Data.KeyValue["channel"],
					},
				},
			},
		},
	}

	// Send to specific plugin
	eventType := fmt.Sprintf("command.%s.execute", targetPlugin)
	return p.eventBus.Broadcast(eventType, cmdData)
}

func (p *Plugin) handleRegisterEvent(event *framework.DataEvent) error {
	if event.Data == nil || event.Data.PluginRequest == nil {
		return nil
	}

	req := event.Data.PluginRequest
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
			log.Printf("[CommandRouter] Invalid min_rank: %v", err)
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
		p.registry[cmd] = info
		p.mu.Unlock()

		// Save to database
		if err := p.saveCommand(cmd, info); err != nil {
			log.Printf("[CommandRouter] Failed to save command %s: %v", cmd, err)
		}

		log.Printf("[CommandRouter] Registered command '%s' for plugin '%s'", cmd, pluginName)
	}

	return nil
}
