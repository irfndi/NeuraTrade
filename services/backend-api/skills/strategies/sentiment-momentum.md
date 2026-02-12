---
id: sentiment-momentum
name: Sentiment Momentum Strategy
version: 1.0.0
description: A trading strategy that combines technical indicators with social sentiment analysis to identify momentum-based opportunities
category: strategy
author: NeuraTrade
tags:
  - trading
  - sentiment
  - momentum
  - technical-analysis
dependencies: []
parameters:
  symbol:
    type: string
    description: Trading pair symbol (e.g., BTC/USDT, ETH/USDT)
    required: true
  sentiment_threshold:
    type: number
    description: Minimum sentiment score to trigger signal (-1 to 1 scale)
    required: false
    default: 0.3
  momentum_lookback:
    type: integer
    description: Number of periods for momentum calculation
    required: false
    default: 14
  rsi_oversold:
    type: number
    description: RSI oversold threshold for buy signals
    required: false
    default: 30
  rsi_overbought:
    type: number
    description: RSI overbought threshold for sell signals
    required: false
    default: 70
  position_size_percent:
    type: number
    description: Percentage of available capital to risk per trade
    required: false
    default: 2.0
examples:
  - name: Basic sentiment momentum trade
    description: Enter long position when sentiment is positive and RSI is oversold
    inputs:
      symbol: "BTC/USDT"
      sentiment_threshold: 0.3
      momentum_lookback: 14
      rsi_oversold: 30
    expected: "buy_signal"
  - name: Sentiment reversal trade
    description: Enter short position when sentiment is negative and RSI is overbought
    inputs:
      symbol: "ETH/USDT"
      sentiment_threshold: -0.3
      momentum_lookback: 20
      rsi_overbought: 70
    expected: "sell_signal"
---

# Sentiment Momentum Strategy

This strategy combines technical momentum indicators with social sentiment analysis to identify high-probability trading opportunities. The core hypothesis is that market sentiment (from social media, news, and community channels) can serve as a leading indicator for price movements.

## Strategy Logic

### Entry Conditions

**Long (Buy) Signal:**
1. Sentiment score > `sentiment_threshold` (default: 0.3)
2. RSI < `rsi_oversold` (default: 30) - oversold condition
3. Price momentum is positive (current price > N-period ago)
4. Volume is increasing or stable

**Short (Sell) Signal:**
1. Sentiment score < `-sentiment_threshold` (default: -0.3)
2. RSI > `rsi_overbought` (default: 70) - overbought condition
3. Price momentum is negative
4. Volume is increasing or stable

### Exit Conditions

1. **Take Profit:** 
   - Profit target: 2-3% for spot, 5-10% for futures
   - Alternative: RSI returns to neutral zone (40-60)

2. **Stop Loss:**
   - Hard stop: 1.5x ATR (Average True Range)
   - Trailing stop: Move to breakeven after 1.5% profit

## Technical Indicators Used

### RSI (Relative Strength Index)
- Period: 14 (configurable via `momentum_lookback`)
- Used for: Overbought/oversold conditions

### Momentum Oscillator
- Measures rate of change in price
- Confirms trend direction

### Volume Analysis
- Confirms price moves with volume
- Divergence = potential reversal

## Sentiment Sources

The strategy incorporates sentiment from:
- Twitter/X social signals
- Reddit community sentiment
- News sentiment analysis
- Market maker activity indicators

Sentiment scores range from -1 (extremely bearish) to +1 (extremely bullish).

## Risk Management

### Position Sizing
- Default: 2% of capital per trade (`position_size_percent`)
- Adjust based on confidence score
- Maximum 5% in single asset

### Correlation Checks
- Avoid multiple positions in correlated assets
- Check sector correlation before adding new positions

## Best Practices

1. **Market Regime Awareness:**
   - This strategy works best in trending markets
   - Avoid during high volatility news events
   - Reduce position size during uncertain markets

2. **Sentiment Quality:**
   - Higher sentiment conviction = larger positions
   - Cross-reference multiple sentiment sources
   - Weight recent sentiment more heavily

3. **Time Horizon:**
   - Optimal holding period: 1-7 days
   - For day trading, use shorter momentum lookback (7 periods)
   - For swing trading, use longer lookback (20-30 periods)

## Safety Constraints

- Do not trade during major news events (FOMC, CPI, etc.)
- Always check exchange connectivity before placing orders
- Verify sentiment source availability - do not rely on single source
- Implement circuit breaker if sentiment diverges >50% from price action
- Maximum 3 concurrent positions per strategy instance

## Integration with NeuraTrade

This skill works with the following NeuraTrade components:
- `analyst_agent`: For generating trade ideas based on sentiment + momentum
- `trader_agent`: For executing trades based on signals
- `notification_service`: For alerting on sentiment shifts
- `risk_manager`: For position sizing and stop loss management
