package needs

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
	name        string
	eventBus    framework.EventBus
	stateClient *framework.PlayerStateClient
	tracker     needsTrackerConfig
	running     bool
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

type needsTrackerConfig struct {
	EnableBuffReporting *bool `json:"enable_buff_reporting"`
	MaxEffectListSize   int   `json:"max_effect_list_size"`
}

func defaultNeedsTrackerConfig() needsTrackerConfig {
	return needsTrackerConfig{
		EnableBuffReporting: ptrBool(true),
		MaxEffectListSize:   3,
	}
}

func New() framework.Plugin {
	return &Plugin{
		name: "needs",
	}
}

func (p *Plugin) Init(rawConfig json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.stateClient = framework.NewPlayerStateClient(bus, p.name)
	p.tracker = defaultNeedsTrackerConfig()

	if len(rawConfig) > 0 {
		var cfg needsTrackerConfig
		if err := json.Unmarshal(rawConfig, &cfg); err == nil {
			p.tracker = mergeNeedsTrackerConfig(cfg)
		}
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	return nil
}

func mergeNeedsTrackerConfig(cfg needsTrackerConfig) needsTrackerConfig {
	merged := defaultNeedsTrackerConfig()
	if cfg.EnableBuffReporting != nil {
		merged.EnableBuffReporting = cfg.EnableBuffReporting
	}
	if cfg.MaxEffectListSize > 0 {
		merged.MaxEffectListSize = cfg.MaxEffectListSize
	}
	return merged
}

func ptrBool(v bool) *bool {
	return &v
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.mu.Unlock()

	if err := p.eventBus.Subscribe("command.needs.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.needs.execute: %w", err)
	}

	p.registerCommand()

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

func (p *Plugin) registerCommand() {
	regEvent := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			To:   "eventfilter",
			From: p.name,
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands":    "needs",
					"min_rank":    "0",
					"description": "check your current needs",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register command: %v", err)
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

	cmd := req.Data.Command
	channel := strings.TrimSpace(cmd.Params["channel"])
	requester := strings.TrimSpace(cmd.Params["username"])
	if channel == "" || requester == "" {
		return nil
	}

	target := requester
	if len(cmd.Args) > 0 {
		target = strings.TrimPrefix(strings.TrimSpace(cmd.Args[0]), "@")
		if target == "" {
			target = requester
		}
	}

	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	state, err := p.stateClient.Get(ctx, channel, target)
	if err != nil {
		logger.Error(p.name, "Failed to get player state for %q: %v", target, err)
		p.sendResponse(requester, channel, fmt.Sprintf("couldn't load needs for %s", target))
		return nil
	}

	var buffs, debuffs []string
	if p.tracker.EnableBuffReporting != nil && *p.tracker.EnableBuffReporting {
		var err error
		buffs, debuffs, err = p.fetchTrackerEffects(ctx, channel, target)
		if err != nil {
			logger.Debug(p.name, "Failed to load buffs/debuffs for %q: %v", target, err)
		}
	}

	message := formatNeedsMessage(
		target,
		state,
		buffs,
		debuffs,
		p.tracker.EnableBuffReporting != nil && *p.tracker.EnableBuffReporting,
		p.tracker.MaxEffectListSize,
	)
	p.sendResponse(requester, channel, message)

	return nil
}

const maxNeedValue int64 = 100
const needsProgressBarWidth = 8
const needsProgressBarFill = "█"
const needsProgressBarEmpty = "░"

var hungerTiers = []string{
	"Stuffed like a corpse",
	"Satisfied",
	"Little hungry",
	"Getting peckish",
	"Properly hungry",
	"Ham-fisted hunger",
	"I can smell food in the room",
	"Your tongue is making prayers",
	"Ravenous",
	"Trespassingly starving",
}

var drunkTiers = []string{
	"Sane as a nun",
	"Just warm, barely",
	"Tipsy",
	"Lit",
	"Already loud",
	"Half-thorough",
	"Pretty hammered",
	"Blindly dancing",
	"Seasick from your own piss",
	"Absolutely coked out",
}

var highTiers = []string{
	"Clear headed",
	"Breezy",
	"Slightly floaty",
	"Head in the clouds",
	"Cloudy",
	"Fully glazed",
	"High and horny",
	"Lost in your own cinema",
	"Spacey and loud",
	"Too far gone to focus",
}

var lustTiers = []string{
	"Chaste and polite",
	"Curious",
	"Feeling your vibe",
	"Already thinking about it",
	"On the edge",
	"Needing attention",
	"Getting needy",
	"Riding hot currents",
	"Unreasonably turn-on-able",
	"Ready to break the internet",
}

var bladderTiers = []string{
	"Dry as dust",
	"Comfortably in control",
	"Lightly full",
	"Noticeably occupied",
	"You feel it",
	"Needs a run",
	"Emergency mindset",
	"One bad mistake away",
	"Full and frantic",
	"Fuller than a water balloon",
}

func formatNeedsMessage(player string, state framework.PlayerState, buffs []string, debuffs []string, showBuffs bool, maxEffectListSize int) string {
	hunger := clampNeed(state.Food)
	drunk := clampNeed(state.Alcohol)
	high := clampNeed(state.Weed)
	horny := clampNeed(state.Lust)
	bladder := clampNeed(state.Bladder)

	sections := []string{
		formatNeedLine("🍽", "Hunger", hunger, needTier(hunger, hungerTiers)),
		formatNeedLine("🍺", "Drunk", drunk, needTier(drunk, drunkTiers)),
		formatNeedLine("🍃", "High", high, needTier(high, highTiers)),
		formatNeedLine("🌶", "Horny", horny, needTier(horny, lustTiers)),
		formatNeedLine("🚽", "Bladder", bladder, needTier(bladder, bladderTiers)),
	}
	base := fmt.Sprintf("%s: %s", player, strings.Join(sections, " || "))

	if !showBuffs {
		return base
	}

	if maxEffectListSize <= 0 {
		maxEffectListSize = 1
	}

	buffText := formatEffectList("none", buffs, maxEffectListSize)
	debuffText := formatEffectList("none", debuffs, maxEffectListSize)
	return fmt.Sprintf("%s || Buffs: %s || Debuffs: %s", base, buffText, debuffText)
}

func formatNeedLine(icon, label string, value int64, tier string) string {
	labelWithColon := label + ":"
	return fmt.Sprintf(
		"%s %s %s (%s)",
		icon, labelWithColon, renderNeedBar(value), tier,
	)
}

func renderNeedBar(value int64) string {
	filled := int(value * needsProgressBarWidth / maxNeedValue)
	if value%maxNeedValue != 0 && value*needsProgressBarWidth%maxNeedValue*2 >= maxNeedValue {
		filled++
	}
	if filled > needsProgressBarWidth {
		filled = needsProgressBarWidth
	}
	if filled < 0 {
		filled = 0
	}

	return fmt.Sprintf(
		"[%s%s]",
		strings.Repeat(needsProgressBarFill, filled),
		strings.Repeat(needsProgressBarEmpty, needsProgressBarWidth-filled),
	)
}

func clampNeed(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > maxNeedValue {
		return maxNeedValue
	}
	return value
}

func needTier(value int64, labels []string) string {
	idx := 0
	switch {
	case value >= maxNeedValue:
		idx = len(labels) - 1
	default:
		idx = int(value / 10)
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(labels) {
		idx = len(labels) - 1
	}
	return labels[idx]
}

func (p *Plugin) fetchTrackerEffects(ctx context.Context, channel, username string) ([]string, []string, error) {
	payload, err := json.Marshal(map[string]string{
		"channel":  channel,
		"username": username,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal tracker payload: %w", err)
	}

	eventData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   fmt.Sprintf("needs-%d", time.Now().UnixNano()),
			From: p.name,
			To:   "bufftracker",
			Type: "bufftracker.list",
			Data: &framework.RequestData{
				RawJSON: payload,
			},
		},
	}

	helper := framework.NewRequestHelper(p.eventBus, p.name)
	response, err := helper.RequestWithConfig(ctx, "bufftracker", "plugin.request", eventData, "fast")
	if err != nil {
		return nil, nil, fmt.Errorf("tracker request failed: %w", err)
	}
	if response == nil || response.PluginResponse == nil {
		return nil, nil, fmt.Errorf("tracker response is empty")
	}
	pluginResp := response.PluginResponse
	if !pluginResp.Success {
		return nil, nil, fmt.Errorf("tracker returned error: %s", pluginResp.Error)
	}
	if pluginResp.Data == nil || len(pluginResp.Data.RawJSON) == 0 {
		return nil, nil, nil
	}

	return parseTrackerEffects(pluginResp.Data.RawJSON)
}

type buffTrackerEffect struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Label    string `json:"label"`
	ID       string `json:"id"`
	Effect   string `json:"effect"`
	Category string `json:"category"`
}

type buffTrackerResponse struct {
	Buffs   []buffTrackerEffect `json:"buffs"`
	Debuffs []buffTrackerEffect `json:"debuffs"`
	Effects []buffTrackerEffect `json:"effects"`
}

func parseTrackerEffects(raw json.RawMessage) ([]string, []string, error) {
	var parsed buffTrackerResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		var list []buffTrackerEffect
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, nil, fmt.Errorf("unmarshal tracker effects: %w", err)
		}
		var buffs, debuffs []string
		for _, effect := range list {
			if strings.EqualFold(effect.Type, "debuff") {
				debuffs = append(debuffs, normalizedEffectName(effect))
				continue
			}
			buffs = append(buffs, normalizedEffectName(effect))
		}
		return uniqueSortedStrings(buffs), uniqueSortedStrings(debuffs), nil
	}

	buffs := make([]string, 0, len(parsed.Buffs))
	for _, effect := range parsed.Buffs {
		buffs = append(buffs, normalizedEffectName(effect))
	}

	debuffs := make([]string, 0, len(parsed.Debuffs))
	for _, effect := range parsed.Debuffs {
		debuffs = append(debuffs, normalizedEffectName(effect))
	}

	for _, effect := range parsed.Effects {
		if strings.EqualFold(effect.Type, "debuff") {
			debuffs = append(debuffs, normalizedEffectName(effect))
			continue
		}
		buffs = append(buffs, normalizedEffectName(effect))
	}

	return uniqueSortedStrings(buffs), uniqueSortedStrings(debuffs), nil
}

func normalizedEffectName(effect buffTrackerEffect) string {
	switch {
	case strings.TrimSpace(effect.Name) != "":
		return strings.TrimSpace(effect.Name)
	case strings.TrimSpace(effect.Label) != "":
		return strings.TrimSpace(effect.Label)
	case strings.TrimSpace(effect.Effect) != "":
		return strings.TrimSpace(effect.Effect)
	case strings.TrimSpace(effect.ID) != "":
		return strings.TrimSpace(effect.ID)
	default:
		return "unknown"
	}
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		norm := strings.ToLower(strings.TrimSpace(value))
		if norm == "" {
			continue
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		deduped = append(deduped, value)
	}
	sort.Strings(deduped)
	return deduped
}

func formatEffectList(fallback string, effects []string, maxCount int) string {
	if len(effects) == 0 {
		return fallback
	}
	if maxCount <= 0 || len(effects) <= maxCount {
		return strings.Join(effects, ", ")
	}

	displayed := effects[:maxCount]
	extra := len(effects) - maxCount
	return fmt.Sprintf("%s +%d more", strings.Join(displayed, ", "), extra)
}

func (p *Plugin) sendResponse(username, channel, message string) {
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

	if err := p.eventBus.Broadcast("plugin.response", response); err != nil {
		logger.Error(p.name, "Failed to send response: %v", err)
	}
}
