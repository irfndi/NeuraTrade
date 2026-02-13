---
id: get_price
name: Get Price
version: 1.0.0
description: Retrieves current market price and order book data for a trading pair
category: tool
author: NeuraTrade
tags:
  - trading
  - price
  - market-data
  - ticker
  - orderbook
dependencies: []
parameters:
  exchange:
    type: string
    description: Exchange to query (e.g., "binance", "polymarket")
    required: true
  symbol:
    type: string
    description: Trading pair symbol (e.g., "BTC/USDT", "ETH/USDT")
    required: true
  include_orderbook:
    type: boolean
    description: Include order book depth in response
    required: false
    default: false
examples:
  - name: Get current price
    description: Retrieve current market price for BTC/USDT
    inputs:
      exchange: "binance"
      symbol: "BTC/USDT"
    expected: "price_data"
  - name: Get price with order book
    description: Retrieve price and order book depth
    inputs:
      exchange: "binance"
      symbol: "BTC/USDT"
      include_orderbook: true
    expected: "price_with_orderbook"
---

# Get Price

Use this skill to retrieve current market price and optionally order book data for a trading pair on a connected exchange.

## When to Use This Skill

Use `get_price` when:
- Checking current market conditions before placing an order
- Calculating potential profit/loss for a trade
- Monitoring price movements for signal generation
- Determining optimal entry/exit points
- Assessing order book liquidity before large orders

## How It Works

This tool queries the CCXT service to fetch real-time market data from the specified exchange. It returns:

1. **Current Price**: Latest trade price (mid-price for less liquid markets)
2. **Bid/Ask Spread**: Best bid and ask prices
3. **24h Statistics**: High, low, volume (when available)
4. **Order Book** (optional): Depth at multiple price levels

## Parameters

- `exchange` (string, required): Target exchange name
- `symbol` (string, required): Trading pair symbol
- `include_orderbook` (boolean, optional): Include order book data (default: false)

## Response Structure

### Basic Price Response
```json
{
  "exchange": "binance",
  "symbol": "BTC/USDT",
  "price": "50123.45",
  "bid": "50122.00",
  "ask": "50125.00",
  "spread": "3.00",
  "spread_pct": "0.006",
  "timestamp": "2024-01-15T10:30:00Z",
  "24h": {
    "high": "51200.00",
    "low": "49500.00",
    "volume": "12345.67"
  }
}
```

### With Order Book
```json
{
  "exchange": "binance",
  "symbol": "BTC/USDT",
  "price": "50123.45",
  "orderbook": {
    "bids": [
      ["50122.00", "1.5"],
      ["50120.00", "2.3"],
      ["50115.00", "5.0"]
    ],
    "asks": [
      ["50125.00", "1.2"],
      ["50130.00", "3.1"],
      ["50135.00", "4.5"]
    ]
  },
  "metrics": {
    "bid_depth_1pct": "150000",
    "ask_depth_1pct": "120000",
    "imbalance": "0.11"
  }
}
```

## Best Practices

1. **Price Verification**: Always verify price before placing LIMIT orders to ensure reasonable pricing.

2. **Spread Analysis**: Check bid/ask spread before trading - wide spreads indicate low liquidity.

3. **Order Book Assessment**: For larger orders, include order book to estimate slippage.

4. **Timestamp Validation**: Always check timestamp to ensure data freshness (stale data > 30 seconds should be treated cautiously).

5. **Multi-Exchange Comparison**: When arbitrage opportunities exist, compare prices across exchanges.

## Safety Constraints

- Do not trade if price data is stale (> 30 seconds old)
- Do not trade if spread exceeds acceptable threshold (typically > 0.5%)
- Verify exchange connectivity before relying on price data
- Cross-reference with multiple data sources for high-value trades
- Halt if price deviates > 5% from recent average (potential data error)

## Use Cases by Strategy

### Scalping
- Check tight spreads (< 0.1%) before entry
- Monitor order book imbalance for directional signals
- Fast price checks for quick entry/exit

### Arbitrage
- Compare prices across multiple exchanges simultaneously
- Calculate spread after fees before execution
- Monitor order book depth for leg execution risk

### Momentum
- Track price changes over time for trend confirmation
- Verify price movement with volume data
- Check for support/resistance levels

## Integration with NeuraTrade

This skill works with:
- `analyst_agent`: For market analysis and signal generation
- `trader_agent`: For price verification before order execution
- `scalping` strategy: For spread and liquidity assessment
- `sum_to_one_arbitrage` strategy: For price discrepancy detection
- `sentiment_momentum` strategy: For momentum confirmation

## API Endpoint

Internal: `GET /api/market/ticker?exchange={exchange}&symbol={symbol}`
With order book: `GET /api/market/orderbook?exchange={exchange}&symbol={symbol}`
