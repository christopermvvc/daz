package core

// Config holds configuration for the core plugin
type Config struct {
	Cytube CytubeConfig `json:"cytube"`
}

// CytubeConfig holds Cytube connection settings
type CytubeConfig struct {
	ServerURL string `json:"server_url"`
	Channel   string `json:"channel"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// SetDefaults applies default values to config
func (c *Config) SetDefaults() {
	// Currently no defaults needed for Cytube config
}
