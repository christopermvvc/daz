# Daz Chatbot Architecture

## Overview

Daz is a modular Go chatbot system designed for Cytube channels. It features an event-driven plugin architecture with PostgreSQL persistence, built for reliability and extensibility.

## Core Architecture

### System Components

```
┌─────────────────────────────────────────────────┐
│              Core Plugin                        │
│    (Standalone - Not Plugin Managed)            │
│    • WebSocket connections to Cytube            │
│    • Multi-room support                         │
│    • Event parsing and broadcasting             │
└─────────────────────┬───────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────┐
│                  EventBus                       │
│         (Central Message Broker)                │
│    • Priority-based message queuing             │
│    • Request/Response patterns                  │
│    • Pattern-based subscriptions                │
└──┬────────┬────────┬────────┬────────┬─────────┘
   │        │        │        │        │
   ▼        ▼        ▼        ▼        ▼
┌─────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌────────┐
│ SQL │ │Event │ │Retry │ │User  │ │Media   │
│     │ │Filter│ │      │ │Track │ │Tracker │
└─────┘ └──────┘ └──────┘ └──────┘ └────────┘
   │                          │         │
   ▼                          ▼         ▼
┌─────────────┐         ┌──────────┐ ┌─────────┐
│  Analytics  │         │ Commands │ │ Others  │
└─────────────┘         └──────────┘ └─────────┘

Plugin Manager controls all plugins except Core
```

### Design Principles

1. **Event-Driven Communication**: All components communicate via EventBus using publish/subscribe patterns
2. **Plugin Modularity**: All functionality implemented as independent plugins
3. **Type Safety**: Minimal interface{} usage - only where required for database compatibility
4. **Selective Persistence**: Not all events are logged; SQL plugin applies configurable logging rules
5. **Multi-Room Support**: Single bot instance can connect to multiple Cytube channels
6. **Graceful Degradation**: System continues operating when individual components fail

## Component Details

### EventBus

The EventBus is the central nervous system of Daz, handling all inter-component communication.

**Features:**
- **Priority Queues**: Each event type has its own priority queue for message ordering
- **Pattern Subscriptions**: Support for wildcard patterns (e.g., `cytube.event.*`)
- **Request/Response**: Synchronous communication with correlation IDs and timeouts
- **Tag Filtering**: Events carry metadata tags for selective processing
- **Buffer Management**: Configurable buffer sizes per event type to prevent memory issues
- **Dropped Event Tracking**: Monitors and reports when events are dropped due to full queues

**Key Event Types:**
- `cytube.event.*` - All events from Cytube channels
- `sql.query` / `sql.exec` - Database operations
- `plugin.command.*` - Command execution events
- `plugin.request.*` / `plugin.response.*` - Inter-plugin communication
- `*.failed` - Failure events for retry mechanism

### Core Plugin

Manages WebSocket connections to Cytube channels using the Socket.IO v4 protocol. This plugin is NOT managed by the PluginManager and has its own initialization flow.

**Key Differences:**
- Initialized separately before PluginManager
- Has custom `Initialize()` method (not framework.Plugin interface)
- Starts room connections only after all other plugins are ready
- Directly integrated with main.go startup sequence

**Responsibilities:**
- Establish WebSocket connections to Cytube servers
- Authenticate with configured credentials
- Parse incoming Cytube events and convert to framework events
- Handle reconnection with exponential backoff
- Support multiple simultaneous channel connections
- Broadcast all Cytube events to EventBus

**Connection Flow:**
1. Discover server URL via `/socketconfig/{channel}.json`
2. Establish WebSocket connection
3. Complete Socket.IO handshake
4. Join channel
5. Wait 2 seconds (Cytube requirement)
6. Send login credentials
7. Begin processing events

### SQL Plugin

Provides database persistence and handles logging requests from other plugins.

**Features:**
- PostgreSQL connection pooling via pgx/v5
- Handles explicit log requests via `log.request` events
- Synchronous query/execute operations via `plugin.request`
- Batch operation support for performance
- Schema management per plugin

**Important**: The SQL plugin does NOT automatically subscribe to Cytube events. Other plugins must explicitly send `log.request` events to log data.

**Logger Rules Example:**
```json
{
  "event_pattern": "cytube.event.chatMsg",
  "enabled": true,
  "table": "daz_chat_log"
}
```
Note: These rules are stored but not actively used for subscriptions in the current implementation.

### EventFilter Plugin

Unified event filtering, routing, and command processing.

**Responsibilities:**
- Filter unwanted events (spam, rate limiting)
- Route events to appropriate handler plugins
- Detect and parse commands from chat messages
- Manage command registry and permissions
- Maintain command execution history

**Command Detection:**
- Configurable command prefix (default: `!`)
- Extracts command and arguments
- Routes to registered command handlers
- Enforces cooldowns and permissions

### Feature Plugins

#### UserTracker
- Tracks active user sessions per channel
- Records join/leave events
- Monitors user activity and inactivity
- Maintains historical user data

#### MediaTracker
- Records all media plays with timestamps
- Tracks queue changes
- Maintains media library cache
- Generates play statistics

#### Analytics
- Aggregates hourly and daily statistics
- Tracks user activity patterns
- Monitors chat volume and media plays
- Provides channel health metrics

#### Retry Plugin
- Persistent retry queue in PostgreSQL
- Exponential backoff with jitter
- Configurable retry policies per operation type
- Dead letter queue for permanent failures
- Priority-based retry scheduling

#### Command Plugins
- **about**: Display bot information
- **help**: Show available commands
- **uptime**: Report bot uptime
- **debug**: Show debug information (admin only)

## Data Flow

### Event Processing Pipeline

```
1. Cytube WebSocket → Core Plugin (parses events)
2. Core Plugin → EventBus.Broadcast("cytube.event.*", data)
3. EventBus → All subscribed plugins receive events
4. Plugins process events and may:
   - Send log.request → SQL Plugin for persistence
   - Emit failure events → Retry Plugin for retry handling
   - Send commands back → Core Plugin → Cytube
```

### Request/Response Flow

```
Plugin A → EventBus.Request() → Plugin B
   ↑                               ↓
   └──── EventBus.Response() ←─────┘
         (with correlation ID)
```

### Logging Flow

```
Plugin → EventBus.Broadcast("log.request", data) → SQL Plugin
                                                       ↓
                                                   Database
```

## Database Schema

### Core Tables

**Event Storage:**
- `daz_core_events` - All Cytube events (configurable logging)
- `daz_chat_log` - Chat messages
- `***REMOVED***_activity` - User join/leave events
- `daz_private_messages` - Private message storage

**System Tables:**
- `daz_retry_queue` - Persistent retry operations
- `daz_retry_history` - Retry attempt history
- `daz_plugin_stats` - Plugin performance metrics

### Plugin Tables

Each plugin manages its own schema with prefixed table names:
- `***REMOVED***tracker_*` - User tracking data
- `daz_mediatracker_*` - Media play history
- `daz_analytics_*` - Aggregated statistics
- `daz_eventfilter_*` - Filter rules and command registry

## Configuration

### Structure

```json
{
  "core": {
    "rooms": [{
      "channel": "channel_name",
      "username": "bot_username",
      "password": "bot_password",
      "enabled": true,
      "reconnect_attempts": 10,
      "cooldown_minutes": 2
    }],
    "database": {
      "host": "localhost",
      "port": 5432,
      "database": "daz",
      "user": "***REMOVED***",
      "password": "password"
    }
  },
  "event_bus": {
    "buffer_sizes": {
      "cytube.event": 5000,
      "sql.query": 100
    }
  },
  "plugins": {
    // Plugin-specific configurations
  }
}
```

### Key Configuration Options

- **Multi-room support**: Configure multiple channels in `core.rooms`
- **Buffer tuning**: Adjust `event_bus.buffer_sizes` for performance
- **Plugin control**: Enable/disable plugins via configuration
- **Retry policies**: Configure retry behavior per operation type
- **Logger rules**: Control which events are persisted

## Deployment

### Requirements

- Go 1.23+
- PostgreSQL 14+
- Linux/macOS/Windows
- Network access to Cytube servers

### Startup Sequence

1. **EventBus Creation**: Initialize with configured buffer sizes
2. **EventBus Start**: Begin message routing
3. **Core Plugin Creation**: Create with room configurations
4. **Core Plugin Initialize**: Connect to EventBus (not rooms yet)
5. **PluginManager Creation**: For managing all other plugins
6. **Plugin Registration**: Register all plugins with manager
7. **Plugin Initialization**: Initialize all plugins (respects dependencies)
8. **Core Plugin Start**: Start Core plugin
9. **Plugin Start**: Start all other plugins via PluginManager
10. **Room Connections**: Core plugin connects to Cytube rooms
11. **Health Service**: Register all components for monitoring

### Running Daz

```bash
# Build
make build

# Run with default config
./bin/daz

# Run with custom config
./bin/daz -config custom-config.json

# Run with health check server
./bin/daz -health-port 8080
```

### Deployment Options

1. **Direct Binary**: Run the compiled binary with systemd or supervisor
2. **Docker**: Use provided Dockerfile for containerized deployment
3. **Docker Compose**: Full stack with PostgreSQL included

### Health Monitoring

HTTP endpoints for health checks:
- `/health` - Overall system health
- `/health/live` - Liveness probe
- `/health/ready` - Readiness probe
- `/metrics` - Prometheus metrics

## Error Handling

### Connection Resilience

- **Exponential backoff**: 2s base delay, max 5 minutes
- **Jitter**: ±25% randomization to prevent thundering herd
- **Persistent state**: Retry timers survive restarts
- **Configurable attempts**: Default 10 retries per cycle

### Plugin Failures

- **Isolation**: Plugin failures don't affect other plugins
- **Retry mechanism**: Failed operations queued for retry
- **Graceful degradation**: Core functionality continues
- **Event dropping**: Full queues drop events rather than blocking

## Security Considerations

### Authentication
- Username/password authentication for Cytube
- No OAuth or advanced auth mechanisms
- Credentials stored in configuration file

### Database Security
- Parameterized queries prevent SQL injection
- Connection pooling with configurable limits
- Plugin isolation - no direct database access

### Best Practices
- Store `config.json` securely (not in version control)
- Use environment variables for sensitive data
- Implement rate limiting for commands
- Regular security updates for dependencies

## Performance

### Optimizations

- **Priority queues**: Important events processed first
- **Batch operations**: SQL plugin supports batch inserts
- **Selective logging**: Only configured events hit database
- **Connection pooling**: Efficient database connection reuse
- **Non-blocking sends**: Events dropped rather than blocking

### Metrics

Available Prometheus metrics:
- Event processing rates
- Dropped event counts
- Plugin execution times
- Database query performance
- WebSocket connection status

## Extending Daz

### Creating a Plugin

1. Implement the `framework.Plugin` interface:
```go
type Plugin interface {
    Init(eventBus framework.EventBus, config json.RawMessage) error
    Start(ctx context.Context) error
    Stop() error
    Status() framework.PluginStatus
    Config() interface{}
}
```

2. Register for events:
```go
eventBus.Subscribe("cytube.event.chatMsg", p.handleChatMessage)
```

3. Add configuration schema
4. Create database migrations if needed
5. Register in main.go

### Plugin Best Practices

- Use structured logging
- Handle context cancellation
- Implement graceful shutdown
- Emit failure events for retry
- Document configuration options
- Write comprehensive tests

## System Requirements

### Minimum
- 512MB RAM
- 1 CPU core
- 1GB disk space
- PostgreSQL 14+

### Recommended
- 2GB RAM
- 2 CPU cores
- 10GB disk space
- PostgreSQL 15+
- SSD storage

## Maintenance

### Database
- Regular VACUUM for performance
- Monitor table sizes
- Archive old event data
- Index optimization

### Monitoring
- Check health endpoints
- Monitor Prometheus metrics
- Review error logs
- Track retry queue size

### Updates
- Regular dependency updates
- Security patches
- Performance tuning
- Feature additions

---

This architecture document represents the Daz chatbot system as currently implemented. The modular design allows for easy extension while maintaining system stability and performance.