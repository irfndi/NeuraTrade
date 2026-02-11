# NeuraTrade Master Plan: Autonomous AI Crypto & Polymarket Trading Bot

## Executive Summary
The financial landscape is undergoing a paradigm shift from high-frequency, deterministic algorithmic trading to autonomous, semantic "agentic" trading. This transition is driven by the emergence of Large Language Models (LLMs) capable of complex reasoning, planning, and tool utilization. This report details the comprehensive technical architecture for transforming a legacy CCXT-based arbitrage bot into a fully autonomous AI trading system. The proposed system integrates a high-capability LLM as the central cognitive engine, operating within a "tool-calling" paradigm where the bot’s infrastructure functions as a set of executable skills.

The architecture leverages a hybrid computational model, utilizing state-of-the-art cloud models like OpenAI’s GPT-5.3 and Anthropic’s Claude Opus 4.6 for high-leverage decision-making, while employing local, quantized models on Apple Silicon (MLX-LM) for routine monitoring to optimize costs. A core innovation of this design is the adoption of the skill.md open standard, which decouples the agent's intelligence from its tools, allowing for dynamic capability discovery and strictly defined safety boundaries.   

The system is designed to operate across both centralized cryptocurrency exchanges (via CCXT) and decentralized prediction markets (Polymarket), exploiting unique arbitrage opportunities such as "sum-to-one" discrepancies in conditional token markets. To support continuous, autonomous operation, the architecture features a dual-layer memory system—Redis for short-term, high-velocity context and SQLite for long-term episodic persistence—mimicking human cognitive processes. Risk management is enforced through a dedicated "Super-Ego" agent role that vetoes unsafe actions, ensuring capital preservation remains paramount. The user interface acts as a command-and-control center via Telegram, providing real-time transparency into the agent's reasoning stream.   

## 1. Introduction to Autonomous Financial Agents

### 1.1 The Evolution of Algorithmic Trading
Historically, algorithmic trading has been defined by speed and deterministic logic. Market makers and arbitrageurs relied on hard-coded strategies—"if moving average X crosses moving average Y, then buy"—executed in microseconds. While effective in stable or trending regimes, these "black box" systems suffer from brittleness. They lack semantic understanding of the market; they cannot interpret a news headline, understand a regulatory shift, or recognize when a technical signal is invalidated by a fundamental event. They are purely reactive to numerical time-series data.

The advent of Large Language Models (LLMs) has introduced a new capability: reasoning. Unlike traditional algorithms, an LLM-based agent can perceive the market in multiple modalities—textual, numerical, and even visual. It can plan multi-step workflows, adapt to changing conditions without code rewrites, and explain its actions in natural language. This shift from "Algo" to "Agent" represents the next frontier in decentralized finance (DeFi) and automated trading.   

### 1.2 Project Objectives

The primary objective of this project is to construct a **NeuraTrade Generalist Trading Agent** capable of autonomous operation in the cryptocurrency and prediction markets. The specific goals are:

Autonomy: The system must operate continuously (24/7), managing its own schedule via "quests" (e.g., hourly scans, daily rebalancing) without human intervention.   

Adaptability: The agent must be able to switch strategies based on market regimes (e.g., from volatility scalping to stablecoin yield farming) by leveraging a library of modular skills.

Safety: Given the non-deterministic nature of LLMs, the system must enforce strict, deterministic safety rails—hard limits on position sizing, stop-losses, and authorized API actions.

Transparency: The "black box" problem must be solved by requiring the agent to log its "Chain of Thought" (CoT) rationale for every trade, accessible to the user via a live Telegram stream.   

Hybrid Efficiency: The architecture must balance performance and cost by routing complex tasks to frontier cloud models (GPT-5.3, Opus 4.6) and routine tasks to local silicon (M1/M3 Pro).   

### 1.3 Scope of Operations
The agent will operate in two distinct market environments:

Cryptocurrency Spot/Futures: Utilizing the CCXT library to access major exchanges (Binance, Kraken, Coinbase), executing technical and sentiment-based strategies.

Prediction Markets (Polymarket): Utilizing the Polymath/Gamma APIs to trade on the outcome of real-world events. This involves unique "conditional token" mechanics and arbitrage strategies distinct from traditional financial assets.   

## 2. Computational Substrate: The LLM Backbone

The "brain" of the NeuraTrade autonomous trading system is the Large Language Model. As of early 2026, the landscape of foundational models has specialized, allowing for a strategic selection of models based on specific agent roles.

### 2.1 Frontier Models: The Cloud Tier
For high-stakes decision-making, architectural reasoning, and complex code synthesis, the system relies on the most capable models available via API.

#### 2.1.1 OpenAI GPT-5.3 (Codex)
GPT-5.3, often referred to as "Codex 5.3," represents the pinnacle of code generation and project-level synthesis. Research indicates that GPT-5.3 excels in "Global Project Synthesis"—the ability to understand and manipulate complex, multi-file codebases and execution environments.   

Role in System: GPT-5.3 is designated as the engine for the Trader and Execution agents. Its ability to generate precise, syntactically correct JSON payloads for API calls and Python scripts for strategy execution is unmatched.

Capabilities: It supports a 64,000-token output limit, enabling it to write extensive logs and self-correction routines during complex trade executions. Its low-latency "turbo" modes make it suitable for the execution leg of the trade where speed is critical.   

#### 2.1.2 Anthropic Claude Opus 4.6
Claude Opus 4.6 is selected for its "Adaptive Thinking" architecture and massive context window (up to 1 million tokens). It is characterized by high reliability in reasoning tasks and a "safety-first" alignment, making it less prone to hallucination in critical scenarios.   

Role in System: Opus 4.6 serves as the Analyst and the Risk Manager. Its large context window allows it to ingest vast amounts of unstructured data—whitepapers, news feeds, long Twitter threads, and governance proposals—without truncation.

Native Multi-Agent Logic: Opus 4.6 has demonstrated superior performance in simulating multi-perspective debates, making it ideal for the internal "boardroom" discussions between the Bullish Analyst and the Bearish Risk Manager.   

### 2.2 Local Inference: The Edge Tier
To mitigate the high costs and network latency associated with continuous cloud API calls, the architecture incorporates local inference capabilities. This "Hybrid AI" approach ensures the bot remains functional and cost-effective for routine monitoring.

#### 2.2.1 Apple Silicon and MLX-LM
The system is optimized for deployment on Apple M-series hardware (M1 Pro through M3 Max). Apple's unified memory architecture and potent Neural Engine allow for the efficient execution of high-parameter models locally.   

Framework: The system utilizes MLX-LM, Apple's open-source framework designed specifically for Apple Silicon. Unlike generic PyTorch implementations, MLX allows for unified memory access, significantly speeding up inference for transformer models.   

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

Inputs: Proposed Trade Payloads, Portfolio State, Risk Rules (SQLite).

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
To maintain agency over long timeframes, the system utilizes a **Quest System**. A "Quest" is a high-level directive with a specific duration, objective, and success criteria, managed by the system's scheduler and persisted in SQLite.

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

The progress of each quest is tracked in SQLite with full state persistence, ensuring that if the bot is restarted, it "remembers" its ongoing missions and resumes exactly where it left off.

### 3.4 AI Session Management
The system manages **AI Sessions** as first-class entities. Each session represents a continuous reasoning context for the AI agent.

**Session Lifecycle:**
1. **Init**: A new session is created when the bot starts or when a new quest requires fresh context.
2. **Active**: The session holds the current reasoning chain, loaded skills, and working memory.
3. **Suspended**: When the AI switches to a higher-priority quest, the current session is serialized to SQLite.
4. **Resumed**: On return, the session is deserialized with full context intact.
5. **Archived**: Completed sessions are compressed and stored for post-game analysis.

**Session State** (persisted in SQLite):
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
- Job state is checkpointed to SQLite every N seconds.
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

Rate Limits: The system respects exchange rate limits (e.g., 1200 requests/minute on Binance) by managing a token bucket locally. If the limit is approached, the "Risk Manager" throttles the "Trader's" ability to make non-essential queries.

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

Limits: 60 authenticated requests per minute.   

Data API (Positions): Used for portfolio reconciliation.

Endpoint: https://data-api.polymarket.com

Usage: "What are my current holdings and PnL?"

### 5.3 Multi-Modal Ingestion
The agent's perception extends beyond numbers.

Textual Data: The system connects to RSS feeds, Twitter lists (via API), and governance forums. The "Analyst" agent uses the Redis Vector Store to search for semantic relevance (e.g., "Retrieve recent news about 'SEC regulation'").

Visual Data: Using the vision capabilities of models like GPT-5.3, the agent can analyze chart images. It can identify patterns (e.g., "Head and Shoulders") or support/resistance zones that are difficult to describe purely mathematically.   

## 6. System Architecture: The Technical Core
The system is architected as a distributed microservices application, prioritizing modularity, concurrency, and persistence. The existing NeuraTrade codebase (Go + TypeScript/Bun) provides the foundation, which will be extended with AI agent capabilities.

### 6.1 High-Level Architecture
The system consists of three primary services communicating via Redis:

**The Agent Service (Go — AI Core):**
- Hosts the LLM orchestration logic (tool-calling loop).
- Parses and loads skill.md files dynamically.
- Manages AI sessions, quest scheduling, and the parallel job system.
- Communicates with LLM APIs (OpenAI, Anthropic) via HTTP.
- Runs the local MLX watchdog process for cost-efficient monitoring.

**The Infrastructure Service (Go — existing backend-api):**
- Manages high-performance networking and WebSocket connections to exchanges.
- Hosts the Telegram Bot API server.
- Handles cryptographic signing and transaction broadcasting.
- Provides the CCXT normalization layer.
- Go is chosen for its superior concurrency primitives (Goroutines), essential for handling hundreds of live data feeds simultaneously.

**The Exchange Bridge (TypeScript/Bun — existing ccxt-service):**
- Wraps the CCXT library for unified exchange access.
- Provides HTTP API for the Go services to interact with 100+ exchanges.
- Handles exchange-specific quirks and rate limiting.

> **Note:** The current codebase uses PostgreSQL for persistence. The migration to **SQLite + Redis** will be implemented as part of Phase 1 to achieve a lighter, more portable deployment suitable for local-first autonomous operation.

### 6.2 Service Communication
The services are decoupled using a **Redis Message Queue**.

**Market Data:** The Go infrastructure service pushes normalized market ticks to Redis Pub/Sub channels (e.g., `market:eth_usdc:ticker`).

**Agent Wake-up:** The Agent service subscribes to these channels. Upon receiving significant data (filtered by the local MLX model), it triggers the LLM reasoning loop.

**Execution:** When the Agent decides to trade, it pushes a JSON command payload to a Redis queue (`orders:execute`). The infrastructure service pops this payload, signs it, and executes it via the exchange API.

**Job Coordination:** Quest and Job state changes are published to Redis channels (`jobs:status`, `quests:progress`) enabling real-time Telegram streaming.

### 6.3 Local vs. Cloud Infrastructure
**Development:** The entire stack (including local LLMs) runs on an Apple M3 Max MacBook Pro. SQLite file lives alongside the binary — zero external database dependencies for dev.

**Production:** The system is containerized (Docker). The services can run on a single VPS with SQLite volume-mounted for persistence.

**VPS Strategy:** For low-latency execution, the Go service executes on a QuantVPS instance located in close proximity to exchange servers (e.g., AWS Tokyo for Binance) or Polygon RPC nodes. This minimizes the "tick-to-trade" latency, crucial for arbitrage and scalping.   

## 7. Memory Systems: The Cognitive Architecture
A major limitation of raw LLMs is "statelessness"—they have no memory of past interactions. To create a continuous, learning agent, the system implements a Dual-Memory architecture.

### 7.1 Short-Term Memory: Redis
Redis serves as the "Working Memory" or RAM of the agent. It stores high-velocity, ephemeral data.

Conversation Context: Stores the sliding window of the current reasoning chain. As new thoughts are generated, old ones are summarized and moved to long-term memory or discarded.

Market Cache: Uses Redis Sorted Sets (ZSET) to maintain time-series data of recent prices (O(logN) access). This allows the agent to ask, "What was the price volatility over the last 1 hour?" instantly without querying an external API.

Concurrency Locks: Redis atomic locks (SETNX) are used to prevent race conditions. For example, if the "Arbitrage Quest" is running, it acquires a lock on the ETH-USDC pair so that the "Rebalance Quest" does not try to sell the same asset simultaneously.   

### 7.2 Long-Term Memory: SQLite
SQLite serves as the "Episodic Memory" or Hard Drive. It stores persistent state that must survive system reboots.

Schema Design:

users: Stores encrypted API keys, risk settings, and Telegram user IDs.

trades: A structured log of every execution (Entry Price, Exit Price, Fees, PnL).

thoughts: A log of the rationale behind every trade. This allows for "Post-Game Analysis"—the user can query, "Why did you buy ETH yesterday?" and the agent retrieves the exact reasoning record.

quests: Tracks the state of long-running tasks.

Vector Database (SQLite + Extension): The system uses a vector extension (like sqlite-vss or a separate Pinecone index) to store embeddings of past market conditions. This allows the agent to perform Semantic Search: "Find past instances where RSI was < 20 and news sentiment was negative." If the agent sees that such conditions previously led to a loss, it can adjust its current strategy—effectively "learning" from experience.   

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

Technicals: Agent monitors Moving Averages and RSI.

Sentiment: Agent analyzes Twitter/News velocity.

Synthesis: If Technicals = Bullish AND Sentiment = Bullish, Execute High Confidence Buy. If Technicals = Bullish but Sentiment = Bearish (e.g., "CEO Arrested"), the agent overrides the technical signal and stays flat. This is the key advantage of the LLM agent over a standard bot.

Mean Reversion:

Using Bollinger Bands and RSI to fade extreme moves. The "Risk Manager" ensures we do not "catch a falling knife" by checking for breaking news that justifies the extreme move.

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

### 9.2 Hard-Coded Circuit Breakers
Beyond the LLM's reasoning (which can fail/hallucinate), hard-coded checks exist in the Go infrastructure layer:

Max Drawdown Kill Switch: If Portfolio Value drops > 5% in 24 hours, the bot automatically closes all positions and shuts down.

Fat Finger Check: Orders deviating > 5% from the current market price are rejected automatically.

Rate Limiters: API calls are throttled to prevent IP bans.

### 9.3 Operational Security
Key Storage: API keys are encrypted using AES-256-GCM in the SQLite database. They are decrypted only in memory at the moment of signing.

Wallet Security: The Polymarket integration uses a Proxy Wallet architecture. The main funds sit in a smart contract (Gnosis Safe or similar), and the bot is given limited allowance to trade, but zero allowance to withdraw.

Logs: Logs are sanitized. The system regex-filters all output to ensure private keys or API secrets never appear in plain text in logs or Telegram messages.   

## 10. User Interface and Human-Agent Interaction
The interface must provide transparency and control without requiring the user to SSH into a server. Telegram is the ubiquitously accessible command center — the single pane of glass for the human operator.

### 10.1 Command Structure
The bot supports a rich command set, parsed by the Go service:

**🔐 Authentication & Wallet Setup:**

| Command | Description |
|---|---|
| `/connect_exchange <exchange> <key> <secret>` | Link a CEX account (Binance, Kraken, etc.). Message auto-deleted after processing. |
| `/connect_polymarket <address> <key> <secret>` | Link Polymarket wallet. |
| `/add_wallet <chain> <address>` | Add a watch-only wallet for portfolio tracking. |
| `/set_ai_key <provider> <key>` | Configure API key for AI models (OpenAI, Anthropic). |
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

**🎮 Control:**

| Command | Description |
|---|---|
| `/pause` | Emergency stop — immediately suspends all trading. |
| `/resume` | Resume trading after pause. |
| `/liquidate <asset>` | Force an immediate exit from a position. |
| `/quest <prompt>` | Manually assign a task (e.g., "Check the price of BTC and tell me if I should buy"). |
| `/stream on/off` | Toggle the live transaction stream in the current chat. |
| `/config` | Show current AI configuration, risk settings, and active strategies. |

### 10.2 The Live Transaction Stream
A dedicated Telegram channel (or inline toggle via `/stream on`) serves as the "Console." The agent pushes real-time updates for every action:

```
🤖 ACTION: BUY
Asset: ETH/USDC
Price: $3,200
Size: 0.5 ETH
Strategy: Sentiment Momentum
Reasoning: RSI oversold (28). News sentiment neutral/positive.
  Support at $3,150 holding.
Risk Check: ✅ PASSED. Exposure remains under 20%.
Quest: "Accumulate 10 ETH" (Progress: 7.5/10 ETH)
```

The stream also includes:
- 📈 **Quest progress updates** (hourly/daily/weekly completion)
- 💰 **Fund milestone alerts** ("Reached $500 — entering Growth phase")
- ⚠️ **Risk events** (kill switch triggers, position liquidations)
- 🧠 **AI reasoning summaries** (why the agent chose this strategy)

This transparency builds trust, allowing the human operator to understand the "why" behind every action.   

## 11. Infrastructure and Deployment
### 11.1 Containerization
The application is Dockerized for portability.

agent-service: Python 3.12, LangChain, PyTorch (for local MLX).

infra-service: Go 1.22.

redis: Alpine image with Redis Stack (for Vector support).

sqlite: Volume mounted for persistence.

### 11.2 Deployment Environments
Local (Mac Studio/Pro): Used for heavy R&D. The skill.md files are edited locally, and the agent hot-reloads them. MLX-LM runs the local Watchdog.

Cloud (QuantVPS): The production deployment. To save costs, the Cloud version might rely solely on API models (GPT-5.3) and skip the local MLX component, or use a small CPU-optimized model (like Llama-3-8B-CPU) for basic filtering.

Network Topology: The VPS should be in the ap-northeast-1 (Tokyo) region for Binance or us-east-1 (Virginia) for Coinbase/Polymarket (Polygon nodes) to minimize network jitter.   

## 12. Implementation Roadmap
### Phase 1: Foundation (Weeks 1-2)
Objective: Core connectivity and data ingestion.

Tasks:

Implement CCXT normalization layer in Go.

Build Polymarket Gamma/CLOB API wrappers.

Set up Redis Pub/Sub and SQLite schema.

Create the basic skill.md for get_price and place_order.

### Phase 2: The Agentic Core (Weeks 3-4)
Objective: "Brain" integration.

Tasks:

Integrate GPT-5.3 and Opus 4.6 APIs.

Implement the LangGraph workflow (Analyst -> Trader -> Risk).

Develop the "Debate Protocol."

Unit test skills with the local MLX model.

### Phase 3: Strategy & UI (Weeks 5-6)
Objective: Logic and Interface.

Tasks:

Codify "Sum-to-One" and "Momentum" strategies into the system prompt/skills.

Build the Telegram Bot handlers.

Implement the "Live Stream" logging.

Paper Trading: Run the system with virtual money for 7 days.

### Phase 4: Production (Week 7+)
Objective: Live Fire.

Tasks:

Deploy to QuantVPS.

Fund with minimal capital ($100 USDC).

Monitor "Kill Switch" functionality.

Scale up capital based on Sharpe Ratio performance.

## 13. Future Horizons
The architecture described herein is foundational. Future iterations will expand into:

On-Chain Agency: Allowing the agent to interact directly with smart contracts (Uniswap, Aave) rather than just API-based exchanges.

DAO Integration: The agent could manage a treasury for a DAO, taking governance votes as input for its "Analyst" module.

Self-Improvement: Using Reinforcement Learning from Human Feedback (RLHF) on the trades database to fine-tune a smaller, local model (Llama 3) to mimic the behavior of the expensive Opus 4.6 model, eventually running the entire stack locally and privately.

This report confirms that the technology stack—LLMs, Skill Standards, and Agentic Architectures—is now mature enough to support fully autonomous, reasoning-based financial entities. The proposed system represents a significant leap forward from the brittle algorithms of the past to the adaptive, intelligent agents of the future.
