package gallery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/logger"
)

// HealthChecker performs health checks on gallery images
type HealthChecker struct {
	store      *Store
	config     *Config
	httpClient *http.Client
	mu         sync.Mutex
	checking   bool
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(store *Store, config *Config) *HealthChecker {
	return &HealthChecker{
		store:  store,
		config: config,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 3 redirects
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// CheckPendingImages checks images that are due for health checking
func (h *HealthChecker) CheckPendingImages() error {
	h.mu.Lock()
	if h.checking {
		h.mu.Unlock()
		logger.Debug("gallery", "Health check already in progress, skipping")
		return nil
	}
	h.checking = true
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.checking = false
		h.mu.Unlock()
	}()

	// Get active images that need checking
	images, err := h.store.GetImagesForHealthCheck(50)
	if err != nil {
		return fmt.Errorf("failed to get images for health check: %w", err)
	}

	// Also check some dead images for recovery (with exponential backoff)
	deadImages, err := h.store.GetDeadImagesForRecovery(10)
	if err != nil {
		logger.Error("gallery", "Failed to get dead images for recovery: %v", err)
		// Continue with regular health checks even if recovery check fails
	} else if len(deadImages) > 0 {
		logger.Debug("gallery", "Checking %d dead images for recovery", len(deadImages))
		images = append(images, deadImages...)
	}

	if len(images) == 0 {
		logger.Debug("gallery", "No images need health checking")
		return nil
	}

	logger.Info("gallery", "Starting health check for %d images", len(images))

	// Use a worker pool to check images concurrently
	const numWorkers = 5
	imageChan := make(chan *GalleryImage, len(images))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for img := range imageChan {
				h.checkImage(img)
			}
		}(i)
	}

	// Queue all images
	for _, img := range images {
		imageChan <- img
	}
	close(imageChan)

	// Wait for all checks to complete
	wg.Wait()

	logger.Info("gallery", "Health check completed for %d images", len(images))
	return nil
}

// CheckAllImages forces a health check on all active images
func (h *HealthChecker) CheckAllImages() error {
	// This is called for manual health checks
	// We'll just trigger a regular check with a higher limit
	images, err := h.store.GetImagesForHealthCheck(500)
	if err != nil {
		return fmt.Errorf("failed to get images for health check: %w", err)
	}

	logger.Info("gallery", "Manual health check started for %d images", len(images))

	for _, img := range images {
		h.checkImage(img)
	}

	logger.Info("gallery", "Manual health check completed")
	return nil
}

func isSafeImageURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" || hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" ||
		strings.HasSuffix(hostname, ".local") {
		return false
	}

	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return false
		}
	}

	if resolvesToPrivateIP(hostname) {
		return false
	}

	return true
}

// checkImage performs a health check on a single image
func (h *HealthChecker) checkImage(img *GalleryImage) {
	logger.Debug("gallery", "Checking health of image %d: %s", img.ID, img.URL)

	if !isSafeImageURL(img.URL) {
		logger.Warn("gallery", "Skipping unsafe image URL for %d: %s", img.ID, img.URL)
		h.markImageFailed(img.ID, "Unsafe URL")
		return
	}

	// Create a HEAD request to check if the image is accessible
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", img.URL, nil)
	if err != nil {
		logger.Error("gallery", "Failed to create request for %s: %v", img.URL, err)
		h.markImageFailed(img.ID, "Invalid URL")
		return
	}

	// Set user agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DazBot/1.0; +https://github.com/hildolfr/daz)")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		logger.Debug("gallery", "Failed to fetch %s: %v", img.URL, err)
		h.markImageFailed(img.ID, fmt.Sprintf("Network error: %v", err))
		return
	}

	if resp.StatusCode == http.StatusMethodNotAllowed {
		_ = resp.Body.Close()
		getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, img.URL, nil)
		if err != nil {
			logger.Error("gallery", "Failed to create GET request for %s: %v", img.URL, err)
			h.markImageFailed(img.ID, "Invalid URL")
			return
		}
		getReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DazBot/1.0; +https://github.com/hildolfr/daz)")
		getReq.Header.Set("Range", "bytes=0-0")
		resp, err = h.httpClient.Do(getReq)
		if err != nil {
			logger.Debug("gallery", "Failed to fetch %s with GET: %v", img.URL, err)
			h.markImageFailed(img.ID, fmt.Sprintf("Network error: %v", err))
			return
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Check the status code
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		// Image is healthy
		h.markImageHealthy(img.ID)
		logger.Debug("gallery", "Image %d is healthy (status %d)", img.ID, resp.StatusCode)

	case http.StatusNotFound, http.StatusGone:
		// Image is definitely dead
		h.markImageFailed(img.ID, fmt.Sprintf("HTTP %d", resp.StatusCode))
		logger.Debug("gallery", "Image %d is dead (status %d)", img.ID, resp.StatusCode)

	case http.StatusForbidden, http.StatusUnauthorized:
		// Access denied, but image might exist
		h.markImageFailed(img.ID, fmt.Sprintf("Access denied (HTTP %d)", resp.StatusCode))
		logger.Debug("gallery", "Image %d access denied (status %d)", img.ID, resp.StatusCode)

	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther:
		// Redirect - consider as healthy since the resource exists
		h.markImageHealthy(img.ID)
		logger.Debug("gallery", "Image %d redirected but considered healthy (status %d)", img.ID, resp.StatusCode)

	default:
		// Other status codes - might be temporary
		if resp.StatusCode >= 500 {
			// Server error - might be temporary
			logger.Debug("gallery", "Image %d server error (status %d), will retry later", img.ID, resp.StatusCode)
			// Don't mark as failed yet, will retry
		} else {
			// Client error or unknown
			h.markImageFailed(img.ID, fmt.Sprintf("HTTP %d", resp.StatusCode))
			logger.Debug("gallery", "Image %d failed (status %d)", img.ID, resp.StatusCode)
		}
	}
}

// markImageHealthy marks an image as healthy
func (h *HealthChecker) markImageHealthy(imageID int64) {
	// First check if this is a dead image that can be recovered
	if recovered, err := h.store.RecoverDeadImage(imageID); err != nil {
		logger.Error("gallery", "Failed to check recovery for image %d: %v", imageID, err)
	} else if recovered {
		logger.Info("gallery", "Successfully recovered dead image %d", imageID)
		return
	}

	// Otherwise just mark as healthy (reset failure count)
	if err := h.store.MarkImageHealthCheck(imageID, false, ""); err != nil {
		logger.Error("gallery", "Failed to mark image %d as healthy: %v", imageID, err)
	}
}

// markImageFailed marks an image as failed
func (h *HealthChecker) markImageFailed(imageID int64, reason string) {
	if err := h.store.MarkImageHealthCheck(imageID, true, reason); err != nil {
		logger.Error("gallery", "Failed to mark image %d as failed: %v", imageID, err)
	}
}
