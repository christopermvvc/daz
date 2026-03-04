package bug

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
	"github.com/hildolfr/daz/pkg/eventbus"
)

const (
	defaultRepoOwner          = "hildolfr"
	defaultRepoName           = "daz"
	defaultAPIBaseURL         = "https://api.github.com"
	defaultTitlePrefix        = "bug report"
	defaultCooldownMinutes    = 60
	defaultRequestTimeoutSecs = 20
	defaultMaxCommentLength   = 700
	chatHistoryLimit          = 50
	issueContextLines         = 10
)

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	config Config

	now             func() time.Time
	httpClient      *http.Client
	createIssueFunc func(ctx context.Context, report bugReport) (*createdIssue, error)

	cooldownMu       sync.Mutex
	nonAdminCooldown map[string]time.Time

	chatMu     sync.RWMutex
	recentChat map[string][]chatLine
}

type Config struct {
	RepoOwner          string   `json:"repo_owner"`
	RepoName           string   `json:"repo_name"`
	APIBaseURL         string   `json:"api_base_url"`
	TitlePrefix        string   `json:"title_prefix"`
	CooldownMinutes    int      `json:"cooldown_minutes"`
	RequestTimeoutSecs int      `json:"request_timeout_seconds"`
	MaxCommentLength   int      `json:"max_comment_length"`
	Labels             []string `json:"labels"`
}

type bugReport struct {
	Channel   string
	Username  string
	Comment   string
	CreatedAt time.Time
}

type chatLine struct {
	Username string
	Message  string
}

type createdIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

type githubErrorResponse struct {
	Message string `json:"message"`
}

type githubStatusError struct {
	StatusCode int
	Message    string
}

func (e *githubStatusError) Error() string {
	return fmt.Sprintf("github api error (%d): %s", e.StatusCode, e.Message)
}

func New() framework.Plugin {
	return &Plugin{
		name:             "bug",
		now:              time.Now,
		nonAdminCooldown: make(map[string]time.Time),
		recentChat:       make(map[string][]chatLine),
	}
}

func (p *Plugin) Init(rawConfig json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.config = Config{
		RepoOwner:          defaultRepoOwner,
		RepoName:           defaultRepoName,
		APIBaseURL:         defaultAPIBaseURL,
		TitlePrefix:        defaultTitlePrefix,
		CooldownMinutes:    defaultCooldownMinutes,
		RequestTimeoutSecs: defaultRequestTimeoutSecs,
		MaxCommentLength:   defaultMaxCommentLength,
	}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal bug config: %w", err)
		}
	}

	p.config.RepoOwner = strings.TrimSpace(p.config.RepoOwner)
	p.config.RepoName = strings.TrimSpace(p.config.RepoName)
	p.config.APIBaseURL = strings.TrimSpace(p.config.APIBaseURL)
	p.config.TitlePrefix = strings.TrimSpace(p.config.TitlePrefix)
	if p.config.RepoOwner == "" {
		p.config.RepoOwner = defaultRepoOwner
	}
	if p.config.RepoName == "" {
		p.config.RepoName = defaultRepoName
	}
	if p.config.APIBaseURL == "" {
		p.config.APIBaseURL = defaultAPIBaseURL
	}
	if p.config.TitlePrefix == "" {
		p.config.TitlePrefix = defaultTitlePrefix
	}
	if p.config.CooldownMinutes <= 0 {
		p.config.CooldownMinutes = defaultCooldownMinutes
	}
	if p.config.RequestTimeoutSecs <= 0 {
		p.config.RequestTimeoutSecs = defaultRequestTimeoutSecs
	}
	if p.config.MaxCommentLength <= 0 {
		p.config.MaxCommentLength = defaultMaxCommentLength
	}
	p.config.Labels = normalizeLabels(p.config.Labels)

	if p.httpClient == nil {
		p.httpClient = &http.Client{Timeout: time.Duration(p.config.RequestTimeoutSecs) * time.Second}
	}
	if p.createIssueFunc == nil {
		p.createIssueFunc = p.createIssueFromReport
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.nonAdminCooldown == nil {
		p.nonAdminCooldown = make(map[string]time.Time)
	}
	if p.recentChat == nil {
		p.recentChat = make(map[string][]chatLine)
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

	if err := p.eventBus.Subscribe("command.bug.execute", p.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command.bug.execute: %w", err)
	}
	if err := p.eventBus.Subscribe(eventbus.EventCytubeChatMsg, p.handleChatMessage); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", eventbus.EventCytubeChatMsg, err)
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

func (p *Plugin) handleChatMessage(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
		return nil
	}
	msg := dataEvent.Data.ChatMessage
	channel := strings.TrimSpace(msg.Channel)
	username := strings.TrimSpace(msg.Username)
	text := strings.TrimSpace(msg.Message)
	if channel == "" || username == "" || text == "" {
		return nil
	}
	p.appendRecentChat(channel, username, text)
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
					"commands":    "bug",
					"min_rank":    "0",
					"description": "file a bug report as a GitHub issue",
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

	params := req.Data.Command.Params
	username := strings.TrimSpace(params["username"])
	channel := strings.TrimSpace(params["channel"])
	isPM := params["is_pm"] == "true"
	isAdmin := params["is_admin"] == "true"

	if username == "" || channel == "" {
		return nil
	}

	comment := strings.TrimSpace(strings.Join(req.Data.Command.Args, " "))
	if comment == "" {
		p.sendResponse(username, channel, isPM, "usage: !bug <comment>")
		return nil
	}
	if len(comment) > p.config.MaxCommentLength {
		p.sendResponse(
			username,
			channel,
			isPM,
			fmt.Sprintf(
				"comment is too long (%d chars). max is %d.",
				len(comment),
				p.config.MaxCommentLength,
			),
		)
		return nil
	}

	if !isAdmin {
		allowed, wait := p.canRunNonAdmin(channel, username, p.now())
		if !allowed {
			p.sendResponse(
				username,
				channel,
				isPM,
				fmt.Sprintf("bug reporting cooldown active. try again in %s.", formatCooldown(wait)),
			)
			return nil
		}
	}

	report := bugReport{
		Channel:   channel,
		Username:  username,
		Comment:   comment,
		CreatedAt: p.now().UTC(),
	}

	ctx := p.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(p.config.RequestTimeoutSecs)*time.Second)
	defer cancel()

	issue, err := p.createIssueFunc(timeoutCtx, report)
	if err != nil {
		p.sendResponse(username, channel, isPM, fmt.Sprintf("failed to file bug issue: %s", safeErrMessage(err)))
		return nil
	}

	if !isAdmin {
		p.markNonAdminRun(channel, username, p.now())
	}

	successMessage := fmt.Sprintf("bug filed as issue #%d: %s", issue.Number, issue.HTMLURL)
	if isPM {
		p.sendResponse(username, channel, true, successMessage)
		return nil
	}

	// For public commands, deliver the link privately to avoid channel noise.
	p.sendResponse(username, channel, true, successMessage)
	p.sendResponse(username, channel, false, "bug filed. sent you the issue link in PM.")
	return nil
}

func (p *Plugin) canRunNonAdmin(channel, username string, now time.Time) (bool, time.Duration) {
	p.cooldownMu.Lock()
	defer p.cooldownMu.Unlock()

	key := cooldownKey(channel, username)
	last, ok := p.nonAdminCooldown[key]
	if !ok {
		return true, 0
	}
	until := last.Add(time.Duration(p.config.CooldownMinutes) * time.Minute)
	if !now.Before(until) {
		return true, 0
	}
	return false, until.Sub(now)
}

func (p *Plugin) markNonAdminRun(channel, username string, when time.Time) {
	p.cooldownMu.Lock()
	defer p.cooldownMu.Unlock()
	key := cooldownKey(channel, username)
	p.nonAdminCooldown[key] = when
}

func cooldownKey(channel, username string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + "|" + strings.ToLower(strings.TrimSpace(username))
}

func formatCooldown(remaining time.Duration) string {
	if remaining <= 0 {
		return "0m"
	}
	minutes := int(remaining.Round(time.Minute).Minutes())
	if minutes < 1 {
		minutes = 1
	}
	hours := minutes / 60
	minutes = minutes % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func (p *Plugin) sendResponse(username, channel string, isPM bool, message string) {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}

	if isPM {
		resp := &framework.EventData{
			PluginResponse: &framework.PluginResponse{
				From:    p.name,
				Success: true,
				Data: &framework.ResponseData{
					CommandResult: &framework.CommandResultData{
						Success: true,
						Output:  msg,
					},
					KeyValue: map[string]string{
						"username": username,
						"channel":  channel,
					},
				},
			},
		}
		if err := p.eventBus.Broadcast("plugin.response", resp); err != nil {
			logger.Warn(p.name, "Failed to send PM command response: %v", err)
		}
		return
	}

	chat := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: channel,
			Message: fmt.Sprintf("%s: %s", username, msg),
		},
	}
	if err := p.eventBus.Broadcast("cytube.send", chat); err != nil {
		logger.Warn(p.name, "Failed to send chat response: %v", err)
	}
}

func safeErrMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "unknown error"
	}
	if len(msg) > 220 {
		return msg[:220] + "..."
	}
	return msg
}

func (p *Plugin) createIssueFromReport(ctx context.Context, report bugReport) (*createdIssue, error) {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is not set")
	}

	title, body := p.buildBugIssueAssets(report, p.getRecentChat(report.Channel, issueContextLines))
	issue, err := p.openIssue(ctx, token, title, body)
	if err != nil {
		return nil, err
	}
	return issue, nil
}

func (p *Plugin) buildBugIssueAssets(report bugReport, recent []chatLine) (title, body string) {
	reportedAt := report.CreatedAt.UTC().Format(time.RFC3339)
	shortComment := report.Comment
	if len(shortComment) > 72 {
		shortComment = strings.TrimSpace(shortComment[:72]) + "..."
	}

	title = fmt.Sprintf("%s (%s): %s", p.config.TitlePrefix, report.Channel, shortComment)
	contextBlock := formatChatContext(recent)
	body = strings.TrimSpace(fmt.Sprintf(
		`This issue was auto-generated from chat command !bug.

- Reporter: %s
- Channel: %s
- Reported At (UTC): %s

Original Comment:
%s

Recent Chat Context (last 10 lines):
%s

Please triage and implement a fix.
`,
		report.Username,
		report.Channel,
		reportedAt,
		quoteBlock(report.Comment),
		contextBlock,
	))
	return title, body
}

func (p *Plugin) openIssue(ctx context.Context, token, title, body string) (*createdIssue, error) {
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	if len(p.config.Labels) > 0 {
		payload["labels"] = p.config.Labels
	}

	path := fmt.Sprintf("/repos/%s/%s/issues", url.PathEscape(p.config.RepoOwner), url.PathEscape(p.config.RepoName))
	var issue createdIssue
	if err := p.githubRequest(ctx, token, http.MethodPost, path, payload, &issue); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	if issue.Number <= 0 || strings.TrimSpace(issue.HTMLURL) == "" {
		return nil, fmt.Errorf("create issue: github response missing issue metadata")
	}
	return &issue, nil
}

func (p *Plugin) githubRequest(
	ctx context.Context,
	token, method, path string,
	payload any,
	out any,
) error {
	apiBase := strings.TrimRight(p.config.APIBaseURL, "/")
	if apiBase == "" {
		apiBase = defaultAPIBaseURL
	}
	endpoint := path
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	fullURL := apiBase + endpoint

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal github request payload: %w", err)
		}
		body = strings.NewReader(string(raw))
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("create github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(bodyText))
		var ghErr githubErrorResponse
		if err := json.Unmarshal(bodyText, &ghErr); err == nil && strings.TrimSpace(ghErr.Message) != "" {
			message = ghErr.Message
		}
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return &githubStatusError{
			StatusCode: resp.StatusCode,
			Message:    message,
		}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}
	return nil
}

func quoteBlock(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + trimmed
	}
	return strings.Join(lines, "\n")
}

func normalizeLabels(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(labels))
	normalized := make([]string, 0, len(labels))
	for _, label := range labels {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, clean)
	}
	return normalized
}

func (p *Plugin) appendRecentChat(channel, username, message string) {
	channelKey := strings.ToLower(strings.TrimSpace(channel))
	if channelKey == "" {
		return
	}
	line := chatLine{
		Username: strings.TrimSpace(username),
		Message:  strings.TrimSpace(message),
	}
	if line.Username == "" || line.Message == "" {
		return
	}

	p.chatMu.Lock()
	defer p.chatMu.Unlock()
	lines := append(p.recentChat[channelKey], line)
	if len(lines) > chatHistoryLimit {
		lines = append([]chatLine(nil), lines[len(lines)-chatHistoryLimit:]...)
	}
	p.recentChat[channelKey] = lines
}

func (p *Plugin) getRecentChat(channel string, limit int) []chatLine {
	if limit <= 0 {
		return nil
	}
	channelKey := strings.ToLower(strings.TrimSpace(channel))
	if channelKey == "" {
		return nil
	}

	p.chatMu.RLock()
	defer p.chatMu.RUnlock()
	lines := p.recentChat[channelKey]
	if len(lines) == 0 {
		return nil
	}
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}
	copied := make([]chatLine, len(lines[start:]))
	copy(copied, lines[start:])
	return copied
}

func formatChatContext(lines []chatLine) string {
	if len(lines) == 0 {
		return "- (no recent chat captured)"
	}
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		user := strings.TrimSpace(line.Username)
		msg := strings.TrimSpace(line.Message)
		if user == "" || msg == "" {
			continue
		}
		msg = strings.Join(strings.Fields(strings.ReplaceAll(msg, "\n", " ")), " ")
		if len(msg) > 180 {
			msg = msg[:177] + "..."
		}
		formatted = append(formatted, fmt.Sprintf("- %s: %s", user, msg))
	}
	if len(formatted) == 0 {
		return "- (no recent chat captured)"
	}
	return strings.Join(formatted, "\n")
}
