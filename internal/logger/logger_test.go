package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name     string
		logLevel LogLevel
		testFunc func(string, string, ...interface{})
		expected bool
	}{
		{
			name:     "DEBUG level allows all",
			logLevel: DEBUG,
			testFunc: Debug,
			expected: true,
		},
		{
			name:     "INFO level blocks DEBUG",
			logLevel: INFO,
			testFunc: Debug,
			expected: false,
		},
		{
			name:     "INFO level allows INFO",
			logLevel: INFO,
			testFunc: Info,
			expected: true,
		},
		{
			name:     "WARN level blocks INFO",
			logLevel: WARN,
			testFunc: Info,
			expected: false,
		},
		{
			name:     "ERROR level allows ERROR",
			logLevel: ERROR,
			testFunc: Error,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(nil)

			// Set log level
			SetLevel(tt.logLevel)

			// Log a message
			tt.testFunc("TestPlugin", "test message")

			// Check if message was logged
			output := buf.String()
			hasOutput := len(output) > 0

			if hasOutput != tt.expected {
				t.Errorf("expected output: %v, got output: %v", tt.expected, hasOutput)
			}
		})
	}
}

func TestSetDebug(t *testing.T) {
	// Test enabling debug
	SetDebug(true)
	if currentLevel != DEBUG {
		t.Errorf("SetDebug(true) should set level to DEBUG, got %v", currentLevel)
	}

	// Test disabling debug
	SetDebug(false)
	if currentLevel != INFO {
		t.Errorf("SetDebug(false) should set level to INFO, got %v", currentLevel)
	}
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name     string
		level    LogLevel
		plugin   string
		message  string
		color    bool
		contains []string
	}{
		{
			name:     "DEBUG message with color",
			level:    DEBUG,
			plugin:   "TestPlugin",
			message:  "debug message",
			color:    true,
			contains: []string{"DEBUG", "TestPlugin", "debug message"},
		},
		{
			name:     "INFO message without color",
			level:    INFO,
			plugin:   "TestPlugin",
			message:  "info message",
			color:    false,
			contains: []string{"INFO", "TestPlugin", "info message"},
		},
		{
			name:     "ERROR message with color",
			level:    ERROR,
			plugin:   "TestPlugin",
			message:  "error message",
			color:    true,
			contains: []string{"ERROR", "TestPlugin", "error message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set color mode
			colorEnabled = tt.color

			// Format message
			formatted := formatMessage(tt.level, tt.plugin, tt.message)

			// Check that all expected strings are present
			for _, expected := range tt.contains {
				if !strings.Contains(formatted, expected) {
					t.Errorf("formatted message should contain %q, got: %s", expected, formatted)
				}
			}

			// Check color codes if enabled
			if tt.color {
				if !strings.Contains(formatted, "\033[") {
					t.Error("formatted message should contain ANSI color codes when color is enabled")
				}
			} else {
				if strings.Contains(formatted, "\033[") {
					t.Error("formatted message should not contain ANSI color codes when color is disabled")
				}
			}
		})
	}
}

func TestDisableColor(t *testing.T) {
	// Enable color first
	colorEnabled = true

	// Disable color
	DisableColor()

	if colorEnabled {
		t.Error("DisableColor() should set colorEnabled to false")
	}
}

func TestLogAliases(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	// Set to DEBUG to see all messages
	SetLevel(DEBUG)

	// Test all aliases
	Debugf("TestPlugin", "debug %s", "test")
	Infof("TestPlugin", "info %s", "test")
	Warnf("TestPlugin", "warn %s", "test")
	Errorf("TestPlugin", "error %s", "test")

	output := buf.String()

	// Check that all messages were logged
	expected := []string{"debug test", "info test", "warn test", "error test"}
	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("output should contain %q, got: %s", exp, output)
		}
	}
}
