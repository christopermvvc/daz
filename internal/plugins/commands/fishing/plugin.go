package fishing

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

const defaultCooldownHours = 2

func New() framework.Plugin {
	return &Plugin{
		name:     "fishing",
		cooldown: 2 * time.Hour,
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

	for _, cmd := range []string{"fish", "fishing", "cast"} {
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
					"commands":    "fish,fishing,cast",
					"min_rank":    "0",
					"description": "go fishing for some cash",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register fishing command: %v", err)
		return fmt.Errorf("failed to register fishing command: %w", err)
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
	if username == "" || channel == "" {
		return nil
	}

	remaining, ok, err := p.checkCooldown(channel, username)
	if err != nil {
		logger.Error(p.name, "Cooldown check failed: %v", err)
		p.sendResponse(channel, username, "the fishing spot's closed right now, try again later", true)
		return nil
	}
	if !ok {
		p.sendResponse(channel, username, waitMessage(username, remaining), true)
		return nil
	}

	bait, valid := baitFromArgs(req.Data.Command.Args)
	if !valid {
		p.sendResponse(channel, username, baitHelpMessage(), true)
		return nil
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	if bait.Cost > 0 {
		balance, err := p.economyClient.GetBalance(ctx, channel, username)
		if err != nil {
			logger.Error(p.name, "Failed to get balance: %v", err)
			p.sendResponse(channel, username, "fishing shack can't check your balance, try again later", true)
			return nil
		}
		if balance < bait.Cost {
			p.sendResponse(channel, username, fmt.Sprintf("you need $%d for %s bait, you've only got $%d", bait.Cost, bait.Name, balance), true)
			return nil
		}
		if _, err := p.economyClient.Debit(ctx, framework.DebitRequest{
			Channel:  channel,
			Username: username,
			Amount:   bait.Cost,
			Reason:   "fishing_bait",
		}); err != nil {
			logger.Error(p.name, "Failed to debit bait cost: %v", err)
			p.sendResponse(channel, username, "the bait shop's jammed, try again later", true)
			return nil
		}
	}

	startMessage := fmt.Sprintf("🎣 You cast a line with %s %s...", bait.Emoji, bait.Name)
	if bait.Cost > 0 {
		startMessage = fmt.Sprintf("🎣 You cast a line with %s %s ($%d)...", bait.Emoji, bait.Name, bait.Cost)
	}
	p.sendResponse(channel, username, startMessage, true)

	result := rollFishingResultFunc(bait, username)
	if result.payout > 0 {
		ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
		defer cancel()
		_, err := p.economyClient.Credit(ctx, framework.CreditRequest{Channel: channel, Username: username, Amount: result.payout, Reason: "fishing"})
		if err != nil {
			logger.Error(p.name, "Failed to credit fishing payout: %v", err)
			p.sendResponse(channel, username, "the fish co-op lost your payout. try again later", true)
			return nil
		}
	}

	update := fishingUpdate{
		Casts:          1,
		Catches:        boolToInt(result.caught),
		Earnings:       result.payout,
		BestCatch:      result.bestCatch,
		RareCatches:    boolToInt(result.rare),
		LegendaryCatch: boolToInt(result.legendary),
		Misses:         boolToInt(!result.caught),
		LastPlayed:     time.Now(),
	}
	if err := p.updateStats(channel, username, update); err != nil {
		logger.Error(p.name, "Failed to update fishing stats: %v", err)
	}

	p.sendResponse(channel, username, result.message, true)
	if result.bigWin {
		announcement := strings.TrimSpace(result.publicAnnouncement)
		if announcement == "" {
			announcement = fmt.Sprintf("🎣 BIG CATCH! %s just landed a $%d haul!", username, result.payout)
		}
		p.sendResponse(channel, username, announcement, false)
	}

	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	query := `
		SELECT last_played_at
		FROM daz_fishing_stats
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

type fishingUpdate struct {
	Casts          int
	Catches        int
	Earnings       int64
	BestCatch      int64
	RareCatches    int
	LegendaryCatch int
	Misses         int
	LastPlayed     time.Time
}

func (p *Plugin) updateStats(channel, username string, update fishingUpdate) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	query := `
		INSERT INTO daz_fishing_stats (
			channel, username, total_casts, total_catches, total_earnings, best_catch,
			rare_catches, legendary_catches, misses, last_played_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (channel, username) DO UPDATE SET
			total_casts = daz_fishing_stats.total_casts + EXCLUDED.total_casts,
			total_catches = daz_fishing_stats.total_catches + EXCLUDED.total_catches,
			total_earnings = daz_fishing_stats.total_earnings + EXCLUDED.total_earnings,
			best_catch = GREATEST(daz_fishing_stats.best_catch, EXCLUDED.best_catch),
			rare_catches = daz_fishing_stats.rare_catches + EXCLUDED.rare_catches,
			legendary_catches = daz_fishing_stats.legendary_catches + EXCLUDED.legendary_catches,
			misses = daz_fishing_stats.misses + EXCLUDED.misses,
			last_played_at = EXCLUDED.last_played_at,
			updated_at = NOW()
	`
	_, err := p.sqlClient.ExecContext(ctx, query,
		channel,
		username,
		update.Casts,
		update.Catches,
		update.Earnings,
		update.BestCatch,
		update.RareCatches,
		update.LegendaryCatch,
		update.Misses,
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

type baitInfo struct {
	Key      string
	Name     string
	Cost     int64
	Modifier float64
	Emoji    string
}

var baitOptions = []baitInfo{
	{Key: "worm", Name: "worm", Cost: 0, Modifier: 0.00, Emoji: "🪱"},
	{Key: "ciggie", Name: "ciggie", Cost: 0, Modifier: 0.02, Emoji: "🚬"},
	{Key: "servo_pie", Name: "servo pie", Cost: 5, Modifier: 0.05, Emoji: "🥧"},
	{Key: "lure", Name: "shiny lure", Cost: 15, Modifier: 0.10, Emoji: "🪝"},
	{Key: "prawn", Name: "prawn", Cost: 25, Modifier: 0.15, Emoji: "🦐"},
	{Key: "squid", Name: "squid", Cost: 40, Modifier: 0.20, Emoji: "🦑"},
}

var baitAliases = map[string]string{
	"worm":     "worm",
	"worms":    "worm",
	"ciggie":   "ciggie",
	"cig":      "ciggie",
	"smoke":    "ciggie",
	"servopie": "servo_pie",
	"pie":      "servo_pie",
	"lure":     "lure",
	"prawn":    "prawn",
	"shrimp":   "prawn",
	"squid":    "squid",
}

func baitFromArgs(args []string) (baitInfo, bool) {
	if len(args) == 0 {
		return baitOptions[0], true
	}
	key := normalizeBait(args[0])
	alias, ok := baitAliases[key]
	if !ok {
		return baitInfo{}, false
	}
	for _, bait := range baitOptions {
		if bait.Key == alias {
			return bait, true
		}
	}
	return baitInfo{}, false
}

func baitHelpMessage() string {
	lines := []string{
		"fishing baits: worm (free), ciggie (free), servo_pie ($5), lure ($15), prawn ($25), squid ($40)",
		"usage: !fish [bait]",
	}
	return strings.Join(lines, "\n")
}

func normalizeBait(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	return value
}

type fishingResult struct {
	message            string
	publicAnnouncement string
	payout             int64
	bestCatch          int64
	bigWin             bool
	caught             bool
	rare               bool
	legendary          bool
	fishName           string
	fishWeight         float64
	fishEmoji          string
}

type fishInfo struct {
	Name       string
	Emoji      string
	MinValue   int
	MaxValue   int
	MinWeight  float64
	MaxWeight  float64
	Tier       string
	Descriptor string
}

type treasureInfo struct {
	Name    string
	MinVal  int
	MaxVal  int
	Comment string
}

var commonFish = []fishInfo{
	{Name: "bream", Emoji: "🐟", MinValue: 6, MaxValue: 18, MinWeight: 0.4, MaxWeight: 1.2, Tier: "common"},
	{Name: "whiting", Emoji: "🐟", MinValue: 5, MaxValue: 16, MinWeight: 0.3, MaxWeight: 1.0, Tier: "common"},
	{Name: "flathead", Emoji: "🐟", MinValue: 8, MaxValue: 20, MinWeight: 0.6, MaxWeight: 1.8, Tier: "common"},
	{Name: "mullet", Emoji: "🐟", MinValue: 4, MaxValue: 14, MinWeight: 0.3, MaxWeight: 1.1, Tier: "common"},
}

var uncommonFish = []fishInfo{
	{Name: "snapper", Emoji: "🐠", MinValue: 18, MaxValue: 40, MinWeight: 1.2, MaxWeight: 3.0, Tier: "uncommon"},
	{Name: "salmon", Emoji: "🐠", MinValue: 20, MaxValue: 45, MinWeight: 1.4, MaxWeight: 3.4, Tier: "uncommon"},
	{Name: "trevally", Emoji: "🐠", MinValue: 16, MaxValue: 38, MinWeight: 1.0, MaxWeight: 2.8, Tier: "uncommon"},
}

var rareFish = []fishInfo{
	{Name: "kingfish", Emoji: "🐡", MinValue: 45, MaxValue: 90, MinWeight: 3.0, MaxWeight: 6.0, Tier: "rare"},
	{Name: "tuna", Emoji: "🐡", MinValue: 50, MaxValue: 110, MinWeight: 3.5, MaxWeight: 6.5, Tier: "rare"},
	{Name: "barra", Emoji: "🐡", MinValue: 40, MaxValue: 85, MinWeight: 2.8, MaxWeight: 5.5, Tier: "rare"},
}

var legendaryFish = []fishInfo{
	{Name: "marlin", Emoji: "🦈", MinValue: 120, MaxValue: 220, MinWeight: 20.0, MaxWeight: 60.0, Tier: "legendary", Descriptor: "legendary"},
	{Name: "giant tuna", Emoji: "🦈", MinValue: 110, MaxValue: 200, MinWeight: 18.0, MaxWeight: 50.0, Tier: "legendary", Descriptor: "legendary"},
	{Name: "saw shark", Emoji: "🦈", MinValue: 130, MaxValue: 240, MinWeight: 22.0, MaxWeight: 70.0, Tier: "legendary", Descriptor: "legendary"},
}

var treasureFinds = []treasureInfo{
	{Name: "rusty safe", MinVal: 220, MaxVal: 420, Comment: "full of soggy notes"},
	{Name: "gold chain", MinVal: 240, MaxVal: 480, Comment: "still shines like new"},
	{Name: "lost esky", MinVal: 260, MaxVal: 520, Comment: "packed with cash and cold tins"},
	{Name: "sunken duffel", MinVal: 300, MaxVal: 650, Comment: "absolutely loaded"},
}

func rollFishingResult(bait baitInfo, username string) fishingResult {
	missChance := 0.22 - (bait.Modifier * 0.5)
	if missChance < 0.05 {
		missChance = 0.05
	}
	if rand.Float64() < missChance {
		misses := []string{
			"❌ Nothing bites... just weeds and heartbreak.",
			"❌ Line goes dead. Not even a nibble.",
			"❌ You snag a boot and chuck it back in disgust.",
			"❌ A crab steals your bait. Rude.",
		}
		return fishingResult{message: misses[rand.Intn(len(misses))], payout: 0, caught: false}
	}

	if rand.Float64() < 0.002 {
		treasure := treasureFinds[rand.Intn(len(treasureFinds))]
		value := rand.Intn(treasure.MaxVal-treasure.MinVal+1) + treasure.MinVal
		message := fmt.Sprintf("💎 HOLY FUCK! You dragged up a %s worth $%d! %s", treasure.Name, value, treasure.Comment)
		announcement := fmt.Sprintf("🚨 %s just fished up a %s worth $%d!", username, treasure.Name, value)
		return fishingResult{
			message:            message,
			publicAnnouncement: announcement,
			payout:             int64(value),
			bestCatch:          int64(value),
			bigWin:             true,
			caught:             true,
			legendary:          true,
		}
	}

	roll := rand.Float64() - bait.Modifier
	if roll < 0.60 {
		return buildFishResult(commonFish[rand.Intn(len(commonFish))])
	}
	if roll < 0.85 {
		return buildFishResult(uncommonFish[rand.Intn(len(uncommonFish))])
	}
	if roll < 0.95 {
		result := buildFishResult(rareFish[rand.Intn(len(rareFish))])
		result.rare = true
		result.bigWin = result.payout >= 70
		if result.bigWin {
			result.publicAnnouncement = fmt.Sprintf("🎣💥 Rare catch! %s just hauled in a %.1fkg %s worth $%d", username, result.fishWeight, result.fishName, result.payout)
		}
		return result
	}
	result := buildFishResult(legendaryFish[rand.Intn(len(legendaryFish))])
	result.legendary = true
	result.bigWin = true
	result.publicAnnouncement = fmt.Sprintf("🎣🔥 Legendary catch! %s just landed a %.1fkg %s worth $%d", username, result.fishWeight, result.fishName, result.payout)
	return result
}

func buildFishResult(fish fishInfo) fishingResult {
	weight := rand.Float64()*(fish.MaxWeight-fish.MinWeight) + fish.MinWeight
	value := rand.Intn(fish.MaxValue-fish.MinValue+1) + fish.MinValue
	message := fmt.Sprintf("%s You caught a %.1fkg %s worth $%d!", fish.Emoji, weight, fish.Name, value)
	return fishingResult{
		message:    message,
		payout:     int64(value),
		bestCatch:  int64(value),
		caught:     true,
		fishName:   fish.Name,
		fishWeight: weight,
		fishEmoji:  fish.Emoji,
	}
}

func waitMessage(username string, remaining time.Duration) string {
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	responses := []string{
		fmt.Sprintf("-%s the fish are spooked. wait %dh %dm", username, hours, minutes),
		fmt.Sprintf("-%s mate, you just fished. %dh %dm left", username, hours, minutes),
		fmt.Sprintf("give it a rest -%s, next cast in %dh %dm", username, hours, minutes),
		fmt.Sprintf("-%s the tide ain't ready. %dh %dm to go", username, hours, minutes),
	}
	return responses[rand.Intn(len(responses))]
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var rollFishingResultFunc = rollFishingResult
