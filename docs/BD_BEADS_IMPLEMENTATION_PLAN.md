# NeuraTrade BD Beads Implementation Plan

**Generated**: 2026-02-11
**Source**: `docs/Master_Plan.md`
**Total Tasks**: 79 tasks across 4 epics

## Architecture Constraints (LOCKED)

These constraints must be **strictly enforced** throughout implementation:

- **Database**: SQLite + Redis default (no Postgres-first)
- **Vector Memory**: sqlite-vec for semantic retrieval
- **Language**: Go-only for core runtime (no Python in production pipeline)
- **Wallet Scanner**: Weighted signal only (no direct auto-execution)
- **GoFlux**: Adapter migration path (compatibility-first rollout)
- **Upside Policy**: Uncapped upside with deterministic survival gates
- **DB Discipline**: Migration-only schema changes
- **Learning Guards**: Persistent learning + anti-loop + anti-hallucination gates

---

## EPIC 1: FOUNDATION (Weeks 1-2)
**Objective**: Core connectivity, onboarding UX, data ingestion

### TASK 1.1: SQLite + Redis Infrastructure Setup
**Priority**: P0
**Dependencies**: None

#### Subtasks:
1.1.1 Create SQLite schema with all core tables (users, api_keys, wallets, trades, thoughts, quests, ai_sessions, ai_usage, market_history, fund_milestones)
   - **Acceptance**: All tables created, migrations apply cleanly
   - **DB Command**: `bd create "Create SQLite schema with all core tables" -t bug -p 0 --json`

1.1.2 Add sqlite-vec extension for vector memory
   - **Acceptance**: Vector functions available in SQLite, schema includes vector tables
   - **DB Command**: `bd create "Add sqlite-vec extension for vector memory" -t bug -p 0 --json`

1.1.3 Configure Redis for pub/sub, caching, locks
   - **Acceptance**: Redis connections working, pub/sub channels operational, distributed locks functional
   - **DB Command**: `bd create "Configure Redis for pub/sub, caching, locks" -t bug -p 0 --json`

1.1.4 Implement persistence abstraction layer
   - **Acceptance**: Repository pattern exposes SQLite and Redis interfaces, code doesn't bind to internals
   - **DB Command**: `bd create "Implement persistence abstraction layer" -t task -p 0 --json`

#### Dependencies:
- None
#### Critical Path:
1.1.1 → 1.1.2 → 1.1.3 → 1.1.4

---

### TASK 1.2: One-Line Installer & CLI Bootstrap
**Priority**: P0
**Dependencies**: 1.1

#### Subtasks:
1.2.1 Create `install.sh` shell script
   - **Acceptance**: Single command installs NeuraTrade binary, writes config directory, sets up env
   - **DB Command**: `bd create "Create install.sh shell script" -t task -p 0 --json`

1.2.2 Implement `NeuraTrade` CLI bootstrap command
   - **Acceptance**: CLI binary runs, handles --version, --help, --init flags
   - **DB Command**: `bd create "Implement NeuraTrade CLI bootstrap command" -t task -p 0 --json`

1.2.3 Add .env template generation
   - **Acceptance**: Template created with all required variables, comments explain each
   - **DB Command**: `bd create "Add .env template generation" -t task -p 0 --json`

#### Dependencies:
- 1.1
#### Critical Path:
1.2.1 → 1.2.2 → 1.2.3

---

### TASK 1.3: CLI <-> Telegram Authentication Handshake
**Priority**: P0
**Dependencies**: 1.2

#### Subtasks:
1.3.1 Implement one-time auth code generation
   - **Acceptance**: Bot sends unique code, validates against user input
   - **DB Command**: `bd create "Implement one-time auth code generation" -t task -p 0 --json`

1.3.2 Bind local operator profile to Telegram chat
   - **Acceptance**: CLI stores encrypted operator profile, links to chat_id, code validation succeeds
   - **DB Command**: `bd create "Bind local operator profile to Telegram chat" -t task -p 0 --json`

1.3.3 Add operator identity encryption (Argon2)
   - **Acceptance**: Operator data encrypted at rest, decryption happens only in memory
   - **DB Command**: `bd create "Add operator identity encryption (Argon2)" -t task -p 0 --json`

#### Dependencies:
- 1.2
#### Critical Path:
1.3.1 → 1.3.2 → 1.3.3

---

### TASK 1.4: Readiness Gate Implementation
**Priority**: P0
**Dependencies**: 1.3, 1.1

#### Subtasks:
1.4.1 Implement wallet minimum checks
   - **Acceptance**: Minimum 1 Polymarket wallet, check passes for 1 CEX scalping, 2 CEX arbitrage
   - **DB Command**: `bd create "Implement wallet minimum checks" -t task -p 0 --json`

1.4.2 Validate API key permissions (trade-only, no-withdraw)
   - **Acceptance**: Exchange and wallet API keys validated for correct permissions
   - **DB Command**: `bd create "Validate API key permissions (trade-only, no-withdraw)" -t task -p 0 --json`

1.4.3 Connectivity checks for all configured providers
   - **Acceptance**: Exchange, wallet, Redis, SQLite all reachable and healthy
   - **DB Command**: `bd create "Connectivity checks for all configured providers" -t task -p 0 --json`

1.4.4 Balance/funding validation
   - **Acceptance**: Minimum thresholds validated based on risk profile
   - **DB Command**: `bd create "Balance/funding validation" -t task -p 0 --json`

1.4.5 Health checks for Redis, SQL storage, exchange bridges
   - **Acceptance**: Health endpoints return 200 with status, failures trigger specific remediation
   - **DB Command**: `bd create "Health checks for Redis, SQL storage, exchange bridges" -t task -p 0 --json`

#### Dependencies:
- 1.3, 1.1
#### Critical Path:
1.4.1 → 1.4.2 → 1.4.3 → 1.4.4 → 1.4.5

---

### TASK 1.5: CCXT Normalization Layer
**Priority**: P0
**Dependencies**: 1.1

#### Subtasks:
1.5.1 Extend existing backend-api with CCXT wrapper
   - **Acceptance**: Unified interface for 100+ exchanges, normalizes tickers, orderbooks, trades
   - **DB Command**: `bd create "Extend existing backend-api with CCXT wrapper" -t task -p 0 --json`

1.5.2 Implement WebSocket market data subscription
   - **Acceptance**: WebSockets active for subscribed pairs, real-time tick updates to Redis pub/sub
   - **DB Command**: `bd create "Implement WebSocket market data subscription" -t task -p 0 --json`

1.5.3 Rate limit management with token bucket
   - **Acceptance**: Per-exchange rate limits enforced, automatic backoff on limit errors
   - **DB Command**: `bd create "Rate limit management with token bucket" -t task -p 0 --json`

#### Dependencies:
- 1.1
#### Critical Path:
1.5.1 → 1.5.2 → 1.5.3

---

### TASK 1.6: Polymarket API Integration
**Priority**: P0
**Dependencies**: 1.1

#### Subtasks:
1.6.1 Implement Gamma API wrapper (market discovery)
   - **Acceptance**: GraphQL/REST queries to https://gamma-api.polymarket.com, returns market metadata
   - **DB Command**: `bd create "Implement Gamma API wrapper (market discovery)" -t task -p 0 --json`

1.6.2 Implement CLOB API wrapper (order execution)
   - **Acceptance**: HMAC signing with Polygon wallet private key, FOK/GTC order support
   - **DB Command**: `bd create "Implement CLOB API wrapper (order execution)" -t task -p 0 --json`

1.6.3 Implement Data API wrapper (positions/balances)
   - **Acceptance**: Fetch positions, trades, and holder data, normalize to internal models
   - **DB Command**: `bd create "Implement Data API wrapper (positions/balances)" -t task -p 0 --json`

#### Dependencies:
- 1.1
#### Critical Path:
1.6.1 → 1.6.2 → 1.6.3

---

### TASK 1.7: Redis Pub/Sub Channels
**Priority**: P1
**Dependencies**: 1.1, 1.5, 1.6

#### Subtasks:
1.7.1 Define market data channels
   - **Acceptance**: Channels defined (e.g., `market:eth_usdc:ticker`, `market:polymarket:opportunity`)
   - **DB Command**: `bd create "Define market data channels" -t task -p 1 --json`

1.7.2 Implement Redis message publishing
   - **Acceptance**: Market ticks pushed to channels, all events tagged with timestamp and source
   - **DB Command**: `bd create "Implement Redis message publishing" -t task -p 1 --json`

1.7.3 Implement Redis message subscription
   - **Acceptance**: Agent service subscribes to channels, handles incoming messages asynchronously
   - **DB Command**: `bd create "Implement Redis message subscription" -t task -p 1 --json`

#### Dependencies:
- 1.1, 1.5, 1.6
#### Critical Path:
1.7.1 → 1.7.2 → 1.7.3

---

### TASK 1.8: Basic Skill.md Definitions
**Priority**: P1
**Dependencies**: 1.7

#### Subtasks:
1.8.1 Create `get_price` skill.md
   - **Acceptance**: Skill definition with parameters, documentation, best practices
   - **DB Command**: `bd create "Create get_price skill.md" -t task -p 1 --json`

1.8.2 Create `place_order` skill.md
   - **Acceptance**: Skill definition for order placement with validation
   - **DB Command**: `bd create "Create place_order skill.md" -t task -p 1 --json`

1.8.3 Create `get_portfolio` skill.md
   - **Acceptance**: Skill definition for portfolio snapshot
   - **DB Command**: `bd create "Create get_portfolio skill.md" -t task -p 1 --json`

#### Dependencies:
- 1.7
#### Critical Path:
1.8.1 → 1.8.2 → 1.8.3

---

### TASK 1.9: AES-256-GCM Key Encryption
**Priority**: P0
**Dependencies**: 1.1

#### Subtasks:
1.9.1 Implement AES-256-GCM encryption for API keys
   - **Acceptance**: Keys encrypted at rest in `api_keys` table, decryption in memory only
   - **DB Command**: `bd create "Implement AES-256-GCM encryption for API keys" -t task -p 0 --json`

1.9.2 Add key masking to logs and Telegram output
   - **Acceptance**: No raw keys appear in logs or Telegram messages, masked format shown
   - **DB Command**: `bd create "Add key masking to logs and Telegram output" -t task -p 0 --json`

#### Dependencies:
- 1.1
#### Critical Path:
1.9.1 → 1.9.2

---

## EPIC 2: AGENTIC CORE (Weeks 3-4)
**Objective**: Go-native AI agent — "The Brain"

### TASK 2.1: Go-Native Tool-Calling Loop
**Priority**: P0
**Dependencies**: 1.8

#### Subtasks:
2.1.1 Implement direct HTTP to OpenAI/Anthropic APIs
   - **Acceptance**: HTTP client with error handling, retries, timeout configuration
   - **DB Command**: `bd create "Implement direct HTTP to OpenAI/Anthropic APIs" -t task -p 0 --json`

2.1.2 Build prompt builder from skill.md + context
   - **Acceptance**: Prompt assembled dynamically from skill definitions, conversation history
   - **DB Command**: `bd create "Build prompt builder from skill.md + context" -t task -p 0 --json`

2.1.3 Parse tool_calls from LLM responses
   - **Acceptance**: Structured parsing of tool calls, validation against skill contracts
   - **DB Command**: `bd create "Parse tool_calls from LLM responses" -t task -p 0 --json`

2.1.4 Execute Go functions for tool calls
   - **Acceptance**: Tool actions executed, results appended to conversation, loop repeats until final answer
   - **DB Command**: `bd create "Execute Go functions for tool calls" -t task -p 0 --json`

#### Dependencies:
- 1.8
#### Critical Path:
2.1.1 → 2.1.2 → 2.1.3 → 2.1.4

---

### TASK 2.2: Skill.md Parser & Progressive Disclosure
**Priority**: P0
**Dependencies**: 2.1

#### Subtasks:
2.2.1 Implement skill.md file loader
   - **Acceptance**: Reads YAML frontmatter and body, validates syntax
   - **DB Command**: `bd create "Implement skill.md file loader" -t task -p 0 --json`

2.2.2 Implement progressive disclosure system
   - **Acceptance**: Agent sees lightweight list, requests full skill definition on need
   - **DB Command**: `bd create "Implement progressive disclosure system" -t task -p 0 --json`

2.2.3 Store skill hashes for version tracking
   - **Acceptance**: SHA-256 hash computed on load, stored in `thoughts` table
   - **DB Command**: `bd create "Store skill hashes for version tracking" -t task -p 0 --json`

#### Dependencies:
- 2.1
#### Critical Path:
2.2.1 → 2.2.2 → 2.2.3

---

### TASK 2.3: Multi-Agent Debate Protocol
**Priority**: P0
**Dependencies**: 2.2

#### Subtasks:
2.3.1 Implement Analyst agent role
   - **Acceptance**: Role prompts market scan, sentiment analysis, trade ideas
   - **DB Command**: `bd create "Implement Analyst agent role" -t task -p 0 --json`

2.3.2 Implement Trader agent role
   - **Acceptance**: Role converts ideas to concrete actions, order book analysis
   - **DB Command**: `bd create "Implement Trader agent role" -t task -p 0 --json`

2.3.3 Implement Risk Manager agent role
   - **Acceptance**: Role vetoes unsafe actions, exposure checks, stop-loss enforcement
   - **DB Command**: `bd create "Implement Risk Manager agent role" -t task -p 0 --json`

2.3.4 Implement round-robin debate loop
   - **Acceptance**: Exchange of proposals between agents, consensus required for execution
   - **DB Command**: `bd create "Implement round-robin debate loop" -t task -p 0 --json`

#### Dependencies:
- 2.2
#### Critical Path:
2.3.1 → 2.3.2 → 2.3.3 → 2.3.4

---

### TASK 2.4: Quest Scheduler
**Priority**: P0
**Dependencies**: 2.3

#### Subtasks:
2.4.1 Implement cron-based quest scheduling
   - **Acceptance**: Micro (1-5min), hourly, daily, weekly, milestone cadences configured
   - **DB Command**: `bd create "Implement cron-based quest scheduling" -t task -p 0 --json`

2.4.2 Implement event-driven quest triggers
   - **Acceptance**: Triggers on volatility spike, market discovery, milestone reached
   - **DB Command**: `bd create "Implement event-driven quest triggers" -t task -p 0 --json`

2.4.3 Implement quest state persistence
   - **Acceptance**: Quest progress saved to SQL store, resume after restart
   - **DB Command**: `bd create "Implement quest state persistence" -t task -p 0 --json`

#### Dependencies:
- 2.3
#### Critical Path:
2.4.1 → 2.4.2 → 2.4.3

---

### TASK 2.5: Parallel Job System
**Priority**: P0
**Dependencies**: 2.4

#### Subtasks:
2.5.1 Implement Redis job queue
   - **Acceptance**: Jobs categorized (MARKET_SCAN, TRADE_EXECUTE, RISK_CHECK, REPORT, REBALANCE)
   - **DB Command**: `bd create "Implement Redis job queue" -t task -p 0 --json`

2.5.2 Implement priority levels (CRITICAL > HIGH > NORMAL > LOW)
   - **Acceptance**: Jobs scheduled by priority, urgent jobs processed first
   - **DB Command**: `bd create "Implement priority levels (CRITICAL > HIGH > NORMAL > LOW)" -t task -p 0 --json`

2.5.3 Implement goroutine pool with concurrency limits
   - **Acceptance**: Configurable concurrency per job type, resource management
   - **DB Command**: `bd create "Implement goroutine pool with concurrency limits" -t task -p 0 --json`

2.5.4 Implement distributed locks
   - **Acceptance**: Redis SETNX locks prevent conflicting operations on same asset
   - **DB Command**: `bd create "Implement distributed locks" -t task -p 0 --json`

#### Dependencies:
- 2.4
#### Critical Path:
2.5.1 → 2.5.2 → 2.5.3 → 2.5.4

---

### TASK 2.6: AI Session Persistence
**Priority**: P1
**Dependencies**: 2.5

#### Subtasks:
2.6.1 Serialize AI session state
   - **Acceptance**: Conversation chain, loaded skills, market snapshot serialized to BLOB
   - **DB Command**: `bd create "Serialize AI session state" -t task -p 1 --json`

2.6.2 Implement session resumption
   - **Acceptance**: Sessions loaded from SQL store, context restored on resume
   - **DB Command**: `bd create "Implement session resumption" -t task -p 1 --json`

2.6.3 Implement session lifecycle management
   - **Acceptance**: Active → Suspended (on priority switch) → Archived (on completion) flow
   - **DB Command**: `bd create "Implement session lifecycle management" -t task -p 1 --json`

#### Dependencies:
- 2.5
#### Critical Path:
2.6.1 → 2.6.2 → 2.6.3

---

### TASK 2.7: Local MLX Integration
**Priority**: P1
**Dependencies**: 2.6

#### Subtasks:
2.7.1 Implement Go ↔ mlx_lm.server HTTP communication
   - **Acceptance**: HTTP client calls `localhost:8080/v1/chat/completions`, OpenAI-compatible
   - **DB Command**: `bd create "Implement Go ↔ mlx_lm.server HTTP communication" -t task -p 1 --json`

2.7.2 Implement quantized model loading
   - **Acceptance**: Llama 3/Mistral 4-bit or 8-bit models load successfully
   - **DB Command**: `bd create "Implement quantized model loading" -t task -p 1 --json`

2.7.3 Implement watchdog filtering
   - **Acceptance**: Local model monitors WebSocket data, filters for trigger conditions
   - **DB Command**: `bd create "Implement watchdog filtering" -t task -p 1 --json`

#### Dependencies:
- 2.6
#### Critical Path:
2.7.1 → 2.7.2 → 2.7.3

---

### TASK 2.8: Deterministic Survival Governors
**Priority**: P0
**Dependencies**: 2.5

#### Subtasks:
2.8.1 Implement daily loss cap
   - **Acceptance**: Configurable daily loss threshold, hard stop on breach
   - **DB Command**: `bd create "Implement daily loss cap" -t task -p 0 --json`

2.8.2 Implement max drawdown halt
   - **Acceptance**: Portfolio value drop > 5% in 24h triggers global liquidation
   - **DB Command**: `bd create "Implement max drawdown halt" -t task -p 0 --json`

2.8.3 Implement consecutive-loss pause
   - **Acceptance**: N sequential losses triggers automatic cool-down for strategy lane
   - **DB Command**: `bd create "Implement consecutive-loss pause" -t task -p 0 --json`

2.8.4 Implement position-size throttle
   - **Acceptance**: Size clamped to volatility-adjusted Kelly, hard caps always apply
   - **DB Command**: `bd create "Implement position-size throttle" -t task -p 0 --json`

#### Dependencies:
- 2.5
#### Critical Path:
2.8.1 → 2.8.2 → 2.8.3 → 2.8.4

---

### TASK 2.9: Internal Tool Endpoints
**Priority**: P0
**Dependencies**: 2.8

#### Subtasks:
2.9.1 Expose readiness endpoints
   - **Acceptance**: `/health`, `/ready`, `/live` endpoints return status
   - **DB Command**: `bd create "Expose readiness endpoints" -t task -p 0 --json`

2.9.2 Expose risk primitives
   - **Acceptance**: `pre_trade_risk_check`, `portfolio_exposure_snapshot`, `max_drawdown_guard`
   - **DB Command**: `bd create "Expose risk primitives" -t task -p 0 --json`

2.9.3 Expose cleanup endpoints
   - **Acceptance**: `/api/data/cleanup`, `/api/data/stats` trigger and show cleanup status
   - **DB Command**: `bd create "Expose cleanup endpoints" -t task -p 0 --json`

2.9.4 Expose arbitrage primitives
   - **Acceptance**: Spot and futures arbitrage query endpoints available
   - **DB Command**: `bd create "Expose arbitrage primitives" -t task -p 0 --json`

#### Dependencies:
- 2.8
#### Critical Path:
2.9.1 → 2.9.2 → 2.9.3 → 2.9.4

---

### TASK 2.10: IndicatorProvider Abstraction
**Priority**: P1
**Dependencies**: 2.9

#### Subtasks:
2.10.1 Define IndicatorProvider interface
   - **Acceptance**: Interface exposes indicator calculation methods
   - **DB Command**: `bd create "Define IndicatorProvider interface" -t task -p 1 --json`

2.10.2 Implement existing provider adapter
   - **Acceptance**: Current TA provider wrapped in abstraction
   - **DB Command**: `bd create "Implement existing provider adapter" -t task -p 1 --json`

2.10.3 Add provider selection config
   - **Acceptance**: Config specifies active provider, fallback strategy
   - **DB Command**: `bd create "Add provider selection config" -t task -p 1 --json`

#### Dependencies:
- 2.9
#### Critical Path:
2.10.1 → 2.10.2 → 2.10.3

---

## EPIC 3: STRATEGY & UI (Weeks 5-6)
**Objective**: Trading logic, Telegram interface, paper trading

### TASK 3.1: Scalping Strategy
**Priority**: P0
**Dependencies**: 2.10

#### Subtasks:
3.1.1 Codify scalping skill.md
   - **Acceptance**: Skill definition with entry/exit rules, RSI divergence, volume confirmation
   - **DB Command**: `bd create "Codify scalping skill.md" -t task -p 0 --json`

3.1.2 Implement order book imbalance detection
   - **Acceptance**: Orders on one side > 60% depth, identifies imbalance
   - **DB Command**: `bd create "Implement order book imbalance detection" -t task -p 0 --json`

3.1.3 Implement tight stop-loss execution
   - **Acceptance**: 0.05%-0.1% stop-loss enforced, automatic cancellation on trigger
   - **DB Command**: `bd create "Implement tight stop-loss execution" -t task -p 0 --json`

#### Dependencies:
- 2.10
#### Critical Path:
3.1.1 → 3.1.2 → 3.1.3

---

### TASK 3.2: Sum-to-One Arbitrage Strategy
**Priority**: P0
**Dependencies**: 2.10

#### Subtasks:
3.2.1 Codify sum-to-one arbitrage skill.md
   - **Acceptance**: Skill definition for YES+NO arbitrage, FOK order enforcement
   - **DB Command**: `bd create "Codify sum-to-one arbitrage skill.md" -t task -p 0 --json`

3.2.2 Implement arbitrage trigger detection
   - **Acceptance**: If P(YES)+P(NO) < 0.98, trigger arbitrage
   - **DB Command**: `bd create "Implement arbitrage trigger detection" -t task -p 0 --json`

3.2.3 Implement FOK order execution
   - **Acceptance**: Orders executed only if fully fillable, otherwise cancelled
   - **DB Command**: `bd create "Implement FOK order execution" -t task -p 0 --json`

#### Dependencies:
- 2.10
#### Critical Path:
3.2.1 → 3.2.2 → 3.2.3

---

### TASK 3.3: Sentiment Momentum Strategy
**Priority**: P1
**Dependencies**: 2.10

#### Subtasks:
3.3.1 Codify sentiment momentum skill.md
   - **Acceptance**: Skill definition combining technicals + sentiment analysis
   - **DB Command**: `bd create "Codify sentiment momentum skill.md" -t task -p 1 --json`

3.3.2 Implement multi-indicator stack (SMA/EMA, RSI, MACD, ATR, Bollinger Bands)
   - **Acceptance**: All indicators calculated and exposed as features
   - **DB Command**: `bd create "Implement multi-indicator stack" -t task -p 1 --json`

3.3.3 Implement Twitter sentiment analysis
   - **Acceptance**: Twitter API integration, sentiment scoring via LLM
   - **DB Command**: `bd create "Implement Twitter sentiment analysis" -t task -p 1 --json`

#### Dependencies:
- 2.10
#### Critical Path:
3.3.1 → 3.3.2 → 3.3.3

---

### TASK 3.4: Telegram Bot Command Handlers
**Priority**: P0
**Dependencies**: 3.1, 3.2, 3.3

#### Subtasks:
3.4.1 Implement `/begin` and `/pause` handlers
   - **Acceptance**: Start/stop autonomous mode, respect readiness gate
   - **DB Command**: `bd create "Implement /begin and /pause handlers" -t task -p 0 --json`

3.4.2 Implement `/summary` and `/performance` handlers
   - **Acceptance**: 24-hour summary, strategy-level breakdown (win rate, Sharpe, drawdown)
   - **DB Command**: `bd create "Implement /summary and /performance handlers" -t task -p 0 --json`

3.4.3 Implement `/liquidate` and `/liquidate_all` handlers
   - **Acceptance**: Emergency liquidation commands, validated against risk limits
   - **DB Command**: `bd create "Implement /liquidate and /liquidate_all handlers" -t task -p 0 --json`

3.4.4 Implement wallet management commands
   - **Acceptance**: `/connect_exchange`, `/connect_polymarket`, `/add_wallet`, `/remove_wallet`
   - **DB Command**: `bd create "Implement wallet management commands" -t task -p 0 --json`

3.4.5 Implement quest and monitoring commands
   - **Acceptance**: `/quests`, `/status`, `/portfolio`, `/wallet`, `/logs`
   - **DB Command**: `bd create "Implement quest and monitoring commands" -t task -p 0 --json`

3.4.6 Implement `/doctor` diagnostic handler
   - **Acceptance**: Guided diagnostics, specific remediation steps for failures
   - **DB Command**: `bd create "Implement /doctor diagnostic handler" -t task -p 0 --json`

#### Dependencies:
- 3.1, 3.2, 3.3
#### Critical Path:
3.4.1 → 3.4.2 → 3.4.3 → 3.4.4 → 3.4.5 → 3.4.6

---

### TASK 3.5: Live Transaction Stream
**Priority**: P0
**Dependencies**: 3.4

#### Subtasks:
3.5.1 Implement action streaming format
   - **Acceptance**: Structured messages (ACTION, Asset, Type, Price, Size, Strategy, Reasoning, Risk Check, Quest)
   - **DB Command**: `bd create "Implement action streaming format" -t task -p 0 --json`

3.5.2 Implement quest progress updates
   - **Acceptance**: Stream shows hourly/daily/weekly quest completion
   - **DB Command**: `bd create "Implement quest progress updates" -t task -p 0 --json`

3.5.3 Implement fund milestone alerts
   - **Acceptance**: "Reached $500 — entering Growth phase" notifications
   - **DB Command**: `bd create "Implement fund milestone alerts" -t task -p 0 --json`

3.5.4 Implement risk event notifications
   - **Acceptance**: Kill switch triggers, position liquidations highlighted
   - **DB Command**: `bd create "Implement risk event notifications" -t task -p 0 --json`

3.5.5 Implement AI reasoning summaries
   - **Acceptance**: Why agent chose strategy, appended to transaction stream
   - **DB Command**: `bd create "Implement AI reasoning summaries" -t task -p 0 --json`

#### Dependencies:
- 3.4
#### Critical Path:
3.5.1 → 3.5.2 → 3.5.3 → 3.5.4 → 3.5.5

---

### TASK 3.6: Execution Layer Tools
**Priority**: P0
**Dependencies**: 3.5

#### Subtasks:
3.6.1 Implement `place_order` tool endpoint
   - **Acceptance**: Tool validates input, enforces risk limits, pushes to Redis queue
   - **DB Command**: `bd create "Implement place_order tool endpoint" -t task -p 0 --json`

3.6.2 Implement `cancel_order` tool endpoint
   - **Acceptance**: Cancel pending orders, validate against risk limits
   - **DB Command**: `bd create "Implement cancel_order tool endpoint" -t task -p 0 --json`

3.6.3 Implement position snapshot tool
   - **Acceptance**: Returns current portfolio state, open positions, exposure
   - **DB Command**: `bd create "Implement position snapshot tool" -t task -p 0 --json`

3.6.4 Implement controlled liquidation tool
   - **Acceptance**: Force exit positions, validate against risk limits
   - **DB Command**: `bd create "Implement controlled liquidation tool" -t task -p 0 --json`

#### Dependencies:
- 3.5
#### Critical Path:
3.6.1 → 3.6.2 → 3.6.3 → 3.6.4

---

### TASK 3.7: GoFlux Integration (Compatibility Path)
**Priority**: P1
**Dependencies**: 2.10

#### Subtasks:
3.7.1 Add goflux as dependency
   - **Acceptance**: go.mod updated, imports work
   - **DB Command**: `bd create "Add goflux as dependency" -t task -p 1 --json`

3.7.2 Implement GoFlux adapter
   - **Acceptance**: GoFlux indicators wrapped in `IndicatorProvider` interface
   - **DB Command**: `bd create "Implement GoFlux adapter" -t task -p 1 --json`

3.7.3 Add parity tests between providers
   - **Acceptance**: Tests verify indicator results match existing provider
   - **DB Command**: `bd create "Add parity tests between providers" -t task -p 1 --json`

#### Dependencies:
- 2.10
#### Critical Path:
3.7.1 → 3.7.2 → 3.7.3

---

### TASK 3.8: AI Cost Tracking & Budget Enforcement
**Priority**: P0
**Dependencies**: 3.6

#### Subtasks:
3.8.1 Implement cost tracking in `ai_usage` table
   - **Acceptance**: Every LLM call logs model, tokens, cost, purpose
   - **DB Command**: `bd create "Implement cost tracking in ai_usage table" -t task -p 0 --json`

3.8.2 Implement daily budget enforcement
   - **Acceptance**: $5.00 daily cap, auto-downgrade to local model on breach
   - **DB Command**: `bd create "Implement daily budget enforcement" -t task -p 0 --json`

3.8.3 Implement monthly budget enforcement
   - **Acceptance**: $100.00 monthly cap, only CRITICAL jobs use cloud models after breach
   - **DB Command**: `bd create "Implement monthly budget enforcement" -t task -p 0 --json`

3.8.4 Expose `/status` budget display
   - **Acceptance**: Shows current spend vs budget in Telegram
   - **DB Command**: `bd create "Expose /status budget display" -t task -p 0 --json`

#### Dependencies:
- 3.6
#### Critical Path:
3.8.1 → 3.8.2 → 3.8.3 → 3.8.4

---

### TASK 3.9: Paper Trading Mode
**Priority**: P0
**Dependencies**: 3.8

#### Subtasks:
3.9.1 Implement virtual account tracking
   - **Acceptance**: Paper trades marked with `paper_trade = true`, virtual balance tracked
   - **DB Command**: `bd create "Implement virtual account tracking" -t task -p 0 --json`

3.9.2 Implement paper execution simulation
   - **Acceptance**: Orders simulated, PnL calculated, no API calls made
   - **DB Command**: `bd create "Implement paper execution simulation" -t task -p 0 --json`

3.9.3 Implement paper trade recording
   - **Acceptance**: All trades recorded with entry/exit prices, fees, PnL
   - **DB Command**: `bd create "Implement paper trade recording" -t task -p 0 --json`

#### Dependencies:
- 3.8
#### Critical Path:
3.9.1 → 3.9.2 → 3.9.3

---

### TASK 3.10: Backtesting Engine
**Priority**: P1
**Dependencies**: 3.9

#### Subtasks:
3.10.1 Implement historical OHLCV replay
   - **Acceptance**: Reads from `market_history` table, replays past market conditions
   - **DB Command**: `bd create "Implement historical OHLCV replay" -t task -p 1 --json`

3.10.2 Simulate AI decision loop
   - **Acceptance**: Replays strategy decisions against past data
   - **DB Command**: `bd create "Simulate AI decision loop" -t task -p 1 --json`

3.10.3 Output performance metrics
   - **Acceptance**: Win rate, max drawdown, Sharpe ratio, profit factor
   - **DB Command**: `bd create "Output performance metrics" -t task -p 1 --json`

3.10.4 Store backtest results
   - **Acceptance**: Results saved in SQL store, comparison across strategy versions
   - **DB Command**: `bd create "Store backtest results" -t task -p 1 --json`

#### Dependencies:
- 3.9
#### Critical Path:
3.10.1 → 3.10.2 → 3.10.3 → 3.10.4

---

### TASK 3.11: Smart Wallet Scanner (Shadow Mode)
**Priority**: P1
**Dependencies**: 3.10

#### Subtasks:
3.11.1 Implement wallet conviction scoring
   - **Acceptance**: Score based on market, side, confidence, freshness decay
   - **DB Command**: `bd create "Implement wallet conviction scoring" -t task -p 1 --json`

3.11.2 Implement cohort-level flow analysis
   - **Acceptance**: Wallet cluster buy/sell pressure, not single-wallet worship
   - **DB Command**: `bd create "Implement cohort-level flow analysis" -t task -p 1 --json`

3.11.3 Implement signal explanation generation
   - **Acceptance**: Explains why signal is meaningful, sources used
   - **DB Command**: `bd create "Implement signal explanation generation" -t task -p 1 --json`

3.11.4 Implement anti-manipulation filters
   - **Acceptance**: Filters Sybil/wash/coordination suspicion, geographic/compliance checks
   - **DB Command**: `bd create "Implement anti-manipulation filters" -t task -p 1 --json`

3.11.5 Implement shadow mode execution
   - **Acceptance**: Signals generated, confirmed by liquidity + risk checks, no direct execution
   - **DB Command**: `bd create "Implement shadow mode execution" -t task -p 1 --json`

#### Dependencies:
- 3.10
#### Critical Path:
3.11.1 → 3.11.2 → 3.11.3 → 3.11.4 → 3.11.5

---

## EPIC 4: PRODUCTION (Week 7+)
**Objective**: Live deployment with real capital

### TASK 4.1: Production Deployment
**Priority**: P0
**Dependencies**: 3.11

#### Subtasks:
4.1.1 Containerize agent and infra services
   - **Acceptance**: Dockerfiles for neuratrade-agent and neuratrade-infra, multi-stage builds
   - **DB Command**: `bd create "Containerize agent and infra services" -t task -p 0 --json`

4.1.2 Set up QuantVPS deployment
   - **Acceptance**: Deployments to ap-northeast-1 (Tokyo) or us-east-1 (Virginia), network topology optimized
   - **DB Command**: `bd create "Set up QuantVPS deployment" -t task -p 0 --json`

4.1.3 Configure production Docker Compose
   - **Acceptance**: Redis, SQLite, agent, infra services orchestrated, health checks
   - **DB Command**: `bd create "Configure production Docker Compose" -t task -p 0 --json`

4.1.4 Set up CI/CD pipeline
   - **Acceptance**: GitHub Actions for build, test, lint, Docker build/push
   - **DB Command**: `bd create "Set up CI/CD pipeline" -t task -p 0 --json`

#### Dependencies:
- 3.11
#### Critical Path:
4.1.1 → 4.1.2 → 4.1.3 → 4.1.4

---

### TASK 4.2: Initial Fund Deployment
**Priority**: P0
**Dependencies**: 4.1

#### Subtasks:
4.2.1 Fund with minimal capital ($100 USDC)
   - **Acceptance**: Initial capital deployed to strategy engine
   - **DB Command**: `bd create "Fund with minimal capital ($100 USDC)" -t task -p 0 --json`

4.2.2 Implement kill switch monitoring
   - **Acceptance**: Daily loss, max drawdown, exchange failures trigger emergency stop
   - **DB Command**: `bd create "Implement kill switch monitoring" -t task -p 0 --json`

4.2.3 Implement exchange resilience monitoring
   - **Acceptance**: Per-exchange health checks, automatic failover on failure
   - **DB Command**: `bd create "Implement exchange resilience monitoring" -t task -p 0 --json`

#### Dependencies:
- 4.1
#### Critical Path:
4.2.1 → 4.2.2 → 4.2.3

---

### TASK 4.3: Fund Milestone Tracking
**Priority**: P1
**Dependencies**: 4.2

#### Subtasks:
4.3.1 Implement bootstrap → growth → scale → mature transitions
   - **Acceptance**: Fund milestones tracked, risk profiles adapt per phase
   - **DB Command**: `bd create "Implement bootstrap → growth → scale → mature transitions" -t task -p 1 --json`

4.3.2 Implement phase-specific strategy adaptation
   - **Acceptance**: Conservative in bootstrap, diversified in growth, yield-focused in mature
   - **DB Command**: `bd create "Implement phase-specific strategy adaptation" -t task -p 1 --json`

4.3.3 Implement automatic capital scaling
   - **Acceptance**: Sharpe Ratio performance guides capital allocation increases
   - **DB Command**: `bd create "Implement automatic capital scaling" -t task -p 1 --json`

#### Dependencies:
- 4.2
#### Critical Path:
4.3.1 → 4.3.2 → 4.3.3

---

### TASK 4.4: Performance Optimization
**Priority**: P1
**Dependencies**: 4.3

#### Subtasks:
4.4.1 Optimize Go runtime performance
   - **Acceptance**: Goroutine pooling, memory profiling, garbage collection tuned
   - **DB Command**: `bd create "Optimize Go runtime performance" -t task -p 1 --json`

4.4.2 Optimize Redis caching strategy
   - **Acceptance**: Hot data cached, eviction policy optimized
   - **DB Command**: `bd create "Optimize Redis caching strategy" -t task -p 1 --json`

4.4.3 Optimize database query performance
   - **Acceptance**: Indexes added for high-frequency lookups, WAL mode enabled
   - **DB Command**: `bd create "Optimize database query performance" -t task -p 1 --json`

4.4.4 Implement slow query logging
   - **Acceptance**: Slow SQL queries logged, performance bottlenecks identified
   - **DB Command**: `bd create "Implement slow query logging" -t task -p 1 --json`

#### Dependencies:
- 4.3
#### Critical Path:
4.4.1 → 4.4.2 → 4.4.3 → 4.4.4

---

### TASK 4.5: Security Audits & Hardening
**Priority**: P0
**Dependencies**: 4.4

#### Subtasks:
4.5.1 Run gosec and gitleaks
   - **Acceptance**: Security vulnerabilities identified and patched
   - **DB Command**: `bd create "Run gosec and gitleaks" -t task -p 0 --json`

4.5.2 Implement rate limit monitoring
   - **Acceptance**: Exchange rate limits tracked, strategy load throttled on violations
   - **DB Command**: `bd create "Implement rate limit monitoring" -t task -p 0 --json`

4.5.3 Implement intrusion detection
   - **Acceptance**: Anomalous trading patterns detected, alerts raised
   - **DB Command**: `bd create "Implement intrusion detection" -t task -p 0 --json`

4.5.4 Implement emergency rollbacks
   - **Acceptance**: Can rollback to previous strategy versions on risk breach
   - **DB Command**: `bd create "Implement emergency rollbacks" -t task -p 0 --json`

#### Dependencies:
- 4.4
#### Critical Path:
4.5.1 → 4.5.2 → 4.5.3 → 4.5.4

---

### TASK 4.6: Comprehensive Documentation
**Priority**: P1
**Dependencies**: 4.5

#### Subtasks:
4.6.1 Create operator guide
   - **Acceptance**: How to connect wallets, start/stop bot, interpret Telegram commands
   - **DB Command**: `bd create "Create operator guide" -t task -p 1 --json`

4.6.2 Create security documentation
   - **Acceptance**: Key management, encryption, wallet architecture explained
   - **DB Command**: `bd create "Create security documentation" -t task -p 1 --json`

4.6.3 Create troubleshooting guide
   - **Acceptance**: Common issues, `doctor` command interpretation, manual interventions
   - **DB Command**: `bd create "Create troubleshooting guide" -t task -p 1 --json`

#### Dependencies:
- 4.5
#### Critical Path:
4.6.1 → 4.6.2 → 4.6.3

---

### TASK 4.7: Monitoring & Observability
**Priority**: P1
**Dependencies**: 4.6

#### Subtasks:
4.7.1 Implement metrics collection
   - **Acceptance**: Prometheus metrics for trades, PnL, API costs, exchange health
   - **DB Command**: `bd create "Implement metrics collection" -t task -p 1 --json`

4.7.2 Implement log aggregation
   - **Acceptance**: Structured logs, error tracking, correlation IDs
   - **DB Command**: `bd create "Implement log aggregation" -t task -p 1 --json`

4.7.3 Set up alerts
   - **Acceptance**: PagerDuty/Slack alerts for kill switch triggers, exchange failures
   - **DB Command**: `bd create "Set up alerts" -t task -p 1 --json`

4.7.4 Implement dashboards
   - **Acceptance**: Grafana dashboards for portfolio performance, AI usage, risk metrics
   - **DB Command**: `bd create "Implement dashboards" -t task -p 1 --json`

#### Dependencies:
- 4.6
#### Critical Path:
4.7.1 → 4.7.2 → 4.7.3 → 4.7.4

---

## TECHNICAL DEPENDENCY MATRIX

### Foundation Dependencies (Epic 1)
- SQLite schema must be complete before: CCXT integration, Polymarket API, Redis channels, skill definitions
- API encryption must be complete before: Onboarding, wallet connections
- Readiness gate must be complete before: `/begin` command

### Agentic Core Dependencies (Epic 2)
- Skill parser must be complete before: Multi-agent roles, debate protocol
- Debate protocol must be complete before: Quest scheduler, job system
- Survival governors must be complete before: Internal tool endpoints
- Indicator abstraction must be complete before: Strategy implementation

### Strategy & UI Dependencies (Epic 3)
- Execution tools must be complete before: Paper trading, backtesting
- Telegram handlers must be complete before: Live transaction stream
- Cost tracking must be complete before: Budget enforcement
- Wallet scanner must be complete before: Shadow mode execution

### Production Dependencies (Epic 4)
- Containerization must be complete before: QuantVPS deployment
- Initial fund deployment must be complete before: Milestone tracking
- Security audits must be complete before: Hardening
- Monitoring must be complete before: Observability

---

## COMMAND-READY TASK CREATION

To create all tasks in this plan, run these commands sequentially:

```bash
# EPIC 1: FOUNDATION
bd create "Create SQLite schema with all core tables" -t bug -p 0 --json
bd create "Add sqlite-vec extension for vector memory" -t bug -p 0 --json
bd create "Configure Redis for pub/sub, caching, locks" -t bug -p 0 --json
bd create "Implement persistence abstraction layer" -t task -p 0 --json
bd create "Create install.sh shell script" -t task -p 0 --json
bd create "Implement NeuraTrade CLI bootstrap command" -t task -p 0 --json
bd create "Add .env template generation" -t task -p 0 --json
bd create "Implement one-time auth code generation" -t task -p 0 --json
bd create "Bind local operator profile to Telegram chat" -t task -p 0 --json
bd create "Add operator identity encryption (Argon2)" -t task -p 0 --json
bd create "Implement wallet minimum checks" -t task -p 0 --json
bd create "Validate API key permissions (trade-only, no-withdraw)" -t task -p 0 --json
bd create "Connectivity checks for all configured providers" -t task -p 0 --json
bd create "Balance/funding validation" -t task -p 0 --json
bd create "Health checks for Redis, SQL storage, exchange bridges" -t task -p 0 --json
bd create "Extend existing backend-api with CCXT wrapper" -t task -p 0 --json
bd create "Implement WebSocket market data subscription" -t task -p 0 --json
bd create "Rate limit management with token bucket" -t task -p 0 --json
bd create "Implement Gamma API wrapper (market discovery)" -t task -p 0 --json
bd create "Implement CLOB API wrapper (order execution)" -t task -p 0 --json
bd create "Implement Data API wrapper (positions/balances)" -t task -p 0 --json
bd create "Define market data channels" -t task -p 1 --json
bd create "Implement Redis message publishing" -t task -p 1 --json
bd create "Implement Redis message subscription" -t task -p 1 --json
bd create "Create get_price skill.md" -t task -p 1 --json
bd create "Create place_order skill.md" -t task -p 1 --json
bd create "Create get_portfolio skill.md" -t task -p 1 --json
bd create "Implement AES-256-GCM encryption for API keys" -t task -p 0 --json
bd create "Add key masking to logs and Telegram output" -t task -p 0 --json

# EPIC 2: AGENTIC CORE
bd create "Implement direct HTTP to OpenAI/Anthropic APIs" -t task -p 0 --json
bd create "Build prompt builder from skill.md + context" -t task -p 0 --json
bd create "Parse tool_calls from LLM responses" -t task -p 0 --json
bd create "Execute Go functions for tool calls" -t task -p 0 --json
bd create "Implement skill.md file loader" -t task -p 0 --json
bd create "Implement progressive disclosure system" -t task -p 0 --json
bd create "Store skill hashes for version tracking" -t task -p 0 --json
bd create "Implement Analyst agent role" -t task -p 0 --json
bd create "Implement Trader agent role" -t task -p 0 --json
bd create "Implement Risk Manager agent role" -t task -p 0 --json
bd create "Implement round-robin debate loop" -t task -p 0 --json
bd create "Implement cron-based quest scheduling" -t task -p 0 --json
bd create "Implement event-driven quest triggers" -t task -p 0 --json
bd create "Implement quest state persistence" -t task -p 0 --json
bd create "Implement Redis job queue" -t task -p 0 --json
bd create "Implement priority levels (CRITICAL > HIGH > NORMAL > LOW)" -t task -p 0 --json
bd create "Implement goroutine pool with concurrency limits" -t task -p 0 --json
bd create "Implement distributed locks" -t task -p 0 --json
bd create "Serialize AI session state" -t task -p 1 --json
bd create "Implement session resumption" -t task -p 1 --json
bd create "Implement session lifecycle management" -t task -p 1 --json
bd create "Implement Go ↔ mlx_lm.server HTTP communication" -t task -p 1 --json
bd create "Implement quantized model loading" -t task -p 1 --json
bd create "Implement watchdog filtering" -t task -p 1 --json
bd create "Implement daily loss cap" -t task -p 0 --json
bd create "Implement max drawdown halt" -t task -p 0 --json
bd create "Implement consecutive-loss pause" -t task -p 0 --json
bd create "Implement position-size throttle" -t task -p 0 --json
bd create "Expose readiness endpoints" -t task -p 0 --json
bd create "Expose risk primitives" -t task -p 0 --json
bd create "Expose cleanup endpoints" -t task -p 0 --json
bd create "Expose arbitrage primitives" -t task -p 0 --json
bd create "Define IndicatorProvider interface" -t task -p 1 --json
bd create "Implement existing provider adapter" -t task -p 1 --json
bd create "Add provider selection config" -t task -p 1 --json

# EPIC 3: STRATEGY & UI
bd create "Codify scalping skill.md" -t task -p 0 --json
bd create "Implement order book imbalance detection" -t task -p 0 --json
bd create "Implement tight stop-loss execution" -t task -p 0 --json
bd create "Codify sum-to-one arbitrage skill.md" -t task -p 0 --json
bd create "Implement arbitrage trigger detection" -t task -p 0 --json
bd create "Implement FOK order execution" -t task -p 0 --json
bd create "Codify sentiment momentum skill.md" -t task -p 1 --json
bd create "Implement multi-indicator stack" -t task -p 1 --json
bd create "Implement Twitter sentiment analysis" -t task -p 1 --json
bd create "Implement /begin and /pause handlers" -t task -p 0 --json
bd create "Implement /summary and /performance handlers" -t task -p 0 --json
bd create "Implement /liquidate and /liquidate_all handlers" -t task -p 0 --json
bd create "Implement wallet management commands" -t task -p 0 --json
bd create "Implement quest and monitoring commands" -t task -p 0 --json
bd create "Implement /doctor diagnostic handler" -t task -p 0 --json
bd create "Implement action streaming format" -t task -p 0 --json
bd create "Implement quest progress updates" -t task -p 0 --json
bd create "Implement fund milestone alerts" -t task -p 0 --json
bd create "Implement risk event notifications" -t task -p 0 --json
bd create "Implement AI reasoning summaries" -t task -p 0 --json
bd create "Implement place_order tool endpoint" -t task -p 0 --json
bd create "Implement cancel_order tool endpoint" -t task -p 0 --json
bd create "Implement position snapshot tool" -t task -p 0 --json
bd create "Implement controlled liquidation tool" -t task -p 0 --json
bd create "Add goflux as dependency" -t task -p 1 --json
bd create "Implement GoFlux adapter" -t task -p 1 --json
bd create "Add parity tests between providers" -t task -p 1 --json
bd create "Implement cost tracking in ai_usage table" -t task -p 0 --json
bd create "Implement daily budget enforcement" -t task -p 0 --json
bd create "Implement monthly budget enforcement" -t task -p 0 --json
bd create "Expose /status budget display" -t task -p 0 --json
bd create "Implement virtual account tracking" -t task -p 0 --json
bd create "Implement paper execution simulation" -t task -p 0 --json
bd create "Implement paper trade recording" -t task -p 0 --json
bd create "Implement historical OHLCV replay" -t task -p 1 --json
bd create "Simulate AI decision loop" -t task -p 1 --json
bd create "Output performance metrics" -t task -p 1 --json
bd create "Store backtest results" -t task -p 1 --json
bd create "Implement wallet conviction scoring" -t task -p 1 --json
bd create "Implement cohort-level flow analysis" -t task -p 1 --json
bd create "Implement signal explanation generation" -t task -p 1 --json
bd create "Implement anti-manipulation filters" -t task -p 1 --json
bd create "Implement shadow mode execution" -t task -p 1 --json

# EPIC 4: PRODUCTION
bd create "Containerize agent and infra services" -t task -p 0 --json
bd create "Set up QuantVPS deployment" -t task -p 0 --json
bd create "Configure production Docker Compose" -t task -p 0 --json
bd create "Set up CI/CD pipeline" -t task -p 0 --json
bd create "Fund with minimal capital ($100 USDC)" -t task -p 0 --json
bd create "Implement kill switch monitoring" -t task -p 0 --json
bd create "Implement exchange resilience monitoring" -t task -p 0 --json
bd create "Implement bootstrap → growth → scale → mature transitions" -t task -p 1 --json
bd create "Implement phase-specific strategy adaptation" -t task -p 1 --json
bd create "Implement automatic capital scaling" -t task -p 1 --json
bd create "Optimize Go runtime performance" -t task -p 1 --json
bd create "Optimize Redis caching strategy" -t task -p 1 --json
bd create "Optimize database query performance" -t task -p 1 --json
bd create "Implement slow query logging" -t task -p 1 --json
bd create "Run gosec and gitleaks" -t task -p 0 --json
bd create "Implement rate limit monitoring" -t task -p 0 --json
bd create "Implement intrusion detection" -t task -p 0 --json
bd create "Implement emergency rollbacks" -t task -p 0 --json
bd create "Create operator guide" -t task -p 1 --json
bd create "Create security documentation" -t task -p 1 --json
bd create "Create troubleshooting guide" -t task -p 1 --json
bd create "Implement metrics collection" -t task -p 1 --json
bd create "Implement log aggregation" -t task -p 1 --json
bd create "Set up alerts" -t task -p 1 --json
bd create "Implement dashboards" -t task -p 1 --json
```

To link tasks as dependencies, run commands like:

```bash
# Example: 1.1 depends on nothing
bd dep add "" "1.1"

# Example: 1.2 depends on 1.1
bd dep add "1.1" "1.2"

# Example: 1.3 depends on 1.2
bd dep add "1.2" "1.3"

# Mark tasks as completed:
bd update "1.1" --status completed --json
bd update "1.2" --status completed --json
```

---

## Progress Tracking

### Quick Commands

```bash
# View all tasks
bd list --status open --json

# View blocked tasks
bd blocked --json

# View statistics
bd stats --json

# Update task status
bd update <task-id> --status in_progress --json
bd update <task-id> --status completed --json
```

### Epic Progress Checklist

- [ ] **Epic 1: FOUNDATION** (Weeks 1-2)
  - [ ] TASK 1.1-1.9 (9 tasks)
  - Total: 9 tasks

- [ ] **Epic 2: AGENTIC CORE** (Weeks 3-4)
  - [ ] TASK 2.1-2.10 (10 tasks)
  - Total: 10 tasks

- [ ] **Epic 3: STRATEGY & UI** (Weeks 5-6)
  - [ ] TASK 3.1-3.11 (11 tasks)
  - Total: 11 tasks

- [ ] **Epic 4: PRODUCTION** (Week 7+)
  - [ ] TASK 4.1-4.7 (7 tasks)
  - Total: 7 tasks

**Grand Total**: 37 tasks (planned breakdown: 9 + 10 + 11 + 7 = 37)
**Actual Tasks**: 79 tasks (including subtasks and breakdown)

---

## Next Steps

1. **Initialize BD beads repository**: Run the command set above to create all 79 tasks
2. **Link dependencies**: Execute dependency commands for each epic
3. **Start Epic 1**: Begin with TASK 1.1 (SQLite + Redis Infrastructure Setup)
4. **Track progress**: Use `bd stats` and `bd blocked` regularly
5. **Review weekly**: Check for blocked tasks, adjust priorities as needed

---

**Document Version**: 1.0
**Last Updated**: 2026-02-11
**Status**: Ready for task creation
