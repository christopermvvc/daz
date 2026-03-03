package gitcommit

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	resolveCommit func() string
}

func New() framework.Plugin {
	return &Plugin{name: "gitcommit"}
}

func (p *Plugin) Init(_ json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())
	if p.resolveCommit == nil {
		p.resolveCommit = defaultCommitMessage
	}
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

	if err := p.eventBus.Subscribe("command.gitcommit.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.gitcommit.execute: %w", err)
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
					"commands":    "gitcommit,gitrev,gitver",
					"min_rank":    "0",
					"admin_only":  "true",
					"description": "show current git commit revision",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register commands: %v", err)
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

	params := req.Data.Command.Params
	username := strings.TrimSpace(params["username"])
	channel := strings.TrimSpace(params["channel"])
	isPM := params["is_pm"] == "true"
	isAdmin := params["is_admin"] == "true"

	message := "This command is admin-only."
	if isAdmin {
		message = p.resolveCommit()
	}

	_ = p.sendResponse(username, channel, isPM, message)
	return nil
}

func (p *Plugin) sendResponse(username, channel string, isPM bool, message string) error {
	if isPM {
		response := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
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
		return p.eventBus.Broadcast("plugin.response", response)
	}

	chat := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	return p.eventBus.Broadcast("cytube.send", chat)
}

func defaultCommitMessage() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "Current git commit: unknown"
	}

	var revision string
	var modified string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value)
		}
	}

	if revision == "" {
		return "Current git commit: unknown"
	}

	if len(revision) > 12 {
		revision = revision[:12]
	}

	if modified == "true" {
		return fmt.Sprintf("Current git commit: %s (dirty)", revision)
	}

	return fmt.Sprintf("Current git commit: %s", revision)
}
