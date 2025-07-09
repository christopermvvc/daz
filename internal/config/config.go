package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// Config represents the complete application configuration
type Config struct {
	Core     CoreConfig              `json:"core"`
	EventBus EventBusConfig          `json:"event_bus"`
	Plugins  map[string]PluginConfig `json:"plugins"`
}

// CoreConfig contains configuration for the core plugin
type CoreConfig struct {
	Cytube   CytubeConfig   `json:"cytube"`
	Database DatabaseConfig `json:"database"`
}

// CytubeConfig contains Cytube connection settings
type CytubeConfig struct {
	Channel           string `json:"channel"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password,omitempty"`
	ReconnectAttempts int    `json:"reconnect_attempts"`
	CooldownMinutes   int    `json:"cooldown_minutes"`
}

// DatabaseConfig contains PostgreSQL connection settings
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	Password string `json:"password"`
}

// EventBusConfig contains event bus settings
type EventBusConfig struct {
	BufferSizes map[string]int `json:"buffer_sizes"`
}

// PluginConfig represents a plugin's configuration as raw JSON
type PluginConfig json.RawMessage

// UnmarshalJSON implements json.Unmarshaler for PluginConfig
func (p *PluginConfig) UnmarshalJSON(data []byte) error {
	*p = PluginConfig(data)
	return nil
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Core: CoreConfig{
			Cytube: CytubeConfig{
				Channel:           "***REMOVED***",
				Username:          "***REMOVED***",
				Password:          "***REMOVED***",
				ReconnectAttempts: 10,
				CooldownMinutes:   30,
			},
			Database: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "daz",
				User:     "***REMOVED***",
				Password: "***REMOVED***",
			},
		},
		EventBus: EventBusConfig{
			BufferSizes: map[string]int{
				"cytube.event":   1000,
				"sql.request":    100,
				"plugin.request": 50,
			},
		},
		Plugins: make(map[string]PluginConfig),
	}
}

// LoadFromFile loads configuration from a JSON file
func LoadFromFile(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close config file: %v", err)
		}
	}()

	config := DefaultConfig()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// MergeWithFlags merges command-line flag values into the configuration
// Command-line flags take precedence over config file values
func (c *Config) MergeWithFlags(channel, username, password, dbHost string, dbPort int, dbName, dbUser, dbPass string) {
	if channel != "" {
		c.Core.Cytube.Channel = channel
	}
	if username != "" {
		c.Core.Cytube.Username = username
	}
	if password != "" {
		c.Core.Cytube.Password = password
	}
	if dbHost != "" {
		c.Core.Database.Host = dbHost
	}
	if dbPort > 0 {
		c.Core.Database.Port = dbPort
	}
	if dbName != "" {
		c.Core.Database.Database = dbName
	}
	if dbUser != "" {
		c.Core.Database.User = dbUser
	}
	if dbPass != "" {
		c.Core.Database.Password = dbPass
	}
}

// GetPluginConfig returns the configuration for a specific plugin
func (c *Config) GetPluginConfig(name string) json.RawMessage {
	if config, exists := c.Plugins[name]; exists {
		return json.RawMessage(config)
	}
	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Core.Cytube.Channel == "" {
		return fmt.Errorf("cytube channel is required")
	}
	if c.Core.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Core.Database.Port <= 0 {
		return fmt.Errorf("database port must be positive")
	}
	if c.Core.Database.Database == "" {
		return fmt.Errorf("database name is required")
	}
	if c.Core.Database.User == "" {
		return fmt.Errorf("database user is required")
	}
	return nil
}
