import { Effect, Option, Schema } from "effect";
import { TelegramConfigPartial } from "./config";

// Error types for API classification
export type ApiErrorType =
  | "auth_failed"
  | "not_found"
  | "server_error"
  | "network_error"
  | "unknown";

export interface ApiError {
  type: ApiErrorType;
  status?: number;
  message: string;
  code?: string;
}

/**
 * Schema that decodes an ApiError-shaped payload from the error-capture
 * boundary. Parsing replaces hand-rolled runtime typeof/field checks while
 * keeping the result strongly typed.
 */
const ApiErrorSchema = Schema.Struct({
  type: Schema.Literals([
    "auth_failed",
    "not_found",
    "server_error",
    "network_error",
    "unknown",
  ]),
  message: Schema.String,
  status: Schema.optional(Schema.Number),
  code: Schema.optional(Schema.String),
});

const decodeOptionApiError = Schema.decodeUnknownOption(ApiErrorSchema);

// Custom error class that preserves both ApiError info and stack trace.
// Implements the ApiError shape directly so type guards and handlers can
// read .type/.message without unwrapping.
export class ApiException extends Error implements ApiError {
  readonly type: ApiErrorType;
  readonly status?: number;
  readonly code?: string;
  readonly apiError: ApiError;

  constructor(apiError: ApiError) {
    super(apiError.message);
    this.name = "ApiException";
    this.type = apiError.type;
    this.status = apiError.status;
    this.code = apiError.code;
    this.apiError = apiError;
    // Preserve stack trace
    if (Error.captureStackTrace) {
      Error.captureStackTrace(this, ApiException);
    }
  }
}

// WeakMap to cache extracted ApiErrors - avoids mutation
const extractedApiErrors = new WeakMap<object, ApiError>();

/**
 * Extract ApiError from a potentially wrapped throwable (immutable).
 *
 * `cause` is the raw value captured at the error I/O boundary (an Error, an
 * Effect-style wrapper with a nested `cause`, or a primitive such as null or
 * undefined). It is decoded here with a schema rather than hand-rolled
 * runtime typeof checks.
 */
export const extractApiError = (cause: unknown): ApiError | null => {
  if (cause == null) {
    return null;
  }

  // ApiException - extract the wrapped apiError before the direct shape
  // check, since ApiException implements ApiError and would otherwise be
  // returned as-is instead of unwrapped.
  if (cause instanceof ApiException) {
    return cause.apiError;
  }

  const isObject = cause instanceof Object;

  // Direct ApiError shape (schema-decoded at the boundary)
  if (isObject) {
    const direct = decodeOptionApiError(cause);
    if (Option.isSome(direct)) {
      return direct.value;
    }
  }

  // Check cache first (populated by extractApiError/isApiError)
  if (isObject) {
    const cached = extractedApiErrors.get(cause);
    if (cached) {
      return cached;
    }
  }

  // Follow Effect-style or generic error chaining via `cause`, preventing
  // self-references from recursing forever.
  if (isObject && "cause" in cause && cause.cause !== cause) {
    const extractedFromCause = extractApiError(cause.cause);
    if (extractedFromCause) {
      // Cache the result for future lookups
      extractedApiErrors.set(cause, extractedFromCause);
      return extractedFromCause;
    }
  }

  // Try to parse Error.message as JSON-encoded ApiError
  if (cause instanceof Error && cause.message) {
    try {
      const decoded = decodeOptionApiError(JSON.parse(cause.message));
      if (Option.isSome(decoded)) {
        // Cache the result
        extractedApiErrors.set(cause, decoded.value);
        return decoded.value;
      }
    } catch {
      // Not JSON or not ApiError-shaped; fall through
    }
  }

  return null;
};

// Type guard for ApiError - also handles Effect's wrapped errors by decoding
// the raw throwable at the boundary (see extractApiError).
export const isApiError = (cause: unknown): cause is ApiError =>
  extractApiError(cause) !== null;

export const createApi = (config: TelegramConfigPartial) => {
  const apiFetch = <T>(
    path: string,
    init: RequestInit = {},
    requireAdmin = false,
  ) =>
    Effect.tryPromise({
      try: async () => {
        const headers: Record<string, string> = {};
        headers["Content-Type"] = "application/json";
        Object.assign(
          headers,
          init.headers as Record<string, string> | undefined,
        );

        if (requireAdmin) {
          headers["X-API-Key"] = config.adminApiKey;
        }

        let response: Response;
        try {
          response = await fetch(`${config.apiBaseUrl}${path}`, {
            ...init,
            headers,
          });
        } catch (networkError) {
          console.error(
            `[API] Network error for ${path}:`,
            networkError instanceof Error ? networkError.message : networkError,
          );
          // Throw ApiException to preserve stack trace and error info
          throw new ApiException({
            type: "network_error",
            message: "Network error: Unable to connect to backend API",
          });
        }

        const payload = await response
          .json()
          .catch(() => ({ message: "Failed to parse response" }));

        if (!response.ok) {
          // Classify error type based on status code
          const errorType: ApiErrorType =
            response.status === 401
              ? "auth_failed"
              : response.status === 404
                ? "not_found"
                : response.status >= 500
                  ? "server_error"
                  : "unknown";

          const message =
            payload?.error ||
            payload?.message ||
            `API request failed (${response.status})`;

          // Log authentication failures with context for debugging
          if (errorType === "auth_failed") {
            console.error(
              `[API] Authentication failed for ${path} - check ADMIN_API_KEY configuration`,
            );
          } else {
            console.error(`[API] ${errorType}: ${response.status} for ${path}`);
          }

          // Throw ApiException to preserve stack trace and error info
          throw new ApiException({
            type: errorType,
            status: response.status,
            message,
            code: payload?.code,
          });
        }

        return payload as T;
      },
      catch: (error) =>
        error instanceof ApiException
          ? error
          : new ApiException({
              type: "unknown",
              message: error instanceof Error ? error.message : String(error),
            }),
    });

  // Internal endpoints - no auth required (restricted to trusted internal callers)
  const getUserByChatId = (chatId: string) =>
    apiFetch<{
      user: { id: string; subscription_tier: string; created_at: string };
    }>(
      `/internal/telegram/users/${encodeURIComponent(chatId)}`,
      {},
      false, // No admin auth needed - internal endpoint
    );

  const getNotificationPreference = (userId: string) =>
    apiFetch<{
      enabled: boolean;
      profit_threshold: number;
      alert_frequency: string;
    }>(
      `/internal/telegram/notifications/${encodeURIComponent(userId)}`,
      {},
      false, // No admin auth needed - internal endpoint
    );

  const setNotificationPreference = (userId: string, enabled: boolean) =>
    apiFetch(
      `/internal/telegram/notifications/${encodeURIComponent(userId)}`,
      {
        method: "POST",
        body: JSON.stringify({ enabled }),
      },
      false, // No admin auth needed - internal endpoint
    );

  const registerTelegramUser = (chatId: string, userId: number) =>
    apiFetch(
      "/api/v1/users/register",
      {
        method: "POST",
        body: JSON.stringify({
          email: `telegram_${userId}@celebrum.ai`,
          password: `${globalThis.crypto.randomUUID()}${globalThis.crypto.randomUUID()}`,
          telegram_chat_id: chatId,
        }),
      },
      false,
    );

  const getOpportunities = () =>
    apiFetch<{ opportunities: any[] }>(
      "/api/v1/arbitrage/opportunities?limit=5&min_profit=0.5",
    );

  const healthCheck = () =>
    apiFetch<{ status: string; timestamp?: string }>("/health");

  /**
   * Verify admin API key is configured and matches backend
   * This helps diagnose configuration issues early
   */
  const verifyAdminAuth = () =>
    Effect.tryPromise(async () => {
      if (!config.adminApiKey) {
        console.warn("[API] ADMIN_API_KEY is not configured");
        return { valid: false, reason: "ADMIN_API_KEY not set" };
      }

      try {
        // Try to make a simple authenticated request
        const response = await fetch(
          `${config.apiBaseUrl}/internal/telegram/users/test`,
          {
            method: "GET",
            headers: {
              "Content-Type": "application/json",
              "X-API-Key": config.adminApiKey,
            },
            signal: AbortSignal.timeout(5000),
          },
        );

        // 404 is expected for a non-existent user, but indicates auth worked
        if (response.status === 404) {
          return {
            valid: true,
            reason: "Auth successful (user not found is expected)",
          };
        }

        // 401 means auth failed
        if (response.status === 401) {
          console.error("[API] ADMIN_API_KEY validation failed - key mismatch");
          return {
            valid: false,
            reason: "ADMIN_API_KEY mismatch with backend",
          };
        }

        return { valid: true, status: response.status };
      } catch (error) {
        console.error("[API] Admin auth verification failed:", error);
        return { valid: false, error: String(error) };
      }
    });

  /**
   * Get current trading mode for a chat
   */
  const getTradingMode = (chatId: string) =>
    apiFetch<{
      mode: string;
      confirmations: number;
      required_confirmations: number;
    }>(`/api/v1/trading-mode/${encodeURIComponent(chatId)}`);

  /**
   * Set trading mode for a chat
   */
  const setTradingMode = (chatId: string, mode: string) =>
    apiFetch<{ success: boolean; mode: string }>(
      `/api/v1/trading-mode/${encodeURIComponent(chatId)}`,
      {
        method: "PUT",
        body: JSON.stringify({ mode }),
      },
    );

  /**
   * Add confirmation for live mode
   */
  const addTradingModeConfirmation = (chatId: string) =>
    apiFetch<{
      confirmations: number;
      required: number;
    }>(`/api/v1/trading-mode/${encodeURIComponent(chatId)}/confirm`, {
      method: "POST",
    });

  return {
    getUserByChatId,
    getNotificationPreference,
    setNotificationPreference,
    registerTelegramUser,
    getOpportunities,
    healthCheck,
    verifyAdminAuth,
    getTradingMode,
    setTradingMode,
    addTradingModeConfirmation,
  };
};

export type Api = ReturnType<typeof createApi>;
