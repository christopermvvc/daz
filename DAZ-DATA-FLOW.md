# Daz Bot Data Flow Visualization

## 1. High-Level Architecture Flow

```
┌─────────────┐     WebSocket      ┌──────────────┐     Broadcast    ┌─────────────┐
│   Cytube    │ ◄═════════════► │ Core Plugin  │ ═══════════► │  Event Bus  │
│   Server    │                     │ + SQL Module │                │ (Go Channels)│
└─────────────┘                     └──────┬───────┘                └──────┬──────┘
                                            │                                │
                                            │ SQL Operations                 │ Route
                                            ▼                                ▼
                                    ┌──────────────┐                ┌─────────────┐
                                    │  PostgreSQL  │                │ EventFilter │
                                    │   Database   │                │   Plugin    │
                                    └──────────────┘                └──────┬──────┘
                                                                            │
                                                                            ▼
                                                                    ┌─────────────┐
                                                                    │  Feature    │
                                                                    │  Plugins    │
                                                                    └─────────────┘
```

## 2. Detailed Message Flow Sequence

```
Cytube ─────────► Core Plugin ─────────► Event Bus ─────────► EventFilter ─────────► Plugins
  │                     │                     │                     │                    │
  │ Chat Event         │ Parse & Log         │ Broadcast         │ Filter &            │ Process
  │                     │                     │                 │ Route               │
  │                     ▼                     │                     │                    ▼
  │             ┌──────────────┐              │                     │            ┌──────────────┐
  │             │ SQL Database │              │                     │            │ SQL Request  │
  │             │ (Log Event)  │              │                     │            └──────┬───────┘
  │             └──────────────┘              │                     │                     │
  │                                           │                     │                     ▼
  │                                           │                     │            ┌──────────────┐
  │◄──────────────────────────────────────────┼─────────────────────┼────────────│ SQL Response │
  │  Chat Reply                               │                     │            └──────────────┘
  └───────────────────────────────────────────┘                     │
```

## 3. Event Bus Message Types

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            EVENT BUS (Go Channels)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐     │
│  │ cytube.event.*  │     │  sql.request    │     │ plugin.request.*│     │
│  │                 │     │                 │     │                 │     │
│  │ • chatMsg       │     │ • INSERT        │     │ • command.exec  │     │
│  │ • userJoin      │     │ • SELECT        │     │ • custom.action │     │
│  │ • userLeave     │     │ • UPDATE        │     │ • plugin.notify │     │
│  │ • videoChange   │     │ • DELETE        │     │                 │     │
│  └─────────────────┘     └─────────────────┘     └─────────────────┘     │
│                                                                             │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐     │
│  │  sql.response   │     │ cytube.send.*   │     │eventfilter.route│     │
│  │                 │     │                 │     │                 │     │
│  │ • Query Results │     │ • sendChatMsg   │     │ • Command Route │     │
│  │ • Row Count     │     │ • sendCommand   │     │ • Event Route   │     │
│  │ • Error Info    │     │ • sendPM        │     │ • Filter Block  │     │
│  └─────────────────┘     └─────────────────┘     └─────────────────┘     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## 4. Connection Lifecycle & Recovery

```
┌──────────┐      ┌──────────┐      ┌──────────┐      ┌──────────┐      ┌──────────┐
│Discovery │ ───► │Handshake │ ───► │ Session  │ ───► │  Join    │ ───► │  Login   │
│/socketcfg│      │Engine.IO │      │Socket.IO │      │ Channel  │      │Authentic │
└──────────┘      └─────┬────┘      └──────────┘      └──────────┘      └──────────┘
                        │                                                        │
                        │ Connection Failed                                      │
                        ▼                                                        │
                ┌──────────────┐           ┌──────────────┐                    │
                │    Retry     │ ────────► │   Cooldown   │                    │
                │ (10 attempts)│           │  (30 min)    │ ◄──────────────────┘
                └──────────────┘           └──────┬───────┘      Exhausted
                        ▲                          │              Retries
                        │                          │
                        └──────────────────────────┘
                              After Cooldown
```

## 5. Plugin Interaction Map

```
                                    ┌─────────────┐
                                    │   Cytube    │
                                    │   Server    │
                                    └──────┬──────┘
                                           │
                                           ▼
                            ┌──────────────────────────┐
                            │      Core Plugin        │
                            │      + SQL Module       │
                            └───────────┬──────────────┘
                                        │
                     ┌──────────────────┼──────────────────┐
                     │                  ▼                  │
              ┌──────┴──────┐    ┌─────────────┐   ┌──────┴──────┐
              │EventFilter  │◄───│  Event Bus  │───►│   Logger    │
              │   Plugin    │    │(Go Channels)│    │   Plugin    │
              └──────┬──────┘    └──────┬──────┘   └─────────────┘
                     │                  │
         ┌───────────┼──────────────────┼──────────────────┐
         │           │                  │                  │
    ┌────┴────┐ ┌───┴────┐      ┌─────┴──────┐   ┌───────┴───────┐
    │ Custom  │ │Analytics│      │   Custom   │   │    Future     │
    │Commands │ │ Plugin  │      │   Plugin   │   │    Plugins    │
    └─────────┘ └─────────┘      └────────────┘   └───────────────┘
                     │
                     ▼
              ┌─────────────┐
              │ PostgreSQL  │
              │  Database   │
              └─────────────┘
```

## 6. Database Schema Overview

```
┌────────────────────────────────────────────────────────────────┐
│                    PostgreSQL Database                         │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  Core Tables:                                                  │
│  ┌──────────────────────┐    ┌──────────────────────┐        │
│  │ daz_core_chat_log    │    │ daz_core_retry_state │        │
│  ├──────────────────────┤    ├──────────────────────┤        │
│  │ • id (SERIAL)        │    │ • channel (TEXT)     │        │
│  │ • timestamp          │    │ • retry_count        │        │
│  │ • channel            │    │ • cooldown_until     │        │
│  │ • username           │    │ • last_attempt       │        │
│  │ • message            │    └──────────────────────┘        │
│  │ • event_type         │                                     │
│  │ • metadata (JSONB)   │                                     │
│  └──────────────────────┘                                     │
│                                                                │
│  EventFilter Tables:                                           │
│  ┌──────────────────────┐    ┌──────────────────────┐        │
│  │ daz_eventfilter_*    │    │ daz_eventfilter_cmds │        │
│  └──────────────────────┘    └──────────────────────┘        │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

## 7. Key Data Transformations

### Input Processing
```
Raw Socket.IO: "42[\"chatMsg\",{\"username\":\"user\",\"msg\":\"hello\",\"time\":1234567890}]"
                    │
                    ▼
Parsed Event: ChatMessageEvent{
    Username: "user",
    Message: "hello",
    Timestamp: time.Time(1234567890),
    Rank: 0,
    Meta: {...}
}
                    │
                    ▼
Event Bus Message: Message{
    Type: "cytube.event.chatMsg",
    Payload: ChatMessageEvent{...}
}
```

### Database Storage
```
Event Object ───► JSONB Metadata ───► Indexed Fields ───► Query Results
                        │                    │
                        ▼                    ▼
                  Flexible Schema      Fast Lookups
                  (metadata column)    (timestamp, username, channel)
```

## Summary

The Daz bot architecture ensures:
- **Reliability**: Connection retry logic with exponential backoff
- **Scalability**: Plugin-based architecture with event-driven design
- **Resilience**: Continues logging even if plugins fail
- **Extensibility**: New plugins can be added without core changes
- **Performance**: Non-blocking event distribution, connection pooling
- **Security**: Parameterized queries, proper authentication flow

## Main Data Flow Path:
**Cytube → Core Plugin → Event Bus → EventFilter → Feature Plugins → Database**

## Key Components:
1. **Cytube Server**: External IRC-like chat service
2. **Core Plugin**: WebSocket connection manager + SQL module
3. **Event Bus**: Go channels for inter-plugin communication
4. **EventFilter Plugin**: Filters events, routes to handlers, processes commands
5. **Feature Plugins**: Custom commands, analytics, logging, etc.
6. **PostgreSQL Database**: Persistent storage

## Message Types:
- `cytube.event.*`: Broadcast messages from Cytube
- `sql.request/response`: Database operations
- `plugin.request.*`: Inter-plugin communication
- `cytube.send.*`: Outbound messages to Cytube
- `eventfilter.route`: Routing decisions from EventFilter

## Connection Flow:
1. Discovery → Handshake → Session → Join Channel → Login
2. Retry mechanism: 10 attempts with exponential backoff
3. 30-minute cooldown after exhausting retries

The architecture emphasizes reliability through retry mechanisms, scalability through plugin-based design, and resilience by continuing core operations even when individual components fail.