# NeuraTrade Master Plan: Autonomous AI Crypto & Polymarket Trading Bot

## Executive Summary
The financial landscape is undergoing a paradigm shift from high-frequency, deterministic algorithmic trading to autonomous, semantic "agentic" trading. This transition is driven by the emergence of Large Language Models (LLMs) capable of complex reasoning, planning, and tool utilization. This report details the comprehensive technical architecture for transforming a legacy CCXT-based arbitrage bot into a fully autonomous AI trading system. The proposed system integrates a high-capability LLM as the central cognitive engine, operating within a "tool-calling" paradigm where the bot’s infrastructure functions as a set of executable skills.

The architecture leverages a hybrid computational model, utilizing state-of-the-art cloud models like OpenAI’s GPT-5.3 and Anthropic’s Claude Opus 4.6 for high-leverage decision-making, while employing local, quantized models on Apple Silicon (MLX-LM) for routine monitoring to optimize costs. A core innovation of this design is the adoption of the skill.md open standard, which decouples the agent's intelligence from its tools, allowing for dynamic capability discovery and strictly defined safety boundaries.   

The system is designed to operate across both centralized cryptocurrency exchanges (via CCXT) and decentralized prediction markets (Polymarket), exploiting unique arbitrage opportunities such as "sum-to-one" discrepancies in conditional token markets. To support continuous, autonomous operation, the architecture uses Redis for short-term, high-velocity context and a durable SQL store for long-term persistence (SQLite by default, with sqlite-vec for local vector retrieval when semantic memory is enabled). Risk management is enforced through a dedicated "Super-Ego" agent role that vetoes unsafe actions, ensuring capital preservation remains paramount. The user interface acts as a command-and-control center via Telegram, providing real-time transparency into actions, risk checks, and progress.   

## Product Happy Path (North-Star UX)
1. User installs NeuraTrade via one-line bootstrap script.
2. User runs `NeuraTrade` in terminal, enters Telegram bot token, and receives a one-time auth code via Telegram.
3. User pastes auth code back into CLI; CLI binds local operator identity to Telegram chat securely.
4. User chooses providers/models for the agent. The CLI resolves currently available model options from a provider registry (for example, OpenAI + Anthropic first).
5. User pastes provider API keys; the system stores them encrypted and masked in UI/log output.
6. Telegram onboarding requires at least one Polymarket wallet, at least one exchange for scalping, and at least two exchanges for arbitrage.
7. System performs readiness checks: API key permissions (trade-only, no-withdraw), connectivity, balances/funding, rate-limit health.
8. After readiness passes, user starts autonomous mode with `/begin`; user can pause with `/pause` and emergency stop/liquidate with `/liquidate_all`.
9. User monitors live stream, 24-hour `/summary`, and can add/remove wallets or exchanges at any time.
10. User can run `/doctor` for guided diagnostics/remediation when configuration or connectivity issues appear.

## 1. Introduction to Autonomous Financial Agents

### 1.1 The Evolution of Algorithmic Trading
Historically, algorithmic trading has been defined by speed and deterministic logic. Market makers and arbitrageurs relied on hard-coded strategies—"if moving average X crosses moving average Y, then buy"—executed in microseconds. While effective in stable or trending regimes, these "black box" systems suffer from brittleness. They lack semantic understanding of the market; they cannot interpret a news headline, understand a regulatory shift, or recognize when a technical signal is invalidated by a fundamental event. They are purely reactive to numerical time-series data.

The advent of Large Language Models (LLMs) has introduced a new capability: reasoning. Unlike traditional algorithms, an LLM-based agent can perceive the market in multiple modalities—textual, numerical, and even visual. It can plan multi-step workflows, adapt to changing conditions without code rewrites, and explain its actions in natural language. This shift from "Algo" to "Agent" represents the next frontier in decentralized finance (DeFi) and automated trading.   

### 1.2 Project Objectives

The primary objective of this project is to construct a **NeuraTrade Generalist Trading Agent** capable of autonomous operation in the cryptocurrency and prediction markets. The specific goals are:

Autonomy: The system must operate continuously (24/7), managing its own schedule via "quests" (e.g., hourly scans, daily rebalancing) without human intervention.   

Adaptability: The agent must be able to switch strategies based on market regimes (e.g., from volatility scalping to stablecoin yield farming) by leveraging a library of modular skills.

Safety: Given the non-deterministic nature of LLMs, the system must enforce strict, deterministic safety rails—hard limits on position sizing, stop-losses, and authorized API actions.

Transparency: The "black box" problem must be solved by logging structured decision audit trails for every trade (market context, model decision, risk checks, execution result, and post-trade outcome), accessible via Telegram and internal reports.   

Hybrid Efficiency: The architecture must balance performance and cost by routing complex tasks to frontier cloud models (GPT-5.3, Opus 4.6) and routine tasks to local silicon (M1/M3 Pro).   

### 1.3 Scope of Operations
The agent will operate in two distinct market environments:

Cryptocurrency Spot/Futures: Utilizing the CCXT library to access major exchanges (Binance, Kraken, Coinbase), executing technical and sentiment-based strategies to do scalping & arbitrage human fund.

Prediction Markets (Polymarket): Utilizing the Polymath/Gamma APIs to trade on the outcome of real-world events. This involves unique "conditional token" mechanics and arbitrage strategies distinct from traditional financial assets.   

## 2. Computational Substrate: The LLM Backbone

The "brain" of the NeuraTrade autonomous trading system is the Large Language Model. As of early 2026, the landscape of foundational models has specialized, allowing for a strategic selection of models based on specific agent roles.

### 2.1 Frontier Models: The Cloud Tier
For high-stakes decision-making, architectural reasoning, and complex code synthesis, the system relies on the most capable models available via API.

#### 2.1.1 OpenAI GPT-5.3 (Codex)
GPT-5.3, often referred to as "Codex 5.3," represents the pinnacle of code generation and project-level synthesis. Research indicates that GPT-5.3 excels in "Global Project Synthesis"—the ability to understand and manipulate complex, multi-file codebases and execution environments.   

Role in System: GPT-5.3 is designated as the engine for the Trader and Execution agents. Its ability to generate precise, syntactically correct JSON payloads for API calls and structured tool-call responses is unmatched.

Capabilities: It supports a 64,000-token output limit, enabling it to write extensive logs and self-correction routines during complex trade executions. Its low-latency "turbo" modes make it suitable for the execution leg of the trade where speed is critical.   

#### 2.1.2 Anthropic Claude Opus 4.6
Claude Opus 4.6 is selected for its "Adaptive Thinking" architecture and massive context window (up to 1 million tokens). It is characterized by high reliability in reasoning tasks and a "safety-first" alignment, making it less prone to hallucination in critical scenarios.   

Role in System: Opus 4.6 serves as the Analyst and the Risk Manager. Its large context window allows it to ingest vast amounts of unstructured data—whitepapers, news feeds, long Twitter threads, and governance proposals—without truncation.

Native Multi-Agent Logic: Opus 4.6 has demonstrated superior performance in simulating multi-perspective debates, making it ideal for the internal "boardroom" discussions between the Bullish Analyst and the Bearish Risk Manager.   

### 2.2 Local Inference: The Edge Tier
To mitigate the high costs and network latency associated with continuous cloud API calls, the architecture incorporates local inference capabilities. This "Hybrid AI" approach ensures the bot remains functional and cost-effective for routine monitoring.

#### 2.2.1 Apple Silicon and MLX-LM
The system is optimized for deployment on Apple M-series hardware (M1 Pro through M3 Max). Apple's unified memory architecture and potent Neural Engine allow for the efficient execution of high-parameter models locally.   

Framework: The system utilizes MLX-LM, Apple's open-source framework designed specifically for Apple Silicon. MLX allows for unified memory access, significantly speeding up inference for transformer models. The Go service communicates with the local MLX model via its built-in HTTP server (`mlx_lm.server`), keeping the AI orchestration entirely in Go.   

Quantization: By utilizing 4-bit or 8-bit quantized versions of open-source models (e.g., Llama 3, Mistral, or specialized financial fine-tunes), the agent can run a "Watchdog" process. This local agent monitors the WebSocket data stream for specific trigger conditions (e.g., "RSI < 30" or "Volume Spike > 500%").

Cost Efficiency: Continuous market monitoring via GPT-5.3 would be prohibitively expensive. The local MLX model acts as a filter, only waking the expensive cloud "brain" when a high-probability setup is detected. This "Cut the Cord" approach can reduce API costs by over 90%.   

| Component | Model Implementation | Role | Hardware/Host |
|---|---|---|---|
| Execution Engine | GPT-5.3 (Codex) | Code Gen, API Formatting | Cloud API |
| Strategic Core | Claude Opus 4.6 | Analysis, Risk Assessment | Cloud API |
| Watchdog | Llama 3 / Mistral (Quantized) | Real-time Monitoring | Local Mac (MLX) |
## 3. The Agentic Workflow: Architecture and Design

To manage the complexity of financial markets, NeuraTrade eschews a monolithic design in favor of a Multi-Agent System (MAS). This approach mimics a professional trading desk, where distinct roles collaborate to execute a strategy.

### 3.1 The Multi-Agent Roles
Research into LLM trading agents highlights that role specialization reduces hallucination and improves performance. The system is composed of three primary agent personas:   

The Analyst (The Eye):

Primary Directive: Information Synthesis.

Inputs: News feeds (RSS, Twitter API), Market Data (OHLCV), On-chain metrics.

Function: It continuously scans the environment. It does not have write access to the trading API. Its output is a Market Regime Classification (e.g., "Bearish Volatile") and Trade Ideas (e.g., "Long ETH due to spot ETF approval rumors").

Tools: search_news, get_historical_prices, analyze_sentiment.   

The Trader (The Hand):

Primary Directive: Execution Efficiency.

Inputs: Trade Ideas from the Analyst, Order Book depth.

Function: It converts abstract ideas into concrete actions. It decides how to buy—Market vs. Limit, splitting orders (TWAP/VWAP) to minimize slippage.

Tools: place_order, cancel_order, get_order_book.   

The Risk Manager (The Conscience):

Primary Directive: Capital Preservation.

Inputs: Proposed Trade Payloads, Portfolio State, Risk Rules (SQL store).

Function: It acts as a veto gate. Every trade instruction from the Trader must be signed off by the Risk Manager. It calculates exposure, leverage, and drawdown impact.

Tools: check_risk_limits, get_portfolio_balance, kill_switch.

### 3.2 Coordination and Debate
The agents interact through a structured Debate Protocol. Before any capital is committed, a "round-robin" dialogue occurs within the system's short-term memory (Redis).   

Step 1: Proposal. The Analyst posts a signal: "Polymarket 'Trump vs. Biden' spread has widened to 5%. Arbitrage opportunity detected."

Step 2: Strategy. The Trader formulates a plan: "Buy 'Yes' on Trump at $0.45 and 'Yes' on Biden at $0.40. Cost basis $0.85. Risk-free profit $0.15."

Step 3: Review. The Risk Manager evaluates: "Portfolio is already 40% exposed to political prediction markets. Hard limit is 50%. The trade size of $1000 keeps us under the limit. Slippage calculation suggests effective price will be $0.87. Profit margin is still acceptable. APPROVED."

Step 4: Consensus. The execution command is signed and sent to the API.

This adversarial loop prevents "impulsive" trading behaviors common in single-prompt LLM setups.   

### 3.3 Autonomous Quests
To maintain agency over long timeframes, the system utilizes a **Quest System**. A "Quest" is a high-level directive with a specific duration, objective, and success criteria, managed by the system's scheduler and persisted in the SQL store.

#### Quest Cadence Tiers

| Tier | Frequency | Examples |
|---|---|---|
| **Micro** | Every 1–5 minutes | Scan for arbitrage spreads, check open order fills, WebSocket health ping |
| **Hourly** | Every hour | Portfolio health check, rebalance drift detection, sentiment snapshot |
| **Daily** | Once per day | Daily PnL report to Telegram, strategy performance review, risk limit recalibration |
| **Weekly** | Once per week | Full portfolio rebalance assessment, strategy rotation review, fee audit |
| **Milestone** | Event-driven | "Grow fund from $100 → $5000", "Achieve 10% monthly return", "Complete 50 profitable trades" |

#### Quest Types

**Routine Quests** (time-triggered): Scheduled on cron-like intervals. Examples: "Perform hourly portfolio health check." "Scan for Polymarket arbitrage every 5 minutes."

**Triggered Quests** (event-driven): Activated by market conditions. Examples: "Volatility detected > 5% → Initiate 'Safe Harbor' protocol." "New Polymarket market with >$100k volume detected → Analyze and propose position."

**Goal-Oriented Quests** (milestone-driven): Long-running objectives with measurable targets. Examples: "Accumulate 10 ETH over 48 hours using TWAP strategy." "Grow fund from $100 USDC to $500 USDC." These quests decompose into sub-quests automatically.

**Fund Milestone System**: The AI tracks fund growth milestones and adapts strategy aggressiveness accordingly:
- **Bootstrap** ($0–$100): Conservative, small position sizes, learning phase.
- **Growth** ($100–$1,000): Moderate risk, diversified strategies.
- **Scale** ($1,000–$10,000): Full strategy suite active, including scalping.
- **Mature** ($10,000+): Capital preservation priority, yield-focused.

The progress of each quest is tracked in the SQL store with full state persistence, ensuring that if the bot is restarted, it "remembers" its ongoing missions and resumes exactly where it left off.

### 3.4 AI Session Management
The system manages **AI Sessions** as first-class entities. Each session represents a continuous reasoning context for the AI agent.

**Session Lifecycle:**
1. **Init**: A new session is created when the bot starts or when a new quest requires fresh context.
2. **Active**: The session holds the current reasoning chain, loaded skills, and working memory.
3. **Suspended**: When the AI switches to a higher-priority quest, the current session is serialized to the SQL store.
4. **Resumed**: On return, the session is deserialized with full context intact.
5. **Archived**: Completed sessions are compressed and stored for post-game analysis.

**Session State** (persisted in SQL store):
- Current quest ID and progress
- Loaded skill.md references
- Conversation/reasoning chain (last N turns)
- Active positions relevant to this session
- Market snapshot at session start

### 3.5 Parallel Job System
The autonomous agent must handle **multiple concurrent activities** without conflict. The Job System manages this:

**Job Queue** (Redis-backed):
- Each quest spawns one or more **Jobs** (atomic units of work).
- Jobs are categorized: `MARKET_SCAN`, `TRADE_EXECUTE`, `RISK_CHECK`, `REPORT`, `REBALANCE`.
- Jobs have priority levels: `CRITICAL` (risk/kill-switch) > `HIGH` (trade execution) > `NORMAL` (scans) > `LOW` (reports).

**Concurrency Control:**
- Redis distributed locks prevent conflicting operations (e.g., two quests trading the same pair).
- A **Job Scheduler** (Go goroutine pool) manages parallel execution with configurable concurrency limits.
- **Asset-level locks**: Only one job can modify a position in a given asset at a time.

**Persistence:**
- Job state is checkpointed to the SQL store every N seconds.
- On restart, incomplete jobs are replayed from their last checkpoint.
- Failed jobs are retried with exponential backoff (max 3 retries).

## 4. The Skill Paradigm: Standardizing Agent Capabilities
A critical innovation in modern agent engineering is the standardization of tool definitions. The system adopts the skill.md open standard to manage its capabilities.

### 4.1 The skill.md Specification
skill.md files are concise markdown specifications that serve as "instruction manuals" for the agent. Instead of hard-coding tool definitions into the system prompt (which consumes valuable context tokens), skills are stored as files in the repository and loaded dynamically.   

Each skill.md file contains:

Metadata: YAML frontmatter defining the skill name and description.

Instruction Body: Natural language explanation of what the skill does and why it should be used.

Parameters: Detailed schema of input arguments.

Best Practices: Guidelines on usage (e.g., "Do not use this tool during high volatility").

This format leverages the LLM's natural ability to read documentation. It treats the agent as a "new employee" reading a handbook, rather than a computer executing a function call.   

### 4.2 Progressive Disclosure
The system utilizes Progressive Disclosure to manage context window efficiency.

Discovery Phase: The agent is presented with a lightweight list of available skills (names and short descriptions only).

Selection Phase: When the agent decides it needs to perform a specific action (e.g., "I need to check the order book"), it requests the full skill definition.

Loading Phase: The system reads the specific skill.md file (e.g., skills/polymarket/get_order_book/skill.md) and injects it into the active context.

Execution Phase: The agent utilizes the detailed instructions to format the API call correctly.

This mechanism allows the agent to have access to hundreds of tools without overwhelming its context window.   

### 4.3 Example Skill Definition
Below is an example of a skill.md file for placing a trade on Polymarket:

name: polymarket_place_order description: Places a Limit or FOK order on the Polymarket CLOB.
Polymarket Order Placement
Use this skill to execute a trade on the Polymarket Central Limit Order Book (CLOB). This interacts with the Layer 2 exchange functionality.

Parameters
condition_id (string, required): The unique identifier for the market condition.

side (string, required): "BUY" or "SELL".

price (float, required): The limit price in USDC (range 0.00 to 1.00).

size (float, required): The number of shares to trade.

order_type (string): "GTC" (Good Till Cancel) or "FOK" (Fill or Kill). Default is "GTC".

Best Practices
Check Liquidity: Before placing a large order (> $1000), always call get_order_book to estimate slippage.

Arbitrage: If executing an arbitrage strategy, use "FOK" (Fill or Kill) to ensure you do not get legged out of the position (i.e., one side fills but the other does not).

Pricing: Prices on Polymarket are probabilities. A price of 0.60 implies a 60% chance of the event occurring.

Safety Constraints
Do not exceed the daily loss limit defined in user settings.

Orders result in immediate settlement; ensure sufficient USDC allowance is approved for the Proxy Wallet.

By externalizing the logic into skill.md files, the developer can update the bot's behavior (e.g., adding a new safety check) simply by editing a text file, without modifying the core codebase or retraining the model.   

## 5. Market Microstructure and Data Ingestion
The effectiveness of the agent is inextricably linked to the quality and timeliness of its data. The system is designed to ingest data from two radically different market structures.

### 5.1 Cryptocurrency Markets (CCXT)
The system utilizes the CCXT (CryptoCurrency eXchange Trading) library to interface with centralized exchanges. CCXT acts as a normalization layer, translating the idiosyncratic API responses of various exchanges into a unified standard.   

Unified Data Models: Whether trading on Binance or Kraken, the agent sees the same JSON structure for an OrderBook or Ticker. This allows the "Trader" agent to be exchange-agnostic.

WebSockets: For real-time data, the Go backend service manages WebSocket connections (via ccxt.pro). It subscribes to diff-depth channels to maintain a local, real-time mirror of the order book.

Rate Limits: The system respects exchange-specific rate limits (which vary by venue, endpoint, and account tier) by managing local token buckets and backoff policies. If limits are approached, the Risk Manager throttles non-essential queries and can temporarily disable affected strategy lanes.

### 5.2 Polymarket and Prediction Markets
Polymarket operates on a fundamentally different architecture known as the Conditional Token Framework (CTF), built on the Gnosis standard and deployed on the Polygon blockchain.   

#### 5.2.1 The Conditional Token Framework
In this system, outcomes are tokenized. If you deposit 1 USDC into a binary market (Question: "Will X happen?"), you receive two tokens: YES_TOKEN and NO_TOKEN.

Minting: 1 USDC = 1 YES + 1 NO.

Redemption: If YES wins, YES_TOKEN becomes redeemable for 1 USDC, and NO_TOKEN becomes worthless.

Merging: At any time, holding 1 YES + 1 NO allows you to merge them back into 1 USDC.

This mathematical certainty (P(YES)+P(NO)=1) underpins the "Sum-to-One" arbitrage strategy. If the market prices diverge (e.g., YES is $0.40, NO is $0.55), the sum is $0.95. The bot can buy both for $0.95 and merge them for $1.00, securing a risk-free profit.   

#### 5.2.2 Polymarket API Architecture
The integration involves three specific API layers :   

Gamma API (Metadata): A GraphQL/REST endpoint used for market discovery. The Analyst agent queries this to find markets based on volume, category (e.g., "Politics"), or liquidity depth.

Endpoint: https://gamma-api.polymarket.com

Usage: "Find all active markets related to 'Bitcoin' with > $100k volume."

CLOB API (Execution): The Central Limit Order Book API. This is where trading happens off-chain for speed.

Endpoint: https://clob.polymarket.com

Authentication: Requires L2 (Layer 2) headers, signed with an HMAC secret derived from the user's Polygon wallet private key.

Limits: Use published Polymarket limits from official documentation at runtime and enforce them through configurable client-side rate controls.   

Data API (Positions): Used for portfolio reconciliation.

Endpoint: https://data-api.polymarket.com

Usage: "What are my current holdings and PnL?"

### 5.3 Multi-Modal Data Enrichment Pipeline
The agent's perception extends far beyond numbers. Each new data source becomes a **skill.md** — the AI learns to call tools like `search_twitter`, `get_reddit_sentiment`, `check_macro_calendar` without any core agent changes.

**Progressive Data Integration:**

| Phase | Data Source | Implementation | Alpha Signal |
|---|---|---|---|
| **Phase 1** | CCXT market data + Polymarket | WebSocket + REST (existing) | Price action, arbitrage |
| **Phase 2** | Twitter/X API | Go HTTP client, sentiment scoring via LLM | Breaking news, whale alerts |
| **Phase 3** | Reddit (r/cryptocurrency), News RSS | Go RSS parser, Reddit API | Community sentiment, narratives |
| **Phase 4** | Macro data (FRED API, CPI, interest rates) | Go HTTP client, scheduled pulls | Macro regime detection |
| **Phase 5** | On-chain data (whale tracking, DEX flows) | Go + blockchain RPCs | Smart money tracking |

**Why multi-source increases win rate:** A pure-technical bot sees "RSI = 28, buy." NeuraTrade's AI sees "RSI = 28, **but** SEC just announced new crypto regulations on Twitter 15 minutes ago → **override**, stay flat." This semantic override is the key advantage.

**Data Flow:**
```
Market Data (CCXT WebSockets) ──┐
Twitter/X API ──────────────────┤
Reddit API ─────────────────────┤──▶ Redis Pub/Sub ──▶ AI Analyst Agent
RSS News Feeds ─────────────────┤      (normalized)     (reasoning loop)
Macro Data (FRED API) ──────────┤
Polymarket Gamma API ───────────┘
```

**Textual Data:** The system connects to RSS feeds, Twitter lists (via API), and governance forums. The "Analyst" agent uses SQLite + `sqlite-vec` embeddings (with optional Redis hot-cache) to search for semantic relevance (e.g., "Retrieve recent news about 'SEC regulation'").

**Visual Data:** Using the vision capabilities of frontier models, the agent can analyze chart images. It can identify patterns (e.g., "Head and Shoulders") or support/resistance zones that are difficult to describe purely mathematically.   

### 5.4 Market Data Lifecycle and Cleanup Policy
To prevent uncontrolled storage growth and stale-signal pollution, data retention is policy-driven:

- **Hot window:** Keep recent high-frequency market and funding data for active strategies.
- **Warm window:** Keep reduced granularity for short-term analytics and debugging.
- **Cold/archive:** Keep only summarized artifacts needed for performance review and audit.
- **Delete aggressively:** Data outside retention policy is deleted by scheduled cleanup jobs.

Operational rules:

- Cleanup runs on a fixed schedule and can also be triggered manually by admin API.
- Retention hours are configurable per data domain (market data, funding rates, opportunities).
- Strategy and trade audit records are retained longer than raw tick/orderbook cache.
- No strategy logic may depend on data that violates retention policy assumptions.

## 6. System Architecture: The Technical Core
The system is architected as a distributed microservices application, prioritizing modularity, concurrency, and persistence. The existing NeuraTrade codebase (Go + TypeScript/Bun) provides the foundation, which will be extended with AI agent capabilities. **The stack is entirely Go + TypeScript — no Python.**

### 6.1 High-Level Architecture
The system consists of three primary services communicating via Redis:

**The Agent Service (Go — AI Core):**
- Implements the **tool-calling loop** natively in Go using direct HTTP calls to LLM APIs (OpenAI, Anthropic). No LangChain, no Python wrappers.
- The loop: `Prompt → LLM API → Parse tool_calls → Execute Go functions → Append results → Loop until done`.
- Parses and loads skill.md files dynamically from the filesystem.
- Manages AI sessions, quest scheduling, and the parallel job system using goroutines.
- Communicates with the local MLX watchdog via its HTTP server endpoint (`localhost:8080/v1/chat/completions` — OpenAI-compatible).

**The Infrastructure Service (Go — existing backend-api):**
- Manages high-performance networking and WebSocket connections to exchanges.
- Hosts the Telegram Bot API server.
- Handles cryptographic signing and transaction broadcasting.
- Provides the CCXT normalization layer.
- Go's superior concurrency primitives (goroutines + channels) handle hundreds of live data feeds simultaneously.

**The Exchange Bridge (TypeScript/Bun — existing ccxt-service):**
- Wraps the CCXT library for unified exchange access.
- Provides HTTP API for the Go services to interact with 100+ exchanges.
- Handles exchange-specific quirks and rate limiting.

> **Note:** Persistence standard is SQLite-first. Redis remains for ephemeral queue/cache/locks, while durable state and learning memory are stored in SQLite.

### 6.1.1 Why Go-Native AI (No Python)
The decision to implement the AI agent orchestration in Go (rather than Python/LangChain) is deliberate:

| Concern | Python (LangChain) | Go (Native) |
|---|---|---|
| **Deployment** | Requires Python runtime, pip, venv | Single static binary |
| **Concurrency** | GIL-limited, asyncio complexity | Goroutines (millions concurrent) |
| **Memory** | High (PyTorch, numpy overhead) | Low (no ML frameworks needed) |
| **Latency** | HTTP overhead + framework abstractions | Direct HTTP calls, zero abstraction tax |
| **Complexity** | LangChain abstractions change frequently | Simple loop, full control |

**The AI agent is just an HTTP client.** The tool-calling loop is ~200 lines of Go:
1. Build prompt from skill.md + context
2. POST to OpenAI/Anthropic API
3. Parse `tool_calls` from response
4. Execute corresponding Go functions
5. Append tool results to conversation
6. Repeat until the model returns a final answer

For local inference, MLX-LM runs as a separate process (`mlx_lm.server`) exposing an OpenAI-compatible HTTP endpoint. The Go agent calls it identically to cloud APIs — no Python imports, no bindings.

### 6.2 Service Communication
The services are decoupled using a **Redis Message Queue**.

**Market Data:** The Go infrastructure service pushes normalized market ticks to Redis Pub/Sub channels (e.g., `market:eth_usdc:ticker`).

**Agent Wake-up:** The Agent service subscribes to these channels. Upon receiving significant data (filtered by the local MLX model), it triggers the LLM reasoning loop.

**Execution:** When the Agent decides to trade, it pushes a JSON command payload to a Redis queue (`orders:execute`). The infrastructure service pops this payload, signs it, and executes it via the exchange API.

**Job Coordination:** Quest and Job state changes are published to Redis channels (`jobs:status`, `quests:progress`) enabling real-time Telegram streaming.

### 6.3 Local vs. Cloud Infrastructure
**Development:** The entire stack (including local LLMs) runs on an Apple M3 Max MacBook Pro. Default dev stack uses SQLite + Redis.

**Production:** The system is containerized (Docker). The services run on a single VPS with SQLite + Redis for durable persistence and queue/cache operations.

**VPS Strategy:** For low-latency execution, the Go service executes on a QuantVPS instance located in close proximity to exchange servers (e.g., AWS Tokyo for Binance) or Polygon RPC nodes. This minimizes the "tick-to-trade" latency, crucial for arbitrage and scalping.   

## 7. Memory Systems: The Cognitive Architecture
A major limitation of raw LLMs is "statelessness"—they have no memory of past interactions. To create a continuous, learning agent, the system implements a Dual-Memory architecture.

### 7.1 Short-Term Memory: Redis
Redis serves as the "Working Memory" or RAM of the agent. It stores high-velocity, ephemeral data.

Conversation Context: Stores the sliding window of the current reasoning chain. As new thoughts are generated, old ones are summarized and moved to long-term memory or discarded.

Market Cache: Uses Redis Sorted Sets (ZSET) to maintain time-series data of recent prices (O(logN) access). This allows the agent to ask, "What was the price volatility over the last 1 hour?" instantly without querying an external API.

Concurrency Locks: Redis atomic locks (SETNX) are used to prevent race conditions. For example, if the "Arbitrage Quest" is running, it acquires a lock on the ETH-USDC pair so that the "Rebalance Quest" does not try to sell the same asset simultaneously.   

### 7.2 Long-Term Memory: SQL Store (SQLite Default)
The SQL store serves as the "Episodic Memory" or Hard Drive. It stores persistent state that must survive system reboots. SQLite is the default durable store for this project; use WAL mode, migration discipline, and periodic snapshot/backup policy for reliability.

**Full Schema Design:**

```sql
-- Core identity and credentials
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    telegram_id TEXT UNIQUE NOT NULL,
    risk_level TEXT DEFAULT 'medium',  -- low/medium/high/custom
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Encrypted exchange and AI provider credentials
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    provider TEXT NOT NULL,           -- 'binance', 'kraken', 'openai', 'anthropic'
    provider_type TEXT NOT NULL,      -- 'exchange' or 'ai_model'
    encrypted_key BLOB NOT NULL,      -- AES-256-GCM encrypted
    encrypted_secret BLOB,            -- AES-256-GCM encrypted
    permissions TEXT,                 -- 'trade_only', 'read_only'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Connected wallets (CEX accounts + on-chain wallets)
CREATE TABLE wallets (
    id INTEGER PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    chain TEXT NOT NULL,              -- 'polygon', 'ethereum', 'binance', 'cex'
    address TEXT NOT NULL,
    wallet_type TEXT NOT NULL,        -- 'trading', 'watch_only'
    label TEXT,                       -- user-friendly name
    encrypted_private_key BLOB,       -- only for trading wallets, AES-256-GCM
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Every trade ever executed (append-only audit trail)
CREATE TABLE trades (
    id INTEGER PRIMARY KEY,
    quest_id INTEGER REFERENCES quests(id),
    strategy_id TEXT NOT NULL,        -- which skill.md was used
    strategy_version TEXT,            -- version hash of the skill.md
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,             -- 'ETH/USDC', 'polymarket:condition_id'
    side TEXT NOT NULL,               -- 'buy' or 'sell'
    entry_price REAL NOT NULL,
    exit_price REAL,
    size REAL NOT NULL,
    fees REAL DEFAULT 0,
    pnl REAL,                         -- realized PnL
    cost_basis REAL,                  -- for tax reporting
    status TEXT DEFAULT 'open',       -- 'open', 'closed', 'cancelled'
    opened_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME
);

-- AI reasoning log (append-only, never deleted)
CREATE TABLE thoughts (
    id INTEGER PRIMARY KEY,
    session_id INTEGER REFERENCES ai_sessions(id),
    quest_id INTEGER REFERENCES quests(id),
    trade_id INTEGER REFERENCES trades(id),
    role TEXT NOT NULL,                -- 'analyst', 'trader', 'risk_manager'
    content TEXT NOT NULL,             -- the actual reasoning
    model_used TEXT,                   -- 'gpt-4o', 'claude-sonnet', 'mlx-local'
    tokens_used INTEGER,
    cost_usd REAL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Quest state tracking
CREATE TABLE quests (
    id INTEGER PRIMARY KEY,
    type TEXT NOT NULL,                -- 'routine', 'triggered', 'goal'
    cadence TEXT,                      -- 'micro', 'hourly', 'daily', 'weekly', 'milestone'
    prompt TEXT NOT NULL,
    target_value REAL,                 -- for milestone quests
    current_value REAL DEFAULT 0,
    status TEXT DEFAULT 'pending',     -- 'pending', 'active', 'completed', 'failed'
    checkpoint BLOB,                   -- serialized state for resume
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

-- AI session persistence
CREATE TABLE ai_sessions (
    id INTEGER PRIMARY KEY,
    quest_id INTEGER REFERENCES quests(id),
    status TEXT DEFAULT 'active',      -- 'active', 'suspended', 'archived'
    context BLOB,                      -- serialized conversation chain
    loaded_skills TEXT,                -- JSON array of skill.md paths
    market_snapshot BLOB,              -- market state at session start
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME
);

-- AI model usage tracking (cost management)
CREATE TABLE ai_usage (
    id INTEGER PRIMARY KEY,
    model TEXT NOT NULL,
    tokens_input INTEGER,
    tokens_output INTEGER,
    cost_usd REAL,
    purpose TEXT,                      -- 'market_scan', 'trade_decision', 'risk_check'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Historical market data (selective, for backtesting)
CREATE TABLE market_history (
    id INTEGER PRIMARY KEY,
    symbol TEXT NOT NULL,
    timeframe TEXT NOT NULL,           -- '1m', '5m', '1h', '1d'
    open REAL, high REAL, low REAL, close REAL, volume REAL,
    timestamp DATETIME NOT NULL,
    UNIQUE(symbol, timeframe, timestamp)
);

-- Fund milestone tracking
CREATE TABLE fund_milestones (
    id INTEGER PRIMARY KEY,
    phase TEXT NOT NULL,               -- 'bootstrap', 'growth', 'scale', 'mature'
    target_value REAL NOT NULL,
    reached_at DATETIME,
    strategy_config TEXT               -- JSON: risk params for this phase
);
```

**What goes in SQL store vs Redis:**

| SQL Store (Persistent) | Redis (Ephemeral) |
|---|---|
| Users, API keys, wallets | Live market ticks, order book cache |
| Trade history, PnL | Conversation context (sliding window) |
| AI sessions, thoughts | Pub/Sub channels, job queue |
| Quest state, fund milestones | Distributed locks, rate limiters |
| Historical OHLCV (selective) | Concurrency control |
| AI usage / cost tracking | Real-time portfolio cache |

**Historical Data Strategy:** Store OHLCV candles (1m, 5m, 1h, 1d) only for actively-traded pairs. Store market condition snapshots at trade decision points for post-game analysis. Do **not** store raw order book history (too large) — let the AI query live data via CCXT when needed.

**Vector Database:** The system uses `sqlite-vec` (SQLite-native vectors) to store embeddings of past market conditions. This allows the agent to perform semantic retrieval such as "Find past instances where RSI was < 20 and news sentiment was negative." If similar setups historically ended in losses, the risk gate can downgrade or reject the current action.

## 8. Execution and Strategy Implementation
The core value of NeuraTrade lies in its trading strategies. The system supports modular strategy definitions as skill.md files, swapped in and out by the "Analyst" based on market conditions.

### 8.1 Polymarket Strategies
Polymarket offers unique opportunities unrelated to broader crypto market beta.

**Sum-to-One Arbitrage:**
- **Concept:** In a binary market, Price(YES) + Price(NO) should equal 1.00.
- **Trigger:** If P(YES)+P(NO)<0.98 (accounting for ~2% spread/fees).
- **Action:** Buy equal amounts of YES and NO.
- **Profit Math:** `Profit = 1.00 − (P_yes + P_no + Fees)`.
- **Execution:** Requires "Fill-or-Kill" (FOK) orders to avoid execution risk (being filled on one side but not the other).

**Cross-Platform Arbitrage:**
- **Concept:** Compare odds between Polymarket (Crypto/Polygon) and Kalshi (Regulated US/CFTC).
- **Mechanism:** If Polymarket gives Trump 45% odds and Kalshi gives 50% odds, there is a theoretical arbitrage. However, capital controls make direct arbitrage difficult. The agent treats this as a "Relative Value" trade.

**Endgame Arbitrage:**
- **Concept:** Buying "certain" outcomes (99% odds) for a 1% yield.
- **Risk:** "Fat finger" risk or resolution disputes. The "Analyst" agent must read the specific resolution rules (Gamma API) to ensure no ambiguity exists.

### 8.2 Crypto Strategies
The agent implements standard quantitative strategies enhanced by semantic understanding.

**Scalping (High-Frequency Short-Term):**
- **Concept:** Exploit micro price movements on 1m–5m timeframes. The AI identifies high-probability setups using order book depth, volume spikes, and momentum indicators.
- **Entry:** RSI divergence on 1m chart + volume confirmation + order book imbalance > 60%.
- **Exit:** Fixed take-profit (0.1%–0.3%) or trailing stop. Maximum hold time: 15 minutes.
- **Risk:** Tight stop-loss (0.05%–0.1%). Maximum 3 concurrent scalp positions. The Risk Manager enforces a "no scalping" rule during high-impact news events.
- **Execution:** Requires the lowest-latency path. The Go service handles order placement directly, bypassing the full debate protocol for pre-approved scalp parameters.

**Sentiment-Weighted Momentum:**

Technicals: Agent monitors a multi-indicator futures-ready stack (SMA/EMA, RSI, MACD, ATR, Bollinger Bands, Stochastic, OBV as baseline; expand with ADX/Ichimoku/SuperTrend where validated).

Sentiment: Agent analyzes Twitter/News velocity.

Synthesis: If Technicals = Bullish AND Sentiment = Bullish, Execute High Confidence Buy. If Technicals = Bullish but Sentiment = Bearish (e.g., "CEO Arrested"), the agent overrides the technical signal and stays flat. This is the key advantage of the LLM agent over a standard bot.

Mean Reversion:

Using Bollinger Bands and RSI to fade extreme moves. The "Risk Manager" ensures we do not "catch a falling knife" by checking for breaking news that justifies the extreme move.

### 8.3 Execution Philosophy: Multi-Engine, Not Single-Style Trader
NeuraTrade is not designed as one directional "trader" style. It is a portfolio of strategy engines with different market assumptions:

- **Scalping engine:** Short-horizon microstructure opportunities on a single exchange.
- **Arbitrage engine:** Cross-venue spread capture with near market-neutral exposure.
- **Prediction-market engine:** Event-driven probability mispricing and resolution-aware positioning.

The orchestrator allocates risk budget across engines dynamically. Under stress, it reduces or disables the weakest engine first instead of forcing one strategy style to run in all regimes.

### 8.4 Technical Analysis Engine Strategy (GoFlux + Compatibility Path)
For futures-heavy operation, indicator breadth and robust backtesting matter more than a minimal MA/RSI stack.

- **Primary direction:** adopt `github.com/irfndi/goflux` as the long-term TA engine target because it provides broader indicator coverage and built-in strategy/backtest tooling.
- **Compatibility-first rollout:** keep the current TA provider path available during migration to reduce operational risk.
- **Adapter architecture:** introduce `IndicatorProvider` abstraction so strategy code does not bind directly to one TA library.
- **Go-only guarantee:** all TA/backtest execution remains in Go; no Python runtime or wrappers in production pipeline.

Provider policy:

| Context | Preferred TA Provider | Rationale |
|---|---|---|
| Production low-latency signal path (initial migration period) | Existing provider path (current implementation) | Stability while adapter and parity tests are completed |
| Research, backtesting, and advanced futures indicators | GoFlux | Richer indicator set and strategy evaluation primitives |
| Post-parity production path | GoFlux (target) | Single-engine simplicity once reliability/perf gates pass |

Migration gates before GoFlux becomes default for production execution:

- Indicator parity checks pass for baseline set used in live strategies.
- Backtest and paper-trade performance are equal or better on risk-adjusted metrics.
- Runtime latency and resource usage remain inside SLO for selected strategy lanes.
- Failure-mode tests pass (stale data, missing candles, burst volatility windows).

## 9. Risk Management and Safety Protocols
In an autonomous system controlling real funds, safety is the primary requirement. The architecture implements a "Defense in Depth" strategy.

### 9.1 The Risk Manager Agent
This agent has the final veto power. It is prompted with a strict "Constitution" of safety rules.

Input: The Trader's proposed JSON payload.

Process: It runs a checklist:

Is the position size within the Kelly Criterion limit?

Is the total portfolio leverage under 2x?

Is the asset on the "Blacklist" (e.g., highly illiquid coins)?

Does the Stop Loss percentage align with volatility?

Output: APPROVED or REJECTED with reason. The system cannot execute a trade without this cryptographic signature from the Risk module.

Position-sizing policy (deterministic):

- **Base risk-per-trade:** default 0.5%-2.0% of current equity, strategy/profile dependent.
- **Sizing method:** volatility-adjusted sizing (ATR or equivalent) to normalize dollar risk across regimes.
- **Kelly policy:** use fractional Kelly only (for example 0.25x-0.50x Kelly), never full Kelly.
- **Hard cap:** absolute per-trade notional and leverage caps always apply even if Kelly suggests larger size.
- **Liquidity-aware clamp:** reduce size when book depth/slippage model exceeds configured cost thresholds.

### 9.2 Hard-Coded Circuit Breakers
Beyond the LLM's reasoning (which can fail/hallucinate), hard-coded checks exist in the Go infrastructure layer:

Max Drawdown Kill Switch: If Portfolio Value drops > 5% in 24 hours, the bot automatically closes all positions and shuts down.

Fat Finger Check: Orders deviating > 5% from the current market price are rejected automatically.

Rate Limiters: API calls are throttled to prevent IP bans.

### 9.3 Operational Security
**Key Storage:** API keys are encrypted using AES-256-GCM in the SQL store (`api_keys` table). They are decrypted only in memory at the moment of signing. The encryption key is derived from a user-provided passphrase via Argon2.

**Wallet Architecture — How `/connect` Works:**

**For Polymarket (`/connect_polymarket`):**
1. User provides their Polygon wallet address + CLOB API key + CLOB secret.
2. These are obtained from the Polymarket account settings page.
3. NeuraTrade stores them encrypted in the SQL store (`api_keys` + `wallets` tables).
4. When trading: The Go service uses the API key to sign CLOB orders via HMAC.
5. **NeuraTrade never holds custody.** The user's funds stay in their Polymarket proxy wallet.
6. The Polymarket proxy wallet uses a smart contract (Gnosis Safe or similar) — the bot is given allowance to trade, but **zero allowance to withdraw**.

**Telegram UX Example (Polymarket):**
- User runs: `/connect_polymarket <wallet_address> <clob_api_key> <clob_api_secret>`
- Bot responds with validation checklist:
  - Wallet format valid
  - API auth test passed
  - Market metadata reachable
  - Trading permissions confirmed
  - Balance/funding check passed
- If any check fails, bot returns exact fix instructions and keeps autonomous mode locked.

**Secret Handling Rule:** Telegram onboarding must never require users to paste raw private keys in chat. Any private-key operation (if needed for credential derivation/recovery) is executed only in trusted local CLI flow with explicit warnings.

**For CEX (`/connect_exchange`):**
1. User provides exchange API key + secret (with trade-only permissions, no withdrawal).
2. Keys are encrypted (AES-256-GCM) and stored in the SQL store.
3. The CCXT service uses these keys for order placement.
4. Security is enforced at the exchange level — API key permissions are set to "Spot Trading Only, No Withdrawal."

**For Watch-Only Wallets (`/add_wallet`):**
1. User provides chain + public address. No private key required.
2. Used for portfolio tracking across chains (Ethereum, Polygon, Solana, etc.).
3. Balance is fetched via blockchain RPCs.

**Security Model:** The bot can trade but **cannot withdraw** — enforced at the exchange/wallet permissions level, not just in code.

**Logs:** Logs are sanitized. The system regex-filters all output to ensure private keys or API secrets never appear in plain text in logs or Telegram messages.

### 9.4 AI Model Cost Management
The autonomous agent must manage its own AI API spend. The system implements a **cost budget system** tracked in the `ai_usage` SQL table.

**Budget Tiers:**
```
ai_budget_daily: $5.00
ai_budget_monthly: $100.00
```

If the daily budget is exhausted, the agent falls back to the local MLX model for all tasks. If the monthly budget is reached, only `CRITICAL` priority jobs (risk checks, kill switch) use cloud models.

**Model Routing Strategy:**

If users connect multiple providers and models we can use models router to select the best model for the task, if not we use the default model that user connected.

| Task | Model | Cost |
|---|---|---|
| Market scan / watchdog | Local MLX (free) | $0 |
| Quick trade decisions | Google Gemini 3 Flash / Claude Haiku | ~$0.001/call |
| Complex analysis / debate | ChatGPT 5.3 Codex / Google Gemini 3 Pro / Claude Opus 4.6 | ~$0.1/call |
| Critical risk assessment | ChatGPT 5.3 Codex / Claude Opus 4.6 (high reasoning) | ~$0.5/call |

**Token Tracking:** Every LLM call logs `model`, `tokens_input`, `tokens_output`, `cost_usd`, and `purpose` to the `ai_usage` table. The `/status` Telegram command shows current spend vs budget.

### 9.5 Exchange Resilience
What happens when an exchange API goes down mid-trade? The system implements circuit breakers:

- **Per-exchange health check:** Ping every 30 seconds. After 3 consecutive failures, mark the exchange as `DEGRADED`.
- **Retry strategy:** Exponential backoff (1s → 2s → 4s → 8s), max 4 retries.
- **Failover:** If the primary exchange is down during an active trade, the Risk Manager evaluates whether to wait or hedge on an alternative exchange.
- **Open order protection:** If connection drops with open orders, the system immediately queries order status on reconnect and reconciles.   

### 9.6 Objective Policy: Survival First, Growth Second
User growth goals are supported, but the default autonomous policy is capital survival:

- **Primary KPI:** Survival score (no liquidation, no hard-stop breaches, controlled drawdown).
- **Secondary KPI:** Risk-adjusted return (Sharpe/Sortino, profit factor), not raw short-term ROI.
- **No hard upside cap:** NeuraTrade does not enforce a fixed take-profit ceiling at portfolio level; when conditions remain favorable, winning positions are allowed to run.
- **Campaign goals:** Aggressive targets (for example, rapid account growth windows) run only as explicit high-risk campaigns with stricter position caps and shorter kill-switch thresholds.
- **Auto-degrade:** On risk breaches, the system downgrades from `AGGRESSIVE` -> `BALANCED` -> `DEFENSIVE` before full halt.
- **Profit protection without capping growth:** Use trailing risk budgets, exposure rebalancing, and volatility/leverage throttles instead of blunt profit caps.
- **Non-negotiable rule:** If survival controls conflict with profit target, survival controls win.

Uncapped-upside guardrail profile:

- **No portfolio take-profit ceiling:** the system does not force-stop profitable campaigns only because profit reached a fixed number.
- **Daily loss breaker:** stop new risk when daily realized loss breaches configured threshold.
- **Max drawdown breaker:** global halt/liquidation policy when drawdown threshold is breached.
- **Consecutive-loss pause:** automatic cool-down after N sequential losses for a strategy lane.
- **Regime gating:** high-volatility/liquidity-stress regimes automatically shrink position multipliers.
- **Ruin-probability discipline:** campaign-level risk budget is bounded so growth quests cannot bypass survival governors.

### 9.7 Rate-Limit and Anti-Spam Governance
NeuraTrade enforces rate controls at every layer to stay within exchange/platform rules and prevent internal spam:

- **Exchange/API layer:** Respect venue-specific limits with token-bucket throttling, backoff, and jitter.
- **Service layer:** Bound concurrent jobs and outbound API bursts using worker/semaphore limits.
- **Telegram layer:** Per-user command and notification rate limits to prevent chat spam.
- **Retry policy:** Retry only retryable errors; cap retries and fail closed when persistent.
- **Capability registry:** Each integration has an explicit `max_rps`, burst, and timeout profile maintained in config.

Violation handling:

- On repeated 429/rate-limit responses, strategy load is reduced automatically.
- Persistent violations trigger temporary strategy disablement for that venue.
- Critical repeated violations can escalate to global `/pause` recommendation or automatic pause.

## 10. User Interface and Human-Agent Interaction
The interface must provide transparency and control without requiring the user to SSH into a server. Telegram is the ubiquitously accessible command center — the single pane of glass for the human operator.

### 10.0 First-Run Onboarding (CLI <-> Telegram Handshake)
NeuraTrade onboarding is designed as a low-friction flow with explicit trust verification:

1. **Install:** User runs a one-line installer (shell bootstrap) that installs the `NeuraTrade` CLI binary and required local config files.
2. **Run:** User launches `NeuraTrade` and enters Telegram bot token in CLI.
3. **Challenge:** NeuraTrade sends a one-time authentication code to Telegram.
4. **Verify:** User copies the code from Telegram and pastes it back into CLI to prove chat ownership.
5. **Bind:** CLI stores encrypted local operator profile and links it to Telegram chat ID.

This handshake ensures the person controlling the local CLI is the same person controlling the Telegram command channel.

### 10.0.1 Readiness Gate Before Autonomous Start
Before `/begin` is allowed, NeuraTrade enforces hard onboarding prerequisites:

- AI providers and models already connected and tested.
- At least **one Polymarket wallet** connected.
- At least **one CEX exchange** connected for scalping workflows.
- At least **two CEX exchanges** connected for cross-exchange arbitrage.
- API key permissions validated as **trade-only** (no withdrawal rights).
- Connectivity checks pass for all configured exchanges/wallet providers.
- Available balances/funding pass minimum thresholds configured by risk profile.
- Health checks pass for Redis, SQL storage, and exchange bridges.

If any prerequisite fails, autonomous mode remains locked and Telegram shows specific remediation steps.

### 10.0.2 Strategy Activation Prerequisites
Autonomous mode can run multiple strategy families, but each family has hard prerequisites:

| Strategy Family | Minimum Integrations | Start Condition |
|---|---|---|
| Scalping | 1 CEX exchange | Exchange health `OK`, trading permission valid, minimum quote balance available |
| Arbitrage | 2+ CEX exchanges | Both exchanges healthy, symbol overlap exists, spread after fees > threshold |
| Prediction markets | 1 Polymarket wallet | Wallet/API auth valid, market data reachable, position/risk limits satisfied |

If one family fails prerequisites, only that family is disabled; other valid families may continue unless global safety controls trigger a full pause.

### 10.1 Command Structure
The bot supports a rich command set, parsed by the Go service:

**🔐 Authentication & Wallet Setup:**

| Command | Description |
|---|---|
| `/connect_exchange <exchange> <key> <secret>` | Link a CEX account (Binance, Kraken, etc.). Message auto-deleted after processing. |
| `/connect_polymarket <address> <key> <secret>` | Link Polymarket wallet. |
| `/add_wallet <chain> <address>` | Add a watch-only wallet for portfolio tracking. |
| `/remove_wallet <wallet_id>` | Remove a wallet integration. |
| `/remove_exchange <exchange>` | Remove a connected exchange account. |
| `/set_ai_key <provider> <key>` | Configure API key for AI models (OpenAI, Anthropic or any models from models.dev). |
| `/set_risk <level>` | Adjust risk thresholds: Low / Medium / High / Custom. |
| `/keys` | List all connected exchanges and AI providers (keys masked). |

**📊 Monitoring & Portfolio:**

| Command | Description |
|---|---|
| `/status` | System health, active quests, memory usage, and AI session state. |
| `/portfolio` | Full PnL summary, open positions (crypto + Polymarket), and fund milestone progress. |
| `/wallet` | Show connected wallets and balances across all chains/exchanges. |
| `/performance` | Strategy-level performance breakdown (win rate, Sharpe ratio, drawdown). |
| `/logs [N]` | Fetch the last N "thoughts" from the agent (default: 5). |
| `/quests` | List all active, pending, and completed quests with progress. |
| `/summary` | 24-hour summary across trades + prediction positions, realized/unrealized PnL, win/loss, major risk events. |
| `/doctor` | Run health diagnostics and return concrete remediation actions (keys, permissions, connectivity, readiness gate failures). |

**🎮 Control:**

| Command | Description |
|---|---|
| `/begin` | Start autonomous execution once readiness checks pass. |
| `/pause` | Emergency stop — immediately suspends all trading. |
| `/resume` | Resume trading after pause. |
| `/liquidate <asset>` | Force an immediate exit from a position. |
| `/liquidate_all` | Force-close all open positions across connected venues (highest-priority emergency command). |
| `/quest <prompt>` | Manually assign a task (e.g., "Check the price of BTC and tell me if I should buy"). |
| `/quest list` | List all active, pending, and completed quests with progress. |
| `/stream on/off` | Toggle the live transaction stream in the current chat (by default is on). |
| `/config` | Show current AI configuration, risk settings, and active strategies. |
| `/help` | Show all available commands. |

### 10.2 The Live Transaction Stream
A dedicated Telegram channel (or inline toggle via `/stream on`) serves as the "Console." The agent pushes real-time updates for every action:

```
🤖 ACTION: BUY
Asset: ETH/USDC
Type: Scalping
Price: $3,200
Size: 0.5 ETH
Strategy: Sentiment Momentum
Reasoning: RSI oversold (28). News sentiment neutral/positive.
  Support at $3,150 holding.
Risk Check: ✅ PASSED. Exposure remains under 20%.
Quest: "Accumulate 10 ETH" (Progress: 7.5/10 ETH)
```

```
🤖 ACTION: BUY
Asset: Will Trump pardon Ghislaine Maxwell by end of 2026?
Type: Prediction Market
Price: $0.5
Size: 100
Strategy: Trump is polling at 50% approval rating.
Reasoning: Smart Wallet Detected.
Risk Check: ✅ PASSED. Exposure remains under 20%.
Quest: "Accumulate 10 ETH" (Progress: 7.5/10 ETH)
```

The stream also includes:
- 📈 **Quest progress updates** (hourly/daily/weekly completion)
- 💰 **Fund milestone alerts** ("Reached $500 — entering Growth phase")
- ⚠️ **Risk events** (kill switch triggers, position liquidations)
- 🧠 **AI reasoning summaries** (why the agent chose this strategy)
- 📊 **24h recap hints** (pointer to `/summary` with key deltas)

This transparency builds trust, allowing the human operator to understand the "why" behind every action.   

## 11. Infrastructure and Deployment
### 11.1 Containerization
The application is Dockerized for portability. **No Python containers.**

**neuratrade-agent:** Go binary (single static binary, ~20MB). Contains the AI orchestration loop, quest scheduler, and job system.

**neuratrade-infra:** Go binary. Telegram bot, WebSocket management, CCXT bridge coordination.

**neuratrade-ccxt:** TypeScript/Bun. CCXT library wrapper for unified exchange access.

**redis:** Alpine image with Redis (Pub/Sub, ephemeral cache, distributed locks, rate limiting).

**sqlite:** Default durable store for production and local deployments.

**sqlite-vec:** SQLite vector extension for semantic memory retrieval and setup similarity checks.

**MLX (local only):** Optional sidecar — runs `mlx_lm.server` on the host Mac for local inference. Exposes OpenAI-compatible endpoint on `localhost:8080`. Not deployed to VPS.

### 11.2 Deployment Environments
**Local (Mac Studio/Pro):** Used for heavy R&D. The skill.md files are edited locally, and the agent hot-reloads them. MLX-LM runs the local Watchdog. Default local stack is SQLite + Redis.

**Cloud (QuantVPS):** The production deployment. The Cloud version relies solely on API models (GPT-4o, Claude Sonnet) — no local MLX. Alternatively, a small CPU-optimized model (like Llama-3-8B via llama.cpp) can run as a sidecar for basic filtering.

**Network Topology:** The VPS should be in ap-northeast-1 (Tokyo) for Binance or us-east-1 (Virginia) for Coinbase/Polymarket (Polygon nodes) to minimize network jitter.   

## 12. Implementation Roadmap
### Phase 1: Foundation (Weeks 1-2)
**Objective:** Core connectivity, onboarding UX, and data ingestion.

**Tasks:**
- Keep SQLite + Redis as default persistence for MVP; preserve a persistence abstraction so storage internals remain swappable.
- Build one-line installer (`install.sh`) and CLI bootstrap command (`NeuraTrade`).
- Implement CLI <-> Telegram one-time auth code handshake for operator binding.
- Implement readiness gate checks (wallet/exchange minimums, permissions, connectivity, and funding).
- Implement CCXT normalization layer in Go (extend existing backend-api).
- Build Polymarket Gamma/CLOB API wrappers in Go.
- Set up Redis Pub/Sub channels for market data.
- Create the basic skill.md files for `get_price`, `place_order`, `get_portfolio`.
- Implement AES-256-GCM key encryption for the `api_keys` table.

### Phase 2: The Agentic Core (Weeks 3-4)
**Objective:** Go-native AI agent — "The Brain."

**Tasks:**
- Implement the **tool-calling loop** in Go (direct HTTP to OpenAI/Anthropic APIs).
- Build the skill.md parser and progressive disclosure system.
- Implement the Multi-Agent **Debate Protocol** (Analyst → Trader → Risk Manager).
- Build the Quest Scheduler (cron-based + event-driven).
- Build the Parallel Job System (Redis job queue + goroutine pool).
- Implement AI Session persistence (serialize/resume from SQL store via repository abstraction).
- Set up local MLX integration (Go ↔ `mlx_lm.server` HTTP).
- Add deterministic survival governors (daily loss cap, max drawdown halt, consecutive-loss pause, position-size throttle).
- Expose readiness/risk/cleanup/arbitrage primitives as stable internal tool endpoints before enabling capital-mutation tools.
- Add `IndicatorProvider` abstraction and provider selection config to support phased TA migration.

### Phase 3: Strategy & UI (Weeks 5-6)
**Objective:** Trading logic, Telegram interface, and paper trading.

**Tasks:**
- Codify **scalping**, "Sum-to-One" arbitrage, and "Sentiment Momentum" strategies as skill.md files.
- Build all Telegram Bot command handlers (§10.1), including `/begin`, `/pause`, `/summary`, `/liquidate_all`, and account add/remove flows.
- Implement the **Live Transaction Stream** (§10.2).
- Build the `/connect_exchange` and `/connect_polymarket` wallet flows.
- Implement execution-layer endpoints for `place_order`, `cancel_order`, position snapshot, and controlled liquidation.
- Integrate GoFlux-backed advanced indicators and backtest workflows behind adapter-based provider switching.
- Implement AI cost tracking and budget enforcement.
- **Paper Trading:** Run the system with virtual money for 7+ days.
- Implement the **backtesting engine** (replay historical OHLCV against strategies).
- Implement smart-wallet scanner in **shadow mode** first (signal-only, no direct execution authority).

### Phase 4: Production (Week 7+)
**Objective:** Live deployment with real capital.

**Tasks:**
- Deploy to QuantVPS (Go binaries + Redis + SQLite).
- Fund with minimal capital ($100 USDC).
- Monitor kill switch functionality and exchange resilience.
- Scale up capital based on Sharpe Ratio performance.
- Enable fund milestone tracking and phase transitions.

## 13. Operational Addenda

### 13.1 Paper Trading Mode
Before going live, NeuraTrade must support a **simulation mode** that uses real market data but virtual money. All trades are recorded in the SQL store with a `paper_trade = true` flag. The Paper Trading mode exercises the full pipeline (data ingestion -> AI reasoning -> debate -> execution logging) without touching any exchange API. This validates strategies with zero financial risk.

### 13.2 Backtesting Engine
The AI should replay historical data against a strategy to evaluate it before live deployment. The backtesting engine:
- Reads OHLCV data from the `market_history` SQL table.
- Simulates the AI's decision loop against past market conditions.
- Outputs: Win rate, max drawdown, Sharpe ratio, profit factor.
- Stores backtest results in the SQL store for comparison across strategy versions.

### 13.3 Tax & Reporting
Every trade generates a taxable event. The `trades` table tracks:
- **Cost basis** (FIFO method) for each position.
- **Realized PnL** on every close.
- **Fee tracking** (exchange fees, gas fees, AI API costs).
- **CSV export** via Telegram `/export_taxes <year>` for tax filing.

### 13.4 Audit Trail
For an autonomous system managing real money, accountability is non-negotiable:
- The `thoughts` table is **append-only** — no updates, no deletes.
- Every trade links to its originating `thought_id`, `quest_id`, and `session_id`.
- A structured decision record (market context, action, risk checks, and execution outcome) is always retrievable.
- System events (kill switch activations, budget exhaustion, exchange failures) are logged with timestamps.

### 13.5 Strategy Versioning
When the AI adapts or you edit a skill.md, the system must track **which version** of a strategy was used for each trade:
- Each skill.md file is hashed (SHA-256) on load.
- The hash is stored in the `trades.strategy_version` column.
- Performance can be correlated with strategy changes: "Win rate dropped after the last edit to `scalping.skill.md`."
- Skill.md files are stored in git — version history is automatically available.

### 13.6 Safe Self-Improvement Protocol
Self-improvement is allowed only through controlled gates:
- **Paper mode first:** New strategy versions must pass replay and paper-trading criteria.
- **Shadow mode second:** New strategy runs in parallel without real execution.
- **Champion/challenger gate:** Promote only if challenger beats incumbent on risk-adjusted metrics and drawdown constraints.
- **Progressive rollout:** Start with small capital allocation and auto-rollback on breach.
- **No unconstrained online RL on live funds.**

### 13.7 Database Change Discipline (Migrations-Only)
Schema consistency is enforced through migrations only:

- **No direct manual schema edits** in production databases.
- Every schema change must ship as an ordered migration file in version control.
- Startup and deploy pipelines run migration status checks, then apply pending migrations.
- Emergency hotfix SQL must be converted into a formal migration immediately after incident response.
- Application code must assume schema evolution is forward-only and rollback-safe by design.

This guarantees reproducible environments and prevents drift between local/dev/staging/prod.

### 13.8 LLM Agent Tool Contract (Minimum Production Set)
The LLM/agent layer should not call arbitrary internals. It must operate through explicit tool contracts with deterministic guards.

**Portfolio and Risk Tools (hard-gate capable):**
- `pre_trade_risk_check`
- `portfolio_exposure_snapshot`
- `max_drawdown_guard`
- `kill_switch`

**Execution and Venue Tools:**
- `get_order_book`
- `place_order`
- `cancel_order`
- `liquidate_all_positions`

**Strategy and Evaluation Tools:**
- `strategy_scorecard`
- `backtest_strategy`
- `paper_trade_execute`
- `champion_challenger_gate`

**Ops and Reliability Tools:**
- `rate_limit_health`
- `exchange_health`
- `readiness_gate_check`
- `run_doctor`

**Audit and Governance Tools:**
- `decision_journal_append`
- `incident_log_append`
- `migration_status`
- `cleanup_status`

**Learning and Memory Tools:**
- `pre_trade_memory_check`
- `append_trade_outcome`
- `retrieve_lessons`
- `hallucination_guard_check`
- `failure_pattern_update`
- `growth_journal_snapshot`

Tool policy:
- Tools that mutate capital must pass deterministic risk gates first.
- Risk tools may veto execution tools unconditionally.
- Tool I/O must be structured and machine-validated (no free-form side effects).

Current codebase capability baseline (to accelerate delivery):
- **Already present and promotable to tools quickly:** health/readiness (`/health`, `/ready`, `/live`), cleanup trigger + stats (`/api/data/cleanup`, `/api/data/stats`), arbitrage/futures opportunity queries (`/api/arbitrage/*`, `/api/futures-arbitrage/*`), and risk/circuit-breaker primitives.
- **Partially present:** exchange connectivity through CCXT bridge plus rate-limit primitives; still needs tool-facing wrappers and stricter policy surfaces.
- **Missing and planned:** first-class execution tools (`place_order`, `cancel_order`, position lifecycle), migration governance tooling (`migration_status`), autonomous decision journal APIs, and wallet-scanner APIs.
- **Insertion points:** implement missing capabilities under `services/backend-api/internal/services/`; expose them in `services/backend-api/internal/api/handlers/`; register routes in `services/backend-api/internal/api/routes.go`.

### 13.9 Smart Wallet Scanner (Prediction-Market Intelligence)
Smart-wallet scanning should be part of the plan as a **signal-intelligence module**, not blind copy-trading.

Current-state note:
- In the existing NeuraTrade codebase, Polymarket wallet-intelligence ingestion is a net-new capability and should be implemented as a dedicated module integrated into the existing signal/risk pipeline.

Data sources:
- Polymarket Data API `/trades` for wallet trade history and behavior features.
- Polymarket Data API `/positions` for current exposure state.
- Polymarket Data API `/holders` for concentration/context and cohort mapping.
- Polymarket CLOB + WebSocket for execution-time liquidity and slippage context.
- Polymarket Gamma metadata for market structure, tags, and lifecycle state.
- On-chain wallet activity relevant to prediction positions.

Core scanner outputs:
- `wallet_conviction_score` (market + side + confidence + freshness decay).
- `wallet_cluster_flow` (cohort-level buy/sell pressure, not single-wallet worship).
- `wallet_signal_explanation` (why this signal is considered meaningful).

Mandatory guardrails:
- No direct auto-execution from one wallet signal.
- Wallet-derived signals must be confirmed by liquidity + risk checks.
- Max wallet-follow exposure cap (portfolio budget bounded).
- Anti-manipulation filters (sybil/wash/coordination suspicion).
- Geographic/compliance policy checks before execution.
- Wallet profitability must pass minimum statistical quality (sample size + consistency), not just headline PnL.
- Wallet signals are weighted inputs in ensemble scoring, never a standalone execution trigger.

Rollout protocol:
- Phase A: Observe only (no execution impact).
- Phase B: Shadow scoring against real outcomes.
- Phase C: Limited-risk live influence with automatic rollback.

Signal policy for wallet intelligence:

- `wallet_alpha_signal` contributes to conviction score but is always gated by `pre_trade_risk_check`.
- Wallet scores decay over time and regime changes; stale winners lose weight automatically.
- If manipulation-risk score or correlation-cluster risk exceeds threshold, wallet contribution is zeroed.
- During high-spread/low-liquidity windows, wallet influence is reduced even when historical quality is high.

### 13.10 Tech Stack Currency Governance (Pinned Checkpoint Date)
As-of checkpoint date for this plan: **2026-02-11**. "Latest stack" must be verifiable from official release channels, not assumptions.

Verified reference points from official sources at this checkpoint:

| Component | Verified current line (as of 2026-02-11) | Source |
|---|---|---|
| Go | `go1.26.0` released 2026-02-10 | https://go.dev/doc/devel/release |
| Bun | `v1.3.9` | https://bun.com/blog/bun-v1.3.9 |
| SQLite | `3.51.2` (released 2026-01-09) | https://www.sqlite.org/changes.html |
| sqlite-vec | Latest stable release `v0.1.6`; newer alpha tags available | https://github.com/asg017/sqlite-vec/releases |
| Redis Open Source | `8.6.0` line available (Feb 2026) | https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/release-notes/redisce/ |
| Docker Engine | `29.2.1` release notes | https://docs.docker.com/engine/release-notes/ |
| CCXT npm | `4.5.37` (`/latest`) | https://registry.npmjs.org/ccxt/latest |
| grammY npm | `1.40.0` (`/latest`) | https://registry.npmjs.org/grammy/latest |
| OpenAI Node SDK | `6.21.0` (`/latest`) | https://registry.npmjs.org/openai/latest |
| Anthropic TS SDK | `0.74.0` (`/latest`) | https://registry.npmjs.org/@anthropic-ai/sdk/latest |

Implementation policy for "latest":
- Keep dependency manifests pinned to explicit versions (no silent drift in production images).
- Run scheduled version-audit checks (at least weekly) and require compatibility tests before upgrade.
- Record upgrade decisions in changelog/migration notes with date and source links.
- If "latest" introduces instability, keep n-1 safe version until canary tests pass.

### 13.11 Persistent Learning, Anti-Loop Memory, and Hallucination Guards
NeuraTrade must learn from historical reasoning/trades/fund outcomes without repeating failed patterns.

Memory model (SQLite source of truth):
- `decision_journal`: append-only structured decision records (strategy lane, regime, features, confidence, tool evidence IDs).
- `trade_outcomes`: execution details, realized/unrealized PnL, slippage, drawdown contribution, and post-trade tags.
- `lesson_cards`: normalized `do` and `dont` rules with confidence, TTL, and provenance.
- `failure_patterns`: recurring failure archetypes (for example, low-liquidity chase, regime mismatch, stale signal execution).
- `fund_journal`: equity curve checkpoints, milestone progression, and risk-budget state transitions.

Pre-trade anti-loop gate (mandatory):
- Run `pre_trade_memory_check` before any capital mutation.
- Retrieve top similar historical setups using `sqlite-vec` embedding search plus structured filters (symbol, timeframe, strategy lane, regime).
- If matched setups map to active `dont` lessons or repeated failure patterns, auto-throttle size or reject trade.

Hallucination guard (mandatory):
- `hallucination_guard_check` requires fresh tool evidence for every claim (order book, volatility, exposure, wallet signal quality).
- If evidence is missing, stale, or inconsistent across tools, action downgrades to `NO_TRADE`.
- No free-form reasoning can bypass deterministic risk and evidence checks.

Learning cadence:
- **Per-trade:** append outcome and update lesson confidence.
- **Daily:** consolidate lessons, decay stale rules, and refresh failure-pattern counters.
- **Weekly:** champion/challenger review; only promote strategy variants that improve risk-adjusted metrics and drawdown profile.

SQLite operations policy:
- Use WAL mode and periodic online backups for durability.
- Keep all learning schema changes migration-only.
- Add indexes for high-frequency lookups (`strategy_lane`, `regime`, `timestamp`) and vector retrieval join keys.
- Run periodic compaction/cleanup to avoid unbounded growth of raw reasoning artifacts.

## 14. Future Horizons
The architecture described herein is foundational. Future iterations will expand into:

**On-Chain Agency:** Allowing the agent to interact directly with smart contracts (Uniswap, Aave) rather than just API-based exchanges.

**DAO Integration:** The agent could manage a treasury for a DAO, taking governance votes as input for its "Analyst" module.

**Self-Improvement:** Use offline learning, paper-mode replay, and champion/challenger promotion with strict rollback gates so model updates never bypass deterministic risk controls.

**Multi-User Platform:** While initially single-user, the SQL schema supports multi-user from day one. Future versions could support multiple operators with isolated strategies and budgets.

This report confirms that the technology stack—LLMs, Skill Standards, and Agentic Architectures—is now mature enough to support fully autonomous, reasoning-based financial entities. The proposed system represents a significant leap forward from the brittle algorithms of the past to the adaptive, intelligent agents of the future.
