---
name: scalping
description: Executes short-term scalping trades to capture small price movements with high frequency
---

# Scalping

Use this skill to execute scalping strategies that capture small price movements through rapid entry and exit positions.

## When to Use This Skill

Use scalping when:
- Market volatility is sufficient (ATR > 0.5% of price)
- Liquidity is high on the target exchange
- You have low-latency connectivity to exchange APIs
- Market regime is favorable (not during high-impact news events)

## How It Works

Scalping exploits small price inefficiencies through high-frequency trading. Key elements:

1. **Entry Signals**: Technical indicators (RSI, MACD, Bollinger Bands) combined with order book imbalance
2. **Position Holding**: Seconds to minutes, not hours
3. **Exit Strategy**: Tight stop-loss (0.05%-0.1%) and small profit targets (0.1%-0.3%)

## Parameters

- `symbol` (string, required): Trading pair (e.g., "BTC/USDT")
- `exchange` (string, required): Exchange to trade on (must be CEX with sufficient liquidity)
- `side` (string, required): "long" or "short"
- `size` (float, required): Position size in quote currency (USDC)
- `stop_loss_pct` (float, optional): Stop-loss percentage (default 0.001 = 0.1%)
- `take_profit_pct` (float, optional): Take-profit percentage (default 0.002 = 0.2%)

## Best Practices

1. **Check News Calendar**: Never scalp during high-impact news events. Use economic calendar to avoid volatility spikes.

2. **Verify Liquidity**: Ensure the order book has sufficient depth. Scalping requires tight spreads.

3. **Use Limit Orders**: Always use limit orders, not market orders, to control execution price.

4. **Monitor Order Book**: Watch for large orders that could move price against you. Scalp in the direction of market flow.

5. **Rotate Positions**: Limit to maximum 3 concurrent scalp positions to manage exposure.

6. **Time of Day**: Best performance during high-volume periods (Asian and US market overlaps).

## Technical Indicators

- **RSI**: Enter oversold (<30) for long, overbought (>70) for short
- **MACD**: Signal line crossovers for momentum
- **Bollinger Bands**: Mean reversion plays at band extremes
- **Order Book Imbalance**: >60% buy/sell ratio indicates directional pressure

## Safety Constraints

- Maximum 3 concurrent scalp positions at any time
- Stop-loss is mandatory (0.05%-0.1% maximum)
- Do not scalp during high-impact news events (check calendar)
- Maximum 5% of available capital across all scalp positions
- Require minimum 0.3% potential profit before entry (spread + target)
- Halt all scalping if 3 consecutive losses occur (consecutive-loss pause)
- Only trade on exchanges with <100ms latency
