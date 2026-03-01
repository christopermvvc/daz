package remind

import (
	"testing"
	"time"
)

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		{"5m", true},
		{"5min", true},
		{"5mins", true},
		{"5minutes", true},
		{"5 minutes", true},
		{"1h30m", true},
		{"1h 30m", true},
		{"1 hour 30 minutes", true},
		{"2hrs", true},
		{"1day", true},
		{"2d", true},
		{"10s", true},
		{"1h0m", true},
		{"15", true},
		{"", false},
		{"5", true},
		{"m5", false},
		{"5x", false},
		{"1h30", false},
	}

	for _, tc := range tests {
		_, ok := parseTimeString(tc.input)
		if ok != tc.ok {
			t.Fatalf("parseTimeString(%q) ok=%v want %v", tc.input, ok, tc.ok)
		}
	}
}

func TestParseTimeStringImplicitMinutes(t *testing.T) {
	d, ok := parseTimeString("15")
	if !ok {
		t.Fatal("expected implicit minute input to parse")
	}
	if d != 15*time.Minute {
		t.Fatalf("expected 15m duration, got %v", d)
	}
}

func TestParseReminderDuration(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantInput    string
		wantDuration time.Duration
		wantOK       bool
	}{
		{
			name:         "single token implicit minutes",
			args:         []string{"15"},
			wantInput:    "15",
			wantDuration: 15 * time.Minute,
			wantOK:       true,
		},
		{
			name:         "multi token compact duration",
			args:         []string{"1h", "30m"},
			wantInput:    "1h 30m",
			wantDuration: 90 * time.Minute,
			wantOK:       true,
		},
		{
			name:         "multi token natural language duration",
			args:         []string{"1", "hour", "30", "minutes"},
			wantInput:    "1 hour 30 minutes",
			wantDuration: 90 * time.Minute,
			wantOK:       true,
		},
		{
			name:      "empty args",
			args:      []string{},
			wantInput: "",
			wantOK:    false,
		},
		{
			name:      "invalid duration",
			args:      []string{"soon", "ish"},
			wantInput: "soon ish",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, duration, ok := parseReminderDuration(tc.args)
			if ok != tc.wantOK {
				t.Fatalf("parseReminderDuration(%v) ok=%v want %v", tc.args, ok, tc.wantOK)
			}
			if input != tc.wantInput {
				t.Fatalf("parseReminderDuration(%v) input=%q want %q", tc.args, input, tc.wantInput)
			}
			if tc.wantOK && duration != tc.wantDuration {
				t.Fatalf("parseReminderDuration(%v) duration=%v want %v", tc.args, duration, tc.wantDuration)
			}
		})
	}
}
