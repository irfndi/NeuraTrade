import { Effect } from "effect";
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

// Custom error class that preserves both ApiError info and stack trace
export class ApiException extends Error {
  readonly apiError: ApiError;

  constructor(apiError: ApiError) {
    super(apiError.message);
    this.name = "ApiException";
    this.apiError = apiError;
    // Preserve stack trace
    if (Error.captureStackTrace) {
      Error.captureStackTrace(this, ApiException);
    }
  }
}

// WeakMap to cache extracted ApiErrors - avoids mutation
const extractedApiErrors = new WeakMap<object, ApiError>();

// Type guard for ApiError - also handles Effect's wrapped errors
// Uses immutable extraction with caching to avoid mutation
export const isApiError = (
  error: unknown,
  visited: WeakSet<object> = new WeakSet(),
): error is ApiError => {
  // Direct ApiError check
  if (
    typeof error === "object" &&
    error !== null &&
    "type" in error &&
    "message" in error &&
    typeof (error as ApiError).type === "string"
  ) {
    return true;
  }

  // Check for ApiException
  if (error instanceof ApiException) {
    return true;
  }

  // Check for Effect's wrapped error (UnknownException with cause)
  if (typeof error === "object" && error !== null && "cause" in error) {
    // Prevent infinite recursion with circular references
    if (visited.has(error)) {
      return false;
    }
    visited.add(error);

    const cause = (error as { cause: unknown }).cause;

    // The cause might be an Error with message containing our ApiError JSON
    if (cause instanceof Error && cause.message) {
      try {
        const parsed = JSON.parse(cause.message);
        if (
          parsed &&
          typeof parsed.type === "string" &&
          typeof parsed.message === "string"
        ) {
          // Cache the parsed ApiError instead of mutating
          extractedApiErrors.set(error, parsed as ApiError);
          return true;
        }
      } catch {
        // Not JSON, continue
      }
    }

    // Check for ApiException in cause
    if (cause instanceof ApiException) {
      extractedApiErrors.set(error, cause.apiError);
      return true;
    }

    // Recursive check with visited set to prevent infinite loops
    if (typeof cause === "object" && cause !== null) {
      return isApiError(cause, visited);
    }
  }

  return false;
};

// Extract ApiError from potentially wrapped errors (immutable)
// This function unwraps nested error structures to find ApiError
export const extractApiError = (error: unknown): ApiError | null => {
  if (error == null) {
    return null;
  }

  // Direct ApiError check
  if (
    typeof error === "object" &&
    "type" in error &&
    "message" in error &&
    typeof (error as ApiError).type === "string"
  ) {
    return error as ApiError;
  }

  // ApiException - extract the wrapped apiError
  if (error instanceof ApiException) {
    return error.apiError;
  }

  // Check cache first (populated by isApiError)
  if (typeof error === "object") {
    const cached = extractedApiErrors.get(error);
    if (cached) {
      return cached;
    }
  }

  // Follow Effect-style or generic error chaining via `cause`
  if (
    typeof error === "object" &&
    "cause" in error &&
    (error as { cause: unknown }).cause !== error // Prevent self-reference
  ) {
    const cause = (error as { cause: unknown }).cause;
    const extractedFromCause = extractApiError(cause);
    if (extractedFromCause) {
      // Cache the result for future lookups
      extractedApiErrors.set(error, extractedFromCause);
      return extractedFromCause;
    }
  }

  // Try to parse Error.message as JSON-encoded ApiError
  if (error instanceof Error && error.message) {
    try {
      const parsed = JSON.parse(error.message);
      if (
        parsed &&
        typeof parsed.type === "string" &&
        typeof parsed.message === "string"
      ) {
        const apiError: ApiError = {
          type: parsed.type as ApiErrorType,
          message: parsed.message,
          status: typeof parsed.status === "number" ? parsed.status : undefined,
          code: typeof parsed.code === "string" ? parsed.code : undefined,
        };
        // Cache the result
        extractedApiErrors.set(error, apiError);
        return apiError;
      }
    } catch {
      // Not JSON or not ApiError-shaped; fall through
    }
  }

  return null;
};

export const createApi = (config: TelegramConfigPartial) => {
  const apiFetch = <T>(
    path: string,
    init: RequestInit = {},
    requireAdmin = false,
  ) =>
    Effect.tryPromise(async () => {
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        ...(init.headers as Record<string, string> | undefined),
      };

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
    });

  // Internal endpoints - no auth required (network-isolated via Docker)
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

  /**
   * Health check to verify backend connectivity
   * Returns true if backend is reachable and responding
   */
  const healthCheck = () =>
    Effect.tryPromise(async () => {
      try {
        const response = await fetch(`${config.apiBaseUrl}/health`, {
          method: "GET",
          signal: AbortSignal.timeout(5000),
        });

        if (!response.ok) {
          console.error(
            `[API] Backend health check failed with status ${response.status}`,
          );
          return { healthy: false, status: response.status };
        }

        const data = await response.json().catch(() => ({}));
        return { healthy: true, status: response.status, data };
      } catch (error) {
        console.error("[API] Backend health check failed:", error);
        return { healthy: false, error: String(error) };
      }
    });

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
          `${config.apiBaseUrl}/api/v1/telegram/internal/users/test`,
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

  return {
    getUserByChatId,
    getNotificationPreference,
    setNotificationPreference,
    registerTelegramUser,
    getOpportunities,
    healthCheck,
    verifyAdminAuth,
  };
};

export type Api = ReturnType<typeof createApi>;
