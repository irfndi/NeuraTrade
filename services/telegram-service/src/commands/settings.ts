import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import { logger } from "../utils/logger";

export function registerSettingsCommands(
  bot: Bot,
  api: BackendApiClient,
): void {
  bot.command("settings", async (ctx) => {
    const userId = ctx.from?.id;
    if (!userId) {
      await ctx.reply("Unable to fetch settings right now.");
      return;
    }

    const preference = await api.getNotificationPreference(String(userId));

    const statusIcon = preference.enabled ? "✅" : "❌";
    const statusText = preference.enabled ? "ON" : "OFF";

    const msg =
      "⚙️ Alert Settings:\n\n" +
      `🔔 Notifications: ${statusIcon} ${statusText}\n` +
      "📊 Min Profit Threshold: 0.5%\n" +
      "⏰ Alert Frequency: Every 5 minutes\n\n" +
      "To change settings:\n" +
      "/stop - Pause notifications\n" +
      "/resume - Resume notifications";

    await ctx.reply(msg);
  });

  bot.command("stop", async (ctx) => {
    const userId = ctx.from?.id;
    if (!userId) {
      await ctx.reply("Unable to update notifications.");
      return;
    }

    try {
      await api.setNotificationPreference(String(userId), false);
    } catch (error) {
      logger.warn("Settings operation failed", { error, userId });
    }

    const msg =
      "⏸️ Notifications Paused\n\n" +
      "You will no longer receive arbitrage alerts.\n\n" +
      "Use /resume to start receiving alerts again.";

    await ctx.reply(msg);
  });

  bot.command("resume", async (ctx) => {
    const userId = ctx.from?.id;
    if (!userId) {
      await ctx.reply("Unable to update notifications.");
      return;
    }

    try {
      await api.setNotificationPreference(String(userId), true);
    } catch (error) {
      logger.warn("Settings operation failed", { error, userId });
    }

    const msg =
      "▶️ Notifications Resumed\n\n" +
      "You will now receive arbitrage alerts again.\n\n" +
      "Use /opportunities to see current opportunities.";

    await ctx.reply(msg);
  });
}
