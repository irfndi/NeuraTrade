---
name: sum_to_one_arbitrage
description: Executes sum-to-one arbitrage across multiple legs to capture price discrepancies
---

# Sum-to-One Arbitrage

Use this skill to execute sum-to-one arbitrage strategies that exploit price differences across multiple trading pairs to normalize to a target price of 1.0.

## When to Use This Skill

Use sum-to-one arbitrage when:
- You identify correlated markets with price sums significantly different from 1.0
- You have sufficient capital to bridge multiple legs
- The spread exceeds your minimum profit threshold after fees

## How It Works

Sum-to-one arbitrage exploits the mathematical relationship between related prediction markets. When multiple markets on the same outcome should sum to 1.0, price discrepancies create arbitrage opportunities.

Example: If "YES" for Trump wins = 0.65, "NO" for Trump wins = 0.40, the sum is 1.05. You would:
1. Sell the overpriced side (YES at 0.65)
2. Buy the underpriced side (NO at 0.40)
3. Profit = spread - fees when the markets converge

## Parameters

- `markets` (array, required): List of market condition IDs to arbitrage
- `side` (string, required): "YES" or "NO" for each market
- `size` (float, required): Position size in USDC
- `max_slippage` (float, optional): Maximum acceptable slippage (default 0.02)

## Best Practices

1. **Calculate Spread First**: Always compute the spread before entering. Minimum profitable spread is typically 0.03 (3%) after estimated fees.

2. **Leg Execution**: Execute all legs simultaneously to minimize "legging" risk where one side fills but another doesn't.

3. **Use FOK Orders**: Use Fill-or-Kill (FOK) orders to ensure all legs execute at once, preventing partial fills that could expose you to risk.

4. **Monitor Correlation**: Ensure markets are truly correlated. Unrelated markets summing to 1.0 are not arbitrage opportunities.

5. **Check Liquidity**: Verify sufficient liquidity in each market before placing large orders. Large orders may move prices unfavorably.

## Safety Constraints

- Do not exceed 5% of available capital in a single arbitrage trade
- Maximum 3 concurrent arbitrage positions at any time
- Require minimum 2% spread before execution
- Always calculate fees into profit calculation
- Halt if any leg fails to fill (FOK requirement)
- Do not arbitrage markets with >10% implied correlation break risk
