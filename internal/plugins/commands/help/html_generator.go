package help

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/logger"
)

const (
	defaultHelpOutputPath = "./data/help"
	defaultHelpBaseURL    = "https://hildolfr.github.io/daz/help/"
	helpMarkerFile        = ".daz_help_marker"
)

type HTMLGenerator struct {
	config            *Config
	entryProvider     func() []*commandEntry
	showAliases       bool
	includeRestricted bool
	mu                sync.Mutex

	gitRunner         gitRunner
	deployKeyResolver func() (string, error)
}

type gitRunner func(ctx context.Context, dir string, env []string, args ...string) (string, error)

func NewHTMLGenerator(config *Config, entryProvider func() []*commandEntry, showAliases bool, includeRestricted bool) *HTMLGenerator {
	return &HTMLGenerator{
		config:            config,
		entryProvider:     entryProvider,
		showAliases:       showAliases,
		includeRestricted: includeRestricted,
		gitRunner:         defaultGitRunner,
		deployKeyResolver: defaultDeployKeyResolver,
	}
}

func defaultGitRunner(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func defaultDeployKeyResolver() (string, error) {
	candidates := []string{
		"dazza_deploy_key",
	}
	cwd, err := os.Getwd()
	if err == nil {
		candidates = append([]string{filepath.Join(cwd, "dazza_deploy_key")}, candidates...)
	}
	executablePath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(executablePath)
		candidates = append(candidates, filepath.Join(execDir, "dazza_deploy_key"))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("deploy key not found")
}

func normalizeHelpBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = defaultHelpBaseURL
	}
	if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		trimmed = "https://" + trimmed
	}
	if !strings.HasSuffix(trimmed, "/") {
		trimmed += "/"
	}
	return trimmed
}

func normalizeHelpOutputPath(raw string) string {
	fallback := mustResolveHelpFallbackPath()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	outputPath := trimmed
	if outputPath == "~" || strings.HasPrefix(outputPath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil || homeDir == "" {
			logger.Warn("help", "Failed to resolve home directory for html output path %q, using %s", raw, fallback)
			return fallback
		}
		if outputPath == "~" {
			outputPath = homeDir
		} else {
			outputPath = filepath.Join(homeDir, outputPath[2:])
		}
	}

	outputPath = filepath.Clean(outputPath)
	if outputPath == "" || outputPath == "." || outputPath == string(filepath.Separator) {
		logger.Warn("help", "Unsafe html output path %q configured, using %s", raw, fallback)
		return fallback
	}

	if !filepath.IsAbs(outputPath) {
		absPath, err := filepath.Abs(outputPath)
		if err != nil {
			logger.Warn("help", "Failed to resolve html output path %q: %v; using %s", raw, err, fallback)
			return fallback
		}
		outputPath = absPath
	}

	if outputPath == string(filepath.Separator) {
		logger.Warn("help", "Unsafe html output path %q configured, using %s", raw, fallback)
		return fallback
	}

	return outputPath
}

func mustResolveHelpFallbackPath() string {
	fallbackAbs, err := filepath.Abs(defaultHelpOutputPath)
	if err != nil {
		logger.Warn("help", "Failed to resolve default html output path %s: %v", defaultHelpOutputPath, err)
		return filepath.Clean(defaultHelpOutputPath)
	}
	return fallbackAbs
}

func (g *HTMLGenerator) GenerateAll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.ensureMarkerFile(); err != nil {
		return err
	}
	if err := os.MkdirAll(g.config.HTMLOutputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	entries := g.entryProvider()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Primary < entries[j].Primary
	})

	content, err := g.renderIndex(entries)
	if err != nil {
		return err
	}

	outputFile := filepath.Join(g.config.HTMLOutputPath, "index.html")
	if err := os.WriteFile(outputFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write help HTML: %w", err)
	}

	if err := g.pushToGitHub(ctx); err != nil {
		logger.Warn("help", "Failed to push help HTML (continuing anyway): %v", err)
	}

	logger.Info("help", "Help HTML generation completed")
	return nil
}

type helpEntryView struct {
	Primary     string
	Description string
	Aliases     string
	MinRank     int
	AdminOnly   bool
}

type helpIndexView struct {
	GeneratedAt       string
	Entries           []helpEntryView
	IncludeRestricted bool
}

func (g *HTMLGenerator) renderIndex(entries []*commandEntry) (string, error) {
	views := make([]helpEntryView, 0, len(entries))
	for _, entry := range entries {
		aliases := ""
		if g.showAliases && len(entry.Aliases) > 0 {
			aliases = strings.Join(entry.Aliases, ", ")
		}
		description := entry.Description
		if g.includeRestricted {
			if entry.AdminOnly {
				description = description + " (admin only)"
			} else if entry.MinRank > 0 {
				description = fmt.Sprintf("%s (min rank %d)", description, entry.MinRank)
			}
		}
		views = append(views, helpEntryView{
			Primary:     entry.Primary,
			Description: description,
			Aliases:     aliases,
			MinRank:     entry.MinRank,
			AdminOnly:   entry.AdminOnly,
		})
	}

	data := helpIndexView{
		GeneratedAt:       time.Now().Format(time.RFC1123),
		Entries:           views,
		IncludeRestricted: g.includeRestricted,
	}

	tmpl, err := template.New("help_index").Parse(helpIndexTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse help template: %w", err)
	}

	var builder strings.Builder
	if err := tmpl.Execute(&builder, data); err != nil {
		return "", fmt.Errorf("failed to render help template: %w", err)
	}

	return builder.String(), nil
}

func (g *HTMLGenerator) markerFilePath() string {
	return filepath.Join(filepath.Clean(g.config.HTMLOutputPath), helpMarkerFile)
}

func (g *HTMLGenerator) ensureMarkerFile() error {
	if !g.isSafeOutputPath() {
		return fmt.Errorf("unsafe html output path: %s", g.config.HTMLOutputPath)
	}
	markerPath := g.markerFilePath()
	gitDir := filepath.Join(filepath.Clean(g.config.HTMLOutputPath), ".git")
	if _, err := os.Stat(gitDir); err == nil {
		if _, err := os.Stat(markerPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("help marker missing in existing git directory: %s", g.config.HTMLOutputPath)
			}
			return fmt.Errorf("failed to stat help marker: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat help git directory: %w", err)
	}
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat help marker: %w", err)
	}
	return os.WriteFile(markerPath, []byte("daz help output"), 0644)
}

func (g *HTMLGenerator) hasMarkerFile() bool {
	if _, err := os.Stat(g.markerFilePath()); err != nil {
		return false
	}
	return true
}

func (g *HTMLGenerator) isSafeOutputPath() bool {
	outputPath := filepath.Clean(g.config.HTMLOutputPath)
	if outputPath == "" || outputPath == "." || outputPath == string(filepath.Separator) {
		return false
	}
	if !filepath.IsAbs(outputPath) {
		return false
	}
	return true
}

func (g *HTMLGenerator) isSafeGitDir() bool {
	if !g.isSafeOutputPath() {
		return false
	}
	if !g.hasMarkerFile() {
		return false
	}
	gitDir := filepath.Join(filepath.Clean(g.config.HTMLOutputPath), ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false
	}
	return true
}

func (g *HTMLGenerator) resetGitState() {
	if !g.isSafeGitDir() {
		logger.Warn("help", "Skipping git reset/clean for unsafe output path: %s", g.config.HTMLOutputPath)
		return
	}

	if _, err := g.runGit(context.Background(), nil, "reset", "--hard", "HEAD"); err != nil {
		logger.Error("help", "Failed to reset git state: %v", err)
	}

	if _, err := g.runGit(context.Background(), nil, "clean", "-fd"); err != nil {
		logger.Error("help", "Failed to clean git directory: %v", err)
	}
}

func (g *HTMLGenerator) pushToGitHub(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			logger.Error("help", "Panic during git push: %v", r)
			g.resetGitState()
		}
	}()

	if !g.isSafeOutputPath() {
		return fmt.Errorf("unsafe html output path: %s", g.config.HTMLOutputPath)
	}
	if err := g.ensureMarkerFile(); err != nil {
		return fmt.Errorf("failed to write help marker file: %w", err)
	}

	var needsRecovery bool
	defer func() {
		if needsRecovery {
			logger.Warn("help", "Git operation failed, resetting state")
			g.resetGitState()
		}
	}()

	githubToken := os.Getenv("GITHUB_TOKEN")

	runAuthPush := func() error {
		if githubToken == "" {
			deployKey, err := g.deployKeyResolver()
			if err != nil {
				return fmt.Errorf("no GitHub token configured")
			}
			if _, err := g.runGit(ctx, []string{
				"GIT_TERMINAL_PROMPT=0",
				"GIT_SSH_COMMAND=ssh -i " + deployKey + " -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
			}, "push", "origin", "gh-pages", "--force"); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}
			return nil
		}

		authURL := fmt.Sprintf("https://x-access-token:%s@github.com/hildolfr/daz.git", githubToken)
		if _, err := g.runGit(ctx, []string{"GIT_TERMINAL_PROMPT=0"}, "push", authURL, "gh-pages", "--force"); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		return nil
	}

	gitDir := filepath.Join(g.config.HTMLOutputPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if _, err := g.runGit(ctx, nil, "init"); err != nil {
			return fmt.Errorf("failed to init git repo: %w", err)
		}
		if _, err := g.runGit(ctx, nil, "config", "user.name", "hildolfr"); err != nil {
			return fmt.Errorf("failed to set git user: %w", err)
		}
		if _, err := g.runGit(ctx, nil, "config", "user.email", "svhildolfr@gmail.com"); err != nil {
			return fmt.Errorf("failed to set git email: %w", err)
		}
	}

	remoteURL := "https://github.com/hildolfr/daz.git"
	if githubToken == "" {
		remoteURL = "git@github.com:hildolfr/daz.git"
	}
	if _, err := g.runGit(ctx, nil, "remote", "add", "origin", remoteURL); err != nil {
		if !strings.Contains(err.Error(), "remote origin already exists") {
			logger.Warn("help", "Failed to add remote: %v", err)
		}
	}

	if _, err := g.runGit(ctx, nil, "checkout", "-B", "gh-pages"); err != nil {
		return fmt.Errorf("failed to checkout gh-pages: %w", err)
	}
	if _, err := g.runGit(ctx, nil, "add", "."); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}
	if _, err := g.runGit(ctx, nil, "commit", "-m", "Update help pages"); err != nil {
		if !strings.Contains(err.Error(), "nothing to commit") {
			needsRecovery = true
			return fmt.Errorf("failed to commit help pages: %w", err)
		}
	}
	if err := runAuthPush(); err != nil {
		needsRecovery = true
		return err
	}

	return nil
}

func (g *HTMLGenerator) runGit(ctx context.Context, env []string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	output, err := g.gitRunner(ctx, g.config.HTMLOutputPath, env, args...)
	if err != nil {
		return output, fmt.Errorf("git %v failed: %w, output: %s", args, err, output)
	}
	return output, nil
}

const helpIndexTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Daz Help</title>
  <style>
    :root {
      --bg: #101314;
      --panel: #171c1f;
      --text: #f1f2f2;
      --muted: #b6bdc2;
      --accent: #f6c453;
      --accent-2: #45a3f6;
      --chip: #1f262b;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "SF Pro Text", "Segoe UI", sans-serif;
      background: radial-gradient(circle at top, #1c2226 0%, #0d0f10 55%);
      color: var(--text);
    }
    header {
      padding: 48px 24px 24px;
      max-width: 1200px;
      margin: 0 auto;
    }
    header h1 {
      margin: 0 0 8px;
      font-size: 38px;
      letter-spacing: 0.6px;
    }
    header p {
      margin: 0;
      color: var(--muted);
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      gap: 18px;
      padding: 24px;
      max-width: 1200px;
      margin: 0 auto 48px;
    }
    .card {
      background: var(--panel);
      border-radius: 16px;
      padding: 18px;
      box-shadow: 0 10px 30px rgba(0,0,0,0.25);
      border: 1px solid rgba(255,255,255,0.04);
    }
    .card h2 {
      margin: 0 0 8px;
      font-size: 20px;
      color: var(--accent);
    }
    .card p {
      margin: 0 0 12px;
      color: var(--muted);
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      font-size: 12px;
      color: var(--muted);
    }
    .chip {
      background: var(--chip);
      padding: 4px 8px;
      border-radius: 999px;
    }
    footer {
      text-align: center;
      color: var(--muted);
      font-size: 12px;
      padding-bottom: 24px;
    }
    a.anchor {
      color: inherit;
      text-decoration: none;
    }
  </style>
</head>
<body>
  <header>
    <h1>Daz Command Help</h1>
    <p>Last updated: {{.GeneratedAt}}</p>
    {{if .IncludeRestricted}}<p style="color: var(--muted);">Some commands require admin or higher rank.</p>{{end}}
  </header>
  <section class="grid">
    {{range .Entries}}
    <article class="card" id="{{.Primary}}">
      <h2><a class="anchor" href="#{{.Primary}}">!{{.Primary}}</a></h2>
      <p>{{.Description}}</p>
      <div class="meta">
        {{if .Aliases}}<span class="chip">Aliases: {{.Aliases}}</span>{{end}}
        {{if .AdminOnly}}<span class="chip" style="color: var(--accent-2);">Admin only</span>{{end}}
        {{if gt .MinRank 0}}<span class="chip">Min rank: {{.MinRank}}</span>{{end}}
      </div>
    </article>
    {{end}}
  </section>
  <footer>Generated by Daz Help</footer>
</body>
</html>`
