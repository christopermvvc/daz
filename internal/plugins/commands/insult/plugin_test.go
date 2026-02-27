package insult

import "testing"

func TestSanitizeTarget_StripsPrefixAndPunct(t *testing.T) {
	got := sanitizeTarget("-bob,", "fallback")
	if got != "bob" {
		t.Fatalf("got %q want %q", got, "bob")
	}
}

func TestSanitizeTarget_Fallback(t *testing.T) {
	got := sanitizeTarget("   ", "fallback")
	if got != "fallback" {
		t.Fatalf("got %q want %q", got, "fallback")
	}
}

func TestPickIndexAvoidingLast_AvoidsRepeat(t *testing.T) {
	stub := func(n int) (int, error) {
		// Always return 0
		return 0, nil
	}

	idx, err := pickIndexAvoidingLast(3, 0, stub)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if idx == 0 {
		t.Fatalf("expected idx != last")
	}
}

func TestIsSelfTarget(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		botUsername string
		expected    bool
	}{
		{name: "explicit bot username", target: "MyBot", botUsername: "mybot", expected: true},
		{name: "default dazza", target: "dazza", botUsername: "", expected: true},
		{name: "default bot", target: "bot", botUsername: "", expected: true},
		{name: "non-self target", target: "someone", botUsername: "", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSelfTarget(tc.target, tc.botUsername); got != tc.expected {
				t.Fatalf("got %v want %v", got, tc.expected)
			}
		})
	}
}
