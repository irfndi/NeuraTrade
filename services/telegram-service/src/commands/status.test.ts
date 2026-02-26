import { describe, expect, test } from "bun:test";
import type { Bot } from "grammy";
import { registerStatusCommand } from "./status";

type CommandHandler = (ctx: MockContext) => Promise<void> | void;

class MockBot {
  readonly handlers = new Map<string, CommandHandler>();

  command(name: string, handler: CommandHandler): void {
    this.handlers.set(name, handler);
  }
}

interface MockContext {
  chat?: { id: string | number };
  from?: { id: string | number };
  readonly replies: string[];
  reply(text: string): Promise<void>;
}

function createContext(
  chatId: string | number = 777,
  fromId: string | number = 888,
): MockContext {
  return {
    chat: { id: chatId },
    from: { id: fromId },
    replies: [],
    async reply(text: string): Promise<void> {
      this.replies.push(text);
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

describe("Status command", () => {
  test("renders account + runtime + trading + AI snapshot", async () => {
    const bot = new MockBot();
    let capturedAIUserId = "";
    const api = {
      async getUserByChatId() {
        return {
          user: {
            id: "user-1",
            subscription_tier: "pro",
            created_at: "2026-02-25T10:00:00Z",
          },
        };
      },
      async getNotificationPreference() {
        return { enabled: true };
      },
      async getDoctor() {
        return {
          overall_status: "healthy",
          checked_at: "2026-02-26T07:00:00Z",
          checks: [
            { name: "autonomous-mode", status: "healthy" },
            { name: "exchange-connection", status: "healthy" },
          ],
        };
      },
      async getTradingMode() {
        return {
          mode: "dry",
          confirmations: 1,
          required_confirmations: 2,
        };
      },
      async getPortfolio() {
        return {
          total_equity: "45.65",
          exposure: "0.00",
          open_orders: 2,
          positions: [],
        };
      },
      async getAIStatus(userId: string) {
        capturedAIUserId = userId;
        return {
          selected_model: "gpt-4o-mini",
          provider: "openai",
          daily_budget_exceeded: false,
        };
      },
      async getLogs() {
        return {
          logs: [
            {
              timestamp: "2026-02-26T07:01:00Z",
              level: "info",
              source: "engine",
              message: "heartbeat",
            },
          ],
        };
      },
    };

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(555, 999);
    await runCommand(bot, "status", ctx);

    expect(capturedAIUserId).toBe("555");
    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("📊 Account Status");
    expect(ctx.replies[0]).toContain("🩺 Runtime Snapshot");
    expect(ctx.replies[0]).toContain("⚙️ Trading Snapshot");
    expect(ctx.replies[0]).toContain("🤖 AI Snapshot");
    expect(ctx.replies[0]).toContain("Mode: DRY (1/2 confirmations)");
    expect(ctx.replies[0]).toContain("Model: gpt-4o-mini (openai, budget ok)");
  });

  test("degrades gracefully when optional status probes fail", async () => {
    const bot = new MockBot();
    const api = {
      async getUserByChatId() {
        return {
          user: {
            id: "user-2",
            subscription_tier: "free",
            created_at: "2026-02-20T10:00:00Z",
          },
        };
      },
      async getNotificationPreference() {
        throw new Error("notifications endpoint down");
      },
      async getDoctor() {
        throw new Error("doctor timeout");
      },
      async getTradingMode() {
        throw new Error("mode unavailable");
      },
      async getPortfolio() {
        throw new Error("portfolio unavailable");
      },
      async getAIStatus() {
        throw new Error("ai status unavailable");
      },
      async getLogs() {
        throw new Error("logs unavailable");
      },
    };

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext();
    await runCommand(bot, "status", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("Subscription: free");
    expect(ctx.replies[0]).toContain("Health: unavailable (doctor timeout)");
    expect(ctx.replies[0]).toContain("Mode: unavailable (mode unavailable)");
    expect(ctx.replies[0]).toContain(
      "Model: unavailable (ai status unavailable)",
    );
  });
});
