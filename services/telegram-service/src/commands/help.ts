import type { Bot } from "grammy";

export function registerHelpCommand(bot: Bot): void {
  bot.command("help", async (ctx) => {
    const msg =
      "🤖 NeuraTrade Bot Commands:\n\n" +
      "/start - Register and get started\n" +
      "/opportunities - View current arbitrage opportunities\n" +
      "/settings - Configure your alert preferences\n" +
      "/upgrade - Upgrade to premium subscription\n" +
      "/status - Check your account status\n" +
      "/stop - Pause all notifications\n" +
      "/resume - Resume notifications\n" +
      "/help - Show this help message\n\n" +
      "💡 Tip: You'll receive automatic alerts when profitable opportunities are detected!";

    await ctx.reply(msg);
  });
}
