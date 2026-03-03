package wsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	defaultPath                     = "/ws"
	defaultReadBufferSize           = 1024
	defaultWriteBufferSize          = 1024
	defaultAuthTimeoutSeconds       = 10
	defaultHeartbeatIntervalSeconds = 20
	defaultClientTimeoutSeconds     = 60
	defaultMaxMessageBytes          = 16 * 1024
	defaultMaxSubscriptions         = 64
	defaultMaxOutboundQueue         = 256
	defaultRateLimitPerSecond       = 20.0
	defaultRateLimitBurst           = 40
	defaultBalanceTimeout           = 6 * time.Second
	commandCorrelationWindow        = 8 * time.Second
)

type Config struct {
	Enabled                  bool          `json:"enabled"`
	ListenAddr               string        `json:"listen_addr"`
	Path                     string        `json:"path"`
	DefaultChannel           string        `json:"default_channel"`
	SessionCookieName        string        `json:"session_cookie_name"`
	AllowedOrigins           []string      `json:"allowed_origins"`
	ReadBufferSize           int           `json:"read_buffer_size"`
	WriteBufferSize          int           `json:"write_buffer_size"`
	AuthTimeoutSeconds       int           `json:"auth_timeout_seconds"`
	HeartbeatIntervalSeconds int           `json:"heartbeat_interval_seconds"`
	ClientTimeoutSeconds     int           `json:"client_timeout_seconds"`
	MaxMessageBytes          int64         `json:"max_message_bytes"`
	MaxSubscriptions         int           `json:"max_subscriptions"`
	MaxOutboundQueue         int           `json:"max_outbound_queue"`
	RateLimitPerSecond       float64       `json:"rate_limit_per_second"`
	RateLimitBurst           int           `json:"rate_limit_burst"`
	Tokens                   []TokenConfig `json:"tokens"`
}

type TokenConfig struct {
	Token    string   `json:"token"`
	Username string   `json:"username"`
	Rank     int      `json:"rank"`
	Admin    bool     `json:"admin"`
	Channels []string `json:"channels"`
}

type authProfile struct {
	Username string
	Rank     int
	Admin    bool
	Channels map[string]struct{}
	TokenID  string
}

type clientState struct {
	id            string
	conn          *websocket.Conn
	send          chan []byte
	profile       authProfile
	limiter       *tokenBucket
	connectedAt   time.Time
	subMu         sync.RWMutex
	subscriptions map[string]struct{}
	pendingMu     sync.Mutex
	pending       []pendingCommand
	closed        atomic.Bool
}

type pendingCommand struct {
	RequestID string
	Channel   string
	CreatedAt time.Time
}

type inboundEnvelope struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type outboundEnvelope struct {
	Type      string     `json:"type"`
	ID        string     `json:"id,omitempty"`
	Timestamp time.Time  `json:"timestamp,omitempty"`
	Topic     string     `json:"topic,omitempty"`
	RefID     string     `json:"ref_id,omitempty"`
	Data      any        `json:"data,omitempty"`
	Error     *wsMessage `json:"error,omitempty"`
}

type wsMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wsInboundError struct {
	code    string
	message string
}

func (e *wsInboundError) Error() string {
	return e.message
}

func newWSInboundError(code, message string) error {
	return &wsInboundError{
		code:    strings.TrimSpace(code),
		message: strings.TrimSpace(message),
	}
}

type subscribePayload struct {
	Topics []string `json:"topics"`
}

type commandPayload struct {
	Command string `json:"command"`
	Channel string `json:"channel,omitempty"`
}

type economyBalancePayload struct {
	Channel  string `json:"channel,omitempty"`
	Username string `json:"username,omitempty"`
}

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	economy   *framework.EconomyClient
	config    Config
	upgrader  websocket.Upgrader
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
	readyChan chan struct{}

	eventsHandled atomic.Int64
	nextClientID  atomic.Uint64
	droppedSend   atomic.Int64

	mu       sync.RWMutex
	running  bool
	server   *http.Server
	clients  map[string]*clientState
	tokenMap map[string]authProfile
}

func New() framework.Plugin {
	return &Plugin{
		name:      "wsbridge",
		readyChan: make(chan struct{}),
		clients:   make(map[string]*clientState),
		tokenMap:  make(map[string]authProfile),
		config: Config{
			Enabled:                  false,
			ListenAddr:               ":8091",
			Path:                     defaultPath,
			SessionCookieName:        "daz_session",
			ReadBufferSize:           defaultReadBufferSize,
			WriteBufferSize:          defaultWriteBufferSize,
			AuthTimeoutSeconds:       defaultAuthTimeoutSeconds,
			HeartbeatIntervalSeconds: defaultHeartbeatIntervalSeconds,
			ClientTimeoutSeconds:     defaultClientTimeoutSeconds,
			MaxMessageBytes:          defaultMaxMessageBytes,
			MaxSubscriptions:         defaultMaxSubscriptions,
			MaxOutboundQueue:         defaultMaxOutboundQueue,
			RateLimitPerSecond:       defaultRateLimitPerSecond,
			RateLimitBurst:           defaultRateLimitBurst,
		},
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Dependencies() []string {
	return []string{"eventfilter"}
}

func (p *Plugin) Ready() bool {
	select {
	case <-p.readyChan:
		return true
	default:
		return false
	}
}

func (p *Plugin) Init(configData json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.economy = framework.NewEconomyClient(bus, p.name)
	if len(configData) > 0 {
		if err := json.Unmarshal(configData, &p.config); err != nil {
			return fmt.Errorf("parse wsbridge config: %w", err)
		}
	}

	p.applyDefaults()

	p.tokenMap = make(map[string]authProfile, len(p.config.Tokens))
	for _, tokenCfg := range p.config.Tokens {
		token := strings.TrimSpace(tokenCfg.Token)
		username := strings.TrimSpace(tokenCfg.Username)
		if token == "" || username == "" {
			continue
		}

		allowed := make(map[string]struct{}, len(tokenCfg.Channels))
		for _, ch := range tokenCfg.Channels {
			trimmed := strings.TrimSpace(ch)
			if trimmed != "" {
				allowed[trimmed] = struct{}{}
			}
		}

		p.tokenMap[token] = authProfile{
			Username: username,
			Rank:     tokenCfg.Rank,
			Admin:    tokenCfg.Admin,
			Channels: allowed,
			TokenID:  token,
		}
	}

	p.upgrader = websocket.Upgrader{
		ReadBufferSize:  p.config.ReadBufferSize,
		WriteBufferSize: p.config.WriteBufferSize,
		CheckOrigin:     p.checkOrigin,
	}

	return nil
}

func (p *Plugin) applyDefaults() {
	if strings.TrimSpace(p.config.ListenAddr) == "" {
		p.config.ListenAddr = ":8091"
	}
	if strings.TrimSpace(p.config.Path) == "" {
		p.config.Path = defaultPath
	}
	if !strings.HasPrefix(p.config.Path, "/") {
		p.config.Path = "/" + p.config.Path
	}
	if p.config.ReadBufferSize <= 0 {
		p.config.ReadBufferSize = defaultReadBufferSize
	}
	if p.config.WriteBufferSize <= 0 {
		p.config.WriteBufferSize = defaultWriteBufferSize
	}
	if p.config.AuthTimeoutSeconds <= 0 {
		p.config.AuthTimeoutSeconds = defaultAuthTimeoutSeconds
	}
	if p.config.HeartbeatIntervalSeconds <= 0 {
		p.config.HeartbeatIntervalSeconds = defaultHeartbeatIntervalSeconds
	}
	if p.config.ClientTimeoutSeconds <= 0 {
		p.config.ClientTimeoutSeconds = defaultClientTimeoutSeconds
	}
	if p.config.MaxMessageBytes <= 0 {
		p.config.MaxMessageBytes = defaultMaxMessageBytes
	}
	if p.config.MaxSubscriptions <= 0 {
		p.config.MaxSubscriptions = defaultMaxSubscriptions
	}
	if p.config.MaxOutboundQueue <= 0 {
		p.config.MaxOutboundQueue = defaultMaxOutboundQueue
	}
	if p.config.RateLimitPerSecond <= 0 {
		p.config.RateLimitPerSecond = defaultRateLimitPerSecond
	}
	if p.config.RateLimitBurst <= 0 {
		p.config.RateLimitBurst = defaultRateLimitBurst
	}
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.mu.Unlock()

	p.startTime = time.Now().UTC()
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if !p.config.Enabled {
		close(p.readyChan)
		logger.Info(p.name, "wsbridge disabled by config")
		return nil
	}

	if len(p.tokenMap) == 0 {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		return fmt.Errorf("wsbridge requires at least one token when enabled")
	}

	if err := p.eventBus.SubscribeWithTags("*", p.handleAnyEvent, nil); err != nil {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		return fmt.Errorf("subscribe wildcard events: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(p.config.Path, p.handleWebSocket)

	p.server = &http.Server{
		Addr:         p.config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		err := p.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			logger.Error(p.name, "ws server stopped unexpectedly: %v", err)
		}
	}()

	close(p.readyChan)
	logger.Info(p.name, "wsbridge listening on %s%s", p.config.ListenAddr, p.config.Path)
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
	for id, client := range p.clients {
		p.closeClientLocked(id, client, websocket.CloseGoingAway, "server shutting down")
	}
	p.clients = make(map[string]*clientState)
	p.mu.Unlock()

	if p.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown wsbridge server: %w", err)
		}
	}

	return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
	_ = event
	return nil
}

func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	state := "stopped"
	if p.running {
		if p.config.Enabled {
			state = "running"
		} else {
			state = "disabled"
		}
	}
	clientCount := len(p.clients)
	p.mu.RUnlock()

	if clientCount > 0 && p.eventsHandled.Load()%500 == 0 {
		logger.Debug(p.name, "active clients=%d dropped_ws_messages=%d", clientCount, p.droppedSend.Load())
	}

	return framework.PluginStatus{
		Name:          p.name,
		State:         state,
		EventsHandled: p.eventsHandled.Load(),
		Uptime:        time.Since(p.startTime),
	}
}

func (p *Plugin) checkOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if len(p.config.AllowedOrigins) == 0 {
		return origin == ""
	}
	if origin == "" {
		return false
	}

	for _, allowed := range p.config.AllowedOrigins {
		a := strings.TrimSpace(allowed)
		if a == "*" {
			return true
		}
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

func (p *Plugin) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	profile, ok := p.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn(p.name, "websocket upgrade failed: %v", err)
		return
	}

	clientID := fmt.Sprintf("ws-%d", p.nextClientID.Add(1))
	client := &clientState{
		id:            clientID,
		conn:          conn,
		send:          make(chan []byte, p.config.MaxOutboundQueue),
		profile:       profile,
		connectedAt:   time.Now().UTC(),
		limiter:       newTokenBucket(p.config.RateLimitPerSecond, p.config.RateLimitBurst),
		subscriptions: make(map[string]struct{}),
	}

	p.mu.Lock()
	p.clients[clientID] = client
	p.mu.Unlock()

	client.subscriptions["system.*"] = struct{}{}

	p.sendOutbound(client, outboundEnvelope{
		Type: "welcome",
		Data: map[string]any{
			"client_id":       clientID,
			"username":        profile.Username,
			"default_channel": p.config.DefaultChannel,
			"server_time":     time.Now().UTC(),
		},
	})

	go p.writePump(client)
	go p.readPump(client)
}

func (p *Plugin) authenticate(r *http.Request) (authProfile, bool) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		}
	}
	if token == "" && strings.TrimSpace(p.config.SessionCookieName) != "" {
		if cookie, err := r.Cookie(p.config.SessionCookieName); err == nil {
			token = strings.TrimSpace(cookie.Value)
		}
	}
	if token == "" {
		return authProfile{}, false
	}
	profile, ok := p.tokenMap[token]
	return profile, ok
}

func (p *Plugin) readPump(client *clientState) {
	defer p.unregisterClient(client.id, websocket.CloseNormalClosure, "connection closed")

	deadline := time.Duration(p.config.ClientTimeoutSeconds) * time.Second
	client.conn.SetReadLimit(p.config.MaxMessageBytes)
	_ = client.conn.SetReadDeadline(time.Now().Add(deadline))
	client.conn.SetPongHandler(func(string) error {
		return client.conn.SetReadDeadline(time.Now().Add(deadline))
	})

	for {
		_, payload, err := client.conn.ReadMessage()
		if err != nil {
			return
		}

		if !client.limiter.Allow() {
			p.sendError(client, "", "RATE_LIMITED", "request rate exceeded")
			continue
		}

		var envelope inboundEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			p.sendError(client, "", "BAD_REQUEST", "invalid json envelope")
			continue
		}

		if err := p.handleInbound(client, envelope); err != nil {
			var inboundErr *wsInboundError
			if errors.As(err, &inboundErr) && inboundErr != nil {
				code := strings.TrimSpace(inboundErr.code)
				if code == "" {
					code = "BAD_REQUEST"
				}
				msg := strings.TrimSpace(inboundErr.message)
				if msg == "" {
					msg = "request failed"
				}
				p.sendError(client, envelope.ID, code, msg)
				continue
			}

			logger.Error(p.name, "inbound handling failed: %v", err)
			p.sendError(client, envelope.ID, "INTERNAL", "internal error")
		}
	}
}

func (p *Plugin) handleInbound(client *clientState, envelope inboundEnvelope) error {
	switch envelope.Type {
	case "ping":
		p.sendAck(client, envelope.ID, map[string]any{"pong": time.Now().UTC()})
		return nil
	case "subscribe":
		var payload subscribePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return newWSInboundError("BAD_REQUEST", "invalid subscribe payload")
		}
		topics, err := p.subscribe(client, payload.Topics)
		if err != nil {
			return newWSInboundError("BAD_REQUEST", err.Error())
		}
		p.sendAck(client, envelope.ID, map[string]any{"topics": topics})
		return nil
	case "unsubscribe":
		var payload subscribePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return newWSInboundError("BAD_REQUEST", "invalid unsubscribe payload")
		}
		topics := p.unsubscribe(client, payload.Topics)
		p.sendAck(client, envelope.ID, map[string]any{"topics": topics})
		return nil
	case "command.execute":
		var payload commandPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return newWSInboundError("BAD_REQUEST", "invalid command payload")
		}
		channel, err := p.dispatchCommand(client, envelope.ID, payload)
		if err != nil {
			return newWSInboundError("BAD_REQUEST", err.Error())
		}
		p.sendAck(client, envelope.ID, map[string]any{
			"accepted": true,
			"channel":  channel,
		})
		return nil
	case "economy.get_balance":
		var payload economyBalancePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return newWSInboundError("BAD_REQUEST", "invalid economy.get_balance payload")
		}
		result, err := p.handleEconomyGetBalance(client, payload)
		if err != nil {
			return err
		}
		p.sendAck(client, envelope.ID, result)
		return nil
	default:
		return newWSInboundError("BAD_REQUEST", fmt.Sprintf("unsupported message type %q", envelope.Type))
	}
}

func (p *Plugin) handleEconomyGetBalance(client *clientState, payload economyBalancePayload) (map[string]any, error) {
	channel, username, err := p.resolveBalanceQuery(client, payload)
	if err != nil {
		return nil, newWSInboundError("BAD_REQUEST", err.Error())
	}

	if p.economy == nil {
		return nil, newWSInboundError("BACKEND_UNAVAILABLE", "balance service unavailable")
	}

	ctx := context.Background()
	if p.ctx != nil {
		ctx = p.ctx
	}
	reqCtx, cancel := context.WithTimeout(ctx, defaultBalanceTimeout)
	defer cancel()

	balance, err := p.economy.GetBalance(reqCtx, channel, username)
	if err != nil {
		code, message := mapEconomyErrorToWSError(err)
		logger.Warn(p.name, "economy.get_balance failed for %s/%s: %v", channel, username, err)
		return nil, newWSInboundError(code, message)
	}

	return map[string]any{
		"channel":  channel,
		"username": username,
		"balance":  balance,
	}, nil
}

func (p *Plugin) resolveBalanceQuery(client *clientState, payload economyBalancePayload) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("session unavailable")
	}
	channel, err := p.resolveChannel(client.profile, strings.TrimSpace(payload.Channel))
	if err != nil {
		return "", "", err
	}

	username := strings.TrimSpace(payload.Username)
	if username == "" {
		username = strings.TrimSpace(client.profile.Username)
	}
	if username == "" {
		return "", "", fmt.Errorf("username is required")
	}

	if !client.profile.Admin && !strings.EqualFold(username, strings.TrimSpace(client.profile.Username)) {
		return "", "", fmt.Errorf("username %q not allowed for session", username)
	}

	return channel, username, nil
}

func mapEconomyErrorToWSError(err error) (string, string) {
	if err == nil {
		return "BACKEND_ERROR", "balance lookup failed"
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "BACKEND_TIMEOUT", "balance lookup timed out"
	}

	var economyErr *framework.EconomyError
	if errors.As(err, &economyErr) && economyErr != nil {
		switch strings.TrimSpace(economyErr.ErrorCode) {
		case "INVALID_ARGUMENT", "INVALID_AMOUNT":
			msg := strings.TrimSpace(economyErr.Message)
			if msg == "" {
				msg = "invalid balance query"
			}
			return "BAD_REQUEST", msg
		case "DB_UNAVAILABLE":
			return "BACKEND_UNAVAILABLE", "balance service unavailable"
		case "DB_ERROR", "INTERNAL":
			return "BACKEND_ERROR", "balance lookup failed"
		default:
			return "BACKEND_ERROR", "balance lookup failed"
		}
	}

	return "BACKEND_ERROR", "balance lookup failed"
}

func (p *Plugin) dispatchCommand(client *clientState, requestID string, payload commandPayload) (string, error) {
	command := strings.TrimSpace(payload.Command)
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}
	if !strings.HasPrefix(command, "!") {
		command = "!" + command
	}

	channel, err := p.resolveChannel(client.profile, strings.TrimSpace(payload.Channel))
	if err != nil {
		return "", err
	}

	chatEvent := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username:    client.profile.Username,
			Message:     command,
			UserRank:    client.profile.Rank,
			Channel:     channel,
			MessageTime: time.Now().UnixMilli(),
		},
	}
	metadata := framework.NewEventMetadata(p.name, eventbus.EventCytubeChatMsg).
		WithPriority(framework.PriorityHigh)

	if err := p.eventBus.BroadcastWithMetadata(eventbus.EventCytubeChatMsg, chatEvent, metadata); err != nil {
		return "", fmt.Errorf("dispatch command: %w", err)
	}

	if requestID != "" {
		client.pendingMu.Lock()
		client.pending = append(client.pending, pendingCommand{
			RequestID: requestID,
			Channel:   channel,
			CreatedAt: time.Now().UTC(),
		})
		client.pendingMu.Unlock()
	}

	return channel, nil
}

func (p *Plugin) resolveChannel(profile authProfile, requested string) (string, error) {
	channel := requested
	if channel == "" {
		channel = strings.TrimSpace(p.config.DefaultChannel)
	}
	if channel == "" {
		for allowed := range profile.Channels {
			channel = allowed
			break
		}
	}
	if channel == "" {
		return "", fmt.Errorf("no channel configured for session")
	}

	if len(profile.Channels) == 0 {
		return channel, nil
	}
	if _, ok := profile.Channels[channel]; !ok {
		return "", fmt.Errorf("channel %q not allowed for session", channel)
	}

	return channel, nil
}

func (p *Plugin) subscribe(client *clientState, topics []string) ([]string, error) {
	if len(topics) == 0 {
		return nil, fmt.Errorf("topics cannot be empty")
	}

	client.subMu.Lock()
	defer client.subMu.Unlock()

	for _, topic := range topics {
		topic = strings.TrimSpace(topic)
		if topic == "" {
			continue
		}
		if len(client.subscriptions) >= p.config.MaxSubscriptions {
			return nil, fmt.Errorf("max subscriptions reached (%d)", p.config.MaxSubscriptions)
		}
		client.subscriptions[topic] = struct{}{}
	}

	return subscriptionList(client.subscriptions), nil
}

func (p *Plugin) unsubscribe(client *clientState, topics []string) []string {
	client.subMu.Lock()
	defer client.subMu.Unlock()

	for _, topic := range topics {
		delete(client.subscriptions, strings.TrimSpace(topic))
	}
	return subscriptionList(client.subscriptions)
}

func subscriptionList(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for topic := range set {
		out = append(out, topic)
	}
	sort.Strings(out)
	return out
}

func (p *Plugin) writePump(client *clientState) {
	heartbeat := time.Duration(p.config.HeartbeatIntervalSeconds) * time.Second
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			if err := client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if err := client.conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
				return
			}
		}
	}
}

func (p *Plugin) sendAck(client *clientState, id string, data any) {
	p.sendOutbound(client, outboundEnvelope{
		Type:      "ack",
		ID:        id,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
}

func (p *Plugin) sendError(client *clientState, id, code, message string) {
	p.sendOutbound(client, outboundEnvelope{
		Type:      "error",
		ID:        id,
		Timestamp: time.Now().UTC(),
		Error: &wsMessage{
			Code:    code,
			Message: message,
		},
	})
}

func (p *Plugin) sendOutbound(client *clientState, envelope outboundEnvelope) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	if client.closed.Load() {
		return
	}
	defer func() {
		if recover() != nil {
			p.droppedSend.Add(1)
		}
	}()

	select {
	case client.send <- raw:
	default:
		p.droppedSend.Add(1)
	}
}

func (p *Plugin) unregisterClient(clientID string, closeCode int, reason string) {
	p.mu.Lock()
	client, ok := p.clients[clientID]
	if ok {
		p.closeClientLocked(clientID, client, closeCode, reason)
		delete(p.clients, clientID)
	}
	p.mu.Unlock()
}

func (p *Plugin) closeClientLocked(clientID string, client *clientState, closeCode int, reason string) {
	if client == nil || client.closed.Swap(true) {
		return
	}
	_ = client.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(closeCode, reason),
		time.Now().Add(time.Second),
	)
	_ = client.conn.Close()
	logger.Debug(p.name, "client disconnected %s (%s)", clientID, reason)
}

func (p *Plugin) handleAnyEvent(event framework.Event) error {
	p.eventsHandled.Add(1)

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent == nil {
		return nil
	}

	topic := event.Type()
	evtData := dataEvent.Data
	timestamp := event.Timestamp().UTC()

	p.mu.RLock()
	clients := make([]*clientState, 0, len(p.clients))
	for _, client := range p.clients {
		clients = append(clients, client)
	}
	p.mu.RUnlock()

	for _, client := range clients {
		if !clientSubscribedToTopic(client, topic) {
			continue
		}
		if !canReceiveEvent(client, topic, evtData) {
			continue
		}

		refID := client.consumePendingRef(topic, evtData, timestamp)
		p.sendOutbound(client, outboundEnvelope{
			Type:      "event",
			Timestamp: timestamp,
			Topic:     topic,
			RefID:     refID,
			Data:      evtData,
		})
	}

	return nil
}

func clientSubscribedToTopic(client *clientState, topic string) bool {
	client.subMu.RLock()
	defer client.subMu.RUnlock()

	for sub := range client.subscriptions {
		if topicMatches(sub, topic) {
			return true
		}
	}
	return false
}

func topicMatches(subscription, topic string) bool {
	sub := strings.TrimSpace(subscription)
	if sub == "" {
		return false
	}
	if sub == topic {
		return true
	}
	if strings.HasSuffix(sub, "*") {
		prefix := strings.TrimSuffix(sub, "*")
		return strings.HasPrefix(topic, prefix)
	}
	return false
}

func canReceiveEvent(client *clientState, topic string, data *framework.EventData) bool {
	if client.profile.Admin {
		return true
	}
	if data == nil {
		return true
	}

	switch topic {
	case "cytube.send.pm", "cytube.event.pm":
		pm := data.PrivateMessage
		if pm == nil {
			return false
		}
		username := strings.ToLower(client.profile.Username)
		return strings.ToLower(pm.ToUser) == username || strings.ToLower(pm.FromUser) == username
	case "plugin.response":
		resp := data.PluginResponse
		if resp == nil || resp.Data == nil || resp.Data.KeyValue == nil {
			return false
		}
		target := strings.ToLower(resp.Data.KeyValue["username"])
		return target != "" && target == strings.ToLower(client.profile.Username)
	default:
		return true
	}
}

func (c *clientState) consumePendingRef(topic string, data *framework.EventData, now time.Time) string {
	if data == nil {
		return ""
	}

	channel := ""
	switch topic {
	case "cytube.send":
		if data.RawMessage == nil {
			return ""
		}
		channel = data.RawMessage.Channel
	case "cytube.send.pm":
		if data.PrivateMessage == nil {
			return ""
		}
		channel = data.PrivateMessage.Channel
	case "plugin.response":
		if data.PluginResponse == nil || data.PluginResponse.Data == nil || data.PluginResponse.Data.KeyValue == nil {
			return ""
		}
		channel = data.PluginResponse.Data.KeyValue["channel"]
	default:
		return ""
	}

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	cutoff := now.Add(-commandCorrelationWindow)
	for len(c.pending) > 0 && c.pending[0].CreatedAt.Before(cutoff) {
		c.pending = c.pending[1:]
	}

	for i := range c.pending {
		if strings.TrimSpace(c.pending[i].Channel) == strings.TrimSpace(channel) {
			ref := c.pending[i].RequestID
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return ref
		}
	}

	return ""
}

type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(ratePerSecond float64, burst int) *tokenBucket {
	now := time.Now()
	b := float64(burst)
	if b <= 0 {
		b = float64(defaultRateLimitBurst)
	}
	if ratePerSecond <= 0 {
		ratePerSecond = defaultRateLimitPerSecond
	}
	return &tokenBucket{
		rate:   ratePerSecond,
		burst:  b,
		tokens: b,
		last:   now,
	}
}

func (tb *tokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.last).Seconds()
	tb.last = now

	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.burst {
		tb.tokens = tb.burst
	}

	if tb.tokens < 1.0 {
		return false
	}

	tb.tokens -= 1.0
	return true
}
