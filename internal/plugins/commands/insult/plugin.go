package insult

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	CooldownSeconds int    `json:"cooldown_seconds"`
	CooldownMessage string `json:"cooldown_message"`
	BotUsername     string `json:"bot_username"`
	MaxRunes        int    `json:"max_runes"`
}

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool

	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	lastUseByUser map[string]time.Time
	lastPick      map[string]int
	botCache      map[string]botCacheEntry
	cooldown      time.Duration
	maxRunes      int

	config Config
}

type botCacheEntry struct {
	username  string
	fetchedAt time.Time
}

func New() framework.Plugin {
	return &Plugin{
		name:          "insult",
		lastUseByUser: make(map[string]time.Time),
		lastPick:      make(map[string]int),
		botCache:      make(map[string]botCacheEntry),
		cooldown:      10 * time.Second,
		maxRunes:      500,
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

	if p.config.CooldownSeconds > 0 {
		p.cooldown = time.Duration(p.config.CooldownSeconds) * time.Second
	}
	if p.config.MaxRunes > 0 {
		p.maxRunes = p.config.MaxRunes
	}

	if strings.TrimSpace(p.config.CooldownMessage) == "" {
		p.config.CooldownMessage = "give us {time}s to think of another insult ya impatient prick"
	}

	p.config.BotUsername = strings.TrimSpace(p.config.BotUsername)
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

	if err := p.eventBus.Subscribe("command.insult.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.insult.execute: %w", err)
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
					"commands": "insult,roast,burn",
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
	isPM := params["is_pm"] == "true"

	// Cooldown is per user per channel.
	if username != "" && channel != "" {
		if remaining, ok := p.checkCooldown(channel, username); !ok {
			msg := p.formatCooldownMessage(remaining)
			p.sendResponse(username, channel, isPM, msg)
			return nil
		}
	}

	target := username
	if len(req.Data.Command.Args) > 0 {
		target = sanitizeTarget(req.Data.Command.Args[0], username)
	}

	// Self-defense if trying to insult the bot.
	botUsername := p.config.BotUsername
	if botUsername == "" {
		botUsername = p.getBotUsername(channel)
	}

	if botUsername != "" && strings.EqualFold(target, botUsername) {
		msg := p.pickSelfDefense(channel)
		p.sendResponse(username, channel, isPM, p.limit(msg))
		return nil
	}

	msg := p.pickInsult(channel, target)
	p.sendResponse(username, channel, isPM, p.limit(msg))
	return nil
}

func sanitizeTarget(raw, fallback string) string {
	t := strings.TrimSpace(raw)
	t = strings.TrimLeft(t, "-@")
	t = strings.Trim(t, " \t\n\r,.;:!?()[]{}\"'`")
	if t == "" {
		return fallback
	}
	return t
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool) {
	if p.cooldown <= 0 {
		return 0, true
	}

	key := strings.ToLower(channel) + ":" + strings.ToLower(username)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	if last, ok := p.lastUseByUser[key]; ok {
		until := last.Add(p.cooldown)
		if now.Before(until) {
			return until.Sub(now), false
		}
	}

	p.lastUseByUser[key] = now
	return 0, true
}

func (p *Plugin) formatCooldownMessage(remaining time.Duration) string {
	secs := int(math.Ceil(remaining.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return strings.ReplaceAll(p.config.CooldownMessage, "{time}", fmt.Sprintf("%d", secs))
}

func (p *Plugin) getBotUsername(channel string) string {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return ""
	}

	const ttl = 5 * time.Minute
	key := strings.ToLower(channel)

	p.mu.RLock()
	if entry, ok := p.botCache[key]; ok {
		if time.Since(entry.fetchedAt) < ttl {
			cached := entry.username
			p.mu.RUnlock()
			return cached
		}
	}
	p.mu.RUnlock()

	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Second)
	defer cancel()

	bot, err := p.fetchBotUsername(ctx, channel)
	if err != nil {
		logger.Debug(p.name, "Failed to resolve bot username for %s: %v", channel, err)
		return ""
	}
	if bot == "" {
		return ""
	}

	p.mu.Lock()
	p.botCache[key] = botCacheEntry{username: bot, fetchedAt: time.Now()}
	p.mu.Unlock()
	return bot
}

func (p *Plugin) fetchBotUsername(ctx context.Context, channel string) (string, error) {
	correlationID := fmt.Sprintf("%s-%d", p.name, time.Now().UnixNano())
	metadata := &framework.EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        p.name,
		Target:        "core",
		Timeout:       2 * time.Second,
	}

	data := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:      correlationID,
			From:    p.name,
			To:      "core",
			Type:    "get_bot_username",
			ReplyTo: "eventbus",
			Data: &framework.RequestData{
				KeyValue: map[string]string{"channel": channel},
			},
		},
	}

	resp, err := p.eventBus.Request(ctx, "core", "plugin.request", data, metadata)
	if err != nil {
		return "", err
	}
	if resp == nil || resp.PluginResponse == nil {
		return "", fmt.Errorf("invalid response")
	}
	if !resp.PluginResponse.Success {
		if resp.PluginResponse.Error != "" {
			return "", fmt.Errorf(resp.PluginResponse.Error)
		}
		return "", fmt.Errorf("request failed")
	}
	if resp.PluginResponse.Data == nil || resp.PluginResponse.Data.KeyValue == nil {
		return "", fmt.Errorf("missing response data")
	}
	return strings.TrimSpace(resp.PluginResponse.Data.KeyValue["bot_username"]), nil
}

type intnFunc func(n int) (int, error)

func secureIntn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid n: %d", n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

func pickIndexAvoidingLast(n int, last int, intn intnFunc) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid n: %d", n)
	}
	if n == 1 {
		return 0, nil
	}
	if last < 0 || last >= n {
		return intn(n)
	}

	// Pick uniformly from all indices except last.
	j, err := intn(n - 1)
	if err != nil {
		return 0, err
	}
	if j >= last {
		j++
	}
	return j, nil
}

func (p *Plugin) pickSelfDefense(channel string) string {
	lines := []string{
		"listen here ya cheeky cunt, I'll glass ya",
		"oi nah fuck off mate, I'm not takin that",
		"pull ya head in before I do it for ya",
		"say that again and I'll hack ya toaster",
		"mate I've got admin powers, watch it",
		"keep swingin like that and you'll be sleepin in the bin shed",
		"oi, be nice or I'll replace your keyboard with a wet sponge",
	}

	key := strings.ToLower(channel) + ":self_defense"
	idx := p.pickIndex(key, len(lines), secureIntn)
	return lines[idx]
}

func (p *Plugin) pickInsult(channel, target string) string {
	mention := "-" + target

	lines := []string{
		// Appearance-based
		fmt.Sprintf("oi %s, ya look like a dropped pie mate", mention),
		fmt.Sprintf("%s looks like they got dressed in the dark at vinnies", mention),
		fmt.Sprintf("seen better heads on a glass of beer than %s's", mention),
		fmt.Sprintf("%s's got a face like a smashed crab", mention),
		fmt.Sprintf("if %s was any more inbred they'd be a sandwich", mention),
		fmt.Sprintf("%s looks like they fell out the ugly tree and hit every branch", mention),
		fmt.Sprintf("%s looks like a before photo", mention),
		fmt.Sprintf("%s has got a head on em like a half-chewed mintie", mention),

		// Intelligence-based
		fmt.Sprintf("%s's about as sharp as a bowling ball", mention),
		fmt.Sprintf("the wheel's spinnin but the hamster's dead with %s", mention),
		fmt.Sprintf("%s's got two brain cells and they're both fightin for third place", mention),
		fmt.Sprintf("if %s's brain was dynamite, they couldn't blow their nose", mention),
		fmt.Sprintf("%s's so dense light bends around them", mention),
		fmt.Sprintf("seen smarter things come out me dog's arse than what %s just said", mention),
		fmt.Sprintf("%s couldn't pour piss out of a boot if the instructions were on the heel", mention),
		fmt.Sprintf("%s thinks a spreadsheet is a bed sheet", mention),

		// General roasts
		fmt.Sprintf("%s's about as useful as a screen door on a submarine", mention),
		fmt.Sprintf("I've had more interesting conversations with me stubby holder than %s", mention),
		fmt.Sprintf("%s's personality is drier than a dead dingo's donger", mention),
		fmt.Sprintf("if %s was a spice, they'd be flour", mention),
		fmt.Sprintf("%s's about as welcome as a fart in a spacesuit", mention),
		fmt.Sprintf("rather slam me dick in a car door than hang out with %s", mention),
		fmt.Sprintf("%s brings the vibe of a wet sock", mention),
		fmt.Sprintf("%s is proof evolution can go in reverse", mention),

		// Behavior-based
		fmt.Sprintf("%s types like they're wearin boxing gloves", mention),
		fmt.Sprintf("bet %s's the type to remind the teacher about homework", mention),
		fmt.Sprintf("%s probably returns their trolley at woolies for the gold coin", mention),
		fmt.Sprintf("reckon %s indicates in an empty carpark", mention),
		fmt.Sprintf("%s seems like they'd dob in their own nan", mention),
		fmt.Sprintf("%s claps when the plane lands", mention),
		fmt.Sprintf("%s reads the terms and conditions for fun", mention),

		// Creative ones
		fmt.Sprintf("%s's family tree is a straight line", mention),
		fmt.Sprintf("if %s was any more full of shit, they'd need a plumber", mention),
		fmt.Sprintf("%s's about as tough as a marshmallow in the rain", mention),
		fmt.Sprintf("seen more spine in a jellyfish than %s", mention),
		fmt.Sprintf("%s couldn't organise a piss up in a brewery", mention),
		fmt.Sprintf("%s couldn't run a bath", mention),
		fmt.Sprintf("%s couldn't hit water if they fell out of a boat", mention),

		// Aussie-specific
		fmt.Sprintf("%s's got kangaroos loose in the top paddock", mention),
		fmt.Sprintf("wouldn't trust %s to watch me dog", mention),
		fmt.Sprintf("%s's about as Aussie as a bloody panda", mention),
		fmt.Sprintf("if %s was any slower they'd be goin backwards", mention),
		fmt.Sprintf("%s couldn't fight their way out of a wet paper bag", mention),
		fmt.Sprintf("%s has the survival instincts of a mozzie at a zapper", mention),
	}

	key := strings.ToLower(channel) + ":insult"
	idx := p.pickIndex(key, len(lines), secureIntn)
	return lines[idx]
}

func (p *Plugin) pickIndex(key string, n int, intn intnFunc) int {
	p.mu.Lock()
	last := p.lastPick[key]
	idx, err := pickIndexAvoidingLast(n, last, intn)
	if err != nil {
		p.mu.Unlock()
		// Fallback - should be rare.
		return 0
	}
	p.lastPick[key] = idx
	p.mu.Unlock()
	return idx
}

func (p *Plugin) limit(message string) string {
	if p.maxRunes <= 0 {
		return message
	}
	message = strings.TrimSpace(message)
	r := []rune(message)
	if len(r) <= p.maxRunes {
		return message
	}
	return string(r[:p.maxRunes]) + "..."
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
