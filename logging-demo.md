# DAZ Logging Improvements Demo

## Before (Old Logging)
```
[SQL Plugin] Received plugin.request event
[SQL Plugin] Routing to SQL exec handler
[EventFilter] Received chat message: 'hello' from user: testuser (prefix: '!')
[EventFilter] Received PM: 'test' from user: someuser
[SQL Plugin] Received plugin.request event
[SQL Plugin] Routing to SQL query handler
[EventFilter] Detected command: help with args: [] from user: testuser (PM: false)
[SQL Plugin] Received plugin.request event
```
*Every single request and message was logged, creating massive spam*

## After - Normal Mode (Default)
```
11:15:58 [INFO ] SQL: Initialized with 3 logger rules
11:15:58 [INFO ] EventFilter: Initialized with command prefix: !
11:15:58 [INFO ] SQL: Started (connecting to database in background)
11:15:58 [INFO ] SQL: Connected to database successfully
11:15:58 [INFO ] EventFilter: Started unified event filtering and command routing
Bot is running! Press Ctrl+C to stop.
```
*Only important startup/shutdown events shown - no spam!*

## After - Verbose Mode (`-verbose` flag)
```
11:15:58 [INFO ] SQL: Connected to database successfully
11:15:58 [DEBUG] SQL: Subscribing to event handlers...
11:15:58 [DEBUG] SQL: Subscribed to plugin.request
11:15:58 [DEBUG] SQL: Received plugin.request event
11:15:58 [DEBUG] SQL: Routing to SQL exec handler
11:15:58 [DEBUG] EventFilter: Received chat message: 'hello' from user: testuser
11:15:58 [DEBUG] EventFilter: Detected command: help with args: []
```
*Debug information available when needed for troubleshooting*

## Usage

### Normal mode (clean output):
```bash
./scripts/run-console.sh
```

### Verbose mode (debug output):
```bash
./scripts/run-console.sh -verbose
```

## Features
- ✅ Color-coded log levels (INFO=green, DEBUG=gray, WARN=yellow, ERROR=red)
- ✅ Bold plugin names for easy scanning
- ✅ Clean timestamps
- ✅ Works with run-console.sh (colors preserved)
- ✅ Spam eliminated in normal mode
- ✅ Full debug info available with -verbose flag