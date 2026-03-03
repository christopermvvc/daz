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
