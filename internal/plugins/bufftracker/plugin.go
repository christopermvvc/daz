package bufftracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName = "bufftracker"

	stateInitialized = "initialized"
	stateRunning     = "running"
	stateStopped     = "stopped"

	operationList   = "bufftracker.list"
	operationApply  = "bufftracker.apply"
	operationRemove = "bufftracker.remove"
	operationClear  = "bufftracker.clear"

	effectTypeBuff   = "buff"
	effectTypeDebuff = "debuff"

	errorCodeInvalidArgument = "INVALID_ARGUMENT"
	errorCodeInternal        = "INTERNAL"
)

type Plugin struct {
	mu sync.RWMutex

	eventBus framework.EventBus
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	status   framework.PluginStatus

	config pluginConfig

	buffDefinitions   map[string]effectDefinition
	debuffDefinitions map[string]effectDefinition
	buffLookup        map[string]string
	debuffLookup      map[string]string

	activeEffects map[playerKey]*playerEffects
}

type pluginConfig struct {
	BuffDictionaryPath   string `json:"buff_dictionary_path"`
	DebuffDictionaryPath string `json:"debuff_dictionary_path"`
}

type effectDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

type effectView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Type  string `json:"type"`
}

type listRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
}

type mutateRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
	Type     string `json:"type"`
	EffectID string `json:"effect_id"`
	Effect   string `json:"effect"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

type clearRequest struct {
	Channel  string `json:"channel"`
	Username string `json:"username"`
	Type     string `json:"type"`
}

type listResponse struct {
	Channel  string       `json:"channel"`
	Username string       `json:"username"`
	Buffs    []effectView `json:"buffs"`
	Debuffs  []effectView `json:"debuffs"`
	Effects  []effectView `json:"effects"`
}

type playerKey struct {
	channel  string
	username string
}

type playerEffects struct {
	buffs   map[string]struct{}
	debuffs map[string]struct{}
}

var defaultBuffDefinitions = []effectDefinition{
	{
		ID:          "lucky",
		Name:        "Lucky",
		Description: "Things keep breaking your way.",
		Aliases:     []string{"luck"},
	},
	{
		ID:          "tough_skin",
		Name:        "Tough Skin",
		Description: "Harder to shake mentally.",
		Aliases:     []string{"tough"},
	},
}

var defaultDebuffDefinitions = []effectDefinition{
	{
		ID:          "std",
		Name:        "STD",
		Description: "Lingering consequences from bad decisions.",
		Aliases:     []string{"sti"},
	},
	{
		ID:          "clumsy",
		Name:        "Clumsy",
		Description: "Awkward timing and sloppy execution.",
		Aliases:     []string{"sloppy"},
	},
}

func New() framework.Plugin {
	return &Plugin{
		status: framework.PluginStatus{
			Name:  pluginName,
			State: stateInitialized,
		},
	}
}

func (p *Plugin) Name() string {
	return pluginName
}

func (p *Plugin) Init(rawConfig json.RawMessage, bus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.eventBus = bus
	p.config = defaultConfig()
	p.activeEffects = make(map[playerKey]*playerEffects)
	p.status = framework.PluginStatus{Name: pluginName, State: stateInitialized}

	if len(rawConfig) > 0 {
		var cfg pluginConfig
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return fmt.Errorf("parse bufftracker config: %w", err)
		}
		if strings.TrimSpace(cfg.BuffDictionaryPath) != "" {
			p.config.BuffDictionaryPath = strings.TrimSpace(cfg.BuffDictionaryPath)
		}
		if strings.TrimSpace(cfg.DebuffDictionaryPath) != "" {
			p.config.DebuffDictionaryPath = strings.TrimSpace(cfg.DebuffDictionaryPath)
		}
	}

	buffDefs, buffLookup, err := loadDefinitions(p.config.BuffDictionaryPath, defaultBuffDefinitions, effectTypeBuff)
	if err != nil {
		return fmt.Errorf("load buff dictionary: %w", err)
	}
	debuffDefs, debuffLookup, err := loadDefinitions(p.config.DebuffDictionaryPath, defaultDebuffDefinitions, effectTypeDebuff)
	if err != nil {
		return fmt.Errorf("load debuff dictionary: %w", err)
	}

	p.buffDefinitions = buffDefs
	p.debuffDefinitions = debuffDefs
	p.buffLookup = buffLookup
	p.debuffLookup = debuffLookup

	p.ctx, p.cancel = context.WithCancel(context.Background())
	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	if err := p.eventBus.Subscribe(eventbus.EventPluginRequest, p.handlePluginRequest); err != nil {
		return fmt.Errorf("subscribe plugin.request: %w", err)
	}

	p.running = true
	p.status.State = stateRunning
	return nil
}

func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		p.status.State = stateStopped
		return nil
	}

	p.running = false
	p.status.State = stateStopped
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
	return p.status
}

func (p *Plugin) handlePluginRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req.To != pluginName || req.ID == "" {
		return nil
	}

	switch req.Type {
	case operationList:
		p.handleList(req)
	case operationApply:
		p.handleApply(req)
	case operationRemove:
		p.handleRemove(req)
	case operationClear:
		p.handleClear(req)
	default:
		p.deliverError(req, errorCodeInvalidArgument, "unknown operation", map[string]interface{}{"operation": req.Type})
	}

	return nil
}

func (p *Plugin) handleList(req *framework.PluginRequest) {
	var payload listRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	channel := normalizeChannel(payload.Channel)
	username := normalizeUsername(payload.Username)
	if channel == "" || username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel or username", nil)
		return
	}

	p.deliverSuccess(req, p.snapshotList(channel, username))
}

func (p *Plugin) handleApply(req *framework.PluginRequest) {
	var payload mutateRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	channel := normalizeChannel(payload.Channel)
	username := normalizeUsername(payload.Username)
	if channel == "" || username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel or username", nil)
		return
	}

	rawEffect := firstNonEmpty(payload.EffectID, payload.Effect, payload.ID, payload.Name)
	effectType, effectID, err := p.resolveEffect(payload.Type, rawEffect)
	if err != nil {
		p.deliverError(req, errorCodeInvalidArgument, err.Error(), nil)
		return
	}

	p.mu.Lock()
	key := playerKey{channel: channel, username: username}
	current := p.activeEffects[key]
	if current == nil {
		current = &playerEffects{
			buffs:   make(map[string]struct{}),
			debuffs: make(map[string]struct{}),
		}
		p.activeEffects[key] = current
	}
	if effectType == effectTypeDebuff {
		current.debuffs[effectID] = struct{}{}
	} else {
		current.buffs[effectID] = struct{}{}
	}
	p.mu.Unlock()

	p.deliverSuccess(req, p.snapshotList(channel, username))
}

func (p *Plugin) handleRemove(req *framework.PluginRequest) {
	var payload mutateRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	channel := normalizeChannel(payload.Channel)
	username := normalizeUsername(payload.Username)
	if channel == "" || username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel or username", nil)
		return
	}

	rawEffect := firstNonEmpty(payload.EffectID, payload.Effect, payload.ID, payload.Name)
	effectType, effectID, err := p.resolveEffect(payload.Type, rawEffect)
	if err != nil {
		p.deliverError(req, errorCodeInvalidArgument, err.Error(), nil)
		return
	}

	p.mu.Lock()
	key := playerKey{channel: channel, username: username}
	current := p.activeEffects[key]
	if current != nil {
		if effectType == effectTypeDebuff {
			delete(current.debuffs, effectID)
		} else {
			delete(current.buffs, effectID)
		}
		if len(current.buffs) == 0 && len(current.debuffs) == 0 {
			delete(p.activeEffects, key)
		}
	}
	p.mu.Unlock()

	p.deliverSuccess(req, p.snapshotList(channel, username))
}

func (p *Plugin) handleClear(req *framework.PluginRequest) {
	var payload clearRequest
	if !p.parseRequest(req, &payload) {
		return
	}

	channel := normalizeChannel(payload.Channel)
	username := normalizeUsername(payload.Username)
	if channel == "" || username == "" {
		p.deliverError(req, errorCodeInvalidArgument, "missing field channel or username", nil)
		return
	}

	clearType := normalizeToken(payload.Type)
	if clearType != "" && clearType != effectTypeBuff && clearType != effectTypeDebuff {
		p.deliverError(req, errorCodeInvalidArgument, "type must be buff or debuff", nil)
		return
	}

	p.mu.Lock()
	key := playerKey{channel: channel, username: username}
	switch clearType {
	case "":
		delete(p.activeEffects, key)
	case effectTypeBuff:
		if current := p.activeEffects[key]; current != nil {
			current.buffs = make(map[string]struct{})
			if len(current.debuffs) == 0 {
				delete(p.activeEffects, key)
			}
		}
	case effectTypeDebuff:
		if current := p.activeEffects[key]; current != nil {
			current.debuffs = make(map[string]struct{})
			if len(current.buffs) == 0 {
				delete(p.activeEffects, key)
			}
		}
	}
	p.mu.Unlock()

	p.deliverSuccess(req, p.snapshotList(channel, username))
}

func (p *Plugin) parseRequest(req *framework.PluginRequest, target interface{}) bool {
	if req.Data == nil || len(req.Data.RawJSON) == 0 {
		p.deliverError(req, errorCodeInvalidArgument, "missing data.raw_json", nil)
		return false
	}
	if err := json.Unmarshal(req.Data.RawJSON, target); err != nil {
		p.deliverError(req, errorCodeInvalidArgument, "invalid request payload", nil)
		return false
	}
	return true
}

func (p *Plugin) resolveEffect(rawType, rawEffect string) (string, string, error) {
	effect := normalizeToken(rawEffect)
	if effect == "" {
		return "", "", fmt.Errorf("missing effect identifier")
	}

	switch normalizeToken(rawType) {
	case "":
		buffID, hasBuff := p.buffLookup[effect]
		debuffID, hasDebuff := p.debuffLookup[effect]

		switch {
		case hasBuff && !hasDebuff:
			return effectTypeBuff, buffID, nil
		case !hasBuff && hasDebuff:
			return effectTypeDebuff, debuffID, nil
		case hasBuff && hasDebuff:
			if buffID == debuffID {
				return effectTypeBuff, buffID, nil
			}
			return "", "", fmt.Errorf("effect is ambiguous; specify type")
		default:
			return "", "", fmt.Errorf("unknown effect")
		}
	case effectTypeBuff:
		id, ok := p.buffLookup[effect]
		if !ok {
			return "", "", fmt.Errorf("unknown buff effect")
		}
		return effectTypeBuff, id, nil
	case effectTypeDebuff:
		id, ok := p.debuffLookup[effect]
		if !ok {
			return "", "", fmt.Errorf("unknown debuff effect")
		}
		return effectTypeDebuff, id, nil
	default:
		return "", "", fmt.Errorf("type must be buff or debuff")
	}
}

func (p *Plugin) snapshotList(channel, username string) listResponse {
	p.mu.RLock()
	key := playerKey{channel: channel, username: username}
	current := p.activeEffects[key]

	var buffIDs []string
	var debuffIDs []string
	if current != nil {
		buffIDs = mapKeys(current.buffs)
		debuffIDs = mapKeys(current.debuffs)
	}
	p.mu.RUnlock()

	buffs := p.toEffectViews(buffIDs, p.buffDefinitions, effectTypeBuff)
	debuffs := p.toEffectViews(debuffIDs, p.debuffDefinitions, effectTypeDebuff)
	effects := append(make([]effectView, 0, len(buffs)+len(debuffs)), buffs...)
	effects = append(effects, debuffs...)

	return listResponse{
		Channel:  channel,
		Username: username,
		Buffs:    buffs,
		Debuffs:  debuffs,
		Effects:  effects,
	}
}

func (p *Plugin) toEffectViews(ids []string, dictionary map[string]effectDefinition, effectType string) []effectView {
	if len(ids) == 0 {
		return nil
	}

	views := make([]effectView, 0, len(ids))
	for _, id := range ids {
		def, ok := dictionary[id]
		if !ok {
			views = append(views, effectView{
				ID:   id,
				Name: displayNameFromID(id),
				Type: effectType,
			})
			continue
		}
		name := strings.TrimSpace(def.Name)
		if name == "" {
			name = displayNameFromID(def.ID)
		}
		views = append(views, effectView{
			ID:    def.ID,
			Name:  name,
			Label: strings.TrimSpace(def.Label),
			Type:  effectType,
		})
	}

	sort.SliceStable(views, func(i, j int) bool {
		left := strings.ToLower(views[i].Name)
		right := strings.ToLower(views[j].Name)
		if left == right {
			return views[i].ID < views[j].ID
		}
		return left < right
	})

	return views
}

func (p *Plugin) deliverSuccess(req *framework.PluginRequest, payload interface{}) {
	raw, err := json.Marshal(payload)
	if err != nil {
		p.deliverError(req, errorCodeInternal, "failed to marshal response payload", nil)
		return
	}

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    pluginName,
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: raw,
			},
		},
	}
	p.eventBus.DeliverResponse(req.ID, response, nil)
}

func (p *Plugin) deliverError(req *framework.PluginRequest, code, message string, details map[string]interface{}) {
	errorPayload := map[string]interface{}{
		"error_code": code,
		"message":    message,
	}
	if details != nil {
		errorPayload["details"] = details
	}

	raw, err := json.Marshal(errorPayload)
	if err != nil {
		raw = []byte(`{"error_code":"INTERNAL","message":"failed to marshal error payload"}`)
		message = "failed to marshal error payload"
	}

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    pluginName,
			Success: false,
			Error:   message,
			Data: &framework.ResponseData{
				RawJSON: raw,
			},
		},
	}
	p.eventBus.DeliverResponse(req.ID, response, nil)
}

func defaultConfig() pluginConfig {
	return pluginConfig{
		BuffDictionaryPath:   "internal/plugins/bufftracker/dictionaries/buffs.json",
		DebuffDictionaryPath: "internal/plugins/bufftracker/dictionaries/debuffs.json",
	}
}

func loadDefinitions(path string, fallback []effectDefinition, fallbackType string) (map[string]effectDefinition, map[string]string, error) {
	definitions := fallback
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath != "" {
		raw, err := os.ReadFile(trimmedPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("read dictionary %s: %w", trimmedPath, err)
			}
			logger.Warn(pluginName, "Dictionary file not found (%s), using built-in defaults", trimmedPath)
		} else if len(raw) > 0 {
			var parsed []effectDefinition
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return nil, nil, fmt.Errorf("parse dictionary %s: %w", trimmedPath, err)
			}
			definitions = parsed
		}
	}

	normalized := normalizeDefinitions(definitions, fallbackType)
	lookup := buildLookup(normalized)
	return normalized, lookup, nil
}

func normalizeDefinitions(definitions []effectDefinition, defaultType string) map[string]effectDefinition {
	out := make(map[string]effectDefinition, len(definitions))
	for _, item := range definitions {
		id := normalizeToken(item.ID)
		if id == "" {
			id = slugify(firstNonEmpty(item.Name, item.Label))
		}
		if id == "" {
			continue
		}

		normalizedType := normalizeToken(item.Type)
		if normalizedType != effectTypeBuff && normalizedType != effectTypeDebuff {
			normalizedType = defaultType
		}

		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = displayNameFromID(id)
		}

		out[id] = effectDefinition{
			ID:          id,
			Name:        name,
			Label:       strings.TrimSpace(item.Label),
			Type:        normalizedType,
			Description: strings.TrimSpace(item.Description),
			Aliases:     item.Aliases,
		}
	}
	return out
}

func buildLookup(definitions map[string]effectDefinition) map[string]string {
	lookup := make(map[string]string, len(definitions)*3)
	for id, def := range definitions {
		lookup[normalizeToken(id)] = id
		lookup[normalizeToken(def.Name)] = id
		if def.Label != "" {
			lookup[normalizeToken(def.Label)] = id
		}
		for _, alias := range def.Aliases {
			token := normalizeToken(alias)
			if token != "" {
				lookup[token] = id
			}
		}
	}
	return lookup
}

func mapKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeChannel(channel string) string {
	return strings.TrimSpace(channel)
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func displayNameFromID(id string) string {
	if id == "" {
		return "Unknown"
	}
	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(id)
	parts := strings.Fields(normalized)
	if len(parts) == 0 {
		return "Unknown"
	}
	for i, part := range parts {
		if len(part) == 1 {
			parts[i] = strings.ToUpper(part)
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func slugify(value string) string {
	if value == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false

	for _, r := range strings.ToLower(value) {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(b.String(), "_")
}
