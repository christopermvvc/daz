package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hildolfr/daz/internal/logger"
)

// Config represents the complete application configuration
type Config struct {
	Core     CoreConfig              `json:"core"`
	EventBus EventBusConfig          `json:"event_bus"`
	Plugins  map[string]PluginConfig `json:"plugins"`
}

// CoreConfig contains configuration for the core plugin
type CoreConfig struct {
	Rooms    []RoomConfig   `json:"rooms"`
	Database DatabaseConfig `json:"database"`
}

// RoomConfig contains configuration for a single room/channel connection
type RoomConfig struct {
	ID                string `json:"id"`
	Channel           string `json:"channel"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password,omitempty"`
	Enabled           bool   `json:"enabled"`
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

// DefaultConfig returns a Config with sensible defaults (no credentials)
func DefaultConfig() *Config {
	return &Config{
		Core: CoreConfig{
			Rooms: []RoomConfig{},
			Database: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "",
				User:     "",
				Password: "",
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
	config := DefaultConfig()

	// Load from file if it exists
	if path != "" {
		file, err := os.Open(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to open config file: %w", err)
		}
		if err == nil {
			defer func() {
				if err := file.Close(); err != nil {
					logger.Warn("Config", "Failed to close config file: %v", err)
				}
			}()

			decoder := json.NewDecoder(file)
			if err := decoder.Decode(config); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %w", err)
			}
		}
	}

	// Apply environment variable overrides
	config.LoadFromEnv()

	return config, nil
}

// LoadFromEnv loads configuration from environment variables
func (c *Config) LoadFromEnv() {
	// For backward compatibility, create a single room from env vars if no rooms configured
	if len(c.Core.Rooms) == 0 {
		if channel := os.Getenv("DAZ_CYTUBE_CHANNEL"); channel != "" {
			room := RoomConfig{
				ID:                "default",
				Channel:           channel,
				Username:          os.Getenv("DAZ_CYTUBE_USERNAME"),
				Password:          os.Getenv("DAZ_CYTUBE_PASSWORD"),
				Enabled:           true,
				ReconnectAttempts: 10,
				CooldownMinutes:   30,
			}
			c.Core.Rooms = []RoomConfig{room}
		}
	}
	if v := os.Getenv("DAZ_DB_USER"); v != "" {
		c.Core.Database.User = v
	}
	if v := os.Getenv("DAZ_DB_PASSWORD"); v != "" {
		c.Core.Database.Password = v
	}
	if v := os.Getenv("DAZ_DB_NAME"); v != "" {
		c.Core.Database.Database = v
	}
	if v := os.Getenv("DAZ_DB_HOST"); v != "" {
		c.Core.Database.Host = v
	}
	if v := os.Getenv("DAZ_DB_PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil && port > 0 {
			c.Core.Database.Port = port
		}
	}
}

// MergeWithFlags merges command-line flag values into the configuration
// Command-line flags take precedence over config file values and environment variables
func (c *Config) MergeWithFlags(channel, username, password, dbHost string, dbPort int, dbName, dbUser, dbPass string) {
	// For backward compatibility, update the first room or create one
	if channel != "" || username != "" || password != "" {
		if len(c.Core.Rooms) == 0 {
			c.Core.Rooms = []RoomConfig{{
				ID:                "default",
				Enabled:           true,
				ReconnectAttempts: 10,
				CooldownMinutes:   30,
			}}
		}
		if channel != "" {
			c.Core.Rooms[0].Channel = channel
		}
		if username != "" {
			c.Core.Rooms[0].Username = username
		}
		if password != "" {
			c.Core.Rooms[0].Password = password
		}
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
	if len(c.Core.Rooms) == 0 {
		return fmt.Errorf("at least one room must be configured")
	}

	enabledRooms := 0
	for i, room := range c.Core.Rooms {
		if !room.Enabled {
			continue
		}
		enabledRooms++

		if room.Channel == "" {
			return fmt.Errorf("room[%d]: channel is required", i)
		}

		// Use channel as identifier if ID is not set
		identifier := room.ID
		if identifier == "" {
			identifier = room.Channel
		}

		if room.Username == "" && room.Password == "" {
			continue
		}
		if room.Username == "" {
			return fmt.Errorf("room[%d] '%s': username is required when password is set", i, identifier)
		}
		if room.Password == "" {
			return fmt.Errorf("room[%d] '%s': password is required when username is set", i, identifier)
		}
	}

	if enabledRooms == 0 {
		return fmt.Errorf("at least one room must be enabled")
	}
	if c.Core.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if c.Core.Database.Port <= 0 {
		return fmt.Errorf("database port must be positive")
	}
	if c.Core.Database.Database == "" {
		return fmt.Errorf("database name is required (set DAZ_DB_NAME environment variable)")
	}
	if c.Core.Database.User == "" {
		return fmt.Errorf("database user is required (set DAZ_DB_USER environment variable)")
	}
	if c.Core.Database.Password == "" {
		return fmt.Errorf("database password is required (set DAZ_DB_PASSWORD environment variable)")
	}
	return nil
}
