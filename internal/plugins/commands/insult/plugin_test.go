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
