package core

import (
	"testing"
)

func TestConfig_SetDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name:  "empty config gets all defaults",
			input: Config{},
			expected: Config{
				Database: DatabaseConfig{
					Host:            "localhost",
					Port:            5432,
					MaxConnections:  10,
					ConnectTimeout:  10,
					MaxConnLifetime: 3600,
				},
			},
		},
		{
			name: "partial config preserves set values",
			input: Config{
				Database: DatabaseConfig{
					Host:     "custom-host",
					Port:     5433,
					Database: "mydb",
					User:     "myuser",
					Password: "mypass",
				},
			},
			expected: Config{
				Database: DatabaseConfig{
					Host:            "custom-host",
					Port:            5433,
					Database:        "mydb",
					User:            "myuser",
					Password:        "mypass",
					MaxConnections:  10,
					ConnectTimeout:  10,
					MaxConnLifetime: 3600,
				},
			},
		},
		{
			name: "custom values are preserved",
			input: Config{
				Database: DatabaseConfig{
					Host:            "db.example.com",
					Port:            5432,
					MaxConnections:  50,
					ConnectTimeout:  30,
					MaxConnLifetime: 7200,
				},
			},
			expected: Config{
				Database: DatabaseConfig{
					Host:            "db.example.com",
					Port:            5432,
					MaxConnections:  50,
					ConnectTimeout:  30,
					MaxConnLifetime: 7200,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.input
			c.SetDefaults()

			if c.Database.Host != tt.expected.Database.Host {
				t.Errorf("Host = %v, want %v", c.Database.Host, tt.expected.Database.Host)
			}
			if c.Database.Port != tt.expected.Database.Port {
				t.Errorf("Port = %v, want %v", c.Database.Port, tt.expected.Database.Port)
			}
			if c.Database.MaxConnections != tt.expected.Database.MaxConnections {
				t.Errorf("MaxConnections = %v, want %v", c.Database.MaxConnections, tt.expected.Database.MaxConnections)
			}
			if c.Database.ConnectTimeout != tt.expected.Database.ConnectTimeout {
				t.Errorf("ConnectTimeout = %v, want %v", c.Database.ConnectTimeout, tt.expected.Database.ConnectTimeout)
			}
			if c.Database.MaxConnLifetime != tt.expected.Database.MaxConnLifetime {
				t.Errorf("MaxConnLifetime = %v, want %v", c.Database.MaxConnLifetime, tt.expected.Database.MaxConnLifetime)
			}
		})
	}
}
