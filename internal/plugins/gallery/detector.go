package gallery

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// ImageDetector detects image URLs in chat messages
type ImageDetector struct {
	// URL regex pattern
	urlRegex *regexp.Regexp

	// Supported image extensions
	imageExtensions map[string]bool
}

// NewImageDetector creates a new image detector
func NewImageDetector() *ImageDetector {
	return &ImageDetector{
		// Match common URL patterns
		urlRegex: regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`),
		imageExtensions: map[string]bool{
			".jpg":  true,
			".jpeg": true,
			".png":  true,
			".gif":  true,
			".webp": true,
			".bmp":  true,
			".svg":  true,
			".webm": true,
		},
	}
}

// DetectImages finds all image URLs in a message
func (d *ImageDetector) DetectImages(message string) []string {
	var images []string

	// Find all URLs in the message
	matches := d.urlRegex.FindAllString(message, -1)

	for _, match := range matches {
		// Clean up the URL (remove trailing punctuation)
		cleanURL := d.cleanURL(match)

		// Check if it's an image URL
		if d.isImageURL(cleanURL) {
			images = append(images, cleanURL)
		}
	}

	return images
}

// cleanURL removes trailing punctuation from URLs
func (d *ImageDetector) cleanURL(rawURL string) string {
	// Remove common trailing punctuation
	for strings.HasSuffix(rawURL, ".") ||
		strings.HasSuffix(rawURL, ",") ||
		strings.HasSuffix(rawURL, "!") ||
		strings.HasSuffix(rawURL, "?") ||
		strings.HasSuffix(rawURL, ";") ||
		strings.HasSuffix(rawURL, ":") ||
		strings.HasSuffix(rawURL, ")") ||
		strings.HasSuffix(rawURL, "]") ||
		strings.HasSuffix(rawURL, "}") {
		rawURL = rawURL[:len(rawURL)-1]
	}

	return rawURL
}

// isImageURL checks if a URL points to an image
func (d *ImageDetector) isImageURL(rawURL string) bool {
	// URL length limit (prevent DoS)
	if len(rawURL) > 2048 {
		return false
	}

	// Parse the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Security: Only allow HTTP and HTTPS schemes
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		// Block dangerous schemes like file://, javascript://, data://, etc.
		return false
	}

	// Security: Prevent localhost and private network access
	hostname := strings.ToLower(u.Hostname())
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" ||
		strings.HasPrefix(hostname, "192.168.") ||
		strings.HasPrefix(hostname, "10.") ||
		strings.HasPrefix(hostname, "172.16.") ||
		strings.HasPrefix(hostname, "172.17.") ||
		strings.HasPrefix(hostname, "172.18.") ||
		strings.HasPrefix(hostname, "172.19.") ||
		strings.HasPrefix(hostname, "172.20.") ||
		strings.HasPrefix(hostname, "172.21.") ||
		strings.HasPrefix(hostname, "172.22.") ||
		strings.HasPrefix(hostname, "172.23.") ||
		strings.HasPrefix(hostname, "172.24.") ||
		strings.HasPrefix(hostname, "172.25.") ||
		strings.HasPrefix(hostname, "172.26.") ||
		strings.HasPrefix(hostname, "172.27.") ||
		strings.HasPrefix(hostname, "172.28.") ||
		strings.HasPrefix(hostname, "172.29.") ||
		strings.HasPrefix(hostname, "172.30.") ||
		strings.HasPrefix(hostname, "172.31.") ||
		strings.HasPrefix(hostname, "169.254.") ||
		strings.HasPrefix(hostname, "fe80:") ||
		strings.HasPrefix(hostname, "fc00:") ||
		strings.HasPrefix(hostname, "fd00:") {
		return false
	}

	// Get the file extension from the path
	ext := strings.ToLower(path.Ext(u.Path))

	// Check if it's a known image extension
	if d.imageExtensions[ext] {
		return true
	}

	// Check for common image hosting patterns
	host := strings.ToLower(u.Host)

	// Direct image hosts
	if strings.Contains(host, "imgur.com") ||
		strings.Contains(host, "i.imgur.com") ||
		strings.Contains(host, "gyazo.com") ||
		strings.Contains(host, "prnt.sc") ||
		strings.Contains(host, "prntscr.com") ||
		strings.Contains(host, "lightshot.net") ||
		strings.Contains(host, "puu.sh") ||
		strings.Contains(host, "cdn.discordapp.com") ||
		strings.Contains(host, "media.discordapp.net") ||
		strings.Contains(host, "pinimg.com") ||
		strings.Contains(host, "ibb.co") {
		return true
	}

	// Check for image parameters in query string
	query := strings.ToLower(u.RawQuery)
	if strings.Contains(query, "format=jpg") ||
		strings.Contains(query, "format=jpeg") ||
		strings.Contains(query, "format=png") ||
		strings.Contains(query, "format=gif") ||
		strings.Contains(query, "format=webp") {
		return true
	}

	// Check Twitter/X image URLs
	if (strings.Contains(host, "pbs.twimg.com") || strings.Contains(host, "ton.twitter.com")) &&
		strings.Contains(u.Path, "/media/") {
		return true
	}

	return false
}

// IsValidImageURL validates if a URL is a valid image URL
func (d *ImageDetector) IsValidImageURL(rawURL string) bool {
	// Parse the URL
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Must be HTTP or HTTPS
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Must have a host
	if u.Host == "" {
		return false
	}

	// Check if it looks like an image
	return d.isImageURL(rawURL)
}
