---
id: place_order
name: Place Order
version: 1.0.0
description: Places a trading order on a connected exchange with validation and risk checks
category: tool
author: NeuraTrade
tags:
  - trading
  - order
  - execution
  - buy
  - sell
dependencies:
  - get_portfolio
parameters:
  exchange:
    type: string
    description: Exchange to place the order on (e.g., "binance", "polymarket")
    required: true
  symbol:
    type: string
    description: Trading pair symbol (e.g., "BTC/USDT", "ETH/USDT")
    required: true
  side:
    type: string
    description: Order side - "BUY" or "SELL"
    required: true
    enum:
      - BUY
      - SELL
  type:
    type: string
    description: Order type - "MARKET" or "LIMIT"
    required: false
    default: "MARKET"
    enum:
      - MARKET
      - LIMIT
  amount:
    type: number
    description: Amount to trade in base currency or quote currency depending on exchange
    required: true
  price:
    type: number
    description: Limit price (required for LIMIT orders)
    required: false
examples:
  - name: Market buy order
    description: Place a market buy order for BTC
    inputs:
      exchange: "binance"
      symbol: "BTC/USDT"
      side: "BUY"
      type: "MARKET"
      amount: "0.1"
    expected: "order_created"
  - name: Limit sell order
    description: Place a limit sell order with specific price
    inputs:
      exchange: "binance"
      symbol: "BTC/USDT"
      side: "SELL"
      type: "LIMIT"
      amount: "0.1"
      price: "52000"
    expected: "order_created"
---

# Place Order

Use this skill to execute trading orders on connected exchanges with built-in validation and risk management checks.

## When to Use This Skill

Use `place_order` when:
- Executing trade signals from analysis
- Opening new positions based on strategy recommendations
- Adjusting portfolio allocation
- Implementing stop-loss or take-profit orders

## How It Works

This tool validates order parameters, checks available balance, applies risk limits, and then submits the order to the specified exchange via the CCXT service or CLOB API.

### Order Flow:
1. **Validation**: Verify symbol, side, amount, and price parameters
2. **Balance Check**: Ensure sufficient funds are available
3. **Risk Assessment**: Apply position size limits and exposure checks
4. **Submission**: Send order to exchange
5. **Confirmation**: Record order in database and return confirmation

## Parameters

- `exchange` (string, required): Target exchange (must be connected)
- `symbol` (string, required): Trading pair (e.g., "BTC/USDT")
- `side` (string, required): "BUY" or "SELL"
- `type` (string, optional): "MARKET" (default) or "LIMIT"
- `amount` (number, required): Quantity to trade
- `price` (number, optional): Required for LIMIT orders

## Response Structure

```json
{
  "status": "success",
  "data": {
    "order": {
      "order_id": "ord-abc123",
      "position_id": "pos-xyz789",
      "exchange": "binance",
      "symbol": "BTC/USDT",
      "side": "BUY",
      "type": "MARKET",
      "amount": "0.1",
      "price": "50000.00",
      "status": "FILLED"
    },
    "position": {
      "position_id": "pos-xyz789",
      "status": "OPEN"
    }
  }
}
```

## Best Practices

1. **Pre-Check Balance**: Call `get_portfolio` before placing orders to verify available funds.

2. **Use Limit Orders**: Prefer LIMIT orders for better price control, especially in volatile markets.

3. **Position Sizing**: Calculate position size based on risk percentage (typically 1-2% of portfolio per trade).

4. **Check News Calendar**: Avoid placing orders during high-impact news events.

5. **Slippage Awareness**: For large orders, check order book depth to estimate slippage.

6. **Order Type Selection**:
   - Use MARKET for immediate execution when speed is critical
   - Use LIMIT for price precision when timing is flexible

## Safety Constraints

- Maximum position size: 5% of available capital (configurable)
- Require sufficient balance + buffer for fees before submission
- Do not place orders during exchange maintenance windows
- Apply rate limiting to avoid exchange API bans
- Halt if consecutive order failures exceed threshold
- Validate symbol is tradeable on the target exchange

## Error Handling

- **Insufficient Balance**: Returns error with available balance
- **Invalid Symbol**: Returns error with list of valid symbols
- **Exchange Unavailable**: Returns error with retry suggestion
- **Rate Limited**: Returns error with cooldown period

## Integration with NeuraTrade

This skill works with:
- `trader_agent`: For executing trade signals
- `risk_manager`: For position size validation and exposure limits
- `scalping` strategy: For rapid entry/exit execution
- `sum_to_one_arbitrage` strategy: For multi-leg order execution

## API Endpoint

Internal: `POST /api/trading/place_order`
