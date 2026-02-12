import type { Bot } from "grammy";
import type { BackendApiClient } from "../../api/client";
import type {
  DoctorCheckResponse,
  OperatorLogEntry,
  PortfolioPosition,
  QuestProgress,
} from "../../api/types";
import {
  formatDoctorDiagnosticsMessage,
  formatQuestProgressMessage,
} from "../../messages";
import { getChatId } from "./helpers";
import { logger } from "../../utils/logger";

function formatQuestRows(quests: readonly QuestProgress[]): string {
  if (quests.length === 0) {
    return "📋 No active quests right now.";
  }

  return quests
    .map((quest) =>
      formatQuestProgressMessage({
        questId: quest.quest_id,
        questName: quest.quest_name,
        current: quest.current,
        target: quest.target,
        percent: quest.percent,
        status: quest.status,
        timeRemaining: quest.time_remaining,
      }),
    )
    .join("\n\n");
}

function formatPosition(position: PortfolioPosition): string {
  const lines = [
    `• ${position.symbol} (${position.side})`,
    `  Size: ${position.size}`,
  ];

  if (position.entry_price) {
    lines.push(`  Entry: ${position.entry_price}`);
  }

  if (position.mark_price) {
    lines.push(`  Mark: ${position.mark_price}`);
  }

  if (position.unrealized_pnl) {
    lines.push(`  Unrealized PnL: ${position.unrealized_pnl}`);
  }

  return lines.join("\n");
}

function formatPortfolioMessage(input: {
  totalEquity: string;
  availableBalance?: string;
  exposure?: string;
  updatedAt?: string;
  positions: readonly PortfolioPosition[];
}): string {
  const lines = ["💼 Portfolio Snapshot", `Total Equity: ${input.totalEquity}`];

  if (input.availableBalance) {
    lines.push(`Available Balance: ${input.availableBalance}`);
  }

  if (input.exposure) {
    lines.push(`Exposure: ${input.exposure}`);
  }

  if (input.updatedAt) {
    lines.push(`Updated At: ${input.updatedAt}`);
  }

  if (input.positions.length === 0) {
    lines.push("", "No open positions.");
    return lines.join("\n");
  }

  const topPositions = input.positions.slice(0, 5);
  lines.push("", "Open Positions:");
  topPositions.forEach((position) => {
    lines.push(formatPosition(position));
  });

  if (input.positions.length > topPositions.length) {
    lines.push(
      "",
      `Showing ${topPositions.length}/${input.positions.length} positions.`,
    );
  }

  return lines.join("\n");
}

function formatLogs(logs: readonly OperatorLogEntry[]): string {
  if (logs.length === 0) {
    return "🧾 No recent logs available.";
  }

  const lines = ["🧾 Recent Logs"];
  logs.slice(0, 10).forEach((entry) => {
    const sourceText = entry.source ? `/${entry.source}` : "";
    lines.push(
      "",
      `[${entry.timestamp}] ${entry.level.toUpperCase()}${sourceText}`,
      entry.message,
    );
  });

  return lines.join("\n");
}

function normalizeDoctorStatus(
  status: string,
): "healthy" | "warning" | "critical" {
  const lowered = status.toLowerCase();
  if (lowered === "healthy") {
    return "healthy";
  }
  if (lowered === "warning") {
    return "warning";
  }
  if (lowered !== "critical") {
    logger.warn("Unknown doctor status received, defaulting to critical", {
      receivedStatus: status,
    });
  }
  return "critical";
}

function mapDoctorCheck(check: DoctorCheckResponse): {
  name: string;
  status: "healthy" | "warning" | "critical";
  message?: string;
  latencyMs?: number;
  details?: Readonly<Record<string, string>>;
} {
  return {
    name: check.name,
    status: normalizeDoctorStatus(check.status),
    message: check.message,
    latencyMs: check.latency_ms,
    details: check.details,
  };
}

export function registerMonitoringCommands(
  bot: Bot,
  api: BackendApiClient,
): void {
  bot.command("quests", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch quests: missing chat information.");
      return;
    }

    try {
      const response = await api.getQuests(chatId);
      await ctx.reply(formatQuestRows(response.quests ?? []));
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch quests (${(error as Error).message}).`,
      );
    }
  });

  bot.command("portfolio", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch portfolio: missing chat information.");
      return;
    }

    try {
      const response = await api.getPortfolio(chatId);
      await ctx.reply(
        formatPortfolioMessage({
          totalEquity: response.total_equity,
          availableBalance: response.available_balance,
          exposure: response.exposure,
          updatedAt: response.updated_at,
          positions: response.positions ?? [],
        }),
      );
    } catch (error) {
      await ctx.reply(
        `❌ Failed to fetch portfolio (${(error as Error).message}).`,
      );
    }
  });

  bot.command("logs", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to fetch logs: missing chat information.");
      return;
    }

    try {
      const response = await api.getLogs(chatId, 10);
      await ctx.reply(formatLogs(response.logs ?? []));
    } catch (error) {
      await ctx.reply(`❌ Failed to fetch logs (${(error as Error).message}).`);
    }
  });

  bot.command("doctor", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to run doctor: missing chat information.");
      return;
    }

    try {
      const response = await api.getDoctor(chatId);
      const message = formatDoctorDiagnosticsMessage({
        overallStatus: normalizeDoctorStatus(response.overall_status),
        summary: response.summary,
        checkedAt: response.checked_at,
        checks: (response.checks ?? []).map(mapDoctorCheck),
      });
      await ctx.reply(message);
    } catch (error) {
      await ctx.reply(
        `❌ Failed to run doctor diagnostics (${(error as Error).message}).`,
      );
    }
  });
}
