import type { Context } from "grammy";

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
