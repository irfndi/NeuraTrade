import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import path from "node:path";
import type { Bot } from "grammy";
import { SessionManager } from "../../session";
import { registerAutonomousCommands } from "./autonomous";
import { registerLiquidationCommands } from "./liquidation";
import { registerMonitoringCommands } from "./monitoring";

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

describe("BD command handlers", () => {
  const originalHome = process.env.NEURATRADE_HOME;
  let tempHome = "";

  beforeEach(async () => {
    tempHome = path.join(process.cwd(), ".tmp-test-neuratrade-home-commands");
    await Bun.$`mkdir -p ${tempHome}`.quiet();
    process.env.NEURATRADE_HOME = tempHome;
  });

  afterEach(async () => {
    process.env.NEURATRADE_HOME = originalHome;
    if (tempHome) {
      await Bun.$`rm -rf ${tempHome}`.quiet();
    }
  });

  test("/begin handles readiness gate failure", async () => {
    await Bun.write(
      path.join(tempHome, "config.json"),
      JSON.stringify({ telegram: {} }),
    );

    const bot = new MockBot();
    const sessions = new SessionManager();
    const api = {
      async beginAutonomous() {
        return {
          ok: false,
          readiness_passed: false,
          failed_checks: ["wallet minimum", "exchange permissions"],
        };
      },
      async pauseAutonomous() {
        return { ok: true };
      },
    };

    registerAutonomousCommands(
      bot as unknown as Bot,
      api as unknown as never,
      sessions,
    );

    const ctx = createContext("/begin");
    await runCommand(bot, "begin", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("Readiness gate blocked autonomous mode");
    expect(ctx.replies[0]).toContain("wallet minimum");

    const cfg = (await Bun.file(path.join(tempHome, "config.json")).json()) as {
      telegram?: { chat_id?: string };
      services?: { telegram?: { chat_id?: string } };
    };
    expect(cfg.telegram?.chat_id).toBe("777");
    expect(cfg.services?.telegram?.chat_id).toBe("777");
  });

  test("/liquidate_all requires confirmation before execution", async () => {
    const bot = new MockBot();
    const sessions = new SessionManager();
    let liquidateAllCalls = 0;
    const api = {
      async liquidateAll() {
        liquidateAllCalls += 1;
        return { ok: true };
      },
      async liquidate() {
        return { ok: true };
      },
    };

    registerLiquidationCommands(
      bot as unknown as Bot,
      api as unknown as never,
      sessions,
    );

    const ctx = createContext("/liquidate_all", 888);
    await runCommand(bot, "liquidate_all", ctx);

    expect(liquidateAllCalls).toBe(0);
    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("/liquidate_all CONFIRM");

    const session = sessions.getSession("888");
    expect(session).not.toBeNull();
    expect(session?.step).toBe("awaiting_liquidation_confirm");
  });

  test("/liquidate_all CONFIRM executes and clears session", async () => {
    const bot = new MockBot();
    const sessions = new SessionManager();
    sessions.setSession("999", {
      step: "awaiting_liquidation_confirm",
      data: { action: "liquidate_all" },
    });

    let liquidateAllCalls = 0;
    const api = {
      async liquidateAll() {
        liquidateAllCalls += 1;
        return {
          ok: true,
          message: "All positions closed",
          liquidated_count: 4,
        };
      },
      async liquidate() {
        return { ok: true };
      },
    };

    registerLiquidationCommands(
      bot as unknown as Bot,
      api as unknown as never,
      sessions,
    );

    const ctx = createContext("/liquidate_all CONFIRM", 999);
    await runCommand(bot, "liquidate_all", ctx);

    expect(liquidateAllCalls).toBe(1);
    expect(ctx.replies[0]).toContain("Emergency Liquidation Complete");
    expect(ctx.replies[0]).toContain("Positions closed: 4");
    expect(sessions.getSession("999")).toBeNull();
  });

  test("/doctor renders diagnostic summary", async () => {
    const bot = new MockBot();
    const api = {
      async getDoctor() {
        return {
          overall_status: "warning",
          summary: "1 degraded dependency",
          checked_at: "2026-02-12T01:23:45Z",
          checks: [
            {
              name: "redis",
              status: "healthy",
              latency_ms: 7,
              details: { region: "ap-southeast-1" },
            },
            {
              name: "exchange-bridge",
              status: "warning",
              message: "retrying websocket",
            },
          ],
        };
      },
      async getQuests() {
        return { quests: [] };
      },
      async getPortfolio() {
        return { total_equity: "0", positions: [] };
      },
      async getLogs() {
        return { logs: [] };
      },
    };

    registerMonitoringCommands(bot as unknown as Bot, api as unknown as never);

    const ctx = createContext("/doctor");
    await runCommand(bot, "doctor", ctx);

    expect(ctx.replies).toHaveLength(1);
    expect(ctx.replies[0]).toContain("Doctor: WARNING");
    expect(ctx.replies[0]).toContain("redis");
    expect(ctx.replies[0]).toContain("exchange-bridge");
  });
});
