import type { Bot, Context } from "grammy";
import type { BackendApiClient } from "../api/client";

export function registerBindCommand(
  bot: Bot,
  api: BackendApiClient,
): void {
  bot.command("bind", async (ctx: Context) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;
    const username = ctx.from?.username;

    if (!chatId || !userId) {
      await ctx.reply(
        "❌ Unable to process binding: missing chat information.\n\n" +
        "Please try again or contact support if the issue persists."
      );
      return;
    }

    const messageText = ctx.message?.text || "";
    const parts = messageText.split(" ").filter((p) => p.trim());
    
    if (parts.length < 2) {
      await ctx.reply(
        "🔐 *Operator Profile Binding*\n\n" +
        "To bind your operator profile to this Telegram chat, please provide your auth code.\n\n" +
        "*Usage:* `/bind <auth_code>`\n\n" +
        "*Example:* `/bind ABC123XYZ`\n\n" +
        "You can obtain an auth code from:\n" +
        "• The web dashboard (Settings → Telegram Binding)\n" +
        "• CLI: `neuratrade generate-auth-code`",
        { parse_mode: "Markdown" }
      );
      return;
    }

    const authCode = parts[1].trim().toUpperCase();

    if (!/^[A-Z0-9]{6,32}$/.test(authCode)) {
      await ctx.reply(
        "❌ Invalid auth code format.\n\n" +
        "Auth codes should be 6-32 characters long and contain only letters and numbers.\n\n" +
        "*Example:* `/bind ABC123XYZ`",
        { parse_mode: "Markdown" }
      );
      return;
    }

    try {
      const result = await api.bindOperatorProfile({
        chatId: String(chatId),
        telegramUserId: String(userId),
        telegramUsername: username || null,
        authCode,
      });

      if (result.success) {
        await ctx.reply(
          "✅ *Binding Successful!*\n\n" +
          `Your operator profile has been successfully bound to this Telegram chat.\n\n` +
          `*Bound Profile:* ${result.operatorName || "Operator"}\n` +
          `*Telegram User:* @${username || userId}\n\n` +
          "You will now receive:\n" +
          "• Trading notifications\n" +
          "• Risk alerts\n" +
          "• Performance summaries\n\n" +
          "Use /settings to configure your notification preferences.",
          { parse_mode: "Markdown" }
        );
      } else {
        await ctx.reply(
          "❌ *Binding Failed*\n\n" +
          `Error: ${result.error || "Unknown error occurred"}\n\n` +
          "Please check your auth code and try again.\n" +
          "If the problem persists, generate a new auth code from the dashboard."
        );
      }
    } catch (error) {
      console.error("[Bind] Error binding operator profile:", error);
      await ctx.reply(
        "❌ *Binding Failed*\n\n" +
        "An unexpected error occurred while processing your binding request.\n\n" +
        "Please try again later or contact support if the issue persists."
      );
    }
  });

  bot.command("unbind", async (ctx: Context) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId || !userId) {
      await ctx.reply("❌ Unable to process: missing chat information.");
      return;
    }

    try {
      const result = await api.unbindOperatorProfile({
        chatId: String(chatId),
        telegramUserId: String(userId),
      });

      if (result.success) {
        await ctx.reply(
          "✅ *Unbinding Successful*\n\n" +
          "Your Telegram chat has been unbound from the operator profile.\n\n" +
          "You will no longer receive notifications on this chat.\n\n" +
          "To re-bind, use /bind <auth_code>"
        );
      } else {
        await ctx.reply(
          "❌ *Unbinding Failed*\n\n" +
          `Error: ${result.error || "No binding found for this chat"}`
        );
      }
    } catch (error) {
      console.error("[Unbind] Error unbinding operator profile:", error);
      await ctx.reply(
        "❌ *Unbinding Failed*\n\n" +
        "An unexpected error occurred. Please try again later."
      );
    }
  });
}
