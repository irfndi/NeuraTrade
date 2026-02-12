import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";

export function registerStartCommand(bot: Bot, api: BackendApiClient): void {
  bot.command("start", async (ctx) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId || !userId) {
      await ctx.reply("Unable to start: missing chat information.");
      return;
    }

    const chatIdStr = String(chatId);

    try {
      const userResult = await api.getUserByChatId(chatIdStr);

      if (!userResult) {
        await api.registerTelegramUser({
          email: `telegram_${userId}@neuratrade.ai`,
          password: `${globalThis.crypto.randomUUID()}${globalThis.crypto.randomUUID()}`,
          telegram_chat_id: chatIdStr,
        });
      }
    } catch {
      // Silently ignore errors to match original behavior
    }

    const welcomeMsg =
      "🚀 Welcome to NeuraTrade!\n\n" +
      "✅ You're now registered and ready to receive arbitrage alerts!\n\n" +
      "Use /opportunities to see current opportunities.\n" +
      "Use /help to see available commands.";

    await ctx.reply(welcomeMsg);
  });
}
