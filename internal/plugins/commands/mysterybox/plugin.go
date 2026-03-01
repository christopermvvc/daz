package mysterybox

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

const defaultCooldownHours = 24

func New() framework.Plugin {
	return &Plugin{
		name:     "mysterybox",
		cooldown: 24 * time.Hour,
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

	for _, cmd := range []string{"mystery_box", "mysterybox", "box"} {
		eventName := fmt.Sprintf("command.%s.execute", cmd)
		if err := p.eventBus.Subscribe(eventName, p.handleCommand); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", eventName, err)
		}
	}

	rand.Seed(time.Now().UnixNano())
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
					"commands":    "mystery_box,mysterybox,box",
					"min_rank":    "0",
					"description": "open a free mystery box",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register mystery box command: %v", err)
		return fmt.Errorf("failed to register mystery box command: %w", err)
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
		p.sendResponse(channel, username, "box machine's cactus right now, try again later", isPM)
		return nil
	}
	if !ok {
		p.sendResponse(channel, username, fmt.Sprintf("-%s ya already cracked a box today. wait %dh %dm", username, int(remaining.Hours()), int(remaining.Minutes())%60), isPM)
		return nil
	}

	result := rollMysteryBoxFunc()
	message := result.message
	if result.amount > 0 {
		ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
		defer cancel()
		_, err := p.economyClient.Credit(ctx, framework.CreditRequest{Channel: channel, Username: username, Amount: int64(result.amount), Reason: "mystery_box"})
		if err != nil {
			logger.Error(p.name, "Failed to credit mystery box winnings: %v", err)
			p.sendResponse(channel, username, "mystery box jammed. try again later", isPM)
			return nil
		}
		message = fmt.Sprintf("%s You scored $%d!", message, result.amount)
	}

	p.sendResponse(channel, username, fmt.Sprintf("🎁 %s", message), isPM)

	if err := p.updateStats(channel, username, result); err != nil {
		logger.Error(p.name, "Failed to update mystery box stats: %v", err)
	}

	if result.bigWin {
		announcement := fmt.Sprintf("🎁💥 %s just pulled a $%d mystery box jackpot!", username, result.amount)
		time.AfterFunc(2*time.Second, func() {
			select {
			case <-p.ctx.Done():
				return
			default:
				p.sendResponse(channel, username, announcement, false)
			}
		})
	}

	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	query := `
		SELECT last_played_at
		FROM daz_mystery_box_stats
		WHERE channel = $1 AND username = $2
		LIMIT 1
	`
	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, true, nil
	}
	var lastPlayed time.Time
	if err := rows.Scan(&lastPlayed); err != nil {
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

func (p *Plugin) updateStats(channel, username string, result mysteryResult) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	currentStreak := 0
	if result.amount > 0 {
		currentStreak = 1
	}

	query := `
		INSERT INTO daz_mystery_box_stats (
			channel, username, total_opens, total_winnings, jackpots_won, bombs_hit,
			best_win, worst_loss, current_streak, best_streak, last_played_at, updated_at
		) VALUES (
			$1, $2, 1, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW()
		)
		ON CONFLICT (channel, username) DO UPDATE SET
			total_opens = daz_mystery_box_stats.total_opens + 1,
			total_winnings = daz_mystery_box_stats.total_winnings + EXCLUDED.total_winnings,
			jackpots_won = daz_mystery_box_stats.jackpots_won + EXCLUDED.jackpots_won,
			bombs_hit = daz_mystery_box_stats.bombs_hit + EXCLUDED.bombs_hit,
			best_win = GREATEST(daz_mystery_box_stats.best_win, EXCLUDED.best_win),
			worst_loss = CASE
				WHEN daz_mystery_box_stats.worst_loss = 0 THEN EXCLUDED.worst_loss
				ELSE LEAST(daz_mystery_box_stats.worst_loss, EXCLUDED.worst_loss)
			END,
			current_streak = CASE WHEN EXCLUDED.current_streak = 0 THEN 0 ELSE daz_mystery_box_stats.current_streak + 1 END,
			best_streak = GREATEST(daz_mystery_box_stats.best_streak, CASE WHEN EXCLUDED.current_streak = 0 THEN 0 ELSE daz_mystery_box_stats.current_streak + 1 END),
			last_played_at = NOW(),
			updated_at = NOW()
	`

	_, err := p.sqlClient.ExecContext(ctx, query,
		channel,
		username,
		result.amount,
		boolToInt(result.jackpot),
		boolToInt(result.amount == 0),
		result.amount,
		0,
		currentStreak,
		currentStreak,
	)
	return err
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

type mysteryResult struct {
	amount  int64
	message string
	jackpot bool
	bigWin  bool
}

func rollMysteryBox() mysteryResult {
	roll := rand.Float64()
	if roll < 0.70 {
		junk := randomChoice(junkFinds)
		return mysteryResult{amount: 0, message: fmt.Sprintf("empty box... just %s", junk), jackpot: false, bigWin: false}
	}
	if roll < 0.95 {
		amount := rand.Intn(16) + 5
		return mysteryResult{amount: int64(amount), message: "nice!", jackpot: false, bigWin: false}
	}
	if roll < 0.99 {
		amount := rand.Intn(51) + 25
		return mysteryResult{amount: int64(amount), message: "bloody beauty!", jackpot: false, bigWin: amount >= 50}
	}
	amount := rand.Intn(301) + 200
	return mysteryResult{amount: int64(amount), message: "JACKPOT!", jackpot: true, bigWin: true}
}

var rollMysteryBoxFunc = rollMysteryBox

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func randomChoice(options []string) string {
	return options[rand.Intn(len(options))]
}

var junkFinds = []string{
	"fish bones",
	"a rusty nail",
	"dusty bottlecaps",
	"a single shoelace",
	"mystery lint",
	"a bent spoon",
	"a dead lighter",
	"a crushed durrie",
}
