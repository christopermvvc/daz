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

## Architecture Principles Maintained

Despite these revisions, the core architectural principles remain intact:
- ✅ Plugin-based modularity
- ✅ Event-driven communication
- ✅ PostgreSQL persistence focus
- ✅ Graceful degradation
- ✅ Restart resilience

All changes enhance rather than contradict the original design goals.