import { homedir } from "node:os";
import path from "node:path";

import { Option, Schema } from "effect";

import { logger } from "../../utils/logger";

export function getChatId(ctx: {
  chat?: { id?: number | string };
}): string | null {
  const chatId = ctx.chat?.id;
  if (!chatId) {
    return null;
  }

  return String(chatId);
}

export function getCommandArgs(ctx: { message?: { text?: string } }): string {
  const text = ctx.message?.text;
  if (!text) {
    return "";
  }

  const firstSpace = text.indexOf(" ");
  if (firstSpace < 0) {
    return "";
  }

  return text.slice(firstSpace + 1).trim();
}

export function toNonEmptyString(
  value: string | undefined,
  fallback: string,
): string {
  if (!value) {
    return fallback;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : fallback;
}

/**
 * Schema for the local NeuraTrade config file. Only the fields this helper
 * reads and rewrites are declared; the schema rejects structurally invalid
 * configs at the JSON boundary instead of narrowing them with hand-rolled
 * runtime checks.
 */
const LocalConfigCategory = Schema.Struct({
  chat_id: Schema.optional(Schema.String),
});

const LocalConfigSchema = Schema.Struct({
  telegram: Schema.optional(LocalConfigCategory),
  services: Schema.optional(
    Schema.Struct({ telegram: Schema.optional(LocalConfigCategory) }),
  ),
});

/** Mutable form of the local config for writing. Decoded by LocalConfigSchema. */
interface LocalConfigCategoryMutable {
  chat_id?: string;
}
interface LocalConfigMutable {
  telegram?: LocalConfigCategoryMutable;
  services?: { telegram?: LocalConfigCategoryMutable };
}

const decodeLocalConfig = Schema.decodeUnknownOption(LocalConfigSchema);

export async function persistChatIdToLocalConfig(
  chatId: string,
): Promise<void> {
  const trimmedChatId = chatId.trim();
  if (!trimmedChatId) {
    return;
  }

  const neuratradeHome =
    process.env.NEURATRADE_HOME || path.join(homedir(), ".neuratrade");
  const configPath = path.join(neuratradeHome, "config.json");
  const configFile = Bun.file(configPath);

  if (!(await configFile.exists())) {
    return;
  }

  let parsed: unknown;
  try {
    parsed = await configFile.json();
  } catch (error) {
    logger.warn(
      "Failed to parse NeuraTrade config while persisting Telegram chat ID",
      {
        configPath,
        error: String(error),
      },
    );
    return;
  }

  const rootOption = decodeLocalConfig(parsed);
  if (Option.isNone(rootOption)) {
    return;
  }
  const current = rootOption.value;

  const currentChatId = current.telegram?.chat_id ?? "";
  const currentServicesChatId = current.services?.telegram?.chat_id ?? "";
  if (
    currentChatId === trimmedChatId &&
    currentServicesChatId === trimmedChatId
  ) {
    return;
  }

  // The decoded result is readonly and keeps only known fields, so update the
  // raw parsed tree instead to preserve every other config property.
  const config = parsed as LocalConfigMutable;
  config.telegram ??= {};
  config.telegram.chat_id = trimmedChatId;
  config.services ??= {};
  config.services.telegram ??= {};
  config.services.telegram.chat_id = trimmedChatId;

  try {
    await Bun.write(configPath, JSON.stringify(config, null, 2) + "\n");
    logger.info("Persisted Telegram chat ID to local config", {
      configPath,
      chatId: trimmedChatId,
    });
  } catch (error) {
    logger.warn("Failed to persist Telegram chat ID to local config", {
      configPath,
      error: String(error),
    });
  }
}
