package help

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
)

type Config struct {
	ShowAliases bool `json:"show_aliases"`
}

type Plugin struct {
	name     string
	eventBus framework.EventBus
	config   *Config
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  bool
}

func New() framework.Plugin {
	return &Plugin{
		name: "help",
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
			ShowAliases: true,
		}
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

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

	// Subscribe to help command execution events
	if err := p.eventBus.Subscribe("command.help.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	// Register our command with the command router
	p.registerCommand()

	log.Printf("[INFO] Help plugin started: %s", p.name)
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

	log.Printf("[INFO] Help plugin stopped: %s", p.name)
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// This is called by the event bus when events arrive
	// The actual handling is done in the handleCommand method
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

func (p *Plugin) registerCommand() {
	// Send registration event to command router
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "commandrouter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "help,h,commands",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		log.Printf("[ERROR] Failed to register help command: %v", err)
	}
}

func (p *Plugin) handleCommand(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	// Handle DataEvent
	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	// Handle asynchronously
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.handleHelpCommand(dataEvent.Data.PluginRequest)
	}()

	return nil
}

func (p *Plugin) handleHelpCommand(req *framework.PluginRequest) {
	// Query command registry from database
	query := `
		SELECT command, plugin_name, is_alias, primary_command, min_rank
		FROM daz_command_registry
		WHERE enabled = true
		ORDER BY plugin_name, is_alias, command
	`

	rows, err := p.eventBus.Query(query)
	if err != nil {
		log.Printf("[ERROR] Failed to query command registry: %v", err)
		p.sendResponse(req, "Error: Unable to retrieve command list")
		return
	}
	defer rows.Close()

	// Build help message
	var lines []string
	lines = append(lines, "Available Commands:")
	lines = append(lines, "")

	currentPlugin := ""
	var commands []string

	for rows.Next() {
		var cmd, pluginName string
		var isAlias bool
		var primaryCmd *string
		var minRank int

		if err := rows.Scan(&cmd, &pluginName, &isAlias, &primaryCmd, &minRank); err != nil {
			continue
		}

		// Skip aliases if configured to not show them
		if isAlias && !p.config.ShowAliases {
			continue
		}

		// New plugin section
		if pluginName != currentPlugin {
			if currentPlugin != "" && len(commands) > 0 {
				lines = append(lines, fmt.Sprintf("%s: %s", currentPlugin, commands))
				commands = nil
			}
			currentPlugin = pluginName
		}

		// Format command
		cmdStr := fmt.Sprintf("!%s", cmd)
		if minRank > 0 {
			cmdStr = fmt.Sprintf("%s (rank %d+)", cmdStr, minRank)
		}
		if isAlias && primaryCmd != nil {
			cmdStr = fmt.Sprintf("%s → !%s", cmdStr, *primaryCmd)
		}

		commands = append(commands, cmdStr)
	}

	// Add last plugin's commands
	if currentPlugin != "" && len(commands) > 0 {
		lines = append(lines, fmt.Sprintf("%s: %s", currentPlugin, commands))
	}

	lines = append(lines, "")
	lines = append(lines, "Use !about for more information")

	message := ""
	for _, line := range lines {
		message += line + "\n"
	}

	p.sendResponse(req, message)
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	// Create a simple text response to send back to chat
	response := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: req.Data.Command.Params["channel"],
		},
	}

	// Send to core plugin to output to chat
	if err := p.eventBus.Send("core", "cytube.send", response); err != nil {
		log.Printf("[ERROR] Failed to send help response: %v", err)
	}
}
