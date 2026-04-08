import { describe, expect, test } from "bun:test";
import type { Bot } from "grammy";
import { registerAICommands } from "./ai";

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
  match?: string;
  readonly replies: string[];
  reply(text: string): Promise<void>;
}

function createContext(
  options: {
    chatId?: string | number;
    fromId?: string | number;
    match?: string;
  } = {},
): MockContext {
  return {
    chat: options.chatId === undefined ? undefined : { id: options.chatId },
    from: options.fromId === undefined ? undefined : { id: options.fromId },
    match: options.match,
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

describe("AI commands", () => {
  test("/ai_status uses chat ID identity for backend calls", async () => {
    const bot = new MockBot();
    let capturedUserId = "";
    const api = {
      async getAIStatus(userId: string) {
        capturedUserId = userId;
        return {
          readiness: "ready",
          selected_model: "gpt-4o-mini",
          provider: "openai",
          daily_spend: "0.00",
          monthly_spend: "0.00",
          budget_limit: "Unlimited",
          daily_budget_exceeded: false,
        };
      },
    };

    registerAICommands(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext({ chatId: -100123, fromId: 987654 });
    await runCommand(bot, "ai_status", ctx);

    expect(capturedUserId).toBe("-100123");
    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("AI Status");
  });

  test("/ai_models splits oversized payload into multiple Telegram messages", async () => {
    const bot = new MockBot();
    const models = Array.from({ length: 90 }, (_, i) => ({
      model_id: `provider-${Math.floor(i / 6)}-model-${i}-${"x".repeat(60)}`,
      display_name: `Model ${i}`,
      provider: `provider_${Math.floor(i / 6)}`,
      supports_tools: true,
      supports_vision: i % 2 === 0,
      supports_reasoning: false,
      cost: "1.00",
      tier: "standard",
      latency_class: "balanced",
    }));
    const api = {
      async getAIModels() {
        return {
          models,
          providers: [],
        };
      },
    };

    registerAICommands(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext({ chatId: 777, fromId: 777 });
    await runCommand(bot, "ai_models", ctx);

    expect(ctx.replies.length).toBeGreaterThan(1);
    expect(ctx.replies.join("\n")).toContain("Use /ai_select <model>");
  });
});
