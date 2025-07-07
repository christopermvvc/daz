package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/hildolfr/daz/internal/core"
	"github.com/hildolfr/daz/internal/plugins/filter"
	"github.com/hildolfr/daz/pkg/eventbus"
)

func main() {
	channel := flag.String("channel", "***REMOVED***", "Cytube channel to join")
	username := flag.String("username", "***REMOVED***", "Username for authentication")
	password := flag.String("password", "***REMOVED***", "Password for authentication")
	dbHost := flag.String("db-host", "localhost", "PostgreSQL host")
	dbPort := flag.Int("db-port", 5432, "PostgreSQL port")
	dbName := flag.String("db-name", "daz", "Database name")
	dbUser := flag.String("db-user", "***REMOVED***", "Database user")
	dbPass := flag.String("db-pass", "***REMOVED***", "Database password")
	flag.Parse()

	if *channel == "" {
		log.Fatal("Please provide a channel name with -channel flag")
	}

	fmt.Println("Daz - Modular Go Chat Bot for Cytube")
	fmt.Printf("Joining channel: %s\n", *channel)
	if *username != "" {
		fmt.Printf("Authenticating as: %s\n", *username)
	}
	fmt.Println("Using WebSocket transport with PostgreSQL persistence")

	// Create core plugin configuration
	config := &core.Config{
		Cytube: core.CytubeConfig{
			Channel:  *channel,
			Username: *username,
			Password: *password,
		},
		Database: core.DatabaseConfig{
			Host:     *dbHost,
			Port:     *dbPort,
			Database: *dbName,
			User:     *dbUser,
			Password: *dbPass,
		},
	}

	if err := run(config); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
}

func run(config *core.Config) error {
	// Create the real event bus with default configuration
	eventBusConfig := &eventbus.Config{
		BufferSizes: map[string]int{
			"cytube.event": 1000,
			"sql.":         100,
			"plugin.":      50,
		},
	}
	bus := eventbus.NewEventBus(eventBusConfig)

	// Start the event bus
	if err := bus.Start(); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}
	defer func() {
		if err := bus.Stop(); err != nil {
			log.Printf("Error stopping event bus: %v", err)
		}
	}()

	// Create and initialize the core plugin
	corePlugin := core.NewPlugin(config)

	log.Println("Initializing core plugin...")
	if err := corePlugin.Initialize(bus); err != nil {
		return fmt.Errorf("failed to initialize core plugin: %w", err)
	}

	// Create and initialize the filter plugin
	filterPlugin := filter.NewPlugin(nil)

	log.Println("Initializing filter plugin...")
	if err := filterPlugin.Initialize(bus); err != nil {
		return fmt.Errorf("failed to initialize filter plugin: %w", err)
	}

	// Start core plugin first
	log.Println("Starting core plugin...")
	if err := corePlugin.Start(); err != nil {
		return fmt.Errorf("failed to start core plugin: %w", err)
	}
	defer func() {
		if err := corePlugin.Stop(); err != nil {
			log.Printf("Error stopping core plugin: %v", err)
		}
	}()

	// Then start filter plugin
	log.Println("Starting filter plugin...")
	if err := filterPlugin.Start(); err != nil {
		return fmt.Errorf("failed to start filter plugin: %w", err)
	}
	defer func() {
		if err := filterPlugin.Stop(); err != nil {
			log.Printf("Error stopping filter plugin: %v", err)
		}
	}()

	log.Println("Bot is running! Press Ctrl+C to stop.")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)

	return nil
}
