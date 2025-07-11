package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var (
	currentLevel = INFO
	colorEnabled = true
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// SetLevel sets the minimum log level
func SetLevel(level LogLevel) {
	currentLevel = level
}

// SetDebug enables or disables debug logging
func SetDebug(enabled bool) {
	if enabled {
		currentLevel = DEBUG
	} else {
		currentLevel = INFO
	}
}

// DisableColor disables color output (useful for file logging)
func DisableColor() {
	colorEnabled = false
}

// formatMessage formats a log message with level, timestamp, and color
func formatMessage(level LogLevel, plugin, message string) string {
	timestamp := time.Now().Format("15:04:05")

	var levelStr, color string
	switch level {
	case DEBUG:
		levelStr = "DEBUG"
		color = colorGray
	case INFO:
		levelStr = "INFO "
		color = colorGreen
	case WARN:
		levelStr = "WARN "
		color = colorYellow
	case ERROR:
		levelStr = "ERROR"
		color = colorRed
	}

	if !colorEnabled {
		return fmt.Sprintf("%s [%s] %s: %s", timestamp, levelStr, plugin, message)
	}

	// Color the level indicator
	levelColored := fmt.Sprintf("%s%s%s", color, levelStr, colorReset)

	// Make plugin name bold
	pluginColored := fmt.Sprintf("%s%s%s", colorBold, plugin, colorReset)

	return fmt.Sprintf("%s [%s] %s: %s", timestamp, levelColored, pluginColored, message)
}

// logf is the internal logging function
func logf(level LogLevel, plugin, format string, args ...interface{}) {
	if level < currentLevel {
		return
	}

	message := fmt.Sprintf(format, args...)
	formatted := formatMessage(level, plugin, message)

	if level == ERROR {
		log.Println(formatted)
		os.Stderr.Sync()
	} else {
		log.Println(formatted)
	}
}

// Debug logs a debug message
func Debug(plugin, format string, args ...interface{}) {
	logf(DEBUG, plugin, format, args...)
}

// Info logs an info message
func Info(plugin, format string, args ...interface{}) {
	logf(INFO, plugin, format, args...)
}

// Warn logs a warning message
func Warn(plugin, format string, args ...interface{}) {
	logf(WARN, plugin, format, args...)
}

// Error logs an error message
func Error(plugin, format string, args ...interface{}) {
	logf(ERROR, plugin, format, args...)
}

// Debugf is an alias for Debug for consistency
func Debugf(plugin, format string, args ...interface{}) {
	Debug(plugin, format, args...)
}

// Infof is an alias for Info for consistency
func Infof(plugin, format string, args ...interface{}) {
	Info(plugin, format, args...)
}

// Warnf is an alias for Warn for consistency
func Warnf(plugin, format string, args ...interface{}) {
	Warn(plugin, format, args...)
}

// Errorf is an alias for Error for consistency
func Errorf(plugin, format string, args ...interface{}) {
	Error(plugin, format, args...)
}

func init() {
	// Remove the default timestamp from log package since we add our own
	log.SetFlags(0)

	// Check if we should force color output
	if os.Getenv("FORCE_COLOR") == "1" {
		colorEnabled = true
	}
}
