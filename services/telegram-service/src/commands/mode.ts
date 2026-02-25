import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";

/**
 * Register the /mode command for checking and changing trading mode.
 */
export function registerModeCommand(bot: Bot, api: BackendApiClient): void {
  // Handle /mode command - show current mode
  bot.command("mode", async (ctx) => {
    const chatId = ctx.chat?.id;
    if (!chatId) {
      await ctx.reply("Unable to check mode: missing chat information.");
      return;
    }

    try {
      // Get current mode from API
      const modeResult = await api.getTradingMode(String(chatId));

      const mode = modeResult.mode || "dry";
      const confirmations = modeResult.confirmations || 0;
      const required = modeResult.required_confirmations || 2;

      let msg: string;
      if (mode === "dry") {
        msg =
          "🧪 Current Mode: DRY (Paper Trading)\n\n" +
          "• No real orders executed\n" +
          "• Safe for testing\n\n" +
          `Confirmations: ${confirmations}/${required}\n\n` +
          "Commands:\n" +
          "/mode confirm - Add confirmation\n" +
          "/mode live - Switch to live (requires confirmations)\n" +
          "/mode dry - Switch to dry mode";
      } else {
        msg =
          "🔴 Current Mode: LIVE (Real Trading)\n\n" +
          "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
          "⚠️ REAL MONEY IS AT RISK\n\n" +
          "Commands:\n" +
          "/mode dry - Switch to safe dry mode";
      }

      await ctx.reply(msg);
    } catch (error) {
      console.error("[Mode] Unexpected error:", error);
      await ctx.reply("❌ Failed to get trading mode. Please try again.");
    }
  });

  // Handle /mode with arguments (live, dry, confirm)
  bot.hears(/\/mode\s+(.+)/i, async (ctx) => {
    const chatId = ctx.chat?.id;
    const messageText = ctx.message?.text || "";

    if (!chatId) {
      await ctx.reply("Unable to change mode: missing chat information.");
      return;
    }

    // Parse the action from the message
    const parts = messageText.split(/\s+/);
    const action = parts[1]?.toLowerCase() || "";

    try {
      if (action === "dry") {
        // Switch to dry mode
        await api.setTradingMode(String(chatId), "dry");
        await ctx.reply(
          "✅ Switched to DRY MODE\n\n" +
            "🧪 Paper trading active\n" +
            "No real orders will be executed.\n\n" +
            "Your funds are safe!",
        );
      } else if (action === "live") {
        // Attempt to switch to live mode
        const result = await api.setTradingMode(String(chatId), "live");

        if (result.success === false) {
          await ctx.reply(
            "⚠️ Cannot switch to LIVE MODE\n\n" +
              "Live mode requires multiple confirmations for safety.\n" +
              "Use /mode confirm to add a confirmation.\n\n" +
              "This protects against accidental live trading.",
          );
        } else {
          await ctx.reply(
            "🔴 LIVE MODE ACTIVATED\n\n" +
              "⚠️ REAL TRADING IS NOW ENABLED\n" +
              "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
              "⚠️ REAL MONEY IS AT RISK\n\n" +
              "Use /mode dry to return to safe mode anytime.",
          );
        }
      } else if (action === "confirm") {
        // Add confirmation
        const result = await api.addTradingModeConfirmation(String(chatId));

        if (result.confirmations >= result.required) {
          await ctx.reply(
            `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
              "You have enough confirmations!\n" +
              "Use /mode live to switch to live trading.\n\n" +
              "⚠️ Remember: Live mode uses real money!",
          );
        } else {
          await ctx.reply(
            `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
              "More confirmations needed before live trading.\n" +
              "Use /mode confirm again to add another confirmation.\n\n" +
              "This protects against accidental live trading.",
          );
        }
      } else {
        await ctx.reply(
          "Unknown mode action.\n\n" +
            "Available commands:\n" +
            "/mode - Check current mode\n" +
            "/mode dry - Switch to paper trading\n" +
            "/mode live - Switch to real trading\n" +
            "/mode confirm - Add confirmation for live mode",
        );
      }
    } catch (error) {
      console.error("[ModeAction] Error:", error);
      await ctx.reply("❌ Failed to change mode. Please try again.");
    }
  });
}
