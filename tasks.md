# Remediation Tasks

## Codebase Interpretation (Snapshot)
- Core plugin manages Cytube WebSocket connections and emits events to the EventBus; the EventBus fans out to plugins using priority queues and subscription patterns.
- SQL, Retry, EventFilter, UserTracker, MediaTracker, Analytics, and command plugins are managed by the PluginManager and communicate solely through EventBus events.
- SQL plugin handles logging and synchronous query/exec requests through `plugin.request`, then conditionally subscribes to configured logger rules after DB connect.

## Test Suite Vetting

### Safe to run offline
- Most unit tests under `internal/` and `pkg/` use mocks, `httptest`, or local fixtures.
- `internal/plugins/sql/*_test.go` uses mocks or `sqlmock` without a real DB.
- `pkg/cytube/socketio_test.go` and `pkg/cytube/parser_test.go` are pure unit tests.

### Requires network, DB, or longer runtime
- `internal/plugins/retry/integration_test.go` (build tag `integration`, requires Postgres and long sleeps).
- `pkg/cytube/discovery_test.go` -> `TestDiscoverServer_Integration` hits the real Cytube server (requires `CYTUBE_INTEGRATION=1`, skips with `-short`).
- `pkg/eventbus/priority_integration_test.go` is time/scheduling sensitive (skips with `-short`).

### Benchmarks
- `pkg/eventbus/priority_queue_bench_test.go` (benchmarks only).

### Recommended default command (offline)
- `go test ./... -short`
- Integration tests require explicit env/build tags.

## Prioritized Remediation Tasks
- [x] P0: Make WebSocket client creation testable without live discovery (add injectable discovery or `NewWebSocketClientWithServerURL` and update `TestNewWebSocketClient`).
- [x] P0: Ensure network integration tests are opt-in (move `TestDiscoverServer_Integration` behind build tags or add explicit flags; keep `-short` behavior).
- [x] P1: Reconcile `docs/daz-chatbot-architecture.md` logger-rule description with SQL plugin behavior (docs say rules are stored but unused; code subscribes after DB connect).
- [x] P1: Stabilize or gate time-sensitive eventbus priority tests (reduce sleeps, make deterministic, or guard with `-short`).
- [x] P2: Replace placeholder gallery store tests with sqlmock or a dedicated test DB harness.
- [x] P2: Add a test matrix section to README/docs clarifying `-short`, build tags, and integration requirements.

## Issue Inventory (Prioritized)
- [x] P1: Replace `log.Printf` usage in core/runtime paths with `internal/logger` (e.g., `pkg/cytube/parser.go`, `internal/framework/request_helper.go`).
- [ ] P1: Decide on logging approach for SQL logger middleware, which currently uses `log.Printf` in runtime paths (`internal/plugins/sql/logger_middleware.go`).
- [ ] P2: Replace `log.Printf` on config file close errors with `internal/logger` or suppress in `internal/config/config.go`.
- [ ] P2: Audit vendored Cytube `util.js` TODOs for relevance (`cytube_src/util.js`, `examples/cytube_src/util.js`).
