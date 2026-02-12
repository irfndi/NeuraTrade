import { test, expect, describe } from "bun:test";
import { ApiError, ApiException, isApiError, extractApiError } from "./api";

describe("ApiException", () => {
  test("creates ApiException with proper properties", () => {
    const apiError: ApiError = {
      type: "network_error",
      message: "Connection failed",
      status: 503,
    };
    const exception = new ApiException(apiError);

    expect(exception).toBeInstanceOf(Error);
    expect(exception).toBeInstanceOf(ApiException);
    expect(exception.name).toBe("ApiException");
    expect(exception.message).toBe("Connection failed");
    expect(exception.apiError).toEqual(apiError);
  });

  test("preserves stack trace", () => {
    const exception = new ApiException({
      type: "server_error",
      message: "Internal error",
    });
    expect(exception.stack).toBeDefined();
    expect(exception.stack).toContain("ApiException");
  });
});

describe("isApiError", () => {
  test("returns true for direct ApiError object", () => {
    const error: ApiError = {
      type: "not_found",
      message: "Resource not found",
    };
    expect(isApiError(error)).toBe(true);
  });

  test("returns true for ApiException", () => {
    const exception = new ApiException({
      type: "auth_failed",
      message: "Invalid credentials",
    });
    expect(isApiError(exception)).toBe(true);
  });

  test("returns false for plain Error", () => {
    const error = new Error("Something went wrong");
    expect(isApiError(error)).toBe(false);
  });

  test("returns false for null", () => {
    expect(isApiError(null)).toBe(false);
  });

  test("returns false for undefined", () => {
    expect(isApiError(undefined)).toBe(false);
  });

  test("returns false for primitive values", () => {
    expect(isApiError("error")).toBe(false);
    expect(isApiError(42)).toBe(false);
    expect(isApiError(true)).toBe(false);
  });

  test("handles Effect wrapped error with JSON cause", () => {
    const apiError: ApiError = {
      type: "network_error",
      message: "Connection refused",
    };
    const wrappedError = {
      cause: new Error(JSON.stringify(apiError)),
    };
    expect(isApiError(wrappedError)).toBe(true);
  });

  test("handles Effect wrapped ApiException in cause", () => {
    const exception = new ApiException({
      type: "server_error",
      message: "Internal error",
    });
    const wrappedError = {
      cause: exception,
    };
    expect(isApiError(wrappedError)).toBe(true);
  });

  test("handles nested cause without infinite recursion", () => {
    const deeplyNested = {
      cause: {
        cause: {
          cause: new Error("Not an API error"),
        },
      },
    };
    expect(isApiError(deeplyNested)).toBe(false);
  });

  test("prevents infinite recursion with circular references", () => {
    const circularError: any = { cause: null };
    circularError.cause = circularError; // Circular reference
    // Should not throw or hang
    expect(() => isApiError(circularError)).not.toThrow();
    expect(isApiError(circularError)).toBe(false);
  });
});

describe("extractApiError", () => {
  test("extracts direct ApiError", () => {
    const error: ApiError = {
      type: "not_found",
      message: "Not found",
      status: 404,
    };
    const extracted = extractApiError(error);
    expect(extracted).toEqual(error);
  });

  test("extracts from ApiException", () => {
    const apiError: ApiError = {
      type: "auth_failed",
      message: "Unauthorized",
      status: 401,
    };
    const exception = new ApiException(apiError);
    const extracted = extractApiError(exception);
    expect(extracted).toEqual(apiError);
  });

  test("extracts from Effect wrapped error", () => {
    const apiError: ApiError = {
      type: "server_error",
      message: "Internal error",
    };
    const wrappedError = {
      cause: new Error(JSON.stringify(apiError)),
    };
    const extracted = extractApiError(wrappedError);
    expect(extracted).toEqual(apiError);
  });

  test("returns null for non-API errors", () => {
    const plainError = new Error("Plain error");
    expect(extractApiError(plainError)).toBeNull();
  });

  test("returns null for null input", () => {
    expect(extractApiError(null)).toBeNull();
  });

  test("returns null for undefined input", () => {
    expect(extractApiError(undefined)).toBeNull();
  });

  test("caches extracted errors for same object", () => {
    const apiError: ApiError = {
      type: "network_error",
      message: "Timeout",
    };
    const wrappedError = {
      cause: new Error(JSON.stringify(apiError)),
    };

    // First extraction
    const first = extractApiError(wrappedError);
    // Second extraction should use cache
    const second = extractApiError(wrappedError);

    expect(first).toEqual(apiError);
    expect(second).toEqual(apiError);
    expect(first).toBe(second); // Should be same object from cache
  });

  test("extracts from deeply nested cause chain", () => {
    const apiError: ApiError = {
      type: "auth_failed",
      message: "Token expired",
      status: 401,
    };
    const deeplyNested = {
      cause: {
        cause: new Error(JSON.stringify(apiError)),
      },
    };
    const extracted = extractApiError(deeplyNested);
    expect(extracted).toEqual(apiError);
  });

  test("extracts from Error with JSON message directly", () => {
    const apiError: ApiError = {
      type: "server_error",
      message: "Database connection failed",
      status: 500,
      code: "DB_ERROR",
    };
    const jsonError = new Error(JSON.stringify(apiError));
    const extracted = extractApiError(jsonError);
    expect(extracted).toEqual(apiError);
  });

  test("handles self-referencing cause", () => {
    const selfRef: any = { cause: null };
    selfRef.cause = selfRef;
    // Should not infinite loop
    expect(extractApiError(selfRef)).toBeNull();
  });

  test("extracts ApiException from cause chain", () => {
    const apiError: ApiError = {
      type: "network_error",
      message: "Connection refused",
    };
    const exception = new ApiException(apiError);
    const wrapped = { cause: exception };
    const extracted = extractApiError(wrapped);
    expect(extracted).toEqual(apiError);
  });
});

describe("ApiError types", () => {
  test("all error types are valid", () => {
    const types = [
      "auth_failed",
      "not_found",
      "server_error",
      "network_error",
      "unknown",
    ] as const;

    for (const type of types) {
      const error: ApiError = { type, message: `Error: ${type}` };
      expect(isApiError(error)).toBe(true);
    }
  });
});
