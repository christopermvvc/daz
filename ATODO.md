# Ingestion Backpressure Work TODO

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
