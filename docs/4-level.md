Got it — here’s the **full end-to-end plan** for **Level-4 autonomous AI trading**, including a **concrete execution plan** (PR sequence, deliverables, acceptance tests, and rollout gates). This is written so you can run it like a project playbook.

---

# Level-4 NeuraTrade Refactor Plan (Go-first, Actor + Policy + Agent)

## North Star

A system where an AI can:

* observe live trading + ops state
* generate strategies / tune parameters
* deploy changes safely
* trade live autonomously
  …while **policy + kill switch + audit** ensure safety and deterministic recovery.

---

# 1) Target Architecture

## 1.1 Two planes

### Execution Plane (deterministic, must be safe without AI)

* actor-based runtime (single-writer state)
* bounded queues + timeouts
* idempotent order placement
* policy gate (risk + safety)
* audit trail + event stream
* portfolio truth maintained centrally

### Agent Control Plane (autonomous operator + researcher)

* consumes event stream + metrics
* runs playbooks (self-heal, degrade, roll back)
* creates/updates strategy plugins
* backtests and promotes strategies
* issues commands to execution plane via controlled API

---

# 2) Final Foldering (Go-first, compatible with your repo)

## 2.1 Backend execution plane (inside `services/backend-api`)

```text id="h7vnnf"
services/backend-api/
  cmd/server/                      # wiring only
  internal/
    platform/
      supervisor/
      actor/
      eventbus/
      breaker/ retry/ timeout/
      metrics/ logging/ tracing/
    ports/
      exchange.go
      state.go
      notifier.go
      eventbus.go
      plugin.go
      policy.go
    adapters/
      ccxt/
      db/
      redis/
      telegram/
    domain/
      marketdata/
      signals/
      risk/
      execution/
      portfolio/
      common/
    app/
      bootstrap/
      marketdata/
      strategy/
      risk/
      execution/
      portfolio/
      notify/
  plugins/
    manifests/
    builtin/
    wasm/                          # later (wazero)
```

## 2.2 Agent control plane (new service)

```text id="dps3yq"
services/agent-control/
  cmd/agent/
  internal/
    ingest/                        # subscribe to events, metrics summaries
    policies/                      # what the agent is allowed to do automatically
    playbooks/                     # self-heal scripts (pause exchange, safe mode)
    research/                      # backtest / eval pipeline hooks
    deploy/                        # plugin promotion / rollback
    client/                        # calls backend command API
    audit/                         # logs every agent action
```

---

# 3) Execution Plane Design (Actors + Events + Commands)

## 3.1 Core actors (minimal set)

1. **MarketDataCollectorActor**
2. **StrategyActor**
3. **RiskActor (Policy Gate)**
4. **ExecutionActor**
5. **PortfolioActor**
6. **NotificationActor** (optional early)

## 3.2 Event flow (the “trading pipeline”)

`MarketTick/Candle`
→ `SignalProposed`
→ `OrderIntentApproved/Rejected`
→ `OrderPlaced`
→ `OrderFilled/Cancelled/Rejected`
→ `PositionUpdated`
→ `PnLUpdated`
→ alerts + audit events

## 3.3 Command surface (what AI/operator calls)

Expose safe commands (HTTP/gRPC) which translate into actor messages:

### Market data commands

* `PauseExchange(exchange)`
* `ResumeExchange(exchange)`
* `SetPollingInterval(exchange, ms)`
* `UpdateSymbols(exchange, symbols[])`

### Strategy commands

* `EnableStrategy(strategyID, config)`
* `DisableStrategy(strategyID)`
* `SetStrategyParam(strategyID, key, value)`
* `SetStrategyMode(strategyID, shadow|paper|live)`

### Risk commands

* `SetRiskBudget(strategyID, budget)`
* `SetMaxExposure(symbol, limit)`
* `EnableSafeMode() / DisableSafeMode()`
* `KillSwitch(on/off)` (hard stop new orders)

### Execution commands

* `CancelAllOpenOrders(exchange|symbol)`
* `FlattenPositions(scope)` (only if policy allows)

> For Level-4: the AI only interacts through this command layer, never by “touching internal state”.

---

# 4) Policy Engine (the Level-4 safety core)

## 4.1 Policy checks on every Order Intent

Hard (cannot override):

* max order size
* max leverage
* max notional per symbol/exchange
* max daily loss and max drawdown
* allowed exchanges/symbols allowlist
* “safe mode” blocks new trades
* circuit breaker blocks on rejection/slippage spikes

Soft (tunable):

* minimum liquidity
* max spread
* confidence threshold
* volatility halt
* cooldown windows after loss streak

## 4.2 Policy output events

* `OrderIntentApproved{IntentID, constraints}`
* `OrderIntentRejected{IntentID, reason}`
* `RiskLimitBreached{type, details}`

## 4.3 Kill Switch

* blocks ExecutionActor from placing any new order
* can optionally cancel open orders
* emits `KillSwitchEngaged` events
* must be instant + deterministic

---

# 5) Plugin System Strategy (Go-safe)

## Phase A (MVP, fastest): Built-in registry + manifests

* plugin = Go package registered in a map
* manifest controls enable + config
* safe and deployable today

## Phase B (production-grade): WASM sandbox (wazero)

* plugin binaries are `.wasm`
* memory/time limits + capability gating
* ideal for “agent generates strategy plugin” future

**You’ll reach Level-4 faster with Phase A first**, then Phase B for safety/scale.

---

# 6) Full PR Execution Plan (mergeable increments)

## PR0 — Observability & Safety Baseline (optional but recommended)

**Scope**

* structured logging (trace IDs)
* `/health`, `/ready`
* metrics endpoint
* CI: `go test -race ./...`

**Acceptance**

* can detect stalls via metrics/logs

---

## PR1 — Platform Foundation (supervisor + actor + eventbus)

**Deliverables**

* `platform/supervisor` (errgroup lifecycle)
* `platform/actor` (bounded mailbox + deadletter)
* `platform/eventbus` (in-proc pub/sub)
* standardized timeouts/retry/breaker utilities

**Acceptance tests**

* shutdown stops everything cleanly
* no goroutine leaks (use `goleak`)
* mailbox backpressure works

---

## PR2 — Ports & Adapters (no behavior change)

**Deliverables**

* `ports/exchange`, `ports/state`, `ports/notifier`
* `adapters/ccxt` wrapping existing ccxt-service calls
* `adapters/telegram`, `adapters/db`, `adapters/redis`

**Acceptance**

* existing endpoints still behave
* integration smoke test passes

---

## PR3 — Collector → MarketDataCollectorActor (big win against deadlocks)

**Deliverables**

* collector state fully actor-owned
* publish MarketTick/Candle events
* pause/resume commands

**Acceptance**

* `-race` passes
* no mutex-heavy shared maps in collector path
* simulated load doesn’t increase goroutines unbounded

---

## PR4 — StrategyActor + Domain extraction

**Deliverables**

* strategy consumes market events
* domain logic moved to `domain/signals`
* emits `SignalProposed`

**Acceptance**

* deterministic replay test: same ticks → same signals
* performance test: bounded latency

---

## PR5 — RiskActor + Policy Engine + Kill Switch

**Deliverables**

* risk checks gate all trading intents
* safe mode + kill switch implemented
* emits approve/reject events with reasons

**Acceptance**

* policy blocks out-of-bounds orders
* safe mode blocks new orders instantly
* kill switch blocks even if AI sends “trade now”

---

## PR6 — ExecutionActor + Idempotency + Audit Trail

**Deliverables**

* `IntentID` → `ClientOrderID` mapping
* idempotent order placement (no duplicates on retry)
* audit log table (or event log) with hash-chain optional
* emits full order lifecycle events

**Acceptance**

* restart-safe: replay doesn’t duplicate orders
* rejection handling emits correct event reasons

---

## PR7 — PortfolioActor + Truth model

**Deliverables**

* portfolio state derived from fills/trades
* single source of truth for exposure
* emits `PositionUpdated`, `PnLUpdated`

**Acceptance**

* reconciliation test (fills → positions)
* exposure calculations stable

---

## PR8 — Plugin MVP (built-in + manifests)

**Deliverables**

* plugin registry
* manifest loader
* enable/disable strategies at runtime via actor commands
* 1–2 strategies as plugins

**Acceptance**

* toggle strategy without restart
* config reload safe

---

## PR9 — Agent Control Plane v1 (Ops autonomy first)

**Deliverables**

* agent subscribes to events + metrics summaries
* playbooks:

  * restart actor on stall
  * pause exchange on error spikes
  * safe mode on drawdown
  * rollback strategy on regression
* audit of agent actions

**Acceptance**

* agent can self-heal in staging
* all actions are logged + explainable

---

## PR10 — Agent Trading Autonomy (Level-4 mode)

**Deliverables**

* agent can propose signals OR deploy strategies
* risk policy remains final gate
* staged rollout system:

  * shadow → paper → live
  * promote only on acceptance criteria
* auto rollback triggers (PnL, slippage, rejections)

**Acceptance**

* live trading only possible when:

  * policy passes
  * safe mode off
  * kill switch off
  * strategy in live mode
* rollback triggers proven by simulation

---

# 7) Rollout Plan (how to deploy safely)

## Stage 1: Local simulation

* replay market data / synthetic ticks
* ensure deterministic signals and no stalls

## Stage 2: Paper trading

* ExecutionActor uses “paper adapter”
* verify policy gating + idempotency

## Stage 3: Shadow mode

* run strategies live, but don’t place orders
* compare predicted vs actual outcomes

## Stage 4: Limited live

* small risk budget, small symbol universe
* only one strategy live
* tight rollback thresholds

## Stage 5: Full autonomy

* AI can promote strategies by itself *within policy*
* continuous evaluation + rollback

---

# 8) Acceptance criteria (Level-4 “done” definition)

✅ **Reliability**

* no deadlocks under load test
* bounded goroutines and memory
* clean shutdown and restart-safe

✅ **Safety**

* kill switch works instantly
* policy gate blocks unsafe intents always
* audit trail for every decision

✅ **Autonomy**

* agent can:

  * self-heal ops incidents
  * tune parameters within limits
  * promote/rollback strategies based on eval
  * trade live through policy gate

✅ **Governance**

* clear permissions: what AI can do automatically vs needs approval
* all actions recorded and explainable

---

# 9) Practical “Execution Plan” for you (what to do next)

### Step 1 (today): PR1 scope lock

Start with **PR1 Platform Foundation** because everything else depends on it.

**PR1 checklist**

* [ ] supervisor
* [ ] actor runtime bounded mailbox
* [ ] eventbus
* [ ] standard timeout wrapper
* [ ] kill goroutine leaks

### Step 2: PR3 Collector Actor conversion

This gives immediate deadlock relief.

### Step 3: Policy + idempotency before AI trading

Don’t let agent trade until PR5+PR6 exist.

---

# 10) What I can generate for you next (choose one)

Since I can’t directly open a PR on GitHub, I can still produce PR-ready artifacts:

### Option A (best): You upload repo zip / backend-api folder

I’ll output:

* PR1 code skeleton (files + content)
* PR description + checklist
* exact `git mv` steps
* test commands

### Option B: Paste key files

If you paste:

* collector service file
* signal processor file
* server main wiring
  …I’ll produce a precise mapping and PR1/PR3 patch plan.

---

If you say **“Start with PR1”**, I will reply with:

* the exact `platform/supervisor`, `platform/actor`, `platform/eventbus` APIs
* recommended defaults (mailbox size, timeouts)
* the folder/file list to add
* PR title + body + test plan + rollout notes
