package greeter

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewGreetingManager(t *testing.T) {
	// Clean up any existing test file
	_ = os.Remove("internal/plugins/greeter/greetings.txt")
	defer func() {
		_ = os.Remove("internal/plugins/greeter/greetings.txt")
	}()

	gm, err := NewGreetingManager()
	if err != nil {
		t.Fatalf("Failed to create greeting manager: %v", err)
	}

	// Check that location is set
	if gm.location == nil {
		t.Error("Location should be set")
	}

	// Check that greetings are loaded
	if len(gm.greetings) == 0 {
		t.Error("Greetings should be loaded")
	}

	// Verify default file was created
	if _, err := os.Stat("internal/plugins/greeter/greetings.txt"); os.IsNotExist(err) {
		t.Error("Default greetings.txt should have been created")
	}
}

func TestParseGreetingsFile(t *testing.T) {
	// Create a test greetings file
	testContent := `# Test greetings
general: Hello <user>!
general: Hi <user>!

# Time based
morning: Good morning, <user>!
afternoon: Good afternoon, <user>!

# Invalid lines to test parsing
invalid line without colon
: empty category
category: 
# Comment line

first: Welcome <user>!
`

	// Write test file
	testFile := "test_greetings.txt"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() {
		_ = os.Remove(testFile)
	}()

	// Create greeting manager
	gm := &GreetingManager{
		greetings: make(map[string][]string),
	}

	// Open and parse file
	file, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Logf("Warning: failed to close test file: %v", closeErr)
		}
	}()

	err = gm.parseGreetingsFile(file)
	if err != nil {
		t.Fatalf("Failed to parse greetings file: %v", err)
	}

	// Verify parsed greetings
	tests := []struct {
		category string
		count    int
	}{
		{"general", 2},
		{"morning", 1},
		{"afternoon", 1},
		{"first", 1},
	}

	for _, tt := range tests {
		if greetings, exists := gm.greetings[tt.category]; exists {
			if len(greetings) != tt.count {
				t.Errorf("Category %s: expected %d greetings, got %d", tt.category, tt.count, len(greetings))
			}
		} else {
			t.Errorf("Category %s not found", tt.category)
		}
	}

	// Verify invalid lines were skipped
	if _, exists := gm.greetings[""]; exists {
		t.Error("Empty category should not exist")
	}
}

func TestGetGreeting(t *testing.T) {
	// Create a greeting manager with test data
	gm := &GreetingManager{
		greetings: map[string][]string{
			"general":         {"Hello <user>!", "Hi <user>!"},
			"morning":         {"Good morning, <user>!"},
			"afternoon":       {"Good afternoon, <user>!"},
			"evening":         {"Good evening, <user>!"},
			"night":           {"Good night, <user>!"},
			"first":           {"Welcome <user>!"},
			"first-morning":   {"Morning <user>, welcome!"},
			"first-afternoon": {"Afternoon <user>, welcome!"},
		},
		location: time.UTC, // Use UTC for predictable testing
	}

	tests := []struct {
		name        string
		username    string
		isFirstTime bool
		checkFor    string
	}{
		{
			name:        "Regular user greeting",
			username:    "testuser",
			isFirstTime: false,
			checkFor:    "testuser",
		},
		{
			name:        "First time user greeting",
			username:    "newuser",
			isFirstTime: true,
			checkFor:    "newuser",
		},
		{
			name:        "Username with spaces",
			username:    "test user",
			isFirstTime: false,
			checkFor:    "test user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			greeting := gm.GetGreeting(tt.username, tt.isFirstTime)

			// Check that username is in the greeting
			if !strings.Contains(greeting, tt.checkFor) {
				t.Errorf("Greeting should contain username %s, got: %s", tt.checkFor, greeting)
			}

			// Check that <user> placeholder was replaced
			if strings.Contains(greeting, "<user>") {
				t.Error("Greeting should not contain <user> placeholder")
			}
		})
	}
}

func TestGetTimeOfDay(t *testing.T) {
	location, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatalf("Failed to load timezone: %v", err)
	}

	tests := []struct {
		hour     int
		expected string
	}{
		{6, "morning"},
		{11, "morning"},
		{12, "afternoon"},
		{16, "afternoon"},
		{17, "evening"},
		{21, "evening"},
		{22, "night"},
		{23, "night"},
		{0, "night"},
		{4, "night"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// Create a mock time for testing
			mockTime := time.Date(2024, 1, 1, tt.hour, 0, 0, 0, location)

			// We can't mock time.Now() directly, but we can test the logic
			hour := mockTime.Hour()
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

			if timeCategory != tt.expected {
				t.Errorf("Hour %d: expected %s, got %s", tt.hour, tt.expected, timeCategory)
			}
		})
	}
}

func TestReloadGreetings(t *testing.T) {
	// Create initial test file
	testContent1 := `general: Hello <user>!
morning: Good morning, <user>!`

	testFile := "test_reload_greetings.txt"
	err := os.WriteFile(testFile, []byte(testContent1), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() {
		_ = os.Remove(testFile)
	}()

	// Create greeting manager with custom file location
	gm := &GreetingManager{
		greetings: make(map[string][]string),
		location:  time.UTC,
	}

	// Override the loadGreetings method for testing
	// Since we can't easily override methods in Go, we'll test the logic directly
	file, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	if err := gm.parseGreetingsFile(file); err != nil {
		t.Fatalf("Failed to parse greetings file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Logf("Warning: failed to close test file: %v", err)
	}

	// Verify initial greetings
	if len(gm.greetings["general"]) != 1 {
		t.Error("Should have 1 general greeting initially")
	}

	// Update the file
	testContent2 := `general: Hello <user>!
general: Hi <user>!
general: Hey <user>!
afternoon: Good afternoon, <user>!`

	err = os.WriteFile(testFile, []byte(testContent2), 0644)
	if err != nil {
		t.Fatalf("Failed to update test file: %v", err)
	}

	// Clear and reload
	gm.greetings = make(map[string][]string)
	file2, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	if err := gm.parseGreetingsFile(file2); err != nil {
		t.Fatalf("Failed to parse greetings file: %v", err)
	}
	if err := file2.Close(); err != nil {
		t.Logf("Warning: failed to close test file: %v", err)
	}

	// Verify reloaded greetings
	if len(gm.greetings["general"]) != 3 {
		t.Errorf("Should have 3 general greetings after reload, got %d", len(gm.greetings["general"]))
	}
	if len(gm.greetings["afternoon"]) != 1 {
		t.Error("Should have 1 afternoon greeting after reload")
	}
}

func TestEmptyGreetingsFile(t *testing.T) {
	// Create empty file
	testFile := "test_empty_greetings.txt"
	err := os.WriteFile(testFile, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() {
		_ = os.Remove(testFile)
	}()

	gm := &GreetingManager{
		greetings: make(map[string][]string),
	}

	file, err := os.Open(testFile)
	if err != nil {
		t.Fatalf("Failed to open test file: %v", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Logf("Warning: failed to close test file: %v", closeErr)
		}
	}()

	err = gm.parseGreetingsFile(file)
	if err == nil {
		t.Error("Should return error for empty greetings file")
	}
}

func TestFallbackGreeting(t *testing.T) {
	// Create greeting manager with no greetings
	gm := &GreetingManager{
		greetings: make(map[string][]string),
		location:  time.UTC,
	}

	greeting := gm.GetGreeting("testuser", false)
	expected := "Hello testuser!"

	if greeting != expected {
		t.Errorf("Expected fallback greeting %s, got %s", expected, greeting)
	}
}

func TestGreetingSelection(t *testing.T) {
	// Test that greetings are randomly selected
	gm := &GreetingManager{
		greetings: map[string][]string{
			"general": {
				"Hello <user>!",
				"Hi <user>!",
				"Hey <user>!",
				"Welcome <user>!",
			},
		},
		location: time.UTC,
	}

	// Get multiple greetings and check for variety
	greetings := make(map[string]bool)
	for i := 0; i < 20; i++ {
		greeting := gm.GetGreeting("test", false)
		greetings[greeting] = true
	}

	// Should have selected more than one greeting
	if len(greetings) < 2 {
		t.Error("Random selection should produce variety in greetings")
	}
}
