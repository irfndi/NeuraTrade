import { GrammyError } from "grammy";

/**
 * Error codes for structured error responses from Telegram API
 */
export enum TelegramErrorCode {
  /** User blocked the bot (403 Forbidden) */
  USER_BLOCKED = "USER_BLOCKED",
  /** Chat not found or user deleted account */
  CHAT_NOT_FOUND = "CHAT_NOT_FOUND",
  /** Rate limited by Telegram API (429 Too Many Requests) */
  RATE_LIMITED = "RATE_LIMITED",
  /** Invalid request (400 Bad Request) - malformed message, invalid chat_id, etc. */
  INVALID_REQUEST = "INVALID_REQUEST",
  /** Network error - connection failed, timeout, etc. */
  NETWORK_ERROR = "NETWORK_ERROR",
  /** Telegram API timeout */
  TIMEOUT = "TIMEOUT",
  /** Internal server error */
  INTERNAL_ERROR = "INTERNAL_ERROR",
  /** Unknown error */
  UNKNOWN = "UNKNOWN",
}

/**
 * Structured error response for Telegram API operations
 */
export interface TelegramErrorInfo {
  code: TelegramErrorCode;
  message: string;
  retryable: boolean;
  retryAfter?: number;
  originalError?: Error;
}

/**
 * Check if an error code indicates a retryable error
 */
export function isRetryableError(code: TelegramErrorCode): boolean {
  switch (code) {
    case TelegramErrorCode.RATE_LIMITED:
    case TelegramErrorCode.NETWORK_ERROR:
    case TelegramErrorCode.TIMEOUT:
    case TelegramErrorCode.INTERNAL_ERROR:
      return true;
    case TelegramErrorCode.USER_BLOCKED:
    case TelegramErrorCode.CHAT_NOT_FOUND:
    case TelegramErrorCode.INVALID_REQUEST:
    case TelegramErrorCode.UNKNOWN:
      return false;
    default:
      return false;
  }
}

/**
 * Classify a GrammyError into a structured error code
 */
export function classifyGrammyError(error: GrammyError): TelegramErrorInfo {
  const description = error.description?.toLowerCase() || "";
  const errorCode = error.error_code;

  // 403 Forbidden - User blocked the bot or chat restrictions
  if (errorCode === 403) {
    if (
      description.includes("blocked") ||
      description.includes("user is deactivated")
    ) {
      return {
        code: TelegramErrorCode.USER_BLOCKED,
        message: "User has blocked the bot or deactivated their account",
        retryable: false,
        originalError: error,
      };
    }
    return {
      code: TelegramErrorCode.CHAT_NOT_FOUND,
      message: "Chat not found or access denied",
      retryable: false,
      originalError: error,
    };
  }

  // 429 Too Many Requests - Rate limited
  if (errorCode === 429) {
    const retryAfter = error.parameters?.retry_after || 30;
    return {
      code: TelegramErrorCode.RATE_LIMITED,
      message: `Rate limited by Telegram API. Retry after ${retryAfter} seconds`,
      retryable: true,
      retryAfter,
      originalError: error,
    };
  }

  // 400 Bad Request - Invalid request
  if (errorCode === 400) {
    if (
      description.includes("chat not found") ||
      description.includes("chat_id is empty")
    ) {
      return {
        code: TelegramErrorCode.CHAT_NOT_FOUND,
        message: "Chat not found",
        retryable: false,
        originalError: error,
      };
    }
    return {
      code: TelegramErrorCode.INVALID_REQUEST,
      message: `Invalid request: ${error.description}`,
      retryable: false,
      originalError: error,
    };
  }

  // 500+ Server errors - Potentially retryable
  if (errorCode >= 500) {
    return {
      code: TelegramErrorCode.INTERNAL_ERROR,
      message: `Telegram server error: ${error.description}`,
      retryable: true,
      originalError: error,
    };
  }

  // Unknown error
  return {
    code: TelegramErrorCode.UNKNOWN,
    message: error.description || "Unknown error",
    retryable: false,
    originalError: error,
  };
}

/**
 * Classify a generic error into a structured error code
 */
export function classifyError(error: unknown): TelegramErrorInfo {
  if (error instanceof GrammyError) {
    return classifyGrammyError(error);
  }

  if (error instanceof Error) {
    const message = error.message.toLowerCase();

    // Check for timeout errors
    if (message.includes("timeout") || message.includes("timed out")) {
      return {
        code: TelegramErrorCode.TIMEOUT,
        message: "Request timed out",
        retryable: true,
        originalError: error,
      };
    }

    // Check for network errors
    if (
      message.includes("network") ||
      message.includes("connection") ||
      message.includes("econnrefused") ||
      message.includes("enotfound")
    ) {
      return {
        code: TelegramErrorCode.NETWORK_ERROR,
        message: `Network error: ${error.message}`,
        retryable: true,
        originalError: error,
      };
    }

    return {
      code: TelegramErrorCode.UNKNOWN,
      message: error.message,
      retryable: false,
      originalError: error,
    };
  }

  return {
    code: TelegramErrorCode.UNKNOWN,
    message: String(error),
    retryable: false,
  };
}
