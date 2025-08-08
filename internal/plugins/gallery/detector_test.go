package gallery

import (
	"testing"
)

func TestImageDetector_SecurityValidation(t *testing.T) {
	detector := NewImageDetector()

	tests := []struct {
		name     string
		message  string
		expected int
		desc     string
	}{
		{
			name:     "RejectLocalhost",
			message:  "Check this image: http://localhost/evil.png",
			expected: 0,
			desc:     "Should reject localhost URLs",
		},
		{
			name:     "RejectPrivateIP",
			message:  "Image at http://192.168.1.1/private.jpg",
			expected: 0,
			desc:     "Should reject private IP addresses",
		},
		{
			name:     "RejectTooLongURL",
			message:  "http://example.com/" + string(make([]byte, 3000)) + ".jpg",
			expected: 0,
			desc:     "Should reject URLs over 2048 chars",
		},
		{
			name:     "AcceptValidImage",
			message:  "Check out https://i.imgur.com/abc123.png",
			expected: 1,
			desc:     "Should accept valid image URLs",
		},
		{
			name:     "AcceptPinimg",
			message:  "https://i.pinimg.com/originals/02/d7/32/02d732ab7eccf66da7b26a8cd055b62e.jpg",
			expected: 1,
			desc:     "Should accept pinimg URLs",
		},
		{
			name:     "RejectNonHTTP",
			message:  "file:///etc/passwd.jpg",
			expected: 0,
			desc:     "Should reject non-HTTP schemes",
		},
		{
			name:     "RejectJavaScript",
			message:  "javascript:alert('xss').jpg",
			expected: 0,
			desc:     "Should reject javascript: URLs",
		},
		{
			name:     "Reject172Network",
			message:  "http://172.16.0.1/internal.png",
			expected: 0,
			desc:     "Should reject 172.16.x.x addresses",
		},
		{
			name:     "AcceptIBB",
			message:  "https://i.ibb.co/test.gif",
			expected: 1,
			desc:     "Should accept ibb.co image host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images := detector.DetectImages(tt.message)
			if len(images) != tt.expected {
				t.Errorf("%s: expected %d images, got %d - %s", 
					tt.name, tt.expected, len(images), tt.desc)
			}
		})
	}
}

func TestImageDetector_CleanURL(t *testing.T) {
	detector := NewImageDetector()

	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/image.jpg.", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg,", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg!", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg?", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg)", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg]", "http://example.com/image.jpg"},
		{"http://example.com/image.jpg...", "http://example.com/image.jpg"},
	}

	for _, tt := range tests {
		result := detector.cleanURL(tt.input)
		if result != tt.expected {
			t.Errorf("cleanURL(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestImageDetector_IPv6Rejection(t *testing.T) {
	detector := NewImageDetector()

	ipv6Tests := []string{
		"http://[::1]/image.png",
		"http://[fe80::1]/image.jpg",
		"http://[fc00::1]/test.gif",
		"http://[fd00::1]/photo.webp",
	}

	for _, url := range ipv6Tests {
		images := detector.DetectImages(url)
		if len(images) > 0 {
			t.Errorf("Should reject IPv6 local address: %s", url)
		}
	}
}