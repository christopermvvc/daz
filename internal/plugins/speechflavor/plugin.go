package speechflavor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	pluginName        = "speechflavor"
	stateInitialized  = "initialized"
	stateRunning      = "running"
	stateStopped      = "stopped"
	operationRewrite  = "rewrite"
	defaultTimeoutMS  = 5000
	defaultMaxTokens  = 96
	defaultTemp       = 0.6
	defaultKeepAlive  = "15m"
	minLengthRatio    = 0.45
	maxLengthRatio    = 1.75
	minPolarityTokens = 1
)

var protectedTokenPattern = regexp.MustCompile(`(![a-zA-Z0-9_]+|%[-+ #0*.0-9]*[bcdeEfFgGosqvxXptT]|<[^<>\s]+>|\$\{[^}]+\}|\{[^{}\s]+\})`)

var affirmativeTokens = map[string]struct{}{
	"yes": {}, "yeah": {}, "yep": {}, "yup": {}, "sure": {}, "ok": {}, "okay": {}, "affirmative": {}, "true": {},
}

var negativeTokens = map[string]struct{}{
	"no": {}, "nope": {}, "nah": {}, "negative": {}, "false": {},
}

type polarity int

const (
	polarityUnknown polarity = iota
	polarityAffirmative
	polarityNegative
)

type Config struct {
	Enabled            bool    `json:"enabled"`
	RewriteStylePrompt string  `json:"rewrite_style_prompt"`
	Model              string  `json:"model"`
	KeepAlive          string  `json:"keep_alive"`
	Temperature        float64 `json:"temperature"`
	MaxTokens          int     `json:"max_tokens"`
	TimeoutMS          int     `json:"timeout_ms"`
	PreserveTokens     bool    `json:"preserve_tokens"`
	WarmOnStart        bool    `json:"warm_on_start"`
	WarmIntervalSecs   int     `json:"warm_interval_seconds"`
}

type Plugin struct {
	ctx          context.Context
	cancel       context.CancelFunc
	eventBus     framework.EventBus
	ollamaClient *framework.OllamaClient
	name         string
	running      bool
	mu           sync.RWMutex
	readyChan    chan struct{}
	status       framework.PluginStatus
	config       *Config

	generateFunc func(ctx context.Context, req framework.OllamaGenerateRequest) (framework.OllamaGenerateResponse, error)
}

type rewriteRequest struct {
	Channel        string   `json:"channel"`
	Username       string   `json:"username"`
	Text           string   `json:"text"`
	PreserveTokens *bool    `json:"preserve_tokens,omitempty"`
	Model          string   `json:"model,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxTokens      int      `json:"max_tokens,omitempty"`
}

type rewriteResponse struct {
	Text         string `json:"text"`
	Model        string `json:"model,omitempty"`
	FallbackUsed bool   `json:"fallback_used"`
	Reason       string `json:"reason,omitempty"`
}

type errorPayload struct {
	ErrorCode string                 `json:"error_code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

func New() framework.Plugin {
	return &Plugin{
		name: pluginName,
		config: &Config{
			Enabled:        true,
			Temperature:    defaultTemp,
			MaxTokens:      defaultMaxTokens,
			TimeoutMS:      defaultTimeoutMS,
			PreserveTokens: true,
			KeepAlive:      defaultKeepAlive,
			WarmOnStart:    true,
		},
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  pluginName,
			State: stateInitialized,
		},
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Dependencies() []string {
	return []string{"ollama"}
}

func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(config) > 0 {
		if err := json.Unmarshal(config, p.config); err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}
	}

	if p.config.Temperature == 0 {
		p.config.Temperature = defaultTemp
	}
	if p.config.MaxTokens <= 0 {
		p.config.MaxTokens = defaultMaxTokens
	}
	if p.config.TimeoutMS <= 0 {
		p.config.TimeoutMS = defaultTimeoutMS
	}
	if strings.TrimSpace(p.config.KeepAlive) == "" {
		p.config.KeepAlive = defaultKeepAlive
	}
	if strings.TrimSpace(p.config.RewriteStylePrompt) == "" {
		p.config.RewriteStylePrompt = defaultRewriteStylePrompt(resolveBotName())
	}

	p.eventBus = bus
	p.ollamaClient = framework.NewOllamaClient(bus, p.name)
	p.generateFunc = p.ollamaClient.Generate
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.status = framework.PluginStatus{
		Name:  pluginName,
		State: stateInitialized,
	}
	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("plugin already running")
	}

	if err := p.eventBus.Subscribe(eventbus.EventPluginRequest, p.handlePluginRequest); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to plugin.request: %w", err)
		return p.status.LastError
	}

	p.running = true
	p.status.State = stateRunning

	if p.config.Enabled && p.generateFunc != nil {
		if p.config.WarmOnStart {
			go p.warmModel("startup")
		}
		if p.config.WarmIntervalSecs > 0 {
			go p.runWarmLoop(time.Duration(p.config.WarmIntervalSecs) * time.Second)
		}
	}

	select {
	case <-p.readyChan:
	default:
		close(p.readyChan)
	}
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
	if !ok || dataEvent.Data == nil {
		return nil
	}

	req := dataEvent.Data.PluginRequest
	if req == nil || req.To != p.name || req.ID == "" {
		return nil
	}

	switch req.Type {
	case operationRewrite:
		p.handleRewriteRequest(req)
	default:
		p.deliverError(req, "UNSUPPORTED_OPERATION", fmt.Sprintf("unknown operation: %s", req.Type), map[string]interface{}{
			"type": req.Type,
		})
	}

	return nil
}

func (p *Plugin) handleRewriteRequest(req *framework.PluginRequest) {
	payload, err := p.parseRewriteRequest(req)
	if err != nil {
		p.deliverError(req, "INVALID_REQUEST", err.Error(), nil)
		return
	}

	original := strings.TrimSpace(payload.Text)
	if original == "" {
		p.deliverError(req, "INVALID_REQUEST", "missing field text", map[string]interface{}{"field": "text"})
		return
	}

	if !p.config.Enabled {
		p.deliverSuccess(req, rewriteResponse{
			Text:         original,
			FallbackUsed: true,
			Reason:       "disabled",
		})
		return
	}

	preserveTokens := p.config.PreserveTokens
	if payload.PreserveTokens != nil {
		preserveTokens = *payload.PreserveTokens
	}

	rewritable := original
	protectedTokens := []string{}
	if preserveTokens {
		rewritable, protectedTokens = protectTokens(original)
	}

	timeout := time.Duration(p.config.TimeoutMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	ollamaReq := framework.OllamaGenerateRequest{
		Channel:      payload.Channel,
		Username:     payload.Username,
		Message:      rewritable,
		SystemPrompt: p.config.RewriteStylePrompt,
		Model:        strings.TrimSpace(payload.Model),
		KeepAlive:    strings.TrimSpace(p.config.KeepAlive),
		MaxTokens:    p.config.MaxTokens,
		Temperature:  p.config.Temperature,
		ExtraContext: map[string]string{
			"source":           "speechflavor",
			"preserve_tokens":  fmt.Sprintf("%t", preserveTokens),
			"preserve_meaning": "true",
		},
	}
	if payload.Temperature != nil && *payload.Temperature > 0 {
		ollamaReq.Temperature = *payload.Temperature
	}
	if payload.MaxTokens > 0 {
		ollamaReq.MaxTokens = payload.MaxTokens
	}
	if ollamaReq.Username == "" {
		ollamaReq.Username = "user"
	}

	resp, err := p.generateFunc(ctx, ollamaReq)
	if err != nil {
		logger.Warn(p.name, "rewrite failed via ollama, returning fallback: %v", err)
		p.deliverSuccess(req, rewriteResponse{
			Text:         original,
			FallbackUsed: true,
			Reason:       "generation_failed",
		})
		return
	}

	rewritten := strings.TrimSpace(resp.Text)
	if rewritten == "" {
		p.deliverSuccess(req, rewriteResponse{
			Text:         original,
			FallbackUsed: true,
			Reason:       "empty_response",
		})
		return
	}

	if preserveTokens && len(protectedTokens) > 0 {
		restored, ok := restoreTokens(rewritten, protectedTokens)
		if !ok {
			p.deliverSuccess(req, rewriteResponse{
				Text:         original,
				FallbackUsed: true,
				Reason:       "token_preservation_failed",
			})
			return
		}
		rewritten = restored
	}

	if !isLengthReasonable(original, rewritten) {
		p.deliverSuccess(req, rewriteResponse{
			Text:         original,
			FallbackUsed: true,
			Reason:       "length_guard_triggered",
		})
		return
	}

	if !isPolarityCompatible(original, rewritten) {
		p.deliverSuccess(req, rewriteResponse{
			Text:         original,
			FallbackUsed: true,
			Reason:       "polarity_guard_triggered",
		})
		return
	}

	p.deliverSuccess(req, rewriteResponse{
		Text:         rewritten,
		Model:        resp.Model,
		FallbackUsed: false,
	})
}

func (p *Plugin) runWarmLoop(interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.warmModel("interval")
		}
	}
}

func (p *Plugin) warmModel(reason string) {
	p.mu.RLock()
	generate := p.generateFunc
	cfg := *p.config
	ctx := p.ctx
	p.mu.RUnlock()

	if generate == nil || ctx == nil || !cfg.Enabled {
		return
	}

	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeoutMS * time.Millisecond
	}
	if timeout < 2*time.Second {
		timeout = 2 * time.Second
	}
	if timeout > 20*time.Second {
		timeout = 20 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := generate(reqCtx, framework.OllamaGenerateRequest{
		Channel:   "system",
		Username:  "speechflavor",
		Message:   "warm the model cache",
		Model:     strings.TrimSpace(cfg.Model),
		KeepAlive: strings.TrimSpace(cfg.KeepAlive),
		MaxTokens: 8,
		ExtraContext: map[string]string{
			"source": "speechflavor",
			"mode":   "warmup",
			"reason": reason,
		},
		SystemPrompt: "You are performing a model warmup request. Reply with exactly: ok",
	})
	if err != nil {
		logger.Debug(p.name, "warmup request failed (%s): %v", reason, err)
		return
	}
	logger.Debug(p.name, "warmup request succeeded (%s)", reason)
}

func (p *Plugin) parseRewriteRequest(req *framework.PluginRequest) (*rewriteRequest, error) {
	if req.Data == nil {
		return nil, fmt.Errorf("missing data")
	}

	payload := &rewriteRequest{}
	if len(req.Data.RawJSON) > 0 {
		if err := json.Unmarshal(req.Data.RawJSON, payload); err != nil {
			return nil, fmt.Errorf("invalid request payload")
		}
	}

	if len(req.Data.KeyValue) > 0 {
		if payload.Channel == "" {
			payload.Channel = req.Data.KeyValue["channel"]
		}
		if payload.Username == "" {
			payload.Username = req.Data.KeyValue["username"]
		}
		if payload.Text == "" {
			payload.Text = req.Data.KeyValue["text"]
		}
		if payload.Text == "" {
			payload.Text = req.Data.KeyValue["sentence"]
		}
	}

	return payload, nil
}

func (p *Plugin) deliverSuccess(req *framework.PluginRequest, payload rewriteResponse) {
	raw, err := json.Marshal(payload)
	if err != nil {
		p.deliverError(req, "INTERNAL", "failed to marshal response", nil)
		return
	}

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    p.name,
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: raw,
			},
		},
	}
	p.eventBus.DeliverResponse(req.ID, response, nil)
}

func (p *Plugin) deliverError(req *framework.PluginRequest, errorCode, message string, details map[string]interface{}) {
	payload := errorPayload{
		ErrorCode: errorCode,
		Message:   message,
		Details:   details,
	}
	raw, _ := json.Marshal(payload)

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    p.name,
			Success: false,
			Error:   message,
			Data: &framework.ResponseData{
				RawJSON: raw,
			},
		},
	}
	p.eventBus.DeliverResponse(req.ID, response, nil)
}

func defaultRewriteStylePrompt(botName string) string {
	return fmt.Sprintf(
		"You rewrite user-provided sentences into how %s would naturally say them in chat.\n\n"+
			"Rules:\n"+
			"- Preserve the original meaning exactly.\n"+
			"- Preserve polarity exactly (affirmative stays affirmative, negative stays negative).\n"+
			"- Keep placeholders/tokens untouched: !commands, %%format tokens, <placeholders>, {vars}, ${vars}.\n"+
			"- Keep roughly the same length; do not expand into multi-sentence paragraphs.\n"+
			"- Add conversational flavor/personality only; do not add new facts or intent.\n"+
			"- Return only the rewritten sentence text.\n\n"+
			"Character flavor:\n"+
			"- laid-back aussie chat tone, casual lowercase, concise phrasing.\n"+
			"- natural human chat style with light slang allowed.\n",
		botName,
	)
}

func resolveBotName() string {
	botName := strings.TrimSpace(os.Getenv("DAZ_BOT_NAME"))
	if botName == "" {
		return "Dazza"
	}
	return botName
}

func protectTokens(input string) (string, []string) {
	tokens := make([]string, 0, 4)
	index := 0
	protected := protectedTokenPattern.ReplaceAllStringFunc(input, func(match string) string {
		tokens = append(tokens, match)
		marker := tokenMarker(index)
		index++
		return marker
	})
	return protected, tokens
}

func restoreTokens(input string, tokens []string) (string, bool) {
	restored := input
	for i, token := range tokens {
		markerRegex := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(tokenMarker(i)))
		if !markerRegex.MatchString(restored) {
			return "", false
		}
		restored = markerRegex.ReplaceAllStringFunc(restored, func(_ string) string {
			return token
		})
	}

	if strings.Contains(strings.ToUpper(restored), "__DAZ_TOKEN_") {
		return "", false
	}
	return restored, true
}

func tokenMarker(i int) string {
	return fmt.Sprintf("__DAZ_TOKEN_%d__", i)
}

func isLengthReasonable(original, rewritten string) bool {
	origLen := runeCount(strings.TrimSpace(original))
	newLen := runeCount(strings.TrimSpace(rewritten))
	if origLen == 0 || newLen == 0 {
		return false
	}

	minLen := int(math.Floor(float64(origLen) * minLengthRatio))
	if minLen < 1 {
		minLen = 1
	}
	maxLen := int(math.Ceil(float64(origLen) * maxLengthRatio))
	if maxLen < minLen {
		maxLen = minLen
	}

	return newLen >= minLen && newLen <= maxLen
}

func isPolarityCompatible(original, rewritten string) bool {
	sourcePolarity := detectPolarity(original)
	if sourcePolarity == polarityUnknown {
		return true
	}
	targetPolarity := detectPolarity(rewritten)
	return targetPolarity == sourcePolarity
}

func detectPolarity(text string) polarity {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
	if len(tokens) == 0 {
		return polarityUnknown
	}

	if p, ok := tokenPolarity(tokens[0]); ok {
		return p
	}

	affCount := 0
	negCount := 0
	for _, token := range tokens {
		if p, ok := tokenPolarity(token); ok {
			if p == polarityAffirmative {
				affCount++
			} else if p == polarityNegative {
				negCount++
			}
		}
	}

	if affCount >= minPolarityTokens && negCount == 0 {
		return polarityAffirmative
	}
	if negCount >= minPolarityTokens && affCount == 0 {
		return polarityNegative
	}
	return polarityUnknown
}

func tokenPolarity(token string) (polarity, bool) {
	if _, ok := affirmativeTokens[token]; ok {
		return polarityAffirmative, true
	}
	if _, ok := negativeTokens[token]; ok {
		return polarityNegative, true
	}
	return polarityUnknown, false
}

func runeCount(value string) int {
	return len([]rune(value))
}
