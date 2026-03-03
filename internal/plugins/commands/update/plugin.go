package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
)

const (
	defaultUpdateRepoPath       = "."
	defaultUpdateBranch         = "master"
	defaultUpdateRemote         = "origin"
	defaultBuildCommand         = "make build"
	defaultRestartCommand       = "systemctl restart daz"
	defaultUpdateTimeoutSeconds = 300
	maxOutputBytes              = 800
	defaultLogFileName          = "update.log"
	defaultLogDir               = "internal/plugins/commands/update"
)

type Plugin struct {
	name      string
	eventBus  framework.EventBus
	running   bool
	startTime time.Time

	mu     sync.RWMutex
	logMu  sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	config   Config
	logFile  string
	updating bool

	runUpdateFlow     func(channel, username string)
	runGitCommandFunc func(ctx context.Context, workingDir string, args ...string) error
	runCommandFunc    func(ctx context.Context, workingDir string, command ...string) error
	commandOutputFunc func(ctx context.Context, workingDir string, command ...string) (string, error)
}

type Config struct {
	RepoPath             string   `json:"repo_path"`
	Branch               string   `json:"branch"`
	Remote               string   `json:"remote"`
	BuildCommand         []string `json:"build_command"`
	RestartCommand       []string `json:"restart_command"`
	LogFilePath          string   `json:"log_file"`
	OperationTimeoutSecs int      `json:"operation_timeout_seconds"`
}

type commitInfo struct {
	full  string
	short string
	subj  string
}

func New() framework.Plugin {
	return &Plugin{
		name: "update",
	}
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.config = Config{
		RepoPath:             defaultUpdateRepoPath,
		Branch:               defaultUpdateBranch,
		Remote:               defaultUpdateRemote,
		OperationTimeoutSecs: defaultUpdateTimeoutSeconds,
	}
	if len(config) != 0 {
		if err := json.Unmarshal(config, &p.config); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	if p.config.RepoPath == "" {
		p.config.RepoPath = defaultUpdateRepoPath
	}
	if p.config.Branch == "" {
		p.config.Branch = defaultUpdateBranch
	}
	if p.config.Remote == "" {
		p.config.Remote = defaultUpdateRemote
	}
	if p.config.OperationTimeoutSecs <= 0 {
		p.config.OperationTimeoutSecs = defaultUpdateTimeoutSeconds
	}
	if strings.TrimSpace(p.config.LogFilePath) == "" {
		p.config.LogFilePath = defaultLogFilePath()
	}

	if len(p.config.BuildCommand) == 0 {
		p.config.BuildCommand = strings.Split(defaultBuildCommand, " ")
	}
	if len(p.config.RestartCommand) == 0 {
		p.config.RestartCommand = strings.Split(defaultRestartCommand, " ")
	}

	absRepoPath, err := filepath.Abs(p.config.RepoPath)
	if err != nil {
		return fmt.Errorf("failed to resolve repository path: %w", err)
	}
	p.config.RepoPath = absRepoPath
	p.logFile = p.config.LogFilePath

	return nil
}

func (p *Plugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("plugin already running")
	}
	p.running = true
	p.startTime = time.Now()
	p.mu.Unlock()

	p.logf("INFO", "update plugin started")

	if err := p.eventBus.Subscribe("command.update.execute", p.handleCommand); err != nil {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		p.logf("ERROR", "failed to subscribe to command.update.execute: %v", err)
		return fmt.Errorf("failed to subscribe to command.update.execute: %w", err)
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

	p.cancel()
	p.wg.Wait()

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

	return framework.PluginStatus{
		Name:   p.name,
		State:  state,
		Uptime: time.Since(p.startTime),
	}
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
					"commands":    "update",
					"min_rank":    "0",
					"admin_only":  "true",
					"description": "pull latest code, rebuild, and restart daz",
				},
			},
		},
	}

	if err := p.eventBus.Broadcast("command.register", regEvent); err != nil {
		logger.Error(p.name, "Failed to register update command: %v", err)
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

	if req.Data.Command.Params["is_pm"] == "true" {
		p.sendPM(
			req.Data.Command.Params["username"],
			"(!update is chat-only and must be sent in room chat.)",
		)
		return nil
	}

	username := strings.TrimSpace(req.Data.Command.Params["username"])
	channel := strings.TrimSpace(req.Data.Command.Params["channel"])

	if username == "" || channel == "" {
		return nil
	}

	if !p.takeUpdateLock() {
		p.sendChat(channel, "Update already in progress.")
		p.logf("INFO", "update command rejected: already in progress (%s)", channel)
		return nil
	}

	p.sendChat(channel, fmt.Sprintf("%s: Updating bot from git remote now, please stand by...", username))
	p.logf("INFO", "update requested by %s in %s", username, channel)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.releaseUpdateLock()
		runner := p.runUpdateFlow
		if runner == nil {
			runner = p.runUpdateCycle
		}
		runner(channel, username)
	}()
	return nil
}

func (p *Plugin) runUpdateCycle(channel, username string) {
	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(p.config.OperationTimeoutSecs)*time.Second)
	defer cancel()
	p.logf("INFO", "update cycle started for %s in %s", username, channel)

	original, err := p.getCommitInfo(ctx, p.config.RepoPath)
	if err != nil {
		p.logf("ERROR", "failed to read current commit before update for %s: %v", username, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed, unable to read current commit: %v", username, err))
		return
	}

	if err := p.runGitCommand(ctx, p.config.RepoPath, "fetch", p.config.Remote, "--prune"); err != nil {
		p.logf("ERROR", "git fetch failed for %s: %v", username, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed while fetching remote: %v", username, err))
		return
	}

	remoteRef := fmt.Sprintf("%s/%s", p.config.Remote, p.config.Branch)
	if err := p.runGitCommand(ctx, p.config.RepoPath, "checkout", "-B", p.config.Branch, remoteRef); err != nil {
		p.logf("ERROR", "git checkout failed for %s (%s): %v", username, remoteRef, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed while checking out %s: %v", username, remoteRef, err))
		return
	}

	if err := p.runGitCommand(ctx, p.config.RepoPath, "reset", "--hard", remoteRef); err != nil {
		p.logf("ERROR", "git reset failed for %s (%s): %v", username, remoteRef, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed while resetting branch: %v", username, err))
		return
	}

	current, err := p.getCommitInfo(ctx, p.config.RepoPath)
	if err != nil {
		p.logf("ERROR", "failed to read commit after update for %s: %v", username, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed after sync, unable to read commit: %v", username, err))
		return
	}

	if current.full == original.full {
		p.logf("INFO", "update complete for %s no-op: commit %s", username, current.short)
		p.sendChat(channel, fmt.Sprintf("%s: already up to date on commit %s — %s. No restart needed.", username, current.short, current.subj))
		return
	}

	if err := p.runCommand(ctx, p.config.RepoPath, p.config.BuildCommand...); err != nil {
		p.logf("ERROR", "build failed for %s (%s): %v", username, current.short, err)
		p.sendChat(channel, fmt.Sprintf("%s: Update failed while rebuilding: %v", username, err))
		return
	}

	p.sendChat(channel, fmt.Sprintf("%s: pulled commit %s — %s. Restarting daz service now.", username, current.short, current.subj))
	p.logf("INFO", "update built new commit for %s: %s", username, current.short)

	if err := p.runCommand(ctx, p.config.RepoPath, p.config.RestartCommand...); err != nil {
		if isExpectedRestartInterruption(err) {
			p.logf("INFO", "restart command interrupted for %s after commit %s (expected during self-restart): %v", username, current.short, err)
			return
		}
		p.logf("ERROR", "restart failed for %s after commit %s: %v", username, current.short, err)
		p.sendChat(channel, fmt.Sprintf("%s: Build succeeded for %s, but restart failed: %v", username, current.short, err))
		return
	}

	p.logf("INFO", "update complete for %s at %s", username, current.short)
	p.sendChat(channel, fmt.Sprintf("%s: Update complete. Running commit %s — %s", username, current.short, current.subj))
}

func (p *Plugin) runUpdateCommand(command []string, dir string, ctx context.Context) error {
	if p.runCommandFunc != nil {
		return p.runCommandFunc(ctx, dir, command...)
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if len(out) > maxOutputBytes {
			out = out[:maxOutputBytes] + "..."
		}
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func (p *Plugin) runCommand(ctx context.Context, workingDir string, command ...string) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}
	if p.runCommandFunc != nil {
		return p.runCommandFunc(ctx, workingDir, command...)
	}
	return p.runUpdateCommand(command, workingDir, ctx)
}

func (p *Plugin) runGitCommand(ctx context.Context, workingDir string, args ...string) error {
	if p.runGitCommandFunc != nil {
		return p.runGitCommandFunc(ctx, workingDir, args...)
	}
	fullCommand := append([]string{"git"}, args...)
	return p.runCommand(ctx, workingDir, fullCommand...)
}

func (p *Plugin) getCommitInfo(ctx context.Context, workingDir string) (commitInfo, error) {
	raw, err := p.runCommandOutput(ctx, workingDir, "git", "log", "-1", "--pretty=format:%H%x00%s")
	if err != nil {
		return commitInfo{}, fmt.Errorf("query current commit: %w", err)
	}

	parts := strings.SplitN(raw, "\x00", 2)
	if len(parts) < 2 {
		return commitInfo{}, fmt.Errorf("malformed git output: %q", raw)
	}

	short, err := p.runCommandOutput(ctx, workingDir, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return commitInfo{}, fmt.Errorf("query short commit: %w", err)
	}

	return commitInfo{
		full:  strings.TrimSpace(parts[0]),
		short: strings.TrimSpace(short),
		subj:  strings.TrimSpace(parts[1]),
	}, nil
}

func (p *Plugin) runCommandOutput(ctx context.Context, workingDir string, command ...string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("empty command")
	}
	if p.commandOutputFunc != nil {
		return p.commandOutputFunc(ctx, workingDir, command...)
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if len(out) > maxOutputBytes {
			out = out[:maxOutputBytes] + "..."
		}
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return strings.TrimSpace(string(output)), nil
}

func isExpectedRestartInterruption(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			sig := status.Signal()
			if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
				return true
			}
		}
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "signal: killed") || strings.Contains(msg, "signal: terminated")
}

func (p *Plugin) sendChat(channel, message string) {
	if channel == "" {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	p.logf("INFO", "chat output to %s: %s", channel, msg)

	data := &framework.EventData{
		RawMessage: &framework.RawMessageData{
			Channel: channel,
			Message: msg,
		},
	}

	if err := p.eventBus.Broadcast("cytube.send", data); err != nil {
		logger.Warn(p.name, "Failed to send update status message to chat: %v", err)
	}
}

func (p *Plugin) sendPM(username, message string) {
	if strings.TrimSpace(username) == "" {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return
	}
	p.logf("INFO", "pm output to %s: %s", username, msg)

	data := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			ToUser:  username,
			Message: msg,
		},
	}
	if err := p.eventBus.Broadcast("cytube.send.pm", data); err != nil {
		logger.Warn(p.name, "Failed to send update PM to %s: %v", username, err)
	}
}

func (p *Plugin) takeUpdateLock() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.updating {
		return false
	}

	p.updating = true
	return true
}

func (p *Plugin) releaseUpdateLock() {
	p.mu.Lock()
	p.updating = false
	p.mu.Unlock()
}

func (p *Plugin) logf(level, format string, args ...any) {
	if level == "" {
		level = "INFO"
	}
	message := fmt.Sprintf(format, args...)
	p.appendLogLine(level, message)
	switch strings.ToUpper(level) {
	case "ERROR", "WARN", "WARNING":
		logger.Warn("update", "%s", message)
	case "DEBUG":
		logger.Debug("update", "%s", message)
	case "INFO":
		logger.Info("update", "%s", message)
	default:
		logger.Info("update", "%s", message)
	}
}

func (p *Plugin) appendLogLine(level, message string) {
	if p.logFile == "" {
		return
	}

	p.logMu.Lock()
	defer p.logMu.Unlock()

	normalizedLevel := strings.ToUpper(level)
	if normalizedLevel == "" {
		normalizedLevel = "INFO"
	}

	logPath := strings.TrimSpace(p.logFile)
	if logPath == "" {
		return
	}
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, fs.FileMode(0o755)); err != nil {
		logger.Warn("update", "failed to create update log directory %q: %v", dir, err)
		return
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		logger.Warn("update", "failed to open update log file %q: %v", logPath, err)
		return
	}
	defer func() {
		_ = f.Close()
	}()

	timestamp := time.Now().Format(time.RFC3339)
	_, err = fmt.Fprintf(f, "[%s] [%s] %s\n", timestamp, normalizedLevel, message)
	if err != nil {
		logger.Warn("update", "failed to write update log entry to %q: %v", logPath, err)
	}
}

func defaultLogFilePath() string {
	if execPath, err := os.Executable(); err == nil {
		candidate := filepath.Clean(filepath.Join(filepath.Dir(execPath), "..", defaultLogDir, defaultLogFileName))
		return filepath.Clean(candidate)
	}

	if cwd, err := os.Getwd(); err == nil {
		return filepath.Clean(filepath.Join(cwd, defaultLogDir, defaultLogFileName))
	}

	return filepath.Join(os.TempDir(), defaultLogFileName)
}
