import type { Context } from "grammy";
import { homedir } from "node:os";
import path from "node:path";

import { logger } from "../../utils/logger";

export function getChatId(ctx: Context): string | null {
  const chatId = ctx.chat?.id;
  if (!chatId) {
    return null;
  }

  return String(chatId);
}

export function getCommandArgs(ctx: Context): string {
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

type JsonObject = Record<string, unknown>;

function asObject(value: unknown): JsonObject | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return null;
  }
  return value as JsonObject;
}

export async function persistChatIdToLocalConfig(chatId: string): Promise<void> {
  const trimmedChatId = chatId.trim();
  if (!trimmedChatId) {
    return;
  }

  const neuratradeHome = process.env.NEURATRADE_HOME || path.join(homedir(), ".neuratrade");
  const configPath = path.join(neuratradeHome, "config.json");
  const configFile = Bun.file(configPath);

  if (!(await configFile.exists())) {
    return;
  }

  let parsed: unknown;
  try {
    parsed = await configFile.json();
  } catch (error) {
    logger.warn("Failed to parse NeuraTrade config while persisting Telegram chat ID", {
      configPath,
      error: String(error),
    });
    return;
  }

  const root = asObject(parsed);
  if (!root) {
    return;
  }

  const telegram = asObject(root.telegram) ?? {};
  const services = asObject(root.services) ?? {};
  const servicesTelegram = asObject(services.telegram) ?? {};

  const currentChatId = typeof telegram.chat_id === "string" ? telegram.chat_id : "";
  const currentServicesChatId =
    typeof servicesTelegram.chat_id === "string" ? servicesTelegram.chat_id : "";
  if (currentChatId === trimmedChatId && currentServicesChatId === trimmedChatId) {
    return;
  }

  telegram.chat_id = trimmedChatId;
  servicesTelegram.chat_id = trimmedChatId;
  services.telegram = servicesTelegram;
  root.telegram = telegram;
  root.services = services;

  try {
    await Bun.write(configPath, JSON.stringify(root, null, 2) + "\n");
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
