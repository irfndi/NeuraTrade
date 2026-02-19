import type { Bot } from "grammy";
import type { BackendApiClient } from "../../api/client";
import type { SessionManager } from "../../session";
import { getChatId, persistChatIdToLocalConfig } from "./helpers";

export function registerAutonomousCommands(
  bot: Bot,
  api: BackendApiClient,
  sessions: SessionManager,
): void {
  bot.command("begin", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply(
        "Unable to start autonomous mode: missing chat information.",
      );
      return;
    }

    // Persist chat ownership as soon as command is received, even if readiness fails.
    await persistChatIdToLocalConfig(chatId);

    try {
      const response = await api.beginAutonomous(chatId);

      if (response.readiness_passed === false) {
        const failedChecks = response.failed_checks ?? [];
        const checksText =
          failedChecks.length > 0
            ? `\n\nFailed checks:\n- ${failedChecks.join("\n- ")}`
            : "";

        await ctx.reply(
          `⚠️ Readiness gate blocked autonomous mode.${checksText}\n\nRun /doctor for guided diagnostics.`,
        );
        return;
      }

      sessions.setSession(chatId, { step: "idle", data: {} });
      await ctx.reply(
        response.message ||
          "✅ Autonomous mode started. Use /pause to stop and /summary for 24h results.",
      );
    } catch (error) {
      await ctx.reply(
        `❌ Failed to start autonomous mode (${(error as Error).message}).\nRun /doctor and try again.`,
      );
    }
  });

  bot.command("pause", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply(
        "Unable to pause autonomous mode: missing chat information.",
      );
      return;
    }

    try {
      const response = await api.pauseAutonomous(chatId);
      sessions.clearSession(chatId);
      await ctx.reply(
        response.message ||
          "⏸️ Autonomous mode paused. Use /begin when you are ready to resume.",
      );
    } catch (error) {
      await ctx.reply(
        `❌ Failed to pause autonomous mode (${(error as Error).message}).`,
      );
    }
  });
}
