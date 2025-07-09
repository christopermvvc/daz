package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	// Database connection flags
	dbHost := flag.String("db-host", "localhost", "PostgreSQL host")
	dbPort := flag.Int("db-port", 5432, "PostgreSQL port")
	dbName := flag.String("db-name", "daz", "Database name")
	dbUser := flag.String("db-user", "", "Database user")
	dbPass := flag.String("db-pass", "", "Database password")
	since := flag.Duration("since", 1*time.Hour, "Show messages from last N duration")
	flag.Parse()

	// Connect to database
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		*dbHost, *dbPort, *dbUser, *dbPass, *dbName)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Test connection
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Println("Connected to database successfully!")

	// Query recent chat messages
	cutoffTime := time.Now().Add(-*since)
	query := `
		SELECT 
			id,
			event_type,
			channel_name,
			timestamp,
			username,
			message,
			raw_data,
			created_at
		FROM daz_core_events
		WHERE event_type = 'chatMsg'
		AND timestamp > $1
		ORDER BY timestamp DESC
		LIMIT 20
	`

	rows, err := db.QueryContext(ctx, query, cutoffTime)
	if err != nil {
		log.Fatalf("Failed to query messages: %v", err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Println("\n=== Recent Chat Messages ===")
	fmt.Println("---------------------------")

	count := 0
	for rows.Next() {
		var (
			id          int64
			eventType   string
			channelName string
			timestamp   time.Time
			username    sql.NullString
			message     sql.NullString
			rawData     json.RawMessage
			createdAt   time.Time
		)

		if err := rows.Scan(&id, &eventType, &channelName, &timestamp, &username, &message, &rawData, &createdAt); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}

		count++
		fmt.Printf("\n[%s] %s", timestamp.Format("15:04:05"), username.String)
		if message.Valid {
			fmt.Printf(": %s", message.String)
		}
		fmt.Printf("\n  Channel: %s", channelName)
		fmt.Printf("\n  DB ID: %d", id)

		// Pretty print raw data
		type eventData struct {
			Meta struct {
				UID string `json:"uid"`
			} `json:"meta"`
		}
		var data eventData
		if err := json.Unmarshal(rawData, &data); err == nil {
			if data.Meta.UID != "" {
				fmt.Printf("\n  User ID: %s", data.Meta.UID)
			}
		}
		fmt.Println("\n---------------------------")
	}

	if count == 0 {
		fmt.Println("No chat messages found in the specified time range.")
		fmt.Printf("Try running the bot first: ./bin/daz -db-pass %s\n", *dbPass)
	} else {
		fmt.Printf("\nFound %d chat messages\n", count)
	}

	// Show latest events of all types
	fmt.Println("\n=== Latest Events (All Types) ===")
	query2 := `
		SELECT event_type, COUNT(*) as count
		FROM daz_core_events
		WHERE timestamp > $1
		GROUP BY event_type
		ORDER BY count DESC
	`

	rows2, err := db.QueryContext(ctx, query2, cutoffTime)
	if err != nil {
		log.Printf("Failed to query event counts: %v", err)
		return
	}
	defer func() { _ = rows2.Close() }()

	for rows2.Next() {
		var eventType string
		var count int
		if err := rows2.Scan(&eventType, &count); err != nil {
			continue
		}
		fmt.Printf("%s: %d events\n", eventType, count)
	}
}
