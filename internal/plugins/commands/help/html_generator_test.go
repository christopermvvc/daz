package help

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelpGenerateAllMissingAuth(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	entries := []*commandEntry{
		{Primary: "ping", Description: "ping the bot"},
		{Primary: "modcmd", Description: "mod only", MinRank: 2},
		{Primary: "admincmd", Description: "admin only", AdminOnly: true},
	}

	generator := NewHTMLGenerator(config, func() []*commandEntry { return entries }, true, true)
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		return "", nil
	}
	generator.deployKeyResolver = func() (string, error) {
		return "", fmt.Errorf("missing deploy key")
	}
	_ = os.Unsetenv("GITHUB_TOKEN")

	if err := generator.GenerateAll(context.Background()); err != nil {
		t.Fatalf("GenerateAll() error = %v", err)
	}

	outputFile := filepath.Join(outputDir, "help", "index.html")
	contents, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("expected index.html to be written: %v", err)
	}
	page := string(contents)
	if !strings.Contains(page, "Some commands require admin") {
		t.Fatalf("expected restricted notice in HTML")
	}
	if !strings.Contains(page, "(admin only)") {
		t.Fatalf("expected admin only label in HTML")
	}
	if !strings.Contains(page, "min rank 2") {
		t.Fatalf("expected min rank label in HTML")
	}
}

func TestHelpGenerateAllSkipsInitWhenGitExists(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create git dir: %v", err)
	}
	markerPath := filepath.Join(outputDir, helpMarkerFile)
	if err := os.WriteFile(markerPath, []byte("daz help output"), 0644); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry { return []*commandEntry{{Primary: "ping", Description: "ping"}} }, true, true)
	var calls []string
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	generator.deployKeyResolver = func() (string, error) {
		return "/tmp/deploy_key", nil
	}
	_ = os.Unsetenv("GITHUB_TOKEN")

	if err := generator.GenerateAll(context.Background()); err != nil {
		t.Fatalf("GenerateAll() error = %v", err)
	}

	for _, call := range calls {
		if strings.HasPrefix(call, "init") {
			t.Fatalf("did not expect init when .git exists")
		}
	}
}

func TestHelpResetGitStateSkipsWithoutMarker(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry { return nil }, true, true)
	var calls []string
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}

	generator.resetGitState()
	if len(calls) != 0 {
		t.Fatalf("expected no git calls without marker, got %v", calls)
	}
}

func TestHelpGenerateAllUsesConfiguredOutputFiles(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	entries := []*commandEntry{
		{Primary: "ping", Description: "ping the bot"},
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry { return entries }, true, true)
	generator.rootOutputFile = "public-index.html"
	generator.helpOutputFile = filepath.Join("custom", "index.html")
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		return "", nil
	}
	generator.deployKeyResolver = func() (string, error) {
		return "", fmt.Errorf("missing deploy key")
	}

	if err := generator.GenerateAll(context.Background()); err != nil {
		t.Fatalf("GenerateAll() error = %v", err)
	}

	rootOutput := filepath.Join(outputDir, "public-index.html")
	helperOutput := filepath.Join(outputDir, "custom", "index.html")
	for _, output := range []string{rootOutput, helperOutput} {
		contents, err := os.ReadFile(output)
		if err != nil {
			t.Fatalf("expected generated output file %s: %v", output, err)
		}
		if !strings.Contains(string(contents), "ping the bot") {
			t.Fatalf("expected generated output in %s to include command", output)
		}
	}
}

func TestHelpGenerateAllWithoutPublishSkipsGitPush(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry {
		return []*commandEntry{{Primary: "ping", Description: "ping the bot"}}
	}, true, true)

	called := false
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		called = true
		return "", nil
	}

	if err := generator.GenerateAllWithoutPublish(context.Background()); err != nil {
		t.Fatalf("GenerateAllWithoutPublish() error = %v", err)
	}

	if called {
		t.Fatalf("expected GenerateAllWithoutPublish to avoid git calls")
	}
}

func TestHelpGenerateAllPublishesToGitHub(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry {
		return []*commandEntry{{Primary: "ping", Description: "ping the bot"}}
	}, true, true)

	var calls []string
	generator.gitRunner = func(ctx context.Context, dir string, env []string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	generator.deployKeyResolver = func() (string, error) {
		return "/tmp/deploy_key", nil
	}
	_ = os.Unsetenv("GITHUB_TOKEN")

	if err := generator.GenerateAll(context.Background()); err != nil {
		t.Fatalf("GenerateAll() error = %v", err)
	}

	found := false
	for _, call := range calls {
		if strings.HasPrefix(call, "push ") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected GenerateAll to invoke git push, got calls: %v", calls)
	}
}

func TestHelpGenerateAllRejectsProjectRootLikePath(t *testing.T) {
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outputDir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "internal"), 0755); err != nil {
		t.Fatalf("failed to create internal dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "cmd"), 0755); err != nil {
		t.Fatalf("failed to create cmd dir: %v", err)
	}

	config := &Config{
		ShowAliases:       true,
		GenerateHTML:      true,
		HTMLOutputPath:    outputDir,
		HelpBaseURL:       defaultHelpBaseURL,
		IncludeRestricted: true,
	}
	generator := NewHTMLGenerator(config, func() []*commandEntry {
		return []*commandEntry{{Primary: "ping", Description: "ping the bot"}}
	}, true, true)

	if err := generator.GenerateAllWithoutPublish(context.Background()); err == nil {
		t.Fatalf("expected unsafe output path error for project-like root path")
	}
}

func TestHelpShouldPublishStateGating(t *testing.T) {
	outputDir := t.TempDir()
	config := &Config{
		HTMLOutputPath:            outputDir,
		PublishMinIntervalSeconds: 60,
		ShowAliases:               true,
		GenerateHTML:              true,
		HelpBaseURL:               defaultHelpBaseURL,
		IncludeRestricted:         true,
	}

	generator := NewHTMLGenerator(config, func() []*commandEntry {
		return []*commandEntry{{Primary: "ping", Description: "ping"}}
	}, true, true)

	rootOutput := filepath.Join(outputDir, generator.rootOutputFile)
	if err := os.WriteFile(rootOutput, []byte("content-v1"), 0644); err != nil {
		t.Fatalf("failed to write root output: %v", err)
	}

	hashV1, err := generator.currentPublishHash()
	if err != nil {
		t.Fatalf("currentPublishHash failed: %v", err)
	}

	now := time.Now().UTC()
	should, _, _, err := generator.shouldPublish(hashV1, now)
	if err != nil {
		t.Fatalf("shouldPublish failed: %v", err)
	}
	if !should {
		t.Fatal("expected initial publish to be allowed")
	}

	if err := generator.savePublishState(publishState{
		ContentHash:   hashV1,
		LastPublished: now,
	}); err != nil {
		t.Fatalf("savePublishState failed: %v", err)
	}

	should, reason, _, err := generator.shouldPublish(hashV1, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("shouldPublish failed: %v", err)
	}
	if should {
		t.Fatal("expected unchanged hash to skip publish")
	}
	if !strings.Contains(reason, "unchanged") {
		t.Fatalf("expected unchanged reason, got %q", reason)
	}

	if err := os.WriteFile(rootOutput, []byte("content-v2"), 0644); err != nil {
		t.Fatalf("failed to write root output v2: %v", err)
	}
	hashV2, err := generator.currentPublishHash()
	if err != nil {
		t.Fatalf("currentPublishHash failed for v2: %v", err)
	}

	should, reason, _, err = generator.shouldPublish(hashV2, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("shouldPublish failed for v2: %v", err)
	}
	if should {
		t.Fatal("expected min-interval gating for changed content")
	}
	if !strings.Contains(reason, "minimum publish interval") {
		t.Fatalf("expected min-interval reason, got %q", reason)
	}

	should, _, _, err = generator.shouldPublish(hashV2, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("shouldPublish failed for delayed publish: %v", err)
	}
	if !should {
		t.Fatal("expected changed content to publish after interval")
	}
}
