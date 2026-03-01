package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name             string
	eventBus         framework.EventBus
	running          bool
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	config           Config
	pendingMu        sync.Mutex
	pendingResponses map[string]chan *framework.PluginResponse
}

type Config struct {
	BotUsername string `json:"bot_username"`
}

var htmlTagPattern = regexp.MustCompile(`<[^>]*>`)

type quoteRow struct {
	Username    string
	Message     string
	MessageTime int64
}

func New() framework.Plugin {
	return &Plugin{
		name:             "quote",
		pendingResponses: make(map[string]chan *framework.PluginResponse),
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
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) != 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	p.config.BotUsername = strings.TrimSpace(p.config.BotUsername)
	p.config.BotUsername = resolveBotUsername(p.config.BotUsername)
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

	if err := p.eventBus.Subscribe("command.quote.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.quote.execute: %w", err)
	}

	if err := p.eventBus.Subscribe("plugin.response.quote", p.handlePluginResponse); err != nil {
		return fmt.Errorf("failed to subscribe to plugin.response.quote: %w", err)
	}

	p.registerCommands()
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

	if p.cancel != nil {
		p.cancel()
	}

	logger.Debug(p.name, "Stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	_ = event
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state := "stopped"
	if p.running {
		state = "running"
	}

	return framework.PluginStatus{Name: p.name, State: state}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) registerCommands() {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "quote,q,rq",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register quote commands: %v", err)
	}
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
	channel := strings.TrimSpace(cmd.Params["channel"])
	commandName := strings.ToLower(strings.TrimSpace(cmd.Name))

	if channel == "" {
		p.sendResponse(req, "This command requires a channel context.")
		return nil
	}

	ctx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
	defer cancel()

	var (
		row *quoteRow
		err error
	)

	botUsername, botErr := p.resolveBotUsernameForChannel(ctx, channel)
	if botErr != nil {
		logger.Warn(p.name, "Failed to resolve bot username for channel %s: %v", channel, botErr)
	}

	switch commandName {
	case "rq":
		row, err = p.getRandomQuote(ctx, channel, "", botUsername)
	default:
		if len(cmd.Args) == 0 || strings.TrimSpace(cmd.Args[0]) == "" {
			p.sendResponse(req, "Usage: !quote <username> (or !rq for random quote)")
			return nil
		}
		target := sanitizeTarget(cmd.Args[0])
		if isSystemUsername(target) {
			p.sendResponse(req, fmt.Sprintf("No quote found for %s.", strings.TrimSpace(cmd.Args[0])))
			return nil
		}
		logger.Debug(p.name, "Quote self-target check: target=%q bot=%q channel=%q", target, botUsername, channel)
		if isSelfTarget(target, botUsername) {
			p.sendResponse(req, "Nope. I won't quote myself.")
			return nil
		}
		row, err = p.getRandomQuote(ctx, channel, target, botUsername)
	}

	if err != nil {
		logger.Error(p.name, "Failed to fetch quote: %v", err)
		p.sendResponse(req, "Couldn't fetch a quote right now.")
		return nil
	}

	if row == nil {
		if commandName == "rq" {
			p.sendResponse(req, "No quotable chat messages found yet.")
		} else {
			p.sendResponse(req, fmt.Sprintf("No quote found for %s.", strings.TrimSpace(cmd.Args[0])))
		}
		return nil
	}

	msg := sanitizeQuoteMessage(row.Message)
	if len(msg) > 220 {
		msg = msg[:217] + "..."
	}

	response := fmt.Sprintf("Quote: %s: %q (%s ago)", row.Username, msg, formatSince(row.MessageTime))
	p.sendResponse(req, response)
	return nil
}

func (p *Plugin) getRandomQuote(ctx context.Context, channel, username, botUsername string) (*quoteRow, error) {
	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)

	query := `
		SELECT username, message, message_time
		FROM daz_core_events
		WHERE event_type = 'cytube.event.chatMsg'
		  AND channel_name = $1
		  AND message IS NOT NULL
		  AND LENGTH(TRIM(message)) > 0
		  AND message NOT LIKE '!%'
		  AND LOWER(username) NOT IN ('system', 'server', 'cytube')
		  AND LENGTH(TRIM(username)) > 0
	`

	args := []interface{}{channel}
	if username != "" {
		query += " AND LOWER(username) = LOWER($2)"
		args = append(args, username)
	} else if botUsername != "" {
		query += " AND LOWER(username) <> LOWER($2)"
		args = append(args, botUsername)
	}

	query += " ORDER BY RANDOM() LIMIT 1"

	rows, err := helper.SlowQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Warn(p.name, "Failed to close quote rows: %v", closeErr)
		}
	}()

	if !rows.Next() {
		return nil, nil
	}

	var row quoteRow
	if err := rows.Scan(&row.Username, &row.Message, &row.MessageTime); err != nil {
		return nil, fmt.Errorf("failed to scan quote row: %w", err)
	}

	return &row, nil
}

func (p *Plugin) handlePluginResponse(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginResponse == nil {
		return nil
	}

	resp := dataEvent.Data.PluginResponse
	if strings.TrimSpace(resp.ID) == "" {
		return nil
	}

	p.pendingMu.Lock()
	responseChan, exists := p.pendingResponses[resp.ID]
	if exists {
		delete(p.pendingResponses, resp.ID)
	}
	p.pendingMu.Unlock()

	if exists {
		select {
		case responseChan <- resp:
		default:
		}
	}

	return nil
}

func (p *Plugin) resolveBotUsernameForChannel(ctx context.Context, channel string) (string, error) {
	configured := resolveBotUsername(p.config.BotUsername)
	if configured != "" {
		return configured, nil
	}

	requestID := fmt.Sprintf("quote-botname-%d", time.Now().UnixNano())
	responseChan := make(chan *framework.PluginResponse, 1)

	p.pendingMu.Lock()
	p.pendingResponses[requestID] = responseChan
	p.pendingMu.Unlock()

	defer func() {
		p.pendingMu.Lock()
		delete(p.pendingResponses, requestID)
		p.pendingMu.Unlock()
	}()

	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   requestID,
			To:   "core",
			From: p.name,
			Type: "get_configured_channels",
		},
	}

	if err := p.eventBus.Broadcast("plugin.request", request); err != nil {
		return "", fmt.Errorf("failed to request configured channels: %w", err)
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-responseChan:
		if resp == nil {
			return "", nil
		}
		if !resp.Success {
			if resp.Error == "" {
				return "", nil
			}
			return "", fmt.Errorf("core get_configured_channels failed: %s", resp.Error)
		}
		if resp.Data == nil || len(resp.Data.RawJSON) == 0 {
			return "", nil
		}
		return extractBotUsernameFromChannels(resp.Data.RawJSON, channel)
	case <-time.After(1 * time.Second):
		return "", nil
	}
}

func extractBotUsernameFromChannels(rawJSON json.RawMessage, channel string) (string, error) {
	var responseData struct {
		Channels []struct {
			Channel  string `json:"channel"`
			Username string `json:"username"`
		} `json:"channels"`
	}

	if err := json.Unmarshal(rawJSON, &responseData); err != nil {
		return "", fmt.Errorf("failed to parse channel config response: %w", err)
	}

	for _, ch := range responseData.Channels {
		if strings.EqualFold(strings.TrimSpace(ch.Channel), strings.TrimSpace(channel)) {
			return strings.TrimSpace(ch.Username), nil
		}
	}

	return "", nil
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
	channel := req.Data.Command.Params["channel"]

	chatData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}

	if err := p.eventBus.Broadcast("cytube.send", chatData); err != nil {
		logger.Error(p.name, "Failed to send quote response: %v", err)
	}
}

func sanitizeTarget(raw string) string {
	t := strings.TrimSpace(raw)
	t = strings.TrimLeft(t, "-@")
	t = strings.Trim(t, " \t\n\r,.;:!?()[]{}\"'`")
	return t
}

func resolveBotUsername(configBotUsername string) string {
	configured := strings.TrimSpace(configBotUsername)
	if configured != "" {
		return configured
	}

	fromBotName := strings.TrimSpace(os.Getenv("DAZ_BOT_NAME"))
	if fromBotName != "" {
		return fromBotName
	}

	return strings.TrimSpace(os.Getenv("DAZ_CYTUBE_USERNAME"))
}

func sanitizeQuoteMessage(raw string) string {
	text := strings.TrimSpace(raw)
	text = html.UnescapeString(text)
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(text), " ")
	text = html.EscapeString(text)
	return text
}

func isSelfTarget(target, botUsername string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	if t == "" {
		return false
	}

	b := strings.ToLower(strings.TrimSpace(botUsername))
	if b == "" {
		return false
	}

	return t == b
}

func isSystemUsername(username string) bool {
	name := strings.ToLower(strings.TrimSpace(username))
	if name == "" {
		return true
	}

	switch name {
	case "system", "server", "cytube":
		return true
	default:
		return false
	}
}

func formatSince(messageTime int64) string {
	if messageTime <= 0 {
		return "unknown time"
	}

	if messageTime < 1_000_000_000_000 {
		messageTime *= 1000
	}

	delta := time.Since(time.UnixMilli(messageTime))
	if delta < 0 {
		delta = 0
	}

	switch {
	case delta < time.Minute:
		return fmt.Sprintf("%ds", int(delta.Seconds()))
	case delta < time.Hour:
		return fmt.Sprintf("%dm", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh", int(delta.Hours()))
	default:
		return fmt.Sprintf("%dd", int(delta.Hours()/24))
	}
}
