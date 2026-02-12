import "./test-setup";
import { test, expect, describe } from "bun:test";
import { GrammyError } from "grammy";
import {
  TelegramErrorCode,
  TelegramErrorInfo,
  classifyGrammyError,
  classifyError,
  isRetryableError,
} from "./telegram-errors";

describe("TelegramErrorCode", () => {
  test("contains all expected error codes", () => {
    expect(String(TelegramErrorCode.USER_BLOCKED)).toBe("USER_BLOCKED");
    expect(String(TelegramErrorCode.CHAT_NOT_FOUND)).toBe("CHAT_NOT_FOUND");
    expect(String(TelegramErrorCode.RATE_LIMITED)).toBe("RATE_LIMITED");
    expect(String(TelegramErrorCode.INVALID_REQUEST)).toBe("INVALID_REQUEST");
    expect(String(TelegramErrorCode.NETWORK_ERROR)).toBe("NETWORK_ERROR");
    expect(String(TelegramErrorCode.TIMEOUT)).toBe("TIMEOUT");
    expect(String(TelegramErrorCode.INTERNAL_ERROR)).toBe("INTERNAL_ERROR");
    expect(String(TelegramErrorCode.UNKNOWN)).toBe("UNKNOWN");
  });
});

describe("isRetryableError", () => {
  test("returns true for retryable error codes", () => {
    expect(isRetryableError(TelegramErrorCode.RATE_LIMITED)).toBe(true);
    expect(isRetryableError(TelegramErrorCode.NETWORK_ERROR)).toBe(true);
    expect(isRetryableError(TelegramErrorCode.TIMEOUT)).toBe(true);
    expect(isRetryableError(TelegramErrorCode.INTERNAL_ERROR)).toBe(true);
  });

  test("returns false for non-retryable error codes", () => {
    expect(isRetryableError(TelegramErrorCode.USER_BLOCKED)).toBe(false);
    expect(isRetryableError(TelegramErrorCode.CHAT_NOT_FOUND)).toBe(false);
    expect(isRetryableError(TelegramErrorCode.INVALID_REQUEST)).toBe(false);
    expect(isRetryableError(TelegramErrorCode.UNKNOWN)).toBe(false);
  });
});

describe("classifyGrammyError", () => {
  test("classifies 403 errors as USER_BLOCKED", () => {
    const error = new GrammyError(
      "Forbidden: bot was blocked by the user",
      {
        ok: false,
        error_code: 403,
        description: "Forbidden: bot was blocked by the user",
      },
      "sendMessage",
      { chat_id: 123, text: "test" },
    );

    const result = classifyGrammyError(error);
    expect(result.code).toBe(TelegramErrorCode.USER_BLOCKED);
    expect(result.retryable).toBe(false);
  });

  test("classifies 400 chat not found errors", () => {
    const error = new GrammyError(
      "Bad Request: chat not found",
      {
        ok: false,
        error_code: 400,
        description: "Bad Request: chat not found",
      },
      "sendMessage",
      { chat_id: 123, text: "test" },
    );

    const result = classifyGrammyError(error);
    expect(result.code).toBe(TelegramErrorCode.CHAT_NOT_FOUND);
    expect(result.retryable).toBe(false);
  });

  test("classifies 429 errors as RATE_LIMITED with retry_after", () => {
    const error = new GrammyError(
      "Too Many Requests: retry after 60",
      {
        ok: false,
        error_code: 429,
        description: "Too Many Requests: retry after 60",
        parameters: { retry_after: 60 },
      },
      "sendMessage",
      { chat_id: 123, text: "test" },
    );

    const result = classifyGrammyError(error);
    expect(result.code).toBe(TelegramErrorCode.RATE_LIMITED);
    expect(result.retryable).toBe(true);
    expect(result.retryAfter).toBe(60);
  });

  test("classifies 500 errors as INTERNAL_ERROR", () => {
    const error = new GrammyError(
      "Internal Server Error",
      { ok: false, error_code: 500, description: "Internal Server Error" },
      "sendMessage",
      { chat_id: 123, text: "test" },
    );

    const result = classifyGrammyError(error);
    expect(result.code).toBe(TelegramErrorCode.INTERNAL_ERROR);
    expect(result.retryable).toBe(true);
  });

  test("classifies 400 errors as INVALID_REQUEST", () => {
    const error = new GrammyError(
      "Bad Request: message text is empty",
      {
        ok: false,
        error_code: 400,
        description: "Bad Request: message text is empty",
      },
      "sendMessage",
      { chat_id: 123, text: "" },
    );

    const result = classifyGrammyError(error);
    expect(result.code).toBe(TelegramErrorCode.INVALID_REQUEST);
    expect(result.retryable).toBe(false);
  });
});

describe("classifyError", () => {
  test("classifies GrammyError correctly", () => {
    const grammyError = new GrammyError(
      "Forbidden: bot was blocked by the user",
      {
        ok: false,
        error_code: 403,
        description: "Forbidden: bot was blocked by the user",
      },
      "sendMessage",
      { chat_id: 123, text: "test" },
    );

    const result = classifyError(grammyError);
    expect(result.code).toBe(TelegramErrorCode.USER_BLOCKED);
  });

  test("classifies timeout errors", () => {
    const timeoutError = new Error("Telegram API timeout");
    const result = classifyError(timeoutError);
    expect(result.code).toBe(TelegramErrorCode.TIMEOUT);
    expect(result.retryable).toBe(true);
  });

  test("classifies network errors", () => {
    const networkError = new Error("ECONNREFUSED");
    const result = classifyError(networkError);
    expect(result.code).toBe(TelegramErrorCode.NETWORK_ERROR);
    expect(result.retryable).toBe(true);
  });

  test("classifies unknown errors", () => {
    const unknownError = new Error("Some random error");
    const result = classifyError(unknownError);
    expect(result.code).toBe(TelegramErrorCode.UNKNOWN);
    expect(result.retryable).toBe(false);
  });

  test("handles non-Error objects", () => {
    const result = classifyError("string error");
    expect(result.code).toBe(TelegramErrorCode.UNKNOWN);
    expect(result.message).toBe("string error");
  });

  test("handles null/undefined", () => {
    const result = classifyError(null);
    expect(result.code).toBe(TelegramErrorCode.UNKNOWN);
    expect(result.message).toBe("null");
  });
});

describe("TelegramErrorInfo", () => {
  test("has all required fields", () => {
    const errorInfo: TelegramErrorInfo = {
      code: TelegramErrorCode.USER_BLOCKED,
      message: "Bot was blocked by user",
      retryable: false,
    };

    expect(errorInfo.code).toBe(TelegramErrorCode.USER_BLOCKED);
    expect(errorInfo.message).toBe("Bot was blocked by user");
    expect(errorInfo.retryable).toBe(false);
    expect(errorInfo.retryAfter).toBeUndefined();
  });

  test("can include retryAfter", () => {
    const errorInfo: TelegramErrorInfo = {
      code: TelegramErrorCode.RATE_LIMITED,
      message: "Too many requests",
      retryable: true,
      retryAfter: 60,
    };

    expect(errorInfo.retryAfter).toBe(60);
  });
});
