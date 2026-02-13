---
id: get_portfolio
name: Get Portfolio
version: 1.0.0
description: Retrieves current portfolio state including balances, positions, and available capital
category: tool
author: NeuraTrade
tags:
  - trading
  - portfolio
  - balance
  - position
dependencies: []
parameters:
  exchange:
    type: string
    description: Exchange to query (e.g., "binance", "polymarket")
    required: false
  symbols:
    type: array
    description: Specific symbols to include in the response
    required: false
examples:
  - name: Get full portfolio
    description: Retrieve complete portfolio across all connected exchanges
    inputs: {}
    expected: "portfolio_summary"
  - name: Get specific exchange portfolio
    description: Retrieve portfolio for a specific exchange
    inputs:
      exchange: "binance"
    expected: "exchange_portfolio"
---

# Get Portfolio

Use this skill to retrieve the current portfolio state, including balances, open positions, and available capital across connected exchanges.

## When to Use This Skill

Use `get_portfolio` when:
- Assessing available capital before placing a trade
- Checking current position exposure
- Verifying balance after a transaction
- Generating portfolio reports for risk management
- Determining position sizing based on available capital

## How It Works

This tool queries the internal portfolio tracking system and, where available, live exchange balances via the CCXT service. It returns:

1. **Available Balance**: Capital available for new positions
2. **Open Positions**: Current active positions with entry prices and unrealized P&L
3. **Total Portfolio Value**: Combined value of all holdings
4. **Exposure Metrics**: Percentage of capital deployed

## Parameters

- `exchange` (string, optional): Filter results to a specific exchange
- `symbols` (array, optional): Filter results to specific trading pairs

## Response Structure

```json
{
  "total_value": "10500.00",
  "available_balance": "8000.00",
  "deployed_capital": "2500.00",
  "positions": [
    {
      "symbol": "BTC/USDT",
      "exchange": "binance",
      "side": "long",
      "size": "0.05",
      "entry_price": "50000.00",
      "current_price": "51000.00",
      "unrealized_pnl": "50.00",
      "unrealized_pnl_pct": "2.0"
    }
  ],
  "balances": {
    "USDC": "8000.00",
    "BTC": "0.05"
  }
}
```

## Best Practices

1. **Before Trading**: Always call `get_portfolio` before placing orders to verify sufficient balance.

2. **Risk Assessment**: Use portfolio data to calculate current exposure and avoid over-leveraging.

3. **Post-Trade Verification**: Call after placing/closing orders to confirm the transaction was executed correctly.

4. **Regular Monitoring**: Use periodic portfolio checks to track performance and detect anomalies.

5. **Multi-Exchange**: When trading across multiple exchanges, request portfolio per exchange to see allocation distribution.

## Safety Constraints

- Do not make trading decisions without first verifying available balance
- Maximum 5% of portfolio in a single position (configurable via risk settings)
- Halt trading if portfolio value drops below minimum threshold
- Always check for pending orders that may affect available balance

## Integration with NeuraTrade

This skill works with:
- `trader_agent`: For position sizing and balance verification before trade execution
- `risk_manager`: For exposure analysis and risk limit enforcement
- `analyst_agent`: For portfolio-based market analysis

## API Endpoint

Internal: `GET /api/trading/portfolio` or `GET /api/telegram/portfolio`
