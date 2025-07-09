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
			name: "rooms config is preserved",
			input: Config{
				Rooms: []RoomConfig{
					{
						ID:                "room1",
						Channel:           "test-channel",
						Username:          "test-user",
						Password:          "test-pass",
						Enabled:           true,
						ReconnectAttempts: 5,
						CooldownMinutes:   15,
					},
				},
			},
			expected: Config{
				Rooms: []RoomConfig{
					{
						ID:                "room1",
						Channel:           "test-channel",
						Username:          "test-user",
						Password:          "test-pass",
						Enabled:           true,
						ReconnectAttempts: 5,
						CooldownMinutes:   15,
					},
				},
			},
		},
		{
			name: "room defaults are applied",
			input: Config{
				Rooms: []RoomConfig{
					{
						ID:       "room1",
						Channel:  "test-channel",
						Username: "test-user",
						Password: "test-pass",
						Enabled:  true,
						// ReconnectAttempts and CooldownMinutes not set
					},
				},
			},
			expected: Config{
				Rooms: []RoomConfig{
					{
						ID:                "room1",
						Channel:           "test-channel",
						Username:          "test-user",
						Password:          "test-pass",
						Enabled:           true,
						ReconnectAttempts: 10, // Default value
						CooldownMinutes:   30, // Default value
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.input
			c.SetDefaults()

			// Compare Rooms config
			if len(c.Rooms) != len(tt.expected.Rooms) {
				t.Errorf("Number of rooms = %v, want %v", len(c.Rooms), len(tt.expected.Rooms))
				return
			}

			for i, room := range c.Rooms {
				expectedRoom := tt.expected.Rooms[i]

				if room.ID != expectedRoom.ID {
					t.Errorf("Room[%d].ID = %v, want %v", i, room.ID, expectedRoom.ID)
				}
				if room.Channel != expectedRoom.Channel {
					t.Errorf("Room[%d].Channel = %v, want %v", i, room.Channel, expectedRoom.Channel)
				}
				if room.Username != expectedRoom.Username {
					t.Errorf("Room[%d].Username = %v, want %v", i, room.Username, expectedRoom.Username)
				}
				if room.Password != expectedRoom.Password {
					t.Errorf("Room[%d].Password = %v, want %v", i, room.Password, expectedRoom.Password)
				}
				if room.Enabled != expectedRoom.Enabled {
					t.Errorf("Room[%d].Enabled = %v, want %v", i, room.Enabled, expectedRoom.Enabled)
				}
				if room.ReconnectAttempts != expectedRoom.ReconnectAttempts {
					t.Errorf("Room[%d].ReconnectAttempts = %v, want %v", i, room.ReconnectAttempts, expectedRoom.ReconnectAttempts)
				}
				if room.CooldownMinutes != expectedRoom.CooldownMinutes {
					t.Errorf("Room[%d].CooldownMinutes = %v, want %v", i, room.CooldownMinutes, expectedRoom.CooldownMinutes)
				}
			}
		})
	}
}
