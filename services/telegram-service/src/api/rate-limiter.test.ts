import { describe, test, expect } from "bun:test";
import {
  RateLimiter,
  createRateLimiter,
  DEFAULT_RATE_LIMIT,
} from "./rate-limiter";

describe("RateLimiter", () => {
  describe("constructor", () => {
    test("creates limiter with default max tokens equal to rate", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 10 });
      expect(limiter.getAvailableTokens()).toBe(10);
    });

    test("creates limiter with custom max tokens", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 10, maxTokens: 20 });
      expect(limiter.getAvailableTokens()).toBe(20);
    });
  });

  describe("tryAcquireToken", () => {
    test("returns true when tokens available", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 5 });
      expect(limiter.tryAcquireToken()).toBe(true);
    });

    test("decrements token count", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 5 });
      limiter.tryAcquireToken();
      expect(limiter.getAvailableTokens()).toBe(4);
    });

    test("returns false when no tokens available", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 1, maxTokens: 1 });
      limiter.tryAcquireToken();
      expect(limiter.tryAcquireToken()).toBe(false);
    });
  });

  describe("acquireToken", () => {
    test("immediately resolves when tokens available", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 10 });
      const start = Date.now();
      await limiter.acquireToken();
      const elapsed = Date.now() - start;
      expect(elapsed).toBeLessThan(50);
    });

    test("waits for token when none available", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 100, maxTokens: 1 });
      limiter.tryAcquireToken();

      const start = Date.now();
      await limiter.acquireToken();
      const elapsed = Date.now() - start;

      expect(elapsed).toBeGreaterThanOrEqual(8);
    });

    test("allows multiple sequential acquisitions", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 100 });

      await limiter.acquireToken();
      await limiter.acquireToken();
      await limiter.acquireToken();

      expect(true).toBe(true);
    });
  });

  describe("getAvailableTokens", () => {
    test("returns current token count", () => {
      const limiter = new RateLimiter({ tokensPerSecond: 10 });
      expect(limiter.getAvailableTokens()).toBe(10);
    });

    test("refills tokens over time", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 100, maxTokens: 10 });

      for (let i = 0; i < 10; i++) {
        limiter.tryAcquireToken();
      }
      expect(limiter.getAvailableTokens()).toBe(0);

      await new Promise((resolve) => setTimeout(resolve, 110));

      expect(limiter.getAvailableTokens()).toBeGreaterThanOrEqual(10);
    });
  });

  describe("token bucket behavior", () => {
    test("tokens cap at maxTokens", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 1000, maxTokens: 5 });

      await new Promise((resolve) => setTimeout(resolve, 50));

      expect(limiter.getAvailableTokens()).toBe(5);
    });

    test("tokens accumulate gradually", async () => {
      const limiter = new RateLimiter({ tokensPerSecond: 100, maxTokens: 10 });

      for (let i = 0; i < 5; i++) {
        limiter.tryAcquireToken();
      }
      expect(limiter.getAvailableTokens()).toBe(5);

      await new Promise((resolve) => setTimeout(resolve, 30));

      const tokens = limiter.getAvailableTokens();
      expect(tokens).toBeGreaterThan(5);
    });
  });
});

describe("createRateLimiter", () => {
  test("creates RateLimiter with specified rate", () => {
    const limiter = createRateLimiter(50);
    expect(limiter.getAvailableTokens()).toBe(50);
  });

  test("creates RateLimiter with custom max tokens", () => {
    const limiter = createRateLimiter(10, 100);
    expect(limiter.getAvailableTokens()).toBe(100);
  });
});

describe("DEFAULT_RATE_LIMIT", () => {
  test("is 30 requests per second", () => {
    expect(DEFAULT_RATE_LIMIT).toBe(30);
  });
});
