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

const maxNeedValue int64 = 100

var hungerTiers = []string{
	"Stuffed like a corpse",
	"Satisfied",
	"Little hungry",
	"Getting peckish",
	"Properly hungry",
	"Ham-fisted hunger",
	"I can smell food in the room",
	"Your tongue is making prayers",
	"Ravenous",
	"Trespassingly starving",
}

var drunkTiers = []string{
	"Sane as a nun",
	"Just warm, barely",
	"Tipsy",
	"Lit",
	"Already loud",
	"Half-thorough",
	"Pretty hammered",
	"Blindly dancing",
	"Seasick from your own piss",
	"Absolutely coked out",
}

var highTiers = []string{
	"Clear headed",
	"Breezy",
	"Slightly floaty",
	"Head in the clouds",
	"Cloudy",
	"Fully glazed",
	"High and horny",
	"Lost in your own cinema",
	"Spacey and loud",
	"Too far gone to focus",
}

var lustTiers = []string{
	"Chaste and polite",
	"Curious",
	"Feeling your vibe",
	"Already thinking about it",
	"On the edge",
	"Needing attention",
	"Getting needy",
	"Riding hot currents",
	"Unreasonably turn-on-able",
	"Ready to break the internet",
}

var bladderTiers = []string{
	"Dry as dust",
	"Comfortably in control",
	"Lightly full",
	"Noticeably occupied",
	"You feel it",
	"Needs a run",
	"Emergency mindset",
	"One bad mistake away",
	"Full and frantic",
	"Fuller than a water balloon",
}

func formatNeedsMessage(player string, state framework.PlayerState) string {
	hunger := clampNeed(state.Food)
	drunk := clampNeed(state.Alcohol)
	high := clampNeed(state.Weed)
	horny := clampNeed(state.Lust)
	bladder := clampNeed(state.Bladder)

	return fmt.Sprintf(
		"%s: %d Hunger (%s), %d Drunk (%s), %d High (%s), %d Horny (%s), %d Bladder (%s)",
		player,
		hunger,
		needTier(hunger, hungerTiers),
		drunk,
		needTier(drunk, drunkTiers),
		high,
		needTier(high, highTiers),
		horny,
		needTier(horny, lustTiers),
		bladder,
		needTier(bladder, bladderTiers),
	)
}

func clampNeed(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxNeedValue {
		return maxNeedValue
	}
	return value
}

func needTier(value int64, labels []string) string {
	idx := 0
	switch {
	case value >= maxNeedValue:
		idx = len(labels) - 1
	default:
		idx = int(value / 10)
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(labels) {
		idx = len(labels) - 1
	}
	return labels[idx]
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
