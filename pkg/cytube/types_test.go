package cytube

import (
	"encoding/json"
	"testing"
)

func TestFlexibleDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		wantErr  bool
	}{
		{
			name:     "unmarshal int value",
			input:    `212`,
			expected: 212,
			wantErr:  false,
		},
		{
			name:     "unmarshal string value",
			input:    `"212"`,
			expected: 212,
			wantErr:  false,
		},
		{
			name:     "unmarshal zero as int",
			input:    `0`,
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "unmarshal zero as string",
			input:    `"0"`,
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "unmarshal large number as string",
			input:    `"3600"`,
			expected: 3600,
			wantErr:  false,
		},
		{
			name:     "unmarshal negative number as int",
			input:    `-1`,
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "unmarshal negative number as string",
			input:    `"-1"`,
			expected: -1,
			wantErr:  false,
		},
		{
			name:     "invalid string value",
			input:    `"not a number"`,
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "null value",
			input:    `null`,
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "boolean value",
			input:    `true`,
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "array value",
			input:    `[1, 2, 3]`,
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "object value",
			input:    `{"duration": 212}`,
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fd FlexibleDuration
			err := json.Unmarshal([]byte(tt.input), &fd)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && fd.Int() != tt.expected {
				t.Errorf("UnmarshalJSON() got = %v, want %v", fd.Int(), tt.expected)
			}
		})
	}
}

func TestFlexibleDuration_Int(t *testing.T) {
	tests := []struct {
		name     string
		duration FlexibleDuration
		expected int
	}{
		{
			name:     "positive value",
			duration: FlexibleDuration(100),
			expected: 100,
		},
		{
			name:     "zero value",
			duration: FlexibleDuration(0),
			expected: 0,
		},
		{
			name:     "negative value",
			duration: FlexibleDuration(-50),
			expected: -50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.duration.Int(); got != tt.expected {
				t.Errorf("Int() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMediaPayload_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedID       string
		expectedType     string
		expectedDuration int
		expectedTitle    string
		wantErr          bool
	}{
		{
			name:             "duration as int",
			input:            `{"id":"abc123","type":"youtube","duration":212,"title":"Test Video"}`,
			expectedID:       "abc123",
			expectedType:     "youtube",
			expectedDuration: 212,
			expectedTitle:    "Test Video",
			wantErr:          false,
		},
		{
			name:             "duration as string",
			input:            `{"id":"xyz789","type":"vimeo","duration":"3600","title":"Long Video"}`,
			expectedID:       "xyz789",
			expectedType:     "vimeo",
			expectedDuration: 3600,
			expectedTitle:    "Long Video",
			wantErr:          false,
		},
		{
			name:             "duration as zero string",
			input:            `{"id":"test","type":"custom","duration":"0","title":"No Duration"}`,
			expectedID:       "test",
			expectedType:     "custom",
			expectedDuration: 0,
			expectedTitle:    "No Duration",
			wantErr:          false,
		},
		{
			name:             "invalid duration string",
			input:            `{"id":"bad","type":"test","duration":"invalid","title":"Bad Duration"}`,
			expectedID:       "",
			expectedType:     "",
			expectedDuration: 0,
			expectedTitle:    "",
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var media MediaPayload
			err := json.Unmarshal([]byte(tt.input), &media)

			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if media.ID != tt.expectedID {
					t.Errorf("ID = %v, want %v", media.ID, tt.expectedID)
				}
				if media.Type != tt.expectedType {
					t.Errorf("Type = %v, want %v", media.Type, tt.expectedType)
				}
				if media.Duration.Int() != tt.expectedDuration {
					t.Errorf("Duration = %v, want %v", media.Duration.Int(), tt.expectedDuration)
				}
				if media.Title != tt.expectedTitle {
					t.Errorf("Title = %v, want %v", media.Title, tt.expectedTitle)
				}
			}
		})
	}
}
