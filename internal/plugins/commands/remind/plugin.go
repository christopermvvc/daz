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
	MaxDurationSeconds  int `json:"max_duration_seconds"`
	CooldownSeconds     int `json:"cooldown_seconds"`
	PollIntervalSeconds int `json:"poll_interval_seconds"`
	MaxRunes            int `json:"max_runes"`
}

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient

	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	running       bool
	lastUseByUser map[string]time.Time
	onlineUsers   map[string]map[string]bool
	config        Config
}

const (
	defaultMaxDuration  = 24 * time.Hour
	defaultCooldown     = 3 * time.Second
	defaultPollInterval = 15 * time.Second
	defaultMaxRunes     = 500
	maxBatchDeliveries  = 50
)

func New() framework.Plugin {
	return &Plugin{
		name:          "remind",
		lastUseByUser: make(map[string]time.Time),
		onlineUsers:   make(map[string]map[string]bool),
		config: Config{
			MaxDurationSeconds:  int(defaultMaxDuration.Seconds()),
			CooldownSeconds:     int(defaultCooldown.Seconds()),
			PollIntervalSeconds: int(defaultPollInterval.Seconds()),
			MaxRunes:            defaultMaxRunes,
		},
	}
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
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
	if p.config.PollIntervalSeconds <= 0 {
		p.config.PollIntervalSeconds = int(defaultPollInterval.Seconds())
	}
	if p.config.MaxRunes <= 0 {
		p.config.MaxRunes = defaultMaxRunes
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

	if err := p.createTable(); err != nil {
		return err
	}

	if err := p.eventBus.Subscribe("command.remind.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.remind.execute: %w", err)
	}
	if err := p.eventBus.Subscribe("cytube.event.addUser", p.handleUserJoin); err != nil {
		return fmt.Errorf("failed to subscribe to cytube.event.addUser: %w", err)
	}
	if err := p.eventBus.Subscribe("cytube.event.userLeave", p.handleUserLeave); err != nil {
		return fmt.Errorf("failed to subscribe to cytube.event.userLeave: %w", err)
	}

	p.registerCommands()

	go p.pollDueReminders()

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
	username := params["username"]
	channel := params["channel"]

	if username != "" && channel != "" {
		if remaining, ok := p.checkCooldown(channel, username); !ok {
			msg := fmt.Sprintf("easy on the reminders mate, wait %ds", int(remaining.Seconds())+1)
			p.sendChannelMessage(channel, msg)
			return nil
		}
	}

	args := req.Data.Command.Args
	if len(args) < 2 {
		p.sendChannelMessage(channel, "usage: !remind <time> <message> or !remind <user> <time> <message>")
		return nil
	}

	requester := username
	targetUser := requester
	timeIndex := 0

	if isSelfAlias(args[0]) {
		timeIndex = 1
	} else if !isTimeString(args[0]) {
		targetUser = args[0]
		timeIndex = 1
		if len(args) < 3 {
			p.sendChannelMessage(channel, "usage: !remind <time> <message> or !remind <user> <time> <message>")
			return nil
		}
	}

	if timeIndex >= len(args) {
		p.sendChannelMessage(channel, "usage: !remind <time> <message> or !remind <user> <time> <message>")
		return nil
	}

	parsedDelay, ok := parseTimeString(args[timeIndex])
	if !ok || parsedDelay <= 0 {
		p.sendChannelMessage(channel, "dunno what time that is mate, try like '5m' or '1h30m'")
		return nil
	}

	maxDuration := time.Duration(p.config.MaxDurationSeconds) * time.Second
	if parsedDelay > maxDuration {
		p.sendChannelMessage(channel, "fuck off I'm not remembering that for more than a day")
		return nil
	}

	message := strings.TrimSpace(strings.Join(args[timeIndex+1:], " "))
	if message == "" {
		p.sendChannelMessage(channel, "oi what am I supposed to remind about?")
		return nil
	}

	targetUser = sanitizeTarget(targetUser)
	if targetUser == "" {
		targetUser = requester
	}

	remindAt := time.Now().Add(parsedDelay)
	if err := p.addReminder(channel, requester, targetUser, message, remindAt); err != nil {
		logger.Error(p.name, "Failed to add reminder: %v", err)
		p.sendChannelMessage(channel, "reminder's cooked, try again later")
		return nil
	}

	response := p.pickAck(requester, targetUser, args[timeIndex])
	p.sendChannelMessage(channel, response)
	return nil
}

func (p *Plugin) handleUserJoin(event framework.Event) error {
	var username, channel string

	if addUserEvent, ok := event.(*framework.AddUserEvent); ok {
		username = addUserEvent.Username
		channel = addUserEvent.ChannelName
	} else if dataEvent, ok := event.(*framework.DataEvent); ok && dataEvent.Data != nil {
		if dataEvent.Data.UserJoin != nil {
			username = dataEvent.Data.UserJoin.Username
			channel = dataEvent.Data.UserJoin.Channel
		} else if dataEvent.Data.RawEvent != nil {
			if addUserEvent, ok := dataEvent.Data.RawEvent.(*framework.AddUserEvent); ok {
				username = addUserEvent.Username
				channel = addUserEvent.ChannelName
			}
		}
	}

	if username == "" || channel == "" {
		return nil
	}

	username = normalizeUsername(username)
	channel = strings.TrimSpace(channel)

	p.mu.Lock()
	if _, ok := p.onlineUsers[channel]; !ok {
		p.onlineUsers[channel] = make(map[string]bool)
	}
	p.onlineUsers[channel][username] = true
	p.mu.Unlock()

	go p.deliverDueForUser(channel, username)
	return nil
}

func (p *Plugin) handleUserLeave(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.UserLeave == nil {
		return nil
	}

	username := normalizeUsername(dataEvent.Data.UserLeave.Username)
	channel := strings.TrimSpace(dataEvent.Data.UserLeave.Channel)
	if username == "" || channel == "" {
		return nil
	}

	p.mu.Lock()
	if users, ok := p.onlineUsers[channel]; ok {
		delete(users, username)
		if len(users) == 0 {
			delete(p.onlineUsers, channel)
		}
	}
	p.mu.Unlock()

	return nil
}

func (p *Plugin) pollDueReminders() {
	interval := time.Duration(p.config.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.deliverDueForOnlineUsers()
		}
	}
}

func (p *Plugin) deliverDueForOnlineUsers() {
	usersByChannel := p.snapshotOnlineUsers()
	for channel, users := range usersByChannel {
		for username := range users {
			p.deliverDueForUser(channel, username)
		}
	}
}

func (p *Plugin) snapshotOnlineUsers() map[string]map[string]bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]map[string]bool, len(p.onlineUsers))
	for channel, users := range p.onlineUsers {
		copyUsers := make(map[string]bool, len(users))
		for user := range users {
			copyUsers[user] = true
		}
		result[channel] = copyUsers
	}

	return result
}

func (p *Plugin) deliverDueForUser(channel, username string) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, requester, target, message
		FROM daz_reminders
		WHERE channel = $1 AND target_normalized = $2 AND delivered = false AND remind_at <= NOW()
		ORDER BY remind_at ASC
		LIMIT $3
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username, maxBatchDeliveries)
	if err != nil {
		logger.Error(p.name, "Failed to query reminders: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var requester, target, message string
		if err := rows.Scan(&id, &requester, &target, &message); err != nil {
			logger.Error(p.name, "Failed to scan reminder: %v", err)
			continue
		}

		p.sendReminder(channel, requester, target, message)
		if err := p.markDelivered(ctx, id); err != nil {
			logger.Error(p.name, "Failed to mark reminder delivered: %v", err)
		}
	}
}

func (p *Plugin) sendReminder(channel, requester, target, message string) {
	reqNorm := normalizeUsername(requester)
	targetNorm := normalizeUsername(target)

	message = p.limit(message)
	var text string
	if reqNorm == targetNorm {
		text = fmt.Sprintf("oi -%s, reminder: %s", target, message)
	} else {
		text = fmt.Sprintf("oi -%s, reminder from -%s: %s", target, requester, message)
	}

	p.sendChannelMessage(channel, text)
}

func (p *Plugin) markDelivered(ctx context.Context, id int64) error {
	query := `UPDATE daz_reminders SET delivered = true, delivered_at = NOW() WHERE id = $1`
	_, err := p.sqlClient.ExecContext(ctx, query, id)
	return err
}

func (p *Plugin) addReminder(channel, requester, target, message string, remindAt time.Time) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_reminders (channel, requester, requester_normalized, target, target_normalized, message, remind_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := p.sqlClient.ExecContext(ctx, query,
		strings.TrimSpace(channel),
		strings.TrimSpace(requester),
		normalizeUsername(requester),
		strings.TrimSpace(target),
		normalizeUsername(target),
		message,
		remindAt,
	)

	return err
}

func (p *Plugin) createTable() error {
	schema := `
		CREATE TABLE IF NOT EXISTS daz_reminders (
			id BIGSERIAL PRIMARY KEY,
			channel VARCHAR(255) NOT NULL,
			requester VARCHAR(255) NOT NULL,
			requester_normalized VARCHAR(255) NOT NULL,
			target VARCHAR(255) NOT NULL,
			target_normalized VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			remind_at TIMESTAMP WITH TIME ZONE NOT NULL,
			delivered BOOLEAN NOT NULL DEFAULT FALSE,
			delivered_at TIMESTAMP WITH TIME ZONE
		);

		CREATE INDEX IF NOT EXISTS idx_reminders_due
			ON daz_reminders(channel, target_normalized, remind_at)
			WHERE delivered = false;
		CREATE INDEX IF NOT EXISTS idx_reminders_target
			ON daz_reminders(target_normalized, delivered);
	`

	if err := p.sqlClient.Exec(schema); err != nil {
		return fmt.Errorf("failed to create reminders table: %w", err)
	}
	return nil
}

func (p *Plugin) pickAck(requester, target, timeStr string) string {
	selfResponses := []string{
		fmt.Sprintf("righto -%s, I'll remind ya in %s", requester, timeStr),
		fmt.Sprintf("no worries -%s, I'll give ya a buzz in %s", requester, timeStr),
		fmt.Sprintf("yeah mate, I'll yell at ya in %s", timeStr),
		fmt.Sprintf("sweet as -%s, reminder set for %s", requester, timeStr),
		fmt.Sprintf("got it -%s, I'll poke ya in %s", requester, timeStr),
		fmt.Sprintf("easy -%s, I'll hassle ya in %s", requester, timeStr),
		fmt.Sprintf("too easy mate, I'll remind ya in %s unless I'm on the piss", timeStr),
		fmt.Sprintf("alright -%s, %s from now I'll give ya a shout", requester, timeStr),
		"set a reminder on me phone... if I remember to charge it",
		"yeah nah I'll try remember mate, no promises after this cone",
		"reminders set, unless I'm passed out by then",
		fmt.Sprintf("I'll remind ya in %s unless shazza's got me doin chores", timeStr),
		fmt.Sprintf("%s from now I'll yell at ya, if I'm not too cooked", timeStr),
		fmt.Sprintf("wrote it on me hand in permanent marker, see ya in %s", timeStr),
		fmt.Sprintf("reminder set for %s, right after me next bong", timeStr),
		fmt.Sprintf("I'll remind ya mate but %s is a long time to stay sober", timeStr),
		fmt.Sprintf("got it, I'll hassle ya in %s like shazza hassles me for rent", timeStr),
		"reminder locked in tighter than me balls in these boardies",
		fmt.Sprintf("%s reminder set, that's about 3 cones from now", timeStr),
		fmt.Sprintf("I'll ping ya in %s unless I'm balls deep in somethin", timeStr),
		"reminder set mate, written on the back of a durrie packet",
	}

	userResponses := []string{
		fmt.Sprintf("yeah alright, I'll tell -%s in %s", target, timeStr),
		fmt.Sprintf("no wukkas, I'll pass it on to -%s in %s", target, timeStr),
		fmt.Sprintf("righto, I'll hassle -%s about it in %s", target, timeStr),
		fmt.Sprintf("sweet, I'll give -%s a yell in %s", target, timeStr),
		fmt.Sprintf("easy done, -%s gets the message in %s", target, timeStr),
		fmt.Sprintf("got it mate, I'll bug -%s in %s", target, timeStr),
		fmt.Sprintf("sure thing, I'll let -%s know in %s", target, timeStr),
		fmt.Sprintf("no dramas, I'll remind -%s in %s if they're around", target, timeStr),
		fmt.Sprintf("I'll nag -%s worse than me missus in %s", target, timeStr),
		fmt.Sprintf("gonna hassle -%s like a debt collector in %s", target, timeStr),
		fmt.Sprintf("I'll pester -%s in %s unless they fucked off", target, timeStr),
		fmt.Sprintf("reminder set to annoy the shit outta -%s in %s", target, timeStr),
		fmt.Sprintf("I'll harass -%s about it in %s", target, timeStr),
		fmt.Sprintf("gonna remind -%s harder than shazza reminds me about child support", target),
		fmt.Sprintf("I'll tell -%s in %s if they haven't carked it", target, timeStr),
		fmt.Sprintf("set to bother -%s in %s like a mozzie at night", target, timeStr),
		fmt.Sprintf("I'll bug -%s about it in %s, no escape", target, timeStr),
		fmt.Sprintf("gonna remind -%s like I'm their disappointed mother in %s", target, timeStr),
	}

	if normalizeUsername(requester) == normalizeUsername(target) {
		return p.limit(randomChoice(selfResponses))
	}

	return p.limit(randomChoice(userResponses))
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

func (p *Plugin) limit(message string) string {
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= p.config.MaxRunes {
		return message
	}
	return string(runes[:p.config.MaxRunes]) + "..."
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func isSelfAlias(value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
	if value == "" {
		return false
	}
	return value == "me" || value == "self" || value == "myself"
}

func sanitizeTarget(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	return value
}

func isTimeString(value string) bool {
	_, ok := parseTimeString(value)
	return ok
}

func parseTimeString(value string) (time.Duration, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0, false
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

func randomChoice(options []string) string {
	if len(options) == 0 {
		return ""
	}
	idx := time.Now().UnixNano() % int64(len(options))
	return options[idx]
}
