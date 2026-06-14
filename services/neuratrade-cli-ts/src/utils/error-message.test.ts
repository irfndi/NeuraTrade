import { describe, expect, it } from "bun:test";
import { Data } from "effect";
import { errorMessage } from "./error-message.ts";

class NetworkError extends Data.TaggedError("NetworkError")<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

class BitgetApiError extends Data.TaggedError("BitgetApiError")<{
  readonly status: number;
  readonly body: string;
  readonly endpoint: string;
}> {}

class CustomError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CustomError";
  }
}

describe("errorMessage", () => {
  describe("string handling", () => {
    it("returns the string unchanged", () => {
      expect(errorMessage("backend down")).toBe("backend down");
    });
    it("handles empty string", () => {
      expect(errorMessage("")).toBe("");
    });
  });

  describe("null and undefined", () => {
    it("returns 'null' for null", () => {
      expect(errorMessage(null)).toBe("null");
    });
    it("returns 'undefined' for undefined", () => {
      expect(errorMessage(undefined)).toBe("undefined");
    });
  });

  describe("Data.TaggedError", () => {
    it("extracts JSON-stringified fields from NetworkError (preserves cause and endpoint)", () => {
      const err = new NetworkError({
        cause: "backend down",
        endpoint: "/backtest",
      });
      const result = errorMessage(err);
      expect(result).toContain("NetworkError");
      expect(result).toContain("backend down");
      expect(result).toContain("/backtest");
    });

    it("extracts JSON-stringified fields from BitgetApiError (preserves status, body, endpoint)", () => {
      const err = new BitgetApiError({
        status: 429,
        body: "rate limit exceeded",
        endpoint: "/api/v1/placeOrder",
      });
      const result = errorMessage(err);
      expect(result).toContain("BitgetApiError");
      expect(result).toContain("429");
      expect(result).toContain("rate limit exceeded");
      expect(result).toContain("/api/v1/placeOrder");
    });

    it("does NOT return empty string for TaggedError (the original bug)", () => {
      const err = new NetworkError({
        cause: "connection refused",
        endpoint: "/health",
      });
      const result = errorMessage(err);
      expect(result).not.toBe("");
      expect(result.length).toBeGreaterThan(0);
    });
  });

  describe("regular Error", () => {
    it("returns the message for a standard Error", () => {
      expect(errorMessage(new Error("disk full"))).toBe("disk full");
    });
    it("returns the message for a custom Error subclass", () => {
      expect(errorMessage(new CustomError("custom failure"))).toBe(
        "custom failure",
      );
    });
    it("returns the name when message is empty", () => {
      const err = new Error("");
      expect(errorMessage(err)).toBe("Error");
    });
    it("returns the custom name when message is empty", () => {
      const err = new CustomError("");
      expect(errorMessage(err)).toBe("CustomError");
    });
  });

  describe("plain objects", () => {
    it("JSON-stringifies a plain object", () => {
      const obj = { code: "INVALID_ORDER", detail: "size too small" };
      const result = errorMessage(obj);
      expect(result).toContain("INVALID_ORDER");
      expect(result).toContain("size too small");
    });
  });

  describe("primitives and other", () => {
    it("stringifies a number", () => {
      expect(errorMessage(42)).toBe("42");
    });
    it("stringifies a boolean", () => {
      expect(errorMessage(false)).toBe("false");
    });
    it("stringifies an array", () => {
      const result = errorMessage([1, 2, 3]);
      expect(result).toContain("1");
      expect(result).toContain("2");
      expect(result).toContain("3");
    });
  });

  describe("circular references", () => {
    it("falls back to String() when JSON.stringify throws", () => {
      const obj: Record<string, unknown> = { name: "circular" };
      obj.self = obj;
      const result = errorMessage(obj);
      expect(typeof result).toBe("string");
      expect(result.length).toBeGreaterThan(0);
    });
  });
});
