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
- [x] P1: Decide on logging approach for SQL logger middleware, which currently uses `log.Printf` in runtime paths (`internal/plugins/sql/logger_middleware.go`).
- [x] P2: Replace `log.Printf` on config file close errors with `internal/logger` or suppress in `internal/config/config.go`.
- [x] P2: Audit vendored Cytube `util.js` TODOs for relevance (`cytube_src/util.js`, `examples/cytube_src/util.js`).
  - Notes: Both TODOs appear in vendored Cytube sources (`highlightsMe` match semantics and emote UI refactor). No direct impact on daz runtime; keep for upstream sync.

## Issue Inventory (Prioritized - Fresh Scan)
- [x] P0: EventBus nil metadata dereference risk in `SendWithMetadata` and `Request` (`pkg/eventbus/eventbus.go`).
- [x] P0: EventBus response channel cleanup may race with `DeliverResponse`, risking send-on-closed panic (`pkg/eventbus/eventbus.go`).
- [x] P1: EventBus spawns a goroutine per subscriber per event; could exhaust resources under load (`pkg/eventbus/eventbus.go`).
- [x] P1: EventBus queue growth ignores configured buffer sizes and can be unbounded (`pkg/eventbus/eventbus.go`, `pkg/eventbus/priority_queue.go`).
- [x] P0: `processRoomEvents` reads from `EventChan` without handling close, risking nil event panics (`internal/core/room_manager.go`).
- [x] P1: Reconnect path starts a new `processRoomEvents` goroutine on each connect (possible duplicate handlers) (`internal/core/room_manager.go`).
- [x] P1: Config validation requires credentials even though anonymous joins are supported (`internal/config/config.go`, `internal/core/room_manager.go`).
- [x] P0: WebSocket client debug logging may include login payloads (credentials) when debug enabled (`pkg/cytube/websocket_client.go`).
- [x] P1: Analytics plugin channel discovery subscribes repeatedly without unsubscribe (possible handler leak) (`internal/plugins/analytics/plugin.go`).

## Issue Inventory (Prioritized - Rescan)
- [x] P0: EventFilter time unit mismatch between chat vs PM timestamps can drop or misclassify commands (`internal/plugins/eventfilter/plugin.go`, `internal/core/plugin.go`).
- [x] P1: EventFilter admin file read failure leaves no admins and blocks admin-only commands (`internal/plugins/eventfilter/plugin.go`).

- [ ] P1: Greeter PM send ignores event bus errors, risking silent message loss (`internal/plugins/greeter/plugin.go`).
- [ ] P1: `scripts/run-console.sh` `.env` parsing is unsafe for quoted values/spaces (`scripts/run-console.sh`).
- [ ] P2: Missing config file path is silently ignored; typos run with defaults (`internal/config/config.go`).
