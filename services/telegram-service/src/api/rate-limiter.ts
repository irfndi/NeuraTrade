export interface RateLimiterOptions {
  tokensPerSecond: number;
  maxTokens?: number;
}

export class RateLimiter {
  private tokens: number;
  private readonly maxTokens: number;
  private readonly refillIntervalMs: number;
  private lastRefill: number;
  private refillTimer: Timer | null = null;

  constructor(options: RateLimiterOptions) {
    this.maxTokens = options.maxTokens ?? options.tokensPerSecond;
    this.tokens = this.maxTokens;
    this.refillIntervalMs = 1000 / options.tokensPerSecond;
    this.lastRefill = Date.now();
  }

  async acquireToken(): Promise<void> {
    this.refillTokens();

    if (this.tokens >= 1) {
      this.tokens -= 1;
      return;
    }

    const waitTime = this.refillIntervalMs;
    await new Promise((resolve) => setTimeout(resolve, waitTime));
    return this.acquireToken();
  }

  tryAcquireToken(): boolean {
    this.refillTokens();

    if (this.tokens >= 1) {
      this.tokens -= 1;
      return true;
    }

    return false;
  }

  private refillTokens(): void {
    const now = Date.now();
    const elapsed = now - this.lastRefill;
    const tokensToAdd = elapsed / this.refillIntervalMs;

    if (tokensToAdd >= 1) {
      this.tokens = Math.min(this.maxTokens, this.tokens + tokensToAdd);
      this.lastRefill = now;
    }
  }

  getAvailableTokens(): number {
    this.refillTokens();
    return Math.floor(this.tokens);
  }
}

export function createRateLimiter(tokensPerSecond: number, maxTokens?: number): RateLimiter {
  return new RateLimiter({ tokensPerSecond, maxTokens });
}

export const DEFAULT_RATE_LIMIT = 30;
