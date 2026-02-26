import type { Bot } from "grammy";
import { logger } from "../utils/logger";

export interface TelegramMenuCommand {
  command: string;
  description: string;
}

export const TELEGRAM_COMMAND_MENU: readonly TelegramMenuCommand[] = [
  { command: "start", description: "Register and get started" },
  { command: "help", description: "Show all commands" },
  { command: "ai_models", description: "List available AI models" },
  { command: "ai_select", description: "Select AI model (/ai_select <model>)" },
  { command: "ai_status", description: "Show selected AI model and spend" },
  {
    command: "ai_route",
    description: "Route best model (/ai_route fast|balanced|accurate)",
  },
  { command: "begin", description: "Start autonomous mode" },
  { command: "pause", description: "Pause autonomous mode" },
  { command: "doctor", description: "Run diagnostics" },
  { command: "summary", description: "24h PnL summary" },
  { command: "performance", description: "Strategy performance breakdown" },
  { command: "portfolio", description: "Portfolio and open positions" },
  { command: "wallet", description: "List connected wallets/exchanges" },
  {
    command: "connect_exchange",
    description: "Connect exchange (/connect_exchange <name>)",
  },
  {
    command: "connect_polymarket",
    description: "Connect Polymarket wallet",
  },
  { command: "add_wallet", description: "Add wallet (/add_wallet <address>)" },
  {
    command: "remove_wallet",
    description: "Remove wallet (/remove_wallet <id|address>)",
  },
  { command: "mode", description: "Get or set trading mode" },
  { command: "quests", description: "Show active quest progress" },
  { command: "logs", description: "Show recent operator logs" },
  { command: "liquidate", description: "Liquidate one symbol position" },
  { command: "liquidate_all", description: "Emergency liquidate all positions" },
  { command: "alerts", description: "List configured alerts" },
  { command: "alert_add", description: "Create a new alert rule" },
  { command: "alert_toggle", description: "Enable/disable an alert rule" },
  { command: "alert_del", description: "Delete an alert rule" },
  { command: "opportunities", description: "Show top arbitrage opportunities" },
  { command: "status", description: "Show bot status" },
  { command: "settings", description: "Show notification settings" },
  { command: "stop", description: "Disable notifications" },
  { command: "resume", description: "Enable notifications" },
] as const;

export async function registerTelegramCommandMenu(bot: Bot): Promise<void> {
  try {
    await bot.api.setMyCommands([...TELEGRAM_COMMAND_MENU]);
    logger.info("Telegram command menu registered", {
      commandCount: TELEGRAM_COMMAND_MENU.length,
    });
  } catch (error) {
    const err = error instanceof Error ? error : new Error(String(error));
    logger.warn("Failed to register Telegram command menu", {
      error: err.message,
    });
  }
}
