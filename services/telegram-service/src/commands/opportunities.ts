import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import type { ArbitrageOpportunity } from "../api/types";

function formatOpportunitiesMessage(
  opps: readonly ArbitrageOpportunity[],
): string {
  if (!opps || opps.length === 0) {
    return "📊 No arbitrage opportunities found right now.";
  }

  const top = opps.slice(0, 5);
  const lines = ["⚡ Top Arbitrage Opportunities", ""];

  top.forEach((opp, index) => {
    lines.push(`${index + 1}. ${opp.symbol}`);
    lines.push(`   Buy: ${opp.buy_exchange} @ ${opp.buy_price}`);
    lines.push(`   Sell: ${opp.sell_exchange} @ ${opp.sell_price}`);
    lines.push(`   Profit: ${Number(opp.profit_percent).toFixed(2)}%`);
    lines.push("");
  });

  return lines.join("\n");
}

export function registerOpportunitiesCommand(
  bot: Bot,
  api: BackendApiClient,
): void {
  bot.command("opportunities", async (ctx) => {
    try {
      const response = await api.getArbitrageOpportunities(5, 0.5);
      await ctx.reply(formatOpportunitiesMessage(response.opportunities));
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch opportunities. Please try again later. (${(error as Error).message})`,
      );
    }
  });
}
