package wsbridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

func TestTopicMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		subscription string
		topic        string
		want         bool
	}{
		{name: "exact", subscription: "cytube.event.chatMsg", topic: "cytube.event.chatMsg", want: true},
		{name: "wildcard prefix", subscription: "cytube.event.*", topic: "cytube.event.pm", want: true},
		{name: "wildcard miss", subscription: "cytube.event.*", topic: "plugin.response", want: false},
		{name: "empty subscription", subscription: "", topic: "plugin.response", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := topicMatches(tc.subscription, tc.topic)
			if got != tc.want {
				t.Fatalf("topicMatches(%q, %q) = %v, want %v", tc.subscription, tc.topic, got, tc.want)
			}
		})
	}
}

func TestResolveChannel(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	p.config.DefaultChannel = "main"

	profile := authProfile{
		Username: "alice",
		Channels: map[string]struct{}{
			"main": {},
			"aux":  {},
		},
	}

	channel, err := p.resolveChannel(profile, "")
	if err != nil {
		t.Fatalf("resolve default channel: %v", err)
	}
	if channel != "main" {
		t.Fatalf("expected main default channel, got %q", channel)
	}

	channel, err = p.resolveChannel(profile, "aux")
	if err != nil {
		t.Fatalf("resolve explicit channel: %v", err)
	}
	if channel != "aux" {
		t.Fatalf("expected aux channel, got %q", channel)
	}

	if _, err := p.resolveChannel(profile, "forbidden"); err == nil {
		t.Fatal("expected error for channel outside allowed list")
	}
}

func TestResolveChannelNoRestrictions(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	profile := authProfile{
		Username: "alice",
		Channels: nil,
	}

	channel, err := p.resolveChannel(profile, "main")
	if err != nil {
		t.Fatalf("resolve unrestricted channel: %v", err)
	}
	if channel != "main" {
		t.Fatalf("expected main channel, got %q", channel)
	}
}

func TestCanReceiveEventNonAdminPM(t *testing.T) {
	t.Parallel()

	client := &clientState{
		profile: authProfile{Username: "alice", Admin: false},
	}

	matching := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			FromUser: "bob",
			ToUser:   "alice",
		},
	}
	if !canReceiveEvent(client, eventbus.EventCytubePM, matching) {
		t.Fatal("expected PM to alice to be visible")
	}

	nonMatching := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			FromUser: "bob",
			ToUser:   "charlie",
		},
	}
	if canReceiveEvent(client, eventbus.EventCytubePM, nonMatching) {
		t.Fatal("expected PM unrelated to alice to be hidden")
	}
}

func TestCanReceiveEventPluginResponseFilter(t *testing.T) {
	t.Parallel()

	client := &clientState{
		profile: authProfile{Username: "alice", Admin: false},
	}

	matching := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			Data: &framework.ResponseData{
				KeyValue: map[string]string{"username": "Alice"},
			},
		},
	}
	if !canReceiveEvent(client, eventbus.EventPluginResponse, matching) {
		t.Fatal("expected response for alice to be visible")
	}

	nonMatching := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			Data: &framework.ResponseData{
				KeyValue: map[string]string{"username": "bob"},
			},
		},
	}
	if canReceiveEvent(client, eventbus.EventPluginResponse, nonMatching) {
		t.Fatal("expected response for another user to be hidden")
	}
}

func TestAuthenticateSources(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	p.config.SessionCookieName = "daz_session"
	p.tokenMap = map[string]authProfile{
		"token-query":  {Username: "query"},
		"token-bearer": {Username: "bearer"},
		"token-cookie": {Username: "cookie"},
	}

	queryReq := httptest.NewRequest("GET", "http://example.com/ws?token=token-query", nil)
	profile, ok := p.authenticate(queryReq)
	if !ok || profile.Username != "query" {
		t.Fatalf("query token auth failed: ok=%v username=%q", ok, profile.Username)
	}

	bearerReq := httptest.NewRequest("GET", "http://example.com/ws", nil)
	bearerReq.Header.Set("Authorization", "Bearer token-bearer")
	profile, ok = p.authenticate(bearerReq)
	if !ok || profile.Username != "bearer" {
		t.Fatalf("bearer token auth failed: ok=%v username=%q", ok, profile.Username)
	}

	cookieReq := httptest.NewRequest("GET", "http://example.com/ws", nil)
	cookieReq.AddCookie(&http.Cookie{Name: "daz_session", Value: "token-cookie"})
	profile, ok = p.authenticate(cookieReq)
	if !ok || profile.Username != "cookie" {
		t.Fatalf("cookie token auth failed: ok=%v username=%q", ok, profile.Username)
	}
}

func TestTokenBucketBurst(t *testing.T) {
	t.Parallel()

	bucket := newTokenBucket(1, 2)
	if !bucket.Allow() {
		t.Fatal("first request should be allowed")
	}
	if !bucket.Allow() {
		t.Fatal("second request should be allowed")
	}
	if bucket.Allow() {
		t.Fatal("third request should be rate limited without refill")
	}
}

func TestResolveBalanceQueryNonAdminUsesSessionUser(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	p.config.DefaultChannel = "always_always_sunny"
	client := &clientState{
		profile: authProfile{
			Username: "alice",
			Admin:    false,
			Channels: map[string]struct{}{"always_always_sunny": {}},
		},
	}

	channel, username, err := p.resolveBalanceQuery(client, economyBalancePayload{})
	if err != nil {
		t.Fatalf("resolveBalanceQuery default payload: %v", err)
	}
	if channel != "always_always_sunny" {
		t.Fatalf("expected default channel, got %q", channel)
	}
	if username != "alice" {
		t.Fatalf("expected session username alice, got %q", username)
	}
}

func TestResolveBalanceQueryNonAdminCannotOverrideUser(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	p.config.DefaultChannel = "always_always_sunny"
	client := &clientState{
		profile: authProfile{
			Username: "alice",
			Admin:    false,
			Channels: map[string]struct{}{"always_always_sunny": {}},
		},
	}

	_, _, err := p.resolveBalanceQuery(client, economyBalancePayload{Username: "bob"})
	if err == nil {
		t.Fatal("expected non-admin username override to fail")
	}
}

func TestResolveBalanceQueryAdminCanOverrideUser(t *testing.T) {
	t.Parallel()

	p := New().(*Plugin)
	p.config.DefaultChannel = "always_always_sunny"
	client := &clientState{
		profile: authProfile{
			Username: "admin",
			Admin:    true,
			Channels: map[string]struct{}{"always_always_sunny": {}},
		},
	}

	channel, username, err := p.resolveBalanceQuery(client, economyBalancePayload{
		Username: "bob",
		Channel:  "always_always_sunny",
	})
	if err != nil {
		t.Fatalf("resolveBalanceQuery admin override: %v", err)
	}
	if channel != "always_always_sunny" {
		t.Fatalf("expected channel always_always_sunny, got %q", channel)
	}
	if username != "bob" {
		t.Fatalf("expected overridden username bob, got %q", username)
	}
}
