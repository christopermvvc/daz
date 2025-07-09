package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/hildolfr/daz/internal/config"
	"github.com/jackc/pgx/v5/stdlib"
)

// EventDiscoveryCommand implements event discovery functionality
type EventDiscoveryCommand struct {
	configPath string
	db         *sql.DB
}

// NewEventDiscoveryCommand creates a new event discovery command
func NewEventDiscoveryCommand(configPath string) *EventDiscoveryCommand {
	return &EventDiscoveryCommand{
		configPath: configPath,
	}
}

// Run executes the event discovery analysis
func (c *EventDiscoveryCommand) Run() error {
	// Load configuration
	cfg, err := config.LoadFromFile(c.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Connect to database
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.Core.Database.Host,
		cfg.Core.Database.Port,
		cfg.Core.Database.User,
		cfg.Core.Database.Password,
		cfg.Core.Database.Database)

	// Register pgx driver
	sql.Register("pgx", stdlib.GetDefaultDriver())

	c.db, err = sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer c.db.Close()

	fmt.Println("CyTube Event Discovery Analysis")
	fmt.Println("===============================")
	fmt.Println()

	// Analyze all events
	if err := c.analyzeAllEvents(); err != nil {
		return fmt.Errorf("analyzing events: %w", err)
	}

	// Analyze unknown events
	if err := c.analyzeUnknownEvents(); err != nil {
		return fmt.Errorf("analyzing unknown events: %w", err)
	}

	// Generate recommendations
	c.generateRecommendations()

	return nil
}

func (c *EventDiscoveryCommand) analyzeAllEvents() error {
	fmt.Println("All CyTube Events (Last 7 Days):")
	fmt.Println("--------------------------------")

	query := `
		SELECT event_type, COUNT(*) as count,
		       MIN(timestamp) as first_seen,
		       MAX(timestamp) as last_seen
		FROM daz_core_events
		WHERE source = 'cytube'
		  AND timestamp > datetime('now', '-7 days')
		GROUP BY event_type
		ORDER BY count DESC
	`

	rows, err := c.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Event Type\tCount\tFirst Seen\tLast Seen")
	fmt.Fprintln(w, "----------\t-----\t----------\t---------")

	for rows.Next() {
		var eventType string
		var count int
		var firstSeen, lastSeen time.Time

		if err := rows.Scan(&eventType, &count, &firstSeen, &lastSeen); err != nil {
			continue
		}

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\n",
			eventType, count,
			firstSeen.Format("2006-01-02 15:04"),
			lastSeen.Format("2006-01-02 15:04"))
	}
	w.Flush()
	fmt.Println()

	return nil
}

func (c *EventDiscoveryCommand) analyzeUnknownEvents() error {
	fmt.Println("Unknown/Generic Events with Sample Data:")
	fmt.Println("---------------------------------------")

	query := `
		SELECT event_type, raw_data, timestamp
		FROM daz_core_events
		WHERE source = 'cytube'
		  AND event_type = 'generic'
		  AND timestamp > datetime('now', '-7 days')
		ORDER BY timestamp DESC
		LIMIT 100
	`

	rows, err := c.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Collect unique unknown events
	unknownEvents := make(map[string]json.RawMessage)
	eventCounts := make(map[string]int)

	for rows.Next() {
		var eventType, rawData string
		var timestamp time.Time

		if err := rows.Scan(&eventType, &rawData, &timestamp); err != nil {
			continue
		}

		// Parse the raw data to extract the actual event type
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rawData), &data); err != nil {
			continue
		}

		// Look for unknown_type field
		if unknownType, ok := data["unknown_type"]; ok {
			var actualType string
			if err := json.Unmarshal(unknownType, &actualType); err == nil {
				eventCounts[actualType]++
				if _, exists := unknownEvents[actualType]; !exists {
					unknownEvents[actualType] = data["raw_json"]
				}
			}
		}
	}

	// Sort by count
	var sortedEvents []string
	for event := range eventCounts {
		sortedEvents = append(sortedEvents, event)
	}
	sort.Slice(sortedEvents, func(i, j int) bool {
		return eventCounts[sortedEvents[i]] > eventCounts[sortedEvents[j]]
	})

	// Display results
	for _, event := range sortedEvents {
		fmt.Printf("\nEvent: %s (Count: %d)\n", event, eventCounts[event])

		// Pretty print sample data
		if sample, ok := unknownEvents[event]; ok {
			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, sample, "", "  "); err == nil {
				fmt.Println("Sample Data:")
				fmt.Println(prettyJSON.String())
			}
		}
	}

	if len(unknownEvents) == 0 {
		fmt.Println("No unknown events found in the last 7 days.")
	}

	fmt.Println()
	return nil
}

func (c *EventDiscoveryCommand) generateRecommendations() {
	fmt.Println("Recommendations:")
	fmt.Println("---------------")
	fmt.Println("1. To discover all possible CyTube events:")
	fmt.Println("   - Monitor the channel during different activities")
	fmt.Println("   - Perform admin actions to trigger admin-only events")
	fmt.Println("   - Review CyTube source code at: https://github.com/calzoneman/sync")
	fmt.Println()
	fmt.Println("2. Events needing implementation:")
	fmt.Println("   - Check the Unknown/Generic events section above")
	fmt.Println("   - Add parser functions for frequently occurring unknown events")
	fmt.Println()
	fmt.Println("3. Enable discovery mode:")
	fmt.Println("   - Set LOG_LEVEL=debug in your configuration")
	fmt.Println("   - All events will be logged with full details")
}

// Add this to your main.go to enable the command
func runEventDiscovery(configPath string) {
	cmd := NewEventDiscoveryCommand(configPath)
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}
