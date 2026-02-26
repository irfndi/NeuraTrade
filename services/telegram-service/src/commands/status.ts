import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import type {
  AIStatusResponse,
  DoctorCheckResponse,
  DoctorResponse,
  LogsResponse,
  PortfolioResponse,
  TradingModeResponse,
} from "../api/types";
import { logger } from "../utils/logger";

function normalizeStatus(status: string): "healthy" | "warning" | "critical" {
  const lowered = status.toLowerCase();
  if (lowered === "healthy" || lowered === "warning" || lowered === "critical") {
    return lowered;
  }
  return "critical";
}

function iconForStatus(status: "healthy" | "warning" | "critical"): string {
  switch (status) {
    case "healthy":
      return "✅";
    case "warning":
      return "⚠️";
    default:
      return "❌";
  }
}

function summarizeCheck(
  checks: readonly DoctorCheckResponse[] | undefined,
  checkName: string,
): string {
  if (!checks || checks.length === 0) {
    return "unknown";
  }
  const found = checks.find((check) => check.name === checkName);
  if (!found) {
    return "unknown";
  }
  const status = normalizeStatus(found.status);
  const detail = found.message ? ` (${found.message})` : "";
  return `${iconForStatus(status)} ${status.toUpperCase()}${detail}`;
}

function summarizeMode(mode: TradingModeResponse): string {
  const modeLabel = mode.mode?.toUpperCase() || "DRY";
  return `${modeLabel} (${mode.confirmations}/${mode.required_confirmations} confirmations)`;
}

function summarizeAI(ai: AIStatusResponse): string {
  const model = ai.selected_model || "none selected";
  const provider = ai.provider || "n/a";
  const budget = ai.daily_budget_exceeded ? "budget exceeded" : "budget ok";
  return `${model} (${provider}, ${budget})`;
}

function shortError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

export function registerStatusCommand(bot: Bot, api: BackendApiClient): void {
  bot.command("status", async (ctx) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId) {
      await ctx.reply("Unable to lookup status: missing chat information.");
      return;
    }

    try {
      const userResult = await api.getUserByChatId(String(chatId));

      if (!userResult) {
        await ctx.reply("User not found. Please use /start to register.");
        return;
      }

      const [
        preferenceResult,
        doctorResult,
        modeResult,
        portfolioResult,
        aiResult,
        logsResult,
      ] = await Promise.allSettled([
        userId
          ? api.getNotificationPreference(String(userId))
          : Promise.resolve({ enabled: true }),
        api.getDoctor(String(chatId)),
        api.getTradingMode(String(chatId)),
        api.getPortfolio(String(chatId)),
        api.getAIStatus(String(chatId)),
        api.getLogs(String(chatId), 1),
      ]);

      const preference =
        preferenceResult.status === "fulfilled"
          ? preferenceResult.value
          : { enabled: true };

      const createdAt = new Date(
        userResult.user.created_at,
      ).toLocaleDateString();
      const tier = userResult.user.subscription_tier;
      const notificationStatus = preference.enabled ? "Active" : "Paused";
      const lines = [
        "📊 Account Status",
        "",
        `💰 Subscription: ${tier}`,
        `📅 Member since: ${createdAt}`,
        `🔔 Notifications: ${notificationStatus}`,
        "",
        "🩺 Runtime Snapshot",
      ];

      if (doctorResult.status === "fulfilled") {
        const doctor: DoctorResponse = doctorResult.value;
        const overall = normalizeStatus(doctor.overall_status);
        lines.push(
          `• Health: ${iconForStatus(overall)} ${overall.toUpperCase()}`,
          `• Autonomous: ${summarizeCheck(doctor.checks, "autonomous-mode")}`,
          `• Exchange: ${summarizeCheck(doctor.checks, "exchange-connection")}`,
        );
        if (doctor.checked_at) {
          lines.push(`• Last health check: ${doctor.checked_at}`);
        }
      } else {
        lines.push(
          `• Health: unavailable (${shortError(doctorResult.reason)})`,
        );
      }

      lines.push("", "⚙️ Trading Snapshot");

      if (modeResult.status === "fulfilled") {
        lines.push(`• Mode: ${summarizeMode(modeResult.value)}`);
      } else {
        lines.push(`• Mode: unavailable (${shortError(modeResult.reason)})`);
      }

      if (portfolioResult.status === "fulfilled") {
        const portfolio: PortfolioResponse = portfolioResult.value;
        lines.push(
          `• Equity: ${portfolio.total_equity}`,
          `• Exposure: ${portfolio.exposure ?? "0.00"}`,
          `• Open positions: ${portfolio.positions?.length ?? 0}`,
        );
        if (typeof portfolio.open_orders === "number") {
          lines.push(`• Open orders: ${portfolio.open_orders}`);
        }
      } else {
        lines.push(
          `• Portfolio: unavailable (${shortError(portfolioResult.reason)})`,
        );
      }

      lines.push("", "🤖 AI Snapshot");

      if (aiResult.status === "fulfilled") {
        lines.push(`• Model: ${summarizeAI(aiResult.value)}`);
      } else {
        lines.push(`• Model: unavailable (${shortError(aiResult.reason)})`);
      }

      if (logsResult.status === "fulfilled") {
        const logs: LogsResponse = logsResult.value;
        if (logs.logs && logs.logs.length > 0) {
          const lastLog = logs.logs[0];
          lines.push(`• Last activity: ${lastLog.timestamp} (${lastLog.level})`);
        }
      }

      lines.push(
        "",
        "ℹ️ No recent trade messages does not always mean stopped. Use /doctor for detailed diagnostics.",
      );

      await ctx.reply(lines.join("\n"));

      if (doctorResult.status === "rejected") {
        logger.warn("[Status] Doctor unavailable while building status", {
          chatId,
          error: shortError(doctorResult.reason),
        });
      }
      if (modeResult.status === "rejected") {
        logger.warn("[Status] Mode unavailable while building status", {
          chatId,
          error: shortError(modeResult.reason),
        });
      }
      if (portfolioResult.status === "rejected") {
        logger.warn("[Status] Portfolio unavailable while building status", {
          chatId,
          error: shortError(portfolioResult.reason),
        });
      }
      if (aiResult.status === "rejected") {
        logger.warn("[Status] AI status unavailable while building status", {
          chatId,
          error: shortError(aiResult.reason),
        });
      }
      if (logsResult.status === "rejected") {
        logger.warn("[Status] Logs unavailable while building status", {
          chatId,
          error: shortError(logsResult.reason),
        });
      }
    } catch (error) {
      logger.error(
        "[Status] Failed to build /status response",
        error instanceof Error ? error : new Error(String(error)),
        { chatId, userId },
      );
      await ctx.reply("Unable to fetch status. Please try again later.");
    }
  });
}
