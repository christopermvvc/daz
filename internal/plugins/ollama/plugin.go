package ollama

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName             = "ollama"
	defaultOllamaURL       = "http://localhost:11434"
	defaultModel           = "gemma3:1b"
	defaultRateLimitSecs   = 60 // 1 minute rate limit per user
	messageFreshnessWindow = 30 * time.Second
	maxResponseLength      = 200 // Maximum characters in response
)

// Config holds ollama plugin configuration
type Config struct {
	// Ollama connection settings
	OllamaURL string `json:"ollama_url"`
	Model     string `json:"model"`

	// Rate limiting
	RateLimitSeconds int `json:"rate_limit_seconds"`

	// Behavior
	Enabled         bool     `json:"enabled"`
	BotName         string   `json:"bot_name"`
	AllowedChannels []string `json:"allowed_channels"`
	IgnoredUsers    []string `json:"ignored_users"`

	// Response settings
	SystemPrompt string  `json:"system_prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// Plugin implements the ollama chat functionality
type Plugin struct {
	ctx       context.Context
	cancel    context.CancelFunc
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	name      string
	running   bool
	mu        sync.RWMutex
	config    *Config
	wg        sync.WaitGroup

	// HTTP client for Ollama API
	httpClient *http.Client

	// Bot name for mention detection
	botName string

	// Ready channel
	readyChan chan struct{}

	// Current users in channels (channel -> username -> true)
	userLists     map[string]map[string]bool
	userListMutex sync.RWMutex

	// Metrics
	totalRequests      int64
	successfulRequests int64
	failedRequests     int64
	metricsLock        sync.RWMutex
	
	// Plugin start time to ignore old messages
	startTime time.Time
}

// OllamaRequest represents a request to the Ollama API
type OllamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Options  Options   `json:"options,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options for the Ollama request
type Options struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

// OllamaResponse represents the response from Ollama API
type OllamaResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// New creates a new ollama plugin instance
func New() framework.Plugin {
	return &Plugin{
		name: pluginName,
		config: &Config{
			OllamaURL:        defaultOllamaURL,
			Model:            defaultModel,
			RateLimitSeconds: defaultRateLimitSecs,
			Enabled:          true,
			Temperature:      0.7,
			MaxTokens:        100,
			SystemPrompt:     "You are Dazza, a friendly and concise chat bot. Keep responses short (1-2 sentences max), casual, and conversational. Never use asterisks for actions or emotes.",
		},
		userLists: make(map[string]map[string]bool),
		readyChan: make(chan struct{}),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

// Ready returns true when the plugin is ready to accept requests
func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

// Init initializes the plugin with configuration and event bus
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	// Parse configuration if provided, merging with defaults
	if len(config) > 0 {
		// Start with current config (which has defaults)
		if err := json.Unmarshal(config, p.config); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
	}
	
	// Ensure defaults are set if not provided in config
	if p.config.OllamaURL == "" {
		p.config.OllamaURL = defaultOllamaURL
	}
	if p.config.Model == "" {
		p.config.Model = defaultModel
	}
	if p.config.RateLimitSeconds == 0 {
		p.config.RateLimitSeconds = defaultRateLimitSecs
	}
	if p.config.Temperature == 0 {
		p.config.Temperature = 0.7
	}
	if p.config.MaxTokens == 0 {
		p.config.MaxTokens = 100
	}
	if p.config.SystemPrompt == "" {
		p.config.SystemPrompt = "You are Dazza, a friendly and concise chat bot. Keep responses short (1-2 sentences max), casual, and conversational. Never use asterisks for actions or emotes."
	}
	
	// IMPORTANT: Default to enabled if not explicitly set
	// Since this is a new plugin, it should be enabled by default
	p.config.Enabled = true

	// Set bot name from environment or config
	p.botName = os.Getenv("DAZ_BOT_NAME")
	if p.botName == "" {
		if p.config.BotName != "" {
			p.botName = p.config.BotName
		} else {
			p.botName = "Dazza"
		}
	}

	return p.Initialize(bus)
}

// Initialize initializes the plugin with event bus (legacy pattern support)
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	p.eventBus = eventBus

	// Initialize SQL client
	p.sqlClient = framework.NewSQLClient(eventBus, p.name)

	// Create context for cancellation
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Subscribe to events
	if err := p.subscribeToEvents(); err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	// Do NOT call Start() here - the plugin manager will call it
	return nil
}

// subscribeToEvents subscribes to necessary events
func (p *Plugin) subscribeToEvents() error {
	// Subscribe to chat messages for mention detection
	if err := p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage); err != nil {
		return fmt.Errorf("failed to subscribe to chat messages: %w", err)
	}

	// Subscribe to user events to maintain userlist
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		return fmt.Errorf("failed to subscribe to addUser events: %w", err)
	}

	if err := p.eventBus.Subscribe("cytube.event.userJoin", p.handleUserJoin); err != nil {
		return fmt.Errorf("failed to subscribe to userJoin events: %w", err)
	}

	if err := p.eventBus.Subscribe(eventbus.EventCytubeUserLeave, p.handleUserLeave); err != nil {
		return fmt.Errorf("failed to subscribe to userLeave events: %w", err)
	}

	return nil
}

// handleUserJoin tracks users joining the channel
func (p *Plugin) handleUserJoin(event framework.Event) error {
	var username, channel string
	var userRank int

	// Handle both DataEvent and typed events
	switch e := event.(type) {
	case *framework.DataEvent:
		if e.Data == nil {
			return nil
		}
		// Check for UserJoin in EventData
		if e.Data.UserJoin != nil {
			username = e.Data.UserJoin.Username
			userRank = e.Data.UserJoin.UserRank
			channel = e.Data.UserJoin.Channel
		} else if e.Data.RawEvent != nil {
			// Check for AddUserEvent in RawEvent
			if addUserEvent, ok := e.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				userRank = addUserEvent.UserRank
				channel = addUserEvent.ChannelName
			} else {
				return nil
			}
		} else {
			return nil
		}
	case *framework.AddUserEvent:
		username = e.Username
		userRank = e.UserRank
		channel = e.ChannelName
	case *framework.UserJoinEvent:
		username = e.Username
		userRank = e.UserRank
		channel = e.ChannelName
	default:
		return nil
	}

	if username == "" || channel == "" {
		return nil
	}

	// Update userlist
	p.userListMutex.Lock()
	if p.userLists[channel] == nil {
		p.userLists[channel] = make(map[string]bool)
	}
	p.userLists[channel][username] = true
	p.userListMutex.Unlock()

	logger.Debug(p.name, "User %s joined channel %s (rank: %d)", username, channel, userRank)
	return nil
}

// handleUserLeave tracks users leaving the channel
func (p *Plugin) handleUserLeave(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.UserLeave == nil {
		return nil
	}

	userLeave := dataEvent.Data.UserLeave
	channel := userLeave.Channel
	username := userLeave.Username

	if username == "" || channel == "" {
		return nil
	}

	// Update userlist
	p.userListMutex.Lock()
	if p.userLists[channel] != nil {
		delete(p.userLists[channel], username)
		// Clean up empty channels to prevent memory leak
		if len(p.userLists[channel]) == 0 {
			delete(p.userLists, channel)
		}
	}
	p.userListMutex.Unlock()

	logger.Debug(p.name, "User %s left channel %s", username, channel)
	return nil
}

// isUserInChannel checks if a user is currently in the channel
func (p *Plugin) isUserInChannel(channel, username string) bool {
	p.userListMutex.RLock()
	defer p.userListMutex.RUnlock()

	if channelUsers, exists := p.userLists[channel]; exists {
		return channelUsers[username]
	}
	return false
}

// handleChatMessage processes chat messages for mentions
func (p *Plugin) handleChatMessage(event framework.Event) error {
	if !p.config.Enabled {
		return nil
	}

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}
	
	if dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	chat := dataEvent.Data.ChatMessage
	channel := chat.Channel
	username := chat.Username
	message := chat.Message
	messageTime := chat.MessageTime

	// Skip messages from before the plugin started (historical messages)
	// messageTime is in milliseconds
	messageTimestamp := time.UnixMilli(messageTime)
	if messageTimestamp.Before(p.startTime) {
		return nil
	}

	// Skip if user is ignored
	for _, ignoredUser := range p.config.IgnoredUsers {
		if strings.EqualFold(username, ignoredUser) {
			logger.Debug(p.name, "User %s is ignored", username)
			return nil
		}
	}

	// Skip system messages or non-entity messages
	if username == "" || strings.EqualFold(username, "System") {
		logger.Debug(p.name, "Skipping system/empty message")
		return nil
	}

	// Check if user is in the channel's userlist
	if !p.isUserInChannel(channel, username) {
		logger.Debug(p.name, "User %s not in channel %s userlist, skipping", username, channel)
		return nil
	}

	// Check if channel is allowed
	if len(p.config.AllowedChannels) > 0 {
		allowed := false
		for _, allowedChannel := range p.config.AllowedChannels {
			if channel == allowedChannel {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil
		}
	}

	// Check if message mentions the bot
	if !p.isBotMentioned(message) {
		logger.Debug(p.name, "Message does not mention bot name '%s'", p.botName)
		return nil
	}

	logger.Info(p.name, "Bot mentioned by %s in %s: %s", username, channel, message)

	// Check message freshness (within 30 seconds)
	messageAge := time.Since(time.UnixMilli(messageTime))
	if messageAge > messageFreshnessWindow {
		logger.Debug(p.name, "Message from %s is too old (%.0f seconds), skipping", username, messageAge.Seconds())
		return nil
	}

	// Calculate message hash for deduplication
	messageHash := p.calculateMessageHash(channel, username, message, messageTime)

	// Check if we've already responded to this exact message
	if p.hasAlreadyResponded(channel, username, messageHash, messageTime) {
		logger.Debug(p.name, "Already responded to message from %s", username)
		return nil
	}

	// Check rate limit
	if p.isRateLimited(channel, username) {
		logger.Debug(p.name, "User %s is rate limited", username)
		return nil
	}

	// Generate and send response
	p.wg.Add(1)
	go p.generateAndSendResponse(channel, username, message, messageHash, messageTime)

	return nil
}

// isBotMentioned checks if the message mentions the bot
func (p *Plugin) isBotMentioned(message string) bool {
	lowerMessage := strings.ToLower(message)
	lowerBotName := strings.ToLower(p.botName)

	// Check for direct mention
	if strings.Contains(lowerMessage, lowerBotName) {
		return true
	}

	// Check for @mention
	if strings.Contains(lowerMessage, "@"+lowerBotName) {
		return true
	}

	return false
}

// calculateMessageHash generates a SHA256 hash of the message for deduplication
func (p *Plugin) calculateMessageHash(channel, username, message string, timestamp int64) string {
	data := fmt.Sprintf("%s:%s:%s:%d", channel, username, message, timestamp)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// hasAlreadyResponded checks if we've already responded to this message
func (p *Plugin) hasAlreadyResponded(channel, username, messageHash string, messageTime int64) bool {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT COUNT(*) FROM daz_ollama_responses 
		WHERE channel = $1 AND username = $2 AND message_hash = $3 AND message_time = $4
	`

	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := helper.FastQuery(ctx, query, channel, username, messageHash, messageTime)
	if err != nil {
		logger.Error(p.name, "Failed to check for existing response: %v", err)
		return false
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	if rows.Next() {
		var count int
		if err := rows.Scan(&count); err == nil && count > 0 {
			return true
		}
	}

	return false
}

// isRateLimited checks if the user is rate limited
func (p *Plugin) isRateLimited(channel, username string) bool {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT last_response_at FROM daz_ollama_rate_limits 
		WHERE channel = $1 AND username = $2
	`

	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	rows, err := helper.FastQuery(ctx, query, channel, username)
	if err != nil {
		logger.Error(p.name, "Failed to check rate limit: %v", err)
		return false
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	if rows.Next() {
		var lastResponseAt time.Time
		if err := rows.Scan(&lastResponseAt); err == nil {
			timeSinceLastResponse := time.Since(lastResponseAt)
			rateLimitDuration := time.Duration(p.config.RateLimitSeconds) * time.Second
			if timeSinceLastResponse < rateLimitDuration {
				return true
			}
		}
	}

	return false
}

// generateAndSendResponse generates an Ollama response and sends it to the channel
func (p *Plugin) generateAndSendResponse(channel, username, message, messageHash string, messageTime int64) {
	defer p.wg.Done()

	// Increment total requests
	p.metricsLock.Lock()
	p.totalRequests++
	p.metricsLock.Unlock()

	// Generate response from Ollama
	response, err := p.callOllama(message)
	if err != nil {
		logger.Error(p.name, "Failed to generate Ollama response for %s: %v", username, err)
		// Increment failed requests
		p.metricsLock.Lock()
		p.failedRequests++
		p.metricsLock.Unlock()
		// Don't send error messages to users - just fail silently
		// Users will understand the bot is not responding
		return
	}

	// Trim response to max length
	if len(response) > maxResponseLength {
		response = response[:maxResponseLength] + "..."
	}

	// Record the response in the database
	p.recordResponse(channel, username, messageHash, messageTime, response)

	// Update rate limit
	p.updateRateLimit(channel, username)

	// Send the response to the channel
	p.sendChannelMessage(channel, response)

	// Increment successful requests
	p.metricsLock.Lock()
	p.successfulRequests++
	p.metricsLock.Unlock()
}

// callOllama makes a request to the Ollama API with retry logic
func (p *Plugin) callOllama(userMessage string) (string, error) {
	request := OllamaRequest{
		Model: p.config.Model,
		Messages: []Message{
			{
				Role:    "system",
				Content: p.config.SystemPrompt,
			},
			{
				Role:    "user",
				Content: userMessage,
			},
		},
		Stream: false,
		Options: Options{
			Temperature: p.config.Temperature,
			NumPredict:  p.config.MaxTokens,
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Ensure we have a valid URL once before retry loop
	ollamaURL := p.config.OllamaURL
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}

	// Retry up to 2 times for transient failures
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// Brief delay before retry
			time.Sleep(500 * time.Millisecond)
		}

		req, err := http.NewRequestWithContext(p.ctx, "POST", ollamaURL+"/api/chat", bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: failed to make request: %w", attempt+1, err)
			continue
		}

		// Check status code before reading body
		statusCode := resp.StatusCode

		// Always read and close body to prevent leaks
		var ollamaResp OllamaResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&ollamaResp)
		if err := resp.Body.Close(); err != nil {
			logger.Error(p.name, "Failed to close response body: %v", err)
		}

		if statusCode != http.StatusOK {
			lastErr = fmt.Errorf("attempt %d: ollama returned status %d", attempt+1, statusCode)
			continue
		}

		if decodeErr != nil {
			return "", fmt.Errorf("failed to decode response: %w", decodeErr)
		}

		return ollamaResp.Message.Content, nil
	}

	return "", lastErr
}

// recordResponse records the response in the database
func (p *Plugin) recordResponse(channel, username, messageHash string, messageTime int64, response string) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_ollama_responses (channel, username, message_hash, message_time, response_text)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (channel, username, message_hash, message_time) DO NOTHING
	`

	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	if _, err := helper.FastExec(ctx, query, channel, username, messageHash, messageTime, response); err != nil {
		logger.Error(p.name, "Failed to record response: %v", err)
	}
}

// updateRateLimit updates the rate limit for a user
func (p *Plugin) updateRateLimit(channel, username string) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_ollama_rate_limits (channel, username, last_response_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (channel, username) 
		DO UPDATE SET last_response_at = NOW()
	`

	helper := framework.NewSQLRequestHelper(p.eventBus, p.name)
	if _, err := helper.FastExec(ctx, query, channel, username); err != nil {
		logger.Error(p.name, "Failed to update rate limit: %v", err)
	}
}

// sendChannelMessage sends a message to the channel
func (p *Plugin) sendChannelMessage(channel, message string) {
	msgData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: channel,
			Message: message,
		},
	}
	if err := p.eventBus.Broadcast("cytube.send", msgData); err != nil {
		logger.Error(p.name, "Failed to broadcast message: %v", err)
	}
}

// testOllamaConnection verifies Ollama is available
func (p *Plugin) testOllamaConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ensure we have a valid URL
	ollamaURL := p.config.OllamaURL
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}

	req, err := http.NewRequestWithContext(ctx, "GET", ollamaURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Error(p.name, "Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	return nil
}

// Start starts the plugin
func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()

	// Test Ollama connectivity
	if err := p.testOllamaConnection(); err != nil {
		logger.Warn(p.name, "Ollama not available at startup (will retry on each request): %v", err)
		// Continue anyway - Ollama might become available later
	}

	// Start cleanup goroutine for old SQL records
	p.wg.Add(1)
	go p.cleanupOldRecords()

	// Mark as ready (only if not already closed)
	select {
	case <-p.readyChan:
		// Already closed, do nothing
	default:
		close(p.readyChan)
	}

	logger.Info(p.name, "Ollama plugin started with bot name: %s", p.botName)
	return nil
}

// cleanupOldRecords periodically removes old response and rate limit records from the database
func (p *Plugin) cleanupOldRecords() {
	defer p.wg.Done()

	// Ensure context is available
	if p.ctx == nil {
		logger.Error(p.name, "Cleanup goroutine started without context")
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)

			// Delete old response records
			responseQuery := `
				DELETE FROM daz_ollama_responses 
				WHERE responded_at < NOW() - INTERVAL '24 hours'
			`
			helper := framework.NewSQLRequestHelper(p.eventBus, p.name)
			responseRows, err := helper.FastExec(ctx, responseQuery)
			if err != nil {
				logger.Error(p.name, "Failed to cleanup old response records: %v", err)
			} else if responseRows > 0 {
				logger.Debug(p.name, "Cleaned up %d old response records", responseRows)
			}

			// Delete old rate limit records (older than 1 hour since they only matter for 1 minute)
			rateLimitQuery := `
				DELETE FROM daz_ollama_rate_limits 
				WHERE last_response_at < NOW() - INTERVAL '1 hour'
			`
			rateLimitRows, err := helper.FastExec(ctx, rateLimitQuery)
			if err != nil {
				logger.Error(p.name, "Failed to cleanup old rate limit records: %v", err)
			} else if rateLimitRows > 0 {
				logger.Debug(p.name, "Cleaned up %d old rate limit records", rateLimitRows)
			}

			cancel()
		}
	}
}

// Stop stops the plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	p.mu.Unlock()

	// Cancel context
	if p.cancel != nil {
		p.cancel()
	}

	// Wait for goroutines to finish
	p.wg.Wait()

	logger.Info(p.name, "Ollama plugin stopped")
	return nil
}

// HandleEvent handles incoming events (required by framework.Plugin interface)
func (p *Plugin) HandleEvent(event framework.Event) error {
	// This plugin uses event subscriptions instead of direct event handling
	return nil
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	running := p.running
	p.mu.RUnlock()

	state := "stopped"
	if running {
		state = "running"
	}

	// Get metrics
	p.metricsLock.RLock()
	total := p.totalRequests
	p.metricsLock.RUnlock()

	return framework.PluginStatus{
		Name:          p.name,
		State:         state,
		EventsHandled: total,
	}
}

