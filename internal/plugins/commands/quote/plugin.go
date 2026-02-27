package quote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

type quoteRow struct {
	Username    string
	Message     string
	MessageTime int64
}

func New() framework.Plugin {
	return &Plugin{name: "quote"}
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(_ json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
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

	if err := p.eventBus.Subscribe("command.quote.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.quote.execute: %w", err)
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

	switch commandName {
	case "rq":
		row, err = p.getRandomQuote(ctx, channel, "")
	default:
		if len(cmd.Args) == 0 || strings.TrimSpace(cmd.Args[0]) == "" {
			p.sendResponse(req, "Usage: !quote <username> (or !rq for random quote)")
			return nil
		}
		target := strings.TrimSpace(cmd.Args[0])
		row, err = p.getRandomQuote(ctx, channel, target)
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

	msg := strings.Join(strings.Fields(strings.TrimSpace(row.Message)), " ")
	if len(msg) > 220 {
		msg = msg[:217] + "..."
	}

	response := fmt.Sprintf("Quote: %s: %q (%s ago)", row.Username, msg, formatSince(row.MessageTime))
	p.sendResponse(req, response)
	return nil
}

func (p *Plugin) getRandomQuote(ctx context.Context, channel, username string) (*quoteRow, error) {
	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)

	query := `
		SELECT username, message, message_time
		FROM daz_core_events
		WHERE event_type = 'cytube.event.chatMsg'
		  AND channel_name = $1
		  AND message IS NOT NULL
		  AND LENGTH(TRIM(message)) > 0
		  AND message NOT LIKE '!%'
	`

	args := []interface{}{channel}
	if username != "" {
		query += " AND LOWER(username) = LOWER($2)"
		args = append(args, username)
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
