package pissingcontest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

type Plugin struct {
	name         string
	eventBus     framework.EventBus
	sqlClient    *framework.SQLClient
	economy      *framework.EconomyClient
	store        *store
	ctx          context.Context
	cancel       context.CancelFunc
	running      bool
	mu           sync.RWMutex
	challenges   map[string]map[string]*activeChallenge
	cooldowns    map[string]map[string]time.Time
	roomBotNames map[string]string
	challengeTTL time.Duration
	cooldown     time.Duration
	config       Config
}

var (
	commandAliases = []string{
		"pissingcontest",
		"piss",
		"pissing_contest",
	}
)

func New() framework.Plugin {
	return &Plugin{
		name:         pluginName,
		challenges:   make(map[string]map[string]*activeChallenge),
		cooldowns:    make(map[string]map[string]time.Time),
		roomBotNames: make(map[string]string),
		challengeTTL: challengeTimeout,
		cooldown:     defaultCooldown,
	}
}

func (p *Plugin) Dependencies() []string {
	return []string{"sql", "economy"}
}

func (p *Plugin) Name() string { return p.name }

func (p *Plugin) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)
	p.economy = framework.NewEconomyClient(bus, p.name)
	p.store = newStore(p.sqlClient)
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}
	p.config.BotUsername = resolveBotUsername(p.config.BotUsername)

	if p.config.ChallengeDurationSeconds > 0 {
		p.challengeTTL = time.Duration(p.config.ChallengeDurationSeconds) * time.Second
	}
	if p.config.CooldownMinutes > 0 {
		p.cooldown = time.Duration(p.config.CooldownMinutes) * time.Minute
	}
	if p.challengeTTL <= 0 {
		p.challengeTTL = challengeTimeout
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

	if err := p.eventBus.Subscribe(fmt.Sprintf("command.%s.execute", p.name), p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe command execution: %w", err)
	}

	if err := p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage); err != nil {
		return fmt.Errorf("failed to subscribe chat message event: %w", err)
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

	p.mu.Lock()
	for _, roomChallenges := range p.challenges {
		for _, ch := range roomChallenges {
			if ch.Timer != nil {
				ch.Timer.Stop()
			}
		}
	}
	p.challenges = make(map[string]map[string]*activeChallenge)
	p.cooldowns = make(map[string]map[string]time.Time)
	p.roomBotNames = make(map[string]string)
	p.mu.Unlock()

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

func (p *Plugin) registerCommands() error {
	event := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands":    "pissingcontest,piss,pissing_contest",
					"min_rank":    "0",
					"description": "challenge someone to a pissing contest",
				},
			},
		},
	}
	if err := p.eventBus.Broadcast("command.register", event); err != nil {
		return fmt.Errorf("failed to register command: %w", err)
	}
	return nil
}

func (p *Plugin) handleCommand(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent == nil || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.Data == nil || req.Data.Command == nil {
		return nil
	}

	params := req.Data.Command.Params
	channel := normalizeChannel(params["channel"])
	username := normalizeUsername(params["username"])
	isPM := params["is_pm"] == "true"
	command := strings.TrimSpace(strings.ToLower(req.Data.Command.Name))
	args := req.Data.Command.Args

	if username == "" || channel == "" {
		return nil
	}
	if isPM {
		p.sendCommandResult(channel, username, "piss contest is a public-only command, run it in chat", true)
		return nil
	}
	isCommand := false
	for _, alias := range commandAliases {
		if command == alias {
			isCommand = true
			break
		}
	}
	if !isCommand {
		return nil
	}
	p.handlePissCommand(channel, username, args)
	return nil
}

func (p *Plugin) handlePissCommand(channel, username string, args []string) {
	if len(args) == 0 {
		p.sendCommandResult(channel, username, "gotta challenge someone mate - !piss <amount> <username> or !piss <username> for bragging rights", false)
		return
	}

	amount := int64(0)
	target := ""
	if parsedAmount, err := strconv.ParseInt(args[0], 10, 64); err == nil {
		if parsedAmount < 0 {
			p.sendCommandResult(channel, username, "can't bet negative money", false)
			return
		}
		amount = parsedAmount
		if len(args) < 2 {
			p.sendCommandResult(channel, username, "gotta specify who to piss against mate - !piss <amount> <username>", false)
			return
		}
		target = normalizePissTarget(args[1])
	} else {
		target = normalizePissTarget(args[0])
	}
	if target == "" {
		p.sendCommandResult(channel, username, "gotta specify who to piss against mate - !piss <amount> <username>", false)
		return
	}
	if target == username {
		p.sendCommandResult(channel, username, "can't piss against yaself", false)
		return
	}

	challenge, err := p.createChallenge(channel, username, target, amount)
	if err != nil {
		p.sendCommandResult(channel, username, err.Error(), false)
		return
	}

	if p.isBotTarget(channel, target) {
		p.sendPublic(channel, fmt.Sprintf("Dazza doesn't run from a challenge — -%s accepts -%s's piss contest!", challenge.Challenged, challenge.Challenger))
		p.handleAcceptChallenge(challenge.Challenger, channel, target)
		return
	}

	if amount > 0 {
		p.sendCommandResult(channel, "", fmt.Sprintf("-%s challenges -%s to a $%d pissing contest! Type 'yes' or 'no' to respond (%ds to accept)",
			challenge.Challenger, challenge.Challenged, challenge.Amount, int(p.challengeTTL.Seconds())), false)
		return
	}
	p.sendCommandResult(channel, "", fmt.Sprintf("-%s challenges -%s to a pissing contest for bragging rights! Type 'yes' or 'no' to respond (%ds to accept)",
		challenge.Challenger, challenge.Challenged, int(p.challengeTTL.Seconds())), false)
}

func (p *Plugin) createChallenge(room, challenger, challenged string, amount int64) (*activeChallenge, error) {
	if challenger == "" || challenged == "" || room == "" {
		return nil, fmt.Errorf("invalid challenge request")
	}

	if err := p.assertCooldownReady(room, challenger); err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	roomChallenges := p.getRoomChallenges(room)
	if _, exists := roomChallenges[challenger]; exists {
		return nil, fmt.Errorf("ya already got your dick out mate, finish that contest first")
	}

	if amount > 0 {
		bal, err := p.getBalance(room, challenger)
		if err != nil {
			return nil, fmt.Errorf("wallet is flaky right now, try again later")
		}
		if bal < amount {
			return nil, fmt.Errorf("ya need $%d to piss mate, you've only got $%d", amount, bal)
		}
	}

	ch := &activeChallenge{
		Challenger: challenger,
		Challenged: challenged,
		Amount:     amount,
		Room:       room,
		Status:     activeChallengeStatus,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(p.challengeTTL),
	}
	roomChallenges[challenger] = ch
	p.challenges[room] = roomChallenges

	ch.Timer = time.AfterFunc(p.challengeTTL, func() {
		p.handleExpiredChallenge(room, challenger)
	})

	return ch, nil
}

func (p *Plugin) handleExpiredChallenge(room, challenger string) {
	challenge := p.removeChallenge(room, challenger, activeChallengeStatus)
	if challenge == nil {
		return
	}
	p.sendPublic(room, fmt.Sprintf("-%s got stood up! Nobody wants to see that tiny thing", challenge.Challenger))
}

func (p *Plugin) findChallengeByTarget(room, target string) (*activeChallenge, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	roomChallenges, exists := p.challenges[room]
	if !exists {
		return nil, ""
	}
	for challenger, ch := range roomChallenges {
		if strings.EqualFold(ch.Challenged, target) && ch.Status == activeChallengeStatus {
			return ch, challenger
		}
	}
	return nil, ""
}

func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}

	msg := dataEvent.Data.ChatMessage
	room := normalizeChannel(msg.Channel)
	if room == "" {
		return nil
	}
	user := normalizeUsername(msg.Username)
	if user == "" {
		return nil
	}

	normalized := normalizeMessage(msg.Message)
	if normalized == "" {
		return nil
	}

	ch, challenger := p.findChallengeByTarget(room, user)
	if ch == nil {
		return nil
	}

	if isInPhraseList(normalized, acceptPhrases) {
		p.handleAcceptChallenge(challenger, room, user)
		return nil
	}
	if isInPhraseList(normalized, declinePhrases) {
		p.handleDeclineChallenge(challenger, room, user)
		return nil
	}

	return nil
}

func (p *Plugin) handleAcceptChallenge(challenger, room, challenged string) {
	p.mu.Lock()
	roomChallenges := p.getRoomChallengesLocked(room)
	challenge, ok := roomChallenges[challenger]
	if !ok || challenge == nil {
		p.mu.Unlock()
		return
	}
	if challenge.Status != activeChallengeStatus || !strings.EqualFold(challenge.Challenged, challenged) {
		p.mu.Unlock()
		return
	}
	delete(roomChallenges, challenger)
	if len(roomChallenges) == 0 {
		delete(p.challenges, room)
	} else {
		p.challenges[room] = roomChallenges
	}
	if challenge.Timer != nil {
		challenge.Timer.Stop()
	}
	p.mu.Unlock()

	if !p.isBotTarget(room, challenged) {
		if err := p.assertCooldownReady(room, challenged); err != nil {
			p.sendPublic(room, err.Error())
			return
		}
	}

	if challenge.Amount > 0 && !p.isBotTarget(room, challenged) {
		balance, err := p.getBalance(room, challenged)
		if err != nil {
			p.sendPublic(room, "ya got a weird wallet error mate - challenge dropped")
			return
		}
		if balance < challenge.Amount {
			p.sendPublic(room, fmt.Sprintf("-%s tried to accept but got broke", challenged))
			p.sendPublic(room, fmt.Sprintf("ya need $%d to accept mate, you've only got $%d", challenge.Amount, balance))
			return
		}
	}

	challenge.Status = "accepted"
	p.startContest(challenge)
}

func (p *Plugin) handleDeclineChallenge(challenger, room, challenged string) {
	ch := p.removeChallenge(room, challenger, activeChallengeStatus)
	if ch == nil {
		return
	}
	p.sendPublic(room, fmt.Sprintf("-%s pussied out! Kept it in their pants like a coward", challenged))
}

func (p *Plugin) isBotTarget(channel, username string) bool {
	normalized := normalizePissTarget(username)
	if normalized == "" {
		return false
	}

	configured := normalizePissTarget(p.config.BotUsername)
	if configured != "" && strings.EqualFold(normalized, configured) {
		return true
	}

	roomBot := p.getResolvedBotName(channel)
	if roomBot != "" && strings.EqualFold(normalized, roomBot) {
		return true
	}

	// Fallback to configured room bot names if we can resolve and cache one.
	resolved, err := p.resolveBotNameForRoom(channel)
	if err != nil {
		return false
	}
	if resolved == "" {
		return false
	}

	return strings.EqualFold(normalized, resolved)
}

func normalizePissTarget(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	t = strings.TrimLeft(t, "-@")
	t = strings.Trim(t, " \t\n\r,.;:!?()[]{}\"'`")
	return normalizeUsername(t)
}

func (p *Plugin) getResolvedBotName(channel string) string {
	room := normalizeChannel(channel)
	if room == "" {
		return ""
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.roomBotNames[room]
}

func (p *Plugin) cacheResolvedBotName(channel, botName string) {
	room := normalizeChannel(channel)
	if room == "" {
		return
	}

	normalized := normalizePissTarget(botName)
	if normalized == "" {
		return
	}

	p.mu.Lock()
	p.roomBotNames[room] = normalized
	p.mu.Unlock()
}

func (p *Plugin) resolveBotNameForRoom(channel string) (string, error) {
	room := normalizeChannel(channel)
	if room == "" {
		return "", nil
	}

	requestID := fmt.Sprintf("piss-botname-%d", time.Now().UnixNano())
	request := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   requestID,
			To:   "core",
			From: p.name,
			Type: "get_configured_channels",
		},
	}

	ctx, cancel := context.WithTimeout(p.ctx, 2*time.Second)
	defer cancel()

	metadata := &framework.EventMetadata{
		Source:        p.name,
		CorrelationID: requestID,
	}

	responseEvent, err := p.eventBus.Request(ctx, "core", "plugin.request", request, metadata)
	if err != nil {
		return "", err
	}
	if responseEvent == nil || responseEvent.PluginResponse == nil {
		return "", nil
	}

	response := responseEvent.PluginResponse
	if !response.Success {
		if response.Error == "" {
			return "", nil
		}
		return "", fmt.Errorf("core get_configured_channels failed: %s", response.Error)
	}
	if response.Data == nil || len(response.Data.RawJSON) == 0 {
		return "", nil
	}

	botName, err := extractBotUsernameFromChannels(response.Data.RawJSON, room)
	if err != nil {
		return "", err
	}

	p.cacheResolvedBotName(room, botName)
	return botName, nil
}

func extractBotUsernameFromChannels(rawJSON json.RawMessage, channel string) (string, error) {
	var responseData struct {
		Channels []struct {
			Channel  string `json:"channel"`
			Username string `json:"username"`
		} `json:"channels"`
	}

	if err := json.Unmarshal(rawJSON, &responseData); err != nil {
		return "", fmt.Errorf("failed to parse configured channel response: %w", err)
	}

	channel = normalizeChannel(channel)
	for _, ch := range responseData.Channels {
		if strings.EqualFold(strings.TrimSpace(ch.Channel), channel) {
			return strings.TrimSpace(ch.Username), nil
		}
	}

	return "", nil
}

func resolveBotUsername(configBotUsername string) string {
	configured := strings.TrimSpace(configBotUsername)
	if configured != "" {
		return normalizeUsername(configured)
	}

	if fromEnv := strings.TrimSpace(os.Getenv("DAZ_BOT_NAME")); fromEnv != "" {
		return normalizeUsername(fromEnv)
	}

	return normalizeUsername(os.Getenv("DAZ_CYTUBE_USERNAME"))
}

func (p *Plugin) applyBotAdvantage(stats *contestResult, opponent *contestResult, ownerMods, opponentMods *contestModifiers, room, username string) {
	if stats == nil {
		return
	}
	if !p.isBotTarget(room, username) {
		return
	}

	advantage := characteristic{
		Name:    "Dazza advantage",
		Effects: map[string]float64{"all": 45, "opponent_aim": -35, "opponent_volume": -35},
	}
	applyCharacteristic(stats, advantage, opponent, ownerMods, opponentMods)
}

func (p *Plugin) startContest(ch *activeChallenge) {
	challenger := ch.Challenger
	challenged := ch.Challenged
	room := ch.Room

	p.sendPublic(room, fmt.Sprintf("%s -%s vs -%s", acceptAnnouncements[rand.Intn(len(acceptAnnouncements))], challenger, challenged))
	p.sendPublic(room, p.contestKickoffLine(ch))

	go p.runContest(ch)
}

func (p *Plugin) runContest(ch *activeChallenge) {
	time.Sleep(1500 * time.Millisecond)

	ch.ChallengerChar = randomCharacteristic()
	ch.ChallengedChar = randomCharacteristic()
	ch.Location = randomLocationAt(timeNowHour())
	ch.Weather = randomWeather()

	ch.ChallengerCond = p.rollContestCondition(&ch.ChallengerChar)
	ch.ChallengedCond = p.rollContestCondition(&ch.ChallengedChar)

	if ch.ChallengerChar.MutualCondition != "" {
		ch.ChallengerCond = p.conditionFromName(ch.ChallengerChar.MutualCondition)
		ch.ChallengedCond = ch.ChallengerCond
	}
	if ch.ChallengedChar.MutualCondition != "" {
		ch.ChallengedCond = p.conditionFromName(ch.ChallengedChar.MutualCondition)
		ch.ChallengerCond = ch.ChallengedCond
	}

	challengerStats := p.baseStats(ch.Room, ch.Challenger)
	challengedStats := p.baseStats(ch.Room, ch.Challenged)

	challengerWindSailor := isWindSailorCharacteristic(ch.ChallengerChar)
	challengedWindSailor := isWindSailorCharacteristic(ch.ChallengedChar)

	ch.ChallengerMods = contestModifiers{}
	ch.ChallengedMods = contestModifiers{}
	p.applyBotAdvantage(&challengerStats, &challengedStats, &ch.ChallengerMods, &ch.ChallengedMods, ch.Room, ch.Challenger)
	p.applyBotAdvantage(&challengedStats, &challengerStats, &ch.ChallengedMods, &ch.ChallengerMods, ch.Room, ch.Challenged)

	applyCharacteristic(&challengerStats, ch.ChallengerChar, &challengedStats, &ch.ChallengerMods, &ch.ChallengedMods)
	applyCharacteristic(&challengedStats, ch.ChallengedChar, &challengerStats, &ch.ChallengedMods, &ch.ChallengerMods)

	if ch.ChallengerCond.Name != "" {
		applyCondition(&challengerStats, ch.ChallengerCond, &challengedStats, &ch.ChallengerMods, &ch.ChallengedMods)
		ch.ChallengerFailMsg = ch.ChallengerCond.Message
	}
	if ch.ChallengedCond.Name != "" {
		applyCondition(&challengedStats, ch.ChallengedCond, &challengerStats, &ch.ChallengedMods, &ch.ChallengerMods)
		ch.ChallengedFailMsg = ch.ChallengedCond.Message
	}

	var weatherShifted bool
	ch.Weather, weatherShifted = p.maybeChangeWeather(ch.Weather, ch.ChallengerMods, ch.ChallengedMods)
	applyLocationEffects(&challengerStats, ch.Location, &ch.ChallengerMods)
	applyLocationEffects(&challengedStats, ch.Location, &ch.ChallengedMods)
	applyWeatherEffects(&challengerStats, ch.Weather, challengerWindSailor, &ch.ChallengerMods)
	applyWeatherEffects(&challengedStats, ch.Weather, challengedWindSailor, &ch.ChallengedMods)
	p.applyResultModifiers(&challengerStats, &challengedStats, &ch.ChallengerMods)
	p.applyResultModifiers(&challengedStats, &challengerStats, &ch.ChallengedMods)

	ch.locationEvents = checkLocationEvents(ch.Location)
	ch.weatherEvents = checkWeatherEvents(ch.Weather)
	if weatherShifted {
		ch.weatherEvents = append(ch.weatherEvents, contestEvent{
			Message: "the weather shifted mid-contest",
		})
	}
	p.sendPublic(ch.Room, p.preContestFlavorLine(ch))

	challengerScore := challengerStats.score()
	challengedScore := challengedStats.score()

	ch.ChallengerStats = challengerStats
	ch.ChallengedStats = challengedStats
	ch.ChallengerScore = challengerScore
	ch.ChallengedScore = challengedScore

	p.sendPublic(ch.Room, fmt.Sprintf("💦 -%s '%s' vs -%s '%s' @ %s", ch.Challenger, ch.ChallengerChar.Name, ch.Challenged, ch.ChallengedChar.Name, ch.Location.Name))
	p.sendPublic(ch.Room, formatWeather(ch.Weather))
	if ch.ChallengerCond.Name != "" && ch.ChallengedCond.Name != "" {
		p.sendPublic(ch.Room, fmt.Sprintf("Conditions: -%s [*%s*], -%s [*%s*]", ch.Challenger, ch.ChallengerCond.Name, ch.Challenged, ch.ChallengedCond.Name))
	}
	weatherFlair := ch.Location.Description
	if ch.Weather.Special != nil && ch.Weather.Special.Message != "" {
		weatherFlair = ch.Weather.Special.Message
	}
	p.sendPublic(ch.Room, fmt.Sprintf("%s - %s", formatWeather(ch.Weather), weatherFlair))
	time.Sleep(1500 * time.Millisecond)
	p.sendPublic(ch.Room, "💦 I'm not sure what happens next...")

	time.Sleep(time.Second * 12)
	p.resolveContest(ch)
}

func (p *Plugin) contestKickoffLine(ch *activeChallenge) string {
	if ch == nil || ch.Challenger == "" || ch.Challenged == "" {
		return "💬 The wall is lit and everybody is taking notes."
	}
	base := randomFromSlice(contestOpeners)
	if ch.Amount > 0 {
		return fmt.Sprintf("💸 %s put $%d on the board. %s", ch.Challenger, ch.Amount, base)
	}
	return "🤍 " + base
}

func (p *Plugin) preContestFlavorLine(ch *activeChallenge) string {
	if ch == nil {
		return "🔎 Something is about to get weird."
	}
	taunt := randomFromSlice(contestTaunts)
	return fmt.Sprintf("💬 %s vs %s, %s", ch.Challenger, ch.Challenged, taunt)
}

func (p *Plugin) victoryFlavorLine(winner string, winnerScore, loserScore int) string {
	flair := randomFromSlice(victoryFlavorMessages)
	diff := winnerScore - loserScore
	switch {
	case diff > 300:
		return fmt.Sprintf("💥 %s absolutely owned this: %s", winner, flair)
	case diff > 0:
		return fmt.Sprintf("💥 %s takes it: %s", winner, flair)
	default:
		return fmt.Sprintf("💥 %s edges out with dirty margins, %s", winner, flair)
	}
}

func (p *Plugin) tieFlavorLine(ch *activeChallenge) string {
	if ch == nil {
		return "🧻 This run ends in static and no clean winner."
	}
	taunt := randomFromSlice(tieFlavorMessages)
	return fmt.Sprintf("🧯 %s and %s are neck-and-neck, %s", ch.Challenger, ch.Challenged, taunt)
}

func failureFlavorLine() string {
	return fmt.Sprintf("🚨 %s", randomFromSlice(failureFlavorMessages))
}

func (p *Plugin) resolveContest(ch *activeChallenge) {
	// Instant failure check
	challengerFail, challengedFail, challengerFailMsg, challengedFailMsg := p.evaluateContestFailure(ch)
	ch.ChallengerFailMsg = firstNonEmpty(challengerFailMsg, ch.ChallengerFailMsg)
	ch.ChallengedFailMsg = firstNonEmpty(challengedFailMsg, ch.ChallengedFailMsg)
	winner, loser := determineWinner(challengerFail, challengedFail, ch.Challenger, ch.Challenged, ch.ChallengerScore, ch.ChallengedScore)

	if challengerFail || challengedFail {
		p.resolveFailure(ch, winner, loser)
		return
	}

	if ch.ChallengerScore == ch.ChallengedScore {
		p.sendPublic(ch.Room, "No winner! Both of you put in the same amount of crap.")
		p.sendPublic(ch.Room, p.tieFlavorLine(ch))
		p.announceSpecialEvents(ch)
		p.sendRibbonTaunt(ch.Room, ch.ChallengerStats, ch.Challenger)
		p.sendPublic(ch.Room, formatStats(ch.ChallengerStats, ch.ChallengerScore, ch.Challenger))
		p.sendRibbonTaunt(ch.Room, ch.ChallengedStats, ch.Challenged)
		p.sendPublic(ch.Room, formatStats(ch.ChallengedStats, ch.ChallengedScore, ch.Challenged))
		p.recordOutcome(ch, "", "", true)
		ctx, cancel := p.withTimeout(10 * time.Second)
		defer cancel()
		_ = p.store.saveCompletedMatch(ctx, ch)
		return
	}
	if winner == ch.Challenger {
		p.sendPublic(ch.Room, fmt.Sprintf("🏆 -%s ['%s'] fuckin WINS with %d points!", winner, ch.ChallengerChar.Name, ch.ChallengerScore))
		p.sendPublic(ch.Room, p.victoryFlavorLine(winner, ch.ChallengerScore, ch.ChallengedScore))
	} else {
		p.sendPublic(ch.Room, fmt.Sprintf("🏆 -%s ['%s'] fuckin WINS with %d points!", winner, ch.ChallengedChar.Name, ch.ChallengedScore))
		p.sendPublic(ch.Room, p.victoryFlavorLine(winner, ch.ChallengedScore, ch.ChallengerScore))
	}

	if ch.Amount > 0 {
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		if _, err := p.economy.Transfer(ctx, framework.TransferRequest{
			Channel:      ch.Room,
			FromUsername: loser,
			ToUsername:   winner,
			Amount:       ch.Amount,
			Reason:       "pissing_contest",
		}); err != nil {
			logger.Error(p.name, "piss contest transfer failed: %v", err)
			p.sendPublic(ch.Room, "payout didn't land, but contest result still stands")
		} else {
			p.sendPublic(ch.Room, fmt.Sprintf("💰 -%s wins $%d from -%s!", winner, ch.Amount, loser))
		}
	}
	p.announceSpecialEvents(ch)

	p.recordOutcome(ch, winner, loser, true)
	p.sendRibbonTaunt(ch.Room, ch.ChallengerStats, ch.Challenger)
	p.sendPublic(ch.Room, formatStats(ch.ChallengerStats, ch.ChallengerScore, ch.Challenger))
	p.sendRibbonTaunt(ch.Room, ch.ChallengedStats, ch.Challenged)
	p.sendPublic(ch.Room, formatStats(ch.ChallengedStats, ch.ChallengedScore, ch.Challenged))
	ctx, cancel := p.withTimeout(10 * time.Second)
	defer cancel()
	_ = p.store.saveCompletedMatch(ctx, ch)
	p.applyFines(ch, winner, loser)
}

func (p *Plugin) rollContestCondition(char *characteristic) condition {
	if char == nil {
		return p.conditionFromName("")
	}
	if char.SelfCondition != "" {
		cond, _ := getConditionByName(char.SelfCondition)
		if cond.Name != "" {
			return cond
		}
	}
	return p.conditionFromName("")
}

func (p *Plugin) conditionFromName(name string) condition {
	if strings.EqualFold(name, "random") || name == "" {
		cond, ok := randomCondition()
		if ok {
			return cond
		}
		return condition{}
	}
	if cond, ok := getConditionByName(name); ok {
		return cond
	}
	return condition{}
}

func (p *Plugin) resolveFailure(ch *activeChallenge, winner, loser string) {
	p.announceSpecialEvents(ch)
	challengerMessage := firstNonEmpty(ch.ChallengerFailMsg, ch.ChallengerCond.Message)
	challengedMessage := firstNonEmpty(ch.ChallengedFailMsg, ch.ChallengedCond.Message)
	if challengerMessage == "" {
		challengerMessage = "couldn't perform the first piss"
	}
	if challengedMessage == "" {
		challengedMessage = "couldn't perform the first piss"
	}

	switch {
	case winner == "" && loser == "":
		switch {
		case ch.ChallengerCond.Mutual && ch.ChallengerCond.Name != "" && ch.ChallengerCond.Name == ch.ChallengedCond.Name && ch.ChallengerCond.Message != "":
			p.sendPublic(ch.Room, fmt.Sprintf("Both cunts %s! No winner! What a fuckin embarrassment!", ch.ChallengerCond.Message))
		case challengerMessage != "" && challengedMessage != "":
			p.sendPublic(ch.Room, fmt.Sprintf("Both cunts failed! -%s %s and -%s %s!", ch.Challenger, challengerMessage, ch.Challenged, challengedMessage))
		default:
			p.sendPublic(ch.Room, "No winner! both spazzed out and no payout.")
		}
	case winner == ch.Challenger:
		p.sendPublic(ch.Room, fmt.Sprintf("💦 -%s %s! -%s WINS!", ch.Challenged, challengedMessage, winner))
		p.handleFailureOutcome(ch, winner, loser, challengedMessage)
	case winner == ch.Challenged:
		p.sendPublic(ch.Room, fmt.Sprintf("💦 -%s %s! -%s WINS!", ch.Challenger, challengerMessage, winner))
		p.handleFailureOutcome(ch, winner, loser, challengerMessage)
	}
	p.sendPublic(ch.Room, failureFlavorLine())
	p.sendRibbonTaunt(ch.Room, ch.ChallengerStats, ch.Challenger)
	p.sendPublic(ch.Room, formatStats(ch.ChallengerStats, ch.ChallengerScore, ch.Challenger))
	p.sendRibbonTaunt(ch.Room, ch.ChallengedStats, ch.Challenged)
	p.sendPublic(ch.Room, formatStats(ch.ChallengedStats, ch.ChallengedScore, ch.Challenged))
	p.recordOutcome(ch, winner, loser, false)
	p.applyFines(ch, winner, loser)
}

func (p *Plugin) sendRibbonTaunt(room string, stats contestResult, username string) {
	if username == "" {
		return
	}
	if !isRibbingMaterial(stats) {
		return
	}

	msg := randomFromSlice(ribbonTaunts)
	if msg == "" {
		return
	}

	p.sendPublic(room, fmt.Sprintf(msg, username))
}

func isRibbingMaterial(stats contestResult) bool {
	if stats.volume >= 1500 {
		return true
	}
	if math.Round(stats.volume) >= 1200 && stats.distance >= 6 {
		return true
	}
	return false
}

func (p *Plugin) announceSpecialEvents(ch *activeChallenge) {
	locationMessages := ch.locationEvents
	weatherMessages := ch.weatherEvents
	if len(locationMessages) == 0 {
		locationMessages = checkLocationEvents(ch.Location)
	}
	if len(weatherMessages) == 0 {
		weatherMessages = checkWeatherEvents(ch.Weather)
	}

	events := make([]contestEvent, 0, len(locationMessages)+len(weatherMessages))
	events = append(events, locationMessages...)
	events = append(events, weatherMessages...)

	if len(events) == 0 {
		return
	}

	for idx, message := range events {
		msg := message.Message
		delay := time.Duration(4+idx) * time.Second
		time.AfterFunc(delay, func() {
			select {
			case <-p.ctx.Done():
				return
			default:
				p.sendPublic(ch.Room, msg)
			}
		})
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func clampChance(chance float64) float64 {
	if chance <= 0 {
		return 0
	}
	if chance >= 1 {
		return 1
	}
	return chance
}

func (p *Plugin) maybeChangeWeather(w weatherRoll, challengerMods, challengedMods contestModifiers) (weatherRoll, bool) {
	changeChance := clampChance(challengerMods.weatherChangeChance + challengedMods.weatherChangeChance)
	if changeChance <= 0 {
		return w, false
	}
	if !rollChance(changeChance) {
		return w, false
	}
	return randomWeather(), true
}

func (p *Plugin) applyResultModifiers(stats, opponent *contestResult, mods *contestModifiers) {
	if stats == nil || opponent == nil || mods == nil {
		return
	}
	if mods.copyBestStat {
		copyKey := bestStat(*opponent)
		switch copyKey {
		case "distance":
			stats.distance = opponent.distance
		case "volume":
			stats.volume = opponent.volume
		case "aim":
			stats.aim = opponent.aim
		case "duration":
			stats.duration = opponent.duration
		}
	}

	if mods.coldTurtle {
		stats.aim *= 0.6
		stats.distance *= 0.7
		stats.volume *= 0.8
	}

	if mods.wrongWay || mods.wrongDirectionChance >= 1 {
		stats.wrongDirection = true
		stats.wrongDirectionCeil = 10
		stats.aim *= 0.25
	}
	if mods.straightUp {
		stats.straightUp = true
		stats.distance *= 0.05
		stats.aim *= 0.05
		stats.duration *= 1.4
	}

	if mods.shaking {
		stats.aim *= 0.75
		stats.duration *= 1.25
	}

	if mods.badRNG {
		stats.distance *= (1 + randBetween(-0.2, 0.2))
		stats.volume *= (1 + randBetween(-0.3, 0.3))
		stats.aim *= (1 + randBetween(-0.25, 0.25))
		stats.duration *= (1 + randBetween(-0.15, 0.15))
	}

	targetBounds(stats)
}

func bestStat(stats contestResult) string {
	best := "distance"
	bestValue := stats.distance
	if stats.volume > bestValue {
		best = "volume"
		bestValue = stats.volume
	}
	if stats.aim > bestValue {
		best = "aim"
		bestValue = stats.aim
	}
	if stats.duration > bestValue {
		best = "duration"
		bestValue = stats.duration
	}
	return best
}

func (p *Plugin) evaluateContestFailure(ch *activeChallenge) (challengerFail bool, challengedFail bool, challengerMessage, challengedMessage string) {
	if ch == nil {
		return false, false, "", ""
	}
	baseChallengerFail := isFailureCondition(ch.ChallengerCond.Type)
	baseChallengedFail := isFailureCondition(ch.ChallengedCond.Type)
	challengerMessage = firstNonEmpty(ch.ChallengerFailMsg, ch.ChallengerCond.Message)
	challengedMessage = firstNonEmpty(ch.ChallengedFailMsg, ch.ChallengedCond.Message)
	baseChallengerMessage := challengerMessage
	baseChallengedMessage := challengedMessage

	for _, evt := range ch.locationEvents {
		if !evt.Forfeit || evt.Message == "" {
			continue
		}
		baseChallengerFail = true
		baseChallengedFail = true
		if challengerMessage == "" {
			challengerMessage = evt.Message
		}
		if challengedMessage == "" {
			challengedMessage = evt.Message
		}
	}
	for _, evt := range ch.weatherEvents {
		if !evt.Forfeit || evt.Message == "" {
			continue
		}
		baseChallengerFail = true
		baseChallengedFail = true
		if challengerMessage == "" {
			challengerMessage = evt.Message
		}
		if challengedMessage == "" {
			challengedMessage = evt.Message
		}
	}

	if baseChallengerFail || baseChallengedFail {
		if baseChallengerMessage != "" {
			challengerMessage = baseChallengerMessage
		}
		if baseChallengedMessage != "" {
			challengedMessage = baseChallengedMessage
		}
		return baseChallengerFail, baseChallengedFail, challengerMessage, challengedMessage
	}

	challengerFail = baseChallengerFail
	challengedFail = baseChallengedFail

	challengerFailChance := clampChance(ch.ChallengerMods.forfeitChance + ch.ChallengerMods.allFailureChance)
	challengedFailChance := clampChance(ch.ChallengedMods.forfeitChance + ch.ChallengedMods.allFailureChance)
	if ch.ChallengerMods.badRNG {
		challengerFailChance = clampChance(challengerFailChance + 0.1)
	}
	if ch.ChallengedMods.badRNG {
		challengedFailChance = clampChance(challengedFailChance + 0.1)
	}

	if rollChance(challengerFailChance) {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "got called out and forfeit'd this round"
		}
	}
	if rollChance(challengedFailChance) {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "got called out and forfeit'd this round"
		}
	}

	if ch.ChallengerMods.wrongWay || rollChance(ch.ChallengerMods.wrongDirectionChance) {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "fired in the wrong direction"
		}
	}
	if ch.ChallengedMods.wrongWay || rollChance(ch.ChallengedMods.wrongDirectionChance) {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "fired in the wrong direction"
		}
	}

	if rollChance(clampChance(ch.ChallengerMods.malfunctionChance)) {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "equipment malfunctioned"
		}
	}
	if rollChance(clampChance(ch.ChallengedMods.malfunctionChance)) {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "equipment malfunctioned"
		}
	}
	if ch.ChallengerMods.randomFailures && rollChance(0.25) {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "failed under chaotic conditions"
		}
	}
	if ch.ChallengedMods.randomFailures && rollChance(0.25) {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "failed under chaotic conditions"
		}
	}

	if ch.ChallengerMods.perfectOrFail {
		challengerFail = challengerFail || rollChance(0.25)
	}
	if ch.ChallengedMods.perfectOrFail {
		challengedFail = challengedFail || rollChance(0.25)
	}
	if ch.ChallengerMods.startStrongFail && ch.ChallengerScore < 300 {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "couldn't get momentum and failed"
		}
	}
	if ch.ChallengedMods.startStrongFail && ch.ChallengedScore < 300 {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "couldn't get momentum and failed"
		}
	}

	_, baseLoser := determineWinner(challengerFail, challengedFail, ch.Challenger, ch.Challenged, ch.ChallengerScore, ch.ChallengedScore)
	if ch.ChallengerMods.quitIfLosing && baseLoser == ch.Challenger {
		challengerFail = true
		if challengerMessage == "" {
			challengerMessage = "gave up after watching the lead"
		}
	}
	if ch.ChallengedMods.quitIfLosing && baseLoser == ch.Challenged {
		challengedFail = true
		if challengedMessage == "" {
			challengedMessage = "gave up after watching the lead"
		}
	}

	if ch.ChallengerMods.multipleFailures && challengerFail {
		challengedFail = true
	}
	if ch.ChallengedMods.multipleFailures && challengedFail {
		challengerFail = true
	}
	if (ch.ChallengerMods.mutualMalfunction || ch.ChallengedMods.mutualMalfunction) &&
		(challengerFail || challengedFail) {
		challengerFail = true
		challengedFail = true
		if challengerMessage == "" {
			challengerMessage = "malfunctioned together and forfeit'd"
		}
		if challengedMessage == "" {
			challengedMessage = "malfunctioned together and forfeit'd"
		}
	}

	return
}

func (p *Plugin) handleFailureOutcome(ch *activeChallenge, winner, loser, loserMessage string) {
	if winner == "" || loser == "" {
		return
	}
	if ch.Amount > 0 {
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		defer cancel()
		if _, err := p.economy.Transfer(ctx, framework.TransferRequest{
			Channel:      ch.Room,
			FromUsername: loser,
			ToUsername:   winner,
			Amount:       ch.Amount,
			Reason:       "pissing_contest",
		}); err != nil {
			logger.Error(p.name, "piss contest failure transfer failed: %v", err)
			p.sendPublic(ch.Room, "payout didn't land, but contest result still stands")
		} else {
			p.sendPublic(ch.Room, fmt.Sprintf("💰 -%s wins $%d from -%s!", winner, ch.Amount, loser))
		}
		return
	}

	loserSuffix := ""
	if loserMessage != "" {
		loserSuffix = fmt.Sprintf(" %s!", loserMessage)
	}
	if len(failureBragMessages) > 0 {
		p.sendPublic(ch.Room, fmt.Sprintf("💦 -%s%s -%s WINS by default! %s", loser, loserSuffix, winner, failureBragMessages[rand.Intn(len(failureBragMessages))]))
	}
}

func (p *Plugin) recordOutcome(ch *activeChallenge, winner, loser string, drainedBladder bool) {
	if ch == nil {
		return
	}
	var (
		wonScorer, loseScorer   contestResult
		winnerScore, loserScore int
	)
	if winner == ch.Challenger {
		wonScorer = ch.ChallengerStats
		loseScorer = ch.ChallengedStats
		winnerScore = ch.ChallengerScore
		loserScore = ch.ChallengedScore
	} else if winner == ch.Challenged {
		wonScorer = ch.ChallengedStats
		loseScorer = ch.ChallengerStats
		winnerScore = ch.ChallengedScore
		loserScore = ch.ChallengerScore
	}

	ctx, cancel := p.withTimeout(10 * time.Second)
	defer cancel()
	if winner != "" {
		_ = p.store.recordContestStats(ctx, ch.Room, winner, loser, winnerScore, loserScore, wonScorer, loseScorer, ch.Amount)
	} else {
		_ = p.store.recordContestStats(ctx, ch.Room, "", "", 0, 0, ch.ChallengerStats, ch.ChallengedStats, 0)
	}
	if !drainedBladder {
		return
	}
	_ = p.store.resetBladder(ctx, ch.Room, ch.Challenger)
	_ = p.store.resetBladder(ctx, ch.Room, ch.Challenged)
	p.setCooldown(ch.Room, ch.Challenger)
	p.setCooldown(ch.Room, ch.Challenged)
}

func (p *Plugin) applyFines(ch *activeChallenge, _ string, _ string) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	applyFineFor := func(username string, cond condition, location location) {
		if username == "" {
			return
		}
		var fine int64
		var message string
		if cond.Fine > 0 && rollChance(cond.FineChance) {
			fine += cond.Fine
			message = cond.FineMessage
		}
		if location.Fine > 0 && rollChance(location.FineChance) {
			fine += location.Fine
			message = location.FineMessage
		}
		if fine <= 0 {
			return
		}
		_, err := p.economy.Debit(ctx, framework.DebitRequest{
			Channel:  ch.Room,
			Username: username,
			Amount:   fine,
			Reason:   "pissing_contest_fine",
		})
		if err != nil {
			p.sendPublic(ch.Room, fmt.Sprintf("%s", message))
			return
		}
		if message != "" {
			p.sendPublic(ch.Room, fmt.Sprintf("-%s %s", username, message))
		}
	}

	applyFineFor(ch.Challenger, ch.ChallengerCond, ch.Location)
	applyFineFor(ch.Challenged, ch.ChallengedCond, ch.Location)
}

func (p *Plugin) baseStats(room, user string) contestResult {
	bladderAmount := p.bladderAmount(room, user)

	distance := randBetween(0.5, 5.0)
	volume := randBetween(200.0, 2000.0)
	aim := randBetween(10.0, 100.0)
	duration := randBetween(2.0, 30.0)

	if bladderAmount <= 0 {
		volume *= 0.5
		distance *= 0.8
	} else {
		volume *= (1 + math.Min(float64(bladderAmount)*0.01, 1))
		aim *= (1 + math.Min(float64(bladderAmount)*0.001, 0.7))
		distance *= (1 + math.Min(float64(bladderAmount)*0.002, 0.5))
		duration *= (1 + math.Min(float64(bladderAmount)*0.02, 2))
	}

	return contestResult{
		distance: distance,
		volume:   volume,
		aim:      aim,
		duration: duration,
	}
}

func (p *Plugin) getBalance(channel, user string) (int64, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	return p.economy.GetBalance(ctx, channel, user)
}

func (p *Plugin) bladderAmount(channel, user string) int64 {
	ctx, cancel := context.WithTimeout(p.ctx, 3*time.Second)
	defer cancel()
	amount, err := p.store.getBladderState(ctx, channel, user)
	if err != nil {
		logger.Warn(p.name, "failed reading bladder for %s/%s: %v", user, channel, err)
		return 0
	}
	return amount
}

func (p *Plugin) assertCooldownReady(room, user string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	roomCD := p.getRoomCooldowns(room)
	last, ok := roomCD[user]
	if !ok {
		return nil
	}
	if elapsed := time.Since(last); elapsed < p.cooldown {
		remain := int(p.cooldown.Seconds() - elapsed.Seconds())
		return fmt.Errorf("still shaking it off mate, wait %ds", remain)
	}
	return nil
}

func (p *Plugin) setCooldown(room, user string) {
	p.mu.Lock()
	roomCD := p.getRoomCooldowns(room)
	roomCD[user] = time.Now()
	p.cooldowns[room] = roomCD
	p.mu.Unlock()
}

func (p *Plugin) removeChallenge(room, challenger, expectedStatus string) *activeChallenge {
	p.mu.Lock()
	defer p.mu.Unlock()
	roomChallenges := p.getRoomChallengesLocked(room)
	ch, ok := roomChallenges[challenger]
	if !ok {
		return nil
	}
	if expectedStatus != "" && ch.Status != expectedStatus {
		return nil
	}
	delete(roomChallenges, challenger)
	if len(roomChallenges) == 0 {
		delete(p.challenges, room)
	} else {
		p.challenges[room] = roomChallenges
	}
	if ch.Timer != nil {
		ch.Timer.Stop()
	}
	return ch
}

func (p *Plugin) getRoomChallenges(room string) map[string]*activeChallenge {
	roomChallenges, exists := p.challenges[room]
	if !exists {
		roomChallenges = make(map[string]*activeChallenge)
		p.challenges[room] = roomChallenges
	}
	return roomChallenges
}

func (p *Plugin) getRoomChallengesLocked(room string) map[string]*activeChallenge {
	roomChallenges, exists := p.challenges[room]
	if !exists {
		roomChallenges = make(map[string]*activeChallenge)
		p.challenges[room] = roomChallenges
	}
	return roomChallenges
}

func (p *Plugin) getRoomCooldowns(room string) map[string]time.Time {
	roomCooldowns, exists := p.cooldowns[room]
	if !exists {
		roomCooldowns = make(map[string]time.Time)
		p.cooldowns[room] = roomCooldowns
	}
	return roomCooldowns
}

func (p *Plugin) sendPublic(channel, message string) {
	data := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Message: message,
			Channel: channel,
		},
	}
	if err := p.eventBus.Broadcast("cytube.send", data); err != nil {
		logger.Error(p.name, "failed to send public message: %v", err)
	}
}

func (p *Plugin) sendCommandResult(channel, username, message string, isPM bool) {
	if !isPM {
		p.sendPublic(channel, message)
		return
	}

	response := &framework.PluginResponse{
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
	}
	if err := p.eventBus.Broadcast("plugin.response", &framework.EventData{
		PluginResponse: response,
	}); err != nil {
		logger.Error(p.name, "failed to send command result: %v", err)
	}
}

func (p *Plugin) withTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(p.ctx, timeout)
}

func isInPhraseList(message string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.EqualFold(message, phrase) {
			return true
		}
	}
	return false
}

func isWindSailorCharacteristic(char characteristic) bool {
	if strings.EqualFold(char.Name, "wind sailor") {
		return true
	}
	return strings.EqualFold(char.SelfCondition, "wind_sailor")
}

func normalizeMessage(message string) string {
	normalized := strings.TrimSpace(strings.ToLower(message))
	normalized = strings.Trim(normalized, "!?.")
	normalized = strings.TrimSuffix(normalized, "!")
	normalized = strings.TrimSuffix(normalized, "?")
	normalized = strings.Trim(normalized, " ")
	return normalized
}

func rollChance(chance float64) bool {
	if chance >= 1 {
		return true
	}
	if chance <= 0 {
		return false
	}
	return rand.Float64() <= chance
}

func determineWinner(challengerFail, challengedFail bool, challenger, challenged string, challengerScore, challengedScore int) (string, string) {
	if challengerFail && challengedFail {
		return "", ""
	}
	if challengerFail {
		return challenged, challenger
	}
	if challengedFail {
		return challenger, challenged
	}
	if challengerScore > challengedScore {
		return challenger, challenged
	}
	if challengedScore > challengerScore {
		return challenged, challenger
	}
	return "", ""
}
