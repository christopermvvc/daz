# Tag Filtering System Design for EventBus

## Overview

The EventBus already has basic tag filtering support through the `SubscribeWithTags` method and `matchesTags` function. This design document outlines enhancements to create a more powerful and flexible tag filtering system.

## Current Implementation Analysis

### Existing Features
- Basic tag matching in `EventMetadata.Tags`
- `SubscribeWithTags` method for subscribing with tag filters
- Simple "contains all" matching logic in `matchesTags`
- Tag support in event metadata

### Current Limitations
- Only supports "contains all" matching (AND logic)
- No support for tag negation
- No support for OR operations or complex boolean expressions
- Limited performance optimization for tag matching

## Proposed Tag Filtering Design

### 1. Tag Matching Strategies

#### A. Match Modes
```go
type TagMatchMode int

const (
    // MatchAll - Event must have ALL specified tags (current behavior)
    MatchAll TagMatchMode = iota
    
    // MatchAny - Event must have AT LEAST ONE of the specified tags
    MatchAny
    
    // MatchExact - Event must have EXACTLY the specified tags, no more, no less
    MatchExact
    
    // MatchExpression - Use a tag expression for complex matching
    MatchExpression
)
```

#### B. Tag Expression Language
For complex matching scenarios, support a simple expression language:

```
Examples:
- "chat AND !spam" - Has 'chat' tag but NOT 'spam' tag
- "(media OR playlist) AND important" - Has either 'media' or 'playlist', AND 'important'
- "user:* AND !user:bot" - Any tag starting with 'user:' but not 'user:bot'
- "priority:high OR priority:critical" - Either high or critical priority
```

### 2. Enhanced Subscription API

```go
// Simple tag subscription with mode
func (eb *EventBus) SubscribeWithTagMode(
    pattern string, 
    handler framework.EventHandler, 
    tags []string, 
    mode TagMatchMode,
) error

// Advanced subscription with tag expression
func (eb *EventBus) SubscribeWithTagExpression(
    pattern string,
    handler framework.EventHandler,
    expression string,
) error

// Subscription builder pattern for complex scenarios
type SubscriptionBuilder struct {
    eventBus *EventBus
    pattern  string
    handler  framework.EventHandler
}

func (eb *EventBus) NewSubscription(pattern string, handler framework.EventHandler) *SubscriptionBuilder

func (sb *SubscriptionBuilder) WithTags(tags ...string) *SubscriptionBuilder
func (sb *SubscriptionBuilder) WithTagMode(mode TagMatchMode) *SubscriptionBuilder
func (sb *SubscriptionBuilder) WithTagExpression(expr string) *SubscriptionBuilder
func (sb *SubscriptionBuilder) WithPriority(priority int) *SubscriptionBuilder
func (sb *SubscriptionBuilder) Subscribe() error
```

### 3. Tag Negation Support

#### A. Negative Tags in Expressions
```go
// Tag expression parser that supports negation
type TagExpression struct {
    raw        string
    compiled   tagExpressionNode // Internal AST representation
}

// Parse expressions like "!spam", "chat AND !bot", etc.
func ParseTagExpression(expr string) (*TagExpression, error)
func (te *TagExpression) Matches(eventTags []string) bool
```

#### B. Exclusion Lists
```go
// Enhanced subscriberInfo with exclusion support
type subscriberInfo struct {
    Handler      framework.EventHandler
    Name         string
    Pattern      string
    Tags         []string      // Required tags
    ExcludeTags  []string      // Excluded tags (NOT logic)
    TagMode      TagMatchMode
    TagExpr      *TagExpression // For complex expressions
}
```

### 4. Integration with Existing System

#### A. Enhanced matchesTags Function
```go
// Replace current matchesTags with more sophisticated version
func (eb *EventBus) matchesTags(sub subscriberInfo, metadata *framework.EventMetadata) bool {
    if metadata == nil || len(metadata.Tags) == 0 {
        // No tags in event
        if len(sub.Tags) == 0 && len(sub.ExcludeTags) == 0 && sub.TagExpr == nil {
            return true // No filter means match all
        }
        return false // Has filter but no tags to match
    }
    
    // Handle tag expression first (highest priority)
    if sub.TagExpr != nil {
        return sub.TagExpr.Matches(metadata.Tags)
    }
    
    // Handle exclusion tags
    for _, excludeTag := range sub.ExcludeTags {
        if containsTag(metadata.Tags, excludeTag) {
            return false // Event has excluded tag
        }
    }
    
    // Handle inclusion tags based on mode
    if len(sub.Tags) == 0 {
        return true // No required tags, only exclusions
    }
    
    switch sub.TagMode {
    case MatchAll:
        return containsAllTags(metadata.Tags, sub.Tags)
    case MatchAny:
        return containsAnyTag(metadata.Tags, sub.Tags)
    case MatchExact:
        return exactTagMatch(metadata.Tags, sub.Tags)
    default:
        return containsAllTags(metadata.Tags, sub.Tags) // Default to current behavior
    }
}
```

#### B. Backward Compatibility
- Existing `SubscribeWithTags` continues to work with "MatchAll" behavior
- No changes to existing event publishing APIs
- Tags remain optional in `EventMetadata`

### 5. Performance Optimizations

#### A. Tag Indexing
```go
// Index structure for fast tag lookups
type tagIndex struct {
    // Map from tag to list of subscribers interested in that tag
    tagToSubscribers map[string][]int // subscriber indices
    
    // Wildcard patterns (e.g., "user:*")
    wildcardPatterns map[string][]int
    
    // Precompiled expressions for performance
    expressions map[int]*TagExpression
}

// Build index when subscribers are added/removed
func (eb *EventBus) rebuildTagIndex(eventType string)
```

#### B. Caching Strategy
```go
// Cache matching results for frequently used tag combinations
type tagMatchCache struct {
    cache map[string][]int // tag combination hash -> matching subscriber indices
    mu    sync.RWMutex
}

// Generate cache key from tags
func tagCacheKey(tags []string) string {
    sorted := make([]string, len(tags))
    copy(sorted, tags)
    sort.Strings(sorted)
    return strings.Join(sorted, "|")
}
```

#### C. Fast Path Optimizations
1. **No-tag events**: Skip tag matching entirely
2. **Single tag subscriptions**: Use direct map lookup
3. **Common patterns**: Cache results for frequent tag combinations
4. **Bitset operations**: For large numbers of tags, use bitsets

### 6. Monitoring and Debugging

#### A. Tag Statistics
```go
type TagStats struct {
    TagCounts    map[string]int64  // How often each tag appears
    MatchCounts  map[string]int64  // How often each subscription matches
    CacheHitRate float64           // Tag cache effectiveness
}

func (eb *EventBus) GetTagStats() *TagStats
```

#### B. Debug Mode
```go
// Enable tag matching debug logs
func (eb *EventBus) SetTagDebug(enabled bool)

// Log output example:
// [EventBus] Tag match: event tags=[chat, public, user:alice] 
//            subscription tags=[chat] mode=MatchAll result=true
```

## Implementation Phases

### Phase 1: Core Enhancements (Week 1)
1. Add `TagMatchMode` enumeration
2. Enhance `subscriberInfo` structure
3. Update `matchesTags` to support new modes
4. Add `SubscribeWithTagMode` method
5. Maintain backward compatibility

### Phase 2: Tag Expressions (Week 2)
1. Design and implement tag expression parser
2. Add expression evaluation engine
3. Add `SubscribeWithTagExpression` method
4. Implement wildcard support (e.g., "user:*")

### Phase 3: Performance Optimization (Week 3)
1. Implement tag indexing
2. Add caching layer
3. Optimize common cases
4. Add performance benchmarks

### Phase 4: Monitoring and Polish (Week 4)
1. Add tag statistics collection
2. Implement debug mode
3. Add comprehensive tests
4. Update documentation and examples

## Example Usage

### Basic Tag Filtering
```go
// Subscribe to chat messages that aren't spam
eb.SubscribeWithTagMode(
    "cytube.event.chatMsg",
    handler,
    []string{"chat"},      // Required tags
    eventbus.MatchAll,
)

// Subscribe to any media or playlist event
eb.SubscribeWithTagMode(
    "cytube.event.*",
    handler,
    []string{"media", "playlist"},
    eventbus.MatchAny,
)
```

### Tag Expressions
```go
// Complex expression: high priority alerts but not test
eb.SubscribeWithTagExpression(
    "alert.*",
    handler,
    "(priority:high OR priority:critical) AND !test",
)

// User events excluding bots
eb.SubscribeWithTagExpression(
    "cytube.event.*",
    handler,
    "user AND presence AND !bot",
)
```

### Builder Pattern
```go
// Fluent API for complex subscriptions
eb.NewSubscription("cytube.event.*", handler).
    WithTags("chat", "public").
    WithTagMode(eventbus.MatchAll).
    WithPriority(2).
    Subscribe()
```

## Performance Considerations

### Tag Matching Complexity
- **MatchAll**: O(n*m) where n=subscription tags, m=event tags
- **MatchAny**: O(n*m) worst case, O(1) best case
- **MatchExact**: O(n+m) with proper implementation
- **Expression**: O(e) where e=expression complexity

### Optimization Strategies
1. **Early termination**: Stop checking once result is determined
2. **Tag indexing**: O(1) lookup for exact tag matches
3. **Caching**: Amortize cost over multiple events
4. **Bitset operations**: For scenarios with fixed tag vocabulary

## Security Considerations

1. **Expression validation**: Prevent malicious expressions
2. **Tag limits**: Maximum tags per event/subscription
3. **Expression complexity limits**: Prevent DoS via complex expressions
4. **Tag name validation**: Enforce naming conventions

## Migration Guide

### For Existing Code
```go
// Old code - still works
eb.SubscribeWithTags("pattern", handler, []string{"tag1", "tag2"})

// Equivalent new code
eb.SubscribeWithTagMode("pattern", handler, []string{"tag1", "tag2"}, eventbus.MatchAll)
```

### Gradual Adoption
1. Phase 1: Use new APIs for new features only
2. Phase 2: Migrate high-value subscriptions
3. Phase 3: Update remaining code at convenience
4. Phase 4: Deprecate old APIs (optional)

## Conclusion

This tag filtering system design provides:
- Multiple matching strategies for different use cases
- Support for tag negation and complex expressions
- Performance optimizations for production use
- Backward compatibility with existing code
- Clear migration path for adoption

The phased implementation approach ensures we can deliver value incrementally while maintaining system stability.