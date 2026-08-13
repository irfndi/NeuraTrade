import { describe, expect, test } from "bun:test";
import type { AIModelInfo } from "../api/types";
import { registerAICommands, type AIApi } from "./ai";

const defaultModel: AIModelInfo = {
  model_id: "test-model",
  display_name: "Test Model",
  provider: "test-provider",
  supports_tools: false,
  supports_vision: false,
  supports_reasoning: false,
  cost: "1.00",
  tier: "standard",
  latency_class: "balanced",
};

/** Build a complete AIApi double, overriding only the methods a test needs. */
function makeAIApi(overrides: Partial<AIApi> = {}): AIApi {
  return {
    getAIModels: async () => ({ models: [], providers: [] }),
    getAIProviders: async () => ({ providers: [] }),
    getAIProviderModels: async () => ({ provider: "", models: [] }),
    selectAIModel: async () => ({ success: false }),
    getAIStatus: async () => ({}),
    routeAIModel: async () => ({ model: defaultModel }),
    ...overrides,
  };
}

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
  const readinessCases = [
    { readiness: "ready", label: "✅ AI Ready" },
    { readiness: "ready_auto_route", label: "✅ AI Ready (auto-routing)" },
    { readiness: "degraded", label: "⚠️ AI Degraded" },
    { readiness: "unavailable", label: "❌ AI Unavailable" },
  ] as const;

  for (const { readiness, label } of readinessCases) {
    test(`/ai_status renders '${readiness}' readiness correctly`, async () => {
      const bot = new MockBot();
      let capturedUserId = "";
      const api = makeAIApi({
        async getAIStatus(userId: string) {
          capturedUserId = userId;
          return {
            readiness,
            selected_model: "gpt-4o-mini",
            provider: "openai",
            daily_spend: "0.00",
            monthly_spend: "0.00",
            budget_limit: "Unlimited",
            daily_budget_exceeded: false,
          };
        },
      });

      registerAICommands(bot, api);
      const ctx = createContext({ chatId: -100123, fromId: 987654 });
      await runCommand(bot, "ai_status", ctx);

      expect(capturedUserId).toBe("-100123");
      expect(ctx.replies).toHaveLength(1);
      expect(ctx.replies[0]).toContain("AI Status");
      expect(ctx.replies[0]).toContain(label);
    });
  }

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
    const api = makeAIApi({
      async getAIModels() {
        return {
          models,
          providers: [],
        };
      },
    });

    registerAICommands(bot, api);
    const ctx = createContext({ chatId: 777, fromId: 777 });
    await runCommand(bot, "ai_models", ctx);

    expect(ctx.replies.length).toBeGreaterThan(1);
    expect(ctx.replies.join("\n")).toContain("Use /ai_select <model>");
  });
});
