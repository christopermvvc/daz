package bong

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
	MaxRunes        int    `json:"max_runes"`
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
	lastPick      map[string]int
	cooldown      time.Duration
	maxRunes      int

	config Config
}

func New() framework.Plugin {
	return &Plugin{
		name:          "bong",
		lastUseByUser: make(map[string]time.Time),
		lastPick:      make(map[string]int),
		cooldown:      5 * time.Minute,
		maxRunes:      500,
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

	if len(config) == 0 {
		p.config.CooldownMessage = defaultCooldownMessage
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
		p.config.CooldownMessage = defaultCooldownMessage
	}

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.mu.Unlock()

	if err := p.eventBus.Subscribe("command.bong.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.bong.execute: %w", err)
	}

	p.registerCommands()
	logger.Debug(p.name, "Started")

	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

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
					"commands": "bong,rip,cone,billy",
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

	if username != "" && channel != "" {
		if remaining, ok := p.checkCooldown(channel, username); !ok {
			msg := p.formatCooldownMessage(remaining)
			p.sendResponse(username, channel, isPM, p.limit(msg))
			return nil
		}
	}

	newCount := int64(0)
	countValid := false
	if channel != "" && username != "" {
		if err := p.logUserBong(channel, username); err != nil {
			logger.Error(p.name, "Failed to log user bong: %v", err)
		} else {
			count, err := p.getDailyCount(channel)
			if err != nil {
				logger.Error(p.name, "Failed to fetch daily bong count: %v", err)
			} else {
				newCount = count
				countValid = true
			}
		}
	}

	response := ""
	if countValid {
		response = p.pickResponse(channel, newCount)
	} else {
		response = p.pickFallbackResponse(channel)
	}
	p.sendResponse(username, channel, isPM, p.limit(response))

	if countValid {
		p.maybeSendMilestone(username, channel, isPM, newCount)
	}
	return nil
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

func (p *Plugin) logUserBong(channel, username string) error {
	normalized := normalizeUsername(username)
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `INSERT INTO daz_bong_sessions (channel, username, session_start, session_end, cone_count)
		VALUES ($1, $2, NOW(), NOW(), 1)`

	_, err := p.sqlClient.ExecContext(ctx, query, channel, normalized)
	return err
}

func (p *Plugin) getDailyCount(channel string) (int64, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `SELECT COALESCE(SUM(cone_count), 0)
		FROM daz_bong_sessions
		WHERE channel = $1 AND session_start::date = CURRENT_DATE`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total int64
	if rows.Next() {
		if err := rows.Scan(&total); err != nil {
			return 0, err
		}
	}

	return total, nil
}

func (p *Plugin) pickResponse(channel string, count int64) string {
	lines := []string{
		fmt.Sprintf("🌿 That's bong number %d for today mate", count),
		fmt.Sprintf("🌿 %d cones punched today, feelin' good", count),
		fmt.Sprintf("🌿 Bong %d done, Shazza's gonna kill me", count),
		fmt.Sprintf("🌿 %d billies today, fuckin' legend", count),
		fmt.Sprintf("🌿 Cone %d sorted, time for a dart", count),
		fmt.Sprintf("🌿 *takes a massive fuckin rip* number %d down the hatch", count),
		fmt.Sprintf("🌿 *coughs violently* fuck me dead that was number %d", count),
		fmt.Sprintf("🌿 *bubbling sounds* ... *exhales* ... %d today, fuckin oath", count),
		fmt.Sprintf("🌿 Number %d... I'm already cooked as... *rips it anyway*", count),
		fmt.Sprintf("🌿 *packs a fresh cone* number %d for you legends *massive rip*", count),
		fmt.Sprintf("🌿 %d today already but... *takes another hit*", count),
		fmt.Sprintf("🌿 *chops up* oi Shazza! That's %d! *bubbling sounds*", count),
		fmt.Sprintf("🌿 *coughing fit* number %d went straight to me head", count),
		fmt.Sprintf("🌿 *rips the billy* %d down, yeah nah yeah that's fuckin mint", count),
		fmt.Sprintf("🌿 Number %d? *loads up the Gatorade bottle bong*", count),
	}

	key := strings.ToLower(channel)
	idx := p.pickIndex(key, len(lines))
	return lines[idx]
}

func (p *Plugin) pickFallbackResponse(channel string) string {
	lines := []string{
		"🌿 *packs a cone* righto, sorted",
		"🌿 *bubbling sounds* ... *exhales*",
		"🌿 cone sorted, yeah nah yeah",
		"🌿 *coughs* fuck me that was a rip",
		"🌿 *rips the billy* mint",
	}

	key := strings.ToLower(channel) + ":fallback"
	idx := p.pickIndex(key, len(lines))
	return lines[idx]
}

func (p *Plugin) pickIndex(key string, n int) int {
	p.mu.Lock()
	last := p.lastPick[key]
	idx, err := pickIndexAvoidingLast(n, last, secureIntn)
	if err != nil {
		p.mu.Unlock()
		return 0
	}
	p.lastPick[key] = idx
	p.mu.Unlock()
	return idx
}

type intnFunc func(n int) (int, error)

func secureIntn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid n: %d", n)
	}
	val, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(val.Int64()), nil
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

	j, err := intn(n - 1)
	if err != nil {
		return 0, err
	}
	if j >= last {
		j++
	}
	return j, nil
}

func (p *Plugin) maybeSendMilestone(username, channel string, isPM bool, count int64) {
	if count <= 0 {
		return
	}

	message := ""
	if count%50 == 0 {
		message = fmt.Sprintf("fuckin hell lads, that's %d cones today! I think I can see through time", count)
	} else if count%25 == 0 {
		message = fmt.Sprintf("%d billies! me lungs are fucked but we soldier on", count)
	}

	if message == "" {
		return
	}

	message = p.limit(message)
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return
	}

	time.AfterFunc(2*time.Second, func() {
		select {
		case <-p.ctx.Done():
			return
		default:
			p.sendResponse(username, channel, isPM, message)
		}
	})
}

func (p *Plugin) limit(message string) string {
	if p.maxRunes <= 0 {
		return strings.TrimSpace(message)
	}
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) <= p.maxRunes {
		return message
	}
	return string(runes[:p.maxRunes]) + "..."
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
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

const defaultCooldownMessage = "easy on the cones mate, ya lungs need {time}s to recover from that last rip"
