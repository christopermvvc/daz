package games

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name           string
	running        bool
	eventBus       framework.EventBus
	lastRequestMap map[string]time.Time
	cooldown       time.Duration
	cleanupCounter int
	config         Config
	mu             sync.RWMutex
}

type Config struct {
	CooldownSeconds int `json:"cooldown_seconds"`
}

var magic8Responses = []string{
	"It is certain.",
	"Without a doubt.",
	"You may rely on it.",
	"Ask again later.",
	"Cannot predict now.",
	"Don't count on it.",
	"Outlook not so good.",
	"Very doubtful.",
}

func New() framework.Plugin {
	return &Plugin{
		name:           "games",
		lastRequestMap: make(map[string]time.Time),
		cooldown:       5 * time.Second,
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) == 0 {
		return nil
	}

	if err := json.Unmarshal(config, &p.config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if p.config.CooldownSeconds > 0 {
		p.cooldown = time.Duration(p.config.CooldownSeconds) * time.Second
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	registerEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "8ball,eightball,coinflip,flip,rps",
					"min_rank": "0",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", registerEvent); err != nil {
		return fmt.Errorf("failed to register command: %w", err)
	}

	subs := map[string]framework.EventHandler{
		"command.8ball.execute":     p.handle8BallCommand,
		"command.eightball.execute": p.handle8BallCommand,
		"command.coinflip.execute":  p.handleCoinFlipCommand,
		"command.flip.execute":      p.handleCoinFlipCommand,
		"command.rps.execute":       p.handleRPSCommand,
	}

	for topic, handler := range subs {
		if err := p.eventBus.Subscribe(topic, handler); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
		}
	}

	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
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

func (p *Plugin) handle8BallCommand(event framework.Event) error {
	return p.handleWithGeneratedResponse(event, p.generate8BallResponse)
}

func (p *Plugin) handleCoinFlipCommand(event framework.Event) error {
	return p.handleWithGeneratedResponse(event, p.generateCoinFlipResponse)
}

func (p *Plugin) handleRPSCommand(event framework.Event) error {
	return p.handleWithGeneratedResponse(event, p.generateRPSResponse)
}

func (p *Plugin) handleWithGeneratedResponse(event framework.Event, generator func([]string) string) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.Command == nil {
		return nil
	}

	username := req.Data.Command.Params["username"]
	channel := req.Data.Command.Params["channel"]
	isPM := req.Data.Command.Params["is_pm"] == "true"
	args := req.Data.Command.Args

	if !p.checkRateLimit(username) {
		p.sendResponse(req.ID, username, channel, isPM, "Slow down! Please wait a few seconds between game commands.")
		return nil
	}

	response := generator(args)
	p.sendResponse(req.ID, username, channel, isPM, response)
	return nil
}

func (p *Plugin) checkRateLimit(username string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	lastRequest, exists := p.lastRequestMap[username]
	if !exists || time.Since(lastRequest) >= p.cooldown {
		p.lastRequestMap[username] = time.Now()
		p.cleanupCounter++
		if p.cleanupCounter >= 100 {
			p.cleanupOldEntries()
			p.cleanupCounter = 0
		}
		return true
	}

	return false
}

func (p *Plugin) cleanupOldEntries() {
	cutoff := time.Now().Add(-2 * p.cooldown)
	for username, timestamp := range p.lastRequestMap {
		if timestamp.Before(cutoff) {
			delete(p.lastRequestMap, username)
		}
	}
}

func (p *Plugin) generate8BallResponse(args []string) string {
	if len(args) == 0 {
		return "Usage: !8ball <question>"
	}

	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(magic8Responses))))
	if err != nil {
		logger.Error(p.name, "failed to generate 8ball response: %v", err)
		return "The magic 8-ball is cloudy right now."
	}

	return fmt.Sprintf("🎱 %s", magic8Responses[idx.Int64()])
}

func (p *Plugin) generateCoinFlipResponse(args []string) string {
	flip, err := rand.Int(rand.Reader, big.NewInt(2))
	if err != nil {
		logger.Error(p.name, "failed to generate coin flip: %v", err)
		return "Couldn't flip a coin right now."
	}

	if flip.Int64() == 0 {
		return "🪙 Heads!"
	}
	return "🪙 Tails!"
}

func (p *Plugin) generateRPSResponse(args []string) string {
	if len(args) == 0 {
		return "Usage: !rps <rock|paper|scissors>"
	}

	player := strings.ToLower(strings.TrimSpace(args[0]))
	if player != "rock" && player != "paper" && player != "scissors" {
		return "Please choose rock, paper, or scissors."
	}

	choices := []string{"rock", "paper", "scissors"}
	idx, err := rand.Int(rand.Reader, big.NewInt(3))
	if err != nil {
		logger.Error(p.name, "failed to generate rps choice: %v", err)
		return "Couldn't play rps right now."
	}

	bot := choices[idx.Int64()]
	result := "It's a tie!"

	if (player == "rock" && bot == "scissors") ||
		(player == "paper" && bot == "rock") ||
		(player == "scissors" && bot == "paper") {
		result = "You win!"
	} else if player != bot {
		result = "I win!"
	}

	return fmt.Sprintf("✂️ You played %s, I played %s — %s", player, bot, result)
}

func (p *Plugin) sendResponse(requestID, username, channel string, isPM bool, message string) {
	responseData := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      requestID,
			From:    p.name,
			Success: true,
			Data: &framework.ResponseData{
				CommandResult: &framework.CommandResultData{Success: true, Output: message},
				KeyValue: map[string]string{
					"username": username,
					"channel":  channel,
					"is_pm":    fmt.Sprintf("%t", isPM),
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("plugin.response", responseData); err != nil {
		logger.Error(p.name, "failed to send response: %v", err)
	}
}
