Below is a **full, detailed refactor plan** to make NeuraTrade robust (deadlock-resistant), keep everything **Go-first**, and still support an **actor-based + plugin-driven** architecture.

I’m going to assume your immediate pain is in the backend API’s “god services” (collector + processors) where shared state + goroutines + locks collide.

---

# 0) Target outcomes (definition of “robust”)

**Robust means:**

1. **No shared mutable state across subsystems** (or extremely limited + owned by one actor).
2. **Structured concurrency** (every goroutine is supervised + cancellable).
3. **Bounded queues** (backpressure) and **timeouts** on every IO.
4. **Idempotent commands** and **event-driven** flow (so retries don’t duplicate side effects).
5. **Observable** (metrics + logs + traces + deadlock/race detection in CI).

---

# 1) Architecture we will implement (Go-first)

## Planes

### Execution Plane (deterministic runtime)

* Actors: marketdata, strategy, risk, execution, portfolio, notification
* Event bus: in-process first (upgrade to Redis Streams/NATS later)
* State stores: DB + Redis behind interfaces
* Adapters: ccxt-service, telegram-service

### Control Plane (optional later)

* “Agentic operator” that reads events/metrics and issues safe commands
* **Not required** to solve deadlocks; add after stabilization

---

# 2) Folder structure (final target)

Do this **inside** `services/backend-api` first (lowest blast radius).

```text
services/backend-api/
  cmd/
    server/                       # wiring only: build graph, start supervisor

  internal/
    platform/                     # mechanics (no business rules)
      supervisor/                 # errgroup lifecycle; stop everything cleanly
      actor/                      # mailbox, router, backpressure, deadletters
      eventbus/                   # pub/sub; in-proc impl first
      breaker/ retry/ timeout/
      metrics/ logging/ tracing/

    ports/                        # app depends on these interfaces
      exchange.go                 # market data + trading gateway contract
      notifier.go                 # telegram/discord/webhook contract
      state.go                    # repositories/stores contracts
      eventbus.go                 # publish/subscribe contract
      plugin.go                   # plugin contract (registry/wasm/grpc)

    adapters/                     # infra implementations
      ccxt/                       # client bridge to services/ccxt-service
      telegram/
      redis/
      db/

    domain/                       # pure logic only (testable, no IO)
      marketdata/
      signals/
      risk/
      execution/
      portfolio/
      common/

    app/                          # actors + use-cases grouped by purpose
      bootstrap/                  # builds dependency graph; readiness/health
      marketdata/
        collector_actor.go
        symbol_registry_actor.go
        normalize.go
      strategy/
        strategy_actor.go
        features.go
      risk/
        risk_actor.go
      execution/
        execution_actor.go
        order_router_actor.go
      portfolio/
        portfolio_actor.go
      notify/
        notification_actor.go

  plugins/                        # plugin packages + manifests (Go-safe)
    manifests/
      strategy-dca.toml
      indicator-macd.toml
    builtin/                      # compiled-in plugins (MVP)
      strategies/
      indicators/
      risk_models/
    wasm/                         # later: *.wasm (wazero) if you choose
```

**Rule:** business code lives in `domain/` and `app/`. Everything concurrency-ish lives in `platform/`.

---

# 3) Core refactor principles (non-negotiable)

## A) No “naked goroutines”

All long-running loops must be started via `platform/supervisor` (errgroup + context cancellation).

## B) Actor = single writer of its own state

* Actors process messages sequentially.
* Anything like `map[string]*Worker`, `cache`, `readiness`, etc. becomes actor-owned.
* Other components interact via `Send(msg)` or via event bus.

## C) Never hold locks across IO

If you still need locks anywhere:

* lock -> read/update small state -> unlock
* then call network/DB outside lock

## D) Every IO has a timeout

* exchange calls
* redis
* db
* telegram
* plugin execution

---

# 4) Platform layer design (exact deliverables)

## 4.1 `platform/supervisor`

**Goal:** deterministic lifecycle, safe shutdown, no leaks.

Deliverables:

* `Run(ctx) error` for each component
* `supervisor.Group` wrapper around `errgroup.WithContext`
* global stop signal on first fatal error
* graceful shutdown timeout

Acceptance criteria:

* `SIGINT` stops all actors within N seconds
* no goroutine leak in tests (use goleak)

## 4.2 `platform/actor`

MVP capabilities:

* `Mailbox` is bounded (e.g., size 256/1024)
* overflow strategy: drop, block, or dead-letter queue
* message envelopes include:

  * `TraceID`
  * `Deadline`
  * `Reply chan` (optional, for request/response)
  * `Type`

Actor interface shape:

* `Run(ctx context.Context) error`
* `Send(msg Message) error`

## 4.3 `platform/eventbus`

Start with **in-process pub/sub**:

* `Publish(topic, event)`
* `Subscribe(topic) <-chan Event`
* support wildcard topics if you want (`market.*`)

Later upgrades (not required now):

* Redis Streams
* NATS JetStream
* Kafka

---

# 5) Ports & Adapters (interfaces that prevent spaghetti)

## 5.1 `ports/exchange.go`

Split into read vs trade:

* `MarketDataGateway`

  * `FetchTick(ctx, symbol) (Tick, error)`
  * `FetchOHLCV(ctx, symbol, tf, since) ([]Candle, error)`
  * `FetchOrderBook(ctx, symbol, depth) (OrderBook, error)`
  * (or stream subscription if ccxt-service supports WS)

* `TradingGateway`

  * `PlaceOrder(ctx, OrderRequest) (OrderResult, error)`
  * `CancelOrder(ctx, orderID) error`
  * `FetchOpenOrders(ctx, symbol) ...`

**Adapter:** `adapters/ccxt` calls your `services/ccxt-service`.

## 5.2 `ports/state.go`

Define repositories:

* `PositionsRepo`, `OrdersRepo`, `TradesRepo`
* `ConfigRepo` (feature flags, risk budgets)
* optional `OutboxRepo` for reliable events (later)

**Adapters:** `adapters/db`, `adapters/redis`

## 5.3 `ports/notifier.go`

* `Send(ctx, Notification) error`

**Adapter:** `adapters/telegram`

---

# 6) Actor set (what they do + messages + events)

You don’t need 20 actors on day one. Start with 5–7.

## 6.1 MarketDataCollectorActor (first priority)

**Owns:**

* symbol list / subscriptions
* worker map / per-exchange state
* last fetch timestamps
* retry/backoff state
* readiness state

**Inbound messages:**

* `StartExchange{ExchangeID}`
* `StopExchange{ExchangeID}`
* `UpdateSymbols{ExchangeID, Symbols[]}`
* `FetchNow{ExchangeID, Symbols[]}` (manual trigger)
* `SetRate{ExchangeID, Interval}`
* `HealthCheck{Reply chan Health}`

**Outbound events:**

* `MarketTick{ExchangeID, Symbol, Bid, Ask, Last, Ts}`
* `MarketCandle{...}`
* `CollectorDegraded{ExchangeID, Reason, Since}`
* `CollectorRecovered{ExchangeID}`
* `CollectorLag{ExchangeID, LagMs}`

**Deadlock killer change:**
Your “workers map + mutex + readiness channel + goroutines” becomes **one actor loop**.

## 6.2 StrategyActor

**Owns:**

* strategy configuration
* per-symbol rolling feature windows (if not in separate FeatureActor)
* last signal timestamp

**Subscribes to events:**

* `MarketTick` / `MarketCandle`

**Publishes:**

* `SignalProposed{StrategyID, Symbol, Side, Confidence, Metadata}`
* `SignalSkipped{Reason}`

## 6.3 RiskActor

**Subscribes:**

* `SignalProposed`
* `PositionUpdated`
* `PnLUpdated`
* `RiskConfigUpdated`

**Publishes:**

* `OrderIntentApproved{...}`
* `OrderIntentRejected{Reason}`
* `RiskLimitBreached{...}`

## 6.4 ExecutionActor

**Subscribes:**

* `OrderIntentApproved`

**Calls adapter:**

* `TradingGateway.PlaceOrder`

**Publishes:**

* `OrderPlaced`, `OrderRejected`, `OrderFilled` (from polling or callbacks)

## 6.5 PortfolioActor

**Subscribes:**

* `OrderFilled`, `OrderPlaced`, `TradeEvents`

**Writes state:**

* positions/trades/orders repositories

**Publishes:**

* `PositionUpdated`, `ExposureUpdated`

## 6.6 NotificationActor (optional early)

**Subscribes:**

* `RiskLimitBreached`, `OrderRejected`, `CollectorDegraded`

**Calls:** notifier

---

# 7) Plugin system (Go-safe, robust)

Your proposal’s “plugins/loader.go runtime load” is good, but **don’t use Go `.so`**.

## Phase A (MVP, fast): Builtin registry + manifests

* A plugin is a Go package registered at init or in a registry map.
* Manifest enables/disables + provides config.

Types:

* Indicator plugin: `(history) -> value`
* Strategy plugin: `(market events + state) -> signal`
* Risk model plugin: `(signal + portfolio) -> approve/reject`

This gives you “plugin feel” without deployment complexity.

## Phase B (Robust sandbox): WASM via wazero

* plugin compiled to wasm (Rust/TinyGo/etc)
* run with:

  * cpu timeouts
  * memory limits
  * no filesystem unless granted
* great for “untrusted” strategies or marketplace ideas

**Recommendation:** Implement Phase A first while stabilizing concurrency.

---

# 8) Migration plan (step-by-step, minimal breakage)

This is the “do it without rewriting everything at once” plan.

## Phase 1 — Stabilization foundations (must do first)

1. Add `platform/supervisor` and route all background jobs through it.
2. Add `platform/timeout`, `retry`, `breaker` (shared utilities).
3. Add `eventbus` in-proc implementation.
4. Add `ports/*` interfaces and wrap existing clients behind adapters (no logic changes yet).

**Exit criteria:**

* service starts/stops cleanly
* no goroutine leaks in tests
* all external calls have timeouts

## Phase 2 — Convert Collector to actor (highest ROI)

1. Create `app/marketdata/collector_actor.go`
2. Move worker map + readiness + periodic loop into actor state.
3. Replace direct calls from other services with:

   * actor messages OR
   * subscriptions to MarketTick events
4. Remove old shared locks and channels.

**Exit criteria:**

* collector runs without mutex-heavy code
* `-race` passes for collector tests
* can pause/resume exchange via message

## Phase 3 — Convert Signal Processor to actor

1. StrategyActor subscribes to Market events.
2. Domain logic moved to `domain/signals`.
3. Any pools become bounded worker pools owned by actor (or sequential for MVP).

**Exit criteria:**

* signal generation no longer touches collector internals
* strategies are deterministic + testable

## Phase 4 — Risk + Execution + Portfolio actors

1. Wire event chain: `SignalProposed -> Risk -> Execution -> Portfolio`
2. Ensure idempotency keys:

   * `SignalID`
   * `IntentID`
   * `OrderClientID`
3. Persist state updates via repos

**Exit criteria:**

* restart-safe (replay recent state)
* duplicate events don’t duplicate orders

## Phase 5 — Plugin MVP

1. Add registry + manifest loader (config only).
2. Convert 1 strategy + 1 indicator into “plugin modules”.

**Exit criteria:**

* enable/disable strategy via config
* strategy config reload safe (actor message)

## Phase 6 — Optional upgrades

* switch eventbus to Redis Streams/NATS
* add WASM plugins
* add agent control plane

---

# 9) Testing & safety gates (how we prevent regressions)

## Unit tests

* Domain logic: pure tests
* Actor behavior: feed messages/events, assert outputs

## Concurrency tests

* run `go test -race ./...`
* use `goleak` to catch goroutine leaks
* fuzz message ordering for actors (optional but powerful)

## Integration tests

* “happy path”: tick -> signal -> risk approve -> place order -> fill -> position updated
* “failure path”: exchange timeout -> breaker open -> CollectorDegraded event -> notification

## Load tests (quick but effective)

* simulate N symbols x M exchanges
* verify mailbox size doesn’t explode
* measure end-to-end latency

---

# 10) Concrete deliverables checklist (so you can track progress)

### Deliverable set A: platform + ports

* [ ] `platform/supervisor`
* [ ] `platform/actor`
* [ ] `platform/eventbus`
* [ ] `ports/exchange`, `ports/state`, `ports/notifier`
* [ ] `adapters/ccxt`, `adapters/db`, `adapters/redis`, `adapters/telegram`

### Deliverable set B: first actor conversions

* [ ] `MarketDataCollectorActor` (replaces shared-state collector)
* [ ] `StrategyActor` (replaces signal processor)
* [ ] `RiskActor`
* [ ] `ExecutionActor`
* [ ] `PortfolioActor`

### Deliverable set C: plugin MVP

* [ ] plugin manifest loader
* [ ] builtin registry
* [ ] 1 indicator plugin + 1 strategy plugin migrated

### Deliverable set D: ops robustness

* [ ] `/health` + `/ready`
* [ ] metrics for:

  * mailbox depth per actor
  * event publish rate
  * latency per stage
  * breaker state
* [ ] structured logs with trace IDs

---

# 11) Immediate next step (most effective start)

If you do only one thing first: **Collector Actor conversion**.

Because it’s where deadlocks usually come from:

* shared maps
* readiness channels
* goroutines per exchange/symbol
* locks mixed with network calls

Once Collector is actor-owned + supervised + bounded, your system behavior becomes predictable.

---

If you want, I can also give you a **file-by-file move map** (old path → new path) and a **first PR scope** that is small enough to merge safely (platform + collector actor skeleton + compatibility wrapper), so you don’t need a big-bang rewrite.
