import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import { logger } from "../utils/logger";

export function registerStartCommand(bot: Bot, api: BackendApiClient): void {
  bot.command("start", async (ctx) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId || !userId) {
      logger.error("[Start] Missing chat information", {
        from: ctx.from?.username || "unknown",
      });
      await ctx.reply("Unable to start: missing chat information.");
      return;
    }

    const chatIdStr = String(chatId);
    logger.info("[Start] Processing command", {
      userId,
      chatId: chatIdStr,
      username: ctx.from?.username,
    });

    try {
      const userResult = await api.getUserByChatId(chatIdStr);

      if (!userResult) {
        logger.info("[Start] Registering new user", { userId, chatId: chatIdStr });
        const password = `${globalThis.crypto.randomUUID()}${globalThis.crypto.randomUUID()}`;
        await api.registerTelegramUser({
          email: `telegram_${userId}@neuratrade.ai`,
          password: password,
          telegram_chat_id: chatIdStr,
        });
        logger.info("[Start] User registered successfully", { userId, chatId: chatIdStr });
        
        // Send credentials securely (in production, use secure channel)
        await ctx.reply(
          "🚀 Welcome to NeuraTrade!\n\n" +
          "✅ Your account has been created.\n\n" +
          "⚠️ IMPORTANT: Save your temporary password:\n" +
          `\`${password}\`\n\n` +
          "Use /settings to change your password.\n\n" +
          "Get started:\n" +
          "• /ai_models - View available AI models\n" +
          "• /ai_select <model> - Select your AI\n" +
          "• /help - See all commands"
        );
        return;
      }
      
      logger.info("[Start] User already exists", { userId, chatId: chatIdStr });
    } catch (error) {
      // Log registration errors for debugging
      logger.error("[Start] User registration failed", {
        userId,
        chatId: chatIdStr,
        error: error instanceof Error ? error.message : String(error),
      });
      // Don't expose internal errors to users
      await ctx.reply(
        "⚠️ Unable to complete registration. Please try again or contact support."
      );
      return;
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

    await ctx.reply(welcomeMsg);
  });
}
