import type { Bot } from "grammy";

export function registerUpgradeCommand(bot: Bot): void {
  bot.command("upgrade", async (ctx) => {
    const msg =
      "🎯 Upgrade to Premium\n\n" +
      "✨ Premium Benefits:\n" +
      "• Unlimited alerts\n" +
      "• Instant notifications\n" +
      "• Custom profit thresholds\n" +
      "• Website dashboard access\n" +
      "• Priority support\n\n" +
      "💰 Price: $29/month\n\n" +
      "To upgrade, please contact support.";

    await ctx.reply(msg);
  });
}
