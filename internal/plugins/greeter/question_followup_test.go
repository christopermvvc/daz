package greeter

import (
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestBuildGreetingPrompt(t *testing.T) {
	plugin := New().(*Plugin)

	got := plugin.buildGreetingPrompt("alice")
	assert.Contains(t, got, "Create a short, casual one-line welcome greeting for alice.")
	assert.Contains(t, got, questionEndHint)
	assert.Contains(t, got, followupPromptHint)
}

func TestBuildGreetingPromptWithoutQuestionAndFollowup(t *testing.T) {
	plugin := New().(*Plugin)
	plugin.config.EnforceQuestionGreeting = false
	plugin.config.EnableFollowUpMode = false

	got := plugin.buildGreetingPrompt("alice")
	assert.NotContains(t, got, questionEndHint)
	assert.NotContains(t, got, followupPromptHint)
}

func TestEnsureQuestionGreeting(t *testing.T) {
	plugin := New().(*Plugin)
	tests := []struct {
		name     string
		input    string
		expected string
		enforce  bool
	}{
		{name: "adds_question_when_missing", input: "how's it going", expected: "how's it going?", enforce: true},
		{name: "preserves_existing_question", input: "how's it going?", expected: "how's it going?", enforce: true},
		{name: "trims_period", input: "how's it going. ", expected: "how's it going?", enforce: true},
		{name: "trims_punctuation", input: "how's it going! ", expected: "how's it going?", enforce: true},
		{name: "leaves_untouched_when_disabled", input: "how's it going.", expected: "how's it going.", enforce: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin.config.EnforceQuestionGreeting = tt.enforce
			assert.Equal(t, tt.expected, plugin.ensureQuestionGreeting(tt.input))
		})
	}
}

func TestGetGreetingFromOllamaAppliesQuestionMark(t *testing.T) {
	mockBus := new(MockEventBus)
	plugin := New().(*Plugin)
	if err := plugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	mockResponse, err := json.Marshal(framework.OllamaGenerateResponse{
		Text:  "welcome to the lounge",
		Model: "test",
	})
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	mockBus.On("Request",
		mock.Anything,
		"ollama",
		"plugin.request",
		mock.MatchedBy(func(data *framework.EventData) bool {
			if data == nil || data.PluginRequest == nil || data.PluginRequest.Data == nil {
				return false
			}

			var payload framework.OllamaGenerateRequest
			if err := json.Unmarshal(data.PluginRequest.Data.RawJSON, &payload); err != nil {
				return false
			}
			assert.True(t, payload.EnableFollowUp)
			assert.Equal(t, followUpGreetingMode, payload.FollowUpMode)
			assert.True(t, payload.FollowUpRespondAll)
			assert.Contains(t, payload.Message, questionEndHint)
			assert.Contains(t, payload.Message, followupPromptHint)
			return true
		}),
		mock.Anything,
	).
		Return(&framework.EventData{
			PluginResponse: &framework.PluginResponse{
				ID:      "request-id",
				Success: true,
				Data: &framework.ResponseData{
					RawJSON: mockResponse,
				},
			},
		}, nil).
		Once()

	plugin.config.EnableFollowUpMode = true
	plugin.ollamaClient = framework.NewOllamaClient(mockBus, plugin.name)

	got := plugin.getGreetingFromOllama("testchannel", "alice", 1)
	assert.Equal(t, "welcome to the lounge?", got)
	mockBus.AssertNumberOfCalls(t, "Request", 1)
}
