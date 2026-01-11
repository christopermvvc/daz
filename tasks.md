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
- [x] P0: Gallery generator `resetGitState` can wipe unintended repos if misconfigured output path points at a real repo (`internal/plugins/gallery/generator.go`).
- [x] P1: EventFilter admin file read failure leaves no admins and blocks admin-only commands (`internal/plugins/eventfilter/plugin.go`).
- [x] P1: Greeter PM send ignores event bus errors, risking silent message loss (`internal/plugins/greeter/plugin.go`).
- [x] P1: `scripts/run-console.sh` `.env` parsing is unsafe for quoted values/spaces (`scripts/run-console.sh`).
- [x] P2: Missing config file path is silently ignored; typos run with defaults (`internal/config/config.go`).

## Issue Inventory (Prioritized - Feature/Command Scan)
- [x] P0: EventFilter admin user map is not initialized before loading (`internal/plugins/eventfilter/plugin.go`).
- [x] P1: Command plugins ignore event bus registration/subscription failures (`internal/plugins/commands/fortune/plugin.go`, `internal/plugins/commands/random/plugin.go`, `internal/plugins/commands/weather/plugin.go`).
- [x] P1: Weather command uses unchecked type assertions on external JSON (`internal/plugins/commands/weather/plugin.go`).
- [x] P1: Seen/Greeter commands assume `req.Data.Command` is non-nil (`internal/plugins/commands/seen/plugin.go`, `internal/plugins/greeter/plugin.go`).
- [x] P1: MediaTracker uptime calculation uses `time.Since(time.Now())` (`internal/plugins/mediatracker/plugin.go`).
- [x] P1: Analytics uptime calculation uses `time.Since(time.Now())` (`internal/plugins/analytics/plugin.go`).
- [x] P1: MediaTracker reads `currentMedia` without locking (`internal/plugins/mediatracker/plugin.go`).
- [x] P1: Analytics stats response lacks command routing context (`internal/plugins/analytics/plugin.go`).
- [x] P2: Gallery HTML generator sleeps before checking shutdown context (`internal/plugins/gallery/plugin.go`).
- [x] P1: Ollama forces enabled state in init, ignoring config (`internal/plugins/ollama/plugin.go`).

## Issue Inventory (Prioritized - Latest Rescan)
- [x] P1: Event discovery registers `pgx` driver inside `Run`, risking duplicate registration panic on repeated invocations (`cmd/daz/event_discovery.go`).
- [x] P2: Event metadata priority warnings should use `internal/logger` instead of `fmt.Printf` (`internal/framework/event_metadata.go`).

## Issue Inventory (Prioritized - Command/Gallery Security)
- [x] P1: EventFilter defines `default_cooldown` but never enforces it, allowing command spam (`internal/plugins/eventfilter/plugin.go`).
- [x] P1: Gallery output path should be absolute and validated before write (`internal/plugins/gallery/generator.go`).
- [x] P1: Gallery git operations should require marker file if a git repo already exists (`internal/plugins/gallery/generator.go`).
- [x] P1: Tell plugin should fail fast on subscription/registration failures (`internal/plugins/commands/tell/plugin.go`).
- [x] P1: Gallery image detector should reject private IP literals (SSRF hardening) (`internal/plugins/gallery/detector.go`).
- [x] P2: Command parsing does not support quoted arguments; multi-word params must be manually joined (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: EventFilter cooldown map should prune old entries to avoid growth (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: Gallery hostname checks should resolve DNS to block private IPs (SSRF hardening) (`internal/plugins/gallery/detector.go`).
- [x] P2: Gallery health checks should skip unsafe URLs before HTTP probes (`internal/plugins/gallery/health.go`).
- [x] P2: EventFilter should persist command failures to history for auditing (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: EventFilter should avoid logging args for failed commands (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: EventFilter should periodically prune cooldowns via ticker (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: EventFilter should enforce command length limits before parsing (`internal/plugins/eventfilter/plugin.go`).
- [x] P2: EventFilter should cap args count and sanitize control chars before logging (`internal/plugins/eventfilter/plugin.go`).

## Prioritized Stability Task List

### P0 – Gallery Safety & Data Integrity
- [x] Validate HTML output path is safe and absolute; enforce marker file for git operations in `internal/plugins/gallery/generator.go`.
- [x] Harden gallery health checker against unsafe URLs and SSRF in `internal/plugins/gallery/health.go` and `internal/plugins/gallery/detector.go`.
- [x] Ensure health checks skip unsafe URLs before HTTP requests; log and mark images appropriately.

### P1 – Gallery Worker Lifecycle
- [x] Ensure gallery background workers respect context cancellation and don’t overlap on rapid ticks in `internal/plugins/gallery/plugin.go`.
- [x] Add guardrails for long-running HTML generation to stop on shutdown in `internal/plugins/gallery/generator.go`.

### P1 – EventBus Handler Safety
- [x] Add panic recovery around subscriber goroutines in `pkg/eventbus/eventbus.go`.
- [x] Review EventBus shutdown sequence for safe drain/close ordering to avoid dropped handlers or response races.

### P1 – EventFilter/Core Stability
- [x] Verify eventfilter cooldown pruning, admin loading, and command routing correctness in `internal/plugins/eventfilter/plugin.go`.
- [x] Confirm core room manager reconnection doesn’t spawn duplicate handlers or leave stale channels in `internal/core/room_manager.go`.

### P2 – Functional Gap (Not Stability)
- [x] Implement queue “move” in MediaTracker (`internal/plugins/mediatracker/plugin.go`) if needed.
