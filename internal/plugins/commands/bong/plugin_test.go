package bong

import (
	"testing"
	"time"
)

func TestPickIndexAvoidingLast_AvoidsRepeat(t *testing.T) {
	stub := func(n int) (int, error) {
		return 0, nil
	}

	idx, err := pickIndexAvoidingLast(4, 0, stub)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if idx == 0 {
		t.Fatalf("expected idx != last")
	}
}

func TestLimit_TruncatesRunes(t *testing.T) {
	p := New().(*Plugin)
	p.maxRunes = 3

	got := p.limit("abcdef")
	if got != "abc..." {
		t.Fatalf("got %q want %q", got, "abc...")
	}
}

func TestFormatCooldownMessage_ReplacesPlaceholder(t *testing.T) {
	p := New().(*Plugin)
	p.config.CooldownMessage = "wait {time}s"

	msg := p.formatCooldownMessage(1500 * time.Second)
	if msg != "wait 1500s" {
		t.Fatalf("got %q want %q", msg, "wait 1500s")
	}
}
