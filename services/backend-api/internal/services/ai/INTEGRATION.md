# AI-Driven Trading Integration Guide

## Overview
This guide shows how to integrate the AI-driven trading system into the NeuraTrade backend.

## Components Created

### 1. AI Brain (`internal/services/ai/brain.go`)
Central AI decision-making system that:
- Analyzes market conditions using LLM
- Makes trading decisions with reasoning
- Integrates with learning system
- Supports multiple strategies (scalping, arbitrage, etc.)

### 2. AI Scalping Service (`internal/services/ai/scalping.go`)
AI-driven scalping implementation:
- Continuously scans for opportunities
- Uses AI Brain to make entry decisions
- Manages position lifecycle (entry/exit)
- Tracks PnL and respects risk limits

### 3. Tool Registry (`internal/services/ai/tools/registry.go`)
Manages AI-callable tools:
- Market data tools
- Execution tools
- Portfolio tools

### 4. Interfaces (`internal/services/ai/interfaces.go`)
Core interfaces:
- ToolRegistry
- LearningSystem
- DecisionRecord
- TradeOutcome

## Integration Steps

### Step 1: Wire AI Components in Main

Edit `cmd/server/main.go`:

```go
import (
    "github.com/irfndi/neuratrade/internal/services/ai"
    "github.com/irfndi/neuratrade/internal/ai/llm"
)

func run() error {
    // ... existing setup ...
    
    // 1. Create LLM client
    llmClient, err := llm.NewClient(cfg.AI.Provider, cfg.AI.APIKey)
    if err != nil {
        return fmt.Errorf("failed to create LLM client: %w", err)
    }
    
    // 2. Create tool registry
    toolRegistry := ai.NewToolRegistry()
    
    // 3. Create learning system (can be in-memory for now)
    learningSystem := ai.NewInMemoryLearningSystem()
    
    // 4. Create AI Brain
    aiBrain := ai.NewAITradingBrain(
        llmClient,
        toolRegistry,
        learningSystem,
        ai.DefaultAIBrainConfig(),
    )
    
    // 5. Create AI Scalping Service
    aiScalpingService := ai.NewAIScalpingService(
        aiBrain,
        toolRegistry,
        ccxtOrderExecutor, // Your existing order executor
        ai.DefaultScalpingConfig(),
    )
    
    // 6. Start AI services
    if err := aiScalpingService.Start(); err != nil {
        return fmt.Errorf("failed to start AI scalping: %w", err)
    }
    defer aiScalpingService.Stop()
    
    // ... rest of setup ...
}
```

### Step 2: Add AI Configuration

Edit `config.yml`:

```yaml
ai:
  provider: "openai"  # or "anthropic", "mlx"
  model: "gpt-4o"
  api_key: ${OPENAI_API_KEY}
  temperature: 0.2
  max_tokens: 2000
  min_confidence: 0.7
  enable_learning: true

scalping:
  enabled: true
  symbols:
    - "BTC/USDT"
    - "ETH/USDT"
    - "SOL/USDT"
  max_positions: 3
  scan_interval: "10s"
  position_hold_time: "2m"
  max_daily_loss: 100
  max_position_size: 100
```

### Step 3: Create AI Trading Endpoints

Edit `internal/api/routes.go`:

```go
func SetupRoutes(
    // ... existing params ...
    aiBrain *ai.AITradingBrain,
    aiScalpingService *ai.AIScalpingService,
) {
    // ... existing routes ...
    
    // AI Trading Routes
    aiHandler := handlers.NewAIHandler(aiBrain, aiScalpingService)
    
    v1 := router.Group("/api/v1")
    {
        ai := v1.Group("/ai")
        {
            ai.POST("/analyze", aiHandler.AnalyzeMarket)
            ai.POST("/decision", aiHandler.GetDecision)
            ai.GET("/scalping/status", aiHandler.GetScalpingStatus)
            ai.POST("/scalping/toggle", aiHandler.ToggleScalping)
        }
    }
}
```

### Step 4: Create AI Handler

Create `internal/api/handlers/ai.go`:

```go
package handlers

import (
    "net/http"
    "github.com/gin-gonic/gin"
    "github.com/irfndi/neuratrade/internal/services/ai"
)

type AIHandler struct {
    brain    *ai.AITradingBrain
    scalping *ai.AIScalpingService
}

func NewAIHandler(brain *ai.AITradingBrain, scalping *ai.AIScalpingService) *AIHandler {
    return &AIHandler{
        brain:    brain,
        scalping: scalping,
    }
}

func (h *AIHandler) AnalyzeMarket(c *gin.Context) {
    var req struct {
        Symbol string `json:"symbol"`
    }
    
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }
    
    // Get AI analysis
    marketState, _ := h.getMarketState(req.Symbol)
    portfolioState, _ := h.getPortfolioState()
    
    reasoningReq := &ai.ReasoningRequest{
        Strategy:       "analysis",
        MarketState:    *marketState,
        PortfolioState: *portfolioState,
    }
    
    resp, err := h.brain.Reason(c, reasoningReq)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "analysis": resp.MarketAnalysis,
        "reasoning": resp.Reasoning,
        "confidence": resp.Confidence,
    })
}

func (h *AIHandler) GetScalpingStatus(c *gin.Context) {
    // Return scalping service status
    c.JSON(http.StatusOK, gin.H{
        "active": true,
        "positions": []interface{}{}, // Get from service
    })
}
```

## Database Schema

Add migration for AI decisions:

```sql
-- database/migrations/XXX_create_ai_decisions.sql
CREATE TABLE ai_decisions (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    strategy TEXT NOT NULL,
    action TEXT NOT NULL,
    side TEXT,
    size_percent REAL,
    entry_price REAL,
    stop_loss REAL,
    take_profit REAL,
    confidence REAL NOT NULL,
    reasoning TEXT NOT NULL,
    market_analysis TEXT,
    risk_assessment TEXT,
    model_used TEXT NOT NULL,
    tokens_used INTEGER,
    executed_at TIMESTAMP,
    outcome TEXT, -- 'win', 'loss', 'pending'
    pnl REAL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ai_decisions_symbol ON ai_decisions(symbol);
CREATE INDEX idx_ai_decisions_created ON ai_decisions(created_at);
CREATE INDEX idx_ai_decisions_strategy ON ai_decisions(strategy);
```

## Usage

### Start AI Scalping

```bash
curl -X POST http://localhost:8080/api/v1/ai/scalping/toggle \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```

### Get AI Analysis

```bash
curl -X POST http://localhost:8080/api/v1/ai/analyze \
  -H "Content-Type: application/json" \
  -d '{"symbol": "BTC/USDT"}'
```

### Get Trading Decision

```bash
curl -X POST http://localhost:8080/api/v1/ai/decision \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "BTC/USDT",
    "strategy": "scalping"
  }'
```

## Monitoring

AI decisions are logged with:
- Decision ID
- Symbol and strategy
- Action and confidence
- Reasoning and analysis
- Model and token usage

View logs:
```bash
tail -f logs/ai_trading.log | grep "AI Scalping"
```

## Next Steps

1. **Implement Market Data Tools** - Connect to your existing market data service
2. **Implement Portfolio Tools** - Connect to wallet/portfolio tracking
3. **Add Learning System** - Store and learn from trade outcomes
4. **Create AI Arbitrage** - Similar to scalping but for arbitrage
5. **Add Risk Management** - AI-driven risk assessment

## Configuration Examples

### Conservative Scalping
```yaml
scalping:
  max_positions: 2
  min_spread_percent: 0.05
  max_position_size: 50
  max_daily_loss: 50
  position_hold_time: "5m"
```

### Aggressive Scalping
```yaml
scalping:
  max_positions: 5
  min_spread_percent: 0.01
  max_position_size: 200
  max_daily_loss: 200
  position_hold_time: "1m"
```

## Troubleshooting

**AI not making decisions:**
- Check LLM API key
- Verify model name is correct
- Check token limits

**Orders not executing:**
- Verify order executor is configured
- Check exchange connectivity
- Review order size limits

**No opportunities found:**
- Check symbol configuration
- Verify market data is flowing
- Review spread/volume filters