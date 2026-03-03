package bufftracker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

type mockEventBus struct {
	lastCorrelationID string
	lastResponse      *framework.EventData
	lastError         error
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	_ = eventType
	_ = data
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, errors.New("not implemented")
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.lastCorrelationID = correlationID
	m.lastResponse = response
	m.lastError = err
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = eventType
	_ = handler
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func TestListStartsEmpty(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	resp := dispatchRequest(t, plugin, bus, "req-list-empty", operationList, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
	})

	if !resp.PluginResponse.Success {
		t.Fatalf("expected success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Buffs) != 0 || len(payload.Debuffs) != 0 || len(payload.Effects) != 0 {
		t.Fatalf("expected empty lists, got buffs=%v debuffs=%v effects=%v", payload.Buffs, payload.Debuffs, payload.Effects)
	}
}

func TestApplyThenRemoveEffect(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	resp := dispatchRequest(t, plugin, bus, "req-apply-buff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "lucky",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected apply success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Buffs) != 1 || payload.Buffs[0].ID != "lucky" {
		t.Fatalf("expected lucky buff to be active, got %#v", payload.Buffs)
	}

	resp = dispatchRequest(t, plugin, bus, "req-remove-buff", operationRemove, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "lucky",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected remove success, got error: %s", resp.PluginResponse.Error)
	}

	payload = decodeListPayload(t, resp)
	if len(payload.Buffs) != 0 {
		t.Fatalf("expected no buffs after remove, got %#v", payload.Buffs)
	}
}

func TestApplyDebuffByAlias(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	resp := dispatchRequest(t, plugin, bus, "req-apply-debuff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "debuff",
		"effect":   "sti",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected apply success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Debuffs) != 1 || payload.Debuffs[0].ID != "std" {
		t.Fatalf("expected std debuff to be active, got %#v", payload.Debuffs)
	}
}

func TestUnknownEffectReturnsError(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	resp := dispatchRequest(t, plugin, bus, "req-unknown", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "does_not_exist",
	})
	if resp.PluginResponse.Success {
		t.Fatalf("expected failure for unknown effect")
	}

	var errorPayload map[string]interface{}
	if err := json.Unmarshal(resp.PluginResponse.Data.RawJSON, &errorPayload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if errorPayload["error_code"] != errorCodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %#v", errorPayload["error_code"])
	}
}

func TestClearAllEffects(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	dispatchRequest(t, plugin, bus, "req-add-buff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "lucky",
	})
	dispatchRequest(t, plugin, bus, "req-add-debuff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "debuff",
		"effect":   "std",
	})

	resp := dispatchRequest(t, plugin, bus, "req-clear", operationClear, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected clear success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Effects) != 0 {
		t.Fatalf("expected no effects after clear, got %#v", payload.Effects)
	}
}

func TestClearByTypeOnlyClearsThatType(t *testing.T) {
	plugin, bus := newTestPlugin(t)

	dispatchRequest(t, plugin, bus, "req-add-buff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "lucky",
	})
	dispatchRequest(t, plugin, bus, "req-add-debuff", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "debuff",
		"effect":   "std",
	})

	resp := dispatchRequest(t, plugin, bus, "req-clear-buff-only", operationClear, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected clear-by-type success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Buffs) != 0 {
		t.Fatalf("expected buffs to be cleared, got %#v", payload.Buffs)
	}
	if len(payload.Debuffs) != 1 || payload.Debuffs[0].ID != "std" {
		t.Fatalf("expected debuff to remain, got %#v", payload.Debuffs)
	}
}

func TestInitMissingDictionaryUsesFallbackDefaults(t *testing.T) {
	tempDir := t.TempDir()
	rawConfig, err := json.Marshal(pluginConfig{
		BuffDictionaryPath:   filepath.Join(tempDir, "missing-buffs.json"),
		DebuffDictionaryPath: filepath.Join(tempDir, "missing-debuffs.json"),
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	bus := &mockEventBus{}
	plugin := New().(*Plugin)
	if err := plugin.Init(rawConfig, bus); err != nil {
		t.Fatalf("init plugin with missing dictionaries: %v", err)
	}

	resp := dispatchRequest(t, plugin, bus, "req-fallback-apply", operationApply, map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"type":     "buff",
		"effect":   "lucky",
	})
	if !resp.PluginResponse.Success {
		t.Fatalf("expected fallback dictionary apply success, got error: %s", resp.PluginResponse.Error)
	}

	payload := decodeListPayload(t, resp)
	if len(payload.Buffs) != 1 || payload.Buffs[0].ID != "lucky" {
		t.Fatalf("expected fallback lucky buff, got %#v", payload.Buffs)
	}
}

func newTestPlugin(t *testing.T) (*Plugin, *mockEventBus) {
	t.Helper()

	tempDir := t.TempDir()
	buffPath := filepath.Join(tempDir, "buffs.json")
	debuffPath := filepath.Join(tempDir, "debuffs.json")

	if err := writeDefinitions(buffPath, []effectDefinition{
		{ID: "lucky", Name: "Lucky", Type: effectTypeBuff, Aliases: []string{"luck"}},
	}); err != nil {
		t.Fatalf("write buff dictionary: %v", err)
	}
	if err := writeDefinitions(debuffPath, []effectDefinition{
		{ID: "std", Name: "STD", Type: effectTypeDebuff, Aliases: []string{"sti"}},
	}); err != nil {
		t.Fatalf("write debuff dictionary: %v", err)
	}

	rawConfig, err := json.Marshal(pluginConfig{
		BuffDictionaryPath:   buffPath,
		DebuffDictionaryPath: debuffPath,
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	bus := &mockEventBus{}
	plugin := New().(*Plugin)
	if err := plugin.Init(rawConfig, bus); err != nil {
		t.Fatalf("init plugin: %v", err)
	}

	return plugin, bus
}

func writeDefinitions(path string, definitions []effectDefinition) error {
	raw, err := json.Marshal(definitions)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

func dispatchRequest(t *testing.T, plugin *Plugin, bus *mockEventBus, requestID, operation string, payload map[string]interface{}) *framework.EventData {
	t.Helper()

	bus.lastCorrelationID = ""
	bus.lastResponse = nil
	bus.lastError = nil

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   requestID,
			To:   pluginName,
			From: "test",
			Type: operation,
			Data: &framework.RequestData{
				RawJSON: raw,
			},
		},
	})

	if err := plugin.handlePluginRequest(event); err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if bus.lastError != nil {
		t.Fatalf("unexpected deliver error: %v", bus.lastError)
	}
	if bus.lastCorrelationID != requestID {
		t.Fatalf("unexpected correlation id: got %q want %q", bus.lastCorrelationID, requestID)
	}
	if bus.lastResponse == nil || bus.lastResponse.PluginResponse == nil {
		t.Fatal("expected plugin response")
	}
	return bus.lastResponse
}

func decodeListPayload(t *testing.T, response *framework.EventData) listResponse {
	t.Helper()

	var payload listResponse
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &payload); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	return payload
}
