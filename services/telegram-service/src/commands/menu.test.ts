import { describe, expect, test } from "bun:test";
import type { Bot } from "grammy";
import {
  TELEGRAM_COMMAND_MENU,
  registerTelegramCommandMenu,
} from "./menu";

describe("Telegram command menu", () => {
  test("registers full command menu via setMyCommands", async () => {
    let registered: ReadonlyArray<{ command: string; description: string }> =
      [];
    const bot = {
      api: {
        async setMyCommands(
          commands: ReadonlyArray<{ command: string; description: string }>,
        ) {
          registered = commands;
          return true;
        },
      },
    };

    await registerTelegramCommandMenu(bot as unknown as Bot);

    expect(registered.length).toBe(TELEGRAM_COMMAND_MENU.length);
    expect(registered.some((c) => c.command === "ai_models")).toBe(true);
    expect(registered.some((c) => c.command === "mode")).toBe(true);
    expect(registered.some((c) => c.command === "portfolio")).toBe(true);
  });
});
