import type { Bot } from "grammy";
import type { BackendApiClient } from "../../api/client";
import type {
  PerformanceBreakdownResponse,
  PerformanceSummaryResponse,
  StrategyPerformance,
} from "../../api/types";
import { formatPerformanceSummaryMessage } from "../../messages";
import { getChatId, toNonEmptyString } from "./helpers";

function toTemplateSummary(
  summary: PerformanceSummaryResponse,
  fallbackTimeframe: string,
): string {
  return formatPerformanceSummaryMessage({
    timeframe: toNonEmptyString(summary.timeframe, fallbackTimeframe),
    pnl: toNonEmptyString(summary.pnl, "N/A"),
    winRate: summary.win_rate,
    sharpe: summary.sharpe,
    drawdown: summary.drawdown,
    trades: summary.trades,
    bestTrade: summary.best_trade,
    worstTrade: summary.worst_trade,
    note: summary.note,
  });
}

function formatStrategyRows(strategies: readonly StrategyPerformance[]): string {
  if (strategies.length === 0) {
    return "No strategy breakdown available.";
  }

  const lines: string[] = [];
  for (const row of strategies) {
    lines.push(`• ${row.strategy}`);
    lines.push(`  PnL: ${toNonEmptyString(row.pnl, "N/A")}`);
    if (row.win_rate) {
      lines.push(`  Win Rate: ${row.win_rate}`);
    }
    if (row.sharpe) {
      lines.push(`  Sharpe: ${row.sharpe}`);
    }
    if (row.drawdown) {
      lines.push(`  Drawdown: ${row.drawdown}`);
    }
    if (typeof row.trades === "number") {
      lines.push(`  Trades: ${row.trades}`);
    }
  }

  return lines.join("\n");
}

function toSummaryFromBreakdown(response: PerformanceBreakdownResponse): PerformanceSummaryResponse {
  return {
    timeframe: response.overall.timeframe || response.timeframe,
    pnl: response.overall.pnl,
    win_rate: response.overall.win_rate,
    sharpe: response.overall.sharpe,
    drawdown: response.overall.drawdown,
    trades: response.overall.trades,
    best_trade: response.overall.best_trade,
    worst_trade: response.overall.worst_trade,
    note: response.overall.note,
  };
}

export function registerPerformanceCommands(bot: Bot, api: BackendApiClient): void {
  bot.command("summary", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch summary: missing chat information.");
      return;
    }

    try {
      const summary = await api.getPerformanceSummary(chatId, "24h");
      await ctx.reply(toTemplateSummary(summary, "24h"));
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch 24h summary (${(error as Error).message}).`,
      );
    }
  });

  bot.command("performance", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch performance: missing chat information.");
      return;
    }

    try {
      const breakdown = await api.getPerformanceBreakdown(chatId, "24h");
      const overall = toTemplateSummary(toSummaryFromBreakdown(breakdown), "24h");
      const strategyRows = formatStrategyRows(breakdown.strategies ?? []);
      const message = `${overall}\n\n📈 Strategy Breakdown\n${strategyRows}`;
      await ctx.reply(message);
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch performance breakdown (${(error as Error).message}).`,
      );
    }
  });
}
