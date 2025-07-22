# Userlist Event Handling Implementation

## Overview
This document describes the implementation of proper userlist event handling in the DAZ bot system. The solution enables plugins to differentiate between bulk user updates (from userlist events) and individual user join/leave events.

## Problem Statement
When reconnecting to a CyTube channel, the server sends a `userlist` event containing all current users. Previously, this was converted to individual `addUser` events without any way for plugins to:
- Know that these events came from a bulk update
- Clean up stale user data before processing the new state
- Optimize database operations for bulk updates

## Solution Design

### Approach: Event Bracketing with Metadata
The implemented solution uses a combination of:
1. **Bracket events** (`userlist.start` and `userlist.end`) to mark the boundaries of bulk updates
2. **Metadata enrichment** on converted `addUser` events to indicate they came from a userlist
3. **Backward compatibility** with existing plugins that only handle individual user events

### Implementation Details

#### 1. Room Manager Changes (`internal/core/room_manager.go`)
- Enhanced `processUserListEvent()` to broadcast bracket events:
  - `cytube.event.userlist.start` - Signals the beginning of bulk user updates
  - `cytube.event.userlist.end` - Signals the completion of bulk user updates
- Added metadata to converted `addUser` events:
  - `from_userlist: "true"` - Indicates the event came from a userlist
  - `room_id` - Identifies which room the event is for
- Included user count in bracket events for tracking

#### 2. UserTracker Plugin Changes (`internal/plugins/usertracker/plugin.go`)
- Added new event handlers:
  - `handleUserListStart()` - Marks existing users as inactive before processing new state
  - `handleUserListEnd()` - Records userlist sync completion in history
- Added state tracking:
  - `processingUserlist` - Boolean flag indicating bulk update is in progress
  - `currentChannel` - Tracks which channel is being updated
- Database operations:
  - On userlist start: Deactivates all active users for the channel
  - During processing: Adds/reactivates users as they appear in the list
  - On userlist end: Records sync event in history table

### Benefits

1. **Data Integrity**: Stale users are properly marked as inactive when not in the new userlist
2. **Performance**: Plugins can batch database operations during bulk updates
3. **Backward Compatibility**: Existing plugins continue to work without modification
4. **Flexibility**: Plugins can choose their level of sophistication:
   - Simple plugins: Just handle addUser events as before
   - Advanced plugins: Use bracket events for optimized processing
5. **Debugging**: Clear event flow with proper logging and metadata

### Event Flow Example

```
1. Connection established to CyTube channel
2. Server sends userlist event with 50 users
3. Room Manager processes:
   → Broadcasts: cytube.event.userlist.start (count: 50)
   → For each user in list:
     → Broadcasts: cytube.event.addUser (with from_userlist=true)
   → Broadcasts: cytube.event.userlist.end (count: 50)
4. UserTracker plugin:
   → On start: Marks all existing users inactive
   → On each addUser: Creates/updates user record
   → On end: Records sync completion
```

### Testing
Comprehensive tests were added to verify:
- Userlist start event properly clears existing user state
- Users are correctly added during userlist processing
- Userlist end event completes the bulk update cycle
- Regular user joins (not from userlist) work as before
- Metadata is properly set on events

## Future Enhancements
- Add transaction support for bulk database operations
- Include timestamp of last sync in userlist.end event
- Add metrics for userlist processing time
- Consider adding a "userlist.error" event for failure scenarios