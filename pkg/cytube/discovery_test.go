package cytube

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverServer(t *testing.T) {
	tests := []struct {
		name          string
		channel       string
		response      interface{}
		statusCode    int
		wantURL       string
		wantErr       bool
		errorContains string
	}{
		{
			name:    "successful discovery with secure server",
			channel: "test-channel",
			response: ServerConfig{
				Servers: []struct {
					URL    string `json:"url"`
					Secure bool   `json:"secure"`
				}{
					{URL: "http://insecure.example.com", Secure: false},
					{URL: "https://secure.example.com", Secure: true},
				},
			},
			statusCode: http.StatusOK,
			wantURL:    "https://secure.example.com",
			wantErr:    false,
		},
		{
			name:    "successful discovery with only insecure servers",
			channel: "test-channel",
			response: ServerConfig{
				Servers: []struct {
					URL    string `json:"url"`
					Secure bool   `json:"secure"`
				}{
					{URL: "http://server1.example.com", Secure: false},
					{URL: "http://server2.example.com", Secure: false},
				},
			},
			statusCode: http.StatusOK,
			wantURL:    "http://server1.example.com",
			wantErr:    false,
		},
		{
			name:          "server returns 404",
			channel:       "nonexistent",
			response:      nil,
			statusCode:    http.StatusNotFound,
			wantErr:       true,
			errorContains: "status 404",
		},
		{
			name:    "empty server list",
			channel: "empty-channel",
			response: ServerConfig{Servers: []struct {
				URL    string `json:"url"`
				Secure bool   `json:"secure"`
			}{}},
			statusCode:    http.StatusOK,
			wantErr:       true,
			errorContains: "no servers found",
		},
		{
			name:          "invalid JSON response",
			channel:       "bad-json",
			response:      "invalid json",
			statusCode:    http.StatusOK,
			wantErr:       true,
			errorContains: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/socketconfig/" + tt.channel + ".json"
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: got %s, want %s", r.URL.Path, expectedPath)
				}

				w.WriteHeader(tt.statusCode)
				if tt.response != nil {
					if err := json.NewEncoder(w).Encode(tt.response); err != nil {
						t.Fatalf("failed to encode response: %v", err)
					}
				}
			}))
			defer ts.Close()

			// Test with our testable function
			gotURL, err := DiscoverServerWithBaseURL(tt.channel, ts.URL)

			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverServerWithBaseURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errorContains != "" {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if gotURL != tt.wantURL {
				t.Errorf("DiscoverServerWithBaseURL() = %v, want %v", gotURL, tt.wantURL)
			}
		})
	}
}

func TestDiscoverServer_Integration(t *testing.T) {
	// This is an integration test that hits the real server
	// Should be skipped in CI/CD environments
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Test with a known channel
	url, err := DiscoverServer("***REMOVED***")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if url == "" {
		t.Error("expected non-empty URL")
	}

	t.Logf("Discovered server URL: %s", url)
}
