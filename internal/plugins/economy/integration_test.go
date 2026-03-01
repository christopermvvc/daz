//go:build integration
// +build integration

package economy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	sqlplugin "github.com/hildolfr/daz/internal/plugins/sql"
	"github.com/hildolfr/daz/pkg/eventbus"
)

type integrationHarness struct {
	eventBus *eventbus.EventBus
	plugins  *framework.PluginManager
	client   *framework.EconomyClient
}

func TestTransfer_IdempotentSameKey(t *testing.T) {
	h := newIntegrationHarness(t)
	t.Cleanup(h.close)

	channel := testChannel("idempotent")
	alice := fmt.Sprintf("alice-%d", time.Now().UnixNano())
	bob := fmt.Sprintf("bob-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := h.client.Credit(ctx, framework.CreditRequest{
		Channel:        channel,
		Username:       alice,
		Amount:         1000,
		IdempotencyKey: fmt.Sprintf("credit-%d", time.Now().UnixNano()),
		Reason:         "integration setup",
	})
	if err != nil {
		t.Fatalf("credit alice: %v", err)
	}

	key := fmt.Sprintf("transfer-same-key-%d", time.Now().UnixNano())
	transferReq := framework.TransferRequest{
		Channel:        channel,
		FromUsername:   alice,
		ToUsername:     bob,
		Amount:         250,
		IdempotencyKey: key,
		Reason:         "idempotency verification",
	}

	firstResp, err := h.client.Transfer(ctx, transferReq)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if firstResp.AlreadyApplied {
		t.Fatal("expected first transfer to be newly applied")
	}

	secondResp, err := h.client.Transfer(ctx, transferReq)
	if err != nil {
		t.Fatalf("second transfer with same key: %v", err)
	}
	if !secondResp.AlreadyApplied {
		t.Fatal("expected second transfer to report already_applied=true")
	}

	aliceBalance, err := h.client.GetBalance(ctx, channel, alice)
	if err != nil {
		t.Fatalf("get alice balance: %v", err)
	}
	bobBalance, err := h.client.GetBalance(ctx, channel, bob)
	if err != nil {
		t.Fatalf("get bob balance: %v", err)
	}

	if aliceBalance != 750 {
		t.Fatalf("expected alice balance 750, got %d", aliceBalance)
	}
	if bobBalance != 250 {
		t.Fatalf("expected bob balance 250, got %d", bobBalance)
	}
}

func TestTransfer_IdempotencyConflict(t *testing.T) {
	h := newIntegrationHarness(t)
	t.Cleanup(h.close)

	channel := testChannel("conflict")
	alice := fmt.Sprintf("alice-%d", time.Now().UnixNano())
	bob := fmt.Sprintf("bob-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := h.client.Credit(ctx, framework.CreditRequest{
		Channel:        channel,
		Username:       alice,
		Amount:         1000,
		IdempotencyKey: fmt.Sprintf("credit-%d", time.Now().UnixNano()),
		Reason:         "integration setup",
	})
	if err != nil {
		t.Fatalf("credit alice: %v", err)
	}

	key := fmt.Sprintf("transfer-conflict-key-%d", time.Now().UnixNano())
	_, err = h.client.Transfer(ctx, framework.TransferRequest{
		Channel:        channel,
		FromUsername:   alice,
		ToUsername:     bob,
		Amount:         100,
		IdempotencyKey: key,
		Reason:         "first apply",
	})
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}

	_, err = h.client.Transfer(ctx, framework.TransferRequest{
		Channel:        channel,
		FromUsername:   alice,
		ToUsername:     bob,
		Amount:         125,
		IdempotencyKey: key,
		Reason:         "conflicting payload",
	})
	if err == nil {
		t.Fatal("expected idempotency conflict on second transfer")
	}

	var economyErr *framework.EconomyError
	if !errors.As(err, &economyErr) {
		t.Fatalf("expected EconomyError, got %T (%v)", err, err)
	}
	if economyErr.ErrorCode != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("expected error_code IDEMPOTENCY_CONFLICT, got %q", economyErr.ErrorCode)
	}

	aliceBalance, err := h.client.GetBalance(ctx, channel, alice)
	if err != nil {
		t.Fatalf("get alice balance: %v", err)
	}
	bobBalance, err := h.client.GetBalance(ctx, channel, bob)
	if err != nil {
		t.Fatalf("get bob balance: %v", err)
	}

	if aliceBalance != 900 {
		t.Fatalf("expected alice balance 900 after conflict, got %d", aliceBalance)
	}
	if bobBalance != 100 {
		t.Fatalf("expected bob balance 100 after conflict, got %d", bobBalance)
	}
}

func TestTransfer_ConcurrentOpposing(t *testing.T) {
	h := newIntegrationHarness(t)
	t.Cleanup(h.close)

	channel := testChannel("concurrent")
	userA := fmt.Sprintf("usera-%d", time.Now().UnixNano())
	userB := fmt.Sprintf("userb-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := h.client.Credit(ctx, framework.CreditRequest{
		Channel:        channel,
		Username:       userA,
		Amount:         1000,
		IdempotencyKey: fmt.Sprintf("credit-a-%d", time.Now().UnixNano()),
		Reason:         "integration setup",
	})
	if err != nil {
		t.Fatalf("credit userA: %v", err)
	}
	_, err = h.client.Credit(ctx, framework.CreditRequest{
		Channel:        channel,
		Username:       userB,
		Amount:         1000,
		IdempotencyKey: fmt.Sprintf("credit-b-%d", time.Now().UnixNano()),
		Reason:         "integration setup",
	})
	if err != nil {
		t.Fatalf("credit userB: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, transferErr := h.client.Transfer(ctx, framework.TransferRequest{
			Channel:        channel,
			FromUsername:   userA,
			ToUsername:     userB,
			Amount:         175,
			IdempotencyKey: fmt.Sprintf("concurrent-a-to-b-%d", time.Now().UnixNano()),
			Reason:         "concurrency test",
		})
		errs <- transferErr
	}()

	go func() {
		defer wg.Done()
		_, transferErr := h.client.Transfer(ctx, framework.TransferRequest{
			Channel:        channel,
			FromUsername:   userB,
			ToUsername:     userA,
			Amount:         40,
			IdempotencyKey: fmt.Sprintf("concurrent-b-to-a-%d", time.Now().UnixNano()),
			Reason:         "concurrency test",
		})
		errs <- transferErr
	}()

	wg.Wait()
	close(errs)

	for transferErr := range errs {
		if transferErr != nil {
			t.Fatalf("concurrent transfer returned error: %v", transferErr)
		}
	}

	balanceA, err := h.client.GetBalance(ctx, channel, userA)
	if err != nil {
		t.Fatalf("get userA balance: %v", err)
	}
	balanceB, err := h.client.GetBalance(ctx, channel, userB)
	if err != nil {
		t.Fatalf("get userB balance: %v", err)
	}

	if balanceA != 865 {
		t.Fatalf("expected userA balance 865, got %d", balanceA)
	}
	if balanceB != 1135 {
		t.Fatalf("expected userB balance 1135, got %d", balanceB)
	}
	if balanceA < 0 || balanceB < 0 {
		t.Fatalf("expected non-negative balances, got userA=%d userB=%d", balanceA, balanceB)
	}
}

func newIntegrationHarness(t *testing.T) *integrationHarness {
	t.Helper()

	// Change working directory to repo root so SQL migrations can be found.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get caller info")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("failed to change dir to %s: %v", repoRoot, err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Errorf("failed to restore cwd: %v", err)
		}
	})

	sqlConfig := mustBuildSQLConfigFromEnv(t)

	bus := eventbus.NewEventBus(nil)
	if err := bus.Start(); err != nil {
		t.Fatalf("start event bus: %v", err)
	}

	pm := framework.NewPluginManager()
	sqlPlugin := sqlplugin.NewPlugin()
	economyPlugin := New()
	if err := pm.RegisterPlugin("sql", sqlPlugin); err != nil {
		_ = bus.Stop()
		t.Fatalf("register sql plugin: %v", err)
	}
	if err := pm.RegisterPlugin(pluginName, economyPlugin); err != nil {
		_ = bus.Stop()
		t.Fatalf("register economy plugin: %v", err)
	}

	pluginConfigs := map[string]json.RawMessage{
		"sql":     sqlConfig,
		"economy": json.RawMessage(`{}`),
	}
	if err := pm.InitializeAll(pluginConfigs, bus); err != nil {
		_ = bus.Stop()
		t.Fatalf("initialize plugins: %v", err)
	}
	if err := pm.StartAll(); err != nil {
		pm.StopAll()
		_ = bus.Stop()
		t.Fatalf("start plugins: %v", err)
	}

	if err := waitForPluginReadiness([]framework.Plugin{sqlPlugin, economyPlugin}, 20*time.Second); err != nil {
		pm.StopAll()
		_ = bus.Stop()
		t.Fatalf("wait for readiness: %v", err)
	}

	return &integrationHarness{
		eventBus: bus,
		plugins:  pm,
		client:   framework.NewEconomyClient(bus, "economy-integration-test"),
	}
}

func (h *integrationHarness) close() {
	if h == nil {
		return
	}
	h.plugins.StopAll()
	_ = h.eventBus.Stop()
}

func mustBuildSQLConfigFromEnv(t *testing.T) json.RawMessage {
	t.Helper()

	host := os.Getenv("DAZ_DB_HOST")
	portRaw := os.Getenv("DAZ_DB_PORT")
	database := os.Getenv("DAZ_DB_NAME")
	user := os.Getenv("DAZ_DB_USER")
	password := os.Getenv("DAZ_DB_PASSWORD")

	missing := make([]string, 0, 5)
	if host == "" {
		missing = append(missing, "DAZ_DB_HOST")
	}
	if portRaw == "" {
		missing = append(missing, "DAZ_DB_PORT")
	}
	if database == "" {
		missing = append(missing, "DAZ_DB_NAME")
	}
	if user == "" {
		missing = append(missing, "DAZ_DB_USER")
	}
	if password == "" {
		missing = append(missing, "DAZ_DB_PASSWORD")
	}
	if len(missing) > 0 {
		t.Skipf("skipping integration test, missing DB env vars: %v", missing)
	}

	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("invalid DAZ_DB_PORT %q: %v", portRaw, err)
	}

	config := map[string]interface{}{
		"database": map[string]interface{}{
			"host":              host,
			"port":              port,
			"database":          database,
			"user":              user,
			"password":          password,
			"max_connections":   8,
			"max_conn_lifetime": 300,
			"connect_timeout":   10,
		},
		"logger_rules": []interface{}{},
	}

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal sql plugin config: %v", err)
	}

	return raw
}

func waitForPluginReadiness(plugins []framework.Plugin, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		allReady := true
		for _, plugin := range plugins {
			readyPlugin, ok := plugin.(interface{ Ready() bool })
			if !ok {
				continue
			}
			if !readyPlugin.Ready() {
				allReady = false
				break
			}
		}

		if allReady {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for plugin readiness")
		}

		<-ticker.C
	}
}

func testChannel(suffix string) string {
	return fmt.Sprintf("test-economy-%s-%d", suffix, time.Now().UnixNano())
}
