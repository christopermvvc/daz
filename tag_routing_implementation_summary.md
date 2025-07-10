# Tag-Based Routing and Filtering Implementation

## Overview
Successfully implemented tag-based routing and wildcard subscriptions in the EventBus, allowing plugins to subscribe to events based on patterns and tags.

## Key Changes

### 1. EventBus Core (`pkg/eventbus/eventbus.go`)
- Added `patternSubscribers` field to track wildcard subscriptions
- Added `SubscribeWithTags` method that accepts:
  - Pattern (e.g., "cytube.event.*" for wildcard matching)
  - Event handler
  - Optional tags for filtering
- Modified `routeEvents` to use `getMatchingSubscribers` which checks both exact and pattern matches
- Added `matchesTags` helper to filter events based on tag criteria
- Maintained backward compatibility with existing `Subscribe` method

### 2. Framework Interface (`internal/framework/plugin.go`)
- Added `SubscribeWithTags` to the EventBus interface
- Existing plugins continue to work without modification

### 3. Event Routing Logic
The new routing logic works as follows:
1. When an event is broadcast, it checks both exact match subscribers and pattern subscribers
2. For pattern subscribers, it uses Go's `path.Match` for wildcard matching
3. If tags are specified in the subscription, all tags must be present in the event metadata
4. Empty tag filter means match all events (no filtering)

## Usage Examples

### Subscribe to All Chat Events
```go
eventBus.SubscribeWithTags("cytube.event.*", handler, []string{"chat"})
```

### Subscribe to User Presence Events
```go
eventBus.SubscribeWithTags("cytube.event.*", handler, []string{"user", "presence"})
```

### Subscribe to All Events of a Type (No Tag Filter)
```go
eventBus.SubscribeWithTags("cytube.event.*", handler, nil)
```

### Traditional Exact Match (Still Works)
```go
eventBus.Subscribe("cytube.event.chatMsg", handler)
```

## Event Tagging Schema
Events are already tagged in `room_manager.go`:
- Chat events: `["chat", "public"]` or `["chat", "private"]`
- User events: `["user", "presence"]`, `["user", "status"]`, etc.
- Media events: `["media", "playlist"]`, `["media", "sync"]`, etc.
- System events: `["system", "auth"]`, `["system", "connection"]`, etc.

## Benefits
1. **Flexible Event Subscription**: Plugins can subscribe to broad categories of events
2. **Reduced Boilerplate**: No need to subscribe to each event type individually
3. **Better Organization**: Events are logically grouped by tags
4. **Performance**: Pattern matching only happens during subscription matching, not registration
5. **Backward Compatible**: Existing exact-match subscriptions continue to work

## Testing
All tests pass and the implementation maintains backward compatibility. The example in `examples/tag_based_routing_example.go` demonstrates various usage patterns.