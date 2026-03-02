package oddjob

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	CooldownHours int `json:"cooldown_hours"`
}

type Plugin struct {
	name          string
	eventBus      framework.EventBus
	sqlClient     *framework.SQLClient
	economyClient *framework.EconomyClient

	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
	running bool

	config   Config
	cooldown time.Duration
}

const defaultCooldownHours = 8

func New() framework.Plugin {
	return &Plugin{
		name:     "oddjob",
		cooldown: 8 * time.Hour,
	}
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql", "economy"}
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.economyClient = framework.NewEconomyClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		if p.config.CooldownHours > 0 {
			p.cooldown = time.Duration(p.config.CooldownHours) * time.Hour
		}
	}
	if p.cooldown <= 0 {
		p.cooldown = time.Duration(defaultCooldownHours) * time.Hour
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

	if err := p.registerCommands(); err != nil {
		return err
	}

	for _, cmd := range []string{"oddjob", "job", "shift"} {
		eventName := fmt.Sprintf("command.%s.execute", cmd)
		if err := p.eventBus.Subscribe(eventName, p.handleCommand); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", eventName, err)
		}
	}

	framework.SeedMathRand()
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

func (p *Plugin) registerCommands() error {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands":    "oddjob,job,shift",
					"min_rank":    "0",
					"description": "do an odd job for cash",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register oddjob command: %v", err)
		return fmt.Errorf("failed to register oddjob command: %w", err)
	}
	return nil
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
	isPM := params["is_pm"] == "true"
	if username == "" || channel == "" {
		return nil
	}

	remaining, ok, err := p.checkCooldown(channel, username)
	if err != nil {
		logger.Error(p.name, "Cooldown check failed: %v", err)
		p.sendResponse(channel, username, "boss ain't answerin' the phone. try again later", isPM)
		return nil
	}
	if !ok {
		p.sendResponse(channel, username, waitMessage(username, remaining), isPM)
		return nil
	}

	intro := introMessages[rand.Intn(len(introMessages))]
	p.sendResponse(channel, username, fmt.Sprintf(intro, username), isPM)

	failed := oddjobFailureRoll()
	if failed {
		p.updateStats(channel, username, oddjobUpdate{Jobs: 1, Fails: 1, LastPlayed: time.Now()})
		p.sendResponse(channel, username, fmt.Sprintf("❌ %s", failMessages[rand.Intn(len(failMessages))]), isPM)
		return nil
	}

	payout := rand.Intn(41) + 10
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	_, err = p.economyClient.Credit(ctx, framework.CreditRequest{Channel: channel, Username: username, Amount: int64(payout), Reason: "oddjob"})
	if err != nil {
		logger.Error(p.name, "Failed to credit oddjob payout: %v", err)
		p.sendResponse(channel, username, "oddjob boss stiffed ya, try again later", isPM)
		return nil
	}

	p.updateStats(channel, username, oddjobUpdate{Jobs: 1, Earnings: int64(payout), LastPlayed: time.Now()})
	p.sendResponse(channel, username, fmt.Sprintf("✅ %s You earned $%d.", successMessages[rand.Intn(len(successMessages))], payout), isPM)
	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	query := `
		SELECT last_played_at
		FROM daz_oddjob_stats
		WHERE channel = $1 AND username = $2
		LIMIT 1
	`
	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		return 0, true, nil
	}
	var lastPlayed time.Time
	if err := rows.Scan(&lastPlayed); err != nil {
		return 0, false, err
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if lastPlayed.IsZero() {
		return 0, true, nil
	}
	until := lastPlayed.Add(p.cooldown)
	if time.Now().Before(until) {
		return until.Sub(time.Now()), false, nil
	}
	return 0, true, nil
}

type oddjobUpdate struct {
	Jobs       int
	Fails      int
	Earnings   int64
	LastPlayed time.Time
}

func (p *Plugin) updateStats(channel, username string, update oddjobUpdate) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	query := `
		INSERT INTO daz_oddjob_stats (
			channel, username, total_jobs, total_earnings, failures, last_played_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (channel, username) DO UPDATE SET
			total_jobs = daz_oddjob_stats.total_jobs + EXCLUDED.total_jobs,
			total_earnings = daz_oddjob_stats.total_earnings + EXCLUDED.total_earnings,
			failures = daz_oddjob_stats.failures + EXCLUDED.failures,
			last_played_at = EXCLUDED.last_played_at,
			updated_at = NOW()
	`
	_, err := p.sqlClient.ExecContext(ctx, query,
		channel,
		username,
		update.Jobs,
		update.Earnings,
		update.Fails,
		update.LastPlayed,
	)
	if err != nil {
		logger.Error(p.name, "Failed to update oddjob stats: %v", err)
	}
}

func (p *Plugin) sendResponse(channel, username, message string, isPM bool) {
	if strings.TrimSpace(message) == "" {
		return
	}
	if isPM {
		pm := &framework.EventData{PrivateMessage: &framework.PrivateMessageData{ToUser: username, Message: message, Channel: channel}}
		_ = p.eventBus.Broadcast("cytube.send.pm", pm)
		return
	}
	chat := &framework.EventData{RawMessage: &framework.RawMessageData{Message: message, Channel: channel}}
	_ = p.eventBus.Broadcast("cytube.send", chat)
}

func waitMessage(username string, remaining time.Duration) string {
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	responses := []string{
		fmt.Sprintf("oi -%s, no more jobs for %dh %dm. boss said piss off", username, hours, minutes),
		fmt.Sprintf("-%s mate, you already did a shift. %dh %dm left", username, hours, minutes),
		fmt.Sprintf("take a breather -%s, next oddjob in %dh %dm", username, hours, minutes),
		fmt.Sprintf("-%s you're on cooldown. try again in %dh %dm", username, hours, minutes),
		fmt.Sprintf("no shifts left -%s, wait %dh %dm", username, hours, minutes),
	}
	return responses[rand.Intn(len(responses))]
}

var introMessages = []string{
	"-%s is heading out for an oddjob...",
	"-%s just scored a dodgy cash-in-hand gig",
	"-%s is off to do a quick shift",
	"-%s is doing a random job for some coins",
}

var successMessages = []string{
	"painted a fence and didn't fall over.",
	"mowed a lawn without losing a toe.",
	"hauled a couch down three flights.",
	"washed a ute for a bloke named Davo.",
	"stacked shelves at Woolies without nicking anything.",
}

var failMessages = []string{
	"got fired for showing up late and stoned",
	"spilled paint everywhere and bolted",
	"fell asleep on the job and got the boot",
	"broke the boss's whipper snipper",
	"forgot to turn up. no pay today",
}

var oddjobFailureRoll = func() bool {
	return rand.Float64() < 0.20
}
