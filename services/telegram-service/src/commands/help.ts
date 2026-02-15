import type { Bot } from "grammy";

export function registerHelpCommand(bot: Bot): void {
  bot.command("help", async (ctx) => {
    const msg =
      "🤖 NeuraTrade Bot Commands:\n\n" +
      "📋 Getting Started\n" +
      "/start - Register and get started\n" +
      "/help - Show this help message\n\n" +
      "🤖 AI Models (via models.dev)\n" +
      "/ai_models - List all available AI models\n" +
      "/ai_select <model> - Select an AI model\n" +
      "/ai_status - Show your AI configuration\n" +
      "/ai_route [fast|balanced|accurate] - Auto-select best model\n\n" +
      "⚡ Autonomous Trading\n" +
      "/begin - Start autonomous mode\n" +
      "/pause - Pause autonomous mode\n" +
      "/doctor - Run diagnostics\n\n" +
      "💰 Trading & Arbitrage\n" +
      "/opportunities - View arbitrage opportunities\n" +
      "/liquidate <symbol> - Emergency close one position\n" +
      "/liquidate_all CONFIRM - Emergency close all\n\n" +
      "📊 Portfolio & Performance\n" +
      "/summary - 24h performance summary\n" +
      "/performance - Strategy breakdown\n" +
      "/portfolio - View current portfolio\n" +
      "/quests - View active quests\n\n" +
      "💳 Wallets & Exchanges\n" +
      "/wallet - View connected wallets\n" +
      "/connect_exchange - Connect exchange\n" +
      "/connect_polymarket - Connect Polymarket\n" +
      "/add_wallet - Add wallet\n" +
      "/remove_wallet - Remove wallet\n\n" +
      "⚙️ Settings\n" +
      "/settings - Alert preferences\n" +
      "/alerts - Manage alerts\n" +
      "/upgrade - Premium subscription\n" +
      "/status - Account status\n" +
      "/stop - Pause notifications\n" +
      "/resume - Resume notifications\n" +
      "/logs - View operator logs\n\n" +
      "💡 Tip: Use /doctor if /begin fails the readiness gate.";

    await ctx.reply(msg);
  });
}
