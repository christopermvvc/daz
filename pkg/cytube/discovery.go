package cytube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ServerConfig represents the response from /socketconfig/{channel}.json
type ServerConfig struct {
	Servers []struct {
		URL    string `json:"url"`
		Secure bool   `json:"secure"`
	} `json:"servers"`
}

// DefaultBaseURL is the default cytube server URL
const DefaultBaseURL = "https://cytu.be"

// DiscoverServer fetches the appropriate server URL for a given channel
func DiscoverServer(channel string) (string, error) {
	return DiscoverServerWithBaseURL(channel, DefaultBaseURL)
}

// DiscoverServerWithBaseURL fetches the appropriate server URL for a given channel using a custom base URL
func DiscoverServerWithBaseURL(channel, baseURL string) (string, error) {
	url := fmt.Sprintf("%s/socketconfig/%s.json", baseURL, channel)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch server config: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var config ServerConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return "", fmt.Errorf("failed to parse server config: %w", err)
	}

	if len(config.Servers) == 0 {
		return "", fmt.Errorf("no servers found in config")
	}

	// Prefer secure servers
	for _, server := range config.Servers {
		if server.Secure {
			return server.URL, nil
		}
	}

	// Fall back to first server if no secure ones found
	return config.Servers[0].URL, nil
}
