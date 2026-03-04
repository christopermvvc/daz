package gallery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewHTMLGenerator_NormalizesOutputPath(t *testing.T) {
	store := &Store{}
	config := &Config{HTMLOutputPath: " ./data/custom-gallery "}

	NewHTMLGenerator(store, config)

	if !filepath.IsAbs(config.HTMLOutputPath) {
		t.Fatalf("expected absolute output path, got %q", config.HTMLOutputPath)
	}
	if strings.Contains(config.HTMLOutputPath, " ") {
		t.Fatalf("expected trimmed output path, got %q", config.HTMLOutputPath)
	}
}

func TestNewHTMLGenerator_FallbackForUnsafePath(t *testing.T) {
	store := &Store{}
	config := &Config{HTMLOutputPath: "/"}

	NewHTMLGenerator(store, config)

	if config.HTMLOutputPath == string(filepath.Separator) {
		t.Fatalf("expected fallback path for root output path")
	}
	if !filepath.IsAbs(config.HTMLOutputPath) {
		t.Fatalf("expected absolute fallback path, got %q", config.HTMLOutputPath)
	}
}

func TestNewHTMLGenerator_ExpandsHomePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	store := &Store{}
	config := &Config{HTMLOutputPath: "~/daz-galleries"}

	NewHTMLGenerator(store, config)

	expected := filepath.Join(homeDir, "daz-galleries")
	if config.HTMLOutputPath != expected {
		t.Fatalf("expected %q, got %q", expected, config.HTMLOutputPath)
	}
}

func TestHTMLGenerator_XSSProtection(t *testing.T) {
	// Create a mock store and generator
	store := &Store{}
	config := &Config{
		HTMLOutputPath: t.TempDir(),
	}
	generator := NewHTMLGenerator(store, config)

	// Test data with safe username but XSS attempt in field
	galleries := []UserGalleryData{
		{
			Username: "testuser<script>",
			Channel:  "test",
			Images: []*GalleryImage{
				{
					URL:      "https://example.com/image.jpg",
					Username: "testuser",
					PostedAt: time.Now(),
				},
			},
			Stats: &GalleryStats{
				ActiveImages: 1,
			},
		},
	}

	// Generate HTML
	html := generator.generateSharedGalleryHTML(galleries, 1, nil)

	// When template fails due to security, it returns error HTML
	if strings.Contains(html, "Error generating gallery") {
		// This is expected - template rejects dangerous input
		return
	}

	// If it doesn't fail, check that script tags are escaped
	if strings.Contains(html, "<script>") && !strings.Contains(html, "&lt;script&gt;") {
		t.Error("HTML should escape or reject script tags")
	}
}

func TestHTMLGenerator_JSEscaping(t *testing.T) {
	// Create a mock store and generator
	store := &Store{}
	config := &Config{
		HTMLOutputPath: t.TempDir(),
	}
	generator := NewHTMLGenerator(store, config)

	// Test data with JS injection attempt
	galleries := []UserGalleryData{
		{
			Username: "normal",
			Channel:  "test",
			Images: []*GalleryImage{
				{
					URL:      "https://example.com/image.jpg');alert('xss",
					Username: "testuser",
					PostedAt: time.Now(),
				},
			},
			Stats: &GalleryStats{
				ActiveImages: 1,
			},
		},
	}

	// Generate HTML
	html := generator.generateSharedGalleryHTML(galleries, 1, nil)

	// Check that quotes are escaped in JavaScript context
	if strings.Contains(html, "');alert('xss") && !strings.Contains(html, "\\x27") {
		t.Error("HTML should escape quotes in JavaScript context")
	}
}

func TestHTMLGenerator_SafeImageTitle(t *testing.T) {
	// Create a mock store and generator
	store := &Store{}
	config := &Config{
		HTMLOutputPath: t.TempDir(),
	}
	generator := NewHTMLGenerator(store, config)

	maliciousTitle := "<img src=x onerror=alert('xss')>"
	galleries := []UserGalleryData{
		{
			Username: "testuser",
			Channel:  "test",
			Images: []*GalleryImage{
				{
					URL:        "https://example.com/image.jpg",
					Username:   "testuser",
					PostedAt:   time.Now(),
					ImageTitle: &maliciousTitle,
				},
			},
			Stats: &GalleryStats{
				ActiveImages: 1,
			},
		},
	}

	// Generate HTML
	html := generator.generateSharedGalleryHTML(galleries, 1, nil)

	// When template fails due to security, it returns error HTML
	if strings.Contains(html, "Error generating gallery") {
		// This is expected - template rejects dangerous input
		return
	}

	// Check that raw img tag is NOT present
	if strings.Contains(html, "<img src=x onerror=alert('xss')>") {
		t.Error("HTML should not contain raw malicious img tags")
	}
}

func TestGenerateSharedGalleryRejectsProjectRootLikePath(t *testing.T) {
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

	store := &Store{}
	config := &Config{
		HTMLOutputPath: outputDir,
	}
	generator := NewHTMLGenerator(store, config)

	if err := generator.GenerateSharedGallery(context.Background(), nil); err == nil {
		t.Fatalf("expected unsafe output path error for project-like root path")
	}
}

func TestGalleryShouldPublishStateGating(t *testing.T) {
	outputDir := t.TempDir()
	galleryDir := filepath.Join(outputDir, "gallery")
	if err := os.MkdirAll(galleryDir, 0755); err != nil {
		t.Fatalf("failed to create gallery dir: %v", err)
	}

	store := &Store{}
	config := &Config{
		HTMLOutputPath:            outputDir,
		PublishMinIntervalSeconds: 60,
	}
	generator := NewHTMLGenerator(store, config)

	indexPath := filepath.Join(galleryDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("gallery-v1"), 0644); err != nil {
		t.Fatalf("failed to write gallery index: %v", err)
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

	if err := os.WriteFile(indexPath, []byte("gallery-v2"), 0644); err != nil {
		t.Fatalf("failed to write gallery index v2: %v", err)
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

func TestIsPushRetryable(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "nil",
			err:       nil,
			retryable: false,
		},
		{
			name:      "missing auth",
			err:       fmt.Errorf("no pages publish token configured and deploy key not found"),
			retryable: false,
		},
		{
			name:      "missing key",
			err:       fmt.Errorf("deploy key not found"),
			retryable: false,
		},
		{
			name:      "transient push failure",
			err:       fmt.Errorf("push failed: EOF"),
			retryable: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPushRetryable(tc.err)
			if got != tc.retryable {
				t.Fatalf("isPushRetryable() = %v, want %v", got, tc.retryable)
			}
		})
	}
}

func TestResolveGalleryPublishToken(t *testing.T) {
	t.Setenv(galleryPublishTokenEnv, "")
	t.Setenv(pagesPublishTokenEnv, "")
	t.Setenv("GITHUB_TOKEN", "issue-only-token")
	if got := resolveGalleryPublishToken(); got != "" {
		t.Fatalf("expected empty token when dedicated env vars are unset, got %q", got)
	}

	t.Setenv(pagesPublishTokenEnv, "shared-pages-token")
	if got := resolveGalleryPublishToken(); got != "shared-pages-token" {
		t.Fatalf("expected shared pages token, got %q", got)
	}

	t.Setenv(galleryPublishTokenEnv, "gallery-pages-token")
	if got := resolveGalleryPublishToken(); got != "gallery-pages-token" {
		t.Fatalf("expected gallery token to take precedence, got %q", got)
	}
}
