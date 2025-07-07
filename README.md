# Daz - Modular Go Chat Bot for Cytube

A modular, resilient chat bot designed for Cytube channels, built in Go with a plugin-based architecture.

## Current Features

- WebSocket connection to Cytube channels
- Event parsing for chat messages, user joins/leaves, and video changes
- Console logging of all events
- Automatic reconnection with configurable retry logic
- User authentication support
- Graceful shutdown handling

## Usage

### Build the project
```bash
make build
```

### Run with default settings (***REMOVED*** channel)
```bash
./bin/daz
```

### Run with custom channel
```bash
./bin/daz -channel your-channel-name -username your-username -password your-password
```

### Using Make
```bash
make run CHANNEL=your-channel-name
```

## Configuration

The bot supports the following command-line flags:

- `-channel`: Cytube channel to join (default: "***REMOVED***")
- `-username`: Username for authentication (default: "***REMOVED***")
- `-password`: Password for authentication (default: "***REMOVED***")

## Architecture

The project follows a modular plugin-based architecture:

- **Core Plugin**: Manages Cytube connection and SQL module
- **Event Bus**: Asynchronous message passing via Go channels
- **Plugin System**: All functionality implemented as plugins
- **Persistent Storage**: PostgreSQL for data persistence (coming soon)

## Development

### Run tests
```bash
make test
```

### Format code
```bash
make fmt
```

### Run linters
```bash
make lint
```

## Project Status

✅ Phase 1: Minimal Core (Complete)
- Basic project structure with Go modules
- Cytube WebSocket connection
- Event parsing from raw Cytube messages
- Console logging of all events
- Basic connection retry logic

🚧 Phase 2: SQL Integration (Next)
- PostgreSQL connection management
- SQL module within core plugin
- Basic event logging to database
- Schema migration system

## Dependencies

- Go 1.21+
- github.com/gorilla/websocket v1.5.1
- github.com/lib/pq v1.10.9 (PostgreSQL driver)