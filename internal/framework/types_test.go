package framework

import (
	"encoding/json"
	"testing"
)

func TestEventDataMarshaling(t *testing.T) {
	tests := []struct {
		name string
		data EventData
		want string
	}{
		{
			name: "chat message data",
			data: EventData{
				ChatMessage: &ChatMessageData{
					Username: "testuser",
					Message:  "Hello world",
					UserRank: 1,
					UserID:   "user123",
				},
			},
			want: `{"chat_message":{"username":"testuser","message":"Hello world","user_rank":1,"user_id":"user123","channel":"","message_time":0}}`,
		},
		{
			name: "user join data",
			data: EventData{
				UserJoin: &UserJoinData{
					Username: "newuser",
					UserRank: 0,
				},
			},
			want: `{"user_join":{"username":"newuser","user_rank":0}}`,
		},
		{
			name: "key value data",
			data: EventData{
				KeyValue: map[string]string{
					"command": "test",
					"target":  "plugin-a",
				},
			},
			want: `{"key_value":{"command":"test","target":"plugin-a"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestRequestDataMarshaling(t *testing.T) {
	tests := []struct {
		name string
		data RequestData
		want string
	}{
		{
			name: "command data",
			data: RequestData{
				Command: &CommandData{
					Name: "echo",
					Args: []string{"hello", "world"},
					Params: map[string]string{
						"flag": "value",
					},
				},
			},
			want: `{"command":{"name":"echo","args":["hello","world"],"params":{"flag":"value"}}}`,
		},
		{
			name: "status query data",
			data: RequestData{
				StatusQuery: &StatusQueryData{
					IncludeMetrics: true,
					IncludeErrors:  false,
				},
			},
			want: `{"status_query":{"include_metrics":true,"include_errors":false}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestResponseDataMarshaling(t *testing.T) {
	data := ResponseData{
		CommandResult: &CommandResultData{
			Success: true,
			Output:  "Command executed successfully",
		},
	}

	want := `{"command_result":{"success":true,"output":"Command executed successfully"}}`

	got, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if string(got) != want {
		t.Errorf("json.Marshal() = %v, want %v", string(got), want)
	}
}

func TestEventDataUnmarshaling(t *testing.T) {
	input := `{"chat_message":{"username":"testuser","message":"Hello world","user_rank":1,"user_id":"user123"}}`

	var data EventData
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if data.ChatMessage == nil {
		t.Fatal("Expected ChatMessage to be non-nil")
	}

	if data.ChatMessage.Username != "testuser" {
		t.Errorf("Username = %v, want %v", data.ChatMessage.Username, "testuser")
	}

	if data.ChatMessage.Message != "Hello world" {
		t.Errorf("Message = %v, want %v", data.ChatMessage.Message, "Hello world")
	}
}
