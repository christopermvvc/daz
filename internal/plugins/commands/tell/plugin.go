package tell

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

const (
	pluginName            = "tell"
	messageLifetime       = 10 * 365 * 24 * time.Hour // 10 years
	deliveryLimit         = 15
	periodicCheckInterval = 5 * time.Minute // Check for online recipients every 5 minutes
)

type Plugin struct {
	ctx            context.Context
	cancel         context.CancelFunc
	eventBus       framework.EventBus
	sqlClient      *framework.SQLClient
	name           string
	running        bool
	mu             sync.RWMutex
	onlineUsers    map[string]map[string]bool // channel -> username -> online
	deliveryTimers map[string]*time.Timer     // channel:username -> timer
	wg             sync.WaitGroup
}

type tellMessage struct {
	ID          int
	Channel     string
	FromUser    string
	ToUser      string
	Message     string
	IsPM        bool
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Delivered   bool
	DeliveredAt sql.NullTime
}

func New() framework.Plugin {
	return &Plugin{
		name:           pluginName,
		onlineUsers:    make(map[string]map[string]bool),
		deliveryTimers: make(map[string]*time.Timer),
	}
}

// Dependencies returns the list of plugins this plugin depends on
func (p *Plugin) Dependencies() []string {
	return []string{"sql"} // We need the SQL plugin to be ready
}

// Ready returns true when the plugin is ready to accept requests
func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	logger.Info(p.name, "Initializing tell plugin")
	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	p.running = true

	// Create database table
	if err := p.createTable(); err != nil {
		logger.Error(p.name, "Failed to create table: %v", err)
		return err
	}

	// Subscribe to events
	// Note: CyTube sends "addUser" when a user joins, not "userJoin"
	logger.Debug(p.name, "Subscribing to cytube.event.addUser")
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		logger.Error(p.name, "Failed to subscribe to addUser: %v", err)
	}

	logger.Debug(p.name, "Subscribing to cytube.event.userLeave")
	if err := p.eventBus.Subscribe("cytube.event.userLeave", p.handleUserLeave); err != nil {
		logger.Error(p.name, "Failed to subscribe to userLeave: %v", err)
	}

	logger.Debug(p.name, "Subscribing to command.tell.execute")
	if err := p.eventBus.Subscribe("command.tell.execute", p.handleCommandEvent); err != nil {
		logger.Error(p.name, "Failed to subscribe to command.tell.execute: %v", err)
	}

	// Register command with eventfilter
	p.registerCommand()

	// Start cleanup goroutine
	p.wg.Add(1)
	go p.cleanupExpiredMessages()

	// Start periodic delivery check goroutine
	p.startPeriodicDeliveryCheck()

	logger.Info(p.name, "Tell plugin started")
	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.cancel()
	p.running = false

	// Cancel all delivery timers
	for _, timer := range p.deliveryTimers {
		timer.Stop()
	}
	p.deliveryTimers = make(map[string]*time.Timer)

	// Wait for goroutines to finish
	p.wg.Wait()

	logger.Info(p.name, "Tell plugin stopped")
	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	// This is called by the event bus when events arrive
	// The actual handling is done in the handler methods
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := "stopped"
	if p.running {
		status = "running"
	}

	onlineCount := 0
	for _, users := range p.onlineUsers {
		onlineCount += len(users)
	}

	return framework.PluginStatus{
		Name:  p.name,
		State: status,
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) createTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS daz_tell_messages (
			id SERIAL PRIMARY KEY,
			channel VARCHAR(255) NOT NULL,
			from_user VARCHAR(255) NOT NULL,
			to_user VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			is_pm BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			delivered BOOLEAN DEFAULT FALSE,
			delivered_at TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tell_pending ON daz_tell_messages(channel, to_user, delivered);
		CREATE INDEX IF NOT EXISTS idx_tell_expires ON daz_tell_messages(expires_at) WHERE delivered = FALSE;
	`

	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	_, err := p.sqlClient.ExecContext(ctx, query)
	return err
}

func (p *Plugin) registerCommand() {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "tell",
					"min_rank": "0",
				},
			},
		},
	}
	_ = p.eventBus.Broadcast("command.register", regEvent)
}

func (p *Plugin) handleUserJoin(event framework.Event) error {
	logger.Debug(p.name, "handleUserJoin called with event type: %T", event)

	var username, channel string

	// Handle direct AddUserEvent (current format)
	if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		username = addUserEvent.Username
		channel = addUserEvent.ChannelName
		logger.Debug(p.name, "User joined (direct AddUserEvent): %s in channel %s", username, channel)
	} else if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		// Handle wrapped in DataEvent (legacy format)
		if dataEvent.Data.UserJoin != nil {
			channel = dataEvent.Data.UserJoin.Channel
			username = dataEvent.Data.UserJoin.Username
			logger.Debug(p.name, "User joined (from UserJoin): %s in channel %s", username, channel)
		} else if dataEvent.Data.RawEvent != nil {
			// Try to extract from RawEvent
			if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				channel = addUserEvent.ChannelName
				logger.Debug(p.name, "User joined (from RawEvent AddUserEvent): %s in channel %s", username, channel)
			} else {
				logger.Debug(p.name, "handleUserJoin: RawEvent is not AddUserEvent, type is %T", dataEvent.Data.RawEvent)
				return nil
			}
		} else {
			logger.Debug(p.name, "handleUserJoin: no UserJoin or RawEvent data in DataEvent")
			return nil
		}
	} else {
		logger.Debug(p.name, "handleUserJoin: event is neither AddUserEvent nor DataEvent")
		return nil
	}

	if username == "" || channel == "" {
		logger.Warn(p.name, "handleUserJoin: missing username or channel")
		return nil
	}

	p.mu.Lock()
	if p.onlineUsers[channel] == nil {
		p.onlineUsers[channel] = make(map[string]bool)
	}
	p.onlineUsers[channel][strings.ToLower(username)] = true
	p.mu.Unlock()

	// Schedule message delivery
	p.scheduleMessageDelivery(channel, username)
	return nil
}

func (p *Plugin) handleUserLeave(event framework.Event) error {
	// Check if this is a DataEvent
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.UserLeave == nil {
		return nil
	}

	data := dataEvent.Data
	channel := data.UserLeave.Channel
	username := data.UserLeave.Username

	p.mu.Lock()
	if p.onlineUsers[channel] != nil {
		delete(p.onlineUsers[channel], strings.ToLower(username))
	}

	// Cancel pending delivery
	timerKey := fmt.Sprintf("%s:%s", channel, strings.ToLower(username))
	if timer, exists := p.deliveryTimers[timerKey]; exists {
		timer.Stop()
		delete(p.deliveryTimers, timerKey)
	}
	p.mu.Unlock()
	return nil
}

func (p *Plugin) handleCommandEvent(event framework.Event) error {
	// Check if this is a DataEvent
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.Command == nil {
		return nil
	}

	cmd := req.Data.Command
	channel := cmd.Params["channel"]
	fromUser := cmd.Params["username"]
	isPM := cmd.Params["is_pm"] == "true"

	// Join the args slice into a single string
	args := strings.Join(cmd.Args, " ")

	p.processTellCommand(channel, fromUser, args, isPM)
	return nil
}

func (p *Plugin) processTellCommand(channel, fromUser, args string, isPM bool) {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) < 2 {
		p.sendResponse(channel, fromUser, "Usage: !tell <username> <message>", isPM)
		return
	}

	toUser := parts[0]
	message := parts[1]

	// Check if user is online in database first
	isOnline, err := p.isUserOnlineInDB(channel, toUser)
	if err != nil {
		logger.Error(p.name, "Failed to check user online status in DB: %v", err)
		// Fall back to in-memory check
		p.mu.RLock()
		isOnline = p.onlineUsers[channel] != nil && p.onlineUsers[channel][strings.ToLower(toUser)]
		p.mu.RUnlock()
	}

	if isOnline {
		p.sendResponse(channel, fromUser, fmt.Sprintf("%s is currently online. Please message them directly.", toUser), isPM)
		return
	}

	logger.Debug(p.name, "User %s not found online (checked lowercase: %s), proceeding to store message", toUser, strings.ToLower(toUser))

	// Store the message - preserve the case as given by the sender
	if err := p.storeMessage(channel, fromUser, toUser, message, isPM); err != nil {
		logger.Error(p.name, "Failed to store message: %v", err)
		p.sendResponse(channel, fromUser, "Failed to store message. Please try again later.", isPM)
		return
	}

	logger.Debug(p.name, "Stored message from %s to %s (case-preserved) in channel %s", fromUser, toUser, channel)
	p.sendResponse(channel, fromUser, fmt.Sprintf("Message for %s has been saved.", toUser), isPM)
}

func (p *Plugin) storeMessage(channel, fromUser, toUser, message string, isPM bool) error {
	query := `
		INSERT INTO daz_tell_messages (channel, from_user, to_user, message, is_pm, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	expiresAt := time.Now().Add(messageLifetime)
	_, err := p.sqlClient.ExecContext(ctx, query, channel, fromUser, toUser, message, isPM, expiresAt)
	return err
}

func (p *Plugin) scheduleMessageDelivery(channel, username string) {
	timerKey := fmt.Sprintf("%s:%s", channel, strings.ToLower(username))

	logger.Debug(p.name, "Scheduling message delivery for user %s in channel %s", username, channel)

	p.mu.Lock()
	// Cancel existing timer if present
	if timer, exists := p.deliveryTimers[timerKey]; exists {
		timer.Stop()
	}

	// Create new timer with 1 second delay
	p.deliveryTimers[timerKey] = time.AfterFunc(1*time.Second, func() {
		logger.Debug(p.name, "Timer fired - delivering messages for user %s in channel %s", username, channel)
		p.deliverMessages(channel, username)

		p.mu.Lock()
		delete(p.deliveryTimers, timerKey)
		p.mu.Unlock()
	})
	p.mu.Unlock()
}

func (p *Plugin) deliverMessages(channel, username string) {
	logger.Debug(p.name, "deliverMessages called for user %s (exact case) in channel %s", username, channel)

	messages, err := p.getPendingMessages(channel, username)
	if err != nil {
		logger.Error(p.name, "Failed to get pending messages: %v", err)
		return
	}

	logger.Debug(p.name, "Found %d pending messages for user %s (query used LOWER for case-insensitive match)", len(messages), username)

	if len(messages) == 0 {
		return
	}

	// Group messages by sender for better formatting
	messagesBySender := make(map[string][]tellMessage)
	senderOrder := []string{} // Maintain order of first message from each sender

	for _, msg := range messages {
		if _, exists := messagesBySender[msg.FromUser]; !exists {
			senderOrder = append(senderOrder, msg.FromUser)
		}
		messagesBySender[msg.FromUser] = append(messagesBySender[msg.FromUser], msg)
	}

	// Track delivered message IDs to properly check remaining messages
	deliveredIDs := make(map[int]bool)

	// Deliver messages in batches of up to 5
	totalDelivered := 0
	messagesInBatch := 0
	isFirstMessage := true
	var lastSender string

	for _, sender := range senderOrder {
		senderMessages := messagesBySender[sender]

		for msgIndex, msg := range senderMessages {
			// Check if we've hit the batch limit
			if messagesInBatch >= deliveryLimit {
				// Send notification about remaining messages
				remaining := len(messages) - totalDelivered
				nextBatch := remaining
				if nextBatch > deliveryLimit {
					nextBatch = deliveryLimit
				}

				// Check if any remaining messages are private
				hasPrivateRemaining := false
				for _, checkMsg := range messages {
					if !deliveredIDs[checkMsg.ID] && checkMsg.IsPM {
						hasPrivateRemaining = true
						break
					}
				}

				text := fmt.Sprintf("And I'll give you the next %d in 3 minutes.", nextBatch)
				// Always send batch notification as PM if any remaining messages are private
				// Otherwise, use the current message's privacy setting
				sendAsPM := hasPrivateRemaining || msg.IsPM
				p.sendResponse(channel, username, text, sendAsPM)

				// Schedule next batch delivery
				p.scheduleNextBatch(channel, username, 3*time.Minute)
				return
			}

			// Format the message based on position and sender
			var text string
			if isFirstMessage {
				// First message ever
				text = fmt.Sprintf("Hi %s, %s says: %s", username, sender, msg.Message)
				isFirstMessage = false
			} else if sender != lastSender {
				// Different sender than previous
				if totalDelivered == 1 {
					text = fmt.Sprintf("And %s says: %s", sender, msg.Message)
				} else {
					text = fmt.Sprintf("Also, %s says: %s", sender, msg.Message)
				}
			} else {
				// Same sender as previous
				if msgIndex == 1 {
					text = fmt.Sprintf("and they said: %s", msg.Message)
				} else {
					text = fmt.Sprintf("and also said: %s", msg.Message)
				}
			}

			// Send message based on how the original tell command was sent
			p.sendResponse(channel, username, text, msg.IsPM)

			// Mark as delivered
			if err := p.markDelivered(msg.ID); err != nil {
				logger.Error(p.name, "Failed to mark message %d as delivered: %v", msg.ID, err)
			}

			// Track this message as delivered
			deliveredIDs[msg.ID] = true

			lastSender = sender
			totalDelivered++
			messagesInBatch++

			// Small delay between messages to avoid flooding
			if totalDelivered < len(messages) && messagesInBatch < deliveryLimit {
				select {
				case <-time.After(2 * time.Second):
					// Continue after small delay
				case <-p.ctx.Done():
					return
				}
			}
		}
	}
}

func (p *Plugin) scheduleNextBatch(channel, username string, delay time.Duration) {
	timerKey := fmt.Sprintf("%s:%s:batch", channel, strings.ToLower(username))

	p.mu.Lock()
	// Cancel existing timer if present
	if timer, exists := p.deliveryTimers[timerKey]; exists {
		timer.Stop()
	}

	// Create new timer for the next batch
	p.deliveryTimers[timerKey] = time.AfterFunc(delay, func() {
		logger.Debug(p.name, "Batch timer fired - delivering next batch for user %s in channel %s", username, channel)
		p.deliverMessages(channel, username)

		p.mu.Lock()
		delete(p.deliveryTimers, timerKey)
		p.mu.Unlock()
	})
	p.mu.Unlock()
}

func (p *Plugin) getPendingMessages(channel, username string) ([]tellMessage, error) {
	query := `
		SELECT id, channel, from_user, to_user, message, is_pm, created_at, expires_at, delivered, delivered_at
		FROM daz_tell_messages
		WHERE channel = $1 AND LOWER(to_user) = LOWER($2) AND delivered = FALSE
		ORDER BY created_at ASC
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	var messages []tellMessage
	for rows.Next() {
		var msg tellMessage
		var deliveredAt sql.NullTime

		err := rows.Scan(
			&msg.ID,
			&msg.Channel,
			&msg.FromUser,
			&msg.ToUser,
			&msg.Message,
			&msg.IsPM,
			&msg.CreatedAt,
			&msg.ExpiresAt,
			&msg.Delivered,
			&deliveredAt,
		)
		if err != nil {
			logger.Error(p.name, "Failed to scan message row: %v", err)
			continue
		}

		msg.DeliveredAt = deliveredAt
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return messages, nil
}

// isUserOnlineInDB checks if a user is online in the usertracker database
func (p *Plugin) isUserOnlineInDB(channel, username string) (bool, error) {
	query := `
		SELECT COUNT(*) FROM daz_user_tracker_sessions 
		WHERE channel = $1 AND LOWER(username) = LOWER($2) AND is_active = true
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		return false, fmt.Errorf("no rows returned")
	}

	var count int
	if err := rows.Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

// getActualUsername retrieves the actual case-sensitive username from the database
func (p *Plugin) getActualUsername(channel, lowercaseUsername string) (string, error) {
	query := `
		SELECT username FROM daz_user_tracker_sessions 
		WHERE channel = $1 AND LOWER(username) = LOWER($2) AND is_active = true
		LIMIT 1
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, lowercaseUsername)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	if !rows.Next() {
		// No rows found, user is not online
		return "", nil
	}

	var actualUsername string
	if err := rows.Scan(&actualUsername); err != nil {
		return "", err
	}

	return actualUsername, nil
}

func (p *Plugin) markDelivered(messageID int) error {
	query := `
		UPDATE daz_tell_messages
		SET delivered = TRUE, delivered_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	_, err := p.sqlClient.ExecContext(ctx, query, messageID)
	return err
}

func (p *Plugin) sendResponse(channel, username, message string, isPM bool) {
	if isPM {
		p.sendPM(channel, username, message)
	} else {
		p.sendChannelMessage(channel, message)
	}
}

func (p *Plugin) sendChannelMessage(channel, message string) {
	msgData := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: channel,
			Message: message,
		},
	}

	if err := p.eventBus.Broadcast("cytube.send", msgData); err != nil {
		logger.Error(p.name, "Failed to send chat message: %v", err)
	}
}

func (p *Plugin) sendPM(channel, toUser, message string) {
	pmData := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  toUser,
			Message: message,
			Channel: channel,
		},
	}
	_ = p.eventBus.Broadcast("cytube.send.pm", pmData)
}

func (p *Plugin) cleanupExpiredMessages() {
	defer p.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.deleteExpiredMessages()
		}
	}
}

func (p *Plugin) deleteExpiredMessages() {
	query := `
		DELETE FROM daz_tell_messages
		WHERE expires_at < CURRENT_TIMESTAMP
	`

	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	rowsAffected, err := p.sqlClient.ExecContext(ctx, query)
	if err != nil {
		logger.Error(p.name, "Failed to delete expired messages: %v", err)
		return
	}

	if rowsAffected > 0 {
		logger.Info(p.name, "Deleted %d expired messages", rowsAffected)
	}
}

func (p *Plugin) startPeriodicDeliveryCheck() {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		// Initial delay to let the bot fully connect and populate user lists
		initialDelay := time.NewTimer(30 * time.Second)
		defer initialDelay.Stop()

		select {
		case <-initialDelay.C:
			// Continue to periodic checks
		case <-p.ctx.Done():
			return
		}

		ticker := time.NewTicker(periodicCheckInterval)
		defer ticker.Stop()

		// Do an initial check
		p.checkPendingDeliveries()

		for {
			select {
			case <-ticker.C:
				p.checkPendingDeliveries()
			case <-p.ctx.Done():
				return
			}
		}
	}()
}

func (p *Plugin) checkPendingDeliveries() {
	logger.Debug(p.name, "Running periodic check for pending deliveries")

	// Get all channels we're tracking
	p.mu.RLock()
	channels := make([]string, 0, len(p.onlineUsers))
	for channel := range p.onlineUsers {
		channels = append(channels, channel)
	}
	p.mu.RUnlock()

	// For each channel, check pending messages
	for _, channel := range channels {
		p.checkChannelPendingDeliveries(channel)
	}
}

func (p *Plugin) checkChannelPendingDeliveries(channel string) {
	// Query for all users with pending messages in this channel
	query := `
		SELECT DISTINCT LOWER(to_user) as username
		FROM daz_tell_messages
		WHERE channel = $1 AND delivered = FALSE
	`

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	rows, err := p.sqlClient.QueryContext(ctx, query, channel)
	if err != nil {
		logger.Error(p.name, "Failed to query pending messages: %v", err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Error(p.name, "Failed to close rows: %v", err)
		}
	}()

	var usersToCheck []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			logger.Error(p.name, "Failed to scan username: %v", err)
			continue
		}
		usersToCheck = append(usersToCheck, username)
	}

	if err := rows.Err(); err != nil {
		logger.Error(p.name, "Error iterating rows: %v", err)
		return
	}

	// Check each user and schedule delivery if online
	for _, username := range usersToCheck {
		// Check database first
		isOnline, err := p.isUserOnlineInDB(channel, username)
		if err != nil {
			logger.Error(p.name, "Failed to check user online status in DB: %v", err)
			// Fall back to in-memory check
			p.mu.RLock()
			channelUsers := p.onlineUsers[channel]
			isOnline = channelUsers != nil && channelUsers[username]
			p.mu.RUnlock()
		}

		if isOnline {
			logger.Debug(p.name, "User %s is online in channel %s, scheduling delivery", username, channel)
			// Query the database for the actual username case
			actualUsername, err := p.getActualUsername(channel, username)
			if err != nil {
				logger.Error(p.name, "Failed to get actual username: %v", err)
				// Fall back to in-memory lookup
				p.mu.RLock()
				for user, online := range p.onlineUsers[channel] {
					if strings.ToLower(user) == username && online {
						actualUsername = user
						break
					}
				}
				p.mu.RUnlock()
			}

			if actualUsername != "" {
				p.scheduleMessageDelivery(channel, actualUsername)
			}
		}
	}
}
