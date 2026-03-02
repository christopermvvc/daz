package help

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	ShowAliases       bool   `json:"show_aliases"`
	GenerateHTML      bool   `json:"generate_html"`
	HTMLOutputPath    string `json:"html_output_path"`
	HelpBaseURL       string `json:"help_base_url"`
	IncludeRestricted bool   `json:"include_restricted"`
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

	cacheMu      sync.RWMutex
	commandCache map[string]*commandEntry
	aliasIndex   map[string]string

	htmlDirty   atomic.Bool
	htmlDirtyCh chan struct{}
	generator   *HTMLGenerator

	cooldownMu    sync.Mutex
	lastHelpByKey map[string]time.Time
}

const helpCommandCooldown = 2 * time.Second

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
	p.commandCache = make(map[string]*commandEntry)
	p.aliasIndex = make(map[string]string)
	p.lastHelpByKey = make(map[string]time.Time)

	if len(config) > 0 {
		p.config = &Config{
			ShowAliases:       true,
			GenerateHTML:      true,
			HTMLOutputPath:    defaultHelpOutputPath,
			HelpBaseURL:       defaultHelpBaseURL,
			IncludeRestricted: true,
		}
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	} else {
		p.config = &Config{
			ShowAliases:       true,
			GenerateHTML:      true,
			HTMLOutputPath:    defaultHelpOutputPath,
			HelpBaseURL:       defaultHelpBaseURL,
			IncludeRestricted: true,
		}
	}
	p.config.HTMLOutputPath = normalizeHelpOutputPath(p.config.HTMLOutputPath)
	p.config.HelpBaseURL = normalizeHelpBaseURL(p.config.HelpBaseURL)

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.htmlDirtyCh = make(chan struct{}, 1)

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

	if err := p.eventBus.Subscribe("command.register", p.handleRegisterEvent); err != nil {
		p.emitFailureEvent("command.help.failed", "subscription", "event_subscription", err)
		return fmt.Errorf("failed to subscribe to command.register: %w", err)
	}

	if err := p.loadCacheFromDB(); err != nil {
		logger.Error(p.name, "Failed to load command cache: %v", err)
	}
	if p.config.GenerateHTML {
		entryProvider := p.snapshotAllEntries
		if !p.config.IncludeRestricted {
			entryProvider = func() []*commandEntry {
				return p.snapshotEntries(0, false)
			}
		}
		p.generator = NewHTMLGenerator(p.config, entryProvider, p.config.ShowAliases, p.config.IncludeRestricted)
		p.markHTMLDirty()
		p.wg.Add(1)
		go p.runHTMLGenerator()
	}

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
					"commands":    "help,h,commands",
					"min_rank":    "0",
					"description": "list available commands",
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

	params := req.Data.Command.Params
	if wait, ok := p.checkHelpCooldown(params["channel"], params["username"]); !ok {
		p.sendResponse(req, fmt.Sprintf("steady on mate, try again in %ds", int(wait.Seconds())+1))
		return
	}

	commandArgs := req.Data.Command.Args
	if len(commandArgs) > 0 {
		p.handleCommandHelp(req, strings.ToLower(commandArgs[0]))
		return
	}
	p.sendResponse(req, fmt.Sprintf("Help: %s", p.config.HelpBaseURL))
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

	_ = aliases
	p.sendResponse(req, fmt.Sprintf("Help: %s#%s", p.config.HelpBaseURL, info.Primary))
}

type commandEntry struct {
	Primary     string
	PluginName  string
	MinRank     int
	Aliases     []string
	AdminOnly   bool
	Description string
}

func (p *Plugin) buildCommandList(req *framework.PluginRequest) ([]string, error) {
	userRank := 0
	isAdmin := false
	if req.Data != nil && req.Data.Command != nil {
		if rankStr := req.Data.Command.Params["rank"]; rankStr != "" {
			if parsed, err := strconv.Atoi(rankStr); err == nil {
				userRank = parsed
			}
		}
		isAdmin = req.Data.Command.Params["is_admin"] == "true"
	}

	entries := p.snapshotEntries(userRank, isAdmin)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Primary < entries[j].Primary
	})

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := fmt.Sprintf("!%s - %s", entry.Primary, entry.Description)
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return []string{"no commands available"}, nil
	}

	return splitMessages("Commands:", lines, 450), nil
}

func (p *Plugin) lookupCommand(req *framework.PluginRequest, command string) (*commandEntry, []string, error) {
	command = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(command, "!")))
	if command == "" {
		return nil, nil, nil
	}

	userRank := 0
	isAdmin := false
	if req.Data != nil && req.Data.Command != nil {
		if rankStr := req.Data.Command.Params["rank"]; rankStr != "" {
			if parsed, err := strconv.Atoi(rankStr); err == nil {
				userRank = parsed
			}
		}
		isAdmin = req.Data.Command.Params["is_admin"] == "true"
	}

	entry := p.lookupEntry(command)
	if entry == nil {
		return nil, nil, nil
	}
	if entry.MinRank > userRank || (entry.AdminOnly && !isAdmin) {
		return nil, nil, nil
	}

	return entry, entry.Aliases, nil
}

func (p *Plugin) lookupEntry(command string) *commandEntry {
	p.cacheMu.RLock()
	primary, ok := p.aliasIndex[command]
	if !ok {
		primary = command
	}
	entry := p.commandCache[primary]
	if entry == nil {
		p.cacheMu.RUnlock()
		return nil
	}
	clone := *entry
	clone.Aliases = append([]string(nil), entry.Aliases...)
	p.cacheMu.RUnlock()
	return &clone
}

func (p *Plugin) snapshotEntries(userRank int, isAdmin bool) []*commandEntry {
	p.cacheMu.RLock()
	entries := make([]*commandEntry, 0, len(p.commandCache))
	for _, entry := range p.commandCache {
		if entry.Primary == "help" || entry.Primary == "greeter" || entry.PluginName == "greeter" {
			continue
		}
		if entry.MinRank > userRank {
			continue
		}
		if entry.AdminOnly && !isAdmin {
			continue
		}
		clone := *entry
		clone.Aliases = append([]string(nil), entry.Aliases...)
		if strings.TrimSpace(clone.Description) == "" {
			clone.Description = "no description yet"
		}
		entries = append(entries, &clone)
	}
	p.cacheMu.RUnlock()
	return entries
}

func (p *Plugin) snapshotAllEntries() []*commandEntry {
	p.cacheMu.RLock()
	entries := make([]*commandEntry, 0, len(p.commandCache))
	for _, entry := range p.commandCache {
		if entry.Primary == "help" || entry.Primary == "greeter" || entry.PluginName == "greeter" {
			continue
		}
		clone := *entry
		clone.Aliases = append([]string(nil), entry.Aliases...)
		if strings.TrimSpace(clone.Description) == "" {
			clone.Description = "no description yet"
		}
		entries = append(entries, &clone)
	}
	p.cacheMu.RUnlock()
	return entries
}

func (p *Plugin) handleRegisterEvent(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.KeyValue == nil {
		return nil
	}

	commandsRaw := strings.TrimSpace(req.Data.KeyValue["commands"])
	if commandsRaw == "" {
		return nil
	}

	minRank := 0
	if minRankStr := strings.TrimSpace(req.Data.KeyValue["min_rank"]); minRankStr != "" {
		if parsed, err := strconv.Atoi(minRankStr); err == nil {
			minRank = parsed
		}
	}

	adminOnlySet, adminOnlyAll := parseAdminOnly(req.Data.KeyValue["admin_only"])
	commands := strings.Split(commandsRaw, ",")
	primary := ""
	if len(commands) > 0 {
		primary = strings.ToLower(strings.TrimSpace(commands[0]))
	}

	p.cacheMu.Lock()
	for i, command := range commands {
		cmd := strings.ToLower(strings.TrimSpace(command))
		if cmd == "" {
			continue
		}
		isAlias := i > 0
		cmdPrimary := primary
		if !isAlias || cmdPrimary == "" {
			cmdPrimary = cmd
		}
		adminOnly := adminOnlyAll || adminOnlySet[cmd]
		if cmdPrimary != "" && adminOnlySet[cmdPrimary] {
			adminOnly = true
		}

		entry, ok := p.commandCache[cmdPrimary]
		if !ok {
			entry = &commandEntry{Primary: cmdPrimary}
			p.commandCache[cmdPrimary] = entry
		}
		entry.PluginName = req.From
		entry.MinRank = minRank
		entry.AdminOnly = adminOnly
		if !isAlias {
			desc := strings.TrimSpace(req.Data.KeyValue["description"])
			if desc != "" {
				entry.Description = desc
			}
		}
		if isAlias && cmd != cmdPrimary {
			p.aliasIndex[cmd] = cmdPrimary
			if !contains(entry.Aliases, cmd) {
				entry.Aliases = append(entry.Aliases, cmd)
			}
		}
	}
	for _, entry := range p.commandCache {
		sort.Strings(entry.Aliases)
	}
	p.cacheMu.Unlock()
	if p.config.GenerateHTML {
		p.markHTMLDirty()
	}

	return nil
}

func (p *Plugin) markHTMLDirty() {
	p.htmlDirty.Store(true)
	select {
	case p.htmlDirtyCh <- struct{}{}:
	default:
	}
}

func (p *Plugin) runHTMLGenerator() {
	defer p.wg.Done()
	debounce := 2 * time.Second
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.htmlDirtyCh:
			timer := time.NewTimer(debounce)
			for {
				select {
				case <-p.ctx.Done():
					timer.Stop()
					return
				case <-p.htmlDirtyCh:
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(debounce)
				case <-timer.C:
					p.generateHelpHTML()
					goto generated
				}
			}
		}
	generated:
		continue
	}
}

func (p *Plugin) generateHelpHTML() {
	if p.generator == nil {
		return
	}
	p.htmlDirty.Store(false)
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	if err := p.generator.GenerateAll(ctx); err != nil {
		logger.Error(p.name, "Failed to generate help HTML: %v", err)
	}
}

func (p *Plugin) loadCacheFromDB() error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT command, plugin_name, is_alias, COALESCE(primary_command, ''), min_rank, enabled, admin_only, description
		FROM daz_eventfilter_commands
		WHERE enabled = true
	`

	rows, err := p.sqlClient.QueryContext(ctx, query)
	if err != nil {
		if isMissingDescriptionError(err) {
			return p.loadCacheFromDBLegacy(ctx)
		}
		return err
	}
	defer rows.Close()

	cache, aliasIndex, err := p.scanCommandRows(rows, true)
	if err != nil {
		return err
	}

	p.cacheMu.Lock()
	p.commandCache = cache
	p.aliasIndex = aliasIndex
	p.cacheMu.Unlock()

	return rows.Err()
}

func (p *Plugin) loadCacheFromDBLegacy(ctx context.Context) error {
	query := `
		SELECT command, plugin_name, is_alias, COALESCE(primary_command, ''), min_rank, enabled, admin_only
		FROM daz_eventfilter_commands
		WHERE enabled = true
	`

	rows, err := p.sqlClient.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	cache, aliasIndex, err := p.scanCommandRows(rows, false)
	if err != nil {
		return err
	}

	p.cacheMu.Lock()
	p.commandCache = cache
	p.aliasIndex = aliasIndex
	p.cacheMu.Unlock()

	return rows.Err()
}

func (p *Plugin) scanCommandRows(rows *framework.QueryRows, hasDescription bool) (map[string]*commandEntry, map[string]string, error) {
	cache := make(map[string]*commandEntry)
	aliasIndex := make(map[string]string)

	for rows.Next() {
		var (
			command     string
			pluginName  string
			isAlias     bool
			primary     string
			minRank     int
			enabled     bool
			adminOnly   bool
			description string
		)

		if hasDescription {
			if err := rows.Scan(&command, &pluginName, &isAlias, &primary, &minRank, &enabled, &adminOnly, &description); err != nil {
				return nil, nil, err
			}
		} else {
			if err := rows.Scan(&command, &pluginName, &isAlias, &primary, &minRank, &enabled, &adminOnly); err != nil {
				return nil, nil, err
			}
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

		entry, ok := cache[primary]
		if !ok {
			entry = &commandEntry{Primary: primary, PluginName: pluginName, MinRank: minRank, AdminOnly: adminOnly, Description: description}
			cache[primary] = entry
		}
		if isAlias && command != primary {
			aliasIndex[command] = primary
			if !contains(entry.Aliases, command) {
				entry.Aliases = append(entry.Aliases, command)
			}
		}
	}

	for _, entry := range cache {
		sort.Strings(entry.Aliases)
	}

	return cache, aliasIndex, nil
}

func isMissingDescriptionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "description") {
		return false
	}
	return strings.Contains(msg, "column") || strings.Contains(msg, "does not exist")
}

func parseAdminOnly(raw string) (map[string]bool, bool) {
	result := make(map[string]bool)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result, false
	}
	if strings.EqualFold(raw, "true") {
		return result, true
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry != "" {
			result[entry] = true
		}
	}
	return result, false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
	if header != "" {
		header += "\n"
	}

	full := header + strings.Join(lines, "\n")
	if len([]rune(full)) <= maxLen {
		return []string{strings.TrimSpace(full)}
	}

	current := header

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

func (p *Plugin) checkHelpCooldown(channel, username string) (time.Duration, bool) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	username = strings.ToLower(strings.TrimSpace(username))
	if channel == "" || username == "" {
		return 0, true
	}

	now := time.Now()
	key := channel + ":" + username

	p.cooldownMu.Lock()
	defer p.cooldownMu.Unlock()

	if last, ok := p.lastHelpByKey[key]; ok {
		until := last.Add(helpCommandCooldown)
		if now.Before(until) {
			return until.Sub(now), false
		}
	}

	p.lastHelpByKey[key] = now
	if len(p.lastHelpByKey) > 10000 {
		cutoff := now.Add(-2 * helpCommandCooldown)
		for k, last := range p.lastHelpByKey {
			if last.Before(cutoff) {
				delete(p.lastHelpByKey, k)
			}
		}
	}

	return 0, true
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	username := req.Data.Command.Params["username"]
	channel := req.Data.Command.Params["channel"]

	if username == "" {
		return
	}

	pmData := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  username,
			Message: message,
			Channel: channel,
		},
	}

	if err := p.eventBus.Broadcast("cytube.send.pm", pmData); err != nil {
		logger.Error(p.name, "Failed to send help PM: %v", err)
		p.emitFailureEvent("command.help.failed", req.ID, "pm_delivery", err)
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
