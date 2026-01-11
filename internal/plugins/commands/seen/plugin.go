package seen

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
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	running   bool
}

func New() framework.Plugin {
	return &Plugin{
		name: "seen",
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

	// Subscribe to seen command execution events
	if err := p.eventBus.Subscribe("command.seen.execute", p.handleCommand); err != nil {
		p.emitFailureEvent("command.seen.failed", "subscription", "event_subscription", err)
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
	return "seen"
}

func (p *Plugin) registerCommand() {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "seen",
					"min_rank": "0",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register seen command: %v", err)
		p.emitFailureEvent("command.seen.failed", "registration", "command_registration", err)
	}
}

func (p *Plugin) handleCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}
	if dataEvent.Data.PluginRequest.Data == nil || dataEvent.Data.PluginRequest.Data.Command == nil {
		return nil
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.handleSeenCommand(dataEvent.Data.PluginRequest)
	}()

	return nil
}

func (p *Plugin) handleSeenCommand(req *framework.PluginRequest) {
	cmd := req.Data.Command
	channel := cmd.Params["channel"]

	// Parse target username from args
	if len(cmd.Args) == 0 {
		p.sendResponse(req, "Usage: !seen <username>")
		return
	}

	targetUser := strings.TrimSpace(cmd.Args[0])
	if targetUser == "" {
		p.sendResponse(req, "Usage: !seen <username>")
		return
	}

	// Query database for user info
	query := `
		SELECT username, joined_at, left_at, last_activity, is_active
		FROM daz_user_tracker_sessions
		WHERE channel = $1 AND LOWER(username) = LOWER($2)
		ORDER BY last_activity DESC
		LIMIT 1
	`

	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	// Use SQL request helper with retry logic
	sqlHelper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := sqlHelper.SlowQuery(ctx, query, channel, targetUser)
	if err != nil {
		logger.Error(p.name, "Failed to query user info: %v", err)
		p.sendResponse(req, "Failed to look up user information")
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	// Parse result
	var username string
	var joinedAt, lastActivity time.Time
	var leftAt *time.Time // Use pointer for nullable field
	var isActive bool

	if rows.Next() {
		err := rows.Scan(&username, &joinedAt, &leftAt, &lastActivity, &isActive)
		if err != nil {
			logger.Error(p.name, "Failed to scan user info: %v", err)
			p.sendResponse(req, "Failed to look up user information")
			return
		}

		// Format response
		var response string
		if isActive {
			response = fmt.Sprintf("%s is online (login: %s, last active: %s)",
				username,
				formatTime(joinedAt),
				formatTime(lastActivity))
		} else {
			leaveTime := lastActivity
			if leftAt != nil {
				leaveTime = *leftAt
			}
			response = fmt.Sprintf("%s (login: %s, active: %s, left: %s)",
				username,
				formatTime(joinedAt),
				formatTime(lastActivity),
				formatTime(leaveTime))
		}

		p.sendResponse(req, response)
	} else {
		p.sendResponse(req, fmt.Sprintf("I haven't seen %s", targetUser))
	}
}

func (p *Plugin) sendResponse(req *framework.PluginRequest, message string) {
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

	if err := p.eventBus.Broadcast("plugin.response", response); err != nil {
		logger.Error(p.name, "Failed to send seen plugin response: %v", err)
		p.emitFailureEvent("command.seen.failed", req.ID, "response_delivery", err)
	}
}

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

	go func() {
		if err := p.eventBus.Broadcast(eventType, failureData); err != nil {
			logger.Debug(p.name, "Failed to emit failure event: %v", err)
		}
	}()
}

func formatTime(t time.Time) string {
	// Now that we store all timestamps as UTC, we can calculate duration directly
	duration := time.Now().UTC().Sub(t)
	if duration < time.Minute {
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	} else if duration < time.Hour {
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	} else if duration < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	} else {
		days := int(duration.Hours()) / 24
		return fmt.Sprintf("%dd ago", days)
	}
}
