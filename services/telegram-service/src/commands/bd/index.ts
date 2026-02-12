import type { Bot } from "grammy";
import type { BackendApiClient } from "../../api/client";
import type { SessionManager } from "../../session";
import { registerAutonomousCommands } from "./autonomous";
import { registerPerformanceCommands } from "./performance";
import { registerLiquidationCommands } from "./liquidation";
import { registerWalletCommands } from "./wallet";
import { registerMonitoringCommands } from "./monitoring";

export { registerAutonomousCommands } from "./autonomous";
export { registerPerformanceCommands } from "./performance";
export { registerLiquidationCommands } from "./liquidation";
export { registerWalletCommands } from "./wallet";
export { registerMonitoringCommands } from "./monitoring";

export function registerBdCommands(
  bot: Bot,
  api: BackendApiClient,
  sessions: SessionManager,
): void {
  registerAutonomousCommands(bot, api, sessions);
  registerPerformanceCommands(bot, api);
  registerLiquidationCommands(bot, api, sessions);
  registerWalletCommands(bot, api, sessions);
  registerMonitoringCommands(bot, api);
}
