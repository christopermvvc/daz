package core

// Config holds configuration for the core plugin
type Config struct {
	Cytube   CytubeConfig   `json:"cytube"`
	Database DatabaseConfig `json:"database"`
}

// CytubeConfig holds Cytube connection settings
type CytubeConfig struct {
	ServerURL string `json:"server_url"`
	Channel   string `json:"channel"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	User            string `json:"user"`
	Password        string `json:"password"`
	MaxConnections  int    `json:"max_connections"`
	ConnectTimeout  int    `json:"connect_timeout"`
	MaxConnLifetime int    `json:"max_conn_lifetime"`
}

// SetDefaults applies default values to config
func (c *Config) SetDefaults() {
	if c.Database.Host == "" {
		c.Database.Host = "localhost"
	}
	if c.Database.Port == 0 {
		c.Database.Port = 5432
	}
	if c.Database.MaxConnections == 0 {
		c.Database.MaxConnections = 10
	}
	if c.Database.ConnectTimeout == 0 {
		c.Database.ConnectTimeout = 10
	}
	if c.Database.MaxConnLifetime == 0 {
		c.Database.MaxConnLifetime = 3600 // 1 hour
	}
}
