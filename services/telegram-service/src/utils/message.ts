const TELEGRAM_MAX_MESSAGE_CHARS = 3900;

export function splitIntoTelegramMessages(text: string): string[] {
  if (!text || text.length === 0) {
    return [];
  }
  if (text.length <= TELEGRAM_MAX_MESSAGE_CHARS) {
    return [text];
  }

  const chunks: string[] = [];
  let remaining = text;
  while (remaining.length > TELEGRAM_MAX_MESSAGE_CHARS) {
    let splitAt = remaining.lastIndexOf("\n", TELEGRAM_MAX_MESSAGE_CHARS);
    if (splitAt <= 0) {
      splitAt = TELEGRAM_MAX_MESSAGE_CHARS;
    }
    chunks.push(remaining.slice(0, splitAt).trimEnd());
    remaining = remaining.slice(splitAt).trimStart();
  }
  if (remaining.length > 0) {
    chunks.push(remaining);
  }
  return chunks;
}
