# Playlist Reception Fix - Utilizing Both Recording Methods

## Current State

The MediaTracker plugin currently records media items to the library in two scenarios:
1. **When media is played** - Via `changeMedia` events
2. **When items are queued** - Via queue update events

However, we're missing the ability to capture the **full playlist** when CyTube sends it as an array during initial connection or refresh.

## Problem

CyTube sends playlist data in two different formats:
1. **Full playlist array** - Sent on connection when authorized: `[{media: {...}}, {media: {...}}, ...]`
2. **Individual updates** - Sent when items are added/removed/moved

Our current implementation only handles individual updates, missing the opportunity to capture the entire playlist at once.

## Solution Overview

We need to implement proper handling for both playlist data formats to ensure complete media library population.

## Implementation Steps

### 1. Create a Dedicated Playlist Event Type

**File: `/home/user/Documents/daz/internal/framework/events.go`**

Add a new event type that can handle the full playlist array:

```go
// PlaylistArrayEvent represents a full playlist sent as an array
type PlaylistArrayEvent struct {
    CytubeEvent
    Items []PlaylistItem `json:"items"`
}

// PlaylistItem represents a single item in the playlist
type PlaylistItem struct {
    MediaID   string                 `json:"media_id"`
    MediaType string                 `json:"media_type"`
    Title     string                 `json:"title"`
    Duration  int                    `json:"duration"`
    QueuedBy  string                 `json:"queued_by"`
    Position  int                    `json:"position"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
}
```

### 2. Update the CyTube Parser

**File: `/home/user/Documents/daz/pkg/cytube/parser.go`**

The parser already detects array vs object format. We need to ensure it properly creates a PlaylistArrayEvent:

```go
func (p *Parser) parsePlaylist(base framework.CytubeEvent, data json.RawMessage) (framework.Event, error) {
    trimmed := string(data)
    if len(trimmed) > 0 && trimmed[0] == '[' {
        // Array format - full playlist
        var items []PlaylistArrayItem
        if err := json.Unmarshal(data, &items); err != nil {
            return nil, fmt.Errorf("unmarshal playlist array: %w", err)
        }
        
        // Convert to PlaylistArrayEvent
        event := &framework.PlaylistArrayEvent{
            CytubeEvent: base,
            Items:       make([]framework.PlaylistItem, len(items)),
        }
        
        for i, item := range items {
            event.Items[i] = framework.PlaylistItem{
                MediaID:   item.Media.ID,
                MediaType: item.Media.Type,
                Title:     item.Media.Title,
                Duration:  item.Media.Seconds,
                QueuedBy:  item.QueueBy,
                Position:  i,
                Metadata:  item.Media.Meta,
            }
        }
        
        return event, nil
    }
    
    // Object format - handle as before
    // ... existing code ...
}
```

### 3. Update MediaTracker to Handle Full Playlists

**File: `/home/user/Documents/daz/internal/plugins/mediatracker/plugin.go`**

Update the `handlePlaylistEvent` function to properly process full playlist arrays:

```go
func (p *Plugin) handlePlaylistEvent(event framework.Event) error {
    // Check if it's a full playlist array event
    if playlistArray, ok := event.(*framework.PlaylistArrayEvent); ok {
        log.Printf("[MediaTracker] Processing full playlist with %d items", len(playlistArray.Items))
        
        // Process all items in batches to avoid overwhelming the database
        batchSize := 50
        for i := 0; i < len(playlistArray.Items); i += batchSize {
            end := i + batchSize
            if end > len(playlistArray.Items) {
                end = len(playlistArray.Items)
            }
            
            batch := playlistArray.Items[i:end]
            for _, item := range batch {
                // Extract metadata if needed
                var metadata interface{}
                if item.Metadata != nil {
                    metadata = item.Metadata
                }
                
                // Add to library
                if err := p.addToLibrary(
                    item.MediaID,
                    item.MediaType,
                    item.Title,
                    item.Duration,
                    item.QueuedBy,
                    p.config.Channel,
                    metadata,
                ); err != nil {
                    log.Printf("[MediaTracker] Failed to add playlist item to library: %v", err)
                }
                
                // Also update the queue table if needed
                if err := p.insertQueueItem(p.config.Channel, &framework.QueueItem{
                    Position:  item.Position,
                    MediaID:   item.MediaID,
                    MediaType: item.MediaType,
                    Title:     item.Title,
                    Duration:  item.Duration,
                    QueuedBy:  item.QueuedBy,
                    QueuedAt:  time.Now().Unix(),
                }); err != nil {
                    log.Printf("[MediaTracker] Failed to update queue: %v", err)
                }
            }
        }
        
        return nil
    }
    
    // Handle other playlist event types (add/remove/move)
    // ... existing queue update handling ...
    
    return nil
}
```

### 4. Optimize Bulk Inserts

For better performance with large playlists, consider implementing a bulk insert method:

```go
func (p *Plugin) bulkAddToLibrary(items []framework.PlaylistItem) error {
    if len(items) == 0 {
        return nil
    }
    
    // Build bulk insert query
    valueStrings := make([]string, 0, len(items))
    valueArgs := make([]framework.SQLParam, 0, len(items)*9)
    
    for i, item := range items {
        valueStrings = append(valueStrings, fmt.Sprintf(
            "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
            i*9+1, i*9+2, i*9+3, i*9+4, i*9+5, i*9+6, i*9+7, i*9+8, i*9+9,
        ))
        
        url := constructMediaURL(item.MediaID, item.MediaType)
        valueArgs = append(valueArgs,
            framework.SQLParam{Value: url},
            framework.SQLParam{Value: item.MediaID},
            framework.SQLParam{Value: item.MediaType},
            framework.SQLParam{Value: item.Title},
            framework.SQLParam{Value: item.Duration},
            framework.SQLParam{Value: time.Now()},
            framework.SQLParam{Value: item.QueuedBy},
            framework.SQLParam{Value: p.config.Channel},
            framework.SQLParam{Value: item.Metadata},
        )
    }
    
    query := fmt.Sprintf(`
        INSERT INTO daz_mediatracker_library 
        (url, media_id, media_type, title, duration, first_seen, added_by, channel, metadata)
        VALUES %s
        ON CONFLICT (url) DO UPDATE SET
            last_played = CASE 
                WHEN EXCLUDED.channel = daz_mediatracker_library.channel 
                THEN CURRENT_TIMESTAMP 
                ELSE daz_mediatracker_library.last_played 
            END
    `, strings.Join(valueStrings, ","))
    
    return p.eventBus.Exec(query, valueArgs...)
}
```

### 5. Event Routing

Ensure the event bus properly routes playlist events:

**File: `/home/user/Documents/daz/internal/core/plugin.go`**

The core plugin should broadcast playlist events with the appropriate type so the MediaTracker can handle them.

## Benefits

1. **Complete Library Population** - Captures all media immediately when joining a channel
2. **Redundancy** - If individual updates are missed, the full playlist ensures completeness
3. **Performance** - Bulk inserts are more efficient than individual inserts
4. **Metadata Preservation** - Captures all metadata sent with the playlist

## Testing

1. Start the bot and join a channel where you have view permissions
2. Check logs for "Processing full playlist with X items"
3. Query the database: `SELECT COUNT(*) FROM daz_mediatracker_library;`
4. Verify all playlist items are recorded with correct URLs

## Conclusion

By implementing both recording methods, we ensure:
- Immediate population of the library when joining a channel
- Continuous updates as new media is added
- No missed media items
- Complete URL tracking for all media in the channel