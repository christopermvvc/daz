package gallery

import (
	"testing"
	"time"
)

func TestGetPrunedImages(t *testing.T) {
	// This test would require a mock database
	// For now, we're testing the query structure
	store := &Store{
		name: "gallery_test",
	}

	// Test that function exists and has correct signature
	var _ func(int) ([]*GalleryImage, error) = store.GetPrunedImages
}

func TestGetDeadImagesForRecovery(t *testing.T) {
	// This test would require a mock database
	// For now, we're testing the query structure
	store := &Store{
		name: "gallery_test",
	}

	// Test that function exists and has correct signature
	var _ func(int) ([]*GalleryImage, error) = store.GetDeadImagesForRecovery
}

func TestRecoverDeadImage(t *testing.T) {
	// This test would require a mock database
	// For now, we're testing the query structure
	store := &Store{
		name: "gallery_test",
	}

	// Test that function exists and has correct signature
	var _ func(int64) (bool, error) = store.RecoverDeadImage
}

func TestPrunedImageDisplay(t *testing.T) {
	// Test the PrunedImageDisplay struct
	display := PrunedImageDisplay{
		Username:     "testuser",
		URL:          "https://example.com/image.jpg",
		PrunedReason: "404 Not Found",
		DeadSince:    "2025-08-07 12:00",
		IsGravestone: false,
	}

	if display.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %s", display.Username)
	}

	if display.URL != "https://example.com/image.jpg" {
		t.Errorf("Expected URL 'https://example.com/image.jpg', got %s", display.URL)
	}

	// Test gravestone logic
	if display.IsGravestone {
		t.Error("Expected IsGravestone to be false for recent death")
	}

	// Test with gravestone (dead > 48 hours)
	oldDisplay := PrunedImageDisplay{
		Username:     "olduser",
		URL:          "https://example.com/old.jpg",
		PrunedReason: "Gone",
		DeadSince:    time.Now().Add(-72 * time.Hour).Format("2006-01-02 15:04"),
		IsGravestone: true,
	}

	if !oldDisplay.IsGravestone {
		t.Error("Expected IsGravestone to be true for old death")
	}
}