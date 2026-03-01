package clap

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

type Config struct {
	CooldownMS int `json:"cooldown_ms"`
}

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool

	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	lastUseByUser map[string]time.Time
	cooldown      time.Duration

	config Config
}

func New() framework.Plugin {
	return &Plugin{
		name:          "clap",
		lastUseByUser: make(map[string]time.Time),
		cooldown:      2 * time.Second,
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) == 0 {
		return nil
	}

	if err := json.Unmarshal(config, &p.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if p.config.CooldownMS > 0 {
		p.cooldown = time.Duration(p.config.CooldownMS) * time.Millisecond
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

	if err := p.eventBus.Subscribe("command.clap.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.clap.execute: %w", err)
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
					"commands":    "clap,clapback,👏",
					"min_rank":    "0",
					"description": "add clap emojis between words",
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
	username := params["username"]
	channel := params["channel"]
	isPM := params["is_pm"] == "true"

	if username != "" && !p.checkCooldown(username) {
		return nil
	}

	args := req.Data.Command.Args
	output, ok := clapify(args)
	if !ok {
		// clapify returns a user-visible error message in output.
		p.sendResponse(username, channel, isPM, output)
		return nil
	}

	p.sendResponse(username, channel, isPM, output)
	return nil
}

func (p *Plugin) checkCooldown(username string) bool {
	if p.cooldown <= 0 {
		return true
	}

	username = strings.ToLower(username)
	if username == "" {
		return true
	}

	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if last, ok := p.lastUseByUser[username]; ok {
		if now.Sub(last) < p.cooldown {
			return false
		}
	}

	p.lastUseByUser[username] = now
	return true
}

func clapify(args []string) (string, bool) {
	if len(args) == 0 {
		return "need 👏 some 👏 text 👏 to 👏 clap 👏 mate", false
	}

	text := strings.Join(args, " 👏 ")
	text = strings.TrimSpace(text)
	if text == "" {
		return "need 👏 some 👏 text 👏 to 👏 clap 👏 mate", false
	}

	const maxRunes = 500
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "..."
	}

	return text, true
}

func (p *Plugin) sendResponse(username, channel string, isPM bool, message string) {
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
		_ = p.eventBus.Broadcast("plugin.response", response)
		return
	}

	chat := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send", chat)
}
