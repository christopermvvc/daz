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
		{"1h30m", true},
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
