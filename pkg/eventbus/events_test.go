package eventbus

import "testing"

func TestGetEventTypePrefix(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		want      string
	}{
		{
			name:      "cytube chat message",
			eventType: "cytube.event.chatMsg",
			want:      "cytube.event",
		},
		{
			name:      "sql request",
			eventType: "sql.request",
			want:      "sql",
		},
		{
			name:      "plugin request",
			eventType: "plugin.request.command",
			want:      "plugin.request",
		},
		{
			name:      "no dots",
			eventType: "simpletEvent",
			want:      "simpletEvent",
		},
		{
			name:      "single dot at end",
			eventType: "event.",
			want:      "event",
		},
		{
			name:      "empty string",
			eventType: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetEventTypePrefix(tt.eventType); got != tt.want {
				t.Errorf("GetEventTypePrefix(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestEventConstants(t *testing.T) {
	// Verify that event constants follow expected patterns
	tests := []struct {
		constant string
		pattern  string
	}{
		{EventCytubeChatMsg, "cytube.event."},
		{EventCytubeUserJoin, "cytube.event."},
		{EventSQLRequest, "sql."},
		{EventPluginRequest, "plugin."},
	}

	for _, tt := range tests {
		if len(tt.constant) < len(tt.pattern) {
			t.Errorf("Event constant %q is too short to match pattern %q", tt.constant, tt.pattern)
			continue
		}
		if tt.constant[:len(tt.pattern)] != tt.pattern {
			t.Errorf("Event constant %q does not start with expected pattern %q", tt.constant, tt.pattern)
		}
	}
}
