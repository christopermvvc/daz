package greeter

import (
	"bufio"
	_ "embed"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// GreetingTemplate represents a single greeting template
type GreetingTemplate struct {
	Category string
	Template string
}

//go:embed greetings.txt
var embeddedGreetings string

// GreetingManager handles loading and selecting greetings
type GreetingManager struct {
	greetings map[string][]string
	location  *time.Location
}

// NewGreetingManager creates a new greeting manager with Melbourne timezone
func NewGreetingManager() (*GreetingManager, error) {
	location, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		return nil, fmt.Errorf("failed to load Melbourne timezone: %w", err)
	}

	gm := &GreetingManager{
		greetings: make(map[string][]string),
		location:  location,
	}

	// Load greetings from file
	if err := gm.loadGreetings(); err != nil {
		return nil, err
	}

	return gm, nil
}

// loadGreetings loads greetings from the embedded greetings.txt file
func (gm *GreetingManager) loadGreetings() error {
	// Parse the embedded greetings directly
	return gm.parseGreetingsString(embeddedGreetings)
}

// parseGreetingsString parses the greetings from a string
func (gm *GreetingManager) parseGreetingsString(content string) error {
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse category: greeting format
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			// Skip malformed lines
			continue
		}

		category := strings.TrimSpace(parts[0])
		greeting := strings.TrimSpace(parts[1])

		if category == "" || greeting == "" {
			continue
		}

		// Add greeting to category
		gm.greetings[category] = append(gm.greetings[category], greeting)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading greetings file: %w", err)
	}

	// Validate we have at least some greetings
	if len(gm.greetings) == 0 {
		return fmt.Errorf("no valid greetings found in file")
	}

	return nil
}


// GetGreeting returns an appropriate greeting for the user
func (gm *GreetingManager) GetGreeting(username string, isFirstTime bool) string {
	// Get current time in Melbourne
	now := time.Now().In(gm.location)
	hour := now.Hour()

	// Determine time of day category
	var timeCategory string
	switch {
	case hour >= 5 && hour < 12:
		timeCategory = "morning"
	case hour >= 12 && hour < 17:
		timeCategory = "afternoon"
	case hour >= 17 && hour < 22:
		timeCategory = "evening"
	default:
		timeCategory = "night"
	}

	// Build category preference order
	var categories []string
	if isFirstTime {
		// For first-time users, prefer first-time specific greetings
		categories = []string{
			"first-" + timeCategory, // e.g., "first-morning"
			"first",                 // general first-time greeting
			timeCategory,            // time-based greeting
			"general",               // fallback to general
		}
	} else {
		// For returning users, use time-based or general greetings
		categories = []string{
			timeCategory, // time-based greeting
			"general",    // fallback to general
		}
	}

	// Try each category in order
	for _, category := range categories {
		if greetings, exists := gm.greetings[category]; exists && len(greetings) > 0 {
			// Select a random greeting from the category
			index := rand.Intn(len(greetings))
			greeting := greetings[index]
			// Replace placeholder with username
			return strings.ReplaceAll(greeting, "<user>", username)
		}
	}

	// Ultimate fallback if no greetings are found
	return fmt.Sprintf("Hello %s!", username)
}

// GetTimeOfDay returns the current time of day category in Melbourne
func (gm *GreetingManager) GetTimeOfDay() string {
	now := time.Now().In(gm.location)
	hour := now.Hour()

	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 22:
		return "evening"
	default:
		return "night"
	}
}

// ReloadGreetings reloads greetings from the file
func (gm *GreetingManager) ReloadGreetings() error {
	// Clear existing greetings
	gm.greetings = make(map[string][]string)

	// Reload from file
	return gm.loadGreetings()
}
