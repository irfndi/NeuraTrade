/**
 * Error formatting utilities.
 *
 * The pattern `err instanceof Error ? err.message : String(err)` does NOT work
 * for `Data.TaggedError` instances: they extend `Error` but `.message` is the
 * default empty string, so the original error context (cause, endpoint, body,
 * etc.) is silently lost. This is especially dangerous for the real-money
 * trading path, where an empty error message during a network failure leaves
 * the operator with no idea what went wrong.
 *
 * Use `errorMessage(err)` everywhere a failure needs to be surfaced to a
 * human, logged, or wrapped in another error. It preserves the full payload
 * for `Data.TaggedError` and falls back gracefully for everything else.
 */

export function errorMessage(err: unknown): string {
  if (typeof err === "string") return err;
  if (err === null) return "null";
  if (err === undefined) return "undefined";
  if (typeof err !== "object") return String(err);
  if ("_tag" in err && typeof err._tag === "string") {
    try {
      return JSON.stringify(err);
    } catch {
      return String(err);
    }
  }
  if (err instanceof Error) {
    if (err.message && err.message.length > 0) return err.message;
    return err.name && err.name.length > 0 ? err.name : String(err);
  }
  try {
    return JSON.stringify(err);
  } catch {
    return String(err);
  }
}
