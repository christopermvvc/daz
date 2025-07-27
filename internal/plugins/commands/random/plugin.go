package random

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
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
	rateLimitSecs  int
	mu             sync.RWMutex
	config         Config
}

type Config struct {
	RateLimitSeconds int `json:"rate_limit_seconds"`
}

func New() framework.Plugin {
	return &Plugin{
		name:           "random",
		running:        false,
		lastRequestMap: make(map[string]time.Time),
		rateLimitSecs:  30, // Default 30 seconds rate limit
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		if p.config.RateLimitSeconds > 0 {
			p.rateLimitSecs = p.config.RateLimitSeconds
		}
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	// Register command with eventfilter
	registerEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "random,rand,r",
					"min_rank": "0",
				},
			},
		},
	}
	_ = p.eventBus.Broadcast("command.register", registerEvent)

	// Subscribe to command execution events
	_ = p.eventBus.Subscribe("command.random.execute", p.handleRandomCommand)
	_ = p.eventBus.Subscribe("command.rand.execute", p.handleRandomCommand)
	_ = p.eventBus.Subscribe("command.r.execute", p.handleRandomCommand)

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
	return p.name
}

func (p *Plugin) handleRandomCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest

	// Extract command data
	username := ""
	channel := ""
	isPM := false

	if req.Data != nil && req.Data.Command != nil && req.Data.Command.Params != nil {
		username = req.Data.Command.Params["username"]
		channel = req.Data.Command.Params["channel"]
		// Check if this is a PM
		if pmFlag := req.Data.Command.Params["is_pm"]; pmFlag == "true" {
			isPM = true
		}
	}

	// Get args
	var args []string
	if req.Data != nil && req.Data.Command != nil {
		args = req.Data.Command.Args
	}

	// Check rate limit
	if !p.checkRateLimit(username) {
		message := fmt.Sprintf("Please wait %d seconds between random requests.", p.rateLimitSecs)
		if isPM {
			p.sendPMResponse(username, channel, message)
		} else {
			p.sendPublicResponse(username, channel, message)
		}
		return nil
	}

	// Generate response
	response := p.generateRandomResponse(args)

	// Send response based on context (PM or public)
	if isPM {
		p.sendPMResponse(username, channel, response)
	} else {
		p.sendPublicResponse(username, channel, response)
	}

	return nil
}

func (p *Plugin) checkRateLimit(username string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	lastRequest, exists := p.lastRequestMap[username]
	if !exists || time.Since(lastRequest) >= time.Duration(p.rateLimitSecs)*time.Second {
		p.lastRequestMap[username] = time.Now()
		return true
	}
	return false
}

func (p *Plugin) generateRandomResponse(args []string) string {
	if len(args) == 0 {
		return "Usage: !random <number> - generates a random number between 0 and <number>"
	}

	// Parse the maximum value
	maxStr := strings.TrimSpace(args[0])
	max, err := strconv.ParseInt(maxStr, 10, 64)
	if err != nil || max <= 0 {
		return "Please provide a valid positive number"
	}

	// Generate secure random number
	randomBig, err := rand.Int(rand.Reader, big.NewInt(max+1))
	if err != nil {
		logger.Error(p.name, "Failed to generate random number: %v", err)
		return "Error generating random number"
	}

	randomNum := randomBig.Int64()

	// Format response
	return fmt.Sprintf("🎲 Random number (0-%d): %d", max, randomNum)
}

func (p *Plugin) sendPMResponse(username, channel, message string) {
	// Send as PM using plugin response system
	responseData := &framework.EventData{
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
	_ = p.eventBus.Broadcast("plugin.response", responseData)
}

func (p *Plugin) sendPublicResponse(username, channel, message string) {
	// Send as public chat message
	chatData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send", chatData)
}
