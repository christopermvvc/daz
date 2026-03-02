package help

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	outputFile := filepath.Join(outputDir, "index.html")
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
		return "", fmt.Errorf("missing deploy key")
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
