package couchcoins

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

const defaultCooldownHours = 12

func New() framework.Plugin {
	return &Plugin{
		name:     "couchcoins",
		cooldown: 12 * time.Hour,
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

	for _, cmd := range []string{"couch_coins", "couch", "couchsearch", "searchcouch"} {
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
					"commands":    "couch_coins,couch,couchsearch,searchcouch",
					"min_rank":    "0",
					"description": "search the couch for coins",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register couch coins command: %v", err)
		return fmt.Errorf("failed to register couch coins command: %w", err)
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
		p.sendResponse(channel, username, "couch ledger's down right now. try again later", isPM)
		return nil
	}
	if !ok {
		message := waitMessage(username, remaining)
		p.sendResponse(channel, username, message, isPM)
		return nil
	}

	publicMessages := []string{
		fmt.Sprintf("-%s is diggin through the couch cushions...", username),
		fmt.Sprintf("-%s is searchin for lost treasure in the couch", username),
		fmt.Sprintf("-%s has their arm deep in the couch crevices", username),
		fmt.Sprintf("-%s is excavating the sacred couch", username),
	}
	if !isPM {
		p.sendResponse(channel, username, publicMessages[rand.Intn(len(publicMessages))], false)
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	balance, err := p.economyClient.GetBalance(ctx, channel, username)
	if err != nil {
		logger.Error(p.name, "Failed to fetch balance: %v", err)
		p.sendResponse(channel, username, "couch is at the tip mate, try again later", isPM)
		return nil
	}

	isDesperate := balance < 20
	badEventChance := 0.08
	if isDesperate {
		badEventChance = 0.05
	}

	location := locations[rand.Intn(len(locations))]
	if couchBadEventRoll() < badEventChance {
		badEvent := badEvents[rand.Intn(len(badEvents))]
		cost := rand.Intn(badEvent.MaxCost-badEvent.MinCost+1) + badEvent.MinCost
		actualCost := int64(cost)
		debitApplied := false
		if balance < actualCost {
			actualCost = balance
		}

		if actualCost > 0 {
			_, err := p.economyClient.Debit(ctx, framework.DebitRequest{
				Channel:  channel,
				Username: username,
				Amount:   actualCost,
				Reason:   "couch_coins_bad_event",
			})
			if err != nil {
				logger.Error(p.name, "Failed to debit bad event cost: %v", err)
				actualCost = 0
			} else {
				debitApplied = true
			}
		}

		if err := p.updateStats(channel, username, couchStatsUpdate{
			Dives:         1,
			Injuries:      1,
			HospitalTrips: 1,
			LastPlayed:    time.Now(),
		}); err != nil {
			logger.Error(p.name, "Failed to update couch stats: %v", err)
		}

		message := fmt.Sprintf("❌ FUCKIN DISASTER! %s", strings.ReplaceAll(badEvent.Message, "-amount", fmt.Sprintf("$%d", actualCost)))
		if !debitApplied {
			message += " Lucky you're broke or that woulda cost ya!"
		}
		p.sendResponse(channel, username, message, isPM)
		return nil
	}

	amount := rollFindAmountFunc(isDesperate)
	messageType := messageTypeForAmount(amount)
	message := ""
	publicAnnouncement := false

	if amount == 0 {
		message = fmt.Sprintf("❌ EMPTY HANDED! Searched %s... %s", location, randomChoice(successMessagesNothing))
		if err := p.updateStats(channel, username, couchStatsUpdate{
			Dives:      1,
			LastPlayed: time.Now(),
		}); err != nil {
			logger.Error(p.name, "Failed to update couch stats: %v", err)
		}
	} else {
		_, err := p.economyClient.Credit(ctx, framework.CreditRequest{
			Channel:  channel,
			Username: username,
			Amount:   int64(amount),
			Reason:   "couch_coins",
		})
		if err != nil {
			logger.Error(p.name, "Failed to credit couch coins: %v", err)
			p.sendResponse(channel, username, "couch search system fucked itself. typical.", isPM)
			return nil
		}

		message = fmt.Sprintf("%s Searched %s... %s", messageHeaderForType(messageType), location, strings.ReplaceAll(randomChoice(successMessagesByType[messageType]), "-amount", fmt.Sprintf("$%d", amount)))
		if isDesperate && amount >= 10 {
			message += fmt.Sprintf(" %s!", randomChoice(desperationMessages))
		}
		if amount >= 31 {
			publicAnnouncement = true
		}

		update := couchStatsUpdate{
			Dives:           1,
			SuccessfulDives: 1,
			TotalFound:      int64(amount),
			BestFind:        int64(amount),
			LastPlayed:      time.Now(),
		}
		if err := p.updateStats(channel, username, update); err != nil {
			logger.Error(p.name, "Failed to update couch stats: %v", err)
		}
	}

	if isPM {
		p.sendResponse(channel, username, message, true)
	} else {
		p.sendResponse(channel, username, message, false)
		if publicAnnouncement {
			announcement := fmt.Sprintf("🚨 COUCH JACKPOT! %s just found $%d %s! Lucky bastard!", username, amount, location)
			couchAfterFunc(couchAnnouncementDelay, func() {
				select {
				case <-p.ctx.Done():
					return
				default:
					p.sendResponse(channel, username, announcement, false)
				}
			})
		}
	}

	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT last_played_at
		FROM daz_couch_stats
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

type couchStatsUpdate struct {
	Dives           int
	SuccessfulDives int
	TotalFound      int64
	BestFind        int64
	Injuries        int
	HospitalTrips   int
	LastPlayed      time.Time
}

func (p *Plugin) updateStats(channel, username string, update couchStatsUpdate) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_couch_stats (
			channel, username, total_dives, successful_dives, total_found, best_find,
			injuries, hospital_trips, last_played_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW()
		)
		ON CONFLICT (channel, username) DO UPDATE SET
			total_dives = daz_couch_stats.total_dives + EXCLUDED.total_dives,
			successful_dives = daz_couch_stats.successful_dives + EXCLUDED.successful_dives,
			total_found = daz_couch_stats.total_found + EXCLUDED.total_found,
			best_find = GREATEST(daz_couch_stats.best_find, EXCLUDED.best_find),
			injuries = daz_couch_stats.injuries + EXCLUDED.injuries,
			hospital_trips = daz_couch_stats.hospital_trips + EXCLUDED.hospital_trips,
			last_played_at = EXCLUDED.last_played_at,
			updated_at = NOW()
	`

	_, err := p.sqlClient.ExecContext(ctx, query,
		channel,
		username,
		update.Dives,
		update.SuccessfulDives,
		update.TotalFound,
		update.BestFind,
		update.Injuries,
		update.HospitalTrips,
		update.LastPlayed,
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

func waitMessage(username string, remaining time.Duration) string {
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	responses := []string{
		fmt.Sprintf("oi -%s, ya already ransacked the couch. wait %dh %dm for it to refill", username, hours, minutes),
		fmt.Sprintf("-%s the couch needs time to accumulate more coins. %dh %dm left", username, hours, minutes),
		fmt.Sprintf("patience -%s, can't search a barren couch. %dh %dm to go", username, hours, minutes),
		fmt.Sprintf("-%s give it a rest mate, %dh %dm til the next search", username, hours, minutes),
		fmt.Sprintf("fuck off -%s, couch is still recoverin. %dh %dm remaining", username, hours, minutes),
	}
	return responses[rand.Intn(len(responses))]
}

func rollFindAmount(isDesperate bool) int {
	roll := rand.Float64()
	if isDesperate {
		switch {
		case roll < 0.15:
			return 0
		case roll < 0.25:
			return rand.Intn(20) + 31
		case roll < 0.50:
			return rand.Intn(15) + 16
		case roll < 0.80:
			return rand.Intn(10) + 6
		default:
			return rand.Intn(5) + 1
		}
	}

	switch {
	case roll < 0.25:
		return 0
	case roll < 0.28:
		return rand.Intn(20) + 31
	case roll < 0.38:
		return rand.Intn(15) + 16
	case roll < 0.63:
		return rand.Intn(10) + 6
	default:
		return rand.Intn(5) + 1
	}
}

var rollFindAmountFunc = rollFindAmount

var couchBadEventRoll = func() float64 {
	return rand.Float64()
}

var couchAnnouncementDelay = 2 * time.Second

var couchAfterFunc = time.AfterFunc

func messageTypeForAmount(amount int) string {
	switch {
	case amount == 0:
		return "nothing"
	case amount <= 5:
		return "small"
	case amount <= 15:
		return "medium"
	case amount <= 30:
		return "good"
	default:
		return "amazing"
	}
}

func messageHeaderForType(messageType string) string {
	switch messageType {
	case "small":
		return "✅ FOUND SOME SHRAPNEL!"
	case "medium":
		return "✅ DECENT FIND!"
	case "good":
		return "💰 RIPPER FIND!"
	case "amazing":
		return "🎰 COUCH JACKPOT!"
	default:
		return "❌ EMPTY HANDED!"
	}
}

func randomChoice(options []string) string {
	return options[rand.Intn(len(options))]
}

var locations = []string{
	"between the crusty couch cushions",
	"under the stained armrest",
	"in the crack where shazza dropped her durries",
	"behind the cushion covered in bong water stains",
	"in the springs poking through the fabric",
	"where the dog pissed last week",
	"under the pizza box from 2019",
	"in the mysterious sticky patch",
	"between the cum-stained cushions",
	"where the VB tinnies fell down",
	"in the ashtray overflow zone",
	"under the moldy TV guide",
	"in the graveyard of lost remotes",
	"where the cat had kittens",
	"in the depths of hell (back corner)",
	"under me old undies",
	"in the sacred bong storage spot",
	"where the cockroaches nest",
}

var successMessagesNothing = []string{
	"searched every fuckin crevice but found sweet fuck all",
	"nothin but dust bunnies and old roaches mate",
	"just found shazza's old tampon... fuckin disgusting",
	"empty handed, just like me soul",
	"found a used condom but no coins. lovely.",
	"nothin but cigarette butts and regret",
}

var successMessagesByType = map[string][]string{
	"small": {
		"scored -amount in shrapnel",
		"found -amount in dirty coins",
		"-amount in sticky change, better than nothin",
		"couple coins worth -amount, enough for half a dart",
		"-amount in coppers, fuckin peasant money",
	},
	"medium": {
		"decent haul! -amount in mixed coins",
		"fuckin oath, -amount just sitting there",
		"-amount! that's a pack of mi goreng sorted",
		"beauty! -amount in forgotten change",
		"-amount score, nearly enough for a pouch",
	},
	"good": {
		"jackpot! -amount in the couch!",
		"fuck me dead, -amount just chillin there",
		"RIPPER! -amount in lost coins!",
		"-amount! must be me birthday",
		"holy shit, -amount in couch treasure!",
	},
	"amazing": {
		"FUCKIN BONANZA! -amount IN THE COUCH!!!",
		"SOMEONE CALL THE COPS, I FOUND -amount!!!",
		"HOLY MOTHER OF FUCK, -amount!!!",
		"-amount!!! THE COUCH GODS HAVE BLESSED ME!!!",
		"JACKPOT CUNT!!! -amount IN PURE COUCH GOLD!!!",
	},
}

var badEvents = []struct {
	Message string
	MinCost int
	MaxCost int
}{
	{Message: "FUCK! redback spider bit ya hand! hospital trip cost ya -amount", MinCost: 10, MaxCost: 25},
	{Message: "shazza caught ya rifling through HER side of the couch. -amount fine for touchin her shit", MinCost: 5, MaxCost: 20},
	{Message: "found a syringe and stabbed yaself. tetanus shot cost -amount", MinCost: 15, MaxCost: 25},
	{Message: "couch collapsed while searchin! had to pay -amount for a new one from vinnies", MinCost: 10, MaxCost: 20},
	{Message: "disturbed a possum nest, little cunt bit ya. rabies shot: -amount", MinCost: 10, MaxCost: 25},
	{Message: "found shazza's secret stash and she went mental. -amount in hush money", MinCost: 5, MaxCost: 15},
	{Message: "allergic reaction to the mold. antihistamines cost ya -amount", MinCost: 5, MaxCost: 15},
	{Message: "cops saw ya through the window and thought ya were burglin. -amount fine", MinCost: 15, MaxCost: 25},
}

var desperationMessages = []string{
	"ya desperate state helped ya search harder",
	"poverty gives ya eagle eyes for coins",
	"desperation is a powerful motivator",
	"bein broke made ya check every millimeter",
	"nothin motivates like an empty wallet",
}
