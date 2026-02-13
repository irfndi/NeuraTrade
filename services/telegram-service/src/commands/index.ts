import type { Bot } from "grammy";
import type { BackendApiClient } from "../api/client";
import type { SessionManager } from "../session";
import { registerStartCommand } from "./start";
import { registerHelpCommand } from "./help";
import { registerOpportunitiesCommand } from "./opportunities";
import { registerStatusCommand } from "./status";
import { registerSettingsCommands } from "./settings";
import { registerUpgradeCommand } from "./upgrade";
import { registerBdCommands } from "./bd";
import { registerBindCommand } from "./bind";

export { registerStartCommand } from "./start";
export { registerHelpCommand } from "./help";
export { registerOpportunitiesCommand } from "./opportunities";
export { registerStatusCommand } from "./status";
export { registerSettingsCommands } from "./settings";
export { registerUpgradeCommand } from "./upgrade";
export { registerBdCommands } from "./bd";
export { registerBindCommand } from "./bind";

export function registerAllCommands(
  bot: Bot,
  api: BackendApiClient,
  sessions: SessionManager,
): void {
  registerStartCommand(bot, api);
  registerHelpCommand(bot);
  registerOpportunitiesCommand(bot, api);
  registerStatusCommand(bot, api);
  registerSettingsCommands(bot, api);
  registerUpgradeCommand(bot);
  registerBdCommands(bot, api, sessions);
  registerBindCommand(bot, api);
}
