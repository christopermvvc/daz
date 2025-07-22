package greeter

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
)

// GreetingTemplate represents a single greeting template
type GreetingTemplate struct {
	Category string
	Template string
}

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

// loadGreetings loads greetings from the greetings.txt file
func (gm *GreetingManager) loadGreetings() error {
	// Try to load from greetings.txt in the plugin directory
	file, err := os.Open("internal/plugins/greeter/greetings.txt")
	if err != nil {
		// If file doesn't exist, create with default greetings
		if os.IsNotExist(err) {
			return gm.createDefaultGreetingsFile()
		}
		return fmt.Errorf("failed to open greetings file: %w", err)
	}
	defer file.Close()

	return gm.parseGreetingsFile(file)
}

// parseGreetingsFile parses the greetings file format
func (gm *GreetingManager) parseGreetingsFile(file *os.File) error {
	scanner := bufio.NewScanner(file)
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

// createDefaultGreetingsFile creates a default greetings.txt file
func (gm *GreetingManager) createDefaultGreetingsFile() error {
	defaultGreetings := `# Greetings configuration file
# Format: category: greeting template
# Available placeholders: <user>
# Categories: morning, afternoon, evening, night, first, general, first-morning, first-afternoon, first-evening, first-night

# General greetings (used as fallback)
general: Hello <user>!
general: Hey there, <user>!
general: Welcome, <user>!
general: Hi <user>!

# Time-based greetings
morning: Good morning, <user>!
morning: Morning <user>! Hope you're having a great start to your day!
morning: Rise and shine, <user>!

afternoon: Good afternoon, <user>!
afternoon: Hey <user>, hope your afternoon is going well!
afternoon: Afternoon, <user>!

evening: Good evening, <user>!
evening: Evening <user>! Hope you had a good day!
evening: Hey <user>, nice to see you this evening!

night: Good night, <user>!
night: Hey <user>, burning the midnight oil?
night: Late night greetings, <user>!

# First-time user greetings
first: Welcome <user>! Great to have you here!
first: Hello <user>, welcome to the channel!
first: Hey <user>! Nice to meet you!

# First-time user time-based greetings
first-morning: Good morning <user>, and welcome to the channel!
first-morning: Morning <user>! Welcome aboard!

first-afternoon: Good afternoon <user>, welcome to our community!
first-afternoon: Hey <user>, welcome! Hope you're having a great afternoon!

first-evening: Good evening <user>, and welcome!
first-evening: Evening <user>! Welcome to the channel!

first-night: Welcome <user>! Great to have you joining us tonight!
first-night: Hey <user>, welcome to the late night crew!
`

	// Ensure directory exists
	if err := os.MkdirAll("internal/plugins/greeter", 0755); err != nil {
		return fmt.Errorf("failed to create greeter directory: %w", err)
	}

	// Write default greetings file
	if err := os.WriteFile("internal/plugins/greeter/greetings.txt", []byte(defaultGreetings), 0644); err != nil {
		return fmt.Errorf("failed to write default greetings file: %w", err)
	}

	// Parse the default greetings
	file, err := os.Open("internal/plugins/greeter/greetings.txt")
	if err != nil {
		return fmt.Errorf("failed to open newly created greetings file: %w", err)
	}
	defer file.Close()

	return gm.parseGreetingsFile(file)
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
			greeting := greetings[rand.Intn(len(greetings))]
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
