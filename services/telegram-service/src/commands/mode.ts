import type { Bot } from "grammy";
import { ApiClientError } from "../api/client";
import type { BackendApiClient } from "../api/client";

function isApiErrorWithStatus(
  error: unknown,
): error is { status: number; message: string } {
  if (error instanceof ApiClientError) {
    return true;
  }
  if (!error || typeof error !== "object") {
    return false;
  }
  const candidate = error as { status?: unknown; message?: unknown };
  return (
    typeof candidate.status === "number" &&
    typeof candidate.message === "string"
  );
}

async function handleModeAction(
  api: BackendApiClient,
  chatId: string,
  action: string,
  reply: (text: string) => Promise<unknown>,
): Promise<void> {
  if (action === "dry") {
    await api.setTradingMode(chatId, "dry");
    await reply(
      "✅ Switched to DRY MODE\n\n" +
        "🧪 Paper trading active\n" +
        "No real orders will be executed.\n\n" +
        "Your funds are safe!",
    );
    return;
  }

  if (action === "live") {
    let result: { success: boolean; mode?: string };
    try {
      result = await api.setTradingMode(chatId, "live");
    } catch (error) {
      if (
        isApiErrorWithStatus(error) &&
        error.status === 400 &&
        (error.message.toLowerCase().includes("requires") ||
          error.message.toLowerCase().includes("confirmation"))
      ) {
        await reply(
          "⚠️ Cannot switch to LIVE MODE\n\n" +
            "Live mode requires multiple confirmations for safety.\n" +
            "Use /mode confirm to add a confirmation.\n\n" +
            "This protects against accidental live trading.",
        );
        return;
      }
      throw error;
    }

    if (result.success === false) {
      await reply(
        "⚠️ Cannot switch to LIVE MODE\n\n" +
          "Live mode requires multiple confirmations for safety.\n" +
          "Use /mode confirm to add a confirmation.\n\n" +
          "This protects against accidental live trading.",
      );
      return;
    }
    await reply(
      "🔴 LIVE MODE ACTIVATED\n\n" +
        "⚠️ REAL TRADING IS NOW ENABLED\n" +
        "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
        "⚠️ REAL MONEY IS AT RISK\n\n" +
        "Use /mode dry to return to safe mode anytime.",
    );
    return;
  }

  if (action === "confirm") {
    const result = await api.addTradingModeConfirmation(chatId);
    if (result.confirmations >= result.required) {
      await reply(
        `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
          "You have enough confirmations!\n" +
          "Use /mode live to switch to live trading.\n\n" +
          "⚠️ Remember: Live mode uses real money!",
      );
      return;
    }
    await reply(
      `✅ Confirmation ${result.confirmations}/${result.required}\n\n` +
        "More confirmations needed before live trading.\n" +
        "Use /mode confirm again to add another confirmation.\n\n" +
        "This protects against accidental live trading.",
    );
    return;
  }

  await reply(
    "Unknown mode action.\n\n" +
      "Available commands:\n" +
      "/mode - Check current mode\n" +
      "/mode dry - Switch to paper trading\n" +
      "/mode live - Switch to real trading\n" +
      "/mode confirm - Add confirmation for live mode",
  );
}

/**
 * Register the /mode command for checking and changing trading mode.
 */
export function registerModeCommand(bot: Bot, api: BackendApiClient): void {
  // Handle /mode and /mode <action>.
  bot.command("mode", async (ctx) => {
    const chatId = ctx.chat?.id;
    const messageText = ctx.message?.text || "";
    if (!chatId) {
      await ctx.reply("Unable to check mode: missing chat information.");
      return;
    }

    try {
      const parts = messageText.trim().split(/\s+/);
      const action = parts[1]?.toLowerCase() || "";
      if (action !== "") {
        await handleModeAction(api, String(chatId), action, (text) =>
          ctx.reply(text),
        );
        return;
      }

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
      await ctx.reply("❌ Failed to process /mode command. Please try again.");
    }
  });
}
