import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";

export function registerStartCommand(bot: Bot, api: BackendApiClient): void {
  bot.command("start", async (ctx) => {
    console.log("[BOT] Processing /start command");
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId || !userId) {
      console.log("[BOT] /start failed: missing chat information");
      await ctx.reply("Unable to start: missing chat information.");
      return;
    }

    const chatIdStr = String(chatId);
    console.log(`[BOT] /start for user ${userId}, chat ${chatIdStr}`);

    try {
      const userResult = await api.getUserByChatId(chatIdStr);

      if (!userResult) {
        console.log("[BOT] Registering new user...");
        await api.registerTelegramUser({
          email: `telegram_${userId}@neuratrade.ai`,
          password: `${globalThis.crypto.randomUUID()}${globalThis.crypto.randomUUID()}`,
          telegram_chat_id: chatIdStr,
        });
        console.log("[BOT] User registered successfully");
      } else {
        console.log("[BOT] User already exists");
      }
    } catch (e) {
      console.log("[BOT] User registration error (ignored):", e);
    }

    const welcomeMsg =
      "🚀 Welcome to NeuraTrade!\n\n" +
      "Your AI-powered trading platform with:\n" +
      "• 🤖 AI Models (OpenAI, Claude, NVIDIA, DeepSeek & more)\n" +
      "• 📈 Automated Trading & Arbitrage Detection\n" +
      "• 💰 Portfolio Management\n" +
      "• ⚡ Real-time Market Data (107+ exchanges)\n\n" +
      "Get started:\n" +
      "• /ai_models - View available AI models\n" +
      "• /ai_select <model> - Select your AI\n" +
      "• /help - See all commands\n\n" +
      "Powered by models.dev - All major AI providers in one place!";

    console.log("[BOT] Sending welcome message...");
    await ctx.reply(welcomeMsg);
    console.log("[BOT] Welcome message sent successfully");
  });
}
