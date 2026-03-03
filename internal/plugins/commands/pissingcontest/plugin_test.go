package pissingcontest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEvent struct {
	eventType string
	data      *framework.EventData
}

type mockPissContestRequest struct {
	target    string
	eventType string
	data      *framework.EventData
}

type mockPissContestBus struct {
	mu            sync.Mutex
	broadcasts    []mockEvent
	subscriptions []string
	requests      []mockPissContestRequest
	requestErr    error
	requestResp   *framework.EventData
	onBroadcast   func(string, *framework.EventData)
}

func (m *mockPissContestBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cb := m.onBroadcast
	m.broadcasts = append(m.broadcasts, mockEvent{eventType: eventType, data: data})
	if cb != nil {
		cb(eventType, data)
	}
	return nil
}

func (m *mockPissContestBus) BroadcastWithMetadata(eventType string, data *framework.EventData, _ *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockPissContestBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockPissContestBus) SendWithMetadata(target string, eventType string, data *framework.EventData, _ *framework.EventMetadata) error {
	return nil
}

func (m *mockPissContestBus) Request(_ context.Context, target string, eventType string, data *framework.EventData, _ *framework.EventMetadata) (*framework.EventData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = append(m.requests, mockPissContestRequest{
		target:    target,
		eventType: eventType,
		data:      data,
	})

	if m.requestErr != nil {
		return nil, m.requestErr
	}

	if m.requestResp != nil {
		return m.requestResp, nil
	}

	return nil, nil
}

func (m *mockPissContestBus) DeliverResponse(correlationID string, response *framework.EventData, _ error) {
}

func (m *mockPissContestBus) Subscribe(eventType string, _ framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions = append(m.subscriptions, eventType)
	return nil
}

func (m *mockPissContestBus) SubscribeWithTags(pattern string, _ framework.EventHandler, _ []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions = append(m.subscriptions, pattern)
	return nil
}

func (m *mockPissContestBus) RegisterPlugin(_ string, _ framework.Plugin) error { return nil }
func (m *mockPissContestBus) UnregisterPlugin(_ string) error                   { return nil }
func (m *mockPissContestBus) GetDroppedEventCounts() map[string]int64           { return map[string]int64{} }
func (m *mockPissContestBus) GetDroppedEventCount(_ string) int64               { return 0 }

func (m *mockPissContestBus) lastRawMessage() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.broadcasts) - 1; i >= 0; i-- {
		if m.broadcasts[i].data != nil && m.broadcasts[i].data.RawMessage != nil {
			return m.broadcasts[i].data.RawMessage.Message, true
		}
	}
	return "", false
}

func (m *mockPissContestBus) lastCommandResponse() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.broadcasts) - 1; i >= 0; i-- {
		resp := m.broadcasts[i].data
		if resp != nil && resp.PluginResponse != nil && resp.PluginResponse.Data != nil &&
			resp.PluginResponse.Data.CommandResult != nil {
			return resp.PluginResponse.Data.CommandResult.Output, true
		}
	}
	return "", false
}

func (m *mockPissContestBus) rawMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	messages := make([]string, 0, len(m.broadcasts))
	for _, event := range m.broadcasts {
		if event.data == nil || event.data.RawMessage == nil {
			continue
		}
		messages = append(messages, event.data.RawMessage.Message)
	}
	return messages
}

func (m *mockPissContestBus) requestCalls() []mockPissContestRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	calls := make([]mockPissContestRequest, len(m.requests))
	copy(calls, m.requests)
	return calls
}

func newTestPlugin(t *testing.T, cfg string) (*Plugin, *mockPissContestBus) {
	t.Helper()

	bus := &mockPissContestBus{}
	plugin := New().(*Plugin)
	if err := plugin.Init(json.RawMessage(cfg), bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	return plugin, bus
}

func makeCommandEvent(command string, args []string, params map[string]string) *framework.DataEvent {
	return framework.NewDataEvent("command.pissingcontest.execute", &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name:   command,
					Args:   args,
					Params: params,
				},
			},
		},
	})
}

func cleanupChallenge(p *Plugin, room, challenger string) {
	ch := p.removeChallenge(room, challenger, activeChallengeStatus)
	if ch != nil && ch.Timer != nil {
		ch.Timer.Stop()
	}
}

func TestNormalizeAndHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeChannel(" TestRoom "); got != "testroom" {
		t.Fatalf("normalizeChannel() = %q, want %q", got, "testroom")
	}
	if got := normalizeUsername(" @TestUser "); got != "testuser" {
		t.Fatalf("normalizeUsername() = %q, want %q", got, "testuser")
	}
	if got := normalizeMessage(" No? "); got != "no" {
		t.Fatalf("normalizeMessage() = %q, want %q", got, "no")
	}
	if !isInPhraseList("yes", acceptPhrases) {
		t.Fatalf("isInPhraseList() should include yes")
	}
	if got := normalizePissTarget("[dazza]"); got != "dazza" {
		t.Fatalf("normalizePissTarget([dazza]) = %q, want %q", got, "dazza")
	}
	if got := normalizePissTarget("@dazza,"); got != "dazza" {
		t.Fatalf("normalizePissTarget(@dazza,) = %q, want %q", got, "dazza")
	}
}

func TestDetermineWinner(t *testing.T) {
	t.Parallel()

	winner, loser := determineWinner(true, true, "alice", "bob", 100, 90)
	if winner != "" || loser != "" {
		t.Fatalf("both failures: winner=%q loser=%q", winner, loser)
	}

	winner, loser = determineWinner(true, false, "alice", "bob", 100, 90)
	if winner != "bob" || loser != "alice" {
		t.Fatalf("challenger fail: winner=%q loser=%q", winner, loser)
	}

	winner, loser = determineWinner(false, true, "alice", "bob", 100, 90)
	if winner != "alice" || loser != "bob" {
		t.Fatalf("challenged fail: winner=%q loser=%q", winner, loser)
	}

	winner, loser = determineWinner(false, false, "alice", "bob", 150, 90)
	if winner != "alice" || loser != "bob" {
		t.Fatalf("score comparison: winner=%q loser=%q", winner, loser)
	}
}

func TestContestResultScore(t *testing.T) {
	t.Parallel()

	score := contestResult{
		distance: 5.0,
		volume:   2000.0,
		aim:      100.0,
		duration: 30.0,
	}.score()
	if score != 1000 {
		t.Fatalf("score() = %d, want %d", score, 1000)
	}
}

func TestApplyEffect(t *testing.T) {
	t.Parallel()

	stats := contestResult{
		distance: 100,
		volume:   100,
		aim:      50,
		duration: 20,
	}
	applyEffect(&stats, nil, nil, nil, map[string]float64{"all": 20})
	if got, want := stats.distance, 120.0; got != want {
		t.Fatalf("all effect distance = %f, want %f", got, want)
	}

	stats = contestResult{
		distance: 100,
		volume:   100,
		aim:      50,
		duration: 20,
	}
	applyEffect(&stats, nil, nil, nil, map[string]float64{"distance_min": 150, "duration_max": 10})
	if got, want := stats.distance, 150.0; got != want {
		t.Fatalf("distance_min effect = %f, want %f", got, want)
	}
	if got, want := stats.duration, 10.0; got != want {
		t.Fatalf("duration_max effect = %f, want %f", got, want)
	}

	attacker := contestResult{aim: 50}
	applyEffect(&attacker, &stats, nil, nil, map[string]float64{"opponent_aim": 100})
	if got, want := stats.aim, 100.0; got != want {
		t.Fatalf("opponent effect not applied, got %f want %f", got, want)
	}
}

func TestApplyBotAdvantage(t *testing.T) {
	p, _ := newTestPlugin(t, `{"bot_username":"botman"}`)
	challengerStats := contestResult{
		distance: 2,
		volume:   100,
		aim:      10,
		duration: 10,
	}
	challengedStats := contestResult{
		distance: 2,
		volume:   100,
		aim:      10,
		duration: 10,
	}
	var challengerMods, challengedMods contestModifiers

	p.applyBotAdvantage(&challengerStats, &challengedStats, &challengerMods, &challengedMods, "room", "botman")
	if challengerStats.distance <= 2 || challengerStats.volume <= 100 {
		t.Fatalf("bot advantage should boost its own distance/volume, got distance %f volume %f", challengerStats.distance, challengerStats.volume)
	}
	if challengedStats.aim >= 10 || challengedStats.volume >= 100 {
		t.Fatalf("bot advantage should debuff challenged stats, got aimed %f volume %f", challengedStats.aim, challengedStats.volume)
	}

	before := challengedStats
	p.applyBotAdvantage(&challengedStats, &challengerStats, &challengedMods, &challengerMods, "room", "alice")
	if challengedStats != before {
		t.Fatalf("non-bot should not trigger advantage")
	}
}

func TestIsBotTargetFromCoreConfig(t *testing.T) {
	p, bus := newTestPlugin(t, `{}`)
	bus.onBroadcast = configuredChannelsResponder(t, p, []coreChannelConfig{
		{Channel: "room", Username: "dazza"},
	})

	if !p.isBotTarget("room", "dazza") {
		t.Fatal("expected target dazza to resolve from core-configured channels")
	}
	if p.isBotTarget("room", "alice") {
		t.Fatal("expected non-bot target to not match")
	}
}

func TestSendRibbonTauntUsesBotTaunts(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{"bot_username":"dazza"}`)
	p.sendRibbonTaunt("room", contestResult{volume: 1600}, "dazza")

	msg, ok := p.eventBus.(*mockPissContestBus).lastRawMessage()
	if !ok {
		t.Fatal("expected ribbon taunt message")
	}
	if !strings.Contains(msg, "Dazza") || !strings.Contains(msg, "his own") {
		t.Fatalf("expected bot-specific ribbon taunt, got %q", msg)
	}
}

func TestWeatherEffectsWindSailorMultiplier(t *testing.T) {
	t.Parallel()

	base := contestResult{
		distance: 100,
		volume:   100,
		aim:      100,
		duration: 10,
	}
	normal := base
	storm := weatherRoll{
		Wind: weather{
			Effects: map[string]float64{
				"distance": 10,
			},
		},
	}
	applyWeatherEffects(&normal, storm, false, nil)
	if diff := normal.distance - 110.0; diff < 0.000001 && diff > -0.000001 {
		// pass
	} else {
		t.Fatalf("non wind-sailor distance = %f, want %f", normal.distance, 110.0)
	}

	windSailor := base
	applyWeatherEffects(&windSailor, storm, true, nil)
	if diff := windSailor.distance - 120.0; diff < 0.000001 && diff > -0.000001 {
		// pass
	} else {
		t.Fatalf("wind-sailor distance = %f, want %f", windSailor.distance, 120.0)
	}
}

func TestRollChanceBoundaries(t *testing.T) {
	t.Parallel()

	if rollChance(0) {
		t.Fatal("rollChance(0) should be false")
	}
	if !rollChance(1) {
		t.Fatal("rollChance(1) should be true")
	}
}

func TestIsWindSailorCharacteristic(t *testing.T) {
	t.Parallel()

	if !isWindSailorCharacteristic(characteristic{Name: "Wind Sailor"}) {
		t.Fatal("expected Wind Sailor by name to match")
	}
	if !isWindSailorCharacteristic(characteristic{SelfCondition: "wind_sailor"}) {
		t.Fatal("expected wind_sailor self condition to match")
	}
}

func TestChainWinner(t *testing.T) {
	t.Parallel()

	ch := &activeChallenge{Challenger: "alice", Challenged: "bob", ChallengerScore: 12, ChallengedScore: 10}
	if got, want := chainWinner(ch), "alice"; got != want {
		t.Fatalf("chainWinner() = %q, want %q", got, want)
	}
}

func TestInitAndConfigParsing(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{"bot_username":" BOTMAN ","challenge_duration_seconds":12,"cooldown_minutes":2}`)

	if p.config.BotUsername != "botman" {
		t.Fatalf("BotUsername = %q, want %q", p.config.BotUsername, "botman")
	}
	if p.challengeTTL != 12*time.Second {
		t.Fatalf("challengeTTL = %s, want %s", p.challengeTTL, 12*time.Second)
	}
	if p.cooldown != 2*time.Minute {
		t.Fatalf("cooldown = %s, want %s", p.cooldown, 2*time.Minute)
	}
}

func TestRegisterCommands(t *testing.T) {
	p, bus := newTestPlugin(t, `{}`)
	if err := p.registerCommands(); err != nil {
		t.Fatalf("registerCommands() error = %v", err)
	}
	if len(bus.broadcasts) != 1 {
		t.Fatalf("expected exactly 1 broadcast, got %d", len(bus.broadcasts))
	}
	req := bus.broadcasts[0].data.PluginRequest
	if req == nil || req.Data == nil {
		t.Fatal("expected plugin request data")
	}
	if got := req.To; got != "eventfilter" {
		t.Fatalf("plugin request target = %q, want eventfilter", got)
	}
	if got := req.Data.KeyValue["commands"]; got != "pissingcontest,piss,pissing_contest" {
		t.Fatalf("command list = %q, want %q", got, "pissingcontest,piss,pissing_contest")
	}
}

func TestHandleCommandValidation(t *testing.T) {
	t.Parallel()

	t.Run("pm route", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "true"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{"alice"}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}
		resp, ok := bus.lastCommandResponse()
		if !ok {
			t.Fatal("expected plugin response for PM route")
		}
		if got, want := resp, "piss contest is a public-only command, run it in chat"; got != want {
			t.Fatalf("PM response = %q, want %q", got, want)
		}
	})

	t.Run("missing args", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}
		msg, ok := bus.lastRawMessage()
		if !ok {
			t.Fatal("expected public message")
		}
		want := "gotta challenge someone mate - !piss <amount> <username> or !piss <username> for bragging rights"
		if msg != want {
			t.Fatalf("public message = %q, want %q", msg, want)
		}
	})

	t.Run("negative bet", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{"-5", "bob"}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}
		msg, ok := bus.lastRawMessage()
		if !ok {
			t.Fatal("expected public message")
		}
		want := "can't bet negative money"
		if msg != want {
			t.Fatalf("public message = %q, want %q", msg, want)
		}
	})

	t.Run("self target", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{"alice"}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}
		msg, ok := bus.lastRawMessage()
		if !ok {
			t.Fatal("expected public message")
		}
		want := "can't piss against yaself"
		if msg != want {
			t.Fatalf("public message = %q, want %q", msg, want)
		}
	})

	t.Run("bot target", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{"botman"}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}
		messages := bus.rawMessages()
		foundAcceptance := false
		for _, msg := range messages {
			if strings.Contains(msg, "Dazza doesn't run from a challenge") {
				foundAcceptance = true
				break
			}
		}
		if !foundAcceptance {
			t.Fatalf("expected bot auto-acceptance message, got messages = %v", messages)
		}
		if _, challenger := p.findChallengeByTarget("room", "botman"); challenger != "" {
			t.Fatalf("expected no pending challenge for bot target, got %q", challenger)
		}
	})

	t.Run("bot target resolved from core config", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{}`)
		bus.onBroadcast = configuredChannelsResponder(t, p, []coreChannelConfig{
			{Channel: "room", Username: "dazza"},
		})
		params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
		if err := p.handleCommand(makeCommandEvent("piss", []string{"[dazza]"}, params)); err != nil {
			t.Fatalf("handleCommand() error = %v", err)
		}

		messages := bus.rawMessages()
		foundAcceptance := false
		for _, msg := range messages {
			if strings.Contains(msg, "Dazza doesn't run from a challenge") {
				foundAcceptance = true
				break
			}
		}
		if !foundAcceptance {
			t.Fatalf("expected bot auto-acceptance message from resolved channel username, got messages = %v", messages)
		}
		if _, challenger := p.findChallengeByTarget("room", "dazza"); challenger != "" {
			t.Fatalf("expected no pending challenge for resolved bot target, got %q", challenger)
		}
	})
}

func TestHandleCommandCreatesChallenge(t *testing.T) {
	t.Parallel()

	p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
	params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
	if err := p.handleCommand(makeCommandEvent("piss", []string{"bob"}, params)); err != nil {
		t.Fatalf("handleCommand() error = %v", err)
	}
	ch, challenger := p.findChallengeByTarget("room", "bob")
	if ch == nil {
		t.Fatal("expected challenge to be created")
	}
	if challenger != "alice" {
		t.Fatalf("challenge key = %q, want %q", challenger, "alice")
	}
	msg, ok := bus.lastRawMessage()
	if !ok {
		t.Fatal("expected public challenge message")
	}
	if want := "-alice challenges -bob to a pissing contest for bragging rights! Type 'yes' or 'no' to respond (30s to accept)"; msg != want {
		t.Fatalf("challenge message = %q, want %q", msg, want)
	}
	cleanupChallenge(p, "room", "alice")
}

type coreChannelConfig struct {
	Channel  string
	Username string
}

func TestHandleCommandAcceptsAlias(t *testing.T) {
	p, bus := newTestPlugin(t, `{}`)
	params := map[string]string{"channel": "room", "username": "alice", "is_pm": "false"}
	if err := p.handleCommand(makeCommandEvent("pissing_contest", []string{"bob"}, params)); err != nil {
		t.Fatalf("handleCommand() error = %v", err)
	}
	ch, challenger := p.findChallengeByTarget("room", "bob")
	if ch == nil {
		t.Fatal("expected challenge to be created with alias")
	}
	if challenger != "alice" {
		t.Fatalf("challenge key = %q, want %q", challenger, "alice")
	}
	msg, ok := bus.lastRawMessage()
	if !ok {
		t.Fatal("expected public challenge message")
	}
	if want := "-alice challenges -bob to a pissing contest for bragging rights! Type 'yes' or 'no' to respond (30s to accept)"; msg != want {
		t.Fatalf("challenge message = %q, want %q", msg, want)
	}
	cleanupChallenge(p, "room", "alice")
}

func mustConfiguredChannelsResponse(t *testing.T, channels []coreChannelConfig) *framework.EventData {
	t.Helper()

	rawJSON, err := json.Marshal(struct {
		Channels []coreChannelConfig `json:"channels"`
	}{Channels: channels})

	if err != nil {
		t.Fatalf("marshal configured channels response: %v", err)
	}

	return &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: rawJSON,
			},
		},
	}
}

func configuredChannelsResponder(t *testing.T, p *Plugin, channels []coreChannelConfig) func(string, *framework.EventData) {
	t.Helper()

	return func(eventType string, data *framework.EventData) {
		if eventType != "plugin.request" || data == nil || data.PluginRequest == nil {
			return
		}
		if data.PluginRequest.Type != "get_configured_channels" {
			return
		}

		resp := mustConfiguredChannelsResponse(t, channels)
		if resp.PluginResponse != nil {
			resp.PluginResponse.ID = data.PluginRequest.ID
		}
		if err := p.handlePluginResponse(framework.NewDataEvent("plugin.response.pissingcontest", resp)); err != nil {
			t.Errorf("mock plugin response error: %v", err)
		}
	}
}

func TestCreateChallengeBalanceChecks(t *testing.T) {
	t.Run("insufficient_funds", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{}`)
		bus.requestResp = mustGetBalanceResponse(t, "room", "alice", 5)

		_, err := p.createChallenge("room", "alice", "bob", 20)
		if err == nil {
			t.Fatal("expected insufficient funds error")
		}
		if got := err.Error(); got != "ya need $20 to piss mate, you've only got $5" {
			t.Fatalf("unexpected error = %q", got)
		}
	})

	t.Run("balance_lookup_error", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{}`)
		bus.requestErr = errors.New("wallet down")

		_, err := p.createChallenge("room", "alice", "bob", 20)
		if err == nil {
			t.Fatal("expected wallet lookup error")
		}
		if got := err.Error(); got != "wallet is flaky right now, try again later" {
			t.Fatalf("unexpected error = %q", got)
		}
	})
}

func TestHandleAcceptChallengeCooldownAndInsufficientFunds(t *testing.T) {
	t.Run("challenged_cooldown_blocks_accept", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{}`)
		ch := &activeChallenge{
			Room:       "room",
			Challenger: "alice",
			Challenged: "bob",
			Status:     activeChallengeStatus,
		}
		p.challenges["room"] = map[string]*activeChallenge{"alice": ch}
		p.setCooldown("room", "bob")

		p.handleAcceptChallenge("alice", "room", "bob")

		msg, ok := bus.lastRawMessage()
		if !ok {
			t.Fatal("expected cooldown message")
		}
		if !strings.Contains(msg, "still shaking it off mate, wait") {
			t.Fatalf("unexpected cooldown message = %q", msg)
		}
		if _, challenger := p.findChallengeByTarget("room", "bob"); challenger != "" {
			t.Fatalf("challenge should be removed on cooldown failure, got key %q", challenger)
		}
	})

	t.Run("challenged_insufficient_funds_drops_challenge", func(t *testing.T) {
		p, bus := newTestPlugin(t, `{}`)
		ch := &activeChallenge{
			Room:       "room",
			Challenger: "alice",
			Challenged: "bob",
			Amount:     25,
			Status:     activeChallengeStatus,
		}
		p.challenges["room"] = map[string]*activeChallenge{"alice": ch}
		bus.requestResp = mustGetBalanceResponse(t, "room", "bob", 8)

		p.handleAcceptChallenge("alice", "room", "bob")

		messages := bus.rawMessages()
		if len(messages) < 2 {
			t.Fatalf("expected two public messages, got %d", len(messages))
		}
		if want := "-bob tried to accept but got broke"; messages[len(messages)-2] != want {
			t.Fatalf("first insufficient funds message = %q, want %q", messages[len(messages)-2], want)
		}
		if want := "ya need $25 to accept mate, you've only got $8"; messages[len(messages)-1] != want {
			t.Fatalf("second insufficient funds message = %q, want %q", messages[len(messages)-1], want)
		}
		if _, challenger := p.findChallengeByTarget("room", "bob"); challenger != "" {
			t.Fatalf("challenge should be removed when funds are insufficient, got key %q", challenger)
		}
	})
}

func TestHandleFailureOutcomeTransfersPayout(t *testing.T) {
	p, bus := newTestPlugin(t, `{}`)
	bus.requestResp = &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			Success: true,
		},
	}
	ch := &activeChallenge{
		Room:       "room",
		Amount:     15,
		Challenger: "alice",
		Challenged: "bob",
	}

	p.handleFailureOutcome(ch, "alice", "bob", "failed the aim")
	requests := bus.requestCalls()
	if len(requests) != 1 {
		t.Fatalf("expected one transfer request, got %d", len(requests))
	}
	if requests[0].eventType != "plugin.request" {
		t.Fatalf("expected plugin.request call, got %s", requests[0].eventType)
	}
	if requests[0].target != "economy" {
		t.Fatalf("expected economy target, got %s", requests[0].target)
	}
	if requests[0].data == nil || requests[0].data.PluginRequest == nil || requests[0].data.PluginRequest.Data == nil {
		t.Fatal("expected economy transfer request payload")
	}

	var transferReq framework.TransferRequest
	if err := json.Unmarshal(requests[0].data.PluginRequest.Data.RawJSON, &transferReq); err != nil {
		t.Fatalf("decode transfer request: %v", err)
	}
	if transferReq.FromUsername != "bob" || transferReq.ToUsername != "alice" || transferReq.Amount != 15 || transferReq.Reason != "pissing_contest" {
		t.Fatalf("unexpected transfer request: %#v", transferReq)
	}
	msg, ok := bus.lastRawMessage()
	if !ok {
		t.Fatal("expected payout message")
	}
	if msg != "💰 -alice wins $15 from -bob!" {
		t.Fatalf("unexpected payout message = %q", msg)
	}
}

func TestCreateChallengeCooldown(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	p.cooldown = 20 * time.Millisecond
	p.setCooldown("room", "alice")
	if _, err := p.createChallenge("room", "alice", "bob", 0); err == nil {
		t.Fatal("expected cooldown error")
	}
}

func TestCreateChallengeDuplicate(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	challenge, err := p.createChallenge("room", "alice", "bob", 0)
	if err != nil {
		t.Fatalf("createChallenge() unexpected error = %v", err)
	}
	_ = challenge
	defer cleanupChallenge(p, "room", "alice")

	if _, err := p.createChallenge("room", "alice", "charlie", 0); err == nil {
		t.Fatalf("expected duplicate challenge error, got nil")
	}
}

func TestHandleDeclineChallenge(t *testing.T) {
	t.Parallel()

	p, bus := newTestPlugin(t, `{"bot_username":"botman"}`)
	ch, err := p.createChallenge("room", "alice", "bob", 0)
	if err != nil {
		t.Fatalf("createChallenge() unexpected error = %v", err)
	}
	defer cleanupChallenge(p, "room", "alice")

	p.handleDeclineChallenge("alice", "room", "bob")
	msg, ok := bus.lastRawMessage()
	if !ok {
		t.Fatal("expected public decline message")
	}
	if want := "-bob pussied out! Kept it in their pants like a coward"; msg != want {
		t.Fatalf("decline message = %q, want %q", msg, want)
	}
	remaining, _ := p.findChallengeByTarget("room", "bob")
	if remaining != nil {
		t.Fatal("challenge should be removed after decline")
	}
	if ch.Timer != nil {
		ch.Timer.Stop()
	}
}

func TestEvaluateContestFailureFromForfeitChance(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	ch := &activeChallenge{
		Challenger: "alice",
		Challenged: "bob",
		Room:       "room",
	}
	ch.ChallengerMods.forfeitChance = 1
	ch.ChallengerScore = 100
	ch.ChallengedScore = 100

	challengerFail, challengedFail, _, _ := p.evaluateContestFailure(ch)
	if !challengerFail {
		t.Fatal("expected challenger to fail from forfeit chance")
	}
	if challengedFail {
		t.Fatal("expected challenged to not fail from forfeit chance")
	}
}

func TestEvaluateContestFailureChallengedFromForfeitChance(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	ch := &activeChallenge{
		Challenger: "alice",
		Challenged: "bob",
		Room:       "room",
	}
	ch.ChallengedMods.forfeitChance = 1
	ch.ChallengerScore = 100
	ch.ChallengedScore = 100

	challengerFail, challengedFail, _, _ := p.evaluateContestFailure(ch)
	if challengerFail {
		t.Fatal("expected challenger to remain clear from challenged forfeit chance")
	}
	if !challengedFail {
		t.Fatal("expected challenged to fail from forfeit chance")
	}
}

func TestEvaluateContestFailureShortCircuitsOnConditionFailure(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	ch := &activeChallenge{
		Challenger: "alice",
		Challenged: "bob",
		Room:       "room",
		ChallengerCond: condition{
			Type:    "failure",
			Name:    "Tripwire",
			Message: "stepped on a tripwire",
		},
		ChallengedCond: condition{
			Name:    "Bad Air",
			Type:    "boon",
			Message: "",
		},
		ChallengerMods: contestModifiers{
			wrongWay:       true,
			randomFailures: true,
			forfeitChance:  1,
		},
		ChallengedMods: contestModifiers{
			randomFailures: true,
			forfeitChance:  1,
		},
		ChallengerScore: 100,
		ChallengedScore: 100,
	}

	challengerFail, challengedFail, challengerMsg, challengedMsg := p.evaluateContestFailure(ch)
	if !challengerFail {
		t.Fatal("expected challenger to fail from condition failure")
	}
	if challengedFail {
		t.Fatal("expected challenged to remain clear when challenger fails from condition failure")
	}
	if challengerMsg != "stepped on a tripwire" {
		t.Fatalf("expected challenger message %q, got %q", "stepped on a tripwire", challengerMsg)
	}
	if challengedMsg != "" {
		t.Fatalf("expected challenged message to be empty, got %q", challengedMsg)
	}
}

func TestEvaluateContestFailureFromWeatherEvent(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	ch := &activeChallenge{
		Challenger:    "alice",
		Challenged:    "bob",
		Room:          "room",
		weatherEvents: []contestEvent{{Message: "got hit by hail", Forfeit: true}},
	}

	challengerFail, challengedFail, challengerMessage, challengedMessage := p.evaluateContestFailure(ch)
	if !challengerFail {
		t.Fatal("expected challenger failure from event")
	}
	if !challengedFail {
		t.Fatal("expected challenged failure from event")
	}
	if challengerMessage != "got hit by hail" {
		t.Fatalf("expected challenger event message = %q, got %q", "got hit by hail", challengerMessage)
	}
	if challengedMessage != "got hit by hail" {
		t.Fatalf("expected challenged event message = %q, got %q", "got hit by hail", challengedMessage)
	}
}

func TestRecordOutcomeCooldown(t *testing.T) {
	t.Parallel()

	p, _ := newTestPlugin(t, `{}`)
	ch := &activeChallenge{
		Room:       "room",
		Challenger: "alice",
		Challenged: "bob",
	}

	if err := p.assertCooldownReady("room", "alice"); err != nil {
		t.Fatalf("expected challenger cooldown to be clear before outcome: %v", err)
	}
	if err := p.assertCooldownReady("room", "bob"); err != nil {
		t.Fatalf("expected challenged cooldown to be clear before outcome: %v", err)
	}

	p.recordOutcome(ch, "alice", "bob", false)

	if err := p.assertCooldownReady("room", "alice"); err != nil {
		t.Fatalf("challenger cooldown should remain clear when no bladder drain occurs: %v", err)
	}
	if err := p.assertCooldownReady("room", "bob"); err != nil {
		t.Fatalf("challenged cooldown should remain clear when no bladder drain occurs: %v", err)
	}

	p.recordOutcome(ch, "alice", "bob", true)

	if err := p.assertCooldownReady("room", "alice"); err == nil {
		t.Fatal("expected challenger cooldown to be set when bladder is drained")
	}
	if err := p.assertCooldownReady("room", "bob"); err == nil {
		t.Fatal("expected challenged cooldown to be set when bladder is drained")
	}
}

func TestApplyConditionIgnoresDebuffsWhenImmune(t *testing.T) {
	t.Parallel()

	stats := contestResult{distance: 100, volume: 100, aim: 100, duration: 10}
	cond := condition{Type: "debuff", Effects: map[string]float64{"aim": -50, "distance": -50}}
	mods := contestModifiers{ignoreDebuffs: true}

	applyCondition(&stats, cond, nil, &mods, nil)

	if stats.aim != 100 || stats.distance != 100 {
		t.Fatalf("debuff should be ignored, got aim=%f distance=%f", stats.aim, stats.distance)
	}
}

func TestApplyFinesChargesContestantsOnFullChance(t *testing.T) {
	bus := &mockPissContestBus{
		requestResp: &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      "fine-1",
				Success: true,
			},
		},
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage(`{}`), bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ch := &activeChallenge{
		Room:       "room",
		Challenger: "alice",
		Challenged: "bob",
		ChallengerCond: condition{
			Fine:        5,
			FineMessage: "alice slipped",
			FineChance:  1,
		},
		ChallengedCond: condition{
			Fine:        4,
			FineMessage: "bob slipped",
			FineChance:  1,
		},
		Location: location{
			Fine:        2,
			FineMessage: "bathroom got flooded",
			FineChance:  1,
		},
	}

	p.applyFines(ch, "", "")

	requests := bus.requestCalls()
	if len(requests) != 2 {
		t.Fatalf("expected 2 economy debit requests, got %d", len(requests))
	}

	var first, second framework.DebitRequest
	if err := json.Unmarshal(requests[0].data.PluginRequest.Data.RawJSON, &first); err != nil {
		t.Fatalf("decode first request: %v", err)
	}
	if err := json.Unmarshal(requests[1].data.PluginRequest.Data.RawJSON, &second); err != nil {
		t.Fatalf("decode second request: %v", err)
	}

	if first.Username != "alice" || first.Amount != 7 {
		t.Fatalf("expected first request for alice fine 7, got %s %d", first.Username, first.Amount)
	}
	if second.Username != "bob" || second.Amount != 6 {
		t.Fatalf("expected second request for bob fine 6, got %s %d", second.Username, second.Amount)
	}
	if first.Reason != "pissing_contest_fine" || second.Reason != "pissing_contest_fine" {
		t.Fatalf("unexpected debit reason: %q and %q", first.Reason, second.Reason)
	}
}

func TestApplyFinesSkipsWhenNoFines(t *testing.T) {
	bus := &mockPissContestBus{
		requestResp: &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				Success: true,
			},
		},
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage(`{}`), bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ch := &activeChallenge{
		Room:       "room",
		Challenger: "alice",
		Challenged: "bob",
		ChallengerCond: condition{
			FineChance: 0.2,
		},
		ChallengedCond: condition{
			FineChance: 0.2,
		},
		Location: location{
			FineChance: 0.2,
		},
	}

	p.applyFines(ch, "", "")

	if requests := bus.requestCalls(); len(requests) != 0 {
		t.Fatalf("expected no debit calls when fines are zero, got %d", len(requests))
	}
}

func TestStartAndLifecycle(t *testing.T) {
	t.Parallel()

	p, bus := newTestPlugin(t, `{}`)

	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer p.Stop()

	if !strings.Contains(joinedSubscriptions(bus), "command.pissingcontest.execute") {
		t.Fatalf("missing subscription for command execution")
	}
	if !strings.Contains(joinedSubscriptions(bus), "cytube.event.chatMsg") {
		t.Fatalf("missing subscription for chat messages")
	}

	if err := p.Start(); err == nil {
		t.Fatal("expected second Start() to fail while running")
	}
}

func joinedSubscriptions(bus *mockPissContestBus) string {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	return strings.Join(bus.subscriptions, ",")
}

func mustGetBalanceResponse(t *testing.T, channel, username string, balance int64) *framework.EventData {
	t.Helper()
	rawJSON, err := json.Marshal(framework.GetBalanceResponse{
		Channel:  channel,
		Username: username,
		Balance:  balance,
	})
	if err != nil {
		t.Fatalf("marshal get balance response: %v", err)
	}
	return &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: rawJSON,
			},
		},
	}
}
