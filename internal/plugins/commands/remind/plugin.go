package remind

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	MaxDurationSeconds int `json:"max_duration_seconds"`
	CooldownSeconds    int `json:"cooldown_seconds"`
}

type reminderEntry struct {
	timer    *time.Timer
	duration time.Duration
}

type Plugin struct {
	name     string
	eventBus framework.EventBus
	config   Config

	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	running         bool
	lastUseByUser   map[string]time.Time
	activeReminders map[string]*reminderEntry
}

const (
	defaultMaxDuration = 24 * time.Hour
	defaultCooldown    = 3 * time.Second
)

func New() framework.Plugin {
	return &Plugin{
		name:            "remind",
		lastUseByUser:   make(map[string]time.Time),
		activeReminders: make(map[string]*reminderEntry),
		config: Config{
			MaxDurationSeconds: int(defaultMaxDuration.Seconds()),
			CooldownSeconds:    int(defaultCooldown.Seconds()),
		},
	}
}

func (p *Plugin) Dependencies() []string {
	return nil
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	if p.config.MaxDurationSeconds <= 0 {
		p.config.MaxDurationSeconds = int(defaultMaxDuration.Seconds())
	}
	if p.config.CooldownSeconds <= 0 {
		p.config.CooldownSeconds = int(defaultCooldown.Seconds())
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

	if err := p.eventBus.Subscribe("command.remind.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.remind.execute: %w", err)
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
	for key, entry := range p.activeReminders {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		delete(p.activeReminders, key)
	}
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
					"commands": "remind,reminder,remindme",
					"min_rank": "0",
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
	if username == "" || channel == "" {
		return nil
	}

	args := req.Data.Command.Args
	if len(args) < 1 {
		p.sendChannelMessage(channel, "usage: !reminder <duration>")
		return nil
	}

	if remaining, ok := p.checkCooldown(channel, username); !ok {
		msg := fmt.Sprintf("easy on it mate, wait %ds", int(remaining.Seconds())+1)
		p.sendChannelMessage(channel, msg)
		return nil
	}

	if p.hasActiveReminder(channel, username) {
		p.sendChannelMessage(channel, fmt.Sprintf("%s, ya already got a reminder runnin", username))
		return nil
	}

	input := strings.TrimSpace(args[0])
	duration, ok := parseTimeString(input)
	if !ok || duration <= 0 {
		p.sendChannelMessage(channel, "dunno what time that is mate, try like '5m' or '1h30m'")
		return nil
	}

	maxDuration := time.Duration(p.config.MaxDurationSeconds) * time.Second
	if duration > maxDuration {
		p.sendChannelMessage(channel, "fuck off I'm not remembering that for more than a day")
		return nil
	}

	p.scheduleReminder(channel, username, input, duration)
	p.sendChannelMessage(channel, fmt.Sprintf("righto %s, timer set for %s", username, input))
	return nil
}

func (p *Plugin) scheduleReminder(channel, username, input string, duration time.Duration) {
	key := reminderKey(channel, username)
	entry := &reminderEntry{duration: duration}

	entry.timer = time.AfterFunc(duration, func() {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		message := fmt.Sprintf("%s, it has been %s mate!", username, input)
		p.sendChannelMessage(channel, message)

		p.mu.Lock()
		delete(p.activeReminders, key)
		p.mu.Unlock()
	})

	p.mu.Lock()
	p.activeReminders[key] = entry
	p.mu.Unlock()
}

func (p *Plugin) hasActiveReminder(channel, username string) bool {
	key := reminderKey(channel, username)

	p.mu.RLock()
	_, ok := p.activeReminders[key]
	p.mu.RUnlock()
	return ok
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool) {
	if p.config.CooldownSeconds <= 0 {
		return 0, true
	}

	key := strings.ToLower(channel) + ":" + strings.ToLower(username)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if last, ok := p.lastUseByUser[key]; ok {
		until := last.Add(time.Duration(p.config.CooldownSeconds) * time.Second)
		if now.Before(until) {
			return until.Sub(now), false
		}
	}

	p.lastUseByUser[key] = now
	return 0, true
}

func (p *Plugin) sendChannelMessage(channel, message string) {
	if strings.TrimSpace(channel) == "" {
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

func reminderKey(channel, username string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + ":" + strings.ToLower(strings.TrimSpace(username))
}

func parseTimeString(value string) (time.Duration, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, false
	}

	if isDigitsOnly(value) {
		minutes, err := strconv.Atoi(value)
		if err != nil || minutes <= 0 {
			return 0, false
		}
		return time.Duration(minutes) * time.Minute, true
	}

	var total time.Duration
	var number strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			number.WriteRune(r)
			continue
		}

		if number.Len() == 0 {
			return 0, false
		}

		amount, err := strconv.Atoi(number.String())
		if err != nil {
			return 0, false
		}
		number.Reset()

		switch r {
		case 's':
			total += time.Duration(amount) * time.Second
		case 'm':
			total += time.Duration(amount) * time.Minute
		case 'h':
			total += time.Duration(amount) * time.Hour
		case 'd':
			total += time.Duration(amount) * 24 * time.Hour
		default:
			return 0, false
		}
	}

	if number.Len() != 0 {
		return 0, false
	}

	if total <= 0 {
		return 0, false
	}
	return total, true
}

func isDigitsOnly(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
