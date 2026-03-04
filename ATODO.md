# Ingestion Backpressure Work TODO

## WebSocket UX Bridge (Phase 1)
- [x] Add `wsbridge` plugin startup/config wiring.
- [x] Define stable inbound/outbound websocket envelope and explicit subscriptions.
- [x] Support authenticated command dispatch (`command.execute`) with correlation IDs.
- [x] Add per-client heartbeat + inbound rate limiting.
- [x] Add baseline privacy filtering for PM/plugin responses by session user.
- [x] Add focused unit tests for matcher/auth/channel/rate-limit behavior.
- [x] Run full `go test ./...` and push.

## Scope
- [x] Confirm target scope is everything except stale-guard bypass (`#5`).

## Work Items
- [x] 1. Make `daz_core_events` logging asynchronous and batched.
- [x] 2. Add startup load shedding/staggering for `mediatracker` and `usertracker`.
- [x] 3. Split SQL lanes/pools for critical command traffic vs background logging traffic.
- [x] 4. Add command fast-path behavior so commands are not delayed behind slow subscribers.
- [x] 6. Drop/deprioritize non-critical events under pressure.

## Verification
- [x] Add/update unit tests for new behavior.
- [x] Run focused package tests.
- [x] Run full test suite (`go test ./...`) if feasible.

## Next Phase: VPS Stability Remediation (2026-03-03 findings)
- [x] Protect command path with a dedicated event bus lane for `command.*` and admin flows.
- [x] Propagate high-priority metadata into command-originated SQL requests.
- [x] Split SQL execution into critical vs background lanes with bounded background queueing.
- [x] Add non-critical SQL/background shedding and coalescing under pressure.
- [x] Debounce reconnect/startup sync so userlist+playlist initialization runs once per reconnect window.
- [x] Prevent overlapping `mediatracker` staged-ingestion runs per channel/session.
- [x] Gate help publish jobs with content-hash checks and a minimum publish interval.
- [x] Gate gallery pushes with content-hash checks, minimum interval, and retry backoff.
- [x] Add migration for `daz_ollama_responses` and `daz_ollama_rate_limits`.
- [x] Add startup schema sanity checks for required Ollama tables.
- [x] Tune event bus worker reservations and plugin background concurrency for 1-vCPU deployment.
- [x] Add pressure tests proving command delivery during reconnect/startup burst traffic.
- [x] Add integration tests for reconnect burst dedupe and staged playlist behavior.
- [x] Run targeted tests (`eventbus`, `eventfilter`, `sql`, `mediatracker`, `usertracker`) plus `go test ./...`.
- [x] Push phased commits to `origin` (lane/debounce first, then jobs/migrations/tuning).
- [ ] Validate on VPS with metrics: `!update` reach rate, SQL timeout count, dropped-event count.

## Performance Follow-Up (selected: 1,3,4,5,6,7,8,9,10)
- [x] 1. Tighten SQL connection/worker budget defaults for constrained hosts.
- [x] 3. Add Postgres tuning guidance + startup diagnostics for low-memory deployments.
- [x] 4. Reduce retry amplification for non-critical write paths.
- [x] 5. Move SQL query/exec hot paths to direct `pgxpool` usage.
- [x] 6. Switch `daz_core_events` batch ingestion to `COPY` with safe fallback.
- [x] 7. Split `plugin.request` routing into classed lanes (command/default/background).
- [x] 8. Bound EventBus handler fanout via admission-before-spawn behavior.
- [x] 9. Add `pg_stat_statements` bootstrap + top-query visibility hooks.
- [x] 10. Add queue depth and lane occupancy observability metrics.

## Follow-Up Verification
- [x] Add/extend tests for new SQL/EventBus routing and ingestion behavior.
- [x] Run focused tests (`./pkg/eventbus`, `./internal/plugins/sql`, `./internal/framework`).
- [x] Run full suite (`go test ./...`) and push.
