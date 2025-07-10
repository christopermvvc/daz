# Daz Architecture Document Revisions

This document tracks architectural decisions and changes that deviate from or enhance the original architecture document.

## Revision History

### 2025-07-07: Phase 1 Implementation Discoveries

#### Socket.IO v4 Protocol Requirement
**Original Plan**: "WebSocket connection using standard WebSocket protocol"  
**Actual Implementation**: WebSocket connection requires Socket.IO v4 protocol layer

**Rationale**: Cytube uses Socket.IO for its real-time communication, which adds a protocol layer on top of standard WebSocket. This includes:
- Engine.IO handshake sequence
- Specific packet type formatting (0, 2, 3, 40, 42)
- Message framing with event names and data arrays

**Impact**: Minimal - the WebSocket transport remains the same, but message formatting follows Socket.IO conventions.

#### Authentication Flow Timing
**Discovery**: Cytube requires a 2-second delay between joining a channel and sending login credentials.

**Implementation**: Added a timer-based delay after channel join before attempting authentication.

**Rationale**: This appears to be a Cytube server-side requirement to prevent rapid authentication attempts.

#### Dynamic Server Discovery
**Enhancement**: Added dynamic server discovery via `/socketconfig/{channel}.json` API.

**Rationale**: Cytube can route different channels to different servers. Dynamic discovery ensures we always connect to the correct server for a given channel.

#### Concrete Types Over interface{}
**Requirement**: Replaced all interface{} usage with concrete types throughout the codebase.

**Implementation**: Created specific structs for:
- `ChannelJoinData`
- `LoginData`
- `ChatMessagePayload`
- `UserPayload`
- `MediaPayload`
- `EventData` (interface with marker method)

**Rationale**: Type safety, better IDE support, and compliance with project coding standards that forbid interface{} usage.

## Future Considerations

### Phase 2: PostgreSQL Integration
Based on Phase 1 learnings, we should consider:
- Using pgx as the PostgreSQL driver (pure Go implementation)
- Storing raw Socket.IO event data in JSONB columns for flexibility
- Creating specific schemas for each event type we've discovered

### Event Bus Design
The concrete types created in Phase 1 will influence the event bus design:
- Events can be strongly typed from the start
- No need for runtime type assertions
- Better compile-time guarantees

### 2025-07-07: Filter Plugin and Command Router Merger

#### EventFilter Plugin Consolidation
**Original Plan**: Separate Filter Plugin and Command Router Plugin  
**Actual Implementation**: Merged into single EventFilter plugin

**Rationale**: Analysis revealed significant overlap between the two plugins:
- Both examine incoming events and make routing decisions
- Both need access to similar configuration and state
- Separate plugins created redundant event processing
- Unclear separation of responsibilities

**New Design**: The EventFilter plugin now provides:
- **Event Filtering**: Spam blocking, rate limiting, content filtering
- **Event Routing**: Directing events to appropriate handler plugins  
- **Command Processing**: Detection, parsing, and routing of commands
- **Unified Permissions**: Single system for both filter rules and command access

**Benefits**:
1. Single analysis pass for each event (performance improvement)
2. Unified configuration system for all routing rules
3. Clearer architectural boundaries
4. Simplified plugin communication flow
5. Reduced code duplication

**Impact**: 
- Startup sequence simplified (one plugin instead of two)
- Event flow now: `Core → EventFilter → Target Plugins`
- Database schema uses `daz_eventfilter_*` prefix for all tables
- Command Router functionality fully preserved within EventFilter

**Implementation Completed**: 2025-07-07
- Created new `internal/plugins/eventfilter/` directory
- Merged functionality from both plugins into single codebase
- Updated main.go to use only eventfilter plugin
- Deleted old filter and commandrouter plugin directories
- All tests passing with new implementation

### 2025-07-07: Phase 5 Plugin Implementation

#### Media Tracker Plugin
**Purpose**: Track video plays, durations, and provide media statistics
**Implementation**: `/internal/plugins/mediatracker/`

**Features Implemented**:
- Real-time media change tracking via `cytube.event.changeMedia` events
- Media play history with start/end times and completion status
- Current "now playing" status with elapsed/remaining time
- Top played media statistics
- PostgreSQL persistence with three tables:
  - `daz_mediatracker_plays` - Individual play records
  - `daz_mediatracker_queue` - Queue state (future use)
  - `daz_mediatracker_stats` - Aggregated play counts

**Integration Points**:
- Subscribes to media change and queue events
- Provides data via `plugin.mediatracker.nowplaying` and `plugin.mediatracker.stats` requests
- Maintains in-memory current media state for fast queries

#### Analytics Plugin
**Purpose**: Aggregate channel statistics and provide insights
**Implementation**: `/internal/plugins/analytics/`

**Features Implemented**:
- Real-time message and user counting
- Hourly statistics aggregation (messages, unique users, media plays)
- Daily statistics rollup with peak users and active hours
- Per-user lifetime statistics tracking
- Top chatters leaderboard
- PostgreSQL persistence with three tables:
  - `daz_analytics_hourly` - Hourly aggregates
  - `daz_analytics_daily` - Daily rollups  
  - `daz_analytics_user_stats` - Per-user statistics

**Integration Points**:
- Subscribes to chat messages for real-time tracking
- Provides formatted stats via `plugin.analytics.stats` requests
- Runs background aggregation tasks on configurable intervals

#### Plugin Loading Order
The current plugin initialization order in main.go:
1. Core Plugin (Cytube connection + SQL)
2. Filter Plugin (event filtering and command detection)
3. UserTracker Plugin (user session tracking)
4. MediaTracker Plugin (media play tracking)
5. Analytics Plugin (statistics aggregation)
6. Command Plugins (help, about, uptime)
7. CommandRouter Plugin (command dispatch)

This order ensures dependencies are satisfied:
- Analytics depends on MediaTracker for play counts
- Commands depend on UserTracker and MediaTracker for data
- CommandRouter must start after all command plugins register

### 2025-07-07: Configuration System Implementation

#### JSON Configuration Support
**Enhancement**: Added comprehensive configuration file support
**Implementation**: `/internal/config/` package

**Features Added**:
- JSON configuration file loading with `-config` flag
- Command-line flag precedence over config file values
- Hierarchical configuration structure (core, event_bus, plugins)
- Type-safe configuration with validation
- Default configuration values when file not present

**Configuration Structure**:
```json
{
  "core": {
    "cytube": { /* connection settings */ },
    "database": { /* PostgreSQL settings */ }
  },
  "event_bus": {
    "buffer_sizes": { /* event type buffer sizes */ }
  },
  "plugins": {
    "pluginname": { /* plugin-specific config */ }
  }
}
```

**Benefits**:
- Easier deployment configuration
- No need to specify all flags on command line
- Plugin-specific configuration sections
- Environment-specific config files

### 2025-07-07: Plugin Interface Standardization

#### framework.Plugin Interface Enhancement
**Original**: Inconsistent plugin initialization patterns
**Enhancement**: Standardized all plugins to implement framework.Plugin interface

**Changes Made**:
1. Added `Name()` method to framework.Plugin interface
2. All plugins now implement consistent interface:
   - `Init(config json.RawMessage, bus EventBus) error`
   - `Start() error`
   - `Stop() error`
   - `HandleEvent(event Event) error`
   - `Status() PluginStatus`
   - `Name() string`

3. Refactored feature plugins (eventfilter, usertracker, mediatracker, analytics):
   - Added `New()` constructor returning framework.Plugin
   - Replaced custom `Initialize()` with standard `Init()`
   - Removed context parameter from HandleEvent
   - Added proper Status() implementation

4. Maintained backward compatibility with deprecated methods

**Plugin Loading in main.go**:
```go
plugins := []struct {
    name   string
    plugin framework.Plugin
}{
    {"eventfilter", eventfilter.New()},
    {"usertracker", usertracker.New()},
    // ... all plugins
}

for _, p := range plugins {
    pluginConfig := cfg.GetPluginConfig(p.name)
    p.plugin.Init(pluginConfig, bus)
    p.plugin.Start()
}
```

**Benefits**:
- Consistent plugin lifecycle management
- Configuration delivery to all plugins
- Better monitoring with Status() method
- Foundation for dynamic plugin loading
- Simplified main.go initialization code

### 2025-07-08: SQL Plugin Compartmentalization and EventBus Enhancement

#### Major Architectural Change: SQL as Standalone Plugin
**Original Design**: SQL module embedded within Core plugin with automatic event logging
**New Design**: SQL as independent plugin communicating exclusively via EventBus

**Rationale**: 
1. **Separation of Concerns**: Core plugin should focus solely on Cytube connection
2. **Selective Logging**: Not all events need database persistence
3. **Performance**: Reduce database load with configurable logging rules
4. **Flexibility**: Plugins can choose what to persist
5. **True Modularity**: SQL becomes just another plugin, not a special case

**Key Changes**:

1. **EventBus as Primary Communication System**:
   - EventBus now starts first, before all plugins
   - Enhanced with request/response infrastructure
   - Added event tagging and metadata system
   - Full bidirectional communication support
   - Correlation IDs for tracking requests/responses

2. **SQL Plugin Architecture**:
   - Completely decoupled from Core plugin
   - Implements logger middleware pattern
   - Configuration-driven logging rules
   - Subscribes to specific event patterns
   - Provides database services via EventBus

3. **Logger Middleware Pattern**:
   ```json
   "logger_rules": [
     {
       "event_pattern": "cytube.event.chatMsg",
       "enabled": true,
       "table": "daz_chat_log",
       "fields": ["username", "message", "timestamp"]
     }
   ]
   ```

4. **New Startup Sequence**:
   1. EventBus initialization (with enhanced features)
   2. SQL Plugin (database service)
   3. Core Plugin (Cytube connection only)
   4. EventFilter Plugin
   5. Feature Plugins

5. **Data Flow Changes**:
   - **Before**: Cytube → Core → Direct SQL logging → EventBus → Plugins
   - **After**: Cytube → Core → EventBus (tagged) → SQL Plugin (selective) → Other Plugins

6. **EventData Enhancement**:
   ```go
   type EventData struct {
       // Metadata for routing and logging
       CorrelationID string
       Source        string
       Target        string
       Timestamp     time.Time
       Priority      int
       
       // Logging control
       Loggable      bool
       LogLevel      string
       Tags          []string
       
       // Payload
       Type          string
       Data          interface{}
       
       // Request/Response
       ReplyTo       string
       Timeout       time.Duration
   }
   ```

**Benefits**:
1. **Reduced Database Load**: Only log what's needed
2. **Better Performance**: Selective logging improves throughput
3. **Plugin Independence**: SQL is no longer special
4. **Flexible Logging**: Plugins can request specific logging
5. **True Decoupling**: No direct dependencies between Core and SQL
6. **Enhanced Communication**: Full request/response patterns

**Migration Impact**:
- Core plugin needs refactoring to remove SQL module
- All events must be properly tagged with metadata
- Existing plugins need updates for new EventData structure
- SQL plugin must be extracted as standalone component
- EventBus requires enhancement for new features

**Implementation Status**: 🔄 IN PROGRESS
- [x] Architecture design completed
- [x] EventBus enhancement plan defined
- [x] SQL plugin structure designed
- [ ] Core plugin refactoring
- [ ] SQL plugin implementation
- [ ] EventBus enhancements
- [ ] Plugin migrations
- [ ] Testing and validation

## Architecture Principles Maintained

Despite these revisions, the core architectural principles remain intact:
- ✅ Plugin-based modularity (enhanced with SQL as plugin)
- ✅ Event-driven communication (enhanced with bidirectional support)
- ✅ PostgreSQL persistence focus (now selective)
- ✅ Graceful degradation
- ✅ Restart resilience
- ✅ Type safety (minimal interface{} usage)
- ✅ Clean, testable code

All changes enhance rather than contradict the original design goals.

### 2025-07-10: Interface{} Elimination Initiative

#### Type Safety Enhancement
**Original State**: Widespread use of interface{} throughout the codebase
**New Implementation**: Eliminated most interface{} usage with concrete types

**Changes Made**:

1. **FlexibleUID Type**:
   - Created custom type to handle Cytube's dual string/number UID fields
   - Implements proper JSON marshaling/unmarshaling
   - Provides type-safe access methods

2. **EventBus SQL Methods**:
   - Changed from `...interface{}` to `...framework.SQLParam`
   - All SQL operations now use typed parameters
   - Maintains compatibility with database/sql at the boundary

3. **Plugin Configuration**:
   - Changed from `map[string]interface{}` to `map[string]json.RawMessage`
   - Plugins receive typed configuration data
   - Better compile-time checking

4. **Event Data Structures**:
   - Replaced generic maps with concrete types
   - Created specific payload types for each event
   - Improved IDE support and documentation

**Justified Exceptions**:
As documented in the architecture document, the following interface{} uses remain:

1. **SQLParam.Value**: Required for database/sql compatibility
2. **Cytube Metadata Fields**: Preserves arbitrary JSON from Cytube
3. **QueryResult.Scan**: Matches sql.Rows interface

**Benefits**:
- Compile-time type checking catches errors early
- Better IDE autocompletion and refactoring support
- Clearer API contracts between components
- Reduced runtime type assertion errors
- Improved code maintainability

**Implementation Status**: ✅ COMPLETE
- All plugins updated to use concrete types
- All tests passing with new signatures
- Binary builds successfully
- Architecture document updated with type safety section

### 2025-07-10: SQL Plugin Event-Driven Architecture Implementation

#### Complete Decoupling from EventBus
**Changes Made**:
- Removed all SQL-specific methods from EventBus interface (Query, Exec, QuerySync, ExecSync)
- SQL plugin now subscribes to event patterns: `sql.query.request`, `sql.exec.request`, `sql.batch.query`
- Created `SQLClient` helper class for plugins to simplify event-based SQL operations
- Implemented logger middleware pattern for selective event logging

**Architecture**:
```go
// Before: Direct SQL methods on EventBus
eventBus.QuerySync("SELECT ...", params...)

// After: Event-based communication
sqlClient := framework.NewSQLClient(eventBus, "plugin-name")
rows, err := sqlClient.QueryContext(ctx, "SELECT ...", params...)
```

**Benefits**:
- EventBus is now a pure message broker
- SQL implementation can be swapped without changing core interfaces
- Clean separation of concerns
- Better testability

**Implementation Status**: ✅ COMPLETE

### 2025-07-10: Enhanced EventBus Features

#### Request/Response Infrastructure
**Implementation**: ✅ COMPLETE
- Added `Request()` method for synchronous operations with timeout support
- Correlation ID tracking for matching requests to responses
- Context-based cancellation support

#### Event Metadata System
**Implementation**: ✅ COMPLETE
- Created comprehensive `EventMetadata` struct with:
  - Correlation ID for request tracking
  - Source/Target for directed communication
  - Priority field for message prioritization
  - Tags for filtering and routing
  - Logging directives (loggable, log level)
- Builder pattern for metadata construction

#### Bidirectional Communication
**Implementation**: ✅ COMPLETE
- Full request/response pattern support
- Plugin-to-plugin communication via EventBus
- Timeout handling with proper cleanup

#### Not Yet Implemented
1. **Priority-based Message Delivery**: Priority field exists but EventBus doesn't use it for routing
2. **Tag-based Routing**: Tags exist in metadata but no routing rules implemented

**Architecture Status**:
The enhanced EventBus provides all the infrastructure needed for the complete implementation. The remaining work is to:
1. Add priority queue support to the EventBus router
2. Implement tag-based subscription patterns
3. Update all Cytube events to include proper metadata with tags