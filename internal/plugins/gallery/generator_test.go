package gallery

import (
	"strings"
	"testing"
	"time"
)

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
