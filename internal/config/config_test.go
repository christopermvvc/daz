package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Core.Cytube.Channel != "***REMOVED***" {
		t.Errorf("expected default channel ***REMOVED***, got %s", config.Core.Cytube.Channel)
	}
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
			name: "valid config",
			content: `{
				"core": {
					"cytube": {
						"channel": "test-channel",
						"username": "testuser",
						"password": "testpass",
						"reconnect_attempts": 5,
						"cooldown_minutes": 15
					},
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
				if c.Core.Cytube.Channel != "test-channel" {
					t.Errorf("expected channel test-channel, got %s", c.Core.Cytube.Channel)
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
			name: "partial config with defaults",
			content: `{
				"core": {
					"cytube": {
						"channel": "custom-channel"
					}
				}
			}`,
			validate: func(t *testing.T, c *Config) {
				if c.Core.Cytube.Channel != "custom-channel" {
					t.Errorf("expected channel custom-channel, got %s", c.Core.Cytube.Channel)
				}
				// Should keep defaults for unspecified values
				if c.Core.Database.Host != "localhost" {
					t.Errorf("expected default host localhost, got %s", c.Core.Database.Host)
				}
				if c.Core.Cytube.ReconnectAttempts != 10 {
					t.Errorf("expected default reconnect attempts 10, got %d", c.Core.Cytube.ReconnectAttempts)
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
	_, err := LoadFromFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
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

	if config.Core.Cytube.Channel != "flag-channel" {
		t.Errorf("expected channel flag-channel, got %s", config.Core.Cytube.Channel)
	}
	if config.Core.Cytube.Username != "flag-user" {
		t.Errorf("expected username flag-user, got %s", config.Core.Cytube.Username)
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

	if config2.Core.Cytube.Channel != "new-channel" {
		t.Errorf("expected channel new-channel, got %s", config2.Core.Cytube.Channel)
	}
	if config2.Core.Cytube.Username != "***REMOVED***" {
		t.Errorf("expected default username ***REMOVED***, got %s", config2.Core.Cytube.Username)
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
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "missing channel",
			modify: func(c *Config) {
				c.Core.Cytube.Channel = ""
			},
			wantErr: true,
			errMsg:  "cytube channel is required",
		},
		{
			name: "missing db host",
			modify: func(c *Config) {
				c.Core.Database.Host = ""
			},
			wantErr: true,
			errMsg:  "database host is required",
		},
		{
			name: "invalid db port",
			modify: func(c *Config) {
				c.Core.Database.Port = 0
			},
			wantErr: true,
			errMsg:  "database port must be positive",
		},
		{
			name: "missing db name",
			modify: func(c *Config) {
				c.Core.Database.Database = ""
			},
			wantErr: true,
			errMsg:  "database name is required",
		},
		{
			name: "missing db user",
			modify: func(c *Config) {
				c.Core.Database.User = ""
			},
			wantErr: true,
			errMsg:  "database user is required",
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
