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
			name:     "empty config remains empty",
			input:    Config{},
			expected: Config{},
		},
		{
			name: "cytube config is preserved",
			input: Config{
				Cytube: CytubeConfig{
					Channel:  "test-channel",
					Username: "test-user",
					Password: "test-pass",
				},
			},
			expected: Config{
				Cytube: CytubeConfig{
					Channel:  "test-channel",
					Username: "test-user",
					Password: "test-pass",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.input
			c.SetDefaults()

			// Compare Cytube config
			if c.Cytube.Channel != tt.expected.Cytube.Channel {
				t.Errorf("Channel = %v, want %v", c.Cytube.Channel, tt.expected.Cytube.Channel)
			}
			if c.Cytube.Username != tt.expected.Cytube.Username {
				t.Errorf("Username = %v, want %v", c.Cytube.Username, tt.expected.Cytube.Username)
			}
			if c.Cytube.Password != tt.expected.Cytube.Password {
				t.Errorf("Password = %v, want %v", c.Cytube.Password, tt.expected.Cytube.Password)
			}
		})
	}
}
