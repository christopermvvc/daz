package core

// Config holds configuration for the core plugin
type Config struct {
	Rooms []RoomConfig `json:"rooms"`
}

// RoomConfig holds configuration for a single room connection
type RoomConfig struct {
	ID                string `json:"id"`
	Channel           string `json:"channel"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	Enabled           bool   `json:"enabled"`
	ReconnectAttempts int    `json:"reconnect_attempts"`
	CooldownMinutes   int    `json:"cooldown_minutes"`
}

// SetDefaults applies default values to config
func (c *Config) SetDefaults() {
	for i := range c.Rooms {
		if c.Rooms[i].ReconnectAttempts == 0 {
			c.Rooms[i].ReconnectAttempts = 10
		}
		if c.Rooms[i].CooldownMinutes == 0 {
			c.Rooms[i].CooldownMinutes = 30
		}
	}
}
