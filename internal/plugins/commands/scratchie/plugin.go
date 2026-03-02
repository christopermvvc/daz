package scratchie

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Config struct {
	CooldownSeconds int `json:"cooldown_seconds"`
}

type Plugin struct {
	name          string
	eventBus      framework.EventBus
	economyClient *framework.EconomyClient

	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	cooldown time.Duration
	lastUse  map[string]time.Time
	config   Config
}

const defaultCooldown = 5 * time.Second

func New() framework.Plugin {
	return &Plugin{
		name:     "scratchie",
		cooldown: defaultCooldown,
		lastUse:  make(map[string]time.Time),
	}
}

func (p *Plugin) Dependencies() []string {
	return []string{"economy"}
}

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.economyClient = framework.NewEconomyClient(bus, p.name)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
		if p.config.CooldownSeconds > 0 {
			p.cooldown = time.Duration(p.config.CooldownSeconds) * time.Second
		}
	}
	if p.cooldown <= 0 {
		p.cooldown = defaultCooldown
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

	for _, cmd := range []string{"scratchie", "scratch", "scratchies", "lotto"} {
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
					"commands":    "scratchie,scratch,scratchies,lotto",
					"min_rank":    "0",
					"description": "buy a scratchie ($5, $20, $50, $100)",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register scratchie command: %v", err)
		return fmt.Errorf("failed to register scratchie command: %w", err)
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

	if remaining, ok := p.checkCooldown(channel, username); !ok {
		msg := fmt.Sprintf("still scratchin' the last one with me car keys mate, gimme %ds", int(remaining.Seconds())+1)
		p.sendResponse(channel, username, msg, isPM)
		return nil
	}

	amount := 5
	if len(req.Data.Command.Args) > 0 {
		parsed, err := strconv.Atoi(req.Data.Command.Args[0])
		if err != nil || !isValidTicketAmount(parsed) {
			p.sendResponse(channel, username, "scratchies only come in $5, $20, $50, or $100 mate", isPM)
			return nil
		}
		amount = parsed
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	balance, err := p.economyClient.GetBalance(ctx, channel, username)
	if err != nil {
		logger.Error(p.name, "Failed to get balance: %v", err)
		p.sendResponse(channel, username, "scratchie machine broke mate", isPM)
		return nil
	}
	if balance < int64(amount) {
		p.sendResponse(channel, username, fmt.Sprintf("ya need $%d for that scratchie mate, you've only got $%d", amount, balance), isPM)
		return nil
	}

	if _, err := p.economyClient.Debit(ctx, framework.DebitRequest{Channel: channel, Username: username, Amount: int64(amount), Reason: "scratchie_buy"}); err != nil {
		logger.Error(p.name, "Failed to debit scratchie cost: %v", err)
		p.sendResponse(channel, username, "scratchie machine broke mate", isPM)
		return nil
	}

	ticket := ticketForAmount(amount)
	startMessage := fmt.Sprintf("🎫 Bought a $%d \"%s\" scratchie...\n\n*scratch scratch scratch*", amount, ticket.Name)
	p.sendResponse(channel, username, startMessage, isPM)

	roll := rand.Float64()
	winnings, resultMessage, bigWin := resolveScratchResult(amount, ticket, roll)

	time.AfterFunc(2*time.Second, func() {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		if winnings > 0 {
			ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
			defer cancel()
			_, err := p.economyClient.Credit(ctx, framework.CreditRequest{Channel: channel, Username: username, Amount: winnings, Reason: "scratchie_win"})
			if err != nil {
				logger.Error(p.name, "Failed to credit scratchie winnings: %v", err)
				p.sendResponse(channel, username, "scratchie printer ate ya winnings. try again later", isPM)
				return
			}
		}

		result := fmt.Sprintf("🎰 RESULT: %s", resultMessage)
		p.sendResponse(channel, username, result, isPM)

		if bigWin {
			announcement := ""
			if winnings >= int64(amount*50) {
				announcement = fmt.Sprintf("🎰💰 HOLY FUCK! %s JUST HIT THE JACKPOT! $%d ON A $%d SCRATCHIE! 💰🎰", username, winnings, amount)
			} else {
				announcement = fmt.Sprintf("🎉 BIG WIN! %s just won $%d on a $%d scratchie!", username, winnings, amount)
			}
			p.sendResponse(channel, username, announcement, false)
		}
	})

	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool) {
	key := strings.ToLower(channel) + ":" + strings.ToLower(username)
	if p.cooldown <= 0 {
		return 0, true
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if last, ok := p.lastUse[key]; ok {
		until := last.Add(p.cooldown)
		if time.Now().Before(until) {
			return until.Sub(time.Now()), false
		}
	}

	p.lastUse[key] = time.Now()
	return 0, true
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

type scratchTicket struct {
	Name        string
	Lose        float64
	Small       float64
	Medium      float64
	Big         float64
	Jackpot     float64
	SmallMult   float64
	MediumMult  float64
	BigMult     float64
	JackpotMult float64
}

func ticketForAmount(amount int) scratchTicket {
	switch amount {
	case 20:
		return scratchTicket{
			Name:        "Golden Nugget",
			Lose:        0.82,
			Small:       0.12,
			Medium:      0.04,
			Big:         0.015,
			Jackpot:     0.005,
			SmallMult:   2,
			MediumMult:  5,
			BigMult:     20,
			JackpotMult: 100,
		}
	case 50:
		return scratchTicket{
			Name:        "Ute Fund Deluxe",
			Lose:        0.8,
			Small:       0.14,
			Medium:      0.04,
			Big:         0.015,
			Jackpot:     0.005,
			SmallMult:   1.5,
			MediumMult:  4,
			BigMult:     15,
			JackpotMult: 150,
		}
	case 100:
		return scratchTicket{
			Name:        "Centrelink's Revenge",
			Lose:        0.78,
			Small:       0.15,
			Medium:      0.05,
			Big:         0.015,
			Jackpot:     0.005,
			SmallMult:   1.5,
			MediumMult:  3,
			BigMult:     10,
			JackpotMult: 200,
		}
	default:
		return scratchTicket{
			Name:        "Lucky 7s",
			Lose:        0.85,
			Small:       0.1,
			Medium:      0.03,
			Big:         0.015,
			Jackpot:     0.005,
			SmallMult:   2,
			MediumMult:  5,
			BigMult:     10,
			JackpotMult: 50,
		}
	}
}

func resolveScratchResult(amount int, ticket scratchTicket, roll float64) (int64, string, bool) {
	loseMessages := []string{
		"fuck all, better luck next time",
		"nothing, bloody ripoff",
		"sweet fuck all mate",
		"donuts, money down the drain",
		"jack shit, typical",
	}

	if roll < ticket.Lose {
		return 0, loseMessages[rand.Intn(len(loseMessages))], false
	}

	roll -= ticket.Lose
	if roll < ticket.Small {
		winnings := int64(float64(amount) * ticket.SmallMult)
		return winnings, fmt.Sprintf("winner! matched 3 beers, won $%d!", winnings), false
	}
	roll -= ticket.Small
	if roll < ticket.Medium {
		winnings := int64(float64(amount) * ticket.MediumMult)
		return winnings, fmt.Sprintf("fuckin beauty! matched 3 bongs, won $%d!", winnings), winnings >= int64(amount*10)
	}
	roll -= ticket.Medium
	if roll < ticket.Big {
		winnings := int64(float64(amount) * ticket.BigMult)
		return winnings, fmt.Sprintf("HOLY SHIT! matched 3 VBs, won $%d!", winnings), true
	}

	winnings := int64(float64(amount) * ticket.JackpotMult)
	return winnings, fmt.Sprintf("JACKPOT CUNT! matched 3 GOLDEN DAZZAS! WON $%d!!!", winnings), true
}

func isValidTicketAmount(amount int) bool {
	return amount == 5 || amount == 20 || amount == 50 || amount == 100
}
