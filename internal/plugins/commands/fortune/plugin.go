package fortune

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
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
	mu             sync.RWMutex
	config         Config
	cleanupCounter int
	fortuneExists  bool
}

type Config struct {
	CooldownSeconds int `json:"cooldown_seconds"`
}

func New() framework.Plugin {
	return &Plugin{
		name:           "fortune",
		running:        false,
		lastRequestMap: make(map[string]time.Time),
		cooldown:       30 * time.Second, // Default 30 seconds
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		if p.config.CooldownSeconds > 0 {
			p.cooldown = time.Duration(p.config.CooldownSeconds) * time.Second
		}
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	// Check if fortune binary exists
	p.checkFortuneBinary()

	// Register command with eventfilter
	registerEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "fortune",
					"min_rank": "0",
				},
			},
		},
	}
	_ = p.eventBus.Broadcast("command.register", registerEvent)

	// Subscribe to command execution events
	_ = p.eventBus.Subscribe("command.fortune.execute", p.handleFortuneCommand)

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

func (p *Plugin) checkFortuneBinary() {
	// Check if fortune command exists
	cmd := exec.Command("which", "fortune")
	err := cmd.Run()
	p.fortuneExists = err == nil
	
	if !p.fortuneExists {
		logger.Warn(p.name, "Fortune binary not found. Install 'fortune-mod' package for this command to work.")
	} else {
		logger.Info(p.name, "Fortune binary found and ready")
	}
}

func (p *Plugin) handleFortuneCommand(event framework.Event) error {
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

	// Check rate limit
	if !p.checkRateLimit(username) {
		// Don't apply formatting to rate limit messages
		message := fmt.Sprintf("Please wait %d seconds between fortune requests.", int(p.cooldown.Seconds()))
		if isPM {
			p.sendPMResponse(username, channel, message)
		} else {
			p.sendRawMessage(channel, message)
		}
		return nil
	}

	// Get fortune
	fortune := p.getFortune()
	
	// Send response based on context (PM or public)
	if isPM {
		p.sendPMResponse(username, channel, fortune)
	} else {
		p.sendPublicResponse(channel, fortune)
	}

	return nil
}

func (p *Plugin) checkRateLimit(username string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	lastRequest, exists := p.lastRequestMap[username]
	if !exists || time.Since(lastRequest) >= p.cooldown {
		p.lastRequestMap[username] = time.Now()
		// Clean up old entries every 100 requests
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
	// Clean up entries older than 2x the cooldown period
	cutoff := time.Now().Add(-2 * p.cooldown)
	for username, timestamp := range p.lastRequestMap {
		if timestamp.Before(cutoff) {
			delete(p.lastRequestMap, username)
		}
	}
}

func (p *Plugin) getFortune() string {
	// Check if fortune binary exists (cached from startup)
	if !p.fortuneExists {
		return "Sorry, fortune is not installed on this system. Ask an admin to install the 'fortune-mod' package."
	}

	// Try with specific categories first
	cmd := exec.Command("fortune", "-so", "platitudes", "tao", "wisdom", "startrek")
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		// If categories don't exist, fall back to default fortune
		if strings.Contains(errBuf.String(), "No fortunes found") || strings.Contains(errBuf.String(), "not found") {
			logger.Debug(p.name, "Specific categories not found, trying default fortune")
			cmd = exec.Command("fortune", "-s")
			out.Reset()
			errBuf.Reset()
			cmd.Stdout = &out
			cmd.Stderr = &errBuf
			err = cmd.Run()
		}
		
		if err != nil {
			logger.Error(p.name, "Failed to execute fortune command: %v, stderr: %s", err, errBuf.String())
			return "Sorry, I couldn't fetch a fortune right now."
		}
	}

	// Clean up the output
	fortune := strings.TrimSpace(out.String())
	if fortune == "" {
		return "The fortune cookie was empty!"
	}

	// Limit length to prevent spam
	maxLength := 500
	if len(fortune) > maxLength {
		fortune = fortune[:maxLength] + "..."
	}

	return fortune
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

func (p *Plugin) sendPublicResponse(channel, message string) {
	// Send as public chat message with fortune formatting
	chatData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: fmt.Sprintf("🔮[FORTUNE]🔮: \"%s\"", message),
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send", chatData)
}

func (p *Plugin) sendRawMessage(channel, message string) {
	// Send as public chat message without formatting
	chatData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send", chatData)
}