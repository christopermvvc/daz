# Daz - Modular Go Chat Bot for Cytube

A modular, resilient chat bot designed for Cytube channels, built in Go with a plugin-based architecture.

## Current Features

- WebSocket connection to Cytube channels with automatic reconnection
- Event parsing for chat messages, user joins/leaves, and video changes
- PostgreSQL persistence for all events with JSONB storage
- Plugin-based architecture with event bus for inter-plugin communication
- Command system with customizable prefix (default: "!")
- Built-in commands: !help, !about, !uptime
- User authentication and rank-based permissions
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

### Run with custom channel and database
```bash
./bin/daz -channel your-channel-name -username your-username -password your-password \
  -db-host localhost -db-name daz -db-user ***REMOVED*** -db-pass your-db-password
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
- `-db-host`: PostgreSQL host (default: "localhost")
- `-db-port`: PostgreSQL port (default: 5432)
- `-db-name`: Database name (default: "daz")
- `-db-user`: Database user (default: "***REMOVED***")
- `-db-pass`: Database password (default: "***REMOVED***")

## Architecture

The project follows a modular plugin-based architecture:

- **Core Plugin**: Manages Cytube connection and SQL module
- **Event Bus**: Asynchronous message passing via Go channels
- **Plugin System**: All functionality implemented as plugins
- **EventFilter Plugin**: Unified event filtering, routing, and command processing
- **Command Plugins**: Individual plugins for each command (help, about, uptime)
- **Persistent Storage**: PostgreSQL for data persistence

### Command System

Commands are processed through the following pipeline:
1. User types a command (e.g., `!help`) in Cytube chat
2. Core plugin receives the chat message and broadcasts it
3. EventFilter plugin detects the command prefix, validates permissions, and routes to appropriate plugin
4. Command plugin executes and sends response back through the event bus
5. Core plugin sends the response to Cytube

### Available Commands

- **!help** (aliases: !h, !commands) - Lists all available commands
- **!about** (aliases: !version, !info) - Shows bot information and version
- **!uptime** (aliases: !up) - Displays current bot uptime

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

## Database Setup

Before running Daz with database support, create the database and user:

```sql
-- Create database and user
CREATE DATABASE daz;
CREATE USER ***REMOVED*** WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE daz TO ***REMOVED***;
```

The bot will automatically create the required tables on first run.

## Project Status

✅ **Phase 1-6: Complete**
- Basic project structure with Go modules
- Cytube WebSocket connection with automatic reconnection
- Event parsing from raw Cytube messages
- PostgreSQL persistence with JSONB storage
- Plugin-based architecture with event bus
- Command system with router and multiple command plugins
- Filter plugin for event routing
- Full test coverage with race detection

### Recent Fixes (2025-07-07)
- Fixed MediaPayload duration parsing (handles both string and int values)
- Fixed command routing system (commands now properly flow through the pipeline)
- Increased EventBus buffer sizes to prevent event dropping
- All command plugins properly initialized and registered

## Dependencies

- Go 1.21+
- github.com/gorilla/websocket v1.5.1
- github.com/jackc/pgx/v5 v5.7.2 (PostgreSQL driver)
- PostgreSQL 12+