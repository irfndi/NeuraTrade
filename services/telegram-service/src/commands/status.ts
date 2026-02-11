import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";

export function registerStatusCommand(bot: Bot, api: BackendApiClient): void {
  bot.command("status", async (ctx) => {
    const chatId = ctx.chat?.id;
    const userId = ctx.from?.id;

    if (!chatId) {
      await ctx.reply("Unable to lookup status: missing chat information.");
      return;
    }

    try {
      const userResult = await api.getUserByChatId(String(chatId));

      if (!userResult) {
        await ctx.reply("User not found. Please use /start to register.");
        return;
      }

      const preference = userId
        ? await api.getNotificationPreference(String(userId))
        : { enabled: true };

      const createdAt = new Date(userResult.user.created_at).toLocaleDateString();
      const tier = userResult.user.subscription_tier;
      const notificationStatus = preference.enabled ? "Active" : "Paused";

      const msg =
        "📊 Account Status:\n\n" +
        `💰 Subscription: ${tier}\n` +
        `📅 Member since: ${createdAt}\n` +
        `🔔 Notifications: ${notificationStatus}`;

      await ctx.reply(msg);
    } catch {
      await ctx.reply("Unable to fetch status. Please try again later.");
    }
  });
}
