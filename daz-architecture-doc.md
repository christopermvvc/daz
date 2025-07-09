# Daz: Modular Go Chat Bot Architecture

## Project Overview

**Daz** is a modular, resilient chat bot designed for Cytube channels, built in Go with a focus on plugin-based architecture and persistent data storage. The system prioritizes reliability, modularity, and data integrity through PostgreSQL persistence.

## Core Architecture Principles

- **EventBus-First Architecture**: EventBus is the primary communication backbone, starting before all plugins
- **Plugin-Based Modularity**: All functionality including SQL implemented as plugins
- **Event-Driven Communication**: Bidirectional message passing via enhanced EventBus with request/response patterns
- **Selective Persistence**: Plugins explicitly request data logging through SQL plugin
- **Tagged Data Flow**: All events carry metadata for routing, logging, and correlation
- **Graceful Degradation**: System continues operating with failed components when possible
- **Restart Resilience**: Minimal data loss on service restarts

## System Architecture

### Component Hierarchy

```
┌─────────────────────────────────────────────────┐
│                  Event Bus                      │
│         (Primary Communication System)          │
│    • Request/Response Infrastructure            │
│    • Event Tagging & Routing                    │
│    • Bidirectional Communication                │
└────────────┬────────────┬────────────┬──────────┘
             │            │            │
     ┌───────▼──────┐ ┌───▼────┐ ┌────▼─────┐
     │ SQL Plugin   │ │  Core  │ │  Event   │
     │              │ │ Plugin │ │  Filter  │
     │ • Database   │ │        │ │          │
     │ • Logger     │ │ Cytube │ │ Command  │
     │   Middleware │ │  Conn  │ │  Router  │
     └──────────────┘ └────────┘ └──────────┘
              │                          │
              └────────┬─────────────────┘
                      │
               ┌──────▼──────┐
               │   Feature   │
               │   Plugins   │
               └─────────────┘
```

### Startup Sequence

1. **EventBus Initialization**: Primary communication system with request/response support
2. **SQL Plugin**: Standalone database service with logger middleware
3. **Core Plugin**: Cytube connection only (no embedded SQL)
4. **EventFilter Plugin**: Event filtering, routing, and command processing
5. **Feature Plugins**: All other plugins (custom commands, analytics, etc.)

### Data Flow

```
Cytube API → Core Plugin → Event Bus (Tagged Events) → SQL Plugin (Logger Middleware)
                                 ↓                            ↓
                           EventFilter                  Selective Logging
                                 ↓
                          Target Plugins
```

## Core Components

### 1. Plugin Loader Framework

- **Responsibility**: Discover and initialize plugins
- **Implementation**: Compiled plugins with self-registration
- **Dependencies**: Manages plugin lifecycle and startup sequence

### 2. Core Plugin

**Cytube Connection Module**:
- WebSocket connection to Cytube using Socket.IO v4 protocol over WebSocket
- Dynamic server discovery via `/socketconfig/{channel}.json` API
- Event parsing from raw Cytube data into structured events
- Bidirectional communication (receive events, send responses)
- Connection retry logic with exponential backoff and persistent timing
- Required authentication flow: join channel → wait 2 seconds → send login
- Thread-safe message writing with mutex protection
- Automatic ping/pong handling for connection keepalive

**Note**: SQL functionality has been moved to a standalone SQL Plugin (see section 3.5)

### 3. Event Bus System

**Implementation**: Enhanced asynchronous message passing with bidirectional support

**Core Features**:
- Configurable channel buffers with pattern-based sizing
- Request/Response infrastructure with correlation IDs
- Event tagging and metadata for routing decisions
- Priority-based message handling
- Non-blocking sends with dropped event metrics

**Message Types**:
- `cytube.event.*` - Tagged events from Cytube channel
- `sql.query` - Database query requests with responses
- `sql.exec` - Database execute requests with responses
- `log.request` - Explicit logging requests to SQL plugin
- `log.batch` - Batch logging for performance
- `plugin.request.*` - Inter-plugin communication with reply-to
- `plugin.response.*` - Response messages with correlation

**Communication Patterns**:
- **Tagged Broadcast**: Events carry metadata for selective processing
- **Request/Response**: Full bidirectional communication with timeouts
- **Direct Messaging**: Targeted plugin-to-plugin communication
- **Logger Middleware**: SQL plugin selectively logs based on rules

### 4. SQL Plugin

- **Purpose**: Standalone database service with selective logging
- **Architecture**: Completely decoupled from Core plugin
- **Functionality**:
  - PostgreSQL connection pooling and management
  - Query/Execute request handling via EventBus
  - Logger middleware for selective event logging
  - Configuration-driven logging rules
  - Batch logging support for performance
- **Logger Middleware**:
  - Pattern-based event subscriptions
  - Transform functions for custom logging
  - Dynamic rule configuration
  - Selective field extraction
- **Database Operations**:
  - Synchronous queries with correlation
  - Transaction support
  - Schema management per plugin
  - Connection health monitoring

### 5. EventFilter Plugin

- **Purpose**: Unified event filtering, routing, and command processing
- **Functionality**: 
  - Filters unwanted events (spam, rate limiting)
  - Routes events to appropriate handler plugins
  - Detects and processes commands from chat messages
  - Manages command registry and permissions
- **Criticality**: System pauses event processing if EventFilter fails
- **Dependencies**: Requires core plugin operational
- **Database Tables**:
  - `daz_eventfilter_rules`: Filter patterns and routing rules
  - `daz_eventfilter_commands`: Command registry and aliases
  - `daz_eventfilter_history`: Command execution audit log

## Error Handling & Resilience

### Connection Retry Strategy

- **Attempts**: 10 retries per cycle
- **Cooldown**: 30 minutes between cycles
- **Persistence**: Retry timers stored in SQL across restarts
- **Behavior**: Infinite retry cycles with graceful backoff

### Plugin Failure Handling

- **Retry Policy**: 10 attempts with graceful cooldown
- **Degradation**: Failed plugins operate in degraded mode after retries
- **Core Dependency**: All plugins shut down gracefully if core fails
- **Recovery**: Core restarts other plugins when restored

### Event Processing

- **Non-blocking**: Drop events for slow plugins rather than blocking
- **No Replay**: Simple event loss rather than complex buffering
- **EventFilter Failure**: Pause command processing but continue event flow
- **SQL Plugin Failure**: Events continue flowing, logging degrades gracefully
- **Selective Logging**: Only configured events are logged to database

## Configuration Management

### JSON Configuration Structure

```json
{
  "core": {
    "cytube": {
      "channel": "your-channel-name",
      "reconnect_attempts": 10,
      "cooldown_minutes": 30
    }
  },
  "event_bus": {
    "buffer_sizes": {
      "cytube.event": 1000,
      "sql.query": 100,
      "sql.exec": 100,
      "log.request": 500,
      "plugin.request": 50
    },
    "request_timeout": "30s",
    "enable_metrics": true
  },
  "plugins": {
    "sql": {
      "enabled": true,
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "daz",
        "user": "***REMOVED***",
        "password": "secure_password",
        "max_connections": 25,
        "connection_timeout": "10s"
      },
      "logger_rules": [
        {
          "event_pattern": "cytube.event.chatMsg",
          "enabled": true,
          "table": "daz_chat_log",
          "fields": ["username", "message", "timestamp"]
        },
        {
          "event_pattern": "cytube.event.user*",
          "enabled": true,
          "table": "***REMOVED***_activity"
        }
      ]
    },
    "filter": {
      "enabled": true,
      "command_prefix": "!"
    },
    "command": {
      "enabled": true,
      "admin_users": ["admin1", "admin2"]
    }
  }
}
```

### Configuration Principles

- **Static Configuration**: No runtime changes supported
- **Restart Required**: All configuration changes require full restart
- **Per-Plugin Sections**: Organized by plugin namespace
- **Type-Specific Buffers**: Configurable buffer sizes by message type

## Plugin Development Framework

### Standard Plugin Interface

```go
type Plugin interface {
    // Lifecycle management
    Init(config json.RawMessage, bus EventBus) error
    Start() error
    Stop() error
    
    // Event handling
    HandleEvent(event Event) error
    
    // Health monitoring
    Status() PluginStatus
}

type EventBus interface {
    // Core broadcasting with tagged events
    Broadcast(eventType string, data *EventData) error
    
    // Direct plugin communication
    Send(target string, eventType string, data *EventData) error
    
    // Request/Response pattern
    Request(ctx context.Context, target string, eventType string, data *EventData) (*EventData, error)
    
    // Event subscription
    Subscribe(eventType string, handler EventHandler) error
    
    // Plugin lifecycle
    RegisterPlugin(name string, plugin Plugin) error
    UnregisterPlugin(name string) error
}

// Enhanced event data with metadata
type EventData struct {
    // Event metadata
    CorrelationID string
    Source        string
    Target        string
    Timestamp     time.Time
    Priority      int
    
    // Logging metadata
    Loggable      bool
    LogLevel      string
    Tags          []string
    
    // Event payload
    Type          string
    Data          interface{}
    
    // Request/Response
    ReplyTo       string
    Timeout       time.Duration
}
```

### Database Schema Management

**Per-Plugin Ownership**:
- Each plugin manages its own tables
- Standardized migration patterns provided
- Naming conventions to prevent conflicts
- SQL module validates all operations

**Migration Pattern**:
```sql
-- Plugin table naming: daz_{plugin_name}_{table_name}
CREATE TABLE IF NOT EXISTS daz_command_handlers (
    id SERIAL PRIMARY KEY,
    command VARCHAR(255) NOT NULL UNIQUE,
    plugin_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
```

## Event Types and Data Structures

### Cytube Events

Based on Cytube's WebSocket API which provides events like changeMedia, queue, chatMsg, addUser, and userLeave:

```go
type CytubeEvent struct {
    Type        string                 `json:"type"`
    Timestamp   time.Time             `json:"timestamp"`
    ChannelName string                `json:"channel"`
    RawData     json.RawMessage       `json:"raw_data"`
    Metadata    map[string]interface{} `json:"metadata"`
}

// Specific event types
type ChatMessageEvent struct {
    CytubeEvent
    Username string `json:"username"`
    Message  string `json:"message"`
    UserRank int    `json:"user_rank"`
    UserID   string `json:"user_id"`
}

type UserJoinEvent struct {
    CytubeEvent
    Username string `json:"username"`
    UserRank int    `json:"user_rank"`
}

type VideoChangeEvent struct {
    CytubeEvent
    VideoID   string `json:"video_id"`
    VideoType string `json:"video_type"` // youtube, etc.
    Duration  int    `json:"duration"`
    Title     string `json:"title"`
}

type UserLeaveEvent struct {
    CytubeEvent
    Username string `json:"username"`
}
```

### SQL Plugin Communication

```go
// Database operations
type SQLRequest struct {
    ID        string          `json:"id"`
    Query     string          `json:"query"`
    Params    []interface{}   `json:"params"`
    Timeout   time.Duration   `json:"timeout"`
    RequestBy string          `json:"request_by"` // Plugin name
}

type SQLResponse struct {
    ID        string          `json:"id"`
    Success   bool            `json:"success"`
    Error     error           `json:"error,omitempty"`
    Rows      []byte          `json:"rows,omitempty"`    // Raw result data
    RowCount  int64           `json:"row_count,omitempty"`
}

// Logger middleware operations
type LogRequest struct {
    Event      *EventData               `json:"event"`
    Table      string                   `json:"table,omitempty"`
    Fields     map[string]interface{}   `json:"fields,omitempty"`
    Transform  bool                     `json:"transform"`
}

type LogBatchRequest struct {
    Events     []*EventData             `json:"events"`
    Table      string                   `json:"table"`
    BatchSize  int                      `json:"batch_size"`
}

type LoggerRule struct {
    EventPattern string                  `json:"event_pattern"`
    Enabled      bool                    `json:"enabled"`
    Table        string                  `json:"table"`
    Fields       []string                `json:"fields"`
}
```

### Plugin Communication

```go
type PluginRequest struct {
    ID         string          `json:"id"`
    From       string          `json:"from"`
    To         string          `json:"to"`
    Type       string          `json:"type"`
    Data       interface{}     `json:"data"`
    ReplyTo    string          `json:"reply_to,omitempty"`
}

type PluginResponse struct {
    ID         string          `json:"id"`
    From       string          `json:"from"`
    Success    bool            `json:"success"`
    Data       interface{}     `json:"data,omitempty"`
    Error      string          `json:"error,omitempty"`
}
```

## Implementation Roadmap

### Phase 1: Minimal Core (Week 1) ✅ COMPLETE
- [x] Basic project structure with Go modules
- [x] Cytube WebSocket connection using Socket.IO v4 protocol over WebSocket
- [x] Event parsing from raw Cytube messages (supports both array and object formats)
- [x] Console logging of all events with formatted output
- [x] Basic connection retry logic with exponential backoff

**Implementation Notes:**
- Successfully implemented WebSocket transport with gorilla/websocket
- Cytube requires Socket.IO v4 protocol with specific handshake sequence
- Dynamic server discovery via `/socketconfig/{channel}.json` API
- Authentication requires 2-second delay between channel join and login
- All interface{} usage replaced with concrete types for type safety

### Phase 2: SQL Plugin Architecture (Week 2) 🔄 REDESIGNED
- [x] PostgreSQL connection management with pgx driver
- [ ] SQL plugin as standalone component (no longer embedded in Core)
- [ ] Logger middleware pattern for selective event logging
- [ ] Configuration-driven logging rules
- [ ] Batch logging support for performance
- [x] Schema migration system with versioned migrations
- [x] Raw SQL query handling with parameterized queries

**Architectural Changes:**
- SQL functionality extracted from Core plugin into standalone SQL plugin
- EventBus-based communication replaces direct method calls
- Selective logging based on configuration rules
- Logger middleware subscribes to specific event patterns
- Plugins explicitly request logging via log.request events
- Bidirectional communication for all SQL operations

### Phase 3: Enhanced Event Bus (Week 3) 🔄 ENHANCED
- [x] Go channel-based message passing
- [x] Message type routing (dot notation)
- [x] Configurable buffer sizes
- [x] Non-blocking event distribution
- [x] Plugin registration system
- [ ] Request/Response infrastructure with correlation IDs
- [ ] Event tagging and metadata system
- [ ] Priority-based message handling
- [ ] Bidirectional communication patterns
- [ ] Response routing and timeout handling

**Enhancement Notes:**
- EventBus now starts first before all plugins
- Added full request/response pattern support
- Event metadata includes source, target, correlation ID, tags
- Logger-specific routing based on event tags
- Support for synchronous and asynchronous operations
- Enhanced metrics for monitoring dropped events

### Phase 4: Filter Plugin (Week 4) ✅ COMPLETE
- [x] Event analysis and routing logic
- [x] Direct message forwarding
- [x] Command detection and parsing
- [x] Error handling for routing failures
- [x] Configuration-based routing rules

**Implementation Notes:**
- Merged filter and commandrouter plugins into unified EventFilter plugin
- Event routing with wildcard pattern matching (e.g., `cytube.event.*`)
- Command detection with configurable prefix (default: "!")
- Command registry with permissions and alias support
- Error handling with logging for failed routes
- Database-backed configuration with `daz_eventfilter_*` tables

### Phase 5: Plugin Framework (Week 5-6) ⚠️ MOSTLY COMPLETE
- [x] Standardized plugin interface
- [x] Plugin lifecycle management
- [x] Inter-plugin communication
- [~] Health monitoring system (partial)
- [x] Example plugins (commands, logging)

**Implementation Notes:**
- Standard Plugin interface with Init, Start, Stop, HandleEvent, Status methods
- Proper lifecycle management with graceful shutdown
- Inter-plugin messaging via EventBus Send() and Broadcast()
- Multiple working plugins: about, help, uptime, usertracker, mediatracker, analytics
- Health monitoring framework exists but needs metrics collection implementation

### Phase 6: Production Hardening (Week 7-8) ⚠️ IN PROGRESS
- [~] Comprehensive error handling (improved)
- [ ] Performance optimization
- [~] Monitoring and metrics (basic metrics added)
- [~] Documentation and tests (good coverage)
- [x] Deployment scripts

**Implementation Notes:**
- Fixed ignored errors in WebSocket disconnect handling
- Added dropped event tracking and metrics to EventBus
- Test coverage: Most packages >50%, core components >70%
- Created deployment options:
  - Docker with docker-compose.yml
  - Systemd service with security hardening
  - Unified deploy.sh script for both methods
- Basic metrics: Dropped event counts available via GetDroppedEventCounts()

## Technical Implementation Details

### WebSocket Connection Protocol

Cytube uses Socket.IO v4 protocol over WebSocket transport. The connection flow requires specific packet types and handshake sequences:

**Connection Flow:**
1. **Server Discovery**: GET `https://cytu.be/socketconfig/{channel}.json` to obtain WebSocket URL
2. **WebSocket Connection**: Connect to discovered server with standard WebSocket headers
3. **Engine.IO Handshake**: Receive packet type `0` with session parameters
4. **Socket.IO Connect**: Send packet `40` to establish Socket.IO session
5. **Channel Join**: Send `42["joinChannel",{"name":"channel_name"}]`
6. **Authentication Delay**: Wait 2 seconds (Cytube protocol requirement)
7. **Login**: Send `42["login",{"name":"username","pw":"password"}]`
8. **Event Reception**: Process incoming `42[event_name, data]` packets

**Packet Types:**
- `0`: Engine.IO open/handshake
- `2`: Engine.IO ping (client sends)
- `3`: Engine.IO pong (server responds)
- `40`: Socket.IO connect
- `42`: Socket.IO event (most common - carries actual data)

### Concrete Types Implementation

To ensure type safety and avoid interface{} usage, the following concrete types were created:

```go
// Connection and authentication types
type ChannelJoinData struct {
    Name string `json:"name"`
}

type LoginData struct {
    Name     string `json:"name"`
    Password string `json:"pw"`
}

// Event payload types
type ChatMessagePayload struct {
    Username string `json:"username"`
    Msg      string `json:"msg"`
    Time     int64  `json:"time"`
    Meta     struct {
        AddedBy string `json:"addedby,omitempty"`
        Shadow  bool   `json:"shadow,omitempty"`
    } `json:"meta"`
}

type UserPayload struct {
    Name  string `json:"name"`
    Rank  int    `json:"rank"`
    AFK   bool   `json:"afk"`
    Muted bool   `json:"muted"`
}

type MediaPayload struct {
    ID       string `json:"id"`
    Title    string `json:"title"`
    Seconds  int    `json:"seconds"`
    Duration int    `json:"duration"`
    Type     string `json:"type"`
}

// Framework event data union type
type EventData interface {
    isCytubeEventData()
}
```

### Connection Stability Features

- **Write Mutex**: Ensures thread-safe WebSocket writes for concurrent operations
- **Ping/Pong Handling**: Automatic keepalive with Engine.IO protocol
- **Reconnection Logic**: Exponential backoff with configurable retry attempts
- **Clean Disconnection**: Proper closure handshake to avoid connection leaks
- **Event Format Handling**: Supports both array format (polling) and object format (WebSocket)

## Example Plugin Implementation

### Simple Command Plugin

```go
package plugins

import (
    "encoding/json"
    "strings"
    "github.com/yourusername/daz/framework"
)

type CommandPlugin struct {
    bus      framework.EventBus
    config   CommandConfig
    commands map[string]CommandHandler
}

type CommandConfig struct {
    Prefix      string   `json:"prefix"`
    AdminUsers  []string `json:"admin_users"`
}

func (p *CommandPlugin) Init(config json.RawMessage, bus framework.EventBus) error {
    if err := json.Unmarshal(config, &p.config); err != nil {
        return err
    }
    p.bus = bus
    p.commands = make(map[string]CommandHandler)
    
    // Register for chat messages from filter
    return bus.Subscribe("plugin.command.execute", p.HandleEvent)
}

func (p *CommandPlugin) HandleEvent(event framework.Event) error {
    switch e := event.(type) {
    case *framework.ChatMessageEvent:
        if strings.HasPrefix(e.Message, p.config.Prefix) {
            return p.handleCommand(e)
        }
    }
    return nil
}

func (p *CommandPlugin) handleCommand(event *framework.ChatMessageEvent) error {
    parts := strings.Fields(event.Message)
    if len(parts) == 0 {
        return nil
    }
    
    cmd := strings.TrimPrefix(parts[0], p.config.Prefix)
    handler, exists := p.commands[cmd]
    if !exists {
        return nil
    }
    
    // Log command execution
    p.bus.Send("sql", "log.request", &framework.EventData{
        Source: "command",
        Loggable: true,
        LogLevel: "info",
        Tags: []string{"command", cmd},
        Data: map[string]interface{}{
            "command": cmd,
            "user": event.Username,
            "args": parts[1:],
        },
    })
    
    response := handler(event, parts[1:])
    if response != "" {
        // Send response back through core with metadata
        return p.bus.Send("core", "cytube.send_message", &framework.EventData{
            Source: "command",
            Target: "core",
            Data: map[string]string{
                "message": response,
                "channel": event.ChannelName,
            },
        })
    }
    
    return nil
}

// Self-registration
func init() {
    framework.RegisterPlugin("command", &CommandPlugin{})
}
```

## Database Schema Examples

### SQL Plugin Tables

```sql
-- Logger configuration
CREATE TABLE daz_sql_logger_rules (
    id SERIAL PRIMARY KEY,
    event_pattern VARCHAR(255) NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    table_name VARCHAR(255) NOT NULL,
    fields TEXT[], -- Array of field names to extract
    transform_function VARCHAR(255),
    priority INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW(),
    INDEX idx_pattern (event_pattern)
);

-- Generic event log (for events without specific tables)
CREATE TABLE daz_sql_event_log (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    source_plugin VARCHAR(100),
    correlation_id VARCHAR(100),
    event_data JSONB NOT NULL,
    tags TEXT[],
    INDEX idx_timestamp (timestamp),
    INDEX idx_event_type (event_type),
    INDEX idx_tags USING GIN (tags)
);

-- Chat history (populated by logger middleware)
CREATE TABLE daz_chat_log (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP NOT NULL,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    user_rank INT,
    raw_data JSONB,
    INDEX idx_timestamp (timestamp),
    INDEX idx_channel_user (channel, username)
);

-- User activity tracking
CREATE TABLE daz_core_user_activity (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    metadata JSONB,
    INDEX idx_user_activity (channel, username, timestamp)
);
```

### Plugin Tables

```sql
-- Filter plugin routing rules
CREATE TABLE daz_filter_routes (
    id SERIAL PRIMARY KEY,
    event_type VARCHAR(255) NOT NULL,
    target_plugin VARCHAR(255) NOT NULL,
    conditions JSONB,
    priority INT DEFAULT 0,
    enabled BOOLEAN DEFAULT TRUE,
    INDEX idx_event_type (event_type)
);

-- Command plugin registry
CREATE TABLE daz_command_registry (
    id SERIAL PRIMARY KEY,
    command VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    usage TEXT,
    min_rank INT DEFAULT 0,
    enabled BOOLEAN DEFAULT TRUE
);

-- Example game plugin leaderboard
CREATE TABLE daz_game_leaderboard (
    id SERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    game_name VARCHAR(100) NOT NULL,
    score BIGINT NOT NULL,
    achieved_at TIMESTAMP DEFAULT NOW(),
    metadata JSONB,
    UNIQUE(username, game_name)
);
```

## Security Considerations

### SQL Injection Prevention
- All SQL queries use parameterized statements
- SQL module validates query structure
- No direct SQL string concatenation

### Access Control
- Plugin isolation through event bus
- No direct database access for plugins
- Configuration-based permission system

### Connection Security
- WebSocket connections should use wss:// scheme for secure connections
- Authentication tokens in configuration
- Rate limiting on retry attempts

## Performance Considerations

### Event Bus Optimization
- Non-blocking message delivery
- Configurable buffer sizes per message type
- Event dropping for slow consumers

### Database Performance
- Connection pooling in SQL module
- Indexed columns for common queries
- JSONB for flexible metadata storage

### Memory Management
- No in-memory caching requirement
- Streaming for large result sets
- Bounded message buffers

## Monitoring and Debugging

### Health Checks
```go
type PluginStatus struct {
    Name         string
    State        string // "running", "degraded", "failed"
    LastError    error
    RetryCount   int
    EventsHandled int64
    Uptime       time.Duration
}
```

### Logging Strategy
- Structured logging with context
- Per-plugin log levels
- Event tracing with message IDs
- SQL query logging with timing

## Deployment Architecture

### Container Strategy
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o daz cmd/daz/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/daz .
COPY --from=builder /app/config.json .
CMD ["./daz"]
```

### Environment Requirements
- Go 1.21+
- PostgreSQL 14+
- Linux/Unix environment
- Network access to Cytube servers

## Future Enhancements

### Planned Features
- Plugin hot-reload capability (future)
- Distributed deployment support
- Advanced analytics plugins
- Web dashboard for monitoring
- Plugin marketplace/registry

### Extensibility Points
- Custom event types
- Plugin middleware system
- External API integrations
- Multi-channel support
- Advanced filtering rules

## Contributing Guidelines

### Code Standards
- Follow standard Go conventions
- Comprehensive error handling
- Unit tests for all plugins
- Documentation for public APIs
- Example implementations

### Plugin Development
- Use provided boilerplate
- Follow naming conventions
- Implement full interface
- Handle graceful shutdown
- Document configuration

---

This architecture provides a solid foundation for building a reliable, extensible chat bot system for Cytube. The modular design ensures that new features can be added without affecting core functionality, while the persistent storage approach guarantees minimal data loss during restarts or failures.