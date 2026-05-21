import { describe, expect, test } from "bun:test";
import type { Bot } from "grammy";
import { registerModeCommand } from "./mode";
import { ApiClientError } from "../api/client";

type CommandHandler = (ctx: MockContext) => Promise<void> | void;

class MockBot {
  readonly handlers = new Map<string, CommandHandler>();

  command(name: string, handler: CommandHandler): void {
    this.handlers.set(name, handler);
  }
}

interface MockContext {
  chat?: { id: number | string };
  message?: { text?: string };
  readonly replies: string[];
  reply(text: string): Promise<void>;
}

function createContext(
  text: string,
  chatId: number | string = 777,
): MockContext {
  return {
    chat: { id: chatId },
    message: { text },
    replies: [],
    async reply(replyText: string): Promise<void> {
      this.replies.push(replyText);
    },
  };
}

async function runCommand(
  bot: MockBot,
  name: string,
  ctx: MockContext,
): Promise<void> {
  const handler = bot.handlers.get(name);
  if (!handler) {
    throw new Error(`Missing command handler: ${name}`);
  }
  await handler(ctx);
}

describe("Mode command", () => {
  test("/mode confirm in LIVE mode returns explicit message", async () => {
    const bot = new MockBot();
    const api = {
      async getTradingMode() {
        return {
          mode: "live" as const,
          confirmations: 0,
          required_confirmations: 2,
        };
      },
      async addTradingModeConfirmation() {
        throw new ApiClientError("already in live mode", 400, "/mode/confirm");
      },
      async setTradingMode() {
        return { success: true, mode: "live" };
      },
    };

    registerModeCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext("/mode confirm");
    await runCommand(bot, "mode", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("LIVE MODE already active");
  });

  test("/mode confirm in DRY mode increments confirmation count", async () => {
    const bot = new MockBot();
    const api = {
      async getTradingMode() {
        return {
          mode: "dry" as const,
          confirmations: 0,
          required_confirmations: 2,
        };
      },
      async addTradingModeConfirmation() {
        return {
          confirmations: 1,
          required: 2,
        };
      },
      async setTradingMode() {
        return { success: true, mode: "dry" };
      },
    };

    registerModeCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext("/mode confirm");
    await runCommand(bot, "mode", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("Confirmation 1/2");
    expect(ctx.replies[0]).toContain("More confirmations needed");
  });

  test("/mode live with insufficient confirmations shows guidance", async () => {
    const bot = new MockBot();
    const api = {
      async getTradingMode() {
        return {
          mode: "dry" as const,
          confirmations: 0,
          required_confirmations: 2,
        };
      },
      async setTradingMode() {
        throw new ApiClientError(
          "switching to live mode requires 2 confirmations (current: 0)",
          400,
          "/mode/live",
        );
      },
      async addTradingModeConfirmation() {
        return {
          confirmations: 1,
          required: 2,
        };
      },
    };

    registerModeCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext("/mode live");
    await runCommand(bot, "mode", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("Cannot switch to LIVE MODE");
    expect(ctx.replies[0]).toContain("Use /mode confirm");
  });

  test("/mode live blocked by paper proof gate explains readiness", async () => {
    const bot = new MockBot();
    const api = {
      async getTradingMode() {
        return {
          mode: "dry" as const,
          confirmations: 2,
          required_confirmations: 2,
        };
      },
      async setTradingMode() {
        throw new ApiClientError(
          "live mode proof gate blocked: scalping live paper proof not met: closed_trades_below_live_trial_minimum",
          400,
          "/mode/live",
        );
      },
      async addTradingModeConfirmation() {
        return {
          confirmations: 2,
          required: 2,
        };
      },
    };

    registerModeCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext("/mode live");
    await runCommand(bot, "mode", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("live-readiness proof gate");
    expect(ctx.replies[0]).toContain("positive net PnL after fees");
    expect(ctx.replies[0]).toContain("Real-money execution remains blocked");
  });

  test("/mode live forwards proof gate message from unsuccessful response", async () => {
    const bot = new MockBot();
    const api = {
      async getTradingMode() {
        return {
          mode: "dry" as const,
          confirmations: 2,
          required_confirmations: 2,
        };
      },
      async setTradingMode() {
        return {
          success: false,
          error:
            "scalping live paper proof not met: closed_trades_below_live_trial_minimum",
        };
      },
      async addTradingModeConfirmation() {
        return {
          confirmations: 2,
          required: 2,
        };
      },
    };

    registerModeCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext("/mode live");
    await runCommand(bot, "mode", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("live-readiness proof gate");
    expect(ctx.replies[0]).toContain("Real-money execution remains blocked");
    expect(ctx.replies[0]).not.toContain("Use /mode confirm");
  });
});
