package tell

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessTellCommand_SelfTell(t *testing.T) {
	p := New().(*Plugin)
	called := false
	p.sendResponseFunc = func(channel, username, message string, isPM bool) {
		called = true
		if !strings.Contains(message, "talk to yourself") {
			t.Fatalf("unexpected self tell response: %q", message)
		}
	}
	p.checkOnlineFunc = func(channel, username string) (bool, error) { return false, nil }
	if err := p.Init(nil, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	p.processTellCommand("chan", "bob", "bob hey mate", false)
	if !called {
		t.Fatal("expected response for self-tell")
	}
}

func TestProcessTellCommand_BotTell(t *testing.T) {
	p := New().(*Plugin)
	called := false
	p.sendResponseFunc = func(channel, username, message string, isPM bool) {
		called = true
		if !strings.Contains(message, "not gonna talk") {
			t.Fatalf("unexpected bot tell response: %q", message)
		}
	}
	p.checkOnlineFunc = func(channel, username string) (bool, error) { return false, nil }
	t.Setenv("DAZ_BOT_NAME", "Dazza")
	if err := p.Init(nil, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	p.processTellCommand("chan", "bob", "Dazza hey mate", false)
	if !called {
		t.Fatal("expected response for bot-tell")
	}
}

func TestProcessTellCommand_DuplicatePending(t *testing.T) {
	p := New().(*Plugin)
	p.checkOnlineFunc = func(channel, username string) (bool, error) { return false, nil }
	p.pendingSenderFunc = func(channel, username string) (string, bool, error) {
		return "alice", true, nil
	}
	called := false
	p.sendResponseFunc = func(channel, username, message string, isPM bool) {
		called = true
		if !strings.Contains(message, "alice") {
			t.Fatalf("expected existing sender in response: %q", message)
		}
	}
	if err := p.Init(nil, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	p.processTellCommand("chan", "bob", "carl hello", false)
	if !called {
		t.Fatal("expected response for duplicate pending")
	}
}

func TestProcessTellCommand_DuplicateInsert(t *testing.T) {
	p := New().(*Plugin)
	p.checkOnlineFunc = func(channel, username string) (bool, error) { return false, nil }
	p.pendingSenderFunc = func(channel, username string) (string, bool, error) { return "", false, nil }
	p.storeMessageFunc = func(channel, fromUser, toUser, message string, isPM bool) error {
		return errTellAlreadyPending
	}
	called := false
	p.sendResponseFunc = func(channel, username, message string, isPM bool) {
		called = true
		if !strings.Contains(message, "inbox") {
			t.Fatalf("expected inbox full response: %q", message)
		}
	}
	if err := p.Init(nil, nil); err != nil {
		t.Fatalf("init: %v", err)
	}

	p.processTellCommand("chan", "bob", "carl hello", false)
	if !called {
		t.Fatal("expected response for duplicate insert")
	}
}

func TestMessageLifetimeIsSevenDays(t *testing.T) {
	if messageLifetime != 7*24*time.Hour {
		t.Fatalf("messageLifetime = %v", messageLifetime)
	}
}

func TestResolveBotUsernameFallback(t *testing.T) {
	t.Setenv("DAZ_BOT_NAME", "")
	t.Setenv("DAZ_CYTUBE_USERNAME", "")
	if got := resolveBotUsername(); got != "" {
		t.Fatalf("expected empty bot username fallback, got %q", got)
	}
}

func TestDuplicateTellErrorDetection(t *testing.T) {
	if !errors.Is(errTellAlreadyPending, errTellAlreadyPending) {
		t.Fatalf("expected sentinel error to match itself")
	}
	if !isDuplicateTellError(errors.New("duplicate key value violates unique constraint \"idx_tell_pending_unique\"")) {
		t.Fatalf("expected duplicate error detection")
	}
}
