import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import { registerStartCommand } from "./start";
import { registerHelpCommand } from "./help";
import { registerOpportunitiesCommand } from "./opportunities";
import { registerStatusCommand } from "./status";
import { registerSettingsCommands } from "./settings";
import { registerUpgradeCommand } from "./upgrade";

export { registerStartCommand } from "./start";
export { registerHelpCommand } from "./help";
export { registerOpportunitiesCommand } from "./opportunities";
export { registerStatusCommand } from "./status";
export { registerSettingsCommands } from "./settings";
export { registerUpgradeCommand } from "./upgrade";

export function registerAllCommands(bot: Bot, api: BackendApiClient): void {
  registerStartCommand(bot, api);
  registerHelpCommand(bot);
  registerOpportunitiesCommand(bot, api);
  registerStatusCommand(bot, api);
  registerSettingsCommands(bot, api);
  registerUpgradeCommand(bot);
}
