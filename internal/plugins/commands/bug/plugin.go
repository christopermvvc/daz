package bug

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

const (
	defaultRepoOwner          = "hildolfr"
	defaultRepoName           = "daz"
	defaultBaseBranch         = "master"
	defaultAPIBaseURL         = "https://api.github.com"
	defaultTitlePrefix        = "bug report"
	defaultCooldownMinutes    = 60
	defaultRequestTimeoutSecs = 20
	defaultMaxCommentLength   = 700
)

var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

type Plugin struct {
	name     string
	eventBus framework.EventBus
	running  bool

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	config Config

	now          func() time.Time
	httpClient   *http.Client
	createPRFunc func(ctx context.Context, report bugReport) (*createdPR, error)

	cooldownMu       sync.Mutex
	nonAdminCooldown map[string]time.Time
}

type Config struct {
	RepoOwner          string `json:"repo_owner"`
	RepoName           string `json:"repo_name"`
	BaseBranch         string `json:"base_branch"`
	APIBaseURL         string `json:"api_base_url"`
	TitlePrefix        string `json:"title_prefix"`
	CooldownMinutes    int    `json:"cooldown_minutes"`
	RequestTimeoutSecs int    `json:"request_timeout_seconds"`
	MaxCommentLength   int    `json:"max_comment_length"`
}

type bugReport struct {
	Channel   string
	Username  string
	Comment   string
	CreatedAt time.Time
}

type createdPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

type githubRefResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
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
	}
}

func (p *Plugin) Init(rawConfig json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.config = Config{
		RepoOwner:          defaultRepoOwner,
		RepoName:           defaultRepoName,
		BaseBranch:         defaultBaseBranch,
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
	p.config.BaseBranch = strings.TrimSpace(p.config.BaseBranch)
	p.config.APIBaseURL = strings.TrimSpace(p.config.APIBaseURL)
	p.config.TitlePrefix = strings.TrimSpace(p.config.TitlePrefix)
	if p.config.RepoOwner == "" {
		p.config.RepoOwner = defaultRepoOwner
	}
	if p.config.RepoName == "" {
		p.config.RepoName = defaultRepoName
	}
	if p.config.BaseBranch == "" {
		p.config.BaseBranch = defaultBaseBranch
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

	if p.httpClient == nil {
		p.httpClient = &http.Client{Timeout: time.Duration(p.config.RequestTimeoutSecs) * time.Second}
	}
	if p.createPRFunc == nil {
		p.createPRFunc = p.createPRFromReport
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.nonAdminCooldown == nil {
		p.nonAdminCooldown = make(map[string]time.Time)
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
					"commands":    "bug",
					"min_rank":    "0",
					"description": "file a bug report as a GitHub pull request",
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

	pr, err := p.createPRFunc(timeoutCtx, report)
	if err != nil {
		p.sendResponse(username, channel, isPM, fmt.Sprintf("failed to file bug PR: %s", safeErrMessage(err)))
		return nil
	}

	if !isAdmin {
		p.markNonAdminRun(channel, username, p.now())
	}

	p.sendResponse(
		username,
		channel,
		isPM,
		fmt.Sprintf("bug filed as PR #%d: %s", pr.Number, pr.HTMLURL),
	)
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

func (p *Plugin) createPRFromReport(ctx context.Context, report bugReport) (*createdPR, error) {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is not set")
	}

	baseSHA, err := p.fetchBaseBranchSHA(ctx, token)
	if err != nil {
		return nil, err
	}

	branchName, err := p.createUniqueBranch(ctx, token, report, baseSHA)
	if err != nil {
		return nil, err
	}

	path, title, fileContent, prBody := p.buildBugReportAssets(report)
	if err := p.commitReportFile(ctx, token, branchName, path, fileContent, report); err != nil {
		return nil, err
	}

	pr, err := p.openPullRequest(ctx, token, branchName, title, prBody)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (p *Plugin) fetchBaseBranchSHA(ctx context.Context, token string) (string, error) {
	path := fmt.Sprintf(
		"/repos/%s/%s/git/ref/heads/%s",
		url.PathEscape(p.config.RepoOwner),
		url.PathEscape(p.config.RepoName),
		url.PathEscape(p.config.BaseBranch),
	)
	var resp githubRefResponse
	if err := p.githubRequest(ctx, token, http.MethodGet, path, nil, &resp); err != nil {
		return "", fmt.Errorf("read base branch ref: %w", err)
	}
	sha := strings.TrimSpace(resp.Object.SHA)
	if sha == "" {
		return "", fmt.Errorf("empty base branch sha for %s", p.config.BaseBranch)
	}
	return sha, nil
}

func (p *Plugin) createUniqueBranch(ctx context.Context, token string, report bugReport, baseSHA string) (string, error) {
	baseSlug := slugFromText(report.Comment, 32)
	if baseSlug == "" {
		baseSlug = "report"
	}
	ts := report.CreatedAt.Format("20060102-150405")

	for attempt := 0; attempt < 5; attempt++ {
		suffix, err := randomSuffix(3)
		if err != nil {
			return "", fmt.Errorf("generate branch suffix: %w", err)
		}
		branch := fmt.Sprintf("daz/bug/%s-%s-%s", ts, baseSlug, suffix)

		path := fmt.Sprintf("/repos/%s/%s/git/refs", url.PathEscape(p.config.RepoOwner), url.PathEscape(p.config.RepoName))
		payload := map[string]string{
			"ref": "refs/heads/" + branch,
			"sha": baseSHA,
		}
		err = p.githubRequest(ctx, token, http.MethodPost, path, payload, nil)
		if err == nil {
			return branch, nil
		}

		var statusErr *githubStatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusUnprocessableEntity {
			continue
		}
		return "", fmt.Errorf("create branch %q: %w", branch, err)
	}

	return "", fmt.Errorf("failed to create unique branch after retries")
}

func (p *Plugin) buildBugReportAssets(report bugReport) (path, title, fileContent, prBody string) {
	reporterSlug := slugFromText(report.Username, 24)
	if reporterSlug == "" {
		reporterSlug = "unknown"
	}
	commentSlug := slugFromText(report.Comment, 40)
	if commentSlug == "" {
		commentSlug = "report"
	}
	reportedAt := report.CreatedAt.UTC().Format(time.RFC3339)
	stamp := report.CreatedAt.UTC().Format("20060102_150405")
	path = fmt.Sprintf("data/bugreports/%s_%s_%s.md", stamp, reporterSlug, commentSlug)

	quotedComment := quoteBlock(report.Comment)
	fileContent = strings.TrimSpace(fmt.Sprintf(
		`# Bug Report

- Reporter: @%s
- Channel: %s
- Reported At (UTC): %s
- Source Command: !bug

## Comment
%s

## Triage Notes
- Auto-generated by the daz bug command plugin.
`,
		report.Username,
		report.Channel,
		reportedAt,
		quotedComment,
	))

	shortComment := report.Comment
	if len(shortComment) > 72 {
		shortComment = strings.TrimSpace(shortComment[:72]) + "..."
	}
	title = fmt.Sprintf("%s (%s): %s", p.config.TitlePrefix, report.Channel, shortComment)

	prBody = strings.TrimSpace(fmt.Sprintf(
		`This PR was auto-generated from chat command !bug.

- Reporter: @%s
- Channel: %s
- Reported At (UTC): %s

Original Comment:
%s

Please triage and implement a fix.
`,
		report.Username,
		report.Channel,
		reportedAt,
		quotedComment,
	))
	return path, title, fileContent, prBody
}

func (p *Plugin) commitReportFile(
	ctx context.Context,
	token, branchName, path, fileContent string,
	report bugReport,
) error {
	commitMessage := fmt.Sprintf("chore(bug): record report from %s in %s", report.Username, report.Channel)
	payload := map[string]string{
		"message": commitMessage,
		"content": base64.StdEncoding.EncodeToString([]byte(fileContent)),
		"branch":  branchName,
	}
	endpoint := fmt.Sprintf(
		"/repos/%s/%s/contents/%s",
		url.PathEscape(p.config.RepoOwner),
		url.PathEscape(p.config.RepoName),
		url.PathEscape(path),
	)
	if err := p.githubRequest(ctx, token, http.MethodPut, endpoint, payload, nil); err != nil {
		return fmt.Errorf("commit report file: %w", err)
	}
	return nil
}

func (p *Plugin) openPullRequest(ctx context.Context, token, branchName, title, prBody string) (*createdPR, error) {
	payload := map[string]any{
		"title":                 title,
		"head":                  branchName,
		"base":                  p.config.BaseBranch,
		"body":                  prBody,
		"maintainer_can_modify": true,
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(p.config.RepoOwner), url.PathEscape(p.config.RepoName))
	var pr createdPR
	if err := p.githubRequest(ctx, token, http.MethodPost, path, payload, &pr); err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}
	if pr.Number <= 0 || strings.TrimSpace(pr.HTMLURL) == "" {
		return nil, fmt.Errorf("create pull request: github response missing PR metadata")
	}
	return &pr, nil
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

func slugFromText(text string, max int) string {
	s := strings.ToLower(strings.TrimSpace(text))
	s = slugRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if max > 0 && len(s) > max {
		s = strings.Trim(s[:max], "-")
	}
	return s
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

func randomSuffix(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 2
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
