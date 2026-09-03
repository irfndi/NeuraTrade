import { ApiClientError } from "../api/client";
import type {
  SetTradingModeResponse,
  TradingModeConfirmationResponse,
  TradingModeResponse,
} from "../api/types";
import { logger } from "../utils/logger";

/** Minimal bot surface the /mode handler depends on. */
export interface ModeCommandBot {
  command(
    name: string,
    handler: (ctx: ModeCommandContext) => Promise<void> | void,
  ): void;
}

/** Minimal grammY context the /mode handler reads and replies through. */
export interface ModeCommandContext {
  chat?: { id?: number | string };
  message?: { text?: string };
  reply(text: string): Promise<unknown>;
}

/** Minimal backend API surface the /mode handler depends on. */
export interface ModeCommandApi {
  getTradingMode(chatId: string): Promise<TradingModeResponse>;
  setTradingMode(
    chatId: string,
    mode: "dry" | "live",
  ): Promise<SetTradingModeResponse>;
  addTradingModeConfirmation(
    chatId: string,
  ): Promise<TradingModeConfirmationResponse>;
}

function isLiveProofGateMessage(message: string): boolean {
  const normalized = message.toLowerCase();
  return (
    normalized.includes("live mode proof gate blocked") ||
    normalized.includes("scalping live paper proof not met")
  );
}

function liveProofGateReply(): string {
  return (
    "⚠️ Cannot switch to LIVE MODE\n\n" +
    "Paper trading has not met the live-readiness proof gate yet.\n" +
    "Keep trading in safe mode until a paper soak shows enough closed trades, wins and losses, positive net PnL after fees, and observed drawdown.\n\n" +
    "Real-money execution remains blocked."
  );
}

const ALREADY_LIVE_REPLY =
  "🔴 Already in LIVE MODE\n\n" +
  "Real trading is currently active.\n" +
  "Use /mode dry to switch back to safe paper mode.";

const LIVE_ACTIVE_CONFIRM_REPLY =
  "🔴 LIVE MODE already active\n\n" +
  "No additional confirmation is required.\n" +
  "Use /mode dry to return to paper trading.";

const CONFIRMATION_REQUIRED_REPLY =
  "⚠️ Cannot switch to LIVE MODE\n\n" +
  "Live mode requires multiple confirmations for safety.\n" +
  "Use /mode confirm to add a confirmation.\n\n" +
  "This protects against accidental live trading.";

const LIVE_ACTIVATED_REPLY =
  "🔴 LIVE MODE ACTIVATED\n\n" +
  "⚠️ REAL TRADING IS NOW ENABLED\n" +
  "⚠️ REAL ORDERS WILL BE EXECUTED\n" +
  "⚠️ REAL MONEY IS AT RISK\n\n" +
  "Use /mode dry to return to safe mode anytime.";

function isAlreadyLiveError(error: ApiClientError): boolean {
  return (
    error.status === 400 &&
    error.message.toLowerCase().includes("already in live mode")
  );
}

function isLiveProofGateError(error: ApiClientError): boolean {
  return error.status === 400 && isLiveProofGateMessage(error.message);
}

function isConfirmationRequiredError(error: ApiClientError): boolean {
  return (
    error.status === 400 &&
    (error.message.toLowerCase().includes("requires") ||
      error.message.toLowerCase().includes("confirmation"))
  );
}

async function handleDryAction(
  api: ModeCommandApi,
  chatId: string,
  currentMode: "dry" | "live" | null,
  reply: (text: string) => Promise<unknown>,
): Promise<void> {
  if (currentMode === "dry") {
    await reply(
      "🧪 Already in DRY MODE\n\n" +
        "Paper trading is active and your funds are safe.\n" +
        "Use /mode live when you are ready for real trading.",
    );
    return;
  }

  await api.setTradingMode(chatId, "dry");
  await reply(
    "✅ Switched to DRY MODE\n\n" +
      "🧪 Paper trading active\n" +
      "No real orders will be executed.\n\n" +
      "Your funds are safe!",
  );
}

async function handleLiveAction(
  api: ModeCommandApi,
  chatId: string,
  currentMode: "dry" | "live" | null,
  reply: (text: string) => Promise<unknown>,
): Promise<void> {
  if (currentMode === "live") {
    await reply(ALREADY_LIVE_REPLY);
    return;
  }

  let result: Awaited<ReturnType<typeof api.setTradingMode>>;
  try {
    result = await api.setTradingMode(chatId, "live");
  } catch (error) {
    if (!(error instanceof ApiClientError)) {
      throw error;
    }
    if (isAlreadyLiveError(error)) {
      await reply(ALREADY_LIVE_REPLY);
      return;
    }
    if (isLiveProofGateError(error)) {
      await reply(liveProofGateReply());
      return;
    }
    if (isConfirmationRequiredError(error)) {
      await reply(CONFIRMATION_REQUIRED_REPLY);
      return;
    }
    throw error;
  }

  if (result.success === false) {
    if (result.error && isLiveProofGateMessage(result.error)) {
      await reply(liveProofGateReply());
      return;
    }
    await reply(CONFIRMATION_REQUIRED_REPLY);
    return;
  }
  await reply(LIVE_ACTIVATED_REPLY);
}

async function handleModeAction(
  api: ModeCommandApi,
  chatId: string,
  action: string,
  reply: (text: string) => Promise<unknown>,
): Promise<void> {
  let currentMode: "dry" | "live" | null = null;
  let requiredConfirmations = 2;
  try {
    const state = await api.getTradingMode(chatId);
    currentMode = state.mode;
    requiredConfirmations = state.required_confirmations || 2;
  } catch (error) {
    logger.warn("Failed to get trading mode", {
      error: error instanceof Error ? error : String(error),
    });
    // Continue with action handling even if state probe fails.
  }

  if (action === "dry") {
    await handleDryAction(api, chatId, currentMode, reply);
    return;
  }
  if (action === "live") {
    await handleLiveAction(api, chatId, currentMode, reply);
    return;
  }
  if (action === "confirm") {
    if (currentMode === "live") {
      await reply(LIVE_ACTIVE_CONFIRM_REPLY);
      return;
    }

    let result: Awaited<ReturnType<typeof api.addTradingModeConfirmation>>;
    try {
      result = await api.addTradingModeConfirmation(chatId);
    } catch (error) {
      if (!(error instanceof ApiClientError)) {
        throw error;
      }
      if (isAlreadyLiveError(error)) {
        await reply(LIVE_ACTIVE_CONFIRM_REPLY);
        return;
      }
      throw error;
    }

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
        `Target confirmations: ${requiredConfirmations}\n\n` +
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
export function registerModeCommand(
  bot: ModeCommandBot,
  api: ModeCommandApi,
): void {
  // Handle /mode and /mode <action>.
  bot.command("mode", async (ctx) => {
    const chatId = ctx.chat?.id;
    const messageText = ctx.message?.text || "";
    if (!chatId) {
      await ctx.reply("Unable to check mode: missing chat information.");
      return;
    }

    let action = "";
    try {
      const parts = messageText.trim().split(/\s+/);
      action = parts[1]?.toLowerCase() || "";
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
      logger.error(
        "[Mode] Unexpected error",
        error instanceof Error ? error : new Error(String(error)),
        { chatId, action },
      );
      await ctx.reply("❌ Failed to process /mode command. Please try again.");
    }
  });
}
