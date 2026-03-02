package needs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockEventBus struct {
	mock.Mock
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	args := m.Called(eventType, data)
	return args.Error(0)
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	args := m.Called(eventType, data, metadata)
	return args.Error(0)
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	args := m.Called(target, eventType, data)
	return args.Error(0)
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	args := m.Called(target, eventType, data, metadata)
	return args.Error(0)
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	args := m.Called(ctx, target, eventType, data, metadata)
	return args.Get(0).(*framework.EventData), args.Error(1)
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.Called(correlationID, response, err)
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	args := m.Called(eventType, handler)
	return args.Error(0)
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	args := m.Called(pattern, handler, tags)
	return args.Error(0)
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	args := m.Called(name, plugin)
	return args.Error(0)
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	args := m.Called()
	return args.Get(0).(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	args := m.Called(eventType)
	return args.Get(0).(int64)
}

func TestNeedsTrackerConfigDefaults(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := defaultNeedsTrackerConfig()
		assert.NotNil(t, cfg.EnableBuffReporting)
		assert.True(t, *cfg.EnableBuffReporting)
		assert.Equal(t, 3, cfg.MaxEffectListSize)
	})
}

func TestMergeNeedsTrackerConfig(t *testing.T) {
	disabled := false
	cfg := mergeNeedsTrackerConfig(needsTrackerConfig{
		EnableBuffReporting: &disabled,
		MaxEffectListSize:   7,
	})

	assert.NotNil(t, cfg.EnableBuffReporting)
	assert.False(t, *cfg.EnableBuffReporting)
	assert.Equal(t, 7, cfg.MaxEffectListSize)
}

func TestFormatNeedsMessage(t *testing.T) {
	state := framework.PlayerState{
		Food:    12,
		Alcohol: 45,
		Weed:    87,
		Lust:    32,
		Bladder: 99,
	}

	t.Run("hides buffs when disabled", func(t *testing.T) {
		msg := formatNeedsMessage("alice", state, []string{"Lucky", "Hot"}, []string{"STD"}, false, 3)
		assert.Equal(
			t,
			"alice: 🍽 Hunger: [█░░░░░░░] (Satisfied) || 🍺 Drunk: [████░░░░] (Already loud) || 🍃 High: [███████░] (Spacey and loud) || 🌶 Horny: [███░░░░░] (Already thinking about it) || 🚽 Bladder: [████████] (Fuller than a water balloon)",
			msg,
		)
		assert.NotContains(t, msg, "Buffs:")
		assert.NotContains(t, msg, "Debuffs:")
	})

	t.Run("shows truncated buffs and debuffs", func(t *testing.T) {
		msg := formatNeedsMessage("alice", state, []string{"Lucky", "Hot"}, []string{"STD", "Clumsy"}, true, 1)
		assert.Equal(
			t,
			"alice: 🍽 Hunger: [█░░░░░░░] (Satisfied) || 🍺 Drunk: [████░░░░] (Already loud) || 🍃 High: [███████░] (Spacey and loud) || 🌶 Horny: [███░░░░░] (Already thinking about it) || 🚽 Bladder: [████████] (Fuller than a water balloon) || Buffs: Lucky +1 more || Debuffs: STD +1 more",
			msg,
		)
	})
}

func TestNeedFormatterHelpers(t *testing.T) {
	t.Run("renders progress bar for min and max", func(t *testing.T) {
		assert.Equal(t, "[░░░░░░░░]", renderNeedBar(0))
		assert.Equal(t, "[████████]", renderNeedBar(100))
		assert.Equal(t, "[████░░░░]", renderNeedBar(45))
	})

	t.Run("formats need line", func(t *testing.T) {
		result := formatNeedLine("🍺", "Drunk", 45, "already lit")
		assert.Equal(t, "🍺 Drunk: [████░░░░] (already lit)", result)
	})
}

func TestFormatEffectList(t *testing.T) {
	t.Run("full when under max", func(t *testing.T) {
		result := formatEffectList("none", []string{"A", "B"}, 3)
		assert.Equal(t, "A, B", result)
	})

	t.Run("truncated when over max", func(t *testing.T) {
		result := formatEffectList("none", []string{"A", "B", "C"}, 2)
		assert.Equal(t, "A, B +1 more", result)
	})

	t.Run("falls back when empty", func(t *testing.T) {
		result := formatEffectList("none", nil, 2)
		assert.Equal(t, "none", result)
	})
}

func TestParseTrackerEffects(t *testing.T) {
	t.Run("object payload", func(t *testing.T) {
		raw := json.RawMessage(`{
			"buffs":[{"name":"Lucky"},{"name":"Tough Skin","type":"buff"}],
			"debuffs":[{"name":"STD","type":"debuff"}],
			"effects":[{"name":"Nimble","type":"buff"},{"name":"Clumsy","type":"debuff"}]
		}`)
		buffs, debuffs, err := parseTrackerEffects(raw)
		assert.NoError(t, err)
		assert.Equal(t, []string{"Lucky", "Nimble", "Tough Skin"}, buffs)
		assert.Equal(t, []string{"Clumsy", "STD"}, debuffs)
	})

	t.Run("array payload", func(t *testing.T) {
		raw := json.RawMessage(`[
			{"name":"Lucky","type":"buff"},
			{"name":"STD","type":"debuff"},
			{"name":"Lucky","type":"buff"}
		]`)
		buffs, debuffs, err := parseTrackerEffects(raw)
		assert.NoError(t, err)
		assert.Equal(t, []string{"Lucky"}, buffs)
		assert.Equal(t, []string{"STD"}, debuffs)
	})

	t.Run("invalid payload", func(t *testing.T) {
		raw := json.RawMessage(`{bad json}`)
		_, _, err := parseTrackerEffects(raw)
		assert.Error(t, err)
	})
}

func TestHandleCommandCanSkipBuffTracker(t *testing.T) {
	mockBus := new(MockEventBus)
	state := framework.PlayerState{
		Food:    10,
		Alcohol: 11,
		Weed:    12,
		Lust:    13,
		Bladder: 14,
	}

	rawState, err := json.Marshal(framework.PlayerStateResponse{
		Channel:     "room",
		Username:    "alice",
		PlayerState: state,
	})
	assert.NoError(t, err)

	mockBus.On("Request", mock.Anything, "playerstate", "plugin.request", mock.Anything, mock.Anything).
		Return(&framework.EventData{
			PluginResponse: &framework.PluginResponse{
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: rawState,
				},
			},
		}, nil).
		Once()

	var output string
	mockBus.On("Broadcast", "plugin.response", mock.Anything).Run(func(args mock.Arguments) {
		event := args.Get(1).(*framework.EventData)
		output = event.PluginResponse.Data.CommandResult.Output
	}).Return(nil).Once()

	rawConfig := []byte(`{"enable_buff_reporting":false}`)
	plugin := New().(*Plugin)
	err = plugin.Init(rawConfig, mockBus)
	assert.NoError(t, err)

	event := framework.NewDataEvent("command.needs.execute", &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: "needs",
					Params: map[string]string{
						"channel":  "room",
						"username": "alice",
					},
				},
			},
		},
	})

	err = plugin.handleCommand(event)
	assert.NoError(t, err)
	assert.Equal(
		t,
		"alice: 🍽 Hunger: [█░░░░░░░] (Satisfied) || 🍺 Drunk: [█░░░░░░░] (Just warm, barely) || 🍃 High: [█░░░░░░░] (Breezy) || 🌶 Horny: [█░░░░░░░] (Curious) || 🚽 Bladder: [█░░░░░░░] (Comfortably in control)",
		output,
	)
	mockBus.AssertExpectations(t)
}

func TestHandleCommandLoadsBuffTracker(t *testing.T) {
	mockBus := new(MockEventBus)

	rawState, err := json.Marshal(framework.PlayerStateResponse{
		Channel:  "room",
		Username: "alice",
		PlayerState: framework.PlayerState{
			Food:    0,
			Alcohol: 0,
			Weed:    0,
			Lust:    0,
			Bladder: 0,
		},
	})
	assert.NoError(t, err)

	rawTracker, err := json.Marshal(map[string]interface{}{
		"buffs": []map[string]string{
			{"name": "Nimble"},
		},
		"debuffs": []map[string]string{
			{"name": "STD"},
		},
		"effects": []map[string]string{
			{"name": "Slow", "type": "debuff"},
			{"name": "Lucky", "type": "buff"},
		},
	})
	assert.NoError(t, err)

	mockBus.On("Request", mock.Anything, "playerstate", "plugin.request", mock.Anything, mock.Anything).
		Return(&framework.EventData{
			PluginResponse: &framework.PluginResponse{
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: rawState,
				},
			},
		}, nil).
		Once()
	mockBus.On("Request", mock.Anything, "bufftracker", "plugin.request", mock.Anything, mock.Anything).
		Return(&framework.EventData{
			PluginResponse: &framework.PluginResponse{
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: rawTracker,
				},
			},
		}, nil).
		Once()

	var output string
	mockBus.On("Broadcast", "plugin.response", mock.Anything).Run(func(args mock.Arguments) {
		event := args.Get(1).(*framework.EventData)
		output = event.PluginResponse.Data.CommandResult.Output
	}).Return(nil).Once()

	rawConfig := []byte(`{"enable_buff_reporting":true,"max_effect_list_size":1}`)
	plugin := New().(*Plugin)
	err = plugin.Init(rawConfig, mockBus)
	assert.NoError(t, err)

	event := framework.NewDataEvent("command.needs.execute", &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: "needs",
					Params: map[string]string{
						"channel":  "room",
						"username": "alice",
					},
				},
			},
		},
	})

	err = plugin.handleCommand(event)
	assert.NoError(t, err)
	assert.Equal(
		t,
		"alice: 🍽 Hunger: [░░░░░░░░] (Stuffed like a corpse) || 🍺 Drunk: [░░░░░░░░] (Sane as a nun) || 🍃 High: [░░░░░░░░] (Clear headed) || 🌶 Horny: [░░░░░░░░] (Chaste and polite) || 🚽 Bladder: [░░░░░░░░] (Dry as dust) || Buffs: Lucky +1 more || Debuffs: STD +1 more",
		output,
	)
	mockBus.AssertExpectations(t)
}
