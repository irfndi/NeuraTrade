import { Effect } from "effect";

/**
 * Resolve a string value from env → runtime → fallback.
 *
 * Mirrors Go `getEnvOrRuntimeString`: env var wins if set (whitespace-trimmed),
 * then runtime value if non-empty, then fallback.
 */
export function getEnvOrRuntimeString(
  name: string,
  runtimeValue: string,
  fallback: string,
): Effect.Effect<string, never, never> {
  return Effect.sync(() => {
    const envVal = process.env[name];
    if (envVal !== undefined) {
      const trimmed = envVal.trim();
      if (trimmed !== "") return trimmed;
    }
    const rtTrimmed = runtimeValue.trim();
    if (rtTrimmed !== "") return rtTrimmed;
    return fallback;
  });
}

/**
 * Resolve a port string from env → runtime → fallback.
 *
 * Env var is used as-is (whitespace-trimmed). Runtime port must be 1–65535.
 * Returns the port as a string.
 */
export function getEnvOrRuntimePort(
  name: string,
  runtimePort: number,
  fallback: string,
): Effect.Effect<string, never, never> {
  return Effect.sync(() => {
    const envVal = process.env[name];
    if (envVal !== undefined) {
      const trimmed = envVal.trim();
      if (trimmed !== "") return trimmed;
    }
    if (
      Number.isInteger(runtimePort) &&
      runtimePort >= 1 &&
      runtimePort <= 65535
    ) {
      return String(runtimePort);
    }
    return fallback;
  });
}

/**
 * Resolve a boolean value from env → runtime → fallback.
 *
 * Env var is parsed with `strtobool`-style rules (true/false/1/0/yes/no).
 * If the env var is set but unparseable, the fallback is returned.
 * If the env var is absent, `runtimeValue` is returned (when defined),
 * otherwise the fallback.
 */
export function getEnvOrRuntimeBool(
  name: string,
  runtimeValue: boolean | undefined,
  fallback: boolean,
): Effect.Effect<boolean, never, never> {
  return Effect.sync(() => {
    const envVal = process.env[name];
    if (envVal !== undefined) {
      const trimmed = envVal.trim().toLowerCase();
      if (
        trimmed === "true" ||
        trimmed === "1" ||
        trimmed === "yes" ||
        trimmed === "on"
      ) {
        return true;
      }
      if (
        trimmed === "false" ||
        trimmed === "0" ||
        trimmed === "no" ||
        trimmed === "off"
      ) {
        return false;
      }
      return fallback;
    }
    if (runtimeValue !== undefined) return runtimeValue;
    return fallback;
  });
}

/**
 * Resolve a duration in seconds from env → runtime → fallback.
 *
 * Env var is parsed as a positive integer. Invalid/zero/negative env values
 * fall through. Runtime must be > 0. Fallback is always accepted.
 */
export function getEnvOrRuntimeDurationSeconds(
  name: string,
  runtimeSeconds: number,
  fallback: number,
): Effect.Effect<number, never, never> {
  return Effect.sync(() => {
    const envVal = process.env[name];
    if (envVal !== undefined) {
      const trimmed = envVal.trim();
      if (trimmed !== "") {
        const parsed = Number(trimmed);
        if (Number.isInteger(parsed) && parsed > 0) {
          return parsed;
        }
        return fallback;
      }
    }
    if (Number.isInteger(runtimeSeconds) && runtimeSeconds > 0) {
      return runtimeSeconds;
    }
    return fallback;
  });
}
