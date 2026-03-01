package help

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	ShowAliases bool `json:"show_aliases"`
}

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	config    *Config
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	running   bool
}

func New() framework.Plugin {
	return &Plugin{
		name: "help",
	}
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)

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
		// Emit failure event for retry
		p.emitFailureEvent("command.help.failed", "subscription", "event_subscription", err)
		return fmt.Errorf("failed to subscribe to command events: %w", err)
	}

	// Register our command with the command router
	p.registerCommand()

	logger.Debug(p.name, "Started")
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

	logger.Debug(p.name, "Stopped")
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

func (p *Plugin) Name() string {
	return "help"
}

func (p *Plugin) registerCommand() {
	// Send registration event to command router
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
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
		logger.Error(p.name, "Failed to register help command: %v", err)
		// Emit failure event for retry
		p.emitFailureEvent("command.help.failed", "registration", "command_registration", err)
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
	if req.Data == nil || req.Data.Command == nil {
		return
	}

	commandArgs := req.Data.Command.Args
	if len(commandArgs) > 0 {
		p.handleCommandHelp(req, strings.ToLower(commandArgs[0]))
		return
	}

	messageBatches, err := p.buildCommandList(req)
	if err != nil {
		logger.Error(p.name, "Failed to build help list: %v", err)
		p.sendResponse(req, "sorry mate, help list is crook right now")
		return
	}

	for _, msg := range messageBatches {
		p.sendResponse(req, msg)
	}
}

func (p *Plugin) handleCommandHelp(req *framework.PluginRequest, command string) {
	command = strings.TrimPrefix(command, "!")
	if command == "" {
		p.sendResponse(req, "tell us the command name ya pelican")
		return
	}

	info, aliases, err := p.lookupCommand(req, command)
	if err != nil {
		logger.Error(p.name, "Failed to load command info: %v", err)
		p.sendResponse(req, "sorry mate, help is crook right now")
		return
	}
	if info == nil {
		p.sendResponse(req, fmt.Sprintf("no idea about '%s'", command))
		return
	}

	aliasText := "none"
	if len(aliases) > 0 {
		aliasText = strings.Join(aliases, ", ")
	}

	message := fmt.Sprintf("!%s (plugin: %s, min rank: %d, aliases: %s)", info.Primary, info.PluginName, info.MinRank, aliasText)
	p.sendResponse(req, p.limit(message))
}

type commandEntry struct {
	Primary    string
	PluginName string
	MinRank    int
	Aliases    []string
}

func (p *Plugin) buildCommandList(req *framework.PluginRequest) ([]string, error) {
	userRank := 0
	if req.Data != nil && req.Data.Command != nil {
		if rankStr := req.Data.Command.Params["rank"]; rankStr != "" {
			if parsed, err := strconv.Atoi(rankStr); err == nil {
				userRank = parsed
			}
		}
	}

	commands, err := p.fetchCommands(userRank)
	if err != nil {
		return nil, err
	}

	entries := make([]*commandEntry, 0, len(commands))
	for _, entry := range commands {
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Primary < entries[j].Primary
	})

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		aliasText := ""
		if p.config.ShowAliases && len(entry.Aliases) > 0 {
			aliasText = fmt.Sprintf(" (aliases: %s)", strings.Join(entry.Aliases, ", "))
		}
		line := fmt.Sprintf("!%s - plugin: %s, min rank: %d%s", entry.Primary, entry.PluginName, entry.MinRank, aliasText)
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return []string{"no commands available"}, nil
	}

	return splitMessages("Commands:", lines, 450), nil
}

func (p *Plugin) fetchCommands(userRank int) (map[string]*commandEntry, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT command, plugin_name, is_alias, COALESCE(primary_command, ''), min_rank, enabled
		FROM daz_eventfilter_commands
		WHERE enabled = true AND min_rank <= $1
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, userRank)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	commands := make(map[string]*commandEntry)
	for rows.Next() {
		var (
			command    string
			pluginName string
			isAlias    bool
			primary    string
			minRank    int
			enabled    bool
		)

		if err := rows.Scan(&command, &pluginName, &isAlias, &primary, &minRank, &enabled); err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}

		command = strings.ToLower(strings.TrimSpace(command))
		primary = strings.ToLower(strings.TrimSpace(primary))
		if command == "" {
			continue
		}
		if !isAlias || primary == "" {
			primary = command
		}

		entry, ok := commands[primary]
		if !ok {
			entry = &commandEntry{Primary: primary, PluginName: pluginName, MinRank: minRank}
			commands[primary] = entry
		}
		if isAlias && command != primary {
			entry.Aliases = append(entry.Aliases, command)
		}
	}

	for _, entry := range commands {
		sort.Strings(entry.Aliases)
	}

	return commands, rows.Err()
}

func (p *Plugin) lookupCommand(req *framework.PluginRequest, command string) (*commandEntry, []string, error) {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return nil, nil, nil
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT command, plugin_name, is_alias, COALESCE(primary_command, ''), min_rank, enabled
		FROM daz_eventfilter_commands
		WHERE command = $1 AND enabled = true
		LIMIT 1
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, command)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil, nil
	}

	var (
		cmd        string
		pluginName string
		isAlias    bool
		primary    string
		minRank    int
		enabled    bool
	)

	if err := rows.Scan(&cmd, &pluginName, &isAlias, &primary, &minRank, &enabled); err != nil {
		return nil, nil, err
	}
	if !enabled {
		return nil, nil, nil
	}

	cmd = strings.ToLower(strings.TrimSpace(cmd))
	primary = strings.ToLower(strings.TrimSpace(primary))
	if cmd == "" {
		return nil, nil, nil
	}
	if !isAlias || primary == "" {
		primary = cmd
	}

	aliases, err := p.fetchAliases(ctx, primary)
	if err != nil {
		return nil, nil, err
	}
	return &commandEntry{Primary: primary, PluginName: pluginName, MinRank: minRank, Aliases: aliases}, aliases, nil
}

func (p *Plugin) fetchAliases(ctx context.Context, primary string) ([]string, error) {
	query := `
		SELECT command
		FROM daz_eventfilter_commands
		WHERE enabled = true AND is_alias = true AND primary_command = $1
		ORDER BY command ASC
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, primary)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	aliases := []string{}
	for rows.Next() {
		var cmd string
		if err := rows.Scan(&cmd); err != nil {
			return nil, err
		}
		cmd = strings.ToLower(strings.TrimSpace(cmd))
		if cmd != "" {
			aliases = append(aliases, cmd)
		}
	}

	return aliases, rows.Err()
}

func (p *Plugin) limit(message string) string {
	const maxRunes = 500
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= maxRunes {
		return message
	}
	return string(runes[:maxRunes]) + "..."
}

func splitMessages(header string, lines []string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = 450
	}

	header = strings.TrimSpace(header)
	current := header
	if current != "" {
		current += "\n"
	}

	var messages []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		candidate := current + line
		if len([]rune(candidate)) > maxLen && current != "" {
			messages = append(messages, strings.TrimSpace(current))
			current = line + "\n"
			continue
		}
		current = candidate + "\n"
	}

	if strings.TrimSpace(current) != "" {
		messages = append(messages, strings.TrimSpace(current))
	}

	return messages
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	// Send response via plugin response system
	username := req.Data.Command.Params["username"]
	channel := req.Data.Command.Params["channel"]

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
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

	// Broadcast to plugin.response event for routing
	if err := p.eventBus.Broadcast("plugin.response", response); err != nil {
		logger.Error(p.name, "Failed to send help plugin response: %v", err)
		// Emit failure event for retry
		p.emitFailureEvent("command.help.failed", req.ID, "response_delivery", err)
	}
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
			logger.Debug(p.name, "Failed to emit failure event: %v", err)
		}
	}()
}
