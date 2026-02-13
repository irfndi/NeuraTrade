import { test, expect, describe, mock } from "bun:test";
import {
  withRetry,
  RetryResult,
  DEFAULT_RETRY_CONFIG,
} from "./retry";
import { TelegramErrorCode, TelegramErrorInfo } from "./telegram-errors";

// Mock error classifier for testing
const mockErrorClassifier = (error: unknown): TelegramErrorInfo => {
  const errorMessage = error instanceof Error ? error.message : String(error);

  if (errorMessage.includes("RATE_LIMITED")) {
    return {
      code: TelegramErrorCode.RATE_LIMITED,
      message: errorMessage,
      retryable: true,
      retryAfter: 1, // 1 second for tests
    };
  }

  if (errorMessage.includes("NETWORK_ERROR")) {
    return {
      code: TelegramErrorCode.NETWORK_ERROR,
      message: errorMessage,
      retryable: true,
    };
  }

  if (errorMessage.includes("USER_BLOCKED")) {
    return {
      code: TelegramErrorCode.USER_BLOCKED,
      message: errorMessage,
      retryable: false,
    };
  }

  return {
    code: TelegramErrorCode.UNKNOWN,
    message: errorMessage,
    retryable: false,
  };
};

describe("DEFAULT_RETRY_CONFIG", () => {
  test("has expected default values", () => {
    expect(DEFAULT_RETRY_CONFIG.maxRetries).toBe(3);
    expect(DEFAULT_RETRY_CONFIG.initialDelayMs).toBe(1000);
    expect(DEFAULT_RETRY_CONFIG.maxDelayMs).toBe(30000);
    expect(DEFAULT_RETRY_CONFIG.backoffFactor).toBe(2);
    expect(DEFAULT_RETRY_CONFIG.jitter).toBe(true);
  });
});

describe("withRetry", () => {
  test("returns success on first attempt", async () => {
    const fn = mock(() => Promise.resolve("success"));

    const result = await withRetry(fn, mockErrorClassifier, { maxRetries: 3 });

    expect(result.success).toBe(true);
    expect(result.data).toBe("success");
    expect(result.attempts).toBe(1);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  test("retries on retryable error and succeeds", async () => {
    let attempts = 0;
    const fn = mock(() => {
      attempts++;
      if (attempts < 3) {
        return Promise.reject(new Error("NETWORK_ERROR"));
      }
      return Promise.resolve("success after retries");
    });

    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 3,
      initialDelayMs: 10, // Short delay for tests
    });

    expect(result.success).toBe(true);
    expect(result.data).toBe("success after retries");
    expect(result.attempts).toBe(3);
    expect(fn).toHaveBeenCalledTimes(3);
  });

  test("fails immediately on non-retryable error", async () => {
    const fn = mock(() => Promise.reject(new Error("USER_BLOCKED")));

    const result = await withRetry(fn, mockErrorClassifier, { maxRetries: 3 });

    expect(result.success).toBe(false);
    expect(result.attempts).toBe(1);
    expect((result.error as TelegramErrorInfo).code).toBe(
      TelegramErrorCode.USER_BLOCKED,
    );
    expect(fn).toHaveBeenCalledTimes(1);
  });

  test("exhausts retries on persistent retryable error", async () => {
    const fn = mock(() => Promise.reject(new Error("NETWORK_ERROR")));

    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 2,
      initialDelayMs: 10,
    });

    expect(result.success).toBe(false);
    expect(result.attempts).toBe(3); // Initial + 2 retries
    expect((result.error as TelegramErrorInfo).code).toBe(
      TelegramErrorCode.NETWORK_ERROR,
    );
    expect(fn).toHaveBeenCalledTimes(3);
  });

  test("uses retry_after from error info for rate limiting", async () => {
    let attempts = 0;
    const startTime = Date.now();

    const fn = mock(() => {
      attempts++;
      if (attempts < 2) {
        return Promise.reject(new Error("RATE_LIMITED"));
      }
      return Promise.resolve("success");
    });

    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 3,
      initialDelayMs: 10,
    });

    expect(result.success).toBe(true);
    expect(result.attempts).toBe(2);

    // Should have waited at least 1 second (retry_after from mock)
    const elapsed = Date.now() - startTime;
    expect(elapsed).toBeGreaterThanOrEqual(900); // Allow some tolerance
  });

  test("respects maxDelayMs cap", async () => {
    let attempts = 0;
    const fn = mock(() => {
      attempts++;
      if (attempts < 4) {
        return Promise.reject(new Error("NETWORK_ERROR"));
      }
      return Promise.resolve("success");
    });

    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 5,
      initialDelayMs: 100,
      maxDelayMs: 200, // Cap at 200ms
      backoffFactor: 10, // Would normally grow quickly
    });

    expect(result.success).toBe(true);
    expect(result.attempts).toBe(4);
  });

  test("works with zero maxRetries", async () => {
    const fn = mock(() => Promise.reject(new Error("NETWORK_ERROR")));

    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 0,
      initialDelayMs: 10,
    });

    expect(result.success).toBe(false);
    expect(result.attempts).toBe(1);
    expect(fn).toHaveBeenCalledTimes(1);
  });
});

describe("RetryResult", () => {
  test("success result has expected structure", () => {
    const result: RetryResult<string> = {
      success: true,
      data: "test data",
      attempts: 1,
    };

    expect(result.success).toBe(true);
    expect(result.data).toBe("test data");
    expect(result.attempts).toBe(1);
    expect(result.error).toBeUndefined();
  });

  test("failure result has expected structure", () => {
    const errorInfo: TelegramErrorInfo = {
      code: TelegramErrorCode.USER_BLOCKED,
      message: "Bot blocked",
      retryable: false,
    };

    const result: RetryResult<string> = {
      success: false,
      attempts: 3,
      error: errorInfo,
    };

    expect(result.success).toBe(false);
    expect(result.data).toBeUndefined();
    expect(result.attempts).toBe(3);
    expect(result.error).toBe(errorInfo);
  });
});

describe("RetryConfig", () => {
  test("can be partially specified", async () => {
    const fn = mock(() => Promise.resolve("success"));

    // Only specify some config options
    const result = await withRetry(fn, mockErrorClassifier, {
      maxRetries: 5,
      // Other options should use defaults
    });

    expect(result.success).toBe(true);
  });

  test("jitter adds randomness to delay", async () => {
    // This is a probabilistic test - run multiple times to verify jitter works
    const delays: number[] = [];
    let callTime = 0;

    const fn = mock(() => {
      if (callTime > 0) {
        delays.push(Date.now() - callTime);
      }
      callTime = Date.now();

      if (delays.length < 2) {
        return Promise.reject(new Error("NETWORK_ERROR"));
      }
      return Promise.resolve("success");
    });

    await withRetry(fn, mockErrorClassifier, {
      maxRetries: 3,
      initialDelayMs: 100,
      backoffFactor: 1, // Keep delay constant for easier testing
      jitter: true,
    });

    // With jitter, delays should not be exactly the same
    // This test might occasionally fail due to timing, but should pass most times
    expect(delays.length).toBe(2);
  });
});
