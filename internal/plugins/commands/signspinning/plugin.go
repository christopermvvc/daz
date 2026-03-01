package signspinning

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

const (
	defaultCooldownHours = 36
)

func New() framework.Plugin {
	return &Plugin{
		name:     "signspinning",
		cooldown: 36 * time.Hour,
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

	rand.Seed(time.Now().UnixNano())

	for _, cmd := range []string{"sign_spinning", "sign", "spin", "signspinning", "signspin"} {
		eventName := fmt.Sprintf("command.%s.execute", cmd)
		if err := p.eventBus.Subscribe(eventName, p.handleCommand); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", eventName, err)
		}
	}

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
					"commands":    "sign_spinning,sign,spin,signspinning,signspin",
					"min_rank":    "0",
					"description": "spin signs for some cash",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register sign spinning command: %v", err)
		return fmt.Errorf("failed to register sign spinning command: %w", err)
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

	if remaining, ok := p.checkCooldown(channel, username); !ok {
		p.sendChannelMessage(channel, waitMessage(username, remaining))
		return nil
	}

	publicMsgs := []string{
		fmt.Sprintf("-%s is grabbing a sign and heading to the corner...", username),
		fmt.Sprintf("-%s is off to spin signs like a sick cunt", username),
		fmt.Sprintf("-%s reckons they can make motorists look at ads", username),
		fmt.Sprintf("-%s is about to show these signs who's boss", username),
	}
	if len(publicMsgs) > 0 {
		p.sendChannelMessage(channel, publicMsgs[rand.Intn(len(publicMsgs))])
	}

	result := rollShift()

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	metadata := map[string]any{
		"sign_type":   result.signType,
		"sign_tier":   result.signTier,
		"weather":     result.weather.Condition,
		"event":       result.eventDesc,
		"injury":      result.injured,
		"injury_desc": result.injuryDesc,
		"injury_cost": result.injuryCost,
	}
	metaJSON, _ := json.Marshal(metadata)
	_, err := p.economyClient.Credit(ctx, framework.CreditRequest{
		Channel:  channel,
		Username: username,
		Amount:   int64(result.finalPay),
		Reason:   "sign_spinning",
		Metadata: metaJSON,
	})
	if err != nil {
		logger.Error(p.name, "Failed to credit sign spinning earnings: %v", err)
		p.sendChannelMessage(channel, "sign spinning agency system crashed. try again later mate.")
		return nil
	}

	if err := p.updateStats(channel, username, result); err != nil {
		logger.Error(p.name, "Failed to update sign spinning stats: %v", err)
	}

	p.sendChannelMessage(channel, result.summaryMessage())

	if result.injured || result.finalPay >= 60 {
		message := result.announcement(username)
		time.AfterFunc(2*time.Second, func() {
			select {
			case <-p.ctx.Done():
				return
			default:
				p.sendChannelMessage(channel, message)
			}
		})
	}

	return nil
}

func (p *Plugin) checkCooldown(channel, username string) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT last_played_at
		FROM daz_sign_spinning_stats
		WHERE channel = $1 AND username = $2
		LIMIT 1
	`

	rows, err := p.sqlClient.QueryContext(ctx, query, channel, username)
	if err != nil {
		return 0, true
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, true
	}
	var lastPlayed time.Time
	if err := rows.Scan(&lastPlayed); err != nil {
		return 0, true
	}
	if lastPlayed.IsZero() {
		return 0, true
	}

	until := lastPlayed.Add(p.cooldown)
	if time.Now().Before(until) {
		return until.Sub(time.Now()), false
	}
	return 0, true
}

func (p *Plugin) updateStats(channel, username string, result signResult) error {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO daz_sign_spinning_stats (
			channel, username, total_spins, total_earnings, cars_hit, cops_called,
			best_shift, worst_shift, perfect_days, times_worked, injuries, best_day,
			last_played_at, updated_at
		) VALUES (
			$1, $2, 1, $3, $4, $5, $6, $7, $8, 1, $9, $10, NOW(), NOW()
		)
		ON CONFLICT (channel, username) DO UPDATE SET
			total_spins = daz_sign_spinning_stats.total_spins + 1,
			total_earnings = daz_sign_spinning_stats.total_earnings + EXCLUDED.total_earnings,
			cars_hit = daz_sign_spinning_stats.cars_hit + EXCLUDED.cars_hit,
			cops_called = daz_sign_spinning_stats.cops_called + EXCLUDED.cops_called,
			best_shift = GREATEST(daz_sign_spinning_stats.best_shift, EXCLUDED.best_shift),
			worst_shift = CASE
				WHEN daz_sign_spinning_stats.worst_shift = 0 THEN EXCLUDED.worst_shift
				ELSE LEAST(daz_sign_spinning_stats.worst_shift, EXCLUDED.worst_shift)
			END,
			perfect_days = daz_sign_spinning_stats.perfect_days + EXCLUDED.perfect_days,
			times_worked = daz_sign_spinning_stats.times_worked + 1,
			injuries = daz_sign_spinning_stats.injuries + EXCLUDED.injuries,
			best_day = GREATEST(daz_sign_spinning_stats.best_day, EXCLUDED.best_day),
			last_played_at = NOW(),
			updated_at = NOW()
	`

	_, err := p.sqlClient.ExecContext(ctx, query,
		channel,
		username,
		result.finalPay,
		boolToInt(result.carsHit),
		boolToInt(result.copsCalled),
		result.finalPay,
		result.finalPay,
		boolToInt(result.finalPay >= 60),
		boolToInt(result.injured),
		result.finalPay,
	)
	return err
}

func (p *Plugin) sendChannelMessage(channel, message string) {
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(message) == "" {
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

func waitMessage(username string, remaining time.Duration) string {
	hours := int(remaining.Hours())
	minutes := int(remaining.Minutes()) % 60
	responses := []string{
		fmt.Sprintf("oi -%s, ya arms are still fucked from last shift! %dh %dm til recovery", username, hours, minutes),
		fmt.Sprintf("-%s mate, can't spin signs every day or you'll do ya shoulder. Wait %dh %dm", username, hours, minutes),
		fmt.Sprintf("ease up -%s, the corner's taken by another spinner. Try again in %dh %dm", username, hours, minutes),
		fmt.Sprintf("bloody hell -%s, even sign spinning needs rest days! %dh %dm to go", username, hours, minutes),
		fmt.Sprintf("-%s the agency said no more shifts for %dh %dm, too many spinners already", username, hours, minutes),
	}
	return responses[rand.Intn(len(responses))]
}

type signType struct {
	Description string
	Tier        string
}

type weatherCondition struct {
	Condition string
	Modifier  float64
}

type signResult struct {
	signType   string
	signTier   string
	weather    weatherCondition
	finalPay   int
	injured    bool
	injuryCost int
	injuryDesc string
	eventDesc  string
	carsHit    bool
	copsCalled bool
}

func (r signResult) summaryMessage() string {
	label := "✅ SHIFT COMPLETE!"
	if r.injured {
		label = "⚠️ INJURED ON THE JOB!"
	} else if r.finalPay >= 60 {
		label = "🎯 PERFECT SHIFT!"
	} else if r.finalPay >= 40 {
		label = "💰 SOLID SPINNING SESSION!"
	}

	lines := []string{
		label,
		fmt.Sprintf("🪧 You spun a sign for a %s", r.signType),
		fmt.Sprintf("🌤️ Weather: %s", r.weather.Condition),
	}
	if r.injured {
		lines = append(lines, fmt.Sprintf("🤕 Injury: %s", r.injuryDesc))
		lines = append(lines, fmt.Sprintf("🏥 Medical costs: $%d", r.injuryCost))
	} else if r.eventDesc != "" {
		lines = append(lines, fmt.Sprintf("📰 Event: %s", r.eventDesc))
	}
	lines = append(lines, fmt.Sprintf("💵 Earned: $%d", r.finalPay))

	suffix := "Not bad for standing around like a dickhead all day!"
	if r.injured {
		suffix = "Fuckin' workers comp better cover this shit!"
	} else if r.finalPay >= 60 {
		suffix = "Absolutely smashed it! Boss might hire ya full time!"
	} else if r.finalPay >= 40 {
		suffix = "Decent day's work! The motorists couldn't ignore ya!"
	} else if r.finalPay < 20 {
		suffix = "Shit pay but it's better than nothing. Maybe try a busier corner next time."
	}

	lines = append(lines, "", suffix)
	return strings.Join(lines, "\n")
}

func (r signResult) announcement(username string) string {
	if r.injured {
		return fmt.Sprintf("🚑 OH SHIT! %s got injured sign spinning! %s", username, r.injuryDesc)
	}
	return fmt.Sprintf("🪧 LEGENDARY SPINNER! %s made $%d throwing signs around like a mad cunt!", username, r.finalPay)
}

func rollShift() signResult {
	sign := rollSignType()
	weather := rollWeather()
	basePay := rollBasePay(sign.Tier)
	finalPay := int(float64(basePay) * weather.Modifier)

	injured := rand.Float64() < 0.10
	injuryDesc := ""
	injuryCost := 0
	if injured {
		injuryDesc = randomChoice(injuryEvents)
		injuryCost = rand.Intn(31)
		finalPay = maxInt(0, finalPay-injuryCost)
	}

	eventDesc := ""
	if !injured && rand.Float64() < 0.40 {
		if rand.Float64() < 0.6 {
			eventDesc = randomChoice(positiveEvents)
		} else {
			eventDesc = randomChoice(negativeEvents)
		}
		if strings.Contains(eventDesc, "fiver") {
			finalPay += 5
		} else if strings.Contains(eventDesc, "twenty") {
			finalPay += 20
		}
	}

	return signResult{
		signType:   sign.Description,
		signTier:   sign.Tier,
		weather:    weather,
		finalPay:   finalPay,
		injured:    injured,
		injuryCost: injuryCost,
		injuryDesc: injuryDesc,
		eventDesc:  eventDesc,
		carsHit:    strings.Contains(injuryDesc, "hit") && strings.Contains(injuryDesc, "car"),
		copsCalled: strings.Contains(eventDesc, "cops"),
	}
}

func rollSignType() signType {
	if rand.Float64() < 0.8 {
		return signType{Description: randomChoice(commonSigns), Tier: "common"}
	}
	return signType{Description: randomChoice(uncommonSigns), Tier: "uncommon"}
}

func rollWeather() weatherCondition {
	return weatherConditions[rand.Intn(len(weatherConditions))]
}

func rollBasePay(tier string) int {
	if tier == "uncommon" {
		return rand.Intn(21) + 40
	}
	return rand.Intn(26) + 15
}

func randomChoice(options []string) string {
	return options[rand.Intn(len(options))]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var commonSigns = []string{
	"pizza joint advertising $5 lunch specials",
	"tax return mob promising massive refunds",
	"mattress store having another closing down sale",
	"cash for gold place offering 'best prices'",
	"phone repair shop with dodgy screens",
	"discount chemist flogging vitamins",
	"gym having a 'no joining fee' special",
	"car wash trying to get customers",
}

var uncommonSigns = []string{
	"head shop advertising 'tobacco' pipes",
	"massage parlour with 'happy endings'",
	"liquidation sale for a failed business",
	"adult store having a discreet sale",
	"vape shop pushing the new flavours",
	"dodgy loan shark advertising quick cash",
	"underground fight club recruitment",
	"psychic reader offering life advice",
}

var positiveEvents = []string{
	"bunch of tradies honked and gave ya thumbs up",
	"some legend chucked ya a fiver from their car",
	"boss reckons you're a natural, might get more shifts",
	"hot sheila winked at ya from the traffic lights",
	"got featured on someone's tiktok, going viral",
	"found a twenty on the ground while spinning",
	"local news filmed ya for a segment on hard workers",
}

var negativeEvents = []string{
	"dropped the sign and it nearly hit a car",
	"cops hassled ya about permits for 20 minutes",
	"some dickhead threw a maccas cup at ya",
	"sign broke in half from spinning too hard",
	"nearly got clipped by a ute doing a burnout",
	"started raining and the sign got soggy",
	"karen complained you're distracting drivers",
}

var injuryEvents = []string{
	"threw out ya shoulder doing sick spins",
	"sign smacked ya in the face, bloody nose",
	"tripped over the gutter, skinned ya knee",
	"pulled a hammy trying to dance with the sign",
	"sign caught the wind and yanked ya back out",
	"got dizzy from spinning and stacked it hard",
	"repetitive strain injury from all the waving",
}

var weatherConditions = []weatherCondition{
	{Condition: "perfect sunny day", Modifier: 1.2},
	{Condition: "bit overcast but not bad", Modifier: 1.0},
	{Condition: "fuckin' hot as balls", Modifier: 0.9},
	{Condition: "started pissing down rain", Modifier: 0.7},
	{Condition: "windy as fuck", Modifier: 0.8},
	{Condition: "nice breeze keeping ya cool", Modifier: 1.1},
}
