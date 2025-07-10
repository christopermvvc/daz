package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Should have empty rooms by default
	if len(config.Core.Rooms) != 0 {
		t.Errorf("expected empty rooms array, got %d rooms", len(config.Core.Rooms))
	}
	if config.Core.Database.User != "" {
		t.Errorf("expected empty default db user, got %s", config.Core.Database.User)
	}
	if config.Core.Database.Password != "" {
		t.Errorf("expected empty default db password, got %s", config.Core.Database.Password)
	}

	// Should still have non-credential defaults
	if config.Core.Database.Host != "localhost" {
		t.Errorf("expected default host localhost, got %s", config.Core.Database.Host)
	}
	if config.Core.Database.Port != 5432 {
		t.Errorf("expected default port 5432, got %d", config.Core.Database.Port)
	}
	if config.EventBus.BufferSizes["cytube.event"] != 1000 {
		t.Errorf("expected cytube.event buffer size 1000, got %d", config.EventBus.BufferSizes["cytube.event"])
	}
}

func TestLoadFromFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		validate func(*testing.T, *Config)
		wantErr  bool
	}{
		{
			name: "valid config with multiple rooms",
			content: `{
				"core": {
					"rooms": [
						{
							"id": "room1",
							"channel": "test-channel",
							"username": "testuser",
							"password": "testpass",
							"enabled": true,
							"reconnect_attempts": 5,
							"cooldown_minutes": 15
						},
						{
							"id": "room2",
							"channel": "another-channel",
							"username": "testuser2",
							"password": "testpass2",
							"enabled": false,
							"reconnect_attempts": 3,
							"cooldown_minutes": 10
						}
					],
					"database": {
						"host": "db.example.com",
						"port": 5433,
						"database": "testdb",
						"user": "testuser",
						"password": "dbpass"
					}
				},
				"event_bus": {
					"buffer_sizes": {
						"cytube.event": 2000,
						"sql.request": 200
					}
				},
				"plugins": {
					"test_plugin": {
						"enabled": true,
						"setting": "value"
					}
				}
			}`,
			validate: func(t *testing.T, c *Config) {
				if len(c.Core.Rooms) != 2 {
					t.Errorf("expected 2 rooms, got %d", len(c.Core.Rooms))
				}
				if c.Core.Rooms[0].ID != "room1" {
					t.Errorf("expected room ID room1, got %s", c.Core.Rooms[0].ID)
				}
				if c.Core.Rooms[0].Channel != "test-channel" {
					t.Errorf("expected channel test-channel, got %s", c.Core.Rooms[0].Channel)
				}
				if !c.Core.Rooms[0].Enabled {
					t.Error("expected room1 to be enabled")
				}
				if c.Core.Rooms[1].Enabled {
					t.Error("expected room2 to be disabled")
				}
				if c.Core.Database.Port != 5433 {
					t.Errorf("expected port 5433, got %d", c.Core.Database.Port)
				}
				if c.EventBus.BufferSizes["cytube.event"] != 2000 {
					t.Errorf("expected buffer size 2000, got %d", c.EventBus.BufferSizes["cytube.event"])
				}
				pluginConfig := c.GetPluginConfig("test_plugin")
				if pluginConfig == nil {
					t.Error("expected plugin config for test_plugin")
				}
			},
			wantErr: false,
		},
		{
			name: "backward compatibility with old format",
			content: `{
				"core": {
					"cytube": {
						"channel": "legacy-channel",
						"username": "legacyuser",
						"password": "legacypass"
					}
				}
			}`,
			validate: func(t *testing.T, c *Config) {
				// LoadFromEnv should create a room from env vars
				if err := os.Setenv("DAZ_CYTUBE_CHANNEL", "legacy-channel"); err != nil {
					t.Fatalf("Failed to set DAZ_CYTUBE_CHANNEL: %v", err)
				}
				if err := os.Setenv("DAZ_CYTUBE_USERNAME", "legacyuser"); err != nil {
					t.Fatalf("Failed to set DAZ_CYTUBE_USERNAME: %v", err)
				}
				if err := os.Setenv("DAZ_CYTUBE_PASSWORD", "legacypass"); err != nil {
					t.Fatalf("Failed to set DAZ_CYTUBE_PASSWORD: %v", err)
				}
				defer func() {
					_ = os.Unsetenv("DAZ_CYTUBE_CHANNEL")
					_ = os.Unsetenv("DAZ_CYTUBE_USERNAME")
					_ = os.Unsetenv("DAZ_CYTUBE_PASSWORD")
				}()

				c.LoadFromEnv()

				if len(c.Core.Rooms) != 1 {
					t.Errorf("expected 1 room from env vars, got %d", len(c.Core.Rooms))
				}
				if len(c.Core.Rooms) > 0 {
					if c.Core.Rooms[0].Channel != "legacy-channel" {
						t.Errorf("expected channel legacy-channel, got %s", c.Core.Rooms[0].Channel)
					}
				}
			},
			wantErr: false,
		},
		{
			name:     "invalid json",
			content:  `{invalid json`,
			validate: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.json")
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			// Load config
			config, err := LoadFromFile(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.validate != nil && config != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	// Should not error on non-existent file, just use defaults
	config, err := LoadFromFile("/nonexistent/path/config.json")
	if err != nil {
		t.Errorf("unexpected error for nonexistent file: %v", err)
	}
	if config == nil {
		t.Error("expected config with defaults for nonexistent file")
	}
}

func TestMergeWithFlags(t *testing.T) {
	config := DefaultConfig()

	// Test merging with all flags
	config.MergeWithFlags(
		"flag-channel",
		"flag-user",
		"flag-pass",
		"flag-host",
		5555,
		"flag-db",
		"flag-dbuser",
		"flag-dbpass",
	)

	if len(config.Core.Rooms) != 1 {
		t.Fatalf("expected 1 room after merge, got %d", len(config.Core.Rooms))
	}
	if config.Core.Rooms[0].Channel != "flag-channel" {
		t.Errorf("expected channel flag-channel, got %s", config.Core.Rooms[0].Channel)
	}
	if config.Core.Rooms[0].Username != "flag-user" {
		t.Errorf("expected username flag-user, got %s", config.Core.Rooms[0].Username)
	}
	if config.Core.Database.Host != "flag-host" {
		t.Errorf("expected host flag-host, got %s", config.Core.Database.Host)
	}
	if config.Core.Database.Port != 5555 {
		t.Errorf("expected port 5555, got %d", config.Core.Database.Port)
	}

	// Test partial merge (empty strings should not override)
	config2 := DefaultConfig()
	config2.MergeWithFlags("new-channel", "", "", "", 0, "", "", "")

	if len(config2.Core.Rooms) != 1 {
		t.Fatalf("expected 1 room after partial merge, got %d", len(config2.Core.Rooms))
	}
	if config2.Core.Rooms[0].Channel != "new-channel" {
		t.Errorf("expected channel new-channel, got %s", config2.Core.Rooms[0].Channel)
	}
	if config2.Core.Rooms[0].Username != "" {
		t.Errorf("expected empty username, got %s", config2.Core.Rooms[0].Username)
	}
	if config2.Core.Database.Port != 5432 {
		t.Errorf("expected default port 5432, got %d", config2.Core.Database.Port)
	}
}

func TestGetPluginConfig(t *testing.T) {
	config := DefaultConfig()

	// Add test plugin config
	testConfig := json.RawMessage(`{"enabled": true, "value": 42}`)
	config.Plugins["test"] = PluginConfig(testConfig)

	// Test existing plugin
	result := config.GetPluginConfig("test")
	if result == nil {
		t.Error("expected plugin config for 'test'")
	}

	// Verify content
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Errorf("failed to unmarshal plugin config: %v", err)
	}
	if parsed["value"].(float64) != 42 {
		t.Errorf("expected value 42, got %v", parsed["value"])
	}

	// Test non-existing plugin
	result = config.GetPluginConfig("nonexistent")
	if result != nil {
		t.Error("expected nil for nonexistent plugin")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with rooms",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: false,
		},
		{
			name: "no rooms configured",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "at least one room must be configured",
		},
		{
			name: "room missing ID is valid",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: false,
		},
		{
			name: "room missing username",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "room[0] 'room1': username is required",
		},
		{
			name: "room missing password",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "room[0] 'room1': password is required",
		},
		{
			name: "room missing channel",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "room[0]: channel is required",
		},
		{
			name: "no enabled rooms",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  false,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "at least one room must be enabled",
		},
		{
			name: "missing db host",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.Host = ""
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "database host is required",
		},
		{
			name: "invalid db port",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.Port = 0
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "database port must be positive",
		},
		{
			name: "missing db name",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.Database = ""
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = "dbpass"
			},
			wantErr: true,
			errMsg:  "database name is required (set DAZ_DB_NAME environment variable)",
		},
		{
			name: "missing db user",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = ""
				c.Core.Database.Password = "dbpass"
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "database user is required (set DAZ_DB_USER environment variable)",
		},
		{
			name: "missing db password",
			modify: func(c *Config) {
				c.Core.Rooms = []RoomConfig{
					{
						ID:       "room1",
						Username: "testuser",
						Password: "testpass",
						Channel:  "testchannel",
						Enabled:  true,
					},
				}
				c.Core.Database.User = "dbuser"
				c.Core.Database.Password = ""
				c.Core.Database.Database = "testdb"
			},
			wantErr: true,
			errMsg:  "database password is required (set DAZ_DB_PASSWORD environment variable)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.modify(config)

			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("expected error message %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}
