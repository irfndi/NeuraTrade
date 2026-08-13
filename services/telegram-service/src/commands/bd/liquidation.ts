import type { LiquidationResponse } from "../../api/types";
import type { SessionManager } from "../../session";
import { getChatId, getCommandArgs } from "./helpers";

function formatLiquidationResult(
  mode: "symbol" | "all",
  message?: string,
  liquidatedCount?: number,
): string {
  const title =
    mode === "all"
      ? "🧯 Emergency Liquidation Complete"
      : "🧯 Liquidation Complete";
  const lines = [title];

  if (message) {
    lines.push("", message);
  }

  if (liquidatedCount !== undefined) {
    lines.push("", `Positions closed: ${liquidatedCount}`);
  }

  return lines.join("\n");
}

/** Minimal grammY context the /liquidate handlers read and reply through. */
export interface LiquidationCommandContext {
  chat?: { id?: number | string };
  message?: { text?: string };
  reply(text: string): Promise<unknown>;
}

/** Minimal bot surface the /liquidate handlers depend on. */
export interface LiquidationCommandBot {
  command(
    name: string,
    handler: (ctx: LiquidationCommandContext) => Promise<void> | void,
  ): void;
}

/** Minimal backend API surface the /liquidate handlers depend on. */
export interface LiquidationCommandApi {
  liquidate(chatId: string, symbol: string): Promise<LiquidationResponse>;
  liquidateAll(chatId: string): Promise<LiquidationResponse>;
}

export function registerLiquidationCommands(
  bot: LiquidationCommandBot,
  api: LiquidationCommandApi,
  sessions: SessionManager,
): void {
  bot.command("liquidate", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply("Unable to liquidate: missing chat information.");
      return;
    }

    const symbol = getCommandArgs(ctx);
    if (!symbol) {
      await ctx.reply(
        "Usage: /liquidate <symbol>\nExample: /liquidate BTC/USDT",
      );
      return;
    }

    // Validate symbol format (e.g., BTC/USDT, ETH-PERP/USDT)
    if (!/^[A-Z0-9-]+\/[A-Z0-9-]+$/.test(symbol)) {
      await ctx.reply(
        "Invalid symbol format. Expected format: BASE/QUOTE (e.g., BTC/USDT)",
      );
      return;
    }

    try {
      const response = await api.liquidate(chatId, symbol);
      await ctx.reply(
        formatLiquidationResult(
          "symbol",
          response.message,
          response.liquidated_count,
        ),
      );
    } catch (error) {
      await ctx.reply(
        `❌ Failed to liquidate ${symbol} (${error instanceof Error ? error.message : String(error)}).`,
      );
    }
  });

  bot.command("liquidate_all", async (ctx) => {
    const chatId = getChatId(ctx);
    if (!chatId) {
      await ctx.reply(
        "Unable to liquidate all positions: missing chat information.",
      );
      return;
    }

    const confirmation = getCommandArgs(ctx).toUpperCase();
    if (confirmation !== "CONFIRM") {
      sessions.setSession(chatId, {
        step: "awaiting_liquidation_confirm",
        data: { action: "liquidate_all" },
      });
      await ctx.reply(
        "⚠️ This will close all positions immediately.\n\nRun /liquidate_all CONFIRM to continue.",
      );
      return;
    }

    try {
      const response = await api.liquidateAll(chatId);
      sessions.clearSession(chatId);
      await ctx.reply(
        formatLiquidationResult(
          "all",
          response.message,
          response.liquidated_count,
        ),
      );
    } catch (error) {
      await ctx.reply(
        `❌ Failed to liquidate all positions (${error instanceof Error ? error.message : String(error)}).`,
      );
    }
  });
}
