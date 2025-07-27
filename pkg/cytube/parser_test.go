package cytube

import (
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewParser(t *testing.T) {
	parser := NewParser("test-channel", "test-room")
	if parser.channel != "test-channel" {
		t.Errorf("channel = %v, want %v", parser.channel, "test-channel")
	}
	if parser.roomID != "test-room" {
		t.Errorf("roomID = %v, want %v", parser.roomID, "test-room")
	}
}

func TestParseChatMessage(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{"username":"testuser","msg":"Hello world","rank":1,"meta":{"uid":"user123"}}`)
	event := Event{
		Type: "chatMsg",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	chatMsg, ok := parsed.(*framework.ChatMessageEvent)
	if !ok {
		t.Fatalf("Expected ChatMessageEvent, got %T", parsed)
	}

	if chatMsg.Username != "testuser" {
		t.Errorf("Username = %v, want %v", chatMsg.Username, "testuser")
	}

	if chatMsg.Message != "Hello world" {
		t.Errorf("Message = %v, want %v", chatMsg.Message, "Hello world")
	}

	if chatMsg.UserRank != 1 {
		t.Errorf("UserRank = %v, want %v", chatMsg.UserRank, 1)
	}

	if chatMsg.UserID != "user123" {
		t.Errorf("UserID = %v, want %v", chatMsg.UserID, "user123")
	}
}

func TestParseUserJoin(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{"name":"newuser","rank":2}`)
	event := Event{
		Type: "userJoin",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	userJoin, ok := parsed.(*framework.UserJoinEvent)
	if !ok {
		t.Fatalf("Expected UserJoinEvent, got %T", parsed)
	}

	if userJoin.Username != "newuser" {
		t.Errorf("Username = %v, want %v", userJoin.Username, "newuser")
	}

	if userJoin.UserRank != 2 {
		t.Errorf("UserRank = %v, want %v", userJoin.UserRank, 2)
	}
}

func TestParseUserLeave(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{"name":"leavinguser"}`)
	event := Event{
		Type: "userLeave",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	userLeave, ok := parsed.(*framework.UserLeaveEvent)
	if !ok {
		t.Fatalf("Expected UserLeaveEvent, got %T", parsed)
	}

	if userLeave.Username != "leavinguser" {
		t.Errorf("Username = %v, want %v", userLeave.Username, "leavinguser")
	}
}

func TestParseVideoChange(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{"id":"dQw4w9WgXcQ","type":"youtube","duration":212,"title":"Never Gonna Give You Up"}`)
	event := Event{
		Type: "changeMedia",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	videoChange, ok := parsed.(*framework.VideoChangeEvent)
	if !ok {
		t.Fatalf("Expected VideoChangeEvent, got %T", parsed)
	}

	if videoChange.VideoID != "dQw4w9WgXcQ" {
		t.Errorf("VideoID = %v, want %v", videoChange.VideoID, "dQw4w9WgXcQ")
	}

	if videoChange.VideoType != "youtube" {
		t.Errorf("VideoType = %v, want %v", videoChange.VideoType, "youtube")
	}

	if videoChange.Duration != 212 {
		t.Errorf("Duration = %v, want %v", videoChange.Duration, 212)
	}

	if videoChange.Title != "Never Gonna Give You Up" {
		t.Errorf("Title = %v, want %v", videoChange.Title, "Never Gonna Give You Up")
	}
}

func TestParseVideoChangeWithStringDuration(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	// Test with duration as a string
	rawData := json.RawMessage(`{"id":"abc123","type":"youtube","duration":"212","title":"Test Video"}`)
	event := Event{
		Type: "changeMedia",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	videoChange, ok := parsed.(*framework.VideoChangeEvent)
	if !ok {
		t.Fatalf("Expected VideoChangeEvent, got %T", parsed)
	}

	if videoChange.VideoID != "abc123" {
		t.Errorf("VideoID = %v, want %v", videoChange.VideoID, "abc123")
	}

	if videoChange.VideoType != "youtube" {
		t.Errorf("VideoType = %v, want %v", videoChange.VideoType, "youtube")
	}

	if videoChange.Duration != 212 {
		t.Errorf("Duration = %v, want %v", videoChange.Duration, 212)
	}

	if videoChange.Title != "Test Video" {
		t.Errorf("Title = %v, want %v", videoChange.Title, "Test Video")
	}
}

func TestParseUnknownEvent(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{"some":"data"}`)
	event := Event{
		Type: "unknownEvent",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	genericEvent, ok := parsed.(*framework.GenericEvent)
	if !ok {
		t.Fatalf("Expected GenericEvent, got %T", parsed)
	}

	if genericEvent.Type() != "unknownEvent" {
		t.Errorf("Type() = %v, want %v", genericEvent.Type(), "unknownEvent")
	}

	if genericEvent.UnknownType != "unknownEvent" {
		t.Errorf("UnknownType = %v, want %v", genericEvent.UnknownType, "unknownEvent")
	}

	// Verify the raw JSON is preserved
	if string(genericEvent.RawJSON) != string(rawData) {
		t.Errorf("RawJSON = %v, want %v", string(genericEvent.RawJSON), string(rawData))
	}

	// Verify parsed data extraction
	if genericEvent.ParsedData == nil {
		t.Error("ParsedData should not be nil")
	} else if val, ok := genericEvent.ParsedData["some"]; !ok || val.String != "data" {
		t.Errorf("ParsedData[\"some\"].String = %v, want \"data\"", val.String)
	}
}

func TestParseMediaUpdate(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	// Test successful parsing
	rawData := json.RawMessage(`{"currentTime":123.45,"paused":false}`)
	event := Event{
		Type: "mediaUpdate",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	mediaUpdate, ok := parsed.(*framework.MediaUpdateEvent)
	if !ok {
		t.Fatalf("Expected MediaUpdateEvent, got %T", parsed)
	}

	if mediaUpdate.CurrentTime != 123.45 {
		t.Errorf("CurrentTime = %v, want %v", mediaUpdate.CurrentTime, 123.45)
	}

	if mediaUpdate.Paused != false {
		t.Errorf("Paused = %v, want %v", mediaUpdate.Paused, false)
	}

	// Verify base event fields
	if mediaUpdate.Type() != "mediaUpdate" {
		t.Errorf("Type() = %v, want %v", mediaUpdate.Type(), "mediaUpdate")
	}

	if mediaUpdate.ChannelName != "test-channel" {
		t.Errorf("ChannelName = %v, want %v", mediaUpdate.ChannelName, "test-channel")
	}
}

func TestParseMediaUpdatePaused(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	// Test with paused = true
	rawData := json.RawMessage(`{"currentTime":0,"paused":true}`)
	event := Event{
		Type: "mediaUpdate",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	mediaUpdate, ok := parsed.(*framework.MediaUpdateEvent)
	if !ok {
		t.Fatalf("Expected MediaUpdateEvent, got %T", parsed)
	}

	if mediaUpdate.CurrentTime != 0 {
		t.Errorf("CurrentTime = %v, want %v", mediaUpdate.CurrentTime, 0)
	}

	if mediaUpdate.Paused != true {
		t.Errorf("Paused = %v, want %v", mediaUpdate.Paused, true)
	}
}

func TestParseMediaUpdateEdgeCases(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	tests := []struct {
		name        string
		data        json.RawMessage
		wantTime    float64
		wantPaused  bool
		expectError bool
	}{
		{
			name:       "negative current time",
			data:       json.RawMessage(`{"currentTime":-10.5,"paused":false}`),
			wantTime:   -10.5,
			wantPaused: false,
		},
		{
			name:       "very large current time",
			data:       json.RawMessage(`{"currentTime":999999.99,"paused":false}`),
			wantTime:   999999.99,
			wantPaused: false,
		},
		{
			name:       "integer current time",
			data:       json.RawMessage(`{"currentTime":100,"paused":false}`),
			wantTime:   100.0,
			wantPaused: false,
		},
		{
			name:        "missing currentTime",
			data:        json.RawMessage(`{"paused":false}`),
			expectError: false, // JSON unmarshaling will use zero value
			wantTime:    0,
			wantPaused:  false,
		},
		{
			name:        "missing paused",
			data:        json.RawMessage(`{"currentTime":50.5}`),
			expectError: false, // JSON unmarshaling will use zero value
			wantTime:    50.5,
			wantPaused:  false,
		},
		{
			name:        "empty object",
			data:        json.RawMessage(`{}`),
			expectError: false,
			wantTime:    0,
			wantPaused:  false,
		},
		{
			name:        "currentTime as string",
			data:        json.RawMessage(`{"currentTime":"123.45","paused":false}`),
			expectError: true,
		},
		{
			name:        "paused as string",
			data:        json.RawMessage(`{"currentTime":123.45,"paused":"false"}`),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Type: "mediaUpdate",
				Data: tt.data,
			}

			parsed, err := parser.ParseEvent(event)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseEvent failed: %v", err)
			}

			mediaUpdate, ok := parsed.(*framework.MediaUpdateEvent)
			if !ok {
				t.Fatalf("Expected MediaUpdateEvent, got %T", parsed)
			}

			if mediaUpdate.CurrentTime != tt.wantTime {
				t.Errorf("CurrentTime = %v, want %v", mediaUpdate.CurrentTime, tt.wantTime)
			}

			if mediaUpdate.Paused != tt.wantPaused {
				t.Errorf("Paused = %v, want %v", mediaUpdate.Paused, tt.wantPaused)
			}
		})
	}
}

func TestParseInvalidJSON(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	tests := []struct {
		name      string
		eventType string
		data      json.RawMessage
	}{
		{"invalid chat JSON", "chatMsg", json.RawMessage(`invalid json`)},
		{"invalid user join", "userJoin", json.RawMessage(`123`)},
		{"invalid user leave", "userLeave", json.RawMessage(`true`)},
		{"invalid video change", "changeMedia", json.RawMessage(`"not an object"`)},
		{"invalid media update", "mediaUpdate", json.RawMessage(`invalid json`)},
		{"media update not object", "mediaUpdate", json.RawMessage(`"string value"`)},
		{"media update array", "mediaUpdate", json.RawMessage(`[1, 2, 3]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Type: tt.eventType,
				Data: tt.data,
			}

			_, err := parser.ParseEvent(event)
			if err == nil {
				t.Error("Expected error for invalid data, got nil")
			}
		})
	}
}

func TestParseLogin(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	t.Run("successful login", func(t *testing.T) {
		rawData := json.RawMessage(`{
			"name": "testuser",
			"rank": 2,
			"success": true,
			"ip": "192.168.1.1",
			"aliases": ["oldname1", "oldname2"]
		}`)
		event := Event{
			Type: "login",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		loginEvent, ok := parsed.(*framework.LoginEvent)
		if !ok {
			t.Fatalf("Expected LoginEvent, got %T", parsed)
		}

		if loginEvent.Username != "testuser" {
			t.Errorf("Username = %v, want %v", loginEvent.Username, "testuser")
		}
		if loginEvent.UserRank != 2 {
			t.Errorf("UserRank = %v, want %v", loginEvent.UserRank, 2)
		}
		if !loginEvent.Success {
			t.Error("Success should be true")
		}
		if loginEvent.IPAddress != "192.168.1.1" {
			t.Errorf("IPAddress = %v, want %v", loginEvent.IPAddress, "192.168.1.1")
		}
		if len(loginEvent.Aliases) != 2 {
			t.Errorf("Aliases length = %v, want %v", len(loginEvent.Aliases), 2)
		}
	})

	t.Run("failed login", func(t *testing.T) {
		rawData := json.RawMessage(`{
			"name": "testuser",
			"rank": 0,
			"success": false,
			"reason": "Invalid password"
		}`)
		event := Event{
			Type: "login",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		loginEvent, ok := parsed.(*framework.LoginEvent)
		if !ok {
			t.Fatalf("Expected LoginEvent, got %T", parsed)
		}

		if loginEvent.Success {
			t.Error("Success should be false")
		}
		if loginEvent.FailReason != "Invalid password" {
			t.Errorf("FailReason = %v, want %v", loginEvent.FailReason, "Invalid password")
		}
	})
}

func TestParseAddUser(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"name": "newuser",
		"rank": 1,
		"addedBy": "admin",
		"timestamp": "2023-01-01T00:00:00Z",
		"registered": true,
		"email": "test@example.com"
	}`)
	event := Event{
		Type: "addUser",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	addUserEvent, ok := parsed.(*framework.AddUserEvent)
	if !ok {
		t.Fatalf("Expected AddUserEvent, got %T", parsed)
	}

	if addUserEvent.Username != "newuser" {
		t.Errorf("Username = %v, want %v", addUserEvent.Username, "newuser")
	}
	if addUserEvent.UserRank != 1 {
		t.Errorf("UserRank = %v, want %v", addUserEvent.UserRank, 1)
	}
	if addUserEvent.AddedBy != "admin" {
		t.Errorf("AddedBy = %v, want %v", addUserEvent.AddedBy, "admin")
	}
	if !addUserEvent.Registered {
		t.Error("Registered should be true")
	}
	if addUserEvent.Email != "test@example.com" {
		t.Errorf("Email = %v, want %v", addUserEvent.Email, "test@example.com")
	}
}

func TestParseChannelMeta(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"field": "title",
		"oldValue": "Old Title",
		"newValue": "New Title",
		"changedBy": "moderator",
		"type": "title"
	}`)
	event := Event{
		Type: "meta",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	metaEvent, ok := parsed.(*framework.ChannelMetaEvent)
	if !ok {
		t.Fatalf("Expected ChannelMetaEvent, got %T", parsed)
	}

	if metaEvent.Field != "title" {
		t.Errorf("Field = %v, want %v", metaEvent.Field, "title")
	}
	if metaEvent.OldValue != "Old Title" {
		t.Errorf("OldValue = %v, want %v", metaEvent.OldValue, "Old Title")
	}
	if metaEvent.NewValue != "New Title" {
		t.Errorf("NewValue = %v, want %v", metaEvent.NewValue, "New Title")
	}
	if metaEvent.ChangedBy != "moderator" {
		t.Errorf("ChangedBy = %v, want %v", metaEvent.ChangedBy, "moderator")
	}
	if metaEvent.ChangeType != "title" {
		t.Errorf("ChangeType = %v, want %v", metaEvent.ChangeType, "title")
	}
}

func TestParsePlaylist(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	t.Run("add items", func(t *testing.T) {
		rawData := json.RawMessage(`{
			"action": "add",
			"items": [{
				"uid": "item1",
				"media_id": "dQw4w9WgXcQ",
				"type": "youtube",
				"title": "Test Video",
				"duration": 212,
				"queueby": "user1",
				"temp": false
			}],
			"pos": 0,
			"user": "user1"
		}`)
		event := Event{
			Type: "playlist",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		playlistEvent, ok := parsed.(*framework.PlaylistEvent)
		if !ok {
			t.Fatalf("Expected PlaylistEvent, got %T", parsed)
		}

		if playlistEvent.Action != "add" {
			t.Errorf("Action = %v, want %v", playlistEvent.Action, "add")
		}
		if playlistEvent.User != "user1" {
			t.Errorf("User = %v, want %v", playlistEvent.User, "user1")
		}
		if playlistEvent.ItemCount != 1 {
			t.Errorf("ItemCount = %v, want %v", playlistEvent.ItemCount, 1)
		}
		if len(playlistEvent.Items) != 1 {
			t.Fatalf("Items length = %v, want %v", len(playlistEvent.Items), 1)
		}

		item := playlistEvent.Items[0]
		if item.MediaID != "dQw4w9WgXcQ" {
			t.Errorf("MediaID = %v, want %v", item.MediaID, "dQw4w9WgXcQ")
		}
		if item.MediaType != "youtube" {
			t.Errorf("MediaType = %v, want %v", item.MediaType, "youtube")
		}
		if item.Title != "Test Video" {
			t.Errorf("Title = %v, want %v", item.Title, "Test Video")
		}
		if item.Duration != 212 {
			t.Errorf("Duration = %v, want %v", item.Duration, 212)
		}
		if item.QueuedBy != "user1" {
			t.Errorf("QueuedBy = %v, want %v", item.QueuedBy, "user1")
		}
	})

	t.Run("move item", func(t *testing.T) {
		rawData := json.RawMessage(`{
			"action": "move",
			"from": 2,
			"to": 5,
			"user": "moderator"
		}`)
		event := Event{
			Type: "playlist",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		playlistEvent, ok := parsed.(*framework.PlaylistEvent)
		if !ok {
			t.Fatalf("Expected PlaylistEvent, got %T", parsed)
		}

		if playlistEvent.Action != "move" {
			t.Errorf("Action = %v, want %v", playlistEvent.Action, "move")
		}
		if playlistEvent.FromPos != 2 {
			t.Errorf("FromPos = %v, want %v", playlistEvent.FromPos, 2)
		}
		if playlistEvent.ToPos != 5 {
			t.Errorf("ToPos = %v, want %v", playlistEvent.ToPos, 5)
		}
		if playlistEvent.User != "moderator" {
			t.Errorf("User = %v, want %v", playlistEvent.User, "moderator")
		}
	})
}

func TestParseGenericEvent(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	t.Run("object data", func(t *testing.T) {
		rawData := json.RawMessage(`{
			"user": "testuser",
			"action": "custom",
			"value": 42,
			"nested": {"key": "value"}
		}`)
		event := Event{
			Type: "customEvent",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		genericEvent, ok := parsed.(*framework.GenericEvent)
		if !ok {
			t.Fatalf("Expected GenericEvent, got %T", parsed)
		}

		if genericEvent.UnknownType != "customEvent" {
			t.Errorf("UnknownType = %v, want %v", genericEvent.UnknownType, "customEvent")
		}

		// Check metadata extraction
		if genericEvent.Metadata["user"] != "testuser" {
			t.Errorf("Metadata[user] = %v, want %v", genericEvent.Metadata["user"], "testuser")
		}
		if genericEvent.Metadata["action"] != "custom" {
			t.Errorf("Metadata[action] = %v, want %v", genericEvent.Metadata["action"], "custom")
		}

		// Check parsed data
		if val, ok := genericEvent.ParsedData["value"]; !ok || val.Number != 42 {
			t.Errorf("ParsedData[value].Number = %v, want %v", val.Number, 42)
		}
	})

	t.Run("array data", func(t *testing.T) {
		rawData := json.RawMessage(`[1, 2, 3]`)
		event := Event{
			Type: "arrayEvent",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		genericEvent, ok := parsed.(*framework.GenericEvent)
		if !ok {
			t.Fatalf("Expected GenericEvent, got %T", parsed)
		}

		// ParsedData should be nil for non-object JSON
		if genericEvent.ParsedData != nil {
			t.Error("ParsedData should be nil for array data")
		}

		// Raw JSON should still be preserved
		if string(genericEvent.RawJSON) != string(rawData) {
			t.Errorf("RawJSON = %v, want %v", string(genericEvent.RawJSON), string(rawData))
		}
	})
}

func TestParseSetPlaylistMeta(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"count": 699,
		"rawTime": 2512852,
		"time": "698:00:52"
	}`)
	event := Event{
		Type: "setPlaylistMeta",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	metaEvent, ok := parsed.(*framework.SetPlaylistMetaEvent)
	if !ok {
		t.Fatalf("Expected SetPlaylistMetaEvent, got %T", parsed)
	}

	if metaEvent.Count != 699 {
		t.Errorf("Count = %v, want %v", metaEvent.Count, 699)
	}
	if metaEvent.RawTime != 2512852 {
		t.Errorf("RawTime = %v, want %v", metaEvent.RawTime, 2512852)
	}
	if metaEvent.FormattedTime != "698:00:52" {
		t.Errorf("FormattedTime = %v, want %v", metaEvent.FormattedTime, "698:00:52")
	}
}

func TestParseUserCount(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`2`)
	event := Event{
		Type: "usercount",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	countEvent, ok := parsed.(*framework.UserCountEvent)
	if !ok {
		t.Fatalf("Expected UserCountEvent, got %T", parsed)
	}

	if countEvent.Count != 2 {
		t.Errorf("Count = %v, want %v", countEvent.Count, 2)
	}
}

func TestParseSetUserRank(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"name": "***REMOVED***",
		"rank": 1
	}`)
	event := Event{
		Type: "setUserRank",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	rankEvent, ok := parsed.(*framework.SetUserRankEvent)
	if !ok {
		t.Fatalf("Expected SetUserRankEvent, got %T", parsed)
	}

	if rankEvent.Username != "***REMOVED***" {
		t.Errorf("Username = %v, want %v", rankEvent.Username, "***REMOVED***")
	}
	if rankEvent.Rank != 1 {
		t.Errorf("Rank = %v, want %v", rankEvent.Rank, 1)
	}
}

func TestParseSetPlaylistLocked(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	t.Run("locked true", func(t *testing.T) {
		rawData := json.RawMessage(`true`)
		event := Event{
			Type: "setPlaylistLocked",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		lockedEvent, ok := parsed.(*framework.SetPlaylistLockedEvent)
		if !ok {
			t.Fatalf("Expected SetPlaylistLockedEvent, got %T", parsed)
		}

		if !lockedEvent.Locked {
			t.Error("Locked should be true")
		}
	})

	t.Run("locked false", func(t *testing.T) {
		rawData := json.RawMessage(`false`)
		event := Event{
			Type: "setPlaylistLocked",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		lockedEvent, ok := parsed.(*framework.SetPlaylistLockedEvent)
		if !ok {
			t.Fatalf("Expected SetPlaylistLockedEvent, got %T", parsed)
		}

		if lockedEvent.Locked {
			t.Error("Locked should be false")
		}
	})
}

func TestParseSetPermissions(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"seeplaylist": -1,
		"playlistadd": 1.5,
		"chat": 0,
		"kick": 1.5,
		"ban": 2
	}`)
	event := Event{
		Type: "setPermissions",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	permsEvent, ok := parsed.(*framework.SetPermissionsEvent)
	if !ok {
		t.Fatalf("Expected SetPermissionsEvent, got %T", parsed)
	}

	if permsEvent.Permissions["seeplaylist"] != -1 {
		t.Errorf("Permissions[seeplaylist] = %v, want %v", permsEvent.Permissions["seeplaylist"], -1)
	}
	if permsEvent.Permissions["playlistadd"] != 1.5 {
		t.Errorf("Permissions[playlistadd] = %v, want %v", permsEvent.Permissions["playlistadd"], 1.5)
	}
	if permsEvent.Permissions["chat"] != 0 {
		t.Errorf("Permissions[chat] = %v, want %v", permsEvent.Permissions["chat"], 0)
	}
}

func TestParseSetMotd(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	t.Run("empty motd", func(t *testing.T) {
		rawData := json.RawMessage(`""`)
		event := Event{
			Type: "setMotd",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		motdEvent, ok := parsed.(*framework.SetMotdEvent)
		if !ok {
			t.Fatalf("Expected SetMotdEvent, got %T", parsed)
		}

		if motdEvent.Motd != "" {
			t.Errorf("Motd = %v, want empty string", motdEvent.Motd)
		}
	})

	t.Run("with motd", func(t *testing.T) {
		rawData := json.RawMessage(`"Welcome to the channel!"`)
		event := Event{
			Type: "setMotd",
			Data: rawData,
		}

		parsed, err := parser.ParseEvent(event)
		if err != nil {
			t.Fatalf("ParseEvent failed: %v", err)
		}

		motdEvent, ok := parsed.(*framework.SetMotdEvent)
		if !ok {
			t.Fatalf("Expected SetMotdEvent, got %T", parsed)
		}

		if motdEvent.Motd != "Welcome to the channel!" {
			t.Errorf("Motd = %v, want %v", motdEvent.Motd, "Welcome to the channel!")
		}
	})
}

func TestParseChannelOpts(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"allow_voteskip": true,
		"voteskip_ratio": 0.5,
		"afk_timeout": 600,
		"pagetitle": "RIFFTRAX_MST3K",
		"chat_antiflood": false,
		"chat_antiflood_params": {
			"burst": 4,
			"sustained": 1,
			"cooldown": 4
		},
		"show_public": false
	}`)
	event := Event{
		Type: "channelOpts",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	optsEvent, ok := parsed.(*framework.ChannelOptsEvent)
	if !ok {
		t.Fatalf("Expected ChannelOptsEvent, got %T", parsed)
	}

	if opt, ok := optsEvent.Options["allow_voteskip"]; !ok || opt.BoolValue != true || opt.ValueType != "bool" {
		t.Errorf("Options[allow_voteskip] = %v, want BoolValue=true, ValueType=bool", opt)
	}
	if opt, ok := optsEvent.Options["voteskip_ratio"]; !ok || opt.FloatValue != 0.5 || opt.ValueType != "float" {
		t.Errorf("Options[voteskip_ratio] = %v, want FloatValue=0.5, ValueType=float", opt)
	}
	if opt, ok := optsEvent.Options["pagetitle"]; !ok || opt.StringValue != "RIFFTRAX_MST3K" || opt.ValueType != "string" {
		t.Errorf("Options[pagetitle] = %v, want StringValue=RIFFTRAX_MST3K, ValueType=string", opt)
	}
}

func TestParseChannelCSSJS(t *testing.T) {
	parser := NewParser("test-channel", "test-room")

	rawData := json.RawMessage(`{
		"css": ".custom { color: red; }",
		"cssHash": "1B2M2Y8AsgTpgAmY7PhCfg==",
		"js": "console.log('hello');",
		"jsHash": "2C3N3Z9BthUqhBnZ8QiDgh=="
	}`)
	event := Event{
		Type: "channelCSSJS",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	cssjsEvent, ok := parsed.(*framework.ChannelCSSJSEvent)
	if !ok {
		t.Fatalf("Expected ChannelCSSJSEvent, got %T", parsed)
	}

	if cssjsEvent.CSS != ".custom { color: red; }" {
		t.Errorf("CSS = %v, want %v", cssjsEvent.CSS, ".custom { color: red; }")
	}
	if cssjsEvent.CSSHash != "1B2M2Y8AsgTpgAmY7PhCfg==" {
		t.Errorf("CSSHash = %v, want %v", cssjsEvent.CSSHash, "1B2M2Y8AsgTpgAmY7PhCfg==")
	}
	if cssjsEvent.JS != "console.log('hello');" {
		t.Errorf("JS = %v, want %v", cssjsEvent.JS, "console.log('hello');")
	}
	if cssjsEvent.JSHash != "2C3N3Z9BthUqhBnZ8QiDgh==" {
		t.Errorf("JSHash = %v, want %v", cssjsEvent.JSHash, "2C3N3Z9BthUqhBnZ8QiDgh==")
	}
}
