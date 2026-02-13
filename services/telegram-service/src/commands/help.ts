import type { Bot } from "grammy";

export function registerHelpCommand(bot: Bot): void {
  bot.command("help", async (ctx) => {
    const msg =
      "🤖 NeuraTrade Bot Commands:\n\n" +
      "/start - Register and get started\n" +
      "/bind <auth_code> - Bind operator profile to this chat\n" +
      "/unbind - Unbind operator profile from this chat\n" +
      "/begin - Start autonomous mode\n" +
      "/pause - Pause autonomous mode\n" +
      "/opportunities - View current arbitrage opportunities\n" +
      "/summary - 24h performance summary\n" +
      "/performance - Strategy performance breakdown\n" +
      "/liquidate <symbol> - Emergency close one position\n" +
      "/liquidate_all CONFIRM - Emergency close all positions\n" +
      "/quests - View active quest progress\n" +
      "/portfolio - View current portfolio\n" +
      "/wallet - View connected wallets\n" +
      "/doctor - Run diagnostics\n" +
      "/logs - View recent operator logs\n" +
      "/connect_exchange - Connect exchange account\n" +
      "/connect_polymarket - Connect Polymarket wallet\n" +
      "/add_wallet - Add wallet\n" +
      "/remove_wallet - Remove wallet\n" +
      "/settings - Configure your alert preferences\n" +
      "/upgrade - Upgrade to premium subscription\n" +
      "/status - Check your account status\n" +
      "/stop - Pause all notifications\n" +
      "/resume - Resume notifications\n" +
      "/help - Show this help message\n\n" +
      "💡 Tip: Use /doctor if /begin fails the readiness gate.";

    await ctx.reply(msg);
  });
}
