# Daz Database Schema

This document outlines the PostgreSQL database requirements for the Daz modular chat bot. The schema is managed through a combination of automatic initialization in plugin code and sequential SQL migration scripts.

## Overview

Daz requires PostgreSQL 14+. The bot uses a modular schema where each plugin manages its own tables. Persistence is primarily handled by the `sql` plugin, which provides a shared database connection pool to other plugins.

- Auto-initialization: Most plugins create their required tables automatically on startup if they do not exist.
- SQL scripts: Complex features (Gallery, Retry, Ollama) use sequential SQL scripts for setup and migrations.
- Role: The database user `daz_user` is referenced in the migration scripts for grants.

## Core SQL Tables

Managed by the `sql` plugin (`internal/plugins/sql/plugin.go`).

- `daz_core_events`: Raw log of all broadcasted events including chat, media changes, and PMs.
- `daz_chat_log`: Optimized table for public chat messages.
- `daz_user_activity`: Tracks user-specific events like joins, leaves, and rank changes.
- `daz_plugin_logs`: Generic storage for plugin-specific log data.
- `daz_private_messages`: Dedicated log for private messages sent to/from the bot.

## Plugin-Specific Tables

### Gallery Plugin

Managed via `internal/plugins/gallery/store.go` and migration scripts `027` through `030`.

- Tables:
  - `daz_gallery_images`: Primary image storage with health monitoring fields.
  - `daz_gallery_locks`: User-level locks to prevent image collection.
  - `daz_gallery_stats`: Aggregated statistics per user/channel.
  - `daz_gallery_health_log`: History of automated image availability checks.
- View: `daz_gallery_view`.
- Stored functions: `add_gallery_image`, `mark_image_for_health_check`, `restore_dead_image`, `update_dead_image_recovery`.

### Retry & Resilience

Managed via `scripts/sql/025_retry_persistence.sql`.

- Table: `daz_retry_queue`.
- View: `daz_retry_queue_stats`.
- Stored functions: `acquire_retry_batch`, `complete_retry`, `fail_retry`, `cleanup_old_retries`.
- Trigger: `update_retry_stats_trigger` updates plugin stats on retry status changes.

### Plugin Statistics

Managed via `scripts/sql/026_plugin_stats.sql`.

- Table: `daz_plugin_stats`.

### MediaTracker Plugin

Managed via `internal/plugins/mediatracker/plugin.go`.

- Tables:
  - `daz_mediatracker_plays`
  - `daz_mediatracker_queue`
  - `daz_mediatracker_stats`
  - `daz_mediatracker_library`

### Analytics Plugin

Managed via `internal/plugins/analytics/plugin.go`.

- Tables:
  - `daz_analytics_hourly`
  - `daz_analytics_daily`
  - `daz_analytics_user_stats`

### UserTracker Plugin

Managed via `internal/plugins/usertracker/plugin.go`.

- Tables:
  - `daz_user_tracker_sessions`
  - `daz_user_tracker_history`
- Stored function: `upsert_user_session`.

### Greeter Plugin

Managed via `internal/plugins/greeter/database.go`.

- Tables:
  - `daz_greeter_history`
  - `daz_greeter_preferences`
  - `daz_greeter_state`

### Tell Plugin

Managed via `internal/plugins/commands/tell/plugin.go`.

- Table: `daz_tell_messages`.

### Ollama Plugin

Managed via `scripts/sql/031_ollama_responses.sql`.

- Tables:
  - `daz_ollama_responses`
  - `daz_ollama_rate_limits`

### EventFilter Plugin

Managed via `internal/plugins/eventfilter/plugin.go`.

- Tables:
  - `daz_eventfilter_rules`
  - `daz_eventfilter_commands`
  - `daz_eventfilter_history`

## SQL Scripts List

These scripts live in `scripts/sql/`:

1. `025_retry_persistence.sql`: Retry queue system.
2. `026_plugin_stats.sql`: Plugin performance tracking.
3. `027_gallery_system.sql`: Initial Gallery tables and functions.
4. `028_fix_gallery_race_condition.sql`: Advisory locks for gallery inserts.
5. `029_graveyard_recovery_procedure.sql`: Dead-image recovery procedures.
6. `030_fix_gallery_duplicates.sql`: URL uniqueness per user across channels.
7. `031_ollama_responses.sql`: Ollama response cache and rate limiting.

Maintenance:
- `scripts/cleanup-stale-sessions.sql`: Manual cleanup for duplicate active user sessions.

## Roles and Permissions

The migration scripts grant privileges to `daz_user`:

- Tables/views: `SELECT`, `INSERT`, `UPDATE`, `DELETE`.
- Sequences: `USAGE`.
- Functions: `EXECUTE`.

Example if you need to grant permissions manually:

```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO daz_user;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO daz_user;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO daz_user;
```
