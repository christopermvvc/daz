# Postgres Performance Notes

This deployment profile targets low-resource hosts (for example 1 vCPU / ~1 GB RAM) where command responsiveness matters more than maximum ingest throughput.

## Baseline Settings

Start with these PostgreSQL settings and tune from there:

- `shared_buffers = 64MB`
- `work_mem = 2MB`
- `effective_cache_size = 256MB`
- `max_connections = 30` (or lower if the host is memory-constrained)

Apply in `postgresql.conf` (or equivalent managed service settings), then restart PostgreSQL.

## pg_stat_statements

Enable query visibility:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Recommended `postgresql.conf` entries:

- `shared_preload_libraries = 'pg_stat_statements'`
- `pg_stat_statements.max = 5000`
- `pg_stat_statements.track = all`

After restart, inspect top queries:

```sql
SELECT query, calls, total_exec_time, mean_exec_time
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;
```

## Daz-Specific Notes

- Keep `plugins.sql.database.max_connections` conservative on small hosts.
- Prefer command/critical lane capacity over background lane capacity.
- Use `plugin.request.command` and SQL request lanes for interactive command paths.
- If ingestion spikes at startup, monitor queue/lane metrics before raising connection counts.
