import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import { splitIntoTelegramMessages } from "../utils";
import { logger } from "../utils/logger";

const ALERT_TYPES = [
  { type: "arbitrage", label: "Arbitrage Opportunities", emoji: "📈" },
  { type: "technical", label: "Technical Analysis", emoji: "📊" },
  { type: "funding", label: "Funding Rate Changes", emoji: "💰" },
  { type: "price", label: "Price Alerts", emoji: "🔔" },
  { type: "risk", label: "Risk Events", emoji: "⚠️" },
];

/**
 * Register Telegram commands for managing user alerts.
 *
 * Registers handlers for the following commands:
 * - /alerts: lists the caller's alerts or shows available alert types and creation example when none exist.
 * - /alert_add: creates a new alert with a type and minimum profit (defaults applied and type validated).
 * - /alert_toggle: toggles an alert's active state by alert id prefix.
 * - /alert_del: deletes an alert by alert id prefix.
 *
 * Handlers communicate results and errors back to the user via chat replies and use the provided
 * BackendApiClient to perform backend operations.
 *
 * @param bot - The Telegram Bot instance to attach command handlers to.
 * @param api - Backend API client used to list, create, update, and delete alerts.
 */
export function registerAlertsCommands(bot: Bot, api: BackendApiClient): void {
  bot.command("alerts", async (ctx) => {
    const userId = String(ctx.from?.id);
    if (!userId) {
      await ctx.reply("Unable to fetch alerts.");
      return;
    }

    try {
      const response = await api.getUserAlerts(userId);

      if (!response.data || response.data.length === 0) {
        const msg =
          "🔔 Your Alerts\n\n" +
          "No alerts configured yet.\n\n" +
          "Available Alert Types:\n" +
          ALERT_TYPES.map((a) => `${a.emoji} ${a.label}`).join("\n") +
          "\n\nTo create an alert:\n" +
          "/alert_add arbitrage 1.0\n" +
          "(Creates arbitrage alert with 1% min profit)\n\n" +
          "Use /help for more commands.";

        await ctx.reply(msg);
        return;
      }

      const alertList = response.data
        .map((alert, i) => {
          const typeInfo = ALERT_TYPES.find((t) => t.type === alert.alert_type);
          const emoji = typeInfo?.emoji || "🔔";
          const status = alert.is_active ? "✅" : "❌";
          return `${i + 1}. ${emoji} ${typeInfo?.label || alert.alert_type}\n   Status: ${status}\n   ID: ${alert.id}`;
        })
        .join("\n\n");

      const msg =
        `🔔 Your Alerts (${response.data.length})\n\n` +
        alertList +
        "\n\nCommands:\n" +
        "/alert_add [type] [min_profit]\n" +
        "/alert_toggle [id]\n" +
        "/alert_del [id]";

      for (const chunk of splitIntoTelegramMessages(msg)) {
        await ctx.reply(chunk);
      }
    } catch (error) {
      logger.warn("Alert operation failed", { error, userId });
      await ctx.reply("Unable to fetch alerts. Please try again.");
    }
  });

  bot.command("alert_add", async (ctx) => {
    const userId = String(ctx.from?.id);
    if (!userId) {
      await ctx.reply("Unable to create alert.");
      return;
    }

    const args = ctx.message?.text.split(" ").slice(1) || [];
    let alertType = args[0]?.toLowerCase() || "arbitrage";
    const minProfit = parseFloat(args[1]) || 1.0;

    if (!ALERT_TYPES.some((t) => t.type === alertType)) {
      const available = ALERT_TYPES.map((t) => t.type).join(", ");
      await ctx.reply(
        `Invalid alert type: ${alertType}\n\nAvailable: ${available}`,
      );
      return;
    }

    try {
      const response = await api.createAlert(userId, alertType, {
        min_profit: minProfit,
      });

      const typeInfo = ALERT_TYPES.find((t) => t.type === alertType);
      const msg =
        `✅ Alert Created\n\n` +
        `Type: ${typeInfo?.emoji || "🔔"} ${typeInfo?.label || alertType}\n` +
        `Min Profit: ${minProfit}%\n` +
        `ID: ${response.data.id}\n\n` +
        `Use /alerts to view all your alerts.`;

      await ctx.reply(msg);
    } catch (error) {
      logger.warn("Alert operation failed", { error, userId });
      await ctx.reply("Failed to create alert. Please try again.");
    }
  });

  bot.command("alert_toggle", async (ctx) => {
    const args = ctx.message?.text.split(" ").slice(1) || [];
    const alertId = args[0];

    if (!alertId) {
      await ctx.reply(
        "Usage: /alert_toggle [alert_id]\n\nUse /alerts to see IDs.",
      );
      return;
    }

    try {
      const response = await api.getUserAlerts(String(ctx.from?.id));
      const alert = response.data.find((a) => a.id.startsWith(alertId));

      if (!alert) {
        await ctx.reply("Alert not found. Use /alerts to see your alerts.");
        return;
      }

      await api.updateAlert(alert.id, !alert.is_active);
      const status = !alert.is_active ? "enabled" : "disabled";

      await ctx.reply(`✅ Alert ${status}.`);
    } catch (error) {
      logger.warn("Alert operation failed", {
        error,
        userId: String(ctx.from?.id),
      });
      await ctx.reply("Failed to update alert. Please try again.");
    }
  });

  bot.command("alert_del", async (ctx) => {
    const args = ctx.message?.text.split(" ").slice(1) || [];
    const alertId = args[0];

    if (!alertId) {
      await ctx.reply(
        "Usage: /alert_del [alert_id]\n\nUse /alerts to see IDs.",
      );
      return;
    }

    try {
      const response = await api.getUserAlerts(String(ctx.from?.id));
      const alert = response.data.find((a) => a.id.startsWith(alertId));

      if (!alert) {
        await ctx.reply("Alert not found. Use /alerts to see your alerts.");
        return;
      }

      await api.deleteAlert(alert.id);

      await ctx.reply("✅ Alert deleted.");
    } catch (error) {
      logger.warn("Alert operation failed", {
        error,
        userId: String(ctx.from?.id),
      });
      await ctx.reply("Failed to delete alert. Please try again.");
    }
  });
}
