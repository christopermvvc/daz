# Gallery Feature Implementation

## Overview
Implementing a comprehensive image gallery system for Daz bot, inspired by dazza's gallery feature. The system automatically collects images from chat, manages galleries per user, includes health monitoring, and provides web-based viewing.

## Project Status
- **Start Date**: 2025-08-07
- **Target Completion**: 2 weeks
- **Current Phase**: Database Schema Design

## Goals

### Primary Goals (Week 1)
- [x] Research daz architecture and plugin system
- [x] Analyze dazza's gallery implementation
- [x] Create implementation plan
- [ ] Design and implement database schema
- [ ] Create gallery plugin with basic functionality
- [ ] Implement image detection from chat
- [ ] Add gallery HTML generation
- [ ] Create basic gallery commands

### Secondary Goals (Week 2)
- [ ] Implement health monitoring system (3-strike policy)
- [ ] Add gallery lock/unlock functionality
- [ ] Implement image sharing tracking
- [ ] Add automatic pruning (25 image limit)
- [ ] Create comprehensive tests
- [ ] Performance optimization

### Future Enhancements
- [ ] REST API endpoints
- [ ] WebSocket real-time updates
- [ ] Batch import from Pastebin
- [ ] Advanced moderation features
- [ ] CDN integration

## Architecture

### Database Schema
```sql
-- Gallery images table
daz_gallery_images:
  - id (BIGSERIAL PRIMARY KEY)
  - username (VARCHAR)
  - url (TEXT)
  - channel (VARCHAR)
  - posted_at (TIMESTAMP)
  - is_active (BOOLEAN)
  - health tracking fields
  - sharing metadata

-- Gallery locks table
daz_gallery_locks:
  - username (PRIMARY KEY)
  - is_locked (BOOLEAN)
  - locked_at (TIMESTAMP)
  - locked_by (VARCHAR)
```

### Plugin Structure
```
internal/plugins/gallery/
├── plugin.go          # Main plugin implementation
├── detector.go        # Image URL detection
├── health.go         # Health monitoring system
├── store.go          # Database operations
├── generator.go      # HTML generation
├── commands.go       # Command handlers
└── plugin_test.go    # Tests
```

### Key Components

1. **Image Detection**
   - Extract URLs from chat messages
   - Validate image formats (jpg, png, gif, webp, etc.)
   - Store with metadata

2. **Health Monitoring**
   - Concurrent health checks using goroutines
   - 3-strike system before marking as dead
   - Exponential backoff for rechecks
   - Automatic recovery mechanism

3. **Gallery Generation**
   - Static HTML with cyberpunk theme
   - Per-user galleries
   - Lock status indicators
   - 25 image limit with auto-pruning

4. **Commands**
   - `!gallery` - Get gallery link
   - `!gallery lock` - Lock gallery
   - `!gallery unlock` - Unlock gallery
   - `!gallery_check` - Manual health check

## Implementation Progress

### Phase 1: Database & Foundation ✅
- [x] Research completed
- [x] Implementation plan created
- [x] SQL migration file (027_gallery_system.sql)
- [x] Basic plugin structure

### Phase 2: Core Functionality ✅
- [x] Plugin initialization
- [x] Event subscriptions
- [x] Image detection logic
- [x] Database operations

### Phase 3: Gallery Generation ✅
- [x] HTML template
- [x] CSS styling (cyberpunk theme)
- [x] Static file generation
- [x] Public directory structure

### Phase 4: Health Monitoring ✅
- [x] Health check worker
- [x] Exponential backoff logic
- [x] Recovery mechanism
- [x] Concurrent checking

### Phase 5: Commands ✅
- [x] Command registration
- [x] Command handlers
- [x] Response formatting
- [x] Permission checks

### Phase 6: Testing & Deployment 🚧
- [ ] Unit tests (skipped for MVP)
- [ ] Integration tests (skipped for MVP)
- [x] Linting and formatting
- [ ] Deployment and live testing

## Files Created

1. `/scripts/sql/027_gallery_system.sql` - Database schema
2. `/internal/plugins/gallery/plugin.go` - Main plugin implementation
3. `/internal/plugins/gallery/detector.go` - Image URL detection
4. `/internal/plugins/gallery/store.go` - Database operations
5. `/internal/plugins/gallery/health.go` - Health monitoring
6. `/internal/plugins/gallery/generator.go` - HTML generation

## Next Steps for Deployment

1. Apply SQL migration:
   ```bash
   psql -U daz_user -d daz_db -f scripts/sql/027_gallery_system.sql
   ```

2. Build the binary:
   ```bash
   make build
   ```

3. Deploy:
   ```bash
   sudo ./scripts/deploy.sh systemd
   ```

4. Create gallery output directory:
   ```bash
   sudo mkdir -p /var/www/daz/galleries
   sudo chown user:user /var/www/daz/galleries
   ```

5. Test commands:
   - `!gallery` - Get gallery link
   - `!gallery_lock` - Lock gallery
   - `!gallery_unlock` - Unlock gallery
   - `!gallery_check` - Manual health check (admin only)

## Technical Decisions

1. **PostgreSQL over SQLite**: Leverage existing daz database
2. **Static HTML first**: Simple, immediate results
3. **Goroutines for concurrency**: Idiomatic Go for health checks
4. **Event-driven architecture**: Integrate with daz EventBus
5. **Reuse SQL plugin**: Don't reinvent database connectivity

## Code Standards

- Concrete types, not interface{}
- Channels for synchronization
- Early returns for readability
- Comprehensive error handling
- Godoc for all exported symbols
- Table-driven tests

## Testing Strategy

1. **Unit Tests**
   - Image detection logic
   - URL validation
   - Health check logic
   - HTML generation

2. **Integration Tests**
   - Database operations
   - Event handling
   - Command processing

3. **Manual Testing**
   - Deploy to test environment
   - Verify gallery generation
   - Test all commands
   - Check health monitoring

## Deployment

1. Build binary: `make build`
2. Deploy: `sudo ./scripts/deploy.sh systemd`
3. Verify: Check systemd logs
4. Test: Use bot commands in chat

## Issues & Blockers

- None currently

## Notes

- Gallery HTML will be served separately (not through bot)
- Consider nginx for serving static galleries
- May need CORS configuration for API endpoints
- Health checks should respect rate limits

## References

- Dazza gallery implementation: `/home/seg/Documents/dazza/src/`
- Daz plugin examples: `/home/seg/Documents/daz/daz/internal/plugins/`
- EventBus documentation: `docs/api.md`