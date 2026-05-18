import { describe, expect, test } from "bun:test";
import type { Bot } from "grammy";
import type { QuestDiagnosticsResponse } from "../api/types";
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

interface StatusApiMock {
  getUserByChatId(chatId: string): Promise<{
    user: {
      id: string;
      subscription_tier: string;
      created_at: string;
    };
  }>;
  getNotificationPreference(userId: string): Promise<{ enabled: boolean }>;
  getDoctor(chatId: string): Promise<{
    overall_status: string;
    checked_at: string;
    checks: Array<{ name: string; status: string }>;
  }>;
  getTradingMode(chatId: string): Promise<{
    mode: string;
    confirmations: number;
    required_confirmations: number;
  }>;
  getPortfolio(chatId: string): Promise<{
    total_equity: string;
    exposure: string;
    open_orders?: number;
    positions: unknown[];
  }>;
  getAIStatus(userId: string): Promise<{
    selected_model: string;
    provider: string;
    daily_budget_exceeded: boolean;
  }>;
  getLogs(
    chatId: string,
    limit: number,
  ): Promise<{
    logs: Array<{
      timestamp: string;
      level: string;
      source?: string;
      message?: string;
    }>;
  }>;
  getQuestDiagnostics(chatId: string): Promise<QuestDiagnosticsResponse>;
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

function createApiMock(overrides: Partial<StatusApiMock> = {}): StatusApiMock {
  return {
    async getUserByChatId() {
      return {
        user: {
          id: "user-default",
          subscription_tier: "free",
          created_at: "2026-02-20T10:00:00Z",
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
        confirmations: 0,
        required_confirmations: 2,
      };
    },
    async getPortfolio() {
      return {
        total_equity: "46.93",
        exposure: "0.00",
        positions: [],
      };
    },
    async getAIStatus() {
      return {
        selected_model: "",
        provider: "",
        daily_budget_exceeded: false,
      };
    },
    async getLogs() {
      return { logs: [] };
    },
    async getQuestDiagnostics() {
      return {
        quest_runtime: {},
        chat_runtime: {},
      };
    },
    ...overrides,
  };
}

describe("Status command", () => {
  test("renders account + runtime + trading + AI snapshot", async () => {
    const bot = new MockBot();
    let capturedAIUserId = "";
    const api = createApiMock({
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
          readiness: "ready",
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
    });

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
    const api = createApiMock({
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
      async getQuestDiagnostics() {
        throw new Error("quest diagnostics unavailable");
      },
    });

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

  test("renders recovery diagnostics using *_current fields", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getQuestDiagnostics() {
        return {
          quest_runtime: {
            cadence_mode: "active_risk",
            risk_lock_active: false,
          },
          chat_runtime: {
            entry_gate_reason_current:
              "drawdown 37.28% in recovery band: waiting for clean cycles before micro-entry",
            entry_gate_type: "recovery_gate",
            next_unblock_condition_current:
              "Reach 1 clean cycle(s) (current 0)",
            recovery_mode: "micro_entry",
            recovery_clean_cycles_current: 0,
            recovery_clean_cycles_required: 1,
            recovery_cycles_to_entry: 1,
            recovery_entry_allowed: false,
            recovery_gate_eval_at: "2026-03-05T10:20:13Z",
          },
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(666, 777);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain("Entry gate reason:");
    expect(ctx.replies[0]).toContain("Next unblock: Reach 1 clean cycle(s)");
    expect(ctx.replies[0]).toContain(
      "Recovery: mode=micro_entry, clean_cycles=0/1",
    );
    expect(ctx.replies[0]).toContain("Recovery cycles-to-entry: 1");
    expect(ctx.replies[0]).toContain(
      "Recovery gate eval: 2026-03-05T10:20:13Z",
    );
  });

  test("prefers explicit recovery gate over persisted rollout hints", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getQuestDiagnostics() {
        return {
          quest_runtime: {
            cadence_mode: "active_risk",
            risk_lock_active: false,
          },
          chat_runtime: {
            entry_gate_reason_current:
              "drawdown 37.28% in recovery band: waiting for clean cycles before micro-entry",
            entry_gate_type: "recovery_gate",
            rollout_stage_current: "shadow",
            rollout_status_current: "active",
            rollout_gate_reason_current:
              "strategy_not_live (stage: shadow, status: active)",
          },
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(667, 778);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain(
      "Entry blocker: recovery_gate (drawdown 37.28% in recovery band: waiting for clean cycles before micro-entry)",
    );
    expect(ctx.replies[0]).toContain(
      "Rollout gate: strategy_not_live (stage: shadow, status: active)",
    );
    expect(ctx.replies[0]).not.toContain("Entry blocker: rollout_gate");
  });

  test("renders rollout gate without relying on attempt block code", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getUserByChatId() {
        return {
          user: {
            id: "user-4",
            subscription_tier: "pro",
            created_at: "2026-02-20T10:00:00Z",
          },
        };
      },
      async getQuestDiagnostics() {
        return {
          quest_runtime: {
            cadence_mode: "active_risk",
            risk_lock_active: false,
          },
          chat_runtime: {
            candidate_viable_count: 2,
            rollout_stage_current: "shadow",
            rollout_status_current: "active",
            rollout_gate_reason_current:
              "strategy_not_live (stage: shadow, status: active)",
          },
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(777, 888);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain(
      "Entry blocker: rollout_gate (strategy_not_live (stage: shadow, status: active))",
    );
    expect(ctx.replies[0]).toContain(
      "Rollout gate: strategy_not_live (stage: shadow, status: active)",
    );
  });

  test("live mode omits confirmation progress from status display", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getTradingMode() {
        return {
          mode: "live",
          confirmations: 2,
          required_confirmations: 2,
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(900, 901);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain("Mode: LIVE");
    expect(ctx.replies[0]).not.toContain("confirmations");
  });

  test("dry mode shows confirmation progress in status display", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getTradingMode() {
        return {
          mode: "dry",
          confirmations: 1,
          required_confirmations: 2,
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(901, 902);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain("Mode: DRY (1/2 confirmations)");
  });

  test("renders rollout gate when viable count is zero", async () => {
    const bot = new MockBot();
    const api = createApiMock({
      async getQuestDiagnostics() {
        return {
          quest_runtime: {
            cadence_mode: "active_risk",
            risk_lock_active: false,
          },
          chat_runtime: {
            account_tier: "micro",
            candidate_viable_count: 0,
            effective_max_concurrent_positions: 1,
            entry_attempt_block_reason: "rollout_shadow_block",
            managed_open_positions_effective: 1,
            rollout_stage_current: "shadow",
            rollout_status_current: "active",
            rollout_gate_reason_current:
              "strategy_not_live (stage: shadow, status: active)",
          },
        };
      },
    });

    registerStatusCommand(bot as unknown as Bot, api as unknown as never);
    const ctx = createContext(778, 889);
    await runCommand(bot, "status", ctx);

    expect(ctx.replies[0]).toContain(
      "Entry blocker: rollout_gate (strategy_not_live (stage: shadow, status: active))",
    );
    expect(ctx.replies[0]).toContain(
      "Entry attempt block: rollout_shadow_block",
    );
    expect(ctx.replies[0]).toContain("Account tier: micro");
    expect(ctx.replies[0]).toContain("Position cap: 1/1 managed open");
    expect(ctx.replies[0]).not.toContain("Entry blocker: none");
  });
});
