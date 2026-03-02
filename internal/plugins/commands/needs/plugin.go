package needs

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
	name        string
	eventBus    framework.EventBus
	stateClient *framework.PlayerStateClient
	running     bool
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

func New() framework.Plugin {
	return &Plugin{
		name: "needs",
	}
}

func (p *Plugin) Init(_ json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.stateClient = framework.NewPlayerStateClient(bus, p.name)
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

	if err := p.eventBus.Subscribe("command.needs.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.needs.execute: %w", err)
	}

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

func (p *Plugin) registerCommand() {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands":    "needs",
					"min_rank":    "0",
					"description": "check your current needs",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register command: %v", err)
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
	requester := strings.TrimSpace(cmd.Params["username"])
	if channel == "" || requester == "" {
		return nil
	}

	target := requester
	if len(cmd.Args) > 0 {
		target = strings.TrimPrefix(strings.TrimSpace(cmd.Args[0]), "@")
		if target == "" {
			target = requester
		}
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	state, err := p.stateClient.Get(ctx, channel, target)
	if err != nil {
		logger.Error(p.name, "Failed to get player state for %q: %v", target, err)
		p.sendResponse(requester, channel, fmt.Sprintf("couldn't load needs for %s", target))
		return nil
	}

	message := formatNeedsMessage(target, state)
	p.sendResponse(requester, channel, message)

	return nil
}

func formatNeedsMessage(player string, state framework.PlayerState) string {
	return fmt.Sprintf(
		"%s: %d Hunger, %d Drunk, %d High, %d Horny, %d Bladder",
		player,
		state.Food,
		state.Alcohol,
		state.Weed,
		state.Lust,
		state.Bladder,
	)
}

func (p *Plugin) sendResponse(username, channel, message string) {
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

	if err := p.eventBus.Broadcast("plugin.response", response); err != nil {
		logger.Error(p.name, "Failed to send response: %v", err)
	}
}
